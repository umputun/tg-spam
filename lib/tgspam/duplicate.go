package tgspam

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
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
	hash      string
	time      time.Time
	messageID int
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

	if strings.TrimSpace(req.Msg) == "" {
		return spamcheck.Response{Name: "duplicate", Spam: false, Details: "empty message skipped"}
	}

	userID, err := strconv.ParseInt(req.UserID, 10, 64)
	if err != nil {
		return spamcheck.Response{Name: "duplicate", Spam: false, Details: "invalid user id"}
	}

	count, extraIDs, isEdit := d.trackMessage(userID, req.Msg, req.Meta.MessageID)

	// if this is an edit of an already-handled message, don't re-trigger spam detection
	if isEdit {
		return spamcheck.Response{Name: "duplicate", Spam: false, Details: "message edit"}
	}

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

// trackMessage tracks a message and returns the count of duplicates, extra message IDs to delete, and whether this is an edit
func (d *duplicateDetector) trackMessage(userID int64, msg string, messageID int) (count int, extraIDs []int, isEdit bool) {
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

	// rebuild trackers with only non-expired entries (including messageIDs from entries)
	filtered, newTrackers := d.filterExpiredEntries(history.entries, now)

	// handle content-changing edits: find and remove the old entry with this messageID
	// this ensures each messageID only belongs to one hash (current content)
	if messageID > 0 {
		for i, entry := range filtered {
			if entry.messageID == messageID && entry.hash != msgHash {
				// found the old entry for this messageID with different content
				oldHash := entry.hash
				// remove this specific entry from filtered
				filtered = append(filtered[:i], filtered[i+1:]...)
				// update tracker for old hash
				if t, exists := newTrackers[oldHash]; exists {
					t.messageIDs = d.removeMessageID(t.messageIDs, messageID)
					t.count--
					if t.count == 0 {
						delete(newTrackers, oldHash)
					} else {
						newTrackers[oldHash] = t
					}
				}
				break
			}
		}
	}

	// detect same-content edit by scanning filtered entries (source of truth)
	// don't rely on tracker.messageIDs as it may be capped and missing old IDs
	tracker := newTrackers[msgHash]
	isEdit = false
	if messageID > 0 {
		for _, entry := range filtered {
			if entry.messageID == messageID && entry.hash == msgHash {
				isEdit = true
				break
			}
		}
	}

	// only count as duplicate if it's not an edit
	if !isEdit {
		// add new entry with messageID
		filtered = append(filtered, hashEntry{hash: msgHash, time: now, messageID: messageID})
		tracker.count++
		// messageID will be added to tracker when we rebuild from entries
		// for now, add it directly since we haven't saved to cache yet
		if messageID > 0 {
			tracker.messageIDs = append(tracker.messageIDs, messageID)
			// cap at min(maxEntriesPerUser, 100) to prevent excessive memory
			maxIDs := min(d.maxEntriesPerUser, 100)
			if len(tracker.messageIDs) > maxIDs {
				tracker.messageIDs = tracker.messageIDs[len(tracker.messageIDs)-maxIDs:]
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
	filtered, newTrackers = d.limitEntries(filtered, newTrackers)

	tracker = newTrackers[msgHash]
	count = tracker.count

	// if threshold reached, prepare extra IDs for deletion (excluding current message)
	// skip if this is an edit - we already handled this message
	if count >= d.threshold && !isEdit {
		if len(tracker.messageIDs) > 1 {
			// return all IDs except the current one (last in the list)
			extraIDs = make([]int, len(tracker.messageIDs)-1)
			copy(extraIDs, tracker.messageIDs[:len(tracker.messageIDs)-1])
			// note: we don't clear messageIDs here as they're needed for edit detection
			// the listener handles deletion errors gracefully if messages are already deleted
		}
	}

	history.entries = filtered
	history.trackers = newTrackers

	// update cache with modified history
	d.cache.Set(userID, history, d.window*2)

	return count, extraIDs, isEdit
}

// filterExpiredEntries removes entries older than window and rebuilds trackers from entries
func (d *duplicateDetector) filterExpiredEntries(in []hashEntry, now time.Time) (res []hashEntry, trk map[string]hashTracker) {
	cutoff := now.Add(-d.window)
	res = make([]hashEntry, 0, len(in)+1)
	trk = make(map[string]hashTracker)

	for _, entry := range in {
		if !entry.time.After(cutoff) {
			continue
		}
		res = append(res, entry)
		t := trk[entry.hash]
		t.count++
		// build messageIDs from entries
		if entry.messageID > 0 {
			t.messageIDs = append(t.messageIDs, entry.messageID)
		}
		// preserve first seen timestamp
		if t.firstSeen.IsZero() {
			t.firstSeen = entry.time
		}
		t.lastSeen = entry.time
		trk[entry.hash] = t
	}

	return res, trk
}

// removeMessageID removes messageID from slice if present
func (d *duplicateDetector) removeMessageID(messageIDs []int, messageID int) []int {
	for i, id := range messageIDs {
		if id == messageID {
			// remove by copying remaining elements
			return append(messageIDs[:i], messageIDs[i+1:]...)
		}
	}
	return messageIDs
}

// limitEntries enforces maxEntriesPerUser limit by removing oldest entries
func (d *duplicateDetector) limitEntries(in []hashEntry, m map[string]hashTracker) (res []hashEntry, out map[string]hashTracker) {
	if len(in) <= d.maxEntriesPerUser {
		return in, m
	}

	// remove oldest entries, keeping only the most recent ones
	startIdx := len(in) - d.maxEntriesPerUser
	// update trackers for removed entries and remove their messageIDs
	for i := range startIdx {
		if t, exists := m[in[i].hash]; exists {
			t.count--
			// remove messageID from tracker to keep it aligned with entries
			if in[i].messageID > 0 {
				t.messageIDs = d.removeMessageID(t.messageIDs, in[i].messageID)
			}
			if t.count == 0 {
				delete(m, in[i].hash)
			} else {
				m[in[i].hash] = t
			}
		}
	}
	in = in[startIdx:]

	// recalculate firstSeen for remaining entries
	seenHashes := make(map[string]bool)
	for _, entry := range in {
		if t, exists := m[entry.hash]; exists {
			if !seenHashes[entry.hash] {
				// this is the first occurrence of this hash in the trimmed entries
				t.firstSeen = entry.time
				m[entry.hash] = t
				seenHashes[entry.hash] = true
			}
		}
	}

	return in, m
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
	keys := d.cache.Keys()
	for _, userID := range keys {
		history, found := d.cache.Get(userID)
		if !found {
			continue
		}

		// use shared filtering logic (rebuilds trackers with messageIDs from entries)
		filtered, newTrackers := d.filterExpiredEntries(history.entries, now)

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
