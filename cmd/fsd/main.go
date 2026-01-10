package main

// Package main is the entry point for the fs-ingest-daemon.
// It handles the service lifecycle (install, start, stop) using the kardianos/service package,
// initializes all internal components (config, store, pruner, ingester, watcher),
// and provides the CLI interface using Cobra.

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/ingest"
	fsdlog "fs-ingest-daemon/internal/logger"
	"fs-ingest-daemon/internal/pruner"
	"fs-ingest-daemon/internal/store"
	"fs-ingest-daemon/internal/watcher"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

var (
	logger      service.Logger
	cfg         *config.Config
	dbStore     *store.Store
	prunerSvc   *pruner.Pruner
	ingesterSvc *ingest.Ingester
	watcherSvc  *watcher.Watcher
)

// program implements the service.Interface required by kardianos/service.
// It acts as the controller for the daemon's lifecycle events.
type program struct{}

// Start is called when the service is started.
// It initializes the configuration, database, and background workers (Pruner, Ingester, Watcher).
// This method must not block; the actual work is done in background goroutines started by the components.
func (p *program) Start(s service.Service) error {
	// 1. Load Config
	// We determine the executable path to locate config.json relative to the binary.
	ex, err := os.Executable()
	if err != nil {
		return err
	}
	exPath := filepath.Dir(ex)

	cfgPath := filepath.Join(exPath, "config.json")
	cfg, err = config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %v", err)
	}

	// Ensure config file exists for user convenience if it didn't
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		config.Save(cfgPath, cfg)
	}

	// 2. Initialize Store
	// The SQLite database is stored alongside the executable.
	dbPath := filepath.Join(exPath, "fsd.db")
	dbStore, err = store.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("failed to init store: %v", err)
	}

	// 3. Start Pruner
	// The pruner runs in the background and cleans up old uploaded files.
	prunerSvc = pruner.NewPruner(cfg, dbStore)
	prunerSvc.Start()

	// 4. Start Ingester
	// The ingester watches the DB for pending files and uploads them.
	ingesterSvc = ingest.NewIngester(cfg, dbStore)
	ingesterSvc.Start()

	// 5. Start Watcher
	// Ensure the watch directory exists before starting the watcher.
	if err := os.MkdirAll(cfg.WatchPath, 0755); err != nil {
		return fmt.Errorf("failed to create watch dir: %v", err)
	}

	// onNewFile is the callback triggered by the watcher when a new file is detected.
	onNewFile := func(path string) {
		info, err := os.Stat(path)
		if err != nil {
			if logger != nil {
				logger.Error(fmt.Errorf("stat error: %v", err))
			}
			return
		}
		if info.IsDir() {
			return
		}

		// Add the detected file to the Store with status PENDING.
		// If the file is already tracked, this might update its metadata.
		if err := dbStore.AddOrUpdateFile(path, info.Size(), info.ModTime()); err != nil {
			if logger != nil {
				logger.Error(fmt.Errorf("db error: %v", err))
			}
		} else {
			if logger != nil {
				logger.Info("Detected: " + path)
			}
		}
	}

	// Initialize the recursive file system watcher.
	watcherSvc, err = watcher.NewWatcher(cfg.WatchPath, onNewFile)
	if err != nil {
		return fmt.Errorf("failed to start watcher: %v", err)
	}

	if logger != nil {
		logger.Info("FS Ingest Daemon Started")
		logger.Info(fmt.Sprintf("Watching: %s", cfg.WatchPath))
		logger.Info(fmt.Sprintf("Endpoint: %s", cfg.Endpoint))
	}

	return nil
}

// Stop is called when the service is being stopped.
// It gracefully shuts down all active components and closes database connections.
func (p *program) Stop(s service.Service) error {
	if logger != nil {
		logger.Info("Stopping FS Ingest Daemon...")
	}
	if watcherSvc != nil {
		watcherSvc.Close()
	}
	if ingesterSvc != nil {
		ingesterSvc.Stop()
	}
	if prunerSvc != nil {
		prunerSvc.Stop()
	}
	if dbStore != nil {
		dbStore.Close()
	}
	return nil
}

// main configures the service meta-data and sets up the Cobra CLI commands.
func main() {
	svcConfig := &service.Config{
		Name:        "fs-ingest-daemon",
		DisplayName: "FS Ingest Daemon",
		Description: "Watches directories and uploads files to the cloud.",
		Arguments:   []string{"run"},
		Option: service.KeyValue{
			"UserService": true, // Run as a user-level service where applicable
		},
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	// Setup the system logger (event log / syslog)
	errs := make(chan error, 5)
	sysLogger, err := s.Logger(errs)
	if err != nil {
		log.Fatal(err)
	}

	// Goroutine to handle logger errors
	go func() {
		for {
			err := <-errs
			if err != nil {
				log.Print(err)
			}
		}
	}()

	// Setup File Logger to write logs to a local file
	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	logPath := filepath.Join(filepath.Dir(ex), "fsd.log")

	// Open file for appending, create if not exists
	logFile, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		// Fallback to stderr if file cannot be opened
		log.Printf("Failed to open log file: %v\n", err)
	} else {
		defer logFile.Close()
	}

	// Create the standard go logger for the file
	var fLogger *log.Logger
	if logFile != nil {
		fLogger = log.New(logFile, "", log.LstdFlags|log.Lmicroseconds)
		log.SetOutput(logFile)
	} else {
		fLogger = log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)
	}

	// Initialize the global logger with the composite logger (System + File)
	logger = fsdlog.New(sysLogger, fLogger)

	// --- CLI Commands Setup ---

	var rootCmd = &cobra.Command{
		Use:   "fsd",
		Short: "FS Ingest Daemon CLI",
	}

	var installCmd = &cobra.Command{
		Use:   "install",
		Short: "Install the service",
		Run: func(cmd *cobra.Command, args []string) {
			err := s.Install()
			if err != nil {
				fmt.Printf("Failed to install: %s\n", err)
				return
			}
			fmt.Println("Service installed.")
		},
	}

	var uninstallCmd = &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the service",
		Run: func(cmd *cobra.Command, args []string) {
			err := s.Uninstall()
			if err != nil {
				fmt.Printf("Failed to uninstall: %s\n", err)
				return
			}
			fmt.Println("Service uninstalled.")
		},
	}

	var startCmd = &cobra.Command{
		Use:   "start",
		Short: "Start the service",
		Run: func(cmd *cobra.Command, args []string) {
			err := s.Start()
			if err != nil {
				fmt.Printf("Failed to start: %s\n", err)
				return
			}
			fmt.Println("Service started.")
		},
	}

	var stopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop the service",
		Run: func(cmd *cobra.Command, args []string) {
			err := s.Stop()
			if err != nil {
				fmt.Printf("Failed to stop: %s\n", err)
				return
			}
			fmt.Println("Service stopped.")
		},
	}

	var runCmd = &cobra.Command{
		Use:   "run",
		Short: "Run the service in foreground",
		Run: func(cmd *cobra.Command, args []string) {
			err := s.Run()
			if err != nil {
				logger.Error(err)
			}
		},
	}

	var statusCmd = &cobra.Command{
		Use:   "status",
		Short: "Show service status",
		Run: func(cmd *cobra.Command, args []string) {
			status, err := s.Status()
			if err != nil {
				fmt.Printf("Error getting status: %v\n", err)
				return
			}
			// StatusUnknown Status = 0
			// StatusRunning Status = 1
			// StatusStopped Status = 2
			switch status {
			case service.StatusRunning:
				fmt.Println("Running")
			case service.StatusStopped:
				fmt.Println("Stopped")
			default:
				fmt.Println("Unknown/Other")
			}
		},
	}

	var logsCmd = &cobra.Command{
		Use:   "logs",
		Short: "Show service logs",
		Run: func(cmd *cobra.Command, args []string) {
			f, err := os.Open(logPath)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("No logs found.")
					return
				}
				fmt.Printf("Error opening log file: %v\n", err)
				return
			}
			defer f.Close()
			if _, err := io.Copy(os.Stdout, f); err != nil {
				fmt.Printf("Error reading logs: %v\n", err)
			}
		},
	}

	rootCmd.AddCommand(installCmd, uninstallCmd, startCmd, stopCmd, runCmd, statusCmd, logsCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
