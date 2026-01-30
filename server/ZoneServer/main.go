package main

import (
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Config struct {
	ServerName string `json:"server_name"`
	TickRateMs int    `json:"tick_rate_ms"`
}

func loadConfig() Config {
	file, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Failed to read config.json: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(file, &cfg); err != nil {
		log.Fatalf("Failed to parse config.json: %v", err)
	}

	return cfg
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime)

	cfg := loadConfig()

	log.Println("=================================")
	log.Println(cfg.ServerName)
	log.Println("Status: STARTED")
	log.Println("=================================")

	// Handle CTRL+C properly
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

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
