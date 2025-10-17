package events

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tbapi "github.com/OvyFlash/telegram-bot-api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/events/mocks"
	"github.com/umputun/tg-spam/app/storage"
)

func TestUserReports_checkReportRateLimit(t *testing.T) {
	ctx := context.Background()

	t.Run("rate limit exceeded", func(t *testing.T) {
		mockReports := &mocks.ReportsMock{
			GetReporterCountSinceFunc: func(ctx context.Context, reporterID int64, since time.Time) (int, error) {
				return 10, nil
			},
		}

		rep := &userReports{
			reports:          mockReports,
			reportRateLimit:  10,
			reportRatePeriod: 1 * time.Hour,
		}

		exceeded, err := rep.checkReportRateLimit(ctx, 123)
		require.NoError(t, err)
		assert.True(t, exceeded, "rate limit should be exceeded")
		require.Equal(t, 1, len(mockReports.GetReporterCountSinceCalls()))
	})

	t.Run("rate limit not exceeded", func(t *testing.T) {
		mockReports := &mocks.ReportsMock{
			GetReporterCountSinceFunc: func(ctx context.Context, reporterID int64, since time.Time) (int, error) {
				return 5, nil
			},
		}

		rep := &userReports{
			reports:          mockReports,
			reportRateLimit:  10,
			reportRatePeriod: 1 * time.Hour,
		}

		exceeded, err := rep.checkReportRateLimit(ctx, 123)
		require.NoError(t, err)
		assert.False(t, exceeded, "rate limit should not be exceeded")
		require.Equal(t, 1, len(mockReports.GetReporterCountSinceCalls()))
	})

	t.Run("rate limiting disabled", func(t *testing.T) {
		mockReports := &mocks.ReportsMock{
			GetReporterCountSinceFunc: func(ctx context.Context, reporterID int64, since time.Time) (int, error) {
				return 100, nil
			},
		}

		rep := &userReports{
			reports:          mockReports,
			reportRateLimit:  0,
			reportRatePeriod: 1 * time.Hour,
		}

		exceeded, err := rep.checkReportRateLimit(ctx, 123)
		require.NoError(t, err)
		assert.False(t, exceeded, "rate limit should be disabled")
		require.Equal(t, 0, len(mockReports.GetReporterCountSinceCalls()), "should not call GetReporterCountSince when disabled")
	})

	t.Run("reports storage not initialized", func(t *testing.T) {
		rep := &userReports{
			reports:          nil,
			reportRateLimit:  10,
			reportRatePeriod: 1 * time.Hour,
		}

		exceeded, err := rep.checkReportRateLimit(ctx, 123)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reports storage not initialized")
		assert.False(t, exceeded)
	})

	t.Run("database error", func(t *testing.T) {
		mockReports := &mocks.ReportsMock{
			GetReporterCountSinceFunc: func(ctx context.Context, reporterID int64, since time.Time) (int, error) {
				return 0, fmt.Errorf("database error")
			},
		}

		rep := &userReports{
			reports:          mockReports,
			reportRateLimit:  10,
			reportRatePeriod: 1 * time.Hour,
		}

		exceeded, err := rep.checkReportRateLimit(ctx, 123)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get reporter count")
		assert.False(t, exceeded)
	})
}

func TestUserReports_DirectUserReport(t *testing.T) {
	t.Run("successful report from regular user", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			GetReporterCountSinceFunc: func(ctx context.Context, reporterID int64, since time.Time) (int, error) {
				return 5, nil
			},
			AddFunc: func(ctx context.Context, report storage.Report) error {
				assert.Equal(t, int(999), report.MsgID)
				assert.Equal(t, int64(123), report.ChatID)
				assert.Equal(t, int64(111), report.ReporterUserID)
				assert.Equal(t, "reporter", report.ReporterUserName)
				assert.Equal(t, int64(666), report.ReportedUserID)
				assert.Equal(t, "spammer", report.ReportedUserName)
				assert.Equal(t, "spam message", report.MsgText)
				return nil
			},
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				return []storage.Report{}, nil // threshold not reached
			},
		}

		rep := &userReports{
			tbAPI:            mockAPI,
			primChatID:       123,
			adminChatID:      456,
			superUsers:       SuperUsers{"superuser"},
			reports:          mockReports,
			reportRateLimit:  10,
			reportRatePeriod: 1 * time.Hour,
			reportThreshold:  2,
		}

		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				Text:      "/report",
				From:      &tbapi.User{UserName: "reporter", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID: 999,
					From:      &tbapi.User{ID: 666, UserName: "spammer"},
					Text:      "spam message",
				},
			},
		}

		err := rep.DirectUserReport(context.Background(), update)
		require.NoError(t, err)
		assert.Equal(t, 1, len(mockAPI.RequestCalls()), "should delete /report message")
		assert.Equal(t, 1, len(mockReports.AddCalls()), "should add report to storage")
		assert.Equal(t, 1, len(mockReports.GetReporterCountSinceCalls()), "should check rate limit")
	})

	t.Run("reporter is superuser - should return error", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{}
		mockReports := &mocks.ReportsMock{}

		rep := &userReports{
			tbAPI:       mockAPI,
			primChatID:  123,
			adminChatID: 456,
			superUsers:  SuperUsers{"superuser"},
			reports:     mockReports,
		}

		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				Text:      "/report",
				From:      &tbapi.User{UserName: "superuser", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID: 999,
					From:      &tbapi.User{ID: 666, UserName: "spammer"},
					Text:      "spam message",
				},
			},
		}

		err := rep.DirectUserReport(context.Background(), update)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "use /spam instead")
		assert.Equal(t, 0, len(mockAPI.RequestCalls()), "should not delete message")
		assert.Equal(t, 0, len(mockReports.AddCalls()), "should not add report")
	})

	t.Run("reported user is superuser - should return error", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{}
		mockReports := &mocks.ReportsMock{}

		rep := &userReports{
			tbAPI:       mockAPI,
			primChatID:  123,
			adminChatID: 456,
			superUsers:  SuperUsers{"superuser"},
			reports:     mockReports,
		}

		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				Text:      "/report",
				From:      &tbapi.User{UserName: "reporter", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID: 999,
					From:      &tbapi.User{ID: 666, UserName: "superuser"},
					Text:      "some message",
				},
			},
		}

		err := rep.DirectUserReport(context.Background(), update)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "from super-user")
		assert.Equal(t, 0, len(mockAPI.RequestCalls()), "should not delete message")
		assert.Equal(t, 0, len(mockReports.AddCalls()), "should not add report")
	})

	t.Run("rate limit exceeded - should delete command and return error", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			GetReporterCountSinceFunc: func(ctx context.Context, reporterID int64, since time.Time) (int, error) {
				return 10, nil
			},
		}

		rep := &userReports{
			tbAPI:            mockAPI,
			primChatID:       123,
			adminChatID:      456,
			superUsers:       SuperUsers{},
			reports:          mockReports,
			reportRateLimit:  10,
			reportRatePeriod: 1 * time.Hour,
		}

		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				Text:      "/report",
				From:      &tbapi.User{UserName: "reporter", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID: 999,
					From:      &tbapi.User{ID: 666, UserName: "spammer"},
					Text:      "spam message",
				},
			},
		}

		err := rep.DirectUserReport(context.Background(), update)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "rate limit exceeded")
		assert.Equal(t, 1, len(mockAPI.RequestCalls()), "should still delete /report message")
		assert.Equal(t, 0, len(mockReports.AddCalls()), "should not add report when rate limited")
	})

	t.Run("reports storage add error", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			GetReporterCountSinceFunc: func(ctx context.Context, reporterID int64, since time.Time) (int, error) {
				return 5, nil
			},
			AddFunc: func(ctx context.Context, report storage.Report) error {
				return fmt.Errorf("database error")
			},
		}

		rep := &userReports{
			tbAPI:            mockAPI,
			primChatID:       123,
			adminChatID:      456,
			superUsers:       SuperUsers{},
			reports:          mockReports,
			reportRateLimit:  10,
			reportRatePeriod: 1 * time.Hour,
		}

		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				Text:      "/report",
				From:      &tbapi.User{UserName: "reporter", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID: 999,
					From:      &tbapi.User{ID: 666, UserName: "spammer"},
					Text:      "spam message",
				},
			},
		}

		err := rep.DirectUserReport(context.Background(), update)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to add report")
		assert.Equal(t, 1, len(mockAPI.RequestCalls()), "should delete /report message")
		assert.Equal(t, 1, len(mockReports.AddCalls()), "should attempt to add report")
	})

	t.Run("empty message text - should use transformed message", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			GetReporterCountSinceFunc: func(ctx context.Context, reporterID int64, since time.Time) (int, error) {
				return 5, nil
			},
			AddFunc: func(ctx context.Context, report storage.Report) error {
				assert.Contains(t, report.MsgText, "caption from image")
				return nil
			},
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				return []storage.Report{}, nil // threshold not reached
			},
		}

		rep := &userReports{
			tbAPI:            mockAPI,
			primChatID:       123,
			adminChatID:      456,
			superUsers:       SuperUsers{},
			reports:          mockReports,
			reportRateLimit:  10,
			reportRatePeriod: 1 * time.Hour,
			reportThreshold:  2,
		}

		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				Text:      "/report",
				From:      &tbapi.User{UserName: "reporter", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID: 999,
					From:      &tbapi.User{ID: 666, UserName: "spammer"},
					Text:      "",
					Caption:   "caption from image",
					Photo:     []tbapi.PhotoSize{{FileID: "photo123"}},
				},
			},
		}

		err := rep.DirectUserReport(context.Background(), update)
		require.NoError(t, err)
		assert.Equal(t, 1, len(mockReports.AddCalls()), "should add report with transformed text")
	})

	t.Run("reported message from channel - should return error", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{}
		mockReports := &mocks.ReportsMock{}

		rep := &userReports{
			tbAPI:       mockAPI,
			primChatID:  123,
			adminChatID: 456,
			superUsers:  SuperUsers{},
			reports:     mockReports,
		}

		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				Text:      "/report",
				From:      &tbapi.User{UserName: "reporter", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID: 999,
					From:      nil, // channel or anonymous admin message
					SenderChat: &tbapi.Chat{
						ID:   -100123456789,
						Type: "channel",
					},
					Text: "channel message",
				},
			},
		}

		err := rep.DirectUserReport(context.Background(), update)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot report messages from channels or anonymous admins")
		assert.Equal(t, 0, len(mockAPI.RequestCalls()), "should not delete message")
		assert.Equal(t, 0, len(mockReports.AddCalls()), "should not add report")
	})

	t.Run("reports storage not initialized - should return error", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
		}

		rep := &userReports{
			tbAPI:            mockAPI,
			primChatID:       123,
			adminChatID:      456,
			superUsers:       SuperUsers{},
			reports:          nil, // not initialized
			reportRateLimit:  0,   // disabled, so rate check passes
			reportRatePeriod: 1 * time.Hour,
		}

		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				Text:      "/report",
				From:      &tbapi.User{UserName: "reporter", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID: 999,
					From:      &tbapi.User{ID: 666, UserName: "spammer"},
					Text:      "spam message",
				},
			},
		}

		err := rep.DirectUserReport(context.Background(), update)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reports storage not initialized")
		assert.Equal(t, 1, len(mockAPI.RequestCalls()), "should still delete /report message")
	})
}

func TestUserReports_CheckReportThreshold(t *testing.T) {
	t.Run("threshold not reached - should return without action", func(t *testing.T) {
		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				return []storage.Report{
					{MsgID: msgID, ChatID: chatID, ReporterUserID: 111},
				}, nil
			},
		}

		rep := &userReports{
			reports:         mockReports,
			reportThreshold: 3, // need 3 reports
		}

		err := rep.checkReportThreshold(context.Background(), 100, 200)
		require.NoError(t, err)
		assert.Equal(t, 1, len(mockReports.GetByMessageCalls()), "should query reports")
	})

	t.Run("threshold reached for new notification - should return without error", func(t *testing.T) {
		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				// return 2 reports, threshold is 2, notification not sent yet
				return []storage.Report{
					{MsgID: msgID, ChatID: chatID, ReporterUserID: 111, NotificationSent: false},
					{MsgID: msgID, ChatID: chatID, ReporterUserID: 222, NotificationSent: false},
				}, nil
			},
		}

		rep := &userReports{
			reports:         mockReports,
			reportThreshold: 2,
		}

		// should call sendReportNotification (stub for now), verify no error
		err := rep.checkReportThreshold(context.Background(), 100, 200)
		require.NoError(t, err)
		assert.Equal(t, 1, len(mockReports.GetByMessageCalls()), "should query reports")
	})

	t.Run("threshold reached for existing notification - should return without error", func(t *testing.T) {
		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				// return 3 reports, threshold is 2, notification already sent
				return []storage.Report{
					{MsgID: msgID, ChatID: chatID, ReporterUserID: 111, NotificationSent: true, AdminMsgID: 999},
					{MsgID: msgID, ChatID: chatID, ReporterUserID: 222, NotificationSent: true, AdminMsgID: 999},
					{MsgID: msgID, ChatID: chatID, ReporterUserID: 333, NotificationSent: true, AdminMsgID: 999},
				}, nil
			},
		}

		rep := &userReports{
			reports:         mockReports,
			reportThreshold: 2,
		}

		// should call updateReportNotification (stub for now), verify no error
		err := rep.checkReportThreshold(context.Background(), 100, 200)
		require.NoError(t, err)
		assert.Equal(t, 1, len(mockReports.GetByMessageCalls()), "should query reports")
	})

	t.Run("exactly at threshold - should trigger notification", func(t *testing.T) {
		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				return []storage.Report{
					{MsgID: msgID, ChatID: chatID, ReporterUserID: 111, NotificationSent: false},
					{MsgID: msgID, ChatID: chatID, ReporterUserID: 222, NotificationSent: false},
					{MsgID: msgID, ChatID: chatID, ReporterUserID: 333, NotificationSent: false},
				}, nil
			},
		}

		rep := &userReports{
			reports:         mockReports,
			reportThreshold: 3, // exactly 3 reports
		}

		err := rep.checkReportThreshold(context.Background(), 100, 200)
		require.NoError(t, err)
	})

	t.Run("reports storage not initialized - should return error", func(t *testing.T) {
		rep := &userReports{
			reports:         nil,
			reportThreshold: 2,
		}

		err := rep.checkReportThreshold(context.Background(), 100, 200)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reports storage not initialized")
	})

	t.Run("GetByMessage error - should return error", func(t *testing.T) {
		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				return nil, fmt.Errorf("database error")
			},
		}

		rep := &userReports{
			reports:         mockReports,
			reportThreshold: 2,
		}

		err := rep.checkReportThreshold(context.Background(), 100, 200)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get reports")
		assert.Contains(t, err.Error(), "database error")
	})
}

func TestUserReports_SendReportNotification(t *testing.T) {
	t.Run("successful notification with single report", func(t *testing.T) {
		var sentMsg tbapi.MessageConfig
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				msg := c.(tbapi.MessageConfig)
				sentMsg = msg
				return tbapi.Message{MessageID: 999}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			UpdateAdminMsgIDFunc: func(ctx context.Context, msgID int, chatID int64, adminMsgID int) error {
				assert.Equal(t, 100, msgID)
				assert.Equal(t, int64(200), chatID)
				assert.Equal(t, 999, adminMsgID)
				return nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
			reports:     mockReports,
		}

		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "reporter1", MsgText: "spam message"},
		}

		err := rep.sendReportNotification(context.Background(), reports)
		require.NoError(t, err)
		assert.Equal(t, 1, len(mockAPI.SendCalls()), "should send message")
		assert.Equal(t, 1, len(mockReports.UpdateAdminMsgIDCalls()), "should update admin msg ID")
		assert.Equal(t, int64(456), sentMsg.ChatID, "should send to admin chat")
		assert.Equal(t, tbapi.ModeMarkdown, sentMsg.ParseMode, "should use markdown")
		assert.Contains(t, sentMsg.Text, "User spam reported (1 reports)", "should contain report count")
		assert.Contains(t, sentMsg.Text, "spammer", "should contain reported user name")
		assert.Contains(t, sentMsg.Text, "spam message", "should contain message text")
		assert.Contains(t, sentMsg.Text, "reporter1", "should contain reporter name")

		// verify inline keyboard
		keyboard, ok := sentMsg.ReplyMarkup.(tbapi.InlineKeyboardMarkup)
		require.True(t, ok, "should have inline keyboard")
		require.Equal(t, 1, len(keyboard.InlineKeyboard), "should have 1 row")
		require.Equal(t, 3, len(keyboard.InlineKeyboard[0]), "should have 3 buttons")
		assert.Equal(t, "✅ Approve Ban", keyboard.InlineKeyboard[0][0].Text)
		assert.Equal(t, "R+666:100", *keyboard.InlineKeyboard[0][0].CallbackData)
		assert.Equal(t, "❌ Reject", keyboard.InlineKeyboard[0][1].Text)
		assert.Equal(t, "R-666:100", *keyboard.InlineKeyboard[0][1].CallbackData)
		assert.Equal(t, "⛔️ Ban Reporter", keyboard.InlineKeyboard[0][2].Text)
		assert.Equal(t, "R?666:100", *keyboard.InlineKeyboard[0][2].CallbackData)
	})

	t.Run("successful notification with multiple reports", func(t *testing.T) {
		var sentMsg tbapi.MessageConfig
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				msg := c.(tbapi.MessageConfig)
				sentMsg = msg
				return tbapi.Message{MessageID: 999}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			UpdateAdminMsgIDFunc: func(ctx context.Context, msgID int, chatID int64, adminMsgID int) error {
				return nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
			reports:     mockReports,
		}

		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "reporter1", MsgText: "spam message"},
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 222, ReporterUserName: "reporter2", MsgText: "spam message"},
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 333, ReporterUserName: "reporter3", MsgText: "spam message"},
		}

		err := rep.sendReportNotification(context.Background(), reports)
		require.NoError(t, err)
		assert.Contains(t, sentMsg.Text, "User spam reported (3 reports)", "should contain report count")
		assert.Contains(t, sentMsg.Text, "reporter1", "should contain first reporter")
		assert.Contains(t, sentMsg.Text, "reporter2", "should contain second reporter")
		assert.Contains(t, sentMsg.Text, "reporter3", "should contain third reporter")
	})

	t.Run("long message text should be truncated", func(t *testing.T) {
		var sentMsg tbapi.MessageConfig
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				msg := c.(tbapi.MessageConfig)
				sentMsg = msg
				return tbapi.Message{MessageID: 999}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			UpdateAdminMsgIDFunc: func(ctx context.Context, msgID int, chatID int64, adminMsgID int) error {
				return nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
			reports:     mockReports,
		}

		longMsg := strings.Repeat("spam ", 100) // 500 chars
		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "reporter1", MsgText: longMsg},
		}

		err := rep.sendReportNotification(context.Background(), reports)
		require.NoError(t, err)
		assert.Contains(t, sentMsg.Text, "...", "should truncate long message")
	})

	t.Run("reporter without username - should use user ID", func(t *testing.T) {
		var sentMsg tbapi.MessageConfig
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				msg := c.(tbapi.MessageConfig)
				sentMsg = msg
				return tbapi.Message{MessageID: 999}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			UpdateAdminMsgIDFunc: func(ctx context.Context, msgID int, chatID int64, adminMsgID int) error {
				return nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
			reports:     mockReports,
		}

		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "", MsgText: "spam"},
		}

		err := rep.sendReportNotification(context.Background(), reports)
		require.NoError(t, err)
		assert.Contains(t, sentMsg.Text, "user111", "should use userID as fallback")
	})

	t.Run("admin chat not configured - should skip notification", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{}
		mockReports := &mocks.ReportsMock{}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 0, // not configured
			reports:     mockReports,
		}

		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "reporter1", MsgText: "spam"},
		}

		err := rep.sendReportNotification(context.Background(), reports)
		require.NoError(t, err)
		assert.Equal(t, 0, len(mockAPI.SendCalls()), "should not send message")
	})

	t.Run("empty reports list - should return error", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{}
		mockReports := &mocks.ReportsMock{}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
			reports:     mockReports,
		}

		err := rep.sendReportNotification(context.Background(), []storage.Report{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no reports provided")
	})

	t.Run("send error - should return error", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{}, fmt.Errorf("network error")
			},
		}

		mockReports := &mocks.ReportsMock{}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
			reports:     mockReports,
		}

		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "reporter1", MsgText: "spam"},
		}

		err := rep.sendReportNotification(context.Background(), reports)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to send notification")
		assert.Contains(t, err.Error(), "network error")
	})

	t.Run("UpdateAdminMsgID error - should not fail", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{MessageID: 999}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			UpdateAdminMsgIDFunc: func(ctx context.Context, msgID int, chatID int64, adminMsgID int) error {
				return fmt.Errorf("database error")
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
			reports:     mockReports,
		}

		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "reporter1", MsgText: "spam"},
		}

		// should not fail even if UpdateAdminMsgID fails
		err := rep.sendReportNotification(context.Background(), reports)
		require.NoError(t, err)
		assert.Equal(t, 1, len(mockAPI.SendCalls()), "should still send message")
		assert.Equal(t, 1, len(mockReports.UpdateAdminMsgIDCalls()), "should attempt to update admin msg ID")
	})

	t.Run("names with markdown special characters should be escaped", func(t *testing.T) {
		var sentMsg tbapi.MessageConfig
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				msg := c.(tbapi.MessageConfig)
				sentMsg = msg
				return tbapi.Message{MessageID: 999}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			UpdateAdminMsgIDFunc: func(ctx context.Context, msgID int, chatID int64, adminMsgID int) error {
				return nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
			reports:     mockReports,
		}

		// names with various markdown special characters
		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spam_user*bot", ReporterUserID: 111, ReporterUserName: "test_reporter", MsgText: "spam"},
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spam_user*bot", ReporterUserID: 222, ReporterUserName: "admin[test]", MsgText: "spam"},
		}

		err := rep.sendReportNotification(context.Background(), reports)
		require.NoError(t, err)

		// verify escaped characters in reported user name
		assert.Contains(t, sentMsg.Text, "spam\\_user\\*bot", "reported user name should be escaped")
		assert.NotContains(t, sentMsg.Text, "spam_user*bot", "reported user name should not contain unescaped characters")

		// verify escaped characters in reporter names
		assert.Contains(t, sentMsg.Text, "test\\_reporter", "first reporter name should be escaped")
		assert.Contains(t, sentMsg.Text, "admin\\[test]", "second reporter name should have escaped bracket")
		assert.NotContains(t, sentMsg.Text, "test_reporter](tg://", "first reporter name should not contain unescaped underscore in link")
		assert.NotContains(t, sentMsg.Text, "admin[test]](tg://", "second reporter name should not contain unescaped opening bracket in link")
	})
}

func TestUserReports_UpdateReportNotification(t *testing.T) {
	t.Run("successful update with multiple reporters", func(t *testing.T) {
		var editedMsg tbapi.EditMessageTextConfig
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				msg := c.(tbapi.EditMessageTextConfig)
				editedMsg = msg
				return tbapi.Message{MessageID: 888}, nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
		}

		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "reporter1", MsgText: "spam message", AdminMsgID: 888},
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 222, ReporterUserName: "reporter2", MsgText: "spam message", AdminMsgID: 888},
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 333, ReporterUserName: "reporter3", MsgText: "spam message", AdminMsgID: 888},
		}

		err := rep.updateReportNotification(context.Background(), reports)
		require.NoError(t, err)
		assert.Equal(t, 1, len(mockAPI.SendCalls()), "should edit message")
		assert.Equal(t, int64(456), editedMsg.ChatID, "should edit in admin chat")
		assert.Equal(t, 888, editedMsg.MessageID, "should edit correct message")
		assert.Equal(t, tbapi.ModeMarkdown, editedMsg.ParseMode, "should use markdown")
		assert.Contains(t, editedMsg.Text, "User spam reported (3 reports)", "should contain updated report count")
		assert.Contains(t, editedMsg.Text, "spammer", "should contain reported user name")
		assert.Contains(t, editedMsg.Text, "spam message", "should contain message text")
		assert.Contains(t, editedMsg.Text, "reporter1", "should contain first reporter")
		assert.Contains(t, editedMsg.Text, "reporter2", "should contain second reporter")
		assert.Contains(t, editedMsg.Text, "reporter3", "should contain third reporter")

		// verify inline keyboard
		require.NotNil(t, editedMsg.ReplyMarkup, "should have inline keyboard")
		require.Equal(t, 1, len(editedMsg.ReplyMarkup.InlineKeyboard), "should have 1 row")
		require.Equal(t, 3, len(editedMsg.ReplyMarkup.InlineKeyboard[0]), "should have 3 buttons")
		assert.Equal(t, "✅ Approve Ban", editedMsg.ReplyMarkup.InlineKeyboard[0][0].Text)
		assert.Equal(t, "R+666:100", *editedMsg.ReplyMarkup.InlineKeyboard[0][0].CallbackData)
		assert.Equal(t, "❌ Reject", editedMsg.ReplyMarkup.InlineKeyboard[0][1].Text)
		assert.Equal(t, "R-666:100", *editedMsg.ReplyMarkup.InlineKeyboard[0][1].CallbackData)
		assert.Equal(t, "⛔️ Ban Reporter", editedMsg.ReplyMarkup.InlineKeyboard[0][2].Text)
		assert.Equal(t, "R?666:100", *editedMsg.ReplyMarkup.InlineKeyboard[0][2].CallbackData)
	})

	t.Run("successful update adding new reporter to existing notification", func(t *testing.T) {
		var editedMsg tbapi.EditMessageTextConfig
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				msg := c.(tbapi.EditMessageTextConfig)
				editedMsg = msg
				return tbapi.Message{MessageID: 888}, nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
		}

		// simulating adding a second reporter to an existing notification
		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "reporter1", MsgText: "spam message", AdminMsgID: 888},
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 222, ReporterUserName: "reporter2", MsgText: "spam message", AdminMsgID: 888},
		}

		err := rep.updateReportNotification(context.Background(), reports)
		require.NoError(t, err)
		assert.Contains(t, editedMsg.Text, "User spam reported (2 reports)", "should update to 2 reports")
		assert.Contains(t, editedMsg.Text, "reporter1", "should still contain first reporter")
		assert.Contains(t, editedMsg.Text, "reporter2", "should add second reporter")
	})

	t.Run("long message text should be truncated", func(t *testing.T) {
		var editedMsg tbapi.EditMessageTextConfig
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				msg := c.(tbapi.EditMessageTextConfig)
				editedMsg = msg
				return tbapi.Message{MessageID: 888}, nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
		}

		longMsg := strings.Repeat("spam ", 100)
		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "reporter1", MsgText: longMsg, AdminMsgID: 888},
		}

		err := rep.updateReportNotification(context.Background(), reports)
		require.NoError(t, err)
		assert.True(t, strings.Contains(editedMsg.Text, "..."), "should truncate long message")
		msgStart := strings.Index(editedMsg.Text, "spam")
		msgEnd := strings.Index(editedMsg.Text[msgStart:], "**Reporters:**")
		msgTextInNotif := editedMsg.Text[msgStart : msgStart+msgEnd]
		assert.True(t, len(msgTextInNotif) < len(longMsg), "message should be shorter than original")
	})

	t.Run("reporter without username should use fallback", func(t *testing.T) {
		var editedMsg tbapi.EditMessageTextConfig
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				msg := c.(tbapi.EditMessageTextConfig)
				editedMsg = msg
				return tbapi.Message{MessageID: 888}, nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
		}

		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "", MsgText: "spam", AdminMsgID: 888},
		}

		err := rep.updateReportNotification(context.Background(), reports)
		require.NoError(t, err)
		assert.Contains(t, editedMsg.Text, "user111", "should use fallback username")
	})

	t.Run("admin chat not configured should skip gracefully", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				t.Fatal("should not send message")
				return tbapi.Message{}, nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 0, // not configured
		}

		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "reporter1", MsgText: "spam", AdminMsgID: 888},
		}

		err := rep.updateReportNotification(context.Background(), reports)
		require.NoError(t, err)
		assert.Equal(t, 0, len(mockAPI.SendCalls()), "should not send message")
	})

	t.Run("empty reports list should return error", func(t *testing.T) {
		rep := &userReports{
			adminChatID: 456,
		}

		err := rep.updateReportNotification(context.Background(), []storage.Report{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reports list is empty")
	})

	t.Run("send error should return error", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{}, fmt.Errorf("telegram api error")
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
		}

		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", ReporterUserID: 111, ReporterUserName: "reporter1", MsgText: "spam", AdminMsgID: 888},
		}

		err := rep.updateReportNotification(context.Background(), reports)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to edit admin notification")
	})

	t.Run("names with markdown special characters should be escaped", func(t *testing.T) {
		var editedMsg tbapi.EditMessageTextConfig
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				msg := c.(tbapi.EditMessageTextConfig)
				editedMsg = msg
				return tbapi.Message{MessageID: 888}, nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
		}

		// names with various markdown special characters
		reports := []storage.Report{
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spam_user*bot", ReporterUserID: 111, ReporterUserName: "test_reporter", MsgText: "spam", AdminMsgID: 888},
			{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spam_user*bot", ReporterUserID: 222, ReporterUserName: "admin[test]", MsgText: "spam", AdminMsgID: 888},
		}

		err := rep.updateReportNotification(context.Background(), reports)
		require.NoError(t, err)

		// verify escaped characters in reported user name
		assert.Contains(t, editedMsg.Text, "spam\\_user\\*bot", "reported user name should be escaped")
		assert.NotContains(t, editedMsg.Text, "spam_user*bot", "reported user name should not contain unescaped characters")

		// verify escaped characters in reporter names
		assert.Contains(t, editedMsg.Text, "test\\_reporter", "first reporter name should be escaped")
		assert.Contains(t, editedMsg.Text, "admin\\[test]", "second reporter name should have escaped bracket")
		assert.NotContains(t, editedMsg.Text, "test_reporter](tg://", "first reporter name should not contain unescaped underscore in link")
		assert.NotContains(t, editedMsg.Text, "admin[test]](tg://", "second reporter name should not contain unescaped opening bracket in link")
	})
}

func TestUserReports_CallbackReportBan(t *testing.T) {
	t.Run("successful ban approval", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{}, nil
			},
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				return []storage.Report{
					{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer", MsgText: "spam msg"},
				}, nil
			},
			DeleteByMessageFunc: func(ctx context.Context, msgID int, chatID int64) error {
				return nil
			},
		}

		mockBot := &mocks.BotMock{
			RemoveApprovedUserFunc: func(userID int64) error { return nil },
			UpdateSpamFunc:         func(msg string) error { return nil },
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
			primChatID:  200,
			reports:     mockReports,
			bot:         mockBot,
		}

		query := &tbapi.CallbackQuery{
			Data: "R+666:100",
			From: &tbapi.User{UserName: "admin"},
			Message: &tbapi.Message{
				Chat:      tbapi.Chat{ID: 456},
				MessageID: 999,
				Text:      "**User spam reported (1 reports)**",
				Date:      int(time.Now().Unix()),
			},
		}

		err := rep.callbackReportBan(context.Background(), query)
		require.NoError(t, err)
		assert.Equal(t, 1, len(mockReports.GetByMessageCalls()))
		assert.Equal(t, 1, len(mockReports.DeleteByMessageCalls()))
		assert.Equal(t, 1, len(mockBot.UpdateSpamCalls()))
	})

	t.Run("no reports found", func(t *testing.T) {
		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				return []storage.Report{}, nil
			},
		}

		rep := &userReports{
			adminChatID: 456,
			primChatID:  200,
			reports:     mockReports,
		}

		query := &tbapi.CallbackQuery{
			Data: "R+666:100",
			Message: &tbapi.Message{
				Chat: tbapi.Chat{ID: 456},
			},
		}

		err := rep.callbackReportBan(context.Background(), query)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no reports found")
	})
}

func TestUserReports_CallbackReportReject(t *testing.T) {
	t.Run("successful reject", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				return []storage.Report{
					{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer"},
				}, nil
			},
			DeleteByMessageFunc: func(ctx context.Context, msgID int, chatID int64) error {
				return nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
			primChatID:  200,
			reports:     mockReports,
		}

		query := &tbapi.CallbackQuery{
			Data: "R-666:100",
			From: &tbapi.User{UserName: "admin"},
			Message: &tbapi.Message{
				Chat:      tbapi.Chat{ID: 456},
				MessageID: 999,
				Text:      "**User spam reported (1 reports)**",
				Date:      int(time.Now().Unix()),
			},
		}

		err := rep.callbackReportReject(context.Background(), query)
		require.NoError(t, err)
		assert.Equal(t, 1, len(mockReports.GetByMessageCalls()))
		assert.Equal(t, 1, len(mockReports.DeleteByMessageCalls()))
	})

	t.Run("no reports found", func(t *testing.T) {
		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				return []storage.Report{}, nil
			},
		}

		rep := &userReports{
			adminChatID: 456,
			primChatID:  200,
			reports:     mockReports,
		}

		query := &tbapi.CallbackQuery{
			Data: "R-666:100",
			Message: &tbapi.Message{
				Chat: tbapi.Chat{ID: 456},
			},
		}

		err := rep.callbackReportReject(context.Background(), query)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no reports found")
	})
}

func TestUserReports_CallbackReportBanReporterAsk(t *testing.T) {
	t.Run("show confirmation with multiple reporters", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				if editMarkup, ok := c.(tbapi.EditMessageReplyMarkupConfig); ok {
					assert.Equal(t, int64(456), editMarkup.ChatID)
					assert.Equal(t, 999, editMarkup.MessageID)
					require.Len(t, editMarkup.ReplyMarkup.InlineKeyboard, 3)
					assert.Equal(t, "Ban reporter1", editMarkup.ReplyMarkup.InlineKeyboard[0][0].Text)
					assert.Equal(t, "R!111:100", *editMarkup.ReplyMarkup.InlineKeyboard[0][0].CallbackData)
					assert.Equal(t, "Ban reporter2", editMarkup.ReplyMarkup.InlineKeyboard[1][0].Text)
					assert.Equal(t, "R!222:100", *editMarkup.ReplyMarkup.InlineKeyboard[1][0].CallbackData)
					assert.Equal(t, "Cancel", editMarkup.ReplyMarkup.InlineKeyboard[2][0].Text)
					assert.Equal(t, "RX666:100", *editMarkup.ReplyMarkup.InlineKeyboard[2][0].CallbackData)
				}
				return tbapi.Message{}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				return []storage.Report{
					{MsgID: 100, ChatID: 200, ReporterUserID: 111, ReporterUserName: "reporter1"},
					{MsgID: 100, ChatID: 200, ReporterUserID: 222, ReporterUserName: "reporter2"},
				}, nil
			},
		}

		rep := &userReports{
			tbAPI:      mockAPI,
			primChatID: 200,
			reports:    mockReports,
		}

		query := &tbapi.CallbackQuery{
			Data:    "R?666:100",
			Message: &tbapi.Message{Chat: tbapi.Chat{ID: 456}, MessageID: 999},
		}

		err := rep.callbackReportBanReporterAsk(context.Background(), query)
		require.NoError(t, err)
		assert.Equal(t, 1, len(mockReports.GetByMessageCalls()))
		assert.Equal(t, 1, len(mockAPI.SendCalls()))
	})

	t.Run("no reports found", func(t *testing.T) {
		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				return []storage.Report{}, nil
			},
		}

		rep := &userReports{primChatID: 200, reports: mockReports}
		query := &tbapi.CallbackQuery{Data: "R?666:100", Message: &tbapi.Message{Chat: tbapi.Chat{ID: 456}}}

		err := rep.callbackReportBanReporterAsk(context.Background(), query)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no reports found")
	})
}

func TestUserReports_CallbackReportBanReporterConfirm(t *testing.T) {
	t.Run("ban reporter with remaining reporters", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{}, nil
			},
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
		}

		var callCount int
		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				callCount++
				if callCount == 1 {
					return []storage.Report{
						{MsgID: 100, ChatID: 200, ReporterUserID: 111, ReporterUserName: "reporter1", ReportedUserID: 666, ReportedUserName: "spammer"},
						{MsgID: 100, ChatID: 200, ReporterUserID: 222, ReporterUserName: "reporter2", ReportedUserID: 666, ReportedUserName: "spammer"},
					}, nil
				}
				return []storage.Report{
					{MsgID: 100, ChatID: 200, ReporterUserID: 222, ReporterUserName: "reporter2", ReportedUserID: 666, ReportedUserName: "spammer"},
				}, nil
			},
			DeleteReporterFunc: func(ctx context.Context, reporterID int64, msgID int, chatID int64) error {
				return nil
			},
		}

		rep := &userReports{
			tbAPI:      mockAPI,
			primChatID: 200,
			reports:    mockReports,
		}

		query := &tbapi.CallbackQuery{
			Data:    "R!111:100",
			From:    &tbapi.User{UserName: "admin"},
			Message: &tbapi.Message{Chat: tbapi.Chat{ID: 456}, MessageID: 999, Text: "Test", Date: int(time.Now().Unix())},
		}

		err := rep.callbackReportBanReporterConfirm(context.Background(), query)
		require.NoError(t, err)
		assert.Equal(t, 2, len(mockReports.GetByMessageCalls()))
		assert.Equal(t, 1, len(mockReports.DeleteReporterCalls()))
	})

	t.Run("ban last reporter", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{}, nil
			},
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
		}

		var callCount int
		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				callCount++
				if callCount == 1 {
					return []storage.Report{
						{MsgID: 100, ChatID: 200, ReporterUserID: 111, ReporterUserName: "reporter1", ReportedUserID: 666, ReportedUserName: "spammer"},
					}, nil
				}
				return []storage.Report{}, nil
			},
			DeleteReporterFunc: func(ctx context.Context, reporterID int64, msgID int, chatID int64) error {
				return nil
			},
			DeleteByMessageFunc: func(ctx context.Context, msgID int, chatID int64) error {
				return nil
			},
		}

		rep := &userReports{
			tbAPI:      mockAPI,
			primChatID: 200,
			reports:    mockReports,
		}

		query := &tbapi.CallbackQuery{
			Data:    "R!111:100",
			From:    &tbapi.User{UserName: "admin"},
			Message: &tbapi.Message{Chat: tbapi.Chat{ID: 456}, MessageID: 999, Text: "Test", Date: int(time.Now().Unix())},
		}

		err := rep.callbackReportBanReporterConfirm(context.Background(), query)
		require.NoError(t, err)
		assert.Equal(t, 2, len(mockReports.GetByMessageCalls()))
		assert.Equal(t, 1, len(mockReports.DeleteReporterCalls()))
		assert.Equal(t, 1, len(mockReports.DeleteByMessageCalls()))
	})
}

func TestUserReports_CallbackReportCancel(t *testing.T) {
	t.Run("restore original buttons", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				if editMarkup, ok := c.(tbapi.EditMessageReplyMarkupConfig); ok {
					assert.Equal(t, int64(456), editMarkup.ChatID)
					assert.Equal(t, 999, editMarkup.MessageID)
					require.Len(t, editMarkup.ReplyMarkup.InlineKeyboard, 2)
					assert.Equal(t, "✅ Approve Ban", editMarkup.ReplyMarkup.InlineKeyboard[0][0].Text)
					assert.Equal(t, "R+666:100", *editMarkup.ReplyMarkup.InlineKeyboard[0][0].CallbackData)
					assert.Equal(t, "❌ Reject", editMarkup.ReplyMarkup.InlineKeyboard[0][1].Text)
					assert.Equal(t, "R-666:100", *editMarkup.ReplyMarkup.InlineKeyboard[0][1].CallbackData)
					assert.Equal(t, "⛔️ Ban Reporter", editMarkup.ReplyMarkup.InlineKeyboard[1][0].Text)
					assert.Equal(t, "R?666:100", *editMarkup.ReplyMarkup.InlineKeyboard[1][0].CallbackData)
				}
				return tbapi.Message{}, nil
			},
		}

		rep := &userReports{tbAPI: mockAPI}

		query := &tbapi.CallbackQuery{
			Data:    "RX666:100",
			From:    &tbapi.User{UserName: "admin"},
			Message: &tbapi.Message{Chat: tbapi.Chat{ID: 456}, MessageID: 999},
		}

		err := rep.callbackReportCancel(context.Background(), query)
		require.NoError(t, err)
		assert.Equal(t, 1, len(mockAPI.SendCalls()))
	})
}

func TestUserReports_HandleReportCallback_SecurityValidation(t *testing.T) {
	t.Run("callback from admin chat should be processed", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				return []storage.Report{
					{MsgID: 100, ChatID: 200, ReportedUserID: 666, ReportedUserName: "spammer"},
				}, nil
			},
			DeleteByMessageFunc: func(ctx context.Context, msgID int, chatID int64) error {
				return nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456, // admin chat ID
			primChatID:  200,
			reports:     mockReports,
		}

		query := &tbapi.CallbackQuery{
			Data: "R-666:100",
			From: &tbapi.User{UserName: "admin"},
			Message: &tbapi.Message{
				Chat:      tbapi.Chat{ID: 456}, // callback from admin chat
				MessageID: 999,
				Text:      "User spam reported",
				Date:      int(time.Now().Unix()),
			},
		}

		err := rep.HandleReportCallback(context.Background(), query)
		require.NoError(t, err)
		// verify callback was processed (reports deleted)
		assert.Equal(t, 1, len(mockReports.GetByMessageCalls()))
		assert.Equal(t, 1, len(mockReports.DeleteByMessageCalls()))
	})

	t.Run("callback from non-admin chat should be rejected", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				t.Fatal("should not call Send for unauthorized callback")
				return tbapi.Message{}, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				t.Fatal("should not call GetByMessage for unauthorized callback")
				return nil, nil
			},
			DeleteByMessageFunc: func(ctx context.Context, msgID int, chatID int64) error {
				t.Fatal("should not call DeleteByMessage for unauthorized callback")
				return nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456, // admin chat ID
			primChatID:  200,
			reports:     mockReports,
		}

		query := &tbapi.CallbackQuery{
			Data: "R-666:100",
			From: &tbapi.User{UserName: "attacker"},
			Message: &tbapi.Message{
				Chat:      tbapi.Chat{ID: 789}, // callback from different chat (not admin)
				MessageID: 999,
				Text:      "User spam reported",
				Date:      int(time.Now().Unix()),
			},
		}

		err := rep.HandleReportCallback(context.Background(), query)
		require.NoError(t, err) // returns nil (silently ignores)
		// verify no reports functions were called
		assert.Equal(t, 0, len(mockReports.GetByMessageCalls()))
		assert.Equal(t, 0, len(mockReports.DeleteByMessageCalls()))
		assert.Equal(t, 0, len(mockAPI.SendCalls()))
	})

	t.Run("R+ callback from non-admin chat should be rejected", func(t *testing.T) {
		mockBot := &mocks.BotMock{
			RemoveApprovedUserFunc: func(userID int64) error {
				t.Fatal("should not call RemoveApprovedUser for unauthorized callback")
				return nil
			},
			UpdateSpamFunc: func(msg string) error {
				t.Fatal("should not call UpdateSpam for unauthorized callback")
				return nil
			},
		}

		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				t.Fatal("should not call GetByMessage for unauthorized callback")
				return nil, nil
			},
		}

		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				t.Fatal("should not call Request (ban) for unauthorized callback")
				return nil, nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			bot:         mockBot,
			adminChatID: 456,
			primChatID:  200,
			reports:     mockReports,
		}

		query := &tbapi.CallbackQuery{
			Data: "R+666:100", // attempt to ban user
			From: &tbapi.User{UserName: "attacker"},
			Message: &tbapi.Message{
				Chat:      tbapi.Chat{ID: 123}, // not admin chat
				MessageID: 999,
				Text:      "fake report",
				Date:      int(time.Now().Unix()),
			},
		}

		err := rep.HandleReportCallback(context.Background(), query)
		require.NoError(t, err) // returns nil (silently ignores)
		// verify no ban or spam update was performed
		assert.Equal(t, 0, len(mockBot.RemoveApprovedUserCalls()))
		assert.Equal(t, 0, len(mockBot.UpdateSpamCalls()))
		assert.Equal(t, 0, len(mockReports.GetByMessageCalls()))
		assert.Equal(t, 0, len(mockAPI.RequestCalls()))
	})

	t.Run("R! callback from non-admin chat should be rejected", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				t.Fatal("should not ban reporter for unauthorized callback")
				return nil, nil
			},
		}

		mockReports := &mocks.ReportsMock{
			GetByMessageFunc: func(ctx context.Context, msgID int, chatID int64) ([]storage.Report, error) {
				t.Fatal("should not call GetByMessage for unauthorized callback")
				return nil, nil
			},
			DeleteReporterFunc: func(ctx context.Context, reporterID int64, msgID int, chatID int64) error {
				t.Fatal("should not call DeleteReporter for unauthorized callback")
				return nil
			},
		}

		rep := &userReports{
			tbAPI:       mockAPI,
			adminChatID: 456,
			primChatID:  200,
			reports:     mockReports,
		}

		query := &tbapi.CallbackQuery{
			Data: "R!111:100", // attempt to ban reporter
			From: &tbapi.User{UserName: "attacker"},
			Message: &tbapi.Message{
				Chat:      tbapi.Chat{ID: 999}, // not admin chat
				MessageID: 888,
				Text:      "fake report",
				Date:      int(time.Now().Unix()),
			},
		}

		err := rep.HandleReportCallback(context.Background(), query)
		require.NoError(t, err) // returns nil (silently ignores)
		// verify no reporter ban was performed
		assert.Equal(t, 0, len(mockReports.GetByMessageCalls()))
		assert.Equal(t, 0, len(mockReports.DeleteReporterCalls()))
		assert.Equal(t, 0, len(mockAPI.RequestCalls()))
	})

	t.Run("callback with invalid data format should return error", func(t *testing.T) {
		rep := &userReports{
			adminChatID: 456,
			primChatID:  200,
		}

		query := &tbapi.CallbackQuery{
			Data: "R+", // invalid: too short
			From: &tbapi.User{UserName: "admin"},
			Message: &tbapi.Message{
				Chat:      tbapi.Chat{ID: 456}, // admin chat
				MessageID: 999,
				Date:      int(time.Now().Unix()),
			},
		}

		err := rep.HandleReportCallback(context.Background(), query)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid callback data")
	})

	t.Run("callback with unknown prefix should return error", func(t *testing.T) {
		rep := &userReports{
			adminChatID: 456,
			primChatID:  200,
		}

		query := &tbapi.CallbackQuery{
			Data: "RZ666:100", // unknown prefix RZ
			From: &tbapi.User{UserName: "admin"},
			Message: &tbapi.Message{
				Chat:      tbapi.Chat{ID: 456}, // admin chat
				MessageID: 999,
				Date:      int(time.Now().Unix()),
			},
		}

		err := rep.HandleReportCallback(context.Background(), query)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown report callback")
	})
}
