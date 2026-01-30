package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// --------------------
// Config
// --------------------

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

// --------------------
// TCP Server
// --------------------

func startTCPServer(port int) net.Listener {
	address := fmt.Sprintf(":%d", port)

	listener, err := net.Listen("tcp", address)
	if err != nil {
		log.Fatalf("Failed to listen on %s: %v", address, err)
	}

	log.Printf("TCP server listening on %s\n", address)
	return listener
}

// --------------------
// Main
// --------------------

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

	// Accept clients in a separate goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				log.Println("Accept error:", err)
				return
			}

			log.Println("Client connected:", conn.RemoteAddr())
			conn.Close()
		}
	}()

	// Server tick loop
	ticker := time.NewTicker(time.Duration(cfg.TickRateMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Println("Server tick")

		case <-shutdown:
			log.Println("Shutdown signal received")
			log.Println("Server shutting down cleanly...")
			return
		}
	}
}

