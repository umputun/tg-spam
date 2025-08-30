package tgspam

import (
	"fmt"
	"testing"
	"time"

	cache "github.com/go-pkgz/expirable-cache/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestDuplicateDetector_Check(t *testing.T) {
	tests := []struct {
		name      string
		threshold int
		window    time.Duration
		messages  []spamcheck.Request
		expected  []bool // expected spam results for each message
	}{
		{
			name:      "disabled detector",
			threshold: 0,
			window:    time.Hour,
			messages: []spamcheck.Request{
				{Msg: "test", UserID: "123"},
				{Msg: "test", UserID: "123"},
			},
			expected: []bool{false, false},
		},
		{
			name:      "single message not spam",
			threshold: 2,
			window:    time.Hour,
			messages: []spamcheck.Request{
				{Msg: "hello", UserID: "123"},
			},
			expected: []bool{false},
		},
		{
			name:      "duplicate messages trigger spam",
			threshold: 3,
			window:    time.Hour,
			messages: []spamcheck.Request{
				{Msg: "spam", UserID: "123"},
				{Msg: "spam", UserID: "123"},
				{Msg: "spam", UserID: "123"},
				{Msg: "spam", UserID: "123"},
			},
			expected: []bool{false, false, true, true},
		},
		{
			name:      "different users don't trigger",
			threshold: 2,
			window:    time.Hour,
			messages: []spamcheck.Request{
				{Msg: "same", UserID: "123"},
				{Msg: "same", UserID: "456"},
				{Msg: "same", UserID: "123"},
			},
			expected: []bool{false, false, true},
		},
		{
			name:      "different messages don't trigger",
			threshold: 2,
			window:    time.Hour,
			messages: []spamcheck.Request{
				{Msg: "first", UserID: "123"},
				{Msg: "second", UserID: "123"},
				{Msg: "third", UserID: "123"},
			},
			expected: []bool{false, false, false},
		},
		{
			name:      "invalid user id",
			threshold: 2,
			window:    time.Hour,
			messages: []spamcheck.Request{
				{Msg: "test", UserID: "invalid"},
			},
			expected: []bool{false},
		},
		{
			name:      "empty user id",
			threshold: 2,
			window:    time.Hour,
			messages: []spamcheck.Request{
				{Msg: "test", UserID: ""},
			},
			expected: []bool{false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDuplicateDetector(tt.threshold, tt.window)

			for i, msg := range tt.messages {
				resp := d.check(msg)
				assert.Equal(t, tt.expected[i], resp.Spam,
					"message %d: %q from user %s", i, msg.Msg, msg.UserID)

				if resp.Spam {
					assert.Contains(t, resp.Details, "repeated")
				}
			}
		})
	}
}

func TestDuplicateDetector_TimeWindow(t *testing.T) {
	d := newDuplicateDetector(2, 100*time.Millisecond)

	// send first message
	resp := d.check(spamcheck.Request{Msg: "test", UserID: "123"})
	assert.False(t, resp.Spam)

	// send second message - should trigger
	resp = d.check(spamcheck.Request{Msg: "test", UserID: "123"})
	assert.True(t, resp.Spam)

	// wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// send third message - should not trigger as previous ones expired
	resp = d.check(spamcheck.Request{Msg: "test", UserID: "123"})
	assert.False(t, resp.Spam)
}

func TestDuplicateDetector_AutomaticCleanup(t *testing.T) {
	d := newDuplicateDetector(3, 100*time.Millisecond)
	d.cleanupInterval = 50 * time.Millisecond // set short cleanup interval for test

	// add messages from multiple users
	for i := 0; i < 5; i++ {
		d.check(spamcheck.Request{Msg: fmt.Sprintf("msg%d", i), UserID: fmt.Sprintf("%d", i)})
	}

	// verify history exists
	assert.Equal(t, 5, len(d.cache.Keys()))

	// wait for messages to expire
	time.Sleep(150 * time.Millisecond)

	// trigger cleanup by sending new message after cleanup interval
	d.check(spamcheck.Request{Msg: "trigger", UserID: "999"})

	// verify old entries are cleaned, only the new one remains
	keys := d.cache.Keys()
	assert.Equal(t, 1, len(keys))
	assert.Equal(t, int64(999), keys[0])
}

func TestDuplicateDetector_CleanupInterval(t *testing.T) {
	d := newDuplicateDetector(3, 100*time.Millisecond)
	d.cleanupInterval = time.Hour // long cleanup interval

	// add messages from multiple users
	for i := 0; i < 5; i++ {
		d.check(spamcheck.Request{Msg: fmt.Sprintf("msg%d", i), UserID: fmt.Sprintf("%d", i)})
	}

	// wait for messages to expire
	time.Sleep(150 * time.Millisecond)

	// send new message - cleanup should NOT trigger (interval not reached)
	d.check(spamcheck.Request{Msg: "trigger", UserID: "999"})

	// verify expired entries are still there (cleanup didn't run)
	assert.Equal(t, 6, len(d.cache.Keys()), "should have all 6 users, cleanup didn't run")
}

func TestDuplicateDetector_NilDetector(t *testing.T) {
	var d *duplicateDetector

	// nil detector should not panic
	resp := d.check(spamcheck.Request{Msg: "test", UserID: "123"})
	assert.False(t, resp.Spam)
	assert.Equal(t, "check disabled", resp.Details)
}

func TestDuplicateDetector_ConcurrentAccess(t *testing.T) {
	d := newDuplicateDetector(5, time.Hour)

	// run concurrent checks
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(userID int) {
			for j := 0; j < 10; j++ {
				d.check(spamcheck.Request{
					Msg:    fmt.Sprintf("msg%d", j%3),
					UserID: fmt.Sprintf("%d", userID),
				})
			}
			done <- true
		}(i)
	}

	// wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// verify detector state is consistent
	keys := d.cache.Keys()
	assert.LessOrEqual(t, len(keys), 10)

	// each user history should be valid
	for _, userID := range keys {
		history, found := d.cache.Get(userID)
		require.True(t, found)
		require.NotNil(t, history)
		assert.LessOrEqual(t, len(history.entries), 10)
		assert.LessOrEqual(t, len(history.hashCounts), 3) // only 3 different messages
	}
}

func TestDuplicateDetector_LRUEviction(t *testing.T) {
	d := newDuplicateDetector(2, time.Hour)
	d.maxUsers = 3 // set small limit for testing
	d.cache = cache.NewCache[int64, userHistory]().WithMaxKeys(3).WithTTL(time.Hour)

	// add messages from 5 users (exceeds limit of 3)
	for i := 0; i < 5; i++ {
		d.check(spamcheck.Request{Msg: "test", UserID: fmt.Sprintf("%d", i)})
	}

	// cache should only have 3 users due to LRU eviction
	keys := d.cache.Keys()
	assert.LessOrEqual(t, len(keys), 3, "cache should respect max users limit")

	// recent users should be in cache (2, 3, 4)
	_, found := d.cache.Get(4)
	assert.True(t, found, "most recent user should be in cache")
}
