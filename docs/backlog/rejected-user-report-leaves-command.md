---
worth: maybe
where: app/events/reports.go:DirectUserReport
added: 2026-09-24
---
# rejected user report leaves the /report command in the chat

`DirectUserReport` returns before deleting the reporter's command message when the reported message is
rejected: `From == nil` (channel or anonymous admin) and a forum topic creation message. `procUserReply`
logs the error and returns handled, and nothing deletes the command later, so `/report` (or `spam`,
`/spam`, `report`) stays visible in the chat. The rejection branches for a super-user target, an
unapproved reporter and the rate limit do delete it.

Surfaced reviewing #458. PR #457 widens the first rejection to any `SenderChat`, so every user report of
a channel post will leave its command behind until #458 lets such reports through. No functional harm;
chat clutter only.
