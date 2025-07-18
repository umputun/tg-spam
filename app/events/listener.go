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

	tbapi "github.com/OvyFlash/telegram-bot-api"
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
	NoSpamReply             bool          // do not reply on spam messages in the primary chat
	SuppressJoinMessage     bool          // delete join message when kick out user
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
	log.Printf("[INFO] primary chat ID: %d", l.chatID)

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
		if err := l.sendBotResponse(bot.Response{Send: true, Text: l.StartupMsg}, l.chatID, NotificationSilent); err != nil {
			log.Printf("[WARN] failed to send startup message, %v", err)
		} else {
			log.Printf("[DEBUG] startup message sent")
		}
	}

	l.adminHandler = &admin{tbAPI: l.TbAPI, bot: l.Bot, locator: l.Locator, primChatID: l.chatID, adminChatID: l.adminChatID,
		superUsers: l.SuperUsers, trainingMode: l.TrainingMode, softBan: l.SoftBanMode, dry: l.Dry, warnMsg: l.WarnMsg}

	adminForwardStatus := "enabled"
	if l.DisableAdminSpamForward {
		adminForwardStatus = "disabled"
	}
	log.Printf("[DEBUG] admin handler created, spam forwarding %s, %+v", adminForwardStatus, l.adminHandler)

	u := tbapi.NewUpdate(0)
	u.Timeout = 60

	updates := l.TbAPI.GetUpdatesChan(u)
	log.Printf("[DEBUG] start listening for updates")
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("listener context canceled: %w", ctx.Err())

		case update, ok := <-updates:
			if !ok {
				return fmt.Errorf("telegram update chan closed")
			}

			// handle admin chat messages. can be just messages (MsgHandler will ignore those)
			// or forwards of undetected spam by admins to admin's chat (in this case MsgHandler will process them and ban/train)
			if update.Message != nil && l.isAdminChat(update.Message.Chat.ID, update.Message.From.UserName, update.Message.From.ID) {
				if l.DisableAdminSpamForward {
					continue
				}
				if err := l.adminHandler.MsgHandler(update); err != nil {
					log.Printf("[WARN] failed to process admin chat message: %v", err)
					errResp := l.sendBotResponse(bot.Response{Send: true, Text: "error: " + err.Error()}, l.adminChatID, NotificationDefault)
					if errResp != nil {
						log.Printf("[WARN] failed to respond on error, %v", errResp)
					}
				}
				continue
			}

			// handle admin chat inline buttons
			if update.CallbackQuery != nil {
				if err := l.adminHandler.InlineCallbackHandler(update.CallbackQuery); err != nil {
					log.Printf("[WARN] failed to process callback: %v", err)
					errResp := l.sendBotResponse(bot.Response{Send: true, Text: "error: " + err.Error()}, l.adminChatID, NotificationDefault)
					if errResp != nil {
						log.Printf("[WARN] failed to respond on error, %v", errResp)
					}
				}
				continue
			}

			// handle edited messages
			if update.EditedMessage != nil {
				log.Printf("[INFO] processing edited message, id: %d", update.EditedMessage.MessageID)
				// we need to process an edited message as a new message, so we create a new update object
				// and copy the edited message to the message field.
				editedUpdate := tbapi.Update{
					Message: update.EditedMessage,
				}
				if err := l.procEvents(editedUpdate); err != nil {
					log.Printf("[WARN] failed to process edited message update: %v", err)
				}
				continue
			}

			if update.Message == nil {
				continue
			}

			// save join messages to locator even if SuppressJoinMessage is set to false
			if update.Message.NewChatMembers != nil {
				err := l.procNewChatMemberMessage(update)
				if err != nil {
					log.Printf("[WARN] failed to process new chat member: %v", err)
				}
				continue
			}

			// handle left member messages, i.e. "blah blah removed from the chat"
			if update.Message.LeftChatMember != nil {
				if l.SuppressJoinMessage {
					err := l.procLeftChatMemberMessage(update)
					if err != nil {
						log.Printf("[WARN] failed to process left chat member: %v", err)
					}
				}
				continue
			}

			// handle spam reports from superusers
			fromSuper := l.SuperUsers.IsSuper(update.Message.From.UserName, update.Message.From.ID)
			if update.Message.ReplyToMessage != nil && fromSuper {
				if l.procSuperReply(update) {
					// superuser command processed, skip the rest
					continue
				}
			}

			// process regular messages, the main part of the bot
			if err := l.procEvents(update); err != nil {
				log.Printf("[WARN] failed to process update: %v", err)
				continue
			}

		case <-time.After(l.IdleDuration): // hit bots on idle timeout
			resp := l.Bot.OnMessage(bot.Message{Text: "idle"}, false)
			if err := l.sendBotResponse(resp, l.chatID, NotificationSilent); err != nil {
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

	// ignore messages with empty text, no media, no video, no video note, no forward
	if strings.TrimSpace(msg.Text) == "" && msg.Image == nil && !msg.WithVideoNote && !msg.WithVideo && !msg.WithForward {
		return nil
	}
	ctx := context.TODO()
	log.Printf("[DEBUG] incoming msg: %+v", strings.ReplaceAll(msg.Text, "\n", " "))
	log.Printf("[DEBUG] incoming msg details: %+v", msg)
	if err := l.Locator.AddMessage(ctx, msg.Text, fromChat, msg.From.ID, msg.From.Username, msg.ID); err != nil {
		log.Printf("[WARN] failed to add message to locator: %v", err)
	}
	resp := l.Bot.OnMessage(*msg, false)

	if !resp.Send { // not spam
		return nil
	}

	// send response to the channel if allowed
	if resp.Send && !l.NoSpamReply && !l.TrainingMode {
		if err := l.sendBotResponse(resp, fromChat, NotificationSilent); err != nil {
			log.Printf("[WARN] failed to respond on update, %v", err)
		}
	}

	errs := new(multierror.Error)

	// ban user if requested by bot
	if resp.Send && resp.BanInterval > 0 {
		log.Printf("[DEBUG] ban initiated for %+v", resp)
		l.SpamLogger.Save(msg, &resp)
		if err := l.Locator.AddSpam(ctx, msg.From.ID, resp.CheckResults); err != nil {
			log.Printf("[WARN] failed to add spam to locator: %v", err)
		}
		banUserStr := l.getBanUsername(resp, update)

		if l.SuperUsers.IsSuper(msg.From.Username, msg.From.ID) {
			if l.TrainingMode {
				l.adminHandler.ReportBan(banUserStr, msg)
			}
			log.Printf("[DEBUG] superuser %s requested ban, ignored", banUserStr)
			return nil
		}

		banReq := banRequest{duration: resp.BanInterval, userID: resp.User.ID, channelID: resp.ChannelID, userName: banUserStr,
			chatID: fromChat, dry: l.Dry, training: l.TrainingMode, tbAPI: l.TbAPI, restrict: l.SoftBanMode}
		if err := banUserOrChannel(banReq); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to ban %s: %w", banUserStr, err))
		} else if l.adminChatID != 0 && msg.From.ID != 0 {
			l.adminHandler.ReportBan(banUserStr, msg)
		}
	}

	// delete message if requested by bot
	if resp.DeleteReplyTo && resp.ReplyTo != 0 && !l.Dry && !l.SuperUsers.IsSuper(msg.From.Username, msg.From.ID) && !l.TrainingMode {
		if _, err := l.TbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
			MessageID:  resp.ReplyTo,
			ChatConfig: tbapi.ChatConfig{ChatID: l.chatID},
		}}); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", resp.ReplyTo, err))
		}
	}

	if err := errs.ErrorOrNil(); err != nil {
		return fmt.Errorf("processing events failed: %w", err)
	}
	return nil
}

// procSuperReply processes superuser commands (reply) /spam, /ban, /warn
func (l *TelegramListener) procSuperReply(update tbapi.Update) (handled bool) {
	switch {
	case strings.EqualFold(update.Message.Text, "/spam") || strings.EqualFold(update.Message.Text, "spam"):
		log.Printf("[DEBUG] superuser %s reported spam", update.Message.From.UserName)
		if err := l.adminHandler.DirectSpamReport(update); err != nil {
			log.Printf("[WARN] failed to process direct spam report: %v", err)
		}
		return true
	case strings.EqualFold(update.Message.Text, "/ban") || strings.EqualFold(update.Message.Text, "ban"):
		log.Printf("[DEBUG] superuser %s requested ban", update.Message.From.UserName)
		if err := l.adminHandler.DirectBanReport(update); err != nil {
			log.Printf("[WARN] failed to process direct ban request: %v", err)
		}
		return true
	case strings.EqualFold(update.Message.Text, "/warn") || strings.EqualFold(update.Message.Text, "warn"):
		log.Printf("[DEBUG] superuser %s requested warning", update.Message.From.UserName)
		if err := l.adminHandler.DirectWarnReport(update); err != nil {
			log.Printf("[WARN] failed to process direct warning request: %v", err)
		}
		return true
	}
	return false
}

// procNewChatMemberMessage saves new chat member message to locator. It is used to delete the message if the user kicked out
func (l *TelegramListener) procNewChatMemberMessage(update tbapi.Update) error {
	fromChat := update.Message.Chat.ID
	// ignore messages from other chats except the one we are monitor and ones from the test list
	if !l.isChatAllowed(fromChat) {
		return nil
	}

	if len(update.Message.NewChatMembers) != 1 {
		log.Printf("[DEBUG] we are expecting only one new chat member, got %d", len(update.Message.NewChatMembers))
		return nil
	}

	errs := new(multierror.Error)

	member := update.Message.NewChatMembers[0]
	msg := fmt.Sprintf("new_%d_%d", fromChat, member.ID)
	if err := l.Locator.AddMessage(context.TODO(), msg, fromChat, member.ID, "", update.Message.MessageID); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to add new chat member message to locator: %w", err))
	}

	if err := errs.ErrorOrNil(); err != nil {
		return fmt.Errorf("failed to process new chat member: %w", err)
	}
	return nil
}

// procLeftChatMemberMessage deletes the message about new chat member if the user kicked out
func (l *TelegramListener) procLeftChatMemberMessage(update tbapi.Update) error {
	fromChat := update.Message.Chat.ID
	// ignore messages from other chats except the one we are monitor and ones from the test list
	if !l.isChatAllowed(fromChat) {
		return nil
	}

	if update.Message.From.ID == update.Message.LeftChatMember.ID {
		log.Printf("[DEBUG] left chat member is the same as the message sender, ignored")
		return nil
	}
	msg, found := l.Locator.Message(context.TODO(), fmt.Sprintf("new_%d_%d", fromChat, update.Message.LeftChatMember.ID))
	if !found {
		log.Printf("[DEBUG] no new chat member message found for %d in chat %d", update.Message.LeftChatMember.ID, fromChat)
		return nil
	}
	if _, err := l.TbAPI.Request(tbapi.DeleteMessageConfig{
		BaseChatMessage: tbapi.BaseChatMessage{ChatConfig: tbapi.ChatConfig{ChatID: fromChat}, MessageID: msg.MsgID},
	}); err != nil {
		return fmt.Errorf("failed to delete new chat member message %d: %w", msg.MsgID, err)
	}

	return nil
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

func (l *TelegramListener) isAdminChat(fromChat int64, from string, fromID int64) bool {
	if fromChat == l.adminChatID {
		log.Printf("[DEBUG] message in admin chat %d, from %s (%d)", fromChat, from, fromID)
		if !l.SuperUsers.IsSuper(from, fromID) {
			log.Printf("[DEBUG] %s (%d) is not superuser in admin chat, ignored", from, fromID)
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
		if update.Message.ReplyToMessage.ForwardOrigin != nil {
			if update.Message.ReplyToMessage.ForwardOrigin.IsUser() {
				botChat.UserName = update.Message.ReplyToMessage.ForwardOrigin.SenderUser.UserName
			}
			if update.Message.ReplyToMessage.ForwardOrigin.IsHiddenUser() {
				botChat.UserName = update.Message.ReplyToMessage.ForwardOrigin.SenderUserName
			}
		}
	}
	return fmt.Sprintf("%v", botChat)
}

// NotificationType defines how a message is delivered to users
type NotificationType int

const (
	// NotificationDefault sends message with standard notification
	NotificationDefault NotificationType = iota
	// NotificationSilent sends message without sound
	NotificationSilent
)

// sendBotResponse sends bot's answer to tg channel
// actionText is a text for the button to unban user, optional
func (l *TelegramListener) sendBotResponse(resp bot.Response, chatID int64, notifyType NotificationType) error {
	if !resp.Send {
		return nil
	}

	log.Printf("[DEBUG] bot response - %+v, reply-to:%d", strings.ReplaceAll(resp.Text, "\n", "\\n"), resp.ReplyTo)
	tbMsg := tbapi.NewMessage(chatID, resp.Text)
	tbMsg.ParseMode = tbapi.ModeMarkdown
	tbMsg.LinkPreviewOptions = tbapi.LinkPreviewOptions{IsDisabled: true}
	tbMsg.ReplyParameters = tbapi.ReplyParameters{MessageID: resp.ReplyTo}
	tbMsg.DisableNotification = notifyType == NotificationSilent

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
// it uses the user ID first, but can match by username if set in the list of super-users.
func (l *TelegramListener) updateSupers() error {
	isSuper := func(username string, id int64) bool {
		for _, super := range l.SuperUsers {
			if super == fmt.Sprintf("%d", id) {
				return true
			}
			if username != "" && super == username {
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
		if admin.User.UserName == "" && admin.User.ID == 0 {
			continue
		}
		if isSuper(admin.User.UserName, admin.User.ID) {
			continue // already in the list
		}
		l.SuperUsers = append(l.SuperUsers, fmt.Sprintf("%d", admin.User.ID))
	}

	log.Printf("[INFO] added admins, full list of supers: {%s}", strings.Join(l.SuperUsers, ", "))
	if err != nil {
		return fmt.Errorf("error getting chat administrators: %w", err)
	}
	return nil
}

// SuperUsers for moderators. Can be either username or user ID.
type SuperUsers []string

// IsSuper checks if userID or username in the list of superusers
// First it treats super as user ID, then as username
func (s SuperUsers) IsSuper(userName string, userID int64) bool {
	for _, super := range s {
		if id, err := strconv.ParseInt(super, 10, 64); err == nil {
			// super is user ID
			if userID == id {
				return true
			}
			continue
		}
		// super is username
		if strings.EqualFold(userName, super) || strings.EqualFold("/"+userName, super) {
			return true
		}
	}
	return false
}
