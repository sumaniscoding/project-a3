#!/usr/bin/env python3
import json
import math
import time
import urllib.error
import urllib.request

import websocket

SEND_DELAY_SEC = 0.10
MOVE_STEP = 9.5
LOGIN_BASE_URL = "http://127.0.0.1:5555"
ZONE_BASE_URL = "http://127.0.0.1:7777"


class JsonConn:
    def __init__(self, host, port):
        self.sock = websocket.create_connection(f"ws://{host}:{port}/ws", timeout=5)

    def send(self, obj):
        self.sock.send(json.dumps(obj))
        time.sleep(SEND_DELAY_SEC)

    def recv_until(self, expected_command, timeout=5.0, prefix="[msg]"):
        return self.recv_until_any([expected_command], timeout=timeout, prefix=prefix)

    def recv_until_any(self, expected_commands, timeout=5.0, prefix="[msg]"):
        deadline = time.time() + timeout
        while time.time() < deadline:
            self.sock.settimeout(max(0.1, deadline - time.time()))
            try:
                line = self.sock.recv()
            except websocket.WebSocketTimeoutException:
                continue
            except Exception as exc:
                print(f"{prefix} recv error: {exc}")
                continue
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
        self.sock.close()


def http_get_json(url, expected_status=200):
    try:
        with urllib.request.urlopen(url, timeout=5) as response:
            status = response.status
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        status = exc.code
        payload = json.loads(exc.read().decode("utf-8"))
    if status != expected_status:
        raise RuntimeError(f"GET {url} returned {status}, expected {expected_status}: {payload!r}")
    return payload


def assert_health_endpoints():
    login_health = http_get_json(f"{LOGIN_BASE_URL}/healthz")
    if login_health.get("status") != "ok":
        raise RuntimeError(f"unexpected login /healthz payload: {login_health!r}")

    login_ready = http_get_json(f"{LOGIN_BASE_URL}/readyz")
    if login_ready.get("status") != "ready":
        raise RuntimeError(f"unexpected login /readyz payload: {login_ready!r}")

    zone_health = http_get_json(f"{ZONE_BASE_URL}/healthz")
    if zone_health.get("status") != "ok":
        raise RuntimeError(f"unexpected zone /healthz payload: {zone_health!r}")

    zone_ready = http_get_json(f"{ZONE_BASE_URL}/readyz")
    if zone_ready.get("status") != "ready":
        raise RuntimeError(f"unexpected zone /readyz payload: {zone_ready!r}")


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

        conn.send({"command": "REGISTER", "username": username, "password": password})
        register_msg = conn.recv_until_any(["REGISTER_OK", "REGISTER_DENIED"], prefix="[login]")
        if register_msg.get("command") == "REGISTER_DENIED" and register_msg.get("payload") != "ACCOUNT_EXISTS":
            raise RuntimeError(f"unexpected register response: {register_msg!r}")

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


def move_to(conn, current_pos, target_pos, prefix):
    x0, y0, z0 = current_pos
    x1, y1, z1 = target_pos
    dx = x1 - x0
    dy = y1 - y0
    dz = z1 - z0
    distance = math.sqrt(dx * dx + dy * dy + dz * dz)
    if distance == 0:
        return target_pos

    steps = max(1, math.ceil(distance / MOVE_STEP))
    for idx in range(1, steps + 1):
        ratio = idx / steps
        step_pos = {
            "x": x0 + dx * ratio,
            "y": y0 + dy * ratio,
            "z": z0 + dz * ratio,
        }
        conn.send({"command": "MOVE", "payload": step_pos})
        move_ok = conn.recv_until("MOVE_OK", prefix=prefix)
        payload = move_ok.get("payload", {})
        current_pos = (
            float(payload.get("x", step_pos["x"])),
            float(payload.get("y", step_pos["y"])),
            float(payload.get("z", step_pos["z"])),
        )
    return current_pos


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


def find_visible_mob(payload, mob_id):
    mobs = payload.get("mobs", [])
    if not isinstance(mobs, list):
        return None
    for mob in mobs:
        if isinstance(mob, dict) and mob.get("id") == mob_id:
            return mob
    return None


def defeat_mob(conn, mob_id, skill_id, prefix):
    for _ in range(18):
        conn.send({"command": "ATTACK_MOB", "payload": {"mob_id": mob_id, "skill_id": skill_id}})
        result = conn.recv_until_any(["MOB_ATTACK_RESULT", "MOB_ATTACK_REJECTED"], timeout=6.0, prefix=prefix)
        if result.get("command") == "MOB_ATTACK_REJECTED":
            payload = result.get("payload")
            if payload == "MOB_ALREADY_DEFEATED":
                time.sleep(0.35)
                continue
            raise RuntimeError(f"unexpected mob attack rejection: {result!r}")

        payload = result.get("payload", {})
        if payload.get("status") == "PLAYER_DIED":
            conn.send({"command": "RECOVER_CORPSE"})
            conn.recv_until("CORPSE_RECOVERY", prefix=prefix)
            continue
        if payload.get("defeated"):
            drops = payload.get("drops", [])
            if not isinstance(drops, list):
                raise RuntimeError(f"expected drops list, got {payload!r}")
            return payload
    raise RuntimeError(f"failed to defeat {mob_id} in smoke test")


def test_zone(token):
    print("[zone] connecting to :7777")
    conn = JsonConn("127.0.0.1", 7777)
    try:
        conn.recv_until("AUTH_REQUIRED", prefix="[zone]")
        conn.send({"command": "AUTH_TOKEN", "payload": {"token": token, "class": "Mage"}})
        conn.recv_until("AUTH_OK", prefix="[zone]")
        conn.recv_until("ENTER_OK", prefix="[zone]")
        state_msg = conn.recv_until("STATE", prefix="[zone]")
        state_payload = state_msg.get("payload", {})

        if state_payload.get("class") != "Mage":
            raise RuntimeError(f"expected Mage state after auth, got {state_payload!r}")
        if not state_payload.get("world"):
            raise RuntimeError(f"unexpected starting world: {state_payload!r}")

        conn.send({"command": "STORAGE_VIEW"})
        conn.recv_until("STORAGE_STATE", prefix="[zone]")

        deposit_amount = min(25, as_int(state_payload.get("gold")))
        if deposit_amount <= 0:
            raise RuntimeError(f"expected positive starter gold, got {state_payload!r}")

        conn.send({"command": "STORAGE_DEPOSIT_GOLD", "payload": {"amount": deposit_amount}})
        gold_deposit = conn.recv_until("STORAGE_STATE", prefix="[zone]")
        gold_payload = gold_deposit.get("payload", {})
        if as_int(gold_payload.get("wallet_gold")) < deposit_amount:
            raise RuntimeError(f"expected wallet gold after deposit, got {gold_payload!r}")

        conn.send({"command": "STORAGE_WITHDRAW_GOLD", "payload": {"amount": deposit_amount}})
        conn.recv_until("STORAGE_STATE", prefix="[zone]")

        current_pos = (0.0, 0.0, 0.0)
        current_pos = move_to(conn, current_pos, (86.0, 0.0, 86.0), "[zone]")

        conn.send({"command": "LIST_ENTITIES"})
        entities_msg = conn.recv_until("ENTITIES", prefix="[zone]")
        entities_payload = entities_msg.get("payload", {})
        if find_visible_mob(entities_payload, "mob_wolf_01") is None:
            raise RuntimeError(f"expected mob_wolf_01 to be visible near farming position, got {entities_payload!r}")

        conn.send({"command": "GET_STATE"})
        before_fight = conn.recv_until("STATE", prefix="[zone]")
        materials_before = as_materials_map(before_fight.get("payload", {}))

        result = defeat_mob(conn, "mob_wolf_01", "arc_bolt", "[zone]")
        if not result.get("defeated"):
            raise RuntimeError(f"expected defeated mob payload, got {result!r}")

        material_drops = {}
        for drop in result.get("drops", []):
            if not isinstance(drop, dict):
                continue
            if drop.get("kind") == "material":
                item_id = drop.get("item_id")
                qty = as_int(drop.get("qty"))
                if item_id and qty > 0:
                    material_drops[item_id] = material_drops.get(item_id, 0) + qty
        if not material_drops:
            raise RuntimeError(f"expected at least one material drop, got {result!r}")

        conn.send({"command": "GET_STATE"})
        after_fight = conn.recv_until("STATE", prefix="[zone]")
        materials_after = as_materials_map(after_fight.get("payload", {}))
        for item_id, qty in material_drops.items():
            delta = materials_after.get(item_id, 0) - materials_before.get(item_id, 0)
            if delta != qty:
                raise RuntimeError(
                    f"material persistence mismatch for {item_id}: before={materials_before.get(item_id, 0)} after={materials_after.get(item_id, 0)} expected_delta={qty}"
                )

        move_to(conn, current_pos, (-10.0, 0.0, 5.0), "[zone]")

        deposit_item_id = next(iter(material_drops.keys()))
        conn.send({"command": "STORAGE_DEPOSIT_MATERIAL", "payload": {"item_id": deposit_item_id, "qty": 1}})
        storage_deposit = conn.recv_until("STORAGE_STATE", prefix="[zone]")
        storage_payload = storage_deposit.get("payload", {})
        storage_materials = storage_payload.get("storage", {}).get("materials", {})
        if as_int(storage_materials.get(deposit_item_id)) < 1:
            raise RuntimeError(f"expected {deposit_item_id} in storage after deposit, got {storage_payload!r}")

        conn.send({"command": "STORAGE_WITHDRAW_MATERIAL", "payload": {"item_id": deposit_item_id, "qty": 1}})
        conn.recv_until("STORAGE_STATE", prefix="[zone]")
    finally:
        conn.close()

    _, reauth_state = auth_and_get_state(token, "Archer", "[reauth]")
    reauth_materials = as_materials_map(reauth_state)
    for item_id, qty in material_drops.items():
        if reauth_materials.get(item_id, 0) < materials_before.get(item_id, 0) + qty:
            raise RuntimeError(
                f"expected persisted material {item_id} after reconnect, got {reauth_materials!r}"
            )
    if reauth_state.get("class") != "Mage":
        raise RuntimeError(f"expected sticky class Mage after reconnect, got {reauth_state!r}")


if __name__ == "__main__":
    assert_health_endpoints()

    class_username = f"SmokeClass_{int(time.time())}"
    class_token = login_and_get_token(class_username, "demo-pass")
    test_class_bootstrap_and_sticky(class_token)

    username = f"SmokeHero_{int(time.time())}"
    token = login_and_get_token(username, "demo-pass")
    test_zone(token)
    print("smoke test passed")
