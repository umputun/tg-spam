// Package storage provides a simple storage for this toy chat application.
// It uses SQLite database to store messages. The database is created automatically if it doesn't exist.
package storage

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // sqlite driver loaded here
)

// Message represents a single message in our application.
type Message struct {
	Content   string
	Username  string
	Timestamp string
}

// Messages is a type that manages messages stored in a database.
type Messages struct {
	db *sql.DB
}

// NewMessages is a function that creates a new instance of Messages struct and initializes its database connection
// by calling the `initDB` method. It takes a `db` parameter of type `*sql.DB` and returns a pointer to a `Messages`
func NewMessages(db *sql.DB) (*Messages, error) {
	res := &Messages{db: db}
	if err := res.initDB(); err != nil {
		return nil, fmt.Errorf("failed to init db: %w", err)
	}
	return &Messages{db: db}, nil
}

// Add is a method that adds a new message to the database. It takes a `content` and `username` parameters
// of type `string` and returns an error if any.
func (m *Messages) Add(content, username string) error {
	_, err := m.db.Exec("INSERT INTO messages (content, username) VALUES (?, ?)", content, username)
	if err != nil {
		return fmt.Errorf("failed to insert message: %w", err)
	}
	return nil
}

// Last is a method that returns last messages from the database. It returns a slice of `Message` and an error if any.
// Sorts messages by timestamp in descending order.
func (m *Messages) Last(count int) ([]Message, error) {
	rows, err := m.db.Query("SELECT content, username, timestamp FROM messages ORDER BY timestamp DESC LIMIT ?", count)
	if err != nil {
		return nil, fmt.Errorf("failed to query messages: %w", err)
	}
	defer rows.Close()

	var res []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.Content, &msg.Username, &msg.Timestamp); err != nil {
			return nil, fmt.Errorf("failed to scan message: %w", err)
		}
		res = append(res, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate over messages: %w", err)
	}
	return res, nil
}

// Count is a method that returns the number of messages in the database.
func (m *Messages) Count() (int, error) {
	var count int
	if err := m.db.QueryRow("SELECT COUNT(*) FROM messages").Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count messages: %w", err)
	}
	return count, nil
}

func (m *Messages) initDB() error {
	var err error
	m.db, err = sql.Open("sqlite", "messages.db")
	if err != nil {
		return fmt.Errorf("failed to open db: %w", err)
	}
	_, err = m.db.Exec(`
		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			content TEXT NOT NULL,
			username TEXT NOT NULL,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}
	return nil
}
