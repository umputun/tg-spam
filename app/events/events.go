package events

import (
	"fmt"
	"log"
	"strings"
	"time"

	tbapi "github.com/OvyFlash/telegram-bot-api"

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
	GetChat(config tbapi.ChatInfoConfig) (tbapi.ChatFullInfo, error)
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
	OnMessage(msg bot.Message, checkOnly bool) (response bot.Response)
	UpdateSpam(msg string) error
	UpdateHam(msg string) error
	AddApprovedUser(id int64, name string) error
	RemoveApprovedUser(id int64) error
	IsApprovedUser(userID int64) bool
}

func escapeMarkDownV1Text(text string) string {
	escSymbols := []string{"_", "*", "`", "["}
	for _, esc := range escSymbols {
		text = strings.ReplaceAll(text, esc, "\\"+esc)
	}
	return text
}

// send a message to the telegram as markdown first and if failed - as plain text
func send(tbMsg tbapi.Chattable, tbAPI TbAPI) error {
	withParseMode := func(tbMsg tbapi.Chattable, parseMode string) tbapi.Chattable {
		switch msg := tbMsg.(type) {
		case tbapi.MessageConfig:
			msg.ParseMode = parseMode
			msg.LinkPreviewOptions = tbapi.LinkPreviewOptions{IsDisabled: true}
			return msg
		case tbapi.EditMessageTextConfig:
			msg.ParseMode = parseMode
			msg.LinkPreviewOptions = tbapi.LinkPreviewOptions{IsDisabled: true}
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
	userName  string

	dry      bool
	training bool // training mode, do not do the actual ban
	restrict bool // restrict instead of ban
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

	bannedEntity := fmt.Sprintf("user %d", r.userID)
	if r.channelID != 0 {
		bannedEntity = fmt.Sprintf("channel %d", r.channelID)
	}
	if r.dry {
		log.Printf("[INFO] dry run: ban %s for %v", bannedEntity, r.duration)
		return nil
	}

	if r.training {
		log.Printf("[INFO] training mode: ban %s for %v", bannedEntity, r.duration)
		return nil
	}

	if r.duration < 30*time.Second {
		r.duration = 1 * time.Minute
	}

	if r.restrict { // soft ban mode
		resp, err := r.tbAPI.Request(tbapi.RestrictChatMemberConfig{
			ChatMemberConfig: tbapi.ChatMemberConfig{
				ChatConfig: tbapi.ChatConfig{ChatID: r.chatID},
				UserID:     r.userID,
			},
			UntilDate: time.Now().Add(r.duration).Unix(),
			Permissions: &tbapi.ChatPermissions{
				CanSendMessages:      false,
				CanSendAudios:        false,
				CanSendDocuments:     false,
				CanSendPhotos:        false,
				CanSendVideos:        false,
				CanSendVideoNotes:    false,
				CanSendVoiceNotes:    false,
				CanSendOtherMessages: false,
				CanChangeInfo:        false,
				CanInviteUsers:       false,
				CanPinMessages:       false,
			},
		})
		if err != nil {
			return err
		}
		if !resp.Ok {
			return fmt.Errorf("response is not Ok: %v", string(resp.Result))
		}
		log.Printf("[INFO] %s restricted by bot for %v", r.userName, r.duration)
		return nil
	}

	if r.channelID != 0 {
		resp, err := r.tbAPI.Request(tbapi.BanChatSenderChatConfig{
			ChatConfig:   tbapi.ChatConfig{ChatID: r.chatID},
			SenderChatID: r.channelID,
			UntilDate:    int(time.Now().Add(r.duration).Unix()),
		})
		if err != nil {
			return err
		}
		if !resp.Ok {
			return fmt.Errorf("response is not Ok: %v", string(resp.Result))
		}
		log.Printf("[INFO] channel %s banned by bot for %v", r.userName, r.duration)
		return nil
	}

	resp, err := r.tbAPI.Request(tbapi.BanChatMemberConfig{
		ChatMemberConfig: tbapi.ChatMemberConfig{
			ChatConfig: tbapi.ChatConfig{ChatID: r.chatID},
			UserID:     r.userID,
		},
		UntilDate: time.Now().Add(r.duration).Unix(),
	})
	if err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("response is not Ok: %v", string(resp.Result))
	}

	log.Printf("[INFO] user %s banned by bot for %v", r.userName, r.duration)
	return nil
}

func transform(msg *tbapi.Message) *bot.Message {
	transformEntities := func(entities []tbapi.MessageEntity) *[]bot.Entity {
		if len(entities) == 0 {
			return nil
		}

		result := make([]bot.Entity, 0, len(entities))
		for _, entity := range entities {
			e := bot.Entity{
				Type:   entity.Type,
				Offset: entity.Offset,
				Length: entity.Length,
				URL:    entity.URL,
			}
			if entity.User != nil {
				e.User = &bot.User{
					ID:          entity.User.ID,
					Username:    entity.User.UserName,
					DisplayName: entity.User.FirstName + " " + entity.User.LastName,
				}
			}
			result = append(result, e)
		}

		return &result
	}

	message := bot.Message{
		ID:   msg.MessageID,
		Sent: msg.Time(),
		Text: msg.Text,
	}

	message.ChatID = msg.Chat.ID

	if msg.From != nil {
		message.From = bot.User{
			ID:       msg.From.ID,
			Username: msg.From.UserName,
		}
	}

	if msg.From != nil && strings.TrimSpace(msg.From.FirstName) != "" {
		message.From.DisplayName = msg.From.FirstName
	}
	if msg.From != nil && strings.TrimSpace(msg.From.LastName) != "" {
		message.From.DisplayName += " " + msg.From.LastName
	}

	if msg.SenderChat != nil {
		message.SenderChat = bot.SenderChat{
			ID:       msg.SenderChat.ID,
			UserName: msg.SenderChat.UserName,
		}
	}

	switch {
	case len(msg.Entities) > 0:
		message.Entities = transformEntities(msg.Entities)

	case len(msg.Photo) > 0:
		sizes := msg.Photo
		lastSize := sizes[len(sizes)-1]
		message.Image = &bot.Image{
			FileID:   lastSize.FileID,
			Width:    lastSize.Width,
			Height:   lastSize.Height,
			Caption:  msg.Caption,
			Entities: transformEntities(msg.CaptionEntities),
		}
	case msg.Video != nil:
		message.WithVideo = true
	case msg.VideoNote != nil:
		message.WithVideoNote = true
	case msg.Story != nil: // telegram story is a sort of video-like thing, mark it as video
		message.WithVideo = true
	case msg.ForwardOrigin != nil:
		message.WithForward = true
	}

	// fill in the message's reply-to message
	if msg.ReplyToMessage != nil {
		message.ReplyTo.Text = msg.ReplyToMessage.Text
		message.ReplyTo.Sent = msg.ReplyToMessage.Time()
		if msg.ReplyToMessage.From != nil {
			message.ReplyTo.From = bot.User{
				ID:          msg.ReplyToMessage.From.ID,
				Username:    msg.ReplyToMessage.From.UserName,
				DisplayName: msg.ReplyToMessage.From.FirstName + " " + msg.ReplyToMessage.From.LastName,
			}
		}
		if msg.ReplyToMessage.SenderChat != nil {
			message.ReplyTo.SenderChat = bot.SenderChat{
				ID:       msg.ReplyToMessage.SenderChat.ID,
				UserName: msg.ReplyToMessage.SenderChat.UserName,
			}
		}
	}

	if msg.Caption != "" {
		if message.Text == "" {
			log.Printf("[DEBUG] caption only message: %q", msg.Caption)
			message.Text = msg.Caption
		} else {
			log.Printf("[DEBUG] caption appended to message: %q", msg.Caption)
			message.Text += "\n" + msg.Caption
		}
	}
	return &message
}
