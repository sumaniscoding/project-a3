package main

import (
	"sort"
	"strconv"
)

const (
	lootKindMaterial = "material"
	lootKindGear     = "gear"
)

const (
	maxCraftQty = 20
)

type MaterialDefinition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type LootEntry struct {
	Kind        string
	ItemID      string
	DropRateBPS int
	MinQty      int
	MaxQty      int
}

type CraftOutput struct {
	TemplateID string `json:"template_id"`
	Qty        int    `json:"qty"`
}

type RecipeDefinition struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	MinLevel int            `json:"min_level"`
	Inputs   map[string]int `json:"inputs"`
	Output   CraftOutput    `json:"output"`
}

var materialCatalog = map[string]MaterialDefinition{
	"wolf_pelt":      {ID: "wolf_pelt", Name: "Rift Wolf Pelt"},
	"bandit_scrap":   {ID: "bandit_scrap", Name: "Bandit Iron Scrap"},
	"shard_core":     {ID: "shard_core", Name: "Shard Core"},
	"mythic_essence": {ID: "mythic_essence", Name: "Mythic Essence"},
}

var gearTemplates = map[string]Item{
	"crafted_wolfhide_bow": {
		ID:      "crafted_wolfhide_bow",
		Name:    "Wolfhide Hunter Bow",
		Grade:   4,
		Rarity:  RarityRare,
		Slot:    SlotWeapon,
		Element: ElementNone,
	},
	"crafted_bandit_mail": {
		ID:      "crafted_bandit_mail",
		Name:    "Banditforged Mail",
		Grade:   4,
		Rarity:  RarityRare,
		Slot:    SlotArmor,
		Element: ElementNone,
	},
	"crafted_shard_blade": {
		ID:      "crafted_shard_blade",
		Name:    "Shardsteel Blade",
		Grade:   6,
		Rarity:  RarityEpic,
		Slot:    SlotWeapon,
		Element: ElementLightning,
	},
}

var lootTables = map[string][]LootEntry{
	"mob_wolf_01": {
		{Kind: lootKindMaterial, ItemID: "wolf_pelt", DropRateBPS: 10000, MinQty: 1, MaxQty: 2},
	},
	"mob_bandit_01": {
		{Kind: lootKindMaterial, ItemID: "bandit_scrap", DropRateBPS: 8500, MinQty: 1, MaxQty: 3},
	},
	"mob_shard_01": {
		{Kind: lootKindMaterial, ItemID: "shard_core", DropRateBPS: 7000, MinQty: 1, MaxQty: 2},
		{Kind: lootKindGear, ItemID: "crafted_shard_blade", DropRateBPS: 700, MinQty: 1, MaxQty: 1},
	},
	"mob_myth_01": {
		{Kind: lootKindMaterial, ItemID: "mythic_essence", DropRateBPS: 8000, MinQty: 1, MaxQty: 2},
	},
}

var recipeCatalog = map[string]RecipeDefinition{
	"wolfhide_bow": {
		ID:       "wolfhide_bow",
		Name:     "Wolfhide Hunter Bow",
		MinLevel: 20,
		Inputs: map[string]int{
			"wolf_pelt": 1,
		},
		Output: CraftOutput{TemplateID: "crafted_wolfhide_bow", Qty: 1},
	},
	"bandit_mail": {
		ID:       "bandit_mail",
		Name:     "Banditforged Mail",
		MinLevel: 20,
		Inputs: map[string]int{
			"bandit_scrap": 5,
		},
		Output: CraftOutput{TemplateID: "crafted_bandit_mail", Qty: 1},
	},
	"shard_blade": {
		ID:       "shard_blade",
		Name:     "Shardsteel Blade",
		MinLevel: 55,
		Inputs: map[string]int{
			"shard_core":   3,
			"bandit_scrap": 2,
		},
		Output: CraftOutput{TemplateID: "crafted_shard_blade", Qty: 1},
	},
}

func recipesPayload() map[string]interface{} {
	recipeIDs := make([]string, 0, len(recipeCatalog))
	for id := range recipeCatalog {
		recipeIDs = append(recipeIDs, id)
	}
	sort.Strings(recipeIDs)

	recipes := make([]map[string]interface{}, 0, len(recipeIDs))
	for _, id := range recipeIDs {
		r := recipeCatalog[id]
		recipes = append(recipes, map[string]interface{}{
			"id":        r.ID,
			"name":      r.Name,
			"min_level": r.MinLevel,
			"inputs":    r.Inputs,
			"output":    r.Output,
		})
	}

	materialIDs := make([]string, 0, len(materialCatalog))
	for id := range materialCatalog {
		materialIDs = append(materialIDs, id)
	}
	sort.Strings(materialIDs)
	materials := make([]MaterialDefinition, 0, len(materialIDs))
	for _, id := range materialIDs {
		materials = append(materials, materialCatalog[id])
	}

	return map[string]interface{}{
		"recipes":   recipes,
		"materials": materials,
	}
}

func craftItem(c *Character, recipeID string, qty int) (map[string]interface{}, bool, string) {
	if qty <= 0 || qty > maxCraftQty {
		return nil, false, "INVALID_QTY"
	}

	recipe, ok := recipeCatalog[recipeID]
	if !ok {
		return nil, false, "RECIPE_NOT_FOUND"
	}
	if c.Level < recipe.MinLevel {
		return nil, false, "LEVEL_TOO_LOW"
	}

	for materialID, need := range recipe.Inputs {
		required := need * qty
		if c.Materials[materialID] < required {
			return nil, false, "INSUFFICIENT_MATERIALS"
		}
	}

	consumed := map[string]int{}
	for materialID, need := range recipe.Inputs {
		total := need * qty
		c.Materials[materialID] -= total
		consumed[materialID] = total
	}

	crafted := make([]Item, 0, qty)
	for i := 0; i < qty; i++ {
		item, ok := buildItemFromTemplate(recipe.Output.TemplateID)
		if !ok {
			return nil, false, "RECIPE_OUTPUT_INVALID"
		}
		c.Inventory = append(c.Inventory, item)
		crafted = append(crafted, item)
	}

	return map[string]interface{}{
		"recipe_id": recipe.ID,
		"recipe":    recipe.Name,
		"qty":       qty,
		"consumed":  consumed,
		"crafted":   crafted,
		"materials": c.Materials,
		"inventory": len(c.Inventory),
	}, true, "OK"
}

func rollLootForMob(c *Character, mobID string) []map[string]interface{} {
	entries, ok := lootTables[mobID]
	if !ok {
		return nil
	}

	drops := make([]map[string]interface{}, 0)
	for _, entry := range entries {
		if !rollDrop(entry.DropRateBPS) {
			continue
		}

		qty := rolledQty(entry.MinQty, entry.MaxQty)
		switch entry.Kind {
		case lootKindMaterial:
			c.Materials[entry.ItemID] += qty
			drops = append(drops, map[string]interface{}{
				"kind":    lootKindMaterial,
				"item_id": entry.ItemID,
				"qty":     qty,
			})
		case lootKindGear:
			for i := 0; i < qty; i++ {
				item, ok := buildItemFromTemplate(entry.ItemID)
				if !ok {
					continue
				}
				c.Inventory = append(c.Inventory, item)
				drops = append(drops, map[string]interface{}{
					"kind":    lootKindGear,
					"item_id": entry.ItemID,
					"qty":     1,
					"item":    item,
				})
			}
		}
	}

	return drops
}

func rollDrop(bps int) bool {
	if bps <= 0 {
		return false
	}
	if bps >= 10_000 {
		return true
	}
	return randIntn(10_000) < bps
}

func rolledQty(minQty, maxQty int) int {
	if minQty < 1 {
		minQty = 1
	}
	if maxQty < minQty {
		maxQty = minQty
	}
	if minQty == maxQty {
		return minQty
	}
	return minQty + randIntn(maxQty-minQty+1)
}

func buildItemFromTemplate(templateID string) (Item, bool) {
	template, ok := gearTemplates[templateID]
	if !ok {
		return Item{}, false
	}
	item := template
	item.ID = templateID + "_" + strconv.Itoa(randIntn(1_000_000))
	return item, true
}
