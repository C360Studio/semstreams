package rule

import (
	"bytes"
	"log/slog"
	"testing"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// spec: rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication
func TestActionPublishAgentMissingPublisherBeforeEffects(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		forEach bool
		items   string
		noWork  bool
	}{
		{name: "single"},
		{name: "fanout", forEach: true, items: `["one","two"]`},
		{name: "non-list fallback", forEach: true, items: "one"},
		{name: "empty fanout", forEach: true, items: `[]`, noWork: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mutator := &mockTripleMutator{}
			manager := newFakeManager()
			executor := NewActionExecutorComplete(nil, mutator, nil, nil, testExecutorPlatform())
			executor.SetLifecycleManager(manager)
			action := Action{
				Type: ActionTypePublishAgent, Subject: "agent.task.test", Role: "researcher",
				Model: "mock-model", Prompt: "investigate", RunScope: "new",
			}
			if tt.forEach {
				action.ForEach = "$entity.triple.coordinator.decision.subtopics"
				action.ForEachVar = "subtopic"
			}
			err := executor.Execute(t.Context(), action, &ExecutionContext{
				EntityID: semantictest.EntityID(t, "acme", "ops", "agentic-loop", "agent", "execution", fixtureCoordinatorLoopToken),
				Entity: &gtypes.EntityState{Triples: []message.Triple{
					{Predicate: "coordinator.decision.subtopics", Object: tt.items},
				}},
			})
			if tt.noWork {
				require.NoError(t, err, "an empty fanout attempts no publication")
			} else {
				assert.True(t, errs.IsInvalid(err), "missing publisher must fail as invalid: %v", err)
				assert.ErrorContains(t, err, "publish_agent requires a configured publisher")
			}
			assert.Empty(t, manager.entities, "no AgentRun may be minted without a task publisher")
			assert.Empty(t, mutator.addedTriples, "no run anchors or spawned-task triple may be written")
		})
	}
}

// spec: rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication
func TestActionPublishAgentMissingPublisherPreservesInvalidPrecedence(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		change     func(*Action)
		want       string
		classified bool
	}{
		{name: "required field", change: func(a *Action) { a.Role = "" }, want: "role is required"},
		{name: "iteration variable", change: func(a *Action) { a.ForEach = "$entity.triple.test.fixture.items" }, want: "for_each_var is required"},
		{name: "reserved subject", change: func(a *Action) { a.Subject = "user.response.test" }, want: "reject reserved publish subject", classified: true},
		{name: "lineage", change: func(a *Action) { a.RelatedLoops = map[string]string{"researcher": "$related.id"} }, want: "validate substituted related_loops", classified: true},
		{name: "budget", change: func(a *Action) { a.LoopMaxIterations = "unbounded" }, want: "validate substituted loop_max_iterations", classified: true},
		{name: "task validation", change: func(a *Action) { a.Prompt = "$entity.triple.test.fixture.empty" }, want: "validate substituted task", classified: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			action := Action{Type: ActionTypePublishAgent, Subject: "agent.task.test", Role: "researcher", Model: "mock-model", Prompt: "investigate"}
			tt.change(&action)
			err := NewActionExecutor(nil).Execute(t.Context(), action, &ExecutionContext{
				EntityID: semantictest.EntityID(t, "acme", "ops", "rule", "test", "action", "precedence"),
				Entity:   &gtypes.EntityState{Triples: []message.Triple{{Predicate: "test.fixture.empty", Object: ""}}},
			})
			require.ErrorContains(t, err, tt.want)
			assert.Equal(t, tt.classified, errs.IsInvalid(err))
			assert.NotContains(t, err.Error(), "requires a configured publisher")
		})
	}
}

// spec: rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication
func TestRunActionsMissingPublisherReportsFailure(t *testing.T) {
	t.Parallel()
	var log bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&log, nil))
	tracker := NewStateTracker(newMockKVBucket(), logger)
	evaluator := NewStatefulEvaluator(tracker, NewActionExecutor(logger), logger)
	metrics := actionFailuresTestMetrics()
	evaluator.SetMetrics(metrics)
	ruleDef := Definition{
		ID: "missing-publisher", Type: "expression",
		OnEnter: []Action{{Type: ActionTypePublishAgent, Subject: "agent.task.test", Role: "researcher", Model: "mock-model", Prompt: "investigate"}},
	}
	entityID := semantictest.EntityID(t, "acme", "ops", "rule", "test", "action", "observable")
	for _, matching := range []bool{false, true} {
		_, err := evaluator.Evaluate(t.Context(), Evaluation{Rule: ruleDef, EntityID: entityID, CurrentlyMatching: matching})
		require.NoError(t, err, "existing evaluator continues after an observed action failure")
	}
	assert.Equal(t, float64(1), testutil.ToFloat64(metrics.actionFailuresTotal.WithLabelValues(ActionTypePublishAgent)))
	assert.Contains(t, log.String(), `"msg":"Failed to execute action"`)
	assert.Contains(t, log.String(), `"action_type":"publish_agent"`)
	assert.Contains(t, log.String(), "publish_agent requires a configured publisher")
}
