package main

import (
	"encoding/json"
	"log"
	"strings"
	"time"
)

func handleClientCommand(conn WSConn, session *ClientSession, visible map[*ClientSession]bool, peerKey string, boundName *string, cmd string, rawPayload interface{}) (bool, bool) {
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

	case ReqTeleport:
		payload := toMap(rawPayload)
		worldID := WorldID(toInt(payload, "world_id"))
		target, exists := worlds[worldID]
		if !exists {
			sendMessage(conn, ServerMessage{Command: RespError, Payload: "INVALID_WORLD"})
			return true, false
		}
		// In a real game, check if the player has permission or is near a teleporter NPC
		session.Character.WorldID = worldID
		session.World = target
		session.Position = DefaultSpawnPosition(worldID)
		sendMessage(conn, ServerMessage{Command: RespTeleportOK, Payload: map[string]interface{}{
			"world": target.Name,
			"spawn": session.Position,
		}})
		// Notify visibility system of a major warp
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
		if arePartyMates(session.Character.Name, victimSession.Character.Name) {
			sendMessage(conn, ServerMessage{Command: RespPVPRejected, Payload: "FRIENDLY_FIRE_BLOCKED"})
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
		if !session.Character.Pet.Acquired {
			sendMessage(conn, ServerMessage{Command: RespPetRejected, Payload: "PET_NOT_ACQUIRED"})
			return true, false
		}
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
		session.Character.Mercenary = MercenaryState{
			Class:     class,
			Level:     session.Character.Level,
			Recruited: true,
			Equipped:  map[string]string{},
		}
		syncMercStats(session.Character)
		sendMessage(conn, ServerMessage{Command: RespMercRecruited, Payload: session.Character.Mercenary})
		return true, true
	case ReqMercEquipItem:
		payload := toMap(rawPayload)
		result, ok, reason := equipMercItem(session.Character, toString(payload, "item_id"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespMercRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespMercUpdate, Payload: result})
		return true, true
	case ReqMercUnequip:
		payload := toMap(rawPayload)
		result, ok, reason := unequipMercItem(session.Character, toString(payload, "slot"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespMercRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespMercUpdate, Payload: result})
		return true, true
	case ReqEquipItem:
		payload := toMap(rawPayload)
		result, ok, reason := equipPlayerItem(session.Character, toString(payload, "item_id"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespEquipRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespEquipOK, Payload: result})
		return true, true
	case ReqUpgradeGear:
		payload := toMap(rawPayload)
		result, ok, reason := upgradeGear(session.Character, toString(payload, "item_id"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGearUpgradeReject, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespGearUpgradeResult, Payload: result})
		return true, true
	case ReqGetRecipes:
		sendMessage(conn, ServerMessage{Command: RespRecipes, Payload: recipesPayload()})
		return true, false
	case ReqCraftItem:
		payload := toMap(rawPayload)
		qty := toInt(payload, "qty")
		if qty == 0 {
			qty = 1
		}
		result, ok, reason := craftItem(session.Character, toString(payload, "recipe_id"), qty)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespCraftRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespCraftOK, Payload: result})
		return true, true
	case ReqPetFeed:
		payload := toMap(rawPayload)
		qty := toInt(payload, "qty")
		if qty == 0 {
			qty = 1
		}
		result, ok, reason := feedPet(session.Character, qty)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespPetRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespPetUpdate, Payload: result})
		return true, true
	case ReqStorageView:
		if !hasNearbyStorageNPC(session) {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: "STORAGE_NPC_REQUIRED"})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespStorageState, Payload: storageViewPayload(session.Character)})
		return true, false
	case ReqStorageDepMat:
		if !hasNearbyStorageNPC(session) {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: "STORAGE_NPC_REQUIRED"})
			return true, false
		}
		payload := toMap(rawPayload)
		result, ok, reason := storageDepositMaterial(session.Character, toString(payload, "item_id"), toInt(payload, "qty"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespStorageState, Payload: result})
		return true, true
	case ReqStorageWdrMat:
		if !hasNearbyStorageNPC(session) {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: "STORAGE_NPC_REQUIRED"})
			return true, false
		}
		payload := toMap(rawPayload)
		result, ok, reason := storageWithdrawMaterial(session.Character, toString(payload, "item_id"), toInt(payload, "qty"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespStorageState, Payload: result})
		return true, true
	case ReqStorageDepItm:
		if !hasNearbyStorageNPC(session) {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: "STORAGE_NPC_REQUIRED"})
			return true, false
		}
		payload := toMap(rawPayload)
		result, ok, reason := storageDepositItem(session.Character, toString(payload, "item_id"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespStorageState, Payload: result})
		return true, true
	case ReqStorageWdrItm:
		if !hasNearbyStorageNPC(session) {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: "STORAGE_NPC_REQUIRED"})
			return true, false
		}
		payload := toMap(rawPayload)
		result, ok, reason := storageWithdrawItem(session.Character, toString(payload, "item_id"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespStorageState, Payload: result})
		return true, true
	case ReqStorageDepGold:
		if !hasNearbyStorageNPC(session) {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: "STORAGE_NPC_REQUIRED"})
			return true, false
		}
		payload := toMap(rawPayload)
		result, ok, reason := storageDepositGold(session.Character, toInt(payload, "amount"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespStorageState, Payload: result})
		return true, true
	case ReqStorageWdrGold:
		if !hasNearbyStorageNPC(session) {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: "STORAGE_NPC_REQUIRED"})
			return true, false
		}
		payload := toMap(rawPayload)
		result, ok, reason := storageWithdrawGold(session.Character, toInt(payload, "amount"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespStorageRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespStorageState, Payload: result})
		return true, true
	case ReqChatSay:
		payload := toMap(rawPayload)
		message := sanitizeChatMessage(toString(payload, "message"))
		if message == "" {
			sendMessage(conn, ServerMessage{Command: RespError, Payload: "MESSAGE_REQUIRED"})
			return true, false
		}
		broadcastSay(session, message)
		return true, false
	case ReqChatWorld:
		payload := toMap(rawPayload)
		message := sanitizeChatMessage(toString(payload, "message"))
		if message == "" {
			sendMessage(conn, ServerMessage{Command: RespError, Payload: "MESSAGE_REQUIRED"})
			return true, false
		}
		broadcastWorld(session, message)
		return true, false
	case ReqChatWhisper:
		payload := toMap(rawPayload)
		message := sanitizeChatMessage(toString(payload, "message"))
		if message == "" {
			sendMessage(conn, ServerMessage{Command: RespError, Payload: "MESSAGE_REQUIRED"})
			return true, false
		}
		ok, reason := broadcastWhisper(session, toString(payload, "target"), message)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespWhisperRejected, Payload: reason})
		}
		return true, false
	case ReqSetPresence:
		payload := toMap(rawPayload)
		status, ok := parsePresenceStatus(toString(payload, "status"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespPresenceRejected, Payload: "INVALID_STATUS"})
			return true, false
		}
		changed := canonicalPresenceStatus(session.Character.Presence) != status
		session.Character.Presence = status
		sendMessage(conn, ServerMessage{Command: RespPresenceUpdate, Payload: map[string]interface{}{"name": session.Character.Name, "status": status}})
		if changed {
			notifyFriendPresenceChanged(session)
		}
		return true, changed
	case ReqGetPresence:
		sendMessage(conn, ServerMessage{Command: RespPresenceUpdate, Payload: map[string]interface{}{"name": session.Character.Name, "status": canonicalPresenceStatus(session.Character.Presence)}})
		return true, false
	case ReqChatParty:
		payload := toMap(rawPayload)
		message := sanitizeChatMessage(toString(payload, "message"))
		if message == "" {
			sendMessage(conn, ServerMessage{Command: RespError, Payload: "MESSAGE_REQUIRED"})
			return true, false
		}
		if !broadcastParty(session, message) {
			sendMessage(conn, ServerMessage{Command: RespPartyRejected, Payload: "NOT_IN_PARTY"})
		}
		return true, false
	case ReqChatGuild:
		payload := toMap(rawPayload)
		message := sanitizeChatMessage(toString(payload, "message"))
		if message == "" {
			sendMessage(conn, ServerMessage{Command: RespError, Payload: "MESSAGE_REQUIRED"})
			return true, false
		}
		if !broadcastGuild(session, message) {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "NOT_IN_GUILD"})
		}
		return true, false
	case ReqWho:
		sendMessage(conn, ServerMessage{Command: RespWhoList, Payload: whoPayload()})
		return true, false
	case ReqFriendList:
		sendMessage(conn, ServerMessage{Command: RespFriendList, Payload: friendListPayload(session.Character)})
		return true, false
	case ReqFriendStatus:
		sendMessage(conn, ServerMessage{Command: RespFriendStatus, Payload: friendStatusPayload(session.Character)})
		return true, false
	case ReqFriendRequest:
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		result, ok, reason := friendRequest(session.Character.Name, target)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespFriendRejected, Payload: reason})
			return true, false
		}
		targetSession := findSessionByCharacterName(target)
		if targetSession != nil && targetSession.Authenticated && targetSession.Character != nil {
			sendMessage(targetSession.Conn, ServerMessage{Command: RespFriendUpdate, Payload: map[string]interface{}{"event": "REQUEST_RECEIVED", "from": session.Character.Name, "expires_sec": result["expires_sec"]}})
		}
		sendMessage(conn, ServerMessage{Command: RespFriendUpdate, Payload: map[string]interface{}{"event": "REQUEST_SENT", "target": target, "expires_sec": result["expires_sec"]}})
		return true, false
	case ReqFriendCancel:
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		result, ok, reason := friendCancelRequest(session.Character.Name, target)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespFriendRejected, Payload: reason})
			return true, false
		}
		if targetSession := findSessionByCharacterName(target); targetSession != nil && targetSession.Authenticated && targetSession.Character != nil {
			sendMessage(targetSession.Conn, ServerMessage{Command: RespFriendUpdate, Payload: map[string]interface{}{"event": "INVITE_CANCELED", "from": session.Character.Name}})
		}
		sendMessage(conn, ServerMessage{Command: RespFriendUpdate, Payload: map[string]interface{}{"event": "INVITE_CANCELED", "target": target, "invite": result}})
		return true, false
	case ReqFriendAccept:
		payload := toMap(rawPayload)
		from := toString(payload, "from")
		result, ok, reason := friendAccept(session.Character.Name, from)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespFriendRejected, Payload: reason})
			return true, false
		}
		friend := toString(result, "friend")
		sendMessage(conn, ServerMessage{Command: RespFriendUpdate, Payload: map[string]interface{}{"event": "FRIEND_ADDED", "friend": friend, "friends": result["friends"]}})
		if inviterSession := findSessionByCharacterName(friend); inviterSession != nil && inviterSession.Authenticated && inviterSession.Character != nil {
			sendMessage(inviterSession.Conn, ServerMessage{Command: RespFriendUpdate, Payload: map[string]interface{}{"event": "FRIEND_ADDED", "friend": session.Character.Name, "friends": friendListPayload(inviterSession.Character)["friends"]}})
			if err := persistCharacter(inviterSession.Character); err != nil {
				log.Printf("Failed to persist friend character %s: %v", inviterSession.Character.Name, err)
			}
		}
		return true, true
	case ReqFriendDecline:
		payload := toMap(rawPayload)
		from := toString(payload, "from")
		result, ok, reason := friendDecline(session.Character.Name, from)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespFriendRejected, Payload: reason})
			return true, false
		}
		inviter := toString(result, "from")
		if inviterSession := findSessionByCharacterName(inviter); inviterSession != nil && inviterSession.Authenticated && inviterSession.Character != nil {
			sendMessage(inviterSession.Conn, ServerMessage{Command: RespFriendUpdate, Payload: map[string]interface{}{"event": "INVITE_DECLINED", "target": session.Character.Name}})
		}
		sendMessage(conn, ServerMessage{Command: RespFriendUpdate, Payload: map[string]interface{}{"event": "DECLINED", "from": inviter}})
		return true, false
	case ReqFriendRemove:
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		result, ok, reason := friendRemove(session.Character.Name, target)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespFriendRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespFriendUpdate, Payload: map[string]interface{}{"event": "FRIEND_REMOVED", "target": target, "friends": result["friends"]}})
		if targetUpdated, _ := result["target_updated"].(bool); targetUpdated {
			targetSession := findSessionByCharacterName(target)
			if targetSession != nil && targetSession.Authenticated && targetSession.Character != nil {
				sendMessage(targetSession.Conn, ServerMessage{Command: RespFriendUpdate, Payload: map[string]interface{}{"event": "FRIEND_REMOVED_BY", "by": session.Character.Name, "friends": friendListPayload(targetSession.Character)["friends"]}})
				if err := persistCharacter(targetSession.Character); err != nil {
					log.Printf("Failed to persist friend character %s: %v", targetSession.Character.Name, err)
				}
			}
		}
		return true, true
	case ReqBlockList:
		sendMessage(conn, ServerMessage{Command: RespBlockList, Payload: blockListPayload(session.Character)})
		return true, false
	case ReqBlockPlayer:
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		result, ok, reason := blockPlayer(session.Character.Name, target)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespBlockRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespBlockUpdate, Payload: map[string]interface{}{"event": "BLOCKED", "target": target, "blocked": result["blocked"]}})
		if targetUpdated, _ := result["target_updated"].(bool); targetUpdated {
			targetSession := findSessionByCharacterName(target)
			if targetSession != nil && targetSession.Authenticated && targetSession.Character != nil {
				sendMessage(targetSession.Conn, ServerMessage{Command: RespFriendUpdate, Payload: map[string]interface{}{"event": "FRIEND_REMOVED_BY", "by": session.Character.Name, "friends": friendListPayload(targetSession.Character)["friends"]}})
				sendMessage(targetSession.Conn, ServerMessage{Command: RespBlockUpdate, Payload: map[string]interface{}{"event": "BLOCKED_BY", "by": session.Character.Name}})
				if err := persistCharacter(targetSession.Character); err != nil {
					log.Printf("Failed to persist block target character %s: %v", targetSession.Character.Name, err)
				}
			}
		}
		return true, true
	case ReqUnblockPlayer:
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		result, ok, reason := unblockPlayer(session.Character.Name, target)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespBlockRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespBlockUpdate, Payload: map[string]interface{}{"event": "UNBLOCKED", "target": target, "blocked": result["blocked"]}})
		return true, true
	case ReqPartyInvite:
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		if !isCharacterOnlineAnywhere(target) {
			sendMessage(conn, ServerMessage{Command: RespPartyRejected, Payload: "TARGET_OFFLINE"})
			return true, false
		}
		result, ok, reason := partyInvite(session.Character.Name, target)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespPartyRejected, Payload: reason})
			return true, false
		}
		sendMessageToCharacter(target, ServerMessage{Command: RespPartyInvite, Payload: map[string]interface{}{"from": session.Character.Name}})
		sendMessage(conn, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "INVITE_SENT", "invite": result}})
		return true, false
	case ReqPartyCancel:
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		result, ok, reason := partyCancelInvite(session.Character.Name, target)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespPartyRejected, Payload: reason})
			return true, false
		}
		sendMessageToCharacter(target, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "INVITE_CANCELED", "from": session.Character.Name}})
		sendMessage(conn, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "INVITE_CANCELED", "target": target, "invite": result}})
		return true, false
	case ReqPartyAccept:
		payload := toMap(rawPayload)
		result, ok, reason := partyAccept(session.Character.Name, toString(payload, "from"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespPartyRejected, Payload: reason})
			return true, false
		}
		partyMap := toMap(result["party"])
		partyID := toString(partyMap, "id")
		notifyPartyMembers(partyID, "MEMBER_JOINED", session.Character.Name)
		sendMessage(conn, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "JOINED", "party": result["party"]}})
		return true, false
	case ReqPartyDecline:
		payload := toMap(rawPayload)
		result, ok, reason := partyDecline(session.Character.Name, toString(payload, "from"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespPartyRejected, Payload: reason})
			return true, false
		}
		inviter := toString(result, "from")
		sendMessageToCharacter(inviter, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "INVITE_DECLINED", "target": session.Character.Name}})
		sendMessage(conn, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "DECLINED", "from": inviter}})
		return true, false
	case ReqPartyLeave:
		result, ok, reason := partyLeave(session.Character.Name)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespPartyRejected, Payload: reason})
			return true, false
		}
		if dissolved, _ := result["dissolved"].(bool); dissolved {
			if remaining, ok := result["remaining"].([]string); ok {
				for _, member := range remaining {
					sendMessageToCharacter(member, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "PARTY_DISSOLVED", "actor": session.Character.Name}})
				}
			}
			sendMessage(conn, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "PARTY_DISSOLVED"}})
			return true, false
		}
		partyMap := toMap(result["party"])
		partyID := toString(partyMap, "id")
		notifyPartyMembers(partyID, "MEMBER_LEFT", session.Character.Name)
		sendMessage(conn, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "LEFT", "party": nil}})
		return true, false
	case ReqPartyKick:
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		result, ok, reason := partyKick(session.Character.Name, target)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespPartyRejected, Payload: reason})
			return true, false
		}
		sendMessageToCharacter(target, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "KICKED", "actor": session.Character.Name}})
		if dissolved, _ := result["dissolved"].(bool); dissolved {
			if remaining, ok := result["remaining"].([]string); ok {
				for _, member := range remaining {
					if member == session.Character.Name {
						continue
					}
					sendMessageToCharacter(member, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "PARTY_DISSOLVED", "actor": session.Character.Name}})
				}
			}
			sendMessage(conn, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "PARTY_DISSOLVED", "actor": session.Character.Name}})
			return true, false
		}
		partyID := toString(toMap(result["party"]), "id")
		notifyPartyMembers(partyID, "MEMBER_KICKED", target)
		sendMessage(conn, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "KICKED_MEMBER", "target": target, "party": result["party"]}})
		return true, false
	case ReqPartyTransfer:
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		result, ok, reason := partyTransferLeader(session.Character.Name, target)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespPartyRejected, Payload: reason})
			return true, false
		}
		partyID := toString(toMap(result["party"]), "id")
		notifyPartyMembers(partyID, "LEADER_CHANGED", session.Character.Name)
		sendMessage(conn, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "LEADERSHIP_TRANSFERRED", "target": target, "party": result["party"]}})
		return true, false
	case ReqPartyDisband:
		result, ok, reason := partyDisband(session.Character.Name)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespPartyRejected, Payload: reason})
			return true, false
		}
		members, _ := result["members"].([]string)
		for _, member := range members {
			payload := map[string]interface{}{"event": "PARTY_DISBANDED", "actor": session.Character.Name, "party": nil}
			sendMessageToCharacter(member, ServerMessage{Command: RespPartyUpdate, Payload: payload})
		}
		return true, false
	case ReqPartyReady:
		payload := toMap(rawPayload)
		ready := true
		if raw, ok := payload["ready"]; ok {
			if b, ok := raw.(bool); ok {
				ready = b
			}
		}
		result, ok, reason := setPartyReady(session.Character.Name, ready)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespPartyRejected, Payload: reason})
			return true, false
		}
		partyID := toString(toMap(result["party"]), "id")
		notifyPartyMembers(partyID, "READY_CHANGED", session.Character.Name)
		sendMessage(conn, ServerMessage{Command: RespPartyUpdate, Payload: map[string]interface{}{"event": "READY_CHANGED", "party": result["party"], "ready": ready}})
		return true, false
	case ReqPartyStatus:
		result, ok, reason := partyStatusForMember(session.Character.Name)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespPartyRejected, Payload: reason})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespPartyStatus, Payload: result})
		return true, false
	case ReqGuildCreate:
		if strings.TrimSpace(session.Character.Guild) != "" {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "ALREADY_IN_GUILD"})
			return true, false
		}
		payload := toMap(rawPayload)
		result, ok, reason := guildCreate(session.Character.Name, toString(payload, "name"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: reason})
			return true, false
		}
		session.Character.Guild = toString(result, "guild")
		session.Character.GuildRole = toString(result, "role")
		registerGuildMember(session.Character.Guild, session.Character.Name, session.Character.GuildRole)
		sendMessage(conn, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "CREATED", "guild": session.Character.Guild}})
		return true, true
	case ReqGuildJoin:
		if strings.TrimSpace(session.Character.Guild) != "" {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "ALREADY_IN_GUILD"})
			return true, false
		}
		payload := toMap(rawPayload)
		result, ok, reason := guildJoin(session.Character.Name, toString(payload, "name"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: reason})
			return true, false
		}
		session.Character.Guild = toString(result, "guild")
		session.Character.GuildRole = toString(result, "role")
		registerGuildMember(session.Character.Guild, session.Character.Name, session.Character.GuildRole)
		sendMessage(conn, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "JOINED", "guild": session.Character.Guild}})
		return true, true
	case ReqGuildInvite:
		if strings.TrimSpace(session.Character.Guild) == "" {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "NOT_IN_GUILD"})
			return true, false
		}
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		if !isCharacterOnlineAnywhere(target) {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "TARGET_OFFLINE"})
			return true, false
		}
		if strings.TrimSpace(characterGuildName(target)) != "" {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "TARGET_ALREADY_IN_GUILD"})
			return true, false
		}
		result, ok, reason := guildInvite(session.Character.Name, target, session.Character.Guild)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: reason})
			return true, false
		}
		sendMessageToCharacter(target, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "INVITED", "guild": toString(result, "guild"), "from": session.Character.Name, "expires_sec": result["expires_sec"]}})
		sendMessage(conn, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "INVITE_SENT", "target": target, "guild": session.Character.Guild}})
		return true, false
	case ReqGuildCancel:
		if strings.TrimSpace(session.Character.Guild) == "" {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "NOT_IN_GUILD"})
			return true, false
		}
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		result, ok, reason := guildCancelInvite(session.Character.Name, target, session.Character.Guild)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: reason})
			return true, false
		}
		sendMessageToCharacter(target, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "INVITE_CANCELED", "from": session.Character.Name, "guild": toString(result, "guild")}})
		sendMessage(conn, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "INVITE_CANCELED", "target": target, "guild": toString(result, "guild")}})
		return true, false
	case ReqGuildAccept:
		if strings.TrimSpace(session.Character.Guild) != "" {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "ALREADY_IN_GUILD"})
			return true, false
		}
		payload := toMap(rawPayload)
		result, ok, reason := guildAccept(session.Character.Name, toString(payload, "from"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: reason})
			return true, false
		}
		session.Character.Guild = toString(result, "guild")
		session.Character.GuildRole = toString(result, "role")
		registerGuildMember(session.Character.Guild, session.Character.Name, session.Character.GuildRole)
		sendMessage(conn, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "JOINED", "guild": session.Character.Guild, "role": session.Character.GuildRole}})
		return true, true
	case ReqGuildDecline:
		if strings.TrimSpace(session.Character.Guild) != "" {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "ALREADY_IN_GUILD"})
			return true, false
		}
		payload := toMap(rawPayload)
		result, ok, reason := guildDecline(session.Character.Name, toString(payload, "from"))
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: reason})
			return true, false
		}
		inviter := toString(result, "from")
		sendMessageToCharacter(inviter, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "INVITE_DECLINED", "target": session.Character.Name, "guild": toString(result, "guild")}})
		sendMessage(conn, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "DECLINED", "from": inviter, "guild": toString(result, "guild")}})
		return true, false
	case ReqGuildKick:
		if strings.TrimSpace(session.Character.Guild) == "" {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "NOT_IN_GUILD"})
			return true, false
		}
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		result, ok, reason := guildKick(session.Character.Name, target, session.Character.Guild)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: reason})
			return true, false
		}
		if targetSession := findSessionByCharacterName(target); targetSession != nil && targetSession.Authenticated && targetSession.Character != nil {
			targetSession.Character.Guild = ""
			targetSession.Character.GuildRole = ""
		}
		sendMessageToCharacter(target, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "KICKED", "guild": session.Character.Guild, "by": session.Character.Name}})
		sendMessage(conn, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "KICKED_MEMBER", "target": target, "guild": toString(result, "guild")}})
		return true, true
	case ReqGuildPromote:
		if strings.TrimSpace(session.Character.Guild) == "" {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "NOT_IN_GUILD"})
			return true, false
		}
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		result, ok, reason := guildPromote(session.Character.Name, target, session.Character.Guild)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: reason})
			return true, false
		}
		if targetSession := findSessionByCharacterName(target); targetSession != nil && targetSession.Authenticated && targetSession.Character != nil {
			targetSession.Character.GuildRole = "officer"
		}
		sendMessageToCharacter(target, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "PROMOTED", "guild": session.Character.Guild, "by": session.Character.Name, "role": "officer"}})
		sendMessage(conn, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "PROMOTED_MEMBER", "target": target, "guild": toString(result, "guild"), "role": "officer"}})
		return true, true
	case ReqGuildDemote:
		if strings.TrimSpace(session.Character.Guild) == "" {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "NOT_IN_GUILD"})
			return true, false
		}
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		result, ok, reason := guildDemote(session.Character.Name, target, session.Character.Guild)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: reason})
			return true, false
		}
		if targetSession := findSessionByCharacterName(target); targetSession != nil && targetSession.Authenticated && targetSession.Character != nil {
			targetSession.Character.GuildRole = "member"
		}
		sendMessageToCharacter(target, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "DEMOTED", "guild": session.Character.Guild, "by": session.Character.Name, "role": "member"}})
		sendMessage(conn, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "DEMOTED_MEMBER", "target": target, "guild": toString(result, "guild"), "role": "member"}})
		return true, true
	case ReqGuildTransfer:
		if strings.TrimSpace(session.Character.Guild) == "" {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "NOT_IN_GUILD"})
			return true, false
		}
		payload := toMap(rawPayload)
		target := toString(payload, "target")
		_, ok, reason := guildTransferLeader(session.Character.Name, target, session.Character.Guild)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: reason})
			return true, false
		}
		session.Character.GuildRole = "member"
		if targetSession := findSessionByCharacterName(target); targetSession != nil && targetSession.Authenticated && targetSession.Character != nil {
			targetSession.Character.GuildRole = "leader"
		}
		sendMessageToCharacter(target, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "LEADERSHIP_GRANTED", "guild": session.Character.Guild, "from": session.Character.Name}})
		sendMessage(conn, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "LEADERSHIP_TRANSFERRED", "guild": session.Character.Guild, "to": target}})
		return true, true
	case ReqGuildDisband:
		if strings.TrimSpace(session.Character.Guild) == "" {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "NOT_IN_GUILD"})
			return true, false
		}
		result, ok, reason := guildDisband(session.Character.Name, session.Character.Guild)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: reason})
			return true, false
		}
		guildName := toString(result, "guild")
		members, _ := result["members"].([]string)
		for _, member := range members {
			target := findSessionByCharacterName(member)
			if target != nil && target.Authenticated && target.Character != nil {
				target.Character.Guild = ""
				target.Character.GuildRole = ""
			}
			sendMessageToCharacter(member, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "DISBANDED", "guild": guildName, "by": session.Character.Name}})
			if target != nil && target.Authenticated && target.Character != nil && member != session.Character.Name {
				if err := persistCharacter(target.Character); err != nil {
					log.Printf("Failed to persist disband member %s: %v", target.Character.Name, err)
				}
			}
		}
		return true, true
	case ReqGuildLeave:
		result, ok, reason := guildLeave(session.Character.Name, session.Character.Guild)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: reason})
			return true, false
		}
		oldGuild := toString(result, "guild")
		session.Character.Guild = ""
		session.Character.GuildRole = ""
		sendMessage(conn, ServerMessage{Command: RespGuildUpdate, Payload: map[string]interface{}{"event": "LEFT", "guild": oldGuild}})
		return true, true
	case ReqGuildList:
		sendMessage(conn, ServerMessage{Command: RespGuildList, Payload: guildListPayload()})
		return true, false
	case ReqGuildMembers:
		payload, ok := guildMembersPayload(session.Character.Guild)
		if !ok {
			sendMessage(conn, ServerMessage{Command: RespGuildRejected, Payload: "NOT_IN_GUILD"})
			return true, false
		}
		sendMessage(conn, ServerMessage{Command: RespGuildMembers, Payload: payload})
		return true, false
	default:
		return false, false
	}
}

func handleAuthToken(conn WSConn, session *ClientSession, visible map[*ClientSession]bool, peerKey string, boundName *string, rawPayload interface{}) (bool, bool) {
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
	account, err := loadAccount(claims.Username)
	if err != nil {
		sendMessage(conn, ServerMessage{Command: RespAuthRejected, Payload: "LOAD_FAILED"})
		return true, false
	}
	// Backfill account store from legacy character-scoped fields one time.
	if !accountHasStoredData(account) && characterHasAccountScopedData(loaded) {
		syncAccountFromCharacter(account, loaded)
		if err := persistAccount(account); err != nil {
			sendMessage(conn, ServerMessage{Command: RespAuthRejected, Payload: "LOAD_FAILED"})
			return true, false
		}
	}
	syncCharacterFromAccount(loaded, account)
	targetWorld := worlds[loaded.WorldID]
	if ok, _ := canEnterWorld(loaded, targetWorld); !ok {
		loaded.WorldID = World1
		targetWorld = worlds[World1]
	}

	session.Character = loaded
	session.Account = account
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
	registerGuildMember(loaded.Guild, loaded.Name, loaded.GuildRole)
	loaded.GuildRole = guildRoleOfMember(loaded.Name, loaded.Guild)
	markCharacterOnline(loaded.Name)

	sendMessage(conn, ServerMessage{Command: RespAuthOK, Payload: map[string]interface{}{"name": loaded.Name, "class": loaded.Class, "world": targetWorld.Name}})
	sendMessage(conn, ServerMessage{Command: RespEnterOK, Payload: map[string]interface{}{"character": loaded.Name, "world": targetWorld.Name, "spawn": session.Position}})
	syncInitialVisibility(session, visible)
	sendMessage(conn, ServerMessage{Command: RespState, Payload: statePayload(session)})
	return true, false
}

func rejectAuthToken(conn WSConn, session *ClientSession, reason string) {
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
			sendMessage(other.Conn, ServerMessage{
				Command: RespPlayerJoined,
				Payload: map[string]interface{}{
					"name":   session.Character.Name,
					"pos":    session.Position,
					"class":  session.Character.Class,
					"weapon": getWeaponType(session.Character),
				},
			})
			sendMessage(session.Conn, ServerMessage{
				Command: RespPlayerJoined,
				Payload: map[string]interface{}{
					"name":   other.Character.Name,
					"pos":    other.Position,
					"class":  other.Character.Class,
					"weapon": getWeaponType(other.Character),
				},
			})
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
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.ReplaceAll(normalized, "_", " ")
	normalized = strings.Join(strings.Fields(normalized), " ")
	switch normalized {
	case "warrior":
		return "Warrior"
	case "mage":
		return "Mage"
	case "archer":
		return "Archer"
	case "healing knight", "healingknight":
		return "Healing Knight"
	case "rogue":
		return "Archer"
	default:
		return ""
	}
}

func notifyPartyMembers(partyID, event, actor string) {
	memberNames := partyMemberNames(partyID)
	if len(memberNames) == 0 {
		return
	}
	partyState := partySnapshotForCharacter(memberNames[0])
	for _, name := range memberNames {
		sendMessageToCharacter(name, ServerMessage{
			Command: RespPartyUpdate,
			Payload: map[string]interface{}{
				"event": event,
				"actor": actor,
				"party": partyState,
			},
		})
	}
}

func notifyFriendPresenceChanged(session *ClientSession) {
	if session == nil || session.Character == nil {
		return
	}
	status := canonicalPresenceStatus(session.Character.Presence)
	for _, friendName := range friendNamesForCharacter(session.Character) {
		friendSession := findSessionByCharacterName(friendName)
		if friendSession == nil || !friendSession.Authenticated || friendSession.Character == nil {
			continue
		}
		sendMessage(friendSession.Conn, ServerMessage{
			Command: RespFriendUpdate,
			Payload: map[string]interface{}{
				"event":  "PRESENCE_CHANGED",
				"friend": session.Character.Name,
				"status": status,
			},
		})
	}
}
