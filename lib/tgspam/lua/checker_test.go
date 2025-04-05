package lua

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestChecker_LoadScript(t *testing.T) {
	// create a temporary script
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test.lua")
	err := os.WriteFile(scriptPath, []byte(`
		function check(req)
			return true, "test details"
		end
	`), 0666)
	require.NoError(t, err)

	// create a checker and load the script
	checker := NewChecker()
	defer checker.Close()

	err = checker.LoadScript(scriptPath)
	require.NoError(t, err)

	// test the loaded script
	checkFunc, err := checker.GetCheck("test")
	require.NoError(t, err)

	resp := checkFunc(spamcheck.Request{
		Msg:      "test message",
		UserID:   "user1",
		UserName: "testuser",
	})

	assert.True(t, resp.Spam)
	assert.Equal(t, "lua-test", resp.Name)
	assert.Equal(t, "test details", resp.Details)
}

func TestChecker_LoadInvalidScript(t *testing.T) {
	// create a temporary script with invalid Lua
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "invalid.lua")
	err := os.WriteFile(scriptPath, []byte(`
		this is not valid lua code
	`), 0666)
	require.NoError(t, err)

	// create a checker and try to load the script
	checker := NewChecker()
	defer checker.Close()

	err = checker.LoadScript(scriptPath)
	require.Error(t, err)
}

func TestChecker_LoadScriptWithoutCheckFunction(t *testing.T) {
	// create a temporary script without a check function
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "missing_check.lua")
	err := os.WriteFile(scriptPath, []byte(`
		function some_other_function()
			return true, "test details"
		end
	`), 0666)
	require.NoError(t, err)

	// create a checker and try to load the script
	checker := NewChecker()
	defer checker.Close()

	err = checker.LoadScript(scriptPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must define a 'check' function")
}

func TestChecker_LoadDirectory(t *testing.T) {
	// create a temporary directory with multiple scripts
	tmpDir := t.TempDir()

	script1Path := filepath.Join(tmpDir, "script1.lua")
	err := os.WriteFile(script1Path, []byte(`
		function check(req)
			return true, "script1 details"
		end
	`), 0666)
	require.NoError(t, err)

	script2Path := filepath.Join(tmpDir, "script2.lua")
	err = os.WriteFile(script2Path, []byte(`
		function check(req)
			return false, "script2 details"
		end
	`), 0666)
	require.NoError(t, err)

	// create a checker and load the directory
	checker := NewChecker()
	defer checker.Close()

	err = checker.LoadDirectory(tmpDir)
	require.NoError(t, err)

	// test the loaded scripts
	checkFunc1, err := checker.GetCheck("script1")
	require.NoError(t, err)
	resp1 := checkFunc1(spamcheck.Request{Msg: "test message"})
	assert.True(t, resp1.Spam)
	assert.Equal(t, "lua-script1", resp1.Name)
	assert.Equal(t, "script1 details", resp1.Details)

	checkFunc2, err := checker.GetCheck("script2")
	require.NoError(t, err)
	resp2 := checkFunc2(spamcheck.Request{Msg: "test message"})
	assert.False(t, resp2.Spam)
	assert.Equal(t, "lua-script2", resp2.Name)
	assert.Equal(t, "script2 details", resp2.Details)
}

func TestChecker_GetAllChecks(t *testing.T) {
	// create a temporary directory with multiple scripts
	tmpDir := t.TempDir()

	script1Path := filepath.Join(tmpDir, "script1.lua")
	err := os.WriteFile(script1Path, []byte(`
		function check(req)
			return true, "script1 details"
		end
	`), 0666)
	require.NoError(t, err)

	script2Path := filepath.Join(tmpDir, "script2.lua")
	err = os.WriteFile(script2Path, []byte(`
		function check(req)
			return false, "script2 details"
		end
	`), 0666)
	require.NoError(t, err)

	// create a checker and load the directory
	checker := NewChecker()
	defer checker.Close()

	err = checker.LoadDirectory(tmpDir)
	require.NoError(t, err)

	// test GetAllChecks
	checks := checker.GetAllChecks()
	assert.Len(t, checks, 2)
	assert.Contains(t, checks, "script1")
	assert.Contains(t, checks, "script2")
}

func TestChecker_Helpers(t *testing.T) {
	// create a temporary script using helper functions
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "helpers_test.lua")
	err := os.WriteFile(scriptPath, []byte(`
		function check(req)
			local lower = to_lower(req.msg)
			local count = count_substring(lower, "test")
			local has_prefix = starts_with(lower, "this")
			
			if count > 0 and has_prefix then
				return true, "detected pattern"
			end
			
			return false, "no pattern detected"
		end
	`), 0666)
	require.NoError(t, err)

	// create a checker and load the script
	checker := NewChecker()
	defer checker.Close()

	err = checker.LoadScript(scriptPath)
	require.NoError(t, err)

	// test the loaded script with helpers
	checkFunc, err := checker.GetCheck("helpers_test")
	require.NoError(t, err)

	// this should match our pattern
	resp1 := checkFunc(spamcheck.Request{Msg: "This is a test message"})
	assert.True(t, resp1.Spam)
	assert.Equal(t, "detected pattern", resp1.Details)

	// this shouldn't match (doesn't start with "this")
	resp2 := checkFunc(spamcheck.Request{Msg: "A test message"})
	assert.False(t, resp2.Spam)
	assert.Equal(t, "no pattern detected", resp2.Details)

	// this shouldn't match (doesn't contain "test")
	resp3 := checkFunc(spamcheck.Request{Msg: "This is a message"})
	assert.False(t, resp3.Spam)
	assert.Equal(t, "no pattern detected", resp3.Details)
}

func TestChecker_InvalidLuaExecution(t *testing.T) {
	// create a temporary script with runtime error
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "error.lua")
	err := os.WriteFile(scriptPath, []byte(`
		function check(req)
			-- Access non-existent field to cause runtime error
			local x = req.does_not_exist.something
			return true, "never reached"
		end
	`), 0666)
	require.NoError(t, err)

	// create a checker and load the script
	checker := NewChecker()
	defer checker.Close()

	err = checker.LoadScript(scriptPath)
	require.NoError(t, err)

	// test the script with runtime error
	checkFunc, err := checker.GetCheck("error")
	require.NoError(t, err)

	resp := checkFunc(spamcheck.Request{Msg: "test message"})
	assert.False(t, resp.Spam) // default to not spam on error
	assert.Contains(t, resp.Details, "error executing lua checker")
	assert.NotNil(t, resp.Error)
}
