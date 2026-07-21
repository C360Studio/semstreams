package fusionnats

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/readiness"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/pkg/fusion"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// statusWaitTest is the client timeout the readiness cases run with. Status spends it
// as the one-shot wait for the watch's first delivery, so the rows where nothing ever
// arrives spend all of it; small keeps the table quick without changing any outcome.
const statusWaitTest = 50 * time.Millisecond

// fakeRequester records the last request and returns a canned reply, or routes
// per-subject through responder when set.
//
// It also serves the GRAPH_STATUS KV bucket, because ADR-083 moved Status off the
// subject onto a KV watch and the production transport (*natsclient.Client) provides
// both faces from one object. Tests that do not touch Status leave the status fields
// zero; a zero fakeRequester serves a key that was never published, which is the honest
// "graph-index is not publishing" shape.
type fakeRequester struct {
	lastSubject string
	lastData    []byte
	resp        []byte
	err         error
	responder   func(subject string, data []byte) ([]byte, error)

	// statusValue is the raw GRAPH_STATUS value the watch delivers on bind;
	// statusCreated is its commit time (the freshness input). A nil statusValue models
	// a key the producer never wrote. statusDeleted delivers it as a tombstone
	// instead. watchErr fails the watch; bucketErr fails the bucket open.
	statusValue   []byte
	statusCreated time.Time
	statusDeleted bool
	watchErr      error
	bucketErr     error

	// mu guards the recorded coordinates below. The watch binds on its own goroutine,
	// so a test reading them would race the bind without it.
	mu               sync.Mutex
	lastStatusBucket string
	lastStatusKey    string
}

func (f *fakeRequester) RequestClassified(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
	f.lastSubject = subject
	f.lastData = append([]byte(nil), data...)
	if f.responder != nil {
		return f.responder(subject, data)
	}
	return f.resp, f.err
}

// GetKeyValueBucket is the narrow readiness.BucketSource capability the production
// transport already has.
func (f *fakeRequester) GetKeyValueBucket(_ context.Context, name string) (jetstream.KeyValue, error) {
	f.mu.Lock()
	f.lastStatusBucket = name
	f.mu.Unlock()
	if f.bucketErr != nil {
		return nil, f.bucketErr
	}
	return &fakeStatusBucket{owner: f}, nil
}

func (f *fakeRequester) statusBucketName() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastStatusBucket
}

func (f *fakeRequester) statusKey() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastStatusKey
}

// fakeStatusBucket serves the single status key. It embeds jetstream.KeyValue so it
// satisfies the interface without the unused methods; ONLY Watch is reached, because
// the readiness watcher holds state rather than polling — a Get here would nil-panic,
// which is the intended loud failure if that ever changes.
type fakeStatusBucket struct {
	jetstream.KeyValue
	owner *fakeRequester
}

func (b *fakeStatusBucket) Watch(_ context.Context, key string, _ ...jetstream.WatchOpt) (jetstream.KeyWatcher, error) {
	o := b.owner
	o.mu.Lock()
	o.lastStatusKey = key
	o.mu.Unlock()
	if o.watchErr != nil {
		return nil, o.watchErr
	}
	// Cap 2 so the pre-load never blocks: the current value (when there is one) plus
	// the nil end-of-initial-values marker a real watch always sends after it. The
	// channel then STAYS OPEN — closing it would end the watch and exercise the
	// rebind loop, a different scenario than the ones under test.
	updates := make(chan jetstream.KeyValueEntry, 2)
	if o.statusValue != nil {
		created := o.statusCreated
		if created.IsZero() {
			created = time.Now()
		}
		op := jetstream.KeyValuePut
		if o.statusDeleted {
			op = jetstream.KeyValueDelete
		}
		updates <- fakeStatusEntry{value: o.statusValue, created: created, op: op}
	}
	updates <- nil
	return &fakeStatusFeed{updates: updates}, nil
}

// fakeStatusFeed is a jetstream.KeyWatcher over a pre-loaded channel.
type fakeStatusFeed struct {
	updates chan jetstream.KeyValueEntry
}

func (f *fakeStatusFeed) Updates() <-chan jetstream.KeyValueEntry { return f.updates }
func (f *fakeStatusFeed) Stop() error                             { return nil }

// fakeStatusEntry is a KV entry with a controllable commit time and operation.
type fakeStatusEntry struct {
	value   []byte
	created time.Time
	op      jetstream.KeyValueOp
}

func (e fakeStatusEntry) Bucket() string                  { return readiness.BucketGraphStatus }
func (e fakeStatusEntry) Key() string                     { return readiness.KeyGraphIndex }
func (e fakeStatusEntry) Value() []byte                   { return e.value }
func (e fakeStatusEntry) Revision() uint64                { return 1 }
func (e fakeStatusEntry) Created() time.Time              { return e.created }
func (e fakeStatusEntry) Delta() uint64                   { return 0 }
func (e fakeStatusEntry) Operation() jetstream.KeyValueOp { return e.op }

// publishing builds a transport whose GRAPH_STATUS key holds st, written now.
func publishing(t *testing.T, st graph.IndexStatusResponse) *fakeRequester {
	t.Helper()
	return &fakeRequester{statusValue: mustJSON(t, st)}
}

// publishingRaw is publishing for a wire value that no current producer can emit —
// notably an envelope from a producer predating a field, which a typed literal cannot
// express because the Go struct always marshals the field.
func publishingRaw(t *testing.T, value []byte) *fakeRequester {
	t.Helper()
	return &fakeRequester{statusValue: value}
}

// newStatusClient builds a client and stops its readiness watch when the test ends, so
// no watch goroutine outlives the case that started it.
func newStatusClient(t *testing.T, transport requester, timeout time.Duration) *Client {
	t.Helper()
	c := New(transport, timeout)
	t.Cleanup(c.Close)
	return c
}

// subjectOnlyTransport has RequestClassified but NOT the KV capability — the shape a
// sister repo would have if it handed this client a request-only wrapper.
type subjectOnlyTransport struct{}

func (subjectOnlyTransport) RequestClassified(context.Context, string, []byte, time.Duration) ([]byte, error) {
	return nil, errors.New("not used")
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
	fake := publishing(t, graph.IndexStatusResponse{
		Ready: true, State: graph.IndexStateReady,
		IndexedRevision: 100, TargetRevision: 100, Lag: 0, Phase: "ready",
		Revision: "100", LastSynced: "now",
	})
	c := newStatusClient(t, fake, time.Second)

	got, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	// The bucket/key coordinates are the cross-repo contract (`nats kv get
	// GRAPH_STATUS graph-index`); pin them where the subject used to be pinned. A
	// successful Status means the watch delivered, so the coordinates are recorded.
	if fake.statusBucketName() != readiness.BucketGraphStatus {
		t.Errorf("bucket = %q, want %q", fake.statusBucketName(), readiness.BucketGraphStatus)
	}
	if fake.statusKey() != readiness.KeyGraphIndex {
		t.Errorf("key = %q, want %q", fake.statusKey(), readiness.KeyGraphIndex)
	}
	if fake.lastSubject != "" {
		t.Errorf("Status issued a request on %q; readiness must not touch a subject", fake.lastSubject)
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

// TestStatus_StalenessSurvivesProductionDecode is the lockstep guard for the ADR-083
// additive field: graph.IndexStatusResponse and fusion.IndexStatus change together,
// and the proof runs through the PRODUCTION decoder (Client.Status) rather than a
// hand-rolled unmarshal — the same shape of guard that caught IndexedRevision/Lag
// being silently dropped by an earlier hand-copied remap.
func TestStatus_StalenessSurvivesProductionDecode(t *testing.T) {
	// The producer's real projection, not a hand-built literal, so a change to how
	// staleness is computed or tagged fails here too.
	envelope := graph.ComputeIndexStatus(graph.IndexStatusInputs{
		Indexed: 40, Target: 100,
		IndexedAt: time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC),
		Now:       time.Date(2026, 7, 20, 12, 0, 2, 500*int(time.Millisecond), time.UTC),
	})
	if envelope.StalenessMs != 2500 {
		t.Fatalf("precondition: producer StalenessMs = %d, want 2500", envelope.StalenessMs)
	}

	c := newStatusClient(t, publishing(t, envelope), time.Second)
	got, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if got.StalenessMs != 2500 {
		t.Errorf("StalenessMs = %d, want 2500 (dropped on the wire or in the decode)", got.StalenessMs)
	}

	// A Ready envelope omits the field; the decode must leave it 0 rather than
	// inventing a value, since 0 is the "no information" encoding.
	readyClient := newStatusClient(t, publishing(t, graph.ComputeIndexStatus(
		graph.IndexStatusInputs{Indexed: 100, Target: 100})), time.Second)
	readyStatus, err := readyClient.Status(context.Background())
	if err != nil {
		t.Fatalf("Status (ready): %v", err)
	}
	if readyStatus.StalenessMs != 0 || !readyStatus.Ready {
		t.Errorf("ready status = %+v, want Ready with StalenessMs 0", readyStatus)
	}
}

// TestStatus_RevisionLagFieldsSurvive is the explicit guard that a mid-build
// envelope (not caught up) round-trips its numeric fields — a consumer gating on
// Lag must see the real lag, not a dropped-to-zero false-ready.
func TestStatus_RevisionLagFieldsSurvive(t *testing.T) {
	fake := publishing(t, graph.IndexStatusResponse{
		Ready: false, State: graph.IndexStateBuilding,
		IndexedRevision: 40, TargetRevision: 100, Lag: 60, Revision: "40",
	})
	c := newStatusClient(t, fake, time.Second)

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
	fake := publishing(t, graph.IndexStatusResponse{Ready: false, State: graph.IndexStateBuilding})
	c := newStatusClient(t, fake, time.Second)

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

// TestStatus_UnknownReadinessIsAnError is the bit-compatibility guard for the ADR-083
// transport move. Every way readiness could fail to be established used to surface as
// an error from the status request, which Fuse's top gate propagates as a hard error
// (never as a degrade, and never as a zero-valued Ready=false success). Each row is one
// of those ways in its post-move shape; the assertion is that they are all still
// errors, so Fuse's inputs are unchanged.
func TestStatus_UnknownReadinessIsAnError(t *testing.T) {
	ready := graph.IndexStatusResponse{Ready: true, State: graph.IndexStateReady}
	staleWindow := readiness.FreshnessWindow(readiness.DefaultHeartbeat)

	tests := []struct {
		name      string
		transport requester
	}{
		{"bucket absent", &fakeRequester{bucketErr: errors.New("bucket not found")}},
		{"key absent (producer never published)", &fakeRequester{}},
		{
			// A tombstone is not evidence of a live, ready producer, so it must land
			// on the same unknown branch an absent key does.
			name:      "key deleted",
			transport: &fakeRequester{statusValue: mustJSON(t, ready), statusDeleted: true},
		},
		{"backend fault", &fakeRequester{watchErr: errors.New("kv unavailable")}},
		{"undecodable value", &fakeRequester{statusValue: []byte("{not json")}},
		{
			// The one case the move adds: request/reply made a dead producer a
			// no-responders error, and its last Ready=true key must not now license
			// authoritative-absence claims against a frozen index.
			name: "stale Ready key (dead producer)",
			transport: &fakeRequester{
				statusValue:   mustJSON(t, ready),
				statusCreated: time.Now().Add(-staleWindow - time.Second),
			},
		},
		{"transport without KV capability", subjectOnlyTransport{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newStatusClient(t, tt.transport, statusWaitTest).Status(context.Background())
			if err == nil {
				t.Fatalf("want an error so Fuse propagates it; got status %+v", got)
			}
			if got != (fusion.IndexStatus{}) {
				t.Errorf("an unestablished readiness must return the zero status, got %+v", got)
			}
		})
	}
}

// TestStatus_FreshKeyIsAccepted is the negative control for the staleness guard above:
// an envelope written within the freshness window reads normally, so the guard cannot
// be satisfied by simply failing every read.
func TestStatus_FreshKeyIsAccepted(t *testing.T) {
	ready := graph.IndexStatusResponse{Ready: true, State: graph.IndexStateReady,
		IndexedRevision: 100, TargetRevision: 100}
	window := readiness.FreshnessWindow(readiness.DefaultHeartbeat)

	for _, age := range []time.Duration{0, readiness.DefaultHeartbeat, window - time.Second} {
		fake := &fakeRequester{statusValue: mustJSON(t, ready), statusCreated: time.Now().Add(-age)}
		got, err := newStatusClient(t, fake, time.Second).Status(context.Background())
		if err != nil {
			t.Fatalf("age %s: Status: %v", age, err)
		}
		if !got.Ready {
			t.Errorf("age %s: Ready = false, want true", age)
		}
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

func TestAuthoritativeRepliesRejectPoisonBeforeProjection(t *testing.T) {
	t.Parallel()

	validID := "acme.ops.test.system.widget.001"
	invalidEntityID := "bad"
	tests := []struct {
		name string
		resp any
		call func(*Client) (any, error)
	}{
		{
			name: "prefix malformed root",
			resp: graph.PrefixQueryResponse{Entities: []graph.EntityState{{ID: validID}, {ID: invalidEntityID}}},
			call: func(c *Client) (any, error) {
				return c.Resolve(context.Background(), fusion.ResolveQuery{Query: "acme.ops", Mode: fusion.ResolveModePrefix, Limit: 10})
			},
		},
		{
			name: "entity malformed subject",
			resp: graph.EntityState{ID: validID, Triples: []message.Triple{{Subject: invalidEntityID, Predicate: "test.state.value"}}},
			call: func(c *Client) (any, error) {
				return c.Entity(context.Background(), validID)
			},
		},
		{
			name: "batch malformed reference",
			resp: map[string]any{"entities": []graph.EntityState{
				{ID: validID},
				{ID: validID, Triples: []message.Triple{{
					Subject: validID, Predicate: "test.state.target", Object: invalidEntityID, Datatype: message.EntityReferenceDatatype,
				}}},
			}},
			call: func(c *Client) (any, error) {
				return c.Entities(context.Background(), []string{validID})
			},
		},
		{
			name: "semantic malformed entity id",
			resp: map[string]any{"results": []map[string]any{{"entity_id": validID}, {"entity_id": invalidEntityID}}},
			call: func(c *Client) (any, error) {
				return c.Resolve(context.Background(), fusion.ResolveQuery{Query: "widget", Mode: fusion.ResolveModeNL, Limit: 10})
			},
		},
		{
			name: "relationship malformed endpoint",
			resp: []map[string]any{{
				"from_entity_id": validID, "to_entity_id": invalidEntityID, "edge_type": "test.state.target",
			}},
			call: func(c *Client) (any, error) {
				return c.Neighbors(context.Background(), validID, nil, fusion.Outgoing)
			},
		},
		{
			name: "name match malformed entity id",
			resp: graph.NewQueryResponse(graph.NameData{Matches: []graph.NameMatch{{EntityID: invalidEntityID, MatchedName: "Widget"}}}),
			call: func(c *Client) (any, error) {
				return c.Names(context.Background(), "Widget", 10)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := New(&fakeRequester{resp: mustJSON(t, tt.resp)}, time.Second)
			got, err := tt.call(client)
			require.Error(t, err)
			assert.True(t, graph.IsStateContractError(err))
			assert.Nil(t, got, "no partial projection may escape")
			var classified *errs.ClassifiedError
			require.ErrorAs(t, err, &classified)
			assert.Equal(t, errs.ErrorFatal, classified.Class)
			assert.Equal(t, graph.ErrorCodeGraphStateResetRequired, classified.Code)
		})
	}
}

func TestNeighbors_OutgoingFiltersPredicates(t *testing.T) {
	resp := []map[string]any{
		{"from_entity_id": "a.b.c.d.e.seed", "to_entity_id": "a.b.c.d.e.callee1", "edge_type": "code.calls"},
		{"from_entity_id": "a.b.c.d.e.seed", "to_entity_id": "a.b.c.d.e.other", "edge_type": "code.imports"},
		{"from_entity_id": "a.b.c.d.e.seed", "to_entity_id": "a.b.c.d.e.callee2", "edge_type": "code.calls"},
	}
	fake := &fakeRequester{resp: mustJSON(t, resp)}
	c := New(fake, time.Second)

	edges, err := c.Neighbors(context.Background(), "a.b.c.d.e.seed", []string{"code.calls"}, fusion.Outgoing)
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
	if edges[0].Target != "a.b.c.d.e.callee1" || edges[0].Predicate != "code.calls" {
		t.Errorf("edge[0] = %+v, want callee1/code.calls", edges[0])
	}
}

func TestNeighbors_IncomingTargetsFromEnd(t *testing.T) {
	resp := []map[string]any{
		{"from_entity_id": "a.b.c.d.e.caller1", "to_entity_id": "a.b.c.d.e.seed", "edge_type": "code.calls"},
	}
	fake := &fakeRequester{resp: mustJSON(t, resp)}
	c := New(fake, time.Second)

	edges, err := c.Neighbors(context.Background(), "a.b.c.d.e.seed", []string{"code.calls"}, fusion.Incoming)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	var req map[string]string
	_ = json.Unmarshal(fake.lastData, &req)
	if req["direction"] != "incoming" {
		t.Errorf("direction = %q, want incoming", req["direction"])
	}
	if len(edges) != 1 || edges[0].Target != "a.b.c.d.e.caller1" {
		t.Fatalf("incoming target should be from_entity_id; got %+v", edges)
	}
}

func TestNeighbors_NoPredicatesMeansNoFilter(t *testing.T) {
	resp := []map[string]any{
		{"from_entity_id": "a.b.c.d.e.seed", "to_entity_id": "a.b.c.d.e.x", "edge_type": "a"},
		{"from_entity_id": "a.b.c.d.e.seed", "to_entity_id": "a.b.c.d.e.y", "edge_type": "b"},
	}
	fake := &fakeRequester{resp: mustJSON(t, resp)}
	c := New(fake, time.Second)

	edges, err := c.Neighbors(context.Background(), "a.b.c.d.e.seed", nil, fusion.Outgoing)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("got %d edges, want 2 (no filter)", len(edges))
	}
}

func TestNames_DedupesAndCaps(t *testing.T) {
	fake := &fakeRequester{resp: mustJSON(t, graph.NewQueryResponse(graph.NameData{Matches: []graph.NameMatch{
		{EntityID: "a.b.c.d.e.1", MatchedName: "Widget"},
		{EntityID: "a.b.c.d.e.2", MatchedName: "Widget"}, // dup name, distinct entity
		{EntityID: "a.b.c.d.e.3", MatchedName: "Gadget"},
		{EntityID: "a.b.c.d.e.4", MatchedName: "Gizmo"},
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

// entity-id-audit:classify intentional-malformed "bad" line=644 column=21 surface=go-assignment:invalidEntityID entity_id_invalid:arity authoritative reply poison fixtures

// TestStatus_BootstrapCompleteSurvivesProductionDecode is the lockstep guard for the
// ADR-084 D2 bit: graph.IndexStatusResponse and fusion.IndexStatus change together, and
// the proof runs through the PRODUCTION decoder rather than a hand-rolled unmarshal.
// A dropped bit here reads as bootstrap_complete=false downstream, which fails closed —
// silently deferring every health-gated read against a perfectly healthy index.
func TestStatus_BootstrapCompleteSurvivesProductionDecode(t *testing.T) {
	for _, bootstrapped := range []bool{true, false} {
		envelope := graph.IndexStatusResponse{
			Ready: false, State: graph.IndexStateBuilding,
			IndexedRevision: 40, TargetRevision: 100, Lag: 60,
			BootstrapComplete: bootstrapped,
		}
		c := newStatusClient(t, publishing(t, envelope), time.Second)
		got, err := c.Status(context.Background())
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if got.BootstrapComplete != bootstrapped {
			t.Errorf("BootstrapComplete = %v, want %v (dropped on the wire or in the decode)",
				got.BootstrapComplete, bootstrapped)
		}
	}

	// A producer that predates the field decodes to false — the fail-closed migration
	// contract, asserted through the real decoder because that is where a default
	// would sneak in.
	legacy := newStatusClient(t, publishingRaw(t, []byte(`{"ready":true,"state":"ready"}`)), time.Second)
	got, err := legacy.Status(context.Background())
	if err != nil {
		t.Fatalf("Status (legacy): %v", err)
	}
	if got.BootstrapComplete {
		t.Error("legacy envelope decoded bootstrap_complete=true; health gates would fail OPEN")
	}
}

// TestStatus_UnknownIsTypedButWiringIsNot pins the ADR-084 D6 split. ADR-083 collapsed
// every readiness failure into one error because a request/reply could not tell them
// apart; held state can, and the two want different responses:
//
//   - a feed we cannot vouch for is a DEGRADE — the engine turns it into an honest
//     empty envelope, which is the correct answer to "is the graph ready" when nobody
//     is answering;
//   - broken wiring is an operator BUG that does not heal, and must not spend the rest
//     of the deployment's life masquerading as "the graph is busy".
func TestStatus_UnknownIsTypedButWiringIsNot(t *testing.T) {
	t.Run("never published is typed unknown", func(t *testing.T) {
		c := newStatusClient(t, &fakeRequester{}, statusWaitTest)
		_, err := c.Status(context.Background())
		require.Error(t, err)
		assert.ErrorIs(t, err, fusion.ErrReadinessUnknown,
			"a producer that never published must degrade, not crash the caller")
	})

	t.Run("a quiet feed past the freshness window is typed unknown", func(t *testing.T) {
		stale := publishing(t, graph.IndexStatusResponse{
			Ready: true, State: graph.IndexStateReady, BootstrapComplete: true,
		})
		stale.statusCreated = time.Now().
			Add(-readiness.FreshnessWindow(readiness.DefaultHeartbeat) - time.Second)
		c := newStatusClient(t, stale, statusWaitTest)

		_, err := c.Status(context.Background())
		require.Error(t, err, "a dead producer's last Ready key must not be served forever")
		assert.ErrorIs(t, err, fusion.ErrReadinessUnknown)
	})

	t.Run("an undecodable value is typed unknown", func(t *testing.T) {
		// The transport worked and delivered something — that is a readiness we cannot
		// vouch for, not a wiring failure.
		c := newStatusClient(t, publishingRaw(t, []byte("{not json")), statusWaitTest)
		_, err := c.Status(context.Background())
		require.Error(t, err)
		assert.ErrorIs(t, err, fusion.ErrReadinessUnknown)
	})

	t.Run("a transport without the KV capability is NOT typed unknown", func(t *testing.T) {
		c := newStatusClient(t, subjectOnlyTransport{}, statusWaitTest)
		_, err := c.Status(context.Background())
		require.Error(t, err)
		assert.NotErrorIs(t, err, fusion.ErrReadinessUnknown,
			"broken wiring must stay loud — degrading it would let a misconfigured "+
				"deployment serve honest-looking empty envelopes forever")
	})
}
