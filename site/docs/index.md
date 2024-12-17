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
- **Meta checks**: TG-Spam can optionally check the message for the number of links and the presence of images. If the number of links is greater than the specified limit, or if the message contains images but no text, it will be marked as spam.
- **Automated Action**: If a message is flagged as spam, TG-Spam takes immediate action by deleting the message and banning the responsible user.

TG-Spam can also run as a server, providing a simple HTTP API to check messages for spam. This is useful for integration with other tools, not related to Telegram. For more details see [Running with webapi server](#running-with-webapi-server) section below. In addition, it provides WEB UI to perform some useful admin tasks. For more details see [WEB UI](#web-ui) section below. All the spam detection modules can be also used as a library. For more details see [Using tg-spam as a library](#using-tg-spam-as-a-library) section below.

## Installation

- The primary method of installation is via Docker. TG-Spam is available as a Docker image, making it easy to deploy and run as a container. The image is available on Docker Hub at [umputun/tg-spam](https://hub.docker.com/r/umputun/tg-spam) as well as on GitHub Packages at [ghcr.io/umputun/tg-spam](https://ghcr.io/umputun/tg-spam).
- Binary releases are also available on the [releases page](https://github.com/umputun/tg-spam/releases/latest).
- TG-Spam can be installed by cloning the repository and building the binary from source by running `make build`.
- It can also be installed using `brew tap umputun/apps && brew install umputun/apps/tg-spam` on macOS.

**Install guide for non-technical users is available [here](/INSTALL.md)**

## Configuration

All the configuration is done via environment variables or command line arguments. Out of the box the bot has reasonable defaults, so user can run it without much hassle.

There are some mandatory parameters what has to be set:

- `--telegram.token=, [$TELEGRAM_TOKEN]` - telegram bot token. See below how to get it.
- `--telegram.group=, [$TELEGRAM_GROUP]` - group name/id. This can be a group name (for public groups it will look like `mygroup`) or group id (for private groups it will look like `-123456789`). To get the group id you can use [this bot](https://t.me/myidbot) or others like it.

As long as theses two parameters are set, the bot will work. Don't forget to add the bot to the group as an admin, otherwise it will not be able to delete messages and ban users.

There are some important customizations available:

First of all - sample files, the bot is using some data files to detect spam. They are located in the `/srv/data` directory of the container and can be mounted from the host. The files are: `spam-samples.txt`, `ham-samples.txt`, `exclude-tokens.txt` and `stop-words.txt`.

User can specify custom location for them with `--files.samples=, [$FILES_SAMPLES]` parameters. This should be a directory, where all the files are located.

Second, are messages the bot is sending. There are three messages user may want to customize:

- `--message.startup=, [$MESSAGE_STARTUP]` - message sent to the group when bot is started, can be empty
- `--message.spam=, [$MESSAGE_SPAM]` - message sent to the group when spam detected
- `--message.dry=, [$MESSAGE_DRY]` - message sent to the group when spam detected in dry mode

By default, the bot reports back to the group with the message `this is spam` and `this is spam (dry mode)` for dry mode. In non-dry mode, the bot will delete the spam message and ban the user permanently. It is possible to suppress those reports with `--no-spam-reply, [$NO_SPAM_REPLY]` parameter.

There are 4 files used by the bot to detect spam:

- `spam-samples.txt` - list of spam samples. Each line in this file is a full text of spam message with removed EOL. I.e. the original message represented as a single line. EOLs can be replaced by spaces
- `ham-samples.txt` - list of ham (non-spam) samples. Each line in this file is a full text of ham message with removed EOL
- `exclude-tokens.txt` - list of tokens to exclude from spam detection, usually common words. Each line in this file is a single token (word), or a comma-separated list of words in dbl-quotes.
- `stop-words.txt` - list of stop words to detect spam right away. Each line in this file is a single phrase (can be one or more words). The bot checks if any of those phrases are present in the message and if so, it marks the message as spam.

_The bot dynamically reloads all 4 files, so user can change them on the fly without restarting the bot._

Another useful feature is the ability to keep the list of approved users persistently and keep other meta-information about detected spam and received messages. The bot will not ban approved users and won't check their messages for spam because they have already passed the initial check. All this info is stored in the internal storage under `--files.dynamic =, [$FILES_DYNAMIC]` directory. User should mount this directory from the host to keep the data persistent. All the files in this directory are handled by bot automatically.

### Configuring spam detection modules and parameters

**Message Analysis**

This is the main spam detection module. It uses the list of spam and ham samples to detect spam by using Bayes classifier. The bot is enabled as long as `--files.samples=, [$FILES_SAMPLES]`, point to existing directory with all the sample files (see above). There is also a parameter to set minimum spam probability percent to ban the user. If the probability of spam is less than `--min-probability=, [$MIN_PROBABILITY]` (default is 50), the message is not marked as spam.

The analysis is active only if both ham and spam samples files are present and not empty.

**Spam message similarity check**

This check uses provides samples files and active by default. The bot compares the message with the samples and if the similarity is greater than `--similarity-threshold=, [$SIMILARITY_THRESHOLD]` (default is 0.5), the message is marked as spam. Setting the similarity threshold to 1 will effectively disable this check.

**Stop Words Comparison**

If stop words file is present, the bot will check the message for the presence of the phrases in the file. The bot is enabled as long as `stop-words.txt` file is present in samples directory and not empty.

**Combot Anti-Spam System (CAS) integration**

Nothing needed to enable CAS integration, it is enabled by default. To disable it, set `--cas.api=, [$CAS_API]` to empty string.

**OpenAI integration**

Setting `--openai.token [$OPENAI_TOKEN]` enables OpenAI integration. All other parameters for OpenAI integration are optional and have reasonable defaults, for more details see [All Application Options](#all-application-options) section below.

To keep the number of calls low and the price manageable, the bot uses the following approach:

- Only the first message(s) from a given user is checked for spam. If `--paranoid` mode is enabled, openai will not be used at all.
- OpenAI check is the last in the chain of checks. By default (if `--openai.veto` is not set), the bot will not even call OpenAI if any of the previous checks marked the message as spam. This default mode makes spam detection stricter, helping detect more spam messages that otherwise could have slipped through the cracks.
- Setting `--openai.veto` changes the workflow. In veto mode, OpenAI is called *only* if the message is classified as spam by other checks. The message is considered spam only if OpenAI confirms the decision. This helps reduce the number of false positives, making spam detection more careful.
- By default, OpenAI integration is disabled.

**Emoji Count**

If the number of emojis in the message is greater than `--max-emoji=, [$MAX_EMOJI]` (default is 2), the message is marked as spam. Setting the max emoji count to -1 will effectively disable this check. Note: setting it to 0 will mark all the messages with any emoji as spam.

**Minimum message length**

This is not a separate check, but rather a parameter to control the minimum message length. If the message length is less than `--min-msg-len=, [$MIN_MSG_LEN]` (default is 50), the message won't be checked for spam. Setting the min message length to 0 will effectively disable this check. This check is needed to avoid false positives on short messages.

**Maximum links in message**

This option is disabled by default. If set to a positive number, the bot will check the message for the number of links. If the number of links is greater than `--meta.links-limit=, [$META_LINKS_LIMIT]` (default is -1), the message will be marked as spam. Setting the limit to -1 will effectively disable this check.

**Links only check**

This option is disabled by default. If set to `true`, the bot will check the message for the presence of any text. If the message contains links but no text, it will be marked as spam.

**Image only check**

This option is disabled by default. If set to `true`, the bot will check the message for the presence of any image. If the message contains images but no text, it will be marked as spam.

**Video only check**

This option is disabled by default. If set to `true`, the bot will check the message for the presence of any video or video notes. If the message contains videos but no text, it will be marked as spam.

**Forward check**

This option is disabled by default. If `--meta.forward` set or `env:META_FORWARD` is `true`, the bot will check if the message forwarded. If the message is a forward, it will be marked as spam.

**Multi-language words**

Using words that mix characters from multiple languages is a common spam technique. To detect such messages, the bot can check the message for the presence of such words. This option is disabled by default and can be enabled with the `--multi-lang=, [$MULTI_LANG]` parameter. Setting it to a number above `0` will enable this check, and the bot will mark the message as spam if it contains words with characters from more than one language in more than the specified number of words.


### Admin chat/group

Optionally, user can specify the admin chat/group name/id. In this case, the bot will send a message to the admin chat as soon as a spammer is detected. Admin can see all the spam and all banned users and could also unban the user, confirm the ban or get results of spam checks by clicking a button directly on the message.

To allow such a feature, `--admin.group=,  [$ADMIN_GROUP]` must be specified. This can be a group name (for public groups), but usually it is a group id (for private groups) or personal accounts.

<details markdown>
  <summary>Screenshots</summary>

![ban-report](https://github.com/umputun/tg-spam/raw/master/site/docs/ban-report.png)

![change-ban](https://github.com/umputun/tg-spam/raw/master/site/docs/change-ban.png)

![unban-confirmation](https://github.com/umputun/tg-spam/raw/master/site/docs/unban-confirmation.png)
</details>

**admin commands**

* Admins can reply to the spam message with the text `spam` or `/spam` to mark it as spam. This is useful for training purposes as the bot will learn from the spam messages marked by the admin and will be able to detect similar spam in the future.

* Replying to the message with the text `ban` or `/ban` will ban the user who sent the message. This is useful for post-moderation purposes. Essentially this is the same as sending `/spam` but without adding the message to the spam samples file.

* Replying to the message with the text `warn` or `/warn` will remove the original message, and send a warning message to the user who sent the message. This is useful for post-moderation purposes. The warning message is defined by `--message.warn=, [$MESSAGE_WARN]` parameter.


### Updating spam and ham samples dynamically

The bot can be configured to update spam samples dynamically. To enable this feature, reporting to the admin chat must be enabled (see `--admin.group=,  [$ADMIN_GROUP]` above. If any of privileged users (`--super=, [$SUPER_USER]`) forwards a message to admin chat or reply to the message with `/spam` or `spam` text, the bot will add this message to the internal spam samples file (`spam-dynamic.txt`) and reload it. This allows the bot to learn new spam patterns on the fly. In addition, the bot will do the best to remove the original spam message from the group and ban the user who sent it. This is not always possible, as the forwarding strips the original user id. To address this limitation, tg-spam keeps the list of latest messages (in fact, it stores hashes) associated with the user id and the message id. This information is used to find the original message and ban the user. There are two parameters to control the lookup of the original message: `--history-duration=  (default: 1h) [$HISTORY_DURATION]` and `
--history-min-size=  (default: 1000) [$HISTORY_MIN_SIZE]`. Both define how many messages to keep in the internal cache and for how long. In other words - if the message is older than `--history-duration=` and the total number of stored messages is greater than `--history-min-size=`, the bot will remove the message from the lookup table. The reason for this is to keep the lookup table small and fast. The default values are reasonable and should work for most cases.

Updating ham samples dynamically works differently. If any of privileged users unban a message in admin chat, the bot will add this message to the internal ham samples file (`ham-dynamic.txt`), reload it and unban the user. This allows the bot to learn new ham patterns on the fly.

Both dynamic spam and ham files are located in the directory set by `--files.dynamic=, [$FILES_DYNAMIC]` parameter. User should mount this directory from the host to keep the data persistent.

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
      --disable-admin-spam-forward  disable handling messages forwarded to admin group as spam [$DISABLE_ADMIN_SPAM_FORWARD]
      --testing-id=                 testing ids, allow bot to reply to them [$TESTING_ID]
      --history-duration=           history duration (default: 24h) [$HISTORY_DURATION]
      --history-min-size=           history minimal size to keep (default: 1000) [$HISTORY_MIN_SIZE]
      --super=                      super-users [$SUPER_USER]
      --no-spam-reply               do not reply to spam messages [$NO_SPAM_REPLY]
      --suppress-join-message       delete join message if user is kicked out [$SUPPRESS_JOIN_MESSAGE]
      --similarity-threshold=       spam threshold (default: 0.5) [$SIMILARITY_THRESHOLD]
      --min-msg-len=                min message length to check (default: 50) [$MIN_MSG_LEN]
      --max-emoji=                  max emoji count in message, -1 to disable check (default: 2) [$MAX_EMOJI]
      --min-probability=            min spam probability percent to ban (default: 50) [$MIN_PROBABILITY]
      --multi-lang=                 number of words in different languages to consider as spam (default: 0) [$MULTI_LANG]
      --paranoid                    paranoid mode, check all messages [$PARANOID]
      --first-messages-count=       number of first messages to check (default: 1) [$FIRST_MESSAGES_COUNT]
      --training                    training mode, passive spam detection only [$TRAINING]
      --soft-ban                    soft ban mode, restrict user actions but not ban [$SOFT_BAN]
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

meta:
      --meta.links-limit=           max links in message, disabled by default (default: -1) [$META_LINKS_LIMIT]
      --meta.image-only             enable image only check [$META_IMAGE_ONLY]
      --meta.links-only             enable links only check [$META_LINKS_ONLY]
      --meta.video-only             enable video only check [$META_VIDEO_ONLY]
      --meta.forward                enable forward check [$META_FORWARD]

openai:
      --openai.token=               openai token, disabled if not set [$OPENAI_TOKEN]
      --openai.apibase=             custom openai API base, default is https://api.openai.com/v1 [$OPENAI_API_BASE]
      --openai.veto                 veto mode, confirm detected spam [$OPENAI_VETO]
      --openai.prompt=              openai system prompt, if empty uses builtin default [$OPENAI_PROMPT]
      --openai.model=               openai model (default: gpt-4o-mini) [$OPENAI_MODEL]
      --openai.max-tokens-response= openai max tokens in response (default: 1024) [$OPENAI_MAX_TOKENS_RESPONSE]
      --openai.max-tokens-request=  openai max tokens in request (default: 2048) [$OPENAI_MAX_TOKENS_REQUEST]
      --openai.max-symbols-request= openai max symbols in request, failback if tokenizer failed (default: 16000) [$OPENAI_MAX_SYMBOLS_REQUEST]
      --openai.retry-count=         openai retry count (default: 1) [$OPENAI_RETRY_COUNT]

files:
      --files.samples=              samples data path (default: data) [$FILES_SAMPLES]
      --files.dynamic=              dynamic data path (default: data) [$FILES_DYNAMIC]
      --files.watch-interval=       watch interval for dynamic files (default: 5s) [$FILES_WATCH_INTERVAL]

message:
      --message.startup=            startup message [$MESSAGE_STARTUP]
      --message.spam=               spam message (default: this is spam) [$MESSAGE_SPAM]
      --message.dry=                spam dry message (default: this is spam (dry mode)) [$MESSAGE_DRY]
      --message.warn=               warning message (default: You've violated our rules and this is your first and last warning. Further violations will lead to permanent access denial. Stay
                                    compliant or face the consequences!) [$MESSAGE_WARN]

server:
      --server.enabled              enable web server [$SERVER_ENABLED]
      --server.listen=              listen address (default: :8080) [$SERVER_LISTEN]
      --server.auth=                basic auth password for user 'tg-spam' (default: auto) [$SERVER_AUTH]
      --server.auth-hash=           basic auth password hash for user 'tg-spam' [$SERVER_AUTH_HASH]

Help Options:
  -h, --help                        Show this help message

```

### Application Options in details

- `super` defines the list of privileged users, can be repeated multiple times or provide as a comma-separated list in the environment. Those users are immune to spam detection and can also unban other users. All the admins of the group are privileged by default.
- `no-spam-reply` - if set to `true`, the bot will not reply to spam messages. By default, the bot will reply to spam messages with the text `this is spam` and `this is spam (dry mode)` for dry mode. In non-dry mode, the bot will delete the spam message and ban the user permanently with no reply to the group.
- `history-duration` defines how long to keep the message in the internal cache. If the message is older than this value, it will be removed from the cache. The default value is 1 hour. The cache is used to match the original message with the forwarded one. See [Updating spam and ham samples dynamically](#updating-spam-and-ham-samples-dynamically) section for more details.
- `history-min-size` defines the minimal number of messages to keep in the internal cache. If the number of messages is greater than this value, and the `history-duration` exceeded, the oldest messages will be removed from the cache.
- `suppress-join-message` - if set to `true`, the bot will delete the join message from the group if the user is kicked out. This is useful to keep the group clean from spam messages.
- `--testing-id` - this is needed to debug things if something unusual is going on. All it does is adding any chat ID to the list of chats bots will listen to. This is useful for debugging purposes only, but should not be used in production.
- `--paranoid` - if set to `true`, the bot will check all the messages for spam, not just the first one. This is useful for testing and training purposes.
- `--first-messages-count` - defines how many messages to check for spam. By default, the bot checks only the first message from a given user. However, in some cases, it is useful to check more than one message. For example, if the observed spam starts with a few non-spam messages, the bot will not be able to detect it. Setting this parameter to a higher value will allow the bot to detect such spam. Note: this parameter is ignored if `--paranoid` mode is enabled.
- `--training` - if set, the bot will not ban users and delete messages but will learn from them. This is useful for training purposes.
- `--soft-ban` - if set, the bot will restrict user actions but won't ban. This is useful for chats where the false-positive is hard or costly to recover from. With soft ban, the user won't be removed from the chat but will be restricted in actions. Practically, it means the user won't be able to send messages, but the recovery is easy - just unban the user, and they won't need to rejoin the chat.
- `--disable-admin-spam-forward` - if set to `true`, the bot will not treat messages forwarded to the admin chat as spam.
- `--dry` - if set to `true`, the bot will not ban users and delete messages. This is useful for testing purposes.
- `--dbg` - if set to `true`, the bot will print debug information to the console.
- `--tg-dbg` - if set to `true`, the bot will print debug information from the telegram library to the console.

## Running the bot with an empty set of samples

The provided set of samples is just an example collected by the bot author. It is not enough to detect all the spam, in all groups and all languages. However, the bot is designed to learn on the fly, so it is possible to start with an empty set of samples and let the bot learn from the spam detected by humans.

To do so, several conditions must be met:

- `--files.samples [$FILES_SAMPLES]` must be set to the new location (directory) without `spam-samples.txt` and `ham-samples.txt` files.
- `--files.dynamic [$FILES_DYNAMIC]` must be set to the new location (directory) where the bot will keep all the dynamic data files. In the case of docker container, this directory must be mapped to the host volume.
- admin chat should be enabled, see [Admin chat/group](#admin-chatgroup) section above.
- admin name(s) should be set with `--super [$SUPER_USER]` parameter.

After that, the moment admin run into a spam message, he could forward it to the tg-spam bot. The bot will add this message to the spam samples file, ban user and delete the message. By doing so, the bot will learn new spam patterns on the fly and eventually will be able to detect spam without admin help. Note: the only thing admin should do is to forward the message to the bot, no need to add any text or comments, or remove/ban the original spammer. The bot will do all the work.

### Training the bot on a live system safely

In case if such an active training on a live system is not possible, the bot can be trained without banning user and deleting messages automatically. Setting `--training ` parameter will disable banning and deleting messages by bot right away, but the rest of the functionality will be the same. This is useful for testing and training purposes as bot can be trained on false-positive samples, by unbanning them in the admin chat as well as with false-negative samples by forwarding them to the bot. Alternatively, admin can reply to the spam message with the text `spam` or `/spam` to mark it as spam.

In this mode admin can ban users manually by clicking the "confirm ban" button on the message. This allows running the bot as a post-moderation tool and training it on the fly.

Pls note: Missed spam messages forwarded to the admin chat will be removed from the primary chat group and the user will be banned.

## Running with webapi server

The bot can be run with a webapi server. This is useful for integration with other tools. The server is disabled by default, to enable it pass `--server.enabled [$SERVER_ENABLED]`. The server will listen on the port specified by `--server.listen [$SERVER_LISTEN]` parameter (default is `:8080`).

By default, the server is protected by basic auth with user `tg-spam` and randomly generated password. This password and the hash are printed to the console on startup. If user wants to set a custom auth password, it can be done with `--server.auth [$SERVER_AUTH]` parameter. Setting it to empty string will disable basic auth protection. 

For better security, it is possible to set the password hash instead, with `--server.auth-hash [$SERVER_AUTH_HASH]` parameter. The hash should be generated with any command what can make bcrypt hash. For example, the following command will generate a hash for the password `your_password`: `htpasswd -n -B -b tg-spam your_password | cut -d':' -f2`

alternatively, it is possible to use one of the following commands to generate the hash:
```
htpasswd -bnBC 10 "" your_password | tr -d ':\n'
mkpasswd --method=bcrypt your_password
openssl passwd -apr1 your_password

```

In case if both `--server.auth` and `--server.auth-hash` are set, the hash will be used.


It is truly a **bad idea** to run the server without basic auth protection, as it allows adding/removing users and updating spam samples to anyone who knows the endpoint. The only reason to run it without protection is inside the trusted network or for testing purposes.  Exposing the server directly to the internet is not recommended either, as basic auth is not secure enough if used without SSL. It is better to use a reverse proxy with TLS termination in front of the server.

**endpoints:**

- `GET /ping` - returns `pong` if the server is running

- `POST /check` - return spam check result for the message passed in the body. The body should be a json object with the following fields:
  - `msg` - message text
  - `user_id` - user id
  - `user_name` - username

- `POST /update/spam` - update spam samples with the message passed in the body. The body should be a json object with the following fields:
  - `msg` - spam text

- `POST /update/ham` - update ham samples with the message passed in the body. The body should be a json object with the following fields:
  - `msg` - ham text

- `POST /delete/spam` - delete spam samples with the message passed in the body. The body should be a json object with the following fields:
  - `msg` - spam text

- `POST /delete/ham` - delete ham samples with the message passed in the body. The body should be a json object with the following fields:
  - `msg` - ham text

- `POST /users/add` - add user to the list of approved users. The body should be a json object with the following fields:
  - `user_id` -  user id to add
  - `user_name` - username, used for user_id lookup if user_id is not set


- `POST /users/delete` - remove user from the list of approved users. The body should be a json object with the following fields:
  - `user_id` -  user id to add
  - `user_name` - username, used for user_id lookup if user_id is not set

- `GET /users` - get the list of approved users. The response is a json object with the following fields:
  - `user_ids` - array of user ids

- `GET /samples` - get the list of spam and ham samples. The response is a json object with the following fields:
  - `spam` - array of spam samples
  - `ham` - array of ham samples

- `PUT /samples` - reload dynamic samples

- `GET /settings` - return the current settings of the bot

_for the real examples of http requests see [webapp.rest](https://github.com/umputun/tg-spam/blob/master/webapp.rest) file._

**how it works**

The server is using the same spam detection logic as the bot itself. It is using the same set of samples and the same set of parameters. The only difference is that the server is not banning users and deleting messages. It also doesn't assume any particular flow user should follow. For example, the `/check` api call doesn't update dynamic spam/ham samples automatically.

However, if users want to update spam/ham dynamic samples, they should call the corresponding endpoint `/update/<spam|ham>`. On the other hand, updating the approved users list is a part of the `/check` api call, so user doesn't need to call it separately. In case if the list of approved users should be managed by the client application, it is possible to call `/users` endpoints directly.

Generally, this is a very basic server, but should be sufficient for most use cases. If a user needs more functionality, it is possible to run the bot [as a library](#using-tg-spam-as-a-library) and implement custom logic on top of it.

See also [examples](https://github.com/umputun/tg-spam/tree/master/_examples/) for small but complete applications using the bot as a library.

### WEB UI

If webapi server enabled (see [Running with webapi server](#running-with-webapi-server) section above), the bot will serve a simple web UI on the root path. It is a basic UI to check a message for spam, manage samples and handle approved users. It is protected by basic auth the same way as webapi server.


<details markdown>
  <summary>Screenshots</summary>

![msg-checker](https://github.com/umputun/tg-spam/raw/master/site/docs/msg-checker.png)

![manage-samples](https://github.com/umputun/tg-spam/raw/master/site/docs/manage-samples.png)

![manage-users](https://github.com/umputun/tg-spam/raw/master/site/docs/manage-users.png)
</details>

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
      - FILES_DYNAMIC=/srv/var
      - NO_SPAM_REPLY=true
      - DEBUG=true
    volumes:
      - ./log/tg-spam:/srv/log
      - ./var/tg-spam:/srv/var
    command: --super=name1 --super=name2 --super=name3
```

## Getting spam samples from CAS

CAS provide an API to get spam samples, which can be used to create a set of spam samples for the bot. Provided [`cas-export.sh`](https://raw.githubusercontent.com/umputun/tg-spam/master/cas-export.sh) script automate the process and result (`messages.txt`) can be used as a base for `spam-samples.txt` file. The script requires `jq` and `curl` to be installed and running it will take a long time.

```bash
curl -s https://raw.githubusercontent.com/umputun/tg-spam/master/cas-export.sh > cas-export.sh
chmod +x cas-export.sh
./cas-export.sh
```

Pls note: using results of this script directly as-is may not be such a good idea, because a particular chat group may have a different spam pattern. It is better to use it as a base by picking samples what seems appropriate for a given chat, and add more spam samples from the group itself.

## Updating spam and ham samples from remote git repository

A small utility and docker container provided to update spam and ham samples from a remote git repository. The utility is designed to be run either as a docker container or as a standalone script or as a part of a cron job. For more details see [updater/README.md](https://github.com/umputun/tg-spam/tree/master/updater/README.md).

It also has an example of [docker-compose.yml](https://github.com/umputun/tg-spam/tree/master/updater/docker-compose.yml) to run it as a container side-by-side with the bot.

## Running tg-spam for multiple groups

It is not possible to run the bot for multiple groups, as the bot is designed to work with a single group only. However, it is possible to run multiple instances of the bot with different tokens and different groups. Note: it has to have a token per bot, because TG doesn't allow using the same token for multiple bots at the same time, and such a reuse attempt will prevent the bot from working properly.

At the same time, multiple instances of the bot can share the same set of samples and dynamic data files. To do so, user should mount the same directory with samples and dynamic data files to all the instances of the bot.

## Using tg-spam as a library

The bot can be used as a library as well. To do so, import the `github.com/umputun/tg-spam/lib` package and create a new instance of the `Detector` struct. Then, call the `Check` method with the message and userID to check. The method will return `true` if the message is spam and `false` otherwise. In addition, the `Check` method will return the list of applied rules as well as the spam-related details.

For more details, see the docs on [pkg.go.dev](https://pkg.go.dev/github.com/umputun/tg-spam/lib)

Example:

```go
package main

import (
  "fmt"
  "io"
  "net/http"
  "strings"

  "github.com/umputun/tg-spam/lib/spamcheck"
  "github.com/umputun/tg-spam/lib/tgspam"
)

func main() {
  // Initialize a new Detector with a Config
  detector := tgspam.NewDetector(tgspam.Config{
    MaxAllowedEmoji:  5,
    MinMsgLen:        10,
    FirstMessageOnly: true,
    CasAPI:           "https://cas.example.com",
    HTTPClient:       &http.Client{},
  })

  // Load stop words
  stopWords := strings.NewReader("\"word1\"\n\"word2\"\n\"hello world\"\n\"some phrase\", \"another phrase\"")
  res, err := detector.LoadStopWords(stopWords)
  if err != nil {
    fmt.Println("Error loading stop words:", err)
    return
  }
  fmt.Println("Loaded", res.StopWords, "stop words")

  // Load spam and ham samples
  spamSamples := strings.NewReader("spam sample 1\nspam sample 2\nspam sample 3")
  hamSamples := strings.NewReader("ham sample 1\nham sample 2\nham sample 3")
  excludedTokens := strings.NewReader("\"the\", \"a\", \"an\"")
  res, err = detector.LoadSamples(excludedTokens, []io.Reader{spamSamples}, []io.Reader{hamSamples})
  if err != nil {
    fmt.Println("Error loading samples:", err)
    return
  }
  fmt.Println("Loaded", res.SpamSamples, "spam samples and", res.HamSamples, "ham samples")

  // check a message for spam
  isSpam, info := detector.Check(spamcheck.Request{Msg: "This is a test message", UserID: "user1", UserName: "John Doe"})
  if isSpam {
    fmt.Println("The message is spam, info:", info)
  } else {
    fmt.Println("The message is not spam, info:", info)
  }

}
```
