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
	"github.com/stretchr/testify/require"
)

// newCompletionTestComponent wires a Component for handleAgentComplete /
// handleAgentFailed unit tests with a response sink. Tests supply exact loop
// authority through withPersistedLoops; no process projection can supply a route.
func newCompletionTestComponent(t *testing.T) (*Component, *captureSink) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sink := &captureSink{}
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
	c := &Component{
		config:  DefaultConfig(),
		logger:  logger,
		metrics: getMetrics(nil),
		decoder: message.NewDecoder(reg),
	}
	c.sendResponseFn = sink.add
	c.sendTerminalResponseFn = func(_ context.Context, response agentic.UserResponse, _ string) error {
		sink.add(response)
		return nil
	}
	return c, sink
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

// spec: agentic-dispatch / Dispatch is exclusively an edge gateway
func TestHandleAgentCompleteDeliversResultWithoutAdvancingLoopAuthority(t *testing.T) {
	c, sink := newCompletionTestComponent(t)
	const loopID = admissionLoopA
	persisted := &agentic.LoopEntity{
		ID: loopID, TaskID: loopID + "-task", State: agentic.LoopStateExecuting,
		MaxIterations: 10, UserID: "alice", ChannelType: "http", ChannelID: "session-1",
	}
	before := *persisted
	withPersistedLoops(c, map[string]*agentic.LoopEntity{loopID: persisted})

	c.handleAgentComplete(context.Background(), completionPayload(t, &agentic.LoopCompletedEvent{
		LoopID:      loopID,
		TaskID:      loopID + "-task",
		Outcome:     agentic.OutcomeSuccess,
		Role:        "general",
		Result:      "the answer",
		CompletedAt: time.Now(),
	}))

	require.Equal(t, before, *persisted, "only agentic-loop advances loop authority")
	responses := sink.all()
	require.Len(t, responses, 1)
	require.Equal(t, agentic.ResponseTypeResult, responses[0].Type)
	require.Equal(t, "the answer", responses[0].Content)
	require.Equal(t, "alice", responses[0].UserID)
	require.Equal(t, "session-1", responses[0].ChannelID)
}

// spec: agentic-dispatch / Dispatch is exclusively an edge gateway
func TestHandleAgentCompleteDoesNotOverwritePersistedTerminalState(t *testing.T) {
	c, sink := newCompletionTestComponent(t)
	const loopID = admissionLoopB
	persisted := &agentic.LoopEntity{
		ID: loopID, TaskID: loopID + "-task", State: agentic.LoopStateComplete,
		MaxIterations: 10, UserID: "bob", ChannelType: "http", ChannelID: "session-2",
		Outcome: agentic.OutcomeSuccess, Result: "durable result", CompletedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	before := *persisted
	withPersistedLoops(c, map[string]*agentic.LoopEntity{loopID: persisted})

	c.handleAgentComplete(context.Background(), completionPayload(t, &agentic.LoopCompletedEvent{
		LoopID:      loopID,
		TaskID:      loopID + "-task",
		Outcome:     agentic.OutcomeSuccess,
		Result:      "done",
		CompletedAt: time.Now(),
	}))

	require.Equal(t, before, *persisted, "terminal delivery never rewrites current authority")
	responses := sink.all()
	require.Len(t, responses, 1)
	require.Equal(t, "done", responses[0].Content)
}

// spec: agentic-dispatch / Dispatch is exclusively an edge gateway
func TestHandleAgentFailedDeliversErrorWithoutAdvancingLoopAuthority(t *testing.T) {
	c, sink := newCompletionTestComponent(t)
	const loopID = admissionLoopA
	persisted := &agentic.LoopEntity{
		ID: loopID, TaskID: loopID + "-task", State: agentic.LoopStateExecuting,
		MaxIterations: 10, UserID: "carol", ChannelType: "http", ChannelID: "session-3",
	}
	before := *persisted
	withPersistedLoops(c, map[string]*agentic.LoopEntity{loopID: persisted})

	c.handleAgentFailed(context.Background(), failurePayload(t, &agentic.LoopFailedEvent{
		LoopID:   loopID,
		TaskID:   loopID + "-task",
		Outcome:  agentic.OutcomeFailed,
		Reason:   "max_iterations",
		Error:    "max iterations reached (10)",
		Role:     "general",
		FailedAt: time.Now(),
	}))

	require.Equal(t, before, *persisted, "failure delivery never advances loop authority")
	responses := sink.all()
	require.Len(t, responses, 1)
	require.Equal(t, agentic.ResponseTypeError, responses[0].Type)
	require.Contains(t, responses[0].Content, "max iterations reached (10)")
	require.Equal(t, "carol", responses[0].UserID)
	require.Equal(t, "session-3", responses[0].ChannelID)
}
