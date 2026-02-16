package main

import (
	"net"
	"strings"
	"sync"
	"time"
)

type authWindow struct {
	windowStart  time.Time
	count        int
	blockedUntil time.Time
}

var (
	loginAuthMu     sync.Mutex
	loginAuthByPeer = map[string]*authWindow{}
)

func loginPeerKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil || host == "" {
		return strings.TrimSpace(remoteAddr)
	}
	return host
}

func allowLoginAuthAttempt(peer string) (bool, time.Duration) {
	const (
		maxAttempts = 8
		window      = 60 * time.Second
		blockFor    = 2 * time.Minute
	)

	now := time.Now()
	loginAuthMu.Lock()
	defer loginAuthMu.Unlock()

	w := loginAuthByPeer[peer]
	if w == nil {
		w = &authWindow{}
		loginAuthByPeer[peer] = w
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

func resetLoginAuthAttempts(peer string) {
	loginAuthMu.Lock()
	defer loginAuthMu.Unlock()
	delete(loginAuthByPeer, peer)
}
