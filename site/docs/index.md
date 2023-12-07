# tg-spam

TG-Spam is a self-hosted anti-spam bot designed for Telegram, offering a seamless and effective solution to keep unwanted spam at bay. Carefully engineered to minimize disruptions for legitimate users while being a formidable barrier against spam bots. TG-Spam utilizes advanced detection techniques to maintain a spam-free environment.

<div align="center">
  <img class="logo" src="logo.png" width="400px" alt="TG-Spam | Spam Hunter"/>
</div>

<div align="center">



</div>

## What is it and how it works?

TG-Spam is a sophisticated anti-spam bot tailored for Telegram groups, designed to run seamlessly as a Docker container. It is simple to set up, requiring only a telegram token and a group name or ID to begin its operation. Once deployed, TG-Spam diligently monitors all messages, employing a robust spam detection system to identify and eliminate spam content.

### Key Features of Spam Detection

TG-Spam's spam detection algorithm is multifaceted, incorporating several criteria to ensure high accuracy and efficiency:

- Message Analysis: It evaluates messages for similarities to known spam, flagging those that match typical spam characteristics.
- Integration with Combot Anti-Spam System (CAS): It cross-references users with the Combot Anti-Spam System, a reputable external anti-spam database.
- Spam Message Similarity Check: TG-Spam assesses the overall resemblance of each message to known spam patterns.
- Stop Words Comparison: Messages are compared against a curated list of stop words commonly found in spam.
- Emoji Count: Messages with an excessive number of emojis are scrutinized, as this is a common trait in spam messages.
- Automated Action: If a message is flagged as spam, TG-Spam takes immediate action by deleting the message and banning the responsible user.

## Admin chat/group

Optionally, user can specify the admin chat/group name/id. In this case, the bot will send a message to the admin chat as soon as a spammer detected. Admin can unban the user by clicking the "unban" link in the message.

In order to allow such a feature, all the params in `admin` section must be specified.

## Getting bot token for Telegram

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


## Application Options

```
Application Options:
  -l, --logs=                 path to spam logs (default: logs) [$SPAM_LOGS]
      --super=                super-users
      --similarity-threshold= spam threshold (default: 0.5) [$SIMILARITY_THRESHOLD]
      --min-msg-len=          min message length to check (default: 50) [$MIN_MSG_LEN]
      --max-emoji=            max emoji count in message (default: 2) [$MAX_EMOJI]
      --paranoid              paranoid mode, check all messages [$PARANOID]
      --dry                   dry mode, no bans [$DRY]
      --dbg                   debug mode [$DEBUG]
      --tg-dbg                telegram debug mode [$TG_DEBUG]

telegram:
      --telegram.token=       telegram bot token [$TELEGRAM_TOKEN]
      --telegram.group=       group name/id [$TELEGRAM_GROUP]
      --telegram.timeout=     http client timeout for telegram (default: 30s) [$TELEGRAM_TIMEOUT]
      --telegram.idle=        idle duration (default: 30s) [$TELEGRAM_IDLE]

admin:
      --admin.url=            admin root url [$ADMIN_URL]
      --admin.address=        admin listen address (default: :8080) [$ADMIN_ADDRESS]
      --admin.secret=         admin secret [$ADMIN_SECRET]
      --admin.group=          admin group name/id [$ADMIN_GROUP]

cas:
      --cas.api=              CAS API (default: https://api.cas.chat) [$CAS_API]
      --cas.timeout=          CAS timeout (default: 5s) [$CAS_TIMEOUT]

files:
      --files.samples-spam=   path to spam samples (default: spam-samples.txt) [$FILES_SAMPLES_SPAM]
      --files.samples-ham=    path to ham samples (default: ham-samples.txt) [$FILES_SAMPLES_HAM]
      --files.exclude-tokens= path to exclude tokens file (default: exclude-tokens.txt) [$FILES_EXCLUDE_TOKENS]
      --files.stop-words=     path to stop words file (default: stop-words.txt) [$FILES_STOP_WORDS]

message:
      --message.startup=      startup message [$MESSAGE_STARTUP]
      --message.spam=         spam message (default: this is spam) [$MESSAGE_SPAM]
      --message.dry=          spam dry message (default: this is spam (dry mode)) [$MESSAGE_DRY]

Help Options:
  -h, --help                  Show this help message

```