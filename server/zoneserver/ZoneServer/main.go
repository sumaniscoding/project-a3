package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

var worlds map[WorldID]*World

const authTimeout = 15 * time.Second

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin:     checkWebSocketOrigin,
}

func canEnterWorld(c *Character, w *World) (bool, string) {
	if w == nil {
		return false, "WORLD_NOT_FOUND"
	}
	worldUnlocked := w.Unlocked || c.UnlockedWorlds[w.ID]
	if !worldUnlocked {
		return false, "WORLD_LOCKED"
	}
	if c.Level < w.MinLevel || c.Level > w.MaxLevel {
		return false, "LEVEL_NOT_IN_RANGE"
	}
	if w.RequiresAura && c.AuraLevel == 0 {
		return false, "AURA_REQUIRED"
	}
	return true, "OK"
}

func sendMessage(conn WSConn, msg ServerMessage) {
	if err := conn.WriteJSON(msg); err != nil {
		log.Printf("Failed to write message: %v", err)
	}
}

func main() {
	if err := validateAuthConfig(); err != nil {
		log.Fatalf("Invalid auth configuration: %v", err)
	}

	rand.Seed(time.Now().UnixNano())
	cfg := loadZoneConfig("config.json")

	log.Println("=================================")
	log.Println(cfg.ServerName)
	log.Println("Status: STARTED")
	log.Println("=================================")

	worlds = DefaultWorlds()
	initWorldEntities()

	log.Println("World status:")
	for _, w := range worlds {
		log.Printf(" - %s (Lv %d-%d) | Unlocked=%v | AuraRequired=%v", w.Name, w.MinLevel, w.MaxLevel, w.Unlocked, w.RequiresAura)
	}

	listenAddr := fmt.Sprintf(":%d", cfg.ListenPort)

	// Init Redis Pub/Sub bus (graceful fallback if unavailable)
	InitRedis()

	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	mux := http.NewServeMux()
	registerHealthEndpoints(mux)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		handleClient(conn)
	})

	server := &http.Server{Addr: listenAddr, Handler: mux}

	go func() {
		log.Printf("ZoneServer listening on %s (WebSocket path: /ws)", listenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start ZoneServer: %v", err)
		}
	}()

	tickRate := time.Duration(cfg.TickRateMS) * time.Millisecond
	ticker := time.NewTicker(tickRate)
	presenceTicker := time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				processServerTick()
			case <-presenceTicker.C:
				refreshRedisPresence()
			case <-ctx.Done():
				return
			}
		}
	}()

	<-sigChan
	cancel()
	ticker.Stop()
	presenceTicker.Stop()
	server.Shutdown(context.Background())
	log.Println("ZoneServer shut down cleanly")
}

func handleClient(conn *websocket.Conn) {
	defer conn.Close()
	remoteAddrStr := conn.RemoteAddr().String()
	log.Printf("Client connected: %s", remoteAddrStr)
	peerKey := zonePeerKey(remoteAddrStr)

	session := NewSession(conn)
	registerSession(session)
	defer unregisterSession(session)

	character := MockCharacter()
	character.Name = fmt.Sprintf("Guest_%s", sanitizeCharacterName(remoteAddrStr))
	ensureCharacterDefaults(character)

	session.Character = character
	session.World = worlds[World1]
	session.Position = DefaultSpawnPosition(World1)
	boundName := ""
	defer func() {
		if boundName != "" {
			unbindSessionCharacterName(session, boundName)
		}
	}()
	defer func() {
		if !session.Authenticated {
			return
		}
		if err := persistSessionState(session); err != nil {
			log.Printf("Failed to persist character %s: %v", session.Character.Name, err)
		}
	}()

	sendMessage(conn, ServerMessage{Command: RespAuthRequired, Payload: MsgLoginRequired})

	visible := make(map[*ClientSession]bool)

	for session.Active {
		if !session.Authenticated {
			_ = conn.SetReadDeadline(time.Now().Add(authTimeout))
		} else {
			_ = conn.SetReadDeadline(time.Time{})
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Client read error from %s: %v", remoteAddrStr, err)
			}
			break
		}

		modified := false

		var msg ClientMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}
		cmd := strings.ToUpper(msg.Command)
		if !session.allowCommand(time.Now()) {
			sendMessage(conn, ServerMessage{Command: RespRateLimited, Payload: MsgTooManyRequests})
			continue
		}
		if !session.Authenticated {
			if cmd != ReqAuthToken {
				sendMessage(conn, ServerMessage{Command: RespAuthRequired, Payload: MsgLoginRequired})
				continue
			}
		}

		handled, modified := handleClientCommand(conn, session, visible, peerKey, &boundName, cmd, msg.Payload)
		if !handled {
			sendMessage(conn, ServerMessage{Command: RespError, Payload: MsgUnknownCommand})
			continue
		}

		if modified {
			if err := persistSessionState(session); err != nil {
				log.Printf("Failed to persist character %s: %v", session.Character.Name, err)
			}
		}
	}

	for other := range visible {
		sendMessage(other.Conn, ServerMessage{Command: RespPlayerLeft, Payload: session.Character.Name})
	}

	log.Printf("Client disconnected: %s", remoteAddrStr)
}

func persistSessionState(session *ClientSession) error {
	if session == nil || session.Character == nil {
		return nil
	}
	if err := persistCharacter(session.Character); err != nil {
		return err
	}
	if session.Account != nil {
		syncAccountFromCharacter(session.Account, session.Character)
		if err := persistAccount(session.Account); err != nil {
			return err
		}
	}
	return nil
}

func toMap(v interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{}
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	return m
}

func toString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

func toInt(m map[string]interface{}, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	default:
		return 0
	}
}

func syncInitialVisibility(session *ClientSession, visible map[*ClientSession]bool) {
	forEachSession(func(other *ClientSession) {
		if other == session || other.World.ID != session.World.ID {
			return
		}
		if isVisible(other.Position, session.Position) {
			sendMessage(other.Conn, ServerMessage{
				Command: RespPlayerJoined,
				Payload: map[string]interface{}{
					"name":   session.Character.Name,
					"pos":    session.Position,
					"class":  session.Character.Class,
					"weapon": getWeaponType(session.Character),
				},
			})
			sendMessage(session.Conn, ServerMessage{
				Command: RespPlayerJoined,
				Payload: map[string]interface{}{
					"name":   other.Character.Name,
					"pos":    other.Position,
					"class":  other.Character.Class,
					"weapon": getWeaponType(other.Character),
				},
			})
			visible[other] = true
		}
	})
}

func getWeaponType(c *Character) string {
	if c == nil || c.Equipped == nil {
		return ""
	}
	weaponID, ok := c.Equipped[SlotWeapon]
	if !ok {
		return ""
	}
	// Find the item in inventory to get its name
	for _, item := range c.Inventory {
		if item.ID == weaponID {
			return item.Name
		}
	}
	return ""
}
