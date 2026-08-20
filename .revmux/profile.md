# tg-spam review profile

## What this software is

A Telegram anti-spam bot plus a reusable Go library. The bot watches group messages, runs them
through a detector, and on a spam verdict deletes the message and permanently bans the sender.
`lib/` ships as `github.com/umputun/tg-spam/lib/...` at v1 and has outside consumers; `app/` is the
bot, its web UI and its storage.

## What a real failure looks like here

Severity follows consequence, and the consequences are asymmetric.

- **A false positive bans a real person permanently.** Any change that can turn a ham verdict into a
  spam one, widen what a check matches, or let a check fire on text the user did not author, is a
  correctness defect at `major` or above even when the code reads fine.
- **A false negative lets spam through**, which is recoverable. Same class of bug, one severity lower.
- **A verdict-path regression is the worst case**: `Detector.Check` is one function whose ordering
  decides which checks can be vetoed and which are final. Moving a check across a `return`, or
  changing what lands in `cr` before `isSpamDetected`, silently changes ban policy. Treat any edit to
  its control flow as high blast radius.
- **A v1 API break in `lib/`** is a real defect. Changing an exported signature under the same import
  path breaks outside consumers; additive types plus adapters are the expected shape.
- **Concurrency**: the Lua VM is a single shared `*lua.LState` and is not goroutine-safe. Anything
  touching `plugin.Checker` must hold its write lock across the whole Lua interaction.
- **Storage**: sqlite and postgres are both supported. A query or migration correct on one and not the
  other is a defect, not a portability nit.

## Blast radius

`lib/tgspam/detector.go` and `lib/tgspam/plugin/` affect every deployment and every library consumer.
`app/events/listener.go` decides what actually gets deleted and banned. `app/storage/` changes reach
existing databases. Example plugins under `_examples/` and docs are read by users writing their own
Lua, so a wrong contract there causes real misconfiguration.

## Reporting bar

- Report defects, not preferences. The linter config (`.golangci.yml`, `lll` at 130 cols, `wrapcheck`,
  `dupl`, `revive`) already enforces style; a finding a linter would catch is noise.
- Missing tests for new behavior are worth reporting. New code is expected to arrive with tests, and
  bug fixes are expected to have a test that failed first.
- Do not report a finding whose fix is larger than the defect. Match the fix to the finding.
- Finding nothing is a valid answer.

## Deliberate conventions, not defects

- Comments and log messages are lowercase; godoc on exported items is the exception.
- Interfaces are defined in the consuming package; mocks are generated with `moq` into `mocks/`
  subpackages and are never hand-edited.
- Errors are wrapped with `fmt.Errorf("context: %w", err)`.
- Private by default: identifiers are exported only when used outside the package.
- Functions called only from a struct's methods are themselves methods.
- The frontend is HTMX with as little JavaScript as possible. Absence of a JS framework is intended.
- Several checks deliberately hard-return before the LLM (short-message flood, prohibited languages)
  so the LLM cannot veto them. That asymmetry is designed, not an oversight.
