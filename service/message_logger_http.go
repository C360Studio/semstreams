package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/natsclient"
)

func init() {
	RegisterOpenAPISpec("message-logger", messageLoggerOpenAPISpec())
}

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
	mux.HandleFunc("GET "+prefix+"trace/{traceID}", ml.handleGetTrace)

	// KV query endpoints (only in development/test mode)
	mux.HandleFunc(prefix+"kv/", ml.handleKVQuery)

	// KV watch SSE endpoint - streams bucket changes in real-time
	// Note: More specific pattern must be registered to avoid conflict with handleKVQuery
	// The handler itself parses the path to extract bucket name
	mux.HandleFunc("GET "+prefix+"kv/{bucket}/watch", ml.handleKVWatch)

	ml.logger.Info("MessageLogger HTTP handlers registered", "prefix", prefix)
}

// OpenAPISpec returns the OpenAPI specification for MessageLogger endpoints
func (ml *MessageLogger) OpenAPISpec() *OpenAPISpec {
	return messageLoggerOpenAPISpec()
}

// messageLoggerOpenAPISpec returns the OpenAPI specification for MessageLogger endpoints.
// This is a standalone function so it can be called during init() for registry registration.
func messageLoggerOpenAPISpec() *OpenAPISpec {
	return &OpenAPISpec{
		Tags: []TagSpec{
			{
				Name:        "MessageLogger",
				Description: "Message observation and debugging endpoints",
			},
		},
		Paths: map[string]PathSpec{
			"/entries": {
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
							SchemaRef:   "#/components/schemas/MessageLogEntry",
							IsArray:     true,
						},
					},
				},
			},
			"/stats": {
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
			},
			"/subjects": {
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
			},
			"/trace/{traceID}": {
				GET: &OperationSpec{
					Summary:     "Get entries by trace ID",
					Description: "Returns all message entries for a specific W3C trace ID, ordered chronologically",
					Tags:        []string{"MessageLogger"},
					Parameters: []ParameterSpec{
						{
							Name:        "traceID",
							In:          "path",
							Description: "W3C trace ID (32 hex characters)",
							Required:    true,
							Schema:      Schema{Type: "string"},
						},
					},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "Trace entries found",
							ContentType: "application/json",
						},
						"400": {
							Description: "Invalid trace ID format",
						},
					},
				},
			},
			"/kv/{bucket}": {
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
						{
							Name:        "status",
							In:          "query",
							Description: "Opt-in filter: keep only records whose top-level JSON 'status' equals this (e.g. 'failed' over EMBEDDING_INDEX for per-entity failure forensics). Empty (default) returns all records.",
							Required:    false,
							Schema:      Schema{Type: "string"},
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
			},
			"/kv/{bucket}/watch": {
				GET: &OperationSpec{
					Summary:     "Watch KV bucket changes",
					Description: "Stream KV bucket changes via Server-Sent Events (SSE). Supports pattern filtering and SSE reconnection with event IDs.",
					Tags:        []string{"MessageLogger"},
					Parameters: []ParameterSpec{
						{
							Name:        "bucket",
							In:          "path",
							Description: "KV bucket name (e.g., ENTITY_STATES, INCOMING_INDEX)",
							Required:    true,
							Schema:      Schema{Type: "string"},
						},
						{
							Name:        "pattern",
							In:          "query",
							Description: "Key pattern to watch (e.g., 'entity.*'). Default: '*' (all keys)",
							Required:    false,
							Schema:      Schema{Type: "string"},
						},
					},
					Responses: map[string]ResponseSpec{
						"200": {
							Description: "SSE stream of KV changes. Events: 'connected' (initial), 'kv_change' (updates), 'error' (failures)",
							ContentType: "text/event-stream",
						},
						"400": {
							Description: "Invalid bucket name or pattern",
						},
						"404": {
							Description: "Bucket not found",
						},
					},
				},
			},
		},
		// MessageLogEntry is the only typed response - stats returns map[string]any
		ResponseTypes: []reflect.Type{
			reflect.TypeOf(MessageLogEntry{}),
		},
	}
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

// handleGetTrace returns all message entries for a specific trace ID
func (ml *MessageLogger) handleGetTrace(w http.ResponseWriter, r *http.Request) {
	traceID := r.PathValue("traceID")
	if traceID == "" {
		http.Error(w, "Trace ID required", http.StatusBadRequest)
		return
	}

	// Validate trace ID format (32 hex chars for W3C trace ID)
	if len(traceID) != 32 || !isHexString(traceID) {
		http.Error(w, "Invalid trace ID format: must be 32 hex characters", http.StatusBadRequest)
		return
	}

	entries := ml.GetEntriesByTrace(traceID)

	response := map[string]any{
		"trace_id": traceID,
		"count":    len(entries),
		"entries":  entries,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		ml.logger.Error("Failed to encode trace entries", "error", err, "trace_id", traceID)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// isHexString checks if a string contains only hex characters
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
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

	// Return the actual race-safe resolved subscription set. Overlap handling
	// is exposed alongside this set by the statistics endpoint.
	subjects, _ := ml.subjectInspection()

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(subjects); err != nil {
		ml.logger.Error("Failed to encode subjects", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// kvBucketProvider is the narrow NATS capability the read-only KV query
// endpoint is allowed to use. It deliberately exposes lookup only: a query
// endpoint must never be able to create a bucket (see queryKVBucket).
type kvBucketProvider interface {
	GetKeyValueBucket(ctx context.Context, name string) (jetstream.KeyValue, error)
}

// The production NATS client must satisfy the read-only query capability.
var _ kvBucketProvider = (*natsclient.Client)(nil)

// handleKVQuery queries NATS KV buckets (development/test only)
func (ml *MessageLogger) handleKVQuery(w http.ResponseWriter, r *http.Request) {
	ml.handleKVQueryWith(w, r, ml.natsClient)
}

// handleKVQueryWith is handleKVQuery with the bucket source injected, so the
// endpoint's behavior can be driven without a live NATS server.
func (ml *MessageLogger) handleKVQueryWith(w http.ResponseWriter, r *http.Request, provider kvBucketProvider) {
	ctx := r.Context()

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

	// Opt-in status filter (#613): when set, keep only records whose top-level JSON
	// "status" field equals it — e.g. ?status=failed over EMBEDDING_INDEX enumerates the
	// durable failed embeddings for per-entity forensics. Empty (the default) is a no-op,
	// so the endpoint is byte-unchanged for every existing caller. This is the DEBUG tier
	// (message-logger is off by default); production failure observability is the L1
	// metrics + L2 GRAPH_STATUS envelope + the fusion/graph-query relay, complete without it.
	statusFilter := query.Get("status")

	// Query KV bucket
	result, err := ml.queryKVBucket(ctx, provider, bucket, pattern, limit, statusFilter)
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

// queryKVBucket queries a NATS KV bucket. statusFilter, when non-empty, keeps only
// records whose top-level JSON "status" field equals it (#613, opt-in).
func (ml *MessageLogger) queryKVBucket(
	ctx context.Context,
	provider kvBucketProvider,
	bucket, pattern string,
	limit int,
	statusFilter string,
) (map[string]any, error) {
	// Look up the bucket; never create it. The bucket name here is
	// caller-supplied and unvalidated beyond path-traversal characters, so
	// creating on read is doubly wrong:
	//
	//   1. A reader that creates wins the cold-boot race against the bucket's
	//      real owner and imposes its own retention on the live graph. This
	//      endpoint used to create with a 7-day TTL, so a debugging GET for
	//      SPATIAL_INDEX/COMMUNITY_INDEX/EMBEDDING_INDEX on a cluster where the
	//      owner had not started yet left that index silently expiring forever
	//      after — live graph state and required current indexes never carry
	//      TTL or lifecycle eviction (ADR-068 D1). Today the owner's
	//      catalog-seam acquisition strips such a foreign TTL at its next
	//      restart, but a reader still must never be the emitter.
	//   2. A typo'd bucket name must 404, not materialize a permanent bucket.
	//
	// Mirrors getKVBucketForWatch in message_logger_kv_watch.go.
	kv, err := provider.GetKeyValueBucket(ctx, bucket)
	if err != nil {
		if errors.Is(err, jetstream.ErrBucketNotFound) {
			return nil, fmt.Errorf("bucket %s not found: %w", bucket, err)
		}
		return nil, fmt.Errorf("failed to get KV bucket %s: %w", bucket, err)
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

		// Opt-in status filter (#613): skip records whose top-level "status" does not
		// match. A non-JSON or non-object value never matches a status filter.
		if !recordMatchesStatus(value, statusFilter) {
			continue
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

// recordMatchesStatus reports whether a decoded KV value passes the opt-in status
// filter (#613). An EMPTY filter matches everything (the filter is off, so the endpoint
// is unchanged). A non-empty filter matches only a JSON object whose top-level "status"
// string equals it; a non-object value (raw string, array, number) never matches, so a
// bucket without status-shaped records returns nothing under a filter rather than leaking
// unfiltered rows.
func recordMatchesStatus(value any, statusFilter string) bool {
	if statusFilter == "" {
		return true
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return false
	}
	s, _ := obj["status"].(string)
	return s == statusFilter
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
