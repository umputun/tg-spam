# max-short-msg-count: flag unapproved users posting too many short messages

## Overview

Add an optional spam check that bans an unapproved user who has posted too many messages without graduating to "approved" status. A spammer probing the channel by posting innocuous short messages ("hi", "hello", "yo", "ok") evades the per-message content checks (each message is too short to classify, and they're all different so the duplicate detector doesn't trigger). The new check closes that gap by counting per-user messages and triggering when the count from a user who is *still* unapproved exceeds an operator-configured threshold.

The feature is opt-in (`--max-short-msg-count=0` disables, default), only effective when first-message evaluation is enabled (`--first-message-only` or `--first-messages-count > 0`). When triggered, the result follows the existing spam-detection pipeline: ban + delete the current message + delete the prior messages (via `ExtraDeleteIDs`), respecting `--training`, `--dry`, and `--soft-ban`.

## Context (from discovery)

- Project: tg-spam, Go 1.24+, Telegram anti-spam bot.
- Detector core: `lib/tgspam/detector.go` — `Detector.Check` orchestrates all sub-checks.
- Existing precedent: `lib/tgspam/duplicate.go` (`duplicateDetector`) catches the *same* short message repeated, populates `ExtraDeleteIDs` for cleanup. The new check is the complement — *different* short messages from the same user.
- Storage: `app/storage/locator.go` — `messages` table with composite index `idx_messages_gid_user_id_time` (locator.go:69) makes per-user `COUNT(*)` an index scan.
- Approval state: `lib/approved/approved.go` (`approved.UserInfo`) tracks `Count` in-memory; only stores `(uid, gid, name, timestamp)` in DB.
- Settings plumbing: `app/config/settings.go` (typed Settings + `zeroAwarePaths`), `app/main.go` (CLI flags + `optToSettings`-equivalent block), `app/settings.go` (`optToSettings` mapping), `app/webapi/config.go` (form parser), `app/webapi/assets/settings.html` (UI).
- E2E test runner: `e2e-ui/e2e_test.go` (Playwright-based settings round-trip).

## Development Approach

- **Testing approach**: Regular (code first, then tests). Each task ends with tests for that task's changes; no task may be marked done with failing tests.
- Complete each task fully before moving to the next.
- Make small, focused changes.
- **CRITICAL: every task MUST include new/updated tests** for code changes in that task.
  - Unit tests are not optional — they are a required part of the checklist.
  - Cover both success and error scenarios.
  - Update existing test cases when behavior changes.
- **CRITICAL: all tests must pass before starting next task** — no exceptions.
- **CRITICAL: update this plan file when scope changes during implementation.**
- Run tests after each change.
- Maintain backward compatibility (the feature defaults to disabled).

## Testing Strategy

- **Unit tests**: required for every task per the rules above.
- **E2E tests**: this project has Playwright-based UI tests (`e2e-ui/e2e_test.go`).
  - The web UI form field round-trip MUST be exercised in `e2e-ui/e2e_test.go` in the same task that adds it.
  - Treat E2E tests with the same rigor as unit tests (must pass before next task).
- **Integration test** (bot path): one focused test in `app/bot/spam_test.go` or `app/events/listener_test.go` exercising the end-to-end flow (mocked locator returns increasing counts; assert spam result + `ExtraDeleteIDs` propagation).

## Progress Tracking

- Mark completed items with `[x]` immediately when done.
- Add newly discovered tasks with `➕` prefix.
- Document issues/blockers with `⚠️` prefix.
- Update plan if implementation deviates from original scope.
- Keep plan in sync with actual work done.

## Solution Overview

**Architecture: Option D — derive count from existing locator**

No new state, no new struct, no in-memory cache. The check queries the `messages` table for the per-user count at decision time and combines it with the existing in-memory `approvedUsers[id].Count`. The trigger condition is:

```
(messagesFromUser - approvedCount) >= MaxShortMsgCount
  AND user is unapproved (approvedCount < FirstMessagesCount)
  AND current message is short (len([]rune(req.Msg)) < d.MinMsgLen)
  AND !req.CheckOnly
```

**Why this works**: each successful long ham message increments `approvedUsers[id].Count`. For an unapproved user, the difference between total messages and approved count is the number of messages that *didn't* graduate them — almost all of which are short (LLM-detected spam users get banned and don't show up here). The proxy is strong enough in practice.

**Wiring pattern**: Pattern 3 — internal method on `Detector`, NOT a separate struct, NOT a `MetaCheck`. The check is stateless within the detector (just reads detector state and queries an injected dependency), so a separate struct would be theatre. The new method `d.isShortMsgFlood(req)` lives alongside existing `isStopWord` / `isCasSpam` / `isMultiLang`. Called inline in `Detector.Check` with an outer guard so disabled mode does no work.

## Technical Details

### Configuration shape

Top-level scalar in `Settings` (NOT a nested group), placed between `MinMsgLen` and `MaxEmoji` to co-locate "short message" knobs:

```go
MaxShortMsgCount int `json:"max_short_msg_count" yaml:"max_short_msg_count" db:"max_short_msg_count"`
```

CLI flag: `--max-short-msg-count` env `MAX_SHORT_MSG_COUNT` default `0`. Add `"MaxShortMsgCount"` to `zeroAwarePaths` so 0=disabled survives Settings merges.

Validation in `app/main.go` startup:
- `MaxShortMsgCount < 0` → error: `"max-short-msg-count (%d) must be >= 0 (0 disables)"`.
- `MaxShortMsgCount > 0 && !FirstMessageOnly && FirstMessagesCount == 0` → error: `"max-short-msg-count requires first-message-only or first-messages-count > 0"`.

### Detector wiring

New consumer-side interface in `lib/tgspam/detector.go`:

```go
type MessageCounter interface {
    CountUserMessages(ctx context.Context, userID string) (int, error)
    UserMessageIDs(ctx context.Context, userID string, limit int) ([]int, error)
}
```

`Detector` gets a new field `messageCounter MessageCounter` and a `WithMessageCounter` setter parallel to `WithUserStorage`.

New method `(d *Detector) isShortMsgFlood(req spamcheck.Request) spamcheck.Response`:
- Fast-bail returns `Spam: false` for: empty UserID, `req.CheckOnly`, message len ≥ MinMsgLen, user already approved (`approvedCount >= FirstMessagesCount`), locator error.
- Trigger returns:
  ```go
  spamcheck.Response{
      Name:           "short-msg-flood",
      Spam:           true,
      Details:        fmt.Sprintf("%d messages without approval (threshold %d)", excess, d.MaxShortMsgCount),
      ExtraDeleteIDs: extraIDs,
  }
  ```
  where `extraIDs` come from `d.messageCounter.UserMessageIDs(ctx, req.UserID, d.MaxShortMsgCount*2)`.

Inline call in `Detector.Check`, after the duplicate-detector branch and before the stop-word/emoji/metachecks block:

```go
if d.MaxShortMsgCount > 0 && d.messageCounter != nil &&
    (d.FirstMessageOnly || d.FirstMessagesCount > 0) {
    if resp := d.isShortMsgFlood(req); resp.Spam {
        cr = append(cr, resp)
        d.spamHistory.Push(req)
        return true, cr
    }
}
```

(No-op when not flagged — do NOT append non-spam noise to `cr`.)

**Deviation from `duplicateDetector` precedent — early return vs. LLM consensus.** The duplicate detector appends its result to `cr` and lets the unified `spamDetected` / LLM-consensus flow (detector.go:314, 322, 356-361) evaluate, so an LLM ham vote can veto a duplicate flag under `LLMConsensusAll` / non-veto modes. This new check returns immediately on flag and bypasses LLM consensus entirely. That is intentional: short-message flood is a behavioral signal that doesn't depend on content, and an LLM looking at the latest single short message has no information that would justify overriding the count. Document this asymmetry in the README copy (Task 8) so operators expecting consensus to apply uniformly are not surprised.

**Context for SQL query**: use `d.ctxWithStoreTimeout()` (detector.go:1232), the same helper used elsewhere for storage I/O under `RLock` (e.g., approved-users write at line 367). Do NOT use `context.Background()`.

### Locator extension

Two new methods on `*Locator` in `app/storage/locator.go`:

```go
func (l *Locator) CountUserMessages(ctx context.Context, userID string) (int, error)
// SELECT COUNT(*) FROM messages WHERE gid = ? AND user_id = ?
// uses index idx_messages_gid_user_id_time (locator.go:69) — pure index scan

func (l *Locator) UserMessageIDs(ctx context.Context, userID string, limit int) ([]int, error)
// thin string->int64 wrapper around existing GetUserMessageIDs (locator.go:344)
```

`*Locator` implements the `tgspam.MessageCounter` interface implicitly. Wired in `app/main.go` after `WithUserStorage` (~line 453):

```go
detector.WithMessageCounter(locator)
```

### Web UI

`app/webapi/config.go` — new form parser block (parallel to existing `minMsgLen` parser at line 525):

```go
if val := r.FormValue("maxShortMsgCount"); val != "" {
    if n, err := strconv.Atoi(val); err == nil {
        settings.MaxShortMsgCount = n
    }
}
```

`app/webapi/assets/settings.html` — two additions next to the `minMsgLen` block:

1. Edit panel input (after `minMsgLen` ~line 163). Surrounding HTML uses `<div class="row mb-3">` containers wrapping `<div class="col-md-6 mb-3">` columns. Pair this new column with an existing or sibling column so the row keeps its 2-column rhythm — do not drop a single col-md-6 into a row by itself:
   ```html
   <div class="col-md-6 mb-3">
       <label for="maxShortMsgCount" class="form-label">Max Short Msgs (Unapproved)</label>
       <input type="number" min="0" class="form-control" id="maxShortMsgCount" name="maxShortMsgCount" value="{{.MaxShortMsgCount}}">
       <div class="form-text">Treat as spam after N messages without graduation (0 disables)</div>
   </div>
   ```

2. Read-only display row (after `Min Message Length` row ~line 254):
   ```html
   <tr><th>Max Short Msgs (Unapproved)</th><td>{{if eq .MaxShortMsgCount 0}}disabled{{else}}{{.MaxShortMsgCount}}{{end}}</td></tr>
   ```

### Edge cases (preserve, do not over-engineer around them)

- **CheckOnly**: skipped via early return in `isShortMsgFlood`.
- **Channel messages (`SenderChat`)**: `bot.OnMessage` already maps `checkUserID = msg.SenderChat.ID` (spam.go:97); locator stores under same ID. Works transparently.
- **Anonymous admin / linked channel posts**: listener.go:387 skips `OnMessage` entirely — never reaches the detector.
- **Pre-approved users**: detector.go:236 short-circuits to `pre-approved` before this check is reached.
- **Edits**: locator keys by `hash(text)` with `INSERT OR REPLACE`. No-op edit doesn't increment count; content-changing edit creates a new row (consistent with `duplicateDetector`).
- **Locator TTL pruning**: rare for actively-evaluated unapproved users (cleanup only kicks in when `total > minSize`). Acceptable; document as known limitation.
- **Rollout on populated DB**: existing unapproved users with ≥N rows in locator may trip on their next message. Acceptable since those users were already exhibiting the pattern. Document.
- **Naturally terse legitimate users**: `approvedCount` only increments on long ham messages (`!isShortMessage` gate at detector.go:366). A user posting (short, long, short, short) by message 4 has `total=4, approvedCount=1, excess=3` — would trip with `MaxShortMsgCount=3`. **The risk is bounded to the evaluation window only**: once the user crosses `FirstMessagesCount` they're approved, the check skips entirely (fast-bail in `isShortMsgFlood`), and they're immune for the lifetime of the bot. So this is a "first few messages of a user's lifetime" risk, not a steady-state one. Recommended baseline: `MaxShortMsgCount >= 3` and a low `FirstMessagesCount` (1–2). Capture in README copy (Task 8) with the "only during evaluation" clarification.

### Deliberately out of scope (YAGNI)

- No time window — Q1/Q2 scope (unapproved + lifetime) auto-bounds the count.
- No in-memory cache — SQL with covering index is fast enough.
- No warn-before-ban path — feature is opt-in; operator can use existing `/warn` flow for softer treatment.
- No per-user override — existing `AddApprovedUser` already handles whitelist (bumps count above `FirstMessagesCount`, skipping the check).

## What Goes Where

- **Implementation Steps** (`[ ]` checkboxes): in-codebase work — Go code, templates, tests, README/CLAUDE.md updates.
- **Post-Completion** (no checkboxes): operator-side rollout notes, manual smoke tests.

## Implementation Steps

### Task 1: Add `MaxShortMsgCount` to Settings struct + zeroAwarePaths

**Files:**
- Modify: `app/config/settings.go`
- Modify: `app/config/settings_test.go`

- [x] add `MaxShortMsgCount int` field to `Settings` struct between `MinMsgLen` and `MaxEmoji` with json/yaml/db tags (`max_short_msg_count`)
- [x] add `"MaxShortMsgCount": true` entry to `zeroAwarePaths` with comment `// lib/tgspam/detector.go:<line> (> 0): 0 disables` (line number filled in once Task 4 lands)
- [x] write test asserting `ApplyDefaults` does NOT overwrite zero `MaxShortMsgCount` when template has a non-zero value (zeroAwarePaths semantics)
- [x] write test asserting `MaxShortMsgCount=5` round-trips through JSON/YAML marshal/unmarshal
- [x] run `go test ./app/config/...` — must pass before task 2

### Task 2: Add CLI flag + startup validation in main.go

**Files:**
- Modify: `app/main.go`
- Modify: `app/settings.go`
- Modify: `app/main_test.go`
- Modify: `app/settings_test.go`

- [x] add `MaxShortMsgCount int` field to opts struct with `long:"max-short-msg-count" env:"MAX_SHORT_MSG_COUNT" default:"0"` and description, placed immediately after `MinMsgLen` (~line 179) to mirror the Settings struct ordering
- [x] add validation block: `MaxShortMsgCount < 0` → error; `> 0 && !FirstMessageOnly && FirstMessagesCount == 0` → error (Settings has no `FirstMessageOnly` field; translated to `ParanoidMode && FirstMessagesCount == 0`, since detector wires `FirstMessageOnly = !ParanoidMode` and paranoid clears `FirstMessagesCount`)
- [x] add `MaxShortMsgCount: opts.MaxShortMsgCount` mapping in `optToSettings` in `app/settings.go` (next to `MinMsgLen` ~line 160)
- [x] extend `TestOptToSettings` in `app/settings_test.go` to assert `MaxShortMsgCount` round-trips through `optToSettings` (the existing test is field-by-field — silently missed mappings are otherwise undetectable)
- [x] add validation test cases in `app/main_test.go` parallel to existing `warn.threshold` tests: `-1` fails; `3` with no first-messages mode fails; `3` with `--first-message-only` OK; `3` with `--first-messages-count=2` OK; `0` OK
- [x] run `go test ./app/...` — must pass before task 3
- [x] add `--max-short-msg-count` row to README.md "All Application Options" table (required by `TestREADMEAllOptionsMatchesHelp` which fires as soon as the flag exists; full README narrative still owned by Task 8)

### Task 3: Add Locator methods + interface implementation

**Files:**
- Modify: `app/storage/locator.go`
- Modify: `app/storage/locator_test.go`

- [x] add `func (l *Locator) CountUserMessages(ctx context.Context, userID string) (int, error)` — parses string→int64, runs `SELECT COUNT(*) FROM messages WHERE gid = ? AND user_id = ?` (uses `idx_messages_gid_user_id_time`)
- [x] add `func (l *Locator) UserMessageIDs(ctx context.Context, userID string, limit int) ([]int, error)` — string→int64 wrapper around existing `GetUserMessageIDs` (locator.go:344)
- [x] write `TestLocator_CountUserMessages`: populate via `AddMessage`, assert count; verify gid filtering (different gid → not counted); invalid userID string → error
- [x] write `TestLocator_UserMessageIDs`: populate, fetch IDs, assert order (DESC by time), assert `limit` honored, assert invalid userID → error
- [x] run `go test ./app/storage/...` — must pass before task 4

### Task 4: Add MessageCounter interface + isShortMsgFlood method to Detector

**Files:**
- Modify: `lib/tgspam/detector.go`
- Create: `lib/tgspam/mocks/message_counter.go` (via `go generate`)
- Modify: `lib/tgspam/detector_test.go`

- [x] add `MessageCounter` interface in detector.go with `CountUserMessages` and `UserMessageIDs` methods
- [x] add `messageCounter MessageCounter` field to `Detector`
- [x] add `WithMessageCounter(mc MessageCounter)` setter (parallel to `WithUserStorage`)
- [x] add `MaxShortMsgCount int` field to `Config` struct, threaded into Detector via `NewDetector`
- [x] add `//go:generate moq --out mocks/message_counter.go --pkg mocks --skip-ensure --with-resets . MessageCounter` directive (matches existing style at detector.go:30-33) and run `go generate ./lib/tgspam/`
- [x] implement `(d *Detector) isShortMsgFlood(req spamcheck.Request) spamcheck.Response` per the design above (fast-bail returns + trigger return with `ExtraDeleteIDs`)
- [x] insert outer-guarded call in `Detector.Check` after duplicate-detector branch, before stop-word/emoji/metachecks
- [x] write `TestDetector_MaxShortMsgCount` with subtests (table-driven where natural):
  - [x] disabled when threshold is 0
  - [x] disabled when no MessageCounter wired
  - [x] disabled when neither FirstMessageOnly nor FirstMessagesCount set
  - [x] triggers on Nth short message from unapproved user (asserts `Name=="short-msg-flood"`, `Spam==true`, Details string format)
  - [x] does not trigger on long messages even when count exceeds threshold
  - [x] does not trigger for already-approved users (Count >= FirstMessagesCount)
  - [x] formula correctness: `excess = total - approvedCount`, threshold compared to excess
  - [x] `ExtraDeleteIDs` populated from `UserMessageIDs(limit=2*threshold)`
  - [x] locator error → falls through to normal checks (no spam)
  - [x] `CheckOnly` request skipped
  - [x] empty `req.UserID` skipped
  - [x] edge case: `excess == 0` (total == approvedCount) → never triggers
  - [x] defensive: `approvedCount > total` (shouldn't happen, but assert no false positive / no underflow)
- [x] run `go test -race ./lib/tgspam/...` — must pass before task 5
- [x] back-fill the line number in `zeroAwarePaths` comment in `app/config/settings.go` from Task 1

### Task 5: Wire Detector to locator + plumb MaxShortMsgCount in main.go

**Files:**
- Modify: `app/main.go`

- [x] add `MaxShortMsgCount: settings.MaxShortMsgCount` to `detectorConfig` block (~line 749)
- [x] add `detector.WithMessageCounter(locator)` immediately after the locator is constructed (~line 463, after `storage.NewLocator(...)` succeeds — NOT next to `WithUserStorage` at line 453, since the locator does not exist yet at that point)
- [x] write/extend a wiring test in `app/main_test.go` (or appropriate test) asserting that with `--max-short-msg-count=3 --first-messages-count=2`, the detector is configured with the threshold (smoke test, not a behavioral test — those live in detector_test.go)
- [x] run `go test -race ./app/...` — must pass before task 6

### Task 6: Add web UI form field + form parser + e2e test

**Files:**
- Modify: `app/webapi/config.go`
- Modify: `app/webapi/assets/settings.html`
- Modify: `app/webapi/webapi_test.go`
- Modify: `e2e-ui/e2e_test.go`

- [x] add `maxShortMsgCount` form parser block in `config.go` next to `minMsgLen` (~line 525), parallel pattern (silent ignore on non-numeric)
- [x] add edit-panel input in `settings.html` after `minMsgLen` block (~line 163) per the snippet above
- [x] add read-only display row in `settings.html` after `Min Message Length` row (~line 254) per the snippet above
- [x] write/extend webapi test for form parse: POST with `maxShortMsgCount=5` → persisted; empty → unchanged; non-numeric → unchanged
- [x] extend `e2e-ui/e2e_test.go` settings round-trip to fill `#maxShortMsgCount`, save, reload, assert value persists
- [x] run `go test -race ./app/webapi/...` — must pass
- [x] run e2e tests if available locally; otherwise verify in CI — must pass before task 7

### Task 7: Bot integration test

**Files:**
- Modify: `app/bot/spam_test.go` (or `app/events/listener_test.go` — pick the one with existing locator-mocked end-to-end coverage)

**Important context for test setup**: in the listener pipeline, `Locator.AddMessage` runs at `app/events/listener.go:380` BEFORE `Bot.OnMessage` at line 392. So by the time `Detector.Check` evaluates, the current message is already counted. The mock for `CountUserMessages` should return values that include the current message (i.e., starts at 1 on the first call, not 0).

- [x] write a focused test: with `MaxShortMsgCount=3`, `FirstMessagesCount=2`, `MinMsgLen=50`, post 3 short messages from a fresh user. Mock `CountUserMessages` returns `1, 2, 3` per successive call. Mock `UserMessageIDs` returns the IDs from prior posts.
- [x] assert messages 1 and 2 → `Spam=false` (excess=1, then 2 — both below threshold 3)
- [x] assert message 3 → `Spam=true` with `Name="short-msg-flood"` (excess=3, hits threshold)
- [x] assert `ExtraDeleteIDs` on message 3 contains the IDs of messages 1 and 2, and that the listener attempts deletion of all of them
- [x] run `go test -race ./app/bot/... ./app/events/...` — must pass before task 8

### Task 8: Update README.md

**Files:**
- Modify: `README.md`

- [x] add new dedicated section "Short-message flood detection" (own section per operator decision, NOT a subsection). Cover: the threat model, trigger formula, relation to `--first-messages-count`, `ExtraDeleteIDs` cleanup behavior, training/dry/soft-ban interaction, the early-return-vs-LLM-consensus deviation (LLM votes do not override this check, by design), rollout note (pre-existing locator data may trip existing unapproved users on first activation), and the naturally-terse-user false-positive note with recommended baselines (`MaxShortMsgCount >= 3`, `FirstMessagesCount` low)
- [x] add `--max-short-msg-count=` row to the "All Application Options" table; copy must match `--help` output exactly
- [x] write/refresh examples — recommended config: `--max-short-msg-count=3 --first-messages-count=2`
- [x] (no automated test — verified by reading the rendered README and matching against `./tg-spam --help`)
- [x] proceed to task 9

### Task 9: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [x] add new "Short Message Flood Detection" subsection under "Spam Detection Architecture", parallel to "Warn Auto-Ban" / "ExtraDeleteIDs Feature". Capture:
  - derivation formula `(total - approvedCount) >= MaxShortMsgCount`
  - stateless within detector (no cache, no struct)
  - dependency on locator + index `idx_messages_gid_user_id_time`
  - relation to locator TTL (pruning rare for actively-evaluated users)
  - edge cases: CheckOnly, anonymous admin, channel SenderChat, edits
  - interaction with training/dry/soft-ban
- [x] (no automated test — informational doc)
- [x] proceed to task 10

### Task 10: Verify acceptance criteria

- [x] verify `--max-short-msg-count=0` (default) is fully no-op: no SQL queries, no behavior change vs. master
- [x] verify validation rejects misconfiguration as designed
- [x] verify trigger fires correctly with mocked locator (already covered in Task 4 + Task 7)
- [x] verify `ExtraDeleteIDs` populated and listener-side deletion happens
- [x] verify training/dry/soft-ban modes intercept correctly (existing pipeline — should "just work")
- [x] verify channel `SenderChat` flow still routes through the same UserID logic
- [x] run full test suite: `go test -race ./...`
- [x] run linter: `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0`
- [x] run formatters: `~/.claude/format.sh`
- [x] normalize comments: `unfuck-ai-comments run --fmt --skip=mocks ./...`
- [x] verify test coverage on new code at or above project standard

### Task 11: Move plan to completed

- [x] verify all checkboxes above are `[x]`
- [x] move this plan to `docs/plans/completed/20260429-max-short-msg-count.md`

## Post-Completion

*Items requiring manual operator action — informational only*

**Manual verification (operator-side, before enabling in production):**

- Run with `--max-short-msg-count=3 --first-messages-count=2` against a test group; post 3 short messages from a fresh account, expect ban + cleanup of prior messages on the 3rd.
- Verify training-mode interaction: with `--training`, expect log entry but no actual ban.
- Verify dry-run: with `--dry`, expect log entry showing what would be banned.
- Verify soft-ban: with `--soft-ban`, expect mute (not full ban) and prior-message cleanup.

**Rollout caveat:**

- On first activation against a populated locator DB, existing unapproved users with ≥N rows in `messages` will trigger on their next message. This is the intended threat-model behavior (those users are exhibiting the pattern), but operators should be informed via the README rollout note added in Task 8.

**No external system updates required** — feature is internal to tg-spam, no consuming projects, no third-party integrations.
