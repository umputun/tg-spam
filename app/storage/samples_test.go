package storage

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSamples(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	tests := []struct {
		name    string
		db      *Engine
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
		groupID string
		message string
		wantErr bool
	}{
		{
			name:    "valid ham preset",
			sType:   SampleTypeHam,
			origin:  SampleOriginPreset,
			message: "test ham message",
			groupID: "group1",
			wantErr: false,
		},
		{
			name:    "valid ham preset, different group",
			sType:   SampleTypeHam,
			origin:  SampleOriginPreset,
			message: "test ham message2",
			groupID: "group2",
			wantErr: false,
		},
		{
			name:    "valid spam user",
			sType:   SampleTypeSpam,
			origin:  SampleOriginUser,
			message: "test spam message",
			groupID: "group1",
			wantErr: false,
		},
		{
			name:    "invalid sample type",
			sType:   "invalid",
			origin:  SampleOriginPreset,
			message: "test message",
			groupID: "group1",
			wantErr: true,
		},
		{
			name:    "invalid origin",
			sType:   SampleTypeHam,
			origin:  "invalid",
			message: "test message",
			groupID: "group1",
			wantErr: true,
		},
		{
			name:    "empty message",
			sType:   SampleTypeHam,
			origin:  SampleOriginPreset,
			message: "",
			groupID: "group1",
			wantErr: true,
		},
		{
			name:    "origin any not allowed",
			sType:   SampleTypeHam,
			origin:  SampleOriginAny,
			message: "test message",
			groupID: "group1",
			wantErr: true,
		},
		{
			name:    "duplicate message same type and origin",
			sType:   SampleTypeHam,
			origin:  SampleOriginPreset,
			message: "test ham message", // Same as first test case
			groupID: "group1",
			wantErr: false, // Should succeed and replace
		},
		{
			name:    "duplicate message same type and origin, different group",
			sType:   SampleTypeHam,
			origin:  SampleOriginPreset,
			message: "test ham message", // Same as the first test case
			groupID: "group11",
			wantErr: false,
		},
		{
			name:    "duplicate message different type",
			sType:   SampleTypeSpam,
			origin:  SampleOriginPreset,
			message: "test ham message", // Same message, different type
			groupID: "group1",
			wantErr: false, // Should succeed and replace
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.Add(ctx, tt.groupID, tt.sType, tt.origin, tt.message)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// verify message exists and has correct type and origin
				var count int
				err = db.Get(&count, "SELECT COUNT(*) FROM samples WHERE message = ? AND type = ? AND origin = ? AND gid = ?",
					tt.message, tt.sType, tt.origin, tt.groupID)
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
	err = s.Add(ctx, "group1", SampleTypeHam, SampleOriginPreset, "test message")
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

	// add test samples with different groups
	samples := []struct {
		gid     string
		sType   SampleType
		origin  SampleOrigin
		message string
	}{
		{"gr1", SampleTypeHam, SampleOriginPreset, "msg1"},
		{"gr2", SampleTypeHam, SampleOriginPreset, "msg1"}, // same message, different group
		{"gr1", SampleTypeSpam, SampleOriginUser, "msg2"},
		{"gr2", SampleTypeSpam, SampleOriginUser, "msg2"}, // same message, different group
	}

	for _, smpl := range samples {
		err := s.Add(ctx, smpl.gid, smpl.sType, smpl.origin, smpl.message)
		require.NoError(t, err)
	}

	t.Run("delete msg1 from gr1", func(t *testing.T) {
		err := s.DeleteMessage(ctx, "gr1", "msg1")
		assert.NoError(t, err)

		// Verify message was deleted from gr1
		var count int
		err = db.Get(&count, `SELECT COUNT(*) FROM samples WHERE gid = ? AND message = ?`, "gr1", "msg1")
		require.NoError(t, err)
		assert.Equal(t, 0, count)

		// Verify message still exists in gr2
		err = db.Get(&count, `SELECT COUNT(*) FROM samples WHERE gid = ? AND message = ?`, "gr2", "msg1")
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("delete msg2 from gr2", func(t *testing.T) {
		err := s.DeleteMessage(ctx, "gr2", "msg2")
		assert.NoError(t, err)

		// Verify message was deleted from gr2
		var count int
		err = db.Get(&count, `SELECT COUNT(*) FROM samples WHERE gid = ? AND message = ?`, "gr2", "msg2")
		require.NoError(t, err)
		assert.Equal(t, 0, count)

		// Verify message still exists in gr1
		err = db.Get(&count, `SELECT COUNT(*) FROM samples WHERE gid = ? AND message = ?`, "gr1", "msg2")
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("delete from non-existent group", func(t *testing.T) {
		err := s.DeleteMessage(ctx, "gr3", "msg1")
		assert.Error(t, err)
	})

	t.Run("delete non-existent message", func(t *testing.T) {
		err := s.DeleteMessage(ctx, "gr1", "msg-none")
		assert.Error(t, err)
	})

	t.Run("concurrent delete", func(t *testing.T) {
		const msg = "concurrent-msg"
		err := s.Add(ctx, "gr1", SampleTypeHam, SampleOriginPreset, msg)
		require.NoError(t, err)

		var wg sync.WaitGroup
		const workers = 10
		errCh := make(chan error, workers)

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := s.DeleteMessage(ctx, "gr1", msg); err != nil && !strings.Contains(err.Error(), "not found") {
					errCh <- err
				}
			}()
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			assert.NoError(t, err)
		}

		// Verify message was deleted
		var count int
		err = db.Get(&count, `SELECT COUNT(*) FROM samples WHERE gid = ? AND message = ?`, "gr1", msg)
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
		groupID string
		message string
	}{
		{SampleTypeHam, SampleOriginPreset, "gr1", "ham preset 1"},
		{SampleTypeHam, SampleOriginUser, "gr1", "ham user 1"},
		{SampleTypeSpam, SampleOriginPreset, "gr1", "spam preset 1"},
		{SampleTypeSpam, SampleOriginUser, "gr1", "spam user 1"},
		{SampleTypeSpam, SampleOriginUser, "gr2", "spam user 12"},
	}

	for _, td := range testData {
		err := s.Add(ctx, td.groupID, td.sType, td.origin, td.message)
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
			samples, err := s.Read(ctx, "gr1", tt.sType, tt.origin)
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
		gid     string
		sType   SampleType
		origin  SampleOrigin
		message string
	}{
		{"group1", SampleTypeHam, SampleOriginPreset, "ham preset 1"},
		{"group1", SampleTypeHam, SampleOriginPreset, "ham preset 2"},
		{"group1", SampleTypeHam, SampleOriginUser, "ham user 1"},
		{"group2", SampleTypeHam, SampleOriginPreset, "ham preset 3"},
		{"group1", SampleTypeSpam, SampleOriginPreset, "spam preset 1"},
		{"group1", SampleTypeSpam, SampleOriginUser, "spam user 1"},
		{"group1", SampleTypeSpam, SampleOriginUser, "spam user 2"},
	}

	for _, td := range testData {
		err := s.Add(ctx, td.gid, td.sType, td.origin, td.message)
		require.NoError(t, err)
	}

	t.Run("stats for group1", func(t *testing.T) {
		stats, err := s.Stats(ctx, "group1")
		require.NoError(t, err)
		require.NotNil(t, stats)

		assert.Equal(t, 3, stats.TotalSpam)
		assert.Equal(t, 3, stats.TotalHam)
		assert.Equal(t, 1, stats.PresetSpam)
		assert.Equal(t, 2, stats.PresetHam)
		assert.Equal(t, 2, stats.UserSpam)
		assert.Equal(t, 1, stats.UserHam)
	})

	t.Run("stats for group2", func(t *testing.T) {
		stats, err := s.Stats(ctx, "group2")
		require.NoError(t, err)
		require.NotNil(t, stats)

		assert.Equal(t, 0, stats.TotalSpam)
		assert.Equal(t, 1, stats.TotalHam)
		assert.Equal(t, 0, stats.PresetSpam)
		assert.Equal(t, 1, stats.PresetHam)
		assert.Equal(t, 0, stats.UserSpam)
		assert.Equal(t, 0, stats.UserHam)
	})

	t.Run("stats for empty group", func(t *testing.T) {
		stats, err := s.Stats(ctx, "nonexistent")
		require.NoError(t, err)
		require.NotNil(t, stats)

		assert.Equal(t, 0, stats.TotalSpam)
		assert.Equal(t, 0, stats.TotalHam)
		assert.Equal(t, 0, stats.PresetSpam)
		assert.Equal(t, 0, stats.PresetHam)
		assert.Equal(t, 0, stats.UserSpam)
		assert.Equal(t, 0, stats.UserHam)
	})
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
	err = s.Add(ctx, "gr1", SampleTypeHam, SampleOriginPreset, "test message")
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
				if _, err := s.Read(ctx, "gr1", SampleTypeHam, SampleOriginAny); err != nil {
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
				if err := s.Add(ctx, "gr1", sType, SampleOriginUser, msg); err != nil {
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
	stats, err := s.Stats(ctx, "gr1")
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
		groupID string
		message string
	}{
		{SampleTypeHam, SampleOriginPreset, "gr1", "ham preset 1"},
		{SampleTypeHam, SampleOriginUser, "gr1", "ham user 1"},
		{SampleTypeSpam, SampleOriginPreset, "gr1", "spam preset 1"},
		{SampleTypeSpam, SampleOriginUser, "gr1", "spam user 1"},
		{SampleTypeSpam, SampleOriginUser, "gr2", "spam user 2"},
	}

	for _, td := range testData {
		err := samples.Add(ctx, td.groupID, td.sType, td.origin, td.message)
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
			iter, err := samples.Iterator(ctx, "gr1", tt.sType, tt.origin)
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
		err := samples.Add(ctx, "gr1", td.sType, td.origin, td.message)
		require.NoError(t, err)
		time.Sleep(time.Second) // ensure each message has a unique timestamp
	}

	iter, err := samples.Iterator(ctx, "gr1", SampleTypeHam, SampleOriginPreset)
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

	countSamples := func(db *Engine, gid string, t SampleType, o SampleOrigin) int {
		var count int
		err := db.Get(&count, "SELECT COUNT(*) FROM samples WHERE gid = ? AND type = ? AND origin = ?", gid, t, o)
		if err != nil {
			return -1
		}
		return count
	}

	prep := func() (*Engine, *Samples, func()) {
		db, teardown := setupTestDB(t)
		s, err := NewSamples(context.Background(), db)
		require.NoError(t, err)
		return db, s, teardown
	}

	t.Run("basic import with cleanup", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		input := strings.NewReader("sample1\nsample2\nsample3")
		stats, err := s.Import(ctx, "group1", SampleTypeHam, SampleOriginPreset, input, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		assert.Equal(t, 3, countSamples(db, "group1", SampleTypeHam, SampleOriginPreset))
		assert.Equal(t, 3, stats.PresetHam)
	})

	t.Run("import without cleanup should append", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		// first import
		input1 := strings.NewReader("existing1\nexisting2")
		_, err := s.Import(ctx, "group1", SampleTypeSpam, SampleOriginPreset, input1, true)
		require.NoError(t, err)
		assert.Equal(t, 2, countSamples(db, "group1", SampleTypeSpam, SampleOriginPreset))

		// second import without cleanup should append
		input2 := strings.NewReader("new1\nnew2")
		stats, err := s.Import(ctx, "group1", SampleTypeSpam, SampleOriginPreset, input2, false)
		require.NoError(t, err)
		require.NotNil(t, stats)

		assert.Equal(t, 4, countSamples(db, "group1", SampleTypeSpam, SampleOriginPreset))
		assert.Equal(t, 4, stats.PresetSpam)

		// verify content includes all samples
		samples, err := s.Read(ctx, "group1", SampleTypeSpam, SampleOriginPreset)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"existing1", "existing2", "new1", "new2"}, samples)
	})

	t.Run("import with cleanup should replace", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		// first import
		input1 := strings.NewReader("old1\nold2\nold3")
		_, err := s.Import(ctx, "group1", SampleTypeSpam, SampleOriginUser, input1, true)
		require.NoError(t, err)
		assert.Equal(t, 3, countSamples(db, "group1", SampleTypeSpam, SampleOriginUser))

		// second import with cleanup should replace
		input2 := strings.NewReader("new1\nnew2")
		stats, err := s.Import(ctx, "group1", SampleTypeSpam, SampleOriginUser, input2, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		assert.Equal(t, 2, countSamples(db, "group1", SampleTypeSpam, SampleOriginUser))
		assert.Equal(t, 2, stats.UserSpam)

		// verify content was replaced
		samples, err := s.Read(ctx, "group1", SampleTypeSpam, SampleOriginUser)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"new1", "new2"}, samples)
	})

	t.Run("import to different groups", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		input1 := strings.NewReader("sample1\nsample2")
		_, err := s.Import(ctx, "group1", SampleTypeHam, SampleOriginPreset, input1, true)
		require.NoError(t, err)
		assert.Equal(t, 2, countSamples(db, "group1", SampleTypeHam, SampleOriginPreset), "imported 2 samples to group1")

		input2 := strings.NewReader("sample1\nsample2\nsample3")
		_, err = s.Import(ctx, "group2", SampleTypeHam, SampleOriginPreset, input2, true)
		require.NoError(t, err)
		assert.Equal(t, 3, countSamples(db, "group2", SampleTypeHam, SampleOriginPreset), "imported 3 samples to group2")

		assert.Equal(t, 2, countSamples(db, "group1", SampleTypeHam, SampleOriginPreset), "group1 samples unchanged")

		// verify cleanup only affects target group
		input3 := strings.NewReader("sample3")
		_, err = s.Import(ctx, "group1", SampleTypeHam, SampleOriginPreset, input3, true)
		require.NoError(t, err)

		assert.Equal(t, 1, countSamples(db, "group1", SampleTypeHam, SampleOriginPreset))
		assert.Equal(t, 3, countSamples(db, "group2", SampleTypeHam, SampleOriginPreset))
	})

	t.Run("different types preserve independence", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		// import ham samples
		inputHam := strings.NewReader("ham1\nham2")
		_, err := s.Import(ctx, "group1", SampleTypeHam, SampleOriginUser, inputHam, true)
		require.NoError(t, err)

		// import spam samples
		inputSpam := strings.NewReader("spam1\nspam2\nspam3")
		stats, err := s.Import(ctx, "group1", SampleTypeSpam, SampleOriginUser, inputSpam, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		assert.Equal(t, 2, countSamples(db, "group1", SampleTypeHam, SampleOriginUser))
		assert.Equal(t, 3, countSamples(db, "group1", SampleTypeSpam, SampleOriginUser))
	})

	t.Run("invalid type", func(t *testing.T) {
		_, s, teardown := prep()
		defer teardown()

		input := strings.NewReader("sample")
		_, err := s.Import(ctx, "group1", "invalid", SampleOriginPreset, input, true)
		assert.Error(t, err)
	})

	t.Run("invalid origin", func(t *testing.T) {
		_, s, teardown := prep()
		defer teardown()

		input := strings.NewReader("sample")
		_, err := s.Import(ctx, "group1", SampleTypeHam, "invalid", input, true)
		assert.Error(t, err)
	})

	t.Run("origin any not allowed", func(t *testing.T) {
		_, s, teardown := prep()
		defer teardown()

		input := strings.NewReader("sample")
		_, err := s.Import(ctx, "group1", SampleTypeHam, SampleOriginAny, input, true)
		assert.Error(t, err)
	})

	t.Run("empty input", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		input := strings.NewReader("")
		stats, err := s.Import(ctx, "group1", SampleTypeHam, SampleOriginPreset, input, true)
		require.NoError(t, err)
		require.NotNil(t, stats)
		assert.Equal(t, 0, countSamples(db, "group1", SampleTypeHam, SampleOriginPreset))
	})

	t.Run("input with empty lines", func(t *testing.T) {
		_, s, teardown := prep()
		defer teardown()

		input := strings.NewReader("sample1\n\n\nsample2\n\n")
		stats, err := s.Import(ctx, "group1", SampleTypeHam, SampleOriginPreset, input, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		samples, err := s.Read(ctx, "group1", SampleTypeHam, SampleOriginPreset)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"sample1", "sample2"}, samples)
	})

	t.Run("nil reader", func(t *testing.T) {
		_, s, teardown := prep()
		defer teardown()

		_, err := s.Import(ctx, "group1", SampleTypeHam, SampleOriginPreset, nil, true)
		assert.Error(t, err)
	})

	t.Run("reader error", func(t *testing.T) {
		_, s, teardown := prep()
		defer teardown()

		errReader := &errorReader{err: fmt.Errorf("read error")}
		_, err := s.Import(ctx, "group1", SampleTypeHam, SampleOriginPreset, errReader, true)
		assert.Error(t, err)
	})

	t.Run("duplicate message different type", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		// import ham samples
		inputHam := strings.NewReader("message1\nmessage2")
		_, err := s.Import(ctx, "group1", SampleTypeHam, SampleOriginUser, inputHam, true)
		require.NoError(t, err)

		// import spam samples with same messages
		inputSpam := strings.NewReader("message1\nmessage2\nmessage3")
		stats, err := s.Import(ctx, "group1", SampleTypeSpam, SampleOriginUser, inputSpam, false)
		require.NoError(t, err)
		require.NotNil(t, stats)

		// verify only new messages are added, duplicates replaced
		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM samples WHERE gid = ?", "group1")
		require.NoError(t, err)
		assert.Equal(t, 3, count)

		// verify type is updated for duplicates
		var spamCount int
		err = db.Get(&spamCount, "SELECT COUNT(*) FROM samples WHERE gid = ? AND type = ?",
			"group1", SampleTypeSpam)
		require.NoError(t, err)
		assert.Equal(t, 3, spamCount)
	})

	t.Run("duplicate message within import", func(t *testing.T) {
		db, s, teardown := prep()
		defer teardown()

		// import with duplicate messages
		input := strings.NewReader("message1\nmessage2\nmessage1")
		stats, err := s.Import(ctx, "group1", SampleTypeHam, SampleOriginUser, input, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		// verify only unique messages are stored
		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM samples WHERE gid = ?", "group1")
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
				require.NoError(t, s.Add(context.Background(), "gr1", SampleTypeHam, SampleOriginPreset, "test1"))
				time.Sleep(time.Second) // ensure each message has a unique timestamp
				require.NoError(t, s.Add(context.Background(), "gr1", SampleTypeHam, SampleOriginPreset, "test2"))
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
			db, teardown := setupTestDB(t)
			defer teardown()

			s, err := NewSamples(context.Background(), db)
			require.NoError(t, err)

			if tt.setup != nil {
				tt.setup(s)
			}

			r, err := s.Reader(context.Background(), "gr1", tt.sampleType, tt.origin)
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
