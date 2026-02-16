package main

import (
	"encoding/json"
	"os"
)

type ZoneConfig struct {
	ServerName string `json:"server_name"`
	TickRateMS int    `json:"tick_rate_ms"`
	ListenPort int    `json:"listen_port"`
}

func loadZoneConfig(path string) ZoneConfig {
	cfg := ZoneConfig{
		ServerName: "Project A3 Zone Server",
		TickRateMS: 1000,
		ListenPort: 7777,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		return ZoneConfig{
			ServerName: "Project A3 Zone Server",
			TickRateMS: 1000,
			ListenPort: 7777,
		}
	}

	if cfg.ServerName == "" {
		cfg.ServerName = "Project A3 Zone Server"
	}
	if cfg.TickRateMS <= 0 {
		cfg.TickRateMS = 1000
	}
	if cfg.ListenPort <= 0 {
		cfg.ListenPort = 7777
	}

	return cfg
}
