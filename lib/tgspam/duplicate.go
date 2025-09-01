package tgspam

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"sync"
	"time"

	cache "github.com/go-pkgz/expirable-cache/v3"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

// duplicateDetector tracks message history for duplicate detection
type duplicateDetector struct {
	threshold         int
	window            time.Duration
	cache             cache.Cache[int64, userHistory] // LRU cache with max users limit
	mu                sync.RWMutex
	lastCleanup       time.Time
	cleanupInterval   time.Duration
	maxUsers          int
	maxEntriesPerUser int
}

type userHistory struct {
	entries  []hashEntry            // for time-based cleanup
	trackers map[string]hashTracker // hash -> tracker with count and message IDs
}

type hashEntry struct {
	hash string
	time time.Time
}

type hashTracker struct {
	count      int   // number of duplicates
	messageIDs []int // message IDs for deletion
}

// newDuplicateDetector creates a new duplicate detector
func newDuplicateDetector(threshold int, window time.Duration) *duplicateDetector {
	if threshold <= 0 {
		return nil // disabled
	}

	const defaultMaxUsers = 10000
	const defaultMaxEntriesPerUser = 200 // prevent memory exhaustion from malicious users

	return &duplicateDetector{
		threshold:         threshold,
		window:            window,
		cache:             cache.NewCache[int64, userHistory]().WithMaxKeys(defaultMaxUsers).WithTTL(window * 2),
		cleanupInterval:   10 * time.Minute,
		maxUsers:          defaultMaxUsers,
		maxEntriesPerUser: defaultMaxEntriesPerUser,
	}
}

// check tracks a message and checks if it's a duplicate spam
func (d *duplicateDetector) check(req spamcheck.Request) spamcheck.Response {
	if d == nil || d.threshold <= 0 || req.UserID == "" {
		return spamcheck.Response{Name: "duplicate", Spam: false, Details: "check disabled"}
	}

	userID, err := strconv.ParseInt(req.UserID, 10, 64)
	if err != nil {
		return spamcheck.Response{Name: "duplicate", Spam: false, Details: "invalid user id"}
	}

	count, extraIDs := d.trackMessage(userID, req.Msg, req.Meta.MessageID)

	if count >= d.threshold {
		return spamcheck.Response{
			Name:           "duplicate",
			Spam:           true,
			Details:        fmt.Sprintf("message repeated %d times in %s", count, d.window),
			ExtraDeleteIDs: extraIDs,
		}
	}

	return spamcheck.Response{Name: "duplicate", Spam: false, Details: "no duplicates found"}
}

// trackMessage tracks a message and returns the count of duplicates and extra message IDs to delete
func (d *duplicateDetector) trackMessage(userID int64, msg string, messageID int) (count int, extraIDs []int) {
	msgHash := d.hash(msg)
	now := time.Now()

	// use mutex to protect the entire Get-Modify-Set operation
	// this ensures atomicity for each user's history updates
	d.mu.Lock()
	defer d.mu.Unlock()

	// periodically cleanup expired entries for all users
	if now.Sub(d.lastCleanup) > d.cleanupInterval {
		d.performCleanup(now)
		d.lastCleanup = now
	}

	// get or create user history
	history, found := d.cache.Get(userID)
	if !found {
		history = userHistory{
			entries:  make([]hashEntry, 0),
			trackers: make(map[string]hashTracker),
		}
	}

	// initialize trackers map if nil (for backward compatibility)
	if history.trackers == nil {
		history.trackers = make(map[string]hashTracker)
	}

	// clean old entries
	cutoff := now.Add(-d.window)
	// create a new slice to avoid sharing underlying array
	filtered := make([]hashEntry, 0, len(history.entries)+1)
	newTrackers := make(map[string]hashTracker)

	for _, entry := range history.entries {
		if entry.time.After(cutoff) {
			filtered = append(filtered, entry)
			tracker := newTrackers[entry.hash]
			tracker.count++
			newTrackers[entry.hash] = tracker
		}
	}

	// preserve message IDs for entries still in window
	for hash, oldTracker := range history.trackers {
		if tracker, exists := newTrackers[hash]; exists {
			tracker.messageIDs = oldTracker.messageIDs
			newTrackers[hash] = tracker
		}
	}

	// add new entry
	filtered = append(filtered, hashEntry{hash: msgHash, time: now})
	tracker := newTrackers[msgHash]
	tracker.count++
	tracker.messageIDs = append(tracker.messageIDs, messageID)
	newTrackers[msgHash] = tracker

	// limit entries per user to prevent memory exhaustion
	if len(filtered) > d.maxEntriesPerUser {
		// remove oldest entries, keeping only the most recent ones
		startIdx := len(filtered) - d.maxEntriesPerUser
		// update trackers for removed entries
		for i := 0; i < startIdx; i++ {
			if t, exists := newTrackers[filtered[i].hash]; exists {
				t.count--
				if t.count == 0 {
					delete(newTrackers, filtered[i].hash)
				} else {
					newTrackers[filtered[i].hash] = t
				}
			}
		}
		filtered = filtered[startIdx:]
	}

	tracker = newTrackers[msgHash]
	count = tracker.count

	// if threshold reached, prepare extra IDs for deletion (excluding current message)
	if count >= d.threshold {
		if len(tracker.messageIDs) > 1 {
			// return all IDs except the current one (last in the list)
			extraIDs = make([]int, len(tracker.messageIDs)-1)
			copy(extraIDs, tracker.messageIDs[:len(tracker.messageIDs)-1])
			// clear the IDs to prevent re-deletion
			tracker.messageIDs = nil
			newTrackers[msgHash] = tracker
		}
	}

	history.entries = filtered
	history.trackers = newTrackers

	// update cache with modified history
	d.cache.Set(userID, history, d.window*2)

	return count, extraIDs
}

// hash calculates sha256 hash of a message
func (d *duplicateDetector) hash(msg string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(msg)))
}

// performCleanup removes expired entries for all users (must be called with lock held)
func (d *duplicateDetector) performCleanup(now time.Time) {
	// cache handles TTL-based eviction automatically
	// we just need to clean expired entries within each user's history
	cutoff := now.Add(-d.window)

	keys := d.cache.Keys()
	for _, userID := range keys {
		history, found := d.cache.Get(userID)
		if !found {
			continue
		}

		// clean old entries
		// create a new slice to avoid sharing underlying array
		filtered := make([]hashEntry, 0, len(history.entries))
		newTrackers := make(map[string]hashTracker)

		for _, entry := range history.entries {
			if entry.time.After(cutoff) {
				filtered = append(filtered, entry)
				tracker := newTrackers[entry.hash]
				tracker.count++
				newTrackers[entry.hash] = tracker
			}
		}

		// preserve message IDs for entries still in window
		for hash, oldTracker := range history.trackers {
			if tracker, exists := newTrackers[hash]; exists {
				tracker.messageIDs = oldTracker.messageIDs
				newTrackers[hash] = tracker
			}
		}

		if len(filtered) == 0 {
			// remove user from cache if no entries left
			d.cache.Invalidate(userID)
		} else {
			history.entries = filtered
			history.trackers = newTrackers
			// update cache with cleaned history
			d.cache.Set(userID, history, d.window*2)
		}
	}
}
