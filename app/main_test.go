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
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-pkgz/rest"
	"github.com/jessevdk/go-flags"
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
	require.Len(t, savedMsgs, 1)
	assert.Equal(t, "Test message blah blah", savedMsgs[0].Text)
	assert.Equal(t, "testuser", savedMsgs[0].UserName)
	assert.Equal(t, int64(123), savedMsgs[0].UserID)
	assert.JSONEq(t, `[{"name":"Check1","spam":true,"details":"Details 1"},{"name":"Check2","spam":false,"details":"Details 2"}]`,
		savedMsgs[0].ChecksJSON)

}

func TestValidateSettings(t *testing.T) {
	tests := []struct {
		name    string
		s       *config.Settings
		wantErr string
	}{
		{
			name:    "all zero is valid (defaults disabled)",
			s:       &config.Settings{},
			wantErr: "",
		},
		{
			name: "report auto-ban threshold below regular threshold",
			s: &config.Settings{
				Report: config.ReportSettings{Threshold: 4, AutoBanThreshold: 2},
			},
			wantErr: "auto-ban-threshold (2) must be >= threshold (4)",
		},
		{
			name: "report auto-ban threshold zero is valid (disabled)",
			s: &config.Settings{
				Report: config.ReportSettings{Threshold: 4, AutoBanThreshold: 0},
			},
			wantErr: "",
		},
		{
			name: "report auto-ban threshold equal to threshold is valid",
			s: &config.Settings{
				Report: config.ReportSettings{Threshold: 4, AutoBanThreshold: 4},
			},
			wantErr: "",
		},
		{
			name: "warn threshold positive but window zero",
			s: &config.Settings{
				Warn: config.WarnSettings{Threshold: 2, Window: 0},
			},
			wantErr: "warn.threshold (2) is set but warn.window (0s) is not positive",
		},
		{
			name: "warn threshold positive but window negative",
			s: &config.Settings{
				Warn: config.WarnSettings{Threshold: 1, Window: -time.Hour},
			},
			wantErr: "warn.threshold (1) is set but warn.window (-1h0m0s) is not positive",
		},
		{
			name: "warn threshold zero with zero window is valid (disabled)",
			s: &config.Settings{
				Warn: config.WarnSettings{Threshold: 0, Window: 0},
			},
			wantErr: "",
		},
		{
			name: "warn threshold positive with positive window is valid",
			s: &config.Settings{
				Warn: config.WarnSettings{Threshold: 3, Window: 24 * time.Hour},
			},
			wantErr: "",
		},
		{
			name: "warn window equal to storage retention is valid",
			s: &config.Settings{
				Warn: config.WarnSettings{Threshold: 2, Window: storage.WarningsRetention},
			},
			wantErr: "",
		},
		{
			name: "warn window above storage retention is rejected",
			s: &config.Settings{
				Warn: config.WarnSettings{Threshold: 2, Window: storage.WarningsRetention + time.Hour},
			},
			wantErr: "exceeds storage retention",
		},
		{
			name: "warn window above storage retention with disabled threshold is valid",
			s: &config.Settings{
				Warn: config.WarnSettings{Threshold: 0, Window: storage.WarningsRetention + time.Hour},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSettings(tt.s)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
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
		execErr := execute(ctx, settings, nil)
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
		execErr := execute(ctx, settings, nil)
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

func Test_normalizeFilePaths(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	wd, err := os.Getwd()
	require.NoError(t, err)

	t.Run("expands tilde and applies samples fallback when empty", func(t *testing.T) {
		s := &config.Settings{Files: config.FilesSettings{DynamicDataPath: "~/tg-data", SamplesDataPath: ""}}
		normalizeFilePaths(s)
		assert.Equal(t, filepath.Join(home, "tg-data"), s.Files.DynamicDataPath)
		assert.Equal(t, filepath.Join(home, "tg-data"), s.Files.SamplesDataPath,
			"empty samples path must inherit the normalized dynamic path")
	})

	t.Run("expands relative dynamic path to absolute", func(t *testing.T) {
		s := &config.Settings{Files: config.FilesSettings{DynamicDataPath: "data", SamplesDataPath: ""}}
		normalizeFilePaths(s)
		assert.Equal(t, filepath.Join(wd, "data"), s.Files.DynamicDataPath)
		assert.Equal(t, filepath.Join(wd, "data"), s.Files.SamplesDataPath)
	})

	t.Run("expands non-empty samples path independently", func(t *testing.T) {
		s := &config.Settings{Files: config.FilesSettings{DynamicDataPath: "/var/dynamic", SamplesDataPath: "~/samples"}}
		normalizeFilePaths(s)
		assert.Equal(t, "/var/dynamic", s.Files.DynamicDataPath)
		assert.Equal(t, filepath.Join(home, "samples"), s.Files.SamplesDataPath,
			"non-empty samples path must be expanded, not replaced by dynamic path")
	})
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

// integration test: verifies the end-to-end flow through activateServer
// produces a non-empty bcrypt hash on settings.Server.AuthHash when both DB
// and CLI start out empty. Uses execute() because activateServer needs a
// fully constructed bot.SpamFilter, locator and DB engine — replicating that
// setup directly is more brittle than reusing the production wiring.
func Test_activateServerEmptyAuthGeneratesBcryptHash(t *testing.T) {
	settings := makeTestSettings()
	settings.Server.Enabled = true
	settings.Server.ListenAddr = ":9991"
	settings.Server.AuthHash = ""
	settings.Transient.WebAuthPasswd = ""
	settings.Transient.AuthFromCLI = false
	settings.InstanceID = "gr1"
	settings.Transient.DataBaseURL = fmt.Sprintf("sqlite://%s", path.Join(t.TempDir(), "tg-spam.db"))
	settings.Files.SamplesDataPath = t.TempDir()
	settings.Files.DynamicDataPath = t.TempDir()
	require.NoError(t, os.WriteFile(path.Join(settings.Files.SamplesDataPath, "spam-samples.txt"), []byte("spam1\n"), 0o644))
	require.NoError(t, os.WriteFile(path.Join(settings.Files.SamplesDataPath, "ham-samples.txt"), []byte("ham1\n"), 0o644))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		execErr := execute(ctx, settings, nil)
		assert.NoError(t, execErr)
		close(done)
	}()

	require.Eventually(t, func() bool {
		resp, getErr := http.Get("http://localhost:9991/ping")
		if getErr != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, time.Second*5, time.Millisecond*100, "server did not start")

	cancel()
	<-done

	assert.NotEmpty(t, settings.Server.AuthHash, "auto-auth fallback should have populated AuthHash with a bcrypt hash")
	assert.True(t, strings.HasPrefix(settings.Server.AuthHash, "$2a$") || strings.HasPrefix(settings.Server.AuthHash, "$2b$"),
		"AuthHash should be a bcrypt hash (got %q)", settings.Server.AuthHash)
}

func TestREADMEAllOptionsMatchesHelp(t *testing.T) {
	// guards drift between CLI flags/env vars and the "All Application Options"
	// block in README.md. parses options struct via go-flags to enumerate every
	// long flag and env var, then asserts each appears in the README block.
	// also verifies the trailing "Available commands" section is present.

	readmeBytes, err := os.ReadFile("../README.md")
	require.NoError(t, err)
	readme := string(readmeBytes)

	// extract the fenced "All Application Options" code block
	const headerMarker = "## All Application Options"
	headerIdx := strings.Index(readme, headerMarker)
	require.NotEqual(t, -1, headerIdx, "README must contain ## All Application Options heading")

	tail := readme[headerIdx+len(headerMarker):]
	openIdx := strings.Index(tail, "```")
	require.NotEqual(t, -1, openIdx, "README must contain a fenced code block after All Application Options")
	tail = tail[openIdx+3:]
	closeIdx := strings.Index(tail, "```")
	require.NotEqual(t, -1, closeIdx, "README options code block must be closed")
	optionsBlock := tail[:closeIdx]

	// build parser identical to main() so option enumeration matches help output
	var opts options
	parser := flags.NewParser(&opts, flags.PrintErrors|flags.PassDoubleDash|flags.HelpFlag)
	parser.SubcommandsOptional = true
	_, err = parser.AddCommand("save-config", "Save current configuration to database",
		"Saves all current settings to the database for future use with --confdb", &struct{}{})
	require.NoError(t, err)

	// walk all groups recursively and collect flags + env vars
	var (
		flagNames = map[string]string{} // long name -> namespace path for diagnostics
		envVars   = map[string]string{}
		groups    []string // group short descriptions used as section headers in help
	)
	var walk func(g *flags.Group, namePrefix, envPrefix string)
	walk = func(g *flags.Group, namePrefix, envPrefix string) {
		if g.Hidden {
			return
		}
		for _, opt := range g.Options() {
			if opt.Hidden || opt.LongName == "" {
				continue
			}
			fullName := opt.LongName
			if namePrefix != "" {
				fullName = namePrefix + "." + opt.LongName
			}
			flagNames["--"+fullName] = fullName
			if opt.EnvDefaultKey != "" {
				envKey := opt.EnvDefaultKey
				if envPrefix != "" {
					envKey = envPrefix + "_" + envKey
				}
				envVars["[$"+envKey+"]"] = fullName
			}
		}
		for _, sub := range g.Groups() {
			if sub.Hidden {
				continue
			}
			subNamePrefix := namePrefix
			if sub.Namespace != "" {
				if subNamePrefix == "" {
					subNamePrefix = sub.Namespace
				} else {
					subNamePrefix = subNamePrefix + "." + sub.Namespace
				}
			}
			subEnvPrefix := envPrefix
			if sub.EnvNamespace != "" {
				if subEnvPrefix == "" {
					subEnvPrefix = sub.EnvNamespace
				} else {
					subEnvPrefix = subEnvPrefix + "_" + sub.EnvNamespace
				}
			}
			if sub.ShortDescription != "" && sub.Namespace != "" {
				groups = append(groups, sub.ShortDescription+":")
			}
			walk(sub, subNamePrefix, subEnvPrefix)
		}
	}
	walk(parser.Group, "", "")

	// every long flag must appear in the README options block
	for flag := range flagNames {
		assert.Contains(t, optionsBlock, flag, "README All Application Options block missing flag %s", flag)
	}

	// every env var must appear in the README options block
	for env := range envVars {
		assert.Contains(t, optionsBlock, env, "README All Application Options block missing env var %s", env)
	}

	// each group header (e.g. "telegram:", "openai:") must be present
	for _, header := range groups {
		assert.Contains(t, optionsBlock, header, "README options block missing group header %q", header)
	}

	// available commands block must include save-config
	require.Contains(t, optionsBlock, "Available commands:",
		`README options block must include "Available commands:" section`)
	require.Regexp(t, `(?m)^\s+save-config\s+Save current configuration to database`,
		optionsBlock, "README options block must list save-config command")

	// reverse direction: catch stale entries in README that no longer exist as flags.
	// extract every "--<long-name>" token from the options block (not in code spans
	// inside descriptions) and assert each is a known flag.
	flagPattern := regexp.MustCompile(`(?m)^\s+(--[a-zA-Z][a-zA-Z0-9.-]*)`)
	for _, match := range flagPattern.FindAllStringSubmatch(optionsBlock, -1) {
		token := match[1]
		assert.Contains(t, flagNames, token,
			"README options block lists flag %s that is not defined in options struct", token)
	}
}
