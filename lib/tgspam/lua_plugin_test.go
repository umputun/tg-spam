//go:generate moq --out mocks/lua_plugin_engine.go --pkg mocks --skip-ensure --with-resets . LuaPluginEngine

package tgspam

import (
	"errors"
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
