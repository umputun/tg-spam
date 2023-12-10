package events

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLocator(t *testing.T) {
	ttl := 10 * time.Minute
	locator := NewLocator(ttl)
	require.NotNil(t, locator)

	assert.Equal(t, ttl, locator.ttl)
	assert.NotZero(t, locator.cleanupDuration)
	assert.NotNil(t, locator.data)
	assert.WithinDuration(t, time.Now(), locator.lastRemoval, time.Second)
}

func TestGet(t *testing.T) {
	locator := NewLocator(10 * time.Minute)

	// adding a message
	msg := "test message"
	chatID := int64(123)
	userID := int64(456)
	msgID := 7890
	locator.Add(msg, chatID, userID, msgID)

	// test retrieval of existing message
	info, found := locator.Get("test message")
	require.True(t, found)
	assert.Equal(t, msgID, info.msgID)
	assert.Equal(t, chatID, info.chatID)
	assert.Equal(t, userID, info.userID)

	// test retrieval of non-existing message
	_, found = locator.Get("no such message") // non-existing msgID
	assert.False(t, found)
}

func TestMsgHash(t *testing.T) {
	locator := NewLocator(10 * time.Minute)

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

func TestAddAndCleanup(t *testing.T) {
	ttl := 2 * time.Second
	cleanupDuration := 1 * time.Second
	locator := NewLocator(ttl)
	locator.cleanupDuration = cleanupDuration

	// Adding a message
	msg := "test message"
	chatID := int64(123)
	userID := int64(456)
	msgID := 7890
	locator.Add(msg, chatID, userID, msgID)

	hash := locator.MsgHash(msg)
	meta, exists := locator.data[hash]
	require.True(t, exists)
	assert.Equal(t, chatID, meta.chatID)
	assert.Equal(t, userID, meta.userID)
	assert.Equal(t, msgID, meta.msgID)

	// wait for cleanup duration and add another message to trigger cleanup
	time.Sleep(cleanupDuration + time.Second)
	locator.Add("another message", 789, 555, 1011)

	_, existsAfterCleanup := locator.data[hash]
	assert.False(t, existsAfterCleanup)
}
