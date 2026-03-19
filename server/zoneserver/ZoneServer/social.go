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
	Ready   map[string]bool
}

type PartyInvite struct {
	From      string
	ExpiresAt time.Time
}

type Guild struct {
	Name    string
	Leader  string
	Members map[string]string // member name -> role
}

type GuildInvite struct {
	GuildName string
	From      string
	ExpiresAt time.Time
}

type FriendInvite struct {
	From      string
	ExpiresAt time.Time
}

var (
	partyMu       sync.RWMutex
	parties       = map[string]*Party{}
	partyByMember = map[string]string{}
	partyInvites  = map[string]PartyInvite{}
	partySeq      int64

	guildMu      sync.RWMutex
	guilds       = map[string]*Guild{}
	guildInvites = map[string]GuildInvite{}

	friendMu      sync.RWMutex
	friendInvites = map[string]FriendInvite{}
)

const partyInviteTTL = 60 * time.Second
const guildInviteTTL = 120 * time.Second
const friendInviteTTL = 120 * time.Second

const (
	presenceOnline  = "online"
	presenceAFK     = "afk"
	presenceDND     = "dnd"
	presenceOffline = "offline"
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

func parsePresenceStatus(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case presenceOnline:
		return presenceOnline, true
	case presenceAFK:
		return presenceAFK, true
	case presenceDND:
		return presenceDND, true
	default:
		return "", false
	}
}

func canonicalPresenceStatus(raw string) string {
	if status, ok := parsePresenceStatus(raw); ok {
		return status
	}
	return presenceOnline
}

func broadcastSay(session *ClientSession, message string) {
	PublishRedisEvent(EvtChatSay, ChatSayPayload{
		From:    session.Character.Name,
		WorldID: int(session.World.ID),
		World:   session.World.Name,
		Message: message,
		Ts:      time.Now().UTC().Format(time.RFC3339),
		X:       session.Position.X,
		Y:       session.Position.Y,
		Z:       session.Position.Z,
	})
}

func broadcastWorld(session *ClientSession, message string) {
	PublishRedisEvent(EvtChatWorld, ChatWorldPayload{
		From:    session.Character.Name,
		WorldID: int(session.World.ID),
		World:   session.World.Name,
		Message: message,
		Ts:      time.Now().UTC().Format(time.RFC3339),
	})
}

func broadcastWhisper(session *ClientSession, targetName, message string) (bool, string) {
	if session == nil || session.Character == nil {
		return false, "SENDER_REQUIRED"
	}
	targetName = strings.TrimSpace(targetName)
	if targetName == "" {
		return false, "TARGET_REQUIRED"
	}
	if targetName == session.Character.Name {
		return false, "INVALID_TARGET"
	}

	if !isCharacterOnlineAnywhere(targetName) {
		return false, "TARGET_OFFLINE"
	}
	if isBlocked(targetName, session.Character.Name) {
		return false, "TARGET_BLOCKED_YOU"
	}
	if isBlocked(session.Character.Name, targetName) {
		return false, "TARGET_BLOCKED_BY_YOU"
	}

	PublishRedisEvent(EvtChatWhisper, ChatWhisperPayload{
		From:    session.Character.Name,
		To:      targetName,
		Message: message,
		Ts:      time.Now().UTC().Format(time.RFC3339),
	})
	return true, "OK"
}

func broadcastGuild(session *ClientSession, message string) bool {
	guild := strings.TrimSpace(session.Character.Guild)
	if guild == "" {
		return false
	}
	PublishRedisEvent(EvtChatGuild, ChatGuildPayload{
		From:    session.Character.Name,
		Guild:   guild,
		Message: message,
		Ts:      time.Now().UTC().Format(time.RFC3339),
	})
	return true
}

func whoPayload() map[string]interface{} {
	list := make([]map[string]interface{}, 0)
	forEachSession(func(s *ClientSession) {
		if !s.Authenticated || s.Character == nil || s.World == nil {
			return
		}
		list = append(list, map[string]interface{}{
			"name":     s.Character.Name,
			"class":    s.Character.Class,
			"level":    s.Character.Level,
			"world":    s.World.Name,
			"guild":    s.Character.Guild,
			"presence": canonicalPresenceStatus(s.Character.Presence),
		})
	})
	sort.Slice(list, func(i, j int) bool {
		return toString(list[i], "name") < toString(list[j], "name")
	})
	return map[string]interface{}{"online": list, "count": len(list)}
}

func friendNamesForCharacter(c *Character) []string {
	if c == nil {
		return nil
	}
	if c.Friends == nil {
		c.Friends = map[string]bool{}
	}
	names := make([]string, 0, len(c.Friends))
	for name, isFriend := range c.Friends {
		if !isFriend {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func blockNamesForCharacter(c *Character) []string {
	if c == nil {
		return nil
	}
	if c.Blocks == nil {
		c.Blocks = map[string]bool{}
	}
	names := make([]string, 0, len(c.Blocks))
	for name, blocked := range c.Blocks {
		if !blocked {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func blockListPayload(c *Character) map[string]interface{} {
	names := blockNamesForCharacter(c)
	return map[string]interface{}{
		"blocked": names,
		"count":   len(names),
	}
}

func isBlocked(blocker, target string) bool {
	blocker = strings.TrimSpace(blocker)
	target = strings.TrimSpace(target)
	if blocker == "" || target == "" {
		return false
	}
	blockerSession := findSessionByCharacterName(blocker)
	if blockerSession != nil && blockerSession.Character != nil {
		if blockerSession.Character.Blocks == nil {
			blockerSession.Character.Blocks = map[string]bool{}
		}
		return blockerSession.Character.Blocks[target]
	}
	blockerCharacter, found, err := loadExistingCharacter(blocker)
	if err != nil || !found || blockerCharacter == nil {
		return false
	}
	if blockerCharacter.Blocks == nil {
		blockerCharacter.Blocks = map[string]bool{}
	}
	return blockerCharacter.Blocks[target]
}

func friendListPayload(c *Character) map[string]interface{} {
	names := friendNamesForCharacter(c)
	return map[string]interface{}{
		"friends": names,
		"count":   len(names),
	}
}

func friendStatusPayload(c *Character) map[string]interface{} {
	names := friendNamesForCharacter(c)
	entries := make([]map[string]interface{}, 0, len(names))
	onlineCount := 0
	for _, name := range names {
		entry := map[string]interface{}{
			"name":   name,
			"online": false,
			"status": presenceOffline,
		}
		if s := findSessionByCharacterName(name); s != nil && s.Authenticated && s.Character != nil && s.World != nil {
			entry["online"] = true
			entry["world"] = s.World.Name
			entry["guild"] = s.Character.Guild
			entry["level"] = s.Character.Level
			entry["status"] = canonicalPresenceStatus(s.Character.Presence)
			onlineCount++
		}
		entries = append(entries, entry)
	}
	return map[string]interface{}{
		"friends":      names,
		"entries":      entries,
		"count":        len(names),
		"online_count": onlineCount,
	}
}

func friendRequest(sender, target string) (map[string]interface{}, bool, string) {
	sender = strings.TrimSpace(sender)
	target = strings.TrimSpace(target)
	if sender == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if sender == target {
		return nil, false, "INVALID_TARGET"
	}

	senderSession := findSessionByCharacterName(sender)
	targetSession := findSessionByCharacterName(target)
	if senderSession == nil || senderSession.Character == nil {
		return nil, false, "SENDER_REQUIRED"
	}
	if targetSession == nil || !targetSession.Authenticated || targetSession.Character == nil {
		return nil, false, "TARGET_OFFLINE"
	}
	if isBlocked(sender, target) || isBlocked(target, sender) {
		return nil, false, "BLOCKED"
	}

	if senderSession.Character.Friends == nil {
		senderSession.Character.Friends = map[string]bool{}
	}
	if senderSession.Character.Friends[target] {
		return nil, false, "ALREADY_FRIENDS"
	}

	friendMu.Lock()
	defer friendMu.Unlock()
	if invite, ok := friendInvites[target]; ok {
		if time.Now().UTC().After(invite.ExpiresAt) {
			delete(friendInvites, target)
		} else if invite.From == sender {
			return nil, false, "INVITE_ALREADY_SENT"
		} else {
			return nil, false, "INVITE_PENDING"
		}
	}
	friendInvites[target] = FriendInvite{
		From:      sender,
		ExpiresAt: time.Now().UTC().Add(friendInviteTTL),
	}
	return map[string]interface{}{
		"from":        sender,
		"to":          target,
		"expires_sec": int(friendInviteTTL.Seconds()),
	}, true, "OK"
}

func friendCancelRequest(sender, target string) (map[string]interface{}, bool, string) {
	sender = strings.TrimSpace(sender)
	target = strings.TrimSpace(target)
	if sender == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if sender == target {
		return nil, false, "INVALID_TARGET"
	}

	friendMu.Lock()
	defer friendMu.Unlock()
	invite, ok := friendInvites[target]
	if !ok {
		return nil, false, "NO_INVITE"
	}
	if time.Now().UTC().After(invite.ExpiresAt) {
		delete(friendInvites, target)
		return nil, false, "INVITE_EXPIRED"
	}
	if invite.From != sender {
		return nil, false, "NOT_INVITER"
	}
	delete(friendInvites, target)
	return map[string]interface{}{
		"from": sender,
		"to":   target,
	}, true, "OK"
}

func friendAccept(target, from string) (map[string]interface{}, bool, string) {
	target = strings.TrimSpace(target)
	from = strings.TrimSpace(from)
	if target == "" {
		return nil, false, "MEMBER_REQUIRED"
	}

	targetSession := findSessionByCharacterName(target)
	if targetSession == nil || !targetSession.Authenticated || targetSession.Character == nil {
		return nil, false, "TARGET_OFFLINE"
	}

	friendMu.Lock()
	invite, ok := friendInvites[target]
	if !ok {
		friendMu.Unlock()
		return nil, false, "NO_INVITE"
	}
	if time.Now().UTC().After(invite.ExpiresAt) {
		delete(friendInvites, target)
		friendMu.Unlock()
		return nil, false, "INVITE_EXPIRED"
	}
	if from != "" && invite.From != from {
		friendMu.Unlock()
		return nil, false, "INVITE_MISMATCH"
	}
	delete(friendInvites, target)
	friendMu.Unlock()

	inviterSession := findSessionByCharacterName(invite.From)
	if inviterSession == nil || !inviterSession.Authenticated || inviterSession.Character == nil {
		return nil, false, "INVITER_OFFLINE"
	}
	if isBlocked(invite.From, target) || isBlocked(target, invite.From) {
		return nil, false, "BLOCKED"
	}
	if targetSession.Character.Friends == nil {
		targetSession.Character.Friends = map[string]bool{}
	}
	if inviterSession.Character.Friends == nil {
		inviterSession.Character.Friends = map[string]bool{}
	}
	if targetSession.Character.Friends[invite.From] && inviterSession.Character.Friends[target] {
		return nil, false, "ALREADY_FRIENDS"
	}

	targetSession.Character.Friends[invite.From] = true
	inviterSession.Character.Friends[target] = true
	return map[string]interface{}{
		"member":  target,
		"friend":  invite.From,
		"friends": friendNamesForCharacter(targetSession.Character),
	}, true, "OK"
}

func friendDecline(target, from string) (map[string]interface{}, bool, string) {
	target = strings.TrimSpace(target)
	from = strings.TrimSpace(from)
	if target == "" {
		return nil, false, "MEMBER_REQUIRED"
	}

	friendMu.Lock()
	defer friendMu.Unlock()
	invite, ok := friendInvites[target]
	if !ok {
		return nil, false, "NO_INVITE"
	}
	if time.Now().UTC().After(invite.ExpiresAt) {
		delete(friendInvites, target)
		return nil, false, "INVITE_EXPIRED"
	}
	if from != "" && invite.From != from {
		return nil, false, "INVITE_MISMATCH"
	}
	delete(friendInvites, target)
	return map[string]interface{}{
		"from": invite.From,
		"to":   target,
	}, true, "OK"
}

func friendRemove(member, target string) (map[string]interface{}, bool, string) {
	member = strings.TrimSpace(member)
	target = strings.TrimSpace(target)
	if member == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if member == target {
		return nil, false, "INVALID_TARGET"
	}

	memberSession := findSessionByCharacterName(member)
	if memberSession == nil || memberSession.Character == nil {
		return nil, false, "MEMBER_REQUIRED"
	}
	if memberSession.Character.Friends == nil {
		memberSession.Character.Friends = map[string]bool{}
	}
	if !memberSession.Character.Friends[target] {
		return nil, false, "NOT_FRIENDS"
	}

	delete(memberSession.Character.Friends, target)
	targetUpdated := false
	if targetSession := findSessionByCharacterName(target); targetSession != nil && targetSession.Authenticated && targetSession.Character != nil {
		if targetSession.Character.Friends == nil {
			targetSession.Character.Friends = map[string]bool{}
		}
		if targetSession.Character.Friends[member] {
			delete(targetSession.Character.Friends, member)
			targetUpdated = true
		}
	}

	friendMu.Lock()
	if invite, ok := friendInvites[member]; ok && invite.From == target {
		delete(friendInvites, member)
	}
	if invite, ok := friendInvites[target]; ok && invite.From == member {
		delete(friendInvites, target)
	}
	friendMu.Unlock()

	return map[string]interface{}{
		"member":         member,
		"target":         target,
		"friends":        friendNamesForCharacter(memberSession.Character),
		"target_updated": targetUpdated,
	}, true, "OK"
}

func blockPlayer(member, target string) (map[string]interface{}, bool, string) {
	member = strings.TrimSpace(member)
	target = strings.TrimSpace(target)
	if member == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if member == target {
		return nil, false, "INVALID_TARGET"
	}

	memberSession := findSessionByCharacterName(member)
	if memberSession == nil || memberSession.Character == nil {
		return nil, false, "MEMBER_REQUIRED"
	}
	if memberSession.Character.Blocks == nil {
		memberSession.Character.Blocks = map[string]bool{}
	}
	if memberSession.Character.Blocks[target] {
		return nil, false, "ALREADY_BLOCKED"
	}
	memberSession.Character.Blocks[target] = true

	if memberSession.Character.Friends == nil {
		memberSession.Character.Friends = map[string]bool{}
	}
	friendRemoved := false
	if memberSession.Character.Friends[target] {
		delete(memberSession.Character.Friends, target)
		friendRemoved = true
	}

	targetUpdated := false
	if targetSession := findSessionByCharacterName(target); targetSession != nil && targetSession.Authenticated && targetSession.Character != nil {
		if targetSession.Character.Friends == nil {
			targetSession.Character.Friends = map[string]bool{}
		}
		if targetSession.Character.Friends[member] {
			delete(targetSession.Character.Friends, member)
			targetUpdated = true
		}
	}

	friendMu.Lock()
	if invite, ok := friendInvites[member]; ok && invite.From == target {
		delete(friendInvites, member)
	}
	if invite, ok := friendInvites[target]; ok && invite.From == member {
		delete(friendInvites, target)
	}
	friendMu.Unlock()

	return map[string]interface{}{
		"member":         member,
		"target":         target,
		"blocked":        blockNamesForCharacter(memberSession.Character),
		"friend_removed": friendRemoved,
		"target_updated": targetUpdated,
	}, true, "OK"
}

func unblockPlayer(member, target string) (map[string]interface{}, bool, string) {
	member = strings.TrimSpace(member)
	target = strings.TrimSpace(target)
	if member == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if member == target {
		return nil, false, "INVALID_TARGET"
	}

	memberSession := findSessionByCharacterName(member)
	if memberSession == nil || memberSession.Character == nil {
		return nil, false, "MEMBER_REQUIRED"
	}
	if memberSession.Character.Blocks == nil {
		memberSession.Character.Blocks = map[string]bool{}
	}
	if !memberSession.Character.Blocks[target] {
		return nil, false, "NOT_BLOCKED"
	}
	delete(memberSession.Character.Blocks, target)

	return map[string]interface{}{
		"member":  member,
		"target":  target,
		"blocked": blockNamesForCharacter(memberSession.Character),
	}, true, "OK"
}

func partyInvite(inviter, target string) (map[string]interface{}, bool, string) {
	if inviter == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if inviter == target {
		return nil, false, "INVALID_TARGET"
	}
	if isBlocked(inviter, target) || isBlocked(target, inviter) {
		return nil, false, "BLOCKED"
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
	invite := PartyInvite{
		From:      inviter,
		ExpiresAt: time.Now().UTC().Add(partyInviteTTL),
	}
	partyInvites[target] = invite
	BroadcastSocialSync(SocialSyncPayload{
		Action:      "PARTY_INVITE_SET",
		Target:      target,
		PartyInvite: &invite,
	})
	return map[string]interface{}{"from": inviter, "to": target}, true, "OK"
}

func partyAccept(target, from string) (map[string]interface{}, bool, string) {
	partyMu.Lock()
	defer partyMu.Unlock()

	invite, ok := partyInvites[target]
	if !ok {
		return nil, false, "NO_INVITE"
	}
	if time.Now().UTC().After(invite.ExpiresAt) {
		delete(partyInvites, target)
		BroadcastSocialSync(SocialSyncPayload{Action: "PARTY_INVITE_CLEAR", Target: target})
		return nil, false, "INVITE_EXPIRED"
	}
	inviter := invite.From
	if from != "" && inviter != from {
		return nil, false, "INVITE_MISMATCH"
	}
	if isBlocked(inviter, target) || isBlocked(target, inviter) {
		delete(partyInvites, target)
		BroadcastSocialSync(SocialSyncPayload{Action: "PARTY_INVITE_CLEAR", Target: target})
		return nil, false, "BLOCKED"
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
			Ready:   map[string]bool{inviter: false},
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
	p.Ready[target] = false
	partyByMember[target] = pid
	delete(partyInvites, target)
	BroadcastSocialSync(SocialSyncPayload{Action: "PARTY_INVITE_CLEAR", Target: target})

	snap := partySnapshotLocked(p)
	BroadcastSocialSync(SocialSyncPayload{
		Action: "PARTY_UPDATE",
		Party:  p,
	})

	return map[string]interface{}{"party": snap}, true, "OK"
}

func partyCancelInvite(inviter, target string) (map[string]interface{}, bool, string) {
	inviter = strings.TrimSpace(inviter)
	target = strings.TrimSpace(target)
	if inviter == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if inviter == target {
		return nil, false, "INVALID_TARGET"
	}

	partyMu.Lock()
	defer partyMu.Unlock()

	invite, ok := partyInvites[target]
	if !ok {
		return nil, false, "NO_INVITE"
	}
	if time.Now().UTC().After(invite.ExpiresAt) {
		delete(partyInvites, target)
		BroadcastSocialSync(SocialSyncPayload{Action: "PARTY_INVITE_CLEAR", Target: target})
		return nil, false, "INVITE_EXPIRED"
	}
	if invite.From != inviter {
		return nil, false, "NOT_INVITER"
	}
	delete(partyInvites, target)
	BroadcastSocialSync(SocialSyncPayload{Action: "PARTY_INVITE_CLEAR", Target: target})
	return map[string]interface{}{
		"from": inviter,
		"to":   target,
	}, true, "OK"
}

func partyDecline(target, from string) (map[string]interface{}, bool, string) {
	target = strings.TrimSpace(target)
	from = strings.TrimSpace(from)
	if target == "" {
		return nil, false, "TARGET_REQUIRED"
	}

	partyMu.Lock()
	defer partyMu.Unlock()

	invite, ok := partyInvites[target]
	if !ok {
		return nil, false, "NO_INVITE"
	}
	if time.Now().UTC().After(invite.ExpiresAt) {
		delete(partyInvites, target)
		BroadcastSocialSync(SocialSyncPayload{Action: "PARTY_INVITE_CLEAR", Target: target})
		return nil, false, "INVITE_EXPIRED"
	}
	if from != "" && invite.From != from {
		return nil, false, "INVITE_MISMATCH"
	}
	delete(partyInvites, target)
	BroadcastSocialSync(SocialSyncPayload{Action: "PARTY_INVITE_CLEAR", Target: target})
	return map[string]interface{}{
		"from": invite.From,
		"to":   target,
	}, true, "OK"
}

func partyKick(leader, target string) (map[string]interface{}, bool, string) {
	if strings.TrimSpace(target) == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if leader == target {
		return nil, false, "INVALID_TARGET"
	}

	partyMu.Lock()
	defer partyMu.Unlock()

	pid, ok := partyByMember[leader]
	if !ok {
		return nil, false, "NOT_IN_PARTY"
	}
	p := parties[pid]
	if p == nil {
		return nil, false, "PARTY_NOT_FOUND"
	}
	if p.Leader != leader {
		return nil, false, "NOT_PARTY_LEADER"
	}
	if _, exists := p.Members[target]; !exists {
		return nil, false, "TARGET_NOT_IN_PARTY"
	}

	delete(p.Members, target)
	delete(p.Ready, target)
	delete(partyByMember, target)

	if len(p.Members) < 2 {
		remaining := sortedMembersLocked(p.Members)
		for name := range p.Members {
			delete(partyByMember, name)
		}
		delete(parties, p.ID)

		BroadcastSocialSync(SocialSyncPayload{
			Action:   "PARTY_DISBAND",
			Party:    p,
			Removals: append(remaining, target),
		})

		return map[string]interface{}{"party_id": pid, "target": target, "dissolved": true, "remaining": remaining}, true, "OK"
	}

	snap := partySnapshotLocked(p)
	BroadcastSocialSync(SocialSyncPayload{
		Action:   "PARTY_UPDATE",
		Party:    p,
		Removals: []string{target},
	})

	return map[string]interface{}{"party": snap, "target": target, "dissolved": false}, true, "OK"
}

func partyTransferLeader(actor, target string) (map[string]interface{}, bool, string) {
	actor = strings.TrimSpace(actor)
	target = strings.TrimSpace(target)
	if actor == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if actor == target {
		return nil, false, "INVALID_TARGET"
	}

	partyMu.Lock()
	defer partyMu.Unlock()

	pid, ok := partyByMember[actor]
	if !ok {
		return nil, false, "NOT_IN_PARTY"
	}
	p := parties[pid]
	if p == nil {
		return nil, false, "PARTY_NOT_FOUND"
	}
	if p.Leader != actor {
		return nil, false, "NOT_PARTY_LEADER"
	}
	if _, exists := p.Members[target]; !exists {
		return nil, false, "TARGET_NOT_IN_PARTY"
	}

	p.Leader = target

	snap := partySnapshotLocked(p)
	BroadcastSocialSync(SocialSyncPayload{
		Action: "PARTY_UPDATE",
		Party:  p,
	})

	return map[string]interface{}{
		"party":      snap,
		"from":       actor,
		"to":         target,
		"new_leader": target,
	}, true, "OK"
}

func partyDisband(leader string) (map[string]interface{}, bool, string) {
	leader = strings.TrimSpace(leader)
	if leader == "" {
		return nil, false, "LEADER_REQUIRED"
	}

	partyMu.Lock()
	defer partyMu.Unlock()

	pid, ok := partyByMember[leader]
	if !ok {
		return nil, false, "NOT_IN_PARTY"
	}
	p := parties[pid]
	if p == nil {
		return nil, false, "PARTY_NOT_FOUND"
	}
	if p.Leader != leader {
		return nil, false, "NOT_PARTY_LEADER"
	}

	members := sortedMembersLocked(p.Members)
	for _, name := range members {
		delete(partyByMember, name)
	}
	delete(parties, pid)

	BroadcastSocialSync(SocialSyncPayload{
		Action:   "PARTY_DISBAND",
		Party:    p,
		Removals: members,
	})

	return map[string]interface{}{
		"party_id":  pid,
		"leader":    leader,
		"members":   members,
		"disbanded": true,
	}, true, "OK"
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
	delete(p.Ready, member)
	delete(partyByMember, member)
	if p.Leader == member {
		p.Leader = firstMemberLocked(p.Members)
	}

	if len(p.Members) < 2 {
		remaining := sortedMembersLocked(p.Members)
		for name := range p.Members {
			delete(partyByMember, name)
		}
		delete(parties, p.ID)

		BroadcastSocialSync(SocialSyncPayload{
			Action:   "PARTY_DISBAND",
			Party:    p,
			Removals: append(remaining, member),
		})

		return map[string]interface{}{"party_id": pid, "dissolved": true, "remaining": remaining}, true, "OK"
	}

	snap := partySnapshotLocked(p)
	BroadcastSocialSync(SocialSyncPayload{
		Action:   "PARTY_UPDATE",
		Party:    p,
		Removals: []string{member},
	})

	return map[string]interface{}{"party": snap, "dissolved": false}, true, "OK"
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

func arePartyMates(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	partyMu.RLock()
	defer partyMu.RUnlock()
	pa, okA := partyByMember[a]
	pb, okB := partyByMember[b]
	return okA && okB && pa != "" && pa == pb
}

func partyNearbyMembers(session *ClientSession) []*ClientSession {
	if session == nil || session.Character == nil || session.World == nil {
		return nil
	}
	partyMu.RLock()
	partyID := partyByMember[session.Character.Name]
	if partyID == "" {
		partyMu.RUnlock()
		return nil
	}
	p := parties[partyID]
	if p == nil {
		partyMu.RUnlock()
		return nil
	}
	memberNames := sortedMembersLocked(p.Members)
	partyMu.RUnlock()

	out := make([]*ClientSession, 0)
	for _, name := range memberNames {
		if name == session.Character.Name {
			continue
		}
		other := findSessionByCharacterName(name)
		if other == nil || !other.Authenticated || other.Character == nil || other.World == nil {
			continue
		}
		if other.World.ID != session.World.ID {
			continue
		}
		if !isVisible(session.Position, other.Position) {
			continue
		}
		out = append(out, other)
	}
	return out
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
		"ready":   readySnapshotLocked(p),
	}
}

func readySnapshotLocked(p *Party) map[string]interface{} {
	states := map[string]bool{}
	readyCount := 0
	for _, member := range sortedMembersLocked(p.Members) {
		isReady := p.Ready[member]
		states[member] = isReady
		if isReady {
			readyCount++
		}
	}
	return map[string]interface{}{
		"states":      states,
		"ready_count": readyCount,
		"all_ready":   len(states) > 0 && readyCount == len(states),
	}
}

func setPartyReady(member string, ready bool) (map[string]interface{}, bool, string) {
	partyMu.Lock()
	defer partyMu.Unlock()

	pid, ok := partyByMember[member]
	if !ok {
		return nil, false, "NOT_IN_PARTY"
	}
	p := parties[pid]
	if p == nil {
		return nil, false, "PARTY_NOT_FOUND"
	}
	if _, exists := p.Members[member]; !exists {
		return nil, false, "NOT_IN_PARTY"
	}
	if p.Ready == nil {
		p.Ready = map[string]bool{}
	}
	p.Ready[member] = ready

	snap := partySnapshotLocked(p)
	BroadcastSocialSync(SocialSyncPayload{
		Action: "PARTY_UPDATE",
		Party:  p,
	})

	return map[string]interface{}{
		"party": snap,
		"actor": member,
		"ready": ready,
	}, true, "OK"
}

func partyStatusForMember(member string) (map[string]interface{}, bool, string) {
	partyMu.RLock()
	defer partyMu.RUnlock()
	pid, ok := partyByMember[member]
	if !ok {
		return nil, false, "NOT_IN_PARTY"
	}
	p := parties[pid]
	if p == nil {
		return nil, false, "PARTY_NOT_FOUND"
	}
	return map[string]interface{}{"party": partySnapshotLocked(p)}, true, "OK"
}

func clearPartyInvitesFor(name string) {
	partyMu.Lock()
	defer partyMu.Unlock()
	delete(partyInvites, name)
	for invitee, invite := range partyInvites {
		if invite.From == name {
			delete(partyInvites, invitee)
		}
	}
}

func clearFriendInvitesFor(name string) {
	friendMu.Lock()
	defer friendMu.Unlock()
	delete(friendInvites, name)
	for invitee, invite := range friendInvites {
		if invite.From == name {
			delete(friendInvites, invitee)
		}
	}
}

func broadcastParty(session *ClientSession, message string) bool {
	if session == nil || session.Character == nil {
		return false
	}
	party := partySnapshotForCharacter(session.Character.Name)
	partyID := toString(party, "id")
	if partyID == "" {
		return false
	}

	PublishRedisEvent(EvtChatParty, ChatPartyPayload{
		From:    session.Character.Name,
		PartyID: partyID,
		Message: message,
		Ts:      time.Now().UTC().Format(time.RFC3339),
	})
	return true
}

func chatDeliveryAllowed(sender, receiver *ClientSession) bool {
	if sender == nil || receiver == nil || sender.Character == nil || receiver.Character == nil {
		return false
	}
	if isBlocked(receiver.Character.Name, sender.Character.Name) {
		return false
	}
	if isBlocked(sender.Character.Name, receiver.Character.Name) {
		return false
	}
	return true
}

func handleSocialDisconnect(name string) {
	clearPartyInvitesFor(name)
	clearFriendInvitesFor(name)
	result, ok, _ := partyLeave(name)
	if !ok {
		return
	}
	if dissolved, _ := result["dissolved"].(bool); dissolved {
		if remaining, ok := result["remaining"].([]string); ok {
			for _, member := range remaining {
				sendMessageToCharacter(member, ServerMessage{
					Command: RespPartyUpdate,
					Payload: map[string]interface{}{
						"event": "PARTY_DISSOLVED",
						"actor": name,
					},
				})
			}
		}
		return
	}
	partyMap := toMap(result["party"])
	partyID := toString(partyMap, "id")
	notifyPartyMembers(partyID, "MEMBER_DISCONNECTED", name)
}

func canonicalGuildRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "leader":
		return "leader"
	case "officer":
		return "officer"
	default:
		return "member"
	}
}

func registerGuildMember(guildName, member, role string) {
	guild := strings.TrimSpace(guildName)
	member = strings.TrimSpace(member)
	if guild == "" || member == "" {
		return
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	g := guilds[guild]
	if g == nil {
		g = &Guild{
			Name:    guild,
			Leader:  member,
			Members: map[string]string{},
		}
		guilds[guild] = g
	}
	if g.Members == nil {
		g.Members = map[string]string{}
	}
	if strings.TrimSpace(role) == "" {
		if member == g.Leader {
			role = "leader"
		} else {
			role = "member"
		}
	}
	role = canonicalGuildRole(role)
	if role == "leader" {
		g.Leader = member
	}
	if g.Leader == member {
		role = "leader"
	}
	g.Members[member] = role
}

func guildRoleOfMember(member, guildName string) string {
	guild := strings.TrimSpace(guildName)
	member = strings.TrimSpace(member)
	if guild == "" || member == "" {
		return ""
	}
	guildMu.RLock()
	defer guildMu.RUnlock()
	g := guilds[guild]
	if g == nil {
		return ""
	}
	if role, ok := g.Members[member]; ok {
		return role
	}
	return ""
}

func guildMemberNames(guildName string) []string {
	guild := strings.TrimSpace(guildName)
	if guild == "" {
		return nil
	}
	guildMu.RLock()
	defer guildMu.RUnlock()
	g := guilds[guild]
	if g == nil || len(g.Members) == 0 {
		return nil
	}
	out := make([]string, 0, len(g.Members))
	for name := range g.Members {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func guildMembersPayload(guildName string) (map[string]interface{}, bool) {
	guild := strings.TrimSpace(guildName)
	if guild == "" {
		return nil, false
	}

	guildMu.RLock()
	g := guilds[guild]
	if g == nil || len(g.Members) == 0 {
		guildMu.RUnlock()
		return nil, false
	}
	names := make([]string, 0, len(g.Members))
	for name := range g.Members {
		names = append(names, name)
	}
	sort.Strings(names)
	roles := map[string]string{}
	for name, role := range g.Members {
		roles[name] = role
	}
	leader := g.Leader
	guildMu.RUnlock()

	members := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		s := findSessionByCharacterName(name)
		online := s != nil && s.Authenticated && s.Character != nil
		members = append(members, map[string]interface{}{
			"name":   name,
			"online": online,
			"role":   roles[name],
		})
	}
	return map[string]interface{}{
		"guild":   guild,
		"leader":  leader,
		"members": members,
		"count":   len(members),
	}, true
}

func guildCreate(member, guildName string) (map[string]interface{}, bool, string) {
	guild := strings.TrimSpace(guildName)
	member = strings.TrimSpace(member)
	if guild == "" {
		return nil, false, "GUILD_NAME_REQUIRED"
	}
	if member == "" {
		return nil, false, "MEMBER_REQUIRED"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	if _, exists := guilds[guild]; exists {
		return nil, false, "GUILD_EXISTS"
	}
	guilds[guild] = &Guild{
		Name:    guild,
		Leader:  member,
		Members: map[string]string{member: "leader"},
	}

	BroadcastSocialSync(SocialSyncPayload{
		Action: "GUILD_UPDATE",
		Guild:  guilds[guild],
	})

	return map[string]interface{}{"guild": guild, "member": member, "role": "leader"}, true, "OK"
}

func guildJoin(member, guildName string) (map[string]interface{}, bool, string) {
	guild := strings.TrimSpace(guildName)
	member = strings.TrimSpace(member)
	if guild == "" {
		return nil, false, "GUILD_NAME_REQUIRED"
	}
	if member == "" {
		return nil, false, "MEMBER_REQUIRED"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	g, exists := guilds[guild]
	if !exists || g == nil {
		return nil, false, "GUILD_NOT_FOUND"
	}
	if _, already := g.Members[member]; already {
		return nil, false, "ALREADY_IN_GUILD"
	}
	g.Members[member] = "member"

	BroadcastSocialSync(SocialSyncPayload{
		Action: "GUILD_UPDATE",
		Guild:  g,
	})

	return map[string]interface{}{"guild": guild, "member": member, "role": "member"}, true, "OK"
}

func guildInvite(inviter, target, guildName string) (map[string]interface{}, bool, string) {
	inviter = strings.TrimSpace(inviter)
	target = strings.TrimSpace(target)
	guild := strings.TrimSpace(guildName)
	if inviter == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if guild == "" {
		return nil, false, "NOT_IN_GUILD"
	}
	if inviter == target {
		return nil, false, "INVALID_TARGET"
	}
	if isBlocked(inviter, target) || isBlocked(target, inviter) {
		return nil, false, "BLOCKED"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	g := guilds[guild]
	if g == nil {
		return nil, false, "GUILD_NOT_FOUND"
	}
	inviterRole := canonicalGuildRole(g.Members[inviter])
	if inviterRole != "leader" && inviterRole != "officer" {
		return nil, false, "NOT_GUILD_LEADER"
	}
	if _, already := g.Members[target]; already {
		return nil, false, "TARGET_ALREADY_IN_GUILD"
	}

	invite := GuildInvite{
		GuildName: guild,
		From:      inviter,
		ExpiresAt: time.Now().UTC().Add(guildInviteTTL),
	}
	guildInvites[target] = invite
	BroadcastSocialSync(SocialSyncPayload{
		Action:      "GUILD_INVITE_SET",
		Target:      target,
		GuildInvite: &invite,
	})
	return map[string]interface{}{"from": inviter, "to": target, "guild": guild, "expires_sec": int(guildInviteTTL.Seconds())}, true, "OK"
}

func guildCancelInvite(inviter, target, guildName string) (map[string]interface{}, bool, string) {
	inviter = strings.TrimSpace(inviter)
	target = strings.TrimSpace(target)
	guild := strings.TrimSpace(guildName)
	if inviter == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if guild == "" {
		return nil, false, "NOT_IN_GUILD"
	}
	if inviter == target {
		return nil, false, "INVALID_TARGET"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	invite, ok := guildInvites[target]
	if !ok {
		return nil, false, "NO_INVITE"
	}
	if time.Now().UTC().After(invite.ExpiresAt) {
		delete(guildInvites, target)
		BroadcastSocialSync(SocialSyncPayload{Action: "GUILD_INVITE_CLEAR", Target: target})
		return nil, false, "INVITE_EXPIRED"
	}
	if invite.From != inviter || invite.GuildName != guild {
		return nil, false, "NOT_INVITER"
	}
	delete(guildInvites, target)
	BroadcastSocialSync(SocialSyncPayload{Action: "GUILD_INVITE_CLEAR", Target: target})
	return map[string]interface{}{
		"from":  inviter,
		"to":    target,
		"guild": guild,
	}, true, "OK"
}

func guildAccept(target, from string) (map[string]interface{}, bool, string) {
	target = strings.TrimSpace(target)
	from = strings.TrimSpace(from)
	if target == "" {
		return nil, false, "MEMBER_REQUIRED"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	invite, ok := guildInvites[target]
	if !ok {
		return nil, false, "NO_INVITE"
	}
	if time.Now().UTC().After(invite.ExpiresAt) {
		delete(guildInvites, target)
		BroadcastSocialSync(SocialSyncPayload{Action: "GUILD_INVITE_CLEAR", Target: target})
		return nil, false, "INVITE_EXPIRED"
	}
	if from != "" && from != invite.From {
		return nil, false, "INVITE_MISMATCH"
	}
	if isBlocked(invite.From, target) || isBlocked(target, invite.From) {
		delete(guildInvites, target)
		BroadcastSocialSync(SocialSyncPayload{Action: "GUILD_INVITE_CLEAR", Target: target})
		return nil, false, "BLOCKED"
	}
	g := guilds[invite.GuildName]
	if g == nil {
		delete(guildInvites, target)
		BroadcastSocialSync(SocialSyncPayload{Action: "GUILD_INVITE_CLEAR", Target: target})
		return nil, false, "GUILD_NOT_FOUND"
	}
	if _, exists := g.Members[target]; exists {
		delete(guildInvites, target)
		BroadcastSocialSync(SocialSyncPayload{Action: "GUILD_INVITE_CLEAR", Target: target})
		return nil, false, "ALREADY_IN_GUILD"
	}
	g.Members[target] = "member"
	delete(guildInvites, target)
	BroadcastSocialSync(SocialSyncPayload{Action: "GUILD_INVITE_CLEAR", Target: target})

	BroadcastSocialSync(SocialSyncPayload{
		Action: "GUILD_UPDATE",
		Guild:  g,
	})

	return map[string]interface{}{"guild": invite.GuildName, "member": target, "role": "member", "from": invite.From}, true, "OK"
}

func guildDecline(target, from string) (map[string]interface{}, bool, string) {
	target = strings.TrimSpace(target)
	from = strings.TrimSpace(from)
	if target == "" {
		return nil, false, "MEMBER_REQUIRED"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	invite, ok := guildInvites[target]
	if !ok {
		return nil, false, "NO_INVITE"
	}
	if time.Now().UTC().After(invite.ExpiresAt) {
		delete(guildInvites, target)
		BroadcastSocialSync(SocialSyncPayload{Action: "GUILD_INVITE_CLEAR", Target: target})
		return nil, false, "INVITE_EXPIRED"
	}
	if from != "" && from != invite.From {
		return nil, false, "INVITE_MISMATCH"
	}
	delete(guildInvites, target)
	BroadcastSocialSync(SocialSyncPayload{Action: "GUILD_INVITE_CLEAR", Target: target})
	return map[string]interface{}{
		"from":  invite.From,
		"to":    target,
		"guild": invite.GuildName,
	}, true, "OK"
}

func guildLeave(member, guildName string) (map[string]interface{}, bool, string) {
	guild := strings.TrimSpace(guildName)
	member = strings.TrimSpace(member)
	if guild == "" {
		return nil, false, "NOT_IN_GUILD"
	}
	if member == "" {
		return nil, false, "MEMBER_REQUIRED"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	g, exists := guilds[guild]
	if !exists || g == nil {
		return nil, false, "GUILD_NOT_FOUND"
	}
	if _, exists := g.Members[member]; !exists {
		return nil, false, "NOT_IN_GUILD"
	}

	delete(g.Members, member)
	if len(g.Members) == 0 {
		delete(guilds, guild)

		BroadcastSocialSync(SocialSyncPayload{
			Action:   "GUILD_DISBAND",
			Guild:    g,
			Removals: []string{member},
		})

		return map[string]interface{}{"guild": guild, "member": member, "disbanded": true}, true, "OK"
	}

	if g.Leader == member {
		names := make([]string, 0, len(g.Members))
		for name := range g.Members {
			names = append(names, name)
		}
		sort.Strings(names)
		g.Leader = names[0]
		g.Members[g.Leader] = "leader"
	}

	BroadcastSocialSync(SocialSyncPayload{
		Action:   "GUILD_UPDATE",
		Guild:    g,
		Removals: []string{member},
	})

	return map[string]interface{}{"guild": guild, "member": member, "leader": g.Leader, "disbanded": false}, true, "OK"
}

func guildDisband(actor, guildName string) (map[string]interface{}, bool, string) {
	actor = strings.TrimSpace(actor)
	guild := strings.TrimSpace(guildName)
	if guild == "" {
		return nil, false, "NOT_IN_GUILD"
	}
	if actor == "" {
		return nil, false, "MEMBER_REQUIRED"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	g, exists := guilds[guild]
	if !exists || g == nil {
		return nil, false, "GUILD_NOT_FOUND"
	}
	if g.Members[actor] != "leader" {
		return nil, false, "NOT_GUILD_LEADER"
	}

	members := make([]string, 0, len(g.Members))
	for name := range g.Members {
		members = append(members, name)
	}
	sort.Strings(members)
	delete(guilds, guild)
	for invitee, invite := range guildInvites {
		if invite.GuildName == guild {
			delete(guildInvites, invitee)
		}
	}

	BroadcastSocialSync(SocialSyncPayload{
		Action:   "GUILD_DISBAND",
		Guild:    g,
		Removals: members,
	})

	return map[string]interface{}{
		"guild":     guild,
		"actor":     actor,
		"members":   members,
		"disbanded": true,
	}, true, "OK"
}

func guildKick(actor, target, guildName string) (map[string]interface{}, bool, string) {
	actor = strings.TrimSpace(actor)
	target = strings.TrimSpace(target)
	guild := strings.TrimSpace(guildName)
	if guild == "" {
		return nil, false, "NOT_IN_GUILD"
	}
	if actor == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if actor == target {
		return nil, false, "INVALID_TARGET"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	g := guilds[guild]
	if g == nil {
		return nil, false, "GUILD_NOT_FOUND"
	}
	actorRole := canonicalGuildRole(g.Members[actor])
	targetRole := canonicalGuildRole(g.Members[target])
	if actorRole == "member" {
		return nil, false, "NOT_GUILD_LEADER"
	}
	if _, ok := g.Members[target]; !ok {
		return nil, false, "TARGET_NOT_IN_GUILD"
	}
	if actorRole == "officer" && targetRole != "member" {
		return nil, false, "INSUFFICIENT_GUILD_PERMS"
	}
	if actorRole == "leader" && targetRole == "leader" {
		return nil, false, "INVALID_TARGET"
	}
	delete(g.Members, target)

	BroadcastSocialSync(SocialSyncPayload{
		Action:   "GUILD_UPDATE",
		Guild:    g,
		Removals: []string{target},
	})

	return map[string]interface{}{
		"guild":  guild,
		"actor":  actor,
		"target": target,
		"leader": g.Leader,
	}, true, "OK"
}

func guildPromote(actor, target, guildName string) (map[string]interface{}, bool, string) {
	actor = strings.TrimSpace(actor)
	target = strings.TrimSpace(target)
	guild := strings.TrimSpace(guildName)
	if guild == "" {
		return nil, false, "NOT_IN_GUILD"
	}
	if actor == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if actor == target {
		return nil, false, "INVALID_TARGET"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	g := guilds[guild]
	if g == nil {
		return nil, false, "GUILD_NOT_FOUND"
	}
	if canonicalGuildRole(g.Members[actor]) != "leader" {
		return nil, false, "NOT_GUILD_LEADER"
	}
	targetRole, ok := g.Members[target]
	if !ok {
		return nil, false, "TARGET_NOT_IN_GUILD"
	}
	if canonicalGuildRole(targetRole) == "leader" {
		return nil, false, "INVALID_TARGET"
	}
	if canonicalGuildRole(targetRole) == "officer" {
		return nil, false, "ALREADY_OFFICER"
	}

	g.Members[target] = "officer"

	BroadcastSocialSync(SocialSyncPayload{
		Action: "GUILD_UPDATE",
		Guild:  g,
	})

	return map[string]interface{}{
		"guild":  guild,
		"actor":  actor,
		"target": target,
		"role":   "officer",
	}, true, "OK"
}

func guildDemote(actor, target, guildName string) (map[string]interface{}, bool, string) {
	actor = strings.TrimSpace(actor)
	target = strings.TrimSpace(target)
	guild := strings.TrimSpace(guildName)
	if guild == "" {
		return nil, false, "NOT_IN_GUILD"
	}
	if actor == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if actor == target {
		return nil, false, "INVALID_TARGET"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	g := guilds[guild]
	if g == nil {
		return nil, false, "GUILD_NOT_FOUND"
	}
	if canonicalGuildRole(g.Members[actor]) != "leader" {
		return nil, false, "NOT_GUILD_LEADER"
	}
	targetRole, ok := g.Members[target]
	if !ok {
		return nil, false, "TARGET_NOT_IN_GUILD"
	}
	if canonicalGuildRole(targetRole) == "leader" {
		return nil, false, "INVALID_TARGET"
	}
	if canonicalGuildRole(targetRole) != "officer" {
		return nil, false, "TARGET_NOT_OFFICER"
	}

	g.Members[target] = "member"

	BroadcastSocialSync(SocialSyncPayload{
		Action: "GUILD_UPDATE",
		Guild:  g,
	})

	return map[string]interface{}{
		"guild":  guild,
		"actor":  actor,
		"target": target,
		"role":   "member",
	}, true, "OK"
}

func guildTransferLeader(actor, target, guildName string) (map[string]interface{}, bool, string) {
	actor = strings.TrimSpace(actor)
	target = strings.TrimSpace(target)
	guild := strings.TrimSpace(guildName)
	if guild == "" {
		return nil, false, "NOT_IN_GUILD"
	}
	if actor == "" || target == "" {
		return nil, false, "TARGET_REQUIRED"
	}
	if actor == target {
		return nil, false, "INVALID_TARGET"
	}

	guildMu.Lock()
	defer guildMu.Unlock()
	g := guilds[guild]
	if g == nil {
		return nil, false, "GUILD_NOT_FOUND"
	}
	if g.Members[actor] != "leader" {
		return nil, false, "NOT_GUILD_LEADER"
	}
	if _, ok := g.Members[target]; !ok {
		return nil, false, "TARGET_NOT_IN_GUILD"
	}
	g.Members[actor] = "member"
	g.Members[target] = "leader"
	g.Leader = target

	BroadcastSocialSync(SocialSyncPayload{
		Action: "GUILD_UPDATE",
		Guild:  g,
	})

	return map[string]interface{}{
		"guild":      guild,
		"from":       actor,
		"to":         target,
		"new_leader": target,
	}, true, "OK"
}

func guildListPayload() map[string]interface{} {
	guildMu.RLock()
	defer guildMu.RUnlock()

	names := make([]string, 0, len(guilds))
	for name := range guilds {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]map[string]interface{}, 0, len(names))
	for _, name := range names {
		g := guilds[name]
		if g == nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"name":         name,
			"leader":       g.Leader,
			"member_count": len(g.Members),
		})
	}
	return map[string]interface{}{"guilds": out, "count": len(out)}
}
