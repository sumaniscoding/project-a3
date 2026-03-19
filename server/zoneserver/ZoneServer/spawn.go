package main

type Position struct {
	X float64
	Y float64
	Z float64
}

func DefaultSpawnPosition(worldID WorldID) Position {
	switch worldID {
	case World1:
		return Position{X: 0, Y: 0, Z: 0}
	case World2:
		return Position{X: 0, Y: 0, Z: 0}
	case World3:
		return Position{X: 0, Y: 0, Z: 0}
	default:
		return Position{X: 0, Y: 0, Z: 0}
	}
}

