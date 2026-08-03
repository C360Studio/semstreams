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
	// instance is the StorageInstance this store serves. Only the OWNED-fallback
	// tests set it: the registry arm resolves by its own map key, but the fallback
	// arm asks the store which instance it serves (gh#875), exactly as the
	// production *objectstore.Store answers via InstanceName().
	instance string
}

func (s readerStore) InstanceName() string { return s.instance }

// namelessStore is a StreamableStore that does NOT implement InstanceName — a
// custom backend wired through WithContentStore. The fallback cannot prove it serves
// any given instance, so it must not answer for one.
type namelessStore struct{ data string }

func (s namelessStore) Put(context.Context, string, []byte) error      { return nil }
func (s namelessStore) Get(context.Context, string) ([]byte, error)    { return []byte(s.data), nil }
func (s namelessStore) List(context.Context, string) ([]string, error) { return nil, nil }
func (s namelessStore) Delete(context.Context, string) error           { return nil }
func (s namelessStore) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(s.data)), nil
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

type countingMetrics struct {
	dedup, failed, resolveErr, resolved, truncated, dedupSkipped int
	identityIncluded, identityAbsent, excluded                   int
	failedReasons                                                []string
	excludedInstances                                            []string
}

func (m *countingMetrics) IncDedupHits()            { m.dedup++ }
func (m *countingMetrics) IncDedupSkipped(string)   { m.dedupSkipped++ }
func (m *countingMetrics) IncTruncated()            { m.truncated++ }
func (m *countingMetrics) IncFailed()               { m.failed++ }
func (m *countingMetrics) IncFailedReason(r string) { m.failedReasons = append(m.failedReasons, r) }
func (m *countingMetrics) SetPending(float64)       {}
func (m *countingMetrics) IncContentResolveError()  { m.resolveErr++ }
func (m *countingMetrics) IncContentResolved()      { m.resolved++ }

func (m *countingMetrics) IncOffloadedIdentityIncluded() { m.identityIncluded++ }
func (m *countingMetrics) IncOffloadedIdentityAbsent()   { m.identityAbsent++ }

func (m *countingMetrics) ReportContentExcluded(_, storageInstance string) {
	m.excluded++
	m.excludedInstances = append(m.excludedInstances, storageInstance)
}

// --- tests ---------------------------------------------------------------

func TestFetchText_RegistryResolvesByInstance(t *testing.T) {
	m := &countingMetrics{}
	w := &Worker{
		ctx:              context.Background(),
		maxSourceTextLen: 100,
		metrics:          m,
		// Fallback serves the SAME instance and points at DIFFERENT content, to prove
		// the registry path wins even when the owned store could also answer.
		contentStore:  readerStore{data: "FALLBACK", instance: "objectstore"},
		storeResolver: fakeResolver{stores: map[string]storage.StreamableStore{"objectstore": readerStore{data: "FROM-REGISTRY"}}},
	}

	got, _, err := w.fetchTextFromStorage(&StorageRef{StorageInstance: "objectstore", Key: "k"})
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

// TestFetchText_FallsBackToOwnedStoreForItsOwnInstance keeps the legacy single-bucket
// deploy shape working (ADR-063): when the registry cannot resolve the instance but the
// worker's OWNED store IS that instance, the body is served from it.
func TestFetchText_FallsBackToOwnedStoreForItsOwnInstance(t *testing.T) {
	w := &Worker{
		ctx:              context.Background(),
		maxSourceTextLen: 100,
		contentStore:     readerStore{data: "FALLBACK", instance: "objectstore"},
		// Registry has some OTHER instance, not the one the ref names.
		storeResolver: fakeResolver{stores: map[string]storage.StreamableStore{"other": readerStore{data: "X"}}},
	}

	got, _, err := w.fetchTextFromStorage(&StorageRef{StorageInstance: "objectstore", Key: "k"})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got != "FALLBACK" {
		t.Fatalf("got %q, want fallback content", got)
	}
}

// TestFetchText_OwnedStoreDoesNotAnswerForAForeignInstance is the gh#875 hop-2 half:
// the owned fallback used to answer for ANY instance, so a reference this process
// cannot serve was opened against an unrelated bucket and the resulting read failure
// was recorded as a durable failed embedding. The fetch must instead report the
// distinguishable no-store-for-this-instance CLASS, matchable with errors.Is.
func TestFetchText_OwnedStoreDoesNotAnswerForAForeignInstance(t *testing.T) {
	owned := readerStore{data: "FALLBACK", instance: "MESSAGES"}
	w := &Worker{
		ctx:              context.Background(),
		maxSourceTextLen: 100,
		contentStore:     owned,
		storeResolver:    fakeResolver{stores: map[string]storage.StreamableStore{"other": readerStore{data: "X"}}},
	}

	got, _, err := w.fetchTextFromStorage(&StorageRef{StorageInstance: "AGENT_CONTENT", Key: "k"})
	if !errors.Is(err, errNoStoreForInstance) {
		t.Fatalf("err = %v, want errNoStoreForInstance for an instance no wired store serves", err)
	}
	if got != "" {
		t.Fatalf("got %q, want no content: the owned store never held this reference", got)
	}
	if !strings.Contains(err.Error(), "AGENT_CONTENT") {
		t.Errorf("error must name the unresolvable instance for the operator; got %v", err)
	}
}

// TestFetchText_OwnedStoreThatCannotNameItsInstanceDoesNotAnswer pins the fail-closed
// branch: a fallback store that cannot state which instance it serves cannot be SHOWN
// to serve this reference, so it does not answer for it. Assuming it does is the
// gh#875 defect itself.
func TestFetchText_OwnedStoreThatCannotNameItsInstanceDoesNotAnswer(t *testing.T) {
	w := &Worker{
		ctx:              context.Background(),
		maxSourceTextLen: 100,
		contentStore:     namelessStore{data: "FALLBACK"},
	}

	_, _, err := w.fetchTextFromStorage(&StorageRef{StorageInstance: "objectstore", Key: "k"})
	if !errors.Is(err, errNoStoreForInstance) {
		t.Fatalf("err = %v, want errNoStoreForInstance for a store that cannot name its instance", err)
	}
}

func TestFetchText_NoStoreConfigured(t *testing.T) {
	w := &Worker{ctx: context.Background(), maxSourceTextLen: 100}
	_, _, err := w.fetchTextFromStorage(&StorageRef{StorageInstance: "objectstore", Key: "k"})
	if !errors.Is(err, errNoStoreForInstance) {
		t.Fatalf("err = %v, want errNoStoreForInstance when neither registry nor fallback can serve the ref", err)
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
