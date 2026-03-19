#!/usr/bin/env python3
import json
import os
import time
import websocket


LOGIN_URL = os.getenv("A3_LOGIN_URL", "ws://127.0.0.1:5555/ws")
ZONE_A_URL = os.getenv("A3_ZONE_A_URL", "ws://127.0.0.1:7777/ws")
ZONE_B_URL = os.getenv("A3_ZONE_B_URL", "ws://127.0.0.1:7778/ws")
PASSWORD = os.getenv("A3_TEST_PASSWORD", "demo-pass")


class JsonConn:
    def __init__(self, url):
        self.sock = websocket.create_connection(url, timeout=3)

    def send(self, obj):
        self.sock.send(json.dumps(obj))

    def recv_until(self, expected_command, timeout=5.0):
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.sock.settimeout(max(0.1, deadline - time.time()))
            line = self.sock.recv()
            if not line:
                continue
            print("[raw]", line)
            msg = json.loads(line)
            if msg.get("command") == expected_command:
                return msg
        raise TimeoutError(f"timed out waiting for {expected_command}")

    def close(self):
        self.sock.close()


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


def login_token(username):
    sock = websocket.create_connection(LOGIN_URL, timeout=3)
    try:
        sock.send(json.dumps({"command": "REGISTER", "username": username, "password": PASSWORD}))
        register_msg = recv_until(sock, {"REGISTER_OK", "REGISTER_DENIED"})
        if register_msg.get("command") == "REGISTER_DENIED" and register_msg.get("payload") != "ACCOUNT_EXISTS":
            raise RuntimeError(f"unexpected register response: {register_msg!r}")
        sock.send(json.dumps({"command": "LOGIN", "username": username, "password": PASSWORD}))
        msg = recv_until(sock, {"LOGIN_OK"})
    finally:
        sock.close()
    return msg["payload"]["token"]


def auth(conn, username):
    conn.recv_until("AUTH_REQUIRED")
    conn.send({"command": "AUTH_TOKEN", "payload": {"token": login_token(username), "class": "Archer"}})
    conn.recv_until("AUTH_OK")
    conn.recv_until("ENTER_OK")
    conn.recv_until("STATE")


def expect_payload(msg, expected, label):
    actual = msg.get("payload")
    if actual != expected:
        raise AssertionError(f"{label}: expected {expected!r}, got {actual!r}")


def main():
    run_id = int(time.time())
    name_a = f"CrossA_{run_id}"
    name_b = f"CrossB_{run_id}"
    guild_name = f"CrossGuild_{run_id}"

    a = JsonConn(ZONE_A_URL)
    b = JsonConn(ZONE_B_URL)

    try:
        auth(a, name_a)
        auth(b, name_b)

        a.send({"command": "CHAT_WHISPER", "payload": {"target": name_b, "message": "cross-node hello"}})
        b.recv_until("CHAT_MESSAGE")
        a.recv_until("CHAT_MESSAGE")

        a.send({"command": "PARTY_INVITE", "payload": {"target": name_b}})
        b.recv_until("PARTY_INVITE")
        a.recv_until("PARTY_UPDATE")

        b.send({"command": "PARTY_ACCEPT", "payload": {"from": name_a}})
        join_b = b.recv_until("PARTY_UPDATE")
        join_a = a.recv_until("PARTY_UPDATE")
        if join_b.get("payload", {}).get("event") not in {"JOINED", "MEMBER_JOINED"}:
            raise AssertionError(f"unexpected party join payload on node B: {join_b!r}")
        if join_a.get("payload", {}).get("event") not in {"JOINED", "MEMBER_JOINED"}:
            raise AssertionError(f"unexpected party join payload on node A: {join_a!r}")

        a.send({"command": "PARTY_DISBAND"})
        a.recv_until("PARTY_UPDATE")
        b.recv_until("PARTY_UPDATE")

        a.send({"command": "GUILD_CREATE", "payload": {"name": guild_name}})
        a.recv_until("GUILD_UPDATE")

        a.send({"command": "GUILD_INVITE", "payload": {"target": name_b}})
        invited_b = b.recv_until("GUILD_UPDATE")
        invited_a = a.recv_until("GUILD_UPDATE")
        if invited_b.get("payload", {}).get("event") != "INVITED":
            raise AssertionError(f"unexpected guild invite payload on node B: {invited_b!r}")
        if invited_a.get("payload", {}).get("event") != "INVITE_SENT":
            raise AssertionError(f"unexpected guild invite payload on node A: {invited_a!r}")

        b.send({"command": "GUILD_ACCEPT", "payload": {"from": name_a}})
        joined_b = b.recv_until("GUILD_UPDATE")
        if joined_b.get("payload", {}).get("event") != "JOINED":
            raise AssertionError(f"unexpected guild join payload on node B: {joined_b!r}")

        a.send({"command": "CHAT_GUILD", "payload": {"message": "guild sync"}})
        guild_chat = b.recv_until("CHAT_MESSAGE")
        if guild_chat.get("payload", {}).get("channel") != "guild":
            raise AssertionError(f"expected guild chat on node B, got {guild_chat!r}")

        a.send({"command": "GUILD_DISBAND"})
        disband_b = b.recv_until("GUILD_UPDATE")
        if disband_b.get("payload", {}).get("event") != "DISBANDED":
            raise AssertionError(f"unexpected guild disband payload on node B: {disband_b!r}")
        print("cross-node social test passed")
    finally:
        a.close()
        b.close()


if __name__ == "__main__":
    main()
