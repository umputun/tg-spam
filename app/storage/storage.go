// Package storage provides a storage engine for sql databases.
// The storage engine is a wrapper around sqlx.DB with additional functionality to work with the various types of database engines.
// Each table is represented by a struct, and each struct has a method to work the table with business logic for this data type.
package storage

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver loaded here

	"github.com/umputun/tg-spam/app/storage/engine"
)

// initDB initializes db table with a schema and handles migration in a transaction
func initDB(ctx context.Context, db *engine.SQL, tableName, schema string, migrateFn func(context.Context, *sqlx.Tx, string) error) error {
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
