package internal

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// --- Rate Limiter ---

type rateLimitEntry struct {
	count     int
	firstSeen time.Time
	banned    bool
	banUntil  time.Time
}

// RateLimiter tracks login attempts per IP with a sliding window.
type RateLimiter struct {
	mu       sync.Mutex
	entries  map[string]*rateLimitEntry
	stopCh   chan struct{}
}

// NewRateLimiter creates a rate limiter and starts a background cleanup goroutine.
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		entries: make(map[string]*rateLimitEntry),
		stopCh:  make(chan struct{}),
	}
	go rl.cleanup()
	return rl
}

// Allow checks if the IP is allowed to attempt login. Returns false if rate-limited or banned.
func (rl *RateLimiter) Allow(ip string) (bool, string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	e, exists := rl.entries[ip]
	now := time.Now()

	if !exists {
		rl.entries[ip] = &rateLimitEntry{count: 1, firstSeen: now}
		return true, ""
	}

	// Check if banned
	if e.banned {
		if now.Before(e.banUntil) {
			remaining := e.banUntil.Sub(now).Round(time.Second)
			return false, fmt.Sprintf("too many attempts, try again in %s", remaining)
		}
		// Ban expired — reset
		e.banned = false
		e.count = 1
		e.firstSeen = now
		return true, ""
	}

	// Reset window if more than 1 minute since first attempt in window
	if now.Sub(e.firstSeen) > time.Minute {
		e.count = 1
		e.firstSeen = now
		return true, ""
	}

	// Within 1-minute window
	e.count++

	// Max 5 attempts per minute
	if e.count > 5 {
		// Check if we should ban (10+ cumulative failures)
		if e.count >= 10 {
			e.banned = true
			e.banUntil = now.Add(15 * time.Minute)
			return false, "too many attempts, banned for 15 minutes"
		}
		return false, "too many attempts, wait before trying again"
	}

	return true, ""
}

// RecordFailure increments the failure count (called alongside Allow, but Allow already counts).
// Kept for explicit failure recording if needed.
func (rl *RateLimiter) RecordFailure(ip string) {
	// Allow() already increments count; this is a no-op for the current design.
}

func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, e := range rl.entries {
				// Remove entries with expired bans older than 15 min
				if e.banned && now.After(e.banUntil) {
					delete(rl.entries, ip)
				}
				// Remove stale unbanned entries older than 5 min
				if !e.banned && now.Sub(e.firstSeen) > 15*time.Minute {
					delete(rl.entries, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopCh:
			return
		}
	}
}

// --- Password Verification ---

// verifyPassword compares a plaintext password against a bcrypt hash (or plaintext fallback).
func verifyPassword(password, stored string) bool {
	// Try bcrypt first
	if err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password)); err == nil {
		return true
	}
	// Plaintext fallback
	return password == stored
}

// HashPassword generates a bcrypt hash from a plaintext password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// --- Session Management ---

// createSessionToken generates a signed session token: base64(timestamp|HMAC-SHA256(secret, timestamp|ip))
func createSessionToken(secret, ip string) string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	payload := fmt.Sprintf("%s|%s", ts, ip)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))

	token := fmt.Sprintf("%s|%s", ts, sig)
	return base64.StdEncoding.EncodeToString([]byte(token))
}

// validateSessionToken checks a session token and returns true if valid.
func validateSessionToken(token, secret, ip string) bool {
	data, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return false
	}

	parts := strings.SplitN(string(data), "|", 2)
	if len(parts) != 2 {
		return false
	}

	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}

	// Check TTL (1 hour)
	if time.Now().Unix()-ts > 3600 {
		return false
	}

	// Verify HMAC
	payload := fmt.Sprintf("%d|%s", ts, ip)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(parts[1]))
}

// --- Admin Handler ---

// AdminHandler serves the admin panel.
type AdminHandler struct {
	DB            *sql.DB
	HMACSecret    string
	AdminPassword string
	RateLimiter   *RateLimiter
	LoginTpl      *template.Template
	PanelTpl      *template.Template
}

// NewAdminHandler creates an admin handler.
func NewAdminHandler(db *sql.DB, hmacSecret, adminPassword string, loginTpl, panelTpl *template.Template) *AdminHandler {
	return &AdminHandler{
		DB:            db,
		HMACSecret:    hmacSecret,
		AdminPassword: adminPassword,
		RateLimiter:   NewRateLimiter(),
		LoginTpl:      loginTpl,
		PanelTpl:      panelTpl,
	}
}

// ServeHTTP routes admin requests.
func (a *AdminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	switch {
	case path == "/panel" && r.Method == http.MethodGet:
		a.handlePanel(w, r)
	case path == "/panel/login" && r.Method == http.MethodPost:
		a.handleLogin(w, r)
	case path == "/panel/logout" && r.Method == http.MethodGet:
		a.handleLogout(w, r)
	case path == "/panel/data" && r.Method == http.MethodGet:
		a.handleData(w, r)
	default:
		http.NotFound(w, r)
	}
}

// handlePanel serves the login page or the admin panel based on session.
func (a *AdminHandler) handlePanel(w http.ResponseWriter, r *http.Request) {
	if a.checkSession(w, r) {
		// Valid session — serve admin panel
		a.PanelTpl.Execute(w, nil)
		return
	}
	// No valid session — serve login page
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	a.LoginTpl.Execute(w, nil)
}

// handleLogin processes the login form.
func (a *AdminHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)

	allowed, msg := a.RateLimiter.Allow(ip)
	if !allowed {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusTooManyRequests)
		a.LoginTpl.Execute(w, map[string]string{"Error": msg})
		return
	}

	if err := r.ParseForm(); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.LoginTpl.Execute(w, map[string]string{"Error": "invalid form"})
		return
	}

	password := r.FormValue("password")

	if !verifyPassword(password, a.AdminPassword) {
		// Explicitly record failure (count already incremented in Allow)
		a.RateLimiter.RecordFailure(ip)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		a.LoginTpl.Execute(w, map[string]string{"Error": "wrong password"})
		return
	}

	// Success — create session
	token := createSessionToken(a.HMACSecret, ip)
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    token,
		Path:     "/panel",
		MaxAge:   3600,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	http.Redirect(w, r, "/panel", http.StatusSeeOther)
}

// handleLogout clears the session and redirects to login.
func (a *AdminHandler) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "admin_session",
		Value:    "",
		Path:     "/panel",
		MaxAge:   -1,
		HttpOnly: true,
	})
	http.Redirect(w, r, "/panel", http.StatusSeeOther)
}

// handleData returns all ratings as JSON (session-protected).
func (a *AdminHandler) handleData(w http.ResponseWriter, r *http.Request) {
	if !a.checkSession(w, r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	ratings, err := GetAllRatings(a.DB)
	if err != nil {
		log.Printf("Error querying ratings: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ratings)
}

// checkSession validates the admin_session cookie. Returns true if valid.
func (a *AdminHandler) checkSession(w http.ResponseWriter, r *http.Request) bool {
	cookie, err := r.Cookie("admin_session")
	if err != nil {
		return false
	}
	if !validateSessionToken(cookie.Value, a.HMACSecret, clientIP(r)) {
		return false
	}
	return true
}

// clientIP extracts the client IP from the request.
func clientIP(r *http.Request) string {
	// Check X-Forwarded-For first (for reverse proxy setups)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the chain
		parts := strings.Split(xff, ",")
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}
	// Fall back to RemoteAddr
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}
