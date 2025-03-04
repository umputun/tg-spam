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
	*engine.SQL
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
	ChecksJSON string               `db:"checks"` // store as JSON
	Checks     []spamcheck.Response `db:"-"`      // don't store in DB directly
}

// detected spam query commands
const (
	CmdCreateDetectedSpamTable engine.DBCmd = iota + 200
	CmdCreateDetectedSpamIndexes
)

// queries holds all detected spam queries
var detectedSpamQueries = engine.NewQueryMap().
	Add(CmdCreateDetectedSpamTable, engine.Query{
		Sqlite: `CREATE TABLE IF NOT EXISTS detected_spam (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            gid TEXT NOT NULL DEFAULT '',
            text TEXT,
            user_id INTEGER,
            user_name TEXT,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
            added BOOLEAN DEFAULT 0,
            checks TEXT
        )`,
		Postgres: `CREATE TABLE IF NOT EXISTS detected_spam (
            id SERIAL PRIMARY KEY,
            gid TEXT NOT NULL DEFAULT '',
            text TEXT,
            user_id BIGINT,
            user_name TEXT,
            timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            added BOOLEAN DEFAULT false,
            checks TEXT
        )`,
	}).
	AddSame(CmdCreateDetectedSpamIndexes, `
      	CREATE INDEX IF NOT EXISTS idx_detected_spam_gid_ts ON detected_spam(gid, timestamp DESC);
        CREATE INDEX IF NOT EXISTS idx_detected_spam_user_id_gid ON detected_spam(user_id, gid);
		CREATE INDEX IF NOT EXISTS idx_spam_gid_time ON detected_spam(gid, timestamp DESC);
        CREATE INDEX IF NOT EXISTS idx_detected_spam_gid ON detected_spam(gid)`,
	).
	Add(CmdAddGIDColumn, engine.Query{
		Sqlite:   "ALTER TABLE detected_spam ADD COLUMN gid TEXT DEFAULT ''",
		Postgres: "ALTER TABLE detected_spam ADD COLUMN IF NOT EXISTS gid TEXT DEFAULT ''",
	})

// NewDetectedSpam creates a new DetectedSpam storage
func NewDetectedSpam(ctx context.Context, db *engine.SQL) (*DetectedSpam, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}
	res := &DetectedSpam{SQL: db, RWLocker: db.MakeLock()}
	cfg := engine.TableConfig{
		Name:          "detected_spam",
		CreateTable:   CmdCreateDetectedSpamTable,
		CreateIndexes: CmdCreateDetectedSpamIndexes,
		MigrateFunc:   res.migrate,
		QueriesMap:    detectedSpamQueries,
	}
	if err := engine.InitTable(ctx, db, cfg); err != nil {
		return nil, fmt.Errorf("failed to init detected spam storage: %w", err)
	}
	return res, nil
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

	query := ds.Adopt("INSERT INTO detected_spam (gid, text, user_id, user_name, timestamp, checks) VALUES (?, ?, ?, ?, ?, ?)")
	_, err = ds.ExecContext(ctx, query, entry.GID, entry.Text, entry.UserID, entry.UserName, entry.Timestamp, string(checksJSON))
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

	query := ds.Adopt("UPDATE detected_spam SET added = ? WHERE id = ?")
	if _, err := ds.ExecContext(ctx, query, true, id); err != nil {
		return fmt.Errorf("failed to update added to samples flag: %w", err)
	}
	return nil
}

// Read returns the latest detected spam entries, up to maxDetectedSpamEntries
func (ds *DetectedSpam) Read(ctx context.Context) ([]DetectedSpamInfo, error) {
	ds.RLock()
	defer ds.RUnlock()

	query := ds.Adopt("SELECT * FROM detected_spam WHERE gid = ? ORDER BY timestamp DESC LIMIT ?")
	var entries []DetectedSpamInfo
	err := ds.SelectContext(ctx, &entries, query, ds.GID(), maxDetectedSpamEntries)
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

	query := ds.Adopt("SELECT * FROM detected_spam WHERE user_id = ? AND gid = ? ORDER BY timestamp DESC LIMIT 1")
	var entry DetectedSpamInfo
	err := ds.GetContext(ctx, &entry, query, userID, ds.GID())
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
	addGIDQuery, err := detectedSpamQueries.Pick(ds.Type(), CmdAddGIDColumn)
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
