package plugin

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestWatcher_Start(t *testing.T) {
	// create a temporary directory for test plugins
	tmpDir := t.TempDir()

	// create a Checker and Watcher
	checker := NewChecker()
	defer checker.Close()
	watcher, err := NewWatcher(checker, tmpDir)
	require.NoError(t, err)

	// test starting the watcher
	err = watcher.Start()
	require.NoError(t, err)

	// test that starting twice doesn't cause an error
	err = watcher.Start()
	require.NoError(t, err)

	// stop the watcher
	watcher.Stop()
}

func TestWatcher_NonExistentDirectory(t *testing.T) {
	// try to create a watcher with a non-existent directory
	checker := NewChecker()
	defer checker.Close()
	watcher, err := NewWatcher(checker, "/nonexistent-directory")
	require.NoError(t, err)

	// starting the watcher should fail
	err = watcher.Start()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestWatcher_FileReload(t *testing.T) {
	// create a temporary directory for test plugins
	tmpDir := t.TempDir()

	// create a simple test Lua script
	scriptPath := filepath.Join(tmpDir, "test_script.lua")
	err := os.WriteFile(scriptPath, []byte(`
function check(request)
	return false, "initial version"
end
	`), 0o644)
	require.NoError(t, err)

	// create a checker and load the script
	checker := NewChecker()
	defer checker.Close()
	err = checker.LoadScript(scriptPath)
	require.NoError(t, err)

	// verify the initial script works
	check, err := checker.GetCheck("test_script")
	require.NoError(t, err)
	response := check(createTestRequest())
	assert.Equal(t, "initial version", response.Details)
	assert.False(t, response.Spam)

	// create a watcher and start it
	watcher, err := NewWatcher(checker, tmpDir)
	require.NoError(t, err)
	checker.SetWatcher(watcher)
	err = watcher.Start()
	require.NoError(t, err)
	defer watcher.Stop()

	// modify the script
	err = os.WriteFile(scriptPath, []byte(`
function check(request)
	return true, "updated version"
end
	`), 0o644)
	require.NoError(t, err)

	// manually add event to the watcher's event queue
	watcher.mu.Lock()
	watcher.events[scriptPath] = time.Now().Add(-time.Second) // make it old enough to process
	watcher.mu.Unlock()

	// manually process events
	watcher.processEvents()

	// wait a moment for the reload to complete
	time.Sleep(100 * time.Millisecond)

	// check that the script was reloaded
	check, err = checker.GetCheck("test_script")
	require.NoError(t, err)
	response = check(createTestRequest())
	assert.Equal(t, "updated version", response.Details)
	assert.True(t, response.Spam)
}

func TestWatcher_HandleEvent(t *testing.T) {
	// create a temporary directory for test plugins
	tmpDir := t.TempDir()

	// create a checker and watcher
	checker := NewChecker()
	defer checker.Close()
	watcher, err := NewWatcher(checker, tmpDir)
	require.NoError(t, err)

	// test handling a create event
	scriptPath := filepath.Join(tmpDir, "new_script.lua")
	event := fsnotify.Event{
		Name: scriptPath,
		Op:   fsnotify.Create,
	}

	watcher.handleEvent(event)

	// verify the event was added to the queue
	watcher.mu.Lock()
	_, exists := watcher.events[scriptPath]
	watcher.mu.Unlock()
	assert.True(t, exists, "Event should be added to the queue")

	// test handling a non-lua file
	nonLuaPath := filepath.Join(tmpDir, "non_script.txt")
	event = fsnotify.Event{
		Name: nonLuaPath,
		Op:   fsnotify.Create,
	}

	watcher.handleEvent(event)

	// verify the event was NOT added to the queue
	watcher.mu.Lock()
	_, exists = watcher.events[nonLuaPath]
	watcher.mu.Unlock()
	assert.False(t, exists, "Non-lua event should not be added to the queue")
}

// Helper function to create a test request
func createTestRequest() spamcheck.Request {
	return spamcheck.Request{
		Msg:      "test message",
		UserID:   "user123",
		UserName: "testuser",
	}
}
