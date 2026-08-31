---
worth: later
where: app/events/listener.go:389
added: 2026-08-30
---
# intake guard drops the messages --meta.contact-only and --meta.audio-only exist to catch

The guard admits a message only on non-empty text, `Image`, `WithVideoNote`, `WithVideo`, `WithForward`
or `WithExternalReply`. `transform` has no guard entry for `WithContact` or `WithAudio`, so a bare shared
contact card, or a caption-less audio file, arrives with empty `Text`, nil `Image` and none of those flags
set, and `procEvents` returns at :391 before `Bot.OnMessage` is ever called.

The two checks written for precisely that case therefore never see it:

- `ContactCheck` (`lib/tgspam/metachecks.go:164`) flags only when `req.Meta.HasContact && req.Msg == ""`,
  so `--meta.contact-only` never fires on a non-forwarded contact card.
- `AudioCheck`'s "audio without text" branch (`metachecks.go:147`) is unreachable for a directly posted
  file; `--meta.audio-only` only ever fires on audio that has a short caption.

An operator who turns either flag on watches the spam it targets pass unflagged, with nothing in the logs
to say why.

`Meta.Giveaway` may be in the same position; not confirmed — it depends on whether a non-forwarded
giveaway message carries text.

The fix is to widen the same guard condition, which sends a new class of previously-dropped messages to
`Bot.OnMessage`, the locator, and (with CAS on by default) one CAS lookup each. That is the same trade
PR #447 made for documents, and it wants a per-check decision rather than a mechanical addition — which is
why this is `later` and best batched with the next meta-check change rather than widening the guard twice
more on its own.

Surfaced reviewing PR #447, which closes exactly this hole for documents and leaves the siblings with it.
