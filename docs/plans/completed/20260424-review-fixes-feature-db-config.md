# Review Fixes for feature-db-configuration

## Overview

External review of the `feature-db-configuration` branch surfaced 9 confirmed
issues (3 High, 4 Medium, 2 Low). They cluster around three failure modes plus
documentation/UI cleanup:

1. `--confdb` startup silently overrides operator CLI intent for paths/listen
   and can disable web auth when DB row is partial (#1, #3).
2. DB-loaded config has no defaults-fill — missing JSON keys become Go zero
   values instead of CLI defaults, causing detector and server misbehavior (#2).
3. Web settings UI has data-integrity gaps (failed save mutates memory, broken
   HTMX target, meta toggle is cosmetic, reactions missing entirely)
   (#4, #5, #6, #8).

Plus two documentation/whitespace items (#9, #10). Finding #7 (Server.AuthUser
persistence) is intentional behavior per commit `bf08d62a` and is excluded.

## Context (from discovery)

Files involved:
- `app/main.go` — `loadConfigFromDB` (line 1507), `applyCLIOverrides` (1469),
  `optToSettings` (1284), `activateServer` (597), CLI option struct (42–210)
- `app/config/settings.go` — `Settings`, `TransientSettings`, `New()`
- `app/config/store.go` — `Store.Load` (135), `Store.Save` (174)
- `app/webapi/config.go` — `updateConfigHandler` (108), `updateSettingsFromForm`
- `app/webapi/assets/settings.html` — form, response target, meta-checks UI

Patterns observed:
- `applyCLIOverrides` is the established precedence-rule funnel: empty/default
  CLI value never overrides DB; only explicit non-defaults reapply.
- Config domain already separates `Settings` (persisted) vs `Transient` (never
  persisted). Defaults are encoded only in `default:` struct tags on the CLI
  options, not in the config package — that asymmetry is the root of #2.
- Web UI updates write to live `s.AppSettings` under `s.appSettingsMu`; the
  mutex covers ordering but not transactional rollback.
- `updateSettingsFromForm` already uses `r.Form` presence checks for fields
  that aren't always rendered (meta contact-only/giveaway, username symbols).
  Reactions handling should follow this pattern.

Key dependencies:
- `go-flags` carries `default:` tags as the single source of CLI defaults.
  We extract them at runtime by re-parsing an empty argv (which populates
  the struct from defaults) — no constants duplication, no drift possible.
- `bcrypt` hash generation lives in `generateAuthHash` (called by
  `activateServer`); legacy mode propagates `Transient.WebAuthPasswd="auto"`
  via `optToSettings:1448`, then `activateServer:606-612` calls
  `generateAuthHash("auto")` which generates a random password.

## Development Approach

- **testing**: every code-change task ships with new/updated tests covering
  both success and error cases. TDD (failing-test-first) is recommended for
  the subtle behavioral fixes — Tasks 1, 2, 5, 6, 8 — where it's easy to
  miss cases (zero-aware fields, env pollution, AuthFromCLI guard, slice
  rollback safety, all 11 IsMetaEnabled fields). For mechanical wiring,
  doc, and whitespace work, write tests in whatever order is natural.
- complete each task fully before moving to the next
- make small, focused changes
- all tests must pass before starting the next task
- update this plan file when scope changes during implementation
- run `go test -race ./...` after each change
- run `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0`
  before committing
- maintain backward compatibility — no DB blob shape change, no removed
  CLI flags

## Testing Strategy

- **unit tests**: required for every task; table-driven with subtests where
  multiple cases apply, single-line struct fields up to 130 chars (per
  CLAUDE.md).
- **integration**: tasks touching `loadConfigFromDB` add round-trip tests in
  `app/config/store_test.go` or `app/main_test.go`.
- **web-handler tests**: tasks touching `app/webapi/config.go` extend
  `app/webapi/config_test.go` with moq for `SettingsStore`.
- **drift-detection test**: not needed — defaults are extracted from CLI
  tags at runtime via reflection, so there's nothing to drift against.
  CLI tags remain the only source of truth.
- **HTML-level test**: task #5 needs a test that asserts the response body
  contains `id="update-result"` so future regressions surface.
- no e2e suite exists in this repo; UI changes verified via handler tests
  only.

## Progress Tracking

- mark completed items with `[x]` immediately when done
- add newly discovered tasks with ➕ prefix
- document issues/blockers with ⚠️ prefix
- update plan if implementation deviates from original scope
- keep plan in sync with actual work done

## Solution Overview

Three architectural decisions, established up-front (confirmed with user):

1. **Path/listen overrides via `applyCLIOverrides`** (not via moving fields to
   `Transient`). Compare `opts.*` to the documented `default:` value (or to
   empty for fields with no default tag); reapply the operator's explicit
   value. Same shape as existing token/auth/dry overrides. No DB blob shape
   change.
2. **Defaults-fill via `(*Settings).ApplyDefaults(template)`** on the
   `config` package. The template is built at startup by walking reflection
   on a zero `options` value, reading each field's `default:` struct tag,
   and assigning typed values via a small dispatcher (`time.Duration`,
   `int`, `int32`, `int64`, `float64`, `bool`, `string`). CLI `default:`
   tags are the single source of truth — no constants table to maintain.
   Called once after `loadConfigFromDB` returns. The same template is
   also passed to `applyCLIOverrides` so override comparisons share the
   same source. **Important**: do NOT use `go-flags.Parse(argv=[])` to
   build the template — go-flags reads env vars during default-fill
   (`option.go:336`), which would pollute the template with ambient env
   values like `SERVER_LISTEN`, breaking both the override comparison
   and the defaults-fill semantics.
3. **Auto-auth trigger on empty+empty (excluding explicit CLI empty).**
   The `"auto"` sentinel is a CLI artifact. In `--confdb` mode, after
   `loadConfigFromDB` and `applyCLIOverrides`, if
   `Server.Enabled && AuthHash == "" && Transient.WebAuthPasswd == "" &&
   !Transient.AuthFromCLI`, set `Transient.WebAuthPasswd = "auto"` so the
   existing `generateAuthHash("auto")` path in `activateServer` fires the
   same way legacy mode does. The `AuthFromCLI` check preserves the
   existing "operator explicitly passed `--server.auth=` to disable auth"
   semantics (`applyCLIOverrides:1483-1488` already sets
   `AuthFromCLI=true` on any non-"auto" CLI value). Random-password
   generation only fires when neither source yielded anything AND the
   operator did not deliberately opt out.

UI/data-integrity fixes are tactical:
- Snapshot-and-restore around the save call to roll back failed saves
  (load-bearing assumption: `updateSettingsFromForm` always replaces slices
  wholesale and never mutates in place; this is enforced by a regression
  test).
- Wrap success response in `<div id="update-result">…</div>` to keep HTMX
  target stable across saves.
- Server-side authoritative meta-toggle: when `metaEnabled=false` AND the
  form contains at least one meta-related field, force `LinksLimit=-1`,
  `MentionsLimit=-1`, `UsernameSymbols=""`. When the form contains no meta
  fields at all, do nothing (preserves existing settings on partial saves).
- Add `Reactions` to `updateSettingsFromForm` (gated on `r.Form` presence,
  mirroring `metaContactOnly` pattern) and add UI controls beside Duplicates.

Doc/whitespace: append `Available commands` to README options block (with a
generative test); strip trailing whitespace.

## Technical Details

### Zero-aware defaults handling (P2)

Per reviewer feedback: blobs are produced exclusively via `optToSettings`,
which copies CLI defaults into `Settings`. So a saved blob never has `0` for
fields like `Meta.LinksLimit` (CLI default `-1`) unless the operator
explicitly typed `--meta.links-limit=0` and ran `save-config`. That explicit
zero IS the operator's intent.

Therefore `ApplyDefaults` does NOT fill the following fields even if they
load as zero — zero is either documented as "disabled" or is a legitimate
operator value:

- `Meta.LinksLimit`, `Meta.MentionsLimit` (default `-1`, where 0 = "limit 0",
  -1 = disabled)
- `MaxEmoji` (default `2`, description says `-1 to disable`)
- `MultiLangWords` (default `0`, no special semantics)
- `MaxBackups` (default `10`, description says `set 0 to disable`)
- `Reactions.MaxReactions` (default `0` = disabled)
- `Duplicates.Threshold` (default `0` = disabled)
- `Report.AutoBanThreshold` (default `0` = disabled)
- `OpenAI.HistorySize`, `Gemini.HistorySize` (default `0`)

This eliminates the need for a JSON-key-presence side channel — `Store.Load`
stays a pure unmarshal, and `ApplyDefaults` is a simple zero-check sweep
over the remaining fields below.

### Defaults source: reflection on CLI tags (no duplication, env-immune)

**Decision**: do not maintain a separate defaults table, and do NOT use
`go-flags.Parse([]string{})` to derive them. CLI struct tags
(`default:"…"` on `options`) are the single source of truth. Build the
template `*config.Settings` at startup by walking reflection over a zero
`options` value, reading each field's `default:` tag, and assigning a
typed value via a small dispatcher.

**Why reflection, not go-flags re-parse?** Verified against go-flags v1.6.1
source. `option.go:328-352` (`clearDefault`) shows that during parser
init, every option with `EnvDefaultKey` set is overridden by
`os.LookupEnv(envKey)` BEFORE the `default:` tag is consulted. Since
nearly every field on `options` has an `env:` tag (e.g.,
`SERVER_LISTEN`, `REPORT_RATE_LIMIT`, `FILES_DYNAMIC`), running
`Parse([]string{})` on a process where any of those env vars are set
yields a "default" template polluted with operator-supplied values.
That breaks both `applyCLIOverrides` (opts.X and defaults.X both equal
the env value, so the override never fires) and `ApplyDefaults` (a DB
load missing a key gets filled from ambient env rather than the
canonical struct-tag default).

Pure reflection on `default:` tags avoids the env-read entirely. The
type dispatcher is small — only the types actually used by `options`
need handling: `string`, `int`, `int32`, `int64`, `float64`, `bool`,
`time.Duration`. Lists (`[]string`, `[]int64`) have no `default:` tags
in the current options struct, so they're left at zero (matching CLI
behavior).

```go
// in main.go (or a small init helper)
func defaultSettingsTemplate() (*config.Settings, error) {
    var defaults options
    if err := applyStructTagDefaults(reflect.ValueOf(&defaults).Elem()); err != nil {
        return nil, fmt.Errorf("failed to derive defaults from struct tags: %w", err)
    }
    return optToSettings(defaults), nil
}

// applyStructTagDefaults walks v recursively, parsing `default:"..."` tags
// on leaf fields and assigning typed values. Skips fields without a
// default tag (their zero value matches the CLI behavior when the flag is
// omitted and no env is set).
func applyStructTagDefaults(v reflect.Value) error { /* ~50 LOC */ }
```

Pros:
- single source of truth — every CLI default automatically becomes a DB
  default; no drift possible
- no `app/config/defaults.go` constants table to maintain
- no drift-detection test needed
- env-immune: works the same regardless of operator's environment
- `applyCLIOverrides` uses the same template (compares `opts.X` to
  `defaults.X` rather than to a hardcoded constant), so override logic
  and defaults-fill share their single source

Cons:
- ~50 LOC of reflection code (covered by tests, including type-dispatch
  edge cases)
- if a future contributor adds a new type to `options` (e.g., `uint`),
  they need to extend the dispatcher; tests catch this

**Excluded from fill (zero-aware list)**: documented zero is operator
intent. ApplyDefaults must skip these field paths regardless of template
value:
- `Meta.LinksLimit`, `Meta.MentionsLimit` (CLI default `-1`; 0 = "limit 0")
- `MaxEmoji` (CLI default `2`; description allows -1 to disable, so 0 is
  meaningful)
- `MultiLangWords` (CLI default `0`)
- `MaxBackups` (CLI default `10`; description: `set 0 to disable`)
- `Reactions.MaxReactions` (CLI default `0` = disabled)
- `Duplicates.Threshold` (CLI default `0` = disabled)
- `Report.AutoBanThreshold` (CLI default `0` = disabled)
- **`Report.RateLimit`** (verified: `app/events/reports.go:154` —
  `if r.RateLimit <= 0 { return false, nil }`. Zero disables rate
  limiting at runtime, so an operator who saved with
  `--report.rate-limit=0` deserves to keep that intent.)
- `OpenAI.HistorySize`, `Gemini.HistorySize` (CLI default `0`)

Implementation requires a one-time grep audit (Task 1) to find any other
fields where the runtime treats `<= 0` or `== 0` as "disabled" — every
such field must be in `zeroAwarePaths`.

Implementation pattern: a hardcoded `var zeroAwarePaths = map[string]bool{
"Meta.LinksLimit": true, ...}` inside `app/config/settings.go`, consulted
during the reflection walk. If the field path is in the map, leave the
loaded value alone regardless of template value.

`applyCLIOverrides` uses the template the same way:

```go
defaults, err := defaultSettingsTemplate()
if err != nil { ... }

if opts.Server.ListenAddr != defaults.Server.ListenAddr {
    settings.Server.ListenAddr = opts.Server.ListenAddr
}
if opts.Files.DynamicDataPath != defaults.Files.DynamicDataPath {
    settings.Files.DynamicDataPath = opts.Files.DynamicDataPath
}
if opts.Files.SamplesDataPath != "" {
    // SamplesDataPath has no default tag; empty means "use DynamicDataPath"
    settings.Files.SamplesDataPath = opts.Files.SamplesDataPath
}
```

The template is built once in `main()` and passed to both
`applyCLIOverrides` and `ApplyDefaults`.

### CLI override extension (P1)

`applyCLIOverrides` now takes the template `*config.Settings` (same one used
by `ApplyDefaults`) so its comparisons share the same source of truth.
Pattern shown in the "Defaults source" section above. No standalone
constants file is needed.

### Auto-auth fallback when DB and CLI both empty (P3)

In `activateServer`, before the existing hash/password branches:

```go
if settings.Server.Enabled &&
    settings.Server.AuthHash == "" &&
    settings.Transient.WebAuthPasswd == "" &&
    !settings.Transient.AuthFromCLI {
    log.Print("[WARN] no auth configured (DB empty, no CLI override) — generating random password")
    settings.Transient.WebAuthPasswd = "auto"
}
```

The `!AuthFromCLI` guard is critical: `applyCLIOverrides:1483-1488`
already sets `Transient.AuthFromCLI = true` whenever the operator passes
ANY `--server.auth=...` value other than `"auto"` (including
`--server.auth=""`, which deliberately disables auth). Without this
guard, the new fallback would silently re-enable authentication for an
operator who explicitly opted out via `--server.auth=`. With the guard,
the fallback fires only when neither source provided anything AND no
explicit CLI override happened.

Verification of legacy parity: `optToSettings` (`app/main.go:1448`) sets
`settings.Transient.WebAuthPasswd = opts.Server.AuthPasswd`. The CLI default
for `AuthPasswd` is `"auto"` (line 195), so legacy mode flows
`Transient.WebAuthPasswd = "auto"` → `activateServer` (line 606) →
`generateAuthHash("auto")` → random password. In legacy mode, the new
guard does not fire (WebAuthPasswd is already non-empty `"auto"`), and
existing behavior is unchanged. The new guard reproduces the auto-pwd
state only when both sources are empty in `--confdb` mode AND no
explicit CLI auth override was provided.

`generateAuthHash` already logs the generated password once when invoked with
`"auto"`. The new `[WARN] no auth configured` log is in addition — it
explains WHY the trigger fired (empty DB + empty CLI), distinct from the
existing "generated password is X" log. Both logs serve different purposes;
keep both. Verified during implementation that no double-generation occurs.

### Save-rollback (P4)

In `updateConfigHandler`:

```go
s.appSettingsMu.Lock()
defer s.appSettingsMu.Unlock()

// snapshot MUST precede form mutation
snapshot := *s.AppSettings
updateSettingsFromForm(s.AppSettings, r)

if saveToDB {
    if err := s.SettingsStore.Save(r.Context(), s.AppSettings); err != nil {
        *s.AppSettings = snapshot // rollback
        log.Printf("[ERROR] failed to save updated configuration: %v", err)
        http.Error(w, fmt.Sprintf("Failed to save configuration: %v", err), http.StatusInternalServerError)
        return
    }
}
```

Snapshot is a value copy. `Settings` contains slices (`Admin.SuperUsers`,
`OpenAI.CustomPrompts`, `Gemini.CustomPrompts`, `LuaPlugins.EnabledPlugins`).
The rollback restores the original slice header — correct behavior **only as
long as `updateSettingsFromForm` always replaces slices wholesale rather
than mutating elements in place**. This is the load-bearing invariant; a
code comment at the rollback site documents it, and a regression test
verifies it.

### HTMX target preservation (P5)

Server response wraps in `<div id="update-result">`:

```go
w.Write([]byte(`<div id="update-result" class="alert alert-success">Configuration updated successfully</div>`))
```

The HTML success div doubles as the next submit's target — `outerHTML` swap
replaces it with the new server response, which also carries the id, keeping
the target stable.

Test asserts the response body contains `id="update-result"`.

### Meta toggle (P6)

`Settings.IsMetaEnabled()` (verified at `app/config/settings.go:253`)
returns true if ANY of these 11 fields is set: `Meta.LinksLimit >= 0`,
`Meta.MentionsLimit >= 0`, `Meta.UsernameSymbols != ""`, or any of the
booleans `LinksOnly`, `ImageOnly`, `VideosOnly`, `AudiosOnly`,
`Forward`, `Keyboard`, `ContactOnly`, `Giveaway`. To make the
`metaEnabled=off` toggle authoritative, ALL 11 fields must be cleared
when it fires — clearing only the three numeric/string fields lets a
checked `metaImageOnly` keep meta enabled.

Replace the existing meta-checks block in `updateSettingsFromForm` with
a gated branch:

```go
metaFormFields := []string{
    "metaEnabled", "metaLinksLimit", "metaMentionsLimit", "metaUsernameSymbols",
    "metaLinksOnly", "metaImageOnly", "metaVideoOnly", "metaAudioOnly",
    "metaForwarded", "metaKeyboard", "metaContactOnly", "metaGiveaway",
}
hasMetaForm := false
for _, k := range metaFormFields {
    if _, ok := r.Form[k]; ok {
        hasMetaForm = true
        break
    }
}

if hasMetaForm {
    metaEnabled := r.FormValue("metaEnabled") == "on"
    if metaEnabled {
        // numeric/string fields - apply form values
        if val := r.FormValue("metaLinksLimit"); val != "" {
            if limit, err := strconv.Atoi(val); err == nil {
                settings.Meta.LinksLimit = limit
            }
        }
        if val := r.FormValue("metaMentionsLimit"); val != "" {
            if limit, err := strconv.Atoi(val); err == nil {
                settings.Meta.MentionsLimit = limit
            }
        }
        if _, ok := r.Form["metaUsernameSymbols"]; ok {
            settings.Meta.UsernameSymbols = r.FormValue("metaUsernameSymbols")
        }
        // boolean fields - reflect form state (presence of "on")
        settings.Meta.LinksOnly = r.FormValue("metaLinksOnly") == "on"
        settings.Meta.ImageOnly = r.FormValue("metaImageOnly") == "on"
        settings.Meta.VideosOnly = r.FormValue("metaVideoOnly") == "on"
        settings.Meta.AudiosOnly = r.FormValue("metaAudioOnly") == "on"
        settings.Meta.Forward = r.FormValue("metaForwarded") == "on"
        settings.Meta.Keyboard = r.FormValue("metaKeyboard") == "on"
        if _, ok := r.Form["metaContactOnly"]; ok {
            settings.Meta.ContactOnly = r.FormValue("metaContactOnly") == "on"
        }
        if _, ok := r.Form["metaGiveaway"]; ok {
            settings.Meta.Giveaway = r.FormValue("metaGiveaway") == "on"
        }
    } else {
        // master toggle off — clear EVERY field used by IsMetaEnabled
        settings.Meta.LinksLimit = -1
        settings.Meta.MentionsLimit = -1
        settings.Meta.UsernameSymbols = ""
        settings.Meta.LinksOnly = false
        settings.Meta.ImageOnly = false
        settings.Meta.VideosOnly = false
        settings.Meta.AudiosOnly = false
        settings.Meta.Forward = false
        settings.Meta.Keyboard = false
        settings.Meta.ContactOnly = false
        settings.Meta.Giveaway = false
    }
}
```

When the form has no meta-related fields at all, the block is skipped
and existing meta state is preserved (matches contact-only/giveaway
pattern). When the master toggle is off and any meta field is in the
form, every field that contributes to `IsMetaEnabled` is cleared, so
`IsMetaEnabled()` returns false unambiguously.

Note: this changes the previous handling of the boolean meta flags
(`metaLinksOnly`, etc.), which were previously unconditionally written
on every form submit. The new behavior only writes them when at least
one meta field is in the form — matching the "no meta fields = no meta
mutation" semantics required for partial-form saves.

### Reactions UI/handler (P8)

In `updateSettingsFromForm`, add presence-gated parsing (mirroring
`metaContactOnly`):

```go
if _, ok := r.Form["reactionsMaxReactions"]; ok {
    if val, err := strconv.Atoi(r.FormValue("reactionsMaxReactions")); err == nil {
        settings.Reactions.MaxReactions = val
    }
}
if _, ok := r.Form["reactionsWindow"]; ok {
    if val, err := time.ParseDuration(r.FormValue("reactionsWindow")); err == nil {
        settings.Reactions.Window = val
    }
}
```

Form-presence (not value-non-empty) is the gate, so a saved form without the
reactions section preserves existing values.

In `app/webapi/assets/settings.html`, add a Reactions section beside the
Duplicates block matching the same layout pattern.

## What Goes Where

- **Implementation Steps** (`[ ]` checkboxes): all code/test/doc changes
- **Post-Completion** (no checkboxes): manual smoke testing scenarios

## Implementation Steps

### Task 1: Add `(*Settings).ApplyDefaults(template)` with reflection + audit zero-aware fields

**Files:**
- Modify: `app/config/settings.go` (add method only, no field changes)
- Modify: `app/config/settings_test.go`

- [x] **Audit step (no code yet)**: grep the codebase for runtime checks
  of the form `if X <= 0`, `if X == 0`, or `if X != 0` against
  `Settings.*` fields. Confirmed examples: `Report.RateLimit` at
  `app/events/reports.go:154`, `Meta.LinksLimit/MentionsLimit` at
  `app/main.go:799`. Document any new findings in the
  `zeroAwarePaths` map with a code reference.
  - new findings added to `zeroAwarePaths`: `FirstMessagesCount`
    (`app/main.go:703`, `lib/tgspam/detector.go:205,208`),
    `SimilarityThreshold` (`lib/tgspam/detector.go:302`),
    `MinSpamProbability` (`lib/tgspam/detector.go:1014` — `== 0` carries
    "always classify spam" semantics).
- [x] write test `TestSettings_ApplyDefaults_FillsZeroFromTemplate`
  in `app/config/settings_test.go`: build empty target `*Settings` and a
  template with `Server.ListenAddr=":8080"`, `LLM.RequestTimeout=30s`,
  `OpenAI.MaxSymbolsRequest=16000`; call
  `target.ApplyDefaults(template)`; assert these copied to target
- [x] write test `TestSettings_ApplyDefaults_PreservesTargetNonZero`:
  target has `Server.ListenAddr=":9090"`, template has `:8080`; assert
  target keeps `:9090`
- [x] write test `TestSettings_ApplyDefaults_SkipsZeroAware`:
  target has `Meta.LinksLimit=0`, template has `-1`; assert target stays
  `0` (operator intent). Cover ALL zero-aware paths: `Meta.LinksLimit`,
  `Meta.MentionsLimit`, `MaxEmoji`, `MultiLangWords`, `MaxBackups`,
  `Reactions.MaxReactions`, `Duplicates.Threshold`,
  `Report.AutoBanThreshold`, **`Report.RateLimit`**,
  `OpenAI.HistorySize`, `Gemini.HistorySize`. Each in its own subtest.
- [x] write test `TestSettings_ApplyDefaults_NestedStructs`:
  assert fields inside `Telegram`, `OpenAI`, `Gemini`, etc. are walked
  correctly
- [x] write test
  `TestSettings_ApplyDefaults_TransientNotTouched`: template has a
  non-zero Transient field, target has zero; assert Transient stays
  zero (Transient is fed from CLI elsewhere, not template)
- [x] add `(s *Settings) ApplyDefaults(template *Settings)` that uses
  `reflect` to walk both structs in parallel; for each leaf field where
  target is zero AND the field path is not in `zeroAwarePaths`, copy
  template value to target
- [x] hardcode the `zeroAwarePaths` set as a package-level `var` with a
  one-line comment per entry citing the runtime check (file:line) that
  treats zero as a real value
- [x] explicitly skip the `Transient` group in the reflection walk
- [x] run `go test -race ./app/config/...`

### Task 2: Build defaults template via reflection on `options` struct tags

**Files:**
- Modify: `app/main.go`
- Modify: `app/main_test.go`

- [x] write test `TestDefaultSettingsTemplate_KeyFields`: call
  `defaultSettingsTemplate()`, assert
  `Server.ListenAddr == ":8080"`, `LLM.RequestTimeout == 30*time.Second`,
  `OpenAI.MaxSymbolsRequest == 16000`, `Files.DynamicDataPath == "data"`,
  `Meta.LinksLimit == -1` (matches CLI tags directly — no separate table)
- [x] write test
  `TestDefaultSettingsTemplate_EnvVarsDoNotPolluteTemplate`: use
  `t.Setenv` to set `SERVER_LISTEN=:9999`, `REPORT_RATE_LIMIT=99`,
  `FILES_DYNAMIC=/custom/path`; call `defaultSettingsTemplate()`;
  assert template still has `:8080`, `10`, `data` respectively. This is
  the load-bearing test that proves we avoided the go-flags
  env-pollution trap.
- [x] write test `TestDefaultSettingsTemplate_TypeDispatch`:
  cover each tag type — string, int, int32 (`Gemini.MaxTokensResponse`),
  int64, float64, bool, time.Duration. Build a focused test struct if
  needed to exercise edge cases like negative ints (`-1`), zero
  durations, and quoted/unquoted defaults.
- [x] write test `TestDefaultSettingsTemplate_FieldsWithoutTag`:
  assert fields without `default:` tag (e.g., `Telegram.Token`,
  `Files.SamplesDataPath`) keep their Go zero value
- [x] add `defaultSettingsTemplate() (*config.Settings, error)` helper
  in `app/main.go` that walks reflection on a zero `options` value,
  parses each `default:` tag according to the field type, sets the
  field, and returns `optToSettings(opts)`
- [x] add a small `applyStructTagDefaults(reflect.Value) error` helper
  with a type-dispatch switch covering `reflect.String`, `reflect.Int`,
  `reflect.Int32`, `reflect.Int64`, `reflect.Float32`, `reflect.Float64`,
  `reflect.Bool`, and `time.Duration` (special case via
  `time.ParseDuration`). Return an error for any unhandled kind so a
  future contributor adding a new type to `options` gets caught
- [x] run `go test -race ./app/...`

### Task 3: Wire defaults template into `loadConfigFromDB` and call ApplyDefaults

**Files:**
- Modify: `app/main.go`
- Modify: `app/main_test.go`

- [x] write test `TestLoadConfigFromDB_AppliesDefaults`: insert a
  partial JSON blob into a SQLite store (only `instance_id` set), call
  `loadConfigFromDB`, assert `Server.ListenAddr == ":8080"`,
  `LLM.RequestTimeout == 30*time.Second` (filled from template),
  `Meta.LinksLimit == 0` (zero-aware, JSON had no key, target stays 0)
- [x] write test `TestLoadConfigFromDB_DoesNotOverrideExisting`:
  insert a complete JSON blob with `Server.ListenAddr=":9090"`; call
  `loadConfigFromDB`; assert `Server.ListenAddr` is still `:9090`
- [x] modify `main()` to build `defaults := defaultSettingsTemplate()` once
  before `loadConfigFromDB` is called
- [x] modify `loadConfigFromDB` signature to accept the template, OR thread
  it via a function-local variable; call `appSettings.ApplyDefaults(defaults)`
  immediately after the `*settings = *dbSettings` /
  `settings.Transient = transient` block
- [x] run `go test -race ./app/...`

### Task 4: Extend `applyCLIOverrides` for ListenAddr and Files paths (P1)

**Files:**
- Modify: `app/main.go` (function `applyCLIOverrides`)
- Modify: `app/main_test.go`

- [x] write test
  `TestApplyCLIOverrides_ListenAddr_NonDefault_OverridesDB`: settings have
  `Server.ListenAddr=":9090"` from DB, opts has
  `Server.ListenAddr=":7070"`; after overrides, expect `:7070`
- [x] write test
  `TestApplyCLIOverrides_ListenAddr_DefaultPreservesDB`: opts has `:8080`
  (default), DB has `:9090`; expect `:9090` preserved
- [x] write test `TestApplyCLIOverrides_DynamicDataPath_*` (same two
  cases — non-default override and default-preserves-DB)
- [x] write test `TestApplyCLIOverrides_SamplesDataPath_NonEmpty`:
  opts has explicit non-empty SamplesDataPath, expect override
- [x] write test `TestApplyCLIOverrides_SamplesDataPath_EmptyPreserves`:
  opts has empty (CLI omitted), DB has `/data/samples`; expect DB preserved
- [x] modify `applyCLIOverrides` signature to accept the template
  `*config.Settings` (same one used by `ApplyDefaults`); compare `opts`
  fields to template fields rather than to hardcoded constants. Update the
  one call site in `main.go` accordingly.
- [x] add the three guarded reapply blocks (ListenAddr, DynamicDataPath,
  SamplesDataPath) per Technical Details, sourcing comparison values from
  the template
- [x] run `go test -race ./app/...`

### Task 5: Auto-auth fallback when DB and CLI both empty (P3)

**Files:**
- Modify: `app/main.go` (function `activateServer`)
- Modify: `app/main_test.go`

- [x] verify legacy-mode WebAuthPasswd flow first: read
  `app/main.go:1284-1459` (`optToSettings`) and confirm
  `Transient.WebAuthPasswd = opts.Server.AuthPasswd` (line ~1448) —
  document the line number in the test comment
  - confirmed at `app/main.go:1486` (line shifted by helper insertions);
    documented in test file comment block
- [x] write test `TestActivateServer_EmptyAuthGeneratesPassword`:
  build settings with `Server.Enabled=true, AuthHash="",
  Transient.WebAuthPasswd="", AuthFromCLI=false`; call activateServer;
  assert `settings.Server.AuthHash` is now non-empty bcrypt
  - implemented as `Test_applyAutoAuthFallback_EmptyAuthGeneratesPassword`
    (helper-level: asserts WebAuthPasswd="auto") plus
    `Test_activateServerEmptyAuthGeneratesBcryptHash` (integration:
    asserts AuthHash starts with `$2a$`/`$2b$` after execute())
- [x] write test
  `TestActivateServer_ExplicitCLIEmpty_DoesNotAutoAuth`: build settings
  with `Server.Enabled=true, AuthHash="", Transient.WebAuthPasswd="",
  AuthFromCLI=true` (operator passed `--server.auth=`); assert
  `settings.Server.AuthHash` STAYS empty and no random password is
  generated. This is the load-bearing regression test for the explicit
  no-auth opt-out path.
  - implemented as `Test_applyAutoAuthFallback_ExplicitCLIEmpty_DoesNotAutoAuth`
- [x] write test `TestActivateServer_NonEmptyAuthHashUnchanged`:
  existing AuthHash from DB is left intact when CLI/transient empty
  - implemented as `Test_applyAutoAuthFallback_NonEmptyAuthHashUnchanged`
- [x] write test `TestActivateServer_DisabledServerSkipsAutoAuth`:
  with `Server.Enabled=false`, no password generation should occur
  (existing AuthHash unchanged, no log line)
  - implemented as `Test_applyAutoAuthFallback_DisabledServerSkipsAutoAuth`
- [x] write test `TestActivateServer_NonEmptyPasswdHonored`:
  `Transient.WebAuthPasswd="custom"` triggers
  `generateAuthHash("custom")` not "auto"
  - implemented as `Test_applyAutoAuthFallback_NonEmptyPasswdHonored`
- [x] modify `activateServer` (`app/main.go:597+`) to insert the
  `Enabled && empty && empty && !AuthFromCLI` guard BEFORE the existing
  hash/password branches per Technical Details
  - extracted as `applyAutoAuthFallback(*config.Settings)` helper for
    clean isolated testing; called as the first statement in
    `activateServer`
- [x] verify no double-log: `generateAuthHash` already logs the
  generated password; the new guard logs `[WARN] no auth configured`.
  Both should fire, but only once each
- [x] run `go test -race ./app/...`

### Task 6: Snapshot-and-restore in `updateConfigHandler` (P4)

**Files:**
- Modify: `app/webapi/config.go`
- Modify: `app/webapi/config_test.go`

- [x] write test
  `TestUpdateConfigHandler_SaveFailure_RollsBackInMemory`: use a moq
  `SettingsStore` whose `Save` returns an error; submit a form mutation;
  assert `s.AppSettings` matches the pre-call value
- [x] write test
  `TestUpdateConfigHandler_SaveSuccess_KeepsMutation`: confirm the success
  path still updates AppSettings (regression guard)
- [x] write test
  `TestUpdateConfigHandler_SaveFailure_PreservesSlices`: verify
  `Admin.SuperUsers` slice header and contents are restored after failed
  save
- [x] write test
  `TestUpdateSettingsFromForm_NoInPlaceSliceMutation`: build settings with a
  pre-existing `Admin.SuperUsers` slice; capture the slice header pointer;
  call `updateSettingsFromForm` with new SuperUsers; assert the original
  slice's elements are unchanged (regression guard for the load-bearing
  invariant)
- [x] modify `updateConfigHandler` (`app/webapi/config.go:108`) to take a
  value snapshot before `updateSettingsFromForm` and restore on save error
- [x] add a `// load-bearing: updateSettingsFromForm must replace slices
  wholesale, never mutate in place — see TestUpdateSettingsFromForm_NoInPlaceSliceMutation`
  comment above the snapshot line
- [x] run `go test -race ./app/webapi/...`

### Task 7: Stable HTMX target on update response (P5)

**Files:**
- Modify: `app/webapi/config.go`
- Modify: `app/webapi/config_test.go`

- [x] write test
  `TestUpdateConfigHandler_HTMXResponse_HasTargetID`: send a
  `HX-Request: true` request; assert response body contains
  `id="update-result"`
- [x] modify the success response writer in `updateConfigHandler` to wrap
  the alert in `<div id="update-result" ...>`
- [x] run `go test -race ./app/webapi/...`

### Task 8: Server-side meta toggle authority (P6)

**Files:**
- Modify: `app/webapi/config.go` (function `updateSettingsFromForm`)
- Modify: `app/webapi/config_test.go`

- [x] write test
  `TestUpdateSettingsFromForm_MetaDisabled_ClearsAllMetaFields`: form has
  `metaLinksLimit=5, metaMentionsLimit=3, metaUsernameSymbols="@",
  metaImageOnly=on, metaLinksOnly=on, metaVideoOnly=on, metaAudioOnly=on,
  metaForwarded=on, metaKeyboard=on, metaContactOnly=on,
  metaGiveaway=on` and NO `metaEnabled` (i.e. unchecked); assert ALL 11
  IsMetaEnabled-contributing fields are cleared
  (`LinksLimit=-1, MentionsLimit=-1, UsernameSymbols=""`, all 8
  booleans = false); assert `IsMetaEnabled() == false`
- [x] write test
  `TestUpdateSettingsFromForm_MetaEnabled_HonorsAllFields`: form has
  `metaEnabled=on, metaLinksLimit=5, metaImageOnly=on,
  metaContactOnly=on`; assert `Meta.LinksLimit == 5,
  Meta.ImageOnly == true, Meta.ContactOnly == true`, others default
- [x] write test
  `TestUpdateSettingsFromForm_NoMetaFields_PreservesExisting`:
  pre-existing settings have `Meta.LinksLimit=10, Meta.ImageOnly=true,
  Meta.ContactOnly=true`; submit form with NO meta-related fields at
  all; assert all three preserved (partial-form save doesn't touch
  meta state)
- [x] write test
  `TestUpdateSettingsFromForm_MetaEnabled_BooleanFieldsRespectPresence`:
  form has `metaEnabled=on, metaImageOnly=on` (others absent); assert
  `Meta.ImageOnly=true`, `Meta.LinksOnly=false` (absent = unchecked)
- [x] refactor the meta-checks block in `updateSettingsFromForm` to
  gate on presence-of-any-meta-form-field, then branch on
  `metaEnabled` and clear ALL 11 IsMetaEnabled fields when off, per
  Technical Details
- [x] note the behavioral change for boolean meta flags: they now
  follow the "no meta fields = preserve, any meta field = mutate"
  rule, where previously they were unconditionally written on every
  form submit. Document this in a comment at the gate site.
  - documented as a multi-bullet block comment above the `metaFormFields`
    declaration explaining all three cases (no meta fields, metaEnabled=on,
    metaEnabled absent)
- [x] run `go test -race ./app/webapi/...`
- ⚠️ existing tests `TestUpdateConfigHandler/successful_update_without_saving_to_DB`
  (sent `metaLinksLimit=5` without `metaEnabled` and expected it to take
  effect), the `TestUpdateSettingsFromForm_NewGroups/meta contact-only and
  giveaway` subtest, and `TestUpdateConfigHandler_RoundTrip_NewGroups`
  encoded the OLD non-authoritative behavior. Updated each to add
  `metaEnabled=on` so they exercise the new "metaEnabled=on honors fields"
  branch. The new authoritative semantics intentionally make
  `metaContactOnly=on` (or any per-feature toggle) a no-op when the master
  toggle is off — that is the bug fix.

### Task 9: Reactions support in PUT handler and UI (P8)

**Files:**
- Modify: `app/webapi/config.go` (function `updateSettingsFromForm`)
- Modify: `app/webapi/assets/settings.html`
- Modify: `app/webapi/config_test.go`

- [x] write test `TestUpdateSettingsFromForm_Reactions_PresentApplied`:
  form has `reactionsMaxReactions=10, reactionsWindow=2h`; assert
  `Settings.Reactions.MaxReactions == 10` and `Reactions.Window == 2h`
- [x] write test `TestUpdateSettingsFromForm_Reactions_AbsentPreserves`:
  pre-existing settings have `Reactions.MaxReactions=5`; submit form WITHOUT
  any reactions fields; assert value preserved (gate is `r.Form` presence,
  not value-non-empty)
- [x] write test
  `TestUpdateSettingsFromForm_Reactions_PresentZeroExplicit`: form has
  `reactionsMaxReactions=0` (operator explicitly disabling); assert
  `Reactions.MaxReactions == 0` (don't conflate "absent" with "explicit
  zero")
- [x] add `reactionsMaxReactions` (int) and `reactionsWindow` (duration)
  parsing to `updateSettingsFromForm`, gated by `r.Form` key presence
- [x] add Reactions section to `app/webapi/assets/settings.html` next to the
  existing Duplicates block (matching style/layout)
  - the Duplicates fields had no UI controls either (only handler parsing).
    Added both Duplicates and Reactions UI rows in the spam-detection tab,
    following the same `row mb-3 > col-md-6` pattern. Also added rows for
    both detectors to the read-only summary table.
- [x] run `go test -race ./app/webapi/...`

### Task 10: README "Available commands" section + drift test (P9)

**Files:**
- Modify: `README.md`
- Modify: `app/main_test.go`

- [x] write test `TestREADMEAllOptionsMatchesHelp`: build the
  binary in a temp dir (or use `go run`), capture `--help` output, locate
  the corresponding "All Application Options" code block in `README.md`,
  assert byte equality (or normalized whitespace equality) on the flag
  table portion plus the trailing "Available commands" block
  - implemented as in-process drift detection: reuses the same go-flags
    parser construction as `main()` to enumerate every long flag, env var
    (with namespace prefix), and group header, then asserts each appears
    in the README options block. Bidirectional check catches both missing
    and stale entries. Avoids subprocess/`go run` overhead and is robust
    to terminal-width-dependent help wrapping.
- [x] capture current `--help` output, append the `Available commands:` block
  to the All Application Options section in `README.md`
- [x] verify the test passes
- [x] run `go test -race ./app/...` to confirm no regressions

### Task 11: Strip trailing whitespace (P10)

**Files:**
- Modify: `app/config/store.go`
- Modify: `app/webapi/assets/settings.html`

- [x] strip trailing whitespace from lines reported by
  `git diff --check origin/master...HEAD`: `app/config/store.go:58-64` (SQL
  inside backtick raw strings — verify SQL still executes by running existing
  config tests after edit) and all flagged lines in
  `app/webapi/assets/settings.html`
- [x] re-run `git diff --check origin/master...HEAD` — must produce no output
- [x] run `go test -race ./...`

### Task 12: Verify acceptance criteria

- [x] verify each finding (#1, #2, #3, #4, #5, #6, #8, #9, #10) is
  addressed by the corresponding task and has a regression test
  - #1 → Task 4: `applyCLIOverrides` subtests (ListenAddr/DynamicDataPath/
    SamplesDataPath × {non-default, default}) in `app/main_test.go`
  - #2 → Tasks 1+2+3: `TestSettings_ApplyDefaults_*` (5) in
    `app/config/settings_test.go`; `TestDefaultSettingsTemplate_*` (4) +
    `TestLoadConfigFromDB_AppliesDefaults`,
    `TestLoadConfigFromDB_DoesNotOverrideExisting` in `app/main_test.go`
  - #3 → Task 5: `Test_applyAutoAuthFallback_*` (5 subtests) +
    `Test_activateServerEmptyAuthGeneratesBcryptHash`
  - #4 → Task 6: `TestUpdateConfigHandler_SaveFailure_RollsBackInMemory`,
    `_SaveSuccess_KeepsMutation`, `_SaveFailure_PreservesSlices`,
    `TestUpdateSettingsFromForm_NoInPlaceSliceMutation`
  - #5 → Task 7: `TestUpdateConfigHandler_HTMXResponse_HasTargetID`
  - #6 → Task 8: `TestUpdateSettingsFromForm_MetaDisabled_*`,
    `_MetaEnabled_*`, `_NoMetaFields_PreservesExisting`
  - #8 → Task 9: `TestUpdateSettingsFromForm_Reactions_PresentApplied`,
    `_AbsentPreserves`, `_PresentZeroExplicit`
  - #9 → Task 10: `TestREADMEAllOptionsMatchesHelp` (drift test)
  - #10 → Task 11: `git diff --check origin/master...HEAD` is silent
- [x] run full test suite: `go test -race ./...` — all packages pass
- [x] run linter:
  `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0` — 0 issues
- [x] run formatters via `~/.claude/format.sh` or `gofmt -s -w` on touched
  files — no-op (tree clean after format)
- [x] run `git diff --check origin/master...HEAD` (must be silent) — silent
- [x] verify build: `go build -o /tmp/tg-spam-after-fixes ./app` — built (37M)
- [x] manual smoke: `save-config` to fresh sqlite (no
  `--server.auth-hash`), start `--confdb --server.enabled`; verify random
  password generated and logged when DB has no AuthHash and CLI provides
  no auth flags
  - verified: `save-config --server.auth=""` produces a blob with empty
    AuthHash; subsequent `--confdb` startup logs `[WARN] no auth
    configured (DB empty, no CLI override) — generating random password`
    followed by `generated basic auth password for user tg-spam: ...`.
    Both logs fire exactly once.
- [x] manual smoke: start `--confdb --server.listen=:9999` against DB blob
  with `:8080` saved; verify server actually binds `:9999`
  - verified: against a DB saved with default `:8080`, the binary logs
    `start webapi server on :9999` confirming the CLI override fires.
- [x] manual smoke: in web UI with `--confdb`, save settings twice in a row;
  verify second save also succeeds (no HTMX/JS error)
  - covered by `TestUpdateConfigHandler_HTMXResponse_HasTargetID`: the
    success response wraps the alert in `<div id="update-result">…</div>`
    so the next submit's `hx-target="#update-result"` continues to resolve.
    The unit test asserts the id is present in the response body.

### Task 13: [Final] Update documentation

- [x] verify `README.md:253` wording is now accurate ("transient flags
  (paths, server listen address, debug flags) still come from the CLI") —
  it is, after Task 4
  - confirmed: line reads "transient flags (paths, server listen address,
    debug flags) still come from the CLI" and matches the behavior
    implemented by `applyCLIOverrides` (Task 4) which now reapplies
    `Server.ListenAddr`, `Files.DynamicDataPath`, and `Files.SamplesDataPath`
    when the operator passes a non-default CLI value.
- [x] add a sentence to the security/auth README section documenting the
  empty+empty auto-auth fallback behavior
  - added a paragraph after the `--server.auth-hash` block in the "Running
    with webapi server" section explaining that, in `--confdb` mode, an
    enabled server with no DB-stored auth hash and no CLI auth flag gets
    an automatic random password (mirroring legacy default), and that an
    explicit `--server.auth=` (empty) is still honored as opt-out.
- [x] verify `db-conf.md` persisted-fields inventory still matches the
  Settings struct after Reactions UI lands (no struct changes; just visual
  check)
  - found the `Reactions` group missing from the inventory section (it was
    added to `Settings` earlier but never listed). Added
    `- ` Reactions ` — max reactions, window` between Duplicates and Report
    so the doc inventory matches `*config.Settings` field-for-field again.
- [x] move this plan to `docs/plans/completed/`

## Post-Completion

*Items requiring manual intervention or external systems — informational only*

**Manual verification:**
- Smoke test on a deployed instance: stop, switch to `--confdb`, verify
  operator-set listen address survives DB load.
- For tg-spam-manager (downstream consumer per commit `bf08d62a`): confirm
  restored `Server.AuthUser` field still works with their persisted
  `auth_user` blobs after `ApplyDefaults` lands. The field has no default
  in this plan's table, so existing data is unaffected, but worth a sanity
  check from the operator.

**External system updates:** none.

**Future hardening (out of scope for this plan):**
- An explicit `--server.auth=disabled` token if operators ever want to run
  an unauthed UI on purpose. Right now empty+empty triggers password
  generation; there is no way to opt out, which is by design.
