package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot"
	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/lib"
)

func TestMakeSpamLogger(t *testing.T) {
	file, err := os.CreateTemp(os.TempDir(), "log")
	require.NoError(t, err)
	defer os.Remove(file.Name())

	logger := makeSpamLogger(file)

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
	}

	logger.Save(msg, response)
	file.Close()

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

func Test_autoSaveApprovedUsers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	director := lib.NewDetector(lib.Config{FirstMessageOnly: true})
	count, err := director.LoadApprovedUsers(bytes.NewBufferString("123\n456"))
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	tmpFile, err := os.CreateTemp("", "approved_users")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	db, err := storage.NewSqliteDB(tmpFile.Name())
	require.NoError(t, err)
	wr, err := storage.NewApprovedUsers(db)
	require.NoError(t, err)
	go autoSaveApprovedUsers(ctx, director, wr, time.Millisecond*100)

	spam, _ := director.Check("some message to check, should be fine", "999")
	assert.False(t, spam)
	time.Sleep(time.Millisecond * 300) // let it tick

	fi, err := os.Stat(tmpFile.Name())
	require.NoError(t, err)
	assert.True(t, fi.Size() > 0)

	count, err = director.LoadApprovedUsers(wr)
	require.NoError(t, err)
	assert.Equal(t, 3, count)
}

func Test_makeDetector(t *testing.T) {
	t.Run("no options", func(t *testing.T) {
		var opts options
		res := makeDetector(opts)
		assert.NotNil(t, res)
	})

	t.Run("with options", func(t *testing.T) {
		var opts options
		opts.OpenAI.Token = "123"
		opts.Files.DynamicSpamFile = "/tmp/dynamic_spam.txt"
		opts.Files.DynamicHamFile = "/tmp/dynamic_ham.txt"
		res := makeDetector(opts)
		assert.NotNil(t, res)
		assert.Equal(t, 0, res.FirstMessagesCount)
		assert.Equal(t, true, res.FirstMessageOnly)
	})

	t.Run("with first msgs count", func(t *testing.T) {
		var opts options
		opts.OpenAI.Token = "123"
		opts.Files.DynamicSpamFile = "/tmp/dynamic_spam.txt"
		opts.Files.DynamicHamFile = "/tmp/dynamic_ham.txt"
		opts.FirstMessagesCount = 10
		res := makeDetector(opts)
		assert.NotNil(t, res)
		assert.Equal(t, 10, res.FirstMessagesCount)
		assert.Equal(t, true, res.FirstMessageOnly)
	})

	t.Run("with first msgs count and paranoid", func(t *testing.T) {
		var opts options
		opts.OpenAI.Token = "123"
		opts.Files.DynamicSpamFile = "/tmp/dynamic_spam.txt"
		opts.Files.DynamicHamFile = "/tmp/dynamic_ham.txt"
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

		_, err = os.Create(filepath.Join(tmpDir, "spam.txt"))
		require.NoError(t, err)
		_, err = os.Create(filepath.Join(tmpDir, "ham.txt"))
		require.NoError(t, err)
		_, err = os.Create(filepath.Join(tmpDir, "exclude.txt"))
		require.NoError(t, err)

		opts.Files.SamplesSpamFile = filepath.Join(tmpDir, "spam.txt")
		opts.Files.SamplesHamFile = filepath.Join(tmpDir, "ham.txt")
		opts.Files.ExcludeTokenFile = filepath.Join(tmpDir, "exclude.txt")

		res, err := makeSpamBot(ctx, opts, makeDetector(opts))
		assert.NoError(t, err)
		assert.NotNil(t, res)
	})
}
