# Generic Game Scoreboard Backend

A lightweight, secure scoreboard backend for games. Single binary — Go + SQLite + embedded frontend.

## Features

- **Score submission** with HMAC-SHA256 authentication (anti-tamper + anti-replay)
- **Top-N leaderboard** (10 / 50 / 100) with in-memory caching
- **Player rank lookup** by name
- **Single-page scoreboard UI** — dark theme, responsive
- **Weighted score formula** — fully configurable via environment variables
- **Hardened systemd service** — `DynamicUser`, `NoNewPrivileges`, `ProtectSystem=strict`
- **Zero external dependencies** except the native SQLite driver
- **Single binary** — frontend and assets embedded at compile time

## Quick Start

```bash
# Clone
git clone https://github.com/furkanbaytekin/generic-game-scoreboard-backend.git
cd generic-game-scoreboard-backend

# Configure
cp .env.example .env
# Edit .env — at minimum set HMAC_SECRET

# Run
make run
```

Open http://localhost:8080

## Configuration

All settings via environment variables or `.env` file:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | HTTP listen port |
| `DB_PATH` | `scores.db` | SQLite database file path |
| `HMAC_SECRET` | *(required)* | Shared secret for request signing |
| `TITLE` | `Game Scoreboard` | Page title and heading |
| `CACHE_TTL` | `60s` | Top-N cache duration (e.g. `30s`, `2m`) |
| `SCORE_WEIGHT_FLOOR` | `100` | Multiplier for floor reached |
| `SCORE_WEIGHT_DAMAGE_DEALT` | `2` | Multiplier for damage dealt |
| `SCORE_WEIGHT_DAMAGE_TAKEN` | `1` | Multiplier for damage taken (subtracted) |
| `SCORE_WEIGHT_REVIVE` | `50` | Multiplier for revives |
| `SCORE_WEIGHT_QUEST` | `200` | Multiplier for quests completed |

### Score Formula

```
score = (floor × W_FLOOR)
      + (damage_dealt × W_DMG_DEALT)
      - (damage_taken × W_DMG_TAKEN)
      + (revives × W_REVIVE)
      + (quests × W_QUEST)

if score < 0 → score = 0
```

## API

### `GET /` — Scoreboard UI

Returns the single-page leaderboard. Logo and favicon are served from `/assets/` if present.

### `POST /api/scores` — Submit Score (HMAC-protected)

**Headers:**
- `X-Signature` — HMAC-SHA256 hex digest
- `X-Timestamp` — Unix timestamp
- `X-Nonce` — Random hex string (16+ bytes), must be unique per request

**Body:**
```json
{
  "name": "PlayerName",
  "floor": 15,
  "damage_dealt": 3200,
  "damage_taken": 850,
  "revives": 2,
  "quests": 7
}
```

**Response** `201`:
```json
{
  "rank": 42,
  "score": 15850
}
```

### `GET /api/scores/top?n=10` — Top Scores

Query `n`: `10`, `50`, or `100` (capped at 100, default 10).

**Response:**
```json
[
  {
    "id": 1,
    "name": "PlayerName",
    "score": 15850,
    "floor": 15,
    "damage_dealt": 3200,
    "damage_taken": 850,
    "revives": 2,
    "quests": 7,
    "created_at": "2026-07-17 12:00:00"
  }
]
```

### `GET /api/scores/rank?name=PlayerName` — Player Rank

**Response:**
```json
{
  "name": "PlayerName",
  "rank": 42,
  "score": 15850
}
```

## HMAC Signing Guide

The client must sign each score submission. Payload format (pipe-separated, in order):

```
{timestamp}|{nonce}|{name}|{floor}|{damage_dealt}|{damage_taken}|{revives}|{quests}
```

**Pseudocode:**
```
payload = timestamp + "|" + nonce + "|" + name + "|" + floor + "|" + dmg_dealt + "|" + dmg_taken + "|" + revives + "|" + quests
signature = hex(hmac_sha256(secret, payload))
```

Send `X-Signature`, `X-Timestamp`, and `X-Nonce` as HTTP headers alongside the JSON body.

**Constraints:**
- Timestamp must be within ±60 seconds of server time
- Nonce must be unique; reused nonces are rejected (tracked for 5 minutes)
- Nonce should be at least 16 random bytes, hex-encoded

### Godot (GDScript) Example

```gdscript
# Requires an HMAC helper — use Crypto class or a native extension.
# Pseudo-code:
# var crypto = Crypto.new()
# var key = "your-secret".to_utf8_buffer()
# var payload = "%d|%s|%s|%d|%d|%d|%d|%d" % [ts, nonce, name, floor, dmg_d, dmg_t, rev, quests]
# var hmac = crypto.hmac_digest(HashingContext.HASH_SHA256, key, payload.to_utf8_buffer())
# var signature = hmac.hex_encode()
```

## Installation (Linux)

```bash
sudo ./install.sh
```

This will:
1. Download the latest Linux binary from GitHub Releases
2. Install to `/usr/local/bin/scoreboard`
3. Create config at `/etc/scoreboard/.env`
4. Install and start the hardened systemd service

After install, edit `/etc/scoreboard/.env` and set a strong `HMAC_SECRET`.

## Build from Source

```bash
# Local dev build
make build

# Linux release (amd64 + arm64)
make linux-release
```

**Requirements:** Go 1.24+, CGO enabled (for SQLite), GCC.

## Assets (Optional)

Place optional files in the `assets/` directory before building:

- `assets/logo.png` — displayed in the header of the scoreboard page
- `assets/favicon.ico` — browser tab icon

Files are served from the working directory at runtime.

## License

MIT
