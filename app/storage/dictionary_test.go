package storage

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDictionary(t *testing.T) {
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
			d, err := NewDictionary(tt.db)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, d)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, d)
			}
		})
	}
}

func TestDictionary_AddPhrase(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	d, err := NewDictionary(db)
	require.NoError(t, err)
	require.NotNil(t, d)

	ctx := context.Background()

	tests := []struct {
		name    string
		dType   DictionaryType
		phrase  string
		wantErr bool
	}{
		{
			name:    "valid stop phrase",
			dType:   DictionaryTypeStopPhrase,
			phrase:  "test stop phrase",
			wantErr: false,
		},
		{
			name:    "valid ignored word",
			dType:   DictionaryTypeIgnoredWord,
			phrase:  "testword",
			wantErr: false,
		},
		{
			name:    "invalid type",
			dType:   "invalid",
			phrase:  "test phrase",
			wantErr: true,
		},
		{
			name:    "empty phrase",
			dType:   DictionaryTypeStopPhrase,
			phrase:  "",
			wantErr: true,
		},
		{
			name:    "duplicate phrase",
			dType:   DictionaryTypeStopPhrase,
			phrase:  "test stop phrase", // Same as first test case
			wantErr: false,              // Should silently succeed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := d.Add(ctx, tt.dType, tt.phrase)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDictionary_Delete(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	d, err := NewDictionary(db)
	require.NoError(t, err)

	ctx := context.Background()

	// Add a test phrase first
	err = d.Add(ctx, DictionaryTypeStopPhrase, "test phrase")
	require.NoError(t, err)

	// Get the ID of the inserted phrase
	var id int64
	err = db.Get(&id, "SELECT id FROM dictionary WHERE data = ?", "test phrase")
	require.NoError(t, err)

	tests := []struct {
		name    string
		id      int64
		wantErr bool
	}{
		{
			name:    "existing phrase",
			id:      id,
			wantErr: false,
		},
		{
			name:    "non-existent phrase",
			id:      99999,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := d.Delete(ctx, tt.id)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDictionary_ReadPhrases(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	d, err := NewDictionary(db)
	require.NoError(t, err)

	ctx := context.Background()

	// Add test phrases
	testData := []struct {
		dType  DictionaryType
		phrase string
	}{
		{DictionaryTypeStopPhrase, "stop phrase 1"},
		{DictionaryTypeStopPhrase, "stop phrase 2"},
		{DictionaryTypeIgnoredWord, "ignored1"},
		{DictionaryTypeIgnoredWord, "ignored2"},
	}

	for _, td := range testData {
		err := d.Add(ctx, td.dType, td.phrase)
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		dType         DictionaryType
		expectedCount int
		wantErr       bool
	}{
		{
			name:          "all stop phrases",
			dType:         DictionaryTypeStopPhrase,
			expectedCount: 2,
			wantErr:       false,
		},
		{
			name:          "all ignored words",
			dType:         DictionaryTypeIgnoredWord,
			expectedCount: 2,
			wantErr:       false,
		},
		{
			name:          "invalid type",
			dType:         "invalid",
			expectedCount: 0,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phrases, err := d.Read(ctx, tt.dType)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, phrases)
			} else {
				assert.NoError(t, err)
				assert.Len(t, phrases, tt.expectedCount)
			}
		})
	}
}

func TestDictionary_Iterator(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	d, err := NewDictionary(db)
	require.NoError(t, err)

	ctx := context.Background()

	// Add test phrases
	testData := []struct {
		dType  DictionaryType
		phrase string
	}{
		{DictionaryTypeStopPhrase, "stop phrase 1"},
		{DictionaryTypeStopPhrase, "stop phrase 2"},
		{DictionaryTypeIgnoredWord, "ignored1"},
		{DictionaryTypeIgnoredWord, "ignored2"},
	}

	for _, td := range testData {
		err := d.Add(ctx, td.dType, td.phrase)
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		dType         DictionaryType
		expectedCount int
		wantErr       bool
	}{
		{
			name:          "iterate stop phrases",
			dType:         DictionaryTypeStopPhrase,
			expectedCount: 2,
			wantErr:       false,
		},
		{
			name:          "iterate ignored words",
			dType:         DictionaryTypeIgnoredWord,
			expectedCount: 2,
			wantErr:       false,
		},
		{
			name:          "invalid type",
			dType:         "invalid",
			expectedCount: 0,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			iter, err := d.Iterator(ctx, tt.dType)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			var phrases []string
			for phrase := range iter {
				phrases = append(phrases, phrase)
			}
			assert.Len(t, phrases, tt.expectedCount)
		})
	}
}

func TestDictionary_Stats(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	d, err := NewDictionary(db)
	require.NoError(t, err)

	ctx := context.Background()

	// Add test phrases
	testData := []struct {
		dType  DictionaryType
		phrase string
	}{
		{DictionaryTypeStopPhrase, "stop phrase 1"},
		{DictionaryTypeStopPhrase, "stop phrase 2"},
		{DictionaryTypeStopPhrase, "stop phrase 3"},
		{DictionaryTypeIgnoredWord, "ignored1"},
		{DictionaryTypeIgnoredWord, "ignored2"},
	}

	for _, td := range testData {
		err := d.Add(ctx, td.dType, td.phrase)
		require.NoError(t, err)
	}

	stats, err := d.Stats(ctx)
	require.NoError(t, err)
	require.NotNil(t, stats)

	assert.Equal(t, 3, stats.TotalStopPhrases)
	assert.Equal(t, 2, stats.TotalIgnoredWords)
}

func TestDictionaryType_Validate(t *testing.T) {
	tests := []struct {
		name    string
		dType   DictionaryType
		wantErr bool
	}{
		{"valid stop phrase", DictionaryTypeStopPhrase, false},
		{"valid ignored word", DictionaryTypeIgnoredWord, false},
		{"invalid type", "invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.dType.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDictionary_Concurrent(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	require.NotNil(t, db)

	// Initialize dictionary with schema
	d, err := NewDictionary(db)
	require.NoError(t, err)
	require.NotNil(t, d)

	// Verify table exists and is accessible
	ctx := context.Background()
	err = d.Add(ctx, DictionaryTypeStopPhrase, "test phrase")
	require.NoError(t, err, "Failed to insert initial test phrase")

	const numWorkers = 10
	const numOps = 50

	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*2)

	// Start readers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				if _, err := d.Read(ctx, DictionaryTypeStopPhrase); err != nil {
					select {
					case errCh <- fmt.Errorf("reader %d failed: %w", workerID, err):
					default:
					}
					return
				}
			}
		}(i)
	}

	// Start writers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				phrase := fmt.Sprintf("test phrase %d-%d", workerID, j)
				dType := DictionaryTypeStopPhrase
				if j%2 == 0 {
					dType = DictionaryTypeIgnoredWord
				}
				if err := d.Add(ctx, dType, phrase); err != nil {
					select {
					case errCh <- fmt.Errorf("writer %d failed: %w", workerID, err):
					default:
					}
					return
				}
			}
		}(i)
	}

	// Wait for all goroutines to finish
	wg.Wait()
	close(errCh)

	// Check for any errors
	for err := range errCh {
		t.Errorf("concurrent operation failed: %v", err)
	}

	// Verify the final state
	stats, err := d.Stats(ctx)
	require.NoError(t, err)
	require.NotNil(t, stats)

	expectedTotal := numWorkers*numOps + 1 // +1 for the initial test phrase
	actualTotal := stats.TotalStopPhrases + stats.TotalIgnoredWords
	assert.Equal(t, expectedTotal, actualTotal, "expected %d total phrases, got %d", expectedTotal, actualTotal)
}
