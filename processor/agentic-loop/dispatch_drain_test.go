package agenticloop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
)

// requestOnSubject decodes the single agent.request a handler result published.
// Subject-filtered rather than indexed: the tools-complete result can publish
// other messages, and picking the wrong one would read as an empty request.
func requestOnSubject(t *testing.T, result HandlerResult) agentic.AgentRequest {
	t.Helper()
	var found *agentic.AgentRequest
	for _, msg := range result.PublishedMessages {
		if !strings.Contains(strings.ToLower(msg.Subject), "agent.request") {
			continue
		}
		var envelope struct {
			Payload agentic.AgentRequest `json:"payload"`
		}
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("decode agent request envelope: %v", err)
		}
		if found != nil {
			t.Fatalf("result published more than one agent.request: %q and %q",
				found.RequestID, envelope.Payload.RequestID)
		}
		request := envelope.Payload
		found = &request
	}
	if found == nil {
		t.Fatal("result published no agent.request")
	}
	return *found
}

// dispatchedCallOnSubject decodes the single tool.execute a handler result
// published — the call that claimed the in-flight slot, with the framework
// correlation an executor would answer with.
func dispatchedCallOnSubject(t *testing.T, result HandlerResult) agentic.ToolCall {
	t.Helper()
	var found *agentic.ToolCall
	for _, msg := range result.PublishedMessages {
		if !strings.Contains(strings.ToLower(msg.Subject), "tool.execute") {
			continue
		}
		var envelope struct {
			Payload agentic.ToolCall `json:"payload"`
		}
		if err := json.Unmarshal(msg.Data, &envelope); err != nil {
			t.Fatalf("decode tool call envelope: %v", err)
		}
		if found != nil {
			t.Fatalf("result dispatched more than one tool call: %q and %q", found.ID, envelope.Payload.ID)
		}
		call := envelope.Payload
		found = &call
	}
	if found == nil {
		t.Fatal("result dispatched no tool call")
	}
	return *found
}

// failEveryQueuedDispatch re-stamps every queued call with an argument
// json.Marshal rejects, so dispatchToolCall's marshal fails and
// tryDispatchOrSynthesize takes the synth-result path for each one — the same
// forced failure TestTryDispatchOrSynthesize_ForcedMarshalFailure uses, and one
// of the two modes dispatchToolCall documents.
//
// It is stamped on the QUEUED copies rather than on the response the model
// sent, because the assistant message held in the loop's context shares those
// argument maps: poisoning them would fail the request mint instead of the
// dispatch, which is a different defect. Identity and order are preserved, so
// the queue the drain walks is the one HandleModelResponse built.
func failEveryQueuedDispatch(t *testing.T, h *MessageHandler, loopID string) {
	t.Helper()
	var poisoned []agentic.ToolCall
	for {
		call, ok := h.loopManager.DequeueToolCall(loopID)
		if !ok {
			break
		}
		call.Arguments = map[string]any{"unmarshalable": make(chan int)}
		poisoned = append(poisoned, call)
	}
	if len(poisoned) == 0 {
		t.Fatal("setup: nothing was queued, so the drain under test would not run")
	}
	h.loopManager.QueueToolCalls(loopID, poisoned)
}

// The dispatch drain carries the same obligation as the skipped-queue drain:
// every call the assistant message advertised ends with a result, because one
// unanswered call invalidates the whole group rather than only itself. Its
// bound was len(GetPendingTools)+64 — a number taken from a different set than
// the one it drains, and by the time it runs the answered call has already left
// that set, so the real bound was the bare 64. A batch whose queued calls all
// fail to dispatch stopped there; the rest stayed queued, undispatched and
// unanswered, and the request handleToolsComplete then minted advertised calls
// nothing answered, so RepairToolPairs removed the assistant message and every
// result with it.
//
// Nothing caps a batch — agentic.AgentResponse validation imposes no limit —
// so the only honest bound is the queue's own length at entry. The sizes
// bracket the retired constant: at 65 calls the queue is 64 long and fits
// inside it, at 66 it does not.
//
// spec: agentic-loop / A logical model request has one deterministic identity
func TestEveryQueuedCallIsAnsweredWhenAllOfThemFailToDispatch(t *testing.T) {
	for _, batch := range []int{20, 65, 66, 70} {
		t.Run(fmt.Sprintf("%d_calls", batch), func(t *testing.T) {
			handler := NewMessageHandler(DefaultConfig())
			ctx := context.Background()

			birthResult, err := handler.HandleTask(ctx, TaskMessage{
				TaskID: "task-dispatch-drain",
				Role:   "general",
				Model:  "test-model",
				Prompt: "summarise the first thing",
			})
			if err != nil {
				t.Fatalf("HandleTask: %v", err)
			}
			loopID := birthResult.LoopID
			birth := requestOnSubject(t, birthResult).RequestID

			calls := make([]agentic.ToolCall, batch)
			for i := range calls {
				calls[i] = agentic.ToolCall{ID: fmt.Sprintf("call-%03d", i), Name: "test_tool"}
			}
			dispatchResult, err := handler.HandleModelResponse(ctx, loopID, agentic.AgentResponse{
				RequestID: birth,
				Status:    agentic.StatusToolCall,
				Message:   agentic.ChatMessage{Role: "assistant", ToolCalls: calls},
			})
			if err != nil {
				t.Fatalf("HandleModelResponse(%d tool_calls): %v", batch, err)
			}
			dispatched := dispatchedCallOnSubject(t, dispatchResult)
			if queued := handler.loopManager.QueuedToolCount(loopID); queued != batch-1 {
				t.Fatalf("setup: %d calls queued after the first dispatch, want %d", queued, batch-1)
			}

			failEveryQueuedDispatch(t, handler, loopID)

			// The one real result. Everything still queued has to be drained
			// and answered before the next request is minted.
			next, err := handler.HandleToolResult(ctx, loopID, agentic.ToolResult{
				CallID:      dispatched.ID,
				Name:        dispatched.Name,
				Content:     "the first thing is looked up",
				RequestID:   dispatched.RequestID,
				ExecutionID: dispatched.ExecutionID,
				CallOrdinal: dispatched.CallOrdinal,
			})
			if err != nil {
				t.Fatalf("HandleToolResult: %v", err)
			}

			if left := handler.loopManager.QueuedToolCount(loopID); left != 0 {
				t.Fatalf("%d of %d calls are still queued — undispatched and unanswered", left, batch-1)
			}

			var advertised, answered int
			for _, m := range requestOnSubject(t, next).Messages {
				advertised += len(m.ToolCalls)
				if m.Role == "tool" {
					answered++
				}
			}
			if advertised != batch {
				t.Fatalf("the next request advertises %d tool calls, want %d; "+
					"a group short one answer is repaired away whole", advertised, batch)
			}
			if answered != batch {
				t.Fatalf("the next request answers %d of %d advertised calls", answered, batch)
			}
		})
	}
}
