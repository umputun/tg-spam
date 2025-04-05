package lua

import (
	"regexp"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// RegisterHelpers registers common helper functions for Lua scripts
func (c *LuaChecker) RegisterHelpers() {
	c.vm.SetGlobal("count_substring", c.vm.NewFunction(countSubstring))
	c.vm.SetGlobal("match_regex", c.vm.NewFunction(matchRegex))
	c.vm.SetGlobal("contains_any", c.vm.NewFunction(containsAny))
	c.vm.SetGlobal("to_lower", c.vm.NewFunction(toLowerCase))
	c.vm.SetGlobal("to_upper", c.vm.NewFunction(toUpperCase))
	c.vm.SetGlobal("trim", c.vm.NewFunction(trim))
	c.vm.SetGlobal("split", c.vm.NewFunction(split))
}

// countSubstring counts occurrences of a substring
func countSubstring(L *lua.LState) int {
	str := L.CheckString(1)
	substr := L.CheckString(2)
	count := strings.Count(str, substr)
	L.Push(lua.LNumber(count))
	return 1
}

// matchRegex checks if a string matches a regex pattern
func matchRegex(L *lua.LState) int {
	text := L.CheckString(1)
	pattern := L.CheckString(2)

	re, err := regexp.Compile(pattern)
	if err != nil {
		L.Push(lua.LBool(false))
		L.Push(lua.LString("invalid pattern: " + err.Error()))
		return 2
	}

	matched := re.MatchString(text)
	L.Push(lua.LBool(matched))
	return 1
}

// containsAny checks if a string contains any of the given substrings
func containsAny(L *lua.LState) int {
	str := L.CheckString(1)
	
	// Check if second argument is a table
	if L.GetTop() >= 2 && L.Get(2).Type() == lua.LTTable {
		table := L.ToTable(2)
		var items []string
		
		table.ForEach(func(k, v lua.LValue) {
			if v.Type() == lua.LTString {
				items = append(items, v.String())
			}
		})
		
		for _, item := range items {
			if strings.Contains(str, item) {
				L.Push(lua.LBool(true))
				L.Push(lua.LString(item))
				return 2
			}
		}
		
		L.Push(lua.LBool(false))
		return 1
	}
	
	// If not a table, treat remaining arguments as strings
	for i := 2; i <= L.GetTop(); i++ {
		substr := L.CheckString(i)
		if strings.Contains(str, substr) {
			L.Push(lua.LBool(true))
			L.Push(lua.LString(substr))
			return 2
		}
	}
	
	L.Push(lua.LBool(false))
	return 1
}

// toLowerCase converts a string to lowercase
func toLowerCase(L *lua.LState) int {
	str := L.CheckString(1)
	L.Push(lua.LString(strings.ToLower(str)))
	return 1
}

// toUpperCase converts a string to uppercase
func toUpperCase(L *lua.LState) int {
	str := L.CheckString(1)
	L.Push(lua.LString(strings.ToUpper(str)))
	return 1
}

// trim removes whitespace from both ends of a string
func trim(L *lua.LState) int {
	str := L.CheckString(1)
	L.Push(lua.LString(strings.TrimSpace(str)))
	return 1
}

// split splits a string by a separator
func split(L *lua.LState) int {
	str := L.CheckString(1)
	sep := L.CheckString(2)
	
	parts := strings.Split(str, sep)
	
	resultTable := L.NewTable()
	for i, part := range parts {
		resultTable.RawSetInt(i+1, lua.LString(part))
	}
	
	L.Push(resultTable)
	return 1
}