package engine

import (
	"sync"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver loaded here
)

// Type is a type of database engine
type Type string

// enum of supported database engines
const (
	Unknown  Type = ""
	Sqlite   Type = "sqlite"
	Postgres Type = "postgres"
)

// SQL is a wrapper for sqlx.DB with type.
// Type allows distinguishing between different database engines.
type SQL struct {
	sqlx.DB
	gid    string // group id, to allow per-group storage in the same database
	dbType Type   // type of the database engine
}

// NewSqlite creates a new sqlite database
func NewSqlite(file, gid string) (*SQL, error) {
	db, err := sqlx.Connect("sqlite", file)
	if err != nil {
		return &SQL{}, err
	}
	if err := setSqlitePragma(db); err != nil {
		return &SQL{}, err
	}
	return &SQL{DB: *db, gid: gid, dbType: Sqlite}, nil
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
