// Package plugin provides a plugin system for spam detection in tg-spam.
// It loads and executes Lua scripts that implement custom spam checking logic.
// Scripts should provide a "check" function that takes a message context and returns
// a boolean (is spam), a string (details), and an optional boolean approval. An exact
// true approves only the current message when the plugin reports ham. It can clear soft
// results in Detector.Check, but not the short-message-flood or prohibited-language
// blocks that return before plugins run. A normal-length cleared message follows the
// ordinary ham path for user graduation and ham history.
package plugin

import (
	"fmt"
	"log"
	"path/filepath"
	"sync"

	lua "github.com/yuin/gopher-lua"

	"github.com/umputun/tg-spam/lib/spamcheck"
)

// Checker implements a Lua plugin engine for spam detection
type Checker struct {
	vm       *lua.LState
	checkers map[string]*lua.LFunction
	warned   map[string]struct{}
	lock     sync.RWMutex // protects the checkers map and serializes access to the shared vm
	watcher  *Watcher     // optional file watcher for dynamic reloading
}

// Check is a function that takes a request and returns a response indicating if message is spam
type Check func(req spamcheck.Request) spamcheck.Response

// Result contains a Lua plugin response and its optional approval of the current message.
type Result struct {
	Response spamcheck.Response
	Approved bool
}

// ResultCheck is a function that returns the full result of a Lua plugin check.
type ResultCheck func(req spamcheck.Request) Result

// NewChecker creates a new Checker
func NewChecker() *Checker {
	L := lua.NewState()
	lc := &Checker{
		vm:       L,
		checkers: make(map[string]*lua.LFunction),
		warned:   make(map[string]struct{}),
	}
	lc.RegisterHelpers() // register helper functions
	return lc
}

// LoadScript loads a Lua script and registers it as a checker
func (c *Checker) LoadScript(path string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	// create a new state for loading this script to avoid interference with other scripts
	tempState := lua.NewState()
	defer tempState.Close()

	// load the script in the temporary state
	if err := tempState.DoFile(path); err != nil {
		return fmt.Errorf("failed to load Lua script: %w", err)
	}

	// extract the checker function from the temporary state
	checkFunc := tempState.GetGlobal("check")
	if checkFunc.Type() != lua.LTFunction {
		return fmt.Errorf("script must define a 'check' function")
	}

	// use filename (without extension) as checker name
	name := filepath.Base(path)
	name = name[:len(name)-len(filepath.Ext(name))]

	// now load the script in the real VM
	if err := c.vm.DoFile(path); err != nil {
		return fmt.Errorf("failed to load Lua script in main VM: %w", err)
	}

	// extract the checker function from the real VM
	realCheckFunc := c.vm.GetGlobal("check")
	if realCheckFunc.Type() != lua.LTFunction {
		return fmt.Errorf("script in main VM must define a 'check' function")
	}

	// store the function from the main VM after both loads have succeeded
	c.checkers[name] = realCheckFunc.(*lua.LFunction)

	return nil
}

// ReloadScript reloads a specific Lua script. A script that fails to load keeps its registry entry,
// so a broken edit does not deregister a working rule. Reloading is not transactional beyond that:
// the candidate runs in the shared VM to be registered, so one that mutates globals before failing
// can still change what the previous version does.
func (c *Checker) ReloadScript(path string) error {
	return c.LoadScript(path)
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

// GetCheck returns a Check for the specified Lua checker
func (c *Checker) GetCheck(name string) (Check, error) {
	resultCheck, err := c.GetResultCheck(name)
	if err != nil {
		return nil, err
	}
	return func(req spamcheck.Request) spamcheck.Response {
		return resultCheck(req).Response
	}, nil
}

// GetResultCheck returns a ResultCheck for the specified Lua checker.
func (c *Checker) GetResultCheck(name string) (ResultCheck, error) {
	c.lock.RLock()
	_, ok := c.checkers[name]
	c.lock.RUnlock()

	if !ok {
		return nil, fmt.Errorf("lua checker %q not found", name)
	}

	return c.createResultCheck(name), nil
}

// GetAllChecks returns all loaded Lua checks
func (c *Checker) GetAllChecks() map[string]Check {
	result := make(map[string]Check)

	c.lock.RLock()
	for name := range c.checkers {
		resultCheck := c.createResultCheck(name)
		result[name] = func(req spamcheck.Request) spamcheck.Response {
			return resultCheck(req).Response
		}
	}
	c.lock.RUnlock()

	return result
}

// GetAllResultChecks returns all loaded Lua result checks.
func (c *Checker) GetAllResultChecks() map[string]ResultCheck {
	result := make(map[string]ResultCheck)

	c.lock.RLock()
	for name := range c.checkers {
		result[name] = c.createResultCheck(name)
	}
	c.lock.RUnlock()

	return result
}

// createResultCheck creates a ResultCheck function for the named Lua checker
func (c *Checker) createResultCheck(name string) ResultCheck {
	return func(req spamcheck.Request) Result {
		// the write lock is required, not just a read lock: everything below mutates the shared
		// *lua.LState (allocating tables, pushing and popping the stack) and gopher-lua states are
		// not goroutine-safe. Detector.Check holds only a read lock of its own, so concurrent
		// checks reach this closure in parallel.
		c.lock.Lock()
		defer c.lock.Unlock()

		// the function is resolved on every call rather than captured: callers such as the detector
		// keep a Check for their lifetime, and capturing would pin them to the version loaded first
		checker, ok := c.checkers[name]
		if !ok {
			err := fmt.Errorf("lua checker %q not found", name)
			return Result{Response: spamcheck.Response{Name: "lua-" + name, Spam: false, Details: err.Error(), Error: err}}
		}

		// create Lua table from request
		reqTable := c.vm.NewTable()
		reqTable.RawSetString("msg", lua.LString(req.Msg))
		reqTable.RawSetString("user_id", lua.LString(req.UserID))
		reqTable.RawSetString("user_name", lua.LString(req.UserName))
		reqTable.RawSetString("first_name", lua.LString(req.FirstName))
		reqTable.RawSetString("last_name", lua.LString(req.LastName))
		reqTable.RawSetString("is_premium", lua.LBool(req.IsPremium))

		// add metadata
		metaTable := c.vm.NewTable()
		metaTable.RawSetString("images", lua.LNumber(req.Meta.Images))
		metaTable.RawSetString("links", lua.LNumber(req.Meta.Links))
		metaTable.RawSetString("mentions", lua.LNumber(req.Meta.Mentions))
		metaTable.RawSetString("has_video", lua.LBool(req.Meta.HasVideo))
		metaTable.RawSetString("has_audio", lua.LBool(req.Meta.HasAudio))
		metaTable.RawSetString("has_forward", lua.LBool(req.Meta.HasForward))
		metaTable.RawSetString("has_keyboard", lua.LBool(req.Meta.HasKeyboard))
		metaTable.RawSetString("has_giveaway", lua.LBool(req.Meta.HasGiveaway))
		metaTable.RawSetString("has_contact", lua.LBool(req.Meta.HasContact))
		metaTable.RawSetString("has_external_reply", lua.LBool(req.Meta.HasExternalReply))
		metaTable.RawSetString("message_id", lua.LNumber(req.Meta.MessageID))
		reqTable.RawSetString("meta", metaTable)

		// call the Lua function
		if err := c.vm.CallByParam(lua.P{
			Fn:      checker,
			NRet:    3,
			Protect: true,
		}, reqTable); err != nil {
			return Result{Response: spamcheck.Response{
				Name:    "lua-" + name,
				Spam:    false,
				Details: "error executing lua checker: " + err.Error(),
				Error:   err,
			}}
		}

		// get return values from stack
		isSpam := c.vm.ToBool(-3)
		details := c.vm.ToString(-2)
		approvalValue := c.vm.Get(-1)
		c.vm.Pop(3) // pop results from stack

		approved := false
		switch value := approvalValue.(type) {
		case *lua.LNilType:
		case lua.LBool:
			if bool(value) {
				if isSpam {
					c.warnOnce(name, "spam-conflict", "[WARN] lua checker %q returned approval=true with spam=true", name)
				} else {
					approved = true
				}
			}
		default:
			valueType := approvalValue.Type().String()
			c.warnOnce(name, "type:"+valueType, "[WARN] lua checker %q returned approval value with type %s", name, valueType)
		}

		return Result{Response: spamcheck.Response{
			Name:    "lua-" + name,
			Spam:    isSpam,
			Details: details,
		}, Approved: approved}
	}
}

func (c *Checker) warnOnce(name, kind, format string, args ...any) {
	key := name + "\x00" + kind
	if _, found := c.warned[key]; found {
		return
	}
	c.warned[key] = struct{}{}
	log.Printf(format, args...)
}

// Close cleans up resources used by the Checker
func (c *Checker) Close() {
	// stop the watcher if it exists
	if c.watcher != nil {
		c.watcher.Stop()
	}
	c.vm.Close()
}

// SetWatcher sets the file watcher for the Checker
func (c *Checker) SetWatcher(watcher *Watcher) {
	c.watcher = watcher
}
