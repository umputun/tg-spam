# tg-spam

TG-Spam is an effective, self-hosted anti-spam bot specifically crafted for Telegram groups. Setting it up is straightforward as a Docker container, needing just a Telegram token and a group name or ID for the user to get started. Once activated, TG-Spam oversees messages, leveraging an advanced spam detection methods to pinpoint and eliminate spam content.

<div align="center">
  <img class="logo" src="logo.png" width="400px" alt="TG-Spam | Spam Hunter"/>
</div>

<div align="center">



</div>

## What is it and how it works?

TG-Spam keeps an eye on messages in Telegram groups, looking out for spam. It's quick to act, deleting spammy messages and banning the users who send them. The bot is also smart and gets smarter over time, learning from human guidance to catch new kinds of spam. It's a self-hosted tool that's pretty flexible in how you set it up, working great as a Docker container on anything from a small VPS to a Raspberry Pi. Plus, its Docker image supports various architectures like amd64, arm64, and armv7, and there are also binaries available for Linux, macOS, and Windows.

TG-Spam's spam detection algorithm is multifaceted, incorporating several criteria to ensure high accuracy and efficiency:

- **Message Analysis**: It evaluates messages for similarities to known spam, flagging those that match typical spam characteristics.
- **Integration with Combot Anti-Spam System (CAS)**: It cross-references users with the Combot Anti-Spam System, a reputable external anti-spam database.
- **Spam Message Similarity Check**: TG-Spam assesses the overall resemblance of each message to known spam patterns.
- **Stop Words Comparison**: Messages are compared against a curated list of stop words commonly found in spam.
- **OpenAI Integration**: TG-Spam may optionally use OpenAI's GPT models to analyze messages for spam patterns.
- **Emoji Count**: Messages with an excessive number of emojis are scrutinized, as this is a common trait in spam messages.
- **Automated Action**: If a message is flagged as spam, TG-Spam takes immediate action by deleting the message and banning the responsible user.

## Installation

- The primary method of installation is via Docker. TG-Spam is available as a Docker image, making it easy to deploy and run as a container. The image is available on Docker Hub at [umputun/tg-spam](https://hub.docker.com/r/umputun/tg-spam) as well as on GitHub Packages at [ghcr.io/umputun/tg-spam](https://ghcr.io/umputun/tg-spam).
- Binary releases are also available on the [releases page](https://github.com/umputun/tg-spam/releases/latest). 
- TG-Spam can be installed by cloning the repository and building the binary from source by running `make build`.
- It can also be installed using `brew tap umputun/apps && brew install umputun/apps/tg-spam` on macOS.


## Configuration

All the configuration is done via environment variables or command line arguments. Out of the box the bot has reasonable defaults, so user can run it without much hassle.

There are some mandatory parameters what has to be set:

- `--telegram.token=, [$TELEGRAM_TOKEN]` - telegram bot token. See below how to get it.
- `--telegram.group=, [$TELEGRAM_GROUP]` - group name/id. This can be a group name (for public groups it will lookg like `mygroup`) or group id (for private groups it will look like `-123456789`). To get the group id you can use [this bot](https://t.me/myidbot) or others like it.

As long as theses two parameters are set, the bot will work. Don't forget to add the bot to the group as an admin, otherwise it will not be able to delete messages and ban users.

There are some customizations available.

First of all - data files, the bot is using some data files to detect spam. They are located in the `/data` directory of the container and can be mounted from the host. The default files are:

- `spam-samples.txt` - list of spam samples
- `ham-samples.txt` - list of ham (non-spam) samples
- `exclude-tokens.txt` - list of tokens to exclude from spam detection, usually common words
- `stop-words.txt` - list of stop words to detect spam right away

User can specify custom location for them with `--files.samples-spam=, [$FILES_SAMPLES_SPAM]`, `--files.samples-ham=, [$FILES_SAMPLES_HAM]`, `--files.exclude-tokens=, [$FILES_EXCLUDE_TOKENS]`, `--files.stop-words=, [$FILES_STOP_WORDS]` parameters.

Second, are messages the bot is sending. There are three messages user may want to customize:

- `--message.startup=, [$MESSAGE_STARTUP]` - message sent to the group when bot is started, can be empty
- `--message.spam=, [$MESSAGE_SPAM]` - message sent to the group when spam detected
- `--message.dry=, [$MESSAGE_DRY]` - message sent to the group when spam detected in dry mode

By default, the bot reports back to the group with the message `this is spam` and `this is spam (dry mode)` for dry mode. In non-dry mode, the bot will delete the spam message and ban the user permanently. It is possible to suppress those reports with `--no-spam-reply, [$NO_SPAM_REPLY]` parameter. 

There are 4 files used by the bot to detect spam:

- `spam-samples.txt` - list of spam samples. Each line in this file is a full text of spam message with removed EOL. I.e. the orginal message represented as a single line. EOLs can be replaced by spaces
- `ham-samples.txt` - list of ham (non-spam) samples. Each line in this file is a full text of ham message with removed EOL
- `exclude-tokens.txt` - list of tokens to exclude from spam detection, usually common words. Each line in this file is a single token (word), or a comma-separated list of words in dbl-quotes.
- `stop-words.txt` - list of stop words to detect spam right away. Each line in this file is a single phrase (can be one or more words). The bot checks if any of those phrases are present in the message and if so, it marks the message as spam.

_The bot dynamically reloads all 4 files, so user can change them on the fly without restarting the bot._

Another useful feature is the ability to keep the list of approved users persistently. The bot will not ban those users and won't check their messages for spam because they have already passed the initial check. IDs of those users are kept in the internal list and stored in the file approved-users.txt. To enable this feature, the user must specify the file with the list of approved users with `--files.approved-users=, [$FILES_APPROVED_USERS]` parameter. The file is binary and can't be edited manually. The bot handles it automatically if the parameter is set and --paranoid mode is not enabled.

### OpenAI integration

Setting `--openai.token [$OPENAI_PROMPT]` enables OpenAI integration. All other parameters for OpenAI integration are optional and have reasonable defaults, for more details see [All Application Options](#all-application-options) section below.

To keep the number of calls low and the price manageable, the bot uses the following approach:

- Only the first message from a given user is checked for spam. If `--paranoid` mode is enabled, openai will not be used at all.
- OpenAI check is the last in the chain of checks. If any of the previous checks marked the message as spam, the bot will not call OpenAI.
- By default, OpenAI integration is disabled. 

### Admin chat/group

Optionally, user can specify the admin chat/group name/id. In this case, the bot will send a message to the admin chat as soon as a spammer is detected. Admin can see all the spam and all banned users and could also unban the user by clicking the "unban" button on the message.

To allow such a feature, `--admin.group=,  [$ADMIN_GROUP]` must be specified. This can be a group name (for public groups), but usually it is a group id (for private groups) or personal accounts.

### Updating spam and ham samples dynamically

The bot can be configured to update spam samples dynamically. To enable this feature, reporting to the admin chat must be enabled (see `--admin.group=,  [$ADMIN_GROUP]` above. If any of privileged users (`--super=, [$SUPER_USER]`) forwards a message to admin chat, the bot will add this message to the internal spam samples file (`spam-dynamic.txt`) and reload it. This allows the bot to learn new spam patterns on the fly. In addition, the bot will do the best to remove the original spam message from the group and ban the user who sent it. This is not always possible, as the forwarding strips the original user id. To address this limitation, tg-spam keeps the list of latest messages (in fact, it stores hashes) associated with the user id and the message id. This information is used to find the original message and ban the user. There are two parameters to control the lookup of the original message: `--history-duration=  (default: 1h) [$HISTORY_DURATION]` and `
--history-min-size=  (default: 1000) [$HISTORY_MIN_SIZE]`. Both define how many messages to keep in the internal cache and for how long. In other words - if the message is older than `--history-duration=` and the total number of stored messages is greater than `--history-min-size=`, the bot will remove the message from the lookup table. The reason for this is to keep the lookup table small and fast. The default values are reasonable and should work for most cases.

Updating ham samples dynamically works differently. If any of privileged users unban a message in admin chat, the bot will add this message to the internal ham samples file (`ham-dynamic.txt`), reload it and unban the user. This allows the bot to learn new ham patterns on the fly.

Note: if the bot is running in docker container, `--files.dynamic-spam=, [$FILES_DYNAMIC_SPAM]` and `--files.dynamic-ham=, [$FILES_DYNAMIC_HAM]` must be set to the mapped volume's location to stay persistent after container restart.

### Logging

The default logging prints spam reports to the console (stdout). The bot can log all the spam messages to the file as well. To enable this feature, set `--logger.enabled, [$LOGGER_ENABLED]` to `true`. By default, the bot will log to the file `tg-spam.log` in the current directory. To change the location, set `--logger.file, [$LOGGER_FILE]` to the desired location. The bot will rotate the log file when it reaches the size specified in `--logger.max-size, [$LOGGER_MAX_SIZE]` (default is 100M). The bot will keep up to `--logger.max-backups, [$LOGGER_MAX_BACKUPS]` (default is 10) of the old, compressed log files.

## Setting up the telegram bot

#### Getting the token

To get a token, talk to [BotFather](https://core.telegram.org/bots#6-botfather). All you need is to send `/newbot` command and choose the name for your bot (it must end in `bot`). That is it, and you got a token which you'll need to write down into remark42 configuration as `TELEGRAM_TOKEN`.

_Example of such a "talk"_:

```
Umputun:
/newbot

BotFather:
Alright, a new bot. How are we going to call it? Please choose a name for your bot.

Umputun:
example_comments

BotFather:
Good. Now let's choose a username for your bot. It must end in `bot`. Like this, for example: TetrisBot or tetris_bot.

Umputun:
example_comments_bot

BotFather:
Done! Congratulations on your new bot. You will find it at t.me/example_comments_bot. You can now add a description, about section and profile picture for your bot, see /help for a list of commands. By the way, when you've finished creating your cool bot, ping our Bot Support if you want a better username for it. Just make sure the bot is fully operational before you do this.

Use this token to access the HTTP API:
12345678:xy778Iltzsdr45tg
```

#### Disabling privacy mode

In some cases, for example, for private groups, bot has to have privacy mode disabled. In order to do that user need to send [BotFather](https://core.telegram.org/bots#6-botfather) the command `/setprivacy` and choose needed bot. Then choose `Disable`. Example of such conversation:

```
Umputun:
/setprivacy

BotFather:
Choose a bot to change group messages settings.

Umputun:
example_comments_bot

BotFather:
'Enable' - your bot will only receive messages that either start with the '/' symbol or mention the bot by username.
'Disable' - your bot will receive all messages that people send to groups.
Current status is: DISABLED

Umputun:
Disable

BotFather:
Success! The new status is: DISABLED. /help
```

**Important:** the privacy has to be disabled _before_ bot is added to the group. If it was done after, user should remove bot from the group and add again.


## All Application Options

```
      --admin.group=                admin group name, or channel id [$ADMIN_GROUP]
      --testing-id=                 testing ids, allow bot to reply to them [$TESTING_ID]
      --history-duration=           history duration (default: 1h) [$HISTORY_DURATION]
      --history-min-size=           history minimal size to keep (default: 1000) [$HISTORY_MIN_SIZE]
      --super=                      super-users [$SUPER_USER]
      --no-spam-reply               do not reply to spam messages [$NO_SPAM_REPLY]
      --similarity-threshold=       spam threshold (default: 0.5) [$SIMILARITY_THRESHOLD]
      --min-msg-len=                min message length to check (default: 50) [$MIN_MSG_LEN]
      --max-emoji=                  max emoji count in message, -1 to disable check (default: 2) [$MAX_EMOJI]
      --paranoid                    paranoid mode, check all messages [$PARANOID]
      --dry                         dry mode, no bans [$DRY]
      --dbg                         debug mode [$DEBUG]
      --tg-dbg                      telegram debug mode [$TG_DEBUG]

telegram:
      --telegram.token=             telegram bot token [$TELEGRAM_TOKEN]
      --telegram.group=             group name/id [$TELEGRAM_GROUP]
      --telegram.timeout=           http client timeout for telegram (default: 30s) [$TELEGRAM_TIMEOUT]
      --telegram.idle=              idle duration (default: 30s) [$TELEGRAM_IDLE]

logger:
      --logger.enabled              enable spam rotated logs [$LOGGER_ENABLED]
      --logger.file=                location of spam log (default: tg-spam.log) [$LOGGER_FILE]
      --logger.max-size=            maximum size before it gets rotated (default: 100M) [$LOGGER_MAX_SIZE]
      --logger.max-backups=         maximum number of old log files to retain (default: 10) [$LOGGER_MAX_BACKUPS]

cas:
      --cas.api=                    CAS API (default: https://api.cas.chat) [$CAS_API]
      --cas.timeout=                CAS timeout (default: 5s) [$CAS_TIMEOUT]

openai:
      --openai.token=               openai token, disabled if not set [$OPENAI_TOKEN]
      --openai.prompt=              openai system prompt, if empty uses builtin default [$OPENAI_PROMPT]
      --openai.model=               openai model (default: gpt-4) [$OPENAI_MODEL]
      --openai.max-tokens-response= openai max tokens in response (default: 1024) [$OPENAI_MAX_TOKENS_RESPONSE]
      --openai.max-tokens-request=  openai max tokens in request (default: 2048) [$OPENAI_MAX_TOKENS_REQUEST]
      --openai.max-symbols-request= openai max symbols in request, failback if tokenizer failed (default: 16000) [$OPENAI_MAX_SYMBOLS_REQUEST]

files:
      --files.samples-spam=         spam samples (default: data/spam-samples.txt) [$FILES_SAMPLES_SPAM]
      --files.samples-ham=          ham samples (default: data/ham-samples.txt) [$FILES_SAMPLES_HAM]
      --files.exclude-tokens=       exclude tokens file (default: data/exclude-tokens.txt) [$FILES_EXCLUDE_TOKENS]
      --files.stop-words=           stop words file (default: data/stop-words.txt) [$FILES_STOP_WORDS]
      --files.dynamic-spam=         dynamic spam file (default: data/spam-dynamic.txt) [$FILES_DYNAMIC_SPAM]
      --files.dynamic-ham=          dynamic ham file (default: data/ham-dynamic.txt) [$FILES_DYNAMIC_HAM]
      --files.watch-interval=       watch interval (default: 5s) [$FILES_WATCH_INTERVAL]
      --files.approved-users=       approved users file (default: data/approved-users.txt) [$FILES_APPROVED_USERS]

message:
      --message.startup=            startup message [$MESSAGE_STARTUP]
      --message.spam=               spam message (default: this is spam) [$MESSAGE_SPAM]
      --message.dry=                spam dry message (default: this is spam (dry mode)) [$MESSAGE_DRY]

Help Options:
  -h, --help                        Show this help message

```

### Application Options in details

- `super` defines the list of privileged users, can be repeated multiple times or provide as a comma-separated list in the environment. Those users are immune to spam detection and can also unban other users. All the admins of the group are privileged by default.
- `no-spam-reply` - if set to `true`, the bot will not reply to spam messages. By default, the bot will reply to spam messages with the text `this is spam` and `this is spam (dry mode)` for dry mode. In non-dry mode, the bot will delete the spam message and ban the user permanently with no reply to the group.
- `history-duration` defines how long to keep the message in the internal cache. If the message is older than this value, it will be removed from the cache. The default value is 1 hour. The cache is used to match the original message with the forwarded one. See [Updating spam and ham samples dynamically](#updating-spam-and-ham-samples-dynamically) section for more details.
- `history-min-size` defines the minimal number of messages to keep in the internal cache. If the number of messages is greater than this value, and the `history-duration` exceeded, the oldest messages will be removed from the cache.
- `--testing-id` - this is needed to debug things if something unusual is going on. All it does is adding any chat ID to the list of chats bots will listen to. This is useful for debugging purposes only, but should not be used in production. 

## Running the bot with an empty set of samples

The provided set of samples is just an example collected by the bot author. It is not enough to detect all the spam, in all groups and all languages. However, the bot is designed to learn on the fly, so it is possible to start with an empty set of samples and let the bot learn from the spam detected by humans. 

To do so, three conditions must be met:

- `--files.samples-spam [$FILES_SAMPLES_SPAM]` and `--files.samples-ham= [$FILES_SAMPLES_HAM]` must be set to the new location without any samples. In the case of docker container, those files must be mapped to the host volume.
- admin chat should be enabled, see [Admin chat/group](#admin-chatgroup) section above.
- admin name(s) should be set with `--super [$SUPER_USER]` parameter.  

After that, the moment admin run into a spam message, he could forward it to the tg-spam bot. The bot will add this message to the spam samples file, ban user and delete the message. By doing so, the bot will learn new spam patterns on the fly and eventually will be able to detect spam without admin help. Note: the only thing admin should do is to forward the message to the bot, no need to add any text or comments, or remove/ban the original spammer. The bot will do all the work.

## Example of docker-compose.yml

This is an example of a docker-compose.yml file to run the bot. It is using the latest stable version of the bot from docker hub and running as a non-root user with uid:gid 1000:1000 (matching host's uid:gid) to avoid permission issues with mounted volumes. The bot is using the host timezone and has a few super-users set. It is logging to the host directory `./log/tg-spam` and keeps all the dynamic data files in `./var/tg-spam`. The bot is using the admin chat and has a secret to protect generated links. It is also using the default set of samples and stop words.


```yaml
services:
  
  tg-spam:
    image: umputun/tg-spam:latest
    hostname: tg-spam
    restart: always
    container_name: tg-spam
    user: "1000:1000" # set uid:gid to host user to avoid permission issues with mounted volumes
    logging: &default_logging
      driver: json-file
      options:
        max-size: "10m"
        max-file: "5"
    environment:
      - TZ=America/Chicago
      - TELEGRAM_TOKEN=ххххх
      - TELEGRAM_GROUP=example_chat # public group name to monitor and protect
      - ADMIN_GROUP=-403767890 # private group id for admin spam-reports
      - LOGGER_ENABLED=true
      - LOGGER_FILE=/srv/log/tg-spam.log
      - LOGGER_MAX_SIZE=5M
      - FILES_DYNAMIC_SPAM=/srv/var/dynamic-spam.txt
      - FILES_DYNAMIC_HAM=/srv/var/dynamic-ham.txt
      - FILES_APPROVED_USERS=/srv/var/approved-users.dat
      - NO_SPAM_REPLY=true
      - DEBUG=true
    volumes:
      - ./log/tg-spam:/srv/log
      - ./var/tg-spam:/srv/var
    command: --super=name1 --super=name2 --super=name3
```

## Updating spam and ham samples from remote git repository

A small utility and docker container provided to update spam and ham samples from a remote git repository. The utility is designed to be run either as a docker container or as a standalone script or as a part of a cron job. For more details see [updater/README.md](https://github.com/umputun/tg-spam/tree/master/updater/README.md).

It also has an example of [docker-compose.yml](https://github.com/umputun/tg-spam/tree/master/updater/docker-compose.yml) to run it as a container side-by-side with the bot.

## Using tg-spam as a library

The bot can be used as a library as well. To do so, import the `github.com/umputun/tg-spam/lib` package and create a new instance of the `Detector` struct. Then, call the `Check` method with the message and userID to check. The method will return `true` if the message is spam and `false` otherwise. In addition, the `Check` method will return the list of applied rules as well as the spam-related details.

For more details, see the docs on [pkg.go.dev](https://pkg.go.dev/github.com/umputun/tg-spam/lib)

Example:

```go
package main

import (
	"io"
    "net/http"
	tgspam "github.com/umputun/tg-spam/lib"
)

func main() {
	detector := tgspam.NewDetector(tgspam.Config{
		SimilarityThreshold: 0.5,
		MinMsgLen:           50,
		MaxEmoji:            2,
		FirstMessageOnly:    false,
		HTTPClient:          &http.Client{Timeout: 30 * time.Second},
	})

	// prepare samples and exclude tokens
	spamSample := bytes.NewBufferString("this is spam\nwin a prize\n") // need io.Reader, in real life it will be a file
	hamSample := bytes.NewBufferString("this is ham\n")
	excludeTokens := bytes.NewBufferString(`"a", "the"`)

	// load samples
	detector.LoadSamples(excludeTokens, []io.Reader{spamSample}, []io.Reader{hamSample})

	isSpam, details := detector.Check("this is spam", "123456")
}
```
