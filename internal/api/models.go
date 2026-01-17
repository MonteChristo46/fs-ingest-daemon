package api

import (
	"time"
)

// IngestRequest represents the payload for initiating a file ingestion.
// It contains metadata about the file and the device context.
type IngestRequest struct {
	DeviceID       string            `json:"device_id"`       // Unique identifier for the edge device
	Filename       string            `json:"filename"`        // Name of the file being uploaded
	FileSizeBytes  int64             `json:"file_size_bytes"` // Size of the file in bytes
	SHA256Checksum string            `json:"sha256_checksum"` // SHA256 hash for integrity verification
	Context        []string          `json:"context"`         // Contextual tags (e.g., directory structure: ["cam1", "2023"])
	Metadata       map[string]string `json:"metadata"`        // Key-value pairs of extracted metadata
	Timestamp      time.Time         `json:"timestamp"`       // Time of capture/ingest
}

// IngestResponse represents the API response after a successful IngestRequest.
// It provides the URL to upload the actual file content.
type IngestResponse struct {
	HandshakeID string    `json:"handshake_id"` // Unique session ID for this upload transaction
	UploadURL   string    `json:"upload_url"`   // Presigned URL (e.g., S3) for putting the file
	ExpiresAt   time.Time `json:"expires_at"`   // Expiration time for the UploadURL
}

// IngestStatus defines the final status of the ingestion process.
type IngestStatus string

const (
	StatusSuccess IngestStatus = "SUCCESS"
	StatusFailed  IngestStatus = "FAILED"
)

// ConfirmRequest represents the payload to finalize the ingestion transaction.
// It tells the API whether the file upload to the UploadURL was successful.
type ConfirmRequest struct {
	HandshakeID  string       `json:"handshake_id"`            // The session ID received in IngestResponse
	Status       IngestStatus `json:"status"`                  // SUCCESS or FAILED
	ErrorMessage *string      `json:"error_message"`           // Error details if Status is FAILED, nullable
	UploadedPath *string      `json:"uploaded_path,omitempty"` // The resulting path/key in cloud storage, optional
}

// PairingRequest represents the payload to request a pairing code.
type PairingRequest struct {
	DeviceID string `json:"device_id"` // The device's unique hardware identifier
}

// PairingResponse represents the response containing the pairing code.
type PairingResponse struct {
	Code      string    `json:"code"`       // The short code for the user to enter
	ExpiresAt time.Time `json:"expires_at"` // When the code expires
}

// PairingStatus defines the status of the pairing process.
type PairingStatus string

const (
	PairingStatusWaiting PairingStatus = "WAITING"
	PairingStatusClaimed PairingStatus = "CLAIMED"
	PairingStatusExpired PairingStatus = "EXPIRED"
)

// PairingStatusResponse represents the response from the pairing status check.
type PairingStatusResponse struct {
	Status PairingStatus `json:"status"` // WAITING, CLAIMED, EXPIRED
	APIKey *string       `json:"apikey"` // The API Key if claimed
}
