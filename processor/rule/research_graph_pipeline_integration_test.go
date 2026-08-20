package rule

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic/research"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/processor/rule/expression"
)

// ADR-045 Phase 1 PR 6 smoke test. Loads the six R0-R6 rules from
// configs/rules/research-graph/ and drives each through
// NewExpressionRule + ExpressionRule.EvaluateEntityState (the
// production rule-engine wire — per
// [[feedback_integration_tests_must_drive_production_wire]] the
// helper-direct path silently slips when the wire breaks). Asserts:
//
//   - R0 fires when research_pipeline loop has research.requested=true
//     and publishes to component.nl_classify.<loopID>
//   - R1 fires on research.classify.complete and dispatches route_search
//   - R2 branches on research.route.action across all four routing
//     actions (synthesize_directly / retighten / walk_seeds / decompose)
//     — locks the action-level when-clause matrix that R2's value
//     proposition rests on
//   - R3 fires on research.execute.complete and dispatches assess
//   - R4 branches on research.assess.sufficient (sufficient → synthesize,
//     insufficient → refine via execute) with the refine branch's
//     remove_triple actions for execute+assess markers
//   - R6 fires on research.search_result.complete and dispatches
//     publish_agent back to the parent role with the SearchResult ref
//     wired into the prompt via $entity.triple.* substitution
//
// What this test does NOT cover (out of scope for PR 6 smoke):
//
//   - Multi-iteration retighten/refine (R2/R4 re-fire after their
//     remove_triple actions clear the stage markers). The retighten
//     branch's MaxIterations=2 and refine branch's MaxIterations=5 are
//     declared in the JSON; their interaction with the per-action
//     iteration counter is unit-tested in actions_test.go. The
//     smoke locks the single-iteration wire only.
//   - Per-component handler logic (covered by per-component test
//     suites under processor/research-graph-*).
//   - NATS / KV / graph-ingest wiring (covered by component-level
//     integration tests + e2e:agentic).
//
// loadResearchGraphRules reads the six PR 6 rule definitions from
// their canonical location. A re-organization of the pack breaks this
// test loudly, prompting the test to move with the pack.
func loadResearchGraphRules(t *testing.T) (r0, r1, r2, r3, r4, r6 Definition) {
	t.Helper()
	root := repoRoot(t)
	dir := filepath.Join(root, "configs", "rules", "research-graph")
	files := []struct {
		filename string
		target   *Definition
	}{
		{"00-kickoff-classify.json", &r0},
		{"01-classify-routes.json", &r1},
		{"02-route-decision-dispatch.json", &r2},
		{"03-execute-assesses.json", &r3},
		{"04-assess-dispatch.json", &r4},
		{"05-continuation.json", &r6},
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(dir, f.filename))
		require.NoError(t, err, "read %s", f.filename)
		require.NoError(t, json.Unmarshal(data, f.target), "parse %s", f.filename)
	}
	return
}

const (
	testLoopID       = "rg_smoketest"
	testTopic        = "drone hover anomalies"
	testParentLoopID = "loop_parent01"
	testParentRole   = "general"
)

// kickoffEntity returns the EntityState the research_graph tool stamps
// at chain entry. Mirrors what research.BuildKickoffTriples produces,
// stripped down to the fields the rules read.
func kickoffEntity(entityID string) *gtypes.EntityState {
	return &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Predicate: "agent.loop.role", Object: research.PipelineRole},
			{Predicate: "research.request.received", Object: "true"},
			{Predicate: "research.request.topic", Object: testTopic},
			{Predicate: "research.loop.id", Object: testLoopID},
			{Predicate: "research.parent.loop", Object: testParentLoopID},
			{Predicate: "research.parent.role", Object: testParentRole},
		},
	}
}

// withTriple returns a clone of base with the given (predicate, object)
// triple appended. Keeps each stage's setup compact while keeping the
// upstream stamps intact so action-level $entity.triple.* substitution
// (e.g. R0's subject literal `component.nl_classify.$entity.triple.research.loop_id`)
// resolves the loopID consistently.
func withTriple(base *gtypes.EntityState, triple message.Triple) *gtypes.EntityState {
	clone := *base
	clone.Triples = append([]message.Triple(nil), base.Triples...)
	clone.Triples = append(clone.Triples, triple)
	return &clone
}

// runOnEnter evaluates the rule against the entity, asserts the
// condition matched, then executes every OnEnter action through the
// production wire (NewActionExecutorFull), filtering by each action's
// When clause via the same expression.Evaluator the stateful evaluator
// uses in production (see stateful_evaluator.go runActions). Returns
// the recorded publisher + mutator outputs for assertions.
//
// Replicating the When-filter inline (instead of constructing a full
// StatefulEvaluator) keeps the smoke test focused on the rule pack's
// dispatch matrix without dragging in state-tracker setup. The
// per-action MaxIterations cap is NOT exercised here — that's the
// rule engine's stateful concern, unit-tested in actions_test.go and
// stateful_evaluator's own integration suite.
func runOnEnter(t *testing.T, def Definition, entity *gtypes.EntityState) (*mockPublisher, *mockTripleMutator) {
	t.Helper()
	rule, err := NewExpressionRule("direct-expression-test", def)
	require.NoError(t, err)
	require.True(t, rule.EvaluateEntityState(context.Background(), entity),
		"rule %s EvaluateEntityState must match (production wire)", def.ID)

	pub := &mockPublisher{}
	mut := &mockTripleMutator{}
	exec := NewActionExecutorFull(nil, mut, pub)
	ec := &ExecutionContext{EntityID: entity.ID, Entity: entity}
	evaluator := expression.NewExpressionEvaluator()
	for _, action := range def.OnEnter {
		if len(action.When) > 0 {
			expr := expression.LogicalExpression{
				Conditions: SubstituteConditionValues(context.Background(), action.When, ec),
				Logic:      expression.LogicAnd,
			}
			match, whenErr := evaluator.EvaluateWithStateAndMessage(entity, nil, nil, expr)
			require.NoError(t, whenErr, "rule %s action %s When evaluation", def.ID, action.ID)
			if !match {
				continue
			}
		}
		require.NoError(t, exec.Execute(context.Background(), action, ec),
			"rule %s action %s execute", def.ID, action.ID)
	}
	return pub, mut
}

// TestResearchGraphPipeline_R0_KickoffDispatchesClassify locks R0:
// research-pipeline loop with research.requested=true fires on_enter
// and publishes to component.nl_classify.<loopID> via the
// $entity.triple.research.loop_id substitution.
func TestResearchGraphPipeline_R0_KickoffDispatchesClassify(t *testing.T) {
	r0, _, _, _, _, _ := loadResearchGraphRules(t)
	entityID := semantictest.EntityID(t, "acme", "ops", "agent", "agentic-loop", "execution", testLoopID)
	pub, _ := runOnEnter(t, r0, kickoffEntity(entityID))

	require.Len(t, pub.published, 1, "R0 must publish exactly one classify dispatch")
	assert.Equal(t, "component.nl_classify."+testLoopID, pub.published[0].subject,
		"R0 subject must substitute $entity.triple.test.research.loop-id into the nl_classify NATS surface")
}

// TestResearchGraphPipeline_R0_DoesNotFireOnNonResearchLoops locks
// the loop.role scoping that keeps this rule pack from firing on
// regular coordinator / investigator agent loops.
func TestResearchGraphPipeline_R0_DoesNotFireOnNonResearchLoops(t *testing.T) {
	r0, _, _, _, _, _ := loadResearchGraphRules(t)
	entityID := semantictest.EntityID(t, "acme", "ops", "agent", "agentic-loop", "execution", "coord-1")
	entity := &gtypes.EntityState{
		ID: entityID,
		Triples: []message.Triple{
			{Predicate: semantictest.Predicate(t, "agent", "loop", "role"), Object: "coordinator"},
			{Predicate: semantictest.Predicate(t, "research", "request", "received"), Object: "true"},
		},
	}
	rule, err := NewExpressionRule("direct-expression-test", r0)
	require.NoError(t, err)
	assert.False(t, rule.EvaluateEntityState(context.Background(), entity),
		"R0 must not fire on coordinator/investigator loops — loop.role scoping is the safety net")
}

// TestResearchGraphPipeline_R1_ClassifyCompleteDispatchesRoute locks
// R1: research.classify.complete stamped on the loop entity fires on_enter
// and dispatches route_search.
func TestResearchGraphPipeline_R1_ClassifyCompleteDispatchesRoute(t *testing.T) {
	_, r1, _, _, _, _ := loadResearchGraphRules(t)
	entityID := semantictest.EntityID(t, "acme", "ops", "agent", "agentic-loop", "execution", testLoopID)
	entity := withTriple(kickoffEntity(entityID), message.Triple{Predicate: semantictest.Predicate(t, "research", "classify", "complete"), Object: "2026-06-02T18:30:45Z"})
	pub, _ := runOnEnter(t, r1, entity)

	require.Len(t, pub.published, 1)
	assert.Equal(t, "component.route_search."+testLoopID, pub.published[0].subject)
}

// TestResearchGraphPipeline_R2_RouteAction_AllFourBranches locks R2's
// action-level when-clause matrix — the load-bearing dispatch core of
// the chain. Each action gets its own entity setup; R2 must publish
// exactly the right next-stage subject (and for retighten, must also
// remove the two stage-marker triples that let R1 re-fire on the next
// classify stamp).
func TestResearchGraphPipeline_R2_RouteAction_AllFourBranches(t *testing.T) {
	_, _, r2, _, _, _ := loadResearchGraphRules(t)
	entityID := semantictest.EntityID(t, "acme", "ops", "agent", "agentic-loop", "execution", testLoopID)

	for _, tc := range []struct {
		name                  string
		action                string
		wantSubject           string
		wantRemovedPredicates []string
	}{
		{
			name:        "synthesize_directly",
			action:      research.ActionSynthesizeDirectly,
			wantSubject: "component.synthesize_answer." + testLoopID,
		},
		{
			name:        "walk_seeds",
			action:      research.ActionWalkSeeds,
			wantSubject: "component.execute_subqueries." + testLoopID,
		},
		{
			name:        "decompose",
			action:      research.ActionDecompose,
			wantSubject: "component.execute_subqueries." + testLoopID,
		},
		{
			name:        "retighten",
			action:      research.ActionRetighten,
			wantSubject: "component.nl_classify." + testLoopID,
			wantRemovedPredicates: []string{
				semantictest.Predicate(t, "research", "classify", "complete"),
				semantictest.Predicate(t, "research", "route", "complete"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entity := withTriple(kickoffEntity(entityID), message.Triple{Predicate: semantictest.Predicate(t, "research", "classify", "complete"), Object: "2026-06-02T18:30:00Z"})
			entity = withTriple(entity, message.Triple{Predicate: semantictest.Predicate(t, "research", "route", "complete"), Object: "2026-06-02T18:30:45Z"})
			entity = withTriple(entity, message.Triple{Predicate: semantictest.Predicate(t, "research", "route", "action"), Object: tc.action})

			pub, mut := runOnEnter(t, r2, entity)
			require.Len(t, pub.published, 1, "R2 must publish exactly one next-stage dispatch for action=%q", tc.action)
			assert.Equal(t, tc.wantSubject, pub.published[0].subject)

			if len(tc.wantRemovedPredicates) > 0 {
				require.Len(t, mut.removedTriples, len(tc.wantRemovedPredicates),
					"R2 retighten must remove exactly %d stage markers so R1 can re-fire on the next classify stamp", len(tc.wantRemovedPredicates))
				got := map[string]bool{}
				for _, r := range mut.removedTriples {
					got[r.predicate] = true
				}
				for _, want := range tc.wantRemovedPredicates {
					assert.True(t, got[want], "expected R2 retighten to remove %s", want)
				}
			} else {
				assert.Empty(t, mut.removedTriples, "non-retighten R2 branches must not touch stage markers")
			}
		})
	}
}

// TestResearchGraphPipeline_R3_ExecuteCompleteDispatchesAssess locks
// R3: research.execute.complete dispatches assess_sufficiency.
func TestResearchGraphPipeline_R3_ExecuteCompleteDispatchesAssess(t *testing.T) {
	_, _, _, r3, _, _ := loadResearchGraphRules(t)
	entityID := semantictest.EntityID(t, "acme", "ops", "agent", "agentic-loop", "execution", testLoopID)
	entity := withTriple(kickoffEntity(entityID), message.Triple{Predicate: semantictest.Predicate(t, "research", "execute", "complete"), Object: "2026-06-02T18:31:00Z"})
	pub, _ := runOnEnter(t, r3, entity)

	require.Len(t, pub.published, 1)
	assert.Equal(t, "component.assess_sufficiency."+testLoopID, pub.published[0].subject)
}

// TestResearchGraphPipeline_R4_AssessDecision_BothBranches locks R4's
// sufficient/refine matrix. Sufficient → synthesize_answer (terminal);
// insufficient → refine via execute_subqueries with execute+assess
// stage-marker clears so R3 fires again on the next execute stamp.
func TestResearchGraphPipeline_R4_AssessDecision_BothBranches(t *testing.T) {
	_, _, _, _, r4, _ := loadResearchGraphRules(t)
	entityID := semantictest.EntityID(t, "acme", "ops", "agent", "agentic-loop", "execution", testLoopID)

	t.Run("sufficient_true_dispatches_synthesize", func(t *testing.T) {
		entity := withTriple(kickoffEntity(entityID), message.Triple{Predicate: semantictest.Predicate(t, "research", "execute", "complete"), Object: "2026-06-02T18:31:00Z"})
		entity = withTriple(entity, message.Triple{Predicate: semantictest.Predicate(t, "research", "assess", "complete"), Object: "2026-06-02T18:31:30Z"})
		entity = withTriple(entity, message.Triple{Predicate: semantictest.Predicate(t, "research", "assess", "sufficient"), Object: "true"})

		pub, mut := runOnEnter(t, r4, entity)
		require.Len(t, pub.published, 1)
		assert.Equal(t, "component.synthesize_answer."+testLoopID, pub.published[0].subject)
		assert.Empty(t, mut.removedTriples, "sufficient=true branch must not touch stage markers — chain advances forward")
	})

	t.Run("sufficient_false_refines_via_execute", func(t *testing.T) {
		entity := withTriple(kickoffEntity(entityID), message.Triple{Predicate: semantictest.Predicate(t, "research", "execute", "complete"), Object: "2026-06-02T18:31:00Z"})
		entity = withTriple(entity, message.Triple{Predicate: semantictest.Predicate(t, "research", "assess", "complete"), Object: "2026-06-02T18:31:30Z"})
		entity = withTriple(entity, message.Triple{Predicate: semantictest.Predicate(t, "research", "assess", "sufficient"), Object: "false"})

		pub, mut := runOnEnter(t, r4, entity)
		require.Len(t, pub.published, 1)
		assert.Equal(t, "component.execute_subqueries."+testLoopID, pub.published[0].subject,
			"refine branch dispatches a fresh execute_subqueries round")

		require.Len(t, mut.removedTriples, 2,
			"refine branch must clear execute.complete + assess.complete so R3 re-fires on the next execute stamp")
		removed := map[string]bool{}
		for _, r := range mut.removedTriples {
			removed[r.predicate] = true
		}
		assert.True(t, removed[research.PredicateResearchExecuteComplete])
		assert.True(t, removed[research.PredicateResearchAssessComplete])
	})

	// Reviewer C1 follow-up — pin the refine cap-exhaust fallback.
	// The default smoke runs synthesize-when-sufficient through
	// runOnEnter without state, where $state.iteration evaluates to 0.
	// To exercise the cap-exhaust path we need to drive
	// EvaluateWithStateAndMessage directly with a stateFields map
	// carrying $state.iteration >= 6 (the 6th R4 fire after 5 refine
	// rounds, where the refine actions are about to hit MaxIterations=5
	// and skip — leaving the cap-exhaust synthesize as the only action
	// that should fire). Without this fallback, the chain stalls
	// silently after 5 insufficient refines.
	t.Run("sufficient_false_at_cap_exhaust_falls_to_synthesize", func(t *testing.T) {
		entity := withTriple(kickoffEntity(entityID), message.Triple{Predicate: semantictest.Predicate(t, "research", "execute", "complete"), Object: "2026-06-02T18:35:00Z"})
		entity = withTriple(entity, message.Triple{Predicate: semantictest.Predicate(t, "research", "assess", "complete"), Object: "2026-06-02T18:35:30Z"})
		entity = withTriple(entity, message.Triple{Predicate: semantictest.Predicate(t, "research", "assess", "sufficient"), Object: "false"})

		rule, err := NewExpressionRule("direct-expression-test", r4)
		require.NoError(t, err)
		require.True(t, rule.EvaluateEntityState(context.Background(), entity))

		pub := &mockPublisher{}
		mut := &mockTripleMutator{}
		exec := NewActionExecutorFull(nil, mut, pub)
		ec := &ExecutionContext{EntityID: entity.ID, Entity: entity}
		evaluator := expression.NewExpressionEvaluator()
		// Replicate the stateful_evaluator's iteration counter + per-
		// action firing cap at the cap-exhaust boundary (6th R4 fire).
		// In production the refine actions hit MaxIterations=5 on the
		// 6th R4 evaluation and stateful_evaluator skips them — so
		// only the cap-exhaust fallback synthesize remains. Simulate
		// the same gate inline: actionIterations[id] >= action.MaxIterations
		// = skip.
		stateFields := expression.StateFields{"$state.iteration": 6}
		actionIterations := map[string]int{
			"assess_refine_clear_execute": 5,
			"assess_refine_clear_assess":  5,
			"assess_refine_redispatch":    5,
		}

		dispatched := []string{}
		for _, action := range r4.OnEnter {
			if action.MaxIterations != nil && *action.MaxIterations > 0 && actionIterations[action.ID] >= *action.MaxIterations {
				continue
			}
			if len(action.When) > 0 {
				expr := expression.LogicalExpression{
					Conditions: SubstituteConditionValues(context.Background(), action.When, ec),
					Logic:      expression.LogicAnd,
				}
				match, whenErr := evaluator.EvaluateWithStateAndMessage(entity, stateFields, nil, expr)
				require.NoError(t, whenErr)
				if !match {
					continue
				}
			}
			require.NoError(t, exec.Execute(context.Background(), action, ec))
			dispatched = append(dispatched, action.ID)
		}

		require.Equal(t, []string{"assess_synthesize_when_refine_capped"}, dispatched,
			"at iteration=6 with sufficient=false + refine actions cap-exhausted, ONLY the cap-exhaust fallback must fire — chain advances to terminal synthesis")
		require.Len(t, pub.published, 1)
		assert.Equal(t, "component.synthesize_answer."+testLoopID, pub.published[0].subject)
		assert.Empty(t, mut.removedTriples,
			"cap-exhaust fallback must NOT clear stage markers — chain advances forward, not loops")
	})
}

// TestResearchGraphPipeline_R6_ContinuationDispatchesParentTask locks
// R6: search_result.complete on the loop entity fires the continuation
// publish_agent back to the parent role with the SearchResult ref +
// topic substituted into the prompt. The parent's next iteration uses
// read_loop_result(loop_id=<rg_…>) to fetch the result.
func TestResearchGraphPipeline_R6_ContinuationDispatchesParentTask(t *testing.T) {
	_, _, _, _, _, r6 := loadResearchGraphRules(t)
	entityID := semantictest.EntityID(t, "acme", "ops", "agent", "agentic-loop", "execution", testLoopID)
	entity := withTriple(kickoffEntity(entityID), message.Triple{Predicate: semantictest.Predicate(t, "research", "search-result", "complete"), Object: "2026-06-02T18:32:00Z"})
	entity = withTriple(entity, message.Triple{Predicate: semantictest.Predicate(t, "research", "search-result", "ref"), Object: "search_result.complete." + testLoopID})

	pub, _ := runOnEnter(t, r6, entity)
	require.Len(t, pub.published, 1, "R6 must dispatch exactly one continuation task to the parent")

	msg := pub.published[0]
	// The subject for publish_agent is the configured agent.task subject
	// rather than the parent role directly — the Role is threaded into
	// the TaskMessage payload (asserted below via extractTask).
	assert.Equal(t, "agent.task.research_continuation", msg.subject)

	task := extractTask(t, msg.data)
	assert.Equal(t, testParentRole, task.Role, "R6 must thread research.parent_role onto the TaskMessage so the parent's persona/role wiring fires")
	assert.Contains(t, task.Prompt, testTopic, "continuation prompt must carry the original topic for parent context")
	assert.Contains(t, task.Prompt, testLoopID, "continuation prompt must carry the research loop_id so the parent can call read_loop_result")
}
