package main

func skillListForClass(class string) map[string]SkillDefinition {
	list, ok := skillCatalog[class]
	if !ok {
		return map[string]SkillDefinition{}
	}
	return list
}

func learnSkill(c *Character, skillID string) (map[string]interface{}, bool, string) {
	skills := skillListForClass(c.Class)
	def, ok := skills[skillID]
	if !ok {
		return nil, false, "SKILL_NOT_FOUND"
	}
	if c.SkillPoints <= 0 {
		return nil, false, "NO_SKILL_POINTS"
	}
	current := c.Skills[skillID]
	if current >= def.MaxRank {
		return nil, false, "MAX_RANK_REACHED"
	}

	c.Skills[skillID] = current + 1
	c.SkillPoints--
	return map[string]interface{}{
		"skill_id":     skillID,
		"new_rank":     c.Skills[skillID],
		"skill_points": c.SkillPoints,
	}, true, "OK"
}

func skillBonus(c *Character, skillID string) int {
	if skillID == "" {
		return 0
	}
	defs := skillListForClass(c.Class)
	def, ok := defs[skillID]
	if !ok {
		return 0
	}
	rank := c.Skills[skillID]
	if rank == 0 {
		return 0
	}
	return def.BaseBonus * rank
}
