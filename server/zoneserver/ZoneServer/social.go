package main

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Party struct {
	ID      string
	Leader  string
	Members map[string]bool
}

var (
	partyMu       sync.RWMutex
	parties       = map[string]*Party{}
	partyByMember = map[string]string{}
	partyInvites  = map[string]string{}
	partySeq      int64

	guildMu     sync.RWMutex
	guildRoster = map[string]map[string]bool{}
)

func sanitizeChatMessage(raw string) string {
	msg := strings.TrimSpace(raw)
	if msg == "" {
		return ""
	}
	if len(msg) > 180 {
		msg = msg[:180]
	}
	return msg
}

func broadcastSay(session *ClientSession, message string) {
	payload := map[string]interface{}{
		"channel": "say",
		"from":    session.Character.Name,
		"world":   session.World.Name,
		"message": message,
		"ts":      time.Now().UTC().Format(time.RFC3339),
	}
	forEachSession(func(other *ClientSession) {
		if !other.Authenticated || other.Character == nil || other.World == nil {
			return
		}
		if other.World.ID != session.World.ID {
			return
		}
		if other != session && !isVisible(session.Position, other.Position) {
			return
		}
		sendMessage(other.Conn, ServerMessage{Command: RespChatMessage, Payload: payload})
	})
}

func broadcastWorld(session *ClientSession, message string) {
	payload := map[string]interface{}{
		"channel": "world",
		"from":    session.Character.Name,
		"world":   session.World.Name,
		"message": message,
		"ts":      time.Now().UTC().Format(time.RFC3339),
	}
	forEachSession(func(other *ClientSession) {
		if !other.Authenticated || other.Character == nil || other.World == nil {
			return
		}
		if other.World.ID != session.World.ID {
			return
		}
		sendMessage(other.Conn, ServerMessage{Command: RespChatMessage, Payload: payload})
	})
}

func whoPayload() map[string]interface{} {
	list := make([]map[string]interface{}, 0)
	forEachSession(func(s *ClientSession) {
		if !s.Authenticated || s.Character == nil || s.World == nil {
			return
		}
		list = append(list, map[string]interface{}{
			"name":  s.Character.Name,
			"class": s.Character.Class,
			"level": s.Character.Level,
			"world": s.World.Name,
			"guild": s.Character.Guild,
		})
	})
	sort.Slice(list, func(i, j int) bool {
		return toString(list[i], "name") < toString(list[j], "name")
	})
	return map[string]interface{}{"online": list, "count": len(list)}
}

func partyInvite(inviter, target string) (map[string]interface{}, bool, string) {
	if inviter == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if inviter == target {
		return nil, false, "INVALID_TARGET"
	}

	partyMu.Lock()
	defer partyMu.Unlock()

	if _, ok := partyByMember[target]; ok {
		return nil, false, "TARGET_ALREADY_IN_PARTY"
	}
	if pid, ok := partyByMember[inviter]; ok {
		p := parties[pid]
		if p == nil || p.Leader != inviter {
			return nil, false, "NOT_PARTY_LEADER"
		}
	}
	partyInvites[target] = inviter
	return map[string]interface{}{"from": inviter, "to": target}, true, "OK"
}

func partyAccept(target, from string) (map[string]interface{}, bool, string) {
	partyMu.Lock()
	defer partyMu.Unlock()

	inviter, ok := partyInvites[target]
	if !ok {
		return nil, false, "NO_INVITE"
	}
	if from != "" && inviter != from {
		return nil, false, "INVITE_MISMATCH"
	}
	if _, inParty := partyByMember[target]; inParty {
		return nil, false, "ALREADY_IN_PARTY"
	}

	pid, inParty := partyByMember[inviter]
	var p *Party
	if !inParty {
		partySeq++
		pid = fmt.Sprintf("party_%d", partySeq)
		p = &Party{
			ID:      pid,
			Leader:  inviter,
			Members: map[string]bool{inviter: true},
		}
		parties[pid] = p
		partyByMember[inviter] = pid
	} else {
		p = parties[pid]
		if p == nil {
			return nil, false, "PARTY_NOT_FOUND"
		}
	}

	p.Members[target] = true
	partyByMember[target] = pid
	delete(partyInvites, target)
	return map[string]interface{}{"party": partySnapshotLocked(p)}, true, "OK"
}

func partyLeave(member string) (map[string]interface{}, bool, string) {
	partyMu.Lock()
	defer partyMu.Unlock()

	pid, ok := partyByMember[member]
	if !ok {
		return nil, false, "NOT_IN_PARTY"
	}
	p := parties[pid]
	if p == nil {
		delete(partyByMember, member)
		return nil, false, "PARTY_NOT_FOUND"
	}

	delete(p.Members, member)
	delete(partyByMember, member)
	if p.Leader == member {
		p.Leader = firstMemberLocked(p.Members)
	}

	if len(p.Members) < 2 {
		for name := range p.Members {
			delete(partyByMember, name)
		}
		delete(parties, p.ID)
		return map[string]interface{}{"party_id": pid, "dissolved": true}, true, "OK"
	}

	return map[string]interface{}{"party": partySnapshotLocked(p), "dissolved": false}, true, "OK"
}

func partySnapshotForCharacter(name string) map[string]interface{} {
	partyMu.RLock()
	defer partyMu.RUnlock()
	pid, ok := partyByMember[name]
	if !ok {
		return nil
	}
	p := parties[pid]
	if p == nil {
		return nil
	}
	return partySnapshotLocked(p)
}

func partyMemberNames(partyID string) []string {
	partyMu.RLock()
	defer partyMu.RUnlock()
	p, ok := parties[partyID]
	if !ok || p == nil {
		return nil
	}
	return sortedMembersLocked(p.Members)
}

func firstMemberLocked(members map[string]bool) string {
	names := sortedMembersLocked(members)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func sortedMembersLocked(members map[string]bool) []string {
	names := make([]string, 0, len(members))
	for name := range members {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func partySnapshotLocked(p *Party) map[string]interface{} {
	if p == nil {
		return nil
	}
	return map[string]interface{}{
		"id":      p.ID,
		"leader":  p.Leader,
		"members": sortedMembersLocked(p.Members),
		"size":    len(p.Members),
	}
}

func clearPartyInvitesFor(name string) {
	partyMu.Lock()
	defer partyMu.Unlock()
	delete(partyInvites, name)
	for invitee, inviter := range partyInvites {
		if inviter == name {
			delete(partyInvites, invitee)
		}
	}
}

func handleSocialDisconnect(name string) {
	clearPartyInvitesFor(name)
	result, ok, _ := partyLeave(name)
	if !ok {
		return
	}
	if dissolved, _ := result["dissolved"].(bool); dissolved {
		return
	}
	partyMap := toMap(result["party"])
	partyID := toString(partyMap, "id")
	notifyPartyMembers(partyID, "MEMBER_DISCONNECTED", name)
}

func registerGuildMember(guildName, member string) {
	guild := strings.TrimSpace(guildName)
	if guild == "" || strings.TrimSpace(member) == "" {
		return
	}
	guildMu.Lock()
	defer guildMu.Unlock()
	members := guildRoster[guild]
	if members == nil {
		members = map[string]bool{}
		guildRoster[guild] = members
	}
	members[member] = true
}

func guildCreate(member, guildName string) (map[string]interface{}, bool, string) {
	guild := strings.TrimSpace(guildName)
	if guild == "" {
		return nil, false, "GUILD_NAME_REQUIRED"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	if _, exists := guildRoster[guild]; exists {
		return nil, false, "GUILD_EXISTS"
	}
	guildRoster[guild] = map[string]bool{member: true}
	return map[string]interface{}{"guild": guild, "member": member}, true, "OK"
}

func guildJoin(member, guildName string) (map[string]interface{}, bool, string) {
	guild := strings.TrimSpace(guildName)
	if guild == "" {
		return nil, false, "GUILD_NAME_REQUIRED"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	members, exists := guildRoster[guild]
	if !exists {
		return nil, false, "GUILD_NOT_FOUND"
	}
	members[member] = true
	return map[string]interface{}{"guild": guild, "member": member}, true, "OK"
}

func guildLeave(member, guildName string) (map[string]interface{}, bool, string) {
	guild := strings.TrimSpace(guildName)
	if guild == "" {
		return nil, false, "NOT_IN_GUILD"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	members, exists := guildRoster[guild]
	if !exists {
		return nil, false, "GUILD_NOT_FOUND"
	}
	delete(members, member)
	if len(members) == 0 {
		delete(guildRoster, guild)
	}
	return map[string]interface{}{"guild": guild, "member": member}, true, "OK"
}

func guildListPayload() map[string]interface{} {
	guildMu.RLock()
	defer guildMu.RUnlock()

	names := make([]string, 0, len(guildRoster))
	for name := range guildRoster {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		members := guildRoster[name]
		out = append(out, map[string]interface{}{
			"name":         name,
			"member_count": len(members),
		})
	}
	return map[string]interface{}{"guilds": out, "count": len(out)}
}
