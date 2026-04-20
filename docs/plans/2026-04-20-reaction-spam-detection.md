# Reaction Spam Detection

## Overview

New spam strategy: bots don't write messages but mass-react to others' posts, luring users into their profile where spam is in bio. Solution: a separate `reactionDetector` with a per-user sliding window, modeled on `duplicateDetector`. Opt-in feature behind a flag, only for unapproved users.

Integrated via a dedicated path (not through `Check(msg)`) since reactions are not messages and routing through LLM/CAS/stopwords is meaningless.

## Context (from discovery)

- `lib/tgspam/duplicate.go` — template for `reactionDetector` (LRU cache, sliding window, per-user)
- `lib/tgspam/detector.go:100-141` — `Config` struct where `ReactionSpam` is added; `NewDetector` initializes detectors
- `app/bot/spam.go:42-55` — `Detector` interface (extend with `CheckReaction`)
- `app/events/events.go:68-75` — `Bot` interface (extend with `OnReaction`)
- `app/events/listener.go:153-156` — `NewUpdate` + main loop, add `MessageReaction` routing here
- `app/main.go:145-148` — CLI flags `Duplicates` group, add `Reactions` group by same pattern
- `Detector` mock generated via `moq` at `app/bot/mocks/detector.go`
- `Bot` mock generated via `moq` at `app/events/mocks/bot.go`

## Development Approach

- **Testing approach**: Regular (code first, then tests)
- Complete each task fully before moving to the next
- Tests are required in every task
- All tests must pass before moving to the next task

## Progress Tracking

- Mark completed items with `[x]`
- Add newly discovered tasks with ➕ prefix
- Mark blockers with ⚠️ prefix

## What Goes Where

- **Implementation Steps**: code and test changes in this repo
- **Post-Completion**: manual testing, updating production bot config

## Implementation Steps

### Task 1: reactionDetector in lib/tgspam/

- [x] create `lib/tgspam/reaction.go` — `reactionDetector` struct with fields: `threshold int`, `window time.Duration`, `cache cache.Cache[int64, reactionHistory]`, `mu sync.RWMutex`
- [x] `reactionHistory` struct: `count int`, `firstSeen time.Time`, `lastSeen time.Time`
- [x] `newReactionDetector(threshold int, window time.Duration) *reactionDetector` — returns nil if threshold <= 0
- [x] `check(userID int64) spamcheck.Response` — bumps counter, returns `spam=true` when threshold exceeded; resets when `firstSeen + window < now`
- [x] create `lib/tgspam/reaction_test.go` — table-driven tests: disabled (threshold=0), below threshold, threshold reached, window expiry resets counter
- [x] `go test -race ./lib/tgspam/... -run TestReaction` must pass

### Task 2: integration into Detector

- [ ] in `lib/tgspam/detector.go` add to `Config` struct (after `DuplicateDetection`):
  ```go
  ReactionSpam struct {
      MaxReactions int
      Window       time.Duration
  }
  ```
- [ ] add field `reactionDetector *reactionDetector` to `Detector` struct
- [ ] in `NewDetector` initialize: `reactionDetector: newReactionDetector(p.ReactionSpam.MaxReactions, p.ReactionSpam.Window)` — returns nil if MaxReactions <= 0 (disabled)
- [ ] add method `CheckReaction(userID int64) spamcheck.Response` on `*Detector` — calls `d.reactionDetector.check(userID)`, returns `{Name:"reactions", Spam:false, Details:"disabled"}` if detector is nil
- [ ] add tests in `lib/tgspam/detector_test.go` for `TestDetectorCheckReaction`: disabled by default, enabled+below threshold, enabled+spam triggered
- [ ] `go test -race ./lib/tgspam/... -run TestDetector` must pass

### Task 3: Detector and Bot interfaces + SpamFilter.OnReaction

- [ ] in `app/bot/spam.go` add `CheckReaction(userID int64) spamcheck.Response` to `Detector` interface
- [ ] regenerate mock: `go generate ./app/bot/...`
- [ ] in `app/events/events.go` add `OnReaction(userID int64, userName string) bot.Response` to `Bot` interface
- [ ] regenerate mock: `go generate ./app/events/...`
- [ ] in `app/bot/spam.go` add `OnReaction(userID int64, userName string) Response` method on `*SpamFilter`:
  - skip if `s.IsApprovedUser(userID)` → `Response{}`
  - call `s.Detector.CheckReaction(userID)`
  - if spam=true → return `Response{Spam: true, BanInterval: permanentBanDuration, User: User{ID: userID, Name: userName}, CheckResults: []spamcheck.Response{resp}}`
- [ ] add `TestSpamFilterOnReaction` in `app/bot/spam_test.go`: approved user skipped, below threshold no ban, threshold reached returns ban response
- [ ] `go test -race ./app/bot/... -run TestSpamFilter` must pass

### Task 4: listener — reaction routing

- [ ] in `app/events/listener.go` in `Do()` add `u.AllowedUpdates = []string{"message", "edited_message", "callback_query", "message_reaction"}` after `u := tbapi.NewUpdate(0)` (line 153)
- [ ] add branch before `if update.Message == nil { continue }`:
  ```go
  if update.MessageReaction != nil {
      if err := l.procReaction(ctx, update.MessageReaction); err != nil {
          log.Printf("[WARN] failed to process reaction: %v", err)
      }
      continue
  }
  ```
- [ ] implement `procReaction(ctx context.Context, r *tbapi.MessageReactionUpdated) error`:
  - skip if `r.User == nil` (anonymous admin)
  - skip if `r.Chat.ID != l.chatID` (only main chat)
  - call `l.Bot.OnReaction(r.User.ID, r.User.UserName)`
  - if `resp.Spam` → call `banUserOrChannel(banRequest{...})` + `l.adminHandler.ReportBan(...)`
- [ ] add `TestProcReaction` in `app/events/listener_test.go`: nil user skip, wrong chatID skip, below threshold no ban, threshold reached triggers ban, approved user not banned even after threshold
- [ ] `go test -race ./app/events/... -run TestProcReaction` must pass

### Task 5: CLI flags and wiring in main.go

- [ ] in `app/main.go` add flag group after `Duplicates`:
  ```go
  Reactions struct {
      MaxReactions int           `long:"max-reactions" env:"MAX_REACTIONS" default:"0" description:"max reactions per user in window to trigger spam ban (0=disabled)"`
      Window       time.Duration `long:"window" env:"WINDOW" default:"1h" description:"time window for reaction spam detection"`
  } `group:"reactions" namespace:"reactions" env-namespace:"REACTIONS"`
  ```
- [ ] wire into `detectorConfig`:
  ```go
  detectorConfig.ReactionSpam.MaxReactions = opts.Reactions.MaxReactions
  detectorConfig.ReactionSpam.Window = opts.Reactions.Window
  ```
- [ ] add `log.Printf` when feature is enabled
- [ ] `go build ./app` must succeed

### Task 6: final verification and docs

- [ ] `go test -race ./...` — all tests pass
- [ ] `golangci-lint run` — no errors
- [ ] `unfuck-ai-comments run --fmt --skip=mocks ./...` — normalize comments
- [ ] update README.md: add reaction spam section in "Spam Detection Modules" and a row in "All Application Options"

## Technical Details

**User writes a message then reacts (edge case):**
- Reaction counter is NOT reset when a user writes a message
- Protection relies on the approval gate: once a user passes enough messages (`FirstMessagesCount`) they become approved, and `OnReaction` skips them via `IsApprovedUser` check
- Gray zone: user who reacts N times while still unapproved can still be banned — this is acceptable given the feature targets bots that never write at all

**`reactionDetector` — simpler than `duplicateDetector`:**
- No message hashes, no `extraIDs` for deletion — reactions don't create messages to delete
- Counter resets when `firstSeen + window < now` (simple TTL instead of rolling window per entry)
- LRU cache with `WithMaxKeys(10000).WithTTL(window * 2)` same as duplicates

**`procReaction` → ban flow:**
- Uses existing `banUserOrChannel()` unchanged
- `resp.BanInterval = 0` means permanent ban (same as other detectors)
- No `ExtraDeleteIDs` — reactions don't create messages

**AllowedUpdates:**
- Telegram doesn't send `message_reaction` updates by default
- Must explicitly add to `UpdateConfig.AllowedUpdates`

## Post-Completion

**Manual testing:**
- Enable with `--reactions.max-reactions=5 --reactions.window=1m` on a test group
- Verify approved users are not banned for active liking
- Verify anonymous reactions (from channels without User) are ignored
