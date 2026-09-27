---
worth: yes
where: app/events/reports.go:banOutcome
added: 2026-09-27
---
# auto-ban notification wording under soft-ban is untested

Nothing checks what the auto-ban notification says with `--soft-ban`. Moving `case restricted:` above
`case banErr != nil:` in `banOutcome`, or passing `false` instead of `r.softBanMode` to `banOutcome` in
`sendAutoBanNotification` or `updateNotificationForAutoBan`, keeps the suite green. Either regression makes
a refused restrict read "user restricted after N reports", the #442 defect class. The code is correct
today; the gap is regression coverage.

Fix: in `TestUserReports_AutoBanFailureNotification`, add restrict-success and restrict-failure rows for
each sender (four rows), make the mock's `RequestFunc` fail `RestrictChatMemberConfig` as well as
`BanChatMemberConfig`, and assert the restrict request was made. Asked for as non-blocking on #463, which
merged before it was added.
