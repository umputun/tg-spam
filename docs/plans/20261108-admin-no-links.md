# Non-clickable spam text in admin-chat notifications (--admin.no-links)

## Overview
- Issue https://github.com/umputun/tg-spam/issues/484: ban and report notifications in the admin chat repeat the spam text, and Telegram turns its URLs, `t.me` links and `@mentions` into clickable links. A moderator can open a phishing link by accident.
- Add an opt-in `--admin.no-links` / `ADMIN_NO_LINKS` flag, default off. When on, the spam text in those notifications is sent inside a `pre` entity, which Telegram clients do not linkify. So are the detection check lines ("spam detection results", "original detection results"): their details come from untrusted input and can repeat the scam URL, for example a matched stop word (`lib/tgspam/detector.go:1390`, printed unchanged by `spamcheck.Response.String`) or an LLM reason. The header (profile link, title) and the buttons stay as they are.
- Admin-chat notifications that carry spam text are built as plain text plus explicit `MessageEntity` values, with no parse mode, whatever the flag. The flag only decides whether the spam text and the check lines get the `pre` entity. This gives one rendering path per notification instead of a Markdown path and an entity path side by side.
- Button edits stop re-parsing the rendered text as Markdown. They resend `query.Message.Text` with `query.Message.Entities` and append their note as new entities. This fixes `docs/backlog/callback-edits-resend-unescaped-text.md` (a literal `[x](url)` in spam turns into a hidden link after "keep banned" or "unban") and keeps the `pre` entity on every edit, so a message sent with the flag on stays protected after the flag is turned off.

## Context (from discovery)
- Notifications carrying spam text: `admin.ReportBan`, `admin.directReport` (app/events/admin.go); `userReports.sendAutoBanNotification`, `sendReportNotification`, `updateNotificationForAutoBan`, `updateReportNotification`, and the remaining-reporters branch of `callbackReportBanReporterConfirm` (app/events/reports.go).
- `admin.ReportUserBan` carries no spam text but shares `sendWithUnbanMarkup` with `ReportBan`, so it moves to the entity path with it.
- Callback edits appending to `query.Message.Text`: `callbackBanConfirmed`, `callbackUnbanConfirmed`, `callbackShowInfo` (admin.go); `callbackReportBan`, `reportBanFailure`, `callbackReportReject`, the all-reporters-banned branch of `callbackReportBanReporterConfirm` (reports.go).
- `send()` (app/events/events.go) forces a parse mode on edits and falls back Markdown, HTML, plain. An entity message must never fall back to plain text, which would bring the links back.
- `sendReportNotification` stores the sent message's ID (`resp.MessageID`, reports.go:607-613) for the later auto-ban and new-report edits, so the entity send must return the message.
- Training reads the rendered text: `getCleanMessage` (admin.go) takes lines 2 to "spam detection results" of `query.Message.Text` and feeds `UpdateSpam`/`UpdateHam`. `extractUsername` parses the rendered first line.
- Config path for an admin option, same as `DisableAdminSpamForward`: `options` in app/main.go, `optToSettings` in app/settings.go, `Settings.Admin` in app/config/settings.go, `TelegramListener` field copied into the `admin{}` and `userReports{}` literals in app/events/listener.go, Behavior tab of app/webapi/assets/settings.html and app/webapi/config.go, a `TestSettings_*Persists` e2e test.
- Vendored `MessageConfig.Entities` and `EditMessageTextConfig.Entities` exist (vendor/github.com/OvyFlash/telegram-bot-api/configs.go:314, :1387); no library change needed.
- Out of scope, all for the same reason: the admin has just forwarded the original into the admin chat, where that forward is already clickable. `MsgHandler`'s detection results (admin.go:199), `msgHandlerFallback`'s detection results (admin.go:279) and its locator-fallback warning (admin.go:316) keep their Markdown path.

## Development Approach
- **testing approach**: TDD (tests first)
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
- maintain backward compatibility

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
- **unit tests**: required for every task (see Development Approach above)
- **e2e tests**: a new `TestSettings_AdminNoLinksPersists` in `e2e-ui/e2e_test.go`, modeled on `TestSettings_MentionOnlyPersists`, in the config task
- notification tests assert `Text`, `Entities` (type, offset, length, URL) and an empty `ParseMode`, not Markdown strings
- mocks prove the payload only; whether Telegram clients actually leave text inside `pre` unlinked is checked by hand (Post-Completion)

## Progress Tracking
- mark completed items with `[x]` immediately when done
- add newly discovered tasks with ➕ prefix
- document issues/blockers with ⚠️ prefix
- update plan if implementation deviates from original scope
- keep plan in sync with actual work done

## Solution Overview
- `entityText` (new, app/events/entities.go) builds a message's text and its entities together, counting offsets in UTF-16 code units as Telegram requires. It can start empty or from an existing message's text and entities.
- `sendEntities` (new, app/events/events.go next to `send`) sends a `MessageConfig` or `EditMessageTextConfig` with entities, no parse mode, previews off, and no fallback. It returns the sent message, and a failure as an error, so a protected message is never resent as plain text.
- Notification builders write the same rendered text Telegram shows today: header, blank line, spam text, blank line, rest. The spam text keeps today's shape: newlines flattened in the ban and report notifications, kept in the `directReport` notification. `getCleanMessage`, `extractUsername` and `hasBanFailureNote` keep working unchanged, and the feature adds no mutation of the spam text: training still gets exactly what it gets today.
- Literal Markdown markers in these messages (`**can't get spam info**`, `**spam detection results**`, `**original detection results**`, `**failed to parse userID ...**`) become bare text with a `bold` entity. Bare "spam detection results" still matches `getCleanMessage`'s prefix check.
- `admin` and `userReports` get a `noLinks bool` field. When true the spam text and the check lines are added with `pre`, otherwise as plain text. Edits append new check lines per the current flag and keep the `pre` entities already on the message.
- With the flag off the visible result matches today's except for user and channel names: today an escaped link label shows its backslashes (`docs/backlog/markdown-link-labels-escaped.md`), and with entities the name shows as written.
- Callback edits start from `entityTextFrom(query.Message.Text, query.Message.Entities)`, which keeps only formatting entities (`bold`, `italic`, `underline`, `strikethrough`, `code`, `pre`, `text_link`, `text_mention`). Auto-detected types (`url`, `mention`, `hashtag` and similar) are dropped, since Telegram detects them again on send.
- Notifications rebuilt from storage use the current flag. A flag change between the first send and a rebuild changes that one notification's protection; accepted.
- Task order: the builder first, then the notifications that create messages, then the callback edits that modify them. The edit task then finds `noLinks` on both types and `banFailureNote` already raw.

## Technical Details

**Design Contract (Task 1):**

Type:
- `entityText` (unexported; used only inside app/events)

Fields: `buf strings.Builder`, `size int` (UTF-16 units written), `ents []tbapi.MessageEntity`

Methods (full signatures):
- `(e *entityText) plain(s string)`
- `(e *entityText) add(s string, ent tbapi.MessageEntity)` - writes `s` and adds `ent` over it with `Offset`/`Length` filled in; skips the entity when `s` is empty
- `(e *entityText) bold(s string)`, `(e *entityText) italic(s string)`, `(e *entityText) pre(s string)`, `(e *entityText) link(label, url string)` - one-line wrappers over `add` with `{Type: "bold"}`, `{Type: "italic"}`, `{Type: "pre"}`, `{Type: "text_link", URL: url}`
- ➕ `(e *entityText) untrusted(s string, noLinks bool)` - spam text or check details: `pre` when `noLinks`, plain otherwise; added in Task 2, since `admin` and `userReports` both need it
- `(e *entityText) mark() int` - current position in UTF-16 units, the start of a span
- `(e *entityText) span(from int, ent tbapi.MessageEntity)` - adds `ent` over everything written since `from`; skips an empty span. Needed for a bold header that contains a link
- `(e *entityText) units(s string) int` - UTF-16 length of `s`; called only from `plain` and `add`
- `(e *entityText) text() string`
- `(e *entityText) entities() []tbapi.MessageEntity`

Standalone helpers planned (justification why NOT a method):
- `entityTextFrom(text string, ents []tbapi.MessageEntity) *entityText` - constructor, called from methods of both `admin` and `userReports`
- `sendEntities(msg tbapi.Chattable, tbAPI TbAPI) (tbapi.Message, error)` - called from methods of both `admin` and `userReports`, same role as `send`; returns the error with only `withMigrationHint` applied, and callers wrap it with their own context as they do today

Exports: none

**Design Contract (Task 2):**

Methods (full signatures):
- `(a *admin) sendWithUnbanMarkup(e *entityText, user bot.User, msgID int) error` - both callers pass `"change ban"` and `a.adminChatID`, so the label is set inside and the chat ID read from the receiver

Standalone helpers planned: none

Exports: none

**Design Contract (Task 3):**

Type:
- `reportNotice` (unexported option struct, no methods): `title string`, `reports []storage.Report`, `note string`, `banErr error`, `padded bool`

Methods (full signatures):
- `(r *userReports) reportNotification(n reportNotice) *entityText` - title in bold, reported user line, spam text (pre or plain per `r.noLinks`), "Reporters:" in bold with one link per reporter, optional italic `note`, ban failure note, super-user attention line, optional braille padding
- `(r *userReports) addReportedUser(e *entityText, name string, id int64)` - replaces `reportedUserMD`: user link, `t.me` link for an `@` channel, plain `name (id)` otherwise

Standalone helpers planned: none

Exports: none

**Text and entity shape (ReportBan, flag on):**
```
text:     "permanently banned NAME\n\nspam text with https://x.example\n\n"
entities: bold(0, len("permanently banned NAME")), text_link over NAME -> tg://user?id=N,
          pre over "spam text with https://x.example"

from := e.mark(); e.plain("permanently banned "); e.link(name, "tg://user?id=N"); e.span(from, bold)
e.plain("\n\n"); e.pre(body); e.plain("\n\n")
```
Telegram trims trailing whitespace from a message, so an entity must not extend into a trailing newline; `add` covers only `s` and the separators are written with `plain`.

## What Goes Where
- **Implementation Steps** (`[ ]` checkboxes): code changes, tests, documentation in this repo
- **Post-Completion** (no checkboxes): the check against a real Telegram client

## Implementation Steps

### Task 1: Entity text builder and entity send path

**Files:**
- Create: `app/events/entities.go`
- Create: `app/events/entities_test.go`
- Modify: `app/events/events.go`
- Modify: `app/events/events_test.go`

- [x] write table-driven tests for `entityText`: plain only, each formatted append, a bold `span` over plain text plus a link, offsets after ASCII, Cyrillic, emoji (surrogate pairs) and the braille padding, empty append or span adds no entity
- [x] write tests for `entityTextFrom`: keeps formatting entities with their offsets, drops `url`/`mention`/`hashtag`, appending after it continues at the right UTF-16 offset
- [x] implement `entityText` and `entityTextFrom` per the Task 1 contract
- [x] write tests for `sendEntities`: message and edit configs reach `Send` with entities, empty `ParseMode` and previews off; the returned message carries the `MessageID` from `Send`; a `Send` error is returned with no second `Send` call and carries the migration hint for an upgraded group
- [x] implement `sendEntities` in `app/events/events.go`
- [x] run tests - must pass before task 2

### Task 2: Admin ban notifications built with entities

**Files:**
- Modify: `app/events/admin.go`
- Modify: `app/events/admin_test.go`

- [x] write tests for `ReportBan` with `noLinks` off and on: same `Text` as today's rendered message, bold header, `text_link` profile link (or `t.me` link / plain name for channels), spam text plain or `pre`, empty parse mode; dry-run header variant
- [x] write tests for `ReportUserBan` (all four header variants)
- [x] write tests for `directReport` with `noLinks` off and on: spam text with its newlines kept, a check whose `Details` is `https://phish.example` inside `pre` with the flag on, the "can't get spam info" fallback as bold text with no asterisks
- [x] add `noLinks bool` to `admin`; build `ReportBan`, `ReportUserBan` and the `directReport` notification with `entityText`; change `sendWithUnbanMarkup` per the Task 2 contract and send through `sendEntities`
- [x] update existing assertions in `admin_test.go` that check Markdown strings or `ParseMode` on these paths, including the `directReport` test from commit 6f8c083b
- [x] run tests - must pass before task 3

### Task 3: Report notifications built with entities

**Files:**
- Modify: `app/events/reports.go`
- Modify: `app/events/reports_test.go`

- [ ] write tests for `reportNotification`: title, reported user (user, `@` channel, plain channel), spam text truncated to 200 runes and plain or `pre` per `noLinks`, reporter links, note, ban failure note with an underscore in the reason shown literally, attention line, padding
- [ ] write a test that `sendReportNotification` still stores the sent message's ID through `UpdateAdminMsgID`
- [ ] add `noLinks bool` to `userReports`; implement `reportNotice`, `reportNotification` and `addReportedUser` per the Task 3 contract
- [ ] switch `sendAutoBanNotification`, `sendReportNotification`, `updateNotificationForAutoBan`, `updateReportNotification` and the remaining-reporters branch of `callbackReportBanReporterConfirm` to `reportNotification` plus `sendEntities`, removing the four copies of the reporter-list code
- [ ] drop Markdown escaping from `superUserAttention` and `banFailureNote`, remove `reportedUserMD`, then remove `markdownV1LinkLabel` once no caller remains; check `docs/backlog/markdown-link-labels-escaped.md` and delete it if this task makes it moot
- [ ] update existing assertions in `reports_test.go` that check Markdown strings or `ParseMode`
- [ ] run tests - must pass before task 4

### Task 4: Callback edits keep the message's entities

**Files:**
- Modify: `app/events/admin.go`
- Modify: `app/events/admin_test.go`
- Modify: `app/events/reports.go`
- Modify: `app/events/reports_test.go`
- Delete: `docs/backlog/callback-edits-resend-unescaped-text.md`

- [ ] write failing tests: a callback whose `query.Message` has spam text `[Support](https://phish.example) a_b` plus a `pre` entity is edited with that exact text, the `pre` entity kept, the note appended as an italic entity, and no parse mode; cover `callbackBanConfirmed`, `callbackUnbanConfirmed`, `callbackShowInfo`, `callbackReportBan`, `reportBanFailure`, `callbackReportReject` and the all-reporters-banned branch
- [ ] write a test that an admin user name with an underscore appears literally in the note
- [ ] write tests for `callbackShowInfo` and `callbackUnbanConfirmed` with a check whose `Details` is `https://phish.example`: inside `pre` with `noLinks` on, plain with it off; and the "can't get spam info" and "failed to parse userID" texts as bold with no asterisks
- [ ] switch those edits to `entityTextFrom` plus `plain`/`italic`/`bold`/`pre` appends and `sendEntities`; `callbackShowInfo` drops its `escapeMarkDownV1Text`, the check lines are written raw (in `pre` when `noLinks` is on), and the `**...**` markers in `callbackShowInfo` and `callbackUnbanConfirmed` become `bold` entities
- [ ] drop the spam text from the `failed to update spam for %q` and `failed to update ham for %q` errors in `callbackBanConfirmed` and `callbackUnbanConfirmed`: `postErrorToAdmin` posts error text to the admin chat through Markdown, where it would be clickable
- [ ] keep `getCleanMessage` results identical: update `TestAdmin_getCleanMessage` cases only where input shape changed, and add one asserting the body of an entity-built message
- [ ] delete the backlog item, since this task fixes every site it lists
- [ ] run tests - must pass before task 5

### Task 5: Wire the --admin.no-links setting

**Files:**
- Modify: `app/main.go`
- Modify: `app/settings.go`
- Modify: `app/settings_test.go`
- Modify: `app/config/settings.go`
- Modify: `app/config/settings_test.go`
- Modify: `app/events/listener.go`
- Modify: `app/events/listener_test.go`
- Modify: `app/webapi/assets/settings.html`
- Modify: `app/webapi/config.go`
- Modify: `app/webapi/config_test.go`
- Modify: `e2e-ui/e2e_test.go`

- [ ] add `AdminNoLinks bool \`long:"admin.no-links" env:"ADMIN_NO_LINKS" description:"show spam text in admin chat reports without clickable links"\`` to `options`, mapped in `optToSettings` to `Settings.Admin.NoLinks` (`json:"no_links" yaml:"no_links" db:"no_links"`); not in `zeroAwarePaths`
- [ ] add `NoLinks bool` to `TelegramListener` with a trailing field comment like its siblings, copy it into the `admin{}` and `userReports{}` literals, and set it from `settings.Admin.NoLinks` in `app/main.go`
- [ ] add the `adminNoLinks` checkbox to the Behavior tab edit panel and its read-only row in `settings.html`, parsed in `config.go` like `disableAdminSpamForward`
- [ ] write tests: `optToSettings` mapping, settings round trip, web form on/off parsing, and a listener test that `NoLinks` reaches both the `admin` and `userReports` handlers
- [ ] add `TestSettings_AdminNoLinksPersists` to `e2e-ui/e2e_test.go`, modeled on `TestSettings_MentionOnlyPersists` with its own port and paths
- [ ] run tests - must pass before task 6

### Task 6: Verify acceptance criteria
- [ ] with the flag on, every notification listed in Context that carries spam text or check lines sends them in `pre` entities, and every button edit keeps those entities
- [ ] with the flag off, rendered text and visible formatting match today's, with links and bold intact, except that names no longer show escape backslashes
- [ ] training input from `getCleanMessage` is unchanged for both flag values
- [ ] run full test suite: `go test -race ./...`
- [ ] run e2e tests with the command the repo documents for `e2e-ui`
- [ ] run linter: `golangci-lint run --max-issues-per-linter=0 --max-same-issues=0`
- [ ] verify test coverage for app/events does not drop

### Task 7: [Final] Update documentation
- [ ] README.md "All Application Options": add `--admin.no-links` exactly as `--help` prints it
- [ ] README.md admin chat section: what the flag does (spam text and check lines), that button edits keep it, and that the header link and buttons are unchanged
- [ ] CLAUDE.md: section on admin-chat notifications built with entities (no parse mode, edits keep entities, no plain-text fallback, `getCleanMessage` layout contract)
- [ ] CLAUDE.md: revise the "No-Message Ban Notifications" bullets, which describe `markdownV1LinkLabel`, `ModeMarkdown` label rules and the `[user](tg://user?id=N)` form, to the entity form
- [ ] move this plan to `docs/plans/completed/`

## Post-Completion
*Items requiring manual intervention or external systems - no checkboxes, informational only*

**Manual verification:**
- in a test group with an admin chat, run with `--admin.no-links` and post spam containing a URL, a `t.me` link, an `@mention` and a literal `[Support](https://phish.example)`; check in the desktop and mobile clients that none of it is clickable in the ban report
- press "info", "change ban" then "keep banned", and "unban"; check the spam text stays unclickable after each, and that the ham/spam sample added matches the original text
- with a stop word set to a URL, check the "spam detection results" line shows it unclickable after "info" and "unban"
- run once without the flag and check the report looks as before, profile link included; press the buttons and check that URLs in the spam text are clickable again after each edit, which confirms Telegram re-detects the dropped `url`/`mention` entities
- confirm Telegram accepts the edits built from `query.Message.Entities` (no "can't parse entities" errors in the log)

Smells pre-check: 5 items fixed before save (sendWithUnbanMarkup cut to 3 params via a contract; mark/span added so a bold header can contain a link; add takes an entity instead of three strings; sendEntities keeps the migration hint; markdownV1LinkLabel removal moved to the report-notification task), plus String renamed to text and the listener field comment
