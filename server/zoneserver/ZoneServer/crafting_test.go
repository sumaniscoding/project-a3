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

func TestCraftGuardianShieldSuccess(t *testing.T) {
	c := MockCharacter()
	c.Level = 40
	c.Materials = map[string]int{"bandit_scrap": 5, "shard_core": 2}
	baseInventory := len(c.Inventory)

	withFixedRandIntn(11, func() {
		result, ok, reason := craftItem(c, "guardian_shield", 1)
		if !ok {
			t.Fatalf("expected success, reason=%s", reason)
		}
		if result["recipe_id"] != "guardian_shield" {
			t.Fatalf("unexpected recipe_id: %v", result["recipe_id"])
		}
	})

	if c.Materials["bandit_scrap"] != 2 || c.Materials["shard_core"] != 1 {
		t.Fatalf("unexpected remaining materials: %#v", c.Materials)
	}
	if len(c.Inventory) != baseInventory+1 {
		t.Fatalf("expected inventory +1, got %d -> %d", baseInventory, len(c.Inventory))
	}
	last := c.Inventory[len(c.Inventory)-1]
	if last.Slot != SlotShield {
		t.Fatalf("expected crafted shield slot, got %s", last.Slot)
	}
}

func TestCraftHunterHelmSuccess(t *testing.T) {
	c := MockCharacter()
	c.Level = 25
	c.Materials = map[string]int{"wolf_pelt": 5, "bandit_scrap": 2}
	baseInventory := len(c.Inventory)

	withFixedRandIntn(13, func() {
		result, ok, reason := craftItem(c, "hunter_helm", 1)
		if !ok {
			t.Fatalf("expected success, reason=%s", reason)
		}
		if result["recipe_id"] != "hunter_helm" {
			t.Fatalf("unexpected recipe_id: %v", result["recipe_id"])
		}
	})

	if c.Materials["wolf_pelt"] != 3 || c.Materials["bandit_scrap"] != 1 {
		t.Fatalf("unexpected remaining materials: %#v", c.Materials)
	}
	if len(c.Inventory) != baseInventory+1 {
		t.Fatalf("expected inventory +1, got %d -> %d", baseInventory, len(c.Inventory))
	}
	last := c.Inventory[len(c.Inventory)-1]
	if last.Slot != SlotHelmet {
		t.Fatalf("expected crafted helmet slot, got %s", last.Slot)
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

func TestCraftItemRejectsInvalidOutputWithoutConsumingMaterials(t *testing.T) {
	origCatalog := recipeCatalog
	recipeCatalog = map[string]RecipeDefinition{}
	for k, v := range origCatalog {
		recipeCatalog[k] = v
	}
	recipeCatalog["invalid_output"] = RecipeDefinition{
		ID:       "invalid_output",
		Name:     "Invalid Output Recipe",
		MinLevel: 1,
		Inputs: map[string]int{
			"wolf_pelt": 1,
		},
		Output: CraftOutput{
			TemplateID: "missing_template",
			Qty:        1,
		},
	}
	defer func() { recipeCatalog = origCatalog }()

	c := MockCharacter()
	c.Materials = map[string]int{"wolf_pelt": 3}
	baseInventory := len(c.Inventory)

	if _, ok, reason := craftItem(c, "invalid_output", 1); ok || reason != "RECIPE_OUTPUT_INVALID" {
		t.Fatalf("expected RECIPE_OUTPUT_INVALID, got ok=%v reason=%s", ok, reason)
	}
	if c.Materials["wolf_pelt"] != 3 {
		t.Fatalf("materials changed on invalid output: %d", c.Materials["wolf_pelt"])
	}
	if len(c.Inventory) != baseInventory {
		t.Fatalf("inventory changed on invalid output: %d -> %d", baseInventory, len(c.Inventory))
	}
}

func TestRecipeCatalogOutputsReferenceKnownTemplates(t *testing.T) {
	for id, recipe := range recipeCatalog {
		if recipe.Output.Qty != 1 {
			t.Fatalf("recipe %s has unexpected output qty %d", id, recipe.Output.Qty)
		}
		if _, ok := gearTemplates[recipe.Output.TemplateID]; !ok {
			t.Fatalf("recipe %s references unknown template %s", id, recipe.Output.TemplateID)
		}
	}
}

func TestRecipeCatalogCoversPrimaryGearSlots(t *testing.T) {
	required := map[string]bool{
		SlotWeapon:   false,
		SlotArmor:    false,
		SlotHelmet:   false,
		SlotGloves:   false,
		SlotBoots:    false,
		SlotPants:    false,
		SlotRing:     false,
		SlotNecklace: false,
		SlotShield:   false,
	}
	for _, recipe := range recipeCatalog {
		template, ok := gearTemplates[recipe.Output.TemplateID]
		if !ok {
			continue
		}
		if _, tracked := required[template.Slot]; tracked {
			required[template.Slot] = true
		}
	}
	for slot, found := range required {
		if !found {
			t.Fatalf("expected at least one recipe for slot %s", slot)
		}
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
