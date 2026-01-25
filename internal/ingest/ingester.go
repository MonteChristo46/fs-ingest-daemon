package ingest

// Package ingest coordinates the core logic of the daemon.
// It polls the local store for pending files and delegates the upload process
// to the Uploader component.

import (
	"fs-ingest-daemon/internal/api"
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/store"
	"log/slog"
	"sync"
	"time"
)

// Ingester manages the file ingestion pipeline.
type Ingester struct {
	cfg       *config.Config // App configuration
	store     *store.Store   // Local metadata database
	uploader  *Uploader      // Worker that handles actual upload logic
	logger    *slog.Logger   // Structured logger
	stop      chan struct{}  // Channel to signal shutdown
	jobs      chan store.FileRecord
	pending   map[string]struct{}
	pendingMu sync.Mutex
	wg        sync.WaitGroup
}

// NewIngester creates a new Ingester instance.
func NewIngester(cfg *config.Config, s *store.Store, logger *slog.Logger) *Ingester {
	client := api.NewClient(cfg.Endpoint, cfg.APITimeout)
	uploader := NewUploader(cfg, s, client, logger)

	return &Ingester{
		cfg:      cfg,
		store:    s,
		uploader: uploader,
		logger:   logger,
		stop:     make(chan struct{}),
		jobs:     make(chan store.FileRecord, cfg.IngestBatchSize),
		pending:  make(map[string]struct{}),
	}
}

// Start initiates the background polling loop and workers.
func (i *Ingester) Start() {
	workerCount := i.cfg.IngestWorkerCount
	if workerCount <= 0 {
		workerCount = 1
	}

	for n := 0; n < workerCount; n++ {
		i.wg.Add(1)
		go func() {
			defer i.wg.Done()
			i.worker()
		}()
	}

	i.wg.Add(1)
	go func() {
		defer i.wg.Done()
		// Poll loop
		interval, err := time.ParseDuration(i.cfg.IngestCheckInterval)
		if err != nil {
			interval = 2 * time.Second
			i.logger.Error("Invalid ingest check interval, defaulting to 2s", "error", err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				i.processBatch()
			case <-i.stop:
				close(i.jobs)
				return
			}
		}
	}()
}

// Stop signals the polling loop to exit.
func (i *Ingester) Stop() {
	close(i.stop)
	i.wg.Wait()
}

// processBatch fetches a batch of PENDING files from the store and triggers their upload.
func (i *Ingester) processBatch() {
	// Fetch pending files based on batch size config
	files, err := i.store.GetPendingFiles(i.cfg.IngestBatchSize)
	if err != nil {
		i.logger.Error("Ingester: Error fetching pending files", "error", err)
		return
	}

	for _, f := range files {
		i.pendingMu.Lock()
		if _, exists := i.pending[f.Path]; exists {
			i.pendingMu.Unlock()
			continue
		}
		i.pending[f.Path] = struct{}{}
		i.pendingMu.Unlock()

		select {
		case i.jobs <- f:
			// successfully queued
		default:
			// Channel full, release pending lock and skip
			i.pendingMu.Lock()
			delete(i.pending, f.Path)
			i.pendingMu.Unlock()
			i.logger.Warn("Ingest job queue full, skipping file", "path", f.Path)
		}
	}
}

func (i *Ingester) worker() {
	for f := range i.jobs {
		i.uploader.Process(f)

		i.pendingMu.Lock()
		delete(i.pending, f.Path)
		i.pendingMu.Unlock()
	}
}
