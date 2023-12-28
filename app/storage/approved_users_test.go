package storage

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovedUsers_StoreAndRead(t *testing.T) {
	tests := []struct {
		name     string
		ids      []string
		expected []string
	}{
		{
			name:     "empty",
			ids:      []string{},
			expected: []string{},
		},
		{
			name:     "single ID",
			ids:      []string{"12345"},
			expected: []string{"12345"},
		},
		{
			name:     "multiple IDs",
			ids:      []string{"123", "456", "789"},
			expected: []string{"123", "456", "789"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "test_approved_users")
			require.NoError(t, err)
			defer os.RemoveAll(tmpDir) // Clean up

			filePath := tmpDir + "/testfile.bin"
			db, err := NewSqliteDB(filePath)
			require.NoError(t, err)
			au, err := NewApprovedUsers(db)
			require.NoError(t, err)

			err = au.Store(tt.ids)
			require.NoError(t, err)

			var readBuffer bytes.Buffer
			buf := make([]byte, 1024) // buffer for reading
			for {
				n, err := au.Read(buf)
				if err == io.EOF {
					break
				}
				require.NoError(t, err)
				readBuffer.Write(buf[:n])
			}

			// convert read data back to IDs
			var readIDs []string
			if readBuffer.Len() > 0 {
				for _, strID := range strings.Split(readBuffer.String(), "\n") {
					if strID == "" {
						continue
					}
					readIDs = append(readIDs, strID)
				}
			}

			// Compare the original and read IDs, treating nil and empty slices as equal
			if len(tt.expected) == 0 {
				assert.Equal(t, 0, len(readIDs))
			} else {
				assert.Equal(t, tt.expected, readIDs)
			}
		})
	}
}

func TestApprovedUser_UpsertWithoutTimestampUpdate(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	au, err := NewApprovedUsers(db)
	require.NoError(t, err)

	err = au.Store([]string{"123"})
	require.NoError(t, err)

	var initialTimestamp time.Time
	err = db.Get(&initialTimestamp, "SELECT timestamp FROM approved_users WHERE id = ?", 123)
	require.NoError(t, err)

	time.Sleep(2 * time.Second)

	err = au.Store([]string{"123"})
	require.NoError(t, err)

	var afterUpsertTimestamp time.Time
	err = db.Get(&afterUpsertTimestamp, "SELECT timestamp FROM approved_users WHERE id = ?", 123)
	require.NoError(t, err)
	assert.Equal(t, initialTimestamp, afterUpsertTimestamp, "Timestamp should not change on upsert")
}

func TestApprovedUsers_Timestamp(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	au, err := NewApprovedUsers(db)
	require.NoError(t, err)

	testID := "123"
	_, err = db.Exec("INSERT INTO approved_users (id, timestamp) VALUES (?, ?)", testID, "2023-01-01T00:00:00")
	require.NoError(t, err)
	expectedTimestamp, err := time.Parse("2006-01-02T15:04:05", "2023-01-01T00:00:00")
	require.NoError(t, err)

	t.Run("existing ID", func(t *testing.T) {
		actualTimestamp, err := au.Timestamp(testID)
		require.NoError(t, err)
		assert.Equal(t, expectedTimestamp, actualTimestamp, "The timestamps should match")
	})

	t.Run("non-existing ID", func(t *testing.T) {
		_, err := au.Timestamp("456")
		assert.Error(t, err, "Should return error for non-existing ID")
	})
}

func TestApprovedUsers_Delete(t *testing.T) {
	db, err := sqlx.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	au, err := NewApprovedUsers(db)
	require.NoError(t, err)

	t.Run("existing ID", func(t *testing.T) {
		err = au.Store([]string{"123"})
		require.NoError(t, err)
		err = au.Store([]string{"456"})
		require.NoError(t, err)

		err = au.Delete("123")
		require.NoError(t, err)

		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM approved_users")
		require.NoError(t, err)
		assert.Equal(t, 1, count, "There should be only one record left")
	})

	t.Run("non-existing ID", func(t *testing.T) {
		err = au.Store([]string{"123"})
		require.NoError(t, err)
		err = au.Store([]string{"456"})
		require.NoError(t, err)

		err = au.Delete("789")
		require.NoError(t, err)

		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM approved_users")
		require.NoError(t, err)
		assert.Equal(t, 2, count, "There should be two records left")
	})

	t.Run("invalid ID", func(t *testing.T) {
		err = au.Store([]string{"123"})
		require.NoError(t, err)
		err = au.Store([]string{"456"})
		require.NoError(t, err)

		err = au.Delete("aaa789")
		require.Error(t, err)

		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM approved_users")
		require.NoError(t, err)
		assert.Equal(t, 2, count, "There should be two records left")
	})
}
