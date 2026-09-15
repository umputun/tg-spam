package tgspam

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestAppendHistoryToLLMMessage(t *testing.T) {
	t.Run("no history", func(t *testing.T) {
		assert.Equal(t, "hello world", appendHistoryToLLMMessage("hello world", nil))
	})

	t.Run("with history", func(t *testing.T) {
		history := []spamcheck.Request{
			{Msg: "first message", UserName: "user1"},
			{Msg: "second message", UserName: ""},
		}

		got := appendHistoryToLLMMessage("current message", history)

		assert.Equal(t, `User message:
current message

History:
"user1": "first message"
"": "second message"
`, got)
	})
}

func TestRunLLMProviderCheck(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		calls := 0
		spam, details := runLLMProviderCheck(context.Background(), "openai", "OpenAI", 3, "test message", nil,
			func(_ context.Context, msg string) (llmResponse, error) {
				calls++
				assert.Equal(t, "test message", msg)
				return llmResponse{IsSpam: true, Reason: "bad text.", Confidence: 91}, nil
			},
		)

		assert.True(t, spam)
		assert.Equal(t, 1, calls)
		assert.Equal(t, "openai", details.Name)
		assert.Equal(t, "bad text, confidence: 91%", details.Details)
		assert.NoError(t, details.Error)
	})

	t.Run("retries until success", func(t *testing.T) {
		calls := 0
		history := []spamcheck.Request{{Msg: "prev", UserName: "alice"}}

		spam, details := runLLMProviderCheck(context.Background(), "gemini", "Gemini", 3, "current", history,
			func(_ context.Context, msg string) (llmResponse, error) {
				calls++
				assert.Equal(t, "User message:\ncurrent\n\nHistory:\n\"alice\": \"prev\"\n", msg)
				if calls < 3 {
					return llmResponse{}, errors.New("temporary failure")
				}
				return llmResponse{IsSpam: false, Reason: "looks fine", Confidence: 42}, nil
			},
		)

		assert.False(t, spam)
		assert.Equal(t, 3, calls)
		assert.Equal(t, "gemini", details.Name)
		assert.Equal(t, "looks fine, confidence: 42%", details.Details)
		assert.NoError(t, details.Error)
	})

	t.Run("retry count defaults to one", func(t *testing.T) {
		calls := 0

		spam, details := runLLMProviderCheck(context.Background(), "gemini", "Gemini", 0, "test", nil,
			func(_ context.Context, msg string) (llmResponse, error) {
				calls++
				return llmResponse{}, errors.New("boom")
			},
		)

		require.False(t, spam)
		assert.Equal(t, 1, calls)
		assert.Equal(t, "gemini", details.Name)
		assert.Equal(t, "Gemini error: boom", details.Details)
		require.Error(t, details.Error)
		assert.EqualError(t, details.Error, "boom")
	})
}
