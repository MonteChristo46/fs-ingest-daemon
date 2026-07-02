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
	cfg            *config.Config
	store          *store.Store
	uploader       *Uploader
	logger         *slog.Logger
	stop           chan struct{}
	jobs           chan store.FileRecord
	wg             sync.WaitGroup
	statsMu        sync.Mutex
	ingestedCount  int
	totalLatency   time.Duration
	nextRetryTime  time.Time
	currentBackoff time.Duration
	backoffMu      sync.Mutex
}

// NewIngester creates a new Ingester instance.
func NewIngester(cfg *config.Config, s *store.Store, logger *slog.Logger) *Ingester {
	client := api.NewClient(cfg.Endpoint, cfg.APITimeout)
	uploader := NewUploader(cfg, s, client, logger)

	return &Ingester{
		cfg:            cfg,
		store:          s,
		uploader:       uploader,
		logger:         logger,
		stop:           make(chan struct{}),
		jobs:           make(chan store.FileRecord, cfg.IngestBatchSize),
		currentBackoff: 10 * time.Second,
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

	i.wg.Add(1)
	go func() {
		defer i.wg.Done()
		i.quotaProbe()
	}()

	i.wg.Add(1)
	go func() {
		defer i.wg.Done()
		// Heartbeat loop
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				i.statsMu.Lock()
				count := i.ingestedCount
				totalLatency := i.totalLatency
				i.ingestedCount = 0
				i.totalLatency = 0
				i.statsMu.Unlock()

				if count > 0 {
					avgLatencyMs := totalLatency.Milliseconds() / int64(count)
					i.logger.Info("Ingest Heartbeat", "files_ingested", count, "avg_latency_ms", avgLatencyMs)
				}
			case <-i.stop:
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

// processBatch atomically claims a batch of files from the store and dispatches them to workers.
func (i *Ingester) processBatch() {
	i.backoffMu.Lock()
	if !i.nextRetryTime.IsZero() && time.Now().Before(i.nextRetryTime) {
		i.backoffMu.Unlock()
		return
	}
	i.backoffMu.Unlock()

	files, err := i.store.ClaimPendingFiles(i.cfg.IngestBatchSize)
	if err != nil {
		i.logger.Error("Ingester: Error claiming files", "error", err)
		return
	}

	for _, f := range files {
		select {
		case i.jobs <- f:
		default:
			// Channel full, release this file back to PENDING so it can be claimed later
			_ = i.store.MarkForRetry(f.Path, "worker queue full")
			i.logger.Warn("Ingest job queue full, releasing file", "path", f.Path)
		}
	}
}

func (i *Ingester) worker() {
	for f := range i.jobs {
		result, latency := i.uploader.Process(f)

		if result == ResultSuccess {
			i.statsMu.Lock()
			i.ingestedCount++
			i.totalLatency += latency
			i.statsMu.Unlock()

			i.backoffMu.Lock()
			i.currentBackoff = 10 * time.Second
			i.nextRetryTime = time.Time{}
			i.backoffMu.Unlock()
		} else if result == ResultQuotaExceeded || result == ResultError {
			i.backoffMu.Lock()
			i.nextRetryTime = time.Now().Add(i.currentBackoff)

			reason := "error"
			if result == ResultQuotaExceeded {
				reason = "quota exceeded"
			}

			i.logger.Warn("Ingester: Backing off", "reason", reason, "backoff", i.currentBackoff)
			i.currentBackoff *= 2
			if i.currentBackoff > 5*time.Minute {
				i.currentBackoff = 5 * time.Minute
			}
			i.backoffMu.Unlock()
		}
	}
}

// quotaProbe periodically checks if quota has been restored during a backoff period.
// It runs independently of the backoff gate, claiming and processing a single file
// each interval. If the upload succeeds, the backoff is immediately reset so bulk
// processing can resume without waiting for the backoff timer to expire.
func (i *Ingester) quotaProbe() {
	interval, err := time.ParseDuration(i.cfg.QuotaCheckInterval)
	if err != nil {
		interval = 30 * time.Second
		i.logger.Error("Invalid quota check interval, defaulting to 30s", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !i.store.IsQuotaExceeded() {
				continue
			}

			i.backoffMu.Lock()
			backoffActive := !i.nextRetryTime.IsZero() && time.Now().Before(i.nextRetryTime)
			i.backoffMu.Unlock()

			if !backoffActive {
				continue
			}

			files, err := i.store.ClaimPendingFiles(1)
			if err != nil {
				i.logger.Error("Quota probe: Error claiming file", "error", err)
				continue
			}
			if len(files) == 0 {
				continue
			}

			result, _ := i.uploader.Process(files[0])

			if result == ResultSuccess {
				i.logger.Info("Quota probe: Upload successful, quota appears restored. Resetting backoff.")
				i.backoffMu.Lock()
				i.currentBackoff = 10 * time.Second
				i.nextRetryTime = time.Time{}
				i.backoffMu.Unlock()
			}

		case <-i.stop:
			return
		}
	}
}
