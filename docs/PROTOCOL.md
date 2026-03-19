# Project A3 Protocol Notes

All messages are JSON objects terminated by a newline (`\\n`).

## LoginServer (`:5555`)

### Requests

- `{"command":"PING"}`
- `{"command":"REGISTER","username":"demo","password":"demo-pass"}`
- `{"command":"LOGIN","username":"demo","password":"demo-pass"}`
- `{"command":"VALIDATE","token":"<signed token>"}` (debug validation)

### Responses

- `PONG` with timestamp payload
- `REGISTER_OK` with `username`
- `REGISTER_DENIED` with `ACCOUNT_EXISTS` or `MISSING_CREDENTIALS`
- `LOGIN_OK` with `username`, `token`, and `expires`
- `RATE_LIMITED` with `retry_after_sec` when peer IP login attempts exceed throttle limits
- `TOKEN_VALID` / `TOKEN_INVALID` for validation checks
- `LOGIN_DENIED` for missing or invalid credentials
- `ERROR` for invalid JSON/unknown command/internal error

Token notes:

- Tokens are HMAC-signed and include `username` + expiry claims.
- Tokens also include `iss`, `ver`, and `iat` claims and are rejected if invalid.
- LoginServer and ZoneServer must share `A3_AUTH_SECRET` (defaults to a dev secret if unset).
- In production (`A3_ENV=prod`), startup fails if `A3_AUTH_SECRET` is unset or default.
- Login credentials are stored in SQLite at `server/LoginServer/data/login_accounts.db`.
- Browser-origin WebSocket connections must match `A3_ALLOWED_ORIGINS` when set; native clients without an `Origin` header remain allowed.

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
  Supported classes: `Archer`, `Mage`, `Warrior`, `Healing Knight`.
  `class` is applied at initial character creation; existing characters keep their persisted class on later auths.
- `GET_STATE`: full character/world snapshot
- `GET_HISTORY`: world unlock pioneer history
- `LIST_ENTITIES`: nearby NPC/mob entities in current world
- `ENTER_WORLD` with payload `{"world_id":2}` to switch worlds when unlocked
- `TALK_NPC` with payload `{"npc":"Elder Rowan","choice":"honor"}` updates trust
- `ACCEPT_QUEST` and `COMPLETE_QUEST` for owner-bound quest progression
- `GET_RECIPES` to list crafting recipes and known material definitions
- `CRAFT_ITEM` with payload `{"recipe_id":"wolfhide_bow","qty":1}` to craft gear from stackable materials
- `UPGRADE_GEAR` with payload `{"item_id":"<inventory item id>"}` to attempt gear level upgrades (`1..10`) using enhance gems
- `PET_FEED` with payload `{"qty":1}` to consume `pet_treat` materials for pet XP/levels (cap 100)
  Rejection reason includes `PET_NOT_ACQUIRED` before pet unlock.
- `STORAGE_VIEW` to fetch account-scoped storage/wallet state (requires nearby storage NPC)
- `STORAGE_DEPOSIT_MATERIAL` / `STORAGE_WITHDRAW_MATERIAL` with payload `{"item_id":"...","qty":1}`
- `STORAGE_DEPOSIT_ITEM` / `STORAGE_WITHDRAW_ITEM` with payload `{"item_id":"..."}`
- `STORAGE_DEPOSIT_GOLD` / `STORAGE_WITHDRAW_GOLD` with payload `{"amount":100}`
- `CHAT_SAY` with payload `{"message":"hello"}` for local proximity chat in current world
- `CHAT_WORLD` with payload `{"message":"hello world"}` for world-wide chat in current world
- `CHAT_WHISPER` with payload `{"target":"PlayerName","message":"psst"}` for direct private messages across ZoneServer nodes when Redis is available
- `SET_PRESENCE` with payload `{"status":"online|afk|dnd"}` to update presence state
- `GET_PRESENCE` to fetch the current presence state
- `WHO` for authenticated online roster
- `FRIEND_REQUEST` with payload `{"target":"PlayerName"}` for online friend invite
- `FRIEND_CANCEL_REQUEST` with payload `{"target":"PlayerName"}` to revoke a pending friend invite you sent
- `FRIEND_ACCEPT` with payload `{"from":"PlayerName"}` to accept pending friend invite
- `FRIEND_DECLINE` with payload `{"from":"PlayerName"}` to reject a pending friend invite
- `FRIEND_REMOVE` with payload `{"target":"PlayerName"}` to remove a friend link
- `FRIEND_LIST` to return the current friend roster
- `FRIEND_STATUS` to return friend roster plus online/world/guild/level metadata when available
- `BLOCK_PLAYER` with payload `{"target":"PlayerName"}` to block direct interactions
- `UNBLOCK_PLAYER` with payload `{"target":"PlayerName"}` to remove a block
- `BLOCK_LIST` to return the current blocked roster
- `PARTY_INVITE`, `PARTY_ACCEPT`, `PARTY_LEAVE` for party flow, including cross-node invite delivery when Redis is available
- `PARTY_CANCEL_INVITE` with payload `{"target":"PlayerName"}` to revoke a pending invite you sent
- `PARTY_DECLINE` with payload `{"from":"PlayerName"}` to reject a pending party invite
- `PARTY_READY` with payload `{"ready":true|false}` to mark encounter readiness
- `PARTY_STATUS` to retrieve party roster + ready-state snapshot
- `PARTY_KICK` with payload `{"target":"PlayerName"}` for leader-only removal
- `PARTY_TRANSFER_LEADER` with payload `{"target":"PlayerName"}` for leader handoff
- `PARTY_DISBAND` for leader-only immediate party dissolution
- `CHAT_PARTY` with payload `{"message":"..."}` for party channel chat
- `GUILD_CREATE`, `GUILD_JOIN`, `GUILD_LEAVE`, `GUILD_LIST` for guild scaffold flow
- `GUILD_DISBAND` for leader-only immediate guild dissolution
- `CHAT_GUILD` with payload `{"message":"hello guild"}` for guild channel chat
- `GUILD_MEMBERS` to list guild roster and online state
- `GUILD_INVITE` with payload `{"target":"PlayerName"}` for leader/officer invites, including cross-node invite delivery when Redis is available
- `GUILD_CANCEL_INVITE` with payload `{"target":"PlayerName"}` to revoke a pending guild invite you sent
- `GUILD_ACCEPT` with payload `{"from":"LeaderName"}` to accept pending guild invite
- `GUILD_DECLINE` with payload `{"from":"LeaderName"}` to reject a pending guild invite
- `GUILD_PROMOTE` with payload `{"target":"PlayerName"}` for leader-only member->officer promotion
- `GUILD_DEMOTE` with payload `{"target":"PlayerName"}` for leader-only officer->member demotion

Quest-related behavior implemented from the GDD:

- race-based unlock for World 2 (`unlock_world2_race`)
- completing `unlock_world2_race` also unlocks pet acquisition (`pet_unlocked=true`)
- aura + unlock path for World 3 (`unlock_world3_legend`)
- hidden trust-gated quest (`npc_oath_hidden`)
- non-repeatable legendary quest paths (`grace_legacy`, `soul_legacy`)

### Combat and build systems

- `ATTACK` with payload `{"target":"rift_wolf","target_level":44}`
- `ATTACK_MOB` with payload `{"mob_id":"mob_wolf_01","skill_id":"burst_arrow"}`
- `ATTACK_PVP` with payload `{"target":"playerX","skill_id":"burst_arrow"}`
- `RECOVER_CORPSE` to reduce XP debt after death
- `EQUIP_ITEM` to equip slot-based gear from inventory
- `SUMMON_PET` to summon a passive-bonus pet
  Rejection reason includes `PET_NOT_ACQUIRED` before pet unlock.
- `RECRUIT_MERC` to recruit class-based mercenary support
- `MERC_EQUIP_ITEM` with payload `{"item_id":"<inventory item id>"}` to equip mercenary gear
- `MERC_UNEQUIP_ITEM` with payload `{"slot":"weapon|armor|helmet|gloves|boots|pants|necklace|ring|shield"}`
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
- equipped gear also contributes via additive `gear_level` bonus
- gear equips enforce item `min_str`/`min_dex`; `shield` slot is class-restricted to `Healing Knight`
- mercenary equips follow same slot/stat model and use `MERC_UPDATE` / `MERC_REJECTED`
- same item instance cannot be simultaneously equipped by mercenary and player (`ITEM_ALREADY_EQUIPPED_BY_MERC`)
- `ATTACK_PVP` blocks same-party friendly fire by default (`FRIENDLY_FIRE_BLOCKED`)

Crafting behaviors:

- recipe catalog is server-defined (code-driven)
- `CRAFT_ITEM` validates recipe existence, level, quantity (`1..20`), and material sufficiency
- `CRAFT_ITEM` failures return `CRAFT_REJECTED` with reasons:
  `RECIPE_NOT_FOUND`, `INSUFFICIENT_MATERIALS`, `INVALID_QTY`, `LEVEL_TOO_LOW`, `RECIPE_OUTPUT_INVALID`
- successful crafting consumes `materials` and appends unique crafted gear into `inventory`
- material catalog now includes enhancement gems (`enhance_gem_t1|t2|t3`) and `pet_treat`
- recipe catalog includes slot coverage craftables such as `guardian_shield`, `hunter_helm`, `hunter_gloves`,
  `hunter_boots`, `hunter_pants`, `sigil_ring`, and `sigil_necklace`
- `UPGRADE_GEAR` consumes enhancement gems by gear level tier with scaled per-level gem costs
- high-tier (`+8/+9`) upgrade failures can apply additive `failure_effect="DOWNGRADED"` and reduce gear level by 1
- `GEAR_UPGRADE_RESULT` includes additive `old_gear_level`, `new_gear_level`, `cost`, and `failure_effect`
- `STATE` now includes additive `materials` data without removing any existing fields
- `STATE` includes additive `strength`, `dexterity`, `gold`, `wallet_gold`, and `storage`
- `STATE` also includes additive `guild` and `party` fields
- `STATE` also includes additive `guild_role` for role-aware social flows
- `STATE` also includes additive `friends` list of known friend character names
- `STATE` also includes additive `blocked` list of blocked character names
- `STATE` also includes additive `presence` (`online|afk|dnd`)

`MOB_ATTACK_RESULT` additive payload field:

- `drops`: array describing material and gear rewards when `defeated=true`
- existing `legendary` field remains unchanged for backward compatibility

Storage behaviors:

- storage is account-scoped with max `1000` total stacks (`storage.materials` non-zero keys + `storage.items`)
- storage requires nearby `Storage Keeper` NPC visibility to access storage commands; out-of-range requests return `STORAGE_REJECTED` with `STORAGE_NPC_REQUIRED`
- weapons/armor/ring/necklace and other gear-slot items are rejected for box storage (`ITEM_NOT_STORABLE`)
- storage deposit rejects any currently equipped item instance (`ITEM_EQUIPPED`)
- storage wallet uses deposit/withdraw gold commands; `STATE` still carries current on-character `gold`

Social response messages:

- `CHAT_MESSAGE` broadcast payload includes `channel`, `from`, `world`, `message`, `ts`
- whisper chat uses `CHAT_MESSAGE` with channel `whisper` and payload includes `to`
- whisper failures return `WHISPER_REJECTED` (`TARGET_REQUIRED`, `INVALID_TARGET`, `TARGET_OFFLINE`, `TARGET_BLOCKED_YOU`, `TARGET_BLOCKED_BY_YOU`)
- block-list filtering now also suppresses `say/world/party/guild` chat delivery between blocked pairs
- presence uses `PRESENCE_UPDATE` and `PRESENCE_REJECTED` (`INVALID_STATUS`)
- friend flow uses `FRIEND_UPDATE`, `FRIEND_LIST`, and `FRIEND_REJECTED`
- friend invite acceptance fails with `INVITE_EXPIRED` after TTL
- friend invite may be rejected with `BLOCKED` if either side has blocked the other
- `FRIEND_STATUS` response includes additive `entries` and `online_count`
- friend invite cancel sends `FRIEND_UPDATE` with `INVITE_CANCELED` to target when online
- friend invite decline sends `FRIEND_UPDATE` with `INVITE_DECLINED` to inviter when online
- `WHO_LIST` and `FRIEND_STATUS.entries` include additive presence/status metadata
- block flow uses `BLOCK_UPDATE`, `BLOCK_LIST`, and `BLOCK_REJECTED`
- `WHO_LIST` returns online character metadata
- party uses `PARTY_INVITE`, `PARTY_UPDATE`, and `PARTY_REJECTED`
- party status snapshots use `PARTY_STATUS` and include per-member ready state
- party invite may be rejected with `BLOCKED` if either side has blocked the other
- party invite acceptance fails with `INVITE_EXPIRED` after TTL
- party invite acceptance may be rejected with `BLOCKED` if either side blocks before accept
- party invite decline sends `PARTY_UPDATE` with `INVITE_DECLINED` to inviter when online
- party invite cancel sends `PARTY_UPDATE` with `INVITE_CANCELED` to target when online
- party leadership handoff uses `PARTY_TRANSFER_LEADER` and emits `PARTY_UPDATE` events
- party disband uses `PARTY_DISBAND` and emits `PARTY_UPDATE` event `PARTY_DISBANDED` to all online members
- guild uses `GUILD_UPDATE`, `GUILD_LIST`, and `GUILD_REJECTED`
- guild member roster uses `GUILD_MEMBERS`
- guild membership now tracks roles (`leader`/`officer`/`member`) and invite TTL (`INVITE_EXPIRED`)
- guild invite may be rejected with `BLOCKED` if either side has blocked the other
- guild invite acceptance may be rejected with `BLOCKED` if either side blocks before accept
- guild invite cancel sends `GUILD_UPDATE` with `INVITE_CANCELED` to target when online
- guild invite decline sends `GUILD_UPDATE` with `INVITE_DECLINED` to inviter when online
- guild disband uses `GUILD_DISBAND` and emits `GUILD_UPDATE` event `DISBANDED` to online members

### Persistence

- Character state is persisted in SQLite at `data/characters.db` (table: `characters`).
- Account-scoped storage and wallet are persisted in SQLite at `data/characters.db` (table: `accounts`), keyed by login username.
- On first auth after this feature rollout, legacy character-scoped `wallet_gold/storage` are backfilled into account scope if the account payload is empty.
- `A3_PERSISTENCE_MODE=db` (default): DB-only mode, no runtime JSON fallback.
- `A3_PERSISTENCE_MODE=hybrid`: DB + legacy JSON fallback for migration windows.
- `A3_PERSISTENCE_MODE=json`: legacy JSON-only mode.
- On DB initialization, legacy files under `data/characters/` are auto-migrated into SQLite.
- In JSON mode, account payload fallback files are stored under `data/accounts/`.
- Persistence runs on character-modifying commands and on disconnect.

### Visibility

Players in same world within `VisibilityRadius` (50 units) receive:

- `PLAYER_JOINED`
- `PLAYER_MOVED`
- `PLAYER_LEFT`
