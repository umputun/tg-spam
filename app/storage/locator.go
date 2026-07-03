package storage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
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
            hash TEXT NOT NULL,
            gid TEXT NOT NULL DEFAULT '',
            time TIMESTAMP,
            chat_id INTEGER,
            user_id INTEGER,
            user_name TEXT,
            msg_id INTEGER,
            PRIMARY KEY (gid, hash)
        );
        CREATE TABLE IF NOT EXISTS spam (
            user_id INTEGER NOT NULL,
            gid TEXT NOT NULL DEFAULT '',
            time TIMESTAMP,
            checks TEXT,
            PRIMARY KEY (gid, user_id)
        )`,
		Postgres: `CREATE TABLE IF NOT EXISTS messages (
            hash TEXT NOT NULL,
            gid TEXT NOT NULL DEFAULT '',
            time TIMESTAMP,
            chat_id BIGINT,
            user_id BIGINT,
            user_name TEXT,
            msg_id INTEGER,
            PRIMARY KEY (gid, hash)
        );
        CREATE TABLE IF NOT EXISTS spam (
            user_id BIGINT NOT NULL,
            gid TEXT NOT NULL DEFAULT '',
            time TIMESTAMP,
            checks TEXT,
            PRIMARY KEY (gid, user_id)
        )`,
	}).
	Add(CmdCreateLocatorIndexes, engine.Query{
		Sqlite: `
			CREATE INDEX IF NOT EXISTS idx_messages_user_id ON messages(user_id);
			CREATE INDEX IF NOT EXISTS idx_messages_user_name ON messages(user_name);
			CREATE INDEX IF NOT EXISTS idx_spam_time ON spam(time);
			CREATE INDEX IF NOT EXISTS idx_messages_gid ON messages(gid);
			CREATE INDEX IF NOT EXISTS idx_messages_gid_user_id_time ON messages(gid, user_id, time DESC);
			CREATE INDEX IF NOT EXISTS idx_spam_gid ON spam(gid) `,
		Postgres: `
			CREATE INDEX IF NOT EXISTS idx_messages_gid_user_id ON messages(gid, user_id);
			CREATE INDEX IF NOT EXISTS idx_messages_gid_user_id_time ON messages(gid, user_id, time DESC);
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
            ON CONFLICT (gid, hash) DO UPDATE SET
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
            ON CONFLICT (gid, user_id) DO UPDATE SET
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

// NewLocator creates new Locator.
// ttl defines how long to keep messages in db, minSize defines minimum messages to keep
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
	if err := l.migrateGIDColumns(ctx, tx, gid); err != nil {
		return err
	}
	// legacy tables used single-column primary keys (messages.hash, spam.user_id) which break
	// cross-group isolation when multiple instances share one database: an upsert from one gid
	// clobbers another gid's row. upgrade them to composite (gid, ...) keys.
	if err := l.migratePrimaryKeys(ctx, tx); err != nil {
		return fmt.Errorf("failed to upgrade locator primary keys: %w", err)
	}
	// free the idx_spam_gid_time name before InitTable creates the locator indexes, in case an
	// older detected_spam schema squatted it (postgres index names are schema-global).
	if err := l.dropMisplacedSpamIndex(ctx, tx); err != nil {
		return fmt.Errorf("failed to free misplaced spam index: %w", err)
	}
	return nil
}

// dropMisplacedSpamIndex drops idx_spam_gid_time when an older schema created it on a table other
// than spam (the detected_spam duplicate). postgres index names are schema-global and NewLocator
// runs before NewDetectedSpam, so leaving the squatted name would make the locator's own
// CREATE INDEX IF NOT EXISTS idx_spam_gid_time ON spam a silent no-op for a whole startup. sqlite
// names the locator's spam index differently (idx_spam_gid), so there is no collision there.
func (l *Locator) dropMisplacedSpamIndex(ctx context.Context, tx *sqlx.Tx) error {
	if l.Type() != engine.Postgres {
		return nil
	}
	var misplaced int
	query := `SELECT COUNT(*) FROM pg_indexes
		WHERE indexname = 'idx_spam_gid_time' AND schemaname = current_schema() AND tablename <> 'spam'`
	if err := tx.GetContext(ctx, &misplaced, query); err != nil {
		return fmt.Errorf("failed to look up idx_spam_gid_time: %w", err)
	}
	if misplaced == 0 {
		return nil // name is free or already owned by the spam table
	}
	// IF EXISTS so a concurrent upgrader that already freed the name turns this into a no-op
	if _, err := tx.ExecContext(ctx, "DROP INDEX IF EXISTS idx_spam_gid_time"); err != nil {
		return fmt.Errorf("failed to drop misplaced idx_spam_gid_time: %w", err)
	}
	log.Printf("[INFO] dropped misplaced idx_spam_gid_time to free the name for the locator spam index")
	return nil
}

// migrateGIDColumns adds the gid column to messages and spam tables and backfills it with the provided gid.
// each table is checked independently so a partially migrated schema (one table with gid, the other without)
// completes before the primary key migration relies on the column.
func (l *Locator) migrateGIDColumns(ctx context.Context, tx *sqlx.Tx, gid string) error {
	tables := []struct {
		name   string
		addCmd engine.DBCmd
	}{
		{name: "messages", addCmd: CmdAddGIDColumnMessages},
		{name: "spam", addCmd: CmdAddGIDColumnSpam},
	}
	for _, table := range tables {
		hasGID, err := l.hasGIDColumn(ctx, tx, table.name)
		if err != nil {
			return fmt.Errorf("failed to check gid column on %s: %w", table.name, err)
		}
		if hasGID {
			continue
		}

		addQuery, err := locatorQueries.Pick(l.Type(), table.addCmd)
		if err != nil {
			return fmt.Errorf("failed to get add GID query for %s: %w", table.name, err)
		}
		if _, err := tx.ExecContext(ctx, addQuery); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return fmt.Errorf("failed to add gid column to %s: %w", table.name, err)
		}

		// update existing records with provided gid, table name is a hardcoded constant
		query := l.Adopt(fmt.Sprintf("UPDATE %s SET gid = ? WHERE gid = ''", table.name)) //nolint:gosec // static table name
		if _, err := tx.ExecContext(ctx, query, gid); err != nil {
			return fmt.Errorf("failed to update gid for existing %s: %w", table.name, err)
		}
		log.Printf("[DEBUG] gid column added to %s and backfilled", table.name)
	}
	return nil
}

// hasGIDColumn reports whether the given table already has the gid column, using catalog
// metadata instead of a probing query so a failed probe can't poison the transaction
func (l *Locator) hasGIDColumn(ctx context.Context, tx *sqlx.Tx, table string) (bool, error) {
	var count int
	switch l.Type() {
	case engine.Sqlite:
		if err := tx.GetContext(ctx, &count, "SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = 'gid'", table); err != nil {
			return false, fmt.Errorf("failed to query table info: %w", err)
		}
	case engine.Postgres:
		query := `SELECT COUNT(*) FROM information_schema.columns
			WHERE table_name = $1 AND column_name = 'gid' AND table_schema = current_schema()`
		if err := tx.GetContext(ctx, &count, query, table); err != nil {
			return false, fmt.Errorf("failed to query information_schema: %w", err)
		}
	default:
		return false, fmt.Errorf("unsupported database type %q", l.Type())
	}
	return count > 0, nil
}

// migratePrimaryKeys upgrades legacy single-column primary keys to composite (gid, ...) keys.
// safe to run repeatedly: it detects the number of primary key columns and skips already-upgraded tables.
// existing data can't violate the new keys: uniqueness on the old single column implies uniqueness on the pair.
func (l *Locator) migratePrimaryKeys(ctx context.Context, tx *sqlx.Tx) error {
	tables := []struct{ name, pkCols string }{
		{name: "messages", pkCols: "gid, hash"},
		{name: "spam", pkCols: "gid, user_id"},
	}
	for _, table := range tables {
		var err error
		switch l.Type() {
		case engine.Sqlite:
			err = l.migratePrimaryKeySqlite(ctx, tx, table.name, table.pkCols)
		case engine.Postgres:
			err = l.migratePrimaryKeyPostgres(ctx, tx, table.name, table.pkCols)
		default:
			err = fmt.Errorf("unsupported database type %q", l.Type())
		}
		if err != nil {
			return fmt.Errorf("table %s: %w", table.name, err)
		}
	}
	return nil
}

// migratePrimaryKeySqlite rebuilds the table with a composite primary key, sqlite can't alter primary keys in place.
// indexes dropped with the old table are recreated by InitTable right after migration.
func (l *Locator) migratePrimaryKeySqlite(ctx context.Context, tx *sqlx.Tx, table, pkCols string) error {
	pk, err := l.primaryKeyColumns(ctx, tx, table)
	if err != nil {
		return err
	}
	if pk == pkCols {
		return nil // already the expected composite key
	}

	// keep newSchemas and copyColumns in sync with the canonical CmdCreateLocatorTables schema:
	// this rebuild path is permanent, and a column added only to the canonical schema would be
	// silently dropped from a legacy database during the rebuild copy below.
	newSchemas := map[string]string{
		"messages": `CREATE TABLE messages_new (
            hash TEXT NOT NULL,
            gid TEXT NOT NULL DEFAULT '',
            time TIMESTAMP,
            chat_id INTEGER,
            user_id INTEGER,
            user_name TEXT,
            msg_id INTEGER,
            PRIMARY KEY (gid, hash)
        )`,
		"spam": `CREATE TABLE spam_new (
            user_id INTEGER NOT NULL,
            gid TEXT NOT NULL DEFAULT '',
            time TIMESTAMP,
            checks TEXT,
            PRIMARY KEY (gid, user_id)
        )`,
	}
	copyColumns := map[string]string{
		"messages": "hash, gid, time, chat_id, user_id, user_name, msg_id",
		"spam":     "user_id, gid, time, checks",
	}

	stmts := []string{
		newSchemas[table],
		fmt.Sprintf("INSERT INTO %[1]s_new (%[2]s) SELECT %[2]s FROM %[1]s", table, copyColumns[table]),
		fmt.Sprintf("DROP TABLE %s", table),
		fmt.Sprintf("ALTER TABLE %[1]s_new RENAME TO %[1]s", table),
	}
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("failed to rebuild with composite primary key: %w", err)
		}
	}
	log.Printf("[INFO] locator table %s rebuilt with composite primary key (%s)", table, pkCols)
	return nil
}

// migratePrimaryKeyPostgres swaps the single-column primary key constraint for a composite one.
// safe under concurrent upgraded starts sharing one database: it double-checks the primary key
// under an exclusive table lock so only the first instance performs the swap.
func (l *Locator) migratePrimaryKeyPostgres(ctx context.Context, tx *sqlx.Tx, table, pkCols string) error {
	// fast path without taking an exclusive lock: already the expected composite key, nothing to do
	pk, err := l.primaryKeyColumns(ctx, tx, table)
	if err != nil {
		return err
	}
	if pk == pkCols {
		return nil
	}

	// lock the table so a second upgraded instance blocks here instead of racing the constraint
	// swap, then re-check under the lock. the ALTER below needs this lock level anyway, so taking
	// it explicitly on the migration path adds no extra contention.
	if _, err = tx.ExecContext(ctx, "LOCK TABLE "+pq.QuoteIdentifier(table)+" IN ACCESS EXCLUSIVE MODE"); err != nil {
		return fmt.Errorf("failed to lock table for primary key migration: %w", err)
	}
	pk, err = l.primaryKeyColumns(ctx, tx, table)
	if err != nil {
		return err
	}
	if pk == pkCols {
		return nil // another instance completed the migration while we waited for the lock
	}

	var constraintName string
	query := `SELECT tc.constraint_name FROM information_schema.table_constraints tc
        WHERE tc.table_name = $1 AND tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = current_schema()`
	if err := tx.GetContext(ctx, &constraintName, query, table); err != nil {
		return fmt.Errorf("failed to get primary key constraint name: %w", err)
	}

	//nolint:gosec // table and pkCols are hardcoded constants, constraint name comes from information_schema
	alter := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s, ADD PRIMARY KEY (%s)",
		table, pq.QuoteIdentifier(constraintName), pkCols)
	if _, err := tx.ExecContext(ctx, alter); err != nil {
		return fmt.Errorf("failed to swap primary key constraint: %w", err)
	}
	log.Printf("[INFO] locator table %s upgraded to composite primary key (%s)", table, pkCols)
	return nil
}

// primaryKeyColumns returns the table's primary key columns in key order, joined as "a, b" to match
// the pkCols form used by the migration. an empty string means no primary key.
func (l *Locator) primaryKeyColumns(ctx context.Context, tx *sqlx.Tx, table string) (string, error) {
	var cols []string
	switch l.Type() {
	case engine.Sqlite:
		if err := tx.SelectContext(ctx, &cols,
			"SELECT name FROM pragma_table_info(?) WHERE pk > 0 ORDER BY pk", table); err != nil {
			return "", fmt.Errorf("failed to read sqlite primary key columns: %w", err)
		}
	case engine.Postgres:
		query := `SELECT kcu.column_name FROM information_schema.table_constraints tc
            JOIN information_schema.key_column_usage kcu
                ON kcu.constraint_name = tc.constraint_name AND kcu.table_schema = tc.table_schema
            WHERE tc.table_name = $1 AND tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = current_schema()
            ORDER BY kcu.ordinal_position`
		if err := tx.SelectContext(ctx, &cols, query, table); err != nil {
			return "", fmt.Errorf("failed to read postgres primary key columns: %w", err)
		}
	default:
		return "", fmt.Errorf("unsupported database type %q", l.Type())
	}
	return strings.Join(cols, ", "), nil
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
		map[string]any{
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

// GetUserMessageIDs returns message IDs from a user for bulk deletion
func (l *Locator) GetUserMessageIDs(ctx context.Context, userID int64, limit int) ([]int, error) {
	l.RLock()
	defer l.RUnlock()

	query := l.Adopt(`SELECT msg_id FROM messages WHERE user_id = ? AND gid = ? ORDER BY time DESC LIMIT ?`)
	var msgIDs []int
	err := l.SelectContext(ctx, &msgIDs, query, userID, l.GID(), limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get user message IDs: %w", err)
	}
	return msgIDs, nil
}

// CountUserMessages returns the number of messages stored for the given user within the current gid.
// userID is parsed as int64 to match the spamcheck.Request.UserID string form.
func (l *Locator) CountUserMessages(ctx context.Context, userID string) (int, error) {
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid user id %q: %w", userID, err)
	}

	l.RLock()
	defer l.RUnlock()

	query := l.Adopt(`SELECT COUNT(*) FROM messages WHERE gid = ? AND user_id = ?`)
	var count int
	if err := l.GetContext(ctx, &count, query, l.GID(), uid); err != nil {
		return 0, fmt.Errorf("failed to count user messages: %w", err)
	}
	return count, nil
}

// UserMessageIDs returns message IDs from a user, newest first, capped by limit.
// userID is parsed as int64 to match the spamcheck.Request.UserID string form.
func (l *Locator) UserMessageIDs(ctx context.Context, userID string, limit int) ([]int, error) {
	uid, err := strconv.ParseInt(userID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid user id %q: %w", userID, err)
	}
	return l.GetUserMessageIDs(ctx, uid, limit)
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
