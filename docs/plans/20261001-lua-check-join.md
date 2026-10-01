# Lua check_join hook: check new members on join

## Overview

Some spam accounts never post. The ad is the display name (for example `🍫❄️Coke 🍚☘️ 24/7🚘🍄`), and the
join event shows it. tg-spam does not look at the account on join: the `NewChatMembers` branch in
`TelegramListener.Do` only deletes the join message (`--delete.join-messages`) or stores it for
`--suppress-join-message`. Lua plugins already get `first_name` and `last_name`, but `check` runs only from
`Detector.Check`, which runs only on messages.

Discussion #448 settled the scope. The maintainer does not want a built-in name check, but accepted an
optional Lua hook on join (comment 18694483). The plugin author owns the policy and its false positives.
Nothing changes for an operator whose plugins do not define the hook.

The join path follows the existing reaction-ban path layer by layer: `procReaction` →
`SpamFilter.OnReaction` → `banUserOrChannel` → `admin.ReportReactionBan`.

## Context (from discovery)

- `lib/tgspam/plugin/checker.go`: one shared `*lua.LState` (`c.vm`) for all scripts. `LoadScript` checks a
  script in a temporary state, runs it in the main VM, then reads the global `check` and stores it in
  `c.checkers[name]`. `createResultCheck` looks the function up by name on every call, under `c.lock`.
- `lib/tgspam/detector.go`: `WithLuaEngine` builds `d.luaChecks` from `LuaPlugins.EnabledPlugins` (or from
  all plugins when the list is empty). It type-asserts the optional `luaResultEngine` interface; the
  exported `LuaPluginEngine` stays unchanged. `Reset` closes the engine and clears `luaChecks`.
- `app/bot/spam.go`: `SpamFilter.OnReaction` skips approved users and returns
  `Response{BanInterval: PermanentBanDuration, User, CheckResults}` on spam.
- `app/events/listener.go`: `procReaction` (line ~828) filters by `l.chatID` and superusers, then calls
  `Locator.AddSpam`, `SpamLogger.Save`, `banUserOrChannel`, `adminHandler.ReportReactionBan`.
- `app/events/admin.go`: `ReportReactionBan` (line ~94) has training and dry wording but no soft-ban
  wording. `extractUsername` reads the name from the markdown-stripped callback text; its plain pattern
  is `permanently banned (.+?) \(-?\d+\)`. For a body-less notification `getCleanMessage` returns an error
  or an empty string, so the `cleanMsg != ""` guard makes unban skip `UpdateHam` and ban confirmation skip
  `UpdateSpam`.
- `bot.User` (`app/bot/bot.go`) already has `ID`, `Username`, `DisplayName`, `FirstName`, `LastName`,
  `IsPremium`.
- Mocks: `app/bot/mocks/detector.go` (`Detector`), `app/events/mocks/bot.go` (`Bot`), generated with moq
  via `go:generate`.

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
- maintain backward compatibility: existing plugins without `check_join` behave exactly as before

## Testing Strategy

- **unit tests**: required for every task (see Development Approach above)
- **e2e tests**: none. The change has no settings and no web UI, so `e2e-ui` is not touched
- commands: `go test -race ./...`, `golangci-lint run`. Regenerate mocks with `go generate ./app/bot/...
  ./app/events/...`; never edit mock files by hand

## Progress Tracking

- mark completed items with `[x]` immediately when done
- add newly discovered tasks with ➕ prefix
- document issues/blockers with ⚠️ prefix
- update plan if implementation deviates from original scope
- keep plan in sync with actual work done

## Solution Overview

### Lua contract

A plugin can define an optional global function `check_join(req)` next to the required `check`.

- `req` has `user_id` (string), `user_name`, `first_name`, `last_name`, `is_premium`. It has no `msg` and
  no `meta`.
- The return values are the same as for `check`: spam (boolean) and details (string). A third value is
  ignored, because on join there is no message to approve.
- A plugin without `check_join` takes no part in the join check. `check` is never called on join, and no
  other detector check runs on join.
- A Lua error in `check_join` gives a response with `Error` set and `Spam` false. It never bans.
- Only enabled plugins are called, by the same rule as `check`: the plugins in
  `--lua-plugins.enabled-plugins`, or all loaded plugins when the list is empty.
- Each plugin's own `check_join` is called. A plugin cannot pick up another plugin's function through the
  shared VM.
- Dynamic reload adds, replaces or removes `check_join` the same way it does for `check`, for scripts that
  were loaded at startup. A script that fails to load keeps its previous registrations. A new file added
  at runtime is not picked up, the same as for `check` (the detector takes the plugin list once in
  `WithLuaEngine`).
- A `check_join` global that is not a function (for example a table) is ignored.
- The hook runs on the `new_chat_members` service message only. `chat_member` updates are out of scope.

No new settings. Defining `check_join` in an enabled plugin is the only switch.

### Who is checked

- Every member in `NewChatMembers`, not the sender of the service message. A member added by someone else
  is checked too.
- Only in the monitored group (`l.chatID`). Testing chats and the admin chat are skipped.
- Skipped: the bot itself (`l.BotUsername != "" && strings.EqualFold(member.UserName, l.BotUsername)`;
  without the empty guard every member without a username would be skipped), superusers (`l.SuperUsers.IsSuper`), and
  approved users (in `SpamFilter.OnJoin`). Approved matters because unban adds the user to the approved
  list, so a user banned by mistake is not banned again on rejoin.
- The check runs before the `DeleteJoinMessages` fork, so it runs whether `--delete.join-messages` and
  `--suppress-join-message` are on or off. The fork itself does not change.

### Ban and notification

- Ban goes through `banUserOrChannel` with `dry`, `training` and `restrict: l.SoftBanMode`, as for
  reactions. `Locator.AddSpam` stores the check results so the info button can show them.
  `SpamLogger.Save` logs `[join spam]`.
- `ReportReactionBan` becomes `ReportUserBan(banUserStr string, user bot.User, cause string)`, shared by
  reactions (`reaction spammer`) and joins (`on join by lua-<name>[, lua-<name>]`). The cause follows the
  user link on the first line. The first line depends on the mode:
  - training: `**[training] would have permanently banned <link> <cause>**`
  - dry: `**[dry run] would have permanently banned <link> <cause>**`
  - soft-ban: `**restricted <link> <cause>**` (new; a soft-banned reaction spammer was reported as
    permanently banned, which is not what happened)
  - otherwise: `**permanently banned <link> <cause>**`
- `extractUsername` plain pattern also accepts `restricted`, so unban keeps the approved user's name in
  soft-ban mode.
- The notification is sent only when `l.adminChatID != 0 && resp.User.ID != 0`, as in `procReaction`.
- The notification body stays empty. `getCleanMessage` returns an error or an empty string, so unban does
  not add the notification text to ham samples. The callback carries `msgID = 0`; `deleteAndBan` already skips the
  delete for 0.

## Technical Details

### Plugin engine (`lib/tgspam/plugin/checker.go`)

```go
type Checker struct {
	vm           *lua.LState
	checkers     map[string]*lua.LFunction
	joinCheckers map[string]*lua.LFunction // optional check_join functions, by script name
	...
}

// JoinCheck runs a plugin's check_join for a new chat member. The boolean is false when the plugin
// has no check_join at call time.
type JoinCheck func(req spamcheck.Request) (spamcheck.Response, bool)
```

`LoadScript`, main-VM part:

```go
c.vm.SetGlobal("check_join", lua.LNil) // a script without check_join must not inherit the previous script's
if err := c.vm.DoFile(path); err != nil {
	return fmt.Errorf("failed to load Lua script in main VM: %w", err)
}
// ... existing check lookup ...
c.checkers[name] = realCheckFunc.(*lua.LFunction)
if joinFunc, ok := c.vm.GetGlobal("check_join").(*lua.LFunction); ok {
	c.joinCheckers[name] = joinFunc
} else {
	delete(c.joinCheckers, name)
}
```

`createJoinCheck(name)`:

```go
return func(req spamcheck.Request) (spamcheck.Response, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	joinFunc, ok := c.joinCheckers[name]
	if !ok {
		return spamcheck.Response{}, false
	}
	reqTable := c.vm.NewTable()
	reqTable.RawSetString("user_id", lua.LString(req.UserID))
	reqTable.RawSetString("user_name", lua.LString(req.UserName))
	reqTable.RawSetString("first_name", lua.LString(req.FirstName))
	reqTable.RawSetString("last_name", lua.LString(req.LastName))
	reqTable.RawSetString("is_premium", lua.LBool(req.IsPremium))
	if err := c.vm.CallByParam(lua.P{Fn: joinFunc, NRet: 2, Protect: true}, reqTable); err != nil {
		return spamcheck.Response{Name: "lua-" + name, Details: "error executing lua join checker: " + err.Error(),
			Error: err}, true
	}
	isSpam := c.vm.ToBool(-2)
	details := c.vm.ToString(-1)
	c.vm.Pop(2)
	return spamcheck.Response{Name: "lua-" + name, Spam: isSpam, Details: details}, true
}
```

`GetJoinCheck(name string) (JoinCheck, error)` returns an error only when no script with that name is in
`c.checkers`. `GetAllJoinChecks() map[string]JoinCheck` returns one entry per name in `c.checkers`, so a
`check_join` added later by reload is picked up.

### Detector (`lib/tgspam/detector.go`)

```go
type luaJoinEngine interface {
	GetJoinCheck(name string) (plugin.JoinCheck, error)
	GetAllJoinChecks() map[string]plugin.JoinCheck
}

// CheckJoin runs the Lua join checks for a new chat member. It returns spam when at least one plugin
// reports spam without an error. It does not touch approved users, history or LLM checks.
func (d *Detector) CheckJoin(req spamcheck.Request) (spam bool, cr []spamcheck.Response)
```

`WithLuaEngine` fills `d.luaJoinChecks []plugin.JoinCheck` from the same plugin list as `luaChecks`, only
when the engine implements `luaJoinEngine`. A `GetJoinCheck` error fails `WithLuaEngine` the same way a
`GetResultCheck` error does. `Reset` sets `luaJoinChecks` to nil together with `luaChecks`. `CheckJoin`
takes `d.lock.RLock()`.

### Bot (`app/bot/spam.go`)

- `Detector` interface: add `CheckJoin(request spamcheck.Request) (spam bool, cr []spamcheck.Response)`.
- `func (s *SpamFilter) OnJoin(user User) Response`: approved user → `Response{}`. Otherwise build
  `spamcheck.Request{UserID, UserName, FirstName, LastName, IsPremium}` from `user` and call `CheckJoin`.
  On spam log at INFO and return `Response{BanInterval: PermanentBanDuration, User: user,
  CheckResults: results}`; otherwise `Response{CheckResults: results}`.

### Listener (`app/events/listener.go`, `app/events/events.go`)

- `Bot` interface: add `OnJoin(user bot.User) bot.Response`.
- `procJoin(ctx context.Context, msg *tbapi.Message) error`, called first in the `NewChatMembers` branch
  of `Do`; its error is logged at WARN and the existing cleanup still runs.
- `procJoin` builds `bot.User` from each `tbapi.User` member: `ID`, `Username`, trimmed `FirstName` and
  `LastName`, `IsPremium`, and `DisplayName = strings.TrimSpace(FirstName + " " + LastName)`.
- An error for one member is collected with `multierror` and the loop continues.
- The cause string is built by a small helper from the responses with `Spam && Error == nil`. The names are
  sorted (`slices.Sort`), because with an empty enabled list `luaJoinChecks` comes from a map and its order
  is random: `"on join by " + strings.Join(names, ", ")`.

## What Goes Where

- **Implementation Steps** (`[ ]` checkboxes): code, tests, README and CLAUDE.md in this repo
- **Post-Completion** (no checkboxes): manual check in a test group, the PR, the reply in discussion #448

## Implementation Steps

### Task 1: Register optional check_join in the Lua plugin engine

**Files:**
- Modify: `lib/tgspam/plugin/checker.go`
- Modify: `lib/tgspam/plugin/checker_test.go`

- [x] write failing tests in `checker_test.go`:
  - a script with `check_join` gives a `JoinCheck` that returns `(Response{Name: "lua-x", Spam, Details}, true)`
  - the request has the five user fields; `req.msg` and `req.meta` are `nil` in Lua
  - a third return value is ignored (`return true, "d", true` gives spam with details `d`)
  - a script without `check_join` gives a `JoinCheck` that returns `false`
  - shared VM: load `a.lua` with `check_join`, then `b.lua` without it; `b`'s `JoinCheck` returns `false`
    and `a`'s still works
  - each script's own function: `a.lua` and `b.lua` both define `check_join` with different details;
    after loading `b`, `a`'s `JoinCheck` still returns `a`'s details; a reload of `b` does not change `a`
  - `check_join = {}` (not a function) gives a `JoinCheck` that returns `false`
  - reload adds `check_join` to a script that had none, replaces it, and removes it when the function is
    deleted from the file; an already obtained `JoinCheck` sees each change
  - a failed reload (syntax error) keeps the previous `check_join`
  - a Lua runtime error gives `Error` set, `Spam` false, ok `true`
  - `GetJoinCheck("missing")` returns an error; `GetAllJoinChecks` has one entry per loaded script
  - `check` keeps working unchanged for a script that also defines `check_join`
- [x] run `go test -race ./lib/tgspam/plugin/...` - new tests fail
- [x] add `joinCheckers` map (init in `NewChecker`), `JoinCheck` type, the `check_join` reset and
  registration in `LoadScript`, `createJoinCheck`, `GetJoinCheck`, `GetAllJoinChecks`
- [x] update the package doc comment to describe the optional `check_join` function
- [x] run `go test -race ./lib/tgspam/plugin/...` - must pass before task 2

### Task 2: Detector.CheckJoin

**Files:**
- Modify: `lib/tgspam/detector.go`
- Modify: `lib/tgspam/plugin_test.go`

- [x] write failing tests in `plugin_test.go`:
  - with a real `plugin.Checker` and an enabled list, `CheckJoin` calls only the enabled plugins that
    have `check_join`, and returns spam when one of them reports spam
  - with an empty enabled list, all loaded plugins with `check_join` are called (assert on the set of
    response names, not their order)
  - a plugin whose `check_join` raises an error gives a response with `Error` and no spam
  - no plugin has `check_join`: `CheckJoin` returns `false` and no responses
  - a legacy engine (`legacyLuaPluginEngine`, no `luaJoinEngine`) gives no join checks
  - `CheckJoin` does not change `ApprovedUsers()` and does not call any `check`
  - `GetJoinCheck` error fails `WithLuaEngine` (fake engine implementing `luaJoinEngine`)
  - `Reset` clears the join checks
  - Lua plugins disabled: `CheckJoin` returns `false` and no responses
- [x] run `go test -race ./lib/tgspam/...` - new tests fail
- [x] add `luaJoinEngine`, `luaJoinChecks`, its filling in `WithLuaEngine`, clearing in `Reset`, and
  `CheckJoin`
- [x] run `go test -race ./lib/tgspam/...` - must pass before task 3

### Task 3: SpamFilter.OnJoin

**Files:**
- Modify: `app/bot/spam.go`
- Modify: `app/bot/mocks/detector.go` (regenerated)
- Modify: `app/bot/spam_test.go`

- [ ] add `CheckJoin` to the `Detector` interface and run `go generate ./app/bot/...`
- [ ] write failing table test `TestSpamFilterOnJoin` (shaped like `TestSpamFilterOnReaction`):
  - approved user: empty response, `CheckJoinCalls()` empty
  - not spam: zero `BanInterval`
  - spam: `PermanentBanDuration`, `User` equals the input, check results passed through
  - the request built for `CheckJoin` carries `UserID` as a decimal string, `UserName`, `FirstName`,
    `LastName`, `IsPremium`, and an empty `Msg`
- [ ] run `go test -race ./app/bot/...` - new test fails
- [ ] implement `OnJoin`
- [ ] run `go test -race ./app/bot/...` - must pass before task 4

### Task 4: Shared no-message ban notification with cause and soft-ban wording

**Files:**
- Modify: `app/events/admin.go`
- Modify: `app/events/admin_test.go`
- Modify: `app/events/listener.go` (reaction call site)
- Modify: `app/events/listener_test.go` (reaction notification test)

- [ ] write failing tests in `admin_test.go`:
  - `ReportUserBan` first line for normal, training, dry and soft-ban modes, for both causes
  - the cause is MarkdownV1-escaped (a plugin name with `_`)
  - the link sits right after `permanently banned` / `restricted`
  - callback data is `?<id>:0` and `!<id>:0`
  - `extractUsername` returns `@spammer` for `restricted @spammer (42) on join by lua-names`, and still
    returns the same results for the existing reaction cases
  - `extractUsername` on a rendered `ReportBan` text whose body contains `restricted foo (123)` still
    returns the name from the first line
  - `callbackUnbanConfirmed` on a rendered join notification calls no `UpdateHam` and adds the approved
    user with the extracted name
- [ ] run `go test -race ./app/events/...` - new tests fail
- [ ] rename `ReportReactionBan` to `ReportUserBan(banUserStr string, user bot.User, cause string)`, add
  the soft-ban case after training and dry, escape the cause
- [ ] extend the plain pattern in `extractUsername` to `(?:permanently banned|restricted) (.+?) \(-?\d+\)`
- [ ] update `procReaction` to call `ReportUserBan(banUserStr, resp.User, "reaction spammer")`; keep the
  existing reaction notification test passing (text unchanged in normal mode)
- [ ] run `go test -race ./app/events/...` - must pass before task 5

### Task 5: Join check in the listener

**Files:**
- Modify: `app/events/events.go`
- Modify: `app/events/mocks/bot.go` (regenerated)
- Modify: `app/events/listener.go`
- Modify: `app/events/listener_test.go`

- [ ] add `OnJoin(user bot.User) bot.Response` to the `Bot` interface and run `go generate ./app/events/...`
- [ ] existing `Do`-driven tests with `NewChatMembers` in the monitored chat use an empty `BotMock` and would
  panic once `OnJoin` is called: `TestTelegramListener_DoWithProcNewChatMemberMessage`,
  `TestTelegramListener_DeleteJoinMessages`, `TestTelegramListener_NoDeleteWhenFlagsDisabled`. Add
  `OnJoinFunc: func(bot.User) bot.Response { return bot.Response{} }` to each `BotMock`; re-check with
  `grep -n NewChatMembers app/events/listener_test.go`
- [ ] write failing `TestProcJoin` (shaped like `TestProcReaction`):
  - every member of a two-member `NewChatMembers` is passed to `OnJoin`, with trimmed names,
    `DisplayName` and `IsPremium` mapped
  - a member added by another user (`From` differs) is still checked
  - a join in another chat (testing chat, admin chat) does not call `OnJoin`
  - the bot itself (username matches `BotUsername`, any case) and a superuser are skipped
  - `BotUsername` empty: a member without a username is still checked
  - two plugins flag the same member: the cause lists their names sorted
  - `adminChatID == 0`: the member is banned and no notification is sent
  - not spam: no `Request` calls, no notification
  - spam: one `BanChatMemberConfig` for the member in `l.chatID`, `Locator.AddSpam` with the results,
    `SpamLogger.Save` with `[join spam]`, notification `permanently banned [...](tg://user?id=N) on join by
    lua-names` with `?N:0` and `!N:0` buttons
  - soft-ban: `RestrictChatMemberConfig` instead of a ban, notification starts with `restricted`
  - dry and training: no ban request, notification uses the dry or training wording
  - a ban error for the first member does not stop the check of the second member, and the join message
    cleanup still runs (deleted with `DeleteJoinMessages`, stored as `new_<chat>_<id>` with
    `SuppressJoinMessage`)
  - `DeleteJoinMessages` on and off: `OnJoin` is called and the join message is still deleted or stored
- [ ] run `go test -race ./app/events/...` - new tests fail
- [ ] implement `procJoin`, the cause helper, and the call at the top of the `NewChatMembers` branch in `Do`
- [ ] run `go test -race ./app/events/...` - must pass before task 6

### Task 6: Verify acceptance criteria

- [ ] verify every point of the Lua contract and "Who is checked" is covered by a test
- [ ] verify a plugin set without `check_join` gives no change in behavior on join (existing join tests
  pass unchanged apart from the added `OnJoinFunc`)
- [ ] run full test suite: `go test -race ./...`
- [ ] run linter: `golangci-lint run`
- [ ] run `go build -o tg-spam ./app`
- [ ] check coverage of the changed packages: `go test -race -coverprofile=coverage.out ./... && go tool
  cover -func=coverage.out | grep -E 'checker.go|detector.go|spam.go|listener.go|admin.go'`

### Task 7: [Final] Update documentation

- [ ] README.md, section "Lua Plugins Support": the `check_join` contract, the request fields, that it runs
  only on the `new_chat_members` service message in the monitored group, who is skipped, that a missing
  function, a non-function `check_join` or a Lua error never bans, that reload works for scripts loaded at
  startup, and a short example that checks the display name
- [ ] README.md reaction-ban paragraph (~line 266): the admin notification starts with `restricted` in
  soft-ban mode
- [ ] CLAUDE.md: add a "Lua Join Check" section under "Spam Detection Architecture" (shared-VM reset,
  optional `luaJoinEngine`, skip rules, sorted cause, `ReportUserBan` and the empty body that keeps unban
  out of ham)
- [ ] CLAUDE.md "Reaction Ban Notifications": rewrite to the current state, `ReportUserBan(…, cause)`, the
  soft-ban `restricted` line, the `(?:permanently banned|restricted)` pattern, and `getCleanMessage`
  returning an error or an empty string
- [ ] move this plan to `docs/plans/completed/`

## Post-Completion

*Items requiring manual intervention or external systems - no checkboxes, informational only*

**Manual verification**:
- in a test group, enable a plugin whose `check_join` flags a name pattern; join with a matching account
  and with a clean account; check the ban, the admin notification, and unban + rejoin (no second ban)
- repeat with `--soft-ban` and with `--dry`

**External**:
- open the PR against `umputun/tg-spam` and link it in discussion #448
