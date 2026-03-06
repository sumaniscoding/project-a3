package main

import (
	"fmt"
	"testing"
)

func TestCanEquipShieldClassRestriction(t *testing.T) {
	c := MockCharacter()
	shield := Item{
		ID:        "test_shield",
		Name:      "Training Shield",
		Slot:      SlotShield,
		GearLevel: 1,
	}

	if ok, reason := canEquipItem(c, &shield); ok || reason != "CLASS_SLOT_RESTRICTED" {
		t.Fatalf("expected CLASS_SLOT_RESTRICTED, got ok=%v reason=%s", ok, reason)
	}

	c.Class = "Healing Knight"
	ensureCharacterDefaults(c)
	if ok, reason := canEquipItem(c, &shield); !ok || reason != "OK" {
		t.Fatalf("expected shield equip success for Healing Knight, got ok=%v reason=%s", ok, reason)
	}
}

func TestCanEquipItemRequiresStats(t *testing.T) {
	c := MockCharacter()
	c.Strength = 5
	c.Dexterity = 5
	item := Item{
		ID:        "high_req",
		Slot:      SlotWeapon,
		GearLevel: 1,
		MinSTR:    15,
		MinDEX:    12,
	}
	if ok, reason := canEquipItem(c, &item); ok || reason != "INSUFFICIENT_STATS" {
		t.Fatalf("expected INSUFFICIENT_STATS, got ok=%v reason=%s", ok, reason)
	}
}

func TestUpgradeGearSuccessAndFailure(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)
	itemID := c.Inventory[0].ID
	c.Materials["enhance_gem_t1"] = 1

	withFixedRandIntn(0, func() {
		result, ok, reason := upgradeGear(c, itemID)
		if !ok || reason != "OK" {
			t.Fatalf("expected success, got ok=%v reason=%s", ok, reason)
		}
		if newLevel := toInt(toMap(result), "new_gear_level"); newLevel != 2 {
			t.Fatalf("expected new gear level 2, got %d", newLevel)
		}
	})
	if c.Materials["enhance_gem_t1"] != 0 {
		t.Fatalf("expected gem consumed on success")
	}

	c.Materials["enhance_gem_t1"] = 1
	withFixedRandIntn(9999, func() {
		result, ok, reason := upgradeGear(c, itemID)
		if !ok || reason != "OK" {
			t.Fatalf("expected handled failure result, got ok=%v reason=%s", ok, reason)
		}
		if success, _ := result["success"].(bool); success {
			t.Fatalf("expected failure roll")
		}
	})
	if c.Materials["enhance_gem_t1"] != 0 {
		t.Fatalf("expected gem consumed on failure")
	}
}

func TestUpgradeConfigScaling(t *testing.T) {
	tests := []struct {
		level        int
		gemID        string
		cost         int
		successBPS   int
		downgradeBPS int
	}{
		{level: 1, gemID: "enhance_gem_t1", cost: 1, successBPS: 9000, downgradeBPS: 0},
		{level: 4, gemID: "enhance_gem_t1", cost: 1, successBPS: 7500, downgradeBPS: 0},
		{level: 5, gemID: "enhance_gem_t2", cost: 2, successBPS: 6200, downgradeBPS: 0},
		{level: 7, gemID: "enhance_gem_t2", cost: 3, successBPS: 5000, downgradeBPS: 0},
		{level: 8, gemID: "enhance_gem_t3", cost: 3, successBPS: 3800, downgradeBPS: 2000},
		{level: 9, gemID: "enhance_gem_t3", cost: 4, successBPS: 3000, downgradeBPS: 3500},
	}

	for _, tc := range tests {
		cfg := upgradeConfigForLevel(tc.level)
		if cfg.GemID != tc.gemID || cfg.GemCost != tc.cost || cfg.SuccessBPS != tc.successBPS || cfg.DowngradeOnFailBPS != tc.downgradeBPS {
			t.Fatalf("unexpected config at level %d: %#v", tc.level, cfg)
		}
	}
}

func TestUpgradeGearFailCanDowngradeAtHighTier(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)
	itemID := c.Inventory[0].ID
	c.Inventory[0].GearLevel = 8
	c.Materials["enhance_gem_t3"] = 3

	orig := randIntn
	call := 0
	randIntn = func(n int) int {
		call++
		if call == 1 {
			return 9999 // fail success roll
		}
		return 0 // pass downgrade roll
	}
	defer func() { randIntn = orig }()

	result, ok, reason := upgradeGear(c, itemID)
	if !ok || reason != "OK" {
		t.Fatalf("expected handled high-tier failure, got ok=%v reason=%s", ok, reason)
	}
	if success, _ := result["success"].(bool); success {
		t.Fatalf("expected failure on first roll")
	}
	if effect, _ := result["failure_effect"].(string); effect != "DOWNGRADED" {
		t.Fatalf("expected DOWNGRADED failure effect, got %v", result["failure_effect"])
	}
	if lvl := toInt(toMap(result), "new_gear_level"); lvl != 7 {
		t.Fatalf("expected downgraded level 7, got %d", lvl)
	}
	if c.Materials["enhance_gem_t3"] != 0 {
		t.Fatalf("expected high-tier gem cost consumed, got %d", c.Materials["enhance_gem_t3"])
	}
}

func TestUpgradeGearRejectsWhenScaledGemCostMissing(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)
	c.Inventory[0].GearLevel = 9
	c.Materials["enhance_gem_t3"] = 3 // level 9 requires 4

	if _, ok, reason := upgradeGear(c, c.Inventory[0].ID); ok || reason != "INSUFFICIENT_GEMS" {
		t.Fatalf("expected INSUFFICIENT_GEMS at level 9 with 3 gems, got ok=%v reason=%s", ok, reason)
	}
}

func TestStorageRestrictionsAndCap(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)

	if _, ok, reason := storageDepositItem(c, c.Inventory[0].ID); ok || reason != "ITEM_NOT_STORABLE" {
		t.Fatalf("expected ITEM_NOT_STORABLE, got ok=%v reason=%s", ok, reason)
	}

	c.Materials["test_mat"] = 5
	for i := 0; i < storageStackLimit; i++ {
		c.Storage.Materials[fmt.Sprintf("stack_%d", i)] = 1
	}
	if _, ok, reason := storageDepositMaterial(c, "test_mat", 1); ok || reason != "STORAGE_FULL" {
		t.Fatalf("expected STORAGE_FULL, got ok=%v reason=%s", ok, reason)
	}
}

func TestCompanionProgressionCapsAndPetFeed(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)
	c.Pet.Acquired = true
	c.Pet.Level = 99
	c.Pet.XP = 0
	c.Mercenary.Recruited = true
	c.Mercenary.Level = 299
	c.Mercenary.XP = 0

	for i := 0; i < 200; i++ {
		applyCompanionProgress(c, 1000)
	}
	if c.Pet.Level > maxPetLevel {
		t.Fatalf("pet level exceeded cap: %d", c.Pet.Level)
	}
	if c.Mercenary.Level > maxMercLevel {
		t.Fatalf("merc level exceeded cap: %d", c.Mercenary.Level)
	}

	c.Materials["pet_treat"] = 3
	if _, ok, reason := feedPet(c, 2); !ok || reason != "OK" {
		t.Fatalf("expected feedPet success, got ok=%v reason=%s", ok, reason)
	}
	if c.Materials["pet_treat"] != 1 {
		t.Fatalf("unexpected pet_treat remaining: %d", c.Materials["pet_treat"])
	}
}

func TestFeedPetRequiresAcquiredPet(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)
	c.Pet.Acquired = false
	c.Materials["pet_treat"] = 1
	if _, ok, reason := feedPet(c, 1); ok || reason != "PET_NOT_ACQUIRED" {
		t.Fatalf("expected PET_NOT_ACQUIRED, got ok=%v reason=%s", ok, reason)
	}
}

func TestMercEquipAndUnequip(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)
	c.Mercenary.Recruited = true
	c.Mercenary.Class = "Warrior"
	c.Mercenary.Level = 50
	syncMercStats(c)
	c.Mercenary.Equipped = map[string]string{}

	result, ok, reason := equipMercItem(c, "starter_bow")
	if !ok || reason != "OK" {
		t.Fatalf("expected merc equip success, got ok=%v reason=%s", ok, reason)
	}
	if toString(toMap(result), "slot") != SlotWeapon {
		t.Fatalf("expected slot weapon, got %#v", result)
	}
	if c.Mercenary.Equipped[SlotWeapon] != "starter_bow" {
		t.Fatalf("expected merc equipped starter_bow, got %q", c.Mercenary.Equipped[SlotWeapon])
	}

	result, ok, reason = unequipMercItem(c, SlotWeapon)
	if !ok || reason != "OK" {
		t.Fatalf("expected merc unequip success, got ok=%v reason=%s", ok, reason)
	}
	if toString(toMap(result), "event") != "UNEQUIPPED" {
		t.Fatalf("unexpected unequip payload: %#v", result)
	}
	if c.Mercenary.Equipped[SlotWeapon] != "" {
		t.Fatalf("expected empty merc weapon slot")
	}
}

func TestMercEquipShieldClassRestriction(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)
	c.Mercenary.Recruited = true
	c.Mercenary.Class = "Warrior"
	c.Mercenary.Level = 50
	syncMercStats(c)
	c.Mercenary.Equipped = map[string]string{}

	c.Inventory = append(c.Inventory, Item{
		ID:        "test_shield_item",
		Name:      "Merc Shield",
		Slot:      SlotShield,
		GearLevel: 1,
		MinSTR:    5,
		MinDEX:    5,
	})
	if _, ok, reason := equipMercItem(c, "test_shield_item"); ok || reason != "CLASS_SLOT_RESTRICTED" {
		t.Fatalf("expected CLASS_SLOT_RESTRICTED, got ok=%v reason=%s", ok, reason)
	}

	c.Mercenary.Class = "Healing Knight"
	if _, ok, reason := equipMercItem(c, "test_shield_item"); !ok || reason != "OK" {
		t.Fatalf("expected shield equip success for Healing Knight merc, got ok=%v reason=%s", ok, reason)
	}
}

func TestMercProgressionCapsAt300AndSyncsStats(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)
	c.Mercenary.Recruited = true
	c.Mercenary.Level = 299
	c.Mercenary.XP = 0
	syncMercStats(c)

	applyCompanionProgress(c, 10000)

	if c.Mercenary.Level != maxMercLevel {
		t.Fatalf("expected merc level %d, got %d", maxMercLevel, c.Mercenary.Level)
	}
	if c.Mercenary.Strength != 151 || c.Mercenary.Dexterity != 151 {
		t.Fatalf("unexpected merc stats at cap: str=%d dex=%d", c.Mercenary.Strength, c.Mercenary.Dexterity)
	}
}

func TestMercEquipRespectsStatParityWithPlayerRules(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)
	c.Mercenary.Recruited = true
	c.Mercenary.Class = "Warrior"
	c.Mercenary.Level = 1
	syncMercStats(c)
	c.Mercenary.Equipped = map[string]string{}

	c.Inventory = append(c.Inventory, Item{
		ID:        "merc_high_req_ring",
		Name:      "Merc High Req Ring",
		Slot:      SlotRing,
		GearLevel: 1,
		MinSTR:    120,
		MinDEX:    120,
	})

	if _, ok, reason := equipMercItem(c, "merc_high_req_ring"); ok || reason != "INSUFFICIENT_STATS" {
		t.Fatalf("expected INSUFFICIENT_STATS at low merc level, got ok=%v reason=%s", ok, reason)
	}

	c.Mercenary.Level = maxMercLevel
	syncMercStats(c)
	if _, ok, reason := equipMercItem(c, "merc_high_req_ring"); !ok || reason != "OK" {
		t.Fatalf("expected merc equip success at cap stats, got ok=%v reason=%s", ok, reason)
	}
	if c.Mercenary.Equipped[SlotRing] != "merc_high_req_ring" {
		t.Fatalf("expected merc ring equipped, got %q", c.Mercenary.Equipped[SlotRing])
	}
}

func TestPlayerEquipRejectedWhenMercHasItem(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)
	c.Mercenary.Recruited = true
	c.Mercenary.Class = "Warrior"
	c.Mercenary.Level = 50
	c.Mercenary.Equipped = map[string]string{}
	syncMercStats(c)

	if _, ok, reason := equipMercItem(c, "starter_bow"); !ok || reason != "OK" {
		t.Fatalf("expected merc equip success, got ok=%v reason=%s", ok, reason)
	}
	if _, ok, reason := equipPlayerItem(c, "starter_bow"); ok || reason != "ITEM_ALREADY_EQUIPPED_BY_MERC" {
		t.Fatalf("expected ITEM_ALREADY_EQUIPPED_BY_MERC, got ok=%v reason=%s", ok, reason)
	}
	if _, ok, reason := unequipMercItem(c, SlotWeapon); !ok || reason != "OK" {
		t.Fatalf("expected merc unequip success, got ok=%v reason=%s", ok, reason)
	}
	if _, ok, reason := equipPlayerItem(c, "starter_bow"); !ok || reason != "OK" {
		t.Fatalf("expected player equip success after merc unequip, got ok=%v reason=%s", ok, reason)
	}
}

func TestStorageDepositRejectsWhenPlayerEquipped(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)
	c.Inventory = append(c.Inventory, Item{
		ID:        "trinket_alpha",
		Name:      "Trinket Alpha",
		Slot:      "misc",
		GearLevel: 1,
	})
	c.Equipped["misc"] = "trinket_alpha"

	if _, ok, reason := storageDepositItem(c, "trinket_alpha"); ok || reason != "ITEM_EQUIPPED" {
		t.Fatalf("expected ITEM_EQUIPPED, got ok=%v reason=%s", ok, reason)
	}
}

func TestStorageDepositRejectsWhenMercEquipped(t *testing.T) {
	c := MockCharacter()
	ensureCharacterDefaults(c)
	c.Mercenary.Recruited = true
	c.Mercenary.Equipped = map[string]string{"misc": "trinket_beta"}
	c.Inventory = append(c.Inventory, Item{
		ID:        "trinket_beta",
		Name:      "Trinket Beta",
		Slot:      "misc",
		GearLevel: 1,
	})

	if _, ok, reason := storageDepositItem(c, "trinket_beta"); ok || reason != "ITEM_EQUIPPED" {
		t.Fatalf("expected ITEM_EQUIPPED, got ok=%v reason=%s", ok, reason)
	}
}
