package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite" // sqlite driver loaded here

	"github.com/umputun/tg-spam/lib/spamcheck"
)

// Locator stores messages metadata and spam results for a given ttl period.
// It is used to locate the message in the chat by its hash and to retrieve spam check results by userID.
// Useful to match messages from admin chat (only text available) to the original message and to get spam results using UserID.
type Locator struct {
	ttl     time.Duration
	minSize int
	db      *Engine
	RWLocker
}

// MsgMeta stores message metadata
type MsgMeta struct {
	Time     time.Time `db:"time"`
	ChatID   int64     `db:"chat_id"`
	UserID   int64     `db:"user_id"`
	UserName string    `db:"user_name"`
	MsgID    int       `db:"msg_id"`
}

// SpamData stores spam data for a given user
type SpamData struct {
	Time   time.Time `db:"time"`
	Checks []spamcheck.Response
}

// NewLocator creates new Locator. ttl defines how long to keep messages in db, minSize defines the minimum number of messages to keep
func NewLocator(ctx context.Context, ttl time.Duration, minSize int, db *Engine) (*Locator, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}

	schema := `
		CREATE TABLE IF NOT EXISTS messages (
			hash TEXT PRIMARY KEY,
			time TIMESTAMP,
			chat_id INTEGER,
			user_id INTEGER,
			user_name TEXT,
			msg_id INTEGER
		);
		CREATE TABLE IF NOT EXISTS spam (
			user_id INTEGER PRIMARY KEY,
			time TIMESTAMP,
			checks TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_messages_user_id ON messages(user_id);
		CREATE INDEX IF NOT EXISTS idx_messages_user_name ON messages(user_name);
		CREATE INDEX IF NOT EXISTS idx_spam_time ON spam(time);
	`

	// create schema in a single transaction
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, schema); err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return &Locator{ttl: ttl, minSize: minSize, db: db, RWLocker: db.MakeLock()}, nil
}

// Close closes the database
func (l *Locator) Close(_ context.Context) error {
	return l.db.Close()
}

// AddMessage adds messages to the locator and also cleans up old messages.
func (l *Locator) AddMessage(ctx context.Context, msg string, chatID, userID int64, userName string, msgID int) error {
	l.Lock()
	defer l.Unlock()

	hash := l.MsgHash(msg)
	log.Printf("[DEBUG] add message to locator: %q, hash:%s, userID:%d, user name:%q, chatID:%d, msgID:%d",
		msg, hash, userID, userName, chatID, msgID)
	_, err := l.db.NamedExecContext(ctx, `INSERT OR REPLACE INTO messages (hash, time, chat_id, user_id, user_name, msg_id) 
        VALUES (:hash, :time, :chat_id, :user_id, :user_name, :msg_id)`,
		struct {
			MsgMeta
			Hash string `db:"hash"`
		}{
			MsgMeta: MsgMeta{
				Time:     time.Now(),
				ChatID:   chatID,
				UserID:   userID,
				UserName: userName,
				MsgID:    msgID,
			},
			Hash: hash,
		})
	if err != nil {
		return fmt.Errorf("failed to insert message: %w", err)
	}
	return l.cleanupMessages(ctx)
}

// AddSpam adds spam data to the locator and also cleans up old spam data.
func (l *Locator) AddSpam(ctx context.Context, userID int64, checks []spamcheck.Response) error {
	l.Lock()
	defer l.Unlock()

	checksStr, err := json.Marshal(checks)
	if err != nil {
		return fmt.Errorf("failed to marshal checks: %w", err)
	}
	_, err = l.db.NamedExecContext(ctx, `INSERT OR REPLACE INTO spam (user_id, time, checks) 
        VALUES (:user_id, :time, :checks)`,
		map[string]interface{}{
			"user_id": userID,
			"time":    time.Now(),
			"checks":  string(checksStr),
		})
	if err != nil {
		return fmt.Errorf("failed to insert spam: %w", err)
	}
	return l.cleanupSpam()
}

// Message returns message MsgMeta for given msg
// this allows to match messages from admin chat (only text available) to the original message
func (l *Locator) Message(ctx context.Context, msg string) (MsgMeta, bool) {
	l.RLock()
	defer l.RUnlock()

	var meta MsgMeta
	hash := l.MsgHash(msg)
	err := l.db.GetContext(ctx, &meta, `SELECT time, chat_id, user_id, user_name, msg_id FROM messages WHERE hash = ?`, hash)
	if err != nil {
		log.Printf("[DEBUG] failed to find message by hash %q: %v", hash, err)
		return MsgMeta{}, false
	}
	return meta, true
}

// UserNameByID returns username by user id. Returns empty string if not found
func (l *Locator) UserNameByID(ctx context.Context, userID int64) string {
	l.RLock()
	defer l.RUnlock()

	var userName string
	err := l.db.GetContext(ctx, &userName, `SELECT user_name FROM messages WHERE user_id = ? LIMIT 1`, userID)
	if err != nil {
		log.Printf("[DEBUG] failed to find user name by id %d: %v", userID, err)
		return ""
	}
	return userName
}

// UserIDByName returns user id by username. Returns 0 if not found
func (l *Locator) UserIDByName(ctx context.Context, userName string) int64 {
	l.RLock()
	defer l.RUnlock()

	var userID int64
	err := l.db.GetContext(ctx, &userID, `SELECT user_id FROM messages WHERE user_name = ? LIMIT 1`, userName)
	if err != nil {
		log.Printf("[DEBUG] failed to find user id by name %q: %v", userName, err)
		return 0
	}
	return userID
}

// Spam returns message SpamData for given msg
func (l *Locator) Spam(ctx context.Context, userID int64) (SpamData, bool) {
	l.RLock()
	defer l.RUnlock()

	var data SpamData
	var checksStr string
	err := l.db.QueryRowContext(ctx, `SELECT time, checks FROM spam WHERE user_id = ?`, userID).Scan(&data.Time, &checksStr)
	if err != nil {
		return SpamData{}, false
	}
	if err := json.Unmarshal([]byte(checksStr), &data.Checks); err != nil {
		return SpamData{}, false
	}

	return data, true
}

// MsgHash returns sha256 hash of a message
// we use hash to avoid storing potentially long messages and all we need is just match
func (l *Locator) MsgHash(msg string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(msg)))
}

// cleanupMessages removes old messages. Messages with expired ttl are removed if the total number of messages exceeds minSize.
// The reason for minSize is to avoid removing messages on low-traffic chats where admin visits are rare.
func (l *Locator) cleanupMessages(ctx context.Context) error {
	_, err := l.db.ExecContext(ctx, `DELETE FROM messages WHERE time < ? AND (SELECT COUNT(*) FROM messages) > ?`,
		time.Now().Add(-l.ttl), l.minSize)
	if err != nil {
		return fmt.Errorf("failed to cleanup messages: %w", err)
	}
	return nil
}

// cleanupSpam removes old spam data
func (l *Locator) cleanupSpam() error {
	_, err := l.db.Exec(`DELETE FROM spam WHERE time < ? AND (SELECT COUNT(*) FROM spam) > ?`,
		time.Now().Add(-l.ttl), l.minSize)
	if err != nil {
		return fmt.Errorf("failed to cleanup spam: %w", err)
	}
	return nil
}

func (m MsgMeta) String() string {
	return fmt.Sprintf("{chatID: %d, user name: %s, userID: %d, msgID: %d, time: %s}",
		m.ChatID, m.UserName, m.UserID, m.MsgID, m.Time.Format(time.RFC3339))
}

func (s SpamData) String() string {
	return fmt.Sprintf("{time: %s, checks: %+v}", s.Time.Format(time.RFC3339), s.Checks)
}
