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
)

// ConfigStoreInterface provides access to configuration stored in database
type ConfigStoreInterface interface {
	Get(ctx context.Context) (string, error)
	GetObject(ctx context.Context, obj *Settings) error
	Set(ctx context.Context, data string) error
	SetObject(ctx context.Context, obj *Settings) error
	Delete(ctx context.Context) error
	LastUpdated(ctx context.Context) (time.Time, error)
}

// saveConfigHandler handles POST /config/save request.
// It saves the current configuration to the database.
func (s *Server) saveConfigHandler(w http.ResponseWriter, r *http.Request) {
	if s.ConfigStore == nil {
		http.Error(w, "Configuration storage not available", http.StatusInternalServerError)
		return
	}

	// important: we don't allow storing auth credentials in the database
	// make a copy of settings to avoid modifying the original
	settings := s.Settings

	// save current settings to database
	err := s.ConfigStore.SetObject(r.Context(), &settings)
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

// loadConfigHandler handles POST /config/load request.
// It loads configuration from the database.
func (s *Server) loadConfigHandler(w http.ResponseWriter, r *http.Request) {
	if s.ConfigStore == nil {
		http.Error(w, "Configuration storage not available", http.StatusInternalServerError)
		return
	}

	// load settings from database
	settings := s.Settings // copy current settings to preserve structure
	err := s.ConfigStore.GetObject(r.Context(), &settings)
	if err != nil {
		log.Printf("[ERROR] failed to load configuration: %v", err)
		http.Error(w, fmt.Sprintf("Failed to load configuration: %v", err), http.StatusInternalServerError)
		return
	}

	// update current settings
	s.Settings = settings

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

	// create a copy of the current settings
	settings := s.Settings

	// update settings based on form values - auth settings are never modified
	updateSettingsFromForm(&settings, r)

	// save changes to database if requested
	if r.FormValue("saveToDb") == "true" && s.ConfigStore != nil {
		err := s.ConfigStore.SetObject(r.Context(), &settings)
		if err != nil {
			log.Printf("[ERROR] failed to save updated configuration: %v", err)
			http.Error(w, fmt.Sprintf("Failed to save configuration: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// update current settings
	s.Settings = settings

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
	if s.ConfigStore == nil {
		http.Error(w, "Configuration storage not available", http.StatusInternalServerError)
		return
	}

	// delete configuration from database
	err := s.ConfigStore.Delete(r.Context())
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
func updateSettingsFromForm(settings *Settings, r *http.Request) {
	// general settings
	if val := r.FormValue("primaryGroup"); val != "" {
		settings.PrimaryGroup = val
	}
	if val := r.FormValue("adminGroup"); val != "" {
		settings.AdminGroup = val
	}
	settings.DisableAdminSpamForward = r.FormValue("disableAdminSpamForward") == "on"
	settings.LoggerEnabled = r.FormValue("loggerEnabled") == "on"
	settings.NoSpamReply = r.FormValue("noSpamReply") == "on"
	settings.CasEnabled = r.FormValue("casEnabled") == "on"

	// parse super users from comma-separated string
	if superUsers := r.FormValue("superUsers"); superUsers != "" {
		users := strings.Split(superUsers, ",")
		settings.SuperUsers = make([]string, 0, len(users))
		for _, user := range users {
			trimmed := strings.TrimSpace(user)
			if trimmed != "" {
				settings.SuperUsers = append(settings.SuperUsers, trimmed)
			}
		}
	}

	// meta checks
	settings.MetaEnabled = r.FormValue("metaEnabled") == "on"
	if val := r.FormValue("metaLinksLimit"); val != "" {
		if limit, err := strconv.Atoi(val); err == nil {
			settings.MetaLinksLimit = limit
		}
	}
	if val := r.FormValue("metaMentionsLimit"); val != "" {
		if limit, err := strconv.Atoi(val); err == nil {
			settings.MetaMentionsLimit = limit
		}
	}
	settings.MetaLinksOnly = r.FormValue("metaLinksOnly") == "on"
	settings.MetaImageOnly = r.FormValue("metaImageOnly") == "on"
	settings.MetaVideoOnly = r.FormValue("metaVideoOnly") == "on"
	settings.MetaAudioOnly = r.FormValue("metaAudioOnly") == "on"
	settings.MetaForwarded = r.FormValue("metaForwarded") == "on"
	settings.MetaKeyboard = r.FormValue("metaKeyboard") == "on"
	if val := r.FormValue("metaUsernameSymbols"); val != "" {
		settings.MetaUsernameSymbols = val
	}

	// openAI settings
	settings.OpenAIEnabled = r.FormValue("openAIEnabled") == "on"
	settings.OpenAIVeto = r.FormValue("openAIVeto") == "on"
	if val := r.FormValue("openAIHistorySize"); val != "" {
		if size, err := strconv.Atoi(val); err == nil {
			settings.OpenAIHistorySize = size
		}
	}
	if val := r.FormValue("openAIModel"); val != "" {
		settings.OpenAIModel = val
	}

	// lua plugins
	settings.LuaPluginsEnabled = r.FormValue("luaPluginsEnabled") == "on"
	settings.LuaDynamicReload = r.FormValue("luaDynamicReload") == "on"
	if val := r.FormValue("luaPluginsDir"); val != "" {
		settings.LuaPluginsDir = val
	}

	// get selected Lua plugins
	settings.LuaEnabledPlugins = r.Form["luaEnabledPlugins"]

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
	settings.StartupMessageEnabled = r.FormValue("startupMessageEnabled") == "on"
	settings.TrainingEnabled = r.FormValue("trainingEnabled") == "on"
	settings.SoftBanEnabled = r.FormValue("softBanEnabled") == "on"
	settings.AbnormalSpacingEnabled = r.FormValue("abnormalSpacingEnabled") == "on"
	if val := r.FormValue("multiLangLimit"); val != "" {
		if limit, err := strconv.Atoi(val); err == nil {
			settings.MultiLangLimit = limit
		}
	}
	if val := r.FormValue("historySize"); val != "" {
		if size, err := strconv.Atoi(val); err == nil {
			settings.HistorySize = size
		}
	}

	// data storage
	if val := r.FormValue("samplesDataPath"); val != "" {
		settings.SamplesDataPath = val
	}
	if val := r.FormValue("dynamicDataPath"); val != "" {
		settings.DynamicDataPath = val
	}
	if val := r.FormValue("watchIntervalSecs"); val != "" {
		if secs, err := strconv.Atoi(val); err == nil {
			settings.WatchIntervalSecs = secs
		}
	}
	if val := r.FormValue("storageTimeout"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			settings.StorageTimeout = duration
		}
	}

	// debug modes
	settings.DebugModeEnabled = r.FormValue("debugModeEnabled") == "on"
	settings.DryModeEnabled = r.FormValue("dryModeEnabled") == "on"
	settings.TGDebugModeEnabled = r.FormValue("tgDebugModeEnabled") == "on"
}
