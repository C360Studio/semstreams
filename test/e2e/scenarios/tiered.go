// Package scenarios provides E2E test scenarios for SemStreams semantic processing
package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/config"
	"github.com/c360studio/semstreams/test/e2e/scenarios/search"
)

// Variant-specific entity count expectations
const (
	// StructuralMinEntities is the minimum entities expected for structural tier
	// All tiers now load testdata/semantic/*.jsonl with 74 unique entities
	StructuralMinEntities = 50

	// StatisticalMinEntities is the minimum entities expected for statistical tier
	// Statistical tier loads testdata/semantic/*.jsonl with 74 unique entities
	StatisticalMinEntities = 50

	// SemanticMinEntities is the minimum entities expected for semantic tier
	// Same testdata as statistical tier
	SemanticMinEntities = 50
)

// TieredScenario validates comprehensive semantic processing
type TieredScenario struct {
	name        string
	description string
	client      *client.ObservabilityClient
	natsClient  *client.NATSValidationClient
	udpAddr     string
	natsURL     string
	config      *TieredConfig

	// Observability clients for consistent metric access
	metrics   *client.MetricsClient
	msgLogger *client.MessageLoggerClient
	tracer    *client.FlowTracer

	// SSE client for real-time KV bucket watching (Phase 8)
	// Falls back to polling if unavailable
	sseClient *client.SSEClient

	// Pre-send baseline for event-driven validation (captured before sending data)
	preSendBaseline *client.MetricsBaseline

	// Search results from executeVerifySearchQuality for reuse by comparison
	searchStats *search.Stats

	// Cached variant detection (set once, reused across stages)
	detectedVariant *variantInfo
}

// TieredConfig contains configuration for tiered E2E tests
type TieredConfig struct {
	// Variant configuration
	Variant string `json:"variant"` // "structural", "statistical", "semantic"

	// Test data configuration
	MessageCount    int           `json:"message_count"`
	MessageInterval time.Duration `json:"message_interval"`

	// Validation configuration (event-driven, matching structural tier patterns)
	ValidationTimeout time.Duration `json:"validation_timeout"` // Timeout for metric waits (30s for semantic)
	PollInterval      time.Duration `json:"poll_interval"`      // Poll interval for metric waits (100ms)
	MinProcessed      int           `json:"min_processed"`

	// Entity verification (from test data files)
	MinExpectedEntities int    `json:"min_expected_entities"`
	NatsURL             string `json:"nats_url"`
	MetricsURL          string `json:"metrics_url"`
	ServiceManagerURL   string `json:"service_manager_url"`
	GatewayURL          string `json:"gateway_url"`
	GraphQLURL          string `json:"graphql_url"` // GraphQL endpoint (port varies by profile)

	// Comparison output configuration
	OutputDir string `json:"output_dir"`

	// Baseline comparison (matching structural tier patterns)
	BaselineFile         string  `json:"baseline_file,omitempty"` // Path to baseline JSON (optional)
	MaxRegressionPercent float64 `json:"max_regression_percent"`  // Default 20%

	// Structural tier config (rules-only, no ML dependencies)
	ExpectedEmbeddings   int `json:"expected_embeddings"`    // 0 for structural variant
	ExpectedClusters     int `json:"expected_clusters"`      // 0 for structural variant
	MinRulesEvaluated    int `json:"min_rules_evaluated"`    // Min rules evaluated for structural
	MinRuleFirings       int `json:"min_rule_firings"`       // Min rule firings (conditions met)
	MinActionsDispatched int `json:"min_actions_dispatched"` // Min actions dispatched
}

// DefaultTieredConfig returns default configuration
func DefaultTieredConfig() *TieredConfig {
	return &TieredConfig{
		Variant:         "", // Auto-detect from environment
		MessageCount:    20,
		MessageInterval: 50 * time.Millisecond,
		// Event-driven validation timeouts.
		// 30s default — entity stabilization needs headroom for Docker
		// pressure on the host (parallel projects spinning containers
		// can slow graph-ingest's pipeline meaningfully). Steady-state
		// stabilization completes in ~100ms; the generous budget only
		// matters under contention. Prior 10s budget consistently flaked
		// in the beta.91 sweep when host was loaded — the SSE path
		// would return `TimedOut: false, FinalCount: 0` before entities
		// landed, and the silent-pass downstream surfaced as
		// "entity not found" in stage 14 (test-pathrag-document).
		// Semantic tier may still override this for ML processing.
		ValidationTimeout:    30 * time.Second,
		PollInterval:         100 * time.Millisecond, // Fast polling for responsiveness
		MinProcessed:         10,                     // At least 50% should make it through
		MinExpectedEntities:  50,                     // Test data has 74 entities, expect at least 50 indexed
		NatsURL:              config.DefaultEndpoints.NATS,
		MetricsURL:           config.DefaultEndpoints.Metrics,
		ServiceManagerURL:    config.DefaultEndpoints.HTTP,
		GatewayURL:           config.DefaultEndpoints.HTTP + "/api-gateway",
		GraphQLURL:           config.DefaultEndpoints.HTTP + "/graph-gateway/graphql", // Via ServiceManager shared mux
		OutputDir:            "test/e2e/results",
		MaxRegressionPercent: 20.0, // 20% regression threshold
		// Structural tier defaults (rules-only, no ML)
		ExpectedEmbeddings:   0, // Structural: NO embeddings
		ExpectedClusters:     0, // Structural: NO clustering
		MinRulesEvaluated:    5,
		MinRuleFirings:       2, // Expect at least 2 rule firings
		MinActionsDispatched: 1, // Expect at least 1 action dispatched
	}
}

// NewTieredScenario creates a new tiered semantic test scenario
func NewTieredScenario(
	obsClient *client.ObservabilityClient,
	udpAddr string,
	cfg *TieredConfig,
) *TieredScenario {
	if cfg == nil {
		cfg = DefaultTieredConfig()
	}
	if udpAddr == "" {
		udpAddr = "localhost:34550"
	}
	natsURL := cfg.NatsURL
	if natsURL == "" {
		natsURL = config.DefaultEndpoints.NATS
	}

	// Set GraphQL URL if not explicitly configured
	// Goes through ServiceManager shared mux on the default HTTP port
	if cfg.GraphQLURL == "" {
		cfg.GraphQLURL = config.DefaultEndpoints.HTTP + "/graph-gateway/graphql"
	}

	return &TieredScenario{
		name:        "tiered",
		description: "Tests tiered semantic stack: Protocol + Graph + Indexes + Queries + Multiple Outputs",
		client:      obsClient,
		udpAddr:     udpAddr,
		natsURL:     natsURL,
		config:      cfg,
	}
}

// Name returns the scenario name
func (s *TieredScenario) Name() string {
	return s.name
}

// Description returns the scenario description
func (s *TieredScenario) Description() string {
	return s.description
}

// Setup prepares the scenario
func (s *TieredScenario) Setup(ctx context.Context) error {
	// Pre-flight: Validate flowgraph configuration before running tests
	// This catches issues like JetStream subscribers connected to NATS publishers
	// which would cause components to hang waiting for streams that never get created
	if err := s.client.CheckFlowHealth(ctx); err != nil {
		return fmt.Errorf("flowgraph pre-flight validation failed: %w", err)
	}

	// Verify UDP endpoint is reachable
	conn, err := net.Dial("udp", s.udpAddr)
	if err != nil {
		return fmt.Errorf("cannot reach UDP endpoint %s: %w", s.udpAddr, err)
	}
	_ = conn.Close()

	// Initialize observability clients (matching structural tier patterns)
	s.metrics = client.NewMetricsClient(s.config.MetricsURL)
	s.msgLogger = client.NewMessageLoggerClient(s.config.ServiceManagerURL)
	s.tracer = client.NewFlowTracer(s.metrics, s.msgLogger)

	// Create NATS validation client for KV bucket assertions
	natsClient, err := client.NewNATSValidationClient(ctx, s.natsURL)
	if err != nil {
		// NATS is optional - warn but don't fail
		// Some stages will skip if natsClient is nil
		return nil
	}
	s.natsClient = natsClient

	// Initialize SSE client for real-time KV bucket watching (Phase 8)
	// Falls back to polling if SSE endpoint is unavailable
	s.sseClient = client.NewSSEClient(s.config.ServiceManagerURL)
	if err := s.sseClient.Health(ctx); err != nil {
		// SSE not available - will fall back to polling
		// This is expected if message-logger service is not running
		s.sseClient = nil
	}

	return nil
}

// Execute runs the tiered semantic test scenario
// stage represents a test stage with variant filtering
type stage struct {
	name     string
	fn       func(context.Context, *Result) error
	variants []string // Empty = run for all variants
}

// getStagesForVariant returns the filtered list of stages for a given variant.
//
// Stages are organized following the progressive enhancement model:
// - Tier 0 (Structural): Graph traversal, indexes, rules - runs on ALL tiers
// - Tier 1 (Statistical): Tier 0 + embeddings, search, communities
// - Tier 2 (Semantic): Tier 1 + neural embeddings, LLM, GraphRAG
func (s *TieredScenario) getStagesForVariant(variant string) []stage {
	allStages := []stage{
		// === Common setup stages (all tiers) ===
		{"verify-components", s.executeVerifyComponents, nil},
		{"send-mixed-data", s.executeSendMixedData, nil},
		{"validate-processing", s.executeValidateProcessing, nil},

		// Wait for embeddings BEFORE counting entities (statistical/semantic tiers)
		// This ensures all entities have completed the embedding pipeline before validation
		{"wait-for-embeddings", s.executeWaitForEmbeddings, []string{"statistical", "semantic"}},

		// Phase 4: Validate embedding queue health after waiting for embeddings
		// Ensures queue is drained and no failures occurred before proceeding
		{"validate-embedding-queue-health", s.validateEmbeddingQueueHealth, []string{"statistical", "semantic"}},

		// Wait for entity stabilization (all tiers)
		// MUST run before hierarchy validation to ensure all entities are loaded
		// SSE-enabled: uses real-time KV watching with polling fallback
		{"wait-for-entity-stabilization", s.executeWaitForEntityStabilization, nil},

		// Phase 4: Validate hierarchy inference is creating container entities
		// Verifies the KV watcher pattern from Phase 3 is working correctly
		// Hierarchy inference is structural (no ML) - runs on all tiers
		{"validate-hierarchy-inference", s.validateHierarchyInference, []string{"structural", "statistical", "semantic"}},

		{"verify-entity-count", s.executeVerifyEntityCount, nil},
		{"verify-entity-retrieval", s.executeVerifyEntityRetrieval, nil},
		{"validate-entity-structure", s.executeValidateEntityStructure, nil},
		{"verify-index-population", s.executeVerifyIndexPopulation, nil},

		// === Tier 0: Structural capabilities (run on ALL tiers) ===
		// PathRAG is pure graph traversal - no embeddings required
		// Sensor PathRAG runs on all tiers (sensor data loads quickly)
		{"test-pathrag-sensor", s.executeTestPathRAGSensor, nil},
		{"test-pathrag-boundary", s.executeTestPathRAGBoundary, nil},
		// Document PathRAG runs on all tiers (entity stabilization ensures documents are loaded)
		{"test-pathrag-document", s.executeTestPathRAGDocument, nil},
		// EntityID hierarchy navigation (6-part EntityID structure)
		{"test-entityid-hierarchy", s.executeTestEntityIDHierarchy, nil},
		{"test-entities-by-prefix", s.executeTestEntitiesByPrefix, nil},
		// Spatial/Temporal index queries (all tiers have indexed geo/time data)
		{"test-spatial-query", s.executeTestSpatialQuery, nil},
		{"test-temporal-query", s.executeTestTemporalQuery, nil},
		// Event-time keying (gh#370/#372): index buckets on time.observation.recorded,
		// not write-time. Structural-only — creates its own observation-stamped entity.
		{"test-temporal-observed-time", s.executeTestTemporalObservedTime, []string{"structural"}},
		{"test-zone-relationships", s.executeTestZoneRelationships, nil},
		// LLM community enhancement wait runs HERE, before B0 and before any
		// answer-model stage. It only reads/polls community KV for enhancement
		// status (it does NOT drive answer synthesis), so it gives the separate
		// community_summary model time to populate Tier-2 summaries without
		// clogging the answer model — exactly what B0 needs to synthesize over real
		// summaries. On a heavy qwen3-8b run summaries keep generating on that
		// separate instance throughout B0's own run as well.
		{"validate-llm-enhancement", s.executeValidateLLMEnhancement, []string{"semantic"}},
		// Epic B increment B0 — the GraphRAG thematic-answer eval — runs HERE, before
		// ANY stage that drives LLM answer synthesis (the NL-intent, graphrag, and
		// known-answer stages below). On a heavy qwen3-8b run those stages leave
		// abandoned-but-still-executing synthesis requests on the answer model — the
		// client gives up long before the raised NATS handler deadline, so the request
		// keeps running and the retry layer piles on — which starves a late-positioned
		// recorder until its own client deadline (observed: run stalled >300s on the
		// first B0 question). Positioned here it gets a clean answer model: the stages
		// above are pure graph traversal / index reads (no answer-model synthesis), and
		// communities + embeddings + community summaries are already populated.
		// Semantic-only; RECORDER, not a hard gate (see validate_thematic_eval.go
		// header). Only an unreachable transport fails.
		{"validate-thematic-answer-eval", s.executeValidateThematicAnswerEval, []string{"semantic"}},
		// Epic B increment B2 — partition co-location diagnostic — runs immediately
		// after B0 so its per-query plurality_share lines up 1:1 (by query id) with
		// the B0 theme-recall recorded on the same run. It only READS the level-0
		// COMMUNITY_INDEX partition (no LLM, no answer synthesis), so it answers the
		// B2 decision (partition vs synthesizer) deterministically on the cheap 1.7b
		// run. Semantic-only; RECORDER, not a hard gate (see
		// validate_partition_colocation.go header). Only an unreachable partition
		// index (GetAllCommunities) fails.
		{"validate-partition-colocation", s.executePartitionColocation, []string{"semantic"}},
		// NL intent routing tests (validates classifier → strategy routing through globalSearch)
		{"test-nl-path-intent", s.executeTestNLPathIntent, nil},
		{"test-nl-temporal-intent", s.executeTestNLTemporalIntent, []string{"statistical", "semantic"}},
		// Alias resolution via ALIAS_INDEX (structural - no ML)
		{"test-entity-by-alias", s.executeTestEntityByAlias, nil},
		// Predicate query API (structural - direct index queries)
		{"test-predicate-list", s.executeTestPredicateList, nil},
		{"test-predicate-stats", s.executeTestPredicateStats, nil},
		{"test-predicate-compound", s.executeTestPredicateCompound, nil},

		// === Tier 0 ONLY: Zero-ML constraint validation ===
		// These verify structural tier has NO ML inference
		{"validate-zero-embeddings", s.executeValidateZeroEmbeddings, []string{"structural"}},
		{"validate-zero-clusters", s.executeValidateZeroClusters, []string{"structural"}},
		{"validate-rule-transitions", s.executeValidateRuleTransitions, []string{"structural"}},
		{"validate-entity-triples", s.executeValidateEntityTriples, []string{"structural"}},
		// ADR-056 Decision-4 fourth path: a dangling entity-ID relationship target
		// is materialised as an envelope-bearing referential stub (structural — no ML).
		{"validate-referential-stub", s.executeValidateReferentialStub, []string{"structural"}},

		// === Tier 1+: Statistical capabilities (statistical + semantic) ===
		{"verify-search-quality", s.executeVerifySearchQuality, []string{"statistical", "semantic"}},
		{"test-http-gateway", s.executeTestHTTPGateway, []string{"statistical", "semantic"}},
		// gh#768: gateway response SHAPE. Every other gateway stage decodes into
		// typed structs, and the wrapped and unwrapped shapes both decode
		// cleanly — so a shape regression is invisible to them by construction.
		// This one asserts over raw JSON keys and HARD-FAILs on a repeated
		// `data` hop. Statistical tier because that is the per-PR tier, and this
		// stage's value is running on the PR that changes the shape.
		{"validate-gateway-response-shape", s.executeValidateGatewayResponseShape, []string{"statistical", "semantic"}},
		{"test-embedding-fallback", s.executeTestEmbeddingFallback, []string{"statistical", "semantic"}},
		{"validate-community-structure", s.executeValidateCommunityStructure, []string{"statistical", "semantic"}},
		// Structural indexes (k-core, pivot) - require community detection for meaningful structure
		{"validate-kcore-index", s.executeValidateKCoreIndex, []string{"statistical", "semantic"}},
		{"validate-pivot-index", s.executeValidatePivotIndex, []string{"statistical", "semantic"}},

		// Phase 5: New index feature verification (all tiers - hierarchy inference is structural)
		// Verifies ContextIndex tracks inference provenance (hierarchy, structural contexts)
		{"validate-context-index-hierarchy", s.validateContextIndexHierarchy, []string{"structural", "statistical", "semantic"}},
		// Verifies IncomingIndex stores predicates (bidirectional traversal preserves relationship types)
		{"validate-incoming-index-predicates", s.validateIncomingIndexPredicates, []string{"structural", "statistical", "semantic"}},

		// Phase 6: Story-telling scenarios that demonstrate feature value (all tiers)
		// Story: "I can audit which relationships came from inference vs user input"
		{"validate-context-provenance-audit", s.validateContextProvenanceAudit, []string{"structural", "statistical", "semantic"}},
		// Story: "I can find who references a container and understand WHY"
		{"validate-bidirectional-traversal", s.validateBidirectionalTraversal, []string{"structural", "statistical", "semantic"}},
		// Story: "Containers explicitly know their members via 'contains' edges"
		{"validate-inverse-edges-materialized", s.validateInverseEdgesMaterialized, []string{"structural", "statistical", "semantic"}},

		// === Tier 2: Semantic capabilities (semantic only) ===
		{"test-graphrag-local", s.executeTestGraphRAGLocal, []string{"semantic"}},
		{"test-graphrag-global", s.executeTestGraphRAGGlobal, []string{"semantic"}},
		// validate-globalsearch-known-answer is the load-bearing guard against
		// the "globalSearch returns count=0 for content that exists" bug class
		// (see semspec Meshtastic report). Probes deterministic single-word
		// terms and HARD-FAILs when the response is empty or unrelated.
		{"validate-globalsearch-known-answer", s.executeValidateGlobalSearchKnownAnswer, []string{"semantic"}},
		// gh#599/#597: graph batch/semantic read-path reconciliation over the real
		// NATS wire (gh#604 contracts) + batch_query_missing_total soak signal. Runs
		// after entity stabilization + embedding readiness so present IDs are
		// queryable and the semantic index ranks. The fusion.Fuse envelope third is
		// re-homed to gh#391 (unreachable — no fusion route in configs/semantic.json).
		{"validate-batch-read-reconciliation", s.executeValidateBatchReadReconciliation, []string{"semantic"}},
		{"validate-anomaly-detection", s.executeValidateAnomalyDetection, []string{"statistical", "semantic"}},
		{"validate-virtual-edges", s.executeValidateVirtualEdges, []string{"semantic"}},

		// Wait for rule evaluations to stabilize before validating
		// With feedback loop prevention, rules should stabilize quickly for all tiers
		{"wait-for-rule-stabilization", s.executeWaitForRuleStabilization, nil},

		// === Common validation stages (all tiers) ===
		{"validate-rules", s.executeValidateRules, nil},
		{"validate-metrics", s.executeValidateMetrics, nil},
		{"verify-outputs", s.executeVerifyOutputs, nil},
	}

	// Filter stages based on variant
	stages := []stage{}
	for _, st := range allStages {
		if len(st.variants) == 0 {
			stages = append(stages, st)
		} else {
			for _, allowedVariant := range st.variants {
				if variant == allowedVariant {
					stages = append(stages, st)
					break
				}
			}
		}
	}
	return stages
}

// executeStages runs all stages with progress logging.
func (s *TieredScenario) executeStages(ctx context.Context, result *Result, stages []stage) bool {
	totalStages := len(stages)
	for i, stage := range stages {
		fmt.Printf("\n[%d/%d] %s starting...\n", i+1, totalStages, stage.name)
		stageStart := time.Now()

		if err := stage.fn(ctx, result); err != nil {
			duration := time.Since(stageStart)
			fmt.Printf("[%d/%d] %s FAILED after %v: %v\n", i+1, totalStages, stage.name, duration, err)
			result.Success = false
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			return false
		}

		duration := time.Since(stageStart)
		fmt.Printf("[%d/%d] %s completed in %v\n", i+1, totalStages, stage.name, duration)
		result.Metrics[fmt.Sprintf("%s_duration_ms", stage.name)] = duration.Milliseconds()
		s.printMetricsIfApplicable(ctx, stage.name)
	}
	return true
}

// printMetricsIfApplicable prints key metrics after data-affecting stages.
func (s *TieredScenario) printMetricsIfApplicable(ctx context.Context, stageName string) {
	if !strings.Contains(stageName, "send") && !strings.Contains(stageName, "validate") && !strings.Contains(stageName, "verify") {
		return
	}
	// Both counters carry labels, so an exact map lookup against a parsed
	// snapshot silently reports zero. Sum every series by metric name instead.
	entities, entityErr := s.metrics.SumMetricsByName(ctx, "semstreams_datamanager_entities_updated_total")
	rules, ruleErr := s.metrics.SumMetricsByName(ctx, "semstreams_rule_evaluations_total")
	if entityErr != nil && ruleErr != nil {
		return
	}
	fmt.Printf("  Metrics: entities=%.0f, rules=%.0f\n", entities, rules)
}

// captureAndCompareBaseline captures final metrics and compares against baseline file if configured.
func (s *TieredScenario) captureAndCompareBaseline(ctx context.Context, result *Result) {
	endBaseline, err := s.metrics.CaptureBaseline(ctx)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to capture end baseline: %v", err))
		return
	}

	result.Details["baseline_snapshot"] = map[string]any{
		"timestamp":   time.Now(),
		"duration_ms": time.Since(result.StartTime).Milliseconds(),
		"metrics":     endBaseline.Metrics,
	}

	if s.config.BaselineFile == "" {
		return
	}
	baselineData, err := os.ReadFile(s.config.BaselineFile)
	if err != nil {
		return
	}
	var loadedBaseline struct {
		Metrics map[string]float64 `json:"metrics"`
	}
	if json.Unmarshal(baselineData, &loadedBaseline) != nil {
		return
	}

	regressions := s.detectRegressions(endBaseline.Metrics, loadedBaseline.Metrics)
	if len(regressions) > 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Performance regressions detected: %v", regressions))
	}
	result.Details["baseline_comparison"] = map[string]any{"baseline_file": s.config.BaselineFile, "regressions": regressions}
}

// detectRegressions compares current metrics against baseline and returns regression descriptions.
func (s *TieredScenario) detectRegressions(current, baseline map[string]float64) []string {
	var regressions []string
	for metric, baselineValue := range baseline {
		if currentValue, ok := current[metric]; ok && baselineValue > 0 {
			percentChange := ((currentValue - baselineValue) / baselineValue) * 100
			if percentChange < -s.config.MaxRegressionPercent {
				regressions = append(regressions, fmt.Sprintf("%s: %.1f%% regression", metric, -percentChange))
			}
		}
	}
	return regressions
}

// Execute runs the tiered semantic test scenario
func (s *TieredScenario) Execute(ctx context.Context) (*Result, error) {
	result := &Result{
		ScenarioName: s.name, StartTime: time.Now(), Success: false,
		Metrics: make(map[string]any), Details: make(map[string]any),
		Errors: []string{}, Warnings: []string{},
	}

	variant := s.config.Variant
	if variant == "" {
		info := s.detectVariantAndProvider(result)
		variant = info.variant
		result.Details["detected_variant"] = variant
		result.Details["detected_embedding_provider"] = info.embeddingProvider
	}
	result.Metrics["variant"] = variant

	stages := s.getStagesForVariant(variant)
	if !s.executeStages(ctx, result, stages) {
		return result, nil
	}

	s.captureAndCompareBaseline(ctx, result)

	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	result.Structured = BuildTieredResults(result, s.searchStats)

	if err := s.validateSemanticRequirements(result); err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("semantic tier validation failed: %v", err)
		return result, nil
	}

	if err := s.validateFallbackBehavior(result); err != nil {
		result.Success = false
		result.Error = fmt.Sprintf("fallback validation failed: %v", err)
		return result, nil
	}

	return result, nil
}

// Teardown cleans up after the scenario
func (s *TieredScenario) Teardown(ctx context.Context) error {
	// Close NATS client if it was created
	if s.natsClient != nil {
		if err := s.natsClient.Close(ctx); err != nil {
			return fmt.Errorf("failed to close NATS client: %w", err)
		}
		s.natsClient = nil
	}
	return nil
}

// Validation functions extracted to:
// - validate_search.go: Search quality validation
// - validate_entity.go: Entity validation
// - validate_infra.go: Infrastructure validation
// - validate_structural.go: Structural index validation

// variantInfo holds detected variant and embedding provider information
type variantInfo struct {
	variant           string
	embeddingProvider string
}

// detectVariantAndProvider detects which variant (structural/statistical/semantic) is running based on semembed availability and metrics.
// Results are cached on first call and reused for subsequent calls.
func (s *TieredScenario) detectVariantAndProvider(result *Result) variantInfo {
	// Return cached result if already detected
	if s.detectedVariant != nil {
		return *s.detectedVariant
	}

	info := variantInfo{variant: "structural", embeddingProvider: "disabled"} // Default to structural (no embeddings)

	// Check semembed availability first
	if semembedAvailable, ok := result.Details["semembed_available"].(bool); ok && semembedAvailable {
		info.variant = "semantic"
		info.embeddingProvider = "http"
	}

	// Check embedding provider from metrics (overrides semembed detection)
	metricsURL := s.config.MetricsURL + "/metrics"
	resp, err := http.Get(metricsURL)
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		metricsText := string(body)

		// Try new metric first: semstreams_graph_embedding_embedder_type
		// Format: semstreams_graph_embedding_embedder_type <value>
		// 0=disabled, 1=bm25, 2=http
		re := regexp.MustCompile(`semstreams_graph_embedding_embedder_type\s+(\d+(?:\.\d+)?)`)
		matches := re.FindStringSubmatch(metricsText)

		// Fall back to legacy metric: indexengine_embedding_provider
		if len(matches) <= 1 {
			re = regexp.MustCompile(`indexengine_embedding_provider\{[^}]*\}\s+(\d+(?:\.\d+)?)`)
			matches = re.FindStringSubmatch(metricsText)
		}

		if len(matches) > 1 {
			switch matches[1] {
			case "2", "2.0":
				info.embeddingProvider = "http"
				info.variant = "semantic"
			case "1", "1.0":
				info.embeddingProvider = "bm25"
				info.variant = "statistical"
			case "0", "0.0":
				info.embeddingProvider = "disabled"
				info.variant = "structural" // No embeddings = structural (rules-only)
			}
		}
		// If metric not found, defaults remain: structural with disabled embeddings
		// This is correct because structural tier doesn't initialize semantic search,
		// so the embedding_provider metric is never registered/set
	}

	// Cache the result for future calls
	s.detectedVariant = &info

	return info
}

// NOTE: Comparison functions removed - use CLI compare instead:
//   ./e2e --compare-structured --baseline results/structural.json --target results/statistical.json
// The structured results from each run contain all the data needed for comparison.

// CommunityComparison represents a comparison of statistical vs LLM summaries for a community
type CommunityComparison struct {
	CommunityID        string   `json:"community_id"`
	Level              int      `json:"level"`
	MemberCount        int      `json:"member_count"`
	StatisticalSummary string   `json:"statistical_summary"`
	LLMSummary         string   `json:"llm_summary,omitempty"`
	SummaryStatus      string   `json:"summary_status"`
	Keywords           []string `json:"keywords"`
	SummaryLengthRatio float64  `json:"summary_length_ratio,omitempty"`
	WordOverlap        float64  `json:"word_overlap,omitempty"`
}

// CommunitySummaryReport contains aggregated community summary metrics
type CommunitySummaryReport struct {
	Variant               string                `json:"variant"`
	Timestamp             time.Time             `json:"timestamp"`
	CommunitiesTotal      int                   `json:"communities_total"`
	LLMEnhancedCount      int                   `json:"llm_enhanced_count"`
	StatisticalOnlyCount  int                   `json:"statistical_only_count"`
	LLMFailedCount        int                   `json:"llm_failed_count,omitempty"`
	LLMPendingCount       int                   `json:"llm_pending_count,omitempty"`
	LLMWaitDurationMs     int64                 `json:"llm_wait_duration_ms,omitempty"`
	AvgSummaryLengthRatio float64               `json:"avg_summary_length_ratio"`
	AvgWordOverlap        float64               `json:"avg_word_overlap"`
	NonSingletonCount     int                   `json:"non_singleton_count"`
	LargestCommunitySize  int                   `json:"largest_community_size"`
	AvgNonSingletonSize   float64               `json:"avg_non_singleton_size"`
	Communities           []CommunityComparison `json:"communities"`
}

// llmWaitResult holds the results of waiting for LLM enhancement
type llmWaitResult struct {
	durationMs   int64
	failedCount  int
	pendingCount int
}

// communityStats holds aggregated statistics about communities
type communityStats struct {
	comparisons          []CommunityComparison
	llmEnhancedCount     int
	statisticalOnlyCount int
	avgLengthRatio       float64
	avgWordOverlap       float64
	nonSingletonCount    int
	largestCommunitySize int
	avgNonSingletonSize  float64
}

// Semantic tier functions are in tiered_semantic.go
// Structural tier functions are in tiered_structural.go
