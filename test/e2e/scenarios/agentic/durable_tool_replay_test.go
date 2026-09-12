package agentic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/test/e2e/client"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type replayFixtureStream struct {
	jetstream.Stream
	wantSubject string
	stored      *jetstream.RawStreamMsg
}

func (s replayFixtureStream) GetLastMsgForSubject(_ context.Context, subject string) (*jetstream.RawStreamMsg, error) {
	if subject != s.wantSubject {
		return nil, fmt.Errorf("result subject = %q, want %q", subject, s.wantSubject)
	}
	return s.stored, nil
}

// spec: agentic-tools / Completed tool outcome identity is globally unambiguous
func TestAwaitReplayToolCallRequiresFreshCorrelatedLoopOutput(t *testing.T) {
	for _, tt := range []struct {
		name     string
		sequence uint64
		mutate   func(*agentic.ToolCall)
		ignored  bool
		invalid  bool
	}{
		{name: "fresh loop output", sequence: 11},
		{name: "old retained sequence", sequence: 10, ignored: true},
		{name: "another loop", sequence: 11, ignored: true, mutate: func(c *agentic.ToolCall) { c.LoopID = "other" }},
		{name: "missing request", sequence: 11, invalid: true, mutate: func(c *agentic.ToolCall) { c.RequestID = "" }},
		{name: "missing execution", sequence: 11, invalid: true, mutate: func(c *agentic.ToolCall) { c.ExecutionID = "" }},
		{name: "zero ordinal", sequence: 11, invalid: true, mutate: func(c *agentic.ToolCall) { c.CallOrdinal = 0 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			call := agentic.ToolCall{
				ID: "provider-call", Name: "query_entity", LoopID: "fresh-loop",
				RequestID: "request", ExecutionID: "execution", CallOrdinal: 1,
			}
			if tt.mutate != nil {
				tt.mutate(&call)
			}
			data, err := json.Marshal(message.NewBaseMessage(call.Schema(), &call, "agentic-loop"))
			if err != nil {
				t.Fatal(err)
			}
			stream := replayFixtureStream{
				wantSubject: "tool.execute.query_entity",
				stored:      &jetstream.RawStreamMsg{Data: data, Sequence: tt.sequence},
			}
			s := &Scenario{decoder: payloadbuiltins.NewTestDecoder(t), config: DefaultConfig()}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.ignored {
				// The fake supplies this stale read before the wait observes
				// cancellation, proving it cannot satisfy the capture.
				cancel()
			}
			got, err := s.awaitReplayToolCall(ctx, stream, "fresh-loop", "query_entity", 10)
			switch {
			case tt.ignored:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("stale capture error = %v, want cancellation after ignoring the record", err)
				}
			case tt.invalid:
				if err == nil || !strings.Contains(err.Error(), "correlation") {
					t.Fatalf("uncorrelated capture error = %v, want correlation refusal", err)
				}
			default:
				if err != nil || got.ID != call.ID || got.RequestID != call.RequestID ||
					got.ExecutionID != call.ExecutionID || got.CallOrdinal != call.CallOrdinal {
					t.Fatalf("captured call = %#v, error = %v; want unchanged execution correlation", got, err)
				}
			}
		})
	}
}

// spec: agentic-tools / Tool outcomes preserve framework execution correlation
// spec: agentic-tools / Tool-call completion SHALL be durable before request acknowledgement
func TestVerifyReplayedToolResultRequiresExecutionCorrelation(t *testing.T) {
	call := agentic.ToolCall{
		ID: "provider-call", Name: "query_entity", LoopID: "loop", TraceID: "trace",
		RequestID: "request", ExecutionID: "execution", CallOrdinal: 1,
	}
	for _, tt := range []struct {
		name       string
		mutate     func(*agentic.ToolResult)
		executions int
		wantError  string
	}{
		{name: "matching", executions: 11},
		{name: "request mismatch", executions: 11, wantError: "correlation", mutate: func(r *agentic.ToolResult) { r.RequestID = "other" }},
		{name: "execution mismatch", executions: 11, wantError: "correlation", mutate: func(r *agentic.ToolResult) { r.ExecutionID = "other" }},
		{name: "ordinal mismatch", executions: 11, wantError: "correlation", mutate: func(r *agentic.ToolResult) { r.CallOrdinal = 2 }},
		{name: "no new execution", executions: 10, wantError: "want exactly 1"},
		{name: "repeated execution", executions: 12, wantError: "want exactly 1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			metrics := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprintf(w, "semstreams_agentic_tools_executions_total{tool_name=\"query_entity\"} %d\n", tt.executions)
			}))
			t.Cleanup(metrics.Close)
			s := &Scenario{decoder: payloadbuiltins.NewTestDecoder(t), metrics: client.NewMetricsClient(metrics.URL)}
			result := agentic.ToolResult{
				CallID: call.ID, Name: call.Name, LoopID: call.LoopID, TraceID: call.TraceID,
				RequestID: call.RequestID, ExecutionID: call.ExecutionID, CallOrdinal: call.CallOrdinal,
				Content: "stored result",
			}
			if tt.mutate != nil {
				tt.mutate(&result)
			}
			data, err := json.Marshal(message.NewBaseMessage(result.Schema(), &result, "e2e-test"))
			if err != nil {
				t.Fatal(err)
			}
			wantMsgID := "tool-result/v1/" + durableCallDigest(call.ExecutionID)
			stream := replayFixtureStream{
				wantSubject: "tool.result." + call.ExecutionID,
				stored: &jetstream.RawStreamMsg{
					Data: data, Header: nats.Header{nats.MsgIdHdr: []string{wantMsgID}},
				},
			}
			msgID, delta, err := s.verifyReplayedToolResult(t.Context(), stream, call, 10)
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("error = %v, want %q", err, tt.wantError)
				}
				return
			}
			if err != nil || msgID != wantMsgID || delta != 1 {
				t.Fatalf("replay = %q, %.0f, %v; want %q, 1, nil", msgID, delta, err, wantMsgID)
			}
		})
	}
}
