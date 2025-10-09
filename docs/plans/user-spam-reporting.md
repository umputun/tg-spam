# User Spam Reporting Feature

## Overview

this feature allows regular users (non-admins) to report spam messages using the `/report` command. When users report a message:

1. the `/report` command message is immediately deleted to keep the main chat clean
2. reports are tracked and accumulated per message
3. when the report threshold is reached, admins are notified with action buttons
4. admins can approve the ban, reject the report, or ban a reporter for false reporting
5. multiple reports for the same message update the same admin notification message (no duplicates)

**Benefits:**
- empowers community to help moderate spam
- reduces admin workload by flagging suspicious messages
- provides protection against abuse via admin approval and reporter banning
- maintains clean chat by immediately deleting report commands

**Integration:**
- extends existing admin notification system with new button actions
- reuses existing Locator storage pattern with new reports table
- follows existing `/spam` command patterns for consistency

## Technical Details

### Data Structures

#### New Reports Table Schema
will be in new file `storage/reports.go` with its own QueryMap:

```go
// reports-related command constants
const (
    CmdCreateReportsTable engine.DBCmd = iota + 500
    CmdCreateReportsIndexes
    CmdAddReport
    CmdGetReportsByMessage
    CmdGetReporterCountSince
    CmdUpdateReportsAdminMsgID
    CmdDeleteReporter
    CmdDeleteReportsByMessage
)

// reportsQueries holds all reports-related queries
var reportsQueries = engine.NewQueryMap().
    Add(CmdCreateReportsTable, engine.Query{
        Sqlite: `CREATE TABLE IF NOT EXISTS reports (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            msg_id INTEGER,
            chat_id INTEGER,
            gid TEXT NOT NULL DEFAULT '',
            reporter_user_id INTEGER,
            reporter_user_name TEXT,
            reported_user_id INTEGER,
            reported_user_name TEXT,
            msg_text TEXT,
            report_time TIMESTAMP,
            admin_msg_id INTEGER DEFAULT 0,
            UNIQUE(gid, msg_id, chat_id, reporter_user_id)
        )`,
        Postgres: `CREATE TABLE IF NOT EXISTS reports (
            id SERIAL PRIMARY KEY,
            msg_id INTEGER,
            chat_id BIGINT,
            gid TEXT NOT NULL DEFAULT '',
            reporter_user_id BIGINT,
            reporter_user_name TEXT,
            reported_user_id BIGINT,
            reported_user_name TEXT,
            msg_text TEXT,
            report_time TIMESTAMP,
            admin_msg_id INTEGER DEFAULT 0,
            UNIQUE(gid, msg_id, chat_id, reporter_user_id)
        )`,
    }).
    AddSame(CmdCreateReportsIndexes, `
        CREATE INDEX IF NOT EXISTS idx_reports_msg_chat_gid ON reports(gid, msg_id, chat_id);
        CREATE INDEX IF NOT EXISTS idx_reports_admin_msg ON reports(admin_msg_id);
        CREATE INDEX IF NOT EXISTS idx_reports_reporter_time ON reports(reporter_user_id, report_time);
        CREATE INDEX IF NOT EXISTS idx_reports_gid_time ON reports(gid, report_time DESC);
    `)
```

#### Reports Storage Struct (storage/reports.go)
```go
// Reports is a storage for user spam reports
type Reports struct {
    *engine.SQL
    engine.RWLocker
}

// Report represents a spam report from a user
type Report struct {
    ID               int64     `db:"id"`
    MsgID            int       `db:"msg_id"`
    ChatID           int64     `db:"chat_id"`
    GID              string    `db:"gid"`
    ReporterUserID   int64     `db:"reporter_user_id"`
    ReporterUserName string    `db:"reporter_user_name"`
    ReportedUserID   int64     `db:"reported_user_id"`
    ReportedUserName string    `db:"reported_user_name"`
    MsgText          string    `db:"msg_text"`
    ReportTime       time.Time `db:"report_time"`
    AdminMsgID       int       `db:"admin_msg_id"`
}

// NewReports creates a new Reports storage
func NewReports(ctx context.Context, db *engine.SQL) (*Reports, error)
```

#### Reports Interface (events package)
```go
// Reports is an interface for user spam reports storage
type Reports interface {
    Add(ctx context.Context, msgID int, chatID int64, reporterID int64, reporterName string,
        reportedID int64, reportedName string, msgText string) error
    GetByMessage(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error)
    GetReporterCountSince(ctx context.Context, reporterID int64, since time.Time) (int, error)
    UpdateAdminMsgID(ctx context.Context, msgID int, chatID int64, adminMsgID int) error
    DeleteByMessage(ctx context.Context, msgID int, chatID int64) error
    DeleteReporter(ctx context.Context, reporterID int64, msgID int, chatID int64) error
}
```

#### Admin Struct Extensions (events package)
```go
type admin struct {
    // ... existing fields ...
    reports          Reports       // NEW: reports storage interface
    reportThreshold  int           // number of reports to trigger notification
    reportRateLimit  int           // max reports per user per period
    reportRatePeriod time.Duration // rate limit period
}
```

#### Configuration Parameters
```go
// new struct for report-related options
type ReportOpts struct {
    Threshold  int           `long:"threshold" env:"THRESHOLD" default:"2" description:"number of reports to trigger admin notification"`
    RateLimit  int           `long:"rate-limit" env:"RATE_LIMIT" default:"10" description:"max reports per user per period"`
    RatePeriod time.Duration `long:"rate-period" env:"RATE_PERIOD" default:"1h" description:"rate limit time period"`
}

// main options struct
type Options struct {
    // ... existing fields ...
    Report ReportOpts `group:"report" namespace:"report" env-namespace:"REPORT"`
}
```

CLI flags will be:
- `--report.threshold=2`
- `--report.rate-limit=10`
- `--report.rate-period=1h`

Environment variables:
- `REPORT_THRESHOLD=2`
- `REPORT_RATE_LIMIT=10`
- `REPORT_RATE_PERIOD=1h`

### Processing Flow

#### User Reports Message
1. user replies to spam message with `/report`
2. validate: reporter is not super user (admins use `/spam`)
3. validate: reported message is not from super user
4. validate: rate limit check for reporter
5. delete `/report` command message immediately
6. store report in database
7. check if threshold reached
8. if threshold reached and no admin notification exists:
   - create admin notification with buttons
   - store admin message ID in all reports for this message
9. if threshold reached and admin notification exists:
   - update admin notification to add new reporter to list

#### Admin Actions
1. **Approve Ban** (button callback: `R+reportedUserID:msgID`)
   - extract reportedUserID and msgID from callback data
   - get report details from database by msgID to find chatID and message text
   - ban reported user permanently
   - update spam samples with message text
   - delete reported message from primary chat
   - delete all reports for this message
   - update admin notification: "banned by {admin} in {duration}"
   - remove buttons

2. **Reject Report** (button callback: `R-reportedUserID:msgID`)
   - extract reportedUserID and msgID from callback data
   - delete all reports for this message
   - update admin notification: "rejected by {admin} in {duration}"
   - remove buttons

3. **Ban Reporter** (button callback: `R?reportedUserID:msgID`)
   - extract reportedUserID and msgID from callback data
   - get all reports for this message
   - show confirmation with list of reporters as inline buttons
   - each reporter button has callback: `R!reporterID:msgID`
   - add "Cancel" button with callback: `RX+reportedUserID:msgID` (restore original view)
   - update buttons only (use tbapi.NewEditMessageReplyMarkup)

4. **Ban Specific Reporter** (button callback: `R!reporterID:msgID`)
   - extract reporterID and msgID from callback data
   - ban reporter permanently
   - remove reporter from report list in database
   - get remaining reports for this message
   - if all reporters removed:
     - treat report as rejected
     - delete all reports
     - update message: "all reporters banned by {admin} in {duration}"
     - remove buttons
   - if reporters remain:
     - update admin notification to remove banned reporter from list
     - restore original buttons (Approve, Reject, Ban Reporter)

5. **Cancel Ban Reporter** (button callback: `RX+reportedUserID:msgID`)
   - restore original button layout (Approve, Reject, Ban Reporter)
   - update buttons only (use tbapi.NewEditMessageReplyMarkup)

### Callback Data Format
follows existing pattern of `prefix+userID:msgID`:
- Approve: `R+reportedUserID:msgID` (R+ prefix for "Report Ban")
- Reject: `R-reportedUserID:msgID` (R- prefix for "Report Reject")
- Ban Reporter (ask confirmation): `R?reportedUserID:msgID` (R? prefix for "Report Ban Reporter Confirmation")
- Ban Specific Reporter: `R!reporterID:msgID` (R! prefix for "Report Ban This Reporter")
- Cancel: `RX+reportedUserID:msgID` (RX prefix for "Report Cancel", restore original view)

### Admin Notification Format
```
**User spam reported (N reports)**

[reported username](tg://user?id=123456)

message text here...

**Reporters:**
- [reporter1](tg://user?id=111)
- [reporter2](tg://user?id=222)

[Approve Ban] [Reject] [Ban Reporter]
```

After clicking "Ban Reporter", buttons change to:
```
[Ban reporter1] [Ban reporter2]
[Cancel]
```

### Callback Parsing
uses existing `parseCallbackData` method which expects `prefix+userID:msgID` format:
- method in admin.go:798 currently strips prefix (?, +, !)
- needs update to also strip R+, R-, R?, R!, RX prefixes
- parses remaining as `userID:msgID`
- for R! callbacks, userID is reporterID instead of reportedUserID
- returns (userID int64, msgID int, err error)

**Note:** existing parseCallbackData handles single-char prefixes, we use two-char prefixes (R+, R-, etc) to avoid conflicts. The method already handles prefix stripping correctly as it checks `data[:1]` - we just need to check for 'R' first and strip two characters.

### Mode Behaviors
- **Training Mode**: reports still work, admin gets notification, but approval doesn't actually ban
- **Soft Ban Mode**: approval applies soft ban (restrictions)
- **Dry Mode**: reports tracked but no actions taken
- **No Admin Chat**: reports silently fail (no admin to notify)

### Report Cleanup
Reports are deleted when:
- admin approves the ban (all reports for that message deleted)
- admin rejects the report (all reports for that message deleted)
- admin bans all reporters for false reporting (all reports deleted, treated as rejected)
- very old reports that never reached threshold (cleanup after 7 days)

## Iterative Development Plan

### Iteration 1: Database Schema and Storage
- [ ] create new file storage/reports.go
- [ ] add Report struct to storage/reports.go (following DetectedSpamInfo pattern)
  - [ ] all fields with db tags as shown in data structures section
- [ ] add database command constants (use iota + 500 offset):
  - [ ] CmdCreateReportsTable
  - [ ] CmdCreateReportsIndexes
  - [ ] CmdAddReport
  - [ ] CmdGetReportsByMessage
  - [ ] CmdGetReporterCountSince
  - [ ] CmdUpdateReportsAdminMsgID
  - [ ] CmdDeleteReporter
  - [ ] CmdDeleteReportsByMessage
- [ ] create reportsQueries map using engine.NewQueryMap():
  - [ ] CmdCreateReportsTable: Use Add() with different SQLite/Postgres (INTEGER vs BIGINT, AUTOINCREMENT vs SERIAL)
  - [ ] CmdCreateReportsIndexes: Use AddSame() (identical for both)
  - [ ] CmdAddReport: Use AddSame() - INSERT...ON CONFLICT...DO NOTHING
  - [ ] CmdGetReportsByMessage: Use AddSame() - SELECT with WHERE msg_id=? AND chat_id=? AND gid=?
  - [ ] CmdGetReporterCountSince: Use AddSame() - SELECT COUNT(*) WHERE reporter_user_id=? AND report_time > ? AND gid=?
  - [ ] CmdUpdateReportsAdminMsgID: Use AddSame() - UPDATE reports SET admin_msg_id=? WHERE msg_id=? AND chat_id=? AND gid=?
  - [ ] CmdDeleteReporter: Use AddSame() - DELETE WHERE reporter_user_id=? AND msg_id=? AND chat_id=? AND gid=?
  - [ ] CmdDeleteReportsByMessage: Use AddSame() - DELETE WHERE msg_id=? AND chat_id=? AND gid=?
- [ ] implement Reports struct (follows ApprovedUsers/DetectedSpam pattern):
  - [ ] struct with *engine.SQL and engine.RWLocker fields
- [ ] implement NewReports(ctx context.Context, db *engine.SQL) (*Reports, error):
  - [ ] check db != nil
  - [ ] create Reports with db and db.MakeLock()
  - [ ] create TableConfig with Name: "reports", CreateTable, CreateIndexes, QueriesMap
  - [ ] call engine.InitTable
  - [ ] return Reports or error
- [ ] implement Add method:
  - [ ] signature: func (r *Reports) Add(ctx context.Context, msgID int, chatID int64, reporterID int64, reporterName string, reportedID int64, reportedName string, msgText string) error
  - [ ] Lock/defer Unlock
  - [ ] log.Printf("[DEBUG] add report: msgID:%d, chatID:%d, reporter:%d, reported:%d", ...)
  - [ ] get query using reportsQueries.Pick(r.Type(), CmdAddReport)
  - [ ] use NamedExecContext with map containing all fields including gid (r.GID()), report_time (time.Now()), admin_msg_id (0)
  - [ ] return fmt.Errorf("failed to insert report: %w", err) on error
  - [ ] call r.cleanupOldReports(ctx) at end
- [ ] implement GetByMessage method:
  - [ ] signature: func (r *Reports) GetByMessage(ctx context.Context, msgID int, chatID int64) ([]Report, error)
  - [ ] RLock/defer RUnlock
  - [ ] get query using reportsQueries.Pick or r.Adopt
  - [ ] use SelectContext(ctx, &reports, query, msgID, chatID, r.GID())
  - [ ] return []Report{}, nil if no records (not an error)
- [ ] implement GetReporterCountSince method:
  - [ ] signature: func (r *Reports) GetReporterCountSince(ctx context.Context, reporterID int64, since time.Time) (int, error)
  - [ ] RLock/defer RUnlock
  - [ ] query: SELECT COUNT(*) FROM reports WHERE reporter_user_id=? AND report_time > ? AND gid=?
  - [ ] use GetContext to get single int value
- [ ] implement UpdateAdminMsgID method:
  - [ ] signature: func (r *Reports) UpdateAdminMsgID(ctx context.Context, msgID int, chatID int64, adminMsgID int) error
  - [ ] Lock/defer Unlock
  - [ ] UPDATE reports SET admin_msg_id=? WHERE msg_id=? AND chat_id=? AND gid=?
- [ ] implement DeleteReporter method:
  - [ ] signature: func (r *Reports) DeleteReporter(ctx context.Context, reporterID int64, msgID int, chatID int64) error
  - [ ] Lock/defer Unlock
  - [ ] DELETE FROM reports WHERE reporter_user_id=? AND msg_id=? AND chat_id=? AND gid=?
  - [ ] log.Printf("[INFO] reporter %d removed from report msgID:%d, chatID:%d", reporterID, msgID, chatID)
- [ ] implement DeleteByMessage method:
  - [ ] signature: func (r *Reports) DeleteByMessage(ctx context.Context, msgID int, chatID int64) error
  - [ ] Lock/defer Unlock
  - [ ] DELETE FROM reports WHERE msg_id=? AND chat_id=? AND gid=?
  - [ ] log.Printf("[INFO] all reports deleted for msgID:%d, chatID:%d", msgID, chatID)
- [ ] implement cleanupOldReports internal method:
  - [ ] signature: func (r *Reports) cleanupOldReports(ctx context.Context) error
  - [ ] no lock (called from Add which already has lock)
  - [ ] query: DELETE FROM reports WHERE report_time < ? AND gid = ? AND admin_msg_id = 0
  - [ ] use r.Adopt() and ExecContext(ctx, query, time.Now().Add(-7*24*time.Hour), r.GID())
  - [ ] note: admin_msg_id = 0 means notification not sent yet
- [ ] add Reports interface to app/events/events.go (after Locator interface)
- [ ] generate mocks: `go generate ./app/events`
- [ ] write tests in storage/reports_test.go:
  - [ ] TestReports_Add
  - [ ] TestReports_GetByMessage
  - [ ] TestReports_GetReporterCountSince
  - [ ] TestReports_UpdateAdminMsgID
  - [ ] TestReports_DeleteReporter
  - [ ] TestReports_DeleteByMessage
  - [ ] TestReports_cleanupOldReports
- [ ] run tests to verify database operations work
- [ ] mark iteration 1 complete and document any changes from original plan in this section

### Iteration 2: Configuration and Rate Limiting
- [ ] add ReportOpts struct to main options (app/main.go or wherever Options is defined)
  - [ ] Threshold field with tag: `long:"threshold" env:"THRESHOLD" default:"2"`
  - [ ] RateLimit field with tag: `long:"rate-limit" env:"RATE_LIMIT" default:"10"`
  - [ ] RatePeriod field with tag: `long:"rate-period" env:"RATE_PERIOD" default:"1h"`
- [ ] add Report field to main Options struct with tag: `group:"report" namespace:"report" env-namespace:"REPORT"`
- [ ] create Reports storage instance in main/listener initialization:
  - [ ] call storage.NewReports(ctx, db) where db is created
  - [ ] pass reports instance when creating listener/admin
- [ ] add reports, reportThreshold, reportRateLimit, reportRatePeriod fields to admin struct in app/events/admin.go
- [ ] pass configuration values when creating admin struct in app/events/listener.go (reports instance, opts.Report.Threshold, etc)
- [ ] implement checkReportRateLimit method in admin struct (returns bool and error)
  - [ ] call a.reports.GetReporterCountSince with reportRatePeriod
  - [ ] compare count against reportRateLimit
- [ ] add tests for rate limiting logic
- [ ] verify existing functionality still works
- [ ] mark iteration 2 complete and document any changes from original plan in this section

### Iteration 3: Report Command Handler
- [ ] add report handler to admin struct (DirectUserReport method)
- [ ] implement report validation (not super user, not reporting super user)
- [ ] implement rate limit check before accepting report
- [ ] implement immediate deletion of /report message
- [ ] integrate with a.reports.Add to store the report
- [ ] after adding report, call checkReportThreshold to see if notification should be sent
- [ ] add report command detection in listener
- [ ] handle regular user /report separately from admin commands
- [ ] add tests for report command handling
- [ ] verify existing /spam command still works
- [ ] mark iteration 3 complete and document any changes from original plan in this section

### Iteration 4: Report Tracking and Threshold Logic
- [ ] implement checkReportThreshold method in admin struct
- [ ] query all reports for a message
- [ ] check if count >= threshold
- [ ] check if admin notification already sent (check if reports[0].AdminMsgID > 0)
  - [ ] all reports for same message have same admin_msg_id (updated together)
  - [ ] if admin_msg_id > 0, notification already sent, just update it
  - [ ] if admin_msg_id == 0, no notification yet, send new one
- [ ] format reporter list for display
- [ ] add tests for threshold logic
- [ ] verify threshold triggering works correctly
- [ ] mark iteration 4 complete and document any changes from original plan in this section

### Iteration 5: Admin Notification
- [ ] implement sendReportNotification method in admin struct
  - [ ] parameter: reports []storage.Report (assumes all reports are for same message)
  - [ ] extract reported user info from first report (all have same reported user)
  - [ ] format message text (escape markdown using escapeMarkDownV1Text)
  - [ ] format reporter list with markdown links: [username](tg://user?id=123)
  - [ ] create notification text: "User spam reported (N reports)" + reported user + message + reporters
  - [ ] create inline keyboard with 3 buttons using tbapi.NewInlineKeyboardMarkup:
    - [ ] "Approve Ban" → callback: R+reportedUserID:msgID
    - [ ] "Reject" → callback: R-reportedUserID:msgID
    - [ ] "Ban Reporter" → callback: R?reportedUserID:msgID
  - [ ] send to admin chat using tbAPI.Send
  - [ ] get sent message ID from response
  - [ ] call a.reports.UpdateAdminMsgID to store admin message ID
- [ ] add tests for notification formatting
- [ ] verify notification appears in admin chat
- [ ] mark iteration 5 complete and document any changes from original plan in this section

### Iteration 6: Update Existing Notification
- [ ] implement updateReportNotification method in admin struct
- [ ] fetch existing admin message by ID from reports
- [ ] append new reporter to list
- [ ] update message text with new reporter count
- [ ] keep same 3 buttons (no change to button row)
- [ ] edit existing admin message
- [ ] add tests for notification updates
- [ ] verify multiple reports update same message
- [ ] mark iteration 6 complete and document any changes from original plan in this section

### Iteration 7: Admin Callback Handlers - Approve/Reject
- [ ] update parseCallbackData in admin.go to handle two-char prefixes (R+, R-, R?, R!, RX)
  - [ ] check if data starts with 'R' and has length >= 3
  - [ ] strip first 2 characters if R prefix detected
  - [ ] keep existing logic for single-char prefixes
- [ ] add report callback detection in InlineCallbackHandler (check for R+, R-, R?, R!, RX prefixes)
- [ ] implement callbackReportBan method (R+ prefix - approve ban)
  - [ ] parse callback data: reportedUserID, msgID
  - [ ] get reports from database by msgID using a.reports.GetByMessage to find chatID and message text
  - [ ] ban reported user permanently (same as directReport ban logic)
  - [ ] update spam samples with message text
  - [ ] delete reported message from primary chat
  - [ ] delete all reports for this message using a.reports.DeleteByMessage
  - [ ] update admin notification text with "banned by {admin} in {duration}"
  - [ ] remove buttons (empty keyboard)
- [ ] implement callbackReportReject method (R- prefix - reject)
  - [ ] parse callback data: reportedUserID, msgID
  - [ ] delete all reports for this message using a.reports.DeleteByMessage
  - [ ] update admin notification text with "rejected by {admin} in {duration}"
  - [ ] remove buttons (empty keyboard)
- [ ] add tests for approve/reject handlers
- [ ] verify approve and reject buttons work correctly
- [ ] mark iteration 7 complete and document any changes from original plan in this section

### Iteration 8: Admin Callback Handlers - Ban Reporter
- [ ] implement callbackReportBanReporterAsk method (R? prefix - show confirmation)
  - [ ] parse callback data: reportedUserID, msgID
  - [ ] get all reports for this message using a.reports.GetByMessage
  - [ ] generate inline keyboard with one button per reporter: "Ban [username]" callback: R!reporterID:msgID
  - [ ] add "Cancel" button in new row with callback: RX+reportedUserID:msgID
  - [ ] update buttons only using tbapi.NewEditMessageReplyMarkup
- [ ] implement callbackReportBanReporterConfirm method (R! prefix - ban specific reporter)
  - [ ] parse callback data: reporterID, msgID
  - [ ] ban reporter permanently using banUserOrChannel
  - [ ] delete reporter from database using a.reports.DeleteReporter
  - [ ] get remaining reports for this message using a.reports.GetByMessage
  - [ ] if no reporters remain:
    - [ ] delete all reports for this message using a.reports.DeleteByMessage
    - [ ] update admin notification: "all reporters banned by {admin} in {duration}"
    - [ ] remove buttons (empty keyboard)
  - [ ] if reporters remain:
    - [ ] update admin notification text to remove banned reporter from list
    - [ ] restore original buttons (Approve, Reject, Ban Reporter) using tbapi.NewEditMessageText
- [ ] implement callbackReportCancel method (RX prefix - cancel ban reporter)
  - [ ] parse callback data: reportedUserID, msgID
  - [ ] restore original button layout (Approve, Reject, Ban Reporter)
  - [ ] update buttons only using tbapi.NewEditMessageReplyMarkup
- [ ] add tests for ban reporter flow
- [ ] verify confirmation dialog works correctly
- [ ] mark iteration 8 complete and document any changes from original plan in this section

### Iteration 9: Integration and Testing
- [ ] test full flow: report → threshold → notification → admin action
- [ ] test multiple reporters updating same notification
- [ ] test rate limiting prevents spam
- [ ] test report command deletion keeps chat clean
- [ ] test ban reporter confirmation flow
- [ ] test training mode behavior
- [ ] test soft ban mode behavior
- [ ] test dry mode behavior
- [ ] verify no regressions in existing /spam functionality
- [ ] verify no regressions in existing ban functionality
- [ ] mark iteration 9 complete and document any changes from original plan in this section

### Iteration 10: Documentation
- [ ] update README.md with /report command documentation
- [ ] add "User Reporting" section explaining the feature
- [ ] document all new CLI flags in "All Application Options"
- [ ] document configuration examples
- [ ] add usage examples for users and admins
- [ ] update ARCHITECTURE.md if needed
- [ ] mark iteration 10 complete and document any changes from original plan in this section

### Iteration 11: Final Validation
- [ ] run full test suite: `go test -race ./...`
- [ ] run linter: `golangci-lint run`
- [ ] run formatter: `gofmt -s -w $(find . -type f -name "*.go" -not -path "./vendor/*" -not -path "*/mocks/*")`
- [ ] run goimports: `goimports -w $(find . -type f -name "*.go" -not -path "./vendor/*" -not -path "*/mocks/*")`
- [ ] run comment normalizer: `unfuck-ai-comments run --fmt --skip=mocks ./...`
- [ ] verify all tests pass
- [ ] verify no linter errors
- [ ] create git commit with changes
- [ ] mark iteration 11 complete and document any changes from original plan in this section

## Implementation Patterns Summary

### Storage Layer Patterns (storage/reports.go)
1. **Command Constants**: Use `iota + 500` offset for reports-related commands
2. **QueryMap**: Use `Add()` for db-specific queries (SQLite vs Postgres), `AddSame()` for identical queries
3. **Separate Module**: Reports is its own storage module (storage/reports.go), following ApprovedUsers/DetectedSpam pattern
4. **Table Creation**: Use engine.InitTable with TableConfig in NewReports constructor
5. **Primary Key**: Use auto-increment id as PRIMARY KEY + UNIQUE constraint on business keys
6. **Locking**: Always Lock/Unlock for writes, RLock/RUnlock for reads
7. **GID Usage**: Always include `gid = ?` in WHERE clauses and use `r.GID()`
8. **Query Execution**: Use `r.Adopt()` for query adaptation, NamedExecContext for inserts with maps
9. **Error Handling**: Return `fmt.Errorf("failed to ...: %w", err)` for wrapped errors
10. **Logging**: Use `log.Printf("[INFO]")` for actions, `[DEBUG]` for detailed info, `[WARN]` for errors
11. **Cleanup**: Internal cleanup methods (lowercase) called from write operations
12. **Interface**: Define Reports interface in events package, implement in storage package

### Admin Handler Patterns (app/events/admin.go)
1. **Callback Prefixes**: Two-char prefixes starting with 'R' to avoid conflicts
2. **parseCallbackData**: Update to handle both single-char and two-char prefixes
3. **Button Creation**: Use `tbapi.NewInlineKeyboardMarkup()` with `NewInlineKeyboardRow()`
4. **Message Updates**: Use `NewEditMessageReplyMarkup` for buttons only, `NewEditMessageText` for text+buttons
5. **Markdown Escaping**: Use `escapeMarkDownV1Text()` for all user-generated content
6. **Ban Logic**: Reuse existing `banUserOrChannel` function with `banRequest` struct
7. **Message Deletion**: Use `tbapi.DeleteMessageConfig` with rate limiting for bulk operations
8. **Admin Checks**: Always validate chatID == adminChatID before processing callbacks
