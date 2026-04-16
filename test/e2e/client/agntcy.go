// Package client provides test utilities for SemStreams E2E tests
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// BucketOASFRecords is the KV bucket name for OASF records
const BucketOASFRecords = "OASF_RECORDS"

// OASFRecord represents an OASF (Open Agent Specification Framework) record
// for E2E testing validation.
type OASFRecord struct {
	Name          string         `json:"name"`
	Version       string         `json:"version"`
	SchemaVersion string         `json:"schema_version"`
	Authors       []string       `json:"authors"`
	CreatedAt     string         `json:"created_at"`
	Description   string         `json:"description"`
	Skills        []OASFSkill    `json:"skills"`
	Domains       []OASFDomain   `json:"domains,omitempty"`
	Extensions    map[string]any `json:"extensions,omitempty"`
}

// OASFSkill represents a skill in an OASF record.
type OASFSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Confidence  float64  `json:"confidence,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
}

// OASFDomain represents a domain in an OASF record.
type OASFDomain struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// GetOASFRecord retrieves an OASF record by entity ID from the OASF_RECORDS bucket.
func (c *NATSValidationClient) GetOASFRecord(ctx context.Context, entityID string) (*OASFRecord, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, BucketOASFRecords)
	if err != nil {
		if isBucketNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get OASF records bucket: %w", err)
	}

	entry, err := bucket.Get(ctx, entityID)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get OASF record: %w", err)
	}

	var record OASFRecord
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal OASF record: %w", err)
	}

	return &record, nil
}

// WaitForOASFRecord waits for an OASF record to appear for an entity.
func (c *NATSValidationClient) WaitForOASFRecord(
	ctx context.Context,
	entityID string,
	timeout time.Duration,
) (*OASFRecord, error) {
	const pollInterval = 200 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		record, err := c.GetOASFRecord(ctx, entityID)
		if err != nil {
			return nil, err
		}
		if record != nil {
			return record, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	return nil, nil
}

// CountOASFRecords counts the number of OASF records in the bucket.
func (c *NATSValidationClient) CountOASFRecords(ctx context.Context) (int, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, BucketOASFRecords)
	if err != nil {
		if isBucketNotFoundError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get OASF records bucket: %w", err)
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if isNoKeysError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to list OASF record keys: %w", err)
	}

	return len(keys), nil
}

// ListOASFRecordIDs returns all OASF record entity IDs.
func (c *NATSValidationClient) ListOASFRecordIDs(ctx context.Context) ([]string, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, BucketOASFRecords)
	if err != nil {
		if isBucketNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get OASF records bucket: %w", err)
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if isNoKeysError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list OASF record keys: %w", err)
	}

	return keys, nil
}

// A2AClient provides HTTP client for A2A adapter testing.
type A2AClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewA2AClient creates a new A2A test client.
func NewA2AClient(baseURL string) *A2AClient {
	return &A2AClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// A2ATask represents an A2A task for testing.
type A2ATask struct {
	ID     string `json:"id"`
	Status struct {
		State string `json:"state"`
	} `json:"status"`
	Messages []A2AMessage `json:"messages,omitempty"`
}

// A2AMessage represents an A2A message.
type A2AMessage struct {
	Role  string `json:"role"`
	Parts []struct {
		Text string `json:"text,omitempty"`
	} `json:"parts"`
}

// A2AAgentCard represents an A2A agent card response.
type A2AAgentCard struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	URL          string `json:"url"`
	Capabilities struct {
		Streaming         bool `json:"streaming"`
		PushNotifications bool `json:"pushNotifications"`
	} `json:"capabilities"`
	Skills []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	} `json:"skills,omitempty"`
}

// Health checks if the A2A adapter is healthy.
func (c *A2AClient) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: %s", resp.Status)
	}

	return nil
}

// GetAgentCard retrieves the agent card from the A2A adapter.
func (c *A2AClient) GetAgentCard(ctx context.Context) (*A2AAgentCard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/.well-known/agent.json", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("agent card request failed: %s - %s", resp.Status, string(body))
	}

	var card A2AAgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err != nil {
		return nil, fmt.Errorf("failed to decode agent card: %w", err)
	}

	return &card, nil
}

// SubmitTask submits a task to the A2A adapter.
func (c *A2AClient) SubmitTask(ctx context.Context, taskID, prompt string) (*A2ATask, error) {
	body := fmt.Sprintf(`{
		"jsonrpc": "2.0",
		"method": "tasks/send",
		"params": {
			"id": "%s",
			"message": {
				"role": "user",
				"parts": [{"text": "%s"}]
			}
		},
		"id": "1"
	}`, taskID, prompt)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/", strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("task submission failed: %s - %s", resp.Status, string(respBody))
	}

	var result struct {
		Result *A2ATask `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("A2A error %d: %s", result.Error.Code, result.Error.Message)
	}

	return result.Result, nil
}

// GetTask retrieves task status from the A2A adapter.
func (c *A2AClient) GetTask(ctx context.Context, taskID string) (*A2ATask, error) {
	body := fmt.Sprintf(`{
		"jsonrpc": "2.0",
		"method": "tasks/get",
		"params": {"id": "%s"},
		"id": "1"
	}`, taskID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/", strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get task failed: %s - %s", resp.Status, string(respBody))
	}

	var result struct {
		Result *A2ATask `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if result.Error != nil {
		return nil, fmt.Errorf("A2A error %d: %s", result.Error.Code, result.Error.Message)
	}

	return result.Result, nil
}

// AGNTCYMockClient provides HTTP client for testing the AGNTCY mock server.
type AGNTCYMockClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewAGNTCYMockClient creates a new client for the AGNTCY mock server.
func NewAGNTCYMockClient(baseURL string) *AGNTCYMockClient {
	return &AGNTCYMockClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// DirectoryRegistration represents an agent registration in the mock directory.
type DirectoryRegistration struct {
	AgentID       string         `json:"agent_id"`
	OASFRecord    map[string]any `json:"oasf_record"`
	RegisteredAt  string         `json:"registered_at"`
	LastHeartbeat string         `json:"last_heartbeat"`
	TTL           string         `json:"ttl"`
}

// Health checks if the AGNTCY mock server is healthy.
func (c *AGNTCYMockClient) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed: %s", resp.Status)
	}

	return nil
}

// ListRegistrations returns all agent registrations from the mock directory.
func (c *AGNTCYMockClient) ListRegistrations(ctx context.Context) ([]DirectoryRegistration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/agents", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list registrations failed: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Agents []DirectoryRegistration `json:"agents"`
		Count  int                     `json:"count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Agents, nil
}

// WaitForRegistration waits for an agent to be registered in the directory.
func (c *AGNTCYMockClient) WaitForRegistration(
	ctx context.Context,
	agentIDSubstring string,
	timeout time.Duration,
) (*DirectoryRegistration, error) {
	const pollInterval = 500 * time.Millisecond //nolint:goconst
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		registrations, err := c.ListRegistrations(ctx)
		if err != nil {
			return nil, err
		}

		for _, reg := range registrations {
			if strings.Contains(reg.AgentID, agentIDSubstring) {
				return &reg, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	return nil, nil
}

// MockServerStats contains statistics from the AGNTCY mock server.
type MockServerStats struct {
	RequestCount    int64 `json:"request_count"`
	Registrations   int   `json:"registrations"`
	TracesReceived  int64 `json:"traces_received"`
	MetricsReceived int64 `json:"metrics_received"`
}

// GetStats retrieves mock server statistics for e2e assertions.
func (c *AGNTCYMockClient) GetStats(ctx context.Context) (*MockServerStats, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/stats", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stats request failed: %s", resp.Status)
	}

	var stats MockServerStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("decode stats: %w", err)
	}

	return &stats, nil
}
