package tgspam

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	tokenizer "github.com/sandwich-go/gpt3-encoder"
	"github.com/sashabaranov/go-openai"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

//go:generate moq --out mocks/openai_client.go --pkg mocks --with-resets --skip-ensure . openAIClient:OpenAIClientMock

// openAIChecker is a wrapper for OpenAI API to check if a text is spam
type openAIChecker struct {
	client openAIClient
	params OpenAIConfig
}

// OpenAIConfig contains parameters for openAIChecker
type OpenAIConfig struct {
	// https://platform.openai.com/docs/api-reference/chat/create#chat/create-max_tokens
	MaxTokensResponse int // Hard limit for the number of tokens in the response
	// The OpenAI has a limit for the number of tokens in the request + response (4097)
	MaxTokensRequest  int // Max request length in tokens
	MaxSymbolsRequest int // Fallback: Max request length in symbols, if tokenizer was failed
	Model             string
	SystemPrompt      string
	RetryCount        int
}

type openAIClient interface {
	CreateChatCompletion(context.Context, openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
}

const defaultPrompt = `
I'll give you a text from the messaging application, and you will return me a JSON with three fields: {"spam": true/false, "reason": "why this is spam", "confidence": 1-100}. Ypu should determine if the message is spam or not.
Set spam:true only if the confidence is above 80. Return JSON only, with no extra formatting!

Consider the following additional criteria:
  - If the message looks like an attempt to sell something, promote a service treat it as spam.
  - If the message promote some questionable content or service (e.g. adult, gambling, crypto, etc.), treat it as spam.
  - If the message seems like a generated text (e.g. random words, gibberish), treat it as spam.
  - If the message text resembles spam patterns, or typical spam topics, treat it as spam.
  - If history of previous messages is provided, use it for context to see if this message is relevant.
  - If the message is a short, generic reaction without meaningful context (e.g. "Какая красота, но зачем?"), 
    or obviously auto-generated fluff that doesn't add real value to the conversation, treat it as spam. Buy make sure to consider the context, because sometimes such messages are valid.
  - If the user's profile data (like suspicious links in username or avatar pattern) is provided, factor it in.
`

type openAIResponse struct {
	IsSpam     bool   `json:"spam"`
	Reason     string `json:"reason"`
	Confidence int    `json:"confidence"`
}

// newOpenAIChecker makes a bot for ChatGPT
func newOpenAIChecker(client openAIClient, params OpenAIConfig) *openAIChecker {
	if params.SystemPrompt == "" {
		params.SystemPrompt = defaultPrompt
	}
	if params.MaxTokensResponse == 0 {
		params.MaxTokensResponse = 1024
	}
	if params.MaxTokensRequest == 0 {
		params.MaxTokensRequest = 1024
	}
	if params.MaxSymbolsRequest == 0 {
		params.MaxSymbolsRequest = 8192
	}
	if params.Model == "" {
		params.Model = "gpt-4o-mini"
	}
	if params.RetryCount <= 0 {
		params.RetryCount = 1
	}
	return &openAIChecker{client: client, params: params}
}

// check checks if a text is spam using OpenAI API
func (o *openAIChecker) check(msg string, history []spamcheck.Request) (spam bool, cr spamcheck.Response) {
	if o.client == nil {
		return false, spamcheck.Response{}
	}

	// update the message with the history
	if len(history) > 0 {
		var hist []string
		for _, h := range history {
			hist = append(hist, fmt.Sprintf("%q: %q", h.UserName, h.Msg))
		}
		msgWithHist := fmt.Sprintf("User message:\n%s\n\nHistory:\n%s\n", msg, strings.Join(hist, "\n"))
		msg = msgWithHist
	}

	// try to send a request several times if it fails
	var resp openAIResponse
	var err error
	for i := 0; i < o.params.RetryCount; i++ {
		if resp, err = o.sendRequest(msg); err == nil {
			break
		}
	}
	if err != nil {
		return false, spamcheck.Response{
			Spam: false, Name: "openai", Details: fmt.Sprintf("OpenAI error: %v", err), Error: err}
	}

	return resp.IsSpam, spamcheck.Response{Spam: resp.IsSpam, Name: "openai",
		Details: strings.TrimSuffix(resp.Reason, ".") + ", confidence: " + fmt.Sprintf("%d%%", resp.Confidence)}
}

func (o *openAIChecker) sendRequest(msg string) (response openAIResponse, err error) {
	// Reduce the request size with tokenizer and fallback to default reducer if it fails.
	// The API supports 4097 tokens ~16000 characters (<=4 per token) for request + result together.
	// The response is limited to 1000 tokens, and OpenAI always reserved it for the result.
	// So the max length of the request should be 3000 tokens or ~12000 characters
	reduceRequest := func(text string) (result string) {
		// defaultReducer is a fallback if tokenizer fails
		defaultReducer := func(text string) (result string) {
			if len(text) <= o.params.MaxSymbolsRequest {
				return text
			}
			return text[:o.params.MaxSymbolsRequest]
		}

		encoder, tokErr := tokenizer.NewEncoder()
		if tokErr != nil {
			return defaultReducer(text)
		}

		tokens, encErr := encoder.Encode(text)
		if encErr != nil {
			return defaultReducer(text)
		}

		if len(tokens) <= o.params.MaxTokensRequest {
			return text
		}

		return encoder.Decode(tokens[:o.params.MaxTokensRequest])
	}

	r := reduceRequest(msg)

	data := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: o.params.SystemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: r},
	}

	resp, err := o.client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:          o.params.Model,
			MaxTokens:      o.params.MaxTokensResponse,
			Messages:       data,
			ResponseFormat: &openai.ChatCompletionResponseFormat{Type: "json_object"},
		},
	)

	if err != nil {
		return openAIResponse{}, err
	}

	// OpenAI platform supports returning multiple chat completion choices, but we use only the first one:
	// https://platform.openai.com/docs/api-reference/chat/create#chat/create-n
	if len(resp.Choices) == 0 {
		return openAIResponse{}, fmt.Errorf("no choices in response")
	}

	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &response); err != nil {
		return openAIResponse{}, fmt.Errorf("can't unmarshal response: %s - %w", resp.Choices[0].Message.Content, err)
	}

	return response, nil
}
