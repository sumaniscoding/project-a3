package main

import "time"

func initWorldEntities() {
	mobMu.Lock()
	defer mobMu.Unlock()
	if worldMobs != nil {
		return
	}

	worldMobs = map[WorldID]map[string]*MobEntity{
		World1: {
			"mob_wolf_01":   {ID: "mob_wolf_01", Name: "Rift Wolf", WorldID: World1, Level: 42, HP: 210, MaxHP: 210, Position: Position{X: 112, Y: 0, Z: 108}, RespawnSec: 8},
			"mob_bandit_01": {ID: "mob_bandit_01", Name: "Dust Bandit", WorldID: World1, Level: 46, HP: 245, MaxHP: 245, Position: Position{X: 118, Y: 0, Z: 110}, RespawnSec: 9},
		},
		World2: {
			"mob_shard_01": {ID: "mob_shard_01", Name: "Shard Revenant", WorldID: World2, Level: 62, HP: 360, MaxHP: 360, Position: Position{X: 510, Y: 0, Z: 507}, RespawnSec: 10},
		},
		World3: {
			"mob_myth_01": {ID: "mob_myth_01", Name: "Mythic Devourer", WorldID: World3, Level: 112, HP: 680, MaxHP: 680, Position: Position{X: 1012, Y: 0, Z: 1009}, RespawnSec: 12},
		},
	}
}

func listNearbyEntities(s *ClientSession) map[string]interface{} {
	initWorldEntities()

	npcs := make([]NPCEntity, 0)
	for _, npc := range worldNPCs[s.World.ID] {
		if isVisible(s.Position, npc.Position) {
			npcs = append(npcs, npc)
		}
	}

	mobMu.RLock()
	defer mobMu.RUnlock()
	mobs := make([]MobEntity, 0)
	for _, mob := range worldMobs[s.World.ID] {
		if mob.HP <= 0 {
			continue
		}
		if isVisible(s.Position, mob.Position) {
			mobs = append(mobs, *mob)
		}
	}

	return map[string]interface{}{
		"world": s.World.Name,
		"npcs":  npcs,
		"mobs":  mobs,
	}
}

func attackMob(s *ClientSession, mobID, skillID string) (map[string]interface{}, bool, string) {
	initWorldEntities()

	mobMu.Lock()
	defer mobMu.Unlock()

	worldMap := worldMobs[s.World.ID]
	mob, ok := worldMap[mobID]
	if !ok {
		return nil, false, "MOB_NOT_FOUND"
	}
	if !isVisible(s.Position, mob.Position) {
		return nil, false, "MOB_OUT_OF_RANGE"
	}
	if mob.HP <= 0 {
		return nil, false, "MOB_ALREADY_DEFEATED"
	}

	damage, died := calculateAttack(s.Character, mob.Level)
	damage += skillBonus(s.Character, skillID)
	mob.HP -= damage

	if died {
		applyDeathPenalty(s.Character, s.Position)
		return map[string]interface{}{
			"mob":      mob.Name,
			"status":   "PLAYER_DIED",
			"xp_debt":  s.Character.XPDebt,
			"corpse":   s.Character.Corpse,
			"skill_id": skillID,
		}, true, "OK"
	}

	result := map[string]interface{}{
		"mob_id":    mob.ID,
		"mob":       mob.Name,
		"mob_hp":    maxInt(mob.HP, 0),
		"damage":    damage,
		"skill_id":  skillID,
		"defeated":  false,
		"xp_gain":   0,
		"legendary": nil,
		"drops":     []map[string]interface{}{},
	}

	if mob.HP <= 0 {
		xpGain := 35 + mob.Level*4
		leveled := gainXP(s.Character, xpGain)
		drops := rollLootForMob(s.Character, mob.ID)
		drop := maybeLegendaryDrop(s.Character)
		if drop != nil {
			drops = append(drops, map[string]interface{}{
				"kind":    lootKindGear,
				"item_id": drop.ID,
				"qty":     1,
				"item":    *drop,
			})
		}
		result["defeated"] = true
		result["xp_gain"] = xpGain
		result["leveled_up"] = leveled
		result["drops"] = drops
		result["legendary"] = drop

		respawnAfter := time.Duration(mob.RespawnSec) * time.Second
		mob.HP = 0
		go respawnMob(s.World.ID, mob.ID, respawnAfter)
	}

	return result, true, "OK"
}

func respawnMob(worldID WorldID, mobID string, wait time.Duration) {
	time.Sleep(wait)
	mobMu.Lock()
	defer mobMu.Unlock()
	worldMap, ok := worldMobs[worldID]
	if !ok {
		return
	}
	mob, ok := worldMap[mobID]
	if !ok {
		return
	}
	mob.HP = mob.MaxHP
	mob.Position.X += float64(randIntn(5) - 2)
	mob.Position.Z += float64(randIntn(5) - 2)
}
