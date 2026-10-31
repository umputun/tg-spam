---
worth: yes
where: INSTALL.md
added: 2026-10-31
---
# install guide installs the compose plugin but runs `docker-compose`

Step 2 installs `docker-compose-plugin` on Ubuntu and Fedora and the note at the top checks
`docker compose --version`, but Step 4 and every Common Questions answer tell the reader to run
`docker-compose` (up, logs, down, pull). The plugin provides `docker compose`, so on a host set up
by the guide itself the legacy command may not exist. Surfaced while fixing the data-dir permission
step for https://github.com/umputun/tg-spam/discussions/241.
