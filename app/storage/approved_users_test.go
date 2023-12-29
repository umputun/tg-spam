package storage

import (
	"strings"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/approved"
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

	t.Run("table already exists", func(t *testing.T) {
		db, err := sqlx.Open("sqlite", ":memory:")
		require.NoError(t, err)
		defer db.Close()

		// Create table with 'name' column
		_, err = db.Exec(`CREATE TABLE approved_users (id TEXT PRIMARY KEY, name TEXT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP)`)
		require.NoError(t, err)

		_, err = NewApprovedUsers(db)
		require.NoError(t, err)

		// Verify that the existing structure has not changed
		var columnCount int
		err = db.Get(&columnCount, "SELECT COUNT(*) FROM pragma_table_info('approved_users')")
		require.NoError(t, err)
		assert.Equal(t, 3, columnCount)
	})

	t.Run("table exists with INTEGER id", func(t *testing.T) {
		db, err := sqlx.Open("sqlite", ":memory:")
		require.NoError(t, err)
		defer db.Close()

		// Create table with INTEGER id
		_, err = db.Exec(`CREATE TABLE approved_users (id INTEGER PRIMARY KEY, name TEXT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP)`)
		require.NoError(t, err)

		_, err = NewApprovedUsers(db)
		require.NoError(t, err)

		// Verify that the new table is created with TEXT id
		var idType string
		err = db.Get(&idType, "SELECT type FROM pragma_table_info('approved_users') WHERE name='id'")
		require.NoError(t, err)
		assert.Equal(t, "TEXT", idType)
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
		err = au.Write(approved.UserInfo{UserID: "123", UserName: "John Doe"})
		require.NoError(t, err)

		var user approvedUsersInfo
		err = db.Get(&user, "SELECT id, name, timestamp FROM approved_users WHERE id = ?", 123)
		require.NoError(t, err)
		assert.Equal(t, "John Doe", user.UserName)
		assert.True(t, time.Since(user.Timestamp) < time.Second, "Timestamp should be recent %v", user.Timestamp)
	})

	t.Run("write new user with timestamp", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		require.NoError(t, err)
		err = au.Write(approved.UserInfo{UserID: "123", UserName: "John Doe", Timestamp: time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)})
		require.NoError(t, err)

		var user approvedUsersInfo
		err = db.Get(&user, "SELECT id, name, timestamp FROM approved_users WHERE id = ?", 123)
		require.NoError(t, err)
		assert.Equal(t, "John Doe", user.UserName)
		assert.Equal(t, time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC), user.Timestamp)
	})

	t.Run("update existing user", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		require.NoError(t, err)
		err = au.Write(approved.UserInfo{UserID: "123", UserName: "John Doe", Timestamp: time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)})
		require.NoError(t, err)

		err = au.Write(approved.UserInfo{UserID: "123", UserName: "John Doe Updated"})
		require.NoError(t, err)

		var user approvedUsersInfo
		err = db.Get(&user, "SELECT id, name, timestamp FROM approved_users WHERE id = ?", 123)
		require.NoError(t, err)
		assert.Equal(t, "John Doe", user.UserName)
		assert.Equal(t, time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC), user.Timestamp)
	})
}

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
		{
			name:     "multiple IDs, with one bad",
			ids:      []string{"123", "456"},
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
				err = au.Write(approved.UserInfo{UserID: id, UserName: "name_" + id})
				require.NoError(t, err)
			}

			res, err := au.Read()
			require.NoError(t, err)
			assert.Equal(t, len(tt.expected), len(res))
		})
	}
}

func TestApprovedUsers_Read(t *testing.T) {
	db, e := NewSqliteDB(":memory:")
	require.NoError(t, e)
	au, e := NewApprovedUsers(db)
	require.NoError(t, e)

	t.Run("empty", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)
		users, err := au.Read()
		require.NoError(t, err)
		assert.Equal(t, []approved.UserInfo{}, users)
	})

	t.Run("direct writes", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		_, err = db.Exec("INSERT INTO approved_users (id, name, timestamp) VALUES (?, ?, ?)",
			"123", "John Doe", time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)

		_, err = db.Exec("INSERT INTO approved_users (id, name, timestamp) VALUES (?, ?, ?)",
			"456", "Jane Doe", time.Date(2023, 10, 3, 0, 0, 0, 0, time.UTC))
		require.NoError(t, err)

		users, err := au.Read()
		require.NoError(t, err)
		assert.Equal(t, []approved.UserInfo{
			{UserID: "456", UserName: "Jane Doe", Timestamp: time.Date(2023, 10, 3, 0, 0, 0, 0, time.UTC)},
			{UserID: "123", UserName: "John Doe", Timestamp: time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)},
		}, users)
	})

	t.Run("write via UserInfo", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		require.NoError(t, err)
		err = au.Write(approved.UserInfo{UserID: "123", UserName: "John Doe", Timestamp: time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)})
		require.NoError(t, err)

		err = au.Write(approved.UserInfo{UserID: "456", UserName: "Jane Doe", Timestamp: time.Date(2023, 10, 3, 0, 0, 0, 0, time.UTC)})
		require.NoError(t, err)

		users, err := au.Read()
		require.NoError(t, err)
		assert.Equal(t, []approved.UserInfo{
			{UserID: "456", UserName: "Jane Doe", Timestamp: time.Date(2023, 10, 3, 0, 0, 0, 0, time.UTC)},
			{UserID: "123", UserName: "John Doe", Timestamp: time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)},
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
		err = au.Write(approved.UserInfo{UserID: "123", UserName: "John Doe"})
		require.NoError(t, err)

		err = au.Delete("123")
		require.NoError(t, err)

		var user approvedUsersInfo
		err = db.Get(&user, "SELECT id, name, timestamp FROM approved_users WHERE id = ?", 123)
		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "no rows in result set"))
	})

	t.Run("delete non-existing user", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		err = au.Delete("123")
		require.Error(t, err)
	})
}
