package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

const maxDetectedSpamEntries = 500

// DetectedSpam is a storage for detected spam entries
type DetectedSpam struct {
	db *Engine
}

// DetectedSpamInfo represents information about a detected spam entry.
type DetectedSpamInfo struct {
	ID         int64                `db:"id"`
	GID        string               `db:"gid"`
	Text       string               `db:"text"`
	UserID     int64                `db:"user_id"`
	UserName   string               `db:"user_name"`
	Timestamp  time.Time            `db:"timestamp"`
	Added      bool                 `db:"added"`  // added to samples
	ChecksJSON string               `db:"checks"` // Store as JSON
	Checks     []spamcheck.Response `db:"-"`      // Don't store in DB directly, for db it uses ChecksJSON
}

var detectedSpamSchema = `
	CREATE TABLE IF NOT EXISTS detected_spam (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		gid TEXT NOT NULL DEFAULT '',
		text TEXT,
		user_id INTEGER,
		user_name TEXT,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		added BOOLEAN DEFAULT 0,
		checks TEXT
	);
	CREATE INDEX IF NOT EXISTS idx_detected_spam_timestamp ON detected_spam(timestamp);
	CREATE INDEX IF NOT EXISTS idx_detected_spam_gid ON detected_spam(gid)
`

// NewDetectedSpam creates a new DetectedSpam storage
func NewDetectedSpam(ctx context.Context, db *Engine) (*DetectedSpam, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}

	// first check if the table exists. we can't do this in a transaction
	// because missing columns will cause the transaction to fail
	var exists int
	err := db.GetContext(ctx, &exists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='detected_spam'")
	if err != nil {
		return nil, fmt.Errorf("failed to check for detected_spam table existence: %w", err)
	}

	if exists == 0 { // table does not exist, create it
		tx, err := db.Begin()
		if err != nil {
			return nil, fmt.Errorf("failed to start transaction: %w", err)
		}
		defer tx.Rollback()

		if _, err = tx.ExecContext(ctx, detectedSpamSchema); err != nil {
			return nil, fmt.Errorf("failed to create schema: %w", err)
		}

		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit transaction: %w", err)
		}
	}

	// migrate detected_spam table
	if err := migrateDetectedSpam(&db.DB, db.GID()); err != nil {
		return nil, fmt.Errorf("failed to migrate detected_spam: %w", err)
	}

	return &DetectedSpam{db: db}, nil
}

// Write adds a new detected spam entry
func (ds *DetectedSpam) Write(ctx context.Context, entry DetectedSpamInfo, checks []spamcheck.Response) error {
	ds.db.Lock()
	defer ds.db.Unlock()

	if entry.GID == "" || entry.Text == "" || entry.UserID == 0 || entry.UserName == "" {
		return fmt.Errorf("missing required fields")
	}

	checksJSON, err := json.Marshal(checks)
	if err != nil {
		return fmt.Errorf("failed to marshal checks: %w", err)
	}

	query := `INSERT INTO detected_spam (gid, text, user_id, user_name, timestamp, checks) VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := ds.db.ExecContext(ctx, query, entry.GID, entry.Text, entry.UserID, entry.UserName, entry.Timestamp, checksJSON); err != nil {
		return fmt.Errorf("failed to insert detected spam entry: %w", err)
	}

	log.Printf("[INFO] detected spam entry added for gid:%s, user_id:%d, name:%s", entry.GID, entry.UserID, entry.UserName)
	return nil
}

// SetAddedToSamplesFlag sets the added flag to true for the detected spam entry with the given id
func (ds *DetectedSpam) SetAddedToSamplesFlag(ctx context.Context, id int64) error {
	ds.db.Lock()
	defer ds.db.Unlock()

	query := `UPDATE detected_spam SET added = 1 WHERE id = ?`
	if _, err := ds.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("failed to update added to samples flag: %w", err)
	}
	return nil
}

// Read returns all detected spam entries
func (ds *DetectedSpam) Read(ctx context.Context) ([]DetectedSpamInfo, error) {
	ds.db.RLock()
	defer ds.db.RUnlock()

	var entries []DetectedSpamInfo
	err := ds.db.SelectContext(ctx, &entries, "SELECT * FROM detected_spam ORDER BY timestamp DESC LIMIT ?", maxDetectedSpamEntries)
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

func migrateDetectedSpam(db *sqlx.DB, gid string) error {
	var exists int
	err := db.Get(&exists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='detected_spam'")
	if err != nil {
		return fmt.Errorf("failed to check for detected_spam table existence: %w", err)
	}

	if exists == 0 {
		return nil
	}

	// add gid column if it doesn't exist
	if _, err = db.Exec(`ALTER TABLE detected_spam ADD COLUMN gid TEXT DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("failed to alter detected_spam table: %w", err)
		}
	}

	// update existing records with the provided gid
	res, err := db.Exec("UPDATE detected_spam SET gid = ? WHERE gid = ''", gid)
	if err != nil {
		return fmt.Errorf("failed to update gid for existing records: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	log.Printf("[DEBUG] detected_spam table migrated, gid updated to %q, records: %d", gid, rowsAffected)
	return nil
}
