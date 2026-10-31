---
worth: yes
where: README.md
added: 2026-10-31
---
# `user: "1000:1000"` comment implies a non-default fix

The README compose example (and `updater/docker-compose.yml`) sets `user: "1000:1000"` with a comment
saying it avoids permission issues with mounted volumes. The image already runs as uid 1000 (`USER app`
in the Dockerfile), so the line changes nothing, and it only helps when the host folder is already owned
by that uid. Reading it as the fix sent the #246 reporter to `user: "33:33"`. The comment should say the
host folder must be owned by the same uid, or the line should go.
