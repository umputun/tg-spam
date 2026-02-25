// Package webapi provides a web API spam detection service.
package webapi

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha1" //nolint
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"math/big"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/didip/tollbooth/v8"
	log "github.com/go-pkgz/lgr"
	"github.com/go-pkgz/rest"
	"github.com/go-pkgz/rest/logger"
	"github.com/go-pkgz/routegroup"

	"github.com/umputun/tg-spam/app/storage"
	"github.com/umputun/tg-spam/app/storage/engine"
	"github.com/umputun/tg-spam/lib/approved"
	"github.com/umputun/tg-spam/lib/spamcheck"
)

//go:generate moq --out mocks/detector.go --pkg mocks --with-resets --skip-ensure . Detector
//go:generate moq --out mocks/spam_filter.go --pkg mocks --with-resets --skip-ensure . SpamFilter
//go:generate moq --out mocks/locator.go --pkg mocks --with-resets --skip-ensure . Locator
//go:generate moq --out mocks/detected_spam.go --pkg mocks --with-resets --skip-ensure . DetectedSpam
//go:generate moq --out mocks/storage_engine.go --pkg mocks --with-resets --skip-ensure . StorageEngine
//go:generate moq --out mocks/dictionary.go --pkg mocks --with-resets --skip-ensure . Dictionary

//go:embed assets/* assets/components/*
var templateFS embed.FS
var tmpl = template.Must(template.ParseFS(templateFS, "assets/*.html", "assets/components/*.html"))

// startTime tracks when the server started
var startTime = time.Now()

// Server is a web API server.
type Server struct {
	Config
}

// Config defines  server parameters
type Config struct {
	Version       string        // version to show in /ping
	ListenAddr    string        // listen address
	Detector      Detector      // spam detector
	SpamFilter    SpamFilter    // spam filter (bot)
	DetectedSpam  DetectedSpam  // detected spam accessor
	Locator       Locator       // locator for user info
	Dictionary    Dictionary    // dictionary for stop phrases and ignored words
	StorageEngine StorageEngine // database engine access for backups
	AuthPasswd    string        // basic auth password for user "tg-spam"
	AuthHash      string        // basic auth hash for user "tg-spam". If both AuthPasswd and AuthHash are provided, AuthHash is used
	Dbg           bool          // debug mode
	Settings      Settings      // application settings
}

// Settings contains all application settings
type Settings struct {
	InstanceID               string        `json:"instance_id"`
	PrimaryGroup             string        `json:"primary_group"`
	AdminGroup               string        `json:"admin_group"`
	DisableAdminSpamForward  bool          `json:"disable_admin_spam_forward"`
	LoggerEnabled            bool          `json:"logger_enabled"`
	SuperUsers               []string      `json:"super_users"`
	NoSpamReply              bool          `json:"no_spam_reply"`
	CasEnabled               bool          `json:"cas_enabled"`
	MetaEnabled              bool          `json:"meta_enabled"`
	MetaLinksLimit           int           `json:"meta_links_limit"`
	MetaMentionsLimit        int           `json:"meta_mentions_limit"`
	MetaLinksOnly            bool          `json:"meta_links_only"`
	MetaImageOnly            bool          `json:"meta_image_only"`
	MetaVideoOnly            bool          `json:"meta_video_only"`
	MetaAudioOnly            bool          `json:"meta_audio_only"`
	MetaForwarded            bool          `json:"meta_forwarded"`
	MetaKeyboard             bool          `json:"meta_keyboard"`
	MetaContactOnly          bool          `json:"meta_contact_only"`
	MetaUsernameSymbols      string        `json:"meta_username_symbols"`
	MetaGiveaway             bool          `json:"meta_giveaway"`
	MultiLangLimit           int           `json:"multi_lang_limit"`
	OpenAIEnabled            bool          `json:"openai_enabled"`
	OpenAIVeto               bool          `json:"openai_veto"`
	OpenAIHistorySize        int           `json:"openai_history_size"`
	OpenAIModel              string        `json:"openai_model"`
	OpenAICheckShortMessages bool          `json:"openai_check_short_messages"`
	OpenAICustomPrompts      []string      `json:"openai_custom_prompts"`
	LuaPluginsEnabled        bool          `json:"lua_plugins_enabled"`
	LuaPluginsDir            string        `json:"lua_plugins_dir"`
	LuaEnabledPlugins        []string      `json:"lua_enabled_plugins"`
	LuaDynamicReload         bool          `json:"lua_dynamic_reload"`
	LuaAvailablePlugins      []string      `json:"lua_available_plugins"` // the list of all available Lua plugins
	SamplesDataPath          string        `json:"samples_data_path"`
	DynamicDataPath          string        `json:"dynamic_data_path"`
	WatchIntervalSecs        int           `json:"watch_interval_secs"`
	SimilarityThreshold      float64       `json:"similarity_threshold"`
	MinMsgLen                int           `json:"min_msg_len"`
	MaxEmoji                 int           `json:"max_emoji"`
	MinSpamProbability       float64       `json:"min_spam_probability"`
	ParanoidMode             bool          `json:"paranoid_mode"`
	FirstMessagesCount       int           `json:"first_messages_count"`
	StartupMessageEnabled    bool          `json:"startup_message_enabled"`
	TrainingEnabled          bool          `json:"training_enabled"`
	StorageTimeout           time.Duration `json:"storage_timeout"`
	SoftBanEnabled           bool          `json:"soft_ban_enabled"`
	AbnormalSpacingEnabled   bool          `json:"abnormal_spacing_enabled"`
	HistorySize              int           `json:"history_size"`
	DebugModeEnabled         bool          `json:"debug_mode_enabled"`
	DryModeEnabled           bool          `json:"dry_mode_enabled"`
	TGDebugModeEnabled       bool          `json:"tg_debug_mode_enabled"`
}

// Detector is a spam detector interface.
type Detector interface {
	Check(req spamcheck.Request) (spam bool, cr []spamcheck.Response)
	ApprovedUsers() []approved.UserInfo
	AddApprovedUser(user approved.UserInfo) error
	RemoveApprovedUser(id string) error
	GetLuaPluginNames() []string // Returns the list of available Lua plugin names
}

// SpamFilter is a spam filter, bot interface.
type SpamFilter interface {
	UpdateSpam(msg string) error
	UpdateHam(msg string) error
	ReloadSamples() (err error)
	DynamicSamples() (spam, ham []string, err error)
	RemoveDynamicSpamSample(sample string) error
	RemoveDynamicHamSample(sample string) error
}

// Locator is a storage interface used to get user id by name and vice versa.
type Locator interface {
	UserIDByName(ctx context.Context, userName string) int64
	UserNameByID(ctx context.Context, userID int64) string
}

// DetectedSpam is a storage interface used to get detected spam messages and set added flag.
type DetectedSpam interface {
	Read(ctx context.Context) ([]storage.DetectedSpamInfo, error)
	SetAddedToSamplesFlag(ctx context.Context, id int64) error
	FindByUserID(ctx context.Context, userID int64) (*storage.DetectedSpamInfo, error)
}

// StorageEngine provides access to the database engine for operations like backup
type StorageEngine interface {
	Backup(ctx context.Context, w io.Writer) error
	Type() engine.Type
	BackupSqliteAsPostgres(ctx context.Context, w io.Writer) error
}

// Dictionary is a storage interface for managing stop phrases and ignored words
type Dictionary interface {
	Add(ctx context.Context, t storage.DictionaryType, data string) error
	Delete(ctx context.Context, id int64) error
	Read(ctx context.Context, t storage.DictionaryType) ([]string, error)
	ReadWithIDs(ctx context.Context, t storage.DictionaryType) ([]storage.DictionaryEntry, error)
	Stats(ctx context.Context) (*storage.DictionaryStats, error)
}

// NewServer creates a new web API server.
func NewServer(config Config) *Server {
	return &Server{Config: config}
}

// Run starts server and accepts requests checking for spam messages.
func (s *Server) Run(ctx context.Context) error {
	router := routegroup.New(http.NewServeMux())
	router.Use(rest.Recoverer(log.Default()))
	router.Use(logger.New(logger.Log(log.Default()), logger.Prefix("[DEBUG]")).Handler)
	router.Use(rest.Throttle(1000))
	router.Use(rest.AppInfo("tg-spam", "umputun", s.Version), rest.Ping)
	router.Use(tollbooth.HTTPMiddleware(tollbooth.NewLimiter(50, nil)))
	router.Use(rest.SizeLimit(1024 * 1024)) // 1M max request size

	if s.AuthPasswd != "" || s.AuthHash != "" {
		log.Printf("[INFO] basic auth enabled for webapi server")
		if s.AuthHash != "" {
			router.Use(rest.BasicAuthWithBcryptHashAndPrompt("tg-spam", s.AuthHash))
		} else {
			router.Use(rest.BasicAuthWithPrompt("tg-spam", s.AuthPasswd))
		}
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

func (s *Server) routes(router *routegroup.Bundle) *routegroup.Bundle {
	// auth api routes
	router.Group().Route(func(authApi *routegroup.Bundle) {
		authApi.Use(s.authMiddleware(rest.BasicAuthWithUserPasswd("tg-spam", s.AuthPasswd)))
		authApi.HandleFunc("POST /check", s.checkMsgHandler)         // check a message for spam
		authApi.HandleFunc("GET /check/{user_id}", s.checkIDHandler) // check user id for spam

		authApi.Mount("/update").Route(func(r *routegroup.Bundle) {
			// update spam/ham samples
			r.HandleFunc("POST /spam", s.updateSampleHandler(s.SpamFilter.UpdateSpam)) // update spam samples
			r.HandleFunc("POST /ham", s.updateSampleHandler(s.SpamFilter.UpdateHam))   // update ham samples
		})

		authApi.Mount("/delete").Route(func(r *routegroup.Bundle) {
			// delete spam/ham samples
			r.HandleFunc("POST /spam", s.deleteSampleHandler(s.SpamFilter.RemoveDynamicSpamSample))
			r.HandleFunc("POST /ham", s.deleteSampleHandler(s.SpamFilter.RemoveDynamicHamSample))
		})

		authApi.Mount("/download").Route(func(r *routegroup.Bundle) {
			r.HandleFunc("GET /spam", s.downloadSampleHandler(func(spam, _ []string) ([]string, string) {
				return spam, "spam.txt"
			}))
			r.HandleFunc("GET /ham", s.downloadSampleHandler(func(_, ham []string) ([]string, string) {
				return ham, "ham.txt"
			}))
			r.HandleFunc("GET /detected_spam", s.downloadDetectedSpamHandler)
			r.HandleFunc("GET /backup", s.downloadBackupHandler)
			r.HandleFunc("GET /export-to-postgres", s.downloadExportToPostgresHandler)
		})

		authApi.HandleFunc("GET /samples", s.getDynamicSamplesHandler)    // get dynamic samples
		authApi.HandleFunc("PUT /samples", s.reloadDynamicSamplesHandler) // reload samples

		authApi.Mount("/users").Route(func(r *routegroup.Bundle) { // manage approved users
			// add user to the approved list and storage
			r.HandleFunc("POST /add", s.updateApprovedUsersHandler(s.Detector.AddApprovedUser))
			// remove user from an approved list and storage
			r.HandleFunc("POST /delete", s.updateApprovedUsersHandler(s.removeApprovedUser))
			// get approved users
			r.HandleFunc("GET /", s.getApprovedUsersHandler)
		})

		authApi.HandleFunc("GET /settings", s.getSettingsHandler) // get application settings

		authApi.Mount("/dictionary").Route(func(r *routegroup.Bundle) { // manage dictionary
			// add stop phrase or ignored word
			r.HandleFunc("POST /add", s.addDictionaryEntryHandler)
			// delete entry by id
			r.HandleFunc("POST /delete", s.deleteDictionaryEntryHandler)
			// get all entries
			r.HandleFunc("GET /", s.getDictionaryEntriesHandler)
		})
	})

	router.Group().Route(func(webUI *routegroup.Bundle) {
		webUI.Use(s.authMiddleware(rest.BasicAuthWithPrompt("tg-spam", s.AuthPasswd)))
		webUI.HandleFunc("GET /", s.htmlSpamCheckHandler)                         // serve template for webUI UI
		webUI.HandleFunc("GET /manage_samples", s.htmlManageSamplesHandler)       // serve manage samples page
		webUI.HandleFunc("GET /manage_users", s.htmlManageUsersHandler)           // serve manage users page
		webUI.HandleFunc("GET /manage_dictionary", s.htmlManageDictionaryHandler) // serve manage dictionary page
		webUI.HandleFunc("GET /detected_spam", s.htmlDetectedSpamHandler)         // serve detected spam page
		webUI.HandleFunc("GET /list_settings", s.htmlSettingsHandler)             // serve settings
		webUI.HandleFunc("POST /detected_spam/add", s.htmlAddDetectedSpamHandler) // add detected spam to samples

		// handle logout - force Basic Auth re-authentication
		webUI.HandleFunc("GET /logout", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("WWW-Authenticate", `Basic realm="tg-spam"`)
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprintln(w, "Logged out successfully")
		})

		// serve only specific static files at root level
		staticFiles := newStaticFS(templateFS,
			staticFileMapping{urlPath: "styles.css", filesysPath: "assets/styles.css"},
			staticFileMapping{urlPath: "logo.png", filesysPath: "assets/logo.png"},
			staticFileMapping{urlPath: "spinner.svg", filesysPath: "assets/spinner.svg"},
		)
		webUI.HandleFiles("/", http.FS(staticFiles))
	})

	return router
}

// checkMsgHandler handles POST /check request.
// it gets message text and user id from request body and returns spam status and check results.
func (s *Server) checkMsgHandler(w http.ResponseWriter, r *http.Request) {
	type CheckResultDisplay struct {
		Spam   bool
		Checks []spamcheck.Response
	}

	isHtmxRequest := r.Header.Get("HX-Request") == "true"

	req := spamcheck.Request{CheckOnly: true}
	if !isHtmxRequest {
		// API request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			_ = rest.EncodeJSON(w, http.StatusBadRequest, rest.JSON{"error": "can't decode request", "details": err.Error()})
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

	if req.Msg == "" {
		w.Header().Set("HX-Retarget", "#error-message")
		fmt.Fprintln(w, "<div class='alert alert-danger'>Valid message required.</div>")
		return
	}

	// render result for HTMX request
	resultDisplay := CheckResultDisplay{
		Spam:   spam,
		Checks: cr,
	}

	if err := tmpl.ExecuteTemplate(w, "check_results", resultDisplay); err != nil {
		log.Printf("[WARN] can't execute result template: %v", err)
		http.Error(w, "Error rendering result", http.StatusInternalServerError)
		return
	}
}

// checkIDHandler handles GET /check/{user_id} request.
// it returns JSON with the status "spam" or "ham" for a given user id.
// if user is spammer, it also returns check results.
func (s *Server) checkIDHandler(w http.ResponseWriter, r *http.Request) {
	type info struct {
		UserName  string               `json:"user_name,omitempty"`
		Message   string               `json:"message,omitempty"`
		Timestamp time.Time            `json:"timestamp,omitzero"`
		Checks    []spamcheck.Response `json:"checks,omitempty"`
	}
	resp := struct {
		Status string `json:"status"`
		Info   *info  `json:"info,omitempty"`
	}{
		Status: "ham",
	}

	userID, err := strconv.ParseInt(r.PathValue("user_id"), 10, 64)
	if err != nil {
		_ = rest.EncodeJSON(w, http.StatusBadRequest, rest.JSON{"error": "can't parse user id", "details": err.Error()})
		return
	}

	si, err := s.DetectedSpam.FindByUserID(r.Context(), userID)
	if err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't get user info", "details": err.Error()})
		return
	}
	if si != nil {
		resp.Status = "spam"
		resp.Info = &info{
			UserName:  si.UserName,
			Message:   si.Text,
			Timestamp: si.Timestamp,
			Checks:    si.Checks,
		}
	}
	rest.RenderJSON(w, resp)
}

// getDynamicSamplesHandler handles GET /samples request. It returns dynamic samples both for spam and ham.
func (s *Server) getDynamicSamplesHandler(w http.ResponseWriter, _ *http.Request) {
	spam, ham, err := s.SpamFilter.DynamicSamples()
	if err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't get dynamic samples", "details": err.Error()})
		return
	}
	rest.RenderJSON(w, rest.JSON{"spam": spam, "ham": ham})
}

// downloadSampleHandler handles GET /download/spam|ham request.
// It returns dynamic samples both for spam and ham.
func (s *Server) downloadSampleHandler(pickFn func(spam, ham []string) ([]string, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		spam, ham, err := s.SpamFilter.DynamicSamples()
		if err != nil {
			_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't get dynamic samples", "details": err.Error()})
			return
		}
		samples, name := pickFn(spam, ham)
		body := strings.Join(samples, "\n")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}
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
				_ = rest.EncodeJSON(w, http.StatusBadRequest, rest.JSON{"error": "can't decode request", "details": err.Error()})
				return
			}
		}

		err := updFn(req.Msg)
		if err != nil {
			_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't update samples", "details": err.Error()})
			return
		}

		if isHtmxRequest {
			s.renderSamples(w, "samples_list")
		} else {
			rest.RenderJSON(w, rest.JSON{"updated": true, "msg": req.Msg})
		}
	}
}

// deleteSampleHandler handles DELETE /samples request. It deletes dynamic samples both for spam and ham.
func (s *Server) deleteSampleHandler(delFn func(msg string) error) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Msg string `json:"msg"`
		}
		isHtmxRequest := r.Header.Get("HX-Request") == "true"
		if isHtmxRequest {
			req.Msg = r.FormValue("msg")
		} else {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				_ = rest.EncodeJSON(w, http.StatusBadRequest, rest.JSON{"error": "can't decode request", "details": err.Error()})
				return
			}
		}

		if err := delFn(req.Msg); err != nil {
			_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't delete sample", "details": err.Error()})
			return
		}

		if isHtmxRequest {
			s.renderSamples(w, "samples_list")
		} else {
			rest.RenderJSON(w, rest.JSON{"deleted": true, "msg": req.Msg, "count": 1})
		}
	}
}

// reloadDynamicSamplesHandler handles PUT /samples request. It reloads dynamic samples from db storage.
func (s *Server) reloadDynamicSamplesHandler(w http.ResponseWriter, _ *http.Request) {
	if err := s.SpamFilter.ReloadSamples(); err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't reload samples", "details": err.Error()})
		return
	}
	rest.RenderJSON(w, rest.JSON{"reloaded": true})
}

// updateApprovedUsersHandler handles POST /users/add and /users/delete requests, it adds or removes users from approved list.
func (s *Server) updateApprovedUsersHandler(updFn func(ui approved.UserInfo) error) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		req := approved.UserInfo{}
		isHtmxRequest := r.Header.Get("HX-Request") == "true"
		if isHtmxRequest {
			req.UserID = r.FormValue("user_id")
			req.UserName = r.FormValue("user_name")
		} else {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				_ = rest.EncodeJSON(w, http.StatusBadRequest, rest.JSON{"error": "can't decode request", "details": err.Error()})
				return
			}
		}

		// try to get userID from request and fallback to userName lookup if it's empty
		if req.UserID == "" {
			req.UserID = strconv.FormatInt(s.Locator.UserIDByName(r.Context(), req.UserName), 10)
		}

		if req.UserID == "" || req.UserID == "0" {
			if isHtmxRequest {
				w.Header().Set("HX-Retarget", "#error-message")
				fmt.Fprintln(w, "<div class='alert alert-danger'>Either userid or valid username required.</div>")
				return
			}
			_ = rest.EncodeJSON(w, http.StatusBadRequest, rest.JSON{"error": "user ID is required"})
			return
		}

		// add or remove user from the approved list of detector
		if err := updFn(req); err != nil {
			_ = rest.EncodeJSON(w, http.StatusInternalServerError,
				rest.JSON{"error": "can't update approved users", "details": err.Error()})
			return
		}

		if isHtmxRequest {
			users := s.Detector.ApprovedUsers()
			tmplData := struct {
				ApprovedUsers      []approved.UserInfo
				TotalApprovedUsers int
			}{
				ApprovedUsers:      users,
				TotalApprovedUsers: len(users),
			}

			if err := tmpl.ExecuteTemplate(w, "users_list", tmplData); err != nil {
				http.Error(w, "Error executing template", http.StatusInternalServerError)
				return
			}

		} else {
			rest.RenderJSON(w, rest.JSON{"updated": true, "user_id": req.UserID, "user_name": req.UserName})
		}
	}
}

// removeApprovedUser is adopter for updateApprovedUsersHandler updFn
func (s *Server) removeApprovedUser(req approved.UserInfo) error {
	if err := s.Detector.RemoveApprovedUser(req.UserID); err != nil {
		return fmt.Errorf("failed to remove approved user %s: %w", req.UserID, err)
	}
	return nil
}

// getApprovedUsersHandler handles GET /users request. It returns list of approved users.
func (s *Server) getApprovedUsersHandler(w http.ResponseWriter, _ *http.Request) {
	rest.RenderJSON(w, rest.JSON{"user_ids": s.Detector.ApprovedUsers()})
}

// getSettingsHandler returns application settings, including the list of available Lua plugins
func (s *Server) getSettingsHandler(w http.ResponseWriter, _ *http.Request) {
	// get the list of available Lua plugins before returning settings
	s.Settings.LuaAvailablePlugins = s.Detector.GetLuaPluginNames()
	rest.RenderJSON(w, s.Settings)
}

// getDictionaryEntriesHandler handles GET /dictionary request. It returns stop phrases and ignored words.
func (s *Server) getDictionaryEntriesHandler(w http.ResponseWriter, r *http.Request) {
	stopPhrases, err := s.Dictionary.Read(r.Context(), storage.DictionaryTypeStopPhrase)
	if err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't get stop phrases", "details": err.Error()})
		return
	}

	ignoredWords, err := s.Dictionary.Read(r.Context(), storage.DictionaryTypeIgnoredWord)
	if err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't get ignored words", "details": err.Error()})
		return
	}

	rest.RenderJSON(w, rest.JSON{"stop_phrases": stopPhrases, "ignored_words": ignoredWords})
}

// addDictionaryEntryHandler handles POST /dictionary/add request. It adds a stop phrase or ignored word.
func (s *Server) addDictionaryEntryHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type string `json:"type"`
		Data string `json:"data"`
	}

	isHtmxRequest := r.Header.Get("HX-Request") == "true"

	if isHtmxRequest {
		req.Type = r.FormValue("type")
		req.Data = r.FormValue("data")
	} else {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			_ = rest.EncodeJSON(w, http.StatusBadRequest, rest.JSON{"error": "can't decode request", "details": err.Error()})
			return
		}
	}

	if req.Data == "" {
		if isHtmxRequest {
			w.Header().Set("HX-Retarget", "#error-message")
			fmt.Fprintln(w, "<div class='alert alert-danger'>Data cannot be empty.</div>")
			return
		}
		_ = rest.EncodeJSON(w, http.StatusBadRequest, rest.JSON{"error": "data cannot be empty"})
		return
	}

	dictType := storage.DictionaryType(req.Type)
	if err := dictType.Validate(); err != nil {
		if isHtmxRequest {
			w.Header().Set("HX-Retarget", "#error-message")
			fmt.Fprintf(w, "<div class='alert alert-danger'>Invalid type: %v</div>", err)
			return
		}
		_ = rest.EncodeJSON(w, http.StatusBadRequest, rest.JSON{"error": "invalid type", "details": err.Error()})
		return
	}

	if err := s.Dictionary.Add(r.Context(), dictType, req.Data); err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't add entry", "details": err.Error()})
		return
	}

	// reload samples to apply dictionary changes immediately
	if err := s.SpamFilter.ReloadSamples(); err != nil {
		log.Printf("[WARN] failed to reload samples after dictionary add: %v", err)
		if !isHtmxRequest {
			_ = rest.EncodeJSON(w, http.StatusInternalServerError,
				rest.JSON{"error": "entry added but reload failed", "details": err.Error()})
			return
		}
		// for HTMX, log but continue rendering (entry was added successfully)
	}

	if isHtmxRequest {
		s.renderDictionary(r.Context(), w, "dictionary_list")
	} else {
		rest.RenderJSON(w, rest.JSON{"added": true, "type": req.Type, "data": req.Data})
	}
}

// deleteDictionaryEntryHandler handles POST /dictionary/delete request. It deletes an entry by data.
func (s *Server) deleteDictionaryEntryHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID int64 `json:"id"`
	}

	isHtmxRequest := r.Header.Get("HX-Request") == "true"

	if isHtmxRequest {
		idStr := r.FormValue("id")
		var err error
		req.ID, err = strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			w.Header().Set("HX-Retarget", "#error-message")
			fmt.Fprintf(w, "<div class='alert alert-danger'>Invalid ID: %v</div>", err)
			return
		}
	} else {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			_ = rest.EncodeJSON(w, http.StatusBadRequest, rest.JSON{"error": "can't decode request", "details": err.Error()})
			return
		}
	}

	if err := s.Dictionary.Delete(r.Context(), req.ID); err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't delete entry", "details": err.Error()})
		return
	}

	// reload samples to apply dictionary changes immediately
	if err := s.SpamFilter.ReloadSamples(); err != nil {
		log.Printf("[WARN] failed to reload samples after dictionary delete: %v", err)
		if !isHtmxRequest {
			_ = rest.EncodeJSON(w, http.StatusInternalServerError,
				rest.JSON{"error": "entry deleted but reload failed", "details": err.Error()})
			return
		}
		// for HTMX, log but continue rendering (entry was deleted successfully)
	}

	if isHtmxRequest {
		s.renderDictionary(r.Context(), w, "dictionary_list")
	} else {
		rest.RenderJSON(w, rest.JSON{"deleted": true, "id": req.ID})
	}
}

// htmlSpamCheckHandler handles GET / request.
// It returns rendered spam_check.html template with all the components.
func (s *Server) htmlSpamCheckHandler(w http.ResponseWriter, _ *http.Request) {
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
	s.renderSamples(w, "manage_samples.html")
}

func (s *Server) htmlManageUsersHandler(w http.ResponseWriter, _ *http.Request) {
	users := s.Detector.ApprovedUsers()
	tmplData := struct {
		ApprovedUsers      []approved.UserInfo
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

func (s *Server) htmlManageDictionaryHandler(w http.ResponseWriter, r *http.Request) {
	s.renderDictionary(r.Context(), w, "manage_dictionary.html")
}

func (s *Server) htmlDetectedSpamHandler(w http.ResponseWriter, r *http.Request) {
	ds, err := s.DetectedSpam.Read(r.Context())
	if err != nil {
		log.Printf("[ERROR] Failed to fetch detected spam: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// clean up detected spam entries
	for i, d := range ds {
		d.Text = strings.ReplaceAll(d.Text, "'", " ")
		d.Text = strings.ReplaceAll(d.Text, "\n", " ")
		d.Text = strings.ReplaceAll(d.Text, "\r", " ")
		d.Text = strings.ReplaceAll(d.Text, "\t", " ")
		d.Text = strings.ReplaceAll(d.Text, "\"", " ")
		d.Text = strings.ReplaceAll(d.Text, "\\", " ")
		ds[i] = d
	}

	// get filter from query param, default to "all"
	filter := r.URL.Query().Get("filter")
	if filter == "" {
		filter = "all"
	}

	// apply filtering
	var filteredDS []storage.DetectedSpamInfo
	switch filter {
	case "non-classified":
		for _, entry := range ds {
			hasClassifierHam := false
			for _, check := range entry.Checks {
				if check.Name == "classifier" && !check.Spam {
					hasClassifierHam = true
					break
				}
			}
			if hasClassifierHam {
				filteredDS = append(filteredDS, entry)
			}
		}
	case "openai":
		for _, entry := range ds {
			hasOpenAI := false
			for _, check := range entry.Checks {
				if check.Name == "openai" {
					hasOpenAI = true
					break
				}
			}
			if hasOpenAI {
				filteredDS = append(filteredDS, entry)
			}
		}
	default: // "all" or any other value
		filteredDS = ds
	}

	tmplData := struct {
		DetectedSpamEntries []storage.DetectedSpamInfo
		TotalDetectedSpam   int
		FilteredCount       int
		Filter              string
		OpenAIEnabled       bool
	}{
		DetectedSpamEntries: filteredDS,
		TotalDetectedSpam:   len(ds),
		FilteredCount:       len(filteredDS),
		Filter:              filter,
		OpenAIEnabled:       s.Settings.OpenAIEnabled,
	}

	// if it's an HTMX request, render both content and count display for OOB swap
	if r.Header.Get("HX-Request") == "true" {
		var buf bytes.Buffer

		// first render the content template
		if err := tmpl.ExecuteTemplate(&buf, "detected_spam_content", tmplData); err != nil {
			log.Printf("[WARN] can't execute content template: %v", err)
			http.Error(w, "Error executing template", http.StatusInternalServerError)
			return
		}

		// then append OOB swap for the count display
		countHTML := ""
		if filter != "all" {
			countHTML = fmt.Sprintf("(%d/%d)", len(filteredDS), len(ds))
		} else {
			countHTML = fmt.Sprintf("(%d)", len(ds))
		}

		buf.WriteString(`<span id="count-display" hx-swap-oob="true">` + countHTML + `</span>`)

		// write the combined response
		if _, err := buf.WriteTo(w); err != nil {
			log.Printf("[WARN] failed to write response: %v", err)
		}
		return
	}

	// full page render for normal requests
	if err := tmpl.ExecuteTemplate(w, "detected_spam.html", tmplData); err != nil {
		log.Printf("[WARN] can't execute template: %v", err)
		http.Error(w, "Error executing template", http.StatusInternalServerError)
		return
	}
}

func (s *Server) htmlAddDetectedSpamHandler(w http.ResponseWriter, r *http.Request) {
	reportErr := func(err error, _ int) {
		w.Header().Set("HX-Retarget", "#error-message")
		fmt.Fprintf(w, "<div class='alert alert-danger'>%s</div>", err)
	}
	msg := r.FormValue("msg")

	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || msg == "" {
		log.Printf("[WARN] bad request: %v", err)
		reportErr(fmt.Errorf("bad request: %v", err), http.StatusBadRequest)
		return
	}

	if err := s.SpamFilter.UpdateSpam(msg); err != nil {
		log.Printf("[WARN] failed to update spam samples: %v", err)
		reportErr(fmt.Errorf("can't update spam samples: %v", err), http.StatusInternalServerError)
		return

	}
	if err := s.DetectedSpam.SetAddedToSamplesFlag(r.Context(), id); err != nil {
		log.Printf("[WARN] failed to update detected spam: %v", err)
		reportErr(fmt.Errorf("can't update detected spam: %v", err), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) htmlSettingsHandler(w http.ResponseWriter, _ *http.Request) {
	// get database information if StorageEngine is available
	var dbInfo struct {
		DatabaseType   string `json:"database_type"`
		GID            string `json:"gid"`
		DatabaseStatus string `json:"database_status"`
	}

	if s.StorageEngine != nil {
		// try to cast to SQL engine to get type information
		if sqlEngine, ok := s.StorageEngine.(*engine.SQL); ok {
			dbInfo.DatabaseType = string(sqlEngine.Type())
			dbInfo.GID = sqlEngine.GID()
			dbInfo.DatabaseStatus = "Connected"
		} else {
			dbInfo.DatabaseType = "Unknown"
			dbInfo.DatabaseStatus = "Connected (unknown type)"
		}
	} else {
		dbInfo.DatabaseStatus = "Not connected"
	}

	// get backup information
	backupURL := "/download/backup"
	backupFilename := fmt.Sprintf("tg-spam-backup-%s-%s.sql.gz", dbInfo.DatabaseType, time.Now().Format("20060102-150405"))

	// get system info - uptime since server start
	uptime := time.Since(startTime)

	// get the list of available Lua plugins
	s.Settings.LuaAvailablePlugins = s.Detector.GetLuaPluginNames()

	data := struct {
		Settings
		Version  string
		Database struct {
			Type   string
			GID    string
			Status string
		}
		Backup struct {
			URL      string
			Filename string
		}
		System struct {
			Uptime string
		}
	}{
		Settings: s.Settings,
		Version:  s.Version,
		Database: struct {
			Type   string
			GID    string
			Status string
		}{
			Type:   dbInfo.DatabaseType,
			GID:    dbInfo.GID,
			Status: dbInfo.DatabaseStatus,
		},
		Backup: struct {
			URL      string
			Filename string
		}{
			URL:      backupURL,
			Filename: backupFilename,
		},
		System: struct {
			Uptime string
		}{
			Uptime: formatDuration(uptime),
		},
	}

	if err := tmpl.ExecuteTemplate(w, "settings.html", data); err != nil {
		log.Printf("[WARN] can't execute template: %v", err)
		http.Error(w, "Error executing template", http.StatusInternalServerError)
		return
	}
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}

	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}

	return fmt.Sprintf("%dm", minutes)
}

func (s *Server) downloadDetectedSpamHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	spam, err := s.DetectedSpam.Read(ctx)
	if err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't get detected spam", "details": err.Error()})
		return
	}

	type jsonSpamInfo struct {
		ID        int64                `json:"id"`
		GID       string               `json:"gid"`
		Text      string               `json:"text"`
		UserID    int64                `json:"user_id"`
		UserName  string               `json:"user_name"`
		Timestamp time.Time            `json:"timestamp"`
		Added     bool                 `json:"added"`
		Checks    []spamcheck.Response `json:"checks"`
	}

	// convert entries to jsonl format with lowercase fields
	lines := make([]string, 0, len(spam))
	for _, entry := range spam {
		data, err := json.Marshal(jsonSpamInfo{
			ID:        entry.ID,
			GID:       entry.GID,
			Text:      entry.Text,
			UserID:    entry.UserID,
			UserName:  entry.UserName,
			Timestamp: entry.Timestamp,
			Added:     entry.Added,
			Checks:    entry.Checks,
		})
		if err != nil {
			_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't marshal entry", "details": err.Error()})
			return
		}
		lines = append(lines, string(data))
	}

	body := strings.Join(lines, "\n")
	w.Header().Set("Content-Type", "application/x-jsonlines")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "detected_spam.jsonl"))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(body))
}

// downloadBackupHandler streams a database backup as an SQL file with gzip compression
// Files are always compressed and always have .gz extension to ensure consistency
func (s *Server) downloadBackupHandler(w http.ResponseWriter, r *http.Request) {
	if s.StorageEngine == nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "storage engine not available"})
		return
	}

	// set filename based on database type and timestamp
	dbType := "db"
	sqlEng, ok := s.StorageEngine.(*engine.SQL)
	if ok {
		dbType = string(sqlEng.Type())
	}
	timestamp := time.Now().Format("20060102-150405")

	// always use a .gz extension as the content is always compressed
	filename := fmt.Sprintf("tg-spam-backup-%s-%s.sql.gz", dbType, timestamp)

	// set headers for file download - note we're using application/octet-stream
	// instead of application/sql to prevent browsers from trying to interpret the file
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// create a gzip writer that streams to response
	gzipWriter := gzip.NewWriter(w)
	defer func() {
		if err := gzipWriter.Close(); err != nil {
			log.Printf("[ERROR] failed to close gzip writer: %v", err)
		}
	}()

	// stream backup directly to response through gzip
	if err := s.StorageEngine.Backup(r.Context(), gzipWriter); err != nil {
		log.Printf("[ERROR] failed to create backup: %v", err)
		// we've already started writing the response, so we can't send a proper error response
		return
	}

	// flush the gzip writer to ensure all data is written
	if err := gzipWriter.Flush(); err != nil {
		log.Printf("[ERROR] failed to flush gzip writer: %v", err)
	}
}

// downloadExportToPostgresHandler streams a PostgreSQL-compatible export from a SQLite database
func (s *Server) downloadExportToPostgresHandler(w http.ResponseWriter, r *http.Request) {
	if s.StorageEngine == nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "storage engine not available"})
		return
	}

	// check if the database is SQLite
	if s.StorageEngine.Type() != engine.Sqlite {
		_ = rest.EncodeJSON(w, http.StatusBadRequest, rest.JSON{"error": "source database must be SQLite"})
		return
	}

	// set filename based on timestamp
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("tg-spam-sqlite-to-postgres-%s.sql.gz", timestamp)

	// set headers for file download
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// create a gzip writer that streams to response
	gzipWriter := gzip.NewWriter(w)
	defer func() {
		if err := gzipWriter.Close(); err != nil {
			log.Printf("[ERROR] failed to close gzip writer: %v", err)
		}
	}()

	// stream export directly to response through gzip
	if err := s.StorageEngine.BackupSqliteAsPostgres(r.Context(), gzipWriter); err != nil {
		log.Printf("[ERROR] failed to create export: %v", err)
		// we've already started writing the response, so we can't send a proper error response
		return
	}

	// flush the gzip writer to ensure all data is written
	if err := gzipWriter.Flush(); err != nil {
		log.Printf("[ERROR] failed to flush gzip writer: %v", err)
	}
}

func (s *Server) renderSamples(w http.ResponseWriter, tmplName string) {
	spam, ham, err := s.SpamFilter.DynamicSamples()
	if err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't fetch samples", "details": err.Error()})
		return
	}

	spam, ham = s.reverseSamples(spam, ham)

	type smpleWithID struct {
		ID     string
		Sample string
	}

	makeID := func(s string) string {
		hash := sha1.New() //nolint
		if _, err := hash.Write([]byte(s)); err != nil {
			return fmt.Sprintf("%x", s)
		}
		return fmt.Sprintf("%x", hash.Sum(nil))
	}

	tmplData := struct {
		SpamSamples      []smpleWithID
		HamSamples       []smpleWithID
		TotalHamSamples  int
		TotalSpamSamples int
	}{
		TotalHamSamples:  len(ham),
		TotalSpamSamples: len(spam),
	}
	for _, s := range spam {
		tmplData.SpamSamples = append(tmplData.SpamSamples, smpleWithID{ID: makeID(s), Sample: s})
	}
	for _, h := range ham {
		tmplData.HamSamples = append(tmplData.HamSamples, smpleWithID{ID: makeID(h), Sample: h})
	}

	if err := tmpl.ExecuteTemplate(w, tmplName, tmplData); err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't execute template", "details": err.Error()})
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

// renderDictionary renders dictionary entries for HTMX or full page request
func (s *Server) renderDictionary(ctx context.Context, w http.ResponseWriter, tmplName string) {
	stopPhrases, err := s.Dictionary.ReadWithIDs(ctx, storage.DictionaryTypeStopPhrase)
	if err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't fetch stop phrases", "details": err.Error()})
		return
	}

	ignoredWords, err := s.Dictionary.ReadWithIDs(ctx, storage.DictionaryTypeIgnoredWord)
	if err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't fetch ignored words", "details": err.Error()})
		return
	}

	tmplData := struct {
		StopPhrases       []storage.DictionaryEntry
		IgnoredWords      []storage.DictionaryEntry
		TotalStopPhrases  int
		TotalIgnoredWords int
	}{
		StopPhrases:       stopPhrases,
		IgnoredWords:      ignoredWords,
		TotalStopPhrases:  len(stopPhrases),
		TotalIgnoredWords: len(ignoredWords),
	}

	if err := tmpl.ExecuteTemplate(w, tmplName, tmplData); err != nil {
		_ = rest.EncodeJSON(w, http.StatusInternalServerError, rest.JSON{"error": "can't execute template", "details": err.Error()})
		return
	}
}

// staticFS is a filtered filesystem that only exposes specific static files
type staticFS struct {
	fs        fs.FS
	urlToPath map[string]string
}

// staticFileMapping defines a mapping between URL path and filesystem path
type staticFileMapping struct {
	urlPath     string
	filesysPath string
}

func newStaticFS(fsys fs.FS, files ...staticFileMapping) *staticFS {
	urlToPath := make(map[string]string)
	for _, f := range files {
		urlToPath[f.urlPath] = f.filesysPath
	}

	return &staticFS{
		fs:        fsys,
		urlToPath: urlToPath,
	}
}

func (sfs *staticFS) Open(name string) (fs.File, error) {
	cleanName := path.Clean("/" + name)[1:]

	fsPath, ok := sfs.urlToPath[cleanName]
	if !ok {
		return nil, fs.ErrNotExist
	}

	file, err := sfs.fs.Open(fsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open static file %s: %w", fsPath, err)
	}
	return file, nil
}

// GenerateRandomPassword generates a random password of a given length
func GenerateRandomPassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*()_+"
	const charsetLen = int64(len(charset))

	result := make([]byte, length)
	for i := range length {
		n, err := rand.Int(rand.Reader, big.NewInt(charsetLen))
		if err != nil {
			return "", fmt.Errorf("failed to generate random number: %w", err)
		}
		result[i] = charset[n.Int64()]
	}
	return string(result), nil
}
