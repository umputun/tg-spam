package storage

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"iter"
	"log"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite/lib" // sqlite driver

	"github.com/umputun/tg-spam/app/storage/engine"
)

// Samples is a storage for samples. It supports both ham and spam, as well as preset samples and user's samples
type Samples struct {
	*engine.SQL
	engine.RWLocker
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

// samples-related command constants
const (
	CmdCreateSamplesTable engine.DBCmd = iota + 500
	CmdCreateSamplesIndexes
	CmdAddSample
	CmdImportSample
)

// queries holds all samples-related queries
var samplesQueries = engine.NewQueryMap().
	Add(CmdCreateSamplesTable, engine.Query{
		Sqlite: `CREATE TABLE IF NOT EXISTS samples (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            gid TEXT NOT NULL DEFAULT '',
            timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
            type TEXT CHECK (type IN ('ham', 'spam')),
            origin TEXT CHECK (origin IN ('preset', 'user')),
            message TEXT NOT NULL,
            UNIQUE(gid, message)
        )`,
		Postgres: `CREATE TABLE IF NOT EXISTS samples (
            id SERIAL PRIMARY KEY,
            gid TEXT NOT NULL DEFAULT '',
            timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
            type TEXT CHECK (type IN ('ham', 'spam')),
            origin TEXT CHECK (origin IN ('preset', 'user')),
            message TEXT NOT NULL,
            message_hash TEXT GENERATED ALWAYS AS (encode(sha256(message::bytea), 'hex')) STORED,
            UNIQUE(gid, message_hash)
        )`,
	}).
	Add(CmdAddSample, engine.Query{
		Sqlite: `INSERT OR REPLACE INTO samples (gid, type, origin, message) VALUES (?, ?, ?, ?)`,
		Postgres: `INSERT INTO samples (gid, type, origin, message) VALUES ($1, $2, $3, $4) 
                  ON CONFLICT (gid, message_hash) DO UPDATE SET type = EXCLUDED.type, origin = EXCLUDED.origin`,
	}).
	Add(CmdImportSample, engine.Query{
		Sqlite: `INSERT OR REPLACE INTO samples (gid, type, origin, message) VALUES (?, ?, ?, ?)`,
		Postgres: `INSERT INTO samples (gid, type, origin, message) 
                  VALUES ($1, $2, $3, $4) 
                  ON CONFLICT (gid, message_hash) DO UPDATE 
                  SET type = EXCLUDED.type, origin = EXCLUDED.origin`,
	}).
	Add(CmdCreateSamplesIndexes, engine.Query{
		Sqlite: `
			CREATE INDEX IF NOT EXISTS idx_samples_gid ON samples(gid);
			CREATE INDEX IF NOT EXISTS idx_samples_timestamp ON samples(timestamp);
			CREATE INDEX IF NOT EXISTS idx_samples_type ON samples(type);
			CREATE INDEX IF NOT EXISTS idx_samples_origin ON samples(origin);
			CREATE INDEX IF NOT EXISTS idx_samples_lookup ON samples(gid, type, origin);
			CREATE INDEX IF NOT EXISTS idx_samples_message ON samples(message)`,
		Postgres: `
			CREATE INDEX IF NOT EXISTS idx_samples_lookup ON samples(gid, type, origin);
            CREATE INDEX IF NOT EXISTS idx_samples_gid_type_origin_ts ON samples(gid, type, origin, timestamp DESC);
			CREATE INDEX IF NOT EXISTS idx_samples_gid ON samples(gid);
			CREATE INDEX IF NOT EXISTS idx_samples_origin ON samples(gid,origin);
			CREATE INDEX IF NOT EXISTS idx_samples_message_hash ON samples(message_hash);`,
	}).
	Add(CmdAddGIDColumn, engine.Query{
		Sqlite:   "ALTER TABLE samples ADD COLUMN gid TEXT DEFAULT ''",
		Postgres: "ALTER TABLE samples ADD COLUMN IF NOT EXISTS gid TEXT DEFAULT ''",
	})

// NewSamples creates a new Samples storage
func NewSamples(ctx context.Context, db *engine.SQL) (*Samples, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}
	res := &Samples{SQL: db, RWLocker: db.MakeLock()}
	cfg := engine.TableConfig{
		Name:          "samples",
		CreateTable:   CmdCreateSamplesTable,
		CreateIndexes: CmdCreateSamplesIndexes,
		MigrateFunc:   res.migrate,
		QueriesMap:    samplesQueries,
	}
	if err := engine.InitTable(ctx, db, cfg); err != nil {
		return nil, fmt.Errorf("failed to init samples storage: %w", err)
	}
	return res, nil
}

// Add adds a sample to the storage. Checks if the sample is already present and skips it if it is.
func (s *Samples) Add(ctx context.Context, t SampleType, o SampleOrigin, message string) error {
	dbgMsg := message
	if len(dbgMsg) > 1024 {
		dbgMsg = dbgMsg[:1024] + "..."
	}
	log.Printf("[DEBUG] adding sample: %s, %s, %q", t, o, dbgMsg)
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

	s.Lock()
	defer s.Unlock()

	query, err := samplesQueries.Pick(s.Type(), CmdAddSample)
	if err != nil {
		return fmt.Errorf("failed to get query: %w", err)
	}
	if _, err := s.ExecContext(ctx, query, s.GID(), t, o, message); err != nil {
		return fmt.Errorf("failed to add sample: %w", err)
	}

	return nil
}

// Delete removes a sample from the storage by its ID
func (s *Samples) Delete(ctx context.Context, id int64) error {
	log.Printf("[DEBUG] deleting sample: %d", id)
	s.Lock()
	defer s.Unlock()

	result, err := s.ExecContext(ctx, s.Adopt(`DELETE FROM samples WHERE id = ?`), id)
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

// DeleteMessage removes a sample from the storage by its message
func (s *Samples) DeleteMessage(ctx context.Context, message string) error {
	log.Printf("[DEBUG] deleting sample: %q", message)
	s.Lock()
	defer s.Unlock()

	// first verify the message exists in this group
	var count int
	gid := s.GID()
	query := s.Adopt(`SELECT COUNT(*) FROM samples WHERE gid = ? AND message = ?`)
	if err := s.GetContext(ctx, &count, query, gid, message); err != nil {
		return fmt.Errorf("failed to check sample existence: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("sample not found: gid=%s, message=%s", gid, message)
	}

	result, err := s.ExecContext(ctx, s.Adopt(`DELETE FROM samples WHERE gid = ? AND message = ?`), gid, message)
	if err != nil {
		return fmt.Errorf("failed to remove sample: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get affected rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("failed to delete sample: gid=%s, message=%s not found", gid, message)
	}
	return nil
}

// Read reads samples from storage by type and origin
func (s *Samples) Read(ctx context.Context, t SampleType, o SampleOrigin) ([]string, error) {
	s.RLock()
	defer s.RUnlock()

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
	gid := s.GID()
	if o == SampleOriginAny {
		query = `SELECT message FROM samples WHERE gid = ? AND type = ?`
		args = []any{gid, t}
	} else {
		query = `SELECT message FROM samples WHERE gid = ? AND type = ? AND origin = ?`
		args = []any{gid, t, o}
	}
	query = s.Adopt(query)

	if err := s.SelectContext(ctx, &samples, query, args...); err != nil {
		return nil, fmt.Errorf("failed to get samples: %w", err)
	}
	log.Printf("[DEBUG] read %d samples: gid=%s, type=%s, origin=%s", len(samples), gid, t, o)
	return samples, nil
}

// Reader returns a reader for samples by type and origin
// Sorts samples by timestamp in descending order, i.e. from the newest to the oldest
func (s *Samples) Reader(ctx context.Context, t SampleType, o SampleOrigin) (io.ReadCloser, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}

	var query string
	var args []any
	gid := s.GID()

	if o == SampleOriginAny {
		query = `SELECT message FROM samples WHERE gid = ? AND  type = ? ORDER BY timestamp DESC`
		args = []any{gid, t}
	} else {
		query = `SELECT message FROM samples WHERE gid = ? AND type = ? AND origin = ? ORDER BY timestamp DESC`
		args = []any{gid, t, o}
	}
	query = s.Adopt(query)

	s.RLock()
	defer s.RUnlock()
	rows, err := s.QueryxContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query samples: %w", err)
	}

	return &sampleReader{rows: rows}, nil
}

// Iterator returns an iterator for samples by type and origin.
// Sorts samples by timestamp in descending order, i.e. from the newest to the oldest.
// The iterator respects context cancellation.
func (s *Samples) Iterator(ctx context.Context, t SampleType, o SampleOrigin) (iter.Seq[string], error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if err := o.Validate(); err != nil {
		return nil, err
	}

	var query string
	var args []any
	gid := s.GID()

	if o == SampleOriginAny {
		query = `SELECT message FROM samples WHERE gid = ? AND type = ? ORDER BY timestamp DESC`
		args = []any{gid, t}
	} else {
		query = `SELECT message FROM samples WHERE gid = ? AND type = ? AND origin = ? ORDER BY timestamp DESC`
		args = []any{gid, t, o}
	}
	query = s.Adopt(query)

	s.RLock()
	rows, err := s.QueryxContext(ctx, query, args...)
	s.RUnlock()
	if err != nil {
		return nil, fmt.Errorf("failed to query samples: %w", err)
	}

	return func(yield func(string) bool) {
		defer rows.Close()
		for rows.Next() {
			// check context before each row
			select {
			case <-ctx.Done():
				return
			default:
			}

			var message string
			if err := rows.Scan(&message); err != nil {
				log.Printf("[ERROR] scan failed: %v", err)
				return
			}

			// check context after scan but before yield
			select {
			case <-ctx.Done():
				return
			default:
			}

			if !yield(message) {
				return
			}
		}
		if err := rows.Err(); err != nil {
			log.Printf("[ERROR] rows iteration failed: %v", err)
			return
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
	gid := s.GID()

	s.Lock()
	defer s.Unlock()

	// start transaction
	tx, err := s.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// remove all samples with the same type and origin if requested
	if withCleanup {
		query := s.Adopt(`DELETE FROM samples WHERE gid = ? AND type = ? AND origin = ?`)
		result, errDel := tx.ExecContext(ctx, query, gid, t, o)
		if errDel != nil {
			return nil, fmt.Errorf("failed to remove old samples: %w", errDel)
		}
		affected, errCount := result.RowsAffected()
		if errCount != nil {
			return nil, fmt.Errorf("failed to get affected rows: %w", errCount)
		}
		log.Printf("[DEBUG] removed %d old samples: gid=%s, type=%s, origin=%s", affected, gid, t, o)
	}

	// add samples
	query, err := samplesQueries.Pick(s.Type(), CmdImportSample)
	if err != nil {
		return nil, fmt.Errorf("failed to get import query: %w", err)
	}
	scanner := bufio.NewScanner(r)
	// set custom buffer size and max token size for large lines
	const maxScanTokenSize = 64 * 1024 // 64KB max line length
	buf := make([]byte, maxScanTokenSize)
	scanner.Buffer(buf, maxScanTokenSize)

	added := 0
	for scanner.Scan() {
		message := scanner.Text()
		if message == "" { // skip empty lines
			continue
		}
		if _, err = tx.ExecContext(ctx, query, gid, t, o, message); err != nil {
			return nil, fmt.Errorf("failed to add sample: %w", err)
		}
		added++
	}

	// check for scanner errors after the scan is complete
	if err = scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading input: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}
	log.Printf("[DEBUG] imported %d samples: gid=%s, type=%s, origin=%s", added, gid, t, o)
	return s.stats(ctx)
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

// String provides a string representation of the statistics
func (st *SamplesStats) String() string {
	return fmt.Sprintf("spam: %d, ham: %d, preset spam: %d, preset ham: %d, user spam: %d, user ham: %d",
		st.TotalSpam, st.TotalHam, st.PresetSpam, st.PresetHam, st.UserSpam, st.UserHam)
}

// Stats returns statistics about samples
func (s *Samples) Stats(ctx context.Context) (*SamplesStats, error) {
	s.RLock()
	defer s.RUnlock()
	return s.stats(ctx)
}

// stats returns statistics about samples without locking
func (s *Samples) stats(ctx context.Context) (*SamplesStats, error) {
	query := s.Adopt(`
        SELECT 
            COUNT(CASE WHEN type = 'spam' THEN 1 END) as spam_count,
            COUNT(CASE WHEN type = 'ham' THEN 1 END) as ham_count,
            COUNT(CASE WHEN type = 'spam' AND origin = 'preset' THEN 1 END) as preset_spam_count,
            COUNT(CASE WHEN type = 'ham' AND origin = 'preset' THEN 1 END) as preset_ham_count,
            COUNT(CASE WHEN type = 'spam' AND origin = 'user' THEN 1 END) as user_spam_count,
            COUNT(CASE WHEN type = 'ham' AND origin = 'user' THEN 1 END) as user_ham_count
        FROM samples 
        WHERE gid = ?`)

	var stats SamplesStats
	if err := s.GetContext(ctx, &stats, query, s.GID()); err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}
	return &stats, nil
}

func (s *Samples) migrate(_ context.Context, _ *sqlx.Tx, _ string) error {
	// no migration needed for now
	return nil
}

// sampleReader implements io.Reader for database rows and handles partial reads with buffering.
type sampleReader struct {
	rows    *sqlx.Rows // database rows iterator
	buffer  []byte     // partial read buffer for cases when p is smaller than message size
	current string     // current message from the database
	closed  bool       // indicates if the reader has been closed
}

// Read implements io.Reader interface. It reads messages from database rows one by one and
// handles partial reads by maintaining an internal buffer. If the provided buffer p is smaller
// than the message size, it will take multiple Read calls to get the complete message.
// Each message is followed by a newline for proper scanning.
func (r *sampleReader) Read(p []byte) (n int, err error) {
	if r.closed {
		return 0, io.ErrClosedPipe
	}

	// if buffer is empty, try to get next message from database
	if len(r.buffer) == 0 {
		if r.rows == nil || !r.rows.Next() {
			if r.rows != nil && r.rows.Err() != nil {
				return 0, fmt.Errorf("rows iteration failed: %w", r.rows.Err())
			}
			return 0, io.EOF
		}

		if err := r.rows.Scan(&r.current); err != nil {
			return 0, fmt.Errorf("scan failed: %w", err)
		}
		// append newline to message for proper scanning
		r.buffer = []byte(r.current + "\n")
	}

	// copy as much as we can to the provided buffer
	n = copy(p, r.buffer)
	// keep the rest for the next read
	r.buffer = r.buffer[n:]
	return n, nil
}

// Close implements io.Closer interface. Can be called multiple times safely.
func (r *sampleReader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.rows != nil {
		if err := r.rows.Close(); err != nil {
			return fmt.Errorf("failed to close rows: %w", err)
		}
		r.rows = nil // prevent double-close
	}
	return nil
}
