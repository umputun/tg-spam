package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/umputun/tg-spam/app/storage/engine"
)

// Config provides access to configuration stored in database
type Config[T any] struct {
	*engine.SQL
	engine.RWLocker
}

// ConfigRecord represents a configuration entry
type ConfigRecord struct {
	GID       string    `db:"gid"`
	Data      string    `db:"data"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}

// all config queries
const (
	CmdCreateConfigTable engine.DBCmd = iota + 200
	CmdCreateConfigIndexes
	CmdSetConfig
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
	AddSame(CmdCreateConfigIndexes, `
		CREATE INDEX IF NOT EXISTS idx_config_gid ON config(gid)
	`).
	Add(CmdSetConfig, engine.Query{
		Sqlite: `INSERT INTO config (gid, data, updated_at) 
			VALUES (?, ?, ?) 
			ON CONFLICT (gid) DO UPDATE 
			SET data = excluded.data, updated_at = excluded.updated_at`,
		Postgres: `INSERT INTO config (gid, data, updated_at) 
			VALUES ($1, $2, $3) 
			ON CONFLICT (gid) DO UPDATE 
			SET data = EXCLUDED.data, updated_at = EXCLUDED.updated_at`,
	})

// NewConfig creates new config instance
func NewConfig[T any](ctx context.Context, db *engine.SQL) (*Config[T], error) {
	if db == nil {
		return nil, fmt.Errorf("no db provided")
	}

	res := &Config[T]{SQL: db, RWLocker: db.MakeLock()}
	cfg := engine.TableConfig{
		Name:          "config",
		CreateTable:   CmdCreateConfigTable,
		CreateIndexes: CmdCreateConfigIndexes,
		QueriesMap:    configQueries,
		MigrateFunc:   func(_ context.Context, _ *sqlx.Tx, _ string) error { return nil }, // no-op migrate function
	}
	if err := engine.InitTable(ctx, db, cfg); err != nil {
		return nil, fmt.Errorf("failed to init config table: %w", err)
	}
	return res, nil
}

// Get retrieves the configuration for the current group
func (c *Config[T]) Get(ctx context.Context) (string, error) {
	c.RLock()
	defer c.RUnlock()

	var record ConfigRecord
	query := c.Adopt("SELECT data FROM config WHERE gid = ?")
	err := c.GetContext(ctx, &record, query, c.GID())
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return "", fmt.Errorf("no configuration found")
		}
		return "", fmt.Errorf("failed to get config: %w", err)
	}
	return record.Data, nil
}

// GetObject retrieves the configuration and deserializes it into the provided struct
func (c *Config[T]) GetObject(ctx context.Context, obj *T) error {
	data, err := c.Get(ctx)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(data), obj); err != nil {
		return fmt.Errorf("failed to unmarshal config: %w", err)
	}
	return nil
}

// Set updates or creates the configuration
func (c *Config[T]) Set(ctx context.Context, data string) error {
	if data == "" {
		return fmt.Errorf("empty data not allowed")
	}

	if data == "null" {
		return fmt.Errorf("null value not allowed")
	}

	// validate JSON structure
	var jsonCheck interface{}
	if err := json.Unmarshal([]byte(data), &jsonCheck); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// validate against the generic type
	var typeCheck T
	if err := json.Unmarshal([]byte(data), &typeCheck); err != nil {
		return fmt.Errorf("invalid type: %w", err)
	}

	c.Lock()
	defer c.Unlock()

	query, err := configQueries.Pick(c.Type(), CmdSetConfig)
	if err != nil {
		return fmt.Errorf("failed to get set query: %w", err)
	}

	// for PostgreSQL, we need to use transactions
	if c.Type() == engine.Postgres {
		return c.setConfigPostgres(ctx, query, data)
	}

	// for SQLite, the RWLock is sufficient
	_, err = c.ExecContext(ctx, query, c.GID(), data, time.Now())
	if err != nil {
		return fmt.Errorf("failed to set config: %w", err)
	}
	return nil
}

// SetObject serializes an object to JSON and stores it
func (c *Config[T]) SetObject(ctx context.Context, obj *T) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return c.Set(ctx, string(data))
}

// Delete removes the configuration
func (c *Config[T]) Delete(ctx context.Context) error {
	c.Lock()
	defer c.Unlock()

	query := c.Adopt("DELETE FROM config WHERE gid = ?")
	_, err := c.ExecContext(ctx, query, c.GID())
	if err != nil {
		return fmt.Errorf("failed to delete config: %w", err)
	}
	return nil
}

// setConfigPostgres handles PostgreSQL-specific configuration storage with transaction
func (c *Config[T]) setConfigPostgres(ctx context.Context, query, data string) error {
	// begin transaction
	tx, err := c.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	// set up rollback on error
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// lock the row for update to ensure serialization
	_, err = tx.ExecContext(ctx, "SELECT id FROM config WHERE gid = $1 FOR UPDATE", c.GID())
	if err != nil && err.Error() != "sql: no rows in result set" {
		return fmt.Errorf("failed to lock row: %w", err)
	}

	// execute the main query
	_, err = tx.ExecContext(ctx, query, c.GID(), data, time.Now())
	if err != nil {
		return fmt.Errorf("failed to set config: %w", err)
	}

	// commit the transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// LastUpdated returns the last update time of the configuration
func (c *Config[T]) LastUpdated(ctx context.Context) (time.Time, error) {
	c.RLock()
	defer c.RUnlock()

	var record struct {
		UpdatedAt time.Time `db:"updated_at"`
	}
	query := c.Adopt("SELECT updated_at FROM config WHERE gid = ?")
	err := c.GetContext(ctx, &record, query, c.GID())
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return time.Time{}, fmt.Errorf("no configuration found")
		}
		return time.Time{}, fmt.Errorf("failed to get config update time: %w", err)
	}
	return record.UpdatedAt, nil
}
