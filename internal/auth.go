package internal

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"sync"
	"time"
)

// NonceTracker keeps track of recently seen nonces to prevent replay attacks.
// Cleanup runs periodically to evict expired entries.
type NonceTracker struct {
	mu     sync.Mutex
	nonces map[string]time.Time
}

// NewNonceTracker creates a tracker and starts a background goroutine
// that purges nonces older than ttl.
func NewNonceTracker(ttl time.Duration) *NonceTracker {
	nt := &NonceTracker{
		nonces: make(map[string]time.Time),
	}
	go func() {
		for {
			time.Sleep(ttl)
			nt.cleanup(ttl)
		}
	}()
	return nt
}

// MarkSeen records a nonce and returns true if it was already seen.
func (nt *NonceTracker) MarkSeen(nonce string) bool {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	if _, exists := nt.nonces[nonce]; exists {
		return true
	}
	nt.nonces[nonce] = time.Now()
	return false
}

func (nt *NonceTracker) cleanup(ttl time.Duration) {
	nt.mu.Lock()
	defer nt.mu.Unlock()

	cutoff := time.Now().Add(-ttl)
	for k, v := range nt.nonces {
		if v.Before(cutoff) {
			delete(nt.nonces, k)
		}
	}
}

// VerifyHMAC checks the request signature using the shared secret.
// It validates:
//  1. Timestamp is within ±tolerance of now.
//  2. Nonce has not been seen before (replay protection).
//  3. HMAC-SHA256(secret, payload) matches the signature.
//
// Payload is constructed as: timestamp|nonce|name|floor|dmgDealt|dmgTaken|revives|quests
func VerifyHMAC(secret string, timestamp int64, nonce, name string, m Metrics, signature string, tracker *NonceTracker, tolerance time.Duration) error {
	// 1. Timestamp check
	now := time.Now().Unix()
	diff := now - timestamp
	if diff < 0 {
		diff = -diff
	}
	if float64(diff) > math.Abs(tolerance.Seconds()) {
		return fmt.Errorf("timestamp out of range: diff=%ds", diff)
	}

	// 2. Nonce check (must be non-empty)
	if nonce == "" {
		return fmt.Errorf("missing nonce")
	}
	if tracker.MarkSeen(nonce) {
		return fmt.Errorf("nonce already used")
	}

	// 3. Build payload and verify HMAC
	payload := fmt.Sprintf("%d|%s|%s|%d|%d|%d|%d|%d",
		timestamp, nonce, name,
		m.Floor, m.DamageDealt, m.DamageTaken, m.Revives, m.Quests,
	)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))

	log.Printf("HMAC_DEBUG secret_len=%d payload=%s expected=%s got=%s", len(secret), payload, expected, signature)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}
