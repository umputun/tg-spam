package storage

import (
	"context"
	"fmt"
	"log"
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

var approvedUsersSchema = `
        CREATE TABLE IF NOT EXISTS approved_users (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
			uid TEXT,
			gid TEXT DEFAULT '',
			name TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
        	UNIQUE(gid, uid)                              
        );
        CREATE INDEX IF NOT EXISTS idx_approved_users_uid ON approved_users(uid);
        CREATE INDEX IF NOT EXISTS idx_approved_users_gid ON approved_users(gid);
        CREATE INDEX IF NOT EXISTS idx_approved_users_name ON approved_users(name);
        CREATE INDEX IF NOT EXISTS idx_approved_users_timestamp ON approved_users(timestamp)
`

// NewApprovedUsers creates a new ApprovedUsers storage
func NewApprovedUsers(ctx context.Context, db *engine.SQL) (*ApprovedUsers, error) {
	err := engine.InitDB(ctx, db, "approved_users", approvedUsersSchema, migrateTableTx)
	if err != nil {
		return nil, err
	}
	return &ApprovedUsers{db: db, RWLocker: db.MakeLock()}, nil
}

// Read returns a list of all approved users
func (au *ApprovedUsers) Read(ctx context.Context) ([]approved.UserInfo, error) {
	au.RLock()
	defer au.RUnlock()

	users := []approvedUsersInfo{}
	gid := au.db.GID()
	err := au.db.SelectContext(ctx, &users, "SELECT uid, gid, name, timestamp FROM approved_users WHERE gid=? ORDER BY uid ASC", gid)
	if err != nil {
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

	query := "INSERT OR REPLACE INTO approved_users (uid, gid, name, timestamp) VALUES (?, ?, ?, ?)"
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

	var user approvedUsersInfo
	err := au.db.GetContext(ctx, &user, "SELECT uid, gid, name, timestamp FROM approved_users WHERE uid = ? AND gid = ?",
		id, au.db.GID())
	if err != nil {
		return fmt.Errorf("failed to get approved user for id %s: %w", id, err)
	}

	if _, err := au.db.ExecContext(ctx, "DELETE FROM approved_users WHERE uid = ? AND gid = ?", id, au.db.GID()); err != nil {
		return fmt.Errorf("failed to delete id %s: %w", id, err)
	}

	log.Printf("[INFO] user %q (%s) deleted from approved users", user.UserName, id)
	return nil
}

func migrateTableTx(ctx context.Context, tx *sqlx.Tx, gid string) error {
	// get current columns
	var cols []struct {
		CID       int     `db:"cid"`
		Name      string  `db:"name"`
		Type      string  `db:"type"`
		NotNull   bool    `db:"notnull"`
		DfltValue *string `db:"dflt_value"`
		PK        bool    `db:"pk"`
	}
	if err := tx.Select(&cols, "PRAGMA table_info(approved_users)"); err != nil {
		return fmt.Errorf("failed to get table info: %w", err)
	}

	// check if already migrated
	for _, col := range cols {
		if col.Name == "uid" {
			log.Printf("[DEBUG] approved_users table already migrated")
			return nil
		}
	}

	// backup data
	var rows []struct {
		ID        string    `db:"id"`
		Name      string    `db:"name"`
		Timestamp time.Time `db:"timestamp"`
	}
	if err := tx.SelectContext(ctx, &rows, "SELECT id, name, timestamp FROM approved_users"); err != nil {
		return fmt.Errorf("failed to backup data: %w", err)
	}

	// drop old table and create new one
	if _, err := tx.ExecContext(ctx, "DROP TABLE approved_users"); err != nil {
		return fmt.Errorf("failed to drop old table: %w", err)
	}

	if _, err := tx.ExecContext(ctx, approvedUsersSchema); err != nil {
		return fmt.Errorf("failed to create new schema: %w", err)
	}

	// restore data
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO approved_users (gid, uid, name, timestamp) VALUES (?, ?, ?, ?)",
			gid, r.ID, r.Name, r.Timestamp,
		); err != nil {
			return fmt.Errorf("failed to restore data: %w", err)
		}
	}

	log.Printf("[DEBUG] approved_users table migrated, records: %d", len(rows))
	return nil
}
