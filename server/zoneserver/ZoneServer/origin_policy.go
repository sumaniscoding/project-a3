package main

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func checkWebSocketOrigin(r *http.Request) bool {
	return isAllowedWebSocketOrigin(r.Header.Get("Origin"))
}

func isAllowedWebSocketOrigin(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return true
	}

	allowed := configuredAllowedOrigins()
	if len(allowed) > 0 {
		_, ok := allowed[origin]
		return ok
	}

	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

func configuredAllowedOrigins() map[string]struct{} {
	raw := strings.TrimSpace(os.Getenv("A3_ALLOWED_ORIGINS"))
	if raw == "" {
		return nil
	}
	out := map[string]struct{}{}
	for _, part := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(part)
		if origin == "" {
			continue
		}
		out[origin] = struct{}{}
	}
	return out
}
