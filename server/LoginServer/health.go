package main

import (
	"encoding/json"
	"net/http"
	"time"
)

type healthResponse struct {
	Status string                 `json:"status"`
	Time   string                 `json:"time"`
	Checks map[string]interface{} `json:"checks,omitempty"`
}

func registerHealthEndpoints(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", loginHealthHandler)
	mux.HandleFunc("/readyz", loginReadyHandler)
}

func loginHealthHandler(w http.ResponseWriter, _ *http.Request) {
	writeHealthResponse(w, http.StatusOK, healthResponse{
		Status: "ok",
		Time:   time.Now().UTC().Format(time.RFC3339),
		Checks: map[string]interface{}{"service": "loginserver"},
	})
}

func loginReadyHandler(w http.ResponseWriter, _ *http.Request) {
	checks := map[string]interface{}{
		"service":     "loginserver",
		"auth_config": "ok",
		"db_backend":  activeLoginDBBackend(),
	}

	if err := validateAuthConfig(); err != nil {
		checks["auth_config"] = err.Error()
		writeHealthResponse(w, http.StatusServiceUnavailable, healthResponse{
			Status: "not_ready",
			Time:   time.Now().UTC().Format(time.RFC3339),
			Checks: checks,
		})
		return
	}

	if _, err := openLoginAccountDB(); err != nil {
		checks["account_store"] = err.Error()
		writeHealthResponse(w, http.StatusServiceUnavailable, healthResponse{
			Status: "not_ready",
			Time:   time.Now().UTC().Format(time.RFC3339),
			Checks: checks,
		})
		return
	}
	checks["account_store"] = "ok"

	writeHealthResponse(w, http.StatusOK, healthResponse{
		Status: "ready",
		Time:   time.Now().UTC().Format(time.RFC3339),
		Checks: checks,
	})
}

func writeHealthResponse(w http.ResponseWriter, statusCode int, payload healthResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
