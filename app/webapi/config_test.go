package webapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSaveConfigHandler(t *testing.T) {
	t.Run("successful save", func(t *testing.T) {
		configStore := &ConfigStoreInterfaceMock{
			SetObjectFunc: func(ctx context.Context, obj *Settings) error {
				return nil
			},
		}

		srv := Server{
			Config: Config{
				ConfigStore: configStore,
				Settings:    Settings{PrimaryGroup: "test-group"},
			},
		}

		req := httptest.NewRequest("POST", "/config/save", nil)
		w := httptest.NewRecorder()
		srv.saveConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)
		assert.Equal(t, 1, len(configStore.SetObjectCalls()))
		assert.Equal(t, "test-group", configStore.SetObjectCalls()[0].Obj.PrimaryGroup)
	})

	t.Run("successful save with HTMX request", func(t *testing.T) {
		configStore := &ConfigStoreInterfaceMock{
			SetObjectFunc: func(ctx context.Context, obj *Settings) error {
				return nil
			},
		}

		srv := Server{
			Config: Config{
				ConfigStore: configStore,
				Settings:    Settings{PrimaryGroup: "test-group"},
			},
		}

		req := httptest.NewRequest("POST", "/config/save", nil)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		srv.saveConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration saved successfully")
		assert.Contains(t, w.Body.String(), "alert-success")
		assert.Equal(t, 1, len(configStore.SetObjectCalls()))
	})

	t.Run("error saving configuration", func(t *testing.T) {
		configStore := &ConfigStoreInterfaceMock{
			SetObjectFunc: func(ctx context.Context, obj *Settings) error {
				return errors.New("test error")
			},
		}

		srv := Server{
			Config: Config{
				ConfigStore: configStore,
				Settings:    Settings{PrimaryGroup: "test-group"},
			},
		}

		req := httptest.NewRequest("POST", "/config/save", nil)
		w := httptest.NewRecorder()
		srv.saveConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "test error")
		assert.Equal(t, 1, len(configStore.SetObjectCalls()))
	})

	t.Run("no config store", func(t *testing.T) {
		srv := Server{
			Config: Config{
				ConfigStore: nil,
				Settings:    Settings{PrimaryGroup: "test-group"},
			},
		}

		req := httptest.NewRequest("POST", "/config/save", nil)
		w := httptest.NewRecorder()
		srv.saveConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration storage not available")
	})
}

func TestLoadConfigHandler(t *testing.T) {
	t.Run("successful load", func(t *testing.T) {
		configStore := &ConfigStoreInterfaceMock{
			GetObjectFunc: func(ctx context.Context, obj *Settings) error {
				obj.PrimaryGroup = "loaded-group"
				return nil
			},
		}

		srv := Server{
			Config: Config{
				ConfigStore: configStore,
				Settings:    Settings{PrimaryGroup: "test-group"},
			},
		}

		req := httptest.NewRequest("POST", "/config/load", nil)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)
		assert.Equal(t, 1, len(configStore.GetObjectCalls()))
		assert.Equal(t, "loaded-group", srv.Settings.PrimaryGroup) // settings updated
	})

	t.Run("successful load with HTMX request", func(t *testing.T) {
		configStore := &ConfigStoreInterfaceMock{
			GetObjectFunc: func(ctx context.Context, obj *Settings) error {
				obj.PrimaryGroup = "loaded-group"
				return nil
			},
		}

		srv := Server{
			Config: Config{
				ConfigStore: configStore,
				Settings:    Settings{PrimaryGroup: "test-group"},
			},
		}

		req := httptest.NewRequest("POST", "/config/load", nil)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration loaded successfully")
		assert.Contains(t, w.Body.String(), "alert-success")
		assert.Equal(t, "true", w.Header().Get("HX-Refresh"))
		assert.Equal(t, 1, len(configStore.GetObjectCalls()))
		assert.Equal(t, "loaded-group", srv.Settings.PrimaryGroup) // settings updated
	})

	t.Run("error loading configuration", func(t *testing.T) {
		configStore := &ConfigStoreInterfaceMock{
			GetObjectFunc: func(ctx context.Context, obj *Settings) error {
				return errors.New("test error")
			},
		}

		srv := Server{
			Config: Config{
				ConfigStore: configStore,
				Settings:    Settings{PrimaryGroup: "test-group"},
			},
		}

		req := httptest.NewRequest("POST", "/config/load", nil)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "test error")
		assert.Equal(t, 1, len(configStore.GetObjectCalls()))
		assert.Equal(t, "test-group", srv.Settings.PrimaryGroup) // settings not updated
	})

	t.Run("no config store", func(t *testing.T) {
		srv := Server{
			Config: Config{
				ConfigStore: nil,
				Settings:    Settings{PrimaryGroup: "test-group"},
			},
		}

		req := httptest.NewRequest("POST", "/config/load", nil)
		w := httptest.NewRecorder()
		srv.loadConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration storage not available")
	})
}

func TestUpdateConfigHandler(t *testing.T) {
	t.Run("update settings from form without save", func(t *testing.T) {
		srv := Server{
			Config: Config{
				Settings: Settings{PrimaryGroup: "test-group"},
			},
		}

		form := url.Values{}
		form.Add("primaryGroup", "updated-group")
		form.Add("noSpamReply", "on")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		srv.updateConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)
		assert.Equal(t, "updated-group", srv.Settings.PrimaryGroup)
		assert.True(t, srv.Settings.NoSpamReply)
	})

	t.Run("update settings from form with save to DB", func(t *testing.T) {
		configStore := &ConfigStoreInterfaceMock{
			SetObjectFunc: func(ctx context.Context, obj *Settings) error {
				return nil
			},
		}

		srv := Server{
			Config: Config{
				ConfigStore: configStore,
				Settings:    Settings{PrimaryGroup: "test-group"},
			},
		}

		form := url.Values{}
		form.Add("primaryGroup", "updated-group")
		form.Add("saveToDb", "true")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		srv.updateConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)
		assert.Equal(t, "updated-group", srv.Settings.PrimaryGroup)
		assert.Equal(t, 1, len(configStore.SetObjectCalls()))
		assert.Equal(t, "updated-group", configStore.SetObjectCalls()[0].Obj.PrimaryGroup)
	})

	t.Run("update settings with HTMX request", func(t *testing.T) {
		srv := Server{
			Config: Config{
				Settings: Settings{PrimaryGroup: "test-group"},
			},
		}

		form := url.Values{}
		form.Add("primaryGroup", "updated-group")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		srv.updateConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration updated successfully")
		assert.Contains(t, w.Body.String(), "alert-success")
		assert.Equal(t, "updated-group", srv.Settings.PrimaryGroup)
	})

	t.Run("error parsing form", func(t *testing.T) {
		srv := Server{
			Config: Config{
				Settings: Settings{PrimaryGroup: "test-group"},
			},
		}

		// create a request with invalid form data - using a malformed data that will trigger the error
		req := httptest.NewRequest("PUT", "/config", bytes.NewReader([]byte("%invalid%encoding")))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		srv.updateConfigHandler(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to parse form")
	})

	t.Run("error saving to DB", func(t *testing.T) {
		configStore := &ConfigStoreInterfaceMock{
			SetObjectFunc: func(ctx context.Context, obj *Settings) error {
				return errors.New("test error")
			},
		}

		srv := Server{
			Config: Config{
				ConfigStore: configStore,
				Settings:    Settings{PrimaryGroup: "test-group"},
			},
		}

		form := url.Values{}
		form.Add("primaryGroup", "updated-group")
		form.Add("saveToDb", "true")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		srv.updateConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "test error")
		assert.Equal(t, 1, len(configStore.SetObjectCalls()))
	})
}

func TestDeleteConfigHandler(t *testing.T) {
	t.Run("successful delete", func(t *testing.T) {
		configStore := &ConfigStoreInterfaceMock{
			DeleteFunc: func(ctx context.Context) error {
				return nil
			},
		}

		srv := Server{
			Config: Config{
				ConfigStore: configStore,
			},
		}

		req := httptest.NewRequest("DELETE", "/config", nil)
		w := httptest.NewRecorder()
		srv.deleteConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"ok"`)
		assert.Equal(t, 1, len(configStore.DeleteCalls()))
	})

	t.Run("successful delete with HTMX request", func(t *testing.T) {
		configStore := &ConfigStoreInterfaceMock{
			DeleteFunc: func(ctx context.Context) error {
				return nil
			},
		}

		srv := Server{
			Config: Config{
				ConfigStore: configStore,
			},
		}

		req := httptest.NewRequest("DELETE", "/config", nil)
		req.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		srv.deleteConfigHandler(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Configuration deleted successfully")
		assert.Contains(t, w.Body.String(), "alert-success")
		assert.Equal(t, 1, len(configStore.DeleteCalls()))
	})

	t.Run("error deleting configuration", func(t *testing.T) {
		configStore := &ConfigStoreInterfaceMock{
			DeleteFunc: func(ctx context.Context) error {
				return errors.New("test error")
			},
		}

		srv := Server{
			Config: Config{
				ConfigStore: configStore,
			},
		}

		req := httptest.NewRequest("DELETE", "/config", nil)
		w := httptest.NewRecorder()
		srv.deleteConfigHandler(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "test error")
		assert.Equal(t, 1, len(configStore.DeleteCalls()))
	})

	t.Run("no config store", func(t *testing.T) {
		srv := Server{
			Config: Config{
				ConfigStore: nil,
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
		settings := Settings{}
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

		updateSettingsFromForm(&settings, req)

		assert.Equal(t, "test-group", settings.PrimaryGroup)
		assert.Equal(t, "admin-group", settings.AdminGroup)
		assert.True(t, settings.NoSpamReply)
		assert.True(t, settings.CasEnabled)
		assert.Equal(t, []string{"user1", "user2", "user3"}, settings.SuperUsers)
		assert.True(t, settings.DebugModeEnabled)
		assert.True(t, settings.DryModeEnabled)
		assert.True(t, settings.TGDebugModeEnabled)
	})

	t.Run("update meta checks settings", func(t *testing.T) {
		settings := Settings{}
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

		updateSettingsFromForm(&settings, req)

		assert.True(t, settings.MetaEnabled)
		assert.Equal(t, 5, settings.MetaLinksLimit)
		assert.Equal(t, 3, settings.MetaMentionsLimit)
		assert.True(t, settings.MetaLinksOnly)
		assert.True(t, settings.MetaImageOnly)
		assert.True(t, settings.MetaVideoOnly)
		assert.True(t, settings.MetaAudioOnly)
		assert.True(t, settings.MetaForwarded)
		assert.True(t, settings.MetaKeyboard)
		assert.Equal(t, "abc", settings.MetaUsernameSymbols)
	})

	t.Run("update openAI settings", func(t *testing.T) {
		settings := Settings{}
		req := createFormRequest(map[string]string{
			"openAIEnabled":     "on",
			"openAIVeto":        "on",
			"openAIHistorySize": "10",
			"openAIModel":       "gpt-4",
		})

		updateSettingsFromForm(&settings, req)

		assert.True(t, settings.OpenAIEnabled)
		assert.True(t, settings.OpenAIVeto)
		assert.Equal(t, 10, settings.OpenAIHistorySize)
		assert.Equal(t, "gpt-4", settings.OpenAIModel)
	})

	t.Run("update lua plugins settings", func(t *testing.T) {
		settings := Settings{}
		form := url.Values{}
		form.Add("luaPluginsEnabled", "on")
		form.Add("luaDynamicReload", "on")
		form.Add("luaPluginsDir", "/plugins")
		form.Add("luaEnabledPlugins", "plugin1")
		form.Add("luaEnabledPlugins", "plugin2")

		req := httptest.NewRequest("PUT", "/config", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.PostForm = form

		updateSettingsFromForm(&settings, req)

		assert.True(t, settings.LuaPluginsEnabled)
		assert.True(t, settings.LuaDynamicReload)
		assert.Equal(t, "/plugins", settings.LuaPluginsDir)
		assert.Equal(t, []string{"plugin1", "plugin2"}, settings.LuaEnabledPlugins)
	})

	t.Run("update spam detection settings", func(t *testing.T) {
		settings := Settings{}
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

		updateSettingsFromForm(&settings, req)

		assert.Equal(t, 0.7, settings.SimilarityThreshold)
		assert.Equal(t, 20, settings.MinMsgLen)
		assert.Equal(t, 3, settings.MaxEmoji)
		assert.Equal(t, 0.8, settings.MinSpamProbability)
		assert.True(t, settings.ParanoidMode)
		assert.Equal(t, 2, settings.FirstMessagesCount)
		assert.Equal(t, 5, settings.MultiLangLimit)
		assert.Equal(t, 100, settings.HistorySize)
	})

	t.Run("update storage settings", func(t *testing.T) {
		settings := Settings{}
		req := createFormRequest(map[string]string{
			"samplesDataPath":   "/samples",
			"dynamicDataPath":   "/dynamic",
			"watchIntervalSecs": "10",
		})

		updateSettingsFromForm(&settings, req)

		assert.Equal(t, "/samples", settings.SamplesDataPath)
		assert.Equal(t, "/dynamic", settings.DynamicDataPath)
		assert.Equal(t, 10, settings.WatchIntervalSecs)
	})

	t.Run("StorageTimeout is not set from form", func(t *testing.T) {
		settings := Settings{}
		req := createFormRequest(map[string]string{
			"storageTimeout": "30s",
		})

		updateSettingsFromForm(&settings, req)

		// StorageTimeout should remain at the zero value
		assert.Equal(t, time.Duration(0), settings.StorageTimeout)
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
