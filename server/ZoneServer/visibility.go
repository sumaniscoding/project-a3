package main

import "math"

const VisibilityRadius = 50.0

func distance(a, b Position) float64 {
	dx := a.X - b.X
	dy := a.Y - b.Y
	dz := a.Z - b.Z
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func isVisible(a, b Position) bool {
	return distance(a, b) <= VisibilityRadius
}

