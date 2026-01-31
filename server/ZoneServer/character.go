package main

type Character struct {
	ID        int
	Name      string
	Level     int
	AuraLevel int
	WorldID   WorldID
}

// Temporary mock character for Epic 2
// (DB will replace this later)
func MockCharacter() *Character {
	return &Character{
		ID:        1,
		Name:      "TestArcher",
		Level:     45,
		AuraLevel: 0,
		WorldID:   World1,
	}
}

