package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	Endpoint      string  `json:"endpoint"`
	MaxDataSizeGB float64 `json:"max_data_size_gb"`
	WatchPath     string  `json:"watch_path"`
}

func Load(path string) (*Config, error) {
	// Default config
	cfg := &Config{
		Endpoint:      "https://example.com/upload",
		MaxDataSizeGB: 1.0,
		WatchPath:     "./data",
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return default if no config exists, maybe create it?
			// For now, let's just return default
			return cfg, nil
		}
		return nil, err
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	if err := decoder.Decode(cfg); err != nil {
		return nil, err
	}
	
	// Normalize path
	if cfg.WatchPath == "./data" || cfg.WatchPath == "" {
		ex, err := os.Executable()
		if err == nil {
			cfg.WatchPath = filepath.Join(filepath.Dir(ex), "data")
		}
	}

	return cfg, nil
}

func Save(path string, cfg *Config) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	return encoder.Encode(cfg)
}
