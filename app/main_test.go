package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestMakeSpamLogger(t *testing.T) {
	file, err := os.CreateTemp(os.TempDir(), "log")
	require.NoError(t, err)
	defer os.Remove(file.Name())

	db, err := storage.NewSqliteDB(":memory:")
	require.NoError(t, err)
	defer db.Close()

	logger, err := makeSpamLogger(context.Background(), "gr1", file, db)
	require.NoError(t, err)

	msg := &bot.Message{
		From: bot.User{
			ID:          123,
			DisplayName: "Test User",
			Username:    "testuser",
		},
		Text: "Test message\nblah blah  \n\n\n",
	}

	response := &bot.Response{
		Text: "spam detected",
		CheckResults: []spamcheck.Response{
			{Name: "Check1", Spam: true, Details: "Details 1"},
			{Name: "Check2", Spam: false, Details: "Details 2"},
		},
	}

	logger.Save(msg, response)
	file.Close()

	// check that the message is saved to the log file
	file, err = os.Open(file.Name())
	require.NoError(t, err)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		t.Log(line)

		var logEntry map[string]interface{}
		err = json.Unmarshal([]byte(line), &logEntry)
		require.NoError(t, err)

		assert.Equal(t, "Test User", logEntry["display_name"])
		assert.Equal(t, "testuser", logEntry["user_name"])
		assert.Equal(t, float64(123), logEntry["user_id"]) // json.Unmarshal converts numbers to float64
		assert.Equal(t, "Test message blah blah", logEntry["text"])
	}
	assert.NoError(t, scanner.Err())

	// check that the message is saved to the database
	savedMsgs := []storage.DetectedSpamInfo{}
	err = db.Select(&savedMsgs, "SELECT text, user_id, user_name, timestamp, checks FROM detected_spam")
	require.NoError(t, err)
	assert.Equal(t, 1, len(savedMsgs))
	assert.Equal(t, "Test message blah blah", savedMsgs[0].Text)
	assert.Equal(t, "testuser", savedMsgs[0].UserName)
	assert.Equal(t, int64(123), savedMsgs[0].UserID)
	assert.Equal(t, `[{"name":"Check1","spam":true,"details":"Details 1"},{"name":"Check2","spam":false,"details":"Details 2"}]`,
		savedMsgs[0].ChecksJSON)

}

func TestMakeSpamLogWriter(t *testing.T) {
	setupLog(true, "super-secret-token")
	t.Run("happy path", func(t *testing.T) {
		file, err := os.CreateTemp(os.TempDir(), "log")
		require.NoError(t, err)
		defer os.Remove(file.Name())

		var opts options
		opts.Logger.Enabled = true
		opts.Logger.FileName = file.Name()
		opts.Logger.MaxSize = "1M"
		opts.Logger.MaxBackups = 1

		writer, err := makeSpamLogWriter(opts)
		require.NoError(t, err)

		_, err = writer.Write([]byte("Test log entry\n"))
		assert.NoError(t, err)
		err = writer.Close()
		assert.NoError(t, err)

		file, err = os.Open(file.Name())
		require.NoError(t, err)

		content, err := io.ReadAll(file)
		assert.NoError(t, err)
		assert.Equal(t, "Test log entry\n", string(content))
	})

	t.Run("failed on wrong size", func(t *testing.T) {
		var opts options
		opts.Logger.Enabled = true
		opts.Logger.FileName = "/tmp"
		opts.Logger.MaxSize = "1f"
		opts.Logger.MaxBackups = 1
		writer, err := makeSpamLogWriter(opts)
		assert.Error(t, err)
		t.Log(err)
		assert.Nil(t, writer)
	})

	t.Run("disabled", func(t *testing.T) {
		var opts options
		opts.Logger.Enabled = false
		opts.Logger.FileName = "/tmp"
		opts.Logger.MaxSize = "10M"
		opts.Logger.MaxBackups = 1
		writer, err := makeSpamLogWriter(opts)
		assert.NoError(t, err)
		assert.IsType(t, nopWriteCloser{}, writer)
	})
}

func Test_makeDetector(t *testing.T) {
	t.Run("no options", func(t *testing.T) {
		var opts options
		res := makeDetector(opts)
		assert.NotNil(t, res)
	})

	t.Run("with first msgs count", func(t *testing.T) {
		var opts options
		opts.OpenAI.Token = "123"
		opts.Files.SamplesDataPath = "/tmp"
		opts.Files.DynamicDataPath = "/tmp"
		opts.FirstMessagesCount = 10
		res := makeDetector(opts)
		assert.NotNil(t, res)
		assert.Equal(t, 10, res.FirstMessagesCount)
		assert.Equal(t, true, res.FirstMessageOnly)
	})

	t.Run("with first msgs count and paranoid", func(t *testing.T) {
		var opts options
		opts.OpenAI.Token = "123"
		opts.Files.SamplesDataPath = "/tmp"
		opts.Files.DynamicDataPath = "/tmp"
		opts.FirstMessagesCount = 10
		opts.ParanoidMode = true
		res := makeDetector(opts)
		assert.NotNil(t, res)
		assert.Equal(t, 0, res.FirstMessagesCount)
		assert.Equal(t, false, res.FirstMessageOnly)
	})
}

func Test_makeSpamBot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Run("no options", func(t *testing.T) {
		var opts options
		_, err := makeSpamBot(ctx, opts, nil, nil)
		assert.Error(t, err)
	})

	t.Run("with valid options", func(t *testing.T) {
		var opts options
		tmpDir := t.TempDir()

		opts.Files.SamplesDataPath = tmpDir
		opts.InstanceID = "gr1"
		detector := makeDetector(opts)
		db, err := storage.NewSqliteDB("tg-spam.db")
		require.NoError(t, err)
		defer db.Close()

		samplesStore, err := storage.NewSamples(ctx, db)
		require.NoError(t, err)
		err = samplesStore.Add(ctx, "gr1", storage.SampleTypeSpam, storage.SampleOriginPreset, "spam1")
		require.NoError(t, err)
		err = samplesStore.Add(ctx, "gr1", storage.SampleTypeHam, storage.SampleOriginPreset, "ham1")
		require.NoError(t, err)

		res, err := makeSpamBot(ctx, opts, db, detector)
		assert.NoError(t, err)
		assert.NotNil(t, res)
	})
}

func Test_activateServerOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var opts options
	opts.Server.Enabled = true
	opts.Server.ListenAddr = ":9988"
	opts.Server.AuthPasswd = "auto"
	opts.InstanceID = "gr1"

	opts.Files.SamplesDataPath = t.TempDir()
	db, err := storage.NewSqliteDB("tg-spam.db")
	require.NoError(t, err)
	defer db.Close()

	samplesStore, err := storage.NewSamples(ctx, db)
	require.NoError(t, err)
	err = samplesStore.Add(ctx, "tg-spam", storage.SampleTypeSpam, storage.SampleOriginPreset, "spam1")
	require.NoError(t, err)
	err = samplesStore.Add(ctx, "tg-spam", storage.SampleTypeHam, storage.SampleOriginPreset, "ham1")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		err := execute(ctx, opts)
		assert.NoError(t, err)
		close(done)
	}()

	// Wait for server to be ready
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://localhost:9988/ping")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, time.Second*2, time.Millisecond*50, "server did not start")

	resp, err := http.Get("http://localhost:9988/ping")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "pong", string(body))
	cancel()
	<-done
}

func Test_checkVolumeMount(t *testing.T) {
	prepEnvAndFileSystem := func(opts *options, envValue string, dynamicDataPath string, notMountedExists bool) func() {
		os.Setenv("TGSPAM_IN_DOCKER", envValue)

		tempDir, _ := os.MkdirTemp("", "test")
		if dynamicDataPath != "" {
			os.MkdirAll(filepath.Join(tempDir, dynamicDataPath), os.ModePerm)
		}

		if notMountedExists {
			os.WriteFile(filepath.Join(tempDir, dynamicDataPath, ".not_mounted"), []byte{}, 0o644)
		}

		if dynamicDataPath == "" {
			dynamicDataPath = "dynamic"
		}
		opts.Files.DynamicDataPath = filepath.Join(tempDir, dynamicDataPath)

		return func() {
			os.RemoveAll(tempDir)
		}
	}

	tests := []struct {
		name             string
		envValue         string
		dynamicDataPath  string
		notMountedExists bool
		expectedOk       bool
	}{
		{
			name:            "not in docker",
			envValue:        "0",
			dynamicDataPath: "",
			expectedOk:      true,
		},
		{
			name:             "in Docker, path mounted, no .not_mounted",
			envValue:         "1",
			dynamicDataPath:  "dynamic",
			notMountedExists: false,
			expectedOk:       true,
		},
		{
			name:             "in docker, .not_mounted exists",
			envValue:         "1",
			dynamicDataPath:  "dynamic",
			notMountedExists: true,
			expectedOk:       false,
		},
		{
			name:             "not in docker, .not_mounted exists",
			envValue:         "0",
			dynamicDataPath:  "dynamic",
			notMountedExists: true,
			expectedOk:       true,
		},
		{
			name:             "in docker, path not mounted",
			envValue:         "1",
			dynamicDataPath:  "",
			notMountedExists: false,
			expectedOk:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := options{}
			cleanup := prepEnvAndFileSystem(&opts, tt.envValue, tt.dynamicDataPath, tt.notMountedExists)
			defer cleanup()

			ok := checkVolumeMount(opts)
			assert.Equal(t, tt.expectedOk, ok)
		})
	}
}

func Test_expandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	currentDir, err := os.Getwd()
	require.NoError(t, err)

	tests := []struct {
		name string
		path string
		want string
	}{
		{"Empty Path", "", ""},
		{"Home Directory", "~", home},
		{"Relative Path", ".", ""},
		{"Relative Path with directory", "data", filepath.Join(currentDir, "data")},
		{"Absolute Path", "/tmp", "/tmp"},
		{"Path with Tilde and Subdirectory", "~/Documents", filepath.Join(home, "Documents")},
		{"Path with Multiple Relative Directories", "../parent/child", ""},
		{"Path with Special Characters", "data/special @#$/file", ""},
		{"Invalid Path", "/some/nonexistent/path", "/some/nonexistent/path"},
		{"Home Directory with Trailing Slash", "~/", home},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := expandPath(tt.path)

			switch {
			case strings.Contains(tt.path, "~"):
				assert.Equal(t, filepath.Join(home, tt.path[1:]), got)
			case tt.path == ".", strings.HasPrefix(tt.path, ".."), strings.Contains(tt.path, "/"):
				// For relative paths, paths starting with "..", and paths with special characters
				expected, err := filepath.Abs(tt.path)
				require.NoError(t, err)
				assert.Equal(t, expected, got)
			default:
				// For absolute paths and invalid paths
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func Test_migrateSamples(t *testing.T) {
	tmpDir := t.TempDir()
	opts := options{}
	opts.Files.SamplesDataPath, opts.Files.DynamicDataPath = tmpDir, tmpDir
	opts.InstanceID = "gr1"

	t.Run("full migration", func(t *testing.T) {
		db, err := storage.NewSqliteDB(":memory:")
		require.NoError(t, err)
		defer db.Close()
		store, err := storage.NewSamples(context.Background(), db)
		require.NoError(t, err)

		// create new files for migration, all 4 files should be migrated
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, samplesSpamFile),
			[]byte("new spam1\nnew spam2\nnew spam 3"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.DynamicDataPath, samplesHamFile),
			[]byte("new ham1\nnew ham2"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, dynamicSpamFile),
			[]byte("new dspam1\nnew dspam2\nnew dspam3"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.DynamicDataPath, dynamicHamFile),
			[]byte("new dham1\nnew dham2"), 0o600))

		err = migrateSamples(context.Background(), opts, store)
		assert.NoError(t, err)

		// verify all files migrated
		_, err = os.Stat(filepath.Join(opts.Files.SamplesDataPath, samplesSpamFile))
		assert.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(opts.Files.DynamicDataPath, samplesHamFile))
		assert.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(opts.Files.SamplesDataPath, dynamicSpamFile))
		assert.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(opts.Files.DynamicDataPath, dynamicHamFile))
		assert.Error(t, err, "original file should be renamed")

		s, err := store.Stats(context.Background(), "gr1")
		require.NoError(t, err)
		assert.Equal(t, 6, s.TotalSpam)
		assert.Equal(t, 4, s.TotalHam)

		res, err := store.Read(context.Background(), "gr1", storage.SampleTypeSpam, storage.SampleOriginUser)
		require.NoError(t, err)
		assert.Equal(t, 3, len(res))
		assert.Equal(t, "new dspam1", res[0])
		assert.Equal(t, "new dspam2", res[1])
		assert.Equal(t, "new dspam3", res[2])
	})

	t.Run("nil storage", func(t *testing.T) {
		err := migrateSamples(context.Background(), opts, nil)
		assert.Error(t, err)
	})

	t.Run("already migrated", func(t *testing.T) {
		db, err := storage.NewSqliteDB(":memory:")
		require.NoError(t, err)
		defer db.Close()
		store, err := storage.NewSamples(context.Background(), db)
		require.NoError(t, err)

		// create already loaded files
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, samplesSpamFile+".loaded"),
			[]byte("old spam"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.DynamicDataPath, dynamicHamFile+".loaded"),
			[]byte("old ham"), 0o600))

		err = migrateSamples(context.Background(), opts, store)
		assert.NoError(t, err)

		// verify old files untouched
		data, err := os.ReadFile(filepath.Join(opts.Files.SamplesDataPath, samplesSpamFile+".loaded"))
		require.NoError(t, err)
		assert.Equal(t, "old spam", string(data))

		// verify new files migrated
		_, err = os.Stat(filepath.Join(opts.Files.SamplesDataPath, samplesSpamFile))
		assert.Error(t, err, "original file should be renamed")
	})

	t.Run("partial migration", func(t *testing.T) {
		db, err := storage.NewSqliteDB(":memory:")
		require.NoError(t, err)
		defer db.Close()
		store, err := storage.NewSamples(context.Background(), db)
		require.NoError(t, err)

		// create mix of loaded and unloaded files
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, samplesSpamFile+".loaded"), []byte("old spam"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.DynamicDataPath, dynamicHamFile), []byte("new ham"), 0o600))

		err = migrateSamples(context.Background(), opts, store)
		assert.NoError(t, err)

		// verify only unloaded files migrated
		_, err = os.Stat(filepath.Join(opts.Files.DynamicDataPath, dynamicHamFile))
		assert.Error(t, err, "unloaded file should be renamed")
		_, err = os.Stat(filepath.Join(opts.Files.DynamicDataPath, dynamicHamFile+".loaded"))
		assert.NoError(t, err)

		s, err := store.Stats(context.Background(), "gr1")
		require.NoError(t, err)
		assert.Equal(t, 0, s.TotalSpam)
		assert.Equal(t, 1, s.TotalHam)
	})

	t.Run("empty files", func(t *testing.T) {
		db, err := storage.NewSqliteDB(":memory:")
		require.NoError(t, err)
		defer db.Close()
		store, err := storage.NewSamples(context.Background(), db)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, samplesSpamFile), []byte(""), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.DynamicDataPath, dynamicHamFile), []byte(""), 0600))

		err = migrateSamples(context.Background(), opts, store)
		assert.NoError(t, err)
	})
}

func Test_migrateDicts(t *testing.T) {
	tmpDir := t.TempDir()
	opts := options{}
	opts.Files.SamplesDataPath = tmpDir
	opts.InstanceID = "gr1"

	t.Run("nil dictionary", func(t *testing.T) {
		err := migrateDicts(context.Background(), opts, nil)
		assert.Error(t, err)
	})

	t.Run("full migration", func(t *testing.T) {
		db, err := storage.NewSqliteDB(":memory:")
		require.NoError(t, err)
		defer db.Close()
		dict, err := storage.NewDictionary(context.Background(), db)
		require.NoError(t, err)

		// create new files for migration
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, stopWordsFile),
			[]byte("stop1\nstop2\nstop3"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, excludeTokensFile),
			[]byte("token1\ntoken2"), 0600))

		err = migrateDicts(context.Background(), opts, dict)
		assert.NoError(t, err)

		// verify files renamed and moved correctly
		_, err = os.Stat(filepath.Join(opts.Files.SamplesDataPath, stopWordsFile))
		assert.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(opts.Files.SamplesDataPath, stopWordsFile+".loaded"))
		assert.NoError(t, err)

		_, err = os.Stat(filepath.Join(opts.Files.SamplesDataPath, excludeTokensFile))
		assert.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(opts.Files.SamplesDataPath, excludeTokensFile+".loaded"))
		assert.NoError(t, err)

		// verify data imported correctly
		s, err := dict.Stats(context.Background(), "gr1")
		require.NoError(t, err)
		assert.Equal(t, 3, s.TotalStopPhrases)
		assert.Equal(t, 2, s.TotalIgnoredWords)
	})

	t.Run("already migrated", func(t *testing.T) {
		db, err := storage.NewSqliteDB(":memory:")
		require.NoError(t, err)
		defer db.Close()
		dict, err := storage.NewDictionary(context.Background(), db)
		require.NoError(t, err)

		// create already loaded files
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, stopWordsFile+".loaded"),
			[]byte("old stop1\nold stop2"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, excludeTokensFile+".loaded"),
			[]byte("old token1"), 0600))

		// create new files
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, stopWordsFile),
			[]byte("new stop1\nnew stop2"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, excludeTokensFile),
			[]byte("new token1\nnew token2"), 0600))

		err = migrateDicts(context.Background(), opts, dict)
		assert.NoError(t, err)

		// verify import happened correctly
		s, err := dict.Stats(context.Background(), "gr1")
		require.NoError(t, err)
		assert.Equal(t, 2, s.TotalStopPhrases)
		assert.Equal(t, 2, s.TotalIgnoredWords)

		// verify old files overwritten
		data, err := os.ReadFile(filepath.Join(opts.Files.SamplesDataPath, stopWordsFile+".loaded"))
		require.NoError(t, err)
		assert.Equal(t, "new stop1\nnew stop2", string(data))
	})

	t.Run("empty files", func(t *testing.T) {
		db, err := storage.NewSqliteDB(":memory:")
		require.NoError(t, err)
		defer db.Close()
		dict, err := storage.NewDictionary(context.Background(), db)
		require.NoError(t, err)

		// create empty files
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, stopWordsFile), []byte(""), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, excludeTokensFile), []byte(""), 0600))

		err = migrateDicts(context.Background(), opts, dict)
		assert.NoError(t, err)

		// verify stats
		s, err := dict.Stats(context.Background(), "gr1")
		require.NoError(t, err)
		assert.Equal(t, 0, s.TotalStopPhrases)
		assert.Equal(t, 0, s.TotalIgnoredWords)
	})

	t.Run("partial migration", func(t *testing.T) {
		db, err := storage.NewSqliteDB(":memory:")
		require.NoError(t, err)
		defer db.Close()
		dict, err := storage.NewDictionary(context.Background(), db)
		require.NoError(t, err)

		// create mix of loaded and unloaded files
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, stopWordsFile+".loaded"),
			[]byte("old stop1\nold stop2"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(opts.Files.SamplesDataPath, excludeTokensFile),
			[]byte("token1\ntoken2"), 0600))

		err = migrateDicts(context.Background(), opts, dict)
		assert.NoError(t, err)

		// verify only unloaded file migrated
		_, err = os.Stat(filepath.Join(opts.Files.SamplesDataPath, excludeTokensFile))
		assert.Error(t, err, "unloaded file should be renamed")
		_, err = os.Stat(filepath.Join(opts.Files.SamplesDataPath, excludeTokensFile+".loaded"))
		assert.NoError(t, err)

		// verify stats reflect only migrated data
		s, err := dict.Stats(context.Background(), "gr1")
		require.NoError(t, err)
		assert.Equal(t, 0, s.TotalStopPhrases)
		assert.Equal(t, 2, s.TotalIgnoredWords)
	})
}
