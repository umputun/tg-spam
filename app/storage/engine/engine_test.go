package engine

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
