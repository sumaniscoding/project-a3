package main

type Position struct {
	X float64
	Y float64
	Z float64
}

func DefaultSpawnPosition(worldID WorldID) Position {
	switch worldID {
	case World1:
		return Position{X: 100, Y: 0, Z: 100}
	case World2:
		return Position{X: 500, Y: 0, Z: 500}
	case World3:
		return Position{X: 1000, Y: 0, Z: 1000}
	default:
		return Position{X: 0, Y: 0, Z: 0}
	}
}

