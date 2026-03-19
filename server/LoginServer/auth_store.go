package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

const loginAccountDBPath = "data/login_accounts.db"

const (
	loginDBBackendSQLite   = "sqlite"
	loginDBBackendPostgres = "postgres"
)

var (
	loginAccountDBOnce sync.Once
	loginAccountDB     *sql.DB
	loginAccountDBErr  error

	loginDBBackendOnce sync.Once
	loginDBBackend     string

	errInvalidCredentials = errors.New("INVALID_CREDENTIALS")
)

func resetLoginAccountRuntimeStateForTests() {
	if loginAccountDB != nil {
		_ = loginAccountDB.Close()
	}
	loginAccountDB = nil
	loginAccountDBErr = nil
	loginAccountDBOnce = sync.Once{}
	loginDBBackend = ""
	loginDBBackendOnce = sync.Once{}
}

func openLoginAccountDB() (*sql.DB, error) {
	loginAccountDBOnce.Do(func() {
		backend := activeLoginDBBackend()
		driverName, dataSourceName, err := loginDBDriverConfig(backend)
		if err != nil {
			loginAccountDBErr = err
			return
		}

		db, err := sql.Open(driverName, dataSourceName)
		if err != nil {
			loginAccountDBErr = err
			return
		}
		applyLoginDBConnectionPool(db, backend)

		for _, stmt := range loginDBSchemaStatements(backend) {
			if _, err := db.Exec(stmt); err != nil {
				_ = db.Close()
				loginAccountDBErr = err
				return
			}
		}
		if err := db.Ping(); err != nil {
			_ = db.Close()
			loginAccountDBErr = err
			return
		}

		loginAccountDB = db
	})

	return loginAccountDB, loginAccountDBErr
}

func activeLoginDBBackend() string {
	loginDBBackendOnce.Do(func() {
		raw := strings.ToLower(strings.TrimSpace(os.Getenv("A3_DB_BACKEND")))
		switch raw {
		case "":
			if strings.TrimSpace(os.Getenv("A3_DATABASE_URL")) != "" {
				loginDBBackend = loginDBBackendPostgres
			} else {
				loginDBBackend = loginDBBackendSQLite
			}
		case loginDBBackendPostgres, "pg", "postgresql":
			loginDBBackend = loginDBBackendPostgres
		case loginDBBackendSQLite:
			loginDBBackend = loginDBBackendSQLite
		default:
			loginDBBackend = loginDBBackendSQLite
		}
	})
	return loginDBBackend
}

func loginDBDriverConfig(backend string) (string, string, error) {
	switch backend {
	case loginDBBackendPostgres:
		dsn := strings.TrimSpace(os.Getenv("A3_DATABASE_URL"))
		if dsn == "" {
			return "", "", fmt.Errorf("A3_DATABASE_URL is required when A3_DB_BACKEND=postgres")
		}
		return "pgx", dsn, nil
	case loginDBBackendSQLite:
		dbPath := strings.TrimSpace(os.Getenv("A3_LOGIN_SQLITE_PATH"))
		if dbPath == "" {
			dbPath = loginAccountDBPath
		}
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return "", "", err
		}
		return "sqlite", dbPath, nil
	default:
		return "", "", fmt.Errorf("unsupported login db backend %q", backend)
	}
}

func applyLoginDBConnectionPool(db *sql.DB, backend string) {
	switch backend {
	case loginDBBackendPostgres:
		db.SetMaxOpenConns(10)
		db.SetMaxIdleConns(5)
	default:
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
	}
}

func loginDBSchemaStatements(backend string) []string {
	switch backend {
	case loginDBBackendPostgres:
		return []string{
			`CREATE TABLE IF NOT EXISTS login_accounts (
			  login_key TEXT PRIMARY KEY,
			  username TEXT NOT NULL,
			  password_hash TEXT NOT NULL,
			  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			)`,
		}
	default:
		return []string{
			`PRAGMA journal_mode=WAL;`,
			`PRAGMA synchronous=NORMAL;`,
			`PRAGMA busy_timeout=5000;`,
			`CREATE TABLE IF NOT EXISTS login_accounts (
			  login_key TEXT PRIMARY KEY,
			  username TEXT NOT NULL,
			  password_hash TEXT NOT NULL,
			  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
			)`,
		}
	}
}

func registerLoginAccount(rawUsername, password string) (string, error) {
	username, loginKey, err := normalizeLoginUsername(rawUsername)
	if err != nil {
		return "", err
	}
	if err := validateLoginPassword(password); err != nil {
		return "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	db, err := openLoginAccountDB()
	if err != nil {
		return "", err
	}
	_, err = db.Exec(
		loginAccountInsertQuery(),
		loginKey,
		username,
		string(hash),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return "", fmt.Errorf("ACCOUNT_EXISTS: %w", err)
		}
		return "", err
	}

	return username, nil
}

func verifyLoginCredentials(rawUsername, password string) (string, bool, error) {
	_, loginKey, err := normalizeLoginUsername(rawUsername)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(password) == "" {
		return "", false, errInvalidCredentials
	}

	db, err := openLoginAccountDB()
	if err != nil {
		return "", false, err
	}

	var username string
	var passwordHash string
	err = db.QueryRow(
		loginAccountSelectQuery(),
		loginKey,
	).Scan(&username, &passwordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return "", false, nil
		}
		return "", false, err
	}

	return username, true, nil
}

func loginAccountInsertQuery() string {
	if activeLoginDBBackend() == loginDBBackendPostgres {
		return `INSERT INTO login_accounts(login_key, username, password_hash, updated_at)
		 VALUES($1, $2, $3, NOW())`
	}
	return `INSERT INTO login_accounts(login_key, username, password_hash, updated_at)
		 VALUES(?, ?, ?, CURRENT_TIMESTAMP)`
}

func loginAccountSelectQuery() string {
	if activeLoginDBBackend() == loginDBBackendPostgres {
		return `SELECT username, password_hash FROM login_accounts WHERE login_key = $1`
	}
	return `SELECT username, password_hash FROM login_accounts WHERE login_key = ?`
}

func normalizeLoginUsername(raw string) (string, string, error) {
	username := strings.TrimSpace(raw)
	if username == "" {
		return "", "", errInvalidCredentials
	}
	if len(username) < 3 || len(username) > 24 {
		return "", "", errInvalidCredentials
	}

	loginKey := sanitizeLoginName(username)
	if len(loginKey) < 3 {
		return "", "", errInvalidCredentials
	}
	return username, loginKey, nil
}

func sanitizeLoginName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
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
	return b.String()
}

func validateLoginPassword(password string) error {
	password = strings.TrimSpace(password)
	if len(password) < 4 || len(password) > 128 {
		return errInvalidCredentials
	}
	return nil
}

func isAccountExistsError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "ACCOUNT_EXISTS")
}

func isInvalidCredentialsError(err error) bool {
	return errors.Is(err, errInvalidCredentials)
}
