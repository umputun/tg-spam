// Package webapi provides a web API spam detection service.
package webapi

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth_chi"
	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/go-pkgz/lgr"
	"github.com/go-pkgz/rest"

	"github.com/umputun/tg-spam/lib"
)

//go:generate moq --out mocks/spam_filter.go --pkg mocks --with-resets --skip-ensure . SpamFilter

// Server is a web API server.
type Server struct {
	Config
}

// Config defines  server parameters
type Config struct {
	Version    string     // version to show in /ping
	ListenAddr string     // listen address
	SpamFilter SpamFilter // spam detector
	AuthPasswd string     // basic auth password for user "tg-spam"
	Dbg        bool       // debug mode
}

// SpamFilter is a spam detector interface.
type SpamFilter interface {
	Check(msg string, userID string) (spam bool, cr []lib.CheckResult)
	UpdateSpam(msg string) error
	UpdateHam(msg string) error
	AddApprovedUsers(ids ...string)
	RemoveApprovedUsers(ids ...string)
	ApprovedUsers() (res []string)
}

// NewServer creates a new web API server.
func NewServer(config Config) *Server {
	return &Server{Config: config}
}

// Run starts server and accepts requests checking for spam messages.
func (s *Server) Run(ctx context.Context) error {
	router := chi.NewRouter()
	router.Use(rest.Recoverer(lgr.Default()))
	router.Use(middleware.Throttle(1000), middleware.Timeout(60*time.Second))
	router.Use(rest.AppInfo("tg-spam", "umputun", s.Version), rest.Ping)
	router.Use(tollbooth_chi.LimitHandler(tollbooth.NewLimiter(50, nil)))
	router.Use(rest.SizeLimit(1024 * 1024)) // 1M max request size

	if s.AuthPasswd != "" {
		log.Printf("[INFO] basic auth enabled for webapi server")
		router.Use(rest.BasicAuth(func(user, passwd string) bool {
			return subtle.ConstantTimeCompare([]byte(user), []byte("tg-spam")) == 1 &&
				subtle.ConstantTimeCompare([]byte(passwd), []byte(s.AuthPasswd)) == 1
		}))
	} else {
		log.Printf("[WARN] basic auth disabled, access to webapi is not protected")
	}

	router = s.routes(router) // setup routes

	srv := &http.Server{Addr: s.ListenAddr, Handler: router, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("[WARN] failed to shutdown webapi server: %v", err)
		} else {
			log.Printf("[INFO] webapi server stopped")
		}
	}()

	log.Printf("[INFO] start webapi server on %s", s.ListenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to run server: %w", err)
	}
	return nil
}

func (s *Server) routes(router *chi.Mux) *chi.Mux {
	router.Post("/check", s.checkHandler) // check a message for spam

	router.Route("/update", func(r chi.Router) { // update spam/ham samples
		r.Post("/spam", s.updateSampleHandler(s.SpamFilter.UpdateSpam)) // update spam samples
		r.Post("/ham", s.updateSampleHandler(s.SpamFilter.UpdateHam))   // update ham samples
	})

	router.Route("/users", func(r chi.Router) { // manage approved users
		r.Post("/", s.updateApprovedUsersHandler(s.SpamFilter.AddApprovedUsers))      // add user to the approved list
		r.Delete("/", s.updateApprovedUsersHandler(s.SpamFilter.RemoveApprovedUsers)) // remove user from approved list
		r.Get("/", s.getApprovedUsersHandler)                                         // get approved users
	})
	return router
}

// checkHandler handles POST /check request.
// it gets message text and user id from request body and returns spam status and check results.
func (s *Server) checkHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Msg    string `json:"msg"`
		UserID string `json:"user_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		rest.RenderJSON(w, rest.JSON{"error": "can't decode request", "details": err.Error()})
		return
	}

	spam, cr := s.SpamFilter.Check(req.Msg, req.UserID)
	rest.RenderJSON(w, rest.JSON{"spam": spam, "checks": cr})
}

// updateSampleHandler handles POST /update/spam and /update/ham requests.
// it gets message text from request body and updates spam or ham dynamic samples.
func (s *Server) updateSampleHandler(updFn func(msg string) error) func(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Msg string `json:"msg"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			rest.RenderJSON(w, rest.JSON{"error": "can't decode request", "details": err.Error()})
			return
		}

		err := updFn(req.Msg)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			rest.RenderJSON(w, rest.JSON{"error": "can't update samples", "details": err.Error()})
			return
		}
		rest.RenderJSON(w, rest.JSON{"updated": true, "msg": req.Msg})
	}
}

// updateApprovedUsersHandler handles POST /users and DELETE /users requests, it adds or removes users from approved list.
func (s *Server) updateApprovedUsersHandler(updFn func(ids ...string)) func(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserIDs []string `json:"user_ids"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			rest.RenderJSON(w, rest.JSON{"error": "can't decode request", "details": err.Error()})
			return
		}

		updFn(req.UserIDs...)
		rest.RenderJSON(w, rest.JSON{"updated": true, "count": len(req.UserIDs)})
	}
}

// getApprovedUsersHandler handles GET /users request. It returns list of approved users.
func (s *Server) getApprovedUsersHandler(w http.ResponseWriter, _ *http.Request) {
	rest.RenderJSON(w, rest.JSON{"user_ids": s.SpamFilter.ApprovedUsers()})
}

// GenerateRandomPassword generates a random password of a given length
func GenerateRandomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+"

	var password strings.Builder
	charsetSize := big.NewInt(int64(len(charset)))

	for i := 0; i < length; i++ {
		randomNumber, err := rand.Int(rand.Reader, charsetSize)
		if err != nil {
			return "", err
		}

		password.WriteByte(charset[randomNumber.Int64()])
	}

	return password.String(), nil
}
