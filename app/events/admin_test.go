package events

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
			banMessage:     "**permanently banned [John_Doe](tg://user?id=123456)** some message text",
			expectedResult: "John_Doe",
			expectError:    false,
		},
		{
			name:           "Plain Format",
			banMessage:     "permanently banned {200312168 umputun Umputun U} some message text",
			expectedResult: "umputun",
			expectError:    false,
		},
		{
			name:        "Invalid Format",
			banMessage:  "permanently banned John_Doe some message text",
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
		require.Equal(t, 1, len(mockAPI.SendCalls()))
		adminMsg := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig)
		assert.Equal(t, int64(456), adminMsg.ChatID) // should be sent to admin chat
		assert.Contains(t, adminMsg.Text, "original detection results for spammer (222)")
		assert.Contains(t, adminMsg.Text, "the user banned by")

		// check that appropriate bot methods were called
		require.Equal(t, 1, len(botMock.RemoveApprovedUserCalls()))
		assert.Equal(t, int64(222), botMock.RemoveApprovedUserCalls()[0].ID)

		// check OnMessage was called to get spam detection results
		require.Equal(t, 1, len(botMock.OnMessageCalls()))
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
		assert.Equal(t, 0, len(botMock.UpdateSpamCalls()))
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
		require.Equal(t, 1, len(botMock.UpdateSpamCalls()))
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
		require.Equal(t, 1, len(mockAPI.SendCalls()))
		adminMsg := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig)
		assert.Equal(t, int64(456), adminMsg.ChatID)
		assert.Contains(t, adminMsg.Text, "original detection results for spammer (222)")

		// in dry mode, no message deletions or bans should occur
		assert.Equal(t, 0, len(mockAPI.RequestCalls()))
		assert.Equal(t, 0, len(botMock.UpdateSpamCalls()))
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
		require.Equal(t, 2, len(mockAPI.RequestCalls()))
		// first delete should be the original message
		assert.Equal(t, 999, mockAPI.RequestCalls()[0].C.(tbapi.DeleteMessageConfig).MessageID)
		// second delete should be the admin's command message
		assert.Equal(t, 789, mockAPI.RequestCalls()[1].C.(tbapi.DeleteMessageConfig).MessageID)

		// check that warning was sent to the main chat
		require.Equal(t, 1, len(mockAPI.SendCalls()))
		warnMsg := mockAPI.SendCalls()[0].C.(tbapi.MessageConfig)
		assert.Equal(t, int64(123), warnMsg.ChatID) // should be sent to the primary chat
		assert.Contains(t, warnMsg.Text, "warning from admin")
		assert.Contains(t, warnMsg.Text, "@user please follow our rules")
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
		assert.Equal(t, 0, len(mockAPI.RequestCalls()))
		assert.Equal(t, 0, len(mockAPI.SendCalls()))
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
		require.Equal(t, 1, len(mockAPI.SendCalls()))
		editMsg := mockAPI.SendCalls()[0].C.(tbapi.EditMessageTextConfig)
		assert.Equal(t, query.Message.Chat.ID, editMsg.ChatID)
		assert.Equal(t, query.Message.MessageID, editMsg.MessageID)
		assert.Contains(t, editMsg.Text, "ban confirmed by admin")
		assert.Empty(t, editMsg.ReplyMarkup.InlineKeyboard)

		// UpdateSpam should be called to update spam samples
		require.Equal(t, 1, len(botMock.UpdateSpamCalls()))
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
			if _, ok := call.C.(tbapi.BanChatMemberConfig); ok {
				foundBanCall = true
				banCall := call.C.(tbapi.BanChatMemberConfig)
				assert.Equal(t, int64(12345), banCall.UserID)
				assert.Equal(t, int64(123), banCall.ChatID)
				break
			}
		}

		assert.True(t, foundBanCall, "Expected a BanChatMemberConfig call")

		// UpdateSpam should be called to update spam samples
		require.Equal(t, 1, len(botMock.UpdateSpamCalls()))
		assert.Equal(t, "Spam message text", botMock.UpdateSpamCalls()[0].Msg)
	})
}

func TestAdmin_PreserveUserLinks_Issue223(t *testing.T) {
	// this test specifically addresses issue #223:
	// pressing inline buttons removes telegram profile link from admin chat report
	// especially for usernames with special characters like underscores

	var markdownErrorCount, htmlSuccessCount int

	// create a mockAPI that simulates real Telegram API behavior with markdown
	mockAPI := &mocks.TbAPIMock{
		SendFunc: func(c tbapi.Chattable) (tbapi.Message, error) {
			switch msg := c.(type) {
			case tbapi.EditMessageTextConfig:
				if msg.ParseMode == tbapi.ModeMarkdown {
					// simulate Telegram API failing on usernames with underscores in markdown mode
					if strings.Contains(msg.Text, "user_name_with_underscore") {
						markdownErrorCount++
						return tbapi.Message{}, fmt.Errorf("Bad Request: can't parse entities: Character '_' is reserved")
					}
				} else if msg.ParseMode == tbapi.ModeHTML {
					// HTML mode should succeed where markdown failed
					if strings.Contains(msg.Text, "user_name_with_underscore") {
						htmlSuccessCount++
						// the link URL should be preserved in HTML mode
						assert.Contains(t, msg.Text, "tg://user?id=12345", "Link URL should be preserved in HTML mode")
						return tbapi.Message{Text: msg.Text}, nil
					}
				} else if msg.ParseMode == "" {
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

		// setup query with a username that has underscores
		query := &tbapi.CallbackQuery{
			ID:   "test-callback-id",
			Data: "!12345:999",
			Message: &tbapi.Message{
				MessageID: 789,
				Chat:      tbapi.Chat{ID: 456},
				Text:      "**permanently banned [user_name_with_underscore](tg://user?id=12345)**\n\nSpam message text",
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
		assert.NoError(t, err)

		// our improved implementation should:
		// 1. Try markdown first (which fails with usernames containing underscores)
		// 2. Try HTML as a fallback (which should succeed and preserve the links)
		assert.Equal(t, 1, markdownErrorCount, "Should have tried and failed with markdown")
		assert.Equal(t, 1, htmlSuccessCount, "Should have tried and succeeded with HTML mode")
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
			assert.Error(t, err)
			assert.Equal(t, "empty message text", err.Error())

			// verify no requests were made to ban or delete
			assert.Equal(t, 0, len(mockAPI.RequestCalls()))
			assert.Equal(t, 0, len(botMock.UpdateSpamCalls()))
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
		assert.NoError(t, err)

		// verify no actions were taken
		assert.Equal(t, 0, len(mockAPI.RequestCalls()))
		assert.Equal(t, 0, len(mockAPI.SendCalls()))
		assert.Equal(t, 0, len(botMock.UpdateSpamCalls()))
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
		assert.Error(t, err)
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
		assert.NoError(t, err)

		// verify correct sequence of operations
		assert.Equal(t, 1, len(mockAPI.SendCalls()), "Should send detection results to admin")
		// the code makes at least 2 Request calls - one to delete message and one to ban user
		assert.GreaterOrEqual(t, len(mockAPI.RequestCalls()), 2, "Should request to delete the message and ban user")
		assert.Equal(t, 1, len(botMock.UpdateSpamCalls()), "Should update spam samples")
		assert.Equal(t, 1, len(botMock.RemoveApprovedUserCalls()), "Should remove user from approved list")
		assert.Equal(t, 1, len(botMock.OnMessageCalls()), "Should check message for spam")

		// verify ban request called
		assert.Equal(t, int64(888), botMock.RemoveApprovedUserCalls()[0].ID, "Should remove correct user ID")
		assert.Equal(t, "spam message text", botMock.UpdateSpamCalls()[0].Msg, "Should update with correct text")
	})

	t.Run("message not found in locator", func(t *testing.T) {
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
		assert.Error(t, err)
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
		assert.NoError(t, err)

		// in dry mode, we should only notify admin but not delete or ban
		assert.Equal(t, 1, len(mockAPI.SendCalls()), "Should send detection results to admin")
		assert.Equal(t, 0, len(mockAPI.RequestCalls()), "Should not make request calls in dry mode")
		assert.Equal(t, 0, len(botMock.UpdateSpamCalls()), "Should not update spam in dry mode")
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
		assert.Equal(t, 1, len(mockAPI.SendCalls()), "Should send detection results to admin")
		// the code makes at least 2 Request calls - one to delete message and one to ban user
		assert.GreaterOrEqual(t, len(mockAPI.RequestCalls()), 2, "Should request to delete the message and ban user")
		assert.Equal(t, 1, len(botMock.UpdateSpamCalls()), "Should update spam samples")
	})
}
