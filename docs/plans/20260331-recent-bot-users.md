# Recent Bot Users — show DM senders in admin UI

## Overview
- Admins often don't know their Telegram user ID, making it hard to fill in the Super Users field
- This feature makes user ID discovery trivial and user-friendly, even for non-technical admins
- A prominent button "Don't know your ID? Message the bot!" with clear instructions guides the user
- Clicking the button reveals a collapsible panel showing recent DM users with "Add" buttons
- The panel updates in real-time via SSE — when someone DMs the bot, they appear instantly
- Flow: admin clicks guide button -> sends a DM to the bot -> sees themselves appear in the panel -> clicks Add -> done

## Context (from discovery)
- files/components involved:
  - `app/events/listener.go` — `procEvents` drops private messages at `isChatAllowed` (line 296), `TelegramListener` struct (lines 29-63)
  - `app/events/events.go` — `transform()` function (line 271), raw `update.Message.Chat.Type` available but unused
  - `app/webapi/webapi.go` — routes (line 218), `Config` struct (line 56), `htmlSettingsHandler` (line 877)
  - `app/webapi/assets/settings.html` — Bot Behavior tab (line 258); currently read-only but user has an editable form with Super Users textarea (screenshots confirm)
  - `app/main.go` — listener creation (line 354), webapi config wiring (line 517)
- related patterns: HTMX partial responses with `HX-Request` header check, `rest.RenderJSON` for API responses, interfaces in webapi.Config
- dependencies: `tbapi.Update.Message.Chat.Type` provides `"private"` for DMs; `update.Message.From` has user ID/name/display name

## Design decisions
- **Storage**: in-memory only (slice + mutex), capped at 50, deduped by UserID, lost on restart (acceptable for this use case)
- **No reply to DM users**: preserves existing behaviour — bot does not respond to private messages, just silently stores sender info
- **UI placement**: below Super Users row in Bot Behavior tab, initially collapsed
- **UX approach**: guide button with friendly text, collapsible panel, clear step-by-step instructions — designed for non-technical users
- **Real-time updates**: SSE (Server-Sent Events) push new DM users to the panel instantly, no polling
- **Add button**: JS appends user ID to Super Users textarea, comma-separated, with duplicate check

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
- DM user storage: test Add (dedup, cap), List (ordering, copy safety)
- procEvents: test private chat detection path
- webapi: test new endpoint returns correct JSON, test HTMX partial rendering
- no e2e tests in this project

## Progress Tracking
- mark completed items with `[x]` immediately when done
- add newly discovered tasks with + prefix
- document issues/blockers with ! prefix
- update plan if implementation deviates from original scope

## Implementation Steps

### Task 1: Add DMUser type and in-memory storage

**Files:**
- Create: `app/events/dm_users.go`
- Create: `app/events/dm_users_test.go`

- [x] create `DMUser` struct with `UserID int64`, `UserName string`, `DisplayName string`, `Timestamp time.Time`
- [x] create `dmUsers` struct with `sync.RWMutex` and `[]DMUser` slice (RWMutex for concurrent reads in List)
- [x] implement `Add(user DMUser)` — dedup by UserID (update timestamp if exists), cap at 50 entries (drop oldest), sort by timestamp desc; after adding, notify all SSE subscribers
- [x] implement `List() []DMUser` — return a copy of the slice (already sorted by timestamp desc from Add)
- [x] implement `Subscribe() <-chan DMUser` and `Unsubscribe(<-chan DMUser)` for SSE support — fan-out new DM users to all active SSE connections
- [x] write tests for Add: basic add, dedup updates timestamp, cap at 50 drops oldest, concurrent safety
- [x] write tests for List: returns copy (modifying result doesn't affect storage), ordering
- [x] write tests for Subscribe/Unsubscribe: subscriber receives new users, unsubscribe stops delivery, multiple subscribers
- [x] run tests: `go test -race ./app/events/ -run TestDMUsers`

### Task 2: Handle private chat messages in procEvents

**Files:**
- Modify: `app/events/listener.go` — add `dmUsers` field to `TelegramListener`, add private chat detection in `procEvents`

- [x] add `dmUsers dmUsers` field to `TelegramListener` struct
- [x] add `GetDMUsers() []DMUser` exported method on `TelegramListener` that calls `l.dmUsers.List()`
- [x] add `SubscribeDMUsers() <-chan DMUser` and `UnsubscribeDMUsers(<-chan DMUser)` exported methods delegating to `l.dmUsers`
- [x] in `procEvents`, BEFORE the `isChatAllowed` check: detect `update.Message.Chat.Type == "private"`, extract user info from `update.Message.From` (ID, UserName, FirstName+LastName for display), call `l.dmUsers.Add(...)`, return nil
- [x] note: DMs intercepted earlier in the `Do` loop (super user replies, orphaned `/report`) will not be tracked — this is acceptable since those users are already known admins
- [x] write test for procEvents with a private chat message — verify it stores the user and returns nil without error
- [x] write test that private chat messages do NOT reach spam checking (no locator call, no bot check)
- [x] run tests: `go test -race ./app/events/ -run TestPrivateChat`

### Task 3: Add web API endpoint for DM users

**Files:**
- Modify: `app/webapi/webapi.go` — add `DMUsersProvider` interface to Config, add handler, register route
- Modify: `app/main.go` — wire `GetDMUsers` to webapi config

- [x] define `DMUsersProvider` interface in webapi.go: `type DMUsersProvider interface { GetDMUsers() []events.DMUser; SubscribeDMUsers() <-chan events.DMUser; UnsubscribeDMUsers(<-chan events.DMUser) }`
- [x] add `//go:generate moq --out mocks/dm_users_provider.go --pkg mocks --with-resets --skip-ensure . DMUsersProvider` directive and run `go generate`
- [x] add `DMUsersProvider DMUsersProvider` field to `Config` struct
- [x] implement `getDMUsersHandler` — if HTMX request, render `dm_users.html` partial template; if API request, return JSON via `rest.RenderJSON` (omit `when` field from JSON — return raw timestamps only, compute relative time server-side for HTMX only)
- [x] implement `sseHandler` — SSE endpoint: sets `Content-Type: text/event-stream`, subscribes to DM user events, sends each new user as an SSE event with rendered HTML row fragment, unsubscribes on client disconnect (`r.Context().Done()`)
- [x] register routes in the web UI auth group: `GET /dm-users` for HTMX/JSON, `GET /dm-users/stream` for SSE
- [x] in `main.go`, wire up: `DMUsersProvider: &tgListener` when creating webapi config (after tgListener creation)
- [x] write test for `getDMUsersHandler` JSON response with mock DMUsersProvider
- [x] write test for `getDMUsersHandler` HTMX response (HX-Request header)
- [x] write test for `sseHandler` — mock subscriber, verify SSE event format
- [x] run tests: `go test -race ./app/webapi/ -run TestDMUsers`

### Task 4: Add UI section in Bot Behavior tab

**Files:**
- Create: `app/webapi/assets/components/dm_users.html`
- Modify: `app/webapi/assets/settings.html`

- [ ] create `dm_users.html` partial template — table with columns: ID, Display Name, Username, When (relative time); each row has an "Add" button; empty state shows friendly instructions
- [ ] the partial template should format timestamps as relative (e.g. "2m ago") — compute in the Go handler and pass as a string field
- [ ] in `settings.html` Bot Behavior tab (after Super Users row): add a collapsible "Find Your User ID" section:
  - a friendly guide button: `"Don't know your ID? Message the bot!"` with an info icon
  - clicking the button reveals (Bootstrap collapse) a panel with:
    - step-by-step instructions: "1. Open Telegram 2. Find the bot (@botname) 3. Send any message 4. Your ID will appear below"
    - the DM users table container, loaded via HTMX `hx-get="/dm-users"` on reveal
    - SSE connection via HTMX `hx-ext="sse"` `sse-connect="/dm-users/stream"` for real-time updates — new rows appear automatically
- [ ] add inline `<script>` with `addSuperUser(userId)` function: reads Super Users textarea value, trims, checks if ID already present, appends with comma if needed; after adding, show brief visual feedback (e.g. button text changes to "Added!" for 2s)
- [ ] each "Add" button in the partial calls `addSuperUser(id)` via `onclick`
- [ ] SSE events append new rows to the table body using HTMX SSE swap (`sse-swap="dm-user"` on tbody)
- [ ] verify the settings handler loads the `dm_users.html` partial via `{{template}}` or as a standalone parsed template
- [ ] write test that `getDMUsersHandler` with HTMX header renders valid HTML containing expected user data
- [ ] write test that settings page Bot Behavior tab contains the collapsible section with HTMX/SSE attributes
- [ ] run tests: `go test -race ./app/webapi/ -run TestDMUsers`

### Task 5: Verify acceptance criteria

- [ ] verify DM to bot stores user info silently (no reply)
- [ ] verify admin UI shows recent DM users in Bot Behavior tab
- [ ] verify Add button appends user ID to Super Users textarea correctly
- [ ] verify dedup: same user DM twice shows once with updated timestamp
- [ ] verify cap: more than 50 DM users drops oldest
- [ ] run full test suite: `go test -race ./...`
- [ ] run linter: `golangci-lint run`
- [ ] normalize comments: `unfuck-ai-comments run --fmt --skip=mocks ./...`

### Task 6: [Final] Update documentation

- [ ] update README.md — mention the recent bot users feature in the admin UI section
- [ ] move this plan to `docs/plans/completed/`

## Technical Details

### DMUser struct
```go
type DMUser struct {
    UserID      int64
    UserName    string
    DisplayName string
    Timestamp   time.Time
}
```

### In-memory storage
```go
type dmUsers struct {
    mu          sync.RWMutex
    users       []DMUser         // sorted by Timestamp desc, capped at 50
    subscribers map[chan DMUser]struct{} // SSE subscribers, notified on Add
}
```

### Private chat detection point (in procEvents)
```go
// before isChatAllowed check
if update.Message.Chat.Type == "private" {
    from := update.Message.From
    displayName := strings.TrimSpace(from.FirstName + " " + from.LastName)
    l.dmUsers.Add(DMUser{
        UserID:      from.ID,
        UserName:    from.UserName,
        DisplayName: displayName,
        Timestamp:   time.Now(),
    })
    return nil
}
```

### API response format (JSON — no `when` field)
```json
[
  {"user_id": 12345678, "user_name": "dkrm", "display_name": "Dmitry K.", "timestamp": "2026-03-31T10:30:00Z"},
  {"user_id": 87654321, "user_name": "alice", "display_name": "Alice", "timestamp": "2026-03-31T10:15:00Z"}
]
```

### SSE event format (dm-users/stream)
```
event: dm-user
data: <tr><td>12345678</td><td>Dmitry K.</td><td>@dkrm</td><td>just now</td><td><button onclick="addSuperUser(12345678)">Add</button></td></tr>

```
Each SSE event is a rendered HTML table row that HTMX appends to the tbody via `sse-swap="dm-user"`.

### JS for Add button (with visual feedback)
```javascript
function addSuperUser(userId, btn) {
    const textarea = document.getElementById('super-users');
    const current = textarea.value.trim();
    const ids = current ? current.split(',').map(s => s.trim()) : [];
    if (ids.includes(String(userId))) {
        btn.textContent = 'Already added';
        setTimeout(() => btn.textContent = 'Add', 2000);
        return;
    }
    textarea.value = current ? current + ', ' + userId : String(userId);
    btn.textContent = 'Added!';
    btn.classList.add('btn-success');
    setTimeout(() => { btn.textContent = 'Add'; btn.classList.remove('btn-success'); }, 2000);
}
```

## Post-Completion

**Manual verification:**
- deploy to a test environment, DM the bot from a personal account, verify the user appears instantly in the admin panel via SSE
- verify the collapsible panel UX: guide button, instructions, real-time table
- verify the Add button correctly appends the ID to the Super Users textarea with visual feedback
