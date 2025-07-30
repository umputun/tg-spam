# Plan: Duplicate Message Detection and Aggressive Spam Cleanup

## Problem Statement

Currently, the tg-spam bot has two critical issues:

1. **Approved users can bypass spam detection with duplicate messages**: Once a user is approved (after sending a certain number of non-spam messages), they can send the same spam message multiple times without being detected. The example shows user "FilippSimonov_6" posting identical spam messages multiple times.

2. **Limited cleanup capability**: When an admin uses the `/spam` command, only the specific replied-to message is deleted. All other messages from the spammer remain in the chat history.

## Root Cause Analysis

### Issue 1: Duplicate Message Detection Gap
- The current spam detection flow checks approved users only when `FirstMessageOnly` is enabled AND they haven't reached `FirstMessagesCount`
- Once approved, users can post identical content repeatedly
- No mechanism exists to detect duplicate/repeated messages from the same user

### Issue 2: Single Message Deletion
- The `/spam` command in `DirectSpamReport()` only deletes the message that was replied to
- Telegram Bot API doesn't provide bulk deletion by user
- Each message must be deleted individually using its message ID

## Proposed Solution

### 1. Duplicate Message Detection System

#### 1.1 New MetaCheck Function
Create a new check in `lib/tgspam/metachecks.go`:

```go
// DuplicateCheck returns a MetaCheck that detects duplicate messages from the same user
func DuplicateCheck(threshold int, windowMinutes int, locator MessageHistoryProvider) MetaCheck {
    return func(req spamcheck.Request) spamcheck.Response {
        // Check if user has sent this exact message recently
        // Count occurrences within the time window
        // Return spam=true if count >= threshold
    }
}
```

#### 1.2 Message History Provider Interface
Define interface in `lib/spamcheck/types.go`:

```go
type MessageHistoryProvider interface {
    // GetRecentMessagesByUser returns recent messages from a user within time window
    GetRecentMessagesByUser(ctx context.Context, userID int64, since time.Time) ([]MessageInfo, error)
    
    // GetAllMessagesByUser returns all messages from a user (limited)
    GetAllMessagesByUser(ctx context.Context, userID int64, limit int) ([]MessageInfo, error)
}

type MessageInfo struct {
    Hash      string
    Text      string
    MessageID int
    Timestamp time.Time
}
```

#### 1.3 Locator Extensions
Extend `app/storage/locator.go` with new methods:

```go
// GetRecentMessagesByUser retrieves messages from a user within a time window
func (l *Locator) GetRecentMessagesByUser(ctx context.Context, userID int64, since time.Time) ([]MessageInfo, error) {
    query := `SELECT hash, msg_id, time FROM messages 
              WHERE user_id = ? AND time > ? AND gid = ?
              ORDER BY time DESC`
    // Implementation details...
}

// GetAllMessagesByUser retrieves all messages from a user
func (l *Locator) GetAllMessagesByUser(ctx context.Context, userID int64, limit int) ([]MessageInfo, error) {
    query := `SELECT hash, msg_id, time FROM messages 
              WHERE user_id = ? AND gid = ?
              ORDER BY time DESC LIMIT ?`
    // Implementation details...
}
```

#### 1.4 Integration with Detector
Update `lib/tgspam/detector.go` Check() method:

```go
// Add after line 184 (emoji check)
if d.DuplicateThreshold > 0 {
    cr = append(cr, d.checkDuplicates(req))
}
```

### 2. Aggressive Spam Cleanup Mode

#### 2.1 Configuration
Add to `app/main.go` Options struct:

```go
AggressiveSpam bool `long:"aggressive-spam" description:"delete all messages from spammer on /spam command"`
```

#### 2.2 Admin Handler Enhancement
Modify `app/events/admin.go`:

```go
type admin struct {
    // ... existing fields ...
    aggressiveSpam bool
}

// Add new method for bulk deletion
func (a *admin) deleteUserMessages(userID int64, limit int) (deleted int, err error) {
    // Get all messages from user
    messages, err := a.locator.GetAllMessagesByUser(context.TODO(), userID, limit)
    if err != nil {
        return 0, err
    }
    
    // Delete with rate limiting (30 msgs/second max)
    rateLimiter := time.NewTicker(35 * time.Millisecond)
    defer rateLimiter.Stop()
    
    for _, msg := range messages {
        <-rateLimiter.C
        _, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{
            BaseChatMessage: tbapi.BaseChatMessage{
                MessageID:  msg.MessageID,
                ChatConfig: tbapi.ChatConfig{ChatID: a.primChatID},
            },
        })
        if err == nil {
            deleted++
        }
    }
    
    return deleted, nil
}
```

Update `DirectSpamReport()` to use bulk deletion:

```go
// After line 344 (ban user)
if a.aggressiveSpam && !a.dry {
    deleted, err := a.deleteUserMessages(origMsg.From.ID, 1000)
    if err != nil {
        errs = multierror.Append(errs, fmt.Errorf("failed to delete user messages: %w", err))
    } else if deleted > 0 {
        log.Printf("[INFO] deleted %d messages from user %d", deleted, origMsg.From.ID)
        // Notify admin chat
        notifyMsg := fmt.Sprintf("_deleted %d messages from user_", deleted)
        if err := send(tbapi.NewMessage(a.adminChatID, notifyMsg), a.tbAPI); err != nil {
            log.Printf("[WARN] failed to send deletion notification: %v", err)
        }
    }
}
```

### 3. Configuration Parameters

Add to SpamConfig:

```go
type SpamConfig struct {
    // ... existing fields ...
    
    // Duplicate detection
    DuplicateThreshold      int           `long:"duplicate-threshold" default:"3" description:"number of duplicate messages to trigger spam"`
    DuplicateWindowMinutes  int           `long:"duplicate-window" default:"60" description:"time window in minutes for duplicate detection"`
    
    // Aggressive cleanup
    AggressiveSpam         bool          `long:"aggressive-spam" description:"delete all messages from spammer on /spam command"`
    AggressiveSpamLimit    int           `long:"aggressive-spam-limit" default:"1000" description:"max messages to delete in aggressive mode"`
}
```

### 4. Implementation Timeline

1. **Phase 1**: Implement duplicate detection (1-2 days)
   - Create MessageHistoryProvider interface
   - Extend Locator with history methods
   - Implement DuplicateCheck
   - Integrate with Detector

2. **Phase 2**: Implement aggressive cleanup (1 day)
   - Add configuration flags
   - Implement bulk deletion with rate limiting
   - Add admin notifications

3. **Phase 3**: Testing and refinement (1 day)
   - Unit tests for new components
   - Integration testing
   - Performance testing with large message counts

### 5. Safety Considerations

1. **Rate Limiting**: Telegram API limits to ~30 deletions/second
2. **Database Performance**: Add indexes for user_id + time queries
3. **Memory Usage**: Limit number of messages fetched at once
4. **Dry Run Support**: All features work in dry mode without actual deletions
5. **Logging**: Comprehensive logging of all bulk operations
6. **Rollback**: Messages can't be recovered after deletion - consider adding confirmation

### 6. Future Enhancements

1. **Pattern Detection**: Detect slight variations in spam messages
2. **Batch Operations**: Use Telegram's batch API when available
3. **Admin UI**: Web interface for reviewing bulk deletions
4. **Metrics**: Track duplicate detection effectiveness

## Migration Notes

- No database schema changes required (using existing messages table)
- Backward compatible - all new features are opt-in
- Default configuration maintains current behavior

## Testing Strategy

1. **Unit Tests**:
   - Test DuplicateCheck with various scenarios
   - Test Locator history methods
   - Test rate limiting in bulk deletion

2. **Integration Tests**:
   - Test duplicate detection with real message flow
   - Test aggressive cleanup with mock Telegram API
   - Test edge cases (user not found, no messages, etc.)

3. **Manual Testing**:
   - Test with test Telegram group
   - Verify rate limiting doesn't trigger API errors
   - Confirm admin notifications work correctly