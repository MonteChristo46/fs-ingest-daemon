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
			return `C:\ProgramData\fsd`
		}
		// Use LocalAppData for non-admin users
		localAppData, err := os.UserConfigDir() // usually AppData/Roaming, but fine for now or we use Env
		if err != nil {
			home, _ := os.UserHomeDir()
			return filepath.Join(home, "fsd")
		}
		// Ideally we want AppData/Local, but UserConfigDir is usually Roaming. 
		// Let's check env var specifically for Local
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "fsd")
		}
		return filepath.Join(localAppData, "fsd")
	}
	
	// Linux / macOS
	if isAdmin() {
		return "/opt/fsd"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "fsd")
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
			fmt.Println("=== FS Ingest Daemon Installer ===")
			fmt.Println("Tip: Press [Enter] to accept the default value shown in brackets [].")

			amAdmin := isAdmin()

			// 1. Admin Check
			if !amAdmin {
				fmt.Println("⚠️  Warning: You are not running as Administrator/Root.")
				fmt.Println("   Installing a system service typically requires elevated privileges.")
				if runtime.GOOS == "windows" {
					fmt.Println("   On Windows, service registration will be SKIPPED if you continue.")
					fmt.Println("   The application will be installed, but you must run it manually via 'fsd run'.")
				} else {
					fmt.Println("   If this fails, please run with 'sudo'.")
				}
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
				fmt.Printf("❌ Error creating directory %s: %v\n", targetDir, err)
				return
			}

			// 3. Self-Copy Binary
			currentExe, err := os.Executable()
			if err != nil {
				fmt.Printf("❌ Error finding current executable: %v\n", err)
				return
			}

			exeName := filepath.Base(currentExe)
			targetExe := filepath.Join(targetDir, exeName)

			// Only copy if we aren't already running from the target
			// Resolve symlinks to be sure
			realCurrent, _ := filepath.EvalSymlinks(currentExe)
			realTarget, _ := filepath.EvalSymlinks(targetExe)

			if realCurrent != realTarget {
				fmt.Printf("-> Copying binary to %s...\n", targetExe)
				// Remove existing if needed (for updates)
				os.Remove(targetExe)
				if err := copyFile(currentExe, targetExe); err != nil {
					fmt.Printf("❌ Error copying binary: %v\n", err)
					return
				}
			} else {
				fmt.Println("-> Binary is already in target location. Skipping copy.")
			}

			// 4. Generate Config
			targetConfigPath := filepath.Join(targetDir, "config.json")
			var cfg *config.Config

			if _, err := os.Stat(targetConfigPath); err == nil {
				fmt.Printf("-> Found existing config at %s. Skipping configuration.\n", targetConfigPath)
				// Load existing config to check for AuthToken later
				var err error
				cfg, err = config.Load(targetConfigPath)
				if err != nil {
					fmt.Printf("⚠️  Warning: Could not load existing config: %v\n", err)
				}
			} else {
				fmt.Println("-> Generating new configuration...")

				// Generate defaults
				deviceID, _ := device.GetMACAddress()
				if deviceID == "" {
					deviceID = "dev-001"
				}

				userInputID := prompt("Device ID", deviceID)
				userInputEndpoint := prompt("API Endpoint", config.DefaultEndpoint)

				fmt.Println("\n--- Sidecar Strategy ---")
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
					DeviceID:               userInputID,
					Endpoint:               userInputEndpoint,
					MaxDataSizeGB:          config.DefaultMaxDataSizeGB,
					WatchPath:              filepath.Join(targetDir, "data"),
					LogPath:                filepath.Join(targetDir, "fsd.log"),
					DBPath:                 filepath.Join(targetDir, "fsd.db"),
					IngestCheckInterval:    config.DefaultIngestCheckInterval,
					IngestBatchSize:        config.DefaultIngestBatchSize,
					IngestWorkerCount:      config.DefaultIngestWorkerCount,
					PruneCheckInterval:     config.DefaultPruneCheckInterval,
					PruneBatchSize:         config.DefaultPruneBatchSize,
					APITimeout:             config.DefaultAPITimeout,
					DebounceDuration:       config.DefaultDebounceDuration,
					OrphanCheckInterval:    config.DefaultOrphanCheckInterval,
					MetadataUpdateInterval: config.DefaultMetadataUpdateInterval,
					WebClientURL:           config.DefaultWebClientURL,
					SidecarStrategy:        userInputStrategy,
				}

				// Create the Watch Directory now
				os.MkdirAll(cfg.WatchPath, 0755)

				// Save Config
				if err := config.Save(targetConfigPath, cfg); err != nil {
					fmt.Printf("❌ Error saving config: %v\n", err)
					return
				}
				fmt.Println("-> Configuration saved.")
			}

			// 4.5 Interactive Pairing (The "User Friendly" Magic)
			if cfg != nil && cfg.AuthToken == "" {
				fmt.Println("\n-> Device not paired. Initiating pairing sequence...")

				apiClient := api.NewClient(cfg.Endpoint, cfg.APITimeout)
				pairingResp, err := apiClient.RequestPairingCode(cfg.DeviceID)

				if err != nil {
					fmt.Printf("⚠️  Pairing request failed: %v\n", err)
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
								fmt.Println("\n✅ Device successfully claimed!")
							if statusResp.APIKey != nil {
								cfg.AuthToken = *statusResp.APIKey
							} else {
								cfg.AuthToken = "provisioned"
							}

							// Save updated config
								if err := config.Save(targetConfigPath, cfg); err != nil {
									fmt.Printf("❌ Error saving paired config: %v\n", err)
								}
								paired = true
								break pollLoop
							} else if statusResp.Status == api.PairingStatusExpired {
								fmt.Println("\n❌ Pairing code expired.")
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
				fmt.Println("\n-> Skipping Service Registration (Not Admin).")
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
				fmt.Println("-> Registering service via installed binary...")
				// Execute: /opt/fsd/fsd service-install
				// We need a hidden command or just call 'install' again but from the new location?
				// If we call 'install' again, it will prompt again. Not good.

				// Solution: We invoke the system service registration manually or use a hidden flag.
				// Easier: We simply run `<targetExe> service-install` (a new hidden command we will add).

				cmd := exec.Command(targetExe, "service-install")
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if err := cmd.Run(); err != nil {
					fmt.Printf("❌ Failed to register service: %v\n", err)
					return
				}
			} else {
				// We are already in the right place, just install
				fmt.Println("-> Registering service...")
				if err := s.Install(); err != nil {
					if strings.Contains(err.Error(), "already exists") {
						fmt.Println("   Service definition already exists. Reinstalling...")
						_ = s.Uninstall() // Ignore uninstall error, just try to clear it
						if err := s.Install(); err != nil {
							fmt.Printf("❌ Service reinstall failed: %v\n", err)
						} else {
							fmt.Println("✅ Service re-registered.")
						}
					} else {
						fmt.Printf("❌ Service install failed: %v\n", err)
					}
				}
			}

			// 6. Start Service
			fmt.Println("-> Starting service...")
			// We can try to start it via the current service object (if local) or shell
			// Best to use the service object if we are local, or shell if remote.
			// Ideally, `service-install` above handles install. We need `service-start`.

			// Let's keep it simple: Try to start using the 's' object.
			// Note: If we just registered a remote binary, 's' might still be pointing to local?
			// kardianos/service controls the service by NAME. So as long as the name matches, s.Start() works.
			if err := s.Start(); err != nil {
				fmt.Printf("⚠️  Service start failed (it might be running): %v\n", err)
			} else {
				fmt.Println("✅ Service started successfully!")
			}

			fmt.Println("\nInstallation Complete!")
			fmt.Printf("Logs:   %s\n", filepath.Join(targetDir, "fsd.log"))
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
			// This command runs INSIDE the target binary (e.g. /opt/fsd/fsd)
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
				fmt.Printf("Internal Install Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Println("Internal Service Registration Successful.")
		},
	}
}