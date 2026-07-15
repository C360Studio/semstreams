package fusionnats

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/fusion"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRequester records the last request and returns a canned reply, or routes
// per-subject through responder when set.
type fakeRequester struct {
	lastSubject string
	lastData    []byte
	resp        []byte
	err         error
	responder   func(subject string, data []byte) ([]byte, error)
}

func (f *fakeRequester) RequestClassified(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
	f.lastSubject = subject
	f.lastData = append([]byte(nil), data...)
	if f.responder != nil {
		return f.responder(subject, data)
	}
	return f.resp, f.err
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestStatus_MapsResponse(t *testing.T) {
	// Round-trip guard (ADR-066 §5): every field of graph.IndexStatusResponse must
	// survive the client decode into fusion.IndexStatus. The revision-lag fields
	// (IndexedRevision/TargetRevision/Lag/Phase) were silently dropped by an earlier
	// hand-copied remap — Lag==0 downstream reads as false-caught-up mid-build.
	fake := &fakeRequester{resp: mustJSON(t, graph.IndexStatusResponse{
		Ready: true, State: graph.IndexStateReady,
		IndexedRevision: 100, TargetRevision: 100, Lag: 0, Phase: "ready",
		Revision: "100", LastSynced: "now",
	})}
	c := New(fake, time.Second)

	got, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if fake.lastSubject != subjectStatus {
		t.Errorf("subject = %q, want %q", fake.lastSubject, subjectStatus)
	}
	want := fusion.IndexStatus{
		Ready: true, State: fusion.StateReady,
		IndexedRevision: 100, TargetRevision: 100, Lag: 0, Phase: "ready",
		Revision: "100", LastSynced: "now",
	}
	if got != want {
		t.Errorf("status = %+v, want %+v", got, want)
	}
}

// TestStatus_RevisionLagFieldsSurvive is the explicit guard that a mid-build
// envelope (not caught up) round-trips its numeric fields — a consumer gating on
// Lag must see the real lag, not a dropped-to-zero false-ready.
func TestStatus_RevisionLagFieldsSurvive(t *testing.T) {
	fake := &fakeRequester{resp: mustJSON(t, graph.IndexStatusResponse{
		Ready: false, State: graph.IndexStateBuilding,
		IndexedRevision: 40, TargetRevision: 100, Lag: 60, Revision: "40",
	})}
	c := New(fake, time.Second)

	got, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Lag != 60 || got.IndexedRevision != 40 || got.TargetRevision != 100 {
		t.Errorf("revision-lag fields dropped: got Lag=%d Indexed=%d Target=%d, want 60/40/100",
			got.Lag, got.IndexedRevision, got.TargetRevision)
	}
	if got.Ready {
		t.Error("mid-build must not read ready")
	}
}

func TestStatus_NotReady(t *testing.T) {
	fake := &fakeRequester{resp: mustJSON(t, graph.IndexStatusResponse{Ready: false, State: graph.IndexStateBuilding})}
	c := New(fake, time.Second)

	got, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.Ready {
		t.Error("expected not ready")
	}
	if got.State != fusion.StateBuilding {
		t.Errorf("state = %q, want building", got.State)
	}
}

func TestResolve_Symbol(t *testing.T) {
	fake := &fakeRequester{resp: mustJSON(t, graph.NewQueryResponse(graph.NameData{Matches: []graph.NameMatch{
		{EntityID: "a.b.c.d.e.1", MatchedName: "Widget"},
		{EntityID: "a.b.c.d.e.2", MatchedName: "Widget"},
	}}))}
	c := New(fake, time.Second)

	ids, err := c.Resolve(context.Background(), fusion.ResolveQuery{Query: "Widget", Mode: fusion.ResolveModeSymbol, Limit: 10})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if fake.lastSubject != subjectByName {
		t.Errorf("subject = %q, want %q", fake.lastSubject, subjectByName)
	}
	// Request must carry the query under "name" and the limit.
	var req map[string]any
	if err := json.Unmarshal(fake.lastData, &req); err != nil {
		t.Fatalf("request not JSON: %v", err)
	}
	if req["name"] != "Widget" {
		t.Errorf("request name = %v, want Widget", req["name"])
	}
	if want := []string{"a.b.c.d.e.1", "a.b.c.d.e.2"}; !equalStrings(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
}

func TestResolve_Prefix(t *testing.T) {
	fake := &fakeRequester{resp: mustJSON(t, graph.PrefixQueryResponse{Entities: []graph.EntityState{
		{ID: "a.b.c.d.e.1"}, {ID: "a.b.c.d.e.2"},
	}})}
	c := New(fake, time.Second)

	ids, err := c.Resolve(context.Background(), fusion.ResolveQuery{Query: "a.b.c", Mode: fusion.ResolveModePrefix, Limit: 10})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if fake.lastSubject != subjectPrefix {
		t.Errorf("subject = %q, want %q", fake.lastSubject, subjectPrefix)
	}
	if want := []string{"a.b.c.d.e.1", "a.b.c.d.e.2"}; !equalStrings(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
}

func TestResolve_InvalidPrefixFailsBeforeNATS(t *testing.T) {
	t.Parallel()

	fake := &fakeRequester{}
	c := New(fake, time.Second)
	_, err := c.Resolve(context.Background(), fusion.ResolveQuery{
		Query: "acme.*",
		Mode:  fusion.ResolveModePrefix,
		Limit: 10,
	})
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, semtypes.ErrorCodeEntityIDPrefixInvalid, classified.Code)
	assert.Empty(t, fake.lastSubject, "invalid prefix must fail before a NATS request")
}

func TestResolve_EmptyPrefixRemainsMatchAll(t *testing.T) {
	t.Parallel()

	fake := &fakeRequester{resp: mustJSON(t, graph.PrefixQueryResponse{})}
	c := New(fake, time.Second)
	_, err := c.Resolve(context.Background(), fusion.ResolveQuery{Mode: fusion.ResolveModePrefix, Limit: 10})
	require.NoError(t, err)
	assert.Equal(t, subjectPrefix, fake.lastSubject)
}

func TestResolve_Semantic(t *testing.T) {
	resp := map[string]any{"results": []map[string]any{
		{"entity_id": "a.b.c.d.e.1", "similarity": 0.9},
		{"entity_id": "", "similarity": 0.1}, // empty IDs are dropped
		{"entity_id": "a.b.c.d.e.2", "similarity": 0.8},
	}}
	fake := &fakeRequester{resp: mustJSON(t, resp)}
	c := New(fake, time.Second)

	ids, err := c.Resolve(context.Background(), fusion.ResolveQuery{Query: "find me a widget", Mode: fusion.ResolveModeNL, Limit: 10})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if fake.lastSubject != subjectSemantic {
		t.Errorf("subject = %q, want %q", fake.lastSubject, subjectSemantic)
	}
	if want := []string{"a.b.c.d.e.1", "a.b.c.d.e.2"}; !equalStrings(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
}

func TestResolve_Semantic_UnscopedByteParity(t *testing.T) {
	// An unscoped NL request body must be byte-identical to the pre-scope wire
	// shape {"query","limit"} — no "scope" key — so every existing caller and
	// every un-migrated server sees exactly today's bytes (ADR-071).
	fake := &fakeRequester{resp: mustJSON(t, map[string]any{"results": []map[string]any{}})}
	c := New(fake, time.Second)
	if _, err := c.Resolve(context.Background(), fusion.ResolveQuery{Query: "find a widget", Mode: fusion.ResolveModeNL, Limit: 10}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := mustJSON(t, map[string]any{"query": "find a widget", "limit": 10})
	if string(fake.lastData) != string(want) {
		t.Errorf("unscoped body = %s, want %s", fake.lastData, want)
	}
}

func TestResolve_Semantic_ScopeInBody(t *testing.T) {
	// A non-empty Scope adds "scope" to the NL request body.
	fake := &fakeRequester{resp: mustJSON(t, map[string]any{"results": []map[string]any{}})}
	c := New(fake, time.Second)
	scope := []string{"c360.semspec.source.doc"}
	if _, err := c.Resolve(context.Background(), fusion.ResolveQuery{Query: "find a widget", Mode: fusion.ResolveModeNL, Scope: scope, Limit: 10}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(fake.lastData, &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	got, ok := body["scope"].([]any)
	if !ok || len(got) != 1 || got[0] != scope[0] {
		t.Errorf("scope in body = %v, want [%q]", body["scope"], scope[0])
	}
}

func TestResolve_SemanticInvalidScopeFailsBeforeNATS(t *testing.T) {
	t.Parallel()

	fake := &fakeRequester{}
	c := New(fake, time.Second)
	_, err := c.Resolve(context.Background(), fusion.ResolveQuery{
		Query: "find a widget",
		Mode:  fusion.ResolveModeNL,
		Scope: []string{"acme.*"},
		Limit: 10,
	})
	require.Error(t, err)
	var classified *errs.ClassifiedError
	require.True(t, errors.As(err, &classified))
	assert.Equal(t, semtypes.ErrorCodeEntityIDPrefixInvalid, classified.Code)
	assert.Empty(t, fake.lastSubject, "invalid scope must fail before a NATS request")
}

func TestResolve_SemanticEmptyScopeEntryRemainsMatchAll(t *testing.T) {
	t.Parallel()

	fake := &fakeRequester{resp: mustJSON(t, map[string]any{"results": []map[string]any{}})}
	c := New(fake, time.Second)
	_, err := c.Resolve(context.Background(), fusion.ResolveQuery{
		Query: "find a widget",
		Mode:  fusion.ResolveModeNL,
		Scope: []string{""},
		Limit: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, subjectSemantic, fake.lastSubject)
}

func TestResolve_Symbol_CarriesNoScope(t *testing.T) {
	// Scope is NL-only: a symbol resolve never carries it even when set.
	fake := &fakeRequester{resp: mustJSON(t, graph.NewQueryResponse(graph.NameData{}))}
	c := New(fake, time.Second)
	if _, err := c.Resolve(context.Background(), fusion.ResolveQuery{Query: "Widget", Mode: fusion.ResolveModeSymbol, Scope: []string{"a.b.c"}, Limit: 10}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(fake.lastData, &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if _, ok := body["scope"]; ok {
		t.Errorf("symbol request carried scope: %s", fake.lastData)
	}
}

func TestResolve_UnknownMode(t *testing.T) {
	c := New(&fakeRequester{}, time.Second)
	_, err := c.Resolve(context.Background(), fusion.ResolveQuery{Query: "x", Mode: fusion.ResolveMode("bogus"), Limit: 10})
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestEntity_Found(t *testing.T) {
	es := graph.EntityState{ID: "a.b.c.d.e.1", Triples: []message.Triple{
		{Subject: "a.b.c.d.e.1", Predicate: "dc.terms.title", Object: "Widget"},
	}}
	fake := &fakeRequester{resp: mustJSON(t, es)}
	c := New(fake, time.Second)

	ent, err := c.Entity(context.Background(), "a.b.c.d.e.1")
	if err != nil {
		t.Fatalf("Entity: %v", err)
	}
	if fake.lastSubject != subjectEntity {
		t.Errorf("subject = %q, want %q", fake.lastSubject, subjectEntity)
	}
	if ent == nil || ent.ID != "a.b.c.d.e.1" {
		t.Fatalf("entity = %+v, want id a.b.c.d.e.1", ent)
	}
	if ent.First("dc.terms.title") != "Widget" {
		t.Errorf("title = %q, want Widget", ent.First("dc.terms.title"))
	}
}

func TestEntity_NotFoundIsAbsence(t *testing.T) {
	fake := &fakeRequester{err: errs.ClassifiedCode(errs.ErrorInvalid, graph.ErrorCodeEntityNotFound, errors.New("not found: x"))}
	c := New(fake, time.Second)

	ent, err := c.Entity(context.Background(), "missing")
	if err != nil {
		t.Fatalf("not-found must be (nil,nil), got err: %v", err)
	}
	if ent != nil {
		t.Errorf("entity = %+v, want nil", ent)
	}
}

func TestEntity_BackendErrorPropagates(t *testing.T) {
	fake := &fakeRequester{err: errs.ClassifiedCode(errs.ErrorTransient, graph.ErrorCodeInternal, errors.New("boom"))}
	c := New(fake, time.Second)

	_, err := c.Entity(context.Background(), "x")
	if err == nil {
		t.Fatal("backend error must propagate, not be swallowed as absence")
	}
}

func TestEntities_BatchDecodes(t *testing.T) {
	resp := map[string]any{"entities": []graph.EntityState{
		{ID: "a.b.c.d.e.1"}, {ID: "a.b.c.d.e.2"},
	}}
	fake := &fakeRequester{resp: mustJSON(t, resp)}
	c := New(fake, time.Second)

	ents, err := c.Entities(context.Background(), []string{"a.b.c.d.e.1", "a.b.c.d.e.2"})
	if err != nil {
		t.Fatalf("Entities: %v", err)
	}
	if fake.lastSubject != subjectBatch {
		t.Errorf("subject = %q, want %q", fake.lastSubject, subjectBatch)
	}
	if len(ents) != 2 {
		t.Fatalf("got %d entities, want 2", len(ents))
	}
}

func TestEntities_EmptyShortCircuits(t *testing.T) {
	fake := &fakeRequester{err: errors.New("should not be called")}
	c := New(fake, time.Second)

	ents, err := c.Entities(context.Background(), nil)
	if err != nil {
		t.Fatalf("Entities(nil): %v", err)
	}
	if ents != nil {
		t.Errorf("entities = %v, want nil", ents)
	}
	if fake.lastSubject != "" {
		t.Error("empty IDs must not hit the wire")
	}
}

func TestNeighbors_OutgoingFiltersPredicates(t *testing.T) {
	resp := []map[string]any{
		{"from_entity_id": "seed", "to_entity_id": "callee1", "edge_type": "code.calls"},
		{"from_entity_id": "seed", "to_entity_id": "other", "edge_type": "code.imports"},
		{"from_entity_id": "seed", "to_entity_id": "callee2", "edge_type": "code.calls"},
	}
	fake := &fakeRequester{resp: mustJSON(t, resp)}
	c := New(fake, time.Second)

	edges, err := c.Neighbors(context.Background(), "seed", []string{"code.calls"}, fusion.Outgoing)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if fake.lastSubject != subjectRelationships {
		t.Errorf("subject = %q, want %q", fake.lastSubject, subjectRelationships)
	}
	// direction must be on the wire as "outgoing"
	var req map[string]string
	_ = json.Unmarshal(fake.lastData, &req)
	if req["direction"] != "outgoing" {
		t.Errorf("direction = %q, want outgoing", req["direction"])
	}
	if len(edges) != 2 {
		t.Fatalf("got %d edges, want 2 (code.imports filtered out)", len(edges))
	}
	// outgoing target is to_entity_id
	if edges[0].Target != "callee1" || edges[0].Predicate != "code.calls" {
		t.Errorf("edge[0] = %+v, want callee1/code.calls", edges[0])
	}
}

func TestNeighbors_IncomingTargetsFromEnd(t *testing.T) {
	resp := []map[string]any{
		{"from_entity_id": "caller1", "to_entity_id": "seed", "edge_type": "code.calls"},
	}
	fake := &fakeRequester{resp: mustJSON(t, resp)}
	c := New(fake, time.Second)

	edges, err := c.Neighbors(context.Background(), "seed", []string{"code.calls"}, fusion.Incoming)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	var req map[string]string
	_ = json.Unmarshal(fake.lastData, &req)
	if req["direction"] != "incoming" {
		t.Errorf("direction = %q, want incoming", req["direction"])
	}
	if len(edges) != 1 || edges[0].Target != "caller1" {
		t.Fatalf("incoming target should be from_entity_id; got %+v", edges)
	}
}

func TestNeighbors_NoPredicatesMeansNoFilter(t *testing.T) {
	resp := []map[string]any{
		{"from_entity_id": "seed", "to_entity_id": "x", "edge_type": "a"},
		{"from_entity_id": "seed", "to_entity_id": "y", "edge_type": "b"},
	}
	fake := &fakeRequester{resp: mustJSON(t, resp)}
	c := New(fake, time.Second)

	edges, err := c.Neighbors(context.Background(), "seed", nil, fusion.Outgoing)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("got %d edges, want 2 (no filter)", len(edges))
	}
}

func TestNames_DedupesAndCaps(t *testing.T) {
	fake := &fakeRequester{resp: mustJSON(t, graph.NewQueryResponse(graph.NameData{Matches: []graph.NameMatch{
		{EntityID: "1", MatchedName: "Widget"},
		{EntityID: "2", MatchedName: "Widget"}, // dup name, distinct entity
		{EntityID: "3", MatchedName: "Gadget"},
		{EntityID: "4", MatchedName: "Gizmo"},
	}}))}
	c := New(fake, time.Second)

	names, err := c.Names(context.Background(), "Wid", 2)
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	if fake.lastSubject != subjectByName {
		t.Errorf("subject = %q, want %q", fake.lastSubject, subjectByName)
	}
	if want := []string{"Widget", "Gadget"}; !equalStrings(names, want) {
		t.Errorf("names = %v, want %v (deduped, capped at 2)", names, want)
	}
}

func TestNew_DefaultsTimeout(t *testing.T) {
	c := New(&fakeRequester{}, 0)
	if c.timeout != defaultTimeout {
		t.Errorf("timeout = %v, want default %v", c.timeout, defaultTimeout)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
