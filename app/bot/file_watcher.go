package bot

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

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

	done := make(chan bool)
	go func() {
		defer close(done)
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
					data, e := readFile(path)
					if e != nil {
						log.Printf("[WARN] failed to read updated file %s: %v", path, e)
						continue
					}
					if e = onDataChange(data); e != nil {
						log.Printf("[WARN] failed to load updated file %s: %v", path, e)
						continue
					}
				}
			case e, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("[WARN] watcher error: %v", e)
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

// watchPair starts watching two files for changes and calls onDataChange callback
func watchPair(ctx context.Context, path1, path2 string, onDataChange func(io.Reader, io.Reader) error) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		err := watch(ctx, path1, func(r io.Reader) error {
			r2, err := readFile(path2)
			if err != nil {
				return err
			}
			return onDataChange(r, r2)
		})
		if err != nil {
			log.Printf("[WARN] failed to watch file %s: %v", path1, err)
		}
	}()

	go func() {
		defer wg.Done()
		err := watch(ctx, path2, func(r io.Reader) error {
			r1, err := readFile(path1)
			if err != nil {
				return err
			}
			return onDataChange(r1, r)
		})
		if err != nil {
			log.Printf("[WARN] failed to watch file %s: %v", path2, err)
		}
	}()

	wg.Wait()
}

func readFile(path string) (io.Reader, error) {
	file, err := os.Open(path) //nolint gosec // path is controlled by the app
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
