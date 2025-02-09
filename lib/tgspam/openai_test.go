package tgspam

import (
	"context"
	"testing"

	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"

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
		assert.Equal(t, "OpenAI error: assert.AnError general error for testing", details.Details)
		assert.Equal(t, "assert.AnError general error for testing", details.Error.Error())
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
