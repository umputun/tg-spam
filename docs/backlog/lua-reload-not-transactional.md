---
worth: later
where: lib/tgspam/plugin/checker.go:64
added: 2026-08-20
---
# a failed Lua reload can still change what the previous version does

`LoadScript` validates a script in a throwaway `lua.LState` and only then runs it in the shared VM with
`c.vm.DoFile(path)`. The candidate has to run there to be registered, so a script that passes validation
and then fails partway through in the main VM has already executed whatever ran before the failure.

The two states are not equivalent: the throwaway one is bare (`RegisterHelpers` runs only on `c.vm`) and
carries none of the globals earlier scripts left behind, so passing it does not predict the main load.
A candidate that reassigns a global the previous version's `check` reads, then errors, leaves the old
registry entry in place answering differently.

`ReloadScript`'s godoc states this limit and `TestChecker_FailedReloadCanStillMutateSharedGlobals` pins
it, so the behavior is documented rather than fixed. Making it transactional means either loading each
script in its own state or snapshotting globals around the load, both of which change how scripts share
the VM — more than the issue #444 fix should carry. Surfaced by its review.
