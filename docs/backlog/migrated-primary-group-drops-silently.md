---
worth: maybe
where: app/events/listener.go:isChatAllowed
added: 2026-10-30
---
# migrated primary group drops every message silently

When `--telegram.group` is a numeric basic-group ID and Telegram upgrades that group to a supergroup, the
chat ID changes. Messages from the new supergroup fail `isChatAllowed` in `Do` and are dropped with no log
line, so moderation stops. At startup `getChatID` returns the numeric ID without asking Telegram, and the
`GetChat` (linked channel) and `updateSupers` calls that do reach Telegram only log a WARN that does not
carry the new ID.

Surfaced working discussion #146, whose fix covers only the admin chat: send errors there now name the new
ID. Candidate shapes, none chosen: log an ERROR with the new ID when an update from `l.chatID` carries
`Message.MigrateToChatID` (the migrate service message), or read `MigrateToChatID` from the startup
`GetChat` error. Unverified whether `getChat` on a migrated ID returns `migrate_to_chat_id`. Rare in
practice: a group with a public username is already a supergroup.
