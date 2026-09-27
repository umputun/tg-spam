---
worth: yes
where: app/events/reports.go:hasBanFailureNote
added: 2026-09-27
---
# hasBanFailureNote is a free function called only from a method

`hasBanFailureNote`'s only production caller is `(*userReports).reportBanFailure`, and its companion
`banFailureNote` is already a method. Functions called only from a struct's methods must be methods, and
every other helper in `reports.go` is a `userReports` method; the package-level helpers live in
`events.go` and are shared across structs.

Fix: make it `func (r *userReports) hasBanFailureNote(text string) bool`, call it as `r.hasBanFailureNote`,
and update `TestUserReports_BanFailureNoteRecognized`. Surfaced reviewing #463.
