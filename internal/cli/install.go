package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"fs-ingest-daemon/internal/api"
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/device"

	"github.com/kardianos/service"
	"github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"
)

// Default paths based on OS and privileges
func getDefaultInstallDir() string {
	if runtime.GOOS == "windows" {
		if isAdmin() {
			return `C:\ProgramData\hunt`
		}
		// Use LocalAppData for non-admin users
		localAppData, err := os.UserConfigDir() // usually AppData/Roaming, but fine for now or we use Env
		if err != nil {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, "hunt")
		}
		// Ideally we want AppData/Local, but UserConfigDir is usually Roaming.
		// Let's check env var specifically for Local
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "hunt")
		}
		return filepath.Join(localAppData, "hunt")
	}

	// Linux / macOS
	if isAdmin() {
		return "/opt/hunt"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "hunt")
}

// Check if running as Admin/Root
func isAdmin() bool {
	if runtime.GOOS == "windows" {
		_, err := os.Open("\\\\.\\PHYSICALDRIVE0")
		return err == nil
	}
	currentUser, err := user.Current()
	if err != nil {
		return false
	}
	return currentUser.Uid == "0"
}

// Helper to prompt user
func prompt(label string, defaultValue string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [%s]: ", label, defaultValue)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultValue
	}
	return input
}

// CopyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	// Copy permissions
	info, err := os.Stat(src)
	if err == nil {
		err = os.Chmod(dst, info.Mode())
	}
	return err
}

func InstallCmd(s service.Service) *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Interactive installer for the service",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Press [Enter] to accept the default settings.")

			amAdmin := isAdmin()

			// 1. Admin Check
			if !amAdmin {
				fmt.Println("[WARN] Notice: Running without Administrator privileges.")
				fmt.Println("   The daemon will be installed for the current user and may need to be started manually.")
				fmt.Print("   Continue anyway? [y/N]: ")
				var response string
				fmt.Scanln(&response)
				if strings.ToLower(response) != "y" {
					fmt.Println("Aborted.")
					return
				}
			}

			// 2. Determine Install Location
			defaultDir := getDefaultInstallDir()
			targetDir := prompt("Install Directory", defaultDir)

			// Create Directory
			if err := os.MkdirAll(targetDir, 0755); err != nil {
				fmt.Printf("[ERROR] Error creating directory %s: %v\n", targetDir, err)
				return
			}

			// 3. Self-Copy Binary
			currentExe, err := os.Executable()
			if err != nil {
				fmt.Printf("[ERROR] Error finding current executable: %v\n", err)
				return
			}

			exeName := filepath.Base(currentExe)
			targetExe := filepath.Join(targetDir, exeName)

			// Only copy if we aren't already running from the target
			// Resolve symlinks to be sure
			realCurrent, _ := filepath.EvalSymlinks(currentExe)
			realTarget, _ := filepath.EvalSymlinks(targetExe)

			if realCurrent != realTarget {
				fmt.Printf("[STATUS] Copying binary to %s...\n", targetExe)
				// Remove existing if needed (for updates)
				os.Remove(targetExe)
				if err := copyFile(currentExe, targetExe); err != nil {
					fmt.Printf("[ERROR] Error copying binary: %v\n", err)
					return
				}
			} else {
				fmt.Println("[STATUS] Running from target location. Skipping self-copy.")
			}

			// 4. Generate Config
			targetConfigPath := filepath.Join(targetDir, "config.json")
			var cfg *config.Config

			var generateNewConfig bool
			if _, err := os.Stat(targetConfigPath); err == nil {
				fmt.Printf("[CONFIG] Found existing config at %s.\n", targetConfigPath)
				// Load existing config to check for AuthToken later
				var err error
				cfg, err = config.Load(targetConfigPath)
				if err != nil {
					fmt.Printf("[WARN] Warning: Could not load existing config: %v\n", err)
					fmt.Print("   Existing config is invalid. Overwrite with new configuration? [y/N]: ")
					var response string
					fmt.Scanln(&response)
					if strings.ToLower(response) == "y" {
						generateNewConfig = true
					}
				} else {
					fmt.Print("   Do you want to update your configuration? [y/N]: ")
					var response string
					fmt.Scanln(&response)
					if strings.ToLower(response) == "y" {
						fmt.Printf("Device ID: %s\n", cfg.DeviceID)
						fmt.Printf("API Endpoint: %s\n", cfg.Endpoint)

						fmt.Println("\n[CONFIG] Sidecar Strategy")
						fmt.Println("Choose how files are paired:")
						fmt.Println("  strict: Waits for a companion .json file (e.g. img.png + img.png.json). Safer for metadata.")
						fmt.Println("  none:   Uploads files immediately. Good for simple image streams.")

						strategyDefault := cfg.SidecarStrategy
						if strategyDefault == "" {
							strategyDefault = config.DefaultSidecarStrategy
						}
						userInputStrategy := prompt("Sidecar Strategy (strict/none)", strategyDefault)
						if userInputStrategy != "strict" && userInputStrategy != "none" {
							fmt.Printf("Invalid choice '%s', defaulting to '%s'\n", userInputStrategy, strategyDefault)
							userInputStrategy = strategyDefault
						}
						cfg.SidecarStrategy = userInputStrategy

						if err := config.Save(targetConfigPath, cfg); err != nil {
							fmt.Printf("[ERROR] Error saving config: %v\n", err)
							return
						}
						fmt.Println("[CONFIG] Configuration updated successfully.")
					}
				}
			} else {
				generateNewConfig = true
			}

			if generateNewConfig {
				fmt.Println("[CONFIG] Generating new configuration...")

				// Generate defaults
				deviceID, _ := device.GetMACAddress()
				if deviceID == "" {
					deviceID = "dev-001"
				}

				userInputID := deviceID
				fmt.Printf("Device ID: %s\n", deviceID)
				userInputEndpoint := config.DefaultEndpoint
				fmt.Printf("API Endpoint: %s\n", userInputEndpoint)

				fmt.Println("\n[CONFIG] Sidecar Strategy")
				fmt.Println("Choose how files are paired:")
				fmt.Println("  strict: Waits for a companion .json file (e.g. img.png + img.png.json). Safer for metadata.")
				fmt.Println("  none:   Uploads files immediately. Good for simple image streams.")
				userInputStrategy := prompt("Sidecar Strategy (strict/none)", config.DefaultSidecarStrategy)
				if userInputStrategy != "strict" && userInputStrategy != "none" {
					fmt.Printf("Invalid choice '%s', defaulting to '%s'\n", userInputStrategy, config.DefaultSidecarStrategy)
					userInputStrategy = config.DefaultSidecarStrategy
				}

				// Create Config Object with ABSOLUTE PATHS
				cfg = &config.Config{
					DeviceID:                userInputID,
					Endpoint:                userInputEndpoint,
					MaxDataSizeGB:           config.DefaultMaxDataSizeGB,
					WatchPath:               filepath.Join(targetDir, "data"),
					LogPath:                 filepath.Join(targetDir, "hunt.log"),
					DBPath:                  filepath.Join(targetDir, "hunt.db"),
					IngestCheckInterval:     config.DefaultIngestCheckInterval,
					IngestBatchSize:         config.DefaultIngestBatchSize,
					IngestWorkerCount:       config.DefaultIngestWorkerCount,
					PruneCheckInterval:      config.DefaultPruneCheckInterval,
					PruneBatchSize:          config.DefaultPruneBatchSize,
					APITimeout:              config.DefaultAPITimeout,
					DebounceDuration:        config.DefaultDebounceDuration,
					OrphanCheckInterval:     config.DefaultOrphanCheckInterval,
					MetadataUpdateInterval:  config.DefaultMetadataUpdateInterval,
					WebClientURL:            config.DefaultWebClientURL,
					SidecarStrategy:         userInputStrategy,
					LogMaxSizeMB:            config.DefaultLogMaxSizeMB,
					LogMaxBackups:           config.DefaultLogMaxBackups,
					LogMaxAgeDays:           config.DefaultLogMaxAgeDays,
					LogCompress:             config.DefaultLogCompress,
					AllowedExtensions:       config.DefaultAllowedExtensions,
					ImageCompressionEnabled: config.DefaultImageCompressionEnabled,
					ImageMaxDimensionPx:     config.DefaultImageMaxDimensionPx,
					ImageCompressionQuality: config.DefaultImageCompressionQuality,
				}

				// Create the Watch Directory now
				os.MkdirAll(cfg.WatchPath, 0755)

				// Save Config
				if err := config.Save(targetConfigPath, cfg); err != nil {
					fmt.Printf("[ERROR] Error saving config: %v\n", err)
					return
				}
				fmt.Println("[CONFIG] Configuration saved.")
			}

			// 4.5 Interactive Pairing (The "User Friendly" Magic)
			if cfg != nil && cfg.AuthToken == "" {
				fmt.Println("\n[STATUS] Device not paired. Initiating pairing sequence...")

				apiClient := api.NewClient(cfg.Endpoint, cfg.APITimeout)
				pairingResp, err := apiClient.RequestPairingCode(cfg.DeviceID)

				if err != nil {
					fmt.Printf("[WARN] Pairing request failed: %v\n", err)
					fmt.Println("   Continuing installation without pairing. You can pair later or edit config.json manually.")
				} else {
					claimURL := fmt.Sprintf("%s/claim/%s", strings.TrimSuffix(cfg.WebClientURL, "/"), pairingResp.Code)

					fmt.Println("\n==========================================")
					fmt.Printf(" 📱 SCAN TO CLAIM DEVICE\n")
					fmt.Printf(" Code: %s\n", pairingResp.Code)
					fmt.Printf(" URL:  %s\n", claimURL)
					fmt.Println("==========================================")

					qrterminal.GenerateHalfBlock(claimURL, qrterminal.L, os.Stdout)

					fmt.Println("\nWaiting for device to be claimed (Ctrl+C to skip)...")

					// Poll loop
					ticker := time.NewTicker(5 * time.Second)
					defer ticker.Stop()

					paired := false
				pollLoop:
					for {
						select {
						case <-ticker.C:
							statusResp, err := apiClient.CheckPairingStatus(cfg.DeviceID, pairingResp.Code)
							if err != nil {
								continue
							}

							if statusResp.Status == api.PairingStatusClaimed {
								fmt.Println("\n[SUCCESS] Device successfully claimed!")
								if statusResp.APIKey != nil {
									cfg.AuthToken = *statusResp.APIKey
								} else {
									cfg.AuthToken = "provisioned"
								}

								// Save updated config
								if err := config.Save(targetConfigPath, cfg); err != nil {
									fmt.Printf("[ERROR] Error saving paired config: %v\n", err)
								}
								paired = true
								break pollLoop
							} else if statusResp.Status == api.PairingStatusExpired {
								fmt.Println("\n[ERROR] Pairing code expired.")
								break pollLoop
							}
						}
					}

					if !paired {
						fmt.Println("   Proceeding with installation (unpaired).")
					}
				}
			}

			// 5. Register Service (POINTING TO THE NEW BINARY)
			// On Windows, if we are not admin, we SKIP this step.
			if runtime.GOOS == "windows" && !amAdmin {
				fmt.Println("\n[WARN] Skipping Service Registration (Not Admin).")
				fmt.Println("   Installation is complete, but the background service was NOT registered.")
				fmt.Println("   To run the daemon, open a terminal and run:")
				fmt.Printf("   %s run\n", targetExe)
				return
			}

			// IMPORTANT: The 's' variable passed in is bound to the CURRENT executable path.
			// We cannot easily change the path of an existing service object.
			// However, kardianos/service usually uses os.Executable() during Install().
			// Since we want to register the *target* binary, we might need to run the install command *from* the target binary.

			if realCurrent != realTarget {
				fmt.Println("[STATUS] Registering service via installed binary...")
				// Execute: /opt/hunt/hunt service-install
				// We need a hidden command or just call 'install' again but from the new location?
				// If we call 'install' again, it will prompt again. Not good.

				// Solution: We invoke the system service registration manually or use a hidden flag.
				// Easier: We simply run `<targetExe> service-install` (a new hidden command we will add).

				cmd := exec.Command(targetExe, "service-install")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					fmt.Printf("[ERROR] Failed to register service: %v\n", err)
					return
				}
			} else {
				// We are already in the right place, just install
				fmt.Println("[STATUS] Registering service...")
				if err := s.Install(); err != nil {
					if strings.Contains(err.Error(), "already exists") {
						fmt.Println("   Service definition already exists. Reinstalling...")
						_ = s.Uninstall() // Ignore uninstall error, just try to clear it
						if err := s.Install(); err != nil {
							fmt.Printf("[ERROR] Service reinstall failed: %v\n", err)
						} else {
							fmt.Println("[SUCCESS] Service re-registered.")
						}
					} else {
						fmt.Printf("[ERROR] Service install failed: %v\n", err)
					}
				}
			}

			// 6. Start Service
			fmt.Println("[STATUS] Starting service...")
			// We can try to start it via the current service object (if local) or shell
			// Best to use the service object if we are local, or shell if remote.
			// Ideally, `service-install` above handles install. We need `service-start`.

			// Let's keep it simple: Try to start using the 's' object.
			// Note: If we just registered a remote binary, 's' might still be pointing to local?
			// kardianos/service controls the service by NAME. So as long as the name matches, s.Start() works.
			if err := s.Start(); err != nil {
				fmt.Printf("[WARN] Service start failed (it might be running): %v\n", err)
			} else {
				fmt.Println("[SUCCESS] Service started successfully!")
			}

			fmt.Println("\nInstallation Complete!")
			fmt.Printf("Logs:   %s\n", filepath.Join(targetDir, "hunt.log"))
			fmt.Printf("Config: %s\n", targetConfigPath)
			fmt.Printf("Data:   %s  <-- PUT FILES HERE\n", cfg.WatchPath)
		},
	}
}

// Hidden command to actually perform the registration logic from the correct path
func ServiceInstallCmd(s service.Service) *cobra.Command {
	return &cobra.Command{
		Use:    "service-install",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			// This command runs INSIDE the target binary (e.g. /opt/hunt/hunt)
			// So s.Install() uses the correct path.
			if err := s.Install(); err != nil {
				if strings.Contains(err.Error(), "already exists") {
					fmt.Println("Service definition already exists. Reinstalling...")
					if err := s.Uninstall(); err != nil {
						fmt.Printf("Failed to uninstall existing service: %v\n", err)
						os.Exit(1)
					}
					// Retry install
					if err := s.Install(); err != nil {
						fmt.Printf("Failed to reinstall service: %v\n", err)
						os.Exit(1)
					}
					fmt.Println("Service reinstalled successfully.")
					return
				}
				fmt.Printf("[ERROR] Internal Install Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("[SUCCESS] Internal Service Registration Successful.")
		},
	}
}
