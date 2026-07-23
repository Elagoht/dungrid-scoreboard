package internal

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ScoreEntry represents a single score record from the database.
type ScoreEntry struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Score       int    `json:"score"`
	Floor       int    `json:"floor"`
	DamageDealt int    `json:"damage_dealt"`
	DamageTaken int    `json:"damage_taken"`
	Revives     int    `json:"revives"`
	Quests      int    `json:"quests"`
	CreatedAt   string `json:"created_at"`
}

// OpenDB opens (or creates) the SQLite database at the given path
// and runs schema migrations. WAL mode is enabled for better concurrency.
func OpenDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS scores (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			score INTEGER NOT NULL,
			floor INTEGER NOT NULL DEFAULT 0,
			damage_dealt INTEGER NOT NULL DEFAULT 0,
			damage_taken INTEGER NOT NULL DEFAULT 0,
			revives INTEGER NOT NULL DEFAULT 0,
			quests INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_scores_score ON scores(score DESC);
		CREATE INDEX IF NOT EXISTS idx_scores_name ON scores(name);

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
		CREATE INDEX IF NOT EXISTS idx_ratings_created ON ratings(created_at DESC);
	`)
	return err
}

// InsertScore inserts a new score record and returns its ID.
func InsertScore(db *sql.DB, name string, m Metrics, score int) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO scores (name, score, floor, damage_dealt, damage_taken, revives, quests)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		name, score, m.Floor, m.DamageDealt, m.DamageTaken, m.Revives, m.Quests,
	)
	if err != nil {
		return 0, fmt.Errorf("insert score: %w", err)
	}
	return res.LastInsertId()
}

// GetTopN returns up to N highest scores. Capped at 100.
func GetTopN(db *sql.DB, n int) ([]ScoreEntry, error) {
	if n > 100 {
		n = 100
	}
	if n < 1 {
		n = 10
	}

	rows, err := db.Query(
		`SELECT id, name, score, floor, damage_dealt, damage_taken, revives, quests, created_at
		 FROM scores ORDER BY score DESC LIMIT ?`, n,
	)
	if err != nil {
		return nil, fmt.Errorf("query top n: %w", err)
	}
	defer rows.Close()

	return scanScores(rows)
}

// GetRank returns the rank (1-based) of the best score for the given name.
// If the name has no scores, rank is 0.
func GetRank(db *sql.DB, name string) (rank int, bestScore int, err error) {
	row := db.QueryRow(`SELECT COALESCE(MAX(score), 0) FROM scores WHERE name = ?`, name)
	if err := row.Scan(&bestScore); err != nil {
		return 0, 0, fmt.Errorf("max score: %w", err)
	}

	if bestScore == 0 {
		return 0, 0, nil
	}

	// Count how many players have a strictly higher best score.
	// Note: this is "rank among distinct names" — each name appears once with their best.
	row = db.QueryRow(
		`SELECT COUNT(*) + 1 FROM scores WHERE score > ?`,
		bestScore,
	)
	if err := row.Scan(&rank); err != nil {
		return 0, 0, fmt.Errorf("rank query: %w", err)
	}

	return rank, bestScore, nil
}

// RatingEntry represents a single rating/feedback record.
type RatingEntry struct {
	ID               int    `json:"id"`
	Fun              int    `json:"fun"`
	Balance          int    `json:"balance"`
	Visuals          int    `json:"visuals"`
	Clarity          int    `json:"clarity"`
	Performance      int    `json:"performance"`
	Audio            int    `json:"audio"`
	Difficulty       int    `json:"difficulty"`
	Comment          string `json:"comment"`
	ContactName      string `json:"contact_name"`
	ContactEmail     string `json:"contact_email"`
	GameMode         string `json:"game_mode"`
	Version          string `json:"version"`
	Locale           string `json:"locale"`
	MatchDurationSec int    `json:"match_duration_sec"`
	UnitsDeployed    int    `json:"units_deployed"`
	AIDifficulty     string `json:"ai_difficulty"`
	Seed             string `json:"seed"`
	TurnsPlayed      int    `json:"turns_played"`
	Result           string `json:"result"`
	SpecialsUsed     string `json:"specials_used"`
	ClientTS         string `json:"client_ts"`
	CreatedAt        string `json:"created_at"`
}

// InsertRating inserts a new rating/feedback record.
func InsertRating(db *sql.DB, r RatingEntry) (int64, error) {
	res, err := db.Exec(
		`INSERT INTO ratings (fun, balance, visuals, clarity, performance, audio, difficulty,
			comment, contact_name, contact_email, game_mode, version, locale,
			match_duration_sec, units_deployed, ai_difficulty, seed, turns_played,
			result, specials_used, client_ts)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.Fun, r.Balance, r.Visuals, r.Clarity, r.Performance, r.Audio, r.Difficulty,
		r.Comment, r.ContactName, r.ContactEmail, r.GameMode, r.Version, r.Locale,
		r.MatchDurationSec, r.UnitsDeployed, r.AIDifficulty, r.Seed, r.TurnsPlayed,
		r.Result, r.SpecialsUsed, r.ClientTS,
	)
	if err != nil {
		return 0, fmt.Errorf("insert rating: %w", err)
	}
	return res.LastInsertId()
}

// GetAllRatings returns all ratings ordered by newest first.
func GetAllRatings(db *sql.DB) ([]RatingEntry, error) {
	rows, err := db.Query(
		`SELECT id, fun, balance, visuals, clarity, performance, audio, difficulty,
			comment, contact_name, contact_email, game_mode, version, locale,
			match_duration_sec, units_deployed, ai_difficulty, seed, turns_played,
			result, specials_used, client_ts, created_at
		 FROM ratings ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query ratings: %w", err)
	}
	defer rows.Close()

	var entries []RatingEntry
	for rows.Next() {
		var e RatingEntry
		if err := rows.Scan(
			&e.ID, &e.Fun, &e.Balance, &e.Visuals, &e.Clarity, &e.Performance,
			&e.Audio, &e.Difficulty, &e.Comment, &e.ContactName, &e.ContactEmail,
			&e.GameMode, &e.Version, &e.Locale, &e.MatchDurationSec, &e.UnitsDeployed,
			&e.AIDifficulty, &e.Seed, &e.TurnsPlayed, &e.Result, &e.SpecialsUsed,
			&e.ClientTS, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan rating: %w", err)
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []RatingEntry{}
	}
	return entries, rows.Err()
}

func scanScores(rows *sql.Rows) ([]ScoreEntry, error) {
	var entries []ScoreEntry
	for rows.Next() {
		var e ScoreEntry
		if err := rows.Scan(&e.ID, &e.Name, &e.Score, &e.Floor, &e.DamageDealt, &e.DamageTaken, &e.Revives, &e.Quests, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan score: %w", err)
		}
		entries = append(entries, e)
	}
	if entries == nil {
		entries = []ScoreEntry{}
	}
	return entries, rows.Err()
}
