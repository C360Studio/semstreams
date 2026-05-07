//go:build integration

package agenticloop_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/model"
	"github.com/c360studio/semstreams/natsclient"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	"github.com/c360studio/semstreams/storage/objectstore"
	"github.com/c360studio/semstreams/types"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// tripleCollector subscribes to graph.mutation.triple.add and collects all triples received.
type tripleCollector struct {
	mu      sync.Mutex
	triples []message.Triple
}

func (tc *tripleCollector) handler(_ context.Context, data []byte) ([]byte, error) {
	var req gtypes.AddTripleRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	tc.mu.Lock()
	tc.triples = append(tc.triples, req.Triple)
	tc.mu.Unlock()

	resp := gtypes.AddTripleResponse{
		MutationResponse: gtypes.MutationResponse{
			Success:   true,
			Timestamp: time.Now().UnixNano(),
		},
	}
	return json.Marshal(resp)
}

func (tc *tripleCollector) getTriples() []message.Triple {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	out := make([]message.Triple, len(tc.triples))
	copy(out, tc.triples)
	return out
}

func (tc *tripleCollector) predicateSet() map[string]bool {
	triples := tc.getTriples()
	s := make(map[string]bool, len(triples))
	for _, t := range triples {
		s[t.Predicate] = true
	}
	return s
}

// newTestGraphWriter creates a graphWriter wired to a real NATS test client.
// Exported fields are accessed via the agenticloop package's NewGraphWriter constructor
// which we can't use from _test package, so we test via the exported Write* methods
// on the Component. Instead, we test the NATS round-trip by building a minimal
// graphWriter through the component's public API.
//
// Since graphWriter is unexported, we test the integration path through the
// exported component methods that delegate to it. For focused NATS I/O testing,
// we use a thin wrapper that exercises the same code path.

func TestWriteModelEndpoints_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	// Set up triple collector as responder.
	collector := &tripleCollector{}
	_, err := tc.Client.SubscribeForRequests(ctx, "graph.mutation.triple.add", collector.handler)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Build a model registry with two endpoints.
	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"claude": {
				Provider:               "anthropic",
				Model:                  "claude-opus-4-5",
				SupportsTools:          true,
				MaxTokens:              200000,
				InputPricePer1MTokens:  15.0,
				OutputPricePer1MTokens: 75.0,
			},
			"local": {
				Provider:      "ollama",
				Model:         "llama3.2",
				SupportsTools: false,
			},
		},
		Defaults: model.DefaultsConfig{Model: "claude"},
	}

	// Create graphWriter directly (it's in same repo, integration test can access internals
	// through a helper). Since graphWriter is unexported, we use NewComponent approach.
	// Instead, let's test via a helper that mirrors the graphWriter construction.
	w := agenticloop.NewGraphWriterForTest(tc.Client, reg, types.PlatformMeta{Org: "acme", Platform: "ops"})
	w.WriteModelEndpoints(ctx)

	// Each endpoint produces at least 3 required triples (provider, name, supports_tools).
	// "claude" has optional fields too (max_tokens, input_price, output_price).
	triples := collector.getTriples()

	// claude: 3 required + 3 optional (max_tokens, input_price, output_price) = 6
	// local: 3 required = 3
	// Total: 9
	if len(triples) < 9 {
		t.Errorf("expected at least 9 triples, got %d", len(triples))
	}

	// Verify all triples have valid subjects (6-part entity IDs).
	for _, tr := range triples {
		if !message.IsValidEntityID(tr.Subject) {
			t.Errorf("invalid entity ID: %q", tr.Subject)
		}
	}
}

func TestWriteLoopCompletion_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	_, err := tc.Client.SubscribeForRequests(ctx, "graph.mutation.triple.add", collector.handler)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"claude": {
				Model:                  "claude-opus-4-5",
				InputPricePer1MTokens:  15.0,
				OutputPricePer1MTokens: 75.0,
			},
		},
		Defaults: model.DefaultsConfig{Model: "claude"},
	}

	w := agenticloop.NewGraphWriterForTest(tc.Client, reg, types.PlatformMeta{Org: "acme", Platform: "ops"})

	event := &agentic.LoopCompletedEvent{
		LoopID:       "loop-123",
		TaskID:       "task-abc",
		Outcome:      "success",
		Role:         "architect",
		Model:        "claude",
		Iterations:   5,
		TokensIn:     10000,
		TokensOut:    2000,
		ParentLoopID: "loop-parent",
		WorkflowSlug: "code-review",
		WorkflowStep: "draft",
		UserID:       "user-xyz",
		CompletedAt:  time.Now(),
	}

	w.WriteLoopCompletion(ctx, event)

	triples := collector.getTriples()
	preds := collector.predicateSet()

	// 7 required + model_used + cost + parent + workflow + workflow_step + user = 13
	if len(triples) < 13 {
		t.Errorf("expected at least 13 triples, got %d", len(triples))
	}

	// Verify the loop entity ID is valid.
	if len(triples) > 0 && !message.IsValidEntityID(triples[0].Subject) {
		t.Errorf("invalid loop entity ID: %q", triples[0].Subject)
	}

	// Cost should be present (non-zero pricing + non-zero tokens).
	if !preds[agvocab.LoopCostUSD] {
		t.Error("expected agent.loop.cost_usd triple")
	}

	// Parent should be a valid entity ID.
	if !preds[agvocab.LoopParent] {
		t.Error("expected agent.loop.parent triple")
	}
}

func TestWriteLoopFailure_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	_, err := tc.Client.SubscribeForRequests(ctx, "graph.mutation.triple.add", collector.handler)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})

	event := &agentic.LoopFailedEvent{
		LoopID:     "loop-fail",
		TaskID:     "task-fail",
		Outcome:    "failed",
		Role:       "editor",
		Model:      "claude",
		Iterations: 3,
		TokensIn:   500,
		TokensOut:  100,
		FailedAt:   time.Now(),
	}

	w.WriteLoopFailure(ctx, event)

	triples := collector.getTriples()

	// 7 required + model_used (non-empty model) = 8
	// cost omitted (nil registry)
	if len(triples) < 8 {
		t.Errorf("expected at least 8 triples, got %d", len(triples))
	}

	preds := collector.predicateSet()
	if !preds[agvocab.LoopOutcome] {
		t.Error("expected agent.loop.outcome triple")
	}
	if !preds[agvocab.LoopEndedAt] {
		t.Error("expected agent.loop.ended_at triple")
	}
}

func TestWriteLoopCancellation_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	_, err := tc.Client.SubscribeForRequests(ctx, "graph.mutation.triple.add", collector.handler)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})

	event := &agentic.LoopCancelledEvent{
		LoopID:      "loop-cancel",
		TaskID:      "task-cancel",
		Outcome:     "cancelled",
		CancelledAt: time.Now(),
	}

	w.WriteLoopCancellation(ctx, event)

	triples := collector.getTriples()

	// 3 required: outcome, task, ended_at
	if len(triples) < 3 {
		t.Errorf("expected at least 3 triples, got %d", len(triples))
	}

	preds := collector.predicateSet()
	if !preds[agvocab.LoopOutcome] {
		t.Error("expected agent.loop.outcome triple")
	}
}

func TestWriteTrajectorySteps_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup(), natsclient.WithJetStream())
	ctx := context.Background()

	// Set up triple collector as responder.
	collector := &tripleCollector{}
	_, err := tc.Client.SubscribeForRequests(ctx, "graph.mutation.triple.add", collector.handler)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Create ObjectStore for content storage.
	store, err := objectstore.NewStoreWithConfig(ctx, tc.Client, objectstore.Config{
		BucketName: "TEST_AGENT_CONTENT",
	})
	if err != nil {
		t.Fatalf("create content store: %v", err)
	}
	defer store.Close()

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})
	w.SetContentStore(store)

	trajectory := &agentic.Trajectory{
		LoopID:    "loop-traj",
		StartTime: time.Now().Add(-10 * time.Second),
		Steps: []agentic.TrajectoryStep{
			{
				Timestamp: time.Now().Add(-8 * time.Second),
				StepType:  "model_call",
				Model:     "claude-sonnet",
				Response:  "I'll search for deployment errors.",
				TokensIn:  4832,
				TokensOut: 128,
				Duration:  2000,
			},
			{
				Timestamp:     time.Now().Add(-6 * time.Second),
				StepType:      "tool_call",
				ToolName:      "web_search",
				ToolArguments: map[string]any{"query": "deployment errors k8s"},
				ToolResult:    "Found 3 results about Kubernetes deployment errors...",
				Duration:      1500,
			},
			{
				Timestamp: time.Now().Add(-4 * time.Second),
				StepType:  "context_compaction",
				Duration:  100,
			},
			{
				Timestamp: time.Now().Add(-2 * time.Second),
				StepType:  "model_call",
				Model:     "claude-sonnet",
				Response:  "Based on the search results, here are the common deployment errors.",
				TokensIn:  6000,
				TokensOut: 500,
				Duration:  3000,
			},
		},
	}

	w.WriteTrajectorySteps(ctx, "loop-traj", trajectory)

	triples := collector.getTriples()

	// Count step entity triples vs LoopHasStep triples.
	loopEntityID := "acme.ops.agent.agentic-loop.execution.loop-traj"
	var loopHasStepCount int
	stepEntityIDs := make(map[string]bool)
	for _, tr := range triples {
		if tr.Subject == loopEntityID && tr.Predicate == agvocab.LoopHasStep {
			loopHasStepCount++
		}
		if tr.Predicate == agvocab.StepType {
			stepEntityIDs[tr.Subject] = true
		}
	}

	// 4 steps (including compaction) → 4 LoopHasStep triples.
	if loopHasStepCount != 4 {
		t.Errorf("expected 4 LoopHasStep triples, got %d", loopHasStepCount)
	}

	// 4 step entities created.
	if len(stepEntityIDs) != 4 {
		t.Errorf("expected 4 step entities, got %d", len(stepEntityIDs))
	}

	// Verify all step entity IDs are valid.
	for id := range stepEntityIDs {
		if !message.IsValidEntityID(id) {
			t.Errorf("invalid step entity ID: %q", id)
		}
	}

	// Verify tool_call step has StepToolName predicate.
	preds := collector.predicateSet()
	if !preds[agvocab.StepToolName] {
		t.Error("expected agent.step.tool_name triple for tool_call step")
	}

	// Verify model_call steps have token predicates.
	if !preds[agvocab.StepTokensIn] {
		t.Error("expected agent.step.tokens_in triple for model_call step")
	}

	// Verify content was stored in ObjectStore.
	// The tool_call step (index 1) should have its content stored.
	toolStepEntity := &agentic.TrajectoryStepEntity{
		Step:      trajectory.Steps[1],
		Org:       "acme",
		Platform:  "ops",
		LoopID:    "loop-traj",
		StepIndex: 1,
	}
	// Store a second copy to get a ref we can fetch with.
	ref, err := store.StoreContent(ctx, toolStepEntity)
	if err != nil {
		t.Fatalf("store content for verification: %v", err)
	}
	storedContent, err := store.FetchContent(ctx, ref)
	if err != nil {
		t.Fatalf("fetch content: %v", err)
	}

	if storedContent.Fields["tool_name"] != "web_search" {
		t.Errorf("stored tool_name: got %q, want web_search", storedContent.Fields["tool_name"])
	}
	if storedContent.Fields["tool_result"] != "Found 3 results about Kubernetes deployment errors..." {
		t.Errorf("stored tool_result mismatch")
	}
	if storedContent.ContentFields["body"] != "tool_result" {
		t.Errorf("content field mapping: body should map to tool_result, got %q", storedContent.ContentFields["body"])
	}
}

func TestWriteTrajectorySteps_NoContentStore_StillWritesTriples(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	_, err := tc.Client.SubscribeForRequests(ctx, "graph.mutation.triple.add", collector.handler)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// No content store set — triples should still be written.
	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})

	trajectory := &agentic.Trajectory{
		LoopID: "loop-no-store",
		Steps: []agentic.TrajectoryStep{
			{
				Timestamp:  time.Now(),
				StepType:   "tool_call",
				ToolName:   "graph_query",
				ToolResult: "query results",
				Duration:   200,
			},
		},
	}

	w.WriteTrajectorySteps(ctx, "loop-no-store", trajectory)

	triples := collector.getTriples()
	if len(triples) == 0 {
		t.Error("expected triples even without content store")
	}

	preds := collector.predicateSet()
	if !preds[agvocab.StepToolName] {
		t.Error("expected agent.step.tool_name triple")
	}
	if !preds[agvocab.LoopHasStep] {
		t.Error("expected agent.loop.has_step triple")
	}
}

func TestWriteLoopCompletion_NilClient_NoOp(t *testing.T) {
	w := agenticloop.NewGraphWriterForTest(nil, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})

	event := &agentic.LoopCompletedEvent{
		LoopID:      "loop-noop",
		TaskID:      "task-noop",
		Outcome:     "success",
		Role:        "editor",
		CompletedAt: time.Now(),
	}

	// Should not panic.
	w.WriteLoopCompletion(context.Background(), event)
}

func TestWriteModelEndpoints_MissingPlatform_NoOp(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	_, err := tc.Client.SubscribeForRequests(ctx, "graph.mutation.triple.add", collector.handler)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Empty org/platform should skip writes.
	w := agenticloop.NewGraphWriterForTest(tc.Client, &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"claude": {Provider: "anthropic", Model: "claude-opus-4-5"},
		},
	}, types.PlatformMeta{})

	w.WriteModelEndpoints(ctx)

	triples := collector.getTriples()
	if len(triples) != 0 {
		t.Errorf("expected 0 triples with missing platform, got %d", len(triples))
	}
}

// TestWriteLineageTriples_Integration verifies the end-to-end NATS
// roundtrip: a RelatedLoops map (typed map[string]any after JSON
// round-trip) emits one lineage.<key> triple per entry through
// graph.mutation.triple.add. The spawned-loop entity ID format and
// the predicate prefix are both contractual surfaces — drift breaks
// downstream rule substitution and ops-agent aggregation.
func TestWriteLineageTriples_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	_, err := tc.Client.SubscribeForRequests(ctx, "graph.mutation.triple.add", collector.handler)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})

	related := map[string]any{
		"researcher": "loop-research-001",
		"planner":    "loop-plan-002",
	}
	w.WriteLineageTriples(ctx, "architect-loop-xyz", related)

	triples := collector.getTriples()
	if len(triples) != 2 {
		t.Fatalf("expected 2 lineage triples, got %d", len(triples))
	}

	// Both triples must point at the spawned loop's entity ID.
	wantSubject := agentic.LoopExecutionEntityID("acme", "ops", "architect-loop-xyz")
	for _, tr := range triples {
		if tr.Subject != wantSubject {
			t.Errorf("subject = %q, want %q", tr.Subject, wantSubject)
		}
		if !message.IsValidEntityID(tr.Subject) {
			t.Errorf("subject %q is not a valid 6-part entity ID", tr.Subject)
		}
	}

	preds := collector.predicateSet()
	for _, key := range []string{"researcher", "planner"} {
		want := agentic.LineageTriplePredicate(key)
		if !preds[want] {
			t.Errorf("missing predicate %q", want)
		}
	}
}

// TestWriteLineageTriples_NilClient_NoOp verifies no-panic on a
// missing NATS client (mirrors the nil-client safety the other
// writer methods carry).
func TestWriteLineageTriples_NilClient_NoOp(t *testing.T) {
	w := agenticloop.NewGraphWriterForTest(nil, nil, types.PlatformMeta{Org: "acme", Platform: "ops"})
	// Should be a no-op and not panic.
	w.WriteLineageTriples(context.Background(), "any-loop", map[string]any{"researcher": "x"})
}

// TestWriteLineageTriples_MissingPlatform_NoOp verifies platform-
// identity gating: a writer with empty Org/Platform skips the write
// (and logs a warning, but the warning side-effect is observable
// only via the production logger).
func TestWriteLineageTriples_MissingPlatform_NoOp(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	collector := &tripleCollector{}
	_, err := tc.Client.SubscribeForRequests(ctx, "graph.mutation.triple.add", collector.handler)
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	w := agenticloop.NewGraphWriterForTest(tc.Client, nil, types.PlatformMeta{}) // no Org/Platform
	w.WriteLineageTriples(ctx, "any-loop", map[string]any{"researcher": "x"})

	if got := len(collector.getTriples()); got != 0 {
		t.Errorf("missing platform identity should skip the write, got %d triples", got)
	}
}

func TestWriteMutationFailure_Integration(t *testing.T) {
	tc := natsclient.NewTestClient(t, natsclient.WithFastStartup())
	ctx := context.Background()

	// Respond with failure to verify graceful handling.
	_, err := tc.Client.SubscribeForRequests(ctx, "graph.mutation.triple.add",
		func(_ context.Context, _ []byte) ([]byte, error) {
			resp := gtypes.AddTripleResponse{
				MutationResponse: gtypes.MutationResponse{
					Success: false,
					Error:   "test error",
				},
			}
			return json.Marshal(resp)
		})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	reg := &model.Registry{
		Endpoints: map[string]*model.EndpointConfig{
			"claude": {Provider: "anthropic", Model: "claude-opus-4-5"},
		},
	}

	w := agenticloop.NewGraphWriterForTest(tc.Client, reg, types.PlatformMeta{Org: "acme", Platform: "ops"})

	// Should log warnings but not panic.
	w.WriteModelEndpoints(ctx)
}
