package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-pkgz/notify"
	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/hashicorp/go-multierror"

	"github.com/umputun/tg-spam/app/bot"
)

//go:generate moq --out mocks/tb_api.go --pkg mocks --with-resets --skip-ensure . TbAPI
//go:generate moq --out mocks/spam_logger.go --pkg mocks --with-resets --skip-ensure . SpamLogger
//go:generate moq --out mocks/bot.go --pkg mocks --with-resets --skip-ensure . Bot
//go:generate moq --out mocks/spam_web.go --pkg mocks --with-resets --skip-ensure . SpamWeb

// TelegramListener listens to tg update, forward to bots and send back responses
// Not thread safe
type TelegramListener struct {
	TbAPI        TbAPI
	SpamLogger   SpamLogger
	Bot          Bot
	Group        string // can be int64 or public group username (without "@" prefix)
	AdminGroup   string // can be int64 or public group username (without "@" prefix)
	IdleDuration time.Duration
	SuperUsers   SuperUser
	TestingIDs   []int64
	StartupMsg   string
	NoSpamReply  bool
	Dry          bool
	SpamWeb      SpamWeb

	chatID      int64
	adminChatID int64

	msgs struct {
		once sync.Once
		ch   chan bot.Response
	}
}

// TbAPI is an interface for telegram bot API, only subset of methods used
type TbAPI interface {
	GetUpdatesChan(config tbapi.UpdateConfig) tbapi.UpdatesChannel
	Send(c tbapi.Chattable) (tbapi.Message, error)
	Request(c tbapi.Chattable) (*tbapi.APIResponse, error)
	GetChat(config tbapi.ChatInfoConfig) (tbapi.Chat, error)
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

// Bot is an interface for bot events.
type Bot interface {
	OnMessage(msg bot.Message) (response bot.Response)
	UpdateSpam(msg string) error
	UpdateHam(msg string) error
}

// SpamWeb is an interface for the web component
type SpamWeb interface {
	UnbanURL(userID int64) string
}

// Do process all events, blocked call
func (l *TelegramListener) Do(ctx context.Context) error {
	log.Printf("[INFO] start telegram listener for %q", l.Group)

	var getChatErr error
	if l.chatID, getChatErr = l.getChatID(l.Group); getChatErr != nil {
		return fmt.Errorf("failed to get chat ID for group %q: %w", l.Group, getChatErr)
	}

	if l.AdminGroup != "" {
		if l.adminChatID, getChatErr = l.getChatID(l.AdminGroup); getChatErr != nil {
			return fmt.Errorf("failed to get chat ID for admin group %q: %w", l.AdminGroup, getChatErr)
		}
		log.Printf("[INFO] admin chat ID: %d", l.adminChatID)
	}

	l.msgs.once.Do(func() {
		l.msgs.ch = make(chan bot.Response, 100)
		if l.IdleDuration == 0 {
			l.IdleDuration = 30 * time.Second
		}
	})

	if l.StartupMsg != "" {
		if err := l.sendBotResponse(bot.Response{Send: true, Text: l.StartupMsg}, l.chatID); err != nil {
			log.Printf("[WARN] failed to send startup message, %v", err)
		}
	}

	u := tbapi.NewUpdate(0)
	u.Timeout = 60

	updates := l.TbAPI.GetUpdatesChan(u)

	for {
		select {

		case <-ctx.Done():
			return ctx.Err()

		case update, ok := <-updates:
			if !ok {
				return fmt.Errorf("telegram update chan closed")
			}

			if update.Message == nil {
				log.Print("[DEBUG] empty message body")
				continue
			}
			if update.Message.Chat == nil {
				log.Print("[DEBUG] ignoring message not from chat")
				continue
			}

			if err := l.procEvents(update); err != nil {
				log.Printf("[WARN] failed to process update: %v", err)
				continue
			}

		case <-time.After(l.IdleDuration): // hit bots on idle timeout
			resp := l.Bot.OnMessage(bot.Message{Text: "idle"})
			if err := l.sendBotResponse(resp, l.chatID); err != nil {
				log.Printf("[WARN] failed to respond on idle, %v", err)
			}
		}
	}
}

func (l *TelegramListener) procEvents(update tbapi.Update) error {
	msgJSON, errJSON := json.Marshal(update.Message)
	if errJSON != nil {
		return fmt.Errorf("failed to marshal update.Message to json: %w", errJSON)
	}
	log.Printf("[DEBUG] %s", string(msgJSON))

	fromChat := update.Message.Chat.ID
	if !l.isChatAllowed(fromChat) {
		// ignore messages from other chats
		return nil
	}

	msg := l.transform(update.Message)
	log.Printf("[DEBUG] incoming msg: %+v", strings.ReplaceAll(msg.Text, "\n", " "))

	resp := l.Bot.OnMessage(*msg)

	if resp.Send && !l.NoSpamReply {
		if err := l.sendBotResponse(resp, fromChat); err != nil {
			log.Printf("[WARN] failed to respond on update, %v", err)
		}
	}

	errs := new(multierror.Error)
	isBanInvoked := resp.Send && resp.BanInterval > 0
	// some bots may request a direct ban for given duration
	if isBanInvoked {
		log.Printf("[DEBUG] ban initiated for %+v", resp)
		l.SpamLogger.Save(msg, &resp)
		banUserStr := l.getBanUsername(resp, update)
		if l.SuperUsers.IsSuper(msg.From.Username) {
			log.Printf("[DEBUG] superuser %s requested ban, ignored", banUserStr)
			return nil
		}
		banSuccessMessage := fmt.Sprintf("[INFO] %s banned by bot for %v", banUserStr, resp.BanInterval)
		if err := l.banUserOrChannel(resp.BanInterval, fromChat, resp.User.ID, resp.ChannelID); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to ban %s: %w", banUserStr, err))
		} else {
			log.Print(banSuccessMessage)
			if l.adminChatID != 0 && msg.From.ID != 0 {
				l.forwardToAdmin(banUserStr, msg)
			}
		}
	}

	// delete message if requested by bot
	if resp.DeleteReplyTo && resp.ReplyTo != 0 && !l.Dry {
		_, err := l.TbAPI.Request(tbapi.DeleteMessageConfig{ChatID: l.chatID, MessageID: resp.ReplyTo})
		if err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", resp.ReplyTo, err))
		}
	}
	return errs.ErrorOrNil()
}

func (l *TelegramListener) isChatAllowed(fromChat int64) bool {
	if fromChat == l.chatID {
		return true
	}
	for _, id := range l.TestingIDs {
		if id == fromChat {
			return true
		}
	}
	return false
}

func (l *TelegramListener) forwardToAdmin(banUserStr string, msg *bot.Message) {
	// escapeMarkDownV1Text escapes markdownV1 special characters, used in places where we want to send text as-is.
	// For example, telegram username with underscores would be italicized if we don't escape it.
	// https://core.telegram.org/bots/api#markdown-style
	escapeMarkDownV1Text := func(text string) string {
		escSymbols := []string{"_", "*", "`", "["}
		for _, esc := range escSymbols {
			text = strings.Replace(text, esc, "\\"+esc, -1)
		}
		return text
	}

	log.Printf("[DEBUG] forward to admin ban data for %s, group: %d", banUserStr, l.adminChatID)
	text := strings.ReplaceAll(escapeMarkDownV1Text(msg.Text), "\n", " ")
	forwardMsg := fmt.Sprintf("**permanently banned [%s](tg://user?id=%d)**\n[⛔︎ unban if wrong ⛔︎](%s)\n\n%s\n\n",
		banUserStr, msg.From.ID, l.SpamWeb.UnbanURL(msg.From.ID), text)
	e := l.sendBotResponse(bot.Response{Send: true, Text: forwardMsg, ParseMode: tbapi.ModeMarkdown}, l.adminChatID)
	if e != nil {
		log.Printf("[WARN] failed to send admin message, %v", e)
	}
}

func (l *TelegramListener) getBanUsername(resp bot.Response, update tbapi.Update) string {
	if resp.ChannelID == 0 {
		return fmt.Sprintf("%v", resp.User)
	}
	botChat := bot.SenderChat{
		ID: resp.ChannelID,
	}
	if update.Message.SenderChat != nil {
		botChat.UserName = update.Message.SenderChat.UserName
	}
	// if not set, that means the ban comes from superuser and username should be taken from ReplyToMessage
	if botChat.UserName == "" && update.Message.ReplyToMessage.SenderChat != nil {
		botChat.UserName = update.Message.ReplyToMessage.SenderChat.UserName
	}
	return fmt.Sprintf("%v", botChat)
}

// sendBotResponse sends bot's answer to tg channel
func (l *TelegramListener) sendBotResponse(resp bot.Response, chatID int64) error {
	if !resp.Send {
		return nil
	}

	log.Printf("[DEBUG] bot response - %+v, reply-to:%d, parse-mode:%s", resp.Text, resp.ReplyTo, resp.ParseMode)
	tbMsg := tbapi.NewMessage(chatID, resp.Text)
	tbMsg.ParseMode = tbapi.ModeMarkdown
	if resp.ParseMode != "" {
		tbMsg.ParseMode = resp.ParseMode
	}
	tbMsg.DisableWebPagePreview = true
	tbMsg.ReplyToMessageID = resp.ReplyTo
	if _, err := l.TbAPI.Send(tbMsg); err != nil {
		return fmt.Errorf("can't send message to telegram %q: %w", resp.Text, err)
	}

	return nil
}

// Submit message text to telegram's group
func (l *TelegramListener) Submit(ctx context.Context, text string) error {
	l.msgs.once.Do(func() { l.msgs.ch = make(chan bot.Response, 100) })

	select {
	case <-ctx.Done():
		return ctx.Err()
	case l.msgs.ch <- bot.Response{Text: text, Send: true}:
	}
	return nil
}

// SubmitHTML message to telegram's group with HTML mode
func (l *TelegramListener) SubmitHTML(ctx context.Context, text string) error {
	// Remove unsupported HTML tags
	text = notify.TelegramSupportedHTML(text)
	l.msgs.once.Do(func() { l.msgs.ch = make(chan bot.Response, 100) })

	select {
	case <-ctx.Done():
		return ctx.Err()
	case l.msgs.ch <- bot.Response{Text: text, Send: true, ParseMode: tbapi.ModeHTML}:
	}
	return nil
}

func (l *TelegramListener) getChatID(group string) (int64, error) {
	chatID, err := strconv.ParseInt(group, 10, 64)
	if err == nil {
		return chatID, nil
	}

	chat, err := l.TbAPI.GetChat(tbapi.ChatInfoConfig{ChatConfig: tbapi.ChatConfig{SuperGroupUsername: "@" + group}})
	if err != nil {
		return 0, fmt.Errorf("can't get chat for %s: %w", group, err)
	}

	return chat.ID, nil
}

// The bot must be an administrator in the supergroup for this to work
// and must have the appropriate admin rights.
// If channel is provided, it is banned instead of provided user, permanently.
func (l *TelegramListener) banUserOrChannel(duration time.Duration, chatID, userID, channelID int64) error {
	// From Telegram Bot API documentation:
	// > If user is restricted for more than 366 days or less than 30 seconds from the current time,
	// > they are considered to be restricted forever
	// Because the API query uses unix timestamp rather than "ban duration",
	// you do not want to accidentally get into this 30-second window of a lifetime ban.
	// In practice BanDuration is equal to ten minutes,
	// so this `if` statement is unlikely to be evaluated to true.

	if l.Dry {
		log.Printf("[INFO] dry run: ban %d for %v", userID, duration)
		return nil
	}

	if duration < 30*time.Second {
		duration = 1 * time.Minute
	}

	if channelID != 0 {
		resp, err := l.TbAPI.Request(tbapi.BanChatSenderChatConfig{
			ChatID:       chatID,
			SenderChatID: channelID,
			UntilDate:    int(time.Now().Add(duration).Unix()),
		})
		if err != nil {
			return err
		}
		if !resp.Ok {
			return fmt.Errorf("response is not Ok: %v", string(resp.Result))
		}
		return nil
	}

	resp, err := l.TbAPI.Request(tbapi.RestrictChatMemberConfig{
		ChatMemberConfig: tbapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: userID,
		},
		UntilDate: time.Now().Add(duration).Unix(),
		Permissions: &tbapi.ChatPermissions{
			CanSendMessages:       false,
			CanSendMediaMessages:  false,
			CanSendOtherMessages:  false,
			CanAddWebPagePreviews: false,
		},
	})
	if err != nil {
		return err
	}
	if !resp.Ok {
		return fmt.Errorf("response is not Ok: %v", string(resp.Result))
	}

	return nil
}

func (l *TelegramListener) transform(msg *tbapi.Message) *bot.Message {
	message := bot.Message{
		ID:   msg.MessageID,
		Sent: msg.Time(),
		Text: msg.Text,
	}

	if msg.Chat != nil {
		message.ChatID = msg.Chat.ID
	}

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
	case msg.Entities != nil && len(msg.Entities) > 0:
		message.Entities = l.transformEntities(msg.Entities)

	case msg.Photo != nil && len(msg.Photo) > 0:
		sizes := msg.Photo
		lastSize := sizes[len(sizes)-1]
		message.Image = &bot.Image{
			FileID:   lastSize.FileID,
			Width:    lastSize.Width,
			Height:   lastSize.Height,
			Caption:  msg.Caption,
			Entities: l.transformEntities(msg.CaptionEntities),
		}
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

	return &message
}

func (l *TelegramListener) transformEntities(entities []tbapi.MessageEntity) *[]bot.Entity {
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
