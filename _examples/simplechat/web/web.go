package web

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam"

	"github.com/umputun/tg-spam/_examples/simplechat/storage"
)

//go:embed assets/*
var assets embed.FS
var tmpl = template.Must(template.New("").ParseFS(assets, "assets/*.html"))

// Server represents a HTTP server that listens and serves requests.
//
// This is demo code for the tg-spam library, not a template for a real service. Its authentication
// is deliberately minimal: passwords live in memory as plain text, sessions never expire and are
// dropped on restart, and there is no CSRF protection, rate limiting or account lockout. Do not
// reuse it outside this example.
type Server struct {
	Addr            string
	Storage         Storage
	UserCredentials map[string]string

	sessions struct {
		data map[string]string // session id to username
		sync.RWMutex
	}
	Detector *tgspam.Detector
}

// Storage is an interface for storing and retrieving messages
type Storage interface {
	Add(content, username string) error
	Last(count int) ([]storage.Message, error)
	Count() (int, error)
}

// ListenAndServe starts the HTTP server
func (s *Server) ListenAndServe() error {
	log.Printf("Starting server on %s", s.Addr)
	s.sessions.data = make(map[string]string)
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.loggingMiddleware(s.authMiddleware(s.indexHandler)))
	mux.HandleFunc("/login", s.loggingMiddleware(s.loginHandler))
	mux.HandleFunc("/post", s.loggingMiddleware(s.authMiddleware(s.postMessageHandler)))
	mux.HandleFunc("/fetch-messages", s.loggingMiddleware(s.authMiddleware(s.fetchMessagesHandler)))
	mux.HandleFunc("/dismiss-spam-report", s.dismissSpamReportHandler)

	srv := &http.Server{
		Addr:              s.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		return fmt.Errorf("server listen failed: %w", err)
	}
	return nil
}

// indexHandler handles the root endpoint ("/") and renders the "index.html" template with the last 100 messages from the storage.
// If there is an error retrieving the messages or executing the template, it returns a server error HTTP status code.
func (s *Server) indexHandler(w http.ResponseWriter, _ *http.Request) {
	messages, err := s.Storage.Last(100)
	if err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "index.html", messages); err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
	}
}

// fetchMessagesHandler handles the HTTP request for fetching messages.
// It retrieves the value of the "message-count" query parameter, if present,
// and converts it to an integer. This count is used to determine if there
// are any new messages to send. If the count matches the count in the
// database, a non-2xx status code is sent to avoid HTMX processing.
// Otherwise, it retrieves the latest messages from the storage and renders
// them using the "message_list.html" template. If any error occurs during
// the retrieval or rendering, a "Server error" status is sent.
func (s *Server) fetchMessagesHandler(w http.ResponseWriter, r *http.Request) {
	count := 0
	if countQuery := r.URL.Query().Get("message-count"); countQuery != "" {
		c, err := strconv.Atoi(countQuery)
		if err == nil {
			count = c
		}
	}
	messages, err := s.Storage.Last(100)
	if err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
		return
	}

	if dbCount, err := s.Storage.Count(); err == nil && dbCount == count {
		// avoid sending messages if there are no new messages
		w.WriteHeader(http.StatusExpectationFailed) // we need some non-2xx to avoid HTMX processing
		return
	}

	if err := tmpl.ExecuteTemplate(w, "message_list.html", messages); err != nil {
		http.Error(w, "Server error", http.StatusInternalServerError)
	}
}

// postMessageHandler handles the HTTP request for posting a new message. It parses the form and
// checks if the message is empty. If the message is not empty, it retrieves the username from the session cookie
// and adds the message to the storage. If the message is spam, it returns a "spam_report.html" template with the spam report.
// If any error occurs during the retrieval or rendering, a "Server error" status is sent.
func (s *Server) postMessageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	content := r.Form.Get("message")
	if content == "" {
		http.Error(w, "Empty message", http.StatusBadRequest)
		return
	}

	username, ok := s.sessionUser(r)
	if !ok {
		http.Error(w, "Invalid session", http.StatusBadRequest)
		return
	}

	// check for spam
	spam, details := s.Detector.Check(spamcheck.Request{Msg: content, UserID: username})
	if spam {
		log.Printf("spam detected: %+v", details)
		w.WriteHeader(http.StatusOK) // use OK status for HTMX to process
		data := struct {
			Content string
			Checks  []spamcheck.Response
		}{
			Content: content,
			Checks:  details,
		}
		if err := tmpl.ExecuteTemplate(w, "spam_report.html", data); err != nil {
			http.Error(w, "Server error", http.StatusInternalServerError)
		}
	} else {
		if err := s.Storage.Add(content, username); err != nil {
			http.Error(w, "Server error", http.StatusInternalServerError)
			return
		}
	}

	s.fetchMessagesHandler(w, r)
}

// sessionUser returns the username behind the request's session cookie. The username comes from the
// server-side session map, never from the cookie value, so a client cannot pick who it posts as.
func (s *Server) sessionUser(r *http.Request) (string, bool) {
	sessionCookie, err := r.Cookie("session")
	if err != nil {
		return "", false
	}
	s.sessions.RLock()
	defer s.sessions.RUnlock()
	username, ok := s.sessions.data[sessionCookie.Value]
	return username, ok
}

// dismissSpamReportHandler handles requests to dismiss a spam report
func (s *Server) dismissSpamReportHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK) // use OK status for HTMX to process
}

// loginHandler handles login requests.
// It serves the login page on GET requests and authenticates the user on POST requests.
func (s *Server) loginHandler(w http.ResponseWriter, r *http.Request) {
	createSession := func(username string) string {
		// the session id must not be derived from the username: anyone could otherwise compute
		// another user's cookie and post as them
		sessionID := rand.Text()
		s.sessions.Lock()
		defer s.sessions.Unlock()
		// one session per user; nothing here expires or evicts, so without this a fresh id on
		// every login would grow the map for as long as the process runs
		for id, name := range s.sessions.data {
			if name == username {
				delete(s.sessions.data, id)
			}
		}
		s.sessions.data[sessionID] = username
		return sessionID
	}

	checkCredentials := func(username, password string) bool {
		pass, ok := s.UserCredentials[username]
		if !ok {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(pass), []byte(password)) == 1
	}
	if r.Method == "GET" {
		err := tmpl.ExecuteTemplate(w, "login.html", nil)
		if err != nil {
			http.Error(w, "Problem with login template", http.StatusInternalServerError)
			return
		}
		return
	}

	if r.Method == "POST" {
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "Invalid form", http.StatusBadRequest)
			return
		}

		username := r.FormValue("username")
		password := r.FormValue("password")

		if checkCredentials(username, password) {
			sessionID := createSession(username)
			http.SetCookie(w, &http.Cookie{ // #nosec G124 - simplechat can run over plain HTTP; keep Secure conditional
				Name:     "session",
				Value:    sessionID,
				Path:     "/",
				HttpOnly: true,
				Secure:   r.TLS != nil,
				SameSite: http.SameSiteLaxMode,
			})
			w.Header().Set("HX-Redirect", "/")
			fmt.Fprint(w, "Login successful")
		} else {
			fmt.Fprint(w, `<div class="alert alert-danger" role="alert">Invalid username or password</div>`)
		}
	}
}

// authMiddleware checks if the request has a valid session cookie. If not, it redirects to the login page.
// Sessions have no expiry, so they stay valid until the process restarts; adequate for a demo, not for
// anything else.
func (s *Server) authMiddleware(handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.sessionUser(r); !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		handler(w, r)
	}
}

// loggingMiddleware logs the request method, URI and remote address
func (s *Server) loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("request: %s %s %s", r.Method, r.RequestURI, r.RemoteAddr)
		next(w, r)
	}
}
