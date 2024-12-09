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

	"github.com/jmoiron/sqlx"
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

	db, err := sqlx.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	logger, err := makeSpamLogger(file, db)
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
		_, err := makeSpamBot(ctx, opts, nil)
		assert.Error(t, err)
	})

	t.Run("with valid options", func(t *testing.T) {
		var opts options
		tmpDir, err := os.MkdirTemp("", "spambot_main_test")
		require.NoError(t, err)
		defer os.RemoveAll(tmpDir)

		_, err = os.Create(filepath.Join(tmpDir, samplesSpamFile))
		require.NoError(t, err)
		_, err = os.Create(filepath.Join(tmpDir, samplesHamFile))
		require.NoError(t, err)
		_, err = os.Create(filepath.Join(tmpDir, excludeTokensFile))
		require.NoError(t, err)

		opts.Files.SamplesDataPath = tmpDir
		detector := makeDetector(opts)
		res, err := makeSpamBot(ctx, opts, detector)
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
	opts.Files.SamplesDataPath = "webapi/testdata"
	opts.Files.DynamicDataPath = "webapi/testdata"

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
