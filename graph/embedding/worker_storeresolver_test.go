package embedding

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/storage"
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
	data    string
	openErr error
}

func (s readerStore) Put(context.Context, string, []byte) error      { return nil }
func (s readerStore) Get(context.Context, string) ([]byte, error)    { return []byte(s.data), nil }
func (s readerStore) List(context.Context, string) ([]string, error) { return nil, nil }
func (s readerStore) Delete(context.Context, string) error           { return nil }
func (s readerStore) Open(context.Context, string) (io.ReadCloser, error) {
	if s.openErr != nil {
		return nil, s.openErr
	}
	return io.NopCloser(strings.NewReader(s.data)), nil
}

type countingMetrics struct{ dedup, failed, resolveErr, resolved int }

func (m *countingMetrics) IncDedupHits()           { m.dedup++ }
func (m *countingMetrics) IncFailed()              { m.failed++ }
func (m *countingMetrics) SetPending(float64)      {}
func (m *countingMetrics) IncContentResolveError() { m.resolveErr++ }
func (m *countingMetrics) IncContentResolved()     { m.resolved++ }

// --- tests ---------------------------------------------------------------

func TestFetchText_RegistryResolvesByInstance(t *testing.T) {
	m := &countingMetrics{}
	w := &Worker{
		ctx:              context.Background(),
		maxSourceTextLen: 100,
		metrics:          m,
		// Fallback points at DIFFERENT content, to prove the registry path wins.
		contentStore:  readerStore{data: "FALLBACK"},
		storeResolver: fakeResolver{stores: map[string]storage.StreamableStore{"objectstore": readerStore{data: "FROM-REGISTRY"}}},
	}

	got, err := w.fetchTextFromStorage(&StorageRef{StorageInstance: "objectstore", Key: "k"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "FROM-REGISTRY" {
		t.Fatalf("got %q, want registry content (registry must win over fallback)", got)
	}
	if m.resolveErr != 0 {
		t.Fatalf("resolveErr = %d, want 0", m.resolveErr)
	}
	if m.resolved != 1 {
		t.Fatalf("resolved = %d, want 1 (successful fetch must count the positive observable)", m.resolved)
	}
}

func TestFetchText_FallsBackToOwnedStoreWhenInstanceAbsent(t *testing.T) {
	w := &Worker{
		ctx:              context.Background(),
		maxSourceTextLen: 100,
		contentStore:     readerStore{data: "FALLBACK"},
		// Registry has some OTHER instance, not the one the ref names.
		storeResolver: fakeResolver{stores: map[string]storage.StreamableStore{"other": readerStore{data: "X"}}},
	}

	got, err := w.fetchTextFromStorage(&StorageRef{StorageInstance: "objectstore", Key: "k"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "FALLBACK" {
		t.Fatalf("got %q, want fallback content", got)
	}
}

func TestFetchText_NoStoreConfigured(t *testing.T) {
	w := &Worker{ctx: context.Background(), maxSourceTextLen: 100}
	_, err := w.fetchTextFromStorage(&StorageRef{StorageInstance: "objectstore", Key: "k"})
	if err == nil {
		t.Fatal("expected error when neither registry nor fallback can serve the ref")
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

	_, err := w.fetchTextFromStorage(&StorageRef{StorageInstance: "objectstore", Key: "k"})
	if err == nil {
		t.Fatal("expected error when a resolved store fails to open")
	}
	// A resolved-but-failed fetch is the distinct M1 class, NOT a plain failure.
	if m.resolveErr != 1 {
		t.Fatalf("resolveErr = %d, want 1 (must count resolve errors distinctly)", m.resolveErr)
	}
}
