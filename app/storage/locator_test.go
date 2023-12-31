package storage

import (
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestNewLocator(t *testing.T) {
	db, err := sqlx.Connect("sqlite", ":memory:")
	require.NoError(t, err)

	const ttl = 10 * time.Minute
	const minSize = 1

	locator, err := NewLocator(ttl, minSize, db)
	require.NoError(t, err)
	assert.NotNil(t, locator)
	locator.Close()
}

func TestLocator_AddAndRetrieveMessage(t *testing.T) {
	locator := newTestLocator(t)

	msg := "test message"
	chatID := int64(123)
	userID := int64(456)
	userName := "user1"
	msgID := 789

	require.NoError(t, locator.AddMessage(msg, chatID, userID, userName, msgID))

	retrievedMsg, found := locator.Message(msg)
	require.True(t, found)
	assert.Equal(t, MsgMeta{Time: retrievedMsg.Time, ChatID: chatID, UserID: userID, UserName: userName, MsgID: msgID}, retrievedMsg)

	res := locator.UserNameByID(userID)
	assert.Equal(t, userName, res)

	res = locator.UserNameByID(123456)
	assert.Equal(t, "", res)

	id := locator.UserIDByName(userName)
	assert.Equal(t, userID, id)

	id = locator.UserIDByName("user2")
	assert.Equal(t, int64(0), id)
}

func TestLocator_AddAndRetrieveManyMessage(t *testing.T) {
	locator := newTestLocator(t)

	// add 100 messages for 10 users
	for i := 0; i < 100; i++ {
		userID := int64(i%10 + 1)
		locator.AddMessage(fmt.Sprintf("test message %d", i), 1234, userID, "name"+strconv.Itoa(int(userID)), i)
	}

	for i := 0; i < 100; i++ {
		retrievedMsg, found := locator.Message(fmt.Sprintf("test message %d", i))
		require.True(t, found)
		assert.Equal(t, MsgMeta{Time: retrievedMsg.Time, ChatID: int64(1234), UserID: int64(i%10 + 1), UserName: "name" + strconv.Itoa(i%10+1), MsgID: i}, retrievedMsg)
	}
}

func TestLocator_AddAndRetrieveSpam(t *testing.T) {
	locator := newTestLocator(t)

	userID := int64(456)
	checks := []spamcheck.Response{{Name: "test", Spam: true, Details: "test spam"}}

	require.NoError(t, locator.AddSpam(userID, checks))

	retrievedSpam, found := locator.Spam(userID)
	require.True(t, found)
	assert.Equal(t, checks, retrievedSpam.Checks)
}

func TestLocator_CleanupLogic(t *testing.T) {
	ttl := 10 * time.Minute
	locator := newTestLocator(t)

	oldTime := time.Now().Add(-2 * ttl) // ensure this is older than the ttl

	// add two of each, we have minSize = 1, so we should have 2 to allow cleanup
	_, err := locator.db.Exec(`INSERT INTO messages (hash, time, chat_id, user_id, user_name, msg_id) VALUES (?, ?, ?, ?, ?, ?)`,
		"old_hash1", oldTime, int64(111), int64(222), "old_user", 333)
	require.NoError(t, err)
	_, err = locator.db.Exec(`INSERT INTO messages (hash, time, chat_id, user_id, user_name, msg_id) VALUES (?, ?, ?, ?, ?, ?)`,
		"old_hash2", oldTime, int64(111), int64(222), "old_user", 333)
	require.NoError(t, err)

	_, err = locator.db.Exec(`INSERT INTO spam (user_id, time, checks) VALUES (?, ?, ?)`,
		int64(222), oldTime, `[{"Name":"old_test","Spam":true,"Details":"old spam"}]`)
	require.NoError(t, err)
	_, err = locator.db.Exec(`INSERT INTO spam (user_id, time, checks) VALUES (?, ?, ?)`,
		int64(223), oldTime, `[{"Name":"old_test","Spam":true,"Details":"old spam"}]`)
	require.NoError(t, err)

	require.NoError(t, locator.cleanupMessages())
	require.NoError(t, locator.cleanupSpam())

	var msgCountAfter, spamCountAfter int
	locator.db.Get(&msgCountAfter, `SELECT COUNT(*) FROM messages`)
	locator.db.Get(&spamCountAfter, `SELECT COUNT(*) FROM spam`)

	assert.Equal(t, 0, msgCountAfter)
	assert.Equal(t, 0, spamCountAfter)
}

func TestLocator_RetrieveNonExistentMessage(t *testing.T) {
	locator := newTestLocator(t)

	msg := "non_existent_message"
	_, found := locator.Message(msg)
	assert.False(t, found, "expected to not find a non-existent message")

	_, found = locator.Spam(1234)
	assert.False(t, found, "expected to not find a non-existent spam")
}

func TestLocator_SpamUnmarshalFailure(t *testing.T) {
	locator := newTestLocator(t)

	// Insert invalid JSON data directly into the database
	userID := int64(456)
	invalidJSON := "invalid json"
	_, err := locator.db.Exec(`INSERT INTO spam (user_id, time, checks) VALUES (?, ?, ?)`, userID, time.Now(), invalidJSON)
	require.NoError(t, err)

	// Attempt to retrieve the spam data, which should fail during unmarshalling
	_, found := locator.Spam(userID)
	assert.False(t, found, "expected to not find valid data due to unmarshalling failure")
}

func newTestLocator(t *testing.T) *Locator {
	db, err := NewSqliteDB(":memory:")
	require.NoError(t, err)

	const ttl = 10 * time.Minute
	const minSize = 1

	locator, err := NewLocator(ttl, minSize, db)
	require.NoError(t, err)

	t.Cleanup(func() {
		locator.Close()
	})

	return locator
}
