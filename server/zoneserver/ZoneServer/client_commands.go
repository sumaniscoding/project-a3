package main

import (
	"encoding/json"
	"log"
	"net"
	"strings"
	"time"
)

func handleClientCommand(conn net.Conn, session *ClientSession, visible map[*ClientSession]bool, peerKey string, boundName *string, cmd string, rawPayload interface{}) (bool, bool) {
	switch cmd {
	case ReqMove:
		data, _ := json.Marshal(rawPayload)
		var move MoveRequest
		if err := json.Unmarshal(data, &move); err != nil {
			return true, false
		}
		newPos := Position{X: move.X, Y: move.Y, Z: move.Z}
		if !isMoveValid(session.Position, newPos) {
			sendMessage(conn, ServerMessage{Command: RespMoveRejected, Payload: "INVALID_MOVE"})
			return true, false
		}
		session.Position = newPos
		sendMessage(conn, ServerMessage{Command: RespMoveOK, Payload: session.Position})
		updateVisibilityForMove(session, visible)
		return true, true

	case ReqAuthToken:
		return handleAuthToken(conn, session, visible, peerKey, boundName, rawPayload)

	case ReqGetState:
		sendMessage(conn, ServerMessage{Command: RespState, Payload: statePayload(session)})
		return true, false
	case ReqGetHistory:
		sendMessage(conn, ServerMessage{Command: RespHistory, Payload: getUnlockHistoryPayload()})
		return true, false
	case ReqListEntities:
		sendMessage(conn, ServerMessage{Command: RespEntities, Payload: listNearbyEntities(session)})
		return true, false
	case ReqSkillTree:
		sendMessage(conn, ServerMessage{Command: RespSkillTree, Payload: map[string]interface{}{
			"class":        session.Character.Class,
			"skill_points": session.Character.SkillPoints,
			"known_skills": session.Character.Skills,
			"catalog":      skillListForClass(session.Character.Class),
		}})
		return true, false
	case ReqLearnSkill:
		payload := toMap(rawPayload)
		result, ok, reason := learnSkill(session.Character, toString(payload, "skill_id"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespSkillRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespSkillLearned, Payload: result})
		return true, true
	case ReqEnterWorld:
		payload := toMap(rawPayload)
		worldID := WorldID(toInt(payload, "world_id"))
		target := worlds[worldID]
		ok, reason := canEnterWorld(session.Character, target)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespEnterDenied, Payload: reason})
			return true, false
		}
		session.Character.WorldID = worldID
		session.World = target
		session.Position = DefaultSpawnPosition(worldID)
		sendMessage(conn, ServerMessage{Command: RespEnterOK, Payload: map[string]interface{}{"world": target.Name, "spawn": session.Position}})
		return true, true
	case ReqTalkNPC:
		payload := toMap(rawPayload)
		npc := toString(payload, "npc")
		if npc == "" {
			npc = "Elder Rowan"
		}
		session.Character.Trust[npc] += trustDelta(toString(payload, "choice"))
		sendMessage(conn, ServerMessage{Command: RespNPCState, Payload: map[string]interface{}{
			"npc":                   npc,
			"trust":                 session.Character.Trust[npc],
			"hidden_quest_unlocked": npc == quests["npc_oath_hidden"].RequiredNPC && session.Character.Trust[npc] >= quests["npc_oath_hidden"].MinTrust,
		}})
		return true, true
	case ReqAcceptQuest:
		payload := toMap(rawPayload)
		questID := toString(payload, "quest_id")
		q, exists := quests[questID]
		if !exists {
			sendMessage(conn, ServerMessage{Command: RespQuestRejected, Payload: "QUEST_NOT_FOUND"})
			return true, false
		}
		if q.Hidden && session.Character.Trust[q.RequiredNPC] < q.MinTrust {
			sendMessage(conn, ServerMessage{Command: RespQuestRejected, Payload: "QUEST_HIDDEN"})
			return true, false
		}
		if q.MinLevel > session.Character.Level {
			sendMessage(conn, ServerMessage{Command: RespQuestRejected, Payload: "LEVEL_TOO_LOW"})
			return true, false
		}
		cur := session.Character.Quests[questID]
		if cur == nil {
			cur = &QuestProgress{}
		}
		cur.Accepted = true
		session.Character.Quests[questID] = cur
		sendMessage(conn, ServerMessage{Command: RespQuestAccepted, Payload: map[string]interface{}{"quest_id": questID, "quest_name": q.Name}})
		return true, true
	case ReqCompleteQuest:
		payload := toMap(rawPayload)
		reward, ok, reason := applyQuestCompletion(session.Character, toString(payload, "quest_id"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespQuestRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespQuestCompleted, Payload: reward})
		return true, true
	case ReqAttack:
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		targetLevel := toInt(payload, "target_level")
		if targetLevel == 0 {
			targetLevel = session.Character.Level
		}
		damage, died := calculateAttack(session.Character, targetLevel)
		if died {
			applyDeathPenalty(session.Character, session.Position)
			sendMessage(conn, ServerMessage{Command: RespPlayerDied, Payload: map[string]interface{}{"target": target, "xp_debt": session.Character.XPDebt, "corpse": session.Character.Corpse, "recovery": "Use RECOVER_CORPSE"}})
			return true, true
		}
		xpGain := 25 + targetLevel*3
		leveled := gainXP(session.Character, xpGain)
		drop := maybeLegendaryDrop(session.Character)
		sendMessage(conn, ServerMessage{Command: RespCombatResult, Payload: map[string]interface{}{"target": target, "damage": damage, "xp_gain": xpGain, "leveled_up": leveled, "legendary": drop}})
		return true, true
	case ReqAttackMob:
		payload := toMap(rawPayload)
		result, ok, reason := attackMob(session, toString(payload, "mob_id"), toString(payload, "skill_id"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespMobAttackRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespMobAttackResult, Payload: result})
		return true, true
	case ReqAttackPVP:
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		if target == "" {
			sendMessage(conn, ServerMessage{Command: RespPVPRejected, Payload: "TARGET_REQUIRED"})
			return true, false
		}
		victimSession := findSessionByCharacterName(target)
		if victimSession == nil || victimSession.Character == nil {
			sendMessage(conn, ServerMessage{Command: RespPVPRejected, Payload: "TARGET_OFFLINE"})
			return true, false
		}
		if victimSession == session {
			sendMessage(conn, ServerMessage{Command: RespPVPRejected, Payload: "INVALID_TARGET"})
			return true, false
		}
		if victimSession.World.ID != session.World.ID {
			sendMessage(conn, ServerMessage{Command: RespPVPRejected, Payload: "TARGET_OTHER_WORLD"})
			return true, false
		}
		if !isVisible(session.Position, victimSession.Position) {
			sendMessage(conn, ServerMessage{Command: RespPVPRejected, Payload: "TARGET_OUT_OF_RANGE"})
			return true, false
		}
		result := attackPlayer(session, victimSession, toString(payload, "skill_id"))
		sendMessage(conn, ServerMessage{Command: RespPVPResult, Payload: result})
		sendMessage(victimSession.Conn, ServerMessage{Command: RespPVPHit, Payload: map[string]interface{}{"from": session.Character.Name, "damage": result["damage"], "target_hp": victimSession.Character.HP, "target_debt": victimSession.Character.XPDebt}})
		if err := persistCharacter(victimSession.Character); err != nil {
			log.Printf("Failed to persist victim character %s: %v", victimSession.Character.Name, err)
		}
		return true, true
	case ReqRecoverCorpse:
		if !recoverCorpse(session.Character) {
			sendMessage(conn, ServerMessage{Command: RespCorpseRecovery, Payload: "NO_CORPSE"})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespCorpseRecovery, Payload: map[string]interface{}{"status": "OK", "xp_debt": session.Character.XPDebt}})
		return true, true
	case ReqSetElement:
		payload := toMap(rawPayload)
		target := strings.ToLower(toString(payload, "target"))
		element := canonicalElement(toString(payload, "element"))
		if target != "weapon" && target != "armor" && target != "pet" {
			sendMessage(conn, ServerMessage{Command: RespElementRejected, Payload: "INVALID_TARGET"})
			return true, false
		}
		session.Character.Elemental[target] = element
		sendMessage(conn, ServerMessage{Command: RespElementSet, Payload: map[string]interface{}{"target": target, "element": element}})
		return true, true
	case ReqSummonPet:
		payload := toMap(rawPayload)
		name := toString(payload, "pet")
		if name != "" {
			session.Character.Pet.Name = name
		}
		session.Character.Pet.Summoned = true
		sendMessage(conn, ServerMessage{Command: RespPetSummoned, Payload: session.Character.Pet})
		return true, true
	case ReqRecruitMerc:
		payload := toMap(rawPayload)
		class := canonicalClassName(toString(payload, "class"))
		if class == "" {
			class = "Warrior"
		}
		session.Character.Mercenary = MercenaryState{Class: class, Level: session.Character.Level, Recruited: true}
		sendMessage(conn, ServerMessage{Command: RespMercRecruited, Payload: session.Character.Mercenary})
		return true, true
	case ReqEquipItem:
		payload := toMap(rawPayload)
		itemID := toString(payload, "item_id")
		for _, item := range session.Character.Inventory {
			if item.ID != itemID {
				continue
			}
			session.Character.Equipped[item.Slot] = item.ID
			sendMessage(conn, ServerMessage{Command: RespEquipOK, Payload: map[string]interface{}{"slot": item.Slot, "item": item}})
			return true, true
		}
		sendMessage(conn, ServerMessage{Command: RespEquipRejected, Payload: "ITEM_NOT_FOUND"})
		return true, false
	default:
		return false, false
	}
}

func handleAuthToken(conn net.Conn, session *ClientSession, visible map[*ClientSession]bool, peerKey string, boundName *string, rawPayload interface{}) (bool, bool) {
	if ok, wait := allowZoneAuthAttempt(peerKey); !ok {
		sendMessage(conn, ServerMessage{Command: RespAuthLocked, Payload: map[string]interface{}{"reason": "TOO_MANY_ATTEMPTS", "retry_after_sec": int(wait.Seconds())}})
		session.Active = false
		return true, false
	}

	payload := toMap(rawPayload)
	token := toString(payload, "token")
	class := toString(payload, "class")
	if token == "" {
		rejectAuthToken(conn, session, "TOKEN_REQUIRED")
		return true, false
	}

	claims, err := validateAuthToken(token)
	if err != nil {
		rejectAuthToken(conn, session, "TOKEN_INVALID")
		return true, false
	}

	oldName := session.Character.Name
	loaded, err := loadCharacter(claims.Username, class)
	if err != nil {
		sendMessage(conn, ServerMessage{Command: RespAuthRejected, Payload: "LOAD_FAILED"})
		return true, false
	}
	targetWorld := worlds[loaded.WorldID]
	if ok, _ := canEnterWorld(loaded, targetWorld); !ok {
		loaded.WorldID = World1
		targetWorld = worlds[World1]
	}

	session.Character = loaded
	session.World = targetWorld
	session.Position = DefaultSpawnPosition(targetWorld.ID)
	session.Authenticated = true
	session.AuthFailures = 0
	resetZoneAuthAttempts(peerKey)

	if *boundName != "" {
		unbindSessionCharacterName(session, *boundName)
	}
	if oldName != "" && oldName != *boundName {
		unbindSessionCharacterName(session, oldName)
	}
	bindSessionCharacterName(session, loaded.Name)
	*boundName = loaded.Name

	sendMessage(conn, ServerMessage{Command: RespAuthOK, Payload: map[string]interface{}{"name": loaded.Name, "class": loaded.Class, "world": targetWorld.Name}})
	sendMessage(conn, ServerMessage{Command: RespEnterOK, Payload: map[string]interface{}{"character": loaded.Name, "world": targetWorld.Name, "spawn": session.Position}})
	syncInitialVisibility(session, visible)
	sendMessage(conn, ServerMessage{Command: RespState, Payload: statePayload(session)})
	return true, false
}

func rejectAuthToken(conn net.Conn, session *ClientSession, reason string) {
	session.AuthFailures++
	sendMessage(conn, ServerMessage{Command: RespAuthRejected, Payload: reason})
	time.Sleep(time.Duration(session.AuthFailures*150) * time.Millisecond)
	if session.AuthFailures >= 3 {
		sendMessage(conn, ServerMessage{Command: RespAuthLocked, Payload: MsgTooManyAuthFailures})
		session.Active = false
	}
}

func updateVisibilityForMove(session *ClientSession, visible map[*ClientSession]bool) {
	forEachSession(func(other *ClientSession) {
		if other == session || other.World.ID != session.World.ID {
			return
		}
		nowVisible := isVisible(other.Position, session.Position)
		wasVisible := visible[other]
		switch {
		case nowVisible && !wasVisible:
			sendMessage(other.Conn, ServerMessage{Command: RespPlayerJoined, Payload: map[string]interface{}{"name": session.Character.Name, "pos": session.Position}})
			sendMessage(session.Conn, ServerMessage{Command: RespPlayerJoined, Payload: map[string]interface{}{"name": other.Character.Name, "pos": other.Position}})
			visible[other] = true
		case !nowVisible && wasVisible:
			sendMessage(other.Conn, ServerMessage{Command: RespPlayerLeft, Payload: session.Character.Name})
			sendMessage(session.Conn, ServerMessage{Command: RespPlayerLeft, Payload: other.Character.Name})
			delete(visible, other)
		case nowVisible && wasVisible:
			sendMessage(other.Conn, ServerMessage{Command: RespPlayerMoved, Payload: map[string]interface{}{"name": session.Character.Name, "pos": session.Position}})
		}
	})
}

func canonicalElement(raw string) Element {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "fire":
		return Element("Fire")
	case "ice":
		return Element("Ice")
	case "lightning":
		return Element("Lightning")
	case "earth":
		return Element("Earth")
	case "light":
		return Element("Light")
	case "dark":
		return Element("Dark")
	default:
		return ElementNone
	}
}

func canonicalClassName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "warrior":
		return "Warrior"
	case "mage":
		return "Mage"
	case "archer":
		return "Archer"
	case "rogue":
		return "Rogue"
	default:
		return ""
	}
}
