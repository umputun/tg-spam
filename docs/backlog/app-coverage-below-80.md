---
worth: later
where: app, app/storage/engine
added: 2026-10-27
---
# app and app/storage/engine test coverage is below 80%

Measured on master at 057c38e6 with `go test -cover`: `app` 68.9%, `app/storage/engine` 78.3%. Most of
the gap in `app` is `main.go` startup and wiring code. Both packages fail the 80% threshold the commit
checks enforce.
