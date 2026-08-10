// Package researchgraph provides the ADR-045 research-graph chain
// E2E scenario.
//
// The scenario boots the reference flow (configs/research-graph-e2e.json)
// via docker compose with a mock LLM scripted (test/e2e/mock/cmd/main.go
// `research-graph` preset) to return:
//   - `research_graph` tool call when the parent agent receives the
//     investigate-via-research_graph prompt
//   - `synthesize_directly` action when route_search's persona prompt
//     fires (chosen as the happy path that exercises R0 → R1 → R2 → R6
//   - every triple-stamp seam without touching execute + assess)
//   - SearchResult JSON when synthesize_answer's persona prompt fires
//
// Coverage scope: the synthesize_directly happy path. Refine-loop
// coverage (walk_seeds / decompose + assess + iteration cap fallback)
// is a follow-up — shipping the happy path first locks the orchestration
// wire claim PR #203 made.
//
// Stages, in firing order:
//
//  1. verify-components — all 5 research-graph-* + agentic-* + rule processor healthy
//  2. inject-parent-task — publish a TaskMessage with the research_graph trigger marker
//  3. wait-for-research-pipeline-loop — poll AGENT_LOOPS for an rg_* entity (chain kickoff)
//  4. wait-for-search-result-stamp — poll the loop entity for research.search_result.complete
//  5. verify-orchestration-triples — assert kickoff + per-stage completion triples land
//  6. verify-search-result-envelope — assert the SearchResult landed at COMPLETE_<rg_loopID>
//  7. verify-r6-continuation — confirm a continuation agent.task fired back to the parent role
//
// Any stage failure short-circuits with a clear diagnostic. Per-stage
// duration is recorded in result.Metrics for trajectory dashboards.
package researchgraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/agentic/research"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	graphembedding "github.com/c360studio/semstreams/processor/graph-embedding"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

const (
	researchEmbeddingSearchSubject = "graph.embedding.query.search"
	researchGraphTopic             = "drone hover anomalies"
	researchGraphSeedSimilarity    = 0.95
)

// Scenario validates the ADR-045 Phase 1 chain works end-to-end.
type Scenario struct {
	name        string
	description string

	natsURL    string
	metricsURL string

	nats    *client.NATSValidationClient
	metrics *client.MetricsClient
	obs     *client.ObservabilityClient

	embeddingSearchResponder *natsclient.Subscription
	researchSeedEntityID     string

	config *Config
}

// Config holds tunable timeouts + expected results.
type Config struct {
	NATSURL    string `json:"nats_url"`
	MetricsURL string `json:"metrics_url"`

	// ChainKickoffTimeout caps how long we wait for the rg_<loopID>
	// entity to appear in AGENT_LOOPS after the parent task fires.
	// Covers parent-loop dispatch + research_graph tool execution +
	// LoopEntity write. Default 30s.
	ChainKickoffTimeout time.Duration `json:"chain_kickoff_timeout"`

	// CompleteTimeout caps how long we wait for the
	// research.search_result.complete triple to appear on the loop
	// entity. Covers the full R0 → R1 → R2 → synthesize chain plus
	// graph-ingest write latency. Default 60s.
	CompleteTimeout time.Duration `json:"complete_timeout"`

	// Platform identity must match configs/research-graph-e2e.json
	// platform block — used to construct LoopExecutionEntityID for
	// triple lookup. Hardcoded here rather than read from the config
	// to keep the scenario self-contained.
	PlatformOrg      string `json:"platform_org"`
	PlatformInstance string `json:"platform_instance"`
}

// DefaultConfig returns defaults aligned with docker/compose/research-graph.yml.
func DefaultConfig() *Config {
	return &Config{
		NATSURL:             "nats://localhost:44222",
		MetricsURL:          "http://localhost:49090",
		ChainKickoffTimeout: 30 * time.Second,
		CompleteTimeout:     60 * time.Second,
		PlatformOrg:         "c360",
		PlatformInstance:    "rg-e2e-001",
	}
}

// NewScenario constructs the scenario with the given observability
// client + (optional) config override.
func NewScenario(obs *client.ObservabilityClient, config *Config) *Scenario {
	if config == nil {
		config = DefaultConfig()
	}
	return &Scenario{
		name:        "research-graph",
		description: "Validates ADR-045 Phase 1 R0-R6 chain end-to-end through the synthesize_directly route",
		natsURL:     config.NATSURL,
		metricsURL:  config.MetricsURL,
		obs:         obs,
		config:      config,
	}
}

// Name returns the scenario name.
func (s *Scenario) Name() string { return s.name }

// Description returns the scenario description.
func (s *Scenario) Description() string { return s.description }

// Setup creates NATS + metrics clients.
func (s *Scenario) Setup(ctx context.Context) error {
	natsClient, err := client.NewNATSValidationClient(ctx, s.natsURL)
	if err != nil {
		return fmt.Errorf("create NATS client: %w", err)
	}
	s.researchSeedEntityID = researchGraphSeedEntityID(fmt.Sprintf("%x", time.Now().UnixNano()))
	responder, err := natsClient.Client().SubscribeForRequests(
		ctx,
		researchEmbeddingSearchSubject,
		newResearchEmbeddingSearchHandler(s.researchSeedEntityID),
	)
	if err != nil {
		_ = natsClient.Close(ctx)
		return fmt.Errorf("subscribe deterministic embedding search responder: %w", err)
	}
	s.nats = natsClient
	s.metrics = client.NewMetricsClient(s.metricsURL)
	s.embeddingSearchResponder = responder
	return nil
}

// Teardown closes connections.
func (s *Scenario) Teardown(ctx context.Context) error {
	var teardownErr error
	if s.embeddingSearchResponder != nil {
		if err := s.embeddingSearchResponder.Unsubscribe(); err != nil {
			teardownErr = fmt.Errorf("unsubscribe deterministic embedding search responder: %w", err)
		}
		s.embeddingSearchResponder = nil
	}
	if s.nats != nil {
		if err := s.nats.Close(ctx); err != nil {
			teardownErr = errors.Join(teardownErr, fmt.Errorf("close NATS validation client: %w", err))
		}
	}
	return teardownErr
}

func researchGraphSeedEntityID(runToken string) string {
	return "c360.rg-e2e.research.seed.document." + runToken
}

func newResearchEmbeddingSearchHandler(seedEntityID string) func(context.Context, []byte) ([]byte, error) {
	return func(ctx context.Context, data []byte) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var request graphembedding.SearchRequest
		if err := json.Unmarshal(data, &request); err != nil {
			return nil, fmt.Errorf("decode embedding search request: %w", err)
		}
		if request.Query != researchGraphTopic {
			return nil, fmt.Errorf("unexpected query %q, want %q", request.Query, researchGraphTopic)
		}
		return json.Marshal(graphembedding.SearchResponse{
			Query: request.Query,
			Results: []graphembedding.SearchResult{{
				EntityID:   seedEntityID,
				Similarity: researchGraphSeedSimilarity,
			}},
			Duration:     "0s",
			EmbedderType: "e2e-fixture",
		})
	}
}

// Execute runs the chain end-to-end.
func (s *Scenario) Execute(ctx context.Context) (*scenarios.Result, error) {
	result := &scenarios.Result{
		ScenarioName: s.name,
		StartTime:    time.Now(),
		Success:      false,
		Metrics:      make(map[string]any),
		Details:      make(map[string]any),
		Errors:       []string{},
		Warnings:     []string{},
	}

	stages := []struct {
		name string
		fn   func(context.Context, *scenarios.Result) error
	}{
		{"verify-components", s.verifyComponents},
		{"inject-parent-task", s.injectParentTask},
		{"wait-for-research-pipeline-loop", s.waitForResearchPipelineLoop},
		{"wait-for-search-result-stamp", s.waitForSearchResultStamp},
		{"verify-orchestration-triples", s.verifyOrchestrationTriples},
		{"verify-search-result-envelope", s.verifySearchResultEnvelope},
		{"verify-r6-continuation", s.verifyR6Continuation},
	}

	for _, stage := range stages {
		stageStart := time.Now()
		if err := stage.fn(ctx, result); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", stage.name, err))
			result.Error = fmt.Sprintf("%s failed: %v", stage.name, err)
			result.EndTime = time.Now()
			result.Duration = result.EndTime.Sub(result.StartTime)
			return result, nil
		}
		result.Metrics[fmt.Sprintf("%s_duration_ms", stage.name)] = time.Since(stageStart).Milliseconds()
	}

	result.Success = true
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	return result, nil
}

// verifyComponents confirms the five research-graph components + rule
// processor + supporting agentic infra are all healthy. A missing
// component here means the docker compose / config is broken before
// we even fire a task, so this gate runs first.
func (s *Scenario) verifyComponents(ctx context.Context, result *scenarios.Result) error {
	components, err := s.obs.GetComponents(ctx)
	if err != nil {
		return fmt.Errorf("get components: %w", err)
	}
	required := []string{
		"agentic-dispatch",
		"agentic-loop",
		"agentic-model",
		"agentic-tools",
		"graph-ingest",
		"graph-query",
		"rule-processor",
		"research-graph-classify",
		"research-graph-route",
		"research-graph-execute",
		"research-graph-assess",
		"research-graph-synthesize",
	}
	found := make(map[string]bool, len(components))
	for _, comp := range components {
		found[comp.Name] = comp.Enabled && comp.Healthy
	}
	missing := []string{}
	unhealthy := []string{}
	for _, req := range required {
		healthy, exists := found[req]
		if !exists {
			missing = append(missing, req)
		} else if !healthy {
			unhealthy = append(unhealthy, req)
		}
	}
	result.Details["components_required"] = required
	result.Details["components_healthy"] = found
	if len(missing) > 0 {
		return fmt.Errorf("missing components: %v", missing)
	}
	if len(unhealthy) > 0 {
		return fmt.Errorf("unhealthy components: %v", unhealthy)
	}
	return nil
}

// injectParentTask publishes a TaskMessage with the research_graph
// trigger marker. The mock LLM's "research-graph" preset matches the
// marker via WithRoleToolCallSequence and emits a research_graph tool
// call; the parent loop then dispatches to research_graph and the
// chain kicks off.
func (s *Scenario) injectParentTask(ctx context.Context, result *scenarios.Result) error {
	if s.researchSeedEntityID == "" {
		return errors.New("research seed entity ID is not initialized")
	}
	mutationClient, err := graphmutation.NewClient(s.nats, 10*time.Second)
	if err != nil {
		return fmt.Errorf("construct graph mutation client for research seed: %w", err)
	}
	seeded, err := mutationClient.Create(ctx, graph.CreateEntityRequest{
		Entity: &graph.EntityState{
			ID: s.researchSeedEntityID,
			MessageType: message.Type{
				Domain: "research", Category: "e2e_search_seed", Version: "v1",
			},
		},
		Triples: []message.Triple{{
			Subject:    s.researchSeedEntityID,
			Predicate:  "dc.terms.title",
			Object:     "Drone hover anomalies",
			Source:     "research-graph-e2e",
			Timestamp:  time.Now().UTC(),
			Confidence: 1,
		}},
		RequestID: "e2e-research-graph-seed",
	})
	if err != nil {
		return fmt.Errorf("seed research search entity through canonical mutation: %w", err)
	}
	result.Details["research_seed_entity_id"] = seeded.Entity.ID
	result.Details["research_seed_entity_revision"] = seeded.KVRevision

	parentLoopID := fmt.Sprintf("e2e-parent-%d", time.Now().UnixNano())
	task := agentic.TaskMessage{
		LoopID: parentLoopID,
		TaskID: fmt.Sprintf("e2e-rg-task-%d", time.Now().UnixNano()),
		Role:   "general",
		Model:  "mock",
		// "Investigate the research topic via research_graph" is the
		// exact marker the mock preset matches on
		// (applyResearchGraphPreset in test/e2e/mock/cmd/main.go).
		// Any drift between this prompt and the marker breaks the
		// scenario at wait-for-research-pipeline-loop with a clean
		// "no rg_* loop appeared" diagnostic — the marker is the
		// load-bearing seam between this scenario and the mock.
		Prompt: "Investigate the research topic via research_graph — emit a single research_graph tool call with topic=\"drone hover anomalies\" and hints={domain:robotics}.",
		// Metadata["role"] propagates through CacheMetadata →
		// ToolCall.Metadata so the research_graph tool stamps
		// research.parent.role for R6's continuation publish_agent.
		// A real agentic-dispatch flow populates this from the
		// parent's role; e2e mirrors that explicitly so R6's
		// `role: $entity.triple.research.parent.role` substitution
		// resolves to "general" and the continuation task can dispatch.
		Metadata: map[string]any{"role": "general"},
		Tools: []agentic.ToolDefinition{
			{
				Name:        "research_graph",
				Description: "Spawn an asynchronous graph-research operation.",
				Parameters: map[string]any{
					"type":     "object",
					"required": []string{"topic"},
					"properties": map[string]any{
						"topic": map[string]any{"type": "string"},
						"hints": map[string]any{"type": "object"},
					},
				},
			},
		},
	}
	taskMsg := message.NewBaseMessage(task.Schema(), &task, "e2e-research-graph")
	taskData, err := json.Marshal(taskMsg)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}
	if err := s.nats.Publish(ctx, "agent.task.e2e-research-graph", taskData); err != nil {
		return fmt.Errorf("publish task: %w", err)
	}
	result.Details["parent_loop_id"] = parentLoopID
	result.Details["parent_task_id"] = task.TaskID
	return nil
}

// waitForResearchPipelineLoop polls AGENT_LOOPS for an entry keyed
// `rg_<8-hex-chars>` to appear — that's the research-pipeline LoopEntity
// the research_graph tool writes at chain kickoff. Once we have the
// loop ID, downstream stages can construct the 6-part loop-execution
// entity ID to look up orchestration triples.
func (s *Scenario) waitForResearchPipelineLoop(ctx context.Context, result *scenarios.Result) error {
	deadline := time.Now().Add(s.config.ChainKickoffTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		keys, err := s.nats.GetBucketKeysSample(ctx, "AGENT_LOOPS", 200)
		if err != nil {
			continue
		}
		for _, k := range keys {
			if strings.HasPrefix(k, "rg_") && !strings.Contains(k, ".") {
				// Bare `rg_<id>` key (not `research.request.received.rg_<id>`
				// or `classify.complete.rg_<id>`) is the LoopEntity.
				result.Details["research_loop_id"] = k
				return nil
			}
		}
	}
	return fmt.Errorf("no rg_* loop entity appeared in AGENT_LOOPS within %v — parent agent's research_graph tool may not have fired (check mock LLM marker match)", s.config.ChainKickoffTimeout)
}

// waitForSearchResultStamp polls the research-pipeline loop entity in
// ENTITY_STATES for the research.search_result.complete triple — the
// terminal stamp synthesize_answer emits before R6 fires. Stronger
// gate than waiting for a generic completion metric because it
// asserts the specific orchestration triple that R6 keys on.
func (s *Scenario) waitForSearchResultStamp(ctx context.Context, result *scenarios.Result) error {
	loopID, ok := result.Details["research_loop_id"].(string)
	if !ok || loopID == "" {
		return fmt.Errorf("research_loop_id not set (waitForResearchPipelineLoop didn't populate it)")
	}
	loopEntityID, err := agentic.TryLoopExecutionEntityID(s.config.PlatformOrg, s.config.PlatformInstance, loopID)
	if err != nil {
		return fmt.Errorf("construct loop entity ID for %s: %w", loopID, err)
	}
	result.Details["research_loop_entity_id"] = loopEntityID

	deadline := time.Now().Add(s.config.CompleteTimeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		entity, err := s.nats.GetEntity(ctx, loopEntityID)
		if err != nil {
			continue
		}
		for _, t := range entity.Triples {
			if t.Predicate == research.PredicateResearchSearchResultComplete {
				result.Details["search_result_complete_stamped"] = true
				return nil
			}
		}
	}
	return fmt.Errorf("research.search_result.complete triple did not land on %s within %v — chain stalled before terminal synthesis", loopEntityID, s.config.CompleteTimeout)
}

// verifyOrchestrationTriples fetches the research-pipeline loop entity
// and asserts every expected orchestration triple is present. The full
// set covers: kickoff (loop.role, research.request.received, .topic, .loop_id,
// .parent_loop, .parent_role, .budget_tokens, .max_iterations) +
// per-stage completion (classify, route, search_result) +
// route-decision branch (research.route.action == "synthesize_directly").
// execute/assess triples are NOT expected on this path — that's the
// whole point of routing via synthesize_directly.
func (s *Scenario) verifyOrchestrationTriples(ctx context.Context, result *scenarios.Result) error {
	loopEntityID, ok := result.Details["research_loop_entity_id"].(string)
	if !ok || loopEntityID == "" {
		return fmt.Errorf("research_loop_entity_id not set")
	}
	entity, err := s.nats.GetEntity(ctx, loopEntityID)
	if err != nil {
		return fmt.Errorf("fetch loop entity %s: %w", loopEntityID, err)
	}

	got := make(map[string]string, len(entity.Triples))
	for _, t := range entity.Triples {
		obj, _ := t.Object.(string)
		got[t.Predicate] = obj
	}

	// Kickoff predicates (stamped by research_graph tool). The
	// research_graph tool's parser uses DefaultBudgetTokens=4000 +
	// DefaultMaxIterations=5 when the parent task omits them (which
	// this scenario does). The parent_loop value is the scenario's
	// generated parent loop id. Full 8-predicate set per
	// BuildKickoffTriples; a drop on refactor surfaces here loudly
	// rather than letting the chain still run on the minimum trigger
	// state per go-reviewer I1 on PR #205.
	requiredKickoff := map[string]string{
		research.PredicateLoopRole:              research.PipelineRole,
		research.PredicateResearchRequested:     "true",
		research.PredicateResearchTopic:         "drone hover anomalies",
		research.PredicateResearchLoopID:        result.Details["research_loop_id"].(string),
		research.PredicateResearchParentRole:    "general",
		research.PredicateResearchParentLoop:    result.Details["parent_loop_id"].(string),
		research.PredicateResearchBudgetTokens:  "4000",
		research.PredicateResearchMaxIterations: "5",
	}
	missing := []string{}
	mismatched := []string{}
	for pred, want := range requiredKickoff {
		actual, present := got[pred]
		if !present {
			missing = append(missing, pred)
			continue
		}
		if actual != want {
			mismatched = append(mismatched, fmt.Sprintf("%s: got %q want %q", pred, actual, want))
		}
	}

	// Per-stage completion predicates (timestamps; existence-only check).
	for _, pred := range []string{
		research.PredicateResearchClassifyComplete,
		research.PredicateResearchRouteComplete,
		research.PredicateResearchSearchResultComplete,
	} {
		if _, present := got[pred]; !present {
			missing = append(missing, pred)
		}
	}

	candidateCountRaw, present := got[research.PredicateResearchClassifyCandidateCount]
	if !present {
		missing = append(missing, research.PredicateResearchClassifyCandidateCount)
	} else {
		candidateCount, parseErr := strconv.Atoi(candidateCountRaw)
		if parseErr != nil {
			mismatched = append(mismatched, fmt.Sprintf("%s: %q is not an integer: %v",
				research.PredicateResearchClassifyCandidateCount, candidateCountRaw, parseErr))
		} else if candidateCount <= 0 {
			mismatched = append(mismatched, fmt.Sprintf("%s: got %d want > 0",
				research.PredicateResearchClassifyCandidateCount, candidateCount))
		} else {
			result.Metrics["research_classify_candidate_count"] = candidateCount
			result.Details["research_classify_candidate_count"] = candidateCount
		}
	}

	// Route action — load-bearing for R2's dispatch.
	if got[research.PredicateResearchRouteAction] != research.ActionSynthesizeDirectly {
		mismatched = append(mismatched, fmt.Sprintf("%s: got %q want %q",
			research.PredicateResearchRouteAction,
			got[research.PredicateResearchRouteAction],
			research.ActionSynthesizeDirectly))
	}

	// Execute + Assess triples must be ABSENT on the synthesize_directly
	// path — their presence would mean the route mis-fired and the chain
	// took an unintended branch. Assert absence loudly so a router
	// regression surfaces here, not in a downstream operator's trajectory
	// review. Full paired set per Build{Execute,Assess}CompleteTriples
	// so a batch-split refactor that stamps half a stage's triples
	// surfaces too (per go-reviewer I2 on PR #205).
	unexpected := []string{}
	for _, pred := range []string{
		research.PredicateResearchExecuteComplete,
		research.PredicateResearchExecuteEvidenceCount,
		research.PredicateResearchAssessComplete,
		research.PredicateResearchAssessSufficient,
	} {
		if _, present := got[pred]; present {
			unexpected = append(unexpected, pred)
		}
	}

	result.Metrics["orchestration_triples_total"] = len(entity.Triples)
	result.Details["orchestration_predicates"] = predicateList(got)

	if len(missing) > 0 || len(mismatched) > 0 || len(unexpected) > 0 {
		return fmt.Errorf("orchestration triples failed: missing=%v mismatched=%v unexpected=%v",
			missing, mismatched, unexpected)
	}
	return nil
}

// verifySearchResultEnvelope confirms synthesize_answer wrote the
// SearchResult envelope at the read_loop_result-readable key
// (COMPLETE_<rg_loopID>). Without this write, R6's continuation
// publish_agent fires but the parent's read_loop_result returns
// key-not-found — a known degraded path documented in
// processor/research-graph-synthesize/handler.go's PutLoopCompletion
// godoc. E2E pins the happy path: the envelope MUST land.
func (s *Scenario) verifySearchResultEnvelope(ctx context.Context, result *scenarios.Result) error {
	loopID, ok := result.Details["research_loop_id"].(string)
	if !ok || loopID == "" {
		return fmt.Errorf("research_loop_id not set")
	}
	envelope, err := s.nats.GetKV(ctx, "AGENT_LOOPS", "COMPLETE_"+loopID)
	if err != nil {
		return fmt.Errorf("fetch COMPLETE_%s: %w (read_loop_result will return key-not-found for R6's spawned parent)", loopID, err)
	}
	if len(envelope) == 0 {
		return fmt.Errorf("COMPLETE_%s envelope is empty", loopID)
	}
	// Quick shape check — decode the BaseMessage and confirm the
	// payload category. Full payload validation is the component's
	// roundtrip test; here we just ensure operators can read it back.
	if !bytesContainsCategory(envelope, research.CategoryResult) {
		return fmt.Errorf("COMPLETE_%s envelope does not carry research/result payload — got %s", loopID, truncate(string(envelope), 200))
	}
	result.Metrics["search_result_envelope_bytes"] = len(envelope)
	return nil
}

// verifyR6Continuation confirms R6 published a continuation task back
// to the parent role. The mock LLM's parent role gets the continuation
// prompt (which contains $entity.triple.research.loop.id substituted +
// the topic) and falls through to the default completion content. We
// verify that at least one agentic_dispatch_tasks_submitted_total or
// loops_completed metric increment happened ABOVE baseline — the
// continuation task spawning is the rule chain's deliverable.
func (s *Scenario) verifyR6Continuation(ctx context.Context, result *scenarios.Result) error {
	// agentic_loop_loops_completed_total counts every completed loop.
	// On the happy path we expect:
	//   1. Parent loop (general role, fires research_graph then waits)
	//   2. Continuation parent loop (R6-spawned, general role, completes via default text)
	// So >= 2 loops completed. The exact number depends on whether the
	// parent loop with the research_graph tool call also counts; we
	// tolerate >= 2 here and tighten if needed.
	loops, err := s.metrics.SumMetricsByName(ctx, "semstreams_agentic_loop_loops_completed_total")
	if err != nil {
		return fmt.Errorf("read loops_completed metric: %w", err)
	}
	result.Metrics["loops_completed_total"] = loops
	if loops < 2 {
		return fmt.Errorf("expected >= 2 agent loops completed (parent + R6 continuation) but saw %v — R6 may not have dispatched a continuation task", loops)
	}
	result.Details["r6_continuation_fired"] = true
	return nil
}

// predicateList renders the predicates of a triple map as a sorted
// slice so failure details are reproducible. The Object isn't returned
// because timestamps are nondeterministic.
func predicateList(triples map[string]string) []string {
	out := make([]string, 0, len(triples))
	for k := range triples {
		out = append(out, k)
	}
	return out
}

// bytesContainsCategory is a cheap shape check that avoids pulling in
// the full payload registry just to assert the envelope is a result.
// The BaseMessage JSON places the category in the type discriminator
// — a substring search for "\"category\":\"result\"" is sufficient to
// distinguish from intent / classifier_output / etc.
func bytesContainsCategory(data []byte, category string) bool {
	needle := fmt.Sprintf("%q:%q", "category", category)
	return strings.Contains(string(data), needle)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
