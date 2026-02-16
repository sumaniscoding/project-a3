package main

type WorldID int

const (
	World1 WorldID = 1 // Level 1–50 (Open)
	World2 WorldID = 2 // Level 51–100 (Locked by quest)
	World3 WorldID = 3 // Level 101+ Aura (Locked by quest)
)

type World struct {
	ID           WorldID
	Name         string
	MinLevel     int
	MaxLevel     int
	Unlocked     bool
	RequiresAura bool
}

func DefaultWorlds() map[WorldID]*World {
	return map[WorldID]*World{
		World1: {
			ID:           World1,
			Name:         "The Known World",
			MinLevel:     1,
			MaxLevel:     50,
			Unlocked:     true,
			RequiresAura: false,
		},
		World2: {
			ID:           World2,
			Name:         "The Shattered World",
			MinLevel:     51,
			MaxLevel:     100,
			Unlocked:     false,
			RequiresAura: false,
		},
		World3: {
			ID:           World3,
			Name:         "The Mythical World",
			MinLevel:     101,
			MaxLevel:     135,
			Unlocked:     false,
			RequiresAura: true,
		},
	}
}
