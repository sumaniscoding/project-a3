package main

import (
	"fmt"
	"strings"
)

func trustDelta(choice string) int {
	switch strings.ToLower(strings.TrimSpace(choice)) {
	case "honor", "help", "protect":
		return 15
	case "ignore":
		return -5
	default:
		return 5
	}
}

func applyQuestCompletion(c *Character, questID string) (map[string]interface{}, bool, string) {
	q, ok := quests[questID]
	if !ok {
		return nil, false, "QUEST_NOT_FOUND"
	}
	state, ok := c.Quests[questID]
	if !ok || !state.Accepted {
		return nil, false, "QUEST_NOT_ACCEPTED"
	}
	if state.Complete && q.NonRepeatable {
		return nil, false, "QUEST_NON_REPEATABLE"
	}
	if c.Level < q.MinLevel {
		return nil, false, "LEVEL_TOO_LOW"
	}
	if q.RequiredNPC != "" && c.Trust[q.RequiredNPC] < q.MinTrust {
		return nil, false, "NPC_TRUST_TOO_LOW"
	}

	reward := map[string]interface{}{"quest": q.Name}
	switch questID {
	case "unlock_world2_race":
		first := unlockWorld(World2, c.Name)
		c.UnlockedWorlds[World2] = true
		reward["world"] = World2
		reward["first_unlock"] = first
		if !first {
			alt := Item{
				ID:      fmt.Sprintf("shattered_medal_%d", randIntn(1_000_000)),
				Name:    "Shattered Champion Medal",
				Grade:   7,
				Rarity:  RarityEpic,
				Slot:    SlotArmor,
				Element: ElementEarth,
			}
			c.Inventory = append(c.Inventory, alt)
			reward["alternate_reward"] = alt
		}
	case "unlock_world3_legend":
		first := unlockWorld(World3, c.Name)
		c.UnlockedWorlds[World3] = true
		c.AuraLevel = 1
		reward["world"] = World3
		reward["first_unlock"] = first
	case "grace_legacy":
		it := Item{
			ID:        fmt.Sprintf("grace_quest_%d", randIntn(1_000_000)),
			Name:      "Grace Relic",
			Grade:     10,
			Rarity:    RarityUnique,
			Slot:      SlotWeapon,
			Element:   ElementLight,
			Legendary: true,
		}
		c.Inventory = append(c.Inventory, it)
		reward["item"] = it
	case "soul_legacy":
		it := Item{
			ID:        fmt.Sprintf("soul_quest_%d", randIntn(1_000_000)),
			Name:      "Soul Relic",
			Grade:     10,
			Rarity:    RarityUnique,
			Slot:      SlotWeapon,
			Element:   ElementDark,
			Legendary: true,
		}
		c.Inventory = append(c.Inventory, it)
		reward["item"] = it
	case "npc_oath_hidden":
		reward["storyline"] = "SECRET_ARCHIVE_UNLOCKED"
	}

	state.Complete = true
	c.Quests[questID] = state
	gainXP(c, 120)
	return reward, true, "OK"
}

func unlockWorld(worldID WorldID, player string) bool {
	historyMu.Lock()
	defer historyMu.Unlock()
	if worlds[worldID].Unlocked {
		return false
	}
	worlds[worldID].Unlocked = true
	worldUnlockHistory[worldID] = player
	return true
}

func getUnlockHistoryPayload() map[string]interface{} {
	historyMu.RLock()
	defer historyMu.RUnlock()
	return map[string]interface{}{
		"world_2_first_unlock": worldUnlockHistory[World2],
		"world_3_first_unlock": worldUnlockHistory[World3],
	}
}
