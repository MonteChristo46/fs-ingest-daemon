package watcher

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatcherDebounce(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "watcher_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	var callbackCount int32
	callbackCh := make(chan string, 10)

	onFile := func(path string) {
		atomic.AddInt32(&callbackCount, 1)
		callbackCh <- path
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	// Use a debounce large enough to cover the sleep intervals below
	debounce := 200 * time.Millisecond

	w, err := NewWatcher(tmpDir, debounce, onFile, logger)
	if err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}
	defer w.Close()

	// Wait a bit for watcher to start watching the root
	time.Sleep(100 * time.Millisecond)

	testFile := filepath.Join(tmpDir, "test.txt")

	// Simulating a slow write: Create + Write + Write
	// Event 1: Create
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("part1")
	f.Sync() // Ensure flush to disk to trigger events

	// Wait less than debounce
	time.Sleep(50 * time.Millisecond)

	// Event 2: Write
	f.WriteString("part2")
	f.Sync()

	// Wait less than debounce
	time.Sleep(50 * time.Millisecond)

	// Event 3: Write
	f.WriteString("part3")
	f.Sync()
	f.Close()

	// At this point:
	// T=0: Create (Timer set for T=200)
	// T=50: Write (Timer reset for T=250)
	// T=100: Write (Timer reset for T=300)
	// Callback should fire at T=300.

	// Now wait for callback.
	// Max wait should be enough to cover T=300 + some buffer.
	select {
	case path := <-callbackCh:
		if path != testFile {
			t.Errorf("Expected path %s, got %s", testFile, path)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for callback")
	}

	// Wait a little more to ensure no extra callbacks come in (if debounce failed)
	time.Sleep(300 * time.Millisecond)

	// Verify count is 1
	count := atomic.LoadInt32(&callbackCount)
	if count != 1 {
		t.Errorf("Expected callback count 1, got %d. Debounce might not be working.", count)
	}
}
