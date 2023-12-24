package bot

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
		CheckFunc: func(msg string, userID string) (bool, []lib.CheckResult) {
			if msg == "spam" {
				return true, []lib.CheckResult{{Name: "something", Spam: true, Details: "some spam"}}
			}
			return false, []lib.CheckResult{{Name: "already approved", Spam: false, Details: "some ham"}}
		},
	}

	t.Run("spam detected", func(t *testing.T) {
		s := NewSpamFilter(ctx, det, SpamConfig{SpamMsg: "detected", SpamDryMsg: "detected dry"})
		resp := s.OnMessage(Message{Text: "spam", From: User{ID: 1, Username: "john"}})
		assert.Equal(t, Response{Text: `detected: "john" (1)`, Send: true, BanInterval: PermanentBanDuration,
			User: User{ID: 1, Username: "john"}, DeleteReplyTo: true,
			CheckResults: []lib.CheckResult{{Name: "something", Spam: true, Details: "some spam"}}}, resp)
		t.Logf("resp: %+v", resp)
	})

	t.Run("spam detected, dry", func(t *testing.T) {
		s := NewSpamFilter(ctx, det, SpamConfig{SpamMsg: "detected", SpamDryMsg: "detected dry", Dry: true})
		resp := s.OnMessage(Message{Text: "spam", From: User{ID: 1, Username: "john"}})
		assert.Equal(t, `detected dry: "john" (1)`, resp.Text)
		assert.True(t, resp.Send)
		assert.Equal(t, []lib.CheckResult{{Name: "something", Spam: true, Details: "some spam"}}, resp.CheckResults)
	})

	t.Run("ham detected", func(t *testing.T) {
		s := NewSpamFilter(ctx, det, SpamConfig{SpamMsg: "detected", SpamDryMsg: "detected dry"})
		resp := s.OnMessage(Message{Text: "good", From: User{ID: 1, Username: "john"}})
		assert.Equal(t, Response{CheckResults: []lib.CheckResult{{Name: "already approved", Spam: false, Details: "some ham"}}}, resp)
	})

}

func TestSpamFilter_reloadSamples(t *testing.T) {
	mockDirector := &mocks.DetectorMock{
		LoadSamplesFunc: func(exclReader io.Reader, spamReaders []io.Reader, hamReaders []io.Reader) (lib.LoadResult, error) {
			return lib.LoadResult{}, nil
		},
		LoadStopWordsFunc: func(readers ...io.Reader) (lib.LoadResult, error) {
			return lib.LoadResult{}, nil
		},
	}

	tests := []struct {
		name        string
		modify      func(s *SpamConfig)
		expectedErr error
	}{
		{
			name:        "Successful execution",
			modify:      func(s *SpamConfig) {},
			expectedErr: nil,
		},
		{
			name: "Spam samples file open failure",
			modify: func(s *SpamConfig) {
				s.SpamSamplesFile = "fail"
			},
			expectedErr: errors.New("failed to open spam samples file \"fail\": open fail: no such file or directory"),
		},
		{
			name: "Ham samples file open failure",
			modify: func(s *SpamConfig) {
				s.HamSamplesFile = "fail"
			},
			expectedErr: errors.New("failed to open ham samples file \"fail\": open fail: no such file or directory"),
		},
		{
			name: "Stop words file not found",
			modify: func(s *SpamConfig) {
				s.StopWordsFile = "notfound"
			},
			expectedErr: nil,
		},
		{
			name: "Excluded tokens file not found",
			modify: func(s *SpamConfig) {
				s.ExcludedTokensFile = "notfound"
			},
			expectedErr: nil,
		},
		{
			name: "Spam dynamic file not found",
			modify: func(s *SpamConfig) {
				s.SpamDynamicFile = "notfound"
			},
			expectedErr: nil,
		},
		{
			name: "Ham dynamic file not found",
			modify: func(s *SpamConfig) {
				s.HamDynamicFile = "notfound"
			},
			expectedErr: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Create temporary files for each test
			spamSamplesFile, err := os.CreateTemp("", "spam")
			require.NoError(t, err)
			defer os.Remove(spamSamplesFile.Name())

			hamSamplesFile, err := os.CreateTemp("", "ham")
			require.NoError(t, err)
			defer os.Remove(hamSamplesFile.Name())

			stopWordsFile, err := os.CreateTemp("", "stopwords")
			require.NoError(t, err)
			defer os.Remove(stopWordsFile.Name())

			excludedTokensFile, err := os.CreateTemp("", "excludedtokens")
			require.NoError(t, err)
			defer os.Remove(excludedTokensFile.Name())

			// reset to default values before each test
			params := SpamConfig{
				SpamSamplesFile:    spamSamplesFile.Name(),
				HamSamplesFile:     hamSamplesFile.Name(),
				StopWordsFile:      stopWordsFile.Name(),
				ExcludedTokensFile: excludedTokensFile.Name(),
				SpamDynamicFile:    "optional",
				HamDynamicFile:     "optional",
			}
			tc.modify(&params)
			s := NewSpamFilter(ctx, mockDirector, params)

			err = s.ReloadSamples()

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

	count := 0
	mockDetector := &mocks.DetectorMock{
		LoadSamplesFunc: func(exclReader io.Reader, spamReaders []io.Reader, hamReaders []io.Reader) (lib.LoadResult, error) {
			count++
			if count == 1 { // only first call should succeed
				return lib.LoadResult{}, nil
			}
			return lib.LoadResult{}, errors.New("error")
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

	NewSpamFilter(ctx, mockDetector, SpamConfig{
		ExcludedTokensFile: excludedTokensFile,
		SpamSamplesFile:    spamSamplesFile,
		HamSamplesFile:     hamSamplesFile,
		StopWordsFile:      stopWordsFile,
		WatchDelay:         time.Millisecond * 100,
	})

	time.Sleep(200 * time.Millisecond) // let it start

	assert.Equal(t, 0, len(mockDetector.LoadSamplesCalls()))
	assert.Equal(t, 0, len(mockDetector.LoadStopWordsCalls()))

	// write to spam samples file
	message := "spam message"
	err = os.WriteFile(spamSamplesFile, []byte(message), 0o600)
	require.NoError(t, err)
	// wait for reload to complete
	time.Sleep(time.Millisecond * 200)

	assert.Equal(t, 1, len(mockDetector.LoadSamplesCalls()))
	assert.Equal(t, 1, len(mockDetector.LoadStopWordsCalls()))

	// write to ham samples file
	message = "ham message"
	err = os.WriteFile(hamSamplesFile, []byte(message), 0o600)
	require.NoError(t, err)
	// wait for reload to complete
	time.Sleep(time.Millisecond * 200)
	assert.Equal(t, 2, len(mockDetector.LoadSamplesCalls()))
	assert.Equal(t, 1, len(mockDetector.LoadStopWordsCalls()))

	// wait to make sure no more reloads happen
	time.Sleep(time.Millisecond * 500)
	assert.Equal(t, 2, len(mockDetector.LoadSamplesCalls()))
	assert.Equal(t, 1, len(mockDetector.LoadStopWordsCalls()))
}

func TestSpamFilter_WatchMultipleUpdates(t *testing.T) {
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

	NewSpamFilter(ctx, mockDetector, SpamConfig{
		ExcludedTokensFile: excludedTokensFile,
		SpamSamplesFile:    spamSamplesFile,
		HamSamplesFile:     hamSamplesFile,
		StopWordsFile:      stopWordsFile,
		WatchDelay:         time.Millisecond * 100,
	})

	time.Sleep(200 * time.Millisecond) // let it start

	// simulate rapid file changes
	message := "spam message"
	for i := 0; i < 5; i++ {
		err = os.WriteFile(spamSamplesFile, []byte(message+strconv.Itoa(i)), 0o600)
		require.NoError(t, err)
		time.Sleep(10 * time.Millisecond) // less than the debounce interval
	}

	// wait for reload to complete
	time.Sleep(200 * time.Millisecond)

	// ponly one reload should happen despite multiple updates
	assert.Equal(t, 1, len(mockDetector.LoadSamplesCalls()))

	// make sure no more reloads happen
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, 1, len(mockDetector.LoadSamplesCalls()))
}

func TestSpamFilter_Update(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockDetector := &mocks.DetectorMock{
		UpdateSpamFunc: func(msg string) error {
			if msg == "err" {
				return errors.New("error")
			}
			return nil
		},
		UpdateHamFunc: func(msg string) error {
			if msg == "err" {
				return errors.New("error")
			}
			return nil
		},
	}

	sf := NewSpamFilter(ctx, mockDetector, SpamConfig{})

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

func TestAddApprovedUsers(t *testing.T) {
	mockDirector := &mocks.DetectorMock{AddApprovedUsersFunc: func(ids ...string) {}}

	t.Run("add single approved user", func(t *testing.T) {
		mockDirector.ResetCalls()
		sf := SpamFilter{Detector: mockDirector}
		sf.AddApprovedUsers(1)
		require.Equal(t, 1, len(mockDirector.AddApprovedUsersCalls()))
		assert.Equal(t, []string{"1"}, mockDirector.AddApprovedUsersCalls()[0].Ids)
	})

	t.Run("add multiple approved users", func(t *testing.T) {
		mockDirector.ResetCalls()
		sf := SpamFilter{Detector: mockDirector}
		sf.AddApprovedUsers(1, 2, 3)
		require.Equal(t, 1, len(mockDirector.AddApprovedUsersCalls()))
		assert.Equal(t, []string{"1", "2", "3"}, mockDirector.AddApprovedUsersCalls()[0].Ids)
	})

	t.Run("add empty list of approved users", func(t *testing.T) {
		mockDirector.ResetCalls()
		sf := SpamFilter{Detector: mockDirector}
		sf.AddApprovedUsers(1, 2, 3)
		require.Equal(t, 1, len(mockDirector.AddApprovedUsersCalls()))
		assert.Equal(t, []string{"1", "2", "3"}, mockDirector.AddApprovedUsersCalls()[0].Ids)
	})
}

func TestRemoveApprovedUsers(t *testing.T) {
	mockDirector := &mocks.DetectorMock{RemoveApprovedUsersFunc: func(ids ...string) {}}

	t.Run("remove single approved user", func(t *testing.T) {
		mockDirector.ResetCalls()
		sf := SpamFilter{Detector: mockDirector}
		sf.RemoveApprovedUsers(1)
		require.Equal(t, 1, len(mockDirector.RemoveApprovedUsersCalls()))
		assert.Equal(t, []string{"1"}, mockDirector.RemoveApprovedUsersCalls()[0].Ids)
	})

	t.Run("remove multiple approved users", func(t *testing.T) {
		mockDirector.ResetCalls()
		sf := SpamFilter{Detector: mockDirector}
		sf.RemoveApprovedUsers(1, 2, 3)
		require.Equal(t, 1, len(mockDirector.RemoveApprovedUsersCalls()))
		assert.Equal(t, []string{"1", "2", "3"}, mockDirector.RemoveApprovedUsersCalls()[0].Ids)
	})

	t.Run("remove empty list of approved users", func(t *testing.T) {
		mockDirector.ResetCalls()
		sf := SpamFilter{Detector: mockDirector}
		sf.RemoveApprovedUsers(1, 2, 3)
		require.Equal(t, 1, len(mockDirector.RemoveApprovedUsersCalls()))
		assert.Equal(t, []string{"1", "2", "3"}, mockDirector.RemoveApprovedUsersCalls()[0].Ids)
	})
}
