package events

import (
	"testing"

	tbapi "github.com/OvyFlash/telegram-bot-api"
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

	t.Run("normal user name", func(t *testing.T) {
		mockAPI.ResetCalls()
		adm.ReportBan("testUser", msg)

		require.Equal(t, 1, len(mockAPI.SendCalls()))
		t.Logf("sent text: %+v", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
		assert.Equal(t, int64(123), mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ChatID)
		assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text, "permanently banned [testUser](tg://user?id=456)")
		assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text, "Test  \\_message\\_")
		assert.NotNil(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ReplyMarkup)
		assert.Equal(t, "⛔︎ change ban",
			mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ReplyMarkup.(tbapi.InlineKeyboardMarkup).InlineKeyboard[0][0].Text)
	})

	t.Run("name with md chars", func(t *testing.T) {
		mockAPI.ResetCalls()
		adm.ReportBan("test_User", msg)

		require.Equal(t, 1, len(mockAPI.SendCalls()))
		t.Logf("sent text: %+v", mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text)
		assert.Equal(t, int64(123), mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ChatID)
		assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text, "permanently banned [test\\_User](tg://user?id=456)")
		assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text, "Test  \\_message\\_")
		assert.NotNil(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ReplyMarkup)
		assert.Equal(t, "⛔︎ change ban",
			mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ReplyMarkup.(tbapi.InlineKeyboardMarkup).InlineKeyboard[0][0].Text)
	})
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
		{
			name:     "spam detection results immediately after header",
			input:    "Line 1\n\nspam detection results:\nLine 3",
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

func TestAdmin_getCleanMessage2(t *testing.T) {
	msg := `permanently banned {157419590 new_Nikita Νικήτας}

и да, этим надо заниматься каждый день по несколько часов. За месяц увидишь ощутимый результат

**spam detection results**
- stopword: ham, not found
- emoji: ham, 0/2
- similarity: ham, 0.15/0.50
- classifier: spam, probability of spam: 71.70%
- cas: ham, record not found

_unbanned by umputun in 1m5s_`

	a := &admin{}
	result, err := a.getCleanMessage(msg)
	assert.NoError(t, err)
	assert.Equal(t, "и да, этим надо заниматься каждый день по несколько часов. За месяц увидишь ощутимый результат", result)
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

func TestAdmin_extractUsername(t *testing.T) {
	tests := []struct {
		name           string
		banMessage     string
		expectedResult string
		expectError    bool
	}{
		{
			name:           "Markdown Format",
			banMessage:     `**permanently banned [John_Doe](tg://user?id=123456)** some message text`,
			expectedResult: "John_Doe",
			expectError:    false,
		},
		{
			name:           "Plain Format",
			banMessage:     `permanently banned {200312168 umputun Umputun U} some message text`,
			expectedResult: "umputun",
			expectError:    false,
		},
		{
			name:        "Invalid Format",
			banMessage:  `permanently banned John_Doe some message text`,
			expectError: true,
		},
	}

	a := admin{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			username, err := a.extractUsername(test.banMessage)
			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, test.expectedResult, username)
			}
		})
	}
}

func TestAdmin_dryModeForwardMessage(t *testing.T) {
	mockAPI := &mocks.TbAPIMock{
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			return tbapi.Message{}, nil
		},
	}
	adm := admin{
		tbAPI: mockAPI,
		dry:   true,
	}
	msg := &bot.Message{}

	adm.ReportBan("testUser", msg)
	assert.Contains(t, mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text, "would have permanently banned [testUser]")

}
