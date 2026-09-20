package agenticloop_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
)

// continuationPrompt is long enough to be unmistakable inside a request body.
const continuationPrompt = "and also summarise the second thing"

// secondContinuationPrompt is the turn typed while the request carrying
// continuationPrompt is itself still in flight. Shares no substring with it.
const secondContinuationPrompt = "one more question about the third thing"

// requestBodyContains reports whether any agent.request in the result carries a
// message with the given content.
func requestBodyContains(t *testing.T, result agenticloop.HandlerResult, content string) bool {
	t.Helper()
	for _, msg := range result.PublishedMessages {
		if !strings.Contains(strings.ToLower(msg.Subject), "agent.request") {
			continue
		}
		var envelope struct {
			Payload agentic.AgentRequest `json:"payload"`
		}
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("decode agent.request on %s: %v", msg.Subject, err)
		}
		for _, m := range envelope.Payload.Messages {
			if strings.Contains(m.Content, content) {
				return true
			}
		}
	}
	return false
}

// startLoopAndAdmitContinuation births a loop, leaves its first request
// outstanding, and admits a second task naming the same loop. Returns the loop
// ID, the birth RequestID, and the continuation's handler result.
func startLoopAndAdmitContinuation(t *testing.T, handler *agenticloop.MessageHandler) (string, string, agenticloop.HandlerResult) {
	t.Helper()
	ctx := context.Background()

	birthResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-defer-1",
		Role:   "general",
		Model:  "test-model",
		Prompt: "summarise the first thing",
	})
	if err != nil {
		t.Fatalf("HandleTask (birth): %v", err)
	}
	loopID := birthResult.LoopID
	birth := oneMintedRequestID(t, "birth", birthResult)

	// No model response has arrived, so the loop is waiting on `birth`.
	continuation, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-defer-2",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: continuationPrompt,
	})
	if err != nil {
		t.Fatalf("HandleTask (continuation): %v", err)
	}
	return loopID, birth, continuation
}

// A continuation admitted while the loop's model request is still outstanding
// must publish NOTHING. The request it would mint carries this iteration's
// name — `<loopID>:req:<iteration>:<retry>` does not move until the loop
// advances — so it would go out with the outstanding request's RequestID and
// Nats-Msg-Id but different bytes, and the stream's duplicate window would drop
// it. The turn is not refused either: refusing throws away a user's message for
// typing while the agent was thinking.
//
// Asserted on the published messages rather than on the marker, because the
// marker is the mechanism and "no second request goes out" is the obligation.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestContinuationBehindAnOutstandingRequestPublishesNothing(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))

	_, birth, continuation := startLoopAndAdmitContinuation(t, handler)

	if ids := mintedRequestIDs(t, continuation); len(ids) != 0 {
		t.Fatalf("the deferred continuation published %d agent.request messages (%v); the first would reuse %q",
			len(ids), ids, birth)
	}
	if len(continuation.PublishedMessages) != 0 {
		t.Fatalf("the deferred continuation published %d messages, want none", len(continuation.PublishedMessages))
	}
	if !continuation.Deferred {
		t.Fatal("the continuation result is not marked Deferred; the delivery cannot tell it apart from a dedup")
	}
}

// The half the previous behaviour got wrong. `design.md` claimed a deferred
// turn "rides the next iteration's request", which is true on a tool-call
// response and FALSE on a completion: the loop completes and there is no next
// request. The completion response must carry the turn instead of settling.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestDeferredContinuationIsCarriedByTheCompletionResponse(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))
	ctx := context.Background()

	loopID, birth, _ := startLoopAndAdmitContinuation(t, handler)

	completion, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: birth,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "the first thing is done"},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(complete): %v", err)
	}

	if completion.State.IsTerminal() {
		t.Fatalf("the loop completed with a turn admitted and never sent; state=%s", completion.State)
	}
	if completion.CompletionState != nil {
		t.Fatal("a completion record was built for a loop that has an unanswered turn")
	}

	next := oneMintedRequestID(t, "deferred carry", completion)
	if want := loopID + ":req:2:0"; next != want {
		t.Fatalf("carried RequestID = %q, want %q", next, want)
	}
	if next == birth {
		t.Fatalf("the carried request reused the outstanding request's name %q", birth)
	}
	if !requestBodyContains(t, completion, continuationPrompt) {
		t.Fatalf("the carried request does not contain the continuation's turn %q", continuationPrompt)
	}

	// The marker stays set and names its carrier. It is persisted before the
	// publish that sends this request, so clearing it here would durably say
	// "nothing deferred" about a send whose durability is still unknown; what
	// stops a second carry is the carrier being recorded, not the marker being
	// gone.
	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if entity.PendingContinuationRequestID != next {
		t.Fatalf("carrier = %q, want the request that carries the turn %q",
			entity.PendingContinuationRequestID, next)
	}
	if handler.HasPendingContinuationForTest(loopID) {
		t.Fatal("the loop still reads as uncarried; the next completion would carry the same turn again")
	}
}

// The tool-call path was already sound — the turn rides iteration N+1 — but
// nothing pinned that it clears the marker. Without the clear, the NEXT
// completion on this loop would defer again forever against a turn that was
// already sent.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestDeferredContinuationRidesTheToolCallPath(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))
	ctx := context.Background()

	loopID, birth, _ := startLoopAndAdmitContinuation(t, handler)

	dispatchResult, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: birth,
		Status:    agentic.StatusToolCall,
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-defer-1", Name: "test_tool"}},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(tool_call): %v", err)
	}
	if ids := mintedRequestIDs(t, dispatchResult); len(ids) != 0 {
		t.Fatalf("a tool-call response minted %d requests (%v); the tools have not answered yet", len(ids), ids)
	}

	dispatched := dispatchedToolCallFromResult(t, dispatchResult)
	toolsComplete, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:      dispatched.ID,
		Name:        dispatched.Name,
		Content:     "tool answered",
		RequestID:   dispatched.RequestID,
		ExecutionID: dispatched.ExecutionID,
		CallOrdinal: dispatched.CallOrdinal,
	})
	if err != nil {
		t.Fatalf("HandleToolResult: %v", err)
	}

	next := oneMintedRequestID(t, "tools complete", toolsComplete)
	if want := loopID + ":req:2:0"; next != want {
		t.Fatalf("tools-complete RequestID = %q, want %q", next, want)
	}
	if !requestBodyContains(t, toolsComplete, continuationPrompt) {
		t.Fatalf("the tools-complete request does not contain the continuation's turn %q", continuationPrompt)
	}

	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if entity.PendingContinuationRequestID != next {
		t.Fatalf("carrier = %q, want the tools-complete request %q",
			entity.PendingContinuationRequestID, next)
	}
	if handler.HasPendingContinuationForTest(loopID) {
		t.Fatal("the loop still reads as uncarried; the next completion would carry the same turn again")
	}
}

// The third request-building site. A length-truncated response compacts and
// re-asks the model at the SAME iteration, and it builds from the same context
// the deferred turn was written into — so the retry carries the turn exactly as
// the iteration request does. When the retry did not end the deferral, the
// completion that answered it deferred a turn that had already been sent and
// spent an iteration re-asking with a context that had gained nothing.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestTruncationRetryCarriesTheDeferredTurn(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))
	ctx := context.Background()

	birthResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-truncation-carry",
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: "summarise the first thing",
	})
	if err != nil {
		t.Fatalf("HandleTask (birth): %v", err)
	}
	loopID := birthResult.LoopID
	birth := oneMintedRequestID(t, "birth", birthResult)

	// Above the compaction threshold, so the truncation self-heals and retries
	// rather than failing fast.
	fillContextToHighUtilization(t, handler, loopID, 80000)

	continuation, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-truncation-carry-2",
		LoopID: loopID,
		Role:   "general",
		Model:  "qwen-32b",
		Prompt: continuationPrompt,
	})
	if err != nil {
		t.Fatalf("HandleTask (continuation): %v", err)
	}
	if !continuation.Deferred {
		t.Fatalf("the continuation was not deferred; state=%s", continuation.State)
	}

	retry, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID:    birth,
		Status:       agentic.StatusLengthTruncated,
		FinishReason: agentic.FinishReasonLength,
		Message:      agentic.ChatMessage{Role: "assistant", Content: "half an answer"},
		TokenUsage:   agentic.TokenUsage{PromptTokens: 50, CompletionTokens: 4096},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(length_truncated): %v", err)
	}
	if retry.State == agentic.LoopStateFailed {
		t.Fatalf("the truncation failed the loop instead of retrying; state=%s", retry.State)
	}
	retryID := oneMintedRequestID(t, "truncation retry", retry)
	if retryID == birth {
		t.Fatalf("the retry reused the truncated request's name %q", birth)
	}
	// The turn is in the request either as the message the deferral wrote or,
	// when compaction at this utilization empties the context, as the prompt
	// recoverEmptyContext re-seeds from — CacheTaskPrompt (handlers.go:974) runs
	// on the continuation too, so the recovered prompt is this turn and not the
	// birth task's. Both are "the model has been asked", which is what makes
	// ending the deferral correct here rather than a dropped turn.
	if !requestBodyContains(t, retry, continuationPrompt) {
		t.Fatalf("the retry request does not contain the continuation's turn %q", continuationPrompt)
	}

	// The turn has gone out. The completion that answers the retry must settle
	// the loop, not defer a turn the model has already been asked.
	completion, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: retryID,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "both things are done"},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(complete): %v", err)
	}
	if ids := mintedRequestIDs(t, completion); len(ids) != 0 {
		t.Fatalf("the completion spent another iteration re-asking with the already-sent turn: minted %v", ids)
	}
	if !completion.State.IsTerminal() {
		t.Fatalf("the loop did not complete after its deferred turn was answered; state=%s", completion.State)
	}
	if completion.CompletionState == nil {
		t.Fatal("no completion record was built for a loop with nothing left deferred")
	}
}

// Two turns in a row, each typed while the previous one's request was still in
// flight. The carrier is what makes this decidable, and it is the half a single
// deferral cannot pin: recording a carrier stops a turn being carried twice,
// and the record names "the request that carries THIS turn". A turn admitted
// now is in no request yet — not in the outstanding one, whose bytes were built
// before it existed — so the record it lands on names no carrier, and the next
// completion has a turn to carry rather than a loop to settle.
//
// Without that reset the second turn inherits the first turn's carrier, the
// response for that request matches it, SettleRequest clears the deferral as
// though the turn had been sent, and the loop completes with the second turn
// sitting in its context, never asked. That is the lost turn the deferral
// exists to prevent, arriving one admission later.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestASecondContinuationUncarriesTheDeferralAndSendsBothTurns(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))
	ctx := context.Background()

	loopID, birth, _ := startLoopAndAdmitContinuation(t, handler)

	// The birth response carries turn one into iteration 2 and records that
	// request as the carrier.
	firstCarry, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: birth,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "the first thing is done"},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(complete birth): %v", err)
	}
	carrier := oneMintedRequestID(t, "first carry", firstCarry)
	if want := loopID + ":req:2:0"; carrier != want {
		t.Fatalf("first carry RequestID = %q, want %q", carrier, want)
	}
	if !requestBodyContains(t, firstCarry, continuationPrompt) {
		t.Fatalf("the carrying request does not contain turn one %q", continuationPrompt)
	}
	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop (after first carry): %v", err)
	}
	if entity.PendingContinuationRequestID != carrier {
		t.Fatalf("carrier = %q, want the request carrying turn one %q",
			entity.PendingContinuationRequestID, carrier)
	}

	// Someone types again while THAT request is in flight.
	second, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-defer-3",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: secondContinuationPrompt,
	})
	if err != nil {
		t.Fatalf("HandleTask (second continuation): %v", err)
	}
	if !second.Deferred {
		t.Fatalf("the second continuation was not deferred; state=%s", second.State)
	}
	if len(second.PublishedMessages) != 0 {
		t.Fatalf("the second continuation published %d messages while %q was outstanding",
			len(second.PublishedMessages), carrier)
	}

	entity, err = handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop (after second continuation): %v", err)
	}
	if entity.PendingContinuationRequestID != "" {
		t.Fatalf("the record still names %q as the carrier of a turn minted after it; "+
			"that request's response would end the deferral and turn two would never be sent",
			entity.PendingContinuationRequestID)
	}
	if !handler.HasPendingContinuationForTest(loopID) {
		t.Fatal("the loop reads as carried with a turn nothing has carried")
	}

	// The carrier's own completion must now carry turn two rather than settle.
	secondCarry, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: carrier,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "the second thing is done"},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(complete carrier): %v", err)
	}
	if secondCarry.State.IsTerminal() {
		t.Fatalf("the loop settled with turn two admitted and never asked; state=%s", secondCarry.State)
	}
	if secondCarry.CompletionState != nil {
		t.Fatal("a completion record was built for a loop that still owes a turn")
	}
	next := oneMintedRequestID(t, "second carry", secondCarry)
	if want := loopID + ":req:3:0"; next != want {
		t.Fatalf("second carry RequestID = %q, want %q", next, want)
	}
	if !requestBodyContains(t, secondCarry, secondContinuationPrompt) {
		t.Fatalf("the carrying request does not contain turn two %q", secondContinuationPrompt)
	}
	// Turn one is not lost to turn two: it went out in `carrier` above and is
	// still in the conversation this request sends.
	if !requestBodyContains(t, secondCarry, continuationPrompt) {
		t.Fatalf("turn one %q is missing from the conversation the carrying request sends", continuationPrompt)
	}

	entity, err = handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop (after second carry): %v", err)
	}
	if entity.PendingContinuationRequestID != next {
		t.Fatalf("carrier = %q, want the request carrying turn two %q",
			entity.PendingContinuationRequestID, next)
	}
	if handler.HasPendingContinuationForTest(loopID) {
		t.Fatal("the loop still reads as uncarried; the next completion would carry turn two again")
	}
}

// The fourth path a completion can arrive on, and the one that was still
// silently settling. A terminal tool ends a loop exactly as terminal model text
// does — the framework's own `decide` executor returns StopLoop: true — so a
// turn admitted while this iteration's request was outstanding has to be
// carried here too. Before this, the StopLoop arm ran the completion path with
// no pending check at all: the review observed `state=complete
// pendingContinuation=true carrier=""`, a loop settled with the user's turn
// still marked as waiting and no request that had ever contained it.
//
// The carried request has to send the terminal tool's own message as well.
// Only the all-tools-complete path drained accumulated results into the
// conversation, because the completing path never needed to; carrying without
// that drain would put an assistant tool_call into the request with no tool
// message answering it, and RepairToolPairs would drop the call the model just
// made rather than send a broken pair.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestDeferredContinuationIsCarriedByATerminalTool(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))
	ctx := context.Background()

	loopID, birth, _ := startLoopAndAdmitContinuation(t, handler)

	dispatchResult, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: birth,
		Status:    agentic.StatusToolCall,
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-terminal-1", Name: "test_tool"}},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(tool_call): %v", err)
	}
	dispatched := dispatchedToolCallFromResult(t, dispatchResult)

	const terminalContent = "the first thing is decided"
	terminal, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:      dispatched.ID,
		Name:        dispatched.Name,
		Content:     terminalContent,
		RequestID:   dispatched.RequestID,
		ExecutionID: dispatched.ExecutionID,
		CallOrdinal: dispatched.CallOrdinal,
		StopLoop:    true,
	})
	if err != nil {
		t.Fatalf("HandleToolResult(StopLoop): %v", err)
	}

	if terminal.State.IsTerminal() {
		t.Fatalf("the terminal tool settled a loop with an admitted turn; state=%s", terminal.State)
	}
	if terminal.CompletionState != nil {
		t.Fatal("a completion record was built for a loop that still owes a turn")
	}
	for _, msg := range terminal.PublishedMessages {
		if strings.Contains(strings.ToLower(msg.Subject), "agent.complete") {
			t.Fatalf("the terminal tool published %s with the admitted turn unanswered", msg.Subject)
		}
	}

	next := oneMintedRequestID(t, "terminal carry", terminal)
	if want := loopID + ":req:2:0"; next != want {
		t.Fatalf("carried RequestID = %q, want %q", next, want)
	}
	if !requestBodyContains(t, terminal, continuationPrompt) {
		t.Fatalf("the carried request does not contain the admitted turn %q", continuationPrompt)
	}
	if !requestBodyContains(t, terminal, terminalContent) {
		t.Fatalf("the carried request does not carry the terminal tool's own result %q; "+
			"its assistant tool_call travels unpaired", terminalContent)
	}

	entity, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop: %v", err)
	}
	if entity.PendingContinuationRequestID != next {
		t.Fatalf("carrier = %q, want the request carrying the turn %q",
			entity.PendingContinuationRequestID, next)
	}
	if handler.HasPendingContinuationForTest(loopID) {
		t.Fatal("the loop still reads as uncarried; the next completion would carry the same turn again")
	}

	// Nothing is deferred now, so the answer to the carried request settles the
	// loop — the terminal tool delayed the completion, it did not cancel it.
	completion, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: next,
		Status:    agentic.StatusComplete,
		Message:   agentic.ChatMessage{Role: "assistant", Content: "both things are done"},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(complete carry): %v", err)
	}
	if !completion.State.IsTerminal() {
		t.Fatalf("the loop did not complete once its deferred turn was answered; state=%s", completion.State)
	}
}

// A terminal tool answering a loop that is already on its last iteration takes
// the one branch in carryDeferredContinuation that cannot carry: there is no
// iteration left to spend, so the loop completes with the terminal tool's
// result — and the turn it owes STAYS on the durable record.
//
// Owner ruling, 2026-09-20 (on #1328): a turn admitted at the ceiling is kept,
// not cleared. The completed record carries PendingContinuation with an empty
// carrier, which is the same shape the quarantined carry already relies on — a
// turn that no request ever contained remains a fact something can recover,
// rather than a Warn in a log. The Warn stays as the operator signal.
//
// Until 90226c38 this branch had one caller that could not reach it
// (HandleModelResponse fails the delivery on the same predicate first); the
// terminal-tool carry is the caller that reaches it, so it is a production
// shape and this is what watches it.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestATerminalToolAtTheIterationCeilingKeepsTheDeferredTurnOnTheRecord(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))
	var logs bytes.Buffer
	handler.SetLogger(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	ctx := context.Background()

	// Reaching the ceiling branch takes the loop THROUGH a successful carry
	// first: the iteration counter only moves where a request is minted for the
	// next iteration (handleToolsComplete and the carry itself), and
	// HandleModelResponse fails any response that arrives at the ceiling, so no
	// tool can be dispatched from a ceiling iteration. What reaches it is a
	// REDELIVERED terminal tool result — at-least-once is the tool lane's
	// contract, HandleToolResult has no request-identity guard, and
	// RemovePendingTool tolerates a call that is already gone — landing on a
	// loop that has since been given another turn. A per-spawn budget narrows
	// the component ceiling (gh#528) so one carry is enough to exhaust it.
	lastIteration := 1
	birthResult, err := handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID:        "task-ceiling-1",
		Role:          "general",
		Model:         "test-model",
		Prompt:        "summarise the first thing",
		MaxIterations: &lastIteration,
	})
	if err != nil {
		t.Fatalf("HandleTask (birth): %v", err)
	}
	loopID := birthResult.LoopID
	birth := oneMintedRequestID(t, "birth", birthResult)

	if _, err = handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-ceiling-2",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: continuationPrompt,
	}); err != nil {
		t.Fatalf("HandleTask (first continuation): %v", err)
	}

	dispatchResult, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: birth,
		Status:    agentic.StatusToolCall,
		Message: agentic.ChatMessage{
			Role:      "assistant",
			ToolCalls: []agentic.ToolCall{{ID: "call-ceiling-1", Name: "test_tool"}},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(tool_call): %v", err)
	}
	dispatched := dispatchedToolCallFromResult(t, dispatchResult)

	const terminalContent = "the first thing is decided"
	terminalResult := agentic.ToolResult{
		CallID:      dispatched.ID,
		Name:        dispatched.Name,
		Content:     terminalContent,
		RequestID:   dispatched.RequestID,
		ExecutionID: dispatched.ExecutionID,
		CallOrdinal: dispatched.CallOrdinal,
		StopLoop:    true,
	}

	// First delivery: the turn IS carried, and that carry spends the loop's
	// last iteration.
	carried, err := handler.HandleToolResult(ctx, loopID, terminalResult)
	if err != nil {
		t.Fatalf("HandleToolResult(StopLoop, first delivery): %v", err)
	}
	if carried.State.IsTerminal() {
		t.Fatalf("the first delivery settled a loop with an admitted turn; state=%s", carried.State)
	}
	oneMintedRequestID(t, "first carry", carried)

	// A second turn lands while the carried request is outstanding.
	if _, err = handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-ceiling-3",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: secondContinuationPrompt,
	}); err != nil {
		t.Fatalf("HandleTask (second continuation): %v", err)
	}

	// The precondition IS the subject: if the fixture stops putting the loop on
	// its ceiling with a turn owed, every assertion below passes for the wrong
	// reason.
	atCeiling, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop (before the redelivery): %v", err)
	}
	if atCeiling.Iterations < atCeiling.MaxIterations {
		t.Fatalf("the loop is not at its ceiling: iterations=%d max=%d; the drop branch is unreachable",
			atCeiling.Iterations, atCeiling.MaxIterations)
	}
	if !handler.HasPendingContinuationForTest(loopID) {
		t.Fatal("no turn is deferred, so nothing can be dropped")
	}

	// The redelivery: same bytes, a loop that has moved on and has no iteration
	// left to spend on the turn it now owes.
	terminal, err := handler.HandleToolResult(ctx, loopID, terminalResult)
	if err != nil {
		t.Fatalf("HandleToolResult(StopLoop, redelivered at the ceiling): %v", err)
	}

	if !terminal.State.IsTerminal() {
		t.Fatalf("the loop did not complete when its deferred turn could not be carried; state=%s", terminal.State)
	}
	if ids := mintedRequestIDs(t, terminal); len(ids) != 0 {
		t.Fatalf("a request was minted past the iteration ceiling: %v", ids)
	}

	// The Warn is the operator signal, and it has to say the turn was not
	// carried — an operator reading "completed" alone would not look.
	if !strings.Contains(logs.String(), "deferred continuation not carried") {
		t.Fatalf("the uncarried turn was not WARNed; logs=%q", logs.String())
	}
	if !strings.Contains(logs.String(), loopID) {
		t.Fatalf("the Warn does not name the loop it happened on; logs=%q", logs.String())
	}

	// The ruling: kept, not cleared. The completed record is the only place the
	// turn still exists, with an empty carrier because no request ever held it.
	settled, err := handler.GetLoop(loopID)
	if err != nil {
		t.Fatalf("GetLoop (after completion): %v", err)
	}
	if !settled.PendingContinuation {
		t.Fatal("the completed record dropped the turn it never answered; " +
			"nothing but the log line records that a user turn was lost")
	}
	if settled.PendingContinuationRequestID != "" {
		t.Fatalf("carrier = %q, want empty: no request ever carried this turn",
			settled.PendingContinuationRequestID)
	}

	// Kept, but inert: a new turn cannot restart the settled loop off that
	// flag. attachContinuation refuses a terminal loop before any deferral
	// bookkeeping runs.
	if _, err = handler.HandleTask(ctx, agenticloop.TaskMessage{
		TaskID: "task-ceiling-4",
		LoopID: loopID,
		Role:   "general",
		Model:  "test-model",
		Prompt: "a third thing, after the loop settled",
	}); err == nil {
		t.Fatal("a turn was admitted to a completed loop; the retained marker must not resurrect it")
	}
}

// carriedMessages decodes the conversation of the one agent.request the result
// published. The carry's whole job is what the next request SAYS, so the
// assertions below read the messages rather than the marker.
func carriedMessages(t *testing.T, result agenticloop.HandlerResult) []agentic.ChatMessage {
	t.Helper()
	var out []agentic.ChatMessage
	found := 0
	for _, msg := range result.PublishedMessages {
		if !strings.Contains(strings.ToLower(msg.Subject), "agent.request") {
			continue
		}
		var envelope struct {
			Payload agentic.AgentRequest `json:"payload"`
		}
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("decode agent.request on %s: %v", msg.Subject, err)
		}
		out = envelope.Payload.Messages
		found++
	}
	if found != 1 {
		t.Fatalf("expected exactly one agent.request, got %d", found)
	}
	return out
}

// Serial dispatch sends the first tool call of an assistant batch and QUEUES
// its siblings. When the first one comes back terminal, the queue is discarded
// — and on the carry path that left the batch incomplete: the assistant message
// still advertises a call with no result, so RepairToolPairs removed the whole
// group on the way out, taking the real terminal result with it. The carried
// request went to the model with system and user messages only, so the turn
// that asked the agent to explain its decision arrived without the decision.
//
// The fix gives every discarded sibling a correlated synthetic result first, so
// the batch the model sees is complete and honest: the terminal tool's own
// result, and a skipped marker for the call that never ran.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestATerminalToolCarriesItsOwnResultWhenTheBatchHasQueuedSiblings(t *testing.T) {
	handler := agenticloop.NewMessageHandler(createTestConfig())
	handler.SetToolRegistry(newTestToolRegistry(t))
	ctx := context.Background()

	loopID, birth, _ := startLoopAndAdmitContinuation(t, handler)

	const terminalCallID = "call-terminal-1"
	const siblingCallID = "call-sibling-1"
	dispatchResult, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
		RequestID: birth,
		Status:    agentic.StatusToolCall,
		Message: agentic.ChatMessage{
			Role: "assistant",
			ToolCalls: []agentic.ToolCall{
				{ID: terminalCallID, Name: "test_tool"},
				{ID: siblingCallID, Name: "test_tool"},
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleModelResponse(two tool_calls): %v", err)
	}
	dispatched := dispatchedToolCallFromResult(t, dispatchResult)
	if dispatched.ID != terminalCallID {
		t.Fatalf("serial dispatch sent %q first, want %q; the sibling must be the QUEUED one",
			dispatched.ID, terminalCallID)
	}

	const terminalContent = "the first thing is decided"
	terminal, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
		CallID:      dispatched.ID,
		Name:        dispatched.Name,
		Content:     terminalContent,
		RequestID:   dispatched.RequestID,
		ExecutionID: dispatched.ExecutionID,
		CallOrdinal: dispatched.CallOrdinal,
		StopLoop:    true,
	})
	if err != nil {
		t.Fatalf("HandleToolResult(StopLoop with a queued sibling): %v", err)
	}
	if terminal.State.IsTerminal() {
		t.Fatalf("the terminal tool settled a loop with an admitted turn; state=%s", terminal.State)
	}

	messages := carriedMessages(t, terminal)
	var assistant *agentic.ChatMessage
	results := map[string]agentic.ChatMessage{}
	sawTurn := false
	for i := range messages {
		m := messages[i]
		switch {
		case len(m.ToolCalls) > 0:
			assistant = &messages[i]
		case m.Role == "tool":
			results[m.ToolCallID] = m
		case m.Role == "user" && strings.Contains(m.Content, continuationPrompt):
			sawTurn = true
		}
	}

	if assistant == nil {
		t.Fatalf("the carried request lost the assistant tool_call message entirely; "+
			"RepairToolPairs removed the batch and the model cannot see what it decided. messages=%d",
			len(messages))
	}
	if len(assistant.ToolCalls) != 2 {
		t.Fatalf("the assistant message carries %d tool calls, want 2", len(assistant.ToolCalls))
	}
	terminalMsg, ok := results[terminalCallID]
	if !ok {
		t.Fatalf("the carried request lost the terminal tool's own result; results=%v", results)
	}
	if !strings.Contains(terminalMsg.Content, terminalContent) {
		t.Fatalf("the terminal result says %q, want it to carry %q", terminalMsg.Content, terminalContent)
	}
	siblingMsg, ok := results[siblingCallID]
	if !ok {
		t.Fatalf("the queued sibling has no result, so the batch is incomplete and the next "+
			"RepairToolPairs will drop the group; results=%v", results)
	}
	if siblingMsg.Content == "" {
		t.Fatal("the sibling's synthetic result carries no diagnostic for the model to read")
	}
	if !sawTurn {
		t.Fatalf("the carried request does not contain the admitted turn %q", continuationPrompt)
	}
}
