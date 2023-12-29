package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver loaded here
)

// ApprovedUsers is a storage for approved users ids
// Read is not thread-safe
type ApprovedUsers struct {
	db         *sqlx.DB
	lastReadID int64 // last id for read. Note: this is not a thread-safe part, don't call parallel reads!
}

// ApprovedUsersInfo represents information about an approved user.
type ApprovedUsersInfo struct {
	UserID    int64     `db:"id"`
	UserName  string    `db:"name"`
	Timestamp time.Time `db:"timestamp"`
}

// NewApprovedUsers creates a new ApprovedUsers storage
func NewApprovedUsers(db *sqlx.DB) (*ApprovedUsers, error) {
	// create the table if it does not exist
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS approved_users (
        id INTEGER PRIMARY KEY,
        name TEXT,
        timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
    )`)
	if err != nil {
		return nil, fmt.Errorf("failed to create or verify approved_users table: %w", err)
	}

	// attempt to add the 'name' column, ignore error if the column already exists
	_, err = db.Exec("ALTER TABLE approved_users ADD COLUMN name TEXT")
	if err != nil {
		// Check if the error is because the column already exists
		if !strings.Contains(err.Error(), "duplicate column name") {
			return nil, fmt.Errorf("failed to add 'name' column to approved_users table: %w", err)
		}
	}
	return &ApprovedUsers{db: db}, nil
}

// Read reads ids from the storage
// Each read returns one id, followed by a newline
func (au *ApprovedUsers) Read(p []byte) (n int, err error) {
	row := au.db.QueryRow("SELECT id FROM approved_users WHERE id > ? ORDER BY id LIMIT 1", au.lastReadID)
	var id int64
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, io.EOF
		}
		return 0, fmt.Errorf("failed to read id: %w", err)
	}
	au.lastReadID = id
	idBytes := []byte(fmt.Sprintf("%d\n", id))
	n = copy(p, idBytes)
	return n, nil
}

// GetAll returns all approved users.
func (au *ApprovedUsers) GetAll() ([]ApprovedUsersInfo, error) {
	users := []ApprovedUsersInfo{}
	err := au.db.Select(&users, "SELECT id, name, timestamp FROM approved_users ORDER BY timestamp DESC")
	if err != nil {
		return nil, fmt.Errorf("failed to get approved users: %w", err)
	}
	return users, nil
}

// Write writes a new user info to the storage
func (au *ApprovedUsers) Write(user ApprovedUsersInfo) error {
	if user.Timestamp.IsZero() {
		user.Timestamp = time.Now()
	}
	// Prepare the query to insert a new record, ignoring if it already exists
	query := "INSERT OR IGNORE INTO approved_users (id, name, timestamp) VALUES (?, ?, ?)"
	if _, err := au.db.Exec(query, user.UserID, user.UserName, user.Timestamp); err != nil {
		return fmt.Errorf("failed to insert user %d: %w", user.UserID, err)
	}

	return nil
}

// Delete deletes the given id from the storage
func (au *ApprovedUsers) Delete(id int64) error {
	if _, err := au.db.Exec("DELETE FROM approved_users WHERE id = ?", id); err != nil {
		return fmt.Errorf("failed to delete id %d: %w", id, err)
	}
	return nil
}
