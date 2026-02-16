# Project A3

Project A3 is a server-authoritative MMO backend prototype with two services:

- LoginServer (`:5555`) for auth/session bootstrap
- ZoneServer (`:7777`) for world progression, quests, movement, combat, and visibility

Implemented gameplay slices now include:

- world unlock race history, NPC trust, hidden/non-repeatable quests
- combat progression with XP debt + corpse recovery
- runtime NPC/mob entities with mob HP/respawn
- class skill trees and skill point spending
- PvP penalty scaling by level difference with online target resolution
- persisted character progression/state in SQLite (`data/characters.db`) with configurable persistence mode
- gear/pet/mercenary/element systems as combat modifiers
- token-authenticated ZoneServer identity binding (`AUTH_TOKEN`)

## Folder layout

- `server/LoginServer`: standalone login server module
- `server/zoneserver/ZoneServer`: standalone zone server module
- `docs/`: protocol and behavior notes
- `tools/`: local smoke test utility

## Run

### LoginServer

```bash
cd server/LoginServer
go run .
```

### ZoneServer

```bash
cd server/zoneserver/ZoneServer
go run .
```

Both services should use the same auth secret:

```bash
export A3_AUTH_SECRET=\"change-me\"
```

For production:

```bash
export A3_ENV=\"prod\"
```

In production mode, both LoginServer and ZoneServer fail fast if `A3_AUTH_SECRET` is missing or left at default.
ZoneServer also enforces:

- unauthenticated auth timeout (15s)
- auth lockout after repeated invalid tokens
- per-session command rate limiting
- peer-IP auth throttling across reconnect attempts (LoginServer and ZoneServer)

Persistence mode (ZoneServer):

- default `A3_PERSISTENCE_MODE=db` (DB-only, strict)
- `A3_PERSISTENCE_MODE=hybrid` for DB + legacy JSON fallback
- `A3_PERSISTENCE_MODE=json` for legacy JSON-only mode
- on DB init, legacy files in `data/characters/*.json` are auto-migrated into SQLite

## Smoke test

With both services running:

```bash
python3 tools/smoke_test.py
```

This sends:

1. `PING` and `LOGIN` to LoginServer
2. GDD-driven ZoneServer commands: state, NPC trust, quest accept/complete, world entry, pet/merc setup, elemental affinity, combat, and movement validation

For real two-client PvP:

```bash
python3 tools/pvp_duel_test.py
```
