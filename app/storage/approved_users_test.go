package storage

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/approved"
)

func TestApprovedUsers_NewApprovedUsers(t *testing.T) {
	t.Run("create new table", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()

		_, err := NewApprovedUsers(context.Background(), db)
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
		db, teardown := setupTestDB(t)
		defer teardown()

		// Create table with updated columns
		_, err := db.Exec(`CREATE TABLE approved_users (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        uid TEXT NOT NULL UNIQUE,
        gid TEXT DEFAULT '',
        name TEXT,
        timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
    )`)
		require.NoError(t, err)

		_, err = NewApprovedUsers(context.Background(), db)
		require.NoError(t, err)

		// Verify that the existing structure has not changed
		var columnCount int
		err = db.Get(&columnCount, "SELECT COUNT(*) FROM pragma_table_info('approved_users')")
		require.NoError(t, err)
		assert.Equal(t, 5, columnCount) // id, uid, gid, name, timestamp
	})
}

func TestApprovedUsers_Write(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	au, e := NewApprovedUsers(ctx, db)
	require.NoError(t, e)

	tests := []struct {
		name    string
		user    approved.UserInfo
		verify  func(t *testing.T, db *sqlx.DB, user approved.UserInfo)
		wantErr bool
	}{
		{
			name: "write new user with group",
			user: approved.UserInfo{
				UserID:   "123",
				UserName: "John Doe",
				GroupID:  "admin",
			},
			verify: func(t *testing.T, db *sqlx.DB, user approved.UserInfo) {
				var saved approvedUsersInfo
				err := db.Get(&saved, "SELECT uid, gid, name FROM approved_users WHERE uid = ?", user.UserID)
				require.NoError(t, err)
				assert.Equal(t, user.UserName, saved.UserName)
				assert.Equal(t, user.GroupID, saved.GroupID)
			},
		},
		{
			name: "write user without group",
			user: approved.UserInfo{
				UserID:   "456",
				UserName: "Jane Doe",
			},
			verify: func(t *testing.T, db *sqlx.DB, user approved.UserInfo) {
				var saved approvedUsersInfo
				err := db.Get(&saved, "SELECT uid, gid, name FROM approved_users WHERE uid = ?", user.UserID)
				require.NoError(t, err)
				assert.Equal(t, user.UserName, saved.UserName)
				assert.Equal(t, user.GroupID, saved.GroupID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := db.Exec("DELETE FROM approved_users")
			require.NoError(t, err)

			err = au.Write(ctx, tt.user)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.verify != nil {
				tt.verify(t, &db.DB, tt.user)
			}
		})
	}
}

func TestApprovedUsers_Read(t *testing.T) {
	db, e := NewSqliteDB(":memory:")
	require.NoError(t, e)
	ctx := context.Background()
	au, e := NewApprovedUsers(ctx, db)
	require.NoError(t, e)

	testTime := time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)

	// write test data
	users := []approved.UserInfo{
		{UserID: "123", UserName: "John", GroupID: "gr1", Timestamp: testTime},
		{UserID: "456", UserName: "Jane", GroupID: "gr2", Timestamp: testTime},
	}
	for _, u := range users {
		err := au.Write(ctx, u)
		require.NoError(t, err)
	}

	t.Run("read users with gr1", func(t *testing.T) {
		users, err := au.Read(ctx, "gr1")
		require.NoError(t, err)
		require.Len(t, users, 1)
		assert.Equal(t, users[0].UserID, "123")
	})

	t.Run("read users with gr2", func(t *testing.T) {
		users, err := au.Read(ctx, "gr2")
		require.NoError(t, err)
		require.Len(t, users, 1)
		assert.Equal(t, users[0].UserID, "456")
	})

	t.Run("read user from non-existing group", func(t *testing.T) {
		users, err := au.Read(ctx, "non-existing")
		require.NoError(t, err)
		require.Len(t, users, 0)
	})
}

func TestApprovedUsers_Delete(t *testing.T) {
	db, e := NewSqliteDB(":memory:")
	require.NoError(t, e)
	ctx := context.Background()
	au, e := NewApprovedUsers(ctx, db)
	require.NoError(t, e)

	t.Run("delete user with group", func(t *testing.T) {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)

		user := approved.UserInfo{
			UserID:   "123",
			UserName: "John",
			GroupID:  "admin",
		}
		err = au.Write(ctx, user)
		require.NoError(t, err)

		err = au.Delete(ctx, user.UserID)
		require.NoError(t, err)

		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM approved_users WHERE uid = ?", user.UserID)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
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
	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := NewSqliteDB(":memory:")
			require.NoError(t, err)
			au, err := NewApprovedUsers(ctx, db)
			require.NoError(t, err)

			for _, id := range tt.ids {
				err = au.Write(ctx, approved.UserInfo{UserID: id, UserName: "name_" + id, GroupID: "gr1"})
				require.NoError(t, err)
			}

			res, err := au.Read(ctx, "gr1")
			require.NoError(t, err)
			assert.Equal(t, len(tt.expected), len(res))
		})
	}
}

func TestApprovedUsers_ContextCancellation(t *testing.T) {
	db, err := NewSqliteDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	ctx := context.Background()
	au, err := NewApprovedUsers(ctx, db)
	require.NoError(t, err)

	t.Run("new with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := NewApprovedUsers(ctx, db)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("read with cancelled context", func(t *testing.T) {
		// prepare data
		err := au.Write(ctx, approved.UserInfo{UserID: "123", UserName: "test", GroupID: "gr1"})
		require.NoError(t, err)

		ctxCanceled, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = au.Read(ctxCanceled, "gr1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("write with cancelled context", func(t *testing.T) {
		ctxCanceled, cancel := context.WithCancel(context.Background())
		cancel()

		err := au.Write(ctxCanceled, approved.UserInfo{UserID: "456", UserName: "test"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("delete with cancelled context", func(t *testing.T) {
		// prepare data
		err := au.Write(ctx, approved.UserInfo{UserID: "789", UserName: "test"})
		require.NoError(t, err)

		ctxCanceled, cancel := context.WithCancel(context.Background())
		cancel()

		err = au.Delete(ctxCanceled, "789")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})

	t.Run("context timeout", func(t *testing.T) {
		ctxTimeout, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
		defer cancel()
		time.Sleep(time.Millisecond)

		err := au.Write(ctxTimeout, approved.UserInfo{UserID: "timeout", UserName: "test"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context deadline exceeded")
	})
}

func TestApprovedUsers_Migrate(t *testing.T) {

	t.Run("migrate from old schema with string id", func(t *testing.T) {
		db, err := NewSqliteDB(":memory:")
		require.NoError(t, err)
		defer db.Close()

		// setup old schema
		_, err = db.Exec(`
            CREATE TABLE approved_users (
                id TEXT PRIMARY KEY,
                name TEXT,
                timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
            )
        `)
		require.NoError(t, err)

		// insert test data in old format
		_, err = db.Exec("INSERT INTO approved_users (id, name) VALUES (?, ?), (?, ?)", "user1", "John", "user2", "Jane")
		require.NoError(t, err)

		// run migration
		err = migrateTable(&db.DB)
		require.NoError(t, err)

		// verify structure
		var cols []struct {
			CID       int     `db:"cid"`
			Name      string  `db:"name"`
			Type      string  `db:"type"`
			NotNull   bool    `db:"notnull"`
			DfltValue *string `db:"dflt_value"`
			PK        bool    `db:"pk"`
		}
		err = db.Select(&cols, "PRAGMA table_info(approved_users)")
		require.NoError(t, err)

		colMap := make(map[string]string)
		for _, col := range cols {
			colMap[col.Name] = col.Type
		}
		assert.Equal(t, "TEXT", colMap["uid"])
		assert.Equal(t, "TEXT", colMap["gid"])

		// verify data migrated correctly
		var users []struct {
			UID  string `db:"uid"`
			Name string `db:"name"`
			GID  string `db:"gid"`
		}
		err = db.Select(&users, "SELECT uid, name, gid FROM approved_users ORDER BY uid")
		require.NoError(t, err)
		assert.Len(t, users, 2)

		assert.Equal(t, "user1", users[0].UID)
		assert.Equal(t, "John", users[0].Name)
		assert.Equal(t, "", users[0].GID)

		assert.Equal(t, "user2", users[1].UID)
		assert.Equal(t, "Jane", users[1].Name)
		assert.Equal(t, "", users[1].GID)
	})

	t.Run("migration not needed", func(t *testing.T) {
		db, err := NewSqliteDB(":memory:")
		require.NoError(t, err)
		defer db.Close()

		// setup new schema
		_, err = db.Exec(approvedUsersSchema)
		require.NoError(t, err)

		// run migration
		err = migrateTable(&db.DB)
		require.NoError(t, err)

		// verify structure
		var cols []struct {
			CID       int     `db:"cid"`
			Name      string  `db:"name"`
			Type      string  `db:"type"`
			NotNull   bool    `db:"notnull"`
			DfltValue *string `db:"dflt_value"`
			PK        bool    `db:"pk"`
		}
		err = db.Select(&cols, "PRAGMA table_info(approved_users)")
		require.NoError(t, err)

		colMap := make(map[string]string)
		for _, col := range cols {
			colMap[col.Name] = col.Type
		}
		assert.Equal(t, "TEXT", colMap["uid"])
		assert.Equal(t, "TEXT", colMap["gid"])
	})
}
