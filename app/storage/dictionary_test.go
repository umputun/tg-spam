package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDictionary(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	t.Run("valid db connection", func(t *testing.T) {
		d, err := NewDictionary(context.Background(), db)
		assert.NoError(t, err)
		assert.NotNil(t, d)
	})

	t.Run("nil db connection", func(t *testing.T) {
		d, err := NewDictionary(context.Background(), nil)
		assert.Error(t, err)
		assert.Nil(t, d)
	})
}

func TestDictionary_AddPhrase(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	d, err := NewDictionary(context.Background(), db)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("valid stop phrase with gid", func(t *testing.T) {
		err := d.Add(ctx, "group1", DictionaryTypeStopPhrase, "test stop phrase")
		assert.NoError(t, err)
	})

	t.Run("valid ignored word with gid", func(t *testing.T) {
		err := d.Add(ctx, "group1", DictionaryTypeIgnoredWord, "testword")
		assert.NoError(t, err)
	})

	t.Run("invalid type", func(t *testing.T) {
		err := d.Add(ctx, "invalid", "test phrase", "group1")
		assert.Error(t, err)
	})

	t.Run("empty phrase", func(t *testing.T) {
		err := d.Add(ctx, "group1", DictionaryTypeStopPhrase, "")
		assert.Error(t, err)
	})

	t.Run("same phrase different gid", func(t *testing.T) {
		phrase := "duplicate phrase"
		err := d.Add(ctx, "group1", DictionaryTypeStopPhrase, phrase)
		require.NoError(t, err)
		err = d.Add(ctx, "group2", DictionaryTypeStopPhrase, phrase)
		assert.NoError(t, err)
	})
}

func TestDictionary_Delete(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	d, err := NewDictionary(context.Background(), db)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("delete existing phrase", func(t *testing.T) {
		err := d.Add(ctx, "group1", DictionaryTypeStopPhrase, "test phrase")
		require.NoError(t, err)

		var id int64
		err = db.Get(&id, "SELECT id FROM dictionary WHERE data = ? AND gid = ?", "test phrase", "group1")
		require.NoError(t, err)

		err = d.Delete(ctx, id)
		assert.NoError(t, err)
	})

	t.Run("delete non-existent phrase", func(t *testing.T) {
		err := d.Delete(ctx, 99999)
		assert.Error(t, err)
	})
}

func TestDictionary_Read(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	d, err := NewDictionary(context.Background(), db)
	require.NoError(t, err)

	ctx := context.Background()

	// setup test data
	err = d.Add(ctx, "group1", DictionaryTypeStopPhrase, "stop1")
	require.NoError(t, err)
	err = d.Add(ctx, "group1", DictionaryTypeStopPhrase, "stop2")
	require.NoError(t, err)
	err = d.Add(ctx, "group1", DictionaryTypeIgnoredWord, "ignored1")
	require.NoError(t, err)
	err = d.Add(ctx, "group2", DictionaryTypeStopPhrase, "stop3")
	require.NoError(t, err)

	t.Run("read stop phrases group1", func(t *testing.T) {
		phrases, err := d.Read(ctx, "group1", DictionaryTypeStopPhrase)
		assert.NoError(t, err)
		assert.Len(t, phrases, 2)
		assert.Contains(t, phrases, "stop1")
		assert.Contains(t, phrases, "stop2")
	})

	t.Run("read ignored words group1", func(t *testing.T) {
		phrases, err := d.Read(ctx, "group1", DictionaryTypeIgnoredWord)
		assert.NoError(t, err)
		assert.Len(t, phrases, 1)
		assert.Contains(t, phrases, "ignored1")
	})

	t.Run("read non-existent group", func(t *testing.T) {
		phrases, err := d.Read(ctx, "nonexistent", DictionaryTypeStopPhrase)
		assert.NoError(t, err)
		assert.Empty(t, phrases)
	})

	t.Run("read with invalid type", func(t *testing.T) {
		phrases, err := d.Read(ctx, "invalid", "group1")
		assert.Error(t, err)
		assert.Nil(t, phrases)
	})
}

func TestDictionary_Iterator(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	d, err := NewDictionary(context.Background(), db)
	require.NoError(t, err)

	ctx := context.Background()

	// setup test data
	err = d.Add(ctx, "group1", DictionaryTypeStopPhrase, "stop1")
	require.NoError(t, err)
	err = d.Add(ctx, "group1", DictionaryTypeStopPhrase, "stop2")
	require.NoError(t, err)
	err = d.Add(ctx, "group2", DictionaryTypeStopPhrase, "stop3")
	require.NoError(t, err)

	t.Run("iterate group1", func(t *testing.T) {
		iter, err := d.Iterator(ctx, "group1", DictionaryTypeStopPhrase)
		require.NoError(t, err)

		var phrases []string
		for phrase := range iter {
			phrases = append(phrases, phrase)
		}
		assert.Len(t, phrases, 2)
		assert.Contains(t, phrases, "stop1")
		assert.Contains(t, phrases, "stop2")
	})

	t.Run("iterate empty group", func(t *testing.T) {
		iter, err := d.Iterator(ctx, "nonexistent", DictionaryTypeStopPhrase)
		require.NoError(t, err)

		var phrases []string
		for phrase := range iter {
			phrases = append(phrases, phrase)
		}
		assert.Empty(t, phrases)
	})

	t.Run("iterate with invalid type", func(t *testing.T) {
		iter, err := d.Iterator(ctx, "invalid", "group1")
		assert.Error(t, err)
		assert.Nil(t, iter)
	})
}

func TestDictionary_Stats(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	d, err := NewDictionary(context.Background(), db)
	require.NoError(t, err)

	ctx := context.Background()

	// setup test data
	err = d.Add(ctx, "group1", DictionaryTypeStopPhrase, "stop1")
	require.NoError(t, err)
	err = d.Add(ctx, "group1", DictionaryTypeStopPhrase, "stop2")
	require.NoError(t, err)
	err = d.Add(ctx, "group2", DictionaryTypeStopPhrase, "stop3")
	require.NoError(t, err)
	err = d.Add(ctx, "group1", DictionaryTypeIgnoredWord, "ignored1")
	require.NoError(t, err)
	err = d.Add(ctx, "group2", DictionaryTypeIgnoredWord, "ignored2")
	require.NoError(t, err)

	t.Run("stats for group1", func(t *testing.T) {
		stats, err := d.Stats(ctx, "group1")
		require.NoError(t, err)
		require.NotNil(t, stats)
		assert.Equal(t, 2, stats.TotalStopPhrases)
		assert.Equal(t, 1, stats.TotalIgnoredWords)
	})

	t.Run("stats for group2", func(t *testing.T) {
		stats, err := d.Stats(ctx, "group2")
		require.NoError(t, err)
		require.NotNil(t, stats)
		assert.Equal(t, 1, stats.TotalStopPhrases)
		assert.Equal(t, 1, stats.TotalIgnoredWords)
	})

	t.Run("stats for non-existent group", func(t *testing.T) {
		stats, err := d.Stats(ctx, "nonexistent")
		require.NoError(t, err)
		require.NotNil(t, stats)
		assert.Equal(t, 0, stats.TotalStopPhrases)
		assert.Equal(t, 0, stats.TotalIgnoredWords)
	})
}

func TestDictionary_Import(t *testing.T) {
	ctx := context.Background()

	t.Run("import with cleanup", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		d, err := NewDictionary(context.Background(), db)
		require.NoError(t, err)

		input := strings.NewReader(`phrase1,phrase2,"phrase 3, with comma"`)
		stats, err := d.Import(ctx, "group1", DictionaryTypeStopPhrase, input, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		phrases, err := d.Read(ctx, "group1", DictionaryTypeStopPhrase)
		require.NoError(t, err)
		assert.Equal(t, 3, len(phrases))
		assert.Contains(t, phrases, "phrase1")
		assert.Contains(t, phrases, "phrase2")
		assert.Contains(t, phrases, "phrase 3, with comma")
	})

	t.Run("import without cleanup", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		d, err := NewDictionary(context.Background(), db)
		require.NoError(t, err)

		input1 := strings.NewReader(`existing1,"existing 2"`)
		_, err = d.Import(ctx, "group1", DictionaryTypeIgnoredWord, input1, true)
		require.NoError(t, err)

		input2 := strings.NewReader(`new1,new2`)
		stats, err := d.Import(ctx, "group1", DictionaryTypeIgnoredWord, input2, false)
		require.NoError(t, err)
		require.NotNil(t, stats)

		phrases, err := d.Read(ctx, "group1", DictionaryTypeIgnoredWord)
		require.NoError(t, err)
		assert.Equal(t, 4, len(phrases))
	})

	t.Run("import to different gids", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		d, err := NewDictionary(context.Background(), db)
		require.NoError(t, err)

		// first, add some initial data to both groups
		input0 := strings.NewReader(`old1,old2`)
		_, err = d.Import(ctx, "group1", DictionaryTypeStopPhrase, input0, true)
		require.NoError(t, err)
		_, err = d.Import(ctx, "group2", DictionaryTypeStopPhrase, input0, true)
		require.NoError(t, err)

		// verify initial state
		phrases0, err := d.Read(ctx, "group1", DictionaryTypeStopPhrase)
		require.NoError(t, err)
		assert.Equal(t, 2, len(phrases0), "initial group1 count")

		// import new data with cleanup
		input1 := strings.NewReader(`phrase1,phrase2`)
		_, err = d.Import(ctx, "group1", DictionaryTypeStopPhrase, input1, true)
		require.NoError(t, err)

		input2 := strings.NewReader(`phrase3,phrase4`)
		_, err = d.Import(ctx, "group2", DictionaryTypeStopPhrase, input2, true)
		require.NoError(t, err)

		// check direct database content for debugging
		var count1, count2 int
		err = db.Get(&count1, "SELECT COUNT(*) FROM dictionary WHERE type = ? AND gid = ?",
			DictionaryTypeStopPhrase, "group1")
		require.NoError(t, err)
		err = db.Get(&count2, "SELECT COUNT(*) FROM dictionary WHERE type = ? AND gid = ?",
			DictionaryTypeStopPhrase, "group2")
		require.NoError(t, err)

		t.Logf("DB counts - group1: %d, group2: %d", count1, count2)

		// verify final state
		phrases1, err := d.Read(ctx, "group1", DictionaryTypeStopPhrase)
		require.NoError(t, err)
		assert.Equal(t, 2, len(phrases1), "should have 2 phrases in group1")
		assert.ElementsMatch(t, []string{"phrase1", "phrase2"}, phrases1, "group1 content mismatch")

		phrases2, err := d.Read(ctx, "group2", DictionaryTypeStopPhrase)
		require.NoError(t, err)
		assert.Equal(t, 2, len(phrases2), "should have 2 phrases in group2")
		assert.ElementsMatch(t, []string{"phrase3", "phrase4"}, phrases2, "group2 content mismatch")
	})

	t.Run("import with invalid input", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		d, err := NewDictionary(context.Background(), db)
		require.NoError(t, err)

		input := strings.NewReader(`this is "bad csv`)
		_, err = d.Import(ctx, "group1", DictionaryTypeStopPhrase, input, true)
		assert.Error(t, err)
	})
}

func TestDictionary_ReadPhrases(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	d, err := NewDictionary(context.Background(), db)
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
		err := d.Add(ctx, "group1", td.dType, td.phrase)
		require.NoError(t, err)
	}
	// add a phrase to a different group
	err = d.Add(ctx, "group2", DictionaryTypeStopPhrase, "stop phrase 3")
	assert.NoError(t, err)

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
			phrases, err := d.Read(ctx, "group1", tt.dType)
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
	d, err := NewDictionary(context.Background(), db)
	require.NoError(t, err)
	require.NotNil(t, d)

	// Verify table exists and is accessible
	ctx := context.Background()
	err = d.Add(ctx, "group1", DictionaryTypeStopPhrase, "test phrase")
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
				if _, err := d.Read(ctx, "group1", DictionaryTypeStopPhrase); err != nil {
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
				if err := d.Add(ctx, "group1", dType, phrase); err != nil {
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
	stats, err := d.Stats(ctx, "group1")
	require.NoError(t, err)
	require.NotNil(t, stats)

	expectedTotal := numWorkers*numOps + 1 // +1 for the initial test phrase
	actualTotal := stats.TotalStopPhrases + stats.TotalIgnoredWords
	assert.Equal(t, expectedTotal, actualTotal, "expected %d total phrases, got %d", expectedTotal, actualTotal)
}

func TestDictionary_Reader(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*Dictionary)
		dType   DictionaryType
		want    []string
		wantErr bool
	}{
		{
			name: "stop phrases",
			setup: func(d *Dictionary) {
				require.NoError(t, d.Add(context.Background(), "group1", DictionaryTypeStopPhrase, "test1"))
				require.NoError(t, d.Add(context.Background(), "group1", DictionaryTypeStopPhrase, "test2"))
				require.NoError(t, d.Add(context.Background(), "group2", DictionaryTypeStopPhrase, "test3"))
			},
			dType:   DictionaryTypeStopPhrase,
			want:    []string{"test1", "test2"},
			wantErr: false,
		},
		{
			name: "empty result",
			setup: func(d *Dictionary) {
				// no setup needed
			},
			dType:   DictionaryTypeIgnoredWord,
			want:    []string{},
			wantErr: false,
		},
		{
			name:    "invalid type",
			setup:   nil,
			dType:   "invalid",
			want:    nil,
			wantErr: true,
		},
		{
			name: "mixed entries",
			setup: func(d *Dictionary) {
				require.NoError(t, d.Add(context.Background(), "group1", DictionaryTypeIgnoredWord, "ignored1"))
				require.NoError(t, d.Add(context.Background(), "group1", DictionaryTypeStopPhrase, "stop1"))
				require.NoError(t, d.Add(context.Background(), "group1", DictionaryTypeIgnoredWord, "ignored2"))
			},
			dType:   DictionaryTypeIgnoredWord,
			want:    []string{"ignored1", "ignored2"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, teardown := setupTestDB(t)
			defer teardown()

			d, err := NewDictionary(context.Background(), db)
			require.NoError(t, err)

			if tt.setup != nil {
				tt.setup(d)
			}

			r, err := d.Reader(context.Background(), "group1", tt.dType)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			defer r.Close()

			data, err := io.ReadAll(r)
			require.NoError(t, err)

			if len(tt.want) == 0 {
				assert.Empty(t, string(data))
				return
			}

			got := strings.Split(string(data), "\n")
			assert.Equal(t, tt.want, got)
		})
	}
}
