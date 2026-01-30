package main

import (
	"bufio"
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
	ID        int
	Conn      net.Conn
	Username  string
	SessionID string
	LoggedIn  bool
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
)

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

	log.Printf("TCP server listening on %s\n", address)
	return listener
}

func generateSessionToken(clientID int) string {
	return fmt.Sprintf("SID-%d-%d", clientID, time.Now().UnixNano())
}

func handleClient(conn net.Conn) {
	defer conn.Close()

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

	messageCount := 0
	lastReset := time.Now()

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}

		// Rate limiting
		if time.Since(lastReset) > 5*time.Second {
			messageCount = 0
			lastReset = time.Now()
		}

		messageCount++
		if messageCount > 10 {
			fmt.Fprintf(conn, "ERROR RATE_LIMIT\n")
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

		// LOGIN command (special)
		if strings.HasPrefix(payload, "LOGIN ") {
			if client.LoggedIn {
				fmt.Fprintf(conn, "ERROR ALREADY_LOGGED_IN\n")
				continue
			}

			username := strings.TrimSpace(strings.TrimPrefix(payload, "LOGIN "))
			if username == "" {
				fmt.Fprintf(conn, "ERROR BAD_LOGIN\n")
				continue
			}

			client.Username = username
			client.SessionID = generateSessionToken(client.ID)
			client.LoggedIn = true

			fmt.Fprintf(conn, "LOGIN_OK %s\n", client.SessionID)
			log.Printf("Client #%d logged in as %s\n", client.ID, client.Username)
			continue
		}

		// Reject all other commands if not logged in
		if !client.LoggedIn {
			fmt.Fprintf(conn, "ERROR NOT_LOGGED_IN\n")
			continue
		}

		log.Printf("Client #%d cmd: %s\n", client.ID, payload)

		switch payload {
		case "PING":
			fmt.Fprintf(conn, "PONG\n")

		default:
			fmt.Fprintf(conn, "ERROR UNKNOWN_COMMAND\n")
		}
	}

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

	// Handle shutdown signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	// Start TCP server
	listener := startTCPServer(cfg.ListenPort)
	defer listener.Close()

	// Accept connections
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				// This happens during normal shutdown
				return
			}
			go handleClient(conn)
		}
	}()

	// Server tick loop
	ticker := time.NewTicker(time.Duration(cfg.TickRateMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			clientsMutex.Lock()
			clientCount := len(clients)
			clientsMutex.Unlock()

			log.Printf("Server tick | Connected clients: %d\n", clientCount)

		case <-shutdown:
			log.Println("Shutdown signal received")
			log.Println("Server shutting down cleanly...")
			return
		}
	}
}

