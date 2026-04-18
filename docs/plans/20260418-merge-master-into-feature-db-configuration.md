# Merge master into feature-db-configuration (PR #294)

## Overview

PR #294 introduces a nested domain-driven `app/config/Settings` model, a DB-backed
config store with encryption, a `--confdb` load/save mode, and replaces the
`webapi` package's flat `Settings` view-model with `*config.Settings`. Since the
merge base `6d70bc55` (2025-05-05) `origin/master` has advanced ~98 commits and
added many new CLI flags, new LLM providers, new event subsystems, new web UI
surfaces, and new view-model fields.

A direct `git merge origin/master` resolves textual conflicts but leaves a
consistent data model as a **silent** open problem: CLI flags would exist that
the domain model doesn't know about, `saveConfigToDB` would drop entire feature
groups on every save, and the web UI would fail to render or accept those
settings. This plan closes that gap.

Core problem: two "Settings" models evolved in parallel. Master kept growing the
flat `webapi.Settings` view-model. PR replaced it with the nested domain model.
We must extend the nested domain model to cover every new master setting
**before** merging, then resolve conflicts deliberately.

Goal: PR #294 mergeable (`mergeStatus: CLEAN`), green tests, green lint, and a
manual `--confdb` round-trip that persists every new feature group across
restart.

## Context (from discovery)

- **Branch**: `feature-db-configuration` (local at origin head `cc641ee5`)
- **Base**: `master` (local at `3e7d4072`)
- **Merge base**: `6d70bc55` (2025-05-05)
- **Drift**: 98 master commits; 12 conflicting files on merge attempt
- **Conflicting files (verified by dry-run merge)**:
  - `.gitignore`, `.golangci.yml`, `README.md`
  - `app/main.go` (15 conflict blocks), `app/main_test.go` (12)
  - `app/webapi/webapi.go` (5), `app/webapi/webapi_test.go` (24)
  - `app/webapi/assets/settings.html` (5)
  - `lib/tgspam/detector.go` (3), `lib/tgspam/detector_test.go` (4), `lib/tgspam/openai.go` (1)
  - `app/events/listener_test.go` (2)
- **New files incoming from master (no conflict, just need wiring)**:
  - `app/events/reports.go`, `app/events/dm_users.go`
  - `app/storage/reports.go`
  - `lib/tgspam/gemini.go`, `lib/tgspam/llm.go`, `lib/tgspam/duplicate.go`
  - `app/webapi/assets/components/dm_users.html`, `assets/manage_dictionary.html`
- **New settings on master not yet in `config.Settings`**:
  - `Delete{JoinMessages, LeaveMessages}` group
  - `Meta.ContactOnly`, `Meta.Giveaway`
  - entire `Gemini{}` LLM provider group (mirrors OpenAI)
  - `LLM{Consensus, RequestTimeout}` group
  - `Duplicates{Threshold, Window}` group
  - `Report{Enabled, Threshold, AutoBanThreshold, RateLimit, RatePeriod}` group
  - top-level `AggressiveCleanup`, `AggressiveCleanupLimit`
- **Drift requiring realignment**:
  - `OpenAI.CustomPrompts` env: master `CUSTOM_PROMPT`, PR `CUSTOM_PROMPTS` → take master
  - `OpenAI.ReasoningEffort`: master `default:"none"` + `choice:"..."`, PR `default:""` → take master
  - `Files.SamplesDataPath`: PR forces `default:"preset"`, master relies on fallback to dynamic → drop PR default
  - `Server.AuthUser`: PR-only addition, master hardcodes user `"tg-spam"` → **drop from this merge** (can re-add in follow-up PR if needed)
- **Schema concern (non-issue)**: settings persist as a single JSON blob wrapped by the Crypter; adding fields is fully additive with zero-value defaults. No migration needed.

## Development Approach

- **Testing approach**: **Regular (code first, then tests)** — this is a merge
  and resolution exercise. For each resolution task we fix code first, then
  extend or adjust tests to cover the newly-threaded fields.
- Complete each task fully before moving to the next.
- Make small, focused commits per task so reviewers can follow resolution
  decisions.
- **CRITICAL: every task MUST include new/updated tests** for code changes in
  that task.
  - adding a new settings group → test round-trip through JSON encode/decode
    and through the encrypted DB store
  - realigning a struct tag → assert flag parses from the new env var name
  - adding a helper method → test true/false boundary cases
  - updating `optToSettings` → assert every new field is copied
- **CRITICAL: all tests must pass before starting next task** — no exceptions.
- **CRITICAL: update this plan file when scope changes during implementation**.
- Run tests after each change: `go test -race ./...`
- **Do not** reshape master behavior during conflict resolution. When master
  and PR disagree on semantics that are not about the settings model itself,
  prefer master (it's the production baseline).
- Preserve PR's `--confdb`, encryption, store, and domain-model architecture.
  These are the reason the PR exists.

## Testing Strategy

- **Unit tests**: required for every task. Go table-driven tests with testify.
  - `app/config/settings_test.go`: helper methods (`IsGeminiEnabled`,
    `IsReportEnabled`, `IsDuplicatesEnabled`, extended `IsMetaEnabled`).
  - `app/config/store_test.go`: DB round-trip of every new group
    (Delete, Gemini, LLM, Duplicates, Report, AggressiveCleanup).
  - `app/main_test.go`: `TestOptToSettings` coverage for all new groups and for
    credential handling on Gemini.Token; `TestApplyCLIOverrides` unchanged
    unless we extend overrides.
  - `app/webapi/config_test.go`: `updateSettingsFromForm` with new form fields
    for each group.
  - `lib/tgspam/*`: no new logic; verify master's tests still pass after
    resolution.
- **Manual smoke test (Pass 3)**: start with `--confdb` against a scratch
  SQLite, toggle one setting from each new group via the web UI, restart,
  confirm the settings survive.
- **No e2e tests**: project has `e2e-ui/` Playwright tests; we do not extend
  them here because this is not a feature change, only a merge. If the
  existing e2e suite breaks we fix it.
- **Regression gate**: `go test -race ./...` must be green before committing
  any task, and `golangci-lint run` (v2 config from master) must report zero
  issues.

## Progress Tracking

- Mark completed items with `[x]` immediately when done.
- Add newly discovered tasks with `➕` prefix.
- Document blockers with `⚠️` prefix.
- Update plan if scope changes.
- Keep plan in sync with actual work done.

## Solution Overview

Four sequential passes on the PR branch:

1. **Pass 0 — Housekeeping** (cheap, unblocks later passes).
2. **Pass 1 — Extend domain model in advance** of the merge. The key insight:
   shape the destination data model first so Pass 2 is pure porting, not
   invention. This moves data-model design decisions out of merge commits and
   into their own auditable commits.
3. **Pass 2 — Merge master** and resolve conflicts in dependency order (leaves
   first, roots last): trivial files → `lib/tgspam` → `app/main.go` options
   and wiring → `app/webapi/webapi.go` Config → `settings.html` template →
   `app/webapi/config.go` handlers → tests.
4. **Pass 3 — Validation** (build, tests, lint, manual `--confdb` round-trip).
5. **Pass 4 — Documentation** (`db-conf.md`, `README.md`).

Rationale for Pass 1 happening before the merge: if we extend `config.Settings`
inside the merge commit, the resulting diff conflates "resolve conflict" with
"add new domain surface", making review nearly impossible. By landing the
domain extension first as its own pre-merge commit, the merge commit becomes
small and mostly mechanical.

## Technical Details

### New domain structures to add to `app/config/settings.go`

All new structs follow the existing tag convention: `json` snake_case, `yaml`
snake_case, `db` prefixed by group. Example skeleton:

```go
type DeleteSettings struct {
    JoinMessages  bool `json:"join_messages"  yaml:"join_messages"  db:"delete_join_messages"`
    LeaveMessages bool `json:"leave_messages" yaml:"leave_messages" db:"delete_leave_messages"`
}

type GeminiSettings struct {
    Token              string   `json:"token"                yaml:"token"                db:"gemini_token"`
    Veto               bool     `json:"veto"                 yaml:"veto"                 db:"gemini_veto"`
    Prompt             string   `json:"prompt"               yaml:"prompt"               db:"gemini_prompt"`
    CustomPrompts      []string `json:"custom_prompts"       yaml:"custom_prompts"       db:"gemini_custom_prompts"`
    Model              string   `json:"model"                yaml:"model"                db:"gemini_model"`
    MaxTokensResponse  int32    `json:"max_tokens_response"  yaml:"max_tokens_response"  db:"gemini_max_tokens_response"` // int32 matches google.golang.org/genai SDK type
    MaxSymbolsRequest  int      `json:"max_symbols_request"  yaml:"max_symbols_request"  db:"gemini_max_symbols_request"`
    RetryCount         int      `json:"retry_count"          yaml:"retry_count"          db:"gemini_retry_count"`
    HistorySize        int      `json:"history_size"         yaml:"history_size"         db:"gemini_history_size"`
    CheckShortMessages bool     `json:"check_short_messages" yaml:"check_short_messages" db:"gemini_check_short_messages"`
}

type LLMSettings struct {
    Consensus      string        `json:"consensus"       yaml:"consensus"       db:"llm_consensus"`
    RequestTimeout time.Duration `json:"request_timeout" yaml:"request_timeout" db:"llm_request_timeout"`
}

type DuplicatesSettings struct {
    Threshold int           `json:"threshold" yaml:"threshold" db:"duplicates_threshold"`
    Window    time.Duration `json:"window"    yaml:"window"    db:"duplicates_window"`
}

type ReportSettings struct {
    Enabled          bool          `json:"enabled"            yaml:"enabled"            db:"report_enabled"`
    Threshold        int           `json:"threshold"          yaml:"threshold"          db:"report_threshold"`
    AutoBanThreshold int           `json:"auto_ban_threshold" yaml:"auto_ban_threshold" db:"report_auto_ban_threshold"`
    RateLimit        int           `json:"rate_limit"         yaml:"rate_limit"         db:"report_rate_limit"`
    RatePeriod       time.Duration `json:"rate_period"        yaml:"rate_period"        db:"report_rate_period"`
}
```

Extend `MetaSettings` with `ContactOnly bool` and `Giveaway bool`.

Extend top-level `Settings` with:

```go
Delete            DeleteSettings
Gemini            GeminiSettings
LLM               LLMSettings
Duplicates        DuplicatesSettings
Report            ReportSettings
AggressiveCleanup      bool `json:"aggressive_cleanup"       yaml:"aggressive_cleanup"       db:"aggressive_cleanup"`
AggressiveCleanupLimit int  `json:"aggressive_cleanup_limit" yaml:"aggressive_cleanup_limit" db:"aggressive_cleanup_limit"`
```

### Drift realignment in PR options struct (done in Pass 1)

- `OpenAI.CustomPrompts`: change env tag `CUSTOM_PROMPTS` → `CUSTOM_PROMPT`
- `OpenAI.ReasoningEffort`: set `default:"none"` + `choice:"none" choice:"low" choice:"medium" choice:"high"`
- `Files.SamplesDataPath`: drop `default:"preset"`; match master's fallback semantics
- `Server.AuthUser`: remove the PR-added field (options struct and `ServerSettings`); keep master's behavior of hardcoded `"tg-spam"` user

### Helper methods on `Settings`

Extend `IsMetaEnabled` to include `ContactOnly`, `Giveaway` — this is
unambiguously needed because `IsMetaEnabled` already exists and callers rely
on it (template and handler).

For `IsGeminiEnabled`/`IsReportEnabled`/`IsDuplicatesEnabled`: **defer the
decision to Task 5 after auditing master's usage**. Master computes these as
flat booleans in the web handler and passes them to the template via
`{{if .GeminiEnabled}}`. Keeping master's render semantics unchanged argues
for computing the booleans in the handler (Task 11) rather than adding new
helper methods. Add a helper only if a non-webapi caller needs it.

### Wiring in `optToSettings` (done in Pass 2)

Every new group must be populated. Credential handling for `Gemini.Token`
mirrors the existing pattern for `OpenAI.Token`:

```go
settings.Gemini.Token = opts.Gemini.Token
```

### webapi.Config unification

Master added `Dictionary`, `DMUsersProvider`, `BotUsername` to the flat
`webapi.Settings`. These are not persisted config; they are runtime bindings
(DB accessors) or transient UI values. Move them directly onto `webapi.Config`
alongside `AppSettings *config.Settings`. The template references
`{{.BotUsername}}` etc. as top-level config fields, not through
`{{.AppSettings.*}}`.

### settings.html porting

The PR's `htmlSettingsHandler` (`app/webapi/webapi.go:796-816`) embeds
`*config.Settings` **anonymously** into the template data struct:

```go
data := struct {
    *config.Settings
    LuaAvailablePlugins []string
    ...
}{
    Settings: s.AppSettings,
    ...
}
```

Embedded struct fields are promoted, so the template references nested
settings **directly** as `{{.Gemini.Model}}`, `{{.Meta.ContactOnly}}`,
`{{.OpenAI.Prompt}}` — **not** `{{.AppSettings.Gemini.Model}}`. Master's flat
fields like `{{.GeminiModel}}` must become `{{.Gemini.Model}}` (lose the flat
name, keep the group nesting).

For booleans that master computed in the handler (e.g. `GeminiEnabled`,
`MetaEnabled`), prefer computing the equivalent booleans directly at the
handler level so the template's `{{if .GeminiEnabled}}` guards can stay
byte-identical to master. Do **not** refactor the template to call helper
methods; keep behavior indistinguishable from master's template path.

For `{{.BotUsername}}` (master-added, used in the reports tab etc.): since the
data struct is already a hybrid, add `BotUsername string` as a named field on
the inline struct and populate it in the handler — same pattern master uses
in the flat `webapi.Settings`. Do not add it to `webapi.Config` (connection
wiring) and do not add it to `config.Settings` (not persisted).

### `webapi/config.go` handlers

`updateSettingsFromForm` must read new form fields. The handler is
field-per-field; extend it additively for each new group. Keep the
`StorageTimeout` transient-skip pattern.

## What Goes Where

- **Implementation Steps** (`[ ]` checkboxes): everything codified in this
  repo — domain model, main.go wiring, webapi handlers/template, tests, docs.
- **Post-Completion** (no checkboxes): PR description update on GitHub,
  manual `--confdb` round-trip verification, force-push the merged branch,
  respond to PR review.

## Implementation Steps

### Task 1: Remove coverage.html and align .gitignore with master

**Files:**
- Delete: `coverage.html`
- Modify: `.gitignore`

- [x] `git rm coverage.html`
- [x] merge `.gitignore` conflict: take union of master's entries and PR's additions (`.claude/`, `issue.md`, `*.bak`, `*.local`)
- [x] add `coverage.html` and `coverage.out` to `.gitignore` if not already covered
- [x] commit with message `chore: remove coverage.html and align .gitignore`
- [x] verify `git status` clean afterwards

No tests needed for this task (housekeeping only), but verify `go build ./...` still succeeds.

### Task 2: Adopt master's golangci-lint v2 config

**Files:**
- Modify: `.golangci.yml`

- [x] copy `.golangci.yml` wholesale from `origin/master`
- [x] run `golangci-lint run` on the current PR tree (pre-merge). If new issues surface on PR-only files (`app/config/`, `app/webapi/config.go`) — e.g. `revive`, `gocyclo`, `nestif` — capture them as a task-list note; fix in-place only if trivial, otherwise add a `➕ follow-up` entry to this plan
- [x] if the PR branch had a `nestif` disable, re-disable only if lint output confirms real issues; do not carry it over blindly
- [x] commit with message `chore: upgrade golangci-lint config to v2 format from master`

Task 2 notes: master's tree lints cleanly (0 issues) under the new config, so all issues against the PR tree live on files that will be replaced by the upcoming merge (Tasks 6-13). PR-only files (`app/config/*`, `app/webapi/config*.go`) produced 50 trivial issues — 13 `gocritic` (http.NoBody), 34 `testifylint` (assert.Len, require.Error, InEpsilon, Empty, Contains), 1 `gofmt`, 1 `lll`, 1 `prealloc` — all fixed in-place. The PR branch had `# - nestif` commented out, which matches master (nestif not enabled), so nothing to carry over. Total lint dropped from 838 to 788; remaining 788 are all in master-overlapping files and will disappear with the Task 6 merge.

No tests. Lint must exit zero.

### Task 3: Add new domain setting structs to config.Settings

**Files:**
- Modify: `app/config/settings.go`
- Modify: `app/config/settings_test.go`

- [x] add `DeleteSettings` struct with `JoinMessages`, `LeaveMessages` + json/yaml/db tags
- [x] add `GeminiSettings` struct mirroring `OpenAISettings` shape (Token, Veto, Prompt, CustomPrompts, Model, MaxTokensResponse `int32`, MaxSymbolsRequest, RetryCount, HistorySize, CheckShortMessages) + tags
- [x] add `LLMSettings` struct with `Consensus string`, `RequestTimeout time.Duration` + tags
- [x] add `DuplicatesSettings` struct with `Threshold int`, `Window time.Duration` + tags
- [x] add `ReportSettings` struct with `Enabled`, `Threshold`, `AutoBanThreshold`, `RateLimit`, `RatePeriod time.Duration` + tags
- [x] add top-level fields to `Settings`: `Delete`, `Gemini`, `LLM`, `Duplicates`, `Report`, `AggressiveCleanup bool`, `AggressiveCleanupLimit int` with tags
- [x] write JSON round-trip test covering every new group (marshal → unmarshal → deep-equal)
- [x] write YAML round-trip test likewise
- [x] run `go test ./app/config/...` — must pass before next task

### Task 4: Extend MetaSettings and drop Server.AuthUser from domain model

**Files:**
- Modify: `app/config/settings.go`
- Modify: `app/config/settings_test.go`

**Scope limited to `app/config/` only.** Options struct drift in `app/main.go`
is handled in Task 8 where we are rewriting options from master's baseline
anyway. Landing options-struct edits in Pass 1 would create extra churn inside
the Pass 2 merge conflict blocks.

- [ ] add `ContactOnly bool` and `Giveaway bool` to `MetaSettings` with tags
- [ ] remove `AuthUser` field from `ServerSettings` (PR-only addition that master never had)
- [ ] grep for and remove any references to `ServerSettings.AuthUser` inside `app/config/` (tests, etc.)
- [ ] add test case in `settings_test.go` asserting `MetaSettings` JSON/YAML round-trip includes `ContactOnly` and `Giveaway`
- [ ] run `go test ./app/config/...` — must pass before next task

### Task 4a: Update crypt.go sensitive-fields plumbing

**Files:**
- Modify: `app/config/crypt.go`
- Modify: `app/config/crypt_test.go`

`crypt.go` has hand-rolled switch-cases per encrypted field constant. Adding
`Gemini.Token` as a sensitive field requires a new constant and matching cases
in both `EncryptSensitiveFields` and `DecryptSensitiveFields`. Removing
`Server.AuthUser` requires removing its constant and its two switch-cases.
Missing this step will leave `Gemini.Token` stored in plaintext in the DB and
leave a dead `FieldServerAuthUser` constant.

- [ ] remove `FieldServerAuthUser` constant from `crypt.go`
- [ ] remove the `case FieldServerAuthUser:` branches in `EncryptSensitiveFields` (line ~192) and `DecryptSensitiveFields` (line ~256)
- [ ] remove `FieldServerAuthUser` from `defaultSensitiveFields()` if present
- [ ] add `FieldGeminiToken = "gemini.token"` constant alongside existing field constants
- [ ] add matching `case FieldGeminiToken:` branches mirroring `FieldOpenAIToken` — encrypt `settings.Gemini.Token`, decrypt likewise
- [ ] add `FieldGeminiToken` to `defaultSensitiveFields()`
- [ ] update `crypt_test.go`: drop any `Server.AuthUser` encrypt/decrypt assertions; add round-trip test that sets `Gemini.Token`, encrypts, verifies `ENC:` prefix, decrypts, asserts original value
- [ ] run `go test ./app/config/... -race` — must pass before next task

### Task 5: Extend IsMetaEnabled; add helpers only if callers exist

**Files:**
- Modify: `app/config/settings.go`
- Modify: `app/config/settings_test.go`

- [ ] extend `IsMetaEnabled()` to include `s.Meta.ContactOnly || s.Meta.Giveaway` (unambiguously needed: existing helper; callers depend on it)
- [ ] update `IsMetaEnabled` test with cases for Giveaway-only and ContactOnly-only
- [ ] grep master for `GeminiEnabled`, `ReportEnabled`, `DuplicatesEnabled` usage locations; if they are only used inside `webapi` handlers (computed via bools, not called as methods), **skip the helper methods** and compute equivalents in the handler to keep master-identical semantics
- [ ] only if a non-webapi caller needs one: add the corresponding helper (`IsGeminiEnabled`, `IsReportEnabled`, `IsDuplicatesEnabled`) with a table-driven test covering true/false
- [ ] run `go test ./app/config/...` — must pass before next task

---

**Pass 1 complete. Commit as one commit (or separate commits per Task 3/4/4a/5 if preferred) with messages like `feat(config): extend domain model for master feature groups`. All Pass 1 edits are confined to `app/config/` and therefore cannot conflict with the upcoming `git merge origin/master` — master has no commits touching `app/config/` since the merge base. Pass 2 starts here.**

---

### Task 6: Start merge and resolve trivial conflicts

**Files:**
- Modify: `.gitignore` (already done in Task 1; just accept incoming)
- Modify: `.golangci.yml` (already done in Task 2; just accept incoming)
- Modify: `README.md`

- [ ] run `git merge origin/master` (expect conflicts)
- [ ] resolve `.gitignore` — should auto-resolve after Task 1, or `git checkout --theirs` then re-apply PR additions
- [ ] resolve `.golangci.yml` — take master version
- [ ] resolve `README.md` — base on master (it has the up-to-date All Application Options block), re-add PR's `--confdb`, `--confdb-encrypt-key` sections; drop any mention of `--server.auth-user` since we removed it
- [ ] verify no conflict markers: `grep -rn '<<<<<<<' .` reports nothing in these files
- [ ] **do not commit the merge yet** — Tasks 7 through 13 complete the resolution

No tests for this task (merge plumbing). README is documentation only.

### Task 7: Resolve lib/tgspam conflicts

**Files:**
- Modify: `lib/tgspam/openai.go`
- Modify: `lib/tgspam/detector.go`
- Modify: `lib/tgspam/detector_test.go`
- Modify: `lib/tgspam/classifier.go` (auto-merged, verify)
- Modify: `lib/tgspam/classifier_test.go` (auto-merged, verify)

- [ ] resolve `lib/tgspam/openai.go` by taking master; master absorbed equivalent behaviors during Gemini/LLM work
- [ ] resolve `lib/tgspam/detector.go` by taking master; preserve any PR-specific helper only if it's referenced elsewhere (grep first)
- [ ] resolve `lib/tgspam/detector_test.go` by taking master, then re-add any PR-specific test helpers still needed
- [ ] run `go build ./lib/...` — must succeed
- [ ] run `go test -race ./lib/...` — must pass before next task
- [ ] verify test coverage not regressed vs. master baseline via `go test -cover ./lib/...`

### Task 8: Resolve app/main.go options struct and top-level wiring

**Files:**
- Modify: `app/main.go`

This is where all options-struct drift fixes land (moved here from Task 4 so
Pass 1 stays confined to `app/config/`). `app/main.go` already has 15 conflict
blocks; consolidating drift fixes here avoids re-touching them.

- [ ] start from master's options struct (it has Delete/Gemini/LLM/Duplicates/Report/AggressiveCleanup)
- [ ] re-add PR's top-level `ConfigDB bool` and `ConfigDBEncryptKey string` fields
- [ ] apply OpenAI drift realignment: master already uses `env:"CUSTOM_PROMPT"` singular and `ReasoningEffort default:"none"` with choices — accepting master's baseline is the correct fix (no further edit)
- [ ] remove `Server.AuthUser` from options struct if present in either side (PR side); replace any `opts.Server.AuthUser` literal with the string `"tg-spam"`
- [ ] accept master's `Files.SamplesDataPath` (no default, falls back to dynamic) — no `default:"preset"` anywhere
- [ ] verify PR's `initLuaPlugins` helper still exists at `app/main.go:664` (confirmed on PR branch pre-merge); route master's plugin-init flow through it — this is a pure rename, not behavioral refactor
- [ ] preserve master's `masked := []string{opts.Telegram.Token, opts.OpenAI.Token, opts.Gemini.Token}` line for token masking
- [ ] preserve master's `reportsStore` creation gate (`if opts.Report.Enabled`) and wiring
- [ ] preserve master's `Delete.JoinMessages/LeaveMessages` handling in the listener wiring
- [ ] preserve master's `AggressiveCleanup*` threading into the spam handler
- [ ] preserve master's listener config `StartupMsg: settings.Message.Startup` and any `StartupMessageEnabled` derived bool (verified at `app/main.go:392` on PR branch pre-merge; keep the same line post-merge)
- [ ] keep PR's `--confdb` load path (`loadConfigFromDB` → `applyCLIOverrides`) at the correct place in `main`
- [ ] run `go build ./app/...` — must compile cleanly
- [ ] tests for main.go are updated in Task 13; for now only compile must succeed

### Task 9: Extend optToSettings, applyCLIOverrides, and DB save/load

**Files:**
- Modify: `app/main.go`
- Modify: `app/main_test.go`

- [ ] extend `optToSettings` to populate `settings.Delete` from `opts.Delete.*`
- [ ] extend `optToSettings` to populate `settings.Gemini` from `opts.Gemini.*` (all fields)
- [ ] extend `optToSettings` to populate `settings.LLM` from `opts.LLM.*`
- [ ] extend `optToSettings` to populate `settings.Duplicates` from `opts.Duplicates.*`
- [ ] extend `optToSettings` to populate `settings.Report` from `opts.Report.*`
- [ ] extend `optToSettings` to populate `settings.AggressiveCleanup` and `settings.AggressiveCleanupLimit`
- [ ] extend `optToSettings` to populate `settings.Meta.ContactOnly` and `settings.Meta.Giveaway`
- [ ] after the struct literal, add credential threading: `settings.Gemini.Token = opts.Gemini.Token` alongside the existing OpenAI.Token line
- [ ] verify `applyCLIOverrides` does not accidentally wipe Gemini.Token on DB load — extend if needed with the same guard used for OpenAI.Token
- [ ] extend `TestOptToSettings` in `main_test.go` with a single table case covering every new group (Delete, Gemini incl. Token, LLM, Duplicates, Report, AggressiveCleanup, Meta.ContactOnly, Meta.Giveaway) with non-zero values — one case, not one per group
- [ ] run `go test ./app -run TestOptToSettings -race` — must pass before next task

### Task 10: Resolve app/webapi/webapi.go — unify Config

**Files:**
- Modify: `app/webapi/webapi.go`
- Modify: `app/webapi/mocks/` (regenerate via `go generate` if interfaces changed)

- [ ] remove master's flat `Settings` struct entirely from `webapi.go` (PR already did this on the PR side; conflict-resolve by taking PR side)
- [ ] add master's new `Dictionary` and `DMUsersProvider` interfaces onto `webapi.go` (not on `config.Settings` — they are runtime deps)
- [ ] add `BotUsername string` to `webapi.Config` as a top-level runtime field; plumb it from `main.go` (resolved from the Telegram bot's `GetMe`) into the `Server`
- [ ] verify `webapi.Config` keeps PR's `AppSettings *config.Settings`, `SettingsStore`, `ConfigDBMode`
- [ ] ensure `AuthUser` reference is gone (we dropped it in Task 4)
- [ ] if `Dictionary` / `DMUsersProvider` interfaces are newly added locally, run `go generate ./app/webapi/...` to regenerate mocks
- [ ] run `go build ./app/webapi/...` — must compile
- [ ] tests for webapi.go wait for Task 13

### Task 11: Port settings.html template to PR's promoted-field pattern

**Files:**
- Modify: `app/webapi/assets/settings.html`
- Modify: `app/webapi/webapi_test.go` (template render smoke test)

**Important correction**: PR template does **not** use `{{.AppSettings.*}}`.
PR's `htmlSettingsHandler` embeds `*config.Settings` anonymously in the
template data struct, so nested fields are promoted. Existing PR template
already uses `{{.OpenAI.Model}}`, `{{.Meta.LinksLimit}}`, etc. — the merge
target shape.

Before starting: run
`rg -n '{{ *\.(Gemini|LLMConsensus|MetaContactOnly|MetaGiveaway|BotUsername|Delete|Duplicates|Report|AggressiveCleanup)' app/webapi/assets/settings.html`
against **both branches** to enumerate every template reference needing
translation. Use the output as the authoritative list for this task.

- [ ] resolve conflict by taking PR's base template (promoted-field style) as the floor
- [ ] rewrite every master-added `{{.GeminiFoo}}` to `{{.Gemini.Foo}}` (keep group nesting, drop the flat prefix)
- [ ] rewrite `{{.LLMConsensus}}` to `{{.LLM.Consensus}}`
- [ ] rewrite `{{.MetaContactOnly}}` to `{{.Meta.ContactOnly}}`, `{{.MetaGiveaway}}` to `{{.Meta.Giveaway}}`
- [ ] rewrite any `{{.DuplicatesThreshold}}`/`{{.DuplicatesWindow}}` to `{{.Duplicates.Threshold}}`/`{{.Duplicates.Window}}`
- [ ] rewrite Report block references to `{{.Report.Enabled}}`, `{{.Report.Threshold}}`, `{{.Report.AutoBanThreshold}}`, `{{.Report.RateLimit}}`, `{{.Report.RatePeriod}}`
- [ ] rewrite Delete block references to `{{.Delete.JoinMessages}}`, `{{.Delete.LeaveMessages}}`
- [ ] rewrite `{{.AggressiveCleanup}}` / `{{.AggressiveCleanupLimit}}` to match their promoted names (top-level fields on `*config.Settings`, so syntax is unchanged — verify)
- [ ] for master's `{{if .GeminiEnabled}}` guards: add `GeminiEnabled bool` (and parallel `ReportEnabled`, `DuplicatesEnabled`) as **named fields on the inline template data struct** in `htmlSettingsHandler`, populate in the handler from `s.AppSettings.Gemini.Token != ""` etc. Keep template guards byte-identical to master (`{{if .GeminiEnabled}}`). Do not introduce method calls in templates.
- [ ] for `{{.BotUsername}}`: add `BotUsername string` as a named field on the inline template data struct in `htmlSettingsHandler`; populate from new `s.BotUsername` runtime binding on `webapi.Server` / `Config` (Task 10)
- [ ] preserve the hidden `saveToDb` input (PR addition) inside the form
- [ ] grep for remaining conflict markers: `grep -n '<<<<<<<' app/webapi/assets/settings.html` — must be empty
- [ ] grep for stale flat references: `rg -n '{{ *\.(Gemini[A-Z]|Duplicates[A-Z]|Report[A-Z]|Delete[A-Z]|Meta(Contact|Giveaway|Links|Image|Video|Audio|Keyboard|Username|Forwarded|Mentions))' app/webapi/assets/settings.html` — must be empty (the computed booleans on the data struct are the only exception; they have no trailing group)
- [ ] add test `TestHtmlSettingsHandler_RendersAllNewSections`: build a fully-populated `*config.Settings` (non-zero values for every new group), render through `htmlSettingsHandler`, assert the response HTML contains one sentinel string per new group ("Gemini", "Duplicates", "Report", "Aggressive Cleanup", "Contact Only", "Giveaway"). Catches template execution errors that would otherwise only fire in production.
- [ ] run `go test ./app/webapi -race -run TestHtmlSettingsHandler` — must pass before next task

### Task 12: Extend webapi/config.go form/JSON handlers

**Files:**
- Modify: `app/webapi/config.go`
- Modify: `app/webapi/config_test.go`

- [ ] in `updateSettingsFromForm`, add read blocks for each new form field group:
  - `delete.join_messages`, `delete.leave_messages`
  - `meta.contact_only`, `meta.giveaway`
  - `gemini.*` (mirror openai form handling)
  - `llm.consensus`, `llm.request_timeout`
  - `duplicates.threshold`, `duplicates.window`
  - `report.enabled`, `report.threshold`, `report.auto_ban_threshold`, `report.rate_limit`, `report.rate_period`
  - `aggressive_cleanup`, `aggressive_cleanup_limit`
- [ ] in the JSON update handler (`updateConfigHandler`), ensure the unmarshaled `config.Settings` is saved without field loss (JSON handler inherits new fields for free, but add assertion tests)
- [ ] add table-driven test `TestUpdateSettingsFromForm_NewGroups` covering each new group's form parsing
- [ ] add test `TestUpdateConfigHandler_RoundTrip_NewGroups` posting JSON with each group and asserting the stored `*config.Settings` matches
- [ ] run `go test ./app/webapi -race` — must pass before next task

### Task 13: Resolve test file conflicts

**Files:**
- Modify: `app/main_test.go`
- Modify: `app/webapi/webapi_test.go`
- Modify: `lib/tgspam/detector_test.go` (already done in Task 7, verify)
- Modify: `app/events/listener_test.go`

- [ ] resolve `app/main_test.go` by taking master where tests cover master's feature set (Gemini/Report/Duplicates/AggressiveCleanup/Delete); re-add PR's `TestSaveAndLoadConfig`, `TestApplyCLIOverrides`, `TestOptToSettings`, encryption tests
- [ ] extend `TestSaveAndLoadConfig` to set non-zero values on every new group and assert they round-trip
- [ ] resolve `app/webapi/webapi_test.go` by taking master tests for render/routes; re-add PR tests for `TestServer_configHandlers` (updateConfigHandler, saveConfigHandler, loadConfigHandler, deleteConfigHandler) and `TestHtmlSettingsHandler` with `AppSettings` seeded for each new group
- [ ] resolve `app/events/listener_test.go` trivial conflicts (likely additive on both sides)
- [ ] run `go test -race ./...` — must be fully green before next task
- [ ] run `go test -cover ./app/config/... ./app/webapi/... ./app/` — coverage must not regress vs PR branch pre-merge

---

**Pass 2 complete. Commit merge with message `Merge branch 'master' into feature-db-configuration`. Pass 3 starts here.**

---

### Task 14: Validation — build, full test suite, lint

**Files:** none (verification only)

- [ ] `go build ./...` — must succeed
- [ ] `go test -race ./...` — all green
- [ ] `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0` — zero issues
- [ ] `command -v unfuck-ai-comments >/dev/null && unfuck-ai-comments run --fmt --skip=mocks ./...` or fallback to `gofmt -s -w` and `goimports -w` per CLAUDE.md
- [ ] if any failure: fix at root, do not suppress
- [ ] record test coverage percentage in task notes

No new tests in this task (it is the test gate).

### Task 15: Extend config DB round-trip integration test

**Files:**
- Modify: `app/config/store_test.go`

- [ ] inspect `store_test.go` to confirm whether the existing test matrix runs SQLite only or SQLite+PostgreSQL — record finding in task note so we know the reach of the round-trip
- [ ] extend `TestSaveAndLoadConfig` (or add new subtest) that constructs a `*config.Settings` with every field in every new group populated to non-default values
- [ ] save to the encrypted store, reload into a fresh `*Settings`, assert deep equality
- [ ] run the test under every engine the existing matrix supports; do not add a new engine case
- [ ] add assertion that encrypted `Gemini.Token` is stored with `ENC:` prefix and decodes back to plaintext
- [ ] run `go test ./app/config/... -race` — must pass before next task

### Task 16: Manual --confdb round-trip smoke test

**Files:** none (manual verification)

- [ ] build: `go build -o /tmp/tg-spam ./app`
- [ ] start with scratch DB: `CONFDB_ENCRYPT_KEY=test-key-test-key-test-key /tmp/tg-spam --confdb --db=/tmp/tg-spam-smoke.db --dry --token=dummy --server.enabled`
- [ ] open web UI at `http://localhost:8080`, login with auto-generated password from startup log
- [ ] toggle one setting from each new group: enable `Delete Join Messages`, enable `Report`, set `Duplicates Threshold=3`, toggle `Meta Contact Only`, toggle `Meta Giveaway`, set `Aggressive Cleanup Limit=50`, configure Gemini token (fake), set LLM Consensus to `all`
- [ ] save settings
- [ ] kill the process, restart with same flags
- [ ] verify every toggled setting survived the restart by checking the settings page
- [ ] document any failures as `⚠️` entries in this plan and fix before continuing

No code changes in this task (unless smoke test reveals a bug, in which case
fix it and add a regression test).

### Task 17: Update db-conf.md

**Files:**
- Modify: `db-conf.md`

- [ ] add to the list of persisted settings: `Delete.*`, `Gemini.*`, `LLM.*`, `Duplicates.*`, `Report.*`, `AggressiveCleanup*`, `Meta.ContactOnly`, `Meta.Giveaway`
- [ ] note schema compatibility: JSON-blob storage is additive; old blobs decode cleanly with zero-value defaults; no migration needed
- [ ] note that PR-preview users who ran with `Server.AuthUser` set will have a leftover `server_auth_user` field in their encrypted blob after this merge. The field is ignored by `json.Unmarshal` (no matching struct field) with no error; it stays as dead data in the encrypted blob until the next save, which overwrites the blob entirely. Document explicitly so users don't panic.
- [ ] document credential precedence: `Gemini.Token` follows the same CLI-over-DB precedence as `OpenAI.Token`, encrypted at rest with the same prefix
- [ ] verify examples in the file still compile against the final API shape

No tests (docs only). Grep for stale references: `grep -n 'AuthUser\|CUSTOM_PROMPTS\|preset' db-conf.md` — must return nothing.

### Task 18: Update README.md

**Files:**
- Modify: `README.md`

- [ ] confirm the merged `README.md` from Task 6 has master's `All Application Options` block intact
- [ ] append the `--confdb` and `--confdb-encrypt-key` lines in the correct position
- [ ] ensure no dangling references to `--server.auth-user`
- [ ] update the descriptive "Database Configuration" section (PR added it) with examples that include at least one new group (e.g., Gemini)
- [ ] run a visual diff against `--help` output: `/tmp/tg-spam --help 2>&1` must match the README options section
- [ ] no tests (docs only)

### Task 19: Verify acceptance criteria and audit master feature adoption

**Files:** none (audit + verification only)

Before ticking off merge-completion checklist, perform a **deep adoption audit** to
prove no master functionality was missed, silently dropped, or broken during
the merge. Textual conflict resolution is not the same as behavioral
preservation — this task is the safety net.

**Adoption audit (deep):**

- [ ] list every master commit in the merge range: `git log --oneline 6d70bc55..origin/master` → save as a checklist
- [ ] for each commit that touched `app/`, `lib/`, `app/webapi/assets/`, `completions/`, `README.md`, open it with `git show <sha>` and verify the feature/fix is present in the merged working tree:
  - follow symbols: if commit added a function `X`, `rg -n 'func X\b'` on current tree; if it added a CLI flag, `./tg-spam --help | grep <flag>`; if it added a template section, `rg -n <sentinel>` in `app/webapi/assets/settings.html`
  - mark each commit OK/MISSING/BROKEN in the checklist
  - any MISSING/BROKEN entry blocks acceptance until fixed
- [ ] run `git diff origin/master -- app/ lib/ app/webapi/assets/ completions/ README.md db-conf.md Dockerfile Makefile .github/` and review hunk-by-hunk. Every `-` hunk (removed from master in our tree) must have a justification: either a PR-intended replacement or the "drop decision" documented in this plan (e.g., `Server.AuthUser`). Any unexplained removal is a regression and blocks acceptance.
- [ ] cross-check every new CLI flag from master against `./tg-spam --help` output: build the binary, run `./tg-spam --help > /tmp/help.txt`, `git show origin/master:app/main.go | rg 'long:"[^"]+"' -o | sort -u > /tmp/master-flags.txt`, `rg 'long:"[^"]+"' -o app/main.go | sort -u > /tmp/merged-flags.txt`, `diff /tmp/master-flags.txt /tmp/merged-flags.txt` — expected differences: +`--confdb`, +`--confdb-encrypt-key`, -`--server.auth-user` (intentional drops/adds); no other differences allowed
- [ ] cross-check every new env var: repeat the diff above with `env:"[^"]+"` — same expected-delta rule
- [ ] cross-check template surfaces: for each of Gemini tab, LLM Consensus row, Meta.ContactOnly, Meta.Giveaway, Report section, Duplicates section, Delete Join/Leave, AggressiveCleanup, BotUsername, render the settings page manually (Task 16 smoke) and visually confirm each section appears and is editable
- [ ] cross-check listener wiring: for `Report.Enabled`, `Delete.JoinMessages`, `Delete.LeaveMessages`, `AggressiveCleanup`, `Message.Startup`, `Duplicates.Threshold/Window`, grep for the field name in `app/events/listener.go` and `app/main.go` — must appear in both options→config→listener path
- [ ] cross-check new packages are actually referenced: `rg -l 'app/events/reports|app/events/dm_users|app/storage/reports|lib/tgspam/gemini|lib/tgspam/llm|lib/tgspam/duplicate' app/ lib/` — each new master-added package must be imported from the main binary wiring, not just sitting as orphan code
- [ ] cross-check tests: `go test -race -count=1 ./...` — count the total test pass rate; should be ≥ master's count (`git checkout origin/master && go test -race -count=1 ./... | tail -5` for comparison baseline, then back to merged branch)
- [ ] run `golangci-lint run` and compare output to pre-merge PR branch — no new lint issues should appear from master code we adopted

**Standard acceptance:**

- [ ] every item under `## Overview` is implemented
- [ ] `go test -race ./...` green
- [ ] `golangci-lint run` green
- [ ] `coverage.html` is not in the repo (`git ls-files | grep coverage.html` returns nothing)
- [ ] manual smoke (Task 16) passed
- [ ] `git log --oneline origin/master..HEAD` shows the commit chain is coherent (Pass 0 housekeeping → Pass 1 domain extension → Pass 2 merge → Pass 3 tests → Pass 4 docs)
- [ ] force-push the branch (PR 294 will refresh): `git push --force-with-lease origin feature-db-configuration` — **ask the user before pushing**
- [ ] run full project test command one last time: `go test -race ./...`

### Task 20: Finalize plan

- [ ] mark all checkboxes complete
- [ ] `mkdir -p docs/plans/completed`
- [ ] move this plan: `git mv docs/plans/20260418-merge-master-into-feature-db-configuration.md docs/plans/completed/`
- [ ] commit the move with message `docs: mark merge-master plan complete`

## Post-Completion

**Manual verification**:
- Update PR #294 description on GitHub noting: (1) Server.AuthUser was dropped (can be re-added in a follow-up if needed), (2) OpenAI env var realigned to singular `CUSTOM_PROMPT` to match master, (3) every new master setting group is now persisted via `--confdb`.
- Re-request review on the PR after force-push.
- Watch CI for any platform-specific failures we didn't catch locally (race detector behavior, SQLite locking in CI).

**External system updates** (none applicable — this is a single-repo merge).

**Follow-up candidates** (out of scope for this merge):
- Re-introduce `Server.AuthUser` as a separate PR with its own web UI exposure.
- Consider a dedicated migration for users on very old encrypted DBs if field additions ever break (currently additive is safe).
- e2e-ui Playwright coverage for the newly-added settings sections (Gemini, Report, Duplicates).
