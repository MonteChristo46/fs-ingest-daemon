package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPairingDoubleExtension(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "pairing_double_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer s.Close()

	imagePath := "/data/img.png"
	jsonPath := "/data/img.png.json"
	modTime := time.Now()
	size := int64(100)

	// 1. Image Arrives First
	if err := s.RegisterFile(imagePath, size, modTime, false, true); err != nil {
		t.Fatalf("Failed to register image: %v", err)
	}
	// Check Image is waiting
	verifyStatus(t, s, imagePath, StatusAwaitingPartner)

	// 2. JSON Arrives
	if err := s.RegisterFile(jsonPath, size, modTime, true, true); err != nil {
		t.Fatalf("Failed to register json: %v", err)
	}

	// 3. Verify Both Paired
	verifyPaired(t, s, imagePath, jsonPath)
	verifyPaired(t, s, jsonPath, imagePath)
}

func TestPairingSingleExtension(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "pairing_single_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer s.Close()

	imagePath := "/data/img_single.png"
	jsonPath := "/data/img_single.json"
	modTime := time.Now()
	size := int64(100)

	// 1. Image Arrives First
	if err := s.RegisterFile(imagePath, size, modTime, false, true); err != nil {
		t.Fatalf("Failed to register image: %v", err)
	}
	// Check Image is waiting
	// Note: Currently it waits for img_single.png.json by default, BUT should accept img_single.json
	verifyStatus(t, s, imagePath, StatusAwaitingPartner)

	// 2. JSON Arrives
	if err := s.RegisterFile(jsonPath, size, modTime, true, true); err != nil {
		t.Fatalf("Failed to register json: %v", err)
	}

	// 3. Verify Both Paired
	verifyPaired(t, s, imagePath, jsonPath)
	verifyPaired(t, s, jsonPath, imagePath)
}

func TestPairingSingleExtension_MetaFirst(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "pairing_single_meta_first")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "test.db")
	s, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer s.Close()

	imagePath := "/data/img_single_mf.png"
	jsonPath := "/data/img_single_mf.json"
	modTime := time.Now()
	size := int64(100)

	// 1. JSON Arrives First
	if err := s.RegisterFile(jsonPath, size, modTime, true, true); err != nil {
		t.Fatalf("Failed to register json: %v", err)
	}
	// Check JSON is waiting
	verifyStatus(t, s, jsonPath, StatusAwaitingPartner)

	// 2. Image Arrives
	if err := s.RegisterFile(imagePath, size, modTime, false, true); err != nil {
		t.Fatalf("Failed to register image: %v", err)
	}

	// 3. Verify Both Paired
	verifyPaired(t, s, imagePath, jsonPath)
	verifyPaired(t, s, jsonPath, imagePath)
}

// Helpers

func verifyStatus(t *testing.T, s *Store, path string, expected FileStatus) {
	var status FileStatus
	err := s.db.QueryRow("SELECT status FROM files WHERE path = ?", path).Scan(&status)
	if err != nil {
		t.Fatalf("Failed to get status for %s: %v", path, err)
	}
	if status != expected {
		t.Errorf("File %s status mismatch. Got %s, want %s", path, status, expected)
	}
}

func verifyPaired(t *testing.T, s *Store, path, partner string) {
	var status FileStatus
	var partnerPath string
	err := s.db.QueryRow("SELECT status, partner_path FROM files WHERE path = ?", path).Scan(&status, &partnerPath)
	if err != nil {
		t.Fatalf("Failed to get info for %s: %v", path, err)
	}
	if status != StatusPending {
		t.Errorf("File %s should be PENDING, got %s", path, status)
	}
	if partnerPath != partner {
		t.Errorf("File %s partner mismatch. Got %s, want %s", path, partnerPath, partner)
	}
}
