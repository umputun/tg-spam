---
worth: maybe
where: app/events/listener.go:procEvents
added: 2026-08-30
---
# --meta.giveaway may never see a directly posted giveaway

The intake guard admits a text-less message only on an image, video, video note, forward, external reply,
or a document, contact or audio file whose own check is on. `transform` sets `WithGiveaway` for four
message forms (`giveaway`, `giveaway_created`, `giveaway_winners`, `giveaway_completed`), none of which
carries text, so a non-forwarded one is dropped before `Bot.OnMessage` and `GiveawayCheck` cannot fire.
Forwarded giveaways are admitted through `WithForward`, and that is the only case a test covers.

Not fixed with the audio half because the right gate is unclear: two of the four forms are service events
(created, completed), and admitting all of them on `Meta.Giveaway` would send those events to the ban
path. Settle first which of these forms Telegram actually delivers to a bot in a group and which should
be moderated, then gate admission on the setting the way `AdmitAudio` does.
