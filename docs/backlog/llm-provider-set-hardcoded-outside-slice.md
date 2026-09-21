---
worth: maybe
where: lib/tgspam/detector.go:Check
added: 2026-09-21
---
# a new LLM provider needs the short-message gate updated, not just the slice

`Detector.Check` builds a `llmChecks` slice that reads as the single place a provider is registered, but
the short-message eligibility block computes `openaiChecksShort` and `geminiChecksShort` as named locals
and ORs them, well before the slice exists. The early return fires only when neither is set, so a provider
added to the slice alone misses short messages exactly when no existing provider already opens that gate,
whatever its own `CheckShortMessages` setting says. With OpenAI or Gemini short-checking already on, the
new entry does run.

That conditional is what makes it a trap: it fails silently, only below `--min-msg-len`, and only on some
configurations, so the provider looks correctly wired until someone asks why short messages do not reach
it on a particular deployment.

`llmResults` is separately preallocated at capacity 2. `append` reallocates, so this is housekeeping rather
than a correctness requirement, but it encodes the provider count a second time.

Surfaced planning the jev provider (`docs/plans/20260921-jev-provider.md`), which carries explicit
checkboxes for all three sites rather than relying on the slice.

`maybe` because the obvious fix has a real cost: folding the eligibility computation into the slice means
constructing `llmChecks` before the short-message block, which moves live code that currently short-circuits
early, in exchange for removing a two-line duplication. Worth a decision, not obviously worth doing.
