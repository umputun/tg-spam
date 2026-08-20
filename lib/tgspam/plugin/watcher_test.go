package plugin

import (
	"bytes"
	"log"
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

// captureLog redirects the standard logger for the duration of the test and returns the accumulated output
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	flags, writer := log.Flags(), log.Writer()
	log.SetOutput(buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
	return buf
}

func TestWatcher_RemovedScriptEventCleared(t *testing.T) {
	// an uncleared event is reprocessed and re-logged on every tick for the life of the process
	tmpDir := t.TempDir()

	scriptPath := filepath.Join(tmpDir, "gone_script.lua")
	err := os.WriteFile(scriptPath, []byte(`
function check(request)
	return true, "still here"
end
	`), 0o644)
	require.NoError(t, err)

	checker := NewChecker()
	defer checker.Close()
	require.NoError(t, checker.LoadScript(scriptPath))

	watcher, err := NewWatcher(checker, tmpDir)
	require.NoError(t, err)

	require.NoError(t, os.Remove(scriptPath))
	watcher.mu.Lock()
	watcher.events[scriptPath] = time.Now().Add(-time.Second) // old enough to pass the debounce
	watcher.mu.Unlock()

	out := captureLog(t)
	watcher.processEvents()

	watcher.mu.Lock()
	pending := len(watcher.events)
	watcher.mu.Unlock()
	assert.Equal(t, 0, pending, "event for a removed script must not stay pending")

	assert.Contains(t, out.String(), "lua script file removed: gone_script, it stays in the plugin registry until restart")
	assert.NotContains(t, out.String(), "it was not loaded")

	// the script is still registered and executable: removing the file unloads nothing
	check, err := checker.GetCheck("gone_script")
	require.NoError(t, err)
	assert.Equal(t, "still here", check(createTestRequest()).Details)
}

func TestWatcher_RemovedUnloadedScriptEventCleared(t *testing.T) {
	tmpDir := t.TempDir()

	// a file that never loaded has no plugin behind it, so nothing must be reported as still loaded
	scriptPath := filepath.Join(tmpDir, "never_loaded.lua")

	checker := NewChecker()
	defer checker.Close()

	watcher, err := NewWatcher(checker, tmpDir)
	require.NoError(t, err)

	watcher.mu.Lock()
	watcher.events[scriptPath] = time.Now().Add(-time.Second)
	watcher.mu.Unlock()

	out := captureLog(t)
	watcher.processEvents()

	watcher.mu.Lock()
	pending := len(watcher.events)
	watcher.mu.Unlock()
	assert.Equal(t, 0, pending, "event for a removed script must not stay pending")

	assert.Contains(t, out.String(), "lua script file removed: never_loaded, it was not loaded")
	assert.NotContains(t, out.String(), "stays in the plugin registry")

	_, err = checker.GetCheck("never_loaded")
	assert.Error(t, err)
}

// Helper function to create a test request
func createTestRequest() spamcheck.Request {
	return spamcheck.Request{
		Msg:      "test message",
		UserID:   "user123",
		UserName: "testuser",
	}
}
