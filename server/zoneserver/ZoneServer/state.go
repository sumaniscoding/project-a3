package main

func statePayload(s *ClientSession) map[string]interface{} {
	c := s.Character
	return map[string]interface{}{
		"name":         c.Name,
		"class":        c.Class,
		"level":        c.Level,
		"xp":           c.XP,
		"xp_debt":      c.XPDebt,
		"hp":           c.HP,
		"max_hp":       c.MaxHP,
		"aura_level":   c.AuraLevel,
		"world":        s.World.Name,
		"position":     s.Position,
		"trust":        c.Trust,
		"quests":       c.Quests,
		"pet":          c.Pet,
		"mercenary":    c.Mercenary,
		"elemental":    c.Elemental,
		"skill_points": c.SkillPoints,
		"skills":       c.Skills,
		"pk_score":     c.PKScore,
		"honor":        c.Honor,
		"inventory":    c.Inventory,
		"materials":    c.Materials,
		"equipped":     c.Equipped,
		"corpse":       c.Corpse,
		"history":      getUnlockHistoryPayload(),
		"guild":        c.Guild,
		"party":        partySnapshotForCharacter(c.Name),
	}
}
