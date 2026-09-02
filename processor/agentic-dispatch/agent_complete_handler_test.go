package agenticdispatch

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
)

// newCompletionTestComponent wires a Component for handleAgentComplete /
// handleAgentFailed unit tests: logger, loopTracker, metrics, response
// sink. No NATS; no model registry. Mirrors newInterviewTestComponent in
// onboarding_interview_test.go but with metrics so the handlers don't
// nil-deref.
func newCompletionTestComponent(t *testing.T) (*Component, *captureSink) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sink := &captureSink{}
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
	c := &Component{
		logger:      logger,
		loopTracker: NewLoopTrackerWithLogger(logger),
		metrics:     getMetrics(nil),
		decoder:     message.NewDecoder(reg),
	}
	c.sendResponseFn = sink.add
	c.sendTerminalResponseFn = func(_ context.Context, response agentic.UserResponse, _ string) error {
		sink.add(response)
		return nil
	}
	c.loadPersistedLoopFn = func(_ context.Context, loopID string) (*agentic.LoopEntity, error) {
		info := c.loopTracker.getSnapshot(loopID)
		if info == nil {
			return nil, nil
		}
		return &agentic.LoopEntity{
			ID:            loopID,
			TaskID:        info.TaskID,
			State:         agentic.LoopStateExecuting,
			MaxIterations: info.MaxIterations,
			UserID:        info.UserID,
			ChannelType:   info.ChannelType,
			ChannelID:     info.ChannelID,
		}, nil
	}
	return c, sink
}

// trackedLoop seeds the tracker with a loop in the executing state,
// matching what dispatch produces for a routed user task.
func trackedLoop(c *Component, loopID, userID, channelID string) {
	c.loopTracker.Track(&LoopInfo{
		LoopID:        loopID,
		TaskID:        loopID + "-task",
		UserID:        userID,
		ChannelType:   "http",
		ChannelID:     channelID,
		State:         "executing",
		Iterations:    1,
		MaxIterations: 10,
		CreatedAt:     time.Now(),
	})
}

// completionPayload builds the BaseMessage envelope that handleAgentComplete
// expects to find on the wire.
func completionPayload(t *testing.T, ev *agentic.LoopCompletedEvent) []byte {
	t.Helper()
	msg := message.NewBaseMessage(ev.Schema(), ev, "test")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal completion: %v", err)
	}
	return data
}

func failurePayload(t *testing.T, ev *agentic.LoopFailedEvent) []byte {
	t.Helper()
	msg := message.NewBaseMessage(ev.Schema(), ev, "test")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failure: %v", err)
	}
	return data
}

// TestHandleAgentComplete_SetsTerminalStateAndCompletionData is the
// regression test for the bug at component.go:691 where handleAgentComplete
// wrote completion.Outcome ("success") into LoopInfo.State, leaving
// state="success" — not a valid terminal state. After the fix, the loop
// routes through UpdateCompletion which translates outcome → state via
// outcomeToState.
func TestHandleAgentComplete_SetsTerminalStateAndCompletionData(t *testing.T) {
	c, _ := newCompletionTestComponent(t)
	const loopID = "loop-success-1"
	trackedLoop(c, loopID, "alice", "session-1")

	c.handleAgentComplete(context.Background(), completionPayload(t, &agentic.LoopCompletedEvent{
		LoopID:      loopID,
		TaskID:      loopID + "-task",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "general",
		Result:      "the answer",
		CompletedAt: time.Now(),
	}))

	info := c.loopTracker.Get(loopID)
	if info == nil {
		t.Fatal("loop info missing after completion")
	}
	if info.State != "complete" {
		t.Errorf("State after success completion = %q, want %q", info.State, "complete")
	}
	if info.Outcome != agentic.OutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", info.Outcome, agentic.OutcomeSuccess)
	}
	if info.Result != "the answer" {
		t.Errorf("Result = %q, want %q", info.Result, "the answer")
	}
	if info.CompletedAt.IsZero() {
		t.Error("CompletedAt not populated")
	}
	if !isTerminalState(info.State) {
		t.Errorf("isTerminalState(%q) = false, want true", info.State)
	}
}

// TestHandleAgentComplete_GetActiveLoopReturnsEmptyAfterSuccess is the
// user-visible-impact regression: pre-fix, isTerminalState("success")
// returned false, so a successful loop kept showing up via GetActiveLoop
// and the user's next message would be routed to the stale "active" loop
// instead of starting a new one.
func TestHandleAgentComplete_GetActiveLoopReturnsEmptyAfterSuccess(t *testing.T) {
	c, _ := newCompletionTestComponent(t)
	const loopID = "loop-success-2"
	trackedLoop(c, loopID, "bob", "session-2")

	if got := c.loopTracker.GetActiveLoop("bob", "session-2"); got != loopID {
		t.Fatalf("pre-completion GetActiveLoop = %q, want %q (sanity)", got, loopID)
	}

	c.handleAgentComplete(context.Background(), completionPayload(t, &agentic.LoopCompletedEvent{
		LoopID:      loopID,
		TaskID:      loopID + "-task",
		Outcome:     agentic.OutcomeSuccess,
		Result:      "done",
		CompletedAt: time.Now(),
	}))

	if got := c.loopTracker.GetActiveLoop("bob", "session-2"); got != "" {
		t.Errorf("post-success GetActiveLoop = %q, want empty (loop is terminal)", got)
	}
}

// TestHandleAgentFailed_SetsTerminalStateAndError is the regression test
// for the sibling site at component.go:796 where handleAgentFailed
// hardcoded UpdateState(loopID, "failed"). That was coincidentally a
// valid state but discarded failure.Error and CompletedAt. After the
// fix, UpdateCompletion records the full failure shape.
func TestHandleAgentFailed_SetsTerminalStateAndError(t *testing.T) {
	c, _ := newCompletionTestComponent(t)
	const loopID = "loop-fail-1"
	trackedLoop(c, loopID, "carol", "session-3")

	c.handleAgentFailed(context.Background(), failurePayload(t, &agentic.LoopFailedEvent{
		LoopID:   loopID,
		TaskID:   loopID + "-task",
		Outcome:  agentic.OutcomeFailed,
		Reason:   "max_iterations",
		Error:    "max iterations reached (10)",
		Role:     "general",
		FailedAt: time.Now(),
	}))

	info := c.loopTracker.Get(loopID)
	if info == nil {
		t.Fatal("loop info missing after failure")
	}
	if info.State != "failed" {
		t.Errorf("State after failure = %q, want %q", info.State, "failed")
	}
	if info.Outcome != agentic.OutcomeFailed {
		t.Errorf("Outcome = %q, want %q", info.Outcome, agentic.OutcomeFailed)
	}
	if info.Error != "max iterations reached (10)" {
		t.Errorf("Error = %q, want recorded failure error", info.Error)
	}
	if info.CompletedAt.IsZero() {
		t.Error("CompletedAt not populated on failure path")
	}
	if !isTerminalState(info.State) {
		t.Errorf("isTerminalState(%q) = false, want true", info.State)
	}
}
