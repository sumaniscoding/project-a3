package main

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestActivePersistenceMode(t *testing.T) {
	t.Cleanup(resetPersistenceRuntimeStateForTests)

	tests := []struct {
		name string
		env  string
		want string
	}{
		{name: "default", env: "", want: persistenceDB},
		{name: "sqlite alias", env: "sqlite", want: persistenceDB},
		{name: "hybrid", env: "hybrid", want: persistenceHybrid},
		{name: "json", env: "json", want: persistenceJSON},
		{name: "legacy alias", env: "legacy", want: persistenceJSON},
		{name: "unknown defaults db", env: "bogus", want: persistenceDB},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetPersistenceRuntimeStateForTests()
			if tc.env == "" {
				_ = os.Unsetenv("A3_PERSISTENCE_MODE")
			} else {
				_ = os.Setenv("A3_PERSISTENCE_MODE", tc.env)
			}
			if got := activePersistenceMode(); got != tc.want {
				t.Fatalf("mode=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestActivePersistenceDBBackend(t *testing.T) {
	t.Cleanup(resetPersistenceRuntimeStateForTests)

	tests := []struct {
		name        string
		backendEnv  string
		databaseURL string
		want        string
	}{
		{name: "default sqlite", want: persistenceBackendSQLite},
		{name: "database url implies postgres", databaseURL: "postgres://example", want: persistenceBackendPostgres},
		{name: "explicit postgres", backendEnv: "postgres", want: persistenceBackendPostgres},
		{name: "postgres alias", backendEnv: "postgresql", want: persistenceBackendPostgres},
		{name: "explicit sqlite", backendEnv: "sqlite", databaseURL: "postgres://example", want: persistenceBackendSQLite},
		{name: "unknown defaults sqlite", backendEnv: "bogus", want: persistenceBackendSQLite},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetPersistenceRuntimeStateForTests()
			if tc.backendEnv == "" {
				_ = os.Unsetenv("A3_DB_BACKEND")
			} else {
				_ = os.Setenv("A3_DB_BACKEND", tc.backendEnv)
			}
			if tc.databaseURL == "" {
				_ = os.Unsetenv("A3_DATABASE_URL")
			} else {
				_ = os.Setenv("A3_DATABASE_URL", tc.databaseURL)
			}
			if got := activePersistenceDBBackend(); got != tc.want {
				t.Fatalf("backend=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestDBModeMigratesLegacyCharacter(t *testing.T) {
	t.Cleanup(resetPersistenceRuntimeStateForTests)
	resetPersistenceRuntimeStateForTests()
	_ = os.Setenv("A3_PERSISTENCE_MODE", "db")

	restoreWD := enterTempDir(t)
	defer restoreWD()

	legacy := MockCharacter()
	legacy.Name = "LegacyHero"
	legacy.Level = 52
	legacy.WorldID = World2
	ensureCharacterDefaults(legacy)

	if err := os.MkdirAll(characterStoreDir, 0o755); err != nil {
		t.Fatalf("mkdir legacy dir: %v", err)
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy: %v", err)
	}
	legacyPath := filepath.Join(characterStoreDir, sanitizeCharacterName(legacy.Name)+".json")
	if err := os.WriteFile(legacyPath, raw, 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	got, err := loadCharacter("LegacyHero", "")
	if err != nil {
		t.Fatalf("loadCharacter: %v", err)
	}
	if got.Level != 52 {
		t.Fatalf("level=%d want 52", got.Level)
	}
	if got.WorldID != World2 {
		t.Fatalf("world=%d want %d", got.WorldID, World2)
	}

	if _, err := os.Stat(characterDBPath); err != nil {
		t.Fatalf("db file missing: %v", err)
	}

	db, err := sql.Open("sqlite", characterDBPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM characters WHERE name=?`, sanitizeCharacterName(legacy.Name)).Scan(&count); err != nil {
		t.Fatalf("query count: %v", err)
	}
	if count != 1 {
		t.Fatalf("count=%d want 1", count)
	}
}

func TestJSONModeWritesLegacyFile(t *testing.T) {
	t.Cleanup(resetPersistenceRuntimeStateForTests)
	resetPersistenceRuntimeStateForTests()
	_ = os.Setenv("A3_PERSISTENCE_MODE", "json")

	restoreWD := enterTempDir(t)
	defer restoreWD()

	c := MockCharacter()
	c.Name = "JsonHero"
	if err := persistCharacter(c); err != nil {
		t.Fatalf("persistCharacter: %v", err)
	}

	path := filepath.Join(characterStoreDir, sanitizeCharacterName(c.Name)+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("json file missing: %v", err)
	}
	if _, err := os.Stat(characterDBPath); err == nil {
		t.Fatalf("unexpected db file in json mode")
	}
}

func TestDBModePersistsCraftingState(t *testing.T) {
	t.Cleanup(resetPersistenceRuntimeStateForTests)
	resetPersistenceRuntimeStateForTests()
	_ = os.Setenv("A3_PERSISTENCE_MODE", "db")

	restoreWD := enterTempDir(t)
	defer restoreWD()

	c, err := loadCharacter("CraftHero", "Archer")
	if err != nil {
		t.Fatalf("loadCharacter: %v", err)
	}
	c.Materials["wolf_pelt"] = 2

	origRand := randIntn
	randIntn = func(n int) int { return 1 }
	defer func() { randIntn = origRand }()

	if _, ok, reason := craftItem(c, "wolfhide_bow", 1); !ok {
		t.Fatalf("craftItem failed: %s", reason)
	}
	if err := persistCharacter(c); err != nil {
		t.Fatalf("persistCharacter: %v", err)
	}

	reloaded, err := loadCharacter("CraftHero", "Archer")
	if err != nil {
		t.Fatalf("reload loadCharacter: %v", err)
	}
	if reloaded.Materials["wolf_pelt"] != 1 {
		t.Fatalf("expected persisted material count 1, got %d", reloaded.Materials["wolf_pelt"])
	}

	foundCrafted := false
	for _, item := range reloaded.Inventory {
		if len(item.ID) >= len("crafted_wolfhide_bow_") && item.ID[:len("crafted_wolfhide_bow_")] == "crafted_wolfhide_bow_" {
			foundCrafted = true
			break
		}
	}
	if !foundCrafted {
		t.Fatalf("expected crafted wolfhide bow item in persisted inventory")
	}
}

func TestDBModePersistsAccountState(t *testing.T) {
	t.Cleanup(resetPersistenceRuntimeStateForTests)
	resetPersistenceRuntimeStateForTests()
	_ = os.Setenv("A3_PERSISTENCE_MODE", "db")

	restoreWD := enterTempDir(t)
	defer restoreWD()

	a, err := loadAccount("AccountHero")
	if err != nil {
		t.Fatalf("loadAccount: %v", err)
	}
	a.WalletGold = 321
	a.Storage.Materials["wolf_pelt"] = 7
	a.Storage.Items = append(a.Storage.Items, Item{
		ID:        "misc_token_1",
		Name:      "Misc Token",
		Slot:      "misc",
		GearLevel: 1,
	})
	if err := persistAccount(a); err != nil {
		t.Fatalf("persistAccount: %v", err)
	}

	reloaded, err := loadAccount("AccountHero")
	if err != nil {
		t.Fatalf("reload loadAccount: %v", err)
	}
	if reloaded.WalletGold != 321 {
		t.Fatalf("expected wallet_gold=321, got %d", reloaded.WalletGold)
	}
	if reloaded.Storage.Materials["wolf_pelt"] != 7 {
		t.Fatalf("expected stored wolf_pelt=7, got %d", reloaded.Storage.Materials["wolf_pelt"])
	}
	if len(reloaded.Storage.Items) != 1 || reloaded.Storage.Items[0].ID != "misc_token_1" {
		t.Fatalf("expected one stored item, got %#v", reloaded.Storage.Items)
	}
}

func TestJSONModeWritesLegacyAccountFile(t *testing.T) {
	t.Cleanup(resetPersistenceRuntimeStateForTests)
	resetPersistenceRuntimeStateForTests()
	_ = os.Setenv("A3_PERSISTENCE_MODE", "json")

	restoreWD := enterTempDir(t)
	defer restoreWD()

	a := newDefaultAccount("JsonAccount")
	a.WalletGold = 99
	if err := persistAccount(a); err != nil {
		t.Fatalf("persistAccount: %v", err)
	}

	path := filepath.Join(accountStoreDir, sanitizeCharacterName(a.Username)+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("account json file missing: %v", err)
	}
}

func TestEnsureCharacterDefaultsInitializesFriends(t *testing.T) {
	c := &Character{Name: "Friendless", Level: 1}
	ensureCharacterDefaults(c)
	if c.Friends == nil {
		t.Fatalf("expected friends map initialized")
	}
	if c.Blocks == nil {
		t.Fatalf("expected blocks map initialized")
	}
	if c.Presence != "online" {
		t.Fatalf("expected default presence online, got %q", c.Presence)
	}
	c.Presence = "unknown_status"
	ensureCharacterDefaults(c)
	if c.Presence != "online" {
		t.Fatalf("expected invalid presence coerced to online, got %q", c.Presence)
	}
}

func TestNewDefaultHealingKnightHasStarterShield(t *testing.T) {
	c := newDefaultCharacter("ShieldHero", "Healing Knight")
	foundShield := false
	for _, item := range c.Inventory {
		if item.Slot == SlotShield && item.ID == "starter_shield" {
			foundShield = true
			break
		}
	}
	if !foundShield {
		t.Fatalf("expected starter_shield in Healing Knight loadout")
	}
}

func TestNewDefaultCharacterClassLoadoutsAndStarterSkills(t *testing.T) {
	tests := []struct {
		class        string
		weaponID     string
		armorID      string
		primarySkill string
		expectShield bool
	}{
		{
			class:        "Archer",
			weaponID:     "starter_bow",
			armorID:      "starter_mail",
			primarySkill: "precise_shot",
			expectShield: false,
		},
		{
			class:        "Mage",
			weaponID:     "starter_focus",
			armorID:      "starter_robe",
			primarySkill: "arc_bolt",
			expectShield: false,
		},
		{
			class:        "Warrior",
			weaponID:     "starter_blade",
			armorID:      "starter_plate",
			primarySkill: "cleave",
			expectShield: false,
		},
		{
			class:        "Healing Knight",
			weaponID:     "starter_mace",
			armorID:      "starter_guard_mail",
			primarySkill: "holy_slash",
			expectShield: true,
		},
	}

	for _, tc := range tests {
		c := newDefaultCharacter("ClassHero_"+tc.class, tc.class)
		if c.Class != tc.class {
			t.Fatalf("expected class %s, got %s", tc.class, c.Class)
		}
		if !inventoryHasItem(c.Inventory, tc.weaponID) {
			t.Fatalf("expected %s starter weapon in inventory", tc.weaponID)
		}
		if !inventoryHasItem(c.Inventory, tc.armorID) {
			t.Fatalf("expected %s starter armor in inventory", tc.armorID)
		}
		foundShield := inventoryHasItem(c.Inventory, "starter_shield")
		if foundShield != tc.expectShield {
			t.Fatalf("shield presence mismatch for class %s: got=%v want=%v", tc.class, foundShield, tc.expectShield)
		}

		defs := skillListForClass(tc.class)
		if len(c.Skills) != len(defs) {
			t.Fatalf("expected only class skills for %s, got %#v", tc.class, c.Skills)
		}
		if c.Skills[tc.primarySkill] != 1 {
			t.Fatalf("expected starter skill %s rank 1 for %s, got %d", tc.primarySkill, tc.class, c.Skills[tc.primarySkill])
		}
		for id := range defs {
			if id == tc.primarySkill {
				continue
			}
			if c.Skills[id] != 0 {
				t.Fatalf("expected non-primary class skill %s rank 0 for %s, got %d", id, tc.class, c.Skills[id])
			}
		}

		wantSTR, wantDEX := classStarterStats(tc.class)
		if c.Strength != wantSTR || c.Dexterity != wantDEX {
			t.Fatalf("unexpected class starter stats for %s: got str=%d dex=%d want str=%d dex=%d", tc.class, c.Strength, c.Dexterity, wantSTR, wantDEX)
		}
	}
}

func TestNewDefaultCharacterUnknownClassFallsBackToArcher(t *testing.T) {
	c := newDefaultCharacter("FallbackHero", "Bard")
	if c.Class != "Archer" {
		t.Fatalf("expected Archer fallback class, got %s", c.Class)
	}
	if !inventoryHasItem(c.Inventory, "starter_bow") {
		t.Fatalf("expected Archer starter bow on unknown class fallback")
	}
	if c.Skills["precise_shot"] != 1 {
		t.Fatalf("expected Archer starter skill on unknown class fallback")
	}
}

func TestLoadCharacterDBModeKeepsExistingClassOnAuthHint(t *testing.T) {
	t.Cleanup(resetPersistenceRuntimeStateForTests)
	resetPersistenceRuntimeStateForTests()
	_ = os.Setenv("A3_PERSISTENCE_MODE", "db")

	restoreWD := enterTempDir(t)
	defer restoreWD()

	created, err := loadCharacter("ClassLockDBHero", "Mage")
	if err != nil {
		t.Fatalf("loadCharacter create failed: %v", err)
	}
	if created.Class != "Mage" {
		t.Fatalf("expected created class Mage, got %s", created.Class)
	}
	if err := persistCharacter(created); err != nil {
		t.Fatalf("persistCharacter failed: %v", err)
	}

	reloaded, err := loadCharacter("ClassLockDBHero", "Warrior")
	if err != nil {
		t.Fatalf("loadCharacter reload failed: %v", err)
	}
	if reloaded.Class != "Mage" {
		t.Fatalf("expected persisted class Mage to be retained, got %s", reloaded.Class)
	}
	if !inventoryHasItem(reloaded.Inventory, "starter_focus") {
		t.Fatalf("expected Mage starter focus retained after re-auth hint")
	}
	if inventoryHasItem(reloaded.Inventory, "starter_blade") {
		t.Fatalf("unexpected Warrior starter blade after re-auth hint")
	}
}

func TestLoadCharacterJSONModeKeepsExistingClassOnAuthHint(t *testing.T) {
	t.Cleanup(resetPersistenceRuntimeStateForTests)
	resetPersistenceRuntimeStateForTests()
	_ = os.Setenv("A3_PERSISTENCE_MODE", "json")

	restoreWD := enterTempDir(t)
	defer restoreWD()

	created, err := loadCharacter("ClassLockJSONHero", "Warrior")
	if err != nil {
		t.Fatalf("loadCharacter create failed: %v", err)
	}
	if created.Class != "Warrior" {
		t.Fatalf("expected created class Warrior, got %s", created.Class)
	}
	if err := persistCharacter(created); err != nil {
		t.Fatalf("persistCharacter failed: %v", err)
	}

	reloaded, err := loadCharacter("ClassLockJSONHero", "Mage")
	if err != nil {
		t.Fatalf("loadCharacter reload failed: %v", err)
	}
	if reloaded.Class != "Warrior" {
		t.Fatalf("expected persisted class Warrior to be retained, got %s", reloaded.Class)
	}
	if !inventoryHasItem(reloaded.Inventory, "starter_blade") {
		t.Fatalf("expected Warrior starter blade retained after re-auth hint")
	}
	if inventoryHasItem(reloaded.Inventory, "starter_focus") {
		t.Fatalf("unexpected Mage starter focus after re-auth hint")
	}
}

func enterTempDir(t *testing.T) func() {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir tmp: %v", err)
	}
	return func() {
		_ = os.Chdir(wd)
	}
}

func inventoryHasItem(items []Item, itemID string) bool {
	for _, item := range items {
		if item.ID == itemID {
			return true
		}
	}
	return false
}
