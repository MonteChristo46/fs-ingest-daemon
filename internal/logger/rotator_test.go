package logger

import (
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLogRotator_Rotate(t *testing.T) {
	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "fsd-log-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "test.log")

	// Create rotator with small size limit (100 bytes)
	// Note: Our implementation interprets MaxSizeMB as MB.
	// We need to allow it to be smaller for testing or modify the implementation to support bytes.
	// Looking at my implementation: return int64(l.MaxSizeMB) * int64(1024*1024)
	// It uses MB integers. I cannot test small byte sizes easily without modifying the struct
	// or making a huge file.

	// Let's modify the struct temporarily in my mind? No, I should make the implementation testable.
	// Or I can modify the implementation to allow "0" to mean default, but I can't easily inject a small limit.
	// Wait, I can make MaxSizeMB 0 and it defaults to 10MB.
	// I should probably add a way to set limit in bytes for testing, or just write 10MB of data?
	// Writing 10MB in a test is fine but maybe slow/wasteful.

	// BETTER APPROACH: Add a "maxSizeOverride" field to the struct (unexported) for testing,
	// or just check logic.
	// Actually, I'll modify the implementation to verify logic.
	// But I can't modify the code just for tests if I can avoid it.

	// Let's write 1MB and set limit to 1MB?
	// MaxSizeMB is int. Minimum 1.
	// So minimum limit is 1MB.
	// I will write 1.5MB of data.

	rotator := &LogRotator{
		Filename:   logFile,
		MaxSizeMB:  1, // 1MB
		MaxBackups: 2,
		Compress:   false, // Test compression separately
	}
	defer rotator.Close()

	// Write 0.6MB
	data1 := make([]byte, 600*1024)
	if _, err := rotator.Write(data1); err != nil {
		t.Fatalf("Write 1 failed: %v", err)
	}

	// Verify file exists and size
	info, err := os.Stat(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 600*1024 {
		t.Errorf("Expected size 614400, got %d", info.Size())
	}

	// Write another 0.6MB (Total 1.2MB > 1MB) -> Should trigger rotation
	if _, err := rotator.Write(data1); err != nil {
		t.Fatalf("Write 2 failed: %v", err)
	}

	// Give it a moment for async operations if any (postRotate is async)
	time.Sleep(100 * time.Millisecond)

	// Now we should have:
	// test.log (current, size 600KB)
	// test-<timestamp>.log (backup, size 600KB)

	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(files))
		for _, f := range files {
			t.Logf("Found: %s", f.Name())
		}
	}
}

func TestLogRotator_Cleanup(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fsd-log-cleanup")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "cleanup.log")

	rotator := &LogRotator{
		Filename:   logFile,
		MaxSizeMB:  1,
		MaxBackups: 2, // Keep 2
		Compress:   false,
	}
	defer rotator.Close()

	// Create dummy backup files
	// cleanup.log (current)
	// cleanup-2023...1.log
	// cleanup-2023...2.log
	// cleanup-2023...3.log
	// cleanup-2023...4.log

	// We manually trigger cleanup by calling postRotate logic effectively,
	// or we just rely on Write triggering it.
	// But we need rotation to happen.

	// Let's manually create "old" files.
	base := filepath.Base(logFile)
	ext := filepath.Ext(base)
	prefix := base[:len(base)-len(ext)]

	// Create 4 old files
	for i := 0; i < 4; i++ {
		// Use different times
		ts := time.Now().Add(-time.Duration(i+1) * time.Hour).Format("2006-01-02T15-04-05.000")
		name := fmt.Sprintf("%s-%s%s", prefix, ts, ext)
		path := filepath.Join(tmpDir, name)
		os.WriteFile(path, []byte("data"), 0644)
	}

	// Trigger a rotation
	rotator.Write(make([]byte, 10)) // Ensure open
	rotator.rotate()                // Force rotate

	// Wait for async cleanup
	time.Sleep(500 * time.Millisecond)

	// We expect:
	// 1 current file
	// 2 backups (MaxBackups=2)
	// The newly rotated one is the 3rd backup?
	// No, MaxBackups includes the one we just rotated?
	// Implementation: "Delete by count... if len(files) > MaxBackups".
	// files includes all matching backups.
	// So we should have MaxBackups files left.

	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Filter for log files
	count := 0
	for _, f := range files {
		if strings.HasPrefix(f.Name(), prefix) {
			count++
		}
	}

	// 1 current + 2 backups = 3 files total expected?
	// cleanup() removes from the list of *oldLogFiles*.
	// oldLogFiles() does NOT include the *current* active log file (checked by exact name match? No, prefix check).
	// My oldLogFiles implementation:
	//   if name == base { continue } ?? No, I didn't add that check explicitly!
	//   Let's check implementation of oldLogFiles in rotator.go:
	//   It checks prefix "prefix-" . The current file is "test.log", prefix is "test".
	//   "test.log" does NOT start with "test-". So current file is NOT in oldLogFiles.

	// So we expect 2 backups + 1 current = 3 files total.
	if count != 3 {
		t.Errorf("Expected 3 files (1 current + 2 backups), got %d", count)
		for _, f := range files {
			t.Logf("Found: %s", f.Name())
		}
	}
}

func TestLogRotator_Compression(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "fsd-log-compress")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	logFile := filepath.Join(tmpDir, "compress.log")

	rotator := &LogRotator{
		Filename:   logFile,
		MaxSizeMB:  1,
		MaxBackups: 1,
		Compress:   true,
	}
	defer rotator.Close()

	// Write data
	if _, err := rotator.Write([]byte("some data")); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Force rotate
	if err := rotator.rotate(); err != nil {
		t.Fatal(err)
	}

	// Wait for async compression
	time.Sleep(500 * time.Millisecond)

	files, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	foundGz := false
	for _, f := range files {
		t.Logf("Found file: %s (size: %d)", f.Name(), -1) // os.DirEntry doesn't have size directly easily without Info()
		info, _ := f.Info()
		t.Logf("Found file: %s (size: %d)", f.Name(), info.Size())

		if strings.HasSuffix(f.Name(), ".gz") {
			foundGz = true
			// Verify content
			fPath := filepath.Join(tmpDir, f.Name())
			gf, err := os.Open(fPath)
			if err != nil {
				t.Fatal(err)
			}
			gz, err := gzip.NewReader(gf)
			if err != nil {
				t.Fatal(err)
			}
			data, err := io.ReadAll(gz)
			if err != nil {
				t.Fatalf("ReadAll failed: %v", err)
			}
			gz.Close()
			gf.Close()
			if string(data) != "some data" {
				t.Errorf("Compressed content mismatch. Got '%s', want 'some data'", string(data))
			}
		}
	}

	if !foundGz {
		t.Error("Did not find compressed .gz file")
	}
}
