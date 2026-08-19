package web

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/tgspam"

	"github.com/umputun/tg-spam/_examples/simplechat/storage"
)

type fakeStorage struct{ messages []storage.Message }

func (f *fakeStorage) Add(content, username string) error {
	f.messages = append(f.messages, storage.Message{Content: content, Username: username})
	return nil
}
func (f *fakeStorage) Last(int) ([]storage.Message, error) { return f.messages, nil }
func (f *fakeStorage) Count() (int, error)                 { return len(f.messages), nil }

func newTestServer(t *testing.T) (*Server, *fakeStorage) {
	t.Helper()
	store := &fakeStorage{}
	srv := &Server{
		Storage:         store,
		UserCredentials: map[string]string{"user1": "password1"},
		Detector:        tgspam.NewDetector(tgspam.Config{}),
	}
	srv.sessions.data = make(map[string]string)
	return srv, store
}

// login performs a successful login and returns the issued session cookie
func login(t *testing.T, srv *Server, username, password string) *http.Cookie {
	t.Helper()
	form := url.Values{"username": {username}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	srv.loginHandler(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	for _, c := range rr.Result().Cookies() {
		if c.Name == "session" {
			return c
		}
	}
	t.Fatalf("no session cookie issued, body: %s", rr.Body.String())
	return nil
}

func TestServer_loginRejectsBadCredentials(t *testing.T) {
	srv, _ := newTestServer(t)

	for _, tt := range []struct{ name, user, pass string }{
		{"wrong password", "user1", "nope"},
		{"unknown user", "nobody", "password1"},
		{"empty password", "user1", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			form := url.Values{"username": {tt.user}, "password": {tt.pass}}
			req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rr := httptest.NewRecorder()
			srv.loginHandler(rr, req)

			assert.Contains(t, rr.Body.String(), "Invalid username or password")
			assert.Empty(t, rr.Result().Cookies())
		})
	}
}

func TestServer_sessionIDIsNotDerivedFromUsername(t *testing.T) {
	srv, _ := newTestServer(t)
	cookie := login(t, srv, "user1", "password1")

	guessed := base64.StdEncoding.EncodeToString([]byte("user1"))
	assert.NotEqual(t, guessed, cookie.Value, "session id must not be computable from the username")

	protected := srv.authMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("issued session accepted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.AddCookie(cookie)
		rr := httptest.NewRecorder()
		protected(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
	})

	t.Run("forged session rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
		req.AddCookie(&http.Cookie{Name: "session", Value: guessed})
		rr := httptest.NewRecorder()
		protected(rr, req)
		assert.Equal(t, http.StatusSeeOther, rr.Code)
	})

	t.Run("no cookie rejected", func(t *testing.T) {
		rr := httptest.NewRecorder()
		protected(rr, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
		assert.Equal(t, http.StatusSeeOther, rr.Code)
	})
}

func TestServer_repeatedLoginsKeepOneSession(t *testing.T) {
	srv, _ := newTestServer(t)

	first := login(t, srv, "user1", "password1")
	second := login(t, srv, "user1", "password1")
	assert.NotEqual(t, first.Value, second.Value, "each login issues a fresh id")

	srv.sessions.RLock()
	defer srv.sessions.RUnlock()
	assert.Len(t, srv.sessions.data, 1, "nothing expires sessions, so a login must replace the previous one")
	assert.Equal(t, "user1", srv.sessions.data[second.Value])
}

func TestServer_postMessageUsesSessionUsername(t *testing.T) {
	srv, store := newTestServer(t)
	cookie := login(t, srv, "user1", "password1")

	form := url.Values{"message": {"hello there"}}
	req := httptest.NewRequest(http.MethodPost, "/post", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	srv.postMessageHandler(rr, req)

	require.Len(t, store.messages, 1)
	assert.Equal(t, "user1", store.messages[0].Username)
	assert.Equal(t, "hello there", store.messages[0].Content)
}

func TestServer_postMessageRejectsUnknownSession(t *testing.T) {
	srv, store := newTestServer(t)

	form := url.Values{"message": {"hello there"}}
	req := httptest.NewRequest(http.MethodPost, "/post", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: "session", Value: base64.StdEncoding.EncodeToString([]byte("user1"))})
	rr := httptest.NewRecorder()
	srv.postMessageHandler(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Empty(t, store.messages)
}
