#!/usr/bin/env python3
import json
import socket
import time

SEND_DELAY_SEC = 0.10


class JsonConn:
    def __init__(self, host, port):
        self.sock = socket.create_connection((host, port), timeout=3)
        self.file = self.sock.makefile("r", encoding="utf-8", newline="\n")

    def send(self, obj):
        self.sock.sendall((json.dumps(obj) + "\n").encode("utf-8"))
        time.sleep(SEND_DELAY_SEC)

    def recv_until(self, expected_command, timeout=5.0, prefix="[msg]"):
        return self.recv_until_any([expected_command], timeout=timeout, prefix=prefix)

    def recv_until_any(self, expected_commands, timeout=5.0, prefix="[msg]"):
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.sock.settimeout(max(0.1, deadline - time.time()))
            line = self.file.readline()
            if not line:
                continue
            line = line.strip()
            if not line:
                continue
            print(prefix, line)
            msg = json.loads(line)
            if msg.get("command") == "RATE_LIMITED":
                time.sleep(0.25)
                continue
            if msg.get("command") in expected_commands:
                return msg
        raise TimeoutError(f"timed out waiting for one of {expected_commands}")

    def close(self):
        try:
            self.file.close()
        finally:
            self.sock.close()


def as_int(value):
    if isinstance(value, bool):
        return int(value)
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        return int(value)
    return 0


def as_materials_map(payload):
    raw = payload.get("materials", {})
    if not isinstance(raw, dict):
        return {}
    return {str(k): as_int(v) for k, v in raw.items()}


def has_inventory_item(payload, item_id):
    items = payload.get("inventory", [])
    if not isinstance(items, list):
        return False
    for item in items:
        if isinstance(item, dict) and item.get("id") == item_id:
            return True
    return False


def skill_rank(payload, skill_id):
    skills = payload.get("skills", {})
    if not isinstance(skills, dict):
        return 0
    return as_int(skills.get(skill_id))


def login_and_get_token(username, password):
    print("[login] connecting to :5555")
    conn = JsonConn("127.0.0.1", 5555)
    try:
        conn.send({"command": "PING"})
        conn.recv_until("PONG", prefix="[login]")

        conn.send({"command": "LOGIN", "username": username, "password": password})
        msg = conn.recv_until("LOGIN_OK", prefix="[login]")
        return msg["payload"]["token"]
    finally:
        conn.close()


def auth_and_get_state(token, class_hint, prefix):
    conn = JsonConn("127.0.0.1", 7777)
    try:
        conn.recv_until("AUTH_REQUIRED", prefix=prefix)
        conn.send({"command": "AUTH_TOKEN", "payload": {"token": token, "class": class_hint}})
        auth_ok = conn.recv_until("AUTH_OK", prefix=prefix)
        conn.recv_until("ENTER_OK", prefix=prefix)
        state_msg = conn.recv_until("STATE", prefix=prefix)
        return auth_ok.get("payload", {}), state_msg.get("payload", {})
    finally:
        conn.close()


def test_class_bootstrap_and_sticky(token):
    first_auth_payload, first_state = auth_and_get_state(token, "Mage", "[class]")
    if first_auth_payload.get("class") != "Mage":
        raise RuntimeError(f"expected first AUTH_OK class Mage, got {first_auth_payload!r}")
    if first_state.get("class") != "Mage":
        raise RuntimeError(f"expected first STATE class Mage, got {first_state!r}")
    if not has_inventory_item(first_state, "starter_focus"):
        raise RuntimeError(f"expected Mage starter_focus on first auth, got {first_state!r}")
    if skill_rank(first_state, "arc_bolt") != 1:
        raise RuntimeError(f"expected Mage starter arc_bolt rank 1, got {first_state!r}")

    second_auth_payload, second_state = auth_and_get_state(token, "Warrior", "[class]")
    if second_auth_payload.get("class") != "Mage":
        raise RuntimeError(f"expected sticky AUTH_OK class Mage on reauth, got {second_auth_payload!r}")
    if second_state.get("class") != "Mage":
        raise RuntimeError(f"expected sticky STATE class Mage on reauth, got {second_state!r}")
    if not has_inventory_item(second_state, "starter_focus"):
        raise RuntimeError(f"expected Mage starter_focus retained on reauth, got {second_state!r}")
    if has_inventory_item(second_state, "starter_blade"):
        raise RuntimeError(f"unexpected Warrior starter_blade after reauth class hint, got {second_state!r}")


def test_zone(token, username):
    print("[zone] connecting to :7777")
    conn = JsonConn("127.0.0.1", 7777)
    try:
        conn.recv_until("AUTH_REQUIRED", prefix="[zone]")

        conn.send({"command": "AUTH_TOKEN", "payload": {"token": token, "class": "Archer"}})
        conn.recv_until("AUTH_OK", prefix="[zone]")
        conn.recv_until("ENTER_OK", prefix="[zone]")
        conn.recv_until("STATE", prefix="[zone]")

        conn.send({"command": "GET_STATE"})
        state_msg = conn.recv_until("STATE", prefix="[zone]")
        state_payload = state_msg.get("payload", {})
        state_before_loot = as_materials_map(state_payload)

        conn.send({"command": "STORAGE_VIEW"})
        conn.recv_until("STORAGE_STATE", prefix="[zone]")

        conn.send({"command": "STORAGE_DEPOSIT_ITEM", "payload": {"item_id": "starter_bow"}})
        non_storable = conn.recv_until_any(["STORAGE_STATE", "STORAGE_REJECTED"], prefix="[zone]")
        if non_storable.get("command") != "STORAGE_REJECTED" or non_storable.get("payload") != "ITEM_NOT_STORABLE":
            raise RuntimeError(
                "expected STORAGE_REJECTED/ITEM_NOT_STORABLE for starter_bow deposit, "
                f"got {non_storable!r}"
            )

        deposit_amount = min(50, as_int(state_payload.get("gold")))
        if deposit_amount > 0:
            conn.send({"command": "STORAGE_DEPOSIT_GOLD", "payload": {"amount": deposit_amount}})
            conn.recv_until("STORAGE_STATE", prefix="[zone]")

            conn.send({"command": "STORAGE_WITHDRAW_GOLD", "payload": {"amount": deposit_amount}})
            conn.recv_until("STORAGE_STATE", prefix="[zone]")
        else:
            conn.send({"command": "STORAGE_DEPOSIT_GOLD", "payload": {"amount": 1}})
            rejected_gold = conn.recv_until_any(["STORAGE_STATE", "STORAGE_REJECTED"], prefix="[zone]")
            if rejected_gold.get("command") != "STORAGE_REJECTED":
                raise RuntimeError(f"expected STORAGE_REJECTED when no on-character gold, got {rejected_gold!r}")

        conn.send({"command": "UPGRADE_GEAR", "payload": {"item_id": "unknown_item"}})
        conn.recv_until("GEAR_UPGRADE_REJECTED", prefix="[zone]")

        conn.send({"command": "PET_FEED", "payload": {"qty": 1}})
        pet_feed_before_unlock = conn.recv_until_any(["PET_UPDATE", "PET_REJECTED"], prefix="[zone]")
        if pet_feed_before_unlock.get("command") != "PET_REJECTED" or pet_feed_before_unlock.get("payload") != "PET_NOT_ACQUIRED":
            raise RuntimeError(f"expected PET_REJECTED/PET_NOT_ACQUIRED before quest unlock, got {pet_feed_before_unlock!r}")

        conn.send({"command": "WHO"})
        conn.recv_until("WHO_LIST", prefix="[zone]")

        conn.send({"command": "GET_PRESENCE"})
        conn.recv_until("PRESENCE_UPDATE", prefix="[zone]")

        conn.send({"command": "SET_PRESENCE", "payload": {"status": "afk"}})
        conn.recv_until("PRESENCE_UPDATE", prefix="[zone]")

        conn.send({"command": "SET_PRESENCE", "payload": {"status": "busy"}})
        conn.recv_until("PRESENCE_REJECTED", prefix="[zone]")

        conn.send({"command": "FRIEND_LIST"})
        conn.recv_until("FRIEND_LIST", prefix="[zone]")

        conn.send({"command": "FRIEND_STATUS"})
        conn.recv_until("FRIEND_STATUS", prefix="[zone]")

        conn.send({"command": "FRIEND_REQUEST", "payload": {"target": "Nobody"}})
        conn.recv_until("FRIEND_REJECTED", prefix="[zone]")

        conn.send({"command": "FRIEND_CANCEL_REQUEST", "payload": {"target": "Nobody"}})
        conn.recv_until("FRIEND_REJECTED", prefix="[zone]")

        conn.send({"command": "FRIEND_ACCEPT", "payload": {"from": "Nobody"}})
        conn.recv_until("FRIEND_REJECTED", prefix="[zone]")

        conn.send({"command": "FRIEND_DECLINE", "payload": {"from": "Nobody"}})
        conn.recv_until("FRIEND_REJECTED", prefix="[zone]")

        conn.send({"command": "FRIEND_REMOVE", "payload": {"target": "Nobody"}})
        conn.recv_until("FRIEND_REJECTED", prefix="[zone]")

        conn.send({"command": "BLOCK_LIST"})
        conn.recv_until("BLOCK_LIST", prefix="[zone]")

        conn.send({"command": "BLOCK_PLAYER", "payload": {"target": username}})
        conn.recv_until("BLOCK_REJECTED", prefix="[zone]")

        conn.send({"command": "UNBLOCK_PLAYER", "payload": {"target": "Nobody"}})
        conn.recv_until("BLOCK_REJECTED", prefix="[zone]")

        conn.send({"command": "CHAT_SAY", "payload": {"message": "hello local"}})
        conn.recv_until("CHAT_MESSAGE", prefix="[zone]")

        conn.send({"command": "CHAT_WORLD", "payload": {"message": "hello world"}})
        conn.recv_until("CHAT_MESSAGE", prefix="[zone]")

        conn.send({"command": "CHAT_WHISPER", "payload": {"target": "Nobody", "message": "hello"}})
        conn.recv_until("WHISPER_REJECTED", prefix="[zone]")

        conn.send({"command": "PARTY_INVITE", "payload": {"target": username}})
        conn.recv_until("PARTY_REJECTED", prefix="[zone]")

        conn.send({"command": "PARTY_CANCEL_INVITE", "payload": {"target": "Nobody"}})
        conn.recv_until("PARTY_REJECTED", prefix="[zone]")

        conn.send({"command": "PARTY_ACCEPT", "payload": {"from": "Nobody"}})
        conn.recv_until("PARTY_REJECTED", prefix="[zone]")

        conn.send({"command": "PARTY_DECLINE", "payload": {"from": "Nobody"}})
        conn.recv_until("PARTY_REJECTED", prefix="[zone]")

        conn.send({"command": "PARTY_LEAVE"})
        conn.recv_until("PARTY_REJECTED", prefix="[zone]")

        conn.send({"command": "PARTY_READY", "payload": {"ready": True}})
        conn.recv_until("PARTY_REJECTED", prefix="[zone]")

        conn.send({"command": "PARTY_STATUS"})
        conn.recv_until("PARTY_REJECTED", prefix="[zone]")

        conn.send({"command": "CHAT_PARTY", "payload": {"message": "party hello"}})
        conn.recv_until("PARTY_REJECTED", prefix="[zone]")

        conn.send({"command": "PARTY_KICK", "payload": {"target": "Nobody"}})
        conn.recv_until("PARTY_REJECTED", prefix="[zone]")

        conn.send({"command": "PARTY_TRANSFER_LEADER", "payload": {"target": "Nobody"}})
        conn.recv_until("PARTY_REJECTED", prefix="[zone]")

        conn.send({"command": "PARTY_DISBAND"})
        conn.recv_until("PARTY_REJECTED", prefix="[zone]")

        conn.send({"command": "GUILD_LIST"})
        conn.recv_until("GUILD_LIST", prefix="[zone]")

        conn.send({"command": "GUILD_LEAVE"})
        conn.recv_until_any(["GUILD_UPDATE", "GUILD_REJECTED"], prefix="[zone]")

        conn.send({"command": "GUILD_DISBAND"})
        conn.recv_until("GUILD_REJECTED", prefix="[zone]")

        guild_name = f"SmokeGuild_{int(time.time())}"
        conn.send({"command": "GUILD_CREATE", "payload": {"name": guild_name}})
        conn.recv_until("GUILD_UPDATE", prefix="[zone]")

        conn.send({"command": "GUILD_LIST"})
        conn.recv_until("GUILD_LIST", prefix="[zone]")

        conn.send({"command": "GUILD_MEMBERS"})
        conn.recv_until("GUILD_MEMBERS", prefix="[zone]")

        conn.send({"command": "GUILD_INVITE", "payload": {"target": username}})
        conn.recv_until("GUILD_REJECTED", prefix="[zone]")

        conn.send({"command": "GUILD_CANCEL_INVITE", "payload": {"target": "Nobody"}})
        conn.recv_until("GUILD_REJECTED", prefix="[zone]")

        conn.send({"command": "GUILD_ACCEPT", "payload": {"from": "Nobody"}})
        conn.recv_until("GUILD_REJECTED", prefix="[zone]")

        conn.send({"command": "GUILD_DECLINE", "payload": {"from": "Nobody"}})
        conn.recv_until("GUILD_REJECTED", prefix="[zone]")

        conn.send({"command": "GUILD_TRANSFER_LEADER", "payload": {"target": "Nobody"}})
        conn.recv_until("GUILD_REJECTED", prefix="[zone]")

        conn.send({"command": "GUILD_KICK", "payload": {"target": "Nobody"}})
        conn.recv_until("GUILD_REJECTED", prefix="[zone]")

        conn.send({"command": "GUILD_PROMOTE", "payload": {"target": "Nobody"}})
        conn.recv_until("GUILD_REJECTED", prefix="[zone]")

        conn.send({"command": "GUILD_DEMOTE", "payload": {"target": "Nobody"}})
        conn.recv_until("GUILD_REJECTED", prefix="[zone]")

        conn.send({"command": "CHAT_GUILD", "payload": {"message": "guild hello"}})
        conn.recv_until("CHAT_MESSAGE", prefix="[zone]")

        conn.send({"command": "GUILD_DISBAND"})
        conn.recv_until("GUILD_UPDATE", prefix="[zone]")

        conn.send({"command": "SKILL_TREE"})
        conn.recv_until("SKILL_TREE", prefix="[zone]")

        conn.send({"command": "GET_RECIPES"})
        recipes_msg = conn.recv_until("RECIPES", prefix="[zone]")
        recipes_payload = recipes_msg.get("payload", {})
        recipe_list = recipes_payload.get("recipes", [])
        recipe_ids = {entry.get("id") for entry in recipe_list if isinstance(entry, dict)}
        if "guardian_shield" not in recipe_ids:
            raise RuntimeError(f"expected guardian_shield recipe in GET_RECIPES, got {recipe_ids!r}")
        if "hunter_helm" not in recipe_ids:
            raise RuntimeError(f"expected hunter_helm recipe in GET_RECIPES, got {recipe_ids!r}")
        if "sigil_ring" not in recipe_ids:
            raise RuntimeError(f"expected sigil_ring recipe in GET_RECIPES, got {recipe_ids!r}")

        conn.send({"command": "CRAFT_ITEM", "payload": {"recipe_id": "unknown_recipe", "qty": 1}})
        conn.recv_until("CRAFT_REJECTED", prefix="[zone]")
        conn.send({"command": "CRAFT_ITEM", "payload": {"recipe_id": "wolfhide_bow", "qty": 21}})
        craft_rejected = conn.recv_until("CRAFT_REJECTED", prefix="[zone]")
        if craft_rejected.get("payload") != "INVALID_QTY":
            raise RuntimeError(f"expected INVALID_QTY craft rejection, got {craft_rejected!r}")

        conn.send({"command": "LEARN_SKILL", "payload": {"skill_id": "burst_arrow"}})
        conn.recv_until_any(["SKILL_LEARNED", "SKILL_REJECTED"], prefix="[zone]")

        conn.send({"command": "LIST_ENTITIES"})
        conn.recv_until("ENTITIES", prefix="[zone]")

        conn.send({"command": "TALK_NPC", "payload": {"npc": "Elder Rowan", "choice": "honor"}})
        conn.recv_until("NPC_STATE", prefix="[zone]")

        conn.send({"command": "SUMMON_PET", "payload": {"pet": "Falcon"}})
        summon_before_unlock = conn.recv_until_any(["PET_SUMMONED", "PET_REJECTED"], prefix="[zone]")
        if summon_before_unlock.get("command") != "PET_REJECTED" or summon_before_unlock.get("payload") != "PET_NOT_ACQUIRED":
            raise RuntimeError(f"expected PET_REJECTED/PET_NOT_ACQUIRED before quest unlock, got {summon_before_unlock!r}")

        conn.send({"command": "ACCEPT_QUEST", "payload": {"quest_id": "unlock_world2_race"}})
        conn.recv_until("QUEST_ACCEPTED", prefix="[zone]")

        conn.send({"command": "COMPLETE_QUEST", "payload": {"quest_id": "unlock_world2_race"}})
        conn.recv_until("QUEST_COMPLETED", prefix="[zone]")

        conn.send({"command": "PET_FEED", "payload": {"qty": 1}})
        pet_feed_after_unlock = conn.recv_until_any(["PET_UPDATE", "PET_REJECTED"], prefix="[zone]")
        if pet_feed_after_unlock.get("command") == "PET_REJECTED" and pet_feed_after_unlock.get("payload") == "PET_NOT_ACQUIRED":
            raise RuntimeError(f"unexpected PET_NOT_ACQUIRED after unlock quest, got {pet_feed_after_unlock!r}")

        conn.send({"command": "ENTER_WORLD", "payload": {"world_id": 2}})
        conn.recv_until("ENTER_DENIED", prefix="[zone]")

        conn.send({"command": "SUMMON_PET", "payload": {"pet": "Falcon"}})
        conn.recv_until("PET_SUMMONED", prefix="[zone]")

        conn.send({"command": "RECRUIT_MERC", "payload": {"class": "Warrior"}})
        conn.recv_until("MERC_RECRUITED", prefix="[zone]")

        conn.send({"command": "MERC_EQUIP_ITEM", "payload": {"item_id": "unknown_item"}})
        conn.recv_until("MERC_REJECTED", prefix="[zone]")

        conn.send({"command": "MERC_EQUIP_ITEM", "payload": {"item_id": "starter_bow"}})
        conn.recv_until_any(["MERC_UPDATE", "MERC_REJECTED"], prefix="[zone]")

        conn.send({"command": "EQUIP_ITEM", "payload": {"item_id": "starter_bow"}})
        equip_rejected = conn.recv_until_any(["EQUIP_REJECTED", "EQUIP_OK"], prefix="[zone]")
        if equip_rejected.get("command") != "EQUIP_REJECTED":
            raise RuntimeError(f"expected EQUIP_REJECTED when merc has starter_bow, got {equip_rejected!r}")

        conn.send({"command": "MERC_UNEQUIP_ITEM", "payload": {"slot": "weapon"}})
        conn.recv_until_any(["MERC_UPDATE", "MERC_REJECTED"], prefix="[zone]")

        conn.send({"command": "EQUIP_ITEM", "payload": {"item_id": "starter_bow"}})
        conn.recv_until("EQUIP_OK", prefix="[zone]")

        conn.send({"command": "SET_ELEMENT", "payload": {"target": "weapon", "element": "Fire"}})
        conn.recv_until("ELEMENT_SET", prefix="[zone]")

        defeated = False
        material_drops = {}
        for _ in range(6):
            conn.send({"command": "ATTACK_MOB", "payload": {"mob_id": "mob_wolf_01", "skill_id": "burst_arrow"}})
            mob_result = conn.recv_until("MOB_ATTACK_RESULT", prefix="[zone]")
            payload = mob_result.get("payload", {})
            if payload.get("defeated"):
                drops = payload.get("drops", [])
                if not isinstance(drops, list) or not drops:
                    raise RuntimeError(f"expected non-empty drops list on defeat, got {drops!r}")
                for drop in drops:
                    if not isinstance(drop, dict):
                        raise RuntimeError(f"invalid drop entry type: {drop!r}")
                    kind = drop.get("kind")
                    item_id = drop.get("item_id")
                    qty = as_int(drop.get("qty"))
                    if kind not in ("material", "gear"):
                        raise RuntimeError(f"invalid drop kind: {drop!r}")
                    if not isinstance(item_id, str) or not item_id:
                        raise RuntimeError(f"missing drop item_id: {drop!r}")
                    if qty < 1:
                        raise RuntimeError(f"invalid drop qty: {drop!r}")
                    if kind == "gear" and "item" not in drop:
                        raise RuntimeError(f"gear drop missing item payload: {drop!r}")
                    if kind == "material":
                        material_drops[item_id] = material_drops.get(item_id, 0) + qty
                defeated = True
                break
            if payload.get("status") == "PLAYER_DIED":
                conn.send({"command": "RECOVER_CORPSE"})
                conn.recv_until("CORPSE_RECOVERY", prefix="[zone]")
        if not defeated:
            raise RuntimeError("failed to defeat mob_wolf_01 in smoke test")
        if not material_drops:
            raise RuntimeError("expected at least one material drop on mob defeat")

        conn.send({"command": "GET_STATE"})
        state_msg = conn.recv_until("STATE", prefix="[zone]")
        state_after_loot = as_materials_map(state_msg.get("payload", {}))
        for item_id, drop_qty in material_drops.items():
            before_qty = state_before_loot.get(item_id, 0)
            after_qty = state_after_loot.get(item_id, 0)
            if after_qty-before_qty != drop_qty:
                raise RuntimeError(
                    f"material delta mismatch for {item_id}: before={before_qty} after={after_qty} expected_delta={drop_qty}"
                )

        # Ensure we have at least one T1 enhance gem so UPGRADE_GEAR happy-path is exercised.
        gem_t1_qty = state_after_loot.get("enhance_gem_t1", 0)
        farm_attempts = 0
        farm_mobs = ("mob_wolf_01", "mob_bandit_01")
        while gem_t1_qty < 1 and farm_attempts < 80:
            mob_id = farm_mobs[farm_attempts % len(farm_mobs)]
            farm_attempts += 1
            defeated_target = False
            for _ in range(6):
                conn.send({"command": "ATTACK_MOB", "payload": {"mob_id": mob_id, "skill_id": "burst_arrow"}})
                attack_result = conn.recv_until_any(["MOB_ATTACK_RESULT", "MOB_ATTACK_REJECTED"], prefix="[zone]")
                if attack_result.get("command") == "MOB_ATTACK_REJECTED":
                    # Mob is likely on respawn; switch target attempt instead of spamming rejects.
                    time.sleep(0.35)
                    break
                attack_payload = attack_result.get("payload", {})
                if attack_payload.get("defeated"):
                    for drop in attack_payload.get("drops", []):
                        if (
                            isinstance(drop, dict)
                            and drop.get("kind") == "material"
                            and drop.get("item_id") == "enhance_gem_t1"
                        ):
                            gem_t1_qty += as_int(drop.get("qty"))
                    defeated_target = True
                    break
                if attack_payload.get("status") == "PLAYER_DIED":
                    conn.send({"command": "RECOVER_CORPSE"})
                    conn.recv_until("CORPSE_RECOVERY", prefix="[zone]")
            if not defeated_target:
                time.sleep(0.25)

        conn.send({"command": "GET_STATE"})
        state_msg = conn.recv_until("STATE", prefix="[zone]")
        state_before_upgrade = as_materials_map(state_msg.get("payload", {}))
        if state_before_upgrade.get("enhance_gem_t1", 0) < 1:
            raise RuntimeError(f"expected at least one enhance_gem_t1 before upgrade, got {state_before_upgrade!r}")

        conn.send({"command": "UPGRADE_GEAR", "payload": {"item_id": "starter_bow"}})
        upgrade_msg = conn.recv_until_any(["GEAR_UPGRADE_RESULT", "GEAR_UPGRADE_REJECTED"], prefix="[zone]")
        if upgrade_msg.get("command") != "GEAR_UPGRADE_RESULT":
            raise RuntimeError(f"expected GEAR_UPGRADE_RESULT for starter_bow upgrade, got {upgrade_msg!r}")
        upgrade_payload = upgrade_msg.get("payload", {})
        for key in ("old_gear_level", "new_gear_level", "success", "gem", "cost", "failure_effect"):
            if key not in upgrade_payload:
                raise RuntimeError(f"missing {key!r} in GEAR_UPGRADE_RESULT payload: {upgrade_payload!r}")
        if as_int(upgrade_payload.get("old_gear_level")) != 1:
            raise RuntimeError(f"expected old_gear_level=1 for starter_bow first upgrade, got {upgrade_payload!r}")
        if upgrade_payload.get("gem") != "enhance_gem_t1" or as_int(upgrade_payload.get("cost")) != 1:
            raise RuntimeError(f"unexpected upgrade gem/cost payload: {upgrade_payload!r}")
        if upgrade_payload.get("failure_effect") != "NONE":
            raise RuntimeError(f"expected failure_effect=NONE for +1 upgrade tier, got {upgrade_payload!r}")
        success = bool(upgrade_payload.get("success"))
        expected_new_level = 2 if success else 1
        if as_int(upgrade_payload.get("new_gear_level")) != expected_new_level:
            raise RuntimeError(
                f"unexpected new_gear_level for success={success}: payload={upgrade_payload!r}"
            )

        conn.send({"command": "GET_STATE"})
        state_msg = conn.recv_until("STATE", prefix="[zone]")
        state_after_upgrade = as_materials_map(state_msg.get("payload", {}))
        if state_before_upgrade.get("enhance_gem_t1", 0)-state_after_upgrade.get("enhance_gem_t1", 0) != 1:
            raise RuntimeError(
                "upgrade gem consumption mismatch for enhance_gem_t1: "
                f"before={state_before_upgrade.get('enhance_gem_t1', 0)} "
                f"after={state_after_upgrade.get('enhance_gem_t1', 0)}"
            )
        state_before_craft = dict(state_after_upgrade)

        conn.send({"command": "CRAFT_ITEM", "payload": {"recipe_id": "wolfhide_bow", "qty": 1}})
        craft_msg = conn.recv_until("CRAFT_OK", prefix="[zone]")
        craft_payload = craft_msg.get("payload", {})
        consumed = craft_payload.get("consumed", {})
        if not isinstance(consumed, dict) or as_int(consumed.get("wolf_pelt")) < 1:
            raise RuntimeError(f"expected wolf_pelt consumption in craft payload, got {craft_payload!r}")

        conn.send({"command": "GET_STATE"})
        state_msg = conn.recv_until("STATE", prefix="[zone]")
        state_after_craft = as_materials_map(state_msg.get("payload", {}))
        consumed_wolf = as_int(consumed.get("wolf_pelt"))
        if state_before_craft.get("wolf_pelt", 0)-state_after_craft.get("wolf_pelt", 0) != consumed_wolf:
            raise RuntimeError(
                "craft material delta mismatch for wolf_pelt: "
                f"before_craft={state_before_craft.get('wolf_pelt', 0)} "
                f"after_craft={state_after_craft.get('wolf_pelt', 0)} "
                f"consumed={consumed_wolf}"
            )

        # Upgrade + craft can validly consume the only tracked materials; farm one more drop if needed.
        if not any(qty > 0 for qty in state_after_craft.values()):
            refill_mobs = ("mob_wolf_01", "mob_bandit_01")
            for mob_id in refill_mobs:
                for _ in range(10):
                    conn.send({"command": "ATTACK_MOB", "payload": {"mob_id": mob_id, "skill_id": "burst_arrow"}})
                    refill_result = conn.recv_until_any(["MOB_ATTACK_RESULT", "MOB_ATTACK_REJECTED"], prefix="[zone]")
                    if refill_result.get("command") == "MOB_ATTACK_REJECTED":
                        time.sleep(0.35)
                        continue
                    refill_payload = refill_result.get("payload", {})
                    if refill_payload.get("status") == "PLAYER_DIED":
                        conn.send({"command": "RECOVER_CORPSE"})
                        conn.recv_until("CORPSE_RECOVERY", prefix="[zone]")
                    if refill_payload.get("defeated"):
                        conn.send({"command": "GET_STATE"})
                        refill_state_msg = conn.recv_until("STATE", prefix="[zone]")
                        state_after_craft = as_materials_map(refill_state_msg.get("payload", {}))
                        if any(qty > 0 for qty in state_after_craft.values()):
                            break
                if any(qty > 0 for qty in state_after_craft.values()):
                    break

        deposit_material_id = None
        for candidate in ("wolf_pelt", "bandit_scrap", "pet_treat", "enhance_gem_t1"):
            if state_after_craft.get(candidate, 0) > 0:
                deposit_material_id = candidate
                break
        if not deposit_material_id:
            for item_id, qty in state_after_craft.items():
                if qty > 0:
                    deposit_material_id = item_id
                    break
        if not deposit_material_id:
            raise RuntimeError(f"expected at least one material to validate storage deposit, got {state_after_craft!r}")

        conn.send({"command": "STORAGE_DEPOSIT_MATERIAL", "payload": {"item_id": deposit_material_id, "qty": 1}})
        storage_state = conn.recv_until("STORAGE_STATE", prefix="[zone]")
        storage_payload = storage_state.get("payload", {})
        storage_materials = storage_payload.get("storage", {}).get("materials", {})
        if as_int(storage_materials.get(deposit_material_id)) < 1:
            raise RuntimeError(
                f"expected {deposit_material_id} in storage after deposit, got {storage_payload!r}"
            )

        conn.send({"command": "STORAGE_WITHDRAW_MATERIAL", "payload": {"item_id": deposit_material_id, "qty": 1}})
        conn.recv_until("STORAGE_STATE", prefix="[zone]")

        conn.send({"command": "ATTACK_PVP", "payload": {"target": "Lowbie", "skill_id": "burst_arrow"}})
        conn.recv_until("PVP_REJECTED", prefix="[zone]")

        conn.send({"command": "ATTACK", "payload": {"target": "rift_wolf", "target_level": 44}})
        conn.recv_until("COMBAT_RESULT", prefix="[zone]")

        conn.send({"command": "MOVE", "payload": {"x": 105.0, "y": 0.0, "z": 105.0}})
        conn.recv_until("MOVE_OK", prefix="[zone]")

        for x in (112.0, 119.0, 126.0, 133.0, 140.0, 147.0):
            conn.send({"command": "MOVE", "payload": {"x": x, "y": 0.0, "z": x}})
            conn.recv_until("MOVE_OK", prefix="[zone]")
        conn.send({"command": "STORAGE_VIEW"})
        storage_far = conn.recv_until_any(["STORAGE_STATE", "STORAGE_REJECTED"], prefix="[zone]")
        if storage_far.get("command") != "STORAGE_REJECTED" or storage_far.get("payload") != "STORAGE_NPC_REQUIRED":
            raise RuntimeError(f"expected STORAGE_REJECTED/STORAGE_NPC_REQUIRED far from storage NPC, got {storage_far!r}")

        conn.send({"command": "MOVE", "payload": {"x": 500.0, "y": 0.0, "z": 500.0}})
        conn.recv_until("MOVE_REJECTED", prefix="[zone]")
    finally:
        conn.close()


if __name__ == "__main__":
    class_username = f"SmokeClass_{int(time.time())}"
    class_token = login_and_get_token(class_username, "demo")
    test_class_bootstrap_and_sticky(class_token)

    username = f"SmokeHero_{int(time.time())}"
    token = login_and_get_token(username, "demo")
    test_zone(token, username)
