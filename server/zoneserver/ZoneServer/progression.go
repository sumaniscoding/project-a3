package main

import (
	"fmt"
	"math/rand"
	"strings"
)

var (
	randIntn    = rand.Intn
	randFloat64 = rand.Float64
)

func gainXP(c *Character, amount int) (leveled bool) {
	if amount <= 0 {
		return false
	}
	if c.XPDebt > 0 {
		pay := amount / 2
		if pay > c.XPDebt {
			pay = c.XPDebt
		}
		c.XPDebt -= pay
		amount -= pay
	}

	c.XP += amount
	leveled = false
	for {
		need := xpForNextLevel(c.Level)
		if c.XP < need {
			break
		}
		c.XP -= need
		c.Level++
		c.MaxHP += 12
		c.HP = c.MaxHP
		c.SkillPoints++
		leveled = true
	}
	return leveled
}

func xpForNextLevel(level int) int {
	return 100 + (level * 15)
}

func applyDeathPenalty(c *Character, at Position) {
	c.Corpse = &Position{X: at.X, Y: at.Y, Z: at.Z}
	debt := 25 + c.Level*3
	c.XPDebt += debt
	if c.HP > c.MaxHP/2 {
		c.HP = c.MaxHP / 2
	}
}

func recoverCorpse(c *Character) bool {
	if c.Corpse == nil {
		return false
	}
	c.Corpse = nil
	c.XPDebt /= 2
	return true
}

func calculateAttack(c *Character, targetLevel int) (damage int, died bool) {
	if targetLevel < 1 {
		targetLevel = 1
	}

	gearAtk := 0
	for _, item := range c.Inventory {
		if c.Equipped[item.Slot] != item.ID {
			continue
		}
		gearAtk += item.Grade * 2
		if item.Rarity == RarityEpic {
			gearAtk += 2
		}
		if item.Rarity == RarityUnique {
			gearAtk += 4
		}
	}

	bonusPct := 0
	if c.Pet.Summoned {
		bonusPct += 5
	}
	if c.Mercenary.Recruited {
		bonusPct += 8
	}

	base := (c.Level * 2) + 12 + gearAtk
	damage = base + randIntn(8)
	damage += damage * bonusPct / 100

	risk := targetLevel - c.Level
	if risk > 4 {
		chance := 0.15 + (float64(risk-4) * 0.06)
		if chance > 0.8 {
			chance = 0.8
		}
		if randFloat64() < chance {
			died = true
		}
	}
	return damage, died
}

func maybeLegendaryDrop(c *Character) *Item {
	// Extreme rarity for Grace/Soul.
	if randIntn(1500) != 0 {
		return nil
	}

	kind := "Grace"
	element := ElementLight
	if randIntn(2) == 1 {
		kind = "Soul"
		element = ElementDark
	}

	item := Item{
		ID:        fmt.Sprintf("legendary_%s_%d", strings.ToLower(kind), randIntn(1_000_000)),
		Name:      kind + " Relic",
		Grade:     10,
		Rarity:    RarityUnique,
		Slot:      SlotWeapon,
		Element:   element,
		Legendary: true,
	}
	c.Inventory = append(c.Inventory, item)
	return &item
}

func maxInt(a, b int) int {
	if a >= b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a <= b {
		return a
	}
	return b
}
