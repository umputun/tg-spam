package storage

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/approved"
)

func TestApprovedUsers_NewApprovedUsers(t *testing.T) {
	t.Run("create new table", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()

		_, err := NewApprovedUsers(context.Background(), db)
		require.NoError(t, err)

		// check if the table exists - use db-agnostic way
		var exists int
		err = db.Get(&exists, `SELECT COUNT(*) FROM approved_users`) // simpler check
		require.NoError(t, err)
		assert.Equal(t, 0, exists) // empty but exists
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

	t.Run("nil db connection", func(t *testing.T) {
		_, err := NewApprovedUsers(context.Background(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "db connection is nil")
	})

	t.Run("context cancelled", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := NewApprovedUsers(ctx, db)
		require.Error(t, err)
	})

	t.Run("commit after migration preserves data", func(t *testing.T) {
		db, teardown := setupTestDB(t)
		defer teardown()

		// create old schema
		_, err := db.Exec(`
            CREATE TABLE approved_users (
                id TEXT PRIMARY KEY,
                name TEXT,
                timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
            )
        `)
		require.NoError(t, err)

		// verify old schema exists
		var exists int
		err = db.Get(&exists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='approved_users'")
		require.NoError(t, err)
		assert.Equal(t, 1, exists)

		oldTime := time.Now().Add(-time.Hour).UTC()
		_, err = db.Exec("INSERT INTO approved_users (id, name, timestamp) VALUES (?, ?, ?)", "user1", "test", oldTime)
		require.NoError(t, err)

		// run migration through NewApprovedUsers
		au, err := NewApprovedUsers(context.Background(), db)
		require.NoError(t, err)

		// verify data migrated correctly
		users, err := au.Read(context.Background())
		require.NoError(t, err)
		require.Len(t, users, 1)
		assert.Equal(t, "user1", users[0].UserID)
		assert.Equal(t, "test", users[0].UserName)
		assert.Equal(t, oldTime.Unix(), users[0].Timestamp.Unix())
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
			},
			verify: func(t *testing.T, db *sqlx.DB, user approved.UserInfo) {
				var saved approvedUsersInfo
				err := db.Get(&saved, "SELECT uid, name FROM approved_users WHERE uid = ?", user.UserID)
				require.NoError(t, err)
				assert.Equal(t, user.UserName, saved.UserName)
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
				err := db.Get(&saved, "SELECT uid, name FROM approved_users WHERE uid = ?", user.UserID)
				require.NoError(t, err)
				assert.Equal(t, user.UserName, saved.UserName)
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
	db, e := engine.NewSqlite(":memory:", "gr1")
	require.NoError(t, e)
	ctx := context.Background()
	au, e := NewApprovedUsers(ctx, db)
	require.NoError(t, e)

	testTime := time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)

	// write test data
	users := []approved.UserInfo{
		{UserID: "123", UserName: "John", Timestamp: testTime},
		{UserID: "456", UserName: "Jane", Timestamp: testTime},
	}
	for _, u := range users {
		err := au.Write(ctx, u)
		require.NoError(t, err)
	}

	t.Run("read users with gr1", func(t *testing.T) {
		users, err := au.Read(ctx)
		require.NoError(t, err)
		require.Len(t, users, 2)
		assert.Equal(t, users[0].UserID, "123")
	})
}

func TestApprovedUsers_Delete(t *testing.T) {
	db, e := engine.NewSqlite(":memory:", "gr1")
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
			db, err := engine.NewSqlite(":memory:", "gr1")
			require.NoError(t, err)
			au, err := NewApprovedUsers(ctx, db)
			require.NoError(t, err)

			for _, id := range tt.ids {
				err = au.Write(ctx, approved.UserInfo{UserID: id, UserName: "name_" + id})
				require.NoError(t, err)
			}

			res, err := au.Read(ctx)
			require.NoError(t, err)
			assert.Equal(t, len(tt.expected), len(res))
		})
	}
}

func TestApprovedUsers_ContextCancellation(t *testing.T) {
	db, err := engine.NewSqlite(":memory:", "gr1")
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
		err := au.Write(ctx, approved.UserInfo{UserID: "123", UserName: "test"})
		require.NoError(t, err)

		ctxCanceled, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = au.Read(ctxCanceled)
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

func TestApprovedUsers_DetailedGroupIsolation(t *testing.T) {
	ctx := context.Background()

	db1, err := engine.NewSqlite(":memory:", "gr1")
	require.NoError(t, err)
	defer db1.Close()

	db2, err := engine.NewSqlite(":memory:", "gr2")
	require.NoError(t, err)
	defer db2.Close()

	au1, err := NewApprovedUsers(ctx, db1)
	require.NoError(t, err)
	au2, err := NewApprovedUsers(ctx, db2)
	require.NoError(t, err)

	// Add user to both groups
	user := approved.UserInfo{
		UserID:   "123",
		UserName: "test_user",
	}
	err = au1.Write(ctx, user)
	require.NoError(t, err)
	err = au2.Write(ctx, user)
	require.NoError(t, err)

	// Verify data in databases directly
	var data1, data2 []struct {
		UID  string `db:"uid"`
		GID  string `db:"gid"`
		Name string `db:"name"`
	}

	err = db1.Select(&data1, "SELECT uid, gid, name FROM approved_users WHERE gid = ?", "gr1")
	require.NoError(t, err)
	t.Logf("DB1 data: %+v", data1)
	err = db2.Select(&data2, "SELECT uid, gid, name FROM approved_users WHERE gid = ?", "gr2")
	require.NoError(t, err)
	t.Logf("DB2 data: %+v", data2)

	// Modify in first group
	user.UserName = "modified"
	err = au1.Write(ctx, user)
	require.NoError(t, err)

	// Check raw data again
	err = db1.Select(&data1, "SELECT uid, gid, name FROM approved_users WHERE gid = ?", "gr1")
	require.NoError(t, err)
	t.Logf("DB1 data after modification: %+v", data1)
	err = db2.Select(&data2, "SELECT uid, gid, name FROM approved_users WHERE gid = ?", "gr2")
	require.NoError(t, err)
	t.Logf("DB2 data after modification: %+v", data2)

	// Final verification through the API
	users1, err := au1.Read(ctx)
	require.NoError(t, err)
	t.Logf("API read from group1: %+v", users1)
	users2, err := au2.Read(ctx)
	require.NoError(t, err)
	t.Logf("API read from group2: %+v", users2)

	require.Len(t, users1, 1)
	require.Len(t, users2, 1)
	assert.Equal(t, "modified", users1[0].UserName)
	assert.Equal(t, "test_user", users2[0].UserName)
}

func TestApprovedUsers_ErrorCases(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	au, err := NewApprovedUsers(ctx, db)
	require.NoError(t, err)

	clearDB := func() {
		_, err := db.Exec("DELETE FROM approved_users")
		require.NoError(t, err)
	}

	t.Run("empty user id", func(t *testing.T) {
		clearDB()
		user := approved.UserInfo{
			UserName: "test",
		}
		err := au.Write(ctx, user)
		require.Error(t, err)
		assert.Equal(t, "user id can't be empty", err.Error())
	})

	t.Run("empty username should be valid", func(t *testing.T) {
		clearDB()
		user := approved.UserInfo{
			UserID: "123",
		}
		err := au.Write(ctx, user)
		require.NoError(t, err)
	})

	t.Run("delete non-existent", func(t *testing.T) {
		clearDB()
		err := au.Delete(ctx, "non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get approved user")
	})

	t.Run("delete empty id", func(t *testing.T) {
		clearDB()
		err := au.Delete(ctx, "")
		require.Error(t, err)
		assert.Equal(t, "user id can't be empty", err.Error())
	})

	t.Run("concurrent write same user", func(t *testing.T) {
		clearDB()
		user := approved.UserInfo{
			UserID:    "456",
			UserName:  "test",
			Timestamp: time.Now(),
		}

		var wg sync.WaitGroup
		errCh := make(chan error, 2)

		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := au.Write(ctx, user); err != nil {
					errCh <- err
				}
			}()
		}

		wg.Wait()
		close(errCh)

		// collect any errors
		var errs []error
		for err := range errCh {
			errs = append(errs, err)
		}
		assert.Empty(t, errs)

		// verify only one record exists
		users, err := au.Read(ctx)
		require.NoError(t, err)
		require.Len(t, users, 1, "should only have one user record")
		assert.Equal(t, user.UserID, users[0].UserID)
		assert.Equal(t, user.UserName, users[0].UserName)
	})
}

func TestApprovedUsers_Cleanup(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	au, err := NewApprovedUsers(ctx, db)
	require.NoError(t, err)

	t.Run("cleanup on migration", func(t *testing.T) {
		// insert some data with invalid format
		_, err := db.Exec("INSERT INTO approved_users (uid, name) VALUES (?, ?)", "", "invalid")
		require.NoError(t, err)

		// trigger migration
		au2, err := NewApprovedUsers(ctx, db)
		require.NoError(t, err)

		// verify invalid data is not visible
		users, err := au2.Read(ctx)
		require.NoError(t, err)
		assert.Empty(t, users)
	})

	t.Run("handle invalid data in read", func(t *testing.T) {
		// insert invalid timestamp
		_, err := db.Exec("INSERT INTO approved_users (uid, name, timestamp) VALUES (?, ?, ?)",
			"123", "test", "invalid-time")
		require.NoError(t, err)

		// reading should skip invalid entries
		users, err := au.Read(ctx)
		require.NoError(t, err)
		assert.Empty(t, users)
	})
}

func TestApprovedUsers_Migrate(t *testing.T) {
	ctx := context.Background()

	t.Run("migrate from old sqlite schema", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()

		// create old schema without uid and gid
		_, err = db.Exec(`
            CREATE TABLE approved_users (
                id TEXT PRIMARY KEY,
                name TEXT,
                timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
            )
        `)
		require.NoError(t, err)

		testData := []struct {
			id   string
			name string
		}{
			{"123", "test1"},
			{"456", "test2"},
		}

		// add test data
		for _, tc := range testData {
			_, err := db.Exec("INSERT INTO approved_users (id, name) VALUES (?, ?)", tc.id, tc.name)
			require.NoError(t, err)
		}

		// verify initial data
		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM approved_users")
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		// run migration via NewApprovedUsers
		au, err := NewApprovedUsers(ctx, db)
		require.NoError(t, err)

		// verify migrated data
		users, err := au.Read(ctx)
		require.NoError(t, err)
		assert.Equal(t, 2, len(users))

		// check data preserved and moved correctly
		assert.Equal(t, "test1", users[0].UserName)
		assert.Equal(t, "123", users[0].UserID)
		assert.Equal(t, "test2", users[1].UserName)
		assert.Equal(t, "456", users[1].UserID)

		// verify table structure
		var cols []string
		rows, err := db.Query("SELECT * FROM approved_users LIMIT 1")
		require.NoError(t, err)
		cols, err = rows.Columns()
		require.NoError(t, err)
		rows.Close()

		// check all needed columns exist
		expected := []string{"id", "uid", "gid", "name", "timestamp"}
		for _, col := range expected {
			assert.Contains(t, cols, col)
		}
	})

	t.Run("no migration needed for new schema", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()

		au1, err := NewApprovedUsers(ctx, db)
		require.NoError(t, err)

		// add user with new schema
		err = au1.Write(ctx, approved.UserInfo{UserID: "123", UserName: "test"})
		require.NoError(t, err)

		// second instance should not trigger migration
		au2, err := NewApprovedUsers(ctx, db)
		require.NoError(t, err)

		users, err := au2.Read(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, len(users))
		assert.Equal(t, "test", users[0].UserName)
		assert.Equal(t, "123", users[0].UserID)
	})

	t.Run("double migration attempt", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()

		// create old schema first
		_, err = db.Exec(`
            CREATE TABLE approved_users (
                id TEXT PRIMARY KEY,
                name TEXT,
                timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
            )
        `)
		require.NoError(t, err)

		// insert test data
		_, err = db.Exec("INSERT INTO approved_users (id, name) VALUES ('123', 'test')")
		require.NoError(t, err)

		// run first migration
		_, err = NewApprovedUsers(ctx, db)
		require.NoError(t, err)

		// try second migration
		au2, err := NewApprovedUsers(ctx, db)
		require.NoError(t, err)

		// verify data preserved
		users, err := au2.Read(ctx)
		require.NoError(t, err)
		assert.Equal(t, 1, len(users))
		assert.Equal(t, "test", users[0].UserName)
		assert.Equal(t, "123", users[0].UserID)
	})

	t.Run("migration preserves indices", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()

		_, err = NewApprovedUsers(ctx, db)
		require.NoError(t, err)

		var indices []struct {
			Name string `db:"name"`
		}
		err = db.Select(&indices, "SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'approved_users'")
		require.NoError(t, err)

		expectedIndices := []string{
			"idx_approved_users_uid",
			"idx_approved_users_gid",
			"idx_approved_users_name",
			"idx_approved_users_timestamp",
		}

		for _, expected := range expectedIndices {
			found := false
			for _, idx := range indices {
				if idx.Name == expected {
					found = true
					break
				}
			}
			assert.True(t, found, "index %s not found", expected)
		}
	})
}
