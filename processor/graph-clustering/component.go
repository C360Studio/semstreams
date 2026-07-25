// Package graphclustering provides the graph-clustering component for community detection.
package graphclustering

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/clustering"
	"github.com/c360studio/semstreams/graph/inference"
	"github.com/c360studio/semstreams/graph/llm"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/graph/structural"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/resource"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/nats-io/nats.go/jetstream"
)

// Ensure Component implements required interfaces
var (
	_ component.Discoverable       = (*Component)(nil)
	_ component.LifecycleComponent = (*Component)(nil)
)

// Per-capability LLM-call timeout defaults applied when neither
// endpoint.request_timeout nor capability.timeout is configured. Sized to
// the implicit 60s fallback that NewOpenAIClient previously applied — kept
// short so an unconfigured deployment doesn't silently extend its
// per-community blocking window. Operators that need 300s+ set
// capability.timeout explicitly; the precedence chain in
// model.ResolveCapabilityTimeout picks it up.
const (
	defaultCommunitySummaryTimeout = 60 * time.Second
	defaultAnomalyReviewTimeout    = 60 * time.Second
)

// Config holds configuration for graph-clustering component
type Config struct {
	Ports                *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	DetectionIntervalStr string                `json:"detection_interval" schema:"type:string,description:Interval between community detection runs (e.g. 30s or 5m),category:basic"`
	BatchSize            int                   `json:"batch_size" schema:"type:int,description:Event count threshold for triggering detection,category:basic"`
	EnableLLM            bool                  `json:"enable_llm" schema:"type:bool,description:Enable LLM-based community summarization (requires model registry with community_summary capability),category:advanced"`
	EnhancementWorkers   int                   `json:"enhancement_workers" schema:"type:int,description:Number of parallel workers for LLM enhancement (default 5),category:advanced"`
	MinCommunitySize     int                   `json:"min_community_size" schema:"type:int,description:Minimum number of entities to form a community,category:advanced"`
	MaxIterations        int                   `json:"max_iterations" schema:"type:int,description:Maximum iterations for LPA algorithm,category:advanced"`

	// AllowUngatedReads permits community detection to run when graph-index's readiness
	// status endpoint is unreachable (gh#474 Codex #4). Default false = FAIL-CLOSED:
	// unknown readiness defers the cycle, so detection cannot derive communities from a
	// partial INCOMING_INDEX during a cutover when the authoritative owner is
	// crashed/restarting. Set true ONLY for a standalone deployment (or test) that runs
	// clustering without a co-deployed graph-index handler; it MUST NOT be used during a
	// format cutover.
	AllowUngatedReads bool `json:"allow_ungated_reads" schema:"type:bool,description:Allow detection when graph-index readiness is unknown (standalone only; never during cutover),category:advanced"`

	// Structural analysis (optional, enables anomaly detection)
	EnableStructural bool `json:"enable_structural" schema:"type:bool,description:Enable structural index computation (k-core and pivot distance),category:advanced"`
	PivotCount       int  `json:"pivot_count" schema:"type:int,description:Number of pivot nodes for distance indexing (default 16),category:advanced"`
	MaxHopDistance   int  `json:"max_hop_distance" schema:"type:int,description:Maximum BFS traversal depth (default 10),category:advanced"`

	// Anomaly detection (optional, requires EnableStructural)
	EnableAnomalyDetection bool             `json:"enable_anomaly_detection" schema:"type:bool,description:Enable anomaly detection after structural computation,category:advanced"`
	AnomalyConfig          inference.Config `json:"anomaly_config" schema:"type:object,description:Configuration for anomaly detection,category:advanced"`

	// Dependency startup configuration
	StartupAttempts int `json:"startup_attempts,omitempty" schema:"type:int,description:Max attempts to wait for dependencies at startup,category:advanced"`
	StartupInterval int `json:"startup_interval_ms,omitempty" schema:"type:int,description:Interval between startup attempts in milliseconds,category:advanced"`

	// EntityID virtual-edge synthesis (gh#461). Omit to keep the built-in
	// defaults (sibling + system-peer edges ON); set include_* false to run
	// community detection on explicit topology alone.
	EntityIDEdges *EntityIDEdgesConfig `json:"entity_id_edges,omitempty" schema:"type:object,description:EntityID virtual-edge synthesis for community detection; omit to keep defaults (siblings + system-peers on),category:advanced"`

	// Semantic co-location virtual-edge synthesis (Epic B B2, ADR-086). Omit to
	// keep the tier OFF (byte-identical to today). When enabled, mutual-kNN
	// semantic edges join the detection vote and the structural edge caps are
	// rebalanced so the new tier competes rather than being dominated.
	SemanticEdges *SemanticEdgesConfig `json:"semantic_edges,omitempty" schema:"type:object,description:Semantic co-location virtual-edge synthesis (mutual-kNN); omit to keep OFF (default),category:advanced"`

	// Parsed durations (set by ApplyDefaults)
	detectionInterval time.Duration
	// Resolved EntityID virtual-edge config (set by ApplyDefaults from
	// EntityIDEdges over a DefaultEntityIDProviderConfig baseline, or over the
	// semantic-enabled rebalanced baseline when SemanticEdges is enabled).
	entityIDEdges clustering.EntityIDProviderConfig
	// Resolved semantic-edge config (set by ApplyDefaults from SemanticEdges).
	semanticEdges resolvedSemanticEdges
}

// EntityIDEdgesConfig is the operator-facing shape for community detection's
// EntityID virtual-edge synthesis (gh#461). The two toggles are pointers so the
// config is tri-state: nil (unset) resolves to the built-in default, and only an
// explicit true/false overrides it — omitting the block therefore preserves the
// current behavior (both ON) rather than silently disabling synthesis. Numeric
// fields default when zero (mirrors clustering.NewEntityIDProvider).
type EntityIDEdgesConfig struct {
	IncludeSiblings    *bool   `json:"include_siblings,omitempty" schema:"type:bool,description:Synthesize sibling edges between entities sharing the 5-part type prefix (default true); set false to run detection on explicit topology alone"`
	IncludeSystemPeers *bool   `json:"include_system_peers,omitempty" schema:"type:bool,description:Synthesize system-peer edges between entities sharing the same system (default true)"`
	SiblingWeight      float64 `json:"sibling_weight,omitempty" schema:"type:number,description:Edge weight for synthesized sibling edges (default 0.7)"`
	MaxSiblings        int     `json:"max_siblings,omitempty" schema:"type:int,description:Max sibling neighbors synthesized per entity (default 10)"`
	SystemPeerWeight   float64 `json:"system_peer_weight,omitempty" schema:"type:number,description:Edge weight for synthesized system-peer edges (default 0.3)"`
	MaxSystemPeers     int     `json:"max_system_peers,omitempty" schema:"type:int,description:Max system-peer neighbors synthesized per entity (default 15)"`
}

// rejectUnknownEntityIDEdgeKeys strict-decodes the entity_id_edges block and
// rejects any key that does not bind, so an operator's toggle typo (e.g.
// "include_sibling" or "disable_siblings") fails loudly at load instead of being
// silently dropped by encoding/json — which would leave synthesis at its default
// (ON) and the gh#461 collapse in place with no error. Mirrors
// inference.RejectUnknownKeys for anomaly_config (ADR-054, no-silent-drop).
func rejectUnknownEntityIDEdgeKeys(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var probe EntityIDEdgesConfig
	if err := dec.Decode(&probe); err != nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "graphclustering", "rejectUnknownEntityIDEdgeKeys",
			fmt.Sprintf("entity_id_edges has a key that does not bind and would be silently ignored at runtime (gh#461/ADR-054): %v", err))
	}
	return nil
}

// SemanticEdgesConfig is the operator-facing block for community detection's
// semantic co-location virtual-edge synthesis (Epic B B2, ADR-086). It is a
// strict-decoded block (see rejectUnknownSemanticEdgeKeys) so an operator's typo
// on the enable toggle fails loudly rather than silently leaving the feature
// off. Omitting the block keeps the tier OFF and the detection edge set exactly
// what it is today (the gh#461 default-preservation invariant).
//
// Numeric fields default when zero (mirrors clustering.NewSemanticEdgeProvider);
// the starting values are EMPIRICAL (ADR-086), tuned against
// partition_colocation_mean — recorded as a starting point, not asserted final.
type SemanticEdgesConfig struct {
	EnableSemanticEdges         bool    `json:"enable_semantic_edges,omitempty" schema:"type:bool,description:Enable semantic co-location (mutual-kNN) virtual edges in community detection (default false; enabling also rebalances the structural edge caps so the tier competes)"`
	SemanticSimilarityThreshold float64 `json:"semantic_similarity_threshold,omitempty" schema:"type:number,description:Minimum cosine similarity for a candidate to count toward an entity's top-k semantic neighbors (starting value 0.75)"`
	SemanticMaxNeighbors        int     `json:"semantic_max_neighbors,omitempty" schema:"type:int,description:Mutual-kNN k — per-direction top-k candidate set size; bounds per-entity semantic degree (starting value 8)"`
	SemanticEdgeWeight          float64 `json:"semantic_edge_weight,omitempty" schema:"type:number,description:Weight of a synthesized mutual-kNN semantic edge (starting value 0.9; below explicit 1.0, above the rebalanced structural tiers)"`
}

// resolvedSemanticEdges is the ApplyDefaults-resolved semantic-edge config the
// provider chain consumes. enabled=false means the tier is OFF and the chain is
// unchanged (two providers, not three).
type resolvedSemanticEdges struct {
	enabled   bool
	threshold float64
	k         int
	weight    float64
}

// resolve maps the operator block onto the resolved semantic-edge config,
// applying the empirical starting values for any unset numeric. A nil receiver
// resolves to enabled=false — the load-bearing "omitted block == feature OFF"
// invariant.
func (s *SemanticEdgesConfig) resolve() resolvedSemanticEdges {
	r := resolvedSemanticEdges{
		threshold: clustering.DefaultSemanticThreshold,
		k:         clustering.DefaultSemanticMaxNeighbors,
		weight:    clustering.DefaultSemanticEdgeWeight,
	}
	if s == nil {
		return r
	}
	r.enabled = s.EnableSemanticEdges
	if s.SemanticSimilarityThreshold > 0 {
		r.threshold = s.SemanticSimilarityThreshold
	}
	if s.SemanticMaxNeighbors > 0 {
		r.k = s.SemanticMaxNeighbors
	}
	if s.SemanticEdgeWeight > 0 {
		r.weight = s.SemanticEdgeWeight
	}
	return r
}

// rejectUnknownSemanticEdgeKeys strict-decodes the semantic_edges block and
// rejects any key that does not bind, so an operator enabling the feature via a
// typo'd key (e.g. "enable_semantic_edge" singular) fails loudly at load rather
// than silently leaving the tier OFF. Mirrors rejectUnknownEntityIDEdgeKeys /
// inference.RejectUnknownKeys (ADR-054, no-silent-drop).
func rejectUnknownSemanticEdgeKeys(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var probe SemanticEdgesConfig
	if err := dec.Decode(&probe); err != nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "graphclustering", "rejectUnknownSemanticEdgeKeys",
			fmt.Sprintf("semantic_edges has a key that does not bind and would be silently ignored at runtime (ADR-086/ADR-054): %v", err))
	}
	return nil
}

// Semantic-enabled structural rebalance (Epic B B2, ADR-086 / design.md). When
// the semantic tier is ON, the sibling/system-peer caps and system-peer weight
// tighten so a third (semantic) voting tier competes rather than being drowned
// out by an ever-larger structural total. EMPIRICAL starting values, tuned
// against partition_colocation_mean — NOT final. These apply ONLY when the
// operator enables semantic edges; an explicit entity_id_edges override still
// wins (resolveOver applies operator overrides over this baseline).
const (
	semanticEnabledSiblingWeight    = 0.7 // unchanged from today's default
	semanticEnabledMaxSiblings      = 5   // 10 -> 5
	semanticEnabledSystemPeerWeight = 0.2 // 0.3 -> 0.2
	semanticEnabledMaxSystemPeers   = 8   // 15 -> 8
)

// semanticEnabledEntityIDBaseline is the EntityID provider baseline used when
// the semantic tier is enabled: the rebalanced structural caps/weights above,
// with synthesis still ON by default.
func semanticEnabledEntityIDBaseline() clustering.EntityIDProviderConfig {
	return clustering.EntityIDProviderConfig{
		SiblingWeight:      semanticEnabledSiblingWeight,
		MaxSiblings:        semanticEnabledMaxSiblings,
		IncludeSiblings:    true,
		IncludeSystemPeers: true,
		SystemPeerWeight:   semanticEnabledSystemPeerWeight,
		MaxSystemPeers:     semanticEnabledMaxSystemPeers,
	}
}

// removedConfigFields maps every field withdrawn from this component's operator
// surface to the guidance that replaces it. encoding/json silently DROPS a key with
// no matching struct field, so without this probe an operator upgrading past ADR-083
// would keep a plausible-looking `index_lag_tolerance: 250` in their config, see no
// error, and get the strict exact gate — the precise silent behavior change that
// motivated the gate work in the first place. A removed knob must fail at load.
//
// The probe is targeted rather than a blanket DisallowUnknownFields on Config: it
// gives the operator the replacement field by name, and it cannot reject unrelated
// keys that other tooling may legitimately carry in the block.
var removedConfigFields = map[string]string{
	"index_lag_tolerance": `removed (ADR-083, BREAKING): the bounded-lag tolerance was replaced by the wall-time "max_staleness", which has itself since been removed — readiness now gates on index HEALTH alone and no view-age tolerance exists to configure. Delete the field. A healthy index serves however far behind it is, and the view age of each detection run is reported on the semstreams_graph_clustering_staleness_at_detection_ms gauge`,
	"max_staleness":       `removed (BREAKING): readiness gates on index HEALTH alone — a fresh status feed, an interpretable state, no degraded/reset_required hard stop, and a completed initial build. There is no view-age tolerance left to declare, because a healthy index serves however far behind it is: lag is a property to REPORT on the answer, not a fault to withhold it for. Delete the field. The view age each detection run proceeded at is reported on the semstreams_graph_clustering_staleness_at_detection_ms gauge — every run, not only the ones a tolerance admitted`,
}

// rejectRemovedConfigKeys fails the load when a withdrawn field is present, naming
// its replacement. Called at factory time so the failure surfaces at startup with the
// component name attached, not as a mysterious behavior change hours later.
func rejectRemovedConfigKeys(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var present map[string]json.RawMessage
	if err := json.Unmarshal(raw, &present); err != nil {
		// Not an object, or malformed — the caller's own decode reports that.
		return nil
	}
	for field, guidance := range removedConfigFields {
		if _, found := present[field]; found {
			return errs.WrapInvalid(errs.ErrInvalidConfig, "graphclustering", "rejectRemovedConfigKeys",
				fmt.Sprintf("config field %q was %s", field, guidance))
		}
	}
	return nil
}

// resolve maps the operator config onto a clustering.EntityIDProviderConfig,
// starting from the built-in defaults (synthesis ON) so that an unset toggle or
// zero numeric keeps the default. A nil receiver resolves to the defaults
// verbatim — the load-bearing "omitted config == current behavior" invariant.
func (e *EntityIDEdgesConfig) resolve() clustering.EntityIDProviderConfig {
	return e.resolveOver(clustering.DefaultEntityIDProviderConfig())
}

// resolveOver is resolve with an explicit baseline, so the semantic-enabled path
// can supply the rebalanced structural caps/weights baseline while operator
// overrides still win. A nil receiver returns the baseline verbatim. The
// zero-arg resolve() over DefaultEntityIDProviderConfig() preserves today's
// behavior exactly (the gh#461 default-preservation invariant).
func (e *EntityIDEdgesConfig) resolveOver(baseline clustering.EntityIDProviderConfig) clustering.EntityIDProviderConfig {
	cfg := baseline
	if e == nil {
		return cfg
	}
	if e.IncludeSiblings != nil {
		cfg.IncludeSiblings = *e.IncludeSiblings
	}
	if e.IncludeSystemPeers != nil {
		cfg.IncludeSystemPeers = *e.IncludeSystemPeers
	}
	if e.SiblingWeight > 0 {
		cfg.SiblingWeight = e.SiblingWeight
	}
	if e.MaxSiblings > 0 {
		cfg.MaxSiblings = e.MaxSiblings
	}
	if e.SystemPeerWeight > 0 {
		cfg.SystemPeerWeight = e.SystemPeerWeight
	}
	if e.MaxSystemPeers > 0 {
		cfg.MaxSystemPeers = e.MaxSystemPeers
	}
	return cfg
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

	// Validate COMMUNITY_INDEX output exists
	hasCommunityIndex := false
	for _, output := range c.Ports.Outputs {
		if output.Subject == graph.BucketCommunityIndex {
			hasCommunityIndex = true
			break
		}
	}
	if !hasCommunityIndex {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", fmt.Sprintf("%s output required", graph.BucketCommunityIndex))
	}

	// Validate detection interval (parsed duration must be positive)
	if c.detectionInterval <= 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "detection_interval must be greater than 0")
	}

	// Validate min community size
	if c.MinCommunitySize <= 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "min_community_size must be greater than 0")
	}

	// Validate max iterations
	if c.MaxIterations <= 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "max_iterations must be greater than 0")
	}

	// Anomaly detection requires structural analysis
	if c.EnableAnomalyDetection && !c.EnableStructural {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "enable_anomaly_detection requires enable_structural to be true")
	}

	// Validate anomaly config if anomaly detection is enabled
	if c.EnableAnomalyDetection && c.AnomalyConfig.Enabled {
		if err := c.AnomalyConfig.Validate(); err != nil {
			return errs.Wrap(err, "Config", "Validate", "anomaly_config")
		}
	}

	return nil
}

// DetectionInterval returns the parsed detection interval duration
func (c *Config) DetectionInterval() time.Duration {
	return c.detectionInterval
}

// ApplyDefaults sets default values for configuration
func (c *Config) ApplyDefaults() {
	// Parse detection interval from string
	if c.DetectionIntervalStr != "" {
		if d, err := time.ParseDuration(c.DetectionIntervalStr); err == nil {
			c.detectionInterval = d
		}
	}
	if c.detectionInterval == 0 {
		c.detectionInterval = 30 * time.Second
	}

	if c.BatchSize == 0 {
		c.BatchSize = 100
	}
	// EnableLLM defaults to false (zero value)
	if c.MinCommunitySize == 0 {
		c.MinCommunitySize = 3
	}
	if c.MaxIterations == 0 {
		c.MaxIterations = 100
	}
	if c.EnhancementWorkers == 0 {
		c.EnhancementWorkers = 5 // Increased from default 3 for better parallelism
	}
	// Structural analysis defaults
	if c.EnableStructural {
		if c.PivotCount == 0 {
			c.PivotCount = 16 // Default from structural package
		}
		if c.MaxHopDistance == 0 {
			c.MaxHopDistance = 10 // Default maximum BFS depth
		}
	}
	// Anomaly detection defaults
	if c.EnableAnomalyDetection {
		c.AnomalyConfig.ApplyDefaults()
	}

	// Dependency startup defaults
	if c.StartupAttempts == 0 {
		c.StartupAttempts = 30 // ~15 seconds with 500ms interval
	}
	if c.StartupInterval == 0 {
		c.StartupInterval = 500 // milliseconds
	}

	// Resolve semantic co-location virtual-edge synthesis (Epic B B2, ADR-086).
	// Nil SemanticEdges resolves to enabled=false — omitting the block keeps the
	// tier OFF and the detection chain unchanged.
	c.semanticEdges = c.SemanticEdges.resolve()

	// Resolve EntityID virtual-edge synthesis (gh#461). Nil EntityIDEdges
	// resolves to the built-in defaults (synthesis ON) — omitting the block
	// preserves current behavior. When the semantic tier is enabled, resolve over
	// the rebalanced structural baseline instead (operator overrides still win);
	// when disabled, this is the exact same call as before — byte-identical.
	if c.semanticEdges.enabled {
		c.entityIDEdges = c.EntityIDEdges.resolveOver(semanticEnabledEntityIDBaseline())
	} else {
		c.entityIDEdges = c.EntityIDEdges.resolve()
	}

	// Add optional output ports based on enabled features
	if c.Ports != nil {
		// Add STRUCTURAL_INDEX output when structural analysis is enabled
		if c.EnableStructural {
			hasStructural := false
			for _, o := range c.Ports.Outputs {
				if o.Subject == graph.BucketStructuralIndex {
					hasStructural = true
					break
				}
			}
			if !hasStructural {
				c.Ports.Outputs = append(c.Ports.Outputs, component.PortDefinition{
					Name:    "structural_index",
					Type:    "kv-write",
					Subject: graph.BucketStructuralIndex,
				})
			}
		}

		// Add ANOMALY_INDEX output when anomaly detection is enabled
		if c.EnableAnomalyDetection {
			hasAnomaly := false
			for _, o := range c.Ports.Outputs {
				if o.Subject == graph.BucketAnomalyIndex {
					hasAnomaly = true
					break
				}
			}
			if !hasAnomaly {
				c.Ports.Outputs = append(c.Ports.Outputs, component.PortDefinition{
					Name:    "anomaly_index",
					Type:    "kv-write",
					Subject: graph.BucketAnomalyIndex,
				})
			}
		}
	}

	if c.Ports == nil {
		// Apply full default port config
		defaultConf := DefaultConfig()
		c.Ports = defaultConf.Ports
	} else {
		// If ports exist but are empty, populate with defaults
		if len(c.Ports.Inputs) == 0 {
			c.Ports.Inputs = []component.PortDefinition{
				{
					Name:    "entity_watch",
					Type:    "kv-watch",
					Subject: graph.BucketEntityStates,
				},
			}
		}
		if len(c.Ports.Outputs) == 0 {
			c.Ports.Outputs = []component.PortDefinition{
				{
					Name:    "communities",
					Type:    "kv-write",
					Subject: graph.BucketCommunityIndex,
				},
			}
		}
	}
}

// DefaultConfig returns a valid default configuration
func DefaultConfig() Config {
	return Config{
		Ports: &component.PortConfig{
			Inputs: []component.PortDefinition{
				{
					Name:    "entity_watch",
					Type:    "kv-watch",
					Subject: graph.BucketEntityStates,
				},
			},
			Outputs: []component.PortDefinition{
				{
					Name:    "communities",
					Type:    "kv-write",
					Subject: graph.BucketCommunityIndex,
				},
			},
		},
		detectionInterval: 30 * time.Second,
		BatchSize:         100,
		EnableLLM:         false,
		MinCommunitySize:  3,
		MaxIterations:     100,
	}
}

// schema defines the configuration schema for graph-clustering component
var schema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Component implements the graph-clustering processor
type Component struct {
	// Component metadata
	name   string
	config Config

	// Dependencies
	natsClient    *natsclient.Client
	logger        *slog.Logger
	modelRegistry model.RegistryReader

	// Domain resources
	communityBucket jetstream.KeyValue
	entityBucket    jetstream.KeyValue
	outgoingBucket  jetstream.KeyValue
	incomingBucket  jetstream.KeyValue

	// Community detection
	detector clustering.CommunityDetector
	storage  *clustering.NATSCommunityStorage

	// Structural analysis (optional)
	structuralBucket  jetstream.KeyValue
	structuralStorage *structural.NATSStructuralIndexStorage
	graphProvider     clustering.Provider    // shared with detector
	previousKCore     *structural.KCoreIndex // for demotion detection

	// Anomaly detection (optional, requires structural)
	anomalyBucket       jetstream.KeyValue
	anomalyStorage      inference.Storage
	anomalyOrchestrator *inference.Orchestrator
	similarityFinder    inference.SimilarityFinder // for semantic gap detection

	// LLM enhancement (optional)
	enhancementWorker *clustering.EnhancementWorker
	llmClient         llm.Client

	// Review worker (optional, for anomaly approval workflow). The review
	// worker may use a dedicated LLM client when the operator binds
	// model.CapabilityAnomalyReview to a different endpoint than
	// community_summary; otherwise it shares llmClient.
	reviewWorker    *inference.ReviewWorker
	reviewLLMClient llm.Client // non-nil only when distinct from llmClient

	// Readiness gate (ADR-083 §3). statusWatcher holds graph-index's last-known
	// ADR-066 envelope from the GRAPH_STATUS KV bucket, so a detection tick decides
	// against held state instead of a per-tick request/reply that dies under exactly
	// the load that makes readiness interesting (gh#590). It is rebuilt on every
	// Start: a stopped Watcher cannot be restarted.
	statusWatcher *readiness.Watcher
	// embeddingStatusWatcher holds graph-embedding's last-known readiness envelope
	// from the SAME GRAPH_STATUS bucket (KeyGraphEmbedding), and is bound ONLY when
	// enable_semantic_edges is true — the second axis of the detection gate
	// (B2 §4). It never DEFERS a cycle: a cold embedding index degrades the cycle
	// to structural-only (semantic_edges_applied=false), it never withholds the
	// always-committing structural floor. Rebuilt on every Start like statusWatcher.
	embeddingStatusWatcher *readiness.Watcher
	// statusHeartbeat declares the producer's publish interval, which sets the
	// freshness window (readiness.FreshnessMultiplier x heartbeat). Zero means
	// readiness.DefaultHeartbeat. Unexported and not an operator knob (ADR-083 D2
	// keeps the multiplier constant); tests shorten it so a feed-dies assertion
	// costs milliseconds rather than three real heartbeats.
	statusHeartbeat time.Duration
	// lastDeferReason is the previous tick's gate outcome, used to log a VISIBLE
	// line on every transition without emitting one per tick forever. Guarded by
	// deferMu because Stop may race the detection goroutine's final tick.
	deferMu         sync.Mutex
	lastDeferReason graph.DeferReason
	// semanticProvider is the concrete SemanticEdgeProvider handle (also the top of
	// c.graphProvider's chain) when enable_semantic_edges is true, nil otherwise. It
	// is held typed so applySemanticGate can toggle it per cycle from embedding
	// readiness. Set under c.mu in initProviderAndDetector before the detection
	// goroutine launches, then only read/toggled by that goroutine (B3's enhancement
	// worker becomes a second reader; the toggle is atomic).
	semanticProvider *clustering.SemanticEdgeProvider
	// lastSemanticApplied tracks the previous cycle's semantic-tier verdict so a
	// structural-only <-> full TRANSITION logs one visible line rather than silence
	// or a per-tick flood. nil = no cycle observed yet. Guarded by semanticMu.
	semanticMu          sync.Mutex
	lastSemanticApplied *bool

	// Lifecycle state
	mu                sync.RWMutex
	running           bool
	initialized       bool
	startTime         time.Time
	wg                sync.WaitGroup
	cancel            context.CancelFunc
	lifecycleReporter component.LifecycleReporter

	// Metrics (atomic)
	messagesProcessed int64
	bytesProcessed    int64
	errors            int64
	lastActivity      atomic.Value // stores time.Time
	graphStatePoison  atomic.Pointer[graph.StateContractError]
	metrics           *clusteringMetrics

	// Query subscriptions (for cleanup)
	querySubscriptions []*natsclient.Subscription
}

// CreateGraphClustering is the factory function for creating graph-clustering components
func CreateGraphClustering(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	// Validate dependencies
	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "CreateGraphClustering", "factory", "NATSClient required")
	}
	natsClient := deps.NATSClient

	// Parse configuration
	var config Config
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &config); err != nil {
			return nil, errs.Wrap(err, "CreateGraphClustering", "factory", "config unmarshal")
		}
		// A field removed from the operator surface must fail the load, not be
		// dropped by encoding/json and silently change the gate (ADR-083 §3.2).
		if err := rejectRemovedConfigKeys(rawConfig); err != nil {
			return nil, errs.Wrap(err, "CreateGraphClustering", "factory", "removed config field")
		}
		// Reject phantom keys in anomaly_config that encoding/json silently drops
		// (ADR-054). Operator configs bypass the strict checked-in-config test, so
		// enforce the no-silent-drop contract here, at load time.
		var rawAnomaly struct {
			AnomalyConfig json.RawMessage `json:"anomaly_config"`
		}
		// rawConfig was already validated as JSON by the unmarshal above, so this
		// re-extraction into a RawMessage cannot fail; the guard is purely defensive.
		if err := json.Unmarshal(rawConfig, &rawAnomaly); err == nil {
			if err := inference.RejectUnknownKeys(rawAnomaly.AnomalyConfig); err != nil {
				return nil, errs.Wrap(err, "CreateGraphClustering", "factory", "anomaly_config")
			}
		}
		// Same no-silent-drop guard for entity_id_edges (gh#461): a toggle typo
		// must fail loudly, not leave synthesis silently at its default.
		var rawEdges struct {
			EntityIDEdges json.RawMessage `json:"entity_id_edges"`
		}
		if err := json.Unmarshal(rawConfig, &rawEdges); err == nil {
			if err := rejectUnknownEntityIDEdgeKeys(rawEdges.EntityIDEdges); err != nil {
				return nil, errs.Wrap(err, "CreateGraphClustering", "factory", "entity_id_edges")
			}
		}
		// Same no-silent-drop guard for semantic_edges (ADR-086): an enable-toggle
		// typo must fail loudly, not silently leave the feature OFF.
		var rawSemantic struct {
			SemanticEdges json.RawMessage `json:"semantic_edges"`
		}
		if err := json.Unmarshal(rawConfig, &rawSemantic); err == nil {
			if err := rejectUnknownSemanticEdgeKeys(rawSemantic.SemanticEdges); err != nil {
				return nil, errs.Wrap(err, "CreateGraphClustering", "factory", "semantic_edges")
			}
		}
	} else {
		config = DefaultConfig()
	}

	// Apply defaults and validate
	config.ApplyDefaults()
	if err := config.Validate(); err != nil {
		return nil, errs.Wrap(err, "CreateGraphClustering", "factory", "config validation")
	}

	// Create logger with component context
	logger := deps.GetLoggerWithComponent("graph-clustering")

	// Create component
	comp := &Component{
		name:          "graph-clustering",
		config:        config,
		natsClient:    natsClient,
		logger:        logger,
		modelRegistry: deps.ModelRegistry,
		metrics:       getMetrics(deps.MetricsRegistry),
	}

	// Expose the #618 semantic_edges_applied gauge ONLY when the tier is enabled, so a
	// disabled deployment does not scrape a misleading 0 that reads as enabled-but-cold
	// (Codex P2#4). The gauge object always exists (getMetrics creates it); this opts
	// it into the registry for enabled deployments only.
	if comp.config.semanticEdges.enabled && comp.metrics != nil {
		comp.metrics.registerSemanticEdgesApplied(deps.MetricsRegistry)
	}

	// Initialize last activity
	comp.lastActivity.Store(time.Now())

	return comp, nil
}

// Register registers the graph-clustering factory with the component registry
func Register(registry *component.Registry) error {
	return registry.RegisterFactory("graph-clustering", &component.Registration{
		Name:        "graph-clustering",
		Type:        "processor",
		Protocol:    "nats",
		Domain:      "graph",
		Description: "Graph community detection and clustering processor",
		Version:     "1.0.0",
		Schema:      schema,
		Factory:     CreateGraphClustering,
	})
}

// ============================================================================
// Discoverable Interface (6 methods)
// ============================================================================

// Meta returns component metadata
func (c *Component) Meta() component.Metadata {
	return component.Metadata{
		Name:        "graph-clustering",
		Type:        "processor",
		Description: "Graph community detection and clustering processor",
		Version:     "1.0.0",
	}
}

// InputPorts returns input port definitions
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

// OutputPorts returns output port definitions
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
		if c.graphStatePoison.Load() != nil {
			status = graph.IndexStateResetRequired
			lastErr = graph.ErrorCodeGraphStateResetRequired
		}
		if errorCount > 0 {
			lastErr = "errors occurred during processing"
		}
	}

	return component.HealthStatus{
		Healthy:    c.running && errorCount == 0 && c.graphStatePoison.Load() == nil,
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
	c.logger.Info("component initialized", slog.String("component", "graph-clustering"))

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
		return errs.WrapFatal(errs.ErrInvalidConfig, "Component", "Start", "component not initialized")
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

	// Create COMMUNITY_INDEX bucket (we are the WRITER)
	communityBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketCommunityIndex,
		Description: "Community detection index",
	})
	if err != nil {
		cancel()
		if ctx.Err() != nil {
			return errs.Wrap(ctx.Err(), "Component", "Start", "context cancelled during bucket creation")
		}
		return errs.Wrap(err, "Component", "Start", fmt.Sprintf("KV bucket creation: %s", graph.BucketCommunityIndex))
	}
	c.communityBucket = communityBucket

	// Create community storage for the detector
	c.storage = clustering.NewNATSCommunityStorage(communityBucket)

	// Bind the readiness feed BEFORE the detection loop can tick, so the first tick
	// decides against a delivered envelope rather than an artificial unknown. Start
	// never fails on an absent bucket or key: a deployment running clustering without
	// graph-index is a legitimate shape whose readiness is simply unknown, and
	// allow_ungated_reads — not a fabricated envelope — is what covers it.
	if err := c.startStatusWatcher(ctx); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "readiness status watcher")
	}

	// Initialize lifecycle reporter and wait for dependencies
	c.initLifecycleReporter(ctx)

	if err := c.waitForDependencies(ctx); err != nil {
		cancel()
		return err
	}

	// Set up query handlers. Contract poison is detected at consume time on
	// the input path (poison-response-scoping D7): every authoritative entity
	// read flows through the latch-wired querier built below, so there is no
	// dedicated ENTITY_STATES contract watcher and no per-write fan-out to
	// clustering.
	if err := c.setupQueryHandlers(ctx); err != nil {
		cancel()
		return errs.Wrap(err, "Component", "Start", "setup query handlers")
	}

	// Create graph provider and detector. The sticky poison latch survives a
	// same-instance Stop/Start (only a process restart clears it): a poisoned
	// component restarts its query surface so it can return the typed reset
	// requirement, but no detector/enhancement/action worker may run against
	// incompatible state.
	c.initProviderAndDetector()
	poisoned := c.graphStatePoison.Load() != nil

	// Initialize structural analysis if enabled
	if !poisoned && c.config.EnableStructural {
		if err := c.initStructural(ctx); err != nil {
			c.logger.Warn("failed to initialize structural analysis, continuing without it",
				slog.Any("error", err))
		}
	}

	// Initialize anomaly detection if enabled (requires structural)
	if !poisoned && c.config.EnableAnomalyDetection && c.structuralStorage != nil {
		if err := c.initAnomalyDetection(ctx); err != nil {
			c.logger.Warn("failed to initialize anomaly detection, continuing without it",
				slog.Any("error", err))
		}
	}

	// Start LLM enhancement worker if enabled
	if !poisoned && c.config.EnableLLM {
		if err := c.startEnhancementWorker(ctx, c.graphProvider); err != nil {
			c.logger.Warn("failed to start enhancement worker, continuing without LLM",
				slog.Any("error", err))
		}
	}

	// Start review worker if enabled (for anomaly approval workflow)
	if !poisoned && c.config.AnomalyConfig.Review.Enabled && c.anomalyStorage != nil {
		if err := c.startReviewWorker(ctx); err != nil {
			c.logger.Warn("failed to start review worker, continuing without anomaly review",
				slog.Any("error", err))
		}
	}

	// Mark as running
	c.running = true
	c.startTime = time.Now()

	// Start detection loop goroutine
	if !poisoned {
		c.wg.Add(1)
		go c.runDetectionLoop(ctx)
	}

	c.logger.Info("component started",
		slog.String("component", "graph-clustering"),
		slog.Time("start_time", c.startTime),
		slog.Duration("detection_interval", c.config.DetectionInterval()),
		slog.Bool("enable_llm", c.config.EnableLLM),
		slog.Bool("enable_structural", c.config.EnableStructural),
		slog.Bool("enable_anomaly_detection", c.config.EnableAnomalyDetection))

	return nil
}

// Stop gracefully shuts down the component
func (c *Component) Stop(timeout time.Duration) error {
	c.mu.Lock()

	if !c.running {
		c.mu.Unlock()
		return nil // Already stopped
	}

	// Unsubscribe from query handlers
	for _, sub := range c.querySubscriptions {
		if sub != nil {
			if err := sub.Unsubscribe(); err != nil {
				c.logger.Warn("query subscription unsubscribe error", slog.Any("error", err))
			}
		}
	}
	c.querySubscriptions = nil

	// Stop review worker if running
	if c.reviewWorker != nil {
		if err := c.reviewWorker.Stop(); err != nil {
			c.logger.Warn("review worker stop error", slog.Any("error", err))
		}
	}

	// Stop enhancement worker if running
	if c.enhancementWorker != nil {
		if err := c.enhancementWorker.Stop(); err != nil {
			c.logger.Warn("enhancement worker stop error", slog.Any("error", err))
		}
	}

	// Close LLM client if present
	if c.llmClient != nil {
		if err := c.llmClient.Close(); err != nil {
			c.logger.Warn("LLM client close error", slog.Any("error", err))
		}
	}

	// Close the dedicated review LLM client if distinct from the shared one.
	if c.reviewLLMClient != nil {
		if err := c.reviewLLMClient.Close(); err != nil {
			c.logger.Warn("review LLM client close error", slog.Any("error", err))
		}
	}

	// Cancel context
	if c.cancel != nil {
		c.cancel()
	}

	// Detach the readiness watchers under the lock but join them after unlocking:
	// Stop blocks until the watch goroutine exits, and a stopped Watcher cannot be
	// restarted, so the next Start builds fresh ones.
	statusWatcher := c.statusWatcher
	c.statusWatcher = nil
	embeddingStatusWatcher := c.embeddingStatusWatcher
	c.embeddingStatusWatcher = nil

	c.running = false
	c.mu.Unlock()

	if statusWatcher != nil {
		statusWatcher.Stop()
	}
	if embeddingStatusWatcher != nil {
		embeddingStatusWatcher.Stop()
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		c.logger.Info("component stopped gracefully", slog.String("component", "graph-clustering"))
		return nil
	case <-time.After(timeout):
		c.logger.Warn("component stop timed out", slog.String("component", "graph-clustering"))
		return errs.WrapTransient(errors.New("timeout"), "Component", "Stop", "graceful shutdown timeout")
	}
}

// initLifecycleReporter initializes the lifecycle reporter for component status tracking.
func (c *Component) initLifecycleReporter(ctx context.Context) {
	statusBucket, err := c.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket:      graph.BucketComponentStatus,
		Description: "Component lifecycle status",
	})
	if err != nil {
		c.logger.Warn("failed to create status bucket, lifecycle reporting disabled",
			slog.Any("error", err))
		c.lifecycleReporter = component.NewNoOpLifecycleReporter()
		return
	}
	c.lifecycleReporter = component.NewKVLifecycleReporter(statusBucket, "graph-clustering", c.logger)
}

// waitForDependencies waits for all required KV buckets and stores references.
func (c *Component) waitForDependencies(ctx context.Context) error {
	js, err := c.natsClient.JetStream()
	if err != nil {
		return errs.Wrap(err, "Component", "waitForDependencies", "JetStream connection")
	}

	watcherCfg := resource.DefaultConfig()
	watcherCfg.StartupAttempts = c.config.StartupAttempts
	watcherCfg.StartupInterval = time.Duration(c.config.StartupInterval) * time.Millisecond
	watcherCfg.Logger = c.logger

	// Wait for ENTITY_STATES bucket
	entityBucket, err := c.waitForBucket(ctx, js, graph.BucketEntityStates, watcherCfg)
	if err != nil {
		return err
	}
	c.entityBucket = entityBucket

	// Wait for OUTGOING_INDEX bucket
	outgoingBucket, err := c.waitForBucket(ctx, js, graph.BucketOutgoingIndex, watcherCfg)
	if err != nil {
		return err
	}
	c.outgoingBucket = outgoingBucket

	// Wait for INCOMING_INDEX bucket
	incomingBucket, err := c.waitForBucket(ctx, js, graph.BucketIncomingIndex, watcherCfg)
	if err != nil {
		return err
	}
	c.incomingBucket = incomingBucket

	return nil
}

// waitForBucket waits for a KV bucket to become available and returns it.
func (c *Component) waitForBucket(ctx context.Context, js jetstream.JetStream, bucketName string, cfg resource.Config) (jetstream.KeyValue, error) {
	if err := c.lifecycleReporter.ReportStage(ctx, "waiting_for_"+bucketName); err != nil {
		c.logger.Debug("failed to report lifecycle stage", slog.String("stage", "waiting_for_"+bucketName), slog.Any("error", err))
	}

	watcher := resource.NewWatcher(
		bucketName,
		func(checkCtx context.Context) error {
			_, err := js.KeyValue(checkCtx, bucketName)
			return err
		},
		cfg,
	)

	if !watcher.WaitForStartup(ctx) {
		return nil, errs.WrapTransient(
			fmt.Errorf("bucket %s not available after %d attempts", bucketName, c.config.StartupAttempts),
			"Component", "waitForBucket", "dependency not available",
		)
	}

	bucket, err := js.KeyValue(ctx, bucketName)
	if err != nil {
		return nil, errs.Wrap(err, "Component", "waitForBucket", fmt.Sprintf("get %s bucket", bucketName))
	}
	return bucket, nil
}

// initProviderAndDetector creates the graph provider and community detector.
func (c *Component) initProviderAndDetector() {
	provider := newKVProvider(c.entityBucket, c.outgoingBucket, c.incomingBucket, c.logger)

	// gh#461: use the operator-resolved config (defaults ON, but overridable)
	// instead of the hardcoded defaults, so a homogeneous entity family can run
	// community detection on explicit topology alone.
	entityIDProvider := clustering.NewEntityIDProvider(
		provider,
		c.config.entityIDEdges,
		c.logger,
	)

	// Epic B B2 (ADR-086): when the semantic-edge tier is enabled, decorate the
	// EntityID provider with mutual-kNN semantic edges so the chain becomes
	// kvProvider -> EntityIDProvider -> SemanticEdgeProvider. When disabled the
	// chain is exactly the two providers it is today (byte-identical). The
	// mutual-kNN candidates come from the component's existing similarity finder
	// path (no second RPC — §1.2).
	var detectorProvider clustering.Provider = entityIDProvider
	if c.config.semanticEdges.enabled {
		detectorProvider = c.wrapSemanticEdges(entityIDProvider)
	}

	entityQuerier := c.newEntityStateQuerier()
	summarizer := clustering.NewStatisticalSummarizer()

	// The semantic-edge decorator is scoped to COMMUNITY DETECTION only (ADR-086):
	// only the LPA detector votes over the mutual-kNN edges. Everything else that
	// reads c.graphProvider — structural k-core/pivot computation
	// (structural.go), anomaly inputs derived from those indices, and the LLM
	// enhancement worker — MUST see the base EntityID chain, so enabling semantic
	// edges does not silently shift k-core degrees, pivot distances, or anomaly
	// scores. When the tier is OFF detectorProvider == entityIDProvider, so both
	// the detector and c.graphProvider get the same base and the wiring is
	// byte-identical to today.
	detector := clustering.NewLPADetector(detectorProvider, c.storage).
		WithLogger(c.logger).
		WithMaxIterations(c.config.MaxIterations).
		WithLevels(3).
		WithSummarizer(summarizer)

	detector.SetEntityProvider(entityQuerier)
	c.detector = detector
	c.graphProvider = entityIDProvider
}

// wrapSemanticEdges decorates the EntityID provider with the mutual-kNN semantic
// tier, sourcing candidates from the existing graph.embedding.query.similar
// finder via a thin readiness-aware adapter (no second similarity RPC — B2 §1.2;
// the fail-open classification is §5.1, inside semanticFinderAdapter). The
// resolved WeightConfig carries the rebalanced structural weights alongside the
// semantic weight so GetEdgeWeight resolves max-not-sum across all four tiers in
// one place (B2 §2). It also stores the concrete provider on c.semanticProvider
// so applySemanticGate can toggle it per cycle from embedding readiness (§4).
func (c *Component) wrapSemanticEdges(entityIDProvider *clustering.EntityIDProvider) clustering.Provider {
	finder := c.initQuerySimilarityFinder()
	if finder == nil {
		// No NATS client -> no similarity path. Degrade to structural-only rather
		// than failing Start; the tier is additive over an always-valid floor.
		// c.semanticProvider stays nil, so applySemanticGate is a no-op and the
		// deployment runs permanent structural-only.
		c.logger.Warn("semantic edges enabled but similarity finder unavailable, running structural-only")
		return entityIDProvider
	}
	weights := clustering.WeightConfig{
		SiblingWeight:    c.config.entityIDEdges.SiblingWeight,
		SystemPeerWeight: c.config.entityIDEdges.SystemPeerWeight,
		SemanticWeight:   c.config.semanticEdges.weight,
	}
	provider := clustering.NewSemanticEdgeProvider(
		entityIDProvider,
		semanticFinderAdapter{finder: finder},
		weights,
		clustering.SemanticEdgeParams{
			K:         c.config.semanticEdges.k,
			Threshold: c.config.semanticEdges.threshold,
		},
		c.logger,
	)
	c.semanticProvider = provider
	return provider
}

// newEntityStateQuerier builds the consume-path ENTITY_STATES reader with the
// component's sticky poison latch wired in. Every clustering read of
// authoritative entity bytes flows through a querier built here (detector
// summarization/corpus reads and LLM enhancement reads), so a poisoned value
// observed at consume time latches the whole-view reset-required projection
// state without a dedicated contract watcher (poison-response-scoping D7).
func (c *Component) newEntityStateQuerier() *kvEntityQuerier {
	querier := newKVEntityQuerier(c.entityBucket, c.logger)
	querier.latchPoison = c.latchGraphStatePoison
	return querier
}

// reportStage safely reports a lifecycle stage change.
// Errors are logged but do not interrupt processing.
func (c *Component) reportStage(ctx context.Context, stage string) {
	if c.lifecycleReporter != nil {
		if err := c.lifecycleReporter.ReportStage(ctx, stage); err != nil {
			c.logger.Debug("failed to report lifecycle stage",
				slog.String("stage", stage),
				slog.Any("error", err))
		}
	}
}

// runDetectionLoop runs community detection on a timer
func (c *Component) runDetectionLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.DetectionInterval())
	defer ticker.Stop()

	// Report initial idle state
	c.reportStage(ctx, "idle")

	c.logger.Info("detection loop started",
		slog.Duration("interval", c.config.DetectionInterval()))

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("detection loop stopping")
			return
		case <-ticker.C:
			// Double-check context before starting new detection
			// This prevents starting a new cycle if shutdown just began
			if ctx.Err() != nil {
				c.logger.Debug("detection loop stopping - context cancelled")
				return
			}
			// Cutover-readiness gate (gh#474 Codex #6): community detection reads
			// INCOMING_INDEX directly, so skip this cycle while graph-index is still
			// building or degraded — running would derive communities from partial
			// topology. The next tick retries once the index is ready.
			decision := c.evaluateReadiness()
			if !decision.proceed {
				c.recordDefer(decision)
				continue
			}
			c.observeDetectionRun(decision)
			// Set the semantic tier active/inactive for THIS cycle from the
			// embedding-readiness verdict BEFORE detection reads the provider (B2 §4).
			c.applySemanticGate(decision)
			c.runCommunityDetection(ctx)
			// Stamp the #618 signal AFTER detection, from the ACTUAL completed mode
			// (an intended-active cycle that aborted mid-build degraded to
			// structural-only). Codex P1#2: report what happened, not the preflight
			// intent.
			c.recordSemanticMode()
		}
	}
}

// startStatusWatcher binds the consumer side of the ADR-083 readiness contract: a
// watch on graph-index's key in the GRAPH_STATUS KV bucket, whose held envelope every
// detection tick reads locally. It replaces the per-tick
// `graph.index.query.status` request/reply, which travelled the same connection as
// the ENTITY_STATES firehose in a single-binary deployment and so timed out under
// load, failing closed with a log line indistinguishable from a genuine not-ready
// (gh#590). There is no request/reply fallback by design — two distribution paths
// would be two things to keep honest.
//
// Only wiring errors (nil source, empty key) fail here; an absent bucket or key is a
// runtime UNKNOWN, not a startup error.
func (c *Component) startStatusWatcher(ctx context.Context) error {
	heartbeat := c.statusHeartbeat
	if heartbeat <= 0 {
		heartbeat = readiness.DefaultHeartbeat
	}
	watcher := readiness.NewWatcher(c.natsClient, readiness.KeyGraphIndex,
		readiness.WithHeartbeat(heartbeat),
		readiness.WithLogger(c.logger))
	if err := watcher.Start(ctx); err != nil {
		return fmt.Errorf("start readiness watcher on %s/%s: %w",
			readiness.BucketGraphStatus, readiness.KeyGraphIndex, err)
	}
	c.statusWatcher = watcher

	// Second readiness axis (B2 §4.1): graph-embedding's status key, bound ONLY
	// when the semantic-edge tier is enabled so an unopted deployment starts no
	// wasted subscription. A cold embedding index does not defer the cycle — it
	// drops the cycle to structural-only — so an absent/unknown feed here is
	// benign, exactly like the index watcher's absent case.
	if c.config.semanticEdges.enabled {
		embWatcher := readiness.NewWatcher(c.natsClient, readiness.KeyGraphEmbedding,
			readiness.WithHeartbeat(heartbeat),
			readiness.WithLogger(c.logger))
		if err := embWatcher.Start(ctx); err != nil {
			return fmt.Errorf("start readiness watcher on %s/%s: %w",
				readiness.BucketGraphStatus, readiness.KeyGraphEmbedding, err)
		}
		c.embeddingStatusWatcher = embWatcher
	}
	return nil
}

// gateDecision is one tick's readiness verdict plus everything needed to attribute it
// without correlating log lines.
type gateDecision struct {
	// proceed reports whether community detection may run this tick.
	proceed bool
	// reason is the typed cause of a defer, and the defer_total label value. Empty
	// (graph.DeferNone) on a proceed.
	reason graph.DeferReason
	// reading is the held readiness state the verdict was reached from.
	reading readiness.Reading
	// ungated marks a proceed that happened ONLY because allow_ungated_reads
	// overrode an unknown status. Such a run has no verified view age, so it must
	// not record a staleness of 0 — that would read as "verified caught up".
	ungated bool
	// semanticActive is the SECOND-axis verdict (B2 §4): whether the semantic-edge
	// tier may apply this cycle — true only when the tier is enabled AND
	// graph-embedding's readiness envelope is healthy. Meaningful only on a proceed
	// and only when semantic edges are enabled; false otherwise (structural-only or
	// n/a). It drives applySemanticGate's per-cycle provider toggle and the
	// semantic_edges_applied signal.
	semanticActive bool
}

// evaluateReadiness decides whether it is safe to run community detection against the
// INCOMING_INDEX (gh#474 Codex #6). It runs when the index is HEALTHY — fresh status,
// interpretable state, no hard stop, initial build complete — and however far behind
// the view happens to be.
//
// Detection used to declare a view-age tolerance (max_staleness) on top of that, and it
// was the only call site in the repository that ever did. The tolerance existed to
// serve an absence license that ADR-084 retired, and detection is the safest possible
// consumer of a stale view: it is periodic, it re-derives the WHOLE partition from
// scratch, and every result is overwritten on the next cycle — so a view a few seconds
// behind yields a partition a few seconds behind, self-correcting, never a wrong one
// that persists. Withholding the run instead just means the last partition (older
// still) stays published. The view age each run proceeded at is stamped on the
// staleness_at_detection_ms gauge, on EVERY run rather than only the ones a tolerance
// admitted.
//
// The semantics live in graph.EvaluateReadinessGate, not here — one home for the rule.
//
// Unknown status FAILS CLOSED by default (gh#474 Codex #4): a crashed or restarting
// graph-index mid-cutover is indistinguishable from an absent one, and its stale
// bucket is not proof of completeness. allow_ungated_reads is the standalone-deploy
// escape, and it applies to EXACTLY the case it always applied to — no verifiable
// status. It never ungates a status that was received and says not-ready.
func (c *Component) evaluateReadiness() gateDecision {
	// Read the watcher under the lock that guards it: Stop nils this field while the
	// detection loop can still be inside a tick body (cancellation is observed at the
	// top of the loop, not as a barrier mid-tick), so an unsynchronized read here
	// races Stop's write. Read (not Stop) is what happens under the lock — Read never
	// blocks on I/O, and Stop is deliberately joined outside c.mu.
	c.mu.RLock()
	watcher := c.statusWatcher
	embWatcher := c.embeddingStatusWatcher
	c.mu.RUnlock()

	var reading readiness.Reading
	if watcher != nil {
		reading = watcher.Read()
	} else {
		// Defensive: the loop only runs after Start bound the watcher. Report it as
		// unknown (fail closed) rather than proceeding on a zero envelope.
		reading.Err = errors.New("readiness status watcher not started")
	}

	// The semantic axis is independent of the index gate: it governs whether the
	// additive tier applies on a cycle the index gate has ALREADY allowed, never
	// whether the cycle runs. Evaluated once here so both the proceed and the
	// ungated proceed carry it.
	semanticActive := c.evaluateSemanticReadiness(embWatcher)

	proceed, reason := graph.EvaluateReadinessGate(
		graph.StatusReading{Status: reading.Status, Fresh: reading.Fresh})
	if proceed {
		return gateDecision{proceed: true, reading: reading, semanticActive: semanticActive}
	}
	if reason == graph.DeferStatusUnknown && c.config.AllowUngatedReads {
		return gateDecision{proceed: true, reading: reading, ungated: true, semanticActive: semanticActive}
	}
	return gateDecision{reason: reason, reading: reading}
}

// evaluateSemanticReadiness reports whether the semantic-edge tier may run this
// cycle: true only when the tier is ENABLED and graph-embedding's readiness
// envelope is HEALTHY (the same graph.EvaluateReadinessGate the index axis uses).
// An unknown or not-ready embedding feed yields false — which degrades the cycle
// to structural-only, never a defer (the additive-over-a-structural-floor tenet).
// It only READS held readiness state; it never queries embeddings, builds, or
// consults the mutual-kNN cache.
func (c *Component) evaluateSemanticReadiness(embWatcher *readiness.Watcher) bool {
	if !c.config.semanticEdges.enabled || embWatcher == nil {
		return false
	}
	return semanticActiveFromReading(embWatcher.Read())
}

// semanticActiveFromReading is the pure reading -> verdict step: the semantic tier
// applies only when graph-embedding's held envelope passes the SAME health gate
// the index axis uses. An unknown feed (never received / stale) is !Fresh and
// yields false (structural-only), never a fabricated ready.
func semanticActiveFromReading(r readiness.Reading) bool {
	proceed, _ := graph.EvaluateReadinessGate(
		graph.StatusReading{Status: r.Status, Fresh: r.Fresh})
	return proceed
}

// recordDefer makes a defer evidence rather than a bare constant. The gh#590
// observer-discrepancy investigation cost three comment cycles because the old line
// carried no fields: a transport failure and a genuinely-behind index logged
// identically. Every defer now increments defer_total{reason} and carries the full
// reading, and the FIRST defer of any reason (including a change of reason) is logged
// at WARN so it is visible without raising the log level — repeats drop to Debug so a
// long defer does not become a per-tick flood.
func (c *Component) recordDefer(d gateDecision) {
	if c.metrics != nil {
		c.metrics.countDefer(d.reason)
	}

	attrs := []any{
		slog.String("reason", string(d.reason)),
		slog.Bool("status_known", d.reading.Known),
		slog.Duration("status_age", d.reading.Age),
		slog.String("state", d.reading.Status.State),
		slog.Uint64("lag", d.reading.Status.Lag),
		slog.Uint64("staleness_ms", d.reading.Status.StalenessMs),
	}
	// The watch/bucket error is what separates "graph-index is not deployed here"
	// from "the feed died" — both read as status_unknown at the gate.
	if d.reading.Err != nil {
		attrs = append(attrs, slog.Any("status_error", d.reading.Err))
	}

	if c.noteGateOutcome(d.reason) {
		c.logger.Warn("community detection deferred", attrs...)
		return
	}
	c.logger.Debug("community detection deferred", attrs...)
}

// observeDetectionRun makes the staleness a run proceeded at operator-visible
// (ADR-083 D5, #579 lesson): it records the gauge on every verified run and, when the
// view was behind, also emits an INFO log carrying the age.
//
// This is the whole "report, don't gate" half of the readiness contract, and it matters
// MORE now than it did under a tolerance: nothing withholds a run for age any more, so
// the gauge and this line are the only places a stale partition announces itself. The
// gauge records every run (0 = the view was exactly caught up) rather than only the
// runs a tolerance admitted.
func (c *Component) observeDetectionRun(d gateDecision) {
	if c.noteGateOutcome(graph.DeferNone) {
		c.logger.Info("readiness gate cleared; resuming community detection",
			slog.Bool("status_known", d.reading.Known),
			slog.String("state", d.reading.Status.State),
			slog.Uint64("staleness_ms", d.reading.Status.StalenessMs))
	}

	// An ungated run verified nothing about the view; recording 0 would claim it did.
	if d.ungated {
		return
	}
	if c.metrics != nil {
		c.metrics.setStalenessAtDetection(d.reading.Status.StalenessMs)
	}
	if d.reading.Status.StalenessMs > 0 {
		c.logger.Info("community detection running on a lagging graph-index view",
			slog.Uint64("staleness_ms", d.reading.Status.StalenessMs),
			slog.Uint64("index_lag", d.reading.Status.Lag))
	}
}

// noteGateOutcome records this tick's outcome and reports whether it CHANGED. It
// turns a steady state into one visible log line per transition instead of either
// silence or a per-tick flood; defer_total remains the rate signal.
func (c *Component) noteGateOutcome(reason graph.DeferReason) (changed bool) {
	c.deferMu.Lock()
	defer c.deferMu.Unlock()
	changed = reason != c.lastDeferReason
	c.lastDeferReason = reason
	return changed
}

// applySemanticGate sets the semantic-edge tier active/inactive for THIS cycle
// from the embedding-readiness verdict, BEFORE detection reads the provider. It
// is the concrete mechanism behind the structural-floor guarantee (B2 §4): the
// provider chain is built once and the mutual-kNN cache is build-once, so "run
// structural-only this cycle" is expressed by DEACTIVATING the provider —
// GetNeighbors/GetEdgeWeight then behave exactly as the wrapped EntityIDProvider
// and never trigger the cache build. Because the cache is only ever built on an
// ACTIVE cycle, it can never latch to the empty structural-only set during a
// cold-embedding window: a later ready cycle reactivates and builds it fresh.
//
// This is ONLY the preflight toggle now (Codex P1#2): the semantic_edges_applied
// signal and its transition log are stamped AFTER detection by recordSemanticMode,
// from the ACTUAL completed mode. Stamping here would report the preflight INTENT —
// wrong whenever an intended-active cycle aborts mid-build (embedding went cold/reset
// past the gate) and degrades to structural-only.
//
// A no-op when the tier is disabled or unwired (no provider to gate) — the n/a row of
// the readiness table.
func (c *Component) applySemanticGate(d gateDecision) {
	if !c.config.semanticEdges.enabled || c.semanticProvider == nil {
		return
	}
	c.semanticProvider.SetActive(d.semanticActive)
}

// recordSemanticMode surfaces the #618 signal AFTER detection, from the provider's
// ACTUAL completed mode rather than the preflight readiness intent (Codex P1#2):
// semantic_edges_applied reads 1 iff the tier was active AND no producer-wide
// not-ready/fatal signal aborted the mutual-kNN build, 0 when it degraded to
// structural-only (readiness deactivated it, OR a mid-build abort collapsed an
// intended-active cycle). A transition log makes a structurally-blind cycle loud
// exactly once. So the gauge distinguishes "semantics ran, found nothing" (1) from
// "semantics never ran / aborted" (0) — the confusion #618 exists to resolve.
//
// A no-op (n/a) when the tier is disabled or unwired: the gauge is never touched, and
// on a disabled deployment it is not even registered (P2#4), so no misleading 0.
func (c *Component) recordSemanticMode() {
	if !c.config.semanticEdges.enabled || c.semanticProvider == nil {
		return
	}
	applied := c.semanticProvider.AppliedSemanticEdges()
	if c.metrics != nil {
		c.metrics.setSemanticEdgesApplied(applied)
	}
	c.noteSemanticApplied(applied)
}

// noteSemanticApplied logs the structural-only <-> full TRANSITION once, at a
// level that is visible without raising the log level, while a steady state stays
// quiet (mirrors noteGateOutcome). The structural-only transition is a WARN
// because a semantically-blind partition is exactly the #618 silent failure this
// diagnostic exists to make loud.
func (c *Component) noteSemanticApplied(active bool) {
	c.semanticMu.Lock()
	changed := c.lastSemanticApplied == nil || *c.lastSemanticApplied != active
	v := active
	c.lastSemanticApplied = &v
	c.semanticMu.Unlock()
	if !changed {
		return
	}
	if active {
		c.logger.Info("semantic-edge tier active; community detection includes mutual-kNN semantic edges",
			slog.Bool("semantic_edges_applied", true))
		return
	}
	c.logger.Warn("semantic-edge tier INACTIVE; running STRUCTURAL-ONLY this cycle — graph-embedding is not ready, so the partition is semantically blind (#618)",
		slog.Bool("semantic_edges_applied", false))
}

// handleDetectionError handles errors during detection, returning true if the error was handled as shutdown.
func (c *Component) handleDetectionError(ctx context.Context, err error, operation string) bool {
	if errors.Is(err, context.Canceled) {
		c.logger.Debug(operation + " interrupted by shutdown")
		return true
	}
	c.latchGraphStatePoison(err)
	c.logger.Error(operation+" failed", slog.Any("error", err))
	atomic.AddInt64(&c.errors, 1)
	if c.lifecycleReporter != nil {
		if repErr := c.lifecycleReporter.ReportCycleError(ctx, err); repErr != nil {
			c.logger.Debug("failed to report cycle error", slog.Any("error", repErr))
		}
	}
	return false
}

// latchGraphStatePoison records an authoritative graph-state contract
// violation observed at consume time. Contract poison is sticky for the
// process lifetime: a later valid write cannot prove that previously derived
// community output is sound, so recovery requires operator reset and restart.
func (c *Component) latchGraphStatePoison(err error) bool {
	var contractErr *graph.StateContractError
	if !errors.As(err, &contractErr) {
		return false
	}
	if c.graphStatePoison.CompareAndSwap(nil, contractErr) {
		c.logger.Error("authoritative graph state requires reset; clustering outputs are blocked",
			slog.String("code", graph.ErrorCodeGraphStateResetRequired),
			slog.String("reason", string(contractErr.Reason)))
	}
	return true
}

func (c *Component) graphStateContractError(operation string) error {
	contractErr := c.graphStatePoison.Load()
	if contractErr == nil {
		return nil
	}
	return errs.WrapFatal(contractErr, "Component", operation,
		"authoritative graph state requires operator reset and canonical reingest")
}

// runStructuralAndAnomalyDetection runs structural computation and anomaly detection if enabled.
func (c *Component) runStructuralAndAnomalyDetection(ctx context.Context) bool {
	if !c.config.EnableStructural {
		return true
	}
	if ctx.Err() != nil {
		c.logger.Debug("skipping structural computation - shutdown in progress")
		return false
	}

	c.reportStage(ctx, "structural_computation")
	kcoreIndex, pivotIndex, err := c.runStructuralComputation(ctx)
	if err != nil {
		c.handleDetectionError(ctx, err, "structural computation")
		return false
	}

	if c.config.EnableAnomalyDetection && kcoreIndex != nil {
		if ctx.Err() != nil {
			c.logger.Debug("skipping anomaly detection - shutdown in progress")
			return false
		}
		c.reportStage(ctx, "anomaly_detection")
		if err := c.runAnomalyDetection(ctx, kcoreIndex, pivotIndex); err != nil {
			c.handleDetectionError(ctx, err, "anomaly detection")
			return false
		}
	}
	return true
}

// runCommunityDetection executes the community detection algorithm
func (c *Component) runCommunityDetection(ctx context.Context) {
	if ctx.Err() != nil {
		c.logger.Debug("skipping detection - shutdown in progress")
		return
	}
	if err := c.graphStateContractError("runCommunityDetection"); err != nil {
		c.logger.Debug("skipping detection because authoritative graph state is unavailable",
			slog.Any("error", err))
		return
	}

	if c.lifecycleReporter != nil {
		if err := c.lifecycleReporter.ReportCycleStart(ctx); err != nil {
			c.logger.Debug("failed to report cycle start", slog.Any("error", err))
		}
	}

	c.logger.Debug("running community detection")
	start := time.Now()
	c.reportStage(ctx, "community_detection")

	communities, err := c.detector.DetectCommunities(ctx)
	if err != nil {
		c.handleDetectionError(ctx, err, "detection")
		return
	}

	totalCommunities := 0
	for _, levelCommunities := range communities {
		totalCommunities += len(levelCommunities)
	}

	atomic.AddInt64(&c.messagesProcessed, int64(totalCommunities))
	c.lastActivity.Store(time.Now())

	detectionTook := time.Since(start)
	if c.metrics != nil {
		c.metrics.observeDetectionDuration(detectionTook)
	}
	c.logger.Debug("community detection complete",
		slog.Int("communities_found", totalCommunities),
		slog.Int("levels", len(communities)),
		slog.Duration("duration", detectionTook))

	if !c.runStructuralAndAnomalyDetection(ctx) {
		return
	}

	if c.lifecycleReporter != nil {
		if err := c.lifecycleReporter.ReportCycleComplete(ctx); err != nil {
			c.logger.Debug("failed to report cycle complete", slog.Any("error", err))
		}
	}
}

// ============================================================================
// KV-based Graph Provider for Community Detection
// ============================================================================

// kvProvider implements clustering.Provider using NATS KV buckets
type kvProvider struct {
	entityBucket   jetstream.KeyValue
	outgoingBucket jetstream.KeyValue
	incomingBucket jetstream.KeyValue
	logger         *slog.Logger
}

// newKVProvider creates a graph provider that reads from KV buckets
func newKVProvider(
	entityBucket jetstream.KeyValue,
	outgoingBucket jetstream.KeyValue,
	incomingBucket jetstream.KeyValue,
	logger *slog.Logger,
) *kvProvider {
	return &kvProvider{
		entityBucket:   entityBucket,
		outgoingBucket: outgoingBucket,
		incomingBucket: incomingBucket,
		logger:         logger,
	}
}

// GetAllEntityIDs returns all entity IDs from the ENTITY_STATES bucket
func (p *kvProvider) GetAllEntityIDs(ctx context.Context) ([]string, error) {
	keys, err := p.entityBucket.Keys(ctx)
	if err != nil {
		// Empty bucket returns an error in some cases
		if err == jetstream.ErrNoKeysFound {
			return nil, nil
		}
		return nil, errs.WrapTransient(err, "kvProvider", "GetAllEntityIDs", "list keys")
	}
	return keys, nil
}

// GetNeighbors returns entity IDs connected to the given entity.
//
// After composite-key sharding (gh#474): INCOMING_INDEX uses a composite format
// "targetID.sourceID.predicate" while OUTGOING_INDEX keeps its flat JSON-array
// format. The two directions are handled by separate methods.
func (p *kvProvider) GetNeighbors(ctx context.Context, entityID string, direction string) ([]string, error) {
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "kvProvider", "GetNeighbors", "entityID is empty")
	}

	neighbors := make(map[string]bool)

	// Get outgoing neighbors — unchanged format: single key, JSON array of {to_entity_id, predicate}
	if direction == "outgoing" || direction == "both" {
		outgoing, err := p.getNeighborsFromBucket(ctx, p.outgoingBucket, entityID)
		if err != nil {
			return nil, errs.WrapTransient(err, "kvProvider", "GetNeighbors", "outgoing topology read")
		}
		for _, n := range outgoing {
			neighbors[n] = true
		}
	}

	// Get incoming neighbors — composite-key sharded format: prefix scan
	if direction == "incoming" || direction == "both" {
		incoming, err := p.getIncomingNeighbors(ctx, entityID)
		if err != nil {
			return nil, errs.WrapTransient(err, "kvProvider", "GetNeighbors", "incoming topology read")
		}
		for _, n := range incoming {
			neighbors[n] = true
		}
	}

	result := make([]string, 0, len(neighbors))
	for n := range neighbors {
		result = append(result, n)
	}
	return result, nil
}

// relationshipEntry represents a relationship in the index buckets
type relationshipEntry struct {
	Predicate    string `json:"predicate"`
	ToEntityID   string `json:"to_entity_id,omitempty"`   // For OUTGOING_INDEX
	FromEntityID string `json:"from_entity_id,omitempty"` // For INCOMING_INDEX
}

// getNeighborsFromBucket reads neighbor entity IDs from the OUTGOING_INDEX bucket.
// The outgoing format is a single key (entityID) with a JSON array of
// {to_entity_id, predicate} entries — unchanged after composite-key sharding (gh#474).
// Do NOT use this for INCOMING_INDEX; call getIncomingNeighbors instead.
func (p *kvProvider) getNeighborsFromBucket(ctx context.Context, bucket jetstream.KeyValue, entityID string) ([]string, error) {
	entry, err := bucket.Get(ctx, entityID)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, nil // No neighbors found
		}
		return nil, err
	}

	// Parse the index entry - format is a list of relationship entries
	var relationships []relationshipEntry
	if err := json.Unmarshal(entry.Value(), &relationships); err != nil {
		return nil, err
	}

	neighbors := make([]string, 0, len(relationships))
	for _, rel := range relationships {
		if _, err := vocabulary.ParsePredicate(rel.Predicate); err != nil {
			return nil, err
		}
		// OUTGOING_INDEX rows have exactly one endpoint direction. Accepting a
		// from_entity_id fallback here lets an INCOMING-shaped or poisoned row
		// silently become an LPA neighbor.
		if rel.ToEntityID == "" {
			return nil, errs.WrapInvalid(errs.ErrInvalidData, "kvProvider", "getNeighborsFromBucket",
				"OUTGOING_INDEX relationship is missing to_entity_id")
		}
		if err := semtypes.ValidateEntityID(rel.ToEntityID); err != nil {
			return nil, err
		}
		neighbors = append(neighbors, rel.ToEntityID)
	}
	return neighbors, nil
}

// getIncomingNeighbors reads entity IDs that have an incoming edge to entityID
// from the composite-keyed INCOMING_INDEX (gh#474). Keys have the form
// "targetID.sourceID.predicate"; this method scans the prefix entityID.">" and
// returns distinct source entity IDs.
func (p *kvProvider) getIncomingNeighbors(ctx context.Context, entityID string) ([]string, error) {
	if err := semtypes.ValidateEntityID(entityID); err != nil {
		return nil, err
	}
	if err := natsclient.ValidateKVWildcardFilter(entityID + ".>"); err != nil {
		return nil, err
	}
	// FilteredKeys handles ErrNoKeysFound → nil, nil (no error on empty bucket).
	keys, err := natsclient.FilteredKeys(ctx, p.incomingBucket, entityID+".>")
	if err != nil {
		return nil, err
	}

	prefix := entityID + "."
	seen := make(map[string]struct{}, len(keys))
	neighbors := make([]string, 0, len(keys))
	for _, key := range keys {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		suffix := key[len(prefix):]
		// suffix = "sourceID.hex(predicate)"; sourceID is exactly 6 dot-separated
		// tokens. Only the source is needed here, so the hex predicate token (gh#474
		// P1a) is left untouched.
		parts := strings.SplitN(suffix, ".", 7)
		if len(parts) < 7 {
			continue
		}
		sourceID := strings.Join(parts[:6], ".")
		if err := semtypes.ValidateEntityID(sourceID); err != nil {
			continue
		}
		predicate, ok := graph.DecodePredicateToken(parts[6])
		if !ok {
			continue
		}
		if _, err := vocabulary.ParsePredicate(predicate); err != nil {
			continue
		}
		if _, dup := seen[sourceID]; !dup {
			seen[sourceID] = struct{}{}
			neighbors = append(neighbors, sourceID)
		}
	}
	return neighbors, nil
}

// GetEdgeWeight returns the weight of the edge between two entities
func (p *kvProvider) GetEdgeWeight(_ context.Context, _, _ string) (float64, error) {
	// For now, return 1.0 for all edges (equal weight)
	// Could be enhanced to read confidence from the relationship data
	return 1.0, nil
}

// ============================================================================
// LLM Enhancement Support
// ============================================================================

// startEnhancementWorker initializes and starts the LLM enhancement worker
func (c *Component) startEnhancementWorker(ctx context.Context, provider clustering.Provider) error {
	// Resolve endpoint AND full config — direct ResolveEndpoint silently strips
	// the connection-hygiene fields (DisableKeepAlives, IdleConnTimeout,
	// ResponseHeaderTimeout) and per-endpoint RequestTimeout that the LLM
	// client builder needs to honour the operator's capability config.
	resolved, ep, err := model.ResolveEndpointWithConfig(c.modelRegistry, model.CapabilityCommunitySummary)
	if err != nil {
		return errs.Wrap(err, "Component", "startEnhancementWorker", "resolve community_summary endpoint")
	}

	// Probe LLM endpoint before committing workers — a fast health check
	// prevents per-community blocking when the endpoint is unreachable
	// (e.g., seminstruct unhealthy, LLM disabled in deployment).
	probeCtx, probeCancel := context.WithTimeout(ctx, 5*time.Second)
	if err := probeLLMEndpoint(probeCtx, resolved.URL); err != nil {
		probeCancel()
		return errs.WrapTransient(err, "Component", "startEnhancementWorker",
			fmt.Sprintf("LLM endpoint unreachable at %s", resolved.URL))
	}
	probeCancel()

	// Create LLM client. OpenAIConfigFromEndpoint plumbs connection-hygiene
	// fields; ResolveCapabilityTimeout applies the endpoint > capability >
	// default precedence chain so configured 300s reaches the HTTP client.
	cfg := llm.OpenAIConfigFromEndpoint(resolved, ep, c.logger)
	cfg.Timeout = model.ResolveCapabilityTimeout(c.modelRegistry, model.CapabilityCommunitySummary, defaultCommunitySummaryTimeout, c.logger)
	llmClient, err := llm.NewOpenAIClient(cfg)
	if err != nil {
		return errs.Wrap(err, "Component", "startEnhancementWorker", "create LLM client")
	}
	c.llmClient = llmClient

	// Create LLM summarizer
	llmSummarizer, err := clustering.NewLLMSummarizer(clustering.LLMSummarizerConfig{
		Client:    llmClient,
		MaxTokens: 200,
	})
	if err != nil {
		llmClient.Close()
		return errs.Wrap(err, "Component", "startEnhancementWorker", "create LLM summarizer")
	}

	// Create entity querier from entity bucket (latch-wired: enhancement reads
	// of poisoned authoritative state must latch the projection reset too)
	querier := c.newEntityStateQuerier()

	// Create enhancement worker. LLMTimeout matches cfg.Timeout so the
	// inner ctx.WithTimeout that wraps each LLM round-trip respects the
	// resolved capability timeout — without this the inner default (30s)
	// caps the effective ceiling regardless of HTTP-client configuration.
	worker, err := clustering.NewEnhancementWorker(&clustering.EnhancementWorkerConfig{
		LLMSummarizer:   llmSummarizer,
		Storage:         c.storage,
		Provider:        provider,
		Querier:         querier,
		CommunityBucket: c.communityBucket,
		Logger:          c.logger,
		LLMTimeout:      cfg.Timeout,
	})
	if err != nil {
		llmClient.Close()
		return errs.Wrap(err, "Component", "startEnhancementWorker", "create enhancement worker")
	}

	// Configure worker parallelism
	worker.WithWorkers(c.config.EnhancementWorkers)

	// Start the worker
	if err := worker.Start(ctx); err != nil {
		llmClient.Close()
		return errs.Wrap(err, "Component", "startEnhancementWorker", "start enhancement worker")
	}

	c.enhancementWorker = worker
	c.logger.Info("LLM enhancement worker started",
		slog.String("endpoint", resolved.URL),
		slog.String("model", resolved.Model),
		slog.Int("workers", c.config.EnhancementWorkers))

	// Start background health monitor that pauses/resumes the worker based
	// on LLM endpoint availability. Checks every 30s so recovery is automatic
	// when the endpoint comes back.
	c.wg.Add(1)
	go c.monitorLLMHealth(ctx, resolved.URL, worker)

	return nil
}

// probeLLMEndpoint performs a fast connectivity check against the LLM service.
// Returns nil if the endpoint responds (any status), error if unreachable.
func probeLLMEndpoint(ctx context.Context, baseURL string) error {
	probeURL := strings.TrimSuffix(baseURL, "/v1") + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// monitorLLMHealth periodically probes the LLM endpoint and pauses/resumes
// the enhancement worker based on availability. This prevents workers from
// blocking on 30s timeouts when the endpoint goes down mid-operation, and
// automatically resumes when it recovers.
func (c *Component) monitorLLMHealth(ctx context.Context, endpointURL string, worker *clustering.EnhancementWorker) {
	defer c.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := probeLLMEndpoint(probeCtx, endpointURL)
			cancel()

			if err != nil && !worker.IsPaused() {
				c.logger.Warn("LLM endpoint unreachable, pausing enhancement worker",
					slog.String("endpoint", endpointURL),
					slog.Any("error", err))
				worker.Pause()
			} else if err == nil && worker.IsPaused() {
				c.logger.Info("LLM endpoint recovered, resuming enhancement worker",
					slog.String("endpoint", endpointURL))
				worker.Resume()
			}
		}
	}
}

// startReviewWorker initializes and starts the anomaly review worker.
// The review worker processes pending anomalies and can auto-approve/reject
// based on confidence thresholds, optionally using LLM for uncertain cases.
//
// LLM client selection precedence:
//  1. If model.CapabilityAnomalyReview is bound in the registry, create a
//     dedicated client for that endpoint. Stored in c.reviewLLMClient for
//     cleanup.
//  2. Otherwise fall back to c.llmClient (the community_summary endpoint),
//     preserving the pre-capability piggyback behavior. May be nil for
//     human-only mode.
func (c *Component) startReviewWorker(ctx context.Context) error {
	// Create relationship applier for approved anomalies
	// Uses the mutation API to go through graph-ingest for proper indexing
	applier := inference.NewMutationRelationshipApplier(c.natsClient, c.logger)

	reviewClient := c.resolveReviewLLMClient()

	// Resolve the capability timeout into ReviewConfig.ReviewTimeout so the
	// inner ctx.WithTimeout that wraps each review LLM call respects the
	// model-registry config. AnomalyConfig.Review.ReviewTimeout becomes the
	// fall-through default — endpoint.request_timeout > capability.timeout >
	// AnomalyConfig.Review.ReviewTimeout. Operators previously had to set
	// the inference-config knob; now anomaly_review.timeout in the model
	// registry suffices.
	reviewConfig := c.config.AnomalyConfig.Review
	if c.modelRegistry != nil && c.modelRegistry.GetCapability(model.CapabilityAnomalyReview) != nil {
		reviewConfig.ReviewTimeout = model.ResolveCapabilityTimeout(c.modelRegistry, model.CapabilityAnomalyReview, reviewConfig.ReviewTimeout, c.logger)
	}

	// Create review worker - reviewClient may be nil for human-only mode
	reviewWorker, err := inference.NewReviewWorker(&inference.ReviewWorkerConfig{
		AnomalyBucket: c.anomalyBucket,
		Storage:       c.anomalyStorage,
		LLMClient:     reviewClient,
		Applier:       applier,
		Config:        reviewConfig,
		Logger:        c.logger,
	})
	if err != nil {
		return errs.Wrap(err, "Component", "startReviewWorker", "create review worker")
	}
	c.reviewWorker = reviewWorker

	// Start the worker
	if err := c.reviewWorker.Start(ctx); err != nil {
		c.reviewWorker = nil
		return errs.Wrap(err, "Component", "startReviewWorker", "start review worker")
	}

	c.logger.Info("review worker started",
		slog.Int("workers", c.config.AnomalyConfig.Review.Workers),
		slog.Bool("llm_enabled", reviewClient != nil),
		slog.Bool("llm_dedicated", c.reviewLLMClient != nil),
		slog.Float64("auto_approve_threshold", c.config.AnomalyConfig.Review.AutoApproveThreshold),
		slog.Float64("auto_reject_threshold", c.config.AnomalyConfig.Review.AutoRejectThreshold))

	return nil
}

// resolveReviewLLMClient returns the LLM client the review worker should
// use. Creates a dedicated client only when CapabilityAnomalyReview is
// *explicitly* bound in the registry — otherwise (capability not present,
// or any client construction error) falls back to c.llmClient, preserving
// the legacy piggyback on the community_summary endpoint.
//
// We check explicit binding via GetCapability rather than ResolveEndpoint
// because Resolve falls through to defaults.model for unknown capabilities;
// using ResolveEndpoint would create a dedicated client for every
// deployment even when no operator opted in, which doubles the connection
// pool against the same endpoint and defeats the point of the fallback.
func (c *Component) resolveReviewLLMClient() llm.Client {
	if c.modelRegistry == nil {
		return c.llmClient
	}
	if c.modelRegistry.GetCapability(model.CapabilityAnomalyReview) == nil {
		// Capability not explicitly bound — preserve legacy piggyback.
		return c.llmClient
	}
	resolved, ep, err := model.ResolveEndpointWithConfig(c.modelRegistry, model.CapabilityAnomalyReview)
	if err != nil {
		c.logger.Warn("anomaly_review capability bound but endpoint resolution failed; falling back to community_summary client",
			slog.Any("error", err))
		return c.llmClient
	}
	cfg := llm.OpenAIConfigFromEndpoint(resolved, ep, c.logger)
	cfg.Timeout = model.ResolveCapabilityTimeout(c.modelRegistry, model.CapabilityAnomalyReview, defaultAnomalyReviewTimeout, c.logger)
	client, err := llm.NewOpenAIClient(cfg)
	if err != nil {
		c.logger.Warn("failed to create dedicated anomaly review LLM client; falling back to community_summary client",
			slog.Any("error", err),
			slog.String("endpoint", resolved.URL))
		return c.llmClient
	}
	c.reviewLLMClient = client
	c.logger.Info("anomaly review using dedicated LLM endpoint",
		slog.String("endpoint", resolved.URL),
		slog.String("model", resolved.Model))
	return client
}

// ============================================================================
// KV-based Entity Querier for Enhancement Worker
// ============================================================================

// kvEntityQuerier implements clustering.EntityQuerier using NATS KV
type kvEntityQuerier struct {
	entityBucket jetstream.KeyValue
	logger       *slog.Logger

	// latchPoison, when set, receives every authoritative decode failure so
	// the owning component can latch its sticky projection reset state at
	// consume time. The querier still fails the whole batch; the callback
	// only guarantees detection is not lost when a caller (LPA
	// summarization, enhancement worker) downgrades the batch error to a
	// warning. This is the input-path replacement for the retired dedicated
	// ENTITY_STATES contract watch (poison-response-scoping D7).
	latchPoison func(error) bool
}

// newKVEntityQuerier creates an entity querier that reads from ENTITY_STATES
func newKVEntityQuerier(entityBucket jetstream.KeyValue, logger *slog.Logger) *kvEntityQuerier {
	return &kvEntityQuerier{
		entityBucket: entityBucket,
		logger:       logger,
	}
}

// GetEntities retrieves entities by their IDs from ENTITY_STATES bucket
// observeIndexingProfileForClustering emits a Debug observation when an entity
// carries an indexing profile that ADR-054 Phase 3 would exclude from the
// community/structural substrates (trace). Phase 1 is LENIENT — it never
// excludes — so this is purely the dry-run signal that informs the Phase 3
// policy, never an action. Strict enforcement is gated on the cost-ledger
// preconditions (gate-silent-exclusion-flips-with-cost-ledger).
func observeIndexingProfileForClustering(logger *slog.Logger, es *graph.EntityState) {
	if v, ok := es.GetPropertyValue(vocabulary.EntityIndexingProfile); ok {
		if profile, _ := v.(string); profile == vocabulary.IndexingProfileTrace {
			logger.Debug("entity has trace indexing profile; clustering it anyway (ADR-054 Phase 1 lenient)",
				slog.String("entity", es.ID),
				slog.String("indexing_profile", profile))
		}
	}
}

func (q *kvEntityQuerier) GetEntities(ctx context.Context, ids []string) ([]*graph.EntityState, error) {
	entities := make([]*graph.EntityState, 0, len(ids))

	for _, id := range ids {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		entry, err := q.entityBucket.Get(ctx, id)
		if err != nil {
			if err == jetstream.ErrKeyNotFound {
				q.logger.Debug("entity not found", slog.String("id", id))
				continue
			}
			return nil, errs.WrapTransient(err, "kvEntityQuerier", "GetEntities", "get entity")
		}

		var entity graph.EntityState
		if err := graph.UnmarshalEntityState(entry.Value(), &entity); err != nil {
			if q.latchPoison != nil {
				q.latchPoison(err)
			}
			return nil, errs.WrapFatal(err, "kvEntityQuerier", "GetEntities", "decode authoritative entity")
		}

		// ADR-054 Phase 1 (lenient): observe the entity's indexing profile but
		// never exclude it from clustering. Community/structural eligibility
		// enforcement is Phase 3, gated on the cost-ledger preconditions — so
		// this is a provable no-op here.
		observeIndexingProfileForClustering(q.logger, &entity)

		entities = append(entities, &entity)
	}

	return entities, nil
}
