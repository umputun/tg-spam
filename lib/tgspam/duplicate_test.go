package tgspam

import (
	"fmt"
	"strconv"
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

				if !resp.Spam && tt.threshold > 0 {
					// when check is enabled and no spam detected
					if msg.UserID == "" {
						assert.Equal(t, "check disabled", resp.Details)
						continue
					}

					if _, err := strconv.ParseInt(msg.UserID, 10, 64); err != nil {
						assert.Equal(t, "invalid user id", resp.Details)
						continue
					}

					assert.Equal(t, "no duplicates found", resp.Details)
				}
			}
		})
	}
}

func TestDuplicateDetector_TimeWindow(t *testing.T) {
	d := newDuplicateDetector(2, 100*time.Millisecond)

	// send first message
	resp := d.check(spamcheck.Request{Msg: "test", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1001}})
	assert.False(t, resp.Spam)

	// send second message - should trigger
	resp = d.check(spamcheck.Request{Msg: "test", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1002}})
	assert.True(t, resp.Spam)

	// wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// send third message - should not trigger as previous ones expired
	resp = d.check(spamcheck.Request{Msg: "test", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1003}})
	assert.False(t, resp.Spam)
}

func TestDuplicateDetector_ExtraDeleteIDs(t *testing.T) {
	d := newDuplicateDetector(3, time.Hour) // long window to avoid expiry

	// send first message
	resp := d.check(spamcheck.Request{Msg: "spam", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1001}})
	assert.False(t, resp.Spam)
	assert.Nil(t, resp.ExtraDeleteIDs)

	// send second message - still not spam
	resp = d.check(spamcheck.Request{Msg: "spam", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1002}})
	assert.False(t, resp.Spam)
	assert.Nil(t, resp.ExtraDeleteIDs)

	// send third message - should trigger spam with extra IDs
	resp = d.check(spamcheck.Request{Msg: "spam", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1003}})
	assert.True(t, resp.Spam)
	assert.Equal(t, []int{1001, 1002}, resp.ExtraDeleteIDs, "should return first two message IDs")

	// send fourth message - should still trigger but IDs were cleared
	resp = d.check(spamcheck.Request{Msg: "spam", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1004}})
	assert.True(t, resp.Spam)
	assert.Nil(t, resp.ExtraDeleteIDs, "IDs should be cleared after first spam detection")
}

func TestDuplicateDetector_ExtraDeleteIDs_DifferentMessages(t *testing.T) {
	d := newDuplicateDetector(2, time.Hour) // threshold of 2

	// send different messages from same user - should not collect IDs
	resp := d.check(spamcheck.Request{Msg: "hello", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1001}})
	assert.False(t, resp.Spam)

	resp = d.check(spamcheck.Request{Msg: "world", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1002}})
	assert.False(t, resp.Spam)

	// now send duplicates
	resp = d.check(spamcheck.Request{Msg: "spam", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1003}})
	assert.False(t, resp.Spam)

	resp = d.check(spamcheck.Request{Msg: "spam", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1004}})
	assert.True(t, resp.Spam)
	assert.Equal(t, []int{1003}, resp.ExtraDeleteIDs, "should only include duplicate message ID")
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
		assert.Equal(t, 10, len(history.entries)) // exactly 10 messages sent per user
		assert.Equal(t, 3, len(history.trackers)) // exactly 3 different messages (msg0, msg1, msg2)
	}
}

func TestDuplicateDetector_DurationBetweenDuplicates(t *testing.T) {
	d := newDuplicateDetector(3, time.Hour) // threshold of 3, large window

	// send first message
	resp := d.check(spamcheck.Request{Msg: "spam", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1001}})
	assert.False(t, resp.Spam)

	// wait a bit
	time.Sleep(100 * time.Millisecond)

	// send second message
	resp = d.check(spamcheck.Request{Msg: "spam", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1002}})
	assert.False(t, resp.Spam)

	// wait more
	time.Sleep(200 * time.Millisecond)

	// send third message - should trigger spam with actual duration
	resp = d.check(spamcheck.Request{Msg: "spam", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1003}})
	assert.True(t, resp.Spam)

	// the duration should be approximately 300ms (between first and third message)
	// check that it's not showing the configured window (1h)
	assert.NotContains(t, resp.Details, "1h")
	assert.Contains(t, resp.Details, "message repeated 3 times in")
	// the duration should be rounded to seconds (0s since less than 1s)
	assert.Contains(t, resp.Details, "message repeated 3 times in 0s")
}

func TestDuplicateDetector_InstantDuplicates(t *testing.T) {
	d := newDuplicateDetector(2, time.Hour) // threshold of 2

	// send two messages instantly
	resp := d.check(spamcheck.Request{Msg: "spam", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1001}})
	assert.False(t, resp.Spam)

	resp = d.check(spamcheck.Request{Msg: "spam", UserID: "123", Meta: spamcheck.MetaData{MessageID: 1002}})
	assert.True(t, resp.Spam)

	// when messages are sent very quickly, should show either "instantly" or 0s (rounded to seconds)
	assert.Contains(t, resp.Details, "message repeated 2 times in")
	// should show either "instantly" or "0s" when rounded to seconds
	assert.Regexp(t, `message repeated 2 times in (instantly|0s)`, resp.Details)
}

func TestDuplicateDetector_MessageIDsGrowthExceedsMax(t *testing.T) {
	d := newDuplicateDetector(1000, time.Hour) // very high threshold
	// maxEntriesPerUser is 200 by default

	// send 300 duplicate messages, staying below threshold but exceeding maxEntriesPerUser
	for i := 0; i < 300; i++ {
		resp := d.check(spamcheck.Request{
			Msg:    "spam message",
			UserID: "123",
			Meta:   spamcheck.MetaData{MessageID: 1000 + i},
		})
		assert.False(t, resp.Spam, "should not trigger spam with threshold 1000")
	}

	// check how many message IDs are stored
	history, found := d.cache.Get(int64(123))
	assert.True(t, found)

	msgHash := d.hash("spam message")
	tracker := history.trackers[msgHash]

	t.Logf("maxEntriesPerUser: %d", d.maxEntriesPerUser)
	t.Logf("Number of entries: %d", len(history.entries))
	t.Logf("Number of message IDs stored: %d", len(tracker.messageIDs))

	// entries should be limited to maxEntriesPerUser
	assert.Equal(t, d.maxEntriesPerUser, len(history.entries))

	// messageIDs should now be capped at 100 (or threshold if lower)
	assert.LessOrEqual(t, len(tracker.messageIDs), 100, "messageIDs should be capped to prevent unbounded growth")
	assert.Greater(t, len(tracker.messageIDs), 0, "should have some message IDs")
}

func TestDuplicateDetector_FirstSeenAfterTrimming(t *testing.T) {
	d := newDuplicateDetector(10, time.Hour) // high threshold to avoid triggering spam
	d.maxEntriesPerUser = 3                  // very small limit to force trimming

	// send 5 messages with delays to have different timestamps
	for i := 0; i < 5; i++ {
		resp := d.check(spamcheck.Request{
			Msg:    "same message",
			UserID: "123",
			Meta:   spamcheck.MetaData{MessageID: 1000 + i},
		})
		assert.False(t, resp.Spam)
		time.Sleep(100 * time.Millisecond)
	}

	// check the history
	history, found := d.cache.Get(int64(123))
	assert.True(t, found)
	assert.Equal(t, 3, len(history.entries), "should only keep 3 most recent entries")

	// check the tracker
	msgHash := d.hash("same message")
	tracker := history.trackers[msgHash]

	// the firstSeen should now match the earliest entry we have after trimming
	earliestEntry := history.entries[0]

	t.Logf("FirstSeen: %v", tracker.firstSeen)
	t.Logf("Earliest entry time: %v", earliestEntry.time)

	// firstSeen should match the first entry after trimming
	assert.Equal(t, earliestEntry.time, tracker.firstSeen, "firstSeen should be updated after trimming")
}

func TestDuplicateDetector_InvalidMessageIDs(t *testing.T) {
	d := newDuplicateDetector(3, time.Hour)

	// send messages with invalid IDs
	resp := d.check(spamcheck.Request{
		Msg:    "test",
		UserID: "123",
		Meta:   spamcheck.MetaData{MessageID: 0}, // invalid ID
	})
	assert.False(t, resp.Spam)

	resp = d.check(spamcheck.Request{
		Msg:    "test",
		UserID: "123",
		Meta:   spamcheck.MetaData{MessageID: -1}, // negative ID
	})
	assert.False(t, resp.Spam)

	resp = d.check(spamcheck.Request{
		Msg:    "test",
		UserID: "123",
		Meta:   spamcheck.MetaData{MessageID: 1001}, // valid ID
	})
	assert.True(t, resp.Spam) // should trigger on 3rd duplicate

	// check that invalid IDs are not in ExtraDeleteIDs
	t.Logf("ExtraDeleteIDs: %v", resp.ExtraDeleteIDs)
	for _, id := range resp.ExtraDeleteIDs {
		assert.Greater(t, id, 0, "should not include invalid IDs (0 or negative)")
	}

	// extra IDs should only contain the valid ID (1001) if any
	// but since we clear on threshold, it might be empty
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

func TestDuplicateDetector_EditedMessagesShouldNotTriggerSpam(t *testing.T) {
	d := newDuplicateDetector(3, time.Hour) // threshold of 3

	// user posts original message
	resp := d.check(spamcheck.Request{
		Msg:    "hello world",
		UserID: "123",
		Meta:   spamcheck.MetaData{MessageID: 1001},
	})
	assert.False(t, resp.Spam, "original message should not be spam")

	// user edits message (same messageID, same or similar content)
	// this simulates how telegram sends edited messages - same messageID but processed as new check
	resp = d.check(spamcheck.Request{
		Msg:    "hello world",
		UserID: "123",
		Meta:   spamcheck.MetaData{MessageID: 1001},
	})
	assert.False(t, resp.Spam, "first edit should not be spam")

	// user edits again
	resp = d.check(spamcheck.Request{
		Msg:    "hello world",
		UserID: "123",
		Meta:   spamcheck.MetaData{MessageID: 1001},
	})
	assert.False(t, resp.Spam, "second edit should not be spam")

	// user edits again
	resp = d.check(spamcheck.Request{
		Msg:    "hello world",
		UserID: "123",
		Meta:   spamcheck.MetaData{MessageID: 1001},
	})
	assert.False(t, resp.Spam, "third edit should not be spam - edits of same message should not count as duplicates")

	// verify that actual duplicate (different messageID) still works
	resp = d.check(spamcheck.Request{
		Msg:    "hello world",
		UserID: "123",
		Meta:   spamcheck.MetaData{MessageID: 1002},
	})
	assert.False(t, resp.Spam, "second message not spam yet")

	resp = d.check(spamcheck.Request{
		Msg:    "hello world",
		UserID: "123",
		Meta:   spamcheck.MetaData{MessageID: 1003},
	})
	assert.True(t, resp.Spam, "third DIFFERENT message should trigger spam (real duplicates)")
}
