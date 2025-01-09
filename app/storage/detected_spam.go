package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	RWLocker
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
	err := initDB(ctx, db, "detected_spam", detectedSpamSchema, migrateDetectedSpamTx)
	if err != nil {
		return nil, err
	}
	return &DetectedSpam{db: db, RWLocker: db.MakeLock()}, nil
}

// Write adds a new detected spam entry
func (ds *DetectedSpam) Write(ctx context.Context, entry DetectedSpamInfo, checks []spamcheck.Response) error {
	ds.Lock()
	defer ds.Unlock()

	if entry.GID == "" {
		return fmt.Errorf("missing required GID field")
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
	ds.Lock()
	defer ds.Unlock()

	query := `UPDATE detected_spam SET added = 1 WHERE id = ?`
	if _, err := ds.db.ExecContext(ctx, query, id); err != nil {
		return fmt.Errorf("failed to update added to samples flag: %w", err)
	}
	return nil
}

// Read returns all detected spam entries
func (ds *DetectedSpam) Read(ctx context.Context) ([]DetectedSpamInfo, error) {
	ds.RLock()
	defer ds.RUnlock()

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

// FindByUserID returns the latest detected spam entry for the given user ID
func (ds *DetectedSpam) FindByUserID(ctx context.Context, userID int64) (*DetectedSpamInfo, error) {
	ds.RLock()
	defer ds.RUnlock()

	var entry DetectedSpamInfo
	err := ds.db.GetContext(ctx, &entry, "SELECT * FROM detected_spam WHERE user_id = ? AND gid = ?"+
		" ORDER BY timestamp DESC LIMIT 1", userID, ds.db.GID())
	if errors.Is(err, sql.ErrNoRows) {
		// not found, return nil *DetectedSpamInfo instead of error
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get detected spam entry for user_id %d: %w", userID, err)
	}

	var checks []spamcheck.Response
	if err := json.Unmarshal([]byte(entry.ChecksJSON), &checks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal checks for entry: %w", err)
	}
	entry.Checks = checks
	entry.Timestamp = entry.Timestamp.Local()
	return &entry, nil
}

func migrateDetectedSpamTx(ctx context.Context, tx *sqlx.Tx, gid string) error {
	// check if gid column exists
	var cols []struct {
		CID       int     `db:"cid"`
		Name      string  `db:"name"`
		Type      string  `db:"type"`
		NotNull   bool    `db:"notnull"`
		DfltValue *string `db:"dflt_value"`
		PK        bool    `db:"pk"`
	}
	if err := tx.Select(&cols, "PRAGMA table_info(detected_spam)"); err != nil {
		return fmt.Errorf("failed to get table info: %w", err)
	}

	hasGID := false
	for _, col := range cols {
		if col.Name == "gid" {
			hasGID = true
			break
		}
	}

	if !hasGID {
		if _, err := tx.ExecContext(ctx, "ALTER TABLE detected_spam ADD COLUMN gid TEXT DEFAULT ''"); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("failed to add gid column: %w", err)
			}
			return nil
		}

		// update existing records
		if _, err := tx.ExecContext(ctx, "UPDATE detected_spam SET gid = ? WHERE gid = ''", gid); err != nil {
			return fmt.Errorf("failed to update gid for existing records: %w", err)
		}

		log.Printf("[DEBUG] detected_spam table migrated, gid column added")
	}

	return nil
}
