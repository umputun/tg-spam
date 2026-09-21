---
worth: maybe
where: lib/tgspam/detector.go:cleanText
added: 2026-09-21
---
# cleanText deletes the quote separator instead of replacing it

`cleanText` skips every `unicode.Cc` and `unicode.Cf` rune with no replacement, and LF is in `Cc`. So the
newline that `SpamFilter.OnMessage` inserts between a post and its quoted or replied-to text is removed
rather than turned into a space, and the two run together with no boundary. Verified by running the
function: `"Спасибо!\nЗАКРЫТЫЙ КАНАЛ, вход тут"` comes out as `"Спасибо!ЗАКРЫТЫЙ КАНАЛ, вход тут"`, with
`Спасибо!ЗАКРЫТЫЙ` glued into one token.

Every consumer of `cleanMsg` sees the fused form today: the OpenAI and Gemini checkers, similarity, and
the classifier. For the LLM providers it removes the boundary a model would use to tell what the sender
wrote from what he quoted. For the classifier and similarity, `tokenize` splits on whitespace
(`strings.FieldsSeq`), so a fused pair is one token and can differ from the whitespace-separated form the
samples were built from.

Surfaced while verifying `docs/plans/20260921-jev-provider.md`, where it matters twice: the jev evaluation
fed the two joined by a newline, so its numbers do not transfer to the fused form the shipped code sends,
and any holdout must run on the fused form to be faithful.

`maybe` rather than `later` because the fix is one character and the design call is not: replacing the
newline with a space changes what every content check sees, so it needs its own before-and-after on the
classifier and similarity rather than riding along with a provider change. A paired comparison on
identical rows is the only thing that isolates the separator — a fresh run would confound it with new
traffic and labels.
