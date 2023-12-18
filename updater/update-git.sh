#!/bin/sh

gitrepo=$1
location=$(realpath ${2:-./samples})

if [ -z "$gitrepo" ]; then
  echo "error: you must specify a git repo url as the first argument."
  exit 1
fi

log() {
  echo "$(date +"%Y-%m-%d %H:%M:%S") $1"
}

# update the git repo if there are changes
function update() {
  git -C $location fetch
  if [ "$(git -C $location rev-parse HEAD)" != "$(git -C $location rev-parse @{u})" ]; then
    log "changes detected - updating"
    git -C $location pull
    ls -l $location
  else
    log "no changes detected in $location"
  fi
}

# check if we are running inside docker or from terminal
if [ -n "$UPDATER_IN_DOCKER" ]; then
  log "running inside docker"
elif [ -t 0 ]; then
  log "running from terminal"
else
  log "running from cron scheduler"
fi

# clone the git repo if it doesn't exist
if [ ! -d "$location/.git" ]; then
  log "cloning git repo to $location"
  git clone -q $gitrepo $location
  ls -l $location
fi

# check if we are running inside docker or from terminal
# if we are running from cron scheduler, run the updater once and exit
# if we are running inside docker, run the updater in an endless loop
if [ -n "$UPDATER_IN_DOCKER" ] || [ -t 0 ]; then
  log "running updater in endless loop, every minute"
  while true; do
    update
    sleep 1m
  done
else
  update
  exit 0
fi