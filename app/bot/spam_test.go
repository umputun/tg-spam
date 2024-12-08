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
	"github.com/umputun/tg-spam/lib/approved"
	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam"
)

func TestSpamFilter_OnMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	det := &mocks.DetectorMock{
		CheckFunc: func(req spamcheck.Request) (bool, []spamcheck.Response) {
			if req.Msg == "spam" {
				return true, []spamcheck.Response{{Name: "something", Spam: true, Details: "some spam"}}
			}
			return false, []spamcheck.Response{{Name: "already approved", Spam: false, Details: "some ham"}}
		},
	}

	t.Run("spam detected", func(t *testing.T) {
		det.ResetCalls()
		s := NewSpamFilter(ctx, det, SpamConfig{SpamMsg: "detected", SpamDryMsg: "detected dry"})
		resp := s.OnMessage(Message{Text: "spam", From: User{ID: 1, Username: "john"}, Image: &Image{FileID: "123"}}, false)
		assert.Equal(t, Response{Text: `detected: "john" (1)`, Send: true, BanInterval: PermanentBanDuration,
			User: User{ID: 1, Username: "john"}, DeleteReplyTo: true,
			CheckResults: []spamcheck.Response{{Name: "something", Spam: true, Details: "some spam"}}}, resp)
		assert.Equal(t, 1, len(det.CheckCalls()))
		assert.Equal(t, spamcheck.Request{Msg: "spam", UserID: "1", UserName: "john", Meta: spamcheck.MetaData{Images: 1, Links: 0}},
			det.CheckCalls()[0].Request)
		t.Logf("resp: %+v", resp)
	})

	t.Run("spam detected, dry", func(t *testing.T) {
		s := NewSpamFilter(ctx, det, SpamConfig{SpamMsg: "detected", SpamDryMsg: "detected dry", Dry: true})
		resp := s.OnMessage(Message{Text: "spam", From: User{ID: 1, Username: "john"}}, false)
		assert.Equal(t, `detected dry: "john" (1)`, resp.Text)
		assert.True(t, resp.Send)
		assert.Equal(t, []spamcheck.Response{{Name: "something", Spam: true, Details: "some spam"}}, resp.CheckResults)
	})

	t.Run("ham detected", func(t *testing.T) {
		s := NewSpamFilter(ctx, det, SpamConfig{SpamMsg: "detected", SpamDryMsg: "detected dry"})
		resp := s.OnMessage(Message{Text: "good", From: User{ID: 1, Username: "john"}}, false)
		assert.Equal(t, Response{CheckResults: []spamcheck.Response{{Name: "already approved", Spam: false, Details: "some ham"}}}, resp)
	})

}

func TestSpamFilter_reloadSamples(t *testing.T) {
	mockDirector := &mocks.DetectorMock{
		LoadSamplesFunc: func(exclReader io.Reader, spamReaders []io.Reader, hamReaders []io.Reader) (tgspam.LoadResult, error) {
			return tgspam.LoadResult{}, nil
		},
		LoadStopWordsFunc: func(readers ...io.Reader) (tgspam.LoadResult, error) {
			return tgspam.LoadResult{}, nil
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
		LoadSamplesFunc: func(exclReader io.Reader, spamReaders []io.Reader, hamReaders []io.Reader) (tgspam.LoadResult, error) {
			count++
			if count == 1 { // only first call should succeed
				return tgspam.LoadResult{}, nil
			}
			return tgspam.LoadResult{}, errors.New("error")
		},
		LoadStopWordsFunc: func(readers ...io.Reader) (tgspam.LoadResult, error) {
			return tgspam.LoadResult{}, nil
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
		LoadSamplesFunc: func(exclReader io.Reader, spamReaders []io.Reader, hamReaders []io.Reader) (tgspam.LoadResult, error) {
			return tgspam.LoadResult{}, nil
		},
		LoadStopWordsFunc: func(readers ...io.Reader) (tgspam.LoadResult, error) {
			return tgspam.LoadResult{}, nil
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
		mockDetector.ResetCalls()
		err := sf.UpdateSpam("spam")
		assert.NoError(t, err)
		assert.Equal(t, 1, len(mockDetector.UpdateSpamCalls()))
		assert.Equal(t, "spam", mockDetector.UpdateSpamCalls()[0].Msg)

		err = sf.UpdateHam("ham")
		assert.NoError(t, err)
		assert.Equal(t, 1, len(mockDetector.UpdateHamCalls()))
		assert.Equal(t, "ham", mockDetector.UpdateHamCalls()[0].Msg)
	})

	t.Run("multi-line update", func(t *testing.T) {
		mockDetector.ResetCalls()
		err := sf.UpdateSpam("spam\nblah")
		assert.NoError(t, err)
		assert.Equal(t, 1, len(mockDetector.UpdateSpamCalls()))
		assert.Equal(t, "spam blah", mockDetector.UpdateSpamCalls()[0].Msg)

		err = sf.UpdateHam("ham\nblah")
		assert.NoError(t, err)
		assert.Equal(t, 1, len(mockDetector.UpdateHamCalls()))
		assert.Equal(t, "ham blah", mockDetector.UpdateHamCalls()[0].Msg)
	})

	t.Run("bad update", func(t *testing.T) {
		err := sf.UpdateSpam("err")
		assert.Error(t, err)

		err = sf.UpdateHam("err")
		assert.Error(t, err)
	})
}

func TestSpamFilter_AddApprovedUsers(t *testing.T) {
	mockDirector := &mocks.DetectorMock{
		AddApprovedUserFunc: func(user approved.UserInfo) error {
			if user.UserID == "-1" {
				return errors.New("error")
			}
			return nil
		},
	}

	sf := SpamFilter{Detector: mockDirector}

	t.Run("add single approved user, success", func(t *testing.T) {
		mockDirector.ResetCalls()
		err := sf.AddApprovedUser(1, "user1")
		assert.NoError(t, err)
		require.Equal(t, 1, len(mockDirector.AddApprovedUserCalls()))
		assert.Equal(t, "1", mockDirector.AddApprovedUserCalls()[0].User.UserID)
	})

	t.Run("add approved users, failed", func(t *testing.T) {
		mockDirector.ResetCalls()
		err := sf.AddApprovedUser(-1, "user1")
		assert.Error(t, err)
	})
}

func TestSpamFilter_RemoveApprovedUsers(t *testing.T) {
	mockDirector := &mocks.DetectorMock{
		RemoveApprovedUserFunc: func(id string) error {
			if id == "-1" {
				return errors.New("error")
			}
			return nil
		},
	}

	t.Run("remove approved user, success", func(t *testing.T) {
		mockDirector.ResetCalls()
		sf := SpamFilter{Detector: mockDirector}
		err := sf.RemoveApprovedUser(1)
		assert.NoError(t, err)
		require.Equal(t, 1, len(mockDirector.RemoveApprovedUserCalls()))
		assert.Equal(t, "1", mockDirector.RemoveApprovedUserCalls()[0].ID)
	})

	t.Run("remove approved user, failed", func(t *testing.T) {
		mockDirector.ResetCalls()
		sf := SpamFilter{Detector: mockDirector}
		err := sf.RemoveApprovedUser(-1)
		assert.Error(t, err)
		require.Equal(t, 1, len(mockDirector.RemoveApprovedUserCalls()))
		assert.Equal(t, "-1", mockDirector.RemoveApprovedUserCalls()[0].ID)
	})
}

func TestSpamFilter_DynamicSamples(t *testing.T) {
	spamFile, err := os.CreateTemp("", "spam_dynamic")
	require.NoError(t, err)
	defer os.Remove(spamFile.Name())

	hamFile, err := os.CreateTemp("", "ham_dynamic")
	require.NoError(t, err)
	defer os.Remove(hamFile.Name())

	_, err = spamFile.WriteString("spam1\nspam2\n")
	require.NoError(t, err)
	_, err = hamFile.WriteString("ham1\nham2\n")
	require.NoError(t, err)

	spamFile.Close()
	hamFile.Close()

	sf := NewSpamFilter(context.Background(), &mocks.DetectorMock{}, SpamConfig{
		SpamDynamicFile: spamFile.Name(),
		HamDynamicFile:  hamFile.Name(),
	})

	t.Run("successful read", func(t *testing.T) {
		spam, ham, err := sf.DynamicSamples()
		require.NoError(t, err)
		assert.Equal(t, []string{"spam1", "spam2"}, spam)
		assert.Equal(t, []string{"ham1", "ham2"}, ham)
	})

	t.Run("spam file not found", func(t *testing.T) {
		sf.params.SpamDynamicFile = "nonexistent_spam_file"
		spam, _, err := sf.DynamicSamples()
		assert.NoError(t, err)
		assert.Empty(t, spam, "Spam slice should be empty when file is not found")
	})

	t.Run("ham file not found", func(t *testing.T) {
		sf.params.HamDynamicFile = "nonexistent_ham_file"
		_, ham, err := sf.DynamicSamples()
		assert.NoError(t, err)
		assert.Empty(t, ham, "Ham slice should be empty when file is not found")
	})
}

func TestSpamFilter_RemoveDynamicSample(t *testing.T) {
	mockDirector := &mocks.DetectorMock{
		LoadSamplesFunc: func(exclReader io.Reader, spamReaders []io.Reader, hamReaders []io.Reader) (tgspam.LoadResult, error) {
			return tgspam.LoadResult{}, nil
		},
		LoadStopWordsFunc: func(readers ...io.Reader) (tgspam.LoadResult, error) {
			return tgspam.LoadResult{}, nil
		},
	}

	prep := func() (res *SpamFilter, teardown func()) {
		tmpDir, err := os.MkdirTemp("", "spamfilter_test")
		require.NoError(t, err)
		t.Logf("tmpDir: %s", tmpDir)
		spamFile, err := os.Create(filepath.Join(tmpDir, "spam_samples.txt"))
		require.NoError(t, err)

		hamFile, err := os.Create(filepath.Join(tmpDir, "ham_samples.txt"))
		require.NoError(t, err)

		excludedTokensFile := filepath.Join(tmpDir, "excluded_tokens.txt")
		spamSamplesFile := filepath.Join(tmpDir, "spam_samples.txt")
		hamSamplesFile := filepath.Join(tmpDir, "ham_samples.txt")
		stopWordsFile := filepath.Join(tmpDir, "stop_words.txt")

		fh, err := os.Create(excludedTokensFile)
		require.NoError(t, err)
		fh.Close()

		fh, err = os.Create(spamSamplesFile)
		require.NoError(t, err)
		fh.Close()
		fh, err = os.Create(hamSamplesFile)
		require.NoError(t, err)
		fh.Close()
		fh, err = os.Create(stopWordsFile)
		require.NoError(t, err)
		fh.Close()

		_, err = spamFile.WriteString("spam1\nspam2\nspam3\nspam3\n")
		require.NoError(t, err)
		_, err = hamFile.WriteString("ham1\nham2\n")
		require.NoError(t, err)

		return NewSpamFilter(context.Background(), mockDirector, SpamConfig{
				SpamDynamicFile:    spamFile.Name(),
				HamDynamicFile:     hamFile.Name(),
				SpamSamplesFile:    spamSamplesFile,
				HamSamplesFile:     hamSamplesFile,
				StopWordsFile:      stopWordsFile,
				ExcludedTokensFile: excludedTokensFile,
			}), func() {
				os.RemoveAll(tmpDir)
				os.Remove(spamFile.Name())
				os.Remove(hamFile.Name())
			}
	}

	t.Run("remove from spam", func(t *testing.T) {
		mockDirector.ResetCalls()
		sf, teardown := prep()
		defer teardown()
		count, err := sf.RemoveDynamicSpamSample("spam1")
		require.NoError(t, err)
		assert.Equal(t, 1, count)
		spam, ham, err := sf.DynamicSamples()
		require.NoError(t, err)
		assert.Equal(t, []string{"spam2", "spam3", "spam3"}, spam)
		assert.Equal(t, []string{"ham1", "ham2"}, ham)
		assert.True(t, len(mockDirector.LoadSamplesCalls()) >= 1, "LoadSamples should be called at least once")
	})

	t.Run("remove multi from spam", func(t *testing.T) {
		sf, teardown := prep()
		defer teardown()

		count, err := sf.RemoveDynamicSpamSample("spam3")
		require.NoError(t, err)
		assert.Equal(t, 2, count)
		spam, ham, err := sf.DynamicSamples()
		require.NoError(t, err)
		assert.Equal(t, []string{"spam1", "spam2"}, spam)
		assert.Equal(t, []string{"ham1", "ham2"}, ham)
	})

	t.Run("remove from ham", func(t *testing.T) {
		sf, teardown := prep()
		defer teardown()

		count, err := sf.RemoveDynamicHamSample("ham2")
		require.NoError(t, err)
		assert.Equal(t, 1, count)
		spam, ham, err := sf.DynamicSamples()
		require.NoError(t, err)
		assert.Equal(t, []string{"spam1", "spam2", "spam3", "spam3"}, spam)
		assert.Equal(t, []string{"ham1"}, ham)
	})

	t.Run("remove from ham, not found", func(t *testing.T) {
		sf, teardown := prep()
		defer teardown()

		count, err := sf.RemoveDynamicHamSample("ham2222")
		assert.ErrorContains(t, err, "failed to remove dynamic ham sample: ")
		assert.Equal(t, 0, count)
		spam, ham, err := sf.DynamicSamples()
		require.NoError(t, err)
		assert.Equal(t, []string{"spam1", "spam2", "spam3", "spam3"}, spam)
		assert.Equal(t, []string{"ham1", "ham2"}, ham)
	})
}

func TestSpamFilter_RemoveDynamicSampleReal(t *testing.T) {
	mockDirector := &mocks.DetectorMock{
		LoadSamplesFunc: func(exclReader io.Reader, spamReaders []io.Reader, hamReaders []io.Reader) (tgspam.LoadResult, error) {
			return tgspam.LoadResult{}, nil
		},
		LoadStopWordsFunc: func(readers ...io.Reader) (tgspam.LoadResult, error) {
			return tgspam.LoadResult{}, nil
		},
	}

	// make a temp file from testdata/spam.txt
	tmpFile, err := os.CreateTemp("", "spam")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())
	input, err := os.ReadFile("testdata/spam.txt")
	require.NoError(t, err)
	_, err = tmpFile.Write(input)
	require.NoError(t, err)

	tmpDir := filepath.Dir(tmpFile.Name())
	spamFile, err := os.Create(filepath.Join(tmpDir, "spam_samples.txt"))
	require.NoError(t, err)
	hamFile, err := os.Create(filepath.Join(tmpDir, "ham_samples.txt"))
	require.NoError(t, err)
	excludedTokensFile := filepath.Join(tmpDir, "excluded_tokens.txt")
	hamSamplesFile := filepath.Join(tmpDir, "ham_samples.txt")
	stopWordsFile := filepath.Join(tmpDir, "stop_words.txt")

	sf := NewSpamFilter(context.Background(), mockDirector, SpamConfig{
		SpamDynamicFile:    tmpFile.Name(),
		HamDynamicFile:     hamFile.Name(),
		SpamSamplesFile:    spamFile.Name(),
		HamSamplesFile:     hamSamplesFile,
		StopWordsFile:      stopWordsFile,
		ExcludedTokensFile: excludedTokensFile,
	})
	spam, _, err := sf.DynamicSamples()
	require.NoError(t, err)
	assert.Contains(t, spam, "Здрαвствуйте, ищем ответственного человеκα для удαленной рαботы в новый проеκт!")

	count, err := sf.RemoveDynamicSpamSample("Здрαвствуйте, ищем ответственного человеκα для удαленной рαботы в новый проеκт!")
	require.NoError(t, err)
	assert.Equal(t, 1, count, "should remove one sample")

	spam, _, err = sf.DynamicSamples()
	require.NoError(t, err)
	assert.NotContains(t, spam, "Здрαвствуйте, ищем ответственного человеκα для удαленной рαботы в новый проеκт!")
}
