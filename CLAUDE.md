# tg-spam Development Guidelines

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
- Format: Use `gofmt` (enforced by linter)
- Line length: Maximum 140 characters
- Error handling: Return errors with context, use multierror for aggregation
- Naming: CamelCase for variables, PascalCase for exported types/functions
- Test tables: Use table-driven tests with descriptive test cases
- Comments: Keep in-code comments lowercase
- Documentation: All exported functions and types must be documented with standard Go comments
- Interfaces: Define interfaces in consumer packages
- Mocks: Generate with github.com/matryer/moq and store in mocks package
