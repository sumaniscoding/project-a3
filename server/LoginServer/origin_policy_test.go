package main

import "testing"

func TestIsAllowedWebSocketOrigin(t *testing.T) {
	t.Setenv("A3_ALLOWED_ORIGINS", "")

	if !isAllowedWebSocketOrigin("") {
		t.Fatalf("expected empty origin to be allowed for native clients")
	}
	if !isAllowedWebSocketOrigin("http://localhost:3000") {
		t.Fatalf("expected localhost origin to be allowed by default")
	}
	if !isAllowedWebSocketOrigin("http://127.0.0.1:8080") {
		t.Fatalf("expected loopback origin to be allowed by default")
	}
	if isAllowedWebSocketOrigin("https://example.com") {
		t.Fatalf("expected non-loopback origin to be rejected by default")
	}
}

func TestConfiguredAllowedOrigins(t *testing.T) {
	t.Setenv("A3_ALLOWED_ORIGINS", "https://game.example.com,https://admin.example.com")

	if !isAllowedWebSocketOrigin("https://game.example.com") {
		t.Fatalf("expected configured origin to be allowed")
	}
	if isAllowedWebSocketOrigin("http://localhost:3000") {
		t.Fatalf("expected default localhost allowance to be disabled when explicit origins are configured")
	}
}
