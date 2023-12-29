package storage

import (
	"fmt"
	"log"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver loaded here

	"github.com/umputun/tg-spam/lib/approved"
)

// ApprovedUsers is a storage for approved users ids
type ApprovedUsers struct {
	db *sqlx.DB
}

// ApprovedUsersInfo represents information about an approved user.
type approvedUsersInfo struct {
	UserID    string    `db:"id"`
	UserName  string    `db:"name"`
	Timestamp time.Time `db:"timestamp"`
}

// NewApprovedUsers creates a new ApprovedUsers storage
func NewApprovedUsers(db *sqlx.DB) (*ApprovedUsers, error) {
	// migrate the table if necessary
	err := migrateTable(db)
	if err != nil {
		return nil, err
	}

	// create the table if it doesn't exist
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS approved_users (
        id TEXT PRIMARY KEY,
        name TEXT,
        timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
    )`)
	if err != nil {
		return nil, fmt.Errorf("failed to create approved_users table: %w", err)
	}

	return &ApprovedUsers{db: db}, nil
}

// Read returns all approved users.
func (au *ApprovedUsers) Read() ([]approved.UserInfo, error) {
	users := []approvedUsersInfo{}
	err := au.db.Select(&users, "SELECT id, name, timestamp FROM approved_users ORDER BY timestamp DESC")
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

// Write writes new user info to the storage
func (au *ApprovedUsers) Write(user approved.UserInfo) error {
	if user.Timestamp.IsZero() {
		user.Timestamp = time.Now()
	}
	// Prepare the query to insert a new record, ignoring if it already exists
	query := "INSERT OR IGNORE INTO approved_users (id, name, timestamp) VALUES (?, ?, ?)"
	if _, err := au.db.Exec(query, user.UserID, user.UserName, user.Timestamp); err != nil {
		return fmt.Errorf("failed to insert user %+v: %w", user, err)
	}
	log.Printf("[INFO] user %s added to approved users", user.String())
	return nil
}

// Delete deletes the given id from the storage
func (au *ApprovedUsers) Delete(id string) error {
	var user approvedUsersInfo

	// retrieve the user's name and timestamp for logging purposes
	err := au.db.Get(&user, "SELECT id, name, timestamp FROM approved_users WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to get approved user for id %s: %w", id, err)
	}

	if _, err := au.db.Exec("DELETE FROM approved_users WHERE id = ?", id); err != nil {
		return fmt.Errorf("failed to delete id %s: %w", id, err)
	}

	log.Printf("[INFO] user %q (%s) deleted from approved users", user.UserName, id)
	return nil
}

func migrateTable(db *sqlx.DB) error {
	// Check if the table exists
	var exists int
	err := db.Get(&exists, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='approved_users'")
	if err != nil {
		return fmt.Errorf("failed to check for approved_users table existence: %w", err)
	}

	if exists == 0 {
		return nil // Table does not exist, no need to migrate
	}

	// Get column info for 'id' column
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

	// Check if 'id' column is INTEGER
	for _, col := range cols {
		if col.Name == "id" && col.Type == "INTEGER" {
			// Drop the table if 'id' is of type INTEGER
			_, err = db.Exec("DROP TABLE approved_users")
			if err != nil {
				return fmt.Errorf("failed to drop old approved_users table: %w", err)
			}
			break
		}
	}

	return nil
}
