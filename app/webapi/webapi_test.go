package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/webapi/mocks"
	"github.com/umputun/tg-spam/lib"
)

func TestServer_Run(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer(Config{ListenAddr: ":9876", Version: "dev", SpamFilter: &mocks.DetectorMock{}})
	done := make(chan struct{})
	go func() {
		err := srv.Run(ctx)
		assert.NoError(t, err)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost:9876/ping")
	assert.NoError(t, err)
	t.Log(resp)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	assert.Equal(t, "pong", string(body))

	assert.Contains(t, resp.Header.Get("App-Name"), "tg-spam")
	assert.Contains(t, resp.Header.Get("App-Version"), "dev")

	cancel()
	<-done
}

func TestServer_RunAuth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mockDetector := &mocks.DetectorMock{
		CheckFunc: func(msg string, userID string) (bool, []lib.CheckResult) {
			return false, []lib.CheckResult{{Details: "not spam"}}
		},
	}

	srv := NewServer(Config{ListenAddr: ":9877", Version: "dev", SpamFilter: mockDetector, AuthPasswd: "test"})
	done := make(chan struct{})
	go func() {
		err := srv.Run(ctx)
		assert.NoError(t, err)
		close(done)
	}()
	time.Sleep(100 * time.Millisecond)

	t.Run("ping", func(t *testing.T) {
		resp, err := http.Get("http://localhost:9877/ping")
		assert.NoError(t, err)
		t.Log(resp)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode) // no auth on ping
	})

	t.Run("check unauthorized, no basic auth", func(t *testing.T) {
		resp, err := http.Get("http://localhost:9877/check")
		assert.NoError(t, err)
		t.Log(resp)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("check authorized", func(t *testing.T) {
		reqBody, err := json.Marshal(map[string]string{
			"msg":     "spam example",
			"user_id": "user123",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "http://localhost:9877/check", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)
		req.SetBasicAuth("tg-spam", "test")
		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		t.Log(resp)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("check forbidden, wrong basic auth", func(t *testing.T) {
		reqBody, err := json.Marshal(map[string]string{
			"msg":     "spam example",
			"user_id": "user123",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "http://localhost:9877/check", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)
		req.SetBasicAuth("tg-spam", "bad")
		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		t.Log(resp)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
	cancel()
	<-done
}

func TestServer_routes(t *testing.T) {
	mockDetector := &mocks.DetectorMock{
		CheckFunc: func(msg string, userID string) (bool, []lib.CheckResult) {
			return false, []lib.CheckResult{{Details: "not spam"}}
		},
		UpdateSpamFunc: func(msg string) error { return nil },
		UpdateHamFunc:  func(msg string) error { return nil },
		AddApprovedUsersFunc: func(ids ...string) {
			if len(ids) == 0 {
				panic("no ids")
			}
		},
		RemoveApprovedUsersFunc: func(ids ...string) {
			if len(ids) == 0 {
				panic("no ids")
			}
		},
		ApprovedUsersFunc: func() []string {
			return []string{"user1", "user2"}
		},
	}
	server := NewServer(Config{SpamFilter: mockDetector})
	ts := httptest.NewServer(server.routes(chi.NewRouter()))
	defer ts.Close()

	t.Run("check", func(t *testing.T) {
		mockDetector.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg":     "spam example",
			"user_id": "user123",
		})
		require.NoError(t, err)
		resp, err := http.Post(ts.URL+"/check", "application/json", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(mockDetector.CheckCalls()))
		assert.Equal(t, "spam example", mockDetector.CheckCalls()[0].Msg)
		assert.Equal(t, "user123", mockDetector.CheckCalls()[0].UserID)
	})

	t.Run("update spam", func(t *testing.T) {
		mockDetector.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg": "test message",
		})
		require.NoError(t, err)
		resp, err := http.Post(ts.URL+"/update/spam", "application/json", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(mockDetector.UpdateSpamCalls()))
		assert.Equal(t, "test message", mockDetector.UpdateSpamCalls()[0].Msg)
	})

	t.Run("update ham", func(t *testing.T) {
		mockDetector.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg": "test message",
		})
		require.NoError(t, err)
		resp, err := http.Post(ts.URL+"/update/ham", "application/json", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(mockDetector.UpdateHamCalls()))
		assert.Equal(t, "test message", mockDetector.UpdateHamCalls()[0].Msg)
	})

	t.Run("add user", func(t *testing.T) {
		mockDetector.ResetCalls()
		reqBody, err := json.Marshal(map[string][]string{
			"user_ids": {"user1", "user2"},
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", ts.URL+"/users", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(mockDetector.AddApprovedUsersCalls()))
		assert.Equal(t, []string{"user1", "user2"}, mockDetector.AddApprovedUsersCalls()[0].Ids)
	})

	t.Run("remove user", func(t *testing.T) {
		mockDetector.ResetCalls()
		reqBody, err := json.Marshal(map[string][]string{
			"user_ids": {"user1", "user2"},
		})
		require.NoError(t, err)
		req, err := http.NewRequest("DELETE", ts.URL+"/users", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(mockDetector.RemoveApprovedUsersCalls()))
		assert.Equal(t, []string{"user1", "user2"}, mockDetector.RemoveApprovedUsersCalls()[0].Ids)
	})

	t.Run("get users", func(t *testing.T) {
		mockDetector.ResetCalls()
		resp, err := http.Get(ts.URL + "/users")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(mockDetector.ApprovedUsersCalls()))
		respBody, err := io.ReadAll(resp.Body)
		assert.NoError(t, err)
		assert.Equal(t, `{"user_ids":["user1","user2"]}`+"\n", string(respBody))
	})
}

func TestServer_checkHandler(t *testing.T) {

	mockDetector := &mocks.DetectorMock{
		CheckFunc: func(msg string, userID string) (bool, []lib.CheckResult) {
			if msg == "spam example" {
				return true, []lib.CheckResult{{Spam: true, Name: "test", Details: "this was spam"}}
			}
			return false, []lib.CheckResult{{Details: "not spam"}}
		},
	}
	server := NewServer(Config{
		SpamFilter: mockDetector,
		Version:    "1.0",
	})

	t.Run("spam", func(t *testing.T) {
		reqBody, err := json.Marshal(map[string]string{
			"msg":     "spam example",
			"user_id": "user123",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "/check", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.checkHandler)

		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")

		var response struct {
			Spam   bool              `json:"spam"`
			Checks []lib.CheckResult `json:"checks"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		assert.NoError(t, err, "error unmarshalling response")
		assert.True(t, response.Spam, "expected spam")
		assert.Equal(t, "test", response.Checks[0].Name, "unexpected check name")
		assert.Equal(t, "this was spam", response.Checks[0].Details, "unexpected check result")
	})

	t.Run("not spam", func(t *testing.T) {
		reqBody, err := json.Marshal(map[string]string{
			"msg":     "not spam example",
			"user_id": "user123",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "/check", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.checkHandler)

		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")

		var response struct {
			Spam   bool              `json:"spam"`
			Checks []lib.CheckResult `json:"checks"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		assert.NoError(t, err, "error unmarshalling response")
		assert.False(t, response.Spam, "expected not spam")
		assert.Equal(t, "not spam", response.Checks[0].Details, "unexpected check result")
	})

	t.Run("bad request", func(t *testing.T) {
		reqBody := []byte("bad request")
		req, err := http.NewRequest("POST", "/check", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)
		req.Body.Close()

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.checkHandler)

		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code, "handler returned wrong status code")
	})

}

func TestServer_updateHandler(t *testing.T) {
	mockDetector := &mocks.DetectorMock{
		UpdateHamFunc: func(msg string) error {
			if msg == "error" {
				return assert.AnError
			}
			return nil
		},
		UpdateSpamFunc: func(msg string) error {
			if msg == "error" {
				return assert.AnError
			}
			return nil
		},
	}
	server := NewServer(Config{SpamFilter: mockDetector})

	t.Run("successful update ham", func(t *testing.T) {
		mockDetector.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg": "test message",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "/update", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.updateSampleHandler(mockDetector.UpdateHam))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")
		var response struct {
			Updated bool   `json:"updated"`
			Msg     string `json:"msg"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.True(t, response.Updated)
		assert.Equal(t, "test message", response.Msg)
		assert.Equal(t, 1, len(mockDetector.UpdateHamCalls()))
		assert.Equal(t, "test message", mockDetector.UpdateHamCalls()[0].Msg)
	})

	t.Run("update ham with error", func(t *testing.T) {
		mockDetector.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg": "error",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "/update", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.updateSampleHandler(mockDetector.UpdateHam))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code, "handler returned wrong status code")
		var response struct {
			Err     string `json:"error"`
			Details string `json:"details"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "can't update samples", response.Err)
		assert.Equal(t, "assert.AnError general error for testing", response.Details)
		assert.Equal(t, 1, len(mockDetector.UpdateHamCalls()))
		assert.Equal(t, "error", mockDetector.UpdateHamCalls()[0].Msg)
	})

	t.Run("bad request", func(t *testing.T) {
		mockDetector.ResetCalls()
		reqBody := []byte("bad request")
		req, err := http.NewRequest("POST", "/update", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.updateSampleHandler(mockDetector.UpdateHam))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code, "handler returned wrong status code")
	})
}

func TestServer_updateApprovedUsersHandler(t *testing.T) {
	mockDetector := &mocks.DetectorMock{
		AddApprovedUsersFunc: func(ids ...string) {
			if len(ids) == 0 {
				panic("no ids")
			}
		},
	}
	server := NewServer(Config{SpamFilter: mockDetector})

	t.Run("successful update", func(t *testing.T) {
		mockDetector.ResetCalls()
		reqBody, err := json.Marshal(map[string][]string{
			"user_ids": {"user1", "user2"},
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "/users/add", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.updateApprovedUsersHandler(mockDetector.AddApprovedUsers))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")
		var response struct {
			Updated bool `json:"updated"`
			Count   int  `json:"count"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.True(t, response.Updated)
		assert.Equal(t, 2, response.Count)
		assert.Equal(t, 1, len(mockDetector.AddApprovedUsersCalls()))
		assert.Equal(t, []string{"user1", "user2"}, mockDetector.AddApprovedUsersCalls()[0].Ids)
	})

	t.Run("bad request", func(t *testing.T) {
		mockDetector.ResetCalls()
		reqBody := []byte("bad request")
		req, err := http.NewRequest("POST", "/users/add", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.updateApprovedUsersHandler(mockDetector.AddApprovedUsers))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code, "handler returned wrong status code")
	})
}
