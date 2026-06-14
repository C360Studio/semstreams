package agentic_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// --- LoopExecutionEntity.EntityID ---

func TestLoopExecutionEntity_EntityID(t *testing.T) {
	e := &agentic.LoopExecutionEntity{
		Org:      "acme",
		Platform: "ops",
		LoopID:   "loop-abc123",
		Task:     &agentic.TaskMessage{TaskID: "t", Role: "r"},
	}
	got := e.EntityID()
	want := "acme.ops.agent.agentic-loop.execution.loop-abc123"
	if got != want {
		t.Errorf("EntityID() = %q, want %q", got, want)
	}
	if !message.IsValidEntityID(got) {
		t.Errorf("EntityID() %q is not a valid 6-part entity ID", got)
	}
}

// --- LoopExecutionEntity.Triples ---

func triples(org, platform, loopID string, task *agentic.TaskMessage) []message.Triple {
	e := &agentic.LoopExecutionEntity{Org: org, Platform: platform, LoopID: loopID, Task: task}
	return e.Triples()
}

func predSet(ts []message.Triple) map[string]bool {
	s := make(map[string]bool, len(ts))
	for _, t := range ts {
		s[t.Predicate] = true
	}
	return s
}

func objFor(ts []message.Triple, pred string) any {
	for _, t := range ts {
		if t.Predicate == pred {
			return t.Object
		}
	}
	return nil
}

func TestLoopExecutionEntity_Triples_NilTask(t *testing.T) {
	e := &agentic.LoopExecutionEntity{Org: "acme", Platform: "ops", LoopID: "loop-1"}
	if got := e.Triples(); got != nil {
		t.Errorf("Triples() with nil Task should return nil, got %d triples", len(got))
	}
}

func TestLoopExecutionEntity_Triples_RequiredFields(t *testing.T) {
	task := &agentic.TaskMessage{
		TaskID: "task-001",
		Role:   "researcher",
	}
	ts := triples("acme", "ops", "loop-001", task)

	loopEntityID := "acme.ops.agent.agentic-loop.execution.loop-001"
	for _, tr := range ts {
		if tr.Subject != loopEntityID {
			t.Errorf("triple subject = %q, want %q", tr.Subject, loopEntityID)
		}
		if tr.Confidence != 1.0 {
			t.Errorf("triple confidence = %v, want 1.0", tr.Confidence)
		}
		if tr.Source != "agentic-loop" {
			t.Errorf("triple source = %q, want agentic-loop", tr.Source)
		}
		if tr.Timestamp.IsZero() {
			t.Error("triple timestamp is zero")
		}
	}

	ps := predSet(ts)
	if !ps[agvocab.LoopRole] {
		t.Errorf("missing required predicate: %s", agvocab.LoopRole)
	}
	if !ps[agvocab.LoopTask] {
		t.Errorf("missing required predicate: %s", agvocab.LoopTask)
	}
	if got := objFor(ts, agvocab.LoopRole); got != "researcher" {
		t.Errorf("LoopRole = %v, want researcher", got)
	}
	if got := objFor(ts, agvocab.LoopTask); got != "task-001" {
		t.Errorf("LoopTask = %v, want task-001", got)
	}
}

func TestLoopExecutionEntity_Triples_AllOptionalPresent(t *testing.T) {
	task := &agentic.TaskMessage{
		TaskID:       "task-full",
		Role:         "architect",
		ParentLoopID: "parent-uuid",
		RunID:        "run-anchor-uuid",
		InReplyTo:    "reply-loop-uuid",
		WorkflowSlug: "code-review",
		WorkflowStep: "draft",
		UserID:       "user-xyz",
		Prompt:       "Design the new auth flow",
	}
	ts := triples("acme", "ops", "loop-full", task)
	ps := predSet(ts)

	// 10 triples: role, task, parent, run, run.entity_id, reply_to, workflow, workflow_step, user, description
	want := 10
	if len(ts) != want {
		preds := make([]string, 0, len(ts))
		for _, tr := range ts {
			preds = append(preds, tr.Predicate)
		}
		t.Errorf("expected %d triples, got %d: %v", want, len(ts), preds)
	}

	optional := []string{
		agvocab.LoopParent,
		agvocab.LoopRun,
		agvocab.LoopRunEntityID,
		agvocab.LoopReplyTo,
		agvocab.LoopWorkflow,
		agvocab.LoopWorkflowStep,
		agvocab.LoopUser,
		agvocab.LoopDescription,
	}
	for _, pred := range optional {
		if !ps[pred] {
			t.Errorf("expected optional predicate %s to be present when field is set", pred)
		}
	}
}

func TestLoopExecutionEntity_Triples_OptionalOmittedWhenEmpty(t *testing.T) {
	task := &agentic.TaskMessage{
		TaskID: "task-min",
		Role:   "researcher",
		// all optional fields empty
	}
	ts := triples("acme", "ops", "loop-min", task)
	ps := predSet(ts)

	optional := []string{
		agvocab.LoopParent,
		agvocab.LoopRun,
		agvocab.LoopRunEntityID,
		agvocab.LoopReplyTo,
		agvocab.LoopWorkflow,
		agvocab.LoopWorkflowStep,
		agvocab.LoopUser,
		agvocab.LoopDescription,
	}
	for _, pred := range optional {
		if ps[pred] {
			t.Errorf("expected %s to be omitted when field is empty", pred)
		}
	}
}

// Parent triple must be a full 6-part entity ID (the navigable reference
// shape that semteams ADR-038 ancestry-walk and rule substitution rely on).
func TestLoopExecutionEntity_Triples_ParentIsEntityID(t *testing.T) {
	task := &agentic.TaskMessage{
		TaskID:       "task-parent",
		Role:         "gather",
		ParentLoopID: "parent-loop-uuid",
	}
	ts := triples("acme", "ops", "loop-child", task)

	parent, ok := objFor(ts, agvocab.LoopParent).(string)
	if !ok {
		t.Fatal("LoopParent object is not a string")
	}
	want := "acme.ops.agent.agentic-loop.execution.parent-loop-uuid"
	if parent != want {
		t.Errorf("LoopParent = %q, want %q", parent, want)
	}
	if !message.IsValidEntityID(parent) {
		t.Errorf("LoopParent %q is not a valid 6-part entity ID", parent)
	}
}

// ReplyTo triple must be a full 6-part loop entity ID.
func TestLoopExecutionEntity_Triples_ReplyToIsEntityID(t *testing.T) {
	task := &agentic.TaskMessage{
		TaskID:    "task-reply",
		Role:      "coordinator",
		InReplyTo: "asking-loop-uuid",
	}
	ts := triples("acme", "ops", "loop-reply", task)

	replyTo, ok := objFor(ts, agvocab.LoopReplyTo).(string)
	if !ok {
		t.Fatal("LoopReplyTo object is not a string")
	}
	want := "acme.ops.agent.agentic-loop.execution.asking-loop-uuid"
	if replyTo != want {
		t.Errorf("LoopReplyTo = %q, want %q", replyTo, want)
	}
	if !message.IsValidEntityID(replyTo) {
		t.Errorf("LoopReplyTo %q is not a valid 6-part entity ID", replyTo)
	}
}

// RunID: LoopRun must be the bare ID (not the 6-part), LoopRunEntityID the 6-part.
func TestLoopExecutionEntity_Triples_RunIDShape(t *testing.T) {
	task := &agentic.TaskMessage{
		TaskID: "task-run",
		Role:   "researcher",
		RunID:  "run-uuid",
	}
	ts := triples("acme", "ops", "loop-run", task)

	runID, ok := objFor(ts, agvocab.LoopRun).(string)
	if !ok {
		t.Fatal("LoopRun object is not a string")
	}
	if runID != "run-uuid" {
		t.Errorf("LoopRun = %q, want bare UUID", runID)
	}
	if strings.Contains(runID, ".") {
		t.Errorf("LoopRun %q contains dots — must be bare, not 6-part", runID)
	}

	runEntityID, ok := objFor(ts, agvocab.LoopRunEntityID).(string)
	if !ok {
		t.Fatal("LoopRunEntityID object is not a string")
	}
	wantEntityID := "acme.ops.agent.chain.execution.run-uuid"
	if runEntityID != wantEntityID {
		t.Errorf("LoopRunEntityID = %q, want %q", runEntityID, wantEntityID)
	}
}

// All triples in one Triples() call must share the same wall-clock timestamp
// (captured once at call time, not per-triple).
func TestLoopExecutionEntity_Triples_SharedTimestamp(t *testing.T) {
	task := &agentic.TaskMessage{
		TaskID:       "task-ts",
		Role:         "researcher",
		ParentLoopID: "parent-id",
		WorkflowSlug: "wf",
		WorkflowStep: "step",
		UserID:       "user",
		Prompt:       "prompt",
	}
	ts := triples("acme", "ops", "loop-ts", task)
	if len(ts) < 2 {
		t.Fatalf("expected multiple triples, got %d", len(ts))
	}
	first := ts[0].Timestamp
	for i, tr := range ts[1:] {
		if !tr.Timestamp.Equal(first) {
			t.Errorf("triple[%d] timestamp %v != triple[0] timestamp %v", i+1, tr.Timestamp, first)
		}
	}
}

// Timestamp must be recent (not zero, not stale).
func TestLoopExecutionEntity_Triples_TimestampNotZero(t *testing.T) {
	task := &agentic.TaskMessage{TaskID: "t", Role: "r"}
	ts := triples("acme", "ops", "loop-tts", task)
	if len(ts) == 0 {
		t.Fatal("expected at least one triple")
	}
	if ts[0].Timestamp.IsZero() {
		t.Error("timestamp is zero")
	}
	if time.Since(ts[0].Timestamp) > time.Minute {
		t.Errorf("timestamp %v is suspiciously old", ts[0].Timestamp)
	}
}

// Long prompts must be truncated to loopExecutionMaxPromptTripleBytes (8KB).
func TestLoopExecutionEntity_Triples_LongPromptTruncated(t *testing.T) {
	longPrompt := strings.Repeat("a", 10_000) // 10KB — above 8KB cap
	task := &agentic.TaskMessage{TaskID: "t", Role: "r", Prompt: longPrompt}
	ts := triples("acme", "ops", "loop-lp", task)

	desc, ok := objFor(ts, agvocab.LoopDescription).(string)
	if !ok {
		t.Fatal("LoopDescription is not a string")
	}
	const maxBytes = 8 * 1024
	if len(desc) > maxBytes {
		t.Errorf("LoopDescription length %d exceeds cap %d", len(desc), maxBytes)
	}
	if !strings.HasSuffix(desc, "…[truncated]") {
		t.Errorf("truncated description missing marker suffix: %q", desc[max(0, len(desc)-20):])
	}
}

// UTF-8 safety: truncation must not split a multi-byte rune.
func TestLoopExecutionEntity_Triples_TruncationUTF8Safe(t *testing.T) {
	// Each "日" is 3 bytes; build a prompt that forces a mid-rune boundary.
	longPrompt := strings.Repeat("日", 3000) // 9000 bytes
	task := &agentic.TaskMessage{TaskID: "t", Role: "r", Prompt: longPrompt}
	ts := triples("acme", "ops", "loop-utf8", task)

	desc, ok := objFor(ts, agvocab.LoopDescription).(string)
	if !ok {
		t.Fatal("LoopDescription is not a string")
	}
	if !utf8.ValidString(desc) {
		t.Errorf("truncated LoopDescription is not valid UTF-8")
	}
}

// --- LoopExecutionMessageType ---

func TestLoopExecutionMessageType_Valid(t *testing.T) {
	mt := agentic.LoopExecutionMessageType()
	if mt.Domain == "" {
		t.Error("MessageType.Domain is empty")
	}
	if mt.Category == "" {
		t.Error("MessageType.Category is empty")
	}
	if mt.Version == "" {
		t.Error("MessageType.Version is empty")
	}
	if !mt.IsValid() {
		t.Errorf("MessageType %v is not valid (IsValid() returned false)", mt)
	}
}

func TestLoopExecutionMessageType_Distinct(t *testing.T) {
	// Must not collide with any other category constant.
	mt := agentic.LoopExecutionMessageType()
	others := []string{
		agentic.CategoryLoopCreated,
		agentic.CategoryLoopCompleted,
		agentic.CategoryLoopFailed,
		agentic.CategoryLoopCancelled,
		agentic.CategoryTask,
	}
	for _, cat := range others {
		if mt.Category == cat {
			t.Errorf("CategoryLoopExecution collides with existing category %q", cat)
		}
	}
}

func TestLoopExecutionMessageType_KeyFormat(t *testing.T) {
	key := agentic.LoopExecutionMessageType().Key()
	want := "agentic.loop_execution.v1"
	if key != want {
		t.Errorf("MessageType.Key() = %q, want %q", key, want)
	}
}
