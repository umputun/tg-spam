package events

import (
	"context"
	"errors"
	"testing"
	"time"

	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/events/mocks"
	"github.com/umputun/tg-spam/lib"
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
		GetChatAdministratorsFunc: func(config tbapi.ChatAdministratorsConfig) ([]tbapi.ChatMember, error) {
			return []tbapi.ChatMember{
				{
					User: &tbapi.User{
						UserName: "admin",
						ID:       1,
					},
					Status: "administrator",
				},
			}, nil
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
		Locator:    NewLocator(10*time.Minute, 10),
		SuperUsers: SuperUser{"super"},
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
	assert.Equal(t, SuperUser{"super", "admin"}, l.SuperUsers)

	assert.Equal(t, 0, len(mockLogger.SaveCalls()))
	require.Equal(t, 2, len(mockAPI.SendCalls()))
	assert.Equal(t, "startup", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
	assert.Equal(t, "bot's answer", mockAPI.SendCalls()[1].C.(tbapi.MessageConfig).Text)
	assert.Equal(t, 1, len(mockAPI.GetChatAdministratorsCalls()))

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
		GetChatAdministratorsFunc: func(config tbapi.ChatAdministratorsConfig) ([]tbapi.ChatMember, error) {
			return nil, nil
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
		Locator:    NewLocator(10*time.Minute, 0),
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

func TestTelegramListener_DoWithTraining(t *testing.T) {
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
		GetChatAdministratorsFunc: func(config tbapi.ChatAdministratorsConfig) ([]tbapi.ChatMember, error) {
			return nil, nil
		},
	}
	b := &mocks.BotMock{OnMessageFunc: func(msg bot.Message) bot.Response {
		t.Logf("on-message: %+v", msg)
		return bot.Response{DeleteReplyTo: true, ReplyTo: msg.ID, ChannelID: msg.ChatID, BanInterval: time.Hour,
			Send: true, Text: "bot's answer", User: bot.User{Username: "user", ID: 1, DisplayName: "First Last"}}
	}}

	l := TelegramListener{
		SpamLogger:   mockLogger,
		TbAPI:        mockAPI,
		Bot:          b,
		Group:        "gr",
		Locator:      NewLocator(10*time.Minute, 0),
		TrainingMode: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		Message: &tbapi.Message{
			Chat: &tbapi.Chat{ID: 123},
			Text: "text 321",
			From: &tbapi.User{UserName: "user", ID: 136817688},
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
	assert.Equal(t, "user", mockLogger.SaveCalls()[0].Msg.From.Username)
	assert.Equal(t, 0, len(mockAPI.SendCalls()), "no messages should be sent in training mode")
	assert.Equal(t, 0, len(mockAPI.RequestCalls()))

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
		GetChatAdministratorsFunc: func(config tbapi.ChatAdministratorsConfig) ([]tbapi.ChatMember, error) {
			return nil, nil
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
		Locator:    NewLocator(10*time.Minute, 0),
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
		RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
			return &tbapi.APIResponse{Ok: true}, nil
		},
		GetChatAdministratorsFunc: func(config tbapi.ChatAdministratorsConfig) ([]tbapi.ChatMember, error) { return nil, nil },
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
		Locator:    NewLocator(10*time.Minute, 0),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	l.Locator.AddMessage("text 123", 123, 88, 999999) // add message to locator

	updMsg := tbapi.Update{
		Message: &tbapi.Message{
			Chat:              &tbapi.Chat{ID: 123},
			Text:              "text 123",
			From:              &tbapi.User{UserName: "umputun", ID: 77},
			Date:              int(time.Date(2020, 2, 11, 19, 35, 55, 9, time.UTC).Unix()),
			ForwardSenderName: "forwarded_name",
			MessageID:         999999,
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

	assert.Equal(t, 2, len(mockAPI.RequestCalls()))
	assert.Equal(t, int64(123), mockAPI.RequestCalls()[0].C.(tbapi.DeleteMessageConfig).ChatID)
	assert.Equal(t, 999999, mockAPI.RequestCalls()[0].C.(tbapi.DeleteMessageConfig).MessageID)

	assert.Equal(t, int64(123), mockAPI.RequestCalls()[1].C.(tbapi.RestrictChatMemberConfig).ChatID)
	assert.Equal(t, int64(88), mockAPI.RequestCalls()[1].C.(tbapi.RestrictChatMemberConfig).UserID)
}

func TestTelegramListener_DoWithAdminUnBan(t *testing.T) {
	mockLogger := &mocks.SpamLoggerMock{}
	mockAPI := &mocks.TbAPIMock{
		GetChatFunc: func(config tbapi.ChatInfoConfig) (tbapi.Chat, error) {
			return tbapi.Chat{ID: 123}, nil
		},
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			if mc, ok := c.(tbapi.MessageConfig); ok {
				return tbapi.Message{Text: mc.Text, From: &tbapi.User{UserName: "user"}}, nil
			}
			return tbapi.Message{}, nil
		},
		RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
			return &tbapi.APIResponse{}, nil
		},
		GetChatAdministratorsFunc: func(config tbapi.ChatAdministratorsConfig) ([]tbapi.ChatMember, error) { return nil, nil },
	}
	b := &mocks.BotMock{
		UpdateHamFunc: func(msg string) error {
			return nil
		},
		AddApprovedUsersFunc: func(id int64, ids ...int64) {},
	}

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		SuperUsers: SuperUser{"admin"},
		Group:      "gr",
		Locator:    NewLocator(10*time.Minute, 0),
		AdminGroup: "123",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		CallbackQuery: &tbapi.CallbackQuery{
			Data: "777",
			Message: &tbapi.Message{
				MessageID:   987654,
				Chat:        &tbapi.Chat{ID: 123},
				Text:        "unban user blah\n\nthis was the ham, not spam",
				From:        &tbapi.User{UserName: "user", ID: 999},
				ForwardDate: int(time.Date(2020, 2, 11, 19, 35, 55, 9, time.UTC).Unix()),
			},
			From: &tbapi.User{UserName: "admin", ID: 1000},
		},
	}
	updChan := make(chan tbapi.Update, 1)
	updChan <- updMsg
	close(updChan)
	mockAPI.GetUpdatesChanFunc = func(config tbapi.UpdateConfig) tbapi.UpdatesChannel { return updChan }

	err := l.Do(ctx)
	assert.EqualError(t, err, "telegram update chan closed")
	require.Equal(t, 1, len(mockAPI.SendCalls()))
	assert.Equal(t, 987654, mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).MessageID)
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).Text, "by admin in ")
	require.Equal(t, 2, len(mockAPI.RequestCalls()))
	assert.Equal(t, "accepted", mockAPI.RequestCalls()[0].C.(tbapi.CallbackConfig).Text)

	assert.Equal(t, int64(777), mockAPI.RequestCalls()[1].C.(tbapi.UnbanChatMemberConfig).UserID)
	require.Equal(t, 1, len(b.UpdateHamCalls()))
	assert.Equal(t, "this was the ham, not spam", b.UpdateHamCalls()[0].Msg)
	require.Equal(t, 1, len(b.AddApprovedUsersCalls()))
	assert.Equal(t, int64(777), b.AddApprovedUsersCalls()[0].ID)
}

func TestTelegramListener_DoWithAdminUnBan_Training(t *testing.T) {
	mockLogger := &mocks.SpamLoggerMock{}
	mockAPI := &mocks.TbAPIMock{
		GetChatFunc: func(config tbapi.ChatInfoConfig) (tbapi.Chat, error) {
			return tbapi.Chat{ID: 123}, nil
		},
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			if mc, ok := c.(tbapi.MessageConfig); ok {
				return tbapi.Message{Text: mc.Text, From: &tbapi.User{UserName: "user"}}, nil
			}
			return tbapi.Message{}, nil
		},
		RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
			return &tbapi.APIResponse{}, nil
		},
		GetChatAdministratorsFunc: func(config tbapi.ChatAdministratorsConfig) ([]tbapi.ChatMember, error) { return nil, nil },
	}
	b := &mocks.BotMock{
		UpdateHamFunc: func(msg string) error {
			return nil
		},
		AddApprovedUsersFunc: func(id int64, ids ...int64) {},
	}

	l := TelegramListener{
		SpamLogger:   mockLogger,
		TbAPI:        mockAPI,
		Bot:          b,
		SuperUsers:   SuperUser{"admin"},
		Group:        "gr",
		Locator:      NewLocator(10*time.Minute, 0),
		AdminGroup:   "123",
		TrainingMode: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		CallbackQuery: &tbapi.CallbackQuery{
			Data: "777",
			Message: &tbapi.Message{
				MessageID:   987654,
				Chat:        &tbapi.Chat{ID: 123},
				Text:        "unban user blah\n\nthis was the ham, not spam",
				From:        &tbapi.User{UserName: "user", ID: 999},
				ForwardDate: int(time.Date(2020, 2, 11, 19, 35, 55, 9, time.UTC).Unix()),
			},
			From: &tbapi.User{UserName: "admin", ID: 1000},
		},
	}
	updChan := make(chan tbapi.Update, 1)
	updChan <- updMsg
	close(updChan)
	mockAPI.GetUpdatesChanFunc = func(config tbapi.UpdateConfig) tbapi.UpdatesChannel { return updChan }

	err := l.Do(ctx)
	assert.EqualError(t, err, "telegram update chan closed")
	require.Equal(t, 1, len(mockAPI.SendCalls()))
	assert.Equal(t, 987654, mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).MessageID)
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).Text, "by admin in ")
	require.Equal(t, 1, len(mockAPI.RequestCalls()))
	assert.Equal(t, "accepted", mockAPI.RequestCalls()[0].C.(tbapi.CallbackConfig).Text)
	require.Equal(t, 1, len(b.UpdateHamCalls()))
	assert.Equal(t, "this was the ham, not spam", b.UpdateHamCalls()[0].Msg)
	require.Equal(t, 1, len(b.AddApprovedUsersCalls()))
	assert.Equal(t, int64(777), b.AddApprovedUsersCalls()[0].ID)
}

func TestTelegramListener_DoWithAdminUnBanConfirmation(t *testing.T) {
	mockLogger := &mocks.SpamLoggerMock{}
	mockAPI := &mocks.TbAPIMock{
		GetChatFunc: func(config tbapi.ChatInfoConfig) (tbapi.Chat, error) {
			return tbapi.Chat{ID: 123}, nil
		},
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			if mc, ok := c.(tbapi.MessageConfig); ok {
				return tbapi.Message{Text: mc.Text, From: &tbapi.User{UserName: "user"}}, nil
			}
			return tbapi.Message{}, nil
		},
		RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
			return &tbapi.APIResponse{}, nil
		},
		GetChatAdministratorsFunc: func(config tbapi.ChatAdministratorsConfig) ([]tbapi.ChatMember, error) { return nil, nil },
	}
	b := &mocks.BotMock{
		UpdateHamFunc: func(msg string) error {
			return nil
		},
		AddApprovedUsersFunc: func(id int64, ids ...int64) {},
	}

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		SuperUsers: SuperUser{"admin"},
		Group:      "gr",
		Locator:    NewLocator(10*time.Minute, 0),
		AdminGroup: "123",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		CallbackQuery: &tbapi.CallbackQuery{
			Data: "?777", // ? means confirmation
			Message: &tbapi.Message{
				MessageID:   987654,
				Chat:        &tbapi.Chat{ID: 123},
				Text:        "unban user blah\n\nthis was the ham, not spam",
				From:        &tbapi.User{UserName: "user", ID: 999},
				ForwardDate: int(time.Date(2020, 2, 11, 19, 35, 55, 9, time.UTC).Unix()),
			},
			From: &tbapi.User{UserName: "admin", ID: 1000},
		},
	}
	updChan := make(chan tbapi.Update, 1)
	updChan <- updMsg
	close(updChan)
	mockAPI.GetUpdatesChanFunc = func(config tbapi.UpdateConfig) tbapi.UpdatesChannel { return updChan }

	err := l.Do(ctx)
	assert.EqualError(t, err, "telegram update chan closed")
	require.Equal(t, 1, len(mockAPI.SendCalls()))
	assert.Equal(t, 987654, mockAPI.SendCalls()[0].C.(tbapi.EditMessageReplyMarkupConfig).MessageID)
	kb := mockAPI.SendCalls()[0].C.(tbapi.EditMessageReplyMarkupConfig).ReplyMarkup.InlineKeyboard
	assert.Equal(t, 2, len(kb[0]), " tow yes/no buttons")
	assert.Equal(t, 0, len(mockAPI.RequestCalls()))
	assert.Equal(t, 0, len(b.UpdateHamCalls()))
	require.Equal(t, 0, len(b.AddApprovedUsersCalls()))
}

func TestTelegramListener_DoWithAdminUnbanDecline(t *testing.T) {
	mockLogger := &mocks.SpamLoggerMock{}
	mockAPI := &mocks.TbAPIMock{
		GetChatFunc: func(config tbapi.ChatInfoConfig) (tbapi.Chat, error) {
			return tbapi.Chat{ID: 123}, nil
		},
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			if mc, ok := c.(tbapi.MessageConfig); ok {
				return tbapi.Message{Text: mc.Text, From: &tbapi.User{UserName: "user"}}, nil
			}
			return tbapi.Message{}, nil
		},
		RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
			return &tbapi.APIResponse{}, nil
		},
		GetChatAdministratorsFunc: func(config tbapi.ChatAdministratorsConfig) ([]tbapi.ChatMember, error) { return nil, nil },
	}
	b := &mocks.BotMock{
		UpdateSpamFunc: func(msg string) error {
			return nil
		},
		AddApprovedUsersFunc: func(id int64, ids ...int64) {},
	}

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		SuperUsers: SuperUser{"admin"},
		Group:      "gr",
		Locator:    NewLocator(10*time.Minute, 0),
		AdminGroup: "123",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		CallbackQuery: &tbapi.CallbackQuery{
			Data: "+999", // + means unban declined
			Message: &tbapi.Message{
				MessageID:   987654,
				Chat:        &tbapi.Chat{ID: 123},
				Text:        "unban user blah\n\nthis was the ham, not spam",
				From:        &tbapi.User{UserName: "user", ID: 999},
				ForwardDate: int(time.Date(2020, 2, 11, 19, 35, 55, 9, time.UTC).Unix()),
			},
			From: &tbapi.User{UserName: "admin", ID: 1000},
		},
	}
	updChan := make(chan tbapi.Update, 1)
	updChan <- updMsg
	close(updChan)
	mockAPI.GetUpdatesChanFunc = func(config tbapi.UpdateConfig) tbapi.UpdatesChannel { return updChan }

	err := l.Do(ctx)
	assert.EqualError(t, err, "telegram update chan closed")
	require.Equal(t, 1, len(mockAPI.SendCalls()))
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).Text, "unban user blah")
	kb := mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).ReplyMarkup.InlineKeyboard
	assert.Equal(t, 0, len(kb), "buttons cleared")
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).Text, "confirmed by admin in ")
	assert.Equal(t, 0, len(mockAPI.RequestCalls()))
	assert.Equal(t, 1, len(b.UpdateSpamCalls()))
	assert.Equal(t, 0, len(b.UpdateHamCalls()))
	require.Equal(t, 0, len(b.AddApprovedUsersCalls()))
}

func TestTelegramListener_DoWithAdminShowInfo(t *testing.T) {
	mockLogger := &mocks.SpamLoggerMock{}
	mockAPI := &mocks.TbAPIMock{
		GetChatFunc: func(config tbapi.ChatInfoConfig) (tbapi.Chat, error) {
			return tbapi.Chat{ID: 123}, nil
		},
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			if mc, ok := c.(tbapi.MessageConfig); ok {
				return tbapi.Message{Text: mc.Text, From: &tbapi.User{UserName: "user"}}, nil
			}
			return tbapi.Message{}, nil
		},
		RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
			return &tbapi.APIResponse{}, nil
		},
		GetChatAdministratorsFunc: func(config tbapi.ChatAdministratorsConfig) ([]tbapi.ChatMember, error) { return nil, nil },
	}
	b := &mocks.BotMock{}

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		SuperUsers: SuperUser{"admin"},
		Group:      "gr",
		Locator:    NewLocator(10*time.Minute, 0),
		AdminGroup: "123",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		CallbackQuery: &tbapi.CallbackQuery{
			Data: "!999", // ! means we show info
			Message: &tbapi.Message{
				MessageID:   987654,
				Chat:        &tbapi.Chat{ID: 123},
				Text:        "unban user blah\n\nthis was the spam",
				From:        &tbapi.User{UserName: "user", ID: 999},
				ForwardDate: int(time.Date(2020, 2, 11, 19, 35, 55, 9, time.UTC).Unix()),
			},
			From: &tbapi.User{UserName: "admin", ID: 1000},
		},
	}
	updChan := make(chan tbapi.Update, 1)
	updChan <- updMsg
	close(updChan)
	mockAPI.GetUpdatesChanFunc = func(config tbapi.UpdateConfig) tbapi.UpdatesChannel { return updChan }

	l.Locator.AddSpam(999, []lib.CheckResult{{Name: "rule1", Spam: true, Details: "details1"}, {Name: "rule2", Spam: true, Details: "details2"}})

	err := l.Do(ctx)
	assert.EqualError(t, err, "telegram update chan closed")
	require.Equal(t, 1, len(mockAPI.SendCalls()))
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).Text, "unban user blah")
	kb := mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).ReplyMarkup.InlineKeyboard
	assert.Equal(t, 0, len(kb), "buttons cleared")
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).Text, "results**\n- rule1: spam, details1\n- rule2: spam, details2")
	assert.Equal(t, 0, len(mockAPI.RequestCalls()))
	assert.Equal(t, 0, len(b.UpdateSpamCalls()))
	assert.Equal(t, 0, len(b.UpdateHamCalls()))
	require.Equal(t, 0, len(b.AddApprovedUsersCalls()))
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

func TestTelegramListener_reportToAdminChat(t *testing.T) {
	mockAPI := &mocks.TbAPIMock{
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			return tbapi.Message{}, nil
		},
	}

	listener := TelegramListener{
		TbAPI:       mockAPI,
		adminChatID: 123,
	}

	msg := &bot.Message{
		From: bot.User{
			ID: 456,
		},
		Text: "Test\n\n_message_",
	}

	listener.reportToAdminChat("testUser", msg)

	require.Equal(t, 1, len(mockAPI.SendCalls()))
	t.Logf("sent text: %+v", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
	assert.Equal(t, int64(123), mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ChatID)
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text, "permanently banned [testUser](tg://user?id=456)")
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text, "Test  \\_message\\_")
	assert.NotNil(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ReplyMarkup)
	assert.Equal(t, "⛔︎ change ban status for user",
		mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ReplyMarkup.(tbapi.InlineKeyboardMarkup).InlineKeyboard[0][0].Text)
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

func TestSuperUser_IsSuper(t *testing.T) {
	tests := []struct {
		name     string
		super    SuperUser
		userName string
		want     bool
	}{
		{
			name:     "User is a super user",
			super:    SuperUser{"Alice", "Bob"},
			userName: "Alice",
			want:     true,
		},
		{
			name:     "User is not a super user",
			super:    SuperUser{"Alice", "Bob"},
			userName: "Charlie",
			want:     false,
		},
		{
			name:     "User is a super user with slash prefix",
			super:    SuperUser{"/Alice", "Bob"},
			userName: "Alice",
			want:     true,
		},
		{
			name:     "User is not a super user with slash prefix",
			super:    SuperUser{"/Alice", "Bob"},
			userName: "Charlie",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.super.IsSuper(tt.userName))
		})
	}
}

func TestUpdateSupers(t *testing.T) {
	tests := []struct {
		name            string
		superUsers      SuperUser
		chatAdmins      []tbapi.ChatMember
		adminFetchError error
		expectedResult  []string
		expectedErr     bool
	}{
		{
			name:           "empty admins",
			chatAdmins:     make([]tbapi.ChatMember, 0),
			expectedResult: make([]string, 0),
			expectedErr:    false,
		},
		{
			name:           "non-empty admin usernames",
			chatAdmins:     []tbapi.ChatMember{{User: &tbapi.User{UserName: "admin1"}}, {User: &tbapi.User{UserName: "admin2"}}},
			expectedResult: []string{"admin1", "admin2"},
			expectedErr:    false,
		},
		{
			name:           "non-empty admin usernames, existing supers",
			superUsers:     SuperUser{"super1"},
			chatAdmins:     []tbapi.ChatMember{{User: &tbapi.User{UserName: "admin1"}}, {User: &tbapi.User{UserName: "admin2"}}},
			expectedResult: []string{"super1", "admin1", "admin2"},
			expectedErr:    false,
		},
		{
			name:           "non-empty admin usernames, existing supers with duplicate",
			superUsers:     SuperUser{"admin1"},
			chatAdmins:     []tbapi.ChatMember{{User: &tbapi.User{UserName: "admin1"}}, {User: &tbapi.User{UserName: "admin2"}}},
			expectedResult: []string{"admin1", "admin2"},
			expectedErr:    false,
		},
		{
			name: "admin usernames with empty string",
			chatAdmins: []tbapi.ChatMember{{User: &tbapi.User{UserName: "admin1"}}, {User: &tbapi.User{UserName: ""}},
				{User: &tbapi.User{UserName: "admin2"}}},
			expectedResult: []string{"admin1", "admin2"},
			expectedErr:    false,
		},
		{
			name:            "fetching admins returns error",
			chatAdmins:      []tbapi.ChatMember{},
			adminFetchError: errors.New("fetch error"),
			expectedResult:  []string{},
			expectedErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &TelegramListener{
				TbAPI: &mocks.TbAPIMock{
					GetChatAdministratorsFunc: func(config tbapi.ChatAdministratorsConfig) ([]tbapi.ChatMember, error) {
						return tt.chatAdmins, tt.adminFetchError
					},
				},
				SuperUsers: tt.superUsers,
			}

			err := l.updateSupers()
			if tt.expectedErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.ElementsMatch(t, tt.expectedResult, l.SuperUsers)
			}
		})
	}
}

func TestGetCleanMessage(t *testing.T) {
	tl := &TelegramListener{}

	tests := []struct {
		name     string
		input    string
		expected string
		err      bool
	}{
		{
			name:     "with spam detection results",
			input:    "Line 1\nLine 2\nspam detection results:\nLine 4",
			expected: "Line 2",
			err:      false,
		},
		{
			name:     "without spam detection results",
			input:    "Line 1\nLine 2\nLine 3",
			expected: "Line 2 Line 3",
			err:      false,
		},
		{
			name:     "only one line",
			input:    "Line 1",
			expected: "",
			err:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tl.getCleanMessage(tt.input)
			if tt.err {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
