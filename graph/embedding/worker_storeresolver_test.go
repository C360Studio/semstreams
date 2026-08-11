package embedding

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/storage"
	"github.com/c360studio/semstreams/storage/storeregistry"
)

// --- fakes ---------------------------------------------------------------

type fakeResolver struct {
	stores map[string]storage.StreamableStore
}

func (f fakeResolver) Streamable(instance string) (storage.StreamableStore, bool) {
	s, ok := f.stores[instance]
	return s, ok
}

type readerStore struct {
	data      string
	openErr   error
	readErr   error
	openCalls *int
}

func (s readerStore) Put(context.Context, string, []byte) error      { return nil }
func (s readerStore) Get(context.Context, string) ([]byte, error)    { return []byte(s.data), nil }
func (s readerStore) List(context.Context, string) ([]string, error) { return nil, nil }
func (s readerStore) Delete(context.Context, string) error           { return nil }
func (s readerStore) Open(context.Context, string) (io.ReadCloser, error) {
	if s.openCalls != nil {
		*s.openCalls++
	}
	if s.openErr != nil {
		return nil, s.openErr
	}
	if s.readErr != nil {
		return failingReadCloser{err: s.readErr}, nil
	}
	return io.NopCloser(strings.NewReader(s.data)), nil
}

type failingReadCloser struct{ err error }

func (r failingReadCloser) Read([]byte) (int, error) { return 0, r.err }
func (failingReadCloser) Close() error               { return nil }

type countingMetrics struct {
	dedup, failed, unresolved, resolveErr, resolved, truncated, dedupSkipped int
	identityIncluded, identityAbsent                                         int
	failedReasons                                                            []string
}

func (m *countingMetrics) IncDedupHits()            { m.dedup++ }
func (m *countingMetrics) IncDedupSkipped(string)   { m.dedupSkipped++ }
func (m *countingMetrics) IncTruncated()            { m.truncated++ }
func (m *countingMetrics) IncFailed()               { m.failed++ }
func (m *countingMetrics) IncFailedReason(r string) { m.failedReasons = append(m.failedReasons, r) }
func (m *countingMetrics) SetPending(float64)       {}
func (m *countingMetrics) IncContentResolveError()  { m.resolveErr++ }
func (m *countingMetrics) IncContentResolved()      { m.resolved++ }
func (m *countingMetrics) IncContentUnresolved()    { m.unresolved++ }

func (m *countingMetrics) IncOffloadedIdentityIncluded() { m.identityIncluded++ }
func (m *countingMetrics) IncOffloadedIdentityAbsent()   { m.identityAbsent++ }

// --- tests ---------------------------------------------------------------

func TestFetchText_RegistryResolvesExactInstance(t *testing.T) {
	m := &countingMetrics{}
	var aOpens, bOpens int
	w := &Worker{
		ctx:              context.Background(),
		maxSourceTextLen: 100,
		metrics:          m,
		storeResolver: fakeResolver{stores: map[string]storage.StreamableStore{
			"store-a": readerStore{data: "FROM-A", openCalls: &aOpens},
			"store-b": readerStore{data: "FROM-B", openCalls: &bOpens},
		}},
	}

	got, _, err := w.fetchTextFromStorage(&StorageRef{StorageInstance: "store-b", Key: "k"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "FROM-B" {
		t.Fatalf("got %q, want exact store-b content", got)
	}
	if aOpens != 0 || bOpens != 1 {
		t.Fatalf("open calls: store-a=%d store-b=%d, want 0/1", aOpens, bOpens)
	}
	if m.resolveErr != 0 {
		t.Fatalf("resolveErr = %d, want 0", m.resolveErr)
	}
	if m.resolved != 1 {
		t.Fatalf("resolved = %d, want 1 (successful fetch must count the positive observable)", m.resolved)
	}
}

func TestGetSourceText_UnregisteredInstanceExcludesBodyAndContinuesIdentity(t *testing.T) {
	m := &countingMetrics{}
	var foreignOpens int
	w := &Worker{
		ctx:              context.Background(),
		maxSourceTextLen: 100,
		metrics:          m,
		storeResolver: fakeResolver{stores: map[string]storage.StreamableStore{
			"other": readerStore{data: "FOREIGN", openCalls: &foreignOpens},
		}},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	got, err := w.getSourceText(&Record{
		IdentityText: "inline identity",
		StorageRef:   &StorageRef{StorageInstance: "missing", Key: "k"},
	})
	if err != nil {
		t.Fatalf("getSourceText: %v", err)
	}
	if got != "inline identity" {
		t.Fatalf("got %q, want inline identity after body exclusion", got)
	}
	if foreignOpens != 0 {
		t.Fatalf("foreign store opened %d times, want 0", foreignOpens)
	}
	if m.unresolved != 1 || m.resolveErr != 0 || m.failed != 0 {
		t.Fatalf("metrics unresolved=%d resolveErr=%d failed=%d, want 1/0/0", m.unresolved, m.resolveErr, m.failed)
	}
}

func TestHandleKVEntry_DeregisteredInstanceSkipsAndDeletesStaleRecord(t *testing.T) {
	ctx := context.Background()
	index := newMemKV()
	s := NewStorage(index, newMemKV())
	const entityID = "acme.ops.robotics.gcs.document.001"
	ref := &StorageRef{StorageInstance: "content", Key: "doc/1"}
	if err := s.SavePendingWithStorageRef(ctx, entityID, "", "", ref, nil, 7); err != nil {
		t.Fatalf("SavePendingWithStorageRef: %v", err)
	}

	registry := storeregistry.New()
	if err := registry.Register("content", readerStore{data: "body"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Admission observed the store, but it stopped before hop 2 fetched the body.
	registry.Deregister("content")

	m := &countingMetrics{}
	w := &Worker{storage: s, ctx: ctx, maxSourceTextLen: 100, metrics: m,
		storeResolver: registry, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	entry, err := index.Get(ctx, entityID)
	if err != nil {
		t.Fatalf("Get pending: %v", err)
	}
	_, _, terminal, outcome, reason := w.handleKVEntry(entry, false, 0)
	if !terminal || outcome != OutcomeSkipped || reason != "" {
		t.Fatalf("terminal outcome = (%v, %v, %q), want (true, skipped, empty reason)", terminal, outcome, reason)
	}
	if index.has(entityID) {
		t.Fatal("stale pending/generated record remains after unresolved no-text skip")
	}
	if m.unresolved != 1 || m.failed != 0 || len(m.failedReasons) != 0 {
		t.Fatalf("metrics unresolved=%d failed=%d reasons=%v, want 1/0/[]", m.unresolved, m.failed, m.failedReasons)
	}
}

func TestFetchText_ResolveErrorMetricOnReadFailure(t *testing.T) {
	m := &countingMetrics{}
	w := &Worker{
		ctx:              context.Background(),
		maxSourceTextLen: 100,
		metrics:          m,
		storeResolver: fakeResolver{stores: map[string]storage.StreamableStore{
			"objectstore": readerStore{openErr: errors.New("bucket deleted")},
		}},
	}

	_, _, err := w.fetchTextFromStorage(&StorageRef{StorageInstance: "objectstore", Key: "k"})
	if err == nil {
		t.Fatal("expected error when a resolved store fails to open")
	}
	// A resolved-but-failed fetch is the distinct M1 class, NOT a plain failure.
	if m.resolveErr != 1 {
		t.Fatalf("resolveErr = %d, want 1 (must count resolve errors distinctly)", m.resolveErr)
	}
}

func TestFetchText_ResolveErrorMetricOnStreamReadFailure(t *testing.T) {
	m := &countingMetrics{}
	w := &Worker{
		ctx:              context.Background(),
		maxSourceTextLen: 100,
		metrics:          m,
		storeResolver: fakeResolver{stores: map[string]storage.StreamableStore{
			"objectstore": readerStore{readErr: errors.New("stream interrupted")},
		}},
	}

	_, _, err := w.fetchTextFromStorage(&StorageRef{StorageInstance: "objectstore", Key: "k"})
	if err == nil {
		t.Fatal("expected error when a resolved store fails during Read")
	}
	if m.resolveErr != 1 || m.unresolved != 0 {
		t.Fatalf("resolveErr=%d unresolved=%d, want 1/0", m.resolveErr, m.unresolved)
	}
}

func TestHandleKVEntry_ResolvedStoreFailureRemainsContentFailure(t *testing.T) {
	ctx := context.Background()
	index := newMemKV()
	s := NewStorage(index, newMemKV())
	const entityID = "acme.ops.robotics.gcs.document.failed"
	ref := &StorageRef{StorageInstance: "content", Key: "doc/missing"}
	if err := s.SavePendingWithStorageRef(ctx, entityID, "", "identity", ref, nil, 9); err != nil {
		t.Fatalf("SavePendingWithStorageRef: %v", err)
	}

	m := &countingMetrics{}
	w := &Worker{
		storage: s, ctx: ctx, maxSourceTextLen: 100, metrics: m,
		storeResolver: fakeResolver{stores: map[string]storage.StreamableStore{
			"content": readerStore{openErr: errors.New("bucket unavailable")},
		}},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	entry, err := index.Get(ctx, entityID)
	if err != nil {
		t.Fatalf("Get pending: %v", err)
	}
	_, _, terminal, outcome, reason := w.handleKVEntry(entry, false, 0)
	if !terminal || outcome != OutcomeFailed || reason != failReasonContentError {
		t.Fatalf("terminal outcome = (%v, %v, %q), want (true, failed, content_error)", terminal, outcome, reason)
	}
	record, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if record.Status != StatusFailed || record.Reason != failReasonContentError {
		t.Fatalf("durable record status=%q reason=%q, want failed/content_error", record.Status, record.Reason)
	}
	if m.resolveErr != 1 || m.unresolved != 0 || m.failed != 1 || len(m.failedReasons) != 1 || m.failedReasons[0] != failReasonContentError {
		t.Fatalf("metrics resolveErr=%d unresolved=%d failed=%d reasons=%v", m.resolveErr, m.unresolved, m.failed, m.failedReasons)
	}
}
