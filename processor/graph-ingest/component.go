// Package graphingest provides the graph-ingest component for entity and triple ingestion.
package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/inference"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/dispatch"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/retry"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
)

// Ensure Component implements required interfaces
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// Package-level prometheus metric (registered once to avoid duplicate registration errors)
var (
	metricsOnce         sync.Once
	entitiesUpdatedOnce prometheus.Counter

	indexingProfileOnce       sync.Once
	indexingProfileDefaultVec *prometheus.CounterVec

	mutationRejectionsOnce sync.Once
	mutationRejectionsVec  *prometheus.CounterVec

	predicateContractRejectionsOnce   sync.Once
	predicateContractRejectionsVec    *prometheus.CounterVec
	entityStateContractRejectionsOnce sync.Once
	entityStateContractRejectionsVec  *prometheus.CounterVec

	stubRestampsOnce    sync.Once
	stubRestampsCounter prometheus.Counter

	foreignEdgeUnclaimedOnce sync.Once
	foreignEdgeUnclaimedVec  *prometheus.CounterVec

	foreignEdgeDroppedOnce sync.Once
	foreignEdgeDroppedVec  *prometheus.CounterVec

	ownerLeaseMismatchOnce sync.Once
	ownerLeaseMismatchVec  *prometheus.CounterVec

	processingDurationOnce      sync.Once
	processingDurationHistogram prometheus.Histogram

	ingestLagOnce      sync.Once
	ingestLagHistogram prometheus.Histogram

	redeliveriesDroppedOnce    sync.Once
	redeliveriesDroppedCounter prometheus.Counter

	casRetriesOnce    sync.Once
	casRetriesCounter prometheus.Counter
)

func getEntitiesUpdatedMetric(registry *metric.MetricsRegistry) prometheus.Counter {
	metricsOnce.Do(func() {
		entitiesUpdatedOnce = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "datamanager",
			Name:      "entities_updated_total",
			Help:      "Total entities updated",
		})
		// Register with the metrics registry if available
		if registry != nil {
			_ = registry.RegisterCounter("graph-ingest", "entities_updated_total", entitiesUpdatedOnce)
		} else {
			// Fallback to default prometheus registry for testing
			// Ignore error if already registered (can happen across tests)
			_ = prometheus.DefaultRegisterer.Register(entitiesUpdatedOnce)
		}
	})
	return entitiesUpdatedOnce
}

// getIndexingProfileDefaultMetric returns the process-wide counter that fires
// whenever graph-ingest falls back to the indexing-profile floor at entity
// creation — i.e. neither a Graphable IndexingProfiler nor a mutation-envelope
// indexing_profile field declared a profile (ADR-054 §5). Labeled by
// message_type so operators can see WHICH producers omit a declaration: a new
// "content" type nobody declared would otherwise silently default to "control"
// and never be embedded. message_type is the low-cardinality registry key; the
// full entity subject is intentionally NOT a label (cardinality bomb).
func getIndexingProfileDefaultMetric(registry *metric.MetricsRegistry) *prometheus.CounterVec {
	indexingProfileOnce.Do(func() {
		indexingProfileDefaultVec = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "indexing_profile_default_total",
			Help:      "Entities whose message type was unclassified (no producer declaration AND not in the indexing-profile registry) and defaulted to control — a registry gap",
		}, []string{"message_type"})
		if registry != nil {
			_ = registry.RegisterCounterVec("graph-ingest", "indexing_profile_default_total", indexingProfileDefaultVec)
		} else {
			_ = prometheus.DefaultRegisterer.Register(indexingProfileDefaultVec)
		}
	})
	return indexingProfileDefaultVec
}

// getMutationRejectionsMetric returns the process-wide counter for rejected
// graph-mutation requests, labelled by subject + reason (the MutationResponse
// ErrorCode — a bounded closed set). It operationalizes ADR-055 §3's "loud
// fail-fast" observability: since the must-exist flip shipped (v1.0.0-beta.112),
// a triple.add targeting a non-existent entity surfaces here as
// reason=entity_not_found instead of silently auto-vivifying. It also meters the
// other rejection classes (validation, CAS conflict, create-or-fail, owner-lease
// stale), giving the ADR-054 cost-ledger discipline its operating baseline.
func getMutationRejectionsMetric(registry *metric.MetricsRegistry) *prometheus.CounterVec {
	mutationRejectionsOnce.Do(func() {
		mutationRejectionsVec = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "mutation_rejections_total",
			Help:      "Graph-mutation requests rejected (success=false), by subject and reason (MutationResponse ErrorCode; 'unclassified' when absent)",
		}, []string{"subject", "reason"})
		if registry != nil {
			_ = registry.RegisterCounterVec("graph-ingest", "mutation_rejections_total", mutationRejectionsVec)
		} else {
			_ = prometheus.DefaultRegisterer.Register(mutationRejectionsVec)
		}
	})
	return mutationRejectionsVec
}

func getPredicateContractRejectionsMetric(registry *metric.MetricsRegistry) *prometheus.CounterVec {
	predicateContractRejectionsOnce.Do(func() {
		predicateContractRejectionsVec = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "predicate_contract_rejections_total",
			Help:      "Canonical predicate contract rejections, counted once per unique bounded reason in a rejected candidate",
		}, []string{"lane", "reason"})
		if registry != nil {
			_ = registry.RegisterCounterVec("graph-ingest", "predicate_contract_rejections_total", predicateContractRejectionsVec)
		} else {
			_ = prometheus.DefaultRegisterer.Register(predicateContractRejectionsVec)
		}
	})
	return predicateContractRejectionsVec
}

func getEntityStateContractRejectionsMetric(registry *metric.MetricsRegistry) *prometheus.CounterVec {
	entityStateContractRejectionsOnce.Do(func() {
		entityStateContractRejectionsVec = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "entity_state_contract_rejections_total",
			Help:      "Canonical entity-state identity contract rejections by bounded ingest lane, field, and reason",
		}, []string{"lane", "field", "reason"})
		if registry != nil {
			_ = registry.RegisterCounterVec("graph-ingest", "entity_state_contract_rejections_total", entityStateContractRejectionsVec)
		} else {
			_ = prometheus.DefaultRegisterer.Register(entityStateContractRejectionsVec)
		}
	})
	return entityStateContractRejectionsVec
}

// getStubRestampsMetric returns the process-wide stub-restamp counter (gh#435):
// referential stubs re-stamped as a real birth on create_with_triples instead of
// rejected as entity_already_exists. A DISTINCT, positive signal so a paid-run
// monitor can tell a healthy stub→real path from a true graph-write reject.
func getStubRestampsMetric(registry *metric.MetricsRegistry) prometheus.Counter {
	stubRestampsOnce.Do(func() {
		stubRestampsCounter = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "stub_restamps_total",
			Help:      "Referential-integrity stubs re-stamped as a real birth on create_with_triples (gh#435) — a healthy stub→real path, NOT a rejection.",
		})
		if registry != nil {
			_ = registry.RegisterCounter("graph-ingest", "stub_restamps_total", stubRestampsCounter)
		} else {
			_ = prometheus.DefaultRegisterer.Register(stubRestampsCounter)
		}
	})
	return stubRestampsCounter
}

// getForeignEdgeUnclaimedMetric returns the process-wide counter for the
// ADR-056 Decision-4 T2-seam reject: a foreign-subject edge (Subject != the
// ingested entity) whose producing (message_type, predicate) has no registered
// ForeignEdgeClaim. For W0 increment 4a this is OBSERVE-ONLY — the edge is still
// routed (deprecated-on-arrival); the metric counts edges CLASSIFIED-unclaimed at
// the seam, NOT routing failures. The hard reject + the ADR-055 must-exist flip
// are gated on this reading zero over a bake window (4c). NOTE: today no
// production producer emits foreign edges (OMS/StoredMessage emit none), so this
// reads zero in cmd/semstreams BY ABSENCE, not because producers migrated.
// Labels: message_type (the dotted registry key, '_invalid' for an unregistered/
// zero type — both bounded, closed sets) and predicate (exact vocabulary string).
func getForeignEdgeUnclaimedMetric(registry *metric.MetricsRegistry) *prometheus.CounterVec {
	foreignEdgeUnclaimedOnce.Do(func() {
		foreignEdgeUnclaimedVec = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "foreign_edge_unclaimed_total",
			Help:      "Unclaimed foreign-subject (message_type,predicate) pairs at the T2-seam — no registered ForeignEdgeClaim (ADR-056 Decision 4; observe-only, edge still routed). Counted once per ingest per pair (deduped), so it is a hatch-not-empty signal, not a per-edge volume",
		}, []string{"message_type", "predicate"})
		if registry != nil {
			_ = registry.RegisterCounterVec("graph-ingest", "foreign_edge_unclaimed_total", foreignEdgeUnclaimedVec)
		} else {
			_ = prometheus.DefaultRegisterer.Register(foreignEdgeUnclaimedVec)
		}
	})
	return foreignEdgeUnclaimedVec
}

// getForeignEdgeDroppedMetric returns the process-wide counter for foreign edges
// the routing seam DROPPED rather than routed (ADR-056 Decision-4 4c-pre-2). It
// is DISTINCT from foreign_edge_unclaimed_total on purpose: that counter is the
// flip-gate's hatch-empty signal (a CLAIMED-as-unclaimed pair); this counter
// meters a *claimed* edge that could not be routed because its target was absent
// and its mode forbade materialising it. Folding the two would corrupt the
// flip-gate reading (a Strict drop would keep the hatch counter non-zero for an
// unrelated reason). Labels: message_type ('_invalid' for an unregistered/zero
// type), predicate (exact vocabulary string), and reason — a bounded closed set:
// 'strict_absent_target' (EdgeStrict, target absent) and 'conditional_deferred'
// (EdgeConditional/EdgeBackfill, routed-with-warn until the PENDING_EDGES buffer
// lands).
func getForeignEdgeDroppedMetric(registry *metric.MetricsRegistry) *prometheus.CounterVec {
	foreignEdgeDroppedOnce.Do(func() {
		foreignEdgeDroppedVec = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "foreign_edge_dropped_total",
			Help:      "Foreign-subject edges dropped or deferred at the routing seam by claim mode (ADR-056 Decision 4). Labels: message_type, predicate, reason (strict_absent_target|conditional_deferred). DISTINCT from foreign_edge_unclaimed_total (the hatch-empty flip-gate signal)",
		}, []string{"message_type", "predicate", "reason"})
		if registry != nil {
			_ = registry.RegisterCounterVec("graph-ingest", "foreign_edge_dropped_total", foreignEdgeDroppedVec)
		} else {
			_ = prometheus.DefaultRegisterer.Register(foreignEdgeDroppedVec)
		}
	})
	return foreignEdgeDroppedVec
}

// getOwnerLeaseMismatchMetric returns the process-wide counter for the
// ADR-056 PR-3 observe-only lease check: an owned write whose OwnerToken does
// not match the live owner claim's "<owner>#<incarnation>" at the graph-ingest
// write seam. OBSERVE-ONLY — the write always commits in PR-3; the reject flip
// is a later PR. Labels: message_type (dotted registry key; '_invalid' for
// unregistered/zero) and predicate (the specific owned predicate whose live
// incarnation differed). Mirrors getForeignEdgeUnclaimedMetric in structure.
func getOwnerLeaseMismatchMetric(registry *metric.MetricsRegistry) *prometheus.CounterVec {
	ownerLeaseMismatchOnce.Do(func() {
		ownerLeaseMismatchVec = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "owner_lease_mismatch_total",
			Help:      "Observe-only count of owned writes whose OwnerToken did not match the live owner lease (ADR-056 PR-3; reject is a later PR — write always commits here). Labels: message_type, predicate.",
		}, []string{"message_type", "predicate"})
		if registry != nil {
			_ = registry.RegisterCounterVec("graph-ingest", "owner_lease_mismatch_total", ownerLeaseMismatchVec)
		} else {
			_ = prometheus.DefaultRegisterer.Register(ownerLeaseMismatchVec)
		}
	})
	return ownerLeaseMismatchVec
}

// getProcessingDurationMetric returns the process-wide histogram of per-message
// apply time — the merge + CAS write inside handleMessage (gh#480). Paired with
// getIngestLagMetric it separates processing time from queue wait, which the
// component previously could not distinguish (callers inferred end-to-end latency
// downstream). Buckets span sub-ms (a warm CAS is ~1.5ms) to seconds (a stalled KV).
func getProcessingDurationMetric(registry *metric.MetricsRegistry) prometheus.Histogram {
	processingDurationOnce.Do(func() {
		processingDurationHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "processing_duration_seconds",
			Help:      "Per-message apply time in graph-ingest (merge + CAS write). The processing half of the queue-wait-vs-processing split (gh#480).",
			Buckets:   []float64{0.0005, 0.001, 0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5},
		})
		if registry != nil {
			_ = registry.RegisterHistogram("graph-ingest", "processing_duration_seconds", processingDurationHistogram)
		} else {
			_ = prometheus.DefaultRegisterer.Register(processingDurationHistogram)
		}
	})
	return processingDurationHistogram
}

// getIngestLagMetric returns the process-wide histogram of message age at the
// moment graph-ingest begins processing it (now − the JetStream message
// timestamp) — i.e. how long a message waited in the stream/delivery buffer
// before ingest reached it (gh#480 queue wait). A rising lag with flat
// processing_duration is the backlog signature (consumer_pending_messages
// climbing) the issue describes.
func getIngestLagMetric(registry *metric.MetricsRegistry) prometheus.Histogram {
	ingestLagOnce.Do(func() {
		ingestLagHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "ingest_lag_seconds",
			Help:      "Age of a message when graph-ingest starts processing it (queue/backlog wait). The queue-wait half of the split (gh#480).",
			Buckets:   []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 2.5, 5, 10, 30, 60},
		})
		if registry != nil {
			_ = registry.RegisterHistogram("graph-ingest", "ingest_lag_seconds", ingestLagHistogram)
		} else {
			_ = prometheus.DefaultRegisterer.Register(ingestLagHistogram)
		}
	})
	return ingestLagHistogram
}

// getRedeliveriesDroppedMetric returns the process-wide counter of messages
// dropped by the keyed-ingest redelivery guard (ADR-072 B1/B2/B3): a delayed
// redelivery whose stream sequence is not newer than the last already applied
// to that entity from the same stream. A small steady number under overload is
// healthy; a large one means MaxAckPending/AckWait are undersized for the lane
// throughput. Disjoint from the pool's dispatch_dropped_total (a full-lane
// non-blocking reject) — these count different events, do not sum them.
func getRedeliveriesDroppedMetric(registry *metric.MetricsRegistry) prometheus.Counter {
	redeliveriesDroppedOnce.Do(func() {
		redeliveriesDroppedCounter = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "redeliveries_dropped_total",
			Help:      "Stale redeliveries dropped by the applied-sequence guard (ADR-072).",
		})
		if registry != nil {
			_ = registry.RegisterCounter("graph-ingest", "redeliveries_dropped_total", redeliveriesDroppedCounter)
		} else {
			_ = prometheus.DefaultRegisterer.Register(redeliveriesDroppedCounter)
		}
	})
	return redeliveriesDroppedCounter
}

// getCasRetriesMetric returns the process-wide counter of CAS-conflict retries
// during entity merge (ADR-072). Incremented each time MergeEntity's CAS
// callback re-runs (attempt > 1) — the previous attempt's revision-checked Put
// lost the CAS and retried. This is a **cross-entity contention-observability**
// signal, NOT a proof of keying correctness (review H1): an entity's OWN key is
// never written concurrently under keying, but legitimate cross-entity
// referential writes (relationship-target stubs, foreign edges, shared
// hierarchy containers) DO touch shared keys and retry. Expect ~0 on workloads
// without hierarchy / dense relationships / entity-birth churn; a spike is a
// workload signal, not necessarily a bug.
func getCasRetriesMetric(registry *metric.MetricsRegistry) prometheus.Counter {
	casRetriesOnce.Do(func() {
		casRetriesCounter = prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "graph_ingest",
			Name:      "cas_retries_total",
			Help:      "Entity-merge CAS-conflict retries — cross-entity contention observability, NOT a keying-correctness proof (ADR-072).",
		})
		if registry != nil {
			_ = registry.RegisterCounter("graph-ingest", "cas_retries_total", casRetriesCounter)
		} else {
			_ = prometheus.DefaultRegisterer.Register(casRetriesCounter)
		}
	})
	return casRetriesCounter
}

// Config holds configuration for graph-ingest component
type Config struct {
	Ports              *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	EnableHierarchy    bool                  `json:"enable_hierarchy" schema:"type:bool,description:Enable hierarchy inference,default:false,category:advanced"`
	EnableTypeSiblings *bool                 `json:"enable_type_siblings" schema:"type:bool,description:Enable sibling edges between same-type entities (default true when hierarchy enabled),category:advanced"`
	// EnforceOwnerLease flips the ADR-056 owner-lease check from observe-only
	// (PR-3: meter owner_lease_mismatch_total + Warn, write commits) to a hard
	// reject (PR-5): a write whose OwnerToken does not match the live owner of a
	// contested predicate is refused with ErrorCodeOwnerLeaseStale. Default false
	// preserves the observe-only bake posture; operators flip it on per-deploy
	// once the mismatch metric reads zero. All fail-open cases (empty token,
	// no claim reader, legacy/pre-fence owner, reader blip) stay fail-open.
	EnforceOwnerLease bool `json:"enforce_owner_lease" schema:"type:bool,description:Reject writes whose OwnerToken does not match the live owner lease (ADR-056 PR-5); default false keeps observe-only metering,default:false,category:advanced"`
	// IngestLanes is the number of keyed-concurrent ingest lanes (ADR-072,
	// gh#480). Messages are partitioned by entity ID (same entity → one lane →
	// serial in arrival order, preserving the arrival-order merge; different
	// entities → parallel), so ingest is no longer bound to a single serial
	// Get+CAS round-trip chain. Default 8 (concurrent); `1` selects the prior
	// fully-serial behavior.
	IngestLanes int `json:"ingest_lanes" schema:"type:int,description:Keyed-concurrent ingest lane count (same entity ID → one lane → ordered; 1 = serial),default:8,category:advanced"`
}

// Validate implements component.Validatable interface
func (c *Config) Validate() error {
	if c.Ports == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "ports configuration required")
	}
	if len(c.Ports.Inputs) == 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "at least one input port required")
	}
	if len(c.Ports.Outputs) == 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "at least one output port required")
	}
	// IngestLanes < 1 clamps to serial (ADR-072). Clamp rather than reject so a
	// mis-set 0/negative degrades to safe-serial instead of failing boot.
	if c.IngestLanes < 1 {
		c.IngestLanes = 1
	}
	return nil
}

// ApplyDefaults sets default values for configuration
func (c *Config) ApplyDefaults() {
	// EnableHierarchy defaults to false
	if c.Ports == nil {
		c.Ports = &component.PortConfig{}
	}
	// Concurrent-by-default (ADR-072): 0 (unset) → 8 lanes. An explicit 1 opts
	// back into serial and is preserved.
	if c.IngestLanes == 0 {
		c.IngestLanes = defaultIngestLanes
	}
}

// DefaultConfig returns a valid default configuration
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:    "entity_stream",
					Type:    "jetstream",
					Subject: "entity.>",
					Config: component.JetStreamPort{
						DeliverPolicy: "all", // Idempotent: catch up on historical entities
					},
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:    "entity_states",
					Type:    "kv-write",
					Subject: graph.BucketEntityStates,
				},
			},
		},
		EnableHierarchy: false,
		IngestLanes:     defaultIngestLanes,
	}
}

// Keyed-concurrent ingest sizing (ADR-072, gh#480).
const (
	// defaultIngestLanes is the default keyed-concurrent lane count. 8 overlaps
	// the KV Get+CAS round-trips on the ~11/12-idle box the issue profiled;
	// tune against the semboids repro via the inflight/queue-wait metrics.
	defaultIngestLanes = 8

	// ingestLaneQueueDepth bounds each lane's in-memory submit queue. Total
	// buffered work is capped at lanes × this; SubmitBlocking applies
	// backpressure past it. Sized generously vs. AckWait so the pipeline drains
	// before redelivery (defense-in-depth atop the applied-sequence guard).
	ingestLaneQueueDepth = 256

	// ingestGuardMemMaxPerLane bounds the in-memory redelivery-guard cache per
	// lane. This is a CACHE-SIZE knob only, not correctness: an eviction just
	// costs a durable-tier read on the next access (ADR-072 B3). Sized to hold
	// the working set of recently-active entities per lane under load.
	ingestGuardMemMaxPerLane = 65536

	// graphIngestGuardBucket is the durable redelivery-guard tier's KV bucket.
	graphIngestGuardBucket = "GRAPH_INGEST_APPLIED_SEQ"
)

// schema defines the configuration schema for graph-ingest component
var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// entityManagerAdapter adapts Component to implement inference.EntityManager interface
type entityManagerAdapter struct {
	component *Component
}

func (a *entityManagerAdapter) ExistsEntity(ctx context.Context, id string) (bool, error) {
	_, err := a.component.entityBucket.Get(ctx, id)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (a *entityManagerAdapter) CreateEntity(ctx context.Context, entity *graph.EntityState) (*graph.EntityState, error) {
	err := a.component.CreateEntity(ctx, entity)
	if err != nil {
		return nil, err
	}
	return entity, nil
}

func (a *entityManagerAdapter) ListWithPrefix(ctx context.Context, prefix string) ([]string, error) {
	// Use server-side prefix filtering (prefix + "." to ensure we match the exact level)
	return a.component.entityBucket.KeysByPrefix(ctx, prefix+".")
}

// tripleAdderAdapter adapts Component to implement inference.TripleAdder interface
type tripleAdderAdapter struct {
	component *Component
}

func (a *tripleAdderAdapter) AddTriple(ctx context.Context, triple message.Triple) error {
	return a.component.AddTriple(ctx, triple)
}

// ownershipClaimReader classifies a producer's foreign-edge predicates and
// resolves the live owner+incarnation for the ADR-056 write-time lease check.
//
// UnclaimedForeignEdges returns the subset of predicates with no covering
// ForeignEdgeClaim (the observe-only seam metric, Decision 4).
//
// ForeignEdgeMode returns one predicate's covering EdgeMode so the routing seam
// can branch (NoBirthStub→stub, Strict→drop, Conditional/Backfill→deferred).
//
// OwnerOf returns the live (owner, incarnation) recorded at RegisterOwner time
// for the OwnerClaim that governs (entityID, predicate) in an owning mode
// (Decision 2). ok=false means the predicate is unclaimed or append-evidence —
// the lease check skips it. incarnation may be "" even when ok=true for a
// legacy/pre-fence claim; the PR-3 check MUST fail-open in that case (do NOT
// reject — the lease is not enforceable against a legacy owner).
//
// *ownership.ClaimReader is the production implementation; tests inject a fake.
// Kept as an interface so all seams are unit-testable without NATS.
type ownershipClaimReader interface {
	UnclaimedForeignEdges(ctx context.Context, producer string, predicates []string) ([]string, error)
	ForeignEdgeMode(ctx context.Context, producer, predicate string) (ownership.EdgeMode, bool, error)
	OwnerOf(ctx context.Context, entityID, predicate string) (owner, incarnation string, ok bool, err error)
}

// Component implements the graph-ingest processor
type Component struct {
	// Component metadata
	name   string
	config Config

	// Dependencies
	decoder    *message.Decoder
	natsClient *natsclient.Client
	logger     *slog.Logger

	// Domain resources
	entityBucket      *natsclient.KVStore            // KV operations with CAS support
	entityStateBucket jetstream.KeyValue             // raw bucket for the process-lifetime query contract guard
	entityCache       cache.Cache[graph.EntityState] // Read-through cache for query handlers
	suffixBucket      *natsclient.KVStore            // KV suffix index: suffix → fullID
	suffixCache       cache.Cache[string]            // TTL cache for suffix resolution

	// ADR-056 Decision-4 T2-seam + PR-3 owner-lease check: read-only view of
	// registered ForeignEdgeClaims and owning OwnerClaims. nil when OWNER_CLAIMS
	// is absent (resourceless/unmigrated deploy) — both seams graceful-skip.
	// An interface so seams are unit-testable with a fake; *ownership.ClaimReader
	// is production. foreignEdgeWarnedKeys dedupes the one-time WARN per
	// (message_type|predicate) so an unclaimed producer logs once, not per message.
	claimReader           ownershipClaimReader
	foreignEdgeWarnedKeys sync.Map

	// maxPrefixResponseBytesOverride, when > 0, replaces the package-level
	// maxPrefixResponseBytes byte budget in handleQueryPrefixNATS. It exists
	// solely so unit tests can exercise the byte-trim cursor path without
	// constructing 800KB of fixtures. Production never sets it (stays 0 →
	// the const default applies). Set once before the handler runs in tests;
	// the handler only reads it, so no synchronisation is required.
	maxPrefixResponseBytesOverride int

	// Inference components
	hierarchyInference *inference.HierarchyInference

	// Lifecycle state
	mu                      sync.RWMutex
	running                 bool
	initialized             bool
	startTime               time.Time
	wg                      sync.WaitGroup
	cancel                  context.CancelFunc
	entityStateWatcher      jetstream.KeyWatcher
	entityStatePoison       atomic.Pointer[graph.StateContractError]
	entityWatchLost         atomic.Bool
	entityBootstrapStarted  atomic.Bool
	entityBootstrapComplete atomic.Bool
	entityQueryMu           sync.RWMutex
	// beforeEntityQueryResponse is a deterministic test seam. Production leaves
	// it nil; every successful query still passes through the final response gate.
	beforeEntityQueryResponse func([]byte)

	// Metrics (atomic)
	messagesProcessed int64
	bytesProcessed    int64
	errors            int64
	lastActivity      atomic.Value // stores time.Time

	// Prometheus metrics. The historical exported series name remains stable
	// because dashboards and operator alerts consume it as an external contract.
	entitiesUpdated               prometheus.Counter
	indexingProfileDefault        *prometheus.CounterVec
	mutationRejections            *prometheus.CounterVec
	predicateContractRejections   *prometheus.CounterVec
	entityStateContractRejections *prometheus.CounterVec
	stubRestamps                  prometheus.Counter
	foreignEdgeUnclaimed          *prometheus.CounterVec
	foreignEdgeDropped            *prometheus.CounterVec
	ownerLeaseMismatch            *prometheus.CounterVec // ADR-056 PR-3 observe-only lease-mismatch counter
	processingDuration            prometheus.Histogram   // gh#480 per-message apply time (processing half)
	ingestLag                     prometheus.Histogram   // gh#480 message age at processing start (queue-wait half)
	redeliveriesDropped           prometheus.Counter     // ADR-072 stale redeliveries dropped by the applied-sequence guard
	casRetries                    prometheus.Counter     // ADR-072 entity-merge CAS-conflict retries (contention observability)
	metricsRegistry               *metric.MetricsRegistry

	// Keyed-concurrent entity ingest (ADR-072, gh#480). The pool partitions
	// ingest by entity ID so same-entity updates stay ordered while different
	// entities ingest in parallel. Built in Start BEFORE subscriptions (M3);
	// drained in Stop with submit-ctx cancelled first. ingestPoolCtx is the
	// Process run ctx (independent of the consume ctx so a consumer-ctx cancel
	// does not abort in-flight merges); ingestSubmitCtx is cancelled first on
	// Stop so a consume callback parked in SubmitBlocking unblocks and Naks.
	ingestPool         *dispatch.KeyedPool[ingestWork]
	ingestPoolCtx      context.Context
	ingestPoolCancel   context.CancelFunc
	ingestSubmitCtx    context.Context
	ingestSubmitCancel context.CancelFunc

	// Redelivery guard (ADR-072 B1/B2/B3), two-tier. ingestGuardMem is the
	// in-memory fast path, one map per lane (lane-local → lock-free, since a
	// lane is drained by a single goroutine). ingestGuardBucket is the durable
	// backstop `(entityID/streamName) → last-applied stream seq`; it survives
	// restart and cache eviction so correctness does not depend on the in-memory
	// retention policy or MaxDeliver/AckWait sizing.
	ingestGuardMem    []*laneGuard
	ingestGuardBucket *natsclient.KVStore

	// Lifecycle reporting
	lifecycleReporter component.LifecycleReporter

	// Query and mutation subscriptions (for cleanup)
	subscriptions []*natsclient.Subscription
}

// CreateGraphIngest is the factory function for creating graph-ingest components
func CreateGraphIngest(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Validate dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "CreateGraphIngest", "factory", "NATSClient required")
	}
	natsClient := deps.NATSClient

	// Parse configuration
	var config Config
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.Wrap(err, "CreateGraphIngest", "factory", "config unmarshal")
		}
	} else {
		config = DefaultConfig()
	}

	// Apply defaults and validate
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, errs.Wrap(err, "CreateGraphIngest", "factory", "config validation")
	}

	// Create logger with component context
	logger := deps.GetLoggerWithComponent("graph-ingest")

	// Create component
	comp := &Component{
		name:                          "graph-ingest",
		config:                        config,
		decoder:                       message.NewDecoder(deps.PayloadRegistry),
		natsClient:                    natsClient,
		logger:                        logger,
		entitiesUpdated:               getEntitiesUpdatedMetric(deps.MetricsRegistry),
		indexingProfileDefault:        getIndexingProfileDefaultMetric(deps.MetricsRegistry),
		mutationRejections:            getMutationRejectionsMetric(deps.MetricsRegistry),
		predicateContractRejections:   getPredicateContractRejectionsMetric(deps.MetricsRegistry),
		entityStateContractRejections: getEntityStateContractRejectionsMetric(deps.MetricsRegistry),
		stubRestamps:                  getStubRestampsMetric(deps.MetricsRegistry),
		foreignEdgeUnclaimed:          getForeignEdgeUnclaimedMetric(deps.MetricsRegistry),
		foreignEdgeDropped:            getForeignEdgeDroppedMetric(deps.MetricsRegistry),
		ownerLeaseMismatch:            getOwnerLeaseMismatchMetric(deps.MetricsRegistry),
		processingDuration:            getProcessingDurationMetric(deps.MetricsRegistry),
		ingestLag:                     getIngestLagMetric(deps.MetricsRegistry),
		redeliveriesDropped:           getRedeliveriesDroppedMetric(deps.MetricsRegistry),
		casRetries:                    getCasRetriesMetric(deps.MetricsRegistry),
		metricsRegistry:               deps.MetricsRegistry,
	}

	// Initialize last activity
	comp.lastActivity.Store(time.Now())

	return comp, nil
}

// Register registers the graph-ingest factory with the component registry
func Register(registry *component.Registry) error {
	return registry.RegisterFactory("graph-ingest", &component.Registration{
		Name:        "graph-ingest",
		Type:        "processor",
		Protocol:    "nats",
		Domain:      "graph",
		Description: "Entity and triple ingestion processor",
		Version:     "1.0.0",
		Schema:      schema,
		Factory:     CreateGraphIngest,
	})
}

// ============================================================================
// Discoverable Interface (6 methods)
// ============================================================================

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "graph-ingest",
		Type:        "processor",
		Description: "Entity and triple ingestion processor for graph system",
		Version:     "1.0.0",
	}
}

// InputPorts returns input port definitions.
// Reads directly from config so ports are available before Initialize().
func (c *Component) InputPorts() []component.Port {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, 0, len(c.config.Ports.Inputs))
	for _, portDef := range c.config.Ports.Inputs {
		ports = append(ports, component.BuildPortFromDefinition(portDef, component.DirectionInput))
	}
	return ports
}

// OutputPorts returns output port definitions.
// Reads directly from config so ports are available before Initialize().
func (c *Component) OutputPorts() []component.Port {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.config.Ports == nil {
		return []component.Port{}
	}

	ports := make([]component.Port, 0, len(c.config.Ports.Outputs))
	for _, portDef := range c.config.Ports.Outputs {
		ports = append(ports, component.BuildPortFromDefinition(portDef, component.DirectionOutput))
	}
	return ports
}

// ConfigSchema returns the configuration schema
func (c *Component) ConfigSchema() component.ConfigSchema {
	return schema
}

// Health returns current health status
func (c *Component) Health() component.HealthStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	uptime := time.Duration(0)
	if c.running && !c.startTime.IsZero() {
		uptime = time.Since(c.startTime)
	}

	errorCount := int(atomic.LoadInt64(&c.errors))
	lastErr := ""
	status := "stopped"

	if c.running {
		status = "running"
		if state := c.entityStatePoison.Load(); state != nil {
			status = graph.IndexStateResetRequired
			lastErr = state.Error()
		} else if c.entityWatchLost.Load() {
			status = graph.IndexStateDegraded
			lastErr = graph.ErrorCodeIndexNotReady + ": ENTITY_STATES contract watcher unavailable"
		} else if errorCount > 0 {
			lastErr = "errors occurred during processing"
		}
	}

	return component.HealthStatus{
		Healthy: c.running && errorCount == 0 && c.entityStatePoison.Load() == nil &&
			!c.entityWatchLost.Load() && (!c.entityBootstrapStarted.Load() || c.entityBootstrapComplete.Load()),
		LastCheck:  time.Now(),
		ErrorCount: errorCount,
		LastError:  lastErr,
		Uptime:     uptime,
		Status:     status,
	}
}

// DataFlow returns current data flow metrics
func (c *Component) DataFlow() component.FlowMetrics {
	messages := atomic.LoadInt64(&c.messagesProcessed)
	bytes := atomic.LoadInt64(&c.bytesProcessed)
	errorCount := atomic.LoadInt64(&c.errors)

	c.mu.RLock()
	uptime := time.Duration(0)
	if c.running && !c.startTime.IsZero() {
		uptime = time.Since(c.startTime)
	}
	c.mu.RUnlock()

	// Calculate rates
	var messagesPerSec, bytesPerSec, errorRate float64
	if uptime > 0 {
		seconds := uptime.Seconds()
		messagesPerSec = float64(messages) / seconds
		bytesPerSec = float64(bytes) / seconds
		if messages > 0 {
			errorRate = float64(errorCount) / float64(messages)
		}
	}

	lastAct := time.Now()
	if stored := c.lastActivity.Load(); stored != nil {
		if t, ok := stored.(time.Time); ok {
			lastAct = t
		}
	}

	return component.FlowMetrics{
		MessagesPerSecond: messagesPerSec,
		BytesPerSecond:    bytesPerSec,
		ErrorRate:         errorRate,
		LastActivity:      lastAct,
	}
}

// ============================================================================
// LifecycleComponent Interface (3 methods)
// ============================================================================

// Initialize validates configuration and sets up ports (no I/O)
func (c *Component) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.initialized {
		return nil // Idempotent
	}

	// Validate configuration
	if err := c.config.Validate(); err != nil {
		return errs.Wrap(err, "Component", "Initialize", "config validation")
	}

	c.initialized = true
	c.logger.Info("component initialized", slog.String("component", "graph-ingest"))

	return nil
}

// Start begins processing (must be initialized first)
func (c *Component) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Component", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Component", "Start", "context already cancelled")
	}

	// Check initialization
	if !c.initialized {
		return errs.WrapFatal(fmt.Errorf("component not initialized"), "Component", "Start", "initialization check")
	}

	// Idempotent - already running
	if c.running {
		return nil
	}

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Check context before proceeding
	if err := ctx.Err(); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "context cancelled")
	}

	// Ensure NATS client is connected
	if c.natsClient.Status() != natsclient.StatusConnected {
		if err := c.natsClient.Connect(ctx); err != nil {
			cancel()
			// Check if this is a context-related error
			if ctx.Err() != nil {
				return errs.Wrap(ctx.Err(), "Component", "Start", "context cancelled during NATS connection")
			}
			return errs.Wrap(err, "Component", "Start", "NATS connection failed")
		}
		if err := c.natsClient.WaitForConnection(ctx); err != nil {
			cancel()
			if ctx.Err() != nil {
				return errs.Wrap(ctx.Err(), "Component", "Start", "context cancelled waiting for NATS")
			}
			return errs.Wrap(err, "Component", "Start", "wait for NATS connection")
		}
	}

	// Initialize storage buckets and query caches
	if err := c.initStorage(ctx); err != nil {
		cancel()
		return err
	}

	// Query safety is independent from graph-ingest's write role. Drain the
	// authoritative snapshot synchronously and keep the same watcher live. A
	// watcher transport failure degrades only queries; ingest writers still boot
	// so operators can repair state through canonical writes before restart.
	c.startEntityStateGuard(ctx, c.entityStateBucket)

	// Initialize lifecycle reporter (throttled for high-throughput ingestion)
	c.initLifecycleReporter(ctx)

	// Initialize hierarchy inference if enabled (synchronous - no Start/Stop)
	c.initHierarchyInference()

	// Build the keyed-concurrent ingest pool BEFORE subscriptions start (ADR-072
	// M3: else the first delivered message submits to a nil pool).
	if err := c.buildIngestPool(); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "keyed ingest pool")
	}

	// Set up subscriptions for input ports. On any boot failure past
	// buildIngestPool, tear down the pool too: Stop early-returns when
	// !running, so without this the lane goroutines + metricsUpdater leak.
	if err := c.setupSubscriptions(ctx); err != nil {
		c.teardownIngestPool()
		cancel()
		return errs.Wrap(err, "Component", "Start", "subscription setup")
	}

	// Set up query handler subscriptions
	if err := c.setupQueryHandlers(ctx); err != nil {
		c.teardownIngestPool()
		cancel()
		return errs.Wrap(err, "Component", "Start", "query handler setup")
	}

	// Set up mutation handler subscriptions (for rule processor actions)
	if err := c.setupMutationHandlers(ctx); err != nil {
		c.teardownIngestPool()
		cancel()
		return errs.Wrap(err, "Component", "Start", "mutation handler setup")
	}

	// Mark as running
	c.running = true
	c.startTime = time.Now()

	// Report initial idle state
	if err := c.lifecycleReporter.ReportStage(ctx, "idle"); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "idle"), slog.Any("error", err))
	}

	c.logger.Info("component started",
		slog.String("component", "graph-ingest"),
		slog.Time("start_time", c.startTime))

	return nil
}

// Stop gracefully shuts down the component
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil // Already stopped
	}
	c.running = false
	pool := c.ingestPool
	submitCancel := c.ingestSubmitCancel
	poolCancel := c.ingestPoolCancel
	consumeCancel := c.cancel
	entityStateWatcher := c.entityStateWatcher
	c.entityStateWatcher = nil
	c.mu.Unlock()

	// ADR-072 M3 shutdown ordering, BEFORE tearing down KV/NATS:
	//  1. cancel the submit ctx so a consume callback parked in SubmitBlocking
	//     unblocks and Naks (else the synchronous consumer callback wedges
	//     teardown until timeout);
	//  2. stop the consumer so no new work is submitted;
	//  3. drain the pool — Process still runs on the live, independent pool ctx;
	//  4. release the pool ctx.
	if submitCancel != nil {
		submitCancel()
	}
	if consumeCancel != nil {
		consumeCancel()
	}
	if entityStateWatcher != nil {
		_ = entityStateWatcher.Stop()
	}
	if pool != nil {
		drainCtx, cancelDrain := context.WithTimeout(context.Background(), timeout)
		if err := pool.Stop(drainCtx); err != nil {
			c.logger.Warn("ingest pool drain incomplete", slog.Any("error", err))
		}
		cancelDrain()
	}
	if poolCancel != nil {
		poolCancel()
	}

	// Teardown query/mutation subscriptions + caches now that the pool is drained.
	c.mu.Lock()
	for _, sub := range c.subscriptions {
		if sub != nil {
			if err := sub.Unsubscribe(); err != nil {
				c.logger.Warn("subscription unsubscribe error", slog.Any("error", err))
			}
		}
	}
	c.subscriptions = nil
	if c.entityCache != nil {
		if err := c.entityCache.Close(); err != nil {
			c.logger.Warn("entity cache close error", slog.Any("error", err))
		}
	}
	if c.suffixCache != nil {
		if err := c.suffixCache.Close(); err != nil {
			c.logger.Warn("suffix cache close error", slog.Any("error", err))
		}
	}
	c.mu.Unlock()

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.logger.Info("component stopped gracefully", slog.String("component", "graph-ingest"))
		return nil
	case <-time.After(timeout):
		c.logger.Warn("component stop timed out", slog.String("component", "graph-ingest"))
		return fmt.Errorf("stop timeout after %v", timeout)
	}
}

// initStorage initializes KV buckets and query caches.
func (c *Component) initStorage(ctx context.Context) error {
	// Entity states KV bucket (create if not exists) - we are the WRITER
	bucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketEntityStates,
		Description: "Entity state storage for graph-ingest",
	})
	if err != nil {
		return errs.Wrap(err, "Component", "Start", "KV bucket creation")
	}
	c.entityBucket = c.natsClient.NewKVStore(bucket)
	c.entityStateBucket = bucket

	// D1 guardrail (ADR-068): ENTITY_STATES is get-or-create, so another process
	// could have won the race with a TTL/size cap (e.g. a stale graph-query TTL
	// default). Age/size eviction is reachability-blind and would silently expire
	// entities with live inbound edges. Fail-closed at boot rather than proceed.
	if err := c.entityBucket.AssertNoLifecycleRetention(ctx, graph.BucketEntityStates); err != nil {
		return errs.Wrap(err, "Component", "Start", "graph retention guardrail")
	}

	// Entity query cache (HybridCache: LRU capacity + TTL freshness)
	entityCache, err := cache.NewFromConfig[graph.EntityState](ctx, cache.Config{
		Enabled:         true,
		Strategy:        cache.StrategyHybrid,
		MaxSize:         5000,
		TTL:             30 * time.Second,
		CleanupInterval: 10 * time.Second,
	}, cache.WithMetrics[graph.EntityState](c.metricsRegistry, "entity_query_cache"))
	if err != nil {
		return errs.Wrap(err, "Component", "Start", "entity cache creation")
	}
	c.entityCache = entityCache

	// Suffix index KV bucket for fast suffix→fullID resolution
	suffixBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "ENTITY_SUFFIX_INDEX",
		Description: "Suffix-to-full-ID reverse index for partial entity ID resolution",
	})
	if err != nil {
		return errs.Wrap(err, "Component", "Start", "suffix index bucket creation")
	}
	c.suffixBucket = c.natsClient.NewKVStore(suffixBucket)

	// Suffix resolution cache (stable mappings, long TTL)
	suffixCacheInst, err := cache.NewFromConfig[string](ctx, cache.Config{
		Enabled:         true,
		Strategy:        cache.StrategyTTL,
		MaxSize:         500,
		TTL:             5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}, cache.WithMetrics[string](c.metricsRegistry, "suffix_resolution_cache"))
	if err != nil {
		return errs.Wrap(err, "Component", "Start", "suffix cache creation")
	}
	c.suffixCache = suffixCacheInst

	// ADR-072 redelivery-guard durable tier: graph-ingest-owned bucket mapping
	// `(entityID/streamName) → last-applied stream sequence`. Operational state
	// graph-ingest owns (bucket-ownership rubric), a bare uint64 value — no
	// cross-repo/EntityState schema change. No TTL: correct for MaxDeliver=0
	// (unlimited redelivery), and the key set is bounded by entity cardinality
	// (same order as ENTITY_STATES).
	guardBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      graphIngestGuardBucket,
		Description: "graph-ingest redelivery guard: (entityID/streamName) -> last-applied stream sequence (ADR-072)",
	})
	if err != nil {
		return errs.Wrap(err, "Component", "Start", "redelivery guard bucket creation")
	}
	c.ingestGuardBucket = c.natsClient.NewKVStore(guardBucket)

	// The guard bucket is correctness-critical no-eviction state (ADR-072 B2/B3):
	// TTL or size-eviction of a sequence stamp silently reopens the restart/
	// cache-eviction overwrite this guard closes. CreateKeyValueBucket returns an
	// existing bucket as-is, so a stale/foreign deploy could have created it with
	// a retention policy — fail-closed at boot, exactly like ENTITY_STATES.
	if err := c.ingestGuardBucket.AssertNoLifecycleRetention(ctx, graphIngestGuardBucket); err != nil {
		return errs.Wrap(err, "Component", "Start", "redelivery guard retention guardrail")
	}

	// ADR-056 Decision-4 T2-seam: open OWNER_CLAIMS read-only to classify
	// foreign-subject edges against registered ForeignEdgeClaims. graph-ingest
	// boots AFTER main's EnsureBuckets, so in an ownership-enabled deploy the
	// bucket exists. If it is absent (resourceless / unmigrated deploy) or
	// transiently unopenable, graceful-skip: leave claimReader nil and the seam
	// routes without classifying (observe-only, no behavior change).
	if reader, err := ownership.NewClaimReader(ctx, c.natsClient, c.logger); err != nil {
		c.logger.Info("graph-ingest: ownership claim reader unavailable — foreign-edge classification disabled this boot (observe-only seam skipped)",
			slog.Any("error", err))
	} else {
		c.claimReader = reader
	}

	return nil
}

// startEntityStateGuard synchronously validates the current ENTITY_STATES
// snapshot and then continues on the same WatchAll stream. It intentionally
// does not fail graph-ingest startup on transport errors: this component is the
// canonical writer and must remain available, while every query fails closed.
func (c *Component) startEntityStateGuard(ctx context.Context, bucket jetstream.KeyValue) {
	c.entityBootstrapStarted.Store(true)
	watcher, err := bucket.WatchAll(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.markEntityWatchLost()
			c.logger.Error("failed to start ENTITY_STATES query contract watcher", slog.Any("error", err))
		}
		return
	}
	c.entityStateWatcher = watcher

	updates := watcher.Updates()
	for {
		select {
		case <-ctx.Done():
			_ = watcher.Stop()
			return
		case entry, ok := <-updates:
			if !ok {
				_ = watcher.Stop()
				if ctx.Err() == nil {
					c.markEntityWatchLost()
				}
				return
			}
			if entry == nil {
				c.entityBootstrapComplete.Store(true)
				c.wg.Add(1)
				go c.runEntityStateGuard(ctx, watcher, updates)
				return
			}
			c.validateEntityStateGuardEntry(entry)
		}
	}
}

func (c *Component) runEntityStateGuard(
	ctx context.Context,
	watcher jetstream.KeyWatcher,
	updates <-chan jetstream.KeyValueEntry,
) {
	defer c.wg.Done()
	for {
		select {
		case <-ctx.Done():
			_ = watcher.Stop()
			return
		case entry, ok := <-updates:
			if !ok {
				_ = watcher.Stop()
				if ctx.Err() == nil {
					c.markEntityWatchLost()
				}
				return
			}
			if entry != nil {
				c.validateEntityStateGuardEntry(entry)
			}
		}
	}
}

func (c *Component) validateEntityStateGuardEntry(entry jetstream.KeyValueEntry) {
	if graph.IsKVTombstone(entry.Operation()) {
		return
	}
	var state graph.EntityState
	if err := graph.UnmarshalEntityState(entry.Value(), &state); err != nil {
		var contractErr *graph.StateContractError
		if !errors.As(err, &contractErr) {
			contractErr = &graph.StateContractError{Reason: graph.GraphStateReasonUnreadableEntity, Err: err}
		}
		latched := c.latchEntityStatePoison(contractErr)
		if latched {
			c.logger.Error("authoritative graph state requires reset; graph-ingest queries blocked",
				slog.String("code", graph.ErrorCodeGraphStateResetRequired),
				slog.String("reason", string(contractErr.Reason)))
		}
	}
}

func (c *Component) latchEntityStatePoison(contractErr *graph.StateContractError) bool {
	c.entityQueryMu.Lock()
	latched := c.entityStatePoison.CompareAndSwap(nil, contractErr)
	c.entityQueryMu.Unlock()
	return latched
}

// classifyStoredStateRMWError re-attributes a read-modify-write failure to
// resident stored-state poison (gh#562). The owner's own RMW reads use
// graph.UnmarshalEntityStateTrusted, so noncanonical STORED state no longer
// fails on the read — it surfaces either as a trusted-decode failure
// (unreadable JSON) or as a MarshalEntityState write-gate rejection that
// would otherwise blame the merged CANDIDATE the caller submitted. On this
// slow path (the write is already failing) re-validate the stored bytes: when
// the poison predates the merge, return the graph-state-reset-required
// classification (mirroring the ENTITY_STATES contract guard) instead of a
// candidate-invalid error. A canonical stored state returns cycleErr
// unchanged — the candidate really was at fault.
//
// Classification only — latching stays with its existing owners (the ingest
// lane's processIngest, the contract guard watcher, and the query lane), so
// their latch-once logging semantics are preserved.
func (c *Component) classifyStoredStateRMWError(current []byte, cycleErr error) error {
	var stored graph.EntityState
	if err := graph.UnmarshalEntityState(current, &stored); err != nil {
		return err
	}
	return cycleErr
}

func (c *Component) markEntityWatchLost() {
	c.entityQueryMu.Lock()
	c.entityWatchLost.Store(true)
	c.entityQueryMu.Unlock()
}

// initLifecycleReporter initializes the lifecycle reporter for component status tracking.
func (c *Component) initLifecycleReporter(ctx context.Context) {
	statusBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      "COMPONENT_STATUS",
		Description: "Component lifecycle status tracking",
	})
	if err != nil {
		c.logger.Warn("Failed to create COMPONENT_STATUS bucket, lifecycle reporting disabled",
			slog.Any("error", err))
		c.lifecycleReporter = component.NewNoOpLifecycleReporter()
		return
	}
	c.lifecycleReporter = component.NewLifecycleReporterFromConfig(component.LifecycleReporterConfig{
		KV:               statusBucket,
		ComponentName:    "graph-ingest",
		Logger:           c.logger,
		EnableThrottling: true,
	})
}

// initHierarchyInference initializes hierarchy inference if enabled.
func (c *Component) initHierarchyInference() {
	if !c.config.EnableHierarchy {
		return
	}

	// Enable sibling edges by default, can be disabled via config
	enableTypeSiblings := true
	if c.config.EnableTypeSiblings != nil {
		enableTypeSiblings = *c.config.EnableTypeSiblings
	}

	hierarchyConfig := inference.HierarchyConfig{
		Enabled:            true,
		CreateTypeEdges:    true,
		CreateSystemEdges:  true,
		CreateDomainEdges:  true,
		CreateTypeSiblings: enableTypeSiblings,
	}

	c.hierarchyInference = inference.NewHierarchyInference(
		&entityManagerAdapter{component: c},
		&tripleAdderAdapter{component: c},
		hierarchyConfig,
		c.logger,
	)
}

// ============================================================================
// Subscription Management
// ============================================================================

// setupSubscriptions sets up JetStream consumers for input ports
func (c *Component) setupSubscriptions(ctx context.Context) error {
	for _, port := range c.config.Ports.Inputs {
		if port.Type != "jetstream" {
			c.logger.Debug("skipping non-jetstream port", slog.String("port", port.Name), slog.String("type", port.Type))
			continue
		}

		if err := c.setupJetStreamConsumer(ctx, port); err != nil {
			return errs.Wrap(err, "Component", "setupSubscriptions",
				fmt.Sprintf("JetStream consumer for %s", port.Subject))
		}
	}
	return nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (c *Component) setupJetStreamConsumer(ctx context.Context, port component.PortDefinition) error {
	// Derive stream name from subject
	streamName := port.StreamName
	if streamName == "" {
		streamName = c.deriveStreamName(port.Subject)
	}
	if streamName == "" {
		return fmt.Errorf("could not derive stream name for subject %s", port.Subject)
	}

	// Wait for stream to be available
	if err := c.waitForStream(ctx, streamName); err != nil {
		return fmt.Errorf("stream %s not available: %w", streamName, err)
	}

	// Generate unique consumer name
	sanitizedSubject := strings.ReplaceAll(port.Subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("graph-ingest-%s", sanitizedSubject)

	c.logger.Debug("Setting up JetStream consumer",
		slog.String("stream", streamName),
		slog.String("consumer", consumerName),
		slog.String("filter_subject", port.Subject))

	// Get consumer config from port definition (allows user configuration)
	// graph-ingest is idempotent (ENTITY_STATES merge/CAS overwrites), so it
	// defaults to "all": it MUST catch up on entities published before its
	// consumer bound. A JSON config omitting deliver_policy would otherwise fall
	// to the framework "new" default and silently drop the first entities (the
	// startup first-message race). An explicit deliver_policy still wins.
	consumerCfg := component.GetConsumerConfigFromDefinitionWithDefault(port, "all")

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: port.Subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		MaxAckPending: consumerCfg.MaxAckPending, // gh#480 backpressure knob (0 = server default)
		AutoCreate:    false,
	}

	subject := port.Subject // capture for closure
	err := c.natsClient.ConsumeStreamWithConfig(ctx, cfg, func(_ context.Context, msg jetstream.Msg) {
		// ADR-072: the consume closure decodes ONCE and submits to the keyed pool;
		// the redelivery guard, apply, and ack move into processIngest (run on a
		// pool lane, not this consumer goroutine). Metadata carries the stream name
		// + sequence the guard keys on; a message without it can't be keyed/guarded.
		meta, metaErr := msg.Metadata()
		if metaErr != nil {
			c.logger.Warn("graph-ingest: message missing JetStream metadata; dropping",
				slog.String("subject", subject), slog.Any("error", metaErr))
			atomic.AddInt64(&c.errors, 1)
			if ackErr := msg.Ack(); ackErr != nil {
				c.logger.Error("Failed to ack metadata-less message", slog.Any("error", ackErr))
			}
			return
		}
		// gh#480 queue-wait (message age when we reach it). Host clock skew can make
		// this marginally negative on a co-located deploy — harmless (skews _sum only).
		if !meta.Timestamp.IsZero() {
			c.ingestLag.Observe(time.Since(meta.Timestamp).Seconds())
		}
		// Decode + extract ONCE (KeyOf reads the already-parsed entity.ID; the lane
		// reuses the parsed entity — no double parse). A decode/extract failure is a
		// poison message: count + ack-drop (today's behavior).
		entity, derr := c.decodeEntity(subject, msg.Data())
		if derr != nil {
			c.logger.Warn("graph-ingest: decode/extract failed; dropping",
				slog.String("subject", subject), slog.Any("error", derr))
			atomic.AddInt64(&c.errors, 1)
			if ackErr := msg.Ack(); ackErr != nil {
				c.logger.Error("Failed to ack poison message", slog.Any("error", ackErr))
			}
			return
		}
		// Submit on the dedicated submit ctx (NOT msgCtx — a block past AckWait would
		// otherwise surface as an ignored error → message neither enqueued nor acked).
		// A submit failure MUST Nak so the server redelivers (ADR-072 B1), never
		// silent-drop.
		work := ingestWork{
			entity:   entity,
			msg:      msg,
			entityID: entity.ID,
			stream:   meta.Stream,
			seq:      meta.Sequence.Stream,
		}
		if serr := c.ingestPool.SubmitBlocking(c.ingestSubmitCtx, work); serr != nil {
			if nakErr := msg.Nak(); nakErr != nil {
				c.logger.Error("Failed to Nak after submit failure", slog.Any("error", nakErr))
			}
		}
	})
	if err != nil {
		return fmt.Errorf("consumer setup failed for stream %s: %w", streamName, err)
	}

	c.logger.Debug("graph-ingest subscribed (JetStream)",
		slog.String("subject", subject),
		slog.String("stream", streamName))
	return nil
}

// deriveStreamName derives a stream name from a subject pattern
func (c *Component) deriveStreamName(subject string) string {
	// Common mappings based on subject prefix
	prefixToStream := map[string]string{
		"sensor.":      "SENSOR",
		"objectstore.": "OBJECTSTORE",
		"entity.":      "ENTITY",
		"events.":      "EVENTS",
	}

	for prefix, stream := range prefixToStream {
		if strings.HasPrefix(subject, prefix) {
			return stream
		}
	}

	// Default: use first segment uppercased
	parts := strings.Split(subject, ".")
	if len(parts) > 0 {
		return strings.ToUpper(parts[0])
	}
	return ""
}

// waitForStream waits for a JetStream stream to be available
func (c *Component) waitForStream(ctx context.Context, streamName string) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return fmt.Errorf("failed to get JetStream context: %w", err)
	}

	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := js.Stream(ctx, streamName)
		if err == nil {
			c.logger.Debug("Stream available", slog.String("stream", streamName))
			return nil
		}

		// Exponential backoff
		c.logger.Debug("Waiting for stream",
			slog.String("stream", streamName),
			slog.Int("attempt", i+1),
			slog.Duration("interval", retryInterval))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryInterval):
			retryInterval = time.Duration(float64(retryInterval) * 1.5)
			if retryInterval > maxInterval {
				retryInterval = maxInterval
			}
		}
	}

	return fmt.Errorf("stream %s not available after %d retries", streamName, maxRetries)
}

// handleMessage processes an incoming message and creates/updates entity state
func (c *Component) handleMessage(ctx context.Context, subject string, data []byte) {
	// Report processing stage (throttled to avoid KV spam)
	if err := c.lifecycleReporter.ReportStage(ctx, "processing"); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "processing"), slog.Any("error", err))
	}

	c.logger.Debug("Received message",
		slog.String("subject", subject),
		slog.Int("size", len(data)))

	entity, err := c.decodeEntity(subject, data)
	if err != nil {
		c.logger.Warn("Failed to decode/extract message",
			slog.String("subject", subject),
			slog.Any("error", err))
		atomic.AddInt64(&c.errors, 1)
		return
	}

	_ = c.ingestEntity(ctx, entity)
}

// decodeEntity decodes a message's bytes into an EntityState: unmarshal the
// BaseMessage envelope, then extract the Graphable payload's entity. Shared by
// the consume closure (decode-once, then submit to the keyed pool) and
// handleMessage (the synchronous test/compat path).
func (c *Component) decodeEntity(subject string, data []byte) (*graph.EntityState, error) {
	baseMsg, err := c.decoder.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("decode base message (subject %s): %w", subject, err)
	}
	entity, err := c.extractEntityFromMessage(baseMsg)
	if err != nil {
		return nil, fmt.Errorf("extract entity (subject %s): %w", subject, err)
	}
	return entity, nil
}

// ingestEntity merges an extracted Graphable entity into ENTITY_STATES and
// regroups any cross-entity edges onto their own subjects (ADR-055 §5 T2). It is
// the orchestration the Fact-arrival consumer runs once decode+extract have
// produced an EntityState: split foreign-subject edges off the primary, merge
// the primary (envelope-bearing), then route the edges via the evidence-append
// path. Exposed as a method so the merge+regroup wire is testable end-to-end
// without standing up the decoder.
func (c *Component) ingestEntity(ctx context.Context, entity *graph.EntityState) error {
	if err := c.prepareFactProjection(entity); err != nil {
		return err
	}

	// ADR-055 §5 T2 + ADR-056 Decision 4: a Graphable may legitimately emit
	// cross-entity edges whose Subject != EntityID() (sensorml inverse isHostedBy,
	// federation/objectstore pass-throughs); extractEntityFromMessage files every
	// triple under the primary key. Normalize at the shared write boundary (split
	// the foreign-subject edges off the primary + classify them), merge only the
	// primary's own facts (envelope-bearing), then route the foreign edges onto
	// their own subjects below.
	own, foreign := c.normalizeProjection(ctx, entity.ID, entity.MessageType, entity.Triples)
	entity.Triples = own

	// Structural-identity predicate gate on the Graphable ingest path
	// (unconditionally fail-closed; the primary vector for product/source-authored
	// predicates). On
	// this lane it is a defense-in-depth backstop: prepareFactProjection above has
	// already rejected any non-canonical predicate via the authoritative
	// ValidateEntityStateContract, so this call cannot fire unless that seam moves.
	if err := c.validateTriplePredicates(entity.Triples); err != nil {
		return err
	}

	// Store entity in KV bucket — MERGE semantics (gh#177). Earlier code
	// used CreateEntity (Put = full-replace) here, which clobbered any
	// pre-existing triples on the entity. The atomic mutation handlers
	// (create_with_triples, update_with_triples, triple.add) all merge;
	// the jetstream consumer path was the lone outlier and silently
	// erased lifecycle-managed entity state on every subsequent
	// Graphable arrival.
	if err := c.MergeEntity(ctx, entity); err != nil {
		c.logger.Error("Failed to merge entity",
			slog.String("entity_id", entity.ID),
			slog.Any("error", err))
		return err
	}

	c.routeForeignEdges(ctx, entity.ID, entity.MessageType, foreign)

	c.logger.Debug("Entity ingested",
		slog.String("entity_id", entity.ID),
		slog.Int("triples", len(entity.Triples)))
	return nil
}

// prepareFactProjection applies the Graphable lane's single projection
// convenience and validates the complete candidate. processIngest calls this
// before its redelivery guard so malformed identity cannot become a guard key
// or trigger guard I/O; ingestEntity calls it as the reusable direct-entry
// safety net. The operation is idempotent.
func (c *Component) prepareFactProjection(entity *graph.EntityState) error {
	if entity == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "ingestEntity", "entity cannot be nil")
	}
	if err := validateEntityID(entity.ID); err != nil {
		return errs.WrapInvalid(&graph.EntityStateContractError{
			Field:       graph.EntityStateContractFieldID,
			TripleIndex: -1,
			Err:         err,
		}, "Component", "ingestEntity", "validate envelope entity ID")
	}

	// Graphable projection convenience: an omitted subject means the envelope
	// entity. Fill only this fact-arrival lane, before the authoritative seam;
	// mutation, direct persistence, and replay receive no such fill.
	entity.Triples = fillFactProjectionSubjects(entity.ID, entity.Triples)

	// Preflight the complete Graphable projection before splitting foreign
	// subjects. This guarantees a malformed foreign edge cannot commit the
	// primary entity first.
	if err := graph.ValidateEntityStateContract(entity); err != nil {
		return errs.WrapInvalid(err, "Component", "ingestEntity", "validate projected entity state")
	}
	return nil
}

// fillFactProjectionSubjects returns triples with each omitted Subject filled
// from the exact Graphable envelope ID. It copies only when a fill is needed
// and never repairs or rewrites non-empty subject bytes.
func fillFactProjectionSubjects(entityID string, triples []message.Triple) []message.Triple {
	var filled []message.Triple
	for index := range triples {
		if triples[index].Subject != "" {
			continue
		}
		if filled == nil {
			filled = append([]message.Triple(nil), triples...)
		}
		filled[index].Subject = entityID
	}
	if filled != nil {
		return filled
	}
	return triples
}

// normalizeProjection is the SHARED projection-normalization seam (ADR-056
// Decision 4): EVERY graph-ingest write path that accepts a set of projected
// triples — fact-arrival ingestEntity AND the mutation API create_with_triples /
// update_with_triples — passes them through here before committing to
// ENTITY_STATES, so foreign-edge enforcement is LANE-INDEPENDENT (not
// fact-arrival-only; the mutation API is a graph write API, not a bypass). It
// splits the triples against their primary subject, classifies the foreign-
// subject edges against the registered ForeignEdgeClaims (observe-only — the
// foreign_edge_unclaimed_total metric + once-WARN; see classifyForeignEdges),
// and returns the own-subject triples to commit on the primary plus the foreign
// edges for the caller to route via routeForeignEdges AFTER the primary commit
// (so a failed primary write never orphans a routed edge).
//
// primaryID is the entity the projected write targets (entity.ID for
// fact-arrival, req.Entity.ID for the mutation API). mt is the producer identity
// (entity.MessageType) for the claim lookup + metric label. For all current
// callers foreign is empty (single-subject batches; cs-api's singleSubject guard
// is a migration guard for missing framework support, not the desired shape) —
// so this is a behavioural no-op today and the foundation a foreign-edge producer
// (e.g. cs-api SensorML hierarchies, once it drops singleSubject) builds on.
func (c *Component) normalizeProjection(ctx context.Context, primaryID string, mt message.Type, triples []message.Triple) (own, foreign []message.Triple) {
	own, foreign = partitionTriplesBySubject(primaryID, triples)
	if len(foreign) > 0 {
		c.logger.Warn("projected write emitted cross-entity edges; regrouping onto their subjects",
			slog.String("primary_id", primaryID),
			slog.Int("foreign_triples", len(foreign)),
			slog.Any("foreign_subjects", distinctSubjects(foreign)))
		c.classifyForeignEdges(ctx, mt, foreign)
	}
	return own, foreign
}

const (
	// invalidMessageTypeLabel bounds the metric label for a producer that
	// registered no/zero MessageType — it cannot match an exact-producer claim,
	// so it gets a single bounded label rather than smearing cardinality.
	invalidMessageTypeLabel = "_invalid"
	// dropReasonStrictAbsent — an EdgeStrict foreign edge dropped because its
	// target was absent (the target must pre-exist by causal ordering).
	dropReasonStrictAbsent = "strict_absent_target"
	// dropReasonConditionalDeferred — an EdgeConditional/EdgeBackfill foreign
	// edge routed-with-warn pending the PENDING_EDGES buffer increment.
	dropReasonConditionalDeferred = "conditional_deferred"
)

// foreignTargetExists reports whether a foreign-edge target entity already
// exists, so routeForeignEdges can decide stub-vs-append-vs-drop WITHOUT relying
// on AddTriples' auto-vivify else-branch (which the ADR-055 must-exist flip
// later removes). A transient read error fails toward the LESS destructive
// outcome — treat as "exists" so the edge is APPENDED (matching today's
// best-effort behaviour) rather than DROPPED; a Strict drop on a read blip would
// lose a legitimately-present edge. TOCTOU note: a concurrent DeleteEntity
// between this Get and the AddTriples append is benign WHILE auto-vivify is
// present (the append re-vivifies); the flip increment makes the post-check
// append must-exist and owns closing that window.
func (c *Component) foreignTargetExists(ctx context.Context, subject string) bool {
	if _, err := c.entityBucket.Get(ctx, subject); err == nil {
		return true
	} else if natsclient.IsKVNotFoundError(err) {
		return false
	}
	return true
}

// routeForeignEdges regroups foreign-subject edges onto their own subjects after
// the primary commit (ADR-056 Decision 4). It is best-effort and called AFTER
// the primary write succeeds — a failed edge is logged, not fatal to the primary
// write that already landed.
//
// 4c-pre-2 replaces the old bare-AddTriples append with a per-edge must-exist
// policy: for an absent target it branches on the covering ForeignEdgeClaim's
// EdgeMode — NoBirthStub materialises the framework's referential-integrity stub
// then appends (the only thing that ever births the sensorml-child target;
// load-bearing especially on the update_with_triples lane, which — unlike the
// fact-arrival/create_with_triples lanes — does NOT run ensureRelationshipTargetsExist
// upstream); Strict drops the edge loudly (metric + one-time WARN); an UNCLAIMED
// edge stays routed deprecated-on-arrival so the hatch-empty flip-gate can still
// drain it; Conditional/Backfill are deferred to a later increment (PENDING_EDGES
// buffer) and route-with-warn in the interim. A present target always appends,
// regardless of mode. When no claim reader is wired (resourceless/unmigrated
// deploy) the seam graceful-skips to the legacy bare append.
//
// mt is the producer identity (entity.MessageType) for the claim lookup + metric
// label. AddTriples validates the batch all-or-nothing, so one malformed edge
// drops the whole appended batch; no current producer emits one.
func (c *Component) routeForeignEdges(ctx context.Context, primaryID string, mt message.Type, foreign []message.Triple) {
	if len(foreign) == 0 {
		return
	}

	// Graceful-skip: with no registered-claim view we cannot resolve modes, so
	// fall back to the legacy bare append (preserves the observe-only contract on
	// a resourceless/unmigrated deploy).
	if c.claimReader == nil {
		c.appendForeignEdges(ctx, primaryID, foreign)
		return
	}

	label := mt.Key()
	if !mt.IsValid() {
		label = invalidMessageTypeLabel
	}

	toAppend := make([]message.Triple, 0, len(foreign))
	for _, edge := range foreign {
		// Present target: append regardless of mode. The own existence check
		// makes this correct both before and after the flip removes auto-vivify.
		if c.foreignTargetExists(ctx, edge.Subject) {
			toAppend = append(toAppend, edge)
			continue
		}

		mode, claimed, err := c.claimReader.ForeignEdgeMode(ctx, mt.Key(), edge.Predicate)
		if err != nil {
			// Read blip — fail-open (never block routing), route deprecated-on-arrival.
			c.logger.Warn("graph-ingest: foreign-edge mode lookup failed — routing edge anyway (observe-only fail-open)",
				slog.String("message_type", label), slog.String("predicate", edge.Predicate), slog.Any("error", err))
			toAppend = append(toAppend, edge)
			continue
		}

		switch {
		case !claimed:
			// UNCLAIMED + absent: the deprecated-on-arrival hatch. Stay routed so
			// foreign_edge_unclaimed_total (metered upstream in classifyForeignEdges)
			// can still reach zero over the flip-gate bake window. Post-ADR-055 the
			// append no longer auto-vivifies the absent target — AddTriples rejects
			// it (appendForeignEdges warn-not-fails), so an unclaimed edge to a
			// not-yet-born target is dropped, not silently birthed. Draining this
			// counter to zero is the rollout gate for the must-exist flip.
			toAppend = append(toAppend, edge)
		case mode == ownership.EdgeNoBirthStub:
			// Backstop: materialise the envelope-bearing stub then append. Idempotent
			// (no-op if the upstream ensureRelationshipTargetsExist already won the race).
			if serr := c.ensureReferencedEntityExists(ctx, edge.Subject, primaryID, mt); serr != nil {
				c.logger.Warn("graph-ingest: no-birth-stub materialisation failed — routing edge anyway (target may dangle)",
					slog.String("message_type", label), slog.String("predicate", edge.Predicate),
					slog.String("target", edge.Subject), slog.Any("error", serr))
			}
			toAppend = append(toAppend, edge)
		case mode == ownership.EdgeStrict:
			// Loud-drop: a Strict edge's target must pre-exist by causal ordering.
			c.foreignEdgeDropped.WithLabelValues(label, edge.Predicate, dropReasonStrictAbsent).Inc()
			c.warnForeignEdgeDropOnce(dropReasonStrictAbsent, label, edge)
			// NOT appended.
		case mode == ownership.EdgeConditional || mode == ownership.EdgeBackfill:
			// DEFERRED to the PENDING_EDGES increment. No live producer reaches here
			// today; route-with-warn so it is observable, not silent. Post-ADR-055 the
			// append no longer auto-vivifies an absent target — AddTriples rejects it
			// (warn-not-fails), same as the unclaimed hatch above.
			c.foreignEdgeDropped.WithLabelValues(label, edge.Predicate, dropReasonConditionalDeferred).Inc()
			c.warnForeignEdgeDropOnce(dropReasonConditionalDeferred, label, edge)
			toAppend = append(toAppend, edge)
		default:
			// Unknown/forward-compat mode: treat as hatch, route-with-warn.
			toAppend = append(toAppend, edge)
		}
	}

	c.appendForeignEdges(ctx, primaryID, toAppend)
}

// appendForeignEdges routes a foreign-edge batch onto its subjects via the
// evidence-append path (AddTriples = append-by-subject, no MessageType restamp),
// logging a partial failure without failing the primary write.
func (c *Component) appendForeignEdges(ctx context.Context, primaryID string, foreign []message.Triple) {
	if len(foreign) == 0 {
		return
	}
	if _, failed, aerr := c.AddTriples(ctx, foreign); aerr != nil {
		c.logger.Warn("failed to regroup cross-entity edges onto their subjects",
			slog.String("primary_id", primaryID),
			slog.Int("failed_subjects", len(failed)),
			slog.Any("error", aerr))
	}
}

// warnForeignEdgeDropOnce logs a one-time WARN per (reason, message_type,
// predicate), deduped via foreignEdgeWarnedKeys with a distinct "fe-drop|" prefix
// so it never collides with classifyForeignEdges' unclaimed-WARN keys.
func (c *Component) warnForeignEdgeDropOnce(reason, label string, edge message.Triple) {
	warnKey := "fe-drop|" + reason + "|" + label + "|" + edge.Predicate
	if _, warned := c.foreignEdgeWarnedKeys.LoadOrStore(warnKey, struct{}{}); warned {
		return
	}
	c.logger.Warn("graph-ingest: foreign-subject edge dropped/deferred at routing seam (ADR-056 Decision 4)",
		slog.String("message_type", label),
		slog.String("predicate", edge.Predicate),
		slog.String("target", edge.Subject),
		slog.String("reason", reason))
}

// classifyForeignEdges runs the ADR-056 Decision-4 T2-seam reject in OBSERVE-ONLY
// mode (W0 increment 4a): it classifies the foreign-subject edges' predicates
// against the registered ForeignEdgeClaims for this producer (one epoch read) and
// counts the unclaimed ones on foreign_edge_unclaimed_total, with a one-time WARN
// per (message_type,predicate). It does NOT change routing — the edges are still
// appended by the caller's AddTriples (deprecated-on-arrival). The hard reject +
// the ADR-055 must-exist flip are 4c. No-op when no claim reader is wired
// (graceful-skip). The metric counts edges CLASSIFIED-unclaimed at the seam, NOT
// routing failures (relevant to 4c's hatch-empty gate semantics).
func (c *Component) classifyForeignEdges(ctx context.Context, mt message.Type, foreign []message.Triple) {
	if c.claimReader == nil {
		return
	}
	// Producer key = the dotted registry MessageType. An invalid/zero type (a
	// producer that registered none) cannot match an exact-producer claim; bound
	// its metric label to "_invalid" so a malformed producer can't smear
	// cardinality, but still classify (a Producer-empty claim may cover it).
	producer := mt.Key()
	label := producer
	if !mt.IsValid() {
		label = invalidMessageTypeLabel
	}

	preds := make([]string, 0, len(foreign))
	for _, t := range foreign {
		preds = append(preds, t.Predicate)
	}
	unclaimed, err := c.claimReader.UnclaimedForeignEdges(ctx, producer, preds)
	if err != nil {
		// Read blip — stay observe-only and fail-open (never block ingest). One
		// log, no metric (this batch could not be classified).
		c.logger.Warn("graph-ingest: foreign-edge claim classification failed — routing without classifying (observe-only)",
			slog.String("message_type", label), slog.Any("error", err))
		return
	}
	for _, p := range unclaimed {
		c.foreignEdgeUnclaimed.WithLabelValues(label, p).Inc()
		warnKey := label + "|" + p
		if _, warned := c.foreignEdgeWarnedKeys.LoadOrStore(warnKey, struct{}{}); !warned {
			c.logger.Warn("graph-ingest: foreign-subject edge has no registered ForeignEdgeClaim — routed deprecated-on-arrival (ADR-056 Decision 4; register a claim before the must-exist flip)",
				slog.String("message_type", label),
				slog.String("predicate", p))
		}
	}
}

// leaseViolation is the first confirmed owner-lease mismatch found in a
// request's owned-predicate set. checkOwnerLease returns a non-nil
// *leaseViolation ONLY when enforcement (Config.EnforceOwnerLease) is on AND a
// mismatch is confirmed; the handler then rejects the write with
// ErrorCodeOwnerLeaseStale. In the default observe-only posture it is always nil
// (the mismatch is still metered + Warn-logged).
type leaseViolation struct {
	predicate     string // the contested owned predicate
	expectedOwner string // live owner id (no incarnation nonce — safe for the caller-facing error)
	got           string // the request's presented OwnerToken
}

// detail renders the caller-facing reject message. It names the live owner that
// holds the lease and the contested predicate, but deliberately omits the live
// incarnation nonce (a liveness fence surfaced only in the operator Warn log).
func (v *leaseViolation) detail() string {
	return fmt.Sprintf("owner lease stale: predicate %q is held by live owner %q; the request's OwnerToken does not match",
		v.predicate, v.expectedOwner)
}

// checkOwnerLease is the ADR-056 owner-lease check at the graph-ingest write
// seam. It inspects each owned predicate being written against the live
// OwnerClaim in the registry. On a mismatch (the request's OwnerToken != the
// live "<owner>#<incarnation>") it ALWAYS meters owner_lease_mismatch_total + a
// Warn. Whether it BLOCKS depends on Config.EnforceOwnerLease:
//
//   - PR-3 default (EnforceOwnerLease=false): observe-only — returns nil, the
//     write always commits. This is the bake posture.
//   - PR-5 enforce (EnforceOwnerLease=true): a confirmed mismatch returns a
//     non-nil *leaseViolation; the handler rejects the write before it commits
//     with ErrorCodeOwnerLeaseStale.
//
// ownedPredicates is the deduped set of owned-write predicates for this request
// (own/addOwn predicates + RemoveTriples predicates on the update lanes).
// messageType is the bounded metric label (registry key or "_invalid").
//
// Two-state skip (return nil — never reject):
//   - ownerToken == ""  → legacy/unowned writer (the single agreed skip signal).
//   - claimReader == nil → resourceless/unmigrated deploy graceful-skip.
//
// Per-predicate (fail-open paths return nil even under enforcement):
//   - err != nil  → Warn + stop (fail-open; honor a mismatch already confirmed
//     on an earlier predicate, but never block on a reader blip alone).
//   - ok == false → continue (unclaimed/append-evidence — not lease-governed).
//   - ok == true && incarnation == "" → fail-open (legacy/pre-fence owner; lease
//     not enforceable — no metric, no Warn, no reject).
//   - ok == true && incarnation != "" → compare ownerToken vs "<owner>#<incarnation>";
//     mismatch → meter + Warn, capture the first violation, continue.
func (c *Component) checkOwnerLease(ctx context.Context, entityID, messageType, ownerToken string, ownedPredicates []string) *leaseViolation {
	// Two-state skip.
	if ownerToken == "" {
		return nil
	}
	if c.claimReader == nil {
		return nil
	}
	if len(ownedPredicates) == 0 {
		return nil
	}

	var violation *leaseViolation
	for _, pred := range ownedPredicates {
		owner, incarnation, ok, err := c.claimReader.OwnerOf(ctx, entityID, pred)
		if err != nil {
			// Fail-open: a transient reader blip must never block a write. Stop
			// checking here (PR-3 behavior), but still honor a mismatch already
			// confirmed on an earlier predicate when enforcing.
			c.logger.Warn("graph-ingest: owner-lease read failed — skipping lease check for this predicate (fail-open)",
				slog.String("entity_id", entityID),
				slog.String("predicate", pred),
				slog.String("message_type", messageType),
				slog.Any("error", err))
			return c.leaseVerdict(violation)
		}
		if !ok {
			// Unclaimed / append-evidence — not lease-governed.
			continue
		}
		if incarnation == "" {
			// Legacy/pre-fence owning claim: the lease is not enforceable.
			// Fail-open — no metric, no Warn, no reject.
			continue
		}
		// Compare the request token against the live owner's expected token.
		// ExpectedOwnerToken keeps the "<owner>#<incarnation>" format in
		// pkg/ownership — graph-ingest never composes it (ADR-056 PR-3.5).
		expected := ownership.ExpectedOwnerToken(owner, incarnation).Wire()
		if ownerToken != expected {
			c.ownerLeaseMismatch.WithLabelValues(messageType, pred).Inc()
			if c.config.EnforceOwnerLease {
				c.logger.Warn("graph-ingest: OwnerToken mismatch — write REJECTED (owner_lease_stale; enforce_owner_lease on)",
					slog.String("entity_id", entityID),
					slog.String("predicate", pred),
					slog.String("message_type", messageType),
					slog.String("owner_token", ownerToken),
					slog.String("expected_token", expected))
			} else {
				c.logger.Warn("graph-ingest: OwnerToken mismatch — write proceeds (observe-only; set enforce_owner_lease to reject)",
					slog.String("entity_id", entityID),
					slog.String("predicate", pred),
					slog.String("message_type", messageType),
					slog.String("owner_token", ownerToken),
					slog.String("expected_token", expected))
			}
			if violation == nil {
				violation = &leaseViolation{predicate: pred, expectedOwner: owner, got: ownerToken}
			}
		}
	}
	return c.leaseVerdict(violation)
}

// leaseVerdict gates the reject on the enforcement toggle: it surfaces a
// confirmed violation ONLY when Config.EnforceOwnerLease is set, so the default
// observe-only path never blocks a write even after a mismatch has been
// metered + Warn-logged.
func (c *Component) leaseVerdict(v *leaseViolation) *leaseViolation {
	if v != nil && c.config.EnforceOwnerLease {
		return v
	}
	return nil
}

// extractEntityFromMessage extracts an EntityState from a BaseMessage
func (c *Component) extractEntityFromMessage(msg *message.BaseMessage) (*graph.EntityState, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil message")
	}

	payload := msg.Payload()
	if payload == nil {
		return nil, fmt.Errorf("message has no payload")
	}

	// Check if payload implements Graphable
	graphable, ok := payload.(graph.Graphable)
	if !ok {
		return nil, fmt.Errorf("payload does not implement Graphable interface")
	}

	// Get entity ID and triples from Graphable
	entityID := graphable.EntityID()
	if entityID == "" {
		return nil, fmt.Errorf("graphable payload returned empty entity ID")
	}

	triples := graphable.Triples()

	// Build EntityState
	entity := &graph.EntityState{
		ID:          entityID,
		Triples:     triples,
		MessageType: msg.Type(),
		Version:     1,
	}

	// ADR-055 §5 StorageRef extraction (consumer half): a Graphable payload that
	// also implements message.Storable carries an ObjectStore reference to its
	// offloaded content. Lift it onto the EntityState so MergeEntity persists it
	// (the create branch marshals it verbatim; the existing-entity branch merges
	// it onto the stored copy). Without this, a ContentStorable producer's offloaded
	// content (tool results, model responses, document bodies) loses its storage
	// pointer at the ingest seam and the embedding worker can never find the
	// text — the "offloaded content stops embedding" gap.
	if storable, ok := payload.(message.Storable); ok {
		if ref := storable.StorageRef(); ref != nil {
			entity.StorageRef = ref
		}
	}

	// ADR-054 channel (a): a Graphable payload MAY also declare its indexing
	// profile via the optional IndexingProfiler interface. Stamp it explicitly
	// here so it's present on entity.Triples by the time MergeEntity reaches
	// its create seam; absence (or an invalid value) falls through to the
	// fallback floor there.
	if profiler, ok := payload.(message.IndexingProfiler); ok {
		stampExplicitIndexingProfile(entity, profiler.IndexingProfile())
	}

	return entity, nil
}

// partitionTriplesBySubject splits a Graphable's triples into those belonging to
// the primary entity and "foreign" triples whose Subject names a DIFFERENT
// entity. A triple is primary when its Subject equals entityID, or is empty (the
// historical filing target — extractEntityFromMessage has always placed
// subject-less triples on the primary). Foreign triples are cross-entity edges —
// e.g. sensorml's inverse isHostedBy stamped on the child subject — that the
// single-key filing in extractEntityFromMessage would otherwise misfile under the
// primary entity (ADR-055 §5 T2). This function only classifies; the caller
// routes foreign triples to their correct subject.
func partitionTriplesBySubject(entityID string, triples []message.Triple) (own, foreign []message.Triple) {
	for _, t := range triples {
		if t.Subject == "" || t.Subject == entityID {
			own = append(own, t)
			continue
		}
		foreign = append(foreign, t)
	}
	return own, foreign
}

// distinctSubjects returns the sorted set of distinct Subjects across the triples,
// for low-cardinality operator logging of where regrouped edges were routed.
func distinctSubjects(triples []message.Triple) []string {
	seen := make(map[string]struct{}, len(triples))
	for _, t := range triples {
		seen[t.Subject] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// hasIndexingProfileTriple reports whether the entity already carries a
// create-time indexing profile. That profile is immutable (ADR-054), so a
// later Graphable arrival must not override it through the newer-wins merge.
func hasIndexingProfileTriple(entity *graph.EntityState) bool {
	for _, t := range entity.Triples {
		if t.Predicate == vocabulary.EntityIndexingProfile {
			return true
		}
	}
	return false
}

// triplesWithoutPredicate returns triples with every triple carrying predicate
// removed. Non-mutating; used to drop an incoming immutable predicate before a
// newer-wins merge.
func triplesWithoutPredicate(triples []message.Triple, predicate string) []message.Triple {
	out := make([]message.Triple, 0, len(triples))
	for _, t := range triples {
		if t.Predicate != predicate {
			out = append(out, t)
		}
	}
	return out
}

// removeIndexingProfileTriples drops every entity.indexing.profile triple from
// the entity (the single-valued predicate is replace-on-write, never appended).
func removeIndexingProfileTriples(entity *graph.EntityState) {
	filtered := entity.Triples[:0]
	for _, t := range entity.Triples {
		if t.Predicate != vocabulary.EntityIndexingProfile {
			filtered = append(filtered, t)
		}
	}
	entity.Triples = filtered
}

// appendIndexingProfileTriple appends one entity.indexing.profile triple.
// Callers are responsible for having removed any prior value first.
func appendIndexingProfileTriple(entity *graph.EntityState, profile string) {
	entity.Triples = append(entity.Triples, message.Triple{
		Subject:    entity.ID,
		Predicate:  vocabulary.EntityIndexingProfile,
		Object:     profile,
		Source:     "graph-ingest-indexing-profile",
		Timestamp:  time.Now(),
		Confidence: 1.0,
	})
}

// stampExplicitIndexingProfile sets entity.indexing.profile to an explicitly
// declared profile (replace-on-write, single-valued). Empty or unrecognized
// values are ignored — the entity then falls through to the fallback floor at
// its create seam rather than failing (lenient Phase 1 semantics). Used by the
// Graphable IndexingProfiler channel (extractEntityFromMessage) and the
// mutation-envelope channel (handleEntityCreateWithTriples).
func stampExplicitIndexingProfile(entity *graph.EntityState, profile string) {
	if entity == nil || !vocabulary.IsValidIndexingProfile(profile) {
		return
	}
	removeIndexingProfileTriples(entity)
	appendIndexingProfileTriple(entity, profile)
}

// reconcileIndexingProfile is the entity-CREATION-seam stamp (ADR-054 §5). It
// runs at every place an entity is born — createEntity, MergeEntity's
// first-write branch, and the stub→real upgrade in MergeEntity's merge branch —
// and never on a plain update of an already-profiled entity, so a profile is
// immutable once set. It enforces the single-valued invariant and applies the
// fallback floor when nothing was declared:
//
//   - ≥1 profile triple present (an explicit declaration was stamped upstream,
//     or a real producer is upgrading a profile-less stub): keep the FIRST and
//     drop any duplicates. No floor, no metric.
//   - 0 profile triples present: apply the registry floor
//     (indexingProfileFloorFor) and append it; increment
//     indexing_profile_default_total{message_type} ONLY when the type was not in
//     the registry (an unclassified gap, not a deliberate registered floor).
func (c *Component) reconcileIndexingProfile(entity *graph.EntityState) {
	if entity == nil {
		return
	}
	kept := false
	filtered := entity.Triples[:0]
	for _, t := range entity.Triples {
		if t.Predicate == vocabulary.EntityIndexingProfile {
			if kept {
				continue // single-valued: drop duplicates, keep the first
			}
			kept = true
		}
		filtered = append(filtered, t)
	}
	entity.Triples = filtered
	if kept {
		return
	}
	// No explicit declaration → apply the registry floor (ADR-054 channel c).
	// The default-fallback metric fires ONLY on a registry MISS — a type that is
	// neither producer-declared nor classified in the seed and silently took the
	// control default. A registered floor (e.g. agentic.request → trace) is a
	// deliberate classification, not an operator gap.
	profile, registered := indexingProfileFloorFor(entity.MessageType)
	appendIndexingProfileTriple(entity, profile)
	if !registered && c.indexingProfileDefault != nil {
		c.indexingProfileDefault.WithLabelValues(indexingProfileMetricLabel(entity.MessageType)).Inc()
	}
}

// indexingProfileMetricLabel renders a message.Type as a stable, low-cardinality
// metric label, or "unknown" for any incomplete Type. The IsValid guard (all
// three of Domain/Category/Version present) prevents a partial Type from
// producing a non-semantic label like ".widget.v1" or "test.widget." — a
// malformed producer surfaces as "unknown" rather than a junk label key.
func indexingProfileMetricLabel(mt message.Type) string {
	if !mt.IsValid() {
		return "unknown"
	}
	return mt.Key()
}

// ============================================================================
// Entity Operations
// ============================================================================

// validateEntityID validates that an entity ID follows the expected format
func validateEntityID(id string) error {
	return semtypes.ValidateEntityID(id)
}

// CreateEntity creates a new entity in the graph using upsert (Put)
// semantics. Existing callers that need last-writer-wins behavior stay
// on this path. Atomic-create callers (the NATS mutation handlers' POST
// 409 path) use
// CreateEntityStrict, which fails fast with natsclient.ErrKVKeyExists
// when the ID is already present.
func (c *Component) CreateEntity(ctx context.Context, entity *graph.EntityState) error {
	return c.createEntity(ctx, entity, false)
}

// MergeEntity ingests a streaming-consumer EntityState (typically built
// by extractEntityFromMessage from a Graphable arriving on the
// JetStream input) WITHOUT clobbering pre-existing triples on the
// entity. First write behaves like CreateEntity (the entity didn't
// exist; its fields land verbatim); subsequent writes MERGE the incoming
// triples predicate-level (replace per (subject,predicate) via
// graph.MergeTriples, gh#466 — the create-time indexing profile is
// preserved) and refresh latest-wins metadata (MessageType, StorageRef,
// UpdatedAt) while monotonically bumping Version.
//
// Closes gh#177: the prior code called CreateEntity (Put = full-
// replace) from handleMessage, which erased any triples written via
// the atomic mutation handlers (create_with_triples, update_with_
// triples, triple.add) — all of which merge. The jetstream consumer
// path was the lone outlier. Lifecycle-managed entities surfaced this
// most loudly: Manager.Create stamped the phase triple, then the
// first Graphable arrival via a downstream processor wiped it.
//
// Uses entityBucket.UpdateWithRetry for atomic CAS read-modify-write,
// so concurrent arrivals on the same Subject converge without racing.
func (c *Component) MergeEntity(ctx context.Context, entity *graph.EntityState) error {
	if entity == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "MergeEntity", "entity cannot be nil")
	}
	if err := validateEntityID(entity.ID); err != nil {
		return err
	}
	if err := graph.ValidateEntityStateContract(entity); err != nil {
		return errs.WrapInvalid(err, "Component", "MergeEntity", "validate entity state contract")
	}
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "MergeEntity", "context cancelled")
	}

	// Hierarchy inference is deterministic per entityID — calling it
	// on every merge would APPEND the same hierarchy triples on each
	// arrival (50 mission-command Graphables → 50 copies of
	// type.container / system.container / etc. on the same entity).
	// Gate to first-write only, applied inside the CAS callback so the
	// fetch happens once per genuine create. See go-reviewer concern 5
	// on the gh#177 fix.
	var hierarchyTriples []message.Triple
	if c.config.EnableHierarchy && c.hierarchyInference != nil {
		// Probe-then-fetch: cheap pre-check avoids the inference cost
		// on guaranteed-second-write paths. Even if the entity is
		// concurrently created between probe and CAS, the callback's
		// len(current) == 0 branch is the gate that actually applies
		// the hierarchy triples — the probe is an optimization, not
		// correctness.
		if _, err := c.entityBucket.Get(ctx, entity.ID); err != nil && natsclient.IsKVNotFoundError(err) {
			triples, herr := c.hierarchyInference.GetHierarchyTriples(ctx, entity.ID)
			if herr != nil {
				c.logger.Warn("Failed to get hierarchy triples",
					slog.String("entity_id", entity.ID),
					slog.Any("error", herr))
			} else {
				hierarchyTriples = triples
			}
		}
	}

	var bytesWritten int
	// casAttempt counts CAS-callback invocations; each re-run (attempt > 1) means
	// the prior revision-checked Put lost the CAS and retried (ADR-072
	// cas_retries — cross-entity contention observability, not a keying proof).
	casAttempt := 0
	err := c.entityBucket.UpdateWithRetry(ctx, entity.ID, func(current []byte) ([]byte, error) {
		casAttempt++
		if casAttempt > 1 && c.casRetries != nil {
			c.casRetries.Inc()
		}
		// First write: entity didn't exist. Apply hierarchy triples
		// (deterministic-per-ID so safe to apply once on create),
		// then store verbatim.
		if len(current) == 0 {
			if len(hierarchyTriples) > 0 {
				entity.Triples = append(entity.Triples, hierarchyTriples...)
			}
			// ADR-054: first write is entity birth — stamp the profile
			// (explicit-if-declared via IndexingProfiler, else floor).
			c.reconcileIndexingProfile(entity)
			data, err := graph.MarshalEntityState(entity)
			if err == nil {
				bytesWritten = len(data)
			}
			return data, err
		}
		// Existing entity: merge triples + refresh latest-wins metadata.
		// Hierarchy triples are NOT re-applied — they landed on the
		// original create and would only produce duplicates here.
		//
		// gh#562: trusted decode — this is the owner's own RMW read on the
		// per-key-serialized ingest hot path; MarshalEntityState below
		// re-validates the merged candidate, so resident poison still fails
		// the write (classified via classifyStoredStateRMWError).
		var existing graph.EntityState
		if err := graph.UnmarshalEntityStateTrusted(current, &existing); err != nil {
			return nil, c.classifyStoredStateRMWError(current, err) // non-retryable
		}
		// gh#466: predicate-level merge (replace per (subject,predicate)), NOT raw
		// append — otherwise a producer republishing the same entity accumulates
		// duplicate triples forever. MergeTriples lets the incoming arrival win on
		// a (subject,predicate) conflict while preserving non-conflicting existing
		// triples (e.g. lifecycle-managed predicates the arrival doesn't carry —
		// gh#177). Same helper the mutation lane (AddTriples) already uses.
		//
		// The indexing profile is the exception: it is create-time-immutable
		// (ADR-054), but MergeTriples is newer-wins, so a re-arrival declaring a
		// different profile would override the create-time one. Drop the incoming
		// profile before merging WHEN the existing entity already carries one; a
		// profile-less stub keeps the incoming declaration as its true birth
		// (reconcileIndexingProfile stamps it below).
		newer := entity.Triples
		if hasIndexingProfileTriple(&existing) {
			newer = triplesWithoutPredicate(newer, vocabulary.EntityIndexingProfile)
		}
		existing.Triples = graph.MergeTriples(existing.Triples, newer)
		existing.MessageType = entity.MessageType
		if entity.StorageRef != nil {
			existing.StorageRef = entity.StorageRef
		}
		// ADR-054: a real producer merging into a profile-less referential-
		// integrity stub is that entity's true birth — reconcile stamps the
		// profile (kept from the incoming declaration, else floor). For an
		// already-profiled entity this is a no-op (keep-first preserves the
		// create-time value), so a re-arrival never re-profiles.
		c.reconcileIndexingProfile(&existing)
		existing.Version++
		existing.UpdatedAt = time.Now()
		data, err := graph.MarshalEntityState(&existing)
		if err != nil {
			return nil, c.classifyStoredStateRMWError(current, err)
		}
		bytesWritten = len(data)
		return data, nil
	})
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "MergeEntity", "CAS update")
	}

	// Cache invalidation matches createEntity — readers must see the
	// merged state on next Get.
	if c.entityCache != nil {
		c.entityCache.Delete(entity.ID) //nolint:errcheck
	}

	c.updateSuffixIndex(ctx, entity.ID)

	atomic.AddInt64(&c.messagesProcessed, 1)
	atomic.AddInt64(&c.bytesProcessed, int64(bytesWritten))
	c.lastActivity.Store(time.Now())
	c.entitiesUpdated.Inc()

	c.logger.Debug("entity merged",
		slog.String("entity_id", entity.ID),
		slog.Int("triples_in", len(entity.Triples)))

	c.ensureRelationshipTargetsExist(ctx, entity)

	return nil
}

// CreateEntityStrict creates a new entity atomically — if the ID is
// already present, returns natsclient.ErrKVKeyExists without
// overwriting. Closes the concurrent-create TOCTOU window the
// graph.mutation.entity.create handler used to have when it relied
// on exists-check + Put.
func (c *Component) CreateEntityStrict(ctx context.Context, entity *graph.EntityState) error {
	return c.createEntity(ctx, entity, true)
}

// createEntity is the shared body for CreateEntity / CreateEntityStrict.
// atomicCreate=true switches the KV write from Put (upsert) to
// Create (atomic key-create); everything else (hierarchy inference,
// referential integrity stubs, cache invalidation, metrics) is
// identical.
func (c *Component) createEntity(ctx context.Context, entity *graph.EntityState, atomicCreate bool) error {
	if entity == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "CreateEntity", "entity cannot be nil")
	}

	// Validate entity ID format
	if err := validateEntityID(entity.ID); err != nil {
		return err
	}
	if err := graph.ValidateEntityStateContract(entity); err != nil {
		return errs.WrapInvalid(err, "Component", "CreateEntity", "validate entity state contract")
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "CreateEntity", "context cancelled")
	}

	// SYNCHRONOUS HIERARCHY INFERENCE:
	// Get hierarchy triples BEFORE writing entity to storage
	// This ensures entity is written once with all triples included (no cascade)
	if c.config.EnableHierarchy && c.hierarchyInference != nil {
		hierarchyTriples, err := c.hierarchyInference.GetHierarchyTriples(ctx, entity.ID)
		if err != nil {
			c.logger.Warn("Failed to get hierarchy triples",
				slog.String("entity_id", entity.ID),
				slog.Any("error", err))
			// Don't fail entity creation if hierarchy fails - just log warning
		} else if len(hierarchyTriples) > 0 {
			// Add hierarchy triples to entity before writing
			entity.Triples = append(entity.Triples, hierarchyTriples...)
		}
	}

	// ADR-054: stamp the indexing profile at the creation seam — keeps an
	// explicit declaration (envelope/Graphable, already on entity.Triples) and
	// otherwise applies the fallback floor + default metric.
	c.reconcileIndexingProfile(entity)

	// Serialize entity (now includes hierarchy triples if enabled)
	data, err := graph.MarshalEntityState(entity)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "CreateEntity", "entity serialization")
	}

	// Store in KV bucket. Put is upsert (last-writer-wins); Create is
	// atomic create-or-fail (returns natsclient.ErrKVKeyExists on
	// conflict). The ErrKVKeyExists sentinel is bubbled verbatim so
	// handlers can branch with errors.Is.
	var writeErr error
	if atomicCreate {
		_, writeErr = c.entityBucket.Create(ctx, entity.ID, data)
		if writeErr != nil && errors.Is(writeErr, natsclient.ErrKVKeyExists) {
			// Expected conflict shape — don't count as a component
			// error and don't wrap (preserves sentinel identity).
			return writeErr
		}
	} else {
		_, writeErr = c.entityBucket.Put(ctx, entity.ID, data)
	}
	if writeErr != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(writeErr, "Component", "CreateEntity", "KV store")
	}

	// Invalidate cache on write (cache consistency)
	if c.entityCache != nil {
		c.entityCache.Delete(entity.ID) //nolint:errcheck
	}

	// Update suffix index (best-effort, don't fail entity creation)
	c.updateSuffixIndex(ctx, entity.ID)

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	atomic.AddInt64(&c.bytesProcessed, int64(len(data)))
	c.lastActivity.Store(time.Now())
	c.entitiesUpdated.Inc()

	c.logger.Debug("entity created",
		slog.String("entity_id", entity.ID),
		slog.Int("triples", len(entity.Triples)))

	c.ensureRelationshipTargetsExist(ctx, entity)

	return nil
}

// ensureRelationshipTargetsExist walks the entity's relationship triples
// and, for each referenced entity that doesn't yet exist, creates a
// stub. Bounded to 5 concurrent KV ops. Errors are best-effort —
// referential integrity is a fallback, not a hard contract — so a
// failure is logged but does not propagate.
//
// Extracted from createEntity to keep that function under the
// revive.toml function-length cap (50 statements).
func (c *Component) ensureRelationshipTargetsExist(ctx context.Context, entity *graph.EntityState) {
	// Deduplicate target IDs — multiple triples may reference the same entity.
	uniqueTargets := make(map[string]struct{})
	for _, triple := range entity.Triples {
		if triple.IsRelationship() {
			targetID, ok := triple.Object.(string)
			if ok && targetID != "" && targetID != entity.ID {
				uniqueTargets[targetID] = struct{}{}
			}
		}
	}

	sem := make(chan struct{}, 5)
	var wg sync.WaitGroup

	for targetID := range uniqueTargets {
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(id string) {
			defer wg.Done()

			// Acquire semaphore with context cancellation support.
			select {
			case <-ctx.Done():
				return
			case sem <- struct{}{}:
				defer func() { <-sem }()
			}

			if ctx.Err() != nil {
				return
			}

			if err := c.ensureReferencedEntityExists(ctx, id, entity.ID, entity.MessageType); err != nil {
				c.logger.Debug("failed to ensure referenced entity exists",
					slog.String("target", id),
					slog.String("referenced_by", entity.ID),
					slog.Any("error", err))
			}
		}(targetID)
	}

	wg.Wait()
}

// Referential-integrity stub identity (ADR-056 Decision 4 lane-ii). The stub is
// the framework's no-birth referential producer: a referenced target with no
// independent producer (e.g. sensorml/cs-api children, only ever referenced via
// a parent's hierarchy) is materialized here. The stub is now a FIRST-CLASS,
// ENVELOPE-BEARING artifact — it carries stubMessageType so the ADR-055
// "no envelope-less ownerless births" invariant is literally true — but it stays
// PROFILE-LESS so MergeEntity still detects the real producer's later merge as
// the entity's true birth (reconcileIndexingProfile keys on profile-absence,
// component.go:1414-1419).
// Stub identity now lives in the graph package (graph.StubMessageType +
// graph.PredStub*) so the producer here and enumerating consumers — notably the
// gated-DAG executor, which must not dispatch a stub (gh#429) — share one
// definition. These locals alias the shared contract.
var stubMessageType = graph.StubMessageType

const (
	predStubMarker        = graph.PredStubMarker
	predStubReferencedBy  = graph.PredStubReferencedBy
	predStubOwner         = graph.PredStubOwner
	stubReferentialSource = "graph-ingest-referential-integrity"
)

// ensureReferencedEntityExists creates a stub entity if the referenced entity
// doesn't exist — the referential-integrity guarantee that a referenced ID
// always resolves to a node (ADR-056 Decision 4 lane-ii). referencedByType is
// the MessageType of the entity that referenced it (the reachable producer
// identity at this seam); the ADR's "the producer's registered ForeignEdgeClaim
// MessageType" is NOT reachable here (we hold only the source entity, not a
// claim), so the stub records the source type — or, for an untyped source (a
// gateway that does not stamp MessageType), the framework referential producer.
func (c *Component) ensureReferencedEntityExists(ctx context.Context, entityID, referencedBy string, referencedByType message.Type) error {
	// Check if entity already exists
	_, err := c.entityBucket.Get(ctx, entityID)
	if err == nil {
		return nil // Entity exists, nothing to do
	}

	stubOwner := stubReferentialSource
	if referencedByType.IsValid() {
		stubOwner = referencedByType.Key()
	}

	// Entity doesn't exist - create an envelope-bearing stub.
	// Version is set to 1 to match the invariant held by every other
	// canonical EntityState write path. When the real entity
	// later arrives, the merge path increments Version to 2 and overwrites
	// MessageType with the real producer's (component.go:1410).
	now := time.Now()
	stub := &graph.EntityState{
		ID:          entityID,
		Version:     1,
		UpdatedAt:   now,
		MessageType: stubMessageType,
		Triples: []message.Triple{
			{Subject: entityID, Predicate: predStubMarker, Object: true, Source: stubReferentialSource, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: predStubReferencedBy, Object: referencedBy, Source: stubReferentialSource, Timestamp: now, Confidence: 1.0},
			{Subject: entityID, Predicate: predStubOwner, Object: stubOwner, Source: stubReferentialSource, Timestamp: now, Confidence: 1.0},
		},
	}

	data, err := graph.MarshalEntityState(stub)
	if err != nil {
		return fmt.Errorf("marshal stub entity: %w", err)
	}

	// Atomic create-if-absent (gh#429). The Get above is only a fast-path: a real
	// producer can birth this entity between that Get (saw absent) and this write.
	// A Put would clobber the real entity back to a stub envelope — which, once
	// gated-DAG filters stubs, silently makes a real unit un-dispatchable. Create
	// returns ErrKVKeyExists on a concurrent create, which we map to a no-op
	// success — so we stub ONLY if the ID is genuinely still absent and never
	// overwrite a real (or sibling-stub) entity.
	if _, err := c.entityBucket.Create(ctx, entityID, data); err != nil {
		if errors.Is(err, natsclient.ErrKVKeyExists) {
			return nil // raced with a real (or sibling stub) create — do not clobber
		}
		return fmt.Errorf("store stub entity: %w", err)
	}

	c.logger.Debug("created envelope-bearing referential-integrity stub",
		slog.String("entity_id", entityID),
		slog.String("referenced_by", referencedBy),
		slog.String("stub_owner", stubOwner))

	return nil
}

// UpdateEntity updates an existing entity
func (c *Component) UpdateEntity(ctx context.Context, entity *graph.EntityState) error {
	if entity == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "UpdateEntity", "entity cannot be nil")
	}

	// Validate entity ID format
	if err := validateEntityID(entity.ID); err != nil {
		return err
	}
	if err := graph.ValidateEntityStateContract(entity); err != nil {
		return errs.WrapInvalid(err, "Component", "UpdateEntity", "validate entity state contract")
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "UpdateEntity", "context cancelled")
	}

	// Serialize entity
	data, err := graph.MarshalEntityState(entity)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdateEntity", "entity serialization")
	}

	// Update in KV bucket
	if _, err := c.entityBucket.Put(ctx, entity.ID, data); err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "UpdateEntity", "KV store")
	}

	// Invalidate cache on write (cache consistency)
	if c.entityCache != nil {
		c.entityCache.Delete(entity.ID) //nolint:errcheck
	}

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	atomic.AddInt64(&c.bytesProcessed, int64(len(data)))
	c.lastActivity.Store(time.Now())
	c.entitiesUpdated.Inc()

	c.logger.Debug("entity updated",
		slog.String("entity_id", entity.ID),
		slog.Uint64("version", entity.Version))

	return nil
}

// updateEntityAtRevision is the CAS-protected variant of UpdateEntity.
// It only commits if the entity's KV revision still matches expectedRev,
// otherwise returns natsclient.ErrKVRevisionMismatch. This closes the
// resurrect-after-delete window the plain UpdateEntity → Put path has:
// a concurrent DeleteEntity that lands between the caller's read and
// this write would otherwise turn an update into a silent re-create.
//
// Used by the mutation handlers' must-exist contract
// (graph.mutation.entity.update / .update_with_triples). UpdateEntity
// itself stays Put-based for callers that want last-writer-wins.
func (c *Component) updateEntityAtRevision(ctx context.Context, entity *graph.EntityState, expectedRev uint64) error {
	if entity == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "Component", "updateEntityAtRevision", "entity cannot be nil")
	}

	if err := validateEntityID(entity.ID); err != nil {
		return err
	}
	if err := graph.ValidateEntityStateContract(entity); err != nil {
		return errs.WrapInvalid(err, "Component", "updateEntityAtRevision", "validate entity state contract")
	}

	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "updateEntityAtRevision", "context cancelled")
	}

	data, err := graph.MarshalEntityState(entity)
	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "updateEntityAtRevision", "entity serialization")
	}

	if _, err := c.entityBucket.Update(ctx, entity.ID, data, expectedRev); err != nil {
		// ErrKVRevisionMismatch and other KV errors propagate verbatim so
		// callers can branch with errors.Is(..., natsclient.ErrKVRevisionMismatch).
		atomic.AddInt64(&c.errors, 1)
		return err
	}

	if c.entityCache != nil {
		c.entityCache.Delete(entity.ID) //nolint:errcheck
	}

	atomic.AddInt64(&c.messagesProcessed, 1)
	atomic.AddInt64(&c.bytesProcessed, int64(len(data)))
	c.lastActivity.Store(time.Now())
	c.entitiesUpdated.Inc()

	c.logger.Debug("entity updated (CAS)",
		slog.String("entity_id", entity.ID),
		slog.Uint64("expected_revision", expectedRev),
		slog.Uint64("version", entity.Version))

	return nil
}

// DeleteEntity removes an entity from the graph
func (c *Component) DeleteEntity(ctx context.Context, entityID string) error {
	// Validate entity ID format
	if err := validateEntityID(entityID); err != nil {
		return err
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "DeleteEntity", "context cancelled")
	}

	// Delete from KV bucket
	if err := c.entityBucket.Delete(ctx, entityID); err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "DeleteEntity", "KV delete")
	}

	// Invalidate cache on delete (cache consistency)
	if c.entityCache != nil {
		c.entityCache.Delete(entityID) //nolint:errcheck
	}

	// Remove suffix index entries (best-effort)
	c.removeSuffixIndex(ctx, entityID)

	// Update metrics
	atomic.AddInt64(&c.messagesProcessed, 1)
	c.lastActivity.Store(time.Now())

	c.logger.Debug("entity deleted", slog.String("entity_id", entityID))

	return nil
}

// ============================================================================
// Triple Operations
// ============================================================================

// invalidateEntityCacheEntry drops a stale entity-query-cache entry after a
// direct entityBucket write, preserving read-after-write coherence for callers
// that query via graph.ingest.query.* (e.g. the gated-DAG executor reading the
// whole unit set right after committing a claim, or any read-after-mutate via
// the NATS mutation API). The high-level CreateEntity/UpdateEntity/DeleteEntity/
// MergeEntity methods already invalidate inline; the triple-add and
// update_with_triples write paths did not, so a query within the 30s cache TTL
// returned the pre-write entity. No-op when the cache is disabled.
func (c *Component) invalidateEntityCacheEntry(id string) {
	if c.entityCache != nil {
		c.entityCache.Delete(id) //nolint:errcheck
	}
}

// AddTriple adds a triple to an entity using CAS for concurrency safety
func (c *Component) AddTriple(ctx context.Context, triple message.Triple) error {
	if err := graph.ValidateEntityStateContract(&graph.EntityState{
		ID: triple.Subject, Triples: []message.Triple{triple},
	}); err != nil {
		return errs.WrapInvalid(err, "Component", "AddTriple", "validate triple contract")
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "AddTriple", "context cancelled")
	}

	// Use UpdateWithRetry for atomic read-modify-write with CAS
	err := c.entityBucket.UpdateWithRetry(ctx, triple.Subject, func(current []byte) ([]byte, error) {
		var entity graph.EntityState

		if len(current) > 0 {
			// gh#562: trusted decode on the owner's own RMW read;
			// MarshalEntityState below re-validates the final candidate.
			if err := graph.UnmarshalEntityStateTrusted(current, &entity); err != nil {
				return nil, c.classifyStoredStateRMWError(current, err) // Non-retryable
			}
		} else {
			// Must-exist (ADR-055): a triple targeting an absent entity is
			// rejected, not silently auto-vivified. NonRetryable stops the CAS
			// loop and surfaces the sentinel for the handler to map.
			return nil, retry.NonRetryable(natsclient.ErrKVKeyNotFound)
		}

		// Add triple
		entity.Triples = append(entity.Triples, triple)
		entity.Version++
		entity.UpdatedAt = time.Now()

		data, err := graph.MarshalEntityState(&entity)
		if err != nil {
			return nil, c.classifyStoredStateRMWError(current, err)
		}
		return data, nil
	})

	if err != nil {
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "AddTriple", "CAS update")
	}

	// Read-after-write coherence: the entity-query cache must not serve the
	// pre-add state on the next graph.ingest.query.* read.
	c.invalidateEntityCacheEntry(triple.Subject)
	return nil
}

// AddTriples adds many triples in one call, batching per entity. Triples
// sharing the same Subject collapse to a single CAS read-modify-write on
// that entity's KV record — N triples on the same loop entity become
// 1 round-trip to graph-ingest, not N. Triples spanning multiple
// entities issue one CAS per entity in deterministic subject order.
//
// Atomicity scope: per-entity, not cross-entity. If two entities are
// in the batch and the first commits but the second exhausts CAS
// retries, the first stays committed. This trade-off is acceptable
// for the primary use case (write_todos, ADR-036) where every triple
// shares one Subject (the loop entity). Multi-subject callers that
// need cross-entity atomicity should use UpdateEntityWithTriples
// instead.
//
// Returns nil on full success. On partial failure returns an error
// whose message names the failing subjects; the per-subject error
// detail surfaces via the handler's FailedSubjects response field.
func (c *Component) AddTriples(ctx context.Context, triples []message.Triple) (writtenCount int, failedSubjects map[string]string, err error) {
	if len(triples) == 0 {
		return 0, nil, nil
	}

	// Preserve the established positional diagnostic for an omitted mutation
	// subject. The authoritative candidate validator below remains acceptance
	// authority for all subject syntax, references, and predicates.
	for index := range triples {
		if triples[index].Subject == "" {
			return 0, nil, errs.WrapInvalid(errs.ErrInvalidData, "Component", "AddTriples", fmt.Sprintf("triple[%d] subject cannot be empty", index))
		}
	}

	// Validate the whole batch before any CAS read. The first subject supplies
	// the synthetic candidate root; every subject/reference/predicate is still
	// validated independently by the authoritative contract.
	if contractErr := graph.ValidateEntityStateContract(&graph.EntityState{
		ID: triples[0].Subject, Triples: triples,
	}); contractErr != nil {
		return 0, nil, errs.WrapInvalid(contractErr, "Component", "AddTriples", "validate batch contract")
	}

	if err := ctx.Err(); err != nil {
		return 0, nil, errs.Wrap(err, "Component", "AddTriples", "context cancelled")
	}

	// Group triples by Subject so each entity sees a single CAS.
	// Map iteration is non-deterministic; we sort subject keys before
	// committing so retries replay in stable order (helpful for tests
	// and for any operator reading a partial-failure trace).
	bySubject := make(map[string][]message.Triple, len(triples))
	for _, t := range triples {
		bySubject[t.Subject] = append(bySubject[t.Subject], t)
	}
	subjects := make([]string, 0, len(bySubject))
	for s := range bySubject {
		subjects = append(subjects, s)
	}
	sort.Strings(subjects)

	failedSubjects = make(map[string]string)
	// allAbsences tracks whether EVERY per-subject failure was an ADR-055
	// entity-not-found rejection. When true the aggregated error wraps
	// ErrKVKeyNotFound so the handler can set ErrorCodeEntityNotFound on the
	// response without string-sniffing. Mixed-reason batches (allAbsences=false)
	// stay un-sentineled → handler leaves ErrorCode empty (unclassified).
	allAbsences := true
	for i, subject := range subjects {
		// Short-circuit on context cancellation: don't burn the retry
		// budget for every remaining subject when the caller has
		// already given up. Mark the rest as failed-due-to-cancel so
		// the response shape is honest about what didn't commit, but
		// only count one operational error event for the cancellation
		// rather than N (one per remaining subject).
		if ctxErr := ctx.Err(); ctxErr != nil {
			for _, s := range subjects[i:] {
				failedSubjects[s] = ctxErr.Error()
			}
			// A cancelled batch is NOT a pure entity-not-found batch.
			allAbsences = false
			atomic.AddInt64(&c.errors, 1)
			break
		}
		group := bySubject[subject]
		casErr := c.entityBucket.UpdateWithRetry(ctx, subject, func(current []byte) ([]byte, error) {
			var entity graph.EntityState

			if len(current) > 0 {
				// gh#562: trusted decode on the owner's own RMW read;
				// MarshalEntityState below re-validates the final candidate.
				if err := graph.UnmarshalEntityStateTrusted(current, &entity); err != nil {
					return nil, c.classifyStoredStateRMWError(current, err) // Non-retryable
				}
			} else {
				// Must-exist (ADR-055): a triple targeting an absent entity is
				// rejected, not silently auto-vivified. NonRetryable stops the CAS
				// loop and surfaces the sentinel for the handler to map.
				return nil, retry.NonRetryable(natsclient.ErrKVKeyNotFound)
			}

			entity.Triples = append(entity.Triples, group...)
			entity.Version++
			entity.UpdatedAt = time.Now()

			data, err := graph.MarshalEntityState(&entity)
			if err != nil {
				return nil, c.classifyStoredStateRMWError(current, err)
			}
			return data, nil
		})

		if casErr != nil {
			atomic.AddInt64(&c.errors, 1)
			failedSubjects[subject] = casErr.Error()
			if !errors.Is(casErr, natsclient.ErrKVKeyNotFound) {
				allAbsences = false
			}
			continue
		}
		// Read-after-write coherence: invalidate the just-written subject's
		// cached entity so the next query reflects the appended triples.
		c.invalidateEntityCacheEntry(subject)
		writtenCount += len(group)
	}

	if len(failedSubjects) == 0 {
		return writtenCount, nil, nil
	}

	// Sort failed-subject names for stable error messages.
	failed := make([]string, 0, len(failedSubjects))
	for s := range failedSubjects {
		failed = append(failed, s)
	}
	sort.Strings(failed)
	var innerErr error
	if allAbsences {
		// All failures were entity-not-found: wrap the sentinel so the handler
		// can set ErrorCodeEntityNotFound without string-sniffing.
		innerErr = fmt.Errorf("CAS update failed for %d/%d subjects: %v: %w",
			len(failedSubjects), len(subjects), failed, natsclient.ErrKVKeyNotFound)
	} else {
		innerErr = fmt.Errorf("CAS update failed for %d/%d subjects: %v",
			len(failedSubjects), len(subjects), failed)
	}
	return writtenCount, failedSubjects, errs.Wrap(innerErr, "Component", "AddTriples", "batch CAS partial failure")
}

// errNoOpRemove exits RemoveTriple's CAS closure when no stored triple
// matches the predicate: a TRUE no-op. The caller maps it to nil success
// BEFORE any KV write, so the closure's only success exit is a
// MarshalEntityState-validated candidate (gh#562 review M1 — previously the
// closure returned the current bytes and UpdateWithRetry CAS-rewrote them
// verbatim, bumping the revision and re-firing every ENTITY_STATES watcher).
var errNoOpRemove = errors.New("remove triple: no matching predicate (no-op)")

// RemoveTriple removes a triple from an entity using CAS for concurrency safety
func (c *Component) RemoveTriple(ctx context.Context, subject, predicate string) error {
	if err := validateEntityID(subject); err != nil {
		return errs.WrapInvalid(err, "Component", "RemoveTriple", "validate subject")
	}
	if _, err := vocabulary.ParsePredicate(predicate); err != nil {
		return errs.WrapInvalid(err, "Component", "RemoveTriple", "validate predicate")
	}

	// Check context
	if err := ctx.Err(); err != nil {
		return errs.Wrap(err, "Component", "RemoveTriple", "context cancelled")
	}

	// Check if entity exists first - if not, nothing to remove
	_, err := c.entityBucket.Get(ctx, subject)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			return nil // Entity doesn't exist, nothing to remove
		}
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "RemoveTriple", "entity lookup")
	}

	// Use UpdateWithRetry for atomic read-modify-write with CAS
	err = c.entityBucket.UpdateWithRetry(ctx, subject, func(current []byte) ([]byte, error) {
		if len(current) == 0 {
			// Entity was deleted between our check and update - nothing to do
			return nil, natsclient.ErrKVKeyNotFound
		}

		// Deserialize existing entity.
		// gh#562: trusted decode on the owner's own RMW read;
		// MarshalEntityState below re-validates the final candidate.
		var entity graph.EntityState
		if err := graph.UnmarshalEntityStateTrusted(current, &entity); err != nil {
			return nil, c.classifyStoredStateRMWError(current, err) // Non-retryable
		}

		// Remove matching triples
		filtered := make([]message.Triple, 0, len(entity.Triples))
		for _, t := range entity.Triples {
			if t.Predicate != predicate {
				filtered = append(filtered, t)
			}
		}

		// No matching predicate: exit via sentinel, NOT by returning the
		// current bytes — UpdateWithRetry CAS-writes whatever the closure
		// returns, so `return current, nil` would be an identity rewrite
		// (revision bump + watcher re-fire), not a skip. The caller maps
		// the sentinel to silent success with no write committed.
		if len(filtered) == len(entity.Triples) {
			return nil, errNoOpRemove
		}

		entity.Triples = filtered
		entity.Version++
		entity.UpdatedAt = time.Now()

		data, err := graph.MarshalEntityState(&entity)
		if err != nil {
			return nil, c.classifyStoredStateRMWError(current, err)
		}
		return data, nil
	})

	// Handle errors - ErrKVKeyNotFound means entity was deleted, which is fine
	if err != nil {
		// True no-op: no triple matched the predicate. Silent success with
		// NO write committed — bytes and revision untouched, so no cache
		// invalidation is needed either.
		if errors.Is(err, errNoOpRemove) {
			return nil
		}
		// Check if it's a wrapped "not found" error
		if natsclient.IsKVNotFoundError(err) {
			return nil // Entity was deleted, nothing to remove
		}
		atomic.AddInt64(&c.errors, 1)
		return errs.Wrap(err, "Component", "RemoveTriple", "CAS update")
	}

	// Read-after-write coherence: drop the cached entity so a query reflects
	// the removed predicate (consumers clear markers via remove on reset).
	c.invalidateEntityCacheEntry(subject)
	return nil
}

// ============================================================================
// Suffix Index Operations
// ============================================================================

// entitySuffixKeys returns the suffix index keys for a given entity ID.
// Entity ID format: org.platform.domain.system.type.instance
// Returns two keys: instance part and type.instance part.
func entitySuffixKeys(entityID string) (instance, typeInstance string) {
	parts := strings.Split(entityID, ".")
	if len(parts) < 2 {
		return entityID, ""
	}
	instance = parts[len(parts)-1]
	typeInstance = parts[len(parts)-2] + "." + parts[len(parts)-1]
	return instance, typeInstance
}

// updateSuffixIndex writes suffix→fullID mappings to the KV suffix index.
// Best-effort: errors are logged but don't fail the caller.
func (c *Component) updateSuffixIndex(ctx context.Context, entityID string) {
	if c.suffixBucket == nil {
		return
	}

	instance, typeInstance := entitySuffixKeys(entityID)
	indexValue := []byte(`{"id":"` + entityID + `"}`)

	if instance != "" {
		if _, err := c.suffixBucket.Put(ctx, instance, indexValue); err != nil {
			c.logger.Debug("suffix index write failed",
				slog.String("key", instance), slog.Any("error", err))
		}
	}
	if typeInstance != "" {
		if _, err := c.suffixBucket.Put(ctx, typeInstance, indexValue); err != nil {
			c.logger.Debug("suffix index write failed",
				slog.String("key", typeInstance), slog.Any("error", err))
		}
	}
}

// removeSuffixIndex removes suffix→fullID mappings from the KV suffix index and cache.
// Best-effort: errors are logged but don't fail the caller.
func (c *Component) removeSuffixIndex(ctx context.Context, entityID string) {
	if c.suffixBucket == nil {
		return
	}

	instance, typeInstance := entitySuffixKeys(entityID)

	if instance != "" {
		_ = c.suffixBucket.Delete(ctx, instance)
		if c.suffixCache != nil {
			c.suffixCache.Delete(instance) //nolint:errcheck
		}
	}
	if typeInstance != "" {
		_ = c.suffixBucket.Delete(ctx, typeInstance)
		if c.suffixCache != nil {
			c.suffixCache.Delete(typeInstance) //nolint:errcheck
		}
	}
}
