package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
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

// NewApprovedUsers creates a new ApprovedUsers storage
func NewApprovedUsers(db *sqlx.DB) (*ApprovedUsers, error) {
	_, err := db.Exec("CREATE TABLE IF NOT EXISTS approved_users " +
		"(id INTEGER PRIMARY KEY, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP)")
	if err != nil {
		return nil, fmt.Errorf("failed to create approved_users table: %w", err)
	}
	return &ApprovedUsers{db: db}, nil
}

// Store saves ids to the storage, overwriting the existing content
func (au *ApprovedUsers) Store(ids []string) error {
	log.Printf("[DEBUG] storing %d ids", len(ids))

	tx, err := au.db.Beginx()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}

	var committed bool
	defer func() {
		// rollback if not committed due to error
		if !committed {
			if err := tx.Rollback(); err != nil {
				log.Printf("[WARN] failed to rollback transaction: %v", err)
			}
		}
	}()

	for _, id := range ids {
		idVal, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse id %s: %w", id, err)
		}

		_, err = tx.Exec("INSERT INTO approved_users (id, timestamp) VALUES (?, ?) ON CONFLICT(id) DO NOTHING", idVal, time.Now())
		if err != nil {
			return fmt.Errorf("failed to insert id %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true
	return nil
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
