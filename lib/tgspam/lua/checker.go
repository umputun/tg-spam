package lua

import (
	"fmt"
	"path/filepath"

	lua "github.com/yuin/gopher-lua"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

// LuaChecker represents a checker that uses Lua scripts to determine if a message is spam
type LuaChecker struct {
	vm       *lua.LState
	checkers map[string]*lua.LFunction
}

// NewLuaChecker creates a new LuaChecker
func NewLuaChecker() *LuaChecker {
	L := lua.NewState()
	return &LuaChecker{
		vm:       L,
		checkers: make(map[string]*lua.LFunction),
	}
}

// LoadScript loads a Lua script and registers it as a checker
func (c *LuaChecker) LoadScript(path string) error {
	if err := c.vm.DoFile(path); err != nil {
		return fmt.Errorf("failed to load Lua script: %w", err)
	}

	// Extract checker function
	checkFunc := c.vm.GetGlobal("check")
	if checkFunc.Type() != lua.LTFunction {
		return fmt.Errorf("script must define a 'check' function")
	}

	// Use filename (without extension) as checker name
	name := filepath.Base(path)
	name = name[:len(name)-len(filepath.Ext(name))]
	c.checkers[name] = checkFunc.(*lua.LFunction)

	return nil
}

// LoadDirectory loads all Lua scripts from a directory
func (c *LuaChecker) LoadDirectory(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.lua"))
	if err != nil {
		return fmt.Errorf("failed to list Lua scripts: %w", err)
	}

	for _, file := range files {
		if err := c.LoadScript(file); err != nil {
			return fmt.Errorf("failed to load script %s: %w", file, err)
		}
	}

	return nil
}

// GetCheck returns a MetaCheck for the specified Lua checker
func (c *LuaChecker) GetCheck(name string) (spamcheck.MetaCheck, error) {
	checker, ok := c.checkers[name]
	if !ok {
		return nil, fmt.Errorf("lua checker %q not found", name)
	}

	return c.createMetaChecker(name, checker), nil
}

// GetAllChecks returns all loaded Lua checks as a map of name to MetaCheck
func (c *LuaChecker) GetAllChecks() map[string]spamcheck.MetaCheck {
	result := make(map[string]spamcheck.MetaCheck)
	for name, checker := range c.checkers {
		result[name] = c.createMetaChecker(name, checker)
	}
	return result
}

// createMetaChecker creates a MetaCheck function from a Lua checker
func (c *LuaChecker) createMetaChecker(name string, checker *lua.LFunction) spamcheck.MetaCheck {
	return func(req spamcheck.Request) spamcheck.Response {
		// Create Lua table from request
		reqTable := c.vm.NewTable()
		reqTable.RawSetString("msg", lua.LString(req.Msg))
		reqTable.RawSetString("user_id", lua.LString(req.UserID))
		reqTable.RawSetString("user_name", lua.LString(req.UserName))
		
		// Add metadata
		metaTable := c.vm.NewTable()
		metaTable.RawSetString("images", lua.LNumber(req.Meta.Images))
		metaTable.RawSetString("links", lua.LNumber(req.Meta.Links))
		metaTable.RawSetString("mentions", lua.LNumber(req.Meta.Mentions))
		metaTable.RawSetString("has_video", lua.LBool(req.Meta.HasVideo))
		metaTable.RawSetString("has_audio", lua.LBool(req.Meta.HasAudio))
		metaTable.RawSetString("has_forward", lua.LBool(req.Meta.HasForward))
		metaTable.RawSetString("has_keyboard", lua.LBool(req.Meta.HasKeyboard))
		reqTable.RawSetString("meta", metaTable)

		// Call the Lua function
		if err := c.vm.CallByParam(lua.P{
			Fn:      checker,
			NRet:    2,
			Protect: true,
		}, reqTable); err != nil {
			return spamcheck.Response{
				Name:    "lua-" + name,
				Spam:    false,
				Details: "error executing lua checker: " + err.Error(),
				Error:   err,
			}
		}

		// Get return values from stack
		isSpam := c.vm.ToBool(-2)
		details := c.vm.ToString(-1)
		c.vm.Pop(2) // pop results from stack

		return spamcheck.Response{
			Name:    "lua-" + name,
			Spam:    isSpam,
			Details: details,
		}
	}
}

// Close closes the Lua VM
func (c *LuaChecker) Close() {
	c.vm.Close()
}