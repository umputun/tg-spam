package bot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/fsnotify/fsnotify"
)

// watch starts watching file for changes and calls onDataChange callback
// this is a helper for dynamic reloading of files used by SpamFilter
func watch(ctx context.Context, path string, onDataChange func(io.Reader) error) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}
	defer watcher.Close()

	readFile := func(path string) (io.Reader, error) {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open file %s: %w", path, err)
		}
		defer file.Close()
		data, err := io.ReadAll(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read file %s: %w", path, err)
		}
		return bytes.NewReader(data), nil
	}

	done := make(chan bool)
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Printf("[INFO] stopping watcher for %s, %v", path, ctx.Err())
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					data, err := readFile(path)
					if err != nil {
						log.Printf("[WARN] failed to read updated file %s: %v", path, err)
						continue
					}
					if err := onDataChange(data); err != nil {
						log.Printf("[WARN] failed to load updated file %s: %v", path, err)
						continue
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("[WARN] watcher error: %v", err)
			}
		}
	}()

	err = watcher.Add(path)
	if err != nil {
		return fmt.Errorf("failed to add %s to watcher: %w", path, err)
	}
	<-done
	return nil
}
