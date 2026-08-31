---
worth: maybe
where: lib/tgspam/metachecks.go:96
added: 2026-08-30
---
# media meta checks measure quoted text as if the sender wrote it

`ImagesCheck` (:96), `VideosCheck` (:119), `AudioCheck` (:142) and `ContactCheck` (:164) all test
`req.Msg`, which `SpamFilter.OnMessage` (`app/bot/spam.go`) has already concatenated with `Quote` or
`ReplyTo.Text`. So a caption-less image, video, or audio file posted as a reply to a message longer than
`--min-msg-len` arrives with `req.Msg` above the threshold and the check does not fire — the sender wrote
nothing, but the parent message's text counts for him.

`Request.AuthoredText()` exists for exactly this split and is what the prohibited-languages hard check
uses (`lib/tgspam/detector.go:288`). CLAUDE.md's "Quoted/Reply-to Text Handling" section states the
current behavior as deliberate: only the hard, LLM-unvetoable check strips the quote, and "all other
(soft, vetoable) content checks still see the full concatenated `req.Msg`". `MentionOnlyCheck`'s godoc
(:70-72) already documents the limitation in writing.

So this is a design call to revisit rather than a bug to patch, and it has a real cost on the other side:
switching these to `AuthoredText()` would flag every caption-less file posted as a reply to a long
message, which is ordinary legitimate behavior, creating a new false-positive ban class. If it is worth
changing at all it is one decision across the whole family, not a per-check edit.

Surfaced reviewing PR #447 (documents-only meta check), where codex filed it as a defect in the new
`DocumentsCheck`; verification moved it here because the new check is byte-identical to its four siblings
and conforms to the documented convention.
