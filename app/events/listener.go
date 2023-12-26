// Package events provide event handlers for telegram bot and all the high-level event handlers.
// It parses messages, sends them to the spam detector and handles the results. It can also ban users
// and send messages to the admin.
//
// In addition to that, it provides support for admin chat handling allowing to unban users via the web service and
// update the list of spam samples.
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

	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/hashicorp/go-multierror"

	"github.com/umputun/tg-spam/app/bot"
)

// TelegramListener listens to tg update, forward to bots and send back responses
// Not thread safe
type TelegramListener struct {
	TbAPI        TbAPI
	SpamLogger   SpamLogger
	Bot          Bot
	Group        string // can be int64 or public group username (without "@" prefix)
	AdminGroup   string // can be int64 or public group username (without "@" prefix)
	IdleDuration time.Duration
	SuperUsers   SuperUsers
	TestingIDs   []int64
	StartupMsg   string
	NoSpamReply  bool
	TrainingMode bool
	Dry          bool
	KeepUser     bool
	Locator      Locator

	adminHandler *admin
	chatID       int64
	adminChatID  int64

	msgs struct {
		once sync.Once
		ch   chan bot.Response
	}
}

// Do process all events, blocked call
func (l *TelegramListener) Do(ctx context.Context) error {
	log.Printf("[INFO] start telegram listener for %q", l.Group)

	if l.TrainingMode {
		log.Printf("[WARN] training mode, no bans")
	}

	// get chat ID for the group we are monitoring
	var getChatErr error
	if l.chatID, getChatErr = l.getChatID(l.Group); getChatErr != nil {
		return fmt.Errorf("failed to get chat ID for group %q: %w", l.Group, getChatErr)
	}

	if err := l.updateSupers(); err != nil {
		log.Printf("[WARN] failed to update superusers: %v", err)
	}

	if l.AdminGroup != "" {
		// get chat ID for the admin group
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

	// send startup message if any set
	if l.StartupMsg != "" && !l.TrainingMode && !l.Dry {
		if err := l.sendBotResponse(bot.Response{Send: true, Text: l.StartupMsg}, l.chatID); err != nil {
			log.Printf("[WARN] failed to send startup message, %v", err)
		}
	}

	l.adminHandler = &admin{tbAPI: l.TbAPI, bot: l.Bot, locator: l.Locator, primChatID: l.chatID, adminChatID: l.adminChatID,
		superUsers: l.SuperUsers, trainingMode: l.TrainingMode, keepUser: l.KeepUser, dry: l.Dry}
	log.Printf("[DEBUG] admin handler created. %+v", l.adminHandler)

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

			if update.Message != nil && l.isAdminChat(update.Message.Chat.ID, update.Message.From.UserName) {
				if err := l.adminHandler.MsgHandler(update); err != nil {
					log.Printf("[WARN] failed to process admin chat message: %v", err)
					_ = l.sendBotResponse(bot.Response{Send: true, Text: "error: " + err.Error()}, l.adminChatID)
				}
				continue
			}

			if update.CallbackQuery != nil {
				if err := l.adminHandler.InlineCallbackHandler(update.CallbackQuery); err != nil {
					log.Printf("[WARN] failed to process callback: %v", err)
					_ = l.sendBotResponse(bot.Response{Send: true, Text: "error: " + err.Error()}, l.adminChatID)
				}
				continue
			}

			if update.Message == nil {
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
	msg := l.transform(update.Message)
	fromChat := update.Message.Chat.ID

	// ignore messages from other chats except the one we are monitor and ones from the test list
	if !l.isChatAllowed(fromChat) {
		return nil
	}

	// ignore empty messages
	if strings.TrimSpace(msg.Text) == "" {
		return nil
	}

	log.Printf("[DEBUG] incoming msg: %+v", strings.ReplaceAll(msg.Text, "\n", " "))
	normalizedMsg := l.adminHandler.Normalize(update.Message.Text) // without \n and markdown, same as ReportBan uses
	if err := l.Locator.AddMessage(normalizedMsg, fromChat, msg.From.ID, msg.From.Username, msg.ID); err != nil {
		log.Printf("[WARN] failed to add message to locator: %v", err)
	}
	resp := l.Bot.OnMessage(*msg)

	// send response to the channel if allowed
	if resp.Send && !l.NoSpamReply && !l.TrainingMode {
		if err := l.sendBotResponse(resp, fromChat); err != nil {
			log.Printf("[WARN] failed to respond on update, %v", err)
		}
	}

	errs := new(multierror.Error)

	// ban user if requested by bot
	if resp.Send && resp.BanInterval > 0 {
		log.Printf("[DEBUG] ban initiated for %+v", resp)
		l.SpamLogger.Save(msg, &resp)
		if err := l.Locator.AddSpam(msg.From.ID, resp.CheckResults); err != nil {
			log.Printf("[WARN] failed to add spam to locator: %v", err)
		}
		banUserStr := l.getBanUsername(resp, update)

		if l.SuperUsers.IsSuper(msg.From.Username) {
			if l.TrainingMode {
				l.adminHandler.ReportBan(banUserStr, msg)
			}
			log.Printf("[DEBUG] superuser %s requested ban, ignored", banUserStr)
			return nil
		}

		banReq := banRequest{duration: resp.BanInterval, userID: resp.User.ID, channelID: resp.ChannelID,
			chatID: fromChat, dry: l.Dry, training: l.TrainingMode, tbAPI: l.TbAPI}
		if err := banUserOrChannel(banReq); err == nil {
			log.Printf("[INFO] %s banned by bot for %v", banUserStr, resp.BanInterval)
			if l.adminChatID != 0 && msg.From.ID != 0 {
				l.adminHandler.ReportBan(banUserStr, msg)
			}
		} else {
			errs = multierror.Append(errs, fmt.Errorf("failed to ban %s: %w", banUserStr, err))
		}
	}

	// delete message if requested by bot
	if resp.DeleteReplyTo && resp.ReplyTo != 0 && !l.Dry && !l.SuperUsers.IsSuper(msg.From.Username) && !l.TrainingMode {
		if _, err := l.TbAPI.Request(tbapi.DeleteMessageConfig{ChatID: l.chatID, MessageID: resp.ReplyTo}); err != nil {
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

func (l *TelegramListener) isAdminChat(fromChat int64, from string) bool {
	if fromChat == l.adminChatID {
		log.Printf("[DEBUG] message in admin chat %d, from %s", fromChat, from)
		if !l.SuperUsers.IsSuper(from) {
			log.Printf("[DEBUG] %s is not superuser in admin chat, ignored", from)
			return false
		}
		return true
	}
	return false
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
	// if botChat.UserName not set, that means the ban comes from superuser and username should be taken from ReplyToMessage
	if botChat.UserName == "" && update.Message.ReplyToMessage.SenderChat != nil {
		botChat.UserName = update.Message.ReplyToMessage.SenderChat.UserName
	}
	return fmt.Sprintf("%v", botChat)
}

// sendBotResponse sends bot's answer to tg channel
// actionText is a text for the button to unban user, optional
func (l *TelegramListener) sendBotResponse(resp bot.Response, chatID int64) error {
	if !resp.Send {
		return nil
	}

	log.Printf("[DEBUG] bot response - %+v, reply-to:%d", strings.ReplaceAll(resp.Text, "\n", "\\n"), resp.ReplyTo)
	tbMsg := tbapi.NewMessage(chatID, resp.Text)
	tbMsg.ParseMode = tbapi.ModeMarkdown
	tbMsg.DisableWebPagePreview = true
	tbMsg.ReplyToMessageID = resp.ReplyTo

	if err := send(tbMsg, l.TbAPI); err != nil {
		return fmt.Errorf("can't send message to telegram %q: %w", resp.Text, err)
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

// updateSupers updates the list of super-users based on the chat administrators fetched from the Telegram API.
func (l *TelegramListener) updateSupers() error {
	isSuper := func(username string) bool {
		for _, super := range l.SuperUsers {
			if super == username {
				return true
			}
		}
		return false
	}

	admins, err := l.TbAPI.GetChatAdministrators(tbapi.ChatAdministratorsConfig{ChatConfig: tbapi.ChatConfig{ChatID: l.chatID}})
	if err != nil {
		return fmt.Errorf("failed to get chat administrators: %w", err)
	}

	for _, admin := range admins {
		if strings.TrimSpace(admin.User.UserName) == "" {
			continue
		}
		if isSuper(admin.User.UserName) {
			continue // already in the list
		}
		l.SuperUsers = append(l.SuperUsers, admin.User.UserName)
	}

	log.Printf("[INFO] added admins, full list of supers: {%s}", strings.Join(l.SuperUsers, ", "))
	return err
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

// SuperUsers for moderators
type SuperUsers []string

// IsSuper checks if username in the list of super users
func (s SuperUsers) IsSuper(userName string) bool {
	for _, super := range s {
		if strings.EqualFold(userName, super) || strings.EqualFold("/"+userName, super) {
			return true
		}
	}
	return false
}
