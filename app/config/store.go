package config

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/umputun/tg-spam/app/storage/engine"
)

// Store provides access to settings stored in database
type Store struct {
	*engine.SQL
	engine.RWLocker
	crypter         *Crypter
	sensitiveFields []string
	defaults        *Settings
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
func NewStore(ctx context.Context, db *engine.SQL, opts ...StoreOption) (*Store, error) {
	if db == nil {
		return nil, errors.New("no db provided")
	}

	// create store with default options
	res := &Store{
		SQL:             db,
		RWLocker:        db.MakeLock(),
		sensitiveFields: defaultSensitiveFields(),
	}

	// apply options
	for _, opt := range opts {
		opt(res)
	}

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

// StoreOption defines functional options for Store
type StoreOption func(*Store)

// WithCrypter adds a crypter to the store for field encryption
func WithCrypter(crypter *Crypter) StoreOption {
	return func(s *Store) {
		s.crypter = crypter
	}
}

// WithSensitiveFields sets the list of sensitive fields to encrypt/decrypt
func WithSensitiveFields(fields []string) StoreOption {
	return func(s *Store) {
		s.sensitiveFields = fields
	}
}

// WithDefaults makes Load start from tmpl instead of empty settings, so a key missing from the
// stored blob takes its default while a key stored as zero stays zero. InstanceID is not seeded:
// an empty instance_id must stay empty so the loader can keep the one given on the command line.
func WithDefaults(tmpl *Settings) StoreOption {
	return func(s *Store) {
		seed := *tmpl
		seed.InstanceID = ""
		s.defaults = &seed
	}
}

// defaultSensitiveFields returns the default list of sensitive fields derived
// from sensitiveFieldAccessors so adding a new field is a single-place change.
// Order is stable across calls to keep error and log output deterministic.
func defaultSensitiveFields() []string {
	fields := make([]string, 0, len(sensitiveFieldAccessors))
	for k := range sensitiveFieldAccessors {
		fields = append(fields, k)
	}
	sort.Strings(fields)
	return fields
}

// Load retrieves the settings from the database
func (s *Store) Load(ctx context.Context) (*Settings, error) {
	s.RLock()
	defer s.RUnlock()
	return s.load(ctx)
}

// load reads and decrypts the stored settings; the caller holds the lock
func (s *Store) load(ctx context.Context) (*Settings, error) {
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
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("no settings found in database: %w", err)
		}
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}

	// defaults go through a JSON round trip, not a struct copy: decoding the blob into a copy
	// would append into the template's slice backing arrays and corrupt it for the next load
	result := New()
	if s.defaults != nil {
		seed, err := json.Marshal(s.defaults)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal defaults: %w", err)
		}
		if err := json.Unmarshal(seed, result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal defaults: %w", err)
		}
	}
	if err := json.Unmarshal([]byte(record.Data), result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal settings: %w", err)
	}

	// decrypt sensitive fields if crypter is configured
	if s.crypter != nil {
		if err := s.crypter.DecryptSensitiveFields(result, s.sensitiveFields...); err != nil {
			return nil, fmt.Errorf("failed to decrypt sensitive fields: %w", err)
		}
	}

	return result, nil
}

// Save stores the settings to the database
func (s *Store) Save(ctx context.Context, settings *Settings) error {
	if settings == nil {
		return errors.New("nil settings")
	}

	s.Lock()
	defer s.Unlock()

	// create a safe copy without sensitive information
	safeCopy := *settings // make a shallow copy

	// credentials supplied on the command line or through the environment are never
	// persisted from memory: write whatever the database already holds for them
	if cliFields := settings.Transient.CredentialsFromCLI; len(cliFields) > 0 {
		stored, err := s.load(ctx)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("failed to read stored credentials: %w", err)
		}
		if err := CopySensitiveFields(&safeCopy, stored, cliFields...); err != nil {
			return fmt.Errorf("failed to keep command-line credentials out of the database: %w", err)
		}
	}

	// clear transient fields that shouldn't be persisted
	safeCopy.Transient = TransientSettings{}

	// encrypt sensitive fields if crypter is configured
	if s.crypter != nil {
		if encErr := s.crypter.EncryptSensitiveFields(&safeCopy, s.sensitiveFields...); encErr != nil {
			return fmt.Errorf("failed to encrypt sensitive fields: %w", encErr)
		}
	}

	// marshal the settings to JSON after optional encryption
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
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, fmt.Errorf("no settings found in database: %w", err)
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
	return nil
}
