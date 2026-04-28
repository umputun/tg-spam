package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
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
	assert.InEpsilon(t, 0.75, s2.SimilarityThreshold, 0.0001)

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
		contactOnly    bool
		giveaway       bool
		expected       bool
	}{
		{name: "all disabled", linksLimit: -1, mentionsLimit: -1, expected: false},
		{name: "imageOnly enabled", imageOnly: true, linksLimit: -1, mentionsLimit: -1, expected: true},
		{name: "linksLimit enabled", linksLimit: 3, mentionsLimit: -1, expected: true},
		{name: "mentionsLimit enabled", linksLimit: -1, mentionsLimit: 5, expected: true},
		{name: "linksOnly enabled", linksLimit: -1, mentionsLimit: -1, linksOnly: true, expected: true},
		{name: "videosOnly enabled", linksLimit: -1, mentionsLimit: -1, videosOnly: true, expected: true},
		{name: "audiosOnly enabled", linksLimit: -1, mentionsLimit: -1, audiosOnly: true, expected: true},
		{name: "forward enabled", linksLimit: -1, mentionsLimit: -1, forward: true, expected: true},
		{name: "keyboard enabled", linksLimit: -1, mentionsLimit: -1, keyboard: true, expected: true},
		{name: "usernameSymbols enabled", linksLimit: -1, mentionsLimit: -1, usernameSymbol: "@$", expected: true},
		{name: "contactOnly enabled", linksLimit: -1, mentionsLimit: -1, contactOnly: true, expected: true},
		{name: "giveaway enabled", linksLimit: -1, mentionsLimit: -1, giveaway: true, expected: true},
		{name: "multiple enabled", imageOnly: true, linksLimit: 5, mentionsLimit: 3, forward: true, expected: true},
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
			s.Meta.ContactOnly = tt.contactOnly
			s.Meta.Giveaway = tt.giveaway
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

// newPopulatedSettings returns a Settings with non-zero values in every
// newly-added group so round-trip tests can detect dropped fields.
func newPopulatedSettings() *Settings {
	s := New()
	s.Delete.JoinMessages = true
	s.Delete.LeaveMessages = true

	s.Meta.ContactOnly = true
	s.Meta.Giveaway = true

	s.Gemini.Token = "gemini-secret"
	s.Gemini.Veto = true
	s.Gemini.Prompt = "gemini-prompt"
	s.Gemini.CustomPrompts = []string{"g1", "g2"}
	s.Gemini.Model = "gemini-pro"
	s.Gemini.MaxTokensResponse = 1024
	s.Gemini.MaxSymbolsRequest = 2048
	s.Gemini.RetryCount = 3
	s.Gemini.HistorySize = 5
	s.Gemini.CheckShortMessages = true

	s.LLM.Consensus = "all"
	s.LLM.RequestTimeout = 45 * time.Second

	s.Duplicates.Threshold = 7
	s.Duplicates.Window = 2 * time.Minute

	s.Reactions.MaxReactions = 5
	s.Reactions.Window = 30 * time.Minute

	s.Report.Enabled = true
	s.Report.Threshold = 4
	s.Report.AutoBanThreshold = 10
	s.Report.RateLimit = 5
	s.Report.RatePeriod = 90 * time.Second

	s.Warn.Threshold = 3
	s.Warn.Window = 12 * time.Hour

	s.AggressiveCleanup = true
	s.AggressiveCleanupLimit = 50

	return s
}

func TestSettings_JSONRoundTrip_NewGroups(t *testing.T) {
	original := newPopulatedSettings()

	data, err := json.Marshal(original)
	require.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, `"delete"`)
	assert.Contains(t, jsonStr, `"gemini"`)
	assert.Contains(t, jsonStr, `"llm"`)
	assert.Contains(t, jsonStr, `"duplicates"`)
	assert.Contains(t, jsonStr, `"reactions"`)
	assert.Contains(t, jsonStr, `"report"`)
	assert.Contains(t, jsonStr, `"warn"`)
	assert.Contains(t, jsonStr, `"aggressive_cleanup"`)
	assert.Contains(t, jsonStr, `"aggressive_cleanup_limit"`)
	assert.Contains(t, jsonStr, `"contact_only"`)
	assert.Contains(t, jsonStr, `"giveaway"`)

	var restored Settings
	require.NoError(t, json.Unmarshal(data, &restored))

	assert.Equal(t, original.Delete, restored.Delete)
	assert.Equal(t, original.Gemini, restored.Gemini)
	assert.Equal(t, original.LLM, restored.LLM)
	assert.Equal(t, original.Duplicates, restored.Duplicates)
	assert.Equal(t, original.Reactions, restored.Reactions)
	assert.Equal(t, original.Report, restored.Report)
	assert.Equal(t, original.Warn, restored.Warn)
	assert.Equal(t, original.AggressiveCleanup, restored.AggressiveCleanup)
	assert.Equal(t, original.AggressiveCleanupLimit, restored.AggressiveCleanupLimit)
	assert.True(t, restored.Meta.ContactOnly)
	assert.True(t, restored.Meta.Giveaway)
}

func TestSettings_YAMLRoundTrip_NewGroups(t *testing.T) {
	original := newPopulatedSettings()

	data, err := yaml.Marshal(original)
	require.NoError(t, err)

	yamlStr := string(data)
	assert.Contains(t, yamlStr, "delete:")
	assert.Contains(t, yamlStr, "gemini:")
	assert.Contains(t, yamlStr, "llm:")
	assert.Contains(t, yamlStr, "duplicates:")
	assert.Contains(t, yamlStr, "reactions:")
	assert.Contains(t, yamlStr, "report:")
	assert.Contains(t, yamlStr, "warn:")
	assert.Contains(t, yamlStr, "aggressive_cleanup:")
	assert.Contains(t, yamlStr, "aggressive_cleanup_limit:")
	assert.Contains(t, yamlStr, "contact_only:")
	assert.Contains(t, yamlStr, "giveaway:")

	var restored Settings
	require.NoError(t, yaml.Unmarshal(data, &restored))

	assert.Equal(t, original.Delete, restored.Delete)
	assert.Equal(t, original.Gemini, restored.Gemini)
	assert.Equal(t, original.LLM, restored.LLM)
	assert.Equal(t, original.Duplicates, restored.Duplicates)
	assert.Equal(t, original.Reactions, restored.Reactions)
	assert.Equal(t, original.Report, restored.Report)
	assert.Equal(t, original.Warn, restored.Warn)
	assert.Equal(t, original.AggressiveCleanup, restored.AggressiveCleanup)
	assert.Equal(t, original.AggressiveCleanupLimit, restored.AggressiveCleanupLimit)
	assert.True(t, restored.Meta.ContactOnly)
	assert.True(t, restored.Meta.Giveaway)
}

func TestSettings_ApplyDefaults_FillsZeroFromTemplate(t *testing.T) {
	target := New()
	template := &Settings{
		Server: ServerSettings{ListenAddr: ":8080"},
		LLM:    LLMSettings{RequestTimeout: 30 * time.Second},
		OpenAI: OpenAISettings{MaxSymbolsRequest: 16000},
	}

	target.ApplyDefaults(template)

	assert.Equal(t, ":8080", target.Server.ListenAddr)
	assert.Equal(t, 30*time.Second, target.LLM.RequestTimeout)
	assert.Equal(t, 16000, target.OpenAI.MaxSymbolsRequest)
}

func TestSettings_ApplyDefaults_PreservesTargetNonZero(t *testing.T) {
	target := New()
	target.Server.ListenAddr = ":9090"
	target.LLM.RequestTimeout = 90 * time.Second
	target.OpenAI.MaxSymbolsRequest = 32000

	template := &Settings{
		Server: ServerSettings{ListenAddr: ":8080"},
		LLM:    LLMSettings{RequestTimeout: 30 * time.Second},
		OpenAI: OpenAISettings{MaxSymbolsRequest: 16000},
	}

	target.ApplyDefaults(template)

	assert.Equal(t, ":9090", target.Server.ListenAddr)
	assert.Equal(t, 90*time.Second, target.LLM.RequestTimeout)
	assert.Equal(t, 32000, target.OpenAI.MaxSymbolsRequest)
}

func TestSettings_ApplyDefaults_SkipsZeroAware(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(target, template *Settings)
		assertFn func(t *testing.T, target *Settings)
	}{
		{
			name: "Meta.LinksLimit",
			setup: func(target, template *Settings) {
				target.Meta.LinksLimit = 0
				template.Meta.LinksLimit = -1
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.Meta.LinksLimit) },
		},
		{
			name: "Meta.MentionsLimit",
			setup: func(target, template *Settings) {
				target.Meta.MentionsLimit = 0
				template.Meta.MentionsLimit = -1
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.Meta.MentionsLimit) },
		},
		{
			name: "MaxEmoji",
			setup: func(target, template *Settings) {
				target.MaxEmoji = 0
				template.MaxEmoji = 2
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.MaxEmoji) },
		},
		{
			name: "MultiLangWords",
			setup: func(target, template *Settings) {
				target.MultiLangWords = 0
				template.MultiLangWords = 5
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.MultiLangWords) },
		},
		{
			name: "MaxBackups",
			setup: func(target, template *Settings) {
				target.MaxBackups = 0
				template.MaxBackups = 10
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.MaxBackups) },
		},
		{
			name: "Reactions.MaxReactions",
			setup: func(target, template *Settings) {
				target.Reactions.MaxReactions = 0
				template.Reactions.MaxReactions = 3
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.Reactions.MaxReactions) },
		},
		{
			name: "Duplicates.Threshold",
			setup: func(target, template *Settings) {
				target.Duplicates.Threshold = 0
				template.Duplicates.Threshold = 5
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.Duplicates.Threshold) },
		},
		{
			name: "Report.AutoBanThreshold",
			setup: func(target, template *Settings) {
				target.Report.AutoBanThreshold = 0
				template.Report.AutoBanThreshold = 4
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.Report.AutoBanThreshold) },
		},
		{
			name: "Warn.Threshold",
			setup: func(target, template *Settings) {
				target.Warn.Threshold = 0
				template.Warn.Threshold = 3
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.Warn.Threshold) },
		},
		{
			name: "Report.RateLimit",
			setup: func(target, template *Settings) {
				target.Report.RateLimit = 0
				template.Report.RateLimit = 10
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.Report.RateLimit) },
		},
		{
			name: "OpenAI.HistorySize",
			setup: func(target, template *Settings) {
				target.OpenAI.HistorySize = 0
				template.OpenAI.HistorySize = 5
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.OpenAI.HistorySize) },
		},
		{
			name: "Gemini.HistorySize",
			setup: func(target, template *Settings) {
				target.Gemini.HistorySize = 0
				template.Gemini.HistorySize = 5
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.Gemini.HistorySize) },
		},
		{
			name: "FirstMessagesCount",
			setup: func(target, template *Settings) {
				target.FirstMessagesCount = 0
				template.FirstMessagesCount = 1
			},
			assertFn: func(t *testing.T, target *Settings) { assert.Equal(t, 0, target.FirstMessagesCount) },
		},
		{
			name: "SimilarityThreshold",
			setup: func(target, template *Settings) {
				target.SimilarityThreshold = 0
				template.SimilarityThreshold = 0.5
			},
			assertFn: func(t *testing.T, target *Settings) { assert.InDelta(t, 0.0, target.SimilarityThreshold, 0.0001) },
		},
		{
			name: "MinSpamProbability",
			setup: func(target, template *Settings) {
				target.MinSpamProbability = 0
				template.MinSpamProbability = 50
			},
			assertFn: func(t *testing.T, target *Settings) { assert.InDelta(t, 0.0, target.MinSpamProbability, 0.0001) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := New()
			template := &Settings{}
			tt.setup(target, template)
			target.ApplyDefaults(template)
			tt.assertFn(t, target)
		})
	}
}

func TestSettings_ApplyDefaults_FillsWarnWindow(t *testing.T) {
	// warn.Window is NOT zero-aware: zero is invalid (caught by startup
	// validation), so the regular merge semantics must fill it from the
	// template when the persisted blob has zero.
	target := New()
	target.Warn.Threshold = 5 // non-zero (preserved either way)
	template := &Settings{
		Warn: WarnSettings{
			Threshold: 1,
			Window:    720 * time.Hour,
		},
	}

	target.ApplyDefaults(template)

	assert.Equal(t, 5, target.Warn.Threshold, "non-zero target threshold preserved")
	assert.Equal(t, 720*time.Hour, target.Warn.Window, "zero target window filled from template")
}

func TestSettings_ApplyDefaults_NestedStructs(t *testing.T) {
	target := New()
	template := &Settings{
		Telegram: TelegramSettings{
			Timeout:      30 * time.Second,
			IdleDuration: 30 * time.Second,
		},
		OpenAI: OpenAISettings{
			Model:             "gpt-4o-mini",
			MaxTokensResponse: 1024,
			MaxTokensRequest:  2048,
			RetryCount:        1,
		},
		Gemini: GeminiSettings{
			Model:             "gemma-4-31b-it",
			MaxTokensResponse: 1024,
			MaxSymbolsRequest: 8192,
			RetryCount:        1,
		},
		Report: ReportSettings{
			Threshold:  2,
			RatePeriod: time.Hour,
		},
	}

	target.ApplyDefaults(template)

	assert.Equal(t, 30*time.Second, target.Telegram.Timeout)
	assert.Equal(t, 30*time.Second, target.Telegram.IdleDuration)
	assert.Equal(t, "gpt-4o-mini", target.OpenAI.Model)
	assert.Equal(t, 1024, target.OpenAI.MaxTokensResponse)
	assert.Equal(t, 2048, target.OpenAI.MaxTokensRequest)
	assert.Equal(t, 1, target.OpenAI.RetryCount)
	assert.Equal(t, "gemma-4-31b-it", target.Gemini.Model)
	assert.Equal(t, int32(1024), target.Gemini.MaxTokensResponse)
	assert.Equal(t, 8192, target.Gemini.MaxSymbolsRequest)
	assert.Equal(t, 1, target.Gemini.RetryCount)
	assert.Equal(t, 2, target.Report.Threshold)
	assert.Equal(t, time.Hour, target.Report.RatePeriod)
}

func TestSettings_ApplyDefaults_TransientNotTouched(t *testing.T) {
	target := New()
	template := &Settings{
		Transient: TransientSettings{
			DataBaseURL:        "tg-spam.db",
			StorageTimeout:     5 * time.Second,
			ConfigDB:           true,
			Dbg:                true,
			TGDbg:              true,
			ConfigDBEncryptKey: "secret",
			WebAuthPasswd:      "auto",
			AuthFromCLI:        true,
		},
	}

	target.ApplyDefaults(template)

	assert.Empty(t, target.Transient.DataBaseURL)
	assert.Equal(t, time.Duration(0), target.Transient.StorageTimeout)
	assert.False(t, target.Transient.ConfigDB)
	assert.False(t, target.Transient.Dbg)
	assert.False(t, target.Transient.TGDbg)
	assert.Empty(t, target.Transient.ConfigDBEncryptKey)
	assert.Empty(t, target.Transient.WebAuthPasswd)
	assert.False(t, target.Transient.AuthFromCLI)
}

func TestSettings_ApplyDefaults_NilTemplate(t *testing.T) {
	target := New()
	target.Server.ListenAddr = ":9090"

	target.ApplyDefaults(nil)

	assert.Equal(t, ":9090", target.Server.ListenAddr)
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
