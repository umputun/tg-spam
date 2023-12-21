package events

import (
	"errors"
	"testing"

	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"

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
