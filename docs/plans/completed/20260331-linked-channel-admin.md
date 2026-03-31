# Linked Channel Admin Rights

## Overview
- Grant the linked channel (whose discussion group is the bot's primary chat) admin-like rights for `/ban`, `/spam`, and `/warn` commands
- Problem: when a channel owner sends commands "as the channel" in the discussion group, `msg.From` is `Channel_Bot` (ID 136817688) which is not a superuser; the real channel identity is in `msg.SenderChat`
- The Telegram Bot API `getChat` returns `LinkedChatID` which identifies the channel linked to a discussion group
- The linked channel's regular messages should also skip spam checking (same as anonymous admin posts from the group itself)

## Context (from discovery)
- Files involved: `app/events/listener.go`, `app/events/listener_test.go`
- No changes needed in `admin.go` — `directReport` already handles channel *targets*; we only change who can *issue* commands
- `ChatFullInfo.LinkedChatID` already exists in the `go-telegram-bot-api` library (`vendor/github.com/OvyFlash/telegram-bot-api/types.go:512`)
- `GetChat` is already in the `TbAPI` interface and returns `ChatFullInfo`
- Existing pattern: anonymous admin posts skip spam check when `SenderChat.ID == fromChat` (line ~335)
- Existing pattern: `fromSuper` check at line ~255 gates `procSuperReply` for `/ban`, `/spam`, `/warn`

## Known Limitations
- Admin notification text in `directReport` and `DirectWarnReport` will show "Channel_Bot" as the issuing admin name (from `update.Message.From.UserName`), not the linked channel's display name. This is cosmetic and can be addressed in a follow-up if needed.

## Development Approach
- **testing approach**: Regular (code first, then tests)
- complete each task fully before moving to the next
- make small, focused changes
- **CRITICAL: every task MUST include new/updated tests** for code changes in that task
- **CRITICAL: all tests must pass before starting next task**
- **CRITICAL: update this plan file when scope changes during implementation**
- run tests after each change
- maintain backward compatibility

## Testing Strategy
- **unit tests**: required for every task
- test linked channel can use `/ban`, `/spam`, and `/warn` commands (table-driven)
- test linked channel messages skip spam check
- test non-linked channels are NOT treated as super
- test `isLinkedChannel` helper edge cases (nil SenderChat, zero linkedChannelID, non-matching ID)
- mock `GetChatFunc` to return `LinkedChatID` in `ChatFullInfo`

## Progress Tracking
- mark completed items with `[x]` immediately when done
- add newly discovered tasks with ➕ prefix
- document issues/blockers with ⚠️ prefix

## Implementation Steps

### Task 1: Add linkedChannelID field, resolve at startup, add helper with unit tests

**Files:**
- Modify: `app/events/listener.go`
- Modify: `app/events/listener_test.go`

- [x] add `linkedChannelID int64` field to `TelegramListener` struct (unexported, alongside `chatID`/`adminChatID`)
- [x] in `Do()`, after `getChatID` resolves `chatID`, call `l.TbAPI.GetChat(tbapi.ChatInfoConfig{ChatConfig: tbapi.ChatConfig{ChatID: l.chatID}})` to get `LinkedChatID`
- [x] store result in `l.linkedChannelID`; failure is non-fatal (log warning, continue); only log `[INFO] linked channel ID: %d` when non-zero
- [x] add `isLinkedChannel(msg *tbapi.Message) bool` helper method — returns `l.linkedChannelID != 0 && msg.SenderChat != nil && msg.SenderChat.ID == l.linkedChannelID`
- [x] write `TestTelegramListener_IsLinkedChannel` — unit test for helper: matching SenderChat, non-matching, nil SenderChat, zero linkedChannelID
- [x] run `go test -race ./app/events/ -run IsLinkedChannel` — must pass before task 2

### Task 2: Extend fromSuper check and spam skip, add integration tests

**Files:**
- Modify: `app/events/listener.go`
- Modify: `app/events/listener_test.go`

- [x] at line ~255, extend `fromSuper` assignment: `fromSuper := l.SuperUsers.IsSuper(update.Message.From.UserName, update.Message.From.ID) || l.isLinkedChannel(update.Message)`
- [x] at line ~335, extend the anonymous admin spam check skip: change `msg.SenderChat.ID == fromChat` to `msg.SenderChat.ID == fromChat || msg.SenderChat.ID == l.linkedChannelID`
- [x] update the comment to reflect both cases (group itself + linked channel)
- [x] write `TestTelegramListener_LinkedChannelBanSpam` — table-driven test with `/ban`, `/spam`, and `/warn` from linked channel (SenderChat matches LinkedChatID, From is Channel_Bot, no SuperUsers configured); verify ban request and message deletion for `/ban`, spam training for `/spam`, warning message for `/warn`
- [x] write `TestTelegramListener_LinkedChannelSkipsSpamCheck` — two subtests: linked channel message skips `OnMessage` call; non-linked channel message still runs spam check
- [x] mock `GetChatFunc` to return `tbapi.ChatFullInfo{Chat: tbapi.Chat{ID: 123}, LinkedChatID: -1001234567890}`
- [x] run tests: `go test -race ./app/events/ -run "LinkedChannel|IsLinkedChannel"` — must pass before task 3

### Task 3: Verify acceptance criteria

- [x] verify linked channel can use `/ban`, `/spam`, `/warn` via tests
- [x] verify linked channel messages skip spam check
- [x] verify non-linked channels are not affected
- [x] run full test suite: `go test -race ./...`
- [x] run linter: `golangci-lint run`
- [x] run comment normaliser: `unfuck-ai-comments run --fmt --skip=mocks ./...`

### Task 4: [Final] Update documentation

- [x] update README.md with linked channel admin feature description
- [x] update CLAUDE.md if new patterns discovered
- [x] move this plan to `docs/plans/completed/`

## Technical Details

**Data flow at startup:**
```
Do() → getChatID(Group) → chatID
     → GetChat(chatID) → ChatFullInfo.LinkedChatID → linkedChannelID
```

Note: when `Group` is a username string, `getChatID` also calls `GetChat` internally but discards `LinkedChatID`. The redundant call is acceptable — it only happens once at startup and keeps the code simple.

**Message processing — linked channel sends /ban:**
```
update.Message.From = Channel_Bot (ID 136817688)
update.Message.SenderChat = {ID: linkedChannelID, Type: "channel"}
→ fromSuper = IsSuper("Channel_Bot", 136817688) || isLinkedChannel(msg)
→ fromSuper = false || true = true
→ procSuperReply → directReport → ban target user
```

**Message processing — linked channel posts normally:**
```
→ procEvents
→ SenderChat.ID != 0 && SenderChat.ID == linkedChannelID
→ skip spam check
```
