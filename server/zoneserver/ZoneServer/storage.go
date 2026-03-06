package main

import "strings"

func storageStackCount(c *Character) int {
	if c == nil {
		return 0
	}
	count := len(c.Storage.Items)
	for _, qty := range c.Storage.Materials {
		if qty > 0 {
			count++
		}
	}
	return count
}

func isStorageRestrictedItem(item Item) bool {
	switch item.Slot {
	case SlotWeapon, SlotArmor, SlotHelmet, SlotGloves, SlotBoots, SlotPants, SlotRing, SlotNecklace, SlotShield:
		return true
	default:
		return false
	}
}

func hasNearbyStorageNPC(session *ClientSession) bool {
	if session == nil || session.World == nil {
		return false
	}
	npcs := worldNPCs[session.World.ID]
	for _, npc := range npcs {
		if !strings.Contains(strings.ToLower(npc.Name), "storage") {
			continue
		}
		if isVisible(session.Position, npc.Position) {
			return true
		}
	}
	return false
}

func storageViewPayload(c *Character) map[string]interface{} {
	return map[string]interface{}{
		"capacity":    storageStackLimit,
		"used_stacks": storageStackCount(c),
		"gold":        c.Gold,
		"wallet_gold": c.WalletGold,
		"storage":     c.Storage,
	}
}

func storageDepositMaterial(c *Character, itemID string, qty int) (map[string]interface{}, bool, string) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, false, "ITEM_REQUIRED"
	}
	if qty <= 0 {
		return nil, false, "INVALID_QTY"
	}
	if c.Materials[itemID] < qty {
		return nil, false, "INSUFFICIENT_MATERIALS"
	}
	if c.Storage.Materials[itemID] == 0 && storageStackCount(c) >= storageStackLimit {
		return nil, false, "STORAGE_FULL"
	}
	c.Materials[itemID] -= qty
	c.Storage.Materials[itemID] += qty
	if c.Materials[itemID] <= 0 {
		delete(c.Materials, itemID)
	}
	return storageViewPayload(c), true, "OK"
}

func storageWithdrawMaterial(c *Character, itemID string, qty int) (map[string]interface{}, bool, string) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, false, "ITEM_REQUIRED"
	}
	if qty <= 0 {
		return nil, false, "INVALID_QTY"
	}
	if c.Storage.Materials[itemID] < qty {
		return nil, false, "INSUFFICIENT_STORAGE"
	}
	c.Storage.Materials[itemID] -= qty
	if c.Storage.Materials[itemID] <= 0 {
		delete(c.Storage.Materials, itemID)
	}
	c.Materials[itemID] += qty
	return storageViewPayload(c), true, "OK"
}

func storageDepositItem(c *Character, itemID string) (map[string]interface{}, bool, string) {
	idx := findInventoryItemIndex(c, itemID)
	if idx < 0 {
		return nil, false, "ITEM_NOT_FOUND"
	}
	if isItemEquippedByPlayer(c, itemID) || isItemEquippedByMerc(c, itemID) {
		return nil, false, "ITEM_EQUIPPED"
	}
	item := c.Inventory[idx]
	if isStorageRestrictedItem(item) {
		return nil, false, "ITEM_NOT_STORABLE"
	}
	if storageStackCount(c) >= storageStackLimit {
		return nil, false, "STORAGE_FULL"
	}
	c.Storage.Items = append(c.Storage.Items, item)
	c.Inventory = append(c.Inventory[:idx], c.Inventory[idx+1:]...)
	return storageViewPayload(c), true, "OK"
}

func storageWithdrawItem(c *Character, itemID string) (map[string]interface{}, bool, string) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return nil, false, "ITEM_REQUIRED"
	}
	idx := -1
	for i := range c.Storage.Items {
		if c.Storage.Items[i].ID == itemID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, false, "ITEM_NOT_FOUND"
	}
	item := c.Storage.Items[idx]
	c.Inventory = append(c.Inventory, item)
	c.Storage.Items = append(c.Storage.Items[:idx], c.Storage.Items[idx+1:]...)
	return storageViewPayload(c), true, "OK"
}

func storageDepositGold(c *Character, amount int) (map[string]interface{}, bool, string) {
	if amount <= 0 {
		return nil, false, "INVALID_AMOUNT"
	}
	if c.Gold < amount {
		return nil, false, "INSUFFICIENT_GOLD"
	}
	c.Gold -= amount
	c.WalletGold += amount
	return storageViewPayload(c), true, "OK"
}

func storageWithdrawGold(c *Character, amount int) (map[string]interface{}, bool, string) {
	if amount <= 0 {
		return nil, false, "INVALID_AMOUNT"
	}
	if c.WalletGold < amount {
		return nil, false, "INSUFFICIENT_WALLET_GOLD"
	}
	c.WalletGold -= amount
	c.Gold += amount
	return storageViewPayload(c), true, "OK"
}
