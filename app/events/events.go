package events

import (
	"fmt"
	"log"
	"strings"
	"time"

	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

//go:generate moq --out mocks/tb_api.go --pkg mocks --with-resets --skip-ensure . TbAPI
//go:generate moq --out mocks/spam_logger.go --pkg mocks --with-resets --skip-ensure . SpamLogger
//go:generate moq --out mocks/bot.go --pkg mocks --with-resets --skip-ensure . Bot

// TbAPI is an interface for telegram bot API, only subset of methods used
type TbAPI interface {
	GetUpdatesChan(config tbapi.UpdateConfig) tbapi.UpdatesChannel
	Send(c tbapi.Chattable) (tbapi.Message, error)
	Request(c tbapi.Chattable) (*tbapi.APIResponse, error)
	GetChat(config tbapi.ChatInfoConfig) (tbapi.Chat, error)
	GetChatAdministrators(config tbapi.ChatAdministratorsConfig) ([]tbapi.ChatMember, error)
}

// SpamLogger is an interface for spam logger
type SpamLogger interface {
	Save(msg *bot.Message, response *bot.Response)
}

// SpamLoggerFunc is a function that implements SpamLogger interface
type SpamLoggerFunc func(msg *bot.Message, response *bot.Response)

// Save is a function that implements SpamLogger interface
func (f SpamLoggerFunc) Save(msg *bot.Message, response *bot.Response) {
	f(msg, response)
}

// Locator is an interface for message locator
type Locator interface {
	AddMessage(msg string, chatID, userID int64, userName string, msgID int) error
	AddSpam(userID int64, checks []spamcheck.Response) error
	Message(msg string) (storage.MsgMeta, bool)
	Spam(userID int64) (storage.SpamData, bool)
	MsgHash(msg string) string
	UserNameByID(userID int64) string
}

// Bot is an interface for bot events.
type Bot interface {
	OnMessage(msg bot.Message) (response bot.Response)
	UpdateSpam(msg string) error
	UpdateHam(msg string) error
	AddApprovedUser(id int64, name string) error
	RemoveApprovedUser(id int64) error
	IsApprovedUser(userID int64) bool
}

func escapeMarkDownV1Text(text string) string {
	escSymbols := []string{"_", "*", "`", "["}
	for _, esc := range escSymbols {
		text = strings.Replace(text, esc, "\\"+esc, -1)
	}
	return text
}

// send a message to the telegram as markdown first and if failed - as plain text
func send(tbMsg tbapi.Chattable, tbAPI TbAPI) error {
	withParseMode := func(tbMsg tbapi.Chattable, parseMode string) tbapi.Chattable {
		switch msg := tbMsg.(type) {
		case tbapi.MessageConfig:
			msg.ParseMode = parseMode
			msg.DisableWebPagePreview = true
			return msg
		case tbapi.EditMessageTextConfig:
			msg.ParseMode = parseMode
			msg.DisableWebPagePreview = true
			return msg
		case tbapi.EditMessageReplyMarkupConfig:
			return msg
		}
		return tbMsg // don't touch other types
	}

	msg := withParseMode(tbMsg, tbapi.ModeMarkdown) // try markdown first
	if _, err := tbAPI.Send(msg); err != nil {
		log.Printf("[WARN] failed to send message as markdown, %v", err)
		msg = withParseMode(tbMsg, "") // try plain text
		if _, err := tbAPI.Send(msg); err != nil {
			return fmt.Errorf("can't send message to telegram: %w", err)
		}
	}
	return nil
}

type banRequest struct {
	tbAPI TbAPI

	userID    int64
	channelID int64
	chatID    int64
	duration  time.Duration

	dry      bool
	training bool
}

// The bot must be an administrator in the supergroup for this to work
// and must have the appropriate admin rights.
// If channel is provided, it is banned instead of provided user, permanently.
func banUserOrChannel(r banRequest) error {
	// From Telegram Bot API documentation:
	// > If user is restricted for more than 366 days or less than 30 seconds from the current time,
	// > they are considered to be restricted forever
	// Because the API query uses unix timestamp rather than "ban duration",
	// you do not want to accidentally get into this 30-second window of a lifetime ban.
	// In practice BanDuration is equal to ten minutes,
	// so this `if` statement is unlikely to be evaluated to true.

	if r.dry {
		log.Printf("[INFO] dry run: ban %d for %v", r.userID, r.duration)
		return nil
	}

	if r.training {
		log.Printf("[INFO] training mode: ban %d for %v", r.userID, r.duration)
		return nil
	}

	if r.duration < 30*time.Second {
		r.duration = 1 * time.Minute
	}

	if r.channelID != 0 {
		resp, err := r.tbAPI.Request(tbapi.BanChatSenderChatConfig{
			ChatID:       r.chatID,
			SenderChatID: r.channelID,
			UntilDate:    int(time.Now().Add(r.duration).Unix()),
		})
		if err != nil {
			return err
		}
		if !resp.Ok {
			return fmt.Errorf("response is not Ok: %v", string(resp.Result))
		}
		return nil
	}

	resp, err := r.tbAPI.Request(tbapi.BanChatMemberConfig{
		ChatMemberConfig: tbapi.ChatMemberConfig{
			ChatID: r.chatID,
			UserID: r.userID,
		},
		UntilDate: time.Now().Add(r.duration).Unix(),
	})
	if err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("response is not Ok: %v", string(resp.Result))
	}

	return nil
}
