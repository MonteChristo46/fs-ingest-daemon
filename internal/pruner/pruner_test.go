package pruner

import (
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/store"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPruner_Eviction(t *testing.T) {
	// Setup Temp Dir
	tmpDir, err := os.MkdirTemp("", "pruner_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Setup DB
	dbPath := filepath.Join(tmpDir, "test.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Configuration: Set limit extremely low to force eviction
	// 0.0000001 GB is roughly 107 bytes. We will create 1KB files.
	cfg := &config.Config{
		MaxDataSizeGB:  0.0000001,
		PruneBatchSize: 10,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	p := NewPruner(cfg, s, logger)

	// --- Scenario Setup ---

	// 1. Old Uploaded File (Target for eviction)
	// Created 2 hours ago, Uploaded.
	oldFile := filepath.Join(tmpDir, "old_uploaded.dat")
	createFile(t, oldFile, 1024)
	// Manually inject into DB to set specific mod time
	s.RegisterFile(oldFile, 1024, time.Now().Add(-2*time.Hour), false, true)
	s.MarkUploaded(oldFile)

	// 2. New Uploaded File (Target for eviction ONLY if space still needed)
	// Created 1 hour ago, Uploaded.
	newFile := filepath.Join(tmpDir, "new_uploaded.dat")
	createFile(t, newFile, 1024)
	s.RegisterFile(newFile, 1024, time.Now().Add(-1*time.Hour), false, true)
	s.MarkUploaded(newFile)

	// 3. Pending File (Protected)
	// Created 3 hours ago (older than others!), but NOT Uploaded.
	// This proves that Status > ModTime for safety.
	pendingFile := filepath.Join(tmpDir, "pending.dat")
	createFile(t, pendingFile, 1024)
	s.RegisterFile(pendingFile, 1024, time.Now().Add(-3*time.Hour), false, true)
	// Status remains PENDING/AWAITING

	// --- Execution ---
	p.Prune()

	// --- Verification ---

	// 1. Old Uploaded should be gone
	if exists(oldFile) {
		t.Error("Old uploaded file was NOT deleted")
	} else {
		// Verify DB record is also gone
		// We can't query DB easily without adding methods to store,
		// but we can check if file is re-registered or rely on Pruner logs/logic.
		// Let's trust fs check for now.
	}

	// 2. New Uploaded should be gone (because limit is ~100 bytes and we have 1KB pending)
	// Actually, wait. The Pruner checks Total Size.
	// Total Size = 3KB. Limit = 100 bytes.
	// It deletes 'oldFile' (1KB). Remaining = 2KB. Still > Limit.
	// It should delete 'newFile' (1KB). Remaining = 1KB (Pending).
	// It CANNOT delete Pending.
	if exists(newFile) {
		t.Error("New uploaded file was NOT deleted (should be deleted to free space)")
	}

	// 3. Pending File MUST exist
	if !exists(pendingFile) {
		t.Error("CRITICAL: Pending file WAS deleted! Data loss occurred.")
	}
}

func TestPruner_Eviction_Hysteresis(t *testing.T) {
	// Setup Temp Dir
	tmpDir, err := os.MkdirTemp("", "pruner_hysteresis_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Setup DB
	dbPath := filepath.Join(tmpDir, "test.db")
	s, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Limit = 100 bytes.
	// High (80%) = 80 bytes.
	// Low (40%) = 40 bytes.
	cfg := &config.Config{
		MaxDataSizeGB:             float64(100) / (1024 * 1024 * 1024), // Exactly 100 bytes
		PruneBatchSize:            1,                                   // Force loop one by one
		PruneHighWatermarkPercent: 80,
		PruneLowWatermarkPercent:  40,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	p := NewPruner(cfg, s, logger)

	// Create 6 files of 20 bytes each = 120 bytes total.
	// 120 bytes > 80 bytes (High Watermark). Trigger!
	files := []string{"f1", "f2", "f3", "f4", "f5", "f6"}
	for i, name := range files {
		path := filepath.Join(tmpDir, name)
		createFile(t, path, 20)
		// Register with increasing mod times (f1=oldest)
		s.RegisterFile(path, 20, time.Now().Add(time.Duration(-len(files)+i)*time.Minute), false, true)
		s.MarkUploaded(path)
	}

	// Pre-check
	size, _ := s.GetTotalSize()
	if size != 120 {
		t.Fatalf("Expected 120 bytes, got %d", size)
	}

	// --- Execution ---
	p.Prune()

	// --- Verification ---
	// Expected:
	// Start: 120
	// 120 > 40 (Low)? Yes. Delete f1 (Oldest). Size -> 100.
	// 100 > 40? Yes. Delete f2. Size -> 80.
	// 80 > 40? Yes. Delete f3. Size -> 60.
	// 60 > 40? Yes. Delete f4. Size -> 40.
	// 40 > 40? No. Stop.
	//
	// Remaining: f5, f6.
	// Deleted: f1, f2, f3, f4.

	finalSize, _ := s.GetTotalSize()
	if finalSize != 40 {
		t.Errorf("Expected final size to be 40 bytes (Low Watermark), got %d", finalSize)
	}

	if exists(filepath.Join(tmpDir, "f4")) {
		t.Error("f4 should have been deleted")
	}
	if !exists(filepath.Join(tmpDir, "f5")) {
		t.Error("f5 should NOT have been deleted")
	}
}

func createFile(t *testing.T, path string, size int64) {
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}
