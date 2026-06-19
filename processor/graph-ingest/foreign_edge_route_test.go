package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/ownership"
)

// routeTestComp builds a graph-ingest component over a mock KV bucket and returns
// BOTH (unlike createTestComponentWithMockKV) so a test can pre-seed entities and
// inject Get errors — the inputs the 4c-pre-2 routing policy branches on.
func routeTestComp(t *testing.T) (*Component, *mockKVBucket) {
	t.Helper()
	mockBucket := newMockKVBucket()
	natsClient, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)
	configJSON, err := json.Marshal(DefaultConfig())
	require.NoError(t, err)
	comp, err := CreateGraphIngest(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)
	c := comp.(*Component)
	c.entityBucket = natsClient.NewKVStore(mockBucket)
	return c, mockBucket
}

func countPredicate(es *graph.EntityState, predicate string) int {
	n := 0
	for _, tr := range es.Triples {
		if tr.Predicate == predicate {
			n++
		}
	}
	return n
}

var routeMT = message.Type{Domain: "test", Category: "fixture", Version: "v1"}

const routeEdgePred = "child.isHostedBy"

func routeEdge() message.Triple {
	return message.Triple{Subject: flChildID, Predicate: routeEdgePred, Object: flParentID}
}

// EdgeNoBirthStub + absent target: the seam materialises the envelope-bearing
// referential stub (the only thing that ever births a sensorml-style child) and
// then routes the edge onto it EXACTLY ONCE — not duplicated by stub+append.
func TestRouteForeignEdges_NoBirthStub_AbsentTarget_StubAndAppendOnce(t *testing.T) {
	comp, _ := routeTestComp(t)
	comp.claimReader = &fakeClassifier{modes: map[string]ownership.EdgeMode{routeEdgePred: ownership.EdgeNoBirthStub}}

	comp.routeForeignEdges(context.Background(), flParentID, routeMT, []message.Triple{routeEdge()})

	child := storedEntity(t, comp, flChildID)
	assert.True(t, hasPredicate(child, predStubMarker), "no-birth-stub target must be materialised as an envelope-bearing stub")
	assert.Equal(t, 1, countPredicate(child, routeEdgePred), "the routed edge must appear EXACTLY ONCE (stub-materialise must not double-append)")
}

// EdgeStrict + absent target: the edge is DROPPED (target must pre-exist by
// causal ordering) — no stub, no edge — and metered on foreign_edge_dropped_total
// with reason=strict_absent_target, distinct from the unclaimed hatch counter.
func TestRouteForeignEdges_Strict_AbsentTarget_DroppedWithMetric(t *testing.T) {
	comp, _ := routeTestComp(t)
	comp.claimReader = &fakeClassifier{modes: map[string]ownership.EdgeMode{routeEdgePred: ownership.EdgeStrict}}

	label := routeMT.Key()
	beforeDropped := testutil.ToFloat64(comp.foreignEdgeDropped.WithLabelValues(label, routeEdgePred, dropReasonStrictAbsent))
	beforeUnclaimed := testutil.ToFloat64(comp.foreignEdgeUnclaimed.WithLabelValues(label, routeEdgePred))

	comp.routeForeignEdges(context.Background(), flParentID, routeMT, []message.Triple{routeEdge()})

	_, err := comp.entityBucket.Get(context.Background(), flChildID)
	assert.True(t, natsclient.IsKVNotFoundError(err), "a Strict drop must NOT vivify or stub the absent target")
	assert.InDelta(t, beforeDropped+1, testutil.ToFloat64(comp.foreignEdgeDropped.WithLabelValues(label, routeEdgePred, dropReasonStrictAbsent)), 0.0001,
		"a Strict drop must increment foreign_edge_dropped_total{reason=strict_absent_target}")
	assert.InDelta(t, beforeUnclaimed, testutil.ToFloat64(comp.foreignEdgeUnclaimed.WithLabelValues(label, routeEdgePred)), 0.0001,
		"a Strict drop must NOT touch the flip-gate hatch counter (foreign_edge_unclaimed_total)")
}

// A present target appends regardless of mode — even EdgeStrict, which would
// drop an ABSENT target, routes onto a target that already exists.
func TestRouteForeignEdges_PresentTarget_AppendsRegardlessOfMode(t *testing.T) {
	comp, _ := routeTestComp(t)
	comp.claimReader = &fakeClassifier{modes: map[string]ownership.EdgeMode{routeEdgePred: ownership.EdgeStrict}}

	// Pre-seed the target so it exists before routing.
	seed, _ := json.Marshal(&graph.EntityState{ID: flChildID, Version: 1})
	_, err := comp.entityBucket.Put(context.Background(), flChildID, seed)
	require.NoError(t, err)

	label := routeMT.Key()
	beforeDropped := testutil.ToFloat64(comp.foreignEdgeDropped.WithLabelValues(label, routeEdgePred, dropReasonStrictAbsent))

	comp.routeForeignEdges(context.Background(), flParentID, routeMT, []message.Triple{routeEdge()})

	child := storedEntity(t, comp, flChildID)
	assert.True(t, hasPredicate(child, routeEdgePred), "an edge onto a PRESENT target must be appended regardless of mode")
	assert.InDelta(t, beforeDropped, testutil.ToFloat64(comp.foreignEdgeDropped.WithLabelValues(label, routeEdgePred, dropReasonStrictAbsent)), 0.0001,
		"a present-target append must NOT increment the drop counter")
}

// UNCLAIMED + absent target: the deprecated-on-arrival hatch — the edge stays
// routed (so the flip-gate hatch counter can still drain to zero) and is NOT
// metered on the drop counter (it is the unclaimed lane, metered upstream).
// ADR-055: after the must-exist flip the routing decision is unchanged (edge
// goes into toAppend), but AddTriples now rejects absent targets → entity
// remains absent. The primary write is unaffected (warn-not-fails contract).
func TestRouteForeignEdges_Unclaimed_AbsentTarget_RouteWithWarn(t *testing.T) {
	comp, _ := routeTestComp(t)
	comp.claimReader = &fakeClassifier{} // no modes configured → ForeignEdgeMode reports unclaimed

	label := routeMT.Key()
	beforeDropped := testutil.ToFloat64(comp.foreignEdgeDropped.WithLabelValues(label, routeEdgePred, dropReasonStrictAbsent))

	comp.routeForeignEdges(context.Background(), flParentID, routeMT, []message.Triple{routeEdge()})

	// ADR-055: the unclaimed routing decision still routes (not dropped), but
	// AddTriples rejects the absent target — entity must NOT exist post-flip.
	_, err := comp.entityBucket.Get(context.Background(), flChildID)
	assert.True(t, natsclient.IsKVNotFoundError(err),
		"ADR-055: unclaimed absent target must NOT be auto-vivified; AddTriples fails (warn-not-fails)")
	assert.InDelta(t, beforeDropped, testutil.ToFloat64(comp.foreignEdgeDropped.WithLabelValues(label, routeEdgePred, dropReasonStrictAbsent)), 0.0001,
		"the unclaimed hatch must NOT increment the drop counter")
}

// A mode-lookup read error inside routeForeignEdges fails OPEN: the edge is
// routed-with-warn (never blocked, never Strict-dropped on a transient read
// blip) — the route-time mirror of the foreignTargetExists fail-open.
// ADR-055: after the must-exist flip the fail-open routing decision is unchanged
// (edge goes into toAppend), but AddTriples rejects the absent target →
// entity remains absent. The primary write is unaffected (warn-not-fails).
func TestRouteForeignEdges_ModeLookupError_FailsOpen(t *testing.T) {
	comp, _ := routeTestComp(t)
	comp.claimReader = &fakeClassifier{err: assertErr} // ForeignEdgeMode returns an error

	label := routeMT.Key()
	beforeDropped := testutil.ToFloat64(comp.foreignEdgeDropped.WithLabelValues(label, routeEdgePred, dropReasonStrictAbsent))

	comp.routeForeignEdges(context.Background(), flParentID, routeMT, []message.Triple{routeEdge()})

	// ADR-055: the fail-open routing decision still routes (not dropped), but
	// AddTriples rejects the absent target — entity must NOT exist post-flip.
	_, err := comp.entityBucket.Get(context.Background(), flChildID)
	assert.True(t, natsclient.IsKVNotFoundError(err),
		"ADR-055: fail-open absent target must NOT be auto-vivified; AddTriples fails (warn-not-fails)")
	assert.InDelta(t, beforeDropped, testutil.ToFloat64(comp.foreignEdgeDropped.WithLabelValues(label, routeEdgePred, dropReasonStrictAbsent)), 0.0001,
		"a fail-open route must NOT increment the drop counter")
}

// An UNKNOWN/future EdgeMode + absent target takes the forward-compat default:
// route-with-warn (hatch), NEVER a silent drop. Locks the contract that adding a
// new EdgeMode cannot silently start dropping edges before the seam handles it.
// ADR-055: after the must-exist flip the forward-compat routing decision is
// unchanged (edge goes into toAppend), but AddTriples rejects the absent target
// → entity remains absent. The primary write is unaffected (warn-not-fails).
func TestRouteForeignEdges_UnknownMode_AbsentTarget_RouteWithWarn(t *testing.T) {
	comp, _ := routeTestComp(t)
	comp.claimReader = &fakeClassifier{modes: map[string]ownership.EdgeMode{routeEdgePred: ownership.EdgeMode("future-unhandled")}}

	label := routeMT.Key()
	beforeDropped := testutil.ToFloat64(comp.foreignEdgeDropped.WithLabelValues(label, routeEdgePred, dropReasonStrictAbsent))

	comp.routeForeignEdges(context.Background(), flParentID, routeMT, []message.Triple{routeEdge()})

	// ADR-055: the unknown-mode routing decision still routes (not dropped), but
	// AddTriples rejects the absent target — entity must NOT exist post-flip.
	_, err := comp.entityBucket.Get(context.Background(), flChildID)
	assert.True(t, natsclient.IsKVNotFoundError(err),
		"ADR-055: unknown-mode absent target must NOT be auto-vivified; AddTriples fails (warn-not-fails)")
	assert.InDelta(t, beforeDropped, testutil.ToFloat64(comp.foreignEdgeDropped.WithLabelValues(label, routeEdgePred, dropReasonStrictAbsent)), 0.0001,
		"an unknown-mode route must NOT increment the drop counter")
}

// foreignTargetExists fails toward the LESS destructive outcome on a transient
// read error: it reports "exists" so the edge is APPENDED, never Strict-dropped
// on a read blip (which would lose a legitimately-present edge).
func TestForeignTargetExists_ReadBlip_FailsTowardExists(t *testing.T) {
	comp, mock := routeTestComp(t)
	ctx := context.Background()

	// notfound → false
	assert.False(t, comp.foreignTargetExists(ctx, flChildID), "absent target reports not-exists")

	// present → true
	seed, _ := json.Marshal(&graph.EntityState{ID: flChildID, Version: 1})
	_, err := comp.entityBucket.Put(ctx, flChildID, seed)
	require.NoError(t, err)
	assert.True(t, comp.foreignTargetExists(ctx, flChildID), "present target reports exists")

	// transient (non-notfound) read error → true (fail-less-destructive)
	mock.getFunc = func(_ context.Context, _ string) (jetstream.KeyValueEntry, error) {
		return nil, errors.New("transient read blip")
	}
	assert.True(t, comp.foreignTargetExists(ctx, flChildID), "a transient read error must fail toward exists (append), not drop")
}
