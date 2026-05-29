package executors

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
)

// recordingNATSQuerier captures the last request and returns a
// canned response. Pattern mirrors recordingPublisher from
// web_emit_test.go.
type recordingNATSQuerier struct {
	gotSubject string
	gotPayload []byte
	gotTimeout time.Duration

	resp []byte
	err  error
}

func (r *recordingNATSQuerier) Request(_ context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) {
	r.gotSubject = subject
	r.gotPayload = data
	r.gotTimeout = timeout
	return r.resp, r.err
}

// RequestClassified routes through Request and then runs ClassifyReply
// against the response bytes. The mock has no headers, so this
// exercises ClassifyReply's legacy-body-prefix fallback path —
// matches how a pre-#93 handler would behave today AND how a
// header-stamping Phase-1 handler appears once the response bytes
// land on the wire.
func (r *recordingNATSQuerier) RequestClassified(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) {
	body, err := r.Request(ctx, subject, data, timeout)
	if err != nil {
		return nil, err
	}
	msg := &nats.Msg{Data: body, Header: nats.Header{}}
	return natsclient.ClassifyReply(msg)
}

func successSummaryResponse(t *testing.T, data graph.SummaryData) []byte {
	t.Helper()
	resp := graph.NewQueryResponse(data)
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal canned response: %v", err)
	}
	return out
}

func errorSummaryResponse(t *testing.T, msg string) []byte {
	t.Helper()
	resp := graph.NewQueryError[graph.SummaryData](msg)
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal error response: %v", err)
	}
	return out
}

func TestSummarizeGraphExecutor_ListTools(t *testing.T) {
	e := NewSummarizeGraphExecutor(&recordingNATSQuerier{})
	tools := e.ListTools()
	if len(tools) != 1 {
		t.Fatalf("ListTools = %d, want 1", len(tools))
	}
	tool := tools[0]
	if tool.Name != "summarize_graph" {
		t.Errorf("name = %q, want summarize_graph", tool.Name)
	}
	props, _ := tool.Parameters["properties"].(map[string]any)
	if _, ok := props["include_predicates"]; !ok {
		t.Errorf("properties missing include_predicates")
	}
	// No required fields — discovery is meant to be zero-arg friendly.
	if r, ok := tool.Parameters["required"].([]string); ok && len(r) > 0 {
		t.Errorf("summarize_graph should have no required args; got %v", r)
	}
}

func TestSummarizeGraphExecutor_HappyPath_FormatsText(t *testing.T) {
	canned := graph.SummaryData{
		TotalEntities:         42,
		EntitySampleTruncated: false,
		EntityTypes: []graph.EntityTypeSummary{
			{Type: "agent.web.observation", Count: 30, Examples: []string{"acme.x.agent.web.observation.h1", "acme.x.agent.web.observation.h2"}},
			{Type: "agent.agentic-loop.execution", Count: 12, Examples: []string{"acme.x.agent.agentic-loop.execution.l1"}},
		},
		Predicates: []graph.PredicateSummary{
			{Predicate: "agent.web.url", EntityCount: 30},
			{Predicate: "agent.loop.outcome", EntityCount: 12},
		},
		PredicateTotal: 2,
	}
	mock := &recordingNATSQuerier{resp: successSummaryResponse(t, canned)}
	e := NewSummarizeGraphExecutor(mock)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      "summarize_graph",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("result error: %q", res.Error)
	}

	// Subject + timeout sanity.
	if mock.gotSubject != summarizeGraphSubject {
		t.Errorf("subject = %q, want %q", mock.gotSubject, summarizeGraphSubject)
	}
	if mock.gotTimeout != summarizeGraphTimeout {
		t.Errorf("timeout = %v, want %v", mock.gotTimeout, summarizeGraphTimeout)
	}

	// Text contains the entity-type names + example IDs + predicate
	// names. Don't pin exact formatting — that's brittle and not the
	// invariant we care about.
	for _, want := range []string{
		"42",
		"agent.web.observation",
		"acme.x.agent.web.observation.h1",
		"agent.agentic-loop.execution",
		"agent.web.url",
		"agent.loop.outcome",
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("Content missing substring %q\n--- full content ---\n%s", want, res.Content)
		}
	}
	// Metadata facets agents / dashboards can read without parsing prose.
	if res.Metadata["total_entities"] != 42 {
		t.Errorf("metadata total_entities = %v, want 42", res.Metadata["total_entities"])
	}
}

// TestSummarizeGraphExecutor_TruncationFlagAppearsInText locks in the
// approximate-counts disclosure: if the server-side scan saturates the
// sample limit, the text must say so. Operators rely on this to avoid
// over-trusting per-type counts on large graphs.
func TestSummarizeGraphExecutor_TruncationFlagAppearsInText(t *testing.T) {
	canned := graph.SummaryData{TotalEntities: 2000, EntitySampleTruncated: true}
	mock := &recordingNATSQuerier{resp: successSummaryResponse(t, canned)}
	e := NewSummarizeGraphExecutor(mock)
	res, _ := e.Execute(context.Background(), agentic.ToolCall{ID: "c", Name: "summarize_graph"})
	if !strings.Contains(strings.ToLower(res.Content), "truncated") &&
		!strings.Contains(strings.ToLower(res.Content), "approximate") {
		t.Errorf("truncation marker missing from text:\n%s", res.Content)
	}
}

// TestSummarizeGraphExecutor_IncludePredicatesArgPropagated confirms
// the arg lands on the NATS payload. Server-side already has unit
// tests for the default vs explicit-false behavior; this just guards
// the agent-tool → server hop.
func TestSummarizeGraphExecutor_IncludePredicatesArgPropagated(t *testing.T) {
	mock := &recordingNATSQuerier{resp: successSummaryResponse(t, graph.SummaryData{})}
	e := NewSummarizeGraphExecutor(mock)
	_, _ = e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c",
		Name:      "summarize_graph",
		Arguments: map[string]any{"include_predicates": false},
	})
	if !strings.Contains(string(mock.gotPayload), `"include_predicates":false`) {
		t.Errorf("payload missing include_predicates=false: %s", string(mock.gotPayload))
	}
}

func TestSummarizeGraphExecutor_NATSError(t *testing.T) {
	mock := &recordingNATSQuerier{err: errors.New("nats down")}
	e := NewSummarizeGraphExecutor(mock)
	res, err := e.Execute(context.Background(), agentic.ToolCall{ID: "c", Name: "summarize_graph"})
	if err != nil {
		t.Errorf("Execute should not return Go err for NATS failure: %v", err)
	}
	if res.ErrorKind != agentic.ToolErrorNetwork {
		t.Errorf("ErrorKind = %v, want network", res.ErrorKind)
	}
}

func TestSummarizeGraphExecutor_ServerSideError(t *testing.T) {
	mock := &recordingNATSQuerier{resp: errorSummaryResponse(t, "graph-ingest unreachable")}
	e := NewSummarizeGraphExecutor(mock)
	res, _ := e.Execute(context.Background(), agentic.ToolCall{ID: "c", Name: "summarize_graph"})
	if res.ErrorKind != agentic.ToolErrorExternal {
		t.Errorf("ErrorKind = %v, want external (server-side error from downstream component)", res.ErrorKind)
	}
	if !strings.Contains(res.Error, "graph-ingest unreachable") {
		t.Errorf("Error should surface server message; got %q", res.Error)
	}
}

func TestSummarizeGraphExecutor_WrongName(t *testing.T) {
	e := NewSummarizeGraphExecutor(&recordingNATSQuerier{})
	res, err := e.Execute(context.Background(), agentic.ToolCall{ID: "c", Name: "wrong"})
	if err == nil {
		t.Errorf("expected routing error")
	}
	if res.ErrorKind != agentic.ToolErrorNotFound {
		t.Errorf("ErrorKind = %v, want not_found", res.ErrorKind)
	}
}

// TestSummarizeGraphExecutor_HandlerErrorEnvelope is the H1
// regression test from the go-reviewer audit. natsclient's
// SubscribeForRequests wraps handler (nil, err) returns as a body
// payload of the form `error: <msg>`. The tool MUST detect this
// before attempting to json.Unmarshal as SummaryData; otherwise a
// downstream component outage surfaces as a cryptic parse error
// pointing the LLM at the wrong remediation.
//
// See memory feedback_natsclient_error_payload_convention.md — the
// same shape bit PR #48 twice.
func TestSummarizeGraphExecutor_HandlerErrorEnvelope(t *testing.T) {
	// Exact byte shape natsclient.SubscribeForRequests emits when the
	// handler returns (nil, err).
	mock := &recordingNATSQuerier{resp: []byte("error: graph-ingest unreachable")}
	e := NewSummarizeGraphExecutor(mock)
	res, err := e.Execute(context.Background(), agentic.ToolCall{ID: "c", Name: "summarize_graph"})
	if err != nil {
		t.Fatalf("Execute should not return Go err: %v", err)
	}
	if res.ErrorKind != agentic.ToolErrorExternal {
		t.Errorf("ErrorKind = %v, want external (downstream alive, reporting failure)", res.ErrorKind)
	}
	if !strings.Contains(res.Error, "graph-ingest unreachable") {
		t.Errorf("Error should surface the handler's message verbatim; got %q", res.Error)
	}
}

// TestSortedPredicatesByCount_StableTieBreak guards prompt-caching:
// equal-count predicates must sort alphabetically so repeat summary
// calls on the same graph produce identical text.
func TestSortedPredicatesByCount_StableTieBreak(t *testing.T) {
	input := []graph.PredicateSummary{
		{Predicate: "zebra", EntityCount: 5},
		{Predicate: "apple", EntityCount: 5},
	}
	sorted := sortedPredicatesByCount(input)
	if sorted[0].Predicate != "apple" {
		t.Errorf("alphabetical tie-break broken: first = %q, want apple", sorted[0].Predicate)
	}
}
