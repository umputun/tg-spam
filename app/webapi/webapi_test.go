package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-pkgz/rest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/app/webapi/mocks"
	"github.com/umputun/tg-spam/lib/approved"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestServer_Run(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := NewServer(Config{ListenAddr: ":9876", Version: "dev", Detector: &mocks.DetectorMock{},
		SpamFilter: &mocks.SpamFilterMock{}, AuthPasswd: "test"})
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
		CheckFunc: func(req spamcheck.Request) (bool, []spamcheck.Response) {
			return false, []spamcheck.Response{{Details: "not spam"}}
		},
	}
	mockSpamFilter := &mocks.SpamFilterMock{}

	hashedPassword, err := rest.GenerateBcryptHash("test")
	require.NoError(t, err)
	t.Logf("hashed password: %s", string(hashedPassword))

	tests := []struct {
		name      string
		srv       *Server
		port      string
		authType  string
		password  string
		useHashed bool
	}{
		{
			name: "plain password auth",
			srv: NewServer(Config{
				ListenAddr: ":9877",
				Version:    "dev",
				Detector:   mockDetector,
				SpamFilter: mockSpamFilter,
				AuthPasswd: "test",
			}),
			port:     "9877",
			authType: "plain",
			password: "test",
		},
		{
			name: "bcrypt hash auth",
			srv: NewServer(Config{
				ListenAddr: ":9878",
				Version:    "dev",
				Detector:   mockDetector,
				SpamFilter: mockSpamFilter,
				AuthHash:   string(hashedPassword),
			}),
			port:      "9878",
			authType:  "hash",
			password:  "test",
			useHashed: true,
		},
	}

	var doneChannels []chan struct{}
	for _, tc := range tests {
		done := make(chan struct{})
		doneChannels = append(doneChannels, done)
		t.Run(tc.authType, func(t *testing.T) {
			go func() {
				err := tc.srv.Run(ctx)
				assert.NoError(t, err)
				close(done)
			}()

			// Wait for server to be ready
			require.Eventually(t, func() bool {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%s/ping", tc.port))
				if err != nil {
					return false
				}
				defer resp.Body.Close()
				return resp.StatusCode == http.StatusOK
			}, time.Second*2, time.Millisecond*50, "server did not start")

			t.Run("ping", func(t *testing.T) {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%s/ping", tc.port))
				assert.NoError(t, err)
				t.Log(resp)
				defer resp.Body.Close()
				assert.Equal(t, http.StatusOK, resp.StatusCode)
			})

			t.Run("check unauthorized, no basic auth", func(t *testing.T) {
				resp, err := http.Get(fmt.Sprintf("http://localhost:%s/check", tc.port))
				assert.NoError(t, err)
				t.Log(resp)
				defer resp.Body.Close()
				assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
				if tc.useHashed {
					assert.Equal(t, `Basic realm="restricted", charset="UTF-8"`, resp.Header.Get("WWW-Authenticate"))
				}
			})

			t.Run("check authorized", func(t *testing.T) {
				reqBody, err := json.Marshal(map[string]string{
					"msg":     "spam example",
					"user_id": "user123",
				})
				require.NoError(t, err)
				req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%s/check", tc.port), bytes.NewBuffer(reqBody))
				assert.NoError(t, err)
				req.SetBasicAuth("tg-spam", tc.password)
				resp, err := http.DefaultClient.Do(req)
				assert.NoError(t, err)
				t.Log(resp)
				defer resp.Body.Close()
				assert.Equal(t, http.StatusOK, resp.StatusCode)
			})

			t.Run("wrong basic auth", func(t *testing.T) {
				reqBody, err := json.Marshal(map[string]string{
					"msg":     "spam example",
					"user_id": "user123",
				})
				require.NoError(t, err)
				req, err := http.NewRequest("POST", fmt.Sprintf("http://localhost:%s/check", tc.port), bytes.NewBuffer(reqBody))
				assert.NoError(t, err)
				req.SetBasicAuth("tg-spam", "bad")
				resp, err := http.DefaultClient.Do(req)
				assert.NoError(t, err)
				t.Log(resp)
				defer resp.Body.Close()
				assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
				if tc.useHashed {
					assert.Equal(t, `Basic realm="restricted", charset="UTF-8"`, resp.Header.Get("WWW-Authenticate"))
				}
			})
		})
	}
	cancel()
	for _, done := range doneChannels {
		<-done
	}
}

func TestServer_routes(t *testing.T) {
	detectorMock := &mocks.DetectorMock{
		CheckFunc: func(req spamcheck.Request) (bool, []spamcheck.Response) {
			return false, []spamcheck.Response{{Details: "not spam"}}
		},
		ApprovedUsersFunc: func() []approved.UserInfo {
			return []approved.UserInfo{{UserID: "user1", UserName: "name1"}, {UserID: "user2", UserName: "name2"}}
		},
		AddApprovedUserFunc: func(user approved.UserInfo) error {
			return nil
		},
		RemoveApprovedUserFunc: func(id string) error {
			return nil
		},
	}
	spamFilterMock := &mocks.SpamFilterMock{
		UpdateHamFunc:               func(msg string) error { return nil },
		UpdateSpamFunc:              func(msg string) error { return nil },
		RemoveDynamicSpamSampleFunc: func(sample string) (int, error) { return 1, nil },
		RemoveDynamicHamSampleFunc:  func(sample string) (int, error) { return 1, nil },
	}
	locatorMock := &mocks.LocatorMock{
		UserIDByNameFunc: func(userName string) int64 {
			if userName == "user1" {
				return 12345
			}
			return 0
		},
	}

	server := NewServer(Config{Detector: detectorMock, SpamFilter: spamFilterMock, Locator: locatorMock})
	ts := httptest.NewServer(server.routes(chi.NewRouter()))
	defer ts.Close()

	t.Run("check", func(t *testing.T) {
		detectorMock.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg":     "spam example",
			"user_id": "user123",
		})
		require.NoError(t, err)
		resp, err := http.Post(ts.URL+"/check", "application/json", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(detectorMock.CheckCalls()))
		assert.Equal(t, "spam example", detectorMock.CheckCalls()[0].Req.Msg)
		assert.Equal(t, "user123", detectorMock.CheckCalls()[0].Req.UserID)
	})

	t.Run("update spam", func(t *testing.T) {
		detectorMock.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg": "test message",
		})
		require.NoError(t, err)
		resp, err := http.Post(ts.URL+"/update/spam", "application/json", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(spamFilterMock.UpdateSpamCalls()))
		assert.Equal(t, "test message", spamFilterMock.UpdateSpamCalls()[0].Msg)
	})

	t.Run("update ham", func(t *testing.T) {
		detectorMock.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg": "test message",
		})
		require.NoError(t, err)
		resp, err := http.Post(ts.URL+"/update/ham", "application/json", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(spamFilterMock.UpdateHamCalls()))
		assert.Equal(t, "test message", spamFilterMock.UpdateHamCalls()[0].Msg)
	})

	t.Run("delete ham sample", func(t *testing.T) {
		detectorMock.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg": "test message",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", ts.URL+"/delete/ham", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(spamFilterMock.RemoveDynamicHamSampleCalls()))
		assert.Equal(t, "test message", spamFilterMock.RemoveDynamicHamSampleCalls()[0].Sample)
	})

	t.Run("delete spam sample", func(t *testing.T) {
		detectorMock.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg": "test message",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", ts.URL+"/delete/spam", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(spamFilterMock.RemoveDynamicSpamSampleCalls()))
		assert.Equal(t, "test message", spamFilterMock.RemoveDynamicSpamSampleCalls()[0].Sample)
	})

	t.Run("add user", func(t *testing.T) {
		detectorMock.ResetCalls()
		locatorMock.ResetCalls()

		req, err := http.NewRequest("POST", ts.URL+"/users/add", bytes.NewBuffer([]byte(`{"user_id" : "123", "user_name":"user1"}`)))
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(detectorMock.AddApprovedUserCalls()))
		assert.Equal(t, "123", detectorMock.AddApprovedUserCalls()[0].User.UserID)
	})

	t.Run("add user without id", func(t *testing.T) {
		detectorMock.ResetCalls()
		locatorMock.ResetCalls()
		req, err := http.NewRequest("POST", ts.URL+"/users/add", bytes.NewBuffer([]byte(`{"user_name" : "user1"}`)))
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(detectorMock.AddApprovedUserCalls()))
		assert.Equal(t, "12345", detectorMock.AddApprovedUserCalls()[0].User.UserID)
		assert.Equal(t, 1, len(locatorMock.UserIDByNameCalls()))
		assert.Equal(t, "user1", locatorMock.UserIDByNameCalls()[0].UserName)
	})

	t.Run("add user by name, not found", func(t *testing.T) {
		detectorMock.ResetCalls()
		locatorMock.ResetCalls()
		req, err := http.NewRequest("POST", ts.URL+"/users/add", bytes.NewBuffer([]byte(`{"user_name" : "user2"}`)))
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Equal(t, 1, len(locatorMock.UserIDByNameCalls()))
		assert.Equal(t, "user2", locatorMock.UserIDByNameCalls()[0].UserName)
	})

	t.Run("remove user by id", func(t *testing.T) {
		detectorMock.ResetCalls()
		locatorMock.ResetCalls()

		req, err := http.NewRequest("POST", ts.URL+"/users/delete", bytes.NewBuffer([]byte(`{"user_id" : "123"}`)))
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(detectorMock.RemoveApprovedUserCalls()))
		assert.Equal(t, "123", detectorMock.RemoveApprovedUserCalls()[0].ID)
		assert.Equal(t, 0, len(locatorMock.UserIDByNameCalls()))
	})

	t.Run("remove user by name", func(t *testing.T) {
		detectorMock.ResetCalls()
		locatorMock.ResetCalls()
		req, err := http.NewRequest("POST", ts.URL+"/users/delete", bytes.NewBuffer([]byte(`{"user_name" : "user1"}`)))
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(detectorMock.RemoveApprovedUserCalls()))
		assert.Equal(t, "12345", detectorMock.RemoveApprovedUserCalls()[0].ID)
		assert.Equal(t, 1, len(locatorMock.UserIDByNameCalls()))
		assert.Equal(t, "user1", locatorMock.UserIDByNameCalls()[0].UserName)
	})

	t.Run("get approved users", func(t *testing.T) {
		detectorMock.ResetCalls()
		resp, err := http.Get(ts.URL + "/users")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(detectorMock.ApprovedUsersCalls()))
		respBody, err := io.ReadAll(resp.Body)
		assert.NoError(t, err)
		assert.Equal(t, `{"user_ids":[{"user_id":"user1","user_name":"name1","timestamp":"0001-01-01T00:00:00Z"},{"user_id":"user2","user_name":"name2","timestamp":"0001-01-01T00:00:00Z"}]}`+"\n", string(respBody))
	})

	t.Run("get settings", func(t *testing.T) {
		server.Settings.MinMsgLen = 10
		resp, err := http.Get(ts.URL + "/settings")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))

		res := Settings{}
		err = json.NewDecoder(resp.Body).Decode(&res)
		assert.NoError(t, err)
		assert.Equal(t, server.Settings, res)
	})
}

func TestServer_checkHandler(t *testing.T) {
	mockDetector := &mocks.DetectorMock{
		CheckFunc: func(req spamcheck.Request) (bool, []spamcheck.Response) {
			if req.Msg == "spam example" {
				return true, []spamcheck.Response{{Spam: true, Name: "test", Details: "this was spam"}}
			}
			return false, []spamcheck.Response{{Details: "not spam"}}
		},
	}
	server := NewServer(Config{
		Detector: mockDetector,
		Version:  "1.0",
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
			Spam   bool                 `json:"spam"`
			Checks []spamcheck.Response `json:"checks"`
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
			Spam   bool                 `json:"spam"`
			Checks []spamcheck.Response `json:"checks"`
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

func TestServer_updateSampleHandler(t *testing.T) {
	spamFilterMock := &mocks.SpamFilterMock{
		UpdateSpamFunc: func(msg string) error {
			if msg == "error" {
				return assert.AnError
			}
			return nil
		},
		UpdateHamFunc: func(msg string) error {
			if msg == "error" {
				return assert.AnError
			}
			return nil
		},
	}

	server := NewServer(Config{SpamFilter: spamFilterMock})

	t.Run("successful update ham", func(t *testing.T) {
		spamFilterMock.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg": "test message",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "/update", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.updateSampleHandler(spamFilterMock.UpdateHam))
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
		assert.Equal(t, 1, len(spamFilterMock.UpdateHamCalls()))
		assert.Equal(t, "test message", spamFilterMock.UpdateHamCalls()[0].Msg)
	})

	t.Run("update ham with error", func(t *testing.T) {
		spamFilterMock.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg": "error",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "/update", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.updateSampleHandler(spamFilterMock.UpdateHam))
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
		assert.Equal(t, 1, len(spamFilterMock.UpdateHamCalls()))
		assert.Equal(t, "error", spamFilterMock.UpdateHamCalls()[0].Msg)
	})

	t.Run("bad request", func(t *testing.T) {
		spamFilterMock.ResetCalls()
		reqBody := []byte("bad request")
		req, err := http.NewRequest("POST", "/update", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.updateSampleHandler(spamFilterMock.UpdateHam))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code, "handler returned wrong status code")
	})
}

func TestServer_deleteSampleHandler(t *testing.T) {
	spamFilterMock := &mocks.SpamFilterMock{
		RemoveDynamicHamSampleFunc: func(sample string) (int, error) { return 1, nil },
		DynamicSamplesFunc: func() ([]string, []string, error) {
			return []string{"spam1", "spam2"}, []string{"ham1", "ham2"}, nil
		},
	}
	server := NewServer(Config{SpamFilter: spamFilterMock})

	t.Run("successful delete ham sample", func(t *testing.T) {
		spamFilterMock.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg": "test message",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "/delete/ham", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.deleteSampleHandler(spamFilterMock.RemoveDynamicHamSample))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")
		var response struct {
			Deleted bool   `json:"deleted"`
			Msg     string `json:"msg"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.True(t, response.Deleted)
		assert.Equal(t, "test message", response.Msg)
		require.Equal(t, 1, len(spamFilterMock.RemoveDynamicHamSampleCalls()))
		assert.Equal(t, "test message", spamFilterMock.RemoveDynamicHamSampleCalls()[0].Sample)
	})

	t.Run("delete ham sample from htmx", func(t *testing.T) {
		spamFilterMock.ResetCalls()
		req, err := http.NewRequest("POST", "/delete/ham", http.NoBody)
		require.NoError(t, err)
		req.Header.Add("HX-Request", "true") // Simulating HTMX request

		// set form htmx request, msg in r.FormValue("msg")
		req.Form = url.Values{}
		req.Form.Set("msg", "test message")

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.deleteSampleHandler(spamFilterMock.RemoveDynamicHamSample))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")
		body := rr.Body.String()
		t.Log(body)
		assert.Contains(t, body, "Spam Samples (2)", "response should contain spam samples")
		assert.Contains(t, body, "Ham Samples (2)", "response should contain ham samples")
		require.Equal(t, 1, len(spamFilterMock.RemoveDynamicHamSampleCalls()))
		assert.Equal(t, "test message", spamFilterMock.RemoveDynamicHamSampleCalls()[0].Sample)
	})

	t.Run("delete ham sample with error", func(t *testing.T) {
		spamFilterMock.RemoveDynamicHamSampleFunc = func(sample string) (int, error) { return 0, assert.AnError }
		spamFilterMock.ResetCalls()
		reqBody, err := json.Marshal(map[string]string{
			"msg": "test message",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "/delete/ham", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.deleteSampleHandler(spamFilterMock.RemoveDynamicHamSample))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code, "handler returned wrong status code")
	})
}

func TestServer_updateApprovedUsersHandler(t *testing.T) {
	mockDetector := &mocks.DetectorMock{
		AddApprovedUserFunc: func(user approved.UserInfo) error {
			if user.UserID == "error" {
				return assert.AnError
			}
			return nil
		},
		ApprovedUsersFunc: func() []approved.UserInfo {
			return []approved.UserInfo{{UserID: "12345", UserName: "user1"}, {UserID: "67890", UserName: "user2"}}
		},
	}
	locatorMock := &mocks.LocatorMock{
		UserIDByNameFunc: func(userName string) int64 {
			if userName == "user1" {
				return 12345
			}
			return 0
		},
	}

	server := NewServer(Config{Detector: mockDetector, Locator: locatorMock})

	t.Run("successful update by name", func(t *testing.T) {
		mockDetector.ResetCalls()
		locatorMock.ResetCalls()

		req, err := http.NewRequest("POST", "/users/add", bytes.NewBuffer([]byte(`{"user_name" : "user1"}`)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.updateApprovedUsersHandler(server.Detector.AddApprovedUser))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")
		var response struct {
			Updated  bool   `json:"updated"`
			UserID   string `json:"user_id"`
			UserName string `json:"user_name"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.True(t, response.Updated)
		assert.Equal(t, "12345", response.UserID)
		assert.Equal(t, "user1", response.UserName)
		assert.Equal(t, 1, len(mockDetector.AddApprovedUserCalls()))
		assert.Equal(t, "12345", mockDetector.AddApprovedUserCalls()[0].User.UserID)
		assert.Equal(t, 1, len(locatorMock.UserIDByNameCalls()))
		assert.Equal(t, "user1", locatorMock.UserIDByNameCalls()[0].UserName)
	})

	t.Run("successful update from htmx", func(t *testing.T) {
		mockDetector.ResetCalls()
		locatorMock.ResetCalls()

		req, err := http.NewRequest("POST", "/users/add", http.NoBody)
		require.NoError(t, err)
		req.Header.Add("HX-Request", "true") // Simulating HTMX request

		req.Form = url.Values{}
		req.Form.Set("user_id", "123")
		req.Form.Set("user_name", "user1")

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.updateApprovedUsersHandler(server.Detector.AddApprovedUser))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")
		body := rr.Body.String()
		t.Log(body)
		assert.Contains(t, body, "<h4>Approved Users (2)</h4>", "response should contain approved users header")
		assert.Contains(t, body, "user1")
		assert.Contains(t, body, "user2")

		assert.Equal(t, 1, len(mockDetector.AddApprovedUserCalls()))
		assert.Equal(t, "123", mockDetector.AddApprovedUserCalls()[0].User.UserID)
		assert.Equal(t, 0, len(locatorMock.UserIDByNameCalls()))
	})

	t.Run("successful update by id", func(t *testing.T) {
		mockDetector.ResetCalls()
		locatorMock.ResetCalls()
		req, err := http.NewRequest("POST", "/users/add", bytes.NewBuffer([]byte(`{"user_id" : "123"}`)))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.updateApprovedUsersHandler(server.Detector.AddApprovedUser))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")
		var response struct {
			Updated  bool   `json:"updated"`
			UserID   string `json:"user_id"`
			UserName string `json:"user_name"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.True(t, response.Updated)
		assert.Equal(t, "123", response.UserID)
		assert.Equal(t, "", response.UserName)
		assert.Equal(t, 1, len(mockDetector.AddApprovedUserCalls()))
		assert.Equal(t, "123", mockDetector.AddApprovedUserCalls()[0].User.UserID)
		assert.Equal(t, 0, len(locatorMock.UserIDByNameCalls()))
	})
	t.Run("bad request", func(t *testing.T) {
		mockDetector.ResetCalls()
		reqBody := []byte("bad request")
		req, err := http.NewRequest("POST", "/users/add", bytes.NewBuffer(reqBody))
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.updateApprovedUsersHandler(server.Detector.AddApprovedUser))
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusBadRequest, rr.Code, "handler returned wrong status code")
	})
}

func TestServer_htmlDetectedSpamHandler(t *testing.T) {
	calls := 0
	ds := &mocks.DetectedSpamMock{
		ReadFunc: func() ([]storage.DetectedSpamInfo, error) {
			calls++
			if calls > 1 {
				return nil, errors.New("test error")
			}
			return []storage.DetectedSpamInfo{
				{
					Text:      "spam1 12345'",
					UserID:    12345,
					UserName:  "user1",
					Timestamp: time.Now(),
				},
				{
					Text:      "spam2",
					UserID:    67890,
					UserName:  "user2",
					Timestamp: time.Now(),
				},
			}, nil
		},
	}
	server := NewServer(Config{DetectedSpam: ds})

	t.Run("successful rendering", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/detected_spam", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.htmlDetectedSpamHandler)

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Contains(t, rr.Body.String(), "<h4>Detected Spam (2)</h4>")
		assert.Contains(t, rr.Body.String(), "spam1 12345 ")
		t.Log(rr.Body.String())
	})

	t.Run("detected spam reading failure", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/detected_spam", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.htmlDetectedSpamHandler)

		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}

func TestServer_htmlAddDetectedSpamHandler(t *testing.T) {
	ds := &mocks.DetectedSpamMock{
		SetAddedToSamplesFlagFunc: func(id int64) error {
			return nil
		},
	}
	sf := &mocks.SpamFilterMock{
		UpdateSpamFunc: func(msg string) error {
			return nil
		},
	}
	server := NewServer(Config{DetectedSpam: ds, SpamFilter: sf})
	req, err := http.NewRequest("POST", "/detected_spam/add?id=123&msg=blah", http.NoBody)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(server.htmlAddDetectedSpamHandler)

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, 1, len(ds.SetAddedToSamplesFlagCalls()))
	assert.Equal(t, int64(123), ds.SetAddedToSamplesFlagCalls()[0].ID)
	assert.Equal(t, 1, len(sf.UpdateSpamCalls()))
	assert.Equal(t, "blah", sf.UpdateSpamCalls()[0].Msg)
}

func TestServer_GenerateRandomPassword(t *testing.T) {
	res1, err := GenerateRandomPassword(32)
	require.NoError(t, err)
	t.Log(res1)
	assert.Len(t, res1, 32)

	res2, err := GenerateRandomPassword(32)
	require.NoError(t, err)
	t.Log(res2)
	assert.Len(t, res2, 32)

	assert.NotEqual(t, res1, res2)
}

func TestServer_checkHandler_HTMX(t *testing.T) {
	mockDetector := &mocks.DetectorMock{
		CheckFunc: func(req spamcheck.Request) (bool, []spamcheck.Response) {
			return req.Msg == "spam example", []spamcheck.Response{{Spam: req.Msg == "spam example", Name: "test", Details: "result details"}}
		},
		RemoveApprovedUserFunc: func(id string) error {
			return nil
		},
	}

	server := NewServer(Config{
		Detector: mockDetector,
		Version:  "1.0",
	})

	t.Run("HTMX request", func(t *testing.T) {
		form := url.Values{}
		form.Set("msg", "spam example")
		form.Set("user_id", "user123")
		req, err := http.NewRequest("POST", "/check", strings.NewReader(form.Encode()))
		require.NoError(t, err)
		req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Add("HX-Request", "true") // Simulating HTMX request

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.checkHandler)

		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")

		// check if the response contains expected HTML snippet
		assert.Contains(t, rr.Body.String(), "strong>Result:</strong> Spam detected", "response should contain spam result")
		assert.Contains(t, rr.Body.String(), "result details")

		assert.Equal(t, 1, len(mockDetector.CheckCalls()))
		assert.Equal(t, "spam example", mockDetector.CheckCalls()[0].Req.Msg)
		assert.Equal(t, "user123", mockDetector.CheckCalls()[0].Req.UserID)

		// check if id cleaned
		assert.Equal(t, 1, len(mockDetector.RemoveApprovedUserCalls()))
		assert.Equal(t, "user123", mockDetector.RemoveApprovedUserCalls()[0].ID)
	})
}

func TestServer_htmlSpamCheckHandler(t *testing.T) {
	server := NewServer(Config{Version: "1.0"})
	rr := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/", http.NoBody)
	require.NoError(t, err)

	handler := http.HandlerFunc(server.htmlSpamCheckHandler)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "handler should return status OK")
	body := rr.Body.String()
	assert.Contains(t, body, "<title>Checker - TG-Spam</title>", "template should contain the correct title")
	assert.Contains(t, body, "Version: 1.0", "template should contain the correct version")
	assert.Contains(t, body, "<form", "template should contain a form")
}

func TestServer_htmlManageSamplesHandler(t *testing.T) {
	spamFilterMock := &mocks.SpamFilterMock{
		DynamicSamplesFunc: func() ([]string, []string, error) {
			return []string{"spam1", "spam2"}, []string{"ham1", "ham2"}, nil
		},
	}

	server := NewServer(Config{Version: "1.0", SpamFilter: spamFilterMock})
	rr := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/manage_samples", http.NoBody)
	require.NoError(t, err)

	handler := http.HandlerFunc(server.htmlManageSamplesHandler)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "handler should return status OK")
	body := rr.Body.String()
	assert.Contains(t, body, "<title>Manage Samples - TG-Spam</title>", "template should contain the correct title")
	assert.Contains(t, body, `<div class="row" id="samples-list">`, "template should contain a samples list")
}

func TestServer_htmlManageUsersHandler(t *testing.T) {
	spamFilterMock := &mocks.SpamFilterMock{}
	detectorMock := &mocks.DetectorMock{
		ApprovedUsersFunc: func() []approved.UserInfo {
			return []approved.UserInfo{{UserID: "user1"}, {UserID: "user2"}}
		},
	}

	server := NewServer(Config{Version: "1.0", SpamFilter: spamFilterMock, Detector: detectorMock})
	rr := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/manage_users", http.NoBody)
	require.NoError(t, err)

	handler := http.HandlerFunc(server.htmlManageUsersHandler)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "handler should return status OK")
	body := rr.Body.String()
	assert.Contains(t, body, "<title>Manage Users - TG-Spam</title>", "template should contain the correct title")
	assert.Contains(t, body, "<h4>Approved Users (2)</h4>", "template should contain users list")
}

func TestServer_htmlSettingsHandler(t *testing.T) {
	server := NewServer(Config{Version: "1.0", Settings: Settings{SuperUsers: []string{"user1", "user2"}, MinMsgLen: 150}})
	rr := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/settings", http.NoBody)
	require.NoError(t, err)

	handler := http.HandlerFunc(server.htmlSettingsHandler)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "handler should return status OK")
	body := rr.Body.String()
	assert.Contains(t, body, "<title>Settings - TG-Spam</title>", "template should contain the correct title")
	assert.Contains(t, body, "<tr><th>Super Users</th><td>user1<br>user2<br></td></tr>", "template should contain supers list")
	assert.Contains(t, body, "<tr><th>Min Message Length</th><td>150</td></tr>")
}

func TestServer_stylesHandler(t *testing.T) {
	server := NewServer(Config{Version: "1.0"})
	rr := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/style.css", http.NoBody)
	require.NoError(t, err)

	handler := http.HandlerFunc(server.stylesHandler)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "handler should return status OK")
	assert.Equal(t, "text/css; charset=utf-8", rr.Header().Get("Content-Type"), "handler should return CSS content type")
	assert.Contains(t, rr.Body.String(), "body", "handler should return CSS content")
}

func TestServer_logoHandler(t *testing.T) {
	server := NewServer(Config{Version: "1.0"})
	rr := httptest.NewRecorder()
	req, err := http.NewRequest("GET", "/logo.png", http.NoBody)
	require.NoError(t, err)

	handler := http.HandlerFunc(server.logoHandler)
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code, "handler should return status OK")
	assert.Equal(t, "image/png", rr.Header().Get("Content-Type"), "handler should return CSS content type")
}

func Test_downloadSampleHandler(t *testing.T) {
	mockSpamFilter := &mocks.SpamFilterMock{
		DynamicSamplesFunc: func() ([]string, []string, error) {
			return []string{"spam1", "spam2"}, []string{"ham1", "ham2"}, nil
		},
	}

	server := NewServer(Config{
		SpamFilter: mockSpamFilter,
	})

	t.Run("successful spam response", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/download/spam", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.downloadSampleHandler(func(spam, ham []string) ([]string, string) {
			return spam, "spam.txt"
		}))

		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "text/plain; charset=utf-8", rr.Header().Get("Content-Type"))
		assert.Contains(t, rr.Header().Get("Content-Disposition"), "attachment; filename=\"spam.txt\"")
	})

	t.Run("successful ham response", func(t *testing.T) {
		req, err := http.NewRequest("GET", "/download/ham", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.downloadSampleHandler(func(spam, ham []string) ([]string, string) {
			return spam, "ham.txt"
		}))

		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "text/plain; charset=utf-8", rr.Header().Get("Content-Type"))
		assert.Contains(t, rr.Header().Get("Content-Disposition"), "attachment; filename=\"ham.txt\"")
	})

	t.Run("error handling", func(t *testing.T) {
		mockSpamFilter.DynamicSamplesFunc = func() ([]string, []string, error) {
			return nil, nil, errors.New("test error")
		}

		req, err := http.NewRequest("GET", "/download/ham", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.downloadSampleHandler(func(spam, ham []string) ([]string, string) {
			return spam, "ham.txt"
		}))

		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)

		var response struct {
			Error   string `json:"error"`
			Details string `json:"details"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "can't get dynamic samples", response.Error)
		assert.Equal(t, "test error", response.Details)
	})
}

func TestServer_reloadDynamicSamplesHandler(t *testing.T) {
	mockSpamFilter := &mocks.SpamFilterMock{
		ReloadSamplesFunc: func() error {
			return nil // Simulate successful reload
		},
	}

	server := NewServer(Config{
		SpamFilter: mockSpamFilter,
	})

	t.Run("successful reload", func(t *testing.T) {
		req, err := http.NewRequest("PUT", "/samples", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.reloadDynamicSamplesHandler)

		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

		var response struct {
			Reloaded bool `json:"reloaded"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.True(t, response.Reloaded)
	})

	t.Run("error during reload", func(t *testing.T) {
		mockSpamFilter.ReloadSamplesFunc = func() error {
			return errors.New("test error") // Simulate error during reload
		}

		req, err := http.NewRequest("PUT", "/samples", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.reloadDynamicSamplesHandler)

		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusInternalServerError, rr.Code)

		var response struct {
			Error   string `json:"error"`
			Details string `json:"details"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, "can't reload samples", response.Error)
		assert.Equal(t, "test error", response.Details)
	})
}

func TestServer_reverseSamples(t *testing.T) {
	tests := []struct {
		name    string
		spam    []string
		ham     []string
		revSpam []string
		revHam  []string
	}{
		{
			name:    "Empty slices",
			spam:    []string{},
			ham:     []string{},
			revSpam: []string{},
			revHam:  []string{},
		},
		{
			name:    "Single element slices",
			spam:    []string{"a"},
			ham:     []string{"1"},
			revSpam: []string{"a"},
			revHam:  []string{"1"},
		},
		{
			name:    "Multiple elements",
			spam:    []string{"a", "b", "c"},
			ham:     []string{"1", "2", "3"},
			revSpam: []string{"c", "b", "a"},
			revHam:  []string{"3", "2", "1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{}
			gotSpam, gotHam := s.reverseSamples(tt.spam, tt.ham)
			assert.Equal(t, tt.revSpam, gotSpam)
			assert.Equal(t, tt.revHam, gotHam)
		})
	}
}

func TestServer_renderSamples(t *testing.T) {
	mockSpamFilter := &mocks.SpamFilterMock{
		DynamicSamplesFunc: func() ([]string, []string, error) {
			return []string{"spam1", "spam2"}, []string{"ham1", "ham2"}, nil
		},
	}

	server := NewServer(Config{
		SpamFilter: mockSpamFilter,
	})
	w := httptest.NewRecorder()
	server.renderSamples(w, "samples_list")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/html; charset=utf-8", w.Header().Get("Content-Type"))
	t.Log(w.Body.String())
	assert.Contains(t, w.Body.String(), "Spam Samples (2)")
	assert.Contains(t, w.Body.String(), "spam1")
	assert.Contains(t, w.Body.String(), "spam2")
	assert.Contains(t, w.Body.String(), "Ham Samples (2)")
	assert.Contains(t, w.Body.String(), "ham1")
	assert.Contains(t, w.Body.String(), "ham2")
}
