package main

import "strings"

const (
	maxGearLevel      = 10
	maxPetLevel       = 100
	maxMercLevel      = 300
	storageStackLimit = 1000
)

func canonicalCharacterClass(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.ReplaceAll(normalized, "_", " ")
	normalized = strings.Join(strings.Fields(normalized), " ")

	switch normalized {
	case "archer":
		return "Archer"
	case "rogue":
		return "Archer"
	case "mage":
		return "Mage"
	case "warrior":
		return "Warrior"
	case "healing knight", "healingknight":
		return "Healing Knight"
	}
	return ""
}

func classBaseStats(class string) (int, int) {
	switch canonicalCharacterClass(class) {
	case "Warrior":
		return 14, 10
	case "Mage":
		return 8, 14
	case "Healing Knight":
		return 12, 12
	default:
		return 10, 14 // Archer default
	}
}

func classStarterStats(class string) (int, int) {
	switch canonicalCharacterClass(class) {
	case "Warrior":
		return 22, 16
	case "Mage":
		return 14, 24
	case "Healing Knight":
		return 20, 20
	default:
		return 18, 22 // Archer default
	}
}

func ensureItemDefaults(item *Item) {
	if item == nil {
		return
	}
	if item.GearLevel <= 0 {
		item.GearLevel = 1
	}
	if item.GearLevel > maxGearLevel {
		item.GearLevel = maxGearLevel
	}
	if item.MinSTR < 0 {
		item.MinSTR = 0
	}
	if item.MinDEX < 0 {
		item.MinDEX = 0
	}
}

func isKnownGearSlot(slot string) bool {
	switch slot {
	case SlotHelmet, SlotGloves, SlotBoots, SlotPants, SlotArmor, SlotNecklace, SlotRing, SlotWeapon, SlotShield:
		return true
	default:
		return false
	}
}

func canEquipItemForStats(str, dex int, class string, item *Item) (bool, string) {
	if item == nil {
		return false, "ITEM_NOT_FOUND"
	}
	if !isKnownGearSlot(item.Slot) {
		return false, "INVALID_SLOT"
	}
	if str < item.MinSTR || dex < item.MinDEX {
		return false, "INSUFFICIENT_STATS"
	}
	if item.Slot == SlotShield && canonicalCharacterClass(class) != "Healing Knight" {
		return false, "CLASS_SLOT_RESTRICTED"
	}
	return true, "OK"
}

func canEquipItem(c *Character, item *Item) (bool, string) {
	if c == nil || item == nil {
		return false, "ITEM_NOT_FOUND"
	}
	return canEquipItemForStats(c.Strength, c.Dexterity, c.Class, item)
}

func isItemEquippedByMerc(c *Character, itemID string) bool {
	if c == nil || strings.TrimSpace(itemID) == "" {
		return false
	}
	for _, equippedID := range c.Mercenary.Equipped {
		if equippedID == itemID {
			return true
		}
	}
	return false
}

func isItemEquippedByPlayer(c *Character, itemID string) bool {
	if c == nil || strings.TrimSpace(itemID) == "" {
		return false
	}
	for _, equippedID := range c.Equipped {
		if equippedID == itemID {
			return true
		}
	}
	return false
}

func equipPlayerItem(c *Character, itemID string) (map[string]interface{}, bool, string) {
	idx := findInventoryItemIndex(c, itemID)
	if idx < 0 {
		return nil, false, "ITEM_NOT_FOUND"
	}
	item := &c.Inventory[idx]
	ensureItemDefaults(item)
	if ok, reason := canEquipItem(c, item); !ok {
		return nil, false, reason
	}
	if isItemEquippedByMerc(c, item.ID) {
		return nil, false, "ITEM_ALREADY_EQUIPPED_BY_MERC"
	}
	if c.Equipped == nil {
		c.Equipped = map[string]string{}
	}
	c.Equipped[item.Slot] = item.ID
	return map[string]interface{}{
		"slot": item.Slot,
		"item": *item,
	}, true, "OK"
}

func findInventoryItemIndex(c *Character, itemID string) int {
	if c == nil || strings.TrimSpace(itemID) == "" {
		return -1
	}
	for i := range c.Inventory {
		if c.Inventory[i].ID == itemID {
			return i
		}
	}
	return -1
}

func upgradeTierForLevel(level int) (gemID string, successBPS int) {
	cfg := upgradeConfigForLevel(level)
	return cfg.GemID, cfg.SuccessBPS
}

type gearUpgradeConfig struct {
	GemID              string
	GemCost            int
	SuccessBPS         int
	DowngradeOnFailBPS int
}

func upgradeConfigForLevel(level int) gearUpgradeConfig {
	switch {
	case level < 5:
		switch level {
		case 1:
			return gearUpgradeConfig{GemID: "enhance_gem_t1", GemCost: 1, SuccessBPS: 9000}
		case 2:
			return gearUpgradeConfig{GemID: "enhance_gem_t1", GemCost: 1, SuccessBPS: 8500}
		case 3:
			return gearUpgradeConfig{GemID: "enhance_gem_t1", GemCost: 1, SuccessBPS: 8000}
		default:
			return gearUpgradeConfig{GemID: "enhance_gem_t1", GemCost: 1, SuccessBPS: 7500}
		}
	case level < 8:
		switch level {
		case 5:
			return gearUpgradeConfig{GemID: "enhance_gem_t2", GemCost: 2, SuccessBPS: 6200}
		case 6:
			return gearUpgradeConfig{GemID: "enhance_gem_t2", GemCost: 2, SuccessBPS: 5600}
		default:
			return gearUpgradeConfig{GemID: "enhance_gem_t2", GemCost: 3, SuccessBPS: 5000}
		}
	default:
		if level <= 8 {
			return gearUpgradeConfig{GemID: "enhance_gem_t3", GemCost: 3, SuccessBPS: 3800, DowngradeOnFailBPS: 2000}
		}
		return gearUpgradeConfig{GemID: "enhance_gem_t3", GemCost: 4, SuccessBPS: 3000, DowngradeOnFailBPS: 3500}
	}
}

func upgradeGear(c *Character, itemID string) (map[string]interface{}, bool, string) {
	idx := findInventoryItemIndex(c, itemID)
	if idx < 0 {
		return nil, false, "ITEM_NOT_FOUND"
	}
	item := &c.Inventory[idx]
	ensureItemDefaults(item)

	if item.GearLevel >= maxGearLevel {
		return nil, false, "MAX_GEAR_LEVEL"
	}

	if ok, reason := canEquipItem(c, item); !ok {
		return nil, false, reason
	}

	oldLevel := item.GearLevel
	cfg := upgradeConfigForLevel(item.GearLevel)
	if c.Materials[cfg.GemID] < cfg.GemCost {
		return nil, false, "INSUFFICIENT_GEMS"
	}
	c.Materials[cfg.GemID] -= cfg.GemCost

	success := rollDrop(cfg.SuccessBPS)
	failureEffect := "NONE"
	if success {
		item.GearLevel++
	} else if cfg.DowngradeOnFailBPS > 0 && item.GearLevel > 1 && rollDrop(cfg.DowngradeOnFailBPS) {
		item.GearLevel--
		failureEffect = "DOWNGRADED"
	}
	return map[string]interface{}{
		"item_id":        item.ID,
		"slot":           item.Slot,
		"old_gear_level": oldLevel,
		"new_gear_level": item.GearLevel,
		"success":        success,
		"gem":            cfg.GemID,
		"cost":           cfg.GemCost,
		"failure_effect": failureEffect,
		"materials":      c.Materials,
	}, true, "OK"
}

func equipMercItem(c *Character, itemID string) (map[string]interface{}, bool, string) {
	if c == nil {
		return nil, false, "MERC_NOT_RECRUITED"
	}
	if !c.Mercenary.Recruited {
		return nil, false, "MERC_NOT_RECRUITED"
	}
	idx := findInventoryItemIndex(c, itemID)
	if idx < 0 {
		return nil, false, "ITEM_NOT_FOUND"
	}
	item := &c.Inventory[idx]
	ensureItemDefaults(item)
	if ok, reason := canEquipItemForStats(c.Mercenary.Strength, c.Mercenary.Dexterity, c.Mercenary.Class, item); !ok {
		return nil, false, reason
	}
	if c.Mercenary.Equipped == nil {
		c.Mercenary.Equipped = map[string]string{}
	}
	if c.Equipped[item.Slot] == item.ID {
		return nil, false, "ITEM_ALREADY_EQUIPPED_BY_PLAYER"
	}
	c.Mercenary.Equipped[item.Slot] = item.ID
	return map[string]interface{}{
		"event":     "EQUIPPED",
		"item_id":   item.ID,
		"slot":      item.Slot,
		"mercenary": c.Mercenary,
	}, true, "OK"
}

func unequipMercItem(c *Character, slot string) (map[string]interface{}, bool, string) {
	if c == nil || !c.Mercenary.Recruited {
		return nil, false, "MERC_NOT_RECRUITED"
	}
	slot = strings.ToLower(strings.TrimSpace(slot))
	if slot == "" {
		return nil, false, "SLOT_REQUIRED"
	}
	if c.Mercenary.Equipped == nil {
		c.Mercenary.Equipped = map[string]string{}
	}
	itemID := c.Mercenary.Equipped[slot]
	if itemID == "" {
		return nil, false, "SLOT_EMPTY"
	}
	delete(c.Mercenary.Equipped, slot)
	return map[string]interface{}{
		"event":     "UNEQUIPPED",
		"item_id":   itemID,
		"slot":      slot,
		"mercenary": c.Mercenary,
	}, true, "OK"
}

func petXPForNextLevel(level int) int {
	return 60 + (level * 8)
}

func mercXPForNextLevel(level int) int {
	return 90 + (level * 10)
}

func syncMercStats(c *Character) {
	if c == nil {
		return
	}
	if c.Mercenary.Level < 1 {
		c.Mercenary.Level = 1
	}
	if c.Mercenary.Level > maxMercLevel {
		c.Mercenary.Level = maxMercLevel
	}
	// Merc progression is intentionally weaker: +1 stat every 2 levels.
	c.Mercenary.Strength = 1 + (c.Mercenary.Level / 2)
	c.Mercenary.Dexterity = 1 + (c.Mercenary.Level / 2)
}

func applyCompanionProgress(c *Character, xp int) {
	if c == nil || xp <= 0 {
		return
	}
	if c.Pet.Level < 1 {
		c.Pet.Level = 1
	}
	if c.Pet.Level > maxPetLevel {
		c.Pet.Level = maxPetLevel
	}
	if c.Pet.Level < c.Level && c.Pet.Level < maxPetLevel {
		c.Pet.Level++
	}
	petXP := xp / 2
	if petXP < 1 {
		petXP = 1
	}
	c.Pet.XP += petXP
	for c.Pet.Level < maxPetLevel && c.Pet.XP >= petXPForNextLevel(c.Pet.Level) {
		c.Pet.XP -= petXPForNextLevel(c.Pet.Level)
		c.Pet.Level++
	}

	if c.Mercenary.Recruited {
		c.Mercenary.XP += maxInt(1, xp/3)
		for c.Mercenary.Level < maxMercLevel && c.Mercenary.XP >= mercXPForNextLevel(c.Mercenary.Level) {
			c.Mercenary.XP -= mercXPForNextLevel(c.Mercenary.Level)
			c.Mercenary.Level++
		}
		syncMercStats(c)
	}
}

func feedPet(c *Character, qty int) (map[string]interface{}, bool, string) {
	if c == nil {
		return nil, false, "PET_REQUIRED"
	}
	if !c.Pet.Acquired {
		return nil, false, "PET_NOT_ACQUIRED"
	}
	if qty <= 0 || qty > 100 {
		return nil, false, "INVALID_QTY"
	}
	const petTreatID = "pet_treat"
	if c.Materials[petTreatID] < qty {
		return nil, false, "INSUFFICIENT_MATERIALS"
	}
	c.Materials[petTreatID] -= qty
	c.Pet.XP += qty * 40
	if c.Pet.Level < 1 {
		c.Pet.Level = 1
	}
	for c.Pet.Level < maxPetLevel && c.Pet.XP >= petXPForNextLevel(c.Pet.Level) {
		c.Pet.XP -= petXPForNextLevel(c.Pet.Level)
		c.Pet.Level++
	}
	return map[string]interface{}{
		"pet":       c.Pet,
		"qty":       qty,
		"materials": c.Materials,
	}, true, "OK"
}
