package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"fs-ingest-daemon/internal/cli"
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/daemon"
	fsdlog "fs-ingest-daemon/internal/logger"

	"github.com/kardianos/service"
)

func main() {
	// 1. Load Config early to get LogPath
	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	cfgPath := filepath.Join(filepath.Dir(ex), "config.json")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		// If config fails to load, we'll try to proceed with defaults or log to stderr later
		log.Printf("Warning: Failed to load config early: %v\n", err)
		// We can't really do much else if we want to respect the user's log path preference,
		// but defaults in Load() should handle it if file is missing.
	}

	svcConfig := &service.Config{
		Name:        "fs-ingest-daemon",
		DisplayName: "FS Ingest Daemon",
		Description: "Watches directories and uploads files to the cloud.",
		Arguments:   []string{"run"},
		Option: service.KeyValue{
			"UserService": true,
		},
	}

	// Create the daemon instance (implements service.Interface)
	// Pass the pre-loaded config
	dmn := &daemon.Daemon{
		Cfg: cfg,
	}

	s, err := service.New(dmn, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	// Setup Logger
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

	// Use LogPath from config
	logPath := cfg.LogPath
	if logPath == "" {
		logPath = filepath.Join(filepath.Dir(ex), "fsd.log")
	}

	// Open file for appending, create if not exists
	logFile, err := os.OpenFile(logPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		// Fallback to stderr if file cannot be opened
		log.Printf("Failed to open log file: %v\n", err)
	} else {
		defer logFile.Close()
	}

	var logWriter io.Writer = logFile
	if logFile == nil {
		logWriter = os.Stderr
	}

	logger := fsdlog.Setup(sysLogger, logWriter)

	// Inject logger into daemon
	dmn.Logger = logger

	// Initialize CLI and execute
	rootCmd := cli.NewRootCmd(s, logger, logPath, cfgPath)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
