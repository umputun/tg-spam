---
worth: yes
where: app/events/admin.go:ReportBan, app/events/reports.go reporter lists
added: 2026-10-01
---
# link labels still go through escapeMarkDownV1Text

Telegram's legacy markdown (`ModeMarkdown`) copies a link label verbatim up to the first `]` and applies
no escapes inside it ("Escaping inside entities is not allowed"). Two places still build
`[label](tg://user?id=N)` with `escapeMarkDownV1Text(label)`:

- `ReportBan` (`admin.go`), the ban notification for message senders: `banUserStr` falls back to the
  display name, so a `]` in it ends the label early and the rest of the name can inject a link.
  `@user_name` renders as `@user\_name`.
- the reporter lists in `reports.go` (`- [%s](tg://user?id=%d)`, five places): same, with the reporter
  name.

`markdownV1LinkLabel` (`events.go`) is the fix: remove `]`, escape nothing else. `ReportUserBan` and
`reportedUserMD` already use it. Left out of the check_join PR to keep it focused. Check
`extractUsername` against the rendered text when changing `ReportBan`: it reads the name back on unban.
