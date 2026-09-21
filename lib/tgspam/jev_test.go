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
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam/mocks"
)

func validJevConfig() JevConfig {
	return JevConfig{
		Token:        "test-token",
		Question:     "Is `message` spam?",
		CriteriaSpam: "promotes or advertises",
		CriteriaHam:  "ordinary conversation",
		Threshold:    0.3,
	}
}

func jevRespBody(t *testing.T, body string) *http.Response {
	t.Helper()
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
}

func TestNewJevChecker_Defaults(t *testing.T) {
	checker, err := newJevChecker(&mocks.HTTPClientMock{}, validJevConfig())
	require.NoError(t, err)
	assert.Equal(t, defaultJevAPIBase, checker.params.APIBase)
	assert.Equal(t, defaultJevModel, checker.params.Model)
	assert.Equal(t, defaultJevMaxSymbolsRequest, checker.params.MaxSymbolsRequest)
	assert.Equal(t, 1, checker.params.RetryCount)
	assert.InDelta(t, 0.3, checker.params.Threshold, 0.0001)
}

func TestNewJevChecker_RejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*JevConfig)
		errMsg string
	}{
		{"NaN threshold", func(c *JevConfig) { c.Threshold = math.NaN() }, "finite"},
		{"inf threshold", func(c *JevConfig) { c.Threshold = math.Inf(1) }, "finite"},
		{"threshold above one", func(c *JevConfig) { c.Threshold = 1.5 }, "(0, 1]"},
		{"negative threshold", func(c *JevConfig) { c.Threshold = -0.2 }, "(0, 1]"},
		{"zero threshold", func(c *JevConfig) { c.Threshold = 0 }, "(0, 1]"},
		{"negative max symbols", func(c *JevConfig) { c.MaxSymbolsRequest = -1 }, "negative"},
		{"empty question", func(c *JevConfig) { c.Question = "" }, "question"},
		{"empty spam criteria", func(c *JevConfig) { c.CriteriaSpam = "" }, "spam criteria"},
		{"empty ham criteria", func(c *JevConfig) { c.CriteriaHam = "" }, "ham criteria"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validJevConfig()
			tt.mutate(&cfg)
			checker, err := newJevChecker(&mocks.HTTPClientMock{}, cfg)
			require.Error(t, err)
			assert.Nil(t, checker)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

func TestNewJevChecker_ZeroTakesDefaultInvalidRejected(t *testing.T) {
	cfg := validJevConfig()
	cfg.MaxSymbolsRequest, cfg.Model, cfg.APIBase = 0, "", ""
	checker, err := newJevChecker(&mocks.HTTPClientMock{}, cfg)
	require.NoError(t, err)
	assert.Equal(t, defaultJevMaxSymbolsRequest, checker.params.MaxSymbolsRequest)
	assert.Equal(t, defaultJevModel, checker.params.Model)
	assert.Equal(t, defaultJevAPIBase, checker.params.APIBase)

	cfg = validJevConfig()
	cfg.MaxSymbolsRequest = -5
	_, err = newJevChecker(&mocks.HTTPClientMock{}, cfg)
	require.Error(t, err)
}

func TestJevChecker_RequestShape(t *testing.T) {
	var gotURL, gotAuth, gotType string
	var gotBody []byte
	clientMock := &mocks.HTTPClientMock{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			gotURL, gotAuth = req.URL.String(), req.Header.Get("Authorization")
			gotType = req.Header.Get("Content-Type")
			gotBody, _ = io.ReadAll(req.Body)
			return jevRespBody(t, `{"model":"jev-1.13.0","answers":{"spam":{"type":"noul","noul":0.9}}}`), nil
		},
	}
	checker, err := newJevChecker(clientMock, validJevConfig())
	require.NoError(t, err)

	spam, resp := checker.check(context.Background(), "buy now", nil)
	assert.True(t, spam)
	assert.Equal(t, "jev", resp.Name)

	assert.Equal(t, defaultJevAPIBase, gotURL)
	assert.Equal(t, "Bearer test-token", gotAuth)
	assert.Equal(t, "application/json", gotType)

	var sent jevRequest
	require.NoError(t, json.Unmarshal(gotBody, &sent))
	assert.Equal(t, defaultJevModel, sent.Model)
	assert.Equal(t, map[string]string{"message": "buy now"}, sent.State)
	require.Len(t, sent.Questions, 1)
	q := sent.Questions[jevQuestionID]
	assert.Equal(t, "noul", q.Type)
	assert.Equal(t, "Is `message` spam?", q.Instructions)
	assert.Equal(t, map[string]string{"true": "promotes or advertises", "false": "ordinary conversation"}, q.Criteria)
}

func TestJevChecker_ThresholdBoundary(t *testing.T) {
	tests := []struct {
		name string
		noul float64
		spam bool
	}{
		{"below threshold", 0.29, false},
		{"at threshold", 0.30, true},
		{"above threshold", 0.31, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientMock := &mocks.HTTPClientMock{
				DoFunc: func(*http.Request) (*http.Response, error) {
					return jevRespBody(t, fmt.Sprintf(`{"answers":{"spam":{"type":"noul","noul":%v}}}`, tt.noul)), nil
				},
			}
			checker, err := newJevChecker(clientMock, validJevConfig())
			require.NoError(t, err)
			spam, _ := checker.check(context.Background(), "msg", nil)
			assert.Equal(t, tt.spam, spam)
		})
	}
}

func TestJevChecker_ConfidenceIsDirectionAware(t *testing.T) {
	tests := []struct {
		name           string
		noul           float64
		wantSpam       bool
		wantConfidence int
	}{
		{"spam verdict reports p", 0.87, true, 87},
		{"ham verdict reports 1-p", 0.02, false, 98},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker, err := newJevChecker(&mocks.HTTPClientMock{}, validJevConfig())
			require.NoError(t, err)
			got := checker.verdict(tt.noul)
			assert.Equal(t, tt.wantSpam, got.IsSpam)
			assert.Equal(t, tt.wantConfidence, got.Confidence)
			assert.Contains(t, got.Reason, "threshold 0.30")
		})
	}
}

func TestJevChecker_ErrorPaths(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		errMsg string
	}{
		{"non-2xx surfaces status and detail", http.StatusUnprocessableEntity,
			`{"detail":"bad question"}`, "422"},
		{"absent spam answer", http.StatusOK, `{"answers":{}}`, `no "spam" answer`},
		{"wrong answer type", http.StatusOK,
			`{"answers":{"spam":{"type":"choice","noul":0.5}}}`, "unexpected answer type"},
		{"noul above one", http.StatusOK,
			`{"answers":{"spam":{"type":"noul","noul":1.4}}}`, "out of range"},
		{"noul below zero", http.StatusOK,
			`{"answers":{"spam":{"type":"noul","noul":-0.1}}}`, "out of range"},
		{"malformed json", http.StatusOK, `{not json`, "can't unmarshal"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientMock := &mocks.HTTPClientMock{
				DoFunc: func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(tt.body))}, nil
				},
			}
			checker, err := newJevChecker(clientMock, validJevConfig())
			require.NoError(t, err)
			_, resp := checker.check(context.Background(), "msg", nil)
			require.Error(t, resp.Error)
			assert.Contains(t, resp.Error.Error(), tt.errMsg)
			assert.False(t, resp.Spam)
		})
	}
}

// pins the null-decodes-as-confident-ham defect: a plain float64 makes missing, null and 0 identical
func TestJevChecker_MissingNullAndZeroNoulAreDistinct(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
		wantRun bool
	}{
		{"missing noul key", `{"answers":{"spam":{"type":"noul"}}}`, "missing noul value", false},
		{"explicit null noul", `{"answers":{"spam":{"type":"noul","noul":null}}}`, "missing noul value", false},
		{"legitimate zero noul", `{"answers":{"spam":{"type":"noul","noul":0}}}`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clientMock := &mocks.HTTPClientMock{
				DoFunc: func(*http.Request) (*http.Response, error) { return jevRespBody(t, tt.body), nil },
			}
			checker, err := newJevChecker(clientMock, validJevConfig())
			require.NoError(t, err)
			spam, resp := checker.check(context.Background(), "msg", nil)
			assert.False(t, spam)
			if !tt.wantRun {
				require.Error(t, resp.Error)
				assert.Contains(t, resp.Error.Error(), tt.wantErr)
				return
			}
			require.NoError(t, resp.Error)
			assert.Contains(t, resp.Details, "confidence: 100%")
		})
	}
}

// runLLMProviderCheck cannot see an HTTP status, so no error is treated as permanent
func TestJevChecker_RetryConsumesAllAttempts(t *testing.T) {
	tests := []struct {
		name   string
		status int
	}{
		{"422 is retried like any other error", http.StatusUnprocessableEntity},
		{"429 is retried", http.StatusTooManyRequests},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calls atomic.Int32
			clientMock := &mocks.HTTPClientMock{
				DoFunc: func(*http.Request) (*http.Response, error) {
					calls.Add(1)
					return &http.Response{StatusCode: tt.status, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
				},
			}
			cfg := validJevConfig()
			cfg.RetryCount = 3
			checker, err := newJevChecker(clientMock, cfg)
			require.NoError(t, err)
			_, resp := checker.check(context.Background(), "msg", nil)
			require.Error(t, resp.Error)
			assert.Equal(t, int32(3), calls.Load())
		})
	}
}

func TestJevChecker_CancelledContextStopsEarly(t *testing.T) {
	var calls atomic.Int32
	clientMock := &mocks.HTTPClientMock{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			calls.Add(1)
			return nil, req.Context().Err()
		},
	}
	cfg := validJevConfig()
	cfg.RetryCount = 5
	checker, err := newJevChecker(clientMock, cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, resp := checker.check(ctx, "msg", nil)
	require.Error(t, resp.Error)
	assert.Equal(t, int32(1), calls.Load(), "canceled context must not consume the remaining attempts")
}

func TestJevChecker_RequestCarriesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var gotCtxErr error
	clientMock := &mocks.HTTPClientMock{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			cancel()
			gotCtxErr = req.Context().Err()
			return nil, req.Context().Err()
		},
	}
	checker, err := newJevChecker(clientMock, validJevConfig())
	require.NoError(t, err)
	_, resp := checker.check(ctx, "msg", nil)
	require.Error(t, resp.Error)
	assert.ErrorIs(t, gotCtxErr, context.Canceled, "request must carry the caller's context")
}

func TestJevChecker_TruncatesByRunes(t *testing.T) {
	var gotBody []byte
	clientMock := &mocks.HTTPClientMock{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			gotBody, _ = io.ReadAll(req.Body)
			return jevRespBody(t, `{"answers":{"spam":{"type":"noul","noul":0.1}}}`), nil
		},
	}
	cfg := validJevConfig()
	cfg.MaxSymbolsRequest = 5
	checker, err := newJevChecker(clientMock, cfg)
	require.NoError(t, err)

	_, resp := checker.check(context.Background(), "абвгдежзий", nil)
	require.NoError(t, resp.Error)

	var sent jevRequest
	require.NoError(t, json.Unmarshal(gotBody, &sent))
	assert.Equal(t, "абвгд", sent.State["message"], "truncation must cut runes, not bytes")
}

func TestJevChecker_NilClient(t *testing.T) {
	checker := &jevChecker{params: validJevConfig()}
	spam, resp := checker.check(context.Background(), "msg", nil)
	assert.False(t, spam)
	assert.Equal(t, spamcheck.Response{}, resp)
}

func TestJevChecker_TransportError(t *testing.T) {
	clientMock := &mocks.HTTPClientMock{
		DoFunc: func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("dial failed") },
	}
	checker, err := newJevChecker(clientMock, validJevConfig())
	require.NoError(t, err)
	_, resp := checker.check(context.Background(), "msg", nil)
	require.Error(t, resp.Error)
	assert.Contains(t, resp.Error.Error(), "dial failed")
}

func TestJevChecker_HistoryReachesRequest(t *testing.T) {
	var gotBody []byte
	clientMock := &mocks.HTTPClientMock{
		DoFunc: func(req *http.Request) (*http.Response, error) {
			gotBody, _ = io.ReadAll(req.Body)
			return jevRespBody(t, `{"answers":{"spam":{"type":"noul","noul":0.1}}}`), nil
		},
	}
	checker, err := newJevChecker(clientMock, validJevConfig())
	require.NoError(t, err)

	hist := []spamcheck.Request{{UserName: "user1", Msg: "earlier message"}}
	_, resp := checker.check(context.Background(), "current", hist)
	require.NoError(t, resp.Error)

	var sent jevRequest
	require.NoError(t, json.Unmarshal(gotBody, &sent))
	assert.Contains(t, sent.State["message"], "current")
	assert.Contains(t, sent.State["message"], "earlier message")
}

func TestJevChecker_BuildRequestNoTruncationWhenShort(t *testing.T) {
	checker, err := newJevChecker(&mocks.HTTPClientMock{}, validJevConfig())
	require.NoError(t, err)
	got := checker.buildRequest("short")
	assert.Equal(t, "short", got.State["message"])

	body, err := json.Marshal(got)
	require.NoError(t, err)
	assert.True(t, bytes.Contains(body, []byte(`"type":"noul"`)))
}
