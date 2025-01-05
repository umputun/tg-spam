package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestDetectedSpam_NewDetectedSpam(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	_, err := NewDetectedSpam(context.Background(), db)
	require.NoError(t, err)

	var exists int
	err = db.Get(&exists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='detected_spam'")
	require.NoError(t, err)
	assert.Equal(t, 1, exists)
}

func TestDetectedSpam_Write(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()

	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	spamEntry := DetectedSpamInfo{
		Text:      "spam message",
		UserID:    1,
		UserName:  "Spammer",
		Timestamp: time.Now(),
		GID:       "group123",
	}

	checks := []spamcheck.Response{
		{
			Name:    "Check1",
			Spam:    true,
			Details: "Details 1",
		},
	}

	err = ds.Write(ctx, spamEntry, checks)
	require.NoError(t, err)

	var count int
	err = db.Get(&count, "SELECT COUNT(*) FROM detected_spam")
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestSetAddedToSamplesFlag(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()

	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	spamEntry := DetectedSpamInfo{
		Text:      "spam message",
		UserID:    1,
		UserName:  "Spammer",
		Timestamp: time.Now(),
		GID:       "group123",
	}

	checks := []spamcheck.Response{
		{
			Name:    "Check1",
			Spam:    true,
			Details: "Details 1",
		},
	}

	err = ds.Write(ctx, spamEntry, checks)
	require.NoError(t, err)
	var added bool
	err = db.Get(&added, "SELECT added FROM detected_spam WHERE text = ?", spamEntry.Text)
	require.NoError(t, err)
	assert.False(t, added)

	err = ds.SetAddedToSamplesFlag(ctx, 1)
	require.NoError(t, err)

	err = db.Get(&added, "SELECT added FROM detected_spam WHERE text = ?", spamEntry.Text)
	require.NoError(t, err)
	assert.True(t, added)
}

func TestDetectedSpam_Read(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()

	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	spamEntry := DetectedSpamInfo{
		Text:      "spam message",
		UserID:    1,
		UserName:  "Spammer",
		Timestamp: time.Now(),
	}

	checks := []spamcheck.Response{
		{
			Name:    "Check1",
			Spam:    true,
			Details: "Details 1",
		},
	}

	checksJSON, err := json.Marshal(checks)
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO detected_spam (text, user_id, user_name, timestamp, checks) VALUES (?, ?, ?, ?, ?)", spamEntry.Text, spamEntry.UserID, spamEntry.UserName, spamEntry.Timestamp, checksJSON)
	require.NoError(t, err)

	entries, err := ds.Read(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	assert.Equal(t, spamEntry.Text, entries[0].Text)
	assert.Equal(t, spamEntry.UserID, entries[0].UserID)
	assert.Equal(t, spamEntry.UserName, entries[0].UserName)

	var retrievedChecks []spamcheck.Response
	err = json.Unmarshal([]byte(entries[0].ChecksJSON), &retrievedChecks)
	require.NoError(t, err)
	assert.Equal(t, checks, retrievedChecks)
	t.Logf("retrieved checks: %+v", retrievedChecks)
}

func TestDetectedSpam_Read_LimitExceeded(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()

	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	for i := 0; i < maxDetectedSpamEntries+10; i++ {
		spamEntry := DetectedSpamInfo{
			Text:      "spam message",
			UserID:    int64(i + 1),
			UserName:  "Spammer",
			Timestamp: time.Now(),
			GID:       "group123",
		}

		checks := []spamcheck.Response{
			{
				Name:    "Check1",
				Spam:    true,
				Details: "Details 1",
			},
		}

		err = ds.Write(ctx, spamEntry, checks)
		require.NoError(t, err)
	}

	entries, err := ds.Read(ctx)
	require.NoError(t, err)
	assert.Len(t, entries, maxDetectedSpamEntries, "expected to retrieve only the maximum number of entries")
}

func TestDetectedSpam_SetAddedToSamplesFlag(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	entry := DetectedSpamInfo{
		GID:       "group123",
		Text:      "spam message",
		UserID:    1,
		UserName:  "Spammer",
		Timestamp: time.Now(),
	}
	checks := []spamcheck.Response{{
		Name:    "Check1",
		Spam:    true,
		Details: "Details 1",
	}}

	err = ds.Write(ctx, entry, checks)
	require.NoError(t, err)

	var added bool
	err = db.Get(&added, "SELECT added FROM detected_spam WHERE gid = ?", entry.GID)
	require.NoError(t, err)
	assert.False(t, added)

	err = ds.SetAddedToSamplesFlag(ctx, 1)
	require.NoError(t, err)

	err = db.Get(&added, "SELECT added FROM detected_spam WHERE gid = ?", entry.GID)
	require.NoError(t, err)
	assert.True(t, added)
}

func TestDetectedSpam(t *testing.T) {
	tests := []struct {
		name    string
		entry   DetectedSpamInfo
		checks  []spamcheck.Response
		wantErr bool
	}{
		{
			name: "basic spam entry",
			entry: DetectedSpamInfo{
				GID:       "group123",
				Text:      "spam message",
				UserID:    1,
				UserName:  "Spammer",
				Timestamp: time.Now(),
			},
			checks: []spamcheck.Response{{
				Name:    "Check1",
				Spam:    true,
				Details: "Details 1",
			}},
		},
		{
			name: "empty gid",
			entry: DetectedSpamInfo{
				Text:      "spam message",
				UserID:    1,
				UserName:  "Spammer",
				Timestamp: time.Now(),
			},
			checks: []spamcheck.Response{{
				Name:    "Check1",
				Spam:    true,
				Details: "Details 1",
			}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, teardown := setupTestDB(t)
			defer teardown()

			ctx := context.Background()
			ds, err := NewDetectedSpam(ctx, db)
			require.NoError(t, err)

			err = ds.Write(ctx, tt.entry, tt.checks)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			entries, err := ds.Read(ctx)
			require.NoError(t, err)
			require.Len(t, entries, 1)

			assert.Equal(t, tt.entry.GID, entries[0].GID)
			assert.Equal(t, tt.entry.Text, entries[0].Text)
			assert.Equal(t, tt.entry.UserID, entries[0].UserID)
			assert.Equal(t, tt.entry.UserName, entries[0].UserName)
			assert.Equal(t, tt.checks, entries[0].Checks)
		})
	}
}

func TestDetectedSpam_Read_LimitAndOrder(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	for i := 0; i < maxDetectedSpamEntries+10; i++ {
		entry := DetectedSpamInfo{
			GID:       "group123",
			Text:      "spam message",
			UserID:    int64(i + 1),
			UserName:  "Spammer",
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
		}
		checks := []spamcheck.Response{{
			Name:    "Check1",
			Spam:    true,
			Details: "Details 1",
		}}

		err = ds.Write(ctx, entry, checks)
		require.NoError(t, err)
	}

	entries, err := ds.Read(ctx)
	require.NoError(t, err)
	assert.Len(t, entries, maxDetectedSpamEntries)

	for i := 1; i < len(entries); i++ {
		assert.True(t, entries[i-1].Timestamp.After(entries[i].Timestamp) ||
			entries[i-1].Timestamp.Equal(entries[i].Timestamp))
	}
}

func TestDetectedSpam_Read_EmptyDB(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	entries, err := ds.Read(ctx)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestDetectedSpam_InvalidJSON(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	// insert invalid JSON directly
	entry := DetectedSpamInfo{
		GID:       "group123",
		Text:      "spam message",
		UserID:    1,
		UserName:  "Spammer",
		Timestamp: time.Now(),
	}

	// invalid JSON string
	_, err = db.Exec(`INSERT INTO detected_spam (gid, text, user_id, user_name, timestamp, checks) 
		VALUES (?, ?, ?, ?, ?, ?)`, entry.GID, entry.Text, entry.UserID, entry.UserName, entry.Timestamp, "{invalid json}")
	require.NoError(t, err)

	// reading should fail
	entries, err := ds.Read(ctx)
	require.ErrorContains(t, err, "failed to unmarshal checks for entry")
	assert.Empty(t, entries)
}

func TestDetectedSpam_ConcurrentAccess(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	const numWorkers = 10
	const entriesPerWorker = 20

	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers*2)

	// prepare all entries first to avoid any races in ID generation
	var allEntries []struct {
		entry  DetectedSpamInfo
		checks []spamcheck.Response
	}
	for i := 0; i < numWorkers; i++ {
		for j := 0; j < entriesPerWorker; j++ {
			allEntries = append(allEntries, struct {
				entry  DetectedSpamInfo
				checks []spamcheck.Response
			}{
				entry: DetectedSpamInfo{
					GID:       db.GID(),
					Text:      fmt.Sprintf("test message %d-%d", i, j),
					UserID:    int64(i*1000 + j + 1), // ensure non-zero
					UserName:  fmt.Sprintf("user%d-%d", i, j),
					Timestamp: time.Now(),
				},
				checks: []spamcheck.Response{{
					Name:    "test",
					Spam:    true,
					Details: fmt.Sprintf("details %d-%d", i, j),
				}},
			})
		}
	}

	// start writers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			start := workerID * entriesPerWorker
			end := start + entriesPerWorker
			for _, item := range allEntries[start:end] {
				if err := ds.Write(ctx, item.entry, item.checks); err != nil {
					errCh <- fmt.Errorf("worker %d write failed: %w", workerID, err)
					return
				}
			}
		}(i)
	}

	// start concurrent readers
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < entriesPerWorker; j++ {
				entries, err := ds.Read(ctx)
				if err != nil {
					errCh <- fmt.Errorf("worker %d read failed: %w", workerID, err)
					return
				}
				// just verify we can read something
				if len(entries) == 0 && j > entriesPerWorker/2 {
					errCh <- fmt.Errorf("worker %d got empty result", workerID)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	require.Empty(t, errs)

	// verify final state
	entries, err := ds.Read(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
	assert.LessOrEqual(t, len(entries), maxDetectedSpamEntries)

	// verify entry format on first entry
	assert.Equal(t, db.GID(), entries[0].GID)
	assert.NotEmpty(t, entries[0].Text)
	assert.NotZero(t, entries[0].UserID)
	assert.NotEmpty(t, entries[0].UserName)
	assert.NotEmpty(t, entries[0].ChecksJSON)
	assert.NotEmpty(t, entries[0].Checks)
}

func TestDetectedSpam_GIDIsolation(t *testing.T) {
	db1, err := NewSqliteDB(":memory:", "gr1")
	require.NoError(t, err)
	defer db1.Close()

	db2, err := NewSqliteDB(":memory:", "gr2")
	require.NoError(t, err)
	defer db2.Close()

	ctx := context.Background()
	ds1, err := NewDetectedSpam(ctx, db1)
	require.NoError(t, err)

	ds2, err := NewDetectedSpam(ctx, db2)
	require.NoError(t, err)

	// Add same text to both GIDs
	entry := DetectedSpamInfo{
		GID:       "gr1",
		Text:      "spam message",
		UserID:    1,
		UserName:  "Spammer",
		Timestamp: time.Now(),
	}
	checks := []spamcheck.Response{{Name: "Check1", Spam: true, Details: "Details"}}

	err = ds1.Write(ctx, entry, checks)
	require.NoError(t, err)

	entry.GID = "gr2"
	err = ds2.Write(ctx, entry, checks)
	require.NoError(t, err)

	// Verify each storage only sees its own entries
	entries1, err := ds1.Read(ctx)
	require.NoError(t, err)
	assert.Len(t, entries1, 1)
	assert.Equal(t, "gr1", entries1[0].GID)

	entries2, err := ds2.Read(ctx)
	require.NoError(t, err)
	assert.Len(t, entries2, 1)
	assert.Equal(t, "gr2", entries2[0].GID)
}

func TestDetectedSpam_ValidationAndEdgeCases(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	tests := []struct {
		name    string
		entry   DetectedSpamInfo
		checks  []spamcheck.Response
		wantErr bool
	}{
		{
			name: "missing gid",
			entry: DetectedSpamInfo{
				Text:      "spam message",
				UserID:    1,
				UserName:  "Spammer",
				Timestamp: time.Now(),
			},
			checks:  []spamcheck.Response{{Name: "Check1", Spam: true}},
			wantErr: true,
		},
		{
			name: "empty text",
			entry: DetectedSpamInfo{
				GID:       "group1",
				UserID:    1,
				UserName:  "Spammer",
				Timestamp: time.Now(),
			},
			checks:  []spamcheck.Response{{Name: "Check1", Spam: true}},
			wantErr: true,
		},
		{
			name: "zero user id",
			entry: DetectedSpamInfo{
				GID:       "group1",
				Text:      "spam message",
				UserName:  "Spammer",
				Timestamp: time.Now(),
			},
			checks:  []spamcheck.Response{{Name: "Check1", Spam: true}},
			wantErr: true,
		},
		{
			name: "empty username",
			entry: DetectedSpamInfo{
				GID:       "group1",
				Text:      "spam message",
				UserID:    1,
				Timestamp: time.Now(),
			},
			checks:  []spamcheck.Response{{Name: "Check1", Spam: true}},
			wantErr: true,
		},
		{
			name: "nil checks",
			entry: DetectedSpamInfo{
				GID:       "group1",
				Text:      "spam message",
				UserID:    1,
				UserName:  "Spammer",
				Timestamp: time.Now(),
			},
			checks:  nil,
			wantErr: false,
		},
		{
			name: "empty checks",
			entry: DetectedSpamInfo{
				GID:       "group1",
				Text:      "spam message",
				UserID:    1,
				UserName:  "Spammer",
				Timestamp: time.Now(),
			},
			checks:  []spamcheck.Response{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ds.Write(ctx, tt.entry, tt.checks)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			entries, err := ds.Read(ctx)
			require.NoError(t, err)
			require.NotEmpty(t, entries)
			assert.Equal(t, tt.entry.Text, entries[0].Text)
		})
	}
}
