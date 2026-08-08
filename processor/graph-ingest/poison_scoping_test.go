package graphingest

// poison_scoping_test.go — per-entity poison inventory behavior
// (poison-response-scoping tasks 7.4–7.6): repair recovery without restart,
// aggregate reads naming every poisoned entity, the three mutation read seams
// returning the typed fatal classification, the observability-only invariant
// (no read or write path consults the inventory), and the Nak disposition for
// resident-poison arrivals.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// poisonScopingTestComponent builds a full factory-created component on the
// mock bucket, marked running so Health() reports live state, with a fresh
// unregistered gauge so gauge assertions cannot interfere across tests.
func poisonScopingTestComponent(t *testing.T) (*Component, *mockKVBucket) {
	t.Helper()
	c, bucket := createTestComponentWithMockKVBucket(t)
	c.running = true
	c.startTime = time.Now()
	c.poisonedEntities = prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_poison_scoping_gauge"})
	return c, bucket
}

func seedPoisonBytes(bucket *mockKVBucket, id string, revision uint64) {
	bucket.data[id] = mockKVData{value: guardTestPoisonBytes(id), revision: revision}
}

func seedValidBytes(bucket *mockKVBucket, id string, revision uint64) {
	bucket.data[id] = mockKVData{value: guardTestValidBytes(id), revision: revision}
}

func queryEntity(t *testing.T, c *Component, id string) ([]byte, error) {
	t.Helper()
	return c.handleQueryEntityNATS(context.Background(), []byte(`{"id":"`+id+`"}`))
}

// TestPoisonRepairRecoveryWithoutRestart drives the spec scenario "operator
// repair recovers Health without restart": delete clears the inventory, the
// gauge reads zero, Health recovers, and a fresh canonical create serves.
func TestPoisonRepairRecoveryWithoutRestart(t *testing.T) {
	const id = "acme.ops.test.system.widget.repair"
	c, bucket := poisonScopingTestComponent(t)
	seedPoisonBytes(bucket, id, 1)

	// First touch detects, inventories, degrades Health.
	data, err := queryEntity(t, c, id)
	require.Nil(t, data)
	assertIngestResetRequired(t, err)
	_, inventoried := poisonInventoryEntry(c, id)
	require.True(t, inventoried)
	assert.Equal(t, float64(1), testutil.ToFloat64(c.poisonedEntities))
	health := c.Health()
	assert.False(t, health.Healthy)
	assert.Equal(t, graph.IndexStateDegraded, health.Status)

	// Canonical wire repair step 1: delete.
	require.NoError(t, c.deleteEntityAtRevision(context.Background(), id, 1))
	_, inventoried = poisonInventoryEntry(c, id)
	assert.False(t, inventoried, "delete must clear the inventory entry (D3a)")
	assert.Equal(t, float64(0), testutil.ToFloat64(c.poisonedEntities))
	health = c.Health()
	assert.True(t, health.Healthy, "Health must recover without restart: %+v", health)

	// Canonical wire repair step 2: fresh create serves.
	require.NoError(t, c.CreateEntity(context.Background(), &graph.EntityState{
		ID:      id,
		Version: 1,
		Triples: []message.Triple{{Subject: id, Predicate: "test.state.value", Object: "fresh", Timestamp: time.Now(), Confidence: 1.0}},
	}))
	data, err = queryEntity(t, c, id)
	require.NoError(t, err, "fresh create after repair must serve")
	assert.Contains(t, string(data), id)
}

// TestPoisonOutOfBandRepairClearsOnNextRead drives the spec scenario "an
// out-of-band repair clears on the next successful read" (D3c).
func TestPoisonOutOfBandRepairClearsOnNextRead(t *testing.T) {
	const id = "acme.ops.test.system.widget.oob"
	c, bucket := poisonScopingTestComponent(t)
	seedPoisonBytes(bucket, id, 1)

	_, err := queryEntity(t, c, id)
	assertIngestResetRequired(t, err)
	_, inventoried := poisonInventoryEntry(c, id)
	require.True(t, inventoried)

	// Out-of-band repair: bytes replaced OUTSIDE the mutation API.
	seedValidBytes(bucket, id, 2)

	data, err := queryEntity(t, c, id)
	require.NoError(t, err, "repaired bytes must serve — refusal derives from stored bytes only")
	assert.Contains(t, string(data), id)
	_, inventoried = poisonInventoryEntry(c, id)
	assert.False(t, inventoried, "successful validating read must clear the entry without restart")
	assert.Equal(t, float64(0), testutil.ToFloat64(c.poisonedEntities))
}

// TestPoisonRevisionGuardConcurrentRepair drives the spec scenario "a
// concurrent repair commit is not erased by a stale record": whichever order
// the record and the clear complete, the inventory ends with no entry — and a
// NEWER poison record is never erased by an older commit's proof.
func TestPoisonRevisionGuardConcurrentRepair(t *testing.T) {
	const id = "acme.ops.test.system.widget.race"
	ctx := context.Background()

	t.Run("record then newer commit clears", func(t *testing.T) {
		c, bucket := poisonScopingTestComponent(t)
		seedValidBytes(bucket, id, 2) // repair already committed at rev 2
		c.inventoryEntityPoison(context.Background(), &graph.StateContractError{
			Reason: graph.GraphStateReasonNoncanonicalPredicate, EntityID: id,
		}, 1) // stale record for poisoned rev 1
		c.clearEntityPoisonOnCommit(ctx, id, 2)
		_, inventoried := poisonInventoryEntry(c, id)
		assert.False(t, inventoried, "commit at a newer revision must clear the stale record")
	})

	t.Run("record arriving after repair records nothing", func(t *testing.T) {
		c, bucket := poisonScopingTestComponent(t)
		seedValidBytes(bucket, id, 2) // repair won the race before the record ran
		// The RMW classification path re-validates current bytes at record time.
		err := c.classifyStoredStateRMWError(ctx, id, guardTestPoisonBytes(id), errors.New("cycle error"))
		var stateErr *graph.StateContractError
		require.True(t, errors.As(err, &stateErr), "stale bytes still classify typed for the failing caller")
		_, inventoried := poisonInventoryEntry(c, id)
		assert.False(t, inventoried, "a record racing a completed repair must leave no stale entry")
	})

	t.Run("older commit proof does not erase newer poison", func(t *testing.T) {
		c, bucket := poisonScopingTestComponent(t)
		seedPoisonBytes(bucket, id, 5)
		c.inventoryEntityPoison(context.Background(), &graph.StateContractError{
			Reason: graph.GraphStateReasonNoncanonicalPredicate, EntityID: id,
		}, 5)
		c.clearEntityPoisonOnCommit(ctx, id, 3) // proof older than the recorded poison
		_, inventoried := poisonInventoryEntry(c, id)
		assert.True(t, inventoried, "an older proof must not erase a newer poison record")
	})
}

// TestPoisonRepoisonIsReinventoriedAndRelogged drives the spec scenario
// "re-poisoned entity is re-inventoried and re-logged". Not parallel: asserts
// on the component-local slog capture.
func TestPoisonRepoisonIsReinventoriedAndRelogged(t *testing.T) {
	const id = "acme.ops.test.system.widget.repoison"
	c, bucket := poisonScopingTestComponent(t)
	var logs bytes.Buffer
	c.logger = slog.New(slog.NewTextHandler(&logs, nil))
	seedPoisonBytes(bucket, id, 1)

	_, err := queryEntity(t, c, id)
	assertIngestResetRequired(t, err)
	require.Equal(t, 1, strings.Count(logs.String(), "code="+graph.ErrorCodeGraphStateResetRequired))

	// Repair out-of-band, next read clears.
	seedValidBytes(bucket, id, 2)
	_, err = queryEntity(t, c, id)
	require.NoError(t, err)
	_, inventoried := poisonInventoryEntry(c, id)
	require.False(t, inventoried)

	// Re-poison: detected again, re-inventoried, re-logged.
	seedPoisonBytes(bucket, id, 3)
	_, err = queryEntity(t, c, id)
	assertIngestResetRequired(t, err)
	rec, inventoried := poisonInventoryEntry(c, id)
	require.True(t, inventoried, "re-poison must re-inventory")
	assert.Equal(t, uint64(3), rec.revision)
	assert.Equal(t, 2, strings.Count(logs.String(), "code="+graph.ErrorCodeGraphStateResetRequired),
		"re-poison must produce a fresh structured ERROR")
}

// TestPoisonInventoryIsObservabilityOnly pins the D2 invariant: no read or
// write path consults the inventory for a decision. A stale entry cannot
// refuse a repaired entity, and a write to an entity with a stale entry
// commits normally.
func TestPoisonInventoryIsObservabilityOnly(t *testing.T) {
	const id = "acme.ops.test.system.widget.stale"
	ctx := context.Background()

	t.Run("stale entry cannot refuse a repaired read", func(t *testing.T) {
		c, bucket := poisonScopingTestComponent(t)
		seedValidBytes(bucket, id, 3)
		c.inventoryEntityPoison(context.Background(), &graph.StateContractError{
			Reason: graph.GraphStateReasonNoncanonicalPredicate, EntityID: id,
		}, 1)

		data, err := queryEntity(t, c, id)
		require.NoError(t, err, "the read validates CURRENT bytes and serves — inventory gates nothing")
		assert.Contains(t, string(data), id)
		_, inventoried := poisonInventoryEntry(c, id)
		assert.False(t, inventoried, "the successful validating read also clears the stale entry")
	})

	t.Run("stale entry cannot block a write", func(t *testing.T) {
		c, bucket := poisonScopingTestComponent(t)
		seedValidBytes(bucket, id, 3)
		c.inventoryEntityPoison(context.Background(), &graph.StateContractError{
			Reason: graph.GraphStateReasonNoncanonicalPredicate, EntityID: id,
		}, 1)

		require.NoError(t, c.MergeEntity(ctx, &graph.EntityState{
			ID:      id,
			Triples: []message.Triple{{Subject: id, Predicate: "test.state.value", Object: "merged", Timestamp: time.Now(), Confidence: 1.0}},
		}), "writes never consult the inventory")
		_, inventoried := poisonInventoryEntry(c, id)
		assert.False(t, inventoried, "the committed write clears the stale entry (D3b)")
	})
}

// TestAggregateReadNamesEveryPoisonedEntity drives the spec scenario "batch
// fetch fails loudly and names all poisoned entities" (D5): one typed error
// naming A and C, both inventoried in the same attempt, no B-only response —
// on BOTH multi-entity read paths (batch and prefix).
func TestAggregateReadNamesEveryPoisonedEntity(t *testing.T) {
	const (
		idA = "acme.ops.test.system.widget.agg-a"
		idB = "acme.ops.test.system.widget.agg-b"
		idC = "acme.ops.test.system.widget.agg-c"
	)
	setup := func(t *testing.T) *Component {
		t.Helper()
		c, bucket := poisonScopingTestComponent(t)
		seedPoisonBytes(bucket, idA, 1)
		seedValidBytes(bucket, idB, 2)
		seedPoisonBytes(bucket, idC, 3)
		return c
	}
	assertAggregate := func(t *testing.T, c *Component, data []byte, err error) {
		t.Helper()
		require.Nil(t, data, "no response may contain B while silently omitting A or C")
		assertIngestResetRequired(t, err)
		assert.Contains(t, err.Error(), idA, "aggregate error must name entity A")
		assert.Contains(t, err.Error(), idC, "aggregate error must name entity C")
		var stateErr *graph.StateContractError
		assert.True(t, errors.As(err, &stateErr), "typed cause must survive the aggregate wrap")
		_, aInv := poisonInventoryEntry(c, idA)
		_, cInv := poisonInventoryEntry(c, idC)
		assert.True(t, aInv && cInv, "one attempt must inventory EVERY poisoned entity")
		_, bInv := poisonInventoryEntry(c, idB)
		assert.False(t, bInv, "the valid entity is never inventoried")
	}

	t.Run("batch", func(t *testing.T) {
		c := setup(t)
		data, err := c.handleQueryBatchNATS(context.Background(),
			[]byte(`{"ids":["`+idA+`","`+idB+`","`+idC+`"]}`))
		assertAggregate(t, c, data, err)
	})

	t.Run("prefix", func(t *testing.T) {
		c := setup(t)
		req, err := json.Marshal(graph.PrefixQueryRequest{Prefix: "acme.ops.test.system.widget"})
		require.NoError(t, err)
		data, qerr := c.handleQueryPrefixWithMaxPayload(context.Background(), req, 1<<20)
		assertAggregate(t, c, data, qerr)
	})
}

// TestMutationReadSeamsReturnTypedFatal proves canonical read-modify-write
// mutations classify resident poison as graph_state_reset_required rather than
// a retry-inviting transient internal error.
func TestMutationReadSeamsReturnTypedFatal(t *testing.T) {
	const id = "acme.ops.test.system.widget.seam"
	validTriple := message.Triple{Subject: id, Predicate: "test.state.value", Object: "v", Timestamp: time.Now(), Confidence: 1.0}

	assertTypedFatalReject := func(t *testing.T, data []byte, err error) {
		t.Helper()
		require.Nil(t, data)
		require.Error(t, err)
		var classified *errs.ClassifiedError
		require.ErrorAs(t, err, &classified)
		assert.Equal(t, errs.ErrorFatal, classified.Class, "resident poison must not invite a retry: %v", err)
		assert.Equal(t, graph.ErrorCodeGraphStateResetRequired, classified.Code)
	}

	t.Run("reconcile read seam", func(t *testing.T) {
		c, bucket := poisonScopingTestComponent(t)
		seedPoisonBytes(bucket, id, 1)
		req, err := json.Marshal(graph.ReconcilePredicatesRequest{
			EntityID: id, ExpectedRevision: 1, Predicates: []string{validTriple.Predicate},
			Desired: []message.Triple{validTriple},
		})
		require.NoError(t, err)
		data, herr := c.handleCanonicalReconcile(context.Background(), req)
		assertTypedFatalReject(t, data, herr)
		_, inventoried := poisonInventoryEntry(c, id)
		assert.True(t, inventoried, "the seam read must inventory the poison")
	})

	t.Run("append RMW seam", func(t *testing.T) {
		c, bucket := poisonScopingTestComponent(t)
		seedPoisonBytes(bucket, id, 1)
		req, err := json.Marshal(graph.AppendTriplesRequest{Triples: []message.Triple{validTriple}})
		require.NoError(t, err)
		data, herr := c.handleCanonicalAppend(context.Background(), req)
		require.NoError(t, herr)
		var response graph.AppendTriplesResponse
		require.NoError(t, json.Unmarshal(data, &response))
		require.Len(t, response.Results, 1)
		assert.Equal(t, graph.MutationFailed, response.Results[0].Outcome)
		require.NotNil(t, response.Results[0].Error)
		assert.Equal(t, graph.ErrorCodeGraphStateResetRequired, response.Results[0].Error.Code)
	})
}

// TestProcessIngestResidentPoisonNakThenAppliesAfterRepair drives the spec
// scenario "valid arrival survives a poison window" (D8): the resident-poison
// arrival is Nak'd (not Term'd), and a redelivery of the SAME message applies
// successfully once the entity is repaired.
func TestProcessIngestResidentPoisonNakThenAppliesAfterRepair(t *testing.T) {
	const id = "acme.ops.test.system.widget.nak-repair"
	c, bucket := poisonScopingTestComponent(t)
	c.ingestGuardMem = []*laneGuard{newLaneGuard(16)}
	seedPoisonBytes(bucket, id, 1)

	work := ingestWork{
		entity: &graph.EntityState{ID: id, Triples: []message.Triple{{
			Subject: id, Predicate: "test.state.value", Object: "survivor", Timestamp: time.Now(), Confidence: 1.0,
		}}},
		msg: &keyedIngestTestMsg{}, entityID: id, stream: "ENTITY", seq: 7,
	}

	// Poison window: Nak, never Term.
	first := work.msg.(*keyedIngestTestMsg)
	require.Error(t, c.processIngest(context.Background(), 0, work))
	assert.True(t, first.nak.Load(), "resident poison must Nak")
	assert.False(t, first.term.Load(), "resident poison must not Term (valid data would be destroyed)")

	// Operator repair: delete + (implicit) fresh state via the redelivery itself.
	require.NoError(t, c.deleteEntityAtRevision(context.Background(), id, 1))

	// Redelivery of the SAME message applies.
	redelivery := &keyedIngestTestMsg{}
	work.msg = redelivery
	require.NoError(t, c.processIngest(context.Background(), 0, work))
	assert.True(t, redelivery.ack.Load(), "post-repair redelivery must apply and ack")
	assert.False(t, redelivery.term.Load())
	data, err := queryEntity(t, c, id)
	require.NoError(t, err, "the survived arrival must be readable after repair")
	assert.Contains(t, string(data), "survivor")
}

// TestBootSweepLogCap: past bootSweepErrorLogCap individually-logged ERRORs,
// the sweep emits one count-only WARN instead of flooding the log. Not
// parallel: asserts on the component-local slog capture.
func TestBootSweepLogCap(t *testing.T) {
	total := bootSweepErrorLogCap + 25
	sweep := make(map[string]entityPoisonRecord, total)
	for i := 0; i < total; i++ {
		id := "acme.ops.test.system.widget.cap-" + string(rune('a'+i%26)) + "-" + string(rune('0'+i/26%10)) + string(rune('0'+i%10))
		sweep[id] = entityPoisonRecord{
			contractErr: &graph.StateContractError{Reason: graph.GraphStateReasonNoncanonicalPredicate, EntityID: id},
			revision:    uint64(i + 1),
		}
	}
	// Guard against fixture collisions silently shrinking the set.
	require.Len(t, sweep, total)

	c, bucket := poisonScopingTestComponent(t)
	// The poison is resident: verify-after-record must find failing bytes,
	// not an absent key (which would correctly drop as repaired-by-delete).
	for id, rec := range sweep {
		seedPoisonBytes(bucket, id, rec.revision)
	}
	var logs bytes.Buffer
	c.logger = slog.New(slog.NewTextHandler(&logs, nil))

	c.recordBootSweepPoison(context.Background(), sweep)

	assert.Equal(t, int64(total), c.entityPoisonSize.Load(), "every entry is inventoried regardless of the log cap")
	assert.Equal(t, bootSweepErrorLogCap, strings.Count(logs.String(), "code="+graph.ErrorCodeGraphStateResetRequired),
		"individual ERRORs are capped")
	assert.Equal(t, 1, strings.Count(logs.String(), "log cap"), "one count-only WARN summarizes the rest")
}

// TestDebugStatusEnumeratesFullInventory pins the DebugStatusProvider surface
// (D6): full enumeration, sorted, JSON-serializable.
func TestDebugStatusEnumeratesFullInventory(t *testing.T) {
	c, bucket := poisonScopingTestComponent(t)
	ids := []string{
		"acme.ops.test.system.widget.dbg-b",
		"acme.ops.test.system.widget.dbg-a",
		"acme.ops.test.system.widget.dbg-c",
	}
	for i, id := range ids {
		seedPoisonBytes(bucket, id, uint64(i+1))
		c.inventoryEntityPoison(context.Background(), &graph.StateContractError{
			Reason: graph.GraphStateReasonNoncanonicalPredicate, EntityID: id,
		}, uint64(i+1))
	}

	status, ok := c.DebugStatus().(entityPoisonDebugStatus)
	require.True(t, ok)
	require.Equal(t, 3, status.PoisonedEntityCount)
	require.Len(t, status.PoisonedEntities, 3)
	assert.Equal(t, "acme.ops.test.system.widget.dbg-a", status.PoisonedEntities[0].EntityID, "enumeration is sorted")
	assert.Equal(t, "acme.ops.test.system.widget.dbg-c", status.PoisonedEntities[2].EntityID)
	for _, entry := range status.PoisonedEntities {
		assert.Equal(t, string(graph.GraphStateReasonNoncanonicalPredicate), entry.Reason)
		assert.NotZero(t, entry.Revision)
		assert.Contains(t, entry.Error, graph.ErrorCodeGraphStateResetRequired)
	}

	// Round-trip: the debug shape must be JSON-serializable (wire contract of
	// the debug surface) — and the typed error itself never rides it raw.
	raw, err := json.Marshal(c.DebugStatus())
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"poisoned_entity_count":3`)
	assert.Contains(t, string(raw), `"entity_id":"acme.ops.test.system.widget.dbg-a"`)
}

// TestVerifyAfterRecordDropsStaleRecordAfterCompletedRepair pins the
// verify-after-record close of the last record/clear interleaving (reviewer
// MEDIUM-1): a repair that completed ENTIRELY between a detection's read and
// its inventory record found no entry to clear, so the fresh record must be
// verified against current bytes and dropped when they prove repair.
func TestVerifyAfterRecordDropsStaleRecordAfterCompletedRepair(t *testing.T) {
	c, bucket := poisonScopingTestComponent(t)
	ctx := context.Background()

	// Repair-by-commit won the race: current bytes are valid at a newer
	// revision when the stale detection records.
	idCommit := "acme.ops.test.system.widget.verify-commit"
	seedValidBytes(bucket, idCommit, 7)
	c.inventoryEntityPoison(ctx, &graph.StateContractError{
		Reason: graph.GraphStateReasonUnreadableEntity, EntityID: idCommit,
	}, 3)
	assert.Equal(t, int64(0), c.entityPoisonSize.Load(),
		"a record made after a completed repair commit must not survive verification")

	// Repair-by-delete won the race: the key is absent at record time.
	idDelete := "acme.ops.test.system.widget.verify-delete"
	c.inventoryEntityPoison(ctx, &graph.StateContractError{
		Reason: graph.GraphStateReasonUnreadableEntity, EntityID: idDelete,
	}, 2)
	assert.Equal(t, int64(0), c.entityPoisonSize.Load(),
		"a record made after a completed repair delete must not survive verification")

	// No repair happened: the record survives verification.
	idKeep := "acme.ops.test.system.widget.verify-keep"
	seedPoisonBytes(bucket, idKeep, 4)
	c.inventoryEntityPoison(ctx, &graph.StateContractError{
		Reason: graph.GraphStateReasonNoncanonicalPredicate, EntityID: idKeep,
	}, 4)
	assert.Equal(t, int64(1), c.entityPoisonSize.Load(),
		"resident poison keeps its record through verification")
}

// TestDetectionInvalidatesCachedEntry pins the spec scenario "detection
// invalidates the cached entry": a warm query-cache entry for an entity whose
// stored bytes were poisoned out-of-band must not outlive detection — the
// inventory record deletes it, bounding the stale-serve window to detection
// latency instead of the cache TTL (design D2).
func TestDetectionInvalidatesCachedEntry(t *testing.T) {
	c, bucket := poisonScopingTestComponent(t)
	ctx := context.Background()

	entityCache, err := cache.NewFromConfig[graph.EntityState](ctx, cache.Config{
		Enabled:         true,
		Strategy:        cache.StrategyHybrid,
		MaxSize:         16,
		TTL:             time.Minute,
		CleanupInterval: time.Minute,
	})
	require.NoError(t, err)
	c.entityCache = entityCache

	id := "acme.ops.test.system.widget.cache-a"
	_, err = entityCache.Set(id, graph.EntityState{ID: id, Version: 1})
	require.NoError(t, err)
	_, hit := entityCache.Get(id)
	require.True(t, hit, "fixture: the entry must be cache-warm before poisoning")

	// Out-of-band poison lands under the cached key; the single-read lane
	// detects it on the stored bytes.
	seedPoisonBytes(bucket, id, 2)
	require.Error(t, c.validateEntityQueryValue(ctx, id, guardTestPoisonBytes(id), 2))

	_, hit = entityCache.Get(id)
	assert.False(t, hit, "the cached response must not outlive poison detection")
}

// TestSuffixResolutionServesIDWhileReadRefuses pins the spec scenario "suffix
// resolution does not serve entity state": resolving a poisoned entity's ID
// needs no decode and may succeed, while the follow-up state read fails with
// the typed classification.
func TestSuffixResolutionServesIDWhileReadRefuses(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()

	id := "c360.logistics.environmental.sensor.temperature.poison-sensor-001"
	require.NoError(t, comp.CreateEntity(ctx, &graph.EntityState{
		ID: id, Triples: []message.Triple{}, Version: 1, UpdatedAt: time.Now(),
	}))
	// Poison the stored bytes out-of-band; the suffix index entry remains.
	_, err := comp.entityBucket.Put(ctx, id, guardTestPoisonBytes(id))
	require.NoError(t, err)

	suffixReq, err := json.Marshal(map[string]string{"suffix": "poison-sensor-001"})
	require.NoError(t, err)
	suffixResp, err := comp.handleQuerySuffixNATS(ctx, suffixReq)
	require.NoError(t, err, "suffix resolution decodes no entity state and serves the ID")
	var resolved struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(suffixResp, &resolved))
	require.Equal(t, id, resolved.ID)

	entityReq, err := json.Marshal(map[string]string{"id": id})
	require.NoError(t, err)
	_, err = comp.handleQueryEntityNATS(ctx, entityReq)
	require.Error(t, err, "the follow-up state read must refuse")
	assert.Contains(t, err.Error(), graph.ErrorCodeGraphStateResetRequired)
}
