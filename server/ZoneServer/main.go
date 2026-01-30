package main

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/lib/pq"
)

//
// --------------------
// Config
// --------------------
//

type Config struct {
	ServerName string `json:"server_name"`
	TickRateMs int    `json:"tick_rate_ms"`
	ListenPort int    `json:"listen_port"`
}

func loadConfig() Config {
	data, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Failed to read config.json: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Fatalf("Failed to parse config.json: %v", err)
	}

	return cfg
}

//
// --------------------
// Client State
// --------------------
//

type Client struct {
	ID          int
	Conn        net.Conn
	SessionID   string
	LoggedIn    bool
	Username    string
	CharacterID int
	Character   string
}

//
// --------------------
// Server State
// --------------------
//

var (
	nextClientID = 1
	clients      = make(map[int]*Client)
	clientsMutex sync.Mutex
	db           *sql.DB
)

//
// --------------------
// Session Validation (DB)
// --------------------
//

func validateSessionToken(token string) (bool, string) {
	var username string
	err := db.QueryRow(
		"SELECT username FROM sessions WHERE token=$1",
		token,
	).Scan(&username)

	if err != nil {
		return false, ""
	}
	return true, username
}

//
// --------------------
// TCP Server
// --------------------
//

func startTCPServer(port int) net.Listener {
	address := fmt.Sprintf(":%d", port)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", address, err)
	}

	log.Printf("ZoneServer listening on %s\n", address)
	return listener
}

func handleClient(conn net.Conn) {
	defer conn.Close()

	// Register client
	clientsMutex.Lock()
	clientID := nextClientID
	nextClientID++

	client := &Client{
		ID:   clientID,
		Conn: conn,
	}
	clients[clientID] = client
	clientsMutex.Unlock()

	log.Printf("Client #%d connected from %s\n", clientID, conn.RemoteAddr())
	fmt.Fprintf(conn, "WELCOME %d\n", clientID)

	reader := bufio.NewReader(conn)

	// Rate limiting
	messageCount := 0
	lastReset := time.Now()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		// Reset rate limit window
		if time.Since(lastReset) > 5*time.Second {
			messageCount = 0
			lastReset = time.Now()
		}

		messageCount++
		if messageCount > 10 {
			fmt.Fprintf(conn, "ERROR RATE_LIMIT\n")
			log.Printf("Client #%d rate limited\n", clientID)
			break
		}

		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, "|", 2)

		if len(parts) != 2 {
			fmt.Fprintf(conn, "ERROR BAD_FORMAT\n")
			continue
		}

		version := parts[0]
		payload := parts[1]

		if version != "1" {
			fmt.Fprintf(conn, "ERROR BAD_VERSION\n")
			continue
		}

		// --------------------
		// SESSION AUTH
		// --------------------
		if strings.HasPrefix(payload, "SESSION ") {
			if client.LoggedIn {
				fmt.Fprintf(conn, "ERROR ALREADY_AUTHENTICATED\n")
				continue
			}

			token := strings.TrimSpace(strings.TrimPrefix(payload, "SESSION "))
			ok, username := validateSessionToken(token)
			if !ok {
				fmt.Fprintf(conn, "ERROR INVALID_SESSION\n")
				continue
			}

			client.SessionID = token
			client.Username = username
			client.LoggedIn = true

			fmt.Fprintf(conn, "SESSION_OK\n")
			log.Printf("Client #%d authenticated as %s\n", clientID, username)
			continue
		}

		// Reject everything until authenticated
		if !client.LoggedIn {
			fmt.Fprintf(conn, "ERROR NOT_AUTHENTICATED\n")
			continue
		}

		log.Printf("Client #%d cmd: %s\n", clientID, payload)

		// --------------------
		// COMMANDS
		// --------------------
		switch {
		case payload == "PING":
			fmt.Fprintf(conn, "PONG\n")

		case payload == "CHAR_LIST":
			rows, err := db.Query(
				"SELECT name, class, level FROM characters WHERE username=$1",
				client.Username,
			)
			if err != nil {
				fmt.Fprintf(conn, "ERROR DB_FAILURE\n")
				break
			}
			defer rows.Close()

			for rows.Next() {
				var name, class string
				var level int
				rows.Scan(&name, &class, &level)
				fmt.Fprintf(conn, "CHAR %s %s %d\n", name, class, level)
			}
			fmt.Fprintf(conn, "CHAR_LIST_END\n")

		case strings.HasPrefix(payload, "CHAR_CREATE "):
			parts := strings.Split(payload, " ")
			if len(parts) != 3 {
				fmt.Fprintf(conn, "ERROR BAD_FORMAT\n")
				break
			}

			name := parts[1]
			class := parts[2]

			_, err := db.Exec(
				"INSERT INTO characters (username, name, class) VALUES ($1, $2, $3)",
				client.Username, name, class,
			)
			if err != nil {
				fmt.Fprintf(conn, "ERROR CHAR_CREATE_FAILED\n")
				break
			}

			fmt.Fprintf(conn, "CHAR_CREATED %s\n", name)

		case strings.HasPrefix(payload, "CHAR_SELECT "):
			name := strings.TrimSpace(strings.TrimPrefix(payload, "CHAR_SELECT "))

			var id int
			err := db.QueryRow(
				"SELECT id FROM characters WHERE username=$1 AND name=$2",
				client.Username, name,
			).Scan(&id)

			if err != nil {
				fmt.Fprintf(conn, "ERROR CHAR_NOT_FOUND\n")
				break
			}

			client.CharacterID = id
			client.Character = name

			fmt.Fprintf(conn, "CHAR_SELECTED %s\n", name)
			log.Printf("Client #%d selected character %s\n", clientID, name)

		default:
			fmt.Fprintf(conn, "ERROR UNKNOWN_COMMAND\n")
		}
	}

	// Cleanup
	clientsMutex.Lock()
	delete(clients, clientID)
	clientsMutex.Unlock()

	log.Printf("Client #%d disconnected\n", clientID)
}

//
// --------------------
// Main
// --------------------
//

func main() {
	log.SetFlags(log.Ldate | log.Ltime)

	cfg := loadConfig()

	log.Println("=================================")
	log.Println(cfg.ServerName)
	log.Println("Status: STARTED")
	log.Println("=================================")

	// Connect to DB
	var err error
	db, err = sql.Open(
		"postgres",
		"dbname=projecta3 sslmode=disable",
	)
	if err != nil {
		log.Fatal(err)
	}

	if err = db.Ping(); err != nil {
		log.Fatal("ZoneServer DB connection failed:", err)
	}

	log.Println("ZoneServer connected to DB")

	// Handle shutdown
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	listener := startTCPServer(cfg.ListenPort)
	defer listener.Close()

	// Accept connections
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleClient(conn)
		}
	}()

	// Tick loop
	ticker := time.NewTicker(time.Duration(cfg.TickRateMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			clientsMutex.Lock()
			count := len(clients)
			clientsMutex.Unlock()
			log.Printf("Server tick | Connected clients: %d\n", count)

		case <-shutdown:
			log.Println("Shutdown signal received")
			log.Println("ZoneServer shutting down cleanly...")
			return
		}
	}
}
