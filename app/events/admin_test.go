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

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/events/mocks"
	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/lib/spamcheck"
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

		require.Len(t, mockAPI.SendCalls(), 1)
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

		require.Len(t, mockAPI.SendCalls(), 1)
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
				require.Error(t, err)
			} else {
				require.NoError(t, err)
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
	require.NoError(t, err)
	assert.Equal(t, "и да, этим надо заниматься каждый день по несколько часов. За месяц увидишь ощутимый результат", result)
}

func TestAdmin_extractUsername(t *testing.T) {
	tests := []struct {
		name           string
		banMessage     string
		expectedResult string
		expectError    bool
	}{
		{name: "markdown format", banMessage: "**permanently banned [John_Doe](tg://user?id=123456)** some text", expectedResult: "John_Doe"},
		{name: "plain format", banMessage: "permanently banned {200312168 umputun Umputun U} some text", expectedResult: "umputun"},
		{name: "t.me channel link", banMessage: "**permanently banned [spamchannel](https://t.me/spamchannel)**\n\nspam text", expectedResult: "spamchannel"},
		{name: "plain channel with ID", banMessage: "**permanently banned mychannel (-100999888)**\n\nspam text", expectedResult: "mychannel"},
		{name: "plain channel multi-word title", banMessage: "**permanently banned Spam News Channel (-100999888)**\n\ntext", expectedResult: "Spam News Channel"},
		{name: "invalid format", banMessage: "permanently banned John_Doe some message text", expectError: true},
	}

	a := admin{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			username, err := a.extractUsername(test.banMessage)
			if test.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
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

func TestAdmin_reportBanChannel(t *testing.T) {
	mockAPI := &mocks.TbAPIMock{
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			return tbapi.Message{}, nil
		},
	}

	adm := admin{tbAPI: mockAPI, adminChatID: 123}

	t.Run("channel message uses SenderChat.ID in callback data", func(t *testing.T) {
		mockAPI.ResetCalls()
		msg := &bot.Message{
			From:       bot.User{ID: 136817688, Username: "Channel_Bot"},
			SenderChat: bot.SenderChat{ID: -100999888, UserName: "spamchannel"},
			Text:       "spam from channel",
		}
		adm.ReportBan("spamchannel", msg)

		require.Len(t, mockAPI.SendCalls(), 1)
		sentText := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text
		markup := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ReplyMarkup.(tbapi.InlineKeyboardMarkup)
		// callback data should contain channel ID (-100999888), not Channel_Bot (136817688)
		require.NotNil(t, markup.InlineKeyboard[0][0].CallbackData)
		assert.Contains(t, *markup.InlineKeyboard[0][0].CallbackData, "-100999888:")
		assert.NotContains(t, *markup.InlineKeyboard[0][0].CallbackData, "136817688")
		// ban message should use t.me link, not tg://user
		assert.Contains(t, sentText, "https://t.me/spamchannel")
		assert.NotContains(t, sentText, "tg://user?id=136817688")
	})

	t.Run("channel message without username uses plain text with ID", func(t *testing.T) {
		mockAPI.ResetCalls()
		msg := &bot.Message{
			From:       bot.User{ID: 136817688, Username: "Channel_Bot"},
			SenderChat: bot.SenderChat{ID: -100999888},
			Text:       "spam from channel",
		}
		adm.ReportBan("Some Channel", msg)

		require.Len(t, mockAPI.SendCalls(), 1)
		sentText := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text
		assert.Contains(t, sentText, "permanently banned Some Channel (-100999888)")
		assert.NotContains(t, sentText, "tg://user")
	})

	t.Run("regular user message uses From.ID in callback data", func(t *testing.T) {
		mockAPI.ResetCalls()
		msg := &bot.Message{
			From: bot.User{ID: 456, Username: "spammer"},
			Text: "spam from user",
		}
		adm.ReportBan("spammer", msg)

		require.Len(t, mockAPI.SendCalls(), 1)
		sentText := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).Text
		markup := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig).ReplyMarkup.(tbapi.InlineKeyboardMarkup)
		require.NotNil(t, markup.InlineKeyboard[0][0].CallbackData)
		assert.Contains(t, *markup.InlineKeyboard[0][0].CallbackData, "456:")
		// regular user should use tg://user link
		assert.Contains(t, sentText, "tg://user?id=456")
	})
}

func TestAdmin_DirectCommands(t *testing.T) {
	// setup common mock objects and test data
	setupTest := func() (*mocks.TbAPIMock, *mocks.BotMock, *admin, func()) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				// return success API response with Ok set to true
				return &tbapi.APIResponse{Ok: true}, nil
			},
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				// handle different types of Chattable
				switch v := c.(type) {
				case tbapi.MessageConfig:
					return tbapi.Message{Text: v.Text}, nil
				case tbapi.EditMessageTextConfig:
					return tbapi.Message{Text: v.Text}, nil
				default:
					return tbapi.Message{}, nil
				}
			},
		}

		botMock := &mocks.BotMock{
			RemoveApprovedUserFunc: func(id int64) error {
				return nil
			},
			OnMessageFunc: func(msg bot.Message, checkOnly bool) bot.Response {
				return bot.Response{
					CheckResults: []spamcheck.Response{
						{Name: "test", Spam: true, Details: "test details"},
					},
				}
			},
			UpdateSpamFunc: func(msg string) error {
				return nil
			},
		}

		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{}, true
			},
			SpamFunc: func(ctx context.Context, userID int64) (storage.SpamData, bool) {
				return storage.SpamData{}, true
			},
			UserNameByIDFunc: func(ctx context.Context, userID int64) string {
				return "testuser"
			},
		}

		adm := &admin{
			tbAPI:       mockAPI,
			bot:         botMock,
			primChatID:  123,
			adminChatID: 456,
			locator:     locatorMock,
			superUsers:  SuperUsers{"superuser"},
			warnMsg:     "please follow our rules",
		}

		teardown := func() {} // no teardown needed when using mocks
		return mockAPI, botMock, adm, teardown
	}

	// verify common responses from a direct report command
	verifyDirectReportResults := func(t *testing.T, mockAPI *mocks.TbAPIMock, botMock *mocks.BotMock) {
		// check that the API was called to delete messages and ban user (3 requests)
		require.GreaterOrEqual(t, len(mockAPI.RequestCalls()), 3)
		// first delete should be the original message
		assert.Equal(t, 999, mockAPI.RequestCalls()[0].C.(tbapi.DeleteMessageConfig).MessageID)
		// second delete should be the admin's command message
		assert.Equal(t, 789, mockAPI.RequestCalls()[1].C.(tbapi.DeleteMessageConfig).MessageID)

		// check that the admin was notified
		require.Len(t, mockAPI.SendCalls(), 1)
		adminMsg := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig)
		assert.Equal(t, int64(456), adminMsg.ChatID) // should be sent to admin chat
		assert.Contains(t, adminMsg.Text, "original detection results for spammer (222)")
		assert.Contains(t, adminMsg.Text, "the user banned by")

		// check that appropriate bot methods were called
		require.Len(t, botMock.RemoveApprovedUserCalls(), 1)
		assert.Equal(t, int64(222), botMock.RemoveApprovedUserCalls()[0].ID)

		// check OnMessage was called to get spam detection results
		require.Len(t, botMock.OnMessageCalls(), 1)
		assert.Equal(t, "spam message text", botMock.OnMessageCalls()[0].Msg.Text)
		assert.Equal(t, int64(222), botMock.OnMessageCalls()[0].Msg.From.ID)
		assert.True(t, botMock.OnMessageCalls()[0].CheckOnly)
	}

	// helper function to create a test update
	createReplyUpdate := func(adminName string, adminID int64, spammerName string, spammerID int64, text string) tbapi.Update {
		return tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				From:      &tbapi.User{UserName: adminName, ID: adminID},
				ReplyToMessage: &tbapi.Message{
					MessageID: 999,
					From:      &tbapi.User{UserName: spammerName, ID: spammerID},
					Text:      text,
				},
			},
		}
	}

	t.Run("DirectBanReport", func(t *testing.T) {
		mockAPI, botMock, adm, teardown := setupTest()
		defer teardown()

		update := createReplyUpdate("admin", 111, "spammer", 222, "spam message text")

		// test the DirectBanReport function
		err := adm.DirectBanReport(update)
		require.NoError(t, err)

		verifyDirectReportResults(t, mockAPI, botMock)

		// UpdateSpam should NOT be called for ban (only for spam)
		assert.Empty(t, botMock.UpdateSpamCalls())
	})

	t.Run("DirectSpamReport", func(t *testing.T) {
		mockAPI, botMock, adm, teardown := setupTest()
		defer teardown()

		update := createReplyUpdate("admin", 111, "spammer", 222, "spam message text")

		// test the DirectSpamReport function
		err := adm.DirectSpamReport(update)
		require.NoError(t, err)

		verifyDirectReportResults(t, mockAPI, botMock)

		// UpdateSpam should be called for DirectSpamReport
		require.Len(t, botMock.UpdateSpamCalls(), 1)
		assert.Equal(t, "spam message text", botMock.UpdateSpamCalls()[0].Msg)
	})

	t.Run("DirectReport_DryMode", func(t *testing.T) {
		mockAPI, botMock, adm, teardown := setupTest()
		defer teardown()
		adm.dry = true // enable dry mode

		update := createReplyUpdate("admin", 111, "spammer", 222, "spam message text")

		// test the DirectSpamReport function in dry mode
		err := adm.DirectSpamReport(update)
		require.NoError(t, err)

		// check that admin was notified
		require.Len(t, mockAPI.SendCalls(), 1)
		adminMsg := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig)
		assert.Equal(t, int64(456), adminMsg.ChatID)
		assert.Contains(t, adminMsg.Text, "original detection results for spammer (222)")

		// in dry mode, no message deletions or bans should occur
		assert.Empty(t, mockAPI.RequestCalls())
		assert.Empty(t, botMock.UpdateSpamCalls())
	})

	t.Run("DirectWarnReport", func(t *testing.T) {
		mockAPI, _, adm, teardown := setupTest()
		defer teardown()

		// create update with non-superuser to not trigger superuser check
		update := createReplyUpdate("admin", 111, "user", 222, "inappropriate message")

		// test the DirectWarnReport function
		err := adm.DirectWarnReport(update)
		require.NoError(t, err)

		// check that the API was called to delete messages
		require.Len(t, mockAPI.RequestCalls(), 2)
		// first delete should be the original message
		assert.Equal(t, 999, mockAPI.RequestCalls()[0].C.(tbapi.DeleteMessageConfig).MessageID)
		// second delete should be the admin's command message
		assert.Equal(t, 789, mockAPI.RequestCalls()[1].C.(tbapi.DeleteMessageConfig).MessageID)

		// check that warning was sent to the main chat
		require.Len(t, mockAPI.SendCalls(), 1)
		warnMsg := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig)
		assert.Equal(t, int64(123), warnMsg.ChatID) // should be sent to the primary chat
		assert.Contains(t, warnMsg.Text, "warning from admin")
		assert.Contains(t, warnMsg.Text, "@user please follow our rules")
	})

	t.Run("DirectSpamReport_ChannelMessage", func(t *testing.T) {
		mockAPI, botMock, adm, teardown := setupTest()
		defer teardown()

		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				From:      &tbapi.User{UserName: "admin", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID:  999,
					From:       &tbapi.User{UserName: "Channel_Bot", ID: 136817688},
					SenderChat: &tbapi.Chat{ID: 12345, UserName: "spam_channel"},
					Text:       "spam message text",
				},
			},
		}

		err := adm.DirectSpamReport(update)
		require.NoError(t, err)

		// verify ban used BanChatSenderChatConfig with channel ID, not BanChatMemberConfig
		var foundChannelBan bool
		for _, call := range mockAPI.RequestCalls() {
			if banCfg, ok := call.C.(tbapi.BanChatSenderChatConfig); ok {
				foundChannelBan = true
				assert.Equal(t, int64(12345), banCfg.SenderChatID)
				assert.Equal(t, int64(123), banCfg.ChatID)
			}
		}
		assert.True(t, foundChannelBan, "expected BanChatSenderChatConfig for channel message")

		// verify BanChatMemberConfig was NOT used (should ban channel, not user)
		for _, call := range mockAPI.RequestCalls() {
			_, isMemberBan := call.C.(tbapi.BanChatMemberConfig)
			assert.False(t, isMemberBan, "should not use BanChatMemberConfig for channel message")
		}

		require.Len(t, botMock.UpdateSpamCalls(), 1)

		// verify admin notification contains actual channel name and ID, not Channel_Bot
		require.Len(t, mockAPI.SendCalls(), 1)
		adminMsg := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig)
		assert.Contains(t, adminMsg.Text, "spam\\_channel") // escaped markdown
		assert.Contains(t, adminMsg.Text, "12345")
		assert.NotContains(t, adminMsg.Text, "Channel\\_Bot")
		assert.NotContains(t, adminMsg.Text, "136817688")
	})

	t.Run("DirectSpamReport_AnonymousAdmin", func(t *testing.T) {
		mockAPI, botMock, adm, teardown := setupTest()
		defer teardown()

		// anonymous admin post where SenderChat.ID equals the group chat ID (123)
		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				From:      &tbapi.User{UserName: "admin", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID:  999,
					From:       &tbapi.User{UserName: "GroupLinkedChannel", ID: 136817688},
					SenderChat: &tbapi.Chat{ID: 123, UserName: "the_group"}, // same as group chat ID
					Text:       "admin message",
				},
			},
		}

		err := adm.DirectSpamReport(update)
		require.NoError(t, err)

		// should NOT use BanChatSenderChatConfig (would ban the group from itself)
		for _, call := range mockAPI.RequestCalls() {
			_, isChannelBan := call.C.(tbapi.BanChatSenderChatConfig)
			assert.False(t, isChannelBan, "should not use BanChatSenderChatConfig for anonymous admin post")
		}

		require.Len(t, botMock.UpdateSpamCalls(), 1)
	})

	t.Run("DirectWarnReport_ChannelMessage", func(t *testing.T) {
		mockAPI, _, adm, teardown := setupTest()
		defer teardown()

		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				From:      &tbapi.User{UserName: "admin", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID:  999,
					From:       &tbapi.User{UserName: "Channel_Bot", ID: 136817688},
					SenderChat: &tbapi.Chat{ID: -100999888, UserName: "spam_channel"},
					Text:       "inappropriate channel message",
				},
			},
		}

		err := adm.DirectWarnReport(update)
		require.NoError(t, err)

		require.Len(t, mockAPI.SendCalls(), 1)
		warnMsg := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig)
		assert.Equal(t, int64(123), warnMsg.ChatID)
		assert.Contains(t, warnMsg.Text, "@spam\\_channel") // escaped markdown
		assert.NotContains(t, warnMsg.Text, "@Channel\\_Bot")
		assert.Contains(t, warnMsg.Text, "please follow our rules")
	})

	t.Run("DirectWarnReport_ChannelMessage_TitleOnly", func(t *testing.T) {
		mockAPI, _, adm, teardown := setupTest()
		defer teardown()

		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				From:      &tbapi.User{UserName: "admin", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID:  999,
					From:       &tbapi.User{UserName: "Channel_Bot", ID: 136817688},
					SenderChat: &tbapi.Chat{ID: -100999888, Title: "Spam Channel"},
					Text:       "inappropriate channel message",
				},
			},
		}

		err := adm.DirectWarnReport(update)
		require.NoError(t, err)

		require.Len(t, mockAPI.SendCalls(), 1)
		warnMsg := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig)
		assert.Equal(t, int64(123), warnMsg.ChatID)
		// title-only channel should not have @ prefix
		assert.Contains(t, warnMsg.Text, "Spam Channel")
		assert.NotContains(t, warnMsg.Text, "@Spam Channel")
		assert.NotContains(t, warnMsg.Text, "@Channel")
		assert.Contains(t, warnMsg.Text, "please follow our rules")
	})

	t.Run("DirectSpamReport_ChannelMessage_TitleOnly", func(t *testing.T) {
		mockAPI, botMock, adm, teardown := setupTest()
		defer teardown()

		update := tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				From:      &tbapi.User{UserName: "admin", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID:  999,
					From:       &tbapi.User{UserName: "Channel_Bot", ID: 136817688},
					SenderChat: &tbapi.Chat{ID: 12345, Title: "Spam Channel"},
					Text:       "spam message text",
				},
			},
		}

		err := adm.DirectSpamReport(update)
		require.NoError(t, err)

		require.Len(t, mockAPI.SendCalls(), 1)
		adminMsg := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig)
		assert.Contains(t, adminMsg.Text, "Spam Channel")
		assert.Contains(t, adminMsg.Text, "12345")
		assert.NotContains(t, adminMsg.Text, "Channel\\_Bot")

		require.Len(t, botMock.UpdateSpamCalls(), 1)
	})

	t.Run("DirectWarnReport_SuperUser", func(t *testing.T) {
		mockAPI, _, adm, teardown := setupTest()
		defer teardown()

		// create update with superuser to trigger superuser check
		update := createReplyUpdate("admin", 111, "superuser", 222, "inappropriate message")

		// test the DirectWarnReport function with superuser
		err := adm.DirectWarnReport(update)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "warn message is from super-user")

		// check that no API calls were made
		assert.Empty(t, mockAPI.RequestCalls())
		assert.Empty(t, mockAPI.SendCalls())
	})
}

func TestAdmin_InlineCallbacks(t *testing.T) {
	setupCallback := func(trainingMode bool, softBan bool) (*mocks.TbAPIMock, *mocks.BotMock, *admin, *tbapi.CallbackQuery) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				// handle different types of Chattable
				switch v := c.(type) {
				case tbapi.MessageConfig:
					return tbapi.Message{Text: v.Text}, nil
				case tbapi.EditMessageTextConfig:
					return tbapi.Message{Text: v.Text}, nil
				default:
					return tbapi.Message{}, nil
				}
			},
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
		}

		botMock := &mocks.BotMock{
			UpdateSpamFunc: func(msg string) error {
				return nil
			},
		}

		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{}, true
			},
			SpamFunc: func(ctx context.Context, userID int64) (storage.SpamData, bool) {
				return storage.SpamData{}, true
			},
			UserNameByIDFunc: func(ctx context.Context, userID int64) string {
				return "testuser"
			},
		}

		adm := &admin{
			tbAPI:        mockAPI,
			bot:          botMock,
			primChatID:   123,
			adminChatID:  456,
			locator:      locatorMock,
			trainingMode: trainingMode,
			softBan:      softBan,
		}

		// create a test query for the callback
		query := &tbapi.CallbackQuery{
			ID:   "test-callback-id",
			Data: "+12345:999", // ban confirmation with userID and msgID
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 456}, // admin chat
				Text:      "**permanently banned [user_name_with_underscore](tg://user?id=12345)**\n\nSpam message text",
				From:      &tbapi.User{UserName: "bot"},
			},
			From: &tbapi.User{
				UserName: "admin",
				ID:       111,
			},
		}

		return mockAPI, botMock, adm, query
	}

	t.Run("callbackBanConfirmed", func(t *testing.T) {
		mockAPI, botMock, adm, query := setupCallback(false, false)

		// test the callback handler
		err := adm.callbackBanConfirmed(query)
		require.NoError(t, err)

		// check that edit message was called with updated text
		require.Len(t, mockAPI.SendCalls(), 1)
		editMsg := mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig)
		assert.Equal(t, query.Message.Chat.ID, editMsg.ChatID)
		assert.Equal(t, query.Message.MessageID, editMsg.MessageID)
		assert.Contains(t, editMsg.Text, "ban confirmed by admin")
		assert.Empty(t, editMsg.ReplyMarkup.InlineKeyboard)

		// UpdateSpam should be called to update spam samples
		require.Len(t, botMock.UpdateSpamCalls(), 1)
		assert.Equal(t, "Spam message text", botMock.UpdateSpamCalls()[0].Msg)
	})

	t.Run("callbackBanConfirmed_TrainingMode", func(t *testing.T) {
		mockAPI, _, adm, query := setupCallback(true, false)

		// test the callback handler in training mode
		err := adm.callbackBanConfirmed(query)
		require.NoError(t, err)

		// in training mode, deleteAndBan should be called
		// verify the API requests (2 calls - 1 for message edit, 1 for deletion)
		require.GreaterOrEqual(t, len(mockAPI.RequestCalls()), 1)
		// the last call should be DeleteMessageConfig
		lastCall := mockAPI.RequestCalls()[len(mockAPI.RequestCalls())-1]
		assert.Equal(t, 999, lastCall.C.(tbapi.DeleteMessageConfig).MessageID)
		assert.Equal(t, int64(123), lastCall.C.(tbapi.DeleteMessageConfig).ChatID)
	})

	t.Run("callbackBanConfirmed_SoftBan", func(t *testing.T) {
		mockAPI, botMock, adm, query := setupCallback(false, true)

		// test the callback handler in soft ban mode
		err := adm.callbackBanConfirmed(query)
		require.NoError(t, err)

		// in soft ban mode, a real ban should be performed
		// verify the API requests
		require.GreaterOrEqual(t, len(mockAPI.RequestCalls()), 1)

		// find the BanChatMemberConfig call
		var foundBanCall bool
		for _, call := range mockAPI.RequestCalls() {
			banCall, ok := call.C.(tbapi.BanChatMemberConfig)
			if !ok {
				continue
			}
			foundBanCall = true
			assert.Equal(t, int64(12345), banCall.UserID)
			assert.Equal(t, int64(123), banCall.ChatID)
			break
		}

		assert.True(t, foundBanCall, "Expected a BanChatMemberConfig call")

		// UpdateSpam should be called to update spam samples
		require.Len(t, botMock.UpdateSpamCalls(), 1)
		assert.Equal(t, "Spam message text", botMock.UpdateSpamCalls()[0].Msg)
	})

	t.Run("callbackUnbanConfirmed_channel", func(t *testing.T) {
		mockAPI, _, adm, _ := setupCallback(false, false)
		botMock := &mocks.BotMock{
			UpdateHamFunc:       func(msg string) error { return nil },
			AddApprovedUserFunc: func(id int64, name string) error { return nil },
		}
		adm.bot = botMock

		// callback data with negative channel ID (channel unban), using t.me link format
		query := &tbapi.CallbackQuery{
			ID:   "test-callback-id",
			Data: "-100999888:999",
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 456},
				Text:      "**permanently banned [spamchannel](https://t.me/spamchannel)**\n\nSpam from channel",
				From:      &tbapi.User{UserName: "bot"},
			},
			From: &tbapi.User{UserName: "admin", ID: 111},
		}

		err := adm.callbackUnbanConfirmed(query)
		require.NoError(t, err)

		// verify UnbanChatSenderChatConfig was used (not UnbanChatMemberConfig)
		var foundChannelUnban bool
		for _, call := range mockAPI.RequestCalls() {
			if unbanCall, ok := call.C.(tbapi.UnbanChatSenderChatConfig); ok {
				foundChannelUnban = true
				assert.Equal(t, int64(-100999888), unbanCall.SenderChatID)
				assert.Equal(t, int64(123), unbanCall.ChatID)
				break
			}
		}
		assert.True(t, foundChannelUnban, "expected UnbanChatSenderChatConfig for channel ID")

		// verify approved user was added with channel ID and extracted name
		require.Len(t, botMock.AddApprovedUserCalls(), 1)
		assert.Equal(t, int64(-100999888), botMock.AddApprovedUserCalls()[0].ID)
		assert.Equal(t, "spamchannel", botMock.AddApprovedUserCalls()[0].Name)
	})

	t.Run("callbackUnbanConfirmed_channel_plain_title", func(t *testing.T) {
		mockAPI, _, adm, _ := setupCallback(false, false)
		botMock := &mocks.BotMock{
			UpdateHamFunc:       func(msg string) error { return nil },
			AddApprovedUserFunc: func(id int64, name string) error { return nil },
		}
		adm.bot = botMock

		// plain-title channel ban message (no username, multi-word title)
		query := &tbapi.CallbackQuery{
			ID:   "test-callback-id",
			Data: "-100999888:999",
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 456},
				Text:      "**permanently banned Spam News Channel (-100999888)**\n\nSpam from channel",
				From:      &tbapi.User{UserName: "bot"},
			},
			From: &tbapi.User{UserName: "admin", ID: 111},
		}

		err := adm.callbackUnbanConfirmed(query)
		require.NoError(t, err)

		// verify UnbanChatSenderChatConfig was used
		var foundChannelUnban bool
		for _, call := range mockAPI.RequestCalls() {
			if unbanCall, ok := call.C.(tbapi.UnbanChatSenderChatConfig); ok {
				foundChannelUnban = true
				assert.Equal(t, int64(-100999888), unbanCall.SenderChatID)
				break
			}
		}
		assert.True(t, foundChannelUnban, "expected UnbanChatSenderChatConfig for channel ID")

		// verify approved user was added with correct multi-word channel name
		require.Len(t, botMock.AddApprovedUserCalls(), 1)
		assert.Equal(t, int64(-100999888), botMock.AddApprovedUserCalls()[0].ID)
		assert.Equal(t, "Spam News Channel", botMock.AddApprovedUserCalls()[0].Name)
	})

	t.Run("callbackBanConfirmed_SoftBan_channel", func(t *testing.T) {
		mockAPI, botMock, adm, _ := setupCallback(false, true)

		// callback data with negative channel ID
		query := &tbapi.CallbackQuery{
			ID:   "test-callback-id",
			Data: "+-100999888:999",
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 456},
				Text:      "**permanently banned [spamchannel](https://t.me/spamchannel)**\n\nSpam from channel",
				From:      &tbapi.User{UserName: "bot"},
			},
			From: &tbapi.User{UserName: "admin", ID: 111},
		}

		err := adm.callbackBanConfirmed(query)
		require.NoError(t, err)

		// in soft ban mode with channel, should use BanChatSenderChatConfig
		var foundChannelBan bool
		for _, call := range mockAPI.RequestCalls() {
			if banCall, ok := call.C.(tbapi.BanChatSenderChatConfig); ok {
				foundChannelBan = true
				assert.Equal(t, int64(-100999888), banCall.SenderChatID)
				assert.Equal(t, int64(123), banCall.ChatID)
				break
			}
		}
		assert.True(t, foundChannelBan, "expected BanChatSenderChatConfig for channel ID")

		_ = botMock // silence unused
	})
}

func TestAdmin_CallbackShowInfo_PreservesUserLinks(t *testing.T) {
	// test that clicking the info button preserves user links in admin chat,
	// including edge cases with usernames containing markdown special characters

	var markdownErrorCount, htmlSuccessCount int

	// create a mockAPI that simulates real Telegram API behavior with markdown
	mockAPI := &mocks.TbAPIMock{
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			switch msg := c.(type) {
			case tbapi.EditMessageTextConfig:
				switch msg.ParseMode {
				case tbapi.ModeMarkdown:
					// with the fix, underscore should be escaped
					if strings.Contains(msg.Text, "user\\_name\\_with\\_underscore") {
						// markdown should now succeed because special chars are escaped
						return tbapi.Message{Text: msg.Text}, nil
					}
				case tbapi.ModeHTML:
					// HTML mode should succeed where markdown failed
					if strings.Contains(msg.Text, "user_name_with_underscore") {
						htmlSuccessCount++
						// the link URL should be preserved in HTML mode
						assert.Contains(t, msg.Text, "tg://user?id=12345", "Link URL should be preserved in HTML mode")
						return tbapi.Message{Text: msg.Text}, nil
					}
				case "":
					// when messages are sent as plain text after markdown failure
					// the link URL should still be preserved
					assert.Contains(t, msg.Text, "tg://user?id=12345", "Link URL should be preserved")
				}
			}
			return tbapi.Message{}, nil
		},
	}

	// set up the admin object
	adm := &admin{
		tbAPI:       mockAPI,
		adminChatID: 456,
	}

	// test with username containing underscore to trigger the issue
	t.Run("Preserve links for usernames with underscores", func(t *testing.T) {
		mockAPI.ResetCalls()
		markdownErrorCount = 0
		htmlSuccessCount = 0

		// setup query with a username that has underscores (already rendered by Telegram)
		query := &tbapi.CallbackQuery{
			ID:   "test-callback-id",
			Data: "!12345:999",
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 456},
				Text:      "permanently banned user_name_with_underscore\n\nSpam message text",
				From:      &tbapi.User{UserName: "bot"},
			},
			From: &tbapi.User{
				UserName: "admin",
				ID:       111,
			},
		}

		// mock functions needed for the test
		adm.locator = &mocks.LocatorMock{
			SpamFunc: func(ctx context.Context, userID int64) (storage.SpamData, bool) {
				return storage.SpamData{
					Checks: []spamcheck.Response{
						{Name: "test", Spam: true, Details: "test details"},
					},
				}, true
			},
		}

		// run the function that needs to maintain the links
		err := adm.callbackShowInfo(query)
		require.NoError(t, err)

		// with the fix, markdown should succeed on first attempt because underscore is escaped
		assert.Len(t, mockAPI.SendCalls(), 1, "Should succeed on first attempt with markdown")
		assert.Equal(t, 0, markdownErrorCount, "Should not fail with markdown")
		assert.Equal(t, 0, htmlSuccessCount, "Should not need HTML fallback")
	})

	t.Run("info button preserves link for normal username", func(t *testing.T) {
		sendAttempts := 0
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				sendAttempts++
				if editMsg, ok := c.(tbapi.EditMessageTextConfig); ok {
					assert.Contains(t, editMsg.Text, "permanently banned")
					assert.Contains(t, editMsg.Text, "spam detection results")
					// for normal username without special chars, markdown should work fine
					assert.Equal(t, tbapi.ModeMarkdown, editMsg.ParseMode)
				}
				return tbapi.Message{}, nil
			},
		}

		adm := admin{
			tbAPI:       mockAPI,
			adminChatID: 123,
		}

		query := &tbapi.CallbackQuery{
			ID:   "test",
			Data: "!6236647121:123",
			Message: &tbapi.Message{
				MessageID: 123,
				Chat:      tbapi.Chat{ID: 456},
				Text:      "permanently banned Евгения\n\nНужны 2-3 человека совершеннолетних, занятость из дома ! От 8К в день Пиши в лс",
				From:      &tbapi.User{UserName: "bot"},
			},
			From: &tbapi.User{UserName: "admin", ID: 111},
		}

		adm.locator = &mocks.LocatorMock{
			SpamFunc: func(ctx context.Context, userID int64) (storage.SpamData, bool) {
				return storage.SpamData{
					Checks: []spamcheck.Response{
						{Name: "stopword", Spam: true, Details: "в лс"},
					},
				}, true
			},
		}

		err := adm.callbackShowInfo(query)
		require.NoError(t, err)
		assert.Equal(t, 1, sendAttempts, "Should succeed on first attempt with markdown")
	})

	t.Run("info button preserves link for username with underscore after fix", func(t *testing.T) {
		sendAttempts := 0
		var usedParseMode string
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				sendAttempts++
				if editMsg, ok := c.(tbapi.EditMessageTextConfig); ok {
					usedParseMode = editMsg.ParseMode
					// with the fix, the underscore should be escaped as surkova\_vlada
					assert.Contains(t, editMsg.Text, "surkova\\_vlada", "Underscore should be escaped")
					assert.Equal(t, tbapi.ModeMarkdown, editMsg.ParseMode)
					// markdown should now succeed because special chars are escaped
					return tbapi.Message{}, nil
				}
				return tbapi.Message{}, nil
			},
		}

		adm := admin{
			tbAPI:       mockAPI,
			adminChatID: 123,
		}

		query := &tbapi.CallbackQuery{
			ID:   "test",
			Data: "!5519827604:123",
			Message: &tbapi.Message{
				MessageID: 123,
				Chat:      tbapi.Chat{ID: 456},
				Text:      "permanently banned surkova_vlada Влада Суркова\n\nЕсть способ заработка от 35 000 р в неделю.",
				From:      &tbapi.User{UserName: "bot"},
			},
			From: &tbapi.User{UserName: "admin", ID: 111},
		}

		adm.locator = &mocks.LocatorMock{
			SpamFunc: func(ctx context.Context, userID int64) (storage.SpamData, bool) {
				return storage.SpamData{
					Checks: []spamcheck.Response{
						{Name: "stopword", Spam: true, Details: "заработка"},
					},
				}, true
			},
		}

		err := adm.callbackShowInfo(query)
		require.NoError(t, err)
		// with the fix, markdown should succeed on first attempt
		assert.Equal(t, 1, sendAttempts, "Should succeed on first attempt with markdown")
		assert.Equal(t, tbapi.ModeMarkdown, usedParseMode, "Should use markdown mode successfully")
	})
}

func TestAdmin_MsgHandlerWithEmptyText(t *testing.T) {
	tests := []struct {
		name string
		msg  *tbapi.Message
	}{
		{
			name: "audio message without text",
			msg: &tbapi.Message{
				Chat: tbapi.Chat{ID: 123},
				From: &tbapi.User{UserName: "admin", ID: 77},
				ForwardOrigin: &tbapi.MessageOrigin{
					Type: "user",
					SenderUser: &tbapi.User{
						ID:       123,
						UserName: "user",
					},
				},
				Audio: &tbapi.Audio{
					FileID: "123",
				},
				MessageID: 999999,
			},
		},
		{
			name: "video message without text",
			msg: &tbapi.Message{
				Chat: tbapi.Chat{ID: 123},
				From: &tbapi.User{UserName: "admin", ID: 77},
				ForwardOrigin: &tbapi.MessageOrigin{
					Type: "user",
					SenderUser: &tbapi.User{
						ID:       123,
						UserName: "user",
					},
				},
				Video: &tbapi.Video{
					FileID: "123",
				},
				MessageID: 999999,
			},
		},
		{
			name: "photo message without text",
			msg: &tbapi.Message{
				Chat: tbapi.Chat{ID: 123},
				From: &tbapi.User{UserName: "admin", ID: 77},
				ForwardOrigin: &tbapi.MessageOrigin{
					Type: "user",
					SenderUser: &tbapi.User{
						ID:       123,
						UserName: "user",
					},
				},
				Photo: []tbapi.PhotoSize{{
					FileID: "123",
				}},
				MessageID: 999999,
			},
		},
	}

	mockAPI := &mocks.TbAPIMock{
		RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
			return &tbapi.APIResponse{Ok: true}, nil
		},
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			// handle different types of Chattable
			switch v := c.(type) {
			case tbapi.MessageConfig:
				return tbapi.Message{Text: v.Text}, nil
			case tbapi.EditMessageTextConfig:
				return tbapi.Message{Text: v.Text}, nil
			default:
				return tbapi.Message{}, nil
			}
		},
	}

	botMock := &mocks.BotMock{
		UpdateSpamFunc: func(msg string) error {
			t.Logf("update-spam: %s", msg)
			return nil
		},
		RemoveApprovedUserFunc: func(id int64) error {
			return nil
		},
	}

	locatorMock := &mocks.LocatorMock{
		AddMessageFunc: func(ctx context.Context, msg string, chatID, userID int64, userName string, msgID int) error {
			return nil
		},
	}

	adminHandler := admin{
		tbAPI:      mockAPI,
		bot:        botMock,
		locator:    locatorMock,
		primChatID: 123,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// reset mocks for each test
			mockAPI.ResetCalls()
			botMock.ResetCalls()

			update := tbapi.Update{Message: tt.msg}
			err := adminHandler.MsgHandler(update)
			require.Error(t, err)
			assert.Equal(t, "empty message text", err.Error())

			// verify no requests were made to ban or delete
			assert.Empty(t, mockAPI.RequestCalls())
			assert.Empty(t, botMock.UpdateSpamCalls())
		})
	}
}

func TestAdmin_MsgHandler(t *testing.T) {
	t.Run("non-forwarded message", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{Text: "test"}, nil
			},
		}

		botMock := &mocks.BotMock{}
		locatorMock := &mocks.LocatorMock{}

		adminHandler := admin{
			tbAPI:       mockAPI,
			bot:         botMock,
			locator:     locatorMock,
			primChatID:  123,
			adminChatID: 456,
		}

		// create a non-forwarded message in admin chat
		msg := &tbapi.Message{
			MessageID: 789,
			Chat:      tbapi.Chat{ID: 456}, // admin chat
			From:      &tbapi.User{UserName: "admin", ID: 123},
			Text:      "regular message",
		}

		update := tbapi.Update{Message: msg}
		err := adminHandler.MsgHandler(update)
		require.NoError(t, err)

		// verify no actions were taken
		assert.Empty(t, mockAPI.RequestCalls())
		assert.Empty(t, mockAPI.SendCalls())
		assert.Empty(t, botMock.UpdateSpamCalls())
	})

	t.Run("forwarded message from super-user", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{Text: "test"}, nil
			},
		}

		botMock := &mocks.BotMock{
			RemoveApprovedUserFunc: func(id int64) error {
				return nil
			},
			OnMessageFunc: func(msg bot.Message, checkOnly bool) bot.Response {
				return bot.Response{
					CheckResults: []spamcheck.Response{
						{Name: "test", Spam: true, Details: "test details"},
					},
				}
			},
		}

		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{
					UserID:   888,
					UserName: "superuser",
					MsgID:    999,
				}, true
			},
		}

		adminHandler := admin{
			tbAPI:       mockAPI,
			bot:         botMock,
			locator:     locatorMock,
			primChatID:  123,
			adminChatID: 456,
			superUsers:  SuperUsers{"superuser"},
		}

		// create a forwarded message in admin chat
		msg := &tbapi.Message{
			MessageID: 789,
			Chat:      tbapi.Chat{ID: 456}, // admin chat
			From:      &tbapi.User{UserName: "admin", ID: 123},
			Text:      "spam message",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type: "user",
				SenderUser: &tbapi.User{
					ID:       555,
					UserName: "user",
				},
			},
		}

		update := tbapi.Update{Message: msg}
		err := adminHandler.MsgHandler(update)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "forwarded message is about super-user")

		// check that API calls were not made (according to admin.go line 106-107)
		// intentionally not checking RemoveApprovedUser since it's called before the super-user check
	})

	t.Run("successful forwarded message processing", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{Text: "test"}, nil
			},
		}

		botMock := &mocks.BotMock{
			RemoveApprovedUserFunc: func(id int64) error {
				return nil
			},
			OnMessageFunc: func(msg bot.Message, checkOnly bool) bot.Response {
				return bot.Response{
					CheckResults: []spamcheck.Response{
						{Name: "test", Spam: true, Details: "test details"},
					},
				}
			},
			UpdateSpamFunc: func(msg string) error {
				return nil
			},
		}

		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{
					UserID:   888,
					UserName: "regularuser",
					MsgID:    999,
				}, true
			},
			SpamFunc: func(ctx context.Context, userID int64) (storage.SpamData, bool) {
				return storage.SpamData{}, true
			},
		}

		adminHandler := admin{
			tbAPI:       mockAPI,
			bot:         botMock,
			locator:     locatorMock,
			primChatID:  123,
			adminChatID: 456,
			superUsers:  SuperUsers{"superuser"},
		}

		// create a forwarded message in admin chat
		msg := &tbapi.Message{
			MessageID: 789,
			Chat:      tbapi.Chat{ID: 456}, // admin chat
			From:      &tbapi.User{UserName: "admin", ID: 123},
			Text:      "spam message text",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type: "user",
				SenderUser: &tbapi.User{
					ID:       555,
					UserName: "user",
				},
			},
		}

		update := tbapi.Update{Message: msg}
		err := adminHandler.MsgHandler(update)
		require.NoError(t, err)

		// verify correct sequence of operations
		assert.Len(t, mockAPI.SendCalls(), 1, "Should send detection results to admin")
		// the code makes at least 2 Request calls - one to delete message and one to ban user
		assert.GreaterOrEqual(t, len(mockAPI.RequestCalls()), 2, "Should request to delete the message and ban user")
		assert.Len(t, botMock.UpdateSpamCalls(), 1, "Should update spam samples")
		assert.Len(t, botMock.RemoveApprovedUserCalls(), 1, "Should remove user from approved list")
		assert.Len(t, botMock.OnMessageCalls(), 1, "Should check message for spam")

		// verify ban request called
		assert.Equal(t, int64(888), botMock.RemoveApprovedUserCalls()[0].ID, "Should remove correct user ID")
		assert.Equal(t, "spam message text", botMock.UpdateSpamCalls()[0].Msg, "Should update with correct text")
	})

	t.Run("channel message uses BanChatSenderChatConfig", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{Text: "test"}, nil
			},
		}

		botMock := &mocks.BotMock{
			RemoveApprovedUserFunc: func(id int64) error { return nil },
			OnMessageFunc: func(msg bot.Message, checkOnly bool) bot.Response {
				return bot.Response{CheckResults: []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}}}
			},
			UpdateSpamFunc: func(msg string) error { return nil },
		}

		// locator returns a negative channel ID (stored by procEvents for channel messages)
		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{UserID: -1001261918100, UserName: "spam_channel", MsgID: 999}, true
			},
			SpamFunc: func(ctx context.Context, userID int64) (storage.SpamData, bool) {
				return storage.SpamData{}, true
			},
		}

		adm := admin{
			tbAPI: mockAPI, bot: botMock, locator: locatorMock,
			primChatID: 123, adminChatID: 456, superUsers: SuperUsers{"superuser"},
		}

		msg := &tbapi.Message{
			MessageID: 789, Chat: tbapi.Chat{ID: 456},
			From: &tbapi.User{UserName: "admin", ID: 111},
			Text: "spam from channel",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type:       "channel",
				SenderChat: &tbapi.Chat{ID: -1001261918100, UserName: "spam_channel"},
			},
		}

		err := adm.MsgHandler(tbapi.Update{Message: msg})
		require.NoError(t, err)

		// verify BanChatSenderChatConfig was used with the channel ID
		var foundChannelBan bool
		for _, call := range mockAPI.RequestCalls() {
			if banCfg, ok := call.C.(tbapi.BanChatSenderChatConfig); ok {
				foundChannelBan = true
				assert.Equal(t, int64(-1001261918100), banCfg.SenderChatID)
				assert.Equal(t, int64(123), banCfg.ChatID)
			}
		}
		assert.True(t, foundChannelBan, "expected BanChatSenderChatConfig for channel message")

		// verify BanChatMemberConfig was NOT used
		for _, call := range mockAPI.RequestCalls() {
			_, isMemberBan := call.C.(tbapi.BanChatMemberConfig)
			assert.False(t, isMemberBan, "should not use BanChatMemberConfig for channel message")
		}
	})

	t.Run("anonymous admin post skips ban in MsgHandler", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{Text: "test"}, nil
			},
		}

		botMock := &mocks.BotMock{
			RemoveApprovedUserFunc: func(id int64) error { return nil },
			OnMessageFunc: func(msg bot.Message, checkOnly bool) bot.Response {
				return bot.Response{CheckResults: []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}}}
			},
			UpdateSpamFunc: func(msg string) error { return nil },
		}

		// locator returns user ID matching the group chat ID (anonymous admin post)
		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{UserID: 123, UserName: "the_group", MsgID: 999}, true
			},
			SpamFunc: func(ctx context.Context, userID int64) (storage.SpamData, bool) {
				return storage.SpamData{}, true
			},
		}

		adm := admin{
			tbAPI: mockAPI, bot: botMock, locator: locatorMock,
			primChatID: 123, adminChatID: 456, superUsers: SuperUsers{"superuser"},
		}

		msg := &tbapi.Message{
			MessageID: 789, Chat: tbapi.Chat{ID: 456},
			From: &tbapi.User{UserName: "admin", ID: 111},
			Text: "admin message forwarded",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type:           "hidden_user",
				SenderUserName: "the_group",
			},
		}

		err := adm.MsgHandler(tbapi.Update{Message: msg})
		require.NoError(t, err)

		// verify the handler reached the locator (not exited early)
		require.Len(t, locatorMock.MessageCalls(), 1)

		// should NOT attempt any ban (no BanChatSenderChatConfig or BanChatMemberConfig)
		for _, call := range mockAPI.RequestCalls() {
			_, isChannelBan := call.C.(tbapi.BanChatSenderChatConfig)
			assert.False(t, isChannelBan, "should not ban channel when user ID matches group chat")
			_, isMemberBan := call.C.(tbapi.BanChatMemberConfig)
			assert.False(t, isMemberBan, "should not ban member when user ID matches group chat")
		}
	})

	t.Run("message not found in locator with hidden user", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{}
		botMock := &mocks.BotMock{}
		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{}, false
			},
		}

		adminHandler := admin{
			tbAPI:       mockAPI,
			bot:         botMock,
			locator:     locatorMock,
			primChatID:  123,
			adminChatID: 456,
		}

		// forwarded message with hidden user (fwdID=0), should return error
		msg := &tbapi.Message{
			MessageID: 789,
			Chat:      tbapi.Chat{ID: 456},
			From:      &tbapi.User{UserName: "admin", ID: 123},
			Text:      "spam message text",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type:           "hidden_user",
				SenderUserName: "hidden",
			},
		}

		update := tbapi.Update{Message: msg}
		err := adminHandler.MsgHandler(update)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("dry mode", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{Text: "test"}, nil
			},
		}

		botMock := &mocks.BotMock{
			RemoveApprovedUserFunc: func(id int64) error {
				return nil
			},
			OnMessageFunc: func(msg bot.Message, checkOnly bool) bot.Response {
				return bot.Response{
					CheckResults: []spamcheck.Response{
						{Name: "test", Spam: true, Details: "test details"},
					},
				}
			},
		}

		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{
					UserID:   888,
					UserName: "regularuser",
					MsgID:    999,
				}, true
			},
			SpamFunc: func(ctx context.Context, userID int64) (storage.SpamData, bool) {
				return storage.SpamData{}, true
			},
		}

		adminHandler := admin{
			tbAPI:       mockAPI,
			bot:         botMock,
			locator:     locatorMock,
			primChatID:  123,
			adminChatID: 456,
			superUsers:  SuperUsers{"superuser"},
			dry:         true, // enable dry mode
		}

		// create a forwarded message in admin chat
		msg := &tbapi.Message{
			MessageID: 789,
			Chat:      tbapi.Chat{ID: 456}, // admin chat
			From:      &tbapi.User{UserName: "admin", ID: 123},
			Text:      "spam message text",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type: "user",
				SenderUser: &tbapi.User{
					ID:       555,
					UserName: "user",
				},
			},
		}

		update := tbapi.Update{Message: msg}
		err := adminHandler.MsgHandler(update)
		require.NoError(t, err)

		// in dry mode, we should only notify admin but not delete or ban
		assert.Len(t, mockAPI.SendCalls(), 1, "Should send detection results to admin")
		assert.Empty(t, mockAPI.RequestCalls(), "Should not make request calls in dry mode")
		assert.Empty(t, botMock.UpdateSpamCalls(), "Should not update spam in dry mode")
	})

	t.Run("error removing approved user", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{Text: "test"}, nil
			},
		}

		botMock := &mocks.BotMock{
			RemoveApprovedUserFunc: func(id int64) error {
				return fmt.Errorf("failed to remove user")
			},
			OnMessageFunc: func(msg bot.Message, checkOnly bool) bot.Response {
				return bot.Response{
					CheckResults: []spamcheck.Response{
						{Name: "test", Spam: true, Details: "test details"},
					},
				}
			},
			UpdateSpamFunc: func(msg string) error {
				return nil
			},
		}

		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{
					UserID:   888,
					UserName: "regularuser",
					MsgID:    999,
				}, true
			},
			SpamFunc: func(ctx context.Context, userID int64) (storage.SpamData, bool) {
				return storage.SpamData{}, true
			},
		}

		adminHandler := admin{
			tbAPI:       mockAPI,
			bot:         botMock,
			locator:     locatorMock,
			primChatID:  123,
			adminChatID: 456,
			superUsers:  SuperUsers{"superuser"},
		}

		// create a forwarded message in admin chat
		msg := &tbapi.Message{
			MessageID: 789,
			Chat:      tbapi.Chat{ID: 456}, // admin chat
			From:      &tbapi.User{UserName: "admin", ID: 123},
			Text:      "spam message text",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type: "user",
				SenderUser: &tbapi.User{
					ID:       555,
					UserName: "user",
				},
			},
		}

		update := tbapi.Update{Message: msg}
		err := adminHandler.MsgHandler(update)

		// in the actual code, the error from RemoveApprovedUser is collected in a multierror
		// so the final error should include that failure
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to remove user")

		// other operations should still proceed
		assert.Len(t, mockAPI.SendCalls(), 1, "Should send detection results to admin")
		// the code makes at least 2 Request calls - one to delete message and one to ban user
		assert.GreaterOrEqual(t, len(mockAPI.RequestCalls()), 2, "Should request to delete the message and ban user")
		assert.Len(t, botMock.UpdateSpamCalls(), 1, "Should update spam samples")
	})
}

func TestAdmin_MsgHandlerFallback(t *testing.T) {
	t.Run("locator fails, ForwardOrigin has user ID", func(t *testing.T) {
		var sentMessages []string
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				if msg, ok := c.(tbapi.MessageConfig); ok {
					sentMessages = append(sentMessages, msg.Text)
				}
				return tbapi.Message{Text: "test"}, nil
			},
		}

		botMock := &mocks.BotMock{
			RemoveApprovedUserFunc: func(id int64) error { return nil },
			OnMessageFunc: func(msg bot.Message, checkOnly bool) bot.Response {
				return bot.Response{CheckResults: []spamcheck.Response{
					{Name: "test", Spam: true, Details: "test details"},
				}}
			},
			UpdateSpamFunc: func(msg string) error { return nil },
		}

		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{}, false // locator fails
			},
		}

		adminHandler := admin{
			tbAPI: mockAPI, bot: botMock, locator: locatorMock,
			primChatID: 123, adminChatID: 456, superUsers: SuperUsers{"superuser"},
		}

		msg := &tbapi.Message{
			MessageID: 789, Chat: tbapi.Chat{ID: 456},
			From: &tbapi.User{UserName: "admin", ID: 123}, Text: "spam message text",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type:       "user",
				SenderUser: &tbapi.User{ID: 555, UserName: "spammer"},
			},
		}

		err := adminHandler.MsgHandler(tbapi.Update{Message: msg})
		require.NoError(t, err)

		// verify ban, spam update, remove approved all called
		assert.Len(t, botMock.RemoveApprovedUserCalls(), 1)
		assert.Equal(t, int64(555), botMock.RemoveApprovedUserCalls()[0].ID)
		assert.Len(t, botMock.UpdateSpamCalls(), 1)
		assert.Equal(t, "spam message text", botMock.UpdateSpamCalls()[0].Msg)
		assert.Len(t, botMock.OnMessageCalls(), 1)
		assert.True(t, botMock.OnMessageCalls()[0].CheckOnly)

		// verify ban request was made
		assert.GreaterOrEqual(t, len(mockAPI.RequestCalls()), 1, "should make ban request")

		// verify detection results and warning messages sent
		require.Len(t, sentMessages, 2, "should send detection results and fallback warning")
		assert.Contains(t, sentMessages[0], `original detection results for "spammer" (555)`)
		assert.Contains(t, sentMessages[1], "locator fallback")
		assert.Contains(t, sentMessages[1], "manual deletion")
		assert.Contains(t, sentMessages[1], "spammer")
	})

	t.Run("locator fails, hidden user (fwdID=0)", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{}
		botMock := &mocks.BotMock{}
		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{}, false
			},
		}

		adminHandler := admin{
			tbAPI: mockAPI, bot: botMock, locator: locatorMock,
			primChatID: 123, adminChatID: 456,
		}

		msg := &tbapi.Message{
			MessageID: 789, Chat: tbapi.Chat{ID: 456},
			From: &tbapi.User{UserName: "admin", ID: 123}, Text: "spam message text",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type:           "hidden_user",
				SenderUserName: "hidden_name",
			},
		}

		err := adminHandler.MsgHandler(tbapi.Update{Message: msg})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")

		// verify no actions were taken
		assert.Empty(t, mockAPI.RequestCalls())
		assert.Empty(t, botMock.UpdateSpamCalls())
	})

	t.Run("locator fails, channel forward (fwdID=0)", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{}
		botMock := &mocks.BotMock{}
		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{}, false
			},
		}

		adminHandler := admin{
			tbAPI: mockAPI, bot: botMock, locator: locatorMock,
			primChatID: 123, adminChatID: 456,
		}

		// channel forward - ForwardOrigin exists but no SenderUser, so getForwardUsernameAndID returns 0
		msg := &tbapi.Message{
			MessageID: 789, Chat: tbapi.Chat{ID: 456},
			From: &tbapi.User{UserName: "admin", ID: 123}, Text: "spam message text",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type: "channel",
			},
		}

		err := adminHandler.MsgHandler(tbapi.Update{Message: msg})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("locator fails, ForwardOrigin has user ID, dry mode", func(t *testing.T) {
		var sentMessages []string
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				if msg, ok := c.(tbapi.MessageConfig); ok {
					sentMessages = append(sentMessages, msg.Text)
				}
				return tbapi.Message{Text: "test"}, nil
			},
		}

		botMock := &mocks.BotMock{
			RemoveApprovedUserFunc: func(id int64) error { return nil },
			OnMessageFunc: func(msg bot.Message, checkOnly bool) bot.Response {
				return bot.Response{CheckResults: []spamcheck.Response{
					{Name: "test", Spam: true, Details: "test details"},
				}}
			},
		}

		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{}, false
			},
		}

		adminHandler := admin{
			tbAPI: mockAPI, bot: botMock, locator: locatorMock,
			primChatID: 123, adminChatID: 456, dry: true,
		}

		msg := &tbapi.Message{
			MessageID: 789, Chat: tbapi.Chat{ID: 456},
			From: &tbapi.User{UserName: "admin", ID: 123}, Text: "spam message text",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type:       "user",
				SenderUser: &tbapi.User{ID: 555, UserName: "spammer"},
			},
		}

		err := adminHandler.MsgHandler(tbapi.Update{Message: msg})
		require.NoError(t, err)

		// in dry mode: no ban, no spam update
		assert.Empty(t, mockAPI.RequestCalls(), "should not ban in dry mode")
		assert.Empty(t, botMock.UpdateSpamCalls(), "should not update spam in dry mode")

		// detection results and warning still sent
		require.Len(t, sentMessages, 2)
		assert.Contains(t, sentMessages[0], "original detection results")
		assert.Contains(t, sentMessages[1], "locator fallback")
		assert.Contains(t, sentMessages[1], "dry mode")
	})

	t.Run("locator fails, ForwardOrigin has user ID, training mode", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{Text: "test"}, nil
			},
		}

		botMock := &mocks.BotMock{
			RemoveApprovedUserFunc: func(id int64) error { return nil },
			OnMessageFunc: func(msg bot.Message, checkOnly bool) bot.Response {
				return bot.Response{CheckResults: []spamcheck.Response{
					{Name: "test", Spam: true, Details: "test details"},
				}}
			},
			UpdateSpamFunc: func(msg string) error { return nil },
		}

		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{}, false
			},
		}

		adminHandler := admin{
			tbAPI: mockAPI, bot: botMock, locator: locatorMock,
			primChatID: 123, adminChatID: 456, trainingMode: true,
		}

		msg := &tbapi.Message{
			MessageID: 789, Chat: tbapi.Chat{ID: 456},
			From: &tbapi.User{UserName: "admin", ID: 123}, Text: "spam message text",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type:       "user",
				SenderUser: &tbapi.User{ID: 555, UserName: "spammer"},
			},
		}

		err := adminHandler.MsgHandler(tbapi.Update{Message: msg})
		require.NoError(t, err)

		// training mode: UpdateSpam called, ban is a no-op (logged only, no tbAPI.Request)
		assert.Len(t, botMock.UpdateSpamCalls(), 1, "should update spam in training mode")
		assert.Len(t, botMock.RemoveApprovedUserCalls(), 1, "should remove approved user")
		assert.Len(t, botMock.OnMessageCalls(), 1, "should check message")
		assert.Empty(t, mockAPI.RequestCalls(), "training mode ban is a no-op, no request calls")
	})

	t.Run("locator fails, ForwardOrigin has user ID, super-user", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{}
		botMock := &mocks.BotMock{}
		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{}, false
			},
		}

		adminHandler := admin{
			tbAPI: mockAPI, bot: botMock, locator: locatorMock,
			primChatID: 123, adminChatID: 456, superUsers: SuperUsers{"superuser"},
		}

		msg := &tbapi.Message{
			MessageID: 789, Chat: tbapi.Chat{ID: 456},
			From: &tbapi.User{UserName: "admin", ID: 123}, Text: "spam message text",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type:       "user",
				SenderUser: &tbapi.User{ID: 555, UserName: "superuser"},
			},
		}

		err := adminHandler.MsgHandler(tbapi.Update{Message: msg})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "super-user")
		assert.Contains(t, err.Error(), "ignored")

		// verify no actions were taken
		assert.Empty(t, mockAPI.RequestCalls())
		assert.Empty(t, botMock.UpdateSpamCalls())
	})

	t.Run("locator fails, ForwardOrigin has user ID, super-user by ID only (no username)", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{}
		botMock := &mocks.BotMock{}
		locatorMock := &mocks.LocatorMock{
			MessageFunc: func(ctx context.Context, msg string) (storage.MsgMeta, bool) {
				return storage.MsgMeta{}, false
			},
		}

		adminHandler := admin{
			tbAPI: mockAPI, bot: botMock, locator: locatorMock,
			primChatID: 123, adminChatID: 456, superUsers: SuperUsers{"555"},
		}

		msg := &tbapi.Message{
			MessageID: 789, Chat: tbapi.Chat{ID: 456},
			From: &tbapi.User{UserName: "admin", ID: 123}, Text: "spam message text",
			ForwardOrigin: &tbapi.MessageOrigin{
				Type:       "user",
				SenderUser: &tbapi.User{ID: 555, UserName: ""},
			},
		}

		err := adminHandler.MsgHandler(tbapi.Update{Message: msg})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "super-user")
		assert.Contains(t, err.Error(), "ignored")

		// verify no actions were taken
		assert.Empty(t, mockAPI.RequestCalls())
		assert.Empty(t, botMock.UpdateSpamCalls())
	})
}

func TestAdmin_DirectSpamReport_ImageOnly(t *testing.T) {
	mockAPI := &mocks.TbAPIMock{
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			return tbapi.Message{}, nil
		},
		RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
			return &tbapi.APIResponse{Ok: true}, nil
		},
	}

	botMock := &mocks.BotMock{
		RemoveApprovedUserFunc: func(id int64) error {
			return nil
		},
		OnMessageFunc: func(msg bot.Message, checkOnly bool) bot.Response {
			return bot.Response{
				CheckResults: []spamcheck.Response{
					{Name: "image-spam", Spam: true, Details: "First message with image"},
				},
			}
		},
		UpdateSpamFunc: func(msg string) error {
			if msg == "" {
				return fmt.Errorf("can't update spam samples: message can't be empty")
			}
			return nil
		},
	}

	locatorMock := &mocks.LocatorMock{}

	adm := &admin{
		tbAPI:       mockAPI,
		bot:         botMock,
		primChatID:  123,
		adminChatID: 456,
		locator:     locatorMock,
		superUsers:  SuperUsers{},
	}

	update := tbapi.Update{
		Message: &tbapi.Message{
			MessageID: 789,
			Chat:      tbapi.Chat{ID: 123},
			Text:      "/spam",
			From:      &tbapi.User{UserName: "admin", ID: 111},
			ReplyToMessage: &tbapi.Message{
				MessageID: 999,
				From:      &tbapi.User{ID: 666, UserName: "spammer"},
				Text:      "",
				Photo:     []tbapi.PhotoSize{{FileID: "photo123"}},
			},
		},
	}

	err := adm.directReport(update, true)
	require.NoError(t, err, "Should handle image-only spam without error")

	assert.Len(t, botMock.RemoveApprovedUserCalls(), 1, "Should remove user from approved list")
	assert.Equal(t, int64(666), botMock.RemoveApprovedUserCalls()[0].ID)

	assert.Len(t, mockAPI.SendCalls(), 1, "Should send detection results to admin")

	assert.GreaterOrEqual(t, len(mockAPI.RequestCalls()), 2, "Should delete message and ban user")

	assert.Empty(t, botMock.UpdateSpamCalls(), "Should not update spam samples for empty messages")
}

func TestAdmin_DirectReportWithAggressiveCleanup(t *testing.T) {
	// setup helper function for aggressive cleanup tests
	setupAggressiveCleanupTest := func(aggressiveCleanup bool, dry bool, messageIDs []int) (*mocks.TbAPIMock, *mocks.LocatorMock, *admin) {
		mockAPI := &mocks.TbAPIMock{
			SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
				return tbapi.Message{}, nil
			},
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				return &tbapi.APIResponse{Ok: true}, nil
			},
		}

		botMock := &mocks.BotMock{
			OnMessageFunc: func(msg bot.Message, checkOnly bool) bot.Response {
				return bot.Response{
					Send:        true,
					BanInterval: bot.PermanentBanDuration,
					CheckResults: []spamcheck.Response{
						{Name: "test", Spam: true, Details: "spam detected"},
					},
				}
			},
			RemoveApprovedUserFunc: func(id int64) error {
				return nil
			},
			UpdateSpamFunc: func(msg string) error {
				return nil
			},
		}

		locatorMock := &mocks.LocatorMock{
			GetUserMessageIDsFunc: func(ctx context.Context, userID int64, limit int) ([]int, error) {
				return messageIDs, nil
			},
		}

		adm := &admin{
			tbAPI:                  mockAPI,
			bot:                    botMock,
			primChatID:             123,
			adminChatID:            456,
			locator:                locatorMock,
			superUsers:             SuperUsers{},
			aggressiveCleanup:      aggressiveCleanup,
			aggressiveCleanupLimit: 100,
			dry:                    dry,
		}

		return mockAPI, locatorMock, adm
	}

	// helper to create test update
	createSpamReportUpdate := func() tbapi.Update {
		return tbapi.Update{
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 123},
				Text:      "/spam",
				From:      &tbapi.User{UserName: "admin", ID: 111},
				ReplyToMessage: &tbapi.Message{
					MessageID: 999,
					From:      &tbapi.User{ID: 666, UserName: "spammer"},
					Text:      "spam message",
				},
			},
		}
	}

	t.Run("aggressive cleanup enabled", func(t *testing.T) {
		mockAPI, locatorMock, adm := setupAggressiveCleanupTest(true, false, []int{100, 101, 102})
		update := createSpamReportUpdate()

		err := adm.directReport(update, true)
		require.NoError(t, err)

		// wait for async cleanup goroutine to complete
		time.Sleep(200 * time.Millisecond)

		// verify GetUserMessageIDs was called
		assert.Len(t, locatorMock.GetUserMessageIDsCalls(), 1)
		assert.Equal(t, int64(666), locatorMock.GetUserMessageIDsCalls()[0].UserID)
		assert.Equal(t, 100, locatorMock.GetUserMessageIDsCalls()[0].Limit)

		// verify deletion messages were sent (2 for original+admin messages, 3 for aggressive cleanup)
		requestCalls := mockAPI.RequestCalls()
		deleteCount := 0
		for _, call := range requestCalls {
			if _, ok := call.C.(tbapi.DeleteMessageConfig); ok {
				deleteCount++
			}
		}
		assert.Equal(t, 5, deleteCount, "Should delete original + admin messages and 3 additional messages")

		// verify notification was sent with correct format
		sendCalls := mockAPI.SendCalls()
		foundNotification := false
		var notificationMsg string
		for _, call := range sendCalls {
			if msg, ok := call.C.(tbapi.MessageConfig); ok {
				if strings.Contains(msg.Text, "deleted 3 messages from spammer") {
					foundNotification = true
					notificationMsg = msg.Text
					break
				}
			}
		}
		assert.True(t, foundNotification, "Should send notification about deleted messages")
		assert.Contains(t, notificationMsg, "deleted 3 messages from spammer \"spammer\" (666)", "Notification should include username and ID")
	})

	t.Run("aggressive cleanup disabled", func(t *testing.T) {
		mockAPI, locatorMock, adm := setupAggressiveCleanupTest(false, false, []int{})
		update := createSpamReportUpdate()

		err := adm.directReport(update, true)
		require.NoError(t, err)

		// verify GetUserMessageIDs was NOT called
		assert.Empty(t, locatorMock.GetUserMessageIDsCalls())

		// verify only original and admin messages were deleted (no aggressive cleanup)
		requestCalls := mockAPI.RequestCalls()
		deleteCount := 0
		for _, call := range requestCalls {
			if _, ok := call.C.(tbapi.DeleteMessageConfig); ok {
				deleteCount++
			}
		}
		assert.Equal(t, 2, deleteCount, "Should only delete original and admin messages")
	})

	t.Run("aggressive cleanup in dry mode", func(t *testing.T) {
		_, locatorMock, adm := setupAggressiveCleanupTest(true, true, []int{})
		update := createSpamReportUpdate()

		err := adm.directReport(update, false)
		require.NoError(t, err)

		// verify GetUserMessageIDs was NOT called in dry mode
		assert.Empty(t, locatorMock.GetUserMessageIDsCalls())
	})
}

func TestAdmin_DeleteUserMessages(t *testing.T) {
	t.Run("successful deletion", func(t *testing.T) {
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				// simulate successful deletion
				return &tbapi.APIResponse{Ok: true}, nil
			},
		}

		locatorMock := &mocks.LocatorMock{
			GetUserMessageIDsFunc: func(ctx context.Context, userID int64, limit int) ([]int, error) {
				assert.Equal(t, int64(666), userID)
				assert.Equal(t, 100, limit)
				return []int{101, 102, 103}, nil
			},
		}

		adm := &admin{
			tbAPI:                  mockAPI,
			locator:                locatorMock,
			primChatID:             123456789,
			aggressiveCleanupLimit: 100,
		}

		deleted, err := adm.deleteUserMessages(666)
		require.NoError(t, err)
		assert.Equal(t, 3, deleted)

		// verify all messages were attempted to be deleted
		requestCalls := mockAPI.RequestCalls()
		assert.Len(t, requestCalls, 3)
		for i, call := range requestCalls {
			deleteConfig, ok := call.C.(tbapi.DeleteMessageConfig)
			require.True(t, ok)
			assert.Equal(t, 101+i, deleteConfig.MessageID)
			assert.Equal(t, int64(123456789), deleteConfig.ChatID)
		}
	})

	t.Run("locator error", func(t *testing.T) {
		locatorMock := &mocks.LocatorMock{
			GetUserMessageIDsFunc: func(ctx context.Context, userID int64, limit int) ([]int, error) {
				return nil, fmt.Errorf("database error")
			},
		}

		adm := &admin{
			locator:                locatorMock,
			aggressiveCleanupLimit: 100,
		}

		deleted, err := adm.deleteUserMessages(666)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get user messages")
		assert.Equal(t, 0, deleted)
	})

	t.Run("partial deletion success", func(t *testing.T) {
		callCount := 0
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				callCount++
				// fail on second message
				if callCount == 2 {
					return nil, fmt.Errorf("message already deleted")
				}
				return &tbapi.APIResponse{Ok: true}, nil
			},
		}

		locatorMock := &mocks.LocatorMock{
			GetUserMessageIDsFunc: func(ctx context.Context, userID int64, limit int) ([]int, error) {
				return []int{201, 202, 203}, nil
			},
		}

		adm := &admin{
			tbAPI:                  mockAPI,
			locator:                locatorMock,
			primChatID:             123456789,
			aggressiveCleanupLimit: 100,
		}

		deleted, err := adm.deleteUserMessages(666)
		require.NoError(t, err)
		assert.Equal(t, 2, deleted) // only 2 successful deletions

		// verify all 3 were attempted
		assert.Len(t, mockAPI.RequestCalls(), 3)
	})

	t.Run("too many consecutive failures", func(t *testing.T) {
		failCount := 0
		mockAPI := &mocks.TbAPIMock{
			RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
				failCount++
				return nil, fmt.Errorf("failed to delete")
			},
		}

		locatorMock := &mocks.LocatorMock{
			GetUserMessageIDsFunc: func(ctx context.Context, userID int64, limit int) ([]int, error) {
				return []int{301, 302, 303, 304, 305, 306, 307}, nil
			},
		}

		adm := &admin{
			tbAPI:                  mockAPI,
			locator:                locatorMock,
			primChatID:             123456789,
			aggressiveCleanupLimit: 100,
		}

		deleted, err := adm.deleteUserMessages(666)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "stopped after 5 consecutive failures")
		assert.Equal(t, 0, deleted)
		assert.Len(t, mockAPI.RequestCalls(), 5) // stopped after 5 failures
	})

	t.Run("empty message list", func(t *testing.T) {
		locatorMock := &mocks.LocatorMock{
			GetUserMessageIDsFunc: func(ctx context.Context, userID int64, limit int) ([]int, error) {
				return []int{}, nil
			},
		}

		adm := &admin{
			locator:                locatorMock,
			aggressiveCleanupLimit: 100,
		}

		deleted, err := adm.deleteUserMessages(666)
		require.NoError(t, err)
		assert.Equal(t, 0, deleted)
	})
}

func TestAdmin_channelDisplayName(t *testing.T) {
	adm := &admin{}

	tests := []struct {
		name     string
		chat     *tbapi.Chat
		expected string
	}{
		{name: "nil chat", chat: nil, expected: ""},
		{name: "username set", chat: &tbapi.Chat{ID: 123, UserName: "mychannel", Title: "My Channel"}, expected: "mychannel"},
		{name: "title only", chat: &tbapi.Chat{ID: 123, Title: "My Channel"}, expected: "My Channel"},
		{name: "neither username nor title", chat: &tbapi.Chat{ID: 123}, expected: "channel_123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, adm.channelDisplayName(tt.chat))
		})
	}
}
