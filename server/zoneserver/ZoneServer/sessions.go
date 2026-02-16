package main

import "sync"

var (
	sessions       = make(map[*ClientSession]bool)
	sessionsByName = make(map[string]*ClientSession)
	sessionsMu     sync.RWMutex
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
	if s.Character != nil && s.Character.Name != "" {
		if current, ok := sessionsByName[s.Character.Name]; ok && current == s {
			delete(sessionsByName, s.Character.Name)
		}
	}
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

func bindSessionCharacterName(s *ClientSession, name string) {
	if name == "" {
		return
	}
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	sessionsByName[name] = s
}

func unbindSessionCharacterName(s *ClientSession, name string) {
	if name == "" {
		return
	}
	sessionsMu.Lock()
	defer sessionsMu.Unlock()
	if current, ok := sessionsByName[name]; ok && current == s {
		delete(sessionsByName, name)
	}
}

func findSessionByCharacterName(name string) *ClientSession {
	sessionsMu.RLock()
	defer sessionsMu.RUnlock()
	return sessionsByName[name]
}
