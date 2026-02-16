#!/usr/bin/env python3
import json
import socket
import time


class JsonConn:
    def __init__(self, host, port):
        self.sock = socket.create_connection((host, port), timeout=3)
        self.file = self.sock.makefile("r", encoding="utf-8", newline="\n")

    def send(self, obj):
        self.sock.sendall((json.dumps(obj) + "\n").encode("utf-8"))

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
            if msg.get("command") in expected_commands:
                return msg
        raise TimeoutError(f"timed out waiting for one of {expected_commands}")

    def close(self):
        try:
            self.file.close()
        finally:
            self.sock.close()


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


def test_zone(token):
    print("[zone] connecting to :7777")
    conn = JsonConn("127.0.0.1", 7777)
    try:
        conn.recv_until("AUTH_REQUIRED", prefix="[zone]")

        conn.send({"command": "AUTH_TOKEN", "payload": {"token": token, "class": "Archer"}})
        conn.recv_until("AUTH_OK", prefix="[zone]")
        conn.recv_until("ENTER_OK", prefix="[zone]")
        conn.recv_until("STATE", prefix="[zone]")

        conn.send({"command": "GET_STATE"})
        conn.recv_until("STATE", prefix="[zone]")

        conn.send({"command": "SKILL_TREE"})
        conn.recv_until("SKILL_TREE", prefix="[zone]")

        conn.send({"command": "GET_RECIPES"})
        conn.recv_until("RECIPES", prefix="[zone]")

        conn.send({"command": "CRAFT_ITEM", "payload": {"recipe_id": "unknown_recipe", "qty": 1}})
        conn.recv_until("CRAFT_REJECTED", prefix="[zone]")

        conn.send({"command": "LEARN_SKILL", "payload": {"skill_id": "burst_arrow"}})
        conn.recv_until_any(["SKILL_LEARNED", "SKILL_REJECTED"], prefix="[zone]")

        conn.send({"command": "LIST_ENTITIES"})
        conn.recv_until("ENTITIES", prefix="[zone]")

        conn.send({"command": "TALK_NPC", "payload": {"npc": "Elder Rowan", "choice": "honor"}})
        conn.recv_until("NPC_STATE", prefix="[zone]")

        conn.send({"command": "ACCEPT_QUEST", "payload": {"quest_id": "unlock_world2_race"}})
        conn.recv_until("QUEST_ACCEPTED", prefix="[zone]")

        conn.send({"command": "COMPLETE_QUEST", "payload": {"quest_id": "unlock_world2_race"}})
        conn.recv_until("QUEST_COMPLETED", prefix="[zone]")

        conn.send({"command": "ENTER_WORLD", "payload": {"world_id": 2}})
        conn.recv_until("ENTER_DENIED", prefix="[zone]")

        conn.send({"command": "SUMMON_PET", "payload": {"pet": "Falcon"}})
        conn.recv_until("PET_SUMMONED", prefix="[zone]")

        conn.send({"command": "RECRUIT_MERC", "payload": {"class": "Warrior"}})
        conn.recv_until("MERC_RECRUITED", prefix="[zone]")

        conn.send({"command": "SET_ELEMENT", "payload": {"target": "weapon", "element": "Fire"}})
        conn.recv_until("ELEMENT_SET", prefix="[zone]")

        defeated = False
        for _ in range(6):
            conn.send({"command": "ATTACK_MOB", "payload": {"mob_id": "mob_wolf_01", "skill_id": "burst_arrow"}})
            mob_result = conn.recv_until("MOB_ATTACK_RESULT", prefix="[zone]")
            payload = mob_result.get("payload", {})
            if payload.get("defeated"):
                defeated = True
                break
            if payload.get("status") == "PLAYER_DIED":
                conn.send({"command": "RECOVER_CORPSE"})
                conn.recv_until("CORPSE_RECOVERY", prefix="[zone]")
        if not defeated:
            raise RuntimeError("failed to defeat mob_wolf_01 in smoke test")

        conn.send({"command": "CRAFT_ITEM", "payload": {"recipe_id": "wolfhide_bow", "qty": 1}})
        conn.recv_until("CRAFT_OK", prefix="[zone]")

        conn.send({"command": "ATTACK_PVP", "payload": {"target": "Lowbie", "skill_id": "burst_arrow"}})
        conn.recv_until("PVP_REJECTED", prefix="[zone]")

        conn.send({"command": "ATTACK", "payload": {"target": "rift_wolf", "target_level": 44}})
        conn.recv_until("COMBAT_RESULT", prefix="[zone]")

        conn.send({"command": "MOVE", "payload": {"x": 105.0, "y": 0.0, "z": 105.0}})
        conn.recv_until("MOVE_OK", prefix="[zone]")

        conn.send({"command": "MOVE", "payload": {"x": 500.0, "y": 0.0, "z": 500.0}})
        conn.recv_until("MOVE_REJECTED", prefix="[zone]")
    finally:
        conn.close()


if __name__ == "__main__":
    token = login_and_get_token("SmokeHero", "demo")
    test_zone(token)
