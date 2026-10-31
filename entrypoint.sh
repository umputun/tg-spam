#!/bin/sh

echo "start tg-spam"

# check if FILES_DYNAMIC is set
if [ -z "$FILES_DYNAMIC" ]; then
  echo "FILES_DYNAMIC is not set, using /srv/data"
  FILES_DYNAMIC="/srv/data"
fi

# docker creates a missing bind-mount source on the host as root:root 755, while the image runs as uid 1000.
# warn only: a root-owned dir with an existing db starts read-only and fails later, on the backup at a version change
if [ ! -w "$FILES_DYNAMIC" ]; then
  echo "error: $FILES_DYNAMIC is missing or not writable by uid $(id -u), tg-spam can't create or update its database"
  echo "error: on the host, with sudo, run: chown -R $(id -u):$(id -g) <host folder mounted to $FILES_DYNAMIC>"
fi

# ensure tg-spam.db doesn't exists before attempting to copy
if [ ! -f "$FILES_DYNAMIC/tg-spam.db" ]; then
  echo "tg-spam.db not found, copying preset files to $FILES_DYNAMIC"
  # copy preset files if tg-spam.db doesn't exist yet
  cp -r /srv/preset/* "$FILES_DYNAMIC"
fi

# show content of FILES_DYNAMIC directory
echo "content of $FILES_DYNAMIC"
ls -la "$FILES_DYNAMIC"

exec /srv/tg-spam "$@"