package events

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	tbapi "github.com/OvyFlash/telegram-bot-api"
	"github.com/hashicorp/go-multierror"

	"github.com/umputun/tg-spam/app/bot"
)

// admin is a helper to handle all admin-group related stuff, created by listener
// public methods kept public (on a private struct) to be able to recognize the api
type admin struct {
	tbAPI                  TbAPI
	bot                    Bot
	locator                Locator
	superUsers             SuperUsers
	primChatID             int64
	adminChatID            int64
	trainingMode           bool
	softBan                bool // if true, the user not banned automatically, but only restricted
	dry                    bool
	warnMsg                string
	aggressiveCleanup      bool
	aggressiveCleanupLimit int
}

const (
	confirmationPrefix = "?"
	banPrefix          = "+"
	infoPrefix         = "!"
)

// ReportBan a ban message to admin chat with a button to unban the user
func (a *admin) ReportBan(banUserStr string, msg *bot.Message) {
	log.Printf("[DEBUG] report to admin chat, ban msgsData for %s, group: %d", banUserStr, a.adminChatID)
	text := strings.ReplaceAll(escapeMarkDownV1Text(msg.Text), "\n", " ")
	would := ""
	if a.dry {
		would = "would have "
	}

	// use channel identity for callback data when message is from a channel,
	// so that unban/info buttons operate on the actual channel, not the shared Channel_Bot user
	callbackUser := msg.From
	if msg.SenderChat.ID != 0 {
		callbackUser = bot.User{ID: msg.SenderChat.ID, Username: msg.SenderChat.UserName}
	}

	// for channels, use t.me link (tg://user doesn't resolve negative IDs);
	// for regular users, keep the standard tg://user link
	banLine := fmt.Sprintf("**%spermanently banned [%s](tg://user?id=%d)**",
		would, escapeMarkDownV1Text(banUserStr), msg.From.ID)
	switch {
	case msg.SenderChat.ID != 0 && msg.SenderChat.UserName != "":
		banLine = fmt.Sprintf("**%spermanently banned [%s](https://t.me/%s)**",
			would, escapeMarkDownV1Text(banUserStr), msg.SenderChat.UserName)
	case msg.SenderChat.ID != 0:
		banLine = fmt.Sprintf("**%spermanently banned %s (%d)**",
			would, escapeMarkDownV1Text(banUserStr), msg.SenderChat.ID)
	}
	forwardMsg := fmt.Sprintf("%s\n\n%s\n\n", banLine, text)
	if err := a.sendWithUnbanMarkup(forwardMsg, "change ban", callbackUser, msg.ID, a.adminChatID); err != nil {
		log.Printf("[WARN] failed to send admin message, %v", err)
	}
}

// MsgHandler handles messages received on admin chat. this is usually forwarded spam failed
// to be detected by the bot. we need to update spam filter with this message and ban the user.
// the user will be banned even in training mode, but not in the dry mode.
// if the locator lookup fails but ForwardOrigin provides the original sender's user ID,
// a degraded fallback path is used: the user is banned and spam samples updated,
// but the original message cannot be deleted automatically.
func (a *admin) MsgHandler(update tbapi.Update) error {
	shrink := func(inp string, maxLen int) string {
		if utf8.RuneCountInString(inp) <= maxLen {
			return inp
		}
		return string([]rune(inp)[:maxLen]) + "..."
	}

	// get forwarded user ID and username; used for logging and fallback path when locator lookup fails
	fwdID, username := a.getForwardUsernameAndID(update)

	log.Printf("[DEBUG] message from admin chat: msg id: %d, update id: %d, from: %s, sender: %q (%d)",
		update.Message.MessageID, update.UpdateID, update.Message.From.UserName,
		username, fwdID)

	if username == "" && update.Message.ForwardOrigin == nil {
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

	if msgTxt == "" {
		return errors.New("empty message text")
	}

	log.Printf("[DEBUG] forwarded message from superuser %q (%d) to admin chat %d: %q",
		update.Message.From.UserName, update.Message.From.ID, a.adminChatID, msgTxt)

	// it would be nice to ban this user right away, but we don't have forwarded user ID here due to tg privacy limitation.
	// it is empty in update.Message. to ban this user, we need to get the match on the message from the locator and ban from there.
	info, ok := a.locator.Message(context.TODO(), msgTxt)
	if !ok {
		// locator lookup failed; if ForwardOrigin provides a user ID, use degraded fallback path
		if fwdID != 0 {
			return a.msgHandlerFallback(update, fwdID, username, msgTxt)
		}
		return fmt.Errorf("not found %q in locator", shrink(msgTxt, 50))
	}

	log.Printf("[DEBUG] locator found message %s", info)
	errs := new(multierror.Error)

	// check if the forwarded message will ban a super-user and ignore it
	if a.superUsers.IsSuper(info.UserName, info.UserID) {
		return fmt.Errorf("forwarded message is about super-user %s (%d), ignored", info.UserName, info.UserID)
	}

	// remove user from the approved list and from storage
	if err := a.bot.RemoveApprovedUser(info.UserID); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to remove user %d from approved list: %w", info.UserID, err))
	}

	// make a message with spam info and send to admin chat
	spamInfo := []string{}
	// check only, don't update the storage, as all we care here is to get checks results.
	// without checkOnly flag, it may add approved user to the storage after we removed it above.
	resp := a.bot.OnMessage(bot.Message{Text: update.Message.Text, From: bot.User{ID: info.UserID}}, true)
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
		if err := errs.ErrorOrNil(); err != nil {
			return fmt.Errorf("dry run errors: %w", err)
		}
		return nil
	}

	// update spam samples
	if err := a.bot.UpdateSpam(msgTxt); err != nil {
		return fmt.Errorf("failed to update spam for %q: %w", msgTxt, err)
	}

	// delete message
	_, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{
		BaseChatMessage: tbapi.BaseChatMessage{
			MessageID:  info.MsgID,
			ChatConfig: tbapi.ChatConfig{ChatID: a.primChatID},
		},
	})
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", info.MsgID, err))
	} else {
		log.Printf("[INFO] message %d deleted", info.MsgID)
	}

	// skip ban for anonymous admin posts - the locator may store them with the group's own chat ID,
	// and banning that would attempt to ban the group from posting in itself
	if info.UserID == a.primChatID {
		log.Printf("[WARN] skipping ban in MsgHandler, user ID %d matches group chat", a.primChatID)
	} else {
		banReq := banRequest{duration: bot.PermanentBanDuration, userID: info.UserID,
			channelID: channelIDFromCallback(info.UserID),
			chatID:    a.primChatID, tbAPI: a.tbAPI, dry: a.dry, training: a.trainingMode, userName: username}
		if err := banUserOrChannel(banReq); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to ban user %d: %w", info.UserID, err))
		}
	}

	if err := errs.ErrorOrNil(); err != nil {
		return fmt.Errorf("spam notification failed: %w", err)
	}
	return nil
}

// msgHandlerFallback handles the degraded fallback path when the locator lookup fails
// but ForwardOrigin provides the original sender's user ID. performs all possible actions
// (ban, spam update, remove from approved) and warns the admin that the original message
// must be deleted manually since we don't have the message ID from the primary chat.
func (a *admin) msgHandlerFallback(update tbapi.Update, fwdID int64, username, msgTxt string) error {
	log.Printf("[INFO] locator fallback: forwarded user %q (%d), processing without locator data", username, fwdID)
	errs := new(multierror.Error)

	// check if the forwarded user is a super-user and ignore if so
	if a.superUsers.IsSuper(username, fwdID) {
		return fmt.Errorf("forwarded message is about super-user %s (%d), ignored", username, fwdID)
	}

	// remove user from the approved list
	if err := a.bot.RemoveApprovedUser(fwdID); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to remove user %d from approved list: %w", fwdID, err))
	}

	// get detection results (check only, don't update storage)
	spamInfo := []string{}
	resp := a.bot.OnMessage(bot.Message{Text: update.Message.Text, From: bot.User{ID: fwdID}}, true)
	spamInfoText := "**can't get spam info**"
	for _, check := range resp.CheckResults {
		spamInfo = append(spamInfo, "- "+escapeMarkDownV1Text(check.String()))
	}
	if len(spamInfo) > 0 {
		spamInfoText = strings.Join(spamInfo, "\n")
	}

	// send detection results to admin chat (no unban button - msgID is unavailable without locator)
	detectionMsg := fmt.Sprintf("**original detection results for %q (%d)**\n\n%s\n\n\n*the user banned*",
		escapeMarkDownV1Text(username), fwdID, spamInfoText)
	if err := send(tbapi.NewMessage(a.adminChatID, detectionMsg), a.tbAPI); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to send spam detection results to admin chat: %w", err))
	}

	if a.dry {
		// warn admin about manual deletion even in dry mode
		warnMsg := fmt.Sprintf("⚠ *locator fallback* (dry mode): user %q (%d), original message needs manual deletion",
			escapeMarkDownV1Text(username), fwdID)
		if err := send(tbapi.NewMessage(a.adminChatID, warnMsg), a.tbAPI); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to send fallback warning: %w", err))
		}
		if err := errs.ErrorOrNil(); err != nil {
			return fmt.Errorf("dry run errors: %w", err)
		}
		return nil
	}

	// update spam samples
	if err := a.bot.UpdateSpam(msgTxt); err != nil {
		return fmt.Errorf("failed to update spam for %q: %w", msgTxt, err)
	}

	// ban user (no message deletion - we don't have the message ID from primary chat)
	banReq := banRequest{duration: bot.PermanentBanDuration, userID: fwdID, chatID: a.primChatID,
		tbAPI: a.tbAPI, dry: a.dry, training: a.trainingMode, userName: username}
	if err := banUserOrChannel(banReq); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to ban user %d: %w", fwdID, err))
	}

	// warn admin that the original message must be deleted manually
	snippet := msgTxt
	if len([]rune(snippet)) > 100 {
		snippet = string([]rune(snippet)[:100]) + "..."
	}
	warnMsg := fmt.Sprintf("⚠ *locator fallback*: original message from %q (%d) needs manual deletion\n\n_%s_",
		escapeMarkDownV1Text(username), fwdID, escapeMarkDownV1Text(snippet))
	if err := send(tbapi.NewMessage(a.adminChatID, warnMsg), a.tbAPI); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to send fallback warning: %w", err))
	}

	if err := errs.ErrorOrNil(); err != nil {
		return fmt.Errorf("spam notification failed: %w", err)
	}
	return nil
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

// DirectWarnReport handles messages replied with "/warn" or "warn" by admin.
// it removes the original message and posts a warning to the main chat.
// for channel messages, the warning targets the channel name instead of the shared Channel_Bot user.
func (a *admin) DirectWarnReport(update tbapi.Update) error {
	warnLogFrom := update.Message.ReplyToMessage.From.UserName
	warnLogID := update.Message.ReplyToMessage.From.ID
	if sc := update.Message.ReplyToMessage.SenderChat; sc != nil && sc.ID != 0 {
		warnLogFrom = a.channelDisplayName(sc)
		warnLogID = sc.ID
	}
	log.Printf("[DEBUG] direct warn by admin %q: msg id: %d, from: %q (%d)",
		update.Message.From.UserName, update.Message.ReplyToMessage.MessageID, warnLogFrom, warnLogID)
	origMsg := update.Message.ReplyToMessage

	// this is a replayed message, it is an example of something we didn't like and want to issue a warning
	msgTxt := origMsg.Text
	if msgTxt == "" { // if no text, try to get it from the transformed message
		m := transform(origMsg)
		msgTxt = m.Text
	}
	log.Printf("[DEBUG] reported warn message from superuser %q (%d): %q",
		update.Message.From.UserName, update.Message.From.ID, msgTxt)
	// check if the reply message will ban a super-user and ignore it
	if origMsg.From.UserName != "" && a.superUsers.IsSuper(origMsg.From.UserName, origMsg.From.ID) {
		return fmt.Errorf("warn message is from super-user %s (%d), ignored", origMsg.From.UserName, origMsg.From.ID)
	}
	errs := new(multierror.Error)
	// delete original message
	_, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
		MessageID:  origMsg.MessageID,
		ChatConfig: tbapi.ChatConfig{ChatID: a.primChatID},
	}})
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", origMsg.MessageID, err))
	} else {
		log.Printf("[INFO] warn message %d deleted", origMsg.MessageID)
	}

	// delete reply message
	_, err = a.tbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
		MessageID:  update.Message.MessageID,
		ChatConfig: tbapi.ChatConfig{ChatID: a.primChatID},
	}})
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", update.Message.MessageID, err))
	} else {
		log.Printf("[INFO] admin warn reprot message %d deleted", update.Message.MessageID)
	}

	// make a warning message and replay to origMsg.MessageID
	warnTarget := "@" + origMsg.From.UserName
	if origMsg.SenderChat != nil && origMsg.SenderChat.ID != 0 && origMsg.SenderChat.ID != a.primChatID {
		chName := a.channelDisplayName(origMsg.SenderChat)
		if origMsg.SenderChat.UserName != "" {
			warnTarget = "@" + chName
		} else {
			warnTarget = chName
		}
	}
	warnMsg := fmt.Sprintf("warning from %s\n\n%s %s", update.Message.From.UserName,
		warnTarget, a.warnMsg)
	if err := send(tbapi.NewMessage(a.primChatID, escapeMarkDownV1Text(warnMsg)), a.tbAPI); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to send warning to main chat: %w", err))
	}

	if err := errs.ErrorOrNil(); err != nil {
		return fmt.Errorf("direct warn report failed: %w", err)
	}
	return nil
}

// returns the user ID and username from the tg update if's forwarded message,
// or just username in case sender is hidden user
func (a *admin) getForwardUsernameAndID(update tbapi.Update) (fwdID int64, username string) {
	if update.Message.ForwardOrigin != nil {
		if update.Message.ForwardOrigin.IsUser() {
			return update.Message.ForwardOrigin.SenderUser.ID, update.Message.ForwardOrigin.SenderUser.UserName
		}
		if update.Message.ForwardOrigin.IsHiddenUser() {
			return 0, update.Message.ForwardOrigin.SenderUserName
		}
	}
	return 0, ""
}

// channelDisplayName resolves a display name for a channel from its tbapi.Chat.
// returns UserName if set, else Title if set, else "channel_<ID>".
func (a *admin) channelDisplayName(ch *tbapi.Chat) string {
	if ch == nil {
		return ""
	}
	if ch.UserName != "" {
		return ch.UserName
	}
	if ch.Title != "" {
		return ch.Title
	}
	return fmt.Sprintf("channel_%d", ch.ID)
}

// deleteUserMessages deletes all recent messages from a user with rate limiting
func (a *admin) deleteUserMessages(userID int64) (deleted int, err error) {
	ctx := context.Background()
	msgIDs, err := a.locator.GetUserMessageIDs(ctx, userID, a.aggressiveCleanupLimit)
	if err != nil {
		return 0, fmt.Errorf("failed to get user messages: %w", err)
	}

	// rate limit: telegram allows 30 msg/sec, we use 35ms delay (~28.6 msg/sec) to stay safely below
	rateLimiter := time.NewTicker(35 * time.Millisecond)
	defer rateLimiter.Stop()

	const maxConsecutiveFailures = 5
	consecutiveFailures := 0
	failed := 0

	for _, msgID := range msgIDs {
		<-rateLimiter.C

		if consecutiveFailures >= maxConsecutiveFailures {
			return deleted, fmt.Errorf("stopped after %d consecutive failures (deleted %d, failed %d)",
				maxConsecutiveFailures, deleted, failed)
		}

		_, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{
			BaseChatMessage: tbapi.BaseChatMessage{
				MessageID:  msgID,
				ChatConfig: tbapi.ChatConfig{ChatID: a.primChatID},
			},
		})
		if err == nil {
			deleted++
			consecutiveFailures = 0 // reset on success
		} else {
			failed++
			consecutiveFailures++
			// continue on error - message might already be deleted
		}
	}

	if failed > 0 {
		log.Printf("[INFO] aggressive cleanup completed: deleted %d messages, failed %d", deleted, failed)
	}
	return deleted, nil
}

// directReport handles messages replayed with "/spam" or "spam", or "/ban" or "ban" by admin
func (a *admin) directReport(update tbapi.Update, updateSamples bool) error {
	logFrom := update.Message.ReplyToMessage.From.UserName
	logID := update.Message.ReplyToMessage.From.ID
	if sc := update.Message.ReplyToMessage.SenderChat; sc != nil && sc.ID != 0 {
		logFrom = a.channelDisplayName(sc)
		logID = sc.ID
	}
	log.Printf("[DEBUG] direct ban by admin %q: msg id: %d, from: %q (%d)",
		update.Message.From.UserName, update.Message.ReplyToMessage.MessageID, logFrom, logID)

	origMsg := update.Message.ReplyToMessage

	// this is a replayed message, it is an example of missed spam
	// we need to update spam filter with this message
	msgTxt := origMsg.Text
	if msgTxt == "" { // if no text, try to get it from the transformed message
		m := transform(origMsg)
		msgTxt = m.Text
	}
	log.Printf("[DEBUG] reported spam message from superuser %q (%d): %q",
		update.Message.From.UserName, update.Message.From.ID, msgTxt)

	// check if the reply message will ban a super-user and ignore it
	if origMsg.From.UserName != "" && a.superUsers.IsSuper(origMsg.From.UserName, origMsg.From.ID) {
		return fmt.Errorf("banned message is from super-user %s (%d), ignored", origMsg.From.UserName, origMsg.From.ID)
	}

	errs := new(multierror.Error)

	// detect channel message and set channel ID early, needed for approval removal and diagnostics.
	// skip when SenderChat.ID equals the group's own chat ID (anonymous admin post)
	var channelID int64
	if origMsg.SenderChat != nil && origMsg.SenderChat.ID != 0 && origMsg.SenderChat.ID != a.primChatID {
		channelID = origMsg.SenderChat.ID
	}

	// remove user or channel from the approved list and from storage.
	// use channel ID for channel posts since approval is tracked per-channel, not per Channel_Bot
	removeID := origMsg.From.ID
	if channelID != 0 {
		removeID = channelID
	}
	if err := a.bot.RemoveApprovedUser(removeID); err != nil {
		// error here is not critical, user may not be in the approved list if we run in paranoid mode or
		// if not reached the threshold for approval yet
		log.Printf("[DEBUG] can't remove user %d from approved list: %v", removeID, err)
	}

	// make a message with spam info and send to admin chat
	spamInfo := []string{}
	// check only, don't update the storage with the new approved user as all we care here is to get checks results.
	// pass SenderChat for channel posts so diagnostics match runtime spam checks
	diagMsg := bot.Message{Text: msgTxt, From: bot.User{ID: origMsg.From.ID}}
	if origMsg.SenderChat != nil && origMsg.SenderChat.ID != 0 {
		diagMsg.SenderChat = bot.SenderChat{ID: origMsg.SenderChat.ID, UserName: origMsg.SenderChat.UserName}
	}
	resp := a.bot.OnMessage(diagMsg, true)
	spamInfoText := "**can't get spam info**"
	for _, check := range resp.CheckResults {
		spamInfo = append(spamInfo, "- "+escapeMarkDownV1Text(check.String()))
	}
	if len(spamInfo) > 0 {
		spamInfoText = strings.Join(spamInfo, "\n")
	}
	displayName := origMsg.From.UserName
	displayID := origMsg.From.ID
	if channelID != 0 {
		displayName = a.channelDisplayName(origMsg.SenderChat)
		displayID = channelID
	}
	newMsgText := fmt.Sprintf("**original detection results for %s (%d)**\n\n%s\n\n%s\n\n\n"+
		"*the user banned by %q and message deleted*",
		escapeMarkDownV1Text(displayName), displayID, msgTxt, escapeMarkDownV1Text(spamInfoText),
		escapeMarkDownV1Text(update.Message.From.UserName))
	if err := send(tbapi.NewMessage(a.adminChatID, newMsgText), a.tbAPI); err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to send spam detection results to admin chat: %w", err))
	}

	if a.dry {
		if err := errs.ErrorOrNil(); err != nil {
			return fmt.Errorf("dry run errors: %w", err)
		}
		return nil
	}

	// update spam samples
	if updateSamples && msgTxt != "" {
		if err := a.bot.UpdateSpam(msgTxt); err != nil {
			return fmt.Errorf("failed to update spam for %q: %w", msgTxt, err)
		}
	}

	// delete original message
	_, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
		MessageID:  origMsg.MessageID,
		ChatConfig: tbapi.ChatConfig{ChatID: a.primChatID},
	}})
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", origMsg.MessageID, err))
	} else {
		log.Printf("[INFO] spam message %d deleted", origMsg.MessageID)
	}

	// delete reply message
	_, err = a.tbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
		MessageID:  update.Message.MessageID,
		ChatConfig: tbapi.ChatConfig{ChatID: a.primChatID},
	}})
	if err != nil {
		errs = multierror.Append(errs, fmt.Errorf("failed to delete message %d: %w", update.Message.MessageID, err))
	} else {
		log.Printf("[INFO] admin spam reprot message %d deleted", update.Message.MessageID)
	}

	_, username := a.getForwardUsernameAndID(update)
	if username == "" && channelID != 0 && origMsg.SenderChat != nil {
		username = a.channelDisplayName(origMsg.SenderChat)
	}

	// skip ban and cleanup for anonymous admin posts - banning the shared system bot user
	// (GroupAnonymousBot) would affect all anonymous admin messages in the group
	if origMsg.SenderChat != nil && origMsg.SenderChat.ID == a.primChatID {
		log.Printf("[WARN] skipping ban for anonymous admin post, sender chat %d matches group chat", a.primChatID)
	} else {
		// ban user or channel
		banReq := banRequest{duration: bot.PermanentBanDuration, userID: origMsg.From.ID, channelID: channelID,
			chatID: a.primChatID, tbAPI: a.tbAPI, dry: a.dry, training: a.trainingMode, userName: username}

		if err := banUserOrChannel(banReq); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to ban user %d: %w", origMsg.From.ID, err))
		}
	}

	// aggressive cleanup - delete all messages from the spammer (non-blocking)
	// use channel ID for lookup when the message was sent on behalf of a channel;
	// skip for anonymous admin posts (channelID == 0 and SenderChat matches group)
	cleanupUserID := origMsg.From.ID
	if channelID != 0 {
		cleanupUserID = channelID
	}
	if a.aggressiveCleanup && !a.dry && (origMsg.SenderChat == nil || origMsg.SenderChat.ID != a.primChatID) {
		go func() {
			deleted, err := a.deleteUserMessages(cleanupUserID)
			if err != nil {
				log.Printf("[WARN] aggressive cleanup failed: %v", err)
				return
			}
			if deleted > 0 {
				cleanupName := origMsg.From.UserName
				if origMsg.SenderChat != nil && origMsg.SenderChat.UserName != "" {
					cleanupName = origMsg.SenderChat.UserName
				}
				log.Printf("[INFO] aggressive cleanup: deleted %d messages from %d", deleted, cleanupUserID)
				notifyMsg := fmt.Sprintf("_deleted %d messages from spammer %q (%d)_",
					deleted, escapeMarkDownV1Text(cleanupName), cleanupUserID)
				if err := send(tbapi.NewMessage(a.adminChatID, notifyMsg), a.tbAPI); err != nil {
					log.Printf("[WARN] failed to send deletion notification: %v", err)
				}
			}
		}()
	}

	if err := errs.ErrorOrNil(); err != nil {
		return fmt.Errorf("spam notification failed: %w", err)
	}
	return nil
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
	updText := query.Message.Text + fmt.Sprintf("\n\n_ban confirmed by %s in %v_", query.From.UserName, sinceQuery(query))
	editMsg := tbapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, updText)
	editMsg.ReplyMarkup = &tbapi.InlineKeyboardMarkup{InlineKeyboard: [][]tbapi.InlineKeyboardButton{}}
	if err := send(editMsg, a.tbAPI); err != nil {
		return fmt.Errorf("failed to clear confirmation, chatID:%d, msgID:%d, %w", query.Message.Chat.ID, query.Message.MessageID, err)
	}

	if cleanMsg, err := a.getCleanMessage(query.Message.Text); err == nil && cleanMsg != "" {
		if err = a.bot.UpdateSpam(cleanMsg); err != nil { // update spam samples
			return fmt.Errorf("failed to update spam for %q: %w", cleanMsg, err)
		}
	} else {
		// we don't want to fail on this error, as lack of a clean message should not prevent deleteAndBan
		// for soft and training modes, we just don't need to update spam samples with empty messages.
		log.Printf("[DEBUG] failed to get clean message: %v", err)
	}

	userID, msgID, parseErr := parseCallbackData(query.Data)
	if parseErr != nil {
		return fmt.Errorf("failed to parse callback's userID %q: %w", query.Data, parseErr)
	}

	if a.trainingMode {
		// in training mode, the user is not banned automatically, here we do the real ban & delete the message
		if err := a.deleteAndBan(query, userID, msgID); err != nil {
			return fmt.Errorf("failed to ban user %d: %w", userID, err)
		}
	}

	// for soft ban we need to ban user for real on confirmation
	if a.softBan && !a.trainingMode {
		userName, err := a.extractUsername(query.Message.Text) // try to extract username from the message
		if err != nil {
			log.Printf("[DEBUG] failed to extract username from %q: %v", query.Message.Text, err)
			userName = ""
		}
		banReq := banRequest{duration: bot.PermanentBanDuration, userID: userID, channelID: channelIDFromCallback(userID),
			chatID: a.primChatID, tbAPI: a.tbAPI, dry: a.dry, training: a.trainingMode, userName: userName, restrict: false}
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

	userID, _, err := parseCallbackData(callbackData)
	if err != nil {
		return fmt.Errorf("failed to parse callback msgsData %q: %w", callbackData, err)
	}

	// get the original spam message to update ham samples
	if cleanMsg, cleanErr := a.getCleanMessage(query.Message.Text); cleanErr == nil && cleanMsg != "" {
		// update ham samples if we have a clean message
		if upErr := a.bot.UpdateHam(cleanMsg); upErr != nil {
			return fmt.Errorf("failed to update ham for %q: %w", cleanMsg, upErr)
		}
	} else {
		// we don't want to fail on this error, as lack of a clean message should not prevent unban action
		log.Printf("[DEBUG] failed to get clean message: %v", cleanErr)
	}

	// unban user or channel if not in training mode (in training mode, the ban is not applied automatically)
	if !a.trainingMode {
		if userID < 0 {
			// negative ID indicates a channel - use channel-specific unban
			if uerr := a.unbanChannel(userID); uerr != nil {
				return uerr
			}
		} else {
			if uerr := a.unban(userID); uerr != nil {
				return uerr
			}
		}
	}

	// add user to the approved list
	name, err := a.extractUsername(query.Message.Text) // try to extract username from the message
	if err != nil {
		log.Printf("[DEBUG] failed to extract username from %q: %v", query.Message.Text, err)
		name = ""
	}
	if err := a.bot.AddApprovedUser(userID, name); err != nil {
		return fmt.Errorf("failed to add user %d to approved list: %w", userID, err)
	}

	// create the original forwarded message with new indication of "unbanned" and an empty keyboard
	updText := query.Message.Text + fmt.Sprintf("\n\n_unbanned by %s in %v_", query.From.UserName, sinceQuery(query))

	// add spam info to the message
	if !strings.Contains(query.Message.Text, "spam detection results") && userID != 0 {
		spamInfoText := []string{"\n\n**original detection results**\n"}

		info, found := a.locator.Spam(context.TODO(), userID)
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
	return nil
}

func (a *admin) unban(userID int64) error {
	if a.softBan { // soft ban, just drop restrictions
		_, err := a.tbAPI.Request(tbapi.RestrictChatMemberConfig{
			ChatMemberConfig: tbapi.ChatMemberConfig{UserID: userID, ChatConfig: tbapi.ChatConfig{ChatID: a.primChatID}},
			Permissions: &tbapi.ChatPermissions{
				CanSendMessages:      true,
				CanSendAudios:        true,
				CanSendDocuments:     true,
				CanSendPhotos:        true,
				CanSendVideos:        true,
				CanSendVideoNotes:    true,
				CanSendVoiceNotes:    true,
				CanSendOtherMessages: true,
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
	cfg := tbapi.UnbanChatMemberConfig{
		ChatMemberConfig: tbapi.ChatMemberConfig{UserID: userID, ChatConfig: tbapi.ChatConfig{ChatID: a.primChatID}},
		OnlyIfBanned:     true,
	}
	_, err := a.tbAPI.Request(cfg)
	// onlyIfBanned seems to prevent user from being removed from the chat according to this confusing doc:
	// https://core.telegram.org/bots/api#unbanchatmember
	if err != nil {
		return fmt.Errorf("failed to unban user %d: %w", userID, err)
	}
	return nil
}

// unbanChannel unbans a previously banned channel (sender chat) from the group
func (a *admin) unbanChannel(channelID int64) error {
	_, err := a.tbAPI.Request(tbapi.UnbanChatSenderChatConfig{
		ChatConfig:   tbapi.ChatConfig{ChatID: a.primChatID},
		SenderChatID: channelID,
	})
	if err != nil {
		return fmt.Errorf("failed to unban channel %d: %w", channelID, err)
	}
	return nil
}

// callbackShowInfo handles the callback when user asks for spam detection details for the ban.
// callback data: !userID:msgID
func (a *admin) callbackShowInfo(query *tbapi.CallbackQuery) error {
	callbackData := query.Data
	spamInfoText := "**can't get spam info**"
	spamInfo := []string{}
	userID, _, err := parseCallbackData(callbackData)
	if err != nil {
		spamInfo = append(spamInfo, fmt.Sprintf("**failed to parse userID from %q: %v**", callbackData[1:], err))
	}

	// collect spam detection details
	if userID != 0 {
		info, found := a.locator.Spam(context.TODO(), userID)
		if found {
			for _, check := range info.Checks {
				spamInfo = append(spamInfo, "- "+escapeMarkDownV1Text(check.String()))
			}
		}
		if len(spamInfo) > 0 {
			spamInfoText = strings.Join(spamInfo, "\n")
		}
	}

	// escape markdown special characters to preserve user links when re-parsing rendered text
	// telegram returns rendered text without markdown syntax, so we need to escape before re-parsing
	escapedMessage := escapeMarkDownV1Text(query.Message.Text) + "\n\n**spam detection results**\n" + spamInfoText
	confirmationKeyboard := [][]tbapi.InlineKeyboardButton{}
	if query.Message.ReplyMarkup != nil && len(query.Message.ReplyMarkup.InlineKeyboard) > 0 {
		confirmationKeyboard = query.Message.ReplyMarkup.InlineKeyboard
		confirmationKeyboard[0] = confirmationKeyboard[0][:1] // remove second button (info)
	}
	editMsg := tbapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, escapedMessage)
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
	userName := a.locator.UserNameByID(context.TODO(), userID)
	banReq := banRequest{
		duration:  bot.PermanentBanDuration,
		userID:    userID,
		channelID: channelIDFromCallback(userID),
		chatID:    a.primChatID,
		tbAPI:     a.tbAPI,
		dry:       a.dry,
		training:  false, // reset training flag, ban for real
		userName:  userName,
	}

	// check if user is super and don't ban if so
	msgFromSuper := userName != "" && a.superUsers.IsSuper(userName, userID)
	if !msgFromSuper {
		if err := banUserOrChannel(banReq); err != nil {
			errs = multierror.Append(errs, fmt.Errorf("failed to ban user %d: %w", userID, err))
		}
	}

	// we allow deleting messages from supers. This can be useful if super is training the bot by adding spam messages
	_, err := a.tbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
		MessageID:  msgID,
		ChatConfig: tbapi.ChatConfig{ChatID: a.primChatID},
	}})
	if err != nil {
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
// text is message with details and action is the button label to unban,
// which is user id prefixed with "?" for confirmation.
// the second button is to show info about the spam analysis.
func (a *admin) sendWithUnbanMarkup(text, action string, user bot.User, msgID int, chatID int64) error {
	log.Printf("[DEBUG] action response %q: user %+v, msgID:%d, text: %q",
		action, user, msgID, strings.ReplaceAll(text, "\n", "\\n"))
	tbMsg := tbapi.NewMessage(chatID, text)
	tbMsg.ParseMode = tbapi.ModeMarkdown
	tbMsg.LinkPreviewOptions = tbapi.LinkPreviewOptions{IsDisabled: true}

	tbMsg.ReplyMarkup = tbapi.NewInlineKeyboardMarkup(
		tbapi.NewInlineKeyboardRow(
			// ?userID to request confirmation
			tbapi.NewInlineKeyboardButtonData("⛔︎ "+action, fmt.Sprintf("%s%d:%d", confirmationPrefix, user.ID, msgID)),
			// !userID to request info
			tbapi.NewInlineKeyboardButtonData("️⚑ info", fmt.Sprintf("%s%d:%d", infoPrefix, user.ID, msgID)),
		),
	)

	if _, err := a.tbAPI.Send(tbMsg); err != nil {
		return fmt.Errorf("can't send message to telegram %q: %w", text, err)
	}
	return nil
}

// extractUsername tries to extract the username from a ban message.
// supports tg://user markdown links, t.me channel links, plain channel name+ID, and plain {id name...} format.
func (a *admin) extractUsername(text string) (string, error) {
	// regex for markdown format: [username](tg://user?id=123456)
	markdownRegex := regexp.MustCompile(`\[(.*?)\]\(tg://user\?id=\d+\)`)
	if matches := markdownRegex.FindStringSubmatch(text); len(matches) > 1 {
		return matches[1], nil
	}

	// regex for t.me channel link format: [channelname](https://t.me/channelname)
	tmeRegex := regexp.MustCompile(`\[(.*?)\]\(https://t\.me/\S+\)`)
	if matches := tmeRegex.FindStringSubmatch(text); len(matches) > 1 {
		return matches[1], nil
	}

	// regex for plain channel format: permanently banned channelname (-100999888)
	// uses (.+?) to handle multi-word channel titles like "Spam News Channel"
	plainChannelRegex := regexp.MustCompile(`permanently banned (.+?) \(-?\d+\)`)
	if matches := plainChannelRegex.FindStringSubmatch(text); len(matches) > 1 {
		return matches[1], nil
	}

	// regex for plain format: {200312168 umputun Umputun U}
	plainRegex := regexp.MustCompile(`\{\d+ (\S+) .+?\}`)
	if matches := plainRegex.FindStringSubmatch(text); len(matches) > 1 {
		return matches[1], nil
	}

	return "", errors.New("username not found")
}
