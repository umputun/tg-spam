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
	mu          sync.RWMutex
	users       []DMUser
	subscribers map[chan DMUser]struct{}
}

// Add stores or updates a DM user. If the user already exists (by UserID),
// the entry is updated with the new timestamp and details. After adding,
// all SSE subscribers are notified.
func (d *dmUsers) Add(user DMUser) {
	d.mu.Lock()

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

	// snapshot subscribers while holding the lock
	subs := make([]chan DMUser, 0, len(d.subscribers))
	for ch := range d.subscribers {
		subs = append(subs, ch)
	}
	d.mu.Unlock()

	// notify subscribers without holding the lock
	for _, ch := range subs {
		select {
		case ch <- user:
		default:
			// subscriber is slow, skip
		}
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

// Subscribe returns a channel that receives new DM users as they are added.
func (d *dmUsers) Subscribe() <-chan DMUser {
	d.mu.Lock()
	defer d.mu.Unlock()
	ch := make(chan DMUser, 10)
	if d.subscribers == nil {
		d.subscribers = make(map[chan DMUser]struct{})
	}
	d.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
func (d *dmUsers) Unsubscribe(ch <-chan DMUser) {
	d.mu.Lock()
	defer d.mu.Unlock()
	// find the matching channel in subscribers map
	for sub := range d.subscribers {
		if sub == ch {
			delete(d.subscribers, sub)
			close(sub)
			return
		}
	}
}
