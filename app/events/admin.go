package events

import (
	"fmt"
	"log"
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
	primChatID   int64
	adminChatID  int64
	trainingMode bool
	keepUser     bool
	dry          bool
}

// ReportBan a ban message to admin chat with a button to unban the user
func (a *admin) ReportBan(banUserStr string, msg *bot.Message) {
	log.Printf("[DEBUG] report to admin chat, ban msgsData for %s, group: %d", banUserStr, a.adminChatID)
	text := strings.ReplaceAll(escapeMarkDownV1Text(msg.Text), "\n", " ")
	forwardMsg := fmt.Sprintf("**permanently banned [%s](tg://user?id=%d)**\n\n%s\n\n", banUserStr, msg.From.ID, text)
	if err := a.sendWithUnbanMarkup(forwardMsg, "change ban", msg.From, a.adminChatID); err != nil {
		log.Printf("[WARN] failed to send admin message, %v", err)
	}
}

// MsgHandler handles messages received on admin chat. This is usually forwarded spam failed
// to be detected by the bot. We need to update spam filter with this message and ban the user.
func (a *admin) MsgHandler(update tbapi.Update) error {
	shrink := func(inp string, max int) string {
		if utf8.RuneCountInString(inp) <= max {
			return inp
		}
		return string([]rune(inp)[:max]) + "..."
	}
	log.Printf("[DEBUG] message from admin chat: msg id: %d, update id: %d, from: %s, sender: %s",
		update.Message.MessageID, update.UpdateID, update.Message.From.UserName, update.Message.ForwardSenderName)

	if update.Message.ForwardSenderName == "" && update.Message.ForwardFrom == nil {
		// this is a regular message from admin chat, not the forwarded one, ignore it
		return nil
	}

	// this is a forwarded message from super to admin chat, it is an example of missed spam
	// we need to update spam filter with this message
	msgTxt := strings.ReplaceAll(update.Message.Text, "\n", " ")
	log.Printf("[DEBUG] forwarded message from superuser %q to admin chat %d: %q", update.Message.From.UserName, a.adminChatID, msgTxt)

	// it would be nice to ban this user right away, but we don't have forwarded user ID here due to tg privacy limitation.
	// it is empty in update.Message. To ban this user, we need to get the match on the message from the locator and ban from there.
	info, ok := a.locator.Message(update.Message.Text)
	if !ok {
		return fmt.Errorf("not found %q in locator", shrink(update.Message.Text, 50))
	}

	log.Printf("[DEBUG] locator found message %s", info)
	errs := new(multierror.Error)

	// remove user from the approved list
	a.bot.RemoveApprovedUsers(info.UserID)

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
		info.UserName, info.UserID, spamInfoText)
	if err := a.send(tbapi.NewMessage(a.adminChatID, newMsgText)); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to send spap detection results to admin chat: %w", err))
	}

	// update spam samples
	if !a.dry {
		if err := a.bot.UpdateSpam(msgTxt); err != nil {
			return fmt.Errorf("failed to update spam for %q: %w", msgTxt, err)
		}
		log.Printf("[INFO] spam updated with %q", shrink(update.Message.Text, 50))
	}

	if a.dry || a.trainingMode {
		return nil
	}

	// delete message
	if _, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{ChatID: a.primChatID, MessageID: info.MsgID}); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", info.MsgID, err))
	} else {
		log.Printf("[INFO] message %d deleted", info.MsgID)
	}

	// ban user
	banReq := banRequest{duration: bot.PermanentBanDuration, userID: info.UserID, chatID: a.primChatID,
		tbAPI: a.tbAPI, dry: a.dry, training: a.trainingMode}

	if err := banUserOrChannel(banReq); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to ban user %d: %w", info.UserID, err))
	}

	log.Printf("[INFO] user %q (%d) banned", update.Message.ForwardSenderName, info.UserID)
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
	if strings.HasPrefix(callbackData, "?") {
		if err := a.callbackAskBanConfirmation(query); err != nil {
			return fmt.Errorf("failed to make ban confirmation dialog: %w", err)
		}
		log.Printf("[DEBUG] unban confirmation sent, chatID: %d, userID: %s, orig: %q", chatID, callbackData[:1], query.Message.Text)
		return nil
	}

	// if callback msgsData starts with "+", we should not unban the user, but rather clear the keyboard and add to spam samples
	if strings.HasPrefix(callbackData, "+") {
		if err := a.callbackBanConfirmed(query); err != nil {
			return fmt.Errorf("failed confirmation ban: %w", err)
		}
		log.Printf("[DEBUG] ban confirmation rejected, chatID: %d, userID: %s, orig: %q", chatID, callbackData, query.Message.Text)
		return nil
	}

	// if callback msgsData starts with "!", we should show a spam info details
	if strings.HasPrefix(callbackData, "!") {
		if err := a.callbackShowInfo(query); err != nil {
			return fmt.Errorf("failed to show spam info: %w", err)
		}
		log.Printf("[DEBUG] spam info sent, chatID: %d, userID: %s, orig: %q", chatID, callbackData, query.Message.Text)
		return nil
	}

	// callback msgsData here is userID, we should unban the user
	log.Printf("[DEBUG] unban action activated, chatID: %d, userID: %s, orig: %q", chatID, callbackData, query.Message.Text)
	if err := a.callbackUnbanConfirmed(query); err != nil {
		return fmt.Errorf("failed to unban user: %w", err)
	}
	log.Printf("[DEBUG] user unbanned, chatID: %d, userID: %s, orig: %q", chatID, callbackData, query.Message.Text)

	return nil
}

// callbackAskBanConfirmation sends a confirmation message to admin chat with two buttons: "unban" and "keep it banned"
// callback data: +userID
func (a *admin) callbackAskBanConfirmation(query *tbapi.CallbackQuery) error {
	callbackData := query.Data
	// replace button with confirmation/rejection buttons
	confirmationKeyboard := tbapi.NewInlineKeyboardMarkup(
		tbapi.NewInlineKeyboardRow(
			tbapi.NewInlineKeyboardButtonData("Unban for real", callbackData[1:]),     // remove "?" prefix
			tbapi.NewInlineKeyboardButtonData("Keep it banned", "+"+callbackData[1:]), // add "+" prefix
		),
	)
	editMsg := tbapi.NewEditMessageReplyMarkup(query.Message.Chat.ID, query.Message.MessageID, confirmationKeyboard)
	if err := a.send(editMsg); err != nil {
		return fmt.Errorf("failed to make confiramtion, chatID:%d, msgID:%d, %w", query.Message.Chat.ID, query.Message.MessageID, err)
	}
	return nil
}

// callbackBanConfirmed handles the callback when user kept banned
// it clears the keyboard and updates the message text with confirmation of ban kept in place.
// it also updates spam samples with the original message
// callback data: +userID
func (a *admin) callbackBanConfirmed(query *tbapi.CallbackQuery) error {
	// clear keyboard and update message text with confirmation
	updText := query.Message.Text + fmt.Sprintf("\n\n_ban confirmed by %s in %v_",
		query.From.UserName, time.Since(time.Unix(int64(query.Message.Date), 0)).Round(time.Second))
	editMsg := tbapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, updText)
	editMsg.ReplyMarkup = &tbapi.InlineKeyboardMarkup{InlineKeyboard: [][]tbapi.InlineKeyboardButton{}}
	if err := a.send(editMsg); err != nil {
		return fmt.Errorf("failed to clear confirmation, chatID:%d, msgID:%d, %w", query.Message.Chat.ID, query.Message.MessageID, err)
	}

	cleanMsg, err := a.getCleanMessage(query.Message.Text)
	if err != nil {
		return fmt.Errorf("failed to get clean message: %w", err)
	}
	if err := a.bot.UpdateSpam(cleanMsg); err != nil { // update spam samples
		return fmt.Errorf("failed to update spam for %q: %w", cleanMsg, err)
	}
	return nil
}

// callbackUnbanConfirmed handles the callback when user unbanned.
// it clears the keyboard and updates the message text with confirmation of unban.
// also it unbans the user, adds it to the approved list and updates ham samples with the original message.
// callback data: userID
func (a *admin) callbackUnbanConfirmed(query *tbapi.CallbackQuery) error {
	callbackData := query.Data
	chatID := query.Message.Chat.ID // this is ID of admin chat
	// callback msgsData here is userID, we should unban the user
	callbackResponse := tbapi.NewCallback(query.ID, "accepted")
	if _, err := a.tbAPI.Request(callbackResponse); err != nil {
		return fmt.Errorf("failed to send callback response: %w", err)
	}

	userID, err := strconv.ParseInt(callbackData, 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse callback msgsData %q: %w", callbackData, err)
	}

	// get the original spam message
	cleanMsg, err := a.getCleanMessage(query.Message.Text)
	if err != nil {
		return fmt.Errorf("failed to get clean message: %w", err)
	}
	// update ham samples, the original message is from the second line, remove newlines and spaces
	if derr := a.bot.UpdateHam(cleanMsg); derr != nil {
		return fmt.Errorf("failed to update ham for %q: %w", cleanMsg, derr)
	}

	// unban user if not in training mode
	if !a.trainingMode {
		_, err = a.tbAPI.Request(tbapi.UnbanChatMemberConfig{
			ChatMemberConfig: tbapi.ChatMemberConfig{UserID: userID, ChatID: a.primChatID}, OnlyIfBanned: a.keepUser})
		if err != nil {
			return fmt.Errorf("failed to unban user %d: %w", userID, err)
		}
	}

	// add user to the approved list
	a.bot.AddApprovedUsers(userID)

	// Create the original forwarded message with new indication of "unbanned" and an empty keyboard
	updText := query.Message.Text + fmt.Sprintf("\n\n_unbanned by %s in %v_",
		query.From.UserName, time.Since(time.Unix(int64(query.Message.Date), 0)).Round(time.Second))
	editMsg := tbapi.NewEditMessageText(chatID, query.Message.MessageID, updText)
	editMsg.ReplyMarkup = &tbapi.InlineKeyboardMarkup{InlineKeyboard: [][]tbapi.InlineKeyboardButton{}}
	if err := a.send(editMsg); err != nil {
		return fmt.Errorf("failed to edit message, chatID:%d, msgID:%d, %w", chatID, query.Message.MessageID, err)
	}
	return nil
}

// callbackShowInfo handles the callback when user asks for spam detection details for the ban.
// callback data: !userID
func (a *admin) callbackShowInfo(query *tbapi.CallbackQuery) error {
	callbackData := query.Data
	spamInfoText := "**can't get spam info**"
	spamInfo := []string{}
	userID, err := strconv.ParseInt(callbackData[1:], 10, 64)
	if err != nil {
		spamInfo = append(spamInfo, fmt.Sprintf("**failed to parse userID %q: %v**", callbackData[1:], err))
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
	if err := a.send(editMsg); err != nil {
		return fmt.Errorf("failed to send spam info, chatID:%d, msgID:%d, %w", query.Message.Chat.ID, query.Message.MessageID, err)
	}
	return nil
}

// getCleanMessage returns the original message without spam info and buttons and without newlines
func (a *admin) getCleanMessage(msg string) (string, error) {
	// the original message is from the second line, remove newlines and spaces
	msgLines := strings.Split(msg, "\n")
	if len(msgLines) < 2 {
		return "", fmt.Errorf("unexpected message from callback msgsData: %q", msg)
	}

	spamInfoLine := len(msgLines)
	for i, line := range msgLines {
		if strings.HasPrefix(line, "spam detection results:") {
			spamInfoLine = i
			break
		}
	}

	// Adjust the slice to include the line before spamInfoLine
	cleanMsg := strings.Join(msgLines[1:spamInfoLine], " ")
	cleanMsg = strings.TrimSpace(cleanMsg)
	return cleanMsg, nil
}

// send message to the telegram as markdown first and if failed - as plain text
func (a *admin) send(tbMsg tbapi.Chattable) error {
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
	if _, err := a.tbAPI.Send(msg); err != nil {
		log.Printf("[WARN] failed to send message as markdown, %v", err)
		msg = withParseMode(tbMsg, "") // try plain text
		if _, err := a.tbAPI.Send(msg); err != nil {
			return fmt.Errorf("can't send message to telegram: %w", err)
		}
	}
	return nil
}

// sendWithUnbanMarkup sends message to admin chat and add buttons to ui.
// text is message with details and action it for the button label to unban, which is user id prefixed with "? for confirmation
// second button is to show info about the spam analysis.
func (a *admin) sendWithUnbanMarkup(text, action string, user bot.User, chatID int64) error {
	log.Printf("[DEBUG] action response %q: user %+v, text: %q", action, user, strings.ReplaceAll(text, "\n", "\\n"))
	tbMsg := tbapi.NewMessage(chatID, text)
	tbMsg.ParseMode = tbapi.ModeMarkdown
	tbMsg.DisableWebPagePreview = true

	tbMsg.ReplyMarkup = tbapi.NewInlineKeyboardMarkup(
		tbapi.NewInlineKeyboardRow(
			tbapi.NewInlineKeyboardButtonData("⛔︎ "+action, fmt.Sprintf("?%d", user.ID)), // ?userID to request confirmation
			tbapi.NewInlineKeyboardButtonData("️⚑ info", fmt.Sprintf("!%d", user.ID)),    // !userID to request info
		),
	)

	if _, err := a.tbAPI.Send(tbMsg); err != nil {
		return fmt.Errorf("can't send message to telegram %q: %w", text, err)
	}
	return nil
}
