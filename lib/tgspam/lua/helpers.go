// Package lua provides a Lua plugin system for spam detection.
// This file contains helper functions that are exposed to Lua scripts.
package lua

import (
	"regexp"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

// RegisterHelpers registers common helper functions for Lua scripts
func (c *Checker) RegisterHelpers() {
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
