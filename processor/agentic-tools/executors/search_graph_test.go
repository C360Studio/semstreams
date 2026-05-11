package executors

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	graphquery "github.com/c360studio/semstreams/processor/graph-query"
)

func marshalSearchResp(t *testing.T, r searchGraphResponsePayload) []byte {
	t.Helper()
	out, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

func TestSearchGraphExecutor_ListTools(t *testing.T) {
	e := NewSearchGraphExecutor(&recordingNATSQuerier{})
	tools := e.ListTools()
	if len(tools) != 1 {
		t.Fatalf("ListTools = %d, want 1", len(tools))
	}
	tool := tools[0]
	if tool.Name != "search_graph" {
		t.Errorf("name = %q, want search_graph", tool.Name)
	}
	// query is the only required arg — locks the contract in.
	required, _ := tool.Parameters["required"].([]string)
	if len(required) != 1 || required[0] != "query" {
		t.Errorf("required = %v, want [\"query\"]", required)
	}
}

func TestSearchGraphExecutor_HappyPath_FormatsAnswer(t *testing.T) {
	canned := searchGraphResponsePayload{
		Strategy:    "graphrag",
		Answer:      "Authentication flows through middleware in the auth package.",
		AnswerModel: "qwen3-coder-30b",
		Count:       3,
		EntityDigests: []searchGraphEntityDigest{
			{ID: "acme.x.source.code.symbol.auth_middleware", Type: "symbol", Label: "AuthMiddleware", Relevance: 0.92},
			{ID: "acme.x.source.code.symbol.session_token", Type: "symbol", Label: "SessionToken", Relevance: 0.85},
		},
	}
	mock := &recordingNATSQuerier{resp: marshalSearchResp(t, canned)}
	e := NewSearchGraphExecutor(mock)

	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID:        "c1",
		Name:      "search_graph",
		Arguments: map[string]any{"query": "how does authentication work"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("result error: %q", res.Error)
	}

	if mock.gotSubject != searchGraphSubject {
		t.Errorf("subject = %q, want %q", mock.gotSubject, searchGraphSubject)
	}
	// query argument lands on the payload.
	if !strings.Contains(string(mock.gotPayload), `"query":"how does authentication work"`) {
		t.Errorf("payload missing query: %s", string(mock.gotPayload))
	}

	// Answer + digest IDs + synth-model attribution must be in text.
	for _, want := range []string{
		"Authentication flows",
		"qwen3-coder-30b",
		"AuthMiddleware",
		"acme.x.source.code.symbol.auth_middleware",
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("Content missing %q\n---\n%s", want, res.Content)
		}
	}
	// Metadata facets for dashboards / non-LLM callers.
	if res.Metadata["strategy"] != "graphrag" {
		t.Errorf("metadata strategy = %v, want graphrag", res.Metadata["strategy"])
	}
	if res.Metadata["degraded"] != false {
		t.Errorf("metadata degraded = %v, want false", res.Metadata["degraded"])
	}
}

// TestSearchGraphExecutor_DegradedBannerLeadsResponse is the
// load-bearing test for the discovery contract: when the server-side
// fallback fired, the FIRST non-empty line of the response must flag
// it so the reading agent treats the body as advisory. Without this
// banner an LLM has no idea the rich-looking answer was synthesized
// from fallback hits.
//
// The assertion uses the literal first line (split by newline) rather
// than substring-anywhere matching so we lock in the position
// invariant, not just the presence. Reviewer L1 nit fix.
func TestSearchGraphExecutor_DegradedBannerLeadsResponse(t *testing.T) {
	canned := searchGraphResponsePayload{
		Strategy:       "semantic_fallback",
		Degraded:       true,
		DegradedReason: "global_search_empty_semantic_fallback",
		Count:          2,
		EntityDigests: []searchGraphEntityDigest{
			{ID: "acme.x.source.code.symbol.foo", Relevance: 0.7},
		},
	}
	mock := &recordingNATSQuerier{resp: marshalSearchResp(t, canned)}
	e := NewSearchGraphExecutor(mock)

	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: "search_graph", Arguments: map[string]any{"query": "x"},
	})
	firstLine := strings.SplitN(res.Content, "\n", 2)[0]
	if !strings.Contains(strings.ToLower(firstLine), "degraded") {
		t.Errorf("degraded banner not the first line; first line = %q\n--- full content ---\n%s", firstLine, res.Content)
	}
	// And the reason is surfaced so operators / agents can route on
	// fallback type.
	if !strings.Contains(res.Content, "global_search_empty_semantic_fallback") {
		t.Errorf("degraded_reason missing from text")
	}
	if res.Metadata["degraded"] != true {
		t.Errorf("metadata degraded should be true on fallback path")
	}
}

// TestSearchGraphResponsePayload_DecodesFromRealGlobalSearchResponse
// is the M1 regression test from the go-reviewer audit. The agent-
// tool's response shape is a deliberate SUBSET of
// processor/graph-query.GlobalSearchResponse — taking a direct
// dependency on graph-query from agentic-tools would risk a future
// import cycle. The downside: a future field add to the source-of-
// truth shape silently won't surface in the agent-tool response.
//
// This test pins the contract: round-trip a fully-populated
// GlobalSearchResponse through JSON and confirm every field the
// formatter touches is preserved on the receiving side. Future
// drift on either side trips the test immediately.
func TestSearchGraphResponsePayload_DecodesFromRealGlobalSearchResponse(t *testing.T) {
	source := graphquery.GlobalSearchResponse{
		Strategy:    "semantic_fallback",
		Answer:      "Test answer body.",
		AnswerModel: "test-model",
		Count:       2,
		EntityDigests: []graphquery.EntityDigest{
			{ID: "a.b.c.d.e.f", Type: "thing", Label: "Thing", Relevance: 0.9},
		},
		CommunitySummaries: []graphquery.CommunitySummary{
			{Summary: "cluster prose"},
		},
		Degraded:       true,
		DegradedReason: "global_search_empty_semantic_fallback",
	}
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatalf("marshal source: %v", err)
	}
	var decoded searchGraphResponsePayload
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("decode into subset: %v", err)
	}
	if decoded.Strategy != source.Strategy ||
		decoded.Answer != source.Answer ||
		decoded.AnswerModel != source.AnswerModel ||
		decoded.Count != source.Count ||
		decoded.Degraded != source.Degraded ||
		decoded.DegradedReason != source.DegradedReason {
		t.Errorf("scalar field drift between GlobalSearchResponse and searchGraphResponsePayload: decoded=%+v", decoded)
	}
	if len(decoded.EntityDigests) != 1 {
		t.Fatalf("EntityDigests length = %d, want 1", len(decoded.EntityDigests))
	}
	if decoded.EntityDigests[0].ID != source.EntityDigests[0].ID ||
		decoded.EntityDigests[0].Type != source.EntityDigests[0].Type ||
		decoded.EntityDigests[0].Label != source.EntityDigests[0].Label ||
		decoded.EntityDigests[0].Relevance != source.EntityDigests[0].Relevance {
		t.Errorf("EntityDigest field drift: decoded=%+v", decoded.EntityDigests[0])
	}
	if len(decoded.CommunitySummaries) != 1 || decoded.CommunitySummaries[0].Summary != "cluster prose" {
		t.Errorf("CommunitySummary field drift: decoded=%+v", decoded.CommunitySummaries)
	}
}

// TestSearchGraphExecutor_HandlerErrorEnvelope is the H1 regression
// test (companion to TestSummarizeGraphExecutor_HandlerErrorEnvelope).
// Same natsclient handler-error-as-body convention; same risk of
// surfacing the wrong remediation if not caught.
func TestSearchGraphExecutor_HandlerErrorEnvelope(t *testing.T) {
	mock := &recordingNATSQuerier{resp: []byte("error: graph-query classifier offline")}
	e := NewSearchGraphExecutor(mock)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: "search_graph", Arguments: map[string]any{"query": "x"},
	})
	if err != nil {
		t.Fatalf("Execute should not return Go err: %v", err)
	}
	if res.ErrorKind != agentic.ToolErrorExternal {
		t.Errorf("ErrorKind = %v, want external", res.ErrorKind)
	}
	if !strings.Contains(res.Error, "classifier offline") {
		t.Errorf("Error should surface handler message verbatim; got %q", res.Error)
	}
}

func TestSearchGraphExecutor_NoResults(t *testing.T) {
	canned := searchGraphResponsePayload{Count: 0}
	mock := &recordingNATSQuerier{resp: marshalSearchResp(t, canned)}
	e := NewSearchGraphExecutor(mock)
	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: "search_graph", Arguments: map[string]any{"query": "x"},
	})
	if !strings.Contains(strings.ToLower(res.Content), "no results") {
		t.Errorf("expected no-results message; got %q", res.Content)
	}
}

func TestSearchGraphExecutor_RejectsMissingQuery(t *testing.T) {
	e := NewSearchGraphExecutor(&recordingNATSQuerier{})
	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: "search_graph", Arguments: map[string]any{},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want invalid_args", res.ErrorKind)
	}
}

func TestSearchGraphExecutor_RejectsEmptyQuery(t *testing.T) {
	e := NewSearchGraphExecutor(&recordingNATSQuerier{})
	res, _ := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: "search_graph", Arguments: map[string]any{"query": ""},
	})
	if res.ErrorKind != agentic.ToolErrorInvalidArgs {
		t.Errorf("ErrorKind = %v, want invalid_args", res.ErrorKind)
	}
}

func TestSearchGraphExecutor_NATSError(t *testing.T) {
	mock := &recordingNATSQuerier{err: errors.New("nats down")}
	e := NewSearchGraphExecutor(mock)
	res, err := e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: "search_graph", Arguments: map[string]any{"query": "x"},
	})
	if err != nil {
		t.Errorf("Execute should not return Go err for NATS failure")
	}
	if res.ErrorKind != agentic.ToolErrorNetwork {
		t.Errorf("ErrorKind = %v, want network", res.ErrorKind)
	}
}

// TestSearchGraphExecutor_OptionalArgsPropagated confirms level +
// max_communities flow through to the NATS payload when supplied.
func TestSearchGraphExecutor_OptionalArgsPropagated(t *testing.T) {
	mock := &recordingNATSQuerier{resp: marshalSearchResp(t, searchGraphResponsePayload{})}
	e := NewSearchGraphExecutor(mock)
	_, _ = e.Execute(context.Background(), agentic.ToolCall{
		ID: "c", Name: "search_graph",
		Arguments: map[string]any{
			"query":           "x",
			"level":           float64(2),
			"max_communities": float64(15),
		},
	})
	payload := string(mock.gotPayload)
	if !strings.Contains(payload, `"level":2`) {
		t.Errorf("payload missing level=2: %s", payload)
	}
	if !strings.Contains(payload, `"max_communities":15`) {
		t.Errorf("payload missing max_communities=15: %s", payload)
	}
}

func TestSearchGraphExecutor_WrongName(t *testing.T) {
	e := NewSearchGraphExecutor(&recordingNATSQuerier{})
	res, err := e.Execute(context.Background(), agentic.ToolCall{ID: "c", Name: "wrong"})
	if err == nil {
		t.Errorf("expected routing error")
	}
	if res.ErrorKind != agentic.ToolErrorNotFound {
		t.Errorf("ErrorKind = %v, want not_found", res.ErrorKind)
	}
}
