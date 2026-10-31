---
worth: maybe
where: docker-compose-minimal.yml
added: 2026-10-31
---
# compose examples bind-mount host folders docker creates as root

`docker-compose-minimal.yml` mounts `./var:/srv/data`, and the server and psql examples mount
`./logs:/srv/logs` for tg-spam. On Linux docker creates a missing bind-mount source as root:root 755
while the image runs as uid 1000, so a fresh start of these examples hits the same restart loop as
https://github.com/umputun/tg-spam/discussions/241 (data) or #66 (logs). INSTALL.md got a mkdir + chown
step and the entrypoint warns about an unwritable data dir; these examples have neither a comment nor
a note. Undecided whether a comment line per mount is enough or the examples should change shape.
