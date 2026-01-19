package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"fs-ingest-daemon/internal/api"
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/device"

	"github.com/kardianos/service"
	"github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"
)

// NewRootCmd creates the root command and all subcommands for the CLI.
func NewRootCmd(s service.Service, logger *slog.Logger, logPath string, cfgPath string) *cobra.Command {
	var rootCmd = &cobra.Command{
		Use:   "fsd",
		Short: "FS Ingest Daemon CLI",
	}

	var installCmd = &cobra.Command{
		Use:   "install",
		Short: "Install the service",
		Run: func(cmd *cobra.Command, args []string) {
			// 1. Load or Initialize Config
			// We use Load() which handles defaults if file is missing.
			cfg, err := config.Load(cfgPath)
			if err != nil {
				fmt.Printf("Error loading config: %v\n", err)
				// We proceed, assuming we might want to fail or just warn?
				// But we need to save the device ID, so we really need a valid config object.
				return
			}

			// 1.5 Ensure WatchPath exists
			if cfg.WatchPath != "" {
				if _, err := os.Stat(cfg.WatchPath); os.IsNotExist(err) {
					fmt.Printf("Watch directory does not exist. Creating: %s\n", cfg.WatchPath)
					if err := os.MkdirAll(cfg.WatchPath, 0755); err != nil {
						fmt.Printf("Error creating watch directory: %v\n", err)
						return
					}
					fmt.Println("Watch directory created successfully.")
				} else {
					fmt.Printf("Watch directory already exists: %s\n", cfg.WatchPath)
				}
			}

			// 2. Check DeviceID Fingerprint
			// "Immutability: If the field is already populated (not 'dev-001' or empty),
			// the daemon ignores the current hardware and uses the stored value."
			if cfg.DeviceID == "" || cfg.DeviceID == "dev-001" {
				fmt.Println("Generating new Device ID (Fingerprint) ...")
				mac, err := device.GetMACAddress()
				if err != nil {
					fmt.Printf("Warning: Could not determine MAC address: %v. Using default ID.\n", err)
				} else {
					cfg.DeviceID = mac
					fmt.Printf("Device ID set to: %s\n", cfg.DeviceID)

					// Save the config back to disk
					if err := config.Save(cfgPath, cfg); err != nil {
						fmt.Printf("Error saving config with new Device ID: %v\n", err)
						return
					}
					fmt.Println("Configuration updated with new Device ID.")
				}
			} else {
				fmt.Printf("Using existing Device ID: %s\n", cfg.DeviceID)
			}

			// 2.5 Device Registration / Pairing
			if cfg.AuthToken == "" {
				apiClient := api.NewClient(cfg.Endpoint, cfg.APITimeout)
				fmt.Println("Requesting pairing code...")
				pairingResp, err := apiClient.RequestPairingCode(cfg.DeviceID)
				if err != nil {
					fmt.Printf("Error requesting pairing code: %v\n", err)
					return
				}

				claimURL := fmt.Sprintf("%s/claim/%s", strings.TrimSuffix(cfg.WebClientURL, "/"), pairingResp.Code)

				fmt.Println("\n==========================================")
				fmt.Printf(" DEVICE PAIRING REQUIRED\n")
				fmt.Printf(" Code: %s\n", pairingResp.Code)
				fmt.Printf(" URL:  %s\n", claimURL)
				fmt.Println("==========================================")

				qrterminal.GenerateHalfBlock(claimURL, qrterminal.L, os.Stdout)

				fmt.Println("\nWaiting for device to be claimed...")

				ticker := time.NewTicker(5 * time.Second)
				defer ticker.Stop()

				// Poll for completion
				for range ticker.C {
					statusResp, err := apiClient.CheckPairingStatus(cfg.DeviceID, pairingResp.Code)
					if err != nil {
						// We log but keep retrying, network might be flaky
						// fmt.Printf("Polling error: %v\n", err)
						continue
					}

					if statusResp.Status == api.PairingStatusClaimed {
						fmt.Println("Device successfully claimed!")
						if statusResp.APIKey != nil {
							cfg.AuthToken = *statusResp.APIKey // Store the API Key
						} else {
							cfg.AuthToken = "provisioned" // Fallback if no key returned
						}

						if err := config.Save(cfgPath, cfg); err != nil {
							fmt.Printf("Error saving config after pairing: %v\n", err)
							return
						}
						break
					} else if statusResp.Status == api.PairingStatusExpired {
						fmt.Printf("Debug: Status received was %s\n", statusResp.Status)
						fmt.Println("Pairing code expired. Please restart installation.")
						return
					}
					// If WAITING, just continue loop
				}
			}

			// 3. Install the Service
			err = s.Install()
			if err != nil {
				if strings.Contains(err.Error(), "Init already exists") {
					fmt.Println("Service definition already exists. Attempting to reinstall...")
					// Attempt to uninstall the existing service
					if uninstallErr := s.Uninstall(); uninstallErr != nil {
						fmt.Printf("Failed to uninstall existing service: %v\n", uninstallErr)
						return
					}
					// Retry install
					if installErr := s.Install(); installErr != nil {
						fmt.Printf("Failed to reinstall service: %v\n", installErr)
						return
					}
					fmt.Println("Service reinstalled successfully.")
					return
				}
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
