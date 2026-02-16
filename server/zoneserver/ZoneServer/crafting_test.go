package main

import "testing"

func withFixedRandIntn(v int, fn func()) {
	orig := randIntn
	randIntn = func(n int) int {
		if n <= 0 {
			return 0
		}
		if v >= n {
			return n - 1
		}
		return v
	}
	defer func() { randIntn = orig }()
	fn()
}

func TestRollLootForMobGuaranteedMaterial(t *testing.T) {
	c := MockCharacter()
	c.Materials = map[string]int{}

	withFixedRandIntn(0, func() {
		drops := rollLootForMob(c, "mob_wolf_01")
		if len(drops) == 0 {
			t.Fatalf("expected drops")
		}
		if c.Materials["wolf_pelt"] < 1 {
			t.Fatalf("expected wolf_pelt material increment, got %d", c.Materials["wolf_pelt"])
		}
	})
}

func TestRollLootForMobMiss(t *testing.T) {
	c := MockCharacter()
	c.Materials = map[string]int{}

	withFixedRandIntn(9999, func() {
		drops := rollLootForMob(c, "mob_bandit_01")
		if len(drops) != 0 {
			t.Fatalf("expected no drops on high roll, got %d", len(drops))
		}
		if c.Materials["bandit_scrap"] != 0 {
			t.Fatalf("unexpected bandit_scrap count: %d", c.Materials["bandit_scrap"])
		}
	})
}

func TestCraftItemSuccess(t *testing.T) {
	c := MockCharacter()
	c.Materials = map[string]int{"wolf_pelt": 2}
	baseInventory := len(c.Inventory)

	withFixedRandIntn(7, func() {
		result, ok, reason := craftItem(c, "wolfhide_bow", 1)
		if !ok {
			t.Fatalf("expected success, reason=%s", reason)
		}
		if result["recipe_id"] != "wolfhide_bow" {
			t.Fatalf("unexpected recipe_id: %v", result["recipe_id"])
		}
	})

	if c.Materials["wolf_pelt"] != 1 {
		t.Fatalf("expected 1 wolf_pelt left, got %d", c.Materials["wolf_pelt"])
	}
	if len(c.Inventory) != baseInventory+1 {
		t.Fatalf("expected inventory +1, got %d -> %d", baseInventory, len(c.Inventory))
	}
}

func TestCraftItemRejections(t *testing.T) {
	c := MockCharacter()
	c.Materials = map[string]int{}

	if _, ok, reason := craftItem(c, "unknown", 1); ok || reason != "RECIPE_NOT_FOUND" {
		t.Fatalf("expected RECIPE_NOT_FOUND, got ok=%v reason=%s", ok, reason)
	}
	if _, ok, reason := craftItem(c, "wolfhide_bow", 0); ok || reason != "INVALID_QTY" {
		t.Fatalf("expected INVALID_QTY, got ok=%v reason=%s", ok, reason)
	}
	if _, ok, reason := craftItem(c, "wolfhide_bow", 21); ok || reason != "INVALID_QTY" {
		t.Fatalf("expected INVALID_QTY for qty>max, got ok=%v reason=%s", ok, reason)
	}
	if _, ok, reason := craftItem(c, "wolfhide_bow", 1); ok || reason != "INSUFFICIENT_MATERIALS" {
		t.Fatalf("expected INSUFFICIENT_MATERIALS, got ok=%v reason=%s", ok, reason)
	}

	low := MockCharacter()
	low.Level = 10
	low.Materials = map[string]int{"wolf_pelt": 10}
	if _, ok, reason := craftItem(low, "wolfhide_bow", 1); ok || reason != "LEVEL_TOO_LOW" {
		t.Fatalf("expected LEVEL_TOO_LOW, got ok=%v reason=%s", ok, reason)
	}
}

func TestEnsureCharacterDefaultsInitializesMaterials(t *testing.T) {
	c := MockCharacter()
	c.Materials = nil
	ensureCharacterDefaults(c)
	if c.Materials == nil {
		t.Fatalf("materials should be initialized")
	}
}
