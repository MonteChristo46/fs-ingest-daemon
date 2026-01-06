package main

import (
	"fmt"
	"fs-ingest-daemon/internal/watcher"
	"log"
	"os"
	"path/filepath"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

var logger service.Logger

// program structures the service logic.
type program struct {
	watcher *watcher.Watcher
}

// TODO what that here?
func (p *program) Start(s service.Service) error {
	// Look for executable directory to find the data folder relative to it
	// TODO Do to do this must be configurable by the user?
	ex, err := os.Executable()
	if err != nil {
		return err
	}
	exPath := filepath.Dir(ex)
	watchPath := filepath.Join(exPath, "data")

	// Ensure data directory exists
	if err := os.MkdirAll(watchPath, 0755); err != nil {
		return err
	}

	if logger != nil {
		logger.Info("Starting FS Ingest Daemon...")
		logger.Info("Watching directory: " + watchPath)
	}

	// This function runs every time a new file is detected
	onNewFile := func(path string) {
		msg := fmt.Sprintf("New Image Detected: %s", path)
		fmt.Println(msg)
		if logger != nil {
			logger.Info(msg)
		}
		// TODO: In next step, we call uploader.Upload(path)
	}

	w, err := watcher.NewWatcher(watchPath, onNewFile)
	if err != nil {
		return err
	}
	p.watcher = w
	return nil
}

func (p *program) Stop(s service.Service) error {
	if logger != nil {
		logger.Info("Stopping FS Ingest Daemon...")
	}
	if p.watcher != nil {
		p.watcher.Close()
	}
	return nil
}

func main() {
	// Service Config
	svcConfig := &service.Config{
		Name:        "fs-ingest-daemon",
		DisplayName: "FS Ingest Daemon",
		Description: "Watches directories and uploads files to the cloud.",
		Arguments:   []string{"run"},
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	errs := make(chan error, 5)
	logger, err = s.Logger(errs)
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

	// Cobra Commands
	var rootCmd = &cobra.Command{
		Use:   "fsd",
		Short: "FS Ingest Daemon CLI",
		Long:  `A daemon to watch file system events and upload files.`,
	}

	var installCmd = &cobra.Command{
		Use:   "install",
		Short: "Install the service",
		Run: func(cmd *cobra.Command, args []string) {
			err := s.Install()
			if err != nil {
				fmt.Printf("Failed to install service: %s\n", err)
				return
			}
			fmt.Println("Service installed successfully.")
		},
	}

	var uninstallCmd = &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the service",
		Run: func(cmd *cobra.Command, args []string) {
			err := s.Uninstall()
			if err != nil {
				fmt.Printf("Failed to uninstall service: %s\n", err)
				return
			}
			fmt.Println("Service uninstalled successfully.")
		},
	}

	var runCmd = &cobra.Command{
		Use:   "run",
		Short: "Run the service (foreground)",
		Run: func(cmd *cobra.Command, args []string) {
			err := s.Run()
			if err != nil {
				if logger != nil {
					logger.Error(err)
				} else {
					fmt.Println(err)
				}
			}
		},
	}

	var startCmd = &cobra.Command{
		Use:   "start",
		Short: "Start the installed service",
		Run: func(cmd *cobra.Command, args []string) {
			err := s.Start()
			if err != nil {
				fmt.Printf("Failed to start service: %s\n", err)
				return
			}
			fmt.Println("Service started.")
		},
	}

	var stopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop the installed service",
		Run: func(cmd *cobra.Command, args []string) {
			err := s.Stop()
			if err != nil {
				fmt.Printf("Failed to stop service: %s\n", err)
				return
			}
			fmt.Println("Service stopped.")
		},
	}

	var statusCmd = &cobra.Command{
		Use:   "status",
		Short: "Get service status",
		Run: func(cmd *cobra.Command, args []string) {
			status, err := s.Status()
			if err != nil {
				fmt.Printf("Error getting status: %s\n", err)
				return
			}
			switch status {
			case service.StatusRunning:
				fmt.Println("Service is running.")
			case service.StatusStopped:
				fmt.Println("Service is stopped.")
			default:
				fmt.Printf("Service status: %d\n", status)
			}
		},
	}

	rootCmd.AddCommand(installCmd, uninstallCmd, runCmd, startCmd, stopCmd, statusCmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
