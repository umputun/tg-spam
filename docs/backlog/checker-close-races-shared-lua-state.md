---
worth: later
where: lib/tgspam/plugin/checker.go:200
added: 2026-08-20
---
# Checker.Close closes the shared Lua state without holding the lock

`Close` calls `c.vm.Close()` without taking `c.lock`, so it can free the shared `*lua.LState` while a
check is running on it. Every other path that touches `c.vm` serializes on that lock. `LoadScript`
takes the write lock, and the closure `createResultCheck` returns takes it for the whole Lua call, so
`Close` is the one place that does not.

`c.watcher.Stop()` on the line above closes the fsnotify watcher and returns; it does not wait for
`watchLoop` to observe `done` and exit. A tick already in `processEvents` can therefore still be inside
`ReloadScript` when `c.vm.Close()` lands.

Not reachable from tg-spam's own shutdown path today, where nothing calls `Close` while messages are
still being checked, which is why it was left. A library consumer of `lib/tgspam/plugin` driving its own
lifecycle can reach it. Surfaced by the review of the issue #444 fix.
