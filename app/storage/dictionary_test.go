package storage

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/storage/engine"
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
		err := d.Add(ctx, DictionaryTypeStopPhrase, "test stop phrase")
		assert.NoError(t, err)
	})

	t.Run("valid ignored word with gid", func(t *testing.T) {
		err := d.Add(ctx, DictionaryTypeIgnoredWord, "testword")
		assert.NoError(t, err)
	})

	t.Run("invalid type", func(t *testing.T) {
		err := d.Add(ctx, "invalid", "test phrase")
		assert.Error(t, err)
	})

	t.Run("empty phrase", func(t *testing.T) {
		err := d.Add(ctx, DictionaryTypeStopPhrase, "")
		assert.Error(t, err)
	})
}

func TestDictionary_Delete(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()
	d, err := NewDictionary(context.Background(), db)
	require.NoError(t, err)

	ctx := context.Background()

	t.Run("delete existing phrase", func(t *testing.T) {
		err := d.Add(ctx, DictionaryTypeStopPhrase, "test phrase")
		require.NoError(t, err)

		var id int64
		err = db.Get(&id, "SELECT id FROM dictionary WHERE data = ? AND gid = ?", "test phrase", "gr1")
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
	err = d.Add(ctx, DictionaryTypeStopPhrase, "stop1")
	require.NoError(t, err)
	err = d.Add(ctx, DictionaryTypeStopPhrase, "stop2")
	require.NoError(t, err)
	err = d.Add(ctx, DictionaryTypeIgnoredWord, "ignored1")
	require.NoError(t, err)

	t.Run("read stop phrases gr1", func(t *testing.T) {
		phrases, err := d.Read(ctx, DictionaryTypeStopPhrase)
		assert.NoError(t, err)
		assert.Len(t, phrases, 2)
		assert.Contains(t, phrases, "stop1")
		assert.Contains(t, phrases, "stop2")
	})

	t.Run("read ignored words group1", func(t *testing.T) {
		phrases, err := d.Read(ctx, DictionaryTypeIgnoredWord)
		assert.NoError(t, err)
		assert.Len(t, phrases, 1)
		assert.Contains(t, phrases, "ignored1")
	})

	t.Run("read with invalid type", func(t *testing.T) {
		phrases, err := d.Read(ctx, "invalid")
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
	err = d.Add(ctx, DictionaryTypeStopPhrase, "stop1")
	require.NoError(t, err)
	err = d.Add(ctx, DictionaryTypeStopPhrase, "stop2")
	require.NoError(t, err)
	err = d.Add(ctx, DictionaryTypeStopPhrase, "stop3")
	require.NoError(t, err)

	t.Run("iterate group", func(t *testing.T) {
		iter, err := d.Iterator(ctx, DictionaryTypeStopPhrase)
		require.NoError(t, err)

		var phrases []string
		for phrase := range iter {
			phrases = append(phrases, phrase)
		}
		assert.Len(t, phrases, 3)
		assert.Contains(t, phrases, "stop1")
		assert.Contains(t, phrases, "stop2")
		assert.Contains(t, phrases, "stop2")
	})

	t.Run("iterate with invalid type", func(t *testing.T) {
		iter, err := d.Iterator(ctx, "invalid")
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
	err = d.Add(ctx, DictionaryTypeStopPhrase, "stop1")
	require.NoError(t, err)
	err = d.Add(ctx, DictionaryTypeStopPhrase, "stop2")
	require.NoError(t, err)
	err = d.Add(ctx, DictionaryTypeStopPhrase, "stop3")
	require.NoError(t, err)
	err = d.Add(ctx, DictionaryTypeIgnoredWord, "ignored1")
	require.NoError(t, err)
	err = d.Add(ctx, DictionaryTypeIgnoredWord, "ignored2")
	require.NoError(t, err)

	stats, err := d.Stats(ctx)
	require.NoError(t, err)
	require.NotNil(t, stats)
	assert.Equal(t, 3, stats.TotalStopPhrases)
	assert.Equal(t, 2, stats.TotalIgnoredWords)
}

func TestDictionary_Import(t *testing.T) {
	ctx := context.Background()

	t.Run("import with cleanup", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		d, err := NewDictionary(context.Background(), db)
		require.NoError(t, err)

		input := strings.NewReader(`phrase1,phrase2,"phrase 3, with comma"`)
		stats, err := d.Import(ctx, DictionaryTypeStopPhrase, input, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		phrases, err := d.Read(ctx, DictionaryTypeStopPhrase)
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
		_, err = d.Import(ctx, DictionaryTypeIgnoredWord, input1, true)
		require.NoError(t, err)

		input2 := strings.NewReader(`new1,new2`)
		stats, err := d.Import(ctx, DictionaryTypeIgnoredWord, input2, false)
		require.NoError(t, err)
		require.NotNil(t, stats)

		phrases, err := d.Read(ctx, DictionaryTypeIgnoredWord)
		require.NoError(t, err)
		assert.Equal(t, 4, len(phrases))
	})

	t.Run("import with invalid input", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()
		d, err := NewDictionary(context.Background(), db)
		require.NoError(t, err)

		input := strings.NewReader(`this is "bad csv`)
		_, err = d.Import(ctx, DictionaryTypeStopPhrase, input, true)
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
				require.NoError(t, d.Add(context.Background(), DictionaryTypeStopPhrase, "test1"))
				require.NoError(t, d.Add(context.Background(), DictionaryTypeStopPhrase, "test2"))
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
				require.NoError(t, d.Add(context.Background(), DictionaryTypeIgnoredWord, "ignored1"))
				require.NoError(t, d.Add(context.Background(), DictionaryTypeStopPhrase, "stop1"))
				require.NoError(t, d.Add(context.Background(), DictionaryTypeIgnoredWord, "ignored2"))
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

			r, err := d.Reader(context.Background(), tt.dType)
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

func TestDictionary_ImportEdgeCases(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	d, err := NewDictionary(ctx, db)
	require.NoError(t, err)

	t.Run("import with quoted strings and special chars", func(t *testing.T) {
		input := strings.NewReader(`"phrase with, comma",simple phrase,"quoted ""special"" chars"`)
		stats, err := d.Import(ctx, DictionaryTypeStopPhrase, input, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		phrases, err := d.Read(ctx, DictionaryTypeStopPhrase)
		require.NoError(t, err)
		require.Len(t, phrases, 3)
		assert.Contains(t, phrases, `phrase with, comma`)
		assert.Contains(t, phrases, `simple phrase`)
		assert.Contains(t, phrases, `quoted "special" chars`)
	})

	t.Run("import duplicate phrases", func(t *testing.T) {
		input := strings.NewReader("phrase1,phrase1,phrase1")
		stats, err := d.Import(ctx, DictionaryTypeStopPhrase, input, true)
		require.NoError(t, err)
		require.NotNil(t, stats)

		phrases, err := d.Read(ctx, DictionaryTypeStopPhrase)
		require.NoError(t, err)
		assert.Len(t, phrases, 1)
	})
}

func TestDictionary_StrictGroupIsolation(t *testing.T) {
	db1, err := engine.NewSqlite(":memory:", "gr1")
	require.NoError(t, err)
	defer db1.Close()

	db2, err := engine.NewSqlite(":memory:", "gr2")
	require.NoError(t, err)
	defer db2.Close()

	ctx := context.Background()
	d1, err := NewDictionary(ctx, db1)
	require.NoError(t, err)

	d2, err := NewDictionary(ctx, db2)
	require.NoError(t, err)

	// add same phrases to both dictionaries
	commonPhrases := []struct {
		text  string
		dType DictionaryType
	}{
		{"stop phrase", DictionaryTypeStopPhrase},
		{"ignored word", DictionaryTypeIgnoredWord},
	}

	for _, p := range commonPhrases {
		require.NoError(t, d1.Add(ctx, p.dType, p.text))
		require.NoError(t, d2.Add(ctx, p.dType, p.text))
	}

	// verify each dictionary only sees its own entries
	verifyDictionary := func(t *testing.T, d *Dictionary, db *engine.SQL) {
		for _, p := range commonPhrases {
			phrases, err := d.Read(ctx, p.dType)
			require.NoError(t, err)
			require.Len(t, phrases, 1)
			assert.Equal(t, p.text, phrases[0])
		}

		// verify data directly in DB
		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM dictionary WHERE gid = ?", db.GID())
		require.NoError(t, err)
		assert.Equal(t, len(commonPhrases), count)
	}

	t.Run("verify gr1", func(t *testing.T) {
		verifyDictionary(t, d1, db1)
	})

	t.Run("verify gr2", func(t *testing.T) {
		verifyDictionary(t, d2, db2)
	})

	// delete from one dictionary shouldn't affect other
	t.Run("verify deletion isolation", func(t *testing.T) {
		var id int64
		err := db1.Get(&id, "SELECT id FROM dictionary WHERE gid = ? LIMIT 1", "gr1")
		require.NoError(t, err)

		require.NoError(t, d1.Delete(ctx, id))

		// verify gr1 has one less entry
		var count int
		err = db1.Get(&count, "SELECT COUNT(*) FROM dictionary WHERE gid = ?", "gr1")
		require.NoError(t, err)
		assert.Equal(t, len(commonPhrases)-1, count)

		// verify gr2 still has all entries
		err = db2.Get(&count, "SELECT COUNT(*) FROM dictionary WHERE gid = ?", "gr2")
		require.NoError(t, err)
		assert.Equal(t, len(commonPhrases), count)
	})
}

func TestDictionary_ReaderErrorHandling(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	d, err := NewDictionary(ctx, db)
	require.NoError(t, err)

	t.Run("read with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r, err := d.Reader(ctx, DictionaryTypeStopPhrase)
		assert.Error(t, err)
		assert.Nil(t, r)
	})

	t.Run("read after close", func(t *testing.T) {
		r, err := d.Reader(ctx, DictionaryTypeStopPhrase)
		require.NoError(t, err)
		require.NoError(t, r.Close())

		// second close should not error
		assert.NoError(t, r.Close())
	})

	t.Run("concurrent reads and close", func(t *testing.T) {
		phrase := "test phrase"
		require.NoError(t, d.Add(ctx, DictionaryTypeStopPhrase, phrase))

		r, err := d.Reader(ctx, DictionaryTypeStopPhrase)
		require.NoError(t, err)

		var wg sync.WaitGroup
		errs := make(chan error, 2)

		// start reader
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 1024)
			for {
				_, err := r.Read(buf)
				if err == io.EOF {
					return
				}
				if err != nil {
					errs <- err
					return
				}
			}
		}()

		// close while reading
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Millisecond) // give reader a chance to start
			if err := r.Close(); err != nil {
				errs <- err
			}
		}()

		wg.Wait()
		close(errs)

		for err := range errs {
			assert.NoError(t, err)
		}
	})
}

func TestDictionary_ComplexConcurrentOperations(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	d, err := NewDictionary(ctx, db)
	require.NoError(t, err)

	const numWorkers = 10
	const opsPerWorker = 100

	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*3) // for read, write and delete operations

	// prepare some initial data
	for i := 0; i < 10; i++ {
		require.NoError(t, d.Add(ctx, DictionaryTypeStopPhrase, fmt.Sprintf("initial phrase %d", i)))
	}

	// concurrent reads, writes and deletes
	for i := 0; i < numWorkers; i++ {
		// writer
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				phrase := fmt.Sprintf("phrase %d-%d", workerID, j)
				if err := d.Add(ctx, DictionaryTypeStopPhrase, phrase); err != nil {
					errCh <- fmt.Errorf("write failed: %w", err)
					return
				}
			}
		}(i)

		// reader
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				if _, err := d.Read(ctx, DictionaryTypeStopPhrase); err != nil {
					errCh <- fmt.Errorf("read failed: %w", err)
					return
				}
			}
		}(i)

		// iterator
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < opsPerWorker/10; j++ { // fewer iterations for iterator
				iter, err := d.Iterator(ctx, DictionaryTypeStopPhrase)
				if err != nil {
					errCh <- fmt.Errorf("iterator creation failed: %w", err)
					return
				}
				count := 0
				for range iter {
					count++
				}
				if count == 0 {
					errCh <- fmt.Errorf("iterator returned no results")
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	var errors []error
	for err := range errCh {
		errors = append(errors, err)
	}
	assert.Empty(t, errors)

	// verify final state
	phrases, err := d.Read(ctx, DictionaryTypeStopPhrase)
	require.NoError(t, err)
	assert.NotEmpty(t, phrases)

	stats, err := d.Stats(ctx)
	require.NoError(t, err)
	assert.NotZero(t, stats.TotalStopPhrases)
}
