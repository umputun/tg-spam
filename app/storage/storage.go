// Package storage provides a storage engine for sql databases.
// The storage engine is a wrapper around sqlx.DB with additional functionality to work with the various types of database engines.
// Each table is represented by a struct, and each struct has a method to work the table with business logic for this data type.
package storage

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver loaded here
)

// EngineType is a type of database engine
type EngineType string

// enum of supported database engines
const (
	EngineTypeUnknown EngineType = ""
	EngineTypeSqlite  EngineType = "sqlite"
)

// Engine is a wrapper for sqlx.DB with type.
// EngineType allows distinguishing between different database engines.
type Engine struct {
	sqlx.DB
	gid    string     // group id, to allow per-group storage in the same database
	dbType EngineType // type of the database engine
}

// NewSqliteDB creates a new sqlite database
func NewSqliteDB(file, gid string) (*Engine, error) {
	db, err := sqlx.Connect("sqlite", file)
	if err != nil {
		return &Engine{}, err
	}
	if err := setSqlitePragma(db); err != nil {
		return &Engine{}, err
	}
	return &Engine{DB: *db, gid: gid, dbType: EngineTypeSqlite}, nil
}

// GID returns the group id
func (e *Engine) GID() string {
	return e.gid
}

// Type returns the database engine type
func (e *Engine) Type() EngineType {
	return e.dbType
}

// MakeLock creates a new lock for the database engine
func (e *Engine) MakeLock() RWLocker {
	if e.dbType == EngineTypeSqlite {
		return new(sync.RWMutex) // sqlite need locking
	}
	return &NoopLocker{} // other engines don't need locking
}

func setSqlitePragma(db *sqlx.DB) error {
	// Set pragmas for SQLite. Commented out pragmas as they are not used in the code yet because we need
	// to make sure if it is worth having 2 more DB-related files for WAL and SHM.
	pragmas := map[string]string{
		// "journal_mode": "WAL",
		// "synchronous":  "NORMAL",
		// "busy_timeout": "5000",
		// "foreign_keys": "ON",
	}

	// set pragma
	for name, value := range pragmas {
		if _, err := db.Exec("PRAGMA " + name + " = " + value); err != nil {
			return err
		}
	}
	return nil
}

// SampleUpdater is a service to update dynamic (user's) samples for detector, either ham or spam.
// The service is used by the detector to update the samples.
type SampleUpdater struct {
	samplesService *Samples
	sampleType     SampleType
	timeout        time.Duration
}

// NewSampleUpdater creates a new SampleUpdater
func NewSampleUpdater(samplesService *Samples, sampleType SampleType, timeout time.Duration) *SampleUpdater {
	return &SampleUpdater{samplesService: samplesService, sampleType: sampleType, timeout: timeout}
}

// Append a message to the samples, forcing user origin
func (u *SampleUpdater) Append(msg string) error {
	ctx, cancel := context.Background(), func() {}
	if u.timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), u.timeout)
	}
	defer cancel()
	return u.samplesService.Add(ctx, u.sampleType, SampleOriginUser, msg)
}

// Remove a message from the samples
func (u *SampleUpdater) Remove(msg string) error {
	ctx, cancel := context.Background(), func() {}
	if u.timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), u.timeout)
	}
	defer cancel()
	return u.samplesService.DeleteMessage(ctx, msg)
}

// Reader returns a reader for the samples
func (u *SampleUpdater) Reader() (io.ReadCloser, error) {
	// we don't want to pass context with timeout here, as it's an async operation
	return u.samplesService.Reader(context.Background(), u.sampleType, SampleOriginUser)
}

// RWLocker is a read-write locker interface
type RWLocker interface {
	sync.Locker
	RLock()
	RUnlock()
}

// NoopLocker is a no-op locker
type NoopLocker struct{}

// Lock is a no-op
func (NoopLocker) Lock() {}

// Unlock is a no-op
func (NoopLocker) Unlock() {}

// RLock is a no-op
func (NoopLocker) RLock() {}

// RUnlock is a no-op
func (NoopLocker) RUnlock() {}

// initDB initializes db table with a schema and handles migration in a transaction
func initDB(ctx context.Context, db *Engine, tableName, schema string, migrateFn func(context.Context, *sqlx.Tx, string) error) error {
	if db == nil {
		return fmt.Errorf("db connection is nil")
	}

	tx, err := db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	var exists int
	err = tx.GetContext(ctx, &exists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", tableName)
	if err != nil {
		return fmt.Errorf("failed to check for %s table existence: %w", tableName, err)
	}

	if exists == 0 {
		// create schema if it doesn't exist, no migration needed
		if _, err = tx.ExecContext(ctx, schema); err != nil {
			return fmt.Errorf("failed to create schema: %w", err)
		}
	}

	if exists > 0 {
		// migrate existing table
		if err = migrateFn(ctx, tx, db.GID()); err != nil {
			return fmt.Errorf("failed to migrate %s: %w", tableName, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
