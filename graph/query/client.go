package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/nats-io/nats.go/jetstream"
)

// Config contains configuration for the Client
type Config struct {
	// AllowUngatedReads permits direct INCOMING_INDEX reads to proceed when graph-index's
	// readiness status endpoint is unreachable (gh#474 Codex #4). Default false =
	// FAIL-CLOSED: an unknown readiness is treated as not-ready, so a query cannot serve
	// a partial bucket during a cutover when the authoritative owner is crashed/restarting.
	// Set true ONLY for a deliberately standalone deployment (or test) that reads the
	// index without a co-deployed graph-index handler — it MUST NOT be used during a
	// format cutover.
	AllowUngatedReads bool `json:"allow_ungated_reads"`

	// EntityCache configuration
	EntityCache cache.Config `json:"entity_cache"`

	// KV Bucket configurations (should match GraphProcessor config)
	EntityStates struct {
		TTL      time.Duration `json:"ttl"`
		History  uint8         `json:"history"`
		Replicas int           `json:"replicas"`
	} `json:"entity_states"`

	SpatialIndex struct {
		TTL      time.Duration `json:"ttl"`
		History  uint8         `json:"history"`
		Replicas int           `json:"replicas"`
	} `json:"spatial_index"`

	IncomingIndex struct {
		TTL      time.Duration `json:"ttl"`
		History  uint8         `json:"history"`
		Replicas int           `json:"replicas"`
	} `json:"incoming_index"`
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() *Config {
	return &Config{
		EntityCache: cache.Config{
			Enabled:         true,
			Strategy:        cache.StrategyHybrid,
			MaxSize:         1000,
			TTL:             5 * time.Minute,
			CleanupInterval: 1 * time.Minute,
		},
		EntityStates: struct {
			TTL      time.Duration `json:"ttl"`
			History  uint8         `json:"history"`
			Replicas int           `json:"replicas"`
		}{
			// TTL MUST be 0 on the live graph: NATS age-eviction is
			// reachability-blind and would silently expire entities with live
			// inbound edges (ADR-068 D1). graph-ingest owns ENTITY_STATES
			// retention; the query client is a reader and must not race in a TTL.
			TTL:      0,
			History:  3,
			Replicas: 1,
		},
		SpatialIndex: struct {
			TTL      time.Duration `json:"ttl"`
			History  uint8         `json:"history"`
			Replicas int           `json:"replicas"`
		}{
			TTL:      0, // no lifecycle retention on the live graph (ADR-068 D1)
			History:  1,
			Replicas: 1,
		},
		IncomingIndex: struct {
			TTL      time.Duration `json:"ttl"`
			History  uint8         `json:"history"`
			Replicas int           `json:"replicas"`
		}{
			TTL:      0, // no lifecycle retention on the live graph (ADR-068 D1)
			History:  1,
			Replicas: 1,
		},
	}
}

// natsClient implements the Client interface for reading graph data from NATS KV buckets
type natsClient struct {
	natsClient *natsclient.Client
	cache      cache.Cache[*gtypes.EntityState]
	config     *Config
	watchCtx   context.Context

	// statusSource is where indexNotReadyErr watches graph-index's ADR-083 readiness
	// envelope. It is always the NATS client in production (set at construction); the
	// narrow interface exists so the gate's fail-closed matrix is unit-testable
	// without a live NATS, which it was not while the gate rode request/reply.
	statusSource readiness.BucketSource

	// The GRAPH_STATUS watch behind the gate, bound on first use and guarded by
	// statusMu. See readinessWatcher for why it is lazy rather than wired at
	// construction.
	statusMu     sync.Mutex
	statusBound  bool
	statusWaited bool
	statusClosed bool
	statusWatch  *readiness.Watcher
	// statusBindWait is the one-shot budget for the watch's first delivery. It
	// defaults to statusBindTimeout and is overridden only by tests, which must not
	// spend seconds per unknown-readiness row.
	statusBindWait time.Duration

	// KV bucket handles
	entityBucket   jetstream.KeyValue
	spatialBucket  jetstream.KeyValue
	incomingBucket jetstream.KeyValue

	// Metrics
	queryCount  int64
	cacheHits   int64
	cacheMisses int64

	// Mutex for bucket initialization
	initMu      sync.Mutex
	initialized bool

	// entityStatePoison is a process-lifetime fail-closed latch. A direct query
	// client must not serve a cached or partial view after any authoritative
	// ENTITY_STATES value violates the canonical graph-state contract.
	entityStatePoison atomic.Pointer[gtypes.StateContractError]
	entityWatchLost   atomic.Bool
	entityObservedRev atomic.Uint64
}

// NewClient creates a new query client with the given NATS client and configuration
func NewClient(ctx context.Context, nc *natsclient.Client, config *Config) (Client, error) {
	return NewClientWithMetrics(ctx, nc, config, nil)
}

// NewClientWithMetrics creates a new query client with the given NATS client, configuration, and optional metrics
func NewClientWithMetrics(
	ctx context.Context,
	nc *natsclient.Client,
	config *Config,
	metricsRegistry *metric.MetricsRegistry,
) (Client, error) {
	if nc == nil {
		return nil, fmt.Errorf("natsClient cannot be nil")
	}
	if config == nil {
		config = DefaultConfig()
	}

	// Create cache with metrics
	entityCache, err := cache.NewFromConfig[*gtypes.EntityState](ctx, config.EntityCache,
		cache.WithMetrics[*gtypes.EntityState](metricsRegistry, "query_client"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create entity cache: %w", err)
	}

	client := &natsClient{
		natsClient:   nc,
		cache:        entityCache,
		config:       config,
		watchCtx:     ctx,
		statusSource: nc,
	}

	return client, nil
}

// ensureBuckets initializes the KV buckets if they haven't been initialized yet
func (qc *natsClient) ensureBuckets(ctx context.Context) error {
	qc.initMu.Lock()
	defer qc.initMu.Unlock()

	if qc.initialized {
		return nil
	}

	// Get or create ENTITY_STATES bucket
	entityConfig := jetstream.KeyValueConfig{
		Bucket:   "ENTITY_STATES",
		TTL:      qc.config.EntityStates.TTL,
		History:  qc.config.EntityStates.History,
		Replicas: qc.config.EntityStates.Replicas,
	}
	entityBucket, err := qc.natsClient.CreateKeyValueBucket(ctx, entityConfig)
	if err != nil {
		return fmt.Errorf("failed to get ENTITY_STATES bucket: %w", err)
	}
	qc.entityBucket = entityBucket

	// Get or create SPATIAL_INDEX bucket
	spatialConfig := jetstream.KeyValueConfig{
		Bucket:   "SPATIAL_INDEX",
		TTL:      qc.config.SpatialIndex.TTL,
		History:  qc.config.SpatialIndex.History,
		Replicas: qc.config.SpatialIndex.Replicas,
	}
	spatialBucket, err := qc.natsClient.CreateKeyValueBucket(ctx, spatialConfig)
	if err != nil {
		return fmt.Errorf("failed to get SPATIAL_INDEX bucket: %w", err)
	}
	qc.spatialBucket = spatialBucket

	// Get or create INCOMING_INDEX bucket
	incomingConfig := jetstream.KeyValueConfig{
		Bucket:   "INCOMING_INDEX",
		TTL:      qc.config.IncomingIndex.TTL,
		History:  qc.config.IncomingIndex.History,
		Replicas: qc.config.IncomingIndex.Replicas,
	}
	incomingBucket, err := qc.natsClient.CreateKeyValueBucket(ctx, incomingConfig)
	if err != nil {
		return fmt.Errorf("failed to get INCOMING_INDEX bucket: %w", err)
	}
	qc.incomingBucket = incomingBucket

	// Open the authoritative watcher only after every bucket handle has been
	// acquired. A later initialization failure must not leak an abandoned
	// watcher that can poison a retrying client. The same watcher is retained
	// from synchronous bootstrap through live observation.
	watcher, err := entityBucket.WatchAll(qc.watchCtx)
	if err != nil {
		return fmt.Errorf("failed to watch ENTITY_STATES: %w", err)
	}
	if err := qc.preflightEntityStates(watcher); err != nil {
		_ = watcher.Stop()
		return err
	}
	go qc.observeEntityStates(watcher)

	qc.initialized = true
	return nil
}

func (qc *natsClient) preflightEntityStates(watcher jetstream.KeyWatcher) error {
	for {
		select {
		case <-qc.watchCtx.Done():
			return fmt.Errorf("ENTITY_STATES preflight cancelled: %w", qc.watchCtx.Err())
		case entry, ok := <-watcher.Updates():
			if !ok {
				return errors.New("ENTITY_STATES watcher closed during preflight")
			}
			if entry == nil {
				return nil
			}
			qc.validateEntityStateEntry(entry)
			qc.entityObservedRev.Store(entry.Revision())
		}
	}
}

func (qc *natsClient) observeEntityStates(watcher jetstream.KeyWatcher) {
	defer func() { _ = watcher.Stop() }()
	for {
		select {
		case <-qc.watchCtx.Done():
			qc.entityWatchLost.Store(true)
			_ = qc.cache.Clear()
			return
		case entry, ok := <-watcher.Updates():
			if !ok {
				qc.entityWatchLost.Store(true)
				_ = qc.cache.Clear()
				return
			}
			if entry == nil {
				continue
			}
			// Invalidate first so a concurrent read cannot reuse a value superseded
			// by this authoritative revision. Validation then permanently poisons
			// the client if the new value is not canonical.
			_, _ = qc.cache.Delete(entry.Key())
			qc.validateEntityStateEntry(entry)
			qc.entityObservedRev.Store(entry.Revision())
		}
	}
}

func (qc *natsClient) validateEntityStateEntry(entry jetstream.KeyValueEntry) {
	if entry.Operation() == jetstream.KeyValueDelete || entry.Operation() == jetstream.KeyValuePurge {
		return
	}
	var state gtypes.EntityState
	if err := gtypes.UnmarshalEntityState(entry.Value(), &state); err != nil {
		var contractErr *gtypes.StateContractError
		if errors.As(err, &contractErr) {
			qc.entityStatePoison.CompareAndSwap(nil, contractErr)
			_ = qc.cache.Clear()
		}
	}
}

func (qc *natsClient) entityStateContractError(operation string) error {
	if contractErr := qc.entityStatePoison.Load(); contractErr != nil {
		return errs.WrapFatal(contractErr, "graph.query", operation,
			"authoritative ENTITY_STATES requires reset and canonical reingest")
	}
	// PERMANENT FOR THIS CLIENT'S LIFETIME (audited under ADR-084 §3.3, deliberately
	// left as-is). Nothing clears entityWatchLost: the only writers are the two
	// Store(true) in observeEntityStates, and the goroutine returns immediately after
	// either. Rebinding is unreachable too — the sole WatchAll call site sits in
	// ensureBuckets behind `if qc.initialized { return nil }`, and initialized is never
	// reset. So a caller that sees this must construct a NEW client; retrying THIS one
	// can never succeed, despite the transient class.
	//
	// The class stays transient because sister repos already match on it, and changing
	// it is a wire-contract break that belongs in its own change rather than riding a
	// readiness-semantics one. A supervised rebind is the honest fix and carries its own
	// design (cache coherence across the gap, bounded attempts, Stop coordination); it is
	// filed separately rather than smuggled in here.
	if qc.entityWatchLost.Load() {
		return errs.ClassifiedCode(errs.ErrorTransient, gtypes.ErrorCodeIndexNotReady,
			errors.New("authoritative ENTITY_STATES watcher is unavailable; this client cannot recover — construct a new one"))
	}
	return nil
}

// GetEntity retrieves a specific entity by ID
func (qc *natsClient) GetEntity(ctx context.Context, entityID string) (*gtypes.EntityState, error) {
	atomic.AddInt64(&qc.queryCount, 1)
	if err := qc.ensureBuckets(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}
	if err := qc.entityStateContractError("GetEntity"); err != nil {
		return nil, err
	}

	// Check cache first
	if cached, exists := qc.cache.Get(entityID); exists {
		if err := qc.entityStateContractError("GetEntity"); err != nil {
			return nil, err
		}
		atomic.AddInt64(&qc.cacheHits, 1)
		return cached, nil
	}

	atomic.AddInt64(&qc.cacheMisses, 1)

	// Get from bucket
	entry, err := qc.entityBucket.Get(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity: %w", err)
	}

	// Unmarshal
	var state gtypes.EntityState
	if err := gtypes.UnmarshalEntityState(entry.Value(), &state); err != nil {
		var contractErr *gtypes.StateContractError
		if errors.As(err, &contractErr) {
			qc.entityStatePoison.CompareAndSwap(nil, contractErr)
			_ = qc.cache.Clear()
		}
		return nil, fmt.Errorf("failed to unmarshal entity state: %w", err)
	}
	if err := qc.entityStateContractError("GetEntity"); err != nil {
		return nil, err
	}

	// Update cache
	if _, err := qc.cache.Set(entityID, &state); err != nil {
		return nil, fmt.Errorf("failed to cache entity: %w", err)
	}

	return &state, nil
}

// GetEntitiesByType returns all entities of a specific type
func (qc *natsClient) GetEntitiesByType(ctx context.Context, entityType string) ([]*gtypes.EntityState, error) {
	entities, err := qc.QueryEntities(ctx, map[string]any{"type": entityType})
	if err != nil {
		return nil, fmt.Errorf("failed to get entities by type: %w", err)
	}
	return entities, nil
}

// GetEntitiesBatch retrieves multiple entities in a single operation
func (qc *natsClient) GetEntitiesBatch(ctx context.Context, entityIDs []string) ([]*gtypes.EntityState, error) {
	if len(entityIDs) == 0 {
		return []*gtypes.EntityState{}, nil
	}

	entities := make([]*gtypes.EntityState, 0, len(entityIDs))
	for _, entityID := range entityIDs {
		entity, err := qc.GetEntity(ctx, entityID)
		if err != nil {
			return nil, fmt.Errorf("failed to get entity %s: %w", entityID, err)
		}
		entities = append(entities, entity)
	}

	return entities, nil
}

// ListEntities returns all entity IDs in the graph
func (qc *natsClient) ListEntities(ctx context.Context) ([]string, error) {
	if err := qc.ensureBuckets(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}
	if err := qc.entityStateContractError("ListEntities"); err != nil {
		return nil, err
	}

	// Get all keys from bucket
	keys, err := qc.entityBucket.Keys(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list entity keys: %w", err)
	}

	return keys, nil
}

// CountEntities returns the total number of entities in the graph
func (qc *natsClient) CountEntities(ctx context.Context) (int, error) {
	keys, err := qc.ListEntities(ctx)
	if err != nil {
		return 0, err
	}
	return len(keys), nil
}

// statusBindTimeout is the one-shot budget for the readiness watch's first delivery.
// It is deliberately the 5s the status request/reply used, so the worst-case latency of
// the FIRST gated read is unchanged by the transport move; every read after it is
// local.
const statusBindTimeout = 5 * time.Second

// readinessWatcher returns the GRAPH_STATUS watch behind the gate, binding it on first
// use, and reports whether readiness is observable at all. It is lazy rather than wired
// in NewClientWithMetrics for two reasons: a client whose caller never touches a gated
// read should not carry a watch goroutine, and this type is also constructed as a bare
// struct literal (tests), which must reach the same single code path rather than a
// second one.
//
// Every caller is serialized on statusMu, so the bind happens once regardless of how
// many goroutines enter the gate together. The watch is started on the client's
// LIFETIME context (watchCtx), never on the per-call context, or the first query to
// finish would cancel the feed for every later one.
func (qc *natsClient) readinessWatcher() *readiness.Watcher {
	qc.statusMu.Lock()
	defer qc.statusMu.Unlock()

	if qc.statusClosed {
		return nil
	}
	if qc.statusBound {
		return qc.statusWatch
	}
	qc.statusBound = true
	if qc.statusSource == nil {
		// No source is a wiring gap, not a readiness answer: leave the watch nil and
		// let the caller take its fail-closed unknown branch.
		return nil
	}
	lifetime := qc.watchCtx
	if lifetime == nil {
		lifetime = context.Background()
	}
	watch := readiness.NewWatcher(qc.statusSource, readiness.KeyGraphIndex)
	if err := watch.Start(lifetime); err != nil {
		return nil
	}
	qc.statusWatch = watch
	return watch
}

// awaitFirstStatus spends the ONE bounded wait for the watch's first delivery. Without
// it a cold client's first gated read would fail closed purely because the watch had
// not delivered yet — a regression against the request/reply, which always produced an
// answer or an error within its timeout. Spent once: with no graph-index deployed
// nothing ever arrives, and a per-call wait would make every read in that (supported,
// allow_ungated_reads) deployment stall for the budget.
func (qc *natsClient) awaitFirstStatus(ctx context.Context, watch *readiness.Watcher) {
	qc.statusMu.Lock()
	if qc.statusWaited {
		qc.statusMu.Unlock()
		return
	}
	qc.statusWaited = true
	budget := qc.statusBindWait
	qc.statusMu.Unlock()

	if budget <= 0 {
		budget = statusBindTimeout
	}
	waitCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	// Dropped deliberately: a timed-out wait is not the verdict. The Read that follows
	// is, and it fails closed on the same unknown.
	_ = watch.WaitForFirst(waitCtx)
}

// indexNotReadyErr gates a direct INCOMING_INDEX read on graph-index HEALTH (gh#474
// Codex #6, narrowed by ADR-084). It returns a classified ErrorCodeIndexNotReady when
// graph-index is up and reports something WRONG — degraded by an unresolved write
// failure, reset-required, or still doing its initial build — which is the cutover/
// failure window where a direct bucket read would return partial topology. It FAILS
// CLOSED when status is unreachable or malformed unless the caller explicitly enables
// AllowUngatedReads for a standalone deployment.
//
// ADR-084 NARROWED this gate: it no longer fires on ordinary revision lag. The
// doc-comment on the transient it emits always said its job was the gh#474 cutover
// window, but gating on exact `Ready` made it fire on any write burst — a healthy index
// two revisions behind was reported as not-ready, and the caller either retried into
// the same window or fell back. That is what sent semsource retrying this transient
// under #592, and it is the conflation ADR-084 names: coverage was being used as a
// proxy for soundness. Lag is now a property to REPORT (staleness_ms on the envelope),
// not a fault to withhold on; the half-built index — the case this gate exists for — is
// caught by bootstrap_complete, which is a fact about the build rather than about how
// far behind it happens to be. This DELIBERATELY supersedes #592's read-path close-out
// and ships in the same wave as ADR-083's breaks so consumers migrate once.
//
// What did NOT change: hard stops still withhold; the unknown branch still fails closed;
// AllowUngatedReads still scopes to exactly the unknown branch and never to a received
// status. Read-your-writes callers are unaffected and better served — they compare their
// own revision against the envelope's IndexedRevision, which ADR-084 makes the one sound
// per-entity check.
//
// ADR-083 moved the source from a `graph.index.query.status` request/reply to the shared
// GRAPH_STATUS watch (graph/readiness). Held state rather than a per-call round-trip is
// the point: the request travelled the same connection as the ENTITY_STATES firehose and
// died under the very load that makes readiness interesting (gh#590).
//
// The unknown set GREW to match what the subject already covered: no bucket, no key,
// a deleted key, a backend fault, an undecodable value, and — new, because state
// persists where a request/reply did not — a feed gone quiet past the freshness window
// (or a key that was ALREADY stale when the watch bound), which is a producer that died
// holding a Ready key. All of them are the one fail-closed branch below.
func (qc *natsClient) indexNotReadyErr(ctx context.Context) error {
	notReady := errs.ClassifiedCode(errs.ErrorTransient, gtypes.ErrorCodeIndexNotReady,
		errors.New("graph index not ready"))
	ungated := qc.config != nil && qc.config.AllowUngatedReads

	watch := qc.readinessWatcher()
	if watch == nil {
		if ungated {
			return nil
		}
		return notReady
	}
	qc.awaitFirstStatus(ctx, watch)

	reading := watch.Read()
	if !reading.Fresh {
		// Unknown readiness. FAIL-CLOSED by default (gh#474 Codex #4): a crashed/restarting
		// graph-index during a cutover is indistinguishable from an intentionally-absent
		// one, and a stale bucket without a reachable authoritative owner is not proof of
		// completeness. Only an explicit standalone config opts into proceeding.
		if ungated {
			return nil
		}
		return notReady
	}
	// Evaluated through the canonical helper so the consumers cannot drift. The gate
	// asks about index HEALTH only: this path wants the best available evidence from a
	// healthy index and reports how stale it was, rather than withholding it.
	// AllowUngatedReads never applies below — a status that WAS received and says
	// unhealthy is not an unknown one.
	if proceed, _ := gtypes.EvaluateReadinessGate(
		gtypes.StatusReading{Status: reading.Status, Fresh: true}); !proceed {
		return notReady
	}
	return nil
}

// GetIncomingEdges retrieves distinct source entity IDs that reference the given
// entity (one per unique source, regardless of how many predicates connect the pair).
//
// Gates on graph-index readiness first (gh#474 Codex #6): reading the INCOMING_INDEX
// bucket directly bypasses graph-index's cutover-readiness gate, so during an in-place
// format upgrade this consumer (and its callers — CountIncomingEdges, the anomaly
// detector) could serve partial topology. When graph-index reports not-ready this
// returns ErrorCodeIndexNotReady instead of a partial result.
func (qc *natsClient) GetIncomingEdges(ctx context.Context, entityID string) ([]string, error) {
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return nil, err
	}
	if err := natsclient.ValidateKVWildcardFilter(entityID + ".>"); err != nil {
		return nil, err
	}
	if err := qc.indexNotReadyErr(ctx); err != nil {
		return nil, err
	}
	if err := qc.ensureBuckets(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}
	if err := qc.entityStateContractError("GetIncomingEdges"); err != nil {
		return nil, err
	}

	// FilteredKeys handles ErrNoKeysFound → nil, nil (no error on empty).
	keys, err := natsclient.FilteredKeys(ctx, qc.incomingBucket, entityID+".>")
	if err != nil {
		return nil, fmt.Errorf("failed to list incoming edges for %s: %w", entityID, err)
	}

	// Parse the sourceID (first 6 tokens) from each composite key suffix.
	prefix := entityID + "."
	seen := make(map[string]struct{}, len(keys))
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		suffix := key[len(prefix):]
		// suffix = "sourceID.hex(predicate)"; sourceID is exactly 6 dot-separated tokens.
		parts := strings.SplitN(suffix, ".", 7)
		if len(parts) < 7 {
			continue
		}
		sourceID := strings.Join(parts[:6], ".")
		if err := semtypes.ValidateEntityID(sourceID); err != nil {
			continue
		}
		predicate, ok := gtypes.DecodePredicateToken(parts[6])
		if !ok {
			continue
		}
		if _, err := vocabulary.ParsePredicate(predicate); err != nil {
			continue
		}
		if _, dup := seen[sourceID]; !dup {
			seen[sourceID] = struct{}{}
			result = append(result, sourceID)
		}
	}

	// Deterministic order (gh#474 P1c): a no-op replay can reshuffle storage order.
	sort.Strings(result)
	return result, nil
}

// GetIncomingRelationships retrieves entity IDs that reference the given entity
func (qc *natsClient) GetIncomingRelationships(ctx context.Context, entityID string) ([]string, error) {
	return qc.GetIncomingEdges(ctx, entityID)
}

// GetOutgoingRelationships returns the entity IDs that this entity points to via relationship triples
func (qc *natsClient) GetOutgoingRelationships(ctx context.Context, entityID string, predicate string) ([]string, error) {
	entity, err := qc.GetEntity(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get entity: %w", err)
	}

	var result []string
	for _, triple := range entity.Triples {
		// Filter by predicate if specified
		if predicate != "" && triple.Predicate != predicate {
			continue
		}
		// Check if this is a relationship triple (object is a valid 6-part EntityID)
		if triple.IsRelationship() {
			if targetID, ok := triple.Object.(string); ok {
				result = append(result, targetID)
			}
		}
	}
	return result, nil
}

// CountIncomingRelationships returns the number of entities pointing to this entity
func (qc *natsClient) CountIncomingRelationships(ctx context.Context, entityID string) (int, error) {
	return qc.CountIncomingEdges(ctx, entityID)
}

// GetEntityConnections returns all entities connected to the specified entity
func (qc *natsClient) GetEntityConnections(ctx context.Context, entityID string) ([]*gtypes.EntityState, error) {
	// Get the source entity
	sourceEntity, err := qc.GetEntity(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get source entity: %w", err)
	}

	var connectedEntities []*gtypes.EntityState
	seenIDs := make(map[string]bool)

	// Get outgoing connections via relationship triples (object is a valid 6-part EntityID)
	for _, triple := range sourceEntity.Triples {
		if triple.IsRelationship() {
			if targetID, ok := triple.Object.(string); ok && !seenIDs[targetID] {
				targetEntity, getErr := qc.GetEntity(ctx, targetID)
				if getErr != nil {
					return nil, fmt.Errorf("failed to get outgoing entity %s: %w", targetID, getErr)
				}
				connectedEntities = append(connectedEntities, targetEntity)
				seenIDs[targetID] = true
			}
		}
	}

	// Get incoming connections using INCOMING_INDEX
	incomingEntityIDs, err := qc.GetIncomingEdges(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("failed to get incoming connections: %w", err)
	}
	for _, incomingID := range incomingEntityIDs {
		if !seenIDs[incomingID] {
			incomingEntity, getErr := qc.GetEntity(ctx, incomingID)
			if getErr != nil {
				return nil, fmt.Errorf("failed to get incoming entity %s: %w", incomingID, getErr)
			}
			connectedEntities = append(connectedEntities, incomingEntity)
			seenIDs[incomingID] = true
		}
	}

	return connectedEntities, nil
}

// VerifyRelationship checks if a relationship exists between two entities
func (qc *natsClient) VerifyRelationship(ctx context.Context, fromID, toID, predicate string) (bool, error) {
	sourceEntity, err := qc.GetEntity(ctx, fromID)
	if err != nil {
		return false, err
	}

	// Check relationship triples
	for _, triple := range sourceEntity.Triples {
		if targetID, ok := triple.Object.(string); ok && targetID == toID {
			if predicate == "" || triple.Predicate == predicate {
				return true, nil
			}
		}
	}

	return false, nil
}

// CountIncomingEdges returns the number of distinct source entities pointing to the
// specified entity (one per unique source, regardless of how many predicates connect
// the pair) — it counts GetIncomingEdges' deduplicated sources, not raw edges.
func (qc *natsClient) CountIncomingEdges(ctx context.Context, entityID string) (int, error) {
	incomingIDs, err := qc.GetIncomingEdges(ctx, entityID)
	if err != nil {
		return 0, err
	}
	return len(incomingIDs), nil
}

// QueryEntities searches for entities matching the specified criteria
func (qc *natsClient) QueryEntities(ctx context.Context, criteria map[string]any) ([]*gtypes.EntityState, error) {
	keys, err := qc.ListEntities(ctx)
	if err != nil {
		return nil, err
	}

	var matchingEntities []*gtypes.EntityState

	for _, key := range keys {
		entity, err := qc.GetEntity(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("failed to load entity %s: %w", key, err)
		}

		if qc.entityMatchesCriteria(entity, criteria) {
			matchingEntities = append(matchingEntities, entity)
		}
	}

	return matchingEntities, nil
}

// GetEntitiesInRegion returns entities in a specific geohash region
func (qc *natsClient) GetEntitiesInRegion(ctx context.Context, geohash string) ([]*gtypes.EntityState, error) {
	if err := qc.ensureBuckets(ctx); err != nil {
		return nil, fmt.Errorf("failed to initialize buckets: %w", err)
	}
	if err := qc.entityStateContractError("GetEntitiesInRegion"); err != nil {
		return nil, err
	}

	entry, err := qc.spatialBucket.Get(ctx, geohash)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return []*gtypes.EntityState{}, nil
		}
		return nil, fmt.Errorf("failed to get spatial index entry %s: %w", geohash, err)
	}

	var spatialData map[string]map[string]any
	if err := json.Unmarshal(entry.Value(), &spatialData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal spatial index: %w", err)
	}

	var entities []*gtypes.EntityState
	if entitiesData, exists := spatialData["entities"]; exists {
		for entityID := range entitiesData {
			entity, getErr := qc.GetEntity(ctx, entityID)
			if getErr != nil {
				return nil, fmt.Errorf("failed to load spatial entity %s: %w", entityID, getErr)
			}
			entities = append(entities, entity)
		}
	}

	return entities, nil
}

// GetCacheStats returns cache performance statistics
func (qc *natsClient) GetCacheStats() CacheStats {
	hits := atomic.LoadInt64(&qc.cacheHits)
	misses := atomic.LoadInt64(&qc.cacheMisses)
	total := hits + misses

	hitRate := 0.0
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	return CacheStats{
		Hits:        hits,
		Misses:      misses,
		Size:        qc.cache.Size(),
		HitRate:     hitRate,
		LastCleared: time.Time{}, // Not available in current cache implementation
	}
}

// Clear clears the cache
func (qc *natsClient) Clear() error {
	qc.cache.Clear()
	atomic.StoreInt64(&qc.cacheHits, 0)
	atomic.StoreInt64(&qc.cacheMisses, 0)
	atomic.StoreInt64(&qc.queryCount, 0)
	return nil
}

// Close shuts down the Client and releases resources, including the readiness watch
// goroutine if one was bound.
func (qc *natsClient) Close() error {
	qc.statusMu.Lock()
	qc.statusClosed = true
	watch := qc.statusWatch
	qc.statusWatch = nil
	qc.statusMu.Unlock()
	// Stop outside the lock: it waits for the watch goroutine, and holding statusMu
	// across that would block a concurrent gate decision behind shutdown.
	if watch != nil {
		watch.Stop()
	}
	return qc.cache.Close()
}

// ExecutePathQuery performs bounded graph traversal with resource limits
func (qc *natsClient) ExecutePathQuery(ctx context.Context, query PathQuery) (*PathResult, error) {
	// Validate query parameters
	if query.StartEntity == "" {
		return nil, fmt.Errorf("start_entity is required")
	}
	if query.MaxDepth <= 0 {
		return nil, fmt.Errorf("max_depth must be positive")
	}
	if query.MaxNodes <= 0 {
		return nil, fmt.Errorf("max_nodes must be positive")
	}
	if query.DecayFactor < 0.0 || query.DecayFactor > 1.0 {
		return nil, fmt.Errorf("decay_factor must be between 0.0 and 1.0")
	}

	// Validate MaxTime is not negative
	if query.MaxTime < 0 {
		return nil, fmt.Errorf("max_time cannot be negative")
	}

	// Validate MaxPaths is not negative
	if query.MaxPaths < 0 {
		return nil, fmt.Errorf("max_paths cannot be negative")
	}

	// Create context with timeout
	queryCtx := ctx
	if query.MaxTime > 0 {
		var cancel context.CancelFunc
		queryCtx, cancel = context.WithTimeout(ctx, query.MaxTime)
		defer cancel()
	}

	// Initialize traversal state
	state := &pathTraversalState{
		visited:  make(map[string]bool),
		entities: make(map[string]*gtypes.EntityState),
		paths:    make([][]string, 0),
		scores:   make(map[string]float64),
	}

	// Get start entity
	startEntity, err := qc.GetEntity(queryCtx, query.StartEntity)
	if err != nil {
		return nil, fmt.Errorf("failed to get start entity %s: %w", query.StartEntity, err)
	}
	if startEntity == nil {
		return nil, fmt.Errorf("start entity %s not found", query.StartEntity)
	}

	// Initialize with start entity
	state.visited[query.StartEntity] = true
	state.entities[query.StartEntity] = startEntity
	state.scores[query.StartEntity] = 1.0 // Start entity has full relevance
	state.nodesVisited = 1

	// Perform depth-first traversal
	if err := qc.traverseGraph(queryCtx, query, state, query.StartEntity, 0, []string{query.StartEntity}); err != nil {
		return nil, fmt.Errorf("traversal failed: %w", err)
	}

	// Convert state to result
	result := &PathResult{
		Entities:  make([]*gtypes.EntityState, 0, len(state.entities)),
		Paths:     state.paths,
		Scores:    state.scores,
		Truncated: state.truncated,
	}

	// Collect entities in deterministic order
	for _, entity := range state.entities {
		result.Entities = append(result.Entities, entity)
	}

	return result, nil
}

// pathTraversalState tracks state during bounded graph traversal
type pathTraversalState struct {
	visited      map[string]bool
	entities     map[string]*gtypes.EntityState
	paths        [][]string
	scores       map[string]float64
	nodesVisited int
	truncated    bool
}

// traverseGraph performs recursive breadth-first traversal with bounds
func (qc *natsClient) traverseGraph(
	ctx context.Context,
	query PathQuery,
	state *pathTraversalState,
	entityID string,
	depth int,
	currentPath []string,
) error {
	// Check context cancellation
	if err := qc.checkTraversalContext(ctx, state); err != nil {
		return err
	}

	// Check depth limit
	if depth >= query.MaxDepth {
		qc.addCompletePath(query, state, currentPath)
		return nil
	}

	// Get current entity
	entity, exists := state.entities[entityID]
	if !exists {
		return fmt.Errorf("entity %s not found in state", entityID)
	}

	// Check if this entity has any valid outgoing relationship triples
	if !qc.hasValidOutgoingRelationships(entity, query.PredicateFilter) {
		qc.addCompletePath(query, state, currentPath)
		return nil
	}

	// Traverse valid outgoing relationship triples
	return qc.traverseRelationships(ctx, query, state, entity, depth, currentPath)
}

// checkTraversalContext checks for context cancellation
func (qc *natsClient) checkTraversalContext(ctx context.Context, state *pathTraversalState) error {
	select {
	case <-ctx.Done():
		state.truncated = true
		return ctx.Err()
	default:
		return nil
	}
}

// addCompletePath adds a path copy to the state if under MaxPaths limit
func (qc *natsClient) addCompletePath(query PathQuery, state *pathTraversalState, currentPath []string) {
	// Check MaxPaths limit (0 means unlimited)
	if query.MaxPaths > 0 && len(state.paths) >= query.MaxPaths {
		return // Don't add more paths
	}

	pathCopy := make([]string, len(currentPath))
	copy(pathCopy, currentPath)
	state.paths = append(state.paths, pathCopy)
}

// hasValidOutgoingRelationships checks if entity has relationship triples matching the filter
func (qc *natsClient) hasValidOutgoingRelationships(entity *gtypes.EntityState, predicateFilter []string) bool {
	for _, triple := range entity.Triples {
		if qc.shouldFollowTriple(triple, predicateFilter) {
			return true
		}
	}
	return false
}

// traverseRelationships processes all valid outgoing relationship triples from an entity
func (qc *natsClient) traverseRelationships(
	ctx context.Context,
	query PathQuery,
	state *pathTraversalState,
	entity *gtypes.EntityState,
	depth int,
	currentPath []string,
) error {
	for _, triple := range entity.Triples {
		if !qc.shouldFollowTriple(triple, query.PredicateFilter) {
			continue
		}

		// Get target ID from triple object (must be a string entity ID)
		targetID, ok := triple.Object.(string)
		if !ok {
			continue // Not a relationship triple
		}

		// Process new target entity if not visited
		if !state.visited[targetID] {
			if err := qc.processNewTargetEntity(ctx, query, state, targetID, entity.ID, currentPath); err != nil {
				if err == errNodeLimitReached {
					return nil // Truncated, but not an error
				}
				return err
			}
		}

		// Continue traversal if not cyclic
		if err := qc.continueTraversalIfValid(ctx, query, state, targetID, depth, currentPath); err != nil {
			return err
		}
	}

	return nil
}

// errNodeLimitReached is returned when node limit is hit
var errNodeLimitReached = fmt.Errorf("node limit reached")

// processNewTargetEntity loads and processes a new target entity
func (qc *natsClient) processNewTargetEntity(
	ctx context.Context,
	query PathQuery,
	state *pathTraversalState,
	targetID, sourceID string,
	currentPath []string,
) error {
	// Check node limit
	if state.nodesVisited >= query.MaxNodes {
		state.truncated = true
		qc.addCompletePath(query, state, currentPath)
		return errNodeLimitReached
	}

	// Load target entity
	targetEntity, err := qc.GetEntity(ctx, targetID)
	if err != nil || targetEntity == nil {
		// Skip entities that can't be loaded
		return nil
	}

	// Add to state
	state.visited[targetID] = true
	state.entities[targetID] = targetEntity
	state.nodesVisited++

	// Calculate score with decay
	decayedScore := state.scores[sourceID] * query.DecayFactor
	state.scores[targetID] = decayedScore

	return nil
}

// continueTraversalIfValid continues traversal if target is not in current path
func (qc *natsClient) continueTraversalIfValid(
	ctx context.Context,
	query PathQuery,
	state *pathTraversalState,
	targetID string,
	depth int,
	currentPath []string,
) error {
	// Avoid cycles by checking current path
	if !qc.containsString(currentPath, targetID) {
		nextPath := append(currentPath, targetID)
		return qc.traverseGraph(ctx, query, state, targetID, depth+1, nextPath)
	}
	return nil
}

// shouldFollowTriple determines if a triple should be followed based on predicate filter
func (qc *natsClient) shouldFollowTriple(triple message.Triple, predicateFilter []string) bool {
	// Only follow relationship triples (object is a valid 6-part EntityID)
	if !triple.IsRelationship() {
		return false
	}

	// If no filter specified, follow all relationship predicates
	if len(predicateFilter) == 0 {
		return true
	}

	// Check if predicate is in filter
	for _, allowedPredicate := range predicateFilter {
		if triple.Predicate == allowedPredicate {
			return true
		}
	}

	return false
}

// containsString checks if a string slice contains a specific value
func (qc *natsClient) containsString(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

// entityMatchesCriteria checks if an entity matches the given criteria
func (qc *natsClient) entityMatchesCriteria(entity *gtypes.EntityState, criteria map[string]any) bool {
	if entity == nil {
		return false
	}

	// Check type criteria - type is now extracted from ID
	if entityType, hasType := criteria["type"].(string); hasType {
		eid, err := message.ParseEntityID(entity.ID)
		if err != nil || eid.Type != entityType {
			return false
		}
	}

	// Check status criteria - status is now domain-specific via triples
	if status, hasStatus := criteria["status"].(string); hasStatus {
		// Look for status in triples
		statusVal, found := entity.GetPropertyValue("status")
		if !found {
			return false
		}
		if statusStr, ok := statusVal.(string); !ok || statusStr != status {
			return false
		}
	}

	// Check property criteria using triples
	for key, expectedValue := range criteria {
		if key == "type" || key == "status" {
			continue // Already checked above
		}

		// Look up property value from triples
		actualValue, found := entity.GetPropertyValue(key)
		if !found || actualValue != expectedValue {
			return false
		}
	}

	return true
}

// GetEntitiesByPredicate returns entity IDs that have a specific predicate.
func (qc *natsClient) GetEntitiesByPredicate(ctx context.Context, predicate string) ([]string, error) {
	if predicate == "" {
		return nil, fmt.Errorf("predicate cannot be empty")
	}

	atomic.AddInt64(&qc.queryCount, 1)

	req := struct {
		Predicate string `json:"predicate"`
	}{Predicate: predicate}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respData, err := qc.natsClient.RequestClassified(ctx, "graph.index.query.predicate", reqData, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to query predicate: %w", err)
	}

	var resp gtypes.PredicateQueryResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return resp.Data.Entities, nil
}

// ListPredicates returns all predicates with their entity counts.
func (qc *natsClient) ListPredicates(ctx context.Context) ([]gtypes.PredicateSummary, error) {
	atomic.AddInt64(&qc.queryCount, 1)

	respData, err := qc.natsClient.RequestClassified(ctx, "graph.index.query.predicateList", []byte("{}"), 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to query predicates: %w", err)
	}

	var resp gtypes.PredicateListQueryResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return resp.Data.Predicates, nil
}

// GetPredicateStats returns detailed statistics for a predicate.
func (qc *natsClient) GetPredicateStats(ctx context.Context, predicate string, sampleLimit int) (*gtypes.PredicateStatsData, error) {
	if predicate == "" {
		return nil, fmt.Errorf("predicate cannot be empty")
	}

	atomic.AddInt64(&qc.queryCount, 1)

	req := struct {
		Predicate   string `json:"predicate"`
		SampleLimit int    `json:"sample_limit"`
	}{Predicate: predicate, SampleLimit: sampleLimit}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respData, err := qc.natsClient.RequestClassified(ctx, "graph.index.query.predicateStats", reqData, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to query predicate stats: %w", err)
	}

	var resp gtypes.PredicateStatsQueryResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &resp.Data, nil
}

// QueryCompoundPredicates performs a compound predicate query with AND/OR logic.
func (qc *natsClient) QueryCompoundPredicates(ctx context.Context, query gtypes.CompoundPredicateQuery) ([]string, error) {
	if len(query.Predicates) == 0 {
		return nil, fmt.Errorf("predicates cannot be empty")
	}
	if query.Operator != "AND" && query.Operator != "OR" {
		return nil, fmt.Errorf("operator must be AND or OR")
	}

	atomic.AddInt64(&qc.queryCount, 1)

	reqData, err := json.Marshal(query)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	respData, err := qc.natsClient.RequestClassified(ctx, "graph.index.query.predicateCompound", reqData, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to query compound predicates: %w", err)
	}

	var resp gtypes.CompoundPredicateQueryResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return resp.Data.Entities, nil
}
