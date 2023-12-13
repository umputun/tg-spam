package server

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	tbapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/server/mocks"
)

func TestSpamWeb_UnbanURL(t *testing.T) {
	tests := []struct {
		name   string
		url    string
		msg    string
		secret string
		want   string
	}{
		{"empty", "", "", "", "/unban?user=123&token=d68b50c4f0747630c33bc736bb3087b4c22f19dc645ec63b3bf90760c553e1ae"},
		{"localhost without msg", "http://localhost", "", "secret",
			"http://localhost/unban?user=123&token=71199ea8c011a49df546451e456ad10b0016566a53c4861bf849ec6b2ad2a0b7"},
		{"127.0.0.1 without msg", "http://127.0.0.1:8080", "", "secret",
			"http://127.0.0.1:8080/unban?user=123&token=71199ea8c011a49df546451e456ad10b0016566a53c4861bf849ec6b2ad2a0b7"},
		{"127.0.0.1 without msg, diff secret", "http://127.0.0.1:8080", "", "secret2",
			"http://127.0.0.1:8080/unban?user=123&token=5385a71e8d5b65ea03e3da10175d78028ae59efd58811004e907baf422019b2e"},
		{"127.0.0.1 with msg", "http://127.0.0.1:8080", "some message example", "secret",
			"http://127.0.0.1:8080/unban?user=123&token=" +
				"71199ea8c011a49df546451e456ad10b0016566a53c4861bf849ec6b2ad2a0b7&msg=c29tZSBtZXNzYWdlIGV4YW1wbGU="},
		{"127.0.0.1 with too long msg", "http://127.0.0.1:8080", "some long long message example", "secret",
			"http://127.0.0.1:8080/unban?user=123&token=" +
				"71199ea8c011a49df546451e456ad10b0016566a53c4861bf849ec6b2ad2a0b7"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := SpamWeb{Config: Config{URL: tt.url, Secret: tt.secret, MaxMsg: 30}}
			res := srv.UnbanURL(123, tt.msg)
			assert.Equal(t, tt.want, res)
		})
	}
}

func TestSpamWeb_Run(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	mockAPI := &mocks.TbAPIMock{
		GetChatFunc: func(config tbapi.ChatInfoConfig) (tbapi.Chat, error) {
			if config.ChatConfig.SuperGroupUsername == "xxx" {
				return tbapi.Chat{}, errors.New("not found")
			}
			if config.ChatConfig.SuperGroupUsername == "@group" {
				return tbapi.Chat{ID: 10}, nil
			}
			return tbapi.Chat{ID: 123}, nil
		},
		RequestFunc: func(c tbapi.Chattable) (*tbapi.APIResponse, error) {
			if c.(tbapi.UnbanChatMemberConfig).UserID == 666 {
				return nil, errors.New("failed")
			}
			return &tbapi.APIResponse{}, nil
		},
	}

	mockDetector := &mocks.DetectorMock{
		UpdateHamFunc: func(msg string) error {
			return nil
		},
	}

	srv, err := NewSpamWeb(mockAPI, mockDetector, Config{
		ListenAddr: ":9900",
		URL:        "http://localhost:9090",
		Secret:     "secret",
		TgGroup:    "group",
	})
	require.NoError(t, err)

	require.Equal(t, 1, len(mockAPI.GetChatCalls()))
	assert.Equal(t, "@group", mockAPI.GetChatCalls()[0].Config.ChatConfig.SuperGroupUsername)

	done := make(chan struct{})
	go func() {
		srv.Run(ctx)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond) // wait for server to start

	t.Run("ping", func(t *testing.T) {
		mockAPI.ResetCalls()
		resp, err := http.Get("http://localhost:9900/ping")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "text/plain", resp.Header.Get("Content-Type"))
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, "pong", string(body))
	})

	t.Run("unban forbidden, wrong token", func(t *testing.T) {
		mockAPI.ResetCalls()
		req, err := http.NewRequest("GET", "http://localhost:9900/unban?user=123&token=ssss", http.NoBody)
		require.NoError(t, err)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.Equal(t, 0, len(mockAPI.RequestCalls()))
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("body: %s", body)
		assert.Contains(t, string(body), "Error")
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
	})

	t.Run("unban failed, bad id", func(t *testing.T) {
		mockAPI.ResetCalls()
		req, err := http.NewRequest("GET",
			"http://localhost:9900/unban?user=xxx&token=71199ea8c011a49df546451e456ad10b0016566a53c4861bf849ec6b2ad2a0b7", http.NoBody)
		require.NoError(t, err)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Equal(t, 0, len(mockAPI.RequestCalls()))
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("body: %s", body)
		assert.Contains(t, string(body), "Error")
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
	})

	t.Run("unban failed, missing user parameter", func(t *testing.T) {
		mockAPI.ResetCalls()
		req, err := http.NewRequest("GET", "http://localhost:9900/unban?token=71199ea8c011a49df546451e456ad10b0016566a53c4861bf849ec6b2ad2a0b7", http.NoBody)
		require.NoError(t, err)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Equal(t, 0, len(mockAPI.RequestCalls()))
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("body: %s", body)
		assert.Contains(t, string(body), "Error")
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
	})

	t.Run("unban failed, unban request failed", func(t *testing.T) {
		mockAPI.ResetCalls()
		req, err := http.NewRequest("GET",
			"http://localhost:9900/unban?user=666&token=4eeb1bfa92a5c9418e8708953daaba267f86df63281da9480c53206d4cb2be32", http.NoBody)
		require.NoError(t, err)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		assert.Equal(t, 1, len(mockAPI.RequestCalls()))
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("body: %s", body)
		assert.Contains(t, string(body), "Error")
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))

	})

	t.Run("unban allowed, matched token", func(t *testing.T) {
		mockAPI.ResetCalls()
		mockDetector.ResetCalls()
		req, err := http.NewRequest("GET",
			"http://localhost:9900/unban?user=123&token=71199ea8c011a49df546451e456ad10b0016566a53c4861bf849ec6b2ad2a0b7", http.NoBody)
		require.NoError(t, err)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, 1, len(mockAPI.RequestCalls()))
		assert.Equal(t, int64(10), mockAPI.RequestCalls()[0].C.(tbapi.UnbanChatMemberConfig).ChatID)
		assert.Equal(t, int64(123), mockAPI.RequestCalls()[0].C.(tbapi.UnbanChatMemberConfig).UserID)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("body: %s", body)
		assert.Contains(t, string(body), "Success")
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
		assert.Equal(t, 0, len(mockDetector.UpdateHamCalls()), "no message, nothing to update")
	})

	t.Run("unban allowed with second attempt", func(t *testing.T) {
		mockAPI.ResetCalls()
		mockDetector.ResetCalls()
		req, err := http.NewRequest("GET",
			"http://localhost:9900/unban?user=1239&token=e2b5356cfe79210553b4a0bc89310ea5961dc76e86046b07c61e479c9835623c", http.NoBody)
		require.NoError(t, err)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, 1, len(mockAPI.RequestCalls()))
		assert.Equal(t, int64(10), mockAPI.RequestCalls()[0].C.(tbapi.UnbanChatMemberConfig).ChatID)
		assert.Equal(t, int64(1239), mockAPI.RequestCalls()[0].C.(tbapi.UnbanChatMemberConfig).UserID)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("body: %s", body)
		assert.Contains(t, string(body), "Success")
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
		assert.Equal(t, 0, len(mockDetector.UpdateHamCalls()), "no message, nothing to update")

		// second attempt
		resp, err = client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		body, err = io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("body: %s", body)
		assert.Contains(t, string(body), "user 1239 already unbanned")

	})

	t.Run("unban allowed with second attempt, but different msg", func(t *testing.T) {
		mockAPI.ResetCalls()
		mockDetector.ResetCalls()
		req, err := http.NewRequest("GET",
			"http://localhost:9900/unban?user=1239&token=e2b5356cfe79210553b4a0bc89310ea5961dc76e86046b07c61e479c9835623c&msg=123", http.NoBody)
		require.NoError(t, err)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, 1, len(mockAPI.RequestCalls()))
		assert.Equal(t, int64(10), mockAPI.RequestCalls()[0].C.(tbapi.UnbanChatMemberConfig).ChatID)
		assert.Equal(t, int64(1239), mockAPI.RequestCalls()[0].C.(tbapi.UnbanChatMemberConfig).UserID)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("body: %s", body)
		assert.Contains(t, string(body), "Success")
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
		assert.Equal(t, 0, len(mockDetector.UpdateHamCalls()), "no message, nothing to update")

		// second attempt
		resp, err = client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		body, err = io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("body: %s", body)
		assert.Contains(t, string(body), "user 1239 already unbanned")

	})

	t.Run("unban allowed, matched token with msg", func(t *testing.T) {
		mockAPI.ResetCalls()
		mockDetector.ResetCalls()
		req, err := http.NewRequest("GET",
			"http://localhost:9900/unban?user=1234&token=aa132fb6e42a7571933f959df3ce014dc89ecc60c085327cf27cfae835936d0e&msg=VGhpcyBpcyBhIHRlc3Qgc3RyaW5n", http.NoBody)
		require.NoError(t, err)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, 1, len(mockAPI.RequestCalls()))
		assert.Equal(t, int64(10), mockAPI.RequestCalls()[0].C.(tbapi.UnbanChatMemberConfig).ChatID)
		assert.Equal(t, int64(1234), mockAPI.RequestCalls()[0].C.(tbapi.UnbanChatMemberConfig).UserID)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("body: %s", body)
		assert.Contains(t, string(body), "Success")
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(mockDetector.UpdateHamCalls()), "message, update ham")
	})

	t.Run("unban allowed, matched token with bad msg", func(t *testing.T) {
		mockAPI.ResetCalls()
		mockDetector.ResetCalls()
		req, err := http.NewRequest("GET",
			"http://localhost:9900/unban?user=12345&token=68120cd2bf2462f939f5374753fe3f7aff68e6ea2321d448b1a0fc74055af209&msg=1-xyz", http.NoBody)
		require.NoError(t, err)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, 1, len(mockAPI.RequestCalls()))
		assert.Equal(t, int64(10), mockAPI.RequestCalls()[0].C.(tbapi.UnbanChatMemberConfig).ChatID)
		assert.Equal(t, int64(12345), mockAPI.RequestCalls()[0].C.(tbapi.UnbanChatMemberConfig).UserID)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("body: %s", body)
		assert.Contains(t, string(body), "Success")
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
		assert.Equal(t, 0, len(mockDetector.UpdateHamCalls()), "invalid message, update ham")
	})

	t.Run("unban failed, missing token parameter", func(t *testing.T) {
		mockAPI.ResetCalls()
		req, err := http.NewRequest("GET", "http://localhost:9900/unban?user=123", http.NoBody)
		require.NoError(t, err)
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		assert.Equal(t, 0, len(mockAPI.RequestCalls()))
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		t.Logf("body: %s", body)
		assert.Contains(t, string(body), "Error")
		assert.Equal(t, "text/html", resp.Header.Get("Content-Type"))
	})

	<-done
}

func TestSpamWeb_CompressAndDecompressString(t *testing.T) {
	sw := SpamWeb{}

	// Test cases
	tests := []struct {
		name        string
		input       string
		max         int
		expectError bool
		compressed  bool
	}{
		{
			name:        "normal string",
			input:       "This is a test string",
			max:         100,
			expectError: false,
			compressed:  false,
		},
		{
			name: "longer string",
			input: "CompressString compresses the input string, encodes it in base64 for URL safety,\n" +
				" and checks if the compressed size is within the specified maximum limit." +
				" If compression is ineffective (resulting in a larger string),\n" +
				" it returns the base64-encoded original string instead.",
			max:         600,
			expectError: false,
			compressed:  true,
		},
		{
			name:        "empty string",
			input:       "",
			max:         100,
			expectError: false,
			compressed:  false,
		},
		{
			name:        "too long string",
			max:         10,
			input:       "Long string 1234567890",
			expectError: true,
			compressed:  false,
		},
		{
			name:        "unicode string",
			input:       "Hello, 世界",
			max:         100,
			expectError: false,
			compressed:  false,
		},
		{
			name:        "string with special characters",
			input:       "!@#$%^&*()",
			max:         100,
			expectError: false,
			compressed:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			compressed, err := sw.compressString(tc.input, tc.max)
			if tc.expectError {
				require.Error(t, err)
				return
			}

			t.Logf("input: %s, compressed: %s", tc.input, compressed)
			assert.NoError(t, err)
			if len(tc.input) > 2 {
				assert.Equal(t, tc.compressed, compressed[:2] == compressedPrefix)
			}
			decompressed, err := sw.decompressString(compressed)
			assert.NoError(t, err)
			assert.Equal(t, tc.input, decompressed)
		})
	}
}

func TestUnbanKey(t *testing.T) {
	s := SpamWeb{}

	t.Run("GeneratesCorrectKey", func(t *testing.T) {
		id := int64(123)
		msg := "test message"
		expectedKey := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d::%s", id, msg))))

		key := s.unbanKey(id, msg)

		assert.Equal(t, expectedKey, key)
	})

	t.Run("GeneratesDifferentKeysForDifferentIDs", func(t *testing.T) {
		id1 := int64(123)
		id2 := int64(456)
		msg := "test message"

		key1 := s.unbanKey(id1, msg)
		key2 := s.unbanKey(id2, msg)

		assert.NotEqual(t, key1, key2)
	})

	t.Run("GeneratesDifferentKeysForDifferentMessages", func(t *testing.T) {
		id := int64(123)
		msg1 := "test message 1"
		msg2 := "test message 2"

		key1 := s.unbanKey(id, msg1)
		key2 := s.unbanKey(id, msg2)

		assert.NotEqual(t, key1, key2)
	})
}
