package storage

import (
	"context"
	"encoding/json"
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
