package events

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/events/mocks"
	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/lib/spamcheck"
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

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		Group:      "gr",
		AdminGroup: "987654321",
		StartupMsg: "startup",
		Locator:    locator,
		SuperUsers: SuperUsers{"super"},
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
	assert.Equal(t, SuperUsers{"super", "admin"}, l.SuperUsers)

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
			return bot.Response{Send: true, Text: "bot's answer", BanInterval: 2 * time.Minute,
				User: bot.User{Username: "user", ID: 1}, CheckResults: []spamcheck.Response{
					{Name: "Check1", Spam: true, Details: "Details 1"}}}
		}
		if msg.From.Username == "ChannelBot" {
			return bot.Response{Send: true, Text: "bot's answer for channel", BanInterval: 2 * time.Minute, User: bot.User{Username: "user", ID: 1}, ChannelID: msg.SenderChat.ID}
		}
		if msg.From.Username == "admin" {
			return bot.Response{Send: true, Text: "bot's answer for admin", BanInterval: 2 * time.Minute, User: bot.User{Username: "user", ID: 1}, ChannelID: msg.ReplyTo.SenderChat.ID}
		}
		return bot.Response{}
	}}

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		SuperUsers: SuperUsers{"admin"},
		Group:      "gr",
		Locator:    locator,
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
		assert.Equal(t, int64(123), mockAPI.RequestCalls()[0].C.(tbapi.BanChatMemberConfig).ChatID)
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

func TestTelegramListener_DoWithBotSoftBan(t *testing.T) {
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
			return bot.Response{Send: true, Text: "bot's answer", BanInterval: 2 * time.Minute,
				User: bot.User{Username: "user", ID: 1}, CheckResults: []spamcheck.Response{
					{Name: "Check1", Spam: true, Details: "Details 1"}}}
		}
		if msg.From.Username == "ChannelBot" {
			return bot.Response{Send: true, Text: "bot's answer for channel", BanInterval: 2 * time.Minute, User: bot.User{Username: "user", ID: 1}, ChannelID: msg.SenderChat.ID}
		}
		if msg.From.Username == "admin" {
			return bot.Response{Send: true, Text: "bot's answer for admin", BanInterval: 2 * time.Minute, User: bot.User{Username: "user", ID: 1}, ChannelID: msg.ReplyTo.SenderChat.ID}
		}
		return bot.Response{}
	}}

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger:  mockLogger,
		TbAPI:       mockAPI,
		Bot:         b,
		SuperUsers:  SuperUsers{"admin"},
		Group:       "gr",
		Locator:     locator,
		SoftBanMode: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

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
	assert.Equal(t, int64(1), mockAPI.RequestCalls()[0].C.(tbapi.RestrictChatMemberConfig).UserID)
	assert.Equal(t, &tbapi.ChatPermissions{CanSendMessages: false, CanSendMediaMessages: false, CanSendOtherMessages: false,
		CanSendPolls: false}, mockAPI.RequestCalls()[0].C.(tbapi.RestrictChatMemberConfig).Permissions)
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

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger:   mockLogger,
		TbAPI:        mockAPI,
		Bot:          b,
		Group:        "gr",
		Locator:      locator,
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

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		Group:      "gr",
		Locator:    locator,
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
		RemoveApprovedUserFunc: func(id int64) error { return nil },
	}

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		Group:      "gr",
		AdminGroup: "123",
		StartupMsg: "startup",
		SuperUsers: SuperUsers{"umputun"},
		Locator:    locator,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	err := l.Locator.AddMessage("text 123", 123, 88, "user", 999999) // add message to locator
	assert.NoError(t, err)

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

	err = l.Do(ctx)
	assert.EqualError(t, err, "telegram update chan closed")
	assert.Equal(t, 0, len(mockLogger.SaveCalls()))

	require.Equal(t, 2, len(mockAPI.SendCalls()))
	assert.Equal(t, "startup", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
	assert.Contains(t, mockAPI.SendCalls()[1].C.(tbapi.MessageConfig).Text, "detection results")
	assert.Equal(t, int64(123), mockAPI.SendCalls()[1].C.(tbapi.MessageConfig).ChatID)

	require.Equal(t, 1, len(b.UpdateSpamCalls()))
	assert.Equal(t, "text 123", b.UpdateSpamCalls()[0].Msg)

	assert.Equal(t, 2, len(mockAPI.RequestCalls()))
	assert.Equal(t, int64(123), mockAPI.RequestCalls()[0].C.(tbapi.DeleteMessageConfig).ChatID)
	assert.Equal(t, 999999, mockAPI.RequestCalls()[0].C.(tbapi.DeleteMessageConfig).MessageID)
	assert.Equal(t, int64(123), mockAPI.RequestCalls()[1].C.(tbapi.BanChatMemberConfig).ChatID)
	assert.Equal(t, int64(88), mockAPI.RequestCalls()[1].C.(tbapi.BanChatMemberConfig).UserID)

	assert.Equal(t, 1, len(b.RemoveApprovedUserCalls()))
	assert.Equal(t, int64(88), b.RemoveApprovedUserCalls()[0].ID)
}

func TestTelegramListener_DoWithDirectSpamReport(t *testing.T) {
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
		RemoveApprovedUserFunc: func(id int64) error {
			return nil
		},
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

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		Group:      "gr",
		StartupMsg: "startup",
		SuperUsers: SuperUsers{"superuser1"}, // include a test superuser
		Locator:    locator,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		Message: &tbapi.Message{
			Chat: &tbapi.Chat{ID: 123},
			Text: "/SpAm",
			From: &tbapi.User{UserName: "superuser1", ID: 77},
			ReplyToMessage: &tbapi.Message{
				MessageID: 999999,
				From:      &tbapi.User{ID: 666, UserName: "user"},
				Text:      "text 123",
			},
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
	assert.Contains(t, mockAPI.SendCalls()[1].C.(tbapi.MessageConfig).Text, "detection results")
	assert.Contains(t, mockAPI.SendCalls()[1].C.(tbapi.MessageConfig).Text, `the user banned by "superuser1"`)

	require.Equal(t, 1, len(b.OnMessageCalls()))
	assert.Equal(t, "text 123", b.OnMessageCalls()[0].Msg.Text)

	require.Equal(t, 1, len(b.UpdateSpamCalls()))
	assert.Equal(t, "text 123", b.UpdateSpamCalls()[0].Msg)

	require.Equal(t, 1, len(b.RemoveApprovedUserCalls()))
	assert.Equal(t, int64(666), b.RemoveApprovedUserCalls()[0].ID)

	require.Equal(t, 3, len(mockAPI.RequestCalls()))
	assert.Equal(t, int64(123), mockAPI.RequestCalls()[0].C.(tbapi.DeleteMessageConfig).ChatID)
	assert.Equal(t, int(999999), mockAPI.RequestCalls()[0].C.(tbapi.DeleteMessageConfig).MessageID)
	assert.Equal(t, int64(123), mockAPI.RequestCalls()[1].C.(tbapi.DeleteMessageConfig).ChatID)
	assert.Equal(t, int(0), mockAPI.RequestCalls()[1].C.(tbapi.DeleteMessageConfig).MessageID)
	assert.Equal(t, int64(123), mockAPI.RequestCalls()[2].C.(tbapi.BanChatMemberConfig).ChatID)
	assert.Equal(t, int64(666), mockAPI.RequestCalls()[2].C.(tbapi.BanChatMemberConfig).UserID)
}

func TestTelegramListener_DoWithDirectWarnReport(t *testing.T) {
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
		RemoveApprovedUserFunc: func(id int64) error {
			return nil
		},
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

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		Group:      "gr",
		StartupMsg: "startup",
		SuperUsers: SuperUsers{"superuser1"}, // include a test superuser
		Locator:    locator,
		WarnMsg:    "You've violated our rules",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		Message: &tbapi.Message{
			Chat: &tbapi.Chat{ID: 123},
			Text: "/wArn",
			From: &tbapi.User{UserName: "superuser1", ID: 77},
			ReplyToMessage: &tbapi.Message{
				MessageID: 999999,
				From:      &tbapi.User{ID: 666, UserName: "user"},
				Text:      "text 123",
			},
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
	assert.Contains(t, mockAPI.SendCalls()[1].C.(tbapi.MessageConfig).Text, "warning from superuser1")
	assert.Contains(t, mockAPI.SendCalls()[1].C.(tbapi.MessageConfig).Text, `@user You've violated our rules`)

	require.Empty(t, b.OnMessageCalls())
	require.Empty(t, b.UpdateSpamCalls())
	require.Empty(t, b.RemoveApprovedUserCalls())

	require.Equal(t, 2, len(mockAPI.RequestCalls()))
	assert.Equal(t, int64(123), mockAPI.RequestCalls()[0].C.(tbapi.DeleteMessageConfig).ChatID)
	assert.Equal(t, int(999999), mockAPI.RequestCalls()[0].C.(tbapi.DeleteMessageConfig).MessageID)
	assert.Equal(t, int64(123), mockAPI.RequestCalls()[1].C.(tbapi.DeleteMessageConfig).ChatID)
	assert.Equal(t, int(0), mockAPI.RequestCalls()[1].C.(tbapi.DeleteMessageConfig).MessageID)
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
		AddApprovedUserFunc: func(id int64, name string) error { return nil },
	}

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		SuperUsers: SuperUsers{"admin"},
		Group:      "gr",
		Locator:    locator,
		AdminGroup: "123",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		CallbackQuery: &tbapi.CallbackQuery{
			Data: "777:999:88",
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
	require.Equal(t, 1, len(b.AddApprovedUserCalls()))
	assert.Equal(t, int64(777), b.AddApprovedUserCalls()[0].ID)
}

func TestTelegramListener_DoWithAdminSoftUnBan(t *testing.T) {
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
		AddApprovedUserFunc: func(id int64, name string) error { return nil },
	}

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger:  mockLogger,
		TbAPI:       mockAPI,
		Bot:         b,
		SuperUsers:  SuperUsers{"admin"},
		Group:       "gr",
		Locator:     locator,
		AdminGroup:  "123",
		SoftBanMode: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		CallbackQuery: &tbapi.CallbackQuery{
			Data: "777:999:88",
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

	assert.Equal(t, int64(777), mockAPI.RequestCalls()[1].C.(tbapi.RestrictChatMemberConfig).UserID)
	assert.Equal(t, &tbapi.ChatPermissions{CanSendMessages: true, CanSendMediaMessages: true, CanSendOtherMessages: true,
		CanSendPolls: true}, mockAPI.RequestCalls()[1].C.(tbapi.RestrictChatMemberConfig).Permissions)
	require.Equal(t, 1, len(b.UpdateHamCalls()))
	assert.Equal(t, "this was the ham, not spam", b.UpdateHamCalls()[0].Msg)
	require.Equal(t, 1, len(b.AddApprovedUserCalls()))
	assert.Equal(t, int64(777), b.AddApprovedUserCalls()[0].ID)
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
		AddApprovedUserFunc: func(id int64, name string) error { return nil },
	}

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger:   mockLogger,
		TbAPI:        mockAPI,
		Bot:          b,
		SuperUsers:   SuperUsers{"admin"},
		Group:        "gr",
		Locator:      locator,
		AdminGroup:   "123",
		TrainingMode: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		CallbackQuery: &tbapi.CallbackQuery{
			Data: "777:999:88",
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
	require.Equal(t, 1, len(b.AddApprovedUserCalls()))
	assert.Equal(t, int64(777), b.AddApprovedUserCalls()[0].ID)
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
		AddApprovedUserFunc: func(id int64, name string) error { return nil },
	}

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		SuperUsers: SuperUsers{"admin"},
		Group:      "gr",
		Locator:    locator,
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
	require.Equal(t, 0, len(b.AddApprovedUserCalls()))
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
		AddApprovedUserFunc: func(id int64, name string) error { return nil },
	}

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		SuperUsers: SuperUsers{"admin"},
		Group:      "gr",
		Locator:    locator,
		AdminGroup: "123",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		CallbackQuery: &tbapi.CallbackQuery{
			Data: "+999:987654:88", // + means unban declined
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
	require.Equal(t, 0, len(b.AddApprovedUserCalls()))
}

func TestTelegramListener_DoWithAdminBanConfirmedTraining(t *testing.T) {
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
		AddApprovedUserFunc: func(id int64, name string) error { return nil },
	}

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger:   mockLogger,
		TbAPI:        mockAPI,
		Bot:          b,
		SuperUsers:   SuperUsers{"admin"},
		Group:        "gr",
		Locator:      locator,
		AdminGroup:   "123",
		TrainingMode: true,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		CallbackQuery: &tbapi.CallbackQuery{
			Data: "+999:987654:88", // + means unban declined
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
	require.Equal(t, 2, len(mockAPI.SendCalls()))
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).Text, "unban user blah")
	kb := mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).ReplyMarkup.InlineKeyboard
	assert.Equal(t, 0, len(kb), "buttons cleared")
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).Text, "confirmed by admin in ")
	require.Equal(t, 2, len(mockAPI.RequestCalls()))
	assert.Equal(t, int64(999), mockAPI.RequestCalls()[0].C.(tbapi.BanChatMemberConfig).UserID, "user banned")
	assert.Equal(t, int64(123), mockAPI.RequestCalls()[0].C.(tbapi.BanChatMemberConfig).ChatID, "chat id")
	assert.Equal(t, 987654, mockAPI.RequestCalls()[1].C.(tbapi.DeleteMessageConfig).MessageID, "message deleted")

	assert.Equal(t, 1, len(b.UpdateSpamCalls()))
	assert.Equal(t, 0, len(b.UpdateHamCalls()))
	require.Equal(t, 0, len(b.AddApprovedUserCalls()))
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

	locator, teardown := prepTestLocator(t)
	defer teardown()

	l := TelegramListener{
		SpamLogger: mockLogger,
		TbAPI:      mockAPI,
		Bot:        b,
		SuperUsers: SuperUsers{"admin"},
		Group:      "gr",
		Locator:    locator,
		AdminGroup: "123",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Minute)
	defer cancel()

	updMsg := tbapi.Update{
		CallbackQuery: &tbapi.CallbackQuery{
			Data: "!999:987654:88", // ! means we show info
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

	err := l.Locator.AddSpam(999, []spamcheck.Response{{Name: "rule1", Spam: true, Details: "details1"},
		{Name: "rule2", Spam: true, Details: "details2"}})
	assert.NoError(t, err)

	err = l.Do(ctx)
	assert.EqualError(t, err, "telegram update chan closed")
	require.Equal(t, 1, len(mockAPI.SendCalls()))
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).Text, "unban user blah")
	kb := mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).ReplyMarkup.InlineKeyboard
	assert.Equal(t, 0, len(kb), "buttons cleared")
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig).Text, "results**\n- rule1: spam, details1\n- rule2: spam, details2")
	assert.Equal(t, 0, len(mockAPI.RequestCalls()))
	assert.Equal(t, 0, len(b.UpdateSpamCalls()))
	assert.Equal(t, 0, len(b.UpdateHamCalls()))
	require.Equal(t, 0, len(b.AddApprovedUserCalls()))
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
			name:     "not allowed, fromUser is not superuser but fromChat is chatID",
			fromChat: 123,
			chatID:   123,
			fromUser: "user",
			expect:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			listener := TelegramListener{
				adminChatID: tc.chatID,
				SuperUsers:  SuperUsers{"umputun"},
			}
			result := listener.isAdminChat(tc.fromChat, tc.fromUser)
			assert.Equal(t, tc.expect, result)
		})
	}
}

func TestSuperUser_IsSuper(t *testing.T) {
	tests := []struct {
		name     string
		super    SuperUsers
		userName string
		want     bool
	}{
		{
			name:     "User is a super user",
			super:    SuperUsers{"Alice", "Bob"},
			userName: "Alice",
			want:     true,
		},
		{
			name:     "User is not a super user",
			super:    SuperUsers{"Alice", "Bob"},
			userName: "Charlie",
			want:     false,
		},
		{
			name:     "User is a super user with slash prefix",
			super:    SuperUsers{"/Alice", "Bob"},
			userName: "Alice",
			want:     true,
		},
		{
			name:     "User is not a super user with slash prefix",
			super:    SuperUsers{"/Alice", "Bob"},
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
		superUsers      SuperUsers
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
			superUsers:     SuperUsers{"super1"},
			chatAdmins:     []tbapi.ChatMember{{User: &tbapi.User{UserName: "admin1"}}, {User: &tbapi.User{UserName: "admin2"}}},
			expectedResult: []string{"super1", "admin1", "admin2"},
			expectedErr:    false,
		},
		{
			name:           "non-empty admin usernames, existing supers with duplicate",
			superUsers:     SuperUsers{"admin1"},
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

func prepTestLocator(t *testing.T) (loc *storage.Locator, teardown func()) {
	f, err := os.CreateTemp("", "locator")
	require.NoError(t, err)
	db, err := storage.NewSqliteDB(f.Name())
	require.NoError(t, err)

	loc, err = storage.NewLocator(10*time.Minute, 100, db)
	require.NoError(t, err)
	return loc, func() {
		_ = os.Remove(f.Name())
	}
}
