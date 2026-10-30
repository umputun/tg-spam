---
worth: yes
where: README.md
added: 2026-10-30
---
# README admin group example uses a basic-group ID

The docker-compose example sets `ADMIN_GROUP=-403767890`, a basic-group ID (supergroup IDs start with
`-100`). Basic groups are the ones Telegram upgrades to supergroups, which changes the ID and breaks every
admin notification until the operator updates it (discussion #146). The example should use a supergroup ID,
and the "Admin chat/group" section could say that a basic group's ID changes when Telegram upgrades it.
