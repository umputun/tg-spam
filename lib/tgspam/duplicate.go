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
	count      int       // number of duplicates
	messageIDs []int     // message IDs for deletion
	firstSeen  time.Time // timestamp of first occurrence
	lastSeen   time.Time // timestamp of last occurrence
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
		msgHash := d.hash(req.Msg)
		return spamcheck.Response{
			Name:           "duplicate",
			Spam:           true,
			Details:        fmt.Sprintf("message repeated %d times in %s", count, d.formatDuration(userID, msgHash)),
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
		if !entry.time.After(cutoff) {
			continue
		}
		filtered = append(filtered, entry)
		tracker := newTrackers[entry.hash]
		tracker.count++
		// preserve first seen timestamp
		if tracker.firstSeen.IsZero() {
			tracker.firstSeen = entry.time
		}
		tracker.lastSeen = entry.time
		newTrackers[entry.hash] = tracker
	}

	// NOTE: We preserve messageIDs for a given hash but limit them to prevent unbounded growth.
	// we keep at most (threshold-1) + current message IDs since that's all we need for deletion.
	maxMessageIDs := d.threshold
	if maxMessageIDs > 100 {
		maxMessageIDs = 100 // cap at 100 to prevent excessive memory usage
	}
	for hash, oldTracker := range history.trackers {
		if tracker, exists := newTrackers[hash]; exists {
			tracker.messageIDs = oldTracker.messageIDs
			// limit the number of message IDs we keep
			if len(tracker.messageIDs) > maxMessageIDs {
				// keep only the most recent IDs
				tracker.messageIDs = tracker.messageIDs[len(tracker.messageIDs)-maxMessageIDs:]
			}
			// preserve timestamps from old tracker if not set
			if tracker.firstSeen.IsZero() && !oldTracker.firstSeen.IsZero() {
				tracker.firstSeen = oldTracker.firstSeen
			}
			newTrackers[hash] = tracker
		}
	}

	// check if this messageID already exists for this hash (indicates an edit)
	tracker := newTrackers[msgHash]
	isEdit := false
	if messageID > 0 {
		for _, existingID := range tracker.messageIDs {
			if existingID == messageID {
				isEdit = true
				break
			}
		}
	}

	// only count as duplicate if it's not an edit
	if !isEdit {
		// add new entry
		filtered = append(filtered, hashEntry{hash: msgHash, time: now})
		tracker.count++
		// only append valid message IDs
		if messageID > 0 {
			tracker.messageIDs = append(tracker.messageIDs, messageID)
			// limit the number of message IDs after adding the new one
			maxMessageIDs := d.threshold
			if maxMessageIDs > 100 {
				maxMessageIDs = 100 // cap at 100 to prevent excessive memory usage
			}
			if len(tracker.messageIDs) > maxMessageIDs {
				// keep only the most recent IDs
				tracker.messageIDs = tracker.messageIDs[len(tracker.messageIDs)-maxMessageIDs:]
			}
		}
		// update timestamps
		if tracker.firstSeen.IsZero() {
			tracker.firstSeen = now
		}
	}
	// always update lastSeen, even for edits
	tracker.lastSeen = now
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

		// recalculate firstSeen for remaining entries
		seenHashes := make(map[string]bool)
		for _, entry := range filtered {
			if t, exists := newTrackers[entry.hash]; exists {
				if !seenHashes[entry.hash] {
					// this is the first occurrence of this hash in the trimmed entries
					t.firstSeen = entry.time
					newTrackers[entry.hash] = t
					seenHashes[entry.hash] = true
				}
			}
		}
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

// formatDuration returns the duration between first and last occurrence of a duplicate
func (d *duplicateDetector) formatDuration(userID int64, msgHash string) string {
	// get user history to find the tracker
	history, found := d.cache.Get(userID)
	if !found {
		return d.window.String() // fallback to window if history not found
	}

	tracker, exists := history.trackers[msgHash]
	if !exists || tracker.firstSeen.IsZero() || tracker.lastSeen.IsZero() {
		return d.window.String() // fallback to window if timestamps not set
	}

	duration := tracker.lastSeen.Sub(tracker.firstSeen)
	// if duration is 0 (all duplicates at same time), show "instantly"
	if duration == 0 {
		return "instantly"
	}

	// round to seconds for cleaner output
	return duration.Round(time.Second).String()
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
			if !entry.time.After(cutoff) {
				continue
			}
			filtered = append(filtered, entry)
			tracker := newTrackers[entry.hash]
			tracker.count++
			// preserve first seen timestamp
			if tracker.firstSeen.IsZero() {
				tracker.firstSeen = entry.time
			}
			tracker.lastSeen = entry.time
			newTrackers[entry.hash] = tracker
		}

		// NOTE: Same rationale as above — keep all known messageIDs for a hash
		// to allow deletion of all duplicates, beyond the time window.
		for hash, oldTracker := range history.trackers {
			if tracker, exists := newTrackers[hash]; exists {
				tracker.messageIDs = oldTracker.messageIDs
				// preserve timestamps from old tracker if not set
				if tracker.firstSeen.IsZero() && !oldTracker.firstSeen.IsZero() {
					tracker.firstSeen = oldTracker.firstSeen
				}
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
