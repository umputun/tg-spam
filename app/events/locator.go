package events

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"time"

	"github.com/umputun/tg-spam/lib"
)

// Locator stores messages for a given time period.
// It is used to locate the message in the chat by its hash and to locate spam info.
// Useful to match messages from admin chat (only text available) to the original message and to get spam info using userID.
// Note: it is not thread-safe, use it from a single goroutine only.
type Locator struct {
	ttl  time.Duration // how long to keep messages
	msgs struct {
		data        map[string]MsgMeta // message hash -> message meta
		lastRemoval time.Time          // last time cleanup was performed
	}
	spam struct {
		data        map[int64]SpamData // message hash -> number of occurrences
		lastRemoval time.Time          // last time cleanup was performed
	}
	cleanupDuration time.Duration // how often to perform cleanup
	minSize         int           // minimum number of messages to keep
}

// MsgMeta stores message metadata
type MsgMeta struct {
	time   time.Time
	chatID int64
	userID int64
	msgID  int
}

// SpamData stores spam data for a given user
type SpamData struct {
	time   time.Time
	checks []lib.CheckResult
}

func (m MsgMeta) String() string {
	return fmt.Sprintf("{chatID: %d, userID: %d, msgID: %d, time: %s}", m.chatID, m.userID, m.msgID, m.time.Format(time.RFC3339))
}

func (s SpamData) String() string {
	return fmt.Sprintf("{time: %s, checks: %+v}", s.time.Format(time.RFC3339), s.checks)
}

// NewLocator creates new Locator. ttl defines how long to keep messages, minSize defines the minimum number of messages to keep
func NewLocator(ttl time.Duration, minSize int) *Locator {
	res := &Locator{
		ttl:             ttl,
		minSize:         minSize,
		cleanupDuration: 5 * time.Minute,
	}
	res.msgs.data = make(map[string]MsgMeta)
	res.msgs.lastRemoval = time.Now()
	res.spam.data = make(map[int64]SpamData)
	res.spam.lastRemoval = time.Now()
	return res
}

// Message returns message MsgMeta for given msg
// this allows to match messages from admin chat (only text available) to the original message
func (l *Locator) Message(msg string) (MsgMeta, bool) {
	hash := l.MsgHash(msg)
	res, ok := l.msgs.data[hash]
	return res, ok
}

// MsgHash returns sha256 hash of a message
func (l *Locator) MsgHash(msg string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(msg)))
}

// AddMessage adds messages to the locator and removes old messages.
// Messages are removed the total number of messages exceeds minSize and the last cleanup was performed more than cleanupDuration ago.
// The reason for minSize is to avoid removing messages on low-traffic chats where admin visits are rare.
// Note: removes old messages only once per cleanupDuration and only if a new message is added
func (l *Locator) AddMessage(msg string, chatID, userID int64, msgID int) {
	l.msgs.data[l.MsgHash(msg)] = MsgMeta{
		time:   time.Now(),
		chatID: chatID,
		userID: userID,
		msgID:  msgID,
	}

	if time.Since(l.msgs.lastRemoval) < l.cleanupDuration || len(l.msgs.data) <= l.minSize {
		return
	}

	// make sorted list of keys
	keys := make([]string, 0, len(l.msgs.data))
	for k := range l.msgs.data {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return l.msgs.data[keys[i]].time.Before(l.msgs.data[keys[j]].time) })

	// remove old messages, but keep at least minSize messages
	for k, v := range l.msgs.data {
		if time.Since(v.time) > l.ttl {
			delete(l.msgs.data, k)
		}
		if len(l.msgs.data) <= l.minSize {
			break
		}
	}
	l.msgs.lastRemoval = time.Now()
}

// Spam returns message SpamData for given msg
func (l *Locator) Spam(userID int64) (SpamData, bool) {
	res, ok := l.spam.data[userID]
	return res, ok
}

// AddSpam adds spam data for a given user
func (l *Locator) AddSpam(userID int64, checks []lib.CheckResult) {
	l.spam.data[userID] = SpamData{
		time:   time.Now(),
		checks: checks,
	}

	// make sorted list of keys
	keys := make([]int64, 0, len(l.spam.data))
	for k := range l.spam.data {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return l.spam.data[keys[i]].time.Before(l.spam.data[keys[j]].time) })

	// remove old messages, but keep at least minSize messages
	for k, v := range l.spam.data {
		if time.Since(v.time) > l.ttl {
			delete(l.spam.data, k)
		}
		if len(l.spam.data) <= l.minSize {
			break
		}
	}
	l.spam.lastRemoval = time.Now()
}
