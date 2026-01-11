package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

// NewRootCmd creates the root command and all subcommands for the CLI.
func NewRootCmd(s service.Service, logger *slog.Logger, logPath string) *cobra.Command {
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
				if logger != nil {
					logger.Error("Run error", "error", err)
				} else {
					fmt.Printf("Run error: %v\n", err)
				}
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
	return rootCmd
}
