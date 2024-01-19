package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

// DetectedSpam is a storage for detected spam entries
type DetectedSpam struct {
	db *sqlx.DB
}

// DetectedSpamInfo represents information about a detected spam entry.
type DetectedSpamInfo struct {
	Text       string               `db:"text"`
	UserID     int64                `db:"user_id"`
	UserName   string               `db:"user_name"`
	Timestamp  time.Time            `db:"timestamp"`
	ChecksJSON string               `db:"checks"` // Store as JSON
	Checks     []spamcheck.Response `db:"-"`      // Don't store in DB
}

// NewDetectedSpam creates a new DetectedSpam storage
func NewDetectedSpam(db *sqlx.DB) (*DetectedSpam, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS detected_spam (
    	id INTEGER PRIMARY KEY AUTOINCREMENT,
        text TEXT,
        user_id INTEGER,
        user_name TEXT,
        timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
        checks TEXT
    )`)
	if err != nil {
		return nil, fmt.Errorf("failed to create detected_spam table: %w", err)
	}
	return &DetectedSpam{db: db}, nil
}

// Write adds a new detected spam entry
func (ds *DetectedSpam) Write(entry DetectedSpamInfo, checks []spamcheck.Response) error {
	checksJSON, err := json.Marshal(checks)
	if err != nil {
		return fmt.Errorf("failed to marshal checks: %w", err)
	}

	query := `INSERT INTO detected_spam (text, user_id, user_name, timestamp, checks) VALUES (?, ?, ?, ?, ?)`
	if _, err := ds.db.Exec(query, entry.Text, entry.UserID, entry.UserName, entry.Timestamp, checksJSON); err != nil {
		return fmt.Errorf("failed to insert detected spam entry: %w", err)
	}

	log.Printf("[INFO] detected spam entry added for user_id:%d, name:%s", entry.UserID, entry.UserName)
	return nil
}

// Read returns all detected spam entries
func (ds *DetectedSpam) Read() ([]DetectedSpamInfo, error) {
	var entries []DetectedSpamInfo
	err := ds.db.Select(&entries, "SELECT text, user_id, user_name, timestamp, checks FROM detected_spam ORDER BY timestamp DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to get detected spam entries: %w", err)
	}

	for i, entry := range entries {
		var checks []spamcheck.Response
		if err := json.Unmarshal([]byte(entry.ChecksJSON), &checks); err != nil {
			return nil, fmt.Errorf("failed to unmarshal checks for entry %d: %w", i, err)
		}
		entries[i].Checks = checks
		entries[i].Timestamp = entry.Timestamp.Local()
	}
	return entries, nil
}
