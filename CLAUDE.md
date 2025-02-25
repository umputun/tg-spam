# tg-spam Development Guidelines

## Build & Test Commands
- Build: `make build`
- Run tests: `go test -race ./...`
- Run single test: `go test -v -race ./path/to/package -run TestName`
- Lint: `golangci-lint run`
- Coverage report: `make test`

## Libraries
- Logging: `github.com/go-pkgz/lgr`
- CLI flags: `github.com/jessevdk/go-flags`
- HTTP/REST: `github.com/go-pkgz/rest` with `github.com/go-pkgz/routegroup`
- Middleware: `github.com/didip/tollbooth/v8`
- Database: `github.com/jmoiron/sqlx` with `modernc.org/sqlite`
- Testing: `github.com/stretchr/testify`
- Color output: `github.com/fatih/color`
- For frontend, HTMX v2

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
- Comments: All exported functions and types must be documented