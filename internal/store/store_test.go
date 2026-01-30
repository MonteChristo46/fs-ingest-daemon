package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRemoveFileUnlinksPartner(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "store_test")
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

	// Scenario:
	// 1. Create Image file record (img.png)
	// 2. Create JSON file record (img.png.json) paired with Image
	// 3. Remove Image file
	// 4. Verify JSON file's partner_path is now NULL

	imagePath := "/data/img.png"
	jsonPath := "/data/img.png.json"
	modTime := time.Now()
	size := int64(1024)

	// Register Image (Waiting)
	if err := s.RegisterFile(imagePath, size, modTime, false, true); err != nil {
		t.Fatalf("Failed to register image: %v", err)
	}

	// Register JSON (Pairs them)
	if err := s.RegisterFile(jsonPath, size, modTime, true, true); err != nil {
		t.Fatalf("Failed to register json: %v", err)
	}

	// Verify they are paired
	files, err := s.GetPendingFiles(10)
	if err != nil {
		t.Fatalf("Failed to get pending files: %v", err)
	}
	
	// Should be 2 files
	if len(files) != 2 {
		t.Errorf("Expected 2 pending files, got %d", len(files))
	}

	for _, f := range files {
		if !f.PartnerPath.Valid || f.PartnerPath.String == "" {
			t.Errorf("File %s should have a partner", f.Path)
		}
	}

	// Action: Remove Image
	if err := s.RemoveFile(imagePath); err != nil {
		t.Fatalf("Failed to remove image: %v", err)
	}

	// Verify Image is gone
	// We can check by listing pending files again
	filesAfter, err := s.GetPendingFiles(10)
	if err != nil {
		t.Fatalf("Failed to get pending files after removal: %v", err)
	}

	// Should be 1 file (the JSON)
	if len(filesAfter) != 1 {
		t.Errorf("Expected 1 pending file, got %d", len(filesAfter))
	}

	jsonFile := filesAfter[0]
	if jsonFile.Path != jsonPath {
		t.Errorf("Expected remaining file to be %s, got %s", jsonPath, jsonFile.Path)
	}

	// Critical Check: PartnerPath should be NULL/Invalid
	if jsonFile.PartnerPath.Valid {
		t.Errorf("Expected JSON partner_path to be NULL after partner removal, but got: %s", jsonFile.PartnerPath.String)
	}
}
