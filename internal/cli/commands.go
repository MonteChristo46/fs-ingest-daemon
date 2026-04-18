package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"fs-ingest-daemon/assets"
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/util"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"
)

func RequireAdmin(cmd *cobra.Command, args []string) {
	if !util.IsAdmin() {
		fmt.Printf("Error: This command requires administrator privileges. Please run with 'sudo hunt %s'.\n", cmd.Use)
		os.Exit(1)
	}
}

// NewRootCmd creates the root command and all subcommands for the CLI.
func NewRootCmd(s service.Service, logger *slog.Logger, logPath string, cfgPath string) *cobra.Command {
	var rootCmd = &cobra.Command{
		Use:     "hunt",
		Short:   "FS Ingest Daemon CLI",
		Long:    assets.Banner() + "\nFS Ingest Daemon CLI",
		Version: assets.Version(),
	}

	// installCmd moved to install.go

	var uninstallCmd = &cobra.Command{
		Use:    "uninstall",
		Short:  "Uninstall the service",
		Hidden: true,
		PreRun: RequireAdmin,
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
				if strings.Contains(err.Error(), "no such file or directory") || strings.Contains(err.Error(), "is not installed") {
					fmt.Printf("[WARN] Service is not currently installed or already removed: %v\n", err)
				} else {
					fmt.Printf("[ERROR] Failed to uninstall service: %v\n", err)
					return
				}
			} else {
				fmt.Println("Service uninstalled.")
			}
		},
	}

	var startCmd = &cobra.Command{
		Use:   "start",
		Short: "Start the service",
		PreRun: RequireAdmin,
		Run: func(cmd *cobra.Command, args []string) {
			err := s.Start()
			if err != nil {
				if strings.Contains(err.Error(), "Load failed: 5: Input/output error") || strings.Contains(err.Error(), "already running") {
					fmt.Println("Service is already running.")
				} else {
					fmt.Printf("Failed to start: %s\n", err)
				}
				return
			}
			fmt.Println("Service started.")
		},
	}

	var stopCmd = &cobra.Command{
		Use:   "stop",
		Short: "Stop the service",
		PreRun: RequireAdmin,
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
		PreRun: RequireAdmin,
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
		Use:    "run",
		Short:  "Run the service in foreground",
		Hidden: true,
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
		PreRun: RequireAdmin,
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
		PreRun: RequireAdmin,
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
