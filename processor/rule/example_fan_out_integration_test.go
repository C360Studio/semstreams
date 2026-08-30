package rule

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/agentic"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// #149 follow-up — the integration test that would have caught both
// #147 (broken Object reference in rule 02) and #149 (hardcoded count
// in rule 03) before shipping. Loads the actual JSONs from
// configs/rules/example-fan-out/, walks the lifecycle (spawn → N
// child completions → join), and asserts each rule's conditions
// match + actions execute correctly at every step.
//
// Pre-#149 this test didn't exist; the unit tests for individual
// helpers (resolveTripleSubject, operatorLengthEq, for_each) all
// passed against the broken pack. The pattern only failed when
// SemTeams tried to wire it — three sessions late.
//
// Runs with N=3 (the original hardcoded count from beta.83) AND
// N=5 (proves the #149 dynamic .length substitution works). Without
// .length, the N=5 run would silently break: rule 03's hardcoded
// length_eq(field, 3) would fire after 3 children, not 5.

// loadExampleFanOutRules reads + parses the three reference pack
// JSONs. Test data lives at the canonical path so a rule
// re-organization breaks the test loudly, prompting the test to
// move with the pack.
func loadExampleFanOutRules(t *testing.T) (rule01, rule02, rule03 Definition) {
	t.Helper()
	root := repoRoot(t)
	dir := filepath.Join(root, "configs", "rules", "example-fan-out")
	for _, p := range []struct {
		filename string
		target   *Definition
	}{
		{"01-fan-out-subtopics.json", &rule01},
		{"02-stamp-completion-on-parent.json", &rule02},
		{"03-synthesize-when-all-complete.json", &rule03},
	} {
		data, err := os.ReadFile(filepath.Join(dir, p.filename))
		require.NoError(t, err, "read %s", p.filename)
		require.NoError(t, json.Unmarshal(data, p.target), "parse %s", p.filename)
	}
	return
}

// repoRoot walks up from cwd until go.mod is found.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate repo root")
		}
		dir = parent
	}
}

// TestExampleFanOutPack_EndToEnd_DynamicCount drives the full
// spawn → N-completion → join lifecycle against the actual reference
// JSONs at N=5. Asserts: rule 01 spawns 5; rule 02 stamps 5 distinct
// counter triples on the coordinator with TaskID Objects; rule 03's
// condition matches when count==5 (via the #149 .length suffix); rule
// 03's action publishes one synthesizer.
//
// Pre-#149 + pre-#147 fixes, this test would have caught BOTH bugs:
//   - The reference config's broken Object reference ($entity.triple.
//     agent.task.subtopic) would cause rule 02 to write the same
//     literal string as Object on every child; graph-ingest set-dedup
//     would collapse the counter to length=1; the length_eq assertion
//     at the end would fail with count=1 instead of N.
//   - Pre-#149, rule 03's length_eq pinned to 3 wouldn't match at N=5
//     and the synthesizer assertion would fail (or worse, fire at N=3
//     before two siblings complete).
//
// Now both gaps closed; this test proves the canonical pattern works
// for any N — the only test that would have caught the
// documentation-vs-reality failure earlier.
func TestExampleFanOutPack_EndToEnd_DynamicCount(t *testing.T) {
	const (
		coordinatorEntityID = "acme.research.agentic-loop.agent.execution.coord-1"
		n                   = 5 // dynamic via #149 .length; cover N>3 specifically since the original pack pinned 3
	)

	rule01, rule02, rule03 := loadExampleFanOutRules(t)

	subtopics, coordinatorEntity := buildCoordinatorWithSubtopics(t, n)

	// --- Phase 1: rule 01 spawns N investigators in parallel ---
	taskIDs := phase1SpawnInvestigators(t, rule01, coordinatorEntityID, coordinatorEntity, subtopics, n)

	// --- Phase 2: each investigator completes; rule 02 stamps counter on parent ---
	stampedTriples := phase2StampCompletions(t, rule02, coordinatorEntityID, taskIDs)

	// --- Phase 3: rule 03 fires on coordinator now that counter ==
	// subtopics-list length. This is the part #149 enables: the
	// length_eq compares against $entity.triple.coordinator.decision.
	// subtopics.length (= N) dynamically. Pre-#149 with N=5, rule 03's
	// pinned value=3 wouldn't match — synthesizer never spawns.
	coordinatorEntity.Triples = append(coordinatorEntity.Triples, stampedTriples...)
	phase3FireJoin(t, rule03, coordinatorEntityID, coordinatorEntity)
}

// buildCoordinatorWithSubtopics constructs the canonical fan-out
// trigger state: a coordinator loop entity with next_action=fan_out
// and a JSON-encoded subtopics list of size n. Returns the subtopic
// strings for later assertions about per-iteration substitution.
func buildCoordinatorWithSubtopics(t *testing.T, n int) ([]string, *gtypes.EntityState) {
	t.Helper()
	subtopics := make([]string, n)
	for i := range subtopics {
		subtopics[i] = fmt.Sprintf("subtopic-%d", i)
	}
	subtopicsJSON, err := json.Marshal(subtopics)
	require.NoError(t, err)
	entity := &gtypes.EntityState{
		Triples: []message.Triple{
			{Predicate: "agent.loop.role", Object: "coordinator"},
			{Predicate: "coordinator.decision.next-action", Object: "fan_out"},
			{Predicate: "coordinator.decision.subtopics", Object: string(subtopicsJSON)},
		},
	}
	return subtopics, entity
}

// phase1SpawnInvestigators verifies rule 01's conditions match on
// the coordinator, executes its actions, and returns the TaskIDs of
// the N spawned investigators (extracted from the published
// TaskMessages). Per-iteration $subtopic substitution into each
// prompt is asserted along the way.
//
// Drives the production wire ExpressionRule.EvaluateEntityState —
// NOT the helper-direct path — so the test locks the actual
// production call chain. A future refactor that removes the
// substitution wire from EvaluateEntityState fails this test
// rather than silently shipping broken (reviewer-found gap on
// PR #150's first cut: helper-direct testing reproduced the exact
// "tests piece, not pattern" class the structural fixes claim to
// close).
func phase1SpawnInvestigators(
	t *testing.T,
	rule01 Definition,
	coordinatorEntityID string,
	coordinator *gtypes.EntityState,
	subtopics []string,
	n int,
) []string {
	t.Helper()
	// EntityState.ID is what EvaluateEntityState uses for logging
	// and the substitution helper builds its EC from; set it
	// explicitly so the wire mirrors what the production
	// entity-watcher path would have populated.
	coordinator.ID = coordinatorEntityID

	rule, err := NewExpressionRule(testPlatform, "direct-expression-test", rule01)
	require.NoError(t, err)
	require.True(t, rule.EvaluateEntityState(context.Background(), coordinator),
		"rule 01 EvaluateEntityState must match a coordinator with next_action=fan_out (drives the production wire including SubstituteConditionValues)")

	publisher := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, publisher, testExecutorPlatform())
	ec := &ExecutionContext{EntityID: coordinatorEntityID, Entity: coordinator}
	for _, action := range rule01.OnEnter {
		require.NoError(t, executor.Execute(context.Background(), action, ec))
	}
	require.Len(t, publisher.published, n, "rule 01 must spawn one investigator per subtopic via for_each")

	taskIDs := make([]string, 0, n)
	for i, msg := range publisher.published {
		task := extractTask(t, msg.data)
		taskIDs = append(taskIDs, task.TaskID)
		assert.Contains(t, task.Prompt, subtopics[i],
			"investigator %d's prompt must carry its assigned subtopic via $subtopic substitution", i)
	}
	return taskIDs
}

// phase2StampCompletions simulates each investigator completing
// successfully and fires rule 02 once per child. Returns the
// counter triples that rule 02 stamped onto the coordinator entity
// (verified to be distinct, on the correct subject, with the right
// predicate) so phase 3 can graft them onto the coordinator's
// triple set before running the join condition.
func phase2StampCompletions(
	t *testing.T,
	rule02 Definition,
	coordinatorEntityID string,
	taskIDs []string,
) []message.Triple {
	t.Helper()
	mutator := &mockTripleMutator{}
	executor := NewActionExecutorFull(nil, mutator, nil, testExecutorPlatform())
	rule, err := NewExpressionRule(testPlatform, "direct-expression-test", rule02)
	require.NoError(t, err)

	for i, taskID := range taskIDs {
		invID := fmt.Sprintf("acme.research.agentic-loop.agent.execution.inv-%d", i)
		investigator := &gtypes.EntityState{
			ID: invID,
			Triples: []message.Triple{
				{Predicate: "agent.loop.role", Object: "investigator"},
				{Predicate: "agent.loop.outcome", Object: "success"},
				{Predicate: "agent.loop.parent", Object: coordinatorEntityID},
				{Predicate: "agent.loop.task", Object: taskID},
			},
		}
		ec := &ExecutionContext{EntityID: invID, Entity: investigator}

		require.True(t, rule.EvaluateEntityState(context.Background(), investigator),
			"rule 02 EvaluateEntityState must match a successful investigator (child %d)", i)

		for _, action := range rule02.OnEnter {
			require.NoError(t, executor.Execute(context.Background(), action, ec),
				"rule 02 stamp for child %d", i)
		}
	}

	n := len(taskIDs)
	require.Len(t, mutator.addedTriples, n, "exactly N counter triples should be stamped (one per child)")
	gotObjects := make(map[string]struct{}, n)
	for _, tr := range mutator.addedTriples {
		assert.Equal(t, coordinatorEntityID, tr.Subject,
			"every counter triple must land on the COORDINATOR entity (proves the #147 subject override is wired)")
		assert.Equal(t, "gather.child.completed", tr.Predicate)
		gotObjects[fmt.Sprintf("%v", tr.Object)] = struct{}{}
	}
	assert.Len(t, gotObjects, n,
		"counter Objects must be distinct (TaskIDs) — proves the dedup-by-TaskID property. If this collapses to len=1, the Object resolution is broken (#147 pre-fix shape).")
	return mutator.addedTriples
}

// phase3FireJoin runs rule 03's condition against the coordinator
// entity (now carrying the accumulated counter triples) and asserts
// the synthesizer dispatches exactly once. Mirrors production's
// SubstituteConditionValues path so the #149 .length substitution
// on the rule's value field resolves to N before the operator runs.
func phase3FireJoin(
	t *testing.T,
	rule03 Definition,
	coordinatorEntityID string,
	coordinator *gtypes.EntityState,
) {
	t.Helper()
	coordinator.ID = coordinatorEntityID
	rule, err := NewExpressionRule(testPlatform, "direct-expression-test", rule03)
	require.NoError(t, err)
	require.True(t, rule.EvaluateEntityState(context.Background(), coordinator),
		"rule 03 EvaluateEntityState must match when len(gather.child.completed) == .length(coordinator.decision.subtopics) — drives the production wire including SubstituteConditionValues + #149 .length substitution + #147 array operator wiring")

	publisher := &mockPublisher{}
	executor := NewActionExecutorFull(nil, nil, publisher, testExecutorPlatform())
	ec := &ExecutionContext{EntityID: coordinatorEntityID, Entity: coordinator}
	for _, action := range rule03.OnEnter {
		require.NoError(t, executor.Execute(context.Background(), action, ec))
	}
	require.Len(t, publisher.published, 1,
		"rule 03 must dispatch exactly one synthesizer when all children are accounted for")

	// ADR-048 PR 4 — assert the new `.triples` prompt-substitution
	// works end-to-end through the production wire. Rule 03's
	// prompt template now includes
	// `$entity.triple.gather.child.completed.triples`; the
	// substitution should inline a JSON array of N strings (one
	// per child TaskID accumulated on the coordinator entity) into
	// the natural-language prompt.
	//
	// This closes the reviewer's soft gap on `.triples` × for_each
	// round-trip end-to-end coverage. The for_each fan-out happens
	// in rule 01 (test phase 1), the per-child counter stamping in
	// rule 02 (phase 2), and the .triples enumeration consumption
	// in rule 03 (here). All three phases drive the production wire
	// per [[feedback_integration_tests_must_drive_production_wire]].
	//
	// NOTE: switched from properties-substitution to prompt-
	// substitution because publish_agent does NOT today thread
	// action.Properties into TaskMessage.Metadata (the publish
	// action does; publish_agent doesn't). Filing as a follow-up
	// to land in a separate PR. Prompt-substitution works today
	// and ships the .triples primitive that semteams needs;
	// properties-threading-on-publish_agent is the higher-fidelity
	// pattern for later.
	task := extractTask(t, publisher.published[0].data)
	expected := make(map[string]struct{}, len(coordinator.Triples))
	for _, tr := range coordinator.Triples {
		if tr.Predicate == "gather.child.completed" {
			expected[fmt.Sprintf("%v", tr.Object)] = struct{}{}
		}
	}
	// Parse the JSON array out of the prompt. The substitution
	// produces `["taskID1","taskID2",...]` inlined into the prompt
	// text — extract the JSON array and assert every stamped
	// TaskID is present, in any order.
	openBracket := strings.Index(task.Prompt, "[")
	closeBracket := strings.Index(task.Prompt[openBracket:], "]")
	require.Greater(t, openBracket, 0,
		"prompt should contain a JSON array from .triples substitution; got %q", task.Prompt)
	require.Greater(t, closeBracket, 0,
		"prompt should contain a closing bracket for the JSON array; got %q", task.Prompt)
	jsonArr := task.Prompt[openBracket : openBracket+closeBracket+1]
	var parsed []string
	require.NoError(t, json.Unmarshal([]byte(jsonArr), &parsed),
		"prompt JSON array must be valid JSON the consumer can parse per the canonical persona-prose template")
	require.Len(t, parsed, len(expected),
		"JSON array length should match the count of gather.child.completed triples on the coordinator")
	for _, got := range parsed {
		_, found := expected[got]
		require.True(t, found,
			"prompt JSON element %q should match a stamped TaskID; expected one of %v",
			got, expected)
	}
}

// TestExampleFanOutPack_EndToEnd_PartialCompletionDoesNotFireJoin
// is the negative case: at N-1 stamps, rule 03's condition must NOT
// match. Together with the positive test this proves the join is
// gated correctly — neither premature-fire (premature synthesis)
// nor permanent-wedge (synthesis never fires).
func TestExampleFanOutPack_EndToEnd_PartialCompletionDoesNotFireJoin(t *testing.T) {
	const n = 4

	_, _, rule03 := loadExampleFanOutRules(t)

	subtopics := make([]string, n)
	for i := range subtopics {
		subtopics[i] = fmt.Sprintf("subtopic-%d", i)
	}
	subtopicsJSON, err := json.Marshal(subtopics)
	require.NoError(t, err)

	// Coordinator with only N-1 counter stamps (one child still
	// pending).
	triples := []message.Triple{
		{Predicate: "agent.loop.role", Object: "coordinator"},
		{Predicate: "coordinator.decision.subtopics", Object: string(subtopicsJSON)},
	}
	for i := 0; i < n-1; i++ {
		triples = append(triples, message.Triple{
			Predicate: "gather.child.completed",
			Object:    fmt.Sprintf("task-%d", i),
		})
	}
	coordinator := &gtypes.EntityState{Triples: triples}

	coordinator.ID = "acme.research.agentic-loop.agent.execution.coord-x"
	rule, err := NewExpressionRule(testPlatform, "direct-expression-test", rule03)
	require.NoError(t, err)
	assert.False(t, rule.EvaluateEntityState(context.Background(), coordinator),
		"rule 03 EvaluateEntityState must NOT match at N-1 completions — premature synthesis would lose the slowest child's findings. Drives the production wire so a future refactor that removes the substitution helper from EvaluateEntityState fails this test rather than silently shipping broken.")
}

// extractTask decodes a published agent.task.* envelope into the
// canonical agentic.TaskMessage. Used by the integration test to
// assert TaskID + per-iteration Prompt substitution.
func extractTask(t *testing.T, data []byte) *agentic.TaskMessage {
	t.Helper()
	baseMsg, err := newActionsTestDecoder(t).Decode(data)
	require.NoError(t, err)
	task, ok := baseMsg.Payload().(*agentic.TaskMessage)
	require.True(t, ok, "payload should decode as *agentic.TaskMessage; got %T", baseMsg.Payload())
	return task
}
