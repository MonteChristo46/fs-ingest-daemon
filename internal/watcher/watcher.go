package watcher

// Package watcher provides a recursive file system watcher.
// It uses fsnotify to listen for file creation events and triggers a callback
// when a new file is detected. It automatically adds subdirectories to the watch list.

import (
	"log"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// Watcher handles the file system events using fsnotify.
type Watcher struct {
	fsWatcher *fsnotify.Watcher
}

// NewWatcher creates and initializes a recursive watcher on the specified root directory.
//
// Arguments:
//
//	root: The directory path to start watching.
//	eventCallback: A function to call when a new file is detected.
func NewWatcher(root string, eventCallback func(string)) (*Watcher, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{fsWatcher: fs}

	// Go routine to process events
	go func() {
		for {
			select {
			case event, ok := <-fs.Events:
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
					} else if err == nil {
						// It's a file! Trigger the callback (Upload logic)
						eventCallback(event.Name)
					}
				}
				// Note: In a real-world scenario, you might also want to handle Write events
				// if files are written slowly, but for atomic moves/copies Create is often sufficient.

			case err, ok := <-fs.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}
	}()

	err = w.AddRecursive(root)
	return w, err
}

// AddRecursive adds the given path and all its sub-directories to the watcher.
func (w *Watcher) AddRecursive(path string) error {
	return filepath.Walk(path, func(newPath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			log.Printf("Watching: %s\n", newPath)
			return w.fsWatcher.Add(newPath)
		}
		return nil
	})
}

// Close shuts down the file system watcher.
func (w *Watcher) Close() {
	w.fsWatcher.Close()
}
