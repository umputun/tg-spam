package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
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

var locatorSchema = `
    CREATE TABLE IF NOT EXISTS messages (
        hash TEXT PRIMARY KEY,
        gid TEXT NOT NULL DEFAULT '',
        time TIMESTAMP,
        chat_id INTEGER,
        user_id INTEGER,
        user_name TEXT,
        msg_id INTEGER
    );
    CREATE TABLE IF NOT EXISTS spam (
        user_id INTEGER PRIMARY KEY,
        gid TEXT NOT NULL DEFAULT '',
        time TIMESTAMP,
        checks TEXT
    );
    CREATE INDEX IF NOT EXISTS idx_messages_user_id ON messages(user_id);
    CREATE INDEX IF NOT EXISTS idx_messages_user_name ON messages(user_name);
    CREATE INDEX IF NOT EXISTS idx_spam_time ON spam(time);
    CREATE INDEX IF NOT EXISTS idx_messages_gid ON messages(gid);
    CREATE INDEX IF NOT EXISTS idx_spam_gid ON spam(gid);
`

// NewLocator creates new Locator. ttl defines how long to keep messages in db, minSize defines the minimum number of messages to keep
func NewLocator(ctx context.Context, ttl time.Duration, minSize int, db *Engine) (*Locator, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}

	// first check if tables exist, we can't do this in a transaction because ALTER TABLE will fail
	var exists int
	err := db.GetContext(ctx, &exists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='messages'")
	if err != nil {
		return nil, fmt.Errorf("failed to check for messages table existence: %w", err)
	}

	if exists == 0 { // tables do not exist, create them
		tx, err := db.Begin()
		if err != nil {
			return nil, fmt.Errorf("failed to start transaction: %w", err)
		}
		defer tx.Rollback()

		if _, err = tx.ExecContext(ctx, locatorSchema); err != nil {
			return nil, fmt.Errorf("failed to create schema: %w", err)
		}

		if err = tx.Commit(); err != nil {
			return nil, fmt.Errorf("failed to commit transaction: %w", err)
		}
	}

	// migrate tables
	if err := migrateLocator(&db.DB, db.GID()); err != nil {
		return nil, fmt.Errorf("failed to migrate locator: %w", err)
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
	_, err := l.db.NamedExecContext(ctx, `INSERT OR REPLACE INTO messages (hash, gid, time, chat_id, user_id, user_name, msg_id) 
        VALUES (:hash, :gid, :time, :chat_id, :user_id, :user_name, :msg_id)`,
		struct {
			MsgMeta
			Hash string `db:"hash"`
			GID  string `db:"gid"`
		}{
			MsgMeta: MsgMeta{
				Time:     time.Now(),
				ChatID:   chatID,
				UserID:   userID,
				UserName: userName,
				MsgID:    msgID,
			},
			Hash: hash,
			GID:  l.db.GID(),
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
	_, err = l.db.NamedExecContext(ctx, `INSERT OR REPLACE INTO spam (user_id, gid, time, checks) 
        VALUES (:user_id, :gid, :time, :checks)`,
		map[string]interface{}{
			"user_id": userID,
			"gid":     l.db.GID(),
			"time":    time.Now(),
			"checks":  string(checksStr),
		})
	if err != nil {
		return fmt.Errorf("failed to insert spam: %w", err)
	}
	return l.cleanupSpam()
}

// Message returns message MsgMeta for given msg and gid
func (l *Locator) Message(ctx context.Context, msg string) (MsgMeta, bool) {
	l.RLock()
	defer l.RUnlock()

	var meta MsgMeta
	hash := l.MsgHash(msg)
	err := l.db.GetContext(ctx, &meta, `SELECT time, chat_id, user_id, user_name, msg_id 
        FROM messages WHERE hash = ? AND gid = ?`, hash, l.db.GID())
	if err != nil {
		log.Printf("[DEBUG] failed to find message by hash %q: %v", hash, err)
		return MsgMeta{}, false
	}
	return meta, true
}

// UserNameByID returns username by user id within the same gid
func (l *Locator) UserNameByID(ctx context.Context, userID int64) string {
	l.RLock()
	defer l.RUnlock()

	var userName string
	err := l.db.GetContext(ctx, &userName, `SELECT user_name FROM messages WHERE user_id = ? AND gid = ? LIMIT 1`, userID, l.db.GID())
	if err != nil {
		log.Printf("[DEBUG] failed to find user name by id %d: %v", userID, err)
		return ""
	}
	return userName
}

// UserIDByName returns user id by username within the same gid
func (l *Locator) UserIDByName(ctx context.Context, userName string) int64 {
	l.RLock()
	defer l.RUnlock()

	var userID int64
	err := l.db.GetContext(ctx, &userID, `SELECT user_id FROM messages WHERE user_name = ? AND gid = ? LIMIT 1`, userName, l.db.GID())
	if err != nil {
		log.Printf("[DEBUG] failed to find user id by name %q: %v", userName, err)
		return 0
	}
	return userID
}

// Spam returns message SpamData for given msg within the same gid
func (l *Locator) Spam(ctx context.Context, userID int64) (SpamData, bool) {
	l.RLock()
	defer l.RUnlock()

	var data SpamData
	var checksStr string
	err := l.db.QueryRowContext(ctx, `SELECT time, checks FROM spam WHERE user_id = ? AND gid = ?`,
		userID, l.db.GID()).Scan(&data.Time, &checksStr)
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
func (l *Locator) cleanupMessages(ctx context.Context) error {
	_, err := l.db.ExecContext(ctx, `DELETE FROM messages WHERE time < ? AND gid = ? AND (SELECT COUNT(*) FROM messages WHERE gid = ?) > ?`,
		time.Now().Add(-l.ttl), l.db.GID(), l.db.GID(), l.minSize)
	if err != nil {
		return fmt.Errorf("failed to cleanup messages: %w", err)
	}
	return nil
}

// cleanupSpam removes old spam data within the same gid
func (l *Locator) cleanupSpam() error {
	_, err := l.db.Exec(`DELETE FROM spam WHERE time < ? AND gid = ? AND (SELECT COUNT(*) FROM spam WHERE gid = ?) > ?`,
		time.Now().Add(-l.ttl), l.db.GID(), l.db.GID(), l.minSize)
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

// migration function
func migrateLocator(db *sqlx.DB, gid string) error {
	// add gid column to messages if it doesn't exist
	if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN gid TEXT DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("failed to alter messages table: %w", err)
		}
	}

	// add gid column to spam if it doesn't exist
	if _, err := db.Exec(`ALTER TABLE spam ADD COLUMN gid TEXT DEFAULT ''`); err != nil {
		if !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("failed to alter spam table: %w", err)
		}
	}

	// update existing records with the provided gid
	res1, err := db.Exec("UPDATE messages SET gid = ? WHERE gid = ''", gid)
	if err != nil {
		return fmt.Errorf("failed to update gid for existing messages: %w", err)
	}
	messagesAffected, err := res1.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get messages affected rows: %w", err)
	}

	res2, err := db.Exec("UPDATE spam SET gid = ? WHERE gid = ''", gid)
	if err != nil {
		return fmt.Errorf("failed to update gid for existing spam: %w", err)
	}
	spamAffected, err := res2.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get spam affected rows: %w", err)
	}

	log.Printf("[DEBUG] locator tables migrated, gid updated to %q, messages: %d, spam: %d",
		gid, messagesAffected, spamAffected)
	return nil
}
