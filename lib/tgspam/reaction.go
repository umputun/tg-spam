package tgspam

import (
	"fmt"
	"sync"
	"time"

	cache "github.com/go-pkgz/expirable-cache/v3"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

// reactionDetector tracks reaction counts per user in a sliding window
type reactionDetector struct {
	threshold int
	window    time.Duration
	cache     cache.Cache[int64, reactionHistory]
	mu        sync.RWMutex
}

type reactionHistory struct {
	count     int
	firstSeen time.Time
}

// newReactionDetector creates a new reaction detector; returns nil if threshold <= 0 (disabled)
func newReactionDetector(threshold int, window time.Duration) *reactionDetector {
	if threshold <= 0 {
		return nil
	}
	const maxUsers = 10000
	return &reactionDetector{
		threshold: threshold,
		window:    window,
		cache:     cache.NewCache[int64, reactionHistory]().WithMaxKeys(maxUsers).WithTTL(window * 2),
	}
}

// check increments the reaction counter for userID and returns spam=true when threshold exceeded within window
func (d *reactionDetector) check(userID int64) spamcheck.Response {
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	h, _ := d.cache.Get(userID)

	// reset counter when window has expired
	if !h.firstSeen.IsZero() && now.After(h.firstSeen.Add(d.window)) {
		h = reactionHistory{}
	}

	if h.firstSeen.IsZero() {
		h.firstSeen = now
	}
	h.count++

	d.cache.Set(userID, h, d.window*2)

	if h.count >= d.threshold {
		return spamcheck.Response{
			Name:    "reactions",
			Spam:    true,
			Details: fmt.Sprintf("%d reactions in %s", h.count, now.Sub(h.firstSeen).Round(time.Second)),
		}
	}

	details := fmt.Sprintf("%d/%d reactions in window", h.count, d.threshold)
	return spamcheck.Response{Name: "reactions", Spam: false, Details: details}
}
