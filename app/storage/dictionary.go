package storage

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"iter"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/umputun/tg-spam/app/storage/engine"
)

// dictionary-related command constants
const (
	CmdCreateDictionaryTable engine.DBCmd = iota + 300
	CmdCreateDictionaryIndexes
	CmdAddDictionaryEntry
	CmdImportDictionaryEntry
)

// all dictionary-related queries
var dictionaryQueries = engine.NewQueryMap().
	Add(CmdCreateDictionaryTable, engine.Query{
		Sqlite: `CREATE TABLE IF NOT EXISTS dictionary (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            gid TEXT DEFAULT '',
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
            type TEXT CHECK (type IN ('stop_phrase', 'ignored_word')),
            data TEXT NOT NULL,
            UNIQUE(gid, data)
        )`,
		Postgres: `CREATE TABLE IF NOT EXISTS dictionary (
            id SERIAL PRIMARY KEY,
            gid TEXT DEFAULT '',
            timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            type TEXT CHECK (type IN ('stop_phrase', 'ignored_word')),
            data TEXT NOT NULL,
            UNIQUE(gid, data)
        )`,
	}).
	Add(CmdCreateDictionaryIndexes, engine.Query{
		Sqlite: `
			CREATE INDEX IF NOT EXISTS idx_dictionary_timestamp ON dictionary(timestamp);
			CREATE INDEX IF NOT EXISTS idx_dictionary_type ON dictionary(type);
			CREATE INDEX IF NOT EXISTS idx_dictionary_phrase ON dictionary(data);
			CREATE INDEX IF NOT EXISTS idx_dictionary_gid ON dictionary(gid)`,
		Postgres: `
			CREATE INDEX IF NOT EXISTS idx_dictionary_gid_type_ts ON dictionary(gid, type, timestamp);
			CREATE INDEX IF NOT EXISTS idx_dictionary_gid_data ON dictionary(gid, data)`,
	}).
	Add(CmdAddGIDColumn, engine.Query{
		Sqlite:   "ALTER TABLE dictionary ADD COLUMN gid TEXT DEFAULT ''",
		Postgres: "ALTER TABLE dictionary ADD COLUMN IF NOT EXISTS gid TEXT DEFAULT ''",
	}).
	Add(CmdAddDictionaryEntry, engine.Query{
		Sqlite:   `INSERT OR IGNORE INTO dictionary (type, data, gid) VALUES (?, ?, ?)`,
		Postgres: `INSERT INTO dictionary (type, data, gid) VALUES ($1, $2, $3) ON CONFLICT (gid, data) DO NOTHING`,
	}).
	Add(CmdImportDictionaryEntry, engine.Query{
		Sqlite:   `INSERT OR REPLACE INTO dictionary (type, data, gid) VALUES (?, ?, ?)`,
		Postgres: `INSERT INTO dictionary (type, data, gid) VALUES ($1, $2, $3) ON CONFLICT (gid, data) DO UPDATE SET type = EXCLUDED.type`,
	})

// Dictionary is a storage for stop words/phrases and ignored words
type Dictionary struct {
	*engine.SQL
	engine.RWLocker
}

// DictionaryType represents the type of dictionary entry
type DictionaryType string

// enum for dictionary types
const (
	DictionaryTypeStopPhrase  DictionaryType = "stop_phrase"
	DictionaryTypeIgnoredWord DictionaryType = "ignored_word"
)

// NewDictionary creates a new Dictionary storage
func NewDictionary(ctx context.Context, db *engine.SQL) (*Dictionary, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}
	res := &Dictionary{SQL: db, RWLocker: db.MakeLock()}
	cfg := engine.TableConfig{
		Name:          "dictionary",
		CreateTable:   CmdCreateDictionaryTable,
		CreateIndexes: CmdCreateDictionaryIndexes,
		MigrateFunc:   res.migrate,
		QueriesMap:    dictionaryQueries,
	}
	if err := engine.InitTable(ctx, db, cfg); err != nil {
		return nil, fmt.Errorf("failed to init dictionary storage: %w", err)
	}
	return res, nil
}

// Add adds a stop phrase or ignored word to the dictionary
func (d *Dictionary) Add(ctx context.Context, t DictionaryType, data string) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if data == "" {
		return fmt.Errorf("data cannot be empty")
	}

	d.Lock()
	defer d.Unlock()

	// get the appropriate query for current db type
	query, err := dictionaryQueries.Pick(d.Type(), CmdAddDictionaryEntry)
	if err != nil {
		return fmt.Errorf("failed to get insert query: %w", err)
	}

	if _, err = d.ExecContext(ctx, query, t, data, d.GID()); err != nil {
		return fmt.Errorf("failed to add data: %w", err)
	}

	return nil
}

// Delete removes an entry from the dictionary by its ID
func (d *Dictionary) Delete(ctx context.Context, id int64) error {
	d.Lock()
	defer d.Unlock()

	result, err := d.ExecContext(ctx, d.Adopt(`DELETE FROM dictionary WHERE id = ?`), id)
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
	d.RLock()
	defer d.RUnlock()

	if err := t.Validate(); err != nil {
		return nil, err
	}

	var data []string
	query := d.Adopt(`SELECT data FROM dictionary WHERE type = ? AND gid = ? ORDER BY timestamp`)
	if err := d.SelectContext(ctx, &data, query, t, d.GID()); err != nil {
		return nil, fmt.Errorf("failed to get data: %w", err)
	}
	return data, nil
}

// Reader returns a reader for phrases by type
// lock is lacking deliberately because Read is already protected
func (d *Dictionary) Reader(ctx context.Context, t DictionaryType) (io.ReadCloser, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	recs, err := d.Read(ctx, t)
	if err != nil {
		return nil, fmt.Errorf("failed to read phrases: %w", err)
	}
	data := strings.Join(recs, "\n")
	return io.NopCloser(strings.NewReader(data)), nil
}

// Iterator returns an iterator for phrases by type
func (d *Dictionary) Iterator(ctx context.Context, t DictionaryType) (iter.Seq[string], error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}

	query := d.Adopt(`SELECT data FROM dictionary WHERE type = ? AND gid = ? ORDER BY timestamp`)

	d.RLock()
	rows, err := d.QueryxContext(ctx, query, t, d.GID())
	d.RUnlock()
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

// Import reads phrases from the reader and imports them into the storage.
// If withCleanup is true removes all entries with the same type before import.
// Input format is either a single phrase per line or a CSV file with multiple phrases.
// Import reads phrases from the reader and imports them into the storage.
func (d *Dictionary) Import(ctx context.Context, t DictionaryType, r io.Reader, withCleanup bool) (*DictionaryStats, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("reader cannot be nil")
	}

	d.Lock()

	// start transaction
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		d.Unlock()
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()
	gid := d.GID()

	// remove all entries with the same type if requested
	if withCleanup {
		if _, err = tx.ExecContext(ctx, d.Adopt(`DELETE FROM dictionary WHERE type = ? AND gid = ?`), t, gid); err != nil {
			d.Unlock()
			return nil, fmt.Errorf("failed to remove old entries: %w", err)
		}
	}

	// get the appropriate insert query for current db type
	query, err := dictionaryQueries.Pick(d.Type(), CmdImportDictionaryEntry)
	if err != nil {
		d.Unlock()
		return nil, fmt.Errorf("failed to get insert query: %w", err)
	}

	// prepare statement for inserts
	insertStmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		d.Unlock()
		return nil, fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer insertStmt.Close()

	// use csv reader to handle quoted strings and comma separation properly
	csvReader := csv.NewReader(r)
	csvReader.FieldsPerRecord = -1 // allow variable number of fields
	csvReader.TrimLeadingSpace = true

	for {
		record, csvErr := csvReader.Read()
		if csvErr == io.EOF {
			break
		}
		if csvErr != nil {
			d.Unlock()
			return nil, fmt.Errorf("error reading input: %w", csvErr)
		}

		// process each field in the record
		for _, field := range record {
			if field == "" { // skip empty entries
				continue
			}

			if _, err = insertStmt.ExecContext(ctx, t, field, gid); err != nil {
				d.Unlock()
				return nil, fmt.Errorf("failed to add entry: %w", err)
			}
		}
	}

	if err = tx.Commit(); err != nil {
		d.Unlock()
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	d.Unlock() // release the lock before getting stats

	return d.Stats(ctx)
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

// String returns a string representation of the stats
func (d *DictionaryStats) String() string {
	return fmt.Sprintf("stop phrases: %d, ignored words: %d", d.TotalStopPhrases, d.TotalIgnoredWords)
}

// Stats returns statistics about dictionary entries for the given GID
func (d *Dictionary) Stats(ctx context.Context) (*DictionaryStats, error) {
	d.RLock()
	defer d.RUnlock()

	query := d.Adopt(`
        SELECT 
            COUNT(CASE WHEN type = ? THEN 1 END) as stop_phrases_count,
            COUNT(CASE WHEN type = ? THEN 1 END) as ignored_words_count
        FROM dictionary
        WHERE gid = ?`,
	)

	var stats DictionaryStats
	if err := d.GetContext(ctx, &stats, query, DictionaryTypeStopPhrase, DictionaryTypeIgnoredWord, d.GID()); err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}
	return &stats, nil
}

func (d *Dictionary) migrate(_ context.Context, _ *sqlx.Tx, _ string) error {
	// no migrations yet
	return nil
}
