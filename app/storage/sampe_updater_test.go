package storage

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

	t.Run("remove message", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		samples, err := NewSamples(context.Background(), db)
		require.NoError(t, err)
		defer db.Close()

		updater := NewSampleUpdater(samples, SampleTypeSpam, time.Second)
		require.NoError(t, updater.Append("test message"))
		require.NoError(t, updater.Remove("test message"))

		reader, err := updater.Reader()
		require.NoError(t, err)
		defer reader.Close()

		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Empty(t, string(data))
	})

	t.Run("remove with timeout", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		samples, err := NewSamples(context.Background(), db)
		require.NoError(t, err)
		defer db.Close()

		updater := NewSampleUpdater(samples, SampleTypeSpam, time.Nanosecond)
		time.Sleep(time.Microsecond)
		err = updater.Remove("test message")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context deadline exceeded")
	})

	t.Run("remove non-existent", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		samples, err := NewSamples(context.Background(), db)
		require.NoError(t, err)
		defer db.Close()

		updater := NewSampleUpdater(samples, SampleTypeSpam, time.Second)
		err = updater.Remove("non-existent message")
		require.Error(t, err)
	})
}
