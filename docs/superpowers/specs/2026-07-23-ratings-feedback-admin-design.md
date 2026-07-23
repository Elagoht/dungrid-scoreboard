# Ratings Feedback Endpoint & Admin Panel — Design

**Date:** 2026-07-23
**Status:** approved

## Overview

Add a HMAC-protected ratings/feedback submission endpoint and a hidden, password-protected admin interface with brute-force hardening and rate limiting to view submissions.

## 1. Database — `ratings` table

New table alongside existing `scores` in SQLite:

```sql
CREATE TABLE IF NOT EXISTS ratings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    fun INTEGER NOT NULL CHECK(fun >= 1 AND fun <= 5),
    balance INTEGER NOT NULL CHECK(balance >= 1 AND balance <= 5),
    visuals INTEGER NOT NULL CHECK(visuals >= 1 AND visuals <= 5),
    clarity INTEGER NOT NULL CHECK(clarity >= 1 AND clarity <= 5),
    performance INTEGER NOT NULL CHECK(performance >= 1 AND performance <= 5),
    audio INTEGER NOT NULL CHECK(audio >= 1 AND audio <= 5),
    difficulty INTEGER NOT NULL CHECK(difficulty >= 1 AND difficulty <= 5),
    comment TEXT DEFAULT '',
    contact_name TEXT DEFAULT '',
    contact_email TEXT DEFAULT '',
    game_mode TEXT DEFAULT '',
    version TEXT DEFAULT '',
    locale TEXT DEFAULT '',
    match_duration_sec INTEGER DEFAULT 0,
    units_deployed INTEGER DEFAULT 0,
    ai_difficulty TEXT DEFAULT '',
    seed TEXT DEFAULT '',
    turns_played INTEGER DEFAULT 0,
    result TEXT DEFAULT '',
    specials_used TEXT DEFAULT '',
    client_ts TEXT DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
```

`specials_used` is stored as a JSON-encoded string array. All meta fields are optional (default empty/zero). All ratings fields have CHECK constraints for 1–5 range.

## 2. Feedback Endpoint — `POST /api/ratings`

Same HMAC pattern as `POST /api/scores`:
- Headers: `X-Signature`, `X-Timestamp`, `X-Nonce`
- HMAC payload: `timestamp|nonce|fun|balance|visuals|clarity|performance|audio|difficulty`
- Only the 7 rating values are signed; comment, contact, and meta are optional

### Request Body
```json
{
    "ratings": {
        "fun": 4, "balance": 3, "visuals": 5, "clarity": 4,
        "performance": 5, "audio": 3, "difficulty": 4
    },
    "comment": "...",
    "contact": {"name": "...", "email": "..."},
    "meta": {
        "game_mode": "tower", "version": "0.9.0", "locale": "tr",
        "match_duration_sec": 487, "units_deployed": 5,
        "ai_difficulty": "hard", "seed": "123456",
        "turns_played": 12, "result": "win",
        "specials_used": ["octopus_ink", "shield_wall"]
    },
    "client_ts": "2026-07-23T14:32:00Z"
}
```

### Validation
- All 7 ratings: required integers, 1–5
- `comment`: optional string, max 2000 chars
- `contact.name`: optional string, max 40 chars
- `contact.email`: optional string, max 80 chars
- All meta fields: type-checked, optional
- `specials_used`: must be JSON string array or null

### Responses
- `200 {"status":"ok"}` — success
- `400 {"status":"error","message":"..."}` — validation error
- `401 {"status":"error","message":"invalid signature"}` — HMAC/timestamp/nonce error
- `500 {"status":"error","message":"internal"}` — server error

Note: existing `WriteError` uses `{"error":"..."}` format. Ratings handler will use its own response format with `{"status":"error","message":"..."}` to match the spec. A new `writeStatusError` helper handles this.

### HMAC Middleware
A separate `RatingsHMACMiddleware` function (different payload structure from scores):
1. Reads X-Signature, X-Timestamp, X-Nonce headers
2. Parses body as `ratingSubmitBody` struct
3. Validates HMAC against ratings fields
4. Stores parsed body in context
5. Calls next handler

### Files
- `internal/ratings.go` — types, validation, DB insert, HMAC middleware, handler

## 3. Admin Panel — `/panel`

Hidden from crawlers: no follow meta tag on page. Memorable path.

### 3a. Authentication

- `ADMIN_PASSWORD` env var: bcrypt hash preferred, plaintext fallback
- Helper function compares password against bcrypt hash or plaintext
- `make hash-password` target to generate bcrypt hash

### 3b. Login Flow

- GET `/panel` → serves login form if no valid session
- POST `/panel/login` → validates password, sets session cookie on success, redirects to `/panel`
- GET `/panel` with valid session → serves admin panel with feedback table

### 3c. Session Cookie

- Name: `admin_session`
- Value: `base64(timestamp|HMAC-SHA256(secret, timestamp|client_ip))`
- TTL: 1 hour
- HttpOnly, SameSite=Strict, Path=/

### 3d. Brute-force Protection

In-memory rate limiter using `sync.Map`:
- Per IP: max 5 login attempts per minute (sliding window)
- After 10 cumulative failures: 15-minute IP ban
- Cleanup goroutine evicts expired entries every 5 minutes

### 3e. Admin Panel Page

- Served when session cookie is valid
- Displays all ratings in a scrollable HTML table, newest first
- Columns: all rating fields, comment, contact, meta fields, client_ts
- Basic responsive design, dark theme to match existing scoreboard
- Logout button (POST `/panel/logout`, clears session)
- Data fetched via JS from `/panel/data` (session-protected JSON endpoint)

### 3f. Data Endpoint

- GET `/panel/data` — returns all ratings as JSON, session-protected
- GET `/panel/logout` — clears session cookie, redirects to login

### 3g. Static Files

- `static/admin_login.html` — embedded, password form
- `static/admin_panel.html` — embedded, admin UI
- Both use the same `embed.FS`

### Files

- `internal/admin.go` — password check, session, rate limiter, handlers

## 4. Route Changes in `main.go`

```go
// New HMAC-protected endpoint
mux.HandleFunc("/api/ratings", func(w http.ResponseWriter, r *http.Request) {
    if r.Method == http.MethodOptions {
        internal.HandleCORS(w, r)
        return
    }
    hmacMw := internal.RatingsHMACMiddleware(hmacSecret, nonceTracker)
    hmacMw(http.HandlerFunc(h.SubmitRating)).ServeHTTP(w, r)
})

// Hidden admin panel
adminHandler := internal.NewAdminHandler(adminPassword, hmacSecret)
mux.HandleFunc("/panel", adminHandler.ServeHTTP)
mux.HandleFunc("/panel/", adminHandler.ServeHTTP)
```

New env var: `ADMIN_PASSWORD`

## 5. Implementation Order

1. Update `internal/db.go` — add ratings table to migration, add insert/query functions
2. Create `internal/ratings.go` — types, validation, HMAC middleware, handler
3. Create `internal/admin.go` — password check, session, rate limiter, admin handler
4. Create `static/admin_login.html` and `static/admin_panel.html`
5. Update `main.go` — routes, embed, wiring
6. Update `.env.example` — new vars
7. Manual testing
