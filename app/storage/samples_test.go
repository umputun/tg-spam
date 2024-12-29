package storage

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//

func setupTestDB(t *testing.T) (res *sqlx.DB, teardown func()) {
	t.Helper()
	tmpFile := os.TempDir() + "/test.db"
	db, err := sqlx.Connect("sqlite", tmpFile)
	require.NoError(t, err)
	require.NotNil(t, db)
	return db, func() {
		db.Close()
		os.Remove(tmpFile)
	}
}

func TestNewSamples(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	tests := []struct {
		name    string
		db      *sqlx.DB
		wantErr bool
	}{
		{
			name:    "valid db connection",
			db:      db,
			wantErr: false,
		},
		{
			name:    "nil db connection",
			db:      nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := NewSamples(context.Background(), tt.db)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, s)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, s)
			}
		})
	}
}

func TestSamples_AddSample(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	s, err := NewSamples(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, s)

	ctx := context.Background()

	tests := []struct {
		name    string
		sType   SampleType
		origin  SampleOrigin
		message string
		wantErr bool
	}{
		{
			name:    "valid ham preset",
			sType:   SampleTypeHam,
			origin:  SampleOriginPreset,
			message: "test ham message",
			wantErr: false,
		},
		{
			name:    "valid spam user",
			sType:   SampleTypeSpam,
			origin:  SampleOriginUser,
			message: "test spam message",
			wantErr: false,
		},
		{
			name:    "invalid sample type",
			sType:   "invalid",
			origin:  SampleOriginPreset,
			message: "test message",
			wantErr: true,
		},
		{
			name:    "invalid origin",
			sType:   SampleTypeHam,
			origin:  "invalid",
			message: "test message",
			wantErr: true,
		},
		{
			name:    "empty message",
			sType:   SampleTypeHam,
			origin:  SampleOriginPreset,
			message: "",
			wantErr: true,
		},
		{
			name:    "origin any not allowed",
			sType:   SampleTypeHam,
			origin:  SampleOriginAny,
			message: "test message",
			wantErr: true,
		},
		{
			name:    "duplicate message same type and origin",
			sType:   SampleTypeHam,
			origin:  SampleOriginPreset,
			message: "test ham message", // Same as first test case
			wantErr: false,              // Should succeed and replace
		},
		{
			name:    "duplicate message different type",
			sType:   SampleTypeSpam,
			origin:  SampleOriginPreset,
			message: "test ham message", // Same message, different type
			wantErr: false,              // Should succeed and replace
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.Add(ctx, tt.sType, tt.origin, tt.message)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// verify message exists and has correct type and origin
				var count int
				err = db.Get(&count, "SELECT COUNT(*) FROM samples WHERE message = ? AND type = ? AND origin = ?",
					tt.message, tt.sType, tt.origin)
				require.NoError(t, err)
				assert.Equal(t, 1, count)
			}
		})
	}
}

func TestSamples_DeleteSample(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	s, err := NewSamples(context.Background(), db)
	require.NoError(t, err)

	ctx := context.Background()

	// add a sample first
	err = s.Add(ctx, SampleTypeHam, SampleOriginPreset, "test message")
	require.NoError(t, err)

	// get the ID of the inserted sample
	var id int64
	err = db.Get(&id, "SELECT id FROM samples WHERE message = ?", "test message")
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      int64
		wantErr bool
	}{
		{
			name:    "existing sample",
			id:      id,
			wantErr: false,
		},
		{
			name:    "non-existent sample",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.Delete(ctx, tt.id)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSamples_DeleteMessage(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	s, err := NewSamples(context.Background(), db)
	require.NoError(t, err)

	ctx := context.Background()

	// add test samples
	testData := []struct {
		sType   SampleType
		origin  SampleOrigin
		message string
	}{
		{SampleTypeHam, SampleOriginPreset, "message to delete"},
		{SampleTypeSpam, SampleOriginUser, "message to keep"},
		{SampleTypeHam, SampleOriginUser, "another message"},
	}

	for _, td := range testData {
		err := s.Add(ctx, td.sType, td.origin, td.message)
		require.NoError(t, err)
	}

	tests := []struct {
		name    string
		message string
		wantErr bool
	}{
		{
			name:    "existing message",
			message: "message to delete",
			wantErr: false,
		},
		{
			name:    "non-existent message",
			message: "no such message",
			wantErr: true,
		},
		{
			name:    "empty message",
			message: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.DeleteMessage(ctx, tt.message)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)

				// verify message no longer exists
				var count int
				err = db.Get(&count, "SELECT COUNT(*) FROM samples WHERE message = ?", tt.message)
				require.NoError(t, err)
				assert.Equal(t, 0, count)

				// verify other messages still exist
				var totalCount int
				err = db.Get(&totalCount, "SELECT COUNT(*) FROM samples")
				require.NoError(t, err)
				assert.Equal(t, len(testData)-1, totalCount)
			}
		})
	}

	t.Run("concurrent delete", func(t *testing.T) {
		// add a message that will be deleted concurrently
		msg := "concurrent delete message"
		err := s.Add(ctx, SampleTypeHam, SampleOriginPreset, msg)
		require.NoError(t, err)

		const numWorkers = 10
		var wg sync.WaitGroup
		errCh := make(chan error, numWorkers)

		// start multiple goroutines trying to delete the same message
		for i := 0; i < numWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := s.DeleteMessage(ctx, msg); err != nil && !strings.Contains(err.Error(), "not found") {
					errCh <- err
				}
			}()
		}

		wg.Wait()
		close(errCh)

		// check for unexpected errors
		for err := range errCh {
			t.Errorf("concurrent delete failed: %v", err)
		}

		// verify message was deleted
		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM samples WHERE message = ?", msg)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})
}

func TestSamples_ReadSamples(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	s, err := NewSamples(context.Background(), db)
	require.NoError(t, err)

	ctx := context.Background()

	// add test samples
	testData := []struct {
		sType   SampleType
		origin  SampleOrigin
		message string
	}{
		{SampleTypeHam, SampleOriginPreset, "ham preset 1"},
		{SampleTypeHam, SampleOriginUser, "ham user 1"},
		{SampleTypeSpam, SampleOriginPreset, "spam preset 1"},
		{SampleTypeSpam, SampleOriginUser, "spam user 1"},
	}

	for _, td := range testData {
		err := s.Add(ctx, td.sType, td.origin, td.message)
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		sType         SampleType
		origin        SampleOrigin
		expectedCount int
		wantErr       bool
	}{
		{
			name:          "all ham samples",
			sType:         SampleTypeHam,
			origin:        SampleOriginAny,
			expectedCount: 2,
			wantErr:       false,
		},
		{
			name:          "preset spam samples",
			sType:         SampleTypeSpam,
			origin:        SampleOriginPreset,
			expectedCount: 1,
			wantErr:       false,
		},
		{
			name:          "invalid type",
			sType:         "invalid",
			origin:        SampleOriginPreset,
			expectedCount: 0,
			wantErr:       true,
		},
		{
			name:          "invalid origin",
			sType:         SampleTypeHam,
			origin:        "invalid",
			expectedCount: 0,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			samples, err := s.Read(ctx, tt.sType, tt.origin)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, samples)
			} else {
				assert.NoError(t, err)
				assert.Len(t, samples, tt.expectedCount)
			}
		})
	}
}

func TestSamples_GetStats(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	s, err := NewSamples(context.Background(), db)
	require.NoError(t, err)

	ctx := context.Background()

	// add test samples
	testData := []struct {
		sType   SampleType
		origin  SampleOrigin
		message string
	}{
		{SampleTypeHam, SampleOriginPreset, "ham preset 1"},
		{SampleTypeHam, SampleOriginPreset, "ham preset 2"},
		{SampleTypeHam, SampleOriginUser, "ham user 1"},
		{SampleTypeSpam, SampleOriginPreset, "spam preset 1"},
		{SampleTypeSpam, SampleOriginUser, "spam user 1"},
		{SampleTypeSpam, SampleOriginUser, "spam user 2"},
	}

	for _, td := range testData {
		err := s.Add(ctx, td.sType, td.origin, td.message)
		require.NoError(t, err)
	}

	stats, err := s.Stats(ctx)
	require.NoError(t, err)
	require.NotNil(t, stats)

	assert.Equal(t, 3, stats.TotalSpam)
	assert.Equal(t, 3, stats.TotalHam)
	assert.Equal(t, 1, stats.PresetSpam)
	assert.Equal(t, 2, stats.PresetHam)
	assert.Equal(t, 2, stats.UserSpam)
	assert.Equal(t, 1, stats.UserHam)
}

func TestSampleType_Validate(t *testing.T) {
	tests := []struct {
		name    string
		sType   SampleType
		wantErr bool
	}{
		{"valid ham", SampleTypeHam, false},
		{"valid spam", SampleTypeSpam, false},
		{"invalid type", "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.sType.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSampleOrigin_Validate(t *testing.T) {
	tests := []struct {
		name    string
		origin  SampleOrigin
		wantErr bool
	}{
		{"valid preset", SampleOriginPreset, false},
		{"valid user", SampleOriginUser, false},
		{"valid any", SampleOriginAny, false},
		{"invalid origin", "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.origin.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSamples_Concurrent(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	require.NotNil(t, db)

	// initialize samples with schema
	s, err := NewSamples(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, s)

	// Verify table exists and is accessible
	ctx := context.Background()
	err = s.Add(ctx, SampleTypeHam, SampleOriginPreset, "test message")
	require.NoError(t, err, "Failed to insert initial test record")

	const numWorkers = 10
	const numOps = 50

	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*2)

	// start readers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				if _, err := s.Read(ctx, SampleTypeHam, SampleOriginAny); err != nil {
					select {
					case errCh <- fmt.Errorf("reader %d failed: %w", workerID, err):
					default:
					}
					return
				}
			}
		}(i)
	}

	// start writers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				msg := fmt.Sprintf("test message %d-%d", workerID, j)
				sType := SampleTypeHam
				if j%2 == 0 {
					sType = SampleTypeSpam
				}
				if err := s.Add(ctx, sType, SampleOriginUser, msg); err != nil {
					select {
					case errCh <- fmt.Errorf("writer %d failed: %w", workerID, err):
					default:
					}
					return
				}
			}
		}(i)
	}

	// wait for all goroutines to finish
	wg.Wait()
	close(errCh)

	// check for any errors
	for err := range errCh {
		t.Errorf("concurrent operation failed: %v", err)
	}

	// verify the final state
	stats, err := s.Stats(ctx)
	require.NoError(t, err)
	require.NotNil(t, stats)

	expectedTotal := numWorkers*numOps + 1 // +1 for the initial test message
	actualTotal := stats.TotalHam + stats.TotalSpam
	require.Equal(t, expectedTotal, actualTotal, "expected %d total samples, got %d", expectedTotal, actualTotal)
}

func TestSamples_Iterator(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	samples, err := NewSamples(context.Background(), db)
	require.NoError(t, err)

	ctx := context.Background()

	// Insert test data
	testData := []struct {
		sType   SampleType
		origin  SampleOrigin
		message string
	}{
		{SampleTypeHam, SampleOriginPreset, "ham preset 1"},
		{SampleTypeHam, SampleOriginUser, "ham user 1"},
		{SampleTypeSpam, SampleOriginPreset, "spam preset 1"},
		{SampleTypeSpam, SampleOriginUser, "spam user 1"},
	}

	for _, td := range testData {
		err := samples.Add(ctx, td.sType, td.origin, td.message)
		require.NoError(t, err)
	}

	// Test cases
	tests := []struct {
		name         string
		sType        SampleType
		origin       SampleOrigin
		expectedMsgs []string
		expectErr    bool
	}{
		{
			name:         "Ham Preset Samples",
			sType:        SampleTypeHam,
			origin:       SampleOriginPreset,
			expectedMsgs: []string{"ham preset 1"},
			expectErr:    false,
		},
		{
			name:         "Spam User Samples",
			sType:        SampleTypeSpam,
			origin:       SampleOriginUser,
			expectedMsgs: []string{"spam user 1"},
			expectErr:    false,
		},
		{
			name:         "All Ham Samples",
			sType:        SampleTypeHam,
			origin:       SampleOriginAny,
			expectedMsgs: []string{"ham preset 1", "ham user 1"},
			expectErr:    false,
		},
		{
			name:         "Invalid Sample Type",
			sType:        "invalid",
			origin:       SampleOriginPreset,
			expectedMsgs: nil,
			expectErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iter, err := samples.Iterator(ctx, tt.sType, tt.origin)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var messages []string
			for msg := range iter {
				messages = append(messages, msg)
			}

			assert.ElementsMatch(t, tt.expectedMsgs, messages)
		})
	}
}

func TestSamples_IteratorOrder(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	samples, err := NewSamples(context.Background(), db)
	require.NoError(t, err)

	ctx := context.Background()

	// Insert test data
	testData := []struct {
		sType   SampleType
		origin  SampleOrigin
		message string
	}{
		{SampleTypeHam, SampleOriginPreset, "ham preset 1"},
		{SampleTypeHam, SampleOriginPreset, "ham preset 2"},
		{SampleTypeHam, SampleOriginPreset, "ham preset 3"},
	}

	for _, td := range testData {
		err := samples.Add(ctx, td.sType, td.origin, td.message)
		require.NoError(t, err)
		time.Sleep(time.Second) // ensure each message has a unique timestamp
	}

	iter, err := samples.Iterator(ctx, SampleTypeHam, SampleOriginPreset)
	require.NoError(t, err)
	var messages []string
	for msg := range iter {
		messages = append(messages, msg)
	}
	require.Len(t, messages, 3)
	assert.Equal(t, "ham preset 3", messages[0])
	assert.Equal(t, "ham preset 2", messages[1])
	assert.Equal(t, "ham preset 1", messages[2])
}

func TestSamples_Import(t *testing.T) {
	ctx := context.Background()

	countSamples := func(db *sqlx.DB, t SampleType, o SampleOrigin) int {
		var count int
		err := db.Get(&count, "SELECT COUNT(*) FROM samples WHERE type = ? AND origin = ?", t, o)
		if err != nil {
			return -1
		}
		return count
	}

	prep := func() (*sqlx.DB, *Samples, func()) {
		db, teardown := setupTestDB(t)
		s, err := NewSamples(context.Background(), db)
		require.NoError(t, err)
		return db, s, teardown
	}

	t.Run("basic import with cleanup", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		input := strings.NewReader("sample1\nsample2\nsample3")
		stats, err := s.Import(ctx, SampleTypeHam, SampleOriginPreset, input, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		assert.Equal(t, 3, countSamples(db, SampleTypeHam, SampleOriginPreset))
		assert.Equal(t, 3, stats.PresetHam)
	})

	t.Run("import without cleanup should append", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		// first import
		input1 := strings.NewReader("existing1\nexisting2")
		_, err := s.Import(ctx, SampleTypeSpam, SampleOriginPreset, input1, true)
		require.NoError(t, err)
		assert.Equal(t, 2, countSamples(db, SampleTypeSpam, SampleOriginPreset))

		// second import without cleanup should append
		input2 := strings.NewReader("new1\nnew2")
		stats, err := s.Import(ctx, SampleTypeSpam, SampleOriginPreset, input2, false)
		require.NoError(t, err)
		require.NotNil(t, stats)

		assert.Equal(t, 4, countSamples(db, SampleTypeSpam, SampleOriginPreset))
		assert.Equal(t, 4, stats.PresetSpam)

		// verify content includes all samples
		samples, err := s.Read(ctx, SampleTypeSpam, SampleOriginPreset)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"existing1", "existing2", "new1", "new2"}, samples)
	})

	t.Run("import with cleanup should replace", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		// first import
		input1 := strings.NewReader("old1\nold2\nold3")
		_, err := s.Import(ctx, SampleTypeSpam, SampleOriginUser, input1, true)
		require.NoError(t, err)
		assert.Equal(t, 3, countSamples(db, SampleTypeSpam, SampleOriginUser))

		// second import with cleanup should replace
		input2 := strings.NewReader("new1\nnew2")
		stats, err := s.Import(ctx, SampleTypeSpam, SampleOriginUser, input2, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		assert.Equal(t, 2, countSamples(db, SampleTypeSpam, SampleOriginUser))
		assert.Equal(t, 2, stats.UserSpam)

		// verify content was replaced
		samples, err := s.Read(ctx, SampleTypeSpam, SampleOriginUser)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"new1", "new2"}, samples)
	})

	t.Run("different types preserve independence", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		// import ham samples
		inputHam := strings.NewReader("ham1\nham2")
		_, err := s.Import(ctx, SampleTypeHam, SampleOriginUser, inputHam, true)
		require.NoError(t, err)

		// import spam samples
		inputSpam := strings.NewReader("spam1\nspam2\nspam3")
		stats, err := s.Import(ctx, SampleTypeSpam, SampleOriginUser, inputSpam, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		assert.Equal(t, 2, countSamples(db, SampleTypeHam, SampleOriginUser))
		assert.Equal(t, 3, countSamples(db, SampleTypeSpam, SampleOriginUser))
	})

	t.Run("invalid type", func(t *testing.T) {
		_, s, teardown := prep()
		defer teardown()

		input := strings.NewReader("sample")
		_, err := s.Import(ctx, "invalid", SampleOriginPreset, input, true)
		assert.Error(t, err)
	})

	t.Run("invalid origin", func(t *testing.T) {
		_, s, teardown := prep()
		defer teardown()

		input := strings.NewReader("sample")
		_, err := s.Import(ctx, SampleTypeHam, "invalid", input, true)
		assert.Error(t, err)
	})

	t.Run("origin any not allowed", func(t *testing.T) {
		_, s, teardown := prep()
		defer teardown()

		input := strings.NewReader("sample")
		_, err := s.Import(ctx, SampleTypeHam, SampleOriginAny, input, true)
		assert.Error(t, err)
	})

	t.Run("empty input", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		input := strings.NewReader("")
		stats, err := s.Import(ctx, SampleTypeHam, SampleOriginPreset, input, true)
		require.NoError(t, err)
		require.NotNil(t, stats)
		assert.Equal(t, 0, countSamples(db, SampleTypeHam, SampleOriginPreset))
	})

	t.Run("input with empty lines", func(t *testing.T) {
		_, s, teardown := prep()
		defer teardown()

		input := strings.NewReader("sample1\n\n\nsample2\n\n")
		stats, err := s.Import(ctx, SampleTypeHam, SampleOriginPreset, input, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		samples, err := s.Read(ctx, SampleTypeHam, SampleOriginPreset)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"sample1", "sample2"}, samples)
	})

	t.Run("nil reader", func(t *testing.T) {
		_, s, teardown := prep()
		defer teardown()

		_, err := s.Import(ctx, SampleTypeHam, SampleOriginPreset, nil, true)
		assert.Error(t, err)
	})

	t.Run("reader error", func(t *testing.T) {
		_, s, teardown := prep()
		defer teardown()

		errReader := &errorReader{err: fmt.Errorf("read error")}
		_, err := s.Import(ctx, SampleTypeHam, SampleOriginPreset, errReader, true)
		assert.Error(t, err)
	})

	t.Run("duplicate message different type", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		// import ham samples
		inputHam := strings.NewReader("message1\nmessage2")
		_, err := s.Import(ctx, SampleTypeHam, SampleOriginUser, inputHam, true)
		require.NoError(t, err)

		// import spam samples with same messages
		inputSpam := strings.NewReader("message1\nmessage2\nmessage3")
		stats, err := s.Import(ctx, SampleTypeSpam, SampleOriginUser, inputSpam, false)
		require.NoError(t, err)
		require.NotNil(t, stats)

		// verify only new messages are added, duplicates replaced
		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM samples")
		require.NoError(t, err)
		assert.Equal(t, 3, count)

		// verify type is updated for duplicates
		var spamCount int
		err = db.Get(&spamCount, "SELECT COUNT(*) FROM samples WHERE type = ?", SampleTypeSpam)
		require.NoError(t, err)
		assert.Equal(t, 3, spamCount)
	})

	t.Run("duplicate message within import", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()
		ctx := context.Background()

		// import with duplicate messages
		input := strings.NewReader("message1\nmessage2\nmessage1")
		stats, err := s.Import(ctx, SampleTypeHam, SampleOriginUser, input, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		// verify only unique messages are stored
		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM samples")
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})
}

func TestSamples_Reader(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(*Samples)
		sampleType SampleType
		origin     SampleOrigin
		want       []string
		wantErr    bool
	}{
		{
			name: "ham samples",
			setup: func(s *Samples) {
				require.NoError(t, s.Add(context.Background(), SampleTypeHam, SampleOriginPreset, "test1"))
				time.Sleep(time.Second) // ensure each message has a unique timestamp
				require.NoError(t, s.Add(context.Background(), SampleTypeHam, SampleOriginPreset, "test2"))
			},
			sampleType: SampleTypeHam,
			origin:     SampleOriginPreset,
			want:       []string{"test2", "test1"}, // ordered by timestamp DESC
		},
		{
			name: "empty result",
			setup: func(s *Samples) {
				// no setup needed, db is empty
			},
			sampleType: SampleTypeSpam,
			origin:     SampleOriginUser,
			want:       []string(nil),
		},
		{
			name:       "invalid type",
			sampleType: "invalid",
			origin:     SampleOriginPreset,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := sqlx.Open("sqlite", ":memory:")
			require.NoError(t, err)
			defer db.Close()

			s, err := NewSamples(context.Background(), db)
			require.NoError(t, err)

			if tt.setup != nil {
				tt.setup(s)
			}

			r, err := s.Reader(context.Background(), tt.sampleType, tt.origin)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			lines := 0
			scanner := bufio.NewScanner(r)
			var got []string
			for scanner.Scan() {
				lines++
				got = append(got, scanner.Text())
			}
			require.NoError(t, scanner.Err())
			assert.Equal(t, tt.want, got)
			assert.Equal(t, len(tt.want), lines)

			assert.NoError(t, r.Close())
		})
	}
}

// errorReader implements io.Reader interface and always returns an error
type errorReader struct {
	err error
}

func (r *errorReader) Read(_ []byte) (n int, err error) {
	return 0, r.err
}
