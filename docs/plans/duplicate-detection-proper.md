# Minimal Implementation: Duplicate Detection and Aggressive Cleanup

## Architecture Analysis

The detector and locator are separate components:
- **Detector** (`lib/tgspam`): Core spam detection logic
- **Locator** (`app/storage`): Message storage and retrieval
- **Listener** (`app/events`): Has access to both detector and locator

The detector already has a pattern for similar checks - it checks messages against stored spam samples for similarity.

## Solution

### 1. Duplicate Detection Check

Since the detector already stores message history in `hamHistory` and `spamHistory`, we can add a duplicate check that uses this existing mechanism.

**Add to `lib/tgspam/detector.go`** (25 lines):

```go
// Add to Config struct:
DuplicateThreshold int // if > 0, check for duplicate messages

// Add this check in Check() method after multi-lang check (around line 220):
if d.DuplicateThreshold > 0 && req.UserID != "" {
    cr = append(cr, d.isDuplicate(req))
}

// New method:
func (d *Detector) isDuplicate(req spamcheck.Request) spamcheck.Response {
    normalizedMsg := d.cleanText(req.Msg)
    count := 0
    
    // Check recent history (both ham and spam)
    // Note: This uses circular buffers with limited capacity (HistorySize),
    // so duplicates beyond the buffer window won't be detected
    for _, hist := range []spamcheck.LastRequest{d.hamHistory, d.spamHistory} {
        for _, r := range hist.Get() {
            if r.UserID == req.UserID && d.cleanText(r.Msg) == normalizedMsg {
                count++
            }
        }
    }
    
    if count >= d.DuplicateThreshold {
        return spamcheck.Response{
            Name:    "duplicate",
            Spam:    true,
            Details: fmt.Sprintf("%d duplicates in recent history", count),
        }
    }
    
    return spamcheck.Response{
        Name:    "duplicate",
        Spam:    false,
        Details: fmt.Sprintf("%d/%d", count, d.DuplicateThreshold),
    }
}
```

**Configuration** in `app/main.go`:
```go
// Add to Options:
DuplicateThreshold int `long:"duplicate-threshold" default:"0" description:"duplicate message count to trigger spam"`

// Add to detectorConfig:
DuplicateThreshold: opts.DuplicateThreshold,
```

### 2. Aggressive Cleanup

**Add to `app/storage/locator.go`** (12 lines):
```go
// GetUserMessageIDs returns recent message IDs from a user
func (l *Locator) GetUserMessageIDs(ctx context.Context, userID int64, limit int) ([]int, error) {
    rows, err := l.db.QueryContext(ctx,
        "SELECT msg_id FROM messages WHERE user_id = ? AND gid = ? ORDER BY time DESC LIMIT ?",
        userID, l.gid, limit)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    var ids []int
    for rows.Next() {
        var id int
        if err := rows.Scan(&id); err == nil {
            ids = append(ids, id)
        }
    }
    return ids, rows.Err()
}
```

**Extend Locator interface in `app/events/events.go`**:
```go
type Locator interface {
    // ... existing methods ...
    GetUserMessageIDs(ctx context.Context, userID int64, limit int) ([]int, error)
}
```

**Modify `app/events/admin.go`** (40 lines):

Add to admin struct:
```go
aggressiveCleanup bool
deleteRateLimit   int // messages per second for cleanup
```

In `DirectSpamReport()` after ban (line 344):
```go
if a.aggressiveCleanup && !a.dry {
    // Cleanup in background
    go func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
        defer cancel()
        
        ids, err := a.locator.GetUserMessageIDs(ctx, origMsg.From.ID, 1000)
        if err != nil {
            log.Printf("[WARN] failed to get user messages: %v", err)
            return
        }
        
        if len(ids) == 0 {
            return
        }
        
        deleted := 0
        failed := 0
        interval := time.Second / time.Duration(a.deleteRateLimit)
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        
        for _, msgID := range ids {
            select {
            case <-ctx.Done():
                log.Printf("[INFO] cleanup cancelled after %d deletions", deleted)
                return
            case <-ticker.C:
                _, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{
                    BaseChatMessage: tbapi.BaseChatMessage{
                        MessageID:  msgID,
                        ChatConfig: tbapi.ChatConfig{ChatID: a.primChatID},
                    },
                })
                if err != nil {
                    failed++
                    log.Printf("[DEBUG] failed to delete message %d: %v", msgID, err)
                } else {
                    deleted++
                }
            }
        }
        
        log.Printf("[INFO] cleanup complete: deleted %d messages, failed %d, user %d", 
            deleted, failed, origMsg.From.ID)
    }()
}
```

**Configuration**:
```go
// Add to Options:
AggressiveCleanup bool `long:"aggressive-cleanup" description:"delete all messages from banned user"`
DeleteRateLimit   int  `long:"delete-rate-limit" default:"25" description:"messages per second for cleanup"`

// Pass to admin when creating:
aggressiveCleanup: opts.AggressiveCleanup,
deleteRateLimit:   opts.DeleteRateLimit,
```

### 3. Database Index

```sql
CREATE INDEX idx_messages_user_lookup ON messages(gid, user_id, time DESC);
```

## Summary

- **Duplicate detection**: Uses existing message history, no new storage needed
- **Aggressive cleanup**: Simple async deletion with rate limiting
- **Total new code**: ~85 lines
- **No new dependencies or interfaces**
- **Follows existing patterns** (similar to how similarity check works)
- **Both features are opt-in via CLI flags**

## Improvements from Review

1. **Added comment** about history window limitation in duplicate detection
2. **Made deletion rate configurable** via `--delete-rate-limit` flag (default 25 msgs/sec)
3. **Added error logging** for failed deletions with debug-level detail
4. **Track and report** both successful and failed deletions in final log message