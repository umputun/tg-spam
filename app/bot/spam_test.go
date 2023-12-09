package bot

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot/mocks"
	"github.com/umputun/tg-spam/lib"
)

func TestSpamFilter_OnMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	det := &mocks.DetectorMock{
		CheckFunc: func(msg string, userID int64) (bool, []lib.CheckResult) {
			if msg == "spam" {
				return true, []lib.CheckResult{{Name: "something", Spam: true, Details: "some spam"}}
			}
			return false, nil
		},
	}

	t.Run("spam detected", func(t *testing.T) {
		s := NewSpamFilter(ctx, det, nil, nil, SpamParams{SpamMsg: "detected", SpamDryMsg: "detected dry"})
		resp := s.OnMessage(Message{Text: "spam", From: User{ID: 1, Username: "john"}})
		assert.Equal(t, Response{Text: `detected: "john" (1)`, Send: true, BanInterval: permanentBanDuration,
			User: User{ID: 1, Username: "john"}, DeleteReplyTo: true}, resp)
		t.Logf("resp: %+v", resp)
	})

	t.Run("spam detected, dry", func(t *testing.T) {
		s := NewSpamFilter(ctx, det, nil, nil, SpamParams{SpamMsg: "detected", SpamDryMsg: "detected dry", Dry: true})
		resp := s.OnMessage(Message{Text: "spam", From: User{ID: 1, Username: "john"}})
		assert.Equal(t, `detected dry: "john" (1)`, resp.Text)
		assert.True(t, resp.Send)
	})

	t.Run("ham detected", func(t *testing.T) {
		s := NewSpamFilter(ctx, det, nil, nil, SpamParams{SpamMsg: "detected", SpamDryMsg: "detected dry"})
		resp := s.OnMessage(Message{Text: "good", From: User{ID: 1, Username: "john"}})
		assert.Equal(t, Response{}, resp)
	})

}

func TestSpamFilter_reloadSamples(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var osOpen = os.Open // Mock for os.Open function

	// Mock os.Open
	openOriginal := osOpen
	defer func() { osOpen = openOriginal }()
	osOpen = func(name string) (*os.File, error) {
		if name == "fail" {
			return nil, errors.New("open error")
		}
		if name == "notfound" {
			return nil, os.ErrNotExist
		}
		return os.Open(os.DevNull) // Open a harmless file for other cases
	}

	mockDirector := &mocks.DetectorMock{
		LoadSamplesFunc: func(exclReader io.Reader, spamReaders []io.Reader, hamReaders []io.Reader) (lib.LoadResult, error) {
			return lib.LoadResult{}, nil
		},
		LoadStopWordsFunc: func(readers ...io.Reader) (lib.LoadResult, error) {
			return lib.LoadResult{}, nil
		},
	}

	s := NewSpamFilter(ctx, mockDirector, nil, nil, SpamParams{
		SpamSamplesFile:    "/dev/null",
		HamSamplesFile:     "/dev/null",
		StopWordsFile:      "optional",
		ExcludedTokensFile: "optional",
		SpamDynamicFile:    "optional",
		HamDynamicFile:     "optional",
	})

	tests := []struct {
		name        string
		modify      func()
		expectedErr error
	}{
		{
			name:        "Successful execution",
			modify:      func() {},
			expectedErr: nil,
		},
		{
			name: "Spam samples file open failure",
			modify: func() {
				s.params.SpamSamplesFile = "fail"
			},
			expectedErr: errors.New("failed to open spam samples file \"fail\": open fail: no such file or directory"),
		},
		{
			name: "Ham samples file open failure",
			modify: func() {
				s.params.HamSamplesFile = "fail"
			},
			expectedErr: errors.New("failed to open ham samples file \"fail\": open fail: no such file or directory"),
		},
		{
			name: "Stop words file not found",
			modify: func() {
				s.params.StopWordsFile = "notfound"
			},
			expectedErr: nil,
		},
		{
			name: "Excluded tokens file not found",
			modify: func() {
				s.params.ExcludedTokensFile = "notfound"
			},
			expectedErr: nil,
		},
		{
			name: "Spam dynamic file not found",
			modify: func() {
				s.params.SpamDynamicFile = "notfound"
			},
			expectedErr: nil,
		},
		{
			name: "Ham dynamic file not found",
			modify: func() {
				s.params.HamDynamicFile = "notfound"
			},
			expectedErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Reset to default values before each test
			s = NewSpamFilter(ctx, mockDirector, nil, nil, SpamParams{
				SpamSamplesFile:    "/dev/null",
				HamSamplesFile:     "/dev/null",
				StopWordsFile:      "optional",
				ExcludedTokensFile: "optional",
				SpamDynamicFile:    "optional",
				HamDynamicFile:     "optional",
			})
			tc.modify()
			err := s.ReloadSamples()

			if tc.expectedErr != nil {
				require.Error(t, err)
				assert.Equal(t, tc.expectedErr.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSpamFilter_watch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockDetector := &mocks.DetectorMock{
		LoadSamplesFunc: func(exclReader io.Reader, spamReaders []io.Reader, hamReaders []io.Reader) (lib.LoadResult, error) {
			return lib.LoadResult{}, nil
		},
		LoadStopWordsFunc: func(readers ...io.Reader) (lib.LoadResult, error) {
			return lib.LoadResult{}, nil
		},
	}

	tmpDir, err := os.MkdirTemp("", "spamfilter_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	excludedTokensFile := filepath.Join(tmpDir, "excluded_tokens.txt")
	spamSamplesFile := filepath.Join(tmpDir, "spam_samples.txt")
	hamSamplesFile := filepath.Join(tmpDir, "ham_samples.txt")
	stopWordsFile := filepath.Join(tmpDir, "stop_words.txt")

	_, err = os.Create(excludedTokensFile)
	require.NoError(t, err)
	_, err = os.Create(spamSamplesFile)
	require.NoError(t, err)
	_, err = os.Create(hamSamplesFile)
	require.NoError(t, err)
	_, err = os.Create(stopWordsFile)
	require.NoError(t, err)

	NewSpamFilter(ctx, mockDetector, nil, nil, SpamParams{
		ExcludedTokensFile: excludedTokensFile,
		SpamSamplesFile:    spamSamplesFile,
		HamSamplesFile:     hamSamplesFile,
		StopWordsFile:      stopWordsFile,
	})

	time.Sleep(100 * time.Millisecond) // let it start

	assert.Equal(t, 0, len(mockDetector.LoadSamplesCalls()))
	assert.Equal(t, 0, len(mockDetector.LoadStopWordsCalls()))

	// write to spam samples file
	message := "spam message"
	err = os.WriteFile(spamSamplesFile, []byte(message), 0o600)
	require.NoError(t, err)
	// wait for reload to complete
	time.Sleep(1 * time.Second)

	assert.Equal(t, 1, len(mockDetector.LoadSamplesCalls()))
	assert.Equal(t, 1, len(mockDetector.LoadStopWordsCalls()))

	// make load samples fail
	mockDetector.LoadSamplesFunc = func(_ io.Reader, _ []io.Reader, _ []io.Reader) (lib.LoadResult, error) {
		return lib.LoadResult{}, errors.New("error")
	}
	// write to spam samples file
	message = "spam message"
	err = os.WriteFile(spamSamplesFile, []byte(message), 0o600)
	require.NoError(t, err)
	// wait for reload to complete
	time.Sleep(1 * time.Second)
	assert.Equal(t, 2, len(mockDetector.LoadSamplesCalls()))
	cancel()
}

func TestSpamFilter_Update(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockDetector := &mocks.DetectorMock{}

	mockSpamUpdater := &mocks.SampleUpdater{
		AppendFunc: func(msg string) error {
			if msg == "err" {
				return errors.New("error")
			}
			return nil
		},
	}
	mockHamUpdater := &mocks.SampleUpdater{
		AppendFunc: func(msg string) error {
			if msg == "err" {
				return errors.New("error")
			}
			return nil
		},
	}

	sf := NewSpamFilter(ctx, mockDetector, mockSpamUpdater, mockHamUpdater, SpamParams{})

	t.Run("good update", func(t *testing.T) {
		err := sf.UpdateSpam("spam")
		assert.NoError(t, err)

		err = sf.UpdateHam("ham")
		assert.NoError(t, err)
	})

	t.Run("bad update", func(t *testing.T) {
		err := sf.UpdateSpam("err")
		assert.Error(t, err)

		err = sf.UpdateHam("err")
		assert.Error(t, err)
	})
}
