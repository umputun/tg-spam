package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/umputun/tg-spam/app/storage/engine"
)

// Store provides access to settings stored in database
type Store struct {
	*engine.SQL
	engine.RWLocker
}

// all config queries
const (
	CmdCreateConfigTable engine.DBCmd = iota + 1000
	CmdCreateConfigIndexes
	CmdUpsertConfig
	CmdSelectConfig
	CmdDeleteConfig
	CmdSelectConfigUpdatedAt
	CmdCountConfig
)

// queries holds all config queries
var configQueries = engine.NewQueryMap().
	Add(CmdCreateConfigTable, engine.Query{
		Sqlite: `CREATE TABLE IF NOT EXISTS config (
			id INTEGER PRIMARY KEY,
			gid TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(gid)
		)`,
		Postgres: `CREATE TABLE IF NOT EXISTS config (
			id SERIAL PRIMARY KEY,
			gid TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(gid)
		)`,
	}).
	AddSame(CmdCreateConfigIndexes, `CREATE INDEX IF NOT EXISTS idx_config_gid ON config(gid)`).
	Add(CmdUpsertConfig, engine.Query{
		Sqlite: `INSERT INTO config (gid, data, updated_at) 
			VALUES (?, ?, ?) 
			ON CONFLICT (gid) DO UPDATE 
			SET data = excluded.data, updated_at = excluded.updated_at`,
		Postgres: `INSERT INTO config (gid, data, updated_at) 
			VALUES ($1, $2, $3) 
			ON CONFLICT (gid) DO UPDATE 
			SET data = EXCLUDED.data, updated_at = EXCLUDED.updated_at`,
	}).
	AddSame(CmdSelectConfig, `SELECT data FROM config WHERE gid = ?`).
	AddSame(CmdDeleteConfig, `DELETE FROM config WHERE gid = ?`).
	AddSame(CmdSelectConfigUpdatedAt, `SELECT updated_at FROM config WHERE gid = ?`).
	AddSame(CmdCountConfig, `SELECT COUNT(*) FROM config WHERE gid = ?`)

// NewStore creates a new settings store
func NewStore(ctx context.Context, db *engine.SQL) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("no db provided")
	}

	res := &Store{SQL: db, RWLocker: db.MakeLock()}

	// initialize the database table using the TableConfig pattern
	cfg := engine.TableConfig{
		Name:          "config",
		CreateTable:   CmdCreateConfigTable,
		CreateIndexes: CmdCreateConfigIndexes,
		MigrateFunc:   noopMigrate, // no migration needed for config table
		QueriesMap:    configQueries,
	}

	if err := engine.InitTable(ctx, db, cfg); err != nil {
		return nil, fmt.Errorf("failed to init config table: %w", err)
	}

	return res, nil
}

// Load retrieves the settings from the database
func (s *Store) Load(ctx context.Context) (*Settings, error) {
	s.RLock()
	defer s.RUnlock()

	var record struct {
		Data string `db:"data"`
	}

	query, err := configQueries.Pick(s.Type(), CmdSelectConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get select query: %w", err)
	}

	query = s.Adopt(query)
	err = s.GetContext(ctx, &record, query, s.GID())
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, fmt.Errorf("no settings found in database")
		}
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}

	result := New()
	if err := json.Unmarshal([]byte(record.Data), result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	return result, nil
}

// Save stores the settings to the database
func (s *Store) Save(ctx context.Context, settings *Settings) error {
	if settings == nil {
		return fmt.Errorf("nil settings")
	}

	s.Lock()
	defer s.Unlock()

	// create a safe copy without sensitive information
	safeCopy := *settings // make a shallow copy

	// clear transient fields that shouldn't be persisted
	safeCopy.Transient = TransientSettings{}

	// ensure credentials are properly saved in domain models
	// they are already stored in the proper domain fields like:
	// - Telegram.Token
	// - OpenAI.Token
	// - Server.AuthHash

	data, err := json.Marshal(&safeCopy)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	query, err := configQueries.Pick(s.Type(), CmdUpsertConfig)
	if err != nil {
		return fmt.Errorf("failed to get upsert query: %w", err)
	}

	query = s.Adopt(query)
	_, err = s.ExecContext(ctx, query, s.GID(), string(data), time.Now())
	if err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}

	return nil
}

// Delete removes the settings from the database
func (s *Store) Delete(ctx context.Context) error {
	s.Lock()
	defer s.Unlock()

	query, err := configQueries.Pick(s.Type(), CmdDeleteConfig)
	if err != nil {
		return fmt.Errorf("failed to get delete query: %w", err)
	}

	query = s.Adopt(query)
	_, err = s.ExecContext(ctx, query, s.GID())
	if err != nil {
		return fmt.Errorf("failed to delete settings: %w", err)
	}

	return nil
}

// LastUpdated returns the last update time of the settings
func (s *Store) LastUpdated(ctx context.Context) (time.Time, error) {
	s.RLock()
	defer s.RUnlock()

	var record struct {
		UpdatedAt time.Time `db:"updated_at"`
	}

	query, err := configQueries.Pick(s.Type(), CmdSelectConfigUpdatedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get updated_at query: %w", err)
	}

	query = s.Adopt(query)
	err = s.GetContext(ctx, &record, query, s.GID())
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return time.Time{}, fmt.Errorf("no settings found in database")
		}
		return time.Time{}, fmt.Errorf("failed to get settings update time: %w", err)
	}

	return record.UpdatedAt, nil
}

// Exists checks if settings exist in the database
func (s *Store) Exists(ctx context.Context) (bool, error) {
	s.RLock()
	defer s.RUnlock()

	var count int
	query, err := configQueries.Pick(s.Type(), CmdCountConfig)
	if err != nil {
		return false, fmt.Errorf("failed to get count query: %w", err)
	}

	query = s.Adopt(query)
	err = s.GetContext(ctx, &count, query, s.GID())
	if err != nil {
		return false, fmt.Errorf("failed to check if settings exist: %w", err)
	}

	return count > 0, nil
}

// noopMigrate is a no-op migration function for the config table
// since there's no need for migrations currently
func noopMigrate(_ context.Context, _ *sqlx.Tx, _ string) error {
	log.Printf("[DEBUG] no migration needed for config table")
	return nil
}
