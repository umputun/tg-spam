package engine

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"sync"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"  // postgres driver loaded here
	_ "modernc.org/sqlite" // sqlite driver loaded here
)

// Type is a type of database engine
type Type string

// enum of supported database engines
const (
	Unknown  Type = ""
	Sqlite   Type = "sqlite"
	Postgres Type = "postgres"
	Mysql    Type = "mysql"
)

// SQL is a wrapper for sqlx.DB with type.
// Type allows distinguishing between different database engines.
type SQL struct {
	sqlx.DB
	gid    string // group id, to allow per-group storage in the same database
	dbType Type   // type of the database engine
}

// New creates a new database engine with a connection URL and group id.
// It detects the database engine type based on the connection URL and initializes
// the appropriate driver. Supports sqlite and postgres database types.
func New(ctx context.Context, connURL, gid string) (*SQL, error) {
	log.Printf("[INFO] new database engine, conn: %s, gid: %s", connURL, gid)
	if connURL == "" {
		return &SQL{}, fmt.Errorf("connection URL is empty")
	}

	switch {
	case connURL == ":memory:":
		return NewSqlite(connURL, gid)
	case strings.HasPrefix(connURL, "file://"):
		return NewSqlite(strings.TrimPrefix(connURL, "file://"), gid)
	case strings.HasPrefix(connURL, "file:"):
		return NewSqlite(strings.TrimPrefix(connURL, "file:"), gid)
	case strings.HasPrefix(connURL, "sqlite://"):
		return NewSqlite(strings.TrimPrefix(connURL, "sqlite://"), gid)
	case strings.HasSuffix(connURL, ".sqlite") || strings.HasSuffix(connURL, ".db"):
		return NewSqlite(connURL, gid)
	case strings.HasPrefix(connURL, "postgres://"):
		return NewPostgres(ctx, connURL, gid)
	}

	return &SQL{}, fmt.Errorf("unsupported database type in connection string %q", connURL)
}

// NewSqlite creates a new sqlite database
func NewSqlite(file, gid string) (*SQL, error) {
	db, err := sqlx.Connect("sqlite", file)
	if err != nil {
		return &SQL{}, fmt.Errorf("failed to connect to sqlite: %w", err)
	}
	if err := setSqlitePragma(db); err != nil {
		return &SQL{}, err
	}
	return &SQL{DB: *db, gid: gid, dbType: Sqlite}, nil
}

// NewPostgres creates a new postgres database. If the database doesn't exist, it will be created
func NewPostgres(ctx context.Context, connURL, gid string) (*SQL, error) {
	u, err := url.Parse(connURL)
	if err != nil {
		return nil, fmt.Errorf("invalid postgres connection url: %w", err)
	}

	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		return nil, fmt.Errorf("database name not specified in connection URL")
	}

	// try to connect first - database might exist
	db, err := sqlx.ConnectContext(ctx, "postgres", connURL)
	if err == nil {
		return &SQL{DB: *db, gid: gid, dbType: Postgres}, nil
	}

	// create URL for postgres database
	u.Path = "/postgres"
	baseURL := u.String()

	// connect to default postgres database
	baseDB, err := sqlx.ConnectContext(ctx, "postgres", baseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}
	defer baseDB.Close()

	// create database
	_, err = baseDB.ExecContext(ctx, fmt.Sprintf(`CREATE DATABASE %q`, dbName))
	if err != nil {
		// Ignore error if database already exists
		if !strings.Contains(err.Error(), "already exists") {
			return nil, fmt.Errorf("failed to create database: %w", err)
		}
	}
	log.Printf("[INFO] created database %s", dbName)

	// connect to the new database
	db, err = sqlx.ConnectContext(ctx, "postgres", connURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to created database: %w", err)
	}
	return &SQL{DB: *db, gid: gid, dbType: Postgres}, nil
}

// GID returns the group id
func (e *SQL) GID() string {
	return e.gid
}

// Type returns the database engine type
func (e *SQL) Type() Type {
	return e.dbType
}

// MakeLock creates a new lock for the database engine
func (e *SQL) MakeLock() RWLocker {
	if e.dbType == Sqlite {
		return new(sync.RWMutex) // sqlite need locking
	}
	return &NoopLocker{} // other engines don't need locking
}

// Adopt adopts placeholders in a query for the database engine
// This is for query which compatible between sqlite and postgres except for the placeholders
func (e *SQL) Adopt(q string) string {
	if e.dbType != Postgres { // no need to replace placeholders for sqlite
		return q
	}

	placeholderCount := 1
	result := ""
	inQuotes := false

	for _, r := range q {
		switch r {
		case '\'':
			inQuotes = !inQuotes
			result += string(r)
		case '?':
			if inQuotes {
				result += string(r)
			} else {
				result += fmt.Sprintf("$%d", placeholderCount)
				placeholderCount++
			}
		default:
			result += string(r)
		}
	}

	return result
}

func setSqlitePragma(db *sqlx.DB) error {
	// Set pragmas for SQLite. Commented out pragmas as they are not used in the code yet because we need
	// to make sure if it is worth having 2 more DB-related files for WAL and SHM.
	pragmas := map[string]string{
		"journal_mode": "DELETE", // explicitly set to DELETE mode to prevent WAL files
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

// TableConfig represents configuration for table initialization
type TableConfig struct {
	Name          string
	CreateTable   DBCmd
	CreateIndexes DBCmd
	MigrateFunc   func(ctx context.Context, tx *sqlx.Tx, gid string) error
	QueriesMap    *QueryMap
}

// InitTable initializes database table with schema and handles migration in a transaction
func InitTable(ctx context.Context, db *SQL, cfg TableConfig) error {
	if db == nil {
		return fmt.Errorf("db connection is nil")
	}

	tx, err := db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// create table first
	createSchema, err := cfg.QueriesMap.Pick(db.Type(), cfg.CreateTable)
	if err != nil {
		return fmt.Errorf("failed to get create table query: %w", err)
	}
	if _, err = tx.ExecContext(ctx, createSchema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// try to migrate if needed
	if err = cfg.MigrateFunc(ctx, tx, db.GID()); err != nil {
		return fmt.Errorf("failed to migrate table: %w", err)
	}

	// create indices after migration when all columns exist
	createIndexes, err := cfg.QueriesMap.Pick(db.Type(), cfg.CreateIndexes)
	if err != nil {
		return fmt.Errorf("failed to get create indexes query: %w", err)
	}
	if _, err = tx.ExecContext(ctx, createIndexes); err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}
