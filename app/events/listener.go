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
	"log/slog"
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
	slog.Info("start telegram listener for", slog.Any("Group", l.Group))

	if l.TrainingMode {
		slog.Warn("training mode, no bans")
	}

	if l.SoftBanMode {
		slog.Warn("soft ban mode, no bans but restrictions")
	}

	// get chat ID for the group we are monitoring
	var getChatErr error
	if l.chatID, getChatErr = l.getChatID(l.Group); getChatErr != nil {
		return fmt.Errorf("failed to get chat ID for group %q: %w", l.Group, getChatErr)
	}

	if err := l.updateSupers(); err != nil {
		slog.Warn("failed to update superusers", slog.Any("error", err))
	}

	if l.AdminGroup != "" {
		// get chat ID for the admin group
		if l.adminChatID, getChatErr = l.getChatID(l.AdminGroup); getChatErr != nil {
			return fmt.Errorf("failed to get chat ID for admin group %q: %w", l.AdminGroup, getChatErr)
		}
		slog.Info(fmt.Sprintf("admin chat ID: %d", l.adminChatID))
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
			slog.Warn("failed to send startup message", slog.Any("error", err))
		}
	}

	l.adminHandler = &admin{tbAPI: l.TbAPI, bot: l.Bot, locator: l.Locator, primChatID: l.chatID, adminChatID: l.adminChatID,
		superUsers: l.SuperUsers, trainingMode: l.TrainingMode, softBan: l.SoftBanMode, dry: l.Dry, warnMsg: l.WarnMsg}

	adminForwardStatus := "enabled"
	if l.DisableAdminSpamForward {
		adminForwardStatus = "disabled"
	}
	slog.Debug(fmt.Sprintf("admin handler created, spam forwarding %s, %+v", adminForwardStatus, l.adminHandler))
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
			if update.Message != nil && l.isAdminChat(update.Message.Chat.ID, update.Message.From.UserName, update.Message.From.ID) {
				if l.DisableAdminSpamForward {
					continue
				}
				if err := l.adminHandler.MsgHandler(update); err != nil {
					msg := fmt.Sprintf("failed to process admin chat message: %v", err)
					slog.Warn(msg)
					_ = l.sendBotResponse(bot.Response{Send: true, Text: "error: " + err.Error()}, l.adminChatID)
				}
				continue
			}

			// handle admin chat inline buttons
			if update.CallbackQuery != nil {
				if err := l.adminHandler.InlineCallbackHandler(update.CallbackQuery); err != nil {
					msg := fmt.Sprintf("failed to process callback: %v", err)
					slog.Warn(msg)
					_ = l.sendBotResponse(bot.Response{Send: true, Text: "error: " + err.Error()}, l.adminChatID)
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
					msg := fmt.Sprintf("failed to process new chat member: %v", err)
					slog.Warn(msg)
				}
				continue
			}

			if update.Message.LeftChatMember != nil {
				if l.SuppressJoinMessage {
					err := l.procLeftChatMemberMessage(update)
					if err != nil {
						msg := fmt.Sprintf("[WARN] failed to process left chat member: %v", err)
						slog.Warn(msg)
					}

				}
				continue
			}

			// handle spam reports from superusers
			if update.Message.ReplyToMessage != nil && l.SuperUsers.IsSuper(update.Message.From.UserName, update.Message.From.ID) {
				if strings.EqualFold(update.Message.Text, "/spam") || strings.EqualFold(update.Message.Text, "spam") {
					slog.Debug(fmt.Sprintf("superuser %s reported spam", update.Message.From.UserName))
					if err := l.adminHandler.DirectSpamReport(update); err != nil {
						msg := fmt.Sprintf("failed to process direct spam report: %v", err)
						slog.Warn(msg)
					}
					continue
				}
				if strings.EqualFold(update.Message.Text, "/ban") || strings.EqualFold(update.Message.Text, "ban") {
					slog.Debug(fmt.Sprintf("superuser %s requested ban", update.Message.From.UserName))
					if err := l.adminHandler.DirectBanReport(update); err != nil {
						msg := fmt.Sprintf("failed to process direct ban request: %v", err)
						slog.Warn(msg)
					}
					continue
				}
				if strings.EqualFold(update.Message.Text, "/warn") || strings.EqualFold(update.Message.Text, "warn") {
					slog.Debug(fmt.Sprintf("superuser %s requested warning", update.Message.From.UserName))
					if err := l.adminHandler.DirectWarnReport(update); err != nil {
						msg := fmt.Sprintf("failed to process direct warning request: %v", err)
						slog.Warn(msg)
					}
					continue
				}
			}

			if err := l.procEvents(update); err != nil {
				slog.Warn(fmt.Sprintf("failed to process update: %v", err))
				continue
			}

		case <-time.After(l.IdleDuration): // hit bots on idle timeout
			resp := l.Bot.OnMessage(bot.Message{Text: "idle"}, false)
			if err := l.sendBotResponse(resp, l.chatID); err != nil {
				slog.Warn(fmt.Sprintf("failed to respond on idle: %v", err))
			}
		}
	}
}

// procNewChatMemberMessage saves new chat member message to locator. It is used to delete the message if the user kicked out
func (l *TelegramListener) procNewChatMemberMessage(update tbapi.Update) error {
	fromChat := update.Message.Chat.ID
	// ignore messages from other chats except the one we are monitor and ones from the test list
	if !l.isChatAllowed(fromChat) {
		return nil
	}

	if len(update.Message.NewChatMembers) != 1 {
		slog.Debug(fmt.Sprintf("we are expecting only one new chat member, got %d", len(update.Message.NewChatMembers)))
		return nil
	}

	errs := new(multierror.Error)

	member := update.Message.NewChatMembers[0]
	msg := fmt.Sprintf("new_%d_%d", fromChat, member.ID)
	if err := l.Locator.AddMessage(msg, fromChat, member.ID, "", update.Message.MessageID); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to add new chat member message to locator: %w", err))
	}

	return errs.ErrorOrNil()
}

// procLeftChatMemberMessage deletes the message about new chat member if the user kicked out
func (l *TelegramListener) procLeftChatMemberMessage(update tbapi.Update) error {
	fromChat := update.Message.Chat.ID
	// ignore messages from other chats except the one we are monitor and ones from the test list
	if !l.isChatAllowed(fromChat) {
		return nil
	}

	if update.Message.From.ID == update.Message.LeftChatMember.ID {
		slog.Debug("left chat member is the same as the message sender, ignored")
		return nil
	}
	msg, found := l.Locator.Message(fmt.Sprintf("new_%d_%d", fromChat, update.Message.LeftChatMember.ID))
	if !found {
		debugMsg := fmt.Sprintf("no new chat member message found for %d in chat %d", update.Message.LeftChatMember.ID, fromChat)
		slog.Debug(debugMsg)
		return nil
	}
	if _, err := l.TbAPI.Request(tbapi.DeleteMessageConfig{
		BaseChatMessage: tbapi.BaseChatMessage{ChatConfig: tbapi.ChatConfig{ChatID: fromChat}, MessageID: msg.MsgID},
	}); err != nil {
		return fmt.Errorf("failed to delete new chat member message %d: %w", msg.MsgID, err)
	}

	return nil
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

	slog.Debug(string(msgJSON))
	msg := transform(update.Message)

	// ignore empty messages
	if strings.TrimSpace(msg.Text) == "" && msg.Image == nil {
		return nil
	}

	slog.Debug(fmt.Sprintf("incoming msg: %+v", strings.ReplaceAll(msg.Text, "\n", " ")))
	slog.Debug(fmt.Sprintf("incoming msg details: %+v", msg))
	if err := l.Locator.AddMessage(msg.Text, fromChat, msg.From.ID, msg.From.Username, msg.ID); err != nil {
		msg := fmt.Sprintf("failed to add message to locator: %v", err)
		slog.Warn(msg)
	}
	resp := l.Bot.OnMessage(*msg, false)

	if !resp.Send { // not spam
		return nil
	}

	// send response to the channel if allowed
	if resp.Send && !l.NoSpamReply && !l.TrainingMode {
		if err := l.sendBotResponse(resp, fromChat); err != nil {
			slog.Warn(fmt.Sprintf("failed to respond on update: %v", err))
		}
	}

	errs := new(multierror.Error)

	// ban user if requested by bot
	if resp.Send && resp.BanInterval > 0 {
		slog.Debug(fmt.Sprintf("ban initiated for %+v", resp))
		l.SpamLogger.Save(msg, &resp)
		if err := l.Locator.AddSpam(msg.From.ID, resp.CheckResults); err != nil {
			slog.Warn(fmt.Sprintf("failed to add spam to locator: %v", err))
		}
		banUserStr := l.getBanUsername(resp, update)

		if l.SuperUsers.IsSuper(msg.From.Username, msg.From.ID) {
			if l.TrainingMode {
				l.adminHandler.ReportBan(banUserStr, msg)
			}
			slog.Debug(fmt.Sprintf("superuser %s requested ban, ignored", banUserStr))
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

func (l *TelegramListener) isAdminChat(fromChat int64, from string, fromID int64) bool {
	if fromChat == l.adminChatID {
		slog.Debug(fmt.Sprintf("message in admin chat %d, from %s (%d)", fromChat, from, fromID))
		if !l.SuperUsers.IsSuper(from, fromID) {
			slog.Debug(fmt.Sprintf("%s (%d) is not superuser in admin chat, ignored", from, fromID))
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

// sendBotResponse sends bot's answer to tg channel
// actionText is a text for the button to unban user, optional
func (l *TelegramListener) sendBotResponse(resp bot.Response, chatID int64) error {
	if !resp.Send {
		return nil
	}

	slog.Debug(fmt.Sprintf("bot response - %+v, reply-to:%d", strings.ReplaceAll(resp.Text, "\n", "\\n"), resp.ReplyTo))
	tbMsg := tbapi.NewMessage(chatID, resp.Text)
	tbMsg.ParseMode = tbapi.ModeMarkdown
	tbMsg.LinkPreviewOptions = tbapi.LinkPreviewOptions{IsDisabled: true}
	tbMsg.ReplyParameters = tbapi.ReplyParameters{MessageID: resp.ReplyTo}

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

	slog.Info(fmt.Sprintf("added admins, full list of supers: {%s}", strings.Join(l.SuperUsers, ", ")))
	return err
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
