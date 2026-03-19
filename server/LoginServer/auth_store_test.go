package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestActiveLoginDBBackend(t *testing.T) {
	t.Cleanup(resetLoginAccountRuntimeStateForTests)

	tests := []struct {
		name        string
		backendEnv  string
		databaseURL string
		want        string
	}{
		{name: "default sqlite", want: loginDBBackendSQLite},
		{name: "database url implies postgres", databaseURL: "postgres://example", want: loginDBBackendPostgres},
		{name: "explicit postgres", backendEnv: "postgres", want: loginDBBackendPostgres},
		{name: "postgres alias", backendEnv: "postgresql", want: loginDBBackendPostgres},
		{name: "explicit sqlite", backendEnv: "sqlite", databaseURL: "postgres://example", want: loginDBBackendSQLite},
		{name: "unknown defaults sqlite", backendEnv: "bogus", want: loginDBBackendSQLite},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetLoginAccountRuntimeStateForTests()
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
			if got := activeLoginDBBackend(); got != tc.want {
				t.Fatalf("backend=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestRegisterAndVerifyLoginAccount(t *testing.T) {
	tempDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalWD)
		resetLoginAccountRuntimeStateForTests()
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir tempdir: %v", err)
	}
	_ = os.Unsetenv("A3_DB_BACKEND")
	_ = os.Unsetenv("A3_DATABASE_URL")
	resetLoginAccountRuntimeStateForTests()

	username, err := registerLoginAccount("TestUser", "demo-pass")
	if err != nil {
		t.Fatalf("registerLoginAccount failed: %v", err)
	}
	if username != "TestUser" {
		t.Fatalf("expected preserved username, got %q", username)
	}

	if _, err := os.Stat(filepath.Join(tempDir, loginAccountDBPath)); err != nil {
		t.Fatalf("expected auth db to exist: %v", err)
	}

	verifiedUsername, ok, err := verifyLoginCredentials("testuser", "demo-pass")
	if err != nil {
		t.Fatalf("verifyLoginCredentials returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected credentials to verify")
	}
	if verifiedUsername != "TestUser" {
		t.Fatalf("expected stored display username, got %q", verifiedUsername)
	}

	if _, ok, err := verifyLoginCredentials("testuser", "wrong-pass"); err != nil {
		t.Fatalf("verifyLoginCredentials returned error for wrong pass: %v", err)
	} else if ok {
		t.Fatalf("expected wrong password to fail verification")
	}
}

func TestRegisterRejectsDuplicateAccount(t *testing.T) {
	tempDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(originalWD)
		resetLoginAccountRuntimeStateForTests()
	}()

	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir tempdir: %v", err)
	}
	_ = os.Unsetenv("A3_DB_BACKEND")
	_ = os.Unsetenv("A3_DATABASE_URL")
	resetLoginAccountRuntimeStateForTests()

	if _, err := registerLoginAccount("TestUser", "demo-pass"); err != nil {
		t.Fatalf("initial register failed: %v", err)
	}
	if _, err := registerLoginAccount("testuser", "demo-pass"); !isAccountExistsError(err) {
		t.Fatalf("expected duplicate register to fail with account exists, got %v", err)
	}
}
