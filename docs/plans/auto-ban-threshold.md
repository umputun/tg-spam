# Auto-Ban Threshold for User Spam Reporting

## Overview

This feature adds an optional `AutoBanThreshold` configuration to user spam reporting that enables automatic banning of reported users when a specified number of reports is reached, without requiring admin approval.

### Problem Statement

Currently, all user spam reports require admin intervention:
1. Users report spam messages with `/report`
2. When threshold (default: 2) is reached, admin receives notification with buttons
3. Admin must manually click "Approve Ban" to ban the user

This creates manual overhead for obvious spam cases where multiple users agree.

### Proposed Solution

Add `AutoBanThreshold` parameter (default: 0/disabled) that works alongside existing `Threshold`:
- `Threshold`: Number of reports to trigger admin notification (existing behavior)
- `AutoBanThreshold`: Number of reports to automatically ban user (new feature)

When `AutoBanThreshold > 0` and reached:
- Automatically ban the reported user permanently
- Delete the reported message
- Update spam samples with message text
- Send informational message to admin chat (no action buttons)
- Delete all reports from database

Both thresholds can work together (e.g., notify at 3, auto-ban at 5) or independently.

### Key Benefits

1. **Reduces admin workload**: Obvious spam with multiple reports handled automatically
2. **Faster response**: Spam removed immediately upon reaching threshold
3. **Flexible configuration**: Can be disabled (0), set equal to Threshold for immediate auto-ban, or set higher for escalation
4. **Maintains oversight**: Admin still notified via info message
5. **Backward compatible**: Default 0 preserves existing behavior

## Technical Details

### Configuration Structure

```go
// app/main.go - CLI options
Report struct {
    Enabled          bool          `long:"enabled" env:"ENABLED" description:"enable user spam reporting"`
    Threshold        int           `long:"threshold" env:"THRESHOLD" default:"2" description:"number of reports to trigger admin notification"`
    AutoBanThreshold int           `long:"auto-ban-threshold" env:"AUTO_BAN_THRESHOLD" default:"0" description:"auto-ban user at this threshold (0=disabled)"`
    RateLimit        int           `long:"rate-limit" env:"RATE_LIMIT" default:"10" description:"max reports per user per period"`
    RatePeriod       time.Duration `long:"rate-period" env:"RATE_PERIOD" default:"1h" description:"rate limit time period"`
}

// app/events/reports.go - Runtime config
type ReportConfig struct {
    Storage          Reports
    Enabled          bool
    Threshold        int           // admin notification threshold
    AutoBanThreshold int           // auto-ban threshold (0=disabled)
    RateLimit        int
    RatePeriod       time.Duration
}
```

### Processing Flow

#### Current Flow (reports.go:153-186 checkReportThreshold)
```
1. Get all reports for message
2. If count < Threshold → return (do nothing)
3. If count >= Threshold:
   - If notification already sent → update it
   - If not sent → send new notification with buttons
```

#### New Flow (with AutoBanThreshold)
```
1. Get all reports for message
2. If AutoBanThreshold > 0 AND count >= AutoBanThreshold:
   → Execute auto-ban flow (new function)
   → return
3. If count < Threshold → return (do nothing)
4. If count >= Threshold:
   - If notification already sent → update it
   - If not sent → send new notification with buttons
```

### Auto-Ban Execution Flow

New function `autoBanReportedUser(ctx, reports)` will:

```
1. Re-query reports from storage (idempotency check for race conditions)
   - If no reports found → return nil (already processed by concurrent call)
   - This prevents duplicate bans/notifications from concurrent reports
2. Extract info from reports (msgID, chatID, reportedUserID, reportedUserName, msgText)
3. Delete stale admin notification (if exists):
   - Check if reports[0].AdminMsgID > 0
   - If yes, delete that message using tbAPI.Request(DeleteMessageConfig)
   - This removes the notification with action buttons from admin chat
4. Remove user from approved list (bot.RemoveApprovedUser)
5. Update spam samples if msgText not empty (bot.UpdateSpam)
6. Delete reported message from chat (tbAPI.Request DeleteMessageConfig)
7. Ban user permanently (banUserOrChannel with full banRequest structure):
   - duration: bot.PermanentBanDuration
   - userID, chatID, userName: from reports
   - tbAPI: r.tbAPI
   - dry: r.dry
   - training: r.trainingMode
8. Delete all reports from database (Storage.DeleteByMessage)
9. Send info message to admin chat:
   - Format: "User auto-banned after N reports"
   - Include: reported user, message preview, reporter list
   - No action buttons (just FYI notification)
   - Always send (whether or not prior notification existed)
```

This mirrors callbackReportBan (reports.go:336-410) logic but:
- Adds idempotency check (step 1)
- Deletes stale admin notifications (step 3)
- Always sends new info message (step 9)
- No callback query handling

### Example Scenarios

#### Scenario 1: Auto-ban only (AutoBanThreshold=3, Threshold=5)
- 1st report → nothing happens
- 2nd report → nothing happens
- 3rd report → AUTO-BAN (user banned, admin gets info message)
- Note: Threshold never reached, no admin action notification

#### Scenario 2: Notification first, then auto-ban (Threshold=2, AutoBanThreshold=4)
- 1st report → nothing happens
- 2nd report → Admin notification sent with buttons
- 3rd report → Admin notification updated (3 reports now)
- 4th report → AUTO-BAN (user banned, previous notification deleted, new info message sent)

#### Scenario 3: Same threshold (Threshold=3, AutoBanThreshold=3)
- 1st report → nothing happens
- 2nd report → nothing happens
- 3rd report → AUTO-BAN (user banned immediately, admin gets info message)
- Note: Admin notification logic never executes (auto-ban runs first)

#### Scenario 4: Disabled (AutoBanThreshold=0, Threshold=2)
- Current behavior: admin notifications at threshold, no auto-ban

## Implementation Iterations

### Iteration 1: Configuration & Structure Updates
- [ ] Add `AutoBanThreshold` CLI parameter to `Report` struct in app/main.go:130-135
  - Type: `int`
  - Long flag: `--auto-ban-threshold`
  - Env var: `REPORT_AUTO_BAN_THRESHOLD`
  - Default: `0` (disabled)
  - Description: "auto-ban user at this threshold (0=disabled)"
- [ ] Add `AutoBanThreshold` field to `ReportConfig` struct in app/events/reports.go:16-23
  - Add comment explaining it works alongside Threshold
- [ ] Pass `AutoBanThreshold` from main to ReportConfig initialization in app/main.go
  - Add to ReportConfig initialization: `AutoBanThreshold: opts.Report.AutoBanThreshold`
- [ ] Run tests to verify config loading: `go test ./app/...`

### Iteration 2: Core Auto-Ban Logic
- [ ] Create `autoBanReportedUser` function in app/events/reports.go (after line 332)
  - Signature: `func (r *userReports) autoBanReportedUser(ctx context.Context, reports []storage.Report) error`
  - **Step 1: Idempotency check** (handles race conditions from concurrent reports)
    - Re-query reports using `r.Storage.GetByMessage(ctx, msgID, chatID)`
    - If no reports found, return nil (already processed by concurrent call)
    - Use fresh reports for remaining logic
  - **Step 2: Extract data** from reports (msgID, chatID, reportedUserID, reportedUserName, msgText)
  - **Step 3: Delete stale admin notification** (if prior notification exists)
    - Check if `reports[0].AdminMsgID > 0`
    - If yes, delete that message using `r.tbAPI.Request(tbapi.DeleteMessageConfig{...})`
    - Delete from admin chat (r.adminChatID) with AdminMsgID
    - Log deletion (may fail if message already deleted - non-critical)
  - **Step 4: Remove user from approved list** using `r.bot.RemoveApprovedUser(reportedUserID)`
  - **Step 5: Update spam samples** if `!r.dry && msgText != ""` using `r.bot.UpdateSpam(msgText)`
  - **Step 6: Delete reported message** using `r.tbAPI.Request(tbapi.DeleteMessageConfig{...})`
  - **Step 7: Ban user permanently** using `banUserOrChannel(banRequest{...})` with:
    - `duration: bot.PermanentBanDuration`
    - `userID: reportedUserID`
    - `chatID: chatID`
    - `tbAPI: r.tbAPI`
    - `dry: r.dry`
    - `training: r.trainingMode`
    - `userName: reportedUserName`
  - **Step 8: Delete all reports** using `r.Storage.DeleteByMessage(ctx, msgID, chatID)`
  - Return error if critical operations fail
- [ ] Implement auto-ban info notification (within autoBanReportedUser)
  - Always send new info message (even if prior notification existed and was deleted)
  - Format message: "**User auto-banned** (N reports)"
  - Include reported user link: `[username](tg://user?id=userID)`
  - Include message preview (escaped, truncated to 200 chars)
  - Include reporter list (same format as regular notifications)
  - Add padding for full-width display (U+2800 × 30)
  - No inline keyboard buttons (info only)
  - Send to r.adminChatID if configured
- [ ] Add logging for auto-ban events
  - Log when auto-ban threshold reached (before idempotency check)
  - Log if idempotency check exits early (concurrent processing detected)
  - Log if stale notification deleted (or if deletion failed)
  - Log ban execution result
  - Log new info notification sent to admin

### Iteration 3: Threshold Check Integration
- [ ] Modify `checkReportThreshold` function in app/events/reports.go:153-186
  - After getting reports from storage (line 160-163)
  - Before existing threshold check (line 166)
  - Add auto-ban check:
    ```go
    // check auto-ban threshold first (if enabled and reached)
    if r.AutoBanThreshold > 0 && len(reports) >= r.AutoBanThreshold {
        log.Printf("[INFO] auto-ban threshold reached for msgID:%d: %d >= %d",
            msgID, len(reports), r.AutoBanThreshold)
        return r.autoBanReportedUser(ctx, reports)
    }
    ```
  - This ensures auto-ban executes before admin notification logic
  - Existing threshold logic remains unchanged
- [ ] Run tests to verify threshold logic: `go test -v -run TestCheckReportThreshold ./app/events/`

### Iteration 4: Testing
- [ ] Add test for auto-ban threshold reached in app/events/reports_test.go
  - Test name: `TestAutoBanThreshold_Reached`
  - Setup: AutoBanThreshold=3, Threshold=5
  - Create 3 reports for same message
  - Verify autoBanReportedUser executed
  - Verify user banned permanently
  - Verify message deleted
  - Verify reports deleted from database
  - Verify admin notification sent (info format, no buttons)
  - Verify spam samples updated if message text present
- [ ] Add test for auto-ban disabled (AutoBanThreshold=0)
  - Test name: `TestAutoBanThreshold_Disabled`
  - Setup: AutoBanThreshold=0, Threshold=2
  - Create 2 reports
  - Verify normal admin notification sent (with buttons)
  - Verify no auto-ban executed
- [ ] Add test for auto-ban and notification both active
  - Test name: `TestAutoBanThreshold_WithNotification`
  - Setup: Threshold=2, AutoBanThreshold=4
  - Create 2 reports → verify notification sent
  - Add 2 more reports → verify auto-ban executed at 4
- [ ] Add test for auto-ban equals notification threshold
  - Test name: `TestAutoBanThreshold_SameAsNotification`
  - Setup: Threshold=3, AutoBanThreshold=3
  - Create 3 reports → verify auto-ban executed (not notification)
- [ ] Add test for dry mode with auto-ban
  - Test name: `TestAutoBanThreshold_DryMode`
  - Setup: dry=true, AutoBanThreshold=2
  - Create 2 reports
  - Verify ban/delete operations skipped
  - Verify notification still sent
- [ ] Add test for race condition / concurrent auto-ban (idempotency)
  - Test name: `TestAutoBanThreshold_RaceCondition`
  - Setup: AutoBanThreshold=2
  - Create 2 reports to trigger auto-ban
  - Simulate concurrent call by calling autoBanReportedUser twice with same reports
  - First call should execute fully
  - Second call should exit early (idempotency check finds no reports)
  - Verify only one ban attempt, one notification sent
  - Verify no errors from second call
- [ ] Add test for stale notification handling
  - Test name: `TestAutoBanThreshold_StaleNotification`
  - Setup: Threshold=2, AutoBanThreshold=4
  - Create 2 reports → admin notification sent with AdminMsgID=123
  - Create 2 more reports → trigger auto-ban at 4 reports
  - Verify existing admin message (ID=123) deleted from admin chat
  - Verify NEW info notification sent (separate from deleted one)
  - Verify user banned, reported message deleted, reports deleted
- [ ] Run full test suite: `go test -race ./...`
- [ ] Verify test coverage for new code: `go test -cover ./app/events/`

### Iteration 5: Documentation Updates
- [ ] Update README.md "All Application Options" section
  - Add `--report.auto-ban-threshold` with description and default (0)
  - Insert after `--report.threshold` line
  - Ensure format matches other options
- [ ] Update README.md "User Spam Reporting" section
  - Add paragraph explaining AutoBanThreshold feature
  - Include example configurations:
    - Auto-ban only: `--report.auto-ban-threshold=3 --report.threshold=999`
    - Both active: `--report.threshold=2 --report.auto-ban-threshold=5`
    - Disabled: `--report.auto-ban-threshold=0` (default)
  - Explain behavior: automatic ban on threshold, info message to admin
- [ ] Run formatters and linter:
  - `~/.dot-files/claude/format.sh`
  - `golangci-lint run`
- [ ] Final verification:
  - All tests pass: `go test -race ./...`
  - Linter clean: `golangci-lint run`
  - Coverage adequate: `go test -cover ./app/events/`

## Testing Checklist

### Unit Tests
- [ ] Auto-ban triggered at exact threshold
- [ ] Auto-ban not triggered below threshold
- [ ] Auto-ban disabled when AutoBanThreshold=0
- [ ] Auto-ban performs full ban behavior (ban + delete + spam update)
- [ ] Auto-ban sends info notification to admin chat
- [ ] Auto-ban info notification has no buttons
- [ ] Auto-ban works with dry mode (notification sent, ban/delete skipped)
- [ ] Both thresholds can work together (notification then auto-ban)
- [ ] Auto-ban takes precedence when both thresholds equal
- [ ] Reports deleted after auto-ban
- [ ] Spam samples updated when message text present
- [ ] Approved user removed from list during auto-ban
- [ ] Idempotency check prevents duplicate bans from concurrent reports
- [ ] Second concurrent auto-ban call exits cleanly without errors
- [ ] Stale admin notification deleted when auto-ban fires
- [ ] New info notification always sent (even when stale notification existed)

### Integration Tests
- [ ] Test with real storage backend (SQLite)
- [ ] Test with mock Telegram API
- [ ] Verify admin chat receives correct message format
- [ ] Verify reported message deleted from primary chat

### Backward Compatibility
- [ ] Default AutoBanThreshold=0 preserves existing behavior
- [ ] Existing report notifications unchanged when AutoBanThreshold=0
- [ ] Existing tests still pass without modifications

## Data Structures

### No Database Changes Required
The existing `reports` table and `Report` struct in storage package require no modifications. All auto-ban logic uses existing fields and methods.

### New Fields Summary
- `opts.Report.AutoBanThreshold` (int) - CLI option in main.go
- `ReportConfig.AutoBanThreshold` (int) - Runtime config in events.go

## Error Handling

### Critical Errors (halt execution)
- Failed to parse callback data
- Failed to get reports from database
- No reports found for message

### Non-Critical Errors (log and continue)
- Failed to remove from approved list
- Failed to update spam samples
- Failed to delete reported message (may already be deleted)
- Failed to delete stale admin notification (may already be deleted)
- Failed to send admin notification (ban still executed)

### Dry Mode Behavior
- Skip: ban user, delete message, update spam samples
- Execute: send admin notification, database operations

## Logging Requirements

- `[INFO]` when auto-ban threshold reached: include msgID, report count, threshold
- `[INFO]` when user auto-banned: include userID, username, msgID
- `[INFO]` when admin notification sent: include msgID, chatID, report count
- `[WARN]` for non-critical failures: include operation and error
- `[DEBUG]` for detailed flow tracking: include decision points

## Edge Cases

1. **AutoBanThreshold < Threshold**: Auto-ban happens before notification (valid use case)
2. **AutoBanThreshold = Threshold**: Auto-ban takes precedence (check happens first in code)
3. **AutoBanThreshold > Threshold**: Notification sent first, auto-ban later (valid escalation)
4. **AutoBanThreshold = 0**: Feature disabled, existing behavior (default)
5. **Concurrent auto-ban calls**: Idempotency check in autoBanReportedUser re-queries storage; second call finds no reports and exits cleanly
6. **Stale admin notification**: When Threshold reached before AutoBanThreshold, auto-ban deletes existing notification and sends new info message
7. **Message already deleted**: DeleteMessage fails gracefully (logged as warning)
8. **Admin chat not configured**: Info notification skipped (logged as debug)
9. **Dry mode**: Ban/delete skipped but notification sent

## Future Enhancements (Not in Scope)

- Configurable auto-ban duration (currently always permanent)
- Different auto-ban behavior based on report count tiers
- Whitelist of users immune to auto-ban
- Auto-ban notification with "undo" button (would require significant refactoring)
- Auto-ban history/audit log in database
