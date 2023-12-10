package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot"
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
		Text: "Test message",
	}

	response := &bot.Response{
		Text: "Test message",
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
		assert.Equal(t, "Test message", logEntry["text"])
	}

	assert.NoError(t, scanner.Err())
}

func TestMakeSpamLogWriter(t *testing.T) {
	setupLog(true, "super-secret-token")
	t.Run("happy path", func(t *testing.T) {
		file, err := os.CreateTemp(os.TempDir(), "log")
		require.NoError(t, err)
		defer os.Remove(file.Name())

		opts.Logger.Enabled = true
		opts.Logger.FileName = file.Name()
		opts.Logger.MaxSize = "1M"
		opts.Logger.MaxBackups = 1

		writer, err := makeSpamLogWriter()
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
		opts.Logger.Enabled = true
		opts.Logger.FileName = "/tmp"
		opts.Logger.MaxSize = "1f"
		opts.Logger.MaxBackups = 1
		writer, err := makeSpamLogWriter()
		assert.Error(t, err)
		t.Log(err)
		assert.Nil(t, writer)
	})

	t.Run("disabled", func(t *testing.T) {
		opts.Logger.Enabled = false
		opts.Logger.FileName = "/tmp"
		opts.Logger.MaxSize = "10M"
		opts.Logger.MaxBackups = 1
		writer, err := makeSpamLogWriter()
		assert.NoError(t, err)
		assert.IsType(t, nopWriteCloser{}, writer)
	})
}
