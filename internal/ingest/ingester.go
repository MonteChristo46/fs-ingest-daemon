package ingest

// Package ingest coordinates the core logic of the daemon.
// It polls the local store for pending files, coordinates with the API to get upload credentials,
// performs the file upload, and updates the file status upon completion.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"fs-ingest-daemon/internal/api"
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/store"
	"fs-ingest-daemon/internal/util"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Ingester manages the file ingestion pipeline.
type Ingester struct {
	cfg       *config.Config // App configuration
	store     *store.Store   // Local metadata database
	apiClient *api.Client    // Client for cloud API interaction
	logger    *slog.Logger   // Structured logger
	stop      chan struct{}  // Channel to signal shutdown
	jobs      chan store.FileRecord
	pending   map[string]struct{}
	pendingMu sync.Mutex
	wg        sync.WaitGroup
}

// NewIngester creates a new Ingester instance.
func NewIngester(cfg *config.Config, s *store.Store, logger *slog.Logger) *Ingester {
	return &Ingester{
		cfg:       cfg,
		store:     s,
		apiClient: api.NewClient(cfg.Endpoint, cfg.APITimeout),
		logger:    logger,
		stop:      make(chan struct{}),
		jobs:      make(chan store.FileRecord, cfg.IngestBatchSize),
		pending:   make(map[string]struct{}),
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
		i.upload(f)

		i.pendingMu.Lock()
		delete(i.pending, f.Path)
		i.pendingMu.Unlock()
	}
}

// upload handles the full lifecycle of a single file upload:
// 1. Calculate SHA256 checksum.
// 2. Extract metadata from path.
// 3. Request ingest URL from API.
// 4. Upload file content to the provided URL.
// 5. Confirm success with the API.
// 6. Mark file as UPLOADED in local store.
func (i *Ingester) upload(f store.FileRecord) {
	// 0. Check if this is a metadata file
	// If it is a .json file AND it has a partner path, we skip it.
	// The partner (the image) will handle the upload and mark this one as done.
	if filepath.Ext(f.Path) == ".json" {
		if f.PartnerPath.Valid && f.PartnerPath.String != "" {
			i.logger.Info("Skipping metadata file, waiting for partner", "path", f.Path, "partner", f.PartnerPath.String)
			return
		}
		// If it's an orphan json (no partner detected or partner lost), we process it?
		// For now, let's proceed, effectively uploading the JSON as a file itself,
		// or we could decide to fail it.
		// Proceeding might be useful for debugging.
	}

	// 0.5. Load DeviceContext from partner if available
	var deviceContext map[string]interface{}
	if f.PartnerPath.Valid && f.PartnerPath.String != "" {
		// Attempt to read the JSON file
		jsonFile, err := os.Open(f.PartnerPath.String)
		if err == nil {
			defer jsonFile.Close()
			if err := json.NewDecoder(jsonFile).Decode(&deviceContext); err != nil {
				i.logger.Warn("Failed to decode device context from partner", "partner", f.PartnerPath.String, "error", err)
			}
		} else {
			i.logger.Warn("Failed to open partner file for context", "partner", f.PartnerPath.String, "error", err)
		}
	}

	if deviceContext == nil {
		deviceContext = make(map[string]interface{})
	}

	// 1. Calculate SHA256 for integrity check
	// Run in a goroutine to allow metadata extraction and request prep to overlap
	type hashResult struct {
		sum string
		err error
	}
	hashCh := make(chan hashResult, 1)
	go func() {
		sum, err := calculateSHA256(f.Path)
		hashCh <- hashResult{sum, err}
	}()

	// 2. Extract Metadata and Context based on directory structure
	context, meta := util.ExtractMetadata(i.cfg.WatchPath, f.Path)

	// 3. Ingest Request - Ask API for permission and upload URL
	req := api.IngestRequest{
		DeviceID:        i.cfg.DeviceID,
		Filename:        filepath.Base(f.Path),
		FileSizeBytes:   f.Size,
		FilePathContext: context,
		DeviceContext:   deviceContext,
		Metadata:        meta,
		Timestamp:       time.Now(),
	}

	// Wait for checksum
	res := <-hashCh
	if res.err != nil {
		if os.IsNotExist(res.err) {
			i.logger.Warn("Ingester: File vanished before processing, removing from DB", "path", f.Path)
			_ = i.store.RemoveFile(f.Path)
			return
		}
		i.logger.Error("Ingester: Failed to calculate checksum", "path", f.Path, "error", res.err)
		return
	}
	req.SHA256Checksum = res.sum

	resp, err := i.apiClient.Ingest(req)
	if err != nil {
		i.logger.Error("Ingester: Ingest request failed", "path", f.Path, "error", err)
		return
	}

	// 4. Upload to Presigned URL
	i.logger.Info("Starting upload", "path", f.Path, "size", f.Size, "upload_url", resp.UploadURL)

	uploadStart := time.Now()
	if err := i.uploadFile(resp.UploadURL, f.Path); err != nil {
		i.logger.Error("Ingester: Upload failed", "path", f.Path, "error", err)

		// Report failure to API so it can handle the failed handshake
		errMsg := err.Error()
		failReq := api.ConfirmRequest{
			HandshakeID:  resp.HandshakeID,
			Status:       api.StatusFailed,
			ErrorMessage: &errMsg,
		}
		_ = i.apiClient.Confirm(failReq)
		return
	}
	uploadDuration := time.Since(uploadStart)

	// 5. Confirm Success with API
	var uploadedPath *string
	u, err := url.Parse(resp.UploadURL)
	if err == nil {
		p := u.Path
		// We capture the path component of the upload URL to store/log if needed.
		uploadedPath = &p
	}

	confirmReq := api.ConfirmRequest{
		HandshakeID:  resp.HandshakeID,
		Status:       api.StatusSuccess,
		UploadedPath: uploadedPath,
	}

	if err := i.apiClient.Confirm(confirmReq); err != nil {
		i.logger.Error("Ingester: Confirm request failed", "path", f.Path, "handshake_id", resp.HandshakeID, "error", err)
		// Note: If confirm fails, we do NOT mark as uploaded locally.
		// This ensures the file is retried in the next batch.
		return
	}

	// 6. Mark as Uploaded in local DB
	if err := i.store.MarkUploaded(f.Path); err != nil {
		i.logger.Error("Ingester: Failed to mark as uploaded", "path", f.Path, "error", err)
	} else {
		i.logger.Info("Upload success", "path", f.Path, "duration", uploadDuration)
		// If we have a partner, mark it as uploaded too
		if f.PartnerPath.Valid && f.PartnerPath.String != "" {
			if err := i.store.MarkUploaded(f.PartnerPath.String); err != nil {
				i.logger.Error("Ingester: Failed to mark partner as uploaded", "partner", f.PartnerPath.String, "error", err)
			}
		}
	}
}

// uploadFile performs a PUT request to upload the file content to the destination URL.
func (i *Ingester) uploadFile(url, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	req, err := http.NewRequest("PUT", url, file)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := i.apiClient.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server responded with status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// calculateSHA256 computes the SHA256 hash of a file.
func calculateSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
