package agentic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/test/e2e/harness/processbarrier"
	"github.com/c360studio/semstreams/test/e2e/mock"
	"github.com/nats-io/nats.go/jetstream"
)

// spec: agentic-loop / Tool execution has stable framework correlation
func TestStageABarrierCapturePreservesLoopProducedIdentity(t *testing.T) {
	call := agentic.ToolCall{
		ID: "provider-call", Name: processbarrier.ToolName,
		LoopID: "dce0441f-79c0-4e72-aae5-6144b7d8ce39", TraceID: "trace",
		RequestID: "request", ExecutionID: "opaque-loop-produced-execution", CallOrdinal: 1,
	}
	wire, err := json.Marshal(message.NewBaseMessage(call.Schema(), &call, "agentic-loop"))
	if err != nil {
		t.Fatal(err)
	}
	stream := replayFixtureStream{
		wantSubject: "tool.execute." + processbarrier.ToolName,
		stored:      &jetstream.RawStreamMsg{Data: wire, Sequence: 11},
	}
	s := &Scenario{decoder: payloadbuiltins.NewTestDecoder(t), config: DefaultConfig()}
	got, err := s.awaitReplayToolCall(t.Context(), stream, call.LoopID, processbarrier.ToolName, 10)
	if err != nil || got.ID != call.ID || got.RequestID != call.RequestID ||
		got.ExecutionID != call.ExecutionID || got.CallOrdinal != call.CallOrdinal {
		t.Fatalf("barrier capture = %#v, %v; want unchanged loop-produced identity", got, err)
	}
}

// spec: agentic-tools / Tool outcomes preserve framework execution correlation
// spec: agentic-tools / Completed tool outcome identity is globally unambiguous
func TestStageAToolResultRequiresFullExecutionCorrelation(t *testing.T) {
	call := agentic.ToolCall{
		ID: "provider-call", Name: processbarrier.ToolName, LoopID: "loop", TraceID: "trace",
		RequestID: "request", ExecutionID: "execution", CallOrdinal: 1,
	}
	for _, tt := range []struct {
		name   string
		mutate func(*agentic.ToolResult)
	}{
		{name: "matching"},
		{name: "provider call", mutate: func(r *agentic.ToolResult) { r.CallID = "other" }},
		{name: "tool name", mutate: func(r *agentic.ToolResult) { r.Name = "other" }},
		{name: "request", mutate: func(r *agentic.ToolResult) { r.RequestID = "other" }},
		{name: "execution", mutate: func(r *agentic.ToolResult) { r.ExecutionID = "other" }},
		{name: "ordinal", mutate: func(r *agentic.ToolResult) { r.CallOrdinal = 2 }},
		{name: "loop", mutate: func(r *agentic.ToolResult) { r.LoopID = "other" }},
		{name: "trace", mutate: func(r *agentic.ToolResult) { r.TraceID = "other" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := agentic.ToolResult{
				CallID: call.ID, Name: call.Name, LoopID: call.LoopID, TraceID: call.TraceID,
				RequestID: call.RequestID, ExecutionID: call.ExecutionID, CallOrdinal: call.CallOrdinal,
				Content: "released barrier",
			}
			if tt.mutate != nil {
				tt.mutate(&result)
			}
			wire, err := json.Marshal(message.NewBaseMessage(result.Schema(), &result, "agentic-tools"))
			if err != nil {
				t.Fatal(err)
			}
			stream := replayFixtureStream{
				wantSubject: "tool.result." + call.ExecutionID,
				stored:      &jetstream.RawStreamMsg{Data: wire},
			}
			s := &Scenario{decoder: payloadbuiltins.NewTestDecoder(t)}
			err = s.waitForToolResult(t.Context(), stream, call, time.Second)
			if tt.mutate == nil && err != nil {
				t.Fatal(err)
			}
			if tt.mutate != nil && (err == nil || !strings.Contains(err.Error(), "correlation")) {
				t.Fatalf("mismatched %s result error = %v, want correlation refusal", tt.name, err)
			}
		})
	}
}

type stageAConsumerFixture struct {
	jetstream.Consumer
	info *jetstream.ConsumerInfo
}

func (c stageAConsumerFixture) Info(context.Context) (*jetstream.ConsumerInfo, error) {
	return c.info, nil
}

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestStageADispatchWaitsForInjectedSourceDelivery(t *testing.T) {
	for _, tt := range []struct {
		name            string
		delivered       uint64
		pending, queued int
		wantDelivery    bool
	}{
		{name: "injected source still queued", delivered: 84, queued: 1},
		{name: "another source pending", delivered: 84, pending: 1, queued: 1},
		{name: "injected source delivered but unacked", delivered: 87, pending: 1, wantDelivery: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			info := &jetstream.ConsumerInfo{NumAckPending: tt.pending, NumPending: uint64(tt.queued)}
			// A consumer delivery count is not the retained stream sequence.
			info.Delivered.Consumer, info.Delivered.Stream = 100, tt.delivered
			info.AckFloor.Stream = 84
			err := waitForConsumerDelivery(t.Context(), stageAConsumerFixture{info: info}, 87, time.Millisecond)
			if tt.wantDelivery {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("publication observation started before the injected source was delivered")
			}
			for _, want := range []string{"stream sequence 87", "delivered_stream=84", "ack_floor_stream=84", "queued=1"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("delivery timeout = %v, want diagnostic %q", err, want)
				}
			}
		})
	}
}

// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestStageAConsumerRequiresSettlementAfterDelivery(t *testing.T) {
	for _, tt := range []struct {
		name     string
		ackFloor uint64
		pending  int
		settled  bool
	}{
		{name: "delivered but pending", ackFloor: 4, pending: 1},
		{name: "ack floor not reached", ackFloor: 4},
		{name: "another delivery remains pending", ackFloor: 5, pending: 1},
		{name: "required delivery settled", ackFloor: 5, settled: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			info := &jetstream.ConsumerInfo{NumAckPending: tt.pending}
			info.Delivered.Consumer, info.AckFloor.Consumer = 5, tt.ackFloor
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if !tt.settled {
				cancel()
			}
			err := waitForConsumerSettled(ctx, stageAConsumerFixture{info: info}, 5, time.Second)
			if tt.settled && err != nil {
				t.Fatal(err)
			}
			if !tt.settled && !errors.Is(err, context.Canceled) {
				t.Fatalf("unsettled source error = %v, want refusal after observing consumer state", err)
			}
		})
	}
}

type stageADispatchStreamFixture struct {
	replayFixtureStream
	count uint64
}

func (s stageADispatchStreamFixture) Info(context.Context, ...jetstream.StreamInfoOpt) (*jetstream.StreamInfo, error) {
	return &jetstream.StreamInfo{State: jetstream.StreamState{Subjects: map[string]uint64{s.wantSubject: s.count}}}, nil
}

// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID
// spec: agentic-dispatch / Every dispatch durable input settles through its owner
func TestStageADispatchAcceptsRepeatedCorrelatedPublications(t *testing.T) {
	loopID := "dce0441f-79c0-4e72-aae5-6144b7d8ce39"
	event := &agentic.LoopCompletedEvent{
		LoopID: loopID, TaskID: "task", Outcome: agentic.OutcomeSuccess,
		Result: "replacement result", CompletedAt: time.Now().UTC(),
	}
	terminal := message.NewBaseMessage(event.Schema(), event, "e2e-process-replacement")
	terminalWire, err := json.Marshal(terminal)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name      string
		count     uint64
		mutate    func(*agentic.UserResponse)
		wantError bool
	}{
		{name: "absent", wantError: true},
		{name: "one publication", count: 1},
		{name: "ordinary publication repeated", count: 2},
		{name: "source mismatch", count: 2, wantError: true, mutate: func(r *agentic.UserResponse) { r.ResponseID = "other" }},
		{name: "loop mismatch", count: 2, wantError: true, mutate: func(r *agentic.UserResponse) { r.InReplyTo = "other" }},
		{name: "route mismatch", count: 2, wantError: true, mutate: func(r *agentic.UserResponse) { r.ChannelID = "other" }},
		{name: "content mismatch", count: 2, wantError: true, mutate: func(r *agentic.UserResponse) { r.Content = "other" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := &agentic.UserResponse{
				ResponseID: "terminal-user-response:" + terminal.ID(), InReplyTo: loopID,
				Type: agentic.ResponseTypeResult, Content: event.Result, Timestamp: event.CompletedAt,
				ChannelType: "e2e-replacement", ChannelID: "channel",
			}
			if tt.mutate != nil {
				tt.mutate(response)
			}
			wire, err := json.Marshal(message.NewBaseMessage(response.Schema(), response, "agentic-dispatch"))
			if err != nil {
				t.Fatal(err)
			}
			subject := "user.response.e2e-replacement.channel"
			stream := stageADispatchStreamFixture{
				replayFixtureStream: replayFixtureStream{wantSubject: subject, stored: &jetstream.RawStreamMsg{Data: wire}},
				count:               tt.count,
			}
			s := &Scenario{decoder: payloadbuiltins.NewTestDecoder(t)}
			count, err := s.verifyDispatchResponse(t.Context(), stream, subject, dispatchTerminalFixture{loopID: loopID, wire: terminalWire})
			if (err != nil) != tt.wantError || (!tt.wantError && count != tt.count) {
				t.Fatalf("response count=%d error=%v; want count=%d error=%v", count, err, tt.count, tt.wantError)
			}
		})
	}
}

// spec: agentic-loop / Tool execution has stable framework correlation
// This measures the existing provider fixture; the scenario observes the loop's
// subsequent execution stamping through the registered tool.execute envelope.
func TestStageAMockRequestsBarrierThenCompletes(t *testing.T) {
	server := mock.NewOpenAIServer()
	if err := server.Start("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Stop(); err != nil {
			t.Error(err)
		}
	})
	request := mock.ChatCompletionRequest{
		Model: "mock", Messages: []mock.ChatMessage{{Role: "user", Content: "Run the barrier, then complete."}},
		Tools: []mock.Tool{{Type: "function", Function: mock.FunctionDef{Name: processbarrier.ToolName, Parameters: map[string]any{"type": "object"}}}},
	}
	for round := range 2 {
		wire, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL()+"/v1/chat/completions", bytes.NewReader(wire))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var completion mock.ChatCompletionResponse
		err = json.NewDecoder(resp.Body).Decode(&completion)
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusOK || len(completion.Choices) != 1 {
			t.Fatalf("unexpected completion: %#v", completion)
		}
		choice := completion.Choices[0]
		if round == 0 {
			if len(choice.Message.ToolCalls) != 1 {
				t.Fatalf("barrier tool calls = %#v", choice.Message.ToolCalls)
			}
			call := choice.Message.ToolCalls[0]
			if call.ID == "" || call.Function.Name != processbarrier.ToolName || call.Function.Arguments != "{}" {
				t.Fatalf("barrier tool call = %#v", call)
			}
			request.Messages = append(request.Messages, choice.Message, mock.ChatMessage{Role: "tool", ToolCallID: call.ID, Content: "released"})
		} else if choice.FinishReason != "stop" || choice.Message.Content == "" || len(choice.Message.ToolCalls) != 0 {
			t.Fatalf("post-barrier completion = %#v", choice)
		}
	}
}
