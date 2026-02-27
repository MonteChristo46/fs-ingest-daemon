package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"

	"fs-ingest-daemon/internal/cli"
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/daemon"
	fsdlog "fs-ingest-daemon/internal/logger"

	"github.com/kardianos/service"
)

func isRoot() bool {
	if runtime.GOOS == "windows" {
		_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
		return err == nil
	}
	u, err := user.Current()
	if err != nil {
		return false
	}
	return u.Uid == "0"
}

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
		if cfg == nil {
			// fallback to an empty config instead of crashing
			cfg = &config.Config{}
		}
	}

	svcConfig := &service.Config{
		Name:        "fs-ingest-daemon",
		DisplayName: "FS Ingest Daemon",
		Description: "Watches directories and uploads files to the cloud.",
		Arguments:   []string{"run"},
	}

	// If not root, force User Service mode
	if !isRoot() {
		svcConfig.Option = service.KeyValue{
			"UserService": true,
		}
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

	// Initialize LogRotator
	rotator := &fsdlog.LogRotator{
		Filename:   logPath,
		MaxSizeMB:  cfg.LogMaxSizeMB,
		MaxBackups: cfg.LogMaxBackups,
		MaxAgeDays: cfg.LogMaxAgeDays,
		Compress:   cfg.LogCompress,
	}

	// Use rotator as the writer
	var logWriter io.Writer = rotator

	logger := fsdlog.Setup(sysLogger, logWriter)

	// Inject logger into daemon
	dmn.Logger = logger

	// Initialize CLI and execute
	rootCmd := cli.NewRootCmd(s, logger, logPath, cfgPath)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// Close rotator on exit (though main usually just exits)
	_ = rotator.Close()
}
