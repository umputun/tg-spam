package bot

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot/mocks"
	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/lib/approved"
	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam"
)

func TestSpamFilter_OnMessage(t *testing.T) {
	tests := []struct {
		name         string
		message      Message
		checkOnly    bool
		dry          bool
		wantResponse Response
		wantRequest  spamcheck.Request
	}{
		{
			name: "spam detected",
			message: Message{
				Text:  "spam message",
				From:  User{ID: 1, Username: "user1"},
				Image: &Image{FileID: "123"},
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "spam message",
				UserID:   "1",
				UserName: "user1",
				Meta:     spamcheck.MetaData{Images: 1},
			},
		},
		{
			name: "spam with both video and forward",
			message: Message{
				Text:        "spam message",
				From:        User{ID: 1, Username: "user1"},
				WithVideo:   true,
				WithForward: true,
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "spam message",
				UserID:   "1",
				UserName: "user1",
				Meta:     spamcheck.MetaData{HasVideo: true, HasForward: true},
			},
		},
		{
			name: "spam with video note",
			message: Message{
				Text:          "spam message",
				From:          User{ID: 1, Username: "user1"},
				WithVideoNote: true,
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "spam message",
				UserID:   "1",
				UserName: "user1",
				Meta:     spamcheck.MetaData{HasVideo: true},
			},
		},
		{
			name: "spam detected dry mode",
			message: Message{
				Text: "spam message",
				From: User{ID: 1, Username: "user1"},
			},
			dry: true,
			wantResponse: Response{
				Text:          `detected dry: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
		},
		{
			name: "ham detected",
			message: Message{
				Text: "good message",
				From: User{ID: 1, Username: "user1"},
			},
			wantResponse: Response{
				CheckResults: []spamcheck.Response{{Name: "test", Spam: false, Details: "ham"}},
			},
		},
		{
			name: "system message without content",
			message: Message{
				Text: "system",
				From: User{ID: 0},
			},
			wantResponse: Response{},
		},
		{
			name: "system message with content",
			message: Message{
				Text:        "system message",
				From:        User{ID: 0},
				WithForward: true,
				Image:       &Image{FileID: "123"},
			},
			wantResponse: Response{},
		},
		{
			name: "with complex links",
			message: Message{
				Text: "message https://example.com/path?param=1 http://test.com http://test.com/another",
				From: User{ID: 1, Username: "user1"},
				Entities: &[]Entity{
					{Type: "url", Offset: 8, Length: 32},
					{Type: "url", Offset: 41, Length: 15},
					{Type: "url", Offset: 57, Length: 23},
				},
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "message https://example.com/path?param=1 http://test.com http://test.com/another",
				UserID:   "1",
				UserName: "user1",
				Meta:     spamcheck.MetaData{Links: 3},
			},
		},
		{
			name: "with display name",
			message: Message{
				Text: "spam message",
				From: User{ID: 1, Username: "user1", DisplayName: "User One"},
			},
			wantResponse: Response{
				Text:          `detected: "User One" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1", DisplayName: "User One"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "spam message",
				UserID:   "1",
				UserName: "user1",
			},
		},
		{
			name: "with text mention entities",
			message: Message{
				Text: "spam message @someone",
				From: User{ID: 1, Username: "user1"},
				Entities: &[]Entity{
					{Type: "mention", Offset: 13, Length: 8},
				},
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "spam message @someone",
				UserID:   "1",
				UserName: "user1",
				Meta:     spamcheck.MetaData{Mentions: 1},
			},
		},
		{
			name: "with image caption mention entities",
			message: Message{
				Text: "Пишите - @zhanna_live23",
				From: User{ID: 1, Username: "user1"},
				Image: &Image{
					FileID:  "123",
					Caption: "Пишите - @zhanna_live23",
					Entities: &[]Entity{
						{Type: "mention", Offset: 9, Length: 14},
					},
				},
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "Пишите - @zhanna_live23",
				UserID:   "1",
				UserName: "user1",
				Meta:     spamcheck.MetaData{Images: 1, Mentions: 1},
			},
		},
		{
			name: "with both text and image caption mentions",
			message: Message{
				Text: "@user1 check this",
				From: User{ID: 1, Username: "user1"},
				Entities: &[]Entity{
					{Type: "mention", Offset: 0, Length: 6},
				},
				Image: &Image{
					FileID:  "123",
					Caption: "contact @someone",
					Entities: &[]Entity{
						{Type: "mention", Offset: 8, Length: 8},
					},
				},
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "@user1 check this",
				UserID:   "1",
				UserName: "user1",
				Meta:     spamcheck.MetaData{Images: 1, Mentions: 2},
			},
		},
		{
			name: "with url entity in text",
			message: Message{
				Text: "check example.com for details",
				From: User{ID: 1, Username: "user1"},
				Entities: &[]Entity{
					{Type: "url", Offset: 6, Length: 11},
				},
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "check example.com for details",
				UserID:   "1",
				UserName: "user1",
				Meta:     spamcheck.MetaData{Links: 1},
			},
		},
		{
			name: "with text_link entity in image caption",
			message: Message{
				Text: "Click here for details",
				From: User{ID: 1, Username: "user1"},
				Image: &Image{
					FileID:  "123",
					Caption: "Click here for details",
					Entities: &[]Entity{
						{Type: "text_link", Offset: 0, Length: 10, URL: "https://example.com"},
					},
				},
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "Click here for details",
				UserID:   "1",
				UserName: "user1",
				Meta:     spamcheck.MetaData{Images: 1, Links: 1},
			},
		},
		{
			name: "with multiple link types",
			message: Message{
				Text: "visit https://site.com or click here",
				From: User{ID: 1, Username: "user1"},
				Entities: &[]Entity{
					{Type: "url", Offset: 6, Length: 16},
					{Type: "text_link", Offset: 26, Length: 10, URL: "https://other.com"},
				},
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "visit https://site.com or click here",
				UserID:   "1",
				UserName: "user1",
				Meta:     spamcheck.MetaData{Links: 2}, // 2 from entities (url + text_link)
			},
		},
		{
			name: "spam in quoted/reply-to text from external channel",
			message: Message{
				Text: "Есть в наличии",
				From: User{ID: 1, Username: "user1"},
				ReplyTo: struct {
					From       User
					Text       string `json:",omitempty"`
					Sent       time.Time
					SenderChat SenderChat `json:"sender_chat,omitzero"`
				}{
					Text: "Мефедрон VHQ Кристалл 1г",
					From: User{ID: 999, Username: "spammer_channel"},
				},
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "Есть в наличии\nМефедрон VHQ Кристалл 1г",
				UserID:   "1",
				UserName: "user1",
			},
		},
		{
			name: "message with empty reply-to text",
			message: Message{
				Text: "spam message",
				From: User{ID: 1, Username: "user1"},
				ReplyTo: struct {
					From       User
					Text       string `json:",omitempty"`
					Sent       time.Time
					SenderChat SenderChat `json:"sender_chat,omitzero"`
				}{
					Text: "",
					From: User{ID: 999, Username: "other_user"},
				},
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "spam message",
				UserID:   "1",
				UserName: "user1",
			},
		},
		{
			name: "empty main text with spam in reply-to",
			message: Message{
				Text: "",
				From: User{ID: 1, Username: "user1"},
				ReplyTo: struct {
					From       User
					Text       string `json:",omitempty"`
					Sent       time.Time
					SenderChat SenderChat `json:"sender_chat,omitzero"`
				}{
					Text: "spam in quoted message",
					From: User{ID: 999, Username: "spammer_channel"},
				},
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "\nspam in quoted message",
				UserID:   "1",
				UserName: "user1",
			},
		},
		{
			name: "spam in Quote field (telegram TextQuote)",
			message: Message{
				Text:  "Мяу в наличии!",
				From:  User{ID: 1, Username: "user1"},
				Quote: "Мефедрон VHQ Кристалл 1г",
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "Мяу в наличии!\nМефедрон VHQ Кристалл 1г",
				UserID:   "1",
				UserName: "user1",
			},
		},
		{
			name: "both Quote and ReplyTo.Text present - Quote takes precedence",
			message: Message{
				Text:  "check this",
				From:  User{ID: 1, Username: "user1"},
				Quote: "spam quote text",
				ReplyTo: struct {
					From       User
					Text       string `json:",omitempty"`
					Sent       time.Time
					SenderChat SenderChat `json:"sender_chat,omitzero"`
				}{
					Text: "full reply text",
				},
			},
			wantResponse: Response{
				Text:          `detected: "user1" (1)`,
				Send:          true,
				BanInterval:   PermanentBanDuration,
				DeleteReplyTo: true,
				User:          User{ID: 1, Username: "user1"},
				CheckResults:  []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}},
			},
			wantRequest: spamcheck.Request{
				Msg:      "check this\nspam quote text",
				UserID:   "1",
				UserName: "user1",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			det := &mocks.DetectorMock{
				CheckFunc: func(req spamcheck.Request) (bool, []spamcheck.Response) {
					if tc.wantRequest != (spamcheck.Request{}) {
						assert.Equal(t, tc.wantRequest, req)
					}
					if tc.message.Text == "good message" {
						return false, []spamcheck.Response{{Name: "test", Spam: false, Details: "ham"}}
					}
					return true, []spamcheck.Response{{Name: "test", Spam: true, Details: "spam"}}
				},
			}

			s := NewSpamFilter(det, SpamConfig{
				SpamMsg:    "detected",
				SpamDryMsg: "detected dry",
				Dry:        tc.dry,
			})

			got := s.OnMessage(tc.message, tc.checkOnly)
			assert.Equal(t, tc.wantResponse, got)

			if tc.message.From.ID == 0 {
				assert.Empty(t, det.CheckCalls())
			} else {
				assert.Len(t, det.CheckCalls(), 1)
			}
		})
	}
}

func TestSpamFilter_UpdateSpam(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		updateErr   error
		expectError bool
	}{
		{
			name:        "successful update",
			message:     "spam message",
			expectError: false,
		},
		{
			name:        "update error",
			message:     "err",
			updateErr:   errors.New("update error"),
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			det := &mocks.DetectorMock{
				UpdateSpamFunc: func(msg string) error { return tc.updateErr },
			}

			samplesStore := &mocks.SamplesStoreMock{}
			dictStore := &mocks.DictStoreMock{
				ReaderFunc: func(ctx context.Context, t storage.DictionaryType) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("")), nil
				},
			}

			s := NewSpamFilter(det, SpamConfig{
				SamplesStore: samplesStore,
				DictStore:    dictStore,
				GroupID:      "gr1",
			})

			err := s.UpdateSpam(tc.message)
			if tc.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, det.UpdateSpamCalls(), 1)
			assert.Equal(t, strings.ReplaceAll(tc.message, "\n", " "), det.UpdateSpamCalls()[0].Msg)
		})
	}
}

func TestSpamFilter_UpdateHam(t *testing.T) {
	tests := []struct {
		name        string
		message     string
		updateErr   error
		expectError bool
	}{
		{
			name:        "successful update",
			message:     "ham message",
			expectError: false,
		},
		{
			name:        "update error",
			message:     "err",
			updateErr:   errors.New("update error"),
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			det := &mocks.DetectorMock{
				UpdateHamFunc: func(msg string) error { return tc.updateErr },
			}

			samplesStore := &mocks.SamplesStoreMock{}
			dictStore := &mocks.DictStoreMock{
				ReaderFunc: func(ctx context.Context, t storage.DictionaryType) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("")), nil
				},
			}

			s := NewSpamFilter(det, SpamConfig{
				SamplesStore: samplesStore,
				DictStore:    dictStore,
				GroupID:      "gr1",
			})

			err := s.UpdateHam(tc.message)
			if tc.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, det.UpdateHamCalls(), 1)
			assert.Equal(t, strings.ReplaceAll(tc.message, "\n", " "), det.UpdateHamCalls()[0].Msg)
		})
	}
}

func TestSpamFilter_ApprovedUsers(t *testing.T) {
	tests := []struct {
		name         string
		userID       int64
		userName     string
		operation    string // "add" or "remove"
		operationErr error
		expectError  bool
	}{
		{
			name:        "add user success",
			userID:      123,
			userName:    "test_user",
			operation:   "add",
			expectError: false,
		},
		{
			name:         "add user operation error",
			userID:       -1,
			userName:     "test_user",
			operation:    "add",
			operationErr: errors.New("operation failed"),
			expectError:  true,
		},
		{
			name:        "remove user success",
			userID:      123,
			operation:   "remove",
			expectError: false,
		},
		{
			name:         "remove user operation error",
			userID:       -1,
			operation:    "remove",
			operationErr: errors.New("operation failed"),
			expectError:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			det := &mocks.DetectorMock{
				AddApprovedUserFunc: func(user approved.UserInfo) error {
					if tc.operationErr != nil {
						return tc.operationErr
					}
					assert.Equal(t, strconv.FormatInt(tc.userID, 10), user.UserID)
					assert.Equal(t, tc.userName, user.UserName)
					return nil
				},
				RemoveApprovedUserFunc: func(id string) error {
					if tc.operationErr != nil {
						return tc.operationErr
					}
					assert.Equal(t, strconv.FormatInt(tc.userID, 10), id)
					return nil
				},
				IsApprovedUserFunc: func(userID string) bool {
					return userID == strconv.FormatInt(tc.userID, 10)
				},
			}

			samplesStore := &mocks.SamplesStoreMock{
				StatsFunc: func(ctx context.Context) (*storage.SamplesStats, error) {
					return &storage.SamplesStats{PresetSpam: 1, PresetHam: 1}, nil
				},
				ReaderFunc: func(ctx context.Context, t storage.SampleType, o storage.SampleOrigin) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("")), nil
				},
			}

			dictStore := &mocks.DictStoreMock{
				ReaderFunc: func(ctx context.Context, t storage.DictionaryType) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("")), nil
				},
			}

			s := NewSpamFilter(det, SpamConfig{
				SamplesStore: samplesStore,
				DictStore:    dictStore,
				GroupID:      "gr1",
			})

			var err error
			switch tc.operation {
			case "add":
				err = s.AddApprovedUser(tc.userID, tc.userName)
			case "remove":
				err = s.RemoveApprovedUser(tc.userID)
			}

			if tc.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			// check IsApprovedUser
			result := s.IsApprovedUser(tc.userID)
			if tc.operation == "add" && !tc.expectError {
				assert.True(t, result)
			}
		})
	}
}

func TestSpamFilter_ReloadSamples(t *testing.T) {
	tests := []struct {
		name         string
		statsResult  *storage.SamplesStats
		statsErr     error
		readerErr    error
		loadErr      error
		stopWordsErr error
		expectError  bool
	}{
		{
			name:        "successful reload",
			statsResult: &storage.SamplesStats{PresetSpam: 10, PresetHam: 5},
		},
		{
			name:        "no preset samples",
			statsResult: &storage.SamplesStats{},
			expectError: true,
		},
		{
			name:        "stats error",
			statsErr:    errors.New("stats error"),
			expectError: true,
		},
		{
			name:        "spam reader error",
			statsResult: &storage.SamplesStats{PresetSpam: 10, PresetHam: 5},
			readerErr:   errors.New("reader error"),
			expectError: true,
		},
		{
			name:        "load samples error",
			statsResult: &storage.SamplesStats{PresetSpam: 10, PresetHam: 5},
			loadErr:     errors.New("load error"),
			expectError: true,
		},
		{
			name:         "stop words error",
			statsResult:  &storage.SamplesStats{PresetSpam: 10, PresetHam: 5},
			stopWordsErr: errors.New("stop words error"),
			expectError:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			det := &mocks.DetectorMock{
				LoadSamplesFunc: func(exclReader io.Reader, spamReaders []io.Reader, hamReaders []io.Reader) (tgspam.LoadResult, error) {
					return tgspam.LoadResult{SpamSamples: 10, HamSamples: 5}, tc.loadErr
				},
				LoadStopWordsFunc: func(readers ...io.Reader) (tgspam.LoadResult, error) {
					return tgspam.LoadResult{StopWords: 3}, tc.stopWordsErr
				},
			}

			samplesStore := &mocks.SamplesStoreMock{
				StatsFunc: func(ctx context.Context) (*storage.SamplesStats, error) {
					return tc.statsResult, tc.statsErr
				},
				ReaderFunc: func(ctx context.Context, t storage.SampleType, o storage.SampleOrigin) (io.ReadCloser, error) {
					if tc.readerErr != nil {
						return nil, tc.readerErr
					}
					return io.NopCloser(strings.NewReader("test data")), nil
				},
			}

			dictStore := &mocks.DictStoreMock{
				ReaderFunc: func(ctx context.Context, t storage.DictionaryType) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("test data")), nil
				},
			}

			s := NewSpamFilter(det, SpamConfig{
				SamplesStore: samplesStore,
				DictStore:    dictStore,
				GroupID:      "gr1",
			})

			err := s.ReloadSamples()
			if tc.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			// verify all required methods were called
			assert.Len(t, det.LoadSamplesCalls(), 1)
			assert.Len(t, det.LoadStopWordsCalls(), 1)
			assert.Len(t, samplesStore.StatsCalls(), 1)
		})
	}
}

func TestSpamFilter_RemoveDynamicSample(t *testing.T) {
	tests := []struct {
		name        string
		sample      string
		deleteErr   error
		loadErr     error
		expectError bool
	}{
		{
			name:   "remove spam success",
			sample: "spam message",
		},
		{
			name:        "delete error",
			sample:      "spam message",
			deleteErr:   errors.New("delete error"),
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			det := &mocks.DetectorMock{
				LoadSamplesFunc: func(exclReader io.Reader, spamReaders []io.Reader, hamReaders []io.Reader) (tgspam.LoadResult, error) {
					return tgspam.LoadResult{}, tc.loadErr
				},
				LoadStopWordsFunc: func(readers ...io.Reader) (tgspam.LoadResult, error) {
					return tgspam.LoadResult{}, nil
				},
				RemoveSpamFunc: func(msg string) error {
					assert.Equal(t, tc.sample, msg)
					return tc.deleteErr
				},
				RemoveHamFunc: func(msg string) error {
					assert.Equal(t, tc.sample, msg)
					return tc.deleteErr
				},
			}

			samplesStore := &mocks.SamplesStoreMock{}

			dictStore := &mocks.DictStoreMock{
				ReaderFunc: func(ctx context.Context, t storage.DictionaryType) (io.ReadCloser, error) {
					return io.NopCloser(strings.NewReader("")), nil
				},
			}

			s := NewSpamFilter(det, SpamConfig{
				SamplesStore: samplesStore,
				DictStore:    dictStore,
				GroupID:      "gr1",
			})

			err := s.RemoveDynamicSpamSample(tc.sample)
			if tc.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, det.RemoveSpamCalls(), 1)
			assert.Equal(t, tc.sample, det.RemoveSpamCalls()[0].Msg)
		})
	}
}

func TestSpamFilter_IsApprovedUser(t *testing.T) {
	tests := []struct {
		name         string
		userID       int64
		expectedCall string
		want         bool
	}{
		{
			name:         "user is approved",
			userID:       123,
			expectedCall: "123",
			want:         true,
		},
		{
			name:         "user is not approved",
			userID:       456,
			expectedCall: "456",
			want:         false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			det := &mocks.DetectorMock{
				IsApprovedUserFunc: func(userID string) bool {
					assert.Equal(t, tc.expectedCall, userID)
					return tc.want
				},
			}

			s := NewSpamFilter(det, SpamConfig{})
			got := s.IsApprovedUser(tc.userID)
			assert.Equal(t, tc.want, got)
			assert.Len(t, det.IsApprovedUserCalls(), 1)
		})
	}
}

func TestSpamFilter_DynamicSamples(t *testing.T) {
	tests := []struct {
		name        string
		spamSamples []string
		hamSamples  []string
		readErr     error
		expectError bool
	}{
		{
			name:        "successful read",
			spamSamples: []string{"spam1", "spam2"},
			hamSamples:  []string{"ham1", "ham2"},
		},
		{
			name:        "read error",
			readErr:     errors.New("read error"),
			expectError: true,
		},
		{
			name:        "empty response",
			spamSamples: []string{},
			hamSamples:  []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			samplesStore := &mocks.SamplesStoreMock{
				ReadFunc: func(ctx context.Context, t storage.SampleType, o storage.SampleOrigin) ([]string, error) {
					if tc.readErr != nil {
						return nil, tc.readErr
					}
					if t == storage.SampleTypeSpam {
						return tc.spamSamples, nil
					}
					return tc.hamSamples, nil
				},
			}

			s := NewSpamFilter(&mocks.DetectorMock{}, SpamConfig{
				SamplesStore: samplesStore,
			})

			spam, ham, err := s.DynamicSamples()
			if tc.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.spamSamples, spam)
			assert.Equal(t, tc.hamSamples, ham)

			// verify all Read calls were made
			calls := samplesStore.ReadCalls()
			require.Len(t, calls, 2)
			assert.Equal(t, storage.SampleTypeSpam, calls[0].T)
			assert.Equal(t, storage.SampleTypeHam, calls[1].T)
			assert.Equal(t, storage.SampleOriginUser, calls[0].O)
			assert.Equal(t, storage.SampleOriginUser, calls[1].O)
		})
	}
}

func TestSpamFilter_RemoveDynamicSamples(t *testing.T) {
	tests := []struct {
		name        string
		sample      string
		sampleType  string // "spam" or "ham"
		deleteErr   error
		expectError bool
	}{
		{
			name:       "remove spam success",
			sample:     "spam sample",
			sampleType: "spam",
		},
		{
			name:        "remove spam delete error",
			sample:      "spam sample",
			sampleType:  "spam",
			deleteErr:   errors.New("delete error"),
			expectError: true,
		},
		{
			name:       "remove ham success",
			sample:     "ham sample",
			sampleType: "ham",
		},
		{
			name:        "remove ham delete error",
			sample:      "ham sample",
			sampleType:  "ham",
			deleteErr:   errors.New("delete error"),
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			det := &mocks.DetectorMock{
				RemoveHamFunc: func(msg string) error {
					assert.Equal(t, tc.sample, msg)
					return tc.deleteErr
				},
				RemoveSpamFunc: func(msg string) error {
					assert.Equal(t, tc.sample, msg)
					return tc.deleteErr
				},
			}

			samplesStore := &mocks.SamplesStoreMock{}

			dictStore := &mocks.DictStoreMock{}

			s := NewSpamFilter(det, SpamConfig{
				SamplesStore: samplesStore,
				DictStore:    dictStore,
				GroupID:      "gr1",
			})

			var err error
			switch tc.sampleType {
			case "spam":
				err = s.RemoveDynamicSpamSample(tc.sample)
			case "ham":
				err = s.RemoveDynamicHamSample(tc.sample)
			}

			if tc.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tc.sampleType == "spam" {
				assert.Len(t, det.RemoveSpamCalls(), 1)
				assert.Equal(t, tc.sample, det.RemoveSpamCalls()[0].Msg)
			}
			if tc.sampleType == "ham" {
				assert.Len(t, det.RemoveHamCalls(), 1)
				assert.Equal(t, tc.sample, det.RemoveHamCalls()[0].Msg)
			}
		})
	}
}
