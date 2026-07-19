package internal

import (
	"database/sql"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

// PageData holds variables for the index HTML template.
type PageData struct {
	Title     string
	HasLogo   bool
	HasFavicon bool
}

// Handler bundles all HTTP dependencies.
type Handler struct {
	DB      *sql.DB
	Cache   *Cache
	Weights Weights
	Title   string
	HasLogo bool
	HasFavicon bool
	IndexTpl *template.Template
}

// Index serves the scoreboard HTML page.
func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		WriteError(w, http.StatusNotFound, "not found")
		return
	}

	data := PageData{
		Title:      h.Title,
		HasLogo:    h.HasLogo,
		HasFavicon: h.HasFavicon,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if err := h.IndexTpl.Execute(w, data); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// SubmitScore handles score submissions (HMAC-protected).
func (h *Handler) SubmitScore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		WriteError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}

	body := bodyFromContext(r.Context())
	if body == nil {
		WriteError(w, http.StatusInternalServerError, "missing parsed body")
		return
	}

	score := CalculateScore(body.Metrics, h.Weights)

	_, err := InsertScore(h.DB, body.Name, body.Metrics, score)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to insert score")
		return
	}

	// Invalidate all cached top-N results
	h.Cache.Invalidate()

	// Get the player's rank using their best score (which includes the just-inserted one)
	rank, _, err := GetRank(h.DB, body.Name)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "failed to get rank")
		return
	}

	WriteJSON(w, http.StatusCreated, map[string]interface{}{
		"rank":  rank,
		"score": score,
	})
}

// TopScores returns the top-N scores.
func (h *Handler) TopScores(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	n := 10
	if nStr := r.URL.Query().Get("n"); nStr != "" {
		if parsed, err := strconv.Atoi(nStr); err == nil {
			n = parsed
		}
	}

	// Validate allowed N values, cap at 100
	validN := map[int]bool{10: true, 50: true, 100: true}
	if n > 100 {
		n = 100
	}
	if _, ok := validN[n]; !ok {
		n = 10
	}

	// Check cache
	if entries, ok := h.Cache.Get(n); ok {
		WriteJSON(w, http.StatusOK, entries)
		return
	}

	// Query DB
	entries, err := GetTopN(h.DB, n)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "query failed")
		return
	}

	// Store in cache
	h.Cache.Set(n, entries)

	WriteJSON(w, http.StatusOK, entries)
}

// Rank returns the rank and best score for a given player name.
func (h *Handler) Rank(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "GET required")
		return
	}

	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		WriteError(w, http.StatusBadRequest, "name query parameter is required")
		return
	}

	rank, bestScore, err := GetRank(h.DB, name)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "query failed")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"name":  name,
		"rank":  rank,
		"score": bestScore,
	})
}

// HandleCORS handles preflight OPTIONS requests with CORS headers.
func HandleCORS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Signature, X-Timestamp, X-Nonce")
	w.Header().Set("Access-Control-Max-Age", "86400")
	w.WriteHeader(http.StatusNoContent)
}
