package tgspam

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
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
	MaxTokensResponse int // hard limit for the number of tokens in the response
	// the OpenAI has a limit for the number of tokens in the request + response (4097)
	MaxTokensRequest             int // max request length in tokens
	MaxSymbolsRequest            int // fallback: Max request length in symbols, if tokenizer was failed
	Model                        string
	SystemPrompt                 string
	CustomPrompts                []string // additional prompts for specific spam patterns
	RetryCount                   int
	ReasoningEffort              string // effort on reasoning for reasoning models: "low", "medium", "high", or "none"
	CheckShortMessagesWithOpenAI bool   // if true, check messages shorter than MinMsgLen with OpenAI
}

type openAIClient interface {
	CreateChatCompletion(context.Context, openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
}

const defaultPrompt = `I'll give you a text from the messaging application and you will return me a json with three fields: {"spam": true/false, "reason":"why this is spam", "confidence":1-100}. Set spam:true only of confidence above 80. Return JSON only with no extra formatting!` + "\n" + `If history of previous messages provided, use them as extra context to make the decision.`

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

// buildSystemPrompt creates the complete system prompt by combining the base prompt with custom prompts
func (o *openAIChecker) buildSystemPrompt() string {
	basePrompt := o.params.SystemPrompt

	// if there are no custom prompts, just return the base prompt
	if len(o.params.CustomPrompts) == 0 {
		return basePrompt
	}

	// combine base prompt with custom prompts
	var sb strings.Builder
	sb.WriteString(basePrompt)
	sb.WriteString("\n\nAlso, specifically check for these patterns:\n")

	// add each custom prompt as a numbered item
	for i, prompt := range o.params.CustomPrompts {
		sb.WriteString(strconv.Itoa(i+1) + ". " + prompt + "\n")
	}

	return sb.String()
}

// isReasoningModel checks if the model requires MaxCompletionTokens instead of MaxTokens
// this includes o1-series models and gpt-5 models
func (o *openAIChecker) isReasoningModel() bool {
	modelLower := strings.ToLower(o.params.Model)
	return strings.HasPrefix(modelLower, "o1") ||
		strings.HasPrefix(modelLower, "o3") ||
		strings.HasPrefix(modelLower, "o4") ||
		strings.Contains(modelLower, "gpt-5")
}

func (o *openAIChecker) sendRequest(msg string) (response openAIResponse, err error) {
	// reduce the request size with tokenizer and fallback to default reducer if it fails.
	// the API supports 4097 tokens ~16000 characters (<=4 per token) for request + result together.
	// the response is limited to 1000 tokens, and OpenAI always reserved it for the result.
	// so the max length of the request should be 3000 tokens or ~12000 characters
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

	// build the complete system prompt with any custom prompts
	completeSystemPrompt := o.buildSystemPrompt()

	data := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: completeSystemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: r},
	}

	request := openai.ChatCompletionRequest{
		Model:          o.params.Model,
		Messages:       data,
		ResponseFormat: &openai.ChatCompletionResponseFormat{Type: "json_object"},
	}

	// use MaxCompletionTokens for reasoning models (o1, o3, o4) and gpt-5, MaxTokens for others
	if o.isReasoningModel() {
		request.MaxCompletionTokens = o.params.MaxTokensResponse
	} else {
		request.MaxTokens = o.params.MaxTokensResponse
	}

	// add reasoning_effort parameter if set and not "none"
	if o.params.ReasoningEffort != "" && o.params.ReasoningEffort != "none" {
		request.ReasoningEffort = o.params.ReasoningEffort
	}

	resp, err := o.client.CreateChatCompletion(
		context.Background(),
		request,
	)

	if err != nil {
		return openAIResponse{}, fmt.Errorf("failed to create chat completion: %w", err)
	}

	// openAI platform supports returning multiple chat completion choices, but we use only the first one:
	// https://platform.openai.com/docs/api-reference/chat/create#chat/create-n
	if len(resp.Choices) == 0 {
		return openAIResponse{}, fmt.Errorf("no choices in response")
	}

	// strip <thought> tags from response content if present
	content := resp.Choices[0].Message.Content
	content = stripThoughtTags(content)

	if err := json.Unmarshal([]byte(content), &response); err != nil {
		return openAIResponse{}, fmt.Errorf("can't unmarshal response: %s - %w", content, err)
	}

	return response, nil
}

var thoughtRegex = regexp.MustCompile(`<thought>(?s).*?</thought>`)

// stripThoughtTags removes any content enclosed in <thought></thought> tags
func stripThoughtTags(content string) string {
	content = thoughtRegex.ReplaceAllString(content, "")
	return content
}
