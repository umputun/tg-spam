package bot

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

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
				assert.Equal(t, 1, len(det.CheckCalls()))
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
			assert.NoError(t, err)
			assert.Equal(t, 1, len(det.UpdateSpamCalls()))
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
			assert.NoError(t, err)
			assert.Equal(t, 1, len(det.UpdateHamCalls()))
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
			assert.NoError(t, err)

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
			assert.NoError(t, err)

			// verify all required methods were called
			assert.Equal(t, 1, len(det.LoadSamplesCalls()))
			assert.Equal(t, 1, len(det.LoadStopWordsCalls()))
			assert.Equal(t, 1, len(samplesStore.StatsCalls()))
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
			assert.NoError(t, err)
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
			assert.Equal(t, 1, len(det.IsApprovedUserCalls()))
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
			assert.NoError(t, err)
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
