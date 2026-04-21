package simulation_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"fs-ingest-daemon/internal/simulation"
)

func TestSimulator_Run_EmbeddedFS(t *testing.T) {
	// 1. Create an in-memory Mock File System (mimicking assets.SimulationData)
	mockFS := fstest.MapFS{
		"good/1.png":     &fstest.MapFile{Data: []byte("fake-good-image-data")},
		"bad/1.png":      &fstest.MapFile{Data: []byte("fake-bad-image-data")},
		"context_1.json": &fstest.MapFile{Data: []byte(`{"batch_id": "TEST-123"}`)},
	}

	// 2. Create an isolated temporary target directory (mimicking the watch_path)
	targetDir := t.TempDir()

	// 3. Configure the Simulator
	cfg := simulation.Config{
		SourceFS:   mockFS,       // Inject our mocked embedded file system
		TargetDir:  targetDir,    // Drop into our temporary watch path
		Rate:       10 * time.Millisecond, // Super fast rate for testing
		DefectRate: 0.5,          // 50% chance to drop the bad image
		Jitter:     0.0,          // No randomization in timing for tests
		Nested:     false,        // Keep output flat: ./default_cam/file.png
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)), // Discard logs to keep test output clean
	}

	// 4. Initialize the Simulator
	sim, err := simulation.New(cfg)
	if err != nil {
		t.Fatalf("Failed to initialize simulator: %v", err)
	}

	// 5. Run the simulator for a brief moment (e.g., 50ms) to let it drop a few files
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = sim.Run(ctx)
	if err != nil {
		t.Fatalf("Simulator Run returned error: %v", err)
	}

	// 6. Verify the results
	// The category should default to "default_cam" based on our "good/" and "bad/" folder structure
	categoryDir := filepath.Join(targetDir, "default_cam")
	
	entries, err := os.ReadDir(categoryDir)
	if err != nil {
		t.Fatalf("Failed to read output directory: %v", err)
	}

	if len(entries) == 0 {
		t.Errorf("Expected files to be generated in target directory, found none")
	}

	pngCount := 0
	jsonCount := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".png") {
			pngCount++
		}
		if strings.HasSuffix(entry.Name(), ".json") {
			jsonCount++
			
			// Verify JSON content uses our mocked file
			content, _ := os.ReadFile(filepath.Join(categoryDir, entry.Name()))
			if !strings.Contains(string(content), "TEST-123") {
				t.Errorf("Expected JSON sidecar to contain mocked data 'TEST-123', got: %s", string(content))
			}
		}
	}

	// Because of our "strict" mock, we should generate an equal amount of images and sidecars
	if pngCount == 0 || jsonCount == 0 {
		t.Errorf("Expected both PNGs and JSONs to be generated, got PNGs: %d, JSONs: %d", pngCount, jsonCount)
	}
}
