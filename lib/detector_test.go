package lib

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/bot/mocks"
)

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
			d := Detector{
				excludedTokens: []string{"the", "she"},
			}
			assert.Equal(t, tt.expected, d.tokenize(tt.input))
		})
	}
}

func TestDetector_tokenChan(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{name: "empty", input: "", expected: []string{}},
		{name: "token per line", input: "hello\nworld", expected: []string{"hello", "world"}},
		{name: "token per line", input: "hello 123\nworld", expected: []string{"hello 123", "world"}},
		{name: "token per line with spaces", input: "hello \n world", expected: []string{"hello", "world"}},
		{name: "tokens comma separated", input: "\"hello\",\"world\"\nsomething", expected: []string{"hello", "world", "something"}},
		{name: "tokens comma separated, extra EOL", input: "\"hello\",world\nsomething\n", expected: []string{"hello", "world", "something"}},
		{name: "tokens comma separated, empty tokens", input: "\"hello\",world,\"\"\nsomething\n ", expected: []string{"hello", "world", "something"}},
	}

	d := Detector{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := d.tokenChan(bytes.NewBufferString(tt.input))
			res := []string{}
			for token := range ch {
				res = append(res, token)
			}
			assert.Equal(t, tt.expected, res)
		})
	}
}

func TestDetector_tokenChanMultipleReaders(t *testing.T) {
	d := Detector{}
	ch := d.tokenChan(bytes.NewBufferString("hello\nworld"), bytes.NewBufferString("something, new"))
	res := []string{}
	for token := range ch {
		res = append(res, token)
	}
	assert.Equal(t, []string{"hello", "world", "something, new"}, res)
}

func TestDetector_CheckStopWords(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1})
	err := d.LoadStopWords(bytes.NewBufferString("в личку\nвсем привет"))
	require.NoError(t, err)

	tests := []struct {
		name     string
		message  string
		expected bool
	}{
		{
			name:     "Stop word present",
			message:  "Hello, please send me a message в личку",
			expected: true,
		},
		{
			name:     "Stop word present with emoji",
			message:  "👋Всем привет\nИщу амбициозного человека к се6е в команду\nКто в поисках дополнительного заработка или хочет попробовать себя в новой  сфере деятельности! 👨🏻\u200d💻\nПишите в лс✍️",
			expected: true,
		},
		{
			name:     "No stop word present",
			message:  "Hello, how are you?",
			expected: false,
		},
		{
			name:     "Case insensitive stop word present",
			message:  "Hello, please send me a message В ЛИЧКУ",
			expected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spam, cr := d.Check(test.message, 0)
			assert.Equal(t, test.expected, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "stopword", cr[0].Name)
			t.Logf("%+v", cr[0].Details)
			if test.expected {
				assert.Subset(t, d.stopWords, []string{cr[0].Details})
			}
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
			spam, cr := d.Check(tt.input, 0)
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
			name:           "User is a spammer",
			mockResp:       `{"ok": true, "description": "Is a spammer"}`,
			mockStatusCode: 200,
			expected:       true,
		},
		{
			name:           "HTTP error",
			mockResp:       "{}",
			mockStatusCode: 500,
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockedHTTPClient := &mocks.HTTPClient{
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
			spam, cr := d.Check("", 123)
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
			assert.Equal(t, respDetails.Description, respDetails.Description)
			assert.Equal(t, 1, len(mockedHTTPClient.DoCalls()))
		})
	}
}

func TestDetector_CheckSimilarity(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1})
	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, nil)
	require.NoError(t, err)
	d.classifier.Reset() // we don't need classifier for this test
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
			d.Config.SimilarityThreshold = test.threshold // Update threshold for each test case
			spam, cr := d.Check(test.message, 0)
			assert.Equal(t, test.expected, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "similarity", cr[0].Name)
		})
	}
}

func TestDetector_CheckClassificator(t *testing.T) {
	d := NewDetector(Config{MaxAllowedEmoji: -1})
	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	hamsSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamsSamples})
	require.NoError(t, err)
	d.tokenizedSpam = nil // we don't need tokenizedSpam samples for this test
	assert.Equal(t, 5, d.classifier.NAllDocument)
	exp := map[string]map[Class]int{"win": {"spam": 1}, "free": {"spam": 1}, "iphone": {"spam": 1}, "lottery": {"spam": 1},
		"prize": {"spam": 1}, "hello": {"ham": 1}, "world": {"ham": 1}, "how": {"ham": 1}, "are": {"ham": 1}, "you": {"ham": 1},
		"have": {"ham": 1}, "good": {"ham": 1}, "day": {"ham": 1}}
	assert.Equal(t, exp, d.classifier.LearningResults)

	tests := []struct {
		name     string
		message  string
		expected bool
		desc     string
	}{
		{"clean ham", "Hello, how are you?", false, "spam:-12.4778, ham:-9.9163"},
		{"clean spam", "Win a free iPhone now!", true, "spam:-10.3983, ham:-12.6889"},
		{"mostly spam", "You won a free lottery iphone, have a good day", true, "spam:-21.9598, ham:-22.0944"},
		{"mostly ham", "win a good day", false, "spam:-8.8943, ham:-8.2581"},
		{"a little bit spam", "free  blah another one user writes good things iPhone day", true, "spam:-28.4337, ham:-29.5698"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spam, cr := d.Check(test.message, 0)
			assert.Equal(t, test.expected, spam)
			require.Len(t, cr, 1)
			assert.Equal(t, "classifier", cr[0].Name)
			assert.Equal(t, test.expected, cr[0].Spam)
			t.Logf("%+v", cr[0].Details)
			assert.Equal(t, test.desc, cr[0].Details)
		})
	}
}

func TestDetector_UpdateSpam(t *testing.T) {
	upd := &mocks.SampleUpdater{
		AppendFunc: func(msg string) error {
			return nil
		},
	}

	d := NewDetector(Config{MaxAllowedEmoji: -1})
	d.WithSpamUpdater(upd)

	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	hamsSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamsSamples})
	require.NoError(t, err)
	d.tokenizedSpam = nil // we don't need tokenizedSpam samples for this test
	assert.Equal(t, 5, d.classifier.NAllDocument)
	exp := map[string]map[Class]int{"win": {"spam": 1}, "free": {"spam": 1}, "iphone": {"spam": 1}, "lottery": {"spam": 1},
		"prize": {"spam": 1}, "hello": {"ham": 1}, "world": {"ham": 1}, "how": {"ham": 1}, "are": {"ham": 1}, "you": {"ham": 1},
		"have": {"ham": 1}, "good": {"ham": 1}, "day": {"ham": 1}}
	assert.Equal(t, exp, d.classifier.LearningResults)

	msg := "another good world one iphone user writes good things day"
	t.Run("initially a little bit ham", func(t *testing.T) {
		spam, cr := d.Check(msg, 0)
		assert.Equal(t, false, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "classifier", cr[0].Name)
		assert.Equal(t, false, cr[0].Spam)
		assert.Equal(t, "spam:-26.2365, ham:-25.8321", cr[0].Details)
	})

	err = d.UpdateSpam("another user writes")
	assert.NoError(t, err)
	assert.Equal(t, 6, d.classifier.NAllDocument)
	assert.Equal(t, 1, len(upd.AppendCalls()))

	t.Run("after update mostly spam", func(t *testing.T) {
		spam, cr := d.Check(msg, 0)
		assert.Equal(t, true, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "classifier", cr[0].Name)
		assert.Equal(t, true, cr[0].Spam)
		assert.Equal(t, "spam:-26.5230, ham:-27.2162", cr[0].Details)
	})
}

func TestDetector_UpdateHam(t *testing.T) {
	upd := &mocks.SampleUpdater{
		AppendFunc: func(msg string) error {
			return nil
		},
	}

	d := NewDetector(Config{MaxAllowedEmoji: -1})
	d.WithHamUpdater(upd)

	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	hamsSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamsSamples})
	require.NoError(t, err)
	d.tokenizedSpam = nil // we don't need tokenizedSpam samples for this test
	assert.Equal(t, 5, d.classifier.NAllDocument)
	exp := map[string]map[Class]int{"win": {"spam": 1}, "free": {"spam": 1}, "iphone": {"spam": 1}, "lottery": {"spam": 1},
		"prize": {"spam": 1}, "hello": {"ham": 1}, "world": {"ham": 1}, "how": {"ham": 1}, "are": {"ham": 1}, "you": {"ham": 1},
		"have": {"ham": 1}, "good": {"ham": 1}, "day": {"ham": 1}}
	assert.Equal(t, exp, d.classifier.LearningResults)

	msg := "another free good world one iphone user writes good things day"
	t.Run("initially a little bit spam", func(t *testing.T) {
		spam, cr := d.Check(msg, 0)
		assert.Equal(t, true, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "classifier", cr[0].Name)
		assert.Equal(t, true, cr[0].Spam)
		assert.Equal(t, "spam:-28.4337, ham:-28.8766", cr[0].Details)
	})

	err = d.UpdateHam("another writes things")
	assert.NoError(t, err)
	assert.Equal(t, 6, d.classifier.NAllDocument)
	assert.Equal(t, 1, len(upd.AppendCalls()))

	t.Run("after update mostly spam", func(t *testing.T) {
		spam, cr := d.Check(msg, 0)
		assert.Equal(t, false, spam)
		require.Len(t, cr, 1)
		assert.Equal(t, "classifier", cr[0].Name)
		assert.Equal(t, false, cr[0].Spam)
		assert.Equal(t, "spam:-30.1575, ham:-29.2050", cr[0].Details)
	})
}

func TestDetector_Reset(t *testing.T) {
	d := NewDetector(Config{})
	spamSamples := strings.NewReader("win free iPhone\nlottery prize xyz")
	hamSamples := strings.NewReader("hello world\nhow are you\nhave a good day")
	err := d.LoadSamples(strings.NewReader("xyz"), []io.Reader{spamSamples}, []io.Reader{hamSamples})
	require.NoError(t, err)
	err = d.LoadStopWords(strings.NewReader("в личку\nвсем привет"))
	require.NoError(t, err)

	assert.Equal(t, 5, d.classifier.NAllDocument)
	assert.Equal(t, 2, len(d.tokenizedSpam))
	assert.Equal(t, 1, len(d.excludedTokens))
	assert.Equal(t, 2, len(d.stopWords))

	d.Reset()
	assert.Equal(t, 0, d.classifier.NAllDocument)
	assert.Equal(t, 0, len(d.tokenizedSpam))
	assert.Equal(t, 0, len(d.excludedTokens))
	assert.Equal(t, 0, len(d.stopWords))
}
