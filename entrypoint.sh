#!/bin/sh

echo "start tg-spam"

# check if FILES_DYNAMIC is set
if [ -z "$FILES_DYNAMIC" ]; then
  echo "FILES_DYNAMIC is not set, using /srv/data"
  FILES_DYNAMIC="/srv/data"
fi

if [ ! -f "$FILES_DYNAMIC/tg-spam.db" ]; then
  # fresh volume: take the whole preset, including the prebuilt db
  echo "tg-spam.db not found, copying preset files to $FILES_DYNAMIC"
  cp -r /srv/preset/* "$FILES_DYNAMIC"
else
  # A db alone is not proof that samples were ever imported into it. Volumes written by
  # older versions keep samples in text files, so the db can exist with an empty samples
  # store, and tg-spam then refuses to start:
  #   can't make spam bot, can't reload samples, no pesistent spam or ham samples found
  # Seed a sample file this volume has never seen - neither pending (.txt) nor already
  # imported (.txt.loaded) - and let the app's own migration import it and mark it.
  #
  # Samples only, never the dictionaries: importing samples deletes just the preset-origin
  # rows, so what the bot learned survives, while a dictionary import replaces the whole
  # table and would drop stop phrases added through the web api. Dictionaries are seeded
  # on a fresh volume only, in the branch above.
  for name in spam-samples.txt ham-samples.txt; do
    if [ -f "/srv/preset/$name" ] && [ ! -f "$FILES_DYNAMIC/$name" ] && [ ! -f "$FILES_DYNAMIC/$name.loaded" ]; then
      echo "$name was never imported into $FILES_DYNAMIC, seeding it from the preset"
      cp "/srv/preset/$name" "$FILES_DYNAMIC/$name"
    fi
  done
fi

# show content of FILES_DYNAMIC directory
echo "content of $FILES_DYNAMIC"
ls -la "$FILES_DYNAMIC"

exec /srv/tg-spam "$@"
