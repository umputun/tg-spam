package tgspam

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	log "github.com/go-pkgz/lgr"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

const (
	defaultJevAPIBase           = "https://api.typesafe.ai/v1/systemone"
	defaultJevModel             = "jev-1.13.0"
	defaultJevMaxSymbolsRequest = 6000
	jevQuestionID               = "spam"
	jevGibberishQuestionID      = "gibberish"

	// the gibberish question and criteria are the wording the gibberish threshold was validated with,
	// so they are not configurable
	jevGibberishQuestion     = "Is `message` random keyboard mashing, made of letter groups that are not words in any language?"
	jevGibberishCriteriaTrue = "The whole message is several random letter groups with no meaning in any language, " +
		"such as sequences of adjacent keyboard keys, typed to fill space or test the chat."
	jevGibberishCriteriaFalse = "It contains real words in any language, slang, laughter, sounds, interjections, memes, " +
		"repeated letters, abbreviations, commands or code, transliteration, or words typed in the wrong keyboard " +
		"layout (Russian typed on an English layout or the reverse), even if short, rude or odd."
)

// JevConfig contains parameters for jevChecker. Threshold and the question text are the
// tuned surface: changing either invalidates the evaluation the default threshold came from.
type JevConfig struct {
	Token              string  // bearer credential
	APIBase            string  // empty falls back to the default endpoint
	Model              string  // pinned version, never an alias
	Question           string  // noul instructions
	CriteriaSpam       string  // criteria.true
	CriteriaHam        string  // criteria.false
	Threshold          float64 // noul at or above this is spam
	MaxSymbolsRequest  int     // max request length in runes, 0 uses the default
	RetryCount         int     // total attempts, honored by the shared LLM runner
	CheckShortMessages bool    // if true, check messages shorter than MinMsgLen with jev
	GibberishThreshold float64 // gibberish noul at or above this is spam, 0 disables the gibberish question
}

// jevChecker is a wrapper for the typesafe.ai decision API to check if a text is spam.
// Unlike the generative providers it asks a typed spam question, plus an optional gibberish
// question, and gets back probabilities, thresholded here so the shared llmResponse contract is unchanged.
type jevChecker struct {
	client HTTPClient
	params JevConfig
}

type jevQuestion struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria"`
}

type jevRequest struct {
	Model     string                 `json:"model"`
	State     map[string]string      `json:"state"`
	Questions map[string]jevQuestion `json:"questions"`
}

// jevAnswer keeps Noul a pointer: a missing key and an explicit null both decode into a
// plain float64 as 0.0, which is in range and would become a confident ham verdict.
type jevAnswer struct {
	Type string   `json:"type"`
	Noul *float64 `json:"noul"`
}

type jevResponse struct {
	Model   string               `json:"model"`
	Answers map[string]jevAnswer `json:"answers"`
}

// newJevChecker makes a checker for the jev decision API. It validates params rather than
// normalizing them, so a caller using the library directly cannot bypass the app-level validator.
func newJevChecker(client HTTPClient, params JevConfig) (*jevChecker, error) {
	// a nil client would make check return an empty non-error response, which reads as a
	// confident ham verdict and clears LLM-eligible heuristic spam in veto mode. The sibling
	// providers tolerate it because their constructors cannot report an error; this one can.
	if client == nil {
		return nil, fmt.Errorf("jev client must not be nil")
	}
	if math.IsNaN(params.Threshold) || math.IsInf(params.Threshold, 0) {
		return nil, fmt.Errorf("jev threshold must be a finite number, got %v", params.Threshold)
	}
	if params.Threshold <= 0 || params.Threshold > 1 {
		return nil, fmt.Errorf("jev threshold must be in (0, 1], got %v", params.Threshold)
	}
	if math.IsNaN(params.GibberishThreshold) || math.IsInf(params.GibberishThreshold, 0) {
		return nil, fmt.Errorf("jev gibberish threshold must be a finite number, got %v", params.GibberishThreshold)
	}
	if params.GibberishThreshold < 0 || params.GibberishThreshold > 1 {
		return nil, fmt.Errorf("jev gibberish threshold must be in [0, 1], got %v", params.GibberishThreshold)
	}
	if params.MaxSymbolsRequest < 0 {
		return nil, fmt.Errorf("jev max symbols request must not be negative, got %d", params.MaxSymbolsRequest)
	}
	if params.Question == "" {
		return nil, fmt.Errorf("jev question must not be empty")
	}
	if params.CriteriaSpam == "" {
		return nil, fmt.Errorf("jev spam criteria must not be empty")
	}
	if params.CriteriaHam == "" {
		return nil, fmt.Errorf("jev ham criteria must not be empty")
	}

	if params.APIBase == "" {
		params.APIBase = defaultJevAPIBase
	}
	if params.Model == "" {
		params.Model = defaultJevModel
	}
	if params.MaxSymbolsRequest == 0 {
		params.MaxSymbolsRequest = defaultJevMaxSymbolsRequest
	}
	if params.RetryCount <= 0 {
		params.RetryCount = 1
	}
	return &jevChecker{client: client, params: params}, nil
}

// check checks if a text is spam using the jev API. askGibberish is false when msg carries text the
// sender did not write (quoted or replied-to), since the gibberish answer overrides the spam verdict.
func (j *jevChecker) check(ctx context.Context, msg string, history []spamcheck.Request,
	askGibberish bool) (spam bool, cr spamcheck.Response) {
	if j.client == nil {
		return false, spamcheck.Response{}
	}
	send := func(ctx context.Context, msg string) (llmResponse, error) {
		return j.sendRequest(ctx, msg, askGibberish)
	}
	return runLLMProviderCheck(ctx, "jev", "Jev", j.params.RetryCount, msg, history, send)
}

func (j *jevChecker) buildRequest(msg string, askGibberish bool) jevRequest {
	if runes := []rune(msg); len(runes) > j.params.MaxSymbolsRequest {
		msg = string(runes[:j.params.MaxSymbolsRequest])
	}
	questions := map[string]jevQuestion{
		jevQuestionID: {
			Type:         "noul",
			Instructions: j.params.Question,
			Criteria:     map[string]string{"true": j.params.CriteriaSpam, "false": j.params.CriteriaHam},
		},
	}
	if askGibberish && j.params.GibberishThreshold > 0 {
		questions[jevGibberishQuestionID] = jevQuestion{
			Type:         "noul",
			Instructions: jevGibberishQuestion,
			Criteria:     map[string]string{"true": jevGibberishCriteriaTrue, "false": jevGibberishCriteriaFalse},
		}
	}
	return jevRequest{Model: j.params.Model, State: map[string]string{"message": msg}, Questions: questions}
}

// verdict maps a probability onto the shared llm contract. Confidence is direction-aware:
// p=0.02 is strong evidence against spam, not 2% confidence in the ham verdict it produces.
func (j *jevChecker) verdict(p float64) llmResponse {
	spam := p >= j.params.Threshold
	confidence := p
	if !spam {
		confidence = 1 - p
	}
	return llmResponse{
		IsSpam:     spam,
		Reason:     fmt.Sprintf("spam probability %.2f, threshold %.2f", p, j.params.Threshold),
		Confidence: int(math.Round(confidence * 100)),
	}
}

func (j *jevChecker) sendRequest(ctx context.Context, msg string, askGibberish bool) (response llmResponse, err error) {
	body, err := json.Marshal(j.buildRequest(msg, askGibberish))
	if err != nil {
		return llmResponse{}, fmt.Errorf("can't marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.params.APIBase, bytes.NewReader(body))
	if err != nil {
		return llmResponse{}, fmt.Errorf("can't make request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+j.params.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := j.client.Do(req)
	if err != nil {
		return llmResponse{}, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do with a close error on a read body

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return llmResponse{}, fmt.Errorf("can't read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return llmResponse{}, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed jevResponse
	if err = json.Unmarshal(respBody, &parsed); err != nil {
		return llmResponse{}, fmt.Errorf("can't unmarshal response: %s - %w", string(respBody), err)
	}

	p, err := j.noul(parsed, jevQuestionID, respBody)
	if err != nil {
		return llmResponse{}, err
	}
	log.Printf("[DEBUG] jev model: %s", parsed.Model)

	if askGibberish && j.params.GibberishThreshold > 0 {
		gib, err := j.noul(parsed, jevGibberishQuestionID, respBody)
		if err != nil {
			return llmResponse{}, err
		}
		if gib >= j.params.GibberishThreshold {
			return llmResponse{
				IsSpam:     true,
				Reason:     fmt.Sprintf("gibberish probability %.2f, threshold %.2f", gib, j.params.GibberishThreshold),
				Confidence: int(math.Round(gib * 100)),
			}, nil
		}
	}
	return j.verdict(p), nil
}

// noul extracts a validated noul answer. A missing or malformed answer is an error, never 0.
func (j *jevChecker) noul(parsed jevResponse, id string, respBody []byte) (float64, error) {
	answer, ok := parsed.Answers[id]
	if !ok {
		return 0, fmt.Errorf("no %q answer in response: %s", id, string(respBody))
	}
	if answer.Type != "noul" {
		return 0, fmt.Errorf("%q answer has type %q, want noul", id, answer.Type)
	}
	if answer.Noul == nil {
		return 0, fmt.Errorf("missing %q noul value in response: %s", id, string(respBody))
	}
	p := *answer.Noul
	if math.IsNaN(p) || p < 0 || p > 1 {
		return 0, fmt.Errorf("%q noul %v out of range", id, p)
	}
	return p, nil
}
