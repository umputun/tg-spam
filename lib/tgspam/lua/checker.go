// Package lua provides a Lua plugin system for spam detection in tg-spam.
// It loads and executes Lua scripts that implement custom spam checking logic.
// Scripts should provide a "check" function that takes a message context and returns
// a boolean (is spam) and a string (details).
package lua

import (
	"fmt"
	"path/filepath"

	lua "github.com/yuin/gopher-lua"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

// PluginCheck is a function that takes a request and returns a response indicating if message is spam
type PluginCheck func(req spamcheck.Request) spamcheck.Response

// Checker implements a Lua plugin engine for spam detection
type Checker struct {
	vm       *lua.LState
	checkers map[string]*lua.LFunction
}

// NewChecker creates a new Checker
func NewChecker() *Checker {
	L := lua.NewState()
	lc := &Checker{
		vm:       L,
		checkers: make(map[string]*lua.LFunction),
	}
	lc.RegisterHelpers() // register helper functions
	return lc
}

// LoadScript loads a Lua script and registers it as a checker
func (c *Checker) LoadScript(path string) error {
	if err := c.vm.DoFile(path); err != nil {
		return fmt.Errorf("failed to load Lua script: %w", err)
	}

	// extract checker function
	checkFunc := c.vm.GetGlobal("check")
	if checkFunc.Type() != lua.LTFunction {
		return fmt.Errorf("script must define a 'check' function")
	}

	// use filename (without extension) as checker name
	name := filepath.Base(path)
	name = name[:len(name)-len(filepath.Ext(name))]
	c.checkers[name] = checkFunc.(*lua.LFunction)

	return nil
}

// LoadDirectory loads all Lua scripts from a directory
func (c *Checker) LoadDirectory(dir string) error {
	files, err := filepath.Glob(filepath.Join(dir, "*.lua"))
	if err != nil {
		return fmt.Errorf("failed to list Lua scripts in %s: %w", dir, err)
	}

	for _, file := range files {
		if err := c.LoadScript(file); err != nil {
			return fmt.Errorf("failed to load script %s: %w", file, err)
		}
	}

	return nil
}

// GetCheck returns a MetaCheck for the specified Lua checker
func (c *Checker) GetCheck(name string) (PluginCheck, error) {
	checker, ok := c.checkers[name]
	if !ok {
		return nil, fmt.Errorf("lua checker %q not found", name)
	}

	return c.createMetaChecker(name, checker), nil
}

// GetAllChecks returns all loaded Lua checks
func (c *Checker) GetAllChecks() map[string]PluginCheck {
	result := make(map[string]PluginCheck)
	for name, checker := range c.checkers {
		result[name] = c.createMetaChecker(name, checker)
	}
	return result
}

// createMetaChecker creates a PluginCheck function from a Lua checker
func (c *Checker) createMetaChecker(name string, checker *lua.LFunction) PluginCheck {
	return func(req spamcheck.Request) spamcheck.Response {
		// create Lua table from request
		reqTable := c.vm.NewTable()
		reqTable.RawSetString("msg", lua.LString(req.Msg))
		reqTable.RawSetString("user_id", lua.LString(req.UserID))
		reqTable.RawSetString("user_name", lua.LString(req.UserName))

		// add metadata
		metaTable := c.vm.NewTable()
		metaTable.RawSetString("images", lua.LNumber(req.Meta.Images))
		metaTable.RawSetString("links", lua.LNumber(req.Meta.Links))
		metaTable.RawSetString("mentions", lua.LNumber(req.Meta.Mentions))
		metaTable.RawSetString("has_video", lua.LBool(req.Meta.HasVideo))
		metaTable.RawSetString("has_audio", lua.LBool(req.Meta.HasAudio))
		metaTable.RawSetString("has_forward", lua.LBool(req.Meta.HasForward))
		metaTable.RawSetString("has_keyboard", lua.LBool(req.Meta.HasKeyboard))
		reqTable.RawSetString("meta", metaTable)

		// call the Lua function
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

		// get return values from stack
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

// Close cleans up resources used by the Checker
func (c *Checker) Close() {
	c.vm.Close()
}
