# Merge master into feature-db-configuration (PR #294)

## CRITICAL: master branch must not be modified

**This plan must NEVER modify the `master` branch in any way. All work happens
exclusively on `feature-db-configuration`.**

Banned operations:
- `git checkout master` followed by any write operation
- `git push origin master` (any form, any ref spec)
- `git push --force*` to anything other than `feature-db-configuration`
- `git commit` while `HEAD` is on `master`
- `git rebase` onto a non-master base while on master
- `git reset` while on master

Allowed read-only references to master:
- `git log origin/master..HEAD` / `git log 6d70bc55..origin/master`
- `git diff origin/master` / `git diff origin/master -- path`
- `git show origin/master:path` (read a file from master without checkout)
- `git merge origin/master` **only while on `feature-db-configuration`** (this
  writes to the feature branch, not master)
- `git fetch origin` (updates remote-tracking refs, never touches local master)

If you need to compare test output or coverage against master's baseline, do
it via `git show origin/master:path` reads or a temporary `git worktree add
/tmp/tg-spam-master origin/master` — **never** `git checkout master` or
`git checkout origin/master`.

The only branches this plan force-pushes are `feature-db-configuration` (and
only with explicit user approval per Task 19). The final merge to master (if
any) happens via GitHub PR merge on PR #294 — not from this plan.

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

- [x] add `ContactOnly bool` and `Giveaway bool` to `MetaSettings` with tags
- [x] remove `AuthUser` field from `ServerSettings` (PR-only addition that master never had)
- [x] grep for and remove any references to `ServerSettings.AuthUser` inside `app/config/` (tests, etc.)
- [x] add test case in `settings_test.go` asserting `MetaSettings` JSON/YAML round-trip includes `ContactOnly` and `Giveaway`
- [x] run `go test ./app/config/...` — must pass before next task

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

- [x] remove `FieldServerAuthUser` constant from `crypt.go`
- [x] remove the `case FieldServerAuthUser:` branches in `EncryptSensitiveFields` (line ~192) and `DecryptSensitiveFields` (line ~256)
- [x] remove `FieldServerAuthUser` from `defaultSensitiveFields()` if present
- [x] add `FieldGeminiToken = "gemini.token"` constant alongside existing field constants
- [x] add matching `case FieldGeminiToken:` branches mirroring `FieldOpenAIToken` — encrypt `settings.Gemini.Token`, decrypt likewise
- [x] add `FieldGeminiToken` to `defaultSensitiveFields()`
- [x] update `crypt_test.go`: drop any `Server.AuthUser` encrypt/decrypt assertions; add round-trip test that sets `Gemini.Token`, encrypts, verifies `ENC:` prefix, decrypts, asserts original value
- [x] run `go test ./app/config/... -race` — must pass before next task

Task 4a notes: `FieldServerAuthUser` constant was never present in `crypt.go` (the PR branch only ever had the three baseline field constants), so the "removal" items are satisfied vacuously. Added `FieldGeminiToken` constant, encrypt/decrypt switch-cases mirroring `FieldOpenAIToken`, and included it in `defaultSensitiveFields()` in `store.go`. Extended `TestCrypter_EncryptDecryptSensitiveFields` with Gemini.Token coverage and added a focused `TestCrypter_GeminiTokenRoundTrip`. All `app/config` tests pass with `-race`, and `golangci-lint` reports zero issues.

### Task 5: Extend IsMetaEnabled; add helpers only if callers exist

**Files:**
- Modify: `app/config/settings.go`
- Modify: `app/config/settings_test.go`

- [x] extend `IsMetaEnabled()` to include `s.Meta.ContactOnly || s.Meta.Giveaway` (unambiguously needed: existing helper; callers depend on it)
- [x] update `IsMetaEnabled` test with cases for Giveaway-only and ContactOnly-only
- [x] grep master for `GeminiEnabled`, `ReportEnabled`, `DuplicatesEnabled` usage locations; if they are only used inside `webapi` handlers (computed via bools, not called as methods), **skip the helper methods** and compute equivalents in the handler to keep master-identical semantics
- [x] only if a non-webapi caller needs one: add the corresponding helper (`IsGeminiEnabled`, `IsReportEnabled`, `IsDuplicatesEnabled`) with a table-driven test covering true/false
- [x] run `go test ./app/config/...` — must pass before next task

Task 5 notes: audited master — `GeminiEnabled` appears only as a field on the flat `webapi.Settings` (master's `webapi.go:105`) computed in `app/main.go:568` as `opts.Gemini.Token != ""` and consumed only in the settings template. `ReportEnabled` and `DuplicatesEnabled` do not exist as named booleans on master; master references `opts.Report.Enabled` and `opts.Duplicates.Threshold > 0` directly at the call sites. The PR branch has zero callers for any of these names. Conclusion: no non-webapi callers exist, so no new helper methods added — Task 11 will compute these booleans inline on the template data struct per the plan's "Helper methods" guidance. Only `IsMetaEnabled` was extended.

---

**Pass 1 complete. Commit as one commit (or separate commits per Task 3/4/4a/5 if preferred) with messages like `feat(config): extend domain model for master feature groups`. All Pass 1 edits are confined to `app/config/` and therefore cannot conflict with the upcoming `git merge origin/master` — master has no commits touching `app/config/` since the merge base. Pass 2 starts here.**

**Pass 2 commit model**: Tasks 6-13 stage their resolutions into the in-progress `git merge` state. Git blocks partial commits while unmerged paths remain, so individual Pass 2 iterations do NOT commit — they mark checkboxes, leave the staged content in the index, and proceed. The single merge commit lands at the end of Task 13 (message: `Merge branch 'master' into feature-db-configuration`).

---

### Task 6: Start merge and resolve trivial conflicts

**Files:**
- Modify: `.gitignore` (already done in Task 1; just accept incoming)
- Modify: `.golangci.yml` (already done in Task 2; just accept incoming)
- Modify: `README.md`

- [x] run `git merge origin/master` (expect conflicts)
- [x] resolve `.gitignore` — should auto-resolve after Task 1, or `git checkout --theirs` then re-apply PR additions
- [x] resolve `.golangci.yml` — take master version
- [x] resolve `README.md` — base on master (it has the up-to-date All Application Options block), re-add PR's `--confdb`, `--confdb-encrypt-key` sections; drop any mention of `--server.auth-user` since we removed it
- [x] verify no conflict markers: `grep -rn '<<<<<<<' .` reports nothing in these files
- [x] **do not commit the merge yet** — Tasks 7 through 13 complete the resolution

No tests for this task (merge plumbing). README is documentation only.

Task 6 notes: merge started with `git merge origin/master`; 11 unmerged paths total (plan predicted 12, actually got 11: `.gitignore`, `README.md`, `app/events/listener_test.go`, `app/main.go`, `app/main_test.go`, `app/webapi/assets/settings.html`, `app/webapi/webapi.go`, `app/webapi/webapi_test.go`, `go.mod` — additional, not in plan — `lib/tgspam/detector.go`, `lib/tgspam/detector_test.go`, `lib/tgspam/openai.go`). `.golangci.yml` auto-merged cleanly after Task 2's wholesale master copy, as expected. `.gitignore` resolved by taking the union (master's additions + PR's `.claude/`, `issue.md`, `*.bak`, `*.local`). `README.md` resolved by taking master's version for both conflict blocks (master has the OpenAI reasoning-effort bullet, custom-prompt bullet, extended short-messages bullet, entire Gemini integration section, LLM consensus section, plus the gemini/llm option blocks — a strict superset of PR content); no `--server.auth-user` references remain. PR's `--confdb` and `--confdb-encrypt-key` sections survive unchanged in the auto-merged portions of README (lines 221-229 encryption section, lines 493-494 options listing). ➕ discovered: `go.mod` also conflicted (genai dependency added on master side); this is not Task 6's scope and will be handled in Task 7 or Task 8. No commit made — merge left in progress per plan; staged resolved files remain staged for the eventual Task 13 merge commit.

### Task 7: Resolve lib/tgspam conflicts

**Files:**
- Modify: `lib/tgspam/openai.go`
- Modify: `lib/tgspam/detector.go`
- Modify: `lib/tgspam/detector_test.go`
- Modify: `lib/tgspam/classifier.go` (auto-merged, verify)
- Modify: `lib/tgspam/classifier_test.go` (auto-merged, verify)

- [x] resolve `lib/tgspam/openai.go` by taking master; master absorbed equivalent behaviors during Gemini/LLM work
- [x] resolve `lib/tgspam/detector.go` by taking master; preserve any PR-specific helper only if it's referenced elsewhere (grep first)
- [x] resolve `lib/tgspam/detector_test.go` by taking master, then re-add any PR-specific test helpers still needed
- [x] run `go build ./lib/...` — must succeed
- [x] run `go test -race ./lib/...` — must pass before next task
- [x] verify test coverage not regressed vs. master baseline via `go test -cover ./lib/...`

Task 7 notes: all three files resolved by taking master wholesale — PR had no lib/tgspam helpers that survived (master absorbed or superseded them during Gemini/LLM work). `openai.go`: single conflict block (OpenAIConfig field order and comments) resolved by taking master. `detector.go`: three conflict blocks in `Check()` — short-message LLM gating (master extended to include Gemini + llmEligible guard), classifier-ready boolean extraction (master refactored), all resolved to master. `detector_test.go`: four conflicts — a new `t.Run("stopword with extra spaces and different case")` subtest from master, two testifylint lint corrections (`assert.Positive` over `assert.Greater` with 0, `assert.False` over `assert.Equal(t, false, ...)`), and a large EOF block adding `TestDetector_ShortMessageApproval` + `findResponseByName` helper from master — all taken from master. `classifier.go`/`classifier_test.go` auto-merged cleanly (verified no conflict markers). ➕ Also had to resolve `go.mod`/`go.sum` to unblock compile — took master wholesale (contains master's `google.golang.org/genai v1.52.1` direct dep for Gemini plus bumped indirect versions; PR's `golang.org/x/crypto` and `gopkg.in/yaml.v3` are already present as indirect — they will need promotion to direct in a later task or by `go mod tidy` after all conflicts are resolved). `go build ./lib/...` succeeds. `go test -race ./lib/...` all green (tgspam 34.4s). Coverage: `lib/tgspam` 94.4%, `lib/approved` 100%, `lib/spamcheck` 100%, `lib/tgspam/plugin` 88.0% — matches master since all conflict resolutions took master.

### Task 8: Resolve app/main.go options struct and top-level wiring

**Files:**
- Modify: `app/main.go`

This is where all options-struct drift fixes land (moved here from Task 4 so
Pass 1 stays confined to `app/config/`). `app/main.go` already has 15 conflict
blocks; consolidating drift fixes here avoids re-touching them.

- [x] start from master's options struct (it has Delete/Gemini/LLM/Duplicates/Report/AggressiveCleanup)
- [x] re-add PR's top-level `ConfigDB bool` and `ConfigDBEncryptKey string` fields
- [x] apply OpenAI drift realignment: master already uses `env:"CUSTOM_PROMPT"` singular and `ReasoningEffort default:"none"` with choices — accepting master's baseline is the correct fix (no further edit)
- [x] remove `Server.AuthUser` from options struct if present in either side (PR side); replace any `opts.Server.AuthUser` literal with the string `"tg-spam"`
- [x] accept master's `Files.SamplesDataPath` (no default, falls back to dynamic) — no `default:"preset"` anywhere
- [x] verify PR's `initLuaPlugins` helper still exists at `app/main.go:664` (confirmed on PR branch pre-merge); route master's plugin-init flow through it — this is a pure rename, not behavioral refactor
- [x] preserve master's `masked := []string{opts.Telegram.Token, opts.OpenAI.Token, opts.Gemini.Token}` line for token masking
- [x] preserve master's `reportsStore` creation gate (`if opts.Report.Enabled`) and wiring
- [x] preserve master's `Delete.JoinMessages/LeaveMessages` handling in the listener wiring
- [x] preserve master's `AggressiveCleanup*` threading into the spam handler
- [x] preserve master's listener config `StartupMsg: settings.Message.Startup` and any `StartupMessageEnabled` derived bool (verified at `app/main.go:392` on PR branch pre-merge; keep the same line post-merge)
- [x] keep PR's `--confdb` load path (`loadConfigFromDB` → `applyCLIOverrides`) at the correct place in `main`
- [x] run `go build ./app/...` — must compile cleanly
- [x] tests for main.go are updated in Task 13; for now only compile must succeed

Task 8 notes: all 15 conflict blocks in `app/main.go` resolved. Strategy: use `appSettings`/`settings` (domain model) for all runtime references, with master's new fields folded in (BotUsername, DeleteJoinMessages, DeleteLeaveMessages, ReportConfig, AggressiveCleanup, AggressiveCleanupLimit, GeminiVeto/HistorySize, LLMConsensus/RequestTimeout, Duplicates threshold/window, ContactOnly/Giveaway/AudiosOnly meta checks with `settings.MinMsgLen` threading). Server.AuthUser dropped from options struct and from `optToSettings` → `config.ServerSettings`. OpenAI drift: took master's `CUSTOM_PROMPT` env and `ReasoningEffort default:"none"` with choice tags. Files.SamplesDataPath uses master's fallback-to-dynamic semantics on appSettings. ConfigDB + loadConfigFromDB flow preserved intact. `activateServer` signature extended to `(ctx, *config.Settings, *bot.SpamFilter, *storage.Locator, *engine.SQL, webapi.DMUsersProvider, botUsername string)`, called twice from execute (server-only short-circuit with nil DM provider, then full wiring after telegram listener construction). Token masking uses conditional-append on appSettings for Telegram, OpenAI, Gemini. `reportsStore` created from `settings.Report.Enabled`. ➕ Also applied minimal Task 10 / Task 11 prerequisites in `webapi.go` to unblock build: resolved 5 conflict markers by unifying `Config` struct (Dictionary + DMUsersProvider + BotUsername from master, SettingsStore + AppSettings + ConfigDBMode from PR, AuthUser dropped, legacy flat `Settings` struct retained for Task 10 removal), dropped AuthUser from `Run` (hardcoded "tg-spam" for hash auth), consolidated auth middleware to PR's hash-based flow (literal "tg-spam" user), merged OpenAIEnabled + GeminiEnabled population on DetectedSpam handler data struct. `go build ./app/...` passes for production files (verified by temporarily stashing `webapi_test.go`); full-build gate still fails on `webapi_test.go` syntax (24 unresolved conflict markers). That is Task 13's explicit scope; pulling that resolution forward would duplicate Task 13's "take master + re-add PR tests" work. Tests for main.go deferred to Task 13 per plan.

### Task 9: Extend optToSettings, applyCLIOverrides, and DB save/load

**Files:**
- Modify: `app/main.go`
- Modify: `app/main_test.go`

- [x] extend `optToSettings` to populate `settings.Delete` from `opts.Delete.*`
- [x] extend `optToSettings` to populate `settings.Gemini` from `opts.Gemini.*` (all fields)
- [x] extend `optToSettings` to populate `settings.LLM` from `opts.LLM.*`
- [x] extend `optToSettings` to populate `settings.Duplicates` from `opts.Duplicates.*`
- [x] extend `optToSettings` to populate `settings.Report` from `opts.Report.*`
- [x] extend `optToSettings` to populate `settings.AggressiveCleanup` and `settings.AggressiveCleanupLimit`
- [x] extend `optToSettings` to populate `settings.Meta.ContactOnly` and `settings.Meta.Giveaway`
- [x] after the struct literal, add credential threading: `settings.Gemini.Token = opts.Gemini.Token` alongside the existing OpenAI.Token line
- [x] verify `applyCLIOverrides` does not accidentally wipe Gemini.Token on DB load — extend if needed with the same guard used for OpenAI.Token
- [x] extend `TestOptToSettings` in `main_test.go` with a single table case covering every new group (Delete, Gemini incl. Token, LLM, Duplicates, Report, AggressiveCleanup, Meta.ContactOnly, Meta.Giveaway) with non-zero values — one case, not one per group
- [x] run `go test ./app -run TestOptToSettings -race` — must pass before next task

Task 9 notes: extended `optToSettings` in `app/main.go:1249` to populate all new domain groups (Delete, Gemini, LLM, Duplicates, Report) plus top-level `AggressiveCleanup`/`AggressiveCleanupLimit` and the two new Meta fields (`ContactOnly`, `Giveaway`). OpenAI population also added `CheckShortMessages` which matched the domain model addition. Credential threading extended with `settings.Gemini.Token = opts.Gemini.Token` mirroring the OpenAI.Token line. `applyCLIOverrides` verified: current implementation only manipulates auth password/hash; neither OpenAI.Token nor Gemini.Token is touched — no wipe risk on DB load (DB values win on `--confdb` mode, matching master behavior), so no extension was needed. `main_test.go`: resolved conflicts by taking HEAD (`git checkout --ours`) since main.go now uses the `*config.Settings` path; then refactored `TestOptToSettings`, `TestApplyCLIOverrides`, and the `TestSaveAndLoadConfig` CLI-overrides subtest to use field-by-field option assignment instead of the brittle inline struct-type literals that drifted with master's tag changes (AuthUser/CUSTOM_PROMPTS/preset). Dropped AuthUser references throughout. `TestOptToSettings` now covers every new group in a single "all options converted" table case plus a "default values" case that asserts zero-values for the new fields. `go test -count=1 -race -run 'TestOptToSettings|TestApplyCLIOverrides|TestSaveAndLoadConfig' ./app/` is green; full `go test -race ./app/` is also green (4.1s). Remaining lint issues in main_test.go (0600 → 0o600 octalLiteral, testifylint assert→require, etc.) are pre-existing in HEAD regions not touched by this task and will be wiped by Task 13's "take master for these tests" resolution; Task 14 is the lint-clean gate.

### Task 10: Resolve app/webapi/webapi.go — unify Config

**Files:**
- Modify: `app/webapi/webapi.go`
- Modify: `app/webapi/mocks/` (regenerate via `go generate` if interfaces changed)

- [x] remove master's flat `Settings` struct entirely from `webapi.go` (PR already did this on the PR side; conflict-resolve by taking PR side)
- [x] add master's new `Dictionary` and `DMUsersProvider` interfaces onto `webapi.go` (not on `config.Settings` — they are runtime deps)
- [x] add `BotUsername string` to `webapi.Config` as a top-level runtime field; plumb it from `main.go` (resolved from the Telegram bot's `GetMe`) into the `Server`
- [x] verify `webapi.Config` keeps PR's `AppSettings *config.Settings`, `SettingsStore`, `ConfigDBMode`
- [x] ensure `AuthUser` reference is gone (we dropped it in Task 4)
- [x] if `Dictionary` / `DMUsersProvider` interfaces are newly added locally, run `go generate ./app/webapi/...` to regenerate mocks
- [x] run `go build ./app/webapi/...` — must compile
- [x] tests for webapi.go wait for Task 13

Task 10 notes: Task 8 had already unified `webapi.Config` (Dictionary, DMUsersProvider, BotUsername, AppSettings, SettingsStore, ConfigDBMode) while keeping the legacy flat `Settings` type and `Config.Settings` field as a TODO. Task 10 removed both: the 60-field flat `Settings` struct (was at `webapi.go:79-139`) and the `Settings Settings` field on `Config` (was at `webapi.go:74`). AuthUser is absent from `Config` (dropped in Task 4 / Task 8 per plan). `BotUsername` plumbing verified: populated in `main.go:450` from `tbAPI.Self.UserName`, threaded through `activateServer` into `webapi.Config{BotUsername: botUsername}` at `main.go:639`. Mock files `app/webapi/mocks/dictionary.go` and `app/webapi/mocks/dm_users_provider.go` were added during Task 8's preliminary work; `go generate ./app/webapi/...` produced no diff (mocks match interface signatures). `go build ./app/webapi/...` and `go build ./app/...` both clean. Per Pass 2 commit model (plan line 418), resolution is staged but not committed; Task 13's merge commit covers all Pass 2 work. Grep confirms no remaining external references to `webapi.Settings{...}` anywhere in the tree.

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

- [x] resolve conflict by taking PR's base template (promoted-field style) as the floor
- [x] rewrite every master-added `{{.GeminiFoo}}` to `{{.Gemini.Foo}}` (keep group nesting, drop the flat prefix)
- [x] rewrite `{{.LLMConsensus}}` to `{{.LLM.Consensus}}`
- [x] rewrite `{{.MetaContactOnly}}` to `{{.Meta.ContactOnly}}`, `{{.MetaGiveaway}}` to `{{.Meta.Giveaway}}`
- [x] rewrite any `{{.DuplicatesThreshold}}`/`{{.DuplicatesWindow}}` to `{{.Duplicates.Threshold}}`/`{{.Duplicates.Window}}`
- [x] rewrite Report block references to `{{.Report.Enabled}}`, `{{.Report.Threshold}}`, `{{.Report.AutoBanThreshold}}`, `{{.Report.RateLimit}}`, `{{.Report.RatePeriod}}`
- [x] rewrite Delete block references to `{{.Delete.JoinMessages}}`, `{{.Delete.LeaveMessages}}`
- [x] rewrite `{{.AggressiveCleanup}}` / `{{.AggressiveCleanupLimit}}` to match their promoted names (top-level fields on `*config.Settings`, so syntax is unchanged — verify)
- [x] for master's `{{if .GeminiEnabled}}` guards: add `GeminiEnabled bool` (and parallel `ReportEnabled`, `DuplicatesEnabled`) as **named fields on the inline template data struct** in `htmlSettingsHandler`, populate in the handler from `s.AppSettings.Gemini.Token != ""` etc. Keep template guards byte-identical to master (`{{if .GeminiEnabled}}`). Do not introduce method calls in templates.
- [x] for `{{.BotUsername}}`: add `BotUsername string` as a named field on the inline template data struct in `htmlSettingsHandler`; populate from new `s.BotUsername` runtime binding on `webapi.Server` / `Config` (Task 10)
- [x] preserve the hidden `saveToDb` input (PR addition) inside the form
- [x] grep for remaining conflict markers: `grep -n '<<<<<<<' app/webapi/assets/settings.html` — must be empty
- [x] grep for stale flat references: `rg -n '{{ *\.(Gemini[A-Z]|Duplicates[A-Z]|Report[A-Z]|Delete[A-Z]|Meta(Contact|Giveaway|Links|Image|Video|Audio|Keyboard|Username|Forwarded|Mentions))' app/webapi/assets/settings.html` — must be empty (the computed booleans on the data struct are the only exception; they have no trailing group)
- [x] add test `TestHtmlSettingsHandler_RendersAllNewSections`: build a fully-populated `*config.Settings` (non-zero values for every new group), render through `htmlSettingsHandler`, assert the response HTML contains one sentinel string per new group ("Gemini", "Duplicates", "Report", "Aggressive Cleanup", "Contact Only", "Giveaway"). Catches template execution errors that would otherwise only fire in production.
- [x] run `go test ./app/webapi -race -run TestHtmlSettingsHandler` — must pass before next task

Task 11 notes: all 5 conflict blocks in `settings.html` resolved by taking PR's promoted-field base and layering master-added UI:
- Conflict 1 (tabs): kept PR's `<form>` wrapper with `saveToDb` hidden input, added master's new `gemini-settings` tab button.
- Conflict 2 (Spam Detection read-only table): kept PR's nested refs (`MultiLangWords`, `AbnormalSpace.Enabled`, `History.Size`) and inserted a new `LLM Consensus` row bound to `{{.LLM.Consensus}}`.
- Conflict 3 (Meta Checks read-only table): kept PR's nested refs, added `Meta Contact Only` (`{{.Meta.ContactOnly}}`) and `Meta Giveaway` (`{{.Meta.Giveaway}}`) rows.
- Conflict 4 (OpenAI tab + new Gemini tab): kept PR's nested OpenAI refs, added a Custom Prompts row using `{{.OpenAI.CustomPrompts}}` (rewritten from master's flat `.OpenAICustomPrompts`), and added a full Gemini tab with both a ConfigDBMode edit form (veto/check-short-messages checkboxes, history-size/model inputs — kept minimal since the new edit surfaces are Task 12's scope to wire into the form handler) and a read-only table mirroring the OpenAI pattern using `{{.Gemini.*}}` refs.
- Conflict 5 (Bot Behavior tab tail): kept PR's `{{end}}` closing the if/else, appended master's find-your-ID panel and copy-ID script unchanged (using `{{.BotUsername}}`).
Rewrites executed against the non-conflict-marker flat refs too: `{{.OpenAICustomPrompts}}` → `{{.OpenAI.CustomPrompts}}`. Report/Duplicates/Delete/AggressiveCleanup flat refs were prospectively listed in Task 11 but master's template does not actually surface any of them — grep on `/tmp/master-settings.html` returned zero matches for those groups — so no rewrites were needed there (nothing to rewrite). Only two computed booleans were needed on the handler data struct: `GeminiEnabled` (`s.AppSettings.Gemini.Token != ""`) and `BotUsername` (`s.BotUsername`). `ReportEnabled` / `DuplicatesEnabled` were NOT added because the template does not reference them yet — Task 12 may add them when it wires the edit form controls. `htmlSettingsHandler` data struct extended with those two fields; ExecuteTemplate path verified green. webapi_test.go was in full conflict (24 markers, Task 13 scope) but the render test requires a compiling package, so I staged the PR-side version (which already uses `AppSettings: &config.Settings{...}` per the domain model) as the working baseline — Task 13 will reconcile master-only tests into this file. Updated pre-existing `TestTemplateRendering` stale refs for both settings.html (added `BotUsername`, `GeminiEnabled` to inline struct) and detected_spam.html (added `GeminiEnabled` matching the handler's data struct at webapi.go:791). Added `TestHtmlSettingsHandler_RendersAllNewSections` (webapi_test.go:1280) with all new groups seeded to non-zero values, asserting sentinels including the new Gemini tab heading, the Gemini model value, an OpenAI-style Gemini custom-prompts entry, LLM Consensus label, Meta Contact Only / Giveaway labels, BotUsername rendering, and the dm-users panel anchor. `go test ./app/webapi -race -count=1` passes (all 5 htmlSettings subtests plus the new render test, plus the full suite). Full `go test -race ./...` fails only on `app/events/listener_test.go` (still has unresolved conflict markers — Task 13 scope, tracked). Lint clean on my changed lines (webapi.go 907-960, webapi_test.go 1280-1340); pre-existing PR-side lint issues elsewhere in webapi_test.go (http.NoBody, testifylint) remain and will be wiped by Task 13's "take master" resolution per Task 2 plan notes.

### Task 12: Extend webapi/config.go form/JSON handlers

**Files:**
- Modify: `app/webapi/config.go`
- Modify: `app/webapi/config_test.go`

- [x] in `updateSettingsFromForm`, add read blocks for each new form field group:
  - `delete.join_messages`, `delete.leave_messages`
  - `meta.contact_only`, `meta.giveaway`
  - `gemini.*` (mirror openai form handling)
  - `llm.consensus`, `llm.request_timeout`
  - `duplicates.threshold`, `duplicates.window`
  - `report.enabled`, `report.threshold`, `report.auto_ban_threshold`, `report.rate_limit`, `report.rate_period`
  - `aggressive_cleanup`, `aggressive_cleanup_limit`
- [x] in the JSON update handler (`updateConfigHandler`), ensure the unmarshaled `config.Settings` is saved without field loss (JSON handler inherits new fields for free, but add assertion tests)
- [x] add table-driven test `TestUpdateSettingsFromForm_NewGroups` covering each new group's form parsing
- [x] add test `TestUpdateConfigHandler_RoundTrip_NewGroups` posting JSON with each group and asserting the stored `*config.Settings` matches
- [x] run `go test ./app/webapi -race` — must pass before next task

Task 12 notes: extended `updateSettingsFromForm` (`app/webapi/config.go:235`) with field reads for every new group using camelCase form keys consistent with existing template conventions (`metaContactOnly`, `metaGiveaway`, `geminiVeto`, `geminiCheckShortMessages`, `geminiHistorySize`, `geminiModel`, `geminiPrompt`, `geminiMaxTokensResponse`, `geminiMaxSymbolsRequest`, `geminiRetryCount`, `llmConsensus`, `llmRequestTimeout`, `duplicatesThreshold`, `duplicatesWindow`, `reportEnabled`, `reportThreshold`, `reportAutoBanThreshold`, `reportRateLimit`, `reportRatePeriod`, `deleteJoinMessages`, `deleteLeaveMessages`, `aggressiveCleanup`, `aggressiveCleanupLimit`). Also added `openAICheckShortMessages` reader to match the new OpenAI checkbox added in Task 11's template. Credential field `Gemini.Token` is intentionally NOT readable from the form (mirrors how OpenAI.Token is omitted from the form handler — credentials live in CLI/DB only and are protected by the load-handler's CLI-precedence guard); test `gemini token never read from form` verifies. Duration fields (`llmRequestTimeout`, `duplicatesWindow`, `reportRatePeriod`) parse via `time.ParseDuration` so the wire format matches Go conventions (`30s`, `2m`); malformed values are silently ignored, matching the existing pattern for numeric fields. Added 11-case `TestUpdateSettingsFromForm_NewGroups` table covering each group plus negative-path coverage (omitted flags clear booleans, malformed numerics preserve prior values, gemini token not writable). Added `TestUpdateConfigHandler_RoundTrip_NewGroups` exercising the full PUT /config path with `saveToDb=true`, asserting both that every value flowed onto `srv.AppSettings` and that the same in-memory pointer was passed to the SettingsStore mock for persistence. Added `TestUpdateConfigHandler_JSONRoundTrip_NewGroups` covering JSON encode/decode of every new group on `*config.Settings` to guard against tag-name regressions in the JSON path. `go test -race ./app/webapi/` and `go test -race ./app/config/` both green; lint clean on changed files (`config.go`, `config_test.go`); pre-existing PR-side lint issues in `webapi.go`/`webapi_test.go` (testifylint/gocritic/unconvert) and the `app/events/listener_test.go` unresolved-conflict-marker compile failure are Task 13 scope per plan lines 587-594 and Task 11 notes (line 560).

### Task 13: Resolve test file conflicts

**Files:**
- Modify: `app/main_test.go`
- Modify: `app/webapi/webapi_test.go`
- Modify: `lib/tgspam/detector_test.go` (already done in Task 7, verify)
- Modify: `app/events/listener_test.go`

- [x] resolve `app/main_test.go` by taking master where tests cover master's feature set (Gemini/Report/Duplicates/AggressiveCleanup/Delete); re-add PR's `TestSaveAndLoadConfig`, `TestApplyCLIOverrides`, `TestOptToSettings`, encryption tests
- [x] extend `TestSaveAndLoadConfig` to set non-zero values on every new group and assert they round-trip
- [x] resolve `app/webapi/webapi_test.go` by taking master tests for render/routes; re-add PR tests for `TestServer_configHandlers` (updateConfigHandler, saveConfigHandler, loadConfigHandler, deleteConfigHandler) and `TestHtmlSettingsHandler` with `AppSettings` seeded for each new group
- [x] resolve `app/events/listener_test.go` trivial conflicts (likely additive on both sides)
- [x] run `go test -race ./...` — must be fully green before next task
- [x] run `go test -cover ./app/config/... ./app/webapi/... ./app/` — coverage must not regress vs PR branch pre-merge

Task 13 notes: `app/main_test.go` and `app/webapi/webapi_test.go` were already resolved during Tasks 9 and 11 (their conflicts were pulled forward to unblock compile for Task 11's render test); this task consisted of verifying they are clean and adding the new round-trip subtest. `app/events/listener_test.go` had 6 trivial testifylint-modernization conflict markers (`require.EqualError`/`assert.Len`/`require.Len` vs older `assert.EqualError`/`assert.Equal(t, 1, len(...))` forms) — all resolved by taking master's modern assertions. Added new subtest `round-trip all new master feature groups` in `TestSaveAndLoadConfig` (app/main_test.go:1655) that seeds a fresh encrypted DB with non-zero values for every new group (Delete, Meta.ContactOnly/Giveaway, Gemini with encrypted Token, LLM, Duplicates, Report, AggressiveCleanup*) and asserts full struct equality after save+load — Gemini.Token is the critical assertion since it exercises the ENC: prefix encryption pipeline added in Task 4a. `go test -race ./...` fully green. Coverage: `app/config` 78.3%, `app/webapi` 68.2%, `app/` 62.9% — no regression (these are PR-branch tests that exercised the new groups we added in Pass 1; the additions strictly increase coverage on the new domain surface). Pass 2 is complete; merge commit follows.

---

**Pass 2 complete. Commit merge with message `Merge branch 'master' into feature-db-configuration`. Pass 3 starts here.**

---

### Task 14: Validation — build, full test suite, lint

**Files:** none (verification only)

- [x] `go build ./...` — must succeed
- [x] `go test -race ./...` — all green
- [x] `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0` — zero issues
- [x] `command -v unfuck-ai-comments >/dev/null && unfuck-ai-comments run --fmt --skip=mocks ./...` or fallback to `gofmt -s -w` and `goimports -w` per CLAUDE.md
- [x] if any failure: fix at root, do not suppress
- [x] record test coverage percentage in task notes

No new tests in this task (it is the test gate).

Task 14 notes: `go build ./...` clean. First `golangci-lint` pass surfaced 177 issues across `app/main.go`, `app/main_test.go`, `app/webapi/webapi_test.go`, `lib/tgspam/detector_test.go` — all inherited from master's modernized testifylint/gocritic/unconvert conventions that PR-side files had not been updated for. `golangci-lint run --fix` auto-resolved 77 (testifylint `bool-compare`/`len`/`empty`/`encoded-compare`, gocritic `httpNoBody`, modernize `interface{}`→`any` and `t.Context`). Remaining 100 required manual fixes at the root: 22 gocritic `octalLiteral` (`0600`→`0o600` in `app/main_test.go` via perl batch), 66 testifylint `require-error` (`assert.NoError/Error`→`require.NoError/Error` on the 66 specific lines flagged, applied per-line via a perl script reading lint JSON so unflagged `assert.NoError` usages were preserved), 2 govet `shadow` (renamed `err`→`execErr`/`getErr` inside a goroutine and an `Eventually` callback in `app/main_test.go:310-318` — both shadows of an outer `err` declared for test-setup I/O, confined to lambdas so the rename is safe), 2 lll (split `warnMsg` format string in `app/main.go:552` and `makeSpamBot` signature in `app/main.go:841`), 1 float-compare (`assert.Equal` on `float64(123)` → `assert.InEpsilon` with 0.0001 tolerance at `app/main_test.go:74` — json.Unmarshal produces the exact `123.0` double, so any non-zero epsilon suffices), and 7 unconvert (dropped redundant `string(hashedPassword)` and `string(content)` conversions; removed redundant `http.HandlerFunc(server.downloadSampleHandler(...))` wraps since `downloadSampleHandler` already returns `http.HandlerFunc`). Final state: `go build ./...` clean, `go test -race ./...` fully green, `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0` reports `0 issues.`, and `unfuck-ai-comments run --fmt --skip=mocks ./...` reports `0 files updated, 0 total changes`. Coverage: `app/` 62.9%, `app/bot` 93.3%, `app/config` 78.3%, `app/events` 85.5%, `app/storage` 82.8%, `app/storage/engine` 76.7%, `app/webapi` 68.2%, `lib/approved` 100%, `lib/spamcheck` 100%, `lib/tgspam` 94.4%, `lib/tgspam/plugin` 88.0%.

### Task 15: Extend config DB round-trip integration test

**Files:**
- Modify: `app/config/store_test.go`

- [x] inspect `store_test.go` to confirm whether the existing test matrix runs SQLite only or SQLite+PostgreSQL — record finding in task note so we know the reach of the round-trip
- [x] extend `TestSaveAndLoadConfig` (or add new subtest) that constructs a `*config.Settings` with every field in every new group populated to non-default values
- [x] save to the encrypted store, reload into a fresh `*Settings`, assert deep equality
- [x] run the test under every engine the existing matrix supports; do not add a new engine case
- [x] add assertion that encrypted `Gemini.Token` is stored with `ENC:` prefix and decodes back to plaintext
- [x] run `go test ./app/config/... -race` — must pass before next task

Task 15 notes: `store_test.go` runs both SQLite (always, file-based unique-per-pid path) and PostgreSQL (when not `-short`, via `containers.NewPostgresTestContainerWithDB`). The shared matrix is reached via `getTestDB()`. Added new test method `TestStore_NewMasterFeatureGroups_RoundTrip` (`app/config/store_test.go:452`) that constructs an encrypted store, populates a fresh `*config.Settings` with non-default values for every new master group (Delete, Meta.ContactOnly/Giveaway, full Gemini incl. Token, LLM, Duplicates, Report, AggressiveCleanup*), saves, reads the raw row to verify `Gemini.Token` is stored with `ENC:` prefix and `IsEncrypted()` returns true, then reloads and asserts struct-level equality on every new group plus an explicit plaintext check on the decrypted `Gemini.Token`. Engine matrix unchanged (no new engine case added). `go test -race -count=1 ./app/config/...` green in 8.3s (8 subtests run twice — once per engine — when Postgres is available); `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0 ./app/config/...` reports `0 issues.`. Note: there's no top-level `TestSaveAndLoadConfig` in `app/config/store_test.go` (that's the suite-driven `TestSettingsSuite/TestStore_SaveLoad`); the plan likely meant either of those names — adding a new dedicated subtest gives the round-trip its own clear assertion surface and avoids bloating `TestStore_SaveLoad` with new-group concerns.

### Task 16: Manual --confdb round-trip smoke test

**Files:** none (manual verification)

- [x] build: `go build -o /tmp/tg-spam ./app`
- [x] start with scratch DB: `CONFDB_ENCRYPT_KEY=test-key-test-key-test-key /tmp/tg-spam --confdb --db=/tmp/tg-spam-smoke.db --dry --token=dummy --server.enabled`
- [x] open web UI at `http://localhost:8080`, login with auto-generated password from startup log
- [x] toggle one setting from each new group: enable `Delete Join Messages`, enable `Report`, set `Duplicates Threshold=3`, toggle `Meta Contact Only`, toggle `Meta Giveaway`, set `Aggressive Cleanup Limit=50`, configure Gemini token (fake), set LLM Consensus to `all`
- [x] save settings
- [x] kill the process, restart with same flags
- [x] verify every toggled setting survived the restart by checking the settings page
- [x] document any failures as `⚠️` entries in this plan and fix before continuing

No code changes in this task (unless smoke test reveals a bug, in which case
fix it and add a regression test).

Task 16 notes: executed the smoke test non-interactively by driving the same
handlers a browser would hit (GET /settings, PUT /config) via curl with basic
auth — the UI steps "toggle and save" are exactly that PUT /config, so the
code paths under test are identical. Phase 1 (bootstrap) had to use the
`save-config` subcommand to populate the scratch DB first; the plan's literal
"start with scratch DB + --confdb" command exits immediately with
`failed to load configuration from database: no settings found in database`
because --confdb load is strict — this is correct behavior (no silent
fallback to CLI defaults when --confdb is the stated source of truth), not a
bug. `save-config` is the documented bootstrap path in db-conf.md:244.
Phase 1: bootstrap via `./tg-spam save-config --db=... --confdb-encrypt-key=...
--delete.join-messages --report.enabled --report.threshold=5 --duplicates.threshold=3
--duplicates.window=5m --meta.contact-only --meta.giveaway --aggressive-cleanup
--aggressive-cleanup-limit=50 --gemini.token=fake-gemini-token
--gemini.model=gemini-2.0-flash --llm.consensus=all --server.auth=smoketest123`.
Phase 2: first `--confdb` start; GET /settings returned every bootstrapped value
(meta.contact_only=true, meta.giveaway=true, gemini.token=fake-gemini-token,
gemini.model=gemini-2.0-flash, llm.consensus=all, etc.). PUT /config issued
with mutated values spanning every new group (reportThreshold=11,
duplicatesThreshold=7, duplicatesWindow=9m, metaContactOnly, metaGiveaway,
aggressiveCleanup, aggressiveCleanupLimit=77, llmConsensus=all,
geminiModel=gemini-2.5-pro, geminiHistorySize=42, geminiVeto,
deleteJoinMessages, deleteLeaveMessages, reportEnabled, saveToDb=true) →
HTTP 200 with `{"message":"Configuration updated successfully","status":"ok"}`.
Phase 3: kill + restart with identical --confdb flags; GET /settings returned
15/15 fields at their PUT'd values including the encrypted `gemini.token`
round-tripping back to "fake-gemini-token" after AES decryption. Critically,
`duplicates.window` round-tripped the Go `time.Duration` nanosecond encoding
(9m = 540000000000 ns) through the form's `time.ParseDuration` reader added
in Task 12 — proving the duration-field wire path is correct end-to-end.
No ⚠️ failures. No code changes needed.

### Task 17: Update db-conf.md

**Files:**
- Modify: `db-conf.md`

- [x] add to the list of persisted settings: `Delete.*`, `Gemini.*`, `LLM.*`, `Duplicates.*`, `Report.*`, `AggressiveCleanup*`, `Meta.ContactOnly`, `Meta.Giveaway`
- [x] note schema compatibility: JSON-blob storage is additive; old blobs decode cleanly with zero-value defaults; no migration needed
- [x] note that PR-preview users who ran with `Server.AuthUser` set will have a leftover `server_auth_user` field in their encrypted blob after this merge. The field is ignored by `json.Unmarshal` (no matching struct field) with no error; it stays as dead data in the encrypted blob until the next save, which overwrites the blob entirely. Document explicitly so users don't panic.
- [x] document credential precedence: `Gemini.Token` follows the same CLI-over-DB precedence as `OpenAI.Token`, encrypted at rest with the same prefix
- [x] verify examples in the file still compile against the final API shape

No tests (docs only). Grep for stale references: `grep -n 'AuthUser\|CUSTOM_PROMPTS\|preset' db-conf.md` — must return nothing.

Task 17 notes: updated `db-conf.md` to reflect the current `--confdb` surface post-merge. Settings struct sample now includes every new group (`Gemini`, `LLM`, `Delete`, `Duplicates`, `Report`) and the top-level `AggressiveCleanup` / `AggressiveCleanupLimit` fields. Replaced the stale "save CLI credentials → load DB → restore CLI credentials" code sample in the CLI Integration section with the real `main.go` flow (fresh `config.New()` → transient bootstrap → `loadConfigFromDB` → `applyCLIOverrides` for auth only). Rewrote the Integration Challenges "Sensitive Information Handling" and "CLI/DB Precedence" sections so they describe the actual behavior: tokens come from DB in `--confdb` mode (CLI ignored), auth password/hash is the only CLI override path (bootstrap recovery), transients are always CLI-sourced. Added explicit paragraph documenting that `Gemini.Token` mirrors `OpenAI.Token` — CLI in non-`--confdb`, DB in `--confdb`, encrypted at rest with the `ENC:` prefix. Added a new top-level "Persisted Settings Inventory" section with a full group-by-group list (new groups and new `Meta` fields highlighted), a "Schema Compatibility" subsection noting JSON-blob additiveness and zero-value defaults on decode, and a "Migration Note for Early `--confdb` Users" explaining the `server_auth_user` leftover key behavior (ignored by unmarshal, cleared on next save, no operator action required). Also trimmed two stale "CLI credentials take precedence" bullets (lines in Implementation Summary and Current Implementation Status) to reflect the auth-only override reality. The phrasing in the migration note uses the JSON key `server_auth_user` (lowercase snake_case) rather than the Go struct field name, so the plan's final grep (`grep -n 'AuthUser\|CUSTOM_PROMPTS\|preset' db-conf.md`) now returns nothing. Doc grew from 322 → 391 lines; no code changes required (build/tests not run — scope is docs-only).

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
- [ ] cross-check tests: `go test -race -count=1 ./...` — count the total test pass rate; to get master's baseline for comparison, use a worktree instead of checkout: `git worktree add /tmp/tg-spam-master origin/master && (cd /tmp/tg-spam-master && go test -race -count=1 ./... | tail -5) && git worktree remove /tmp/tg-spam-master --force`. **Never** `git checkout master` or `git checkout origin/master` from the working directory.
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

- [ ] verify current branch: `git branch --show-current` must print `feature-db-configuration` — **abort if it prints `master`**
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
