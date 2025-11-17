// Package client provides HTTP clients for StreamKit core component E2E tests
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/c360/semstreams/test/e2e/config"
)

// ObservabilityClient interacts with StreamKit component management endpoints
type ObservabilityClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewObservabilityClient creates a new client for StreamKit observability endpoints
func NewObservabilityClient(baseURL string) *ObservabilityClient {
	return &ObservabilityClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: config.DefaultTestConfig.Timeout,
		},
	}
}

// PlatformHealth represents overall platform health status
type PlatformHealth struct {
	Healthy bool   `json:"healthy"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// ComponentInfo represents a single component's information
// Matches StreamKit's /components/list API response format
type ComponentInfo struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Enabled   bool   `json:"enabled"`
	State     string `json:"state"`
	Healthy   bool   `json:"healthy"`
	LastError string `json:"last_error,omitempty"`
}

// GetPlatformHealth retrieves overall platform health
func (c *ObservabilityClient) GetPlatformHealth(ctx context.Context) (*PlatformHealth, error) {
	url := c.baseURL + config.ServicePaths.Health

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	// Health endpoint may return 503 when unhealthy but still have valid JSON
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusServiceUnavailable {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var health PlatformHealth
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &health, nil
}

// GetComponents retrieves information about all managed components
func (c *ObservabilityClient) GetComponents(ctx context.Context) ([]ComponentInfo, error) {
	url := c.baseURL + config.ComponentPaths.List

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var components []ComponentInfo
	if err := json.NewDecoder(resp.Body).Decode(&components); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return components, nil
}
