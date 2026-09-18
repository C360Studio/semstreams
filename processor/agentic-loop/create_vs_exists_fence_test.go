package agenticloop

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/agentic-loop/prompt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Internal, deliberately: I7 is a claim about the three per-loop maps
// CreateLoopWithID writes, and the only honest way to observe "unchanged" is to
// read them, not to infer it from an error value.

const fenceSystemFragment = "FENCE-SYSTEM-PROMPT"

// fenceHandler builds a MessageHandler whose assembled system prompt is
// non-empty, so "does not re-seed the system prompt" is a load-bearing
// assertion rather than a tautology over an empty string.
func fenceHandler(t *testing.T) *MessageHandler {
	t.Helper()
	h := NewMessageHandler(DefaultConfig())
	reg := prompt.NewRegistry()
	reg.Add(prompt.Fragment{
		ID:       "fence-system",
		Category: prompt.CategorySystem,
		Content:  fenceSystemFragment,
	})
	h.SetPromptRegistry(reg)
	return h
}

func indexOfContent(msgs []agentic.ChatMessage, content string) int {
	for i, m := range msgs {
		if m.Content == content {
			return i
		}
	}
	return -1
}

// TestCreateLoopWithIDRefusesExistingTokenWithoutMutation is I7: a refused
// create leaves the loop entity, the pending-tool set, and the context manager
// holding exactly the values they held before the call. Before the fence, all
// three were overwritten unconditionally and the conversation under the token
// was destroyed.
func TestCreateLoopWithIDRefusesExistingTokenWithoutMutation(t *testing.T) {
	lm := NewLoopManager()
	loopID := lm.GenerateLoopID()

	if _, err := lm.CreateLoopWithID(loopID, "task-original", "general", "model-a", 7); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := lm.AddPendingTool(loopID, "call-in-flight"); err != nil {
		t.Fatalf("AddPendingTool: %v", err)
	}
	cmBefore := lm.GetContextManager(loopID)
	if err := cmBefore.AddMessage(RegionRecentHistory, agentic.ChatMessage{
		Role: "user", Content: "the conversation so far",
	}); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	entityBefore, err := lm.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}

	_, err = lm.CreateLoopWithID(loopID, "task-second", "reviewer", "model-b", 3)
	if err == nil {
		t.Fatal("create over a registered token succeeded; the fence is not in place")
	}
	if !errors.Is(err, ErrLoopAlreadyExists) {
		t.Fatalf("refusal = %v, want errors.Is ErrLoopAlreadyExists", err)
	}
	if !errs.IsInvalid(err) {
		t.Fatalf("refusal class = %v, want invalid", err)
	}

	// Map 1 — the loop entity.
	entityAfter, err := lm.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop after refusal: %v", err)
	}
	if !reflect.DeepEqual(entityAfter, entityBefore) {
		t.Fatalf("loop entity mutated by a refused create:\n before %+v\n after  %+v", entityBefore, entityAfter)
	}

	// Map 2 — the pending-tool set. A fresh make() would have emptied it.
	pending := lm.GetPendingTools(loopID)
	if len(pending) != 1 || pending[0] != "call-in-flight" {
		t.Fatalf("pending tools = %v, want [call-in-flight]", pending)
	}

	// Map 3 — the context manager, by identity and by content.
	if cmAfter := lm.GetContextManager(loopID); cmAfter != cmBefore {
		t.Fatal("context manager replaced by a refused create")
	}
	msgs := cmBefore.GetContext()
	if len(msgs) != 1 || msgs[0].Content != "the conversation so far" {
		t.Fatalf("conversation = %+v, want the single seeded turn", msgs)
	}
}

// TestFormRefusalPrecedesAlreadyExists locks the refusal ORDER: a
// non-canonical token is reported as malformed whether or not something is
// registered under it. Collapsing the two would tell a caller holding a
// hand-authored token that the token is taken, sending them to look for a loop
// that a canonical-token deployment can never have.
func TestFormRefusalPrecedesAlreadyExists(t *testing.T) {
	const authored = "workflow-7"

	// No loop registered under the malformed token.
	lm := NewLoopManager()
	_, err := lm.CreateLoopWithID(authored, "task-1", "general", "model-a")
	if err == nil {
		t.Fatal("malformed token accepted")
	}
	if errors.Is(err, ErrLoopAlreadyExists) {
		t.Fatalf("malformed token reported as a collision: %v", err)
	}

	// Same token, now registered. Only a direct map write can set this up —
	// CreateLoopWithID itself refuses to register a non-canonical token.
	registered := agentic.NewLoopEntity(authored, "task-1", "general", "model-a", 5)
	lm.loops[authored] = &registered

	_, err = lm.CreateLoopWithID(authored, "task-2", "general", "model-a")
	if err == nil {
		t.Fatal("malformed token accepted when a loop was registered under it")
	}
	if errors.Is(err, ErrLoopAlreadyExists) {
		t.Fatalf("form refusal did not precede the already-exists check: %v", err)
	}
	if !errs.IsInvalid(err) {
		t.Fatalf("refusal class = %v, want invalid", err)
	}
}

// spec: agentic-loop / A task owns one execution without rebinding
func TestDifferentTaskRefusalPreservesContextAndProducesNoWork(t *testing.T) {
	h := fenceHandler(t)
	task := TaskMessage{LoopID: uuid.NewString(), TaskID: "task-1", Role: "general", Model: "model-a", Prompt: "first turn"}
	_, err := h.HandleTask(t.Context(), task)
	require.NoError(t, err)
	cm := h.loopManager.GetContextManager(task.LoopID)
	require.NoError(t, cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{Role: "assistant", Content: "assistant turn"}))
	before := cm.GetContext()
	entity, err := h.GetLoop(task.LoopID)
	require.NoError(t, err)

	task.TaskID = "task-2"
	task.Prompt = "second turn"
	result, err := h.HandleTask(t.Context(), task)
	require.True(t, errs.IsFatal(err), "different-task reuse must be fatal correlation: %v", err)
	require.Empty(t, result.PublishedMessages)
	after, err := h.GetLoop(task.LoopID)
	require.NoError(t, err)
	require.Equal(t, entity, after)
	require.Same(t, cm, h.loopManager.GetContextManager(task.LoopID))
	require.Equal(t, before, cm.GetContext())
	require.Len(t, cm.regions[RegionSystemPrompt], 1, "refusal must not reseed instructions")
}

// spec: agentic-loop / A task owns one execution without rebinding
func TestSameTaskRedeliveryIsDeduplicatedWithoutReseeding(t *testing.T) {
	h := fenceHandler(t)
	task := TaskMessage{LoopID: uuid.NewString(), TaskID: "task-1", Role: "general", Model: "model-a", Prompt: "first turn"}
	_, err := h.HandleTask(t.Context(), task)
	require.NoError(t, err)
	cm := h.loopManager.GetContextManager(task.LoopID)
	before := cm.GetContext()
	result, err := h.HandleTask(t.Context(), task)
	require.NoError(t, err)
	require.Equal(t, task.LoopID, result.LoopID)
	require.False(t, result.Created)
	require.Empty(t, result.PublishedMessages)
	require.Same(t, cm, h.loopManager.GetContextManager(task.LoopID))
	require.Equal(t, before, cm.GetContext())
	require.Equal(t, 1, countContent(cm.GetContext(), task.Prompt))
}

func countContent(msgs []agentic.ChatMessage, content string) int {
	n := 0
	for _, m := range msgs {
		if m.Content == content {
			n++
		}
	}
	return n
}

// A different task cannot steal the partially assembled tool round.
// spec: agentic-loop / A task owns one execution without rebinding
func TestDifferentTaskWithToolsInFlightIsQuarantined(t *testing.T) {
	ctx := t.Context()
	h := fenceHandler(t)

	first, err := h.HandleTask(ctx, TaskMessage{
		LoopID: uuid.NewString(),
		TaskID: "task-1", Role: "general", Model: "model-a", Prompt: "first turn"})
	if err != nil {
		t.Fatalf("HandleTask (first): %v", err)
	}
	loopID := first.LoopID

	// The half-written round: the assistant turn carrying tool_calls is in the
	// conversation, the matching tool result has not arrived, and the call is
	// outstanding in the pending-tool set.
	cm := h.loopManager.GetContextManager(loopID)
	if err := cm.AddMessage(RegionRecentHistory, agentic.ChatMessage{
		Role: "assistant",
		ToolCalls: []agentic.ToolCall{{
			ID: "call-in-flight", Name: "search", Arguments: map[string]any{},
		}},
	}); err != nil {
		t.Fatalf("seed assistant tool_calls turn: %v", err)
	}
	if err := h.loopManager.AddPendingTool(loopID, "call-in-flight"); err != nil {
		t.Fatalf("AddPendingTool: %v", err)
	}
	before, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	turnsBefore := len(cm.GetContext())

	result, err := h.HandleTask(ctx, TaskMessage{
		TaskID: "task-2", LoopID: loopID, Role: "general", Model: "model-a", Prompt: "second turn",
	})
	if err == nil {
		t.Fatalf("conflicting task of a loop with an outstanding tool call accepted: %+v", result)
	}
	if !errs.IsFatal(err) {
		t.Fatalf("refusal = %v, want fatal correlation conflict", err)
	}
	if len(result.PublishedMessages) != 0 {
		t.Fatalf("refused conflicting task published %d messages", len(result.PublishedMessages))
	}

	// Nothing moved: no user turn appended, the call is still outstanding, and
	// the loop's own task association still names the round in flight.
	if got := len(cm.GetContext()); got != turnsBefore {
		t.Fatalf("conversation length after refusal = %d, want %d (a turn was appended)", got, turnsBefore)
	}
	if idx := indexOfContent(cm.GetContext(), "second turn"); idx >= 0 {
		t.Fatalf("refused conflicting task appended its prompt at index %d", idx)
	}
	pending := h.loopManager.GetPendingTools(loopID)
	if len(pending) != 1 || pending[0] != "call-in-flight" {
		t.Fatalf("pending tools after refusal = %v, want [call-in-flight]", pending)
	}
	after, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop after refusal: %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("loop mutated by a refused conflicting task:\n before %+v\n after  %+v", before, after)
	}
	if after.TaskID != "task-1" {
		t.Fatalf("task association rebound by a refused conflicting task: %q", after.TaskID)
	}
}

// Refusal preserves the gate so its later human decision still targets A.
// spec: agentic-loop / A task owns one execution without rebinding
func TestDifferentTaskAwaitingApprovalIsQuarantined(t *testing.T) {
	ctx := t.Context()
	h := fenceHandler(t)

	loopID := setUpAwaitingLoop(t, h, time.Minute, time.Second)
	before, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if before.State != agentic.LoopStateAwaitingApproval {
		t.Fatalf("fixture state = %s, want awaiting_approval", before.State)
	}
	// The other half of the in-flight rule must not be what refuses this one.
	if pending := h.loopManager.GetPendingTools(loopID); len(pending) != 0 {
		t.Fatalf("fixture holds outstanding tool calls %v; this case must isolate awaiting_approval", pending)
	}

	result, err := h.HandleTask(ctx, TaskMessage{
		TaskID: "task-2", LoopID: loopID, Role: "general", Model: "model-a", Prompt: "second turn",
	})
	if err == nil {
		t.Fatalf("conflicting task of a loop awaiting approval accepted: %+v", result)
	}
	if !errs.IsFatal(err) {
		t.Fatalf("refusal = %v, want fatal correlation conflict", err)
	}
	if len(result.PublishedMessages) != 0 {
		t.Fatalf("refused conflicting task published %d messages", len(result.PublishedMessages))
	}

	after, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop after refusal: %v", err)
	}
	if after.State != agentic.LoopStateAwaitingApproval {
		t.Fatalf("loop moved off awaiting_approval to %s; the human's decision would now be dropped", after.State)
	}
	if after.PendingApproval == nil || after.PendingApproval.CallID != "call-gated" {
		t.Fatalf("pending approval lost by a refused conflicting task: %+v", after.PendingApproval)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("loop mutated by a refused conflicting task:\n before %+v\n after  %+v", before, after)
	}
}
