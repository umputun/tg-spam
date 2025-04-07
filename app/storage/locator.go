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

	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

// locator-related command constants
const (
	CmdCreateLocatorTables engine.DBCmd = iota + 400
	CmdCreateLocatorIndexes
	CmdAddGIDColumnMessages
	CmdAddGIDColumnSpam
	CmdAddLocatorMessage
	CmdAddLocatorSpam
)

// locatorQueries holds all locator-related queries
var locatorQueries = engine.NewQueryMap().
	Add(CmdCreateLocatorTables, engine.Query{
		Sqlite: `CREATE TABLE IF NOT EXISTS messages (
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
        )`,
		Postgres: `CREATE TABLE IF NOT EXISTS messages (
            hash TEXT PRIMARY KEY,
            gid TEXT NOT NULL DEFAULT '',
            time TIMESTAMP,
            chat_id BIGINT,
            user_id BIGINT,
            user_name TEXT,
            msg_id INTEGER
        );
        CREATE TABLE IF NOT EXISTS spam (
            user_id BIGINT PRIMARY KEY,
            gid TEXT NOT NULL DEFAULT '',
            time TIMESTAMP,
            checks TEXT
        )`,
	}).
	Add(CmdCreateLocatorIndexes, engine.Query{
		Sqlite: `
			CREATE INDEX IF NOT EXISTS idx_messages_user_id ON messages(user_id);
			CREATE INDEX IF NOT EXISTS idx_messages_user_name ON messages(user_name);
			CREATE INDEX IF NOT EXISTS idx_spam_time ON spam(time);
			CREATE INDEX IF NOT EXISTS idx_messages_gid ON messages(gid);
			CREATE INDEX IF NOT EXISTS idx_spam_gid ON spam(gid) `,
		Postgres: `
			CREATE INDEX IF NOT EXISTS idx_messages_gid_user_id ON messages(gid, user_id);
			CREATE INDEX IF NOT EXISTS idx_messages_gid_user_name ON messages(gid, user_name);
			CREATE INDEX IF NOT EXISTS idx_spam_gid_time ON spam(gid, time DESC);
			CREATE INDEX IF NOT EXISTS idx_spam_user_id_gid ON spam(user_id, gid)`,
	}).
	Add(CmdAddGIDColumnMessages, engine.Query{
		Sqlite:   "ALTER TABLE messages ADD COLUMN gid TEXT DEFAULT ''",
		Postgres: "ALTER TABLE messages ADD COLUMN IF NOT EXISTS gid TEXT DEFAULT ''",
	}).
	Add(CmdAddGIDColumnSpam, engine.Query{
		Sqlite:   "ALTER TABLE spam ADD COLUMN gid TEXT DEFAULT ''",
		Postgres: "ALTER TABLE spam ADD COLUMN IF NOT EXISTS gid TEXT DEFAULT ''",
	}).
	Add(CmdAddLocatorMessage, engine.Query{
		Sqlite: `INSERT OR REPLACE INTO messages (hash, gid, time, chat_id, user_id, user_name, msg_id) 
            VALUES (:hash, :gid, :time, :chat_id, :user_id, :user_name, :msg_id)`,
		Postgres: `INSERT INTO messages (hash, gid, time, chat_id, user_id, user_name, msg_id) 
            VALUES (:hash, :gid, :time, :chat_id, :user_id, :user_name, :msg_id)
            ON CONFLICT (hash) DO UPDATE SET 
            gid = :gid, 
            time = :time, 
            chat_id = :chat_id, 
            user_id = :user_id, 
            user_name = :user_name, 
            msg_id = :msg_id`,
	}).
	Add(CmdAddLocatorSpam, engine.Query{
		Sqlite: `INSERT OR REPLACE INTO spam (user_id, gid, time, checks) 
            VALUES (:user_id, :gid, :time, :checks)`,
		Postgres: `INSERT INTO spam (user_id, gid, time, checks) 
            VALUES (:user_id, :gid, :time, :checks)
            ON CONFLICT (user_id) DO UPDATE SET 
            gid = :gid, 
            time = :time, 
            checks = :checks`,
	})

// Locator stores messages metadata and spam results for a given ttl period.
// It is used to locate the message in the chat by its hash and to retrieve spam check results by userID.
// Useful to match messages from admin chat (only text available) to the original message and to get spam results using UserID.
type Locator struct {
	*engine.SQL
	ttl     time.Duration
	minSize int
	engine.RWLocker
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
func NewLocator(ctx context.Context, ttl time.Duration, minSize int, db *engine.SQL) (*Locator, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}
	res := &Locator{ttl: ttl, minSize: minSize, SQL: db, RWLocker: db.MakeLock()}
	cfg := engine.TableConfig{
		Name:          "messages_spam",
		CreateTable:   CmdCreateLocatorTables,
		CreateIndexes: CmdCreateLocatorIndexes,
		MigrateFunc:   res.migrate,
		QueriesMap:    locatorQueries,
	}
	if err := engine.InitTable(ctx, db, cfg); err != nil {
		return nil, fmt.Errorf("failed to init locator storage: %w", err)
	}
	return res, nil
}

func (l *Locator) migrate(ctx context.Context, tx *sqlx.Tx, gid string) error {
	// try to select with new structure, if works - already migrated
	var count int
	err := tx.GetContext(ctx, &count, "SELECT COUNT(*) FROM messages WHERE gid = ''")
	if err == nil {
		log.Printf("[DEBUG] locator tables already migrated")
		return nil
	}

	// add gid column to messages
	addGIDMessagesQuery, err := locatorQueries.Pick(l.Type(), CmdAddGIDColumnMessages)
	if err != nil {
		return fmt.Errorf("failed to get add messages GID query: %w", err)
	}

	_, err = tx.ExecContext(ctx, addGIDMessagesQuery)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("failed to add gid column to messages: %w", err)
	}

	// add gid column to spam
	addGIDSpamQuery, err := locatorQueries.Pick(l.Type(), CmdAddGIDColumnSpam)
	if err != nil {
		return fmt.Errorf("failed to get add spam GID query: %w", err)
	}

	_, err = tx.ExecContext(ctx, addGIDSpamQuery)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("failed to add gid column to spam: %w", err)
	}

	// update existing records with provided gid
	query := l.Adopt("UPDATE messages SET gid = ? WHERE gid = ''")
	if _, err = tx.ExecContext(ctx, query, gid); err != nil {
		return fmt.Errorf("failed to update gid for existing messages: %w", err)
	}

	if _, err = tx.ExecContext(ctx, "UPDATE spam SET gid = ? WHERE gid = ''", gid); err != nil {
		return fmt.Errorf("failed to update gid for existing spam: %w", err)
	}

	log.Printf("[DEBUG] locator tables migrated")
	return nil
}

// Close closes the database
func (l *Locator) Close(_ context.Context) error {
	if err := l.SQL.Close(); err != nil {
		return fmt.Errorf("failed to close SQL connection: %w", err)
	}
	return nil
}

// AddMessage adds messages to the locator and also cleans up old messages.
func (l *Locator) AddMessage(ctx context.Context, msg string, chatID, userID int64, userName string, msgID int) error {
	l.Lock()
	defer l.Unlock()

	hash := l.MsgHash(msg)
	log.Printf("[DEBUG] add message to locator: %q, hash:%s, userID:%d, user name:%q, chatID:%d, msgID:%d",
		msg, hash, userID, userName, chatID, msgID)

	query, err := locatorQueries.Pick(l.Type(), CmdAddLocatorMessage)
	if err != nil {
		return fmt.Errorf("failed to get add message query: %w", err)
	}

	_, err = l.NamedExecContext(ctx, query,
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
			GID:  l.GID(),
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

	query, err := locatorQueries.Pick(l.Type(), CmdAddLocatorSpam)
	if err != nil {
		return fmt.Errorf("failed to get add spam query: %w", err)
	}

	_, err = l.NamedExecContext(ctx, query,
		map[string]interface{}{
			"user_id": userID,
			"gid":     l.GID(),
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
	query := l.Adopt(`SELECT time, chat_id, user_id, user_name, msg_id FROM messages WHERE hash = ? AND gid = ?`)
	err := l.GetContext(ctx, &meta, query, hash, l.GID())
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
	query := l.Adopt(`SELECT user_name FROM messages WHERE user_id = ? AND gid = ? LIMIT 1`)
	err := l.GetContext(ctx, &userName, query, userID, l.GID())
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
	query := l.Adopt(`SELECT user_id FROM messages WHERE user_name = ? AND gid = ? LIMIT 1`)
	err := l.GetContext(ctx, &userID, query, userName, l.GID())
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
	query := l.Adopt(`SELECT time, checks FROM spam WHERE user_id = ? AND gid = ?`)
	err := l.QueryRowContext(ctx, query, userID, l.GID()).Scan(&data.Time, &checksStr)
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
	query := l.Adopt(`DELETE FROM messages WHERE time < ? AND gid = ? AND (SELECT COUNT(*) FROM messages WHERE gid = ?) > ?`)
	_, err := l.ExecContext(ctx, query, time.Now().Add(-l.ttl), l.GID(), l.GID(), l.minSize)
	if err != nil {
		return fmt.Errorf("failed to cleanup messages: %w", err)
	}
	return nil
}

// cleanupSpam removes old spam data within the same gid
func (l *Locator) cleanupSpam() error {
	query := l.Adopt(`DELETE FROM spam WHERE time < ? AND gid = ? AND (SELECT COUNT(*) FROM spam WHERE gid = ?) > ?`)
	_, err := l.Exec(query, time.Now().Add(-l.ttl), l.GID(), l.GID(), l.minSize)
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
