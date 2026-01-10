package config

// Package config handles loading, saving, and managing the daemon's configuration.
// It supports reading from a JSON file and provides default values for valid initialization.

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config represents the application configuration structure.
type Config struct {
	DeviceID      string  `json:"device_id"`        // Unique identifier for the device (e.g., "dev-001")
	Endpoint      string  `json:"endpoint"`         // The API base URL
	MaxDataSizeGB float64 `json:"max_data_size_gb"` // Maximum allowed size for the local storage in GB before pruning kicks in
	WatchPath     string  `json:"watch_path"`       // The local directory path to watch for new files
}

// Load reads the configuration from the specified path.
// If the file does not exist, it returns a default configuration structure.
func Load(path string) (*Config, error) {
	// Initialize with sensible defaults
	cfg := &Config{
		DeviceID:      "dev-001",
		Endpoint:      "https://localhost:8080",
		MaxDataSizeGB: 1.0,
		WatchPath:     "./data",
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

	// Normalize WatchPath:
	// If it is the default relative path "./data" or empty, resolve it relative to the executable.
	if cfg.WatchPath == "./data" || cfg.WatchPath == "" {
		ex, err := os.Executable()
		if err == nil {
			cfg.WatchPath = filepath.Join(filepath.Dir(ex), "data")
		}
	}

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
