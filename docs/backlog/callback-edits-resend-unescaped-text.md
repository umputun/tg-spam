---
worth: yes
where: app/events/admin.go:callbackUnbanConfirmed
added: 2026-10-01
---
# callback edits re-send rendered notification text as Markdown without escaping it

The ban and report callbacks edit the admin notification by taking `query.Message.Text` and appending an
italic note (`_unbanned by X in 3s_`, `_ban confirmed by ..._`, `_rejected by ..._`). Telegram returns
`Message.Text` as rendered text with the markup stripped, and `send()` puts it back through legacy
Markdown. Any `_`, `*`, backtick or `[` in that text is then parsed as markup. With an odd count the
parse fails, `send()` logs `failed to send message as markdown` and falls back to plain text, so the
edited message shows the literal `_unbanned by admin in 3s_`. With an even count the parse succeeds and
the characters are eaten. The ban, unban or approve itself still happens; only the edit degrades.

Sites: `callbackBanConfirmed` and `callbackUnbanConfirmed` in `app/events/admin.go`, and the approve,
ban-failure, reject and ban-reporter edits in `app/events/reports.go`. `callbackShowInfo` already does it
right with `escapeMarkDownV1Text(query.Message.Text)`.

What triggers it today: a spam message body with an underscore or asterisk (escaped when first sent, so
it renders bare), and an admin username with an underscore, which goes into the italic note unescaped.

Fix: escape `query.Message.Text` with `escapeMarkDownV1Text` before appending the note at each site, and
escape `query.From.UserName` inside the note. This is safe for notifications whose link label carries a
literal `\_`: the escape turns it into `\\_`, legacy Markdown only treats a backslash as an escape before
`_`, `*`, a backtick or `[`, so the first backslash stays and the second escapes the underscore.

Do this before any change that stops escaping link labels in `ReportBan` or the reporter lists. Escaping
inside a label is wrong for legacy Markdown, but those literal backslashes are what keeps user names from
breaking the re-parse today, so removing them without this fix brings the plain-text fallback to every
ban notification for a user with an underscore in the name.
