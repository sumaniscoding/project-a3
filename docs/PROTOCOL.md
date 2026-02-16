# Project A3 Protocol Notes

All messages are JSON objects terminated by a newline (`\\n`).

## LoginServer (`:5555`)

### Requests

- `{"command":"PING"}`
- `{"command":"LOGIN","username":"demo","password":"demo"}`
- `{"command":"VALIDATE","token":"<signed token>"}` (debug validation)

### Responses

- `PONG` with timestamp payload
- `LOGIN_OK` with `username`, `token`, and `expires`
- `RATE_LIMITED` with `retry_after_sec` when peer IP login attempts exceed throttle limits
- `TOKEN_VALID` / `TOKEN_INVALID` for validation checks
- `LOGIN_DENIED` for missing credentials
- `ERROR` for invalid JSON/unknown command/internal error

Token notes:

- Tokens are HMAC-signed and include `username` + expiry claims.
- Tokens also include `iss`, `ver`, and `iat` claims and are rejected if invalid.
- LoginServer and ZoneServer must share `A3_AUTH_SECRET` (defaults to a dev secret if unset).
- In production (`A3_ENV=prod`), startup fails if `A3_AUTH_SECRET` is unset or default.

## ZoneServer (`:7777`)

### Initial flow

On connect, server immediately responds:

- `AUTH_REQUIRED` with `LOGIN_REQUIRED`

After a valid `AUTH_TOKEN`, server loads/creates the character, evaluates world gates, and then sends:

- `AUTH_OK` with character identity
- `ENTER_OK` with character/world/spawn
- `STATE` snapshot

If chosen world is not valid for the character, it falls back to World 1 before `ENTER_OK`.

Authentication enforcement:

- In normal flow, clients must call `AUTH_TOKEN` before gameplay/state commands.
- Before auth, non-auth commands return `AUTH_REQUIRED` with `LOGIN_REQUIRED`.
- Unauthenticated sockets time out after 15 seconds if `AUTH_TOKEN` is not provided.
- After 3 invalid `AUTH_TOKEN` attempts, server returns `AUTH_LOCKED` and closes session.
- If peer-IP auth throttle is exceeded, server returns `AUTH_LOCKED` with `reason=TOO_MANY_ATTEMPTS` and `retry_after_sec`, then closes.
- Per-session command flood protection returns `RATE_LIMITED` when limits are exceeded.
- Cross-connection auth throttling is applied per peer IP for both LoginServer and ZoneServer.
- Unknown commands now return `ERROR` with payload `UNKNOWN_COMMAND`.

### Movement

Client sends:

- `{"command":"MOVE","payload":{"x":<float>,"y":<float>,"z":<float>}}`

Rules:

- anti-teleport limit: max 10 units per update

Responses:

- `MOVE_OK` with accepted position
- `MOVE_REJECTED` with `INVALID_MOVE`

### Progression and narrative commands

- `AUTH_TOKEN` with payload `{"token":"<login token>","class":"Archer"}` loads/creates persisted character state from token username.
- `GET_STATE`: full character/world snapshot
- `GET_HISTORY`: world unlock pioneer history
- `LIST_ENTITIES`: nearby NPC/mob entities in current world
- `ENTER_WORLD` with payload `{"world_id":2}` to switch worlds when unlocked
- `TALK_NPC` with payload `{"npc":"Elder Rowan","choice":"honor"}` updates trust
- `ACCEPT_QUEST` and `COMPLETE_QUEST` for owner-bound quest progression
- `GET_RECIPES` to list crafting recipes and known material definitions
- `CRAFT_ITEM` with payload `{"recipe_id":"wolfhide_bow","qty":1}` to craft gear from stackable materials
- `CHAT_SAY` with payload `{"message":"hello"}` for local proximity chat in current world
- `CHAT_WORLD` with payload `{"message":"hello world"}` for world-wide chat in current world
- `WHO` for authenticated online roster
- `PARTY_INVITE`, `PARTY_ACCEPT`, `PARTY_LEAVE` for party flow
- `GUILD_CREATE`, `GUILD_JOIN`, `GUILD_LEAVE`, `GUILD_LIST` for guild scaffold flow
- `CHAT_GUILD` with payload `{"message":"hello guild"}` for guild channel chat
- `GUILD_MEMBERS` to list guild roster and online state

Quest-related behavior implemented from the GDD:

- race-based unlock for World 2 (`unlock_world2_race`)
- aura + unlock path for World 3 (`unlock_world3_legend`)
- hidden trust-gated quest (`npc_oath_hidden`)
- non-repeatable legendary quest paths (`grace_legacy`, `soul_legacy`)

### Combat and build systems

- `ATTACK` with payload `{"target":"rift_wolf","target_level":44}`
- `ATTACK_MOB` with payload `{"mob_id":"mob_wolf_01","skill_id":"burst_arrow"}`
- `ATTACK_PVP` with payload `{"target":"playerX","skill_id":"burst_arrow"}`
- `RECOVER_CORPSE` to reduce XP debt after death
- `EQUIP_ITEM` to equip weapon/armor items from inventory
- `SUMMON_PET` to summon a passive-bonus pet
- `RECRUIT_MERC` to recruit class-based mercenary support
- `SET_ELEMENT` with payload `{"target":"weapon|armor|pet","element":"Fire|Ice|Lightning|Earth|Light|Dark"}`
- `SKILL_TREE` and `LEARN_SKILL` for class skill progression
- `GET_RECIPES` and `CRAFT_ITEM` for the Epic 4 loot/crafting loop

Combat/progression behaviors:

- server-authoritative damage and XP processing
- death penalty with XP debt and corpse recovery flow
- PvP penalty scaling based on attacker-victim level difference
- PvP target resolution is session-based (`target` must be an online character name in same world and range)
- runtime mobs with HP, defeat, and respawn timers
- mob-specific loot tables that can drop stackable materials and occasional gear template rewards
- nearby party members receive shared XP on mob kills, and killers gain party bonus XP/loot when grouped
- extreme-rarity legendary drop roll for Grace/Soul items
- gear grades and rarity tiers influence attack output
- `ATTACK_PVP` blocks same-party friendly fire by default (`FRIENDLY_FIRE_BLOCKED`)

Crafting behaviors:

- recipe catalog is server-defined (code-driven)
- `CRAFT_ITEM` validates recipe existence, level, quantity (`1..20`), and material sufficiency
- successful crafting consumes `materials` and appends unique crafted gear into `inventory`
- `STATE` now includes additive `materials` data without removing any existing fields
- `STATE` also includes additive `guild` and `party` fields

`MOB_ATTACK_RESULT` additive payload field:

- `drops`: array describing material and gear rewards when `defeated=true`
- existing `legendary` field remains unchanged for backward compatibility

Social response messages:

- `CHAT_MESSAGE` broadcast payload includes `channel`, `from`, `world`, `message`, `ts`
- `WHO_LIST` returns online character metadata
- party uses `PARTY_INVITE`, `PARTY_UPDATE`, and `PARTY_REJECTED`
- guild uses `GUILD_UPDATE`, `GUILD_LIST`, and `GUILD_REJECTED`
- guild member roster uses `GUILD_MEMBERS`

### Persistence

- Character state is persisted in SQLite at `data/characters.db` (table: `characters`).
- `A3_PERSISTENCE_MODE=db` (default): DB-only mode, no runtime JSON fallback.
- `A3_PERSISTENCE_MODE=hybrid`: DB + legacy JSON fallback for migration windows.
- `A3_PERSISTENCE_MODE=json`: legacy JSON-only mode.
- On DB initialization, legacy files under `data/characters/` are auto-migrated into SQLite.
- Persistence runs on character-modifying commands and on disconnect.

### Visibility

Players in same world within `VisibilityRadius` (50 units) receive:

- `PLAYER_JOINED`
- `PLAYER_MOVED`
- `PLAYER_LEFT`
