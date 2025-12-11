package events

import (
	"context"
	"fmt"
	"log"
	"strconv"
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
//go:generate moq --out mocks/locator.go --pkg mocks --with-resets --skip-ensure . Locator
//go:generate moq --out mocks/reports.go --pkg mocks --with-resets --skip-ensure . Reports

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
	AddMessage(ctx context.Context, msg string, chatID, userID int64, userName string, msgID int) error
	AddSpam(ctx context.Context, userID int64, checks []spamcheck.Response) error
	Message(ctx context.Context, msg string) (storage.MsgMeta, bool)
	Spam(ctx context.Context, userID int64) (storage.SpamData, bool)
	MsgHash(msg string) string
	UserNameByID(ctx context.Context, userID int64) string
	GetUserMessageIDs(ctx context.Context, userID int64, limit int) ([]int, error)
}

// Reports is an interface for user spam reports storage
type Reports interface {
	Add(ctx context.Context, report storage.Report) error
	GetByMessage(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error)
	GetReporterCountSince(ctx context.Context, reporterID int64, since time.Time) (int, error)
	UpdateAdminMsgID(ctx context.Context, msgID int, chatID int64, adminMsgID int) error
	DeleteByMessage(ctx context.Context, msgID int, chatID int64) error
	DeleteReporter(ctx context.Context, reporterID int64, msgID int, chatID int64) error
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

// escapeMarkDownV1Text escapes special characters used in Telegram's MarkdownV1 parse mode.
// It escapes: _ (underscore), * (asterisk), ` (backtick), [ (left bracket)
// This is used when re-parsing already rendered text to prevent markdown parsing errors.
func escapeMarkDownV1Text(text string) string {
	escSymbols := []string{"_", "*", "`", "["}
	for _, esc := range escSymbols {
		text = strings.ReplaceAll(text, esc, "\\"+esc)
	}
	return text
}

// truncateString truncates a string to maxRunes runes (not bytes) and appends suffix if truncated.
// this is safe for multi-byte UTF-8 characters (emoji, cyrillic, etc.)
//
//nolint:unparam // maxRunes may vary in future uses
func truncateString(s string, maxRunes int, suffix string) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + suffix
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

	// for issue #223: Special handling for messages containing profile links with usernames that have underscores
	// these often fail in markdown mode because underscores are used for formatting, but we need to preserve the links
	hasTelegramProfileLink := false
	switch v := tbMsg.(type) {
	case tbapi.EditMessageTextConfig:
		// check if this message contains a Telegram user link, which we want to preserve
		hasTelegramProfileLink = strings.Contains(v.Text, "tg://user?id=")
	}

	// try markdown first, as it's the nicer rendering
	msg := withParseMode(tbMsg, tbapi.ModeMarkdown)
	if _, err := tbAPI.Send(msg); err != nil {
		log.Printf("[WARN] failed to send message as markdown, %v", err)

		// for messages with Telegram profile links, we need to ensure the links are preserved
		// when falling back to plain text, even if markdown fails
		if hasTelegramProfileLink {
			// use HTML mode as a fallback, which better handles usernames with special characters
			htmlMsg := withParseMode(tbMsg, tbapi.ModeHTML)
			if _, err := tbAPI.Send(htmlMsg); err != nil {
				// if HTML also fails, fall back to plain text
				log.Printf("[WARN] failed to send message as HTML, %v", err)
				plainMsg := withParseMode(tbMsg, "") // plain text
				if _, err := tbAPI.Send(plainMsg); err != nil {
					return fmt.Errorf("can't send message to telegram: %w", err)
				}
			}
			return nil
		}

		// for regular messages, just fall back to plain text
		msg = withParseMode(tbMsg, "")
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
	// from Telegram Bot API documentation:
	// > If user is restricted for more than 366 days or less than 30 seconds from the current time,
	// > they are considered to be restricted forever
	// because the API query uses unix timestamp rather than "ban duration",
	// you do not want to accidentally get into this 30-second window of a lifetime ban.
	// in practice BanDuration is equal to ten minutes,
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
			return fmt.Errorf("failed to restrict user: %w", err)
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
			return fmt.Errorf("failed to ban channel: %w", err)
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
		return fmt.Errorf("failed to ban user: %w", err)
	}
	if !resp.Ok {
		return fmt.Errorf("response is not Ok: %v", string(resp.Result))
	}

	log.Printf("[INFO] user %s banned by bot for %v", r.userName, r.duration)
	return nil
}

// transform converts telegram message to internal message format.
// properly handles all message types - text, photo, video, etc, and their combinations.
// also handles forwarded messages, replies, and message entities like links and mentions.
func transform(msg *tbapi.Message) *bot.Message {
	// helper function to convert telegram entities to internal format
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

	// initialize message with basic fields
	message := bot.Message{
		ID:     msg.MessageID,
		Sent:   msg.Time(),
		Text:   msg.Text,
		ChatID: msg.Chat.ID,
	}

	// set sender info
	if msg.From != nil {
		message.From = bot.User{
			ID:       msg.From.ID,
			Username: msg.From.UserName,
		}
		// combine first and last name for display name if present
		if firstName := strings.TrimSpace(msg.From.FirstName); firstName != "" {
			message.From.DisplayName = firstName
		}
		if lastName := strings.TrimSpace(msg.From.LastName); lastName != "" {
			message.From.DisplayName += " " + lastName
		}
	}

	// set sender chat for messages sent on behalf of a channel
	if msg.SenderChat != nil {
		message.SenderChat = bot.SenderChat{
			ID:       msg.SenderChat.ID,
			UserName: msg.SenderChat.UserName,
		}
	}

	// handle message content and type flags independently
	if len(msg.Entities) > 0 {
		message.Entities = transformEntities(msg.Entities)
	}

	if len(msg.Photo) > 0 {
		sizes := msg.Photo
		lastSize := sizes[len(sizes)-1] // use the highest quality photo
		message.Image = &bot.Image{
			FileID:   lastSize.FileID,
			Width:    lastSize.Width,
			Height:   lastSize.Height,
			Caption:  msg.Caption,
			Entities: transformEntities(msg.CaptionEntities),
		}
	}

	// set media type flags
	if msg.Video != nil {
		message.WithVideo = true
	}
	if msg.VideoNote != nil {
		message.WithVideoNote = true
	}
	if msg.Story != nil { // telegram story is treated as video
		message.WithVideo = true
	}
	if msg.Audio != nil {
		message.WithAudio = true
	}
	if msg.ForwardOrigin != nil {
		message.WithForward = true
	}
	if msg.ReplyMarkup != nil { // detect attached keyboards/buttons
		message.WithKeyboard = true
	}
	if msg.Contact != nil {
		message.WithContact = true
	}

	// handle reply-to message if present
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

	// handle caption - either as main text if no text present, or append to existing text
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

// parseCallbackData parses callback data format: [prefix]userID:msgID
// prefix can be: ?, +, !, or two-char report prefixes (R+, R-, R?, R!, RX)
func parseCallbackData(data string) (userID int64, msgID int, err error) {
	if len(data) < 3 {
		return 0, 0, fmt.Errorf("unexpected callback data, too short %q", data)
	}

	// remove prefix if present from the parsed data
	// check for two-char report prefixes first (R+, R-, R?, R!, RX)
	if len(data) >= 3 && data[:1] == "R" {
		// two-char report prefix
		data = data[2:]
	} else if data[:1] == "?" || data[:1] == "+" || data[:1] == "!" {
		// single-char prefix
		data = data[1:]
	}

	parts := strings.Split(data, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected callback data, should have both ids %q", data)
	}
	if userID, err = strconv.ParseInt(parts[0], 10, 64); err != nil {
		return 0, 0, fmt.Errorf("failed to parse userID %q: %w", parts[0], err)
	}
	if msgID, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, fmt.Errorf("failed to parse msgID %q: %w", parts[1], err)
	}

	return userID, msgID, nil
}

// sinceQuery calculates time elapsed since callback query message was sent
func sinceQuery(query *tbapi.CallbackQuery) time.Duration {
	res := time.Since(time.Unix(int64(query.Message.Date), 0)).Round(time.Second)
	if res < 0 { // negative duration possible if clock is not in sync with tg times and a message is from the future
		res = 0
	}
	return res
}
