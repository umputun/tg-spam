---
description: 'Create implementation plan for issue #336 - restrict reporting to approved users and add auto-ban threshold'
execution-mode: sequential
---

# Implement Report Enhancements (Issue #336)

## Context

This prompt creates a detailed implementation plan for two user spam reporting enhancements requested in issue #336:

**Feature 1: Approved-Only Reporting**
- Restrict `/report` command to approved users only
- Prevents malicious users from abusing the report system
- Configurable via `--report.approved-only` flag (default: false for backward compatibility)

**Feature 2: Auto-Ban Threshold**
- Add optional threshold for automatic action without admin approval
- When N reports received, automatically delete message and ban user
- Must respect `--soft-ban` mode (restrict vs permanent ban)
- Configurable via `--report.auto-ban-threshold` flag (default: 0 = disabled)

**Current Implementation:**
- Any user can use `/report` command (app/events/reports.go:39-126)
- Threshold triggers admin notification with manual approve/reject buttons
- Admin clicks "✅ Approve Ban" to permanently ban reported user
- Config in Report struct (app/main.go:130-135) with flags: enabled, threshold, rate-limit, rate-period

## Instructions

Create a comprehensive implementation plan covering:

### 1. CLI Flag Additions

**File:** `app/main.go`

Add two new fields to the `Report` struct (lines 130-135):
```go
Report struct {
    Enabled         bool          `long:"enabled" env:"ENABLED" description:"enable user spam reporting"`
    Threshold       int           `long:"threshold" env:"THRESHOLD" default:"2" description:"number of reports to trigger admin notification"`
    ApprovedOnly    bool          `long:"approved-only" env:"APPROVED_ONLY" description:"restrict reporting to approved users only"`
    AutoBanThreshold int          `long:"auto-ban-threshold" env:"AUTO_BAN_THRESHOLD" default:"0" description:"auto-ban after N reports (0=disabled, must be >= threshold)"`
    RateLimit       int           `long:"rate-limit" env:"RATE_LIMIT" default:"10" description:"max reports per user per period"`
    RatePeriod      time.Duration `long:"rate-period" env:"RATE_PERIOD" default:"1h" description:"rate limit time period"`
}
```

### 2. Config Propagation

**File:** `app/events/reports.go`

Update `ReportConfig` struct (lines 16-23):
```go
type ReportConfig struct {
    Storage          Reports
    Enabled          bool
    Threshold        int
    ApprovedOnly     bool          // new field
    AutoBanThreshold int           // new field
    RateLimit        int
    RatePeriod       time.Duration
}
```

**File:** `app/main.go`

Update config mapping (around line 355-361):
```go
ReportConfig: events.ReportConfig{
    Storage:          reportsStore,
    Enabled:          opts.Report.Enabled,
    Threshold:        opts.Report.Threshold,
    ApprovedOnly:     opts.Report.ApprovedOnly,     // new
    AutoBanThreshold: opts.Report.AutoBanThreshold, // new
    RateLimit:        opts.Report.RateLimit,
    RatePeriod:       opts.Report.RatePeriod,
},
```

### 3. Feature 1: Approved-Only Reporter Check

**File:** `app/events/reports.go`

Add check in `DirectUserReport()` function after line 64 (after super-user checks):
```go
// validate reporter is approved user if approved-only mode enabled
if r.ApprovedOnly && !r.bot.IsApprovedUser(update.Message.From.ID) {
    log.Printf("[INFO] report rejected: reporter %d (%s) not in approved list",
        update.Message.From.ID, update.Message.From.UserName)
    // still delete the /report command to keep chat clean
    _, _ = r.tbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
        MessageID:  update.Message.MessageID,
        ChatConfig: tbapi.ChatConfig{ChatID: r.primChatID},
    }})
    return fmt.Errorf("reporter %d not in approved list", update.Message.From.ID)
}
```

### 4. Feature 2: Auto-Ban Logic

**File:** `app/events/reports.go`

Modify `checkReportThreshold()` function (lines 154-186) to handle auto-ban:

```go
func (r *userReports) checkReportThreshold(ctx context.Context, msgID int, chatID int64) error {
    if r.Storage == nil {
        return fmt.Errorf("reports storage not initialized")
    }

    // query all reports for this message
    reports, err := r.Storage.GetByMessage(ctx, msgID, chatID)
    if err != nil {
        return fmt.Errorf("failed to get reports: %w", err)
    }

    reportCount := len(reports)

    // check if auto-ban threshold reached
    if r.AutoBanThreshold > 0 && reportCount >= r.AutoBanThreshold {
        log.Printf("[INFO] auto-ban threshold reached for msgID:%d, chatID:%d: %d reports (threshold: %d)",
            msgID, chatID, reportCount, r.AutoBanThreshold)
        return r.executeAutoBan(ctx, reports)
    }

    // check if manual approval threshold reached
    if reportCount < r.Threshold {
        log.Printf("[DEBUG] report threshold not reached for msgID:%d, chatID:%d: %d < %d",
            msgID, chatID, reportCount, r.Threshold)
        return nil
    }

    log.Printf("[INFO] report threshold reached for msgID:%d, chatID:%d: %d reports",
        msgID, chatID, reportCount)

    // check if admin notification already sent
    if len(reports) > 0 && reports[0].NotificationSent {
        // notification already sent, update it
        log.Printf("[DEBUG] updating existing notification for msgID:%d, admin_msg_id:%d",
            msgID, reports[0].AdminMsgID)
        return r.updateReportNotification(ctx, reports)
    }

    // no notification sent yet, send new one
    log.Printf("[DEBUG] sending new notification for msgID:%d", msgID)
    return r.sendReportNotification(ctx, reports)
}
```

Add new `executeAutoBan()` method (similar to `callbackReportBan` but automatic):

```go
// executeAutoBan executes automatic ban when auto-ban threshold is reached
func (r *userReports) executeAutoBan(ctx context.Context, reports []storage.Report) error {
    if len(reports) == 0 {
        return fmt.Errorf("no reports provided")
    }

    // extract info from first report
    firstReport := reports[0]
    msgID := firstReport.MsgID
    chatID := firstReport.ChatID
    reportedUserID := firstReport.ReportedUserID
    reportedUserName := firstReport.ReportedUserName
    msgText := firstReport.MsgText

    log.Printf("[INFO] executing auto-ban for user %d (%s) based on %d reports",
        reportedUserID, reportedUserName, len(reports))

    // remove user from approved list
    if remErr := r.bot.RemoveApprovedUser(reportedUserID); remErr != nil {
        log.Printf("[DEBUG] can't remove user %d from approved list: %v", reportedUserID, remErr)
    }

    // update spam samples with message text (if not empty)
    if !r.dry && msgText != "" {
        if spamErr := r.bot.UpdateSpam(msgText); spamErr != nil {
            log.Printf("[WARN] failed to update spam samples: %v", spamErr)
        }
    }

    // delete reported message from primary chat
    if !r.dry {
        _, err := r.tbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
            MessageID:  msgID,
            ChatConfig: tbapi.ChatConfig{ChatID: chatID},
        }})
        if err != nil {
            log.Printf("[WARN] failed to delete reported message %d: %v", msgID, err)
        } else {
            log.Printf("[INFO] reported message %d auto-deleted", msgID)
        }
    }

    // ban reported user - CRITICAL: respect soft-ban mode
    banReq := banRequest{
        duration: bot.PermanentBanDuration,
        userID:   reportedUserID,
        chatID:   chatID,
        tbAPI:    r.tbAPI,
        dry:      r.dry,
        training: r.trainingMode,
        userName: reportedUserName,
        restrict: r.softBanMode, // IMPORTANT: use soft-ban if enabled
    }
    if err := banUserOrChannel(banReq); err != nil {
        log.Printf("[WARN] failed to auto-ban user %d: %v", reportedUserID, err)
    }

    // handle admin notification - update existing or send new
    // CRITICAL: only delete reports if notification succeeds, otherwise admin callbacks will fail
    var notificationErr error
    if r.adminChatID != 0 {
        if len(reports) > 0 && reports[0].NotificationSent {
            // notification already sent (manual threshold reached earlier), update it
            notificationErr = r.updateNotificationForAutoBan(ctx, reports)
        } else {
            // no previous notification, send new one
            notificationErr = r.sendAutoBanNotification(reports)
        }

        if notificationErr != nil {
            log.Printf("[WARN] failed to send/update auto-ban notification: %v", notificationErr)
            // don't delete reports - keep them so admin callbacks still work if buttons remain
            // reports will be cleaned up by cleanupOldReports() after 7 days if never resolved
            return fmt.Errorf("auto-ban executed for user %d but notification failed: %w", reportedUserID, notificationErr)
        }
    }

    // delete all reports for this message ONLY if notification succeeded (or no admin chat configured)
    if err := r.Storage.DeleteByMessage(ctx, msgID, chatID); err != nil {
        log.Printf("[WARN] failed to delete reports for msgID:%d: %v", msgID, err)
    }

    log.Printf("[INFO] auto-ban executed for user %d by %d reports", reportedUserID, len(reports))
    return nil
}
```

Add `sendAutoBanNotification()` method to inform admin of automatic action:

```go
// sendAutoBanNotification sends notification to admin chat about automatic ban
func (r *userReports) sendAutoBanNotification(reports []storage.Report) error {
    if len(reports) == 0 {
        return fmt.Errorf("no reports provided")
    }

    firstReport := reports[0]
    reportedUserID := firstReport.ReportedUserID
    reportedUserName := firstReport.ReportedUserName

    // escape message text for markdown
    msgText := strings.ReplaceAll(escapeMarkDownV1Text(firstReport.MsgText), "\n", " ")
    msgText = truncateString(msgText, 200, "...")

    // format reporter list
    reporterList := make([]string, 0, len(reports))
    for _, report := range reports {
        reporterName := report.ReporterUserName
        if reporterName == "" {
            reporterName = fmt.Sprintf("user%d", report.ReporterUserID)
        }
        reporterList = append(reporterList, fmt.Sprintf("- [%s](tg://user?id=%d)",
            escapeMarkDownV1Text(reporterName), report.ReporterUserID))
    }

    actionType := "banned"
    if r.softBanMode {
        actionType = "restricted"
    }

    notificationText := fmt.Sprintf("**Auto-%s user after %d reports**\n\n[%s](tg://user?id=%d)\n\n%s\n\n**Reporters:**\n%s",
        actionType,
        len(reports),
        escapeMarkDownV1Text(reportedUserName),
        reportedUserID,
        msgText,
        strings.Join(reporterList, "\n"))

    // send to admin chat (no buttons - action already taken)
    tbMsg := tbapi.NewMessage(r.adminChatID, notificationText)
    tbMsg.ParseMode = tbapi.ModeMarkdown
    tbMsg.LinkPreviewOptions = tbapi.LinkPreviewOptions{IsDisabled: true}

    if _, err := r.tbAPI.Send(tbMsg); err != nil {
        return fmt.Errorf("failed to send auto-ban notification: %w", err)
    }

    log.Printf("[INFO] auto-ban notification sent to admin chat for user %d", reportedUserID)
    return nil
}
```

Add `updateNotificationForAutoBan()` method to update existing admin notification when auto-ban executes:

```go
// updateNotificationForAutoBan updates existing admin notification when auto-ban is executed
// this prevents leaving orphaned notifications with live buttons that would fail on click
func (r *userReports) updateNotificationForAutoBan(ctx context.Context, reports []storage.Report) error {
    if len(reports) == 0 {
        return fmt.Errorf("reports list is empty")
    }

    firstReport := reports[0]
    adminMsgID := firstReport.AdminMsgID
    if adminMsgID == 0 {
        return fmt.Errorf("admin message ID is 0, cannot update")
    }

    reportedUserID := firstReport.ReportedUserID
    reportedUserName := firstReport.ReportedUserName

    // escape message text for markdown
    msgText := strings.ReplaceAll(escapeMarkDownV1Text(firstReport.MsgText), "\n", " ")
    msgText = truncateString(msgText, 200, "...")

    // format reporter list
    reporterList := make([]string, 0, len(reports))
    for _, report := range reports {
        reporterName := report.ReporterUserName
        if reporterName == "" {
            reporterName = fmt.Sprintf("user%d", report.ReporterUserID)
        }
        reporterList = append(reporterList, fmt.Sprintf("- [%s](tg://user?id=%d)",
            escapeMarkDownV1Text(reporterName), report.ReporterUserID))
    }

    actionType := "banned"
    if r.softBanMode {
        actionType = "restricted"
    }

    // create updated notification text with auto-ban confirmation
    updatedText := fmt.Sprintf("**User spam reported (%d reports)**\n\n[%s](tg://user?id=%d)\n\n%s\n\n**Reporters:**\n%s\n\n_auto-%s after reaching %d reports_",
        len(reports),
        escapeMarkDownV1Text(reportedUserName),
        reportedUserID,
        msgText,
        strings.Join(reporterList, "\n"),
        actionType,
        len(reports))

    // edit existing admin message, remove buttons
    editMsg := tbapi.NewEditMessageText(r.adminChatID, adminMsgID, updatedText)
    editMsg.ParseMode = "Markdown"
    editMsg.LinkPreviewOptions = tbapi.LinkPreviewOptions{IsDisabled: true}
    editMsg.ReplyMarkup = &tbapi.InlineKeyboardMarkup{InlineKeyboard: [][]tbapi.InlineKeyboardButton{}} // remove buttons

    if _, err := r.tbAPI.Send(editMsg); err != nil {
        return fmt.Errorf("failed to update notification for auto-ban (msgID:%d, adminMsgID:%d): %w",
            firstReport.MsgID, adminMsgID, err)
    }

    log.Printf("[INFO] updated admin notification %d for auto-ban of user %d", adminMsgID, reportedUserID)
    return nil
}
```

**File:** `app/events/reports.go`

Update `userReports` struct to include soft-ban mode (around line 26-35):
```go
type userReports struct {
    ReportConfig
    tbAPI        TbAPI
    bot          Bot
    locator      Locator
    superUsers   SuperUsers
    primChatID   int64
    adminChatID  int64
    trainingMode bool
    softBanMode  bool  // new field
    dry          bool
}
```

**File:** `app/events/listener.go`

Wire `softBanMode` when creating userReports handler (around line 117-122):
```go
l.reportsHandler = &userReports{
    ReportConfig: l.ReportConfig,
    tbAPI:        l.TbAPI, bot: l.Bot, locator: l.Locator, superUsers: l.SuperUsers,
    primChatID: l.chatID, adminChatID: l.adminChatID,
    trainingMode: l.TrainingMode, softBanMode: l.SoftBanMode, dry: l.Dry,  // add softBanMode
}
```

**CRITICAL FIX #1:** Update `cleanupOldReports()` to prevent memory leak from failed notification updates

**File:** `app/storage/reports.go`

Update `cleanupOldReports()` method (around line 254-273) to delete ALL old reports:
```go
// cleanupOldReports removes ALL reports older than 7 days.
// retention period: 7 days - includes reports that:
//   - never reached threshold (notification_sent = false)
//   - reached threshold but notification update failed (notification_sent = true but orphaned)
//   - admin hasn't acted on within retention period
func (r *Reports) cleanupOldReports(ctx context.Context) error {
    const retentionDays = 7
    // removed notification_sent filter - delete ALL old reports to prevent memory leak
    query := r.Adopt("DELETE FROM reports WHERE gid = ? AND report_time < ?")
    cutoffTime := time.Now().Add(-retentionDays * 24 * time.Hour)

    result, err := r.ExecContext(ctx, query, r.GID(), cutoffTime)
    if err != nil {
        return fmt.Errorf("failed to cleanup old reports: %w", err)
    }

    if rowsAffected, err := result.RowsAffected(); err == nil && rowsAffected > 0 {
        log.Printf("[DEBUG] cleaned up %d old reports (retention: %d days)", rowsAffected, retentionDays)
    }

    return nil
}
```

**Why this fix is necessary:**
- Without it: reports with `notification_sent = true` that fail to update accumulate forever
- Scenario: manual threshold reached → notification sent → auto-ban fails to update → reports kept but never cleaned
- Original cleanup only removed `notification_sent = false` reports
- New cleanup removes ALL reports older than 7 days regardless of notification status
- Prevents database memory leak while maintaining 7-day grace period for admin action

**BONUS BUG FIX:** Fix existing `callbackReportBan` to respect soft-ban mode (currently broken)

**File:** `app/events/reports.go`

Update `callbackReportBan()` method (around line 382-393) to include `restrict` field:
```go
// ban reported user permanently (or restrict if soft-ban enabled)
banReq := banRequest{
    duration: bot.PermanentBanDuration,
    userID:   reportedUserID,
    chatID:   chatID,
    tbAPI:    r.tbAPI,
    dry:      r.dry,
    training: r.trainingMode,
    userName: reportedUserName,
    restrict: r.softBanMode,  // ADD THIS LINE - respect soft-ban mode
}
if err := banUserOrChannel(banReq); err != nil {
    log.Printf("[WARN] failed to ban user %d: %v", reportedUserID, err)
}
```

### 5. Validation Logic

Add validation in `main.go` after parsing options (around line 200):
```go
// validate auto-ban threshold
if opts.Report.AutoBanThreshold > 0 && opts.Report.AutoBanThreshold < opts.Report.Threshold {
    log.Fatalf("[ERROR] auto-ban-threshold (%d) must be >= threshold (%d) or 0 (disabled)",
        opts.Report.AutoBanThreshold, opts.Report.Threshold)
}
```

### 6. Test Requirements

**New test file:** `app/events/reports_approved_test.go`
- Test approved-only mode rejects non-approved users
- Test approved users can still report when approved-only enabled
- Test backward compatibility (approved-only=false allows any user)

**New test file:** `app/events/reports_autoban_test.go`
- Test auto-ban triggers at correct threshold
- Test auto-ban respects soft-ban mode (restrict vs ban)
- Test auto-ban deletes message and removes from approved list
- Test auto-ban updates spam samples
- Test auto-ban sends admin notification when no prior notification exists
- Test auto-ban UPDATES existing notification (when manual threshold was reached first)
  - Scenario: threshold=2, auto-ban=5, verify at 5 reports the admin message is edited (not new message sent)
  - Verify buttons are removed from updated notification
  - Verify updated text shows "auto-banned after N reports"
- Test manual approval still works when count < auto-ban threshold
- Test validation: auto-ban-threshold >= threshold
- **Test notification failure handling (CRITICAL)**:
  - Mock updateNotificationForAutoBan to return error
  - Verify reports are NOT deleted when notification fails
  - Verify auto-ban still executed (user banned, message deleted)
  - Verify error returned indicating notification failure
  - Verify admin callbacks still work (reports still in DB)

**Existing test file update:** `app/events/reports_test.go`
- Add test for callbackReportBan respecting soft-ban mode (bug fix verification)

**Existing test file update:** `app/storage/reports_test.go`
- **Test cleanup deletes ALL old reports (CRITICAL for memory leak prevention)**:
  - Create reports with `notification_sent = false` and age > 7 days → verify deleted
  - Create reports with `notification_sent = true` and age > 7 days → verify deleted
  - Create reports with `notification_sent = true` and age < 7 days → verify NOT deleted
  - Verify cleanup doesn't depend on notification status, only age
  - Verify cleanup is called from Add() after inserting report

### 7. Documentation Updates

**File:** `README.md`

Update "User Spam Reporting" section and "All Application Options" section with new flags:
```
--report.approved-only      restrict reporting to approved users only (default: false)
--report.auto-ban-threshold auto-ban after N reports, 0=disabled (default: 0)
```

Add usage examples:
```bash
# Only allow approved users to report spam
tg-spam --report.approved-only

# Auto-ban after 5 reports (threshold=2 for notification, 5 for auto-action)
tg-spam --report.threshold=2 --report.auto-ban-threshold=5

# Auto-ban with soft-ban mode (restrict instead of ban)
tg-spam --soft-ban --report.auto-ban-threshold=3
```

### 8. Implementation Order

1. Add CLI flags and config propagation (steps 1-2)
2. Add validation logic (step 5)
3. **CRITICAL**: Fix `cleanupOldReports()` to prevent memory leak (remove notification_sent filter)
4. Write tests for cleanup fix (verify ALL old reports deleted regardless of notification status)
5. Implement Feature 1: approved-only check (step 3)
6. Write tests for Feature 1
7. Implement Feature 2: auto-ban logic with notification error handling (step 4)
8. Write tests for Feature 2, including critical notification failure test
9. Fix existing callbackReportBan soft-ban bug
10. Update documentation (step 7)
11. Run full test suite: `go test -race ./...`
12. Run linter: `golangci-lint run`
13. Manual testing with different threshold combinations
14. **Manual testing of notification failure scenario**:
    - Temporarily break Telegram API connection
    - Trigger auto-ban
    - Verify reports NOT deleted
    - Verify admin callbacks still work

## Expected Output

The plan should result in:
- Two new CLI flags with proper defaults for backward compatibility
- Feature 1 working: non-approved users blocked from reporting when enabled
- Feature 2 working: automatic ban at threshold with soft-ban support
- Comprehensive tests covering both features and edge cases
- Updated README with clear usage examples
- All tests passing and linter clean

## Notes

- Auto-ban threshold must be >= regular threshold (or 0 to disable)
- Soft-ban mode MUST be respected in auto-ban (use `restrict: r.softBanMode` in banRequest)
- Keep backward compatibility: approved-only=false, auto-ban-threshold=0 by default
- Admin notification handling is critical:
  - If manual threshold reached first (e.g., threshold=2), notification sent with buttons
  - When auto-ban triggers later (e.g., auto-ban=5), MUST update existing notification
  - Prevents orphaned notifications with live buttons that fail when clicked
  - Update removes buttons and adds "auto-banned after N reports" text
- **Error handling for notification failures**:
  - If admin chat configured and notification update/send fails: keep reports in DB
  - This ensures admin callbacks still work if buttons remain active
  - Reports will be cleaned up when next report arrives (cleanup runs in Add())
  - **CRITICAL**: cleanup must delete ALL reports older than 7 days, not just unsent ones
  - Without this, reports with `notification_sent = true` accumulate (memory leak)
  - Auto-ban execution continues (user banned, message deleted) even if notification fails
  - Error returned to signal notification failure but ban succeeded
- **Cleanup mechanism**:
  - `cleanupOldReports()` called from `Reports.Add()` (existing pattern)
  - Deletes ALL reports older than 7 days (regardless of notification_sent status)
  - Simple, no background goroutines or lifecycle management
  - Works for most scenarios (groups with some activity)
  - Very quiet groups (no reports for 7+ days) may accumulate a few orphaned rows - acceptable tradeoff for simplicity
- Consider logging carefully to distinguish auto-ban from admin-approved ban
- Bonus bug fix: existing `callbackReportBan` also doesn't respect soft-ban mode, fix it
