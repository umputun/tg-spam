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

	"github.com/go-pkgz/rest"
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

		var logEntry map[string]any
		err = json.Unmarshal([]byte(line), &logEntry)
		require.NoError(t, err)

		assert.Equal(t, "Test User", logEntry["display_name"])
		assert.Equal(t, "testuser", logEntry["user_name"])
		assert.InEpsilon(t, float64(123), logEntry["user_id"], 0.0001) // json.Unmarshal converts numbers to float64
		assert.Equal(t, "Test message blah blah", logEntry["text"])
	}
	require.NoError(t, scanner.Err())

	// check that the message is saved to the database
	savedMsgs := []storage.DetectedSpamInfo{}
	err = db.Select(&savedMsgs, "SELECT text, user_id, user_name, timestamp, checks FROM detected_spam")
	require.NoError(t, err)
	assert.Len(t, savedMsgs, 1)
	assert.Equal(t, "Test message blah blah", savedMsgs[0].Text)
	assert.Equal(t, "testuser", savedMsgs[0].UserName)
	assert.Equal(t, int64(123), savedMsgs[0].UserID)
	assert.JSONEq(t, `[{"name":"Check1","spam":true,"details":"Details 1"},{"name":"Check2","spam":false,"details":"Details 2"}]`,
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
		require.NoError(t, err)
		err = writer.Close()
		require.NoError(t, err)

		file, err = os.Open(file.Name())
		require.NoError(t, err)

		content, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, "Test log entry\n", string(content))
	})

	t.Run("failed on wrong size", func(t *testing.T) {
		settings := makeTestSettings()
		settings.Logger.Enabled = true
		settings.Logger.FileName = "/tmp"
		settings.Logger.MaxSize = "1f"
		settings.Logger.MaxBackups = 1

		writer, err := makeSpamLogWriter(settings)
		require.Error(t, err)
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
		require.NoError(t, err)
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
		settings.OpenAI.Token = "123"
		settings.Files.SamplesDataPath = "/tmp"
		settings.Files.DynamicDataPath = "/tmp"
		settings.FirstMessagesCount = 10

		res := makeDetector(settings)
		assert.NotNil(t, res)
		assert.Equal(t, 10, res.FirstMessagesCount)
		assert.True(t, res.FirstMessageOnly)
	})

	t.Run("with first msgs count and paranoid", func(t *testing.T) {
		settings := makeTestSettings()
		settings.OpenAI.Token = "123"
		settings.Files.SamplesDataPath = "/tmp"
		settings.Files.DynamicDataPath = "/tmp"
		settings.FirstMessagesCount = 10
		settings.ParanoidMode = true

		res := makeDetector(settings)
		assert.NotNil(t, res)
		assert.Equal(t, 0, res.FirstMessagesCount)
		assert.False(t, res.FirstMessageOnly)
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
	ctx := t.Context()

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
		require.NoError(t, err)
		assert.NotNil(t, res)
	})
}

func Test_activateServerOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	settings := makeTestSettings()
	settings.Server.Enabled = true
	settings.Server.ListenAddr = ":9988"
	settings.Transient.WebAuthPasswd = "auto"
	settings.InstanceID = "gr1"
	settings.Transient.DataBaseURL = fmt.Sprintf("sqlite://%s", path.Join(t.TempDir(), "tg-spam.db"))

	// create sample directories
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
		execErr := execute(ctx, settings)
		assert.NoError(t, execErr)
		close(done)
	}()

	// wait for server to be ready
	require.Eventually(t, func() bool {
		resp, getErr := http.Get("http://localhost:9988/ping")
		if getErr != nil {
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

func Test_activateServerAuthHashOnly(t *testing.T) {
	// regression for issue #381: SERVER_AUTH left at default "auto" with
	// SERVER_AUTH_HASH explicitly set must authenticate via hash alone, both
	// on the webUI and on authApi routes.
	const knownPassword = "secret-known-pass"
	authHash, err := rest.GenerateBcryptHash(knownPassword)
	require.NoError(t, err)

	baseURL := runActivateServerForTest(t, ":9989", "auto", authHash)

	t.Run("webUI correct password accepted", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, baseURL+"/", http.NoBody)
		require.NoError(t, err)
		req.SetBasicAuth("tg-spam", knownPassword)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("webUI wrong password rejected", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, baseURL+"/", http.NoBody)
		require.NoError(t, err)
		req.SetBasicAuth("tg-spam", "wrong-password")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("authApi correct password accepted", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, baseURL+"/settings", http.NoBody)
		require.NoError(t, err)
		req.SetBasicAuth("tg-spam", knownPassword)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("authApi wrong password rejected", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, baseURL+"/settings", http.NoBody)
		require.NoError(t, err)
		req.SetBasicAuth("tg-spam", "wrong-password")
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}

func Test_activateServerAuthHashOverridesExplicitPasswd(t *testing.T) {
	// when both SERVER_AUTH (explicit) and SERVER_AUTH_HASH are set, hash wins:
	// the explicit password must not be accepted, only the hash-matching one.
	const knownPassword = "hash-matching-pass"
	const explicitPassword = "ignored-explicit-pass"
	authHash, err := rest.GenerateBcryptHash(knownPassword)
	require.NoError(t, err)

	baseURL := runActivateServerForTest(t, ":9990", explicitPassword, authHash)

	t.Run("hash-matching password accepted", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, baseURL+"/", http.NoBody)
		require.NoError(t, err)
		req.SetBasicAuth("tg-spam", knownPassword)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("explicit password ignored", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, baseURL+"/", http.NoBody)
		require.NoError(t, err)
		req.SetBasicAuth("tg-spam", explicitPassword)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}

// runActivateServerForTest boots the full execute() flow with a minimal set of
// settings sufficient for web-server-only mode, waits for /ping to respond, and
// returns the base URL. Cleanup (context cancel + goroutine join) is registered
// via t.Cleanup.
func runActivateServerForTest(t *testing.T, listenAddr, authPasswd, authHash string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	settings := makeTestSettings()
	settings.Server.Enabled = true
	settings.Server.ListenAddr = listenAddr
	settings.Server.AuthHash = authHash
	settings.Transient.WebAuthPasswd = authPasswd
	settings.InstanceID = "gr1"
	settings.Transient.DataBaseURL = fmt.Sprintf("sqlite://%s", path.Join(t.TempDir(), "tg-spam.db"))
	settings.Files.SamplesDataPath = t.TempDir()
	settings.Files.DynamicDataPath = t.TempDir()

	fh, err := os.Create(path.Join(settings.Files.SamplesDataPath, "spam-samples.txt"))
	require.NoError(t, err)
	_, err = fh.WriteString("spam1\nspam2\nspam3\n")
	require.NoError(t, err)
	require.NoError(t, fh.Close())

	fh, err = os.Create(path.Join(settings.Files.SamplesDataPath, "ham-samples.txt"))
	require.NoError(t, err)
	_, err = fh.WriteString("ham1\nham2\nham3\n")
	require.NoError(t, err)
	require.NoError(t, fh.Close())

	done := make(chan struct{})
	go func() {
		execErr := execute(ctx, settings)
		assert.NoError(t, execErr)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	baseURL := "http://localhost" + listenAddr
	require.Eventually(t, func() bool {
		pingResp, pingErr := http.Get(baseURL + "/ping")
		if pingErr != nil {
			return false
		}
		defer pingResp.Body.Close()
		return pingResp.StatusCode == http.StatusOK
	}, time.Second*5, time.Millisecond*100, "server did not start")

	return baseURL
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
		require.NoError(t, err)

		// verify all files migrated
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, samplesSpamFile))
		require.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.DynamicDataPath, samplesHamFile))
		require.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, dynamicSpamFile))
		require.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.DynamicDataPath, dynamicHamFile))
		require.Error(t, err, "original file should be renamed")

		s, err := store.Stats(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 6, s.TotalSpam)
		assert.Equal(t, 4, s.TotalHam)

		res, err := store.Read(context.Background(), storage.SampleTypeSpam, storage.SampleOriginUser)
		require.NoError(t, err)
		assert.Len(t, res, 3)
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
		require.NoError(t, err)

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
		require.NoError(t, err)

		// verify only unloaded files migrated
		_, err = os.Stat(filepath.Join(settings.Files.DynamicDataPath, dynamicHamFile))
		require.Error(t, err, "unloaded file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.DynamicDataPath, dynamicHamFile+".loaded"))
		require.NoError(t, err)

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

		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, samplesSpamFile), []byte(""), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.DynamicDataPath, dynamicHamFile), []byte(""), 0o600))

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
			[]byte("stop1\nstop2\nstop3"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile),
			[]byte("token1\ntoken2"), 0o600))

		err = migrateDicts(context.Background(), settings, dict)
		require.NoError(t, err)

		// verify files renamed and moved correctly
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, stopWordsFile))
		require.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, stopWordsFile+".loaded"))
		require.NoError(t, err)

		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile))
		require.Error(t, err, "original file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile+".loaded"))
		require.NoError(t, err)

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
			[]byte("old stop1\nold stop2"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile+".loaded"),
			[]byte("old token1"), 0o600))

		// create new files
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, stopWordsFile),
			[]byte("new stop1\nnew stop2"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile),
			[]byte("new token1\nnew token2"), 0o600))

		err = migrateDicts(context.Background(), settings, dict)
		require.NoError(t, err)

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
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, stopWordsFile), []byte(""), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile), []byte(""), 0o600))

		err = migrateDicts(context.Background(), settings, dict)
		require.NoError(t, err)

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
			[]byte("old stop1\nold stop2"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile),
			[]byte("token1\ntoken2"), 0o600))

		err = migrateDicts(context.Background(), settings, dict)
		require.NoError(t, err)

		// verify only unloaded file migrated
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile))
		require.Error(t, err, "unloaded file should be renamed")
		_, err = os.Stat(filepath.Join(settings.Files.SamplesDataPath, excludeTokensFile+".loaded"))
		require.NoError(t, err)

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
		require.NoError(t, os.WriteFile(dbFile, []byte("test data"), 0o600))

		err := backupDB(dbFile, "v1", 0)
		require.NoError(t, err)
		files, err := filepath.Glob(dbFile + ".*")
		require.NoError(t, err)
		require.Empty(t, files)
	})

	t.Run("skip existing backup", func(t *testing.T) {
		dir := t.TempDir()
		dbFile := filepath.Join(dir, "test.db")
		require.NoError(t, os.WriteFile(dbFile, []byte("test data"), 0o600))

		backupFile := dbFile + ".master-123-20250108T00:01:26"
		require.NoError(t, os.WriteFile(backupFile, []byte("old backup"), 0o600))
		origSize := fileSize(t, backupFile)

		err := backupDB(dbFile, "master-123-20250108T00:01:26", 1)
		require.NoError(t, err)

		newSize := fileSize(t, backupFile)
		require.Equal(t, origSize, newSize, "backup file should not be modified")
	})

	t.Run("make new backup and cleanup", func(t *testing.T) {
		dir := t.TempDir()
		dbFile := filepath.Join(dir, "test.db")
		require.NoError(t, os.WriteFile(dbFile, []byte("test data"), 0o600))

		// create some old backups
		oldBackups := []string{
			"master-111-20250108T00:01:26",
			"master-222-20250108T00:02:26",
			"master-333-20250108T00:03:26",
		}
		for _, v := range oldBackups {
			require.NoError(t, os.WriteFile(dbFile+"."+v, []byte("old"), 0o600))
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
		require.Equal(t, "test data", content)
	})

	t.Run("mixed_formats", func(t *testing.T) {
		dir := t.TempDir()
		dbFile := filepath.Join(dir, "test.db")
		require.NoError(t, os.WriteFile(dbFile, []byte("test data"), 0o600))

		// make older files with version suffix
		require.NoError(t, os.WriteFile(dbFile+".master-aaa-20250101T12:00:00", []byte("1"), 0o600))
		require.NoError(t, os.WriteFile(dbFile+".master-bbb-20250101T13:00:00", []byte("2"), 0o600))

		// make normal files dated between versioned ones
		testTime := time.Date(2025, 1, 1, 12, 30, 0, 0, time.Local)
		require.NoError(t, os.WriteFile(dbFile+".backup1", []byte("3"), 0o600))
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
		require.NoError(t, os.WriteFile(dbFile, []byte("test data"), 0o600))

		version := "master-123-1.2.3-20250108T00:01:26"
		err := backupDB(dbFile, version, 1)
		require.NoError(t, err)

		expectedBackup := dbFile + "." + strings.ReplaceAll(version, ".", "_")
		_, err = os.Stat(expectedBackup)
		require.NoError(t, err)

		content := readFile(t, expectedBackup)
		require.Equal(t, "test data", content)

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

func TestApplyCLIOverrides(t *testing.T) {
	makeOpts := func(passwd, hash string) options {
		var o options
		o.Server.AuthPasswd = passwd
		o.Server.AuthHash = hash
		return o
	}

	tests := []struct {
		name            string
		settings        config.Settings
		opts            options
		expectedPasswd  string
		expectedHash    string
		expectedFromCLI bool
	}{
		{
			name: "override auth password when not default",
			settings: config.Settings{
				Server:    config.ServerSettings{AuthHash: "existing-hash"},
				Transient: config.TransientSettings{WebAuthPasswd: "old-password"},
			},
			opts:            makeOpts("new-password", ""),
			expectedPasswd:  "new-password",
			expectedHash:    "", // hash should be cleared
			expectedFromCLI: true,
		},
		{
			name: "override auth hash when explicitly provided",
			settings: config.Settings{
				Server:    config.ServerSettings{AuthHash: "old-hash"},
				Transient: config.TransientSettings{WebAuthPasswd: "password"},
			},
			opts:            makeOpts("auto", "$2a$10$newHashFromCLI"),
			expectedPasswd:  "", // password should be cleared
			expectedHash:    "$2a$10$newHashFromCLI",
			expectedFromCLI: true,
		},
		{
			name: "no override when using default values",
			settings: config.Settings{
				Server:    config.ServerSettings{AuthHash: "existing-hash"},
				Transient: config.TransientSettings{WebAuthPasswd: "existing-password"},
			},
			opts:            makeOpts("auto", ""),
			expectedPasswd:  "existing-password",
			expectedHash:    "existing-hash",
			expectedFromCLI: false,
		},
		{
			name: "hash takes precedence when both provided",
			settings: config.Settings{
				Server:    config.ServerSettings{AuthHash: "old-hash"},
				Transient: config.TransientSettings{WebAuthPasswd: "old-password"},
			},
			opts:            makeOpts("cli-password", "$2a$10$cliHash"),
			expectedPasswd:  "", // password cleared because hash takes precedence
			expectedHash:    "$2a$10$cliHash",
			expectedFromCLI: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// make a copy of settings to avoid modifying the test case
			settingsCopy := tt.settings
			applyCLIOverrides(&settingsCopy, tt.opts)

			assert.Equal(t, tt.expectedPasswd, settingsCopy.Transient.WebAuthPasswd,
				"WebAuthPasswd should match expected value")
			assert.Equal(t, tt.expectedHash, settingsCopy.Server.AuthHash,
				"AuthHash should match expected value")
			assert.Equal(t, tt.expectedFromCLI, settingsCopy.Transient.AuthFromCLI,
				"AuthFromCLI should match expected value")
		})
	}

	t.Run("dry flag preserves DB value when CLI default", func(t *testing.T) {
		// dry=true persisted in DB must survive startup when --dry is not passed
		settings := config.Settings{Dry: true}
		var opts options
		opts.Server.AuthPasswd = "auto"
		applyCLIOverrides(&settings, opts)
		assert.True(t, settings.Dry, "DB Dry=true must not be clobbered by CLI default")
	})

	t.Run("dry flag CLI true overrides DB false", func(t *testing.T) {
		settings := config.Settings{Dry: false}
		var opts options
		opts.Server.AuthPasswd = "auto"
		opts.Dry = true
		applyCLIOverrides(&settings, opts)
		assert.True(t, settings.Dry, "CLI --dry must override DB Dry=false")
	})

	t.Run("telegram token CLI overrides DB value", func(t *testing.T) {
		settings := config.Settings{Telegram: config.TelegramSettings{Token: "db-token"}}
		var opts options
		opts.Server.AuthPasswd = "auto"
		opts.Telegram.Token = "cli-token"
		applyCLIOverrides(&settings, opts)
		assert.Equal(t, "cli-token", settings.Telegram.Token, "CLI telegram token must override DB value")
	})

	t.Run("empty telegram CLI token preserves DB value", func(t *testing.T) {
		settings := config.Settings{Telegram: config.TelegramSettings{Token: "db-token"}}
		var opts options
		opts.Server.AuthPasswd = "auto"
		applyCLIOverrides(&settings, opts)
		assert.Equal(t, "db-token", settings.Telegram.Token, "DB telegram token must survive when CLI is empty")
	})

	t.Run("openai token CLI overrides DB value", func(t *testing.T) {
		settings := config.Settings{OpenAI: config.OpenAISettings{Token: "db-openai"}}
		var opts options
		opts.Server.AuthPasswd = "auto"
		opts.OpenAI.Token = "cli-openai"
		applyCLIOverrides(&settings, opts)
		assert.Equal(t, "cli-openai", settings.OpenAI.Token, "CLI openai token must override DB value")
	})

	t.Run("gemini token CLI overrides DB value", func(t *testing.T) {
		settings := config.Settings{Gemini: config.GeminiSettings{Token: "db-gemini"}}
		var opts options
		opts.Server.AuthPasswd = "auto"
		opts.Gemini.Token = "cli-gemini"
		applyCLIOverrides(&settings, opts)
		assert.Equal(t, "cli-gemini", settings.Gemini.Token, "CLI gemini token must override DB value")
	})
}

func TestOptToSettings(t *testing.T) {
	// build a fully-populated options struct via field assignment to avoid
	// brittle inline struct-type literals that drift with tag changes
	makeFullOpts := func() options {
		var o options
		o.InstanceID = "test-instance"
		o.MinMsgLen = 100
		o.MaxEmoji = 5
		o.MinSpamProbability = 0.8
		o.SimilarityThreshold = 0.9
		o.MultiLangWords = 3
		o.NoSpamReply = true
		o.SuppressJoinMessage = true
		o.ParanoidMode = true
		o.FirstMessagesCount = 10
		o.Training = true
		o.SoftBan = true
		o.Convert = "enabled"
		o.MaxBackups = 5
		o.Dry = true
		o.DataBaseURL = "sqlite://test.db"
		o.StorageTimeout = 30 * time.Second
		o.ConfigDB = true
		o.ConfigDBEncryptKey = "test-key"
		o.Dbg = true
		o.TGDbg = true
		o.AdminGroup = "123456"
		o.DisableAdminSpamForward = true
		o.TestingIDs = []int64{1, 2, 3}
		o.SuperUsers = []string{"user1", "user2"}
		o.HistoryDuration = 24 * time.Hour
		o.HistoryMinSize = 100
		o.HistorySize = 1000
		o.AggressiveCleanup = true
		o.AggressiveCleanupLimit = 200

		o.Telegram.Token = "bot-token"
		o.Telegram.Group = "test-group"
		o.Telegram.IdleDuration = 5 * time.Minute
		o.Telegram.Timeout = 30 * time.Second

		o.Logger.Enabled = true
		o.Logger.FileName = "test.log"
		o.Logger.MaxSize = "50M"
		o.Logger.MaxBackups = 5

		o.CAS.API = "https://cas.example.com"
		o.CAS.Timeout = 10 * time.Second
		o.CAS.UserAgent = "test-agent"

		o.Meta.LinksLimit = 2
		o.Meta.MentionsLimit = 3
		o.Meta.ImageOnly = true
		o.Meta.LinksOnly = true
		o.Meta.VideosOnly = true
		o.Meta.AudiosOnly = true
		o.Meta.Forward = true
		o.Meta.Keyboard = true
		o.Meta.UsernameSymbols = "@#$"
		o.Meta.ContactOnly = true
		o.Meta.Giveaway = true

		o.OpenAI.Token = "openai-token"
		o.OpenAI.APIBase = "https://custom.api.com"
		o.OpenAI.Veto = true
		o.OpenAI.Prompt = "custom prompt"
		o.OpenAI.CustomPrompts = []string{"/path/to/prompts"}
		o.OpenAI.Model = "gpt-4"
		o.OpenAI.MaxTokensResponse = 2048
		o.OpenAI.MaxTokensRequest = 32000
		o.OpenAI.MaxSymbolsRequest = 5000
		o.OpenAI.RetryCount = 3
		o.OpenAI.HistorySize = 5
		o.OpenAI.ReasoningEffort = "high"
		o.OpenAI.CheckShortMessages = true

		o.Gemini.Token = "gemini-token"
		o.Gemini.Veto = true
		o.Gemini.Prompt = "gemini prompt"
		o.Gemini.CustomPrompts = []string{"/path/to/gprompts"}
		o.Gemini.Model = "gemma-4"
		o.Gemini.MaxTokensResponse = 1500
		o.Gemini.MaxSymbolsRequest = 4096
		o.Gemini.RetryCount = 2
		o.Gemini.HistorySize = 3
		o.Gemini.CheckShortMessages = true

		o.LLM.Consensus = "all"
		o.LLM.RequestTimeout = 45 * time.Second

		o.Delete.JoinMessages = true
		o.Delete.LeaveMessages = true

		o.Duplicates.Threshold = 3
		o.Duplicates.Window = 2 * time.Hour

		o.Report.Enabled = true
		o.Report.Threshold = 4
		o.Report.AutoBanThreshold = 6
		o.Report.RateLimit = 20
		o.Report.RatePeriod = 30 * time.Minute

		o.LuaPlugins.Enabled = true
		o.LuaPlugins.PluginsDir = "/custom/plugins"
		o.LuaPlugins.EnabledPlugins = []string{"plugin1", "plugin2"}
		o.LuaPlugins.DynamicReload = true

		o.AbnormalSpacing.Enabled = true
		o.AbnormalSpacing.SpaceRatioThreshold = 0.4
		o.AbnormalSpacing.ShortWordRatioThreshold = 0.8
		o.AbnormalSpacing.ShortWordLen = 2
		o.AbnormalSpacing.MinWords = 10

		o.Files.SamplesDataPath = "/samples"
		o.Files.DynamicDataPath = "/dynamic"
		o.Files.WatchInterval = 10 * time.Minute

		o.Message.Startup = "Bot started"
		o.Message.Spam = "Spam detected for %s"
		o.Message.Dry = "Spam detected for %s (dry)"
		o.Message.Warn = "Warning for %s"

		o.Server.Enabled = true
		o.Server.ListenAddr = ":9090"
		o.Server.AuthPasswd = "secret"
		o.Server.AuthHash = "$2a$10$test"

		return o
	}

	tests := []struct {
		name     string
		opts     options
		validate func(t *testing.T, settings *config.Settings)
	}{
		{
			name: "all options converted",
			opts: makeFullOpts(),
			validate: func(t *testing.T, settings *config.Settings) {
				// verify all fields are correctly mapped
				assert.Equal(t, "test-instance", settings.InstanceID)
				assert.Equal(t, 100, settings.MinMsgLen)
				assert.Equal(t, 5, settings.MaxEmoji)
				assert.InEpsilon(t, 0.8, settings.MinSpamProbability, 0.0001)
				assert.InEpsilon(t, 0.9, settings.SimilarityThreshold, 0.0001)
				assert.Equal(t, 3, settings.MultiLangWords)
				assert.True(t, settings.NoSpamReply)
				assert.True(t, settings.SuppressJoinMessage)
				assert.True(t, settings.ParanoidMode)
				assert.Equal(t, 10, settings.FirstMessagesCount)
				assert.True(t, settings.Training)
				assert.True(t, settings.SoftBan)
				assert.Equal(t, "enabled", settings.Convert)
				assert.Equal(t, 5, settings.MaxBackups)
				assert.True(t, settings.Dry)
				assert.True(t, settings.AggressiveCleanup)
				assert.Equal(t, 200, settings.AggressiveCleanupLimit)

				// telegram settings
				assert.Equal(t, "bot-token", settings.Telegram.Token)
				assert.Equal(t, "test-group", settings.Telegram.Group)
				assert.Equal(t, 5*time.Minute, settings.Telegram.IdleDuration)
				assert.Equal(t, 30*time.Second, settings.Telegram.Timeout)

				// admin settings
				assert.Equal(t, "123456", settings.Admin.AdminGroup)
				assert.True(t, settings.Admin.DisableAdminSpamForward)
				assert.Equal(t, []int64{1, 2, 3}, settings.Admin.TestingIDs)
				assert.Equal(t, []string{"user1", "user2"}, settings.Admin.SuperUsers)

				// history settings
				assert.Equal(t, 24*time.Hour, settings.History.Duration)
				assert.Equal(t, 100, settings.History.MinSize)
				assert.Equal(t, 1000, settings.History.Size)

				// logger settings
				assert.True(t, settings.Logger.Enabled)
				assert.Equal(t, "test.log", settings.Logger.FileName)
				assert.Equal(t, "50M", settings.Logger.MaxSize)
				assert.Equal(t, 5, settings.Logger.MaxBackups)

				// cas settings
				assert.Equal(t, "https://cas.example.com", settings.CAS.API)
				assert.Equal(t, 10*time.Second, settings.CAS.Timeout)
				assert.Equal(t, "test-agent", settings.CAS.UserAgent)

				// meta settings
				assert.Equal(t, 2, settings.Meta.LinksLimit)
				assert.Equal(t, 3, settings.Meta.MentionsLimit)
				assert.True(t, settings.Meta.ImageOnly)
				assert.True(t, settings.Meta.LinksOnly)
				assert.True(t, settings.Meta.VideosOnly)
				assert.True(t, settings.Meta.AudiosOnly)
				assert.True(t, settings.Meta.Forward)
				assert.True(t, settings.Meta.Keyboard)
				assert.Equal(t, "@#$", settings.Meta.UsernameSymbols)
				assert.True(t, settings.Meta.ContactOnly)
				assert.True(t, settings.Meta.Giveaway)

				// openai settings
				assert.Equal(t, "openai-token", settings.OpenAI.Token)
				assert.Equal(t, "https://custom.api.com", settings.OpenAI.APIBase)
				assert.True(t, settings.OpenAI.Veto)
				assert.Equal(t, "custom prompt", settings.OpenAI.Prompt)
				assert.Equal(t, []string{"/path/to/prompts"}, settings.OpenAI.CustomPrompts)
				assert.Equal(t, "gpt-4", settings.OpenAI.Model)
				assert.Equal(t, 2048, settings.OpenAI.MaxTokensResponse)
				assert.Equal(t, 32000, settings.OpenAI.MaxTokensRequest)
				assert.Equal(t, 5000, settings.OpenAI.MaxSymbolsRequest)
				assert.Equal(t, 3, settings.OpenAI.RetryCount)
				assert.Equal(t, 5, settings.OpenAI.HistorySize)
				assert.Equal(t, "high", settings.OpenAI.ReasoningEffort)
				assert.True(t, settings.OpenAI.CheckShortMessages)

				// gemini settings
				assert.Equal(t, "gemini-token", settings.Gemini.Token)
				assert.True(t, settings.Gemini.Veto)
				assert.Equal(t, "gemini prompt", settings.Gemini.Prompt)
				assert.Equal(t, []string{"/path/to/gprompts"}, settings.Gemini.CustomPrompts)
				assert.Equal(t, "gemma-4", settings.Gemini.Model)
				assert.Equal(t, int32(1500), settings.Gemini.MaxTokensResponse)
				assert.Equal(t, 4096, settings.Gemini.MaxSymbolsRequest)
				assert.Equal(t, 2, settings.Gemini.RetryCount)
				assert.Equal(t, 3, settings.Gemini.HistorySize)
				assert.True(t, settings.Gemini.CheckShortMessages)

				// llm settings
				assert.Equal(t, "all", settings.LLM.Consensus)
				assert.Equal(t, 45*time.Second, settings.LLM.RequestTimeout)

				// delete settings
				assert.True(t, settings.Delete.JoinMessages)
				assert.True(t, settings.Delete.LeaveMessages)

				// duplicates settings
				assert.Equal(t, 3, settings.Duplicates.Threshold)
				assert.Equal(t, 2*time.Hour, settings.Duplicates.Window)

				// report settings
				assert.True(t, settings.Report.Enabled)
				assert.Equal(t, 4, settings.Report.Threshold)
				assert.Equal(t, 6, settings.Report.AutoBanThreshold)
				assert.Equal(t, 20, settings.Report.RateLimit)
				assert.Equal(t, 30*time.Minute, settings.Report.RatePeriod)

				// lua plugins settings
				assert.True(t, settings.LuaPlugins.Enabled)
				assert.Equal(t, "/custom/plugins", settings.LuaPlugins.PluginsDir)
				assert.Equal(t, []string{"plugin1", "plugin2"}, settings.LuaPlugins.EnabledPlugins)
				assert.True(t, settings.LuaPlugins.DynamicReload)

				// abnormal space settings
				assert.True(t, settings.AbnormalSpace.Enabled)
				assert.InEpsilon(t, 0.4, settings.AbnormalSpace.SpaceRatioThreshold, 0.0001)
				assert.InEpsilon(t, 0.8, settings.AbnormalSpace.ShortWordRatioThreshold, 0.0001)
				assert.Equal(t, 2, settings.AbnormalSpace.ShortWordLen)
				assert.Equal(t, 10, settings.AbnormalSpace.MinWords)

				// files settings
				assert.Equal(t, "/samples", settings.Files.SamplesDataPath)
				assert.Equal(t, "/dynamic", settings.Files.DynamicDataPath)
				assert.Equal(t, 10*60, settings.Files.WatchInterval) // converted to seconds

				// message settings
				assert.Equal(t, "Bot started", settings.Message.Startup)
				assert.Equal(t, "Spam detected for %s", settings.Message.Spam)
				assert.Equal(t, "Spam detected for %s (dry)", settings.Message.Dry)
				assert.Equal(t, "Warning for %s", settings.Message.Warn)

				// server settings (AuthUser removed in this merge; hardcoded to tg-spam elsewhere)
				assert.True(t, settings.Server.Enabled)
				assert.Equal(t, ":9090", settings.Server.ListenAddr)
				assert.Equal(t, "$2a$10$test", settings.Server.AuthHash)

				// transient settings
				assert.Equal(t, "sqlite://test.db", settings.Transient.DataBaseURL)
				assert.Equal(t, 30*time.Second, settings.Transient.StorageTimeout)
				assert.True(t, settings.Transient.ConfigDB)
				assert.Equal(t, "test-key", settings.Transient.ConfigDBEncryptKey)
				assert.True(t, settings.Transient.Dbg)
				assert.True(t, settings.Transient.TGDbg)
				assert.Equal(t, "secret", settings.Transient.WebAuthPasswd)
			},
		},
		{
			name: "default values",
			opts: options{
				InstanceID: "default-instance",
			},
			validate: func(t *testing.T, settings *config.Settings) {
				assert.Equal(t, "default-instance", settings.InstanceID)
				// verify some defaults are applied
				assert.Equal(t, 0, settings.MinMsgLen)
				assert.Equal(t, 0, settings.MaxEmoji)
				assert.False(t, settings.NoSpamReply)
				assert.False(t, settings.ParanoidMode)
				assert.Empty(t, settings.Telegram.Token)
				assert.Empty(t, settings.Gemini.Token)
				assert.False(t, settings.Report.Enabled)
				assert.Equal(t, 0, settings.Duplicates.Threshold)
				assert.False(t, settings.Delete.JoinMessages)
				assert.False(t, settings.AggressiveCleanup)
				assert.False(t, settings.Meta.ContactOnly)
				assert.False(t, settings.Meta.Giveaway)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := optToSettings(tc.opts)
			tc.validate(t, result)
		})
	}
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
				Token:        "test-token", // token directly in domain model
			},
			OpenAI: config.OpenAISettings{
				Token: "", // empty token
			},
			History: config.HistorySettings{
				Size:     200,
				Duration: 24 * time.Hour,
				MinSize:  500,
			},
			Transient: config.TransientSettings{
				DataBaseURL:    dbFile,
				StorageTimeout: 30 * time.Second,
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
		assert.True(t, loadedSettings.Transient.ConfigDB)
		assert.Empty(t, loadedSettings.Transient.StorageTimeout)
	})

	t.Run("verify tokens are stored with domain model", func(t *testing.T) {
		// create test settings with token values in domain models
		settings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Token: "telegram-token",
			},
			OpenAI: config.OpenAISettings{
				Token: "openai-token",
			},
			Transient: config.TransientSettings{
				DataBaseURL: dbFile,
				Dbg:         true, // debug mode enabled for testing
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

		// verify domain model tokens are loaded
		assert.Equal(t, "telegram-token", loadedSettings.Telegram.Token)
		assert.Equal(t, "openai-token", loadedSettings.OpenAI.Token)
	})

	t.Run("verify auth hash is generated and stored", func(t *testing.T) {
		// create test settings with auth password but no hash
		settings := &config.Settings{
			InstanceID: "test-instance",
			Server: config.ServerSettings{
				Enabled:  true,
				AuthHash: "", // no hash provided
			},
			Transient: config.TransientSettings{
				DataBaseURL:   dbFile,
				WebAuthPasswd: "test-password", // password should be hashed
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

		// verify hash in original settings was set
		assert.NotEmpty(t, settings.Server.AuthHash, "Auth hash should be generated in original settings")
		assert.True(t, strings.HasPrefix(settings.Server.AuthHash, "$2a$"),
			"Auth hash should be a bcrypt hash starting with $2a$")

		// verify hash loaded from database
		assert.NotEmpty(t, loadedSettings.Server.AuthHash, "Auth hash should be loaded from database")
		assert.True(t, strings.HasPrefix(loadedSettings.Server.AuthHash, "$2a$"),
			"Loaded auth hash should be a bcrypt hash starting with $2a$")
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
				WebAuthPasswd:  "cli-password",
			},
			Telegram: config.TelegramSettings{
				Group: "cli-group",
				Token: "cli-token",
			},
			OpenAI: config.OpenAISettings{
				Token: "openai-cli-token",
			},
			Server: config.ServerSettings{
				AuthHash: "cli-hash",
			},
		}

		// save original credentials and transient values
		originalTransient := cliSettings.Transient
		telegramToken := cliSettings.Telegram.Token
		openAIToken := cliSettings.OpenAI.Token
		authHash := cliSettings.Server.AuthHash

		// test loading configuration from database
		err = loadConfigFromDB(ctx, cliSettings)
		require.NoError(t, err)

		// restore transient values
		cliSettings.Transient = originalTransient

		// restore credentials directly
		cliSettings.Telegram.Token = telegramToken
		cliSettings.OpenAI.Token = openAIToken
		cliSettings.Server.AuthHash = authHash

		// now verify - CLI values should be preserved
		assert.Equal(t, "test-instance", cliSettings.InstanceID)
		assert.True(t, cliSettings.Transient.Dbg)
		assert.True(t, cliSettings.Transient.TGDbg)
		assert.Equal(t, "cli-password", cliSettings.Transient.WebAuthPasswd)
		assert.Equal(t, "cli-hash", cliSettings.Server.AuthHash)
		assert.Equal(t, "cli-token", cliSettings.Telegram.Token)
		assert.Equal(t, "openai-cli-token", cliSettings.OpenAI.Token)
		assert.Equal(t, 30*time.Second, cliSettings.Transient.StorageTimeout)

		// DB values should be loaded for non-transient fields
		assert.True(t, cliSettings.Dry)                             // from DB
		assert.Equal(t, 5, cliSettings.MultiLangWords)              // from DB
		assert.Equal(t, 48*time.Hour, cliSettings.History.Duration) // from DB
		assert.Equal(t, "db-group", cliSettings.Telegram.Group)     // from DB
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
		require.Error(t, err, "Expected error when trying to save to invalid database path")

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
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load") // matches "failed to load settings from database"
	})

	t.Run("encryption with config db encrypt key", func(t *testing.T) {
		// create test settings with encryption key and sensitive data
		encryptKey := "test-encryption-key-32-chars-long"
		settings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Token: "telegram-secret-token",
			},
			OpenAI: config.OpenAISettings{
				Token: "openai-secret-token",
			},
			Server: config.ServerSettings{
				AuthHash: "$2a$10$secrethash",
			},
			Transient: config.TransientSettings{
				DataBaseURL:        dbFile,
				ConfigDB:           true,
				ConfigDBEncryptKey: encryptKey,
			},
		}

		// save config to DB
		ctx := context.Background()
		err := saveConfigToDB(ctx, settings)
		require.NoError(t, err)

		// load config without encryption key - should fail to decrypt
		loadedSettingsNoKey := &config.Settings{
			InstanceID: "test-instance",
			Transient: config.TransientSettings{
				DataBaseURL:        dbFile,
				ConfigDB:           true,
				ConfigDBEncryptKey: "", // no key
			},
		}

		err = loadConfigFromDB(ctx, loadedSettingsNoKey)
		require.NoError(t, err) // loading succeeds but tokens should be garbled

		// verify tokens are not decrypted properly
		assert.NotEqual(t, "telegram-secret-token", loadedSettingsNoKey.Telegram.Token)
		assert.NotEqual(t, "openai-secret-token", loadedSettingsNoKey.OpenAI.Token)

		// load config with correct encryption key
		loadedSettingsWithKey := &config.Settings{
			InstanceID: "test-instance",
			Transient: config.TransientSettings{
				DataBaseURL:        dbFile,
				ConfigDB:           true,
				ConfigDBEncryptKey: encryptKey,
			},
		}

		err = loadConfigFromDB(ctx, loadedSettingsWithKey)
		require.NoError(t, err)

		// verify tokens are decrypted properly
		assert.Equal(t, "telegram-secret-token", loadedSettingsWithKey.Telegram.Token)
		assert.Equal(t, "openai-secret-token", loadedSettingsWithKey.OpenAI.Token)
		assert.Equal(t, "$2a$10$secrethash", loadedSettingsWithKey.Server.AuthHash)
	})

	t.Run("save and load with CLI overrides", func(t *testing.T) {
		// simulate the flow in main.go where CLI values override database values

		// first, save initial config to database
		dbSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "db-group",
				Token: "db-token",
			},
			Server: config.ServerSettings{
				Enabled:  true,
				AuthHash: "$2a$10$dbhash",
			},
			Transient: config.TransientSettings{
				DataBaseURL:   dbFile,
				WebAuthPasswd: "db-password",
			},
		}

		ctx := context.Background()
		err := saveConfigToDB(ctx, dbSettings)
		require.NoError(t, err)

		// create CLI options with explicit password override
		var cliOpts options
		cliOpts.InstanceID = "test-instance"
		cliOpts.Server.AuthPasswd = "cli-password"

		// load settings from database
		loadedSettings := &config.Settings{
			InstanceID: "test-instance",
			Transient: config.TransientSettings{
				DataBaseURL: dbFile,
				ConfigDB:    true,
			},
		}

		err = loadConfigFromDB(ctx, loadedSettings)
		require.NoError(t, err)

		// apply CLI overrides
		applyCLIOverrides(loadedSettings, cliOpts)

		// verify database values were loaded
		assert.Equal(t, "db-group", loadedSettings.Telegram.Group)
		assert.Equal(t, "db-token", loadedSettings.Telegram.Token)

		// verify CLI override was applied
		assert.Equal(t, "cli-password", loadedSettings.Transient.WebAuthPasswd)
		assert.Empty(t, loadedSettings.Server.AuthHash) // hash cleared when password provided
	})

	t.Run("round-trip all new master feature groups", func(t *testing.T) {
		// use a fresh DB file and encryption key so Gemini.Token round-trips via ENC: prefix
		freshDBFile := filepath.Join(tmpDir, "all-groups.db")
		encryptKey := "test-encryption-key-32-chars-long"

		settings := &config.Settings{
			InstanceID: "test-instance",
			Delete: config.DeleteSettings{
				JoinMessages:  true,
				LeaveMessages: true,
			},
			Meta: config.MetaSettings{
				ContactOnly: true,
				Giveaway:    true,
			},
			Gemini: config.GeminiSettings{
				Token:              "gemini-secret-token",
				Veto:               true,
				Prompt:             "gemini prompt",
				CustomPrompts:      []string{"cp1", "cp2"},
				Model:              "gemini-2.5-flash",
				MaxTokensResponse:  1024,
				MaxSymbolsRequest:  9000,
				RetryCount:         2,
				HistorySize:        5,
				CheckShortMessages: true,
			},
			LLM: config.LLMSettings{
				Consensus:      "all",
				RequestTimeout: 45 * time.Second,
			},
			Duplicates: config.DuplicatesSettings{
				Threshold: 3,
				Window:    2 * time.Minute,
			},
			Report: config.ReportSettings{
				Enabled:          true,
				Threshold:        5,
				AutoBanThreshold: 10,
				RateLimit:        7,
				RatePeriod:       15 * time.Minute,
			},
			AggressiveCleanup:      true,
			AggressiveCleanupLimit: 50,
			Transient: config.TransientSettings{
				DataBaseURL:        freshDBFile,
				ConfigDBEncryptKey: encryptKey,
			},
		}

		ctx := context.Background()
		err := saveConfigToDB(ctx, settings)
		require.NoError(t, err)

		loaded := &config.Settings{
			InstanceID: "test-instance",
			Transient: config.TransientSettings{
				DataBaseURL:        freshDBFile,
				ConfigDB:           true,
				ConfigDBEncryptKey: encryptKey,
			},
		}
		err = loadConfigFromDB(ctx, loaded)
		require.NoError(t, err)

		assert.Equal(t, settings.Delete, loaded.Delete, "Delete group round-trip")
		assert.Equal(t, settings.Meta.ContactOnly, loaded.Meta.ContactOnly)
		assert.Equal(t, settings.Meta.Giveaway, loaded.Meta.Giveaway)
		assert.Equal(t, settings.Gemini, loaded.Gemini, "Gemini group round-trip incl. encrypted token")
		assert.Equal(t, settings.LLM, loaded.LLM, "LLM group round-trip")
		assert.Equal(t, settings.Duplicates, loaded.Duplicates, "Duplicates group round-trip")
		assert.Equal(t, settings.Report, loaded.Report, "Report group round-trip")
		assert.Equal(t, settings.AggressiveCleanup, loaded.AggressiveCleanup)
		assert.Equal(t, settings.AggressiveCleanupLimit, loaded.AggressiveCleanupLimit)
	})
}
