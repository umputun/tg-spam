package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		db, err := NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()

		locker := db.MakeLock()
		_, ok := locker.(*sync.RWMutex)
		assert.True(t, ok)
	})

	t.Run("unknown type uses noop", func(t *testing.T) {
		e := &SQL{dbType: Unknown}
		locker := e.MakeLock()
		_, ok := locker.(*NoopLocker)
		assert.True(t, ok)
	})
}
