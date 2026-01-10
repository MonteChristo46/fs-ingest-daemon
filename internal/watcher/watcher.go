package watcher

import (
	"log"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// Watcher handles the file system events
type Watcher struct {
	fsWatcher *fsnotify.Watcher
}

// NewWatcher creates and initializes a recursive watcher
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
				if event.Has(fsnotify.Create) {
					info, err := os.Stat(event.Name)
					if err == nil && info.IsDir() {
						w.AddRecursive(event.Name)
					} else if err == nil {
						// It's a file! Trigger the callback (Upload logic)
						eventCallback(event.Name)
					}
				}
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

// AddRecursive adds a path and all sub-directories to the watcher
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

func (w *Watcher) Close() {
	w.fsWatcher.Close()
}
