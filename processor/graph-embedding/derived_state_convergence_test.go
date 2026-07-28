package graphembedding

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/graph/embedding"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

// This file is the unit coverage for the embedding-derived-state-convergence
// change (#629 single-writer hop-1 seam, #625 markStranded + repair loop). The
// tests drive the PRODUCTION paths — applyEntityWatchEntry (the watcher),
// processEntityBatch (the coalesced flush callback), repairStranded (the repair
// tick body) — against in-package KV fakes set on the unexported bucket fields
// (memkv_test.go precedent; no production test hooks).

// stateEntry is a KV entry with caller-chosen key/value/revision/operation —
// the package's shared mockKVEntry (fixed key, always Put) cannot express a
// revisioned update or tombstone.
type stateEntry struct {
	key   string
	value []byte
	rev   uint64
	op    jetstream.KeyValueOp
}

func (e *stateEntry) Key() string                     { return e.key }
func (e *stateEntry) Value() []byte                   { return e.value }
func (e *stateEntry) Revision() uint64                { return e.rev }
func (e *stateEntry) Created() time.Time              { return time.Now() }
func (e *stateEntry) Delta() uint64                   { return 0 }
func (e *stateEntry) Operation() jetstream.KeyValueOp { return e.op }
func (e *stateEntry) Bucket() string                  { return "ENTITY_STATES" }

// entityStatesStub is an ENTITY_STATES stand-in for the hop-1 seam tests. Only
// Get — reconcileEntity's sole read — is implemented; every other method hits
// the nil embedded interface and panics loudly, so a test that strays off the
// covered surface fails instead of silently succeeding on a zero value
// (memkv_test.go's "fail loudly" property, by a cheaper mechanism).
type entityStatesStub struct {
	jetstream.KeyValue // nil: any un-overridden call panics loudly

	mu      sync.Mutex
	entries map[string]*stateEntry
	getErr  error // when set, every Get returns it (transient-fault injection)
	// getHook, when set, receives the base lookup's result and decides what the
	// caller sees. It runs OUTSIDE the stub's mutex so it may block (T1's
	// captured-entry decorator).
	getHook func(key string, entry jetstream.KeyValueEntry, err error) (jetstream.KeyValueEntry, error)
	gets    []string // every Get key, in order (repair-scope assertions)
}

func newEntityStatesStub() *entityStatesStub {
	return &entityStatesStub{entries: make(map[string]*stateEntry)}
}

func (s *entityStatesStub) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	s.mu.Lock()
	s.gets = append(s.gets, key)
	var entry jetstream.KeyValueEntry
	var err error
	switch {
	case s.getErr != nil:
		err = s.getErr
	default:
		if e, ok := s.entries[key]; ok {
			entry = e
		} else {
			err = jetstream.ErrKeyNotFound
		}
	}
	hook := s.getHook
	s.mu.Unlock()
	if hook != nil {
		return hook(key, entry, err)
	}
	return entry, err
}

func (s *entityStatesStub) set(key string, value []byte, rev uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = &stateEntry{key: key, value: value, rev: rev, op: jetstream.KeyValuePut}
}

func (s *entityStatesStub) remove(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
}

func (s *entityStatesStub) setGetErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getErr = err
}

func (s *entityStatesStub) recordedGets() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.gets))
	copy(out, s.gets)
	return out
}

// newSeamTestComponent builds a Component wired for hop-1 seam tests: the
// findings-test base (real Storage over the mock index, watermark, failed map)
// plus an ENTITY_STATES stub on the unexported bucket field.
func newSeamTestComponent(t *testing.T, index jetstream.KeyValue, states *entityStatesStub) *Component {
	t.Helper()
	c := newFindingsTestComponent(t, index)
	c.entityStatesBucket = states
	return c
}

// TestHop1Seam_CoalescedFlushCannotResurrectTombstonedEntity is the #629
// resurrection repro (T1), the deterministic version of the issue's race:
//
//  1. the coalesced flush reads authoritative ENTITY_STATES and sees the entity
//     PRESENT at revision 4 (the stub captures that entry, signals, and blocks —
//     modelling a read that linearized before the delete but whose result is
//     used after);
//  2. the tombstone (revision 5) is processed by the watcher path mid-window;
//  3. the flush is released and continues with its captured pre-delete entry.
//
// Two legs, both discriminating against the pre-seam code:
//
//   - ORDERING: under the hop-1 seam the tombstone's delete is BLOCKED on hop1Mu
//     while the flush's authoritative read is in flight, so it must NOT complete
//     before the release. Pre-seam nothing blocks it and it completes in
//     microseconds.
//   - CONVERGENCE: after both complete, the derived record is ABSENT — the
//     tombstone's delete serialized after the stale flush's SavePending and won.
//     Pre-seam the flush's unguarded SavePending recreates the key AFTER the
//     delete and the dead entity regains a vector record.
func TestHop1Seam_CoalescedFlushCannotResurrectTombstonedEntity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.robotics.gcs.drone.res1"

	index := newMockKVBucket()
	states := newEntityStatesStub()
	c := newSeamTestComponent(t, index, states)

	// The entity exists at revision 4, with a pending record from an earlier
	// delivery, and is queued in the coalescing window.
	states.set(entityID, textEntityJSON(entityID), 4)
	require.NoError(t, c.storage.SavePending(ctx, entityID, "", "survey the north field", 3))

	getIssued := make(chan struct{})
	releaseGet := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseGet) }) }
	defer release() // never leak the blocked flush goroutine on a failed leg
	var signalOnce sync.Once
	states.getHook = func(_ string, entry jetstream.KeyValueEntry, err error) (jetstream.KeyValueEntry, error) {
		// Capture the pre-delete lookup result, signal, block until released.
		signalOnce.Do(func() { close(getIssued) })
		<-releaseGet
		return entry, err
	}

	// The coalesced flush fires on its own goroutine (production: the
	// CoalescingSet callback goroutine). The entity has already been extracted
	// from the pending set at this point, so the tombstone's coalescer.Remove is
	// irrelevant to this interleaving (coalescer stays nil).
	flushDone := make(chan struct{})
	go func() {
		defer close(flushDone)
		c.processEntityBatch(ctx, []string{entityID})
	}()

	<-getIssued

	// The tombstone lands mid-window: authoritative state gone, then the watcher
	// processes the delete (production: the ENTITY_STATES watcher goroutine).
	states.remove(entityID)
	tombstoneDone := make(chan struct{})
	go func() {
		defer close(tombstoneDone)
		c.applyEntityWatchEntry(ctx, &stateEntry{key: entityID, rev: 5, op: jetstream.KeyValueDelete})
	}()

	// ORDERING LEG. Bounded absence wait, not a sleep-for-progress: under the
	// seam the tombstone goroutine is provably blocked on hop1Mu (held across the
	// flush's in-flight authoritative read), so this timeout can only cost wall
	// time, never flake. On the pre-seam code the delete has nothing to block it
	// and completes in microseconds, so 200ms is orders of magnitude of margin
	// for observing the violation.
	select {
	case <-tombstoneDone:
		t.Error("tombstone delete completed while the coalesced flush's authoritative read was in flight; " +
			"the hop-1 seam must serialize them (#629)")
	case <-time.After(200 * time.Millisecond):
	}

	release()
	<-flushDone
	<-tombstoneDone

	rec, err := c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.Nil(t, rec,
		"a tombstoned entity must not regain a derived embedding record via a stale coalesced flush (#629)")
}

// TestCoalescedFlush_AuthoritativeAbsenceDeletesDerivedRecord (T2): when the
// flush's reconcile reads ENTITY_STATES and finds the key absent or deleted
// (BOTH JetStream sentinels), the derived record is DELETED — not silently
// drained past, which is what the pre-change processEntityBatch did (its
// missing-entity branch completed the watermark and left the record queryable).
// The watermark still drains either way (a stranded flush must not pin
// readiness, ADR-066 §3).
func TestCoalescedFlush_AuthoritativeAbsenceDeletesDerivedRecord(t *testing.T) {
	t.Parallel()
	sentinels := []struct {
		name   string
		getErr error // nil = natural miss (ErrKeyNotFound from the stub's lookup)
	}{
		{name: "ErrKeyNotFound", getErr: nil},
		{name: "ErrKeyDeleted", getErr: jetstream.ErrKeyDeleted},
	}
	for _, tc := range sentinels {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			const entityID = "acme.ops.robotics.gcs.drone.abs1"

			index := newMockKVBucket()
			states := newEntityStatesStub() // entity NOT present
			if tc.getErr != nil {
				states.setGetErr(tc.getErr)
			}
			c := newSeamTestComponent(t, index, states)

			// A derived record exists from before the deletion, and its revision is
			// still pending in the watermark (the coalescer collapsed it).
			require.NoError(t, c.storage.SavePending(ctx, entityID, "", "stale text", 3))
			c.watermark.Observe(3, entityID, time.Now())

			c.processEntityBatch(ctx, []string{entityID})

			rec, err := c.storage.GetEmbedding(ctx, entityID)
			require.NoError(t, err)
			require.Nil(t, rec,
				"authoritative absence on the coalesced lane must DELETE the derived record, not skip it")
			require.Equal(t, uint64(3), c.watermark.Indexed(),
				"the watermark must still drain on the absence branch (ADR-066 §3)")
			count, _, _ := c.failedSnapshot()
			require.Zero(t, count, "a clean absence convergence is not a failure")
		})
	}
}

// TestFailedDelete_MarksStrandedAndRepairs (T3) is the #625 statement for the
// tombstone-delete-failure site:
//
//   - a failed derived-record delete leaves the record present, enters the
//     current-failed map under ReasonDeleteFailed (→ degraded, previously a
//     silent `ready` leak), while the readiness watermark STILL drains at the
//     tombstone's revision (#624: a failed delete never pins readiness);
//   - the repair loop body (repairStranded, called directly — no ticker, no
//     sleep) re-drives the entity until the derived key is absent, decrements
//     FailedCount, and leaves the watermark UNCHANGED.
func TestFailedDelete_MarksStrandedAndRepairs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.robotics.gcs.drone.del1"

	index := newMockKVBucket()
	deleteFailing := true
	index.deleteFunc = func(_ context.Context, key string, _ ...jetstream.KVDeleteOpt) error {
		if deleteFailing {
			return errors.New("kv delete unavailable")
		}
		index.mu.Lock()
		defer index.mu.Unlock()
		delete(index.data, key)
		return nil
	}
	states := newEntityStatesStub() // authoritative state already absent
	c := newSeamTestComponent(t, index, states)

	// Derived record exists at revision 5; the tombstone arrives at revision 6.
	require.NoError(t, c.storage.SavePending(ctx, entityID, "", "old text", 5))
	c.applyEntityWatchEntry(ctx, &stateEntry{key: entityID, rev: 6, op: jetstream.KeyValueDelete})

	rec, err := c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.NotNil(t, rec, "the failed delete leaves the derived record present until repaired")

	// #624 invariant, asserted explicitly: the watermark has drained at the
	// tombstone's TRUE revision despite the failed delete.
	require.Equal(t, uint64(6), c.watermark.Indexed(),
		"a failed delete must never pin the readiness watermark (#624)")

	count, reasons, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), count,
		"a failed derived-record delete must enter the current-failed accounting (#625)")
	require.Equal(t, uint64(1), reasons[embedding.ReasonDeleteFailed])

	// State projection with the component's own inputs: degraded, never ready.
	// (graph.ComputeIndexStatus is the exact projection computeEmbeddingStatus
	// wires these inputs into; the full compute needs a real bucket's
	// BucketLastSeq and is covered by the T8 integration test.)
	require.Equal(t, graph.IndexStateDegraded,
		graph.ComputeIndexStatus(graph.IndexStatusInputs{Indexed: 6, Target: 6, FailedCount: count}).State,
		"a producer holding a failed derived delete reports degraded, not ready")

	// The KV fault clears; the repair tick body re-drives the delete. Called
	// directly — no ticker, no sleep — the loop's only job is cadence.
	deleteFailing = false
	c.repairStranded(ctx)

	rec, err = c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.Nil(t, rec, "repair must retry the delete until the derived key is absent (#625)")
	count, _, _ = c.failedSnapshot()
	require.Zero(t, count, "FailedCount decrements when the last stranded delete converges")
	require.Equal(t, uint64(6), c.watermark.Indexed(),
		"the readiness watermark is UNCHANGED by repair (it drained at the tombstone, #624)")
	require.Equal(t, graph.IndexStateReady,
		graph.ComputeIndexStatus(graph.IndexStatusInputs{Indexed: 6, Target: 6, FailedCount: count}).State,
		"degraded clears to ready once repaired")
}

// TestSavePendingFailure_MarksStrandedForRepair (T4) is the #625 statement for
// the pending-write-failure site: a failed hop-1 SavePending leaves the
// revision UNCOMPLETED (#613 F2 — asserted explicitly) and now ALSO enters the
// current-failed map under ReasonPendingWriteFailed so it surfaces as degraded
// immediately and repair re-drives it, instead of waiting for the next
// incidental re-delivery.
func TestSavePendingFailure_MarksStrandedForRepair(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.robotics.gcs.drone.pend1"

	index := newMockKVBucket()
	putFailing := true
	index.putFunc = func(_ context.Context, key string, value []byte) (uint64, error) {
		if putFailing {
			return 0, errors.New("kv write unavailable")
		}
		index.mu.Lock()
		defer index.mu.Unlock()
		index.data[key] = value
		return 1, nil
	}
	// The guarded writer creates an absent key via KV Create (not Put), so the
	// write-failure premise needs the create hook too (#722 B2).
	hooked := &createHookKV{mockKVBucket: index}
	hooked.createFunc = func(_ context.Context, key string, value []byte) (uint64, error) {
		if putFailing {
			return 0, errors.New("kv write unavailable")
		}
		return hooked.mockKVBucket.Create(context.Background(), key, value)
	}
	states := newEntityStatesStub()
	states.set(entityID, textEntityJSON(entityID), 7)
	c := newSeamTestComponent(t, hooked, states)

	// The update is delivered through the production watcher path (immediate
	// mode); its pending write fails.
	c.applyEntityWatchEntry(ctx, &stateEntry{key: entityID, value: textEntityJSON(entityID), rev: 7, op: jetstream.KeyValuePut})

	require.Zero(t, c.embeddingCompletions.Load(),
		"a SavePending failure is non-terminal: the watermark must NOT be completed (#613 F2)")
	require.Equal(t, uint64(6), c.watermark.Indexed(),
		"revision 7 stays pending: readiness stays honest over un-persisted work")

	count, reasons, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), count,
		"a failed pending write must enter the current-failed accounting (#625)")
	require.Equal(t, uint64(1), reasons[embedding.ReasonPendingWriteFailed])

	// The KV fault clears; repair re-queues from AUTHORITATIVE state (a fresh
	// Get under the seam), writing the pending record hop 2 will drive.
	putFailing = false
	c.repairStranded(ctx)

	rec, err := c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.NotNil(t, rec, "repair must re-queue the pending record from authoritative state")
	require.Equal(t, embedding.StatusPending, rec.Status)
	require.Equal(t, uint64(7), rec.SourceRevision,
		"the re-queued record carries the authoritative revision read at repair time")

	// Queue success IS the hop-1 convergence: the stranding obligation is
	// discharged at repair time (causal-clear invariant, #722 B1) — a pending
	// record is not a failure; readiness is carried by the still-open watermark
	// until hop 2's terminal.
	count, _, _ = c.failedSnapshot()
	require.Zero(t, count, "the pending-write stranding is discharged by the successful re-queue")
	require.Equal(t, uint64(6), c.watermark.Indexed(),
		"revision 7 stays PENDING until hop 2 completes it — readiness stays honest past the repair")

	// Hop 2's terminal (the production onTerminal path) drains the watermark.
	c.completeEmbedding(entityID, 7, embedding.OutcomeGenerated, "")
	require.Equal(t, uint64(7), c.watermark.Indexed(),
		"revision 7 completes at the true hop-2 terminal")
}

// TestRepairScope_EmbedderSideReasonNotReDriven (T5): the repair set is scoped
// to the three derived-write/read reasons ONLY. An embedder-side failure
// (connection_refused here) keeps its existing recovery contract — re-delivery
// on restart or a new revision — and must not be re-driven by the repair loop:
// a permanently-failing entity (poison content, dead endpoint) would otherwise
// hot-loop KV and embedder traffic every tick forever.
func TestRepairScope_EmbedderSideReasonNotReDriven(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.robotics.gcs.drone.scope1"

	index := newMockKVBucket()
	states := newEntityStatesStub()
	c := newSeamTestComponent(t, index, states)

	// An embedder-side failure at a real revision (the hop-2 onTerminal path).
	c.completeEmbedding(entityID, 5, embedding.OutcomeFailed, "connection_refused")

	c.repairStranded(ctx)

	require.Empty(t, states.recordedGets(),
		"an embedder-side failure reason must not enter the repair lane (no authoritative re-read)")
	count, reasons, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), count, "the failure stays counted (degraded) — repair just does not drive it")
	require.Equal(t, uint64(1), reasons["connection_refused"], "the reason is untouched")
}

// TestStrandedReasons_NeverPersistToStoredRecords (T6): the three derived-write
// reasons are PROCESS-LOCAL accounting only. Driving all three stranding sites,
// no stored EMBEDDING_INDEX record may ever carry StatusFailed or any of the
// derived-write reasons — SaveFailed is never called on these paths (a
// derived-write failure means the durable state could not be brought to truth;
// writing a durable "failed" record about it is the same class of write). The
// sibling guard — normalizeFailureReason NOT extended with these reasons —
// lives in graph/embedding/derived_reasons_test.go next to the unexported enum.
func TestStrandedReasons_NeverPersistToStoredRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	index := newMockKVBucket()
	var (
		writesMu sync.Mutex
		writes   [][]byte
	)
	failWrites := true
	index.putFunc = func(_ context.Context, key string, value []byte) (uint64, error) {
		writesMu.Lock()
		writes = append(writes, append([]byte(nil), value...))
		writesMu.Unlock()
		if failWrites {
			return 0, errors.New("kv write unavailable")
		}
		index.mu.Lock()
		defer index.mu.Unlock()
		index.data[key] = value
		return 1, nil
	}
	index.deleteFunc = func(_ context.Context, _ string, _ ...jetstream.KVDeleteOpt) error {
		return errors.New("kv delete unavailable")
	}
	// The guarded writer creates an absent key via KV Create (not Put): record
	// and fail those payloads too, so the invariant scan covers the create lane.
	hooked := &createHookKV{mockKVBucket: index}
	hooked.createFunc = func(_ context.Context, _ string, value []byte) (uint64, error) {
		writesMu.Lock()
		writes = append(writes, append([]byte(nil), value...))
		writesMu.Unlock()
		return 0, errors.New("kv write unavailable")
	}
	states := newEntityStatesStub()
	c := newSeamTestComponent(t, hooked, states)

	// Site 1: tombstone delete failure.
	c.applyEntityWatchEntry(ctx, &stateEntry{key: "acme.ops.robotics.gcs.drone.t6a", rev: 3, op: jetstream.KeyValueDelete})
	// Site 2: pending write failure (update through the watcher path).
	const pendID = "acme.ops.robotics.gcs.drone.t6b"
	states.set(pendID, textEntityJSON(pendID), 4)
	c.applyEntityWatchEntry(ctx, &stateEntry{key: pendID, value: textEntityJSON(pendID), rev: 4, op: jetstream.KeyValuePut})
	// Site 3: authoritative read failure (coalesced-flush reconcile).
	states.setGetErr(errors.New("nats timeout"))
	c.processEntityBatch(ctx, []string{"acme.ops.robotics.gcs.drone.t6c"})

	// All three sites actually fired (discriminating precondition).
	_, reasons, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), reasons[embedding.ReasonDeleteFailed])
	require.Equal(t, uint64(1), reasons[embedding.ReasonPendingWriteFailed])
	require.Equal(t, uint64(1), reasons[embedding.ReasonEntityReadFailed])

	// No write attempted on any stranding path was a failed-record write, and no
	// payload anywhere carries a derived-write reason.
	writesMu.Lock()
	defer writesMu.Unlock()
	for _, raw := range writes {
		var rec embedding.Record
		require.NoError(t, json.Unmarshal(raw, &rec))
		require.NotEqual(t, embedding.StatusFailed, rec.Status,
			"a stranding path must never write a durable failed record (in-memory only)")
		for _, reason := range []string{embedding.ReasonDeleteFailed, embedding.ReasonPendingWriteFailed, embedding.ReasonEntityReadFailed} {
			require.NotEqual(t, reason, rec.Reason,
				"a derived-write reason must never persist into a stored record")
		}
	}
}

// createHookKV decorates the shared mockKVBucket with a Create hook. The shared
// mock predates the guarded pending-create lane (and is generated — DO NOT
// EDIT), so the hook lives here: the guarded hop-1 writer creates an absent key
// via KV Create, which mockKVBucket.putFunc cannot intercept.
type createHookKV struct {
	*mockKVBucket
	createFunc func(ctx context.Context, key string, value []byte) (uint64, error)
}

func (k *createHookKV) Create(ctx context.Context, key string, value []byte, _ ...jetstream.KVCreateOpt) (uint64, error) {
	if k.createFunc != nil {
		return k.createFunc(ctx, key, value)
	}
	return k.mockKVBucket.Create(ctx, key, value)
}

// TestObsoleteTerminal_CannotClearStranding_TombstoneSite (Codex #722 B1): hop 2
// is deliberately OUTSIDE the hop-1 seam, so a worker already in flight for an
// OLDER revision can reach its terminal AFTER a stranding is recorded. An
// obsolete terminal must not count as convergence: rev-6 tombstone's delete
// fails (stranded), then an in-flight rev-5 Generated terminal lands through
// the production completeEmbedding path — the mark MUST survive (under the old
// floor-0 rule, 5 >= 0 cleared it: dead vector queryable, FailedCount 0,
// ready, repair no longer targeting — the masking class). A causally NEWER
// terminal or the repair convergence still clears it.
func TestObsoleteTerminal_CannotClearStranding_TombstoneSite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.robotics.gcs.drone.obs1"

	index := newMockKVBucket()
	deleteFailing := true
	index.deleteFunc = func(_ context.Context, key string, _ ...jetstream.KVDeleteOpt) error {
		if deleteFailing {
			return errors.New("kv delete unavailable")
		}
		index.mu.Lock()
		defer index.mu.Unlock()
		delete(index.data, key)
		return nil
	}
	states := newEntityStatesStub()
	c := newSeamTestComponent(t, index, states)

	// Record exists at revision 5; tombstone at revision 6, delete fails → stranded.
	require.NoError(t, c.storage.SavePending(ctx, entityID, "", "old text", 5))
	c.applyEntityWatchEntry(ctx, &stateEntry{key: entityID, rev: 6, op: jetstream.KeyValueDelete})
	count, reasons, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), count, "precondition: the failed delete stranded the entity")
	require.Equal(t, uint64(1), reasons[embedding.ReasonDeleteFailed])

	// An in-flight hop-2 worker for OLDER revision 5 reaches its terminal AFTER
	// the stranding, via the production onTerminal path.
	c.completeEmbedding(entityID, 5, embedding.OutcomeGenerated, "")

	count, reasons, _ = c.failedSnapshot()
	require.Equal(t, uint64(1), count,
		"an OBSOLETE in-flight terminal (rev 5 < stranding rev 6) must not clear the repair obligation")
	require.Equal(t, uint64(1), reasons[embedding.ReasonDeleteFailed], "the stranding reason survives")

	// Repair convergence still clears it — the mark is causal, not unclearable.
	deleteFailing = false
	c.repairStranded(ctx)
	rec, err := c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.Nil(t, rec)
	count, _, _ = c.failedSnapshot()
	require.Zero(t, count, "explicit reconcile convergence clears the stranding")
}

// TestObsoleteTerminal_CannotClearStranding_PendingWriteSite is the same B1
// statement for the pending-write site, plus the clearable-by-causal-terminal
// leg (the trap the old floor-0 rule feared): a terminal AT the stranding
// revision or newer DOES clear.
func TestObsoleteTerminal_CannotClearStranding_PendingWriteSite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.robotics.gcs.drone.obs2"

	index := newMockKVBucket()
	putFailing := true
	index.putFunc = func(_ context.Context, key string, value []byte) (uint64, error) {
		if putFailing {
			return 0, errors.New("kv write unavailable")
		}
		index.mu.Lock()
		defer index.mu.Unlock()
		index.data[key] = value
		return 1, nil
	}
	hooked := &createHookKV{mockKVBucket: index}
	hooked.createFunc = func(_ context.Context, key string, value []byte) (uint64, error) {
		if putFailing {
			return 0, errors.New("kv write unavailable")
		}
		return hooked.mockKVBucket.Create(context.Background(), key, value)
	}
	states := newEntityStatesStub()
	states.set(entityID, textEntityJSON(entityID), 7)
	c := newSeamTestComponent(t, hooked, states)

	// Delivery at revision 7; the pending write fails → stranded at 7.
	c.applyEntityWatchEntry(ctx, &stateEntry{key: entityID, value: textEntityJSON(entityID), rev: 7, op: jetstream.KeyValuePut})
	count, reasons, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), count, "precondition: the failed pending write stranded the entity")
	require.Equal(t, uint64(1), reasons[embedding.ReasonPendingWriteFailed])

	// Obsolete in-flight terminal (rev 6 < 7): the mark survives.
	c.completeEmbedding(entityID, 6, embedding.OutcomeGenerated, "")
	count, reasons, _ = c.failedSnapshot()
	require.Equal(t, uint64(1), count,
		"an obsolete terminal below the stranding revision must not clear the mark")
	require.Equal(t, uint64(1), reasons[embedding.ReasonPendingWriteFailed])

	// Causal terminal AT the stranding revision clears — the mark is not a pin.
	c.completeEmbedding(entityID, 7, embedding.OutcomeGenerated, "")
	count, _, _ = c.failedSnapshot()
	require.Zero(t, count, "a terminal at/above the stranding revision clears causally")
}

// TestStaleRepairSnapshot_CannotDowngradeGeneratedRecord (Codex #722 B2):
// repairTargets snapshots then RELEASES failedMu, so hop 2 can generate and
// causally clear the stranding before the dispatch loop reaches the entity. The
// stale re-drive then re-queues through the sole hop-1 writer — whose pending
// write used to be an unconditional Put that DOWNGRADED the fresh
// StatusGenerated record to StatusPending (vector dropped from the cache, no
// new watermark obligation, FailedCount already 0 → ready with the vector gone
// until regeneration). The guarded writer must SKIP when a generated record at
// a same-or-newer source revision exists. The snapshot dispatch is driven
// manually (repairTargets + the exact repairStranded loop body) so the
// hop-2-between-snapshot-and-dispatch gate is deterministic.
func TestStaleRepairSnapshot_CannotDowngradeGeneratedRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.robotics.gcs.drone.snap1"

	index := newMockKVBucket()
	putFailing := true
	index.putFunc = func(_ context.Context, key string, value []byte) (uint64, error) {
		if putFailing {
			return 0, errors.New("kv write unavailable")
		}
		index.mu.Lock()
		defer index.mu.Unlock()
		index.data[key] = value
		return 1, nil
	}
	hooked := &createHookKV{mockKVBucket: index}
	hooked.createFunc = func(_ context.Context, key string, value []byte) (uint64, error) {
		if putFailing {
			return 0, errors.New("kv write unavailable")
		}
		return hooked.mockKVBucket.Create(context.Background(), key, value)
	}
	states := newEntityStatesStub()
	states.set(entityID, textEntityJSON(entityID), 7)
	c := newSeamTestComponent(t, hooked, states)

	// Strand: delivery at 7, pending write fails.
	c.applyEntityWatchEntry(ctx, &stateEntry{key: entityID, value: textEntityJSON(entityID), rev: 7, op: jetstream.KeyValuePut})
	count, _, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), count, "precondition: stranded")

	// The repair tick takes its snapshot FIRST...
	targets := c.repairTargets()
	require.Equal(t, []string{entityID}, targets)

	// ...then hop 2 recovers, generates at revision 7, and its terminal clears
	// the stranding causally — all BEFORE the dispatch loop runs.
	putFailing = false
	require.NoError(t, c.storage.SavePending(ctx, entityID, "", "survey the north field", 7))
	require.NoError(t, c.storage.SaveGenerated(ctx, entityID, []float32{1, 2, 3}, "bm25-384", 384, "hash-1", 7))
	c.completeEmbedding(entityID, 7, embedding.OutcomeGenerated, "")
	count, _, _ = c.failedSnapshot()
	require.Zero(t, count, "precondition: hop 2 cleared the stranding causally")

	// The STALE snapshot dispatches — the exact repairStranded loop body.
	for _, id := range targets {
		c.reconcileEntity(ctx, id)
	}

	rec, err := c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, embedding.StatusGenerated, rec.Status,
		"a stale repair re-drive must not downgrade a fresh generated record to pending")
	require.Equal(t, uint64(7), rec.SourceRevision)
	require.NotEmpty(t, rec.Vector, "the stored vector must be unchanged (cache eviction proxy)")
	count, _, _ = c.failedSnapshot()
	require.Zero(t, count)
}

// TestWatcherRedelivery_DoesNotDowngradeGeneratedRecord is B2's watcher-lane
// corollary: after a restart, last-per-subject re-delivers the entity at a
// revision whose vector is ALREADY generated. The guarded writer skips instead
// of downgrading (previously: overwrite to pending + full hop-2 regeneration on
// every restart), and the delivered revision completes as a skip so the
// watermark still drains.
func TestWatcherRedelivery_DoesNotDowngradeGeneratedRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.robotics.gcs.drone.redeliver1"

	index := newMockKVBucket()
	states := newEntityStatesStub()
	c := newSeamTestComponent(t, index, states)

	// Generated vector at revision 7 exists from before the restart.
	require.NoError(t, c.storage.SavePending(ctx, entityID, "", "survey the north field", 7))
	require.NoError(t, c.storage.SaveGenerated(ctx, entityID, []float32{1, 2, 3}, "bm25-384", 384, "hash-1", 7))

	// Restart re-delivery of the same revision through the watcher path.
	states.set(entityID, textEntityJSON(entityID), 7)
	c.applyEntityWatchEntry(ctx, &stateEntry{key: entityID, value: textEntityJSON(entityID), rev: 7, op: jetstream.KeyValuePut})

	rec, err := c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, embedding.StatusGenerated, rec.Status,
		"a re-delivered already-generated revision must skip, not downgrade to pending")
	require.Equal(t, uint64(7), rec.SourceRevision)
	require.NotEmpty(t, rec.Vector)
	require.Equal(t, uint64(7), c.watermark.Indexed(),
		"the skipped re-delivery still completes its revision (the watermark drains)")
}

// TestNoTextTransition_FailedDeleteMarksStrandedAndRepairs (reviewer round,
// class-scope): the no-text-transition delete is a MEMBER of the "failed
// derived-record delete" class the spec delta guarantees repair for — reached
// by the immediate watcher, the coalesced reconcile, AND repair. The harmful
// shape is a LIVE entity with a served StatusGenerated vector at revision N
// that transitions to no-text at N+1: if the delete fails transiently, the
// unconditional Skipped completion used to report FailedCount 0 / ready while
// the stale vector stayed queryable — the #625 harm, unrepaired because
// unmarked. Now: watermark drains at the delivered revision (ADR-066 §3,
// unchanged), the entity strands under ReasonDeleteFailed (degraded), and
// repairStranded converges it (present → no-text → delete retried → the
// successful pass's Skipped clears the mark).
func TestNoTextTransition_FailedDeleteMarksStrandedAndRepairs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.robotics.gcs.drone.ntx1"

	index := newMockKVBucket()
	deleteFailing := true
	index.deleteFunc = func(_ context.Context, key string, _ ...jetstream.KVDeleteOpt) error {
		if deleteFailing {
			return errors.New("kv delete unavailable")
		}
		index.mu.Lock()
		defer index.mu.Unlock()
		delete(index.data, key)
		return nil
	}
	states := newEntityStatesStub()
	c := newSeamTestComponent(t, index, states)

	// A previously GENERATED (served) vector at revision 5 — the harmful residue,
	// not the StatusFailed one the site's old comment named.
	require.NoError(t, c.storage.SavePending(ctx, entityID, "", "old text", 5))
	require.NoError(t, c.storage.SaveGenerated(ctx, entityID, []float32{1, 2, 3}, "bm25-384", 384, "hash-1", 5))

	// The live entity transitions to no-text at revision 6, via the immediate
	// watcher path; the derived-record delete fails transiently.
	states.set(entityID, noTextEntityJSON(entityID), 6)
	c.applyEntityWatchEntry(ctx, &stateEntry{key: entityID, value: noTextEntityJSON(entityID), rev: 6, op: jetstream.KeyValuePut})

	rec, err := c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.NotNil(t, rec, "the failed delete leaves the stale generated record present until repaired")
	require.Equal(t, embedding.StatusGenerated, rec.Status)

	require.Equal(t, uint64(6), c.watermark.Indexed(),
		"the Skipped terminal still drains the watermark at the delivered revision (ADR-066 §3)")
	count, reasons, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), count,
		"a failed no-text-transition delete must enter the failed accounting — same class as the tombstone site")
	require.Equal(t, uint64(1), reasons[embedding.ReasonDeleteFailed])
	require.Equal(t, graph.IndexStateDegraded,
		graph.ComputeIndexStatus(graph.IndexStatusInputs{Indexed: 6, Target: 6, FailedCount: count}).State)

	// The KV fault clears; repair converges: present → no-text → delete retried →
	// the successful pass's Skipped clears the mark.
	deleteFailing = false
	c.repairStranded(ctx)

	rec, err = c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.Nil(t, rec, "repair must retry the no-text delete until the derived key is absent")
	count, _, _ = c.failedSnapshot()
	require.Zero(t, count)
	require.Equal(t, uint64(6), c.watermark.Indexed(), "the watermark is unchanged by repair")
	require.Equal(t, graph.IndexStateReady,
		graph.ComputeIndexStatus(graph.IndexStatusInputs{Indexed: 6, Target: 6, FailedCount: count}).State)
}

// TestNoTextTransition_FailedDeleteDoesNotMaskPriorStranding is the
// repair-masking leg: an entity already stranded (pending_write_failed, floor
// 0) whose text was then removed. The repair re-drive reaches the no-text
// branch; its delete fails; the branch's fresh-revision Skipped completion
// CLEARS the floor-0 mark — so without the re-mark, degraded would clear
// WITHOUT convergence and repair would stop retrying. The mark must survive
// (as ReasonDeleteFailed — the delete is now the current stranding).
func TestNoTextTransition_FailedDeleteDoesNotMaskPriorStranding(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.robotics.gcs.drone.ntx2"

	index := newMockKVBucket()
	index.deleteFunc = func(_ context.Context, _ string, _ ...jetstream.KVDeleteOpt) error {
		return errors.New("kv delete unavailable")
	}
	states := newEntityStatesStub()
	c := newSeamTestComponent(t, index, states)

	// A stale derived record, a pending_write_failed stranding at revision 8,
	// and a live no-text entity — the state repairStranded would re-drive.
	require.NoError(t, c.storage.SavePending(ctx, entityID, "", "old text", 8))
	c.markStranded(entityID, embedding.ReasonPendingWriteFailed, 8)
	states.set(entityID, noTextEntityJSON(entityID), 9)

	c.reconcileEntity(ctx, entityID)

	count, reasons, _ := c.failedSnapshot()
	require.Equal(t, uint64(1), count,
		"a failing no-text delete must NOT clear a prior stranding without convergence (repair-masking)")
	require.Equal(t, uint64(1), reasons[embedding.ReasonDeleteFailed],
		"the surviving mark carries the CURRENT stranding reason (the failed delete)")
}

// TestImmediateMode_SemanticsUnchanged (T7): with no coalescer (coalesce_ms=0)
// the watcher's update and tombstone semantics are byte-identical to the
// pre-seam behavior — pending record written at the delivered revision with
// hop 2 owning the terminal, tombstone deleting the record and draining the
// watermark at its true revision. The seam only serializes; it changes no
// outcome (the ADDED requirement's "coalesced processing MUST NOT change
// outcomes" has this as its immediate-mode baseline).
func TestImmediateMode_SemanticsUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const entityID = "acme.ops.robotics.gcs.drone.imm1"

	index := newMockKVBucket()
	states := newEntityStatesStub()
	c := newSeamTestComponent(t, index, states) // entityCoalescer nil = immediate mode

	// Update at revision 9 → pending record at revision 9, watermark held open
	// for hop 2 (no immediate completion).
	states.set(entityID, textEntityJSON(entityID), 9)
	c.applyEntityWatchEntry(ctx, &stateEntry{key: entityID, value: textEntityJSON(entityID), rev: 9, op: jetstream.KeyValuePut})

	rec, err := c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, embedding.StatusPending, rec.Status)
	require.Equal(t, uint64(9), rec.SourceRevision)
	require.Zero(t, c.embeddingCompletions.Load(), "hop 2 owns the terminal for a queued record")
	require.Equal(t, uint64(8), c.watermark.Indexed(), "revision 9 stays pending until hop 2 completes it")

	// Tombstone at revision 10 → record deleted, watermark drained at 10.
	c.applyEntityWatchEntry(ctx, &stateEntry{key: entityID, rev: 10, op: jetstream.KeyValueDelete})

	rec, err = c.storage.GetEmbedding(ctx, entityID)
	require.NoError(t, err)
	require.Nil(t, rec, "the tombstone deletes the derived record (gh#614)")
	require.Equal(t, uint64(10), c.watermark.Indexed(), "the tombstone drains its own and the superseded revision")
	count, _, _ := c.failedSnapshot()
	require.Zero(t, count)
}
