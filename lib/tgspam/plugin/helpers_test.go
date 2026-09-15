package plugin

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

func TestChecker_BasicHelpers(t *testing.T) {
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
	`), 0o666)
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

func TestChecker_AdvancedHelpers(t *testing.T) {
	// create a temporary script to test regex, contains_any, uppercase, trim, split, join, ends_with
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "advanced_helpers.lua")
	err := os.WriteFile(scriptPath, []byte(`
		function check(req)
			-- Test match_regex
			if not match_regex(req.msg, "[A-Z][a-z]+") then
				return false, "no capitalized words found"
			end

			-- Test to_upper and trim
			local upper = to_upper(req.msg)
			local trimmed = trim("  " .. req.msg .. "  ")
			if upper ~= string.upper(req.msg) or trimmed ~= req.msg then
				return false, "uppercase or trim failed"
			end

			-- Test split and join
			local words = split(req.msg, " ")
			local joined = join("-", words)
			if not string.find(joined, "-") then
				return false, "split/join failed"
			end

			-- Test contains_any with direct args
			if contains_any(req.msg, "spam", "scam", "fraud") then
				return true, "detected direct spam term"
			end

			-- Test contains_any with table
			local terms = {"bitcoin", "crypto", "investment", "opportunity"}
			local contains, found = contains_any(req.msg, terms)
			if contains then
				return true, "detected spam term from table: " .. found
			end

			-- Test ends_with
			if ends_with(req.msg, "now") then
				return true, "suspicious ending detected"
			end

			return false, "message passed all checks"
		end
	`), 0o666)
	require.NoError(t, err)

	// create a checker and load the script
	checker := NewChecker()
	defer checker.Close()

	err = checker.LoadScript(scriptPath)
	require.NoError(t, err)

	checkFunc, err := checker.GetCheck("advanced_helpers")
	require.NoError(t, err)

	testCases := []struct {
		name    string
		msg     string
		isSpam  bool
		details string
	}{
		{
			name:    "Clean message",
			msg:     "Hello this is a normal message",
			isSpam:  false,
			details: "message passed all checks",
		},
		{
			name:    "Spam term detection",
			msg:     "Hello this message contains spam inside",
			isSpam:  true,
			details: "detected direct spam term",
		},
		{
			name:    "Crypto term detection",
			msg:     "Check out this crypto project",
			isSpam:  true,
			details: "detected spam term from table: crypto",
		},
		{
			name:    "Suspicious ending",
			msg:     "You should buy this now",
			isSpam:  true,
			details: "suspicious ending detected",
		},
		{
			name:    "No capitalization",
			msg:     "no capitalization here",
			isSpam:  false,
			details: "no capitalized words found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := checkFunc(spamcheck.Request{Msg: tc.msg})
			assert.Equal(t, tc.isSpam, resp.Spam, "Spam detection mismatch")
			assert.Contains(t, resp.Details, tc.details, "Details should contain expected message")
		})
	}
}

func TestChecker_EdgeCaseHelpers(t *testing.T) {
	// create a temporary script to test edge cases for matchRegex and join
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "edge_case_helpers.lua")
	err := os.WriteFile(scriptPath, []byte(`
		function check(req)
			local result = {}

			-- Test matchRegex with invalid pattern
			local success, error_msg = match_regex(req.msg, "[") -- Invalid regex pattern
			result.invalid_regex = not success
			result.error_msg = error_msg or ""

			-- Test join with empty table
			local empty_table = {}
			local join1 = join(",", empty_table)
			result.empty_join = join1 == ""

			-- Test join with no arguments (edge case)
			if req.msg == "test_no_args" then
				local join2 = join(",")
				result.no_args_join = join2 == ""
			end

			-- Test join with multiple individual arguments
			local join3 = join("|", "arg1", "arg2", "arg3")
			result.multi_arg_join = join3 == "arg1|arg2|arg3"

			if req.msg == "trigger_error" then
				return true, "error handling tested"
			end

			return false, table.concat({
				"regex error: " .. tostring(result.invalid_regex),
				"error message: " .. result.error_msg,
				"empty join: " .. tostring(result.empty_join),
				"multi-arg join: " .. tostring(result.multi_arg_join)
			}, ", ")
		end
	`), 0o666)
	require.NoError(t, err)

	// create a checker and load the script
	checker := NewChecker()
	defer checker.Close()

	err = checker.LoadScript(scriptPath)
	require.NoError(t, err)

	checkFunc, err := checker.GetCheck("edge_case_helpers")
	require.NoError(t, err)

	// test regex error handling
	t.Run("RegexErrorHandling", func(t *testing.T) {
		resp := checkFunc(spamcheck.Request{Msg: "test message"})
		assert.False(t, resp.Spam)
		assert.Contains(t, resp.Details, "regex error: true", "Should detect invalid regex")
		assert.Contains(t, resp.Details, "invalid pattern:", "Should return error message")
	})

	// test join with empty table and multiple arguments
	t.Run("JoinEdgeCases", func(t *testing.T) {
		resp := checkFunc(spamcheck.Request{Msg: "test message"})
		assert.Contains(t, resp.Details, "empty join: true", "Should handle empty table join")
		assert.Contains(t, resp.Details, "multi-arg join: true", "Should handle multiple arguments join")
	})

	// test join with no arguments
	t.Run("JoinNoArgs", func(t *testing.T) {
		resp := checkFunc(spamcheck.Request{Msg: "test_no_args"})
		assert.False(t, resp.Spam)
	})
}

func TestHTTPRequest(t *testing.T) {
	// create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
		case "/headers":
			headerValue := r.Header.Get("X-Test-Header")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(headerValue))
		case "/post":
			if r.Method != "POST" {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"received":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// test basic GET request
	t.Run("basic GET", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		L.Push(lua.LString(server.URL + "/ok"))
		ret := httpRequest(L)
		assert.Equal(t, 3, ret)
		assert.JSONEq(t, `{"status":"ok"}`, string(L.Get(-3).(lua.LString)))
		assert.InDelta(t, float64(200), float64(L.Get(-2).(lua.LNumber)), 0.001)
		assert.Equal(t, lua.LNil, L.Get(-1))
		L.Pop(3)
	})

	// test with custom headers
	t.Run("with headers", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		L.Push(lua.LString(server.URL + "/headers"))
		L.Push(lua.LString("GET"))
		headers := L.NewTable()
		headers.RawSetString("X-Test-Header", lua.LString("test-value"))
		L.Push(headers)
		ret := httpRequest(L)
		assert.Equal(t, 3, ret)
		assert.Equal(t, lua.LString("test-value"), L.Get(-3))
		assert.InDelta(t, float64(200), float64(L.Get(-2).(lua.LNumber)), 0.001)
		assert.Equal(t, lua.LNil, L.Get(-1))
		L.Pop(3)
	})

	// test POST request with body
	t.Run("POST with body", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		L.Push(lua.LString(server.URL + "/post"))
		L.Push(lua.LString("POST"))
		L.Push(lua.LNil) // no headers
		L.Push(lua.LString(`{"test":"data"}`))
		ret := httpRequest(L)
		assert.Equal(t, 3, ret)
		assert.JSONEq(t, `{"received":true}`, string(L.Get(-3).(lua.LString)))
		assert.InDelta(t, float64(200), float64(L.Get(-2).(lua.LNumber)), 0.001)
		assert.Equal(t, lua.LNil, L.Get(-1))
		L.Pop(3)
	})

	// test 404 error
	t.Run("404 error", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		L.Push(lua.LString(server.URL + "/not-found"))
		ret := httpRequest(L)
		assert.Equal(t, 3, ret)
		assert.InDelta(t, float64(404), float64(L.Get(-2).(lua.LNumber)), 0.001)
		L.Pop(3)
	})

	// test invalid URL
	t.Run("invalid URL", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		L.Push(lua.LString("invalid-url"))
		ret := httpRequest(L)
		assert.Equal(t, 3, ret)
		assert.Equal(t, lua.LNil, L.Get(-3))
		assert.InDelta(t, float64(0), float64(L.Get(-2).(lua.LNumber)), 0.001)
		assert.NotEqual(t, lua.LNil, L.Get(-1)) // should have error message
		L.Pop(3)
	})
}

func TestJSONEncodeDecode(t *testing.T) {
	t.Run("encode and decode", func(t *testing.T) {
		L := lua.NewState()
		defer L.Close()

		// create a test table
		table := L.NewTable()
		table.RawSetString("string", lua.LString("value"))
		table.RawSetString("number", lua.LNumber(42))
		table.RawSetString("bool", lua.LBool(true))

		// create a nested table
		nested := L.NewTable()
		nested.RawSetString("key", lua.LString("nested value"))
		table.RawSetString("nested", nested)

		// create an array
		array := L.NewTable()
		array.RawSetInt(1, lua.LString("first"))
		array.RawSetInt(2, lua.LString("second"))
		table.RawSetString("array", array)

		// encode to JSON
		L.Push(table)
		ret := jsonEncode(L)
		assert.Equal(t, 2, ret)

		// get encoded JSON and verify correct encoding
		jsonStr := L.Get(-2).String()
		assert.Contains(t, jsonStr, `"string":"value"`)
		assert.Contains(t, jsonStr, `"number":42`)
		assert.Contains(t, jsonStr, `"bool":true`)
		assert.Contains(t, jsonStr, `"nested":{"key":"nested value"}`)
		assert.Contains(t, jsonStr, `"array":["first","second"]`)

		// create a new state for decode test to avoid any stack issues
		L2 := lua.NewState()
		defer L2.Close()

		// now decode it back
		L2.Push(lua.LString(jsonStr))
		ret = jsonDecode(L2)
		assert.Equal(t, 2, ret)
		assert.Equal(t, lua.LNil, L2.Get(-1)) // no error

		// check the decoded table
		decoded := L2.Get(-2).(*lua.LTable)
		assert.Equal(t, lua.LString("value"), decoded.RawGetString("string"))
		assert.InDelta(t, float64(42), float64(decoded.RawGetString("number").(lua.LNumber)), 0.001)
		assert.Equal(t, lua.LBool(true), decoded.RawGetString("bool"))

		// check nested objects
		nestedDecoded := decoded.RawGetString("nested").(*lua.LTable)
		assert.Equal(t, lua.LString("nested value"), nestedDecoded.RawGetString("key"))

		// check array
		arrayDecoded := decoded.RawGetString("array").(*lua.LTable)
		assert.Equal(t, lua.LString("first"), arrayDecoded.RawGetInt(1))
		assert.Equal(t, lua.LString("second"), arrayDecoded.RawGetInt(2))
	})
}

func TestURLEncode(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello+world"},
		{"a=1&b=2", "a%3D1%26b%3D2"},
		{"привет мир", "%D0%BF%D1%80%D0%B8%D0%B2%D0%B5%D1%82+%D0%BC%D0%B8%D1%80"},
		{"!@#$%^&*()", "%21%40%23%24%25%5E%26%2A%28%29"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()

			L.Push(lua.LString(tc.input))
			ret := urlEncode(L)
			assert.Equal(t, 1, ret)
			assert.Equal(t, lua.LString(tc.expected), L.Get(-1))
			L.Pop(1)
		})
	}
}
