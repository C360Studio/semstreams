// Package client provides test utilities for SemStreams E2E tests
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
//
// ID is the AGNTCY OASF taxonomy class ID (uint32); name is the
// matching hierarchical path. Mirrors processor/oasf-generator.OASFSkill.
type OASFSkill struct {
	ID          uint32   `json:"id"`
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
// Mirrors the server-side input/a2a.Task wire shape.
type A2ATask struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionId,omitempty"`
	Status    A2ATaskStatus  `json:"status"`
	Message   A2AMessage     `json:"message,omitzero"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// A2ATaskStatus mirrors input/a2a.TaskStatus.
type A2ATaskStatus struct {
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// A2AMessage represents an A2A message.
type A2AMessage struct {
	Role  string           `json:"role"`
	Parts []A2AMessagePart `json:"parts"`
}

// A2AMessagePart mirrors input/a2a.MessagePart for the text variant.
type A2AMessagePart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
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

// SubmitTask submits a task to the A2A adapter via POST /tasks/send.
// The adapter authenticates via X-Agent-DID; we pass a fixed test DID so the
// stage works whether or not auth is enabled on the deployment.
func (c *A2AClient) SubmitTask(ctx context.Context, taskID, prompt string) (*A2ATask, error) {
	task := A2ATask{
		ID: taskID,
		Message: A2AMessage{
			Role:  "user",
			Parts: []A2AMessagePart{{Type: "text", Text: prompt}},
		},
	}
	body, err := json.Marshal(task)
	if err != nil {
		return nil, fmt.Errorf("marshal task: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tasks/send", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-DID", "did:semstreams:e2e-test")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// handleSendTask returns 202 Accepted on success.
	if resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("task submission failed: %s - %s", resp.Status, string(respBody))
	}

	var result A2ATask
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// SubmitTaskRaw submits an arbitrary request body to /tasks/send. Used for
// negative-path tests that need malformed payloads. Returns status code and body.
func (c *A2AClient) SubmitTaskRaw(ctx context.Context, rawBody []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tasks/send", bytes.NewReader(rawBody))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-DID", "did:semstreams:e2e-test")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, nil
}

// GetTask retrieves task status via GET /tasks/get?id=<id>.
func (c *A2AClient) GetTask(ctx context.Context, taskID string) (*A2ATask, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/tasks/get?id="+url.QueryEscape(taskID), nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get task failed: %s - %s", resp.Status, string(respBody))
	}

	var result A2ATask
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
}

// CancelTask cancels a task via POST /tasks/cancel.
func (c *A2AClient) CancelTask(ctx context.Context, taskID string) (*A2ATask, error) {
	body, err := json.Marshal(map[string]string{"id": taskID})
	if err != nil {
		return nil, fmt.Errorf("marshal cancel: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/tasks/cancel", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-DID", "did:semstreams:e2e-test")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cancel task failed: %s - %s", resp.Status, string(respBody))
	}

	var result A2ATask
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &result, nil
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

// DirectoryRegistration represents an agent registration in the mock
// directory. Field/JSON-tag shape matches the mock server's
// AgentRegistration, which itself mirrors the production wire
// (output/directory-bridge.RegistrationRequest). Pre-fix the JSON tags
// here diverged from the mock and from production both, silently
// dropping every field on unmarshal.
type DirectoryRegistration struct {
	AgentDID      string         `json:"agent_did"`
	OASFRecord    map[string]any `json:"oasf_record"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	RegisteredAt  string         `json:"registered_at"`
	LastHeartbeat string         `json:"last_heartbeat"`
	TTLSeconds    int            `json:"ttl_seconds"`
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

// WaitForRegistration polls the mock directory until an agent
// registration appears whose OASF record name or metadata
// semstreams_entity_id contains the given substring, or the timeout
// elapses. Returns nil if the timeout elapses without a match (the
// caller treats nil as "not found" — does not fail the e2e stage).
//
// Why search record name + metadata instead of the agent DID: the DID
// is generated by the local identity provider and won't contain a
// substring keyed on the source entity ID. The OASF record name
// (derived via oasfgenerator.extractAgentName from the entity ID) and
// the metadata.semstreams_entity_id field both encode the source
// entity, so either is a reliable correlation target.
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
			if registrationMatches(&reg, agentIDSubstring) {
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

// registrationMatches reports whether a directory registration encodes
// the given substring in any reliable correlation field (OASF record
// name, metadata.semstreams_entity_id). Both are best-effort — the
// helper is lenient about missing/wrong-type fields because mock JSON
// can legitimately omit them on incomplete records.
func registrationMatches(reg *DirectoryRegistration, substring string) bool {
	if reg == nil || substring == "" {
		return false
	}
	if reg.OASFRecord != nil {
		if name, ok := reg.OASFRecord["name"].(string); ok && strings.Contains(name, substring) {
			return true
		}
	}
	if reg.Metadata != nil {
		if eid, ok := reg.Metadata["semstreams_entity_id"].(string); ok && strings.Contains(eid, substring) {
			return true
		}
	}
	return false
}

// MockServerStats contains statistics from the AGNTCY mock server.
// Structural fields (Spans*, MetricsDataPoints*, *Names) are populated by the
// mock's OTLP-JSON parser and allow assertions beyond byte-count proof-of-life.
type MockServerStats struct {
	RequestCount    int64 `json:"request_count"`
	Registrations   int   `json:"registrations"`
	TracesReceived  int64 `json:"traces_received"`
	MetricsReceived int64 `json:"metrics_received"`

	// Structural trace aggregates parsed from OTLP JSON payloads.
	TracesSpansTotal       int64    `json:"traces_spans_total"`
	TracesStatusOK         int64    `json:"traces_status_ok"`
	TracesStatusError      int64    `json:"traces_status_error"`
	TracesParentChildLinks int      `json:"traces_parent_child_links"`
	TracesSpanNames        []string `json:"traces_span_names"`
	TracesLoopIDs          []string `json:"traces_loop_ids"`

	// Structural metric aggregates.
	MetricsDataPointsTotal int64    `json:"metrics_data_points_total"`
	MetricsNames           []string `json:"metrics_names"`
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
