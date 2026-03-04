package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fs-ingest-daemon/internal/simulation"

	"github.com/spf13/cobra"
)

func SimulateCmd(logger *slog.Logger) *cobra.Command {
	var (
		sourceDir  string
		targetDir  string
		rate       time.Duration
		categories []string
	)

	cmd := &cobra.Command{
		Use:   "simulate",
		Short: "Run ingestion simulation",
		Long: `Simulate ingestion by generating files in the target directory.
Two modes are supported:
1. Synthetic (default): Generates dummy files with random content.
2. Replay: Copies real images from --source directory.`,
		Run: func(cmd *cobra.Command, args []string) {
			logger := logger.With("service", "simulator")
			cfg := simulation.Config{
				SourceDir:  sourceDir,
				TargetDir:  targetDir,
				Rate:       rate,
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

	cmd.Flags().StringVar(&sourceDir, "source", "", "Path to source images (optional, enables Replay mode)")
	cmd.Flags().StringVar(&targetDir, "target", "./data", "Target directory to drop files")
	cmd.Flags().DurationVar(&rate, "rate", 1*time.Second, "Interval between file generation (e.g., 500ms, 1s)")
	cmd.Flags().StringSliceVar(&categories, "categories", nil, "Comma-separated list of categories to simulate (e.g. bottle,cable). If empty, uses all found in source or default synthetic ones.")

	return cmd
}
