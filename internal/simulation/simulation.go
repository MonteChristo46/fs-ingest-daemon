package simulation

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Default categories acting as simulated data sources (cameras)
var DefaultCategories = []string{
	"cam_1", "cam_2", "cam_3", "cam_4", "cam_5",
}

type Config struct {
	SourceDir  string        // If empty, use synthetic mode
	TargetDir  string        // Where to drop files
	Rate       time.Duration // Interval between files
	Categories []string      // Optional list of specific categories to simulate
	Logger     *slog.Logger
}

type Simulator struct {
	cfg        Config
	categories []string
	imageFiles map[string][]string // category -> list of file paths
}

func New(cfg Config) (*Simulator, error) {
	s := &Simulator{
		cfg:        cfg,
		categories: DefaultCategories,
		imageFiles: make(map[string][]string),
	}

	if len(cfg.Categories) > 0 {
		s.categories = cfg.Categories
	}

	if cfg.SourceDir != "" {
		if err := s.scanSourceDir(); err != nil {
			return nil, fmt.Errorf("failed to scan source dir: %w", err)
		}
	}

	// Ensure target directory exists
	if err := os.MkdirAll(cfg.TargetDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create target dir: %w", err)
	}

	return s, nil
}

func (s *Simulator) scanSourceDir() error {
	s.cfg.Logger.Info("Scanning source directory", "path", s.cfg.SourceDir)

	// Reset categories if we find real ones
	foundCategories := make(map[string]bool)
	allowedCategories := make(map[string]bool)
	for _, c := range s.cfg.Categories {
		allowedCategories[c] = true
	}

	err := filepath.Walk(s.cfg.SourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == "ground_truth" {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}

		// Check for image extensions
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
			return nil
		}

		// Try to determine category from path
		// Expected structure: source/category/.../image.png
		relPath, err := filepath.Rel(s.cfg.SourceDir, path)
		if err != nil {
			return nil
		}

		parts := strings.Split(relPath, string(os.PathSeparator))
		if len(parts) > 1 {
			category := parts[0]
			if len(allowedCategories) == 0 || allowedCategories[category] {
				s.imageFiles[category] = append(s.imageFiles[category], path)
				foundCategories[category] = true
			}
		}

		return nil
	})

	if err != nil {
		return err
	}

	if len(foundCategories) > 0 {
		s.categories = make([]string, 0, len(foundCategories))
		for cat := range foundCategories {
			s.categories = append(s.categories, cat)
		}
		s.cfg.Logger.Info("Found categories in source", "count", len(s.categories), "categories", s.categories)
	} else if len(s.cfg.Categories) > 0 {
		// Used requested categories even if not found in source, or if source was empty
		s.cfg.Logger.Warn("No source files found for requested categories. Simulation might be limited.", "categories", s.cfg.Categories)
	} else {
		s.cfg.Logger.Warn("No categories found in source directory, falling back to default categories with synthetic data")
	}

	return nil
}

func (s *Simulator) Run(ctx context.Context) error {
	s.cfg.Logger.Info("Starting simulation",
		"rate", s.cfg.Rate,
		"target", s.cfg.TargetDir,
		"mode", s.modeName(),
	)

	ticker := time.NewTicker(s.cfg.Rate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.cfg.Logger.Info("Stopping simulation")
			return nil
		case <-ticker.C:
			if err := s.generateFile(); err != nil {
				s.cfg.Logger.Error("Failed to generate file", "error", err)
			}
		}
	}
}

func (s *Simulator) modeName() string {
	if len(s.imageFiles) > 0 {
		return "Replay"
	}
	return "Synthetic"
}

func (s *Simulator) generateFile() error {
	// 1. Pick random category
	category := s.categories[rand.Intn(len(s.categories))]

	// 2. Determine source content
	var sourcePath string
	var content []byte
	var ext string = ".png" // Default for synthetic

	if files, ok := s.imageFiles[category]; ok && len(files) > 0 {
		sourcePath = files[rand.Intn(len(files))]
		ext = filepath.Ext(sourcePath)
	}

	// 3. Construct target path
	// Structure: target/category/filename
	filename := fmt.Sprintf("%s%s", uuid.New().String(), ext)
	targetDir := filepath.Join(s.cfg.TargetDir, category)
	targetPath := filepath.Join(targetDir, filename)

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", targetDir, err)
	}

	// 4. Write file
	if sourcePath != "" {
		// Replay mode: Copy file
		src, err := os.Open(sourcePath)
		if err != nil {
			return fmt.Errorf("failed to open source file: %w", err)
		}
		defer src.Close()

		dst, err := os.Create(targetPath)
		if err != nil {
			return fmt.Errorf("failed to create target file: %w", err)
		}
		defer dst.Close()

		if _, err := io.Copy(dst, src); err != nil {
			return fmt.Errorf("failed to copy file: %w", err)
		}
		s.cfg.Logger.Debug("Copied file", "source", sourcePath, "target", targetPath)
	} else {
		// Synthetic mode: Generate dummy content
		// Create a small dummy image (just random bytes or a specific pattern)
		// For now, let's just write a string "DUMMY IMAGE CONTENT"
		// Ideally this would be a valid PNG header, but for basic ingestion testing strings might suffice
		// unless the ingestor validates image headers strictly.
		// Let's create a minimal 1x1 PNG to be safe if validation exists.
		// Or just random bytes if validation is loose.
		// Given the requirements, let's generate random noise for now.
		content = make([]byte, 1024)
		rand.Read(content)

		if err := os.WriteFile(targetPath, content, 0644); err != nil {
			return fmt.Errorf("failed to write synthetic file: %w", err)
		}
		s.cfg.Logger.Info("Generated", "target", targetPath)
	}

	// 5. Generate JSON sidecar to satisfy "strict" strategy
	// We only do this for images (to avoid sidecars for sidecars)
	if !strings.HasSuffix(targetPath, ".json") {
		jsonPath := targetPath + ".json"
		// Simple dummy context
		jsonContent := []byte(fmt.Sprintf(`{"simulation": true, "category": %q, "timestamp": %q}`,
			category, time.Now().Format(time.RFC3339)))

		if err := os.WriteFile(jsonPath, jsonContent, 0644); err != nil {
			return fmt.Errorf("failed to write sidecar file: %w", err)
		}
		s.cfg.Logger.Debug("Generated sidecar", "path", jsonPath)
	}

	return nil
}
