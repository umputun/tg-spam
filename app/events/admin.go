package events

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/hashicorp/go-multierror"

	"github.com/umputun/tg-spam/app/bot"
)

// admin is a helper to handle all admin-group related stuff, created by listener
// public methods kept public (on a private struct) to be able to recognize the api
type admin struct {
	tbAPI        TbAPI
	bot          Bot
	locator      Locator
	superUsers   SuperUsers
	primChatID   int64
	adminChatID  int64
	trainingMode bool
	softBan      bool // if true, the user not banned automatically, but only restricted
	dry          bool
	warnMsg      string
}

const (
	confirmationPrefix = "?"
	banPrefix          = "+"
	infoPrefix         = "!"
)

// ReportBan a ban message to admin chat with a button to unban the user
func (a *admin) ReportBan(banUserStr string, msg *bot.Message, spamReplyID int) {
	log.Printf("[DEBUG] report to admin chat, ban msgsData for %s, group: %d", banUserStr, a.adminChatID)
	text := strings.ReplaceAll(escapeMarkDownV1Text(msg.Text), "\n", " ")
	would := ""
	if a.dry {
		would = "would have "
	}
	forwardMsg := fmt.Sprintf("**%spermanently banned [%s](tg://user?id=%d)**\n\n%s\n\n", would, banUserStr, msg.From.ID, text)
	if err := a.sendWithUnbanMarkup(forwardMsg, "change ban", msg.From, msg.ID, a.adminChatID, spamReplyID); err != nil {
		log.Printf("[WARN] failed to send admin message, %v", err)
	}
}

// MsgHandler handles messages received on admin chat. this is usually forwarded spam failed
// to be detected by the bot. we need to update spam filter with this message and ban the user.
// the user will be baned even in training mode, but not in the dry mode.
func (a *admin) MsgHandler(update tbapi.Update) error {
	shrink := func(inp string, max int) string {
		if utf8.RuneCountInString(inp) <= max {
			return inp
		}
		return string([]rune(inp)[:max]) + "..."
	}

	// try to get the forwarded user ID, this is just for logging
	var fwdID int64
	if update.Message.ForwardFrom != nil {
		fwdID = update.Message.ForwardFrom.ID
	}

	log.Printf("[DEBUG] message from admin chat: msg id: %d, update id: %d, from: %s, sender: %q (%d)",
		update.Message.MessageID, update.UpdateID, update.Message.From.UserName,
		update.Message.ForwardSenderName, fwdID)

	if update.Message.ForwardSenderName == "" && update.Message.ForwardFrom == nil {
		// this is a regular message from admin chat, not the forwarded one, ignore it
		return nil
	}

	// this is a forwarded message from super to admin chat, it is an example of missed spam
	// we need to update spam filter with this message
	msgTxt := update.Message.Text
	if msgTxt == "" { // if no text, try to get it from the transformed message
		m := transform(update.Message)
		msgTxt = m.Text
	}
	log.Printf("[DEBUG] forwarded message from superuser %q (%d) to admin chat %d: %q",
		update.Message.From.UserName, update.Message.From.ID, a.adminChatID, msgTxt)

	// it would be nice to ban this user right away, but we don't have forwarded user ID here due to tg privacy limitation.
	// it is empty in update.Message. to ban this user, we need to get the match on the message from the locator and ban from there.
	info, ok := a.locator.Message(msgTxt)
	if !ok {
		return fmt.Errorf("not found %q in locator", shrink(msgTxt, 50))
	}

	log.Printf("[DEBUG] locator found message %s", info)
	errs := new(multierror.Error)

	// check if the forwarded message will ban a super-user and ignore it
	if info.UserName != "" && a.superUsers.IsSuper(info.UserName, info.UserID) {
		return fmt.Errorf("forwarded message is about super-user %s (%d), ignored", info.UserName, info.UserID)
	}

	// remove user from the approved list and from storage
	if err := a.bot.RemoveApprovedUser(info.UserID); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to remove user %d from approved list: %w", info.UserID, err))
	}

	// make a message with spam info and send to admin chat
	spamInfo := []string{}
	resp := a.bot.OnMessage(bot.Message{Text: update.Message.Text, From: bot.User{ID: info.UserID}})
	spamInfoText := "**can't get spam info**"
	for _, check := range resp.CheckResults {
		spamInfo = append(spamInfo, "- "+escapeMarkDownV1Text(check.String()))
	}
	if len(spamInfo) > 0 {
		spamInfoText = strings.Join(spamInfo, "\n")
	}
	newMsgText := fmt.Sprintf("**original detection results for %q (%d)**\n\n%s\n\n\n*the user banned and message deleted*",
		escapeMarkDownV1Text(info.UserName), info.UserID, spamInfoText)
	if err := send(tbapi.NewMessage(a.adminChatID, newMsgText), a.tbAPI); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to send spap detection results to admin chat: %w", err))
	}

	if a.dry {
		return errs.ErrorOrNil()
	}

	// update spam samples
	if err := a.bot.UpdateSpam(msgTxt); err != nil {
		return fmt.Errorf("failed to update spam for %q: %w", msgTxt, err)
	}

	// delete message
	if _, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{ChatID: a.primChatID, MessageID: info.MsgID}); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", info.MsgID, err))
	} else {
		log.Printf("[INFO] message %d deleted", info.MsgID)
	}

	// ban user
	banReq := banRequest{duration: bot.PermanentBanDuration, userID: info.UserID, chatID: a.primChatID,
		tbAPI: a.tbAPI, dry: a.dry, training: a.trainingMode, userName: update.Message.ForwardSenderName}

	if err := banUserOrChannel(banReq); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to ban user %d: %w", info.UserID, err))
	}

	return errs.ErrorOrNil()
}

// DirectSpamReport handles messages replayed with "/spam" or "spam" by admin
func (a *admin) DirectSpamReport(update tbapi.Update) error {
	return a.directReport(update, true)
}

// DirectBanReport handles messages replayed with "/ban" or "ban" by admin. doing all the same as DirectSpamReport
// but without updating spam samples
func (a *admin) DirectBanReport(update tbapi.Update) error {
	return a.directReport(update, false)
}

// DirectWarnReport handles messages replayed with "/warn" or "warn" by admin.
// it is removing the original message and posting a warning to the main chat as well as recording the warning th admin chat
func (a *admin) DirectWarnReport(update tbapi.Update) error {
	log.Printf("[DEBUG] direct warn by admin %q: msg id: %d, from: %q (%d)",
		update.Message.From.UserName, update.Message.ReplyToMessage.MessageID,
		update.Message.ReplyToMessage.From.UserName, update.Message.ReplyToMessage.From.ID)
	origMsg := update.Message.ReplyToMessage

	// this is a replayed message, it is an example of something we didn't like and want to issue a warning
	msgTxt := origMsg.Text
	if msgTxt == "" { // if no text, try to get it from the transformed message
		m := transform(origMsg)
		msgTxt = m.Text
	}
	log.Printf("[DEBUG] reported warn message from superuser %q (%d): %q", update.Message.From.UserName, update.Message.From.ID, msgTxt)
	// check if the reply message will ban a super-user and ignore it
	if origMsg.From.UserName != "" && a.superUsers.IsSuper(origMsg.From.UserName, origMsg.From.ID) {
		return fmt.Errorf("warn message is from super-user %s (%d), ignored", origMsg.From.UserName, origMsg.From.ID)
	}
	errs := new(multierror.Error)
	// delete original message
	if _, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{ChatID: a.primChatID, MessageID: origMsg.MessageID}); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", origMsg.MessageID, err))
	} else {
		log.Printf("[INFO] warn message %d deleted", origMsg.MessageID)
	}

	// delete reply message
	if _, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{ChatID: a.primChatID, MessageID: update.Message.MessageID}); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", update.Message.MessageID, err))
	} else {
		log.Printf("[INFO] admin warn reprot message %d deleted", update.Message.MessageID)
	}

	// make a warning message and replay to origMsg.MessageID
	warnMsg := fmt.Sprintf("warning from %s\n\n@%s %s", update.Message.From.UserName,
		origMsg.From.UserName, a.warnMsg)
	if err := send(tbapi.NewMessage(a.primChatID, escapeMarkDownV1Text(warnMsg)), a.tbAPI); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to send warning to main chat: %w", err))
	}

	return errs.ErrorOrNil()
}

// directReport handles messages replayed with "/spam" or "spam", or "/ban" or "ban" by admin
func (a *admin) directReport(update tbapi.Update, updateSamples bool) error {
	log.Printf("[DEBUG] direct ban by admin %q: msg id: %d, from: %q (%d)",
		update.Message.From.UserName, update.Message.ReplyToMessage.MessageID,
		update.Message.ReplyToMessage.From.UserName, update.Message.ReplyToMessage.From.ID)

	origMsg := update.Message.ReplyToMessage

	// this is a replayed message, it is an example of missed spam
	// we need to update spam filter with this message
	msgTxt := origMsg.Text
	if msgTxt == "" { // if no text, try to get it from the transformed message
		m := transform(origMsg)
		msgTxt = m.Text
	}
	log.Printf("[DEBUG] reported spam message from superuser %q (%d): %q", update.Message.From.UserName, update.Message.From.ID, msgTxt)

	// check if the reply message will ban a super-user and ignore it
	if origMsg.From.UserName != "" && a.superUsers.IsSuper(origMsg.From.UserName, origMsg.From.ID) {
		return fmt.Errorf("banned message is from super-user %s (%d), ignored", origMsg.From.UserName, origMsg.From.ID)
	}

	errs := new(multierror.Error)
	// remove user from the approved list and from storage
	if err := a.bot.RemoveApprovedUser(origMsg.From.ID); err != nil {
		// error here is not critical, user may not be in the approved list if we run in paranoid mode or
		// if not reached the threshold for approval yet
		log.Printf("[DEBUG] can't remove user %d from approved list: %v", origMsg.From.ID, err)
	}

	// make a message with spam info and send to admin chat
	spamInfo := []string{}
	resp := a.bot.OnMessage(bot.Message{Text: msgTxt, From: bot.User{ID: origMsg.From.ID}})
	spamInfoText := "**can't get spam info**"
	for _, check := range resp.CheckResults {
		spamInfo = append(spamInfo, "- "+escapeMarkDownV1Text(check.String()))
	}
	if len(spamInfo) > 0 {
		spamInfoText = strings.Join(spamInfo, "\n")
	}
	newMsgText := fmt.Sprintf("**original detection results for %s (%d)**\n\n%s\n\n%s\n\n\n*the user banned by %q and message deleted*",
		escapeMarkDownV1Text(origMsg.From.UserName), origMsg.From.ID, msgTxt, escapeMarkDownV1Text(spamInfoText),
		escapeMarkDownV1Text(update.Message.From.UserName))
	if err := send(tbapi.NewMessage(a.adminChatID, newMsgText), a.tbAPI); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to send spam detection results to admin chat: %w", err))
	}

	if a.dry {
		return errs.ErrorOrNil()
	}

	// update spam samples
	if updateSamples {
		if err := a.bot.UpdateSpam(msgTxt); err != nil {
			return fmt.Errorf("failed to update spam for %q: %w", msgTxt, err)
		}
	}

	// delete original message
	if _, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{ChatID: a.primChatID, MessageID: origMsg.MessageID}); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", origMsg.MessageID, err))
	} else {
		log.Printf("[INFO] spam message %d deleted", origMsg.MessageID)
	}

	// delete reply message
	if _, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{ChatID: a.primChatID, MessageID: update.Message.MessageID}); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", update.Message.MessageID, err))
	} else {
		log.Printf("[INFO] admin spam reprot message %d deleted", update.Message.MessageID)
	}

	// ban user
	banReq := banRequest{duration: bot.PermanentBanDuration, userID: origMsg.From.ID, chatID: a.primChatID,
		tbAPI: a.tbAPI, dry: a.dry, training: a.trainingMode, userName: update.Message.ForwardSenderName}

	if err := banUserOrChannel(banReq); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to ban user %d: %w", origMsg.From.ID, err))
	}

	return errs.ErrorOrNil()
}

// InlineCallbackHandler handles a callback from Telegram, which is a response to a message with inline keyboard.
// The callback contains user info, which is used to unban the user.
func (a *admin) InlineCallbackHandler(query *tbapi.CallbackQuery) error {
	callbackData := query.Data
	chatID := query.Message.Chat.ID // this is ID of admin chat
	if chatID != a.adminChatID {    // ignore callbacks from other chats, only admin chat is allowed
		return nil
	}

	// if callback msgsData starts with "?", we should show a confirmation message
	if strings.HasPrefix(callbackData, confirmationPrefix) {
		if err := a.callbackAskBanConfirmation(query); err != nil {
			return fmt.Errorf("failed to make ban confirmation dialog: %w", err)
		}
		log.Printf("[DEBUG] unban confirmation request sent, chatID: %d, userID: %s, orig: %q",
			chatID, callbackData[1:], query.Message.Text)
		return nil
	}

	// if callback msgsData starts with "+", we should not unban the user, but rather clear the keyboard and add to spam samples
	if strings.HasPrefix(callbackData, banPrefix) {
		if err := a.callbackBanConfirmed(query); err != nil {
			return fmt.Errorf("failed confirmation ban: %w", err)
		}
		log.Printf("[DEBUG] ban confirmed, chatID: %d, userID: %s, orig: %q", chatID, callbackData, query.Message.Text)
		return nil
	}

	// if callback msgsData starts with "!", we should show a spam info details
	if strings.HasPrefix(callbackData, infoPrefix) {
		if err := a.callbackShowInfo(query); err != nil {
			return fmt.Errorf("failed to show spam info: %w", err)
		}
		log.Printf("[DEBUG] spam info sent, chatID: %d, userID: %s, orig: %q", chatID, callbackData, query.Message.Text)
		return nil
	}

	// no prefix, callback msgsData here is userID, we should unban the user
	log.Printf("[DEBUG] unban action activated, chatID: %d, userID: %s, orig: %q", chatID, callbackData, query.Message.Text)
	if err := a.callbackUnbanConfirmed(query); err != nil {
		return fmt.Errorf("failed to unban user: %w", err)
	}
	log.Printf("[INFO] user unbanned, chatID: %d, userID: %s, orig: %q", chatID, callbackData, query.Message.Text)

	return nil
}

// callbackAskBanConfirmation sends a confirmation message to admin chat with two buttons: "unban" and "keep it banned"
// callback data: ?userID:msgID
func (a *admin) callbackAskBanConfirmation(query *tbapi.CallbackQuery) error {
	callbackData := query.Data

	keepBanned := "Keep it banned"
	if a.trainingMode {
		keepBanned = "Confirm ban"
	}

	// replace button with confirmation/rejection buttons
	confirmationKeyboard := tbapi.NewInlineKeyboardMarkup(
		tbapi.NewInlineKeyboardRow(
			tbapi.NewInlineKeyboardButtonData("Unban for real", callbackData[1:]),     // remove "?" prefix
			tbapi.NewInlineKeyboardButtonData(keepBanned, banPrefix+callbackData[1:]), // set "+" prefix
		),
	)
	editMsg := tbapi.NewEditMessageReplyMarkup(query.Message.Chat.ID, query.Message.MessageID, confirmationKeyboard)
	if err := send(editMsg, a.tbAPI); err != nil {
		return fmt.Errorf("failed to make confiramtion, chatID:%d, msgID:%d, %w", query.Message.Chat.ID, query.Message.MessageID, err)
	}
	return nil
}

// callbackBanConfirmed handles the callback when user kept banned
// it clears the keyboard and updates the message text with confirmation of ban kept in place.
// it also updates spam samples with the original message
// callback data: +userID:msgID
func (a *admin) callbackBanConfirmed(query *tbapi.CallbackQuery) error {
	// clear keyboard and update message text with confirmation
	updText := query.Message.Text + fmt.Sprintf("\n\n_ban confirmed by %s in %v_", query.From.UserName, a.sinceQuery(query))
	editMsg := tbapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, updText)
	editMsg.ReplyMarkup = &tbapi.InlineKeyboardMarkup{InlineKeyboard: [][]tbapi.InlineKeyboardButton{}}
	if err := send(editMsg, a.tbAPI); err != nil {
		return fmt.Errorf("failed to clear confirmation, chatID:%d, msgID:%d, %w", query.Message.Chat.ID, query.Message.MessageID, err)
	}

	cleanMsg, err := a.getCleanMessage(query.Message.Text)
	if err != nil {
		return fmt.Errorf("failed to get clean message: %w", err)
	}

	if err = a.bot.UpdateSpam(cleanMsg); err != nil { // update spam samples
		return fmt.Errorf("failed to update spam for %q: %w", cleanMsg, err)
	}

	userID, msgID, spamReplyID, parseErr := a.parseCallbackData(query.Data)
	if parseErr != nil {
		return fmt.Errorf("failed to parse callback's userID %q: %w", query.Data, parseErr)
	}

	if a.trainingMode {
		// in training mode, the user is not banned automatically, here we do the real ban & delete the message
		if err = a.deleteAndBan(query, userID, msgID); err != nil {
			return fmt.Errorf("failed to ban user %d: %w", userID, err)
		}
	}

	// for  soft ban we need to ba user for real on confirmation
	if a.softBan && !a.trainingMode {
		// we can delete our reply to the chat
		deleteMsg := tbapi.NewDeleteMessage(a.primChatID, spamReplyID)
		if err := send(deleteMsg, a.tbAPI); err != nil {
			log.Printf("[WARN] failed to delete our spam reply message %d: %v", spamReplyID, err)
		}
		userName, err := a.extractUsername(query.Message.Text) // try to extract username from the message
		if err != nil {
			log.Printf("[DEBUG] failed to extract username from %q: %v", query.Message.Text, err)
			userName = ""
		}
		banReq := banRequest{duration: bot.PermanentBanDuration, userID: userID, chatID: a.primChatID,
			tbAPI: a.tbAPI, dry: a.dry, training: a.trainingMode, userName: userName, restrict: false}
		if err := banUserOrChannel(banReq); err != nil {
			return fmt.Errorf("failed to ban user %d: %w", userID, err)
		}
	}

	return nil
}

// callbackUnbanConfirmed handles the callback when user unbanned.
// it clears the keyboard and updates the message text with confirmation of unban.
// also it unbans the user, adds it to the approved list and updates ham samples with the original message.
// callback data: userID:msgID
func (a *admin) callbackUnbanConfirmed(query *tbapi.CallbackQuery) error {
	callbackData := query.Data
	chatID := query.Message.Chat.ID // this is ID of admin chat
	log.Printf("[DEBUG] unban action activated, chatID: %d, userID: %s", chatID, callbackData)
	// callback msgsData here is userID, we should unban the user
	callbackResponse := tbapi.NewCallback(query.ID, "accepted")
	if _, err := a.tbAPI.Request(callbackResponse); err != nil {
		return fmt.Errorf("failed to send callback response: %w", err)
	}

	userID, _, spamReplyID, err := a.parseCallbackData(callbackData)
	if err != nil {
		return fmt.Errorf("failed to parse callback msgsData %q: %w", callbackData, err)
	}

	// get the original spam message to update ham samples
	cleanMsg, err := a.getCleanMessage(query.Message.Text)
	if err != nil {
		return fmt.Errorf("failed to get clean message: %w", err)
	}
	if derr := a.bot.UpdateHam(cleanMsg); derr != nil {
		return fmt.Errorf("failed to update ham for %q: %w", cleanMsg, derr)
	}

	// unban user if not in training mode (in training mode, the user is not banned automatically)
	if !a.trainingMode {
		if uerr := a.unban(userID); uerr != nil {
			return uerr
		}
	}

	// add user to the approved list
	name, err := a.extractUsername(query.Message.Text) // try to extract username from the message
	if err != nil {
		log.Printf("[DEBUG] failed to extract username from %q: %v", query.Message.Text, err)
		name = ""
	}
	if err := a.bot.AddApprovedUser(userID, name); err != nil { // name is not available here
		return fmt.Errorf("failed to add user %d to approved list: %w", userID, err)
	}

	// Create the original forwarded message with new indication of "unbanned" and an empty keyboard
	updText := query.Message.Text + fmt.Sprintf("\n\n_unbanned by %s in %v_", query.From.UserName, a.sinceQuery(query))

	// add spam info to the message
	if !strings.Contains(query.Message.Text, "spam detection results") && userID != 0 {
		spamInfoText := []string{"\n\n**original detection results**\n"}

		info, found := a.locator.Spam(userID)
		if found {
			for _, check := range info.Checks {
				spamInfoText = append(spamInfoText, "- "+escapeMarkDownV1Text(check.String()))
			}
		}

		if len(spamInfoText) > 1 {
			updText += strings.Join(spamInfoText, "\n")
		}
	}

	editMsg := tbapi.NewEditMessageText(chatID, query.Message.MessageID, updText)
	editMsg.ReplyMarkup = &tbapi.InlineKeyboardMarkup{InlineKeyboard: [][]tbapi.InlineKeyboardButton{}}
	if err := send(editMsg, a.tbAPI); err != nil {
		return fmt.Errorf("failed to edit message, chatID:%d, msgID:%d, %w", chatID, query.Message.MessageID, err)
	}

	// we can delete our reply to the chat
	if spamReplyID != 0 {
		deleteMsg := tbapi.NewDeleteMessage(a.primChatID, spamReplyID)
		if err := send(deleteMsg, a.tbAPI); err != nil {
			log.Printf("[WARN] failed to delete our spam reply message %d: %v", spamReplyID, err)
		}
	}
	return nil
}

func (a *admin) unban(userID int64) error {
	if a.softBan { // soft ban, just drop restrictions
		_, err := a.tbAPI.Request(tbapi.RestrictChatMemberConfig{
			ChatMemberConfig: tbapi.ChatMemberConfig{UserID: userID, ChatID: a.primChatID},
			Permissions: &tbapi.ChatPermissions{
				CanSendMessages:      true,
				CanSendMediaMessages: true,
				CanSendOtherMessages: true,
				CanSendPolls:         true,
				CanChangeInfo:        true,
				CanInviteUsers:       true,
				CanPinMessages:       true,
			},
		})
		if err != nil {
			return fmt.Errorf("failed to drop restrictions for user %d: %w", userID, err)
		}
		return nil
	}

	// hard ban, unban the user for real
	_, err := a.tbAPI.Request(tbapi.UnbanChatMemberConfig{
		ChatMemberConfig: tbapi.ChatMemberConfig{UserID: userID, ChatID: a.primChatID}, OnlyIfBanned: true})
	// onlyIfBanned seems to prevent user from being removed from the chat according to this confusing doc:
	// https://core.telegram.org/bots/api#unbanchatmember
	if err != nil {
		return fmt.Errorf("failed to unban user %d: %w", userID, err)
	}
	return nil
}

// callbackShowInfo handles the callback when user asks for spam detection details for the ban.
// callback data: !userID:msgID
func (a *admin) callbackShowInfo(query *tbapi.CallbackQuery) error {
	callbackData := query.Data
	spamInfoText := "**can't get spam info**"
	spamInfo := []string{}
	userID, _, _, err := a.parseCallbackData(callbackData)
	if err != nil {
		spamInfo = append(spamInfo, fmt.Sprintf("**failed to parse userID from %q: %v**", callbackData[1:], err))
	}

	// collect spam detection details
	if userID != 0 {
		info, found := a.locator.Spam(userID)
		if found {
			for _, check := range info.Checks {
				spamInfo = append(spamInfo, "- "+escapeMarkDownV1Text(check.String()))
			}
		}
		if len(spamInfo) > 0 {
			spamInfoText = strings.Join(spamInfo, "\n")
		}
	}

	updText := query.Message.Text + "\n\n**spam detection results**\n" + spamInfoText
	confirmationKeyboard := [][]tbapi.InlineKeyboardButton{}
	if query.Message.ReplyMarkup != nil && len(query.Message.ReplyMarkup.InlineKeyboard) > 0 {
		confirmationKeyboard = query.Message.ReplyMarkup.InlineKeyboard
		confirmationKeyboard[0] = confirmationKeyboard[0][:1] // remove second button (info)
	}
	editMsg := tbapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, updText)
	editMsg.ReplyMarkup = &tbapi.InlineKeyboardMarkup{InlineKeyboard: confirmationKeyboard}
	editMsg.ParseMode = tbapi.ModeMarkdown
	if err := send(editMsg, a.tbAPI); err != nil {
		return fmt.Errorf("failed to send spam info, chatID:%d, msgID:%d, %w", query.Message.Chat.ID, query.Message.MessageID, err)
	}
	return nil
}

// deleteAndBan deletes the message and bans the user
func (a *admin) deleteAndBan(query *tbapi.CallbackQuery, userID int64, msgID int) error {
	errs := new(multierror.Error)
	userName := a.locator.UserNameByID(userID)
	banReq := banRequest{
		duration: bot.PermanentBanDuration,
		userID:   userID,
		chatID:   a.primChatID,
		tbAPI:    a.tbAPI,
		dry:      a.dry,
		training: false, // reset training flag, ban for real
		userName: userName,
	}

	// check if user is super and don't ban if so
	msgFromSuper := userName != "" && a.superUsers.IsSuper(userName, userID)
	if !msgFromSuper {
		if err := banUserOrChannel(banReq); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to ban user %d: %w", userID, err))
		}
	}

	// we allow deleting messages from supers. This can be useful if super is training the bot by adding spam messages
	if _, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{ChatID: a.primChatID, MessageID: msgID}); err != nil {
		return fmt.Errorf("failed to delete message %d: %w", query.Message.MessageID, err)
	}

	// any errors happened above will be returned
	if errs.ErrorOrNil() != nil {
		errMsgs := []string{}
		for _, err := range errs.Errors {
			errStr := err.Error()
			errMsgs = append(errMsgs, errStr)
		}
		return errors.New(strings.Join(errMsgs, "\n")) // reformat to be md friendly
	}

	if msgFromSuper {
		log.Printf("[INFO] message %d deleted, user %q (%d) is super, not banned", msgID, userName, userID)
	} else {
		log.Printf("[INFO] message %d deleted, user %q (%d) banned", msgID, userName, userID)
	}
	return nil
}

// getCleanMessage returns the original message without spam info and buttons
// the messages in admin chat look like this:
//
//	permanently banned {6762723796 VladimirSokolov24 Владимир Соколов}
//
//	the original message is here
//	another line of the original message
//
// spam detection results
func (a *admin) getCleanMessage(msg string) (string, error) {
	// the original message is from the second line, remove newlines and spaces
	msgLines := strings.Split(msg, "\n")
	if len(msgLines) < 2 {
		return "", fmt.Errorf("unexpected message from callback msgsData: %q", msg)
	}

	spamInfoLine := len(msgLines)
	for i, line := range msgLines {
		if strings.HasPrefix(line, "spam detection results") || strings.HasPrefix(line, "**spam detection results**") {
			spamInfoLine = i
			break
		}
	}

	// ensure we have at least one line of content
	if spamInfoLine <= 2 {
		return "", fmt.Errorf("no original message found in callback msgsData: %q", msg)
	}

	cleanMsg := strings.Join(msgLines[2:spamInfoLine], "\n")
	return strings.TrimSpace(cleanMsg), nil
}

// sendWithUnbanMarkup sends a message to admin chat and adds buttons to ui.
// text is message with details and action it for the button label to unban, which is user id prefixed with "?" for confirmation;
// the second button is to show info about the spam analysis.
func (a *admin) sendWithUnbanMarkup(text, action string, user bot.User, msgID int, chatID int64, spamReplyID int) error {
	log.Printf("[DEBUG] action response %q: user %+v, msgID:%d, text: %q", action, user, msgID, strings.ReplaceAll(text, "\n", "\\n"))
	tbMsg := tbapi.NewMessage(chatID, text)
	tbMsg.ParseMode = tbapi.ModeMarkdown
	tbMsg.DisableWebPagePreview = true

	tbMsg.ReplyMarkup = tbapi.NewInlineKeyboardMarkup(
		tbapi.NewInlineKeyboardRow(
			// ?userID to request confirmation
			tbapi.NewInlineKeyboardButtonData("⛔︎ "+action, fmt.Sprintf("%s%d:%d:%d", confirmationPrefix, user.ID, msgID, spamReplyID)),
			// !userID to request info
			tbapi.NewInlineKeyboardButtonData("️⚑ info", fmt.Sprintf("%s%d:%d:%d", infoPrefix, user.ID, msgID, spamReplyID)),
		),
	)

	if _, err := a.tbAPI.Send(tbMsg); err != nil {
		return fmt.Errorf("can't send message to telegram %q: %w", text, err)
	}
	return nil
}

// callbackData is a string with userID, msgID and spamReplyID separated by ":"
func (a *admin) parseCallbackData(data string) (userID int64, msgID int, spamReplyID int, err error) {
	if len(data) < 4 {
		return 0, 0, 0, fmt.Errorf("unexpected callback data, too short %q", data)
	}

	// remove prefix if present from the parsed data
	if data[:1] == confirmationPrefix || data[:1] == banPrefix || data[:1] == infoPrefix {
		data = data[1:]
	}

	parts := strings.Split(data, ":")
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("unexpected callback data, should have 3 ids %q", data)
	}
	if userID, err = strconv.ParseInt(parts[0], 10, 64); err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse userID %q: %w", parts[0], err)
	}
	if msgID, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse msgID %q: %w", parts[1], err)
	}

	if spamReplyID, err = strconv.Atoi(parts[2]); err != nil {
		return 0, 0, 0, fmt.Errorf("failed to parse spamReplyID %q: %w", parts[2], err)
	}

	return userID, msgID, spamReplyID, nil
}

// extractUsername tries to extract the username from a ban message
func (a *admin) extractUsername(text string) (string, error) {
	// regex for markdown format: [username](tg://user?id=123456)
	markdownRegex := regexp.MustCompile(`\[(.*?)\]\(tg://user\?id=\d+\)`)
	matches := markdownRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1], nil
	}

	// regex for plain format: {200312168 umputun Umputun U}
	plainRegex := regexp.MustCompile(`\{\d+ (\S+) .+?\}`)
	matches = plainRegex.FindStringSubmatch(text)
	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", errors.New("username not found")
}

// sinceQuery calculates the time elapsed since the message of the query was sent
func (a *admin) sinceQuery(query *tbapi.CallbackQuery) time.Duration {
	res := time.Since(time.Unix(int64(query.Message.Date), 0)).Round(time.Second)
	if res < 0 { // negative duration possible if clock is not in sync with tg times and a message is from the future
		res = 0
	}
	return res
}
