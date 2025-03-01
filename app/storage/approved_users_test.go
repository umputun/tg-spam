package storage

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/approved"
)

func (s *StorageTestSuite) TestApprovedUsers_NewApprovedUsers() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			s.Run("create new table", func() {
				_, err := NewApprovedUsers(ctx, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE approved_users")

				// check if the table exists - use db-agnostic way
				var exists int
				err = db.Get(&exists, `SELECT COUNT(*) FROM approved_users`) // simpler check
				s.Require().NoError(err)
				s.Equal(0, exists) // empty but exists
			})

			s.Run("table already exists", func() {
				if db.Type() != engine.Sqlite {
					s.T().Skip("skipping for non-sqlite database")
				}
				defer db.Exec("DROP TABLE approved_users")

				// create table with updated columns
				_, err := db.Exec(`CREATE TABLE approved_users (
                    id INTEGER PRIMARY KEY AUTOINCREMENT,
                    uid TEXT NOT NULL UNIQUE,
                    gid TEXT DEFAULT '',
                    name TEXT,
                    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
                )`)
				s.Require().NoError(err)

				_, err = NewApprovedUsers(ctx, db)
				s.Require().NoError(err)

				// verify that the existing structure has not changed
				var columnCount int
				err = db.Get(&columnCount, "SELECT COUNT(*) FROM pragma_table_info('approved_users')")
				s.Require().NoError(err)
				s.Equal(5, columnCount) // id, uid, gid, name, timestamp
			})

			s.Run("nil db connection", func() {
				_, err := NewApprovedUsers(ctx, nil)
				s.Require().Error(err)
				defer db.Exec("DROP TABLE approved_users")

				s.Contains(err.Error(), "db connection is nil")
			})

			s.Run("context cancelled", func() {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				_, err := NewApprovedUsers(ctx, db)
				s.Require().Error(err)
				defer db.Exec("DROP TABLE approved_users")
			})

			s.Run("commit after migration preserves data", func() {
				if db.Type() != engine.Sqlite {
					s.T().Skip("skipping for non-sqlite database")
				}
				// create old schema
				_, err := db.Exec(`
                    CREATE TABLE approved_users (
                        id TEXT PRIMARY KEY,
                        name TEXT,
                        timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
                    )
                `)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE approved_users")

				// verify old schema exists
				var exists int
				err = db.Get(&exists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='approved_users'")
				s.Require().NoError(err)
				s.Equal(1, exists)

				oldTime := time.Now().Add(-time.Hour).UTC()
				_, err = db.Exec("INSERT INTO approved_users (id, name, timestamp) VALUES (?, ?, ?)", "user1", "test", oldTime)
				s.Require().NoError(err)

				// run migration through NewApprovedUsers
				au, err := NewApprovedUsers(ctx, db)
				s.Require().NoError(err)

				// verify data migrated correctly
				users, err := au.Read(ctx)
				s.Require().NoError(err)
				s.Require().Len(users, 1)
				s.Equal("user1", users[0].UserID)
				s.Equal("test", users[0].UserName)
				s.Equal(oldTime.Unix(), users[0].Timestamp.Unix())
			})
		})
	}
}

func (s *StorageTestSuite) TestApprovedUsers_Write() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			tests := []struct {
				name    string
				user    approved.UserInfo
				verify  func(s *StorageTestSuite, db *engine.SQL, user approved.UserInfo)
				wantErr bool
			}{
				{
					name: "write new user with group",
					user: approved.UserInfo{
						UserID:   "123",
						UserName: "John Doe",
					},
					verify: func(s *StorageTestSuite, db *engine.SQL, user approved.UserInfo) {
						var saved approvedUsersInfo
						query := db.Adopt("SELECT uid, name FROM approved_users WHERE uid = ?")
						err := db.Get(&saved, query, user.UserID)
						s.Require().NoError(err)
						s.Equal(user.UserName, saved.UserName)
					},
				},
				{
					name: "write user without group",
					user: approved.UserInfo{
						UserID:   "456",
						UserName: "Jane Doe",
					},
					verify: func(s *StorageTestSuite, db *engine.SQL, user approved.UserInfo) {
						var saved approvedUsersInfo
						query := db.Adopt("SELECT uid, name FROM approved_users WHERE uid = ?")
						err := db.Get(&saved, query, user.UserID)
						s.Require().NoError(err)
						s.Equal(user.UserName, saved.UserName)
					},
				},
			}

			for _, tt := range tests {
				s.Run(tt.name, func() {
					au, err := NewApprovedUsers(ctx, db)
					s.Require().NoError(err)
					defer db.Exec("DROP TABLE approved_users")

					err = au.Write(ctx, tt.user)
					if tt.wantErr {
						s.Require().Error(err)
						return
					}
					s.Require().NoError(err)

					if tt.verify != nil {
						tt.verify(s, db, tt.user)
					}
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestApprovedUsers_Read() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			defer db.Exec("DROP TABLE approved_users")

			au, err := NewApprovedUsers(ctx, db)
			s.Require().NoError(err)

			testTime := time.Date(2023, 10, 2, 0, 0, 0, 0, time.UTC)

			// write test data
			users := []approved.UserInfo{
				{UserID: "123", UserName: "John", Timestamp: testTime},
				{UserID: "456", UserName: "Jane", Timestamp: testTime},
			}
			for _, u := range users {
				err := au.Write(ctx, u)
				s.Require().NoError(err)
			}

			// read users with group id
			users, err = au.Read(ctx)
			s.Require().NoError(err)
			s.Require().Len(users, 2)
			s.Equal(users[0].UserID, "123")
		})
	}
}

func (s *StorageTestSuite) TestApprovedUsers_Delete() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			defer db.Exec("DROP TABLE approved_users")

			au, err := NewApprovedUsers(ctx, db)
			s.Require().NoError(err)

			s.Run("delete user with group", func() {
				_, err := db.Exec("DELETE FROM approved_users")
				s.Require().NoError(err)

				user := approved.UserInfo{
					UserID:   "123",
					UserName: "John",
				}
				err = au.Write(ctx, user)
				s.Require().NoError(err)

				err = au.Delete(ctx, user.UserID)
				s.Require().NoError(err)

				var count int
				query := db.Adopt("SELECT COUNT(*) FROM approved_users WHERE uid = ?")
				err = db.Get(&count, query, user.UserID)
				s.Require().NoError(err)
				s.Equal(0, count)
			})
		})
	}
}
func (s *StorageTestSuite) TestApprovedUsers_StoreAndRead() {
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
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			for _, tt := range tests {
				s.Run(tt.name, func() {
					defer db.Exec("DROP TABLE approved_users")

					au, err := NewApprovedUsers(ctx, db)
					s.Require().NoError(err)

					for _, id := range tt.ids {
						err = au.Write(ctx, approved.UserInfo{UserID: id, UserName: "name_" + id})
						s.Require().NoError(err)
					}

					res, err := au.Read(ctx)
					s.Require().NoError(err)
					s.Equal(len(tt.expected), len(res))
				})
			}
		})
	}
}

func (s *StorageTestSuite) TestApprovedUsers_ContextCancellation() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			defer db.Exec("DROP TABLE approved_users")

			au, err := NewApprovedUsers(ctx, db)
			s.Require().NoError(err)

			s.Run("new with cancelled context", func() {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				_, err := NewApprovedUsers(ctx, db)
				s.Require().Error(err)
				s.Contains(err.Error(), "context canceled")
			})

			s.Run("read with cancelled context", func() {
				// prepare data
				err := au.Write(ctx, approved.UserInfo{UserID: "123", UserName: "test"})
				s.Require().NoError(err)

				ctxCanceled, cancel := context.WithCancel(context.Background())
				cancel()

				_, err = au.Read(ctxCanceled)
				s.Require().Error(err)
				s.Contains(err.Error(), "context canceled")
			})

			s.Run("write with cancelled context", func() {
				ctxCanceled, cancel := context.WithCancel(context.Background())
				cancel()

				err := au.Write(ctxCanceled, approved.UserInfo{UserID: "456", UserName: "test"})
				s.Require().Error(err)
				s.Contains(err.Error(), "context canceled")
			})

			s.Run("delete with cancelled context", func() {
				// prepare data
				err := au.Write(ctx, approved.UserInfo{UserID: "789", UserName: "test"})
				s.Require().NoError(err)

				ctxCanceled, cancel := context.WithCancel(context.Background())
				cancel()

				err = au.Delete(ctxCanceled, "789")
				s.Require().Error(err)
				s.Contains(err.Error(), "context canceled")
			})

			s.Run("context timeout", func() {
				ctxTimeout, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
				defer cancel()
				time.Sleep(time.Millisecond)

				err := au.Write(ctxTimeout, approved.UserInfo{UserID: "timeout", UserName: "test"})
				s.Require().Error(err)
				s.Contains(err.Error(), "context deadline exceeded")
			})
		})
	}
}
func (s *StorageTestSuite) TestApprovedUsers_ErrorCases() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			au, err := NewApprovedUsers(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE approved_users")

			clearDB := func() {
				_, err := db.Exec(db.Adopt("DELETE FROM approved_users"))
				s.Require().NoError(err)
			}

			s.Run("empty user id", func() {
				clearDB()
				user := approved.UserInfo{
					UserName: "test",
				}
				err := au.Write(ctx, user)
				s.Require().Error(err)
				s.Equal("user id can't be empty", err.Error())
			})

			s.Run("empty username should be valid", func() {
				clearDB()
				user := approved.UserInfo{
					UserID: "123",
				}
				err := au.Write(ctx, user)
				s.Require().NoError(err)
			})

			s.Run("delete non-existent", func() {
				clearDB()
				err := au.Delete(ctx, "non-existent")
				s.Require().Error(err)
				s.Contains(err.Error(), "failed to get approved user")
			})

			s.Run("delete empty id", func() {
				clearDB()
				err := au.Delete(ctx, "")
				s.Require().Error(err)
				s.Equal("user id can't be empty", err.Error())
			})

			s.Run("concurrent write same user", func() {
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
				s.Empty(errs)

				// verify only one record exists
				users, err := au.Read(ctx)
				s.Require().NoError(err)
				s.Require().Len(users, 1, "should only have one user record")
				s.Equal(user.UserID, users[0].UserID)
				s.Equal(user.UserName, users[0].UserName)
			})
		})
	}
}

func (s *StorageTestSuite) TestApprovedUsers_Cleanup() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			au, err := NewApprovedUsers(ctx, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE approved_users")

			s.Run("cleanup on migration", func() {
				// insert some data with invalid format
				_, err := db.Exec(db.Adopt("INSERT INTO approved_users (uid, name) VALUES (?, ?)"), "", "invalid")
				s.Require().NoError(err)

				// trigger migration
				au2, err := NewApprovedUsers(ctx, db)
				s.Require().NoError(err)

				// verify invalid data is not visible
				users, err := au2.Read(ctx)
				s.Require().NoError(err)
				s.Empty(users)
			})

			s.Run("handle invalid data in read", func() {
				query := "INSERT INTO approved_users (uid, name, timestamp) VALUES (?, ?, ?)"
				if db.Type() == engine.Postgres {
					query = "INSERT INTO approved_users (uid, name, timestamp) VALUES ($1, $2, $3::timestamp)"
				}

				// insert invalid timestamp
				_, err := db.Exec(query, "123", "test", "invalid-time")
				if db.Type() == engine.Postgres {
					s.T().Skip("postgres prevents invalid timestamp format")
				}
				s.Require().NoError(err)

				// reading should skip invalid entries
				users, err := au.Read(ctx)
				s.Require().NoError(err)
				s.Empty(users)
			})
		})
	}
}

func (s *StorageTestSuite) TestApprovedUsers_Migrate() {
	ctx := context.Background()

	s.Run("migrate from old sqlite schema", func() {
		db, err := engine.NewSqlite(":memory:", "gr1")
		s.Require().NoError(err)
		defer db.Close()

		// create old schema without uid and gid
		_, err = db.Exec(`
            CREATE TABLE approved_users (
                id TEXT PRIMARY KEY,
                name TEXT,
                timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
            )
        `)
		s.Require().NoError(err)

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
			s.Require().NoError(err)
		}

		// verify initial data
		var count int
		err = db.Get(&count, "SELECT COUNT(*) FROM approved_users")
		s.Require().NoError(err)
		s.Assert().Equal(2, count)

		// run migration via NewApprovedUsers
		au, err := NewApprovedUsers(ctx, db)
		s.Require().NoError(err)

		// verify migrated data
		users, err := au.Read(ctx)
		s.Require().NoError(err)
		s.Assert().Equal(2, len(users))

		// check data preserved and moved correctly
		s.Assert().Equal("test1", users[0].UserName)
		s.Assert().Equal("123", users[0].UserID)
		s.Assert().Equal("test2", users[1].UserName)
		s.Assert().Equal("456", users[1].UserID)

		// verify table structure
		var cols []string
		rows, err := db.Query("SELECT * FROM approved_users LIMIT 1")
		s.Require().NoError(err)
		cols, err = rows.Columns()
		s.Require().NoError(err)
		rows.Close()

		// check all needed columns exist
		expected := []string{"id", "uid", "gid", "name", "timestamp"}
		for _, col := range expected {
			s.Assert().Contains(cols, col)
		}
	})

	s.Run("no migration needed for new schema", func() {
		db, err := engine.NewSqlite(":memory:", "gr1")
		s.Require().NoError(err)
		defer db.Close()

		au1, err := NewApprovedUsers(ctx, db)
		s.Require().NoError(err)

		// add user with new schema
		err = au1.Write(ctx, approved.UserInfo{UserID: "123", UserName: "test"})
		s.Require().NoError(err)

		// second instance should not trigger migration
		au2, err := NewApprovedUsers(ctx, db)
		s.Require().NoError(err)

		users, err := au2.Read(ctx)
		s.Require().NoError(err)
		s.Assert().Equal(1, len(users))
		s.Assert().Equal("test", users[0].UserName)
		s.Assert().Equal("123", users[0].UserID)
	})

	s.Run("double migration attempt", func() {
		db, err := engine.NewSqlite(":memory:", "gr1")
		s.Require().NoError(err)
		defer db.Close()

		// create old schema first
		_, err = db.Exec(`
            CREATE TABLE approved_users (
                id TEXT PRIMARY KEY,
                name TEXT,
                timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
            )
        `)
		s.Require().NoError(err)

		// insert test data
		_, err = db.Exec("INSERT INTO approved_users (id, name) VALUES ('123', 'test')")
		s.Require().NoError(err)

		// run first migration
		_, err = NewApprovedUsers(ctx, db)
		s.Require().NoError(err)

		// try second migration
		au2, err := NewApprovedUsers(ctx, db)
		s.Require().NoError(err)

		// verify data preserved
		users, err := au2.Read(ctx)
		s.Require().NoError(err)
		s.Assert().Equal(1, len(users))
		s.Assert().Equal("test", users[0].UserName)
		s.Assert().Equal("123", users[0].UserID)
	})

	s.Run("migration preserves indices", func() {
		db, err := engine.NewSqlite(":memory:", "gr1")
		s.Require().NoError(err)
		defer db.Close()

		_, err = NewApprovedUsers(ctx, db)
		s.Require().NoError(err)

		var indices []struct {
			Name string `db:"name"`
		}
		err = db.Select(&indices, "SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'approved_users'")
		s.Require().NoError(err)

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
			s.Assert().True(found, "index %s not found", expected)
		}
	})
}
