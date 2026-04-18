package plugin

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// RegisterHelpers registers common helper functions for Lua scripts
func (c *Checker) RegisterHelpers() {
	// string manipulation helpers
	c.vm.SetGlobal("count_substring", c.vm.NewFunction(countSubstring))
	c.vm.SetGlobal("match_regex", c.vm.NewFunction(matchRegex))
	c.vm.SetGlobal("contains_any", c.vm.NewFunction(containsAny))
	c.vm.SetGlobal("to_lower", c.vm.NewFunction(toLowerCase))
	c.vm.SetGlobal("to_upper", c.vm.NewFunction(toUpperCase))
	c.vm.SetGlobal("trim", c.vm.NewFunction(trim))
	c.vm.SetGlobal("split", c.vm.NewFunction(split))
	c.vm.SetGlobal("join", c.vm.NewFunction(join))
	c.vm.SetGlobal("starts_with", c.vm.NewFunction(startsWith))
	c.vm.SetGlobal("ends_with", c.vm.NewFunction(endsWith))

	// HTTP and JSON helpers
	c.vm.SetGlobal("http_request", c.vm.NewFunction(httpRequest))
	c.vm.SetGlobal("json_encode", c.vm.NewFunction(jsonEncode))
	c.vm.SetGlobal("json_decode", c.vm.NewFunction(jsonDecode))
	c.vm.SetGlobal("url_encode", c.vm.NewFunction(urlEncode))
}

// countSubstring counts occurrences of a substring
func countSubstring(l *lua.LState) int {
	str := l.CheckString(1)
	substr := l.CheckString(2)
	count := strings.Count(str, substr)
	l.Push(lua.LNumber(count))
	return 1
}

// matchRegex checks if a string matches a regex pattern
func matchRegex(l *lua.LState) int {
	text := l.CheckString(1)
	pattern := l.CheckString(2)

	re, err := regexp.Compile(pattern)
	if err != nil {
		l.Push(lua.LBool(false))
		l.Push(lua.LString("invalid pattern: " + err.Error()))
		return 2
	}

	matched := re.MatchString(text)
	l.Push(lua.LBool(matched))
	return 1
}

// containsAny checks if a string contains any of the given substrings
func containsAny(l *lua.LState) int {
	str := l.CheckString(1)

	// check if second argument is a table
	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		table := l.ToTable(2)
		var items []string

		table.ForEach(func(_, v lua.LValue) {
			if v.Type() == lua.LTString {
				items = append(items, v.String())
			}
		})

		for _, item := range items {
			if strings.Contains(str, item) {
				l.Push(lua.LBool(true))
				l.Push(lua.LString(item))
				return 2
			}
		}

		l.Push(lua.LBool(false))
		return 1
	}

	// if not a table, treat remaining arguments as strings
	for i := 2; i <= l.GetTop(); i++ {
		substr := l.CheckString(i)
		if strings.Contains(str, substr) {
			l.Push(lua.LBool(true))
			l.Push(lua.LString(substr))
			return 2
		}
	}

	l.Push(lua.LBool(false))
	return 1
}

// toLowerCase converts a string to lowercase
func toLowerCase(l *lua.LState) int {
	str := l.CheckString(1)
	l.Push(lua.LString(strings.ToLower(str)))
	return 1
}

// toUpperCase converts a string to uppercase
func toUpperCase(l *lua.LState) int {
	str := l.CheckString(1)
	l.Push(lua.LString(strings.ToUpper(str)))
	return 1
}

// trim removes whitespace from both ends of a string
func trim(l *lua.LState) int {
	str := l.CheckString(1)
	l.Push(lua.LString(strings.TrimSpace(str)))
	return 1
}

// split splits a string by a separator
func split(l *lua.LState) int {
	str := l.CheckString(1)
	sep := l.CheckString(2)

	parts := strings.Split(str, sep)

	resultTable := l.NewTable()
	for i, part := range parts {
		resultTable.RawSetInt(i+1, lua.LString(part))
	}

	l.Push(resultTable)
	return 1
}

// join joins strings with a separator
func join(l *lua.LState) int {
	sep := l.CheckString(1)

	if l.GetTop() >= 2 && l.Get(2).Type() == lua.LTTable {
		table := l.ToTable(2)
		var items []string

		table.ForEach(func(_, v lua.LValue) {
			if v.Type() == lua.LTString {
				items = append(items, v.String())
			}
		})

		l.Push(lua.LString(strings.Join(items, sep)))
		return 1
	}

	var strs []string
	for i := 2; i <= l.GetTop(); i++ {
		strs = append(strs, l.CheckString(i))
	}

	l.Push(lua.LString(strings.Join(strs, sep)))
	return 1
}

// startsWith checks if a string starts with a prefix
func startsWith(l *lua.LState) int {
	str := l.CheckString(1)
	prefix := l.CheckString(2)
	l.Push(lua.LBool(strings.HasPrefix(str, prefix)))
	return 1
}

// endsWith checks if a string ends with a suffix
func endsWith(l *lua.LState) int {
	str := l.CheckString(1)
	suffix := l.CheckString(2)
	l.Push(lua.LBool(strings.HasSuffix(str, suffix)))
	return 1
}

// httpRequest makes an HTTP request to the given URL and returns the response
// Lua usage: response, status_code, err = http_request(url, [method], [headers], [body], [timeout])
// Example: http_request("https://example.com/api", "POST", {["Content-Type"]="application/json"}, "{}", 10)
func httpRequest(l *lua.LState) int {
	urlStr := l.CheckString(1)

	// optional parameters with defaults
	method := "GET"
	if l.GetTop() >= 2 && l.Get(2) != lua.LNil {
		method = l.CheckString(2)
	}

	// default timeout of 5 seconds, can be overridden as last parameter
	timeout := 5.0
	if l.GetTop() >= 5 && l.Get(5) != lua.LNil {
		timeout = float64(l.CheckNumber(5))
	}

	// create HTTP client with timeout
	client := &http.Client{
		Timeout: time.Duration(timeout * float64(time.Second)),
	}

	// handle request body if provided
	var body io.Reader
	if l.GetTop() >= 4 && l.Get(4) != lua.LNil {
		body = strings.NewReader(l.CheckString(4))
	}

	// create the request with context for proper cancellation
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LNumber(0))
		l.Push(lua.LString(err.Error()))
		return 3
	}

	// add headers if provided
	if l.GetTop() >= 3 && l.Get(3) != lua.LNil && l.Get(3).Type() == lua.LTTable {
		headers := l.CheckTable(3)
		headers.ForEach(func(k, v lua.LValue) {
			if k.Type() == lua.LTString && v.Type() == lua.LTString {
				req.Header.Set(k.String(), v.String())
			}
		})
	}

	// add default User-Agent header if not already set
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "TG-Spam-Lua-Plugin")
	}

	// execute the request
	resp, err := client.Do(req) //nolint:gosec // url constructed from plugin configuration
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LNumber(0))
		l.Push(lua.LString(err.Error()))
		return 3
	}
	defer resp.Body.Close()

	// read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LNumber(resp.StatusCode))
		l.Push(lua.LString(err.Error()))
		return 3
	}

	// return response body, status code, and nil error
	l.Push(lua.LString(string(respBody)))
	l.Push(lua.LNumber(resp.StatusCode))
	l.Push(lua.LNil) // no error
	return 3
}

// jsonEncode converts a Lua table to a JSON string
// Lua usage: json_string = json_encode(table)
func jsonEncode(l *lua.LState) int {
	// function to convert Lua value to Go value
	var convertValue func(lv lua.LValue) any
	convertValue = func(lv lua.LValue) any {
		switch lv.Type() {
		case lua.LTNil:
			return nil
		case lua.LTBool:
			return lua.LVAsBool(lv)
		case lua.LTNumber:
			return float64(lua.LVAsNumber(lv))
		case lua.LTString:
			return lua.LVAsString(lv)
		case lua.LTTable:
			table := lv.(*lua.LTable)

			// check if it's an array
			maxn := table.MaxN()
			if maxn > 0 {
				// it's an array
				array := make([]any, 0, maxn)
				for i := 1; i <= maxn; i++ {
					val := table.RawGetInt(i)
					if val != lua.LNil {
						array = append(array, convertValue(val))
					}
				}
				return array
			}

			// it's a map/object
			obj := make(map[string]any)
			table.ForEach(func(key, value lua.LValue) {
				if key.Type() == lua.LTString {
					obj[key.String()] = convertValue(value)
				}
			})
			return obj
		default:
			return nil
		}
	}

	// check we have at least one argument and it's a table
	if l.GetTop() < 1 {
		l.Push(lua.LString(""))
		l.Push(lua.LString("no input provided"))
		return 2
	}

	luaValue := l.Get(1)
	goValue := convertValue(luaValue)

	// marshal to JSON
	jsonBytes, err := json.Marshal(goValue)
	if err != nil {
		l.Push(lua.LString(""))
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LString(string(jsonBytes)))
	l.Push(lua.LNil) // no error
	return 2
}

// jsonDecode parses a JSON string into a Lua table
// Lua usage: lua_table, err = json_decode(json_string)
func jsonDecode(l *lua.LState) int {
	jsonStr := l.CheckString(1)

	// function to convert Go value to Lua value
	var convertValue func(val any) lua.LValue
	convertValue = func(val any) lua.LValue {
		if val == nil {
			return lua.LNil
		}

		switch v := val.(type) {
		case bool:
			return lua.LBool(v)
		case float64:
			return lua.LNumber(v)
		case string:
			return lua.LString(v)
		case []any:
			// array
			table := l.NewTable()
			for i, item := range v {
				table.RawSetInt(i+1, convertValue(item))
			}
			return table
		case map[string]any:
			// object
			table := l.NewTable()
			for key, value := range v {
				table.RawSetString(key, convertValue(value))
			}
			return table
		default:
			return lua.LNil
		}
	}

	// unmarshal JSON
	var data any
	if err := json.Unmarshal([]byte(jsonStr), &data); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(convertValue(data))
	l.Push(lua.LNil) // no error
	return 2
}

// urlEncode encodes a string for safe use in URLs
// Lua usage: encoded = url_encode(str)
func urlEncode(l *lua.LState) int {
	str := l.CheckString(1)
	l.Push(lua.LString(url.QueryEscape(str)))
	return 1
}
