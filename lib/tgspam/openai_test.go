package tgspam

import (
	"context"
	"testing"

	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"

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
		Model:             "gpt-4",
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
		spam, details := checker.check("some text")
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.True(t, spam)
		assert.Equal(t, "openai", details.Name)
		assert.Equal(t, "bad text, confidence: 100%", details.Details)
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
		spam, details := checker.check("some text")
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.False(t, spam)
		assert.Equal(t, "openai", details.Name)
		assert.Equal(t, "good text, confidence: 99%", details.Details)
	})

	t.Run("error response", func(t *testing.T) {
		clientMock.CreateChatCompletionFunc = func(
			contextMoqParam context.Context, chatCompletionRequest openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
			return openai.ChatCompletionResponse{}, assert.AnError
		}
		spam, details := checker.check("some text")
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.False(t, spam)
		assert.Equal(t, "openai", details.Name)
		assert.Equal(t, "OpenAI error: assert.AnError general error for testing", details.Details)
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
		spam, details := checker.check("some text")
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.False(t, spam)
		assert.Equal(t, "openai", details.Name)
		assert.Equal(t, "OpenAI error: can't unmarshal response: invalid character 'b' looking for beginning of value", details.Details)
	})

	t.Run("no choices", func(t *testing.T) {
		clientMock.CreateChatCompletionFunc = func(
			contextMoqParam context.Context, chatCompletionRequest openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
			return openai.ChatCompletionResponse{}, nil
		}
		spam, details := checker.check("some text")
		t.Logf("spam: %v, details: %+v", spam, details)
		assert.False(t, spam)
		assert.Equal(t, "openai", details.Name)
		assert.Equal(t, "OpenAI error: no choices in response", details.Details)
	})
}
