//go:generate moq --out mocks/lua_plugin_engine.go --pkg mocks --skip-ensure --with-resets . LuaPluginEngine

package tgspam

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/spamcheck"
	"github.com/umputun/tg-spam/lib/tgspam/mocks"
)

func TestDetector_WithLuaEngine(t *testing.T) {
	config := Config{}
	config.LuaPlugins.Enabled = true
	config.LuaPlugins.PluginsDir = "/path/to/plugins"
	config.LuaPlugins.EnabledPlugins = []string{"plugin1", "plugin2"}

	detector := NewDetector(config)
	
	// Create mock Lua engine
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		LoadDirectoryFunc: func(dir string) error {
			assert.Equal(t, "/path/to/plugins", dir)
			return nil
		},
		GetCheckFunc: func(name string) (MetaCheck, error) {
			assert.Contains(t, []string{"plugin1", "plugin2"}, name)
			return func(req spamcheck.Request) spamcheck.Response {
				return spamcheck.Response{Name: "lua-" + name, Spam: true, Details: "test"}
			}, nil
		},
		CloseFunc: func() {
			// Do nothing in the test
		},
	}
	
	// Apply the mock engine
	err := detector.WithLuaEngine(mockLuaEngine)
	assert.NoError(t, err)
	
	// Check if mock was called correctly
	assert.Equal(t, 1, len(mockLuaEngine.LoadDirectoryCalls()))
	assert.Equal(t, 2, len(mockLuaEngine.GetCheckCalls()))
	assert.Equal(t, 2, len(detector.metaChecks))
}

func TestDetector_WithLuaEngine_Disabled(t *testing.T) {
	config := Config{}
	config.LuaPlugins.Enabled = false // Disabled

	detector := NewDetector(config)
	
	// Create mock Lua engine
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		LoadDirectoryFunc: func(dir string) error {
			return nil
		},
	}
	
	// Apply the mock engine
	err := detector.WithLuaEngine(mockLuaEngine)
	assert.NoError(t, err)
	
	// Check that LoadDirectory was not called
	assert.Equal(t, 0, len(mockLuaEngine.LoadDirectoryCalls()))
}

func TestDetector_WithLuaEngine_NoDirectory(t *testing.T) {
	config := Config{}
	config.LuaPlugins.Enabled = true
	config.LuaPlugins.PluginsDir = "" // No directory

	detector := NewDetector(config)
	
	// Create mock Lua engine
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		LoadDirectoryFunc: func(dir string) error {
			return nil
		},
	}
	
	// Apply the mock engine
	err := detector.WithLuaEngine(mockLuaEngine)
	assert.NoError(t, err)
	
	// Check that LoadDirectory was not called
	assert.Equal(t, 0, len(mockLuaEngine.LoadDirectoryCalls()))
}

func TestDetector_WithLuaEngine_LoadError(t *testing.T) {
	config := Config{}
	config.LuaPlugins.Enabled = true
	config.LuaPlugins.PluginsDir = "/path/to/plugins"

	detector := NewDetector(config)
	
	// Create mock Lua engine with load error
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		LoadDirectoryFunc: func(dir string) error {
			return errors.New("load error")
		},
		CloseFunc: func() {
			// Do nothing in the test
		},
	}
	
	// Apply the mock engine
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
	
	// Create mock Lua engine with GetCheck error
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		LoadDirectoryFunc: func(dir string) error {
			return nil
		},
		GetCheckFunc: func(name string) (MetaCheck, error) {
			return nil, errors.New("get check error")
		},
		CloseFunc: func() {
			// Do nothing in the test
		},
	}
	
	// Apply the mock engine
	err := detector.WithLuaEngine(mockLuaEngine)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get Lua check")
}

func TestDetector_WithLuaEngine_AllChecks(t *testing.T) {
	config := Config{}
	config.LuaPlugins.Enabled = true
	config.LuaPlugins.PluginsDir = "/path/to/plugins"
	// No EnabledPlugins specified, should load all

	detector := NewDetector(config)
	
	// Create mock Lua engine
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		LoadDirectoryFunc: func(dir string) error {
			return nil
		},
		GetAllChecksFunc: func() map[string]MetaCheck {
			return map[string]MetaCheck{
				"plugin1": func(req spamcheck.Request) spamcheck.Response {
					return spamcheck.Response{Name: "lua-plugin1", Spam: true, Details: "test1"}
				},
				"plugin2": func(req spamcheck.Request) spamcheck.Response {
					return spamcheck.Response{Name: "lua-plugin2", Spam: false, Details: "test2"}
				},
			}
		},
		CloseFunc: func() {
			// Do nothing in the test
		},
	}
	
	// Apply the mock engine
	err := detector.WithLuaEngine(mockLuaEngine)
	assert.NoError(t, err)
	
	// Check if mock was called correctly
	assert.Equal(t, 1, len(mockLuaEngine.LoadDirectoryCalls()))
	assert.Equal(t, 1, len(mockLuaEngine.GetAllChecksCalls()))
	assert.Equal(t, 2, len(detector.metaChecks))
}

func TestDetector_Reset_ClosesLuaEngine(t *testing.T) {
	detector := NewDetector(Config{})
	
	// Create mock Lua engine
	mockLuaEngine := &mocks.LuaPluginEngineMock{
		CloseFunc: func() {
			// Just count the call
		},
	}
	
	detector.luaEngine = mockLuaEngine
	
	// Reset the detector
	detector.Reset()
	
	// Check that Close was called
	assert.Equal(t, 1, len(mockLuaEngine.CloseCalls()))
	assert.Nil(t, detector.luaEngine)
}