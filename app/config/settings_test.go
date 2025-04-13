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

	// add credentials directly to domain fields
	s.Telegram.Token = "secret-token"
	s.OpenAI.Token = "secret-openai-token"
	s.Server.AuthHash = "secret-hash"
	s.Transient.ConfigDB = true
	s.Transient.Dbg = true

	// marshal to JSON
	data, err := json.Marshal(s)
	require.NoError(t, err)

	// check that data is correctly serialized
	jsonStr := string(data)
	assert.Contains(t, jsonStr, "test-instance")
	assert.Contains(t, jsonStr, "test-group")
	assert.Contains(t, jsonStr, "test spam message")
	// sensitive data should be included since it's part of the domain model
	assert.Contains(t, jsonStr, "secret-token")
	assert.Contains(t, jsonStr, "secret-openai-token")
	assert.Contains(t, jsonStr, "secret-hash")
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

	// credentials should be preserved in unmarshaled object
	assert.Equal(t, "secret-token", s2.Telegram.Token)
	assert.Equal(t, "secret-openai-token", s2.OpenAI.Token)
	assert.Equal(t, "secret-hash", s2.Server.AuthHash)
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
