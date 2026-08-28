package agenticloop

import (
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// WriteSpawnIdentity births the loop-execution entity through canonical create
// using LoopExecutionEntity.Triples() as the
// origin triple set. These tests exercise the pure predicate-set contract on
// LoopExecutionEntity directly — the same contract that was previously tested
// through buildSpawnIdentityTriples before it was removed (the logic moved to
// agentic.LoopExecutionEntity). The wire-level birth behaviour (create
// request/response, idempotency on already-exists) is covered by the integration
// tests in graph_writer_integration_test.go.
//
// Helper: buildSpawnTriples delegates to LoopExecutionEntity.Triples() so
// the tests read identically to the old buildSpawnIdentityTriples-based
// tests with minimal churn.
func buildSpawnTriples(loopEntityID string, task *agentic.TaskMessage, org, platform string) []message.Triple {
	e := &agentic.LoopExecutionEntity{
		Org:      org,
		Platform: platform,
		// extract the loopID from the 6-part entity ID — tests pass it
		// directly so we reconstruct the struct fields from the caller args.
		// LoopExecutionEntityID format: org.platform.agentic-loop.agent.execution.loopID
		// The LoopID field is not strictly needed since EntityID() is
		// computed from it, but it must be set to produce a valid EntityID.
		LoopID: loopIDFromEntityID(loopEntityID, org, platform),
		Task:   task,
	}
	return e.Triples()
}

// loopIDFromEntityID extracts the loopID segment from a pre-built entity ID.
// Used only by the test helper above.
func loopIDFromEntityID(entityID, org, platform string) string {
	prefix := org + "." + platform + ".agentic-loop.agent.execution."
	if len(entityID) > len(prefix) {
		return entityID[len(prefix):]
	}
	return entityID
}

func TestBuildSpawnIdentityTriples_RequiredFields(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.spawn-001"
	task := &agentic.TaskMessage{
		TaskID: "task-spawn-001",
		Role:   "researcher",
		Prompt: "Investigate the deployment failure",
	}

	triples := buildSpawnTriples(loopEntityID, task, "acme", "ops")

	for _, tr := range triples {
		if tr.Subject != loopEntityID {
			t.Errorf("unexpected subject: got %q, want %q", tr.Subject, loopEntityID)
		}
		if tr.Confidence != 1.0 {
			t.Errorf("unexpected confidence: got %v, want 1.0", tr.Confidence)
		}
		if tr.Source != "agentic-loop" {
			t.Errorf("unexpected source: got %q, want %q", tr.Source, "agentic-loop")
		}
	}

	facts := predicateSet(triples)
	required := []string{
		agvocab.LoopRole,
		agvocab.LoopTask,
		agvocab.LoopDescription,
	}
	for _, pred := range required {
		if !facts[pred] {
			t.Errorf("missing required predicate: %s", pred)
		}
	}

	if got := objectFor(triples, agvocab.LoopRole); got != "researcher" {
		t.Errorf("%s: got %v, want researcher", agvocab.LoopRole, got)
	}
	if got := objectFor(triples, agvocab.LoopTask); got != "task-spawn-001" {
		t.Errorf("%s: got %v, want task-spawn-001", agvocab.LoopTask, got)
	}
}

// Parent must be stamped as a valid 6-part entity ID (the same shape
// the completion-path previously produced), so semteams ADR-038
// ancestry-walk and rule-engine $entity.triple.agent.loop.parent
// substitutions continue to resolve to a navigable entity reference.
func TestBuildSpawnIdentityTriples_StampsParent(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.child-001"
	task := &agentic.TaskMessage{
		TaskID:       "task-child-001",
		Role:         "gather",
		ParentLoopID: "parent-loop-uuid",
	}

	triples := buildSpawnTriples(loopEntityID, task, "acme", "ops")
	facts := predicateSet(triples)

	if !facts[agvocab.LoopParent] {
		t.Fatalf("expected agent.loop.parent triple; got predicates: %v", facts)
	}

	parent, ok := objectFor(triples, agvocab.LoopParent).(string)
	if !ok {
		t.Fatal("LoopParent object is not a string")
	}
	want := "acme.ops.agentic-loop.agent.execution.parent-loop-uuid"
	if parent != want {
		t.Errorf("LoopParent = %q, want %q", parent, want)
	}
	if !message.IsValidEntityID(parent) {
		t.Errorf("LoopParent %q is not a valid 6-part entity ID", parent)
	}
}

func TestBuildSpawnIdentityTriples_OptionalFieldsPresentWhenSet(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.spawn-full"
	task := &agentic.TaskMessage{
		TaskID:       "task-spawn-full",
		Role:         "architect",
		ParentLoopID: "parent-uuid",
		WorkflowSlug: "code-review",
		WorkflowStep: "draft",
		UserID:       "user-xyz",
		Prompt:       "Design the new auth flow",
	}

	triples := buildSpawnTriples(loopEntityID, task, "acme", "ops")
	facts := predicateSet(triples)

	optional := []string{
		agvocab.LoopParent,
		agvocab.LoopWorkflow,
		agvocab.LoopWorkflowStep,
		agvocab.LoopUser,
	}
	for _, pred := range optional {
		if !facts[pred] {
			t.Errorf("expected predicate %s to be present, but it was omitted", pred)
		}
	}

	if got := objectFor(triples, agvocab.LoopWorkflow); got != "code-review" {
		t.Errorf("%s: got %v, want code-review", agvocab.LoopWorkflow, got)
	}
	if got := objectFor(triples, agvocab.LoopWorkflowStep); got != "draft" {
		t.Errorf("%s: got %v, want draft", agvocab.LoopWorkflowStep, got)
	}
	if got := objectFor(triples, agvocab.LoopUser); got != "user-xyz" {
		t.Errorf("%s: got %v, want user-xyz", agvocab.LoopUser, got)
	}
}

func TestBuildSpawnIdentityTriples_OptionalFieldsOmittedWhenEmpty(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.spawn-minimal"
	task := &agentic.TaskMessage{
		TaskID: "task-spawn-minimal",
		Role:   "researcher",
		// ParentLoopID, WorkflowSlug, WorkflowStep, UserID, Prompt all empty
	}

	triples := buildSpawnTriples(loopEntityID, task, "acme", "ops")
	facts := predicateSet(triples)

	optional := []string{
		agvocab.LoopParent,
		agvocab.LoopWorkflow,
		agvocab.LoopWorkflowStep,
		agvocab.LoopUser,
		agvocab.LoopDescription,
	}
	for _, pred := range optional {
		if facts[pred] {
			t.Errorf("expected predicate %s to be omitted when empty, but it was present", pred)
		}
	}

	// Role + task survive.
	if !facts[agvocab.LoopRole] {
		t.Errorf("expected %s to be present", agvocab.LoopRole)
	}
	if !facts[agvocab.LoopTask] {
		t.Errorf("expected %s to be present", agvocab.LoopTask)
	}
}

func TestBuildSpawnIdentityTriples_LongPromptTruncated(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.spawn-long-prompt"

	// 10KB prompt — above maxPromptTripleBytes (8KB).
	longPrompt := ""
	for i := 0; i < 10240; i++ {
		longPrompt += "a"
	}

	task := &agentic.TaskMessage{
		TaskID: "task-long",
		Role:   "researcher",
		Prompt: longPrompt,
	}

	triples := buildSpawnTriples(loopEntityID, task, "acme", "ops")

	desc, ok := objectFor(triples, agvocab.LoopDescription).(string)
	if !ok {
		t.Fatal("LoopDescription object is not a string")
	}
	if len(desc) > maxPromptTripleBytes {
		t.Errorf("LoopDescription length %d exceeds cap %d", len(desc), maxPromptTripleBytes)
	}
}

// Equal timestamps on all triples in one spawn batch are a soft
// expectation downstream — none of the rule-engine semantics depend
// on it, but it makes diff/inspection cleaner. Pin the property so a
// refactor that reaches for time.Now() in the per-triple constructor
// (instead of once at the top of LoopExecutionEntity.Triples()) fails
// this test.
func TestBuildSpawnIdentityTriples_SharedTimestamp(t *testing.T) {
	task := &agentic.TaskMessage{
		TaskID:       "task-shared-ts",
		Role:         "researcher",
		ParentLoopID: "parent-id",
		WorkflowSlug: "wf",
		WorkflowStep: "step",
		UserID:       "user",
		Prompt:       "prompt",
	}

	triples := buildSpawnTriples("acme.ops.agentic-loop.agent.execution.spawn-shared", task, "acme", "ops")
	if len(triples) < 2 {
		t.Fatalf("expected multiple triples, got %d", len(triples))
	}

	first := triples[0].Timestamp
	for i, tr := range triples[1:] {
		if !tr.Timestamp.Equal(first) {
			t.Errorf("triple[%d] timestamp %v != triple[0] timestamp %v", i+1, tr.Timestamp, first)
		}
	}
}

// Pin a regression guard: future Edits that add new spawn-known fields
// to TaskMessage should also add them to LoopExecutionEntity.Triples(). The
// counter test surfaces missed coverage when a TaskMessage field with
// a vocab-defined predicate gets added but the entity doesn't emit it.
func TestBuildSpawnIdentityTriples_TripleCountWithFullTask(t *testing.T) {
	task := &agentic.TaskMessage{
		TaskID:       "task-count",
		Role:         "researcher",
		ParentLoopID: "parent-id",
		RunID:        "run-anchor-uuid",
		InReplyTo:    "asking-loop-uuid",
		WorkflowSlug: "wf",
		WorkflowStep: "step",
		UserID:       "user",
		Prompt:       "prompt",
	}

	triples := buildSpawnTriples("acme.ops.agentic-loop.agent.execution.spawn-count", task, "acme", "ops")
	want := 10 // role, task, parent, run, run.entity_id, reply_to, workflow, workflow_step, user, description
	if len(triples) != want {
		preds := make([]string, 0, len(triples))
		for _, tr := range triples {
			preds = append(preds, tr.Predicate)
		}
		t.Errorf("expected %d triples, got %d: %v", want, len(triples), preds)
	}
}

// Sanity check: now-vs-past timestamps are sensible (not zero).
func TestBuildSpawnIdentityTriples_TimestampPopulated(t *testing.T) {
	task := &agentic.TaskMessage{TaskID: "task-ts", Role: "researcher"}
	triples := buildSpawnTriples("acme.ops.agentic-loop.agent.execution.spawn-ts", task, "acme", "ops")

	if len(triples) == 0 {
		t.Fatal("expected at least one triple")
	}
	if triples[0].Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
	if time.Since(triples[0].Timestamp) > time.Minute {
		t.Errorf("timestamp suspiciously old: %v", triples[0].Timestamp)
	}
}

// --- ADR-053 Pass A: agent.loop.run triple in spawn identity ---

// TestBuildSpawnIdentityTriples_StampsRunID verifies that agent.loop.run is stamped
// with the bare RunID string when TaskMessage.RunID is set (ADR-053 D7).
func TestBuildSpawnIdentityTriples_StampsRunID(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.child-run-001"
	task := &agentic.TaskMessage{
		TaskID: "task-run-001",
		Role:   "researcher",
		RunID:  "root-loop-uuid",
	}

	triples := buildSpawnTriples(loopEntityID, task, "acme", "ops")
	facts := predicateSet(triples)

	if !facts[agvocab.LoopRun] {
		t.Fatalf("expected agent.loop.run triple; got predicates: %v", facts)
	}

	runID, ok := objectFor(triples, agvocab.LoopRun).(string)
	if !ok {
		t.Fatal("LoopRun object is not a string")
	}
	if runID != "root-loop-uuid" {
		t.Errorf("LoopRun = %q, want %q", runID, "root-loop-uuid")
	}

	// ADR-053 follow-up: agent.run.entity-id is the FULL 6-part chain entity ID,
	// the rule-addressable upsert subject.
	if !facts[agvocab.LoopRunEntityID] {
		t.Fatalf("expected agent.run.entity-id triple; got predicates: %v", facts)
	}
	runEntityID, ok := objectFor(triples, agvocab.LoopRunEntityID).(string)
	if !ok {
		t.Fatal("LoopRunEntityID object is not a string")
	}
	if want := "acme.ops.chain.agent.execution.root-loop-uuid"; runEntityID != want {
		t.Errorf("LoopRunEntityID = %q, want %q", runEntityID, want)
	}
}

// TestBuildSpawnIdentityTriples_OmitsRunIDWhenEmpty verifies that agent.loop.run is
// NOT emitted when TaskMessage.RunID is empty — loops not in a run must not
// carry a zero-value triple.
func TestBuildSpawnIdentityTriples_OmitsRunIDWhenEmpty(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.solo-loop"
	task := &agentic.TaskMessage{
		TaskID: "task-solo",
		Role:   "researcher",
		// RunID empty
	}

	triples := buildSpawnTriples(loopEntityID, task, "acme", "ops")
	facts := predicateSet(triples)

	if facts[agvocab.LoopRun] {
		t.Errorf("expected agent.loop.run triple to be omitted when RunID is empty")
	}
	if facts[agvocab.LoopRunEntityID] {
		t.Errorf("expected agent.run.entity-id triple to be omitted when RunID is empty")
	}
}

// TestBuildSpawnIdentityTriples_RunIDIsNotEntityID guards that the LoopRun
// object is the BARE run loop-id (the 6-part form lives on the separate
// agent.run.entity-id triple). Conflating the two would break ResolveRun,
// which reads agent.loop.run as a bare id and rebuilds the chain entity ID.
func TestBuildSpawnIdentityTriples_RunIDIsNotEntityID(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.child-run-002"
	task := &agentic.TaskMessage{
		TaskID: "task-run-002",
		Role:   "coordinator",
		RunID:  "bare-run-uuid",
	}

	triples := buildSpawnTriples(loopEntityID, task, "acme", "ops")
	runID, ok := objectFor(triples, agvocab.LoopRun).(string)
	if !ok {
		t.Fatal("LoopRun object is not a string")
	}

	// Must be the bare UUID, not a dotted entity ID.
	if len(runID) > 0 && runID != "bare-run-uuid" {
		t.Errorf("LoopRun = %q, want bare UUID %q", runID, "bare-run-uuid")
	}
	// Defensive: must not contain dots (which would indicate it was expanded to a 6-part ID).
	for _, ch := range runID {
		if ch == '.' {
			t.Errorf("LoopRun %q contains dots — must be bare loop UUID, not a 6-part entity ID", runID)
			break
		}
	}
}

// --- gh#256: agent.loop.reply_to in spawn identity ---

// TestBuildSpawnIdentityTriples_StampsReplyTo verifies that agent.loop.reply_to
// is stamped as a valid 6-part loop entity reference (mirroring agent.loop.parent)
// when TaskMessage.InReplyTo is set, so a resume rule reading
// $entity.triple.agent.loop.reply_to gets a navigable reference and can fire on
// the reply (ADR-053 §4b-2 clarification resume).
func TestBuildSpawnIdentityTriples_StampsReplyTo(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.reply-001"
	task := &agentic.TaskMessage{
		TaskID:    "task-reply-001",
		Role:      "coordinator",
		InReplyTo: "asking-loop-uuid",
	}

	triples := buildSpawnTriples(loopEntityID, task, "acme", "ops")
	facts := predicateSet(triples)

	if !facts[agvocab.LoopReplyTo] {
		t.Fatalf("expected agent.loop.reply_to triple; got predicates: %v", facts)
	}

	replyTo, ok := objectFor(triples, agvocab.LoopReplyTo).(string)
	if !ok {
		t.Fatal("LoopReplyTo object is not a string")
	}
	want := "acme.ops.agentic-loop.agent.execution.asking-loop-uuid"
	if replyTo != want {
		t.Errorf("LoopReplyTo = %q, want %q", replyTo, want)
	}
	if !message.IsValidEntityID(replyTo) {
		t.Errorf("LoopReplyTo %q is not a valid 6-part entity ID", replyTo)
	}
}

// TestBuildSpawnIdentityTriples_OmitsReplyToWhenEmpty verifies that
// agent.loop.reply_to is NOT emitted for a non-reply loop — an ordinary
// continuation must not be mis-marked as a reply.
func TestBuildSpawnIdentityTriples_OmitsReplyToWhenEmpty(t *testing.T) {
	loopEntityID := "acme.ops.agentic-loop.agent.execution.non-reply"
	task := &agentic.TaskMessage{
		TaskID: "task-non-reply",
		Role:   "researcher",
		// InReplyTo empty
	}

	triples := buildSpawnTriples(loopEntityID, task, "acme", "ops")
	if predicateSet(triples)[agvocab.LoopReplyTo] {
		t.Errorf("expected agent.loop.reply_to triple to be omitted when InReplyTo is empty")
	}
}

// TestBuildSpawnIdentityTriples_SharedTimestamp_WithRunID extends the timestamp
// test to include RunID — all triples in one batch share the same timestamp.
func TestBuildSpawnIdentityTriples_SharedTimestamp_WithRunID(t *testing.T) {
	task := &agentic.TaskMessage{
		TaskID:       "task-shared-ts-run",
		Role:         "researcher",
		ParentLoopID: "parent-id",
		RunID:        "run-id",
		WorkflowSlug: "wf",
		WorkflowStep: "step",
		UserID:       "user",
		Prompt:       "prompt",
	}

	triples := buildSpawnTriples("acme.ops.agentic-loop.agent.execution.spawn-shared-run", task, "acme", "ops")
	if len(triples) < 2 {
		t.Fatalf("expected multiple triples, got %d", len(triples))
	}

	first := triples[0].Timestamp
	for i, tr := range triples[1:] {
		if !tr.Timestamp.Equal(first) {
			t.Errorf("triple[%d] timestamp %v != triple[0] timestamp %v", i+1, tr.Timestamp, first)
		}
	}
}
