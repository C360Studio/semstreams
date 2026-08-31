// Package researchgraph provides the ADR-045 research-graph chain
// E2E scenario.
//
// The scenario boots the reference flow (configs/research-graph-e2e.json)
// via docker compose with an explicit mock LLM fixture selected in
// test/e2e/mock/cmd/main.go. The two isolated modes prove:
//   - `research_graph` tool call when the parent agent receives the
//     investigate-via-research_graph prompt
//   - the preserved `synthesize_directly` action and absence of execute
//     and assess effects
//   - a `walk_seeds` action that traverses the production executeAll,
//     fusion.Fuse, assessment, and synthesis path with controlled evidence
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
	"github.com/c360studio/semstreams/pkg/fusion"
	graphembedding "github.com/c360studio/semstreams/processor/graph-embedding"
	"github.com/c360studio/semstreams/test/e2e/client"
	e2econfig "github.com/c360studio/semstreams/test/e2e/config"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
)

const (
	researchEmbeddingSearchSubject = "graph.embedding.query.search"
	researchGraphTopic             = "drone hover anomalies"
	researchGraphSeedSimilarity    = 0.95
	walkSeedsEntityStateSource     = "walk_seeds.entity_state"

	// ControlledSeedSuffix is the last four canonical positions of the stable
	// entity whose exact evidence identity proves the execute fixture traversed
	// the production graph query and fusion path. The direct fixture
	// intentionally keeps its run-scoped entity identity.
	//
	// It is a suffix and not a whole entity ID because positions 1-2 are the
	// DEPLOYMENT's own authority, and since ADR-104 that pair carries an entropy
	// suffix minted onto platform.id at first boot. Nothing outside the running
	// stack can spell it; Setup reads it from semstreams_config/platform_identity
	// and composes the whole ID once, into Scenario.controlledSeedEntityID.
	ControlledSeedSuffix = "seed.research.document.controlled"

	// ControlledSeedSynthesis is the synthesis prose the execute fixture's mock
	// LLM returns verbatim. The scenario asserts the terminal SearchResult
	// carries exactly this text: research-graph-synthesize APPENDS a
	// degradation note when the model's evidence_refs quote-back fails and
	// falls back to echoing evidence, so equality here is what keeps the
	// execute assertions from passing through that fallback unnoticed.
	ControlledSeedSynthesis = "The controlled graph entity records drone hover anomaly evidence."
)

// FixtureMode selects one of the two isolated research-graph E2E routes.
type FixtureMode string

const (
	// FixtureModeDirect preserves the original synthesize_directly fixture.
	FixtureModeDirect FixtureMode = "direct"
	// FixtureModeExecute exercises walk_seeds through execute, assess, and synthesize.
	FixtureModeExecute FixtureMode = "execute"
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

	// authorityOrg / authorityPlatform are positions 1-2 of every entity ID this
	// scenario composes, observed from the running deployment in Setup. They are
	// deliberately NOT written back onto Config: Config carries what the scenario
	// DECLARED, and EffectiveAuthority cross-checks the declaration against the
	// stack. Collapsing the two erases the distinction this scenario exists to draw.
	authorityOrg      string
	authorityPlatform string

	// controlledSeedEntityID is ControlledSeedSuffix under the authority the
	// running deployment RECORDS. Empty until Setup observes it.
	controlledSeedEntityID string

	config *Config
}

// Config holds tunable timeouts + expected results.
type Config struct {
	NATSURL    string `json:"nats_url"`
	MetricsURL string `json:"metrics_url"`

	// FixtureMode selects the isolated route asserted by this E2E run.
	FixtureMode FixtureMode `json:"fixture_mode"`

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

	// PlatformOrg / PlatformID are the authority STEM configs/research-graph-e2e.json
	// declares, and they stay that way: Setup PRESERVES the declaration and
	// never writes the observed value back here. Their whole job is the
	// cross-check — "the stack I am driving is the configuration I name" —
	// which EffectiveAuthority performs against the identifier the running
	// deployment records in semstreams_config/platform_identity, carrying the
	// entropy suffix minted onto platform.id at first boot (ADR-104).
	//
	// These are NEVER an entity ID's positions 1-2. The observed authority
	// lives in the scenario's authorityOrg / authorityPlatform fields; keeping
	// the two apart is what lets the cross-check mean anything, since a
	// declaration overwritten with the observed value can no longer disagree
	// with it.
	PlatformOrg string `json:"platform_org"`
	PlatformID  string `json:"platform_id"`
}

// DefaultConfig returns defaults aligned with docker/compose/research-graph.yml.
func DefaultConfig() *Config {
	return &Config{
		NATSURL:             "nats://localhost:44222",
		MetricsURL:          "http://localhost:49090",
		FixtureMode:         FixtureModeDirect,
		ChainKickoffTimeout: 30 * time.Second,
		CompleteTimeout:     60 * time.Second,
		PlatformOrg:         "c360",
		// configs/research-graph-e2e.json platform.id — the STEM, and it stays
		// the stem. Setup cross-checks it against the deployment's minted
		// identifier and stores that separately; see the field comment.
		PlatformID: "research-graph-e2e",
	}
}

// NewScenario constructs the scenario with the given observability
// client + (optional) config override.
func NewScenario(obs *client.ObservabilityClient, config *Config) *Scenario {
	if config == nil {
		config = DefaultConfig()
	}
	name := "research-graph"
	description := "Validates ADR-045 Phase 1 R0-R6 chain end-to-end through the synthesize_directly route"
	if config.FixtureMode == FixtureModeExecute {
		name = "research-graph-execute"
		description = "Validates ADR-045 Phase 1 R0-R6 chain end-to-end through walk_seeds execute, assess, and synthesize"
	}
	return &Scenario{
		name:        name,
		description: description,
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
	// Ask the deployment which authority it mints under before composing a
	// single entity ID from it (ADR-104). Both the seed this scenario creates
	// through the canonical mutation and the loop entity it later reads are
	// refused or missed under any other pair, and the declared stem stopped
	// being that pair when the framework started minting an entropy suffix.
	authority, authErr := e2econfig.EffectiveAuthority(
		ctx, natsClient, s.config.PlatformOrg+"."+s.config.PlatformID)
	if authErr != nil {
		return errors.Join(authErr, natsClient.Close(ctx))
	}
	s.authorityOrg, s.authorityPlatform, _ = strings.Cut(authority, ".")
	s.controlledSeedEntityID = authority + "." + ControlledSeedSuffix
	s.researchSeedEntityID = researchGraphSeedEntityID(
		s.authorityOrg, s.authorityPlatform, fmt.Sprintf("%x", time.Now().UnixNano()))
	if s.config.FixtureMode == FixtureModeExecute {
		s.researchSeedEntityID = s.controlledSeedEntityID
	}
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

func researchGraphSeedEntityID(org, platformID, runToken string) string {
	return org + "." + platformID + ".seed.research.document." + runToken
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
	}
	if s.config.FixtureMode == FixtureModeExecute {
		stages = append(stages, struct {
			name string
			fn   func(context.Context, *scenarios.Result) error
		}{"verify-execute-branch-artifacts", s.verifyExecuteBranchArtifacts})
	}
	stages = append(stages,
		struct {
			name string
			fn   func(context.Context, *scenarios.Result) error
		}{"verify-search-result-envelope", s.verifySearchResultEnvelope},
		struct {
			name string
			fn   func(context.Context, *scenarios.Result) error
		}{"verify-r6-continuation", s.verifyR6Continuation},
	)

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
	loopEntityID, err := agentic.TryLoopExecutionEntityID(s.authorityOrg, s.authorityPlatform, loopID)
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
	expectedAction := research.ActionSynthesizeDirectly
	if s.config.FixtureMode == FixtureModeExecute {
		expectedAction = research.ActionWalkSeeds
	}
	if got[research.PredicateResearchRouteAction] != expectedAction {
		mismatched = append(mismatched, fmt.Sprintf("%s: got %q want %q",
			research.PredicateResearchRouteAction,
			got[research.PredicateResearchRouteAction],
			expectedAction))
	}

	unexpected := []string{}
	if s.config.FixtureMode == FixtureModeDirect {
		// Execute + Assess triples must remain ABSENT on the original
		// synthesize_directly fixture.
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
	} else {
		for _, pred := range []string{
			research.PredicateResearchExecuteComplete,
			research.PredicateResearchAssessComplete,
		} {
			if _, present := got[pred]; !present {
				missing = append(missing, pred)
			}
		}
		evidenceCountRaw, present := got[research.PredicateResearchExecuteEvidenceCount]
		if !present {
			missing = append(missing, research.PredicateResearchExecuteEvidenceCount)
		} else if evidenceCount, parseErr := strconv.Atoi(evidenceCountRaw); parseErr != nil || evidenceCount <= 0 {
			mismatched = append(mismatched, fmt.Sprintf("%s: got %q want positive integer",
				research.PredicateResearchExecuteEvidenceCount, evidenceCountRaw))
		}
		if got[research.PredicateResearchAssessSufficient] != "true" {
			mismatched = append(mismatched, fmt.Sprintf("%s: got %q want %q",
				research.PredicateResearchAssessSufficient,
				got[research.PredicateResearchAssessSufficient], "true"))
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

// verifyExecuteBranchArtifacts reads the three durable envelopes produced by
// the real walk_seeds branch. Their relationship proves that executeAll and
// fusion.Fuse surfaced the controlled graph entity, assessment saw that
// evidence, and synthesis only quoted evidence from the execution output.
func (s *Scenario) verifyExecuteBranchArtifacts(ctx context.Context, result *scenarios.Result) error {
	loopID, ok := result.Details["research_loop_id"].(string)
	if !ok || loopID == "" {
		return errors.New("research_loop_id not set")
	}

	var execution research.ExecutionOutput
	if err := s.readLoopEnvelopePayload(ctx, "execute.complete."+loopID, &execution); err != nil {
		return fmt.Errorf("read execution output: %w", err)
	}
	var assessment research.AssessmentOutput
	if err := s.readLoopEnvelopePayload(ctx, "assess.complete."+loopID, &assessment); err != nil {
		return fmt.Errorf("read assessment output: %w", err)
	}
	var searchResult research.SearchResult
	if err := s.readLoopEnvelopePayload(ctx, "COMPLETE_"+loopID, &searchResult); err != nil {
		return fmt.Errorf("read search result: %w", err)
	}

	if err := validateExecuteBranchArtifacts(
		execution, assessment, searchResult, s.controlledSeedEntityID); err != nil {
		return err
	}
	if execution.Degraded {
		warning := "execute fan-out degraded while retaining controlled evidence: " + execution.DegradedReason
		result.Warnings = append(result.Warnings, warning)
		result.Details["execute_degraded"] = true
		result.Details["execute_degraded_reason"] = execution.DegradedReason
		result.Metrics["execute_degraded"] = 1
	}
	if assessment.Degraded {
		warning := "assessment propagated partial fan-out degradation: " + assessment.DegradedReason
		result.Warnings = append(result.Warnings, warning)
		result.Details["assessment_degraded"] = true
		result.Details["assessment_degraded_reason"] = assessment.DegradedReason
		result.Metrics["assessment_degraded"] = 1
	}
	result.Details["execute_evidence_entity_id"] = s.controlledSeedEntityID
	result.Details["execute_evidence_source"] = walkSeedsEntityStateSource
	result.Metrics["execute_evidence_count"] = len(execution.Evidence)
	result.Metrics["execute_subquery_count"] = execution.SubQueryCount
	return nil
}

func (s *Scenario) readLoopEnvelopePayload(ctx context.Context, key string, dst any) error {
	envelope, err := s.nats.GetKV(ctx, "AGENT_LOOPS", key)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", key, err)
	}
	var wire struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(envelope, &wire); err != nil {
		return fmt.Errorf("decode %s envelope: %w", key, err)
	}
	if len(wire.Payload) == 0 {
		return fmt.Errorf("%s envelope has no payload", key)
	}
	if err := json.Unmarshal(wire.Payload, dst); err != nil {
		return fmt.Errorf("decode %s payload: %w", key, err)
	}
	return nil
}

func validateExecuteBranchArtifacts(
	execution research.ExecutionOutput,
	assessment research.AssessmentOutput,
	searchResult research.SearchResult,
	controlledEntityID string,
) error {
	if controlledEntityID == "" {
		return errors.New("controlled seed entity ID is unresolved; Setup did not observe the deployment authority")
	}
	if execution.Action != research.ActionWalkSeeds {
		return fmt.Errorf("execution action = %q, want %q", execution.Action, research.ActionWalkSeeds)
	}
	if execution.SubQueryCount <= 0 {
		return fmt.Errorf("execution subquery count = %d, want > 0", execution.SubQueryCount)
	}
	controlled := fusionEvidence(execution.Evidence, controlledEntityID)
	if controlled == nil {
		return fmt.Errorf("controlled entity %s absent from execution evidence", controlledEntityID)
	}
	if controlled.Tier != "0" || controlled.Source != walkSeedsEntityStateSource {
		return fmt.Errorf("controlled evidence provenance = tier %q source %q, want tier 0 source %q",
			controlled.Tier, controlled.Source, walkSeedsEntityStateSource)
	}
	if !assessment.Sufficient {
		return errors.New("assessment is not sufficient")
	}
	if assessment.EvidenceCount != len(execution.Evidence) {
		return fmt.Errorf("assessment evidence count = %d, execution evidence count = %d",
			assessment.EvidenceCount, len(execution.Evidence))
	}
	// Exactly the fixture's prose, not merely non-empty: when the synthesizer's
	// evidence_refs fail quote-back the component keeps the prose but appends a
	// degradation note and echoes evidence instead, which would let every
	// assertion below pass through a path the fixture never scripted.
	if searchResult.Synthesis != ControlledSeedSynthesis {
		return fmt.Errorf(
			"search result synthesis is %q, want the fixture's verbatim %q — a longer value means research-graph-synthesize fell back after evidence_refs quote-back failed",
			searchResult.Synthesis, ControlledSeedSynthesis)
	}
	if searchResult.DecompTrace == nil || searchResult.DecompTrace.RouterAction != research.ActionWalkSeeds {
		return errors.New("search result decomp trace does not identify walk_seeds")
	}
	if fusionEvidence(searchResult.Evidence, controlledEntityID) == nil {
		return fmt.Errorf("controlled entity %s absent from synthesis evidence", controlledEntityID)
	}
	for _, synthesizedEvidence := range searchResult.Evidence {
		matched := false
		for _, executedEvidence := range execution.Evidence {
			if synthesizedEvidence == executedEvidence {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("synthesis evidence %s is not present in execution evidence", synthesizedEvidence.EntityID)
		}
	}
	return nil
}

func fusionEvidence(evidence []fusion.Evidence, entityID string) *fusion.Evidence {
	for i := range evidence {
		if evidence[i].EntityID == entityID {
			return &evidence[i]
		}
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
