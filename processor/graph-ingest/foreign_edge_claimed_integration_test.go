//go:build integration

package graphingest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/ownership"
	"github.com/c360studio/semstreams/pkg/projection"
)

// bindClaimAndBootIngest spins up a real NATS, creates OWNER_CLAIMS, binds the
// given foreign-edge contract under ownerID, then boots a graph-ingest component
// whose Start() SELF-WIRES the real ClaimReader against the bound claim — the
// production wire under test, not a hand-set field. Returns the component + ctx.
func bindClaimAndBootIngest(t *testing.T, ownerID string, contract projection.Contract) (*Component, context.Context) {
	t.Helper()
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{{Name: "ENTITY", Subjects: []string{"entity.>"}}}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	reg, err := ownership.EnsureBuckets(ctx, testClient.Client, nil, nil)
	require.NoError(t, err, "EnsureBuckets (OWNER_CLAIMS)")
	require.NoError(t, projection.Bind(ctx, reg, ownerID, contract), "Bind foreign-edge contract")

	configJSON, err := json.Marshal(DefaultConfig())
	require.NoError(t, err)
	comp, err := CreateGraphIngest(configJSON, component.Dependencies{NATSClient: testClient.Client})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })
	require.NotNil(t, c.claimReader, "Start must self-wire the claim reader once OWNER_CLAIMS exists")
	return c, ctx
}

// TestIntegration_SharedSeam_ClaimedForeignEdge_RoutesNoBirthStub proves the full
// ADR-056 Decision-4 CLAIMED foreign-edge chain end-to-end through the production
// wire: a projection.Contract declaring a NoBirthStub ForeignEdge is derived to a
// ForeignEdgeClaim and Bound into OWNER_CLAIMS; graph-ingest's Start() self-wires
// a real ClaimReader against that bucket; a create_with_triples carrying a
// foreign-subject edge from the contract's producer MessageType is then
// classified as CLAIMED (not metered on foreign_edge_unclaimed_total) and routed
// via the NoBirthStub lane (the target is materialised as an envelope-bearing
// referential stub, then the edge is appended) — NOT dropped.
//
// 4c-pre-2 unit-tested routeForeignEdges' mode branch with a fake classifier;
// 4c-pre-3 e2e-proved the UNCLAIMED referential-stub lane. This proves the
// CLAIMED lane through the real Contract→Bind→ClaimReader→routeForeignEdges wire
// — the cs-api SensorML-hierarchy path, before the must-exist flip makes it
// load-bearing. The contract here is the REFERENCE SHAPE cs-api adopts (it
// currently stamps an empty MessageType in ingestTriples, so its real adoption is
// either a Producer-empty claim or stamping a System type — see the Producer-empty
// test below).
func TestIntegration_SharedSeam_ClaimedForeignEdge_RoutesNoBirthStub(t *testing.T) {
	const (
		producerType = "c360.csapi.system.v1"
		isHostedBy   = "sensorml.component.isHostedBy"
		systemID     = "c360.csapi.facility.gateway.system.001"
		componentID  = "c360.csapi.facility.gateway.component.001"
	)
	producerMT := message.Type{Domain: "c360", Category: "csapi.system", Version: "v1"}
	require.Equal(t, producerType, producerMT.Key(), "producer Key() must match the contract MessageType")

	c, ctx := bindClaimAndBootIngest(t, "cs-api-systems", projection.Contract{
		Name:          "csapi.system.hierarchy",
		MessageType:   producerType, // stamped as the ForeignEdgeClaim Producer
		EntityPattern: "*.*.*.*.system.*",
		ForeignEdges:  []projection.ForeignEdge{{Predicate: isHostedBy, Mode: ownership.EdgeNoBirthStub}},
	})

	unclaimedBefore := testutil.ToFloat64(c.foreignEdgeUnclaimed.WithLabelValues(producerType, isHostedBy))
	droppedStrictBefore := testutil.ToFloat64(c.foreignEdgeDropped.WithLabelValues(producerType, isHostedBy, dropReasonStrictAbsent))
	droppedDeferredBefore := testutil.ToFloat64(c.foreignEdgeDropped.WithLabelValues(producerType, isHostedBy, dropReasonConditionalDeferred))

	// Drive create_with_triples: a System (the contract's producer type) whose
	// triples include the foreign-subject isHostedBy edge onto an absent child.
	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: systemID, MessageType: producerMT},
		Triples: []message.Triple{
			{Subject: systemID, Predicate: "system.label", Object: "Gateway", Timestamp: time.Now(), Confidence: 1}, // own
			{Subject: componentID, Predicate: isHostedBy, Object: systemID, Timestamp: time.Now(), Confidence: 1},   // foreign
		},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	respData, err := c.handleEntityCreateWithTriples(ctx, data)
	require.NoError(t, err)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))
	require.True(t, resp.Success, "create_with_triples should succeed: %s", resp.Error)

	// The foreign edge was CLAIMED → NOT metered on the unclaimed-hatch counter.
	assert.InDelta(t, unclaimedBefore, testutil.ToFloat64(c.foreignEdgeUnclaimed.WithLabelValues(producerType, isHostedBy)), 0.0001,
		"a CLAIMED foreign edge must NOT increment foreign_edge_unclaimed_total (the hatch-empty flip-gate signal)")

	// NoBirthStub routing materialised the child as an envelope-bearing stub AND
	// appended the edge — distinct from the unclaimed auto-vivify (child + edge,
	// but NO stub marker).
	child := storedEntity(t, c, componentID)
	assert.True(t, hasPredicate(child, predStubMarker), "NoBirthStub target must carry the core.identity.stub marker")
	assert.True(t, hasPredicate(child, isHostedBy), "the routed foreign edge must land on the child")

	// A claimed NoBirthStub edge is never dropped — under EITHER drop reason.
	assert.InDelta(t, droppedStrictBefore, testutil.ToFloat64(c.foreignEdgeDropped.WithLabelValues(producerType, isHostedBy, dropReasonStrictAbsent)), 0.0001,
		"a claimed NoBirthStub edge must NOT increment foreign_edge_dropped_total{reason=strict_absent_target}")
	assert.InDelta(t, droppedDeferredBefore, testutil.ToFloat64(c.foreignEdgeDropped.WithLabelValues(producerType, isHostedBy, dropReasonConditionalDeferred)), 0.0001,
		"a claimed NoBirthStub edge must NOT increment foreign_edge_dropped_total{reason=conditional_deferred}")
}

// TestIntegration_SharedSeam_ClaimedForeignEdge_StrictDropsAbsentTarget proves the
// CLAIMED EdgeStrict lane through the real wire: a Strict foreign edge whose target
// is absent is LOUD-DROPPED — metered on foreign_edge_dropped_total, NOT routed,
// the target NOT materialised — distinct from NoBirthStub (which materialises a
// stub). Strict is for edges whose target must pre-exist by causal ordering.
func TestIntegration_SharedSeam_ClaimedForeignEdge_StrictDropsAbsentTarget(t *testing.T) {
	const (
		producerType = "c360.csapi.strict.v1"
		strictEdge   = "test.strict.hosted_by"
		systemID     = "c360.csapi.strict.gateway.system.001"
		componentID  = "c360.csapi.strict.gateway.component.001"
	)
	producerMT := message.Type{Domain: "c360", Category: "csapi.strict", Version: "v1"}

	c, ctx := bindClaimAndBootIngest(t, "cs-api-strict", projection.Contract{
		Name:          "csapi.strict",
		MessageType:   producerType,
		EntityPattern: "*.*.*.*.system.*",
		ForeignEdges:  []projection.ForeignEdge{{Predicate: strictEdge, Mode: ownership.EdgeStrict}},
	})

	unclaimedBefore := testutil.ToFloat64(c.foreignEdgeUnclaimed.WithLabelValues(producerType, strictEdge))
	droppedBefore := testutil.ToFloat64(c.foreignEdgeDropped.WithLabelValues(producerType, strictEdge, dropReasonStrictAbsent))

	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: systemID, MessageType: producerMT},
		Triples: []message.Triple{
			{Subject: systemID, Predicate: "system.label", Object: "Gateway", Timestamp: time.Now(), Confidence: 1}, // own
			{Subject: componentID, Predicate: strictEdge, Object: systemID, Timestamp: time.Now(), Confidence: 1},   // foreign, Strict, absent target
		},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	respData, err := c.handleEntityCreateWithTriples(ctx, data)
	require.NoError(t, err)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))
	require.True(t, resp.Success, "primary create must succeed even when the foreign edge is dropped: %s", resp.Error)

	// Claimed Strict + absent target → LOUD DROP: metered, NOT routed, NOT materialised.
	assert.InDelta(t, droppedBefore+1, testutil.ToFloat64(c.foreignEdgeDropped.WithLabelValues(producerType, strictEdge, dropReasonStrictAbsent)), 0.0001,
		"a claimed EdgeStrict edge to an absent target must increment foreign_edge_dropped_total{reason=strict_absent_target}")
	assert.InDelta(t, unclaimedBefore, testutil.ToFloat64(c.foreignEdgeUnclaimed.WithLabelValues(producerType, strictEdge)), 0.0001,
		"a claimed Strict drop must NOT touch the unclaimed-hatch counter")
	_, getErr := c.entityBucket.Get(ctx, componentID)
	assert.True(t, natsclient.IsKVNotFoundError(getErr), "EdgeStrict must NOT materialise or vivify the absent target")
}

// TestIntegration_SharedSeam_ClaimedForeignEdge_UpdateLaneMaterialisesStub proves
// NoBirthStub routing on the update_with_triples lane — where it is MOST
// load-bearing. Unlike create_with_triples / fact-arrival, update_with_triples does
// NOT run the upstream ensureRelationshipTargetsExist stub walk, so routeForeignEdges'
// own NoBirthStub materialisation is the ONLY thing that births the foreign-edge
// target. Post-flip (auto-vivify removed) this claimed routing is what keeps the
// update-lane foreign edge from being dropped.
func TestIntegration_SharedSeam_ClaimedForeignEdge_UpdateLaneMaterialisesStub(t *testing.T) {
	const (
		producerType = "c360.csapi.updatelane.v1"
		isHostedBy   = "sensorml.component.isHostedBy"
		systemID     = "c360.csapi.updatelane.gateway.system.001"
		componentID  = "c360.csapi.updatelane.gateway.component.001"
	)
	producerMT := message.Type{Domain: "c360", Category: "csapi.updatelane", Version: "v1"}

	c, ctx := bindClaimAndBootIngest(t, "cs-api-updatelane", projection.Contract{
		Name:          "csapi.updatelane",
		MessageType:   producerType,
		EntityPattern: "*.*.*.*.system.*",
		ForeignEdges:  []projection.ForeignEdge{{Predicate: isHostedBy, Mode: ownership.EdgeNoBirthStub}},
	})

	// Pre-create the parent so update_with_triples has an existing entity (must-exist on the primary).
	require.NoError(t, c.CreateEntity(ctx, &graph.EntityState{
		ID: systemID, MessageType: producerMT, Version: 1, UpdatedAt: time.Now(),
		Triples: []message.Triple{{Subject: systemID, Predicate: "system.label", Object: "Gateway", Timestamp: time.Now(), Confidence: 1}},
	}))

	unclaimedBefore := testutil.ToFloat64(c.foreignEdgeUnclaimed.WithLabelValues(producerType, isHostedBy))

	req := graph.UpdateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: systemID, MessageType: producerMT},
		AddTriples: []message.Triple{
			{Subject: systemID, Predicate: "system.note", Object: "n", Timestamp: time.Now(), Confidence: 1},      // own
			{Subject: componentID, Predicate: isHostedBy, Object: systemID, Timestamp: time.Now(), Confidence: 1}, // foreign
		},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	respData, err := c.handleEntityUpdateWithTriples(ctx, data)
	require.NoError(t, err)
	var resp graph.UpdateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))
	require.True(t, resp.Success, "update_with_triples must succeed: %s", resp.Error)

	assert.InDelta(t, unclaimedBefore, testutil.ToFloat64(c.foreignEdgeUnclaimed.WithLabelValues(producerType, isHostedBy)), 0.0001,
		"a CLAIMED foreign edge on the update lane must NOT increment the unclaimed counter")
	// On the update lane the NoBirthStub routing is the ONLY materialiser — the stub
	// marker proves routeForeignEdges (not an upstream walk) births the target.
	child := storedEntity(t, c, componentID)
	assert.True(t, hasPredicate(child, predStubMarker), "update-lane NoBirthStub target must carry the core.identity.stub marker")
	assert.True(t, hasPredicate(child, isHostedBy), "the routed foreign edge must land on the child")
}

// TestIntegration_SharedSeam_ProducerEmptyClaim_RoutesAnyProducer proves the
// Producer-empty ("any producer") claim path at the seam — the shape cs-api uses
// TODAY, because its ingestTriples stamps an EMPTY MessageType. An empty-MessageType
// producer's foreign edge (lookup key "..", metric label "_invalid") is matched by
// a Producer-empty ForeignEdgeClaim and routed via NoBirthStub, NOT metered unclaimed.
func TestIntegration_SharedSeam_ProducerEmptyClaim_RoutesAnyProducer(t *testing.T) {
	const (
		anyEdge      = "test.anyproducer.hosted_by"
		systemID     = "c360.csapi.anyprod.gateway.system.001"
		componentID  = "c360.csapi.anyprod.gateway.component.001"
		invalidLabel = "_invalid" // the bounded metric label for an invalid/zero MessageType
	)

	c, ctx := bindClaimAndBootIngest(t, "cs-api-anyproducer", projection.Contract{
		Name:          "csapi.anyproducer",
		MessageType:   "", // Producer-empty → matches any producer at the seam (transitional shape)
		EntityPattern: "*.*.*.*.system.*",
		ForeignEdges:  []projection.ForeignEdge{{Predicate: anyEdge, Mode: ownership.EdgeNoBirthStub}},
	})

	unclaimedBefore := testutil.ToFloat64(c.foreignEdgeUnclaimed.WithLabelValues(invalidLabel, anyEdge))

	// The producer stamps an EMPTY MessageType (cs-api's real shape today).
	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: systemID}, // empty MessageType, like cs-api ingestTriples
		Triples: []message.Triple{
			{Subject: systemID, Predicate: "system.label", Object: "Gateway", Timestamp: time.Now(), Confidence: 1}, // own
			{Subject: componentID, Predicate: anyEdge, Object: systemID, Timestamp: time.Now(), Confidence: 1},      // foreign
		},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	respData, err := c.handleEntityCreateWithTriples(ctx, data)
	require.NoError(t, err)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))
	require.True(t, resp.Success, "create must succeed: %s", resp.Error)

	// The any-producer claim matched at the seam → NOT metered unclaimed (under the _invalid label).
	assert.InDelta(t, unclaimedBefore, testutil.ToFloat64(c.foreignEdgeUnclaimed.WithLabelValues(invalidLabel, anyEdge)), 0.0001,
		"a Producer-empty claim must match an empty-MessageType producer at the seam (NOT metered unclaimed)")
	child := storedEntity(t, c, componentID)
	assert.True(t, hasPredicate(child, predStubMarker), "Producer-empty NoBirthStub target must carry the stub marker")
	assert.True(t, hasPredicate(child, anyEdge), "the routed foreign edge must land on the child")
}
