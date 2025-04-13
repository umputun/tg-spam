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

func TestSettings_IsOpenAIEnabled(t *testing.T) {
	tests := []struct {
		name     string
		apiBase  string
		token    string
		expected bool
	}{
		{
			name:     "both empty",
			apiBase:  "",
			token:    "",
			expected: false,
		},
		{
			name:     "only token",
			apiBase:  "",
			token:    "token-123",
			expected: true,
		},
		{
			name:     "only api base",
			apiBase:  "https://api.openai.example.com",
			token:    "",
			expected: true,
		},
		{
			name:     "both present",
			apiBase:  "https://api.openai.example.com",
			token:    "token-123",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.OpenAI.APIBase = tt.apiBase
			s.OpenAI.Token = tt.token
			assert.Equal(t, tt.expected, s.IsOpenAIEnabled())
		})
	}
}

func TestSettings_IsMetaEnabled(t *testing.T) {
	tests := []struct {
		name           string
		imageOnly      bool
		linksLimit     int
		mentionsLimit  int
		linksOnly      bool
		videosOnly     bool
		audiosOnly     bool
		forward        bool
		keyboard       bool
		usernameSymbol string
		expected       bool
	}{
		{
			name:           "all disabled",
			imageOnly:      false,
			linksLimit:     -1,
			mentionsLimit:  -1,
			linksOnly:      false,
			videosOnly:     false,
			audiosOnly:     false,
			forward:        false,
			keyboard:       false,
			usernameSymbol: "",
			expected:       false,
		},
		{
			name:           "imageOnly enabled",
			imageOnly:      true,
			linksLimit:     -1,
			mentionsLimit:  -1,
			linksOnly:      false,
			videosOnly:     false,
			audiosOnly:     false,
			forward:        false,
			keyboard:       false,
			usernameSymbol: "",
			expected:       true,
		},
		{
			name:           "linksLimit enabled",
			imageOnly:      false,
			linksLimit:     3,
			mentionsLimit:  -1,
			linksOnly:      false,
			videosOnly:     false,
			audiosOnly:     false,
			forward:        false,
			keyboard:       false,
			usernameSymbol: "",
			expected:       true,
		},
		{
			name:           "mentionsLimit enabled",
			imageOnly:      false,
			linksLimit:     -1,
			mentionsLimit:  5,
			linksOnly:      false,
			videosOnly:     false,
			audiosOnly:     false,
			forward:        false,
			keyboard:       false,
			usernameSymbol: "",
			expected:       true,
		},
		{
			name:           "linksOnly enabled",
			imageOnly:      false,
			linksLimit:     -1,
			mentionsLimit:  -1,
			linksOnly:      true,
			videosOnly:     false,
			audiosOnly:     false,
			forward:        false,
			keyboard:       false,
			usernameSymbol: "",
			expected:       true,
		},
		{
			name:           "videosOnly enabled",
			imageOnly:      false,
			linksLimit:     -1,
			mentionsLimit:  -1,
			linksOnly:      false,
			videosOnly:     true,
			audiosOnly:     false,
			forward:        false,
			keyboard:       false,
			usernameSymbol: "",
			expected:       true,
		},
		{
			name:           "audiosOnly enabled",
			imageOnly:      false,
			linksLimit:     -1,
			mentionsLimit:  -1,
			linksOnly:      false,
			videosOnly:     false,
			audiosOnly:     true,
			forward:        false,
			keyboard:       false,
			usernameSymbol: "",
			expected:       true,
		},
		{
			name:           "forward enabled",
			imageOnly:      false,
			linksLimit:     -1,
			mentionsLimit:  -1,
			linksOnly:      false,
			videosOnly:     false,
			audiosOnly:     false,
			forward:        true,
			keyboard:       false,
			usernameSymbol: "",
			expected:       true,
		},
		{
			name:           "keyboard enabled",
			imageOnly:      false,
			linksLimit:     -1,
			mentionsLimit:  -1,
			linksOnly:      false,
			videosOnly:     false,
			audiosOnly:     false,
			forward:        false,
			keyboard:       true,
			usernameSymbol: "",
			expected:       true,
		},
		{
			name:           "usernameSymbols enabled",
			imageOnly:      false,
			linksLimit:     -1,
			mentionsLimit:  -1,
			linksOnly:      false,
			videosOnly:     false,
			audiosOnly:     false,
			forward:        false,
			keyboard:       false,
			usernameSymbol: "@$",
			expected:       true,
		},
		{
			name:           "multiple enabled",
			imageOnly:      true,
			linksLimit:     5,
			mentionsLimit:  3,
			linksOnly:      false,
			videosOnly:     false,
			audiosOnly:     false,
			forward:        true,
			keyboard:       false,
			usernameSymbol: "",
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.Meta.ImageOnly = tt.imageOnly
			s.Meta.LinksLimit = tt.linksLimit
			s.Meta.MentionsLimit = tt.mentionsLimit
			s.Meta.LinksOnly = tt.linksOnly
			s.Meta.VideosOnly = tt.videosOnly
			s.Meta.AudiosOnly = tt.audiosOnly
			s.Meta.Forward = tt.forward
			s.Meta.Keyboard = tt.keyboard
			s.Meta.UsernameSymbols = tt.usernameSymbol
			assert.Equal(t, tt.expected, s.IsMetaEnabled())
		})
	}
}

func TestSettings_IsCASEnabled(t *testing.T) {
	tests := []struct {
		name     string
		api      string
		expected bool
	}{
		{
			name:     "disabled",
			api:      "",
			expected: false,
		},
		{
			name:     "enabled",
			api:      "https://api.cas.chat",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.CAS.API = tt.api
			assert.Equal(t, tt.expected, s.IsCASEnabled())
		})
	}
}

func TestSettings_IsStartupMessageEnabled(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected bool
	}{
		{
			name:     "disabled",
			message:  "",
			expected: false,
		},
		{
			name:     "enabled",
			message:  "Bot started",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New()
			s.Message.Startup = tt.message
			assert.Equal(t, tt.expected, s.IsStartupMessageEnabled())
		})
	}
}
