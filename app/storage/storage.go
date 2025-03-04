package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/umputun/tg-spam/app/storage/engine"
)

// New creates a new store based on the connection URL and the session duration.
func New(ctx context.Context, connURL, gid string) (*engine.SQL, error) {
	dbType, conn, err := prepareStoreURL(connURL)
	if err != nil {
		return nil, err
	}

	switch dbType {
	case engine.Sqlite:
		res, err := engine.NewSqlite(conn, gid)
		if err != nil {
			return nil, fmt.Errorf("failed to create sqlite engine: %w", err)
		}
		return res, nil
	case engine.Postgres:
		res, err := engine.NewPostgres(ctx, conn, gid)
		if err != nil {
			return nil, fmt.Errorf("failed to create postgres engine: %w", err)
		}
		return res, nil
	default:
		return nil, fmt.Errorf("unsupported database type in connection string")
	}
}

// prepareStoreURL returns the type of the store based on the connection URL and the connection URL.
// supported connection strings for psq, mysql and sqlite
// returns the type of the store and the connection string to use with sqlx
func prepareStoreURL(connURL string) (dbType engine.Type, conn string, err error) {
	if strings.HasPrefix(connURL, "postgres://") {
		return engine.Postgres, connURL, nil
	}
	if strings.Contains(connURL, "@tcp(") {
		// check if parseTime=true is set in the connection string and add it if not
		if !strings.Contains(connURL, "parseTime=true") {
			if strings.Contains(connURL, "?") {
				connURL += "&parseTime=true"
			} else {
				connURL += "?parseTime=true"
			}
		}
		return engine.Mysql, connURL, nil
	}
	if strings.HasPrefix(connURL, "sqlite:/") || strings.HasPrefix(connURL, "sqlite3:/") ||
		strings.HasSuffix(connURL, ".sqlite") || strings.HasSuffix(connURL, ".db") {
		connURL = strings.Replace(connURL, "sqlite:/", "file:/", 1)
		connURL = strings.Replace(connURL, "sqlite3:/", "file:/", 1)
		return engine.Sqlite, connURL, nil
	}

	if connURL == "memory" || strings.Contains(connURL, ":memory:") ||
		strings.HasPrefix(connURL, "memory://") || strings.HasPrefix(connURL, "mem://") {
		return engine.Sqlite, ":memory:", nil
	}

	return engine.Unknown, "", fmt.Errorf("unsupported database type in connection string")
}
