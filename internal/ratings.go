package internal

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"
)

// ratingSubmitBody holds the parsed JSON body for a ratings submission.
type ratingSubmitBody struct {
	Ratings  *ratingValues   `json:"ratings"`
	Comment  string          `json:"comment"`
	Contact  *ratingContact  `json:"contact"`
	Meta     *ratingMeta     `json:"meta"`
	ClientTS string          `json:"client_ts"`
}

type ratingValues struct {
	Fun         int `json:"fun"`
	Balance     int `json:"balance"`
	Visuals     int `json:"visuals"`
	Clarity     int `json:"clarity"`
	Performance int `json:"performance"`
	Audio       int `json:"audio"`
	Difficulty  int `json:"difficulty"`
}

type ratingContact struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type ratingMeta struct {
	GameMode         string   `json:"game_mode"`
	Version          string   `json:"version"`
	Locale           string   `json:"locale"`
	MatchDurationSec int      `json:"match_duration_sec"`
	UnitsDeployed    int      `json:"units_deployed"`
	AIDifficulty     string   `json:"ai_difficulty"`
	Seed             string   `json:"seed"`
	TurnsPlayed      int      `json:"turns_played"`
	Result           string   `json:"result"`
	SpecialsUsed     []string `json:"specials_used"`
}

// validateRatings checks all required and optional fields.
// Returns an error message string, or empty if valid.
func (b *ratingSubmitBody) validate() string {
	if b.Ratings == nil {
		return "ratings is required"
	}

	r := b.Ratings
	for _, pair := range []struct {
		name  string
		value int
	}{
		{"fun", r.Fun},
		{"balance", r.Balance},
		{"visuals", r.Visuals},
		{"clarity", r.Clarity},
		{"performance", r.Performance},
		{"audio", r.Audio},
		{"difficulty", r.Difficulty},
	} {
		if pair.value < 1 || pair.value > 5 {
			return fmt.Sprintf("ratings.%s must be between 1 and 5", pair.name)
		}
	}

	if len(b.Comment) > 2000 {
		return "comment must be at most 2000 characters"
	}

	if b.Contact != nil {
		if len(b.Contact.Name) > 40 {
			return "contact.name must be at most 40 characters"
		}
		if len(b.Contact.Email) > 80 {
			return "contact.email must be at most 80 characters"
		}
	}

	return ""
}

// VerifyRatingsHMAC checks the request signature using ratings fields.
// Payload: timestamp|nonce|fun|balance|visuals|clarity|performance|audio|difficulty
func VerifyRatingsHMAC(secret string, timestamp int64, nonce string, r ratingValues, signature string, tracker *NonceTracker, tolerance time.Duration) error {
	// 1. Timestamp check
	now := time.Now().Unix()
	diff := now - timestamp
	if diff < 0 {
		diff = -diff
	}
	if float64(diff) > math.Abs(tolerance.Seconds()) {
		return fmt.Errorf("timestamp out of range: diff=%ds", diff)
	}

	// 2. Nonce check
	if nonce == "" {
		return fmt.Errorf("missing nonce")
	}
	if tracker.MarkSeen(nonce) {
		return fmt.Errorf("nonce already used")
	}

	// 3. Build payload and verify HMAC
	payload := fmt.Sprintf("%d|%s|%d|%d|%d|%d|%d|%d|%d",
		timestamp, nonce,
		r.Fun, r.Balance, r.Visuals, r.Clarity, r.Performance, r.Audio, r.Difficulty,
	)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))

	log.Printf("RATINGS_HMAC_DEBUG secret_len=%d payload=%s expected=%s got=%s", len(secret), payload, expected, signature)
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return fmt.Errorf("invalid signature")
	}

	return nil
}

// RatingsHMACMiddleware returns a middleware that validates HMAC signatures for ratings submission.
func RatingsHMACMiddleware(secret string, tracker *NonceTracker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sig := r.Header.Get("X-Signature")
			tsStr := r.Header.Get("X-Timestamp")
			nonce := r.Header.Get("X-Nonce")

			ts, err := strconv.ParseInt(tsStr, 10, 64)
			if err != nil {
				writeStatusError(w, http.StatusUnauthorized, "invalid timestamp")
				return
			}

			var body ratingSubmitBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeStatusError(w, http.StatusBadRequest, "invalid json body")
				return
			}

			if body.Ratings == nil {
				writeStatusError(w, http.StatusBadRequest, "ratings is required")
				return
			}

			if err := VerifyRatingsHMAC(secret, ts, nonce, *body.Ratings, sig, tracker, 60*time.Second); err != nil {
				writeStatusError(w, http.StatusUnauthorized, err.Error())
				return
			}

			ctx := r.Context()
			ctx = contextWithRatingBody(ctx, &body)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// SubmitRating handles rating/feedback submissions (HMAC-protected).
func (h *Handler) SubmitRating(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeStatusError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	body := ratingBodyFromContext(r.Context())
	if body == nil {
		writeStatusError(w, http.StatusInternalServerError, "missing parsed body")
		return
	}

	if msg := body.validate(); msg != "" {
		writeStatusError(w, http.StatusBadRequest, msg)
		return
	}

	entry := RatingEntry{
		Fun:          body.Ratings.Fun,
		Balance:      body.Ratings.Balance,
		Visuals:      body.Ratings.Visuals,
		Clarity:      body.Ratings.Clarity,
		Performance:  body.Ratings.Performance,
		Audio:        body.Ratings.Audio,
		Difficulty:   body.Ratings.Difficulty,
		Comment:      body.Comment,
	}

	if body.Contact != nil {
		entry.ContactName = body.Contact.Name
		entry.ContactEmail = body.Contact.Email
	}

	if body.Meta != nil {
		entry.GameMode = body.Meta.GameMode
		entry.Version = body.Meta.Version
		entry.Locale = body.Meta.Locale
		entry.MatchDurationSec = body.Meta.MatchDurationSec
		entry.UnitsDeployed = body.Meta.UnitsDeployed
		entry.AIDifficulty = body.Meta.AIDifficulty
		entry.Seed = body.Meta.Seed
		entry.TurnsPlayed = body.Meta.TurnsPlayed
		entry.Result = body.Meta.Result
		if len(body.Meta.SpecialsUsed) > 0 {
			b, _ := json.Marshal(body.Meta.SpecialsUsed)
			entry.SpecialsUsed = string(b)
		}
	}

	entry.ClientTS = body.ClientTS

	if _, err := InsertRating(h.DB, entry); err != nil {
		writeStatusError(w, http.StatusInternalServerError, "internal")
		return
	}

	writeStatusOK(w)
}

// context key for rating body
type ratingContextKey string

const ratingBodyKey ratingContextKey = "parsed_rating_body"

func contextWithRatingBody(ctx context.Context, b *ratingSubmitBody) context.Context {
	return context.WithValue(ctx, ratingBodyKey, b)
}

func ratingBodyFromContext(ctx context.Context) *ratingSubmitBody {
	b, _ := ctx.Value(ratingBodyKey).(*ratingSubmitBody)
	return b
}

// writeStatusError writes a JSON error in the format {"status":"error","message":"..."}
func writeStatusError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "error",
		"message": message,
	})
}

// writeStatusOK writes {"status":"ok"}
func writeStatusOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
