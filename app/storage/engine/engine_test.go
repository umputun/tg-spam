package engine

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestNew(t *testing.T) {
	temp := t.TempDir()

	tests := []struct {
		name    string
		url     string
		gid     string
		want    Type
		wantErr string
	}{
		{
			name: "in-memory sqlite",
			url:  ":memory:",
			gid:  "gr1",
			want: Sqlite,
		},
		{
			name: "file:// prefix",
			url:  "file://" + filepath.Join(temp, "file1.db"),
			gid:  "gr2",
			want: Sqlite,
		},
		{
			name: "file: prefix",
			url:  "file:" + filepath.Join(temp, "file2.db"),
			gid:  "gr2",
			want: Sqlite,
		},
		{
			name: "sqlite:// prefix",
			url:  "sqlite://" + filepath.Join(temp, "file3.db"),
			gid:  "gr2",
			want: Sqlite,
		},
		{
			name: ".sqlite suffix",
			url:  filepath.Join(temp, "file4.sqlite"),
			gid:  "gr2",
			want: Sqlite,
		},
		{
			name: ".db suffix",
			url:  filepath.Join(temp, "file5.db"),
			gid:  "gr2",
			want: Sqlite,
		},
		{
			name:    "postgres ok format",
			url:     "postgres://user:pass@localhost/db",
			gid:     "gr3",
			want:    Postgres,
			wantErr: "failed to connect to postgres", // can't connect but format is ok
		},
		{
			name:    "empty url",
			url:     "",
			wantErr: "connection URL is empty",
		},
		{
			name:    "unsupported",
			url:     "unsupported://localhost/db",
			wantErr: "unsupported database type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := New(context.Background(), tt.url, tt.gid)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			defer res.Close()
			assert.Equal(t, tt.want, res.Type())
			assert.Equal(t, tt.gid, res.GID())
		})
	}
}

func TestEngine(t *testing.T) {
	t.Run("type and gid", func(t *testing.T) {
		db, err := NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()

		assert.Equal(t, Sqlite, db.Type())
		assert.Equal(t, "gr1", db.GID())
	})

	t.Run("invalid file", func(t *testing.T) {
		db, err := NewSqlite("/invalid/path", "gr1")
		assert.Error(t, err)
		assert.Equal(t, &SQL{}, db)
	})

	t.Run("default type", func(t *testing.T) {
		e := &SQL{}
		assert.Equal(t, Unknown, e.Type())
		assert.Equal(t, "", e.GID())
	})
}

func TestEngine_Adopt(t *testing.T) {
	tests := []struct {
		name     string
		dbType   Type
		query    string
		expected string
	}{
		{
			name:     "sqlite simple query",
			dbType:   Sqlite,
			query:    "SELECT * FROM test WHERE id = ?",
			expected: "SELECT * FROM test WHERE id = ?",
		},
		{
			name:     "sqlite multiple placeholders",
			dbType:   Sqlite,
			query:    "INSERT INTO test (id, name) VALUES (?, ?)",
			expected: "INSERT INTO test (id, name) VALUES (?, ?)",
		},
		{
			name:     "postgres simple query",
			dbType:   Postgres,
			query:    "SELECT * FROM test WHERE id = ?",
			expected: "SELECT * FROM test WHERE id = $1",
		},
		{
			name:     "postgres multiple placeholders",
			dbType:   Postgres,
			query:    "INSERT INTO test (id, name) VALUES (?, ?)",
			expected: "INSERT INTO test (id, name) VALUES ($1, $2)",
		},
		{
			name:     "postgres complex query",
			dbType:   Postgres,
			query:    "SELECT * FROM test WHERE id = ? AND name = ? OR value = ?",
			expected: "SELECT * FROM test WHERE id = $1 AND name = $2 OR value = $3",
		},
		{
			name:     "no placeholders",
			dbType:   Postgres,
			query:    "SELECT * FROM test",
			expected: "SELECT * FROM test",
		},
		{
			name:     "question mark in string literal",
			dbType:   Postgres,
			query:    "SELECT * FROM test WHERE text = '?' AND id = ?",
			expected: "SELECT * FROM test WHERE text = '?' AND id = $1",
		},
		{
			name:     "empty query",
			dbType:   Postgres,
			query:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &SQL{dbType: tt.dbType}
			result := e.Adopt(tt.query)
			assert.Equal(t, tt.expected, result)
		})
	}

	t.Run("unknown type defaults to non-postgres", func(t *testing.T) {
		e := &SQL{dbType: Unknown}
		query := "SELECT * FROM test WHERE id = ?"
		result := e.Adopt(query)
		assert.Equal(t, query, result)
	})
}

func TestConcurrentDBAccess(t *testing.T) {
	db, err := NewSqlite(":memory:", "gr1")
	require.NoError(t, err)
	defer db.Close()

	// create test table
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS test (id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errChan := make(chan error, 10)
	locker := db.MakeLock()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			locker.Lock()
			_, err := db.Exec("INSERT INTO test (value) VALUES (?)", i)
			locker.Unlock()
			if err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("concurrent access error: %v", err)
	}

	var count int
	err = db.Get(&count, "SELECT COUNT(*) FROM test")
	require.NoError(t, err)
	assert.Equal(t, 10, count)
}

func TestInitTable(t *testing.T) {
	const (
		cmdCreateTable DBCmd = iota + 1000
		cmdCreateIndexes
	)

	queries := NewQueryMap().
		Add(cmdCreateTable, Query{
			Sqlite: `CREATE TABLE IF NOT EXISTS test_table (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				gid TEXT DEFAULT '',
				data TEXT
			)`,
			Postgres: `CREATE TABLE IF NOT EXISTS test_table (
				id SERIAL PRIMARY KEY,
				gid TEXT DEFAULT '',
				data TEXT
			)`,
		}).
		AddSame(cmdCreateIndexes, `CREATE INDEX IF NOT EXISTS idx_test_table ON test_table(gid)`)

	t.Run("success case", func(t *testing.T) {
		db, err := NewSqlite(":memory:", "test_group")
		require.NoError(t, err)
		defer db.Close()

		migrateCalled := false
		cfg := TableConfig{
			Name:          "test_table",
			CreateTable:   cmdCreateTable,
			CreateIndexes: cmdCreateIndexes,
			MigrateFunc: func(ctx context.Context, tx *sqlx.Tx, gid string) error {
				migrateCalled = true
				assert.Equal(t, "test_group", gid)
				return nil
			},
			QueriesMap: queries,
		}

		err = InitTable(context.Background(), db, cfg)
		require.NoError(t, err)
		assert.True(t, migrateCalled, "migrate function should be called")

		// verify table exists
		var exists bool
		err = db.Get(&exists, "SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='test_table'")
		require.NoError(t, err)
		assert.True(t, exists, "table should exist")

		// verify index exists
		err = db.Get(&exists, "SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='index' AND name='idx_test_table'")
		require.NoError(t, err)
		assert.True(t, exists, "index should exist")
	})

	// Rest of the test cases remain unchanged as they don't interact with QueryMap structure
	t.Run("nil db", func(t *testing.T) {
		cfg := TableConfig{
			Name:          "test_table",
			CreateTable:   cmdCreateTable,
			CreateIndexes: cmdCreateIndexes,
			MigrateFunc:   func(context.Context, *sqlx.Tx, string) error { return nil },
			QueriesMap:    queries,
		}

		err := InitTable(context.Background(), nil, cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "db connection is nil")
	})

	t.Run("failed migration", func(t *testing.T) {
		db, err := NewSqlite(":memory:", "test_group")
		require.NoError(t, err)
		defer db.Close()

		migrationErr := fmt.Errorf("migration failed")
		cfg := TableConfig{
			Name:          "test_table",
			CreateTable:   cmdCreateTable,
			CreateIndexes: cmdCreateIndexes,
			MigrateFunc:   func(context.Context, *sqlx.Tx, string) error { return migrationErr },
			QueriesMap:    queries,
		}

		err = InitTable(context.Background(), db, cfg)
		assert.Error(t, err)
		assert.ErrorIs(t, err, migrationErr)

		// verify table doesn't exist after rollback
		var exists bool
		err = db.Get(&exists, "SELECT COUNT(*) > 0 FROM sqlite_master WHERE type='table' AND name='test_table'")
		require.NoError(t, err)
		assert.False(t, exists, "table should not exist after rollback")
	})

	t.Run("invalid query cmd", func(t *testing.T) {
		db, err := NewSqlite(":memory:", "test_group")
		require.NoError(t, err)
		defer db.Close()

		cfg := TableConfig{
			Name:          "test_table",
			CreateTable:   999, // invalid command
			CreateIndexes: cmdCreateIndexes,
			MigrateFunc:   func(context.Context, *sqlx.Tx, string) error { return nil },
			QueriesMap:    queries,
		}

		err = InitTable(context.Background(), db, cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get create table query")
	})
}

func TestNewPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	ctx := context.Background()

	t.Log("starting postgres container")
	req := testcontainers.ContainerRequest{
		Image:        "postgres:17",
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_PASSWORD": "secret",
			"POSTGRES_DB":       "postgres",
		},
		WaitingFor: wait.ForAll(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			wait.ForListeningPort("5432/tcp"),
		).WithDeadline(1 * time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)
	t.Log("postgres container started")
	defer func() { assert.NoError(t, container.Terminate(ctx)) }()

	// give postgres a moment to fully initialize
	time.Sleep(time.Second)

	host, err := container.Host(ctx)
	require.NoError(t, err)
	port, err := container.MappedPort(ctx, "5432")
	require.NoError(t, err)

	tests := []struct {
		name    string
		connStr string
		wantErr string
	}{
		{
			name:    "create new database",
			connStr: fmt.Sprintf("postgres://postgres:secret@%s:%d/test_db1?sslmode=disable", host, port.Int()),
		},
		{
			name:    "connect to existing database",
			connStr: fmt.Sprintf("postgres://postgres:secret@%s:%d/test_db1?sslmode=disable", host, port.Int()),
		},
		{
			name:    "invalid url",
			connStr: "postgres://invalid::url", // truly invalid URL format
			wantErr: "invalid postgres connection url",
		},
		{
			name:    "empty database name",
			connStr: fmt.Sprintf("postgres://postgres:secret@%s:%d/?sslmode=disable", host, port.Int()),
			wantErr: "database name not specified",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := NewPostgres(ctx, tt.connStr, "test")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			defer db.Close()

			// verify we can execute queries
			var result int
			err = db.Get(&result, "SELECT 1")
			assert.NoError(t, err)
			assert.Equal(t, 1, result)
		})
	}
}
