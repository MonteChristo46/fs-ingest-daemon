package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"

	"fs-ingest-daemon/internal/config"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

// NewRootCmd creates the root command and all subcommands for the CLI.
func NewRootCmd(s service.Service, logger *slog.Logger, logPath string, cfgPath string) *cobra.Command {
	var rootCmd = &cobra.Command{
		Use:   "fsd",
		Short: "FS Ingest Daemon CLI",
	}

	// installCmd moved to install.go

	var uninstallCmd = &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall the service",
		Run: func(cmd *cobra.Command, args []string) {
			// Clear AuthToken on uninstall to force re-pairing
			cfg, err := config.Load(cfgPath)
			if err == nil {
				cfg.AuthToken = ""
				if err := config.Save(cfgPath, cfg); err != nil {
					fmt.Printf("Warning: Failed to clear auth_token: %v\n", err)
				} else {
					fmt.Println("Auth token cleared.")
				}
			}

			err = s.Uninstall()
			if err != nil {
				fmt.Printf("Failed to uninstall service: %s\n", err)
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

	var restartCmd = &cobra.Command{
		Use:   "restart",
		Short: "Restart the service",
		Run: func(cmd *cobra.Command, args []string) {
			err := s.Restart()
			if err != nil {
				fmt.Printf("Failed to restart: %s\n", err)
				return
			}
			fmt.Println("Service restarted.")
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

	// Add commands
	rootCmd.AddCommand(
		InstallCmd(s),
		ServiceInstallCmd(s), // Hidden command for self-registration
		uninstallCmd,
		startCmd,
		stopCmd,
		restartCmd,
		runCmd,
		statusCmd,
		logsCmd,
		SimulateCmd(logger),
	)
	return rootCmd
}
