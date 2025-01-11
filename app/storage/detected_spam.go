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

	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

const maxDetectedSpamEntries = 500

// DetectedSpam is a storage for detected spam entries
type DetectedSpam struct {
	db *engine.SQL
	engine.RWLocker
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
	Checks     []spamcheck.Response `db:"-"`      // Don't store in DB directly
}

// CmdCreateDetectedSpamTable creates detected_spam table
const CmdCreateDetectedSpamTable engine.DBCmd = iota + 200

// queries holds all detected spam queries
var detectedSpamQueries = engine.QueryMap{
	engine.Sqlite: {
		CmdCreateDetectedSpamTable: `
			CREATE TABLE IF NOT EXISTS detected_spam (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				gid TEXT NOT NULL DEFAULT '',
				text TEXT,
				user_id INTEGER,
				user_name TEXT,
				timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
				added BOOLEAN DEFAULT 0,
				checks TEXT
			)`,
		CmdAddGIDColumn: "ALTER TABLE detected_spam ADD COLUMN gid TEXT DEFAULT ''",
	},
	engine.Postgres: {
		CmdCreateDetectedSpamTable: `
			CREATE TABLE IF NOT EXISTS detected_spam (
				id SERIAL PRIMARY KEY,
				gid TEXT NOT NULL DEFAULT '',
				text TEXT,
				user_id BIGINT,
				user_name TEXT,
				timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				added BOOLEAN DEFAULT false,
				checks TEXT
			)`,
		CmdAddGIDColumn: "ALTER TABLE detected_spam ADD COLUMN IF NOT EXISTS gid TEXT DEFAULT ''",
	},
}

// NewDetectedSpam creates a new DetectedSpam storage
func NewDetectedSpam(ctx context.Context, db *engine.SQL) (*DetectedSpam, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}

	res := &DetectedSpam{db: db, RWLocker: db.MakeLock()}
	if err := res.init(ctx); err != nil {
		return nil, fmt.Errorf("failed to init detected spam storage: %w", err)
	}
	return res, nil
}

func (ds *DetectedSpam) init(ctx context.Context) error {
	tx, err := ds.db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// create table first
	createSchema, err := engine.PickQuery(detectedSpamQueries, ds.db.Type(), CmdCreateDetectedSpamTable)
	if err != nil {
		return fmt.Errorf("failed to get create table query: %w", err)
	}
	if _, err = tx.ExecContext(ctx, createSchema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// try to migrate if needed
	if err = ds.migrate(ctx, tx, ds.db.GID()); err != nil {
		return fmt.Errorf("failed to migrate table: %w", err)
	}

	// create indices after migration when all columns exist

	createIndexes := `
		CREATE INDEX IF NOT EXISTS idx_detected_spam_timestamp ON detected_spam(timestamp);
		CREATE INDEX IF NOT EXISTS idx_detected_spam_gid ON detected_spam(gid)`
	if _, err = tx.ExecContext(ctx, createIndexes); err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
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

	query := ds.db.Adopt("INSERT INTO detected_spam (gid, text, user_id, user_name, timestamp, checks) VALUES (?, ?, ?, ?, ?, ?)")
	_, err = ds.db.ExecContext(ctx, query, entry.GID, entry.Text, entry.UserID, entry.UserName, entry.Timestamp, string(checksJSON))
	if err != nil {
		return fmt.Errorf("failed to insert detected spam entry: %w", err)
	}

	log.Printf("[INFO] detected spam entry added for gid:%s, user_id:%d, name:%s", entry.GID, entry.UserID, entry.UserName)
	return nil
}

// SetAddedToSamplesFlag sets the added flag to true for the detected spam entry with the given id
func (ds *DetectedSpam) SetAddedToSamplesFlag(ctx context.Context, id int64) error {
	ds.Lock()
	defer ds.Unlock()

	query := ds.db.Adopt("UPDATE detected_spam SET added = ? WHERE id = ?")
	if _, err := ds.db.ExecContext(ctx, query, true, id); err != nil {
		return fmt.Errorf("failed to update added to samples flag: %w", err)
	}
	return nil
}

// Read returns all detected spam entries
func (ds *DetectedSpam) Read(ctx context.Context) ([]DetectedSpamInfo, error) {
	ds.RLock()
	defer ds.RUnlock()

	query := ds.db.Adopt("SELECT * FROM detected_spam ORDER BY timestamp DESC LIMIT ?")
	var entries []DetectedSpamInfo
	err := ds.db.SelectContext(ctx, &entries, query, maxDetectedSpamEntries)
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

	query := ds.db.Adopt("SELECT * FROM detected_spam WHERE user_id = ? AND gid = ? ORDER BY timestamp DESC LIMIT 1")
	var entry DetectedSpamInfo
	err := ds.db.GetContext(ctx, &entry, query, userID, ds.db.GID())
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

func (ds *DetectedSpam) migrate(ctx context.Context, tx *sqlx.Tx, gid string) error {
	// try to select with new structure, if works - already migrated
	var count int
	err := tx.GetContext(ctx, &count, "SELECT COUNT(*) FROM detected_spam WHERE gid = ''")
	if err == nil {
		log.Printf("[DEBUG] detected_spam table already migrated")
		return nil
	}

	// add gid column using db-specific query
	addGIDQuery, err := engine.PickQuery(detectedSpamQueries, ds.db.Type(), CmdAddGIDColumn)
	if err != nil {
		return fmt.Errorf("failed to get add GID query: %w", err)
	}

	_, err = tx.ExecContext(ctx, addGIDQuery)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("failed to add gid column: %w", err)
	}

	// update existing records with provided gid
	if _, err = tx.ExecContext(ctx, "UPDATE detected_spam SET gid = ? WHERE gid = ''", gid); err != nil {
		return fmt.Errorf("failed to update gid for existing records: %w", err)
	}

	log.Printf("[DEBUG] detected_spam table migrated")
	return nil
}
