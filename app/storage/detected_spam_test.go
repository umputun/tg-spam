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

	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestNewDetectedSpam(t *testing.T) {
	t.Run("with empty database", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()

		ctx := context.Background()
		ds, err := NewDetectedSpam(ctx, db)
		require.NoError(t, err)
		require.NotNil(t, ds)

		// verify table exists with correct schema
		var cols []struct {
			CID       int     `db:"cid"`
			Name      string  `db:"name"`
			Type      string  `db:"type"`
			NotNull   bool    `db:"notnull"`
			DfltValue *string `db:"dflt_value"`
			PK        bool    `db:"pk"`
		}
		err = db.Select(&cols, "PRAGMA table_info(detected_spam)")
		require.NoError(t, err)

		colMap := make(map[string]string)
		for _, col := range cols {
			colMap[col.Name] = col.Type
		}

		assert.Equal(t, "TEXT", colMap["gid"])
		assert.Equal(t, "TEXT", colMap["text"])
		assert.Equal(t, "INTEGER", colMap["user_id"])
		assert.Equal(t, "TEXT", colMap["user_name"])
	})

	t.Run("with existing old schema", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()

		// create old schema
		_, err := db.Exec(`
			CREATE TABLE detected_spam (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				text TEXT,
				user_id INTEGER,
				user_name TEXT,
				timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
				added BOOLEAN DEFAULT 0,
				checks TEXT
			)
		`)
		require.NoError(t, err)

		// insert test data
		_, err = db.Exec(`
			INSERT INTO detected_spam (text, user_id, user_name, checks)
			VALUES (?, ?, ?, ?)`,
			"test spam", 123, "test_user", `[{"Name":"test","Spam":true}]`)
		require.NoError(t, err)

		ctx := context.Background()
		ds, err := NewDetectedSpam(ctx, db)
		require.NoError(t, err)
		require.NotNil(t, ds)

		// verify data was preserved and migrated
		entries, err := ds.Read(ctx)
		require.NoError(t, err)
		require.Len(t, entries, 1)

		assert.Equal(t, db.GID(), entries[0].GID)
		assert.Equal(t, "test spam", entries[0].Text)
		assert.Equal(t, int64(123), entries[0].UserID)
		assert.Equal(t, "test_user", entries[0].UserName)
	})

	t.Run("with nil db", func(t *testing.T) {
		ctx := context.Background()
		_, err := NewDetectedSpam(ctx, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "db connection is nil")
	})

	t.Run("with cancelled context", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := NewDetectedSpam(ctx, db)
		require.Error(t, err)
	})
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
	db1, err := engine.NewSqlite(":memory:", "gr1")
	require.NoError(t, err)
	defer db1.Close()

	db2, err := engine.NewSqlite(":memory:", "gr2")
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
			name: "empty username",
			entry: DetectedSpamInfo{
				GID:       "group1",
				Text:      "spam message",
				UserID:    1,
				Timestamp: time.Now(),
			},
			checks:  []spamcheck.Response{{Name: "Check1", Spam: true}},
			wantErr: false,
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

func TestDetectedSpam_ComplexMigration(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	// create old schema without gid
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS detected_spam (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			text TEXT,
			user_id INTEGER,
			user_name TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			added BOOLEAN DEFAULT 0,
			checks TEXT
		)
	`)
	require.NoError(t, err)

	// insert test data
	testData := []struct {
		text     string
		userID   int64
		userName string
		checks   string
	}{
		{"spam1", 1, "user1", `[{"Name":"test1","Spam":true}]`},
		{"spam2", 2, "user2", `[{"Name":"test2","Spam":true}]`},
	}

	for _, td := range testData {
		_, err = db.Exec(`
			INSERT INTO detected_spam (text, user_id, user_name, checks)
			VALUES (?, ?, ?, ?)`,
			td.text, td.userID, td.userName, td.checks)
		require.NoError(t, err)
	}

	// create instance to trigger migration
	ctx := context.Background()
	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)
	require.NotNil(t, ds)

	// verify migration results
	var entries []struct {
		Text     string `db:"text"`
		UserID   int64  `db:"user_id"`
		UserName string `db:"user_name"`
		GID      string `db:"gid"`
		Checks   string `db:"checks"`
	}

	err = db.Select(&entries, "SELECT text, user_id, user_name, gid, checks FROM detected_spam")
	require.NoError(t, err)
	assert.Len(t, entries, len(testData))

	for _, entry := range entries {
		assert.Equal(t, db.GID(), entry.GID)
		assert.NotEmpty(t, entry.Text)
		assert.NotZero(t, entry.UserID)
		assert.NotEmpty(t, entry.UserName)
		assert.NotEmpty(t, entry.Checks)
	}
}

func TestDetectedSpam_MigrationEdgeCases(t *testing.T) {
	t.Run("migration with empty table", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()

		// create empty table
		_, err := db.Exec(`
            CREATE TABLE IF NOT EXISTS detected_spam (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                text TEXT,
                user_id INTEGER,
                user_name TEXT,
                timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
                added BOOLEAN DEFAULT 0,
                checks TEXT
            )
        `)
		require.NoError(t, err)

		ctx := context.Background()
		ds, err := NewDetectedSpam(ctx, db)
		require.NoError(t, err)
		require.NotNil(t, ds)

		// verify migration didn't affect empty table
		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM detected_spam")
		require.NoError(t, err)
		assert.Zero(t, count)
	})

	t.Run("migration with existing gid column", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()

		// get schema creation queries
		schema, err := detectedSpamQueries.Pick(db.Type(), CmdCreateDetectedSpamTable)
		require.NoError(t, err)

		// create schema with gid already present
		_, err = db.Exec(schema)
		require.NoError(t, err)

		// insert test data with existing gid
		_, err = db.Exec(`
	        INSERT INTO detected_spam (text, user_id, user_name, gid, checks)
	        VALUES (?, ?, ?, ?, ?)`,
			"spam", 1, "user1", "existing_gid", `[{"Name":"test","Spam":true}]`)
		require.NoError(t, err)

		ctx := context.Background()
		ds, err := NewDetectedSpam(ctx, db)
		require.NoError(t, err)
		require.NotNil(t, ds)

		// verify existing gid wasn't changed
		var gid string
		err = db.Get(&gid, "SELECT gid FROM detected_spam WHERE user_id = 1")
		require.NoError(t, err)
		assert.Equal(t, "existing_gid", gid)
	})
}

func TestDetectedSpam_ConcurrentWrites(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	const workers = 10
	const entriesPerWorker = 10

	// create a buffered channel large enough to avoid blocking
	errCh := make(chan error, workers*entriesPerWorker)
	var wg sync.WaitGroup
	var mu sync.Mutex                   // protect log spam check
	spamMap := make(map[int64]struct{}) // track used IDs

	for w := 0; w < workers; w++ {
		wg.Add(1)
		workerID := w + 1 // ensure non-zero worker ID
		go func(workerID int) {
			defer wg.Done()

			for i := 0; i < entriesPerWorker; i++ {
				userID := int64((workerID * 100) + i + 1) // ensure non-zero user ID

				mu.Lock()
				if _, exists := spamMap[userID]; exists {
					mu.Unlock()
					errCh <- fmt.Errorf("duplicate user ID: %d", userID)
					continue
				}
				spamMap[userID] = struct{}{}
				mu.Unlock()

				entry := DetectedSpamInfo{
					GID:       "group1",
					Text:      fmt.Sprintf("spam message from worker %d-%d", workerID, i),
					UserID:    userID,
					UserName:  fmt.Sprintf("user%d-%d", workerID, i),
					Timestamp: time.Now(),
				}
				checks := []spamcheck.Response{{
					Name:    fmt.Sprintf("check%d-%d", workerID, i),
					Spam:    true,
					Details: "test details",
				}}

				if err := ds.Write(ctx, entry, checks); err != nil {
					errCh <- fmt.Errorf("worker %d failed to write entry: %w", workerID, err)
				}

				// small sleep to avoid too much contention
				time.Sleep(time.Millisecond)
			}
		}(workerID)
	}

	wg.Wait()
	close(errCh)

	// Collect all errors
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	require.Empty(t, errs, "should not have any errors from workers: %v", errs)

	// verify entries
	entries, err := ds.Read(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	assert.LessOrEqual(t, len(entries), maxDetectedSpamEntries)

	// verify entry formats and uniqueness
	seenIDs := make(map[int64]bool)
	for _, entry := range entries {
		assert.NotEmpty(t, entry.GID)
		assert.NotEmpty(t, entry.Text)
		assert.NotZero(t, entry.UserID)
		assert.NotEmpty(t, entry.UserName)
		assert.NotEmpty(t, entry.ChecksJSON)
		assert.NotEmpty(t, entry.Checks)
		assert.False(t, seenIDs[entry.UserID], "duplicate UserID found: %d", entry.UserID)
		seenIDs[entry.UserID] = true
	}
}

func TestDetectedSpam_ReadAfterCleanup(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	// add more entries than maxDetectedSpamEntries
	for i := 0; i < maxDetectedSpamEntries+10; i++ {
		entry := DetectedSpamInfo{
			GID:       "group1",
			Text:      fmt.Sprintf("spam message %d", i),
			UserID:    int64(i + 1), // ensure non-zero
			UserName:  fmt.Sprintf("user%d", i),
			Timestamp: time.Now().Add(time.Duration(-i) * time.Hour),
		}
		checks := []spamcheck.Response{{Name: "test", Spam: true}}

		err := ds.Write(ctx, entry, checks)
		require.NoError(t, err)
	}

	// read and verify
	entries, err := ds.Read(ctx)
	require.NoError(t, err)
	assert.Equal(t, maxDetectedSpamEntries, len(entries), "should have exactly maxDetectedSpamEntries entries")

	// verify order (newest first)
	for i := 1; i < len(entries); i++ {
		assert.True(t, entries[i-1].Timestamp.After(entries[i].Timestamp) ||
			entries[i-1].Timestamp.Equal(entries[i].Timestamp),
			"entries should be ordered by timestamp descending")
	}
}

func TestDetectedSpam_FindByUserID(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	ds, err := NewDetectedSpam(ctx, db)
	require.NoError(t, err)

	t.Run("user not found", func(t *testing.T) {
		entry, err := ds.FindByUserID(ctx, 123)
		require.NoError(t, err)
		assert.Nil(t, entry)
	})

	t.Run("basic case", func(t *testing.T) {
		ts := time.Now().Truncate(time.Second)
		expected := DetectedSpamInfo{
			GID:       db.GID(),
			Text:      "test spam",
			UserID:    456,
			UserName:  "spammer",
			Timestamp: ts,
		}
		checks := []spamcheck.Response{{
			Name:    "test",
			Spam:    true,
			Details: "test details",
		}}

		err := ds.Write(ctx, expected, checks)
		require.NoError(t, err)

		entry, err := ds.FindByUserID(ctx, 456)
		require.NoError(t, err)
		require.NotNil(t, entry)
		assert.Equal(t, expected.GID, entry.GID)
		assert.Equal(t, expected.Text, entry.Text)
		assert.Equal(t, expected.UserID, entry.UserID)
		assert.Equal(t, expected.UserName, entry.UserName)
		assert.Equal(t, checks, entry.Checks)
		assert.Equal(t, ts.Local(), entry.Timestamp)
	})

	t.Run("multiple entries", func(t *testing.T) {
		// write two entries for same user
		for i := 0; i < 2; i++ {
			entry := DetectedSpamInfo{
				GID:       db.GID(),
				Text:      fmt.Sprintf("spam %d", i),
				UserID:    789,
				UserName:  "spammer",
				Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
			}
			checks := []spamcheck.Response{{Name: fmt.Sprintf("check%d", i), Spam: true}}
			err := ds.Write(ctx, entry, checks)
			require.NoError(t, err)
		}

		// should get the latest one
		entry, err := ds.FindByUserID(ctx, 789)
		require.NoError(t, err)
		require.NotNil(t, entry)
		assert.Equal(t, "spam 1", entry.Text)
		assert.Equal(t, "check1", entry.Checks[0].Name)
	})

	t.Run("invalid checks json", func(t *testing.T) {
		// insert invalid json directly to db
		_, err := db.Exec(`INSERT INTO detected_spam 
			(gid, text, user_id, user_name, timestamp, checks) 
			VALUES (?, ?, ?, ?, ?, ?)`,
			db.GID(), "test", 999, "test", time.Now(), "{invalid}")
		require.NoError(t, err)

		entry, err := ds.FindByUserID(ctx, 999)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal checks")
		assert.Nil(t, entry)
	})

	t.Run("gid isolation", func(t *testing.T) {
		// create another db instance with different gid
		db2, err := engine.NewSqlite(":memory:", "other_gid")
		require.NoError(t, err)
		defer db2.Close()

		ds2, err := NewDetectedSpam(ctx, db2)
		require.NoError(t, err)

		entry1 := DetectedSpamInfo{
			GID:      db.GID(),
			Text:     "spam1",
			UserID:   111,
			UserName: "spammer1",
		}
		entry2 := DetectedSpamInfo{
			GID:      "other_gid",
			Text:     "spam2",
			UserID:   111,
			UserName: "spammer2",
		}
		checks := []spamcheck.Response{{Name: "test", Spam: true}}

		// write different entries to each db
		err = ds.Write(ctx, entry1, checks)
		require.NoError(t, err)
		err = ds2.Write(ctx, entry2, checks)
		require.NoError(t, err)

		// first db should not find entry with other gid
		res1, err := ds.FindByUserID(ctx, 111)
		require.NoError(t, err)
		require.NotNil(t, res1)
		assert.Equal(t, "spam1", res1.Text)
		assert.Equal(t, db.GID(), res1.GID)

		// second db should find its own entry
		res2, err := ds2.FindByUserID(ctx, 111)
		require.NoError(t, err)
		require.NotNil(t, res2)
		assert.Equal(t, "spam2", res2.Text)
		assert.Equal(t, "other_gid", res2.GID)
	})
}

func TestDetectedSpam_All(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx := context.Background()
	for dbName, provider := range providers {
		t.Run(dbName, func(t *testing.T) {
			runDetectedSpamTests(t, ctx, provider)
		})
	}
}

func runDetectedSpamTests(t *testing.T, ctx context.Context, provider engineProvider) {
	t.Helper()

	// basic operations tests
	t.Run("basic_ops", func(t *testing.T) {
		db, teardown := provider(t, ctx)
		defer teardown()

		ds, err := NewDetectedSpam(ctx, db)
		require.NoError(t, err)

		entry := DetectedSpamInfo{
			GID:       db.GID(),
			Text:      "test spam",
			UserID:    123,
			UserName:  "spammer",
			Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		}
		checks := []spamcheck.Response{{
			Name:    "test",
			Spam:    true,
			Details: "test details",
		}}

		// test write
		err = ds.Write(ctx, entry, checks)
		require.NoError(t, err)

		// test read
		entries, err := ds.Read(ctx)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, entry.Text, entries[0].Text)
		assert.Equal(t, entry.UserID, entries[0].UserID)
		assert.Equal(t, entry.UserName, entries[0].UserName)
		assert.Equal(t, entry.Timestamp.Unix(), entries[0].Timestamp.Unix())
		assert.Equal(t, checks, entries[0].Checks)
	})

	t.Run("unicode_content", func(t *testing.T) {
		db, teardown := provider(t, ctx)
		defer teardown()

		ds, err := NewDetectedSpam(ctx, db)
		require.NoError(t, err)

		entry := DetectedSpamInfo{
			GID:       db.GID(),
			Text:      "привет 你好 こんにちは",
			UserID:    123,
			UserName:  "スパマー",
			Timestamp: time.Now().UTC(),
		}
		checks := []spamcheck.Response{{Name: "test", Spam: true}}

		err = ds.Write(ctx, entry, checks)
		require.NoError(t, err)

		entries, err := ds.Read(ctx)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, entry.Text, entries[0].Text)
		assert.Equal(t, entry.UserName, entries[0].UserName)
	})

	t.Run("concurrent_ops", func(t *testing.T) {
		db, teardown := provider(t, ctx)
		defer teardown()

		ds, err := NewDetectedSpam(ctx, db)
		require.NoError(t, err)

		var wg sync.WaitGroup
		errCh := make(chan error, 10)

		// Start multiple writers
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()

				entry := DetectedSpamInfo{
					GID:       db.GID(),
					Text:      fmt.Sprintf("spam %d", i),
					UserID:    int64(i),
					UserName:  fmt.Sprintf("user%d", i),
					Timestamp: time.Now().UTC(),
				}
				checks := []spamcheck.Response{{Name: fmt.Sprintf("check%d", i), Spam: true}}

				if err := ds.Write(ctx, entry, checks); err != nil {
					errCh <- fmt.Errorf("write failed: %w", err)
				}
			}(i)
		}

		wg.Wait()
		close(errCh)

		for err := range errCh {
			t.Errorf("concurrent operation error: %v", err)
		}

		entries, err := ds.Read(ctx)
		require.NoError(t, err)
		assert.Len(t, entries, 10)
	})

	t.Run("entry_limit", func(t *testing.T) {
		db, teardown := provider(t, ctx)
		defer teardown()

		ds, err := NewDetectedSpam(ctx, db)
		require.NoError(t, err)

		// add more entries than the limit
		for i := 0; i < maxDetectedSpamEntries+10; i++ {
			entry := DetectedSpamInfo{
				GID:       db.GID(),
				Text:      fmt.Sprintf("spam %d", i),
				UserID:    int64(i),
				UserName:  fmt.Sprintf("user%d", i),
				Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			}
			checks := []spamcheck.Response{{Name: "test", Spam: true}}

			err = ds.Write(ctx, entry, checks)
			require.NoError(t, err)
		}

		entries, err := ds.Read(ctx)
		require.NoError(t, err)
		assert.Len(t, entries, maxDetectedSpamEntries)

		// verify entries are ordered by timestamp desc
		for i := 1; i < len(entries); i++ {
			assert.True(t, entries[i-1].Timestamp.After(entries[i].Timestamp) ||
				entries[i-1].Timestamp.Equal(entries[i].Timestamp))
		}
	})

	t.Run("find_by_user_id", func(t *testing.T) {
		db, teardown := provider(t, ctx)
		defer teardown()

		ds, err := NewDetectedSpam(ctx, db)
		require.NoError(t, err)

		// add multiple entries for the same user
		for i := 0; i < 3; i++ {
			entry := DetectedSpamInfo{
				GID:       db.GID(),
				Text:      fmt.Sprintf("spam %d", i),
				UserID:    123,
				UserName:  "spammer",
				Timestamp: time.Now().Add(time.Duration(i) * time.Hour),
			}
			checks := []spamcheck.Response{{Name: fmt.Sprintf("check%d", i), Spam: true}}

			err = ds.Write(ctx, entry, checks)
			require.NoError(t, err)
		}

		found, err := ds.FindByUserID(ctx, 123)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, "spam 2", found.Text)
		assert.Equal(t, "check2", found.Checks[0].Name)
	})
}
