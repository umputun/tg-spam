package webapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/config"
	"github.com/umputun/tg-spam/app/webapi/mocks"
)

func TestSaveConfigHandler(t *testing.T) {
	t.Run("successful save", func(t *testing.T) {
		settingsStore := &mocks.SettingsStoreMock{
			SaveFunc: func(ctx context.Context, settings *config.Settings) error {
				return nil
			},
		}

		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   appSettings,
			},
		}

		req := httptest.NewRequest("POST", "/config", http.NoBody)
		w := httptest.NewRecorder()
		srv.saveConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)
		assert.Len(t, settingsStore.SaveCalls(), 1)
		assert.Equal(t, appSettings, settingsStore.SaveCalls()[0].Settings)
	})

	t.Run("successful save with HTMX request", func(t *testing.T) {
		settingsStore := &mocks.SettingsStoreMock{
			SaveFunc: func(ctx context.Context, settings *config.Settings) error {
				return nil
			},
		}

		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   appSettings,
			},
		}

		req := httptest.NewRequest("POST", "/config", http.NoBody)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		srv.saveConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration saved successfully")
		assert.Len(t, settingsStore.SaveCalls(), 1)
		assert.Equal(t, appSettings, settingsStore.SaveCalls()[0].Settings)
	})

	t.Run("storage error", func(t *testing.T) {
		settingsStore := &mocks.SettingsStoreMock{
			SaveFunc: func(ctx context.Context, settings *config.Settings) error {
				return errors.New("storage error")
			},
		}

		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   appSettings,
			},
		}

		req := httptest.NewRequest("POST", "/config", http.NoBody)
		w := httptest.NewRecorder()
		srv.saveConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to save configuration")
		assert.Len(t, settingsStore.SaveCalls(), 1)
	})

	t.Run("no storage", func(t *testing.T) {
		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: nil,
				AppSettings:   appSettings,
			},
		}

		req := httptest.NewRequest("POST", "/config", http.NoBody)
		w := httptest.NewRecorder()
		srv.saveConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration storage not available")
	})
}

func TestLoadConfigHandler(t *testing.T) {
	t.Run("successful load", func(t *testing.T) {
		storedSettings := &config.Settings{
			InstanceID: "stored-instance",
			Telegram: config.TelegramSettings{
				Group: "stored-group",
			},
		}

		settingsStore := &mocks.SettingsStoreMock{
			LoadFunc: func(ctx context.Context) (*config.Settings, error) {
				return storedSettings, nil
			},
		}

		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   appSettings,
			},
		}

		req := httptest.NewRequest("POST", "/config/reload", http.NoBody)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)
		assert.Len(t, settingsStore.LoadCalls(), 1)

		// verify the current settings have been updated
		assert.Equal(t, "stored-instance", srv.AppSettings.InstanceID)
		assert.Equal(t, "stored-group", srv.AppSettings.Telegram.Group)
	})

	t.Run("successful load with HTMX request", func(t *testing.T) {
		storedSettings := &config.Settings{
			InstanceID: "stored-instance",
			Telegram: config.TelegramSettings{
				Group: "stored-group",
			},
		}

		settingsStore := &mocks.SettingsStoreMock{
			LoadFunc: func(ctx context.Context) (*config.Settings, error) {
				return storedSettings, nil
			},
		}

		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   appSettings,
			},
		}

		req := httptest.NewRequest("POST", "/config/reload", http.NoBody)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration loaded successfully")
		assert.Len(t, settingsStore.LoadCalls(), 1)
	})

	t.Run("load error", func(t *testing.T) {
		settingsStore := &mocks.SettingsStoreMock{
			LoadFunc: func(ctx context.Context) (*config.Settings, error) {
				return nil, errors.New("load error")
			},
		}

		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   appSettings,
			},
		}

		req := httptest.NewRequest("POST", "/config/reload", http.NoBody)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to load configuration")
		assert.Len(t, settingsStore.LoadCalls(), 1)

		// verify the current settings haven't changed
		assert.Equal(t, "test-instance", srv.AppSettings.InstanceID)
		assert.Equal(t, "test-group", srv.AppSettings.Telegram.Group)
	})

	t.Run("db tokens win on reload, transient and cli auth preserved", func(t *testing.T) {
		storedSettings := &config.Settings{
			InstanceID: "stored-instance",
			Telegram: config.TelegramSettings{
				Group: "stored-group",
				Token: "stored-token",
			},
			OpenAI: config.OpenAISettings{
				Token: "stored-openai-token",
			},
			Gemini: config.GeminiSettings{
				Token: "stored-gemini-token",
			},
			Server: config.ServerSettings{
				AuthHash: "stored-auth-hash",
			},
			Transient: config.TransientSettings{
				Dbg: true,
			},
		}

		settingsStore := &mocks.SettingsStoreMock{
			LoadFunc: func(ctx context.Context) (*config.Settings, error) {
				return storedSettings, nil
			},
		}

		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
				Token: "memory-token",
			},
			OpenAI: config.OpenAISettings{
				Token: "memory-openai-token",
			},
			Gemini: config.GeminiSettings{
				Token: "memory-gemini-token",
			},
			Server: config.ServerSettings{
				AuthHash: "cli-auth-hash",
			},
			Transient: config.TransientSettings{
				Dbg:           false,
				DataBaseURL:   "db-url",
				ConfigDB:      true,
				WebAuthPasswd: "cli-web-passwd",
				AuthFromCLI:   true,
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   appSettings,
				Version:       "test-version",
			},
		}

		req := httptest.NewRequest("POST", "/config/reload", http.NoBody)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Len(t, settingsStore.LoadCalls(), 1)

		// stored DB values win for tokens - in --confdb mode DB is authoritative
		assert.Equal(t, "stored-instance", srv.AppSettings.InstanceID)
		assert.Equal(t, "stored-group", srv.AppSettings.Telegram.Group)
		assert.Equal(t, "stored-token", srv.AppSettings.Telegram.Token)
		assert.Equal(t, "stored-openai-token", srv.AppSettings.OpenAI.Token)
		assert.Equal(t, "stored-gemini-token", srv.AppSettings.Gemini.Token)

		// CLI-overridable auth (hash/passwd) is preserved from in-memory state
		// only when AuthFromCLI marks it as originating from applyCLIOverrides
		assert.Equal(t, "cli-auth-hash", srv.AppSettings.Server.AuthHash)
		assert.Equal(t, "cli-web-passwd", srv.AppSettings.Transient.WebAuthPasswd)

		// transient settings always preserved from in-memory state
		assert.False(t, srv.AppSettings.Transient.Dbg)
		assert.Equal(t, "db-url", srv.AppSettings.Transient.DataBaseURL)
		assert.True(t, srv.AppSettings.Transient.ConfigDB)
		assert.True(t, srv.AppSettings.Transient.AuthFromCLI)
	})

	t.Run("db auth hash wins when not CLI-originated", func(t *testing.T) {
		// simulates external DB hash rotation: in-memory hash was loaded from DB
		// at startup (no CLI override), so reload must pick up the fresh DB value
		// instead of preserving the stale in-memory one.
		storedSettings := &config.Settings{
			InstanceID: "stored-instance",
			Server: config.ServerSettings{
				AuthHash: "fresh-db-hash",
			},
		}

		settingsStore := &mocks.SettingsStoreMock{
			LoadFunc: func(ctx context.Context) (*config.Settings, error) {
				return storedSettings, nil
			},
		}

		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Server: config.ServerSettings{
				AuthHash: "stale-in-memory-hash",
			},
			Transient: config.TransientSettings{
				ConfigDB:    true,
				AuthFromCLI: false,
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   appSettings,
			},
		}

		req := httptest.NewRequest("POST", "/config/reload", http.NoBody)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "fresh-db-hash", srv.AppSettings.Server.AuthHash,
			"DB hash must win when AuthFromCLI is false")
	})

	t.Run("empty in-memory passwd not restored when DB also empty", func(t *testing.T) {
		// verifies that transient copy preserves WebAuthPasswd (always CLI-origin)
		// but does not fabricate a value when both sides are empty.
		storedSettings := &config.Settings{
			InstanceID: "stored-instance",
		}

		settingsStore := &mocks.SettingsStoreMock{
			LoadFunc: func(ctx context.Context) (*config.Settings, error) {
				return storedSettings, nil
			},
		}

		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Transient: config.TransientSettings{
				ConfigDB: true,
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   appSettings,
			},
		}

		req := httptest.NewRequest("POST", "/config/reload", http.NoBody)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Empty(t, srv.AppSettings.Server.AuthHash)
		assert.Empty(t, srv.AppSettings.Transient.WebAuthPasswd)
	})

	t.Run("no storage", func(t *testing.T) {
		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: nil,
				AppSettings:   appSettings,
			},
		}

		req := httptest.NewRequest("POST", "/config/reload", http.NoBody)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration storage not available")
	})
}

func TestUpdateConfigHandler(t *testing.T) {
	t.Run("successful update without saving to DB", func(t *testing.T) {
		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
			},
			Meta: config.MetaSettings{
				LinksLimit: -1,
			},
			OpenAI: config.OpenAISettings{
				Model: "gpt-3.5-turbo",
			},
		}

		srv := Server{
			Config: Config{
				AppSettings: appSettings,
			},
		}

		// create form data
		form := url.Values{}
		form.Add("primaryGroup", "new-group")
		form.Add("metaLinksLimit", "5")
		form.Add("similarityThreshold", "0.8")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		srv.updateConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)

		// verify the settings were updated
		assert.Equal(t, "new-group", srv.AppSettings.Telegram.Group)
		assert.Equal(t, 5, srv.AppSettings.Meta.LinksLimit)
		assert.InEpsilon(t, 0.8, srv.AppSettings.SimilarityThreshold, 0.0001)
	})

	t.Run("successful update with HTMX request", func(t *testing.T) {
		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
			},
		}

		srv := Server{
			Config: Config{
				AppSettings: appSettings,
			},
		}

		// create form data
		form := url.Values{}
		form.Add("primaryGroup", "new-group")
		form.Add("paranoidMode", "on")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		srv.updateConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration updated successfully")

		// verify the settings were updated
		assert.Equal(t, "new-group", srv.AppSettings.Telegram.Group)
		assert.True(t, srv.AppSettings.ParanoidMode)
	})

	t.Run("successful update with save to DB", func(t *testing.T) {
		settingsStore := &mocks.SettingsStoreMock{
			SaveFunc: func(ctx context.Context, settings *config.Settings) error {
				return nil
			},
		}

		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   appSettings,
			},
		}

		// create form data
		form := url.Values{}
		form.Add("primaryGroup", "new-group")
		form.Add("saveToDb", "true")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		srv.updateConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)

		// verify the settings were saved to DB
		assert.Len(t, settingsStore.SaveCalls(), 1)
		assert.Equal(t, appSettings, settingsStore.SaveCalls()[0].Settings)
	})

	t.Run("parse form error", func(t *testing.T) {
		srv := Server{
			Config: Config{
				AppSettings: &config.Settings{},
			},
		}

		// create a malformed request that will trigger a ParseForm error
		req := httptest.NewRequest("PUT", "/config", strings.NewReader("%&"))
		// force Content-Type to application/x-www-form-urlencoded to make ParseForm try to parse it
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		srv.updateConfigHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to parse form")
	})

	t.Run("DB save error", func(t *testing.T) {
		settingsStore := &mocks.SettingsStoreMock{
			SaveFunc: func(ctx context.Context, settings *config.Settings) error {
				return errors.New("save error")
			},
		}

		appSettings := &config.Settings{
			InstanceID: "test-instance",
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   appSettings,
			},
		}

		// create form data
		form := url.Values{}
		form.Add("saveToDb", "true")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		srv.updateConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to save configuration")
	})

	t.Run("complex settings update", func(t *testing.T) {
		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
			},
			Meta: config.MetaSettings{
				LinksLimit:    -1,
				MentionsLimit: -1,
			},
			OpenAI: config.OpenAISettings{
				Model: "gpt-3.5-turbo",
			},
			Admin: config.AdminSettings{
				SuperUsers: []string{"user1"},
			},
			LuaPlugins: config.LuaPluginsSettings{
				Enabled: false,
			},
			CAS: config.CASSettings{
				API: "",
			},
		}

		srv := Server{
			Config: Config{
				AppSettings: appSettings,
			},
		}

		// create complex form data
		form := url.Values{}
		form.Add("metaEnabled", "on")
		form.Add("metaLinksLimit", "3")
		form.Add("metaMentionsLimit", "5")
		form.Add("metaLinksOnly", "on")
		form.Add("metaImageOnly", "on")
		form.Add("metaUsernameSymbols", "@#")
		form.Add("casEnabled", "on")
		form.Add("openAIModel", "gpt-4")
		form.Add("openAIHistorySize", "10")
		form.Add("luaPluginsEnabled", "on")
		form.Add("luaPluginsDir", "/plugins")
		form.Add("luaEnabledPlugins", "plugin1")
		form.Add("luaEnabledPlugins", "plugin2")
		form.Add("superUsers", "user1,user2, user3")
		form.Add("similarityThreshold", "0.75")
		form.Add("minMsgLen", "10")
		form.Add("maxEmoji", "5")
		form.Add("minSpamProbability", "65")
		form.Add("historySize", "20")
		form.Add("watchIntervalSecs", "30")
		form.Add("startupMessageEnabled", "on")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		srv.updateConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// verify all the complex settings were updated
		settings := srv.AppSettings
		assert.Equal(t, 3, settings.Meta.LinksLimit)
		assert.Equal(t, 5, settings.Meta.MentionsLimit)
		assert.True(t, settings.Meta.LinksOnly)
		assert.True(t, settings.Meta.ImageOnly)
		assert.Equal(t, "@#", settings.Meta.UsernameSymbols)
		assert.Equal(t, "https://api.cas.chat", settings.CAS.API)
		assert.Equal(t, "gpt-4", settings.OpenAI.Model)
		assert.Equal(t, 10, settings.OpenAI.HistorySize)
		assert.True(t, settings.LuaPlugins.Enabled)
		assert.Equal(t, "/plugins", settings.LuaPlugins.PluginsDir)
		assert.Equal(t, []string{"plugin1", "plugin2"}, settings.LuaPlugins.EnabledPlugins)
		assert.Equal(t, []string{"user1", "user2", "user3"}, settings.Admin.SuperUsers)
		assert.InEpsilon(t, 0.75, settings.SimilarityThreshold, 0.0001)
		assert.Equal(t, 10, settings.MinMsgLen)
		assert.Equal(t, 5, settings.MaxEmoji)
		assert.InEpsilon(t, 65.0, settings.MinSpamProbability, 0.0001)
		assert.Equal(t, 20, settings.History.Size)
		assert.Equal(t, 30, settings.Files.WatchInterval)
		assert.Equal(t, "Bot started", settings.Message.Startup)
	})
}

func TestDeleteConfigHandler(t *testing.T) {
	t.Run("successful delete", func(t *testing.T) {
		settingsStore := &mocks.SettingsStoreMock{
			DeleteFunc: func(ctx context.Context) error {
				return nil
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
			},
		}

		req := httptest.NewRequest("DELETE", "/config", http.NoBody)
		w := httptest.NewRecorder()
		srv.deleteConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)
		assert.Len(t, settingsStore.DeleteCalls(), 1)
	})

	t.Run("successful delete with HTMX request", func(t *testing.T) {
		settingsStore := &mocks.SettingsStoreMock{
			DeleteFunc: func(ctx context.Context) error {
				return nil
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
			},
		}

		req := httptest.NewRequest("DELETE", "/config", http.NoBody)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		srv.deleteConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration deleted successfully")
		assert.Len(t, settingsStore.DeleteCalls(), 1)
	})

	t.Run("delete error", func(t *testing.T) {
		settingsStore := &mocks.SettingsStoreMock{
			DeleteFunc: func(ctx context.Context) error {
				return errors.New("delete error")
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
			},
		}

		req := httptest.NewRequest("DELETE", "/config", http.NoBody)
		w := httptest.NewRecorder()
		srv.deleteConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to delete configuration")
		assert.Len(t, settingsStore.DeleteCalls(), 1)
	})

	t.Run("no storage", func(t *testing.T) {
		srv := Server{
			Config: Config{
				SettingsStore: nil,
			},
		}

		req := httptest.NewRequest("DELETE", "/config", http.NoBody)
		w := httptest.NewRecorder()
		srv.deleteConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration storage not available")
	})
}

func TestUpdateSettingsFromForm(t *testing.T) {
	t.Run("boolean flags and simple fields", func(t *testing.T) {
		settings := &config.Settings{
			Telegram: config.TelegramSettings{
				Group: "original-group",
			},
			Meta:         config.MetaSettings{},
			ParanoidMode: false,
			NoSpamReply:  false,
		}

		form := url.Values{}
		form.Add("primaryGroup", "new-group")
		form.Add("paranoidMode", "on")
		form.Add("noSpamReply", "on")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		err := req.ParseForm()
		require.NoError(t, err)

		updateSettingsFromForm(settings, req)

		assert.Equal(t, "new-group", settings.Telegram.Group)
		assert.True(t, settings.ParanoidMode)
		assert.True(t, settings.NoSpamReply)
	})

	t.Run("numeric values", func(t *testing.T) {
		settings := &config.Settings{
			SimilarityThreshold: 0.5,
			MinMsgLen:           5,
			MaxEmoji:            10,
			MinSpamProbability:  50,
			FirstMessagesCount:  3,
		}

		form := url.Values{}
		form.Add("similarityThreshold", "0.75")
		form.Add("minMsgLen", "10")
		form.Add("maxEmoji", "15")
		form.Add("minSpamProbability", "60")
		form.Add("firstMessagesCount", "5")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		err := req.ParseForm()
		require.NoError(t, err)

		updateSettingsFromForm(settings, req)

		assert.InEpsilon(t, 0.75, settings.SimilarityThreshold, 0.0001)
		assert.Equal(t, 10, settings.MinMsgLen)
		assert.Equal(t, 15, settings.MaxEmoji)
		assert.InEpsilon(t, 60.0, settings.MinSpamProbability, 0.0001)
		assert.Equal(t, 5, settings.FirstMessagesCount)
	})

	t.Run("super users parsing", func(t *testing.T) {
		settings := &config.Settings{
			Admin: config.AdminSettings{
				SuperUsers: []string{"user1"},
			},
		}

		form := url.Values{}
		form.Add("superUsers", "user2, user3,user4")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		err := req.ParseForm()
		require.NoError(t, err)

		updateSettingsFromForm(settings, req)

		assert.Equal(t, []string{"user2", "user3", "user4"}, settings.Admin.SuperUsers)
	})

	t.Run("super users empty clears list", func(t *testing.T) {
		settings := &config.Settings{
			Admin: config.AdminSettings{
				SuperUsers: []string{"user1", "user2"},
			},
		}

		form := url.Values{}
		form.Add("superUsers", "")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		err := req.ParseForm()
		require.NoError(t, err)

		updateSettingsFromForm(settings, req)

		assert.Empty(t, settings.Admin.SuperUsers)
	})

	t.Run("super users omitted preserves list", func(t *testing.T) {
		settings := &config.Settings{
			Admin: config.AdminSettings{
				SuperUsers: []string{"user1", "user2"},
			},
		}

		form := url.Values{}
		form.Add("primaryGroup", "unrelated")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		err := req.ParseForm()
		require.NoError(t, err)

		updateSettingsFromForm(settings, req)

		assert.Equal(t, []string{"user1", "user2"}, settings.Admin.SuperUsers)
	})

	t.Run("CAS API handling", func(t *testing.T) {
		t.Run("enable CAS", func(t *testing.T) {
			settings := &config.Settings{
				CAS: config.CASSettings{
					API: "",
				},
			}

			form := url.Values{}
			form.Add("casEnabled", "on")

			req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			err := req.ParseForm()
			require.NoError(t, err)

			updateSettingsFromForm(settings, req)

			assert.Equal(t, "https://api.cas.chat", settings.CAS.API)
		})

		t.Run("disable CAS", func(t *testing.T) {
			settings := &config.Settings{
				CAS: config.CASSettings{
					API: "https://api.cas.chat",
				},
			}

			form := url.Values{}
			// casEnabled not set

			req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			err := req.ParseForm()
			require.NoError(t, err)

			updateSettingsFromForm(settings, req)

			assert.Empty(t, settings.CAS.API)
		})
	})

	t.Run("meta settings", func(t *testing.T) {
		settings := &config.Settings{
			Meta: config.MetaSettings{
				LinksLimit:      -1,
				MentionsLimit:   -1,
				UsernameSymbols: "",
			},
		}

		form := url.Values{}
		form.Add("metaEnabled", "on")
		form.Add("metaLinksLimit", "3")
		form.Add("metaMentionsLimit", "5")
		form.Add("metaLinksOnly", "on")
		form.Add("metaImageOnly", "on")
		form.Add("metaUsernameSymbols", "@#")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		err := req.ParseForm()
		require.NoError(t, err)

		updateSettingsFromForm(settings, req)

		assert.Equal(t, 3, settings.Meta.LinksLimit)
		assert.Equal(t, 5, settings.Meta.MentionsLimit)
		assert.True(t, settings.Meta.LinksOnly)
		assert.True(t, settings.Meta.ImageOnly)
		assert.Equal(t, "@#", settings.Meta.UsernameSymbols)
	})

	t.Run("OpenAI settings", func(t *testing.T) {
		settings := &config.Settings{
			OpenAI: config.OpenAISettings{
				APIBase:     "",
				Model:       "gpt-3.5-turbo",
				HistorySize: 5,
				Veto:        false,
			},
		}

		form := url.Values{}
		form.Add("openAIModel", "gpt-4")
		form.Add("openAIHistorySize", "10")
		form.Add("openAIVeto", "on")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		err := req.ParseForm()
		require.NoError(t, err)

		updateSettingsFromForm(settings, req)

		assert.Equal(t, "gpt-4", settings.OpenAI.Model)
		assert.Equal(t, 10, settings.OpenAI.HistorySize)
		assert.True(t, settings.OpenAI.Veto)
	})

	t.Run("form never destroys OpenAI credentials", func(t *testing.T) {
		// credential management is CLI-only; no UI path should be able to wipe
		// aPIBase or Token regardless of what the form contains.
		settings := &config.Settings{
			OpenAI: config.OpenAISettings{
				APIBase: "https://api.example.com/v1",
				Token:   "sk-secret",
				Model:   "gpt-4",
			},
		}

		form := url.Values{}
		form.Add("openAIModel", "gpt-4o")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		require.NoError(t, req.ParseForm())

		updateSettingsFromForm(settings, req)

		assert.Equal(t, "https://api.example.com/v1", settings.OpenAI.APIBase)
		assert.Equal(t, "sk-secret", settings.OpenAI.Token)
		assert.True(t, settings.IsOpenAIEnabled())
		assert.Equal(t, "gpt-4o", settings.OpenAI.Model)
	})

	t.Run("multi-lingual words form uses multiLangWords", func(t *testing.T) {
		settings := &config.Settings{}

		form := url.Values{}
		form.Add("multiLangWords", "7")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		require.NoError(t, req.ParseForm())

		updateSettingsFromForm(settings, req)

		assert.Equal(t, 7, settings.MultiLangWords)
	})

	t.Run("Lua plugins settings", func(t *testing.T) {
		settings := &config.Settings{
			LuaPlugins: config.LuaPluginsSettings{
				Enabled:        false,
				DynamicReload:  false,
				PluginsDir:     "",
				EnabledPlugins: nil,
			},
		}

		form := url.Values{}
		form.Add("luaPluginsEnabled", "on")
		form.Add("luaDynamicReload", "on")
		form.Add("luaPluginsDir", "/plugins")
		form.Add("luaEnabledPlugins", "plugin1")
		form.Add("luaEnabledPlugins", "plugin2")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		err := req.ParseForm()
		require.NoError(t, err)

		updateSettingsFromForm(settings, req)

		assert.True(t, settings.LuaPlugins.Enabled)
		assert.True(t, settings.LuaPlugins.DynamicReload)
		assert.Equal(t, "/plugins", settings.LuaPlugins.PluginsDir)
		assert.Equal(t, []string{"plugin1", "plugin2"}, settings.LuaPlugins.EnabledPlugins)
	})

	t.Run("startup message handling", func(t *testing.T) {
		t.Run("enable startup message", func(t *testing.T) {
			settings := &config.Settings{
				Message: config.MessageSettings{
					Startup: "",
				},
			}

			form := url.Values{}
			form.Add("startupMessageEnabled", "on")

			req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			err := req.ParseForm()
			require.NoError(t, err)

			updateSettingsFromForm(settings, req)

			assert.Equal(t, "Bot started", settings.Message.Startup)
		})

		t.Run("disable startup message", func(t *testing.T) {
			settings := &config.Settings{
				Message: config.MessageSettings{
					Startup: "Bot started",
				},
			}

			form := url.Values{}
			// startupMessageEnabled not set

			req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			err := req.ParseForm()
			require.NoError(t, err)

			updateSettingsFromForm(settings, req)

			assert.Empty(t, settings.Message.Startup)
		})
	})
}

func TestUpdateSettingsFromForm_NewGroups(t *testing.T) {
	tests := []struct {
		name   string
		form   url.Values
		assert func(t *testing.T, s *config.Settings)
	}{
		{
			name: "delete group toggles on",
			form: url.Values{
				"deleteJoinMessages":  []string{"on"},
				"deleteLeaveMessages": []string{"on"},
			},
			assert: func(t *testing.T, s *config.Settings) {
				assert.True(t, s.Delete.JoinMessages)
				assert.True(t, s.Delete.LeaveMessages)
			},
		},
		{
			// fields not rendered in the ConfigDB UI form must not be silently
			// reset to zero when the user saves unrelated changes. Starts at false
			// here; the dedicated TestUpdateSettingsFromForm_PreservesUnrenderedFields
			// test exercises the same path with non-zero initial state.
			name: "delete group omitted preserves current values",
			form: url.Values{},
			assert: func(t *testing.T, s *config.Settings) {
				assert.False(t, s.Delete.JoinMessages)
				assert.False(t, s.Delete.LeaveMessages)
			},
		},
		{
			name: "meta contact-only and giveaway",
			form: url.Values{
				"metaContactOnly": []string{"on"},
				"metaGiveaway":    []string{"on"},
			},
			assert: func(t *testing.T, s *config.Settings) {
				assert.True(t, s.Meta.ContactOnly)
				assert.True(t, s.Meta.Giveaway)
			},
		},
		{
			name: "gemini full payload",
			form: url.Values{
				"geminiVeto":               []string{"on"},
				"geminiCheckShortMessages": []string{"on"},
				"geminiHistorySize":        []string{"7"},
				"geminiModel":              []string{"gemini-2.0-flash"},
				"geminiPrompt":             []string{"detect spam"},
				"geminiMaxTokensResponse":  []string{"512"},
				"geminiMaxSymbolsRequest":  []string{"4096"},
				"geminiRetryCount":         []string{"3"},
			},
			assert: func(t *testing.T, s *config.Settings) {
				assert.True(t, s.Gemini.Veto)
				assert.True(t, s.Gemini.CheckShortMessages)
				assert.Equal(t, 7, s.Gemini.HistorySize)
				assert.Equal(t, "gemini-2.0-flash", s.Gemini.Model)
				assert.Equal(t, "detect spam", s.Gemini.Prompt)
				assert.Equal(t, int32(512), s.Gemini.MaxTokensResponse)
				assert.Equal(t, 4096, s.Gemini.MaxSymbolsRequest)
				assert.Equal(t, 3, s.Gemini.RetryCount)
			},
		},
		{
			name: "gemini token never read from form",
			form: url.Values{
				"geminiToken": []string{"hijacked"},
			},
			assert: func(t *testing.T, s *config.Settings) {
				assert.Empty(t, s.Gemini.Token, "Gemini.Token must not be writable via form")
			},
		},
		{
			name: "gemini malformed numeric values are ignored",
			form: url.Values{
				"geminiHistorySize":       []string{"oops"},
				"geminiMaxTokensResponse": []string{"oops"},
				"geminiMaxSymbolsRequest": []string{"oops"},
				"geminiRetryCount":        []string{"oops"},
			},
			assert: func(t *testing.T, s *config.Settings) {
				assert.Equal(t, 1, s.Gemini.HistorySize, "preserved when input invalid")
				assert.Equal(t, int32(2), s.Gemini.MaxTokensResponse, "preserved when input invalid")
				assert.Equal(t, 3, s.Gemini.MaxSymbolsRequest, "preserved when input invalid")
				assert.Equal(t, 4, s.Gemini.RetryCount, "preserved when input invalid")
			},
		},
		{
			name: "llm orchestration",
			form: url.Values{
				"llmConsensus":      []string{"all"},
				"llmRequestTimeout": []string{"45s"},
			},
			assert: func(t *testing.T, s *config.Settings) {
				assert.Equal(t, "all", s.LLM.Consensus)
				assert.Equal(t, 45*time.Second, s.LLM.RequestTimeout)
			},
		},
		{
			name: "duplicates detector",
			form: url.Values{
				"duplicatesThreshold": []string{"4"},
				"duplicatesWindow":    []string{"30s"},
			},
			assert: func(t *testing.T, s *config.Settings) {
				assert.Equal(t, 4, s.Duplicates.Threshold)
				assert.Equal(t, 30*time.Second, s.Duplicates.Window)
			},
		},
		{
			name: "report enabled with thresholds",
			form: url.Values{
				"reportEnabled":          []string{"on"},
				"reportThreshold":        []string{"3"},
				"reportAutoBanThreshold": []string{"10"},
				"reportRateLimit":        []string{"5"},
				"reportRatePeriod":       []string{"2m"},
			},
			assert: func(t *testing.T, s *config.Settings) {
				assert.True(t, s.Report.Enabled)
				assert.Equal(t, 3, s.Report.Threshold)
				assert.Equal(t, 10, s.Report.AutoBanThreshold)
				assert.Equal(t, 5, s.Report.RateLimit)
				assert.Equal(t, 2*time.Minute, s.Report.RatePeriod)
			},
		},
		{
			name: "aggressive cleanup with limit",
			form: url.Values{
				"aggressiveCleanup":      []string{"on"},
				"aggressiveCleanupLimit": []string{"50"},
			},
			assert: func(t *testing.T, s *config.Settings) {
				assert.True(t, s.AggressiveCleanup)
				assert.Equal(t, 50, s.AggressiveCleanupLimit)
			},
		},
		{
			name: "openai check-short-messages toggle",
			form: url.Values{
				"openAICheckShortMessages": []string{"on"},
			},
			assert: func(t *testing.T, s *config.Settings) {
				assert.True(t, s.OpenAI.CheckShortMessages)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			settings := &config.Settings{
				Gemini: config.GeminiSettings{
					HistorySize:       1,
					MaxTokensResponse: 2,
					MaxSymbolsRequest: 3,
					RetryCount:        4,
				},
			}

			req := httptest.NewRequest("PUT", "/config", strings.NewReader(tc.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			require.NoError(t, req.ParseForm())

			updateSettingsFromForm(settings, req)

			tc.assert(t, settings)
		})
	}
}

func TestUpdateConfigHandler_RoundTrip_NewGroups(t *testing.T) {
	// posts a form populating every new group, asserts in-memory settings reflect every value
	// and that the optional saveToDb path persists the same struct end-to-end via the store mock
	store := &mocks.SettingsStoreMock{
		SaveFunc: func(_ context.Context, _ *config.Settings) error { return nil },
	}

	appSettings := &config.Settings{
		Gemini: config.GeminiSettings{HistorySize: 0, MaxTokensResponse: 0},
	}
	srv := Server{Config: Config{SettingsStore: store, AppSettings: appSettings}}

	form := url.Values{
		"saveToDb":                 []string{"true"},
		"deleteJoinMessages":       []string{"on"},
		"deleteLeaveMessages":      []string{"on"},
		"metaContactOnly":          []string{"on"},
		"metaGiveaway":             []string{"on"},
		"geminiVeto":               []string{"on"},
		"geminiCheckShortMessages": []string{"on"},
		"geminiHistorySize":        []string{"6"},
		"geminiModel":              []string{"gemini-pro"},
		"geminiPrompt":             []string{"prompt-text"},
		"geminiMaxTokensResponse":  []string{"800"},
		"geminiMaxSymbolsRequest":  []string{"5000"},
		"geminiRetryCount":         []string{"2"},
		"llmConsensus":             []string{"any"},
		"llmRequestTimeout":        []string{"15s"},
		"duplicatesThreshold":      []string{"5"},
		"duplicatesWindow":         []string{"1m"},
		"reportEnabled":            []string{"on"},
		"reportThreshold":          []string{"4"},
		"reportAutoBanThreshold":   []string{"12"},
		"reportRateLimit":          []string{"6"},
		"reportRatePeriod":         []string{"3m"},
		"aggressiveCleanup":        []string{"on"},
		"aggressiveCleanupLimit":   []string{"99"},
	}

	req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.updateConfigHandler(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, store.SaveCalls(), 1, "saveToDb=true must trigger a single Save call")

	saved := store.SaveCalls()[0].Settings
	assert.Same(t, appSettings, saved, "store receives the same in-memory settings pointer")

	// every new group is persisted
	assert.True(t, saved.Delete.JoinMessages)
	assert.True(t, saved.Delete.LeaveMessages)
	assert.True(t, saved.Meta.ContactOnly)
	assert.True(t, saved.Meta.Giveaway)
	assert.True(t, saved.Gemini.Veto)
	assert.True(t, saved.Gemini.CheckShortMessages)
	assert.Equal(t, 6, saved.Gemini.HistorySize)
	assert.Equal(t, "gemini-pro", saved.Gemini.Model)
	assert.Equal(t, "prompt-text", saved.Gemini.Prompt)
	assert.Equal(t, int32(800), saved.Gemini.MaxTokensResponse)
	assert.Equal(t, 5000, saved.Gemini.MaxSymbolsRequest)
	assert.Equal(t, 2, saved.Gemini.RetryCount)
	assert.Equal(t, "any", saved.LLM.Consensus)
	assert.Equal(t, 15*time.Second, saved.LLM.RequestTimeout)
	assert.Equal(t, 5, saved.Duplicates.Threshold)
	assert.Equal(t, time.Minute, saved.Duplicates.Window)
	assert.True(t, saved.Report.Enabled)
	assert.Equal(t, 4, saved.Report.Threshold)
	assert.Equal(t, 12, saved.Report.AutoBanThreshold)
	assert.Equal(t, 6, saved.Report.RateLimit)
	assert.Equal(t, 3*time.Minute, saved.Report.RatePeriod)
	assert.True(t, saved.AggressiveCleanup)
	assert.Equal(t, 99, saved.AggressiveCleanupLimit)
}

func TestUpdateSettingsFromForm_PreservesUnrenderedFields(t *testing.T) {
	// regression: the ConfigDB UI form does not render controls for these flags,
	// but the handler previously overwrote them from absent form values, which
	// silently wiped configuration on every unrelated save. Submitting a form
	// that omits these fields must leave the current values intact.
	settings := &config.Settings{
		Meta: config.MetaSettings{
			ContactOnly:     true,
			Giveaway:        true,
			UsernameSymbols: "@#",
		},
		Delete: config.DeleteSettings{
			JoinMessages:  true,
			LeaveMessages: true,
		},
		Report: config.ReportSettings{
			Enabled: true,
		},
		AggressiveCleanup: true,
	}

	form := url.Values{}
	form.Add("primaryGroup", "new-group") // simulate user saving an unrelated change

	req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	require.NoError(t, req.ParseForm())

	updateSettingsFromForm(settings, req)

	assert.True(t, settings.Meta.ContactOnly, "Meta.ContactOnly must be preserved when form omits it")
	assert.True(t, settings.Meta.Giveaway, "Meta.Giveaway must be preserved when form omits it")
	assert.Equal(t, "@#", settings.Meta.UsernameSymbols, "Meta.UsernameSymbols must be preserved when form omits it")
	assert.True(t, settings.Delete.JoinMessages, "Delete.JoinMessages must be preserved when form omits it")
	assert.True(t, settings.Delete.LeaveMessages, "Delete.LeaveMessages must be preserved when form omits it")
	assert.True(t, settings.Report.Enabled, "Report.Enabled must be preserved when form omits it")
	assert.True(t, settings.AggressiveCleanup, "AggressiveCleanup must be preserved when form omits it")
}

func TestUpdateSettingsFromForm_MetaUsernameSymbolsEmptyDisables(t *testing.T) {
	// the UI hint below the input says "leave empty to disable". Clearing the
	// field in the form must clear the setting regardless of metaEnabled state.
	settings := &config.Settings{
		Meta: config.MetaSettings{
			UsernameSymbols: "@#",
		},
	}

	form := url.Values{}
	form.Add("metaEnabled", "on")
	form.Add("metaUsernameSymbols", "")

	req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	require.NoError(t, req.ParseForm())

	updateSettingsFromForm(settings, req)

	assert.Empty(t, settings.Meta.UsernameSymbols, "empty form value must clear the setting")
}
