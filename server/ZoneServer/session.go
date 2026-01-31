package main

import "net"

type ClientSession struct {
	Conn      net.Conn
	Character *Character
	World     *World
	Position  Position
	Active    bool
}

func NewSession(conn net.Conn) *ClientSession {
	return &ClientSession{
		Conn:   conn,
		Active: true,
	}
}

