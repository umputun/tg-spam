package storage

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprovedUsers_NewApprovedUsers(t *testing.T) {
	t.Run("create new table", func(t *testing.T) {
		db, err := sqlx.Open("sqlite", ":memory:")
		require.NoError(t, err)
		defer db.Close()

		_, err = NewApprovedUsers(db)
		require.NoError(t, err)

		// check if the table and columns exist
		var exists int
		err = db.Get(&exists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='approved_users'")
		require.NoError(t, err)
		assert.Equal(t, 1, exists)

		err = db.Get(&exists, "SELECT COUNT(*) FROM pragma_table_info('approved_users') WHERE name='name'")
		require.NoError(t, err)
		assert.Equal(t, 1, exists)
	})

	t.Run("add name column to existing table", func(t *testing.T) {
		db, err := sqlx.Open("sqlite", ":memory:")
		require.NoError(t, err)
		defer db.Close()

		// create table without 'name' column
		_, err = db.Exec(`CREATE TABLE approved_users (id INTEGER PRIMARY KEY, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP)`)
		require.NoError(t, err)

		_, err = NewApprovedUsers(db)
		require.NoError(t, err)

		// check if the 'name' column was added
		var exists int
		err = db.Get(&exists, "SELECT COUNT(*) FROM pragma_table_info('approved_users') WHERE name='name'")
		require.NoError(t, err)
		assert.Equal(t, 1, exists)
	})

	t.Run("table and name column already exist", func(t *testing.T) {
		db, err := sqlx.Open("sqlite", ":memory:")
		require.NoError(t, err)
		defer db.Close()

		// create table with 'name' column
		_, err = db.Exec(`CREATE TABLE approved_users (id INTEGER PRIMARY KEY, name TEXT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP)`)
		require.NoError(t, err)

		_, err = NewApprovedUsers(db)
		require.NoError(t, err)

		// verify that the existing structure has not changed
		var columnCount int
		err = db.Get(&columnCount, "SELECT COUNT(*) FROM pragma_table_info('approved_users')")
		require.NoError(t, err)
		assert.Equal(t, 3, columnCount)
	})
}

func TestApprovedUsers_Write(t *testing.T) {
	db, e := sqlx.Open("sqlite", ":memory:")
	require.NoError(t, e)
	defer db.Close()
	au, e := NewApprovedUsers(db)
	require.NoError(t, e)

	t.Run("write new user without timestamp", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		require.NoError(t, err)
		err = au.Write(ApprovedUsersInfo{UserID: 123, UserName: "John Doe"})
		require.NoError(t, err)

		var user ApprovedUsersInfo
		err = db.Get(&user, "SELECT id, name, timestamp FROM approved_users WHERE id = ?", 123)
		require.NoError(t, err)
		assert.Equal(t, "John Doe", user.UserName)
		assert.True(t, time.Since(user.Timestamp) < time.Second, "Timestamp should be recent %v", user.Timestamp)
	})

	t.Run("write new user with timestamp", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		require.NoError(t, err)
		err = au.Write(ApprovedUsersInfo{UserID: 123, UserName: "John Doe", Timestamp: time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)})
		require.NoError(t, err)

		var user ApprovedUsersInfo
		err = db.Get(&user, "SELECT id, name, timestamp FROM approved_users WHERE id = ?", 123)
		require.NoError(t, err)
		assert.Equal(t, "John Doe", user.UserName)
		assert.Equal(t, time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC), user.Timestamp)
	})

	t.Run("update existing user", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		require.NoError(t, err)
		err = au.Write(ApprovedUsersInfo{UserID: 123, UserName: "John Doe", Timestamp: time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)})
		require.NoError(t, err)

		err = au.Write(ApprovedUsersInfo{UserID: 123, UserName: "John Doe Updated"})
		require.NoError(t, err)

		var user ApprovedUsersInfo
		err = db.Get(&user, "SELECT id, name, timestamp FROM approved_users WHERE id = ?", 123)
		require.NoError(t, err)
		assert.Equal(t, "John Doe", user.UserName)
		assert.Equal(t, time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC), user.Timestamp)
	})
}

func TestApprovedUsers_StoreAndRead(t *testing.T) {
	tests := []struct {
		name     string
		ids      []int64
		expected []string
	}{
		{
			name:     "empty",
			ids:      []int64{},
			expected: []string{},
		},
		{
			name:     "single ID",
			ids:      []int64{12345},
			expected: []string{"12345"},
		},
		{
			name:     "multiple IDs",
			ids:      []int64{123, 456, 789},
			expected: []string{"123", "456", "789"},
		},
		{
			name:     "multiple IDs, with one bad",
			ids:      []int64{123, 456},
			expected: []string{"123", "456"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := NewSqliteDB(":memory:")
			require.NoError(t, err)
			au, err := NewApprovedUsers(db)
			require.NoError(t, err)

			for _, id := range tt.ids {
				err := au.Write(ApprovedUsersInfo{UserID: id, UserName: fmt.Sprintf("name_%d", id)})
				require.NoError(t, err)
			}

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

func TestApprovedUsers_GetAll(t *testing.T) {
	db, e := NewSqliteDB(":memory:")
	require.NoError(t, e)
	au, e := NewApprovedUsers(db)
	require.NoError(t, e)

	t.Run("empty", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)
		users, err := au.GetAll()
		require.NoError(t, err)
		assert.Equal(t, []ApprovedUsersInfo{}, users)
	})

	t.Run("direct writes", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		_, err = db.Exec("INSERT INTO approved_users (id, name, timestamp) VALUES (?, ?, ?)", 123, "John Doe", time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)

		_, err = db.Exec("INSERT INTO approved_users (id, name, timestamp) VALUES (?, ?, ?)", 456, "Jane Doe", time.Date(2023, 10, 3, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)

		users, err := au.GetAll()
		require.NoError(t, err)
		assert.Equal(t, []ApprovedUsersInfo{
			{UserID: 456, UserName: "Jane Doe", Timestamp: time.Date(2023, 10, 3, 0, 0, 0, 0, time.UTC)},
			{UserID: 123, UserName: "John Doe", Timestamp: time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)},
		}, users)
	})

	t.Run("write via ApprovedUsers", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		require.NoError(t, err)
		err = au.Write(ApprovedUsersInfo{UserID: 123, UserName: "John Doe", Timestamp: time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)})
		require.NoError(t, err)

		err = au.Write(ApprovedUsersInfo{UserID: 456, UserName: "Jane Doe", Timestamp: time.Date(2023, 10, 3, 0, 0, 0, 0, time.UTC)})
		require.NoError(t, err)

		users, err := au.GetAll()
		require.NoError(t, err)
		assert.Equal(t, []ApprovedUsersInfo{
			{UserID: 456, UserName: "Jane Doe", Timestamp: time.Date(2023, 10, 3, 0, 0, 0, 0, time.UTC)},
			{UserID: 123, UserName: "John Doe", Timestamp: time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)},
		}, users)
	})
}

func TestApprovedUsers_Delete(t *testing.T) {
	db, e := NewSqliteDB(":memory:")
	require.NoError(t, e)
	au, e := NewApprovedUsers(db)
	require.NoError(t, e)

	t.Run("delete existing user", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		require.NoError(t, err)
		err = au.Write(ApprovedUsersInfo{UserID: 123, UserName: "John Doe"})
		require.NoError(t, err)

		err = au.Delete(123)
		require.NoError(t, err)

		var user ApprovedUsersInfo
		err = db.Get(&user, "SELECT id, name, timestamp FROM approved_users WHERE id = ?", 123)
		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "no rows in result set"))
	})

	t.Run("delete non-existing user", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		err = au.Delete(123)
		require.NoError(t, err)
	})
}
