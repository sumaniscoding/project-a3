package main

type Character struct {
	ID             int
	Name           string
	Class          string
	Level          int
	AuraLevel      int
	WorldID        WorldID
	XP             int
	XPDebt         int
	HP             int
	MaxHP          int
	UnlockedWorlds map[WorldID]bool
	Trust          map[string]int
	Quests         map[string]*QuestProgress
	Inventory      []Item
	Equipped       map[string]string
	Pet            PetState
	Mercenary      MercenaryState
	Elemental      map[string]Element
	SkillPoints    int
	Skills         map[string]int
	PKScore        int
	Honor          int
	Corpse         *Position
}

// Temporary mock character for Epic 2
// (DB will replace this later)
func MockCharacter() *Character {
	return &Character{
		ID:        1,
		Name:      "TestArcher",
		Class:     "Archer",
		Level:     45,
		AuraLevel: 0,
		WorldID:   World1,
		XP:        0,
		XPDebt:    0,
		MaxHP:     120,
		HP:        120,
		UnlockedWorlds: map[WorldID]bool{
			World1: true,
		},
		Trust:    make(map[string]int),
		Quests:   make(map[string]*QuestProgress),
		Equipped: make(map[string]string),
		Pet: PetState{
			Name:     "Falcon",
			Passive:  "Critical Focus",
			Summoned: false,
		},
		Mercenary: MercenaryState{
			Class:     "",
			Level:     1,
			Recruited: false,
		},
		Elemental: map[string]Element{
			"weapon": ElementNone,
			"armor":  ElementNone,
			"pet":    ElementNone,
		},
		SkillPoints: 3,
		Skills: map[string]int{
			"precise_shot": 1,
			"evasion_step": 0,
			"burst_arrow":  0,
		},
		PKScore: 0,
		Honor:   0,
		Inventory: []Item{
			{
				ID:      "starter_bow",
				Name:    "Scout Bow",
				Grade:   2,
				Rarity:  RarityCommon,
				Slot:    SlotWeapon,
				Element: ElementNone,
			},
			{
				ID:      "starter_mail",
				Name:    "Pathfinder Mail",
				Grade:   2,
				Rarity:  RarityRare,
				Slot:    SlotArmor,
				Element: ElementNone,
			},
		},
	}
}
