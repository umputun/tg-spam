package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestMakeSpamLogger(t *testing.T) {
	file, err := os.CreateTemp(os.TempDir(), "log")
	require.NoError(t, err)
	defer os.Remove(file.Name())

	db, err := engine.NewSqlite(":memory:", "gr1")
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

func Test_initLuaPlugins(t *testing.T) {
	t.Run("basic plugin initialization", func(t *testing.T) {
		var opts options
		opts.LuaPlugins.Enabled = true
		opts.LuaPlugins.PluginsDir = "/path/to/plugins"
		opts.LuaPlugins.EnabledPlugins = []string{"plugin1", "plugin2"}
		opts.LuaPlugins.DynamicReload = true

		detector := makeDetector(options{}) // create a clean detector

		// run the function to test
		initLuaPlugins(detector, opts)

		// verify that the detector's config matches the opts
		assert.True(t, detector.LuaPlugins.Enabled)
		assert.Equal(t, "/path/to/plugins", detector.LuaPlugins.PluginsDir)
		assert.Equal(t, []string{"plugin1", "plugin2"}, detector.LuaPlugins.EnabledPlugins)
		assert.True(t, detector.LuaPlugins.DynamicReload)

		// verify the Lua engine was initialized
		// we can't directly check detector.luaEngine since it's unexported
		// but we can infer it's initialized because the settings were applied
	})

	t.Run("all enabled plugins", func(t *testing.T) {
		var opts options
		opts.LuaPlugins.Enabled = true
		opts.LuaPlugins.PluginsDir = "/path/to/plugins"
		// no specific plugins enabled - should enable all
		opts.LuaPlugins.DynamicReload = false

		detector := makeDetector(options{}) // create a clean detector

		// run the function to test
		initLuaPlugins(detector, opts)

		// verify the settings were transferred
		assert.True(t, detector.LuaPlugins.Enabled)
		assert.Equal(t, "/path/to/plugins", detector.LuaPlugins.PluginsDir)
		assert.Empty(t, detector.LuaPlugins.EnabledPlugins)
		assert.False(t, detector.LuaPlugins.DynamicReload)
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
		opts.Files.DynamicDataPath = tmpDir
		opts.InstanceID = "gr1"
		detector := makeDetector(opts)
		db, err := engine.NewSqlite(path.Join(tmpDir, "tg-spam.db"), "gr1")
		require.NoError(t, err)
		defer db.Close()

		samplesStore, err := storage.NewSamples(ctx, db)
		require.NoError(t, err)
		err = samplesStore.Add(ctx, storage.SampleTypeSpam, storage.SampleOriginPreset, "spam1")
		require.NoError(t, err)
		err = samplesStore.Add(ctx, storage.SampleTypeHam, storage.SampleOriginPreset, "ham1")
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
	opts.DataBaseURL = fmt.Sprintf("sqlite://%s", path.Join(t.TempDir(), "tg-spam.db"))

	opts.Files.SamplesDataPath, opts.Files.DynamicDataPath = t.TempDir(), t.TempDir()

	// write some sample files
	fh, err := os.Create(path.Join(opts.Files.SamplesDataPath, "spam-samples.txt"))
	require.NoError(t, err)
	_, err = fh.WriteString("spam1\nspam2\nspam3\n")
	require.NoError(t, err)
	fh.Close()

	fh, err = os.Create(path.Join(opts.Files.SamplesDataPath, "ham-samples.txt"))
	require.NoError(t, err)
	_, err = fh.WriteString("ham1\nham2\nham3\n")
	require.NoError(t, err)
	fh.Close()

	done := make(chan struct{})
	go func() {
		err := execute(ctx, opts)
		assert.NoError(t, err)
		close(done)
	}()

	// wait for server to be ready
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://localhost:9988/ping")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, time.Second*5, time.Millisecond*100, "server did not start")

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
				// for relative paths, paths starting with "..", and paths with special characters
				expected, err := filepath.Abs(tt.path)
				require.NoError(t, err)
				assert.Equal(t, expected, got)
			default:
				// for absolute paths and invalid paths
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
		db, err := engine.NewSqlite(":memory:", "gr1")
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

		s, err := store.Stats(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 6, s.TotalSpam)
		assert.Equal(t, 4, s.TotalHam)

		res, err := store.Read(context.Background(), storage.SampleTypeSpam, storage.SampleOriginUser)
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
		db, err := engine.NewSqlite(":memory:", "gr1")
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
		db, err := engine.NewSqlite(":memory:", "gr1")
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

		s, err := store.Stats(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0, s.TotalSpam)
		assert.Equal(t, 1, s.TotalHam)
	})

	t.Run("empty files", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
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
		db, err := engine.NewSqlite(":memory:", "gr1")
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
		s, err := dict.Stats(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 3, s.TotalStopPhrases)
		assert.Equal(t, 2, s.TotalIgnoredWords)
	})

	t.Run("already migrated", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
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
		s, err := dict.Stats(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 2, s.TotalStopPhrases)
		assert.Equal(t, 2, s.TotalIgnoredWords)

		// verify old files overwritten
		data, err := os.ReadFile(filepath.Join(opts.Files.SamplesDataPath, stopWordsFile+".loaded"))
		require.NoError(t, err)
		assert.Equal(t, "new stop1\nnew stop2", string(data))
	})

	t.Run("empty files", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
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
		s, err := dict.Stats(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0, s.TotalStopPhrases)
		assert.Equal(t, 0, s.TotalIgnoredWords)
	})

	t.Run("partial migration", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
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
		s, err := dict.Stats(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 0, s.TotalStopPhrases)
		assert.Equal(t, 2, s.TotalIgnoredWords)
	})
}

func TestBackupDB(t *testing.T) {
	// helper functions
	fileSize := func(t *testing.T, path string) int64 {
		t.Helper()
		info, err := os.Stat(path)
		require.NoError(t, err)
		return info.Size()
	}

	readFile := func(t *testing.T, path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		return string(data)
	}

	t.Run("no backup if max 0", func(t *testing.T) {
		dir := t.TempDir()
		dbFile := filepath.Join(dir, "test.db")
		require.NoError(t, os.WriteFile(dbFile, []byte("test data"), 0600))

		err := backupDB(dbFile, "v1", 0)
		require.NoError(t, err)
		files, err := filepath.Glob(dbFile + ".*")
		require.NoError(t, err)
		require.Empty(t, files)
	})

	t.Run("skip existing backup", func(t *testing.T) {
		dir := t.TempDir()
		dbFile := filepath.Join(dir, "test.db")
		require.NoError(t, os.WriteFile(dbFile, []byte("test data"), 0600))

		backupFile := dbFile + ".master-123-20250108T00:01:26"
		require.NoError(t, os.WriteFile(backupFile, []byte("old backup"), 0600))
		origSize := fileSize(t, backupFile)

		err := backupDB(dbFile, "master-123-20250108T00:01:26", 1)
		require.NoError(t, err)

		newSize := fileSize(t, backupFile)
		require.Equal(t, origSize, newSize, "backup file should not be modified")
	})

	t.Run("make new backup and cleanup", func(t *testing.T) {
		dir := t.TempDir()
		dbFile := filepath.Join(dir, "test.db")
		require.NoError(t, os.WriteFile(dbFile, []byte("test data"), 0600))

		// create some old backups
		oldBackups := []string{
			"master-111-20250108T00:01:26",
			"master-222-20250108T00:02:26",
			"master-333-20250108T00:03:26",
		}
		for _, v := range oldBackups {
			require.NoError(t, os.WriteFile(dbFile+"."+v, []byte("old"), 0600))
		}

		// make new backup with maxBackups=2
		newVer := "master-444-20250108T00:04:26"
		err := backupDB(dbFile, newVer, 2)
		require.NoError(t, err)

		// check files
		files, err := filepath.Glob(dbFile + ".*")
		require.NoError(t, err)
		require.Len(t, files, 2)

		// verify correct files remain (2 newest)
		sort.Strings(files) // sort for stable test
		for _, f := range files {
			base := filepath.Base(f)
			require.True(t, strings.HasSuffix(base, oldBackups[2]) || strings.HasSuffix(base, newVer),
				"unexpected file: %s", base)
		}

		content := readFile(t, dbFile+"."+newVer)
		require.Equal(t, "test data", string(content))
	})

	t.Run("mixed_formats", func(t *testing.T) {
		dir := t.TempDir()
		dbFile := filepath.Join(dir, "test.db")
		require.NoError(t, os.WriteFile(dbFile, []byte("test data"), 0600))

		// make older files with version suffix
		require.NoError(t, os.WriteFile(dbFile+".master-aaa-20250101T12:00:00", []byte("1"), 0600))
		require.NoError(t, os.WriteFile(dbFile+".master-bbb-20250101T13:00:00", []byte("2"), 0600))

		// make normal files dated between versioned ones
		testTime := time.Date(2025, 1, 1, 12, 30, 0, 0, time.Local)
		require.NoError(t, os.WriteFile(dbFile+".backup1", []byte("3"), 0600))
		require.NoError(t, os.Chtimes(dbFile+".backup1", testTime, testTime))

		// make new backup, should keep only 3 newest files
		err := backupDB(dbFile, "master-ccc-20250101T14:00:00", 3)
		require.NoError(t, err)

		// check remaining files
		files, err := filepath.Glob(dbFile + ".*")
		require.NoError(t, err)
		require.Len(t, files, 3)

		// verify we have the three newest files by checking their names
		foundFiles := make(map[string]bool)
		for _, f := range files {
			foundFiles[filepath.Base(f)] = true
			t.Logf("found file: %s", filepath.Base(f))
		}

		require.True(t, foundFiles["test.db.master-ccc-20250101T14:00:00"], "newest versioned backup")
		require.True(t, foundFiles["test.db.master-bbb-20250101T13:00:00"], "middle versioned backup")
		require.True(t, foundFiles["test.db.backup1"], "normal backup with mod time in between")

		// and oldest versioned backup should be removed
		_, err = os.Stat(dbFile + ".master-aaa-20250101T12:00:00")
		require.True(t, os.IsNotExist(err), "oldest versioned file should be gone")
	})

	t.Run("version with dots", func(t *testing.T) {
		dir := t.TempDir()
		dbFile := filepath.Join(dir, "test.db")
		require.NoError(t, os.WriteFile(dbFile, []byte("test data"), 0600))

		version := "master-123-1.2.3-20250108T00:01:26"
		err := backupDB(dbFile, version, 1)
		require.NoError(t, err)

		expectedBackup := dbFile + "." + strings.ReplaceAll(version, ".", "_")
		_, err = os.Stat(expectedBackup)
		require.NoError(t, err)

		content := readFile(t, expectedBackup)
		require.Equal(t, "test data", string(content))

		require.Contains(t, expectedBackup, "master-123-1_2_3-20250108T00:01:26")
		require.NotContains(t, expectedBackup, "master-123-1.2.3-20250108T00:01:26")
	})

	t.Run("backup with no db file", func(t *testing.T) {
		dir := t.TempDir()
		nonExistentFile := filepath.Join(dir, "non-existent.db")
		err := backupDB(nonExistentFile, "v1", 1)
		require.NoError(t, err)
	})
}

func TestSaveAndLoadConfig(t *testing.T) {
	// setup test environment
	setupLog(true)

	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "config-test.db")

	t.Run("save and load config", func(t *testing.T) {
		// create test options with some values
		opts := options{
			InstanceID:      "test-instance",
			DataBaseURL:     dbFile,
			ConfigDB:        false,
			Dry:             true,
			SoftBan:         true,
			MinMsgLen:       100,
			MaxEmoji:        5,
			HistorySize:     200,
			ParanoidMode:    true,
			Training:        true,
			MultiLangWords:  3,
			StorageTimeout:  30 * time.Second,
			HistoryDuration: 24 * time.Hour,
			HistoryMinSize:  500,
		}

		// fill nested struct fields
		opts.Telegram.Token = "test-token"
		opts.Telegram.Group = "test-group"
		opts.Telegram.Timeout = 5 * time.Second
		opts.Telegram.IdleDuration = 10 * time.Second

		// test saving config to DB
		ctx := context.Background()
		err := saveConfigToDB(ctx, &opts)
		require.NoError(t, err)

		// create a new options struct to load into
		loadedOpts := options{
			DataBaseURL: dbFile,
			ConfigDB:    true,
			InstanceID:  "test-instance",
		}

		// test loading config from DB
		err = loadConfigFromDB(ctx, &loadedOpts)
		require.NoError(t, err)

		// verify loaded values match original (except sensitive fields that should be cleared)
		assert.Equal(t, opts.InstanceID, loadedOpts.InstanceID)
		assert.Equal(t, opts.Dry, loadedOpts.Dry)
		assert.Equal(t, opts.SoftBan, loadedOpts.SoftBan)
		assert.Equal(t, opts.MinMsgLen, loadedOpts.MinMsgLen)
		assert.Equal(t, opts.MaxEmoji, loadedOpts.MaxEmoji)
		assert.Equal(t, opts.HistorySize, loadedOpts.HistorySize)
		assert.Equal(t, opts.ParanoidMode, loadedOpts.ParanoidMode)
		assert.Equal(t, opts.Training, loadedOpts.Training)
		assert.Equal(t, opts.MultiLangWords, loadedOpts.MultiLangWords)
		assert.Equal(t, opts.HistoryDuration, loadedOpts.HistoryDuration)
		assert.Equal(t, opts.HistoryMinSize, loadedOpts.HistoryMinSize)

		// group-related fields
		assert.Equal(t, opts.Telegram.Group, loadedOpts.Telegram.Group)
		assert.Equal(t, opts.Telegram.Timeout, loadedOpts.Telegram.Timeout)
		assert.Equal(t, opts.Telegram.IdleDuration, loadedOpts.Telegram.IdleDuration)

		// verify sensitive fields are properly stored
		assert.Equal(t, "test-token", loadedOpts.Telegram.Token)
	})

	t.Run("verify tokens are stored with different values", func(t *testing.T) {
		// create test options with different token values
		opts := options{
			InstanceID:  "test-instance",
			DataBaseURL: dbFile,
			ConfigDB:    false,
			Dbg:         true, // debug mode enabled for testing
		}

		// fill in the nested fields
		opts.Telegram.Token = "secret-token"
		opts.OpenAI.Token = "openai-token"

		// test saving config to DB with debug mode
		ctx := context.Background()
		err := saveConfigToDB(ctx, &opts)
		require.NoError(t, err)

		// create a new options struct to load into
		loadedOpts := options{
			DataBaseURL: dbFile,
			ConfigDB:    true,
			InstanceID:  "test-instance",
		}

		// test loading config from DB
		err = loadConfigFromDB(ctx, &loadedOpts)
		require.NoError(t, err)

		// verify tokens are stored when debug mode is enabled
		assert.Equal(t, "secret-token", loadedOpts.Telegram.Token)
		assert.Equal(t, "openai-token", loadedOpts.OpenAI.Token)
	})

	t.Run("verify auth hash is generated and stored", func(t *testing.T) {
		// create test options with auth password but no hash
		opts := options{
			InstanceID:  "test-instance",
			DataBaseURL: dbFile,
			ConfigDB:    false,
		}

		// set server auth settings
		opts.Server.Enabled = true
		opts.Server.AuthUser = "test-user"
		opts.Server.AuthPasswd = "test-password" // password should be hashed
		opts.Server.AuthHash = ""                // no hash provided

		// save to DB
		ctx := context.Background()
		err := saveConfigToDB(ctx, &opts)
		require.NoError(t, err)

		// load from DB
		loadedOpts := options{
			DataBaseURL: dbFile,
			ConfigDB:    true,
			InstanceID:  "test-instance",
		}

		err = loadConfigFromDB(ctx, &loadedOpts)
		require.NoError(t, err)

		// verify auth settings
		assert.Equal(t, "test-user", loadedOpts.Server.AuthUser)
		assert.Empty(t, loadedOpts.Server.AuthPasswd, "Password should not be stored")
		assert.NotEmpty(t, loadedOpts.Server.AuthHash, "Auth hash should be generated and stored")

		// verify the hash is valid by checking if it's a bcrypt hash (starts with $2a$)
		assert.True(t, strings.HasPrefix(loadedOpts.Server.AuthHash, "$2a$"),
			"Auth hash should be a bcrypt hash starting with $2a$")
	})

	t.Run("override cli values", func(t *testing.T) {
		// this test simulates what happens in main.go, where we save CLI values, load from DB, then restore CLI values

		// first save a configuration as if it already exists in database
		// NOTE: Important to use the same GID ("test-instance") for all operations in this test
		saveOpts := options{
			InstanceID:      "test-instance", // use same GID for config
			DataBaseURL:     dbFile,
			Dry:             true,
			Dbg:             false,
			TGDbg:           false,
			StorageTimeout:  10 * time.Second,
			HistoryDuration: 48 * time.Hour,
			MultiLangWords:  5,
		}

		// fill in nested fields for saveOpts
		saveOpts.Telegram.Token = "db-token"
		saveOpts.Telegram.Group = "db-group"
		saveOpts.Server.AuthPasswd = "db-password"
		saveOpts.Server.AuthHash = "db-hash"

		ctx := context.Background()
		err := saveConfigToDB(ctx, &saveOpts)
		require.NoError(t, err)

		// create test options with CLI values that should be preserved
		origOpts := options{
			InstanceID:     "test-instance", // same GID needed to read config
			DataBaseURL:    dbFile,
			ConfigDB:       true,
			Dry:            false,
			Dbg:            true,
			TGDbg:          true,
			StorageTimeout: 30 * time.Second,
		}

		// fill in nested fields
		origOpts.Telegram.Token = "cli-token"
		origOpts.Telegram.Group = "cli-group"
		origOpts.Server.AuthPasswd = "cli-password"
		origOpts.Server.AuthHash = "cli-hash"

		// store original values (in the same way main.go does)
		originalValues := map[string]interface{}{
			"DataBaseURL":       origOpts.DataBaseURL,
			"InstanceID":        origOpts.InstanceID,
			"ConfigDB":          origOpts.ConfigDB,
			"Dbg":               origOpts.Dbg,
			"TGDbg":             origOpts.TGDbg,
			"Server.AuthPasswd": origOpts.Server.AuthPasswd,
			"Server.AuthHash":   origOpts.Server.AuthHash,
			"Telegram.Token":    origOpts.Telegram.Token,
			"OpenAI.Token":      origOpts.OpenAI.Token,
			"StorageTimeout":    origOpts.StorageTimeout,
		}

		// test loading configuration from database
		err = loadConfigFromDB(ctx, &origOpts)
		require.NoError(t, err)

		// manually restore the CLI values that should override DB values
		// (this simulates what happens in main.go)
		origOpts.DataBaseURL = originalValues["DataBaseURL"].(string)
		origOpts.InstanceID = originalValues["InstanceID"].(string)
		origOpts.ConfigDB = originalValues["ConfigDB"].(bool)
		origOpts.Dbg = originalValues["Dbg"].(bool)
		origOpts.TGDbg = originalValues["TGDbg"].(bool)
		origOpts.Server.AuthPasswd = originalValues["Server.AuthPasswd"].(string)
		origOpts.Server.AuthHash = originalValues["Server.AuthHash"].(string)
		origOpts.Telegram.Token = originalValues["Telegram.Token"].(string)
		origOpts.StorageTimeout = originalValues["StorageTimeout"].(time.Duration)

		// now verify - CLI values should be preserved
		assert.Equal(t, "test-instance", origOpts.InstanceID)
		assert.Equal(t, true, origOpts.Dbg)
		assert.Equal(t, true, origOpts.TGDbg)
		assert.Equal(t, "cli-password", origOpts.Server.AuthPasswd)
		assert.Equal(t, "cli-hash", origOpts.Server.AuthHash)
		assert.Equal(t, "cli-token", origOpts.Telegram.Token)
		assert.Equal(t, 30*time.Second, origOpts.StorageTimeout)

		// DB values should be loaded for non-preserved fields
		assert.Equal(t, true, origOpts.Dry)                     // from DB
		assert.Equal(t, 5, origOpts.MultiLangWords)             // from DB
		assert.Equal(t, 48*time.Hour, origOpts.HistoryDuration) // from DB
		assert.Equal(t, "db-group", origOpts.Telegram.Group)    // from DB
	})

	t.Run("error handling - database error", func(t *testing.T) {
		// let's use a path that doesn't exist and that the user definitely doesn't have permissions to create
		invalidPath := "/root/protected/file.db"
		// on non-Unix systems, this might not work, so we'll skip the test if we can actually create this file
		if _, err := os.Stat("/root"); err == nil {
			t.Skip("Can access /root directory, this test might not be reliable")
		}

		invalidOpts := options{
			DataBaseURL: invalidPath,
			InstanceID:  "test-instance",
			ConfigDB:    true,
		}

		ctx := context.Background()
		err := saveConfigToDB(ctx, &invalidOpts)
		assert.Error(t, err, "Expected error when trying to save to invalid database path")

		err = loadConfigFromDB(ctx, &invalidOpts)
		assert.Error(t, err, "Expected error when trying to load from invalid database path")
	})

	t.Run("error handling - non-existent config", func(t *testing.T) {
		// use a new database file to ensure no config exists
		newDbFile := filepath.Join(tmpDir, "empty-config.db")

		// first create a valid database with no config
		db, err := engine.NewSqlite(newDbFile, "test-instance")
		require.NoError(t, err)
		db.Close()

		emptyOpts := options{
			DataBaseURL: newDbFile,
			InstanceID:  "test-instance",
			ConfigDB:    true,
		}

		ctx := context.Background()
		// try to load from a database with no config
		err = loadConfigFromDB(ctx, &emptyOpts)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load configuration")
	})
}
