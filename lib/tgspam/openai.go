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

//go:generate moq --out mocks/openai_client.go --pkg mocks --skip-ensure . openAIClient:OpenAIClientMock

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
}

type openAIClient interface {
	CreateChatCompletion(context.Context, openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
}

const defaultPrompt = `I'll give you a text from the messaging application and you will return me a json with three fields: {"spam": true/false, "reason":"why this is spam", "confidence":1-100}. Set spam:true only of confidence above 80`

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
		params.Model = "gpt-4"
	}
	return &openAIChecker{client: client, params: params}
}

// check checks if a text is spam
func (o *openAIChecker) check(msg string) (spam bool, cr spamcheck.Response) {
	if o.client == nil {
		return false, spamcheck.Response{}
	}

	resp, err := o.sendRequest(msg)
	if err != nil {
		return false, spamcheck.Response{Spam: false, Name: "openai", Details: fmt.Sprintf("OpenAI error: %v", err)}
	}
	return resp.IsSpam, spamcheck.Response{Spam: resp.IsSpam, Name: "openai",
		Details: strings.TrimSuffix(resp.Reason, ".") + ", confidence: " + fmt.Sprintf("%d%%", resp.Confidence)}
}

func (o *openAIChecker) sendRequest(msg string) (response openAIResponse, err error) {
	// Reduce the request size with tokenizer and fallback to default reducer if it fails
	// The API supports 4097 tokens ~16000 characters (<=4 per token) for request + result together
	// The response is limited to 1000 tokens and OpenAI always reserved it for the result
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
		openai.ChatCompletionRequest{Model: o.params.Model, MaxTokens: o.params.MaxTokensResponse, Messages: data},
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
		return openAIResponse{}, fmt.Errorf("can't unmarshal response: %w", err)
	}

	return response, nil
}
