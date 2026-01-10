package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/ingest"
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

// CompositeLogger writes logs to both the system service logger and a file.
type CompositeLogger struct {
	svcLogger  service.Logger
	fileLogger *log.Logger
}

func (l *CompositeLogger) Error(v ...interface{}) error {
	l.fileLogger.Println(append([]interface{}{"ERROR:"}, v...)...)
	if l.svcLogger != nil {
		return l.svcLogger.Error(v...)
	}
	return nil
}

func (l *CompositeLogger) Warning(v ...interface{}) error {
	l.fileLogger.Println(append([]interface{}{"WARNING:"}, v...)...)
	if l.svcLogger != nil {
		return l.svcLogger.Warning(v...)
	}
	return nil
}

func (l *CompositeLogger) Info(v ...interface{}) error {
	l.fileLogger.Println(append([]interface{}{"INFO:"}, v...)...)
	if l.svcLogger != nil {
		return l.svcLogger.Info(v...)
	}
	return nil
}

func (l *CompositeLogger) Errorf(format string, a ...interface{}) error {
	l.fileLogger.Printf("ERROR: "+format, a...)
	if l.svcLogger != nil {
		return l.svcLogger.Errorf(format, a...)
	}
	return nil
}

func (l *CompositeLogger) Warningf(format string, a ...interface{}) error {
	l.fileLogger.Printf("WARNING: "+format, a...)
	if l.svcLogger != nil {
		return l.svcLogger.Warningf(format, a...)
	}
	return nil
}

func (l *CompositeLogger) Infof(format string, a ...interface{}) error {
	l.fileLogger.Printf("INFO: "+format, a...)
	if l.svcLogger != nil {
		return l.svcLogger.Infof(format, a...)
	}
	return nil
}

type program struct{}

func (p *program) Start(s service.Service) error {
	// 1. Load Config
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
	dbPath := filepath.Join(exPath, "fsd.db")
	dbStore, err = store.NewStore(dbPath)
	if err != nil {
		return fmt.Errorf("failed to init store: %v", err)
	}

	// 3. Start Pruner
	prunerSvc = pruner.NewPruner(cfg, dbStore)
	prunerSvc.Start()

	// 4. Start Ingester
	ingesterSvc = ingest.NewIngester(cfg, dbStore)
	ingesterSvc.Start()

	// 5. Start Watcher
	if err := os.MkdirAll(cfg.WatchPath, 0755); err != nil {
		return fmt.Errorf("failed to create watch dir: %v", err)
	}

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

		// Add to Store as PENDING
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

func main() {
	svcConfig := &service.Config{
		Name:        "fs-ingest-daemon",
		DisplayName: "FS Ingest Daemon",
		Description: "Watches directories and uploads files to the cloud.",
		Arguments:   []string{"run"},
		Option: service.KeyValue{
			"UserService": true,
		},
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	errs := make(chan error, 5)
	sysLogger, err := s.Logger(errs)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			err := <-errs
			if err != nil {
				log.Print(err)
			}
		}
	}()

	// Setup File Logger
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

	var fLogger *log.Logger
	if logFile != nil {
		fLogger = log.New(logFile, "", log.LstdFlags|log.Lmicroseconds)
		log.SetOutput(logFile)
	} else {
		fLogger = log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)
	}

	// Initialize the global logger with the composite logger
	logger = &CompositeLogger{
		svcLogger:  sysLogger,
		fileLogger: fLogger,
	}

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
