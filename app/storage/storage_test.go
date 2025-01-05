package storage

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEngine(t *testing.T) {
	t.Run("type and gid", func(t *testing.T) {
		db, err := NewSqliteDB(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()

		assert.Equal(t, EngineTypeSqlite, db.Type())
		assert.Equal(t, "gr1", db.GID())
	})

	t.Run("invalid file", func(t *testing.T) {
		db, err := NewSqliteDB("/invalid/path", "gr1")
		assert.Error(t, err)
		assert.Equal(t, &Engine{}, db)
	})

	t.Run("default type", func(t *testing.T) {
		e := &Engine{}
		assert.Equal(t, EngineTypeUnknown, e.Type())
		assert.Equal(t, "", e.GID())
	})
}

func TestNoopLocker(t *testing.T) {
	var l NoopLocker
	done := make(chan bool)

	go func() {
		l.Lock()
		l.RLock()
		l.RUnlock()
		l.Unlock()
		done <- true
	}()

	select {
	case <-done:
		// success
	case <-time.After(time.Second):
		t.Error("deadlock in NoopLocker")
	}
}

func TestRWLockerSelection(t *testing.T) {
	t.Run("sqlite uses mutex", func(t *testing.T) {
		db, err := NewSqliteDB(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()

		locker := db.MakeLock()
		_, ok := locker.(*sync.RWMutex)
		assert.True(t, ok)
	})

	t.Run("unknown type uses noop", func(t *testing.T) {
		e := &Engine{dbType: EngineTypeUnknown}
		locker := e.MakeLock()
		_, ok := locker.(*NoopLocker)
		assert.True(t, ok)
	})
}

func TestConcurrentDBAccess(t *testing.T) {
	db, err := NewSqliteDB(":memory:", "gr1")
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

func TestSampleUpdater(t *testing.T) {

	t.Run("append and read with normal timeout", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		samples, err := NewSamples(context.Background(), db)
		require.NoError(t, err)
		defer db.Close()

		updater := NewSampleUpdater(samples, SampleTypeSpam, time.Second)
		require.NoError(t, updater.Append("test spam message"))

		reader, err := updater.Reader()
		require.NoError(t, err)
		defer reader.Close()

		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, "test spam message\n", string(data))
	})

	t.Run("append multiple messages", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		samples, err := NewSamples(context.Background(), db)
		require.NoError(t, err)
		defer db.Close()

		updater := NewSampleUpdater(samples, SampleTypeHam, 1*time.Second)

		messages := []string{"msg1", "msg2", "msg3"}
		for _, msg := range messages {
			require.NoError(t, updater.Append(msg))
			time.Sleep(time.Millisecond) // ensure messages have different timestamps
		}

		reader, err := updater.Reader()
		require.NoError(t, err)
		defer reader.Close()

		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		result := strings.Split(strings.TrimSpace(string(data)), "\n")
		assert.Equal(t, len(messages), len(result))
		for _, msg := range messages {
			assert.Contains(t, result, msg)
		}
	})

	t.Run("timeout on append", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		samples, err := NewSamples(context.Background(), db)
		require.NoError(t, err)
		defer db.Close()

		updater := NewSampleUpdater(samples, SampleTypeSpam, time.Nanosecond)
		time.Sleep(time.Microsecond)
		err = updater.Append("timeout message")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context deadline exceeded")
	})

	t.Run("tiny timeout", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		samples, err := NewSamples(context.Background(), db)
		require.NoError(t, err)
		defer db.Close()

		updater := NewSampleUpdater(samples, SampleTypeSpam, 1)
		assert.Error(t, updater.Append("test message"))
	})

	t.Run("verify user origin", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		samples, err := NewSamples(context.Background(), db)
		require.NoError(t, err)
		defer db.Close()

		updater := NewSampleUpdater(samples, SampleTypeSpam, time.Second)
		require.NoError(t, updater.Append("test message"))

		// verify the message was stored with user origin
		ctx := context.Background()
		messages, err := samples.Read(ctx, SampleTypeSpam, SampleOriginUser)
		require.NoError(t, err)
		assert.Contains(t, messages, "test message")

		// verify it's not in preset origin
		messages, err = samples.Read(ctx, SampleTypeSpam, SampleOriginPreset)
		require.NoError(t, err)
		assert.NotContains(t, messages, "test message")
	})

	t.Run("sample type consistency", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		samples, err := NewSamples(context.Background(), db)
		require.NoError(t, err)
		defer db.Close()

		spamUpdater := NewSampleUpdater(samples, SampleTypeSpam, time.Second)
		hamUpdater := NewSampleUpdater(samples, SampleTypeHam, time.Second)

		require.NoError(t, spamUpdater.Append("spam message"))
		require.NoError(t, hamUpdater.Append("ham message"))

		ctx := context.Background()

		// verify spam messages
		messages, err := samples.Read(ctx, SampleTypeSpam, SampleOriginUser)
		require.NoError(t, err)
		assert.Contains(t, messages, "spam message")
		assert.NotContains(t, messages, "ham message")

		// verify ham messages
		messages, err = samples.Read(ctx, SampleTypeHam, SampleOriginUser)
		require.NoError(t, err)
		assert.Contains(t, messages, "ham message")
		assert.NotContains(t, messages, "spam message")
	})

	t.Run("read empty", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()

		samples, err := NewSamples(context.Background(), db)
		require.NoError(t, err)

		updater := NewSampleUpdater(samples, SampleTypeSpam, time.Second)
		reader, err := updater.Reader()
		require.NoError(t, err)
		defer reader.Close()

		data, err := reader.Read(make([]byte, 100))
		require.Equal(t, 0, data)
		require.Equal(t, io.EOF, err)
	})

	t.Run("timeout triggers", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()

		samples, err := NewSamples(context.Background(), db)
		require.NoError(t, err)

		updater := NewSampleUpdater(samples, SampleTypeSpam, time.Nanosecond)
		time.Sleep(time.Microsecond)
		require.Error(t, updater.Append("test"))
	})

	t.Run("no timeout", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()

		samples, err := NewSamples(context.Background(), db)
		require.NoError(t, err)

		updater := NewSampleUpdater(samples, SampleTypeSpam, 0)
		require.NoError(t, updater.Append("test"))

		reader, err := updater.Reader()
		require.NoError(t, err)
		defer reader.Close()

		data := make([]byte, 100)
		n, err := reader.Read(data)
		require.NoError(t, err)
		assert.Equal(t, "test\n", string(data[:n]))
	})
}

func setupTestDB(t *testing.T) (res *Engine, teardown func()) {
	t.Helper()
	tmpFile := os.TempDir() + "/test.db"
	t.Log("db file:", tmpFile)
	db, err := sqlx.Connect("sqlite", tmpFile)
	require.NoError(t, err)
	require.NotNil(t, db)
	engine, err := NewSqliteDB(tmpFile, "gr1")
	require.NoError(t, err)

	return engine, func() {
		db.Close()
		os.Remove(tmpFile)
	}
}
