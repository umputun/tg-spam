package webapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/app/config"
)

func TestSaveConfigHandler(t *testing.T) {
	t.Run("successful save", func(t *testing.T) {
		settingsStore := &SettingsStoreMock{
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

		req := httptest.NewRequest("POST", "/config", nil)
		w := httptest.NewRecorder()
		srv.saveConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)
		assert.Equal(t, 1, len(settingsStore.SaveCalls()))
		assert.Equal(t, appSettings, settingsStore.SaveCalls()[0].Settings)
	})

	t.Run("successful save with HTMX request", func(t *testing.T) {
		settingsStore := &SettingsStoreMock{
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

		req := httptest.NewRequest("POST", "/config", nil)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		srv.saveConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration saved successfully")
		assert.Equal(t, 1, len(settingsStore.SaveCalls()))
		assert.Equal(t, appSettings, settingsStore.SaveCalls()[0].Settings)
	})

	t.Run("storage error", func(t *testing.T) {
		settingsStore := &SettingsStoreMock{
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

		req := httptest.NewRequest("POST", "/config", nil)
		w := httptest.NewRecorder()
		srv.saveConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to save configuration")
		assert.Equal(t, 1, len(settingsStore.SaveCalls()))
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

		req := httptest.NewRequest("POST", "/config", nil)
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

		settingsStore := &SettingsStoreMock{
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

		req := httptest.NewRequest("GET", "/config", nil)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)
		assert.Equal(t, 1, len(settingsStore.LoadCalls()))

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

		settingsStore := &SettingsStoreMock{
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

		req := httptest.NewRequest("GET", "/config", nil)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration loaded successfully")
		assert.Equal(t, 1, len(settingsStore.LoadCalls()))
	})

	t.Run("load error", func(t *testing.T) {
		settingsStore := &SettingsStoreMock{
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

		req := httptest.NewRequest("GET", "/config", nil)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to load configuration")
		assert.Equal(t, 1, len(settingsStore.LoadCalls()))

		// verify the current settings haven't changed
		assert.Equal(t, "test-instance", srv.AppSettings.InstanceID)
		assert.Equal(t, "test-group", srv.AppSettings.Telegram.Group)
	})

	t.Run("preserve cli settings", func(t *testing.T) {
		storedSettings := &config.Settings{
			InstanceID: "stored-instance",
			Telegram: config.TelegramSettings{
				Group: "stored-group",
				Token: "stored-token",
			},
			OpenAI: config.OpenAISettings{
				Token: "stored-openai-token",
			},
			Transient: config.TransientSettings{
				Dbg: true,
			},
		}

		settingsStore := &SettingsStoreMock{
			LoadFunc: func(ctx context.Context) (*config.Settings, error) {
				return storedSettings, nil
			},
		}

		appSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "test-group",
				Token: "cli-token",
			},
			OpenAI: config.OpenAISettings{
				Token: "cli-openai-token",
			},
			Transient: config.TransientSettings{
				Dbg: false,
				// CLI-only settings
				DataBaseURL: "db-url",
				ConfigDB:    true,
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   appSettings,
				Version:       "test-version",
			},
		}

		req := httptest.NewRequest("GET", "/config", nil)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, 1, len(settingsStore.LoadCalls()))

		// verify stored settings loaded but CLI settings preserved
		assert.Equal(t, "stored-instance", srv.AppSettings.InstanceID)
		assert.Equal(t, "stored-group", srv.AppSettings.Telegram.Group)
		assert.Equal(t, "cli-token", srv.AppSettings.Telegram.Token)
		assert.Equal(t, "cli-openai-token", srv.AppSettings.OpenAI.Token)
		assert.Equal(t, false, srv.AppSettings.Transient.Dbg)
		assert.Equal(t, "db-url", srv.AppSettings.Transient.DataBaseURL)
		assert.Equal(t, true, srv.AppSettings.Transient.ConfigDB)
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

		req := httptest.NewRequest("GET", "/config", nil)
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
		form.Add("openAIEnabled", "on")
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
		assert.Equal(t, 0.8, srv.AppSettings.SimilarityThreshold)
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
		settingsStore := &SettingsStoreMock{
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
		assert.Equal(t, 1, len(settingsStore.SaveCalls()))
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
		settingsStore := &SettingsStoreMock{
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
		form.Add("openAIEnabled", "on")
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
		assert.Equal(t, 0.75, settings.SimilarityThreshold)
		assert.Equal(t, 10, settings.MinMsgLen)
		assert.Equal(t, 5, settings.MaxEmoji)
		assert.Equal(t, 65.0, settings.MinSpamProbability)
		assert.Equal(t, 20, settings.History.Size)
		assert.Equal(t, 30, settings.Files.WatchInterval)
		assert.Equal(t, "Bot started", settings.Message.Startup)
	})
}

func TestDeleteConfigHandler(t *testing.T) {
	t.Run("successful delete", func(t *testing.T) {
		settingsStore := &SettingsStoreMock{
			DeleteFunc: func(ctx context.Context) error {
				return nil
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
			},
		}

		req := httptest.NewRequest("DELETE", "/config", nil)
		w := httptest.NewRecorder()
		srv.deleteConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)
		assert.Equal(t, 1, len(settingsStore.DeleteCalls()))
	})

	t.Run("successful delete with HTMX request", func(t *testing.T) {
		settingsStore := &SettingsStoreMock{
			DeleteFunc: func(ctx context.Context) error {
				return nil
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
			},
		}

		req := httptest.NewRequest("DELETE", "/config", nil)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		srv.deleteConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration deleted successfully")
		assert.Equal(t, 1, len(settingsStore.DeleteCalls()))
	})

	t.Run("delete error", func(t *testing.T) {
		settingsStore := &SettingsStoreMock{
			DeleteFunc: func(ctx context.Context) error {
				return errors.New("delete error")
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
			},
		}

		req := httptest.NewRequest("DELETE", "/config", nil)
		w := httptest.NewRecorder()
		srv.deleteConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to delete configuration")
		assert.Equal(t, 1, len(settingsStore.DeleteCalls()))
	})

	t.Run("no storage", func(t *testing.T) {
		srv := Server{
			Config: Config{
				SettingsStore: nil,
			},
		}

		req := httptest.NewRequest("DELETE", "/config", nil)
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

		assert.Equal(t, 0.75, settings.SimilarityThreshold)
		assert.Equal(t, 10, settings.MinMsgLen)
		assert.Equal(t, 15, settings.MaxEmoji)
		assert.Equal(t, 60.0, settings.MinSpamProbability)
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

			assert.Equal(t, "", settings.CAS.API)
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
		form.Add("openAIEnabled", "on")
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

			assert.Equal(t, "", settings.Message.Startup)
		})
	})
}
