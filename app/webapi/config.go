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

//go:generate moq --out mocks/settings_store.go --pkg mocks --with-resets --skip-ensure . SettingsStore

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

	// save current settings to database; hold the read lock across the call so
	// concurrent mutations don't race with JSON encoding in the store
	s.appSettingsMu.RLock()
	err := s.SettingsStore.Save(r.Context(), s.AppSettings)
	s.appSettingsMu.RUnlock()
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

// loadConfigHandler handles POST /config/reload request.
// It reloads configuration from the database, replacing in-memory settings.
// This is a state-changing action so it must use a non-safe HTTP method:
// Go's cross-origin protection middleware treats GET/HEAD/OPTIONS as safe
// and lets them through unchecked, which would expose the reload to CSRF.
func (s *Server) loadConfigHandler(w http.ResponseWriter, r *http.Request) {
	if s.SettingsStore == nil {
		http.Error(w, "Configuration storage not available", http.StatusInternalServerError)
		return
	}

	// hold the write lock across the DB read and memory swap so a concurrent
	// updateConfigHandler can't commit a newer snapshot between our Load and
	// the assignment below, which would leave memory stale relative to the DB.
	s.appSettingsMu.Lock()
	settings, err := s.SettingsStore.Load(r.Context())
	if err != nil {
		s.appSettingsMu.Unlock()
		log.Printf("[ERROR] failed to load configuration: %v", err)
		http.Error(w, fmt.Sprintf("Failed to load configuration: %v", err), http.StatusInternalServerError)
		return
	}

	// reapply startup-equivalent normalization: fills any zero fields left by a
	// partial/legacy DB blob from the defaults template and reasserts operator-
	// supplied operational CLI overrides (--files.dynamic, --files.samples,
	// --server.listen, --dry) so reload doesn't silently revert them to DB values.
	// run BEFORE transient/auth preservation so the closure can't accidentally
	// touch in-memory transient state.
	if s.ReloadNormalize != nil {
		s.ReloadNormalize(settings)
	}

	// preserve transient settings (never stored in DB). Tokens are NOT preserved:
	// in --confdb mode the DB is authoritative for Telegram/OpenAI/Gemini tokens,
	// so reload must pick up fresh DB values. Auth hash is preserved only when
	// transient.AuthFromCLI is set, which marks an in-memory hash that must
	// survive reload (set by applyCLIOverrides for explicit --server.auth/-hash
	// flags, and by applyAutoAuthFallback for the auto-generated safety net).
	// when auth originated from the DB, fresh DB values win so external hash
	// rotations are picked up.
	settings.Transient = s.AppSettings.Transient
	if s.AppSettings.Transient.AuthFromCLI {
		settings.Server.AuthHash = s.AppSettings.Server.AuthHash
	}
	s.AppSettings = settings
	s.appSettingsMu.Unlock()

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

	log.Printf("[DEBUG] updateConfigHandler: saveToDb=%s, SettingsStore=%v", r.FormValue("saveToDb"), s.SettingsStore != nil)

	// hold the write lock across form application and optional DB save so the
	// settings struct can't be observed in a partially updated state and so a
	// concurrent loadConfigHandler can't swap the pointer mid-save.
	s.appSettingsMu.Lock()

	// load-bearing: updateSettingsFromForm must replace slices wholesale, never
	// mutate in place. The snapshot below is a value copy that captures slice
	// headers — if a future change adds in-place slice mutation, a failed save
	// would not roll back the slice contents. Enforced by
	// testUpdateSettingsFromForm_NoInPlaceSliceMutation.
	snapshot := *s.AppSettings
	updateSettingsFromForm(s.AppSettings, r)

	// normalize Lua plugin selection: a freshly-rendered settings page checks
	// every plugin when EnabledPlugins is empty (semantic "all enabled"). If
	// the operator hits Save without unchecking anything, the form posts the
	// full list and would freeze auto-enable for future plugins. Collapse
	// "all available selected" back to nil so the empty-list semantic survives.
	if s.Detector != nil {
		s.AppSettings.LuaPlugins.EnabledPlugins = normalizeLuaEnabledPlugins(
			s.AppSettings.LuaPlugins.EnabledPlugins, s.Detector.GetLuaPluginNames())
	}

	saveToDB := r.FormValue("saveToDb") == "true" && s.SettingsStore != nil
	if saveToDB {
		log.Printf("[DEBUG] saving settings to database")
		if err := s.SettingsStore.Save(r.Context(), s.AppSettings); err != nil {
			*s.AppSettings = snapshot // rollback in-memory mutation on save failure
			s.appSettingsMu.Unlock()
			log.Printf("[ERROR] failed to save updated configuration: %v", err)
			http.Error(w, fmt.Sprintf("Failed to save configuration: %v", err), http.StatusInternalServerError)
			return
		}
		log.Printf("[DEBUG] settings saved successfully")
	}
	s.appSettingsMu.Unlock()

	if r.Header.Get("HX-Request") == "true" {
		// wrap the alert in #update-result so the next outerHTML swap finds the
		// same target — without the id, the first save replaces #update-result
		// with a plain alert div and subsequent saves silently no-op
		if _, err := w.Write([]byte(`<div id="update-result" class="alert alert-success">Configuration updated successfully</div>`)); err != nil {
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

// normalizeLuaEnabledPlugins collapses a "all available plugins selected"
// submission back to nil so the semantic "no preference, enable all" stored in
// EnabledPlugins survives a UI round-trip. The settings page renders every
// plugin checkbox as checked when EnabledPlugins is empty; without this
// normalization, hitting Save once freezes the list at the currently-loaded
// set and silently disables auto-enable for plugins added later.
//
// Returns selected unchanged when:
//   - available is empty (no detector or no plugins loaded)
//   - selected is shorter than available (operator unchecked at least one)
//   - selected omits any plugin in available (literal selection differs)
//
// Returns nil only when selected covers every entry in available.
func normalizeLuaEnabledPlugins(selected, available []string) []string {
	if len(available) == 0 || len(selected) < len(available) {
		return selected
	}
	selectedSet := make(map[string]struct{}, len(selected))
	for _, name := range selected {
		selectedSet[name] = struct{}{}
	}
	for _, name := range available {
		if _, ok := selectedSet[name]; !ok {
			return selected
		}
	}
	return nil
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
	switch casEnabled := r.FormValue("casEnabled") == "on"; {
	case !casEnabled:
		settings.CAS.API = ""
	case settings.CAS.API == "":
		settings.CAS.API = "https://api.cas.chat" // default CAS API endpoint
	}

	// parse super users from comma-separated string; only write when the form
	// contains the field so unrelated saves preserve the list, but honor an
	// explicit empty value as "clear all super users"
	if _, ok := r.Form["superUsers"]; ok {
		superUsers := r.FormValue("superUsers")
		if superUsers == "" {
			settings.Admin.SuperUsers = nil
		} else {
			users := strings.Split(superUsers, ",")
			settings.Admin.SuperUsers = make([]string, 0, len(users))
			for _, user := range users {
				trimmed := strings.TrimSpace(user)
				if trimmed != "" {
					settings.Admin.SuperUsers = append(settings.Admin.SuperUsers, trimmed)
				}
			}
		}
	}

	// meta checks: server-side authoritative master toggle. Behavior:
	//   - form contains zero meta-related fields → skip the entire block so
	//     unrelated saves preserve all 11 IsMetaEnabled-contributing fields
	//   - metaEnabled=on → write rendered fields from form; rendered booleans
	//     follow presence-of-on (absent == unchecked == false), unrendered
	//     booleans (metaContactOnly, metaGiveaway) and the optional
	//     metaUsernameSymbols are gated on r.Form presence so submits without
	//     them preserve existing values
	//   - metaEnabled absent → master toggle off, clear ALL 11 fields used by
	//     isMetaEnabled() so a checked per-feature box (e.g., metaImageOnly)
	//     cannot keep meta enabled
	metaFormFields := []string{
		"metaEnabled", "metaLinksLimit", "metaMentionsLimit", "metaUsernameSymbols",
		"metaLinksOnly", "metaImageOnly", "metaVideoOnly", "metaAudioOnly",
		"metaForwarded", "metaKeyboard", "metaContactOnly", "metaGiveaway",
	}
	hasMetaForm := false
	for _, k := range metaFormFields {
		if _, ok := r.Form[k]; ok {
			hasMetaForm = true
			break
		}
	}
	if hasMetaForm {
		if r.FormValue("metaEnabled") == "on" {
			if val := r.FormValue("metaLinksLimit"); val != "" {
				if limit, err := strconv.Atoi(val); err == nil {
					settings.Meta.LinksLimit = limit
				}
			}
			if val := r.FormValue("metaMentionsLimit"); val != "" {
				if limit, err := strconv.Atoi(val); err == nil {
					settings.Meta.MentionsLimit = limit
				}
			}
			// honor the "leave empty to disable" UI hint: clear the setting whenever
			// the form posts an empty value. The field is only written when the form
			// contains it, so submits without it preserve the existing value.
			if _, ok := r.Form["metaUsernameSymbols"]; ok {
				settings.Meta.UsernameSymbols = r.FormValue("metaUsernameSymbols")
			}
			settings.Meta.LinksOnly = r.FormValue("metaLinksOnly") == "on"
			settings.Meta.ImageOnly = r.FormValue("metaImageOnly") == "on"
			settings.Meta.VideosOnly = r.FormValue("metaVideoOnly") == "on"
			settings.Meta.AudiosOnly = r.FormValue("metaAudioOnly") == "on"
			settings.Meta.Forward = r.FormValue("metaForwarded") == "on"
			settings.Meta.Keyboard = r.FormValue("metaKeyboard") == "on"
			// metaContactOnly and metaGiveaway are not currently rendered in the ConfigDB
			// UI form. Gate them behind form presence so saves that don't render them
			// can't silently wipe values set via save-config CLI or external DB tooling.
			if _, ok := r.Form["metaContactOnly"]; ok {
				settings.Meta.ContactOnly = r.FormValue("metaContactOnly") == "on"
			}
			if _, ok := r.Form["metaGiveaway"]; ok {
				settings.Meta.Giveaway = r.FormValue("metaGiveaway") == "on"
			}
		} else {
			// master toggle off — clear EVERY field used by IsMetaEnabled so the
			// returned settings unambiguously satisfy IsMetaEnabled() == false
			settings.Meta.LinksLimit = -1
			settings.Meta.MentionsLimit = -1
			settings.Meta.UsernameSymbols = ""
			settings.Meta.LinksOnly = false
			settings.Meta.ImageOnly = false
			settings.Meta.VideosOnly = false
			settings.Meta.AudiosOnly = false
			settings.Meta.Forward = false
			settings.Meta.Keyboard = false
			settings.Meta.ContactOnly = false
			settings.Meta.Giveaway = false
		}
	}

	// openAI settings. enablement (APIBase/Token) is managed via CLI and save-config
	// to avoid destroying credentials through a UI toggle; only non-credential fields
	// are accepted from the form here, mirroring Gemini's handling.
	settings.OpenAI.Veto = r.FormValue("openAIVeto") == "on"
	settings.OpenAI.CheckShortMessages = r.FormValue("openAICheckShortMessages") == "on"

	if val := r.FormValue("openAIHistorySize"); val != "" {
		if size, err := strconv.Atoi(val); err == nil {
			settings.OpenAI.HistorySize = size
		}
	}

	if val := r.FormValue("openAIModel"); val != "" {
		settings.OpenAI.Model = val
	}

	// gemini settings (mirror openAI handling; do not touch Gemini.Token from form — credential lives in CLI/DB only)
	settings.Gemini.Veto = r.FormValue("geminiVeto") == "on"
	settings.Gemini.CheckShortMessages = r.FormValue("geminiCheckShortMessages") == "on"

	if val := r.FormValue("geminiHistorySize"); val != "" {
		if size, err := strconv.Atoi(val); err == nil {
			settings.Gemini.HistorySize = size
		}
	}

	if val := r.FormValue("geminiModel"); val != "" {
		settings.Gemini.Model = val
	}

	if val := r.FormValue("geminiPrompt"); val != "" {
		settings.Gemini.Prompt = val
	}

	if val := r.FormValue("geminiMaxTokensResponse"); val != "" {
		if n, err := strconv.ParseInt(val, 10, 32); err == nil {
			settings.Gemini.MaxTokensResponse = int32(n)
		}
	}

	if val := r.FormValue("geminiMaxSymbolsRequest"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			settings.Gemini.MaxSymbolsRequest = n
		}
	}

	if val := r.FormValue("geminiRetryCount"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			settings.Gemini.RetryCount = n
		}
	}

	// llm orchestration settings
	if val := r.FormValue("llmConsensus"); val != "" {
		settings.LLM.Consensus = val
	}

	if val := r.FormValue("llmRequestTimeout"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			settings.LLM.RequestTimeout = d
		}
	}

	// duplicates detector
	if val := r.FormValue("duplicatesThreshold"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			settings.Duplicates.Threshold = n
		}
	}

	if val := r.FormValue("duplicatesWindow"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			settings.Duplicates.Window = d
		}
	}

	// reactions detector. Gated on r.Form key presence (not value-non-empty) so a
	// saved form without the reactions section preserves existing values, while an
	// explicit zero from the operator (disabling the detector) is honored.
	if _, ok := r.Form["reactionsMaxReactions"]; ok {
		if n, err := strconv.Atoi(r.FormValue("reactionsMaxReactions")); err == nil {
			settings.Reactions.MaxReactions = n
		}
	}
	if _, ok := r.Form["reactionsWindow"]; ok {
		if d, err := time.ParseDuration(r.FormValue("reactionsWindow")); err == nil {
			settings.Reactions.Window = d
		}
	}

	// user reports. reportEnabled is not rendered in the ConfigDB UI form, so
	// gate the write on form presence to avoid silently wiping values set via
	// save-config or external DB tooling when saving unrelated changes.
	if _, ok := r.Form["reportEnabled"]; ok {
		settings.Report.Enabled = r.FormValue("reportEnabled") == "on"
	}

	if val := r.FormValue("reportThreshold"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			settings.Report.Threshold = n
		}
	}

	if val := r.FormValue("reportAutoBanThreshold"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			settings.Report.AutoBanThreshold = n
		}
	}

	if val := r.FormValue("reportRateLimit"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			settings.Report.RateLimit = n
		}
	}

	if val := r.FormValue("reportRatePeriod"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			settings.Report.RatePeriod = d
		}
	}

	// warn auto-ban. warnThreshold mirrors reportAutoBanThreshold (parse on non-empty,
	// 0 is a valid "disabled" value the operator may post explicitly). warnWindow
	// mirrors reactionsWindow (gate on r.Form key presence so unrelated saves preserve
	// the existing window).
	if val := r.FormValue("warnThreshold"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			settings.Warn.Threshold = n
		}
	}
	if _, ok := r.Form["warnWindow"]; ok {
		if d, err := time.ParseDuration(r.FormValue("warnWindow")); err == nil {
			settings.Warn.Window = d
		}
	}

	// service-message deletion. These flags are not rendered in the ConfigDB UI
	// form; gate the writes on form presence so unrelated saves don't wipe them.
	if _, ok := r.Form["deleteJoinMessages"]; ok {
		settings.Delete.JoinMessages = r.FormValue("deleteJoinMessages") == "on"
	}
	if _, ok := r.Form["deleteLeaveMessages"]; ok {
		settings.Delete.LeaveMessages = r.FormValue("deleteLeaveMessages") == "on"
	}

	// aggressive cleanup. Flag not rendered in the ConfigDB UI form; gate the
	// write on form presence so unrelated saves don't wipe it.
	if _, ok := r.Form["aggressiveCleanup"]; ok {
		settings.AggressiveCleanup = r.FormValue("aggressiveCleanup") == "on"
	}
	if val := r.FormValue("aggressiveCleanupLimit"); val != "" {
		if n, err := strconv.Atoi(val); err == nil {
			settings.AggressiveCleanupLimit = n
		}
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
	switch startupMessageEnabled := r.FormValue("startupMessageEnabled") == "on"; {
	case !startupMessageEnabled:
		settings.Message.Startup = ""
	case settings.Message.Startup == "":
		settings.Message.Startup = "Bot started"
	}

	settings.Training = r.FormValue("trainingEnabled") == "on"
	settings.SoftBan = r.FormValue("softBanEnabled") == "on"
	settings.AbnormalSpace.Enabled = r.FormValue("abnormalSpacingEnabled") == "on"

	if val := r.FormValue("multiLangWords"); val != "" {
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

	// dry-run is a real persisted setting. Dbg/TGDbg are CLI-only and not accepted
	// from the form because Transient is stripped by the store on save, so any value
	// posted here would be silently dropped.
	settings.Dry = r.FormValue("dryModeEnabled") == "on"
}
