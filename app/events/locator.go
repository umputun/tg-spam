package events

import (
	"crypto/sha256"
	"fmt"
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

// NewLocator creates new Locator
func NewLocator(ttl time.Duration) *Locator {
	return &Locator{
		ttl:             ttl,
		data:            make(map[string]MsgMeta),
		lastRemoval:     time.Now(),
		cleanupDuration: 5 * time.Minute,
	}
}

// Get returns message MsgMeta for give msg
func (l *Locator) Get(msg string) (MsgMeta, bool) {
	hash := l.MsgHash(msg)
	res, ok := l.data[hash]
	return res, ok
}

// MsgHash returns sha256 hash of a message
func (l *Locator) MsgHash(msg string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(msg)))
}

// Add adds message to the locator
func (l *Locator) Add(msg string, chatID, userID int64, msgID int) {
	l.data[l.MsgHash(msg)] = MsgMeta{
		time:   time.Now(),
		chatID: chatID,
		userID: userID,
		msgID:  msgID,
	}

	if time.Since(l.lastRemoval) < l.cleanupDuration {
		return
	}

	// remove old messages
	for k, v := range l.data {
		if time.Since(v.time) > l.ttl {
			delete(l.data, k)
		}
	}
	l.lastRemoval = time.Now()
}
