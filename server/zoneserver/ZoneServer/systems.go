package main

import (
	"sync"
)

type Element string

const (
	ElementNone      Element = "None"
	ElementFire      Element = "Fire"
	ElementIce       Element = "Ice"
	ElementLightning Element = "Lightning"
	ElementEarth     Element = "Earth"
	ElementLight     Element = "Light"
	ElementDark      Element = "Dark"
)

const (
	RarityCommon = "Common"
	RarityRare   = "Rare"
	RarityEpic   = "Epic"
	RarityUnique = "Unique"
)

const (
	SlotWeapon   = "weapon"
	SlotArmor    = "armor"
	SlotHelmet   = "helmet"
	SlotGloves   = "gloves"
	SlotBoots    = "boots"
	SlotPants    = "pants"
	SlotNecklace = "necklace"
	SlotRing     = "ring"
	SlotShield   = "shield"
)

type Item struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Grade     int     `json:"grade"`
	Rarity    string  `json:"rarity"`
	Slot      string  `json:"slot"`
	Element   Element `json:"element"`
	Legendary bool    `json:"legendary"`
	GearLevel int     `json:"gear_level"`
	MinSTR    int     `json:"min_str"`
	MinDEX    int     `json:"min_dex"`
}

type PetState struct {
	Name     string `json:"name"`
	Passive  string `json:"passive"`
	Summoned bool   `json:"summoned"`
	Acquired bool   `json:"acquired"`
	Level    int    `json:"level"`
	XP       int    `json:"xp"`
}

type MercenaryState struct {
	Class     string            `json:"class"`
	Level     int               `json:"level"`
	XP        int               `json:"xp"`
	Recruited bool              `json:"recruited"`
	Strength  int               `json:"strength"`
	Dexterity int               `json:"dexterity"`
	Equipped  map[string]string `json:"equipped"`
}

type StorageState struct {
	Materials map[string]int `json:"materials"`
	Items     []Item         `json:"items"`
}

type Quest struct {
	ID            string
	Name          string
	Hidden        bool
	NonRepeatable bool
	RequiredNPC   string
	MinTrust      int
	MinLevel      int
}

type QuestProgress struct {
	Accepted bool `json:"accepted"`
	Complete bool `json:"complete"`
}

type SkillDefinition struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	MaxRank     int    `json:"max_rank"`
	BaseBonus   int    `json:"base_bonus"`
	Description string `json:"description"`
}

type NPCEntity struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	WorldID  WorldID  `json:"world_id"`
	Position Position `json:"position"`
}

type MobEntity struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	WorldID    WorldID  `json:"world_id"`
	Level      int      `json:"level"`
	HP         int      `json:"hp"`
	MaxHP      int      `json:"max_hp"`
	Position   Position `json:"position"`
	RespawnSec int      `json:"respawn_sec"`
}

var quests = map[string]Quest{
	"unlock_world2_race": {
		ID:            "unlock_world2_race",
		Name:          "The Shattered Sigil Race",
		NonRepeatable: false,
		MinLevel:      45,
	},
	"unlock_world3_legend": {
		ID:            "unlock_world3_legend",
		Name:          "Aura of the Mythical Gate",
		NonRepeatable: false,
		MinLevel:      101,
	},
	"npc_oath_hidden": {
		ID:            "npc_oath_hidden",
		Name:          "Whisper Oath",
		Hidden:        true,
		NonRepeatable: true,
		RequiredNPC:   "Elder Rowan",
		MinTrust:      60,
		MinLevel:      30,
	},
	"grace_legacy": {
		ID:            "grace_legacy",
		Name:          "Legacy of Grace",
		NonRepeatable: true,
		MinLevel:      90,
	},
	"soul_legacy": {
		ID:            "soul_legacy",
		Name:          "Legacy of Soul",
		NonRepeatable: true,
		MinLevel:      90,
	},
}

var (
	worldUnlockHistory = map[WorldID]string{}
	historyMu          sync.RWMutex
	mobMu              sync.RWMutex
	worldMobs          map[WorldID]map[string]*MobEntity
)

var skillCatalog = map[string]map[string]SkillDefinition{
	"Archer": {
		"precise_shot": {ID: "precise_shot", Name: "Precise Shot", MaxRank: 5, BaseBonus: 7, Description: "Single-target precision boost"},
		"evasion_step": {ID: "evasion_step", Name: "Evasion Step", MaxRank: 3, BaseBonus: 3, Description: "Reduces death chance in risky fights"},
		"burst_arrow":  {ID: "burst_arrow", Name: "Burst Arrow", MaxRank: 5, BaseBonus: 10, Description: "High burst skill"},
	},
	"Warrior": {
		"cleave":      {ID: "cleave", Name: "Cleave", MaxRank: 5, BaseBonus: 9, Description: "Heavy melee sweep"},
		"iron_wall":   {ID: "iron_wall", Name: "Iron Wall", MaxRank: 3, BaseBonus: 3, Description: "Defensive stance"},
		"battle_rush": {ID: "battle_rush", Name: "Battle Rush", MaxRank: 5, BaseBonus: 8, Description: "Momentum attack"},
	},
	"Mage": {
		"arc_bolt":       {ID: "arc_bolt", Name: "Arc Bolt", MaxRank: 5, BaseBonus: 10, Description: "Elemental bolt"},
		"mana_barrier":   {ID: "mana_barrier", Name: "Mana Barrier", MaxRank: 3, BaseBonus: 3, Description: "Protective shield"},
		"cataclysm_nova": {ID: "cataclysm_nova", Name: "Cataclysm Nova", MaxRank: 5, BaseBonus: 12, Description: "Explosive spell"},
	},
	"Healing Knight": {
		"holy_slash":      {ID: "holy_slash", Name: "Holy Slash", MaxRank: 5, BaseBonus: 8, Description: "Hybrid strike"},
		"guardian_shield": {ID: "guardian_shield", Name: "Guardian Shield", MaxRank: 3, BaseBonus: 4, Description: "Shielded protection"},
		"renewal_burst":   {ID: "renewal_burst", Name: "Renewal Burst", MaxRank: 5, BaseBonus: 7, Description: "Healing pulse"},
	},
}

var worldNPCs = map[WorldID][]NPCEntity{
	World1: {
		{ID: "npc_elder_rowan", Name: "Elder Rowan", WorldID: World1, Position: Position{X: 108, Y: 0, Z: 106}},
		{ID: "npc_gear_smith", Name: "Gear Smith Halan", WorldID: World1, Position: Position{X: 96, Y: 0, Z: 99}},
		{ID: "npc_storage_keeper", Name: "Storage Keeper Lysa", WorldID: World1, Position: Position{X: 101, Y: 0, Z: 100}},
	},
	World2: {
		{ID: "npc_shattered_keeper", Name: "Shattered Keeper", WorldID: World2, Position: Position{X: 507, Y: 0, Z: 502}},
	},
	World3: {
		{ID: "npc_myth_warden", Name: "Myth Warden", WorldID: World3, Position: Position{X: 1007, Y: 0, Z: 1004}},
	},
}
