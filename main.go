package main

import (
	"context"
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/furkanbaytekin/generic-game-scoreboard-backend/internal"
)

//go:embed static/*
var staticFiles embed.FS

func main() {
	// --- Load .env file (if present) ---
	if _, err := os.Stat(".env"); err == nil {
		if err := internal.LoadEnvFile(".env"); err != nil {
			log.Printf("Warning: failed to load .env: %v", err)
		}
	}

	// --- Config from env ---
	port := internal.Getenv("PORT", "8080")
	dbPath := internal.Getenv("DB_PATH", "scores.db")
	hmacSecret := internal.Getenv("HMAC_SECRET", "")
	adminPassword := internal.Getenv("ADMIN_PASSWORD", "")
	title := internal.Getenv("TITLE", "Game Scoreboard")
	cacheTTL := parseDuration(internal.Getenv("CACHE_TTL", "60s"), 60*time.Second)

	if hmacSecret == "" {
		log.Println("WARNING: HMAC_SECRET is empty — score submission is unprotected")
	}
	if adminPassword == "" {
		log.Println("WARNING: ADMIN_PASSWORD is not set — admin panel is disabled")
	}

	// --- Database ---
	db, err := internal.OpenDB(dbPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()
	log.Printf("Database opened: %s", dbPath)

	// --- Cache ---
	cache := internal.NewCache(cacheTTL)

	// --- HMAC ---
	nonceTracker := internal.NewNonceTracker(5 * time.Minute)

	// --- Weights ---
	weights := internal.LoadWeights()

	// --- Assets ---
	hasLogo := fileExists("assets/logo.png")
	hasFavicon := fileExists("assets/favicon.ico")

	// --- Templates ---
	indexHTML, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		log.Fatalf("Failed to read embedded index.html: %v", err)
	}
	indexTpl := template.Must(template.New("index").Parse(string(indexHTML)))

	adminLoginHTML, err := staticFiles.ReadFile("static/admin_login.html")
	if err != nil {
		log.Fatalf("Failed to read embedded admin_login.html: %v", err)
	}
	adminLoginTpl := template.Must(template.New("admin_login").Parse(string(adminLoginHTML)))

	adminPanelHTML, err := staticFiles.ReadFile("static/admin_panel.html")
	if err != nil {
		log.Fatalf("Failed to read embedded admin_panel.html: %v", err)
	}
	adminPanelTpl := template.Must(template.New("admin_panel").Parse(string(adminPanelHTML)))

	// --- Handlers ---
	h := &internal.Handler{
		DB:         db,
		Cache:      cache,
		Weights:    weights,
		Title:      title,
		HasLogo:    hasLogo,
		HasFavicon: hasFavicon,
		IndexTpl:   indexTpl,
	}

	mux := http.NewServeMux()

	// Public endpoints
	mux.HandleFunc("/", h.Index)
	mux.HandleFunc("/api/scores/top", h.TopScores)
	mux.HandleFunc("/api/scores/rank", h.Rank)
	mux.HandleFunc("/api/scores", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			internal.HandleCORS(w, r)
			return
		}
		// Wrap submit with HMAC middleware
		hmacMw := internal.HMACMiddleware(hmacSecret, nonceTracker)
		hmacMw(http.HandlerFunc(h.SubmitScore)).ServeHTTP(w, r)
	})

	// Ratings/feedback endpoint (HMAC-protected)
	mux.HandleFunc("/api/ratings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			internal.HandleCORS(w, r)
			return
		}
		hmacMw := internal.RatingsHMACMiddleware(hmacSecret, nonceTracker)
		hmacMw(http.HandlerFunc(h.SubmitRating)).ServeHTTP(w, r)
	})

	// Hidden admin panel
	if adminPassword != "" {
		adminHandler := internal.NewAdminHandler(db, hmacSecret, adminPassword, adminLoginTpl, adminPanelTpl)
		mux.Handle("/panel", adminHandler)
		mux.Handle("/panel/", adminHandler)
	}

	// Serve assets from disk if present (logo, favicon)
	if hasLogo || hasFavicon {
		fs := http.FileServer(http.Dir("assets"))
		mux.Handle("/assets/", http.StripPrefix("/assets/", fs))
	}

	// --- Server ---
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
	}()

	log.Printf("Starting server on :%s (title: %s)", port, title)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
	log.Println("Server stopped")
}

func parseDuration(s string, defaultVal time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultVal
	}
	return d
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
