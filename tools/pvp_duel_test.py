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
    a = JsonConn("127.0.0.1", 7777)
    b = JsonConn("127.0.0.1", 7777)

    try:
        auth(a, "DuelA")
        auth(b, "DuelB")

        a.send({"command": "MOVE", "payload": {"x": 102.0, "y": 0.0, "z": 102.0}})
        a.recv_until("MOVE_OK")

        b.send({"command": "MOVE", "payload": {"x": 103.0, "y": 0.0, "z": 103.0}})
        b.recv_until("MOVE_OK")

        a.send({"command": "ATTACK_PVP", "payload": {"target": "DuelB", "skill_id": "burst_arrow"}})
        print("[DuelA]", a.recv_until("PVP_RESULT"))
        print("[DuelB]", b.recv_until("PVP_HIT"))
    finally:
        a.close()
        b.close()


if __name__ == "__main__":
    main()
