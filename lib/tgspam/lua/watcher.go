package lua

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher monitors a directory for changes to Lua scripts and reloads them as needed
type Watcher struct {
	checker      *Checker
	pluginsDir   string
	watcher      *fsnotify.Watcher
	done         chan struct{}
	debounceTime time.Duration
	events       map[string]time.Time
	mu           sync.Mutex
	started      bool
}

// NewWatcher creates a new file system watcher for Lua plugins
func NewWatcher(checker *Checker, pluginsDir string) (*Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	return &Watcher{
		checker:      checker,
		pluginsDir:   pluginsDir,
		watcher:      watcher,
		done:         make(chan struct{}),
		debounceTime: 500 * time.Millisecond, // default debounce time
		events:       make(map[string]time.Time),
	}, nil
}

// Start begins watching the plugins directory for changes
func (w *Watcher) Start() error {
	// avoid starting multiple watchers
	w.mu.Lock()
	if w.started {
		w.mu.Unlock()
		return nil
	}
	w.started = true
	w.mu.Unlock()

	// ensure the directory exists
	if _, err := os.Stat(w.pluginsDir); os.IsNotExist(err) {
		return fmt.Errorf("plugins directory %s does not exist: %w", w.pluginsDir, err)
	}

	// add the directory to the watch list
	if err := w.watcher.Add(w.pluginsDir); err != nil {
		return fmt.Errorf("failed to watch plugins directory: %w", err)
	}

	log.Printf("[INFO] started watching lua plugins directory: %s", w.pluginsDir)

	// start the watcher goroutine
	go w.watchLoop()

	return nil
}

// Stop terminates the file watcher
func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.started {
		return
	}

	close(w.done)
	if err := w.watcher.Close(); err != nil {
		log.Printf("[ERROR] failed to close file watcher: %v", err)
	}
	w.started = false
	log.Printf("[INFO] stopped watching Lua plugins directory: %s", w.pluginsDir)
}

// watchLoop is the main loop that handles file system events
func (w *Watcher) watchLoop() {
	ticker := time.NewTicker(w.debounceTime)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			w.handleEvent(event)
		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("[WARN] watcher error: %v", err)
		case <-ticker.C:
			w.processEvents()
		}
	}
}

// handleEvent processes a single fsnotify event
func (w *Watcher) handleEvent(event fsnotify.Event) {
	// only process .lua files
	if filepath.Ext(event.Name) != ".lua" {
		return
	}

	// track create, write, and remove operations
	if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Remove) {
		w.mu.Lock()
		w.events[event.Name] = time.Now()
		w.mu.Unlock()
	}
}

// processEvents handles the debounced events
func (w *Watcher) processEvents() {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	for filename, timestamp := range w.events {
		// skip events that are too recent (debouncing)
		if now.Sub(timestamp) < w.debounceTime {
			continue
		}

		// handle the event based on whether the file exists
		if _, err := os.Stat(filename); os.IsNotExist(err) {
			// file was deleted
			scriptName := filepath.Base(filename)
			scriptName = scriptName[:len(scriptName)-len(filepath.Ext(scriptName))]
			log.Printf("[INFO] lua script removed: %s", scriptName)
			continue
		}
		// file was created or modified
		log.Printf("[INFO] reloading lua script: %s", filename)
		if err := w.checker.ReloadScript(filename); err != nil {
			log.Printf("[WARN] failed to reload lua script %s: %v", filename, err)
		}

		// remove processed event
		delete(w.events, filename)
	}
}
