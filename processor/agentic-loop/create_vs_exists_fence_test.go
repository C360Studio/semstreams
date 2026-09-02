package agenticloop

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/processor/agentic-loop/prompt"
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

func requestMessages(t *testing.T, result HandlerResult) []agentic.ChatMessage {
	t.Helper()
	if len(result.PublishedMessages) == 0 {
		t.Fatal("handler result published no messages; expected the agent request")
	}
	var envelope struct {
		Payload struct {
			Messages []agentic.ChatMessage `json:"messages"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(result.PublishedMessages[0].Data, &envelope); err != nil {
		t.Fatalf("decode agent request envelope: %v", err)
	}
	return envelope.Payload.Messages
}

func countRole(msgs []agentic.ChatMessage, role string) int {
	n := 0
	for _, m := range msgs {
		if m.Role == role {
			n++
		}
	}
	return n
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

// TestContinuationReusesContextManager is I6: across an accepted continuation
// the loop's context-manager identity is unchanged, the prior turns survive,
// and the new prompt lands after them — in the manager AND in the request the
// model actually receives.
func TestContinuationReusesContextManager(t *testing.T) {
	ctx := context.Background()
	h := fenceHandler(t)

	first, err := h.HandleTask(ctx, TaskMessage{
		TaskID: "task-1", Role: "general", Model: "model-a", Prompt: "first turn",
	})
	if err != nil {
		t.Fatalf("HandleTask (first): %v", err)
	}
	loopID := first.LoopID

	cmBefore := h.loopManager.GetContextManager(loopID)
	if err := cmBefore.AddMessage(RegionRecentHistory, agentic.ChatMessage{
		Role: "assistant", Content: "assistant turn",
	}); err != nil {
		t.Fatalf("seed assistant turn: %v", err)
	}
	if err := cmBefore.AddMessage(RegionToolResults, agentic.ChatMessage{
		Role: "tool", Content: "tool result", ToolCallID: "call-1",
	}); err != nil {
		t.Fatalf("seed tool result: %v", err)
	}
	if err := h.loopManager.AddPendingTool(loopID, "call-in-flight"); err != nil {
		t.Fatalf("AddPendingTool: %v", err)
	}

	second, err := h.HandleTask(ctx, TaskMessage{
		TaskID: "task-2", LoopID: loopID, Role: "general", Model: "model-a", Prompt: "second turn",
	})
	if err != nil {
		t.Fatalf("HandleTask (continuation): %v", err)
	}
	if second.LoopID != loopID {
		t.Fatalf("continuation loop id = %q, want %q", second.LoopID, loopID)
	}

	if cmAfter := h.loopManager.GetContextManager(loopID); cmAfter != cmBefore {
		t.Fatal("continuation replaced the context manager; the conversation was discarded")
	}

	msgs := cmBefore.GetContext()
	firstIdx := indexOfContent(msgs, "first turn")
	assistantIdx := indexOfContent(msgs, "assistant turn")
	toolIdx := indexOfContent(msgs, "tool result")
	secondIdx := indexOfContent(msgs, "second turn")
	for name, idx := range map[string]int{
		"first turn": firstIdx, "assistant turn": assistantIdx,
		"tool result": toolIdx, "second turn": secondIdx,
	} {
		if idx < 0 {
			t.Fatalf("%q missing from the continued conversation: %+v", name, msgs)
		}
	}
	if !(firstIdx < assistantIdx && assistantIdx < secondIdx) {
		t.Fatalf("new prompt not appended after the prior turns: %+v", msgs)
	}

	// The pending-tool set survives the attach.
	pending := h.loopManager.GetPendingTools(loopID)
	if len(pending) != 1 || pending[0] != "call-in-flight" {
		t.Fatalf("pending tools after attach = %v, want [call-in-flight]", pending)
	}

	// The observable half: the request published for the continuation carries
	// the accumulated conversation, not just the new turn.
	sent := requestMessages(t, second)
	if indexOfContent(sent, "first turn") < 0 || indexOfContent(sent, "assistant turn") < 0 {
		t.Fatalf("continuation request dropped the prior turns: %+v", sent)
	}
	if indexOfContent(sent, "second turn") < 0 {
		t.Fatalf("continuation request omits the new turn: %+v", sent)
	}
}

// TestContinuationDoesNotReseedSystemPrompt: attaching must not add a second
// system prompt. A re-seed is silent — the loop keeps working — and every
// later iteration then carries the persona twice.
func TestContinuationDoesNotReseedSystemPrompt(t *testing.T) {
	ctx := context.Background()
	h := fenceHandler(t)

	first, err := h.HandleTask(ctx, TaskMessage{
		TaskID: "task-1", Role: "general", Model: "model-a", Prompt: "first turn",
	})
	if err != nil {
		t.Fatalf("HandleTask (first): %v", err)
	}
	loopID := first.LoopID

	cm := h.loopManager.GetContextManager(loopID)
	if got := len(cm.regions[RegionSystemPrompt]); got != 1 {
		t.Fatalf("system-prompt region after create = %d entries, want 1 (fixture registry not wired?)", got)
	}

	if _, err = h.HandleTask(ctx, TaskMessage{
		TaskID: "task-2", LoopID: loopID, Role: "general", Model: "model-a", Prompt: "second turn",
	}); err != nil {
		t.Fatalf("HandleTask (continuation): %v", err)
	}

	// Read the loop's CURRENT manager, not the captured one: a create-over-token
	// would have swapped it, and asserting on the orphan would score a pass.
	cmAfter := h.loopManager.GetContextManager(loopID)
	if cmAfter != cm {
		t.Fatal("continuation replaced the context manager; the conversation was discarded")
	}
	if got := len(cmAfter.regions[RegionSystemPrompt]); got != 1 {
		t.Fatalf("system-prompt region after continuation = %d entries, want 1", got)
	}
	if got := countRole(cmAfter.GetContext(), "system"); got != 1 {
		t.Fatalf("system messages in the continued conversation = %d, want 1", got)
	}
	if got := countContent(cmAfter.GetContext(), "first turn"); got != 1 {
		t.Fatalf("prior user turn count = %d, want 1 — the conversation was reset", got)
	}
}

// TestContinuationOfTerminalLoopIsRefused: a settled loop cannot be advanced,
// and its token must not be recycled into a replacement. Refusing is what keeps
// the recorded outcome the answer for that token (ruling 5 on #1227).
func TestContinuationOfTerminalLoopIsRefused(t *testing.T) {
	ctx := context.Background()
	h := fenceHandler(t)

	first, err := h.HandleTask(ctx, TaskMessage{
		TaskID: "task-1", Role: "general", Model: "model-a", Prompt: "first turn",
	})
	if err != nil {
		t.Fatalf("HandleTask (first): %v", err)
	}
	loopID := first.LoopID

	if err := h.loopManager.TransitionLoop(loopID, agentic.LoopStateComplete); err != nil {
		t.Fatalf("TransitionLoop: %v", err)
	}
	if err := h.loopManager.UpdateCompletion(loopID, agentic.OutcomeSuccess, "the settled answer", ""); err != nil {
		t.Fatalf("UpdateCompletion: %v", err)
	}
	settled, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}

	result, err := h.HandleTask(ctx, TaskMessage{
		TaskID: "task-2", LoopID: loopID, Role: "general", Model: "model-a", Prompt: "second turn",
	})
	if err == nil {
		t.Fatalf("continuation of a settled loop accepted: %+v", result)
	}
	if !errors.Is(err, ErrLoopTerminal) {
		t.Fatalf("refusal = %v, want errors.Is ErrLoopTerminal", err)
	}
	if len(result.PublishedMessages) != 0 {
		t.Fatalf("refused continuation published %d messages", len(result.PublishedMessages))
	}

	after, err := h.loopManager.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop after refusal: %v", err)
	}
	if !reflect.DeepEqual(after, settled) {
		t.Fatalf("settled loop mutated by a refused continuation:\n before %+v\n after  %+v", settled, after)
	}
	if after.Result != "the settled answer" {
		t.Fatalf("settled outcome overwritten: %q", after.Result)
	}
}

// TestRedeliveredContinuationIsDeduplicated is task 6.4, which I6 does not
// cover. Intake dedupes on TaskID against non-terminal loops; if an attach left
// the previous turn's TaskID on the entity, a JetStream redelivery of the
// continuation would attach a second time and append the user's turn twice.
func TestRedeliveredContinuationIsDeduplicated(t *testing.T) {
	ctx := context.Background()
	h := fenceHandler(t)

	first, err := h.HandleTask(ctx, TaskMessage{
		TaskID: "task-1", Role: "general", Model: "model-a", Prompt: "first turn",
	})
	if err != nil {
		t.Fatalf("HandleTask (first): %v", err)
	}
	loopID := first.LoopID

	continuation := TaskMessage{
		TaskID: "task-2", LoopID: loopID, Role: "general", Model: "model-a", Prompt: "second turn",
	}
	if _, err = h.HandleTask(ctx, continuation); err != nil {
		t.Fatalf("HandleTask (continuation): %v", err)
	}

	cm := h.loopManager.GetContextManager(loopID)
	turnsAfterAttach := countContent(cm.GetContext(), "second turn")
	if turnsAfterAttach != 1 {
		t.Fatalf("user turns after attach = %d, want 1", turnsAfterAttach)
	}

	redelivered, err := h.HandleTask(ctx, continuation)
	if err != nil {
		t.Fatalf("HandleTask (redelivery): %v", err)
	}
	if redelivered.LoopID != loopID {
		t.Fatalf("redelivery loop id = %q, want %q", redelivered.LoopID, loopID)
	}
	if redelivered.Created {
		t.Fatal("redelivery reported Created; it must be recognised as a duplicate")
	}
	if len(redelivered.PublishedMessages) != 0 {
		t.Fatalf("redelivery published %d messages", len(redelivered.PublishedMessages))
	}
	if got := countContent(cm.GetContext(), "second turn"); got != 1 {
		t.Fatalf("user turns after redelivery = %d, want 1 — the attach was replayed", got)
	}
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
