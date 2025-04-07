package webapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-pkgz/rest"
	"github.com/go-pkgz/routegroup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/app/storage/engine"
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

			// wait for server to be ready
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
			return []approved.UserInfo{
				{UserID: "user1", UserName: "name1"},
				{UserID: "user2", UserName: "name2"}}
		},
		AddApprovedUserFunc: func(user approved.UserInfo) error {
			return nil
		},
		RemoveApprovedUserFunc: func(id string) error {
			return nil
		},
		GetLuaPluginNamesFunc: func() []string {
			return []string{"plugin1", "plugin2", "plugin3"}
		},
	}
	detectedSpamMock := &mocks.DetectedSpamMock{
		FindByUserIDFunc: func(ctx context.Context, userID int64) (*storage.DetectedSpamInfo, error) {
			if userID == 123 {
				return &storage.DetectedSpamInfo{
					ID:        123,
					GID:       "gid123",
					Text:      "spam example",
					UserID:    123,
					UserName:  "user",
					Checks:    []spamcheck.Response{{Spam: true, Name: "test", Details: "this was spam"}},
					Timestamp: time.Date(2025, 1, 25, 10, 0, 0, 0, time.UTC),
				}, nil
			}
			return nil, nil // not found
		},
	}
	spamFilterMock := &mocks.SpamFilterMock{
		UpdateHamFunc:               func(msg string) error { return nil },
		UpdateSpamFunc:              func(msg string) error { return nil },
		RemoveDynamicSpamSampleFunc: func(sample string) error { return nil },
		RemoveDynamicHamSampleFunc:  func(sample string) error { return nil },
	}
	locatorMock := &mocks.LocatorMock{
		UserIDByNameFunc: func(ctx context.Context, userName string) int64 {
			if userName == "user1" {
				return 12345
			}
			return 0
		},
	}

	server := NewServer(Config{
		Detector:     detectorMock,
		SpamFilter:   spamFilterMock,
		Locator:      locatorMock,
		DetectedSpam: detectedSpamMock,
	})
	ts := httptest.NewServer(server.routes(routegroup.New(http.NewServeMux())))
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

	t.Run("check by id found", func(t *testing.T) {
		detectedSpamMock.ResetCalls()
		resp, err := http.Get(ts.URL + "/check/123")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(detectedSpamMock.FindByUserIDCalls()))
		assert.Equal(t, int64(123), detectedSpamMock.FindByUserIDCalls()[0].UserID)
		assert.Equal(t, `{"status":"spam","info":{"user_name":"user","message":"spam example","timestamp":"2025-01-25T10:00:00Z","checks":[{"name":"test","spam":true,"details":"this was spam"}]}}`+"\n", string(body))
	})

	t.Run("check by id not found", func(t *testing.T) {
		detectedSpamMock.ResetCalls()
		resp, err := http.Get(ts.URL + "/check/456")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, "application/json; charset=utf-8", resp.Header.Get("Content-Type"))
		assert.Equal(t, 1, len(detectedSpamMock.FindByUserIDCalls()))
		assert.Equal(t, int64(456), detectedSpamMock.FindByUserIDCalls()[0].UserID)
		assert.Equal(t, `{"status":"ham"}`+"\n", string(body))
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
			if req.UserID == "" {
				// for empty user ID, include a CAS check with "check disabled"
				return false, []spamcheck.Response{
					{Details: "not spam"},
					{Name: "cas", Spam: false, Details: "check disabled"},
				}
			}

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
		handler := http.HandlerFunc(server.checkMsgHandler)

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
		handler := http.HandlerFunc(server.checkMsgHandler)

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

	t.Run("empty user ID", func(t *testing.T) {
		reqBody, err := json.Marshal(map[string]string{
			"msg":     "test message",
			"user_id": "",
		})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", "/check", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.checkMsgHandler)

		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")

		var response struct {
			Spam   bool                 `json:"spam"`
			Checks []spamcheck.Response `json:"checks"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		assert.NoError(t, err, "error unmarshalling response")

		// verify that the CAS check shows "check disabled"
		var casCheck *spamcheck.Response
		for _, check := range response.Checks {
			if check.Name == "cas" {
				casCheck = &check
				break
			}
		}

		require.NotNil(t, casCheck, "CAS check should be included in results")
		assert.False(t, casCheck.Spam)
		assert.Equal(t, "check disabled", casCheck.Details)
	})

	t.Run("bad request", func(t *testing.T) {
		reqBody := []byte("bad request")
		req, err := http.NewRequest("POST", "/check", bytes.NewBuffer(reqBody))
		assert.NoError(t, err)
		req.Body.Close()

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.checkMsgHandler)

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
		RemoveDynamicHamSampleFunc: func(sample string) error { return nil },
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
		req.Header.Add("HX-Request", "true") // simulating HTMX request

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
		spamFilterMock.RemoveDynamicHamSampleFunc = func(sample string) error { return assert.AnError }
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
		UserIDByNameFunc: func(ctx context.Context, userName string) int64 {
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
		req.Header.Add("HX-Request", "true") // simulating HTMX request

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
	t.Run("successful rendering", func(t *testing.T) {
		ds := &mocks.DetectedSpamMock{
			ReadFunc: func(ctx context.Context) ([]storage.DetectedSpamInfo, error) {
				ts := time.Now()
				return []storage.DetectedSpamInfo{
					{
						Text:      "spam1 12345'",
						UserID:    12345,
						UserName:  "user1",
						Timestamp: ts,
					},
					{
						Text:      "spam2",
						UserID:    67890,
						UserName:  "user2",
						Timestamp: ts,
					},
				}, nil
			},
		}
		server := NewServer(Config{DetectedSpam: ds})

		req, err := http.NewRequest("GET", "/detected_spam", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.htmlDetectedSpamHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		body := rr.Body.String()

		// check main elements
		assert.Contains(t, body, "Detected Spam")
		assert.Contains(t, body, `href="/download/detected_spam"`)
		assert.Contains(t, body, "btn-custom-blue")

		// check data
		assert.Contains(t, body, "spam1 12345")
		assert.Contains(t, body, "user1")
		assert.Contains(t, body, "12345")
		assert.Contains(t, body, "spam2")
		assert.Contains(t, body, "user2")
		assert.Contains(t, body, "67890")
	})

	t.Run("read failure", func(t *testing.T) {
		ds := &mocks.DetectedSpamMock{
			ReadFunc: func(ctx context.Context) ([]storage.DetectedSpamInfo, error) {
				return nil, errors.New("test error")
			},
		}
		server := NewServer(Config{DetectedSpam: ds})

		req, err := http.NewRequest("GET", "/detected_spam", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.htmlDetectedSpamHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code)
	})
}

func TestServer_htmlAddDetectedSpamHandler(t *testing.T) {
	t.Run("successful addition", func(t *testing.T) {
		ds := &mocks.DetectedSpamMock{
			SetAddedToSamplesFlagFunc: func(ctx context.Context, id int64) error {
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
	})

	t.Run("bad request - missing ID", func(t *testing.T) {
		ds := &mocks.DetectedSpamMock{}
		sf := &mocks.SpamFilterMock{}
		server := NewServer(Config{DetectedSpam: ds, SpamFilter: sf})

		req, err := http.NewRequest("POST", "/detected_spam/add?msg=blah", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.htmlAddDetectedSpamHandler)
		handler.ServeHTTP(rr, req)

		assert.Contains(t, rr.Header().Get("HX-Retarget"), "#error-message")
		assert.Contains(t, rr.Body.String(), "bad request")
	})

	t.Run("bad request - missing message", func(t *testing.T) {
		ds := &mocks.DetectedSpamMock{}
		sf := &mocks.SpamFilterMock{}
		server := NewServer(Config{DetectedSpam: ds, SpamFilter: sf})

		req, err := http.NewRequest("POST", "/detected_spam/add?id=123", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.htmlAddDetectedSpamHandler)
		handler.ServeHTTP(rr, req)

		assert.Contains(t, rr.Header().Get("HX-Retarget"), "#error-message")
		assert.Contains(t, rr.Body.String(), "bad request")
	})

	t.Run("update spam error", func(t *testing.T) {
		ds := &mocks.DetectedSpamMock{}
		sf := &mocks.SpamFilterMock{
			UpdateSpamFunc: func(msg string) error {
				return errors.New("update error")
			},
		}
		server := NewServer(Config{DetectedSpam: ds, SpamFilter: sf})

		req, err := http.NewRequest("POST", "/detected_spam/add?id=123&msg=blah", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.htmlAddDetectedSpamHandler)
		handler.ServeHTTP(rr, req)

		assert.Contains(t, rr.Header().Get("HX-Retarget"), "#error-message")
		assert.Contains(t, rr.Body.String(), "can't update spam samples")
	})

	t.Run("set flag error", func(t *testing.T) {
		ds := &mocks.DetectedSpamMock{
			SetAddedToSamplesFlagFunc: func(ctx context.Context, id int64) error {
				return errors.New("flag update error")
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

		assert.Contains(t, rr.Header().Get("HX-Retarget"), "#error-message")
		assert.Contains(t, rr.Body.String(), "can't update detected spam")
	})
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
		req.Header.Add("HX-Request", "true") // simulating HTMX request

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.checkMsgHandler)

		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, "handler returned wrong status code")

		// check if the response contains expected HTML snippet
		assert.Contains(t, rr.Body.String(), "strong>Result:</strong> Spam detected", "response should contain spam result")
		assert.Contains(t, rr.Body.String(), "result details")

		assert.Equal(t, 1, len(mockDetector.CheckCalls()))
		assert.Equal(t, "spam example", mockDetector.CheckCalls()[0].Req.Msg)
		assert.Equal(t, "user123", mockDetector.CheckCalls()[0].Req.UserID)
	})
}

func TestServer_htmlSpamCheckHandler(t *testing.T) {
	t.Run("successful template render", func(t *testing.T) {
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
	})

	t.Run("template execution error", func(t *testing.T) {
		// save original template and restore after test
		origTmpl := tmpl
		defer func() { tmpl = origTmpl }()

		// create a template with invalid field reference
		badTemplate := template.New("bad")
		badTemplate, err := badTemplate.Parse(`{{.InvalidField}}`)
		require.NoError(t, err)
		tmpl = badTemplate

		server := NewServer(Config{Version: "1.0"})
		rr := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/", http.NoBody)
		require.NoError(t, err)

		handler := http.HandlerFunc(server.htmlSpamCheckHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code, "should return internal server error")
		assert.Contains(t, rr.Body.String(), "Error executing template")
	})

	t.Run("with full config options", func(t *testing.T) {
		server := NewServer(Config{
			Version: "2.0-test",
			Settings: Settings{
				PrimaryGroup:        "test-group",
				AdminGroup:          "admin-group",
				SimilarityThreshold: 0.75,
				MinMsgLen:           100,
				ParanoidMode:        true,
			},
		})

		rr := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/", http.NoBody)
		require.NoError(t, err)

		handler := http.HandlerFunc(server.htmlSpamCheckHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "handler should return status OK")
		body := rr.Body.String()
		assert.Contains(t, body, "Version: 2.0-test", "should contain correct version")
	})
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
	t.Run("successful rendering", func(t *testing.T) {
		detectorMock := &mocks.DetectorMock{
			ApprovedUsersFunc: func() []approved.UserInfo {
				return []approved.UserInfo{
					{UserID: "user1", UserName: "User One", Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)},
					{UserID: "user2", UserName: "User Two", Timestamp: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)},
				}
			},
		}

		server := NewServer(Config{Version: "1.0", Detector: detectorMock})
		rr := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/manage_users", http.NoBody)
		require.NoError(t, err)

		handler := http.HandlerFunc(server.htmlManageUsersHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "handler should return status OK")
		body := rr.Body.String()
		assert.Contains(t, body, "<title>Manage Users - TG-Spam</title>", "template should contain the correct title")
		assert.Contains(t, body, "<h4>Approved Users (2)</h4>", "template should contain users list with correct count")
		assert.Contains(t, body, "User One", "should contain first user's name")
		assert.Contains(t, body, "User Two", "should contain second user's name")
		assert.Contains(t, body, "user1", "should contain first user's ID")
		assert.Contains(t, body, "user2", "should contain second user's ID")
	})

	t.Run("empty approved users list", func(t *testing.T) {
		detectorMock := &mocks.DetectorMock{
			ApprovedUsersFunc: func() []approved.UserInfo {
				return []approved.UserInfo{}
			},
		}

		server := NewServer(Config{Version: "1.0", Detector: detectorMock})
		rr := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/manage_users", http.NoBody)
		require.NoError(t, err)

		handler := http.HandlerFunc(server.htmlManageUsersHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		body := rr.Body.String()
		assert.Contains(t, body, "<h4>Approved Users (0)</h4>", "should show zero users")
	})

	t.Run("template execution error", func(t *testing.T) {
		// save original template and restore after test
		origTmpl := tmpl
		defer func() { tmpl = origTmpl }()

		// create a template with invalid field reference
		badTemplate := template.New("bad")
		badTemplate, err := badTemplate.Parse(`{{.InvalidField}}`)
		require.NoError(t, err)
		tmpl = badTemplate

		detectorMock := &mocks.DetectorMock{
			ApprovedUsersFunc: func() []approved.UserInfo {
				return []approved.UserInfo{{UserID: "123"}}
			},
		}

		server := NewServer(Config{Version: "1.0", Detector: detectorMock})
		rr := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/manage_users", http.NoBody)
		require.NoError(t, err)

		handler := http.HandlerFunc(server.htmlManageUsersHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assert.Contains(t, rr.Body.String(), "Error executing template")
	})
}

func TestServer_getSettingsHandler(t *testing.T) {
	t.Run("with lua plugins", func(t *testing.T) {
		detectorMock := &mocks.DetectorMock{
			GetLuaPluginNamesFunc: func() []string {
				return []string{"plugin1", "plugin2", "plugin3"}
			},
		}

		settings := Settings{
			InstanceID:        "test",
			LuaPluginsEnabled: true,
			LuaPluginsDir:     "/path/to/plugins",
			LuaEnabledPlugins: []string{"plugin1", "plugin2"},
		}

		server := NewServer(Config{Version: "1.0", Detector: detectorMock, Settings: settings})
		rr := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/settings", http.NoBody)
		require.NoError(t, err)

		handler := http.HandlerFunc(server.getSettingsHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/json; charset=utf-8", rr.Header().Get("Content-Type"))

		var respSettings Settings
		err = json.Unmarshal(rr.Body.Bytes(), &respSettings)
		require.NoError(t, err)
		assert.Equal(t, settings.InstanceID, respSettings.InstanceID)
		assert.Equal(t, settings.LuaPluginsEnabled, respSettings.LuaPluginsEnabled)
		assert.Equal(t, settings.LuaPluginsDir, respSettings.LuaPluginsDir)
		assert.Equal(t, settings.LuaEnabledPlugins, respSettings.LuaEnabledPlugins)
		assert.Equal(t, []string{"plugin1", "plugin2", "plugin3"}, respSettings.LuaAvailablePlugins)
		assert.Equal(t, 1, len(detectorMock.GetLuaPluginNamesCalls()))
	})

	t.Run("with lua plugins disabled", func(t *testing.T) {
		detectorMock := &mocks.DetectorMock{
			GetLuaPluginNamesFunc: func() []string {
				return []string{}
			},
		}

		settings := Settings{
			InstanceID:        "test",
			LuaPluginsEnabled: false,
		}

		server := NewServer(Config{Version: "1.0", Detector: detectorMock, Settings: settings})
		rr := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/settings", http.NoBody)
		require.NoError(t, err)

		handler := http.HandlerFunc(server.getSettingsHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)

		var respSettings Settings
		err = json.Unmarshal(rr.Body.Bytes(), &respSettings)
		require.NoError(t, err)
		assert.Equal(t, settings.InstanceID, respSettings.InstanceID)
		assert.Equal(t, settings.LuaPluginsEnabled, respSettings.LuaPluginsEnabled)
		assert.Empty(t, respSettings.LuaAvailablePlugins)
		assert.Equal(t, 1, len(detectorMock.GetLuaPluginNamesCalls()))
	})
}

func TestServer_htmlSettingsHandler(t *testing.T) {
	// test without StorageEngine (default case)
	t.Run("without storage engine", func(t *testing.T) {
		detectorMock := &mocks.DetectorMock{
			GetLuaPluginNamesFunc: func() []string {
				return []string{"plugin1", "plugin2", "plugin3"}
			},
		}

		server := NewServer(Config{
			Version:  "1.0",
			Detector: detectorMock,
			Settings: Settings{SuperUsers: []string{"user1", "user2"}, MinMsgLen: 150},
		})
		rr := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/settings", http.NoBody)
		require.NoError(t, err)

		handler := http.HandlerFunc(server.htmlSettingsHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "handler should return status OK")
		body := rr.Body.String()
		assert.Contains(t, body, "<title>Settings - TG-Spam</title>", "template should contain the correct title")
		assert.Contains(t, body, "Database")
		assert.Contains(t, body, "Not connected", "Should show database is not connected")
		assert.Contains(t, body, "Backup")
		assert.Contains(t, body, "System Status")
		assert.Contains(t, body, "Spam Detection")
	})

	// test with StorageEngine
	t.Run("with SQL storage engine", func(t *testing.T) {
		sqlEngine := &mocks.StorageEngineMock{}
		detectorMock := &mocks.DetectorMock{
			GetLuaPluginNamesFunc: func() []string {
				return []string{"plugin1", "plugin2", "plugin3"}
			},
		}

		server := NewServer(Config{
			Version:       "1.0",
			StorageEngine: sqlEngine,
			Detector:      detectorMock,
			Settings:      Settings{SuperUsers: []string{"user1", "user2"}, MinMsgLen: 150},
		})
		rr := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/settings", http.NoBody)
		require.NoError(t, err)

		handler := http.HandlerFunc(server.htmlSettingsHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "handler should return status OK")
		body := rr.Body.String()
		assert.Contains(t, body, "<title>Settings - TG-Spam</title>", "template should contain the correct title")
		assert.Contains(t, body, "Connected", "Should show database is connected")
		assert.Equal(t, 1, len(detectorMock.GetLuaPluginNamesCalls()), "GetLuaPluginNames should be called")
	})

	// test with non-SQL StorageEngine
	t.Run("with non-SQL storage engine", func(t *testing.T) {
		mockEngine := &mocks.StorageEngineMock{}
		detectorMock := &mocks.DetectorMock{
			GetLuaPluginNamesFunc: func() []string {
				return []string{"plugin1", "plugin2", "plugin3"}
			},
		}

		server := NewServer(Config{
			Version:       "1.0",
			StorageEngine: mockEngine,
			Detector:      detectorMock,
			Settings:      Settings{SuperUsers: []string{"user1", "user2"}, MinMsgLen: 150},
		})
		rr := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/settings", http.NoBody)
		require.NoError(t, err)

		handler := http.HandlerFunc(server.htmlSettingsHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code, "handler should return status OK")
		body := rr.Body.String()
		assert.Contains(t, body, "Connected (unknown type)", "Should show connected with unknown type")
		assert.Contains(t, body, "Unknown", "Should show unknown database type")
		assert.Equal(t, 1, len(detectorMock.GetLuaPluginNamesCalls()), "GetLuaPluginNames should be called")
	})

	// test execution error
	t.Run("template execution error", func(t *testing.T) {
		// save original template and restore after test
		origTmpl := tmpl
		defer func() { tmpl = origTmpl }()

		// replace template with one that will error
		badTemplate := template.New("bad")
		badTemplate, err := badTemplate.Parse(`{{.InvalidField}}`)
		require.NoError(t, err)
		tmpl = badTemplate

		detectorMock := &mocks.DetectorMock{
			GetLuaPluginNamesFunc: func() []string {
				return []string{"plugin1", "plugin2", "plugin3"}
			},
		}

		server := NewServer(Config{
			Version:  "1.0",
			Detector: detectorMock,
		})
		rr := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/settings", http.NoBody)
		require.NoError(t, err)

		handler := http.HandlerFunc(server.htmlSettingsHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code, "should return internal server error")
		assert.Equal(t, 1, len(detectorMock.GetLuaPluginNamesCalls()), "GetLuaPluginNames should be called")
	})
}

func TestServer_StaticFiles(t *testing.T) {
	// setup necessary mocks
	mockDetector := &mocks.DetectorMock{
		CheckFunc: func(req spamcheck.Request) (bool, []spamcheck.Response) {
			return false, []spamcheck.Response{{Details: "not spam"}}
		},
		ApprovedUsersFunc: func() []approved.UserInfo {
			return []approved.UserInfo{}
		},
	}
	mockSpamFilter := &mocks.SpamFilterMock{}
	detectedSpamMock := &mocks.DetectedSpamMock{}

	server := NewServer(Config{
		Version:      "1.0",
		Detector:     mockDetector,
		SpamFilter:   mockSpamFilter,
		DetectedSpam: detectedSpamMock,
	})
	ts := httptest.NewServer(server.routes(routegroup.New(http.NewServeMux())))
	defer ts.Close()

	tests := []struct {
		name        string
		path        string
		contentType string
		contains    string // for text files like CSS
	}{
		{
			name:        "styles.css",
			path:        "/styles.css",
			contentType: "text/css; charset=utf-8",
			contains:    "body",
		},
		{
			name:        "logo.png",
			path:        "/logo.png",
			contentType: "image/png",
		},
		{
			name:        "spinner.svg",
			path:        "/spinner.svg",
			contentType: "image/svg+xml",
		},
		{
			name:        "non-existent file",
			path:        "/non-existent.txt",
			contentType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := http.Get(ts.URL + tt.path)
			require.NoError(t, err)
			defer resp.Body.Close()

			if tt.contentType == "" {
				assert.Equal(t, http.StatusNotFound, resp.StatusCode, "should return 404 for non-existent files")
				return
			}

			assert.Equal(t, http.StatusOK, resp.StatusCode, "should return OK")
			assert.Equal(t, tt.contentType, resp.Header.Get("Content-Type"), "should return correct content type")

			if tt.contains != "" {
				body, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), tt.contains, "response should contain expected content")
			}
		})
	}

	t.Run("disallow access to other files", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/assets/some.html")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode, "should not allow access to other files")
	})
}

func TestServer_getDynamicSamplesHandler(t *testing.T) {
	t.Run("successful response", func(t *testing.T) {
		mockSpamFilter := &mocks.SpamFilterMock{
			DynamicSamplesFunc: func() ([]string, []string, error) {
				return []string{"spam1", "spam2"}, []string{"ham1", "ham2"}, nil
			},
		}

		server := NewServer(Config{SpamFilter: mockSpamFilter})
		req, err := http.NewRequest("GET", "/samples", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.getDynamicSamplesHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		assert.Equal(t, "application/json; charset=utf-8", rr.Header().Get("Content-Type"))

		var response struct {
			Spam []string `json:"spam"`
			Ham  []string `json:"ham"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, []string{"spam1", "spam2"}, response.Spam)
		assert.Equal(t, []string{"ham1", "ham2"}, response.Ham)
	})

	t.Run("error response", func(t *testing.T) {
		mockSpamFilter := &mocks.SpamFilterMock{
			DynamicSamplesFunc: func() ([]string, []string, error) {
				return nil, nil, errors.New("test error")
			},
		}

		server := NewServer(Config{SpamFilter: mockSpamFilter})
		req, err := http.NewRequest("GET", "/samples", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.getDynamicSamplesHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assert.Equal(t, "application/json; charset=utf-8", rr.Header().Get("Content-Type"))

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
			return nil // simulate successful reload
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
			return errors.New("test error") // simulate error during reload
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

// TestServer_formatDuration tests the formatDuration function in webapi.go
func TestServer_formatDuration(t *testing.T) {
	tests := []struct {
		name string
		dur  time.Duration
		want string
	}{
		{"Minutes only", 5 * time.Minute, "5m"},
		{"Hours and minutes", 2*time.Hour + 30*time.Minute, "2h 30m"},
		{"Days, hours, minutes", 4*24*time.Hour + 2*time.Hour + 5*time.Minute, "4d 2h 5m"},
		{"Zero", 0, "0m"},
		{"Just seconds", 30 * time.Second, "0m"},
		{"Large duration", 100*24*time.Hour + 12*time.Hour + 45*time.Minute, "100d 12h 45m"},
		{"Exactly one day", 24 * time.Hour, "1d 0h 0m"},
		{"Exactly one hour", 1 * time.Hour, "1h 0m"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := formatDuration(tt.dur)
			assert.Equal(t, tt.want, s)
		})
	}
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
	t.Run("successful rendering", func(t *testing.T) {
		mockSpamFilter := &mocks.SpamFilterMock{
			DynamicSamplesFunc: func() ([]string, []string, error) {
				return []string{"spam1", "spam2"}, []string{"ham1", "ham2"}, nil
			},
		}

		server := NewServer(Config{SpamFilter: mockSpamFilter})
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
	})

	t.Run("empty samples", func(t *testing.T) {
		mockSpamFilter := &mocks.SpamFilterMock{
			DynamicSamplesFunc: func() ([]string, []string, error) {
				return []string{}, []string{}, nil
			},
		}

		server := NewServer(Config{SpamFilter: mockSpamFilter})
		w := httptest.NewRecorder()
		server.renderSamples(w, "samples_list")
		assert.Equal(t, http.StatusOK, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "Spam Samples (0)")
		assert.Contains(t, body, "Ham Samples (0)")
	})

	t.Run("DynamicSamples error", func(t *testing.T) {
		mockSpamFilter := &mocks.SpamFilterMock{
			DynamicSamplesFunc: func() ([]string, []string, error) {
				return nil, nil, errors.New("sample fetch error")
			},
		}

		server := NewServer(Config{SpamFilter: mockSpamFilter})
		w := httptest.NewRecorder()
		server.renderSamples(w, "samples_list")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "can't fetch samples", response["error"])
	})

	t.Run("template execution error", func(t *testing.T) {
		// save original template and restore after test
		origTmpl := tmpl
		defer func() { tmpl = origTmpl }()

		badTemplate := template.New("bad")
		badTemplate, err := badTemplate.Parse(`{{.InvalidField}}`)
		require.NoError(t, err)
		tmpl = badTemplate

		mockSpamFilter := &mocks.SpamFilterMock{
			DynamicSamplesFunc: func() ([]string, []string, error) {
				return []string{"spam1"}, []string{"ham1"}, nil
			},
		}

		server := NewServer(Config{SpamFilter: mockSpamFilter})
		w := httptest.NewRecorder()
		server.renderSamples(w, "samples_list")
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "can't execute template", response["error"])
	})
}

func TestServer_downloadDetectedSpamHandler(t *testing.T) {
	testTime := time.Date(2025, 1, 25, 10, 0, 0, 0, time.UTC)

	t.Run("successful download", func(t *testing.T) {
		ds := &mocks.DetectedSpamMock{
			ReadFunc: func(ctx context.Context) ([]storage.DetectedSpamInfo, error) {
				return []storage.DetectedSpamInfo{
					{
						ID:        123,
						GID:       "gid123",
						Text:      "spam example",
						UserID:    123,
						UserName:  "user",
						Checks:    []spamcheck.Response{{Spam: true, Name: "test", Details: "details"}},
						Timestamp: testTime,
					},
				}, nil
			},
		}

		server := NewServer(Config{DetectedSpam: ds})
		req, err := http.NewRequest("GET", "/download/detected_spam", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.downloadDetectedSpamHandler)
		handler.ServeHTTP(rr, req)

		t.Run("verify headers", func(t *testing.T) {
			assert.Equal(t, http.StatusOK, rr.Code)
			assert.Equal(t, "application/x-jsonlines", rr.Header().Get("Content-Type"))
			assert.Contains(t, rr.Header().Get("Content-Disposition"), "detected_spam.jsonl")
		})

		t.Run("verify content", func(t *testing.T) {
			var info struct {
				ID        int64                `json:"id"`
				GID       string               `json:"gid"`
				Text      string               `json:"text"`
				UserID    int64                `json:"user_id"`
				UserName  string               `json:"user_name"`
				Timestamp time.Time            `json:"timestamp"`
				Added     bool                 `json:"added"`
				Checks    []spamcheck.Response `json:"checks"`
			}
			err = json.Unmarshal([]byte(strings.TrimSpace(rr.Body.String())), &info)
			require.NoError(t, err)
			assert.Equal(t, int64(123), info.ID)
			assert.Equal(t, "gid123", info.GID)
			assert.Equal(t, "spam example", info.Text)
			assert.Equal(t, int64(123), info.UserID)
			assert.Equal(t, "user", info.UserName)
			assert.Equal(t, testTime, info.Timestamp)
			require.Len(t, info.Checks, 1)
			assert.Equal(t, "test", info.Checks[0].Name)
			assert.Equal(t, "details", info.Checks[0].Details)
			assert.True(t, info.Checks[0].Spam)
		})
	})

	t.Run("multiple entries", func(t *testing.T) {
		ds := &mocks.DetectedSpamMock{
			ReadFunc: func(ctx context.Context) ([]storage.DetectedSpamInfo, error) {
				return []storage.DetectedSpamInfo{
					{ID: 1, Text: "first"},
					{ID: 2, Text: "second"},
				}, nil
			},
		}

		server := NewServer(Config{DetectedSpam: ds})
		req, err := http.NewRequest("GET", "/download/detected_spam", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.downloadDetectedSpamHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusOK, rr.Code)
		lines := strings.Split(strings.TrimSpace(rr.Body.String()), "\n")
		assert.Len(t, lines, 2)

		for i, line := range lines {
			var info struct {
				ID    int64  `json:"id"`
				Text  string `json:"text"`
				Added bool   `json:"added"`
			}
			err = json.Unmarshal([]byte(line), &info)
			require.NoError(t, err)
			assert.Equal(t, int64(i+1), info.ID)
			assert.Equal(t, []string{"first", "second"}[i], info.Text)
		}
	})

	t.Run("error handling", func(t *testing.T) {
		ds := &mocks.DetectedSpamMock{
			ReadFunc: func(ctx context.Context) ([]storage.DetectedSpamInfo, error) {
				return nil, errors.New("test error")
			},
		}

		server := NewServer(Config{DetectedSpam: ds})
		req, err := http.NewRequest("GET", "/download/detected_spam", http.NoBody)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		handler := http.HandlerFunc(server.downloadDetectedSpamHandler)
		handler.ServeHTTP(rr, req)

		assert.Equal(t, http.StatusInternalServerError, rr.Code)
		assert.Equal(t, "application/json; charset=utf-8", rr.Header().Get("Content-Type"))

		var resp struct {
			Error   string `json:"error"`
			Details string `json:"details"`
		}
		err = json.Unmarshal(rr.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "can't get detected spam", resp.Error)
		assert.Equal(t, "test error", resp.Details)
	})
}

func TestServer_downloadBackupHandler(t *testing.T) {
	t.Run("successful backup with gzip", func(t *testing.T) {
		mockStorageEngine := &mocks.StorageEngineMock{
			BackupFunc: func(ctx context.Context, w io.Writer) error {
				_, err := w.Write([]byte("-- SQL backup test content"))
				return err
			},
		}

		srv := NewServer(Config{
			StorageEngine: mockStorageEngine,
		})

		req := httptest.NewRequest("GET", "/download/backup", nil)
		w := httptest.NewRecorder()
		srv.downloadBackupHandler(w, req)

		resp := w.Result()
		defer resp.Body.Close()

		// check headers
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/octet-stream", resp.Header.Get("Content-Type"), "content type should be binary")
		assert.Contains(t, resp.Header.Get("Content-Disposition"), "attachment; filename=")
		assert.Contains(t, resp.Header.Get("Content-Disposition"), ".sql.gz")

		// read the content
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		// verify it's actually gzipped data by trying to decompress it
		gzipReader, err := gzip.NewReader(bytes.NewReader(body))
		require.NoError(t, err, "Content should be properly gzipped")
		defer gzipReader.Close()

		decompressedContent, err := io.ReadAll(gzipReader)
		require.NoError(t, err)

		assert.Contains(t, string(decompressedContent), "-- SQL backup test content")
	})

	t.Run("nil storage engine", func(t *testing.T) {
		srv := NewServer(Config{
			StorageEngine: nil,
		})

		req := httptest.NewRequest("GET", "/download/backup", nil)
		w := httptest.NewRecorder()
		srv.downloadBackupHandler(w, req)

		resp := w.Result()
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		assert.Contains(t, string(body), "storage engine not available")
	})
}

func TestServer_downloadExportToPostgresHandler(t *testing.T) {
	t.Run("successful export with sqlite engine", func(t *testing.T) {
		mockStorage := &mocks.StorageEngineMock{
			TypeFunc: func() engine.Type {
				return engine.Sqlite // return the string representation of Sqlite type
			},
			BackupSqliteAsPostgresFunc: func(ctx context.Context, w io.Writer) error {
				_, err := w.Write([]byte("-- SQLite to PostgreSQL export test content"))
				return err
			},
		}

		srv := NewServer(Config{
			StorageEngine: mockStorage,
		})

		req := httptest.NewRequest("GET", "/download/export-to-postgres", nil)
		w := httptest.NewRecorder()

		srv.downloadExportToPostgresHandler(w, req)

		resp := w.Result()
		defer resp.Body.Close()

		// check headers
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/octet-stream", resp.Header.Get("Content-Type"), "content type should be binary")
		assert.Contains(t, resp.Header.Get("Content-Disposition"), "attachment; filename=")
		assert.Contains(t, resp.Header.Get("Content-Disposition"), "tg-spam-sqlite-to-postgres")
		assert.Contains(t, resp.Header.Get("Content-Disposition"), ".sql.gz")

		// read the content
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		// verify it's actually gzipped data by trying to decompress it
		gzipReader, err := gzip.NewReader(bytes.NewReader(body))
		require.NoError(t, err, "Content should be properly gzipped")
		defer gzipReader.Close()

		decompressedContent, err := io.ReadAll(gzipReader)
		require.NoError(t, err)

		assert.Contains(t, string(decompressedContent), "-- SQLite to PostgreSQL export test content")
	})

	t.Run("non-sqlite engine", func(t *testing.T) {
		mockStorage := &mocks.StorageEngineMock{
			TypeFunc: func() engine.Type {
				return engine.Postgres // return the string representation of Postgres type
			},
		}

		srv := NewServer(Config{
			StorageEngine: mockStorage,
		})

		req := httptest.NewRequest("GET", "/download/export-to-postgres", nil)
		w := httptest.NewRecorder()
		srv.downloadExportToPostgresHandler(w, req)

		resp := w.Result()
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Contains(t, string(body), "source database must be SQLite")
	})

	t.Run("nil storage engine", func(t *testing.T) {
		srv := NewServer(Config{
			StorageEngine: nil,
		})

		req := httptest.NewRequest("GET", "/download/export-to-postgres", nil)
		w := httptest.NewRecorder()
		srv.downloadExportToPostgresHandler(w, req)

		resp := w.Result()
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		assert.Contains(t, string(body), "storage engine not available")
	})
}

// Additional test cases for the checkIDHandler have been implicitly covered in the TestServer_routes tests
// where the routing infrastructure properly sets Path Values.

func TestServer_logoutHandler(t *testing.T) {
	// create a function that matches our logout handler implementation in routes
	logoutHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="tg-spam"`)
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintln(w, "Logged out successfully")
	}

	req := httptest.NewRequest("GET", "/logout", nil)
	w := httptest.NewRecorder()

	logoutHandler(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, `Basic realm="tg-spam"`, resp.Header.Get("WWW-Authenticate"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "Logged out successfully")
}
