package daemon

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/ingest"
	"fs-ingest-daemon/internal/pruner"
	"fs-ingest-daemon/internal/store"
	"fs-ingest-daemon/internal/watcher"

	"github.com/kardianos/service"
)

// Daemon implements the service.Interface required by kardianos/service.
// It acts as the controller for the daemon's lifecycle events.
type Daemon struct {
	Logger      *slog.Logger
	Cfg         *config.Config
	DbStore     *store.Store
	PrunerSvc   *pruner.Pruner
	IngesterSvc *ingest.Ingester
	WatcherSvc  *watcher.Watcher
}

// Start is called when the service is started.
// It initializes the configuration, database, and background workers (Pruner, Ingester, Watcher).
func (d *Daemon) Start(s service.Service) error {
	// 1. Load Config
	ex, err := os.Executable()
	if err != nil {
		return err
	}
	exPath := filepath.Dir(ex)
	cfgPath := filepath.Join(exPath, "config.json")

	d.Cfg, err = config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	// Ensure config file exists for user convenience if it didn't
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		config.Save(cfgPath, d.Cfg)
	}

	// 2. Initialize Store
	dbPath := filepath.Join(exPath, "fsd.db")
	d.DbStore, err = store.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("failed to init store: %v", err)
	}

	// 3. Start Pruner
	d.PrunerSvc = pruner.NewPruner(d.Cfg, d.DbStore, d.Logger)
	d.PrunerSvc.Start()

	// 4. Start Ingester
	d.IngesterSvc = ingest.NewIngester(d.Cfg, d.DbStore, d.Logger)
	d.IngesterSvc.Start()

	// 5. Start Watcher
	if err := os.MkdirAll(d.Cfg.WatchPath, 0755); err != nil {
		return fmt.Errorf("failed to create watch dir: %v", err)
	}

	onNewFile := func(path string) {
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

		if err := d.DbStore.AddOrUpdateFile(path, info.Size(), info.ModTime()); err != nil {
			if d.Logger != nil {
				d.Logger.Error("db error", "error", err)
			}
		} else {
			if d.Logger != nil {
				d.Logger.Info("Detected", "path", path)
			}
		}
	}

	d.WatcherSvc, err = watcher.NewWatcher(d.Cfg.WatchPath, onNewFile, d.Logger)
	if err != nil {
		return fmt.Errorf("failed to start watcher: %v", err)
	}

	if d.Logger != nil {
		d.Logger.Info("FS Ingest Daemon Started")
		d.Logger.Info("Configuration", "watch_path", d.Cfg.WatchPath, "endpoint", d.Cfg.Endpoint)
	}

	return nil
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
