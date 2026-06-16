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
// either a Producer-empty claim or stamping a System type — a semconnect detail).
func TestIntegration_SharedSeam_ClaimedForeignEdge_RoutesNoBirthStub(t *testing.T) {
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{{Name: "ENTITY", Subjects: []string{"entity.>"}}}
	testClient := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	// The producer + foreign-edge predicate the contract claims. isHostedBy is the
	// genuine sensorml child→system foreign edge; NoBirthStub because the child is
	// never independently published (the parent's Graphable emits it).
	const (
		producerType  = "c360.csapi.system.v1"
		isHostedBy    = "sensorml.component.isHostedBy"
		systemID      = "c360.csapi.facility.gateway.system.001"
		componentID   = "c360.csapi.facility.gateway.component.001"
		bindOwnerID   = "cs-api-systems"
		entityPattern = "*.*.*.*.system.*"
	)
	producerMT := message.Type{Domain: "c360", Category: "csapi.system", Version: "v1"}
	require.Equal(t, producerType, producerMT.Key(), "producer Key() must match the contract MessageType")

	// 1. Eagerly create OWNER_CLAIMS + bind the reference contract BEFORE the
	//    component boots, so Start()'s NewClaimReader sees the bound claim.
	reg, err := ownership.EnsureBuckets(ctx, testClient.Client, nil, nil)
	require.NoError(t, err, "EnsureBuckets (OWNER_CLAIMS)")

	contract := projection.Contract{
		Name:          "csapi.system.hierarchy",
		MessageType:   producerType, // stamped as the ForeignEdgeClaim Producer
		EntityPattern: entityPattern,
		ForeignEdges: []projection.ForeignEdge{
			{Predicate: isHostedBy, Mode: ownership.EdgeNoBirthStub},
		},
	}
	require.NoError(t, projection.Bind(ctx, reg, bindOwnerID, contract), "Bind reference foreign-edge contract")

	// 2. Boot graph-ingest — Start() self-wires the real ClaimReader (production wire).
	configJSON, err := json.Marshal(DefaultConfig())
	require.NoError(t, err)
	comp, err := CreateGraphIngest(configJSON, component.Dependencies{NATSClient: testClient.Client})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	defer func() { _ = c.Stop(5 * time.Second) }()
	require.NotNil(t, c.claimReader, "Start must self-wire the claim reader once OWNER_CLAIMS exists")

	unclaimedBefore := testutil.ToFloat64(c.foreignEdgeUnclaimed.WithLabelValues(producerType, isHostedBy))
	droppedStrictBefore := testutil.ToFloat64(c.foreignEdgeDropped.WithLabelValues(producerType, isHostedBy, dropReasonStrictAbsent))
	droppedDeferredBefore := testutil.ToFloat64(c.foreignEdgeDropped.WithLabelValues(producerType, isHostedBy, dropReasonConditionalDeferred))

	// 3. Drive create_with_triples: a System (the contract's producer type) whose
	//    triples include the foreign-subject isHostedBy edge onto an absent child.
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

	// 4a. The foreign edge was CLAIMED → NOT metered on the unclaimed-hatch counter.
	assert.InDelta(t, unclaimedBefore, testutil.ToFloat64(c.foreignEdgeUnclaimed.WithLabelValues(producerType, isHostedBy)), 0.0001,
		"a CLAIMED foreign edge must NOT increment foreign_edge_unclaimed_total (the hatch-empty flip-gate signal)")

	// 4b. NoBirthStub routing materialised the child as an envelope-bearing stub
	//     AND appended the edge — distinct from the unclaimed auto-vivify (which
	//     would create the child with the edge but NO stub marker).
	entry, getErr := c.entityBucket.Get(ctx, componentID)
	require.NoError(t, getErr, "NoBirthStub must materialise the foreign-edge target")
	var child graph.EntityState
	require.NoError(t, json.Unmarshal(entry.Value, &child))
	assert.True(t, hasPredicate(&child, predStubMarker), "NoBirthStub target must carry the core.identity.stub marker")
	assert.True(t, hasPredicate(&child, isHostedBy), "the routed foreign edge must land on the child")

	// 4c. A claimed NoBirthStub edge is never dropped — under EITHER drop reason.
	assert.InDelta(t, droppedStrictBefore, testutil.ToFloat64(c.foreignEdgeDropped.WithLabelValues(producerType, isHostedBy, dropReasonStrictAbsent)), 0.0001,
		"a claimed NoBirthStub edge must NOT increment foreign_edge_dropped_total{reason=strict_absent_target}")
	assert.InDelta(t, droppedDeferredBefore, testutil.ToFloat64(c.foreignEdgeDropped.WithLabelValues(producerType, isHostedBy, dropReasonConditionalDeferred)), 0.0001,
		"a claimed NoBirthStub edge must NOT increment foreign_edge_dropped_total{reason=conditional_deferred}")
}
