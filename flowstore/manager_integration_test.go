//go:build integration

package flowstore

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
)

func TestManagerDiagramCRUDAndVersioning(t *testing.T) {
	client := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV()).Client
	store, err := NewManager(client)
	if err != nil {
		t.Fatal(err)
	}
	flow := validTestFlow()
	if err := store.Create(context.Background(), &flow); err != nil {
		t.Fatal(err)
	}
	if flow.Version != 1 || flow.CreatedAt.IsZero() || flow.UpdatedAt.IsZero() {
		t.Fatalf("create did not set audit/version: %#v", flow)
	}

	got, err := store.Get(context.Background(), flow.ID)
	if err != nil {
		t.Fatal(err)
	}
	stale := *got
	got.Name = "Updated"
	if err := store.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 {
		t.Fatalf("update version = %d, want 2", got.Version)
	}
	staleErr := store.Update(context.Background(), &stale)
	if staleErr == nil {
		t.Fatal("stale update succeeded")
	}
	if !errors.Is(staleErr, errs.ErrRevisionMismatch) {
		t.Errorf("stale update error is not the typed conflict: %v", staleErr)
	}
	if !errs.IsInvalid(staleErr) || errs.IsTransient(staleErr) {
		t.Errorf("stale update conflict must be invalid and not transient: %v", staleErr)
	}
	if stored := storedFlow(t, store, flow.ID); stored.Version != 2 || stored.Name != "Updated" {
		t.Errorf("stale update wrote: stored version=%d name=%q, want 2/%q", stored.Version, stored.Name, "Updated")
	}

	listed, err := store.List(context.Background())
	if err != nil || len(listed) == 0 {
		t.Fatalf("list: len=%d err=%v", len(listed), err)
	}
	if err := store.Delete(context.Background(), flow.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(context.Background(), flow.ID); err == nil {
		t.Fatal("deleted flow still readable")
	}
}

// copyFlow returns a deep copy of f: Nodes, Connections and each node Config are
// reference types, so a shallow struct copy would alias them and reflect.DeepEqual
// against it could not detect in-place mutation of the caller's value.
func copyFlow(f Flow) Flow {
	out := f
	if f.Nodes != nil {
		out.Nodes = make([]FlowNode, len(f.Nodes))
		copy(out.Nodes, f.Nodes)
		for i := range out.Nodes {
			if f.Nodes[i].Config == nil {
				continue
			}
			config := make(map[string]any, len(f.Nodes[i].Config))
			for k, v := range f.Nodes[i].Config {
				config[k] = v
			}
			out.Nodes[i].Config = config
		}
	}
	if f.Connections != nil {
		out.Connections = make([]FlowConnection, len(f.Connections))
		copy(out.Connections, f.Connections)
	}
	return out
}

// newTestManager returns a Manager over a fresh NATS server's semstreams_flows
// bucket together with the client, so a second Manager can be built over the
// same bucket.
func newTestManager(t *testing.T) (*Manager, *natsclient.Client) {
	t.Helper()
	client := natsclient.NewTestClient(t, natsclient.WithJetStream(), natsclient.WithKV()).Client
	store, err := NewManager(client)
	if err != nil {
		t.Fatal(err)
	}
	return store, client
}

// storedFlow reads the record straight out of KV so assertions are about what was
// persisted, not about what Update left in the caller's value.
func storedFlow(t *testing.T, store *Manager, id string) Flow {
	t.Helper()
	entry, err := store.kvStore.Get(t.Context(), id)
	if err != nil {
		t.Fatalf("read stored flow %s: %v", id, err)
	}
	var flow Flow
	if err := json.Unmarshal(entry.Value, &flow); err != nil {
		t.Fatalf("decode stored flow %s: %v", id, err)
	}
	return flow
}

func TestManagerUpdatePreservesStoredCreatedAt(t *testing.T) {
	store, _ := newTestManager(t)
	created := validTestFlow()
	created.CreatedBy = "author-a"
	if err := store.Create(t.Context(), &created); err != nil {
		t.Fatal(err)
	}
	createdAt := storedFlow(t, store, created.ID).CreatedAt
	if createdAt.IsZero() {
		t.Fatal("create did not stamp created_at")
	}

	// The semstreams-ui editor save path sends no timestamps at all.
	request := validTestFlow()
	request.Name = "Renamed by the editor"
	request.Version = created.Version
	request.CreatedBy = "author-b"
	if !request.CreatedAt.IsZero() || !request.UpdatedAt.IsZero() || !request.LastModified.IsZero() {
		t.Fatal("request fixture must carry no timestamps")
	}
	if err := store.Update(t.Context(), &request); err != nil {
		t.Fatal(err)
	}

	stored := storedFlow(t, store, created.ID)
	if !stored.CreatedAt.Equal(createdAt) {
		t.Errorf("stored created_at = %v, want %v (restored from the stored record)", stored.CreatedAt, createdAt)
	}
	if !request.CreatedAt.Equal(createdAt) {
		t.Errorf("returned created_at = %v, want %v", request.CreatedAt, createdAt)
	}
	// created_by is caller-preserved: the framework does not restore it.
	if stored.CreatedBy != "author-b" {
		t.Errorf("stored created_by = %q, want %q (caller-preserved)", stored.CreatedBy, "author-b")
	}
}

func TestManagerUpdateIgnoresForgedCreatedAt(t *testing.T) {
	store, _ := newTestManager(t)
	created := validTestFlow()
	if err := store.Create(t.Context(), &created); err != nil {
		t.Fatal(err)
	}
	createdAt := storedFlow(t, store, created.ID).CreatedAt

	forged := time.Date(1999, time.January, 2, 3, 4, 5, 0, time.UTC)
	request := copyFlow(created)
	request.CreatedAt = forged
	request.UpdatedAt = forged
	request.LastModified = forged
	if err := store.Update(t.Context(), &request); err != nil {
		t.Fatal(err)
	}

	stored := storedFlow(t, store, created.ID)
	if !stored.CreatedAt.Equal(createdAt) {
		t.Errorf("stored created_at = %v, want %v (forged value must be ignored)", stored.CreatedAt, createdAt)
	}
	if stored.UpdatedAt.Equal(forged) || stored.LastModified.Equal(forged) {
		t.Errorf("stored update timestamps took the forged value: updated_at=%v last_modified=%v",
			stored.UpdatedAt, stored.LastModified)
	}
}

func TestManagerUpdateTwoManagersExactlyOneWins(t *testing.T) {
	storeA, client := newTestManager(t)
	storeB, err := NewManager(client)
	if err != nil {
		t.Fatal(err)
	}

	created := validTestFlow()
	if err := storeA.Create(t.Context(), &created); err != nil {
		t.Fatal(err)
	}
	baseVersion := created.Version
	baseCreatedAt := storedFlow(t, storeA, created.ID).CreatedAt

	// Both writers start from the same read, so both observe the same KV revision.
	inputA := copyFlow(created)
	inputA.Name = "written by A"
	inputB := copyFlow(created)
	inputB.Name = "written by B"
	preA := copyFlow(inputA)
	preB := copyFlow(inputB)

	readyA, readyB := make(chan struct{}), make(chan struct{})
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	storeA.beforeUpdateWrite = func(context.Context) { close(readyA); <-releaseA }
	storeB.beforeUpdateWrite = func(context.Context) { close(readyB); <-releaseB }

	errA, errB := make(chan error, 1), make(chan error, 1)
	go func() { errA <- storeA.Update(t.Context(), &inputA) }()
	go func() { errB <- storeB.Update(t.Context(), &inputB) }()

	// Explicit synchronization: neither writer can proceed past its read until
	// both have read. No sleep and no retry probability is involved.
	<-readyA
	<-readyB
	if !reflect.DeepEqual(inputA, preA) {
		t.Errorf("A mutated its input before commit:\n got %#v\nwant %#v", inputA, preA)
	}
	if !reflect.DeepEqual(inputB, preB) {
		t.Errorf("B mutated its input before commit:\n got %#v\nwant %#v", inputB, preB)
	}
	close(releaseA)
	close(releaseB)
	resultA, resultB := <-errA, <-errB

	winners := 0
	var winnerName string
	var loserInput, loserPre Flow
	for _, side := range []struct {
		name  string
		err   error
		input Flow
		pre   Flow
	}{
		{"A", resultA, inputA, preA},
		{"B", resultB, inputB, preB},
	} {
		if side.err == nil {
			winners++
			winnerName = "written by " + side.name
			continue
		}
		loserInput, loserPre = side.input, side.pre
		if !errors.Is(side.err, errs.ErrRevisionMismatch) {
			t.Errorf("loser %s error is not the typed conflict: %v", side.name, side.err)
		}
		if !errs.IsInvalid(side.err) {
			t.Errorf("loser %s error is not classified invalid: %v", side.name, side.err)
		}
		if errs.IsTransient(side.err) {
			t.Errorf("loser %s error is classified transient: %v", side.name, side.err)
		}
	}
	if winners != 1 {
		t.Fatalf("want exactly one winner, got %d (A=%v B=%v)", winners, resultA, resultB)
	}
	if !reflect.DeepEqual(loserInput, loserPre) {
		t.Errorf("loser input mutated:\n got %#v\nwant %#v", loserInput, loserPre)
	}

	stored := storedFlow(t, storeA, created.ID)
	if stored.Version != baseVersion+1 {
		t.Errorf("stored version = %d, want %d (advanced exactly once)", stored.Version, baseVersion+1)
	}
	if stored.Name != winnerName {
		t.Errorf("stored name = %q, want %q (the winner's content)", stored.Name, winnerName)
	}
	if !stored.CreatedAt.Equal(baseCreatedAt) {
		t.Errorf("stored created_at = %v, want %v", stored.CreatedAt, baseCreatedAt)
	}
}

func TestManagerUpdateFailedWriteDoesNotMutateInput(t *testing.T) {
	store, _ := newTestManager(t)
	created := validTestFlow()
	if err := store.Create(t.Context(), &created); err != nil {
		t.Fatal(err)
	}

	t.Run("logical version mismatch", func(t *testing.T) {
		stale := copyFlow(created)
		stale.Name = "stale writer"
		stale.Version = created.Version - 1
		pre := copyFlow(stale)

		err := store.Update(t.Context(), &stale)
		if err == nil {
			t.Fatal("stale logical version was accepted")
		}
		if !errors.Is(err, errs.ErrRevisionMismatch) {
			t.Errorf("error is not the typed conflict: %v", err)
		}
		if !errs.IsInvalid(err) || errs.IsTransient(err) {
			t.Errorf("conflict must be invalid and not transient: %v", err)
		}
		if !reflect.DeepEqual(stale, pre) {
			t.Errorf("input mutated by a rejected update:\n got %#v\nwant %#v", stale, pre)
		}
		if stored := storedFlow(t, store, created.ID); stored.Version != created.Version {
			t.Errorf("stored version = %d, want %d (no write on a logical mismatch)", stored.Version, created.Version)
		}
	})

	t.Run("lost revision fence", func(t *testing.T) {
		input := copyFlow(created)
		input.Name = "fence loser"
		pre := copyFlow(input)

		// Deterministic fence loss: a competing write commits while this Update is
		// held at the seam, after it has read the revision it is about to fence on.
		store.beforeUpdateWrite = func(ctx context.Context) {
			competitor := copyFlow(created)
			competitor.Name = "fence winner"
			data, err := json.Marshal(competitor)
			if err != nil {
				t.Error(err)
				return
			}
			if _, err := store.kvStore.Put(ctx, competitor.ID, data); err != nil {
				t.Error(err)
			}
		}
		err := store.Update(t.Context(), &input)
		store.beforeUpdateWrite = nil
		if err == nil {
			t.Fatal("update committed over a foreign write")
		}
		if !errors.Is(err, errs.ErrRevisionMismatch) {
			t.Errorf("error is not the typed conflict: %v", err)
		}
		if !reflect.DeepEqual(input, pre) {
			t.Errorf("input mutated by a failed write:\n got %#v\nwant %#v", input, pre)
		}
		if stored := storedFlow(t, store, created.ID); stored.Name != "fence winner" {
			t.Errorf("stored name = %q, want %q (the loser must not have written)", stored.Name, "fence winner")
		}
	})

	t.Run("read failure on a missing key", func(t *testing.T) {
		missing := validTestFlow()
		missing.ID = "no-such-flow"
		missing.Version = 1
		pre := copyFlow(missing)

		err := store.Update(t.Context(), &missing)
		if err == nil {
			t.Fatal("update of a missing key succeeded")
		}
		if !errs.IsTransient(err) {
			t.Errorf("missing key must stay transient: %v", err)
		}
		if !reflect.DeepEqual(missing, pre) {
			t.Errorf("input mutated by a read failure:\n got %#v\nwant %#v", missing, pre)
		}
	})

	t.Run("decode failure on a corrupt record", func(t *testing.T) {
		corrupt := validTestFlow()
		corrupt.ID = "corrupt-flow"
		corrupt.Version = 1
		if _, err := store.kvStore.Put(t.Context(), corrupt.ID, []byte("{not json")); err != nil {
			t.Fatal(err)
		}
		pre := copyFlow(corrupt)

		err := store.Update(t.Context(), &corrupt)
		if err == nil {
			t.Fatal("update over a corrupt record succeeded")
		}
		if !errs.IsFatal(err) {
			t.Errorf("corrupt stored JSON must be fatal: %v", err)
		}
		if !reflect.DeepEqual(corrupt, pre) {
			t.Errorf("input mutated by a decode failure:\n got %#v\nwant %#v", corrupt, pre)
		}
	})

	t.Run("marshal failure", func(t *testing.T) {
		unmarshalable := copyFlow(created)
		unmarshalable.Nodes[0].Config["unserializable"] = math.Inf(1)
		pre := copyFlow(unmarshalable)

		err := store.Update(t.Context(), &unmarshalable)
		if err == nil {
			t.Fatal("update with an unmarshalable config succeeded")
		}
		if !reflect.DeepEqual(unmarshalable, pre) {
			t.Errorf("input mutated by a marshal failure:\n got %#v\nwant %#v", unmarshalable, pre)
		}
	})

	t.Run("structural validation failure", func(t *testing.T) {
		invalid := copyFlow(created)
		invalid.Name = ""
		pre := copyFlow(invalid)

		if err := store.Update(t.Context(), &invalid); err == nil {
			t.Fatal("structurally invalid flow was accepted")
		}
		if !reflect.DeepEqual(invalid, pre) {
			t.Errorf("input mutated by a validation failure:\n got %#v\nwant %#v", invalid, pre)
		}
	})
}

func TestManagerUpdateSuccessMutatesInputAfterCommit(t *testing.T) {
	store, _ := newTestManager(t)
	created := validTestFlow()
	if err := store.Create(t.Context(), &created); err != nil {
		t.Fatal(err)
	}
	createdAt := storedFlow(t, store, created.ID).CreatedAt

	input := copyFlow(created)
	input.Name = "committed name"
	input.CreatedAt = time.Time{}
	input.UpdatedAt = time.Time{}
	input.LastModified = time.Time{}
	pre := copyFlow(input)

	ready := make(chan struct{})
	release := make(chan struct{})
	store.beforeUpdateWrite = func(context.Context) { close(ready); <-release }
	done := make(chan error, 1)
	go func() { done <- store.Update(t.Context(), &input) }()

	<-ready
	if !reflect.DeepEqual(input, pre) {
		t.Errorf("input mutated before commit:\n got %#v\nwant %#v", input, pre)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	store.beforeUpdateWrite = nil

	stored := storedFlow(t, store, created.ID)
	if input.Version != created.Version+1 || stored.Version != created.Version+1 {
		t.Errorf("version: input=%d stored=%d, want %d", input.Version, stored.Version, created.Version+1)
	}
	if !input.CreatedAt.Equal(createdAt) || !stored.CreatedAt.Equal(createdAt) {
		t.Errorf("created_at: input=%v stored=%v, want %v", input.CreatedAt, stored.CreatedAt, createdAt)
	}
	if !input.UpdatedAt.Equal(input.LastModified) {
		t.Errorf("input updated_at=%v last_modified=%v, want one server instant", input.UpdatedAt, input.LastModified)
	}
	if !stored.UpdatedAt.Equal(stored.LastModified) {
		t.Errorf("stored updated_at=%v last_modified=%v, want one server instant", stored.UpdatedAt, stored.LastModified)
	}
	if !stored.UpdatedAt.Equal(input.UpdatedAt) {
		t.Errorf("input updated_at=%v, stored=%v — the caller must observe the committed record",
			input.UpdatedAt, stored.UpdatedAt)
	}
	if !stored.UpdatedAt.After(createdAt) {
		t.Errorf("stored updated_at=%v is not after created_at=%v", stored.UpdatedAt, createdAt)
	}
	if stored.Name != "committed name" {
		t.Errorf("stored name = %q, want %q", stored.Name, "committed name")
	}
}

// listTestFlow returns a valid Flow with the supplied ID so a List test can
// hold more than one saved record over a single bucket.
func listTestFlow(id string) Flow {
	flow := validTestFlow()
	flow.ID = id
	flow.Name = "Flow " + id
	return flow
}

// listIDs returns the IDs of a List result in the order List produced them.
// List promises no ordering, so assertions compare sets or single elements.
func listIDs(flows []*Flow) []string {
	ids := make([]string, 0, len(flows))
	for _, f := range flows {
		ids = append(ids, f.ID)
	}
	return ids
}

func TestManagerListEmptyBucketReturnsNonNilEmpty(t *testing.T) {
	store, _ := newTestManager(t)

	flows, err := store.List(t.Context())
	if err != nil {
		t.Fatalf("List over an empty bucket returned an error: %v", err)
	}
	if flows == nil {
		t.Error("List over an empty bucket returned a nil slice, want a non-nil empty slice")
	}
	if len(flows) != 0 {
		t.Errorf("List over an empty bucket returned %d flows (%v), want 0", len(flows), listIDs(flows))
	}
}

func TestManagerListSkipsOnlyVanishedKey(t *testing.T) {
	store, _ := newTestManager(t)
	flowA := listTestFlow("flow-a")
	flowB := listTestFlow("flow-b")
	if err := store.Create(t.Context(), &flowA); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(t.Context(), &flowB); err != nil {
		t.Fatal(err)
	}

	// Explicit synchronization: B is deleted through the seam between the key
	// enumeration and B's own read. No sleep and no retry probability.
	seamHitsForB := 0
	store.beforeListGet = func(ctx context.Context, key string) {
		if key != flowB.ID {
			return
		}
		seamHitsForB++
		if err := store.kvStore.Delete(ctx, flowB.ID); err != nil {
			t.Errorf("seam delete of %s: %v", flowB.ID, err)
		}
	}

	flows, err := store.List(t.Context())
	if err != nil {
		t.Fatalf("List with a key deleted at the seam returned an error: %v", err)
	}
	if len(flows) != 1 || flows[0].ID != flowA.ID {
		t.Fatalf("List returned %v, want exactly [%s]", listIDs(flows), flowA.ID)
	}
	if seamHitsForB != 1 {
		t.Errorf("seam fired %d times for %s, want exactly 1", seamHitsForB, flowB.ID)
	}

	store.beforeListGet = nil
	after, err := store.List(t.Context())
	if err != nil {
		t.Fatalf("List after the deletion returned an error: %v", err)
	}
	if len(after) != 1 || after[0].ID != flowA.ID {
		t.Fatalf("List after the deletion returned %v, want exactly [%s]", listIDs(after), flowA.ID)
	}
}

func TestManagerListPreservesPerKeyTransientFailure(t *testing.T) {
	store, _ := newTestManager(t)
	flowA := listTestFlow("flow-a")
	flowB := listTestFlow("flow-b")
	if err := store.Create(t.Context(), &flowA); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(t.Context(), &flowB); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store.beforeListGet = func(_ context.Context, key string) {
		if key == flowB.ID {
			cancel()
		}
	}

	flows, err := store.List(ctx)
	if err == nil {
		t.Fatalf("List under a cancelled read returned no error (flows=%v)", listIDs(flows))
	}
	if !errs.IsTransient(err) {
		t.Errorf("List error is not classified transient: %v", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("List error lost the cancellation cause: %v", err)
	}
	if errors.Is(err, natsclient.ErrKVKeyNotFound) {
		t.Errorf("a read that could not complete was reported as typed absence: %v", err)
	}
	if flows != nil {
		t.Errorf("List returned a partial result %v with an error, want nil", listIDs(flows))
	}
}

func TestManagerListPreservesCorruptRecordFailure(t *testing.T) {
	store, _ := newTestManager(t)
	flowA := listTestFlow("flow-a")
	if err := store.Create(t.Context(), &flowA); err != nil {
		t.Fatal(err)
	}
	if _, err := store.kvStore.Put(t.Context(), "corrupt-flow", []byte("{not json")); err != nil {
		t.Fatal(err)
	}

	flows, err := store.List(t.Context())
	if err == nil {
		t.Fatalf("List over a record that does not decode returned no error (flows=%v)", listIDs(flows))
	}
	if !errs.IsFatal(err) {
		t.Errorf("a stored record that does not decode is not classified fatal: %v", err)
	}
	if errs.IsTransient(err) {
		t.Errorf("a stored record that does not decode is classified transient: %v", err)
	}
	if errors.Is(err, natsclient.ErrKVKeyNotFound) {
		t.Errorf("a decode failure was reported as typed absence: %v", err)
	}
	if flows != nil {
		t.Errorf("List returned a partial result %v with an error, want nil", listIDs(flows))
	}
}
