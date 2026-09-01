# tg-spam Development Guidelines

## Build & Test Commands
- Build: `go build -o tg-spam ./app`
- Run tests: `go test -race ./...`
- Run single test: `go test -v -race ./path/to/package -run TestName`
- Lint: `golangci-lint run`
- Coverage report: `go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out`

## Important Workflow Notes
- Always run tests and linter before committing
- For linter use `golangci-lint run`
- Run tests and linter after making significant changes to verify functionality
- Go version: 1.25+
- Don't add "Generated with Claude Code" or "Co-Authored-By: Claude" to commit messages or PRs
- Do not include "Test plan" sections in PR descriptions
- Do not add comments that describe changes, progress, or historical modifications. Avoid comments like "new function," "added test," "now we changed this," or "previously used X, now using Y." Comments should only describe the current state and purpose of the code, not its history or evolution.
- Use `go:generate` for generating mocks, never modify generated files manually. Mocks are generated with `moq` and stored in the `mocks` package.
- After important functionality added, update README.md accordingly
- When adding new CLI parameters or environment variables, update BOTH:
  1. The "All Application Options" section in README.md (should match `--help` output exactly)
  2. The appropriate descriptive section in README.md (e.g., spam detection modules, OpenAI integration, etc.)
- When adding a new configurable parameter (CLI flag, env var, or any user-tunable knob), wire it through ALL of these layers — missing any one leaves the feature partially broken:
  1. **CLI/env definition**: `app/main.go` (flag + env var) and `app/settings.go` `optToSettings` mapping (CLI → Settings)
  2. **DB-backed config**: `app/config/settings.go` — add to the typed `Settings` struct with `json`/`yaml`/`db` tags. If `0`/empty must survive merges as a meaningful "disabled" value, add the dotted path to `zeroAwarePaths`. Add startup validation if needed.
  3. **Web settings UI**: `app/webapi/assets/settings.html` — add the form input (edit panel) AND the read-only display row. `app/webapi/config.go` — parse the new form field exactly like an existing analogous field (look for the closest sibling: int → `reportAutoBanThreshold`, duration → `reactionsWindow`, bool → existing checkboxes).
  4. **e2e UI test**: `e2e-ui/e2e_test.go` — add to the settings round-trip test so the form field is exercised.
  5. **Docs**: README.md (both options-table and descriptive section, per the rule above).
- When merging master changes to an active branch, make sure both branches are pulled and up to date first
- Don't add "Test plan" section to PRs

## Libraries
- Logging: `github.com/go-pkgz/lgr`
- CLI flags: `github.com/jessevdk/go-flags`
- HTTP/REST: `github.com/go-pkgz/rest` with `github.com/go-pkgz/routegroup`
- Middleware: `github.com/didip/tollbooth/v8`
- Database: `github.com/jmoiron/sqlx` with `modernc.org/sqlite`
- Testing: `github.com/stretchr/testify`
- Mock generation: `github.com/matryer/moq`
- OpenAI: `github.com/sashabaranov/go-openai`
- Frontend: HTMX v2. Try to avoid using JS.
- For containerized tests use `github.com/go-pkgz/testutils`
- To access libraries, figure how to use ang check their documentation, use `go doc` command and `gh` tool

## Web Server Setup
- Create server with routegroup: `router := routegroup.New(http.NewServeMux())`
- Apply middleware: `router.Use(rest.Recoverer(), rest.Throttle(), rest.BasicAuth())`
- Define routes with groups: `router.Mount("/api").Route(func(r *routegroup.Bundle) {...})`
- Start server: `srv := &http.Server{Addr: addr, Handler: router}; srv.ListenAndServe()`

## Code Style
- Format: Use `gofmt` (enforced by linter) - exclude mocks: `gofmt -s -w $(find . -type f -name "*.go" -not -path "./vendor/*" -not -path "*/mocks/*")`
- goimports: `goimports -w $(find . -type f -name "*.go" -not -path "./vendor/*" -not -path "*/mocks/*")`
- Line length: Maximum 130 characters (enforced by the lll linter)
- Error handling: Return errors with context, use multierror for aggregation
- Naming: CamelCase for variables, PascalCase for exported types/functions
- Test tables: Use table-driven tests with descriptive test cases
- Comments: Keep in-code comments lowercase
- Documentation: All exported functions and types must be documented with standard Go comments
- Interfaces: Define interfaces in consumer packages
- Mocks: Generate with github.com/matryer/moq and store in mocks package

## Spam Detection Architecture

### Quoted/Reply-to Text Handling
- When checking messages for spam, quoted text is concatenated with the main message text
- This catches spammers who quote spam content from external channels
- `Quote` (from Telegram's TextQuote) takes precedence over `ReplyTo.Text`
- The concatenation uses newline separator: `msg.Text + "\n" + msg.Quote`
- Empty quote/reply-to text is ignored (no extra newline added)
- All three paths apply the same Quote concatenation: auto-detection (`app/bot/spam.go:OnMessage`), admin `/spam` (`app/events/admin.go:directReport`), and user `/report` (`app/events/reports.go:DirectUserReport`)
- Quote concatenation is placed AFTER the transform fallback block so image-only messages with quotes get both caption text and quote text
- Exception: `bot.OnMessage` also records the appended quote/reply text in `spamcheck.Request.Quote`. The prohibited-language hard check reads `req.AuthoredText()` (which strips the trailing `"\n"+Quote` from `Msg`) instead of `req.Msg`, so quoted foreign-script content the user did not author cannot trigger its LLM-unvetoable permanent ban; all other (soft, vetoable) content checks still see the full concatenated `req.Msg`

### Rich Message (Article) Handling
- Telegram's client "Article" compose type (attach menu → Article) arrives via the Bot API as `Message.rich_message` (added in Bot API 10.2; requires `OvyFlash/telegram-bot-api` ≥ the 2026-07 / 10.2 revision). Older clients and the Bot API render it as "This message is not supported in your version of Telegram". Spammers used it to hide the pitch (e.g. free-VPN ads) out of `msg.Text` so no content check fired
- `transform` (`app/events/events.go`) flattens `rich_message` into `message.Text` right after the caption block, mirroring the caption pattern (set if `Text` empty, else append with `"\n"`). This routes the content through the entire existing pipeline unchanged — stopwords/classifier/LLM/prohibited-langs plus the locator (so duplicate detection works). No new config, not gated: it just feeds previously-invisible text to the same checks
- The library types `RichBlock` and `RichText` are bare `any` with no custom unmarshaler, so an incoming `rich_message` decodes as generic `map[string]interface{}`/`[]interface{}`, NOT the concrete `RichBlock*`/`RichText*` structs (those are for outgoing construction). A nested recursive closure walks the generic tree and collects non-empty string leaves under text-bearing keys (`text`, `caption`, `credit`, `summary`, `expression`, `alternative_text`); URLs, `type`, and list-item marker labels are intentionally dropped. Maps are visited in sorted-key order for deterministic output; block/span arrays keep document order
- Because the flattened text is non-empty, the message survives the `listener.go:397-398` empty-text guard automatically; forwarded articles (`forward_origin` set) also work. A rich message with no extractable text (media-only) still yields empty `Text` and is dropped by that guard — accepted, since there is no content to check

### External Reply Detection
- `--meta.external-reply` / `META_EXTERNAL_REPLY` (Settings.Meta.ExternalReply) is an opt-in meta check that flags any message replying to a message from another chat (Telegram's `external_reply`, distinct from an in-chat `reply_to_message`). Disabled by default. Targets the "reply to an external scam channel + short fake-testimonial comment" pattern (observed: replies to a Beeline-scam channel with `text`="Пришло быстро!" and a bold `quote`="Написать менеджеру: @Beeline"), where the classifier/similarity/LLM see only the tiny comment and clear it
- Mirrors `ForwardedCheck` exactly: blunt (any external reply → spam), no gating beyond the on/off toggle. It is a distinct check from `forward` because `external_reply` is a reply, not a forward — `msg.ForwardOrigin` is nil for these, so the `forward` check reports "not a forwarded message" and never fires on them
- Wiring (standard meta-check path): `msg.ExternalReply != nil` → `bot.Message.WithExternalReply` (transform, `app/events/events.go`) → `spamcheck.Request.Meta.HasExternalReply` (`bot.OnMessage`) → `ExternalReplyCheck()` (`lib/tgspam/metachecks.go`, returns `external-reply` spam/`external reply`). Gated in `app/main.go` by `settings.Meta.ExternalReply`; part of the `IsMetaEnabled()` aggregate; NOT in `zeroAwarePaths` (a plain opt-in bool, default false). Also exposed to Lua plugins as `req.meta.has_external_reply` (`lib/tgspam/plugin/checker.go`), like every other `Has*` meta field
- `transform` only sets `WithExternalReply` for a genuinely cross-chat reply: `msg.ExternalReply != nil && (msg.ExternalReply.Chat == nil || msg.ExternalReply.Chat.ID != msg.Chat.ID)`. Telegram's `external_reply` also fires for a reply *across forum topics within the same chat*; without the chat-ID guard, a legitimate cross-topic reply in a forum supergroup would be flagged (and, when enabled, permanently banned). A hidden/inaccessible origin chat (`Chat == nil`) is treated as external. The observed Beeline spam has `external_reply.chat.id != msg.chat.id`, so it is still caught
- Bot API limitation: `ExternalReplyInfo` (`vendor/.../types.go`) has no text/caption field, so the referenced external post's body text is unavailable — only its origin/chat identity and media type. The check therefore keys purely on presence, not content. The quoted snippet a spammer selects still arrives separately via `msg.Quote` and is handled by the existing quote concatenation

### Channel Message Handling
- When a channel posts in a group, Telegram uses a shared fake user `Channel_Bot` (ID `136817688`) in `msg.From`
- The actual channel identity is in `msg.SenderChat` with unique ID and username
- `SenderChat.ID` is used for locator tracking (`AddMessage`, `AddSpam`), and for banning via `BanChatSenderChatConfig`
- `bot.OnMessage` sets `Response.ChannelID = msg.SenderChat.ID` when SenderChat is present
- Admin `/spam` command (`directReport`) detects `origMsg.SenderChat` and passes channel ID for ban and cleanup
- Anonymous admin posts (where `SenderChat.ID == group chat ID`) skip spam check entirely
- Linked channel (resolved via `ChatFullInfo.LinkedChatID` at startup) is treated as superuser for `/ban`, `/spam`, `/warn` commands
- Linked channel messages also skip spam checking (same as anonymous admin posts)
- `isLinkedChannel(msg)` helper checks `l.linkedChannelID != 0 && msg.SenderChat != nil && msg.SenderChat.ID == l.linkedChannelID`
- `channelDisplayName` resolves display name from `*tbapi.Chat`: UserName > Title > `channel_<ID>`
- `ReportBan` uses `https://t.me/<username>` links for channels (not `tg://user` which doesn't resolve negative IDs); plain name+ID for channels without username
- Admin `/warn` command (`DirectWarnReport`) targets the channel display name instead of `@Channel_Bot`
- Admin notification text in `directReport` shows channel display name and channel ID instead of Channel_Bot identity
- `extractUsername` supports `tg://user` links, `t.me` channel links, plain channel name+ID, and `{id name...}` formats

### Report Command Routing
- `spam`, `/spam`, `report`, `/report` are equivalent for a non-superuser: all four are matched by `isReportCommand` (`app/events/listener.go`) and behave identically in every path. `/report@botname` is additionally accepted; there is no `/spam@botname` equivalent, deliberately (nothing autocompletes it, the bot never calls `SetMyCommands`)
- The same text from a superuser or the linked channel never reaches `isReportCommand` — `procSuperReply` intercepts it earlier in the `Do` loop for ban + spam-sample training. Identical text, different handler, decided purely by sender privilege
- **`isReportCommand` has TWO consumers**, and this is the trap: `procUserReply` (files the report) and the orphaned-command cleanup in `Do`, which deletes a matching message sent *without* a reply. Anything added to the predicate immediately becomes deletable as an orphaned command, in every chat the bot receives updates for, and that branch is NOT gated on `ReportConfig.Enabled` — unlike `procUserReply`, which suppresses the command when reporting is off
- The user-report dispatch requires `update.Message.SenderChat == nil`. Senders posting on behalf of a chat (anonymous admin `GroupAnonymousBot`, "post as channel" `Channel_Bot`) carry a pseudo-user in `From` that can never be an approved reporter, so without the guard `DirectUserReport` deletes their command message and files nothing
- Known gap, unrelated to the command forms: `DirectUserReport` never resolves `SenderChat` for the *reported* message, so a channel-origin spam post is filed and banned as the shared `Channel_Bot` identity rather than the channel. `admin.go`'s `directReport` handles this correctly; the user-report path does not

### ExtraDeleteIDs Feature
- `spamcheck.Response` includes `ExtraDeleteIDs []int` field for additional message IDs to delete when spam is detected
- Any spam checker can populate this field to request deletion of related messages
- Currently used by duplicate detector to delete all previous duplicates when threshold is reached
- The listener handles these deletions with rate limiting (35ms between deletions) to respect Telegram API limits
- Deletion errors are logged but don't fail the operation (messages might be already deleted or too old)
- Design principle: When a spammer is detected, aggressively clean up ALL their spam messages, not just the triggering one

### Warn Auto-Ban
- `/warn` admin command optionally records each warning to `warnings` table and bans the user when count within `warn.window` reaches `warn.threshold`
- Disabled by default (`warn.threshold=0`); enabling preserves the existing warn message + delete behavior and adds the count/ban path on top
- Threshold semantics match `Report.AutoBanThreshold`: `count >= threshold` triggers ban (so threshold=2 bans on the 2nd warn)
- Storage layer (`app/storage/warnings.go`) opportunistically prunes rows older than 1 year on each `Add`; the 1y cap is a storage bound, NOT the configured window — `CountWithin` enforces the window at query time
- Ban does NOT delete the warning rows: subsequent `/warn` on an already-banned user re-triggers the ban path. Telegram's `BanChatMemberConfig` is idempotent, so the repeat ban is a no-op API call but produces a fresh admin-chat notification (audit visibility for repeat offenders)
- Warn auto-ban does NOT update spam samples (`bot.UpdateSpam` is not called) — warnings reflect admin policy, not spam content
- `executeWarnBan` mirrors `executeAutoBan` for dry/training/soft-ban handling but does not share an abstraction (the two diverge on spam-sample updates and on `From` vs `SenderChat` resolution)
- Settings: only `Warn.Threshold` is in `zeroAwarePaths` (0=disabled, must survive merges); `Warn.Window` zero is invalid and rejected by startup validation

### Short Message Flood Detection
- `--max-short-msg-count` (Settings.MaxShortMsgCount) bans an unapproved user who accumulates too many short messages without graduating to approved status; closes the gap left by content-based checks for spammers probing with innocuous one-word messages
- Disabled by default (`MaxShortMsgCount=0`); incompatible with paranoid mode (paranoid forces `FirstMessageOnly=false` and `FirstMessagesCount=0` in `makeDetector`, which would silently disable this check). Startup validation in `app/main.go` rejects `MaxShortMsgCount > 0 && ParanoidMode`
- `Settings.MaxShortMsgCount` is in `zeroAwarePaths` (0=disabled must survive merges); validation rejects negative values
- Trigger formula: `excess := total - approvedCount; spam := excess >= MaxShortMsgCount` where `total` is `CountUserMessages` from the locator and `approvedCount` is the in-memory `approvedUsers[id].Count` (incremented only on long ham messages, see the block at `detector.go:386-400`)
- Stateless within the detector — no cache, no separate struct. The check is an internal method `(d *Detector) isShortMsgFlood(req)` (`lib/tgspam/detector.go:1059`) called inline in `Detector.Check` after the duplicate-detector branch and before the stop-word/emoji/metachecks block, gated by an outer guard so disabled mode does no work
- Dependency: `MessageCounter` interface (consumer-side, defined in `lib/tgspam/detector.go`) implemented by `app/storage/locator.go` via `CountUserMessages` and `UserMessageIDs`. Wired in `app/main.go` with `detector.WithMessageCounter(locator)` after the locator is constructed
- Count query uses index `idx_messages_gid_user_id_time` (locator.go:70 sqlite, :74 postgres) — pure index scan, gid-scoped per the listener's locator
- Locator TTL pruning is rare for actively-evaluated unapproved users (cleanup only kicks in when `total > minSize`); accepted as a known limitation
- On flag, the response carries `ExtraDeleteIDs` populated from `UserMessageIDs(ctx, userID, MaxShortMsgCount*2)` so the listener cleans up prior messages alongside the triggering one (same pattern as duplicate detector)
- Bypasses LLM consensus by design: returns `true, cr` immediately when flagged instead of letting the unified `spamDetected`/LLM-consensus flow evaluate. Rationale: short-message flood is a behavioral signal that does not depend on content, and an LLM looking at the latest single short message has no information that would justify overriding the count
- Edge cases (handled via fast-bail returns in `isShortMsgFlood`): empty `req.UserID`, `req.CheckOnly`, message length ≥ `MinMsgLen`, user already approved (`approvedCount >= FirstMessagesCount`), locator error (logged at WARN, falls through to normal checks)
- Channel `SenderChat` flow works transparently: `bot.OnMessage` already maps `checkUserID = msg.SenderChat.ID` and the locator stores under the same ID; anonymous admin and linked-channel posts skip `OnMessage` entirely (listener.go:419-422)
- Edits: locator keys by `hash(text)` with `INSERT OR REPLACE` — no-op edits do not increment count; content-changing edits create a new row (consistent with `duplicateDetector`). Exception: text-less messages key by `hash("chatID:userID:msgID")` (`locator.go:AddMessage`), because every empty text hashes to `sha256("")` and would collapse all caption-less media in a gid onto one row under `PRIMARY KEY (gid, hash)`. An edit of such a message keeps the same key, so the no-op-edit property holds
- Media-only messages (photo/video/document with no caption, GIFs included) therefore count toward `total`: they reach `AddMessage` with empty text (`listener.go:397-398` passes them on `msg.Image`/`WithVideo`/`WithVideoNote`/`WithForward`/`WithExternalReply`, and on `WithDocument` only when `AdmitDocuments` is set) and `isShortMsgFlood` treats length 0 as short. This is intentional — image-flood probing is a real spam pattern — but it means an unapproved user posting N caption-less photos accumulates N, not 1
- Training/dry/soft-ban: feature emits a normal spam result through the existing pipeline, so the listener's mode handling intercepts identically to other spam checks (no special-casing)
- Naturally-terse legitimate users carry a bounded false-positive risk during the evaluation window only; once the user crosses `FirstMessagesCount` they are approved and immune. Recommended baseline: `MaxShortMsgCount >= 3` with low `FirstMessagesCount` (1–2)

### Document Detection
- `--meta.document-only` / `META_DOCUMENT_ONLY` (Settings.Meta.DocumentsOnly — Go field stays plural, like `VideosOnly` behind `--meta.video-only`) flags a message carrying a document (file attachment) whose text is empty or shorter than `--min-msg-len`. Disabled by default. Mirrors `VideosCheck`/`AudioCheck`: `DocumentsCheck(minTextLen int)` in `lib/tgspam/metachecks.go`, gated in `makeDetector` by the setting, wired through `spamcheck.MetaData.HasDocument` (`bot.OnMessage`) and `bot.Message.WithDocument` (`transform`, `app/events/events.go`). Also exposed to Lua as `req.meta.has_document`. Web form field is `metaDocumentOnly`
- Unlike its siblings the check measures `req.AuthoredText()`, not `req.Msg`: `bot.OnMessage` appends quoted/reply-to text to `Msg`, so a caption-less file posted as a reply would otherwise arrive with a non-empty `Msg` written by someone else and clear the length gate (at the default `min-msg-len=50` a 49-rune parent suffices, the appended `"\n"` counting). Same reasoning as the prohibited-languages check, but here it changes an ordinary soft verdict, not a hard ban
- Forwards are not special-cased. Telegram delivers a forwarded document as an ordinary message with `document` set and the *original* caption in `caption`, and `transform` already folds `caption` into `message.Text` before this check runs. A forwarded file with a long original caption looks identical to a directly posted file with a long caption — both pass. The missing text is the signal, not the forwarding; an operator who wants to act on forwarding as such already has `--meta.forward`
- GIFs count as documents. Telegram sets `message.document` for animations too (`vendor/github.com/OvyFlash/telegram-bot-api/types.go`, the `Animation` field doc: "For backward compatibility, when this field is set, the document field will also be set"). The check does not exclude `msg.Animation`, so a GIF whose caption is shorter than `--min-msg-len` — nearly every GIF — is flagged when the check is on. Deliberate strictness, not an oversight
- The intake guard at `listener.go:397-398` admits a text-less document only when `TelegramListener.AdmitDocuments` is set (wired from `settings.Meta.DocumentsOnly` in `app/main.go`), so with the check off a directly posted one is dropped before `Locator.AddMessage` as before. Not so for a forwarded file or one attached to a cross-chat reply: `WithForward`/`WithExternalReply` short-circuit the same guard on their own, so those reach the detector and the locator whatever this setting is — pre-existing, and unchanged either way by the documents check. Gating it matters because intake feeds the locator, and those rows are what `CountUserMessages` hands `isShortMsgFlood` — an unconditional guard would make one caption-less file or GIF the third strike under `--max-short-msg-count=3` for an operator who never enabled documents. With the check *on*, caption-less documents reach the detector and locator: they count toward `--max-short-msg-count`, and four identity-based checks run before the min-length gate (`detector.go:338`) and can each flag spam on their own: `isStopWord` (`detector.go:300`) matches against username and user ID as well as message text, so it can fire on an empty message; the meta-checks loop (`detector.go:309`) makes `UsernameSymbolsCheck` reachable, since it is message-independent; the Lua checks loop (`detector.go:314`) sees this message class; and CAS (`detector.go:324`) — enabled by default (`app/main.go:81`, `default:"https://api.cas.chat"`) — runs one lookup per such message and can permanently ban a CAS-listed user. Content checks find nothing in an empty message, and no LLM is reached at all: `DocumentsCheck` has already made `softSpam` true, so `Check` returns at `detector.go:355` ahead of the LLM block, whatever `--openai.check-short-messages` / `--gemini.check-short-messages` say, and neither provider's veto can override the verdict. Non-short cases proceed to the normal LLM path, where an enabled veto-mode provider can override the verdict. This includes a caption-less file whose appended parent brings `req.Msg` to at least `MinMsgLen`, or any caption-less file when `MinMsgLen` is 0
- No dedicated `--meta.document-text-len` knob. Threshold is `settings.MinMsgLen`, like `videos` and `audio`
- Not in `zeroAwarePaths`: a plain opt-in bool, default off, same as `Meta.Forward` and `Meta.ExternalReply`

### Prohibited Languages Detection
- `--prohibited-langs` (Settings.ProhibitedLangs, comma-separated) blocks messages written in an admin-configured set of Unicode scripts; solves English-only or single-script chats getting spam in another script (Chinese observed). Disabled by default (empty list)
- The check is an internal method `(d *Detector) isProhibitedLang(msg string)` (`lib/tgspam/detector.go:1186`) called inline in `Detector.Check` right after the short-msg-flood block and before the stop-words block (`detector.go:282-288`), gated by `len(d.ProhibitedScripts) > 0 && d.ProhibitedLangsMin > 0` so both an empty script list and a zero min (treated as disabled) do no work
- Hard return like short-msg-flood: on a hit it appends the response, `d.spamHistory.Push(req)`, and `return true, cr`, bypassing the LLM (policy rule the LLM can't override); a confirmed foreign-script message cannot be vetoed
- Unlike `isShortMsgFlood` it does NOT bail on `req.CheckOnly` and takes `msg string` rather than the full request: this is a pure content check, so the web `/check` UI must exercise it
- `Check` passes `req.AuthoredText()` (the user's own text), NOT the concatenated `req.Msg`, so quoted/reply-to content the user did not write cannot drive this hard, LLM-unvetoable permanent ban; the softer content checks below still see the full `req.Msg`. `AuthoredText()` strips the trailing `"\n"+Quote` from `Msg` when `Request.Quote` is set (populated by `bot.OnMessage`) and returns `Msg` unchanged when `Quote` is empty, so API/library callers that set only `Msg` are unaffected
- Counts letters only: walks runes, skips `!unicode.IsLetter(r)`, tests each letter against the configured script tables via `unicode.Is`, increments per-script counts, and returns spam the moment any single script reaches `ProhibitedLangsMin`. Digits, punctuation, emoji, and spaces do not count
- Deterministic reporting: `scriptNames` is sorted (`sort.Strings`) for stable tie-breaking and a stable `Details` string; the flagged response uses `Details: "<script>: <count>"` (e.g. `"Han: 3"`), the non-flagged response reports `"<script>: <count>/<min>"`
- Scope by placement: with default `FirstMessageOnly=true` the approval gate short-circuits earlier, so this runs only for non-approved users; in `--paranoid` mode or with `FirstMessageOnly=false` it also runs for approved users, same as every other content check. No `MinMsgLen` gate (short foreign spam is the target)
- `ResolveProhibitedScripts(names []string) (map[string]*unicode.RangeTable, error)` lives in `lib/tgspam/prohibited.go:29` (exported for cross-package use) and is the underlying resolver used directly by `makeDetector` (`app/main.go:768`, error already caught by validation so it discards it). It expands friendly aliases via `scriptAliases` (chinese→Han, russian/ukrainian→Cyrillic, japanese→Hiragana+Katakana, arabic, korean→Hangul, hebrew, thai, greek), matches other names case-insensitively against `unicode.Scripts`, skips empty/whitespace entries, and errors naming an unknown entry
- Validation goes through `ValidateProhibitedLangs(langs string, minCount int) error` (`lib/tgspam/prohibited.go:64`), which resolves the names and requires `min >= 1` when the list is non-empty. It is called from `Settings.Validate()` (`app/config/settings.go:276`), the single validator invoked at startup (`app/main.go:360`), by `save-config` (`app/main.go:393`), and at the web settings save boundary (`app/webapi/config.go:158`, rolls back the in-memory mutation on failure); this keeps a bad script name (or any other invalid settings combination) from being persisted and bricking the next `--confdb` startup
- `Config` fields: `ProhibitedScripts map[string]*unicode.RangeTable` (script name → range table, set directly from the resolver) and `ProhibitedLangsMin int` (`detector.go:128-133`). The map, not a slice, so the check can report which script hit and the resolver output is settable cross-package from `app`
- Two config knobs: `--prohibited-langs` / `PROHIBITED_LANGS` (empty disables) and `--prohibited-langs-min` / `PROHIBITED_LANGS_MIN` (default 3). Validation requires `min >= 1` when the list is non-empty
- `Settings.ProhibitedLangs` is in `zeroAwarePaths` (empty=disabled must survive merges, `config/settings.go:325`); `ProhibitedLangsMin` is NOT (default 3, zero is not a meaningful "disabled")

### Reaction Ban Notifications
- Reaction-spammer bans (`procReaction` in `app/events/listener.go`) are reported to the admin chat via `admin.ReportReactionBan`, which reuses the same `change ban`/`info` inline keyboard as `ReportBan` (`sendWithUnbanMarkup`) so admins can unban+approve a wrongly banned reaction spammer
- Reactions have no underlying message, so the callback carries `msgID = 0` as a sentinel. Two paths must honor it: `deleteAndBan` skips the Telegram delete when `msgID == 0` (removing the `if msgID != 0` guard would make every reaction-ban confirmation try to delete message 0), and `callbackUnbanConfirmed` already discards msgID (`userID, _, err := parseCallbackData(...)`)
- The notification text keeps the `[user](tg://user?id=N)` link immediately after "permanently banned" because Telegram strips markdown from callback text; `extractUsername`'s plain-channel regex would otherwise capture any words placed between "permanently banned" and the trailing `(id)` as the approved-user name
- `getCleanMessage` returns empty/error on the short reaction notification (no message body), so the unban path correctly skips `UpdateHam` — no `[reaction spam]` sample is learned

### LLM Checker Structure
- Shared provider-agnostic LLM flow lives in `lib/tgspam/llm.go`
- Keep provider-specific transport and request construction in `lib/tgspam/openai.go`, `lib/tgspam/gemini.go`, etc
- Common behavior such as history formatting, retry handling, and response detail formatting should be implemented once in `llm.go` to avoid `dupl` violations
- Shared behavior belongs in `lib/tgspam/llm_test.go`; provider tests should focus on API-specific behavior such as request shape, model options, truncation, and safety settings
- `context.Context` is threaded from `Detector.collectLLMCheck` → `check()` → `runLLMProviderCheck` → `sendRequest()` → API call; the detector creates a per-request context with `LLMRequestTimeout` (default 30s)