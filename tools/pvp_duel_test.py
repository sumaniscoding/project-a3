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

    def recv_until(self, expected_command, timeout=5.0):
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.sock.settimeout(max(0.1, deadline - time.time()))
            line = self.file.readline()
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
        try:
            self.file.close()
        finally:
            self.sock.close()


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
    with socket.create_connection(("127.0.0.1", 5555), timeout=3) as sock:
        sock.sendall((json.dumps({"command": "LOGIN", "username": username, "password": "demo"}) + "\n").encode("utf-8"))
        line = b""
        while not line.endswith(b"\n"):
            chunk = sock.recv(4096)
            if not chunk:
                break
            line += chunk
    msg = json.loads(line.decode("utf-8").strip())
    return msg["payload"]["token"]


def main():
    run_id = int(time.time())
    name_a = f"DuelA_{run_id}"
    name_b = f"DuelB_{run_id}"

    a = JsonConn("127.0.0.1", 7777)
    b = JsonConn("127.0.0.1", 7777)

    try:
        auth(a, name_a)
        auth(b, name_b)

        a.send({"command": "MOVE", "payload": {"x": 102.0, "y": 0.0, "z": 102.0}})
        a.recv_until("MOVE_OK")

        b.send({"command": "MOVE", "payload": {"x": 103.0, "y": 0.0, "z": 103.0}})
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
