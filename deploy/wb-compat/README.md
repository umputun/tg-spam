# Running the WB antispam bots

Two bots run from this repository, one per chat. They share the same image and the same
update path; everything that differs between them lives in the stack environment in
Portainer, not in the code.

| | support chat | update channel |
|---|---|---|
| Portainer stack | `tg-spam-wirenboard-chat` | `tg-antispam-update_ch` |
| Telegram chat | `t.me/wirenboard`, `-1001443452450` | `-1002273392360` |
| Admin chat | `-1002485520426` | same |
| Compose file | `docker-compose.portainer.yml` | `deploy/wb-compat/docker-compose.update-channel.yml` |
| Container | `tg-spam-wb` | `tg-aspam` |
| Data volume | `tg-spam-wirenboard-chat_tg-spam-wb_data` → `/srv/data` | `tg-antispam-update_ch_data-tg-spam` → `/srv/var` |
| Log volume | `tg-spam-wirenboard-chat_tg-spam-wb_log` | `tg-antispam-update_ch_log-tg-spam` |
| Web UI | `:8081` | none |
| Ban mode | soft ban (restrict) | same |

Both pull `ghcr.io/wb-aleksandr-khlebnikov/tg-spam:master` through the stack variable
`IMAGE`. CI publishes under whichever account owns the repository, so after a move to the
wirenboard org the stacks only need `IMAGE=ghcr.io/wirenboard/tg-spam:master`. Every build
also keeps a `:<sha>` tag, so setting `IMAGE` to it pins or rolls back one stack.

## What the update channel overrides, and why

The defaults in `docker-compose.portainer.yml` are the support chat profile, so that stack
sets only credentials, volume names and the superuser list. The update channel carries
announcements with comments rather than a conversation, and overrides these - and only
these:

| Variable | Support chat | Update channel | What it changes |
|---|---|---|---|
| `OPENAI_VETO` | `false` | `true` | In veto mode the model only confirms spam another check already flagged; with it off the model flags on its own. The channel leans on the model less and pays for fewer calls |
| `MIN_MSG_LEN` | `40` | `20` | Shorter messages are skipped by the ordinary checks. Comments under a post are short, so the channel still looks at them |
| `SIMILARITY_THRESHOLD` | `0.65` | `0.7` | How close to a known spam sample a message has to be. Higher catches less and misfires less |
| `MAX_EMOJI` | `2` | `3` | |
| `DISABLE_ADMIN_SPAM_FORWARD` | `false` | `true` | The channel reports what it caught but does not copy the spam itself into the admin chat |
| `MIN_PROBABILITY`, `META_*`, `OPENAI_CHECK_SHORT_MESSAGES` | set | unset | The channel runs without the metadata checks (links, mentions, forwards, keyboards) and without the model check for short messages |
| `SERVER_ENABLED` | `true`, published on `:8081` | no web UI | The channel's samples and stop-words can only be changed through the bot itself |
| `SUPER_USER` | staff list | the same list plus one login | Both lists live in the stack environment, never in this public repository |

Three further differences are structural rather than tuning, and they are why the two
stacks still need two compose files:

- **The data volume is mounted at `/srv/var`** and `FILES_DYNAMIC=/srv/var` follows it.
  The application itself defaults to `data` relative to its working directory `/srv`,
  which is what the support chat uses. `/srv/var` is inherited from the stack this one
  replaced; nothing in the code needs it.
- **The container runs as `root`**, because the files in that volume belong to root while
  the image runs as `app` (uid 1000).
- **The stack is not deployed from git.** Its compose was pasted into the Portainer web
  editor with every value hard-coded, instead of coming from the stack environment.

Recreating that stack from `docker-compose.portainer.yml` would remove all three at once,
but its data has to reach `/srv/data` first - either by moving the files inside the volume
or by turning the mount point into a variable. A stack's repository URL cannot be changed
after creation, so moving the stack to this repository needs a recreation regardless.

## How an update reaches the bots

Push to `master` → `.github/workflows/wb-image.yml` asserts the rendered production
config, builds the image, publishes it to ghcr, and only then calls the Portainer webhook
that redeploys the stack. A failed check or build leaves `:master` on the previous image
and never reaches a bot.

Nothing is built on the deployment host. It is a 2-CPU EC2 instance: the build takes
minutes and dies on the reverse-proxy timeout, which is why builds through the Portainer
UI fail. If CI cannot publish, `deploy/wb-compat/build-image.sh` builds on the host under
the same image name and the stack is then deployed with `PULL_POLICY=never`.

To redeploy by hand: Portainer → the stack → **Pull and redeploy**. Leave Portainer's
"re-pull image" off - `pull_policy: always` in the compose already pulls.

## Rollback

Each version change makes the bot back its database up next to it as
`tg-spam.db.<version>-<timestamp>` inside the data volume (`MAX_BACKUPS=10`). To go back
to a known build, set the stack's image tag to that build's `:<sha>` and redeploy.

## Things that have already broken

- **One bot token, one process.** Telegram gives updates to a single long-polling
  consumer; a second container on the same token makes both flap on 409. Stop the old
  stack before starting a replacement.
- **Volume names are explicit and mandatory** (`${DATA_VOLUME_NAME:?}`). Compose's
  implicit names are project-prefixed, so renaming a stack silently creates empty
  volumes - that is how the learned database was lost on 2026-04-16. A deploy without
  the names fails loudly instead.
- **A database file does not mean the samples are in it.** Older versions kept samples in
  text files, so `entrypoint.sh` seeds a sample file the volume has never seen (neither
  `.txt` nor `.txt.loaded`). Without that the bot exits with "no pesistent spam or ham
  samples found in the store".
- **Importing a dictionary replaces the whole table**, unlike samples, whose import only
  clears preset-origin rows. Never seed `stop-words.txt` into a live volume: phrases added
  through the web UI would be dropped.
- **Log volumes are per bot.** The update channel runs as root, the support chat as the
  image's `app` user; pointing both at one log volume gives the app user a root-owned file
  and "can't write to log ... permission denied".

## Web UI password

Without `SERVER_AUTH_HASH` the bot generates a random password on every start and prints
it to the log. To pin one, generate a bcrypt hash and put it in the stack environment:

```sh
printf %s 'the password' | go run ./deploy/wb-compat/authhash
```

The password is read from stdin, so it stays out of shell history and the process list.
The root README suggests `htpasswd` or `mkpasswd`; this helper needs nothing but a
checked-out repository.

## Secrets

Tokens and the superuser list live in the stack environment in Portainer (and in
Bitwarden), never in this repository - it is public. The OpenAI key must come from the
shared company account, not a personal one.
