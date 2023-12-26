package events

import (
	"testing"

	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/events/mocks"
)

func TestAdmin_reportBan(t *testing.T) {
	mockAPI := &mocks.TbAPIMock{
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			return tbapi.Message{}, nil
		},
	}

	adm := admin{
		tbAPI:       mockAPI,
		adminChatID: 123,
	}

	msg := &bot.Message{
		From: bot.User{
			ID: 456,
		},
		Text: "Test\n\n_message_",
	}

	adm.ReportBan("testUser", msg)

	require.Equal(t, 1, len(mockAPI.SendCalls()))
	t.Logf("sent text: %+v", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
	assert.Equal(t, int64(123), mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ChatID)
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text, "permanently banned [testUser](tg://user?id=456)")
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text, "Test  \\_message\\_")
	assert.NotNil(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ReplyMarkup)
	assert.Equal(t, "⛔︎ change ban",
		mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ReplyMarkup.(tbapi.InlineKeyboardMarkup).InlineKeyboard[0][0].Text)
}

func TestAdmin_getCleanMessage(t *testing.T) {
	a := &admin{}

	tests := []struct {
		name     string
		input    string
		expected string
		err      bool
	}{
		{
			name:     "with spam detection results",
			input:    "Line 1\n\nLine 2\nLine3\n\nspam detection results:\nLine 4",
			expected: "Line 2\nLine3",
			err:      false,
		},
		{
			name:     "without spam detection results",
			input:    "Line 1\n\nLine 2\nLine 3",
			expected: "Line 2\nLine 3",
			err:      false,
		},
		{
			name:     "without spam detection results, single line",
			input:    "Line 1\n\nLine 2",
			expected: "Line 2",
			err:      false,
		},
		{
			name:     "with spam detection results, single line",
			input:    "Line 1\n\nLine 2\n\nspam detection results:\nLine 4",
			expected: "Line 2",
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
			result, err := a.getCleanMessage(tt.input)
			if tt.err {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestAdmin_parseCallbackData(t *testing.T) {
	var tests = []struct {
		name       string
		data       string
		wantUserID int64
		wantMsgID  int
		wantErr    bool
	}{
		{"Valid data", "12345:678", 12345, 678, false},
		{"Data too short", "12", 0, 0, true},
		{"No colon separator", "12345678", 0, 0, true},
		{"Invalid userID", "abc:678", 0, 0, true},
		{"Invalid msgID", "12345:xyz", 0, 0, true},
		{"wrong prefix with valid data", "c12345:678", 0, 0, true},
		{"valid prefix+ with valid data", "+12345:678", 12345, 678, false},
		{"valid prefix! with valid data", "!12345:678", 12345, 678, false},
		{"valid prefix? with valid data", "?12345:678", 12345, 678, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := admin{}
			gotUserID, gotMsgID, err := a.parseCallbackData(tt.data)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantUserID, gotUserID)
				assert.Equal(t, tt.wantMsgID, gotMsgID)
			}
		})
	}
}
