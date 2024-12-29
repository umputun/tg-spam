package storage

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver loaded here

	"github.com/umputun/tg-spam/lib/approved"
)

// ApprovedUsers is a storage for approved users
type ApprovedUsers struct {
	db   *sqlx.DB
	lock *sync.RWMutex
}

type approvedUsersInfo struct {
	UserID    string    `db:"id"`
	UserName  string    `db:"name"`
	Timestamp time.Time `db:"timestamp"`
}

// NewApprovedUsers creates a new ApprovedUsers storage
func NewApprovedUsers(ctx context.Context, db *sqlx.DB) (*ApprovedUsers, error) {
	if db == nil {
		return nil, fmt.Errorf("db connection is nil")
	}
	if err := setSqlitePragma(db); err != nil {
		return nil, fmt.Errorf("failed to set sqlite pragma: %w", err)
	}

	if err := migrateTable(db); err != nil {
		return nil, err
	}

	query := `CREATE TABLE IF NOT EXISTS approved_users (
        id TEXT PRIMARY KEY,
        name TEXT,
        timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
    )`
	if _, err := db.ExecContext(ctx, query); err != nil {
		return nil, fmt.Errorf("failed to create approved_users table: %w", err)
	}

	return &ApprovedUsers{db: db, lock: &sync.RWMutex{}}, nil
}

// Read returns a list of all approved users
func (au *ApprovedUsers) Read(ctx context.Context) ([]approved.UserInfo, error) {
	au.lock.RLock()
	defer au.lock.RUnlock()

	users := []approvedUsersInfo{}
	err := au.db.SelectContext(ctx, &users, "SELECT id, name, timestamp FROM approved_users ORDER BY timestamp DESC")
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

// Write adds a new approved user
func (au *ApprovedUsers) Write(ctx context.Context, user approved.UserInfo) error {
	au.lock.Lock()
	defer au.lock.Unlock()

	if user.Timestamp.IsZero() {
		user.Timestamp = time.Now()
	}

	query := "INSERT OR IGNORE INTO approved_users (id, name, timestamp) VALUES (?, ?, ?)"
	if _, err := au.db.ExecContext(ctx, query, user.UserID, user.UserName, user.Timestamp); err != nil {
		return fmt.Errorf("failed to insert user %+v: %w", user, err)
	}
	log.Printf("[INFO] user %s added to approved users", user.String())
	return nil
}

// Delete removes an approved user by its ID
func (au *ApprovedUsers) Delete(ctx context.Context, id string) error {
	au.lock.Lock()
	defer au.lock.Unlock()

	var user approvedUsersInfo
	err := au.db.GetContext(ctx, &user, "SELECT id, name, timestamp FROM approved_users WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to get approved user for id %s: %w", id, err)
	}

	if _, err := au.db.ExecContext(ctx, "DELETE FROM approved_users WHERE id = ?", id); err != nil {
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

	for _, col := range cols {
		if col.Name == "id" && col.Type == "INTEGER" {
			if _, err = db.Exec("DROP TABLE approved_users"); err != nil {
				return fmt.Errorf("failed to drop old approved_users table: %w", err)
			}
			break
		}
	}

	return nil
}
