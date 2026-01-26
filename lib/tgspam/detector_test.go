package tgspam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/approved"
	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam/mocks"
)

func TestDetector_CheckWithShort(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: 1, MinMsgLen: 150})
	lr, err := d.LoadStopWords(bytes.NewBufferString("в личку\nвсем привет"))
	require.NoError(t, err)
	assert.Equal(t, LoadResult{StopWords: 2}, lr)

	t.Run("short message without spam", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "good message"})
		assert.False(t, spam)
		require.Len(t, cr, 3, cr)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "not found", cr[0].Details)
		assert.Equal(t, "emoji", cr[1].Name)
		assert.False(t, cr[1].Spam)
		assert.Equal(t, "0/1", cr[1].Details)
		assert.Equal(t, "message length", cr[2].Name)
		assert.False(t, cr[2].Spam)
		assert.Equal(t, "too short", cr[2].Details)
	})

	t.Run("short message with stopwords", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello, please send me a message в личку"})
		assert.True(t, spam)
		require.Len(t, cr, 3, cr)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.True(t, cr[0].Spam)
		assert.Equal(t, "в личку", cr[0].Details)
		assert.Equal(t, "emoji", cr[1].Name)
		assert.False(t, cr[1].Spam)
		assert.Equal(t, "0/1", cr[1].Details)
		assert.Equal(t, "message length", cr[2].Name)
		assert.False(t, cr[2].Spam)
		assert.Equal(t, "too short", cr[2].Details)
	})

	t.Run("short message with emojis", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello 😁🐶🍕"})
		assert.True(t, spam)
		require.Len(t, cr, 3, cr)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "not found", cr[0].Details)
		assert.Equal(t, "emoji", cr[1].Name)
		assert.True(t, cr[1].Spam)
		assert.Equal(t, "3/1", cr[1].Details)
		assert.Equal(t, "message length", cr[2].Name)
		assert.False(t, cr[2].Spam)
		assert.Equal(t, "too short", cr[2].Details)
	})

	t.Run("short message with emojis and stop words", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello 😁🐶🍕в личку"})
		assert.True(t, spam)
		require.Len(t, cr, 3, cr)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.True(t, cr[0].Spam)
		assert.Equal(t, "в личку", cr[0].Details)
		assert.Equal(t, "emoji", cr[1].Name)
		assert.True(t, cr[1].Spam)
		assert.Equal(t, "3/1", cr[1].Details)
		assert.Equal(t, "message length", cr[2].Name)
		assert.False(t, cr[2].Spam)
		assert.Equal(t, "too short", cr[2].Details)
	})

	t.Run("stopword with extra spaces and different case", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: 1, MinMsgLen: 150})
		lr, err := d.LoadStopWords(bytes.NewBufferString("дам денег"))
		require.NoError(t, err)
		assert.Equal(t, LoadResult{StopWords: 1}, lr)

		// test with extra spaces between words
		spam, cr := d.Check(spamcheck.Request{Msg: "Дам  денег"})
		assert.True(t, spam, "should detect stopword with extra spaces")
		require.Len(t, cr, 3, cr)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.True(t, cr[0].Spam, "should match 'дам денег' even with extra space in 'Дам  денег'")
		assert.Equal(t, "дам денег", cr[0].Details)

		// test with different case
		spam, cr = d.Check(spamcheck.Request{Msg: "ДАМ ДЕНЕГ"})
		assert.True(t, spam, "should detect stopword with uppercase")
		require.Len(t, cr, 3, cr)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.True(t, cr[0].Spam, "should match 'дам денег' even with uppercase 'ДАМ ДЕНЕГ'")
		assert.Equal(t, "дам денег", cr[0].Details)

		// test with multiple extra spaces
		spam, cr = d.Check(spamcheck.Request{Msg: "дам    денег"})
		assert.True(t, spam, "should detect stopword with multiple spaces")
		require.Len(t, cr, 3, cr)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.True(t, cr[0].Spam, "should match 'дам денег' even with multiple spaces")
		assert.Equal(t, "дам денег", cr[0].Details)
	})

	t.Run("short message skips classifier and similarity", func(t *testing.T) {
		// load spam and ham samples to enable classifier and similarity checks
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 50, SimilarityThreshold: 0.5})

		spamSamples := strings.NewReader("buy cheap viagra now\nclick here for free money\nwin lottery prize")
		hamSamples := strings.NewReader("hello world\nhow are you\nhave a good day")

		lr, err := d.LoadSamples(strings.NewReader(""), []io.Reader{spamSamples}, []io.Reader{hamSamples})
		require.NoError(t, err)
		assert.Positive(t, lr.SpamSamples)
		assert.Positive(t, lr.HamSamples)

		// test short message - should NOT have classifier or similarity results
		spam, cr := d.Check(spamcheck.Request{Msg: "hi"})
		assert.False(t, spam)

		// verify we only get message length check, no classifier or similarity
		require.Len(t, cr, 1)
		assert.Equal(t, "message length", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "too short", cr[0].Details)

		// verify classifier and similarity are NOT in the results
		for _, r := range cr {
			assert.NotEqual(t, "classifier", r.Name, "classifier should not run for short messages")
			assert.NotEqual(t, "similarity", r.Name, "similarity should not run for short messages")
		}

		// test long message - should have classifier and similarity results
		spam2, cr2 := d.Check(spamcheck.Request{Msg: "this is a much longer message that should trigger all checks including classifier and similarity"})
		assert.False(t, spam2)

		// verify we get classifier and similarity checks for long messages
		hasClassifier := false
		hasSimilarity := false
		for _, r := range cr2 {
			if r.Name == "classifier" {
				hasClassifier = true
			}
			if r.Name == "similarity" {
				hasSimilarity = true
			}
		}
		assert.True(t, hasClassifier, "classifier should run for long messages")
		assert.True(t, hasSimilarity, "similarity should run for long messages")
	})
}

func TestDetector_CheckStopWords(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1})
	lr, err := d.LoadStopWords(bytes.NewBufferString("в личку\nвсеМ прИвет\nspambot\n12345"))
	require.NoError(t, err)
	assert.Equal(t, LoadResult{StopWords: 4}, lr)

	tests := []struct {
		name     string
		message  string
		username string
		userID   string
		expected bool
		details  string
	}{
		{
			name:     "Stop word present in message",
			message:  "Hello, please send me a message в личкУ",
			username: "user1",
			userID:   "987654321",
			expected: true,
			details:  "в личку",
		},
		{
			name:     "Stop word present with emoji",
			message:  "👋Всем привет\nИщу амбициозного человека к се6е в команду\nКто в поисках дополнительного заработка или хочет попробовать себя в новой  сфере деятельности! 👨🏻\u200d💻\nПишите в лс✍️",
			username: "user1",
			userID:   "987654321",
			expected: true,
			details:  "всем привет",
		},
		{
			name:     "No stop word present",
			message:  "Hello, how are you?",
			username: "user1",
			userID:   "987654321",
			expected: false,
			details:  "not found",
		},
		{
			name:     "Case insensitive stop word present",
			message:  "Hello, please send me a message В ЛИЧКУ",
			username: "user1",
			userID:   "987654321",
			expected: true,
			details:  "в личку",
		},
		{
			name:     "Stop word in username",
			message:  "Hello, how are you?",
			username: "spambot_seller",
			userID:   "987654321",
			expected: true,
			details:  "spambot",
		},
		{
			name:     "Stop word in username case insensitive",
			message:  "Hello, how are you?",
			username: "SpAmBoT_seller",
			userID:   "987654321",
			expected: true,
			details:  "spambot",
		},
		{
			name:     "Stop word in user ID",
			message:  "Hello, how are you?",
			username: "normal_user",
			userID:   "12345_account",
			expected: true,
			details:  "12345",
		},
		{
			name:     "No stop word in anything",
			message:  "Regular message",
			username: "normal_user",
			userID:   "987654321",
			expected: false,
			details:  "not found",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spam, cr := d.Check(spamcheck.Request{Msg: test.message, UserName: test.username, UserID: test.userID})
			assert.Equal(t, test.expected, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "stopword", cr[0].Name)
			assert.Equal(t, test.details, cr[0].Details)
			t.Logf("%+v", cr[0].Details)
		})
	}
}

func TestDetector_CheckStopWordsExactMatch(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1})
	// "=buy now" requires exact match, "hello" uses substring match
	lr, err := d.LoadStopWords(bytes.NewBufferString("=buy now\nhello\n=spammer123"))
	require.NoError(t, err)
	assert.Equal(t, LoadResult{StopWords: 3}, lr)

	tests := []struct {
		name     string
		message  string
		username string
		userID   string
		expected bool
		details  string
	}{
		{
			name:     "exact match - message equals stop word",
			message:  "buy now",
			expected: true,
			details:  "buy now",
		},
		{
			name:     "exact match - message equals stop word case insensitive",
			message:  "BUY NOW",
			expected: true,
			details:  "buy now",
		},
		{
			name:     "exact match - message contains stop word but not exact",
			message:  "please buy now today",
			expected: false,
			details:  "not found",
		},
		{
			name:     "exact match - message with extra spaces normalized",
			message:  "buy  now",
			expected: true,
			details:  "buy now",
		},
		{
			name:     "substring match - message contains stop word",
			message:  "hello world",
			expected: true,
			details:  "hello",
		},
		{
			name:     "substring match - stop word anywhere in message",
			message:  "say hello to everyone",
			expected: true,
			details:  "hello",
		},
		{
			name:     "exact match username - exact match",
			message:  "normal message",
			username: "spammer123",
			expected: true,
			details:  "spammer123",
		},
		{
			name:     "exact match username - not exact",
			message:  "normal message",
			username: "spammer123_bot",
			expected: false,
			details:  "not found",
		},
		{
			name:     "exact match user id - exact match",
			message:  "normal message",
			userID:   "spammer123",
			expected: true,
			details:  "spammer123",
		},
		{
			name:     "no match",
			message:  "regular text",
			expected: false,
			details:  "not found",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spam, cr := d.Check(spamcheck.Request{Msg: test.message, UserName: test.username, UserID: test.userID})
			assert.Equal(t, test.expected, spam, "spam detection mismatch")
			require.Len(t, cr, 1)
			assert.Equal(t, "stopword", cr[0].Name)
			assert.Equal(t, test.details, cr[0].Details)
		})
	}
}

func TestDetector_CheckStopWordsEdgeCases(t *testing.T) {
	t.Run("equals-only stop word should not match anything", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1})
		_, err := d.LoadStopWords(bytes.NewBufferString("=\nhello"))
		require.NoError(t, err)

		// empty message should not match "=" stop word
		spam, cr := d.Check(spamcheck.Request{Msg: ""})
		assert.False(t, spam)
		assert.Equal(t, "not found", cr[0].Details)

		// whitespace-only message should not match "=" stop word
		spam, cr = d.Check(spamcheck.Request{Msg: "   "})
		assert.False(t, spam)
		assert.Equal(t, "not found", cr[0].Details)

		// but "hello" substring still works
		spam, cr = d.Check(spamcheck.Request{Msg: "say hello"})
		assert.True(t, spam)
		assert.Equal(t, "hello", cr[0].Details)
	})

	t.Run("double equals prefix matches literal equals", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1})
		_, err := d.LoadStopWords(bytes.NewBufferString("==test"))
		require.NoError(t, err)

		// "==test" means exact match for "=test" (first = is prefix, second is literal)
		spam, cr := d.Check(spamcheck.Request{Msg: "=test"})
		assert.True(t, spam)
		assert.Equal(t, "=test", cr[0].Details)

		// should not match "test" without the leading equals
		spam, cr = d.Check(spamcheck.Request{Msg: "test"})
		assert.False(t, spam)
		assert.Equal(t, "not found", cr[0].Details)
	})
}

func TestDetector_CheckEmojis(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: 2})
	tests := []struct {
		name  string
		input string
		count int
		spam  bool
	}{
		{"NoEmoji", "Hello, world!", 0, false},
		{"OneEmoji", "Hi there 👋", 1, false},
		{"TwoEmojis", "Good morning 🌞🌻", 2, false},
		{"Mixed", "👨‍👩‍👧‍👦 Family emoji", 1, false},
		{"EmojiSequences", "🏳️‍🌈 Rainbow flag", 1, false},
		{"TextAfterEmoji", "😊 Have a nice day!", 1, false},
		{"OnlyEmojis", "😁🐶🍕", 3, true},
		{"WithCyrillic", "Привет 🌞 🍕 мир! 👋", 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spam, cr := d.Check(spamcheck.Request{Msg: tt.input})
			assert.Equal(t, tt.spam, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "emoji", cr[0].Name)
			assert.Equal(t, tt.spam, cr[0].Spam)
			assert.Equal(t, fmt.Sprintf("%d/2", tt.count), cr[0].Details)
		})
	}
}

func TestDetector_CheckDuplicates(t *testing.T) {
	d := NewDetector(Config{
		DuplicateDetection: struct {
			Threshold int
			Window    time.Duration
		}{
			Threshold: 3,
			Window:    time.Hour,
		},
	})

	// first message - not spam
	spam, cr := d.Check(spamcheck.Request{Msg: "test message", UserID: "123"})
	assert.False(t, spam)
	dupResp := findResponseByName(cr, "duplicate")
	require.NotNil(t, dupResp)
	assert.False(t, dupResp.Spam)

	// second identical message - still not spam (threshold is 3)
	spam, cr = d.Check(spamcheck.Request{Msg: "test message", UserID: "123"})
	assert.False(t, spam)
	dupResp = findResponseByName(cr, "duplicate")
	require.NotNil(t, dupResp)
	assert.False(t, dupResp.Spam)

	// third identical message - should trigger spam
	spam, cr = d.Check(spamcheck.Request{Msg: "test message", UserID: "123"})
	assert.True(t, spam)
	dupResp = findResponseByName(cr, "duplicate")
	require.NotNil(t, dupResp)
	assert.True(t, dupResp.Spam)
	assert.Contains(t, dupResp.Details, "3 times")

	// different message from same user - not spam
	spam, _ = d.Check(spamcheck.Request{Msg: "different message", UserID: "123"})
	assert.False(t, spam)

	// same message from different user - not spam
	spam, _ = d.Check(spamcheck.Request{Msg: "test message", UserID: "456"})
	assert.False(t, spam)
}

func TestDetector_CheckDuplicatesDisabled(t *testing.T) {
	// test with threshold 0 (disabled)
	d := NewDetector(Config{
		DuplicateDetection: struct {
			Threshold int
			Window    time.Duration
		}{
			Threshold: 0,
			Window:    time.Hour,
		},
	})

	// send same message multiple times - should never trigger
	for range 5 {
		spam, cr := d.Check(spamcheck.Request{Msg: "test", UserID: "123"})
		assert.False(t, spam)
		dupResp := findResponseByName(cr, "duplicate")
		if dupResp != nil {
			assert.False(t, dupResp.Spam)
			assert.Equal(t, "check disabled", dupResp.Details)
		}
	}
}

func TestDetector_CheckDuplicatesTimeWindow(t *testing.T) {
	d := NewDetector(Config{
		DuplicateDetection: struct {
			Threshold int
			Window    time.Duration
		}{
			Threshold: 2,
			Window:    100 * time.Millisecond,
		},
	})

	// first message
	spam, _ := d.Check(spamcheck.Request{Msg: "test", UserID: "123"})
	assert.False(t, spam)

	// second message - should trigger
	spam, cr := d.Check(spamcheck.Request{Msg: "test", UserID: "123"})
	assert.True(t, spam)
	dupResp := findResponseByName(cr, "duplicate")
	require.NotNil(t, dupResp)
	assert.True(t, dupResp.Spam)

	// wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// third message - should not trigger as previous ones expired
	spam, _ = d.Check(spamcheck.Request{Msg: "test", UserID: "123"})
	assert.False(t, spam)
}

func TestDetector_CheckDuplicatesConcurrency(t *testing.T) {
	d := NewDetector(Config{
		DuplicateDetection: struct {
			Threshold int
			Window    time.Duration
		}{
			Threshold: 50,
			Window:    time.Minute,
		},
	})

	userID := "12345"
	message := "concurrent test message"
	concurrency := 10
	iterations := 10
	expectedCount := concurrency * iterations

	var wg sync.WaitGroup
	wg.Add(concurrency)

	// send same message concurrently from same user
	for range concurrency {
		go func() {
			defer wg.Done()
			for range iterations {
				d.Check(spamcheck.Request{
					UserID: userID,
					Msg:    message,
				})
			}
		}()
	}

	wg.Wait()

	// verify the count is correct (no lost updates)
	// one more check to get the final count
	spam, cr := d.Check(spamcheck.Request{
		UserID: userID,
		Msg:    message,
	})

	assert.True(t, spam, "should be detected as spam after %d messages", expectedCount+1)

	dupResp := findResponseByName(cr, "duplicate")
	require.NotNil(t, dupResp)
	assert.True(t, dupResp.Spam)

	// extract count from details
	var count int
	_, err := fmt.Sscanf(dupResp.Details, "message repeated %d times", &count)
	require.NoError(t, err)

	// count should be exactly expectedCount + 1 (for the final check)
	assert.Equal(t, expectedCount+1, count, "count should be accurate with no race conditions")
}

func TestDetector_CheckDuplicatesMemoryProtection(t *testing.T) {
	d := NewDetector(Config{
		DuplicateDetection: struct {
			Threshold int
			Window    time.Duration
		}{
			Threshold: 2,         // low threshold to detect duplicates
			Window:    time.Hour, // long window to keep all messages
		},
	})

	userID := "12345" // use numeric user ID

	// send many unique messages to try to exhaust memory
	// the detector should limit to maxEntriesPerUser (200)
	for i := range 300 {
		message := fmt.Sprintf("unique message %d", i)
		d.Check(spamcheck.Request{
			UserID: userID,
			Msg:    message,
		})
	}

	// now send a duplicate of an early message that should have been evicted
	// message 50 was sent early (300-50 = 250 messages ago), so it should be evicted
	spam, cr := d.Check(spamcheck.Request{
		UserID: userID,
		Msg:    "unique message 50",
	})
	assert.False(t, spam, "early message should have been evicted, not detected as duplicate")
	dupResp := findResponseByName(cr, "duplicate")
	assert.False(t, dupResp.Spam, "should not be spam since it was evicted")

	// send a duplicate of a recent message that should still be tracked
	// message 250 is within the last 200 messages, so it should still be tracked
	spam, cr = d.Check(spamcheck.Request{
		UserID: userID,
		Msg:    "unique message 250",
	})
	assert.True(t, spam, "recent message should be tracked and trigger spam detection")
	dupResp = findResponseByName(cr, "duplicate")
	require.NotNil(t, dupResp)
	assert.True(t, dupResp.Spam)
	assert.Contains(t, dupResp.Details, "message repeated 2 times")
}

func TestDetector_DuplicateDetectionForApprovedUsers(t *testing.T) {
	// this test demonstrates bug: approved users bypass duplicate detection
	d := NewDetector(Config{
		FirstMessageOnly:   true,
		FirstMessagesCount: 1,
		DuplicateDetection: struct {
			Threshold int
			Window    time.Duration
		}{
			Threshold: 2,
			Window:    5 * time.Minute,
		},
	})

	userID := "8050302772" // real user from the bug report
	duplicateMsg := "Кто не против пообщатся"

	// first message - user not approved yet
	req1 := spamcheck.Request{
		Msg:    duplicateMsg,
		UserID: userID,
		Meta:   spamcheck.MetaData{MessageID: 658144},
	}
	spam1, results1 := d.Check(req1)

	// should be ham, user gets approved
	assert.False(t, spam1, "first message should be ham")
	dupResp1 := findResponseByName(results1, "duplicate")
	require.NotNil(t, dupResp1, "duplicate check should have run")
	assert.False(t, dupResp1.Spam, "first message is not a duplicate")

	// verify user is now approved
	d.lock.RLock()
	userInfo := d.approvedUsers[userID]
	d.lock.RUnlock()
	assert.Equal(t, 1, userInfo.Count, "user should have count=1 after first message")

	// second identical message - user IS approved now
	req2 := spamcheck.Request{
		Msg:    duplicateMsg,
		UserID: userID,
		Meta:   spamcheck.MetaData{MessageID: 658145},
	}
	spam2, results2 := d.Check(req2)

	// BUG: currently returns false (pre-approved), should detect duplicate spam
	assert.True(t, spam2, "duplicate message should be detected as spam even for approved user")

	// verify duplicate check ran and detected spam
	dupResp2 := findResponseByName(results2, "duplicate")
	require.NotNil(t, dupResp2, "duplicate check should have run for approved user")
	assert.True(t, dupResp2.Spam, "duplicate check should detect spam")
	assert.Contains(t, dupResp2.Details, "message repeated 2 times")

	// verify ExtraDeleteIDs are set for cleanup
	assert.NotEmpty(t, dupResp2.ExtraDeleteIDs, "should have extra message IDs to delete")
	assert.Contains(t, dupResp2.ExtraDeleteIDs, 658144, "should include first message ID for deletion")
}

func TestDetector_DuplicateDetectionEdgeCases(t *testing.T) {
	t.Run("approved user with different messages - no false positive", func(t *testing.T) {
		// approved users should be able to send different messages without spam detection
		d := NewDetector(Config{
			FirstMessageOnly:   true,
			FirstMessagesCount: 1,
			DuplicateDetection: struct {
				Threshold int
				Window    time.Duration
			}{
				Threshold: 2,
				Window:    5 * time.Minute,
			},
		})

		userID := "12345"

		// first message - gets approved
		spam1, _ := d.Check(spamcheck.Request{Msg: "first message", UserID: userID})
		assert.False(t, spam1)

		// second different message from approved user - should be ham
		spam2, results2 := d.Check(spamcheck.Request{Msg: "second different message", UserID: userID})
		assert.False(t, spam2, "different message from approved user should be ham")

		// verify it was pre-approved (duplicate check ran but found no duplicates)
		preApproved := findResponseByName(results2, "pre-approved")
		assert.NotNil(t, preApproved, "should have pre-approved response")
	})

	t.Run("duplicate spam with ExtraDeleteIDs", func(t *testing.T) {
		// test that ExtraDeleteIDs are returned for cleanup when duplicates detected
		d := NewDetector(Config{
			FirstMessageOnly:   true,
			FirstMessagesCount: 2, // need 2 messages before approval
			DuplicateDetection: struct {
				Threshold int
				Window    time.Duration
			}{
				Threshold: 3, // trigger on 3rd duplicate
				Window:    5 * time.Minute,
			},
		})

		userID := "999"
		msg := "spam spam spam"

		// send 3 identical messages
		d.Check(spamcheck.Request{Msg: msg, UserID: userID, Meta: spamcheck.MetaData{MessageID: 100}})
		d.Check(spamcheck.Request{Msg: msg, UserID: userID, Meta: spamcheck.MetaData{MessageID: 101}})
		spam3, results3 := d.Check(spamcheck.Request{Msg: msg, UserID: userID, Meta: spamcheck.MetaData{MessageID: 102}})

		assert.True(t, spam3, "third duplicate should be spam")
		dupResp := findResponseByName(results3, "duplicate")
		require.NotNil(t, dupResp)
		assert.True(t, dupResp.Spam)

		// should have ExtraDeleteIDs for previous duplicates
		assert.Len(t, dupResp.ExtraDeleteIDs, 2, "should have 2 previous message IDs")
		assert.Contains(t, dupResp.ExtraDeleteIDs, 100)
		assert.Contains(t, dupResp.ExtraDeleteIDs, 101)
	})

	t.Run("pre-approval still works for non-duplicate content checks", func(t *testing.T) {
		// ensure approved users still skip expensive content checks (similarity, classifier)
		d := NewDetector(Config{
			FirstMessageOnly:    true,
			FirstMessagesCount:  1,
			SimilarityThreshold: 0.8, // enable similarity check
			MinMsgLen:           10,
			DuplicateDetection: struct {
				Threshold int
				Window    time.Duration
			}{
				Threshold: 2,
				Window:    5 * time.Minute,
			},
		})

		// load spam samples for similarity check
		d.LoadSamples(bytes.NewBufferString(""), []io.Reader{bytes.NewBufferString("buy crypto now\nget rich quick")}, []io.Reader{bytes.NewBufferString("hello world")})

		userID := "777"

		// first message - user gets approved
		spam1, _ := d.Check(spamcheck.Request{Msg: "first message here", UserID: userID})
		assert.False(t, spam1)

		// second message similar to spam samples but from approved user
		spam2, results2 := d.Check(spamcheck.Request{Msg: "buy crypto now and get rich", UserID: userID})
		assert.False(t, spam2, "approved user should skip content checks")

		// verify similarity check was NOT run (pre-approval skipped it)
		similarityResp := findResponseByName(results2, "similarity")
		assert.Nil(t, similarityResp, "similarity check should be skipped for approved users")

		// but duplicate check should have run
		dupResp := findResponseByName(results2, "duplicate")
		assert.NotNil(t, dupResp, "duplicate check should run for approved users")
	})
}

func TestSpam_CheckIsCasSpam(t *testing.T) {
	tests := []struct {
		name           string
		mockResp       string
		mockStatusCode int
		expected       bool
	}{
		{
			name:           "User is not a spammer",
			mockResp:       `{"ok": false, "description": "Not a spammer"}`,
			mockStatusCode: 200,
			expected:       false,
		},
		{
			name:           "User is not a spammer, message case",
			mockResp:       `{"ok": false, "description": "Not A spamMer."}`,
			mockStatusCode: 200,
			expected:       false,
		},
		{
			name:           "User is a spammer",
			mockResp:       `{"ok": true, "description": "Is a spammer"}`,
			mockStatusCode: 200,
			expected:       true,
		},
		{
			name:           "User is a spammer",
			mockResp:       `{"ok": true, "description": ""}`,
			mockStatusCode: 200,
			expected:       true,
		},
		{
			name:           "HTTP 503 service unavailable",
			mockResp:       `{"ok": false, "description": "not found"}`,
			mockStatusCode: 503,
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockedHTTPClient := &mocks.HTTPClientMock{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: tt.mockStatusCode,
						Body:       io.NopCloser(bytes.NewBufferString(tt.mockResp)),
					}, nil
				},
			}

			d := NewDetector(Config{
				CasAPI:           "http://localhost",
				HTTPClient:       mockedHTTPClient,
				MaxAllowedEmoji:  -1,
				FirstMessageOnly: true,
			})
			spam, cr := d.Check(spamcheck.Request{UserID: "123"})
			assert.Equal(t, tt.expected, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "cas", cr[0].Name)
			assert.Equal(t, tt.expected, cr[0].Spam)

			// for 5xx errors, retry logic kicks in, so we expect different behavior
			if tt.mockStatusCode >= 500 {
				assert.Contains(t, cr[0].Details, "failed to send request")
				assert.Greater(t, len(mockedHTTPClient.DoCalls()), 1, "should retry on 5xx errors")
			} else {
				respDetails := struct {
					OK          bool   `json:"ok"`
					Description string `json:"description"`
				}{}
				err := json.Unmarshal([]byte(tt.mockResp), &respDetails)
				require.NoError(t, err)
				expResp := strings.ToLower(respDetails.Description)
				if expResp == "" {
					expResp = "spam detected"
				}
				expResp = strings.TrimSuffix(expResp, ".")
				assert.Equal(t, expResp, cr[0].Details)
				assert.Len(t, mockedHTTPClient.DoCalls(), 1)
			}
		})
	}
}

func TestSpam_CheckIsCasSpamEmptyUserID(t *testing.T) {
	mockedHTTPClient := &mocks.HTTPClientMock{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			assert.Fail(t, "HTTP client should not be called with empty userID")
			return nil, nil
		},
	}

	d := NewDetector(Config{
		CasAPI:           "http://localhost",
		HTTPClient:       mockedHTTPClient,
		MaxAllowedEmoji:  -1,
		FirstMessageOnly: true,
	})
	spam, cr := d.Check(spamcheck.Request{UserID: "", Msg: "test message"})
	assert.False(t, spam)

	// find the CAS check in the results
	var casCheck *spamcheck.Response
	for _, check := range cr {
		if check.Name == "cas" {
			casCheck = &check
			break
		}
	}

	require.NotNil(t, casCheck, "CAS check should be included in results")
	assert.False(t, casCheck.Spam)
	assert.Equal(t, "check disabled", casCheck.Details)

	// verify HTTP client was never called
	assert.Empty(t, mockedHTTPClient.DoCalls())
}

func TestSpam_CheckIsCasSpamUserAgent(t *testing.T) {
	const customUserAgent = "MyCustomUserAgent/1.0"

	tests := []struct {
		name           string
		userAgent      string
		expectedHeader string
	}{
		{
			name:           "with custom user agent",
			userAgent:      customUserAgent,
			expectedHeader: customUserAgent,
		},
		{
			name:           "with default user agent",
			userAgent:      "",
			expectedHeader: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockedHTTPClient := &mocks.HTTPClientMock{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					// check if User-Agent header is set correctly
					if tt.expectedHeader != "" {
						assert.Equal(t, tt.expectedHeader, req.Header.Get("User-Agent"))
					} else {
						// default User-Agent should be Go's default, let's just verify it's not our custom one
						assert.NotEqual(t, customUserAgent, req.Header.Get("User-Agent"))
					}

					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewBufferString(`{"ok": false, "description": "Not a spammer"}`)),
					}, nil
				},
			}

			d := NewDetector(Config{
				CasAPI:           "http://localhost",
				CasUserAgent:     tt.userAgent,
				HTTPClient:       mockedHTTPClient,
				MaxAllowedEmoji:  -1,
				FirstMessageOnly: true,
			})

			d.Check(spamcheck.Request{UserID: "123", Msg: "test message"})

			// verify HTTP client was called
			assert.Len(t, mockedHTTPClient.DoCalls(), 1)
		})
	}
}

func TestSpam_CheckIsCasSpamRetry(t *testing.T) {
	t.Run("retry on network failure then success", func(t *testing.T) {
		callCount := 0
		mockedHTTPClient := &mocks.HTTPClientMock{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				callCount++
				if callCount == 1 {
					return nil, fmt.Errorf("network error")
				}
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewBufferString(`{"ok": false, "description": "not found"}`)),
				}, nil
			},
		}

		d := NewDetector(Config{
			CasAPI:           "http://localhost",
			HTTPClient:       mockedHTTPClient,
			MaxAllowedEmoji:  -1,
			FirstMessageOnly: true,
		})
		spam, cr := d.Check(spamcheck.Request{UserID: "123", Msg: "test"})
		assert.False(t, spam)
		assert.Equal(t, 2, callCount, "should retry once after network failure")
		var casCheck *spamcheck.Response
		for _, check := range cr {
			if check.Name == "cas" {
				casCheck = &check
				break
			}
		}
		require.NotNil(t, casCheck)
		assert.False(t, casCheck.Spam)
	})

	t.Run("retry on 5xx error then success", func(t *testing.T) {
		callCount := 0
		mockedHTTPClient := &mocks.HTTPClientMock{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				callCount++
				if callCount == 1 {
					return &http.Response{
						StatusCode: 500,
						Body:       io.NopCloser(bytes.NewBufferString("Internal Server Error")),
					}, nil
				}
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewBufferString(`{"ok": false, "description": "not found"}`)),
				}, nil
			},
		}

		d := NewDetector(Config{
			CasAPI:           "http://localhost",
			HTTPClient:       mockedHTTPClient,
			MaxAllowedEmoji:  -1,
			FirstMessageOnly: true,
		})
		spam, cr := d.Check(spamcheck.Request{UserID: "123", Msg: "test"})
		assert.False(t, spam)
		assert.Equal(t, 2, callCount, "should retry once after 5xx error")
		var casCheck *spamcheck.Response
		for _, check := range cr {
			if check.Name == "cas" {
				casCheck = &check
				break
			}
		}
		require.NotNil(t, casCheck)
		assert.False(t, casCheck.Spam)
	})

	t.Run("max retries exceeded", func(t *testing.T) {
		callCount := 0
		mockedHTTPClient := &mocks.HTTPClientMock{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				callCount++
				return nil, fmt.Errorf("network error")
			},
		}

		d := NewDetector(Config{
			CasAPI:           "http://localhost",
			HTTPClient:       mockedHTTPClient,
			MaxAllowedEmoji:  -1,
			FirstMessageOnly: true,
		})
		spam, cr := d.Check(spamcheck.Request{UserID: "123", Msg: "test"})
		assert.False(t, spam)
		assert.Equal(t, 3, callCount, "should attempt 3 times with Repeats=3")
		var casCheck *spamcheck.Response
		for _, check := range cr {
			if check.Name == "cas" {
				casCheck = &check
				break
			}
		}
		require.NotNil(t, casCheck)
		assert.False(t, casCheck.Spam)
		assert.Contains(t, casCheck.Details, "failed to send request")
	})
}

func TestSpam_CheckIsCasSpamHTMLResponse(t *testing.T) {
	t.Run("retry on HTML response then success", func(t *testing.T) {
		// test issue #325: CAS returns HTML error page instead of JSON, then succeeds
		callCount := 0
		mockedHTTPClient := &mocks.HTTPClientMock{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				callCount++
				if callCount == 1 {
					return &http.Response{
						StatusCode: 200,
						Header:     http.Header{"Content-Type": []string{"text/html"}},
						Body:       io.NopCloser(bytes.NewBufferString("<html><body>Error</body></html>")),
					}, nil
				}
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewBufferString(`{"ok": false, "description": "not found"}`)),
				}, nil
			},
		}

		d := NewDetector(Config{
			CasAPI:           "http://localhost",
			HTTPClient:       mockedHTTPClient,
			MaxAllowedEmoji:  -1,
			FirstMessageOnly: true,
		})
		spam, cr := d.Check(spamcheck.Request{UserID: "123", Msg: "test"})
		assert.False(t, spam)
		assert.Equal(t, 2, callCount, "should retry once after HTML response")
		var casCheck *spamcheck.Response
		for _, check := range cr {
			if check.Name == "cas" {
				casCheck = &check
				break
			}
		}
		require.NotNil(t, casCheck)
		assert.False(t, casCheck.Spam)
	})

	t.Run("max retries on persistent HTML response", func(t *testing.T) {
		callCount := 0
		mockedHTTPClient := &mocks.HTTPClientMock{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				callCount++
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"Content-Type": []string{"text/html"}},
					Body:       io.NopCloser(bytes.NewBufferString("<html><body>Error</body></html>")),
				}, nil
			},
		}

		d := NewDetector(Config{
			CasAPI:           "http://localhost",
			HTTPClient:       mockedHTTPClient,
			MaxAllowedEmoji:  -1,
			FirstMessageOnly: true,
		})
		spam, cr := d.Check(spamcheck.Request{UserID: "123", Msg: "test"})
		assert.False(t, spam)
		assert.Equal(t, 3, callCount, "should attempt 3 times with Repeats=3")
		var casCheck *spamcheck.Response
		for _, check := range cr {
			if check.Name == "cas" {
				casCheck = &check
				break
			}
		}
		require.NotNil(t, casCheck)
		assert.False(t, casCheck.Spam)
		assert.Contains(t, casCheck.Details, "unexpected content type")
	})
}

func TestSpam_CheckIsCasSpamNon200Status(t *testing.T) {
	t.Run("retry on 4xx errors then success", func(t *testing.T) {
		callCount := 0
		mockedHTTPClient := &mocks.HTTPClientMock{
			DoFunc: func(req *http.Request) (*http.Response, error) {
				callCount++
				if callCount == 1 {
					return &http.Response{
						StatusCode: 429,
						Body:       io.NopCloser(bytes.NewBufferString("Rate limited")),
					}, nil
				}
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(bytes.NewBufferString(`{"ok": false, "description": "not found"}`)),
				}, nil
			},
		}

		d := NewDetector(Config{
			CasAPI:           "http://localhost",
			HTTPClient:       mockedHTTPClient,
			MaxAllowedEmoji:  -1,
			FirstMessageOnly: true,
		})
		spam, cr := d.Check(spamcheck.Request{UserID: "123", Msg: "test"})
		assert.False(t, spam)
		assert.Equal(t, 2, callCount, "should retry once after 429 error")
		var casCheck *spamcheck.Response
		for _, check := range cr {
			if check.Name == "cas" {
				casCheck = &check
				break
			}
		}
		require.NotNil(t, casCheck)
		assert.False(t, casCheck.Spam)
	})

	t.Run("max retries on persistent 4xx errors", func(t *testing.T) {
		tests := []struct {
			name       string
			statusCode int
		}{
			{"400 Bad Request", 400},
			{"404 Not Found", 404},
			{"429 Too Many Requests", 429},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				callCount := 0
				mockedHTTPClient := &mocks.HTTPClientMock{
					DoFunc: func(req *http.Request) (*http.Response, error) {
						callCount++
						return &http.Response{
							StatusCode: tt.statusCode,
							Body:       io.NopCloser(bytes.NewBufferString("Error")),
						}, nil
					},
				}

				d := NewDetector(Config{
					CasAPI:           "http://localhost",
					HTTPClient:       mockedHTTPClient,
					MaxAllowedEmoji:  -1,
					FirstMessageOnly: true,
				})
				spam, cr := d.Check(spamcheck.Request{UserID: "123", Msg: "test"})
				assert.False(t, spam)
				assert.Equal(t, 3, callCount, "should attempt 3 times with Repeats=3")
				var casCheck *spamcheck.Response
				for _, check := range cr {
					if check.Name == "cas" {
						casCheck = &check
						break
					}
				}
				require.NotNil(t, casCheck)
				assert.False(t, casCheck.Spam)
				assert.Contains(t, casCheck.Details, "unexpected status")
			})
		}
	})
}

func TestDetector_CheckSimilarity(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1})
	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, nil)
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 2}, lr)
	d.classifier.reset() // we don't need a classifier for this test
	assert.Len(t, d.tokenizedSpam, 2)
	t.Logf("%+v", d.tokenizedSpam)
	assert.Equal(t, map[string]int{"win": 1, "free": 1, "iphone": 1}, d.tokenizedSpam[0])
	assert.Equal(t, map[string]int{"lottery": 1, "prize": 1}, d.tokenizedSpam[1])

	tests := []struct {
		name      string
		message   string
		threshold float64
		expected  bool
	}{
		{"Not Spam", "Hello, how are you?", 0.5, false},
		{"Exact Match", "Win a free iPhone now!", 0.5, true},
		{"Similar Match", "You won a lottery prize!", 0.3, true},
		{"High Threshold", "You won a lottery prize!", 0.9, false},
		{"Partial Match", "win free", 0.9, false},
		{"Low Threshold", "win free", 0.8, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d.SimilarityThreshold = test.threshold // update threshold for each test case
			spam, cr := d.Check(spamcheck.Request{Msg: test.message})
			assert.Equal(t, test.expected, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "similarity", cr[0].Name)
		})
	}
}

func TestDetector_CheckClassifier(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1, MinSpamProbability: 60})
	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	hamsSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamsSamples})
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 2, HamSamples: 3}, lr)
	d.tokenizedSpam = nil // we don't need tokenizedSpam samples for this test
	assert.Equal(t, 5, d.classifier.nAllDocument)
	exp := map[string]map[spamClass]int{"win": {"spam": 1}, "free": {"spam": 1}, "iphone": {"spam": 1}, "lottery": {"spam": 1},
		"prize": {"spam": 1}, "hello": {"ham": 1}, "world": {"ham": 1}, "how": {"ham": 1}, "are": {"ham": 1}, "you": {"ham": 1},
		"have": {"ham": 1}, "good": {"ham": 1}, "day": {"ham": 1}}
	assert.Equal(t, exp, d.classifier.learningResults)

	tests := []struct {
		name     string
		message  string
		expected bool
		desc     string
	}{
		{"clean ham", "Hello, how are you?", false, "probability of ham: 92.83%"},
		{"clean spam", "Win a free iPhone now!", true, "probability of spam: 90.81%"},
		{"a little bit spam", "You won a free lottery iphone good day", true, "probability of spam: 66.23%"},
		{"spam below threshold", "You won a free lottery iphone have a good day", false, "probability of spam: 53.36%"},
		{"mostly ham", "win a good day", false, "probability of ham: 65.39%"},
		{"mostly spam", "free  blah another one user writes good things iPhone day", true, "probability of spam: 75.70%"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spam, cr := d.Check(spamcheck.Request{Msg: test.message})
			assert.Equal(t, test.expected, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "classifier", cr[0].Name)
			assert.Equal(t, test.expected, cr[0].Spam)
			t.Logf("%+v", cr[0].Details)
			assert.Equal(t, test.desc, cr[0].Details)
		})
	}

	t.Run("without minSpamProbability", func(t *testing.T) {
		d.MinSpamProbability = 0
		spam, cr := d.Check(spamcheck.Request{Msg: "You won a free lottery iphone have a good day"})
		assert.True(t, spam)
		assert.Equal(t, "probability of spam: 53.36%", cr[0].Details)

	})
}

func TestDetector_CheckClassifierNoHam(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1, MinSpamProbability: 60})
	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, nil)
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 2, HamSamples: 0}, lr)
	d.tokenizedSpam = nil // we don't need tokenizedSpam samples for this test
	assert.Equal(t, 2, d.classifier.nAllDocument)
	assert.Equal(t, 2, d.classifier.nDocumentByClass["spam"])
	assert.Equal(t, 0, d.classifier.nDocumentByClass["ham"])
	exp := map[string]map[spamClass]int{"win": {"spam": 1}, "free": {"spam": 1}, "iphone": {"spam": 1},
		"lottery": {"spam": 1}, "prize": {"spam": 1}}
	assert.Equal(t, exp, d.classifier.learningResults)

	tests := []string{
		"Hello, how are you?",
		"Win a free iPhone now!",
		"You won a free lottery iphone good day",
		"You won a free lottery iphone have a good day",
		"win a good day",
		"free  blah another one user writes good things iPhone day",
	}

	for _, test := range tests {
		t.Run(test, func(t *testing.T) {
			spam, cr := d.Check(spamcheck.Request{Msg: test})
			assert.False(t, spam)
			require.Empty(t, cr)
		})
	}
}

func TestDetector_CheckOpenAI(t *testing.T) {
	t.Run("with openai and first-only", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.True(t, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "openai", cr[0].Name)
		assert.True(t, cr[0].Spam)
		assert.Equal(t, "bad text, confidence: 100%", cr[0].Details)
		assert.Len(t, mockOpenAIClient.CreateChatCompletionCalls(), 1)
	})

	t.Run("with openai and not first-only", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.False(t, spam)
		require.Empty(t, cr)
		assert.Empty(t, mockOpenAIClient.CreateChatCompletionCalls())
	})

	t.Run("without openai", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1})
		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.False(t, spam)
		require.Empty(t, cr)
	})

	t.Run("with openai, first-only but spam detected before", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		_, err := d.LoadStopWords(strings.NewReader("some message"))
		require.NoError(t, err)

		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.True(t, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.True(t, cr[0].Spam)
		assert.Equal(t, "some message", cr[0].Details)
		assert.Empty(t, mockOpenAIClient.CreateChatCompletionCalls())
	})

	t.Run("with openai, first-only spam detected before, veto passes", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true, OpenAIVeto: true})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		_, err := d.LoadStopWords(strings.NewReader("some message"))
		require.NoError(t, err)

		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.True(t, spam)
		require.Len(t, cr, 2)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.True(t, cr[0].Spam)
		assert.Equal(t, "some message", cr[0].Details)

		assert.Equal(t, "openai", cr[1].Name)
		assert.True(t, cr[1].Spam)
		assert.Equal(t, "bad text, confidence: 100%", cr[1].Details)

		assert.Len(t, mockOpenAIClient.CreateChatCompletionCalls(), 1)
	})

	t.Run("with openai, first-only spam detected before, veto failed", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true, OpenAIVeto: true})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": false, "reason":"good text", "confidence":100}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		_, err := d.LoadStopWords(strings.NewReader("some message"))
		require.NoError(t, err)
		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.False(t, spam)
		require.Len(t, cr, 2)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.True(t, cr[0].Spam)
		assert.Equal(t, "some message", cr[0].Details)

		assert.Equal(t, "openai", cr[1].Name)
		assert.False(t, cr[1].Spam)
		assert.Equal(t, "good text, confidence: 100%", cr[1].Details)

		assert.Len(t, mockOpenAIClient.CreateChatCompletionCalls(), 1)
	})

	t.Run("with openai, first-only spam detected before, openai error", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true, OpenAIVeto: true})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": false, "reason":"good text", "confidence":100}`},
					}},
				}, errors.New("openai error")
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		_, err := d.LoadStopWords(strings.NewReader("some message"))
		require.NoError(t, err)
		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.True(t, spam)
		require.Len(t, cr, 2)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.True(t, cr[0].Spam)
		assert.Equal(t, "some message", cr[0].Details)

		assert.Equal(t, "openai", cr[1].Name)
		assert.False(t, cr[1].Spam)
		assert.Equal(t, "OpenAI error: failed to create chat completion: openai error", cr[1].Details)
		assert.Equal(t, "failed to create chat completion: openai error", cr[1].Error.Error())

		assert.Len(t, mockOpenAIClient.CreateChatCompletionCalls(), 1)
	})

	t.Run("with openai, first-only spam not detected before", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true, OpenAIVeto: false})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		_, err := d.LoadStopWords(strings.NewReader("some message"))
		require.NoError(t, err)

		spam, cr := d.Check(spamcheck.Request{Msg: "1234"})
		assert.True(t, spam)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "not found", cr[0].Details)

		assert.Equal(t, "openai", cr[1].Name)
		assert.True(t, cr[1].Spam)
		assert.Equal(t, "bad text, confidence: 100%", cr[1].Details)

		assert.Len(t, mockOpenAIClient.CreateChatCompletionCalls(), 1)
	})

	t.Run("with openai and MinMsgLen - short message skips openai by default", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true, MinMsgLen: 50})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		// checkShortMessagesWithOpenAI is not set, so it defaults to false (skips checking)
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})

		// test with short message (less than MinMsgLen)
		spam, cr := d.Check(spamcheck.Request{Msg: "short msg"})
		assert.False(t, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "message length", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "too short", cr[0].Details)
		// verify openai was NOT called
		assert.Empty(t, mockOpenAIClient.CreateChatCompletionCalls())

		// test with long message (more than MinMsgLen)
		spam2, cr2 := d.Check(spamcheck.Request{Msg: "this is a much longer message that exceeds the minimum length requirement"})
		assert.True(t, spam2)
		require.Len(t, cr2, 1)
		assert.Equal(t, "openai", cr2[0].Name)
		assert.True(t, cr2[0].Spam)
		assert.Equal(t, "bad text, confidence: 100%", cr2[0].Details)
		// verify openai WAS called for long message
		assert.Len(t, mockOpenAIClient.CreateChatCompletionCalls(), 1)
	})

	t.Run("with openai and MinMsgLen - short message checked when flag is true", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true, MinMsgLen: 50})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		// explicitly set CheckShortMessagesWithOpenAI to true
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4", CheckShortMessagesWithOpenAI: true})

		// test with short message (less than MinMsgLen)
		spam, cr := d.Check(spamcheck.Request{Msg: "short msg"})
		assert.True(t, spam)
		require.Len(t, cr, 2)
		assert.Equal(t, "message length", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "too short", cr[0].Details)
		assert.Equal(t, "openai", cr[1].Name)
		assert.True(t, cr[1].Spam)
		assert.Equal(t, "bad text, confidence: 100%", cr[1].Details)
		// verify openai WAS called even for short message
		assert.Len(t, mockOpenAIClient.CreateChatCompletionCalls(), 1)

		// test with long message (more than MinMsgLen)
		spam2, cr2 := d.Check(spamcheck.Request{Msg: "this is a much longer message that exceeds the minimum length requirement"})
		assert.True(t, spam2)
		require.Len(t, cr2, 1)
		assert.Equal(t, "openai", cr2[0].Name)
		assert.True(t, cr2[0].Spam)
		assert.Equal(t, "bad text, confidence: 100%", cr2[0].Details)
		// verify openai WAS called this time too
		assert.Len(t, mockOpenAIClient.CreateChatCompletionCalls(), 2)
	})

	t.Run("with openai and MinMsgLen - short message already spam skips openai", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true, MinMsgLen: 50})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		// enable checking short messages with openai
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4", CheckShortMessagesWithOpenAI: true})

		// load stop words
		_, err := d.LoadStopWords(strings.NewReader("viagra"))
		require.NoError(t, err)

		// test with short message containing stop word
		spam, cr := d.Check(spamcheck.Request{Msg: "buy viagra"})
		assert.True(t, spam)
		require.Len(t, cr, 2)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.True(t, cr[0].Spam)
		assert.Equal(t, "viagra", cr[0].Details)
		assert.Equal(t, "message length", cr[1].Name)
		assert.False(t, cr[1].Spam)
		assert.Equal(t, "too short", cr[1].Details)
		// verify openai was NOT called because spam was already detected
		assert.Empty(t, mockOpenAIClient.CreateChatCompletionCalls())
	})

	t.Run("with openai enabled for short messages - still skips classifier/similarity", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true, MinMsgLen: 50, SimilarityThreshold: 0.5})

		// load samples to enable classifier/similarity
		spamSamples := strings.NewReader("buy cheap viagra now\nclick here for free money")
		hamSamples := strings.NewReader("hello world\nhow are you")
		lr, err := d.LoadSamples(strings.NewReader(""), []io.Reader{spamSamples}, []io.Reader{hamSamples})
		require.NoError(t, err)
		assert.Positive(t, lr.SpamSamples)
		assert.Positive(t, lr.HamSamples)

		// setup openai mock
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": false, "reason":"too short to tell", "confidence":30}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4", CheckShortMessagesWithOpenAI: true})

		// test short message
		spam, cr := d.Check(spamcheck.Request{Msg: "hi there"})
		assert.False(t, spam)

		// verify we get message length and openai, but NO classifier or similarity
		hasMessageLength := false
		hasOpenAI := false
		for _, r := range cr {
			switch r.Name {
			case "message length":
				hasMessageLength = true
				assert.False(t, r.Spam)
				assert.Equal(t, "too short", r.Details)
			case "openai":
				hasOpenAI = true
				assert.False(t, r.Spam)
			case "classifier":
				t.Error("classifier should not run for short messages even with openai enabled")
			case "similarity":
				t.Error("similarity should not run for short messages even with openai enabled")
			}
		}

		assert.True(t, hasMessageLength, "should have message length check")
		assert.True(t, hasOpenAI, "should have openai check when enabled for short messages")
		assert.Len(t, mockOpenAIClient.CreateChatCompletionCalls(), 1)
	})

	t.Run("short message with CheckShortMessagesWithOpenAI ignores veto mode", func(t *testing.T) {
		// test that when CheckShortMessagesWithOpenAI=true and OpenAIVeto=true,
		// short messages are still checked by OpenAI (veto mode should be ignored for short messages)
		d := NewDetector(Config{
			MaxAllowedEmoji:  -1,
			FirstMessageOnly: true,
			MinMsgLen:        50,
			OpenAIVeto:       true, // veto mode enabled
		})

		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"suspicious short message", "confidence":95}`},
					}},
				}, nil
			},
		}

		// explicitly set CheckShortMessagesWithOpenAI to true
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{
			Model:                        "gpt4",
			CheckShortMessagesWithOpenAI: true,
		})

		// test with short message (less than MinMsgLen)
		spam, cr := d.Check(spamcheck.Request{Msg: "short msg"})

		// should detect as spam because OpenAI says it's spam
		assert.True(t, spam)
		require.Len(t, cr, 2)

		// verify we have message length check
		assert.Equal(t, "message length", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "too short", cr[0].Details)

		// verify we have openai check
		assert.Equal(t, "openai", cr[1].Name)
		assert.True(t, cr[1].Spam)
		assert.Equal(t, "suspicious short message, confidence: 95%", cr[1].Details)

		// verify openai WAS called even with veto mode enabled
		assert.Len(t, mockOpenAIClient.CreateChatCompletionCalls(), 1)
	})

	t.Run("short message with CheckShortMessagesWithOpenAI and OpenAIVeto=false", func(t *testing.T) {
		// test that short messages work correctly with veto mode disabled
		d := NewDetector(Config{
			MaxAllowedEmoji:  -1,
			FirstMessageOnly: true,
			MinMsgLen:        50,
			OpenAIVeto:       false, // veto mode disabled
		})

		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": false, "reason":"looks fine", "confidence":90}`},
					}},
				}, nil
			},
		}

		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{
			Model:                        "gpt4",
			CheckShortMessagesWithOpenAI: true,
		})

		spam, cr := d.Check(spamcheck.Request{Msg: "hi there"})

		// should not detect spam because OpenAI says it's clean
		assert.False(t, spam)
		require.Len(t, cr, 2)

		assert.Equal(t, "message length", cr[0].Name)
		assert.Equal(t, "openai", cr[1].Name)
		assert.False(t, cr[1].Spam)

		// verify openai WAS called
		assert.Len(t, mockOpenAIClient.CreateChatCompletionCalls(), 1)
	})

	t.Run("short message with veto mode - OpenAI returns ham", func(t *testing.T) {
		// test that when OpenAI returns ham for a short message, result is ham
		d := NewDetector(Config{
			MaxAllowedEmoji:  -1,
			FirstMessageOnly: true,
			MinMsgLen:        50,
			OpenAIVeto:       true,
		})

		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": false, "reason":"looks clean", "confidence":85}`},
					}},
				}, nil
			},
		}

		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{
			Model:                        "gpt4",
			CheckShortMessagesWithOpenAI: true,
		})

		spam, cr := d.Check(spamcheck.Request{Msg: "hello"})

		// should not detect spam
		assert.False(t, spam)
		require.Len(t, cr, 2)

		assert.Equal(t, "message length", cr[0].Name)
		assert.Equal(t, "openai", cr[1].Name)
		assert.False(t, cr[1].Spam)
		assert.Equal(t, "looks clean, confidence: 85%", cr[1].Details)

		assert.Len(t, mockOpenAIClient.CreateChatCompletionCalls(), 1)
	})
}

func TestDetector_CheckWithMeta(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1})
	d.WithMetaChecks(LinksCheck(1), ImagesCheck(0), LinkOnlyCheck())

	t.Run("no links, no images", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello, how are you?"})
		assert.False(t, spam)
		require.Len(t, cr, 3)
		assert.Equal(t, "links", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "links 0/1", cr[0].Details)
		assert.Equal(t, "images", cr[1].Name)
		assert.False(t, cr[1].Spam)
		assert.Equal(t, "text or no images", cr[1].Details)
		assert.Equal(t, "link-only", cr[2].Name)
		assert.False(t, cr[2].Spam)
	})

	t.Run("one link, no images", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello, how are you? https://google.com"})
		assert.False(t, spam)
		require.Len(t, cr, 3)
		assert.Equal(t, "links", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "links 1/1", cr[0].Details)
		assert.Equal(t, "images", cr[1].Name)
		assert.False(t, cr[1].Spam)
		assert.Equal(t, "text or no images", cr[1].Details)
		assert.Equal(t, "link-only", cr[2].Name)
		assert.False(t, cr[2].Spam)
	})

	t.Run("one link, no text", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: " https://google.com"})
		assert.True(t, spam)
		t.Logf("%+v", cr)
		require.Len(t, cr, 3)
		assert.Equal(t, "links", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "links 1/1", cr[0].Details)
		assert.Equal(t, "images", cr[1].Name)
		assert.False(t, cr[1].Spam)
		assert.Equal(t, "text or no images", cr[1].Details)
		assert.Equal(t, "link-only", cr[2].Name)
		assert.True(t, cr[2].Spam)
	})

	t.Run("one link, one image with some text", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello, how are you? https://google.com", Meta: spamcheck.MetaData{Images: 1}})
		assert.False(t, spam)
		t.Logf("%+v", cr)
		require.Len(t, cr, 3)
		assert.Equal(t, "links", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "links 1/1", cr[0].Details)
		assert.Equal(t, "images", cr[1].Name)
		assert.False(t, cr[1].Spam)
		assert.Equal(t, "text or no images", cr[1].Details)
		assert.Equal(t, "link-only", cr[2].Name)
		assert.False(t, cr[2].Spam)
		assert.Equal(t, "message contains text", cr[2].Details)
	})

	t.Run("two links, one image with text", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello, how are you? https://google.com https://google.com", Meta: spamcheck.MetaData{Images: 1}})
		assert.True(t, spam)
		t.Logf("%+v", cr)
		require.Len(t, cr, 3)
		assert.Equal(t, "links", cr[0].Name)
		assert.True(t, cr[0].Spam)
		assert.Equal(t, "too many links 2/1", cr[0].Details)
		assert.Equal(t, "images", cr[1].Name)
		assert.False(t, cr[1].Spam)
		assert.Equal(t, "text or no images", cr[1].Details)
		assert.Equal(t, "link-only", cr[2].Name)
		assert.False(t, cr[2].Spam)
		assert.Equal(t, "message contains text", cr[2].Details)
	})

	t.Run("no links, two images, no text", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "", Meta: spamcheck.MetaData{Images: 2}})
		assert.True(t, spam)
		t.Logf("%+v", cr)
		require.Len(t, cr, 3)
		assert.Equal(t, "links", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "links 0/1", cr[0].Details)
		assert.Equal(t, "images", cr[1].Name)
		assert.True(t, cr[1].Spam)
		assert.Equal(t, "image without text", cr[1].Details)
		assert.Equal(t, "link-only", cr[2].Name)
		assert.False(t, cr[2].Spam)
		assert.Equal(t, "empty message", cr[2].Details)
	})
}

func TestDetector_CheckMultiLang(t *testing.T) {
	d := NewDetector(Config{MultiLangWords: 2, MaxAllowedEmoji: -1})
	tests := []struct {
		name  string
		input string
		count int
		spam  bool
	}{
		{"No MultiLang", "Hello, world!\n 12345-980! _", 0, false},
		{"One MultiLang", "Hi therе", 1, false}, //nolint:misspell // intentional Cyrillic 'е' for multilang test
		{"Two MultiLang", "Gооd moфning", 2, true},
		{"WithCyrillic no MultiLang", "Привет  мир", 0, false},
		{"WithCyrillic two MultiLang", "Привеt  мip", 2, true},
		{"WithCyrillic and special symbols", "Привет мир -@#$%^&*(_", 0, false},
		{"WithCyrillic real example 1", "Ищем заинтeрeсoвaнных в зaрaбoткe нa кpиптoвaлютe. Всeгдa хотeли пoпpoбовать сeбя в этом, нo нe знали с чeго нaчaть? Тогдa вaм кo мнe 3aнимаемся aрбuтражeм, зaрабaтывaeм на paзницe курсов с минимaльныmи pискaми 💲Рынok oчень волатильный и нам это выгoднo, пo этoмe пишиte @vitalgoescra и зapaбaтывaйтe сo мнoй ", 31, true},
		{"WithCyrillic real example 2", "В поuске паpтнеров, заuнтересованных в пассuвном дoходе с затpатой мuнuмум лuчного временu. Все деталu в лс", 10, true},
		{"WithCyrillic real example 3", "Всем привет, есть простая шабашка, подойдет любому. Даю 15 тысяч. Накину на проезд, сигареты, обед. ", 0, false},
		{"WithCyrillic and i", "Привет  мiр", 0, false},
		{"strange with cyrillic", "𝐇айди и𝐇𝐓и𝐦𝐇ы𝐞 ф𝐨𝐓𝐤и лю𝐛𝐨й д𝐞𝐁𝐲ш𝐤и ч𝐞𝐩𝐞𝟓 𝐛𝐨𝐓а", 7, true},
		{"coptic capital leter", "✔️ⲠⲢⲞⳜⲈЙ-ⲖЮⳜⲨЮ-ⲆⲈⲂⲨⲰⲔⲨ...\n\nⲎⲀЙⲆⳘ ⲤⲔⲢЫⲦⲈ ⲂⳘⲆⲞⲤЫ-ⲪⲞⲦⲞⳠⲔⳘ ⳘⲎⲦⳘⲘⲎⲞⲄⲞ-ⲬⲀⲢⲀⲔⲦⲈⲢⲀ..\n@INTIM0CHKI110DE\n\n", 5, true},
		{"mix with gothic, cyrillic and greek", "𐌿РОВЕРЬ ЛЮБУЮ НА НАЛИЧИЕ ПОШЛЫХ ΦΟͲΟ-ΒͶДξΟ, 🍑НАБЕРИ В Т𐌲 𐌿ОИСКЕ СЛOВО: 30GRL", 5, true},
		{"Mixed Latin and numbers", "Meta-Llama-3.1-8B-Instruct-IQ4_XS Meta-Llama-3.1-8B-Instruct-Q3_K_L Meta-Llama-3.1-8B-Instruct-Q4_K_M", 0, false},
		{"Mixed Latin, numbers, and Cyrillic", "Meta-Llama-3.1-8B-Instruct-IQ4_XS Meta-Llama-3.1-8B-Instruct-Q3_K_L коллеги, подскажите пожалуйста", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spam, cr := d.Check(spamcheck.Request{Msg: tt.input})
			assert.Equal(t, tt.spam, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "multi-lingual", cr[0].Name)
			assert.Equal(t, tt.spam, cr[0].Spam)
			assert.Equal(t, fmt.Sprintf("%d/2", tt.count), cr[0].Details)
		})
	}

	d.MultiLangWords = 0 // disable multi-lingual check
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spam, _ := d.Check(spamcheck.Request{Msg: tt.input})
			assert.False(t, spam)
		})
	}
}

func TestDetector_CheckWithAbnormalSpacing(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1})
	d.AbnormalSpacing.Enabled = true
	d.AbnormalSpacing.ShortWordLen = 3
	d.AbnormalSpacing.ShortWordRatioThreshold = 0.7
	d.AbnormalSpacing.SpaceRatioThreshold = 0.3
	d.AbnormalSpacing.MinWordsCount = 6

	tests := []struct {
		name     string
		input    string
		expected bool
		details  string
	}{
		{
			name:     "normal text",
			input:    "This is a normal message with regular spacing between words",
			expected: false,
			details:  "normal (ratio: 0.18, short: 20%)",
		},
		{
			name:     "another normal text",
			input:    "For structured content, like code or tables, the ratio can vary significantly based on formatting and indentation",
			expected: false,
			details:  "normal (ratio: 0.17, short: 35%)",
		},
		{
			name: "normal text, cyrillic",
			input: "Если продолжать этот парад благородного безумия, я бы предложил базу имеющихся примеров спама" +
				" сконвертировать в эмбеддинги и проверять через них после байеса, но до gpt4. Эмбеддинги должны быть" +
				" устойчивы к разделенным словам и даже переформулировкам, при этом они гораздо дешевле большой модели.",
			expected: false,
			details:  "normal (ratio: 0.17, short: 26%)",
		},
		{
			name:     "suspicious spacing cyrillic",
			input:    "СРО ЧНО ЭТО КАСА ЕТСЯ КАЖ ДОГО В ЭТ ОЙ ГРУ ППЕ",
			expected: true,
			details:  "abnormal (ratio: 0.31, short: 75%)",
		},
		{
			name:     "very suspicious spacing cyrillic",
			input:    "н а р к о т и к о в, и н в е с т и ц и й",
			expected: true,
			details:  "abnormal (ratio: 0.95, short: 100%)",
		},
		{
			name:     "mixed normal and suspicious",
			input:    "Hello there к а ж д о г о в э т о й г р у п п е",
			expected: true,
			details:  "abnormal (ratio: 0.68, short: 90%)",
		},
		{
			name:     "short spaced text",
			input:    "a b c d",
			expected: false, // too short overall
			details:  "too short",
		},
		{
			name:     "text with some extra spaces",
			input:    "This   is    a    message    with    some    extra    spaces",
			expected: true,
			details:  "abnormal (ratio: 0.82, short: 25%)",
		},
		{
			name: "real spam example",
			input: "СРО ЧНО ЭТО КАСА ЕТСЯ КАЖ ДОГО В ЭТ ОЙ ГРУ ППЕ  Строго 20+ В данный момент про ходит обуч ение " +
				"для нови чков  Сразу говорю - без н а р к о т и к о в, и н в е с т и ц и й и прочей еру нды.  Быстрый старт, " +
				"при быль вы полу чите уже в пе рвый день раб   Все легально  Для раб оты нужен смарт фон и всего 1 час твоего " +
				"врем ени в день  Дове дём вас за ру чку до при были Не бер ём н и к а к и х о п л а т от вас, мы ра60таем " +
				"на % (вы зараба тываете и  только потом делитесь с нами) Кому действ ительно инте ресно " +
				"пишите в и я обязат ельно тебе отвечу",
			expected: true,
			details:  "abnormal (ratio: 0.36, short: 63%)",
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
			details:  "too short",
		},
		{
			name:     "low number of words",
			input:    "с впн всё работает",
			expected: false,
			details:  "too few words (4)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spam, resp := d.Check(spamcheck.Request{Msg: tt.input})
			t.Logf("Response: %+v", resp)
			require.Len(t, resp, 1)
			assert.Equal(t, tt.expected, spam, "spam detection mismatch")
			assert.Equal(t, tt.details, resp[0].Details, "details mismatch")
		})
	}

	t.Run("disabled short word threshold", func(t *testing.T) {
		d.AbnormalSpacing.ShortWordLen = 0
		spam, resp := d.Check(spamcheck.Request{Msg: "СРО ЧНО ЭТО КАС АЕТ СЯ КАЖ ДОГО В ЭТ ОЙ ГРУ ППЕ something else"})
		t.Logf("Response: %+v", resp)
		assert.False(t, spam)
		assert.Equal(t, "normal (ratio: 0.29, short: 0%)", resp[0].Details)
	})

	t.Run("enabled short word threshold", func(t *testing.T) {
		d.AbnormalSpacing.ShortWordLen = 3
		spam, resp := d.Check(spamcheck.Request{Msg: "СРО ЧНО ЭТО КАС АЕТ СЯ КАЖ ДОГО В ЭТ ОЙ ГРУ ППЕ something else"})
		t.Logf("Response: %+v", resp)
		assert.True(t, spam)
		assert.Equal(t, "abnormal (ratio: 0.29, short: 80%)", resp[0].Details)
	})

}

func TestDetector_UpdateSpam(t *testing.T) {
	upd := &mocks.SampleUpdaterMock{
		AppendFunc: func(msg string) error {
			return nil
		},
	}

	d := NewDetector(Config{MaxAllowedEmoji: -1})
	d.WithSpamUpdater(upd)

	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	hamsSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamsSamples})
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 2, HamSamples: 3}, lr)
	d.tokenizedSpam = nil // we don't need tokenizedSpam samples for this test
	assert.Equal(t, 5, d.classifier.nAllDocument)
	exp := map[string]map[spamClass]int{"win": {"spam": 1}, "free": {"spam": 1}, "iphone": {"spam": 1}, "lottery": {"spam": 1},
		"prize": {"spam": 1}, "hello": {"ham": 1}, "world": {"ham": 1}, "how": {"ham": 1}, "are": {"ham": 1}, "you": {"ham": 1},
		"have": {"ham": 1}, "good": {"ham": 1}, "day": {"ham": 1}}
	assert.Equal(t, exp, d.classifier.learningResults)

	msg := "another good world one iphone user writes good things day"
	t.Run("initially a little bit ham", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: msg})
		assert.False(t, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "classifier", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "probability of ham: 59.97%", cr[0].Details)
	})

	err = d.UpdateSpam("another user writes")
	require.NoError(t, err)
	assert.Equal(t, 6, d.classifier.nAllDocument)
	assert.Len(t, upd.AppendCalls(), 1)

	t.Run("after update mostly spam", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: msg})
		assert.True(t, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "classifier", cr[0].Name)
		assert.True(t, cr[0].Spam)
		assert.Equal(t, "probability of spam: 66.67%", cr[0].Details)
	})
}

func TestDetector_UpdateHam(t *testing.T) {
	upd := &mocks.SampleUpdaterMock{
		AppendFunc: func(msg string) error {
			return nil
		},
	}

	d := NewDetector(Config{MaxAllowedEmoji: -1})
	d.WithHamUpdater(upd)

	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	hamsSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamsSamples})
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 2, HamSamples: 3}, lr)
	d.tokenizedSpam = nil // we don't need tokenizedSpam samples for this test
	assert.Equal(t, 5, d.classifier.nAllDocument)
	exp := map[string]map[spamClass]int{"win": {"spam": 1}, "free": {"spam": 1}, "iphone": {"spam": 1}, "lottery": {"spam": 1},
		"prize": {"spam": 1}, "hello": {"ham": 1}, "world": {"ham": 1}, "how": {"ham": 1}, "are": {"ham": 1}, "you": {"ham": 1},
		"have": {"ham": 1}, "good": {"ham": 1}, "day": {"ham": 1}}
	assert.Equal(t, exp, d.classifier.learningResults)

	msg := "another free good world one iphone user writes good things day"
	t.Run("initially a little bit spam", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: msg})
		assert.True(t, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "classifier", cr[0].Name)
		assert.True(t, cr[0].Spam)
		assert.Equal(t, "probability of spam: 60.89%", cr[0].Details)
	})

	err = d.UpdateHam("another writes things")
	require.NoError(t, err)
	assert.Equal(t, 6, d.classifier.nAllDocument)
	assert.Len(t, upd.AppendCalls(), 1)

	t.Run("after update mostly ham", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: msg})
		assert.False(t, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "classifier", cr[0].Name)
		assert.False(t, cr[0].Spam)
		assert.Equal(t, "probability of ham: 72.16%", cr[0].Details)
	})
}

func TestDetector_Reset(t *testing.T) {
	d := NewDetector(Config{})
	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	hamSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamSamples})
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 2, HamSamples: 3}, lr)
	sr, err := d.LoadStopWords(strings.NewReader("в личку\nвсем привет"))
	require.NoError(t, err)
	assert.Equal(t, LoadResult{StopWords: 2}, sr)

	assert.Equal(t, 5, d.classifier.nAllDocument)
	assert.Len(t, d.tokenizedSpam, 2)
	assert.Len(t, d.excludedTokens, 1)
	assert.Len(t, d.stopWords, 2)

	d.Reset()
	assert.Equal(t, 0, d.classifier.nAllDocument)
	assert.Empty(t, d.tokenizedSpam)
	assert.Empty(t, d.excludedTokens)
	assert.Empty(t, d.stopWords)
}

func TestDetector_FirstMessagesCount(t *testing.T) {
	t.Run("first message is spam", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: 1, MinMsgLen: 5, FirstMessagesCount: 2, FirstMessageOnly: true})
		spam, _ := d.Check(spamcheck.Request{Msg: "spam, too many emojis 🤣🤣🤣", UserID: "123"})
		assert.True(t, spam)
	})
	t.Run("first messages are ham, third is spam", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: 1, MinMsgLen: 5, FirstMessagesCount: 3, FirstMessageOnly: true})

		// first ham
		spam, _ := d.Check(spamcheck.Request{Msg: "ham, no emojis", UserID: "123"})
		assert.False(t, spam)

		// second ham
		spam, _ = d.Check(spamcheck.Request{Msg: "ham, no emojis", UserID: "123"})
		assert.False(t, spam)

		spam, _ = d.Check(spamcheck.Request{Msg: "spam, too many emojis 🤣🤣🤣", UserID: "123"})
		assert.True(t, spam)
	})
	t.Run("first messages are ham, spam after approved", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: 1, MinMsgLen: 5, FirstMessagesCount: 2, FirstMessageOnly: true})

		// first ham
		spam, _ := d.Check(spamcheck.Request{Msg: "ham, no emojis", UserID: "123"})
		assert.False(t, spam)

		// second ham
		spam, _ = d.Check(spamcheck.Request{Msg: "ham, no emojis", UserID: "123"})
		assert.False(t, spam)

		spam, _ = d.Check(spamcheck.Request{Msg: "spam, too many emojis 🤣🤣🤣", UserID: "123"})
		assert.False(t, spam, "spam is not detected because user is approved")
	})
	t.Run("first messages are ham, spam after approved, FirstMessageOnly were false", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: 1, MinMsgLen: 5, FirstMessagesCount: 2, FirstMessageOnly: false})

		// first ham
		spam, _ := d.Check(spamcheck.Request{Msg: "ham, no emojis", UserID: "123"})
		assert.False(t, spam)

		// second ham
		spam, _ = d.Check(spamcheck.Request{Msg: "ham, no emojis", UserID: "123"})
		assert.False(t, spam)

		spam, _ = d.Check(spamcheck.Request{Msg: "spam, too many emojis 🤣🤣🤣", UserID: "123"})
		assert.False(t, spam)
	})
}

func TestDetector_ApprovedUsers(t *testing.T) {
	mockUserStore := &mocks.UserStorageMock{
		ReadFunc: func(context.Context) ([]approved.UserInfo, error) {
			return []approved.UserInfo{{UserID: "123"}, {UserID: "456"}}, nil
		},
		WriteFunc:  func(_ context.Context, au approved.UserInfo) error { return nil },
		DeleteFunc: func(_ context.Context, id string) error { return nil },
	}

	t.Run("load with storage", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		count, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
		err = d.AddApprovedUser(approved.UserInfo{UserID: "999", UserName: "test"})
		require.NoError(t, err)
		res := d.ApprovedUsers()
		ids := make([]string, len(res))
		for i, u := range res {
			ids[i] = u.UserID
		}
		sort.Strings(ids)
		assert.Equal(t, []string{"123", "456", "999"}, ids)
		assert.Len(t, mockUserStore.WriteCalls(), 1)
		assert.Equal(t, "999", mockUserStore.WriteCalls()[0].Au.UserID)
		assert.Equal(t, "test", mockUserStore.WriteCalls()[0].Au.UserName)
	})

	t.Run("user not approved, spam detected", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		count, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
		_, err = d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "999"})
		t.Logf("%+v", info)
		assert.True(t, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "stopword", info[0].Name)
		assert.False(t, d.IsApprovedUser("999"))
		assert.Len(t, mockUserStore.ReadCalls(), 1)
		assert.Empty(t, mockUserStore.WriteCalls())
		assert.Empty(t, mockUserStore.DeleteCalls())
	})

	t.Run("user pre-approved, spam check avoided", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		count, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
		_, err = d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "123"})
		t.Logf("%+v", info)
		assert.False(t, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "pre-approved", info[0].Name)
		assert.True(t, d.IsApprovedUser("123"))
	})

	t.Run("user pre-approved with count, spam check avoided", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessagesCount: 10})
		_, err := d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)
		count, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "123"})
		t.Logf("%+v", info)
		assert.False(t, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "pre-approved", info[0].Name)
		assert.True(t, d.IsApprovedUser("123"))
	})

	t.Run("remove user with store", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		_, err := d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)
		count, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		err = d.AddApprovedUser(approved.UserInfo{UserID: "999"})
		require.NoError(t, err)
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "999"})
		t.Logf("%+v", info)
		assert.False(t, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "pre-approved", info[0].Name)
		assert.True(t, d.IsApprovedUser("999"))
		assert.Len(t, mockUserStore.WriteCalls(), 1)
		assert.Equal(t, "999", mockUserStore.WriteCalls()[0].Au.UserID)

		d.RemoveApprovedUser("123")
		isSpam, info = d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "123"})
		t.Logf("%+v", info)
		assert.True(t, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "stopword", info[0].Name)
		assert.False(t, d.IsApprovedUser("123"))
		assert.Len(t, mockUserStore.DeleteCalls(), 1)
		assert.Equal(t, "123", mockUserStore.DeleteCalls()[0].ID)
	})

	t.Run("remove user no store", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		_, err := d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)

		d.AddApprovedUser(approved.UserInfo{UserID: "123"})
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "123"})
		t.Logf("%+v", info)
		assert.False(t, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "pre-approved", info[0].Name)
		assert.True(t, d.IsApprovedUser("123"))

		d.RemoveApprovedUser("123")
		isSpam, info = d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "123"})
		t.Logf("%+v", info)
		assert.True(t, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "stopword", info[0].Name)
		assert.False(t, d.IsApprovedUser("123"))
		assert.Empty(t, mockUserStore.WriteCalls())
		assert.Empty(t, mockUserStore.DeleteCalls())
	})

	t.Run("add user", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		_, err := d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)
		count, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		d.AddApprovedUser(approved.UserInfo{UserID: "777"})
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "777"})
		t.Logf("%+v", info)
		assert.False(t, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "pre-approved", info[0].Name)
		assert.True(t, d.IsApprovedUser("777"))
		assert.Len(t, mockUserStore.WriteCalls(), 1)
		assert.Equal(t, "777", mockUserStore.WriteCalls()[0].Au.UserID)
	})

	t.Run("add user, no store", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		_, err := d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)

		d.AddApprovedUser(approved.UserInfo{UserID: "777"})
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "777"})
		t.Logf("%+v", info)
		assert.False(t, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "pre-approved", info[0].Name)
		assert.True(t, d.IsApprovedUser("777"))
		assert.Empty(t, mockUserStore.WriteCalls())
	})

}

func TestDetector_LoadSamples(t *testing.T) {
	t.Run("basic loading", func(t *testing.T) {
		d := NewDetector(Config{})
		spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz XyZ")
		hamSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
		exclSamples := strings.NewReader("xyz")

		lr, err := d.LoadSamples(exclSamples, []io.Reader{spamSamples}, []io.Reader{hamSamples})

		require.NoError(t, err)
		assert.Equal(t, 1, lr.ExcludedTokens)
		assert.Equal(t, 2, lr.SpamSamples)
		assert.Equal(t, 3, lr.HamSamples)

		// verify excluded tokens
		assert.Contains(t, d.excludedTokens, "xyz")

		// verify tokenized spam samples
		assert.Len(t, d.tokenizedSpam, 2)
		assert.Contains(t, d.tokenizedSpam[0], "win")
		assert.Contains(t, d.tokenizedSpam[1], "lottery")

		// verify classifier learning
		assert.Equal(t, 5, d.classifier.nAllDocument)
		assert.Contains(t, d.classifier.learningResults, "win")
		assert.Contains(t, d.classifier.learningResults["win"], spamClass("spam"))
		assert.Contains(t, d.classifier.learningResults, "world")
		assert.Contains(t, d.classifier.learningResults["world"], spamClass("ham"))

		// verify excluded tokens in learning results
		assert.NotContains(t, d.classifier.learningResults, "xyz", "excluded token should not be in learning results")
		assert.NotContains(t, d.classifier.learningResults, "XyZ", "excluded token should not be in learning results")
	})

	t.Run("empty samples", func(t *testing.T) {
		d := NewDetector(Config{})
		exclSamples := strings.NewReader("")
		spamSamples := strings.NewReader("")
		hamSamples := strings.NewReader("")

		lr, err := d.LoadSamples(exclSamples, []io.Reader{spamSamples}, []io.Reader{hamSamples})

		require.NoError(t, err)
		assert.Equal(t, 0, lr.ExcludedTokens)
		assert.Equal(t, 0, lr.SpamSamples)
		assert.Equal(t, 0, lr.HamSamples)
		assert.Equal(t, 0, d.classifier.nAllDocument)
	})

	t.Run("multiple readers", func(t *testing.T) {
		d := NewDetector(Config{})
		exclSamples := strings.NewReader("xy\n z\n the\n")
		spamSamples1 := strings.NewReader("win free iPhone")
		spamSamples2 := strings.NewReader("lottery prize xyz")
		hamsSamples1 := strings.NewReader("hello world\nhow are you\nhave a good day")
		hamsSamples2 := strings.NewReader("some other text\nwith more words")

		lr, err := d.LoadSamples(
			exclSamples,
			[]io.Reader{spamSamples1, spamSamples2},
			[]io.Reader{hamsSamples1, hamsSamples2},
		)

		require.NoError(t, err)
		assert.Equal(t, 3, lr.ExcludedTokens)

		exTkns := make([]string, 0, len(d.excludedTokens))
		for k := range d.excludedTokens {
			exTkns = append(exTkns, k)
		}
		sort.Strings(exTkns)

		assert.Equal(t, []string{"the", "xy", "z"}, exTkns)
		assert.Equal(t, 2, lr.SpamSamples)
		assert.Equal(t, 5, lr.HamSamples)
		t.Logf("Learning results: %+v", d.classifier.learningResults)
		assert.Equal(t, 7, d.classifier.nAllDocument)
		assert.Contains(t, d.classifier.learningResults["win"], spamClass("spam"))
		assert.Contains(t, d.classifier.learningResults["prize"], spamClass("spam"))
		assert.Contains(t, d.classifier.learningResults["world"], spamClass("ham"))
		assert.Contains(t, d.classifier.learningResults["some"], spamClass("ham"))
	})
}

func TestDetector_tokenize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]int
	}{
		{name: "empty", input: "", expected: map[string]int{}},
		{name: "no filters or cleanups", input: "hello world", expected: map[string]int{"hello": 1, "world": 1}},
		{name: "with excluded tokens", input: "hello world the she", expected: map[string]int{"hello": 1, "world": 1}},
		{name: "with short tokens", input: "hello world the she a or", expected: map[string]int{"hello": 1, "world": 1}},
		{name: "with repeated tokens", input: "hello world hello world", expected: map[string]int{"hello": 2, "world": 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Detector{excludedTokens: map[string]struct{}{"the": {}, "she": {}}}
			assert.Equal(t, tt.expected, d.tokenize(tt.input))
		})
	}
}

func TestDetector_readerIterator(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{name: "empty", input: "", expected: []string{}},
		{name: "token per line", input: "hello\nworld", expected: []string{"hello", "world"}},
		{name: "token per line", input: "hello 123\nworld", expected: []string{"hello 123", "world"}},
		{name: "token per line with spaces", input: "hello \n world", expected: []string{"hello", "world"}},
		{name: "multiple tokens per line with", input: " hello blah\n the new world ",
			expected: []string{"hello blah", "the new world"}},
		{name: "with extra EOL", input: " hello blah\n the new world \n  ", expected: []string{"hello blah", "the new world"}},
		{name: "with empty lines", input: " hello blah\n\n  \n the new world \n  \n", expected: []string{"hello blah", "the new world"}},
	}

	d := Detector{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := d.readerIterator(bytes.NewBufferString(tt.input))
			res := make([]string, 0) //nolint:prealloc // iterator size unknown
			for token := range ch {
				res = append(res, token)
			}
			assert.Equal(t, tt.expected, res)
		})
	}
}

func TestDetector_readerIteratorMultipleReaders(t *testing.T) {
	d := Detector{}
	ch := d.readerIterator(bytes.NewBufferString("hello\nworld"), bytes.NewBufferString("something, new"))
	res := make([]string, 0) //nolint:prealloc // iterator size unknown
	for token := range ch {
		res = append(res, token)
	}
	sort.Strings(res)
	assert.Equal(t, []string{"hello", "something, new", "world"}, res)
}

func TestCleanText(t *testing.T) {
	d := Detector{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "English text with word joiners",
			input:    "D\u2062ude i\u2062t i\u2062s t\u2062he la\u2062st da\u2062y",
			expected: "Dude it is the last day",
		},
		{
			name:     "Russian text with word joiners",
			input:    "Р\u2062ебята д\u2062авайте б\u2062ыстрее",
			expected: "Ребята давайте быстрее",
		},
		{
			name:     "Text with pop directional formatting",
			input:    "F\u2068ast t\u2068ake i\u2068t",
			expected: "Fast take it",
		},
		{
			name:     "Mixed invisible characters",
			input:    "Hello\u200BWorld\u2062Test\u206FCase",
			expected: "HelloWorldTestCase",
		},
		{
			name:     "No invisible characters",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "Only invisible characters",
			input:    "\u200B\u2062\u206F",
			expected: "",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "URLs with invisible characters",
			input:    "https://\u2062example\u2062.com/\u2062test",
			expected: "https://example.com/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// test regex-based implementation
			result := d.cleanText(tt.input)
			assert.Equal(t, tt.expected, result, "failed for case: %s", tt.name)
		})
	}
}

func Test_countEmoji(t *testing.T) {
	tests := []struct {
		name  string
		input string
		count int
	}{
		{"NoEmoji", "Hello, world!", 0},
		{"OneEmoji", "Hi there 👋", 1},
		{"DupEmoji", "️‍🌈Hi 👋there 👋", 3},
		{"TwoEmojis", "Good morning 🌞🌻", 2},
		{"Mixed", "👨‍👩👦 Family emoji", 3},
		{"TextAfterEmoji", "😊 Have a nice day!", 1},
		{"OnlyEmojis", "😁🐶🍕", 3},
		{"WithCyrillic", "Привет 🌞 🍕 мир! 👋", 3},
		{"real1", "❗️НУЖЕН 1 ЧЕЛОВЕК НА ДИСТАНЦИОННУЮ РАБОТУ❗️", 2},
		{"real2", "⏰💯⚡️💯🤝🤝🤝🤝🤝🤝🤝🤝  ❗️HУЖHЫ OТВЕТCТВЕHHЫЕ ЛЮДИ❗️              🔤🔤  ➡️@yyyyy🥢" +
			"  ⚡️(OТ 2️⃣1️⃣ ВOЗРАCТ)🟢 🔋OHЛАЙH ЗАРАБOТOК 🟢 ✅COПРOВOЖДЕHИЕ🟢 ❗1-2 ЧАCА В ДЕHЬ 🟢   👍1️⃣2️⃣0️⃣0️⃣💸" +
			"➕в неделю🟢 ПИCАТЬ ✉️@xxxxxx✉️", 38},
		{"real3", "‼️СРОЧНО‼️  ‼️ЭТО КАСАЕТСЯ КАЖДОГО В ЭТОЙ ГРУППЕ‼️  🔥Строго 20+  В данный момент проходит обучение " +
			"для новичков 🔥 Сразу говорю - без наркотиков, инвестиций и прочей ерунды. 🔥 Быстрый старт, прибыль вы получите" +
			" уже в первый день работы 🔥 Все легально 🔥 Для работы нужен смартфон и всего 1 час твоего времени" +
			" в день 🔥 Доведём вас за ручку до прибыли ‼️", 11},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.count, countEmoji(tt.input))
		})
	}
}

func Test_cleanEmoji(t *testing.T) {
	tests := []struct {
		name  string
		input string
		clean string
	}{
		{"NoEmoji", "Hello, world!", "Hello, world!"},
		{"OneEmoji", "Hi there 👋", "Hi there "},
		{"TwoEmojis", "Good morning 🌞🌻", "Good morning "},
		{"Mixed", "👨‍👩‍👧‍👦 Family emoji", " Family emoji"},
		{"EmojiSequences", "🏳️‍🌈 Rainbow flag", " Rainbow flag"},
		{"TextAfterEmoji", "😊 Have a nice day!", " Have a nice day!"},
		{"OnlyEmojis", "😁🐶🍕", ""},
		{"WithCyrillic", "Привет 🌞 🍕 мир! 👋", "Привет   мир! "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.clean, cleanEmoji(tt.input))
		})
	}
}

func TestDetector_RemoveHam(t *testing.T) {
	// setup a test detector with mock updater
	updMock := &mocks.SampleUpdaterMock{
		RemoveFunc: func(msg string) error {
			return nil
		},
		AppendFunc: func(msg string) error {
			return nil
		},
	}

	d := NewDetector(Config{})
	d.WithHamUpdater(updMock)

	// setup the classifier with ham samples
	hamSamples := strings.NewReader("test message\nhello world")
	lr, err := d.LoadSamples(strings.NewReader(""), nil, []io.Reader{hamSamples})
	require.NoError(t, err)
	require.Equal(t, 2, lr.HamSamples)

	// test removing a ham sample that exists
	require.NoError(t, d.RemoveHam("test message"))
	assert.Len(t, updMock.RemoveCalls(), 1)
	assert.Equal(t, "test message", updMock.RemoveCalls()[0].Msg)

	// test error handling
	updMock.RemoveFunc = func(msg string) error {
		return errors.New("remove error")
	}
	err = d.RemoveHam("hello world")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remove error")
}

func TestDetector_RemoveSpamHam(t *testing.T) {
	updSpam := &mocks.SampleUpdaterMock{
		RemoveFunc: func(msg string) error {
			return nil
		},
		AppendFunc: func(msg string) error {
			return nil
		},
	}

	d := NewDetector(Config{MaxAllowedEmoji: -1})
	d.WithSpamUpdater(updSpam)

	// load initial samples WITHOUT the test message
	spamSamples := strings.NewReader("lottery prize xyz")
	hamsSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamsSamples})
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 1, HamSamples: 3}, lr)

	// add our test message separately
	spamMsg := "win free iPhone"
	err = d.UpdateSpam(spamMsg)
	require.NoError(t, err)

	t.Run("initially classified as spam", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "win free iPhone hello world"})
		t.Logf("%+v", cr)
		require.True(t, spam, "should initially be classified as spam")
		require.NotEmpty(t, cr, "should have classification results")
		assert.Equal(t, "classifier", cr[0].Name)
		assert.True(t, cr[0].Spam)
	})

	err = d.RemoveSpam(spamMsg)
	require.NoError(t, err)
	assert.Len(t, updSpam.RemoveCalls(), 1)
	assert.Equal(t, spamMsg, updSpam.RemoveCalls()[0].Msg)

	t.Run("after removing spam", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "win free iPhone hello world"})
		t.Logf("%+v", cr)
		require.NotEmpty(t, cr, "should have classification results")
		assert.Equal(t, "classifier", cr[0].Name)
		assert.False(t, spam, "should no longer be classified as spam")
		assert.False(t, cr[0].Spam)
	})

	t.Run("error on updater", func(t *testing.T) {
		failingUpd := &mocks.SampleUpdaterMock{
			RemoveFunc: func(msg string) error {
				return fmt.Errorf("remove error")
			},
		}
		d.WithSpamUpdater(failingUpd)
		err := d.RemoveSpam(spamMsg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "can't unlearn spam samples")
	})

	t.Run("error on non-existent message", func(t *testing.T) {
		err := d.RemoveSpam("not-learned-message")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "can't unlearn spam samples")
	})
}

func TestDetector_buildDocs(t *testing.T) {
	d := &Detector{excludedTokens: map[string]struct{}{"the": {}, "and": {}}}

	docs := d.buildDocs("buy crypto coins now", "spam")
	assert.Len(t, docs, 1, "should create single document")
	assert.Equal(t, spamClass("spam"), docs[0].spamClass)
	assert.ElementsMatch(t, []string{"buy", "crypto", "coins", "now"}, docs[0].tokens)
}

func TestDetector_CheckHistory(t *testing.T) {
	d := NewDetector(Config{HistorySize: 5})
	_, err := d.LoadStopWords(strings.NewReader("spam text\nbad text"))
	require.NoError(t, err)

	// first message is ham
	isSpam, _ := d.Check(spamcheck.Request{Msg: "good message", UserID: "1"})
	assert.False(t, isSpam)
	hamMsgs := d.hamHistory.Last(5)
	require.Len(t, hamMsgs, 1)
	assert.Equal(t, "good message", hamMsgs[0].Msg)
	assert.Empty(t, d.spamHistory.Last(5))

	// second message is spam
	isSpam, _ = d.Check(spamcheck.Request{Msg: "spam text", UserID: "1"})
	assert.True(t, isSpam)
	spamMsgs := d.spamHistory.Last(5)
	require.Len(t, spamMsgs, 1)
	assert.Equal(t, "spam text", spamMsgs[0].Msg)
	assert.Len(t, d.hamHistory.Last(5), 1, "ham history should remain unchanged")

	// third message is ham
	isSpam, _ = d.Check(spamcheck.Request{Msg: "another good one", UserID: "2"})
	assert.False(t, isSpam)
	hamMsgs = d.hamHistory.Last(5)
	require.Len(t, hamMsgs, 2)
	assert.Equal(t, "good message", hamMsgs[0].Msg)
	assert.Equal(t, "another good one", hamMsgs[1].Msg)
	assert.Len(t, d.spamHistory.Last(5), 1, "spam history should remain unchanged")
}

func TestDetector_CheckHistory_ShortMessagesNotAddedToHam(t *testing.T) {
	t.Run("short ham message without openai should not be added to history", func(t *testing.T) {
		d := NewDetector(Config{HistorySize: 5, MinMsgLen: 10})
		// no openai checker configured, so short messages won't be properly validated

		// send a short message that isn't spam
		isSpam, cr := d.Check(spamcheck.Request{Msg: "hi", UserID: "1"})
		assert.False(t, isSpam)
		assert.Contains(t, spamcheck.ChecksToString(cr), "message length")

		// short unvalidated message should NOT be in hamHistory
		hamMsgs := d.hamHistory.Last(5)
		assert.Empty(t, hamMsgs, "short unchecked messages should not be added to hamHistory")
	})

	t.Run("short ham message with openai enabled should be added to history", func(t *testing.T) {
		d := NewDetector(Config{HistorySize: 5, MinMsgLen: 10, FirstMessageOnly: true})
		mockClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{Content: `{"spam":false,"reason":"good","confidence":30}`}}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockClient, OpenAIConfig{CheckShortMessagesWithOpenAI: true})

		// send a short message that's checked by openai
		isSpam, _ := d.Check(spamcheck.Request{Msg: "hi", UserID: "1", UserName: "user1"})
		assert.False(t, isSpam)

		// properly validated short message should be in hamHistory
		hamMsgs := d.hamHistory.Last(5)
		assert.Len(t, hamMsgs, 1, "short messages checked by openai should be added to hamHistory")
		assert.Equal(t, "hi", hamMsgs[0].Msg)
	})

	t.Run("normal length ham message should be added to history", func(t *testing.T) {
		d := NewDetector(Config{HistorySize: 5, MinMsgLen: 10})

		// send a normal length message
		isSpam, _ := d.Check(spamcheck.Request{Msg: "this is a normal length message", UserID: "1"})
		assert.False(t, isSpam)

		// normal messages should be in hamHistory
		hamMsgs := d.hamHistory.Last(5)
		assert.Len(t, hamMsgs, 1, "normal length messages should be added to hamHistory")
		assert.Equal(t, "this is a normal length message", hamMsgs[0].Msg)
	})
}

func BenchmarkTokenize(b *testing.B) {
	d := &Detector{
		excludedTokens: map[string]struct{}{"the": {}, "and": {}, "or": {}, "but": {}, "in": {}, "on": {}, "at": {}, "to": {}},
	}

	tests := []struct {
		name string
		text string
	}{
		{
			name: "Short_NoExcluded",
			text: "hello world test message",
		},
		{
			name: "Short_WithExcluded",
			text: "the quick brown fox and the lazy dog",
		},
		{
			name: "Medium_Mixed",
			text: strings.Repeat("hello world and test message with some excluded tokens ", 10),
		},
		{
			name: "Long_MixedWithPunct",
			text: strings.Repeat("hello, world! test? message. with!! some... excluded tokens!!! ", 50),
		},
		{
			name: "WithEmoji",
			text: "hello 👋 world 🌍 test 🧪 message 📝 with emoji 😊",
		},
		{
			name: "RealWorldSample",
			text: "🔥 EXCLUSIVE OFFER! Don't miss out on this amazing deal. Buy now and get 50% OFF! Limited time offer. Click here: http://example.com #deal #shopping #discount",
		},
	}

	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = d.tokenize(tc.text)
			}
		})
	}
}

func BenchmarkLoadSamples(b *testing.B) {
	makeReader := func(lines []string) io.Reader {
		return strings.NewReader(strings.Join(lines, "\n"))
	}

	tests := []struct {
		name     string
		spam     []string
		ham      []string
		excluded []string
	}{
		{
			name:     "Small",
			spam:     []string{"spam message 1", "buy now spam 2", "spam offer 3"},
			ham:      []string{"hello world", "normal message", "how are you"},
			excluded: []string{"the", "and", "or"},
		},
		{
			name:     "Medium",
			spam:     []string{"spam message 1", "buy now spam 2", "spam offer 3", "urgent offer", "free money"},
			ham:      []string{"hello world", "normal message", "how are you", "meeting tomorrow", "project update"},
			excluded: []string{"the", "and", "or", "but", "in", "on", "at"},
		},
		{
			name: "Large_RealWorld",
			// use actual spam samples from your data
			spam: []string{
				"Здравствуйте   Мы занимаемая новым видом заработка в интернете   Наша сфера даст вам опыт, знания",
				"У кого нет карты карты Тинькофф? Можете оформить по моей ссылке и получите 500р от меня",
				"😀😀😀 Для тeх ктo ищeт дoпoлнительный доход предлагаю перспективный и прибыльный зaрaботok",
			},
			ham: []string{
				"When is our next meeting?",
				"Here's the project update you requested",
				"Thanks for the feedback, I'll review it",
			},
			excluded: []string{"the", "and", "or", "but", "in", "on", "at", "to", "for", "with"},
		},
	}

	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			d := NewDetector(Config{})

			b.ResetTimer()
			b.ReportAllocs()
			for range b.N {
				// need to create new readers for each iteration
				spamReader := makeReader(tc.spam)
				hamReader := makeReader(tc.ham)
				exclReader := makeReader(tc.excluded)

				_, err := d.LoadSamples(exclReader, []io.Reader{spamReader}, []io.Reader{hamReader})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestDetector_ClassifierNaNHandling(t *testing.T) {
	d := NewDetector(Config{})

	t.Run("empty classifier no NaN", func(t *testing.T) {
		// test classifier with no training data - should not produce NaN
		spam, cr := d.Check(spamcheck.Request{Msg: "test message with no training data"})
		assert.False(t, spam)
		// no classifier check should be performed since classifier is empty
		for _, r := range cr {
			if r.Name == "classifier" {
				t.Error("classifier check should not be performed with empty training data")
			}
		}
	})

	t.Run("minimal training data no NaN", func(t *testing.T) {
		// load minimal training data
		spamReader := strings.NewReader("spam")
		hamReader := strings.NewReader("ham")
		exclReader := strings.NewReader("")
		_, err := d.LoadSamples(exclReader, []io.Reader{spamReader}, []io.Reader{hamReader})
		require.NoError(t, err)

		// check with unknown tokens - should not produce NaN
		spam, cr := d.Check(spamcheck.Request{Msg: "completely unknown tokens that were never seen"})
		assert.False(t, spam)

		// find classifier result
		var classifierResult *spamcheck.Response
		for i := range cr {
			if cr[i].Name == "classifier" {
				classifierResult = &cr[i]
				break
			}
		}

		require.NotNil(t, classifierResult, "classifier result should be present")
		assert.NotContains(t, classifierResult.Details, "NaN", "probability should not be NaN")

		// verify the details format is correct
		assert.Regexp(t, `probability of (spam|ham): \d+\.\d+%`, classifierResult.Details)
	})

	t.Run("edge case tokens no NaN", func(t *testing.T) {
		// test with very unusual input that might trigger edge cases
		testCases := []string{
			"",    // empty string
			"   ", // only spaces
			"a",   // single char
			"aa",  // two chars
			"😀😀😀😀😀😀😀😀😀😀", // only emojis
			"\n\n\n\n",                 // only newlines
			strings.Repeat("a", 10000), // very long single token
		}

		for _, tc := range testCases {
			spam, cr := d.Check(spamcheck.Request{Msg: tc})
			// just verify no panic and no NaN in results
			for _, r := range cr {
				if r.Name == "classifier" {
					assert.NotContains(t, r.Details, "NaN", "probability should not be NaN for input: %q", tc)
				}
			}
			_ = spam // result doesn't matter, just checking for NaN
		}
	})
}

func TestDetector_ShortMessageApproval(t *testing.T) {
	t.Run("short messages don't count towards approval", func(t *testing.T) {
		d := NewDetector(Config{MinMsgLen: 10, FirstMessagesCount: 3, FirstMessageOnly: true, MaxAllowedEmoji: -1})

		// send 3 short messages (less than MinMsgLen)
		for range 3 {
			spam, cr := d.Check(spamcheck.Request{Msg: "hi", UserID: "123"})
			assert.False(t, spam)
			assert.NotEmpty(t, cr)
			// find the message length check in results
			found := false
			for _, r := range cr {
				if r.Name == "message length" {
					assert.Equal(t, "too short", r.Details)
					found = true
					break
				}
			}
			assert.True(t, found, "message length check not found")
		}

		// user should NOT be approved after 3 short messages
		assert.False(t, d.IsApprovedUser("123"))

		// now send a spam message with stopword
		_, err := d.LoadStopWords(strings.NewReader("spam"))
		require.NoError(t, err)
		spam, cr := d.Check(spamcheck.Request{Msg: "spam", UserID: "123"})
		// even though message is short, stopword check runs and detects spam
		assert.True(t, spam)
		found := false
		for _, r := range cr {
			if r.Name == "stopword" && r.Spam {
				found = true
				break
			}
		}
		assert.True(t, found, "stopword check should detect spam")

		// send a normal length spam message - should be detected
		spam, cr = d.Check(spamcheck.Request{Msg: "this is a spam message", UserID: "123"})
		assert.True(t, spam)
		// verify stopword check triggered
		found = false
		for _, r := range cr {
			if r.Name == "stopword" && r.Spam {
				found = true
				break
			}
		}
		assert.True(t, found, "stopword check not found")
	})

	t.Run("normal messages count towards approval", func(t *testing.T) {
		d := NewDetector(Config{MinMsgLen: 10, FirstMessagesCount: 3, FirstMessageOnly: true, MaxAllowedEmoji: -1})

		// send 2 normal messages
		for range 2 {
			spam, _ := d.Check(spamcheck.Request{Msg: "this is a normal message", UserID: "456"})
			assert.False(t, spam)
		}

		// user should NOT be approved yet (need 3 messages)
		assert.False(t, d.IsApprovedUser("456"))

		// send 3rd normal message
		spam, _ := d.Check(spamcheck.Request{Msg: "another normal message here", UserID: "456"})
		assert.False(t, spam)

		// user should NOT be approved yet (need count > FirstMessagesCount)
		assert.False(t, d.IsApprovedUser("456"))

		// after 3 messages, user is pre-approved (count >= FirstMessagesCount)
		// but IsApprovedUser returns false because it checks count > FirstMessagesCount
		// this is the existing behavior, not related to our fix

		// 4th message will be pre-approved
		spam, cr := d.Check(spamcheck.Request{Msg: "fourth normal message", UserID: "456"})
		assert.False(t, spam)
		assert.Equal(t, "pre-approved", cr[0].Name)

		// count stays at 3 because pre-approved messages don't increment
		d.lock.RLock()
		actualCount := d.approvedUsers["456"].Count
		d.lock.RUnlock()
		assert.Equal(t, 3, actualCount)

		// IsApprovedUser still returns false (3 > 3 is false)
		assert.False(t, d.IsApprovedUser("456"))
	})

	t.Run("mix of short and normal messages", func(t *testing.T) {
		d := NewDetector(Config{MinMsgLen: 10, FirstMessagesCount: 3, FirstMessageOnly: true, MaxAllowedEmoji: -1})

		// send alternating short and normal messages
		// short message 1
		spam, _ := d.Check(spamcheck.Request{Msg: "hi", UserID: "789"})
		assert.False(t, spam)
		assert.False(t, d.IsApprovedUser("789"))

		// normal message 1
		spam, _ = d.Check(spamcheck.Request{Msg: "this is a normal message", UserID: "789"})
		assert.False(t, spam)
		assert.False(t, d.IsApprovedUser("789"))

		// short message 2
		spam, _ = d.Check(spamcheck.Request{Msg: "ok", UserID: "789"})
		assert.False(t, spam)
		assert.False(t, d.IsApprovedUser("789"))

		// normal message 2
		spam, _ = d.Check(spamcheck.Request{Msg: "another normal message here", UserID: "789"})
		assert.False(t, spam)
		assert.False(t, d.IsApprovedUser("789"))

		// short message 3
		spam, _ = d.Check(spamcheck.Request{Msg: "yes", UserID: "789"})
		assert.False(t, spam)
		assert.False(t, d.IsApprovedUser("789"))

		// normal message 3
		spam, _ = d.Check(spamcheck.Request{Msg: "third normal message finally", UserID: "789"})
		assert.False(t, spam)

		// with the mix of short and normal messages, only the normal ones count
		// after 3 normal messages, count is 3, user is pre-approved but IsApprovedUser
		// returns false because it checks count > FirstMessagesCount
		assert.False(t, d.IsApprovedUser("789"))
	})

	t.Run("short messages with storage", func(t *testing.T) {
		mockUserStore := &mocks.UserStorageMock{
			ReadFunc: func(context.Context) ([]approved.UserInfo, error) {
				return []approved.UserInfo{}, nil
			},
			WriteFunc:  func(_ context.Context, au approved.UserInfo) error { return nil },
			DeleteFunc: func(_ context.Context, id string) error { return nil },
		}

		d := NewDetector(Config{MinMsgLen: 10, FirstMessagesCount: 2, FirstMessageOnly: true, MaxAllowedEmoji: -1})
		_, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)

		// send 2 short messages
		d.Check(spamcheck.Request{Msg: "hi", UserID: "111"})
		d.Check(spamcheck.Request{Msg: "ok", UserID: "111"})

		// storage should NOT be called for short messages
		assert.Empty(t, mockUserStore.WriteCalls())

		// send 2 normal messages
		d.Check(spamcheck.Request{Msg: "normal message one", UserID: "111"})
		assert.Len(t, mockUserStore.WriteCalls(), 1)

		d.Check(spamcheck.Request{Msg: "normal message two", UserID: "111"})
		assert.Len(t, mockUserStore.WriteCalls(), 2)

		// user is not approved yet (need > 2)
		assert.False(t, d.IsApprovedUser("111"))

		// after 2 messages, count is 2 which equals FirstMessagesCount
		// so the 3rd message will be pre-approved and won't update storage
		d.Check(spamcheck.Request{Msg: "normal message three", UserID: "111"})
		// storage write is NOT called for pre-approved message
		assert.Len(t, mockUserStore.WriteCalls(), 2)
		// IsApprovedUser returns false because count (2) is not > FirstMessagesCount (2)
		assert.False(t, d.IsApprovedUser("111"))
	})
}

// helper function to find response by name
func findResponseByName(responses []spamcheck.Response, name string) *spamcheck.Response {
	for _, r := range responses {
		if r.Name == name {
			return &r
		}
	}
	return nil
}
