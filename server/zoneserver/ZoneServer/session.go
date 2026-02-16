package main

import (
	"net"
	"time"
)

type ClientSession struct {
	Conn          net.Conn
	Character     *Character
	World         *World
	Position      Position
	Active        bool
	Authenticated bool
	AuthFailures  int
	WindowStart   time.Time
	WindowCount   int
}

func NewSession(conn net.Conn) *ClientSession {
	return &ClientSession{
		Conn:          conn,
		Active:        true,
		Authenticated: false,
	}
}

func (s *ClientSession) allowCommand(now time.Time) bool {
	const (
		perSecondAuthed   = 60
		perSecondUnauthed = 12
	)

	if s.WindowStart.IsZero() || now.Sub(s.WindowStart) >= time.Second {
		s.WindowStart = now
		s.WindowCount = 0
	}
	s.WindowCount++

	limit := perSecondUnauthed
	if s.Authenticated {
		limit = perSecondAuthed
	}
	return s.WindowCount <= limit
}
