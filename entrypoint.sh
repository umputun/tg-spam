#!/bin/sh

echo "start tg-spam"

# check if FILES_DYNAMIC is set
if [ -z "$FILES_DYNAMIC" ]; then
  echo "FILES_DYNAMIC is not set"
else
  # ensure tg-spam.db doesn't exists before attempting to copy
  if [ ! -f "$FILES_DYNAMIC/tg-spam.db" ]; then
    echo "tg-spam.db not found, copying preset files to $FILES_DYNAMIC"
    cp -r /srv/preset/* "$FILES_DYNAMIC"
  fi
  echo "content of $FILES_DYNAMIC"
  ls -la "$FILES_DYNAMIC"
fi

exec /srv/tg-spam "$@"