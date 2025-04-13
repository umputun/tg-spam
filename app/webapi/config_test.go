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
		assert.Contains(t, w.Body.String(), "alert-success")
		assert.Equal(t, 1, len(settingsStore.SaveCalls()))
	})

	t.Run("error saving configuration", func(t *testing.T) {
		settingsStore := &SettingsStoreMock{
			SaveFunc: func(ctx context.Context, settings *config.Settings) error {
				return errors.New("test error")
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
		assert.Contains(t, w.Body.String(), "test error")
		assert.Equal(t, 1, len(settingsStore.SaveCalls()))
	})

	t.Run("no config store", func(t *testing.T) {
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
		loadedSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "loaded-group",
			},
		}

		settingsStore := &SettingsStoreMock{
			LoadFunc: func(ctx context.Context) (*config.Settings, error) {
				return loadedSettings, nil
			},
		}

		originalSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "original-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   originalSettings,
			},
		}

		req := httptest.NewRequest("GET", "/config", nil)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)
		assert.Equal(t, 1, len(settingsStore.LoadCalls()))
		assert.Equal(t, "loaded-group", srv.AppSettings.Telegram.Group) // settings updated
	})

	t.Run("successful load with HTMX request", func(t *testing.T) {
		loadedSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "loaded-group",
			},
		}

		settingsStore := &SettingsStoreMock{
			LoadFunc: func(ctx context.Context) (*config.Settings, error) {
				return loadedSettings, nil
			},
		}

		originalSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "original-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   originalSettings,
			},
		}

		req := httptest.NewRequest("GET", "/config", nil)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration loaded successfully")
		assert.Contains(t, w.Body.String(), "alert-success")
		assert.Equal(t, "true", w.Header().Get("HX-Refresh"))
		assert.Equal(t, 1, len(settingsStore.LoadCalls()))
		assert.Equal(t, "loaded-group", srv.AppSettings.Telegram.Group) // settings updated
	})

	t.Run("error loading configuration", func(t *testing.T) {
		settingsStore := &SettingsStoreMock{
			LoadFunc: func(ctx context.Context) (*config.Settings, error) {
				return nil, errors.New("test error")
			},
		}

		originalSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "original-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: settingsStore,
				AppSettings:   originalSettings,
			},
		}

		req := httptest.NewRequest("GET", "/config", nil)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "test error")
		assert.Equal(t, 1, len(settingsStore.LoadCalls()))
		assert.Equal(t, "original-group", srv.AppSettings.Telegram.Group) // settings not updated
	})

	t.Run("no config store", func(t *testing.T) {
		originalSettings := &config.Settings{
			InstanceID: "test-instance",
			Telegram: config.TelegramSettings{
				Group: "original-group",
			},
		}

		srv := Server{
			Config: Config{
				SettingsStore: nil,
				AppSettings:   originalSettings,
			},
		}

		req := httptest.NewRequest("GET", "/config", nil)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration storage not available")
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
		assert.Contains(t, w.Body.String(), "alert-success")
		assert.Equal(t, 1, len(settingsStore.DeleteCalls()))
	})

	t.Run("error deleting configuration", func(t *testing.T) {
		settingsStore := &SettingsStoreMock{
			DeleteFunc: func(ctx context.Context) error {
				return errors.New("test error")
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
		assert.Contains(t, w.Body.String(), "test error")
		assert.Equal(t, 1, len(settingsStore.DeleteCalls()))
	})

	t.Run("no config store", func(t *testing.T) {
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
	t.Run("update basic settings", func(t *testing.T) {
		settings := &config.Settings{}
		req := createFormRequest(map[string]string{
			"primaryGroup":       "test-group",
			"adminGroup":         "admin-group",
			"noSpamReply":        "on",
			"casEnabled":         "on",
			"superUsers":         "user1,user2, user3",
			"debugModeEnabled":   "on",
			"dryModeEnabled":     "on",
			"tgDebugModeEnabled": "on",
		})

		updateSettingsFromForm(settings, req)

		assert.Equal(t, "test-group", settings.Telegram.Group)
		assert.Equal(t, "admin-group", settings.Admin.AdminGroup)
		assert.True(t, settings.NoSpamReply)
		assert.Equal(t, "https://api.cas.chat", settings.CAS.API) // default CAS API is set when enabled
		assert.Equal(t, []string{"user1", "user2", "user3"}, settings.Admin.SuperUsers)
		assert.True(t, settings.Transient.Dbg)
		assert.True(t, settings.Dry)
		assert.True(t, settings.Transient.TGDbg)
	})

	t.Run("update meta checks settings", func(t *testing.T) {
		settings := &config.Settings{}
		req := createFormRequest(map[string]string{
			"metaEnabled":         "on",
			"metaLinksLimit":      "5",
			"metaMentionsLimit":   "3",
			"metaLinksOnly":       "on",
			"metaImageOnly":       "on",
			"metaVideoOnly":       "on",
			"metaAudioOnly":       "on",
			"metaForwarded":       "on",
			"metaKeyboard":        "on",
			"metaUsernameSymbols": "abc",
		})

		updateSettingsFromForm(settings, req)

		assert.Equal(t, 5, settings.Meta.LinksLimit)
		assert.Equal(t, 3, settings.Meta.MentionsLimit)
		assert.True(t, settings.Meta.LinksOnly)
		assert.True(t, settings.Meta.ImageOnly)
		assert.True(t, settings.Meta.VideosOnly)
		assert.True(t, settings.Meta.AudiosOnly)
		assert.True(t, settings.Meta.Forward)
		assert.True(t, settings.Meta.Keyboard)
		assert.Equal(t, "abc", settings.Meta.UsernameSymbols)
		assert.True(t, settings.IsMetaEnabled()) // using helper method to verify Meta is enabled
	})

	t.Run("update openAI settings", func(t *testing.T) {
		settings := &config.Settings{}
		req := createFormRequest(map[string]string{
			"openAIEnabled":     "on",
			"openAIVeto":        "on",
			"openAIHistorySize": "10",
			"openAIModel":       "gpt-4",
		})

		updateSettingsFromForm(settings, req)

		// since OpenAI.APIBase is set when enabled but we don't have a field
		// in the form for it, we check the veto and model settings
		assert.True(t, settings.OpenAI.Veto)
		assert.Equal(t, 10, settings.OpenAI.HistorySize)
		assert.Equal(t, "gpt-4", settings.OpenAI.Model)
	})

	t.Run("update lua plugins settings", func(t *testing.T) {
		settings := &config.Settings{}
		form := url.Values{}
		form.Add("luaPluginsEnabled", "on")
		form.Add("luaDynamicReload", "on")
		form.Add("luaPluginsDir", "/plugins")
		form.Add("luaEnabledPlugins", "plugin1")
		form.Add("luaEnabledPlugins", "plugin2")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.PostForm = form

		updateSettingsFromForm(settings, req)

		assert.True(t, settings.LuaPlugins.Enabled)
		assert.True(t, settings.LuaPlugins.DynamicReload)
		assert.Equal(t, "/plugins", settings.LuaPlugins.PluginsDir)
		assert.Equal(t, []string{"plugin1", "plugin2"}, settings.LuaPlugins.EnabledPlugins)
	})

	t.Run("update spam detection settings", func(t *testing.T) {
		settings := &config.Settings{}
		req := createFormRequest(map[string]string{
			"similarityThreshold": "0.7",
			"minMsgLen":           "20",
			"maxEmoji":            "3",
			"minSpamProbability":  "0.8",
			"paranoidMode":        "on",
			"firstMessagesCount":  "2",
			"multiLangLimit":      "5",
			"historySize":         "100",
		})

		updateSettingsFromForm(settings, req)

		assert.Equal(t, 0.7, settings.SimilarityThreshold)
		assert.Equal(t, 20, settings.MinMsgLen)
		assert.Equal(t, 3, settings.MaxEmoji)
		assert.Equal(t, 0.8, settings.MinSpamProbability)
		assert.True(t, settings.ParanoidMode)
		assert.Equal(t, 2, settings.FirstMessagesCount)
		assert.Equal(t, 5, settings.MultiLangWords)
		assert.Equal(t, 100, settings.History.Size)
	})

	t.Run("update storage settings", func(t *testing.T) {
		settings := &config.Settings{}
		req := createFormRequest(map[string]string{
			"samplesDataPath":   "/samples",
			"dynamicDataPath":   "/dynamic",
			"watchIntervalSecs": "10",
		})

		updateSettingsFromForm(settings, req)

		assert.Equal(t, "/samples", settings.Files.SamplesDataPath)
		assert.Equal(t, "/dynamic", settings.Files.DynamicDataPath)
		assert.Equal(t, 10, settings.Files.WatchInterval)
	})

	t.Run("StorageTimeout is not set from form", func(t *testing.T) {
		settings := &config.Settings{}
		req := createFormRequest(map[string]string{
			"storageTimeout": "30s",
		})

		updateSettingsFromForm(settings, req)

		// StorageTimeout should remain at the zero value
		assert.Equal(t, time.Duration(0), settings.Transient.StorageTimeout)
	})
}

// Helper to create a request with form values
func createFormRequest(values map[string]string) *http.Request {
	form := url.Values{}
	for k, v := range values {
		form.Add(k, v)
	}

	req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// parse the form
	req.PostForm = form

	return req
}
