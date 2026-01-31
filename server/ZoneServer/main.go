package main

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var worlds map[WorldID]*World

// --------------------
// WORLD ENTRY CHECK
// --------------------
func canEnterWorld(c *Character, w *World) (bool, string) {
	if w == nil {
		return false, "WORLD_NOT_FOUND"
	}
	if !w.Unlocked {
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

// --------------------
// NETWORK SEND HELPER
// --------------------
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

// --------------------
// MAIN
// --------------------
func main() {
	log.Println("=================================")
	log.Println("Project A3 Zone Server")
	log.Println("Status: STARTED")
	log.Println("=================================")

	// Initialize worlds
	worlds = DefaultWorlds()

	log.Println("World status:")
	for _, w := range worlds {
		log.Printf(
			" - %s (Lv %d–%d) | Unlocked=%v | AuraRequired=%v",
			w.Name, w.MinLevel, w.MaxLevel, w.Unlocked, w.RequiresAura,
		)
	}

	listener, err := net.Listen("tcp", ":7777")
	if err != nil {
		log.Fatalf("Failed to start ZoneServer: %v", err)
	}
	log.Println("ZoneServer listening on :7777")

	ctx, cancel := context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Accept loop
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

	// Server tick
	ticker := time.NewTicker(1 * time.Second)
	go func() {
		for {
			select {
			case <-ticker.C:
				log.Println("Server tick")
			case <-ctx.Done():
				return
			}
		}
	}()

	<-sigChan
	log.Println("Shutdown signal received")

	cancel()
	ticker.Stop()
	listener.Close()

	log.Println("ZoneServer shut down cleanly")
}

// --------------------
// CLIENT HANDLER
// --------------------
func handleClient(conn net.Conn) {
	defer conn.Close()

	log.Printf("Client connected: %s", conn.RemoteAddr())

	session := NewSession(conn)
	registerSession(session)
	defer unregisterSession(session)

	character := MockCharacter()
	world := worlds[character.WorldID]

	ok, reason := canEnterWorld(character, world)
	if !ok {
		sendMessage(conn, ServerMessage{
			Command: "ENTER_DENIED",
			Payload: reason,
		})
		return
	}

	session.Character = character
	session.World = world
	session.Position = DefaultSpawnPosition(world.ID)

	sendMessage(conn, ServerMessage{
		Command: "ENTER_OK",
		Payload: map[string]interface{}{
			"character": character.Name,
			"world":     world.Name,
			"spawn":     session.Position,
		},
	})

	// Track visibility
	visible := make(map[*ClientSession]bool)

	// Initial visibility sync
	forEachSession(func(other *ClientSession) {
		if other == session {
			return
		}
		if other.World.ID != session.World.ID {
			return
		}
		if isVisible(other.Position, session.Position) {
			sendMessage(other.Conn, ServerMessage{
				Command: "PLAYER_JOINED",
				Payload: map[string]interface{}{
					"name": session.Character.Name,
					"pos":  session.Position,
				},
			})
			sendMessage(session.Conn, ServerMessage{
				Command: "PLAYER_JOINED",
				Payload: map[string]interface{}{
					"name": other.Character.Name,
					"pos":  other.Position,
				},
			})
			visible[other] = true
		}
	})

	reader := bufio.NewScanner(conn)

	for session.Active && reader.Scan() {
		var msg ClientMessage
		if err := json.Unmarshal(reader.Bytes(), &msg); err != nil {
			continue
		}

		switch msg.Command {

		case "MOVE":
			data, _ := json.Marshal(msg.Payload)
			var move MoveRequest
			if err := json.Unmarshal(data, &move); err != nil {
				continue
			}

			newPos := Position{X: move.X, Y: move.Y, Z: move.Z}

			if !isMoveValid(session.Position, newPos) {
				sendMessage(conn, ServerMessage{
					Command: "MOVE_REJECTED",
					Payload: "INVALID_MOVE",
				})
				continue
			}

			session.Position = newPos

			sendMessage(conn, ServerMessage{
				Command: "MOVE_OK",
				Payload: session.Position,
			})

			forEachSession(func(other *ClientSession) {
				if other == session {
					return
				}
				if other.World.ID != session.World.ID {
					return
				}

				nowVisible := isVisible(other.Position, session.Position)
				wasVisible := visible[other]

				switch {
				case nowVisible && !wasVisible:
					sendMessage(other.Conn, ServerMessage{
						Command: "PLAYER_JOINED",
						Payload: map[string]interface{}{
							"name": session.Character.Name,
							"pos":  session.Position,
						},
					})
					sendMessage(session.Conn, ServerMessage{
						Command: "PLAYER_JOINED",
						Payload: map[string]interface{}{
							"name": other.Character.Name,
							"pos":  other.Position,
						},
					})
					visible[other] = true

				case !nowVisible && wasVisible:
					sendMessage(other.Conn, ServerMessage{
						Command: "PLAYER_LEFT",
						Payload: session.Character.Name,
					})
					sendMessage(session.Conn, ServerMessage{
						Command: "PLAYER_LEFT",
						Payload: other.Character.Name,
					})
					delete(visible, other)

				case nowVisible && wasVisible:
					sendMessage(other.Conn, ServerMessage{
						Command: "PLAYER_MOVED",
						Payload: map[string]interface{}{
							"name": session.Character.Name,
							"pos":  session.Position,
						},
					})
				}
			})
		}
	}

	// Disconnect cleanup
	for other := range visible {
		sendMessage(other.Conn, ServerMessage{
			Command: "PLAYER_LEFT",
			Payload: session.Character.Name,
		})
	}

	log.Printf("Client disconnected: %s", conn.RemoteAddr())
}

