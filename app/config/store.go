package config

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/umputun/tg-spam/app/storage/engine"
)

// Store provides access to settings stored in database
type Store struct {
	*engine.SQL
	engine.RWLocker
}

// NewStore creates a new settings store
func NewStore(ctx context.Context, db *engine.SQL) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("no db provided")
	}

	res := &Store{SQL: db, RWLocker: db.MakeLock()}

	// initialize the database table
	err := initDbTable(ctx, res.SQL)
	if err != nil {
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

	query := s.Adopt("SELECT data FROM config WHERE gid = ?")
	err := s.GetContext(ctx, &record, query, s.GID())
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

	data, err := json.Marshal(&safeCopy)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	query := s.Adopt(`
		INSERT INTO config (gid, data, updated_at) 
		VALUES (?, ?, ?) 
		ON CONFLICT (gid) DO UPDATE 
		SET data = excluded.data, updated_at = excluded.updated_at
	`)

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

	query := s.Adopt("DELETE FROM config WHERE gid = ?")
	_, err := s.ExecContext(ctx, query, s.GID())
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

	query := s.Adopt("SELECT updated_at FROM config WHERE gid = ?")
	err := s.GetContext(ctx, &record, query, s.GID())
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
	query := s.Adopt("SELECT COUNT(*) FROM config WHERE gid = ?")
	err := s.GetContext(ctx, &count, query, s.GID())
	if err != nil {
		return false, fmt.Errorf("failed to check if settings exist: %w", err)
	}

	return count > 0, nil
}

// initDbTable initializes the config table if it doesn't exist
func initDbTable(ctx context.Context, db *engine.SQL) error {
	var createTableSQL string

	if db.Type() == engine.Postgres {
		createTableSQL = `
			CREATE TABLE IF NOT EXISTS config (
				id SERIAL PRIMARY KEY,
				gid TEXT NOT NULL,
				data TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(gid)
			);
			
			DO $$
			BEGIN
				IF NOT EXISTS (
					SELECT 1 FROM pg_indexes WHERE indexname = 'idx_config_gid'
				) THEN
					CREATE INDEX idx_config_gid ON config(gid);
				END IF;
			END $$;
		`
	} else {
		// sQLite
		createTableSQL = `
			CREATE TABLE IF NOT EXISTS config (
				id INTEGER PRIMARY KEY,
				gid TEXT NOT NULL,
				data TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(gid)
			);
			
			CREATE INDEX IF NOT EXISTS idx_config_gid ON config(gid);
		`
	}

	_, err := db.ExecContext(ctx, createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create config table: %w", err)
	}
	return nil
}
