package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"fs-ingest-daemon/internal/api"
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/ingest"
	"fs-ingest-daemon/internal/pruner"
	"fs-ingest-daemon/internal/store"
	"fs-ingest-daemon/internal/sysinfo"
	"fs-ingest-daemon/internal/watcher"

	"github.com/kardianos/service"
)

// Daemon implements the service.Interface required by kardianos/service.
// It acts as the controller for the daemon's lifecycle events.
type Daemon struct {
	Logger      *slog.Logger
	Cfg         *config.Config
	DbStore     *store.Store
	ApiClient   *api.Client
	PrunerSvc   *pruner.Pruner
	IngesterSvc *ingest.Ingester
	WatcherSvc  *watcher.Watcher
}

// Start is called when the service is started.
// It initializes the configuration, database, and background workers (Pruner, Ingester, Watcher).
func (d *Daemon) Start(s service.Service) error {
	// 1. Load Config if not already loaded (main usually loads it)
	ex, err := os.Executable()
	if err != nil {
		return err
	}
	exPath := filepath.Dir(ex)
	cfgPath := filepath.Join(exPath, "config.json")

	if d.Cfg == nil {
		d.Cfg, err = config.Load(cfgPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %v", err)
		}
	}

	// Ensure config file exists for user convenience if it didn't
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		config.Save(cfgPath, d.Cfg)
	}

	// 2. Initialize Store using configured DB Path
	d.DbStore, err = store.NewStore(d.Cfg.DBPath)
	if err != nil {
		return fmt.Errorf("failed to init store at %s: %v", d.Cfg.DBPath, err)
	}

	// 3. Initialize API Client
	d.ApiClient = api.NewClient(d.Cfg.Endpoint, d.Cfg.APITimeout)

	// 4. Start Pruner
	d.PrunerSvc = pruner.NewPruner(d.Cfg, d.DbStore, d.Logger)
	d.PrunerSvc.Start()

	// 5. Start Ingester
	d.IngesterSvc = ingest.NewIngester(d.Cfg, d.DbStore, d.Logger)
	d.IngesterSvc.Start()

	// 6. Start Watcher
	if err := os.MkdirAll(d.Cfg.WatchPath, 0755); err != nil {
		return fmt.Errorf("failed to create watch dir: %v", err)
	}

	debounceDur, err := time.ParseDuration(d.Cfg.DebounceDuration)
	if err != nil {
		if d.Logger != nil {
			d.Logger.Error("Invalid debounce duration, defaulting to 500ms", "error", err)
		}
		debounceDur = 500 * time.Millisecond
	}

	d.WatcherSvc, err = watcher.NewWatcher(d.Cfg.WatchPath, debounceDur, d.processFile, d.Logger)
	if err != nil {
		return fmt.Errorf("failed to start watcher: %v", err)
	}

	// 7. Initial Scan to catch files created while daemon was offline
	go d.scanExistingFiles()

	// 8. Start Orphan Checker
	go d.orphanChecker()

	// 9. Start Metadata Updater
	go d.metadataUpdater()

	if d.Logger != nil {
		d.Logger.Info("FS Ingest Daemon Started")
		d.Logger.Info("Configuration", "watch_path", d.Cfg.WatchPath, "endpoint", d.Cfg.Endpoint)
	}

	return nil
}

// metadataUpdater runs periodically to collect and send system metadata.
func (d *Daemon) metadataUpdater() {
	interval, err := time.ParseDuration(d.Cfg.MetadataUpdateInterval)
	if err != nil {
		if d.Logger != nil {
			d.Logger.Error("Invalid metadata update interval, defaulting to 24h", "error", err)
		}
		interval = 24 * time.Hour
	}

	// Wait a bit before the first run to allow the system to stabilize
	time.Sleep(10 * time.Second)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	update := func() {
		info, err := sysinfo.Collect()
		if err != nil {
			if d.Logger != nil {
				d.Logger.Error("Failed to collect system info", "error", err)
			}
			return
		}

		if _, err := d.ApiClient.UpdateDeviceMetadata(d.Cfg.DeviceID, info); err != nil {
			if d.Logger != nil {
				d.Logger.Error("Failed to update device metadata", "error", err)
			}
		} else {
			if d.Logger != nil {
				d.Logger.Info("Device metadata updated successfully")
			}
		}
	}

	// Run immediately once
	update()

	for {
		select {
		case <-ticker.C:
			update()
		}
	}
}

// orphanChecker runs periodically to mark timed-out files as ORPHAN.
func (d *Daemon) orphanChecker() {
	orphanInterval, err := time.ParseDuration(d.Cfg.OrphanCheckInterval)
	if err != nil {
		d.Logger.Error("Invalid orphan check interval, defaulting to 5 minutes", "error", err)
		orphanInterval = 5 * time.Minute
	}

	ticker := time.NewTicker(orphanInterval)
	defer ticker.Stop()

	// Use a timeout slightly less than the check interval to avoid race conditions.
	// For example, if we check every 5m, mark files older than 4m as orphans.
	timeout := orphanInterval - 1*time.Minute
	if timeout < 1*time.Minute { // Ensure timeout is not ridiculously small
		timeout = 1 * time.Minute
	}

	for {
		select {
		case <-ticker.C:
			if err := d.DbStore.MarkOrphans(timeout); err != nil {
				if d.Logger != nil {
					d.Logger.Error("Failed to mark orphans", "error", err)
				}
			}
			// We rely on service stop to kill this goroutine implicitly when the process exits,
			// or we could add a stop channel if strictly needed.
			// For simplicity in this daemon structure, we assume process termination.
		}
	}
}

// processFile handles a detected file by adding it to the store.
func (d *Daemon) processFile(path string) {
	info, err := os.Stat(path)
	if err != nil {
		if d.Logger != nil {
			d.Logger.Error("stat error", "error", err)
		}
		return
	}
	if info.IsDir() {
		return
	}

	// Check extension to determine if it is metadata
	isMeta := filepath.Ext(path) == ".json"

	if err := d.DbStore.RegisterFile(path, info.Size(), info.ModTime(), isMeta); err != nil {
		if d.Logger != nil {
			d.Logger.Error("db error", "error", err)
		}
	} else {
		if d.Logger != nil {
			d.Logger.Info("Detected", "path", path)
		}
	}
}

// scanExistingFiles walks the watch path and processes all existing files.
func (d *Daemon) scanExistingFiles() {
	if d.Logger != nil {
		d.Logger.Info("Performing initial scan", "path", d.Cfg.WatchPath)
	}
	err := filepath.Walk(d.Cfg.WatchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			d.processFile(path)
		}
		return nil
	})
	if err != nil && d.Logger != nil {
		d.Logger.Error("Initial scan failed", "error", err)
	}
}

// Stop is called when the service is being stopped.
func (d *Daemon) Stop(s service.Service) error {
	if d.Logger != nil {
		d.Logger.Info("Stopping FS Ingest Daemon...")
	}
	if d.WatcherSvc != nil {
		d.WatcherSvc.Close()
	}
	if d.IngesterSvc != nil {
		d.IngesterSvc.Stop()
	}
	if d.PrunerSvc != nil {
		d.PrunerSvc.Stop()
	}
	if d.DbStore != nil {
		d.DbStore.Close()
	}
	return nil
}
