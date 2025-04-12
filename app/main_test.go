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
	"github.com/umputun/tg-spam/app/config"
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

// Helper function to create settings for testing
func makeTestSettings() *config.Settings {
	return &config.Settings{
		InstanceID: "test-instance",
		Logger: config.LoggerSettings{
			Enabled:    false,
			FileName:   "/tmp/test.log",
			MaxSize:    "10M",
			MaxBackups: 1,
		},
		Files: config.FilesSettings{
			SamplesDataPath: "/tmp/samples",
			DynamicDataPath: "/tmp/dynamic",
		},
		Transient: config.TransientSettings{
			Dbg: true,
		},
	}
}

func TestMakeSpamLogWriter(t *testing.T) {
	setupLog(true, "super-secret-token")
	t.Run("happy path", func(t *testing.T) {
		file, err := os.CreateTemp(os.TempDir(), "log")
		require.NoError(t, err)
		defer os.Remove(file.Name())

		settings := makeTestSettings()
		settings.Logger.Enabled = true
		settings.Logger.FileName = file.Name()
		settings.Logger.MaxSize = "1M"
		settings.Logger.MaxBackups = 1

		writer, err := makeSpamLogWriter(settings)
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
		settings := makeTestSettings()
		settings.Logger.Enabled = true
		settings.Logger.FileName = "/tmp"
		settings.Logger.MaxSize = "1f"
		settings.Logger.MaxBackups = 1
		
		writer, err := makeSpamLogWriter(settings)
		assert.Error(t, err)
		t.Log(err)
		assert.Nil(t, writer)
	})

	t.Run("disabled", func(t *testing.T) {
		settings := makeTestSettings()
		settings.Logger.Enabled = false
		settings.Logger.FileName = "/tmp"
		settings.Logger.MaxSize = "10M"
		settings.Logger.MaxBackups = 1
		
		writer, err := makeSpamLogWriter(settings)
		assert.NoError(t, err)
		assert.IsType(t, nopWriteCloser{}, writer)
	})
}

func Test_makeDetector(t *testing.T) {
	t.Run("basic settings", func(t *testing.T) {
		settings := makeTestSettings()
		res := makeDetector(settings)
		assert.NotNil(t, res)
	})

	t.Run("with first msgs count", func(t *testing.T) {
		settings := makeTestSettings()
		settings.Transient.Credentials.OpenAIToken = "123"
		settings.Files.SamplesDataPath = "/tmp"
		settings.Files.DynamicDataPath = "/tmp"
		settings.FirstMessagesCount = 10
		
		res := makeDetector(settings)
		assert.NotNil(t, res)
		assert.Equal(t, 10, res.FirstMessagesCount)
		assert.Equal(t, true, res.FirstMessageOnly)
	})

	t.Run("with first msgs count and paranoid", func(t *testing.T) {
		settings := makeTestSettings()
		settings.Transient.Credentials.OpenAIToken = "123"
		settings.Files.SamplesDataPath = "/tmp"
		settings.Files.DynamicDataPath = "/tmp"
		settings.FirstMessagesCount = 10
		settings.ParanoidMode = true
		
		res := makeDetector(settings)
		assert.NotNil(t, res)
		assert.Equal(t, 0, res.FirstMessagesCount)
		assert.Equal(t, false, res.FirstMessageOnly)
	})
}

func Test_initLuaPlugins(t *testing.T) {
	t.Run("basic plugin initialization", func(t *testing.T) {
		settings := makeTestSettings()
		settings.LuaPlugins.Enabled = true
		settings.LuaPlugins.PluginsDir = "/path/to/plugins"
		settings.LuaPlugins.EnabledPlugins = []string{"plugin1", "plugin2"}
		settings.LuaPlugins.DynamicReload = true

		detector := makeDetector(makeTestSettings()) // create a clean detector

		// run the function to test
		initLuaPlugins(detector, settings)

		// verify that the detector's config matches the settings
		assert.True(t, detector.LuaPlugins.Enabled)
		assert.Equal(t, "/path/to/plugins", detector.LuaPlugins.PluginsDir)
		assert.Equal(t, []string{"plugin1", "plugin2"}, detector.LuaPlugins.EnabledPlugins)
		assert.True(t, detector.LuaPlugins.DynamicReload)

		// verify the Lua engine was initialized
		// we can't directly check detector.luaEngine since it's unexported
		// but we can infer it's initialized because the settings were applied
	})

	t.Run("all enabled plugins", func(t *testing.T) {
		settings := makeTestSettings()
		settings.LuaPlugins.Enabled = true
		settings.LuaPlugins.PluginsDir = "/path/to/plugins"
		// no specific plugins enabled - should enable all
		settings.LuaPlugins.DynamicReload = false

		detector := makeDetector(makeTestSettings()) // create a clean detector

		// run the function to test
		initLuaPlugins(detector, settings)

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

	t.Run("no settings", func(t *testing.T) {
		settings := makeTestSettings()
		_, err := makeSpamBot(ctx, settings, nil, nil)
		assert.Error(t, err)
	})

	t.Run("with valid settings", func(t *testing.T) {
		tmpDir := t.TempDir()

		settings := makeTestSettings()
		settings.Files.SamplesDataPath = tmpDir
		settings.Files.DynamicDataPath = tmpDir
		settings.InstanceID = "gr1"
		
		detector := makeDetector(settings)
		db, err := engine.NewSqlite(path.Join(tmpDir, "tg-spam.db"), "gr1")
		require.NoError(t, err)
		defer db.Close()

		samplesStore, err := storage.NewSamples(ctx, db)
		require.NoError(t, err)
		err = samplesStore.Add(ctx, storage.SampleTypeSpam, storage.SampleOriginPreset, "spam1")
		require.NoError(t, err)
		err = samplesStore.Add(ctx, storage.SampleTypeHam, storage.SampleOriginPreset, "ham1")
		require.NoError(t, err)

		res, err := makeSpamBot(ctx, settings, db, detector)
		assert.NoError(t, err)
		assert.NotNil(t, res)
	})
}

func Test_activateServerOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	settings := makeTestSettings()
	settings.Server.Enabled = true
	settings.Server.ListenAddr = ":9988"
	settings.Transient.Credentials.WebAuthPasswd = "auto"
	settings.InstanceID = "gr1"
	settings.Transient.DataBaseURL = fmt.Sprintf("sqlite://%s", path.Join(t.TempDir(), "tg-spam.db"))

	// Create sample directories
	settings.Files.SamplesDataPath = t.TempDir()
	settings.Files.DynamicDataPath = t.TempDir()

	// write some sample files
	fh, err := os.Create(path.Join(settings.Files.SamplesDataPath, "spam-samples.txt"))
	require.NoError(t, err)
	_, err = fh.WriteString("spam1\nspam2\nspam3\n")
	require.NoError(t, err)
	fh.Close()

	fh, err = os.Create(path.Join(settings.Files.SamplesDataPath, "ham-samples.txt"))
	require.NoError(t, err)
	_, err = fh.WriteString("ham1\nham2\nham3\n")
	require.NoError(t, err)
	fh.Close()

	done := make(chan struct{})
	go func() {
		err := execute(ctx, settings)
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
	prepEnvAndFileSystem := func(settings *config.Settings, envValue string, dynamicDataPath string, notMountedExists bool) func() {
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
		settings.Files.DynamicDataPath = filepath.Join(tempDir, dynamicDataPath)

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
			settings := makeTestSettings()
			cleanup := prepEnvAndFileSystem(settings, tt.envValue, tt.dynamicDataPath, tt.notMountedExists)
			defer cleanup()

			ok := checkVolumeMount(settings)
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
	settings := makeTestSettings()
	settings.Files.SamplesDataPath, settings.Files.DynamicDataPath = tmpDir, tmpDir
	settings.InstanceID = "gr1"

	t.Run("full migration", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()
		store, err := storage.NewSamples(context.Background(), db)
		require.NoError(t, err)

		// create new files for migration, all 4 files should be migrated
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, samplesSpamFile),
			[]byte("new spam1\nnew spam2\nnew spam 3"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.DynamicDataPath, samplesHamFile),
			[]byte("new ham1\nnew ham2"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, dynamicSpamFile),
			[]byte("new dspam1\nnew dspam2\nnew dspam3"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.DynamicDataPath, dynamicHamFile),
			[]byte("new dham1\nnew dham2"), 0o600))

		err = migrateSamples(context.Background(), settings, store)
		assert.NoError(t, err)

		// verify all files migrated
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, samplesSpamFile))
		assert.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.DynamicDataPath, samplesHamFile))
		assert.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, dynamicSpamFile))
		assert.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.DynamicDataPath, dynamicHamFile))
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
		err := migrateSamples(context.Background(), settings, nil)
		assert.Error(t, err)
	})

	t.Run("already migrated", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()
		store, err := storage.NewSamples(context.Background(), db)
		require.NoError(t, err)

		// create already loaded files
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, samplesSpamFile+".loaded"),
			[]byte("old spam"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.DynamicDataPath, dynamicHamFile+".loaded"),
			[]byte("old ham"), 0o600))

		err = migrateSamples(context.Background(), settings, store)
		assert.NoError(t, err)

		// verify old files untouched
		data, err := os.ReadFile(filepath.Join(settings.Files.SamplesDataPath, samplesSpamFile+".loaded"))
		require.NoError(t, err)
		assert.Equal(t, "old spam", string(data))

		// verify new files migrated
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, samplesSpamFile))
		assert.Error(t, err, "original file should be renamed")
	})

	t.Run("partial migration", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()
		store, err := storage.NewSamples(context.Background(), db)
		require.NoError(t, err)

		// create mix of loaded and unloaded files
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, samplesSpamFile+".loaded"), []byte("old spam"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.DynamicDataPath, dynamicHamFile), []byte("new ham"), 0o600))

		err = migrateSamples(context.Background(), settings, store)
		assert.NoError(t, err)

		// verify only unloaded files migrated
		_, err = os.Stat(filepath.Join(settings.Files.DynamicDataPath, dynamicHamFile))
		assert.Error(t, err, "unloaded file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.DynamicDataPath, dynamicHamFile+".loaded"))
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

		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, samplesSpamFile), []byte(""), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.DynamicDataPath, dynamicHamFile), []byte(""), 0600))

		err = migrateSamples(context.Background(), settings, store)
		assert.NoError(t, err)
	})
}

func Test_migrateDicts(t *testing.T) {
	tmpDir := t.TempDir()
	settings := makeTestSettings()
	settings.Files.SamplesDataPath = tmpDir
	settings.InstanceID = "gr1"

	t.Run("nil dictionary", func(t *testing.T) {
		err := migrateDicts(context.Background(), settings, nil)
		assert.Error(t, err)
	})

	t.Run("full migration", func(t *testing.T) {
		db, err := engine.NewSqlite(":memory:", "gr1")
		require.NoError(t, err)
		defer db.Close()
		dict, err := storage.NewDictionary(context.Background(), db)
		require.NoError(t, err)

		// create new files for migration
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, stopWordsFile),
			[]byte("stop1\nstop2\nstop3"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile),
			[]byte("token1\ntoken2"), 0600))

		err = migrateDicts(context.Background(), settings, dict)
		assert.NoError(t, err)

		// verify files renamed and moved correctly
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, stopWordsFile))
		assert.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, stopWordsFile+".loaded"))
		assert.NoError(t, err)

		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile))
		assert.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile+".loaded"))
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
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, stopWordsFile+".loaded"),
			[]byte("old stop1\nold stop2"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile+".loaded"),
			[]byte("old token1"), 0600))

		// create new files
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, stopWordsFile),
			[]byte("new stop1\nnew stop2"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile),
			[]byte("new token1\nnew token2"), 0600))

		err = migrateDicts(context.Background(), settings, dict)
		assert.NoError(t, err)

		// verify import happened correctly
		s, err := dict.Stats(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 2, s.TotalStopPhrases)
		assert.Equal(t, 2, s.TotalIgnoredWords)

		// verify old files overwritten
		data, err := os.ReadFile(filepath.Join(settings.Files.SamplesDataPath, stopWordsFile+".loaded"))
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
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, stopWordsFile), []byte(""), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile), []byte(""), 0600))

		err = migrateDicts(context.Background(), settings, dict)
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
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, stopWordsFile+".loaded"),
			[]byte("old stop1\nold stop2"), 0600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile),
			[]byte("token1\ntoken2"), 0600))

		err = migrateDicts(context.Background(), settings, dict)
		assert.NoError(t, err)

		// verify only unloaded file migrated
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile))
		assert.Error(t, err, "unloaded file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile+".loaded"))
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
		// create test settings with some values
		settings := &config.Settings{
			InstanceID:          "test-instance",
			Dry:                 true,
			SoftBan:             true,
			MinMsgLen:           100,
			MaxEmoji:            5,
			ParanoidMode:        true,
			Training:            true,
			MultiLangWords:      3,
			FirstMessagesCount:  1,
			MinSpamProbability:  50,
			SimilarityThreshold: 0.5,
			Telegram: config.TelegramSettings{
				Group:        "test-group",
				Timeout:      5 * time.Second,
				IdleDuration: 10 * time.Second,
			},
			History: config.HistorySettings{
				Size:     200,
				Duration: 24 * time.Hour,
				MinSize:  500,
			},
		}

		// add transient settings (should not be stored)
		settings.Transient = config.TransientSettings{
			DataBaseURL:    dbFile,
			StorageTimeout: 30 * time.Second,
			Credentials: config.Credentials{
				TelegramToken: "test-token",
				OpenAIToken:   "",
			},
		}

		// test saving config to DB
		ctx := context.Background()
		err := saveConfigToDB(ctx, settings)
		require.NoError(t, err)

		// create a new settings struct to load into
		loadedSettings := &config.Settings{
			InstanceID: "test-instance",
			Transient: config.TransientSettings{
				DataBaseURL: dbFile,
				ConfigDB:    true,
			},
		}

		// test loading config from DB
		err = loadConfigFromDB(ctx, loadedSettings)
		require.NoError(t, err)

		// verify loaded values match original (except sensitive fields that should be cleared)
		assert.Equal(t, settings.InstanceID, loadedSettings.InstanceID)
		assert.Equal(t, settings.Dry, loadedSettings.Dry)
		assert.Equal(t, settings.SoftBan, loadedSettings.SoftBan)
		assert.Equal(t, settings.MinMsgLen, loadedSettings.MinMsgLen)
		assert.Equal(t, settings.MaxEmoji, loadedSettings.MaxEmoji)
		assert.Equal(t, settings.ParanoidMode, loadedSettings.ParanoidMode)
		assert.Equal(t, settings.Training, loadedSettings.Training)
		assert.Equal(t, settings.MultiLangWords, loadedSettings.MultiLangWords)
		assert.Equal(t, settings.History.Duration, loadedSettings.History.Duration)
		assert.Equal(t, settings.History.MinSize, loadedSettings.History.MinSize)
		assert.Equal(t, settings.History.Size, loadedSettings.History.Size)

		// group-related fields
		assert.Equal(t, settings.Telegram.Group, loadedSettings.Telegram.Group)
		assert.Equal(t, settings.Telegram.Timeout, loadedSettings.Telegram.Timeout)
		assert.Equal(t, settings.Telegram.IdleDuration, loadedSettings.Telegram.IdleDuration)

		// verify original transient fields were NOT loaded
		assert.Equal(t, dbFile, loadedSettings.Transient.DataBaseURL)
		assert.Equal(t, true, loadedSettings.Transient.ConfigDB)
		assert.Empty(t, loadedSettings.Transient.StorageTimeout)
	})

	t.Run("verify tokens are stored with different values", func(t *testing.T) {
		// create test settings with token values
		settings := &config.Settings{
			InstanceID: "test-instance",
			Transient: config.TransientSettings{
				DataBaseURL: dbFile,
				Dbg:         true, // debug mode enabled for testing
				Credentials: config.Credentials{
					TelegramToken: "secret-token",
					OpenAIToken:   "openai-token",
				},
			},
		}

		// test saving config to DB with debug mode
		ctx := context.Background()
		err := saveConfigToDB(ctx, settings)
		require.NoError(t, err)

		// create a new settings struct to load into
		loadedSettings := &config.Settings{
			InstanceID: "test-instance",
			Transient: config.TransientSettings{
				DataBaseURL: dbFile,
				ConfigDB:    true,
			},
		}

		// test loading config from DB
		err = loadConfigFromDB(ctx, loadedSettings)
		require.NoError(t, err)

		// verify original transient credentials are not in the loaded settings
		assert.Empty(t, loadedSettings.Transient.Credentials.TelegramToken)
		assert.Empty(t, loadedSettings.Transient.Credentials.OpenAIToken)
	})

	t.Run("verify auth hash is generated and stored", func(t *testing.T) {
		// create test settings with auth password but no hash
		settings := &config.Settings{
			InstanceID: "test-instance",
			Server: config.ServerSettings{
				Enabled:  true,
				AuthUser: "test-user",
			},
			Transient: config.TransientSettings{
				DataBaseURL: dbFile,
				Credentials: config.Credentials{
					WebAuthPasswd: "test-password", // password should be hashed
					WebAuthHash:   "",             // no hash provided
				},
			},
		}

		// save to DB
		ctx := context.Background()
		err := saveConfigToDB(ctx, settings)
		require.NoError(t, err)

		// load from DB with a new settings struct
		loadedSettings := &config.Settings{
			InstanceID: "test-instance",
			Transient: config.TransientSettings{
				DataBaseURL: dbFile,
				ConfigDB:    true,
			},
		}

		err = loadConfigFromDB(ctx, loadedSettings)
		require.NoError(t, err)

		// verify auth settings
		assert.Equal(t, "test-user", loadedSettings.Server.AuthUser)
		
		// verify hash in original settings was set
		assert.NotEmpty(t, settings.Transient.Credentials.WebAuthHash, "Auth hash should be generated in original settings")
		assert.True(t, strings.HasPrefix(settings.Transient.Credentials.WebAuthHash, "$2a$"),
			"Auth hash should be a bcrypt hash starting with $2a$")
	})

	t.Run("override cli values", func(t *testing.T) {
		// this test simulates what happens in main.go, where we save CLI values, load from DB, then restore CLI values

		// first save a configuration to the database
		dbSettings := &config.Settings{
			InstanceID:     "test-instance",
			Dry:            true,
			MultiLangWords: 5,
			History: config.HistorySettings{
				Duration: 48 * time.Hour,
			},
			Telegram: config.TelegramSettings{
				Group: "db-group",
			},
			Transient: config.TransientSettings{
				DataBaseURL: dbFile,
			},
		}
		
		ctx := context.Background()
		err := saveConfigToDB(ctx, dbSettings)
		require.NoError(t, err)

		// create settings with CLI values that should be preserved
		cliSettings := &config.Settings{
			InstanceID: "test-instance",
			Dry:        false,
			Transient: config.TransientSettings{
				DataBaseURL:    dbFile,
				ConfigDB:       true,
				Dbg:            true,
				TGDbg:          true,
				StorageTimeout: 30 * time.Second,
				Credentials: config.Credentials{
					TelegramToken: "cli-token",
					WebAuthPasswd: "cli-password",
					WebAuthHash:   "cli-hash",
				},
			},
			Telegram: config.TelegramSettings{
				Group: "cli-group",
			},
		}

		// Save original credentials and transient values
		originalCreds := cliSettings.GetCredentials()
		originalTransient := cliSettings.Transient

		// test loading configuration from database
		err = loadConfigFromDB(ctx, cliSettings)
		require.NoError(t, err)

		// Restore transient values
		cliSettings.Transient = originalTransient
		
		// Restore credentials
		cliSettings.SetCredentials(originalCreds)

		// now verify - CLI values should be preserved
		assert.Equal(t, "test-instance", cliSettings.InstanceID)
		assert.Equal(t, true, cliSettings.Transient.Dbg)
		assert.Equal(t, true, cliSettings.Transient.TGDbg)
		assert.Equal(t, "cli-password", cliSettings.Transient.Credentials.WebAuthPasswd)
		assert.Equal(t, "cli-hash", cliSettings.Transient.Credentials.WebAuthHash)
		assert.Equal(t, "cli-token", cliSettings.Transient.Credentials.TelegramToken)
		assert.Equal(t, 30*time.Second, cliSettings.Transient.StorageTimeout)

		// DB values should be loaded for non-transient fields
		assert.Equal(t, true, cliSettings.Dry)                         // from DB
		assert.Equal(t, 5, cliSettings.MultiLangWords)                 // from DB
		assert.Equal(t, 48*time.Hour, cliSettings.History.Duration)    // from DB
		assert.Equal(t, "db-group", cliSettings.Telegram.Group)        // from DB
	})

	t.Run("error handling - database error", func(t *testing.T) {
		// let's use a path that doesn't exist and that the user definitely doesn't have permissions to create
		invalidPath := "/root/protected/file.db"
		// on non-Unix systems, this might not work, so we'll skip the test if we can actually create this file
		if _, err := os.Stat("/root"); err == nil {
			t.Skip("Can access /root directory, this test might not be reliable")
		}

		invalidSettings := &config.Settings{
			InstanceID: "test-instance",
			Transient: config.TransientSettings{
				DataBaseURL: invalidPath,
				ConfigDB:    true,
			},
		}

		ctx := context.Background()
		err := saveConfigToDB(ctx, invalidSettings)
		assert.Error(t, err, "Expected error when trying to save to invalid database path")

		err = loadConfigFromDB(ctx, invalidSettings)
		assert.Error(t, err, "Expected error when trying to load from invalid database path")
	})

	t.Run("error handling - non-existent config", func(t *testing.T) {
		// use a new database file to ensure no config exists
		newDbFile := filepath.Join(tmpDir, "empty-config.db")

		// first create a valid database with no config
		db, err := engine.NewSqlite(newDbFile, "test-instance")
		require.NoError(t, err)
		db.Close()

		emptySettings := &config.Settings{
			InstanceID: "test-instance",
			Transient: config.TransientSettings{
				DataBaseURL: newDbFile,
				ConfigDB:    true,
			},
		}

		ctx := context.Background()
		// try to load from a database with no config
		err = loadConfigFromDB(ctx, emptySettings)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load") // matches "failed to load settings from database"
	})
}
