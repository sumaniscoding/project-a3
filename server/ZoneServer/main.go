package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
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
	ID   int
	Conn net.Conn
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

	// Block until client disconnects
	buffer := make([]byte, 1)
	for {
		_, err := conn.Read(buffer)
		if err != nil {
			break
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

