package plugin

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	`), 0o666)
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

func TestChecker_UserFields(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "user_fields.lua")
	err := os.WriteFile(scriptPath, []byte(`
		function check(req)
			if req.first_name == "John" and req.last_name == "Doe" and req.is_premium then
				return true, "premium user John Doe"
			end
			return false, "not matched"
		end
	`), 0o666)
	require.NoError(t, err)

	checker := NewChecker()
	defer checker.Close()
	err = checker.LoadScript(scriptPath)
	require.NoError(t, err)

	checkFunc, err := checker.GetCheck("user_fields")
	require.NoError(t, err)

	t.Run("all fields set", func(t *testing.T) {
		resp := checkFunc(spamcheck.Request{
			Msg: "test", UserID: "1", UserName: "johndoe",
			FirstName: "John", LastName: "Doe", IsPremium: true,
		})
		assert.True(t, resp.Spam)
		assert.Equal(t, "premium user John Doe", resp.Details)
	})

	t.Run("fields not matching", func(t *testing.T) {
		resp := checkFunc(spamcheck.Request{
			Msg: "test", UserID: "2", UserName: "jane",
			FirstName: "Jane", LastName: "Smith", IsPremium: false,
		})
		assert.False(t, resp.Spam)
		assert.Equal(t, "not matched", resp.Details)
	})

	t.Run("empty fields", func(t *testing.T) {
		resp := checkFunc(spamcheck.Request{Msg: "test", UserID: "3", UserName: "anon"})
		assert.False(t, resp.Spam)
		assert.Equal(t, "not matched", resp.Details)
	})
}

func TestChecker_MetaFields(t *testing.T) {
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "meta_fields.lua")
	err := os.WriteFile(scriptPath, []byte(`
		function check(req)
			if req.meta.has_contact and req.meta.message_id == 42 and req.meta.has_giveaway and req.meta.has_external_reply then
				return true, "meta matched"
			end
			return false, "not matched"
		end
	`), 0o666)
	require.NoError(t, err)

	checker := NewChecker()
	defer checker.Close()
	err = checker.LoadScript(scriptPath)
	require.NoError(t, err)

	checkFunc, err := checker.GetCheck("meta_fields")
	require.NoError(t, err)

	t.Run("all meta fields set", func(t *testing.T) {
		resp := checkFunc(spamcheck.Request{
			Msg: "test", UserID: "1", UserName: "user",
			Meta: spamcheck.MetaData{HasContact: true, MessageID: 42, HasGiveaway: true, HasExternalReply: true},
		})
		assert.True(t, resp.Spam)
		assert.Equal(t, "meta matched", resp.Details)
	})

	t.Run("meta fields not matching", func(t *testing.T) {
		resp := checkFunc(spamcheck.Request{
			Msg: "test", UserID: "2", UserName: "user",
			Meta: spamcheck.MetaData{HasContact: false, MessageID: 10},
		})
		assert.False(t, resp.Spam)
		assert.Equal(t, "not matched", resp.Details)
	})
}

func TestChecker_LoadInvalidScript(t *testing.T) {
	// create a temporary script with invalid Lua
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "invalid.lua")
	err := os.WriteFile(scriptPath, []byte(`
		this is not valid lua code
	`), 0o666)
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
	`), 0o666)
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
	`), 0o666)
	require.NoError(t, err)

	script2Path := filepath.Join(tmpDir, "script2.lua")
	err = os.WriteFile(script2Path, []byte(`
		function check(req)
			return false, "script2 details"
		end
	`), 0o666)
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
	`), 0o666)
	require.NoError(t, err)

	script2Path := filepath.Join(tmpDir, "script2.lua")
	err = os.WriteFile(script2Path, []byte(`
		function check(req)
			return false, "script2 details"
		end
	`), 0o666)
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
	`), 0o666)
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
	assert.Error(t, resp.Error)
}

func TestChecker_ReloadScript(t *testing.T) {
	// create a temporary directory for test plugins
	tmpDir := t.TempDir()

	// create a simple test Lua script
	scriptPath := filepath.Join(tmpDir, "reload_test.lua")
	err := os.WriteFile(scriptPath, []byte(`
function check(request)
	return false, "original version"
end
	`), 0o644)
	require.NoError(t, err)

	// create a checker and load the script
	checker := NewChecker()
	defer checker.Close()
	err = checker.LoadScript(scriptPath)
	require.NoError(t, err)

	// verify the script works
	check, err := checker.GetCheck("reload_test")
	require.NoError(t, err)
	response := check(spamcheck.Request{
		Msg:      "test message",
		UserID:   "user123",
		UserName: "testuser",
	})
	assert.Equal(t, "original version", response.Details)
	assert.False(t, response.Spam)

	// modify the script file
	err = os.WriteFile(scriptPath, []byte(`
function check(request)
	return true, "reloaded version"
end
	`), 0o644)
	require.NoError(t, err)

	// reload the script
	err = checker.ReloadScript(scriptPath)
	require.NoError(t, err)

	// verify the reloaded script works with new behavior
	check, err = checker.GetCheck("reload_test")
	require.NoError(t, err)
	response = check(spamcheck.Request{
		Msg:      "test message",
		UserID:   "user123",
		UserName: "testuser",
	})
	assert.Equal(t, "reloaded version", response.Details)
	assert.True(t, response.Spam)
}

func TestChecker_ReloadNonExistentScript(t *testing.T) {
	checker := NewChecker()
	defer checker.Close()

	// try to reload a non-existent script
	err := checker.ReloadScript("/path/to/nonexistent/script.lua")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load Lua script")
}

func TestChecker_ConcurrentAccess(t *testing.T) {
	// create a temporary directory for test plugins
	tmpDir := t.TempDir()

	// create a simple test Lua script
	scriptPath := filepath.Join(tmpDir, "concurrent_test.lua")
	err := os.WriteFile(scriptPath, []byte(`
function check(request)
	return false, "concurrent test"
end
	`), 0o644)
	require.NoError(t, err)

	// create a checker and load the script
	checker := NewChecker()
	defer checker.Close()
	err = checker.LoadScript(scriptPath)
	require.NoError(t, err)

	// get the check function
	check, err := checker.GetCheck("concurrent_test")
	require.NoError(t, err)

	// simulate concurrent access - this will deadlock if locks aren't implemented correctly
	done := make(chan bool)
	go func() {
		// access the checker from a goroutine
		for range 10 {
			resp := check(spamcheck.Request{Msg: "test"})
			assert.Equal(t, "concurrent test", resp.Details)
		}
		done <- true
	}()

	// reload the script multiple times
	for range 5 {
		err = checker.ReloadScript(scriptPath)
		assert.NoError(t, err)
	}

	// wait for the goroutine to finish
	<-done
}

func TestChecker_ConcurrentChecks(t *testing.T) {
	tmpDir := t.TempDir()

	// the script echoes the user id back so a corrupted stack shows up as a mismatched result
	// rather than only as a race detector report
	scriptPath := filepath.Join(tmpDir, "parallel_test.lua")
	err := os.WriteFile(scriptPath, []byte(`
function check(request)
	return request.user_id == "spammer", "seen " .. request.user_id
end
	`), 0o644)
	require.NoError(t, err)

	checker := NewChecker()
	defer checker.Close()
	require.NoError(t, checker.LoadScript(scriptPath))

	check, err := checker.GetCheck("parallel_test")
	require.NoError(t, err)

	// every goroutine drives the same shared lua state; without the write lock in
	// the closure returned by createResultCheck, this fails under -race or panics inside gopher-lua
	const workers, iterations = 8, 50
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			userID := fmt.Sprintf("user%d", i)
			if i == 0 {
				userID = "spammer"
			}
			for range iterations {
				resp := check(spamcheck.Request{Msg: "test message", UserID: userID})
				assert.Equal(t, "lua-parallel_test", resp.Name)
				assert.Equal(t, "seen "+userID, resp.Details)
				assert.Equal(t, userID == "spammer", resp.Spam)
			}
		})
	}
	wg.Wait()
}

func TestChecker_ReloadReachesAlreadyObtainedCheck(t *testing.T) {
	// the detector keeps a Check for its lifetime, so refreshing only the registry leaves it on the old script
	tmpDir := t.TempDir()

	scriptPath := filepath.Join(tmpDir, "held_test.lua")
	err := os.WriteFile(scriptPath, []byte(`
function check(request)
	return true, "original version"
end
	`), 0o644)
	require.NoError(t, err)

	checker := NewChecker()
	defer checker.Close()
	require.NoError(t, checker.LoadScript(scriptPath))

	held, err := checker.GetCheck("held_test")
	require.NoError(t, err)
	resp := held(createTestRequest())
	require.Equal(t, "original version", resp.Details)

	err = os.WriteFile(scriptPath, []byte(`
function check(request)
	return false, "reloaded version"
end
	`), 0o644)
	require.NoError(t, err)
	require.NoError(t, checker.ReloadScript(scriptPath))

	resp = held(createTestRequest())
	assert.Equal(t, "reloaded version", resp.Details, "check obtained before the reload must run the new script")
	assert.False(t, resp.Spam)
}

func TestChecker_ReloadReachesHeldResultChecks(t *testing.T) {
	// Detector stores ResultCheck values for its lifetime, so a captured function would pin both
	// the details and the approval bit to the version loaded first
	tmpDir := t.TempDir()

	scriptPath := filepath.Join(tmpDir, "held_result.lua")
	err := os.WriteFile(scriptPath, []byte(`
function check(request)
	return false, "original version", false
end
	`), 0o644)
	require.NoError(t, err)

	checker := NewChecker()
	defer checker.Close()
	require.NoError(t, checker.LoadScript(scriptPath))

	held, err := checker.GetResultCheck("held_result")
	require.NoError(t, err)
	heldAll := checker.GetAllResultChecks()["held_result"]
	require.NotNil(t, heldAll)

	res := held(createTestRequest())
	require.Equal(t, "original version", res.Response.Details)
	require.False(t, res.Approved)

	err = os.WriteFile(scriptPath, []byte(`
function check(request)
	return false, "reloaded version", true
end
	`), 0o644)
	require.NoError(t, err)
	require.NoError(t, checker.ReloadScript(scriptPath))

	for name, check := range map[string]ResultCheck{"GetResultCheck": held, "GetAllResultChecks": heldAll} {
		res = check(createTestRequest())
		assert.Equal(t, "reloaded version", res.Response.Details, "%s must run the new script", name)
		assert.True(t, res.Approved, "%s must see the new approval", name)
	}
}

func TestChecker_FailedReloadKeepsRegistryEntry(t *testing.T) {
	tmpDir := t.TempDir()

	scriptPath := filepath.Join(tmpDir, "broken_test.lua")
	err := os.WriteFile(scriptPath, []byte(`
function check(request)
	return true, "original version"
end
	`), 0o644)
	require.NoError(t, err)

	checker := NewChecker()
	defer checker.Close()
	require.NoError(t, checker.LoadScript(scriptPath))
	held, err := checker.GetCheck("broken_test")
	require.NoError(t, err)

	err = os.WriteFile(scriptPath, []byte(`function check(request) this is not lua`), 0o644)
	require.NoError(t, err)
	require.Error(t, checker.ReloadScript(scriptPath))

	// a broken edit must not deregister a script that is still being executed
	assert.Contains(t, checker.GetAllChecks(), "broken_test", "plugin stays registered after a failed reload")
	_, err = checker.GetCheck("broken_test")
	require.NoError(t, err)

	resp := held(createTestRequest())
	assert.Equal(t, "original version", resp.Details)
	assert.True(t, resp.Spam)
}

func TestChecker_FailedReloadCanStillMutateSharedGlobals(t *testing.T) {
	// keeping the old entry does not make a failed reload a no-op: the candidate runs in the shared VM
	tmpDir := t.TempDir()

	scriptPath := filepath.Join(tmpDir, "partial_test.lua")
	err := os.WriteFile(scriptPath, []byte(`
GREETING = "original version"
MAIN_VM = true
function check(request)
	return true, GREETING
end
	`), 0o644)
	require.NoError(t, err)

	checker := NewChecker()
	defer checker.Close()
	require.NoError(t, checker.LoadScript(scriptPath))
	held, err := checker.GetCheck("partial_test")
	require.NoError(t, err)
	require.Equal(t, "original version", held(createTestRequest()).Details)

	// MAIN_VM exists only in the shared VM, so this validates in the throwaway state and fails after reassigning GREETING
	err = os.WriteFile(scriptPath, []byte(`
GREETING = "mutated before failure"
if MAIN_VM then error("boom") end
function check(request)
	return true, GREETING
end
	`), 0o644)
	require.NoError(t, err)
	require.Error(t, checker.ReloadScript(scriptPath))

	assert.Contains(t, checker.GetAllChecks(), "partial_test", "the entry survives the failed reload")
	assert.Equal(t, "mutated before failure", held(createTestRequest()).Details, "but its behavior does not")
}

func TestChecker_ResultCheck(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantSpam     bool
		wantApproved bool
		wantError    bool
	}{
		{name: "absent approval remains compatible", body: `return false, "clean"`},
		{name: "explicit false does not approve", body: `return false, "clean", false`},
		{name: "exact true approves ham", body: `return false, "trusted message", true`, wantApproved: true},
		{name: "true cannot approve spam", body: `return true, "spam", true`, wantSpam: true},
		{name: "string is not an approval", body: `return false, "clean", "false"`},
		{name: "runtime error does not approve", body: `local value = request.missing.value; return false, value, true`, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := NewChecker()
			defer checker.Close()

			scriptPath := filepath.Join(t.TempDir(), "result.lua")
			script := "function check(request)\n" + tt.body + "\nend\n"
			require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o600))
			require.NoError(t, checker.LoadScript(scriptPath))

			check, err := checker.GetResultCheck("result")
			require.NoError(t, err)
			result := check(spamcheck.Request{Msg: "test"})

			assert.Equal(t, tt.wantSpam, result.Response.Spam)
			assert.Equal(t, tt.wantApproved, result.Approved)
			assert.Equal(t, "lua-result", result.Response.Name)
			if tt.wantError {
				assert.Error(t, result.Response.Error)
			} else {
				assert.NoError(t, result.Response.Error)
			}
		})
	}
}

func TestChecker_ResultCheckWarnings(t *testing.T) {
	var logs bytes.Buffer
	previousWriter := log.Writer()
	previousFlags := log.Flags()
	previousPrefix := log.Prefix()
	log.SetOutput(&logs)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
		log.SetPrefix(previousPrefix)
	})

	checker := NewChecker()
	defer checker.Close()
	scriptPath := filepath.Join(t.TempDir(), "types.lua")
	script := `
function check(request)
    if request.msg == "false" then
        return false, "clean", false
    end
    if request.msg == "nil" then
        return false, "clean"
    end
    if request.msg == "string" then
        return false, "clean", "false"
    end
    if request.msg == "number" then
        return false, "clean", 1
    end
    return true, "spam", true
end
`
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o600))
	require.NoError(t, checker.LoadScript(scriptPath))
	check, err := checker.GetResultCheck("types")
	require.NoError(t, err)

	for range 2 {
		assert.False(t, check(spamcheck.Request{Msg: "false"}).Approved)
		assert.False(t, check(spamcheck.Request{Msg: "nil"}).Approved)
		assert.False(t, check(spamcheck.Request{Msg: "string"}).Approved)
		assert.False(t, check(spamcheck.Request{Msg: "number"}).Approved)
		assert.False(t, check(spamcheck.Request{Msg: "conflict"}).Approved)
	}

	output := logs.String()
	assert.Equal(t, 1, strings.Count(output, `lua checker "types" returned approval value with type string`))
	assert.Equal(t, 1, strings.Count(output, `lua checker "types" returned approval value with type number`))
	assert.Equal(t, 1, strings.Count(output, `lua checker "types" returned approval=true with spam=true`))
	assert.NotContains(t, output, `approval value "false"`)
	assert.Len(t, strings.Split(strings.TrimSpace(output), "\n"), 3)
}

func TestChecker_ResultAdapters(t *testing.T) {
	checker := NewChecker()
	defer checker.Close()
	scriptPath := filepath.Join(t.TempDir(), "adapter.lua")
	require.NoError(t, os.WriteFile(scriptPath, []byte(`
function check(request)
    return false, "approved", true
end
`), 0o600))
	require.NoError(t, checker.LoadScript(scriptPath))

	legacyCheck, err := checker.GetCheck("adapter")
	require.NoError(t, err)
	assert.Equal(t, spamcheck.Response{Name: "lua-adapter", Details: "approved"}, legacyCheck(spamcheck.Request{}))

	resultChecks := checker.GetAllResultChecks()
	require.Contains(t, resultChecks, "adapter")
	assert.True(t, resultChecks["adapter"](spamcheck.Request{}).Approved)
}
