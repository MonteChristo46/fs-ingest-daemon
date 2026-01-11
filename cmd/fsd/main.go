package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"fs-ingest-daemon/internal/cli"
	"fs-ingest-daemon/internal/daemon"
	fsdlog "fs-ingest-daemon/internal/logger"

	"github.com/kardianos/service"
)

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

	// Create the daemon instance (implements service.Interface)
	dmn := &daemon.Daemon{}

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

	var logWriter io.Writer = logFile
	if logFile == nil {
		logWriter = os.Stderr
	}

	logger := fsdlog.Setup(sysLogger, logWriter)

	// Inject logger into daemon
	dmn.Logger = logger

	// Initialize CLI and execute
	rootCmd := cli.NewRootCmd(s, logger, logPath)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
