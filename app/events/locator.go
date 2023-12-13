package events

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"time"
)

// Locator stores messages for a given time period.
// It is used to locate the message in the chat by its hash.
// Useful to match messages from admin chat (only text available) to the original message.
// Note: it is not thread-safe, use it from a single goroutine only.
type Locator struct {
	ttl             time.Duration      // how long to keep messages
	data            map[string]MsgMeta // message hash -> message meta
	lastRemoval     time.Time          // last time cleanup was performed
	cleanupDuration time.Duration      // how often to perform cleanup
	minSize         int                // minimum number of messages to keep
}

// MsgMeta stores message metadata
type MsgMeta struct {
	time   time.Time
	chatID int64
	userID int64
	msgID  int
}

func (m MsgMeta) String() string {
	return fmt.Sprintf("{chatID: %d, userID: %d, msgID: %d, time: %s}", m.chatID, m.userID, m.msgID, m.time.Format(time.RFC3339))
}

// NewLocator creates new Locator. ttl defines how long to keep messages, minSize defines the minimum number of messages to keep
func NewLocator(ttl time.Duration, minSize int) *Locator {
	return &Locator{
		ttl:             ttl,
		data:            make(map[string]MsgMeta),
		lastRemoval:     time.Now(),
		minSize:         minSize,
		cleanupDuration: 5 * time.Minute,
	}
}

// Get returns message MsgMeta for given msg
// this allows to match messages from admin chat (only text available) to the original message
func (l *Locator) Get(msg string) (MsgMeta, bool) {
	hash := l.MsgHash(msg)
	res, ok := l.data[hash]
	return res, ok
}

// MsgHash returns sha256 hash of a message
func (l *Locator) MsgHash(msg string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(msg)))
}

// Add adds messages to the locator and removes old messages.
// Messages are removed the total number of messages exceeds minSize and the last cleanup was performed more than cleanupDuration ago.
// The reason for minSize is to avoid removing messages on low-traffic chats where admin visits are rare.
// Note: removes old messages only once per cleanupDuration and only if a new message is added
func (l *Locator) Add(msg string, chatID, userID int64, msgID int) {
	l.data[l.MsgHash(msg)] = MsgMeta{
		time:   time.Now(),
		chatID: chatID,
		userID: userID,
		msgID:  msgID,
	}

	if time.Since(l.lastRemoval) < l.cleanupDuration || len(l.data) <= l.minSize {
		return
	}

	// make sorted list of keys
	keys := make([]string, 0, len(l.data))
	for k := range l.data {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return l.data[keys[i]].time.Before(l.data[keys[j]].time) })

	// remove old messages, but keep at least minSize messages
	for k, v := range l.data {
		if time.Since(v.time) > l.ttl {
			delete(l.data, k)
		}
		if len(l.data) <= l.minSize {
			break
		}
	}
	l.lastRemoval = time.Now()
}
