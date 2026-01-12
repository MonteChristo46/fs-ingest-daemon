package watcher

// Package watcher provides a recursive file system watcher.
// It uses fsnotify to listen for file creation and write events, triggering a callback
// only after a debounce period (when no new write events occur for a specified duration).
// It automatically adds subdirectories to the watch list.

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher handles the file system events using fsnotify.
type Watcher struct {
	fsWatcher *fsnotify.Watcher
	logger    *slog.Logger
	debounce  time.Duration
	callback  func(string)

	mu     sync.Mutex
	timers map[string]*time.Timer
}

// NewWatcher creates and initializes a recursive watcher on the specified root directory.
//
// Arguments:
//
//	root: The directory path to start watching.
//	debounce: The duration to wait after the last write event before triggering the callback.
//	eventCallback: A function to call when a file is ready (debounced).
//	logger: Structured logger.
func NewWatcher(root string, debounce time.Duration, eventCallback func(string), logger *slog.Logger) (*Watcher, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		fsWatcher: fs,
		logger:    logger,
		debounce:  debounce,
		callback:  eventCallback,
		timers:    make(map[string]*time.Timer),
	}

	// Go routine to process events
	go w.start()

	err = w.AddRecursive(root)
	if err != nil {
		w.Close()
		return nil, err
	}
	return w, nil
}

func (w *Watcher) start() {
	for {
		select {
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}

			// If a new directory is created, watch it too (Recursive)
			// We check for fsnotify.Create events.
			if event.Has(fsnotify.Create) {
				info, err := os.Stat(event.Name)
				if err == nil && info.IsDir() {
					// Add the new directory to the watcher
					w.AddRecursive(event.Name)
					// Directories don't trigger the file callback
					continue
				}
			}

			// Handle File Events (Create or Write) for Debouncing
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
				w.resetTimer(event.Name)
			} else if event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
				w.cancelTimer(event.Name)
			}

		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			w.logger.Error("Watcher error", "error", err)
		}
	}
}

// resetTimer starts or resets the debounce timer for a given file path.
func (w *Watcher) resetTimer(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Stop existing timer if it exists
	if t, ok := w.timers[path]; ok {
		t.Stop()
	}

	// Create a new timer
	w.timers[path] = time.AfterFunc(w.debounce, func() {
		w.mu.Lock()
		delete(w.timers, path)
		w.mu.Unlock()

		// Trigger the callback
		w.callback(path)
	})
}

// cancelTimer stops and removes the debounce timer for a given file path.
func (w *Watcher) cancelTimer(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if t, ok := w.timers[path]; ok {
		t.Stop()
		delete(w.timers, path)
	}
}

// AddRecursive adds the given path and all its sub-directories to the watcher.
func (w *Watcher) AddRecursive(path string) error {
	return filepath.Walk(path, func(newPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			w.logger.Info("Watching directory", "path", newPath)
			return w.fsWatcher.Add(newPath)
		}
		return nil
	})
}

// Close shuts down the file system watcher and cleans up any pending timers.
func (w *Watcher) Close() {
	w.fsWatcher.Close()

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, t := range w.timers {
		t.Stop()
	}
	w.timers = make(map[string]*time.Timer)
}
