package main

import (
	"net"
	"strings"
	"sync"
	"time"
)

type zoneAuthWindow struct {
	windowStart  time.Time
	count        int
	blockedUntil time.Time
}

var (
	zoneAuthMu     sync.Mutex
	zoneAuthByPeer = map[string]*zoneAuthWindow{}
)

func zonePeerKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil || host == "" {
		return strings.TrimSpace(remoteAddr)
	}
	return host
}

func allowZoneAuthAttempt(peer string) (bool, time.Duration) {
	const (
		maxAttempts = 10
		window      = 60 * time.Second
		blockFor    = 2 * time.Minute
	)

	now := time.Now()
	zoneAuthMu.Lock()
	defer zoneAuthMu.Unlock()

	w := zoneAuthByPeer[peer]
	if w == nil {
		w = &zoneAuthWindow{}
		zoneAuthByPeer[peer] = w
	}

	if now.Before(w.blockedUntil) {
		return false, time.Until(w.blockedUntil)
	}
	if w.windowStart.IsZero() || now.Sub(w.windowStart) > window {
		w.windowStart = now
		w.count = 0
	}

	w.count++
	if w.count > maxAttempts {
		w.blockedUntil = now.Add(blockFor)
		return false, blockFor
	}
	return true, 0
}

func resetZoneAuthAttempts(peer string) {
	zoneAuthMu.Lock()
	defer zoneAuthMu.Unlock()
	delete(zoneAuthByPeer, peer)
}
