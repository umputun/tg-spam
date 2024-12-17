package events

import (
	"errors"
	"testing"
	"time"

	tbapi "github.com/OvyFlash/telegram-bot-api"
	"github.com/stretchr/testify/assert"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/events/mocks"
)

func TestEvents_escapeMarkDownV1Text(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Test with all markdown symbols",
			input:    "_*`[",
			expected: "\\_\\*\\`\\[",
		},
		{
			name:     "Test with no markdown symbols",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "Test with mixed content",
			input:    "Hello_World*`[",
			expected: "Hello\\_World\\*\\`\\[",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeMarkDownV1Text(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestEvents_send(t *testing.T) {
	mockAPI := &mocks.TbAPIMock{
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			if mc, ok := c.(tbapi.MessageConfig); ok {
				if mc.Text == "badmd" && mc.ParseMode == "Markdown" {
					return tbapi.Message{}, errors.New("bad markdown")
				}
			}
			return tbapi.Message{}, nil
		},
	}

	t.Run("send with markdown passed", func(t *testing.T) {
		mockAPI.ResetCalls()
		err := send(tbapi.NewMessage(123, "test"), mockAPI)
		assert.NoError(t, err)
		assert.Equal(t, 1, len(mockAPI.SendCalls()))
		assert.Equal(t, int64(123), mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ChatID)
		assert.Equal(t, "test", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
		assert.Equal(t, "Markdown", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ParseMode)
	})

	t.Run("send with markdown failed", func(t *testing.T) {
		mockAPI.ResetCalls()
		err := send(tbapi.NewMessage(123, "badmd"), mockAPI)
		assert.NoError(t, err)

		assert.Equal(t, 2, len(mockAPI.SendCalls()))

		assert.Equal(t, int64(123), mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ChatID)
		assert.Equal(t, "badmd", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
		assert.Equal(t, "Markdown", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ParseMode)

		assert.Equal(t, int64(123), mockAPI.SendCalls()[1].C.(tbapi.MessageConfig).ChatID)
		assert.Equal(t, "badmd", mockAPI.SendCalls()[1].C.(tbapi.MessageConfig).Text)
		assert.Equal(t, "", mockAPI.SendCalls()[1].C.(tbapi.MessageConfig).ParseMode)
	})

}

func TestTelegramListener_transformTextMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    *tbapi.Message
		expected *bot.Message
	}{
		{
			name: "Basic text message",
			input: &tbapi.Message{
				Chat: tbapi.Chat{
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
			expected: &bot.Message{
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
		},
		{
			name: "Text message with nil values",
			input: &tbapi.Message{
				Chat:      tbapi.Chat{ID: 123456},
				MessageID: 31,
				Date:      1579627415,
				Text:      "",
			},
			expected: &bot.Message{
				ID:     31,
				Sent:   time.Unix(1579627415, 0),
				Text:   "",
				ChatID: 123456,
			},
		},
		{
			name: "Text message with sender chat",
			input: &tbapi.Message{
				Chat: tbapi.Chat{ID: 123456},
				SenderChat: &tbapi.Chat{
					ID:       654321,
					UserName: "channelname",
				},
				MessageID: 32,
				Date:      1579627416,
				Text:      "Channel Message",
			},
			expected: &bot.Message{
				ID:     32,
				Sent:   time.Unix(1579627416, 0),
				Text:   "Channel Message",
				ChatID: 123456,
				SenderChat: bot.SenderChat{
					ID:       654321,
					UserName: "channelname",
				},
			},
		},
		{
			name: "Message with forward",
			input: &tbapi.Message{
				Chat: tbapi.Chat{ID: 123456},
				From: &tbapi.User{
					ID:        100000001,
					UserName:  "username",
					FirstName: "First",
					LastName:  "Last",
				},
				MessageID:     30,
				Date:          1578627415,
				Text:          "Forwarded message",
				ForwardOrigin: &tbapi.MessageOrigin{Date: time.Unix(1578627415, 0).Unix()},
			},
			expected: &bot.Message{
				ID: 30,
				From: bot.User{
					ID:          100000001,
					Username:    "username",
					DisplayName: "First Last",
				},
				Sent:        time.Unix(1578627415, 0),
				Text:        "Forwarded message",
				ChatID:      123456,
				WithForward: true,
			},
		},
		{
			name: "Message with story",
			input: &tbapi.Message{
				Chat: tbapi.Chat{ID: 123456},
				From: &tbapi.User{
					ID:        100000001,
					UserName:  "username",
					FirstName: "First",
					LastName:  "Last",
				},
				MessageID: 30,
				Date:      1578627415,
				Text:      "Message with story",
				Story:     &tbapi.Story{},
			},
			expected: &bot.Message{
				ID: 30,
				From: bot.User{
					ID:          100000001,
					Username:    "username",
					DisplayName: "First Last",
				},
				Sent:      time.Unix(1578627415, 0),
				Text:      "Message with story",
				ChatID:    123456,
				WithVideo: true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, transform(tt.input))
		})
	}
}

func TestTelegramListener_transformPhoto(t *testing.T) {
	assert.Equal(t,
		&bot.Message{
			Text: "caption",
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
		transform(
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

func TestTelegramListener_transformEntities(t *testing.T) {
	tests := []struct {
		name     string
		input    *tbapi.Message
		expected *bot.Message
	}{
		{
			name: "Message with mentions and italics",
			input: &tbapi.Message{
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
			expected: &bot.Message{
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
		},
		{
			name: "Message with URL entity",
			input: &tbapi.Message{
				Date: 1578627416,
				Text: "Check this link",
				Entities: []tbapi.MessageEntity{
					{
						Type:   "url",
						Offset: 6,
						Length: 4,
						URL:    "https://example.com",
					},
				},
			},
			expected: &bot.Message{
				Sent: time.Unix(1578627416, 0),
				Text: "Check this link",
				Entities: &[]bot.Entity{
					{
						Type:   "url",
						Offset: 6,
						Length: 4,
						URL:    "https://example.com",
					},
				},
			},
		},
		{
			name: "Message with user entity",
			input: &tbapi.Message{
				Date: 1578627417,
				Text: "Message mentioning @user",
				Entities: []tbapi.MessageEntity{
					{
						Type:   "mention",
						Offset: 18,
						Length: 5,
						User: &tbapi.User{
							ID:        100000002,
							UserName:  "user",
							FirstName: "First",
							LastName:  "User",
						},
					},
				},
			},
			expected: &bot.Message{
				Sent: time.Unix(1578627417, 0),
				Text: "Message mentioning @user",
				Entities: &[]bot.Entity{
					{
						Type:   "mention",
						Offset: 18,
						Length: 5,
						User: &bot.User{
							ID:          100000002,
							Username:    "user",
							DisplayName: "First User",
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, transform(tt.input))
		})
	}
}
