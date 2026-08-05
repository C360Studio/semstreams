package graphingest

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type keyedIngestTestMsg struct {
	ack  atomic.Bool
	nak  atomic.Bool
	term atomic.Bool
}

// entity-id-audit:classify intentional-malformed "bad" line=164 column=61 surface=go-field:EntityState.ID entity_id_invalid:arity malformed keyed-ingest state rejection fixture

func (*keyedIngestTestMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (*keyedIngestTestMsg) Data() []byte                              { return nil }
func (*keyedIngestTestMsg) Headers() nats.Header                      { return nil }
func (*keyedIngestTestMsg) Subject() string                           { return "entity.test" }
func (*keyedIngestTestMsg) Reply() string                             { return "" }
func (m *keyedIngestTestMsg) Ack() error                              { m.ack.Store(true); return nil }
func (*keyedIngestTestMsg) DoubleAck(context.Context) error           { return nil }
func (m *keyedIngestTestMsg) Nak() error                              { m.nak.Store(true); return nil }
func (m *keyedIngestTestMsg) NakWithDelay(time.Duration) error        { m.nak.Store(true); return nil }
func (*keyedIngestTestMsg) InProgress() error                         { return nil }
func (m *keyedIngestTestMsg) Term() error                             { m.term.Store(true); return nil }
func (m *keyedIngestTestMsg) TermWithReason(string) error             { m.term.Store(true); return nil }

// --- laneGuard (in-memory guard tier, ADR-072) ---

func TestLaneGuard_GetSet(t *testing.T) {
	g := newLaneGuard(8)
	_, ok := g.get("k")
	assert.False(t, ok, "absent key is a miss")

	g.set("k", 5)
	v, ok := g.get("k")
	assert.True(t, ok)
	assert.Equal(t, uint64(5), v)

	g.set("k", 9) // update in place
	v, _ = g.get("k")
	assert.Equal(t, uint64(9), v)
}

func TestLaneGuard_BoundedEviction(t *testing.T) {
	g := newLaneGuard(2)
	g.set("a", 1)
	g.set("b", 2)
	require.Len(t, g.seq, 2)

	// Inserting a third distinct key evicts one arbitrary entry (durable tier
	// backs correctness, so which one is immaterial) — the map stays bounded.
	g.set("c", 3)
	assert.Len(t, g.seq, 2, "map stays bounded at max")
	v, ok := g.get("c")
	assert.True(t, ok, "the just-inserted key is present")
	assert.Equal(t, uint64(3), v)
}

func TestLaneGuard_UpdateAtCapDoesNotEvict(t *testing.T) {
	g := newLaneGuard(2)
	g.set("a", 1)
	g.set("b", 2)
	// Updating an EXISTING key at capacity must not evict (no new entry).
	g.set("a", 10)
	assert.Len(t, g.seq, 2)
	va, _ := g.get("a")
	vb, okb := g.get("b")
	assert.Equal(t, uint64(10), va)
	assert.True(t, okb, "existing sibling not evicted by an in-place update")
	assert.Equal(t, uint64(2), vb)
}

func TestGuardKey(t *testing.T) {
	assert.Equal(t, "c360.ops.robotics.gcs.drone.001/SENSOR",
		guardKey("c360.ops.robotics.gcs.drone.001", "SENSOR"))
}

// --- ingestGuardStale: in-memory tier + nil-durable (no NATS) ---

func TestIngestGuardStale_MemoryTierAndFirstSeen(t *testing.T) {
	c := &Component{
		ingestGuardMem:    []*laneGuard{newLaneGuard(16)},
		ingestGuardBucket: nil, // no durable tier in this unit test
	}
	ctx := context.Background()
	work := ingestWork{entityID: "c360.a.b.c.d.001", stream: "SENSOR", seq: 5}

	// First-seen (tier-1 miss + nil durable) → not stale.
	stale, err := c.ingestGuardStale(ctx, 0, work)
	require.NoError(t, err)
	assert.False(t, stale, "an unseen (entity,stream) is not stale")

	// Stamp the in-memory tier at seq 5, then re-check.
	c.ingestGuardMem[0].set(guardKey(work.entityID, work.stream), 5)

	older := work
	older.seq = 3
	stale, err = c.ingestGuardStale(ctx, 0, older)
	require.NoError(t, err)
	assert.True(t, stale, "a lower sequence than last-applied is a stale redelivery")

	same := work
	same.seq = 5
	stale, err = c.ingestGuardStale(ctx, 0, same)
	require.NoError(t, err)
	assert.True(t, stale, "an equal sequence (redelivery of the applied message) is stale")

	newer := work
	newer.seq = 6
	stale, err = c.ingestGuardStale(ctx, 0, newer)
	require.NoError(t, err)
	assert.False(t, stale, "a higher sequence is a fresh update, not stale")
}

// A different stream for the same entity must NOT be judged against another
// stream's sequence (independent per-stream sequence spaces — round-2 fix).
func TestIngestGuardStale_PerStreamIndependence(t *testing.T) {
	c := &Component{
		ingestGuardMem:    []*laneGuard{newLaneGuard(16)},
		ingestGuardBucket: nil,
	}
	ctx := context.Background()
	entity := "c360.a.b.c.d.001"

	// Stream A applied a high sequence.
	c.ingestGuardMem[0].set(guardKey(entity, "STREAM_A"), 1000)

	// A low sequence from stream B is a DIFFERENT key → not stale.
	fromB := ingestWork{entityID: entity, stream: "STREAM_B", seq: 5}
	stale, err := c.ingestGuardStale(ctx, 0, fromB)
	require.NoError(t, err)
	assert.False(t, stale, "a low seq from stream B is not silenced by stream A's high seq")
}

func TestProcessIngest_InvalidGraphableTerminatesBeforeGuardIO(t *testing.T) {
	validID := "acme.ops.test.system.widget.001"
	tests := []struct {
		name        string
		entity      *graph.EntityState
		field       string
		reason      string
		tripleIndex int
		predicate   bool
		unmetered   bool
	}{
		{name: "invalid envelope", entity: &graph.EntityState{ID: "bad"}, field: "id", reason: "arity", tripleIndex: -1},
		{name: "cross subject", entity: &graph.EntityState{ID: validID, Triples: []message.Triple{{Subject: "bad", Predicate: "test.state.value"}}}, field: "id", reason: "unknown", tripleIndex: -1, unmetered: true},
		{name: "invalid explicit reference", entity: &graph.EntityState{ID: validID, Triples: []message.Triple{{Subject: validID, Predicate: "test.state.value", Object: 42, Datatype: message.EntityReferenceDatatype}}}, field: "reference", reason: "object_type", tripleIndex: 0},
		{
			name: "invalid predicate",
			entity: &graph.EntityState{ID: validID, Triples: []message.Triple{{
				Subject: validID, Predicate: "bad.two", // predicate-audit:invalid {"kind":"stored-predicate","value":"bad.two","reason":"arity"}
			}}},
			field: "predicate", reason: "arity", tripleIndex: -1,
			predicate: true, // predicate-audit:unrelated {"column":15,"surface":"go-field:predicate","value":"","basis":"reviewed boolean expectation flag, not predicate syntax"}
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			component, _ := createTestComponentWithMockKVBucket(t)
			var logs bytes.Buffer
			component.logger = slog.New(slog.NewTextHandler(&logs, nil))
			guardBucket := newMockKVBucket()
			var guardGets atomic.Int32
			guardBucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
				guardGets.Add(1)
				return nil, jetstream.ErrKeyNotFound
			}
			component.ingestGuardBucket = component.natsClient.NewKVStore(guardBucket)
			component.ingestGuardMem = []*laneGuard{newLaneGuard(16)}
			msg := &keyedIngestTestMsg{}
			work := ingestWork{entity: tt.entity, msg: msg, entityID: tt.entity.ID, stream: "ENTITY", seq: 1}
			errorsBefore := atomic.LoadInt64(&component.errors)

			var before float64
			if tt.predicate {
				before = testutil.ToFloat64(component.predicateContractRejections.WithLabelValues("graphable", tt.reason))
			} else {
				before = testutil.ToFloat64(component.entityStateContractRejections.WithLabelValues("graphable", tt.field, tt.reason))
			}

			err := component.processIngest(context.Background(), 0, work)
			require.Error(t, err)
			require.True(t, errs.IsInvalid(err), "structural failure must be nonretryable")
			require.Equal(t, int32(0), guardGets.Load(), "invalid candidate reached durable guard Get")
			require.True(t, msg.term.Load(), "immutable invalid message must Term")
			require.False(t, msg.nak.Load(), "immutable invalid message must not redeliver")
			require.False(t, msg.ack.Load(), "terminal rejection is not an ack path")
			require.Equal(t, errorsBefore, atomic.LoadInt64(&component.errors), "pre-guard rejection must not poison sticky error state")

			if !tt.unmetered && tt.predicate {
				assert.InDelta(t, before+1, testutil.ToFloat64(component.predicateContractRejections.WithLabelValues("graphable", tt.reason)), 0.0001)
			} else if !tt.unmetered {
				assert.InDelta(t, before+1, testutil.ToFloat64(component.entityStateContractRejections.WithLabelValues("graphable", tt.field, tt.reason)), 0.0001)
			}
			logLine := logs.String()
			assert.Equal(t, 1, strings.Count(logLine, "level=WARN"), "one rejection warning per message")
			assert.Contains(t, logLine, "lane=graphable")
			assert.Contains(t, logLine, "field="+tt.field)
			assert.Contains(t, logLine, "reason="+tt.reason)
			if tt.tripleIndex >= 0 {
				assert.Contains(t, logLine, "triple_index=0")
			} else {
				assert.NotContains(t, logLine, "triple_index=")
			}
			assert.NotContains(t, logLine, "entity_id=")
			assert.NotContains(t, logLine, "error=")
		})
	}
}

func TestProcessIngest_DirtyStoredStateNaksAndInventoriesPerEntity(t *testing.T) {
	tests := []struct {
		name        string
		entityID    string
		stored      []byte
		resetReason graph.StateResetReason
	}{
		{
			name:        "noncanonical predicate",
			entityID:    "acme.ops.test.system.widget.dirty-predicate",
			stored:      []byte(`{"id":"acme.ops.test.system.widget.dirty-predicate","triples":[{"subject":"acme.ops.test.system.widget.dirty-predicate","predicate":"bad.two"}]}`), // predicate-audit:invalid {"kind":"stored-predicate","value":"bad.two","reason":"arity"}
			resetReason: graph.GraphStateReasonNoncanonicalPredicate,
		},
		{
			name:        "noncanonical subject",
			entityID:    "acme.ops.test.system.widget.dirty-subject",
			stored:      []byte(`{"id":"acme.ops.test.system.widget.dirty-subject","triples":[{"subject":"bad","predicate":"test.state.value"}]}`),
			resetReason: graph.GraphStateReasonNoncanonicalEntityID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			component, entityBucket := createTestComponentWithMockKVBucket(t)
			entityBucket.data[tt.entityID] = mockKVData{value: tt.stored, revision: 1}
			component.ingestGuardMem = []*laneGuard{newLaneGuard(16)}
			guardBucket := newMockKVBucket()
			var guardStamps atomic.Int32
			guardBucket.putFunc = func(context.Context, string, []byte) (uint64, error) {
				guardStamps.Add(1)
				return 1, nil
			}
			component.ingestGuardBucket = component.natsClient.NewKVStore(guardBucket)
			var logs bytes.Buffer
			component.logger = slog.New(slog.NewTextHandler(&logs, nil))
			msg := &keyedIngestTestMsg{}
			work := ingestWork{
				entity: &graph.EntityState{ID: tt.entityID, Triples: []message.Triple{{
					Subject: tt.entityID, Predicate: "test.state.value", Object: "new",
				}}},
				msg: msg, entityID: tt.entityID, stream: "ENTITY", seq: 2,
			}
			predicateBefore := testutil.ToFloat64(component.predicateContractRejections.WithLabelValues("graphable", "arity"))
			entityBefore := testutil.ToFloat64(component.entityStateContractRejections.WithLabelValues("graphable", "subject", "arity"))

			err := component.processIngest(context.Background(), 0, work)
			require.Error(t, err)
			var stateErr *graph.StateContractError
			require.True(t, errors.As(err, &stateErr), "dirty persisted state must retain its typed reset error")
			assert.Equal(t, tt.resetReason, stateErr.Reason)
			assert.Equal(t, tt.entityID, stateErr.EntityID, "RMW classification must stamp the entity ID")
			// D8: resident poison is the ENVIRONMENT's fault — Nak so the valid
			// arrival survives the repair window, never Term (permanent loss).
			assert.True(t, msg.nak.Load(), "resident poison must Nak for redelivery after repair")
			assert.False(t, msg.term.Load(), "resident poison must not destroy the valid arrival")
			assert.False(t, msg.ack.Load())
			assert.Equal(t, int32(0), guardStamps.Load(), "failed apply must not stamp the redelivery guard")
			// Per-entity inventory replaces the retired surface-global latch:
			// the entity is inventoried, queries stay READY.
			rec, inventoried := poisonInventoryEntry(component, tt.entityID)
			require.True(t, inventoried, "apply-discovered poison must be inventoried per-entity")
			assert.Equal(t, tt.entityID, rec.contractErr.EntityID)
			assert.NoError(t, component.ensureEntityQueriesReady(), "one poisoned entity must not gate the query surface")
			assert.Equal(t, predicateBefore, testutil.ToFloat64(component.predicateContractRejections.WithLabelValues("graphable", "arity")), "stored poison is not a producer predicate rejection")
			assert.Equal(t, entityBefore, testutil.ToFloat64(component.entityStateContractRejections.WithLabelValues("graphable", "subject", "arity")), "stored poison is not a producer entity-state rejection")
			assert.Equal(t, 1, strings.Count(logs.String(), "code=graph_state_reset_required"), "first inventory record logs once")
			assert.Contains(t, logs.String(), "reason="+string(tt.resetReason))

			// Redelivery loop dedup: a second delivery re-Naks without re-logging.
			msg2 := &keyedIngestTestMsg{}
			work.msg = msg2
			require.Error(t, component.processIngest(context.Background(), 0, work))
			assert.True(t, msg2.nak.Load())
			assert.Equal(t, 1, strings.Count(logs.String(), "code=graph_state_reset_required"), "redelivery must not re-log an inventoried entity")
		})
	}
}

func TestProcessIngest_GenericFatalApplyErrorTerminatesWithoutPoison(t *testing.T) {
	const entityID = "acme.ops.test.system.widget.storage-fatal"
	component, entityBucket := createTestComponentWithMockKVBucket(t)
	entityBucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
		return nil, errs.WrapFatal(errs.ErrStorageFull, "test", "entityGet", "storage exhausted")
	}
	component.entityBucket = component.natsClient.NewKVStore(entityBucket, func(options *natsclient.KVOptions) {
		options.MaxRetries = 0
	})
	component.ingestGuardMem = []*laneGuard{newLaneGuard(16)}
	var logs bytes.Buffer
	component.logger = slog.New(slog.NewTextHandler(&logs, nil))
	msg := &keyedIngestTestMsg{}

	err := component.processIngest(context.Background(), 0, ingestWork{
		entity: &graph.EntityState{ID: entityID, Triples: []message.Triple{{
			Subject: entityID, Predicate: "test.state.value", Object: "new",
		}}},
		msg: msg, entityID: entityID, stream: "ENTITY", seq: 1,
	})

	require.Error(t, err)
	require.True(t, errs.IsFatal(err), "classified fatal apply error must remain fatal through merge wrappers")
	assert.True(t, msg.term.Load(), "generic fatal apply failure must Term")
	assert.False(t, msg.nak.Load(), "generic fatal apply failure must not redeliver")
	assert.False(t, msg.ack.Load())
	assert.Equal(t, int64(0), component.entityPoisonSize.Load(), "generic fatal failure must not masquerade as graph-state poison")
	assert.NoError(t, component.ensureEntityQueriesReady(), "generic fatal failure must not gate queries")
	assert.Contains(t, logs.String(), "fatal ingest failure; terminating")
	assert.NotContains(t, logs.String(), "code=graph_state_reset_required")
}

func TestProcessIngest_FillsEmptyFactSubjectBeforeStaleGuard(t *testing.T) {
	component, _ := createTestComponentWithMockKVBucket(t)
	component.ingestGuardMem = []*laneGuard{newLaneGuard(16)}
	validID := "acme.ops.test.system.widget.001"
	component.ingestGuardMem[0].set(guardKey(validID, "ENTITY"), 1)
	entity := &graph.EntityState{ID: validID, Triples: []message.Triple{{Subject: "", Predicate: "test.state.value"}}}
	msg := &keyedIngestTestMsg{}

	err := component.processIngest(context.Background(), 0, ingestWork{
		entity: entity, msg: msg, entityID: validID, stream: "ENTITY", seq: 1,
	})
	require.NoError(t, err)
	require.Equal(t, validID, entity.Triples[0].Subject, "fact projection fill must precede stale-drop guard")
	require.True(t, msg.ack.Load())
	require.False(t, msg.term.Load())
	require.False(t, msg.nak.Load())
}
