package storage

import "github.com/jmoiron/sqlx"

// NewSqliteDB creates a new sqlite database
func NewSqliteDB(file string) (*sqlx.DB, error) {
	return sqlx.Connect("sqlite", file)
}
