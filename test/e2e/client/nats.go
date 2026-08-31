// Package client provides test utilities for SemStreams E2E tests
package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/natsclient"
)

// EntityState represents an entity stored in NATS KV
type EntityState struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Triples    []Triple       `json:"triples,omitempty"`
	Version    int            `json:"version"`
	UpdatedAt  string         `json:"updated_at,omitempty"`
}

// Triple represents a semantic triple (subject, predicate, object)
type Triple struct {
	Subject   string `json:"subject"`
	Predicate string `json:"predicate"`
	Object    any    `json:"object"`
	Context   string `json:"context,omitempty"`
}

// Anomaly represents a structural anomaly detected by the inference system
type Anomaly struct {
	ID         string                 `json:"id"`
	Type       string                 `json:"type"`
	EntityA    string                 `json:"entity_a"`
	EntityB    string                 `json:"entity_b,omitempty"`
	Confidence float64                `json:"confidence"`
	Status     string                 `json:"status"`
	Evidence   map[string]interface{} `json:"evidence,omitempty"`
	DetectedAt string                 `json:"detected_at,omitempty"`
}

// AnomalyCounts holds counts of anomalies by type and status
type AnomalyCounts struct {
	ByType   map[string]int `json:"by_type"`
	ByStatus map[string]int `json:"by_status"`
	Total    int            `json:"total"`
}

// NATSValidationClient wraps natsclient.Client for E2E test validation
type NATSValidationClient struct {
	client *natsclient.Client
	closed bool
	mu     sync.Mutex
}

// BucketEntityStates is the KV bucket name for entity states — re-exported
// from the framework KV catalog's name constant so the harness cannot drift.
const BucketEntityStates = graph.BucketEntityStates

// NewNATSValidationClient creates a new NATS validation client
func NewNATSValidationClient(ctx context.Context, natsURL string) (*NATSValidationClient, error) {
	client, err := natsclient.NewClient(natsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create NATS client: %w", err)
	}

	if err := client.Connect(ctx); err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	return &NATSValidationClient{
		client: client,
	}, nil
}

// Client exposes the underlying natsclient.Client so a scenario can drive a
// production writer/reader that requires the concrete type — e.g. the ops
// scenario requests promotion from the E2E app's in-process LessonCurator
// to act as the operator/product promotion path. Returns the live connection;
// callers must not Close it independently (Close is owned by this wrapper).
func (c *NATSValidationClient) Client() *natsclient.Client {
	return c.client
}

// Close closes the NATS connection
func (c *NATSValidationClient) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.client != nil {
		return c.client.Close(ctx)
	}
	return nil
}

// GetKV reads a single KV entry's value from the named bucket. Returns
// the raw bytes so callers can unmarshal into their own type. Used by
// scenarios that verify an agent's CRUD tool write landed in KV with
// the expected content. A nats.ErrKeyNotFound is surfaced distinctly
// so callers can tell "no write" from "transport failure".
func (c *NATSValidationClient) GetKV(ctx context.Context, bucket, key string) ([]byte, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	js, err := c.client.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream: %w", err)
	}

	kv, err := js.KeyValue(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("failed to open bucket %s: %w", bucket, err)
	}

	entry, err := kv.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to read key %s/%s: %w", bucket, key, err)
	}
	return entry.Value(), nil
}

// IsKVKeyNotFound reports whether err is exactly "that key is absent" — not a
// missing bucket, an unreachable server, or a closed client.
//
// GetKV wraps its causes with %w, so the distinction survives; without it an
// assertion of the form "reading this key must fail" reports GREEN for every
// infrastructure failure, which is the fail-open shape a negative assertion is
// most prone to.
func IsKVKeyNotFound(err error) bool {
	return errors.Is(err, jetstream.ErrKeyNotFound)
}

// PutKV writes a key-value entry to the named bucket, creating the
// bucket on first use. Used by e2e scenarios to seed fixtures (persona
// overrides, workflow definitions) before the scenario's assertions run.
func (c *NATSValidationClient) PutKV(ctx context.Context, bucket, key string, value []byte) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	js, err := c.client.JetStream()
	if err != nil {
		return fmt.Errorf("failed to get JetStream: %w", err)
	}

	kv, err := js.KeyValue(ctx, bucket)
	if err != nil {
		// Try to create the bucket if it doesn't exist
		kv, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
			Bucket: bucket,
		})
		if err != nil {
			return fmt.Errorf("failed to get or create bucket %s: %w", bucket, err)
		}
	}

	if _, err := kv.Put(ctx, key, value); err != nil {
		return fmt.Errorf("failed to put key %s: %w", key, err)
	}

	return nil
}

// Publish publishes a message to a NATS subject via JetStream.
// Used for injecting test messages into the system.
func (c *NATSValidationClient) Publish(ctx context.Context, subject string, data []byte) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	return c.client.PublishToStream(ctx, subject, data)
}

// Request sends a NATS request/reply and returns the raw response payload.
// Prefer RequestClassified for handlers on the ADR-060 typed-error contract
// (the graph mutation lane); Request remains for raw-body request/reply.
func (c *NATSValidationClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	return c.client.Request(ctx, subject, data, timeout)
}

// RequestClassified sends a NATS request/reply and surfaces handler failures as
// a classified error (ADR-060) rather than an "error: <msg>" body that a caller
// would silently mis-decode. Used by the graph mutation lane, whose handlers
// return (nil, *errs.ClassifiedError) on failure.
func (c *NATSValidationClient) RequestClassified(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	return c.client.RequestClassified(ctx, subject, data, timeout)
}

// CountEntities counts the number of entities in the ENTITY_STATES bucket
// Returns 0, nil if bucket doesn't exist (graceful degradation)
func (c *NATSValidationClient) CountEntities(ctx context.Context) (int, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, BucketEntityStates)
	if err != nil {
		// Bucket doesn't exist - return 0, not error
		if isBucketNotFoundError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get bucket: %w", err)
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		// Handle empty bucket
		if isNoKeysError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to list keys: %w", err)
	}

	return len(keys), nil
}

// GetEntity retrieves an entity by ID from the ENTITY_STATES bucket
func (c *NATSValidationClient) GetEntity(ctx context.Context, entityID string) (*EntityState, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, BucketEntityStates)
	if err != nil {
		return nil, fmt.Errorf("failed to get bucket: %w", err)
	}

	entry, err := bucket.Get(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("entity not found: %w", err)
	}

	var entity EntityState
	if err := json.Unmarshal(entry.Value(), &entity); err != nil {
		return nil, fmt.Errorf("failed to unmarshal entity: %w", err)
	}

	return &entity, nil
}

// GetTrajectoryPages retrieves every currently visible reference-only fact page
// through the agentic.query.trajectory request/reply handler.
func (c *NATSValidationClient) GetTrajectoryPages(
	ctx context.Context,
	loopID string,
) ([]agentic.TrajectoryPage, error) {
	const maxPages = 1024

	pages := make([]agentic.TrajectoryPage, 0, 1)
	cursor := ""
	seenCursors := make(map[string]struct{})
	for len(pages) < maxPages {
		req, err := json.Marshal(agentic.TrajectoryQueryRequest{
			LoopID: loopID,
			Limit:  256,
			Cursor: cursor,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to marshal trajectory request: %w", err)
		}

		// ADR-060: RequestClassified surfaces handler errors via err instead
		// of a body that could silently decode as an empty page.
		resp, err := c.client.RequestClassified(ctx, "agentic.query.trajectory", req, 5*time.Second)
		if err != nil {
			return nil, fmt.Errorf("trajectory query failed for loop %s: %w", loopID, err)
		}

		var page agentic.TrajectoryPage
		if err := json.Unmarshal(resp, &page); err != nil {
			return nil, fmt.Errorf("failed to unmarshal trajectory page: %w", err)
		}
		if page.SchemaVersion != agentic.TrajectorySchemaV1 || page.LoopID != loopID || page.Coverage != "observed" {
			return nil, fmt.Errorf("trajectory query returned invalid page metadata for loop %s", loopID)
		}
		pages = append(pages, page)
		if page.NextCursor == "" {
			return pages, nil
		}
		if _, duplicate := seenCursors[page.NextCursor]; duplicate {
			return nil, fmt.Errorf("trajectory query repeated cursor for loop %s", loopID)
		}
		seenCursors[page.NextCursor] = struct{}{}
		cursor = page.NextCursor
	}

	return nil, fmt.Errorf("trajectory query exceeded %d pages for loop %s", maxPages, loopID)
}

// ValidateIndexPopulated checks if an index bucket has entries
// Returns false, nil if bucket doesn't exist (graceful degradation)
func (c *NATSValidationClient) ValidateIndexPopulated(ctx context.Context, indexName string) (bool, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, indexName)
	if err != nil {
		// Bucket doesn't exist - return false, not error
		if isBucketNotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to get index bucket: %w", err)
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		// Handle empty bucket
		if isNoKeysError(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to list index keys: %w", err)
	}

	return len(keys) > 0, nil
}

// BucketExists checks if a KV bucket exists
func (c *NATSValidationClient) BucketExists(ctx context.Context, bucketName string) (bool, error) {
	_, err := c.client.GetKeyValueBucket(ctx, bucketName)
	if err != nil {
		if isBucketNotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check bucket: %w", err)
	}
	return true, nil
}

// ListBuckets lists all KV buckets
func (c *NATSValidationClient) ListBuckets(ctx context.Context) ([]string, error) {
	return c.client.ListKeyValueBuckets(ctx)
}

// isBucketNotFoundError checks if an error indicates a bucket doesn't exist
func isBucketNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// JetStream returns specific errors for bucket not found
	return err == jetstream.ErrBucketNotFound ||
		err == jetstream.ErrKeyNotFound
}

// isNoKeysError checks if an error indicates no keys exist
func isNoKeysError(err error) bool {
	if err == nil {
		return false
	}
	return err == jetstream.ErrNoKeysFound
}

// CountBucketKeys counts the number of keys in a specific KV bucket
// Returns 0, nil if bucket doesn't exist (graceful degradation)
func (c *NATSValidationClient) CountBucketKeys(ctx context.Context, bucketName string) (int, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, bucketName)
	if err != nil {
		if isBucketNotFoundError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to get bucket %s: %w", bucketName, err)
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if isNoKeysError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("failed to list keys in %s: %w", bucketName, err)
	}

	return len(keys), nil
}

// GetBucketKeysSample returns a sample of keys from a bucket (first n keys)
// Useful for verifying key patterns without loading all data
func (c *NATSValidationClient) GetBucketKeysSample(ctx context.Context, bucketName string, limit int) ([]string, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, bucketName)
	if err != nil {
		if isBucketNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get bucket %s: %w", bucketName, err)
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if isNoKeysError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list keys in %s: %w", bucketName, err)
	}

	if len(keys) <= limit {
		return keys, nil
	}
	return keys[:limit], nil
}

// GetEntitySample returns a sample of entities from ENTITY_STATES bucket
// Used for entity structure validation in E2E tests
func (c *NATSValidationClient) GetEntitySample(ctx context.Context, limit int) ([]*EntityState, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, BucketEntityStates)
	if err != nil {
		if isBucketNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get entity states bucket: %w", err)
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if isNoKeysError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list entity keys: %w", err)
	}

	// Limit the sample size
	sampleSize := limit
	if len(keys) < limit {
		sampleSize = len(keys)
	}

	entities := make([]*EntityState, 0, sampleSize)
	for i := 0; i < sampleSize; i++ {
		entry, err := bucket.Get(ctx, keys[i])
		if err != nil {
			// Skip entities that can't be retrieved
			continue
		}

		var entity EntityState
		if err := json.Unmarshal(entry.Value(), &entity); err != nil {
			// Skip entities that can't be unmarshaled
			continue
		}

		entities = append(entities, &entity)
	}

	return entities, nil
}

// GetAllEntityIDs returns all entity IDs from ENTITY_STATES bucket.
// Used for hierarchy inference validation in E2E tests (Phase 4).
func (c *NATSValidationClient) GetAllEntityIDs(ctx context.Context) ([]string, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, BucketEntityStates)
	if err != nil {
		if isBucketNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get entity states bucket: %w", err)
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if isNoKeysError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list entity keys: %w", err)
	}

	return keys, nil
}

// IndexBuckets defines the standard index bucket names
var IndexBuckets = struct {
	EntityStates  string
	Predicate     string
	Incoming      string
	Outgoing      string
	Alias         string
	Spatial       string
	Temporal      string
	Embedding     string
	EmbeddingDedp string
	Community     string
}{
	EntityStates:  graph.BucketEntityStates,
	Predicate:     graph.BucketPredicateIndex,
	Incoming:      graph.BucketIncomingIndex,
	Outgoing:      graph.BucketOutgoingIndex,
	Alias:         graph.BucketAliasIndex,
	Spatial:       graph.BucketSpatialIndex,
	Temporal:      graph.BucketTemporalIndex,
	Embedding:     graph.BucketEmbeddingIndex,
	EmbeddingDedp: graph.BucketEmbeddingDedup,
	Community:     graph.BucketCommunityIndex,
}

// GetAllCommunities retrieves all communities from the COMMUNITY_INDEX bucket
// Used for comparing statistical vs LLM-enhanced summaries in E2E tests
func (c *NATSValidationClient) GetAllCommunities(ctx context.Context) ([]*clustering.Community, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, IndexBuckets.Community)
	if err != nil {
		if isBucketNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get community bucket: %w", err)
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if isNoKeysError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list community keys: %w", err)
	}

	var communities []*clustering.Community
	for _, key := range keys {
		// Skip entity-to-community index entries (they have different structure)
		// Community keys have format: "{level}.{communityID}"
		// Entity index keys have format: "entity.{level}.{entityID}"
		if strings.HasPrefix(key, "entity.") {
			continue
		}

		entry, err := bucket.Get(ctx, key)
		if err != nil {
			// Skip entries that can't be retrieved
			continue
		}

		var comm clustering.Community
		if err := json.Unmarshal(entry.Value(), &comm); err != nil {
			// Skip entries that can't be unmarshaled as communities
			continue
		}

		// Only include valid communities (have ID and members)
		if comm.ID != "" && len(comm.Members) > 0 {
			communities = append(communities, &comm)
		}
	}

	return communities, nil
}

// GetCommunitySummaries retrieves every LLM community-summary record from the
// COMMUNITY_SUMMARIES bucket (ADR-087), keyed by its raw {level}.{membership_hash}
// KV key — exactly what clustering.SummaryKey builds and graph-query's read path
// joins on.
//
// After the B3 ownership split the enhancement worker writes summaries HERE rather
// than onto COMMUNITY_INDEX, so enhancement observability must JOIN each community
// to this store by clustering.MembershipHash(members). A missing bucket (e.g. a
// statistical-tier deployment that never runs the worker) is not an error: it
// returns an empty map so callers degrade to the statistical floor, mirroring the
// production read path.
func (c *NATSValidationClient) GetCommunitySummaries(ctx context.Context) (map[string]*clustering.CommunitySummaryRecord, error) {
	summaries := make(map[string]*clustering.CommunitySummaryRecord)

	bucket, err := c.client.GetKeyValueBucket(ctx, graph.BucketCommunitySummaries)
	if err != nil {
		if isBucketNotFoundError(err) {
			return summaries, nil
		}
		return nil, fmt.Errorf("failed to get community summaries bucket: %w", err)
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if isNoKeysError(err) {
			return summaries, nil
		}
		return nil, fmt.Errorf("failed to list community summary keys: %w", err)
	}

	for _, key := range keys {
		entry, err := bucket.Get(ctx, key)
		if err != nil {
			continue
		}
		var rec clustering.CommunitySummaryRecord
		if err := json.Unmarshal(entry.Value(), &rec); err != nil {
			continue
		}
		summaries[key] = &rec
	}

	return summaries, nil
}

// classifyCommunitySummaryStatus joins each community to COMMUNITY_SUMMARIES by
// membership hash (ADR-087) and buckets it: enhanced (a usable llm-enhanced
// record), failed (an llm-failed marker), or pending (no record, or an unusable
// empty enhanced record). It mirrors graph-query's SummaryFor join so the e2e
// counts match what GraphRAG would actually read.
func classifyCommunitySummaryStatus(
	communities []*clustering.Community,
	summaries map[string]*clustering.CommunitySummaryRecord,
) (enhanced, failed, pending int) {
	for _, comm := range communities {
		if comm == nil || len(comm.Members) == 0 {
			continue
		}
		key := clustering.SummaryKey(comm.Level, clustering.MembershipHash(comm.Members))
		rec, ok := summaries[key]
		switch {
		case !ok:
			pending++
		case rec.Status == clustering.SummaryStatusEnhanced && rec.LLMSummary != "":
			enhanced++
		case rec.Status == clustering.SummaryStatusFailed:
			failed++
		default:
			// A record exists but is neither a usable enhanced summary nor a failure
			// marker — do not let it falsely terminate the wait.
			pending++
		}
	}
	return enhanced, failed, pending
}

// WaitForCommunitySummaryEnhancement polls COMMUNITY_SUMMARIES until every supplied
// community has a terminal summary record (llm-enhanced or llm-failed), joining by
// membership hash (ADR-087). Returns counts of enhanced, failed, and pending
// communities.
//
// It replaces the pre-split poll over COMMUNITY_INDEX.SummaryStatus: after B3 the
// worker no longer writes that field, so the old wait could never observe
// pending→0 and always burned its full ceiling before reporting enhanced=0. This
// wait terminates as soon as the summary store is caught up.
func (c *NATSValidationClient) WaitForCommunitySummaryEnhancement(
	ctx context.Context,
	communities []*clustering.Community,
	timeout time.Duration,
	pollInterval time.Duration,
) (enhanced, failed, pending int, err error) {
	// With no communities there is nothing to enhance; report a settled state.
	if len(communities) == 0 {
		return 0, 0, 0, nil
	}

	deadline := time.Now().Add(timeout)

	for {
		summaries, serr := c.GetCommunitySummaries(ctx)
		if serr != nil {
			return 0, 0, 0, fmt.Errorf("failed to get community summaries: %w", serr)
		}

		enhanced, failed, pending = classifyCommunitySummaryStatus(communities, summaries)

		// Every community reached a terminal record.
		if pending == 0 {
			return enhanced, failed, pending, nil
		}

		if !time.Now().Before(deadline) {
			// Timeout reached; return current state without error.
			return enhanced, failed, pending, nil
		}

		select {
		case <-ctx.Done():
			return enhanced, failed, pending, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// GetAnomalyCounts retrieves counts of anomalies by type and status from ANOMALY_INDEX bucket
func (c *NATSValidationClient) GetAnomalyCounts(ctx context.Context) (*AnomalyCounts, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	js, err := c.client.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	bucket, err := js.KeyValue(ctx, "ANOMALY_INDEX")
	if err != nil {
		// Bucket doesn't exist - return zero counts
		return &AnomalyCounts{
			ByType:   make(map[string]int),
			ByStatus: make(map[string]int),
			Total:    0,
		}, nil
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		// No keys - return zero counts
		return &AnomalyCounts{
			ByType:   make(map[string]int),
			ByStatus: make(map[string]int),
			Total:    0,
		}, nil
	}

	counts := &AnomalyCounts{
		ByType:   make(map[string]int),
		ByStatus: make(map[string]int),
		Total:    0,
	}

	for _, key := range keys {
		// Skip index keys (they have format anomaly.idx.*)
		if len(key) > 11 && key[:11] == "anomaly.idx" {
			continue
		}
		// Skip non-anomaly keys
		if len(key) < 8 || key[:8] != "anomaly." {
			continue
		}

		entry, err := bucket.Get(ctx, key)
		if err != nil {
			continue
		}

		var anomaly Anomaly
		if err := json.Unmarshal(entry.Value(), &anomaly); err != nil {
			continue
		}

		counts.Total++
		counts.ByType[anomaly.Type]++
		counts.ByStatus[anomaly.Status]++
	}

	return counts, nil
}

// GetAnomalies retrieves all anomalies from ANOMALY_INDEX bucket
func (c *NATSValidationClient) GetAnomalies(ctx context.Context) ([]*Anomaly, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	js, err := c.client.JetStream()
	if err != nil {
		return nil, fmt.Errorf("failed to get JetStream context: %w", err)
	}

	bucket, err := js.KeyValue(ctx, "ANOMALY_INDEX")
	if err != nil {
		// Bucket doesn't exist - return empty list
		return []*Anomaly{}, nil
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		// No keys - return empty list
		return []*Anomaly{}, nil
	}

	var anomalies []*Anomaly
	for _, key := range keys {
		// Skip index keys (they have format anomaly.idx.*)
		if len(key) > 11 && key[:11] == "anomaly.idx" {
			continue
		}
		// Skip non-anomaly keys
		if len(key) < 8 || key[:8] != "anomaly." {
			continue
		}

		entry, err := bucket.Get(ctx, key)
		if err != nil {
			continue
		}

		var anomaly Anomaly
		if err := json.Unmarshal(entry.Value(), &anomaly); err != nil {
			continue
		}

		anomalies = append(anomalies, &anomaly)
	}

	return anomalies, nil
}

// WaitForAnomalyDetection waits for anomaly detection to complete by polling
// until the anomaly count stabilizes or timeout is reached.
// Returns the final total count and any error encountered.
func (c *NATSValidationClient) WaitForAnomalyDetection(
	ctx context.Context,
	timeout time.Duration,
	pollInterval time.Duration,
) (total int, err error) {
	deadline := time.Now().Add(timeout)
	var lastCount int
	stableCount := 0

	for time.Now().Before(deadline) {
		counts, err := c.GetAnomalyCounts(ctx)
		if err != nil {
			return 0, fmt.Errorf("failed to get anomaly counts: %w", err)
		}

		if counts.Total == lastCount {
			stableCount++
			// Consider stable after 3 consecutive identical readings
			if stableCount >= 3 {
				return counts.Total, nil
			}
		} else {
			stableCount = 0
			lastCount = counts.Total
		}

		select {
		case <-ctx.Done():
			return lastCount, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	// Timeout reached, return current count without error
	return lastCount, nil
}

// VirtualEdgeCounts holds counts of virtual edges by predicate and status.
type VirtualEdgeCounts struct {
	Total       int            // Total virtual edges found
	ByBand      map[string]int // Counts by similarity band (high, medium, related)
	AutoApplied int            // Edges that were auto-applied
}

// CountVirtualEdges counts virtual edges (inferred relationships) via the
// production query API (graph.index.query.predicateList), scoped to the
// "inferred." namespace. Virtual edges use predicates starting with that
// prefix.
//
// Routed through the query API rather than reading PREDICATE_INDEX
// directly (ADR-065): the bucket's internal key format is hashed and
// opaque, so a raw-bucket reader can no longer recover predicate names or
// counts on its own. A genuine query failure or unparseable response is a
// real error here — not swallowed to zero counts — so a caller can
// distinguish "no virtual edges exist" from "couldn't find out." An empty
// but successfully-parsed predicate list is a legitimate zero (e.g. no
// semantic gaps met the auto-apply threshold), not an error.
func (c *NATSValidationClient) CountVirtualEdges(ctx context.Context) (*VirtualEdgeCounts, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	reqJSON, err := json.Marshal(graph.PredicateListQuery{Namespace: "inferred.semantic"})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal predicateList request: %w", err)
	}

	respData, err := c.client.RequestClassified(ctx, "graph.index.query.predicateList", reqJSON, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("predicateList query failed: %w", err)
	}

	var resp graph.PredicateListQueryResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal predicateList response: %w", err)
	}

	counts := &VirtualEdgeCounts{
		ByBand: make(map[string]int),
	}
	for _, p := range resp.Data.Predicates {
		counts.Total += p.EntityCount

		// Parse the band from the predicate (e.g., "inferred.semantic.high" -> "high")
		parts := strings.Split(p.Predicate, ".")
		if len(parts) >= 3 && parts[1] == "semantic" {
			band := parts[2]
			counts.ByBand[band] += p.EntityCount
		}
	}

	return counts, nil
}

// GetAutoAppliedAnomalyCount returns the count of anomalies with status "auto_applied".
func (c *NATSValidationClient) GetAutoAppliedAnomalyCount(ctx context.Context) (int, error) {
	counts, err := c.GetAnomalyCounts(ctx)
	if err != nil {
		return 0, err
	}
	return counts.ByStatus["auto_applied"], nil
}

// IncomingEntry matches the indexmanager.IncomingEntry structure.
// Phase 5: Added to verify IncomingIndex predicate storage.
type IncomingEntry struct {
	Predicate    string `json:"predicate"`
	FromEntityID string `json:"from_entity_id"`
}

// GetIncomingEntries retrieves incoming relationship entries for a target entity.
//
// After composite-key sharding (gh#474) INCOMING_INDEX stores one empty-value key
// per directed edge: "targetID.sourceID.predicate". This reader prefix-scans
// "targetID.>" and reconstructs each entry from its key — the same reconstruction
// graph-index's handleQueryIncomingNATS performs. It reads the ON-DISK format
// directly (not via the query API) so a bug shared by the writer and the query
// handler cannot pass this gate. The source entity ID is exactly 6 dot-separated
// tokens; the predicate is everything after it.
func (c *NATSValidationClient) GetIncomingEntries(ctx context.Context, targetEntityID string) ([]IncomingEntry, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, IndexBuckets.Incoming)
	if err != nil {
		if isBucketNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get incoming bucket: %w", err)
	}

	keys, err := natsclient.FilteredKeys(ctx, bucket, targetEntityID+".>")
	if err != nil {
		return nil, fmt.Errorf("failed to list incoming keys for %s: %w", targetEntityID, err)
	}

	entries := make([]IncomingEntry, 0, len(keys))
	for _, key := range keys {
		entry, ok := incomingEntryFromCompositeKey(key, targetEntityID)
		if !ok {
			continue // malformed key — skip, mirrors handleQueryIncomingNATS
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// incomingEntryFromCompositeKey reconstructs an IncomingEntry from an
// INCOMING_INDEX composite key of the form "targetID.sourceID.predicate", with
// targetID already known and stripped. Mirrors graph-index's incomingEntryFromKey:
// the source ID is exactly 6 dot-separated tokens and the predicate is everything
// after it. Returns false when the key is malformed (too short or empty predicate).
func incomingEntryFromCompositeKey(key, targetID string) (IncomingEntry, bool) {
	prefix := targetID + "."
	if !strings.HasPrefix(key, prefix) {
		return IncomingEntry{}, false
	}
	suffix := key[len(prefix):]

	// suffix = "sourceID.hex(predicate)"; sourceID is exactly 6 dot-separated tokens;
	// the predicate is hex-encoded (gh#474 P1a — graph.DecodePredicateToken).
	parts := strings.SplitN(suffix, ".", 7)
	if len(parts) < 7 {
		return IncomingEntry{}, false
	}
	predicate, ok := graph.DecodePredicateToken(parts[6])
	if !ok || predicate == "" {
		return IncomingEntry{}, false
	}
	return IncomingEntry{
		FromEntityID: strings.Join(parts[:6], "."),
		Predicate:    predicate,
	}, true
}

// AuthorityTripleMatch is one E2E diagnostic match found directly in
// ENTITY_STATES. It is not an application query contract.
type AuthorityTripleMatch struct {
	EntityID string `json:"entity_id"`
	Triple   Triple `json:"triple"`
}

const maxAuthorityProvenanceScan = 10_000

// FindAuthorityTriplesByPredicatePrefix scans bounded authoritative current
// state for triples whose predicates carry predicatePrefix. Selection is
// independent of provenance so the caller can detect missing or changed context.
// It fails rather than truncating if the tier exceeds the diagnostic bound.
func (c *NATSValidationClient) FindAuthorityTriplesByPredicatePrefix(
	ctx context.Context,
	predicatePrefix string,
) ([]AuthorityTripleMatch, error) {
	return c.findAuthorityTriplesByPredicatePrefix(ctx, predicatePrefix, maxAuthorityProvenanceScan)
}

func (c *NATSValidationClient) findAuthorityTriplesByPredicatePrefix(
	ctx context.Context,
	predicatePrefix string,
	maxEntities int,
) ([]AuthorityTripleMatch, error) {
	if predicatePrefix == "" {
		return nil, fmt.Errorf("predicate prefix cannot be empty")
	}
	if maxEntities <= 0 {
		return nil, fmt.Errorf("authority provenance entity limit must be positive")
	}
	bucket, err := c.client.GetKeyValueBucket(ctx, BucketEntityStates)
	if err != nil {
		return nil, fmt.Errorf("failed to get authoritative entity states: %w", err)
	}
	lister, err := bucket.ListKeys(ctx)
	if err != nil {
		if isNoKeysError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list authoritative entity states: %w", err)
	}
	defer func() { _ = lister.Stop() }()

	keys := make([]string, 0, maxEntities)
	for key := range lister.Keys() {
		keys = append(keys, key)
		if len(keys) > maxEntities {
			return nil, fmt.Errorf("authority provenance scan exceeds entity limit %d", maxEntities)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list authoritative entity states: %w", err)
	}
	sort.Strings(keys)

	matches := make([]AuthorityTripleMatch, 0)
	for _, key := range keys {
		entry, getErr := bucket.Get(ctx, key)
		if getErr != nil {
			return nil, fmt.Errorf("failed to read authoritative entity %s: %w", key, getErr)
		}
		var entity EntityState
		if unmarshalErr := json.Unmarshal(entry.Value(), &entity); unmarshalErr != nil {
			return nil, fmt.Errorf("failed to decode authoritative entity %s: %w", key, unmarshalErr)
		}
		for _, triple := range entity.Triples {
			if strings.HasPrefix(triple.Predicate, predicatePrefix) {
				matches = append(matches, AuthorityTripleMatch{EntityID: key, Triple: triple})
			}
		}
	}
	return matches, nil
}

// OutgoingEntry matches the indexmanager.OutgoingEntry structure.
// Phase 6: Added to verify inverse edges are materialized in container's outgoing relationships.
type OutgoingEntry struct {
	Predicate  string `json:"predicate"`
	ToEntityID string `json:"to_entity_id"`
}

// GetOutgoingEntries retrieves outgoing relationship entries for a source entity.
// Phase 6: Added for inverse edges scenario - verifies containers have outgoing 'contains' edges.
func (c *NATSValidationClient) GetOutgoingEntries(ctx context.Context, sourceEntityID string) ([]OutgoingEntry, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, IndexBuckets.Outgoing)
	if err != nil {
		if isBucketNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get outgoing bucket: %w", err)
	}

	entry, err := bucket.Get(ctx, sourceEntityID)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, nil // No outgoing relationships
		}
		return nil, fmt.Errorf("failed to get outgoing entry: %w", err)
	}

	var entries []OutgoingEntry
	if err := json.Unmarshal(entry.Value(), &entries); err != nil {
		return nil, fmt.Errorf("failed to unmarshal outgoing entries: %w", err)
	}
	return entries, nil
}

// --- Phase 8: SSE-Enabled Wait Functions ---
//
// These functions use SSE streaming for real-time KV bucket watching,
// with automatic fallback to polling if SSE is unavailable.

// EntityStabilizationResult contains the result of waiting for entity count to stabilize.
type EntityStabilizationResult struct {
	FinalCount   int
	WaitDuration time.Duration
	Stabilized   bool
	TimedOut     bool
	UsedSSE      bool
}

// WaitForEntityCountSSE waits for entity count to reach target and stabilize using SSE.
// NATS KV watch sends all existing keys first (initial sync), then streams updates.
// We use UniqueKeyCountReaches to count unique non-deleted keys for accurate counting.
// Falls back to polling if SSE is unavailable.
func (c *NATSValidationClient) WaitForEntityCountSSE(
	ctx context.Context,
	expectedCount int,
	timeout time.Duration,
	sseClient *SSEClient,
) EntityStabilizationResult {
	startWait := time.Now()

	// Try SSE first - NATS KV watch sends all existing keys during initial sync
	if sseClient != nil {
		if err := sseClient.Health(ctx); err == nil {
			opts := KVWatchOpts{
				Timeout: timeout,
				Pattern: "*",
			}

			// Use UniqueKeyCountReaches to count actual entities, not events
			// This properly handles initial sync (existing keys) + real-time updates
			events, err := sseClient.WatchKVBucket(ctx, BucketEntityStates, UniqueKeyCountReaches(expectedCount), opts)
			if err == nil {
				// Count unique keys from all events (initial + new)
				uniqueKeys := CountUniqueKeys(events)
				return EntityStabilizationResult{
					FinalCount:   uniqueKeys,
					WaitDuration: time.Since(startWait),
					Stabilized:   uniqueKeys >= expectedCount,
					TimedOut:     false,
					UsedSSE:      true,
				}
			}
			// SSE failed or timed out - check if we got partial results
			if len(events) > 0 {
				uniqueKeys := CountUniqueKeys(events)
				// If we have enough keys despite timeout, consider it a success
				if uniqueKeys >= expectedCount {
					return EntityStabilizationResult{
						FinalCount:   uniqueKeys,
						WaitDuration: time.Since(startWait),
						Stabilized:   true,
						TimedOut:     false,
						UsedSSE:      true,
					}
				}
			}
			// SSE failed - fall through to polling
		}
	}

	// Fallback to polling
	result := c.waitForEntityCountPolling(ctx, expectedCount, timeout)
	result.WaitDuration = time.Since(startWait)
	result.UsedSSE = false
	return result
}

// WaitForSourceEntityCountSSE waits for SOURCE entity count (excluding containers) to reach target.
// Container entities (ending in .group, .group.container, .group.container.level) are excluded.
// This is used to wait for testdata to fully load before validation.
func (c *NATSValidationClient) WaitForSourceEntityCountSSE(
	ctx context.Context,
	expectedCount int,
	timeout time.Duration,
	sseClient *SSEClient,
) EntityStabilizationResult {
	startWait := time.Now()

	// Try SSE first - NATS KV watch sends all existing keys during initial sync
	if sseClient != nil {
		if err := sseClient.Health(ctx); err == nil {
			opts := KVWatchOpts{
				Timeout: timeout,
				Pattern: "*",
			}

			// Use SourceEntityCountReaches to count only source entities (exclude containers)
			events, err := sseClient.WatchKVBucket(ctx, BucketEntityStates, SourceEntityCountReaches(expectedCount), opts)
			if err == nil {
				sourceCount := CountSourceEntities(events)
				return EntityStabilizationResult{
					FinalCount:   sourceCount,
					WaitDuration: time.Since(startWait),
					Stabilized:   sourceCount >= expectedCount,
					TimedOut:     false,
					UsedSSE:      true,
				}
			}
			// SSE failed or timed out - check if we got partial results
			if len(events) > 0 {
				sourceCount := CountSourceEntities(events)
				if sourceCount >= expectedCount {
					return EntityStabilizationResult{
						FinalCount:   sourceCount,
						WaitDuration: time.Since(startWait),
						Stabilized:   true,
						TimedOut:     false,
						UsedSSE:      true,
					}
				}
			}
			// SSE failed - fall through to polling
		}
	}

	// Fallback to polling for source entities
	result := c.waitForSourceEntityCountPolling(ctx, expectedCount, timeout)
	result.WaitDuration = time.Since(startWait)
	result.UsedSSE = false
	return result
}

// waitForSourceEntityCountPolling polls NATS KV until source entity count reaches and stabilizes.
func (c *NATSValidationClient) waitForSourceEntityCountPolling(
	ctx context.Context,
	expectedCount int,
	timeout time.Duration,
) EntityStabilizationResult {
	const stabilizationChecks = 2
	const checkInterval = 50 * time.Millisecond
	const progressInterval = 1 * time.Second

	deadline := time.Now().Add(timeout)
	lastProgress := time.Now()

	var lastCount int
	stableCount := 0
	pollCount := 0

	for time.Now().Before(deadline) {
		count, err := c.CountSourceEntities(ctx)
		pollCount++
		if err != nil {
			// Log progress with error
			if time.Since(lastProgress) >= progressInterval {
				fmt.Printf("    [poll %d] error counting entities: %v\n", pollCount, err)
				lastProgress = time.Now()
			}
			time.Sleep(checkInterval)
			continue
		}

		// Log progress every second
		if time.Since(lastProgress) >= progressInterval {
			fmt.Printf("    [poll %d] entities: %d/%d (stable: %d/%d)\n",
				pollCount, count, expectedCount, stableCount, stabilizationChecks)
			lastProgress = time.Now()
		}

		if count == lastCount && count >= expectedCount {
			stableCount++
			if stableCount >= stabilizationChecks {
				fmt.Printf("    [poll %d] stabilized at %d entities\n", pollCount, count)
				return EntityStabilizationResult{
					FinalCount: count,
					Stabilized: true,
					TimedOut:   false,
				}
			}
		} else {
			stableCount = 0
		}

		lastCount = count
		time.Sleep(checkInterval)
	}

	fmt.Printf("    [poll %d] TIMEOUT - got %d/%d entities\n", pollCount, lastCount, expectedCount)
	return EntityStabilizationResult{
		FinalCount: lastCount,
		Stabilized: false,
		TimedOut:   true,
	}
}

// CountSourceEntities counts non-container entities in ENTITY_STATES bucket.
func (c *NATSValidationClient) CountSourceEntities(ctx context.Context) (int, error) {
	allIDs, err := c.GetAllEntityIDs(ctx)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, id := range allIDs {
		if !isContainerEntityID(id) {
			count++
		}
	}
	return count, nil
}

// isContainerEntityID checks if an entity ID is a container (hierarchy inference).
func isContainerEntityID(id string) bool {
	return strings.HasSuffix(id, ".group") ||
		strings.HasSuffix(id, ".group.container") ||
		strings.HasSuffix(id, ".group.container.level")
}

// waitForEntityCountPolling polls NATS KV until entity count reaches and stabilizes.
func (c *NATSValidationClient) waitForEntityCountPolling(
	ctx context.Context,
	expectedCount int,
	timeout time.Duration,
) EntityStabilizationResult {
	const stabilizationChecks = 2
	const checkInterval = 50 * time.Millisecond

	deadline := time.Now().Add(timeout)

	var lastCount int
	stableCount := 0

	for time.Now().Before(deadline) {
		count, err := c.CountEntities(ctx)
		if err != nil {
			time.Sleep(checkInterval)
			continue
		}

		if count == lastCount && count >= expectedCount {
			stableCount++
			if stableCount >= stabilizationChecks {
				return EntityStabilizationResult{
					FinalCount: count,
					Stabilized: true,
					TimedOut:   false,
				}
			}
		} else {
			stableCount = 0
		}

		lastCount = count
		time.Sleep(checkInterval)
	}

	return EntityStabilizationResult{
		FinalCount: lastCount,
		Stabilized: false,
		TimedOut:   true,
	}
}

// WaitForKeySSE waits for a specific key to appear in a bucket using SSE streaming.
// Falls back to polling if SSE is unavailable.
func (c *NATSValidationClient) WaitForKeySSE(
	ctx context.Context,
	bucket, key string,
	timeout time.Duration,
	sseClient *SSEClient,
) (found bool, usedSSE bool, err error) {
	// Try SSE first
	if sseClient != nil {
		if err := sseClient.Health(ctx); err == nil {
			opts := KVWatchOpts{
				Timeout: timeout,
				Pattern: "*",
			}

			events, err := sseClient.WatchKVBucket(ctx, bucket, KeyExists(key), opts)
			if err == nil {
				for _, e := range events {
					if e.Key == key {
						return true, true, nil
					}
				}
				return false, true, nil
			}
			// SSE failed - fall through to polling
		}
	}

	// Fallback: poll for key
	found, err = c.waitForKeyPolling(ctx, bucket, key, timeout)
	return found, false, err
}

// waitForKeyPolling polls for a specific key to appear.
func (c *NATSValidationClient) waitForKeyPolling(
	ctx context.Context,
	bucket, key string,
	timeout time.Duration,
) (bool, error) {
	const pollInterval = 200 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		kvBucket, err := c.client.GetKeyValueBucket(ctx, bucket)
		if err == nil {
			_, err = kvBucket.Get(ctx, key)
			if err == nil {
				return true, nil
			}
		}

		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	return false, nil
}

// WaitForContainerGroupsSSE waits for container groups (keys ending in ".group") using SSE.
// Falls back to polling if SSE is unavailable.
func (c *NATSValidationClient) WaitForContainerGroupsSSE(
	ctx context.Context,
	expectedCount int,
	timeout time.Duration,
	sseClient *SSEClient,
) (count int, usedSSE bool, err error) {
	// Try SSE first
	if sseClient != nil {
		if err := sseClient.Health(ctx); err == nil {
			opts := KVWatchOpts{
				Timeout: timeout,
				Pattern: "*",
			}

			events, err := sseClient.WatchKVBucket(ctx, BucketEntityStates, KeySuffixCount(".group", expectedCount), opts)
			if err == nil {
				// Count unique .group keys
				seen := make(map[string]bool)
				for _, e := range events {
					if strings.HasSuffix(e.Key, ".group") {
						seen[e.Key] = true
					}
				}
				return len(seen), true, nil
			}
			// SSE failed - fall through to polling
		}
	}

	// Fallback: poll for groups
	count, err = c.waitForContainerGroupsPolling(ctx, expectedCount, timeout)
	return count, false, err
}

// waitForContainerGroupsPolling polls for container group entities.
func (c *NATSValidationClient) waitForContainerGroupsPolling(
	ctx context.Context,
	expectedCount int,
	timeout time.Duration,
) (int, error) {
	const pollInterval = 200 * time.Millisecond
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		allIDs, err := c.GetAllEntityIDs(ctx)
		if err == nil {
			groupCount := 0
			for _, id := range allIDs {
				if strings.HasSuffix(id, ".group") {
					groupCount++
				}
			}
			if groupCount >= expectedCount {
				return groupCount, nil
			}
		}

		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	// Return final count even on timeout
	allIDs, _ := c.GetAllEntityIDs(ctx)
	groupCount := 0
	for _, id := range allIDs {
		if strings.HasSuffix(id, ".group") {
			groupCount++
		}
	}
	return groupCount, nil
}

// ============================================================================
// Workflow Execution Validation (for agentic integration testing)
// ============================================================================

// BucketWorkflowExecutions is the KV bucket for workflow executions
const BucketWorkflowExecutions = "WORKFLOW_EXECUTIONS"

// BucketWorkflowDefinitions is the KV bucket for workflow definitions
const BucketWorkflowDefinitions = "WORKFLOW_DEFINITIONS"

// WorkflowExecution represents a workflow execution state from KV
type WorkflowExecution struct {
	ID           string                 `json:"id"`
	WorkflowID   string                 `json:"workflow_id"`
	WorkflowName string                 `json:"workflow_name"`
	State        string                 `json:"state"`
	CurrentStep  int                    `json:"current_step"`
	CurrentName  string                 `json:"current_name"`
	Iteration    int                    `json:"iteration"`
	StepResults  map[string]StepResult  `json:"step_results,omitempty"`
	Error        string                 `json:"error,omitempty"`
	Trigger      map[string]interface{} `json:"trigger,omitempty"`
	CreatedAt    string                 `json:"created_at"`
	UpdatedAt    string                 `json:"updated_at"`
}

// StepResult represents a workflow step result
type StepResult struct {
	StepName  string          `json:"step_name"`
	Status    string          `json:"status"`
	Output    json.RawMessage `json:"output,omitempty"`
	Error     string          `json:"error,omitempty"`
	Iteration int             `json:"iteration"`
}

// GetWorkflowExecution retrieves a workflow execution by ID
func (c *NATSValidationClient) GetWorkflowExecution(ctx context.Context, execID string) (*WorkflowExecution, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, BucketWorkflowExecutions)
	if err != nil {
		if isBucketNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get workflow executions bucket: %w", err)
	}

	entry, err := bucket.Get(ctx, execID)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get workflow execution: %w", err)
	}

	var exec WorkflowExecution
	if err := json.Unmarshal(entry.Value(), &exec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal workflow execution: %w", err)
	}

	return &exec, nil
}

// GetAllWorkflowExecutions retrieves all workflow executions
func (c *NATSValidationClient) GetAllWorkflowExecutions(ctx context.Context) ([]*WorkflowExecution, error) {
	bucket, err := c.client.GetKeyValueBucket(ctx, BucketWorkflowExecutions)
	if err != nil {
		if isBucketNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get workflow executions bucket: %w", err)
	}

	keys, err := bucket.Keys(ctx)
	if err != nil {
		if isNoKeysError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list workflow execution keys: %w", err)
	}

	var executions []*WorkflowExecution
	for _, key := range keys {
		entry, err := bucket.Get(ctx, key)
		if err != nil {
			continue
		}

		var exec WorkflowExecution
		if err := json.Unmarshal(entry.Value(), &exec); err != nil {
			continue
		}

		executions = append(executions, &exec)
	}

	return executions, nil
}

// WaitForWorkflowState waits for any workflow to reach a terminal state (completed or failed)
// Returns the execution that reached terminal state, or nil on timeout
func (c *NATSValidationClient) WaitForWorkflowState(
	ctx context.Context,
	workflowID string,
	targetStates []string,
	timeout time.Duration,
) (*WorkflowExecution, error) {
	const pollInterval = 200 * time.Millisecond
	deadline := time.Now().Add(timeout)

	targetSet := make(map[string]bool)
	for _, s := range targetStates {
		targetSet[s] = true
	}

	for time.Now().Before(deadline) {
		executions, err := c.GetAllWorkflowExecutions(ctx)
		if err != nil {
			return nil, err
		}

		for _, exec := range executions {
			if workflowID != "" && exec.WorkflowID != workflowID {
				continue
			}
			if targetSet[exec.State] {
				return exec, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	// Timeout - return latest execution state for diagnostics
	executions, _ := c.GetAllWorkflowExecutions(ctx)
	if len(executions) > 0 {
		return executions[len(executions)-1], nil
	}
	return nil, nil
}

// WaitForWorkflowCompletion waits for a workflow to complete successfully
func (c *NATSValidationClient) WaitForWorkflowCompletion(
	ctx context.Context,
	workflowID string,
	timeout time.Duration,
) (*WorkflowExecution, error) {
	return c.WaitForWorkflowState(ctx, workflowID, []string{"completed"}, timeout)
}

// WaitForWorkflowTerminal waits for a workflow to reach any terminal state
func (c *NATSValidationClient) WaitForWorkflowTerminal(
	ctx context.Context,
	workflowID string,
	timeout time.Duration,
) (*WorkflowExecution, error) {
	return c.WaitForWorkflowState(ctx, workflowID, []string{"completed", "failed", "cancelled"}, timeout)
}

// GetWorkflowExecutionsByState returns executions filtered by state
func (c *NATSValidationClient) GetWorkflowExecutionsByState(ctx context.Context, state string) ([]*WorkflowExecution, error) {
	executions, err := c.GetAllWorkflowExecutions(ctx)
	if err != nil {
		return nil, err
	}

	var filtered []*WorkflowExecution
	for _, exec := range executions {
		if exec.State == state {
			filtered = append(filtered, exec)
		}
	}

	return filtered, nil
}

// DeleteKV deletes a key from a KV bucket (for test cleanup)
func (c *NATSValidationClient) DeleteKV(ctx context.Context, bucket, key string) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	js, err := c.client.JetStream()
	if err != nil {
		return fmt.Errorf("failed to get JetStream: %w", err)
	}

	kv, err := js.KeyValue(ctx, bucket)
	if err != nil {
		return fmt.Errorf("failed to get bucket %s: %w", bucket, err)
	}

	return kv.Delete(ctx, key)
}
