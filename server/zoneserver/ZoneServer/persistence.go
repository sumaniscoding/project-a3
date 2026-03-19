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

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

const (
	characterStoreDir = "data/characters"
	accountStoreDir   = "data/accounts"
	characterDBPath   = "data/characters.db"
)

const (
	persistenceDB     = "db"
	persistenceHybrid = "hybrid"
	persistenceJSON   = "json"
)

const (
	persistenceBackendSQLite   = "sqlite"
	persistenceBackendPostgres = "postgres"
)

var (
	characterDBOnce sync.Once
	characterDBConn *sql.DB
	characterDBErr  error

	persistenceBackendOnce sync.Once
	persistenceBackend     string

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
	persistenceBackend = ""
	persistenceBackendOnce = sync.Once{}
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

func loadExistingCharacter(name string) (*Character, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false, nil
	}

	mode := activePersistenceMode()
	safeName := sanitizeCharacterName(name)
	switch mode {
	case persistenceJSON:
		return loadCharacterFromLegacy(name, "")
	case persistenceDB, persistenceHybrid:
		db, err := openCharacterDB()
		if err != nil {
			if mode == persistenceHybrid {
				return loadCharacterFromLegacy(name, "")
			}
			return nil, false, fmt.Errorf("character db unavailable in db mode: %w", err)
		}

		c, found, loadErr := loadCharacterFromDB(db, safeName, name, "")
		if loadErr == nil {
			return c, found, nil
		}
		if mode != persistenceHybrid {
			return nil, false, loadErr
		}

		legacy, found, legacyErr := loadCharacterFromLegacy(name, "")
		if legacyErr != nil {
			return nil, false, legacyErr
		}
		if found {
			if persistErr := persistCharacterToDB(db, legacy); persistErr != nil {
				log.Printf("Character migration to DB failed for %q: %v", name, persistErr)
			}
		}
		return legacy, found, nil
	default:
		return nil, false, fmt.Errorf("unknown persistence mode %q", mode)
	}
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

func loadAccount(username string) (*Account, error) {
	if strings.TrimSpace(username) == "" {
		username = "Wanderer"
	}

	mode := activePersistenceMode()
	safeUser := sanitizeCharacterName(username)
	switch mode {
	case persistenceJSON:
		if a, found, err := loadAccountFromLegacy(username); err != nil {
			return nil, err
		} else if found {
			return a, nil
		}
		return newDefaultAccount(username), nil

	case persistenceDB, persistenceHybrid:
		db, err := openCharacterDB()
		if err != nil {
			if mode == persistenceHybrid {
				log.Printf("Account DB unavailable, falling back to JSON: %v", err)
				if a, found, loadErr := loadAccountFromLegacy(username); loadErr != nil {
					return nil, loadErr
				} else if found {
					return a, nil
				}
				return newDefaultAccount(username), nil
			}
			return nil, fmt.Errorf("account db unavailable in db mode: %w", err)
		}

		if a, found, loadErr := loadAccountFromDB(db, safeUser, username); loadErr == nil && found {
			return a, nil
		} else if loadErr != nil {
			if mode == persistenceHybrid {
				log.Printf("Account DB read failed for %q; trying JSON fallback: %v", username, loadErr)
				if legacy, found, legacyErr := loadAccountFromLegacy(username); legacyErr == nil && found {
					if persistErr := persistAccountToDB(db, legacy); persistErr != nil {
						log.Printf("Account migration to DB failed for %q: %v", username, persistErr)
					}
					return legacy, nil
				} else if legacyErr != nil {
					return nil, legacyErr
				}
				return newDefaultAccount(username), nil
			}
			return nil, loadErr
		}

		return newDefaultAccount(username), nil
	}

	return nil, fmt.Errorf("unknown persistence mode %q", mode)
}

func persistAccount(a *Account) error {
	if a == nil {
		return nil
	}
	ensureAccountDefaults(a)

	mode := activePersistenceMode()
	switch mode {
	case persistenceJSON:
		return persistAccountLegacy(a)
	case persistenceDB:
		db, err := openCharacterDB()
		if err != nil {
			return fmt.Errorf("account db unavailable in db mode: %w", err)
		}
		return persistAccountToDB(db, a)
	case persistenceHybrid:
		db, err := openCharacterDB()
		if err == nil {
			if persistErr := persistAccountToDB(db, a); persistErr == nil {
				return nil
			} else {
				log.Printf("Account DB write failed for %q, falling back to JSON: %v", a.Username, persistErr)
			}
		} else {
			log.Printf("Account DB unavailable, falling back to JSON: %v", err)
		}
		return persistAccountLegacy(a)
	default:
		return fmt.Errorf("unknown persistence mode %q", mode)
	}
}

func openCharacterDB() (*sql.DB, error) {
	characterDBOnce.Do(func() {
		backend := activePersistenceDBBackend()
		driverName, dataSourceName, err := persistenceDriverConfig(backend)
		if err != nil {
			characterDBErr = err
			return
		}

		db, err := sql.Open(driverName, dataSourceName)
		if err != nil {
			characterDBErr = err
			return
		}
		applyPersistenceConnectionPool(db, backend)

		for _, stmt := range persistenceSchemaStatements(backend) {
			if _, err := db.Exec(stmt); err != nil {
				_ = db.Close()
				characterDBErr = err
				return
			}
		}
		if err := db.Ping(); err != nil {
			_ = db.Close()
			characterDBErr = err
			return
		}

		if err := migrateLegacyCharactersToDB(db); err != nil {
			_ = db.Close()
			characterDBErr = err
			return
		}
		if err := migrateLegacyAccountsToDB(db); err != nil {
			_ = db.Close()
			characterDBErr = err
			return
		}

		characterDBConn = db
	})
	return characterDBConn, characterDBErr
}

func activePersistenceDBBackend() string {
	persistenceBackendOnce.Do(func() {
		raw := strings.ToLower(strings.TrimSpace(os.Getenv("A3_DB_BACKEND")))
		switch raw {
		case "":
			if strings.TrimSpace(os.Getenv("A3_DATABASE_URL")) != "" {
				persistenceBackend = persistenceBackendPostgres
			} else {
				persistenceBackend = persistenceBackendSQLite
			}
		case persistenceBackendPostgres, "pg", "postgresql":
			persistenceBackend = persistenceBackendPostgres
		case persistenceBackendSQLite:
			persistenceBackend = persistenceBackendSQLite
		default:
			log.Printf("Unknown A3_DB_BACKEND=%q, defaulting to sqlite", raw)
			persistenceBackend = persistenceBackendSQLite
		}
		log.Printf("Persistence DB backend: %s", persistenceBackend)
	})
	return persistenceBackend
}

func persistenceDriverConfig(backend string) (string, string, error) {
	switch backend {
	case persistenceBackendPostgres:
		dsn := strings.TrimSpace(os.Getenv("A3_DATABASE_URL"))
		if dsn == "" {
			return "", "", fmt.Errorf("A3_DATABASE_URL is required when A3_DB_BACKEND=postgres")
		}
		return "pgx", dsn, nil
	case persistenceBackendSQLite:
		dbPath := strings.TrimSpace(os.Getenv("A3_SQLITE_PATH"))
		if dbPath == "" {
			dbPath = characterDBPath
		}
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return "", "", err
		}
		return "sqlite", dbPath, nil
	default:
		return "", "", fmt.Errorf("unsupported persistence backend %q", backend)
	}
}

func applyPersistenceConnectionPool(db *sql.DB, backend string) {
	switch backend {
	case persistenceBackendPostgres:
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
	default:
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}
}

func persistenceSchemaStatements(backend string) []string {
	switch backend {
	case persistenceBackendPostgres:
		return []string{
			`CREATE TABLE IF NOT EXISTS characters (
			  name TEXT PRIMARY KEY,
			  payload TEXT NOT NULL,
			  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`,
			`CREATE TABLE IF NOT EXISTS accounts (
			  username TEXT PRIMARY KEY,
			  payload TEXT NOT NULL,
			  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`,
		}
	default:
		return []string{
			`PRAGMA journal_mode=WAL;`,
			`PRAGMA synchronous=NORMAL;`,
			`PRAGMA busy_timeout=5000;`,
			`CREATE TABLE IF NOT EXISTS characters (
			  name TEXT PRIMARY KEY,
			  payload TEXT NOT NULL,
			  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
			`CREATE TABLE IF NOT EXISTS accounts (
			  username TEXT PRIMARY KEY,
			  payload TEXT NOT NULL,
			  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
		}
	}
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

func migrateLegacyAccountsToDB(db *sql.DB) error {
	entries, err := os.ReadDir(accountStoreDir)
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
		path := filepath.Join(accountStoreDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		var a Account
		if err := json.Unmarshal(data, &a); err != nil {
			return err
		}

		if strings.TrimSpace(a.Username) == "" {
			a.Username = rawName
		}
		ensureAccountDefaults(&a)
		if err := persistAccountToDB(db, &a); err != nil {
			return err
		}
	}
	return nil
}

func loadCharacterFromDB(db *sql.DB, safeName, inputName, class string) (*Character, bool, error) {
	var payload string
	err := db.QueryRow(characterSelectQuery(), safeName).Scan(&payload)
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
	if canonical := canonicalCharacterClass(class); canonical != "" {
		// Class selection is intended for initial creation only.
		// Existing characters keep their stored class to avoid accidental rerolls.
		if existing := canonicalCharacterClass(c.Class); existing == "" {
			c.Class = canonical
		}
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
		characterUpsertQuery(),
		sanitizeCharacterName(c.Name),
		string(payload),
	)
	return err
}

func loadAccountFromDB(db *sql.DB, safeUser, inputUser string) (*Account, bool, error) {
	var payload string
	err := db.QueryRow(accountSelectQuery(), safeUser).Scan(&payload)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var a Account
	if err := json.Unmarshal([]byte(payload), &a); err != nil {
		return nil, false, err
	}
	a.Username = inputUser
	ensureAccountDefaults(&a)
	return &a, true, nil
}

func persistAccountToDB(db *sql.DB, a *Account) error {
	payload, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		accountUpsertQuery(),
		sanitizeCharacterName(a.Username),
		string(payload),
	)
	return err
}

func characterSelectQuery() string {
	if activePersistenceDBBackend() == persistenceBackendPostgres {
		return `SELECT payload FROM characters WHERE name = $1`
	}
	return `SELECT payload FROM characters WHERE name = ?`
}

func accountSelectQuery() string {
	if activePersistenceDBBackend() == persistenceBackendPostgres {
		return `SELECT payload FROM accounts WHERE username = $1`
	}
	return `SELECT payload FROM accounts WHERE username = ?`
}

func characterUpsertQuery() string {
	if activePersistenceDBBackend() == persistenceBackendPostgres {
		return `INSERT INTO characters(name, payload, updated_at)
		 VALUES($1, $2, NOW())
		 ON CONFLICT(name) DO UPDATE SET
		   payload=EXCLUDED.payload,
		   updated_at=NOW()`
	}
	return `INSERT INTO characters(name, payload, updated_at)
		 VALUES(?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(name) DO UPDATE SET
		   payload=excluded.payload,
		   updated_at=CURRENT_TIMESTAMP`
}

func accountUpsertQuery() string {
	if activePersistenceDBBackend() == persistenceBackendPostgres {
		return `INSERT INTO accounts(username, payload, updated_at)
		 VALUES($1, $2, NOW())
		 ON CONFLICT(username) DO UPDATE SET
		   payload=EXCLUDED.payload,
		   updated_at=NOW()`
	}
	return `INSERT INTO accounts(username, payload, updated_at)
		 VALUES(?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(username) DO UPDATE SET
		   payload=excluded.payload,
		   updated_at=CURRENT_TIMESTAMP`
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
	if canonical := canonicalCharacterClass(class); canonical != "" {
		// Class selection is intended for initial creation only.
		// Existing characters keep their stored class to avoid accidental rerolls.
		if existing := canonicalCharacterClass(c.Class); existing == "" {
			c.Class = canonical
		}
	}
	ensureCharacterDefaults(&c)
	return &c, true, nil
}

func loadAccountFromLegacy(username string) (*Account, bool, error) {
	path := filepath.Join(accountStoreDir, sanitizeCharacterName(username)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}

	var a Account
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, false, err
	}
	a.Username = username
	ensureAccountDefaults(&a)
	return &a, true, nil
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

func persistAccountLegacy(a *Account) error {
	if err := os.MkdirAll(accountStoreDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(accountStoreDir, sanitizeCharacterName(a.Username)+".json")
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func classStarterSkills(class string) map[string]int {
	canonical := canonicalCharacterClass(class)
	defs := skillListForClass(canonical)
	skills := make(map[string]int, len(defs))
	for id := range defs {
		skills[id] = 0
	}

	switch canonical {
	case "Warrior":
		skills["cleave"] = 1
	case "Mage":
		skills["arc_bolt"] = 1
	case "Healing Knight":
		skills["holy_slash"] = 1
	default:
		skills["precise_shot"] = 1
	}

	return skills
}

func classStarterInventory(class string) []Item {
	switch canonicalCharacterClass(class) {
	case "Warrior":
		return []Item{
			{
				ID:        "starter_blade",
				Name:      "Recruit Blade",
				Grade:     2,
				Rarity:    RarityCommon,
				Slot:      SlotWeapon,
				Element:   ElementNone,
				GearLevel: 1,
				MinSTR:    12,
				MinDEX:    8,
			},
			{
				ID:        "starter_plate",
				Name:      "Recruit Plate",
				Grade:     2,
				Rarity:    RarityRare,
				Slot:      SlotArmor,
				Element:   ElementNone,
				GearLevel: 1,
				MinSTR:    10,
				MinDEX:    8,
			},
		}
	case "Mage":
		return []Item{
			{
				ID:        "starter_focus",
				Name:      "Initiate Focus",
				Grade:     2,
				Rarity:    RarityCommon,
				Slot:      SlotWeapon,
				Element:   ElementNone,
				GearLevel: 1,
				MinSTR:    6,
				MinDEX:    12,
			},
			{
				ID:        "starter_robe",
				Name:      "Initiate Robe",
				Grade:     2,
				Rarity:    RarityRare,
				Slot:      SlotArmor,
				Element:   ElementNone,
				GearLevel: 1,
				MinSTR:    6,
				MinDEX:    10,
			},
		}
	case "Healing Knight":
		return []Item{
			{
				ID:        "starter_mace",
				Name:      "Initiate Mace",
				Grade:     2,
				Rarity:    RarityCommon,
				Slot:      SlotWeapon,
				Element:   ElementLight,
				GearLevel: 1,
				MinSTR:    10,
				MinDEX:    8,
			},
			{
				ID:        "starter_guard_mail",
				Name:      "Initiate Guard Mail",
				Grade:     2,
				Rarity:    RarityRare,
				Slot:      SlotArmor,
				Element:   ElementLight,
				GearLevel: 1,
				MinSTR:    10,
				MinDEX:    8,
			},
			{
				ID:        "starter_shield",
				Name:      "Initiate Guard Shield",
				Grade:     2,
				Rarity:    RarityCommon,
				Slot:      SlotShield,
				Element:   ElementLight,
				GearLevel: 1,
				MinSTR:    8,
				MinDEX:    6,
			},
		}
	default:
		return []Item{
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
		}
	}
}

func newDefaultCharacter(name, class string) *Character {
	c := MockCharacter()
	c.Name = name
	if canonical := canonicalCharacterClass(class); canonical != "" {
		c.Class = canonical
	} else if canonical := canonicalCharacterClass(c.Class); canonical != "" {
		c.Class = canonical
	} else {
		c.Class = "Archer"
	}
	c.Inventory = classStarterInventory(c.Class)
	c.Skills = classStarterSkills(c.Class)
	c.Equipped = map[string]string{}
	c.Strength, c.Dexterity = classStarterStats(c.Class)
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
	c.Presence = canonicalPresenceStatus(c.Presence)
	if c.Materials == nil {
		c.Materials = map[string]int{}
	}
	if c.Friends == nil {
		c.Friends = map[string]bool{}
	}
	if c.Blocks == nil {
		c.Blocks = map[string]bool{}
	}
	if c.Skills == nil {
		c.Skills = map[string]int{}
	}
	if strings.TrimSpace(c.Guild) == "" {
		c.GuildRole = ""
	} else if strings.TrimSpace(c.GuildRole) == "" {
		c.GuildRole = "member"
	}
	if canonical := canonicalCharacterClass(c.Class); canonical != "" {
		c.Class = canonical
	} else {
		c.Class = "Archer"
	}
	if c.Strength <= 0 || c.Dexterity <= 0 {
		baseSTR, baseDEX := classBaseStats(c.Class)
		level := c.Level
		if level < 1 {
			level = 1
		}
		c.Strength = baseSTR + (level - 1)
		c.Dexterity = baseDEX + (level - 1)
	}
	if c.Gold < 0 {
		c.Gold = 0
	}
	if c.WalletGold < 0 {
		c.WalletGold = 0
	}
	if c.Storage.Materials == nil {
		c.Storage.Materials = map[string]int{}
	}
	if c.Storage.Items == nil {
		c.Storage.Items = []Item{}
	}
	for i := range c.Inventory {
		ensureItemDefaults(&c.Inventory[i])
	}
	for i := range c.Storage.Items {
		ensureItemDefaults(&c.Storage.Items[i])
	}
	if c.Pet.Level < 1 {
		c.Pet.Level = 1
	}
	if c.Pet.Level > maxPetLevel {
		c.Pet.Level = maxPetLevel
	}
	if c.Pet.XP < 0 {
		c.Pet.XP = 0
	}
	if !c.Pet.Acquired {
		if c.Pet.Summoned || c.Pet.Level > 1 || c.Pet.XP > 0 {
			c.Pet.Acquired = true
		}
		if qp, ok := c.Quests["unlock_world2_race"]; ok && qp != nil && qp.Complete {
			c.Pet.Acquired = true
		}
	}
	if !c.Pet.Acquired {
		c.Pet.Summoned = false
	}
	if c.Mercenary.Equipped == nil {
		c.Mercenary.Equipped = map[string]string{}
	}
	if canonical := canonicalClassName(c.Mercenary.Class); canonical != "" {
		c.Mercenary.Class = canonical
	} else if c.Mercenary.Class == "" {
		c.Mercenary.Class = "Warrior"
	}
	if c.Mercenary.Level < 1 {
		c.Mercenary.Level = 1
	}
	if c.Mercenary.Level > maxMercLevel {
		c.Mercenary.Level = maxMercLevel
	}
	if c.Mercenary.XP < 0 {
		c.Mercenary.XP = 0
	}
	syncMercStats(c)
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
