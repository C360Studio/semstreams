package agenticdispatch

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
)

// TestLoopFromEntity_RoundTrip verifies that a LoopEntity survives a
// marshal→unmarshal cycle and projects correctly onto the Loop wire type.
// In particular it guards the id→loop_id normalisation and the
// LoopState.String() conversion.
func TestLoopFromEntity_RoundTrip(t *testing.T) {
	entity := agentic.LoopEntity{
		ID:            "loop-abc",
		TaskID:        "task-xyz",
		State:         agentic.LoopStateExecuting,
		Role:          "coordinator",
		Model:         "gpt-4o",
		Iterations:    3,
		MaxIterations: 20,
		UserID:        "user-1",
		ChannelType:   "cli",
		ParentLoopID:  "loop-parent",
		RunID:         "run-root",
		Outcome:       "",
		Result:        "",
		Error:         "",
	}

	// Production round-trip: marshal to JSON then unmarshal back.
	raw, err := json.Marshal(entity)
	if err != nil {
		t.Fatalf("marshal LoopEntity: %v", err)
	}

	var decoded agentic.LoopEntity
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal LoopEntity: %v", err)
	}

	got := loopFromEntity(&decoded, "c360", "ops")

	if got.LoopID != entity.ID {
		t.Errorf("LoopID: got %q, want %q (id→loop_id normalisation)", got.LoopID, entity.ID)
	}
	if got.TaskID != entity.TaskID {
		t.Errorf("TaskID: got %q, want %q", got.TaskID, entity.TaskID)
	}
	if got.State != entity.State.String() {
		t.Errorf("State: got %q, want %q", got.State, entity.State.String())
	}
	if got.Role != entity.Role {
		t.Errorf("Role: got %q, want %q", got.Role, entity.Role)
	}
	if got.Iterations != entity.Iterations {
		t.Errorf("Iterations: got %d, want %d", got.Iterations, entity.Iterations)
	}
	if got.MaxIterations != entity.MaxIterations {
		t.Errorf("MaxIterations: got %d, want %d", got.MaxIterations, entity.MaxIterations)
	}
	if got.UserID != entity.UserID {
		t.Errorf("UserID: got %q, want %q", got.UserID, entity.UserID)
	}
	if got.ChannelType != entity.ChannelType {
		t.Errorf("ChannelType: got %q, want %q", got.ChannelType, entity.ChannelType)
	}
	if got.ParentLoopID != entity.ParentLoopID {
		t.Errorf("ParentLoopID: got %q, want %q", got.ParentLoopID, entity.ParentLoopID)
	}
	// ADR-053: run_id is the bare id; run_entity_id is the derived 6-part the UI
	// feeds to getEntity() without client-side reconstruction.
	if got.RunID != entity.RunID {
		t.Errorf("RunID: got %q, want %q", got.RunID, entity.RunID)
	}
	if want := "c360.ops.chain.agent.execution.run-root"; got.RunEntityID != want {
		t.Errorf("RunEntityID: got %q, want %q (derived from org/platform + RunID)", got.RunEntityID, want)
	}
	// Token and prompt fields are absent from live entities.
	if got.TokensIn != 0 || got.TokensOut != 0 {
		t.Errorf("expected zero token counts from entity, got in=%d out=%d", got.TokensIn, got.TokensOut)
	}
	if got.Prompt != "" {
		t.Errorf("expected empty Prompt from entity, got %q", got.Prompt)
	}
}

// TestLoopRunFields_EmptyRunIDOmitted verifies the run fields stay empty (and so
// omitempty-drop from the wire) when the loop is not part of a run.
func TestLoopRunFields_EmptyRunIDOmitted(t *testing.T) {
	got := loopFromEntity(&agentic.LoopEntity{ID: "loop-x", State: agentic.LoopStateExecuting, MaxIterations: 5}, "c360", "ops")
	if got.RunID != "" || got.RunEntityID != "" {
		t.Errorf("expected empty run fields for non-run loop, got run_id=%q run_entity_id=%q", got.RunID, got.RunEntityID)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal Loop: %v", err)
	}
	if bytes.Contains(raw, []byte("run_id")) || bytes.Contains(raw, []byte("run_entity_id")) {
		t.Errorf("expected run fields omitted from JSON, got %s", raw)
	}
}

// TestLoopFromCompletion_RunFields verifies run_id + run_entity_id project from a
// real terminal event (the event carries both; no derivation needed here).
func TestLoopFromCompletion_RunFields(t *testing.T) {
	event := agentic.LoopCompletedEvent{
		LoopID:      "loop-child",
		TaskID:      "task-1",
		Outcome:     agentic.OutcomeSuccess,
		RunID:       "run-root",
		RunEntityID: "c360.ops.chain.agent.execution.run-root",
	}
	raw, err := json.Marshal(&event)
	if err != nil {
		t.Fatalf("marshal LoopCompletedEvent: %v", err)
	}
	got, ok := loopFromCompletion(raw)
	if !ok {
		t.Fatalf("loopFromCompletion returned ok=false")
	}
	if got.RunID != event.RunID {
		t.Errorf("RunID: got %q, want %q", got.RunID, event.RunID)
	}
	if got.RunEntityID != event.RunEntityID {
		t.Errorf("RunEntityID: got %q, want %q", got.RunEntityID, event.RunEntityID)
	}
}

// TestLoopFromCompletion_CompletedEvent verifies that a LoopCompletedEvent
// survives a marshal→loopFromCompletion round-trip with all consumer-critical
// fields intact, including the parent_loop→parent_loop_id tag normalisation.
func TestLoopFromCompletion_CompletedEvent(t *testing.T) {
	event := agentic.LoopCompletedEvent{
		LoopID:       "loop-done",
		TaskID:       "task-1",
		Outcome:      agentic.OutcomeSuccess,
		Role:         "coordinator",
		Result:       "answer text",
		Prompt:       "what is 2+2?",
		Model:        "gpt-4o",
		Iterations:   5,
		TokensIn:     100,
		TokensOut:    200,
		ParentLoopID: "loop-root",
		CompletedAt:  time.Now(),
	}

	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal LoopCompletedEvent: %v", err)
	}

	got, ok := loopFromCompletion(raw)
	if !ok {
		t.Fatalf("loopFromCompletion returned ok=false for valid LoopCompletedEvent")
	}

	if got.LoopID != event.LoopID {
		t.Errorf("LoopID: got %q, want %q", got.LoopID, event.LoopID)
	}
	if got.TaskID != event.TaskID {
		t.Errorf("TaskID: got %q, want %q", got.TaskID, event.TaskID)
	}
	if got.Outcome != event.Outcome {
		t.Errorf("Outcome: got %q, want %q", got.Outcome, event.Outcome)
	}
	if got.Result != event.Result {
		t.Errorf("Result: got %q, want %q", got.Result, event.Result)
	}
	if got.Prompt != event.Prompt {
		t.Errorf("Prompt: got %q, want %q", got.Prompt, event.Prompt)
	}
	if got.TokensIn != event.TokensIn {
		t.Errorf("TokensIn: got %d, want %d", got.TokensIn, event.TokensIn)
	}
	if got.TokensOut != event.TokensOut {
		t.Errorf("TokensOut: got %d, want %d", got.TokensOut, event.TokensOut)
	}
	// The critical tag-drift normalisation: events.go uses "parent_loop";
	// Loop must expose it as parent_loop_id.
	if got.ParentLoopID != event.ParentLoopID {
		t.Errorf("ParentLoopID (parent_loop tag normalisation): got %q, want %q", got.ParentLoopID, event.ParentLoopID)
	}
}

// TestLoopFromCompletion_FailedEvent verifies the LoopFailedEvent path,
// specifically that Error and Prompt survive and that parent_loop is normalised.
func TestLoopFromCompletion_FailedEvent(t *testing.T) {
	event := agentic.LoopFailedEvent{
		LoopID:       "loop-fail",
		TaskID:       "task-2",
		Outcome:      agentic.OutcomeFailed,
		Role:         "researcher",
		Error:        "context limit exceeded",
		Prompt:       "summarise everything",
		Model:        "gpt-4o-mini",
		Iterations:   20,
		TokensIn:     500,
		TokensOut:    10,
		ParentLoopID: "loop-chain-root",
	}

	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal LoopFailedEvent: %v", err)
	}

	got, ok := loopFromCompletion(raw)
	if !ok {
		t.Fatalf("loopFromCompletion returned ok=false for valid LoopFailedEvent")
	}

	if got.LoopID != event.LoopID {
		t.Errorf("LoopID: got %q, want %q", got.LoopID, event.LoopID)
	}
	if got.Error != event.Error {
		t.Errorf("Error: got %q, want %q", got.Error, event.Error)
	}
	if got.Prompt != event.Prompt {
		t.Errorf("Prompt: got %q, want %q", got.Prompt, event.Prompt)
	}
	if got.TokensIn != event.TokensIn {
		t.Errorf("TokensIn: got %d, want %d", got.TokensIn, event.TokensIn)
	}
	if got.TokensOut != event.TokensOut {
		t.Errorf("TokensOut: got %d, want %d", got.TokensOut, event.TokensOut)
	}
	if got.ParentLoopID != event.ParentLoopID {
		t.Errorf("ParentLoopID: got %q, want %q", got.ParentLoopID, event.ParentLoopID)
	}
}

// TestLoopFromCompletion_CancelledEvent verifies the LoopCancelledEvent path —
// the terminal variant with the most absent fields. It carries no Role/Result/
// Error/Prompt/State/tokens/parent_loop, so the projection must populate
// LoopID/TaskID/Outcome and zero-fill the rest cleanly rather than erroring.
func TestLoopFromCompletion_CancelledEvent(t *testing.T) {
	event := agentic.LoopCancelledEvent{
		LoopID:      "loop-cancel",
		TaskID:      "task-3",
		Outcome:     agentic.OutcomeCancelled,
		CancelledBy: "operator",
	}

	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal LoopCancelledEvent: %v", err)
	}

	got, ok := loopFromCompletion(raw)
	if !ok {
		t.Fatalf("loopFromCompletion returned ok=false for valid LoopCancelledEvent")
	}

	if got.LoopID != event.LoopID {
		t.Errorf("LoopID: got %q, want %q", got.LoopID, event.LoopID)
	}
	if got.TaskID != event.TaskID {
		t.Errorf("TaskID: got %q, want %q", got.TaskID, event.TaskID)
	}
	if got.Outcome != event.Outcome {
		t.Errorf("Outcome: got %q, want %q", got.Outcome, event.Outcome)
	}
	// Fields absent from LoopCancelledEvent must zero-fill cleanly.
	if got.Role != "" || got.Result != "" || got.Error != "" || got.Prompt != "" || got.State != "" {
		t.Errorf("expected absent fields to be empty, got Role=%q Result=%q Error=%q Prompt=%q State=%q",
			got.Role, got.Result, got.Error, got.Prompt, got.State)
	}
	if got.TokensIn != 0 || got.TokensOut != 0 {
		t.Errorf("expected zero tokens, got in=%d out=%d", got.TokensIn, got.TokensOut)
	}
	if got.ParentLoopID != "" {
		t.Errorf("ParentLoopID: got %q, want empty", got.ParentLoopID)
	}
}

// TestLoopFromCompletion_InvalidPayload verifies that ok=false is returned for
// byte sequences that cannot be decoded or yield no loop_id.
func TestLoopFromCompletion_InvalidPayload(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
	}{
		{"not JSON", []byte("not-json")},
		{"empty object", []byte("{}")},
		{"null", []byte("null")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := loopFromCompletion(tc.raw)
			if ok {
				t.Errorf("expected ok=false for %q", tc.raw)
			}
		})
	}
}

// TestLoopFromInfo_Projection verifies that LoopInfo fields project correctly
// onto the Loop wire type, including PendingApproval survival and the
// intentional omission of ParentLoopID (not tracked in LoopInfo today).
func TestLoopFromInfo_Projection(t *testing.T) {
	approval := &PendingApprovalInfo{
		CallID:      "call-1",
		ToolName:    "bash",
		Reason:      "destructive",
		RequestedAt: time.Now(),
	}
	info := &LoopInfo{
		LoopID:          "loop-tracked",
		TaskID:          "task-t",
		State:           "executing",
		Role:            "ops",
		Iterations:      2,
		MaxIterations:   10,
		UserID:          "user-ops",
		ChannelType:     "slack",
		Outcome:         "success",
		Result:          "done",
		Error:           "",
		PendingApproval: approval,
	}

	got := loopFromInfo(info)

	if got.LoopID != info.LoopID {
		t.Errorf("LoopID: got %q, want %q", got.LoopID, info.LoopID)
	}
	if got.State != info.State {
		t.Errorf("State: got %q, want %q", got.State, info.State)
	}
	if got.Outcome != info.Outcome {
		t.Errorf("Outcome: got %q, want %q", got.Outcome, info.Outcome)
	}
	if got.Result != info.Result {
		t.Errorf("Result: got %q, want %q", got.Result, info.Result)
	}
	if got.PendingApproval == nil {
		t.Fatal("PendingApproval should not be nil")
	}
	if got.PendingApproval.CallID != approval.CallID {
		t.Errorf("PendingApproval.CallID: got %q, want %q", got.PendingApproval.CallID, approval.CallID)
	}
	// ParentLoopID must be empty — LoopInfo does not track spawn relationships.
	if got.ParentLoopID != "" {
		t.Errorf("ParentLoopID: expected empty, got %q", got.ParentLoopID)
	}
	// Token and prompt fields not on LoopInfo — must be zero.
	if got.TokensIn != 0 || got.TokensOut != 0 {
		t.Errorf("expected zero token counts from LoopInfo, got in=%d out=%d", got.TokensIn, got.TokensOut)
	}
	if got.Prompt != "" {
		t.Errorf("expected empty Prompt from LoopInfo, got %q", got.Prompt)
	}
}
