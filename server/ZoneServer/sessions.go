package main

import "sync"

var (
	sessions   = make(map[*ClientSession]bool)
	sessionsMu sync.RWMutex
)

func registerSession(s *ClientSession) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	sessions[s] = true
}

func unregisterSession(s *ClientSession) {
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	delete(sessions, s)
}

func getSessions() []*ClientSession {
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()

	result := make([]*ClientSession, 0, len(sessions))
	for s := range sessions {
		result = append(result, s)
	}
	return result
}

func forEachSession(fn func(*ClientSession)) {
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	for s := range sessions {
		fn(s)
	}
}

