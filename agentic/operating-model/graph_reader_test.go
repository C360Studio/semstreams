package operatingmodel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// discardLogger returns a logger that discards all output, for tests.
// Lives in the test file so production binaries don't carry test-only scaffolding.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeKV is an in-memory kvGetter for unit tests. It stores raw bytes per
// key and returns ErrKVKeyNotFound for unknown keys, matching the contract
// of natsclient.KVStore.Get.
type fakeKV struct {
	store map[string][]byte
}

func newFakeKV() *fakeKV {
	return &fakeKV{store: make(map[string][]byte)}
}

func (f *fakeKV) Get(_ context.Context, key string) (*natsclient.KVEntry, error) {
	v, ok := f.store[key]
	if !ok {
		return nil, natsclient.ErrKVKeyNotFound
	}
	return &natsclient.KVEntry{Key: key, Value: v, Revision: 1}, nil
}

func (f *fakeKV) putState(t *testing.T, state graph.EntityState) {
	t.Helper()
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state %s: %v", state.ID, err)
	}
	f.store[state.ID] = b
}

// writeProfile writes the full chain (profile → layer → entry) for one user
// using the production triple writer. New triples are appended to existing
// entity states — matching graph-ingest's AddTriple semantics, which preserve
// multi-valued relationship predicates like has_layer and has_entry.
func (f *fakeKV) writeProfile(t *testing.T, ref ProfileRef, layer string, entries []Entry) {
	t.Helper()
	now := time.Now().UTC()
	triples := LayerTriples(ref, layer, "checkpoint", entries, now)

	bySubject := make(map[string][]message.Triple)
	for _, tr := range triples {
		bySubject[tr.Subject] = append(bySubject[tr.Subject], tr)
	}
	for id, ts := range bySubject {
		existing, _ := f.Get(context.Background(), id)
		state := graph.EntityState{ID: id, UpdatedAt: now}
		if existing != nil {
			if err := json.Unmarshal(existing.Value, &state); err != nil {
				t.Fatalf("unmarshal existing state %s: %v", id, err)
			}
		}
		state.Triples = append(state.Triples, ts...)
		f.putState(t, state)
	}
}

func mkEntry(layer, suffix, title, summary string) Entry {
	return Entry{
		EntryID:          "om-" + layer + "-" + suffix,
		Title:            title,
		Summary:          summary,
		SourceConfidence: ConfidenceConfirmed,
		Status:           StatusActive,
	}
}

func entryIDs(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.EntryID
	}
	return out
}

func newTestReader(kv kvGetter) *GraphProfileReader {
	return &GraphProfileReader{kv: kv, logger: discardLogger()}
}

func TestReadOperatingModel_NoProfile(t *testing.T) {
	r := newTestReader(newFakeKV())

	got, err := r.ReadOperatingModel(context.Background(), "acme", "ops", "alice")
	if err != nil {
		t.Fatalf("ReadOperatingModel = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("ReadOperatingModel = nil, want empty result")
	}
	if len(got.Entries) != 0 {
		t.Errorf("Entries = %d, want 0", len(got.Entries))
	}
	if got.Version != 0 {
		t.Errorf("Version = %d, want 0", got.Version)
	}
}

func TestReadOperatingModel_EmptyArgs(t *testing.T) {
	r := newTestReader(newFakeKV())

	for _, tc := range []struct {
		name            string
		org, plat, user string
	}{
		{"empty org", "", "ops", "alice"},
		{"empty platform", "acme", "", "alice"},
		{"empty userID", "acme", "ops", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.ReadOperatingModel(context.Background(), tc.org, tc.plat, tc.user)
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if got != nil {
				t.Errorf("result = %+v, want nil", got)
			}
		})
	}
}

func TestReadOperatingModel_SingleUserHappyPath(t *testing.T) {
	kv := newFakeKV()
	ref := ProfileRef{Org: "acme", Platform: "ops", UserID: "alice", Version: 2}
	want := []Entry{
		mkEntry(LayerOperatingRhythms, "1", "weekly planning", "Mondays 9-10am"),
		mkEntry(LayerOperatingRhythms, "2", "1:1 with manager", "every other Tuesday"),
	}
	kv.writeProfile(t, ref, LayerOperatingRhythms, want)

	r := newTestReader(kv)
	got, err := r.ReadOperatingModel(context.Background(), ref.Org, ref.Platform, ref.UserID)
	if err != nil {
		t.Fatalf("ReadOperatingModel = %v", err)
	}
	if got == nil {
		t.Fatal("got = nil, want result")
	}
	if got.Version != ref.Version {
		t.Errorf("Version = %d, want %d", got.Version, ref.Version)
	}
	if len(got.Entries) != len(want) {
		t.Fatalf("Entries = %d, want %d", len(got.Entries), len(want))
	}
	gotIDs := entryIDs(got.Entries)
	wantIDs := entryIDs(want)
	for _, w := range wantIDs {
		if !contains(gotIDs, w) {
			t.Errorf("missing entry %q in result %v", w, gotIDs)
		}
	}
}

// TestReadOperatingModel_MultiUserIsolation is the regression test for
// issue #14. Before the fix, ReadOperatingModel scanned all entries by
// flat KV prefix and returned every user's entries indiscriminately.
// After the fix, traversing profile → has_layer → has_entry must scope
// the result to the requested user.
func TestReadOperatingModel_MultiUserIsolation(t *testing.T) {
	kv := newFakeKV()

	aliceRef := ProfileRef{Org: "acme", Platform: "ops", UserID: "alice", Version: 1}
	aliceEntries := []Entry{
		mkEntry(LayerOperatingRhythms, "a1", "alice planning", "alice Mondays"),
		mkEntry(LayerFriction, "a2", "alice friction", "alice context-switch tax"),
	}
	kv.writeProfile(t, aliceRef, LayerOperatingRhythms, aliceEntries[:1])
	kv.writeProfile(t, aliceRef, LayerFriction, aliceEntries[1:])

	bobRef := ProfileRef{Org: "acme", Platform: "ops", UserID: "bob", Version: 3}
	bobEntries := []Entry{
		mkEntry(LayerDependencies, "b1", "bob dependencies", "bob upstream team"),
	}
	kv.writeProfile(t, bobRef, LayerDependencies, bobEntries)

	r := newTestReader(kv)

	t.Run("alice gets only alice", func(t *testing.T) {
		got, err := r.ReadOperatingModel(context.Background(), aliceRef.Org, aliceRef.Platform, aliceRef.UserID)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		gotIDs := entryIDs(got.Entries)
		if len(gotIDs) != len(aliceEntries) {
			t.Fatalf("alice entries = %d (%v), want %d", len(gotIDs), gotIDs, len(aliceEntries))
		}
		for _, b := range bobEntries {
			if contains(gotIDs, b.EntryID) {
				t.Errorf("LEAK: alice's result contains bob's entry %q", b.EntryID)
			}
		}
		if got.Version != aliceRef.Version {
			t.Errorf("Version = %d, want %d", got.Version, aliceRef.Version)
		}
	})

	t.Run("bob gets only bob", func(t *testing.T) {
		got, err := r.ReadOperatingModel(context.Background(), bobRef.Org, bobRef.Platform, bobRef.UserID)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		gotIDs := entryIDs(got.Entries)
		if len(gotIDs) != len(bobEntries) {
			t.Fatalf("bob entries = %d (%v), want %d", len(gotIDs), gotIDs, len(bobEntries))
		}
		for _, a := range aliceEntries {
			if contains(gotIDs, a.EntryID) {
				t.Errorf("LEAK: bob's result contains alice's entry %q", a.EntryID)
			}
		}
		if got.Version != bobRef.Version {
			t.Errorf("Version = %d, want %d", got.Version, bobRef.Version)
		}
	})
}

func TestReadOperatingModel_ProfileExistsButNoLayers(t *testing.T) {
	kv := newFakeKV()
	now := time.Now().UTC()
	profileID := ProfileEntityID("acme", "ops", "alice")
	kv.putState(t, graph.EntityState{
		ID: profileID,
		Triples: []message.Triple{{
			Subject:    profileID,
			Predicate:  PredicateProfileVersion,
			Object:     int64(7),
			Source:     TripleSource,
			Timestamp:  now,
			Confidence: 1.0,
		}},
		UpdatedAt: now,
	})

	r := newTestReader(kv)
	got, err := r.ReadOperatingModel(context.Background(), "acme", "ops", "alice")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got.Entries) != 0 {
		t.Errorf("Entries = %d, want 0", len(got.Entries))
	}
	if got.Version != 7 {
		t.Errorf("Version = %d, want 7", got.Version)
	}
}

func TestReadOperatingModel_LayerEntityMissing(t *testing.T) {
	kv := newFakeKV()
	ref := ProfileRef{Org: "acme", Platform: "ops", UserID: "alice", Version: 1}
	kv.writeProfile(t, ref, LayerOperatingRhythms, []Entry{
		mkEntry(LayerOperatingRhythms, "a1", "alice planning", "alice Mondays"),
	})

	// Delete the layer state (simulate partial-write or KV eviction).
	layerID := LayerEntityID(ref.Org, ref.Platform, ref.UserID, LayerOperatingRhythms)
	delete(kv.store, layerID)

	r := newTestReader(kv)
	got, err := r.ReadOperatingModel(context.Background(), ref.Org, ref.Platform, ref.UserID)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got.Entries) != 0 {
		t.Errorf("Entries = %d, want 0 (layer state missing)", len(got.Entries))
	}
	if got.Version != ref.Version {
		t.Errorf("Version = %d, want %d", got.Version, ref.Version)
	}
}

func TestReadOperatingModel_EntryEntityMissing(t *testing.T) {
	kv := newFakeKV()
	ref := ProfileRef{Org: "acme", Platform: "ops", UserID: "alice", Version: 1}
	entries := []Entry{
		mkEntry(LayerOperatingRhythms, "a1", "kept", "still here"),
		mkEntry(LayerOperatingRhythms, "a2", "vanished", "deleted below"),
	}
	kv.writeProfile(t, ref, LayerOperatingRhythms, entries)

	// Delete one entry's state but leave the has_entry edge in place.
	missingID := EntryEntityID(ref.Org, ref.Platform, entries[1].EntryID)
	delete(kv.store, missingID)

	r := newTestReader(kv)
	got, err := r.ReadOperatingModel(context.Background(), ref.Org, ref.Platform, ref.UserID)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	gotIDs := entryIDs(got.Entries)
	if len(gotIDs) != 1 || gotIDs[0] != entries[0].EntryID {
		t.Errorf("Entries = %v, want only %q", gotIDs, entries[0].EntryID)
	}
}

func TestReadOperatingModel_DeduplicatesEntries(t *testing.T) {
	// Defensive: if the same entry is referenced from two layers (shouldn't
	// happen by design but might during a buggy migration), the reader must
	// return it once, not twice.
	kv := newFakeKV()
	ref := ProfileRef{Org: "acme", Platform: "ops", UserID: "alice", Version: 1}
	shared := mkEntry(LayerOperatingRhythms, "shared", "shared title", "shared summary")
	kv.writeProfile(t, ref, LayerOperatingRhythms, []Entry{shared})

	// Inject a second has_entry edge from a different layer pointing at the
	// same entry entity.
	otherLayerID := LayerEntityID(ref.Org, ref.Platform, ref.UserID, LayerFriction)
	now := time.Now().UTC()
	profileID := ProfileEntityID(ref.Org, ref.Platform, ref.UserID)
	sharedEntryID := EntryEntityID(ref.Org, ref.Platform, shared.EntryID)
	kv.putState(t, graph.EntityState{
		ID: otherLayerID,
		Triples: []message.Triple{
			{Subject: otherLayerID, Predicate: PredicateLayerName, Object: LayerFriction, Source: TripleSource, Timestamp: now, Confidence: 1.0},
			{Subject: otherLayerID, Predicate: PredicateLayerHasEntry, Object: sharedEntryID, Source: TripleSource, Timestamp: now, Confidence: 1.0},
		},
		UpdatedAt: now,
	})
	// Add a second has_layer edge from the profile pointing at the new layer.
	prof, _ := kv.Get(context.Background(), profileID)
	var profState graph.EntityState
	_ = json.Unmarshal(prof.Value, &profState)
	profState.Triples = append(profState.Triples, message.Triple{
		Subject: profileID, Predicate: PredicateProfileHasLayer, Object: otherLayerID,
		Source: TripleSource, Timestamp: now, Confidence: 1.0,
	})
	kv.putState(t, profState)

	r := newTestReader(kv)
	got, err := r.ReadOperatingModel(context.Background(), ref.Org, ref.Platform, ref.UserID)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("Entries = %d, want 1 (dedup), got %v", len(got.Entries), entryIDs(got.Entries))
	}
}

func TestReadOperatingModel_EntryMissingRequiredFieldsSkipped(t *testing.T) {
	kv := newFakeKV()
	ref := ProfileRef{Org: "acme", Platform: "ops", UserID: "alice", Version: 1}
	entries := []Entry{
		mkEntry(LayerOperatingRhythms, "ok", "ok title", "ok summary"),
	}
	kv.writeProfile(t, ref, LayerOperatingRhythms, entries)

	// Inject a malformed entry (missing Title and Summary) referenced from
	// the layer's has_entry list. The reader must skip it without error.
	malformedID := EntryEntityID(ref.Org, ref.Platform, "om-bad")
	now := time.Now().UTC()
	kv.putState(t, graph.EntityState{
		ID: malformedID,
		Triples: []message.Triple{
			{Subject: malformedID, Predicate: PredicateEntrySourceConfidence, Object: ConfidenceConfirmed, Source: TripleSource, Timestamp: now, Confidence: 1.0},
		},
		UpdatedAt: now,
	})
	layerID := LayerEntityID(ref.Org, ref.Platform, ref.UserID, LayerOperatingRhythms)
	layerEntry, _ := kv.Get(context.Background(), layerID)
	var layerState graph.EntityState
	_ = json.Unmarshal(layerEntry.Value, &layerState)
	layerState.Triples = append(layerState.Triples, message.Triple{
		Subject: layerID, Predicate: PredicateLayerHasEntry, Object: malformedID,
		Source: TripleSource, Timestamp: now, Confidence: 1.0,
	})
	kv.putState(t, layerState)

	r := newTestReader(kv)
	got, err := r.ReadOperatingModel(context.Background(), ref.Org, ref.Platform, ref.UserID)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got.Entries) != 1 {
		t.Errorf("Entries = %d, want 1 (malformed skipped)", len(got.Entries))
	}
}

func TestReadProfileVersion(t *testing.T) {
	t.Run("no profile", func(t *testing.T) {
		r := newTestReader(newFakeKV())
		v, err := r.ReadProfileVersion(context.Background(), "acme", "ops", "alice")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if v != 0 {
			t.Errorf("version = %d, want 0", v)
		}
	})

	t.Run("returns persisted version", func(t *testing.T) {
		kv := newFakeKV()
		ref := ProfileRef{Org: "acme", Platform: "ops", UserID: "alice", Version: 5}
		kv.writeProfile(t, ref, LayerOperatingRhythms,
			[]Entry{mkEntry(LayerOperatingRhythms, "1", "t", "s")})

		r := newTestReader(kv)
		v, err := r.ReadProfileVersion(context.Background(), ref.Org, ref.Platform, ref.UserID)
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if v != ref.Version {
			t.Errorf("version = %d, want %d", v, ref.Version)
		}
	})

	t.Run("empty args return zero without error", func(t *testing.T) {
		r := newTestReader(newFakeKV())
		v, err := r.ReadProfileVersion(context.Background(), "", "ops", "alice")
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		if v != 0 {
			t.Errorf("version = %d, want 0", v)
		}
	})
}

func TestReadOperatingModel_KVError(t *testing.T) {
	t.Run("profile read fails", func(t *testing.T) {
		// Every key fails: covers the "profile fetch hits transient KV
		// error" path.
		kv := &errKV{err: errors.New("nats down")}
		r := newTestReader(kv)
		got, err := r.ReadOperatingModel(context.Background(), "acme", "ops", "alice")
		// getState swallows non-NotFound errors as "no profile" (matches
		// pre-fix behaviour). The reader must not surface the underlying
		// error for missing-profile scenarios.
		if err != nil {
			t.Fatalf("err = %v, want nil (KV transient errors are logged, not surfaced)", err)
		}
		if got == nil {
			t.Fatal("result = nil, want empty ProfileResult")
		}
		if len(got.Entries) != 0 {
			t.Errorf("Entries = %d, want 0", len(got.Entries))
		}
	})

	t.Run("partial failure: profile loads, layer fetch errors", func(t *testing.T) {
		// Profile loads cleanly, but the layer entity get returns a
		// transport error. Reader must skip the broken layer (logged at
		// Debug) and return a result with the version intact and zero
		// entries — no crash, no surfaced error.
		base := newFakeKV()
		ref := ProfileRef{Org: "acme", Platform: "ops", UserID: "alice", Version: 4}
		base.writeProfile(t, ref, LayerOperatingRhythms, []Entry{
			mkEntry(LayerOperatingRhythms, "1", "weekly", "blocked"),
		})

		layerID := LayerEntityID(ref.Org, ref.Platform, ref.UserID, LayerOperatingRhythms)
		kv := &selectiveErrKV{
			delegate: base,
			failKey:  layerID,
			err:      errors.New("nats down on layer"),
		}
		r := newTestReader(kv)
		got, err := r.ReadOperatingModel(context.Background(), ref.Org, ref.Platform, ref.UserID)
		if err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		if got == nil {
			t.Fatal("result = nil")
		}
		if got.Version != ref.Version {
			t.Errorf("Version = %d, want %d (profile loaded successfully)", got.Version, ref.Version)
		}
		if len(got.Entries) != 0 {
			t.Errorf("Entries = %d, want 0 (layer fetch failed)", len(got.Entries))
		}
	})
}

// errKV always returns the configured error from Get. Used to cover the
// "transient KV failure" path on the profile read.
type errKV struct{ err error }

func (e *errKV) Get(_ context.Context, _ string) (*natsclient.KVEntry, error) {
	return nil, e.err
}

// selectiveErrKV delegates Get to an underlying fakeKV but injects a
// configured error for one specific key. Used to exercise the partial-
// failure path: profile loads, but a layer or entry fetch errors.
type selectiveErrKV struct {
	delegate *fakeKV
	failKey  string
	err      error
}

func (s *selectiveErrKV) Get(ctx context.Context, key string) (*natsclient.KVEntry, error) {
	if key == s.failKey {
		return nil, s.err
	}
	return s.delegate.Get(ctx, key)
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
