# Warn Auto-Ban (issue #71)

## Overview

Track admin `/warn` commands per `(user, gid)` in a new `warnings` table. When a user accumulates `--warn-threshold` warns within `--warn-window`, auto-ban them. Default behavior is unchanged: `--warn-threshold=0` disables the feature, and the existing `/warn` warning-message-and-delete flow continues to work for users who don't opt in.

This complements the existing `--auto-ban-threshold` (which aggregates user `/report` submissions for one *message*) by adding analogous behavior for admin warns aggregated per *user*.

## Context (from discovery)

- **Closest analog** in the codebase: `app/storage/reports.go` (event-log table with cleanup) + `app/events/reports.go executeAutoBan` (auto-ban path triggered by aggregate threshold).
- **Settings layering** (post PR #294, three layers): CLI flags in `app/main.go` → persisted `Settings` struct in `app/config/settings.go` (json/yaml/db tags + `zeroAwarePaths` for fields where 0 means "disabled, do not overwrite") → web UI in `app/webapi/config.go` form parser + `assets/settings.html` inputs. CLI→Settings mapping lives in `app/settings.go optToSettings`.
- **Storage convention**: structs embed both `*engine.SQL` and `engine.RWLocker`, define `migrate` (no-op for new tables) and `Add` that sets `gid` from `r.GID()` and timestamps internally, optionally trigger cleanup inline.
- **Consumer interfaces** for the events layer live in `app/events/events.go`, alongside `Reports`, `Locator`, `Bot`, `TbAPI`. moq-generated mocks go in `app/events/mocks/`.
- **Ban primitives** are shared via `banRequest` and `banUserOrChannel` in `app/events/events.go`; both `executeAutoBan` (reports) and existing admin paths use them. New `executeWarnBan` will use the same primitives — no shared abstraction with `executeAutoBan` (the two diverge meaningfully).

## Development Approach

- **Testing approach**: Regular — code + tests in the same task. Tests are mandatory deliverables of every task; no task is complete until its tests pass.
- Complete each task fully before moving to the next.
- Make small, focused changes.
- **Every task MUST include new/updated tests** for code changes in that task. Tests are not optional.
- **All tests must pass before starting the next task** — no exceptions.
- Storage tests must run against both SQLite and PostgreSQL (existing pattern in `app/storage/*_test.go` parameterizes on both).
- Maintain backward compatibility: `--warn-threshold=0` (default) preserves current `/warn` behavior exactly.

## Testing Strategy

- **Unit tests** required for every task.
- **Storage tests** parameterized on both SQLite and Postgres (use `engine.NewSqlite` and `containers.PostgresTestContainer` patterns from existing tests).
- **e2e tests**: project has Playwright e2e tests for the web UI (`/cmd/e2e/`). New settings inputs in `settings.html` should be exercised by an e2e case verifying the form persists and reloads the values correctly when running in `--confdb` mode.

## Progress Tracking

- Mark completed items with `[x]` immediately when done.
- Add newly discovered tasks with ➕ prefix.
- Document blockers with ⚠️ prefix.
- Update this plan if implementation deviates from the original scope.

## Solution Overview

Five wiring layers, in dependency order:

1. **Storage** (`app/storage/warnings.go`): per-event log table; `Add` writes a row + opportunistically cleans up rows older than the longest reasonable window; `CountWithin` returns the row count for `(gid, user_id)` newer than `now-window`.
2. **Consumer interface** (`app/events/events.go`): `Warnings` interface with `Add` and `CountWithin`; moq mock generated to `app/events/mocks/warnings.go`.
3. **Settings** (`app/config/settings.go`): new `WarnSettings` substruct with `db:"warn_threshold"` and `db:"warn_window"`; `Warn WarnSettings` field on top-level `Settings`; `Warn.Threshold` added to `zeroAwarePaths` so 0 ("disabled") is preserved on settings load.
4. **CLI plumbing** (`app/main.go` flags + `app/settings.go optToSettings`): two new flags, mapped into `WarnSettings` exactly like `Report.AutoBanThreshold`/`History.Duration`.
5. **Handler** (`app/events/admin.go`): `DirectWarnReport` extended — when threshold>0 record warn → count → if `count >= threshold` invoke `executeWarnBan`. New helper bans the user/channel via existing `banUserOrChannel`, respects training/dry/soft-ban, posts an admin-chat notification. **Does not** update spam samples.
6. **Web UI** (`app/webapi/config.go` + `assets/settings.html`): two form fields (`warnThreshold` int, `warnWindow` time.Duration) parsed exactly like `reportAutoBanThreshold` and `reactionsWindow`.

## Technical Details

### CLI flag namespacing

Flags use the project's grouped pattern (matching `Report`, `Files`, `Reactions`, `History`):

```go
Warn struct {
    Threshold int           `long:"threshold" env:"THRESHOLD" default:"0" description:"auto-ban after N warns within window (0=disabled)"`
    Window    time.Duration `long:"window" env:"WINDOW" default:"720h" description:"sliding window for counting warns"`
} `group:"warn" namespace:"warn" env-namespace:"WARN"`
```

User-facing names: `--warn.threshold`, `--warn.window`; env vars: `WARN_THRESHOLD`, `WARN_WINDOW`. This is consistent with `--report.auto-ban-threshold` / `REPORT_AUTO_BAN_THRESHOLD`.

### Storage schema

```sql
-- SQLite
CREATE TABLE IF NOT EXISTS warnings (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  gid TEXT NOT NULL DEFAULT '',
  user_id INTEGER NOT NULL,
  user_name TEXT NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_warnings_gid_user_created
  ON warnings(gid, user_id, created_at);

-- Postgres: id SERIAL, otherwise identical
```

Both dialects use `IF NOT EXISTS`. Idempotent and additive. No changes to existing tables. `migrate` is a no-op (new table only).

Queries are registered via `engine.NewQueryMap()` with typed `engine.DBCmd` constants — same pattern as `Reports` (`app/storage/reports.go:37-104`):

```go
const (
    CmdCreateWarningsTable engine.DBCmd = ...
    CmdCreateWarningsIndexes
    CmdAddWarning
    CmdCountWarningsWithin
    CmdCleanupWarnings
)
var warningsQueries = engine.NewQueryMap().
    Add(CmdCreateWarningsTable, engine.Query{Sqlite: ..., Postgres: ...}).
    Add(CmdCreateWarningsIndexes, engine.Query{Sqlite: ..., Postgres: ...}).
    Add(CmdAddWarning, engine.Query{Sqlite: ..., Postgres: ...}).
    Add(CmdCountWarningsWithin, engine.Query{Sqlite: ..., Postgres: ...}).
    Add(CmdCleanupWarnings, engine.Query{Sqlite: ..., Postgres: ...})
```

### Consumer interface

```go
// app/events/events.go (added near Reports interface)
type Warnings interface {
    Add(ctx context.Context, userID int64, userName string) error
    CountWithin(ctx context.Context, userID int64, window time.Duration) (int, error)
}
```

`gid` is **not** a parameter — the storage struct derives it internally via `r.GID()`, matching `Reports.Add` convention.

moq directive (matches existing style in `app/events/events.go:18-22`):
```
//go:generate moq --out mocks/warnings.go --pkg mocks --with-resets --skip-ensure . Warnings
```

### TelegramListener wiring

`admin` is constructed inside `app/events/listener.go:128` from fields on `TelegramListener`, not directly in `app/main.go`. New fields on `TelegramListener` (matching the flat-field convention used for other admin settings like `WarnMsg`, `TrainingMode`, `SoftBanMode`):

```go
WarnThreshold int           // auto-ban threshold for /warn (0=disabled)
WarnWindow    time.Duration // sliding window for warn counting
Warnings      Warnings      // storage for warn records
```

These get populated in `app/main.go` where the `tgListener := events.TelegramListener{...}` literal is built (around `app/main.go:481`), and copied into the `&admin{...}` literal at `listener.go:128`.

### Handler flow

Insertion point: NEW logic goes after the existing warning-message-post block but BEFORE the final `return errs.ErrorOrNil()` at the end of `DirectWarnReport`.

```text
DirectWarnReport(update):
    [existing] resolve target, skip super-user, delete original msg, delete admin reply, post warn message
    [NEW, before final return]
    if a.warnThreshold == 0 || a.warnings == nil: return errs.ErrorOrNil()  # disabled
    resolve targetID, targetName, channelID:
        if origMsg.SenderChat != nil && origMsg.SenderChat.ID != 0:
            targetID, targetName = origMsg.SenderChat.ID, channelDisplayName(origMsg.SenderChat)
            channelID = origMsg.SenderChat.ID
        elif origMsg.From != nil:
            targetID, targetName = origMsg.From.ID, origMsg.From.UserName
            channelID = 0
        else:
            return errs.ErrorOrNil()  # nil-target guard, never expected from Telegram
    if err := a.warnings.Add(ctx, targetID, targetName); err != nil:
        log [WARN]; return errs.ErrorOrNil()
    count, err := a.warnings.CountWithin(ctx, targetID, a.warnWindow)
    if err != nil: log [WARN]; return errs.ErrorOrNil()
    if count >= a.warnThreshold:
        if err := a.executeWarnBan(targetID, targetName, channelID, count); err != nil:
            errs = multierror.Append(errs, err)
    return errs.ErrorOrNil()

executeWarnBan(userID, userName, channelID, count):
    if dry / training: log + post advisory; return  # do not actually ban
    build banRequest{userID, channelID, chatID: a.primChatID, ...}
    issue ban via banUserOrChannel(banRequest)
    post admin-chat notification: "auto-ban: @user banned after N warnings within <window>"
    # NOTE: no spam-sample update; warn != spam content
```

### Threshold semantics

`count >= threshold` triggers ban (matches `Report.AutoBanThreshold` convention in `app/events/reports.go:191`). So `--warn.threshold=2` means "ban on the 2nd warn within the window".

### Repeated-ban behavior

After a ban fires (count=2 with threshold=2), the corresponding warning rows stay in the table. A subsequent `/warn` on the same already-banned user will find count=3 ≥ 2 and re-trigger the ban path. Telegram's `BanChatMemberConfig` is idempotent (banning an already-banned user is a no-op), so this is safe and intentional — no row deletion on ban. The repeat ban posts a fresh admin notification, which serves as audit/visibility for repeat offenders.

### Cleanup

`Warnings.Add` opportunistically deletes rows where `created_at < now - 1y` to keep the table small. The 1-year bound is a hardcoded storage cap, NOT the user-configured window — we only need cleanup to bound storage growth, not to enforce policy. Same pattern as `Reports.cleanupOldReports`.

### Settings persistence note

Only `Warn.Threshold` belongs in `zeroAwarePaths` (0 means "disabled, do not overwrite"). `Warn.Window` does NOT — its zero value is invalid and is rejected by the startup validation, so the regular merge semantics are correct.

## What Goes Where

- **Implementation Steps** (`[ ]` checkboxes): all code, tests, settings wiring, web UI, README updates.
- **Post-Completion** (no checkboxes): manual smoke test in dev with `--confdb` toggling threshold via web UI to verify persistence round-trip.

## Implementation Steps

### Task 1: Add `Warnings` storage layer

**Files:**
- Create: `app/storage/warnings.go`
- Create: `app/storage/warnings_test.go`

- [x] declare typed `engine.DBCmd` constants `CmdCreateWarningsTable`, `CmdCreateWarningsIndexes`, `CmdAddWarning`, `CmdCountWarningsWithin`, `CmdCleanupWarnings`
- [x] declare `warningsQueries` via `engine.NewQueryMap()` with SQLite and Postgres `engine.Query` entries for each command (mirror `reportsQueries` at `app/storage/reports.go:37-104`)
- [x] create `Warnings` struct embedding `*engine.SQL` and `engine.RWLocker` (mirror `Reports` shape)
- [x] add `NewWarnings(ctx, db) (*Warnings, error)` constructor that calls `engine.InitTable` with `engine.TableConfig` containing `warningsQueries`, the create commands, the index command, and a no-op `migrate` function (matches `reports.go:126`)
- [x] implement `Add(ctx, userID, userName) error`: `Lock`/`defer Unlock`, set `gid = r.GID()`, set `created_at = time.Now()`, pick `CmdAddWarning` query, run via `NamedExecContext`, then call private `cleanupOld(ctx)` to prune rows older than 1 year (storage cutoff, not the user's configured window)
- [x] implement `CountWithin(ctx, userID, window) (int, error)`: `RLock`/`defer RUnlock`, pick `CmdCountWarningsWithin`, adopt placeholders via `r.Adopt`, scan result via `GetContext`
- [x] implement private `cleanupOld(ctx) error` using `CmdCleanupWarnings` (`DELETE FROM warnings WHERE created_at < ?`), log warn on failure but don't propagate
- [x] write tests for `Add` (single insert, multiple inserts, multi-user isolation, multi-gid isolation, both SQLite and Postgres engines)
- [x] write tests for `CountWithin` (zero result, within window, outside window cutoff, mixed inside/outside, both engines)
- [x] write test for `cleanupOld` (verify rows older than 1y are removed on Add, recent rows preserved)
- [x] run `go test -race ./app/storage/...` — must pass before next task

### Task 2: Add `Warnings` consumer interface and moq mock

**Files:**
- Modify: `app/events/events.go`
- Create: `app/events/mocks/warnings.go` (generated)
- Modify: `app/events/admin.go` (struct fields only)

- [x] add `Warnings` interface to `app/events/events.go` next to `Reports` interface with `Add(ctx, userID, userName) error` and `CountWithin(ctx, userID, window) (int, error)` methods
- [x] add `//go:generate moq --out mocks/warnings.go --pkg mocks --with-resets --skip-ensure . Warnings` directive next to existing directives at `events.go:18-22` (matches existing flag style)
- [x] add `warnings Warnings`, `warnThreshold int`, `warnWindow time.Duration` fields to the `admin` struct in `app/events/admin.go`
- [x] regenerate mocks via `go generate ./app/events/...`
- [x] verify mock compiles and existing admin tests still pass: `go test -race ./app/events/...`

### Task 3: Wire CLI flags and Settings

**Files:**
- Modify: `app/main.go`
- Modify: `app/config/settings.go`
- Modify: `app/settings.go`
- Modify: `app/settings_test.go`
- Modify: `app/config/settings_test.go`

- [ ] add `Warn struct {...} \`group:"warn" namespace:"warn" env-namespace:"WARN"\`` block to the opts struct in `app/main.go`, with `Threshold int \`long:"threshold" env:"THRESHOLD" default:"0"...\`` and `Window time.Duration \`long:"window" env:"WINDOW" default:"720h"...\`` (yields `--warn.threshold`/`WARN_THRESHOLD` and `--warn.window`/`WARN_WINDOW`, matching the `Report`, `Files`, `Reactions` group conventions)
- [ ] define `WarnSettings` struct in `app/config/settings.go` with `Threshold int` (`json:"threshold" yaml:"threshold" db:"warn_threshold"`) and `Window time.Duration` (`json:"window" yaml:"window" db:"warn_window"`)
- [ ] add `Warn WarnSettings \`json:"warn" yaml:"warn" db:"warn"\`` field to top-level `Settings` struct
- [ ] add `"Warn.Threshold": true` entry to `zeroAwarePaths` with a `// app/main.go:NN, app/events/admin.go:NN (>0): 0 disables` comment matching existing entries
- [ ] do NOT add `Warn.Window` to `zeroAwarePaths` (zero is invalid, caught by startup validation)
- [ ] add `Warn: config.WarnSettings{Threshold: opts.Warn.Threshold, Window: opts.Warn.Window}` block in `optToSettings` in `app/settings.go`
- [ ] add `appSettings.Warn.Threshold > 0 && appSettings.Warn.Window <= 0` validation in `app/main.go` near the existing `AutoBanThreshold` validation; log fatal if violated
- [ ] write/extend tests in `app/settings_test.go` covering the new CLI→Settings mapping and zero-aware behavior
- [ ] write/extend tests in `app/config/settings_test.go` covering the new struct's serialization (json + yaml + db), `zeroAwarePaths` honor for `Warn.Threshold`, and merge behavior
- [ ] write/extend test in `app/main_test.go` for the validation fatal case (threshold>0 && window<=0)
- [ ] run `go test -race ./app/... -run 'Settings|Config|Validate'` — must pass before next task

### Task 4: Wire `Warnings` through `TelegramListener` into admin construction

**Files:**
- Modify: `app/main.go`
- Modify: `app/events/listener.go`
- Modify: `app/events/listener_test.go` (if existing tests reference TelegramListener literal)

- [ ] in `app/main.go`, after the existing storage init block, construct `warnings, err := storage.NewWarnings(ctx, db)` with proper error handling matching the surrounding pattern
- [ ] add three new flat fields to `TelegramListener` struct in `app/events/listener.go:40-52`: `WarnThreshold int`, `WarnWindow time.Duration`, `Warnings Warnings` (with brief comments matching neighbors)
- [ ] populate these three fields in the `tgListener := events.TelegramListener{...}` literal in `app/main.go` (around line 481) from `appSettings.Warn.Threshold`, `appSettings.Warn.Window`, and the new `warnings` value
- [ ] in `app/events/listener.go:128`, add `warnings: l.Warnings, warnThreshold: l.WarnThreshold, warnWindow: l.WarnWindow` to the `&admin{...}` literal
- [ ] update any existing `TelegramListener` test fixtures that build the struct literal so they still compile (likely just zero-value the new fields)
- [ ] run `go test -race ./app/...` — must pass before next task (admin handler logic still TBD; tests should pass with zero-valued threshold)

### Task 5: Extend `DirectWarnReport` and add `executeWarnBan`

**Files:**
- Modify: `app/events/admin.go`
- Modify: `app/events/admin_test.go`

- [ ] insert new logic in `DirectWarnReport` AFTER the existing warning-message-post block but BEFORE the final `return errs.ErrorOrNil()` (the insertion point is the end of the function, but inside the existing `errs` accumulator scope so any new errors are aggregated)
- [ ] add early return: `if a.warnThreshold == 0 || a.warnings == nil { return errs.ErrorOrNil() }` — keeps current behavior identical when feature disabled
- [ ] resolve `targetID`, `targetName`, `channelID` from `origMsg.SenderChat` (channel: ID + `channelDisplayName`, channelID = SenderChat.ID) or `origMsg.From` (user: ID + UserName, channelID = 0); add nil-target guard returning `errs.ErrorOrNil()` if both are nil/zero
- [ ] call `a.warnings.Add(ctx, targetID, targetName)` — on error log `[WARN]` and return existing errs (warning message already posted, best-effort)
- [ ] call `count, err := a.warnings.CountWithin(ctx, targetID, a.warnWindow)` — on error log `[WARN]` and return
- [ ] if `count >= a.warnThreshold` call `a.executeWarnBan(targetID, targetName, channelID, count)` and `errs = multierror.Append(errs, err)` on error
- [ ] implement `executeWarnBan(userID int64, userName string, channelID int64, count int) error`: respect dry/training/soft-ban modes (mirror `executeAutoBan` in `app/events/reports.go:220-270`); build `banRequest{userID: userID, channelID: channelID, chatID: a.primChatID, ...}`; call existing `banUserOrChannel`; on success post admin-chat notification "auto-ban: @username banned after N warnings within Wh"; **do not** call `bot.UpdateSpam` (warn ≠ spam content)
- [ ] write `admin_test.go` cases extending `TestAdmin_DirectWarnReport`:
  - threshold=0: no Warnings calls, behavior identical to current
  - threshold=2, CountWithin=1: warn posted, no ban call, no admin notification
  - threshold=2, CountWithin=2: warn posted, ban call issued, admin notification posted
  - channel target (SenderChat): ban uses channelID path, notification uses channel display name
  - dry mode: no actual ban call, but admin notification reflects the would-be ban
  - training mode: same as dry — no ban
  - `Warnings.Add` returns error: warn still posted, no count/ban attempted
  - `Warnings.CountWithin` returns error: warn still posted, no ban attempted
  - `a.warnings == nil`: no panic, behaves like threshold=0 (defensive)
- [ ] run `go test -race ./app/events/...` — must pass before next task

### Task 6: Web UI form parsing

**Files:**
- Modify: `app/webapi/config.go`
- Modify: `app/webapi/config_test.go`

- [ ] in the form parser handler, add `warnThreshold` int parsing (`strconv.Atoi`) and `warnWindow` `time.Duration` parsing (`time.ParseDuration`) — mirror the `reportAutoBanThreshold` and `reactionsWindow` handlers exactly
- [ ] write `config_test.go` cases: valid threshold/window persist into `settings.Warn.Threshold`/`Window`, malformed values do not silently zero (existing handlers leave the field unchanged on parse error — match that behavior), missing form fields don't touch the field
- [ ] run `go test -race ./app/webapi/...` — must pass before next task

### Task 7: Web UI form inputs

**Files:**
- Modify: `app/webapi/assets/settings.html`
- Modify or create: an `e2e-ui/` test file (extend existing settings e2e if present, or add `warn_threshold_test.go`)

- [ ] add a "Warn auto-ban" subsection in `settings.html` with two inputs: `warnThreshold` (number, min=0) and `warnWindow` (text, default `720h`), labeled with brief help text
- [ ] follow existing styling/accessibility patterns from the report and reactions sections (e.g., the `reportAutoBanThreshold` input around `settings.html:506`)
- [ ] add an e2e test verifying: render with default values, change threshold + window, save, reload, values persist correctly (project uses Go-based Playwright tests in `e2e-ui/` with build tag `e2e`)
- [ ] run e2e: `make e2e-ui` (equivalent to `go test -v -count=1 -timeout=5m -tags=e2e ./e2e-ui/...`, see `Makefile:48-53`) — must pass before next task

### Task 8: Verify acceptance criteria

- [ ] verify all 6 listed comments from issue #71 are addressed (count tracked, configurable threshold, optional disable)
- [ ] verify default behavior unchanged: with `--warn-threshold=0` (default), no DB writes and no behavior changes vs master
- [ ] verify all existing `/warn` tests still pass without modification
- [ ] run full test suite: `go test -race ./...`
- [ ] run linter: `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0`
- [ ] run formatters: `~/.claude/format.sh` or fallback gofmt + goimports
- [ ] verify race-free: `go test -race ./app/storage/... ./app/events/...`
- [ ] verify coverage: `go test -cover ./...` — coverage of new files at or above project standard

### Task 9: [Final] Update documentation

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md` (if new patterns warrant)

- [ ] add `--warn.threshold` and `--warn.window` to the "All Application Options" section, matching `--help` output exactly (with `WARN_THRESHOLD` and `WARN_WINDOW` env vars)
- [ ] add a short paragraph under the admin commands / spam detection section explaining the warn-driven auto-ban (default off, sliding-window behavior, ban path, repeat-ban behavior, no spam-sample update)
- [ ] update `CLAUDE.md` with a brief subsection under "Spam Detection Architecture" describing the warnings storage and the threshold/window semantics — only if it adds value beyond what the code conveys
- [ ] move this plan to `docs/plans/completed/`

## Post-Completion

**Manual verification**:
- in a dev instance with `--confdb` enabled, set threshold/window via the web UI, verify the `warnings` table is populated when admin issues `/warn`, verify ban fires at the configured count, verify the admin-chat notification text is correct
- verify migration safety: start the new binary against an existing DB, confirm the `warnings` table is created and existing tables are untouched (`PRAGMA table_info(...)` for SQLite, `\d` for Postgres)
- verify rollback: stop the new binary, run the previous version against the modified DB, confirm it still operates normally (the orphan `warnings` table is harmless)
