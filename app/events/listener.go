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
	TbAPI                   TbAPI         // telegram bot API
	SpamLogger              SpamLogger    // logger to save spam to files and db
	Bot                     Bot           // bot to handle messages
	Group                   string        // can be int64 or public group username (without "@" prefix)
	AdminGroup              string        // can be int64 or public group username (without "@" prefix)
	IdleDuration            time.Duration // idle timeout to send "idle" message to bots
	SuperUsers              SuperUsers    // list of superusers, can ban and report spam, can't be banned
	TestingIDs              []int64       // list of chat IDs to test the bot
	StartupMsg              string        // message to send on startup to the primary chat
	WarnMsg                 string        // message to send on warning
	RestoreMsg			    string        // message to send on restore
	NoSpamReply             bool          // do not reply on spam messages in the primary chat
	TrainingMode            bool          // do not ban users, just report and train spam detector
	SoftBanMode             bool          // do not ban users, but restrict their actions
	Locator                 Locator       // message locator to get info about messages
	DisableAdminSpamForward bool          // disable forwarding spam reports to admin chat support
	Dry                     bool          // dry run, do not ban or send messages

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

	if l.SoftBanMode {
		log.Printf("[INFO] soft ban mode, no bans but restrictions")
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
		if _, err := l.sendBotResponse(bot.Response{Send: true, Text: l.StartupMsg}, l.chatID, true ); err != nil {
			log.Printf("[WARN] failed to send startup message, %v", err)
		}
	}

	l.adminHandler = &admin{tbAPI: l.TbAPI, bot: l.Bot, locator: l.Locator, primChatID: l.chatID, adminChatID: l.adminChatID,
		superUsers: l.SuperUsers, trainingMode: l.TrainingMode, softBan: l.SoftBanMode, dry: l.Dry, warnMsg: l.WarnMsg, restoreMsg: l.RestoreMsg}

	adminForwardStatus := "enabled"
	if l.DisableAdminSpamForward {
		adminForwardStatus = "disabled"
	}
	log.Printf("[DEBUG] admin handler created, spam forvarding %s, %+v", adminForwardStatus, l.adminHandler)

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

			// handle admin chat messages
			if update.Message != nil && l.isAdminChat(update.Message.Chat.ID, update.Message.From.UserName) {
				if l.DisableAdminSpamForward {
					continue
				}
				if err := l.adminHandler.MsgHandler(update); err != nil {
					log.Printf("[WARN] failed to process admin chat message: %v", err)
					_, _ = l.sendBotResponse(bot.Response{Send: true, Text: "error: " + err.Error()}, l.adminChatID, false)
				}
				continue
			}

			// handle admin chat inline buttons
			if update.CallbackQuery != nil {
				if err := l.adminHandler.InlineCallbackHandler(update.CallbackQuery); err != nil {
					log.Printf("[WARN] failed to process callback: %v", err)
					_, _ = l.sendBotResponse(bot.Response{Send: true, Text: "error: " + err.Error()}, l.adminChatID, false)
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

			// handle spam reports from superusers
			if update.Message.ReplyToMessage != nil && l.SuperUsers.IsSuper(update.Message.From.UserName) {
				if strings.EqualFold(update.Message.Text, "/spam") || strings.EqualFold(update.Message.Text, "spam") {
					log.Printf("[DEBUG] superuser %s reported spam", update.Message.From.UserName)
					if err := l.adminHandler.DirectSpamReport(update); err != nil {
						log.Printf("[WARN] failed to process direct spam report: %v", err)
					}
					continue
				}
				if strings.EqualFold(update.Message.Text, "/ban") || strings.EqualFold(update.Message.Text, "ban") {
					log.Printf("[DEBUG] superuser %s requested ban", update.Message.From.UserName)
					if err := l.adminHandler.DirectBanReport(update); err != nil {
						log.Printf("[WARN] failed to process direct ban request: %v", err)
					}
					continue
				}
				if strings.EqualFold(update.Message.Text, "/warn") || strings.EqualFold(update.Message.Text, "warn") {
					log.Printf("[DEBUG] superuser %s requested warning", update.Message.From.UserName)
					if err := l.adminHandler.DirectWarnReport(update); err != nil {
						log.Printf("[WARN] failed to process direct warning request: %v", err)
					}
					continue
				}
			}

			if err := l.procEvents(update); err != nil {
				log.Printf("[WARN] failed to process update: %v", err)
				continue
			}

		case <-time.After(l.IdleDuration): // hit bots on idle timeout
			resp := l.Bot.OnMessage(bot.Message{Text: "idle"})
			if _, err := l.sendBotResponse(resp, l.chatID, true); err != nil {
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
	fromChat := update.Message.Chat.ID
	// ignore messages from other chats except the one we are monitor and ones from the test list
	if !l.isChatAllowed(fromChat) {
		return nil
	}

	log.Printf("[DEBUG] %s", string(msgJSON))
	msg := transform(update.Message)

	// ignore empty messages
	if strings.TrimSpace(msg.Text) == "" && msg.Image == nil {
		return nil
	}

	log.Printf("[DEBUG] incoming msg: %+v", strings.ReplaceAll(msg.Text, "\n", " "))
	if err := l.Locator.AddMessage(msg.Text, fromChat, msg.From.ID, msg.From.Username, msg.ID); err != nil {
		log.Printf("[WARN] failed to add message to locator: %v", err)
	}
	resp := l.Bot.OnMessage(*msg)

	if !resp.Send { // not spam
		return nil
	}

	// send response to the channel if allowed
	spamReplyID := 0
	if resp.Send && !l.NoSpamReply && !l.TrainingMode {
		var err error
		if spamReplyID, err = l.sendBotResponse(resp, fromChat, true); err != nil {
			log.Printf("[WARN] failed to respond on update, %v", err)
		}
		log.Printf("[DEBUG] response sent, spamReplyID: %d", spamReplyID)
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
				l.adminHandler.ReportBan(banUserStr, msg, 0)
			}
			log.Printf("[DEBUG] superuser %s requested ban, ignored", banUserStr)
			return nil
		}

		banReq := banRequest{duration: resp.BanInterval, userID: resp.User.ID, channelID: resp.ChannelID, userName: banUserStr,
			chatID: fromChat, dry: l.Dry, training: l.TrainingMode, tbAPI: l.TbAPI, restrict: l.SoftBanMode}
		if err := banUserOrChannel(banReq); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to ban %s: %w", banUserStr, err))
		} else if l.adminChatID != 0 && msg.From.ID != 0 {
			l.adminHandler.ReportBan(banUserStr, msg, spamReplyID)
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
func (l *TelegramListener) sendBotResponse(resp bot.Response, chatID int64, disableNotification bool) (int, error) {
	if !resp.Send {
		return 0, nil
	}

	log.Printf("[DEBUG] bot response - %+v, reply-to:%d", strings.ReplaceAll(resp.Text, "\n", "\\n"), resp.ReplyTo)
	tbMsg := tbapi.NewMessage(chatID, resp.Text)
	tbMsg.ParseMode = tbapi.ModeMarkdown
	tbMsg.DisableWebPagePreview = true
	tbMsg.ReplyToMessageID = resp.ReplyTo
	tbMsg.DisableNotification = disableNotification

	tbResp, err := l.TbAPI.Send(tbMsg)
	if err != nil {
		return 0, fmt.Errorf("can't send message to telegram %q: %w", resp.Text, err)
	}
	return tbResp.MessageID, nil
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
