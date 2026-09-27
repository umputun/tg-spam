---
worth: yes
where: app/events/reports_test.go
added: 2026-09-27
---
# new report tests carry doc and in-body comments

#463 added multi-line doc comments to seven test functions in `reports_test.go`
(`TestUserReports_BanOutcomeReporting`, `_ReporterBanOutcome`, `_BanAndNoteFailureBothReported`,
`_BanFailureNoteAddedOnce`, `_AutoBanSuppressedModes`, `_AutoBanFailureNotification`,
`_BanFailureNoteRecognized`), plus in-body comments and one on `TestTelegramListener_CallbackErrorHidesBotToken`
in `listener_test.go`. The rule is no comments in tests except a one-line regression reason, and
`reports_test.go` had none before. Several restate the table case names and will drift.

Fix: delete them; keep a single lowercase line where a regression reason is worth recording (the
underscore-in-italics case). Surfaced reviewing #463.
