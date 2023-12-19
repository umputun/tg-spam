#!/bin/sh

gitrepo=$1
location=$(realpath ${2:-./samples})
internal_location="/srv/.samples"

if [ -z "$gitrepo" ]; then
  echo "error: you must specify a git repo url as the first argument."
  exit 1
fi

log() {
  echo "$(date +"%Y-%m-%d %H:%M:%S") $1"
}

sync_files() {
  rsync -av --update --checksum --info=name --exclude='.git' --exclude=".github" --exclude='README.md' "$internal_location/" "$location/" | grep -v '^\.\/$'
}

# Clone or update the internal git repo and sync changes to $location
function sync_repo() {
  if [ ! -d "$internal_location/.git" ]; then
    log "cloning git repo to $internal_location"
    git clone -q $gitrepo $internal_location
    sync_files
  else
    git -C $internal_location fetch
    if [ "$(git -C $internal_location rev-parse HEAD)" != "$(git -C $internal_location rev-parse @{u})" ]; then
      log "changes detected - updating"
      git -C $internal_location pull
      sync_files
    fi
  fi
}

# Initial setup
log "setting up"
mkdir -p "$location"
sync_repo

# Running loop
log "running updater in endless loop, every minute"
while true; do
  sync_repo
  sleep 1m
done
