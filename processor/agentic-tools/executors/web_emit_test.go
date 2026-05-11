package executors

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/message"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// recordingPublisher captures every triple AddTriple is called with and
// optionally fails on the Nth call so partial-failure tests can exercise
// the log+continue path. web_search and http_request still use the
// per-triple AddTriple path (NOT batch) because their emissions span
// multiple Subjects per call — the per-Subject atomic batch surface
// doesn't fit their cross-entity write pattern. See
// project_addtriplebatch_migration_followup memory.
type recordingPublisher struct {
	triples  []message.Triple
	failOn   int
	failWith error
}

func (p *recordingPublisher) AddTriple(_ context.Context, tr message.Triple) error {
	p.triples = append(p.triples, tr)
	if p.failWith != nil && len(p.triples) == p.failOn {
		return p.failWith
	}
	return nil
}

// AddTriplesBatch satisfies the widened TriplePublisher interface but
// web_search / http_request never call it (their emissions span
// multiple Subjects per call so the atomic per-Subject batch doesn't
// help). Present only for interface conformance — if a test hits it,
// the migration model is broken.
func (p *recordingPublisher) AddTriplesBatch(_ context.Context, _ []message.Triple) error {
	panic("recordingPublisher: AddTriplesBatch unexpectedly called; web_search/http_request emit per-Subject via AddTriple")
}

func fixedClock(t time.Time) func() time.Time {
	return func() time.Time { return t }
}

func testPlatform() component.PlatformMeta {
	return component.PlatformMeta{Org: "acme", Platform: "ops"}
}

// TestWebSearchExecutor_NilPublisher_NoEmission confirms the executor
// behaves exactly as pre-emission code when no publisher is wired —
// the canonical "additive, opt-in" guarantee.
func TestWebSearchExecutor_NilPublisher_NoEmission(t *testing.T) {
	e := NewWebSearchExecutor("test-key") // no options
	if e.publisher != nil {
		t.Fatalf("publisher should default to nil")
	}
	// emitObservations is the side-effect surface; with publisher==nil
	// the wider Execute path skips calling it. Confirm the field default
	// is nil and the executor stays zero-impact.
}

// TestWebSearchExecutor_EmitObservations_HappyPath drives the emission
// helper directly with two synthetic results. Verifies the full triple
// set lands: 6 per URL entity + 1 loop back-link per result.
func TestWebSearchExecutor_EmitObservations_HappyPath(t *testing.T) {
	clock := time.Date(2026, 5, 11, 14, 22, 0, 0, time.UTC)
	pub := &recordingPublisher{}
	e := NewWebSearchExecutor("test-key",
		WithWebSearchTriplePublisher(pub),
		WithWebSearchPlatform(testPlatform()),
		WithWebSearchClock(fixedClock(clock)),
	)

	results := []searchResult{
		{title: "Go net/http docs", url: "https://pkg.go.dev/net/http", description: "Package http provides HTTP client and server implementations."},
		{title: "Go json docs", url: "https://pkg.go.dev/encoding/json", description: "Package json implements encoding and decoding of JSON."},
	}
	call := agentic.ToolCall{ID: "call-1", Name: "web_search", LoopID: "loop-abc"}

	e.emitObservations(context.Background(), call, "go stdlib http and json", results)

	// 2 results × (6 URL triples + 1 loop back-link) = 14 triples.
	if got := len(pub.triples); got != 14 {
		t.Fatalf("triples published = %d, want 14", got)
	}

	loopEntityID, err := agentic.TryLoopExecutionEntityID("acme", "ops", "loop-abc")
	if err != nil {
		t.Fatalf("loop entity id: %v", err)
	}

	// Per-result invariants: each result produces an agent.web.observation
	// entity referenced by exactly one LoopObservedWeb back-link.
	loopBacklinks := map[string]int{}
	urlByEntity := map[string]string{}
	for _, tr := range pub.triples {
		if tr.Subject == loopEntityID && tr.Predicate == agvocab.LoopObservedWeb {
			loopBacklinks[tr.Object.(string)]++
		}
		if tr.Predicate == agvocab.WebURL {
			urlByEntity[tr.Subject] = tr.Object.(string)
		}
		// Source field must distinguish web_search emissions for operator
		// graph queries.
		if tr.Source != "agent-web-search" {
			t.Errorf("triple Source = %q, want %q", tr.Source, "agent-web-search")
		}
	}
	if len(loopBacklinks) != 2 {
		t.Errorf("expected 2 distinct observation entities back-linked from loop, got %d", len(loopBacklinks))
	}
	for entity, count := range loopBacklinks {
		if count != 1 {
			t.Errorf("observation entity %q referenced %d times in back-links, want 1", entity, count)
		}
	}

	// Canonical URLs land verbatim on agent.web.url.
	wantURLs := map[string]bool{
		"https://pkg.go.dev/net/http":      true,
		"https://pkg.go.dev/encoding/json": true,
	}
	for _, u := range urlByEntity {
		if !wantURLs[u] {
			t.Errorf("unexpected canonical url %q", u)
		}
	}

	// The source query lands on every observation. Rule-opaque per
	// vocabulary metadata; verified-rule-opaque is a separate test in
	// the vocab package.
	queryCount := 0
	for _, tr := range pub.triples {
		if tr.Predicate == agvocab.WebSourceQuery {
			queryCount++
			if tr.Object.(string) != "go stdlib http and json" {
				t.Errorf("source_query = %v, want literal", tr.Object)
			}
		}
	}
	if queryCount != 2 {
		t.Errorf("source_query triple count = %d, want 2 (one per observation entity)", queryCount)
	}
}

// TestWebSearchExecutor_EmitObservations_PublishFailure_ContinuesBatch
// confirms a per-triple publish failure does not abort the rest of the
// batch — graph emission is additive substrate, not the LLM-facing
// contract.
func TestWebSearchExecutor_EmitObservations_PublishFailure_ContinuesBatch(t *testing.T) {
	pub := &recordingPublisher{failOn: 2, failWith: errors.New("nats degraded")}
	e := NewWebSearchExecutor("test-key",
		WithWebSearchTriplePublisher(pub),
		WithWebSearchPlatform(testPlatform()),
	)

	results := []searchResult{
		{title: "A", url: "https://example.com/a", description: ""},
		{title: "B", url: "https://example.com/b", description: ""},
	}
	call := agentic.ToolCall{ID: "call-2", Name: "web_search", LoopID: "loop-xyz"}

	// Must not panic, must not return an error (executor caller would never
	// see one — emitObservations is fire-and-forget by design).
	e.emitObservations(context.Background(), call, "q", results)

	if len(pub.triples) != 14 {
		t.Errorf("AddTriple call count = %d, want 14 (continue-on-failure semantics)", len(pub.triples))
	}
}

// TestWebSearchExecutor_EmitObservations_MissingLoopID_Skips ensures the
// emission path declines cleanly when the tool call lacks a loop_id —
// the URL-side entities have nothing to back-link to.
func TestWebSearchExecutor_EmitObservations_MissingLoopID_Skips(t *testing.T) {
	pub := &recordingPublisher{}
	e := NewWebSearchExecutor("test-key",
		WithWebSearchTriplePublisher(pub),
		WithWebSearchPlatform(testPlatform()),
	)
	results := []searchResult{{title: "A", url: "https://example.com/a"}}
	call := agentic.ToolCall{ID: "no-loop", Name: "web_search"} // LoopID empty

	e.emitObservations(context.Background(), call, "q", results)

	if len(pub.triples) != 0 {
		t.Errorf("expected 0 triples when loop_id missing, got %d", len(pub.triples))
	}
}

// --- HTTPRequestExecutor emission tests ---

// TestHTTPRequestExecutor_NilPublisher_NoEmission confirms the
// pre-emission behavior is preserved when no publisher is wired.
func TestHTTPRequestExecutor_NilPublisher_NoEmission(t *testing.T) {
	e := NewHTTPRequestExecutor() // no options
	if e.publisher != nil {
		t.Errorf("publisher should default to nil")
	}
}

// TestHTTPRequestExecutor_EmitObservation_HappyPath drives the emission
// helper with a synthetic http.Response. Verifies all 8 triples land
// (7 URL-side + 1 loop back-link) and the truncated flag rides through.
func TestHTTPRequestExecutor_EmitObservation_HappyPath(t *testing.T) {
	clock := time.Date(2026, 5, 11, 14, 22, 0, 0, time.UTC)
	pub := &recordingPublisher{}
	e := NewHTTPRequestExecutor(
		WithHTTPTriplePublisher(pub),
		WithHTTPPlatform(testPlatform()),
		WithHTTPClock(fixedClock(clock)),
	)

	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
	}
	call := agentic.ToolCall{ID: "call-1", Name: "http_request", LoopID: "loop-abc"}

	e.emitObservation(context.Background(), call, "https://example.com/docs", resp, "<html>body</html>", false)

	if got := len(pub.triples); got != 8 {
		t.Fatalf("triples published = %d, want 8 (7 URL-side + 1 loop back-link)", got)
	}

	byPredicate := map[string]any{}
	loopBacklinkCount := 0
	for _, tr := range pub.triples {
		if tr.Source != "agent-http-request" {
			t.Errorf("Source = %q, want agent-http-request", tr.Source)
		}
		if tr.Predicate == agvocab.LoopFetchedWeb {
			loopBacklinkCount++
			continue
		}
		byPredicate[tr.Predicate] = tr.Object
	}
	if loopBacklinkCount != 1 {
		t.Errorf("LoopFetchedWeb back-link count = %d, want 1", loopBacklinkCount)
	}

	checks := map[string]any{
		agvocab.WebURL:         "https://example.com/docs",
		agvocab.WebContentType: "text/html; charset=utf-8",
		agvocab.WebStatusCode:  200,
		agvocab.WebText:        "<html>body</html>",
		agvocab.WebTruncated:   false,
	}
	for pred, want := range checks {
		if got := byPredicate[pred]; got != want {
			t.Errorf("predicate %q = %v, want %v", pred, got, want)
		}
	}
}

// TestHTTPRequestExecutor_EmitObservation_TruncatedFlag confirms the
// truncated bool reflects what was passed in — operators can rely on
// the structural fact even though WebText is rule-opaque.
func TestHTTPRequestExecutor_EmitObservation_TruncatedFlag(t *testing.T) {
	pub := &recordingPublisher{}
	e := NewHTTPRequestExecutor(
		WithHTTPTriplePublisher(pub),
		WithHTTPPlatform(testPlatform()),
	)
	resp := &http.Response{StatusCode: 200, Header: http.Header{}}

	e.emitObservation(context.Background(), agentic.ToolCall{ID: "c", LoopID: "l"}, "https://example.com/big", resp, "(big body)", true)

	found := false
	for _, tr := range pub.triples {
		if tr.Predicate == agvocab.WebTruncated {
			if tr.Object != true {
				t.Errorf("truncated = %v, want true", tr.Object)
			}
			found = true
		}
	}
	if !found {
		t.Errorf("no WebTruncated triple emitted")
	}
}

// TestHTTPRequestExecutor_EmitObservation_PublishFailure_ContinuesBatch
// confirms log+continue when a per-triple publish fails.
func TestHTTPRequestExecutor_EmitObservation_PublishFailure_ContinuesBatch(t *testing.T) {
	pub := &recordingPublisher{failOn: 3, failWith: errors.New("nats degraded")}
	e := NewHTTPRequestExecutor(
		WithHTTPTriplePublisher(pub),
		WithHTTPPlatform(testPlatform()),
	)
	resp := &http.Response{StatusCode: 200, Header: http.Header{}}

	e.emitObservation(context.Background(), agentic.ToolCall{ID: "c", LoopID: "l"}, "https://example.com/x", resp, "body", false)

	// All 8 AddTriple calls should have been issued, even though the
	// 3rd one returned an error — the helper logs and continues.
	if got := len(pub.triples); got != 8 {
		t.Errorf("AddTriple call count = %d, want 8 (continue-on-failure)", got)
	}
}

// TestHTTPRequestExecutor_EmitObservation_MissingLoopID_Skips
// confirms the helper bails when no loop_id is available; emitting
// dangling URL entities with no back-link to the consuming agent
// loses the audit trail.
func TestHTTPRequestExecutor_EmitObservation_MissingLoopID_Skips(t *testing.T) {
	pub := &recordingPublisher{}
	e := NewHTTPRequestExecutor(
		WithHTTPTriplePublisher(pub),
		WithHTTPPlatform(testPlatform()),
	)
	resp := &http.Response{StatusCode: 200, Header: http.Header{}}
	call := agentic.ToolCall{ID: "no-loop"} // empty LoopID

	e.emitObservation(context.Background(), call, "https://example.com/x", resp, "body", false)

	if len(pub.triples) != 0 {
		t.Errorf("expected 0 triples when loop_id missing, got %d", len(pub.triples))
	}
}

// TestWebSearchExecutor_EmitObservations_CancelledCtx_StillEmits is
// the regression for go-reviewer B1: caller-side ctx cancellation must
// NOT half-write batches, because graph emission is opportunistic
// substrate observation, not the LLM-facing contract. The Execute path
// wraps the emission ctx via context.WithoutCancel + bounded timeout;
// driving emitObservations directly with a pre-cancelled ctx exercises
// the inner path (publishes still flow because recordingPublisher
// ignores ctx). For full E2E coverage of the Execute-level detach, the
// integration-tier mock publisher inspects ctx.Err().
func TestWebSearchExecutor_EmitObservations_CancelledCtx_StillEmits(t *testing.T) {
	pub := &recordingPublisher{}
	e := NewWebSearchExecutor("test-key",
		WithWebSearchTriplePublisher(pub),
		WithWebSearchPlatform(testPlatform()),
	)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel; the emission helper does not gate on ctx.Err()
	e.emitObservations(ctx, agentic.ToolCall{ID: "c", LoopID: "l"}, "q", []searchResult{{title: "A", url: "https://example.com/a"}})
	// 1 result × 7 triples = 7. If the emission helper short-circuited
	// on a dead ctx, only the loop back-link (or zero) would land.
	if got := len(pub.triples); got != 7 {
		t.Errorf("AddTriple call count = %d, want 7 (helper must not short-circuit on ctx)", got)
	}
}

// TestWebSearchExecutor_EmitObservations_DedupsAcrossCalls asserts the
// hashed entity ID converges across two emission batches with the same
// URL. The reviewer flagged this invariant as stated in three places in
// docstrings but never property-tested. This locks it in.
func TestWebSearchExecutor_EmitObservations_DedupsAcrossCalls(t *testing.T) {
	pub := &recordingPublisher{}
	e := NewWebSearchExecutor("test-key",
		WithWebSearchTriplePublisher(pub),
		WithWebSearchPlatform(testPlatform()),
	)
	results := []searchResult{{title: "T", url: "https://example.com/x", description: "d"}}

	e.emitObservations(context.Background(), agentic.ToolCall{ID: "c1", LoopID: "loop-a"}, "q", results)
	firstCallTripleCount := len(pub.triples)

	e.emitObservations(context.Background(), agentic.ToolCall{ID: "c2", LoopID: "loop-b"}, "q", results)

	// Find the WebURL subjects from both batches.
	var subjects []string
	for _, tr := range pub.triples {
		if tr.Predicate == agvocab.WebURL {
			subjects = append(subjects, tr.Subject)
		}
	}
	if len(subjects) != 2 {
		t.Fatalf("WebURL triple count = %d, want 2 (one per emission call)", len(subjects))
	}
	if subjects[0] != subjects[1] {
		t.Errorf("observation entity diverged across calls: %q vs %q (dedup property broken)", subjects[0], subjects[1])
	}
	if len(pub.triples) != firstCallTripleCount*2 {
		t.Errorf("second call did not produce equal triple count: total=%d first=%d", len(pub.triples), firstCallTripleCount)
	}
}

// TestWebSearchExecutor_EmitObservations_LoopBacklinkFirst asserts the
// reordering from go-reviewer N2: the LoopObservedWeb back-link is the
// first triple in each per-result batch so partial-writes under
// graph-ingest degradation leave a discoverable pointer.
func TestWebSearchExecutor_EmitObservations_LoopBacklinkFirst(t *testing.T) {
	pub := &recordingPublisher{}
	e := NewWebSearchExecutor("test-key",
		WithWebSearchTriplePublisher(pub),
		WithWebSearchPlatform(testPlatform()),
	)
	results := []searchResult{{title: "A", url: "https://example.com/a"}}
	e.emitObservations(context.Background(), agentic.ToolCall{ID: "c", LoopID: "l"}, "q", results)

	if len(pub.triples) == 0 {
		t.Fatalf("no triples emitted")
	}
	if pub.triples[0].Predicate != agvocab.LoopObservedWeb {
		t.Errorf("first triple predicate = %q, want %q (back-link must precede URL-side triples for partial-write resilience)",
			pub.triples[0].Predicate, agvocab.LoopObservedWeb)
	}
}

// TestHTTPRequestExecutor_EmitObservation_LoopBacklinkFirst is the
// http_request counterpart to the websearch reordering check.
func TestHTTPRequestExecutor_EmitObservation_LoopBacklinkFirst(t *testing.T) {
	pub := &recordingPublisher{}
	e := NewHTTPRequestExecutor(
		WithHTTPTriplePublisher(pub),
		WithHTTPPlatform(testPlatform()),
	)
	resp := &http.Response{StatusCode: 200, Header: http.Header{}}
	e.emitObservation(context.Background(), agentic.ToolCall{ID: "c", LoopID: "l"}, "https://example.com/x", resp, "body", false)

	if len(pub.triples) == 0 {
		t.Fatalf("no triples emitted")
	}
	if pub.triples[0].Predicate != agvocab.LoopFetchedWeb {
		t.Errorf("first triple predicate = %q, want %q", pub.triples[0].Predicate, agvocab.LoopFetchedWeb)
	}
}

// TestWebSearchExecutor_EmitObservations_MalformedURL_Skips_Result
// confirms a per-result entity-ID failure doesn't abort the batch.
// Different results can have different validity (search providers
// occasionally return relative URLs); the bad one is skipped, the rest
// land.
func TestWebSearchExecutor_EmitObservations_MalformedURL_Skips_Result(t *testing.T) {
	pub := &recordingPublisher{}
	e := NewWebSearchExecutor("test-key",
		WithWebSearchTriplePublisher(pub),
		WithWebSearchPlatform(testPlatform()),
	)
	results := []searchResult{
		{title: "bad", url: "not-a-url", description: ""},
		{title: "good", url: "https://example.com/x", description: ""},
	}
	call := agentic.ToolCall{ID: "call-3", Name: "web_search", LoopID: "loop-1"}

	e.emitObservations(context.Background(), call, "q", results)

	// Only the good result emits its 7 triples; the bad one contributes 0.
	if len(pub.triples) != 7 {
		t.Fatalf("expected 7 triples (good result only), got %d", len(pub.triples))
	}
	for _, tr := range pub.triples {
		if tr.Predicate == agvocab.WebURL {
			if got := tr.Object.(string); !strings.Contains(got, "example.com/x") {
				t.Errorf("good result url = %q, want host example.com/x", got)
			}
		}
	}
}
