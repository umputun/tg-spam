package storage

import (
	"context"
	"fmt"
	"iter"
	"sync"

	"github.com/jmoiron/sqlx"
)

// Dictionary is a storage for stop words/phrases and ignored words
type Dictionary struct {
	db   *sqlx.DB
	lock *sync.RWMutex
}

// DictionaryType represents the type of dictionary entry
type DictionaryType string

// enum for dictionary types
const (
	DictionaryTypeStopPhrase  DictionaryType = "stop_phrase"
	DictionaryTypeIgnoredWord DictionaryType = "ignored_word"
)

// NewDictionary creates a new Dictionary storage
func NewDictionary(db *sqlx.DB) (*Dictionary, error) {
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
        CREATE TABLE IF NOT EXISTS dictionary (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
            type TEXT CHECK (type IN ('stop_phrase', 'ignored_word')),
            data TEXT NOT NULL UNIQUE
        );
        CREATE INDEX IF NOT EXISTS idx_dictionary_timestamp ON dictionary(timestamp);
        CREATE INDEX IF NOT EXISTS idx_dictionary_type ON dictionary(type);
        CREATE INDEX IF NOT EXISTS idx_dictionary_phrase ON dictionary(data);
    `

	if _, err = tx.Exec(schema); err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &Dictionary{db: db, lock: &sync.RWMutex{}}, nil
}

// Add adds a stop phrase or ignored word to the dictionary
func (d *Dictionary) Add(ctx context.Context, t DictionaryType, data string) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if data == "" {
		return fmt.Errorf("data cannot be empty")
	}

	d.lock.Lock()
	defer d.lock.Unlock()

	// start transaction
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// try to insert, if it fails due to UNIQUE constraint - that's ok
	query := `INSERT OR IGNORE INTO dictionary (type, data) VALUES (?, ?)`
	if _, err = tx.ExecContext(ctx, query, t, data); err != nil {
		return fmt.Errorf("failed to add data: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// Delete removes an entry from the dictionary by its ID
func (d *Dictionary) Delete(ctx context.Context, id int64) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	result, err := d.db.ExecContext(ctx, `DELETE FROM dictionary WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("failed to remove phrase: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("phrase with id %d not found", id)
	}
	return nil
}

// Read reads all entries from the dictionary by type
func (d *Dictionary) Read(ctx context.Context, t DictionaryType) ([]string, error) {
	d.lock.RLock()
	defer d.lock.RUnlock()

	if err := t.Validate(); err != nil {
		return nil, err
	}

	var data []string
	query := `SELECT data FROM dictionary WHERE type = ? ORDER BY timestamp`
	if err := d.db.SelectContext(ctx, &data, query, t); err != nil {
		return nil, fmt.Errorf("failed to get data: %w", err)
	}
	return data, nil
}

// Iterator returns an iterator for phrases by type
func (d *Dictionary) Iterator(ctx context.Context, t DictionaryType) (iter.Seq[string], error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}

	query := `SELECT data FROM dictionary WHERE type = ? ORDER BY timestamp`

	d.lock.RLock()
	rows, err := d.db.QueryxContext(ctx, query, t)
	d.lock.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("failed to query phrases: %w", err)
	}

	return func(yield func(string) bool) {
		defer rows.Close()
		for rows.Next() {
			var phrase string
			if err := rows.Scan(&phrase); err != nil {
				return // terminate iteration on scan error
			}
			if !yield(phrase) {
				return // stop iteration if `yield` returns false
			}
		}
	}, nil
}

// String implements Stringer interface
func (t DictionaryType) String() string { return string(t) }

// Validate checks if the dictionary type is valid
func (t DictionaryType) Validate() error {
	switch t {
	case DictionaryTypeStopPhrase, DictionaryTypeIgnoredWord:
		return nil
	}
	return fmt.Errorf("invalid dictionary type: %s", t)
}

// DictionaryStats returns statistics about dictionary entries
type DictionaryStats struct {
	TotalStopPhrases  int `db:"stop_phrases_count"`
	TotalIgnoredWords int `db:"ignored_words_count"`
}

// Stats returns statistics about dictionary entries
func (d *Dictionary) Stats(ctx context.Context) (*DictionaryStats, error) {
	d.lock.RLock()
	defer d.lock.RUnlock()

	query := `
        SELECT 
            COUNT(CASE WHEN type = 'stop_phrase' THEN 1 END) as stop_phrases_count,
            COUNT(CASE WHEN type = 'ignored_word' THEN 1 END) as ignored_words_count
        FROM dictionary`

	var stats DictionaryStats
	if err := d.db.GetContext(ctx, &stats, query); err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}
	return &stats, nil
}
