package storage

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			s, err := NewSamples(tt.db)
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
	s, err := NewSamples(db)
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
			name:    "duplicate message",
			sType:   SampleTypeHam,
			origin:  SampleOriginPreset,
			message: "test ham message", // Same as first test case
			wantErr: false,              // Should silently succeed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.AddSample(ctx, tt.sType, tt.origin, tt.message)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSamples_RemoveSample(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	s, err := NewSamples(db)
	require.NoError(t, err)

	ctx := context.Background()

	// add a sample first
	err = s.AddSample(ctx, SampleTypeHam, SampleOriginPreset, "test message")
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
			err := s.RemoveSample(ctx, tt.id)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSamples_ReadSamples(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	s, err := NewSamples(db)
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
		err := s.AddSample(ctx, td.sType, td.origin, td.message)
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
			samples, err := s.ReadSamples(ctx, tt.sType, tt.origin)
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
	s, err := NewSamples(db)
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
		err := s.AddSample(ctx, td.sType, td.origin, td.message)
		require.NoError(t, err)
	}

	stats, err := s.GetStats(ctx)
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
	s, err := NewSamples(db)
	require.NoError(t, err)
	require.NotNil(t, s)

	// Verify table exists and is accessible
	ctx := context.Background()
	err = s.AddSample(ctx, SampleTypeHam, SampleOriginPreset, "test message")
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
				if _, err := s.ReadSamples(ctx, SampleTypeHam, SampleOriginAny); err != nil {
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
				if err := s.AddSample(ctx, sType, SampleOriginUser, msg); err != nil {
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
	stats, err := s.GetStats(ctx)
	require.NoError(t, err)
	require.NotNil(t, stats)

	expectedTotal := numWorkers*numOps + 1 // +1 for the initial test message
	actualTotal := stats.TotalHam + stats.TotalSpam
	require.Equal(t, expectedTotal, actualTotal, "expected %d total samples, got %d", expectedTotal, actualTotal)
}
