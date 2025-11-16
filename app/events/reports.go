package events

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	tbapi "github.com/OvyFlash/telegram-bot-api"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/storage"
)

// ReportConfig is user spam reporting configuration
type ReportConfig struct {
	Storage          Reports       // reports storage for user spam reports
	Enabled          bool          // enable user spam reporting
	Threshold        int           // number of reports to trigger admin notification
	ApprovedOnly     bool          // restrict reporting to approved users only
	AutoBanThreshold int           // auto-ban after N reports (0=disabled)
	RateLimit        int           // max reports per user per period
	RatePeriod       time.Duration // rate limit time period
}

// userReports handles user spam reporting functionality
type userReports struct {
	ReportConfig
	tbAPI        TbAPI
	bot          Bot
	locator      Locator
	superUsers   SuperUsers
	primChatID   int64
	adminChatID  int64
	trainingMode bool
	softBanMode  bool
	dry          bool
}

// DirectUserReport handles messages replied with "/report" by regular users
func (r *userReports) DirectUserReport(ctx context.Context, update tbapi.Update) error {
	origMsg := update.Message.ReplyToMessage
	if origMsg == nil {
		return fmt.Errorf("must reply to a message to report it")
	}

	// validate reported message has a user (not from channel or anonymous admin)
	if origMsg.From == nil {
		log.Printf("[DEBUG] user report ignored: reported message from channel or anonymous admin")
		return fmt.Errorf("cannot report messages from channels or anonymous admins")
	}

	log.Printf("[DEBUG] user report: msg id: %d, reporter: %q (%d), reported: %q (%d)",
		origMsg.MessageID,
		update.Message.From.UserName, update.Message.From.ID,
		origMsg.From.UserName, origMsg.From.ID)

	// validate reporter is not super user (super users should use /spam instead)
	if r.superUsers.IsSuper(update.Message.From.UserName, update.Message.From.ID) {
		return fmt.Errorf("report from super-user %s (%d), use /spam instead", update.Message.From.UserName, update.Message.From.ID)
	}

	// validate reported user is not super user (check both username and ID)
	if r.superUsers.IsSuper(origMsg.From.UserName, origMsg.From.ID) {
		return fmt.Errorf("reported message is from super-user %s (%d), ignored", origMsg.From.UserName, origMsg.From.ID)
	}

	// validate reporter is approved user if approved-only mode enabled
	if r.ApprovedOnly && !r.bot.IsApprovedUser(update.Message.From.ID) {
		log.Printf("[INFO] report rejected: reporter %d (%s) not in approved list",
			update.Message.From.ID, update.Message.From.UserName)
		// still delete the /report command to keep chat clean
		_, _ = r.tbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
			MessageID:  update.Message.MessageID,
			ChatConfig: tbapi.ChatConfig{ChatID: r.primChatID},
		}})
		return fmt.Errorf("reporter %d not in approved list", update.Message.From.ID)
	}

	// check rate limit for reporter
	rateLimited, err := r.checkReportRateLimit(ctx, update.Message.From.ID)
	if err != nil {
		return fmt.Errorf("failed to check rate limit: %w", err)
	}
	if rateLimited {
		log.Printf("[INFO] reporter %d (%s) exceeded rate limit", update.Message.From.ID, update.Message.From.UserName)
		// still delete the /report command to keep chat clean
		_, _ = r.tbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
			MessageID:  update.Message.MessageID,
			ChatConfig: tbapi.ChatConfig{ChatID: r.primChatID},
		}})
		return fmt.Errorf("rate limit exceeded for reporter %d", update.Message.From.ID)
	}

	// delete the /report command message immediately to keep chat clean
	_, err = r.tbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
		MessageID:  update.Message.MessageID,
		ChatConfig: tbapi.ChatConfig{ChatID: r.primChatID},
	}})
	if err != nil {
		log.Printf("[WARN] failed to delete report message %d: %v", update.Message.MessageID, err)
	} else {
		log.Printf("[INFO] report message %d deleted", update.Message.MessageID)
	}

	// extract message text
	msgTxt := origMsg.Text
	if msgTxt == "" { // if no text, try to get it from the transformed message
		m := transform(origMsg)
		msgTxt = m.Text
	}

	// check if reports storage is initialized
	if r.Storage == nil {
		return fmt.Errorf("reports storage not initialized")
	}

	// create report
	report := storage.Report{
		MsgID:            origMsg.MessageID,
		ChatID:           r.primChatID,
		ReporterUserID:   update.Message.From.ID,
		ReporterUserName: update.Message.From.UserName,
		ReportedUserID:   origMsg.From.ID,
		ReportedUserName: origMsg.From.UserName,
		MsgText:          msgTxt,
	}

	// store report
	if err := r.Storage.Add(ctx, report); err != nil {
		return fmt.Errorf("failed to add report: %w", err)
	}

	// check if threshold reached
	if err := r.checkReportThreshold(ctx, origMsg.MessageID, r.primChatID); err != nil {
		log.Printf("[WARN] failed to check report threshold: %v", err)
	}

	return nil
}

// checkReportRateLimit checks if a reporter has exceeded their rate limit
// returns true if rate limit exceeded, false otherwise
func (r *userReports) checkReportRateLimit(ctx context.Context, reporterID int64) (bool, error) {
	if r.RateLimit <= 0 {
		// rate limiting disabled, no need for storage
		return false, nil
	}
	if r.Storage == nil {
		return false, fmt.Errorf("reports storage not initialized")
	}

	since := time.Now().Add(-r.RatePeriod)
	count, err := r.Storage.GetReporterCountSince(ctx, reporterID, since)
	if err != nil {
		return false, fmt.Errorf("failed to get reporter count: %w", err)
	}

	if count >= r.RateLimit {
		log.Printf("[DEBUG] reporter %d exceeded rate limit: %d >= %d", reporterID, count, r.RateLimit)
		return true, nil
	}

	return false, nil
}

// checkReportThreshold checks if report threshold is reached and sends admin notification if needed
func (r *userReports) checkReportThreshold(ctx context.Context, msgID int, chatID int64) error {
	if r.Storage == nil {
		return fmt.Errorf("reports storage not initialized")
	}

	// query all reports for this message
	reports, err := r.Storage.GetByMessage(ctx, msgID, chatID)
	if err != nil {
		return fmt.Errorf("failed to get reports: %w", err)
	}

	reportCount := len(reports)

	// check if auto-ban threshold reached
	if r.AutoBanThreshold > 0 && reportCount >= r.AutoBanThreshold {
		log.Printf("[INFO] auto-ban threshold reached for msgID:%d, chatID:%d: %d reports (threshold: %d)",
			msgID, chatID, reportCount, r.AutoBanThreshold)
		return r.executeAutoBan(ctx, reports)
	}

	// check if manual approval threshold reached
	if reportCount < r.Threshold {
		log.Printf("[DEBUG] report threshold not reached for msgID:%d, chatID:%d: %d < %d",
			msgID, chatID, reportCount, r.Threshold)
		return nil
	}

	log.Printf("[INFO] report threshold reached for msgID:%d, chatID:%d: %d reports",
		msgID, chatID, reportCount)

	// check if admin notification already sent
	if len(reports) > 0 && reports[0].NotificationSent {
		// notification already sent, update it
		log.Printf("[DEBUG] updating existing notification for msgID:%d, admin_msg_id:%d",
			msgID, reports[0].AdminMsgID)
		return r.updateReportNotification(ctx, reports)
	}

	// no notification sent yet, send new one
	log.Printf("[DEBUG] sending new notification for msgID:%d", msgID)
	return r.sendReportNotification(ctx, reports)
}

// executeAutoBan executes automatic ban when auto-ban threshold is reached
func (r *userReports) executeAutoBan(ctx context.Context, reports []storage.Report) error {
	if len(reports) == 0 {
		return fmt.Errorf("no reports provided")
	}

	// extract info from first report
	firstReport := reports[0]
	msgID := firstReport.MsgID
	chatID := firstReport.ChatID
	reportedUserID := firstReport.ReportedUserID
	reportedUserName := firstReport.ReportedUserName
	msgText := firstReport.MsgText

	log.Printf("[INFO] executing auto-ban for user %d (%s) based on %d reports",
		reportedUserID, reportedUserName, len(reports))

	// remove user from approved list
	if remErr := r.bot.RemoveApprovedUser(reportedUserID); remErr != nil {
		log.Printf("[DEBUG] can't remove user %d from approved list: %v", reportedUserID, remErr)
	}

	// update spam samples with message text (if not empty)
	if !r.dry && msgText != "" {
		if spamErr := r.bot.UpdateSpam(msgText); spamErr != nil {
			log.Printf("[WARN] failed to update spam samples: %v", spamErr)
		}
	}

	// delete reported message from primary chat
	if !r.dry {
		_, err := r.tbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
			MessageID:  msgID,
			ChatConfig: tbapi.ChatConfig{ChatID: chatID},
		}})
		if err != nil {
			log.Printf("[WARN] failed to delete reported message %d: %v", msgID, err)
		} else {
			log.Printf("[INFO] reported message %d auto-deleted", msgID)
		}
	}

	// ban reported user - CRITICAL: respect soft-ban mode
	banReq := banRequest{
		duration: bot.PermanentBanDuration,
		userID:   reportedUserID,
		chatID:   chatID,
		tbAPI:    r.tbAPI,
		dry:      r.dry,
		training: r.trainingMode,
		userName: reportedUserName,
		restrict: r.softBanMode, // IMPORTANT: use soft-ban if enabled
	}
	if err := banUserOrChannel(banReq); err != nil {
		log.Printf("[WARN] failed to auto-ban user %d: %v", reportedUserID, err)
	}

	// handle admin notification - update existing or send new
	// CRITICAL: only delete reports if notification succeeds, otherwise admin callbacks will fail
	var notificationErr error
	if r.adminChatID != 0 {
		if len(reports) > 0 && reports[0].NotificationSent {
			// notification already sent (manual threshold reached earlier), update it
			notificationErr = r.updateNotificationForAutoBan(reports)
		} else {
			// no previous notification, send new one
			notificationErr = r.sendAutoBanNotification(reports)
		}

		if notificationErr != nil {
			log.Printf("[WARN] failed to send/update auto-ban notification: %v", notificationErr)
			// don't delete reports - keep them so admin callbacks still work if buttons remain
			// reports will be cleaned up by cleanupOldReports() after 7 days if never resolved
			return fmt.Errorf("auto-ban executed for user %d but notification failed: %w", reportedUserID, notificationErr)
		}
	}

	// delete all reports for this message ONLY if notification succeeded (or no admin chat configured)
	if err := r.Storage.DeleteByMessage(ctx, msgID, chatID); err != nil {
		log.Printf("[WARN] failed to delete reports for msgID:%d: %v", msgID, err)
	}

	log.Printf("[INFO] auto-ban executed for user %d by %d reports", reportedUserID, len(reports))
	return nil
}

// sendAutoBanNotification sends notification to admin chat about automatic ban
func (r *userReports) sendAutoBanNotification(reports []storage.Report) error {
	if len(reports) == 0 {
		return fmt.Errorf("no reports provided")
	}

	firstReport := reports[0]
	reportedUserID := firstReport.ReportedUserID
	reportedUserName := firstReport.ReportedUserName

	// escape message text for markdown
	msgText := strings.ReplaceAll(escapeMarkDownV1Text(firstReport.MsgText), "\n", " ")
	msgText = truncateString(msgText, 200, "...")

	// format reporter list
	reporterList := make([]string, 0, len(reports))
	for _, report := range reports {
		reporterName := report.ReporterUserName
		if reporterName == "" {
			reporterName = fmt.Sprintf("user%d", report.ReporterUserID)
		}
		reporterList = append(reporterList, fmt.Sprintf("- [%s](tg://user?id=%d)",
			escapeMarkDownV1Text(reporterName), report.ReporterUserID))
	}

	actionType := "banned"
	if r.softBanMode {
		actionType = "restricted"
	}

	notificationText := fmt.Sprintf("**Auto-%s user after %d reports**\n\n[%s](tg://user?id=%d)\n\n%s\n\n**Reporters:**\n%s",
		actionType,
		len(reports),
		escapeMarkDownV1Text(reportedUserName),
		reportedUserID,
		msgText,
		strings.Join(reporterList, "\n"))

	// send to admin chat (no buttons - action already taken)
	tbMsg := tbapi.NewMessage(r.adminChatID, notificationText)
	tbMsg.ParseMode = tbapi.ModeMarkdown
	tbMsg.LinkPreviewOptions = tbapi.LinkPreviewOptions{IsDisabled: true}

	if _, err := r.tbAPI.Send(tbMsg); err != nil {
		return fmt.Errorf("failed to send auto-ban notification: %w", err)
	}

	log.Printf("[INFO] auto-ban notification sent to admin chat for user %d", reportedUserID)
	return nil
}

// updateNotificationForAutoBan updates existing admin notification when auto-ban is executed
// this prevents leaving orphaned notifications with live buttons that would fail on click
func (r *userReports) updateNotificationForAutoBan(reports []storage.Report) error {
	if len(reports) == 0 {
		return fmt.Errorf("reports list is empty")
	}

	firstReport := reports[0]
	adminMsgID := firstReport.AdminMsgID
	if adminMsgID == 0 {
		return fmt.Errorf("admin message ID is 0, cannot update")
	}

	reportedUserID := firstReport.ReportedUserID
	reportedUserName := firstReport.ReportedUserName

	// escape message text for markdown
	msgText := strings.ReplaceAll(escapeMarkDownV1Text(firstReport.MsgText), "\n", " ")
	msgText = truncateString(msgText, 200, "...")

	// format reporter list
	reporterList := make([]string, 0, len(reports))
	for _, report := range reports {
		reporterName := report.ReporterUserName
		if reporterName == "" {
			reporterName = fmt.Sprintf("user%d", report.ReporterUserID)
		}
		reporterList = append(reporterList, fmt.Sprintf("- [%s](tg://user?id=%d)",
			escapeMarkDownV1Text(reporterName), report.ReporterUserID))
	}

	actionType := "banned"
	if r.softBanMode {
		actionType = "restricted"
	}

	// create updated notification text with auto-ban confirmation
	updatedText := fmt.Sprintf("**User spam reported (%d reports)**\n\n[%s](tg://user?id=%d)\n\n%s\n\n**Reporters:**\n%s\n\n_auto-%s after reaching %d reports_",
		len(reports),
		escapeMarkDownV1Text(reportedUserName),
		reportedUserID,
		msgText,
		strings.Join(reporterList, "\n"),
		actionType,
		len(reports))

	// edit existing admin message, remove buttons
	editMsg := tbapi.NewEditMessageText(r.adminChatID, adminMsgID, updatedText)
	editMsg.ParseMode = "Markdown"
	editMsg.LinkPreviewOptions = tbapi.LinkPreviewOptions{IsDisabled: true}
	editMsg.ReplyMarkup = &tbapi.InlineKeyboardMarkup{InlineKeyboard: [][]tbapi.InlineKeyboardButton{}} // remove buttons

	if _, err := r.tbAPI.Send(editMsg); err != nil {
		return fmt.Errorf("failed to update notification for auto-ban (msgID:%d, adminMsgID:%d): %w",
			firstReport.MsgID, adminMsgID, err)
	}

	log.Printf("[INFO] updated admin notification %d for auto-ban of user %d", adminMsgID, reportedUserID)
	return nil
}

// sendReportNotification sends a new admin notification for user reports
func (r *userReports) sendReportNotification(ctx context.Context, reports []storage.Report) error {
	if len(reports) == 0 {
		return fmt.Errorf("no reports provided")
	}
	if r.adminChatID == 0 {
		log.Printf("[DEBUG] admin chat not configured, skipping notification")
		return nil
	}

	// extract info from first report (all reports have same message/reported user)
	firstReport := reports[0]
	msgID := firstReport.MsgID
	chatID := firstReport.ChatID
	reportedUserID := firstReport.ReportedUserID
	reportedUserName := firstReport.ReportedUserName

	// escape message text for markdown
	msgText := strings.ReplaceAll(escapeMarkDownV1Text(firstReport.MsgText), "\n", " ")
	msgText = truncateString(msgText, 200, "...")

	// format reporter list
	reporterList := make([]string, 0, len(reports))
	for _, report := range reports {
		reporterName := report.ReporterUserName
		if reporterName == "" {
			reporterName = fmt.Sprintf("user%d", report.ReporterUserID)
		}
		reporterList = append(reporterList, fmt.Sprintf("- [%s](tg://user?id=%d)",
			escapeMarkDownV1Text(reporterName), report.ReporterUserID))
	}

	// format notification message
	notificationText := fmt.Sprintf("**User spam reported (%d reports)**\n\n[%s](tg://user?id=%d)\n\n%s\n\n**Reporters:**\n%s",
		len(reports),
		escapeMarkDownV1Text(reportedUserName),
		reportedUserID,
		msgText,
		strings.Join(reporterList, "\n"))

	// add padding to ensure full-width buttons - telegram sizes buttons based on message text width
	padding := strings.Repeat("\u2800", 30) // braille pattern blank (U+2800) - invisible but takes width
	notificationText += "\n\n" + padding

	// create inline keyboard with action buttons
	// callback format: R+reportedUserID:msgID, R-reportedUserID:msgID, R?reportedUserID:msgID
	keyboard := tbapi.NewInlineKeyboardMarkup(
		tbapi.NewInlineKeyboardRow(
			tbapi.NewInlineKeyboardButtonData("✅ Approve Ban", fmt.Sprintf("R+%d:%d", reportedUserID, msgID)),
			tbapi.NewInlineKeyboardButtonData("❌ Reject", fmt.Sprintf("R-%d:%d", reportedUserID, msgID)),
			tbapi.NewInlineKeyboardButtonData("⛔️ Ban Reporter", fmt.Sprintf("R?%d:%d", reportedUserID, msgID)),
		),
	)

	// send to admin chat
	tbMsg := tbapi.NewMessage(r.adminChatID, notificationText)
	tbMsg.ParseMode = tbapi.ModeMarkdown
	tbMsg.LinkPreviewOptions = tbapi.LinkPreviewOptions{IsDisabled: true}
	tbMsg.ReplyMarkup = keyboard

	resp, err := r.tbAPI.Send(tbMsg)
	if err != nil {
		return fmt.Errorf("failed to send notification to admin chat: %w", err)
	}

	// update all reports with admin message ID
	if err := r.Storage.UpdateAdminMsgID(ctx, msgID, chatID, resp.MessageID); err != nil {
		log.Printf("[WARN] failed to update admin message ID for msgID:%d: %v", msgID, err)
		// don't fail - notification was sent successfully
	}

	log.Printf("[INFO] user report notification sent to admin chat: msgID:%d, reported:%s (%d), %d reports",
		msgID, reportedUserName, reportedUserID, len(reports))
	return nil
}

// updateReportNotification updates existing admin notification when new reports come in after threshold reached
func (r *userReports) updateReportNotification(_ context.Context, reports []storage.Report) error {
	// validate reports list
	if len(reports) == 0 {
		return fmt.Errorf("reports list is empty")
	}

	// skip if admin chat not configured
	if r.adminChatID == 0 {
		log.Printf("[DEBUG] admin chat not configured, skipping report notification update")
		return nil
	}

	// extract info from first report (all reports have same message/reported user/admin_msg_id)
	firstReport := reports[0]
	adminMsgID := firstReport.AdminMsgID
	msgID := firstReport.MsgID
	chatID := firstReport.ChatID
	reportedUserID := firstReport.ReportedUserID
	reportedUserName := firstReport.ReportedUserName

	// escape message text for markdown
	msgText := strings.ReplaceAll(escapeMarkDownV1Text(firstReport.MsgText), "\n", " ")
	msgText = truncateString(msgText, 200, "...")

	// format reporter list
	reporterList := make([]string, 0, len(reports))
	for _, report := range reports {
		reporterName := report.ReporterUserName
		if reporterName == "" {
			reporterName = fmt.Sprintf("user%d", report.ReporterUserID)
		}
		reporterList = append(reporterList, fmt.Sprintf("- [%s](tg://user?id=%d)",
			escapeMarkDownV1Text(reporterName), report.ReporterUserID))
	}

	// create notification message
	notification := fmt.Sprintf("**User spam reported (%d reports)**\n\n", len(reports)) +
		fmt.Sprintf("[%s](tg://user?id=%d)\n\n", escapeMarkDownV1Text(reportedUserName), reportedUserID) +
		fmt.Sprintf("%s\n\n", msgText) +
		fmt.Sprintf("**Reporters:**\n%s", strings.Join(reporterList, "\n"))

	// add padding to ensure full-width buttons - telegram sizes buttons based on message text width
	padding := strings.Repeat("\u2800", 30) // braille pattern blank (U+2800) - invisible but takes width
	notification += "\n\n" + padding

	// create inline keyboard with 3 buttons (same as sendReportNotification)
	keyboard := tbapi.NewInlineKeyboardMarkup(
		tbapi.NewInlineKeyboardRow(
			tbapi.NewInlineKeyboardButtonData("✅ Approve Ban", fmt.Sprintf("R+%d:%d", reportedUserID, msgID)),
			tbapi.NewInlineKeyboardButtonData("❌ Reject", fmt.Sprintf("R-%d:%d", reportedUserID, msgID)),
			tbapi.NewInlineKeyboardButtonData("⛔️ Ban Reporter", fmt.Sprintf("R?%d:%d", reportedUserID, msgID)),
		),
	)

	// edit existing admin message
	editMsg := tbapi.NewEditMessageText(r.adminChatID, adminMsgID, notification)
	editMsg.ParseMode = "Markdown"
	editMsg.LinkPreviewOptions = tbapi.LinkPreviewOptions{IsDisabled: true}
	editMsg.ReplyMarkup = &keyboard

	if _, err := r.tbAPI.Send(editMsg); err != nil {
		return fmt.Errorf("failed to edit admin notification for msgID:%d, chatID:%d: %w", msgID, chatID, err)
	}

	log.Printf("[INFO] updated report notification for msgID:%d (reported user:%d, %d reports total, admin_msg_id:%d)",
		msgID, reportedUserID, len(reports), adminMsgID)
	return nil
}

// callbackReportBan handles the callback when admin approves a user report and bans the reported user
// callback data: R+reportedUserID:msgID
func (r *userReports) callbackReportBan(ctx context.Context, query *tbapi.CallbackQuery) error {
	// parse callback data
	reportedUserID, msgID, err := parseCallbackData(query.Data)
	if err != nil {
		return fmt.Errorf("failed to parse callback data: %w", err)
	}

	// get reports from database to find chatID and message text
	reports, err := r.Storage.GetByMessage(ctx, msgID, r.primChatID)
	if err != nil {
		return fmt.Errorf("failed to get reports for msgID:%d: %w", msgID, err)
	}
	if len(reports) == 0 {
		return fmt.Errorf("no reports found for msgID:%d", msgID)
	}

	chatID := reports[0].ChatID
	msgText := reports[0].MsgText
	reportedUserName := reports[0].ReportedUserName

	// remove user from approved list
	if remErr := r.bot.RemoveApprovedUser(reportedUserID); remErr != nil {
		log.Printf("[DEBUG] can't remove user %d from approved list: %v", reportedUserID, remErr)
	}

	// update spam samples with message text (if not empty)
	if !r.dry && msgText != "" {
		if spamErr := r.bot.UpdateSpam(msgText); spamErr != nil {
			log.Printf("[WARN] failed to update spam samples: %v", spamErr)
		}
	}

	// delete reported message from primary chat
	if !r.dry {
		_, err = r.tbAPI.Request(tbapi.DeleteMessageConfig{BaseChatMessage: tbapi.BaseChatMessage{
			MessageID:  msgID,
			ChatConfig: tbapi.ChatConfig{ChatID: chatID},
		}})
		if err != nil {
			log.Printf("[WARN] failed to delete reported message %d: %v", msgID, err)
		} else {
			log.Printf("[INFO] reported message %d deleted", msgID)
		}
	}

	// ban reported user permanently (or restrict if soft-ban enabled)
	banReq := banRequest{
		duration: bot.PermanentBanDuration,
		userID:   reportedUserID,
		chatID:   chatID,
		tbAPI:    r.tbAPI,
		dry:      r.dry,
		training: r.trainingMode,
		userName: reportedUserName,
		restrict: r.softBanMode, // respect soft-ban mode
	}
	if err := banUserOrChannel(banReq); err != nil {
		log.Printf("[WARN] failed to ban user %d: %v", reportedUserID, err)
	}

	// delete all reports for this message
	if err := r.Storage.DeleteByMessage(ctx, msgID, chatID); err != nil {
		log.Printf("[WARN] failed to delete reports for msgID:%d: %v", msgID, err)
	}

	// update admin notification text with confirmation
	updText := query.Message.Text + fmt.Sprintf("\n\n_banned by %s in %v_", query.From.UserName, sinceQuery(query))
	editMsg := tbapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, updText)
	editMsg.ReplyMarkup = &tbapi.InlineKeyboardMarkup{InlineKeyboard: [][]tbapi.InlineKeyboardButton{}}
	if err := send(editMsg, r.tbAPI); err != nil {
		return fmt.Errorf("failed to update notification, chatID:%d, msgID:%d, %w",
			query.Message.Chat.ID, query.Message.MessageID, err)
	}

	log.Printf("[INFO] report ban approved for user %d by admin %s", reportedUserID, query.From.UserName)
	return nil
}

// callbackReportReject handles the callback when admin rejects a user report
// callback data: R-reportedUserID:msgID
func (r *userReports) callbackReportReject(ctx context.Context, query *tbapi.CallbackQuery) error {
	// parse callback data
	_, msgID, err := parseCallbackData(query.Data)
	if err != nil {
		return fmt.Errorf("failed to parse callback data: %w", err)
	}

	// get chatID from reports
	reports, err := r.Storage.GetByMessage(ctx, msgID, r.primChatID)
	if err != nil {
		return fmt.Errorf("failed to get reports for msgID:%d: %w", msgID, err)
	}
	if len(reports) == 0 {
		return fmt.Errorf("no reports found for msgID:%d", msgID)
	}

	chatID := reports[0].ChatID

	// delete all reports for this message
	if err := r.Storage.DeleteByMessage(ctx, msgID, chatID); err != nil {
		log.Printf("[WARN] failed to delete reports for msgID:%d: %v", msgID, err)
	}

	// update admin notification text with confirmation
	updText := query.Message.Text + fmt.Sprintf("\n\n_rejected by %s in %v_", query.From.UserName, sinceQuery(query))
	editMsg := tbapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, updText)
	editMsg.ReplyMarkup = &tbapi.InlineKeyboardMarkup{InlineKeyboard: [][]tbapi.InlineKeyboardButton{}}
	if err := send(editMsg, r.tbAPI); err != nil {
		return fmt.Errorf("failed to update notification, chatID:%d, msgID:%d, %w",
			query.Message.Chat.ID, query.Message.MessageID, err)
	}

	log.Printf("[INFO] report rejected by admin %s for msgID:%d", query.From.UserName, msgID)
	return nil
}

// callbackReportBanReporterAsk handles the callback when admin wants to ban a reporter (show confirmation)
// callback data: R?reportedUserID:msgID
func (r *userReports) callbackReportBanReporterAsk(ctx context.Context, query *tbapi.CallbackQuery) error {
	// parse callback data
	reportedUserID, msgID, err := parseCallbackData(query.Data)
	if err != nil {
		return fmt.Errorf("failed to parse callback data: %w", err)
	}

	// get all reports for this message
	reports, err := r.Storage.GetByMessage(ctx, msgID, r.primChatID)
	if err != nil {
		return fmt.Errorf("failed to get reports for msgID:%d: %w", msgID, err)
	}
	if len(reports) == 0 {
		return fmt.Errorf("no reports found for msgID:%d", msgID)
	}

	// generate inline keyboard with one button per reporter
	keyboard := make([][]tbapi.InlineKeyboardButton, 0, len(reports)+1)
	for _, report := range reports {
		reporterName := report.ReporterUserName
		if reporterName == "" {
			reporterName = fmt.Sprintf("user_%d", report.ReporterUserID)
		}
		button := tbapi.NewInlineKeyboardButtonData(
			fmt.Sprintf("Ban %s", reporterName),
			fmt.Sprintf("R!%d:%d", report.ReporterUserID, msgID),
		)
		keyboard = append(keyboard, []tbapi.InlineKeyboardButton{button})
	}

	// add cancel button in new row
	cancelButton := tbapi.NewInlineKeyboardButtonData(
		"Cancel",
		fmt.Sprintf("RX%d:%d", reportedUserID, msgID),
	)
	keyboard = append(keyboard, []tbapi.InlineKeyboardButton{cancelButton})

	// update buttons only (don't change text)
	editMsg := tbapi.NewEditMessageReplyMarkup(
		query.Message.Chat.ID,
		query.Message.MessageID,
		tbapi.InlineKeyboardMarkup{InlineKeyboard: keyboard},
	)
	if _, err := r.tbAPI.Send(editMsg); err != nil {
		return fmt.Errorf("failed to update keyboard, chatID:%d, msgID:%d, %w",
			query.Message.Chat.ID, query.Message.MessageID, err)
	}

	log.Printf("[INFO] ban reporter confirmation shown for msgID:%d", msgID)
	return nil
}

// callbackReportBanReporterConfirm handles the callback when admin confirms banning a specific reporter
// callback data: R!reporterID:msgID
func (r *userReports) callbackReportBanReporterConfirm(ctx context.Context, query *tbapi.CallbackQuery) error {
	// parse callback data
	reporterID, msgID, err := parseCallbackData(query.Data)
	if err != nil {
		return fmt.Errorf("failed to parse callback data: %w", err)
	}

	// get reports to find chatID and reporter details
	reports, err := r.Storage.GetByMessage(ctx, msgID, r.primChatID)
	if err != nil {
		return fmt.Errorf("failed to get reports for msgID:%d: %w", msgID, err)
	}
	if len(reports) == 0 {
		return fmt.Errorf("no reports found for msgID:%d", msgID)
	}

	chatID := reports[0].ChatID

	// find reporter details
	var reporterName string
	for _, report := range reports {
		if report.ReporterUserID == reporterID {
			reporterName = report.ReporterUserName
			break
		}
	}
	if reporterName == "" {
		reporterName = fmt.Sprintf("user_%d", reporterID)
	}

	// ban reporter permanently
	banReq := banRequest{
		duration: bot.PermanentBanDuration,
		userID:   reporterID,
		chatID:   chatID,
		tbAPI:    r.tbAPI,
		dry:      r.dry,
		training: r.trainingMode,
		userName: reporterName,
	}
	if banErr := banUserOrChannel(banReq); banErr != nil {
		log.Printf("[WARN] failed to ban reporter %d: %v", reporterID, banErr)
	}

	// delete reporter from database
	if delErr := r.Storage.DeleteReporter(ctx, reporterID, msgID, chatID); delErr != nil {
		log.Printf("[WARN] failed to delete reporter %d from database: %v", reporterID, delErr)
	}

	// get remaining reports
	remainingReports, err := r.Storage.GetByMessage(ctx, msgID, r.primChatID)
	if err != nil {
		log.Printf("[WARN] failed to get remaining reports for msgID:%d: %v", msgID, err)
	}

	if len(remainingReports) == 0 {
		// no reporters remain - delete all reports and update notification
		if delErr := r.Storage.DeleteByMessage(ctx, msgID, chatID); delErr != nil {
			log.Printf("[WARN] failed to delete reports for msgID:%d: %v", msgID, delErr)
		}

		updText := query.Message.Text + fmt.Sprintf("\n\n_all reporters banned by %s in %v_", query.From.UserName, sinceQuery(query))
		editMsg := tbapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, updText)
		editMsg.ReplyMarkup = &tbapi.InlineKeyboardMarkup{InlineKeyboard: [][]tbapi.InlineKeyboardButton{}}
		if err := send(editMsg, r.tbAPI); err != nil {
			return fmt.Errorf("failed to update notification, chatID:%d, msgID:%d, %w",
				query.Message.Chat.ID, query.Message.MessageID, err)
		}
	} else {
		// reporters remain - update text to remove banned reporter and restore original buttons
		// reconstruct notification text without the banned reporter
		reportedUserID := reports[0].ReportedUserID
		reportedUserName := reports[0].ReportedUserName

		// escape message text for markdown and flatten newlines
		msgText := strings.ReplaceAll(escapeMarkDownV1Text(remainingReports[0].MsgText), "\n", " ")
		msgText = truncateString(msgText, 200, "...")

		// build reporter list from remaining reports
		var reporterList []string
		for _, report := range remainingReports {
			rName := report.ReporterUserName
			if rName == "" {
				rName = fmt.Sprintf("user%d", report.ReporterUserID)
			}
			reporterList = append(reporterList, fmt.Sprintf("- [%s](tg://user?id=%d)",
				escapeMarkDownV1Text(rName), report.ReporterUserID))
		}

		updText := fmt.Sprintf("**User spam reported (%d reports)**\n\n[%s](tg://user?id=%d)\n\n%s\n\n**Reporters:**\n%s",
			len(remainingReports),
			escapeMarkDownV1Text(reportedUserName),
			reportedUserID,
			msgText,
			strings.Join(reporterList, "\n"))
		updText += fmt.Sprintf("\n\n_reporter %s banned by %s_", escapeMarkDownV1Text(reporterName), query.From.UserName)

		// add padding to ensure full-width buttons - telegram sizes buttons based on message text width
		padding := strings.Repeat("\u2800", 30) // braille pattern blank (U+2800) - invisible but takes width
		updText += "\n\n" + padding

		// restore original buttons
		keyboard := tbapi.NewInlineKeyboardMarkup(
			tbapi.NewInlineKeyboardRow(
				tbapi.NewInlineKeyboardButtonData("✅ Approve Ban", fmt.Sprintf("R+%d:%d", reportedUserID, msgID)),
				tbapi.NewInlineKeyboardButtonData("❌ Reject", fmt.Sprintf("R-%d:%d", reportedUserID, msgID)),
				tbapi.NewInlineKeyboardButtonData("⛔️ Ban Reporter", fmt.Sprintf("R?%d:%d", reportedUserID, msgID)),
			),
		)

		editMsg := tbapi.NewEditMessageText(query.Message.Chat.ID, query.Message.MessageID, updText)
		editMsg.ReplyMarkup = &keyboard
		if err := send(editMsg, r.tbAPI); err != nil {
			return fmt.Errorf("failed to update notification, chatID:%d, msgID:%d, %w",
				query.Message.Chat.ID, query.Message.MessageID, err)
		}
	}

	log.Printf("[INFO] reporter %s banned by admin %s for msgID:%d", reporterName, query.From.UserName, msgID)
	return nil
}

// callbackReportCancel handles the callback when admin cancels ban reporter action
// callback data: RXreportedUserID:msgID
func (r *userReports) callbackReportCancel(_ context.Context, query *tbapi.CallbackQuery) error {
	// parse callback data
	reportedUserID, msgID, err := parseCallbackData(query.Data)
	if err != nil {
		return fmt.Errorf("failed to parse callback data: %w", err)
	}

	// restore original button layout
	keyboard := tbapi.NewInlineKeyboardMarkup(
		tbapi.NewInlineKeyboardRow(
			tbapi.NewInlineKeyboardButtonData("✅ Approve Ban", fmt.Sprintf("R+%d:%d", reportedUserID, msgID)),
			tbapi.NewInlineKeyboardButtonData("❌ Reject", fmt.Sprintf("R-%d:%d", reportedUserID, msgID)),
			tbapi.NewInlineKeyboardButtonData("⛔️ Ban Reporter", fmt.Sprintf("R?%d:%d", reportedUserID, msgID)),
		),
	)

	// update buttons only (don't change text)
	editMsg := tbapi.NewEditMessageReplyMarkup(
		query.Message.Chat.ID,
		query.Message.MessageID,
		keyboard,
	)
	if _, err := r.tbAPI.Send(editMsg); err != nil {
		return fmt.Errorf("failed to restore keyboard, chatID:%d, msgID:%d, %w",
			query.Message.Chat.ID, query.Message.MessageID, err)
	}

	log.Printf("[INFO] ban reporter canceled by admin %s for msgID:%d", query.From.UserName, msgID)
	return nil
}

// HandleReportCallback routes report callbacks to the appropriate handler
func (r *userReports) HandleReportCallback(ctx context.Context, query *tbapi.CallbackQuery) error {
	chatID := query.Message.Chat.ID
	if chatID != r.adminChatID {
		return nil
	}

	callbackData := query.Data
	if len(callbackData) < 3 {
		return fmt.Errorf("invalid callback data: %s", callbackData)
	}

	switch callbackData[:2] {
	case "R+": // approve ban
		return r.callbackReportBan(ctx, query)
	case "R-": // reject report
		return r.callbackReportReject(ctx, query)
	case "R?": // ban reporter - show confirmation
		return r.callbackReportBanReporterAsk(ctx, query)
	case "R!": // ban specific reporter
		return r.callbackReportBanReporterConfirm(ctx, query)
	case "RX": // cancel ban reporter
		return r.callbackReportCancel(ctx, query)
	default:
		return fmt.Errorf("unknown report callback: %s", callbackData)
	}
}
