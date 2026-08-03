package embedding

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/c360studio/semstreams/storage"
	"github.com/c360studio/semstreams/storage/storeregistry"
)

// terminalReport is one hop-2 terminal outcome captured from the production callback.
type terminalReport struct {
	outcome TerminalOutcome
	reason  string
}

// runHop2 drives the REAL hop-2 wire — Worker.Start → WatchAll → handleKVEntry — over
// an offloaded pending record, and returns the terminal it reported plus the durable
// record left behind. Both branches of the gh#875 split are asserted through it, so
// neither can be verified against a helper the production path does not use.
func runHop2(t *testing.T, w *Worker, s *Storage, index *watchableKV, rec *Record) (terminalReport, *Record) {
	t.Helper()

	done := make(chan terminalReport, 4)
	w.WithOnTerminal(func(_ string, _ uint64, o TerminalOutcome, r string) {
		done <- terminalReport{outcome: o, reason: r}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = w.Stop() }()

	if err := s.SavePendingWithStorageRef(ctx, rec.EntityID, rec.ContentHash, rec.IdentityText,
		rec.StorageRef, nil, rec.SourceRevision); err != nil {
		t.Fatalf("SavePendingWithStorageRef: %v", err)
	}

	var got terminalReport
	select {
	case got = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a hop-2 terminal outcome")
	}

	stored, err := s.GetEmbedding(context.Background(), rec.EntityID)
	if err != nil && !errors.Is(err, ErrRecordGone) {
		t.Fatalf("GetEmbedding: %v", err)
	}
	_ = index
	return got, stored
}

func offloadedPending(instance string) *Record {
	return &Record{
		EntityID:       "acme.ops.docs.sys.doc.001",
		IdentityText:   "hydraulic pump service report",
		StorageRef:     &StorageRef{StorageInstance: instance, Key: "loops/x/step/1"},
		SourceRevision: 7,
	}
}

func okEmbedder() *stubEmbedder {
	return &stubEmbedder{model: "m", dimensions: 3, generate: func(texts []string) ([][]float32, error) {
		out := make([][]float32, len(texts))
		for i := range texts {
			out[i] = []float32{1, 2, 3}
		}
		return out, nil
	}}
}

// TestHop2_UnresolvableInstanceExcludesAndEmbedsInlineText is the gh#875 core: an
// offloaded record naming a storage instance NO wired store serves must NOT become a
// durable failed embedding. Re-delivery cannot register a missing store, so a failed
// record here is permanent — it re-seeds on restart and pins the index Degraded with
// no exit but deleting the entity. It must instead be reported through the
// excluded-content path and still reach a terminal outcome from its inline text.
func TestHop2_UnresolvableInstanceExcludesAndEmbedsInlineText(t *testing.T) {
	index := newWatchableKV()
	s := NewStorage(index, newMemKV())
	m := &recordingWorkerMetrics{}

	w := NewWorker(s, okEmbedder(), index, discardLogger()).
		WithWorkers(1).
		WithMaxSourceTextLen(4000).
		WithMetrics(m).
		// The worker OWNS a store for a different instance, the exact shape that used
		// to answer for every instance and open the reference against a foreign bucket.
		WithContentStore(readerStore{data: "WRONG BUCKET BODY", instance: "MESSAGES"})

	got, stored := runHop2(t, w, s, index, offloadedPending("AGENT_CONTENT"))

	if got.outcome == OutcomeFailed {
		t.Fatalf("terminal = failed(%q): an unreachable body is an exclusion, not a failure", got.reason)
	}
	if stored != nil && stored.Status == StatusFailed {
		t.Fatalf("durable record is %q: no failed record may be written for an unresolvable instance", stored.Status)
	}
	if stored == nil || stored.Status != StatusGenerated {
		t.Fatalf("stored = %+v, want a generated vector from the record's INLINE identity text", stored)
	}
	// A QUALIFIED success: the vector is real and servable, and the record says what is
	// missing from it. Without the qualifier the record is indistinguishable from a
	// complete one — unenumerable, and frozen by SavePendingGuarded's skip.
	if stored.Reason != ReasonContentExcluded {
		t.Fatalf("stored reason = %q, want %q: a degraded success must record its qualifier",
			stored.Reason, ReasonContentExcluded)
	}
	if len(stored.Vector) == 0 {
		t.Fatal("the qualified success must still store its vector — the inline text WAS embedded")
	}

	snap := m.snapshot()
	if snap.excluded != 1 {
		t.Fatalf("excluded reports = %d, want 1: a silent skip is not acceptable", snap.excluded)
	}
	if snap.excludedRefs[0].instance != "AGENT_CONTENT" {
		t.Errorf("exclusion must name the instance an operator has to wire; got %q", snap.excludedRefs[0].instance)
	}
	if snap.excludedRefs[0].entityID != "acme.ops.docs.sys.doc.001" {
		t.Errorf("exclusion must name the entity; got %q", snap.excludedRefs[0].entityID)
	}
	if len(snap.failedReasons) != 0 {
		t.Errorf("no failure reason may be counted for a wiring gap; got %v", snap.failedReasons)
	}
	if snap.resolveErr != 0 {
		t.Errorf("content_resolve_error counts a RESOLVED store's read fault; got %d", snap.resolveErr)
	}
}

// TestHop2_ResolvedStoreReadFailureStillFails is the other branch, and the reason it
// is tested beside the first: a test that only covered the new exclusion path could
// not detect that the OLD failure path was swallowed along with it. A store that
// resolves and then fails to read is an infra fault that genuinely recovers on
// re-delivery, so it stays a reason-classified, counted, durable failure.
func TestHop2_ResolvedStoreReadFailureStillFails(t *testing.T) {
	index := newWatchableKV()
	s := NewStorage(index, newMemKV())
	m := &recordingWorkerMetrics{}

	reg := storeregistry.New()
	if err := reg.Register("AGENT_CONTENT", readerStore{openErr: errors.New("bucket deleted")}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	w := NewWorker(s, okEmbedder(), index, discardLogger()).
		WithWorkers(1).
		WithMaxSourceTextLen(4000).
		WithMetrics(m).
		WithStoreResolver(reg)

	got, stored := runHop2(t, w, s, index, offloadedPending("AGENT_CONTENT"))

	if got.outcome != OutcomeFailed || got.reason != failReasonContentError {
		t.Fatalf("terminal = (%v, %q), want (OutcomeFailed, %q): a resolved store's read failure is still a failure",
			got.outcome, got.reason, failReasonContentError)
	}
	if stored == nil || stored.Status != StatusFailed {
		t.Fatalf("stored = %+v, want a DURABLE failed record so re-delivery re-processes it", stored)
	}
	if stored.Reason != failReasonContentError {
		t.Fatalf("stored reason = %q, want %q: the failure classification is unchanged by the qualifier reframe",
			stored.Reason, failReasonContentError)
	}
	snap := m.snapshot()
	if len(snap.failedReasons) != 1 || snap.failedReasons[0] != failReasonContentError {
		t.Errorf("failures_total{reason} = %v, want [content_error]", snap.failedReasons)
	}
	if snap.resolveErr != 1 {
		t.Errorf("content_resolve_error = %d, want 1 for a resolved store's read fault", snap.resolveErr)
	}
	if snap.excluded != 0 {
		t.Errorf("a read fault is NOT an exclusion; excluded reports = %d", snap.excluded)
	}
}

// TestHop2_StoreDeregisteredBetweenGateAndFetchExcludes is why the fix is in two
// places rather than one. The registry contract is per-fetch with no caching and
// deregistration on component Stop is live, so a store that resolved when hop 1
// decided to fetch can be gone by the time hop 2 reads. Bounding the gate alone would
// leave that race producing the same permanent durable-failed latch.
func TestHop2_StoreDeregisteredBetweenGateAndFetchExcludes(t *testing.T) {
	index := newWatchableKV()
	s := NewStorage(index, newMemKV())
	m := &recordingWorkerMetrics{}

	// Registered: the hop-1 gate's question resolves TRUE right now.
	reg := storeregistry.New()
	if err := reg.Register("AGENT_CONTENT", readerStore{data: "the offloaded body"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	var resolver StoreResolver = reg
	if _, ok := resolver.Streamable("AGENT_CONTENT"); !ok {
		t.Fatal("precondition: the instance must resolve at the gate")
	}

	w := NewWorker(s, okEmbedder(), index, discardLogger()).
		WithWorkers(1).
		WithMaxSourceTextLen(4000).
		WithMetrics(m).
		WithStoreResolver(reg).
		WithContentStore(readerStore{data: "WRONG BUCKET BODY", instance: "MESSAGES"})

	// The owning storage component Stops between the gate and the fetch.
	reg.Deregister("AGENT_CONTENT")

	got, stored := runHop2(t, w, s, index, offloadedPending("AGENT_CONTENT"))

	if got.outcome == OutcomeFailed {
		t.Fatalf("terminal = failed(%q): a store deregistered mid-flight must not latch a durable failure", got.reason)
	}
	if stored != nil && stored.Status == StatusFailed {
		t.Fatalf("durable record is %q, want no failed record", stored.Status)
	}
	if stored == nil || stored.Reason != ReasonContentExcluded {
		t.Fatalf("stored = %+v, want a generated record qualified %q — the race produces a degraded success too",
			stored, ReasonContentExcluded)
	}
	if snap := m.snapshot(); snap.excluded != 1 {
		t.Fatalf("excluded reports = %d, want 1: the race must be reported, not silent", snap.excluded)
	}
}

// TestHop2_UnresolvableInstanceWithNoInlineTextIsTerminalNotFailed covers the empty
// half of "still reaches a terminal outcome from whatever inline text it carries": an
// entity with an unreachable body and NO inline identity text has nothing to embed.
// That is the ordinary no-text terminal (the pending record is deleted), never a
// failure — the watermark still drains, so readiness cannot wedge.
func TestHop2_UnresolvableInstanceWithNoInlineTextIsTerminalNotFailed(t *testing.T) {
	index := newWatchableKV()
	s := NewStorage(index, newMemKV())
	m := &recordingWorkerMetrics{}

	w := NewWorker(s, okEmbedder(), index, discardLogger()).
		WithWorkers(1).
		WithMaxSourceTextLen(4000).
		WithMetrics(m).
		WithContentStore(readerStore{data: "WRONG BUCKET BODY", instance: "MESSAGES"})

	rec := offloadedPending("AGENT_CONTENT")
	rec.IdentityText = ""

	got, stored := runHop2(t, w, s, index, rec)

	if got.outcome != OutcomeSkipped {
		t.Fatalf("terminal = %v, want OutcomeSkipped (no text to embed is not a failure)", got.outcome)
	}
	if stored != nil {
		t.Fatalf("stored = %+v, want the pending record deleted by the no-text path", stored)
	}
	if snap := m.snapshot(); snap.excluded != 1 {
		t.Errorf("excluded reports = %d, want 1", snap.excluded)
	}
}

// TestResolveStore_BoundsTheOwnedFallback pins the resolver decision itself, so the
// bound is verifiable without a full hop-2 drive.
func TestResolveStore_BoundsTheOwnedFallback(t *testing.T) {
	owned := readerStore{data: "OWNED", instance: "MESSAGES"}
	reg := storeregistry.New()
	if err := reg.Register("content", readerStore{data: "REGISTERED"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	cases := []struct {
		name     string
		instance string
		want     storage.StreamableStore
	}{
		{"registry resolves the named instance", "content", readerStore{data: "REGISTERED"}},
		{"owned store answers for its OWN instance", "MESSAGES", owned},
		{"owned store does not answer for a foreign instance", "AGENT_CONTENT", nil},
		{"an unnamed instance matches nothing", "", nil},
	}

	w := &Worker{contentStore: owned, storeResolver: reg}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := w.resolveStore(tc.instance)
			if tc.want == nil {
				if got != nil {
					t.Fatalf("resolveStore(%q) = %v, want nil", tc.instance, got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("resolveStore(%q) = %v, want %v", tc.instance, got, tc.want)
			}
		})
	}
}
