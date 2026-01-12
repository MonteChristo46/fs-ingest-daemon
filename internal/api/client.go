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
