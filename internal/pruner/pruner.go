package pruner

// Package pruner implements the disk space management logic.
// It ensures the directory watched by the daemon does not exceed a configured size limit (MaxDataSizeGB).
// It deletes files that have been successfully UPLOADED, starting with the least recently modified (LRM).

import (
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/store"
	"log/slog"
	"os"
	"time"
)

// Pruner manages the file eviction process.
type Pruner struct {
	cfg    *config.Config // App configuration
	store  *store.Store   // Reference to the database to find candidates
	logger *slog.Logger   // Structured logger
	stop   chan struct{}  // Channel to signal shutdown
}

// NewPruner creates a new Pruner instance.
func NewPruner(cfg *config.Config, s *store.Store, logger *slog.Logger) *Pruner {
	return &Pruner{
		cfg:    cfg,
		store:  s,
		logger: logger,
		stop:   make(chan struct{}),
	}
}

// Start runs the pruning logic in a background goroutine, checking based on config interval.
func (p *Pruner) Start() {
	interval, err := time.ParseDuration(p.cfg.PruneCheckInterval)
	if err != nil {
		interval = 1 * time.Minute
		p.logger.Error("Invalid prune check interval, defaulting to 1m", "error", err)
	}

	ticker := time.NewTicker(interval)
	go func() {
		for {
			select {
			case <-ticker.C:
				p.Prune()
			case <-p.stop:
				ticker.Stop()
				return
			}
		}
	}()
}

// Stop signals the background goroutine to stop.
func (p *Pruner) Stop() {
	close(p.stop)
}

// Prune checks the total size of files and evicts old uploaded files if the limit is exceeded.
func (p *Pruner) Prune() {
	maxBytes := int64(p.cfg.MaxDataSizeGB * 1024 * 1024 * 1024)

	// Calculate Hysteresis Watermarks
	highMark := p.cfg.PruneHighWatermarkPercent
	if highMark <= 0 {
		highMark = 90
	}
	lowMark := p.cfg.PruneLowWatermarkPercent
	if lowMark <= 0 {
		lowMark = 75
	}

	highWatermarkBytes := int64(float64(maxBytes) * float64(highMark) / 100.0)
	lowWatermarkBytes := int64(float64(maxBytes) * float64(lowMark) / 100.0)

	// Get total tracked size from DB
	currentSize, err := p.store.GetTotalSize()
	if err != nil {
		p.logger.Error("Pruner: Error getting total size", "error", err)
		return
	}

	if currentSize <= highWatermarkBytes {
		return // usage is within limits
	}

	p.logger.Info("Pruner: High watermark exceeded",
		"current_size_bytes", currentSize,
		"max_bytes", maxBytes,
		"high_watermark_bytes", highWatermarkBytes,
		"target_low_watermark_bytes", lowWatermarkBytes,
		"status", "starting_eviction")

	// Eviction Loop
	for currentSize > lowWatermarkBytes {
		// Fetch candidates for deletion.
		// Only files with status='UPLOADED' are eligible.
		candidates, err := p.store.GetPruneCandidates(p.cfg.PruneBatchSize)
		if err != nil {
			p.logger.Error("Pruner: Error fetching candidates", "error", err)
			return
		}

		// Backpressure mechanism:
		// If the disk is full but we have no uploaded files to delete, we are in a critical state.
		// We cannot delete PENDING files as that would mean data loss.
		if len(candidates) == 0 {
			p.logger.Warn("Pruner: Disk usage high but no UPLOADED files to delete! Backpressure active.", "current_size", currentSize)
			return
		}

		deletedCount := 0
		// Evict candidates
		for _, f := range candidates {
			// Attempt to remove the file from filesystem
			err := os.Remove(f.Path)
			if err != nil && !os.IsNotExist(err) {
				p.logger.Error("Pruner: Failed to remove file", "path", f.Path, "error", err)
				continue
			}

			// Remove record from DB
			if err := p.store.RemoveFile(f.Path); err != nil {
				p.logger.Error("Pruner: Failed to remove DB record", "path", f.Path, "error", err)
			} else {
				p.logger.Info("Pruned file", "path", f.Path, "size", f.Size)
				currentSize -= f.Size // Decrement local tracker
				deletedCount++
			}

			if currentSize <= lowWatermarkBytes {
				break
			}
		}

		if deletedCount == 0 {
			// Avoid infinite loop if we have candidates but fail to delete them
			p.logger.Error("Pruner: Failed to delete any files in batch, aborting cycle")
			break
		}
	}

	p.logger.Info("Pruner: Eviction cycle complete", "final_size", currentSize)
}
