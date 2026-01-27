package daemon

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/store"
)

func TestDaemonInitialScan(t *testing.T) {
	// 1. Setup Temp Directories and Config
	tmpDir, err := os.MkdirTemp("", "daemon_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	watchDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(tmpDir, "fsd.db")

	cfg := &config.Config{
		DeviceID:            "test-dev",
		Endpoint:            "http://localhost:8080",
		WatchPath:           watchDir,
		DBPath:              dbPath,
		MaxDataSizeGB:       1.0,
		IngestCheckInterval: "100ms", // Fast interval for testing
		AllowedExtensions:   []string{".jpg", ".jpeg", ".png", ".json"},
	}

	// 2. Create Pre-existing files (Simulate offline creation)
	files := []string{"file1.png", "file2.jpg"}
	for _, name := range files {
		p := filepath.Join(watchDir, name)
		if err := os.WriteFile(p, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
		// Create partner JSON
		jsonPath := p + ".json"
		if err := os.WriteFile(jsonPath, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// 3. Initialize Daemon (partially)
	// We manually construct it to avoid full service start overhead if possible,
	// but calling Start is the best way to test the flow.
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	d := &Daemon{
		Logger: logger,
		Cfg:    cfg,
	}

	// Mocking service interface isn't strictly necessary if we just call Start and ignore the service arg for now,
	// but the Start method expects a service.Service.
	// However, looking at the code, it doesn't actually use the `s` argument.
	// So we can pass nil.

	// We need to stop it eventually.
	defer d.Stop(nil)

	// 4. Start Daemon
	if err := d.Start(nil); err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}

	// 5. Wait for Initial Scan to complete
	// Since it's a goroutine, we need to poll the DB.
	time.Sleep(1 * time.Second) // Give it a moment

	// 6. Verify files are in DB
	// We can inspect d.DbStore directly

	var pending []store.FileRecord
	pending, err = d.DbStore.GetPendingFiles(100)
	if err != nil {
		t.Fatalf("Failed to get pending files: %v", err)
	}

	if len(pending) != 4 {
		t.Errorf("Expected 4 pending files, got %d", len(pending))
	}

	foundCount := 0
	for _, f := range pending {
		base := filepath.Base(f.Path)
		// We expect file1.png, file1.png.json, file2.jpg, file2.jpg.json
		if base == "file1.png" || base == "file2.jpg" || base == "file1.png.json" || base == "file2.jpg.json" {
			foundCount++
		}
	}
	if foundCount != 4 {
		t.Errorf("Expected to find files and partners, but got matches: %d", foundCount)
	}
}

func TestDaemonNoSidecarStrategy(t *testing.T) {
	// 1. Setup Temp Directories and Config
	tmpDir, err := os.MkdirTemp("", "daemon_test_no_sidecar")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	watchDir := filepath.Join(tmpDir, "data")
	if err := os.MkdirAll(watchDir, 0755); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(tmpDir, "fsd.db")

	cfg := &config.Config{
		DeviceID:            "test-dev",
		Endpoint:            "http://localhost:8080",
		WatchPath:           watchDir,
		DBPath:              dbPath,
		MaxDataSizeGB:       1.0,
		IngestCheckInterval: "100ms",
		SidecarStrategy:     "none", // CRITICAL: Disable sidecar requirement
		AllowedExtensions:   []string{".jpg", ".jpeg", ".png", ".json"},
	}

	// 2. Create orphan file
	p := filepath.Join(watchDir, "orphan.png")
	if err := os.WriteFile(p, []byte("image data"), 0644); err != nil {
		t.Fatal(err)
	}

	// 3. Initialize Daemon
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	d := &Daemon{
		Logger: logger,
		Cfg:    cfg,
	}
	defer d.Stop(nil)

	if err := d.Start(nil); err != nil {
		t.Fatalf("Failed to start daemon: %v", err)
	}

	// 4. Wait for Scan
	time.Sleep(1 * time.Second)

	// 5. Verify file is PENDING
	pending, err := d.DbStore.GetPendingFiles(100)
	if err != nil {
		t.Fatalf("Failed to get pending files: %v", err)
	}

	if len(pending) != 1 {
		t.Errorf("Expected 1 pending file, got %d", len(pending))
	} else {
		if filepath.Base(pending[0].Path) != "orphan.png" {
			t.Errorf("Expected orphan.png, got %s", pending[0].Path)
		}
		if pending[0].Status != store.StatusPending {
			t.Errorf("Expected status PENDING, got %s", pending[0].Status)
		}
	}
}
