#!/bin/sh
# Render each production compose file with a prod-like env and assert the values those
# stacks depend on. Catches lost env passthroughs and silent default drift before they
# reach a redeploy - and, since CI gates the deploy on this script, before they reach the
# bots at all.
set -eu
cd "$(dirname "$0")/../.."

fail=0
OUT=
need() {
  if ! printf '%s\n' "$OUT" | grep -Eq "$1"; then
    echo "FAIL: expected /$1/ in rendered config" >&2
    fail=1
  fi
}

renders_without_env() { # $1 = compose file
  docker compose -f "$1" config >/dev/null 2>&1
}

### support chat (docker-compose.portainer.yml)

OUT=$(docker compose --env-file deploy/wb-compat/stack-config-test.env -f docker-compose.portainer.yml config)

# image identity: the host must pull the CI-published image, never build or reuse a
# local tag - a stale local image is how a deploy silently ran old code before
need 'image: ghcr.io/(wb-aleksandr-khlebnikov|wirenboard)/tg-spam:'
need 'pull_policy: always'

# the superuser list must reach the container as an env var, and nothing may pass --super:
# that flag overrides the env var, which is how the list set in Portainer was ignored
need 'SUPER_USER:'
if printf '%s\n' "$OUT" | grep -Eq -- '--super'; then
  echo "FAIL: --super on the command line overrides SUPER_USER from the stack env" >&2
  fail=1
fi

# values the stack env pins
need 'OPENAI_MODEL: gpt-5.6-sol'
need 'MIN_PROBABILITY: "?35"?'
need 'META_LINKS_LIMIT: "?1"?'
need 'META_MENTIONS_LIMIT: "?1"?'
need 'META_GIVEAWAY: "?true"?'
need 'META_CONTACT_ONLY: "?true"?'
# values that must come from file defaults (stack env does not set them)
need 'MIN_MSG_LEN: "?40"?'
need 'SIMILARITY_THRESHOLD: "?0.65"?'
need 'FIRST_MESSAGES_COUNT: "?5"?'
need 'HISTORY_DURATION: "?72h"?'
need 'DELETE_JOIN_MESSAGES: "?true"?'
need 'DELETE_LEAVE_MESSAGES: "?true"?'
# volume identity
need 'name: tg-spam-wirenboard-chat_tg-spam-wb_data'
need 'name: tg-spam-wirenboard-chat_tg-spam-wb_log'

if renders_without_env docker-compose.portainer.yml; then
  echo "FAIL: config rendered without DATA_VOLUME_NAME/LOG_VOLUME_NAME - :? guard lost" >&2
  fail=1
fi

### update channel (deploy/wb-compat/docker-compose.update-channel.yml)

UC=deploy/wb-compat/docker-compose.update-channel.yml
OUT=$(docker compose --env-file deploy/wb-compat/update-channel-test.env -f "$UC" config)

need 'image: ghcr.io/(wb-aleksandr-khlebnikov|wirenboard)/tg-spam:'
need 'pull_policy: always'
# this bot keeps its dynamic data at the volume root, mounted at /srv/var - the mount
# point and FILES_DYNAMIC must agree or it starts on an empty database
need 'FILES_DYNAMIC: /srv/var'
need 'source: data-tg-spam'
need 'target: /srv/var'
# tuning that differs from the support chat and must not drift into its defaults
need 'SOFT_BAN: "?true"?'
need 'DISABLE_ADMIN_SPAM_FORWARD: "?true"?'
need 'MIN_MSG_LEN: "?20"?'
need 'SIMILARITY_THRESHOLD: "?0.7"?'
# the volumes belong to the stack this one replaced, so they must stay external:
# adopting them by label would fail, and a non-external declaration could delete them
need 'external: true'
need 'name: tg-antispam-update_ch_data-tg-spam'
need 'name: tg-antispam-update_ch_log-tg-spam'

if renders_without_env "$UC"; then
  echo "FAIL: update-channel config rendered without its required env - :? guards lost" >&2
  fail=1
fi

if [ "$fail" -eq 0 ]; then
  echo "stack config OK"
fi
exit "$fail"
