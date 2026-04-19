package simulation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
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

// Variables for directory structure randomization
var (
	Factories = []string{"munich", "berlin", "stuttgart"}
	Lines     = []string{"line_a", "line_b", "line_c"}
)

type Config struct {
	SourceDir  string        // Used mostly for logging if it's external, empty if embedded
	SourceFS   fs.FS         // The filesystem to use for source files
	TargetDir  string        // Where to drop files
	Rate       time.Duration // Interval between files
	DefectRate float64       // Probability of generating a defect image (0.0 to 1.0)
	Jitter     float64       // Variance in rate (e.g. 0.2 for +/- 20%)
	Nested     bool          // Whether to generate nested directories (e.g. factory/line/category)
	Categories []string      // Optional list of specific categories to simulate
	Logger     *slog.Logger
}

type Simulator struct {
	cfg          Config
	categories   []string
	goodFiles    map[string][]string // category -> list of good file paths
	defectFiles  map[string][]string // category -> list of defect file paths
	contextFiles []string            // list of cached context json paths
}

func New(cfg Config) (*Simulator, error) {
	s := &Simulator{
		cfg:         cfg,
		categories:  DefaultCategories,
		goodFiles:   make(map[string][]string),
		defectFiles: make(map[string][]string),
	}

	if len(cfg.Categories) > 0 {
		s.categories = cfg.Categories
	}

	if err := s.scanSourceDir(); err != nil {
		return nil, fmt.Errorf("failed to scan source dir: %w", err)
	}

	// Ensure target directory exists
	if err := os.MkdirAll(cfg.TargetDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create target dir: %w", err)
	}

	return s, nil
}

func (s *Simulator) scanSourceDir() error {
	s.cfg.Logger.Info("Scanning source directory")

	foundCategories := make(map[string]bool)
	allowedCategories := make(map[string]bool)
	for _, c := range s.cfg.Categories {
		allowedCategories[c] = true
	}

	err := fs.WalkDir(s.cfg.SourceFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "ground_truth" {
			return fs.SkipDir
		}
		if d.IsDir() {
			return nil
		}

		// Check for context json files
		if strings.HasSuffix(strings.ToLower(path), "context.json") || strings.HasSuffix(strings.ToLower(path), "context_1.json") || strings.HasSuffix(strings.ToLower(path), "context_2.json") {
			s.contextFiles = append(s.contextFiles, path)
			return nil
		}

		// Check for image extensions
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".png" && ext != ".jpg" && ext != ".jpeg" {
			return nil
		}

		// Try to determine category from path
		parts := strings.Split(path, "/")
		if len(parts) > 1 {
			category := parts[0]

			// For simple structures like "good/0.png" or "bad/0.png", default category to something sensible
			if category == "good" || category == "bad" {
				category = "default_cam"
			}

			if len(allowedCategories) == 0 || allowedCategories[category] {
				isDefect := true
				if strings.Contains(strings.ToLower(path), "/good/") || strings.HasPrefix(strings.ToLower(path), "good/") {
					isDefect = false
				}

				if !isDefect {
					s.goodFiles[category] = append(s.goodFiles[category], path)
				} else {
					s.defectFiles[category] = append(s.defectFiles[category], path)
				}
				foundCategories[category] = true
			}
		} else {
			// Files in root directory
			category := "default_cam"
			if len(allowedCategories) == 0 || allowedCategories[category] {
				s.goodFiles[category] = append(s.goodFiles[category], path)
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
		s.cfg.Logger.Warn("No source files found for requested categories. Simulation might be limited.", "categories", s.cfg.Categories)
	} else {
		s.cfg.Logger.Warn("No categories found in source directory, falling back to default categories with synthetic data")
	}

	return nil
}

func (s *Simulator) Run(ctx context.Context) error {
	s.cfg.Logger.Info("Starting simulation",
		"rate", s.cfg.Rate,
		"defect_rate", s.cfg.DefectRate,
		"jitter", s.cfg.Jitter,
		"target", s.cfg.TargetDir,
		"mode", s.modeName(),
	)

	timer := time.NewTimer(s.nextInterval())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			s.cfg.Logger.Info("Stopping simulation")
			return nil
		case <-timer.C:
			if err := s.generateFile(); err != nil {
				s.cfg.Logger.Error("Failed to generate file", "error", err)
			}
			timer.Reset(s.nextInterval())
		}
	}
}

func (s *Simulator) nextInterval() time.Duration {
	base := float64(s.cfg.Rate)
	if s.cfg.Jitter <= 0 {
		return s.cfg.Rate
	}

	variance := ((rand.Float64() * 2.0) - 1.0) * s.cfg.Jitter
	newDuration := time.Duration(base * (1.0 + variance))

	if newDuration <= 0 {
		return time.Millisecond
	}
	return newDuration
}

func (s *Simulator) modeName() string {
	if len(s.goodFiles) > 0 || len(s.defectFiles) > 0 {
		return "Replay"
	}
	return "Synthetic"
}

func (s *Simulator) generateFile() error {
	category := s.categories[rand.Intn(len(s.categories))]

	var sourcePath string
	var content []byte
	var ext string = ".png"

	isDefect := rand.Float64() < s.cfg.DefectRate
	var files []string

	if isDefect {
		files = s.defectFiles[category]
		if len(files) == 0 {
			files = s.goodFiles[category]
			isDefect = false
		}
	} else {
		files = s.goodFiles[category]
		if len(files) == 0 {
			files = s.defectFiles[category]
			isDefect = true
		}
	}

	if len(files) > 0 {
		sourcePath = files[rand.Intn(len(files))]
		ext = filepath.Ext(sourcePath)
	}

	var depth int
	if s.cfg.Nested {
		depth = rand.Intn(3)
	} else {
		depth = 0
	}

	var factory, line string
	var pathParts []string

	if depth >= 1 {
		factory = Factories[rand.Intn(len(Factories))]
		pathParts = append(pathParts, factory)
	}
	if depth >= 2 {
		line = Lines[rand.Intn(len(Lines))]
		pathParts = append(pathParts, line)
	}

	pathParts = append(pathParts, category)

	filename := fmt.Sprintf("%s%s", uuid.New().String(), ext)
	targetDir := filepath.Join(s.cfg.TargetDir, filepath.Join(pathParts...))
	targetPath := filepath.Join(targetDir, filename)

	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", targetDir, err)
	}

	if sourcePath != "" {
		src, err := s.cfg.SourceFS.Open(sourcePath)
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
		content = make([]byte, 1024)
		rand.Read(content)

		if err := os.WriteFile(targetPath, content, 0644); err != nil {
			return fmt.Errorf("failed to write synthetic file: %w", err)
		}
		s.cfg.Logger.Info("Generated", "target", targetPath)
	}

	if !strings.HasSuffix(targetPath, ".json") {
		jsonPath := targetPath + ".json"
		
		contextMap := map[string]interface{}{}

		// Try to load a cached context JSON file
		if len(s.contextFiles) > 0 {
			ctxPath := s.contextFiles[rand.Intn(len(s.contextFiles))]
			if ctxFile, err := s.cfg.SourceFS.Open(ctxPath); err == nil {
				jsonContent, _ := io.ReadAll(ctxFile)
				ctxFile.Close()
				
				// Unmarshal static JSON to dynamically inject properties
				json.Unmarshal(jsonContent, &contextMap)
			}
		}

		// Inject/Overwrite dynamic simulation properties
		contextMap["simulation"] = true
		contextMap["category"] = category
		contextMap["is_defect"] = isDefect
		contextMap["timestamp"] = time.Now().Format(time.RFC3339)
		if factory != "" {
			contextMap["factory"] = factory
		}
		if line != "" {
			contextMap["line"] = line
		}

		finalJsonContent, err := json.MarshalIndent(contextMap, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to encode sidecar json: %w", err)
		}

		if err := os.WriteFile(jsonPath, finalJsonContent, 0644); err != nil {
			return fmt.Errorf("failed to write sidecar file: %w", err)
		}
		s.cfg.Logger.Debug("Generated sidecar", "path", jsonPath)
	}

	return nil
}
