package internal

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// HMACMiddleware returns a middleware that validates HMAC signatures for score submission.
func HMACMiddleware(secret string, tracker *NonceTracker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sig := r.Header.Get("X-Signature")
			tsStr := r.Header.Get("X-Timestamp")
			nonce := r.Header.Get("X-Nonce")

			ts, err := strconv.ParseInt(tsStr, 10, 64)
			if err != nil {
				WriteError(w, http.StatusUnauthorized, "invalid timestamp")
				return
			}

			// Parse body to build payload for verification
			var body submitBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				WriteError(w, http.StatusBadRequest, "invalid json body")
				return
			}

			if body.Name == "" {
				WriteError(w, http.StatusBadRequest, "name is required")
				return
			}

			if err := VerifyHMAC(secret, ts, nonce, body.Name, body.Metrics, sig, tracker, 60*time.Second); err != nil {
				WriteError(w, http.StatusUnauthorized, err.Error())
				return
			}

			// Store parsed data in context for the handler
			ctx := r.Context()
			ctx = contextWithBody(ctx, &body)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
