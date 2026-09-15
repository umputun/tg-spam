# Fix /spam and /report command quote handling

## Overview
- When an admin uses `/spam` (or a user uses `/report`) on a message containing quoted text from an external channel, only the main message text (e.g., "Thank you") is processed — the quoted spam content is ignored
- This results in useless spam samples being added to the classifier and incorrect diagnostic results shown to admins
- The auto-detection path (`bot/spam.go:OnMessage`) correctly concatenates `msg.Quote` with `msg.Text`, but the admin/report paths skip this step
- Fix: include `origMsg.Quote.Text` in `msgTxt` in both `directReport` and user report handlers
- Related to #376

## Context
- `app/events/admin.go:directReport` (line 453) — sets `msgTxt := origMsg.Text` without Quote
- `app/events/reports.go` (line 113) — same pattern, sets `msgTxt := origMsg.Text` without Quote
- `origMsg` is `*tbapi.Message`, Quote is `origMsg.Quote` (`*TextQuote`), text is `origMsg.Quote.Text`
- `msgTxt` feeds into: `UpdateSpam(msgTxt)`, `diagMsg.Text`, and admin notification text
- The Telegram `TextQuote` struct has `Text string`, `Entities []MessageEntity`, `Position int`, `IsManual bool`

## Development Approach
- **testing approach**: TDD - write failing test first, verify failure, then fix
- complete each task fully before moving to the next
- make small, focused changes
- **CRITICAL: every task MUST include new/updated tests**
- **CRITICAL: all tests must pass before starting next task**
- run tests after each change
- maintain backward compatibility

## Progress Tracking
- mark completed items with `[x]` immediately when done
- add newly discovered tasks with ➕ prefix
- document issues/blockers with ⚠️ prefix

## Implementation Steps

### Task 1: Fix `directReport` in admin.go

**Files:**
- Modify: `app/events/admin.go`
- Modify: `app/events/admin_test.go`

- [x] write test in `admin_test.go`: admin `/spam` on message with `Quote` field set — verify `UpdateSpam` receives concatenated text (`origMsg.Text + "\n" + origMsg.Quote.Text`)
- [x] write test: admin `/spam` on message with Quote but empty Quote.Text — verify `UpdateSpam` receives only `origMsg.Text`
- [x] write test: admin `/spam` on message without Quote — verify existing behavior unchanged
- [x] write test: admin `/spam` on message with empty Text but Quote present — verify Quote text is included (transform fallback + Quote)
- [x] run tests — confirm new tests fail
- [x] in `directReport`, add Quote concatenation AFTER the transform fallback block (after line 457), not before it: if `origMsg.Quote != nil && origMsg.Quote.Text != ""` then `msgTxt = msgTxt + "\n" + origMsg.Quote.Text`
- [x] run tests — all must pass

### Task 2: Fix user report path in reports.go

**Files:**
- Modify: `app/events/reports.go`
- Modify: `app/events/reports_test.go`

- [x] write test in `reports_test.go`: user report on message with `Quote` field — verify `UpdateSpam` receives concatenated text
- [x] write test: user report on message without Quote — verify existing behavior unchanged
- [x] run tests — confirm new tests fail
- [x] in reports.go, add Quote concatenation AFTER the transform fallback block (after line 117), same pattern as admin.go
- [x] run tests — all must pass

### Task 3: Verify acceptance criteria

- [x] verify `/spam` on a quoted-reply message includes quote text in spam samples
- [x] verify `/spam` diagnostics (`OnMessage` call) see the full concatenated text
- [x] verify admin notification shows the full text including quote
- [x] verify `/report` path has the same fix
- [x] verify messages without quotes are unaffected (backward compatible)
- [x] run full test suite: `go test -race ./...`
- [x] run linter: `golangci-lint run`

### Task 4: [Final] Update documentation

- [x] update CLAUDE.md if needed (document the Quote handling pattern)
- [x] move this plan to `docs/plans/completed/`

## Technical Details

The fix in both `admin.go` and `reports.go` is identical — add Quote concatenation AFTER the existing transform fallback block.

In `admin.go` (after line 457) and `reports.go` (after line 117), after:
```go
msgTxt := origMsg.Text
if msgTxt == "" {
    m := transform(origMsg)
    msgTxt = m.Text
}
```

Add:
```go
if origMsg.Quote != nil && origMsg.Quote.Text != "" {
    msgTxt = msgTxt + "\n" + origMsg.Quote.Text
}
```

**Placement is critical**: the Quote concatenation must go AFTER the transform fallback. If placed before it, an image-only message with a Quote would have non-empty `msgTxt` (from Quote) and skip the transform fallback, losing the caption text.

This mirrors the logic in `bot/spam.go:OnMessage` (lines 83-89) which uses the same concatenation pattern with `msg.Quote` (the `bot.Message` string version of the same data).

**Note**: `DirectWarnReport` also uses `origMsg.Text` but only for logging — no fix needed there.

## Post-Completion

**Future improvements** (out of scope for this fix):
- `ExternalReply` metadata is completely ignored — could add `HasExternalReply` meta flag for spam checkers
- Quote entities (links inside quoted text) are not counted in `Meta.Links`/`Meta.Mentions`
- Locator stores plain text, not quote-combined text
