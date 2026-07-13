# Prohibited Languages Spam Detector

## Overview
- Add a spam checker that flags messages containing enough letters of an admin-configured set of prohibited Unicode scripts.
- Solves: English-only chats getting spam in other scripts (Chinese observed). Admin sets a blocklist; a message with >= N prohibited-script letters is marked spam.
- Hard block: bypasses the LLM and returns spam immediately (policy rule the LLM can't override), mirroring `isShortMsgFlood`.
- Integrates as one more content check in `Detector.Check`, placed after the approval gate so it runs on non-approved users in the normal config.

## Context (from discovery)
- Files/components involved:
  - `lib/tgspam/detector.go` — `Check` (:229), `Config` (:108), `isMultiLang` (:1215, reuse `unicode.Scripts` approach), `isShortMsgFlood` (:1099, hard-return template)
  - `app/main.go` — flags struct, `validateSettings(s *config.Settings)` (:392), `makeDetector(settings *config.Settings)` (:759, no error return)
  - `app/settings.go` — `optToSettings`
  - `app/config/settings.go` — typed `Settings`, `zeroAwarePaths`
  - `app/webapi/config.go` — form parsing; `app/webapi/assets/settings.html` — edit inputs + read-only rows
  - `e2e-ui/e2e_test.go` — settings round-trip
  - `README.md`, `CLAUDE.md`
- Related patterns found:
  - `isMultiLang` already walks runes against `unicode.Scripts` tables with an ascii fast path.
  - `isShortMsgFlood` is the hard-return template (`return true, cr` + `d.spamHistory.Push(req)`), but it bails on `req.CheckOnly` because it is behavioral. This check must NOT copy that bail.
  - Approval gate at detector.go:250 short-circuits content checks only when `FirstMessageOnly` is true.
- Dependencies: none new; stdlib `unicode` only.

## Development Approach
- **testing approach**: Regular (code, then tests within the same task).
- complete each task fully before the next; small focused changes.
- **CRITICAL: every task MUST include new/updated tests** for code changes in that task (success + error scenarios), listed as separate checklist items.
- **CRITICAL: all tests must pass before starting next task.**
- run tests after each change; maintain backward compatibility (feature disabled by default, empty list).

## Code-Quality Rules (HARD — verify against every task before marking complete)

These rules supplement project CLAUDE.md and are NOT optional. They are the gate for marking any task complete. If a rule is violated, the task is not done — refactor, re-test, then mark complete.

**Signatures (hard limits):**
- No function or method has 4+ parameters. `ctx context.Context` does not count toward the budget. If you need 4+, use an option struct (e.g., `type fooOpts struct { ... }`).
- No function or method has 4+ return values. Split the function into two single-purpose ones, or return a struct.
- Multiple adjacent same-type parameters (`oldLine, newLine int`) are a swap hazard — review whether they belong on a struct.

**Methods vs standalone helpers (project rule, hard):**
- If a function is called only from methods of a single struct, it MUST be a method on that struct. Calling pattern decides, not field access.
- Standalone helpers are reserved for: (a) constructors and entry points (`Parse...`, `New...`, `Decorate...`), (b) utilities shared by multiple unrelated types or by both standalone functions AND methods, (c) tiny cross-cutting helpers.
- Before adding any standalone helper, mentally walk its callers. If every caller is a method of one type, make the helper a method on that type.

**Visibility (private by default, hard):**
- Lowercase identifiers by default. Only export when an out-of-package caller exists.
- Exception (per CLAUDE.md): methods called by other structs in the same package CAN be exported for inter-component API clarity. This is the only exception. It does not extend to types, functions, constants, or variables.
- Before exporting any new identifier, grep for cross-package callers. If none, lowercase it.

**Comments (default: none, hard):**
- Default to writing no comments. Add one only when the WHY is non-obvious (a hidden invariant, a workaround, behavior that would surprise a reader).
- Exported items get godoc comments starting with the name. Unexported items get lowercase non-godoc comments — or no comment at all.
- Never describe WHAT the code does when the code itself is self-evident. Never write multi-paragraph comments on routine helpers.

**Per-task gate (before marking ANY checkbox complete):**
1. Formatter runs clean (`~/.claude/format.sh` or `gofmt -s -w` + `goimports -w`).
2. `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0` reports zero issues.
3. `go test ./... -race` passes.
4. Scan the new code for the four rule classes above. Specifically:
   - Grep new function signatures: `grep -nE '^func.*\(.*,.*,.*,.*\)' app/<path>/*.go` — any hit with 4+ comma-separated params (excluding `ctx`) is a violation. Same for the return-value side.
   - For every new standalone helper, `grep -rn 'helperName(' --include='*.go'` and confirm at least one caller is NOT a method of a single type. If all callers are methods of one type, convert.
   - For every new exported identifier, grep cross-package. If no out-of-package hit, lowercase it.
5. Only after 1–4 pass: mark the task complete.

If a previous task shipped a violation (spotted later by user, reviewer, or yourself): fix it in the next commit BEFORE starting the next task. Do not let violations accumulate.

## Testing Strategy
- **unit tests**: required for every task. Follow `lib/tgspam/detector_test.go` patterns (table-driven, testify). One test file per source file (`detector_test.go` for `detector.go`).
- **e2e tests**: this project has Go+Playwright e2e in `e2e-ui/`. The two new settings fields must be added to the settings round-trip test (Task 7), treated with the same rigor as unit tests.

## Progress Tracking
- mark completed items `[x]` immediately; add ➕ for new tasks, ⚠️ for blockers; keep plan in sync.

## Solution Overview
- One resolver (`ResolveProhibitedScripts`, exported — called cross-package from `app/main.go`) turns the config string list into a validated `map[string]*unicode.RangeTable` (script name → table), applying friendly aliases and passing through raw `unicode.Scripts` names. Used by both `validateSettings` (fail fast on typos) and `makeDetector`.
- `Detector.Config` gains `ProhibitedScripts map[string]*unicode.RangeTable` and `ProhibitedLangsMin int`. The map (not a slice) is stored because the check reports which script hit in `Details` and the resolver's map is directly settable cross-package from `app`.
- `Detector.isProhibitedLang` counts prohibited-script letters and hard-returns spam at the threshold.
- Two config knobs plumbed through all layers; disabled by default (empty list).

## Technical Details
- Resolver signature (exported for cross-package use by `app/main.go`):
  - `func ResolveProhibitedScripts(names []string) (map[string]*unicode.RangeTable, error)`
  - alias map (lowercased input): `chinese→Han`, `russian→Cyrillic`, `ukrainian→Cyrillic`, `arabic→Arabic`, `korean→Hangul`, `japanese→[Hiragana,Katakana]`, `hebrew→Hebrew`, `thai→Thai`, `greek→Greek`. An alias may map to more than one script (japanese), each added to the result map under its `unicode.Scripts` name.
  - non-alias entries resolved case-insensitively against `unicode.Scripts` keys; unknown → error listing the bad name.
  - empty/whitespace entries skipped; empty input → empty map, nil error.
- Detector method (method, not standalone — called only from `Detector.Check`):
  - `func (d *Detector) isProhibitedLang(msg string) spamcheck.Response`
  - walk runes; skip non-letters via `!unicode.IsLetter(r)`; for each letter test membership across `d.ProhibitedScripts` tables; increment per-script counts; once any script's count reaches `d.ProhibitedLangsMin`, return `spamcheck.Response{Name: "prohibited-language", Spam: true, Details: fmt.Sprintf("%s: %d", name, count)}`.
  - below threshold → `Response{Name: "prohibited-language", Spam: false, Details: "..."}`.
  - does NOT check `req.CheckOnly` (pure content check; web `/check` UI must exercise it). Takes `msg string`, not the full request, for the same reason.
- `Check` wiring (after the short-msg-flood block ~:265, before stop-words):
  ```go
  if len(d.ProhibitedScripts) > 0 {
      if resp := d.isProhibitedLang(req.Msg); resp.Spam {
          cr = append(cr, resp)
          d.spamHistory.Push(req)
          return true, cr
      }
  }
  ```
- Scope note (documented behavior): with the default `FirstMessageOnly=true`, the approval gate at :250 means this runs only for non-approved users. In paranoid mode / `FirstMessageOnly=false`, it also applies to approved users — same as every other content check. Not rejected as an incompatible combo (this check works regardless of the first-message path).
- Config knobs:
  - `--prohibited-langs` / `PROHIBITED_LANGS` (string, default `""`, empty disables)
  - `--prohibited-langs-min` / `PROHIBITED_LANGS_MIN` (int, default `3`)
- Validation (`validateSettings(s *config.Settings)`, runs in CLI and `--confdb` modes): resolve the list (error on unknown name); if list non-empty, require `min >= 1`.
- Aggression note (documented): no `MinMsgLen` gate — a message with `min` foreign letters is flagged regardless of length (short foreign spam is the target). Admins raise `--prohibited-langs-min` if too aggressive.

## What Goes Where
- **Implementation Steps** (`[ ]`): resolver, detector check + config, CLI/env + validation, DB settings, webapi, e2e, docs.
- **Post-Completion** (no checkboxes): manual live verification in a test chat, deployment (rebuild image + redeploy on master.radio-t.com).

## Implementation Steps

### Task 1: Script resolver with alias map

**Files:**
- Create: `lib/tgspam/prohibited.go`
- Create: `lib/tgspam/prohibited_test.go`

- [x] create `lib/tgspam/prohibited.go` with `ResolveProhibitedScripts(names []string) (map[string]*unicode.RangeTable, error)` and an unexported `scriptAliases` map (alias → []scriptName)
- [x] add a godoc comment on `ResolveProhibitedScripts` starting with the name (exported symbol)
- [x] resolve each entry: trim + lowercase; skip empty; expand via `scriptAliases`, else match case-insensitively against `unicode.Scripts`; error on unknown with the offending name
- [x] write tests for aliases (chinese→Han, japanese→Hiragana+Katakana), raw script names (Cyrillic, Arabic), case-insensitivity, empty input, whitespace-only entries
- [x] write tests for error case (unknown name returns error naming it)
- [x] run `go test ./lib/tgspam/ -race` — must pass before task 2

### Task 2: Detector check and Config wiring

**Files:**
- Modify: `lib/tgspam/detector.go`
- Modify: `lib/tgspam/detector_test.go`

- [x] add `ProhibitedScripts map[string]*unicode.RangeTable` and `ProhibitedLangsMin int` to `Config` (detector.go:108), each with a godoc comment starting with the field name (exported fields); they flow into the `Detector` via the embedded config
- [x] add method `(d *Detector) isProhibitedLang(msg string) spamcheck.Response` — letters-only rune walk, per-script count, early-return `Spam:true` at `ProhibitedLangsMin`, `Details` reports `"<script>: <count>"`; NO `req.CheckOnly` bail
- [x] wire the hard-return block into `Check` after the short-msg-flood block (~:265), gated by `len(d.ProhibitedScripts) > 0`, pushing `spamHistory` and returning `true, cr`
- [x] write tests: han/cyrillic/arabic messages at/above threshold flagged; below-threshold not flagged; mixed English+few CJK below threshold passes; digits/punctuation/emoji do not count toward the letter count; `Details` names the script
- [x] write test: with no prohibited scripts configured the check is a no-op (feature off by default)
- [x] write test: approved user bypass — with `FirstMessageOnly=true` and an approved user, `Check` returns pre-approved before the prohibited-lang check runs
- [x] run `go test ./lib/tgspam/ -race` — must pass before task 3

### Task 3: DB-backed Settings (typed fields first, so later layers compile)

**Files:**
- Modify: `app/config/settings.go`
- Modify: `app/config/settings_test.go`

- [x] add `ProhibitedLangs string` (`json/yaml/db:"prohibited_langs"`) and `ProhibitedLangsMin int` (`json/yaml/db:"prohibited_langs_min"`) to the typed `Settings` struct
- [x] add `"ProhibitedLangs"` to `zeroAwarePaths` (empty = disabled must survive merges); comment referencing the detector gate. `ProhibitedLangsMin` stays OUT (default 3; zero is not a meaningful "disabled")
- [x] write tests: round-trip (marshal/unmarshal) of both fields
- [x] write test: merge guard — build an explicit template with `ProhibitedLangs="chinese"` and a target with `ProhibitedLangs=""`, assert the merge keeps it `""` (the CLI default is empty, so the template value must be set artificially to exercise the guard; mirrors the `MaxShortMsgCount` zeroAwarePaths precedent)
- [x] run `go test ./app/config/ -race` — must pass before task 4

### Task 4: CLI flags, env, settings mapping, startup validation

**Files:**
- Modify: `app/main.go`
- Modify: `app/settings.go`
- Modify: `app/main_test.go`
- Modify: `app/settings_test.go`

- [x] add `ProhibitedLangs string` (`--prohibited-langs` / `PROHIBITED_LANGS`, default `""`) and `ProhibitedLangsMin int` (`--prohibited-langs-min` / `PROHIBITED_LANGS_MIN`, default `3`) to the options struct in `app/main.go`
- [x] in `optToSettings` (app/settings.go, returns `*config.Settings`): map both opts → the `Settings` fields added in Task 3
- [x] in `validateSettings(s *config.Settings)` (app/main.go:392, runs in BOTH CLI and `--confdb` modes): split `s.ProhibitedLangs` on comma, call `tgspam.ResolveProhibitedScripts`, return error on unknown; if the list is non-empty require `s.ProhibitedLangsMin >= 1`
- [x] in `makeDetector(settings *config.Settings)` (no error return): resolve with `scripts, _ := tgspam.ResolveProhibitedScripts(...)` (error already caught by `validateSettings`) and set `Config.ProhibitedScripts` + `Config.ProhibitedLangsMin`. Keep this resolve block short/distinct from the `validateSettings` one to avoid a `dupl` false positive
- [x] write tests: `validateSettings` accepts known aliases/script names, rejects an unknown name, rejects `min < 1` with a non-empty list; `optToSettings` maps both fields
- [x] run `go test ./app/ -race` — must pass before task 5

### Task 5: Web settings UI (edit + read-only) and form parsing

**Files:**
- Modify: `app/webapi/assets/settings.html`
- Modify: `app/webapi/config.go`
- Modify: `app/webapi/config_test.go`

- [x] add edit inputs to `settings.html`: text input `prohibitedLangs` bound to `{{.ProhibitedLangs}}` and number input `prohibitedLangsMin` bound to `{{.ProhibitedLangsMin}}` (mirror `multiLangWords` for the int)
- [x] add read-only display rows for both (mirror the `Multi Lingual Words` row)
- [x] parse `prohibitedLangs` with the CLEARABLE presence-gated pattern (like `metaUsernameSymbols`, config.go:316): `if _, ok := r.Form["prohibitedLangs"]; ok { settings.ProhibitedLangs = r.FormValue("prohibitedLangs") }` so an empty submit DISABLES the feature. Do NOT use the `if val := r.FormValue(); val != ""` gate (it can't clear). Parse `prohibitedLangsMin` as int like `reportAutoBanThreshold`
- [x] write test: form parse sets both fields
- [x] write test: clear-to-empty — set `prohibitedLangs` non-empty, then submit empty, assert the field is cleared (guards the disable path; the naive `val != ""` gate would fail this)
- [x] run `go test ./app/webapi/ -race` — must pass before task 6

### Task 6: e2e settings persistence

**Files:**
- Modify: `e2e-ui/e2e_test.go`

- [x] add `TestSettings_ProhibitedLangsPersists` mirroring `TestSettings_MaxShortMsgCountPersists` (e2e_test.go:368) — spin up a `--confdb` server via the `save-config` bootstrap, fill `#prohibitedLangs` / `#prohibitedLangsMin`, save, reload, assert persisted (a full per-field server test ~100 lines, not a one-line addition; there is no shared "round-trip" test)
- [x] run the e2e-ui suite locally per its documented command (compiled; `go vet -tags=e2e ./e2e-ui/...` clean; full browser run deferred — playwright driver download blocked by no network in this env, covered by CI)

### Task 7: Documentation

**Files:**
- Modify: `README.md`
- Modify: `CLAUDE.md`

- [x] README: add both flags to the "All Application Options" table (match `--help` exactly)
- [x] README: add a "Prohibited Languages" descriptive section — how it works, the alias set, the Han=Chinese-also-matches-Japanese-kanji caveat, count threshold + aggression note, hard-block/non-approved-scope behavior
- [x] CLAUDE.md: add a "Prohibited Languages Detection" subsection under Spam Detection Architecture (mirror the "Short Message Flood Detection" section) covering placement, hard-return, no-CheckOnly-bail, scope-in-paranoid-mode, resolver location, config knobs + zeroAwarePaths
- [x] no tests (docs only)

### Task 8: Verify acceptance criteria
- [x] verify all Overview requirements are implemented (blocklist, count threshold, hard block, non-approved scope by placement, friendly-names+passthrough config) — confirmed by reading shipped code: `ProhibitedScripts` map in `Config` (detector.go:128), `ProhibitedLangsMin` threshold, hard-return `true, cr` (detector.go:277-281) after the approval gate (:257), alias+passthrough resolver (prohibited.go)
- [x] verify edge cases: empty list = disabled (`len(d.ProhibitedScripts) > 0` gate); unknown name rejected at startup (`validateSettings` main.go:419-422); letters-only counting (`!unicode.IsLetter` detector.go:1190); `CheckOnly` still runs the check (no bail; test detector_test.go:3926)
- [x] run full suite: `go test -race ./...` — all packages pass
- [x] run e2e-ui suite per its documented command — compiles clean (`go vet -tags=e2e ./e2e-ui/...` clean); full browser run driver-skipped (no network for playwright driver download in this env, per Task 6), covered by CI
- [x] `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0` clean (0 issues); coverage meets project standard — touched packages total 84.7%, new `ResolveProhibitedScripts` and `isProhibitedLang` at 100%

### Task 9: [Final] Finalize documentation
- [x] confirm README `--help` table matches actual `--help` output — `TestREADMEAllOptionsMatchesHelp` passes; both `--prohibited-langs` / `--prohibited-langs-min` rows (README.md:593-594) match `--help`; descriptive "Prohibited languages" section (README.md:201-209) accurate to shipped behavior
- [x] confirm CLAUDE.md section is accurate to the shipped code — spot-checked all cited references: `isProhibitedLang` at detector.go:1180, Check wiring detector.go:274-282, Config fields detector.go:128-133, `ResolveProhibitedScripts` prohibited.go, validateSettings main.go:419, makeDetector main.go:791, zeroAwarePaths config/settings.go:325, alias set — all correct, no edits needed
- [x] move plan to completed/ (deferred to exec orchestrator final step to keep the file available for review phases)

## Post-Completion
*Items requiring manual intervention or external systems - no checkboxes, informational only*

**Manual verification:**
- in a test chat, post a Chinese message from an unapproved user with `--prohibited-langs=chinese` set; confirm it is flagged/banned and the admin notification shows the `prohibited-language` check.
- confirm a normal English message with a stray single foreign character is NOT flagged (below threshold).

**External system updates:**
- rebuild the `umputun/tg-spam:master` image and redeploy the `tg-spam` container on `master.radio-t.com`, then set `PROHIBITED_LANGS` (and optionally `PROHIBITED_LANGS_MIN`) in that compose env to enable it for `radio_t_chat`.

---
Smells pre-check: 2 items fixed before save (explicit godoc checklist items added to Task 1 and Task 2; no hard-limit signature violations).
Plan-review (auto): applied — reordered Task 3/4 (typed config.Settings fields before optToSettings/validateSettings/makeDetector, else non-compilable), corrected `validate()`→`validateSettings(s *config.Settings)`, switched web parse of `prohibitedLangs` to the clearable presence-gated pattern (+ clear-to-empty test), fixed merge-test to use an artificial non-empty template, and named the e2e test `TestSettings_ProhibitedLangsPersists` (per-field, not a shared round-trip). Design decisions unchanged.
