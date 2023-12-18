package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib"
)

func TestNewLocator(t *testing.T) {
	ttl := 10 * time.Minute
	locator := NewLocator(ttl, 10)
	require.NotNil(t, locator)

	assert.Equal(t, ttl, locator.ttl)
	assert.NotZero(t, locator.cleanupDuration)
	assert.NotNil(t, locator.msgs.data)
	assert.NotNil(t, locator.spam.data)
	assert.WithinDuration(t, time.Now(), locator.msgs.lastRemoval, time.Second)
	assert.WithinDuration(t, time.Now(), locator.spam.lastRemoval, time.Second)
}

func TestGetMessage(t *testing.T) {
	locator := NewLocator(10*time.Minute, 10)

	// adding a message
	msg := "test message"
	chatID := int64(123)
	userID := int64(456)
	msgID := 7890
	locator.AddMessage(msg, chatID, userID, "user", msgID)

	// test retrieval of existing message
	info, found := locator.Message("test message")
	require.True(t, found)
	assert.Equal(t, msgID, info.msgID)
	assert.Equal(t, chatID, info.chatID)
	assert.Equal(t, userID, info.userID)
	assert.Equal(t, "user", info.userName)

	// test retrieval of non-existing message
	_, found = locator.Message("no such message") // non-existing msgID
	assert.False(t, found)
}

func TestGetSpam(t *testing.T) {
	locator := NewLocator(10*time.Minute, 10)

	// adding a message
	userID := int64(456)
	checkResults := []lib.CheckResult{
		{Name: "test", Spam: true, Details: "test spam"},
		{Name: "test2", Spam: false, Details: "test not spam"},
	}
	locator.AddSpam(userID, checkResults)

	// test retrieval of existing message
	res, found := locator.Spam(userID)
	require.True(t, found)
	assert.Equal(t, checkResults, res.checks)

	// test retrieval of non-existing message
	_, found = locator.Spam(int64(1)) // non-existing msgID
	assert.False(t, found)
}

func TestMsgHash(t *testing.T) {
	locator := NewLocator(10*time.Minute, 10)

	t.Run("hash for empty message", func(t *testing.T) {
		hash := locator.MsgHash("")
		assert.Equal(t, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", hash)
	})

	t.Run("hash for non-empty message", func(t *testing.T) {
		hash := locator.MsgHash("test message")
		assert.Equal(t, "3f0a377ba0a4a460ecb616f6507ce0d8cfa3e704025d4fda3ed0c5ca05468728", hash)
	})

	t.Run("hash for different non-empty message", func(t *testing.T) {
		hash := locator.MsgHash("test message blah")
		assert.Equal(t, "21b7035e5ab5664eb7571b1f63d96951d5554a5465302b9cdd2e3de510eda6d8", hash)
	})
}

func TestAddMessageAndCleanup(t *testing.T) {
	ttl := 2 * time.Second
	cleanupDuration := 1 * time.Second
	locator := NewLocator(ttl, 0)
	locator.cleanupDuration = cleanupDuration

	// Adding a message
	msg := "test message"
	chatID := int64(123)
	userID := int64(456)
	msgID := 7890
	locator.AddMessage(msg, chatID, userID, "user1", msgID)

	hash := locator.MsgHash(msg)
	meta, exists := locator.msgs.data[hash]
	require.True(t, exists)
	assert.Equal(t, chatID, meta.chatID)
	assert.Equal(t, userID, meta.userID)
	assert.Equal(t, "user1", meta.userName)
	assert.Equal(t, msgID, meta.msgID)

	// wait for cleanup duration and add another message to trigger cleanup
	time.Sleep(cleanupDuration + time.Second)
	locator.AddMessage("another message", 789, 555, "user2", 1011)

	_, existsAfterCleanup := locator.msgs.data[hash]
	assert.False(t, existsAfterCleanup)
}

func TestAddAndCleanup_withMinSize(t *testing.T) {
	ttl := 2 * time.Second
	cleanupDuration := 1 * time.Second
	locator := NewLocator(ttl, 2)
	locator.cleanupDuration = cleanupDuration

	// Adding first message
	msg := "test message"
	chatID := int64(123)
	userID := int64(456)
	msgID := 7890
	locator.AddMessage(msg, chatID, userID, "user1", msgID) // add first message

	hash := locator.MsgHash(msg)
	meta, exists := locator.msgs.data[hash]
	require.True(t, exists)
	assert.Equal(t, chatID, meta.chatID)
	assert.Equal(t, userID, meta.userID)
	assert.Equal(t, msgID, meta.msgID)

	// wait for cleanup duration and add another message to trigger cleanup
	time.Sleep(cleanupDuration + time.Millisecond*200)
	locator.AddMessage("second message", 789, 555, "user2", 1011)
	_, existsAfterCleanup := locator.msgs.data[hash]
	assert.True(t, existsAfterCleanup, "minSize should prevent cleanup")

	// wait for cleanup duration and add another message to trigger cleanup
	time.Sleep(cleanupDuration + +time.Millisecond*200)
	locator.AddMessage("third message", chatID, 9000, "user3", 2000)
	_, existsAfterCleanup = locator.msgs.data[hash]
	assert.False(t, existsAfterCleanup, "minSize should allow cleanup")

	assert.Len(t, locator.msgs.data, 2, "should keep minSize messages")
}

func TestAddSpamAndCleanup(t *testing.T) {
	ttl := 2 * time.Second
	cleanupDuration := 1 * time.Second
	locator := NewLocator(ttl, 0)
	locator.cleanupDuration = cleanupDuration

	// Adding a spam
	msgID := int64(123)
	checkResults := []lib.CheckResult{{Name: "test", Spam: true, Details: "test spam"}}
	locator.AddSpam(msgID, checkResults)

	meta, exists := locator.spam.data[msgID]
	require.True(t, exists)
	assert.Equal(t, []lib.CheckResult{{Name: "test", Spam: true, Details: "test spam"}}, meta.checks)

	// wait for cleanup duration and add another message to trigger cleanup
	time.Sleep(cleanupDuration + time.Second)
	locator.AddSpam(789, []lib.CheckResult{{Name: "test2", Spam: true, Details: "test spam2"}})

	_, existsAfterCleanup := locator.spam.data[msgID]
	assert.False(t, existsAfterCleanup)

	_, existsAfterCleanup = locator.spam.data[int64(789)]
	assert.True(t, existsAfterCleanup)
}
