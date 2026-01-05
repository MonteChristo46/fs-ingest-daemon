package main

import (
	"fmt"
	"fs-ingest-daemon/internal/watcher"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// TODO 1: Setup configuration (e.g., read from env or config file)
	watchPath := "./data" // Make sure this folder exists!
	os.MkdirAll(watchPath, 0755)

	fmt.Println("--- FS-INGEST-DAEMON STARTING ---")

	// This function runs every time a new file is detected
	onNewFile := func(path string) {
		fmt.Printf("New Image Detected: %s\n", path)
		// TODO: In next step, we call uploader.Upload(path)
	}

	w, err := watcher.NewWatcher(watchPath, onNewFile)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Close()

	// Keep the app running until you press Ctrl+C
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	fmt.Println("Watcher is active. Press Ctrl+C to stop.")
	<-done
}
