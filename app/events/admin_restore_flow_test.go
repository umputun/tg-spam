package events

import (
	"context"
	"strings"
	"testing"

	tbapi "github.com/OvyFlash/telegram-bot-api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/events/mocks"
	"github.com/umputun/tg-spam/app/storage"
)

// covers the restore flow: the bot remembers the id of its own spam reply, carries it
// through the admin report callback data, and on unban deletes that reply from the
// primary chat and reposts the original message.

func TestAdmin_reportBanEmbedsSpamReplyID(t *testing.T) {
	mockAPI := &mocks.TbAPIMock{
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) { return tbapi.Message{}, nil },
	}
	adm := &admin{tbAPI: mockAPI, adminChatID: 456}

	msg := &bot.Message{ID: 999, From: bot.User{ID: 777, Username: "spammer"}, Text: "spam text"}
	adm.ReportBan("spammer", msg, 555)

	require.Len(t, mockAPI.SendCalls(), 1)
	mc, ok := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig)
	require.True(t, ok, "admin report must be a MessageConfig")
	markup, ok := mc.ReplyMarkup.(tbapi.InlineKeyboardMarkup)
	require.True(t, ok, "admin report must carry inline keyboard")
	require.NotEmpty(t, markup.InlineKeyboard)
	require.NotEmpty(t, markup.InlineKeyboard[0])
	for _, btn := range markup.InlineKeyboard[0] {
		require.NotNil(t, btn.CallbackData)
		assert.True(t, strings.HasSuffix(*btn.CallbackData, ":555"),
			"callback data %q must end with the spam reply id", *btn.CallbackData)
	}
}

func TestAdmin_callbackUnbanConfirmedRestoreFlow(t *testing.T) {
	setup := func(restoreMsg string) (*mocks.TbAPIMock, *admin) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc:    func(c tbapi.Chattable) (tbapi.Message, error) { return tbapi.Message{}, nil },
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) { return &tbapi.APIResponse{Ok: true}, nil },
		}
		botMock := &mocks.BotMock{
			UpdateHamFunc:       func(msg string) error { return nil },
			AddApprovedUserFunc: func(id int64, name string) error { return nil },
		}
		locatorMock := &mocks.LocatorMock{
			SpamFunc: func(ctx context.Context, userID int64) (storage.SpamData, bool) { return storage.SpamData{}, false },
		}
		adm := &admin{
			tbAPI:       mockAPI,
			bot:         botMock,
			locator:     locatorMock,
			primChatID:  123,
			adminChatID: 456,
			restoreMsg:  restoreMsg,
		}
		return mockAPI, adm
	}

	mkQuery := func(data string) *tbapi.CallbackQuery {
		return &tbapi.CallbackQuery{
			ID:   "test-callback-id",
			Data: data,
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 456}, // admin chat
				Text:      "**permanently banned [spammer](tg://user?id=777)**\n\nSpam message text",
				From:      &tbapi.User{UserName: "bot"},
			},
			From: &tbapi.User{UserName: "admin", ID: 111},
		}
	}

	deleteCalls := func(mockAPI *mocks.TbAPIMock) (res []tbapi.DeleteMessageConfig) {
		for _, call := range mockAPI.RequestCalls() {
			if del, ok := call.C.(tbapi.DeleteMessageConfig); ok {
				res = append(res, del)
			}
		}
		return res
	}

	primChatSends := func(mockAPI *mocks.TbAPIMock) (res []tbapi.MessageConfig) {
		for _, call := range mockAPI.SendCalls() {
			if mc, ok := call.C.(tbapi.MessageConfig); ok && mc.ChatID == 123 {
				res = append(res, mc)
			}
		}
		return res
	}

	t.Run("deletes spam reply and restores original message", func(t *testing.T) {
		mockAPI, adm := setup("прошу прощения, принял вас за спамера. Вот ваше сообщение")

		err := adm.callbackUnbanConfirmed(mkQuery("777:999:555"))
		require.NoError(t, err)

		dels := deleteCalls(mockAPI)
		require.Len(t, dels, 1, "bot's spam reply must be deleted from the primary chat")
		assert.Equal(t, 555, dels[0].MessageID)
		assert.Equal(t, int64(123), dels[0].ChatID)

		sends := primChatSends(mockAPI)
		require.Len(t, sends, 1, "original message must be reposted to the primary chat")
		assert.Contains(t, sends[0].Text, "прошу прощения, принял вас за спамера")
		assert.Contains(t, sends[0].Text, "Spam message text")
		assert.Equal(t, tbapi.ModeMarkdown, sends[0].ParseMode)
		assert.True(t, sends[0].LinkPreviewOptions.IsDisabled)
	})

	t.Run("legacy callback data without spam reply id still restores", func(t *testing.T) {
		mockAPI, adm := setup("прошу прощения")

		err := adm.callbackUnbanConfirmed(mkQuery("777:999"))
		require.NoError(t, err)

		assert.Empty(t, deleteCalls(mockAPI), "nothing to delete when spam reply id is unknown")
		assert.Len(t, primChatSends(mockAPI), 1, "restore must not depend on the spam reply id")
	})

	t.Run("empty restore message disables reposting", func(t *testing.T) {
		mockAPI, adm := setup("")

		err := adm.callbackUnbanConfirmed(mkQuery("777:999:555"))
		require.NoError(t, err)

		require.Len(t, deleteCalls(mockAPI), 1, "spam reply cleanup must not depend on restore message")
		assert.Empty(t, primChatSends(mockAPI), "no restore message configured - nothing goes to the chat")
	})
}
