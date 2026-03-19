package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type LoginRequest struct {
	Command  string                 `json:"command"`
	Payload  map[string]interface{} `json:"payload"`
	Username string                 `json:"username"`
	Password string                 `json:"password"`
	Token    string                 `json:"token"`
}

type LoginResponse struct {
	Command string      `json:"command"`
	Payload interface{} `json:"payload"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     checkWebSocketOrigin,
}

func main() {
	if err := validateAuthConfig(); err != nil {
		log.Fatalf("Invalid auth configuration: %v", err)
	}

	log.Println("=================================")
	log.Println("Project A3 Login Server")
	log.Println("Status: STARTED")
	log.Println("=================================")

	mux := http.NewServeMux()
	registerHealthEndpoints(mux)
	mux.HandleFunc("/ws", handleWebSocket)

	log.Println("LoginServer listening on :5555 (WebSocket path: /ws)")
	if err := http.ListenAndServe(":5555", mux); err != nil {
		log.Fatalf("Failed to start LoginServer: %v", err)
	}
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	log.Printf("Login client connected: %s", remoteAddr)
	peerKey := loginPeerKey(remoteAddr)

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Login connection read error from %s: %v", remoteAddr, err)
			}
			break
		}

		log.Printf("Login request from %s: %s", remoteAddr, string(message))

		var req LoginRequest
		if err := json.Unmarshal(message, &req); err != nil {
			sendWS(conn, "ERROR", "INVALID_JSON")
			continue
		}

		switch strings.ToUpper(strings.TrimSpace(req.Command)) {
		case "PING":
			sendWS(conn, "PONG", map[string]interface{}{"ts": time.Now().UTC().Format(time.RFC3339)})
		case "REGISTER":
			if ok, wait := allowLoginAuthAttempt(peerKey); !ok {
				sendWS(conn, "RATE_LIMITED", map[string]interface{}{"retry_after_sec": int(wait.Seconds())})
				continue
			}

			username, password := credentialsFromRequest(req)
			if username == "" || password == "" {
				sendWS(conn, "REGISTER_DENIED", "MISSING_CREDENTIALS")
				continue
			}

			registeredUsername, err := registerLoginAccount(username, password)
			if err != nil {
				switch {
				case isAccountExistsError(err):
					sendWS(conn, "REGISTER_DENIED", "ACCOUNT_EXISTS")
				case isInvalidCredentialsError(err):
					sendWS(conn, "REGISTER_DENIED", err.Error())
				default:
					log.Printf("account registration failed: %v", err)
					sendWS(conn, "ERROR", "INTERNAL_ERROR")
				}
				continue
			}

			sendWS(conn, "REGISTER_OK", map[string]interface{}{
				"username": registeredUsername,
			})
		case "LOGIN":
			if ok, wait := allowLoginAuthAttempt(peerKey); !ok {
				sendWS(conn, "RATE_LIMITED", map[string]interface{}{"retry_after_sec": int(wait.Seconds())})
				continue
			}

			username, password := credentialsFromRequest(req)
			if username == "" || password == "" {
				sendWS(conn, "LOGIN_DENIED", "MISSING_CREDENTIALS")
				continue
			}

			accountUsername, ok, err := verifyLoginCredentials(username, password)
			if err != nil {
				if isInvalidCredentialsError(err) {
					sendWS(conn, "LOGIN_DENIED", err.Error())
					continue
				}
				log.Printf("credential verification failed: %v", err)
				sendWS(conn, "ERROR", "INTERNAL_ERROR")
				continue
			}
			if !ok {
				sendWS(conn, "LOGIN_DENIED", "INVALID_CREDENTIALS")
				continue
			}

			token, expires, err := issueAuthToken(accountUsername, 30*time.Minute)
			if err != nil {
				log.Printf("token generation failed: %v", err)
				sendWS(conn, "ERROR", "INTERNAL_ERROR")
				continue
			}

			sendWS(conn, "LOGIN_OK", map[string]interface{}{
				"username": accountUsername,
				"token":    token,
				"expires":  expires.Format(time.RFC3339),
			})
		case "VALIDATE":
			tokenStr := requestString(req, "token")
			claims, err := parseAndValidateAuthToken(strings.TrimSpace(tokenStr))
			if err != nil {
				sendWS(conn, "TOKEN_INVALID", err.Error())
				continue
			}
			sendWS(conn, "TOKEN_VALID", map[string]interface{}{
				"username": claims.Username,
				"expires":  time.Unix(claims.Exp, 0).UTC().Format(time.RFC3339),
			})
		default:
			sendWS(conn, "ERROR", "UNKNOWN_COMMAND")
		}
	}

	log.Printf("Login client disconnected: %s", remoteAddr)
}

func sendWS(conn *websocket.Conn, command string, payload interface{}) {
	res := LoginResponse{Command: command, Payload: payload}
	if err := conn.WriteJSON(res); err != nil {
		log.Printf("send response failed: %v", err)
	}
}

func credentialsFromRequest(req LoginRequest) (string, string) {
	return requestString(req, "username"), requestString(req, "password")
}

func requestString(req LoginRequest, key string) string {
	switch key {
	case "username":
		if value := strings.TrimSpace(req.Username); value != "" {
			return value
		}
	case "password":
		if value := strings.TrimSpace(req.Password); value != "" {
			return value
		}
	case "token":
		if value := strings.TrimSpace(req.Token); value != "" {
			return value
		}
	}
	if req.Payload == nil {
		return ""
	}
	value, _ := req.Payload[key].(string)
	return strings.TrimSpace(value)
}
