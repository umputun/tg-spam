package tgspam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/sashabaranov/go-openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/approved"
	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam/mocks"
)

func TestDetector_CheckWithShort(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: 1, MinMsgLen: 150})
	lr, err := d.LoadStopWords(bytes.NewBufferString("в личку\nвсем привет"))
	require.NoError(t, err)
	assert.Equal(t, LoadResult{StopWords: 2}, lr)

	t.Run("short message without spam", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "good message"})
		assert.False(t, spam)
		require.Len(t, cr, 3, cr)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.Equal(t, false, cr[0].Spam)
		assert.Equal(t, "not found", cr[0].Details)
		assert.Equal(t, "emoji", cr[1].Name)
		assert.Equal(t, false, cr[1].Spam)
		assert.Equal(t, "0/1", cr[1].Details)
		assert.Equal(t, "message length", cr[2].Name)
		assert.Equal(t, false, cr[2].Spam)
		assert.Equal(t, "too short", cr[2].Details)
	})

	t.Run("short message with stopwords", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello, please send me a message в личку"})
		assert.True(t, spam)
		require.Len(t, cr, 3, cr)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.Equal(t, true, cr[0].Spam)
		assert.Equal(t, "в личку", cr[0].Details)
		assert.Equal(t, "emoji", cr[1].Name)
		assert.Equal(t, false, cr[1].Spam)
		assert.Equal(t, "0/1", cr[1].Details)
		assert.Equal(t, "message length", cr[2].Name)
		assert.Equal(t, false, cr[2].Spam)
		assert.Equal(t, "too short", cr[2].Details)
	})

	t.Run("short message with emojis", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello 😁🐶🍕"})
		assert.True(t, spam)
		require.Len(t, cr, 3, cr)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.Equal(t, false, cr[0].Spam)
		assert.Equal(t, "not found", cr[0].Details)
		assert.Equal(t, "emoji", cr[1].Name)
		assert.Equal(t, true, cr[1].Spam)
		assert.Equal(t, "3/1", cr[1].Details)
		assert.Equal(t, "message length", cr[2].Name)
		assert.Equal(t, false, cr[2].Spam)
		assert.Equal(t, "too short", cr[2].Details)
	})

	t.Run("short message with emojis and stop words", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello 😁🐶🍕в личку"})
		assert.True(t, spam)
		require.Len(t, cr, 3, cr)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.Equal(t, true, cr[0].Spam)
		assert.Equal(t, "в личку", cr[0].Details)
		assert.Equal(t, "emoji", cr[1].Name)
		assert.Equal(t, true, cr[1].Spam)
		assert.Equal(t, "3/1", cr[1].Details)
		assert.Equal(t, "message length", cr[2].Name)
		assert.Equal(t, false, cr[2].Spam)
		assert.Equal(t, "too short", cr[2].Details)
	})
}

func TestDetector_CheckStopWords(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1})
	lr, err := d.LoadStopWords(bytes.NewBufferString("в личку\nвсеМ прИвет\nspambot\n12345"))
	require.NoError(t, err)
	assert.Equal(t, LoadResult{StopWords: 4}, lr)

	tests := []struct {
		name     string
		message  string
		username string
		userID   string
		expected bool
		details  string
	}{
		{
			name:     "Stop word present in message",
			message:  "Hello, please send me a message в личкУ",
			username: "user1",
			userID:   "987654321",
			expected: true,
			details:  "в личку",
		},
		{
			name:     "Stop word present with emoji",
			message:  "👋Всем привет\nИщу амбициозного человека к се6е в команду\nКто в поисках дополнительного заработка или хочет попробовать себя в новой  сфере деятельности! 👨🏻\u200d💻\nПишите в лс✍️",
			username: "user1",
			userID:   "987654321",
			expected: true,
			details:  "всем привет",
		},
		{
			name:     "No stop word present",
			message:  "Hello, how are you?",
			username: "user1",
			userID:   "987654321",
			expected: false,
			details:  "not found",
		},
		{
			name:     "Case insensitive stop word present",
			message:  "Hello, please send me a message В ЛИЧКУ",
			username: "user1",
			userID:   "987654321",
			expected: true,
			details:  "в личку",
		},
		{
			name:     "Stop word in username",
			message:  "Hello, how are you?",
			username: "spambot_seller",
			userID:   "987654321",
			expected: true,
			details:  "spambot",
		},
		{
			name:     "Stop word in username case insensitive",
			message:  "Hello, how are you?",
			username: "SpAmBoT_seller",
			userID:   "987654321",
			expected: true,
			details:  "spambot",
		},
		{
			name:     "Stop word in user ID",
			message:  "Hello, how are you?",
			username: "normal_user",
			userID:   "12345_account",
			expected: true,
			details:  "12345",
		},
		{
			name:     "No stop word in anything",
			message:  "Regular message",
			username: "normal_user",
			userID:   "987654321",
			expected: false,
			details:  "not found",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spam, cr := d.Check(spamcheck.Request{Msg: test.message, UserName: test.username, UserID: test.userID})
			assert.Equal(t, test.expected, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "stopword", cr[0].Name)
			assert.Equal(t, test.details, cr[0].Details)
			t.Logf("%+v", cr[0].Details)
		})
	}
}

//nolint:stylecheck // it has unicode symbols purposely
func TestDetector_CheckEmojis(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: 2})
	tests := []struct {
		name  string
		input string
		count int
		spam  bool
	}{
		{"NoEmoji", "Hello, world!", 0, false},
		{"OneEmoji", "Hi there 👋", 1, false},
		{"TwoEmojis", "Good morning 🌞🌻", 2, false},
		{"Mixed", "👨‍👩‍👧‍👦 Family emoji", 1, false},
		{"EmojiSequences", "🏳️‍🌈 Rainbow flag", 1, false},
		{"TextAfterEmoji", "😊 Have a nice day!", 1, false},
		{"OnlyEmojis", "😁🐶🍕", 3, true},
		{"WithCyrillic", "Привет 🌞 🍕 мир! 👋", 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spam, cr := d.Check(spamcheck.Request{Msg: tt.input})
			assert.Equal(t, tt.spam, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "emoji", cr[0].Name)
			assert.Equal(t, tt.spam, cr[0].Spam)
			assert.Equal(t, fmt.Sprintf("%d/2", tt.count), cr[0].Details)
		})
	}
}

func TestSpam_CheckIsCasSpam(t *testing.T) {
	tests := []struct {
		name           string
		mockResp       string
		mockStatusCode int
		expected       bool
	}{
		{
			name:           "User is not a spammer",
			mockResp:       `{"ok": false, "description": "Not a spammer"}`,
			mockStatusCode: 200,
			expected:       false,
		},
		{
			name:           "User is not a spammer, message case",
			mockResp:       `{"ok": false, "description": "Not A spamMer."}`,
			mockStatusCode: 200,
			expected:       false,
		},
		{
			name:           "User is a spammer",
			mockResp:       `{"ok": true, "description": "Is a spammer"}`,
			mockStatusCode: 200,
			expected:       true,
		},
		{
			name:           "User is a spammer",
			mockResp:       `{"ok": true, "description": ""}`,
			mockStatusCode: 200,
			expected:       true,
		},
		{
			name:           "HTTP error",
			mockResp:       `{"ok": false, "description": "not found"}`,
			mockStatusCode: 500,
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockedHTTPClient := &mocks.HTTPClientMock{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: tt.mockStatusCode,
						Body:       io.NopCloser(bytes.NewBufferString(tt.mockResp)),
					}, nil
				},
			}

			d := NewDetector(Config{
				CasAPI:           "http://localhost",
				HTTPClient:       mockedHTTPClient,
				MaxAllowedEmoji:  -1,
				FirstMessageOnly: true,
			})
			spam, cr := d.Check(spamcheck.Request{UserID: "123"})
			assert.Equal(t, tt.expected, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "cas", cr[0].Name)
			assert.Equal(t, tt.expected, cr[0].Spam)

			respDetails := struct {
				OK          bool   `json:"ok"`
				Description string `json:"description"`
			}{}
			err := json.Unmarshal([]byte(tt.mockResp), &respDetails)
			require.NoError(t, err)
			expResp := strings.ToLower(respDetails.Description)
			if expResp == "" {
				expResp = "spam detected"
			}
			expResp = strings.TrimSuffix(expResp, ".")
			assert.Equal(t, expResp, cr[0].Details)

			assert.Equal(t, respDetails.Description, respDetails.Description)
			assert.Equal(t, 1, len(mockedHTTPClient.DoCalls()))
		})
	}
}

func TestSpam_CheckIsCasSpamEmptyUserID(t *testing.T) {
	mockedHTTPClient := &mocks.HTTPClientMock{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			assert.Fail(t, "HTTP client should not be called with empty userID")
			return nil, nil
		},
	}

	d := NewDetector(Config{
		CasAPI:           "http://localhost",
		HTTPClient:       mockedHTTPClient,
		MaxAllowedEmoji:  -1,
		FirstMessageOnly: true,
	})
	spam, cr := d.Check(spamcheck.Request{UserID: "", Msg: "test message"})
	assert.False(t, spam)

	// find the CAS check in the results
	var casCheck *spamcheck.Response
	for _, check := range cr {
		if check.Name == "cas" {
			casCheck = &check
			break
		}
	}

	require.NotNil(t, casCheck, "CAS check should be included in results")
	assert.False(t, casCheck.Spam)
	assert.Equal(t, "check disabled", casCheck.Details)

	// verify HTTP client was never called
	assert.Equal(t, 0, len(mockedHTTPClient.DoCalls()))
}

func TestSpam_CheckIsCasSpamUserAgent(t *testing.T) {
	const customUserAgent = "MyCustomUserAgent/1.0"

	tests := []struct {
		name           string
		userAgent      string
		expectedHeader string
	}{
		{
			name:           "with custom user agent",
			userAgent:      customUserAgent,
			expectedHeader: customUserAgent,
		},
		{
			name:           "with default user agent",
			userAgent:      "",
			expectedHeader: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockedHTTPClient := &mocks.HTTPClientMock{
				DoFunc: func(req *http.Request) (*http.Response, error) {
					// check if User-Agent header is set correctly
					if tt.expectedHeader != "" {
						assert.Equal(t, tt.expectedHeader, req.Header.Get("User-Agent"))
					} else {
						// default User-Agent should be Go's default, let's just verify it's not our custom one
						assert.NotEqual(t, customUserAgent, req.Header.Get("User-Agent"))
					}

					return &http.Response{
						StatusCode: 200,
						Body:       io.NopCloser(bytes.NewBufferString(`{"ok": false, "description": "Not a spammer"}`)),
					}, nil
				},
			}

			d := NewDetector(Config{
				CasAPI:           "http://localhost",
				CasUserAgent:     tt.userAgent,
				HTTPClient:       mockedHTTPClient,
				MaxAllowedEmoji:  -1,
				FirstMessageOnly: true,
			})

			d.Check(spamcheck.Request{UserID: "123", Msg: "test message"})

			// verify HTTP client was called
			assert.Equal(t, 1, len(mockedHTTPClient.DoCalls()))
		})
	}
}

func TestDetector_CheckSimilarity(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1})
	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, nil)
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 2}, lr)
	d.classifier.reset() // we don't need a classifier for this test
	assert.Len(t, d.tokenizedSpam, 2)
	t.Logf("%+v", d.tokenizedSpam)
	assert.Equal(t, map[string]int{"win": 1, "free": 1, "iphone": 1}, d.tokenizedSpam[0])
	assert.Equal(t, map[string]int{"lottery": 1, "prize": 1}, d.tokenizedSpam[1])

	tests := []struct {
		name      string
		message   string
		threshold float64
		expected  bool
	}{
		{"Not Spam", "Hello, how are you?", 0.5, false},
		{"Exact Match", "Win a free iPhone now!", 0.5, true},
		{"Similar Match", "You won a lottery prize!", 0.3, true},
		{"High Threshold", "You won a lottery prize!", 0.9, false},
		{"Partial Match", "win free", 0.9, false},
		{"Low Threshold", "win free", 0.8, true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d.Config.SimilarityThreshold = test.threshold // update threshold for each test case
			spam, cr := d.Check(spamcheck.Request{Msg: test.message})
			assert.Equal(t, test.expected, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "similarity", cr[0].Name)
		})
	}
}

func TestDetector_CheckClassifier(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1, MinSpamProbability: 60})
	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	hamsSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamsSamples})
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 2, HamSamples: 3}, lr)
	d.tokenizedSpam = nil // we don't need tokenizedSpam samples for this test
	assert.Equal(t, 5, d.classifier.nAllDocument)
	exp := map[string]map[spamClass]int{"win": {"spam": 1}, "free": {"spam": 1}, "iphone": {"spam": 1}, "lottery": {"spam": 1},
		"prize": {"spam": 1}, "hello": {"ham": 1}, "world": {"ham": 1}, "how": {"ham": 1}, "are": {"ham": 1}, "you": {"ham": 1},
		"have": {"ham": 1}, "good": {"ham": 1}, "day": {"ham": 1}}
	assert.Equal(t, exp, d.classifier.learningResults)

	tests := []struct {
		name     string
		message  string
		expected bool
		desc     string
	}{
		{"clean ham", "Hello, how are you?", false, "probability of ham: 92.83%"},
		{"clean spam", "Win a free iPhone now!", true, "probability of spam: 90.81%"},
		{"a little bit spam", "You won a free lottery iphone good day", true, "probability of spam: 66.23%"},
		{"spam below threshold", "You won a free lottery iphone have a good day", false, "probability of spam: 53.36%"},
		{"mostly ham", "win a good day", false, "probability of ham: 65.39%"},
		{"mostly spam", "free  blah another one user writes good things iPhone day", true, "probability of spam: 75.70%"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spam, cr := d.Check(spamcheck.Request{Msg: test.message})
			assert.Equal(t, test.expected, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "classifier", cr[0].Name)
			assert.Equal(t, test.expected, cr[0].Spam)
			t.Logf("%+v", cr[0].Details)
			assert.Equal(t, test.desc, cr[0].Details)
		})
	}

	t.Run("without minSpamProbability", func(t *testing.T) {
		d.MinSpamProbability = 0
		spam, cr := d.Check(spamcheck.Request{Msg: "You won a free lottery iphone have a good day"})
		assert.True(t, spam)
		assert.Equal(t, "probability of spam: 53.36%", cr[0].Details)

	})
}

func TestDetector_CheckClassifierNoHam(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1, MinSpamProbability: 60})
	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, nil)
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 2, HamSamples: 0}, lr)
	d.tokenizedSpam = nil // we don't need tokenizedSpam samples for this test
	assert.Equal(t, 2, d.classifier.nAllDocument)
	assert.Equal(t, 2, d.classifier.nDocumentByClass["spam"])
	assert.Equal(t, 0, d.classifier.nDocumentByClass["ham"])
	exp := map[string]map[spamClass]int{"win": {"spam": 1}, "free": {"spam": 1}, "iphone": {"spam": 1},
		"lottery": {"spam": 1}, "prize": {"spam": 1}}
	assert.Equal(t, exp, d.classifier.learningResults)

	tests := []string{
		"Hello, how are you?",
		"Win a free iPhone now!",
		"You won a free lottery iphone good day",
		"You won a free lottery iphone have a good day",
		"win a good day",
		"free  blah another one user writes good things iPhone day",
	}

	for _, test := range tests {
		t.Run(test, func(t *testing.T) {
			spam, cr := d.Check(spamcheck.Request{Msg: test})
			assert.False(t, spam)
			require.Len(t, cr, 0)
		})
	}
}

func TestDetector_CheckOpenAI(t *testing.T) {
	t.Run("with openai and first-only", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.Equal(t, true, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "openai", cr[0].Name)
		assert.Equal(t, true, cr[0].Spam)
		assert.Equal(t, "bad text, confidence: 100%", cr[0].Details)
		assert.Equal(t, 1, len(mockOpenAIClient.CreateChatCompletionCalls()))
	})

	t.Run("with openai and not first-only", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.Equal(t, false, spam)
		require.Len(t, cr, 0)
		assert.Equal(t, 0, len(mockOpenAIClient.CreateChatCompletionCalls()))
	})

	t.Run("without openai", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1})
		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.Equal(t, false, spam)
		require.Len(t, cr, 0)
	})

	t.Run("with openai, first-only but spam detected before", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		_, err := d.LoadStopWords(strings.NewReader("some message"))
		assert.NoError(t, err)

		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.Equal(t, true, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.Equal(t, true, cr[0].Spam)
		assert.Equal(t, "some message", cr[0].Details)
		assert.Equal(t, 0, len(mockOpenAIClient.CreateChatCompletionCalls()))
	})

	t.Run("with openai, first-only spam detected before, veto passes", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true, OpenAIVeto: true})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		_, err := d.LoadStopWords(strings.NewReader("some message"))
		assert.NoError(t, err)

		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.Equal(t, true, spam)
		require.Len(t, cr, 2)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.Equal(t, true, cr[0].Spam)
		assert.Equal(t, "some message", cr[0].Details)

		assert.Equal(t, "openai", cr[1].Name)
		assert.Equal(t, true, cr[1].Spam)
		assert.Equal(t, "bad text, confidence: 100%", cr[1].Details)

		assert.Equal(t, 1, len(mockOpenAIClient.CreateChatCompletionCalls()))
	})

	t.Run("with openai, first-only spam detected before, veto failed", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true, OpenAIVeto: true})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": false, "reason":"good text", "confidence":100}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		_, err := d.LoadStopWords(strings.NewReader("some message"))
		assert.NoError(t, err)
		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.Equal(t, false, spam)
		require.Len(t, cr, 2)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.Equal(t, true, cr[0].Spam)
		assert.Equal(t, "some message", cr[0].Details)

		assert.Equal(t, "openai", cr[1].Name)
		assert.Equal(t, false, cr[1].Spam)
		assert.Equal(t, "good text, confidence: 100%", cr[1].Details)

		assert.Equal(t, 1, len(mockOpenAIClient.CreateChatCompletionCalls()))
	})

	t.Run("with openai, first-only spam detected before, openai error", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true, OpenAIVeto: true})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": false, "reason":"good text", "confidence":100}`},
					}},
				}, errors.New("openai error")
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		_, err := d.LoadStopWords(strings.NewReader("some message"))
		assert.NoError(t, err)
		spam, cr := d.Check(spamcheck.Request{Msg: "some message 1234"})
		assert.Equal(t, true, spam)
		require.Len(t, cr, 2)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.Equal(t, true, cr[0].Spam)
		assert.Equal(t, "some message", cr[0].Details)

		assert.Equal(t, "openai", cr[1].Name)
		assert.Equal(t, false, cr[1].Spam)
		assert.Equal(t, "OpenAI error: failed to create chat completion: openai error", cr[1].Details)
		assert.Equal(t, "failed to create chat completion: openai error", cr[1].Error.Error())

		assert.Equal(t, 1, len(mockOpenAIClient.CreateChatCompletionCalls()))
	})

	t.Run("with openai, first-only spam not detected before", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: -1, FirstMessageOnly: true, OpenAIVeto: false})
		mockOpenAIClient := &mocks.OpenAIClientMock{
			CreateChatCompletionFunc: func(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
				return openai.ChatCompletionResponse{
					Choices: []openai.ChatCompletionChoice{{
						Message: openai.ChatCompletionMessage{Content: `{"spam": true, "reason":"bad text", "confidence":100}`},
					}},
				}, nil
			},
		}
		d.WithOpenAIChecker(mockOpenAIClient, OpenAIConfig{Model: "gpt4"})
		_, err := d.LoadStopWords(strings.NewReader("some message"))
		assert.NoError(t, err)

		spam, cr := d.Check(spamcheck.Request{Msg: "1234"})
		assert.Equal(t, true, spam)
		assert.Equal(t, "stopword", cr[0].Name)
		assert.Equal(t, false, cr[0].Spam)
		assert.Equal(t, "not found", cr[0].Details)

		assert.Equal(t, "openai", cr[1].Name)
		assert.Equal(t, true, cr[1].Spam)
		assert.Equal(t, "bad text, confidence: 100%", cr[1].Details)

		assert.Equal(t, 1, len(mockOpenAIClient.CreateChatCompletionCalls()))
	})
}

func TestDetector_CheckWithMeta(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1})
	d.WithMetaChecks(LinksCheck(1), ImagesCheck(), LinkOnlyCheck())

	t.Run("no links, no images", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello, how are you?"})
		assert.Equal(t, false, spam)
		require.Len(t, cr, 3)
		assert.Equal(t, "links", cr[0].Name)
		assert.Equal(t, false, cr[0].Spam)
		assert.Equal(t, "links 0/1", cr[0].Details)
		assert.Equal(t, "images", cr[1].Name)
		assert.Equal(t, false, cr[1].Spam)
		assert.Equal(t, "no images without text", cr[1].Details)
		assert.Equal(t, "link-only", cr[2].Name)
		assert.Equal(t, false, cr[2].Spam)
	})

	t.Run("one link, no images", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello, how are you? https://google.com"})
		assert.Equal(t, false, spam)
		require.Len(t, cr, 3)
		assert.Equal(t, "links", cr[0].Name)
		assert.Equal(t, false, cr[0].Spam)
		assert.Equal(t, "links 1/1", cr[0].Details)
		assert.Equal(t, "images", cr[1].Name)
		assert.Equal(t, false, cr[1].Spam)
		assert.Equal(t, "no images without text", cr[1].Details)
		assert.Equal(t, "link-only", cr[2].Name)
		assert.Equal(t, false, cr[2].Spam)
	})

	t.Run("one link, no text", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: " https://google.com"})
		assert.Equal(t, true, spam)
		t.Logf("%+v", cr)
		require.Len(t, cr, 3)
		assert.Equal(t, "links", cr[0].Name)
		assert.Equal(t, false, cr[0].Spam)
		assert.Equal(t, "links 1/1", cr[0].Details)
		assert.Equal(t, "images", cr[1].Name)
		assert.Equal(t, false, cr[1].Spam)
		assert.Equal(t, "no images without text", cr[1].Details)
		assert.Equal(t, "link-only", cr[2].Name)
		assert.Equal(t, true, cr[2].Spam)
	})

	t.Run("one link, one image with some text", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello, how are you? https://google.com", Meta: spamcheck.MetaData{Images: 1}})
		assert.Equal(t, false, spam)
		t.Logf("%+v", cr)
		require.Len(t, cr, 3)
		assert.Equal(t, "links", cr[0].Name)
		assert.Equal(t, false, cr[0].Spam)
		assert.Equal(t, "links 1/1", cr[0].Details)
		assert.Equal(t, "images", cr[1].Name)
		assert.Equal(t, false, cr[1].Spam)
		assert.Equal(t, "no images without text", cr[1].Details)
		assert.Equal(t, "link-only", cr[2].Name)
		assert.Equal(t, false, cr[2].Spam)
		assert.Equal(t, "message contains text", cr[2].Details)
	})

	t.Run("two links, one image with text", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "Hello, how are you? https://google.com https://google.com", Meta: spamcheck.MetaData{Images: 1}})
		assert.Equal(t, true, spam)
		t.Logf("%+v", cr)
		require.Len(t, cr, 3)
		assert.Equal(t, "links", cr[0].Name)
		assert.Equal(t, true, cr[0].Spam)
		assert.Equal(t, "too many links 2/1", cr[0].Details)
		assert.Equal(t, "images", cr[1].Name)
		assert.Equal(t, false, cr[1].Spam)
		assert.Equal(t, "no images without text", cr[1].Details)
		assert.Equal(t, "link-only", cr[2].Name)
		assert.Equal(t, false, cr[2].Spam)
		assert.Equal(t, "message contains text", cr[2].Details)
	})

	t.Run("no links, two images, no text", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "", Meta: spamcheck.MetaData{Images: 2}})
		assert.Equal(t, true, spam)
		t.Logf("%+v", cr)
		require.Len(t, cr, 3)
		assert.Equal(t, "links", cr[0].Name)
		assert.Equal(t, false, cr[0].Spam)
		assert.Equal(t, "links 0/1", cr[0].Details)
		assert.Equal(t, "images", cr[1].Name)
		assert.Equal(t, true, cr[1].Spam)
		assert.Equal(t, "images without text", cr[1].Details)
		assert.Equal(t, "link-only", cr[2].Name)
		assert.Equal(t, false, cr[2].Spam)
		assert.Equal(t, "empty message", cr[2].Details)
	})
}

func TestDetector_CheckMultiLang(t *testing.T) {
	d := NewDetector(Config{MultiLangWords: 2, MaxAllowedEmoji: -1})
	tests := []struct {
		name  string
		input string
		count int
		spam  bool
	}{
		{"No MultiLang", "Hello, world!\n 12345-980! _", 0, false},
		{"One MultiLang", "Hi therе", 1, false},
		{"Two MultiLang", "Gооd moфning", 2, true},
		{"WithCyrillic no MultiLang", "Привет  мир", 0, false},
		{"WithCyrillic two MultiLang", "Привеt  мip", 2, true},
		{"WithCyrillic and special symbols", "Привет мир -@#$%^&*(_", 0, false},
		{"WithCyrillic real example 1", "Ищем заинтeрeсoвaнных в зaрaбoткe нa кpиптoвaлютe. Всeгдa хотeли пoпpoбовать сeбя в этом, нo нe знали с чeго нaчaть? Тогдa вaм кo мнe 3aнимаемся aрбuтражeм, зaрабaтывaeм на paзницe курсов с минимaльныmи pискaми 💲Рынok oчень волатильный и нам это выгoднo, пo этoмe пишиte @vitalgoescra и зapaбaтывaйтe сo мнoй ", 31, true},
		{"WithCyrillic real example 2", "В поuске паpтнеров, заuнтересованных в пассuвном дoходе с затpатой мuнuмум лuчного временu. Все деталu в лс", 10, true},
		{"WithCyrillic real example 3", "Всем привет, есть простая шабашка, подойдет любому. Даю 15 тысяч. Накину на проезд, сигареты, обед. ", 0, false},
		{"WithCyrillic and i", "Привет  мiр", 0, false},
		{"strange with cyrillic", "𝐇айди и𝐇𝐓и𝐦𝐇ы𝐞 ф𝐨𝐓𝐤и лю𝐛𝐨й д𝐞𝐁𝐲ш𝐤и ч𝐞𝐩𝐞𝟓 𝐛𝐨𝐓а", 7, true},
		{"coptic capital leter", "✔️ⲠⲢⲞⳜⲈЙ-ⲖЮⳜⲨЮ-ⲆⲈⲂⲨⲰⲔⲨ...\n\nⲎⲀЙⲆⳘ ⲤⲔⲢЫⲦⲈ ⲂⳘⲆⲞⲤЫ-ⲪⲞⲦⲞⳠⲔⳘ ⳘⲎⲦⳘⲘⲎⲞⲄⲞ-ⲬⲀⲢⲀⲔⲦⲈⲢⲀ..\n@INTIM0CHKI110DE\n\n", 5, true},
		{"mix with gothic, cyrillic and greek", "𐌿РОВЕРЬ ЛЮБУЮ НА НАЛИЧИЕ ПОШЛЫХ ΦΟͲΟ-ΒͶДξΟ, 🍑НАБЕРИ В Т𐌲 𐌿ОИСКЕ СЛOВО: 30GRL", 5, true},
		{"Mixed Latin and numbers", "Meta-Llama-3.1-8B-Instruct-IQ4_XS Meta-Llama-3.1-8B-Instruct-Q3_K_L Meta-Llama-3.1-8B-Instruct-Q4_K_M", 0, false},
		{"Mixed Latin, numbers, and Cyrillic", "Meta-Llama-3.1-8B-Instruct-IQ4_XS Meta-Llama-3.1-8B-Instruct-Q3_K_L коллеги, подскажите пожалуйста", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spam, cr := d.Check(spamcheck.Request{Msg: tt.input})
			assert.Equal(t, tt.spam, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "multi-lingual", cr[0].Name)
			assert.Equal(t, tt.spam, cr[0].Spam)
			assert.Equal(t, fmt.Sprintf("%d/2", tt.count), cr[0].Details)
		})
	}

	d.MultiLangWords = 0 // disable multi-lingual check
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spam, _ := d.Check(spamcheck.Request{Msg: tt.input})
			assert.False(t, spam)
		})
	}
}

func TestDetector_CheckWithAbnormalSpacing(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1})
	d.Config.AbnormalSpacing.Enabled = true
	d.Config.AbnormalSpacing.ShortWordLen = 3
	d.Config.AbnormalSpacing.ShortWordRatioThreshold = 0.7
	d.Config.AbnormalSpacing.SpaceRatioThreshold = 0.3
	d.Config.AbnormalSpacing.MinWordsCount = 6

	tests := []struct {
		name     string
		input    string
		expected bool
		details  string
	}{
		{
			name:     "normal text",
			input:    "This is a normal message with regular spacing between words",
			expected: false,
			details:  "normal (ratio: 0.18, short: 20%)",
		},
		{
			name:     "another normal text",
			input:    "For structured content, like code or tables, the ratio can vary significantly based on formatting and indentation",
			expected: false,
			details:  "normal (ratio: 0.17, short: 35%)",
		},
		{
			name: "normal text, cyrillic",
			input: "Если продолжать этот парад благородного безумия, я бы предложил базу имеющихся примеров спама" +
				" сконвертировать в эмбеддинги и проверять через них после байеса, но до gpt4. Эмбеддинги должны быть" +
				" устойчивы к разделенным словам и даже переформулировкам, при этом они гораздо дешевле большой модели.",
			expected: false,
			details:  "normal (ratio: 0.17, short: 26%)",
		},
		{
			name:     "suspicious spacing cyrillic",
			input:    "СРО ЧНО ЭТО КАСА ЕТСЯ КАЖ ДОГО В ЭТ ОЙ ГРУ ППЕ",
			expected: true,
			details:  "abnormal (ratio: 0.31, short: 75%)",
		},
		{
			name:     "very suspicious spacing cyrillic",
			input:    "н а р к о т и к о в, и н в е с т и ц и й",
			expected: true,
			details:  "abnormal (ratio: 0.95, short: 100%)",
		},
		{
			name:     "mixed normal and suspicious",
			input:    "Hello there к а ж д о г о в э т о й г р у п п е",
			expected: true,
			details:  "abnormal (ratio: 0.68, short: 90%)",
		},
		{
			name:     "short spaced text",
			input:    "a b c d",
			expected: false, // too short overall
			details:  "too short",
		},
		{
			name:     "text with some extra spaces",
			input:    "This   is    a    message    with    some    extra    spaces",
			expected: true,
			details:  "abnormal (ratio: 0.82, short: 25%)",
		},
		{
			name: "real spam example",
			input: "СРО ЧНО ЭТО КАСА ЕТСЯ КАЖ ДОГО В ЭТ ОЙ ГРУ ППЕ  Строго 20+ В данный момент про ходит обуч ение " +
				"для нови чков  Сразу говорю - без н а р к о т и к о в, и н в е с т и ц и й и прочей еру нды.  Быстрый старт, " +
				"при быль вы полу чите уже в пе рвый день раб   Все легально  Для раб оты нужен смарт фон и всего 1 час твоего " +
				"врем ени в день  Дове дём вас за ру чку до при были Не бер ём н и к а к и х о п л а т от вас, мы ра60таем " +
				"на % (вы зараба тываете и  только потом делитесь с нами) Кому действ ительно инте ресно " +
				"пишите в и я обязат ельно тебе отвечу",
			expected: true,
			details:  "abnormal (ratio: 0.36, short: 63%)",
		},
		{
			name:     "empty string",
			input:    "",
			expected: false,
			details:  "too short",
		},
		{
			name:     "low number of words",
			input:    "с впн всё работает",
			expected: false,
			details:  "too few words (4)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spam, resp := d.Check(spamcheck.Request{Msg: tt.input})
			t.Logf("Response: %+v", resp)
			require.Len(t, resp, 1)
			assert.Equal(t, tt.expected, spam, "spam detection mismatch")
			assert.Equal(t, tt.details, resp[0].Details, "details mismatch")
		})
	}

	t.Run("disabled short word threshold", func(t *testing.T) {
		d.Config.AbnormalSpacing.ShortWordLen = 0
		spam, resp := d.Check(spamcheck.Request{Msg: "СРО ЧНО ЭТО КАС АЕТ СЯ КАЖ ДОГО В ЭТ ОЙ ГРУ ППЕ something else"})
		t.Logf("Response: %+v", resp)
		assert.False(t, spam)
		assert.Equal(t, "normal (ratio: 0.29, short: 0%)", resp[0].Details)
	})

	t.Run("enabled short word threshold", func(t *testing.T) {
		d.Config.AbnormalSpacing.ShortWordLen = 3
		spam, resp := d.Check(spamcheck.Request{Msg: "СРО ЧНО ЭТО КАС АЕТ СЯ КАЖ ДОГО В ЭТ ОЙ ГРУ ППЕ something else"})
		t.Logf("Response: %+v", resp)
		assert.True(t, spam)
		assert.Equal(t, "abnormal (ratio: 0.29, short: 80%)", resp[0].Details)
	})

}

func TestDetector_UpdateSpam(t *testing.T) {
	upd := &mocks.SampleUpdaterMock{
		AppendFunc: func(msg string) error {
			return nil
		},
	}

	d := NewDetector(Config{MaxAllowedEmoji: -1})
	d.WithSpamUpdater(upd)

	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	hamsSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamsSamples})
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 2, HamSamples: 3}, lr)
	d.tokenizedSpam = nil // we don't need tokenizedSpam samples for this test
	assert.Equal(t, 5, d.classifier.nAllDocument)
	exp := map[string]map[spamClass]int{"win": {"spam": 1}, "free": {"spam": 1}, "iphone": {"spam": 1}, "lottery": {"spam": 1},
		"prize": {"spam": 1}, "hello": {"ham": 1}, "world": {"ham": 1}, "how": {"ham": 1}, "are": {"ham": 1}, "you": {"ham": 1},
		"have": {"ham": 1}, "good": {"ham": 1}, "day": {"ham": 1}}
	assert.Equal(t, exp, d.classifier.learningResults)

	msg := "another good world one iphone user writes good things day"
	t.Run("initially a little bit ham", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: msg})
		assert.Equal(t, false, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "classifier", cr[0].Name)
		assert.Equal(t, false, cr[0].Spam)
		assert.Equal(t, "probability of ham: 59.97%", cr[0].Details)
	})

	err = d.UpdateSpam("another user writes")
	assert.NoError(t, err)
	assert.Equal(t, 6, d.classifier.nAllDocument)
	assert.Equal(t, 1, len(upd.AppendCalls()))

	t.Run("after update mostly spam", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: msg})
		assert.Equal(t, true, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "classifier", cr[0].Name)
		assert.Equal(t, true, cr[0].Spam)
		assert.Equal(t, "probability of spam: 66.67%", cr[0].Details)
	})
}

func TestDetector_UpdateHam(t *testing.T) {
	upd := &mocks.SampleUpdaterMock{
		AppendFunc: func(msg string) error {
			return nil
		},
	}

	d := NewDetector(Config{MaxAllowedEmoji: -1})
	d.WithHamUpdater(upd)

	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	hamsSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamsSamples})
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 2, HamSamples: 3}, lr)
	d.tokenizedSpam = nil // we don't need tokenizedSpam samples for this test
	assert.Equal(t, 5, d.classifier.nAllDocument)
	exp := map[string]map[spamClass]int{"win": {"spam": 1}, "free": {"spam": 1}, "iphone": {"spam": 1}, "lottery": {"spam": 1},
		"prize": {"spam": 1}, "hello": {"ham": 1}, "world": {"ham": 1}, "how": {"ham": 1}, "are": {"ham": 1}, "you": {"ham": 1},
		"have": {"ham": 1}, "good": {"ham": 1}, "day": {"ham": 1}}
	assert.Equal(t, exp, d.classifier.learningResults)

	msg := "another free good world one iphone user writes good things day"
	t.Run("initially a little bit spam", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: msg})
		assert.Equal(t, true, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "classifier", cr[0].Name)
		assert.Equal(t, true, cr[0].Spam)
		assert.Equal(t, "probability of spam: 60.89%", cr[0].Details)
	})

	err = d.UpdateHam("another writes things")
	assert.NoError(t, err)
	assert.Equal(t, 6, d.classifier.nAllDocument)
	assert.Equal(t, 1, len(upd.AppendCalls()))

	t.Run("after update mostly ham", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: msg})
		assert.Equal(t, false, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "classifier", cr[0].Name)
		assert.Equal(t, false, cr[0].Spam)
		assert.Equal(t, "probability of ham: 72.16%", cr[0].Details)
	})
}

func TestDetector_Reset(t *testing.T) {
	d := NewDetector(Config{})
	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	hamSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamSamples})
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 2, HamSamples: 3}, lr)
	sr, err := d.LoadStopWords(strings.NewReader("в личку\nвсем привет"))
	require.NoError(t, err)
	assert.Equal(t, LoadResult{StopWords: 2}, sr)

	assert.Equal(t, 5, d.classifier.nAllDocument)
	assert.Equal(t, 2, len(d.tokenizedSpam))
	assert.Equal(t, 1, len(d.excludedTokens))
	assert.Equal(t, 2, len(d.stopWords))

	d.Reset()
	assert.Equal(t, 0, d.classifier.nAllDocument)
	assert.Equal(t, 0, len(d.tokenizedSpam))
	assert.Equal(t, 0, len(d.excludedTokens))
	assert.Equal(t, 0, len(d.stopWords))
}

func TestDetector_FirstMessagesCount(t *testing.T) {
	t.Run("first message is spam", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: 1, MinMsgLen: 5, FirstMessagesCount: 2, FirstMessageOnly: true})
		spam, _ := d.Check(spamcheck.Request{Msg: "spam, too many emojis 🤣🤣🤣", UserID: "123"})
		assert.Equal(t, true, spam)
	})
	t.Run("first messages are ham, third is spam", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: 1, MinMsgLen: 5, FirstMessagesCount: 3, FirstMessageOnly: true})

		// first ham
		spam, _ := d.Check(spamcheck.Request{Msg: "ham, no emojis", UserID: "123"})
		assert.Equal(t, false, spam)

		// second ham
		spam, _ = d.Check(spamcheck.Request{Msg: "ham, no emojis", UserID: "123"})
		assert.Equal(t, false, spam)

		spam, _ = d.Check(spamcheck.Request{Msg: "spam, too many emojis 🤣🤣🤣", UserID: "123"})
		assert.Equal(t, true, spam)
	})
	t.Run("first messages are ham, spam after approved", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: 1, MinMsgLen: 5, FirstMessagesCount: 2, FirstMessageOnly: true})

		// first ham
		spam, _ := d.Check(spamcheck.Request{Msg: "ham, no emojis", UserID: "123"})
		assert.Equal(t, false, spam)

		// second ham
		spam, _ = d.Check(spamcheck.Request{Msg: "ham, no emojis", UserID: "123"})
		assert.Equal(t, false, spam)

		spam, _ = d.Check(spamcheck.Request{Msg: "spam, too many emojis 🤣🤣🤣", UserID: "123"})
		assert.Equal(t, false, spam, "spam is not detected because user is approved")
	})
	t.Run("first messages are ham, spam after approved, FirstMessageOnly were false", func(t *testing.T) {
		d := NewDetector(Config{MaxAllowedEmoji: 1, MinMsgLen: 5, FirstMessagesCount: 2, FirstMessageOnly: false})

		// first ham
		spam, _ := d.Check(spamcheck.Request{Msg: "ham, no emojis", UserID: "123"})
		assert.Equal(t, false, spam)

		// second ham
		spam, _ = d.Check(spamcheck.Request{Msg: "ham, no emojis", UserID: "123"})
		assert.Equal(t, false, spam)

		spam, _ = d.Check(spamcheck.Request{Msg: "spam, too many emojis 🤣🤣🤣", UserID: "123"})
		assert.Equal(t, false, spam)
	})
}

func TestDetector_ApprovedUsers(t *testing.T) {
	mockUserStore := &mocks.UserStorageMock{
		ReadFunc: func(context.Context) ([]approved.UserInfo, error) {
			return []approved.UserInfo{{UserID: "123"}, {UserID: "456"}}, nil
		},
		WriteFunc:  func(_ context.Context, au approved.UserInfo) error { return nil },
		DeleteFunc: func(_ context.Context, id string) error { return nil },
	}

	t.Run("load with storage", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		count, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
		err = d.AddApprovedUser(approved.UserInfo{UserID: "999", UserName: "test"})
		assert.NoError(t, err)
		res := d.ApprovedUsers()
		ids := make([]string, len(res))
		for i, u := range res {
			ids[i] = u.UserID
		}
		sort.Strings(ids)
		assert.Equal(t, []string{"123", "456", "999"}, ids)
		assert.Equal(t, 1, len(mockUserStore.WriteCalls()))
		assert.Equal(t, "999", mockUserStore.WriteCalls()[0].Au.UserID)
		assert.Equal(t, "test", mockUserStore.WriteCalls()[0].Au.UserName)
	})

	t.Run("user not approved, spam detected", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		count, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
		_, err = d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "999"})
		t.Logf("%+v", info)
		assert.Equal(t, true, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "stopword", info[0].Name)
		assert.False(t, d.IsApprovedUser("999"))
		assert.Equal(t, 1, len(mockUserStore.ReadCalls()))
		assert.Equal(t, 0, len(mockUserStore.WriteCalls()))
		assert.Equal(t, 0, len(mockUserStore.DeleteCalls()))
	})

	t.Run("user pre-approved, spam check avoided", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		count, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
		_, err = d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "123"})
		t.Logf("%+v", info)
		assert.Equal(t, false, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "pre-approved", info[0].Name)
		assert.True(t, d.IsApprovedUser("123"))
	})

	t.Run("user pre-approved with count, spam check avoided", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessagesCount: 10})
		_, err := d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)
		count, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "123"})
		t.Logf("%+v", info)
		assert.Equal(t, false, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "pre-approved", info[0].Name)
		assert.True(t, d.IsApprovedUser("123"))
	})

	t.Run("remove user with store", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		_, err := d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)
		count, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		err = d.AddApprovedUser(approved.UserInfo{UserID: "999"})
		assert.NoError(t, err)
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "999"})
		t.Logf("%+v", info)
		assert.Equal(t, false, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "pre-approved", info[0].Name)
		assert.True(t, d.IsApprovedUser("999"))
		assert.Equal(t, 1, len(mockUserStore.WriteCalls()))
		assert.Equal(t, "999", mockUserStore.WriteCalls()[0].Au.UserID)

		d.RemoveApprovedUser("123")
		isSpam, info = d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "123"})
		t.Logf("%+v", info)
		assert.Equal(t, true, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "stopword", info[0].Name)
		assert.False(t, d.IsApprovedUser("123"))
		assert.Equal(t, 1, len(mockUserStore.DeleteCalls()))
		assert.Equal(t, "123", mockUserStore.DeleteCalls()[0].ID)
	})

	t.Run("remove user no store", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		_, err := d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)

		d.AddApprovedUser(approved.UserInfo{UserID: "123"})
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "123"})
		t.Logf("%+v", info)
		assert.Equal(t, false, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "pre-approved", info[0].Name)
		assert.True(t, d.IsApprovedUser("123"))

		d.RemoveApprovedUser("123")
		isSpam, info = d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "123"})
		t.Logf("%+v", info)
		assert.Equal(t, true, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "stopword", info[0].Name)
		assert.False(t, d.IsApprovedUser("123"))
		assert.Equal(t, 0, len(mockUserStore.WriteCalls()))
		assert.Equal(t, 0, len(mockUserStore.DeleteCalls()))
	})

	t.Run("add user", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		_, err := d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)
		count, err := d.WithUserStorage(mockUserStore)
		require.NoError(t, err)
		assert.Equal(t, 2, count)

		d.AddApprovedUser(approved.UserInfo{UserID: "777"})
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "777"})
		t.Logf("%+v", info)
		assert.Equal(t, false, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "pre-approved", info[0].Name)
		assert.True(t, d.IsApprovedUser("777"))
		assert.Equal(t, 1, len(mockUserStore.WriteCalls()))
		assert.Equal(t, "777", mockUserStore.WriteCalls()[0].Au.UserID)
	})

	t.Run("add user, no store", func(t *testing.T) {
		mockUserStore.ResetCalls()
		d := NewDetector(Config{MaxAllowedEmoji: -1, MinMsgLen: 5, FirstMessageOnly: true})
		_, err := d.LoadStopWords(strings.NewReader("spam\nbuy cryptocurrency"))
		require.NoError(t, err)

		d.AddApprovedUser(approved.UserInfo{UserID: "777"})
		isSpam, info := d.Check(spamcheck.Request{Msg: "Hello, how are you my friend? buy cryptocurrency now!", UserID: "777"})
		t.Logf("%+v", info)
		assert.Equal(t, false, isSpam)
		require.Len(t, info, 1)
		assert.Equal(t, "pre-approved", info[0].Name)
		assert.True(t, d.IsApprovedUser("777"))
		assert.Equal(t, 0, len(mockUserStore.WriteCalls()))
	})

}

func TestDetector_LoadSamples(t *testing.T) {
	t.Run("basic loading", func(t *testing.T) {
		d := NewDetector(Config{})
		spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz XyZ")
		hamSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
		exclSamples := strings.NewReader("xyz")

		lr, err := d.LoadSamples(exclSamples, []io.Reader{spamSamples}, []io.Reader{hamSamples})

		require.NoError(t, err)
		assert.Equal(t, 1, lr.ExcludedTokens)
		assert.Equal(t, 2, lr.SpamSamples)
		assert.Equal(t, 3, lr.HamSamples)

		// verify excluded tokens
		assert.Contains(t, d.excludedTokens, "xyz")

		// verify tokenized spam samples
		assert.Len(t, d.tokenizedSpam, 2)
		assert.Contains(t, d.tokenizedSpam[0], "win")
		assert.Contains(t, d.tokenizedSpam[1], "lottery")

		// verify classifier learning
		assert.Equal(t, 5, d.classifier.nAllDocument)
		assert.Contains(t, d.classifier.learningResults, "win")
		assert.Contains(t, d.classifier.learningResults["win"], spamClass("spam"))
		assert.Contains(t, d.classifier.learningResults, "world")
		assert.Contains(t, d.classifier.learningResults["world"], spamClass("ham"))

		// verify excluded tokens in learning results
		assert.NotContains(t, d.classifier.learningResults, "xyz", "excluded token should not be in learning results")
		assert.NotContains(t, d.classifier.learningResults, "XyZ", "excluded token should not be in learning results")
	})

	t.Run("empty samples", func(t *testing.T) {
		d := NewDetector(Config{})
		exclSamples := strings.NewReader("")
		spamSamples := strings.NewReader("")
		hamSamples := strings.NewReader("")

		lr, err := d.LoadSamples(exclSamples, []io.Reader{spamSamples}, []io.Reader{hamSamples})

		require.NoError(t, err)
		assert.Equal(t, 0, lr.ExcludedTokens)
		assert.Equal(t, 0, lr.SpamSamples)
		assert.Equal(t, 0, lr.HamSamples)
		assert.Equal(t, 0, d.classifier.nAllDocument)
	})

	t.Run("multiple readers", func(t *testing.T) {
		d := NewDetector(Config{})
		exclSamples := strings.NewReader("xy\n z\n the\n")
		spamSamples1 := strings.NewReader("win free iPhone")
		spamSamples2 := strings.NewReader("lottery prize xyz")
		hamsSamples1 := strings.NewReader("hello world\nhow are you\nhave a good day")
		hamsSamples2 := strings.NewReader("some other text\nwith more words")

		lr, err := d.LoadSamples(
			exclSamples,
			[]io.Reader{spamSamples1, spamSamples2},
			[]io.Reader{hamsSamples1, hamsSamples2},
		)

		require.NoError(t, err)
		assert.Equal(t, 3, lr.ExcludedTokens)

		exTkns := []string{}
		for k := range d.excludedTokens {
			exTkns = append(exTkns, k)
		}
		sort.Strings(exTkns)

		assert.Equal(t, []string{"the", "xy", "z"}, exTkns)
		assert.Equal(t, 2, lr.SpamSamples)
		assert.Equal(t, 5, lr.HamSamples)
		t.Logf("Learning results: %+v", d.classifier.learningResults)
		assert.Equal(t, 7, d.classifier.nAllDocument)
		assert.Contains(t, d.classifier.learningResults["win"], spamClass("spam"))
		assert.Contains(t, d.classifier.learningResults["prize"], spamClass("spam"))
		assert.Contains(t, d.classifier.learningResults["world"], spamClass("ham"))
		assert.Contains(t, d.classifier.learningResults["some"], spamClass("ham"))
	})
}

func TestDetector_tokenize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]int
	}{
		{name: "empty", input: "", expected: map[string]int{}},
		{name: "no filters or cleanups", input: "hello world", expected: map[string]int{"hello": 1, "world": 1}},
		{name: "with excluded tokens", input: "hello world the she", expected: map[string]int{"hello": 1, "world": 1}},
		{name: "with short tokens", input: "hello world the she a or", expected: map[string]int{"hello": 1, "world": 1}},
		{name: "with repeated tokens", input: "hello world hello world", expected: map[string]int{"hello": 2, "world": 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Detector{excludedTokens: map[string]struct{}{"the": {}, "she": {}}}
			assert.Equal(t, tt.expected, d.tokenize(tt.input))
		})
	}
}

func TestDetector_readerIterator(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{name: "empty", input: "", expected: []string{}},
		{name: "token per line", input: "hello\nworld", expected: []string{"hello", "world"}},
		{name: "token per line", input: "hello 123\nworld", expected: []string{"hello 123", "world"}},
		{name: "token per line with spaces", input: "hello \n world", expected: []string{"hello", "world"}},
		{name: "multiple tokens per line with", input: " hello blah\n the new world ",
			expected: []string{"hello blah", "the new world"}},
		{name: "with extra EOL", input: " hello blah\n the new world \n  ", expected: []string{"hello blah", "the new world"}},
		{name: "with empty lines", input: " hello blah\n\n  \n the new world \n  \n", expected: []string{"hello blah", "the new world"}},
	}

	d := Detector{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := d.readerIterator(bytes.NewBufferString(tt.input))
			res := []string{}
			for token := range ch {
				res = append(res, token)
			}
			assert.Equal(t, tt.expected, res)
		})
	}
}

func TestDetector_readerIteratorMultipleReaders(t *testing.T) {
	d := Detector{}
	ch := d.readerIterator(bytes.NewBufferString("hello\nworld"), bytes.NewBufferString("something, new"))
	res := []string{}
	for token := range ch {
		res = append(res, token)
	}
	sort.Strings(res)
	assert.Equal(t, []string{"hello", "something, new", "world"}, res)
}

func TestCleanText(t *testing.T) {
	d := Detector{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "English text with word joiners",
			input:    "D\u2062ude i\u2062t i\u2062s t\u2062he la\u2062st da\u2062y",
			expected: "Dude it is the last day",
		},
		{
			name:     "Russian text with word joiners",
			input:    "Р\u2062ебята д\u2062авайте б\u2062ыстрее",
			expected: "Ребята давайте быстрее",
		},
		{
			name:     "Text with pop directional formatting",
			input:    "F\u2068ast t\u2068ake i\u2068t",
			expected: "Fast take it",
		},
		{
			name:     "Mixed invisible characters",
			input:    "Hello\u200BWorld\u2062Test\u206FCase",
			expected: "HelloWorldTestCase",
		},
		{
			name:     "No invisible characters",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "Only invisible characters",
			input:    "\u200B\u2062\u206F",
			expected: "",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "URLs with invisible characters",
			input:    "https://\u2062example\u2062.com/\u2062test",
			expected: "https://example.com/test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// test regex-based implementation
			result := d.cleanText(tt.input)
			assert.Equal(t, tt.expected, result, "failed for case: %s", tt.name)
		})
	}
}

//nolint:stylecheck // it has unicode symbols purposely
func Test_countEmoji(t *testing.T) {
	tests := []struct {
		name  string
		input string
		count int
	}{
		{"NoEmoji", "Hello, world!", 0},
		{"OneEmoji", "Hi there 👋", 1},
		{"DupEmoji", "️‍🌈Hi 👋there 👋", 3},
		{"TwoEmojis", "Good morning 🌞🌻", 2},
		{"Mixed", "👨‍👩👦 Family emoji", 3},
		{"TextAfterEmoji", "😊 Have a nice day!", 1},
		{"OnlyEmojis", "😁🐶🍕", 3},
		{"WithCyrillic", "Привет 🌞 🍕 мир! 👋", 3},
		{"real1", "❗️НУЖЕН 1 ЧЕЛОВЕК НА ДИСТАНЦИОННУЮ РАБОТУ❗️", 2},
		{"real2", "⏰💯⚡️💯🤝🤝🤝🤝🤝🤝🤝🤝  ❗️HУЖHЫ OТВЕТCТВЕHHЫЕ ЛЮДИ❗️              🔤🔤  ➡️@yyyyy🥢" +
			"  ⚡️(OТ 2️⃣1️⃣ ВOЗРАCТ)🟢 🔋OHЛАЙH ЗАРАБOТOК 🟢 ✅COПРOВOЖДЕHИЕ🟢 ❗1-2 ЧАCА В ДЕHЬ 🟢   👍1️⃣2️⃣0️⃣0️⃣💸" +
			"➕в неделю🟢 ПИCАТЬ ✉️@xxxxxx✉️", 38},
		{"real3", "‼️СРОЧНО‼️  ‼️ЭТО КАСАЕТСЯ КАЖДОГО В ЭТОЙ ГРУППЕ‼️  🔥Строго 20+  В данный момент проходит обучение " +
			"для новичков 🔥 Сразу говорю - без наркотиков, инвестиций и прочей ерунды. 🔥 Быстрый старт, прибыль вы получите" +
			" уже в первый день работы 🔥 Все легально 🔥 Для работы нужен смартфон и всего 1 час твоего времени" +
			" в день 🔥 Доведём вас за ручку до прибыли ‼️", 11},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.count, countEmoji(tt.input))
		})
	}
}

//nolint:stylecheck // it has unicode symbols purposely
func Test_cleanEmoji(t *testing.T) {
	tests := []struct {
		name  string
		input string
		clean string
	}{
		{"NoEmoji", "Hello, world!", "Hello, world!"},
		{"OneEmoji", "Hi there 👋", "Hi there "},
		{"TwoEmojis", "Good morning 🌞🌻", "Good morning "},
		{"Mixed", "👨‍👩‍👧‍👦 Family emoji", " Family emoji"},
		{"EmojiSequences", "🏳️‍🌈 Rainbow flag", " Rainbow flag"},
		{"TextAfterEmoji", "😊 Have a nice day!", " Have a nice day!"},
		{"OnlyEmojis", "😁🐶🍕", ""},
		{"WithCyrillic", "Привет 🌞 🍕 мир! 👋", "Привет   мир! "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.clean, cleanEmoji(tt.input))
		})
	}
}

func TestDetector_RemoveHam(t *testing.T) {
	// setup a test detector with mock updater
	updMock := &mocks.SampleUpdaterMock{
		RemoveFunc: func(msg string) error {
			return nil
		},
		AppendFunc: func(msg string) error {
			return nil
		},
	}

	d := NewDetector(Config{})
	d.WithHamUpdater(updMock)

	// setup the classifier with ham samples
	hamSamples := strings.NewReader("test message\nhello world")
	lr, err := d.LoadSamples(strings.NewReader(""), nil, []io.Reader{hamSamples})
	require.NoError(t, err)
	require.Equal(t, 2, lr.HamSamples)

	// test removing a ham sample that exists
	assert.NoError(t, d.RemoveHam("test message"))
	assert.Equal(t, 1, len(updMock.RemoveCalls()))
	assert.Equal(t, "test message", updMock.RemoveCalls()[0].Msg)

	// test error handling
	updMock.RemoveFunc = func(msg string) error {
		return errors.New("remove error")
	}
	err = d.RemoveHam("hello world")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "remove error")
}

func TestDetector_RemoveSpamHam(t *testing.T) {
	updSpam := &mocks.SampleUpdaterMock{
		RemoveFunc: func(msg string) error {
			return nil
		},
		AppendFunc: func(msg string) error {
			return nil
		},
	}

	d := NewDetector(Config{MaxAllowedEmoji: -1})
	d.WithSpamUpdater(updSpam)

	// load initial samples WITHOUT the test message
	spamSamples := strings.NewReader("lottery prize xyz")
	hamsSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	lr, err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamsSamples})
	require.NoError(t, err)
	assert.Equal(t, LoadResult{ExcludedTokens: 1, SpamSamples: 1, HamSamples: 3}, lr)

	// add our test message separately
	spamMsg := "win free iPhone"
	err = d.UpdateSpam(spamMsg)
	require.NoError(t, err)

	t.Run("initially classified as spam", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "win free iPhone hello world"})
		t.Logf("%+v", cr)
		require.True(t, spam, "should initially be classified as spam")
		require.NotEmpty(t, cr, "should have classification results")
		assert.Equal(t, "classifier", cr[0].Name)
		assert.True(t, cr[0].Spam)
	})

	err = d.RemoveSpam(spamMsg)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(updSpam.RemoveCalls()))
	assert.Equal(t, spamMsg, updSpam.RemoveCalls()[0].Msg)

	t.Run("after removing spam", func(t *testing.T) {
		spam, cr := d.Check(spamcheck.Request{Msg: "win free iPhone hello world"})
		t.Logf("%+v", cr)
		require.NotEmpty(t, cr, "should have classification results")
		assert.Equal(t, "classifier", cr[0].Name)
		assert.False(t, spam, "should no longer be classified as spam")
		assert.False(t, cr[0].Spam)
	})

	t.Run("error on updater", func(t *testing.T) {
		failingUpd := &mocks.SampleUpdaterMock{
			RemoveFunc: func(msg string) error {
				return fmt.Errorf("remove error")
			},
		}
		d.WithSpamUpdater(failingUpd)
		err := d.RemoveSpam(spamMsg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "can't unlearn spam samples")
	})

	t.Run("error on non-existent message", func(t *testing.T) {
		err := d.RemoveSpam("not-learned-message")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "can't unlearn spam samples")
	})
}

func TestDetector_buildDocs(t *testing.T) {
	d := &Detector{excludedTokens: map[string]struct{}{"the": {}, "and": {}}}

	docs := d.buildDocs("buy crypto coins now", "spam")
	assert.Equal(t, 1, len(docs), "should create single document")
	assert.Equal(t, spamClass("spam"), docs[0].spamClass)
	assert.ElementsMatch(t, []string{"buy", "crypto", "coins", "now"}, docs[0].tokens)
}

func TestDetector_CheckHistory(t *testing.T) {
	d := NewDetector(Config{HistorySize: 5})
	_, err := d.LoadStopWords(strings.NewReader("spam text\nbad text"))
	require.NoError(t, err)

	// first message is ham
	isSpam, _ := d.Check(spamcheck.Request{Msg: "good message", UserID: "1"})
	assert.False(t, isSpam)
	hamMsgs := d.hamHistory.Last(5)
	require.Equal(t, 1, len(hamMsgs))
	assert.Equal(t, "good message", hamMsgs[0].Msg)
	assert.Empty(t, d.spamHistory.Last(5))

	// second message is spam
	isSpam, _ = d.Check(spamcheck.Request{Msg: "spam text", UserID: "1"})
	assert.True(t, isSpam)
	spamMsgs := d.spamHistory.Last(5)
	require.Equal(t, 1, len(spamMsgs))
	assert.Equal(t, "spam text", spamMsgs[0].Msg)
	assert.Equal(t, 1, len(d.hamHistory.Last(5)), "ham history should remain unchanged")

	// third message is ham
	isSpam, _ = d.Check(spamcheck.Request{Msg: "another good one", UserID: "2"})
	assert.False(t, isSpam)
	hamMsgs = d.hamHistory.Last(5)
	require.Equal(t, 2, len(hamMsgs))
	assert.Equal(t, "good message", hamMsgs[0].Msg)
	assert.Equal(t, "another good one", hamMsgs[1].Msg)
	assert.Equal(t, 1, len(d.spamHistory.Last(5)), "spam history should remain unchanged")
}

func BenchmarkTokenize(b *testing.B) {
	d := &Detector{
		excludedTokens: map[string]struct{}{"the": {}, "and": {}, "or": {}, "but": {}, "in": {}, "on": {}, "at": {}, "to": {}},
	}

	tests := []struct {
		name string
		text string
	}{
		{
			name: "Short_NoExcluded",
			text: "hello world test message",
		},
		{
			name: "Short_WithExcluded",
			text: "the quick brown fox and the lazy dog",
		},
		{
			name: "Medium_Mixed",
			text: strings.Repeat("hello world and test message with some excluded tokens ", 10),
		},
		{
			name: "Long_MixedWithPunct",
			text: strings.Repeat("hello, world! test? message. with!! some... excluded tokens!!! ", 50),
		},
		{
			name: "WithEmoji",
			text: "hello 👋 world 🌍 test 🧪 message 📝 with emoji 😊",
		},
		{
			name: "RealWorldSample",
			text: "🔥 EXCLUSIVE OFFER! Don't miss out on this amazing deal. Buy now and get 50% OFF! Limited time offer. Click here: http://example.com #deal #shopping #discount",
		},
	}

	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = d.tokenize(tc.text)
			}
		})
	}
}

func BenchmarkLoadSamples(b *testing.B) {
	makeReader := func(lines []string) io.Reader {
		return strings.NewReader(strings.Join(lines, "\n"))
	}

	tests := []struct {
		name     string
		spam     []string
		ham      []string
		excluded []string
	}{
		{
			name:     "Small",
			spam:     []string{"spam message 1", "buy now spam 2", "spam offer 3"},
			ham:      []string{"hello world", "normal message", "how are you"},
			excluded: []string{"the", "and", "or"},
		},
		{
			name:     "Medium",
			spam:     []string{"spam message 1", "buy now spam 2", "spam offer 3", "urgent offer", "free money"},
			ham:      []string{"hello world", "normal message", "how are you", "meeting tomorrow", "project update"},
			excluded: []string{"the", "and", "or", "but", "in", "on", "at"},
		},
		{
			name: "Large_RealWorld",
			// use actual spam samples from your data
			spam: []string{
				"Здравствуйте   Мы занимаемая новым видом заработка в интернете   Наша сфера даст вам опыт, знания",
				"У кого нет карты карты Тинькофф? Можете оформить по моей ссылке и получите 500р от меня",
				"😀😀😀 Для тeх ктo ищeт дoпoлнительный доход предлагаю перспективный и прибыльный зaрaботok",
			},
			ham: []string{
				"When is our next meeting?",
				"Here's the project update you requested",
				"Thanks for the feedback, I'll review it",
			},
			excluded: []string{"the", "and", "or", "but", "in", "on", "at", "to", "for", "with"},
		},
	}

	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			d := NewDetector(Config{})
			spamReader := makeReader(tc.spam)
			hamReader := makeReader(tc.ham)
			exclReader := makeReader(tc.excluded)

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				// need to rewind readers for each iteration
				spamReader = makeReader(tc.spam)
				hamReader = makeReader(tc.ham)
				exclReader = makeReader(tc.excluded)

				_, err := d.LoadSamples(exclReader, []io.Reader{spamReader}, []io.Reader{hamReader})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
