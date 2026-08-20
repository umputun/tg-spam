---
worth: yes
where: app/config/settings.go:302
added: 2026-08-20
---
# no validation for max-short-msg-count with min-msg-len at 0 or 1

`isShortMsgFlood` bails at `len([]rune(req.Msg)) >= d.MinMsgLen` (`lib/tgspam/detector.go:1131`), so with
`MinMsgLen` at 0 or 1 that condition holds for every non-empty message and short-message-flood detection
never fires, however `MaxShortMsgCount` is set.

`Validate` already guards the analogous trap one line above, rejecting `MaxShortMsgCount > 0 &&
ParanoidMode` with the comment "would silently disable this check". The `MinMsgLen` interaction has the
same shape and no rule. Reachable today in plain CLI mode with `--min-msg-len=0 --max-short-msg-count=3`,
which looks like a working config and silently is not.

Fix mirrors the existing check: reject `MaxShortMsgCount > 0 && MinMsgLen <= 1` in `Settings.Validate`,
which covers startup, `save-config`, and the web settings save boundary in one place. Note that
`min_msg_len=1` is behaviorally identical to 0 at every other call site, so the guard has to cover both
rather than just 0.

Unrelated to issue #443, found while reading that code.
