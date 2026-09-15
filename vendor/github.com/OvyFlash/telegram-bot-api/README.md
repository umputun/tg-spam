# Golang Bindings for the Telegram Bot API
[![Go Reference](https://pkg.go.dev/badge/github.com/OvyFlash/telegram-bot-api.svg)](https://pkg.go.dev/github.com/OvyFlash/telegram-bot-api)
[![Test](https://github.com/OvyFlash/telegram-bot-api/actions/workflows/test.yml/badge.svg)](https://github.com/OvyFlash/telegram-bot-api/actions/workflows/test.yml)

# Headline

This is a maintained fork of go-telegram-bot-api that preserves the original design while staying current with the latest Telegram Bot API specifications. It continues the development of the original [github.com/go-telegram-bot-api](https://github.com/go-telegram-bot-api/telegram-bot-api) package.

## Features

- Complete coverage of the Telegram Bot API
- Simple and intuitive interface
- Minimal dependencies
- Production-ready and actively maintained
- Preserves original API structure for easy migration

## Installation

Install the latest version:
```bash
go get -u github.com/OvyFlash/telegram-bot-api
```


> ❗ Tag versioning is discouraged due to the unstable nature of the Telegram Bot API, and it might break at any time after the API changes.
> ~~`go get -u github.com/OvyFlash/telegram-bot-api/v7`~~

## Quick Start

Here's a simple echo bot using polling:

```go
package main

import (
    "log"
    "time"
    api "github.com/OvyFlash/telegram-bot-api"
)

func main() {
    bot, err := api.NewBotAPI("BOT_TOKEN")
    if err != nil {
        panic(err)
    }

    log.Printf("Authorized on account %s", bot.Self.UserName)

    updateConfig := api.NewUpdate(0)
    updateConfig.Timeout = 60
    updatesChannel := bot.GetUpdatesChan(updateConfig)

    // Optional: Clear initial updates
    time.Sleep(time.Millisecond * 500)
    updatesChannel.Clear()

    for update := range updatesChannel {
        if update.Message == nil {
            continue
        }

        log.Printf("[%s] %s", update.Message.From.UserName, update.Message.Text)

        msg := api.NewMessage(update.Message.Chat.ID, update.Message.Text)
        msg.ReplyParameters.MessageID = update.Message.MessageID

        bot.Send(msg)
    }
}
```

## Webhook Setup

For webhook implementation:
1. Check the [examples](./examples) folder for examples of webhook implementations
2. Use HTTPS (required by Telegram)
   - Generate SSL certificate:
    ```bash
    openssl req -x509 -newkey rsa:2048 -keyout key.pem -out cert.pem -days 3560 -subj "//O=Org\CN=Test" -nodes
    ```
   - Bring your own SSL certificate. [Let's Encrypt](https://letsencrypt.org) is recommended for free TLS certificates in production.

## Documentation

- [GoDoc](https://pkg.go.dev/github.com/OvyFlash/telegram-bot-api)
- Check the [examples](./examples) directory for more use cases


## Contributing

- Issues and feature requests are welcome
- Pull requests should maintain the existing design philosophy
- The focus is on providing a clean API wrapper without additional features

## License

>The [MIT License](./LICENSE.txt)
>
>Copyright (c) 2015 Syfaro
>
>Permission is hereby granted, free of charge, to any person obtaining a copy
>of this software and associated documentation files (the "Software"), to deal
>in the Software without restriction, including without limitation the rights
>to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
>copies of the Software, and to permit persons to whom the Software is
>furnished to do so, subject to the following conditions:
>
>The above copyright notice and this permission notice shall be included in all
>copies or substantial portions of the Software.
>
>THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
>IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
>FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
>AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
>LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
>OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
>SOFTWARE.
