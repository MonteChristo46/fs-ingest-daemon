package config

// Package config handles loading, saving, and managing the daemon's configuration.
// It supports reading from a JSON file and provides default values for valid initialization.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Config represents the application configuration structure.
type Config struct {
	DeviceID                  string   `json:"device_id"`                    // Unique identifier for the device (e.g., "dev-001")
	Endpoint                  string   `json:"endpoint"`                     // The API base URL
	MaxDataSizeGB             float64  `json:"max_data_size_gb"`             // Maximum allowed size for the local storage in GB before pruning kicks in
	WatchPath                 string   `json:"watch_path"`                   // The local directory path to watch for new files
	LogPath                   string   `json:"log_path"`                     // Path to the log file
	DBPath                    string   `json:"db_path"`                      // Path to the SQLite database
	IngestCheckInterval       string   `json:"ingest_check_interval"`        // Duration string (e.g. "2s") for ingest polling
	IngestBatchSize           int      `json:"ingest_batch_size"`            // Number of files to process per ingest tick
	IngestWorkerCount         int      `json:"ingest_worker_count"`          // Number of concurrent upload workers
	PruneCheckInterval        string   `json:"prune_check_interval"`         // Duration string (e.g. "1m") for prune checks
	PruneBatchSize            int      `json:"prune_batch_size"`             // Number of files to prune per tick
	PruneHighWatermarkPercent int      `json:"prune_high_watermark_percent"` // Start pruning when usage > MaxDataSizeGB * (High/100)
	PruneLowWatermarkPercent  int      `json:"prune_low_watermark_percent"`  // Stop pruning when usage < MaxDataSizeGB * (Low/100)
	APITimeout                string   `json:"api_timeout"`                  // HTTP Client timeout duration string
	DebounceDuration          string   `json:"debounce_duration"`            // Duration string (e.g. "500ms") for watcher debounce
	OrphanCheckInterval       string   `json:"orphan_check_interval"`        // Duration string (e.g. "5m") for orphan checks
	MetadataUpdateInterval    string   `json:"metadata_update_interval"`     // Duration string (e.g. "24h") for device metadata updates
	AuthToken                 string   `json:"auth_token"`                   // Token indicating the device is registered (or empty if not)
	WebClientURL              string   `json:"web_client_url"`               // URL where the user claims the device
	SidecarStrategy           string   `json:"sidecar_strategy"`             // "strict" (default) or "none" (image only)
	LogMaxSizeMB              int      `json:"log_max_size_mb"`              // Max size in MB before rotation. Default 10.
	LogMaxBackups             int      `json:"log_max_backups"`              // Max number of old files to keep. Default 3.
	LogMaxAgeDays             int      `json:"log_max_age_days"`             // Max number of days to keep old files. Default 28.
	LogCompress               bool     `json:"log_compress"`                 // Whether to compress old files. Default true.
	AllowedExtensions         []string `json:"allowed_extensions"`           // List of allowed file extensions (e.g. [".jpg", ".json"])
}

var (
	// Default configuration values
	DefaultEndpoint                  = "https://glitch-hunt-ingestion.my-basement.cloud"
	DefaultWebClientURL              = "http://glitch-hunt.my-basement.cloud"
	DefaultMaxDataSizeGB             = 1.0
	DefaultIngestCheckInterval       = "20ms"
	DefaultIngestBatchSize           = 10
	DefaultIngestWorkerCount         = 5
	DefaultPruneCheckInterval        = "1m"
	DefaultPruneBatchSize            = 50
	DefaultPruneHighWatermarkPercent = 90
	DefaultPruneLowWatermarkPercent  = 75
	DefaultAPITimeout                = "30s"
	DefaultDebounceDuration          = "500ms"
	DefaultOrphanCheckInterval       = "5m"
	DefaultMetadataUpdateInterval    = "24h"
	DefaultSidecarStrategy           = "none"
	DefaultLogMaxSizeMB              = 10
	DefaultLogMaxBackups             = 1
	DefaultLogMaxAgeDays             = 28
	DefaultLogCompress               = true
	DefaultAllowedExtensions         = []string{".jpg", ".jpeg", ".png", ".json"}
)

// Load reads the configuration from the specified path.
// If the file does not exist, it returns a default configuration structure.
func Load(path string) (*Config, error) {
	// Initialize with sensible defaults
	cfg := &Config{
		DeviceID:                  "dev-001",
		Endpoint:                  DefaultEndpoint,
		MaxDataSizeGB:             DefaultMaxDataSizeGB,
		WatchPath:                 "./data",
		LogPath:                   "./fsd.log",
		DBPath:                    "./fsd.db",
		IngestCheckInterval:       DefaultIngestCheckInterval,
		IngestBatchSize:           DefaultIngestBatchSize,
		IngestWorkerCount:         DefaultIngestWorkerCount,
		PruneCheckInterval:        DefaultPruneCheckInterval,
		PruneBatchSize:            DefaultPruneBatchSize,
		PruneHighWatermarkPercent: DefaultPruneHighWatermarkPercent,
		PruneLowWatermarkPercent:  DefaultPruneLowWatermarkPercent,
		APITimeout:                DefaultAPITimeout,
		DebounceDuration:          DefaultDebounceDuration,
		OrphanCheckInterval:       DefaultOrphanCheckInterval,
		MetadataUpdateInterval:    DefaultMetadataUpdateInterval,
		WebClientURL:              DefaultWebClientURL,
		SidecarStrategy:           DefaultSidecarStrategy,
		LogMaxSizeMB:              DefaultLogMaxSizeMB,
		LogMaxBackups:             DefaultLogMaxBackups,
		LogMaxAgeDays:             DefaultLogMaxAgeDays,
		LogCompress:               DefaultLogCompress,
		AllowedExtensions:         DefaultAllowedExtensions,
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default if no config exists.
			// The caller (main) may decide to save this default to disk.
			return cfg, nil
		}
		return nil, err
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	if err := decoder.Decode(cfg); err != nil {
		return nil, err
	}

	// Helper to resolve relative paths against executable directory
	resolvePath := func(p string) string {
		if p == "" {
			return p
		}
		if !filepath.IsAbs(p) && (strings.HasPrefix(p, "./") || !strings.HasPrefix(p, "/")) { // simplistic check
			ex, err := os.Executable()
			if err == nil {
				return filepath.Join(filepath.Dir(ex), p)
			}
		}
		return p
	}

	// Normalize Paths if they are defaults or relative
	if cfg.WatchPath == "./data" {
		cfg.WatchPath = resolvePath("data")
	} else {
		cfg.WatchPath = resolvePath(cfg.WatchPath)
	}

	cfg.LogPath = resolvePath(cfg.LogPath)
	cfg.DBPath = resolvePath(cfg.DBPath)

	return cfg, nil
}

// Save writes the provided Config struct to the specified path as a JSON file.
func Save(path string, cfg *Config) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ") // Pretty print for human readability
	return encoder.Encode(cfg)
}
