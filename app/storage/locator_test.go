package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

func (s *StorageTestSuite) TestNewLocator() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			const ttl = 10 * time.Minute
			const minSize = 1

			locator, err := NewLocator(ctx, ttl, minSize, db)
			s.Require().NoError(err)
			s.NotNil(locator)
		})
	}
}

func (s *StorageTestSuite) TestLocator_GetUserMessageIDs() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			locator, err := NewLocator(ctx, time.Hour, 1000, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE messages")
			defer db.Exec("DROP TABLE spam")

			userID := int64(100)

			// add multiple messages for the same user
			msgIDs := []int{10, 20, 30, 40, 50}
			for i, msgID := range msgIDs {
				msg := fmt.Sprintf("message%d", i)
				s.Require().NoError(locator.AddMessage(ctx, msg, 123, userID, "user1", msgID))
				time.Sleep(10 * time.Millisecond) // ensure different timestamps
			}

			// add message for different user
			s.Require().NoError(locator.AddMessage(ctx, "other message", 123, 200, "user2", 60))

			// get all message IDs for user
			ids, err := locator.GetUserMessageIDs(ctx, userID, 100)
			s.Require().NoError(err)
			s.Require().Len(ids, 5)

			// should be in reverse chronological order (newest first)
			s.Equal(50, ids[0])
			s.Equal(40, ids[1])
			s.Equal(30, ids[2])
			s.Equal(20, ids[3])
			s.Equal(10, ids[4])

			// test with limit
			ids, err = locator.GetUserMessageIDs(ctx, userID, 3)
			s.Require().NoError(err)
			s.Require().Len(ids, 3)
			s.Equal(50, ids[0])
			s.Equal(40, ids[1])
			s.Equal(30, ids[2])

			// test different user
			ids, err = locator.GetUserMessageIDs(ctx, 200, 100)
			s.Require().NoError(err)
			s.Require().Len(ids, 1)
			s.Equal(60, ids[0])

			// test non-existent user
			ids, err = locator.GetUserMessageIDs(ctx, 999, 100)
			s.Require().NoError(err)
			s.Empty(ids)
		})
	}
}

func (s *StorageTestSuite) TestLocator_CountUserMessages() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			locator, err := NewLocator(ctx, time.Hour, 1000, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE messages")
			defer db.Exec("DROP TABLE spam")

			userID := int64(100)

			// initially zero
			count, err := locator.CountUserMessages(ctx, strconv.FormatInt(userID, 10))
			s.Require().NoError(err)
			s.Equal(0, count)

			// add 3 messages for user 100
			for i, msgID := range []int{10, 20, 30} {
				s.Require().NoError(locator.AddMessage(ctx, fmt.Sprintf("msg-%d", i), 123, userID, "user1", msgID))
			}

			// add a message for a different user
			s.Require().NoError(locator.AddMessage(ctx, "other", 123, 200, "user2", 60))

			count, err = locator.CountUserMessages(ctx, strconv.FormatInt(userID, 10))
			s.Require().NoError(err)
			s.Equal(3, count)

			// verify other user counted independently
			count, err = locator.CountUserMessages(ctx, "200")
			s.Require().NoError(err)
			s.Equal(1, count)

			// non-existent user yields zero
			count, err = locator.CountUserMessages(ctx, "999")
			s.Require().NoError(err)
			s.Equal(0, count)

			// invalid user id string fails
			_, err = locator.CountUserMessages(ctx, "not-a-number")
			s.Require().Error(err)
		})
	}
}

func (s *StorageTestSuite) TestLocator_CountUserMessages_GIDIsolation() {
	db1, err := engine.NewSqlite(":memory:", "gr1")
	s.Require().NoError(err)
	defer db1.Close()

	db2, err := engine.NewSqlite(":memory:", "gr2")
	s.Require().NoError(err)
	defer db2.Close()

	ctx := context.Background()

	locator1, err := NewLocator(ctx, time.Hour, 5, db1)
	s.Require().NoError(err)
	locator2, err := NewLocator(ctx, time.Hour, 5, db2)
	s.Require().NoError(err)

	// add messages for the same user id under different gids
	s.Require().NoError(locator1.AddMessage(ctx, "m1", 1, 42, "u", 1))
	s.Require().NoError(locator1.AddMessage(ctx, "m2", 1, 42, "u", 2))
	s.Require().NoError(locator2.AddMessage(ctx, "m3", 2, 42, "u", 3))

	count1, err := locator1.CountUserMessages(ctx, "42")
	s.Require().NoError(err)
	s.Equal(2, count1, "locator1 should count only its own gid messages")

	count2, err := locator2.CountUserMessages(ctx, "42")
	s.Require().NoError(err)
	s.Equal(1, count2, "locator2 should count only its own gid messages")
}

func (s *StorageTestSuite) TestLocator_UserMessageIDs() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			locator, err := NewLocator(ctx, time.Hour, 1000, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE messages")
			defer db.Exec("DROP TABLE spam")

			userID := int64(100)
			msgIDs := []int{10, 20, 30, 40, 50}
			for i, msgID := range msgIDs {
				s.Require().NoError(locator.AddMessage(ctx, fmt.Sprintf("m-%d", i), 123, userID, "user1", msgID))
				time.Sleep(10 * time.Millisecond) // ensure ordering by time
			}

			// fetch all, newest first
			ids, err := locator.UserMessageIDs(ctx, "100", 100)
			s.Require().NoError(err)
			s.Require().Len(ids, 5)
			s.Equal([]int{50, 40, 30, 20, 10}, ids)

			// limit honored
			ids, err = locator.UserMessageIDs(ctx, "100", 3)
			s.Require().NoError(err)
			s.Equal([]int{50, 40, 30}, ids)

			// non-existent user yields empty
			ids, err = locator.UserMessageIDs(ctx, "999", 10)
			s.Require().NoError(err)
			s.Empty(ids)

			// invalid user id string fails
			_, err = locator.UserMessageIDs(ctx, "abc", 10)
			s.Require().Error(err)
		})
	}
}

func (s *StorageTestSuite) TestLocator_AddAndRetrieveMessage() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			locator, err := NewLocator(ctx, time.Hour, 1000, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE messages")
			defer db.Exec("DROP TABLE spam")

			msg := "test message"
			chatID := int64(123)
			userID := int64(456)
			userName := "user1"
			msgID := 789

			s.Require().NoError(locator.AddMessage(ctx, msg, chatID, userID, userName, msgID))

			retrievedMsg, found := locator.Message(ctx, msg)
			s.Require().True(found)
			s.Equal(MsgMeta{Time: retrievedMsg.Time, ChatID: chatID, UserID: userID, UserName: userName, MsgID: msgID}, retrievedMsg)

			res := locator.UserNameByID(ctx, userID)
			s.Equal(userName, res)

			res = locator.UserNameByID(ctx, 123456)
			s.Empty(res)

			id := locator.UserIDByName(ctx, userName)
			s.Equal(userID, id)

			id = locator.UserIDByName(ctx, "user2")
			s.Equal(int64(0), id)
		})
	}
}

func (s *StorageTestSuite) TestLocator_AddAndRetrieveManyMessage() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			locator, err := NewLocator(ctx, time.Hour, 1000, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE messages")
			defer db.Exec("DROP TABLE spam")

			// add 100 messages for 10 users
			for i := range 100 {
				userID := int64(i%10 + 1)
				s.Require().NoError(locator.AddMessage(ctx, fmt.Sprintf("test message %d", i), 1234, userID, "name"+strconv.Itoa(int(userID)), i))
			}

			for i := range 100 {
				retrievedMsg, found := locator.Message(ctx, fmt.Sprintf("test message %d", i))
				s.Require().True(found)
				s.Equal(MsgMeta{Time: retrievedMsg.Time, ChatID: int64(1234), UserID: int64(i%10 + 1), UserName: "name" + strconv.Itoa(i%10+1), MsgID: i}, retrievedMsg)
			}
		})
	}
}

func (s *StorageTestSuite) TestLocator_AddAndRetrieveSpam() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			locator, err := NewLocator(ctx, time.Hour, 1000, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE messages")
			defer db.Exec("DROP TABLE spam")

			userID := int64(456)
			checks := []spamcheck.Response{{Name: "test", Spam: true, Details: "test spam"}}

			s.Require().NoError(locator.AddSpam(ctx, userID, checks))

			retrievedSpam, found := locator.Spam(ctx, userID)
			s.Require().True(found)
			s.Equal(checks, retrievedSpam.Checks)
		})
	}
}

func (s *StorageTestSuite) TestLocator_CleanupLogic() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			ttl := 10 * time.Minute
			locator, err := NewLocator(ctx, ttl, 1, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE messages")
			defer db.Exec("DROP TABLE spam")

			oldTime := time.Now().Add(-2 * ttl) // ensure this is older than the ttl

			// add two of each to both groups, we have minSize = 1, so we should have 2 to allow cleanup
			q := `INSERT INTO messages (hash, gid, time, chat_id, user_id, user_name, msg_id) VALUES (?, ?, ?, ?, ?, ?, ?)`
			_, err = locator.Exec(db.Adopt(q), "old_hash1", locator.GID(), oldTime, int64(111), int64(222), "old_user", 333)
			s.Require().NoError(err)
			_, err = locator.Exec(db.Adopt(q), "old_hash2", locator.GID(), oldTime, int64(111), int64(222), "old_user", 333)
			s.Require().NoError(err)

			_, err = locator.Exec(db.Adopt(`INSERT INTO spam (user_id, gid, time, checks) VALUES (?, ?, ?, ?)`),
				int64(222), locator.GID(), oldTime, `[{"Name":"old_test","Spam":true,"Details":"old spam"}]`)
			s.Require().NoError(err)
			_, err = locator.Exec(db.Adopt(`INSERT INTO spam (user_id, gid, time, checks) VALUES (?, ?, ?, ?)`),
				int64(223), locator.GID(), oldTime, `[{"Name":"old_test","Spam":true,"Details":"old spam"}]`)
			s.Require().NoError(err)

			s.Require().NoError(locator.cleanupMessages(ctx))
			s.Require().NoError(locator.cleanupSpam())

			var msgCountAfter, spamCountAfter int
			err = locator.Get(&msgCountAfter, db.Adopt(`SELECT COUNT(*) FROM messages WHERE gid = ?`), locator.GID())
			s.Require().NoError(err)
			err = locator.Get(&spamCountAfter, db.Adopt(`SELECT COUNT(*) FROM spam WHERE gid = ?`), locator.GID())
			s.Require().NoError(err)

			s.Equal(0, msgCountAfter, "messages should be cleaned up")
			s.Equal(0, spamCountAfter, "spam should be cleaned up")
		})
	}
}

func (s *StorageTestSuite) TestLocator_RetrieveNonExistentMessage() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			locator, err := NewLocator(ctx, time.Hour, 1000, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE messages")
			defer db.Exec("DROP TABLE spam")

			msg := "non_existent_message"
			_, found := locator.Message(ctx, msg)
			s.False(found, "expected to not find a non-existent message")

			_, found = locator.Spam(ctx, 1234)
			s.False(found, "expected to not find a non-existent spam")
		})
	}
}

func (s *StorageTestSuite) TestLocator_SpamUnmarshalFailure() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			locator, err := NewLocator(ctx, time.Hour, 1000, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE messages")
			defer db.Exec("DROP TABLE spam")

			// insert invalid JSON data directly into the database
			userID := int64(456)
			invalidJSON := "invalid json"
			_, err = locator.Exec(db.Adopt(`INSERT INTO spam (user_id, gid, time, checks) VALUES (?, ?, ?, ?)`),
				userID, locator.GID(), time.Now(), invalidJSON)
			s.Require().NoError(err)

			// attempt to retrieve the spam data, which should fail during unmarshalling
			_, found := locator.Spam(ctx, userID)
			s.False(found, "expected to not find valid data due to unmarshalling failure")
		})
	}
}

func (s *StorageTestSuite) TestLocator_HashCollisions() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB
		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			_, err := NewLocator(ctx, time.Hour, 1000, db)
			s.Require().NoError(err)
			defer db.Exec("DROP TABLE messages")

			// force hash collision by manually inserting with same hash
			hash := "collision_hash"
			msg1 := MsgMeta{
				ChatID:   100,
				UserID:   1,
				UserName: "user1",
				MsgID:    1,
				Time:     time.Now(),
			}
			msg2 := MsgMeta{
				ChatID:   200,
				UserID:   2,
				UserName: "user2",
				MsgID:    2,
				Time:     time.Now(),
			}

			// insert first message
			q := db.Adopt(`INSERT INTO messages (hash, gid, time, chat_id, user_id, user_name, msg_id) VALUES (?, ?, ?, ?, ?, ?, ?)`)
			_, err = db.Exec(q, hash, db.GID(), msg1.Time, msg1.ChatID, msg1.UserID, msg1.UserName, msg1.MsgID)
			s.Require().NoError(err)

			// try to insert second message with same hash
			_, err = db.Exec(q, hash, db.GID(), msg2.Time, msg2.ChatID, msg2.UserID, msg2.UserName, msg2.MsgID)
			// should fail due to hash being primary key
			s.Require().Error(err)

			// verify only first message exists
			var count int
			err = db.Get(&count, db.Adopt("SELECT COUNT(*) FROM messages WHERE hash = ? AND gid = ?"), hash, db.GID())
			s.Require().NoError(err)
			s.Equal(1, count)
		})
	}
}

func (s *StorageTestSuite) TestLocator_Migration() {
	ctx := context.Background()
	for _, dbt := range s.getTestDB() {
		db := dbt.DB

		s.Run(fmt.Sprintf("with %s", db.Type()), func() {
			// pkColCount returns the number of primary-key columns for a table, engine-aware
			pkColCount := func(table string) int {
				var n int
				switch db.Type() {
				case engine.Sqlite:
					s.Require().NoError(db.Get(&n, "SELECT COUNT(*) FROM pragma_table_info('"+table+"') WHERE pk > 0"))
				case engine.Postgres:
					q := `SELECT COUNT(*) FROM information_schema.table_constraints tc
						JOIN information_schema.key_column_usage kcu
							ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
						WHERE tc.table_name = $1 AND tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = current_schema()`
					s.Require().NoError(db.Get(&n, q, table))
				}
				return n
			}

			// gidColCount returns how many gid columns a table has, engine-aware
			gidColCount := func(table string) int {
				var n int
				switch db.Type() {
				case engine.Sqlite:
					s.Require().NoError(db.Get(&n, "SELECT COUNT(*) FROM pragma_table_info('"+table+"') WHERE name = 'gid'"))
				case engine.Postgres:
					q := `SELECT COUNT(*) FROM information_schema.columns
						WHERE table_name = $1 AND column_name = 'gid' AND table_schema = current_schema()`
					s.Require().NoError(db.Get(&n, q, table))
				}
				return n
			}

			// pkColNames returns the primary-key columns in key order, engine-aware
			pkColNames := func(table string) []string {
				var names []string
				switch db.Type() {
				case engine.Sqlite:
					s.Require().NoError(db.Select(&names, "SELECT name FROM pragma_table_info('"+table+"') WHERE pk > 0 ORDER BY pk"))
				case engine.Postgres:
					q := `SELECT kcu.column_name FROM information_schema.table_constraints tc
						JOIN information_schema.key_column_usage kcu
							ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
						WHERE tc.table_name = $1 AND tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = current_schema()
						ORDER BY kcu.ordinal_position`
					s.Require().NoError(db.Select(&names, q, table))
				}
				return names
			}

			db.Exec("DROP TABLE IF EXISTS messages")
			db.Exec("DROP TABLE IF EXISTS spam")

			s.Run("migrate from old schema", func() {
				defer db.Exec("DROP TABLE IF EXISTS messages")
				defer db.Exec("DROP TABLE IF EXISTS spam")

				// setup old schema without gid, single-column primary keys (valid on both engines)
				_, err := db.Exec(`
					CREATE TABLE messages (
						hash TEXT PRIMARY KEY,
						time TIMESTAMP,
						chat_id INTEGER,
						user_id INTEGER,
						user_name TEXT,
						msg_id INTEGER
					);
					CREATE TABLE spam (
						user_id INTEGER PRIMARY KEY,
						time TIMESTAMP,
						checks TEXT
					)
				`)
				s.Require().NoError(err)

				// insert test data in old format
				_, err = db.Exec(db.Adopt(`INSERT INTO messages (hash, time, chat_id, user_id, user_name, msg_id)
					VALUES ('hash1', ?, 1, 100, 'user1', 1)`), time.Now())
				s.Require().NoError(err)

				checksJSON := `[{"Name":"test","Spam":true,"Details":"test"}]`
				_, err = db.Exec(db.Adopt(`INSERT INTO spam (user_id, time, checks) VALUES (100, ?, ?)`), time.Now(), checksJSON)
				s.Require().NoError(err)

				// run migration through NewLocator
				_, err = NewLocator(ctx, 10*time.Minute, 1, db)
				s.Require().NoError(err)

				// gid column added to both tables and backfilled with the connection's gid
				s.Equal(1, gidColCount("messages"))
				s.Equal(1, gidColCount("spam"))
				var msgGID, spamGID string
				s.Require().NoError(db.Get(&msgGID, db.Adopt("SELECT gid FROM messages WHERE hash = ?"), "hash1"))
				s.Equal("gr1", msgGID)
				s.Require().NoError(db.Get(&spamGID, db.Adopt("SELECT gid FROM spam WHERE user_id = ?"), 100))
				s.Equal("gr1", spamGID)

				// full row survived the rebuild/backfill, not just hash and gid
				var m MsgMeta
				s.Require().NoError(db.Get(&m,
					db.Adopt("SELECT time, chat_id, user_id, user_name, msg_id FROM messages WHERE hash = ? AND gid = ?"),
					"hash1", "gr1"))
				s.Equal(int64(1), m.ChatID)
				s.Equal(int64(100), m.UserID)
				s.Equal("user1", m.UserName)
				s.Equal(1, m.MsgID)

				// primary keys upgraded to the exact composite columns in key order
				s.Equal([]string{"gid", "hash"}, pkColNames("messages"), "messages pk should be (gid, hash)")
				s.Equal([]string{"gid", "user_id"}, pkColNames("spam"), "spam pk should be (gid, user_id)")

				// the migrated tables accept real upserts: a repeated insert must resolve through
				// ON CONFLICT (gid, hash)/(gid, user_id) rather than error or duplicate
				loc, err := NewLocator(ctx, 10*time.Minute, 1, db)
				s.Require().NoError(err)
				s.Require().NoError(loc.AddMessage(ctx, "post-migration text", 7, 500, "u500", 9))
				s.Require().NoError(loc.AddMessage(ctx, "post-migration text", 7, 500, "u500", 11)) // upsert
				got, found := loc.Message(ctx, "post-migration text")
				s.Require().True(found)
				s.Equal(11, got.MsgID, "upsert should update the existing row in place")
				s.Require().NoError(loc.AddSpam(ctx, 500, []spamcheck.Response{{Name: "a", Spam: true}}))
				s.Require().NoError(loc.AddSpam(ctx, 500, []spamcheck.Response{{Name: "b", Spam: false}})) // upsert
				sd, found := loc.Spam(ctx, 500)
				s.Require().True(found)
				s.False(sd.Checks[0].Spam, "spam upsert should replace the existing row")
			})

			s.Run("migrate from partial schema", func() {
				defer db.Exec("DROP TABLE IF EXISTS messages")
				defer db.Exec("DROP TABLE IF EXISTS spam")

				// messages already has gid (nullable, as the real migration adds it), spam does not -
				// each table must migrate independently
				_, err := db.Exec(`
					CREATE TABLE messages (
						hash TEXT PRIMARY KEY,
						gid TEXT DEFAULT '',
						time TIMESTAMP,
						chat_id INTEGER,
						user_id INTEGER,
						user_name TEXT,
						msg_id INTEGER
					);
					CREATE TABLE spam (
						user_id INTEGER PRIMARY KEY,
						time TIMESTAMP,
						checks TEXT
					)
				`)
				s.Require().NoError(err)

				checksJSON := `[{"Name":"test","Spam":true,"Details":"test"}]`
				_, err = db.Exec(db.Adopt(`INSERT INTO spam (user_id, time, checks) VALUES (100, ?, ?)`), time.Now(), checksJSON)
				s.Require().NoError(err)

				_, err = NewLocator(ctx, 10*time.Minute, 1, db)
				s.Require().NoError(err)

				// spam got the gid column backfilled and both tables got composite keys
				var spamGID string
				s.Require().NoError(db.Get(&spamGID, db.Adopt("SELECT gid FROM spam WHERE user_id = ?"), 100))
				s.Equal("gr1", spamGID)

				s.Equal(2, pkColCount("messages"))
				s.Equal(2, pkColCount("spam"))
			})

			s.Run("migration idempotency", func() {
				defer db.Exec("DROP TABLE IF EXISTS messages")
				defer db.Exec("DROP TABLE IF EXISTS spam")

				// start from the current schema (already composite + gid), then run migration twice
				createSchema, err := locatorQueries.Pick(db.Type(), CmdCreateLocatorTables)
				s.Require().NoError(err)
				_, err = db.Exec(createSchema)
				s.Require().NoError(err)

				_, err = NewLocator(ctx, 10*time.Minute, 1, db)
				s.Require().NoError(err)
				_, err = NewLocator(ctx, 10*time.Minute, 1, db)
				s.Require().NoError(err)

				// still exactly one gid column and a composite primary key
				s.Equal(1, gidColCount("messages"), "should have exactly one gid column")
				s.Equal(2, pkColCount("messages"))
			})
		})
	}
}

func (s *StorageTestSuite) TestLocator_MigratePrimaryKeyPostgres() {
	if testing.Short() {
		s.T().Skip("skipping postgres test in short mode")
	}
	if s.pgContainer == nil {
		s.T().Skip("postgres is not available")
	}

	ctx := context.Background()
	db, err := engine.NewPostgres(ctx, s.pgContainer.ConnectionString(), "gr1")
	s.Require().NoError(err)
	defer db.Close()

	db.Exec("DROP TABLE IF EXISTS messages")
	db.Exec("DROP TABLE IF EXISTS spam")
	defer db.Exec("DROP TABLE IF EXISTS messages")
	defer db.Exec("DROP TABLE IF EXISTS spam")

	// legacy schema: gid column present but single-column primary keys. gid is nullable to match
	// how the real migration adds it (ADD COLUMN ... DEFAULT ''), so the constraint swap must rely
	// on the implicit SET NOT NULL that ADD PRIMARY KEY performs on the composite columns
	_, err = db.Exec(`
		CREATE TABLE messages (
			hash TEXT PRIMARY KEY,
			gid TEXT DEFAULT '',
			time TIMESTAMP,
			chat_id BIGINT,
			user_id BIGINT,
			user_name TEXT,
			msg_id INTEGER
		);
		CREATE TABLE spam (
			user_id BIGINT PRIMARY KEY,
			gid TEXT DEFAULT '',
			time TIMESTAMP,
			checks TEXT
		)
	`)
	s.Require().NoError(err)

	_, err = db.Exec(`INSERT INTO messages (hash, gid, time, chat_id, user_id, user_name, msg_id)
		VALUES ('hash1', 'gr1', now(), 1, 100, 'user1', 1)`)
	s.Require().NoError(err)

	// run migration through NewLocator
	_, err = NewLocator(ctx, 10*time.Minute, 1, db)
	s.Require().NoError(err)

	pkColsQuery := `SELECT COUNT(*) FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
		WHERE tc.table_name = $1 AND tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = current_schema()`

	var msgPKCols, spamPKCols int
	s.Require().NoError(db.Get(&msgPKCols, pkColsQuery, "messages"))
	s.Equal(2, msgPKCols, "messages should have composite (gid, hash) primary key")
	s.Require().NoError(db.Get(&spamPKCols, pkColsQuery, "spam"))
	s.Equal(2, spamPKCols, "spam should have composite (gid, user_id) primary key")

	// verify data survived the constraint swap
	var hash string
	s.Require().NoError(db.Get(&hash, "SELECT hash FROM messages WHERE gid = 'gr1'"))
	s.Equal("hash1", hash)

	// migration is idempotent
	_, err = NewLocator(ctx, 10*time.Minute, 1, db)
	s.Require().NoError(err)
}

func (s *StorageTestSuite) TestLocator_FreesMisplacedSpamIndex() {
	if testing.Short() {
		s.T().Skip("skipping postgres test in short mode")
	}
	if s.pgContainer == nil {
		s.T().Skip("postgres is not available")
	}

	ctx := context.Background()
	db, err := engine.NewPostgres(ctx, s.pgContainer.ConnectionString(), "gr1")
	s.Require().NoError(err)
	defer db.Close()

	db.Exec("DROP TABLE IF EXISTS messages")
	db.Exec("DROP TABLE IF EXISTS spam")
	db.Exec("DROP TABLE IF EXISTS legacy_squatter")
	defer db.Exec("DROP TABLE IF EXISTS messages")
	defer db.Exec("DROP TABLE IF EXISTS spam")
	defer db.Exec("DROP TABLE IF EXISTS legacy_squatter")

	// an older schema squatted the schema-global idx_spam_gid_time name on another table
	_, err = db.Exec(`CREATE TABLE legacy_squatter (gid TEXT, ts TIMESTAMP);
		CREATE INDEX idx_spam_gid_time ON legacy_squatter(gid, ts DESC)`)
	s.Require().NoError(err)

	// locator init must free the name and create its own index on the spam table in the same pass
	_, err = NewLocator(ctx, 10*time.Minute, 1, db)
	s.Require().NoError(err)

	var table string
	s.Require().NoError(db.Get(&table,
		`SELECT tablename FROM pg_indexes WHERE indexname = 'idx_spam_gid_time' AND schemaname = current_schema()`))
	s.Equal("spam", table, "idx_spam_gid_time should now belong to the locator's spam table")
}

func (s *StorageTestSuite) TestLocator_SharedDBGIDIsolation() {
	ctx := context.Background()

	testIsolation := func(db1, db2 *engine.SQL) {
		locator1, err := NewLocator(ctx, time.Hour, 5, db1)
		s.Require().NoError(err)
		locator2, err := NewLocator(ctx, time.Hour, 5, db2)
		s.Require().NoError(err)

		// same text lands in both groups, the upsert must not clobber the other group's row
		msg := "identical message posted in two groups"
		s.Require().NoError(locator1.AddMessage(ctx, msg, 100, 1, "user1", 1))
		s.Require().NoError(locator2.AddMessage(ctx, msg, 200, 2, "user2", 2))

		meta1, found := locator1.Message(ctx, msg)
		s.Require().True(found, "group1 message should survive group2 insert of the same text")
		s.Equal(int64(100), meta1.ChatID)
		s.Equal("user1", meta1.UserName)

		meta2, found := locator2.Message(ctx, msg)
		s.Require().True(found)
		s.Equal(int64(200), meta2.ChatID)
		s.Equal("user2", meta2.UserName)

		// same user flagged in both groups, spam data must stay per-group
		s.Require().NoError(locator1.AddSpam(ctx, 42, []spamcheck.Response{{Name: "check1", Spam: true}}))
		s.Require().NoError(locator2.AddSpam(ctx, 42, []spamcheck.Response{{Name: "check2", Spam: false}}))

		spam1, found := locator1.Spam(ctx, 42)
		s.Require().True(found, "group1 spam data should survive group2 insert for the same user")
		s.True(spam1.Checks[0].Spam)

		spam2, found := locator2.Spam(ctx, 42)
		s.Require().True(found)
		s.False(spam2.Checks[0].Spam)
	}

	s.Run("sqlite shared file", func() {
		file := filepath.Join(s.T().TempDir(), "shared.db")
		db1, err := engine.NewSqlite(file, "gr1")
		s.Require().NoError(err)
		defer db1.Close()
		db2, err := engine.NewSqlite(file, "gr2")
		s.Require().NoError(err)
		defer db2.Close()
		testIsolation(db1, db2)
	})

	s.Run("postgres shared database", func() {
		if testing.Short() {
			s.T().Skip("skipping postgres test in short mode")
		}
		if s.pgContainer == nil {
			s.T().Skip("postgres is not available")
		}
		connStr := s.pgContainer.ConnectionString()
		db1, err := engine.NewPostgres(ctx, connStr, "loc-iso-gr1")
		s.Require().NoError(err)
		defer db1.Close()
		db2, err := engine.NewPostgres(ctx, connStr, "loc-iso-gr2")
		s.Require().NoError(err)
		defer db2.Close()
		defer db1.Exec("DELETE FROM messages WHERE gid IN ('loc-iso-gr1', 'loc-iso-gr2')")
		defer db1.Exec("DELETE FROM spam WHERE gid IN ('loc-iso-gr1', 'loc-iso-gr2')")
		testIsolation(db1, db2)
	})
}

func (s *StorageTestSuite) TestLocator_GIDIsolation() {
	db1, err := engine.NewSqlite(":memory:", "gr1")
	s.Require().NoError(err)
	defer db1.Close()

	db2, err := engine.NewSqlite(":memory:", "gr2")
	s.Require().NoError(err)
	defer db2.Close()

	ctx := context.Background()

	locator1, err := NewLocator(ctx, time.Hour, 5, db1)
	s.Require().NoError(err)

	locator2, err := NewLocator(ctx, time.Hour, 5, db2)
	s.Require().NoError(err)

	// add same message to both locators
	msg := "test message"
	err = locator1.AddMessage(ctx, msg, 100, 1, "user1", 1)
	s.Require().NoError(err)
	err = locator2.AddMessage(ctx, msg, 200, 2, "user2", 2)
	s.Require().NoError(err)

	// verify messages are isolated
	meta1, found := locator1.Message(ctx, msg)
	s.Require().True(found)
	s.Equal(int64(100), meta1.ChatID)
	s.Equal("user1", meta1.UserName)

	meta2, found := locator2.Message(ctx, msg)
	s.Require().True(found)
	s.Equal(int64(200), meta2.ChatID)
	s.Equal("user2", meta2.UserName)

	// verify spam data isolation
	checks1 := []spamcheck.Response{{Name: "test1", Spam: true}}
	checks2 := []spamcheck.Response{{Name: "test2", Spam: false}}

	err = locator1.AddSpam(ctx, 1, checks1)
	s.Require().NoError(err)
	err = locator2.AddSpam(ctx, 1, checks2)
	s.Require().NoError(err)

	// verify spam data is isolated
	spam1, found := locator1.Spam(ctx, 1)
	s.Require().True(found)
	s.True(spam1.Checks[0].Spam)

	spam2, found := locator2.Spam(ctx, 1)
	s.Require().True(found)
	s.False(spam2.Checks[0].Spam)
}
