package storage

import (
	"context"
	"fmt"
	"strconv"
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
			s.Equal("", res)

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
			for i := 0; i < 100; i++ {
				userID := int64(i%10 + 1)
				s.Require().NoError(locator.AddMessage(ctx, fmt.Sprintf("test message %d", i), 1234, userID, "name"+strconv.Itoa(int(userID)), i))
			}

			for i := 0; i < 100; i++ {
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
			if db.Type() == engine.Postgres {
				s.T().Skip("skipping postgres for now")
			}
			db.Exec("DROP TABLE IF EXISTS messages")
			db.Exec("DROP TABLE IF EXISTS spam")

			s.Run("migrate from old schema", func() {
				defer db.Exec("DROP TABLE messages")
				defer db.Exec("DROP TABLE spam")

				// setup old schema without gid
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
				_, err = db.Exec(`INSERT INTO messages (hash, time, chat_id, user_id, user_name, msg_id)
            		VALUES ('hash1', ?, 1, 100, 'user1', 1)`, time.Now())
				s.Require().NoError(err)

				checksJSON := `[{"Name":"test","Spam":true,"Details":"test"}]`
				_, err = db.Exec(`INSERT INTO spam (user_id, time, checks)
            		VALUES (100, ?, ?)`, time.Now(), checksJSON)
				s.Require().NoError(err)

				// run migration through NewLocator
				_, err = NewLocator(context.Background(), 10*time.Minute, 1, db)
				s.Require().NoError(err)

				// verify structure
				var msgCols, spamCols []struct {
					CID       int     `db:"cid"`
					Name      string  `db:"name"`
					Type      string  `db:"type"`
					NotNull   bool    `db:"notnull"`
					DfltValue *string `db:"dflt_value"`
					PK        bool    `db:"pk"`
				}
				err = db.Select(&msgCols, "PRAGMA table_info(messages)")
				s.Require().NoError(err)
				err = db.Select(&spamCols, "PRAGMA table_info(spam)")
				s.Require().NoError(err)

				msgColMap := make(map[string]string)
				for _, col := range msgCols {
					msgColMap[col.Name] = col.Type
				}
				s.Assert().Equal("TEXT", msgColMap["gid"])

				spamColMap := make(map[string]string)
				for _, col := range spamCols {
					spamColMap[col.Name] = col.Type
				}
				s.Assert().Equal("TEXT", spamColMap["gid"])

				// verify data migrated correctly
				var msgGID, spamGID string
				err = db.Get(&msgGID, "SELECT gid FROM messages WHERE hash = 'hash1'")
				s.Require().NoError(err)
				s.Assert().Equal("gr1", msgGID)

				err = db.Get(&spamGID, "SELECT gid FROM spam WHERE user_id = 100")
				s.Require().NoError(err)
				s.Assert().Equal("gr1", spamGID)
			})

			s.Run("migration idempotency", func() {
				_, err := NewLocator(ctx, time.Hour, 1000, db)
				s.Require().NoError(err)
				defer db.Exec("DROP TABLE messages")
				defer db.Exec("DROP TABLE spam")

				// create schema with new tables including gid
				createSchema, err := locatorQueries.Pick(db.Type(), CmdCreateLocatorTables)
				s.Require().NoError(err)
				_, err = db.Exec(createSchema)
				s.Require().NoError(err)

				// create first locator which should trigger migration
				_, err = NewLocator(context.Background(), 10*time.Minute, 1, db)
				s.Require().NoError(err)

				// create second locator which should trigger migration again
				_, err = NewLocator(context.Background(), 10*time.Minute, 1, db)
				s.Require().NoError(err)

				// verify structure remained the same
				var msgCols []struct {
					CID       int     `db:"cid"`
					Name      string  `db:"name"`
					Type      string  `db:"type"`
					NotNull   bool    `db:"notnull"`
					DfltValue *string `db:"dflt_value"`
					PK        bool    `db:"pk"`
				}
				err = db.Select(&msgCols, "PRAGMA table_info(messages)")
				s.Require().NoError(err)

				gidColCount := 0
				for _, col := range msgCols {
					if col.Name == "gid" {
						gidColCount++
					}
				}
				s.Assert().Equal(1, gidColCount, "should have exactly one gid column")
			})
		})
	}

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
	s.Assert().Equal(int64(100), meta1.ChatID)
	s.Assert().Equal("user1", meta1.UserName)

	meta2, found := locator2.Message(ctx, msg)
	s.Require().True(found)
	s.Assert().Equal(int64(200), meta2.ChatID)
	s.Assert().Equal("user2", meta2.UserName)

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
	s.Assert().True(spam1.Checks[0].Spam)

	spam2, found := locator2.Spam(ctx, 1)
	s.Require().True(found)
	s.Assert().False(spam2.Checks[0].Spam)
}
