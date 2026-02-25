# Locator Fallback for Admin Forwarded Messages

## Overview
- When a superuser forwards a spam message to the admin chat, `MsgHandler` looks up the message in the locator to get the original poster's user ID and message ID. If the bot never processed the original message (restart, network gap, Telegram update loss), the locator lookup fails and the entire operation aborts — no ban, no spam sample update, no removal from approved list.
- This plan adds a degraded fallback path: when the locator lookup fails but `ForwardOrigin` provides the original sender's user ID (`IsUser()` — sender hasn't hidden forwards), the bot performs all possible actions (ban, spam update, remove from approved) and warns the admin that the original message must be deleted manually.
- When `ForwardOrigin` doesn't provide a user ID (`IsHiddenUser()` or no ForwardOrigin), behavior remains unchanged — return error as today.

Related to #373

## Context (from discovery)
- files/components involved:
  - `app/events/admin.go` — `MsgHandler` (lines 61–171), `getForwardUsernameAndID` (lines 242–252)
  - `app/events/admin_test.go` — existing tests for `MsgHandler` (lines 728+, 847+)
- related patterns found:
  - `directReport()` in the same file handles ban/delete/spam-update with `multierror` for partial failures — good model for the fallback path
  - `getForwardUsernameAndID()` already extracts `fwdID` and `username` from `ForwardOrigin` but currently only used for logging
- dependencies identified:
  - `Locator` interface (`Message()` method) — the failing dependency
  - `Bot` interface — `UpdateSpam()`, `RemoveApprovedUser()`, `OnMessage()`
  - `banUserOrChannel()` — needs `userID` and `chatID`

## Development Approach
- **testing approach**: Regular (code first, then tests)
- complete each task fully before moving to the next
- make small, focused changes
- **CRITICAL: every task MUST include new/updated tests** for code changes in that task
- **CRITICAL: all tests must pass before starting next task**
- **CRITICAL: update this plan file when scope changes during implementation**
- run tests after each change
- maintain backward compatibility

## Testing Strategy
- **unit tests**: required for every task
- existing test `TestAdmin_MsgHandler` has a "message not found in locator" subtest — this needs to be updated/extended to cover the fallback path
- new subtests needed: locator fails + ForwardOrigin has user ID, locator fails + hidden user, locator fails + channel forward, locator fails + training mode, locator fails + no ForwardOrigin

## Progress Tracking
- mark completed items with `[x]` immediately when done
- add newly discovered tasks with ➕ prefix
- document issues/blockers with ⚠️ prefix
- update plan if implementation deviates from original scope

## Implementation Steps

### Task 1: Add degraded fallback path in MsgHandler

**Files:**
- Modify: `app/events/admin.go`

- [x] refactor `MsgHandler` at lines 98-100: when `a.locator.Message()` returns `!ok`, check `fwdID` (already extracted at line 70) — if `fwdID != 0`, enter the fallback path instead of returning error
- [x] in the fallback path, perform all possible operations using `fwdID` and `username` from ForwardOrigin:
  - call `a.bot.RemoveApprovedUser(fwdID)` (log error but don't fail, same as `directReport` pattern)
  - call `a.bot.OnMessage(bot.Message{Text: update.Message.Text, From: bot.User{ID: fwdID}}, true)` — use `update.Message.Text` (not `msgTxt`) to match normal path line 120
  - send detection results to admin chat (plain `send()`, no unban button — `msgID` is unavailable without locator)
  - if not dry run: call `a.bot.UpdateSpam(msgTxt)` — use `msgTxt` here (may include caption from `transform()`)
  - if not dry run: call `banUserOrChannel()` with `fwdID`, passing `training: a.trainingMode` to match normal path behavior
  - send warning to admin chat that original message must be deleted manually (include message text snippet, username, and user ID for identification)
- [x] check if forwarded user is a super-user using `a.superUsers.IsSuper(username, fwdID)` before banning — same guard as the normal path
- [x] when `fwdID == 0` (hidden user or no ForwardOrigin), keep existing behavior — return error
- [x] write tests in `app/events/admin_test.go` for the fallback path:
  - subtest: locator fails, ForwardOrigin.IsUser() with valid user ID → verify ban (with training flag), spam update, remove approved all called, warning sent about manual deletion
  - subtest: locator fails, ForwardOrigin.IsHiddenUser() (fwdID=0) → verify error returned as before
  - subtest: locator fails, ForwardOrigin.IsChannel() (fwdID=0) → verify error returned as before
  - subtest: locator fails, ForwardOrigin.IsUser() + dry mode → verify no ban/spam-update, detection results still sent
  - subtest: locator fails, ForwardOrigin.IsUser() + training mode → verify UpdateSpam called, ban request made with training=true
  - subtest: locator fails, ForwardOrigin.IsUser() + super-user → verify ignored with appropriate error
- [x] run `go test -race ./app/events/` — must pass before next task

### Task 2: Verify acceptance criteria
- [x] verify all requirements from Overview are implemented
- [x] verify edge cases are handled (hidden user, no ForwardOrigin, super-user, dry mode)
- [x] run full unit test suite: `go test -race ./...`
- [x] run linter: `golangci-lint run`
- [x] run formatters

### Task 3: [Final] Update documentation
- [x] update README.md if behavior change warrants mention
- [x] move this plan to `docs/plans/completed/`

## Technical Details

**Fallback decision tree:**
```
locator.Message() fails
├── fwdID != 0 (ForwardOrigin.IsUser())
│   ├── super-user check → ignore if super
│   ├── RemoveApprovedUser(fwdID) — log error, don't fail
│   ├── OnMessage(update.Message.Text, checkOnly=true) → display detection results
│   ├── if !dry: UpdateSpam(msgTxt)
│   ├── if !dry: banUserOrChannel(fwdID, training=a.trainingMode)
│   └── warn admin: "original message needs manual deletion"
└── fwdID == 0 (IsHiddenUser() / IsChannel() / no ForwardOrigin)
    └── return error (same as today)
```

**Key implementation notes:**
- `OnMessage()` uses `update.Message.Text` (matching normal path line 120), while `UpdateSpam()` uses `msgTxt` (which may include caption via `transform()`)
- no unban button in fallback mode — `msgID` from primary chat is unavailable without locator, so `sendWithUnbanMarkup` cannot be used
- `banUserOrChannel` must receive `training: a.trainingMode` — in training mode the ban is a no-op (logged only), matching normal path behavior

**Warning message to admin chat** (in the fallback path):
- should clearly indicate this is a degraded operation
- include username, user ID, and message text snippet so admin can find and delete the original message manually
- use markdown formatting consistent with existing admin messages

## Post-Completion

**Manual verification:**
- test with a real forwarded message where the bot was restarted between the original message and the forward
- verify the admin chat warning is clear and actionable
