package main

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"strings"
	"time"
)

type LoginRequest struct {
	Command  string `json:"command"`
	Username string `json:"username"`
	Password string `json:"password"`
	Token    string `json:"token"`
}

type LoginResponse struct {
	Command string      `json:"command"`
	Payload interface{} `json:"payload"`
}

func main() {
	if err := validateAuthConfig(); err != nil {
		log.Fatalf("Invalid auth configuration: %v", err)
	}

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

		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()

	log.Printf("Login client connected: %s", conn.RemoteAddr())
	peerKey := loginPeerKey(conn.RemoteAddr().String())

	reader := bufio.NewScanner(conn)
	reader.Buffer(make([]byte, 0, 1024), 1024*1024)

	for reader.Scan() {
		log.Printf("Login request from %s: %s", conn.RemoteAddr(), reader.Text())

		var req LoginRequest
		if err := json.Unmarshal(reader.Bytes(), &req); err != nil {
			send(conn, "ERROR", "INVALID_JSON")
			continue
		}

		switch strings.ToUpper(strings.TrimSpace(req.Command)) {
		case "PING":
			send(conn, "PONG", map[string]interface{}{"ts": time.Now().UTC().Format(time.RFC3339)})
		case "LOGIN":
			if ok, wait := allowLoginAuthAttempt(peerKey); !ok {
				send(conn, "RATE_LIMITED", map[string]interface{}{"retry_after_sec": int(wait.Seconds())})
				continue
			}
			if strings.TrimSpace(req.Username) == "" || strings.TrimSpace(req.Password) == "" {
				send(conn, "LOGIN_DENIED", "MISSING_CREDENTIALS")
				continue
			}
			token, expires, err := issueAuthToken(req.Username, 30*time.Minute)
			if err != nil {
				log.Printf("token generation failed: %v", err)
				send(conn, "ERROR", "INTERNAL_ERROR")
				continue
			}

			send(conn, "LOGIN_OK", map[string]interface{}{
				"username": req.Username,
				"token":    token,
				"expires":  expires.Format(time.RFC3339),
			})
		case "VALIDATE":
			claims, err := parseAndValidateAuthToken(strings.TrimSpace(req.Token))
			if err != nil {
				send(conn, "TOKEN_INVALID", err.Error())
				continue
			}
			send(conn, "TOKEN_VALID", map[string]interface{}{
				"username": claims.Username,
				"expires":  time.Unix(claims.Exp, 0).UTC().Format(time.RFC3339),
			})
		default:
			send(conn, "ERROR", "UNKNOWN_COMMAND")
		}
	}

	if err := reader.Err(); err != nil {
		log.Printf("Login connection read error from %s: %v", conn.RemoteAddr(), err)
	}

	log.Printf("Login client disconnected: %s", conn.RemoteAddr())
}

func send(conn net.Conn, command string, payload interface{}) {
	res := LoginResponse{Command: command, Payload: payload}
	data, err := json.Marshal(res)
	if err != nil {
		log.Printf("marshal response failed: %v", err)
		return
	}
	data = append(data, '\n')
	if _, err := conn.Write(data); err != nil {
		log.Printf("send response failed: %v", err)
	}
}
