---
worth: yes
where: README.md
added: 2026-09-27
---
# README does not say report bans are a no-op under dry and training

User Spam Reporting says Approve Ban will "immediately ban the reported user and delete the reported
message", and Auto-Ban Threshold says the bot "will automatically delete the message and ban the user".
Since #463 neither path bans or deletes under `--dry` or `--training`, and the admin notification reads
"would have been banned (dry)" / "(training)". A rejected ban keeps the report and its buttons, adds a
"ban failed: <reason>" line and can be retried. None of that is documented.

The training section adds to the confusion: "In this mode admin can ban users manually by clicking the
"confirm ban" button" is true for the forward-to-admin flow (`admin.go`, `training: false`), but report
Approve Ban stays a hard no-ban in training by the #442 decision. An admin running `--training` clicks
Approve Ban expecting a real ban and the spam stays up.

Fix: one sentence under Approve Ban / Auto-Ban Threshold on dry/training and failure behavior, and qualify
the training paragraph so "confirm ban" names the ban-notification button, not report Approve Ban.
Surfaced reviewing #463.
