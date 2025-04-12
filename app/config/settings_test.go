package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSettings_JSON(t *testing.T) {
	s := New()
	s.InstanceID = "test-instance"
	s.SimilarityThreshold = 0.75
	s.Telegram.Group = "test-group"
	s.Telegram.Timeout = 30 * time.Second
	s.Server.Enabled = true
	s.Server.ListenAddr = ":9000"
	s.Message.Spam = "test spam message"

	// add credentials that should not be serialized
	s.Transient.Credentials.TelegramToken = "secret-token"
	s.Transient.Credentials.OpenAIToken = "secret-openai-token"
	s.Transient.Credentials.WebAuthHash = "secret-hash"
	s.Transient.ConfigDB = true
	s.Transient.Dbg = true

	// marshal to JSON
	data, err := json.Marshal(s)
	require.NoError(t, err)

	// ensure sensitive data is not included
	jsonStr := string(data)
	assert.Contains(t, jsonStr, "test-instance")
	assert.Contains(t, jsonStr, "test-group")
	assert.Contains(t, jsonStr, "test spam message")
	assert.NotContains(t, jsonStr, "secret-token")
	assert.NotContains(t, jsonStr, "secret-openai-token")
	assert.NotContains(t, jsonStr, "secret-hash")
	assert.NotContains(t, jsonStr, "ConfigDB")
	assert.NotContains(t, jsonStr, "Dbg")

	// unmarshal back to settings
	var s2 Settings
	require.NoError(t, json.Unmarshal(data, &s2))

	// check that fields match
	assert.Equal(t, "test-instance", s2.InstanceID)
	assert.Equal(t, "test-group", s2.Telegram.Group)
	assert.Equal(t, 30*time.Second, s2.Telegram.Timeout)
	assert.Equal(t, ":9000", s2.Server.ListenAddr)
	assert.Equal(t, "test spam message", s2.Message.Spam)
	assert.True(t, s2.Server.Enabled)
	assert.Equal(t, 0.75, s2.SimilarityThreshold)

	// credentials should be empty in unmarshaled object
	assert.Empty(t, s2.Transient.Credentials.TelegramToken)
	assert.Empty(t, s2.Transient.Credentials.OpenAIToken)
	assert.Empty(t, s2.Transient.Credentials.WebAuthHash)
	assert.False(t, s2.Transient.ConfigDB)
	assert.False(t, s2.Transient.Dbg)
}

func TestSettings_Credentials(t *testing.T) {
	s := New()

	// set and get credentials
	creds := Credentials{
		TelegramToken: "telegram-123",
		OpenAIToken:   "openai-456",
		WebAuthHash:   "hash-789",
		WebAuthPasswd: "passwd-xyz",
	}

	s.SetCredentials(creds)
	gotCreds := s.GetCredentials()

	assert.Equal(t, creds, gotCreds)
}
