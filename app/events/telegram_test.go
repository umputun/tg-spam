package events

import (
	"context"
	"net/http"
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
	assert.Equal(t, 1, len(mockAPI.SendCalls()))
	assert.Equal(t, "bot's answer", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
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
	// t.Run("test ban of the channel on behalf of the superuser", func(t *testing.T) {
	// 	mockLogger.ResetCalls()
	// 	mockAPI.ResetCalls()
	// 	updMsg := tbapi.Update{
	// 		Message: &tbapi.Message{
	// 			ReplyToMessage: &tbapi.Message{
	// 				SenderChat: &tbapi.Chat{
	// 					ID:       54321,
	// 					UserName: "original_bot",
	// 				},
	// 			},
	// 			Chat: &tbapi.Chat{ID: 123},
	// 			Text: "text 543",
	// 			From: &tbapi.User{UserName: "admin", ID: 555},
	// 			Date: int(time.Date(2020, 2, 11, 19, 37, 55, 9, time.UTC).Unix()),
	// 		},
	// 	}
	//
	// 	updChan := make(chan tbapi.Update, 1)
	// 	updChan <- updMsg
	// 	close(updChan)
	// 	mockAPI.GetUpdatesChanFunc = func(config tbapi.UpdateConfig) tbapi.UpdatesChannel { return updChan }
	//
	// 	err := l.Do(ctx)
	// 	assert.EqualError(t, err, "telegram update chan closed")
	// 	require.Equal(t, 1, len(mockLogger.SaveCalls()))
	// 	assert.Equal(t, "text 543", mockLogger.SaveCalls()[0].Msg.Text)
	// 	assert.Equal(t, "admin", mockLogger.SaveCalls()[0].Msg.From.Username)
	// 	assert.Equal(t, "bot's answer for admin", mockLogger.SaveCalls()[0].Response.Text)
	// 	assert.Equal(t, 1, len(mockAPI.SendCalls()))
	// 	assert.Equal(t, "bot's answer for admin", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
	// 	require.Equal(t, 0, len(mockAPI.RequestCalls()))
	// })
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

func TestTelegramListener_unbanURL(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		secret string
		want   string
	}{
		{"empty", "", "", "?user=123&token=d68b50c4f0747630c33bc736bb3087b4c22f19dc645ec63b3bf90760c553e1ae"},
		{"test1", "http://localhost/unban", "secret",
			"http://localhost/unban?user=123&token=71199ea8c011a49df546451e456ad10b0016566a53c4861bf849ec6b2ad2a0b7"},
		{"test2", "http://127.0.0.1:8080/unban", "secret",
			"http://127.0.0.1:8080/unban?user=123&token=71199ea8c011a49df546451e456ad10b0016566a53c4861bf849ec6b2ad2a0b7"},
		{"test3", "http://127.0.0.1:8080/unban", "secret2",
			"http://127.0.0.1:8080/unban?user=123&token=5385a71e8d5b65ea03e3da10175d78028ae59efd58811004e907baf422019b2e"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			listener := TelegramListener{AdminURL: tt.url, AdminSecret: tt.secret}
			res := listener.unbanURL(123)
			assert.Equal(t, tt.want, res)
		})
	}

	listener := TelegramListener{AdminURL: "http://localhost/unban", AdminSecret: "secret"}
	res := listener.unbanURL(123)
	assert.Equal(t, "http://localhost/unban?user=123&token=71199ea8c011a49df546451e456ad10b0016566a53c4861bf849ec6b2ad2a0b7", res)
}

func TestTelegramListener_runUnbanServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	mockAPI := &mocks.TbAPIMock{
		GetChatFunc: func(config tbapi.ChatInfoConfig) (tbapi.Chat, error) {
			return tbapi.Chat{ID: 123}, nil
		},
		RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
			return &tbapi.APIResponse{}, nil
		},
	}

	mockBot := &mocks.BotMock{}

	listener := TelegramListener{
		TbAPI:           mockAPI,
		Bot:             mockBot,
		AdminListenAddr: ":9900",
		AdminURL:        "http://localhost:9090",
		AdminSecret:     "secret",
		chatID:          10,
	}

	done := make(chan struct{})
	go func() {
		listener.runUnbanServer(ctx)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond) // wait for server to start

	t.Run("unban forbidden, wrong token", func(t *testing.T) {
		mockAPI.ResetCalls()
		req, err := http.NewRequest("GET", "http://localhost:9900/unban?user=123&token=ssss", http.NoBody)
		require.NoError(t, err)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.Equal(t, 0, len(mockAPI.RequestCalls()))
	})

	t.Run("unban allowed, matched token", func(t *testing.T) {
		mockAPI.ResetCalls()
		req, err := http.NewRequest("GET",
			"http://localhost:9900/unban?user=123&token=71199ea8c011a49df546451e456ad10b0016566a53c4861bf849ec6b2ad2a0b7", http.NoBody)
		require.NoError(t, err)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, 1, len(mockAPI.RequestCalls()))
		assert.Equal(t, int64(10), mockAPI.RequestCalls()[0].C.(tbapi.UnbanChatMemberConfig).ChatID)
		assert.Equal(t, int64(123), mockAPI.RequestCalls()[0].C.(tbapi.UnbanChatMemberConfig).UserID)
	})

	<-done
}
