package tgspam

import (
	"context"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam/mocks"
)

func TestGeminiChecker_Check(t *testing.T) {
	clientMock := &mocks.GeminiClientMock{
		GenerateContentFunc: func(ctx context.Context, model string, contents []*genai.Content,
			config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: ""}}}}},
			}, nil
		},
	}

	checker := newGeminiChecker(clientMock, GeminiConfig{
		MaxOutputTokens:   300,
		MaxSymbolsRequest: 12000,
		Model:             "gemma-4-31b-it",
	})

	t.Run("spam response", func(t *testing.T) {
		clientMock.GenerateContentFunc = func(ctx context.Context, model string, contents []*genai.Content,
			config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{
					Content: &genai.Content{Parts: []*genai.Part{{Text: `{"spam": true, "reason":"bad text", "confidence":100}`}}},
				}},
			}, nil
		}
		spam, details := checker.check("some text", nil)
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.True(t, spam)
		assert.Equal(t, "gemini", details.Name)
		assert.Equal(t, "bad text, confidence: 100%", details.Details)
		assert.NoError(t, details.Error)
	})

	t.Run("not spam response", func(t *testing.T) {
		clientMock.GenerateContentFunc = func(ctx context.Context, model string, contents []*genai.Content,
			config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{
					Content: &genai.Content{Parts: []*genai.Part{{Text: `{"spam": false, "reason":"good text", "confidence":99}`}}},
				}},
			}, nil
		}
		spam, details := checker.check("some text", nil)
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.False(t, spam)
		assert.Equal(t, "gemini", details.Name)
		assert.Equal(t, "good text, confidence: 99%", details.Details)
		assert.NoError(t, details.Error)
	})

	t.Run("error response", func(t *testing.T) {
		clientMock.GenerateContentFunc = func(ctx context.Context, model string, contents []*genai.Content,
			config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return nil, assert.AnError
		}
		spam, details := checker.check("some text", nil)
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.False(t, spam)
		assert.Equal(t, "gemini", details.Name)
		assert.Equal(t, "Gemini error: failed to generate content: assert.AnError general error for testing", details.Details)
		assert.Equal(t, "failed to generate content: assert.AnError general error for testing", details.Error.Error())
	})

	t.Run("bad encoding", func(t *testing.T) {
		clientMock.GenerateContentFunc = func(ctx context.Context, model string, contents []*genai.Content,
			config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{
					Content: &genai.Content{Parts: []*genai.Part{{Text: `bad json`}}},
				}},
			}, nil
		}
		spam, details := checker.check("some text", nil)
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.False(t, spam)
		assert.Equal(t, "gemini", details.Name)
		assert.Equal(t, "Gemini error: can't unmarshal response: bad json - invalid character 'b' looking for beginning of value",
			details.Details)
		assert.Equal(t, "can't unmarshal response: bad json - invalid character 'b' looking for beginning of value",
			details.Error.Error())
	})

	t.Run("no candidates", func(t *testing.T) {
		clientMock.GenerateContentFunc = func(ctx context.Context, model string, contents []*genai.Content,
			config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return &genai.GenerateContentResponse{}, nil
		}
		spam, details := checker.check("some text", nil)
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.False(t, spam)
		assert.Equal(t, "gemini", details.Name)
		assert.Equal(t, "Gemini error: no candidates in response", details.Details)
	})

	t.Run("nil client", func(t *testing.T) {
		nilChecker := newGeminiChecker(nil, GeminiConfig{})
		spam, details := nilChecker.check("some text", nil)
		assert.False(t, spam)
		assert.Equal(t, spamcheck.Response{}, details)
	})
}

func TestGeminiChecker_CheckWithHistory(t *testing.T) {
	clientMock := &mocks.GeminiClientMock{
		GenerateContentFunc: func(ctx context.Context, model string, contents []*genai.Content,
			config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			// verify the request contains history
			assert.Len(t, contents, 1)
			assert.Contains(t, contents[0].Parts[0].Text, "History:")
			assert.Contains(t, contents[0].Parts[0].Text, `"user1": "first message"`)
			assert.Contains(t, contents[0].Parts[0].Text, `"user2": "second message"`)
			assert.Contains(t, contents[0].Parts[0].Text, `"user1": "third message"`)

			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{
					Content: &genai.Content{Parts: []*genai.Part{
						{Text: `{"spam": true, "reason":"suspicious pattern in history", "confidence":90}`},
					}},
				}},
			}, nil
		},
	}

	checker := newGeminiChecker(clientMock, GeminiConfig{Model: "gemma-4-31b-it"})
	history := []spamcheck.Request{
		{Msg: "first message", UserName: "user1"},
		{Msg: "second message", UserName: "user2"},
		{Msg: "third message", UserName: "user1"},
	}

	spam, details := checker.check("current message", history)
	t.Logf("spam: %v, details: %+v", spam, details)
	assert.True(t, spam)
	assert.Equal(t, "gemini", details.Name)
	assert.Equal(t, "suspicious pattern in history, confidence: 90%", details.Details)
	require.NoError(t, details.Error)
	assert.Len(t, clientMock.GenerateContentCalls(), 1)
}

func TestGeminiChecker_SafetySettings(t *testing.T) {
	var capturedConfig *genai.GenerateContentConfig
	clientMock := &mocks.GeminiClientMock{
		GenerateContentFunc: func(ctx context.Context, model string, contents []*genai.Content,
			config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			capturedConfig = config
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{
					Content: &genai.Content{Parts: []*genai.Part{{Text: `{"spam": false, "reason":"test", "confidence":50}`}}},
				}},
			}, nil
		},
	}

	checker := newGeminiChecker(clientMock, GeminiConfig{Model: "gemma-4-31b-it"})
	checker.check("test message", nil)

	require.NotNil(t, capturedConfig)
	require.Len(t, capturedConfig.SafetySettings, 4)
	for _, ss := range capturedConfig.SafetySettings {
		assert.Equal(t, genai.HarmBlockThresholdOff, ss.Threshold, "safety should be OFF for category %s", ss.Category)
	}
}

func TestGeminiChecker_CustomPrompts(t *testing.T) {
	var capturedConfig *genai.GenerateContentConfig
	clientMock := &mocks.GeminiClientMock{
		GenerateContentFunc: func(ctx context.Context, model string, contents []*genai.Content,
			config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			capturedConfig = config
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{
					Content: &genai.Content{Parts: []*genai.Part{{Text: `{"spam": false, "reason":"ok", "confidence":10}`}}},
				}},
			}, nil
		},
	}

	checker := newGeminiChecker(clientMock, GeminiConfig{
		Model:         "gemma-4-31b-it",
		CustomPrompts: []string{"check for crypto scams", "check for fake giveaways"},
	})
	checker.check("test message", nil)

	require.NotNil(t, capturedConfig)
	require.NotNil(t, capturedConfig.SystemInstruction)
	sysText := capturedConfig.SystemInstruction.Parts[0].Text
	assert.Contains(t, sysText, "Also, specifically check for these patterns:")
	assert.Contains(t, sysText, "1. check for crypto scams")
	assert.Contains(t, sysText, "2. check for fake giveaways")
}

func TestGeminiChecker_Defaults(t *testing.T) {
	checker := newGeminiChecker(nil, GeminiConfig{})
	assert.Equal(t, "gemma-4-31b-it", checker.params.Model)
	assert.Equal(t, int32(1024), checker.params.MaxOutputTokens)
	assert.Equal(t, 8192, checker.params.MaxSymbolsRequest)
	assert.Equal(t, 1, checker.params.RetryCount)
	assert.Equal(t, defaultPrompt, checker.params.SystemPrompt)
}

func TestGeminiChecker_RequestTruncation(t *testing.T) {
	var capturedContents []*genai.Content
	clientMock := &mocks.GeminiClientMock{
		GenerateContentFunc: func(ctx context.Context, model string, contents []*genai.Content,
			config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			capturedContents = contents
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{
					Content: &genai.Content{Parts: []*genai.Part{{Text: `{"spam": false, "reason":"ok", "confidence":10}`}}},
				}},
			}, nil
		},
	}

	checker := newGeminiChecker(clientMock, GeminiConfig{MaxSymbolsRequest: 10})
	longMsg := "this is a very long message that should be truncated"
	checker.check(longMsg, nil)

	require.Len(t, capturedContents, 1)
	assert.Len(t, capturedContents[0].Parts[0].Text, 10)
}

func TestGeminiChecker_ResponseWithThoughtTags(t *testing.T) {
	clientMock := &mocks.GeminiClientMock{
		GenerateContentFunc: func(ctx context.Context, model string, contents []*genai.Content,
			config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{
					Content: &genai.Content{Parts: []*genai.Part{
						{Text: `<thought>analyzing the message</thought>{"spam": true, "reason":"crypto scam", "confidence":95}`},
					}},
				}},
			}, nil
		},
	}

	checker := newGeminiChecker(clientMock, GeminiConfig{Model: "gemma-4-31b-it"})
	spam, details := checker.check("buy crypto now", nil)
	assert.True(t, spam)
	assert.Equal(t, "gemini", details.Name)
	assert.Equal(t, "crypto scam, confidence: 95%", details.Details)
}

func TestGeminiChecker_TruncateUTF8(t *testing.T) {
	var capturedMsg string
	clientMock := &mocks.GeminiClientMock{
		GenerateContentFunc: func(ctx context.Context, model string, contents []*genai.Content,
			config *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error) {
			capturedMsg = contents[0].Parts[0].Text
			return &genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{
					Content: &genai.Content{Parts: []*genai.Part{{Text: `{"spam": false, "reason":"ok", "confidence":100}`}}},
				}},
			}, nil
		},
	}

	// 'Привет' is 12 bytes in UTF-8 (2 bytes per char)
	// '🌞' is 4 bytes
	msg := "Привет🌞"

	t.Run("truncate in middle of 2-byte char", func(t *testing.T) {
		checker := newGeminiChecker(clientMock, GeminiConfig{
			MaxSymbolsRequest: 1, // Will take 1 rune
		})
		checker.check(msg, nil)
		assert.True(t, utf8.ValidString(capturedMsg), "Truncated string should be valid UTF-8. Got: %x", capturedMsg)
		assert.Equal(t, "П", capturedMsg)
	})

	t.Run("truncate in middle of 4-byte emoji", func(t *testing.T) {
		checker := newGeminiChecker(clientMock, GeminiConfig{
			MaxSymbolsRequest: 7, // 6 runes for 'Привет' + 1 rune for '🌞'
		})
		checker.check(msg, nil)
		assert.True(t, utf8.ValidString(capturedMsg), "Truncated string should be valid UTF-8. Got: %x", capturedMsg)
		assert.Equal(t, "Привет🌞", capturedMsg)
	})
}
