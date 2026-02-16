package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

const (
	characterStoreDir = "data/characters"
	characterDBPath   = "data/characters.db"
)

const (
	persistenceDB     = "db"
	persistenceHybrid = "hybrid"
	persistenceJSON   = "json"
)

var (
	characterDBOnce sync.Once
	characterDBConn *sql.DB
	characterDBErr  error

	persistenceModeOnce sync.Once
	persistenceMode     string
)

func resetPersistenceRuntimeStateForTests() {
	if characterDBConn != nil {
		_ = characterDBConn.Close()
	}
	characterDBConn = nil
	characterDBErr = nil
	characterDBOnce = sync.Once{}
	persistenceMode = ""
	persistenceModeOnce = sync.Once{}
}

func loadCharacter(name, class string) (*Character, error) {
	if strings.TrimSpace(name) == "" {
		name = "Wanderer"
	}

	mode := activePersistenceMode()
	safeName := sanitizeCharacterName(name)
	switch mode {
	case persistenceJSON:
		if c, found, err := loadCharacterFromLegacy(name, class); err != nil {
			return nil, err
		} else if found {
			return c, nil
		}
		return newDefaultCharacter(name, class), nil

	case persistenceDB, persistenceHybrid:
		db, err := openCharacterDB()
		if err != nil {
			if mode == persistenceHybrid {
				log.Printf("Character DB unavailable, falling back to JSON: %v", err)
				if c, found, loadErr := loadCharacterFromLegacy(name, class); loadErr != nil {
					return nil, loadErr
				} else if found {
					return c, nil
				}
				return newDefaultCharacter(name, class), nil
			}
			return nil, fmt.Errorf("character db unavailable in db mode: %w", err)
		}

		if c, found, loadErr := loadCharacterFromDB(db, safeName, name, class); loadErr == nil && found {
			return c, nil
		} else if loadErr != nil {
			if mode == persistenceHybrid {
				log.Printf("Character DB read failed for %q; trying JSON fallback: %v", name, loadErr)
				if legacy, found, legacyErr := loadCharacterFromLegacy(name, class); legacyErr == nil && found {
					if persistErr := persistCharacterToDB(db, legacy); persistErr != nil {
						log.Printf("Character migration to DB failed for %q: %v", name, persistErr)
					}
					return legacy, nil
				} else if legacyErr != nil {
					return nil, legacyErr
				}
				return newDefaultCharacter(name, class), nil
			}
			return nil, loadErr
		}

		return newDefaultCharacter(name, class), nil
	}

	return nil, fmt.Errorf("unknown persistence mode %q", mode)
}

func persistCharacter(c *Character) error {
	if c == nil {
		return nil
	}
	ensureCharacterDefaults(c)

	mode := activePersistenceMode()
	switch mode {
	case persistenceJSON:
		return persistCharacterLegacy(c)
	case persistenceDB:
		db, err := openCharacterDB()
		if err != nil {
			return fmt.Errorf("character db unavailable in db mode: %w", err)
		}
		return persistCharacterToDB(db, c)
	case persistenceHybrid:
		db, err := openCharacterDB()
		if err == nil {
			if persistErr := persistCharacterToDB(db, c); persistErr == nil {
				return nil
			} else {
				log.Printf("Character DB write failed for %q, falling back to JSON: %v", c.Name, persistErr)
			}
		} else {
			log.Printf("Character DB unavailable, falling back to JSON: %v", err)
		}
		return persistCharacterLegacy(c)
	default:
		return fmt.Errorf("unknown persistence mode %q", mode)
	}
}

func openCharacterDB() (*sql.DB, error) {
	characterDBOnce.Do(func() {
		if err := os.MkdirAll(filepath.Dir(characterDBPath), 0o755); err != nil {
			characterDBErr = err
			return
		}

		db, err := sql.Open("sqlite", characterDBPath)
		if err != nil {
			characterDBErr = err
			return
		}
		db.SetMaxOpenConns(1)

		schema := `
CREATE TABLE IF NOT EXISTS characters (
  name TEXT PRIMARY KEY,
  payload TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);`
		if _, err := db.Exec(schema); err != nil {
			_ = db.Close()
			characterDBErr = err
			return
		}

		// One-time migration from legacy JSON files into SQLite.
		if err := migrateLegacyCharactersToDB(db); err != nil {
			_ = db.Close()
			characterDBErr = err
			return
		}

		characterDBConn = db
	})
	return characterDBConn, characterDBErr
}

func migrateLegacyCharactersToDB(db *sql.DB) error {
	entries, err := os.ReadDir(characterStoreDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		rawName := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		path := filepath.Join(characterStoreDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var c Character
		if err := json.Unmarshal(data, &c); err != nil {
			return err
		}

		if strings.TrimSpace(c.Name) == "" {
			c.Name = rawName
		}
		ensureCharacterDefaults(&c)
		if err := persistCharacterToDB(db, &c); err != nil {
			return err
		}
	}
	return nil
}

func loadCharacterFromDB(db *sql.DB, safeName, inputName, class string) (*Character, bool, error) {
	var payload string
	err := db.QueryRow(`SELECT payload FROM characters WHERE name = ?`, safeName).Scan(&payload)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var c Character
	if err := json.Unmarshal([]byte(payload), &c); err != nil {
		return nil, false, err
	}

	c.Name = inputName
	if strings.TrimSpace(class) != "" {
		c.Class = strings.Title(strings.ToLower(strings.TrimSpace(class)))
	}
	ensureCharacterDefaults(&c)
	return &c, true, nil
}

func persistCharacterToDB(db *sql.DB, c *Character) error {
	payload, err := json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT INTO characters(name, payload, updated_at)
		 VALUES(?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(name) DO UPDATE SET
		   payload=excluded.payload,
		   updated_at=CURRENT_TIMESTAMP`,
		sanitizeCharacterName(c.Name),
		string(payload),
	)
	return err
}

func loadCharacterFromLegacy(name, class string) (*Character, bool, error) {
	path := filepath.Join(characterStoreDir, sanitizeCharacterName(name)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var c Character
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, false, err
	}
	c.Name = name
	if strings.TrimSpace(class) != "" {
		c.Class = strings.Title(strings.ToLower(strings.TrimSpace(class)))
	}
	ensureCharacterDefaults(&c)
	return &c, true, nil
}

func persistCharacterLegacy(c *Character) error {
	if err := os.MkdirAll(characterStoreDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(characterStoreDir, sanitizeCharacterName(c.Name)+".json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func newDefaultCharacter(name, class string) *Character {
	c := MockCharacter()
	c.Name = name
	if strings.TrimSpace(class) != "" {
		c.Class = strings.Title(strings.ToLower(strings.TrimSpace(class)))
	}
	ensureCharacterDefaults(c)
	return c
}

func activePersistenceMode() string {
	persistenceModeOnce.Do(func() {
		raw := strings.ToLower(strings.TrimSpace(os.Getenv("A3_PERSISTENCE_MODE")))
		switch raw {
		case "", "db", "sqlite":
			persistenceMode = persistenceDB
		case "hybrid":
			persistenceMode = persistenceHybrid
		case "json", "legacy":
			persistenceMode = persistenceJSON
		default:
			log.Printf("Unknown A3_PERSISTENCE_MODE=%q, defaulting to db", raw)
			persistenceMode = persistenceDB
		}
		log.Printf("Persistence mode: %s", persistenceMode)
	})
	return persistenceMode
}

func ensureCharacterDefaults(c *Character) {
	if c.UnlockedWorlds == nil {
		c.UnlockedWorlds = map[WorldID]bool{World1: true}
	}
	if !c.UnlockedWorlds[World1] {
		c.UnlockedWorlds[World1] = true
	}
	if c.Trust == nil {
		c.Trust = make(map[string]int)
	}
	if c.Quests == nil {
		c.Quests = make(map[string]*QuestProgress)
	}
	if c.Equipped == nil {
		c.Equipped = make(map[string]string)
	}
	if c.Elemental == nil {
		c.Elemental = map[string]Element{
			"weapon": ElementNone,
			"armor":  ElementNone,
			"pet":    ElementNone,
		}
	}
	if c.Skills == nil {
		c.Skills = map[string]int{}
	}
	if c.Class == "" {
		c.Class = "Archer"
	}
	classSkills := skillListForClass(c.Class)
	for id := range classSkills {
		if _, ok := c.Skills[id]; !ok {
			c.Skills[id] = 0
		}
	}
	if c.MaxHP <= 0 {
		c.MaxHP = 100 + c.Level*2
	}
	if c.HP <= 0 {
		c.HP = c.MaxHP
	}
}

func sanitizeCharacterName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "wanderer"
	}
	var b strings.Builder
	for _, ch := range name {
		switch {
		case ch >= 'a' && ch <= 'z':
			b.WriteRune(ch)
		case ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		case ch == '_' || ch == '-':
			b.WriteRune(ch)
		case ch == ' ':
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "wanderer"
	}
	return b.String()
}
