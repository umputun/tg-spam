# tg-spam

Anti-Spam bot for Telegram.



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
-l, --logs=             path to logs (default: logs) [$TELEGRAM_LOGS]
--super=            super-users
--idle=             idle duration (default: 30s) [$IDLE]
--api=              CAS API (default: https://api.cas.chat) [$CAS_API]
--timeout=          CAS timeout (default: 5s) [$TIMEOUT]
--threshold=        spam threshold (default: 0.5) [$THRESHOLD]
--min-msg-len=      min message length to check (default: 100) [$MIN_MSG_LEN]
--max-emoji=        max emoji count in message (default: 5) [$MAX_EMOJI]
--samples=          path to spam samples [$SAMPLES]
--exclude-tokens=   path to exclude tokens file [$EXCLUDE_TOKENS]
--stop-words=       path to stop words file [$STOP_WORDS]
--spam-msg=         spam message (default: this is spam: ) [$SPAM_MSG]
--spam-dry-msg=     spam dry message (default: this is spam (dry mode): ) [$SPAM_DRY_MSG]
--dry               dry mode, no bans [$DRY]
--dbg               debug mode [$DEBUG]

telegram:
--telegram.token=   telegram bot token (default: test) [$TELEGRAM_TOKEN]
--telegram.group=   group name/id (default: test) [$TELEGRAM_GROUP]
--telegram.timeout= http client timeout for getting files from Telegram (default: 30s) [$TELEGRAM_TIMEOUT]

Help Options:
-h, --help              Show this help message
```