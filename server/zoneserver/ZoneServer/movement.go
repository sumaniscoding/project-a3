package main

import "math"

type MoveRequest struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Simple distance check (anti-teleport)
func isMoveValid(from, to Position) bool {
	dx := to.X - from.X
	dy := to.Y - from.Y
	dz := to.Z - from.Z

	distance := math.Sqrt(dx*dx + dy*dy + dz*dz)

	// Max distance per tick (tunable later)
	return distance <= 10.0
}

