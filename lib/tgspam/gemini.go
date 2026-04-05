package tgspam

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/genai"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

//go:generate moq --out mocks/gemini_client.go --pkg mocks --with-resets --skip-ensure . geminiClient:GeminiClientMock

// geminiChecker is a wrapper for Google Gemini API to check if a text is spam
type geminiChecker struct {
	client geminiClient
	params GeminiConfig
}

// GeminiConfig contains parameters for geminiChecker
type GeminiConfig struct {
	MaxOutputTokens    int32    // max tokens in the response
	MaxSymbolsRequest  int      // max request length in symbols
	Model              string   // gemini model name
	SystemPrompt       string   // system prompt for spam detection
	CustomPrompts      []string // additional prompts for specific spam patterns
	RetryCount         int      // number of retries on failure
	CheckShortMessages bool     // if true, check messages shorter than MinMsgLen with Gemini
}

type geminiClient interface {
	GenerateContent(context.Context, string, []*genai.Content, *genai.GenerateContentConfig) (*genai.GenerateContentResponse, error)
}

// newGeminiChecker creates a new geminiChecker
func newGeminiChecker(client geminiClient, params GeminiConfig) *geminiChecker {
	if params.SystemPrompt == "" {
		params.SystemPrompt = defaultPrompt
	}
	if params.MaxOutputTokens == 0 {
		params.MaxOutputTokens = 1024
	}
	if params.MaxSymbolsRequest == 0 {
		params.MaxSymbolsRequest = 8192
	}
	if params.Model == "" {
		params.Model = "gemma-4-31b-it"
	}
	if params.RetryCount <= 0 {
		params.RetryCount = 1
	}
	return &geminiChecker{client: client, params: params}
}

// check checks if a text is spam using Gemini API
func (g *geminiChecker) check(ctx context.Context, msg string, history []spamcheck.Request) (spam bool, cr spamcheck.Response) {
	if g.client == nil {
		return false, spamcheck.Response{}
	}

	return runLLMProviderCheck(ctx, "gemini", "Gemini", g.params.RetryCount, msg, history, g.sendRequest)
}

// buildSystemPrompt creates the complete system prompt by combining the base prompt with custom prompts
func (g *geminiChecker) buildSystemPrompt() string {
	basePrompt := g.params.SystemPrompt

	if len(g.params.CustomPrompts) == 0 {
		return basePrompt
	}

	var sb strings.Builder
	sb.WriteString(basePrompt)
	sb.WriteString("\n\nAlso, specifically check for these patterns:\n")

	for i, prompt := range g.params.CustomPrompts {
		sb.WriteString(strconv.Itoa(i+1) + ". " + prompt + "\n")
	}

	return sb.String()
}

func (g *geminiChecker) sendRequest(ctx context.Context, msg string) (response llmResponse, err error) {
	// truncate request if needed
	if len(msg) > g.params.MaxSymbolsRequest {
		runes := []rune(msg)
		if len(runes) > g.params.MaxSymbolsRequest {
			msg = string(runes[:g.params.MaxSymbolsRequest])
		}
	}

	completeSystemPrompt := g.buildSystemPrompt()

	config := &genai.GenerateContentConfig{
		MaxOutputTokens:   g.params.MaxOutputTokens,
		ResponseMIMEType:  "application/json",
		SystemInstruction: genai.NewContentFromText(completeSystemPrompt, genai.RoleUser),
		SafetySettings: []*genai.SafetySetting{
			{Category: genai.HarmCategoryHarassment, Threshold: genai.HarmBlockThresholdOff},
			{Category: genai.HarmCategoryHateSpeech, Threshold: genai.HarmBlockThresholdOff},
			{Category: genai.HarmCategorySexuallyExplicit, Threshold: genai.HarmBlockThresholdOff},
			{Category: genai.HarmCategoryDangerousContent, Threshold: genai.HarmBlockThresholdOff},
		},
	}

	resp, err := g.client.GenerateContent(
		ctx,
		g.params.Model,
		genai.Text(msg),
		config,
	)
	if err != nil {
		return llmResponse{}, fmt.Errorf("failed to generate content: %w", err)
	}

	if resp == nil || len(resp.Candidates) == 0 {
		return llmResponse{}, fmt.Errorf("no candidates in response")
	}

	content := resp.Text()
	content = stripThoughtTags(content)

	if err := json.Unmarshal([]byte(content), &response); err != nil {
		return llmResponse{}, fmt.Errorf("can't unmarshal response: %s - %w", content, err)
	}

	return response, nil
}
