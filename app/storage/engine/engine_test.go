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
