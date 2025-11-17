package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Compile-time check that MessageLogger implements HTTPHandler
var _ HTTPHandler = (*MessageLogger)(nil)

// RegisterHTTPHandlers registers HTTP endpoints for the MessageLogger service
func (ml *MessageLogger) RegisterHTTPHandlers(prefix string, mux *http.ServeMux) {
	// Ensure prefix ends with /
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}

	// Register handlers
	mux.HandleFunc(prefix+"entries", ml.handleGetEntries)
	mux.HandleFunc(prefix+"stats", ml.handleGetStats)
	mux.HandleFunc(prefix+"subjects", ml.handleGetSubjects)

	// KV query endpoints (only in development/test mode)
	mux.HandleFunc(prefix+"kv/", ml.handleKVQuery)

	ml.logger.Info("MessageLogger HTTP handlers registered", "prefix", prefix)
}

// OpenAPISpec returns the OpenAPI specification for MessageLogger endpoints
func (ml *MessageLogger) OpenAPISpec() *OpenAPISpec {
	spec := NewOpenAPISpec()

	// Add tags
	spec.Tags = []TagSpec{
		{
			Name:        "MessageLogger",
			Description: "Message observation and debugging endpoints",
		},
	}

	// Add /entries endpoint
	spec.Paths["/entries"] = PathSpec{
		GET: &OperationSpec{
			Summary:     "Get recent message entries",
			Description: "Returns the most recent logged messages from the circular buffer",
			Tags:        []string{"MessageLogger"},
			Parameters: []ParameterSpec{
				{
					Name:        "limit",
					In:          "query",
					Description: "Maximum number of entries to return (default: 100, max: 10000)",
					Required:    false,
					Schema:      Schema{Type: "integer"},
				},
				{
					Name:        "subject",
					In:          "query",
					Description: "Filter by NATS subject pattern",
					Required:    false,
					Schema:      Schema{Type: "string"},
				},
			},
			Responses: map[string]ResponseSpec{
				"200": {
					Description: "List of message entries",
					ContentType: "application/json",
				},
			},
		},
	}

	// Add /stats endpoint
	spec.Paths["/stats"] = PathSpec{
		GET: &OperationSpec{
			Summary:     "Get message statistics",
			Description: "Returns statistics about processed messages",
			Tags:        []string{"MessageLogger"},
			Responses: map[string]ResponseSpec{
				"200": {
					Description: "Message statistics",
					ContentType: "application/json",
				},
			},
		},
	}

	// Add /subjects endpoint
	spec.Paths["/subjects"] = PathSpec{
		GET: &OperationSpec{
			Summary:     "Get monitored subjects",
			Description: "Returns list of NATS subjects being monitored",
			Tags:        []string{"MessageLogger"},
			Responses: map[string]ResponseSpec{
				"200": {
					Description: "List of monitored subjects",
					ContentType: "application/json",
				},
			},
		},
	}

	// Add /kv/{bucket} endpoint
	spec.Paths["/kv/{bucket}"] = PathSpec{
		GET: &OperationSpec{
			Summary:     "Query KV bucket",
			Description: "Query NATS KV bucket entries (development/test only)",
			Tags:        []string{"MessageLogger"},
			Parameters: []ParameterSpec{
				{
					Name:        "bucket",
					In:          "path",
					Description: "KV bucket name",
					Required:    true,
					Schema:      Schema{Type: "string"},
				},
				{
					Name:        "pattern",
					In:          "query",
					Description: "Key pattern to match (e.g., 'entity.*')",
					Required:    false,
					Schema:      Schema{Type: "string"},
				},
				{
					Name:        "limit",
					In:          "query",
					Description: "Maximum number of entries to return (default: 100, max: 1000)",
					Required:    false,
					Schema:      Schema{Type: "integer"},
				},
			},
			Responses: map[string]ResponseSpec{
				"200": {
					Description: "KV bucket entries",
					ContentType: "application/json",
				},
				"403": {
					Description: "KV query disabled in production",
				},
				"404": {
					Description: "Bucket not found",
				},
			},
		},
	}

	return spec
}

// handleGetEntries returns recent message entries
func (ml *MessageLogger) handleGetEntries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	query := r.URL.Query()

	// Get limit parameter
	limit := 100
	if limitStr := query.Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
			if limit > 10000 {
				limit = 10000
			}
		}
	}

	// Get subject filter
	subjectFilter := query.Get("subject")

	// Get entries
	entries := ml.GetLogEntries(limit)

	// Apply subject filter if provided
	if subjectFilter != "" {
		filtered := make([]MessageLogEntry, 0, len(entries))
		for _, entry := range entries {
			if matchesPattern(entry.Subject, subjectFilter) {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(entries); err != nil {
		ml.logger.Error("Failed to encode entries", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleGetStats returns message statistics
func (ml *MessageLogger) handleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Calculate statistics
	stats := ml.GetStatistics()

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		ml.logger.Error("Failed to encode stats", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleGetSubjects returns list of monitored subjects
func (ml *MessageLogger) handleGetSubjects(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get current subjects
	subjects := ml.config.MonitorSubjects

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(subjects); err != nil {
		ml.logger.Error("Failed to encode subjects", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleKVQuery queries NATS KV buckets (development/test only)
func (ml *MessageLogger) handleKVQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Check if KV query is enabled (should be configurable)
	// For now, we'll allow it in all environments but log a warning
	ml.logger.Warn("KV query endpoint accessed - should be restricted to dev/test environments")

	// Extract bucket name from path
	path := strings.TrimPrefix(r.URL.Path, "/message-logger/kv/")
	path = strings.TrimSuffix(path, "/")

	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Bucket name required", http.StatusBadRequest)
		return
	}

	// Validate and decode bucket name
	bucket, err := url.QueryUnescape(parts[0])
	if err != nil {
		http.Error(w, "Invalid bucket name", http.StatusBadRequest)
		return
	}

	// Validate bucket name for security
	if bucket == "" || bucket == "." || bucket == ".." ||
		strings.Contains(bucket, "/") || strings.Contains(bucket, "\\") {
		http.Error(w, "Invalid bucket name", http.StatusBadRequest)
		return
	}

	// Get query parameters
	query := r.URL.Query()
	pattern := query.Get("pattern")
	if pattern == "" {
		pattern = "*"
	}

	limit := 100
	if limitStr := query.Get("limit"); limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
			if limit > 1000 {
				limit = 1000
			}
		}
	}

	// Query KV bucket
	result, err := ml.queryKVBucket(bucket, pattern, limit)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, fmt.Sprintf("Bucket not found: %s", bucket), http.StatusNotFound)
		} else {
			ml.logger.Error("Failed to query KV bucket", "bucket", bucket, "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(result); err != nil {
		ml.logger.Error("Failed to encode KV result", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// queryKVBucket queries a NATS KV bucket
func (ml *MessageLogger) queryKVBucket(bucket, pattern string, limit int) (map[string]any, error) {
	ctx := context.Background()

	// Create or get KV bucket using resilient pattern
	// For query endpoints, we create with minimal config if it doesn't exist
	kv, err := ml.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      bucket,
		Description: fmt.Sprintf("KV bucket %s (auto-created by query)", bucket),
		History:     5,                  // Minimal history for query buckets
		TTL:         7 * 24 * time.Hour, // 7 days
		MaxBytes:    -1,                 // Unlimited
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create/get KV bucket %s: %w", bucket, err)
	}

	// List keys matching pattern
	keys, err := kv.Keys(context.Background(), jetstream.IgnoreDeletes())
	if err != nil {
		// Handle empty bucket as a valid state, not an error
		if strings.Contains(err.Error(), "no keys found") {
			// Return empty result for empty bucket
			return map[string]any{
				"bucket":  bucket,
				"pattern": pattern,
				"count":   0,
				"entries": []map[string]any{},
			}, nil
		}
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	// Collect entries
	entries := make([]map[string]any, 0, limit)
	count := 0

	for _, key := range keys {
		if count >= limit {
			break
		}

		// Check if key matches pattern
		if !matchesPattern(key, pattern) {
			continue
		}

		// Get entry
		entry, err := kv.Get(context.Background(), key)
		if err != nil {
			ml.logger.Warn("Failed to get KV entry", "key", key, "error", err)
			continue
		}

		// Parse value as JSON if possible
		var value any
		if err := json.Unmarshal(entry.Value(), &value); err != nil {
			// If not JSON, use raw string
			value = string(entry.Value())
		}

		entries = append(entries, map[string]any{
			"key":      key,
			"value":    value,
			"revision": entry.Revision(),
			"created":  entry.Created(),
		})
		count++
	}

	return map[string]any{
		"bucket":  bucket,
		"pattern": pattern,
		"count":   len(entries),
		"entries": entries,
	}, nil
}

// matchesPattern checks if a string matches a simple glob pattern
func matchesPattern(str, pattern string) bool {
	if pattern == "*" || pattern == "" {
		return true
	}

	// Simple pattern matching (supports * wildcard)
	if strings.Contains(pattern, "*") {
		// Convert pattern to simple prefix/suffix match
		if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
			// *substring*
			substr := strings.Trim(pattern, "*")
			return strings.Contains(str, substr)
		} else if strings.HasPrefix(pattern, "*") {
			// *suffix
			suffix := strings.TrimPrefix(pattern, "*")
			return strings.HasSuffix(str, suffix)
		} else if strings.HasSuffix(pattern, "*") {
			// prefix*
			prefix := strings.TrimSuffix(pattern, "*")
			return strings.HasPrefix(str, prefix)
		}
		// prefix*suffix
		parts := strings.Split(pattern, "*")
		if len(parts) == 2 {
			return strings.HasPrefix(str, parts[0]) && strings.HasSuffix(str, parts[1])
		}
	}

	// Exact match
	return str == pattern
}

// ptr is a helper function to get a pointer to a value
func ptr[T any](v T) *T {
	return &v
}
