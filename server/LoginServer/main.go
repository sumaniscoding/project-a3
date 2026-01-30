package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

var db *sql.DB

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

		var dbPassword string
		err = db.QueryRow(
			"SELECT password FROM accounts WHERE username=$1",
			username,
		).Scan(&dbPassword)

		if err != nil || dbPassword != password {
			fmt.Fprintln(conn, "ERROR INVALID_CREDENTIALS")
			continue
		}

		token := generateSessionToken(username)

		_, err = db.Exec(
			"INSERT INTO sessions (token, username) VALUES ($1, $2)",
			token,
			username,
		)
		if err != nil {
			fmt.Fprintln(conn, "ERROR DB_FAILURE")
			continue
		}

		fmt.Fprintf(conn, "LOGIN_OK %s\n", token)
		log.Printf("User %s logged in, session created\n", username)
	}
}

func main() {
	var err error

	db, err = sql.Open(
		"postgres",
		"dbname=projecta3 sslmode=disable",
	)
	if err != nil {
		log.Fatal(err)
	}

	// Force actual connection
	if err = db.Ping(); err != nil {
		log.Fatal("DB connection failed:", err)
	}

	log.Println("LoginServer connected to DB")

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
