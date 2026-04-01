package events

import (
	"sync"
	"time"
)

const maxDMUsers = 50

// DMUser represents a user who sent a direct message to the bot
type DMUser struct {
	UserID      int64
	UserName    string
	DisplayName string
	Timestamp   time.Time
}

// dmUsers stores recent DM senders in memory with concurrent-safe access.
// entries are sorted by timestamp descending and capped at maxDMUsers.
type dmUsers struct {
	mu    sync.RWMutex
	users []DMUser
}

// Add stores or updates a DM user. If the user already exists (by UserID),
// the entry is updated with the new timestamp and details.
func (d *dmUsers) Add(user DMUser) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// dedup: remove existing entry with same UserID
	for i, u := range d.users {
		if u.UserID == user.UserID {
			d.users = append(d.users[:i], d.users[i+1:]...)
			break
		}
	}

	// prepend new entry (newest first)
	d.users = append([]DMUser{user}, d.users...)

	// cap at maxDMUsers, drop oldest (tail)
	if len(d.users) > maxDMUsers {
		d.users = d.users[:maxDMUsers]
	}
}

// List returns a copy of the stored DM users, sorted by timestamp descending.
func (d *dmUsers) List() []DMUser {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]DMUser, len(d.users))
	copy(result, d.users)
	return result
}
