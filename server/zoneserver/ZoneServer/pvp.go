package main

func applyPvPPenalty(attacker *Character, victimLevel int) map[string]interface{} {
	levelDiff := attacker.Level - victimLevel
	penalty := map[string]interface{}{
		"level_diff": levelDiff,
		"xp_debt":    0,
		"pk_gain":    0,
		"honor_loss": 0,
	}

	if levelDiff >= 10 {
		debt := 80 + levelDiff*5
		attacker.XPDebt += debt
		attacker.PKScore += 3
		attacker.Honor -= 20
		penalty["xp_debt"] = debt
		penalty["pk_gain"] = 3
		penalty["honor_loss"] = 20
		return penalty
	}
	if levelDiff >= 5 {
		debt := 35 + levelDiff*4
		attacker.XPDebt += debt
		attacker.PKScore += 2
		attacker.Honor -= 10
		penalty["xp_debt"] = debt
		penalty["pk_gain"] = 2
		penalty["honor_loss"] = 10
		return penalty
	}
	if levelDiff >= 2 {
		debt := 20 + levelDiff*2
		attacker.XPDebt += debt
		attacker.PKScore++
		attacker.Honor -= 4
		penalty["xp_debt"] = debt
		penalty["pk_gain"] = 1
		penalty["honor_loss"] = 4
		return penalty
	}

	// Near-equal level fight: low penalty and slight honor gain.
	attacker.Honor += 2
	penalty["honor_gain"] = 2
	return penalty
}

func attackPlayer(attacker *ClientSession, victim *ClientSession, skillID string) map[string]interface{} {
	victimLevel := victim.Character.Level
	damage, attackerDied := calculateAttack(attacker.Character, victimLevel)
	damage += skillBonus(attacker.Character, skillID)
	if damage < 1 {
		damage = 1
	}

	penalty := applyPvPPenalty(attacker.Character, victimLevel)

	// Apply direct damage to victim and death penalty flow.
	victim.Character.HP -= damage
	victimDied := victim.Character.HP <= 0
	if victimDied {
		applyDeathPenalty(victim.Character, victim.Position)
		victim.Character.HP = victim.Character.MaxHP / 2
		if victim.Character.HP < 1 {
			victim.Character.HP = 1
		}
	}

	if attackerDied {
		applyDeathPenalty(attacker.Character, attacker.Position)
	}

	return map[string]interface{}{
		"target":        victim.Character.Name,
		"target_level":  victimLevel,
		"damage":        damage,
		"skill_id":      skillID,
		"attacker_died": attackerDied,
		"target_died":   victimDied,
		"penalty":       penalty,
		"attacker_state": map[string]interface{}{
			"xp_debt":  attacker.Character.XPDebt,
			"pk_score": attacker.Character.PKScore,
			"honor":    attacker.Character.Honor,
		},
		"target_state": map[string]interface{}{
			"hp":      victim.Character.HP,
			"xp_debt": victim.Character.XPDebt,
		},
	}
}
