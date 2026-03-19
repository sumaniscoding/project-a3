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
- Postgres-backed ZoneServer persistence when `A3_DB_BACKEND=postgres`
- gear/pet/mercenary/element systems as combat modifiers
- token-authenticated ZoneServer identity binding (`AUTH_TOKEN`)
- account-backed LoginServer auth with the same configurable DB backend as ZoneServer

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

Browser WebSocket origins are restricted by default to loopback hosts. To allow deployed frontends explicitly:

```bash
export A3_ALLOWED_ORIGINS=\"https://game.example.com,https://admin.example.com\"
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
- default DB backend is SQLite for local development
- set `A3_DB_BACKEND=postgres` and `A3_DATABASE_URL=postgres://...` to use Postgres for both LoginServer and ZoneServer
- on DB init, legacy files in `data/characters/*.json` and `data/accounts/*.json` are auto-migrated into the configured DB backend

LoginServer auth storage:

- default backend is SQLite at `server/LoginServer/data/login_accounts.db`
- when `A3_DB_BACKEND=postgres`, LoginServer stores `login_accounts` in the same Postgres database referenced by `A3_DATABASE_URL`

Health/readiness endpoints:

- LoginServer: `GET /healthz`, `GET /readyz`
- ZoneServer: `GET /healthz`, `GET /readyz`

## Smoke test

With both services running:

```bash
python3 tools/smoke_test.py
```

This sends:

1. `PING`, `REGISTER`, and `LOGIN` to LoginServer
2. ZoneServer commands covering auth, state, movement, storage, combat, persistence, and reconnect validation

For real two-client PvP:

```bash
python3 tools/pvp_duel_test.py
```

## CI

GitHub Actions backend validation is defined in:

- `.github/workflows/backend.yml`

It runs:

1. `go test ./...` for LoginServer
2. `go test ./...` for ZoneServer
3. a live smoke test against LoginServer + Postgres-backed ZoneServer + Redis
