package storage

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver loaded here

	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/approved"
)

// ApprovedUsers is a storage for approved users
type ApprovedUsers struct {
	db *engine.SQL
	engine.RWLocker
}

// approvedUsersInfo is a struct to store approved user info in the database
type approvedUsersInfo struct {
	UserID    string    `db:"uid"`
	GroupID   string    `db:"gid"`
	UserName  string    `db:"name"`
	Timestamp time.Time `db:"timestamp"`
}

// all approved users queries
const (
	CmdCreateApprovedUsersTable engine.DBCmd = iota + 100
	CmdCreateApprovedUsersIndexes
	CmdAddApprovedUser
	CmdAddUIDColumn
	CmdAddGIDColumn
)

// queries holds all approved users queries
var approvedUsersQueries = engine.QueryMap{
	engine.Sqlite: {
		CmdCreateApprovedUsersTable: `
			CREATE TABLE IF NOT EXISTS approved_users (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				uid TEXT,
				gid TEXT DEFAULT '',
				name TEXT,
				timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(gid, uid)                              
			)`,
		CmdCreateApprovedUsersIndexes: `
			CREATE INDEX IF NOT EXISTS idx_approved_users_uid ON approved_users(uid);
			CREATE INDEX IF NOT EXISTS idx_approved_users_gid ON approved_users(gid);
			CREATE INDEX IF NOT EXISTS idx_approved_users_name ON approved_users(name);
			CREATE INDEX IF NOT EXISTS idx_approved_users_timestamp ON approved_users(timestamp)`,
		CmdAddApprovedUser: "INSERT OR REPLACE INTO approved_users (uid, gid, name, timestamp) VALUES (?, ?, ?, ?)",
		CmdAddUIDColumn:    "ALTER TABLE approved_users ADD COLUMN uid TEXT",
		CmdAddGIDColumn:    "ALTER TABLE approved_users ADD COLUMN gid TEXT DEFAULT ''",
	},
	engine.Postgres: {
		CmdCreateApprovedUsersTable: `
			CREATE TABLE IF NOT EXISTS approved_users (
				id SERIAL PRIMARY KEY,
				uid TEXT,
				gid TEXT DEFAULT '',
				name TEXT,
				timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				UNIQUE(gid, uid)                              
			)`,
		CmdCreateApprovedUsersIndexes: `
			CREATE INDEX IF NOT EXISTS idx_approved_users_uid ON approved_users(uid);
			CREATE INDEX IF NOT EXISTS idx_approved_users_gid ON approved_users(gid);
			CREATE INDEX IF NOT EXISTS idx_approved_users_name ON approved_users(name);
			CREATE INDEX IF NOT EXISTS idx_approved_users_timestamp ON approved_users(timestamp)`,
		CmdAddApprovedUser: "INSERT INTO approved_users (uid, gid, name, timestamp) VALUES ($1, $2, $3, $4) ON CONFLICT (gid, uid) DO UPDATE SET name=EXCLUDED.name, timestamp=EXCLUDED.timestamp",
		CmdAddUIDColumn:    "ALTER TABLE approved_users ADD COLUMN IF NOT EXISTS uid TEXT",
		CmdAddGIDColumn:    "ALTER TABLE approved_users ADD COLUMN IF NOT EXISTS gid TEXT DEFAULT ''",
	},
}

// NewApprovedUsers creates a new ApprovedUsers storage
func NewApprovedUsers(ctx context.Context, db *engine.SQL) (*ApprovedUsers, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}
	res := &ApprovedUsers{db: db, RWLocker: db.MakeLock()}
	if err := res.init(ctx); err != nil {
		return nil, fmt.Errorf("failed to init approved users storage: %w", err)
	}
	return res, nil
}

func (au *ApprovedUsers) init(ctx context.Context) error {
	tx, err := au.db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// create table first
	createSchema, err := engine.PickQuery(approvedUsersQueries, au.db.Type(), CmdCreateApprovedUsersTable)
	if err != nil {
		return fmt.Errorf("failed to get create table query: %w", err)
	}
	if _, err = tx.ExecContext(ctx, createSchema); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// try to migrate if needed
	if err = au.migrate(ctx, tx, au.db.GID()); err != nil {
		return fmt.Errorf("failed to migrate table: %w", err)
	}

	// create indices after migration when all columns exist
	createIndexes, err := engine.PickQuery(approvedUsersQueries, au.db.Type(), CmdCreateApprovedUsersIndexes)
	if err != nil {
		return fmt.Errorf("failed to get create indexes query: %w", err)
	}
	if _, err = tx.ExecContext(ctx, createIndexes); err != nil {
		return fmt.Errorf("failed to create indexes: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// Read returns a list of all approved users
func (au *ApprovedUsers) Read(ctx context.Context) ([]approved.UserInfo, error) {
	au.RLock()
	defer au.RUnlock()

	query := au.db.Adopt("SELECT uid, gid, name, timestamp FROM approved_users WHERE gid = ? ORDER BY uid ASC")
	users := []approvedUsersInfo{}
	if err := au.db.SelectContext(ctx, &users, query, au.db.GID()); err != nil {
		return nil, fmt.Errorf("failed to get approved users: %w", err)
	}

	res := make([]approved.UserInfo, len(users))
	for i, u := range users {
		res[i] = approved.UserInfo{
			UserID:    u.UserID,
			UserName:  u.UserName,
			Timestamp: u.Timestamp,
		}
	}
	log.Printf("[DEBUG] read %d approved users", len(res))
	return res, nil
}

// Write adds a user to the approved list
func (au *ApprovedUsers) Write(ctx context.Context, user approved.UserInfo) error {
	if user.UserID == "" {
		return fmt.Errorf("user id can't be empty")
	}

	au.Lock()
	defer au.Unlock()

	if user.Timestamp.IsZero() {
		user.Timestamp = time.Now()
	}

	query, err := engine.PickQuery(approvedUsersQueries, au.db.Type(), CmdAddApprovedUser)
	if err != nil {
		return fmt.Errorf("failed to get write query: %w", err)
	}

	if _, err := au.db.ExecContext(ctx, query, user.UserID, au.db.GID(), user.UserName, user.Timestamp); err != nil {
		return fmt.Errorf("failed to insert user %+v: %w", user, err)
	}

	log.Printf("[INFO] user %q (%s) added to approved users", user.UserName, user.UserID)
	return nil
}

// Delete removes a user from the approved list
func (au *ApprovedUsers) Delete(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("user id can't be empty")
	}

	au.Lock()
	defer au.Unlock()

	// check if user exists first
	var user approvedUsersInfo
	query := au.db.Adopt("SELECT uid, gid, name, timestamp FROM approved_users WHERE uid = ? AND gid = ?")
	if err := au.db.GetContext(ctx, &user, query, id, au.db.GID()); err != nil {
		return fmt.Errorf("failed to get approved user for id %s: %w", id, err)
	}

	// delete user  "DELETE FROM approved_users WHERE uid = ? AND gid = ?"
	query = au.db.Adopt("DELETE FROM approved_users WHERE uid = ? AND gid = ?")
	if _, err := au.db.ExecContext(ctx, query, id, au.db.GID()); err != nil {
		return fmt.Errorf("failed to delete id %s: %w", id, err)
	}

	log.Printf("[INFO] user %q (%s) deleted from approved users", user.UserName, id)
	return nil
}

// migrateTableTx handles migration within a transaction
func (au *ApprovedUsers) migrate(ctx context.Context, tx *sqlx.Tx, gid string) error {
	// try to select with new structure, if works - already migrated
	var count int
	err := tx.GetContext(ctx, &count, "SELECT COUNT(*) FROM approved_users WHERE uid='' AND gid=''")
	if err == nil {
		log.Printf("[DEBUG] approved_users table already migrated")
		return nil
	}

	// add columns using db-specific queries
	addUIDQuery, err := engine.PickQuery(approvedUsersQueries, au.db.Type(), CmdAddUIDColumn)
	if err != nil {
		return fmt.Errorf("failed to get add UID query: %w", err)
	}

	_, err = tx.ExecContext(ctx, addUIDQuery)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("failed to add uid column: %w", err)
	}

	addGIDQuery, err := engine.PickQuery(approvedUsersQueries, au.db.Type(), CmdAddGIDColumn)
	if err != nil {
		return fmt.Errorf("failed to get add GID query: %w", err)
	}

	_, err = tx.ExecContext(ctx, addGIDQuery)
	if err != nil && !strings.Contains(err.Error(), "duplicate column") {
		return fmt.Errorf("failed to add gid column: %w", err)
	}

	migrateQuery := au.db.Adopt("UPDATE approved_users SET uid = id, gid = ? WHERE uid IS NULL OR uid = ''")
	if _, err = tx.ExecContext(ctx, migrateQuery, gid); err != nil {
		return fmt.Errorf("failed to migrate data: %w", err)
	}

	log.Printf("[DEBUG] approved_users table migrated")
	return nil
}
