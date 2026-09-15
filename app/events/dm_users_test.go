package events

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDMUsers_Add(t *testing.T) {
	t.Run("basic add", func(t *testing.T) {
		var d dmUsers
		d.Add(DMUser{UserID: 1, UserName: "alice", DisplayName: "Alice", Timestamp: time.Now()})
		assert.Len(t, d.List(), 1)
		assert.Equal(t, int64(1), d.List()[0].UserID)
	})

	t.Run("dedup updates timestamp", func(t *testing.T) {
		var d dmUsers
		ts1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
		ts2 := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)

		d.Add(DMUser{UserID: 1, UserName: "alice", DisplayName: "Alice", Timestamp: ts1})
		d.Add(DMUser{UserID: 2, UserName: "bob", DisplayName: "Bob", Timestamp: ts1})
		d.Add(DMUser{UserID: 1, UserName: "alice_new", DisplayName: "Alice New", Timestamp: ts2})

		users := d.List()
		assert.Len(t, users, 2, "should still have 2 users after dedup")
		assert.Equal(t, int64(1), users[0].UserID, "updated user should be first")
		assert.Equal(t, ts2, users[0].Timestamp, "timestamp should be updated")
		assert.Equal(t, "alice_new", users[0].UserName, "username should be updated")
		assert.Equal(t, "Alice New", users[0].DisplayName, "display name should be updated")
	})

	t.Run("cap at 50 drops oldest", func(t *testing.T) {
		var d dmUsers
		base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

		// add 55 users
		for i := range 55 {
			d.Add(DMUser{
				UserID:    int64(i),
				UserName:  "user",
				Timestamp: base.Add(time.Duration(i) * time.Minute),
			})
		}

		users := d.List()
		assert.Len(t, users, maxDMUsers)

		// newest should be first (user 54)
		assert.Equal(t, int64(54), users[0].UserID)
		// oldest kept should be user 5 (0-4 dropped)
		assert.Equal(t, int64(5), users[len(users)-1].UserID)
	})

	t.Run("ordering newest first", func(t *testing.T) {
		var d dmUsers
		ts1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
		ts2 := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
		ts3 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

		d.Add(DMUser{UserID: 1, Timestamp: ts1})
		d.Add(DMUser{UserID: 2, Timestamp: ts2})
		d.Add(DMUser{UserID: 3, Timestamp: ts3})

		users := d.List()
		assert.Equal(t, int64(3), users[0].UserID)
		assert.Equal(t, int64(2), users[1].UserID)
		assert.Equal(t, int64(1), users[2].UserID)
	})

	t.Run("concurrent safety", func(t *testing.T) {
		var d dmUsers
		var wg sync.WaitGroup

		for i := range 100 {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				d.Add(DMUser{UserID: int64(id), Timestamp: time.Now()})
			}(i)
		}
		wg.Wait()

		users := d.List()
		assert.LessOrEqual(t, len(users), maxDMUsers)
		assert.NotEmpty(t, users)
	})
}

func TestDMUsers_List(t *testing.T) {
	t.Run("returns copy", func(t *testing.T) {
		var d dmUsers
		d.Add(DMUser{UserID: 1, UserName: "alice", Timestamp: time.Now()})

		list := d.List()
		list[0].UserName = "modified"

		original := d.List()
		assert.Equal(t, "alice", original[0].UserName, "modifying returned slice should not affect storage")
	})

	t.Run("empty list", func(t *testing.T) {
		var d dmUsers
		users := d.List()
		assert.Empty(t, users)
		assert.NotNil(t, users)
	})
}
