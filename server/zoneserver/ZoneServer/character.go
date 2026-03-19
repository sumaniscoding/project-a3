package main

type Character struct {
	ID             int
	Name           string
	Class          string
	Strength       int
	Dexterity      int
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
	Presence       string
	Materials      map[string]int
	Storage        StorageState
	Gold           int
	WalletGold     int
	Friends        map[string]bool
	Blocks         map[string]bool
	Guild          string
	GuildRole      string
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
		Strength:  18,
		Dexterity: 22,
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
		Equipped: map[string]string{
			SlotWeapon: "starter_bow",
		},
		Pet: PetState{
			Name:     "Falcon",
			Passive:  "Critical Focus",
			Summoned: false,
			Acquired: false,
			Level:    1,
			XP:       0,
		},
		Mercenary: MercenaryState{
			Class:     "Warrior",
			Level:     1,
			XP:        0,
			Recruited: false,
			Strength:  1,
			Dexterity: 1,
			Equipped:  map[string]string{},
		},
		Elemental: map[string]Element{
			"weapon": ElementNone,
			"armor":  ElementNone,
			"pet":    ElementNone,
		},
		Presence:    "online",
		Materials:   map[string]int{},
		Storage:     StorageState{Materials: map[string]int{}, Items: []Item{}},
		Gold:        500,
		WalletGold:  0,
		Friends:     map[string]bool{},
		Blocks:      map[string]bool{},
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
				ID:        "starter_bow",
				Name:      "Scout Bow",
				Grade:     2,
				Rarity:    RarityCommon,
				Slot:      SlotWeapon,
				Element:   ElementNone,
				GearLevel: 1,
				MinSTR:    8,
				MinDEX:    12,
			},
			{
				ID:        "starter_mail",
				Name:      "Pathfinder Mail",
				Grade:     2,
				Rarity:    RarityRare,
				Slot:      SlotArmor,
				Element:   ElementNone,
				GearLevel: 1,
				MinSTR:    10,
				MinDEX:    8,
			},
		},
	}
}
