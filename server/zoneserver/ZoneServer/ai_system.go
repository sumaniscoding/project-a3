package main

import (
	"math/rand"
	"math"
)

// processServerTick is called periodically by the main server loop.
// It handles entity AI, such as monster wandering and aggro.
func processServerTick() {
	if worldMobs == nil {
		return
	}

	sessions := getSessions()

	mobMu.Lock()
	defer mobMu.Unlock()

	for worldID, worldMap := range worldMobs {
		worldSessions := make([]*ClientSession, 0)
		for _, s := range sessions {
			if s.Authenticated && s.World != nil && s.World.ID == worldID && s.Character != nil && s.Character.HP > 0 {
				worldSessions = append(worldSessions, s)
			}
		}

		for _, mob := range worldMap {
			if mob.HP <= 0 {
				continue // Dead mobs don't move
			}

			var target *ClientSession
			minDistSq := 40000.0 // Aggro range squared (~200 units)
			
			for _, s := range worldSessions {
				dx := s.Position.X - mob.Position.X
				dz := s.Position.Z - mob.Position.Z
				distSq := dx*dx + dz*dz
				if distSq < minDistSq {
					minDistSq = distSq
					target = s
				}
			}

			if target != nil {
				// We have a target, either move towards it or attack
				if minDistSq <= 600.0 { // Attack range (~24 units)
					// Simple simulated damage logic based on mob level vs player level
					damage := mob.Level * 2
					if damage < 1 { damage = 1 }
					
					target.Character.HP -= damage
					
					if target.Character.HP <= 0 {
						applyDeathPenalty(target.Character, target.Position)
						sendMessage(target.Conn, ServerMessage{
							Command: RespPlayerDied, 
							Payload: map[string]interface{}{"target": mob.Name, "xp_debt": target.Character.XPDebt, "corpse": target.Character.Corpse, "recovery": "Use RECOVER_CORPSE"},
						})
					} else {
						sendMessage(target.Conn, ServerMessage{
							Command: RespPVPHit, 
							Payload: map[string]interface{}{"from": mob.Name, "damage": damage, "target_hp": target.Character.HP, "target_debt": target.Character.XPDebt},
						})
					}
				} else {
					// Move towards target
					dirX := target.Position.X - mob.Position.X
					dirZ := target.Position.Z - mob.Position.Z
					dist := math.Sqrt(minDistSq)
					if dist > 0 {
						// move speed is ~15 units per tick
						mob.Position.X += (dirX / dist) * 15.0
						mob.Position.Z += (dirZ / dist) * 15.0
					}
				}
			} else {
				// No targets, 15% chance to wander
				if rand.Float64() < 0.15 {
					mob.Position.X += float64(rand.Intn(21) - 10) // -10 to +10
					mob.Position.Z += float64(rand.Intn(21) - 10)
				}
			}
		}
	}
}
