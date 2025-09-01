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
	hashCounts map[string]int   // hash -> count for O(1) lookup
	entries    []hashEntry      // for time-based cleanup
	messageIDs map[string][]int // hash -> message IDs for deletion
}

type hashEntry struct {
	hash string
	time time.Time
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
			hashCounts: make(map[string]int),
			entries:    make([]hashEntry, 0),
			messageIDs: make(map[string][]int),
		}
	}

	// initialize messageIDs map if nil (for backward compatibility)
	if history.messageIDs == nil {
		history.messageIDs = make(map[string][]int)
	}

	// clean old entries
	cutoff := now.Add(-d.window)
	// create a new slice to avoid sharing underlying array
	filtered := make([]hashEntry, 0, len(history.entries)+1)
	newHashCounts := make(map[string]int)
	newMessageIDs := make(map[string][]int)

	for _, entry := range history.entries {
		if entry.time.After(cutoff) {
			filtered = append(filtered, entry)
			newHashCounts[entry.hash]++
		}
	}

	// preserve message IDs for entries still in window (do this separately to avoid duplicates)
	for hash, ids := range history.messageIDs {
		if newHashCounts[hash] > 0 {
			newMessageIDs[hash] = ids
		}
	}

	// add new entry
	filtered = append(filtered, hashEntry{hash: msgHash, time: now})
	newHashCounts[msgHash]++
	newMessageIDs[msgHash] = append(newMessageIDs[msgHash], messageID)

	// limit entries per user to prevent memory exhaustion
	if len(filtered) > d.maxEntriesPerUser {
		// remove oldest entries, keeping only the most recent ones
		startIdx := len(filtered) - d.maxEntriesPerUser
		// update hash counts and message IDs for removed entries
		for i := 0; i < startIdx; i++ {
			newHashCounts[filtered[i].hash]--
			if newHashCounts[filtered[i].hash] == 0 {
				delete(newHashCounts, filtered[i].hash)
				delete(newMessageIDs, filtered[i].hash)
			}
		}
		filtered = filtered[startIdx:]
	}

	count = newHashCounts[msgHash]

	// if threshold reached, prepare extra IDs for deletion (excluding current message)
	if count >= d.threshold {
		if ids, ok := newMessageIDs[msgHash]; ok && len(ids) > 1 {
			// return all IDs except the current one (last in the list)
			extraIDs = make([]int, len(ids)-1)
			copy(extraIDs, ids[:len(ids)-1])
			// clear the IDs to prevent re-deletion
			newMessageIDs[msgHash] = nil
		}
	}

	history.entries = filtered
	history.hashCounts = newHashCounts
	history.messageIDs = newMessageIDs

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
		newHashCounts := make(map[string]int)

		for _, entry := range history.entries {
			if entry.time.After(cutoff) {
				filtered = append(filtered, entry)
				newHashCounts[entry.hash]++
			}
		}

		if len(filtered) == 0 {
			// remove user from cache if no entries left
			d.cache.Invalidate(userID)
		} else {
			history.entries = filtered
			history.hashCounts = newHashCounts
			// update cache with cleaned history
			d.cache.Set(userID, history, d.window*2)
		}
	}
}
