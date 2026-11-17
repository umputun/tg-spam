---
worth: later
where: app/events/admin.go:directReport
added: 2026-11-17
---
# admin /spam and forward notices claim a ban in dry run and training mode

In dry run, admin `/spam` (`directReport`) and admin-chat forwards (`MsgHandler`, `msgHandlerFallback`)
send the summary with "the user banned ... and message deleted" (fallback: "the user banned") and return
before deleting or banning anything. In training mode all three save the sample, `directReport` and
`MsgHandler` still delete the message, and the fallback still asks for manual deletion, but
`banUserOrChannel` returns nil without calling Telegram, so the summary still says "banned".

Deferred from the fix for https://github.com/umputun/tg-spam/discussions/326, which made these summaries
name a delete or ban Telegram rejected but kept the dry and training text unchanged by choice. Fix: word
the outcome by mode the way `userReports.banOutcome` does ("would have been banned (dry)", "(training)"),
text only, actions unchanged.
