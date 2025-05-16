package tgspam

import (
	"context"
	"testing"

	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam/mocks"
)

func TestOpenAIChecker_Check(t *testing.T) {
	clientMock := &mocks.OpenAIClientMock{
		CreateChatCompletionFunc: func(contextMoqParam context.Context, chatCompletionRequest openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
			return openai.ChatCompletionResponse{
				Choices: []openai.ChatCompletionChoice{
					{
						Message: openai.ChatCompletionMessage{Content: ""},
					},
				},
			}, nil
		},
	}

	checker := newOpenAIChecker(clientMock, OpenAIConfig{
		MaxTokensResponse: 300,
		MaxTokensRequest:  3000,
		MaxSymbolsRequest: 12000,
		Model:             "gpt-4o-mini",
	})

	t.Run("spam response", func(t *testing.T) {
		clientMock.CreateChatCompletionFunc = func(
			contextMoqParam context.Context, chatCompletionRequest openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
			return openai.ChatCompletionResponse{
				Choices: []openai.ChatCompletionChoice{{
					Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
				}},
			}, nil
		}
		spam, details := checker.check("some text", nil)
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.True(t, spam)
		assert.Equal(t, "openai", details.Name)
		assert.Equal(t, "bad text, confidence: 100%", details.Details)
		assert.NoError(t, details.Error)
	})

	t.Run("not spam response", func(t *testing.T) {
		clientMock.CreateChatCompletionFunc = func(
			contextMoqParam context.Context, chatCompletionRequest openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
			return openai.ChatCompletionResponse{
				Choices: []openai.ChatCompletionChoice{{
					Message: openai.ChatCompletionMessage{Content: `{"spam": false, "reason":"good text", "confidence":99}`},
				}},
			}, nil
		}
		spam, details := checker.check("some text", nil)
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.False(t, spam)
		assert.Equal(t, "openai", details.Name)
		assert.Equal(t, "good text, confidence: 99%", details.Details)
		assert.NoError(t, details.Error)
	})

	t.Run("error response", func(t *testing.T) {
		clientMock.CreateChatCompletionFunc = func(
			contextMoqParam context.Context, chatCompletionRequest openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
			return openai.ChatCompletionResponse{}, assert.AnError
		}
		spam, details := checker.check("some text", nil)
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.False(t, spam)
		assert.Equal(t, "openai", details.Name)
		assert.Equal(t, "OpenAI error: failed to create chat completion: assert.AnError general error for testing", details.Details)
		assert.Equal(t, "failed to create chat completion: assert.AnError general error for testing", details.Error.Error())
	})

	t.Run("bad encoding", func(t *testing.T) {
		clientMock.CreateChatCompletionFunc = func(
			contextMoqParam context.Context, chatCompletionRequest openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
			return openai.ChatCompletionResponse{
				Choices: []openai.ChatCompletionChoice{{
					Message: openai.ChatCompletionMessage{Content: `bad json`},
				}},
			}, nil
		}
		spam, details := checker.check("some text", nil)
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.False(t, spam)
		assert.Equal(t, "openai", details.Name)
		assert.Equal(t, "OpenAI error: can't unmarshal response: bad json - invalid character 'b' looking for beginning of value",
			details.Details)
		assert.Equal(t, "can't unmarshal response: bad json - invalid character 'b' looking for beginning of value",
			details.Error.Error())
	})

	t.Run("no choices", func(t *testing.T) {
		clientMock.CreateChatCompletionFunc = func(
			contextMoqParam context.Context, chatCompletionRequest openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
			return openai.ChatCompletionResponse{}, nil
		}
		spam, details := checker.check("some text", nil)
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.False(t, spam)
		assert.Equal(t, "openai", details.Name)
		assert.Equal(t, "OpenAI error: no choices in response", details.Details)
	})
}

func TestOpenAIChecker_CheckWithHistory(t *testing.T) {
	clientMock := &mocks.OpenAIClientMock{
		CreateChatCompletionFunc: func(contextMoqParam context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
			// verify the request contains history
			assert.Contains(t, req.Messages[1].Content, "History:")
			assert.Contains(t, req.Messages[1].Content, `"user1": "first message"`)
			assert.Contains(t, req.Messages[1].Content, `"user2": "second message"`)
			assert.Contains(t, req.Messages[1].Content, `"user1": "third message"`)

			return openai.ChatCompletionResponse{
				Choices: []openai.ChatCompletionChoice{{
					Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"suspicious pattern in history", "confidence":90}`},
				}},
			}, nil
		},
	}

	checker := newOpenAIChecker(clientMock, OpenAIConfig{Model: "gpt-4o-mini"})
	history := []spamcheck.Request{
		{Msg: "first message", UserName: "user1"},
		{Msg: "second message", UserName: "user2"},
		{Msg: "third message", UserName: "user1"},
	}

	spam, details := checker.check("current message", history)
	t.Logf("spam: %v, details: %+v", spam, details)
	assert.True(t, spam)
	assert.Equal(t, "openai", details.Name)
	assert.Equal(t, "suspicious pattern in history, confidence: 90%", details.Details)
	assert.NoError(t, details.Error)
	assert.Equal(t, 1, len(clientMock.CreateChatCompletionCalls()))
}

func TestOpenAIChecker_FormatMessage(t *testing.T) {
	var capturedMsg string
	clientMock := &mocks.OpenAIClientMock{
		CreateChatCompletionFunc: func(contextMoqParam context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
			capturedMsg = req.Messages[1].Content // capture the message being sent
			return openai.ChatCompletionResponse{
				Choices: []openai.ChatCompletionChoice{{
					Message: openai.ChatCompletionMessage{Content: `{"spam": false, "reason":"test message", "confidence":90}`},
				}},
			}, nil
		},
	}

	tests := []struct {
		name            string
		currentMsg      string
		history         []spamcheck.Request
		expectedMessage string
	}{
		{
			name:            "message with no history",
			currentMsg:      "hello world",
			history:         []spamcheck.Request{},
			expectedMessage: "hello world",
		},
		{
			name:       "message with history",
			currentMsg: "current message",
			history: []spamcheck.Request{
				{Msg: "first message", UserName: "user1"},
				{Msg: "second message", UserName: "user2"},
			},
			expectedMessage: `User message:
current message

History:
"user1": "first message"
"user2": "second message"
`,
		},
		{
			name:       "message with empty username in history",
			currentMsg: "current message",
			history: []spamcheck.Request{
				{Msg: "first message", UserName: ""},
				{Msg: "second message", UserName: "user2"},
			},
			expectedMessage: `User message:
current message

History:
"": "first message"
"user2": "second message"
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientMock.ResetCalls() // reset mock before each test case
			checker := newOpenAIChecker(clientMock, OpenAIConfig{Model: "gpt-4o-mini"})
			checker.check(tt.currentMsg, tt.history)
			assert.Equal(t, tt.expectedMessage, capturedMsg, "message formatting mismatch")
			assert.Equal(t, 1, len(clientMock.CreateChatCompletionCalls()))
		})
	}
}

func TestStripThoughtTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no thought tags",
			input:    "This is a normal response",
			expected: "This is a normal response",
		},
		{
			name:     "single thought tag",
			input:    "This is <thought>some thinking process</thought> a response with thoughts",
			expected: "This is  a response with thoughts",
		},
		{
			name:     "multiple thought tags",
			input:    "<thought>Initial thinking</thought>Response<thought>More thinking</thought>",
			expected: "Response",
		},
		{
			name:     "multiline thought tags",
			input:    "Start\n<thought>\nMultiline\nthinking\n</thought>\nEnd",
			expected: "Start\n\nEnd",
		},
		{
			name:     "JSON content with thought tags",
			input:    `{"result": "<thought>thinking about spam</thought>This is spam"}`,
			expected: `{"result": "This is spam"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripThoughtTags(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReasoningEffortInRequest(t *testing.T) {
	tests := []struct {
		name            string
		reasoningEffort string
		expectInRequest bool
		expectedEffort  string
	}{
		{
			name:            "empty reasoning effort",
			reasoningEffort: "",
			expectInRequest: false,
		},
		{
			name:            "none reasoning effort",
			reasoningEffort: "none",
			expectInRequest: true,
			expectedEffort:  "none",
		},
		{
			name:            "low reasoning effort",
			reasoningEffort: "low",
			expectInRequest: true,
			expectedEffort:  "low",
		},
		{
			name:            "medium reasoning effort",
			reasoningEffort: "medium",
			expectInRequest: true,
			expectedEffort:  "medium",
		},
		{
			name:            "high reasoning effort",
			reasoningEffort: "high",
			expectInRequest: true,
			expectedEffort:  "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedRequest openai.ChatCompletionRequest

			clientMock := &mocks.OpenAIClientMock{
				CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
					capturedRequest = req
					return openai.ChatCompletionResponse{
						Choices: []openai.ChatCompletionChoice{{
							Message: openai.ChatCompletionMessage{Content: `{"spam": false, "reason":"test", "confidence":90}`},
						}},
					}, nil
				},
			}

			checker := newOpenAIChecker(clientMock, OpenAIConfig{
				Model:           "gpt-4o-mini",
				ReasoningEffort: tt.reasoningEffort,
			})

			// call the check method to trigger the client call
			checker.check("test message", nil)

			// verify the reasoning_effort parameter in the request
			if tt.expectInRequest {
				require.Equal(t, tt.expectedEffort, capturedRequest.ReasoningEffort)
			} else {
				require.Empty(t, capturedRequest.ReasoningEffort)
			}
		})
	}
}

func TestBuildSystemPromptWithCustomPrompts(t *testing.T) {
	tests := []struct {
		name          string
		systemPrompt  string
		customPrompts []string
		expected      string
	}{
		{
			name:          "with empty custom prompts",
			systemPrompt:  "Base prompt",
			customPrompts: []string{},
			expected:      "Base prompt",
		},
		{
			name:          "with one custom prompt",
			systemPrompt:  "Base prompt",
			customPrompts: []string{"Check for pattern X"},
			expected:      "Base prompt\n\nAlso, specifically check for these patterns:\n1. Check for pattern X\n",
		},
		{
			name:          "with multiple custom prompts",
			systemPrompt:  "Base prompt",
			customPrompts: []string{"Check for pattern X", "Check for pattern Y", "Watch for price patterns like $X,XXX"},
			expected:      "Base prompt\n\nAlso, specifically check for these patterns:\n1. Check for pattern X\n2. Check for pattern Y\n3. Watch for price patterns like $X,XXX\n",
		},
		{
			name:          "with default prompt when system prompt is empty",
			systemPrompt:  "",
			customPrompts: []string{"Custom pattern check"},
			expected:      defaultPrompt + "\n\nAlso, specifically check for these patterns:\n1. Custom pattern check\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientMock := &mocks.OpenAIClientMock{
				CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
					return openai.ChatCompletionResponse{
						Choices: []openai.ChatCompletionChoice{{
							Message: openai.ChatCompletionMessage{Content: `{"spam": false, "reason":"test", "confidence":90}`},
						}},
					}, nil
				},
			}

			checker := newOpenAIChecker(clientMock, OpenAIConfig{
				SystemPrompt:  tt.systemPrompt,
				CustomPrompts: tt.customPrompts,
			})

			// test the buildSystemPrompt function
			result := checker.buildSystemPrompt()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCustomPromptsInActualRequest(t *testing.T) {
	tests := []struct {
		name          string
		systemPrompt  string
		customPrompts []string
	}{
		{
			name:          "with no custom prompts",
			systemPrompt:  "Base system prompt",
			customPrompts: []string{},
		},
		{
			name:          "with custom prompts",
			systemPrompt:  "Base system prompt",
			customPrompts: []string{"Check for 'will perform X - $Y' pattern", "Watch for excessive emoji usage"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedContent string

			clientMock := &mocks.OpenAIClientMock{
				CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
					capturedContent = req.Messages[0].Content // capture the system message content
					return openai.ChatCompletionResponse{
						Choices: []openai.ChatCompletionChoice{{
							Message: openai.ChatCompletionMessage{Content: `{"spam": false, "reason":"test", "confidence":90}`},
						}},
					}, nil
				},
			}

			checker := newOpenAIChecker(clientMock, OpenAIConfig{
				SystemPrompt:  tt.systemPrompt,
				CustomPrompts: tt.customPrompts,
			})

			// call the check method to trigger the request
			checker.check("test message", nil)

			// verify the system message in the request contains what we expect
			expectedContent := checker.buildSystemPrompt()
			assert.Equal(t, expectedContent, capturedContent)

			// if we have custom prompts, ensure they are in the system message
			for _, prompt := range tt.customPrompts {
				assert.Contains(t, capturedContent, prompt)
			}
		})
	}
}
