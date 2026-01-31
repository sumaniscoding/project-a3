package main

import (
	"log"
	"net"
)

func main() {
	log.Println("=================================")
	log.Println("Project A3 Login Server")
	log.Println("Status: STARTED")
	log.Println("=================================")

	listener, err := net.Listen("tcp", ":5555")
	if err != nil {
		log.Fatalf("Failed to start LoginServer: %v", err)
	}
	defer listener.Close()

	log.Println("LoginServer listening on :5555")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}

		log.Printf("Login client connected: %s", conn.RemoteAddr())

		// Placeholder for auth logic
		conn.Close()
	}
}

