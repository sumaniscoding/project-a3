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
