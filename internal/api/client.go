package api

// Package api provides a client for interacting with the Glitch Hunt Ingestion API.
// It handles authentication (via handshake), data ingestion requests, and confirmation of uploads.
// The structures defined here mirror the OpenAPI specification (openapi.json).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is the HTTP client wrapper for communicating with the Ingestion API.
type Client struct {
	BaseURL    string       // The root URL of the API
	HTTPClient *http.Client // underlying http.Client with timeouts configured
}

// NewClient creates a new API client with configured timeouts and connection pooling.
func NewClient(baseURL string, timeoutStr string) *Client {
	timeout, err := time.ParseDuration(timeoutStr)
	if err != nil {
		timeout = 30 * time.Second
	}

	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,              // Keep connections open for high throughput
				MaxIdleConnsPerHost: 100,              // Match max idle conns per host
				IdleConnTimeout:     90 * time.Second, // Close idle connections after 90s to purge memory
				TLSHandshakeTimeout: 10 * time.Second, // Don't hang forever if TLS fails
			},
		},
	}
}

// Ingest sends a request to initiate a file transfer.
// Returns the IngestResponse containing the upload URL, or an error.
func (c *Client) Ingest(req IngestRequest) (*IngestResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ingest request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/ingest/request", c.BaseURL)
	resp, err := c.HTTPClient.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to send ingest request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ingest request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var ingestResp IngestResponse
	if err := json.NewDecoder(resp.Body).Decode(&ingestResp); err != nil {
		return nil, fmt.Errorf("failed to decode ingest response: %w", err)
	}

	return &ingestResp, nil
}

// Confirm notifies the API about the outcome of the file upload (Success/Failure).
func (c *Client) Confirm(req ConfirmRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal confirm request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/ingest/confirm", c.BaseURL)
	resp, err := c.HTTPClient.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to send confirm request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("confirm request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// RequestPairingCode requests a new pairing code for the device.
func (c *Client) RequestPairingCode(deviceID string) (*PairingResponse, error) {
	req := PairingRequest{DeviceID: deviceID}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pairing request: %w", err)
	}

	url := fmt.Sprintf("%s/v1/pairing/request", c.BaseURL)
	resp, err := c.HTTPClient.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to send pairing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("pairing request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var pairingResp PairingResponse
	if err := json.NewDecoder(resp.Body).Decode(&pairingResp); err != nil {
		return nil, fmt.Errorf("failed to decode pairing response: %w", err)
	}

	return &pairingResp, nil
}

// CheckPairingStatus checks if the device has been claimed.
func (c *Client) CheckPairingStatus(deviceID string, code string) (*PairingStatusResponse, error) {
	url := fmt.Sprintf("%s/v1/pairing/status?device_id=%s&code=%s", c.BaseURL, deviceID, code)
	fmt.Printf("DEBUG: Checking status at %s\n", url)
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to check pairing status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// If 202 or 404 are returned as status codes for logic, handle them.
		// However, the spec says it returns JSON with status enum.
		// If the server returns non-200 for logical states (like 404 for expired), handle that:
		if resp.StatusCode == http.StatusNotFound {
			return &PairingStatusResponse{Status: PairingStatusExpired}, nil
		}
		if resp.StatusCode == http.StatusAccepted {
			return &PairingStatusResponse{Status: PairingStatusWaiting}, nil
		}

		respBody, _ := io.ReadAll(resp.Body)
		// Explicitly print the status code for debugging in the error
		return nil, fmt.Errorf("check pairing status failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var statusResp PairingStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusResp); err != nil {
		return nil, fmt.Errorf("failed to decode pairing status response: %w", err)
	}

	return &statusResp, nil
}
