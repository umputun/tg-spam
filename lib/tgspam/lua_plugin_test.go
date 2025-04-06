//go:generate moq --out mocks/lua_plugin_engine.go --pkg mocks --skip-ensure --with-resets . LuaPluginEngine

package tgspam

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam/lua"
	"github.com/umputun/tg-spam/lib/tgspam/mocks"
)

func TestDetector_WithLuaEngine(t *testing.T) {
	config := Config{}
	config.LuaPlugins.Enabled = true
	config.LuaPlugins.PluginsDir = "/path/to/plugins"
	config.LuaPlugins.EnabledPlugins = []string{"plugin1", "plugin2"}

	detector := NewDetector(config)

	// create mock Lua engine
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		LoadDirectoryFunc: func(dir string) error {
			assert.Equal(t, "/path/to/plugins", dir)
			return nil
		},
		GetCheckFunc: func(name string) (lua.PluginCheck, error) {
			assert.Contains(t, []string{"plugin1", "plugin2"}, name)
			return func(req spamcheck.Request) spamcheck.Response {
				return spamcheck.Response{Name: "lua-" + name, Spam: true, Details: "test"}
			}, nil
		},
		CloseFunc: func() {
			// do nothing in the test
		},
	}

	// apply the mock engine
	err := detector.WithLuaEngine(mockLuaEngine)
	assert.NoError(t, err)

	// check if mock was called correctly
	assert.Equal(t, 1, len(mockLuaEngine.LoadDirectoryCalls()))
	assert.Equal(t, 2, len(mockLuaEngine.GetCheckCalls()))
	assert.Equal(t, 2, len(detector.luaChecks))
	assert.Equal(t, 0, len(detector.metaChecks))
}

func TestDetector_WithLuaEngine_Disabled(t *testing.T) {
	config := Config{}
	config.LuaPlugins.Enabled = false // disabled

	detector := NewDetector(config)

	// create mock Lua engine
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		LoadDirectoryFunc: func(dir string) error {
			return nil
		},
	}

	// apply the mock engine
	err := detector.WithLuaEngine(mockLuaEngine)
	assert.NoError(t, err)

	// check that LoadDirectory was not called
	assert.Equal(t, 0, len(mockLuaEngine.LoadDirectoryCalls()))
}

func TestDetector_WithLuaEngine_NoDirectory(t *testing.T) {
	config := Config{}
	config.LuaPlugins.Enabled = true
	config.LuaPlugins.PluginsDir = "" // no directory

	detector := NewDetector(config)

	// create mock Lua engine
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		LoadDirectoryFunc: func(dir string) error {
			return nil
		},
	}

	// apply the mock engine
	err := detector.WithLuaEngine(mockLuaEngine)
	assert.NoError(t, err)

	// check that LoadDirectory was not called
	assert.Equal(t, 0, len(mockLuaEngine.LoadDirectoryCalls()))
}

func TestDetector_WithLuaEngine_LoadError(t *testing.T) {
	config := Config{}
	config.LuaPlugins.Enabled = true
	config.LuaPlugins.PluginsDir = "/path/to/plugins"

	detector := NewDetector(config)

	// create mock Lua engine with load error
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		LoadDirectoryFunc: func(dir string) error {
			return errors.New("load error")
		},
		CloseFunc: func() {
			// do nothing in the test
		},
	}

	// apply the mock engine
	err := detector.WithLuaEngine(mockLuaEngine)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load Lua plugins")
}

func TestDetector_WithLuaEngine_GetCheckError(t *testing.T) {
	config := Config{}
	config.LuaPlugins.Enabled = true
	config.LuaPlugins.PluginsDir = "/path/to/plugins"
	config.LuaPlugins.EnabledPlugins = []string{"plugin1"}

	detector := NewDetector(config)

	// create mock Lua engine with GetCheck error
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		LoadDirectoryFunc: func(dir string) error {
			return nil
		},
		GetCheckFunc: func(name string) (lua.PluginCheck, error) {
			return nil, errors.New("get check error")
		},
		CloseFunc: func() {
			// do nothing in the test
		},
	}

	// apply the mock engine
	err := detector.WithLuaEngine(mockLuaEngine)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get Lua check")
}

func TestDetector_WithLuaEngine_AllChecks(t *testing.T) {
	config := Config{}
	config.LuaPlugins.Enabled = true
	config.LuaPlugins.PluginsDir = "/path/to/plugins"
	// no EnabledPlugins specified, should load all

	detector := NewDetector(config)

	// create mock Lua engine
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		LoadDirectoryFunc: func(dir string) error {
			return nil
		},
		GetAllChecksFunc: func() map[string]lua.PluginCheck {
			return map[string]lua.PluginCheck{
				"plugin1": func(req spamcheck.Request) spamcheck.Response {
					return spamcheck.Response{Name: "lua-plugin1", Spam: true, Details: "test1"}
				},
				"plugin2": func(req spamcheck.Request) spamcheck.Response {
					return spamcheck.Response{Name: "lua-plugin2", Spam: false, Details: "test2"}
				},
			}
		},
		CloseFunc: func() {
			// do nothing in the test
		},
	}

	// apply the mock engine
	err := detector.WithLuaEngine(mockLuaEngine)
	assert.NoError(t, err)

	// check if mock was called correctly
	assert.Equal(t, 1, len(mockLuaEngine.LoadDirectoryCalls()))
	assert.Equal(t, 1, len(mockLuaEngine.GetAllChecksCalls()))
	assert.Equal(t, 2, len(detector.luaChecks))
	assert.Equal(t, 0, len(detector.metaChecks))
}

func TestDetector_Reset_ClosesLuaEngine(t *testing.T) {
	detector := NewDetector(Config{})

	// create mock Lua engine
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		CloseFunc: func() {
			// just count the call
		},
	}

	detector.luaEngine = mockLuaEngine

	// reset the detector
	detector.Reset()

	// check that Close was called and luaChecks are cleared
	assert.Equal(t, 1, len(mockLuaEngine.CloseCalls()))
	assert.Nil(t, detector.luaEngine)
	assert.Empty(t, detector.luaChecks)
}

func TestDetector_GetLuaPluginNames(t *testing.T) {
	t.Run("no Lua engine", func(t *testing.T) {
		detector := NewDetector(Config{})
		assert.Empty(t, detector.GetLuaPluginNames())
	})

	t.Run("Lua disabled", func(t *testing.T) {
		config := Config{}
		config.LuaPlugins.Enabled = false
		detector := NewDetector(config)

		// create mock Lua engine
		mockLuaEngine := &mocks.LuaPluginEngineMock{
			GetAllChecksFunc: func() map[string]lua.PluginCheck {
				return map[string]lua.PluginCheck{
					"plugin1": nil,
					"plugin2": nil,
				}
			},
		}

		detector.luaEngine = mockLuaEngine
		assert.Empty(t, detector.GetLuaPluginNames())
		assert.Equal(t, 0, len(mockLuaEngine.GetAllChecksCalls()))
	})

	t.Run("with plugins", func(t *testing.T) {
		config := Config{}
		config.LuaPlugins.Enabled = true
		detector := NewDetector(config)

		// create mock Lua engine with some plugins
		mockLuaEngine := &mocks.LuaPluginEngineMock{
			GetAllChecksFunc: func() map[string]lua.PluginCheck {
				return map[string]lua.PluginCheck{
					"plugin1": func(req spamcheck.Request) spamcheck.Response {
						return spamcheck.Response{Name: "lua-plugin1", Spam: true, Details: "test"}
					},
					"plugin2": func(req spamcheck.Request) spamcheck.Response {
						return spamcheck.Response{Name: "lua-plugin2", Spam: false, Details: "test"}
					},
					"plugin3": func(req spamcheck.Request) spamcheck.Response {
						return spamcheck.Response{Name: "lua-plugin3", Spam: true, Details: "test"}
					},
				}
			},
		}

		detector.luaEngine = mockLuaEngine
		pluginNames := detector.GetLuaPluginNames()

		assert.Equal(t, 1, len(mockLuaEngine.GetAllChecksCalls()))
		assert.Len(t, pluginNames, 3)

		// make sure the names are sorted
		sort.Strings(pluginNames)
		assert.Equal(t, []string{"plugin1", "plugin2", "plugin3"}, pluginNames)
	})
}

func TestDetector_WithRealLuaPlugins(t *testing.T) {
	// set up configuration to use testdata directory
	config := Config{}
	config.LuaPlugins.Enabled = true
	config.LuaPlugins.PluginsDir = "./testdata"
	config.LuaPlugins.EnabledPlugins = []string{"domain_blacklist", "repeat_chars", "simple_test"}

	// create and initialize detector with real Lua plugins
	detector := NewDetector(config)
	engine := lua.NewChecker()
	defer engine.Close()

	err := detector.WithLuaEngine(engine)
	require.NoError(t, err)

	// verify that all plugins were loaded
	assert.Len(t, detector.luaChecks, 3)

	// helper function to find a specific check in the checks slice
	findCheck := func(checks []spamcheck.Response, name string) *spamcheck.Response {
		for _, check := range checks {
			if check.Name == name {
				return &check
			}
		}
		return nil
	}

	t.Run("DomainBlacklist", func(t *testing.T) {
		// test with blacklisted domain
		req := spamcheck.Request{
			Msg:      "Check out https://suspicious.xyz for great deals!",
			UserID:   "user1",
			UserName: "testuser",
		}
		isSpam, checks := detector.Check(req)
		t.Logf("checks: %+v", checks)
		assert.True(t, isSpam, "message with suspicious domain should be detected as spam")
		domainCheck := findCheck(checks, "lua-domain_blacklist")
		assert.NotNil(t, domainCheck, "domain_blacklist check should be present")
		assert.True(t, domainCheck.Spam, "domain_blacklist should detect this as spam")
		assert.Contains(t, domainCheck.Details, "blacklisted TLD: .xyz", "should contain details about the blacklisted TLD")

		// test with legitimate domain
		req = spamcheck.Request{
			Msg:      "Check out https://legitimate.com for great deals!",
			UserID:   "user1",
			UserName: "testuser",
		}
		isSpam, checks = detector.Check(req)
		assert.False(t, isSpam, "message with legitimate domain shouldn't be detected as spam")
		domainCheck = findCheck(checks, "lua-domain_blacklist")
		if domainCheck != nil {
			assert.False(t, domainCheck.Spam, "legitimate domain shouldn't be detected as spam")
		}
	})

	t.Run("RepeatChars", func(t *testing.T) {
		req := spamcheck.Request{
			Msg:      "Hellooooooo everyone!!!!!",
			UserID:   "user1",
			UserName: "testuser",
		}
		isSpam, checks := detector.Check(req)
		assert.True(t, isSpam, "message with excessive repeating chars should be detected as spam")
		repeatCheck := findCheck(checks, "lua-repeat_chars")
		assert.NotNil(t, repeatCheck, "repeat_chars check should be present")
		assert.True(t, repeatCheck.Spam, "repeat_chars should detect this as spam")
		assert.Contains(t, repeatCheck.Details, "excessive repeated characters", "should contain details about excessive character repetition")

		req = spamcheck.Request{
			Msg:      "Hello everyone!",
			UserID:   "user1",
			UserName: "testuser",
		}
		isSpam, checks = detector.Check(req)
		assert.False(t, isSpam, "normal message shouldn't be detected as spam")
		repeatCheck = findCheck(checks, "lua-repeat_chars")
		if repeatCheck != nil {
			assert.False(t, repeatCheck.Spam, "normal message shouldn't be detected as spam")
		}
	})

	t.Run("SimpleTest", func(t *testing.T) {
		req := spamcheck.Request{
			Msg:      "Great crypto investment opportunity!",
			UserID:   "user1",
			UserName: "testuser",
		}
		isSpam, checks := detector.Check(req)
		assert.True(t, isSpam, "message with spam keyword should be detected as spam")
		simpleCheck := findCheck(checks, "lua-simple_test")
		assert.NotNil(t, simpleCheck, "simple_test check should be present")
		assert.True(t, simpleCheck.Spam, "simple_test should detect this as spam")
		assert.Contains(t, simpleCheck.Details, "detected spam keyword: crypto", "should contain details about the detected keyword")

		req = spamcheck.Request{
			Msg:      "Just a normal message without keywords.",
			UserID:   "user1",
			UserName: "testuser",
		}
		isSpam, checks = detector.Check(req)
		assert.False(t, isSpam, "normal message shouldn't be detected as spam")
		simpleCheck = findCheck(checks, "lua-simple_test")
		if simpleCheck != nil {
			assert.False(t, simpleCheck.Spam, "normal message shouldn't be detected as spam")
		}
	})
}

func TestDetector_WithAPICheckLuaPlugin(t *testing.T) {
	// create a mock HTTP server that simulates a spam detection API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// verify request method and headers
		require.Equal(t, "POST", r.Method, "request method should be POST")
		require.Equal(t, "application/json", r.Header.Get("Content-Type"), "Content-Type header should be set")
		require.Equal(t, "application/json", r.Header.Get("Accept"), "Accept header should be set")

		// parse the request body
		var requestData map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&requestData)
		require.NoError(t, err, "should be able to decode request JSON")
		defer r.Body.Close()

		// verify the request structure
		require.Contains(t, requestData, "message", "request should contain message field")
		require.Contains(t, requestData, "user_id", "request should contain user_id field")
		require.Contains(t, requestData, "user_name", "request should contain user_name field")

		// prepare different responses based on the message content
		message, ok := requestData["message"].(string)
		require.True(t, ok, "message should be a string")

		// different API responses for different message types
		if message == "This message contains spam from API" {
			// respond with a spam detection
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"is_spam":    true,
				"confidence": 0.95,
				"reason":     "API spam pattern detected",
			})
			return
		} else if message == "This causes an error response" {
			// simulate an API error
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "Internal server error",
			})
			return
		} else if message == "This causes invalid JSON" {
			// simulate invalid JSON response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("{invalid json"))
			return
		} else {
			// normal clean message response
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"is_spam":    false,
				"confidence": 0.1,
				"reason":     "No spam patterns detected",
			})
		}
	}))
	defer server.Close()

	// create a new Lua engine for our test
	modifiedApiCheck := lua.NewChecker()

	// get the path to the testdata directory where we can create a temporary file
	tmpScript := filepath.Join("./testdata", "api_check_test.lua")

	// write a modified version of the script with the test server URL
	luaCode := fmt.Sprintf(`
-- api_check.lua (test version with mock server)
function check(req)
  -- Normalize the message
  local msg = to_lower(req.msg)
  
  -- Check for common spam patterns before making API requests
  if count_substring(msg, "crypto") > 0 or count_substring(msg, "investment") > 0 then
    return true, "local detection: spam keywords found"
  end
  
  -- For special test_mode handling
  if req.msg == "__test_mode__" then
    return false, "API check skipped in test mode"
  end
  
  -- Prepare data for the API request
  local request_data = {
    message = req.msg,
    user_id = req.user_id,
    user_name = req.user_name
  }
  
  -- Create headers
  local headers = {
    ["Content-Type"] = "application/json",
    ["Accept"] = "application/json"
  }
  
  -- Convert data to JSON
  local json_data, json_err = json_encode(request_data)
  if json_err then
    return false, "json encoding error: " .. json_err
  end
  
  -- Using test server URL
  local endpoint = "%s"
  
  -- Make a POST request to the mock API with a short timeout
  local response, status, err = http_request(endpoint, "POST", headers, json_data, 3)
  
  -- Handle request errors
  if err then
    return false, "API request failed: " .. err .. " (allowing message)"
  end
  
  -- Check for non-200 status codes
  if status ~= 200 then
    return false, "API returned status " .. status .. " (allowing message)"
  end
  
  -- Parse API response
  local result, parse_err = json_decode(response)
  if parse_err then
    return false, "API response parse error: " .. parse_err .. " (allowing message)"
  end
  
  -- Process API result
  if result.is_spam then
    return true, "API detection: " .. (result.reason or "unknown reason")
  end
  
  -- Message is not spam according to the API
  return false, "API verified: not spam"
end
`, server.URL)

	err := os.WriteFile(tmpScript, []byte(luaCode), 0644)
	require.NoError(t, err, "should load modified api_check.lua")

	// load the modified script
	err = modifiedApiCheck.LoadScript(tmpScript)
	require.NoError(t, err, "should load the script")

	// create detector with API check plugin
	config := Config{}
	config.LuaPlugins.Enabled = true
	// set empty plugins dir to prevent automatic loading
	config.LuaPlugins.PluginsDir = ""

	// print the available checks
	allChecks := modifiedApiCheck.GetAllChecks()
	t.Logf("Available Lua checks: %v", allChecks)

	// create detector
	detector := NewDetector(config)
	err = detector.WithLuaEngine(modifiedApiCheck)
	require.NoError(t, err, "should initialize detector with Lua engine")

	// add the Lua checks directly
	for name, check := range allChecks {
		detector.luaChecks = append(detector.luaChecks, check)
		t.Logf("Added Lua check: %s", name)
	}

	// print the detector Lua checks
	t.Logf("Detector Lua checks: %d", len(detector.luaChecks))
	t.Logf("Active Lua plugin names: %v", detector.GetLuaPluginNames())

	// helper function to find a specific check in the checks slice
	findCheck := func(checks []spamcheck.Response, name string) *spamcheck.Response {
		for _, check := range checks {
			if check.Name == name {
				return &check
			}
		}
		return nil
	}

	t.Run("LocalSpamDetection", func(t *testing.T) {
		// message with a keyword that should be caught locally without API call
		req := spamcheck.Request{
			Msg:      "Great crypto opportunity!",
			UserID:   "user1",
			UserName: "testuser",
		}
		isSpam, checks := detector.Check(req)

		assert.True(t, isSpam, "message with local spam keyword should be detected as spam")
		apiCheck := findCheck(checks, "lua-api_check_test")
		require.NotNil(t, apiCheck, "api_check should be present")
		assert.True(t, apiCheck.Spam, "api_check should detect this as spam")
		assert.Contains(t, apiCheck.Details, "local detection", "should indicate local detection")
	})

	t.Run("APISpamDetection", func(t *testing.T) {
		// message that will trigger a spam response from the mock API
		req := spamcheck.Request{
			Msg:      "This message contains spam from API",
			UserID:   "user1",
			UserName: "testuser",
		}
		isSpam, checks := detector.Check(req)

		assert.True(t, isSpam, "message detected as spam by API should be marked as spam")
		apiCheck := findCheck(checks, "lua-api_check_test")
		require.NotNil(t, apiCheck, "api_check should be present")
		assert.True(t, apiCheck.Spam, "api_check should detect this as spam")
		assert.Contains(t, apiCheck.Details, "API detection", "should indicate API detection")
		assert.Contains(t, apiCheck.Details, "API spam pattern detected", "should include the reason from API")
	})

	t.Run("APICleanDetection", func(t *testing.T) {
		// clean message that the API will verify as not spam
		req := spamcheck.Request{
			Msg:      "This is a perfectly normal message",
			UserID:   "user1",
			UserName: "testuser",
		}
		isSpam, checks := detector.Check(req)

		assert.False(t, isSpam, "clean message should not be marked as spam")
		apiCheck := findCheck(checks, "lua-api_check_test")
		require.NotNil(t, apiCheck, "api_check should be present")
		assert.False(t, apiCheck.Spam, "api_check should not mark this as spam")
		assert.Contains(t, apiCheck.Details, "API verified: not spam", "should indicate API verification")
	})

	t.Run("APIErrorHandling", func(t *testing.T) {
		// message that will trigger a server error from the mock API
		req := spamcheck.Request{
			Msg:      "This causes an error response",
			UserID:   "user1",
			UserName: "testuser",
		}
		isSpam, checks := detector.Check(req)

		assert.False(t, isSpam, "message causing API error should fail open (not spam)")
		apiCheck := findCheck(checks, "lua-api_check_test")
		require.NotNil(t, apiCheck, "api_check should be present")
		assert.False(t, apiCheck.Spam, "api_check should fail open on API error")
		assert.Contains(t, apiCheck.Details, "API returned status 500", "should indicate API error status")
	})

	t.Run("JSONParseErrorHandling", func(t *testing.T) {
		// message that will trigger an invalid JSON response from the mock API
		req := spamcheck.Request{
			Msg:      "This causes invalid JSON",
			UserID:   "user1",
			UserName: "testuser",
		}
		isSpam, checks := detector.Check(req)

		assert.False(t, isSpam, "message causing JSON parse error should fail open (not spam)")
		apiCheck := findCheck(checks, "lua-api_check_test")
		require.NotNil(t, apiCheck, "api_check should be present")
		assert.False(t, apiCheck.Spam, "api_check should fail open on JSON parse error")
		assert.Contains(t, apiCheck.Details, "API response parse error", "should indicate JSON parse error")
	})

	t.Run("TestModeSkipsAPI", func(t *testing.T) {
		// special test mode message that should skip API call
		req := spamcheck.Request{
			Msg:      "__test_mode__",
			UserID:   "user1",
			UserName: "testuser",
		}
		isSpam, checks := detector.Check(req)

		assert.False(t, isSpam, "test mode message should not be marked as spam")
		apiCheck := findCheck(checks, "lua-api_check_test")
		require.NotNil(t, apiCheck, "api_check should be present")
		assert.False(t, apiCheck.Spam, "api_check should not mark test mode as spam")
		assert.Contains(t, apiCheck.Details, "skipped in test mode", "should indicate test mode")
	})

	// clean up temporary file
	defer os.Remove(tmpScript)
}

func TestDetector_WithLuaEngine_DynamicReload(t *testing.T) {
	// create a temporary directory for test plugins
	tmpDir := t.TempDir()

	// create a test plugin
	scriptPath := filepath.Join(tmpDir, "dynamic_reload.lua")
	err := os.WriteFile(scriptPath, []byte(`
function check(request)
	return false, "original plugin"
end
	`), 0644)
	require.NoError(t, err)

	// set up configuration with dynamic reload enabled
	config := Config{}
	config.LuaPlugins.Enabled = true
	config.LuaPlugins.PluginsDir = tmpDir
	config.LuaPlugins.DynamicReload = true

	detector := NewDetector(config)

	// create real Lua engine
	engine := lua.NewChecker()
	defer engine.Close()

	// apply the engine with dynamic reload
	err = detector.WithLuaEngine(engine)
	require.NoError(t, err)

	// verify the Checker has a watcher set
	_, ok := detector.luaEngine.(*lua.Checker)
	require.True(t, ok)

	// we can't directly check if watcher is set since it's not exported,
	// but we can verify the plugin is loaded and working
	names := detector.GetLuaPluginNames()
	assert.Contains(t, names, "dynamic_reload")

	// run a SpamCheck with the plugin
	req := spamcheck.Request{
		Msg:      "test message",
		UserID:   "user1",
		UserName: "testuser",
	}
	_, results := detector.Check(req)

	// find the plugin's result
	var luaResult *spamcheck.Response
	for _, res := range results {
		if res.Name == "lua-dynamic_reload" {
			luaResult = &res
			break
		}
	}

	require.NotNil(t, luaResult, "Lua plugin result should be present")
	assert.Equal(t, "original plugin", luaResult.Details)
	assert.False(t, luaResult.Spam)
}
