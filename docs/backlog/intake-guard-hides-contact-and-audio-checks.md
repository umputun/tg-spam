---
worth: later
where: app/events/listener.go:389
added: 2026-08-30
---
# intake guard drops the messages --meta.audio-only exists to catch

The guard admits a message only on non-empty text, `Image`, `WithVideoNote`, `WithVideo`, `WithForward`,
`WithExternalReply` or an admitted document/contact. `transform` has no guard entry for `WithAudio`, so a
caption-less audio file arrives with empty `Text`, nil `Image` and none of those flags set, and
`procEvents` returns before `Bot.OnMessage` is ever called.

`AudioCheck`'s "audio without text" branch (`metachecks.go:147`) is therefore unreachable for a directly
posted file; `--meta.audio-only` only ever fires on audio that has a short caption. An operator who turns
the flag on watches the spam it targets pass unflagged, with nothing in the logs to say why.

`Meta.Giveaway` may be in the same position; not confirmed — it depends on whether a non-forwarded
giveaway message carries text.

The fix is to widen the same guard condition, which sends a new class of previously-dropped messages to
`Bot.OnMessage`, the locator, and (with a CAS token set) one CAS lookup each. That is the same trade
PR #447 made for documents and the contact change made for `--meta.contact-only`: gate the admission on
the check's own setting rather than widening the guard unconditionally.

Surfaced reviewing PR #447, which closed this hole for documents. The contact half is closed the same way;
audio (and possibly giveaway) remain.
