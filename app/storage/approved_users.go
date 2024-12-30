package storage

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver loaded here

	"github.com/umputun/tg-spam/lib/approved"
)

// ApprovedUsers is a storage for approved users
type ApprovedUsers struct {
	db *Engine
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
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
        );
        CREATE INDEX IF NOT EXISTS idx_approved_users_uid ON approved_users(uid);
        CREATE INDEX IF NOT EXISTS idx_approved_users_gid ON approved_users(gid);
        CREATE INDEX IF NOT EXISTS idx_approved_users_name ON approved_users(name);
        CREATE INDEX IF NOT EXISTS idx_approved_users_timestamp ON approved_users(timestamp)
`

// NewApprovedUsers creates a new ApprovedUsers storage
func NewApprovedUsers(ctx context.Context, db *Engine) (*ApprovedUsers, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}

	// create schema in a single transaction
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx, approvedUsersSchema); err != nil {
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	if err := migrateTable(&db.DB); err != nil {
		return nil, err
	}

	return &ApprovedUsers{db: db}, nil
}

// Read returns a list of all approved users
func (au *ApprovedUsers) Read(ctx context.Context) ([]approved.UserInfo, error) {
	au.db.RLock()
	defer au.db.RUnlock()

	users := []approvedUsersInfo{}
	err := au.db.SelectContext(ctx, &users, "SELECT uid, gid, name, timestamp FROM approved_users ORDER BY uid ASC")
	if err != nil {
		return nil, fmt.Errorf("failed to get approved users: %w", err)
	}

	res := make([]approved.UserInfo, len(users))
	for i, u := range users {
		res[i] = approved.UserInfo{
			UserID:    u.UserID,
			GroupID:   u.GroupID,
			UserName:  u.UserName,
			Timestamp: u.Timestamp,
		}
	}
	log.Printf("[DEBUG] read %d approved users", len(res))
	return res, nil
}

// Write adds a new approved user
func (au *ApprovedUsers) Write(ctx context.Context, user approved.UserInfo) error {
	au.db.Lock()
	defer au.db.Unlock()

	if user.Timestamp.IsZero() {
		user.Timestamp = time.Now()
	}

	query := "INSERT OR IGNORE INTO approved_users (uid, gid, name, timestamp) VALUES (?, ?, ?, ?)"
	if _, err := au.db.ExecContext(ctx, query, user.UserID, user.GroupID, user.UserName, user.Timestamp); err != nil {
		return fmt.Errorf("failed to insert user %+v: %w", user, err)
	}
	log.Printf("[INFO] user %s added to approved users", user.String())
	return nil
}

// Delete removes an approved user by its ID
func (au *ApprovedUsers) Delete(ctx context.Context, id string) error {
	au.db.Lock()
	defer au.db.Unlock()

	var user approvedUsersInfo
	err := au.db.GetContext(ctx, &user, "SELECT uid, gid, name, timestamp FROM approved_users WHERE uid = ?", id)
	if err != nil {
		return fmt.Errorf("failed to get approved user for id %s: %w", id, err)
	}

	if _, err := au.db.ExecContext(ctx, "DELETE FROM approved_users WHERE uid = ?", id); err != nil {
		return fmt.Errorf("failed to delete id %s: %w", id, err)
	}

	log.Printf("[INFO] user %q (%s) deleted from approved users", user.UserName, id)
	return nil
}
func migrateTable(db *sqlx.DB) error {
	var exists int
	err := db.Get(&exists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='approved_users'")
	if err != nil {
		return fmt.Errorf("failed to check for approved_users table existence: %w", err)
	}

	if exists == 0 {
		return nil
	}
	log.Printf("[DEBUG] approved_users table migration")

	// get current columns
	var cols []struct {
		CID       int     `db:"cid"`
		Name      string  `db:"name"`
		Type      string  `db:"type"`
		NotNull   bool    `db:"notnull"`
		DfltValue *string `db:"dflt_value"`
		PK        bool    `db:"pk"`
	}
	err = db.Select(&cols, "PRAGMA table_info(approved_users)")
	if err != nil {
		return fmt.Errorf("failed to get table info for approved_users: %w", err)
	}

	// check if already migrated
	hasUID := false
	for _, col := range cols {
		if col.Name == "uid" {
			hasUID = true
			break
		}
	}
	if hasUID {
		log.Printf("[DEBUG] approved_users table already migrated")
		return nil
	}

	tx, err := db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback()

	// backup data
	var rows []struct {
		ID        string    `db:"id"`
		Name      string    `db:"name"`
		Timestamp time.Time `db:"timestamp"`
	}
	if err = tx.Select(&rows, "SELECT id, name, timestamp FROM approved_users"); err != nil {
		return fmt.Errorf("failed to backup data: %w", err)
	}

	// drop old table
	if _, err = tx.Exec("DROP TABLE approved_users"); err != nil {
		return fmt.Errorf("failed to drop old table: %w", err)
	}

	// create new table with updated schema
	if _, err = tx.Exec(approvedUsersSchema); err != nil {
		return fmt.Errorf("failed to create new schema: %w", err)
	}

	// restore data
	for _, r := range rows {
		_, err = tx.Exec(
			"INSERT INTO approved_users (uid, name, timestamp) VALUES (?, ?, ?)",
			r.ID, r.Name, r.Timestamp,
		)
		if err != nil {
			return fmt.Errorf("failed to restore data: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration: %w", err)
	}
	log.Printf("[DEBUG] approved_users table migrated")

	return nil
}
