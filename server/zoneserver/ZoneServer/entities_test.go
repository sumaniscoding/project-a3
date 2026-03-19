package main

import "testing"

func withWorldMob(worldID WorldID, mobID string, mob *MobEntity, fn func()) {
	mobMu.Lock()
	orig := worldMobs
	worldMobs = map[WorldID]map[string]*MobEntity{
		worldID: {
			mobID: mob,
		},
	}
	mobMu.Unlock()
	defer func() {
		mobMu.Lock()
		worldMobs = orig
		mobMu.Unlock()
	}()
	fn()
}

func TestAttackMobDefeatReturnsDropsAndLegendary(t *testing.T) {
	resetSocialStateForTests()

	c := MockCharacter()
	c.Name = "LootTester"
	c.Materials = map[string]int{}
	s := &ClientSession{
		Character: c,
		World:     &World{ID: World1, Name: "Test World 1"},
		Position:  Position{X: 100, Y: 0, Z: 100},
	}

	withWorldMob(World1, "mob_wolf_01", &MobEntity{
		ID:         "mob_wolf_01",
		Name:       "Rift Wolf",
		WorldID:    World1,
		Level:      42,
		HP:         1,
		MaxHP:      1,
		Position:   Position{X: 100, Y: 0, Z: 100},
		RespawnSec: 99,
	}, func() {
		withFixedRandIntn(0, func() {
			result, ok, reason := attackMob(s, "mob_wolf_01", "burst_arrow")
			if !ok {
				t.Fatalf("attackMob failed: %s", reason)
			}

			if defeated, _ := result["defeated"].(bool); !defeated {
				t.Fatalf("expected defeated=true, got %#v", result["defeated"])
			}

			drops, ok := result["drops"].([]map[string]interface{})
			if !ok {
				t.Fatalf("expected drops slice, got %#v", result["drops"])
			}
			if len(drops) < 2 {
				t.Fatalf("expected material + legendary drops, got %d", len(drops))
			}
			if c.Materials["wolf_pelt"] < 1 {
				t.Fatalf("expected wolf_pelt materials increment, got %d", c.Materials["wolf_pelt"])
			}

			legendary, ok := result["legendary"].(*Item)
			if !ok || legendary == nil {
				t.Fatalf("expected legendary item pointer, got %#v", result["legendary"])
			}
			if !legendary.Legendary {
				t.Fatalf("expected legendary flag true")
			}
		})
	})
}

func TestAttackMobNonDefeatKeepsAdditiveDropFields(t *testing.T) {
	resetSocialStateForTests()

	c := MockCharacter()
	c.Name = "LootTester2"
	c.Materials = map[string]int{}
	s := &ClientSession{
		Character: c,
		World:     &World{ID: World1, Name: "Test World 1"},
		Position:  Position{X: 100, Y: 0, Z: 100},
	}

	withWorldMob(World1, "mob_wolf_01", &MobEntity{
		ID:         "mob_wolf_01",
		Name:       "Rift Wolf",
		WorldID:    World1,
		Level:      42,
		HP:         9999,
		MaxHP:      9999,
		Position:   Position{X: 100, Y: 0, Z: 100},
		RespawnSec: 99,
	}, func() {
		withFixedRandIntn(0, func() {
			result, ok, reason := attackMob(s, "mob_wolf_01", "burst_arrow")
			if !ok {
				t.Fatalf("attackMob failed: %s", reason)
			}

			if defeated, _ := result["defeated"].(bool); defeated {
				t.Fatalf("expected defeated=false for high HP mob")
			}

			drops, ok := result["drops"].([]map[string]interface{})
			if !ok {
				t.Fatalf("expected drops slice, got %#v", result["drops"])
			}
			if len(drops) != 0 {
				t.Fatalf("expected empty drops on non-defeat, got %d", len(drops))
			}
			if result["legendary"] != nil {
				t.Fatalf("expected nil legendary on non-defeat, got %#v", result["legendary"])
			}
		})
	})
}
