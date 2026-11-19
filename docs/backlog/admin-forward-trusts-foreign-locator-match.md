---
worth: yes
where: app/events/admin.go:MsgHandler
added: 2026-11-19
---
# admin forward acts on a locator match from another chat or sender

`MsgHandler` looks up a forwarded text with `locator.Message` and deletes `info.MsgID` and bans
`info.UserID` in the primary chat without checking either against the forward. The locator keys
messages on (gid, text hash) with an upsert, and `procEvents` writes rows for testing chats too, so:

- two users posting the same text: forwarding the earlier post deletes and bans the later sender, even
  when `ForwardOrigin` gives the earlier sender's id (`fwdID`, used only by the fallback today)
- a match stored from a `TestingIDs` chat pairs that chat's message id with `primChatID`, where the
  same id can be an unrelated message

`MsgMeta.ChatID` is stored and never read here. The fix needs a check that the match came from
`primChatID` and, when `fwdID` is known, that it equals `info.UserID`. Neither identifies the original
post for a sender who hides forwards or who repeated the same text, and on a mismatch the fallback
(ban and train without deleting) is not obviously right either, since a sender id alone does not
establish the original chat. That behavior needs its own decision and regression tests.

Surfaced by the revmux round on the backlog-sweep branch; confirmed by reading the code.
