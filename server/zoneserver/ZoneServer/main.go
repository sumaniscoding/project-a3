package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var worlds map[WorldID]*World

const authTimeout = 15 * time.Second

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

func sendMessage(conn net.Conn, msg ServerMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("Failed to marshal message: %v", err)
		return
	}
	data = append(data, '\n')
	_, err = conn.Write(data)
	if err != nil {
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
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Failed to start ZoneServer: %v", err)
	}
	log.Printf("ZoneServer listening on %s", listenAddr)

	ctx, cancel := context.WithCancel(context.Background())
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					log.Printf("Accept error: %v", err)
					continue
				}
			}
			go handleClient(conn)
		}
	}()

	tickRate := time.Duration(cfg.TickRateMS) * time.Millisecond
	ticker := time.NewTicker(tickRate)
	go func() {
		for {
			select {
			case <-ticker.C:
				log.Printf("Server tick (%dms)", cfg.TickRateMS)
			case <-ctx.Done():
				return
			}
		}
	}()

	<-sigChan
	cancel()
	ticker.Stop()
	listener.Close()
	log.Println("ZoneServer shut down cleanly")
}

func handleClient(conn net.Conn) {
	defer conn.Close()
	log.Printf("Client connected: %s", conn.RemoteAddr())
	peerKey := zonePeerKey(conn.RemoteAddr().String())

	session := NewSession(conn)
	registerSession(session)
	defer unregisterSession(session)

	character := MockCharacter()
	character.Name = fmt.Sprintf("Guest_%s", sanitizeCharacterName(conn.RemoteAddr().String()))
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
		if err := persistCharacter(session.Character); err != nil {
			log.Printf("Failed to persist character %s: %v", session.Character.Name, err)
		}
	}()

	sendMessage(conn, ServerMessage{Command: RespAuthRequired, Payload: MsgLoginRequired})

	visible := make(map[*ClientSession]bool)

	reader := bufio.NewScanner(conn)
	reader.Buffer(make([]byte, 0, 4096), 1024*1024)

	for session.Active {
		if !session.Authenticated {
			_ = conn.SetReadDeadline(time.Now().Add(authTimeout))
		} else {
			_ = conn.SetReadDeadline(time.Time{})
		}

		if !reader.Scan() {
			break
		}

		modified := false

		var msg ClientMessage
		if err := json.Unmarshal(reader.Bytes(), &msg); err != nil {
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
			if err := persistCharacter(session.Character); err != nil {
				log.Printf("Failed to persist character %s: %v", session.Character.Name, err)
			}
		}
	}

	for other := range visible {
		sendMessage(other.Conn, ServerMessage{Command: RespPlayerLeft, Payload: session.Character.Name})
	}

	if err := reader.Err(); err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			log.Printf("Client auth timeout: %s", conn.RemoteAddr())
		} else {
			log.Printf("Client read error from %s: %v", conn.RemoteAddr(), err)
		}
	}

	log.Printf("Client disconnected: %s", conn.RemoteAddr())
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
				Payload: map[string]interface{}{"name": session.Character.Name, "pos": session.Position},
			})
			sendMessage(session.Conn, ServerMessage{
				Command: RespPlayerJoined,
				Payload: map[string]interface{}{"name": other.Character.Name, "pos": other.Position},
			})
			visible[other] = true
		}
	})
}
