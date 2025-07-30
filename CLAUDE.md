# tg-spam Development Guidelines

## Project Overview

tg-spam is a Telegram anti-spam bot with clear separation of concerns and layered architecture.

### Component Responsibilities & Boundaries

**1. Detector (`lib/tgspam`)**
- **Responsibility**: Pure spam detection logic
- **Boundaries**: No side effects, no storage, no Telegram API knowledge
- **Inputs**: Message text, metadata (links count, has images, etc.)
- **Outputs**: Boolean (spam/ham) + array of check results
- **Key insight**: Maintains in-memory state for approved users, samples, classifier, and message history

**2. Bot/SpamFilter (`app/bot`)**
- **Responsibility**: Business logic layer between detector and application
- **Boundaries**: No direct Telegram API access, indirect storage access via interfaces
- **What it does**:
  - Wraps detector calls with bot-specific logic
  - Manages approved users list (add/remove/check)
  - Updates spam/ham samples dynamically
  - Formats responses for the listener
  - Loads samples from SamplesStore and DictStore interfaces
- **Key insight**: This is where "approved user" logic lives, NOT in detector

**3. Locator (`app/storage`)**
- **Responsibility**: Message and spam data persistence
- **Boundaries**: Pure storage, no business logic, no spam detection
- **What it stores**:
  - ALL messages (hash, user_id, msg_id, time) for later reference
  - Spam detection results linked to user_id
  - Used for: finding who sent a forwarded message, bulk deletion, analytics
- **Key insight**: Separate from detector - messages stored regardless of spam status

**4. Listener (`app/events`)**
- **Responsibility**: Orchestration and Telegram API interaction
- **Boundaries**: Only component that knows about Telegram API
- **What it does**:
  - Receives Telegram updates
  - Stores EVERY message in Locator (for forensics)
  - Calls Bot.OnMessage() for spam check
  - Executes actions based on response (ban, delete, notify)
  - Handles special cases (superusers, admin commands)
- **Key insight**: This is the conductor - it has Bot and Locator interfaces

**5. Admin Handler (`app/events/admin.go`)**
- **Responsibility**: Process admin-initiated actions
- **What it handles**:
  - `/spam` command: Manual spam marking + user ban + sample update
  - `/ban` command: Ban without updating samples  
  - `/warn` command: Delete message + warning
  - Forwarded messages to admin chat
  - Unban callbacks from admin chat buttons
- **Key insight**: Can override normal flow, operates on historical data via Locator

**6. SuperUsers (`app/events/superusers.go`)**
- **Responsibility**: Manage list of privileged users
- **What it does**:
  - Maintains list of users who can execute admin commands
  - Prevents these users from being banned or marked as spam
  - Can be specified by username or user ID
- **Key insight**: Critical security component - superuser messages bypass all spam checks

### Control Flow

**Normal Message Flow:**
1. User sends message to monitored chat (new or edited)
2. Telegram → Listener.procEvents()
3. Listener stores in Locator.AddMessage() (ALWAYS, even for spam)
4. Listener calls Bot.OnMessage()
5. Bot checks if user is approved (may skip some checks)
6. Bot calls Detector.Check() with message + metadata
7. Detector runs checks sequentially:
   - Pre-checks (approved users with FirstMessageOnly)
   - Stop words, emojis, meta checks
   - Similarity, classifier (skipped for short messages)
   - CAS API
   - OpenAI (if configured):
     - In veto mode: can override spam detection (reduce false positives)
     - In normal mode: can detect spam the other checks missed
8. Bot returns Response{Spam: bool, BanInterval, Text, DeleteReplyTo}
9. Listener acts on response:
   - If spam: ban user, delete message, notify admin chat
   - If ham: add to approved users (after N messages)

**Admin Command Flow:**
1. Admin replies to message with `/spam`
2. Listener detects superuser + reply + command
3. Calls Admin.DirectSpamReport()
4. Admin handler:
   - Removes user from approved list
   - Updates spam samples
   - Deletes original message
   - Bans user permanently
   - Sends report to admin chat

**Key Behavioral Rules:**
- Approved users still go through checks unless FirstMessageOnly=true
- Short messages skip similarity/classifier but not other checks
- Superuser messages are NEVER marked as spam
- All messages stored in Locator, even if deleted later
- Admin commands work on historical data (can ban based on old messages)
- Edited messages are treated as new messages for spam detection
- OpenAI can work in two modes: veto spam (reduce false positives) or detect spam (reduce false negatives)

## Build & Test Commands
- Build: `go build -o tg-spam ./app`
- Run tests: `go test -race ./...`
- Run single test: `go test -v -race ./path/to/package -run TestName`
- Lint: `golangci-lint run`
- Coverage report: `go test -race -coverprofile=coverage.out ./... && go tool cover -func=coverage.out`
- Normalize code comments: `command -v unfuck-ai-comments >/dev/null || go install github.com/umputun/unfuck-ai-comments@latest; unfuck-ai-comments run --fmt --skip=mocks ./...`

## Important Workflow Notes
- Always run tests, linter and normalize comments before committing
- For linter use `golangci-lint run`
- Run tests and linter after making significant changes to verify functionality
- Go version: 1.24+
- Don't add "Generated with Claude Code" or "Co-Authored-By: Claude" to commit messages or PRs
- Do not include "Test plan" sections in PR descriptions
- Do not add comments that describe changes, progress, or historical modifications. Avoid comments like “new function,” “added test,” “now we changed this,” or “previously used X, now using Y.” Comments should only describe the current state and purpose of the code, not its history or evolution.
- Use `go:generate` for generating mocks, never modify generated files manually. Mocks are generated with `moq` and stored in the `mocks` package.
- After important functionality added, update README.md accordingly
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
- Line length: Maximum 140 characters
- Error handling: Return errors with context, use multierror for aggregation
- Naming: CamelCase for variables, PascalCase for exported types/functions
- Test tables: Use table-driven tests with descriptive test cases
- Comments: Keep in-code comments lowercase
- Documentation: All exported functions and types must be documented with standard Go comments
- Interfaces: Define interfaces in consumer packages
- Mocks: Generate with github.com/matryer/moq and store in mocks package
