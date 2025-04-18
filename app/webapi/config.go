package webapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	log "github.com/go-pkgz/lgr"
	"github.com/go-pkgz/rest"

	"github.com/umputun/tg-spam/app/config"
)

//go:generate moq --out config_store_mock.go --with-resets --skip-ensure . SettingsStore

// SettingsStore provides access to configuration stored in database
type SettingsStore interface {
	Load(ctx context.Context) (*config.Settings, error)
	Save(ctx context.Context, settings *config.Settings) error
	Delete(ctx context.Context) error
	LastUpdated(ctx context.Context) (time.Time, error)
	Exists(ctx context.Context) (bool, error)
}

// saveConfigHandler handles POST /config request.
// It saves the current configuration to the database.
func (s *Server) saveConfigHandler(w http.ResponseWriter, r *http.Request) {
	if s.SettingsStore == nil {
		http.Error(w, "Configuration storage not available", http.StatusInternalServerError)
		return
	}

	// save current settings to database
	err := s.SettingsStore.Save(r.Context(), s.AppSettings)
	if err != nil {
		log.Printf("[ERROR] failed to save configuration: %v", err)
		http.Error(w, fmt.Sprintf("Failed to save configuration: %v", err), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		// return a success message for HTMX
		if _, err := w.Write([]byte(`<div class="alert alert-success">Configuration saved successfully</div>`)); err != nil {
			log.Printf("[ERROR] failed to write response: %v", err)
		}
		return
	}

	// return JSON response for API calls
	rest.RenderJSON(w, rest.JSON{"status": "ok", "message": "Configuration saved successfully"})
}

// loadConfigHandler handles GET /config request.
// It loads configuration from the database.
func (s *Server) loadConfigHandler(w http.ResponseWriter, r *http.Request) {
	if s.SettingsStore == nil {
		http.Error(w, "Configuration storage not available", http.StatusInternalServerError)
		return
	}

	// load settings from database
	settings, err := s.SettingsStore.Load(r.Context())
	if err != nil {
		log.Printf("[ERROR] failed to load configuration: %v", err)
		http.Error(w, fmt.Sprintf("Failed to load configuration: %v", err), http.StatusInternalServerError)
		return
	}

	// preserve CLI-provided credentials and transient settings
	// CLI credentials have precedence over database values if provided
	transient := s.AppSettings.Transient
	telegramToken := s.AppSettings.Telegram.Token
	openAIToken := s.AppSettings.OpenAI.Token
	webAuthHash := s.AppSettings.Server.AuthHash
	webAuthPasswd := s.AppSettings.Transient.WebAuthPasswd

	// restore transient values
	settings.Transient = transient

	// restore CLI-provided credentials if they were set
	// these override database values because CLI parameters take precedence
	if telegramToken != "" {
		settings.Telegram.Token = telegramToken
	}
	if openAIToken != "" {
		settings.OpenAI.Token = openAIToken
	}
	if webAuthHash != "" {
		settings.Server.AuthHash = webAuthHash
	}
	if webAuthPasswd != "" {
		settings.Transient.WebAuthPasswd = webAuthPasswd
	}

	// update current settings
	s.AppSettings = settings

	if r.Header.Get("HX-Request") == "true" {
		// return a success message for HTMX with reload
		w.Header().Set("HX-Refresh", "true") // force page reload to reflect new settings
		if _, err := w.Write([]byte(`<div class="alert alert-success">Configuration loaded successfully. Refreshing page...</div>`)); err != nil {
			log.Printf("[ERROR] failed to write response: %v", err)
		}
		return
	}

	// return JSON response for API calls
	rest.RenderJSON(w, rest.JSON{"status": "ok", "message": "Configuration loaded successfully"})
}

// updateConfigHandler handles PUT /config request.
// It updates specific configuration settings in memory and optionally saves to database.
func (s *Server) updateConfigHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse form: %v", err), http.StatusBadRequest)
		return
	}

	// update settings based on form values - auth settings are never modified
	updateSettingsFromForm(s.AppSettings, r)

	// save changes to database if requested
	if r.FormValue("saveToDb") == "true" && s.SettingsStore != nil {
		err := s.SettingsStore.Save(r.Context(), s.AppSettings)
		if err != nil {
			log.Printf("[ERROR] failed to save updated configuration: %v", err)
			http.Error(w, fmt.Sprintf("Failed to save configuration: %v", err), http.StatusInternalServerError)
			return
		}
	}

	if r.Header.Get("HX-Request") == "true" {
		// return a success message for HTMX
		if _, err := w.Write([]byte(`<div class="alert alert-success">Configuration updated successfully</div>`)); err != nil {
			log.Printf("[ERROR] failed to write response: %v", err)
		}
		return
	}

	// return JSON response for API calls
	rest.RenderJSON(w, rest.JSON{"status": "ok", "message": "Configuration updated successfully"})
}

// deleteConfigHandler handles DELETE /config request.
// It deletes the saved configuration from the database.
func (s *Server) deleteConfigHandler(w http.ResponseWriter, r *http.Request) {
	if s.SettingsStore == nil {
		http.Error(w, "Configuration storage not available", http.StatusInternalServerError)
		return
	}

	// delete configuration from database
	err := s.SettingsStore.Delete(r.Context())
	if err != nil {
		log.Printf("[ERROR] failed to delete configuration: %v", err)
		http.Error(w, fmt.Sprintf("Failed to delete configuration: %v", err), http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		// return a success message for HTMX
		if _, err := w.Write([]byte(`<div class="alert alert-success">Configuration deleted successfully</div>`)); err != nil {
			log.Printf("[ERROR] failed to write response: %v", err)
		}
		return
	}

	// return JSON response for API calls
	rest.RenderJSON(w, rest.JSON{"status": "ok", "message": "Configuration deleted successfully"})
}

// updateSettingsFromForm updates settings from form values
func updateSettingsFromForm(settings *config.Settings, r *http.Request) {
	// general settings
	if val := r.FormValue("primaryGroup"); val != "" {
		settings.Telegram.Group = val
	}
	if val := r.FormValue("adminGroup"); val != "" {
		settings.Admin.AdminGroup = val
	}
	settings.Admin.DisableAdminSpamForward = r.FormValue("disableAdminSpamForward") == "on"
	settings.Logger.Enabled = r.FormValue("loggerEnabled") == "on"
	settings.NoSpamReply = r.FormValue("noSpamReply") == "on"

	// handle CasEnabled separately because we need to set CAS.API
	casEnabled := r.FormValue("casEnabled") == "on"
	if casEnabled && settings.CAS.API == "" {
		settings.CAS.API = "https://api.cas.chat" // default CAS API endpoint
	} else if !casEnabled {
		settings.CAS.API = ""
	}

	// parse super users from comma-separated string
	if superUsers := r.FormValue("superUsers"); superUsers != "" {
		users := strings.Split(superUsers, ",")
		settings.Admin.SuperUsers = make([]string, 0, len(users))
		for _, user := range users {
			trimmed := strings.TrimSpace(user)
			if trimmed != "" {
				settings.Admin.SuperUsers = append(settings.Admin.SuperUsers, trimmed)
			}
		}
	}

	// meta checks
	metaEnabled := r.FormValue("metaEnabled") == "on"

	if val := r.FormValue("metaLinksLimit"); val != "" {
		if limit, err := strconv.Atoi(val); err == nil {
			settings.Meta.LinksLimit = limit
		}
	} else if !metaEnabled {
		settings.Meta.LinksLimit = -1
	}

	if val := r.FormValue("metaMentionsLimit"); val != "" {
		if limit, err := strconv.Atoi(val); err == nil {
			settings.Meta.MentionsLimit = limit
		}
	} else if !metaEnabled {
		settings.Meta.MentionsLimit = -1
	}

	settings.Meta.LinksOnly = r.FormValue("metaLinksOnly") == "on"
	settings.Meta.ImageOnly = r.FormValue("metaImageOnly") == "on"
	settings.Meta.VideosOnly = r.FormValue("metaVideoOnly") == "on"
	settings.Meta.AudiosOnly = r.FormValue("metaAudioOnly") == "on"
	settings.Meta.Forward = r.FormValue("metaForwarded") == "on"
	settings.Meta.Keyboard = r.FormValue("metaKeyboard") == "on"

	if val := r.FormValue("metaUsernameSymbols"); val != "" {
		settings.Meta.UsernameSymbols = val
	} else if !metaEnabled {
		settings.Meta.UsernameSymbols = ""
	}

	// openAI settings
	openAIEnabled := r.FormValue("openAIEnabled") == "on"
	if !openAIEnabled {
		settings.OpenAI.APIBase = ""
	}

	settings.OpenAI.Veto = r.FormValue("openAIVeto") == "on"

	if val := r.FormValue("openAIHistorySize"); val != "" {
		if size, err := strconv.Atoi(val); err == nil {
			settings.OpenAI.HistorySize = size
		}
	}

	if val := r.FormValue("openAIModel"); val != "" {
		settings.OpenAI.Model = val
	}

	// lua plugins
	settings.LuaPlugins.Enabled = r.FormValue("luaPluginsEnabled") == "on"
	settings.LuaPlugins.DynamicReload = r.FormValue("luaDynamicReload") == "on"

	if val := r.FormValue("luaPluginsDir"); val != "" {
		settings.LuaPlugins.PluginsDir = val
	}

	// get selected Lua plugins
	settings.LuaPlugins.EnabledPlugins = r.Form["luaEnabledPlugins"]

	// spam detection
	if val := r.FormValue("similarityThreshold"); val != "" {
		if threshold, err := strconv.ParseFloat(val, 64); err == nil {
			settings.SimilarityThreshold = threshold
		}
	}

	if val := r.FormValue("minMsgLen"); val != "" {
		if msgLen, err := strconv.Atoi(val); err == nil {
			settings.MinMsgLen = msgLen
		}
	}

	if val := r.FormValue("maxEmoji"); val != "" {
		if count, err := strconv.Atoi(val); err == nil {
			settings.MaxEmoji = count
		}
	}

	if val := r.FormValue("minSpamProbability"); val != "" {
		if prob, err := strconv.ParseFloat(val, 64); err == nil {
			settings.MinSpamProbability = prob
		}
	}

	settings.ParanoidMode = r.FormValue("paranoidMode") == "on"

	if val := r.FormValue("firstMessagesCount"); val != "" {
		if count, err := strconv.Atoi(val); err == nil {
			settings.FirstMessagesCount = count
		}
	}

	// startupMessageEnabled controls Message.Startup
	startupMessageEnabled := r.FormValue("startupMessageEnabled") == "on"
	if startupMessageEnabled && settings.Message.Startup == "" {
		settings.Message.Startup = "Bot started"
	} else if !startupMessageEnabled {
		settings.Message.Startup = ""
	}

	settings.Training = r.FormValue("trainingEnabled") == "on"
	settings.SoftBan = r.FormValue("softBanEnabled") == "on"
	settings.AbnormalSpace.Enabled = r.FormValue("abnormalSpacingEnabled") == "on"

	if val := r.FormValue("multiLangLimit"); val != "" {
		if limit, err := strconv.Atoi(val); err == nil {
			settings.MultiLangWords = limit
		}
	}

	if val := r.FormValue("historySize"); val != "" {
		if size, err := strconv.Atoi(val); err == nil {
			settings.History.Size = size
		}
	}

	// data storage
	if val := r.FormValue("samplesDataPath"); val != "" {
		settings.Files.SamplesDataPath = val
	}

	if val := r.FormValue("dynamicDataPath"); val != "" {
		settings.Files.DynamicDataPath = val
	}

	if val := r.FormValue("watchIntervalSecs"); val != "" {
		if secs, err := strconv.Atoi(val); err == nil {
			settings.Files.WatchInterval = secs
		}
	}

	// debug modes - they're primarily CLI settings but we still update them here
	settings.Transient.Dbg = r.FormValue("debugModeEnabled") == "on"
	settings.Dry = r.FormValue("dryModeEnabled") == "on"
	settings.Transient.TGDbg = r.FormValue("tgDebugModeEnabled") == "on"
}
