package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// newSummaryTestComponent builds a Component wired only with what
// handleQueryGraphSummary touches: a mock natsClient, the static
// router, a logger, and the config's QueryTimeout.
func newSummaryTestComponent(req func(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)) *Component {
	mock := newMockNATSClient()
	mock.requestFunc = req
	c := &Component{
		natsClient: mock,
		router:     NewStaticRouter(slog.Default()),
		logger:     slog.Default(),
		config: Config{
			QueryTimeout: 5 * time.Second,
		},
	}
	return c
}

// TestHandleQueryGraphSummary_PrefixHandlerErrorPropagates is the P1a regression
// lock (ADR-060 PR-D review). The summary's prefix fetch uses RequestClassified:
// a classified graph-ingest handler error MUST propagate, not be decoded by
// extractEntityIDsFromPrefixResponse into an empty entity list that returns a
// bogus TotalEntities=0 summary with nil error (the silent-corruption shape the
// {message,detail} envelope would otherwise produce on the raw-Request path).
func TestHandleQueryGraphSummary_PrefixHandlerErrorPropagates(t *testing.T) {
	mock := newMockNATSClient()
	mock.requestClassifiedFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		return nil, errs.ClassifiedCode(errs.ErrorTransient, "internal", errors.New("graph-ingest store unavailable"))
	}
	c := &Component{
		natsClient: mock,
		router:     NewStaticRouter(slog.Default()),
		logger:     slog.Default(),
		config:     Config{QueryTimeout: 5 * time.Second},
	}

	respBytes, err := c.handleQueryGraphSummary(context.Background(), []byte(`{}`))
	if err == nil {
		t.Fatalf("a classified prefix-handler error must propagate; got nil err, resp=%q", respBytes)
	}
	if !errs.IsTransient(err) {
		t.Errorf("prefix failure's transient class must survive the summary handler; IsTransient=false, err=%v", err)
	}
}

// prefixEnvelope marshals the {"entities": [...]} shape graph-ingest
// returns from handleQueryPrefixNATS.
func prefixEnvelope(ids ...string) []byte {
	type ent struct {
		ID string `json:"id"`
	}
	entities := make([]ent, len(ids))
	for i, id := range ids {
		entities[i] = ent{ID: id}
	}
	out, _ := json.Marshal(map[string]any{"entities": entities})
	return out
}

func TestHandleQueryGraphSummary_PoisonedPrefixReplyPropagates(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidEntityID := "bad"
	poisoned, err := json.Marshal(graph.PrefixQueryResponse{Entities: []graph.EntityState{
		{ID: validID},
		{ID: validID, Triples: []message.Triple{{Subject: invalidEntityID, Predicate: "test.state.value"}}},
	}})
	if err != nil {
		t.Fatalf("marshal poison response: %v", err)
	}
	c := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		if subject == "graph.ingest.query.prefix" {
			return poisoned, nil
		}
		return predicateListResponse(), nil
	})

	response, err := c.handleQueryGraphSummary(context.Background(), []byte(`{"include_predicates":false}`))
	if err == nil || !graph.IsStateContractError(err) {
		t.Fatalf("error = %T %v, want graph state reset contract", err, err)
	}
	if response != nil {
		t.Fatalf("response = %s, want nil before summary projection", response)
	}
}

// predicateListResponse marshals a successful predicate-list response
// from graph.index.query.predicateList.
func predicateListResponse(preds ...graph.PredicateSummary) []byte {
	resp := graph.NewQueryResponse(graph.PredicateListData{
		Predicates: preds,
		Total:      len(preds),
	})
	out, _ := json.Marshal(resp)
	return out
}

// TestHandleQueryGraphSummary_HappyPath exercises the full composite:
// prefix returns entities, predicates return three predicates, the
// handler aggregates by domain.system.type and returns both facets.
func TestHandleQueryGraphSummary_HappyPath(t *testing.T) {
	c := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.ingest.query.prefix":
			return prefixEnvelope(
				"acme.platform.agent.web.observation.h1",
				"acme.platform.agent.web.observation.h2",
				"acme.platform.agent.web.observation.h3",
				"acme.platform.agent.agentic-loop.execution.l1",
				"acme.platform.agent.agentic-loop.execution.l2",
				"acme.platform.semsource.golang.workspace.f1",
			), nil
		case "graph.index.query.predicateList":
			return predicateListResponse(
				graph.PredicateSummary{Predicate: "agent.web.url", EntityCount: 3},
				graph.PredicateSummary{Predicate: "agent.loop.outcome", EntityCount: 2},
				graph.PredicateSummary{Predicate: "source.code.symbol_name", EntityCount: 1},
			), nil
		}
		return nil, errors.New("unexpected subject: " + subject)
	})

	respBytes, err := c.handleQueryGraphSummary(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp graph.QueryResponse[graph.SummaryData]
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// ADR-060: success is the nil err above; QueryResponse no longer carries Error.

	summary := resp.Data
	if summary.TotalEntities != 6 {
		t.Errorf("TotalEntities = %d, want 6", summary.TotalEntities)
	}
	if summary.EntitySampleTruncated {
		t.Errorf("EntitySampleTruncated = true, want false (6 < default sample limit)")
	}
	if got := len(summary.EntityTypes); got != 3 {
		t.Fatalf("EntityTypes = %d, want 3 buckets", got)
	}
	// Buckets must be sorted by Count desc; first is the 3-entity web.observation.
	if summary.EntityTypes[0].Type != "agent.web.observation" {
		t.Errorf("top bucket = %q, want agent.web.observation", summary.EntityTypes[0].Type)
	}
	if summary.EntityTypes[0].Count != 3 {
		t.Errorf("top bucket Count = %d, want 3", summary.EntityTypes[0].Count)
	}
	// Example IDs default to 2 per bucket — verify cap respected on the
	// 3-entity bucket.
	if got := len(summary.EntityTypes[0].Examples); got != 2 {
		t.Errorf("top bucket Examples = %d, want 2 (default examples_per_type cap)", got)
	}
	// Predicates pass through with exact counts.
	if summary.PredicateTotal != 3 {
		t.Errorf("PredicateTotal = %d, want 3", summary.PredicateTotal)
	}
	if got := len(summary.Predicates); got != 3 {
		t.Errorf("Predicates length = %d, want 3", got)
	}
}

// TestHandleQueryGraphSummary_IncludePredicatesFalse confirms the
// predicate facet can be opted out — useful when callers cache it or
// when response size matters.
func TestHandleQueryGraphSummary_IncludePredicatesFalse(t *testing.T) {
	predicateCalled := false
	c := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.ingest.query.prefix":
			return prefixEnvelope("acme.platform.agent.web.observation.h1"), nil
		case "graph.index.query.predicateList":
			predicateCalled = true
			return predicateListResponse(graph.PredicateSummary{Predicate: "x", EntityCount: 1}), nil
		}
		return nil, errors.New("unexpected subject: " + subject)
	})

	respBytes, err := c.handleQueryGraphSummary(context.Background(), []byte(`{"include_predicates": false}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if predicateCalled {
		t.Errorf("predicate subject should NOT be requested when include_predicates=false")
	}

	var resp graph.QueryResponse[graph.SummaryData]
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Data.Predicates != nil {
		t.Errorf("Predicates should be nil when include_predicates=false")
	}
	if resp.Data.PredicateTotal != 0 {
		t.Errorf("PredicateTotal = %d, want 0", resp.Data.PredicateTotal)
	}
}

// TestHandleQueryGraphSummary_PredicateFailure_StillReturnsEntityFacet
// confirms the predicate facet failing is non-fatal — the caller gets
// the entity overview anyway. This is the "best-effort composite"
// invariant documented on handleQueryGraphSummary.
func TestHandleQueryGraphSummary_PredicateFailure_StillReturnsEntityFacet(t *testing.T) {
	c := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.ingest.query.prefix":
			return prefixEnvelope("acme.platform.agent.web.observation.h1"), nil
		case "graph.index.query.predicateList":
			return nil, errors.New("graph-index degraded")
		}
		return nil, errors.New("unexpected subject: " + subject)
	})

	respBytes, err := c.handleQueryGraphSummary(context.Background(), nil)
	if err != nil {
		t.Fatalf("composite should not fail just because predicate facet failed: %v", err)
	}
	var resp graph.QueryResponse[graph.SummaryData]
	_ = json.Unmarshal(respBytes, &resp)
	if resp.Data.TotalEntities != 1 {
		t.Errorf("TotalEntities = %d, want 1 (entity facet should land)", resp.Data.TotalEntities)
	}
	if resp.Data.Predicates != nil {
		t.Errorf("Predicates should be nil on predicate-facet failure (partial response)")
	}
}

// TestHandleQueryGraphSummary_PrefixFailure_Propagates confirms that
// the entity-facet failure DOES propagate — without entities there
// is nothing useful to return.
func TestHandleQueryGraphSummary_PrefixFailure_Propagates(t *testing.T) {
	c := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		if subject == "graph.ingest.query.prefix" {
			return nil, errors.New("graph-ingest down")
		}
		return predicateListResponse(), nil
	})
	if _, err := c.handleQueryGraphSummary(context.Background(), nil); err == nil {
		t.Errorf("expected error when entity facet fails; got nil")
	}
}

// TestHandleQueryGraphSummary_TruncationFlag confirms the
// EntitySampleTruncated flag is set when the prefix scan saturates
// the limit. Callers use this to know counts are approximate.
func TestHandleQueryGraphSummary_TruncationFlag(t *testing.T) {
	c := newSummaryTestComponent(func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		if subject == "graph.ingest.query.prefix" {
			// Return exactly the limit we asked for — simulates a full sample.
			return prefixEnvelope(
				"acme.platform.agent.web.observation.h1",
				"acme.platform.agent.web.observation.h2",
				"acme.platform.agent.web.observation.h3",
			), nil
		}
		return predicateListResponse(), nil
	})
	respBytes, err := c.handleQueryGraphSummary(context.Background(), []byte(`{"entity_sample_limit": 3, "include_predicates": false}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp graph.QueryResponse[graph.SummaryData]
	_ = json.Unmarshal(respBytes, &resp)
	if !resp.Data.EntitySampleTruncated {
		t.Errorf("EntitySampleTruncated = false, want true when scan saturates limit")
	}
}

// TestAggregateEntityTypes_SkipsMalformedIDs documents that the
// aggregation only counts entities with the canonical 6-part shape;
// shorter IDs (legacy tests, junk) are silently skipped rather than
// failing the whole summary.
func TestAggregateEntityTypes_SkipsMalformedIDs(t *testing.T) {
	ids := []string{
		"acme.platform.agent.web.observation.h1", // valid
		"too.short",                              // 2 parts, skip
		"acme.platform.agent.web.observation.h2", // valid
		"three.part.thing",                       // 3 parts, skip
	}
	got := aggregateEntityTypes(ids, 2)
	if len(got) != 1 {
		t.Fatalf("buckets = %d, want 1 (only valid IDs grouped)", len(got))
	}
	if got[0].Count != 2 {
		t.Errorf("Count = %d, want 2", got[0].Count)
	}
}

// TestAggregateEntityTypes_StableTieBreak — equal-count buckets sort
// alphabetically so prompt-caching downstream stays stable across
// repeat summary calls on the same graph.
func TestAggregateEntityTypes_StableTieBreak(t *testing.T) {
	ids := []string{
		"acme.platform.zeta.system.type.e1",
		"acme.platform.alpha.system.type.e1",
	}
	got := aggregateEntityTypes(ids, 1)
	if len(got) != 2 {
		t.Fatalf("buckets = %d, want 2", len(got))
	}
	if got[0].Type != "alpha.system.type" {
		t.Errorf("first bucket = %q, want alpha.system.type (ties break alphabetically)", got[0].Type)
	}
}

// TestParseGraphSummaryRequest_Defaults confirms missing fields fall
// back to V1 defaults rather than zero values (which would silently
// disable the scan).
func TestParseGraphSummaryRequest_Defaults(t *testing.T) {
	req := parseSummaryRequest([]byte(`{}`))
	if !req.IncludePredicates {
		t.Errorf("IncludePredicates default = false, want true")
	}
	if req.EntitySampleLimit != graphSummaryDefaultSampleLimit {
		t.Errorf("EntitySampleLimit default = %d, want %d", req.EntitySampleLimit, graphSummaryDefaultSampleLimit)
	}
	if req.ExamplesPerType != graphSummaryDefaultExamplesPerType {
		t.Errorf("ExamplesPerType default = %d, want %d", req.ExamplesPerType, graphSummaryDefaultExamplesPerType)
	}
}

// TestParseGraphSummaryRequest_ExplicitFalseOverrides confirms that
// passing include_predicates=false actually disables predicates
// (the default-bool footgun: distinguishing "unset" from "false").
func TestParseGraphSummaryRequest_ExplicitFalseOverrides(t *testing.T) {
	req := parseSummaryRequest([]byte(`{"include_predicates": false}`))
	if req.IncludePredicates {
		t.Errorf("explicit include_predicates=false ignored")
	}
}

// entity-id-audit:classify intentional-malformed "bad" line=78 column=21 surface=go-assignment:invalidEntityID graph summary prefix aggregate subject poison fixture

// TestParseSummaryRequest_RoundTrip locks in the JSON tag contract
// per the project's polymorphic-config rule (memory:
// feedback_polymorphic_config_needs_json_roundtrip_test.md). The
// request struct IS operator-visible via GraphQL variables → NATS
// payload, so a stealth rename of a snake_case tag would silently
// break operators without surfacing here. This test catches that.
func TestParseSummaryRequest_RoundTrip(t *testing.T) {
	original := graph.SummaryRequest{
		IncludePredicates: false,
		EntitySampleLimit: 500,
		ExamplesPerType:   3,
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded := parseSummaryRequest(encoded)
	if decoded.IncludePredicates != original.IncludePredicates {
		t.Errorf("IncludePredicates round-trip: got %v, want %v", decoded.IncludePredicates, original.IncludePredicates)
	}
	if decoded.EntitySampleLimit != original.EntitySampleLimit {
		t.Errorf("EntitySampleLimit round-trip: got %d, want %d", decoded.EntitySampleLimit, original.EntitySampleLimit)
	}
	if decoded.ExamplesPerType != original.ExamplesPerType {
		t.Errorf("ExamplesPerType round-trip: got %d, want %d", decoded.ExamplesPerType, original.ExamplesPerType)
	}
}
