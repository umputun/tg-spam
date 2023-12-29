package storage

import (
	"fmt"
	"log"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver loaded here

	"github.com/umputun/tg-spam/lib"
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
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS approved_users (
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
func (au *ApprovedUsers) Read() ([]lib.UserInfo, error) {
	users := []approvedUsersInfo{}
	err := au.db.Select(&users, "SELECT id, name, timestamp FROM approved_users ORDER BY timestamp DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to get approved users: %w", err)
	}
	res := make([]lib.UserInfo, len(users))
	for i, u := range users {
		res[i] = lib.UserInfo{
			UserID:    u.UserID,
			UserName:  u.UserName,
			Timestamp: u.Timestamp,
		}
	}
	log.Printf("[DEBUG] read %d approved users", len(res))
	return res, nil
}

// Write writes new user info to the storage
func (au *ApprovedUsers) Write(user lib.UserInfo) error {
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
