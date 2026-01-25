package ingest

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
	"time"
)

// Uploader handles the details of uploading a single file.
type Uploader struct {
	cfg       *config.Config
	apiClient *api.Client
	store     *store.Store
	logger    *slog.Logger
}

// NewUploader creates a new Uploader.
func NewUploader(cfg *config.Config, s *store.Store, client *api.Client, logger *slog.Logger) *Uploader {
	return &Uploader{
		cfg:       cfg,
		store:     s,
		apiClient: client,
		logger:    logger,
	}
}

// Process handles the full lifecycle of a single file upload:
// 1. Calculate SHA256 checksum.
// 2. Extract metadata from path.
// 3. Request ingest URL from API.
// 4. Upload file content to the provided URL.
// 5. Confirm success with the API.
// 6. Mark file as UPLOADED in local store.
func (u *Uploader) Process(f store.FileRecord) {
	// 0. Check if this is a metadata file
	// If it is a .json file AND it has a partner path, we skip it.
	// The partner (the image) will handle the upload and mark this one as done.
	if filepath.Ext(f.Path) == ".json" {
		if f.PartnerPath.Valid && f.PartnerPath.String != "" {
			u.logger.Info("Skipping metadata file, waiting for partner", "path", f.Path, "partner", f.PartnerPath.String)
			return
		}
		// If it's an orphan json (no partner detected or partner lost), we process it.
	}

	// 0.5. Load DeviceContext from partner if available
	var deviceContext map[string]interface{}
	if f.PartnerPath.Valid && f.PartnerPath.String != "" {
		// Attempt to read the JSON file
		jsonFile, err := os.Open(f.PartnerPath.String)
		if err == nil {
			defer jsonFile.Close()
			if err := json.NewDecoder(jsonFile).Decode(&deviceContext); err != nil {
				u.logger.Warn("Failed to decode device context from partner", "partner", f.PartnerPath.String, "error", err)
			}
		} else {
			u.logger.Warn("Failed to open partner file for context", "partner", f.PartnerPath.String, "error", err)
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
		sum, err := u.calculateSHA256(f.Path)
		hashCh <- hashResult{sum, err}
	}()

	// 2. Extract Metadata and Context based on directory structure
	context, meta := util.ExtractMetadata(u.cfg.WatchPath, f.Path)

	// 3. Ingest Request - Ask API for permission and upload URL
	req := api.IngestRequest{
		DeviceID:        u.cfg.DeviceID,
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
			u.logger.Warn("Ingester: File vanished before processing, removing from DB", "path", f.Path)
			_ = u.store.RemoveFile(f.Path)
			return
		}
		u.logger.Error("Ingester: Failed to calculate checksum", "path", f.Path, "error", res.err)
		return
	}
	req.SHA256Checksum = res.sum

	resp, err := u.apiClient.Ingest(req)
	if err != nil {
		u.logger.Error("Ingester: Ingest request failed", "path", f.Path, "error", err)
		return
	}

	// 4. Upload to Presigned URL
	u.logger.Info("Starting upload", "path", f.Path, "size", f.Size, "upload_url", resp.UploadURL)

	uploadStart := time.Now()
	if err := u.uploadFile(resp.UploadURL, f.Path); err != nil {
		u.logger.Error("Ingester: Upload failed", "path", f.Path, "error", err)

		// Report failure to API so it can handle the failed handshake
		errMsg := err.Error()
		failReq := api.ConfirmRequest{
			HandshakeID:  resp.HandshakeID,
			Status:       api.StatusFailed,
			ErrorMessage: &errMsg,
		}
		_ = u.apiClient.Confirm(failReq)
		return
	}
	uploadDuration := time.Since(uploadStart)

	// 5. Confirm Success with API
	var uploadedPath *string
	pUrl, err := url.Parse(resp.UploadURL)
	if err == nil {
		p := pUrl.Path
		// We capture the path component of the upload URL to store/log if needed.
		uploadedPath = &p
	}

	confirmReq := api.ConfirmRequest{
		HandshakeID:  resp.HandshakeID,
		Status:       api.StatusSuccess,
		UploadedPath: uploadedPath,
	}

	if err := u.apiClient.Confirm(confirmReq); err != nil {
		u.logger.Error("Ingester: Confirm request failed", "path", f.Path, "handshake_id", resp.HandshakeID, "error", err)
		// Note: If confirm fails, we do NOT mark as uploaded locally.
		// This ensures the file is retried in the next batch.
		return
	}

	// 6. Mark as Uploaded in local DB
	if err := u.store.MarkUploaded(f.Path); err != nil {
		u.logger.Error("Ingester: Failed to mark as uploaded", "path", f.Path, "error", err)
	} else {
		u.logger.Info("Upload success", "path", f.Path, "duration", uploadDuration)
		// If we have a partner, mark it as uploaded too
		if f.PartnerPath.Valid && f.PartnerPath.String != "" {
			if err := u.store.MarkUploaded(f.PartnerPath.String); err != nil {
				u.logger.Error("Ingester: Failed to mark partner as uploaded", "partner", f.PartnerPath.String, "error", err)
			}
		}
	}
}

// uploadFile performs a PUT request to upload the file content to the destination URL.
func (u *Uploader) uploadFile(url, path string) error {
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

	resp, err := u.apiClient.HTTPClient.Do(req)
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
func (u *Uploader) calculateSHA256(path string) (string, error) {
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
