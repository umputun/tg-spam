package storage

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestNewLocator(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	const ttl = 10 * time.Minute
	const minSize = 1

	locator, err := NewLocator(ctx, ttl, minSize, db)
	require.NoError(t, err)
	assert.NotNil(t, locator)
	locator.Close(ctx)
}

func TestLocator_AddAndRetrieveMessage(t *testing.T) {
	locator := newTestLocator(t)
	ctx := context.Background()

	msg := "test message"
	chatID := int64(123)
	userID := int64(456)
	userName := "user1"
	msgID := 789

	require.NoError(t, locator.AddMessage(ctx, msg, chatID, userID, userName, msgID))

	retrievedMsg, found := locator.Message(ctx, msg)
	require.True(t, found)
	assert.Equal(t, MsgMeta{Time: retrievedMsg.Time, ChatID: chatID, UserID: userID, UserName: userName, MsgID: msgID}, retrievedMsg)

	res := locator.UserNameByID(ctx, userID)
	assert.Equal(t, userName, res)

	res = locator.UserNameByID(ctx, 123456)
	assert.Equal(t, "", res)

	id := locator.UserIDByName(ctx, userName)
	assert.Equal(t, userID, id)

	id = locator.UserIDByName(ctx, "user2")
	assert.Equal(t, int64(0), id)
}

func TestLocator_AddAndRetrieveManyMessage(t *testing.T) {
	locator := newTestLocator(t)
	ctx := context.Background()
	// add 100 messages for 10 users
	for i := 0; i < 100; i++ {
		userID := int64(i%10 + 1)
		locator.AddMessage(ctx, fmt.Sprintf("test message %d", i), 1234, userID, "name"+strconv.Itoa(int(userID)), i)
	}

	for i := 0; i < 100; i++ {
		retrievedMsg, found := locator.Message(ctx, fmt.Sprintf("test message %d", i))
		require.True(t, found)
		assert.Equal(t, MsgMeta{Time: retrievedMsg.Time, ChatID: int64(1234), UserID: int64(i%10 + 1), UserName: "name" + strconv.Itoa(i%10+1), MsgID: i}, retrievedMsg)
	}
}

func TestLocator_AddAndRetrieveSpam(t *testing.T) {
	locator := newTestLocator(t)
	ctx := context.Background()
	userID := int64(456)
	checks := []spamcheck.Response{{Name: "test", Spam: true, Details: "test spam"}}

	require.NoError(t, locator.AddSpam(ctx, userID, checks))

	retrievedSpam, found := locator.Spam(ctx, userID)
	require.True(t, found)
	assert.Equal(t, checks, retrievedSpam.Checks)
}

func TestLocator_CleanupLogic(t *testing.T) {
	ttl := 10 * time.Minute
	locator := newTestLocator(t)
	ctx := context.Background()

	oldTime := time.Now().Add(-2 * ttl) // ensure this is older than the ttl

	// add two of each to both groups, we have minSize = 1, so we should have 2 to allow cleanup
	_, err := locator.db.Exec(`INSERT INTO messages (hash, gid, time, chat_id, user_id, user_name, msg_id) 
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"old_hash1", locator.db.GID(), oldTime, int64(111), int64(222), "old_user", 333)
	require.NoError(t, err)
	_, err = locator.db.Exec(`INSERT INTO messages (hash, gid, time, chat_id, user_id, user_name, msg_id) 
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"old_hash2", locator.db.GID(), oldTime, int64(111), int64(222), "old_user", 333)
	require.NoError(t, err)

	_, err = locator.db.Exec(`INSERT INTO spam (user_id, gid, time, checks) VALUES (?, ?, ?, ?)`,
		int64(222), locator.db.GID(), oldTime, `[{"Name":"old_test","Spam":true,"Details":"old spam"}]`)
	require.NoError(t, err)
	_, err = locator.db.Exec(`INSERT INTO spam (user_id, gid, time, checks) VALUES (?, ?, ?, ?)`,
		int64(223), locator.db.GID(), oldTime, `[{"Name":"old_test","Spam":true,"Details":"old spam"}]`)
	require.NoError(t, err)

	require.NoError(t, locator.cleanupMessages(ctx))
	require.NoError(t, locator.cleanupSpam())

	var msgCountAfter, spamCountAfter int
	err = locator.db.Get(&msgCountAfter, `SELECT COUNT(*) FROM messages WHERE gid = ?`, locator.db.GID())
	require.NoError(t, err)
	err = locator.db.Get(&spamCountAfter, `SELECT COUNT(*) FROM spam WHERE gid = ?`, locator.db.GID())
	require.NoError(t, err)

	assert.Equal(t, 0, msgCountAfter, "messages should be cleaned up")
	assert.Equal(t, 0, spamCountAfter, "spam should be cleaned up")

	// verify cleanup respects gid
	db2, err := engine.NewSqlite(":memory:", "gr2")
	require.NoError(t, err)
	locator2, err := NewLocator(ctx, ttl, 1, db2)
	require.NoError(t, err)
	defer db2.Close()

	_, err = locator2.db.Exec(`INSERT INTO messages (hash, gid, time, chat_id, user_id, user_name, msg_id) 
        VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"gr2_hash", locator2.db.GID(), oldTime, int64(111), int64(222), "gr2_user", 333)
	require.NoError(t, err)

	var gr2Count int
	err = locator2.db.Get(&gr2Count, `SELECT COUNT(*) FROM messages WHERE gid = ?`, locator2.db.GID())
	require.NoError(t, err)
	assert.Equal(t, 1, gr2Count, "messages in gr2 should remain")
}

func TestLocator_RetrieveNonExistentMessage(t *testing.T) {
	locator := newTestLocator(t)
	ctx := context.Background()
	msg := "non_existent_message"
	_, found := locator.Message(ctx, msg)
	assert.False(t, found, "expected to not find a non-existent message")

	_, found = locator.Spam(ctx, 1234)
	assert.False(t, found, "expected to not find a non-existent spam")
}

func TestLocator_SpamUnmarshalFailure(t *testing.T) {
	locator := newTestLocator(t)
	ctx := context.Background()
	// Insert invalid JSON data directly into the database
	userID := int64(456)
	invalidJSON := "invalid json"
	_, err := locator.db.Exec(`INSERT INTO spam (user_id, time, checks) VALUES (?, ?, ?)`, userID, time.Now(), invalidJSON)
	require.NoError(t, err)

	// Attempt to retrieve the spam data, which should fail during unmarshalling
	_, found := locator.Spam(ctx, userID)
	assert.False(t, found, "expected to not find valid data due to unmarshalling failure")
}

func TestLocator_Migration(t *testing.T) {
	t.Run("migrate from old schema", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()

		// setup old schema without gid
		_, err = db.Exec(`
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
		require.NoError(t, err)

		// insert test data in old format
		_, err = db.Exec(`INSERT INTO messages (hash, time, chat_id, user_id, user_name, msg_id)
            VALUES ('hash1', ?, 1, 100, 'user1', 1)`, time.Now())
		require.NoError(t, err)

		checksJSON := `[{"Name":"test","Spam":true,"Details":"test"}]`
		_, err = db.Exec(`INSERT INTO spam (user_id, time, checks)
            VALUES (100, ?, ?)`, time.Now(), checksJSON)
		require.NoError(t, err)

		// run migration through NewLocator
		locator, err := NewLocator(context.Background(), 10*time.Minute, 1, db)
		require.NoError(t, err)
		defer locator.Close(context.Background())

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
		require.NoError(t, err)
		err = db.Select(&spamCols, "PRAGMA table_info(spam)")
		require.NoError(t, err)

		msgColMap := make(map[string]string)
		for _, col := range msgCols {
			msgColMap[col.Name] = col.Type
		}
		assert.Equal(t, "TEXT", msgColMap["gid"])

		spamColMap := make(map[string]string)
		for _, col := range spamCols {
			spamColMap[col.Name] = col.Type
		}
		assert.Equal(t, "TEXT", spamColMap["gid"])

		// verify data migrated correctly
		var msgGID, spamGID string
		err = db.Get(&msgGID, "SELECT gid FROM messages WHERE hash = 'hash1'")
		require.NoError(t, err)
		assert.Equal(t, "gr1", msgGID)

		err = db.Get(&spamGID, "SELECT gid FROM spam WHERE user_id = 100")
		require.NoError(t, err)
		assert.Equal(t, "gr1", spamGID)
	})

	t.Run("migration idempotency", func(t *testing.T) {
		// create a new db connection for this test
		db, err := engine.NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()

		// create schema with new tables including gid
		_, err = db.Exec(locatorSchema)
		require.NoError(t, err)

		// create first locator which should trigger migration
		locator, err := NewLocator(context.Background(), 10*time.Minute, 1, db)
		require.NoError(t, err)
		defer locator.Close(context.Background())

		// create second locator which should trigger migration again
		locator2, err := NewLocator(context.Background(), 10*time.Minute, 1, db)
		require.NoError(t, err)
		defer locator2.Close(context.Background())

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
		require.NoError(t, err)

		gidColCount := 0
		for _, col := range msgCols {
			if col.Name == "gid" {
				gidColCount++
			}
		}
		assert.Equal(t, 1, gidColCount, "should have exactly one gid column")
	})
}

func TestLocator_HashCollisions(t *testing.T) {
	db, teardown := setupTestDB(t)
	defer teardown()

	ctx := context.Background()
	_, err := NewLocator(ctx, time.Hour, 1000, db)
	require.NoError(t, err)

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
	_, err = db.Exec(`INSERT INTO messages (hash, gid, time, chat_id, user_id, user_name, msg_id) 
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		hash, db.GID(), msg1.Time, msg1.ChatID, msg1.UserID, msg1.UserName, msg1.MsgID)
	require.NoError(t, err)

	// try to insert second message with same hash
	_, err = db.Exec(`INSERT INTO messages (hash, gid, time, chat_id, user_id, user_name, msg_id) 
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		hash, db.GID(), msg2.Time, msg2.ChatID, msg2.UserID, msg2.UserName, msg2.MsgID)
	// should fail due to hash being primary key
	assert.Error(t, err)

	// verify only first message exists
	var count int
	err = db.Get(&count, "SELECT COUNT(*) FROM messages WHERE hash = ? AND gid = ?", hash, db.GID())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestLocator_GIDIsolation(t *testing.T) {
	db1, err := engine.NewSqlite(":memory:", "gr1")
	require.NoError(t, err)
	defer db1.Close()

	db2, err := engine.NewSqlite(":memory:", "gr2")
	require.NoError(t, err)
	defer db2.Close()

	ctx := context.Background()

	locator1, err := NewLocator(ctx, time.Hour, 5, db1)
	require.NoError(t, err)

	locator2, err := NewLocator(ctx, time.Hour, 5, db2)
	require.NoError(t, err)

	// add same message to both locators
	msg := "test message"
	err = locator1.AddMessage(ctx, msg, 100, 1, "user1", 1)
	require.NoError(t, err)
	err = locator2.AddMessage(ctx, msg, 200, 2, "user2", 2)
	require.NoError(t, err)

	// verify messages are isolated
	meta1, found := locator1.Message(ctx, msg)
	require.True(t, found)
	assert.Equal(t, int64(100), meta1.ChatID)
	assert.Equal(t, "user1", meta1.UserName)

	meta2, found := locator2.Message(ctx, msg)
	require.True(t, found)
	assert.Equal(t, int64(200), meta2.ChatID)
	assert.Equal(t, "user2", meta2.UserName)

	// verify spam data isolation
	checks1 := []spamcheck.Response{{Name: "test1", Spam: true}}
	checks2 := []spamcheck.Response{{Name: "test2", Spam: false}}

	err = locator1.AddSpam(ctx, 1, checks1)
	require.NoError(t, err)
	err = locator2.AddSpam(ctx, 1, checks2)
	require.NoError(t, err)

	// verify spam data is isolated
	spam1, found := locator1.Spam(ctx, 1)
	require.True(t, found)
	assert.True(t, spam1.Checks[0].Spam)

	spam2, found := locator2.Spam(ctx, 1)
	require.True(t, found)
	assert.False(t, spam2.Checks[0].Spam)
}

func newTestLocator(t *testing.T) *Locator {
	db, err := engine.NewSqlite(":memory:", "gr1")
	require.NoError(t, err)
	ctx := context.Background()
	const ttl = 10 * time.Minute
	const minSize = 1

	locator, err := NewLocator(ctx, ttl, minSize, db)
	require.NoError(t, err)

	t.Cleanup(func() { locator.Close(ctx) })

	return locator
}
