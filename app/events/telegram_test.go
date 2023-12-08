package events

import (
	"context"
	"testing"
	"time"

	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/events/mocks"
)

func TestTelegramListener_Do(t *testing.T) {
	mockLogger := &mocks.SpamLoggerMock{SaveFunc: func(msg *bot.Message, response *bot.Response) {}}
	mockAPI := &mocks.TbAPIMock{
		GetChatFunc: func(config tbapi.ChatInfoConfig) (tbapi.Chat, error) {
			return tbapi.Chat{ID: 123}, nil
		},
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			return tbapi.Message{Text: c.(tbapi.MessageConfig).Text, From: &tbapi.User{UserName: "user"}}, nil
		},
	}
	b := &mocks.BotMock{OnMessageFunc: func(msg bot.Message) bot.Response {
		t.Logf("on-message: %+v", msg)
		if msg.Text == "text 123" && msg.From.Username == "user" {
			return bot.Response{Send: true, Text: "bot's answer"}
		}
		return bot.Response{}
	}}

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		Group:      "gr",
		AdminGroup: "987654321",
		StartupMsg: "startup",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		Message: &tbapi.Message{
			Chat: &tbapi.Chat{ID: 123},
			Text: "text 123",
			From: &tbapi.User{UserName: "user"},
			Date: int(time.Date(2020, 2, 11, 19, 35, 55, 9, time.UTC).Unix()),
		},
	}

	updChan := make(chan tbapi.Update, 1)
	updChan <- updMsg
	close(updChan)
	mockAPI.GetUpdatesChanFunc = func(config tbapi.UpdateConfig) tbapi.UpdatesChannel { return updChan }

	err := l.Do(ctx)
	assert.EqualError(t, err, "telegram update chan closed")
	assert.Equal(t, 0, len(mockLogger.SaveCalls()))
	require.Equal(t, 2, len(mockAPI.SendCalls()))
	assert.Equal(t, "startup", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
	assert.Equal(t, "bot's answer", mockAPI.SendCalls()[1].C.(tbapi.MessageConfig).Text)
}

func TestTelegramListener_DoWithBotBan(t *testing.T) {
	mockLogger := &mocks.SpamLoggerMock{SaveFunc: func(msg *bot.Message, response *bot.Response) {}}
	mockAPI := &mocks.TbAPIMock{
		GetChatFunc: func(config tbapi.ChatInfoConfig) (tbapi.Chat, error) {
			return tbapi.Chat{ID: 123}, nil
		},
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			return tbapi.Message{Text: c.(tbapi.MessageConfig).Text, From: &tbapi.User{UserName: "user"}}, nil
		},
		RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
			return &tbapi.APIResponse{}, nil
		},
	}
	b := &mocks.BotMock{OnMessageFunc: func(msg bot.Message) bot.Response {
		t.Logf("on-message: %+v", msg)
		if msg.Text == "text 123" && msg.From.Username == "user" {
			return bot.Response{Send: true, Text: "bot's answer", BanInterval: 2 * time.Minute, User: bot.User{Username: "user", ID: 1}}
		}
		if msg.From.Username == "ChannelBot" {
			return bot.Response{Send: true, Text: "bot's answer for channel", BanInterval: 2 * time.Minute, User: bot.User{Username: "user", ID: 1}, ChannelID: msg.SenderChat.ID}
		}
		if msg.From.Username == "admin" {
			return bot.Response{Send: true, Text: "bot's answer for admin", BanInterval: 2 * time.Minute, User: bot.User{Username: "user", ID: 1}, ChannelID: msg.ReplyTo.SenderChat.ID}
		}
		return bot.Response{}
	}}

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		SuperUsers: SuperUser{"admin"},
		Group:      "gr",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	t.Run("test ban of the user", func(t *testing.T) {
		mockLogger.ResetCalls()
		updMsg := tbapi.Update{
			Message: &tbapi.Message{
				Chat: &tbapi.Chat{ID: 123},
				Text: "text 123",
				From: &tbapi.User{UserName: "user", ID: 123},
				Date: int(time.Date(2020, 2, 11, 19, 35, 55, 9, time.UTC).Unix()),
			},
		}

		updChan := make(chan tbapi.Update, 1)
		updChan <- updMsg
		close(updChan)
		mockAPI.GetUpdatesChanFunc = func(config tbapi.UpdateConfig) tbapi.UpdatesChannel { return updChan }

		err := l.Do(ctx)
		assert.EqualError(t, err, "telegram update chan closed")
		assert.Equal(t, 1, len(mockLogger.SaveCalls()))
		assert.Equal(t, "text 123", mockLogger.SaveCalls()[0].Msg.Text)
		assert.Equal(t, "user", mockLogger.SaveCalls()[0].Msg.From.Username)
		assert.Equal(t, 1, len(mockAPI.SendCalls()))
		assert.Equal(t, "bot's answer", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
		assert.Equal(t, 1, len(mockAPI.RequestCalls()))
		assert.Equal(t, int64(123), mockAPI.RequestCalls()[0].C.(tbapi.RestrictChatMemberConfig).ChatID)
	})

	t.Run("test ban of the channel", func(t *testing.T) {
		mockLogger.ResetCalls()
		mockAPI.ResetCalls()
		updMsg := tbapi.Update{
			Message: &tbapi.Message{
				Chat: &tbapi.Chat{ID: 123},
				Text: "text 321",
				From: &tbapi.User{UserName: "ChannelBot", ID: 136817688},
				SenderChat: &tbapi.Chat{
					ID:       12345,
					UserName: "test_bot",
				},
				Date: int(time.Date(2020, 2, 11, 19, 35, 55, 9, time.UTC).Unix()),
			},
		}

		updChan := make(chan tbapi.Update, 1)
		updChan <- updMsg
		close(updChan)
		mockAPI.GetUpdatesChanFunc = func(config tbapi.UpdateConfig) tbapi.UpdatesChannel { return updChan }

		err := l.Do(ctx)
		assert.EqualError(t, err, "telegram update chan closed")
		assert.Equal(t, 1, len(mockLogger.SaveCalls()))
		assert.Equal(t, "text 321", mockLogger.SaveCalls()[0].Msg.Text)
		assert.Equal(t, "ChannelBot", mockLogger.SaveCalls()[0].Msg.From.Username)
		assert.Equal(t, "user", mockLogger.SaveCalls()[0].Response.User.Username)
		assert.Equal(t, "bot's answer for channel", mockLogger.SaveCalls()[0].Response.Text)
		assert.Equal(t, 1, len(mockAPI.SendCalls()))
		assert.Equal(t, "bot's answer for channel", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
		assert.Equal(t, 1, len(mockAPI.RequestCalls()))
		assert.Equal(t, int64(123), mockAPI.RequestCalls()[0].C.(tbapi.BanChatSenderChatConfig).ChatID)
		assert.Equal(t, int64(12345), mockAPI.RequestCalls()[0].C.(tbapi.BanChatSenderChatConfig).SenderChatID)
	})

	//nolint
	t.Run("test ban of the channel on behalf of the superuser", func(t *testing.T) {
		mockLogger.ResetCalls()
		mockAPI.ResetCalls()
		updMsg := tbapi.Update{
			Message: &tbapi.Message{
				ReplyToMessage: &tbapi.Message{
					SenderChat: &tbapi.Chat{
						ID:       54321,
						UserName: "original_bot",
					},
				},
				Chat: &tbapi.Chat{ID: 123},
				Text: "text 543",
				From: &tbapi.User{UserName: "admin", ID: 555},
				Date: int(time.Date(2020, 2, 11, 19, 37, 55, 9, time.UTC).Unix()),
			},
		}

		updChan := make(chan tbapi.Update, 1)
		updChan <- updMsg
		close(updChan)
		mockAPI.GetUpdatesChanFunc = func(config tbapi.UpdateConfig) tbapi.UpdatesChannel { return updChan }

		err := l.Do(ctx)
		assert.EqualError(t, err, "telegram update chan closed")
		require.Equal(t, 1, len(mockLogger.SaveCalls()))
		assert.Equal(t, "text 543", mockLogger.SaveCalls()[0].Msg.Text)
		assert.Equal(t, "admin", mockLogger.SaveCalls()[0].Msg.From.Username)
		assert.Equal(t, "bot's answer for admin", mockLogger.SaveCalls()[0].Response.Text)
		assert.Equal(t, 1, len(mockAPI.SendCalls()))
		assert.Equal(t, "bot's answer for admin", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
		require.Equal(t, 0, len(mockAPI.RequestCalls()))
	})
}

func TestTelegramListener_DoDeleteMessages(t *testing.T) {
	mockLogger := &mocks.SpamLoggerMock{SaveFunc: func(msg *bot.Message, response *bot.Response) {}}
	mockAPI := &mocks.TbAPIMock{
		GetChatFunc: func(config tbapi.ChatInfoConfig) (tbapi.Chat, error) {
			return tbapi.Chat{ID: 123}, nil
		},
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			return tbapi.Message{Text: c.(tbapi.MessageConfig).Text, From: &tbapi.User{UserName: "ChannelBot"}}, nil
		},
		RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
			return &tbapi.APIResponse{Ok: true}, nil
		},
	}
	b := &mocks.BotMock{OnMessageFunc: func(msg bot.Message) bot.Response {
		t.Logf("on-message: %+v", msg)
		if msg.Text == "text 123" && msg.From.Username == "user" {
			return bot.Response{DeleteReplyTo: true, ReplyTo: msg.ID, ChannelID: msg.ChatID, BanInterval: time.Hour,
				Send: true, Text: "bot's answer", User: bot.User{Username: "user", ID: 1, DisplayName: "First Last"}}
		}
		return bot.Response{}
	}}

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		Group:      "gr",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		Message: &tbapi.Message{
			MessageID: 321,
			Chat:      &tbapi.Chat{ID: 123},
			Text:      "text 123",
			From:      &tbapi.User{UserName: "user"},
			Date:      int(time.Date(2020, 2, 11, 19, 35, 55, 9, time.UTC).Unix()),
			ReplyToMessage: &tbapi.Message{
				SenderChat: &tbapi.Chat{
					ID: 54321,
				},
			},
			SenderChat: &tbapi.Chat{ID: 54321},
		},
	}

	updChan := make(chan tbapi.Update, 2)
	updChan <- updMsg
	close(updChan)
	mockAPI.GetUpdatesChanFunc = func(config tbapi.UpdateConfig) tbapi.UpdatesChannel { return updChan }

	err := l.Do(ctx)
	assert.EqualError(t, err, "telegram update chan closed")
	require.Equal(t, 1, len(mockLogger.SaveCalls()))
	assert.Equal(t, "text 123", mockLogger.SaveCalls()[0].Msg.Text)
	assert.Equal(t, "user", mockLogger.SaveCalls()[0].Msg.From.Username)
	// asking to delete the message produces DeleteMessageConfig call with the same message and channel IDs
	require.Equal(t, 2, len(mockAPI.RequestCalls()))
	assert.Equal(t, 321, mockAPI.RequestCalls()[1].C.(tbapi.DeleteMessageConfig).MessageID)
	assert.Equal(t, int64(123), mockAPI.RequestCalls()[1].C.(tbapi.DeleteMessageConfig).ChatID)
}

func TestTelegramListener_DoWithForwarded(t *testing.T) {
	mockLogger := &mocks.SpamLoggerMock{SaveFunc: func(msg *bot.Message, response *bot.Response) {}}
	mockAPI := &mocks.TbAPIMock{
		GetChatFunc: func(config tbapi.ChatInfoConfig) (tbapi.Chat, error) {
			return tbapi.Chat{ID: 123}, nil
		},
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			return tbapi.Message{Text: c.(tbapi.MessageConfig).Text, From: &tbapi.User{UserName: "user"}}, nil
		},
	}
	b := &mocks.BotMock{
		OnMessageFunc: func(msg bot.Message) bot.Response {
			t.Logf("on-message: %+v", msg)
			if msg.Text == "text 123" && msg.From.Username == "user" {
				return bot.Response{Send: true, Text: "bot's answer"}
			}
			return bot.Response{}
		},
		UpdateSpamFunc: func(msg string) error {
			t.Logf("update-spam: %s", msg)
			return nil
		},
	}

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		Group:      "gr",
		AdminGroup: "123",
		StartupMsg: "startup",
		SuperUsers: SuperUser{"umputun"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		Message: &tbapi.Message{
			Chat:              &tbapi.Chat{ID: 123},
			Text:              "text 123",
			From:              &tbapi.User{UserName: "umputun"},
			Date:              int(time.Date(2020, 2, 11, 19, 35, 55, 9, time.UTC).Unix()),
			ForwardSenderName: "forwarded_name",
		},
	}

	updChan := make(chan tbapi.Update, 1)
	updChan <- updMsg
	close(updChan)
	mockAPI.GetUpdatesChanFunc = func(config tbapi.UpdateConfig) tbapi.UpdatesChannel { return updChan }

	err := l.Do(ctx)
	assert.EqualError(t, err, "telegram update chan closed")
	assert.Equal(t, 0, len(mockLogger.SaveCalls()))
	require.Equal(t, 1, len(mockAPI.SendCalls()))
	assert.Equal(t, "startup", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
	require.Equal(t, 1, len(b.UpdateSpamCalls()))
	assert.Equal(t, "text 123", b.UpdateSpamCalls()[0].Msg)
}

func TestTelegram_transformTextMessage(t *testing.T) {
	l := TelegramListener{}
	assert.Equal(
		t,
		&bot.Message{
			ID: 30,
			From: bot.User{
				ID:          100000001,
				Username:    "username",
				DisplayName: "First Last",
			},
			Sent:   time.Unix(1578627415, 0),
			Text:   "Message",
			ChatID: 123456,
		},
		l.transform(
			&tbapi.Message{
				Chat: &tbapi.Chat{
					ID: 123456,
				},
				From: &tbapi.User{
					ID:        100000001,
					UserName:  "username",
					FirstName: "First",
					LastName:  "Last",
				},
				MessageID: 30,
				Date:      1578627415,
				Text:      "Message",
			},
		),
	)
}

func TestTelegram_transformPhoto(t *testing.T) {
	l := TelegramListener{}
	assert.Equal(
		t,
		&bot.Message{
			Sent: time.Unix(1578627415, 0),
			Image: &bot.Image{
				FileID:  "AgADAgADFKwxG8r0qUiQByxwp9Gi4s1qwQ8ABAEAAwIAA3kAA5K9AgABFgQ",
				Width:   1280,
				Height:  597,
				Caption: "caption",
				Entities: &[]bot.Entity{
					{
						Type:   "bold",
						Offset: 0,
						Length: 7,
					},
				},
			},
		},
		l.transform(
			&tbapi.Message{
				Date: 1578627415,
				Photo: []tbapi.PhotoSize{
					{
						FileID:   "AgADAgADFKwxG8r0qUiQByxwp9Gi4s1qwQ8ABAEAAwIAA20AA5C9AgABFgQ",
						Width:    320,
						Height:   149,
						FileSize: 6262,
					},
					{
						FileID:   "AgADAgADFKwxG8r0qUiQByxwp9Gi4s1qwQ8ABAEAAwIAA3gAA5G9AgABFgQ",
						Width:    800,
						Height:   373,
						FileSize: 30240,
					},
					{
						FileID:   "AgADAgADFKwxG8r0qUiQByxwp9Gi4s1qwQ8ABAEAAwIAA3kAA5K9AgABFgQ",
						Width:    1280,
						Height:   597,
						FileSize: 55267,
					},
				},
				Caption: "caption",
				CaptionEntities: []tbapi.MessageEntity{
					{
						Type:   "bold",
						Offset: 0,
						Length: 7,
					},
				},
			},
		),
	)
}

func TestTelegram_transformEntities(t *testing.T) {
	l := TelegramListener{}
	assert.Equal(
		t,
		&bot.Message{
			Sent: time.Unix(1578627415, 0),
			Text: "@username тебя слишком много, отдохни...",
			Entities: &[]bot.Entity{
				{
					Type:   "mention",
					Offset: 0,
					Length: 9,
				},
				{
					Type:   "italic",
					Offset: 10,
					Length: 30,
				},
			},
		},
		l.transform(
			&tbapi.Message{
				Date: 1578627415,
				Text: "@username тебя слишком много, отдохни...",
				Entities: []tbapi.MessageEntity{
					{
						Type:   "mention",
						Offset: 0,
						Length: 9,
					},
					{
						Type:   "italic",
						Offset: 10,
						Length: 30,
					},
				},
			},
		),
	)
}

func TestTelegramListener_isChatAllowed(t *testing.T) {
	testCases := []struct {
		name       string
		fromChat   int64
		chatID     int64
		testingIDs []int64
		expect     bool
	}{
		{
			name:       "Chat is allowed - fromChat equals chatID",
			fromChat:   123,
			chatID:     123,
			testingIDs: []int64{},
			expect:     true,
		},
		{
			name:       "Chat is allowed - fromChat in testingIDs",
			fromChat:   456,
			chatID:     123,
			testingIDs: []int64{456},
			expect:     true,
		},
		{
			name:       "Chat is not allowed",
			fromChat:   789,
			chatID:     123,
			testingIDs: []int64{456},
			expect:     false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			listener := TelegramListener{
				chatID:     tc.chatID,
				TestingIDs: tc.testingIDs,
			}
			result := listener.isChatAllowed(tc.fromChat)
			assert.Equal(t, tc.expect, result)
		})
	}
}

func TestTelegramListener_forwardToAdmin(t *testing.T) {
	mockAPI := &mocks.TbAPIMock{
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			return tbapi.Message{}, nil
		},
	}

	sr := &mocks.SpamWebMock{
		UnbanURLFunc: func(userID int64) string { return "url" },
	}

	listener := TelegramListener{
		TbAPI:       mockAPI,
		SpamWeb:     sr,
		adminChatID: 123,
	}

	msg := &bot.Message{
		From: bot.User{
			ID: 456,
		},
		Text: "Test\n\n_message_",
	}

	listener.forwardToAdmin("testUser", msg)

	require.Equal(t, 1, len(mockAPI.SendCalls()))
	assert.Equal(t, int64(123), mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ChatID)
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text, "permanently banned [testUser](tg://user?id=456)")
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text, "Test  \\_message\\_")
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text, "[⛔︎ unban if wrong ⛔︎](url)")
}

func TestTelegramListener_isAdminChat(t *testing.T) {
	testCases := []struct {
		name     string
		fromChat int64
		fromUser string
		chatID   int64
		expect   bool
	}{
		{
			name:     "allowed, fromUser is superuser and fromChat equals chatID",
			fromChat: 123,
			chatID:   123,
			fromUser: "umputun",
			expect:   true,
		},
		{
			name:     "not allowed, fromUser is superuser and fromChat is not chatID",
			fromChat: 456,
			chatID:   123777,
			fromUser: "umputun",
			expect:   false,
		},
		{
			name:     "not allowed, fromUser is not superuser and fromChat is chatID",
			fromChat: 456,
			chatID:   123,
			fromUser: "user",
			expect:   false,
		},
		{
			name:     "not allowed, fromUser is not superuser and fromChat is not chatID",
			fromChat: 456,
			chatID:   123777,
			fromUser: "user",
			expect:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			listener := TelegramListener{
				adminChatID: tc.chatID,
				SuperUsers:  SuperUser{"umputun"},
			}
			result := listener.isAdminChat(tc.fromChat, tc.fromUser)
			assert.Equal(t, tc.expect, result)
		})
	}
}
