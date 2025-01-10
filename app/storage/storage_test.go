package storage

import (
	"os"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/storage/engine"
)

func setupTestDB(t *testing.T) (res *engine.SQL, teardown func()) {
	t.Helper()
	tmpFile := os.TempDir() + "/test.db"
	t.Log("db file:", tmpFile)
	db, err := sqlx.Connect("sqlite", tmpFile)
	require.NoError(t, err)
	require.NotNil(t, db)
	engine, err := engine.NewSqlite(tmpFile, "gr1")
	require.NoError(t, err)

	return engine, func() {
		db.Close()
		os.Remove(tmpFile)
	}
}
