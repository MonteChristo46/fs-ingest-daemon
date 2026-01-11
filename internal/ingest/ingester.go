package ingest

// Package ingest coordinates the core logic of the daemon.
// It polls the local store for pending files, coordinates with the API to get upload credentials,
// performs the file upload, and updates the file status upon completion.

import (
	"crypto/sha256"
	"encoding/hex"
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
	"time"
)

// Ingester manages the file ingestion pipeline.
type Ingester struct {
	cfg       *config.Config // App configuration
	store     *store.Store   // Local metadata database
	apiClient *api.Client    // Client for cloud API interaction
	logger    *slog.Logger   // Structured logger
	stop      chan struct{}  // Channel to signal shutdown
}

// NewIngester creates a new Ingester instance.
func NewIngester(cfg *config.Config, s *store.Store, logger *slog.Logger) *Ingester {
	return &Ingester{
		cfg:       cfg,
		store:     s,
		apiClient: api.NewClient(cfg.Endpoint),
		logger:    logger,
		stop:      make(chan struct{}),
	}
}

// Start initiates the background polling loop.
// It checks for pending files every 2 seconds.
func (i *Ingester) Start() {
	go func() {
		// Poll loop
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				i.processBatch()
			case <-i.stop:
				return
			}
		}
	}()
}

// Stop signals the polling loop to exit.
func (i *Ingester) Stop() {
	close(i.stop)
}

// processBatch fetches a batch of PENDING files from the store and triggers their upload.
func (i *Ingester) processBatch() {
	// Fetch up to 10 pending files to avoid overwhelming the network/system
	files, err := i.store.GetPendingFiles(10)
	if err != nil {
		i.logger.Error("Ingester: Error fetching pending files", "error", err)
		return
	}

	for _, f := range files {
		i.upload(f)
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
	// 0. Calculate SHA256 for integrity check
	checksum, err := calculateSHA256(f.Path)
	if err != nil {
		i.logger.Error("Ingester: Failed to calculate checksum", "path", f.Path, "error", err)
		return
	}

	// 1. Extract Metadata and Context based on directory structure
	context, meta := util.ExtractMetadata(i.cfg.WatchPath, f.Path)

	// 2. Ingest Request - Ask API for permission and upload URL
	req := api.IngestRequest{
		DeviceID:       i.cfg.DeviceID,
		Filename:       filepath.Base(f.Path),
		FileSizeBytes:  f.Size,
		SHA256Checksum: checksum,
		Context:        context,
		Metadata:       meta,
		Timestamp:      time.Now(),
	}

	resp, err := i.apiClient.Ingest(req)
	if err != nil {
		i.logger.Error("Ingester: Ingest request failed", "path", f.Path, "error", err)
		return
	}

	// 3. Upload to Presigned URL
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

	// 4. Confirm Success with API
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

	// 5. Mark as Uploaded in local DB
	if err := i.store.MarkUploaded(f.Path); err != nil {
		i.logger.Error("Ingester: Failed to mark as uploaded", "path", f.Path, "error", err)
	} else {
		i.logger.Info("Upload success", "path", f.Path, "duration", uploadDuration)
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
