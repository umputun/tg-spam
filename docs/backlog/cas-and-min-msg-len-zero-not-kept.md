---
worth: later
where: app/config/settings.go:zeroAwarePaths
added: 2026-09-25
---
# disabling CAS or min-msg-len in --confdb mode is undone on load

`CAS.API = ""` and `MinMsgLen = 0` are documented ways to turn those checks off, but neither path is in
`zeroAwarePaths`, so `ApplyDefaults` puts the CLI default back on every startup and `/config/reload`
(issue #443). The fix is to register both paths.

The seeded load from #460 removed the objection raised in #443: a blob that omits `cas_api` or
`min_msg_len` now gets the default from the seed, so registering them no longer turns CAS off for an
operator who never set it.

Blocked by `cas-form-clears-on-partial-put.md`. Today `ApplyDefaults` masks a partial `PUT /config`
that clears `CAS.API`; once `CAS.API` is registered, that clear becomes permanent. Settle that first.
