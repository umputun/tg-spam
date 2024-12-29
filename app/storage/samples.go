package storage

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"iter"
	"sync"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite/lib" // sqlite driver
)

// Samples is a storage for samples. It supports both ham and spam, as well as preset samples and user's samples
type Samples struct {
	db   *sqlx.DB
	lock *sync.RWMutex
}

// SampleType represents the type of the sample
type SampleType string

// enum for sample types
const (
	SampleTypeHam  SampleType = "ham"
	SampleTypeSpam SampleType = "spam"
)

// SampleOrigin represents the origin of the sample
type SampleOrigin string

// enum for sample origins
const (
	SampleOriginPreset SampleOrigin = "preset"
	SampleOriginUser   SampleOrigin = "user"
	SampleOriginAny    SampleOrigin = "any"
)

// NewSamples creates a new Samples storage
func NewSamples(db *sqlx.DB) (*Samples, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}

	if err := setSqlitePragma(db); err != nil {
		return nil, fmt.Errorf("failed to set sqlite pragma: %w", err)
	}

	// create schema in a single transaction
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	schema := `
        CREATE TABLE IF NOT EXISTS samples (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
            type TEXT CHECK (type IN ('ham', 'spam')),
            origin TEXT CHECK (origin IN ('preset', 'user')),
            message NOT NULL UNIQUE
        );
        CREATE INDEX IF NOT EXISTS idx_samples_timestamp ON samples(timestamp);
        CREATE INDEX IF NOT EXISTS idx_samples_type ON samples(type);
        CREATE INDEX IF NOT EXISTS idx_samples_origin ON samples(origin);
    `

	if _, err = tx.Exec(schema); err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &Samples{db: db, lock: &sync.RWMutex{}}, nil
}

// Add adds a sample to the storage. Checks if the sample is already present and skips it if it is.
func (s *Samples) Add(ctx context.Context, t SampleType, o SampleOrigin, message string) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if err := o.Validate(); err != nil {
		return err
	}
	if o == SampleOriginAny {
		return fmt.Errorf("can't add sample with origin 'any'")
	}
	if message == "" {
		return fmt.Errorf("message can't be empty")
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	// start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// add new sample, replace if exists
	query := `INSERT OR REPLACE INTO samples (type, origin, message) VALUES (?, ?, ?)`
	if _, err = tx.ExecContext(ctx, query, t, o, message); err != nil {
		return fmt.Errorf("failed to add sample: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// Delete removes a sample from the storage by its ID
func (s *Samples) Delete(ctx context.Context, id int64) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	result, err := s.db.ExecContext(ctx, `DELETE FROM samples WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to remove sample: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("sample %d not found", id)
	}
	return nil
}

// Read reads samples from storage by type and origin
func (s *Samples) Read(ctx context.Context, t SampleType, o SampleOrigin) ([]string, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	if err := t.Validate(); err != nil {
		return nil, err
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}

	var (
		query   string
		args    []any
		samples []string
	)

	if o == SampleOriginAny {
		query = `SELECT message FROM samples WHERE type = ?`
		args = []any{t}
	} else {
		query = `SELECT message FROM samples WHERE type = ? AND origin = ?`
		args = []any{t, o}
	}

	if err := s.db.SelectContext(ctx, &samples, query, args...); err != nil {
		return nil, fmt.Errorf("failed to get samples: %w", err)
	}
	return samples, nil
}

// Iterator returns an iterator for samples by type and origin
// Sorts samples by timestamp in descending order, i.e. from the newest to the oldest
func (s *Samples) Iterator(ctx context.Context, t SampleType, o SampleOrigin) (iter.Seq[string], error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}

	var query string
	var args []any

	if o == SampleOriginAny {
		query = `SELECT message FROM samples WHERE type = ? ORDER BY timestamp DESC`
		args = []any{t}
	} else {
		query = `SELECT message FROM samples WHERE type = ? AND origin = ? ORDER BY timestamp DESC`
		args = []any{t, o}
	}

	s.lock.RLock()
	rows, err := s.db.QueryxContext(ctx, query, args...)
	s.lock.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("failed to query samples: %w", err)
	}

	// create an iterator getting rows from the database
	return func(yield func(string) bool) {
		defer rows.Close()
		for rows.Next() {
			var message string
			if err := rows.Scan(&message); err != nil {
				return // terminate iteration on scan error
			}
			if !yield(message) {
				return // stop iteration if `yield` returns false
			}
		}
	}, nil
}

// Import reads samples from the reader and imports them into the storage.
// Returns statistics about imported samples.
// If withCleanup is true removes all samples with the same type and origin before import.
func (s *Samples) Import(ctx context.Context, t SampleType, o SampleOrigin, r io.Reader, withCleanup bool) (*SamplesStats, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}
	if o == SampleOriginAny {
		return nil, fmt.Errorf("can't import samples with origin 'any'")
	}
	if r == nil {
		return nil, fmt.Errorf("reader cannot be nil")
	}

	s.lock.Lock()

	// start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		s.lock.Unlock()
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// remove all samples with the same type and origin if requested
	if withCleanup {
		query := `DELETE FROM samples WHERE type = ? AND origin = ?`
		if _, err = tx.ExecContext(ctx, query, t, o); err != nil {
			s.lock.Unlock()
			return nil, fmt.Errorf("failed to remove old samples: %w", err)
		}
	}

	// add samples
	query := `INSERT OR REPLACE INTO samples (type, origin, message) VALUES (?, ?, ?)`
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		message := scanner.Text()
		if message == "" { // skip empty lines
			continue
		}
		if _, err = tx.ExecContext(ctx, query, t, o, message); err != nil {
			s.lock.Unlock()
			return nil, fmt.Errorf("failed to add sample: %w", err)
		}
	}

	// check for scanner errors after the scan is complete
	if err = scanner.Err(); err != nil {
		s.lock.Unlock()
		return nil, fmt.Errorf("error reading input: %w", err)
	}

	if err = tx.Commit(); err != nil {
		s.lock.Unlock()
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.lock.Unlock() // release the lock before getting stats

	return s.Stats(ctx)
}

// String implements Stringer interface
func (t SampleType) String() string { return string(t) }

// Validate checks if the sample type is valid
func (t SampleType) Validate() error {
	switch t {
	case SampleTypeHam, SampleTypeSpam:
		return nil
	}
	return fmt.Errorf("invalid sample type: %s", t)
}

// String implements Stringer interface
func (o SampleOrigin) String() string { return string(o) }

// Validate checks if the sample origin is valid
func (o SampleOrigin) Validate() error {
	switch o {
	case SampleOriginPreset, SampleOriginUser, SampleOriginAny:
		return nil
	}
	return fmt.Errorf("invalid sample origin: %s", o)
}

// SamplesStats returns statistics about samples
type SamplesStats struct {
	TotalSpam  int `db:"spam_count"`
	TotalHam   int `db:"ham_count"`
	PresetSpam int `db:"preset_spam_count"`
	PresetHam  int `db:"preset_ham_count"`
	UserSpam   int `db:"user_spam_count"`
	UserHam    int `db:"user_ham_count"`
}

// Stats returns statistics about samples
func (s *Samples) Stats(ctx context.Context) (*SamplesStats, error) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	query := `
        SELECT 
            COUNT(CASE WHEN type = 'spam' THEN 1 END) as spam_count,
            COUNT(CASE WHEN type = 'ham' THEN 1 END) as ham_count,
            COUNT(CASE WHEN type = 'spam' AND origin = 'preset' THEN 1 END) as preset_spam_count,
            COUNT(CASE WHEN type = 'ham' AND origin = 'preset' THEN 1 END) as preset_ham_count,
            COUNT(CASE WHEN type = 'spam' AND origin = 'user' THEN 1 END) as user_spam_count,
            COUNT(CASE WHEN type = 'ham' AND origin = 'user' THEN 1 END) as user_ham_count
        FROM samples`

	var stats SamplesStats
	if err := s.db.GetContext(ctx, &stats, query); err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}
	return &stats, nil
}
