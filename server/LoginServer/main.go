package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

var users = map[string]string{
	"anthony": "password",
	"test":    "password",
}

func generateSessionToken(username string) string {
	return fmt.Sprintf("TOKEN-%s-%d", username, time.Now().UnixNano())
}

func handleClient(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	fmt.Fprintln(conn, "LOGIN_SERVER_READY")

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}

		line = strings.TrimSpace(line)
		parts := strings.Split(line, " ")

		if len(parts) != 3 || parts[0] != "LOGIN" {
			fmt.Fprintln(conn, "ERROR BAD_FORMAT")
			continue
		}

		username := parts[1]
		password := parts[2]

		if users[username] != password {
			fmt.Fprintln(conn, "ERROR INVALID_CREDENTIALS")
			continue
		}

		token := generateSessionToken(username)
		fmt.Fprintf(conn, "LOGIN_OK %s\n", token)
		log.Printf("User %s logged in, token issued\n", username)
	}
}

func main() {
	listener, err := net.Listen("tcp", ":8888")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("LoginServer listening on :8888")

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleClient(conn)
	}
}

