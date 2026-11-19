---
worth: maybe
where: lib/tgspam/metachecks.go:ImagesCheck
added: 2026-08-30
---
# media meta checks measure quoted text as if the sender wrote it

`ImagesCheck`, `VideosCheck` and `AudioCheck` test `req.Msg`, which `SpamFilter.OnMessage`
(`app/bot/spam.go`) has already concatenated with `Quote` or `ReplyTo.Text`. So a caption-less image,
video, or audio file posted as a reply to a long enough message clears the check's threshold
(`--min-msg-len`, or `--meta.image-text-len` for images when set) and the check does not fire: the
sender wrote nothing, but the parent message's text counts for him. `DocumentsCheck` and `ContactCheck`
already measure `req.AuthoredText()`, so the family is split today.

The cost on the other side is real: switching the remaining three to `AuthoredText()` would flag every
caption-less image, video or audio file posted as a reply to a long message, which is ordinary legitimate
behavior, creating a new false-positive ban class. A moderation decision, not a per-check patch.
