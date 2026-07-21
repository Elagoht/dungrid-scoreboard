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
