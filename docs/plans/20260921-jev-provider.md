# jev as an optional LLM provider

## Overview

Add jev (typesafe.ai decision model) as a third optional provider in tg-spam's LLM check path,
alongside OpenAI and Gemini. jev is not a text generator: one HTTP POST carries a `state` plus typed
questions, and each answer returns as a number. We send one `noul` question ("is this message
spam?") and get a bare probability 0..1, which the code thresholds.

Problem it solves: the LLM call is the slowest and most expensive step in the pipeline. On radio-t
production traffic jev produced comparable observed results with no reliable quality difference
established, at a median 307ms and an input rate of $0.042/Mtok with output free. That gives an
operator who wants the LLM stage cheap and fast an option, without losing the existing one. No
production cost ratio is claimed here: the earlier $0.042-against-$0.15 figure was jev against
gpt-4o-mini's input rate, while radio-t runs gpt-5-mini, and total spend also depends on token
volume and the reasoning-effort setting.

Integration: jev becomes a third entry in the existing `llmChecks` slice in `Detector.Check`. It
inherits veto mode, `any`/`all` consensus, history sizing and the first-message gate unchanged. No
change to `llm.go` or to the `llmResponse` provider contract.

## Measurement behind the design

Evaluated on 150 production messages from the radio-t container log — every message in the retained
window where OpenAI actually ran, so both a flagged and a cleared population. Input reconstructed
from the raw Telegram JSON identically for both outcomes (text-or-caption plus `"\n"` plus quote,
Quote taking precedence over ReplyTo), with admin `/spam` diagnostics excluded structurally by
requiring an intake since the previous decision. Labeled by content, blind to scores and verdicts:
86 spam, 62 ham, 2 excluded as insufficient-context.

The split was 85/62/3 until the maintainer stated the moderation rule that a post naming a `*_bot`
handle as a recommendation is spam whether or not someone asked. Applying that rule uniformly moved
one row (`"аудио из рф у меня нормально качается через vpnconcord_bot"`, jev 0.20) from ambiguous to
spam. That is a stated-policy decision, recorded here so the count change is traceable, not a
relabel to suit a score — the row is a miss for both checkers at 0.30, so it cannot flatter jev.

| checker | caught | recall | ham flagged |
|---|---|---|---|
| OpenAI gpt-5-mini, production prompt, history size 10 | 79/86 | 91.9% | 0/62 |
| jev @0.25 | 82/86 | 95.3% | 0/62 |
| jev @0.30 | 80/86 | 93.0% | 0/62 |
| jev @0.35 | 79/86 | 91.9% | 0/62 |

Median jev latency 307ms; highest-scoring labeled ham 0.24. Price $0.042/Mtok input, output free.

**Known discrepancy between the evaluated input and what the shipped code will send.** The
evaluation fed jev `authored + "\n" + quote`. Production will not: `Detector.cleanText`
(`detector.go:1490`) skips every `unicode.Cc` rune, and LF is one, with no replacement character —
verified by running it, `"Спасибо!\nЗАКРЫТЫЙ КАНАЛ"` becomes `"Спасибо!ЗАКРЫТЫЙ КАНАЛ"`. So the
post and the quoted text arrive fused with no boundary. This is pre-existing behavior that OpenAI
and Gemini already live with, not something jev introduces, and it is out of scope here because
changing it alters what every provider sees. It matters twice for this plan: the frozen
configuration below must record the separator actually used, and the holdout must send the fused
form to be faithful to production.

**These are development-set numbers.** The honest reading, agreed with the codex review: comparable
observed results, no reliable quality difference established — 80/86 against 79/86 is one message.
The thresholds were chosen after seeing these scores, so 0.30 ships as a **development candidate,
not a validated default**, and the 0.06 distance to the highest labeled ham is not a safety margin.
A holdout on fresh intake, with acceptance criteria frozen before scores are seen, is required
before the default is called validated. That holdout is Post-Completion — it needs new traffic, not
code.

Deferred, not measured on this set: multiple narrow questions combined with a max. The evidence
against it came from an earlier, superseded corpus whose extraction was context-mismatched — at a
matched false-positive budget the composition caught fewer spam than the single question, and a
legitimate bare `t.me` link scored 0.90 on "does this redirect you elsewhere" while the blunt spam
question correctly said 0.49. That is suggestive of the failure mode but it is NOT a measured result
on these 150 rows. The standing position is no demonstrated benefit, so it is deferred; it is not a
settled finding.

## Context (from discovery)

- `lib/tgspam/llm.go` — `runLLMProviderCheck` holds the shared retry, history flattening and
  `Details` formatting. Provider contract is `llmResponse{IsSpam bool, Reason string, Confidence int}`.
- `lib/tgspam/openai.go`, `lib/tgspam/gemini.go` — the template: a `Config` struct, a `check()` that
  delegates to `runLLMProviderCheck`, and a `sendRequest()` doing transport.
- `lib/tgspam/detector.go:185` — `HTTPClient` interface, already moq-generated at
  `lib/tgspam/mocks/http_client.go`, already used by the CAS check.
- `lib/tgspam/detector.go:343-352` — short-message eligibility computes per-provider booleans
  **outside** the `llmChecks` slice; a third provider must be added here as well as in the slice.
- `lib/tgspam/detector.go:395-416` — the `llmChecks` slice.
- `app/config/settings.go` — `GeminiSettings` is the exact shape to copy, including db tags.
- `app/config/crypt.go:155` — `sensitiveFieldAccessors` enumerates secrets by name.
- `app/main.go:331` — log masking is one `if` per token.
- `app/webapi/config.go:396-430` — Gemini form parsing; `strconv.ParseFloat` precedent at :549.
- `app/webapi/assets/settings.html:468-512` — Gemini edit panel and read-only display rows.

Architectural guidance (go-architect): reuse `HTTPClient` rather than defining a `jevClient`
interface — the existing `openAIClient`/`geminiClient` interfaces exist because the vendor SDK types
are unmockable, not as a convention, and reusing `HTTPClient` lets tests assert the actual JSON body
and URL. Thresholding inside `sendRequest` is the right seam: nothing downstream consumes a score,
`applyLLMConsensus` is boolean and `spamcheck.Response` has no numeric field. The two-line
duplication at `detector.go:343-352` is not worth refactoring for a third provider — backlog it.

## Development Approach

- **testing approach**: Regular (code first, then tests) — matches the existing provider files, whose
  tests assert request shape against a constructed client.
- complete each task fully before moving to the next
- make small, focused changes
- **CRITICAL: every task MUST include new/updated tests** for code changes in that task
  - tests are not optional - they are a required part of the checklist
  - write unit tests for new functions/methods
  - write unit tests for modified functions/methods
  - add new test cases for new code paths
  - update existing test cases if behavior changes
  - tests cover both success and error scenarios
- **CRITICAL: all tests must pass before starting next task** - no exceptions
- **CRITICAL: update this plan file when scope changes during implementation**
- run tests after each change
- maintain backward compatibility: jev disabled unless a token is set, no behavior change otherwise

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
   - Grep new function signatures: `grep -nE '^func.*\(.*,.*,.*,.*\)' <dir>/*.go` — any hit with 4+ comma-separated params (excluding `ctx`) is a violation. Same for the return-value side. Use the directory the task actually touched: `lib/tgspam/` for Tasks 1-2, `app/` for Tasks 3-5.
   - Check new exported STRUCT FIELDS and TYPES too, not only funcs: `Config.JevVeto`, `Config.JevHistorySize`, every `JevConfig` and `JevSettings` field, and `Settings.Jev`. Each needs a godoc line and an out-of-package caller, or it gets lowercased.
   - For every new standalone helper, `grep -rn 'helperName(' --include='*.go'` and confirm at least one caller is NOT a method of a single type. If all callers are methods of one type, convert.
   - For every new exported identifier, grep cross-package. If no out-of-package hit, lowercase it —
     EXCEPT where this plan names the planned consumer explicitly: `JevConfig` and `WithJevChecker`
     are exported in Task 1-2 for the `app/main.go` caller added in Task 4, so do not lowercase them
     mid-plan for lacking a caller that a later task adds.
5. Only after 1–4 pass: mark the task complete.

If a previous task shipped a violation (spotted later by user, reviewer, or yourself): fix it in the next commit BEFORE starting the next task. Do not let violations accumulate.

## Testing Strategy

- **unit tests**: required for every task (see Development Approach above)
- **e2e tests**: the project has Playwright e2e at `e2e-ui/e2e_test.go` with a settings round-trip
  test. The new settings form fields must be added to it in the same task as the UI change, and
  treated with the same rigor as unit tests.
- provider tests follow `openai_test.go`/`gemini_test.go`: construct the checker with a mocked
  `HTTPClient`, assert the outgoing JSON body and URL, and assert the mapped `llmResponse`.

## Progress Tracking

- mark completed items with `[x]` immediately when done
- add newly discovered tasks with ➕ prefix
- document issues/blockers with ⚠️ prefix
- update plan if implementation deviates from original scope
- keep plan in sync with actual work done

## Solution Overview

`jevChecker` mirrors `openAIChecker` and `geminiChecker`: it holds a client and a config, exposes
`check()` which delegates to the shared `runLLMProviderCheck`, and does transport in `sendRequest()`.
The probability-to-verdict mapping happens inside `sendRequest`, so `llm.go` and the `llmResponse`
contract are untouched and jev inherits the shared retry loop, history flattening and `Details`
formatting unchanged — including the fact that the retry is not status-selective.

Key decisions:

- **One noul question, not several.** Multi-question composition is deferred for want of any
  demonstrated benefit, not rejected on a measurement of this set (see the evidence section).
- **Flat combined message, not structured state.** jev accepts a structured `state`, but it produced
  comparable results on the bare combined text with no history, so widening the provider interface
  now buys something the evidence does not ask for. The message reaching jev is whatever
  `runLLMProviderCheck` produces, which already includes the quote concatenation and, when
  `HistorySize > 0`, the flattened history.
- **Reuse the `HTTPClient` interface, not the CAS-tuned instance.** No new interface and no new mock,
  and tests assert the real request bytes; but jev gets its own client, because
  `Config.HTTPClient` is built with `settings.CAS.Timeout` (`app/main.go:763`).
- **Pin the model version.** `jev-1.13.0`, not `jev-latest`: an alias moves and silently changes the
  numbers a tuned threshold depends on. The resolved `model` from the response is logged.
- **A missing or malformed answer is an error, never a zero.** Treating an absent probability as 0.0
  would turn a failed call into a confident "not spam".
- **Confidence is direction-aware.** `p*100` for a spam verdict, `(1-p)*100` for a ham verdict —
  p=0.02 is strong evidence against spam, not 2% confidence in ham.

## Technical Details

Endpoint: `POST https://api.typesafe.ai/v1/systemone`, `Authorization: Bearer <token>`,
`Content-Type: application/json`.

Request body:

```json
{
  "model": "jev-1.13.0",
  "state": {"message": "<combined message text>"},
  "questions": {
    "spam": {
      "type": "noul",
      "instructions": "<JevConfig.Question>",
      "criteria": {"true": "<CriteriaSpam>", "false": "<CriteriaHam>"}
    }
  }
}
```

Response body (only the fields we read):

```json
{"model": "jev-1.13.0", "answers": {"spam": {"type": "noul", "noul": 0.87}}}
```

Mapping to the existing contract:

- `IsSpam` = `noul >= Threshold`
- `Confidence` = `round(noul*100)` when spam, `round((1-noul)*100)` when ham
- `Reason` = `"spam probability 0.87, threshold 0.30"` — `runLLMProviderCheck` appends
  `", confidence: N%"`, so the reason must not repeat it

Error cases that must return an error rather than a verdict: non-2xx status (surface status and raw
`detail`), a missing or null probability, wrong answer `type`, `noul` outside 0..1 or non-finite.

**Retry behavior is inherited, not status-selective.** `runLLMProviderCheck` (`llm.go:28-35`) breaks
out of its loop on `err == nil` and cannot see an HTTP status, and its `RetryCount` is *total
attempts*, not retries after the first. So jev inherits exactly what OpenAI and Gemini get: every
error is retried until attempts are exhausted or the context ends. Status-selective retry is not a
requirement here and is explicitly NOT implemented — no nested retry loop inside `sendRequest`, and
a permanent error is never swallowed as ham. Default `RetryCount` is 1, meaning one attempt.

**A missing or null probability must not decode as zero.** `{"answers":{"spam":{"type":"noul"}}}`
and the same with `"noul": null` both leave a plain `float64` field at 0.0, which is in range and
becomes a confident ham verdict — and in veto mode that clears heuristic spam. The field must be
presence-aware (`*float64`) and a missing or null value must be an error, tested separately from a
legitimate `noul: 0`.

**Numeric config must be finite and bounded.** `strconv.ParseFloat` accepts `NaN`, and a
`t <= 0 || t > 1` guard does not reject it; every `p >= NaN` is false, so a NaN threshold forces ham
on everything. Validation must require a finite `Threshold` in (0, 1] on both the CLI and the web
form path. `MaxSymbolsRequest` is rune-sliced, and Gemini's truncation shape (`gemini.go:87-93`)
panics on a negative value — `len(msg) > -1` is true and `runes[:-1]` panics — so a negative value
must be rejected and zero must mean the default.

Config defaults: `Model` `jev-1.13.0`, `Threshold` 0.30, `MaxSymbolsRequest` 6000, `RetryCount` 1
(one attempt), `HistorySize` 0. The cap is 6000 runes, not Gemini's 8192, so the shipped defaults
instantiate the frozen candidate rather than contradicting it.

**Question and criteria defaults must be `default:` struct tags, not constructor fallbacks.**
`defaultSettingsTemplate` (`app/settings.go:214`) fills from struct tags by reflection and then runs
`optToSettings`, so a default applied only inside `newJevChecker` never reaches `Settings.Validate()`
and a token-only startup would fail its own empty-`Question` check. The exact text is recorded in
the appendix below and must be copied verbatim into the flag tags.

**`--jev.apibase` must carry no `default:` tag.** The endpoint URL fallback
(`https://api.typesafe.ai/v1/systemone`) is applied inside `newJevChecker` when `params.APIBase` is
empty, the same way the other defaults are. `IsOpenAIEnabled` (`settings.go:316`) is
`APIBase != "" || Token != ""`, which is only safe because `--openai.apibase` (`main.go:106`) has no
`default:` tag. A flag default on jev's apibase would make the field never empty, so an
`IsOpenAIEnabled`-shaped predicate would report jev enabled on every startup.

**One predicate, used everywhere.** `IsJevEnabled()` returns `s.Jev.Token != ""` — the Gemini
precedent (`main.go:844`), not the OpenAI one. Startup wiring and `Settings.Validate()` both gate on
it, so a jev pointed at a proxy apibase with no token cannot end up enabled with an unvalidated
`Threshold` (a stored 0 would flag every message).

Startup validation in `Settings.Validate()`, when `IsJevEnabled()`: reject `Threshold` outside
(0, 1] and reject an empty `Question`.

## What Goes Where

- **Implementation Steps** (`[ ]` checkboxes): tasks achievable within this codebase
- **Post-Completion** (no checkboxes): the holdout, which needs fresh production traffic

## Implementation Steps

### Task 1: Add the jev checker and its config

**Files:**
- Create: `lib/tgspam/jev.go`
- Create: `lib/tgspam/jev_test.go`

**Design Contract:**

Type:
- `jevChecker` (unexported — constructed only inside `lib/tgspam` by `Detector.WithJevChecker`)
- `JevConfig` (exported — `app/main.go` constructs it, same as `OpenAIConfig`/`GeminiConfig`)
- `jevRequest`, `jevResponse` (unexported — wire types, marshalled only inside this file)

`JevConfig` fields (exact names; `optToSettings` in Task 3 and `makeDetector` in Task 4 both depend
on them, and each needs a godoc line):

```go
type JevConfig struct {
    Token              string  // bearer credential
    APIBase            string  // empty falls back to the default endpoint
    Model              string  // pinned version, never an alias
    Question           string  // noul instructions
    CriteriaSpam       string  // criteria.true
    CriteriaHam        string  // criteria.false
    Threshold          float64 // noul at or above this is spam
    MaxSymbolsRequest  int
    RetryCount         int
    CheckShortMessages bool
}
```

`CheckShortMessages` takes the Gemini spelling, not OpenAI's `CheckShortMessagesWithOpenAI`
(`openai.go:66` vs `gemini.go:37`); `detector.go:345` and `:409` read both, so Task 2's
`jevChecksShort` must reference this name. `Token` and `APIBase` live on the config even though
`newJevChecker` takes the client separately — the client is transport, the credential and endpoint
are request construction.

Methods (full signatures):
- `(j *jevChecker) check(ctx context.Context, msg string, history []spamcheck.Request) (spam bool, cr spamcheck.Response)`
- `(j *jevChecker) sendRequest(ctx context.Context, msg string) (response llmResponse, err error)`
- `(j *jevChecker) buildRequest(msg string) jevRequest`
- `(j *jevChecker) verdict(p float64) llmResponse`

Standalone helpers planned (justification why NOT a method):
- `newJevChecker(client HTTPClient, params JevConfig) (*jevChecker, error)` — constructor, matches
  `newOpenAIChecker`/`newGeminiChecker` in shape but returns an error, because it validates. There
  is precedent in this file: `WithLuaEngine` (`detector.go:604`) returns `error` and
  `WithUserStorage` (`:681`) returns `(count int, err error)`. The alternatives — panicking,
  returning nil and silently disabling jev, or keeping invalid config — are all worse, and this is
  new API so nothing depends on the old shape.

Exports (justification per item: who outside the package calls this?):
- `JevConfig` — constructed in `app/main.go` `makeDetector` and passed to `WithJevChecker`
- (`WithJevChecker(client HTTPClient, config JevConfig) error` is added in Task 2, on `Detector`,
  and is called from `app/main.go`. It returns the constructor's error and assigns `d.jevChecker`
  only on success, so a `Detector` never holds a half-valid checker.)

- [x] create `lib/tgspam/jev.go` with `JevConfig`, `jevChecker`, `newJevChecker` and the wire types
- [x] implement `buildRequest` (truncate to `MaxSymbolsRequest` by runes, build the one noul question)
- [x] implement `sendRequest`: `http.NewRequestWithContext` so the detector's LLM deadline is
      honored, bearer auth, `defer resp.Body.Close()`, surface status and raw `detail` on non-2xx
- [x] decode the probability into a `*float64` so a missing or null `noul` is distinguishable from a
      legitimate `0` and returns an error
- [x] implement `verdict`: threshold the probability, set direction-aware `Confidence`, format `Reason`
- [x] have `newJevChecker` return `(*jevChecker, error)` and validate before constructing, so direct
      library use cannot bypass the app validator. The full contract, identical to the one
      `Settings.Validate()` enforces in Task 3: `Threshold` finite and in (0, 1]; `MaxSymbolsRequest`
      non-negative, with zero meaning the 6000 default; `Question`, `CriteriaSpam` and `CriteriaHam`
      all non-empty. Supply the intended defaults for zero-valued fields, but never silently replace
      an invalid non-zero input — reject it
- [x] implement `check` delegating to `runLLMProviderCheck` with name `jev`, returning an empty
      response when the client is nil (mirrors `openAIChecker.check`)
- [x] log the resolved `model` from the response at DEBUG so an alias move is visible
- [x] write tests for the happy path: assert outgoing URL, bearer header and exact JSON body
- [x] write tests for the threshold boundary (p just below, at, and above `Threshold`)
- [x] write tests for direction-aware `Confidence` on both a spam and a ham verdict
- [x] write tests for error paths: non-2xx, absent `answers.spam`, wrong `type`, out-of-range `noul`
- [x] write SEPARATE tests for a missing `noul` key, an explicit `"noul": null`, and a legitimate
      `"noul": 0` — the first two must error, the third must be a valid ham verdict
- [x] write tests for attempt counts under the inherited retry contract: a 422 and a 429 both
      consume all `RetryCount` attempts, and a cancelled context stops early
- [x] write a test asserting context cancellation aborts the in-flight request
- [x] write tests rejecting, at construction, each of: a NaN threshold, a threshold of 0, a
      threshold of 1.5, a negative `MaxSymbolsRequest`, an empty `Question`, an empty `CriteriaSpam`
      and an empty `CriteriaHam` — each returning an error rather than a checker
- [x] write a test proving zero-valued `MaxSymbolsRequest` and `Model` get their defaults while an
      invalid non-zero value is rejected rather than normalized
- [x] write a test asserting rune-safe truncation at `MaxSymbolsRequest`
- [x] run tests - must pass before task 2

### Task 2: Wire jev into the detector

**Files:**
- Modify: `lib/tgspam/detector.go`
- Modify: `lib/tgspam/detector_test.go`

- [x] add `jevChecker *jevChecker` field to `Detector`
- [x] add `JevVeto bool` and `JevHistorySize int` to `Config`, next to the OpenAI and Gemini pairs
- [x] add `WithJevChecker(client HTTPClient, config JevConfig) error` alongside `WithOpenAIChecker`,
      returning the constructor's error and assigning `d.jevChecker` only on success
      (`WithLuaEngine` at `detector.go:604` is the error-returning precedent)
- [x] write a test proving a rejected config leaves `d.jevChecker` nil and the detector usable
- [x] add the `jev` entry to the `llmChecks` slice (`detector.go:395-416`)
- [x] bump the `llmResults` preallocation at `detector.go:394` from 2 to 3 — housekeeping, not a
      correctness requirement, since `append` reallocates
- [x] add `jevChecksShort` to the short-message eligibility block (`detector.go:343-352`) and include
      it in the `(!openaiChecksShort && !geminiChecksShort)` condition. **The slice alone is not
      enough, conditionally:** the early return fires only when no provider sets its short-message
      flag, so a slice-only jev misses short messages precisely when neither OpenAI nor Gemini is
      already short-checking, and runs on them when one of them is. That configuration dependence is
      what makes the omission hard to spot
- [x] write tests for jev participating in `any` and `all` consensus
- [x] write tests for jev in veto mode (clears heuristic spam) and non-veto mode (flips ham)
- [x] write a test proving a short message reaches jev when `CheckShortMessages` is set, and does not
      when it is not
- [x] write a test proving a jev error leaves the base decision unchanged (`flip` stays false)
- [x] run tests - must pass before task 3

### Task 3: Add CLI flags and the settings struct

**Files:**
- Modify: `app/main.go`
- Modify: `app/settings.go`
- Modify: `app/config/settings.go`
- Modify: `app/config/settings_test.go`

- [x] add the `jev` flag group to `app/main.go` mirroring the `gemini` group: `token`, `veto`,
      `apibase`, `model` (default `jev-1.13.0`), `question`, `criteria-spam`, `criteria-ham`,
      `threshold` (default `0.30`), `max-symbols-request` (default `6000`), `retry-count`,
      `history-size`, `check-short-messages`, with `namespace:"jev" env-namespace:"JEV"`
- [x] give `question`, `criteria-spam` and `criteria-ham` non-empty `default:` struct tags copied
      verbatim from the appendix — NOT constructor fallbacks: `defaultSettingsTemplate`
      (`app/settings.go:214`) fills by reflection over the tags and then runs `optToSettings`, so a
      constructor-only default never reaches `Validate()` and token-only startup would fail its own
      empty-`Question` check
- [x] add `JevSettings` to `app/config/settings.go` with `json`/`yaml`/`db` tags, copying the
      `GeminiSettings` shape, and add the `Jev` field to `Settings`
- [x] add `IsJevEnabled()` returning `s.Jev.Token != ""` — token only, the Gemini precedent, NOT
      `IsOpenAIEnabled`'s token-or-apibase form; `--jev.apibase` gets no `default:` tag
- [x] map the flags in `optToSettings` (`app/settings.go`) and in the token-override block
- [x] add `Jev.HistorySize` to `zeroAwarePaths` with the same comment form as `Gemini.HistorySize`
- [x] add validation to `Settings.Validate()` gated on `IsJevEnabled()` — the same predicate Task 4
      wires on, and the SAME contract `newJevChecker` enforces, so a saved config can never construct
      successfully at validation time and then fail at startup: finite `Threshold` in (0, 1],
      non-negative `MaxSymbolsRequest`, and non-empty `Question`, `CriteriaSpam` and `CriteriaHam`
- [x] write tests for `optToSettings` mapping every new field
- [x] write tests for `Validate` rejecting a threshold of 0, of 1.5, and an empty question
- [x] write a test proving `Jev.HistorySize` zero survives an `ApplyDefaults` merge
- [x] write a token-only startup test: setting just `--jev.token` passes `Validate()` because the
      question and criteria defaults arrived from the struct tags
- [x] write a test proving a custom `--jev.question` overrides the default rather than merging
- [x] write a legacy-DB test: settings stored before these fields existed get the tag defaults
- [x] write a test rejecting a NaN threshold from the CLI path
- [x] run tests - must pass before task 4


- [x] ➕ add the jev flags to the README "All Application Options" block. Moved here from
      Task 7: `TestREADMEAllOptionsMatchesHelp` (`app/main_test.go:1253`) asserts every long
      flag and env var appears in that block, so adding the flag group breaks it immediately
      and Task 3's own gate cannot pass without it. The block uses an unwrapped single-line
      style at description column 40, not raw `--help` output, which wraps long env names
### Task 4: Construct the checker at startup and protect the credential

**Files:**
- Modify: `app/main.go`
- Modify: `app/config/crypt.go`
- Modify: `app/config/crypt_test.go`
- Modify: `app/main_test.go`

- [x] in `makeDetector`, build `tgspam.JevConfig` from settings and call `detector.WithJevChecker`
      when `settings.IsJevEnabled()`, logging `[WARN] jev enabled` like the others
- [x] treat a `WithJevChecker` error as a startup failure rather than continuing with jev silently
      disabled — an operator who set a token and a bad threshold must be told, not quietly left
      without the provider he configured
- [x] write a test proving a bad jev config fails startup rather than booting with jev off
- [x] write a test proving jev stays disabled when only `--jev.apibase` is set with no token
- [x] set `Config.JevVeto` and `Config.JevHistorySize` from settings alongside the OpenAI pair
- [x] pass jev its OWN `*http.Client`, not `Config.HTTPClient` — that instance is built with
      `settings.CAS.Timeout` (`app/main.go:763`) and would impose a CAS deadline on LLM calls. The
      per-request deadline comes from the detector's LLM context via `NewRequestWithContext`
- [x] add the jev token to the masking list in `app/main.go:331`
- [x] add `FieldJevToken` and its entry in `sensitiveFieldAccessors` (`app/config/crypt.go:155`) so
      the credential is encrypted at rest in `--confdb`. **Documented exemption to the visibility
      rule:** `FieldTelegramToken`, `FieldOpenAIToken`, `FieldGeminiToken` and `FieldServerAuthHash`
      (`crypt.go:21-24`) all have zero callers outside `app/config`, so the rule says lowercase —
      but a lone `fieldJevToken` among four exported siblings in one const block is worse. Keep it
      exported for const-block consistency; lowercasing all four is separate cleanup
- [x] write tests for encrypt/decrypt round-trip of the jev token
- [x] write a test proving the jev token is masked in log output
- [x] run tests - must pass before task 5


- [x] ➕ extract `collectMaskedSecrets` from `main()`. The masking list was built inline, so
      no token had a test and jev's could not get one without a seam. One production caller,
      and the test now covers all four provider tokens plus the auto-password exclusion
### Task 5: Add the settings UI and its e2e coverage

**Files:**
- Modify: `app/webapi/assets/settings.html`
- Modify: `app/webapi/config.go`
- Modify: `app/webapi/config_test.go`
- Modify: `e2e-ui/e2e_test.go`

- [ ] add a `jev` tab, nav link and edit panel to `settings.html` mirroring the Gemini block at
      :468-512, with ids `jevVeto`, `jevCheckShortMessages`, `jevHistorySize`, `jevModel`,
      `jevQuestion`, `jevCriteriaSpam`, `jevCriteriaHam`, `jevThreshold`, `jevRetryCount`,
      `jevMaxSymbolsRequest`
- [ ] add the read-only display rows for the same fields
- [ ] parse the new form fields in `app/webapi/config.go` following the Gemini block at :396-430;
      `jevThreshold` uses `strconv.ParseFloat` as at :549, and the jev token is NOT read from the
      form (credential stays in CLI/DB, same as Gemini)
- [ ] run the same validation at the settings-save boundary before persisting, rolling back the
      in-memory mutation on failure — the existing pattern at `app/webapi/config.go:158` — so the UI
      cannot accept a negative cap or an emptied criteria field that bricks the next startup
- [ ] write tests for parsing every new form field, including a malformed threshold leaving the
      stored value untouched, `NaN` being rejected rather than stored, and a negative cap or an
      emptied criteria field being rejected with the prior settings restored
- [ ] add the new fields to the settings round-trip test in `e2e-ui/e2e_test.go`
- [ ] run tests - must pass before task 6
- [ ] run the e2e suite - must pass before task 6

### Task 6: Verify acceptance criteria

- [ ] verify all requirements from Overview are implemented
- [ ] verify jev stays disabled with no token set and that no behavior changes in that case
- [ ] verify a jev failure is a non-flipping participant, tested in mixed `any` and `all` cases with
      another provider — under `any` consensus a second successful provider can still flip the
      result, so this is not a global fallback
- [ ] run full test suite: `go test -race ./...`
- [ ] run e2e tests: the Playwright suite in `e2e-ui/`
- [ ] run `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0`
- [ ] verify test coverage meets the project's 80% standard for the new file

### Task 7: [Final] Update documentation

- [x] add every new flag to the "All Application Options" section of README.md (done in Task 3,
      forced by `TestREADMEAllOptionsMatchesHelp`)
- [ ] add a descriptive jev section to README.md covering what the provider is, the one-question
      design, the threshold, and that 0.30 is a development candidate rather than a validated default
- [ ] add a CLAUDE.md section documenting the provider, the threshold's development-candidate status,
      and the two-place wiring trap at `detector.go:343-352`
- [ ] move this plan to `docs/plans/completed/`

## Post-Completion

*Items requiring manual intervention or external systems - no checkboxes, informational only*

**Holdout validation before calling 0.30 a validated default:**

The evaluated configuration is frozen and must be reproduced exactly: `jev-1.13.0`, the question and
criteria text recorded in `docs/plans/completed/` alongside this plan, combined authored plus
quote/reply input, no history, 6000-rune input cap, threshold 0.30. The evaluation joined post and
quote with a newline; the shipped code fuses them (see the discrepancy above), so the numbers in
this plan do not transfer to fused input and 0.30 stays an unvalidated candidate for the real
production transform.

The holdout must exercise the same construction path, in this order: combined authored plus
quote/reply, then `cleanText` (which removes every Cc and Cf rune, not only the joining newline),
then history formatting when enabled, then the rune cap, then the request JSON. With history off the
joined boundary disappears entirely; with history on, the shared runner adds its own newlines
afterwards. An offline request-capture test proves those bytes without calling jev at all.

A worse holdout result would NOT show the separator needs fixing — new traffic, new labels and model
behavior are all confounded with it. Only a paired comparison on identical rows isolates the
separator, and none is needed now.

- collect fresh intake at the provider boundary — messages both checkers see, not only bans
- freeze acceptance criteria before any scores are looked at
- label independently of whoever tunes the threshold; a single labeler is exploratory evidence only
- report raw false-positive counts, recall and uncertainty against the existing provider, not a
  favorable threshold sweep
- if it fails and the threshold is retuned, that holdout becomes development data and a new one is
  needed

**Not validated by this work:**

- veto mode. Its input population is heuristic-spam candidates, which this evaluation never covered;
  any p below the threshold clears them, ambiguous cases included.
- the false-positive rate. Zero on 62 selected ham gives an approximate one-sided 95% bound near one
  in twenty, which is not a production rate.

**Separate cleanup, not part of this change:**

- `detector.go:343-352` computes per-provider short-message booleans outside the `llmChecks` slice,
  so a provider registered only in the slice misses short messages whenever no existing provider
  already opens that gate. Filed as `docs/backlog/llm-provider-set-hardcoded-outside-slice.md`;
  folding it in would move live code that currently short-circuits early.

- `cleanText` deletes newlines instead of replacing them with a space, fusing the post to its quoted
  text for every LLM provider and gluing the adjoining words together. Affects OpenAI and Gemini
  today. Filed as `docs/backlog/cleantext-deletes-quote-separator.md`; fixing it changes what every
  content check sees, so it needs its own before-and-after rather than riding along with this
  change.

## Appendix: the frozen evaluated policy

This is the exact question and criteria the 150-message evaluation used. Copy verbatim into the
`default:` struct tags in Task 3. Changing any of it invalidates the 0.30 candidate.

**instructions:**

```
Is `message`, posted in a public Telegram group chat, spam?
```

**criteria.true:**

```
It promotes, advertises, or offers paid services, paid subscriptions, paid content, donations, crypto wallets, paid promotion of content or accounts, job recruitment, hiring, looking for employees, unsolicited job postings, easy money offers, work-from-home offers with specific payment amounts, VPN promotion, or invitations to join Telegram bots or channels for earnings.
```

**criteria.false:**

```
Ordinary conversation between chat members. Casual discussion or mentioning prices of well-known services and products such as GitHub Copilot, ChatGPT Plus, cloud providers or software tools is NOT spam. Off-topic banter, rudeness, profanity, questions, and links shared as part of a conversation are NOT spam. Only direct selling, promoting, or advertising counts as spam.
```

Derived from the radio-t production `OPENAI_CUSTOM_PROMPT`, with the output-format and
confidence-floor instructions removed — a noul returns a probability, so asking for a
JSON verdict above a confidence floor would threshold an already-thresholded decision.

**This text is the evaluated baseline, not an encoding of the bot-handle rule.** Its false side
expressly permits ordinary conversation and conversational links and says only selling, promoting or
advertising counts, so it does not ban a bot recommendation that answers a genuine question. That is
precisely why the `vpnconcord_bot` row is a legitimate model miss under the labels rather than a
scoring error: the label follows the maintainer's stated policy, the prompt does not yet encode it.
Recorded as a known policy miss in the baseline. Encoding the rule is a separate, explicit new
prompt candidate that keeps this historical text and its scores intact — freezing an experiment
records what was measured, it does not forbid correcting policy later. Any such candidate should
stay scoped to this chat's policy rather than becoming a blanket ban on every bot mention.

The labels also mix two kinds of judgment, and the distinction is worth keeping visible: the initial
pass was blind content labeling, while the bot-handle adjudication is a later policy decision
applied uniformly. A policy call does not need to pretend it was a blind label.
Neither criteria string may be empty; an operator who wants one side unconstrained sets it to a
single explanatory sentence rather than blanking it, and normalization must not silently rewrite
what an operator chose.

Smells pre-check: 7 items fixed before save (enablement predicate vs APIBase default, validation/enablement predicate divergence, JevConfig field enumeration, FieldJevToken visibility exemption recorded, llmResults capacity as a third hard-coded site, per-task gate directory scoping, gate coverage for exported fields and types).
