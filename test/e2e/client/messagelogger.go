// Package client provides HTTP clients for SemStreams E2E tests
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/c360studio/semstreams/test/e2e/config"
)

// MessageLoggerClient provides HTTP client access to the MessageLogger service
type MessageLoggerClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewMessageLoggerClient creates a new client for MessageLogger HTTP endpoints
func NewMessageLoggerClient(baseURL string) *MessageLoggerClient {
	return &MessageLoggerClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: config.DefaultTestConfig.Timeout,
		},
	}
}

// MessageEntry represents a logged message from the MessageLogger service
type MessageEntry struct {
	Sequence    uint64          `json:"sequence"`
	Timestamp   time.Time       `json:"timestamp"`
	Subject     string          `json:"subject"`
	MessageType string          `json:"message_type,omitempty"`
	MessageID   string          `json:"message_id,omitempty"`
	TraceID     string          `json:"trace_id,omitempty"`
	SpanID      string          `json:"span_id,omitempty"`
	Summary     string          `json:"summary"`
	RawData     json.RawMessage `json:"raw_data,omitempty"`
	Metadata    map[string]any  `json:"metadata,omitempty"`
}

// TraceResponse represents the response from the trace query endpoint
type TraceResponse struct {
	TraceID string         `json:"trace_id"`
	Count   int            `json:"count"`
	Entries []MessageEntry `json:"entries"`
}

// LoggerStats represents statistics from the MessageLogger service
type LoggerStats struct {
	TotalMessages     int64     `json:"total_messages"`
	ValidMessages     int64     `json:"valid_messages"`
	InvalidMessages   int64     `json:"invalid_messages"`
	StartTime         time.Time `json:"start_time"`
	LastMessageTime   time.Time `json:"last_message_time"`
	UptimeSeconds     float64   `json:"uptime_seconds"`
	MonitoredSubjects []string  `json:"monitored_subjects"`
	MaxEntries        int       `json:"max_entries"`
}

// MessageTrace tracks a single message through the processing pipeline
type MessageTrace struct {
	MessageID string         // Original message ID
	Entries   []MessageEntry // All log entries related to this message
	Flow      []string       // Subject flow path: ["input.udp", "process.rule", "process.graph"]
	Duration  time.Duration  // Time from first to last entry
}

// KVQueryResult represents the result of a KV bucket query
type KVQueryResult struct {
	Bucket  string    `json:"bucket"`
	Pattern string    `json:"pattern"`
	Count   int       `json:"count"`
	Entries []KVEntry `json:"entries"`
}

// KVEntry represents a single KV bucket entry
type KVEntry struct {
	Key      string    `json:"key"`
	Value    any       `json:"value"`
	Revision uint64    `json:"revision"`
	Created  time.Time `json:"created"`
}

// GetEntries fetches logged messages with optional subject filter
func (c *MessageLoggerClient) GetEntries(ctx context.Context, limit int, subjectPattern string) ([]MessageEntry, error) {
	reqURL := fmt.Sprintf("%s/message-logger/entries?limit=%d", c.baseURL, limit)
	if subjectPattern != "" {
		reqURL += "&subject=" + url.QueryEscape(subjectPattern)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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

	var entries []MessageEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return entries, nil
}

// GetEntriesByTrace fetches all entries for a specific W3C trace ID
func (c *MessageLoggerClient) GetEntriesByTrace(ctx context.Context, traceID string) (*TraceResponse, error) {
	reqURL := fmt.Sprintf("%s/message-logger/trace/%s", c.baseURL, traceID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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

	var result TraceResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}

// WaitForTrace polls one trace until validate accepts the observed entries.
// The caller owns the deadline through ctx.
func (c *MessageLoggerClient) WaitForTrace(
	ctx context.Context,
	traceID string,
	validate func([]MessageEntry) error,
) ([]MessageEntry, error) {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var lastErr error
	for {
		trace, err := c.GetEntriesByTrace(ctx, traceID)
		if err == nil {
			if err = validate(trace.Entries); err == nil {
				return trace.Entries, nil
			}
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for trace %s: %w (last observation: %v)", traceID, ctx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}

// GetStats returns message logger statistics
func (c *MessageLoggerClient) GetStats(ctx context.Context) (*LoggerStats, error) {
	reqURL := fmt.Sprintf("%s/message-logger/stats", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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

	var stats LoggerStats
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &stats, nil
}

// GetSubjects returns list of monitored NATS subjects
func (c *MessageLoggerClient) GetSubjects(ctx context.Context) ([]string, error) {
	reqURL := fmt.Sprintf("%s/message-logger/subjects", c.baseURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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

	var subjects []string
	if err := json.NewDecoder(resp.Body).Decode(&subjects); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return subjects, nil
}

// QueryKV queries a NATS KV bucket
func (c *MessageLoggerClient) QueryKV(ctx context.Context, bucket, pattern string, limit int) (*KVQueryResult, error) {
	reqURL := fmt.Sprintf("%s/message-logger/kv/%s?limit=%d", c.baseURL, url.PathEscape(bucket), limit)
	if pattern != "" {
		reqURL += "&pattern=" + url.QueryEscape(pattern)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
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

	var result KVQueryResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}

// WaitForMessages waits until N messages matching pattern are logged
func (c *MessageLoggerClient) WaitForMessages(ctx context.Context, pattern string, count int, timeout time.Duration) ([]MessageEntry, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	var lastCount int
	var lastErr error

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled while waiting for messages: %w", ctx.Err())
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, fmt.Errorf("timeout waiting for %d messages matching %q (last count: %d, last error: %v)",
					count, pattern, lastCount, lastErr)
			}

			entries, err := c.GetEntries(ctx, count*2, pattern) // Fetch extra in case of filtering
			if err != nil {
				lastErr = err
				continue
			}

			lastCount = len(entries)
			lastErr = nil

			if len(entries) >= count {
				// Return only requested count
				if len(entries) > count {
					entries = entries[:count]
				}
				return entries, nil
			}
		}
	}
}

// TraceMessage finds all log entries related to a specific message ID
func (c *MessageLoggerClient) TraceMessage(ctx context.Context, messageID string) (*MessageTrace, error) {
	// Fetch all recent entries
	entries, err := c.GetEntries(ctx, 10000, "")
	if err != nil {
		return nil, fmt.Errorf("fetching entries: %w", err)
	}

	trace := &MessageTrace{
		MessageID: messageID,
		Entries:   make([]MessageEntry, 0),
		Flow:      make([]string, 0),
	}

	// Filter entries by message ID
	for _, entry := range entries {
		if entry.MessageID == messageID {
			trace.Entries = append(trace.Entries, entry)
			// Track unique subjects in order
			if len(trace.Flow) == 0 || trace.Flow[len(trace.Flow)-1] != entry.Subject {
				trace.Flow = append(trace.Flow, entry.Subject)
			}
		}
	}

	// Calculate duration from first to last entry
	if len(trace.Entries) >= 2 {
		first := trace.Entries[len(trace.Entries)-1].Timestamp // Entries are reverse chronological
		last := trace.Entries[0].Timestamp
		trace.Duration = last.Sub(first)
	}

	return trace, nil
}

// CountMessagesBySubject returns count of messages matching subject pattern
func (c *MessageLoggerClient) CountMessagesBySubject(ctx context.Context, pattern string) (int, error) {
	entries, err := c.GetEntries(ctx, 10000, pattern)
	if err != nil {
		return 0, err
	}
	return len(entries), nil
}

// Health checks if the message logger endpoint is reachable
func (c *MessageLoggerClient) Health(ctx context.Context) error {
	_, err := c.GetStats(ctx)
	return err
}
