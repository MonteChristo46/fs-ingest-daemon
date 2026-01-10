package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"fs-ingest-daemon/internal/api"
	"fs-ingest-daemon/internal/config"
	"fs-ingest-daemon/internal/store"
	"fs-ingest-daemon/internal/util"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type Ingester struct {
	cfg       *config.Config
	store     *store.Store
	apiClient *api.Client
	stop      chan struct{}
}

func NewIngester(cfg *config.Config, s *store.Store) *Ingester {
	return &Ingester{
		cfg:       cfg,
		store:     s,
		apiClient: api.NewClient(cfg.Endpoint),
		stop:      make(chan struct{}),
	}
}

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

func (i *Ingester) Stop() {
	close(i.stop)
}

func (i *Ingester) processBatch() {
	files, err := i.store.GetPendingFiles(10)
	if err != nil {
		log.Printf("Ingester: Error fetching pending files: %v", err)
		return
	}

	for _, f := range files {
		i.upload(f)
	}
}

func (i *Ingester) upload(f store.FileRecord) {
	// 0. Calculate SHA256
	checksum, err := calculateSHA256(f.Path)
	if err != nil {
		log.Printf("Ingester: Failed to calculate checksum for %s: %v", f.Path, err)
		return
	}

	// 1. Extract Metadata and Context
	context, meta := util.ExtractMetadata(i.cfg.WatchPath, f.Path)

	// 2. Ingest Request
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
		log.Printf("Ingester: Ingest request failed for %s: %v", f.Path, err)
		return
	}

	// 3. Upload to Presigned URL
	log.Printf("[UPLOAD] Start: %s (Size: %d) -> UploadURL: %s", f.Path, f.Size, resp.UploadURL)

	uploadStart := time.Now()
	if err := i.uploadFile(resp.UploadURL, f.Path); err != nil {
		log.Printf("Ingester: Upload failed for %s: %v", f.Path, err)

		// Report failure to API
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

	// 4. Confirm Success
	var uploadedPath *string
	u, err := url.Parse(resp.UploadURL)
	if err == nil {
		p := u.Path
		// Remove leading slash if needed, but usually S3 keys are relative to bucket, or path includes bucket.
		// Assuming standard S3 URL structure (host/bucket/key or bucket.host/key)
		// For now we just send the path.
		uploadedPath = &p
	}

	confirmReq := api.ConfirmRequest{
		HandshakeID:  resp.HandshakeID,
		Status:       api.StatusSuccess,
		UploadedPath: uploadedPath,
	}

	if err := i.apiClient.Confirm(confirmReq); err != nil {
		log.Printf("Ingester: Confirm request failed for %s (HandshakeID: %s): %v", f.Path, resp.HandshakeID, err)
		// Even if confirm fails, we might want to retry?
		// For now, if confirm fails, we don't mark as uploaded, so it will be retried (duplicate ingest request though).
		return
	}

	// 5. Mark as Uploaded
	if err := i.store.MarkUploaded(f.Path); err != nil {
		log.Printf("Ingester: Failed to mark %s as uploaded: %v", f.Path, err)
	} else {
		log.Printf("[UPLOAD] Success: %s (took %s)", f.Path, uploadDuration)
	}
}

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
