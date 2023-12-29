// Package webapi provides a web API spam detection service.
package webapi

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth_chi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-pkgz/lgr"
	"github.com/go-pkgz/rest"

	"github.com/umputun/tg-spam/lib"
)

//go:generate moq --out mocks/detector.go --pkg mocks --with-resets --skip-ensure . Detector
//go:generate moq --out mocks/spam_filter.go --pkg mocks --with-resets --skip-ensure . SpamFilter
//go:generate moq --out mocks/locator.go --pkg mocks --with-resets --skip-ensure . Locator

//go:embed assets/* assets/components/*
var templateFS embed.FS

// Server is a web API server.
type Server struct {
	Config
}

// Config defines  server parameters
type Config struct {
	Version    string     // version to show in /ping
	ListenAddr string     // listen address
	Detector   Detector   // spam detector
	SpamFilter SpamFilter // spam filter (bot)
	Locator    Locator    // locator for user info
	AuthPasswd string     // basic auth password for user "tg-spam"
	Dbg        bool       // debug mode
}

// Detector is a spam detector interface.
type Detector interface {
	Check(req lib.CheckRequest) (spam bool, cr []lib.CheckResult)
	ApprovedUsers() []lib.UserInfo
	AddApprovedUser(user lib.UserInfo) error
	RemoveApprovedUser(id string) error
}

// SpamFilter is a spam filter, bot interface.
type SpamFilter interface {
	UpdateSpam(msg string) error
	UpdateHam(msg string) error
	ReloadSamples() (err error)
	DynamicSamples() (spam, ham []string, err error)
	RemoveDynamicSpamSample(sample string) (int, error)
	RemoveDynamicHamSample(sample string) (int, error)
}

// Locator is a storage interface used to get user id by name and vice versa.
type Locator interface {
	UserIDByName(userName string) int64
	UserNameByID(userID int64) string
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
	} else {
		log.Printf("[WARN] basic auth disabled, access to webapi is not protected")
	}
	router.Use(rest.BasicAuthWithPrompt("tg-spam", s.AuthPasswd))

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
	// auth api routes
	router.Group(func(authApi chi.Router) {
		authApi.Use(s.authMiddleware(rest.BasicAuthWithUserPasswd("tg-spam", s.AuthPasswd)))
		authApi.Post("/check", s.checkHandler) // check a message for spam

		authApi.Route("/update", func(r chi.Router) { // update spam/ham samples
			r.Post("/spam", s.updateSampleHandler(s.SpamFilter.UpdateSpam)) // update spam samples
			r.Post("/ham", s.updateSampleHandler(s.SpamFilter.UpdateHam))   // update ham samples
		})

		authApi.Route("/delete", func(r chi.Router) { // delete spam/ham samples
			r.Post("/spam", s.deleteSampleHandler(s.SpamFilter.RemoveDynamicSpamSample))
			r.Post("/ham", s.deleteSampleHandler(s.SpamFilter.RemoveDynamicHamSample))
		})

		authApi.Get("/samples", s.getDynamicSamplesHandler)    // get dynamic samples
		authApi.Put("/samples", s.reloadDynamicSamplesHandler) // reload samples

		authApi.Route("/users", func(r chi.Router) { // manage approved users
			r.Post("/add", s.updateApprovedUsersHandler(s.Detector.AddApprovedUser)) // add user to the approved list and storage
			r.Post("/delete", s.updateApprovedUsersHandler(s.removeApprovedUser))    // remove user from approved list and storage
			r.Get("/", s.getApprovedUsersHandler)                                    // get approved users
		})
	})

	router.Group(func(webUI chi.Router) {
		webUI.Use(s.authMiddleware(rest.BasicAuthWithPrompt("tg-spam", s.AuthPasswd)))
		webUI.Get("/", s.htmlSpamCheckHandler)                   // serve template for webUI UI
		webUI.Get("/manage_samples", s.htmlManageSamplesHandler) // serve manage samples page
		webUI.Get("/manage_users", s.htmlManageUsersHandler)     // serve manage users page
		webUI.Get("/styles.css", s.stylesHandler)                // serve styles.css
		webUI.Get("/logo.png", s.logoHandler)                    // serve logo.png

	})

	return router
}

// checkHandler handles POST /check request.
// it gets message text and user id from request body and returns spam status and check results.
func (s *Server) checkHandler(w http.ResponseWriter, r *http.Request) {

	type CheckResultDisplay struct {
		Spam   bool
		Checks []lib.CheckResult
	}

	isHtmxRequest := r.Header.Get("HX-Request") == "true"

	req := lib.CheckRequest{}
	if !isHtmxRequest {
		// API request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			rest.RenderJSON(w, rest.JSON{"error": "can't decode request", "details": err.Error()})
			log.Printf("[WARN] can't decode request: %v", err)
			return
		}
	} else {
		// for hx-request (HTMX) we need to get the values from the form
		req.UserID = r.FormValue("user_id")
		req.UserName = r.FormValue("user_name")
		req.Msg = r.FormValue("msg")
	}

	spam, cr := s.Detector.Check(req)
	if !isHtmxRequest {
		// for API request return JSON
		rest.RenderJSON(w, rest.JSON{"spam": spam, "checks": cr})
		return
	}

	// render result for HTMX request
	resultDisplay := CheckResultDisplay{
		Spam:   spam,
		Checks: cr,
	}

	tmpl, err := template.New("").ParseFS(templateFS, "assets/components/check_results.html")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		rest.RenderJSON(w, rest.JSON{"error": "can't parse template", "details": err.Error()})
		return
	}
	if err := tmpl.ExecuteTemplate(w, "check_results.html", resultDisplay); err != nil {
		log.Printf("[WARN] can't execute result template: %v", err)
		http.Error(w, "Error rendering result", http.StatusInternalServerError)
		return
	}
}

// getDynamicSamplesHandler handles GET /samples request. It returns dynamic samples both for spam and ham.
func (s *Server) getDynamicSamplesHandler(w http.ResponseWriter, _ *http.Request) {
	spam, ham, err := s.SpamFilter.DynamicSamples()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		rest.RenderJSON(w, rest.JSON{"error": "can't get dynamic samples", "details": err.Error()})
		return
	}
	rest.RenderJSON(w, rest.JSON{"spam": spam, "ham": ham})
}

// updateSampleHandler handles POST /update/spam|ham request. It updates dynamic samples both for spam and ham.
func (s *Server) updateSampleHandler(updFn func(msg string) error) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Msg string `json:"msg"`
		}

		isHtmxRequest := r.Header.Get("HX-Request") == "true"

		if isHtmxRequest {
			req.Msg = r.FormValue("msg")
		} else {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				rest.RenderJSON(w, rest.JSON{"error": "can't decode request", "details": err.Error()})
				return
			}
		}

		err := updFn(req.Msg)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			rest.RenderJSON(w, rest.JSON{"error": "can't update samples", "details": err.Error()})
			return
		}

		if isHtmxRequest {
			s.renderSamples(w)
		} else {
			rest.RenderJSON(w, rest.JSON{"updated": true, "msg": req.Msg})
		}
	}
}

// deleteSampleHandler handles DELETE /samples request. It deletes dynamic samples both for spam and ham.
func (s *Server) deleteSampleHandler(delFn func(msg string) (int, error)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Msg string `json:"msg"`
		}
		isHtmxRequest := r.Header.Get("HX-Request") == "true"
		if isHtmxRequest {
			req.Msg = r.FormValue("msg")
		} else {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				rest.RenderJSON(w, rest.JSON{"error": "can't decode request", "details": err.Error()})
				return
			}
		}

		count, err := delFn(req.Msg)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			rest.RenderJSON(w, rest.JSON{"error": "can't delete sample", "details": err.Error()})
			return
		}

		if isHtmxRequest {
			s.renderSamples(w)
		} else {
			rest.RenderJSON(w, rest.JSON{"deleted": true, "msg": req.Msg, "count": count})
		}
	}
}

// reloadDynamicSamplesHandler handles PUT /samples request. It reloads dynamic samples from files
func (s *Server) reloadDynamicSamplesHandler(w http.ResponseWriter, _ *http.Request) {
	if err := s.SpamFilter.ReloadSamples(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		rest.RenderJSON(w, rest.JSON{"error": "can't reload samples", "details": err.Error()})
		return
	}
	rest.RenderJSON(w, rest.JSON{"reloaded": true})
}

// updateApprovedUsersHandler handles POST /users/add and /users/delete requests, it adds or removes users from approved list.
func (s *Server) updateApprovedUsersHandler(updFn func(ui lib.UserInfo) error) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		req := lib.UserInfo{}
		isHtmxRequest := r.Header.Get("HX-Request") == "true"
		if isHtmxRequest {
			req.UserID = r.FormValue("user_id")
			req.UserName = r.FormValue("user_name")
		} else {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				rest.RenderJSON(w, rest.JSON{"error": "can't decode request", "details": err.Error()})
				return
			}
		}

		// try to get userID from request and fallback to userName lookup if it's empty
		if req.UserID == "" {
			req.UserID = strconv.FormatInt(s.Locator.UserIDByName(req.UserName), 10)
		}

		if req.UserID == "" || req.UserID == "0" {
			if isHtmxRequest {
				w.Header().Set("HX-Retarget", "#error-message")
				fmt.Fprintln(w, "<div class='alert alert-danger'>Either userid or valid username required.</div>")
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			rest.RenderJSON(w, rest.JSON{"error": "user ID is required"})
			return
		}

		// add or remove user from the approved list of detector
		if err := updFn(req); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			rest.RenderJSON(w, rest.JSON{"error": "can't update approved users", "details": err.Error()})
			return
		}

		if isHtmxRequest {
			tmpl, err := template.New("").ParseFS(templateFS, "assets/components/users_list.html")
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				rest.RenderJSON(w, rest.JSON{"error": "can't parse template", "details": err.Error()})
				return
			}
			users := s.Detector.ApprovedUsers()
			tmplData := struct {
				ApprovedUsers      []lib.UserInfo
				TotalApprovedUsers int
			}{
				ApprovedUsers:      users,
				TotalApprovedUsers: len(users),
			}

			if err := tmpl.ExecuteTemplate(w, "users_list.html", tmplData); err != nil {
				log.Printf("[WARN] can't execute template: %v", err)
				http.Error(w, "Error executing template", http.StatusInternalServerError)
				return
			}

		} else {
			rest.RenderJSON(w, rest.JSON{"updated": true, "user_id": req.UserID, "user_name": req.UserName})
		}
	}
}

// removeApprovedUser is adopter for updateApprovedUsersHandler updFn
func (s *Server) removeApprovedUser(req lib.UserInfo) error {
	return s.Detector.RemoveApprovedUser(req.UserID)
}

// getApprovedUsersHandler handles GET /users request. It returns list of approved users.
func (s *Server) getApprovedUsersHandler(w http.ResponseWriter, _ *http.Request) {
	rest.RenderJSON(w, rest.JSON{"user_ids": s.Detector.ApprovedUsers()})
}

// htmlSpamCheckHandler handles GET / request.
// It returns rendered spam_check.html template with all the components.
func (s *Server) htmlSpamCheckHandler(w http.ResponseWriter, _ *http.Request) {
	tmpl, err := template.New("").ParseFS(templateFS, "assets/spam_check.html", "assets/components/navbar.html")
	if err != nil {
		log.Printf("[WARN] can't load template: %v", err)
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	tmplData := struct {
		Version string
	}{
		Version: s.Version,
	}

	if err := tmpl.ExecuteTemplate(w, "spam_check.html", tmplData); err != nil {
		log.Printf("[WARN] can't execute template: %v", err)
		http.Error(w, "Error executing template", http.StatusInternalServerError)
		return
	}
}

// htmlManageSamplesHandler handles GET /manage_samples request.
// It returns rendered manage_samples.html template with all the components.
func (s *Server) htmlManageSamplesHandler(w http.ResponseWriter, _ *http.Request) {
	spam, ham, err := s.SpamFilter.DynamicSamples()
	if err != nil {
		log.Printf("[ERROR] Failed to fetch dynamic samples: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	spam, ham = s.reverseSamples(spam, ham)

	tmplData := struct {
		SpamSamples      []string
		HamSamples       []string
		TotalSpamSamples int
		TotalHamSamples  int
	}{
		SpamSamples:      spam,
		HamSamples:       ham,
		TotalSpamSamples: len(spam),
		TotalHamSamples:  len(ham),
	}

	// Parse the navbar and manage_samples templates
	tmpl, err := template.New("").ParseFS(templateFS,
		"assets/manage_samples.html", "assets/components/navbar.html", "assets/components/samples_list.html")
	if err != nil {
		log.Printf("[WARN] failed to parse templates: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Execute the manage_samples template with the data
	if err := tmpl.ExecuteTemplate(w, "manage_samples.html", tmplData); err != nil {
		log.Printf("[WARN] failed to execute template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func (s *Server) htmlManageUsersHandler(w http.ResponseWriter, _ *http.Request) {
	tmpl, err := template.New("").ParseFS(templateFS, "assets/manage_users.html",
		"assets/components/navbar.html", "assets/components/users_list.html")
	if err != nil {
		log.Printf("[WARN] can't load template: %v", err)
		http.Error(w, "Error loading template", http.StatusInternalServerError)
		return
	}

	users := s.Detector.ApprovedUsers()
	tmplData := struct {
		ApprovedUsers      []lib.UserInfo
		TotalApprovedUsers int
	}{
		ApprovedUsers:      users,
		TotalApprovedUsers: len(users),
	}
	tmplData.TotalApprovedUsers = len(tmplData.ApprovedUsers)

	if err := tmpl.ExecuteTemplate(w, "manage_users.html", tmplData); err != nil {
		log.Printf("[WARN] can't execute template: %v", err)
		http.Error(w, "Error executing template", http.StatusInternalServerError)
		return
	}
}

// stylesHandler handles GET /styles.css request. It returns styles.css file.
func (s *Server) stylesHandler(w http.ResponseWriter, _ *http.Request) {
	body, err := templateFS.ReadFile("assets/styles.css")
	if err != nil {
		log.Printf("[WARN] can't read styles.css: %v", err)
		http.Error(w, "Error reading styles.css", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// logoHandler handles GET /logo.png request. It returns assets/logo.png file.
func (s *Server) logoHandler(w http.ResponseWriter, _ *http.Request) {
	img, err := templateFS.ReadFile("assets/logo.png")
	if err != nil {
		http.Error(w, "Logo not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(img)
}

func (s *Server) renderSamples(w http.ResponseWriter) {
	spam, ham, err := s.SpamFilter.DynamicSamples()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		rest.RenderJSON(w, rest.JSON{"error": "can't fetch samples", "details": err.Error()})
		return
	}

	tmpl, err := template.New("").ParseFS(templateFS, "assets/components/samples_list.html")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		rest.RenderJSON(w, rest.JSON{"error": "can't parse template", "details": err.Error()})
		return
	}

	spam, ham = s.reverseSamples(spam, ham)
	tmplData := struct {
		SpamSamples      []string
		HamSamples       []string
		TotalHamSamples  int
		TotalSpamSamples int
	}{
		SpamSamples:      spam,
		HamSamples:       ham,
		TotalHamSamples:  len(ham),
		TotalSpamSamples: len(spam),
	}

	if err := tmpl.ExecuteTemplate(w, "samples_list.html", tmplData); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		rest.RenderJSON(w, rest.JSON{"error": "can't execute template", "details": err.Error()})
		return
	}
}

func (s *Server) authMiddleware(mw func(next http.Handler) http.Handler) func(next http.Handler) http.Handler {
	if s.AuthPasswd == "" {
		return func(next http.Handler) http.Handler {
			return next
		}
	}
	return func(next http.Handler) http.Handler {
		return mw(next)
	}
}

// reverseSamples returns reversed lists of spam and ham samples
func (s *Server) reverseSamples(spam, ham []string) (revSpam, revHam []string) {
	revSpam = make([]string, len(spam))
	revHam = make([]string, len(ham))

	for i, j := 0, len(spam)-1; i < len(spam); i, j = i+1, j-1 {
		revSpam[i] = spam[j]
	}
	for i, j := 0, len(ham)-1; i < len(ham); i, j = i+1, j-1 {
		revHam[i] = ham[j]
	}
	return revSpam, revHam
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
