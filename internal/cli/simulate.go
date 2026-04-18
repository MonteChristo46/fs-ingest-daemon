package cli

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fs-ingest-daemon/assets"
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/simulation"

	"github.com/spf13/cobra"
)

func SimulateCmd(logger *slog.Logger, appCfg *config.Config) *cobra.Command {
	var (
		sourceDir  string
		targetDir  string
		rate       time.Duration
		defectRate float64
		jitter     float64
		nested     bool
		categories []string
	)

	cmd := &cobra.Command{
		Use:   "simulate",
		Short: "Run ingestion simulation",
		Long: `Simulate ingestion by generating files in the target directory.
Modes:
1. Replay (default): Uses embedded simulation images and contexts.
2. Replay Custom: Copies real images from --source directory.`,
		Run: func(cmd *cobra.Command, args []string) {
			logger := logger.With("service", "simulator")

			var sourceFS fs.FS
			var err error
			if sourceDir != "" {
				sourceFS = os.DirFS(sourceDir)
			} else {
				// Use embedded simulation data
				sourceFS, err = fs.Sub(assets.SimulationData, "simulation-data")
				if err != nil {
					fmt.Printf("Error accessing embedded simulation data: %v\n", err)
					return
				}
			}

			cfg := simulation.Config{
				SourceDir:  sourceDir,
				SourceFS:   sourceFS,
				TargetDir:  targetDir,
				Rate:       rate,
				DefectRate: defectRate,
				Jitter:     jitter,
				Nested:     nested,
				Categories: categories,
				Logger:     logger,
			}

			sim, err := simulation.New(cfg)
			if err != nil {
				fmt.Printf("Error initializing simulation: %v\n", err)
				return
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle interrupt signal
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigChan
				fmt.Println("\nStopping simulation...")
				cancel()
			}()

			if err := sim.Run(ctx); err != nil {
				fmt.Printf("Simulation error: %v\n", err)
			}
		},
	}

	cmd.Flags().StringVar(&sourceDir, "source", "", "Path to source images (optional, overrides default embedded images)")
	cmd.Flags().StringVar(&targetDir, "target", appCfg.WatchPath, "Target directory to drop files")
	cmd.Flags().DurationVar(&rate, "rate", 1*time.Second, "Interval between file generation (e.g., 500ms, 1s)")
	cmd.Flags().Float64Var(&defectRate, "defect-rate", 0.1, "Probability of generating a defect image (0.0 to 1.0)")
	cmd.Flags().Float64Var(&jitter, "jitter", 0.2, "Variance in generation rate (e.g., 0.2 for +/- 20%)")
	cmd.Flags().BoolVar(&nested, "nested", false, "Generate deep directory structure (factory/line/category/)")
	cmd.Flags().StringSliceVar(&categories, "categories", nil, "Comma-separated list of categories to simulate (e.g. bottle,cable). If empty, uses all found in source or default synthetic ones.")

	return cmd
}
