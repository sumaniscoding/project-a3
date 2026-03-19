#!/usr/bin/env python3
import json
import os
import socket
import time
import websocket


class JsonConn:
    def __init__(self, host, port):
        self.sock = websocket.create_connection(f"ws://{host}:{port}/ws", timeout=3)

    def send(self, obj):
        self.sock.send(json.dumps(obj))

    def recv_until(self, expected_command, timeout=5.0):
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.sock.settimeout(max(0.1, deadline - time.time()))
            try:
                line = self.sock.recv()
            except websocket.WebSocketTimeoutException:
                continue
            except Exception as e:
                print(f"[raw] recv error:", e)
                continue
            if not line:
                continue
            line = line.strip()
            if not line:
                continue
            print("[raw]", line)
            msg = json.loads(line)
            if msg.get("command") == expected_command:
                return msg
        raise TimeoutError(f"timed out waiting for {expected_command}")

    def close(self):
        self.sock.close()


ZONE_HOST = os.getenv("A3_ZONE_HOST", "127.0.0.1")
ZONE_PORT_A = int(os.getenv("A3_ZONE_PORT_A", "7777"))
ZONE_PORT_B = int(os.getenv("A3_ZONE_PORT_B", str(ZONE_PORT_A)))
LOGIN_URL = os.getenv("A3_LOGIN_URL", "ws://127.0.0.1:5555/ws")


def expect_payload(msg, expected, label):
    actual = msg.get("payload")
    if actual != expected:
        raise AssertionError(f"{label}: expected payload {expected!r}, got {actual!r}")


def auth(conn, name):
    conn.recv_until("AUTH_REQUIRED")
    conn.send({"command": "AUTH_TOKEN", "payload": {"token": login_token(name), "class": "Archer"}})
    conn.recv_until("AUTH_OK")
    conn.recv_until("ENTER_OK")
    conn.recv_until("STATE")


def login_token(username):
    sock = websocket.create_connection(LOGIN_URL, timeout=3)
    try:
        password = "demo-pass"
        sock.send(json.dumps({"command": "REGISTER", "username": username, "password": password}))
        register_msg = recv_until(sock, {"REGISTER_OK", "REGISTER_DENIED"})
        if register_msg.get("command") == "REGISTER_DENIED" and register_msg.get("payload") != "ACCOUNT_EXISTS":
            raise RuntimeError(f"unexpected register response: {register_msg!r}")
        sock.send(json.dumps({"command": "LOGIN", "username": username, "password": password}))
        msg = recv_until(sock, {"LOGIN_OK"})
    finally:
        sock.close()
    return msg["payload"]["token"]


def recv_until(sock, expected_commands, timeout=5.0):
    deadline = time.time() + timeout
    while time.time() < deadline:
        sock.settimeout(max(0.1, deadline - time.time()))
        line = sock.recv()
        if not line:
            continue
        msg = json.loads(line)
        if msg.get("command") in expected_commands:
            return msg
    raise TimeoutError(f"timed out waiting for {expected_commands}")


def main():
    run_id = int(time.time())
    name_a = f"DuelA_{run_id}"
    name_b = f"DuelB_{run_id}"

    a = JsonConn(ZONE_HOST, ZONE_PORT_A)
    b = JsonConn(ZONE_HOST, ZONE_PORT_B)

    try:
        auth(a, name_a)
        auth(b, name_b)

        a.send({"command": "MOVE", "payload": {"x": 7.0, "y": 0.0, "z": 7.0}})
        a.recv_until("MOVE_OK")

        b.send({"command": "MOVE", "payload": {"x": 6.0, "y": 0.0, "z": 6.0}})
        b.recv_until("MOVE_OK")

        a.send({"command": "SET_PRESENCE", "payload": {"status": "online"}})
        a.recv_until("PRESENCE_UPDATE")
        b.send({"command": "SET_PRESENCE", "payload": {"status": "afk"}})
        b.recv_until("PRESENCE_UPDATE")

        # Friend flow: request -> cancel -> request -> decline -> request -> accept -> remove.
        a.send({"command": "FRIEND_REQUEST", "payload": {"target": name_b}})
        b.recv_until("FRIEND_UPDATE")
        a.recv_until("FRIEND_UPDATE")
        a.send({"command": "FRIEND_CANCEL_REQUEST", "payload": {"target": name_b}})
        a.recv_until("FRIEND_UPDATE")
        b.recv_until("FRIEND_UPDATE")
        a.send({"command": "FRIEND_REQUEST", "payload": {"target": name_b}})
        b.recv_until("FRIEND_UPDATE")
        a.recv_until("FRIEND_UPDATE")
        b.send({"command": "FRIEND_DECLINE", "payload": {"from": name_a}})
        b.recv_until("FRIEND_UPDATE")
        a.recv_until("FRIEND_UPDATE")
        a.send({"command": "FRIEND_REQUEST", "payload": {"target": name_b}})
        b.recv_until("FRIEND_UPDATE")
        a.recv_until("FRIEND_UPDATE")
        b.send({"command": "FRIEND_ACCEPT", "payload": {"from": name_a}})
        b.recv_until("FRIEND_UPDATE")
        a.recv_until("FRIEND_UPDATE")
        a.send({"command": "FRIEND_STATUS"})
        a.recv_until("FRIEND_STATUS")
        a.send({"command": "FRIEND_REMOVE", "payload": {"target": name_b}})
        a.recv_until("FRIEND_UPDATE")
        b.recv_until("FRIEND_UPDATE")

        # Block flow affects whisper delivery.
        a.send({"command": "BLOCK_PLAYER", "payload": {"target": name_b}})
        a.recv_until("BLOCK_UPDATE")
        a.send({"command": "CHAT_WHISPER", "payload": {"target": name_b, "message": "blocked?"}})
        a.recv_until("WHISPER_REJECTED")
        a.send({"command": "UNBLOCK_PLAYER", "payload": {"target": name_b}})
        a.recv_until("BLOCK_UPDATE")

        # Direct whisper check.
        a.send({"command": "CHAT_WHISPER", "payload": {"target": name_b, "message": "ready?"}})
        b.recv_until("CHAT_MESSAGE")
        a.recv_until("CHAT_MESSAGE")

        # Blocked players cannot send party invites.
        b.send({"command": "BLOCK_PLAYER", "payload": {"target": name_a}})
        b.recv_until("BLOCK_UPDATE")
        a.send({"command": "PARTY_INVITE", "payload": {"target": name_b}})
        expect_payload(a.recv_until("PARTY_REJECTED"), "BLOCKED", "party invite while blocked")
        b.send({"command": "UNBLOCK_PLAYER", "payload": {"target": name_a}})
        b.recv_until("BLOCK_UPDATE")

        # Guild invite flow: leader invites, target accepts.
        a.send({"command": "GUILD_CREATE", "payload": {"name": f"DuelGuild_{int(time.time())}"}})
        a.recv_until("GUILD_UPDATE")

        # Blocked players cannot send guild invites.
        b.send({"command": "BLOCK_PLAYER", "payload": {"target": name_a}})
        b.recv_until("BLOCK_UPDATE")
        a.send({"command": "GUILD_INVITE", "payload": {"target": name_b}})
        expect_payload(a.recv_until("GUILD_REJECTED"), "BLOCKED", "guild invite while blocked")
        b.send({"command": "UNBLOCK_PLAYER", "payload": {"target": name_a}})
        b.recv_until("BLOCK_UPDATE")

        a.send({"command": "GUILD_INVITE", "payload": {"target": name_b}})
        b.recv_until("GUILD_UPDATE")
        a.recv_until("GUILD_UPDATE")
        a.send({"command": "GUILD_CANCEL_INVITE", "payload": {"target": name_b}})
        a.recv_until("GUILD_UPDATE")
        b.recv_until("GUILD_UPDATE")
        a.send({"command": "GUILD_INVITE", "payload": {"target": name_b}})
        b.recv_until("GUILD_UPDATE")
        a.recv_until("GUILD_UPDATE")
        b.send({"command": "GUILD_DECLINE", "payload": {"from": name_a}})
        b.recv_until("GUILD_UPDATE")
        a.recv_until("GUILD_UPDATE")
        a.send({"command": "GUILD_INVITE", "payload": {"target": name_b}})
        b.recv_until("GUILD_UPDATE")
        a.recv_until("GUILD_UPDATE")
        b.send({"command": "GUILD_ACCEPT", "payload": {"from": name_a}})
        b.recv_until("GUILD_UPDATE")
        a.send({"command": "GUILD_PROMOTE", "payload": {"target": name_b}})
        a.recv_until("GUILD_UPDATE")
        b.recv_until("GUILD_UPDATE")
        a.send({"command": "GUILD_DEMOTE", "payload": {"target": name_b}})
        a.recv_until("GUILD_UPDATE")
        b.recv_until("GUILD_UPDATE")
        a.send({"command": "GUILD_TRANSFER_LEADER", "payload": {"target": name_b}})
        a.recv_until("GUILD_UPDATE")
        b.recv_until("GUILD_UPDATE")
        b.send({"command": "GUILD_KICK", "payload": {"target": name_a}})
        a.recv_until("GUILD_UPDATE")
        b.recv_until("GUILD_UPDATE")
        b.send({"command": "GUILD_DISBAND"})
        b.recv_until("GUILD_UPDATE")

        # Party up first: friendly fire should be blocked.
        a.send({"command": "PARTY_INVITE", "payload": {"target": name_b}})
        b.recv_until("PARTY_INVITE")
        a.recv_until("PARTY_UPDATE")

        a.send({"command": "PARTY_CANCEL_INVITE", "payload": {"target": name_b}})
        a.recv_until("PARTY_UPDATE")
        b.recv_until("PARTY_UPDATE")

        a.send({"command": "PARTY_INVITE", "payload": {"target": name_b}})
        b.recv_until("PARTY_INVITE")
        a.recv_until("PARTY_UPDATE")

        b.send({"command": "PARTY_DECLINE", "payload": {"from": name_a}})
        b.recv_until("PARTY_UPDATE")
        a.recv_until("PARTY_UPDATE")

        a.send({"command": "PARTY_INVITE", "payload": {"target": name_b}})
        b.recv_until("PARTY_INVITE")
        a.recv_until("PARTY_UPDATE")

        b.send({"command": "PARTY_ACCEPT", "payload": {"from": name_a}})
        b.recv_until("PARTY_UPDATE")
        a.recv_until("PARTY_UPDATE")

        a.send({"command": "PARTY_TRANSFER_LEADER", "payload": {"target": name_b}})
        a.recv_until("PARTY_UPDATE")
        b.recv_until("PARTY_UPDATE")

        a.send({"command": "ATTACK_PVP", "payload": {"target": name_b, "skill_id": "burst_arrow"}})
        print("[DuelA]", a.recv_until("PVP_REJECTED"))

        # Disband party and try again: PvP should now resolve.
        b.send({"command": "PARTY_DISBAND"})
        b.recv_until("PARTY_UPDATE")
        a.recv_until("PARTY_UPDATE")

        a.send({"command": "ATTACK_PVP", "payload": {"target": name_b, "skill_id": "burst_arrow"}})
        print("[DuelA]", a.recv_until("PVP_RESULT"))
        print("[DuelB]", b.recv_until("PVP_HIT"))
    finally:
        a.close()
        b.close()


if __name__ == "__main__":
    main()
