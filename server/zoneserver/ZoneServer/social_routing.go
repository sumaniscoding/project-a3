package main

import "strings"

func characterGuildName(name string) string {
	character, found, err := findCharacterByName(name)
	if err != nil || !found || character == nil {
		return ""
	}
	return strings.TrimSpace(character.Guild)
}

func findCharacterByName(name string) (*Character, bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false, nil
	}
	if session := findSessionByCharacterName(name); session != nil && session.Character != nil {
		return session.Character, true, nil
	}
	return loadExistingCharacter(name)
}

func reconcileLocalGuildState() {
	forEachSession(func(session *ClientSession) {
		if session == nil || !session.Authenticated || session.Character == nil {
			return
		}
		guild, role := guildMembershipForCharacter(session.Character.Name)
		switch {
		case guild != "":
			session.Character.Guild = guild
			session.Character.GuildRole = role
		case strings.TrimSpace(session.Character.Guild) != "":
			session.Character.Guild = ""
			session.Character.GuildRole = ""
		}
	})
}

func guildMembershipForCharacter(name string) (string, string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", ""
	}

	guildMu.RLock()
	defer guildMu.RUnlock()
	for guildName, guild := range guilds {
		if guild == nil {
			continue
		}
		if role, ok := guild.Members[name]; ok {
			return guildName, canonicalGuildRole(role)
		}
	}
	return "", ""
}
