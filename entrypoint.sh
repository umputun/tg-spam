#!/bin/sh

echo "start tg-spam"

# check if FILES_DYNAMIC is set
if [ -z "$FILES_DYNAMIC" ]; then
  echo "FILES_DYNAMIC is not set, using /srv/data"
  FILES_DYNAMIC="/srv/data"
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