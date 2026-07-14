package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
)

// gh#435: a real create_with_triples for an ID that exists only as a referential
// stub must RE-STAMP the stub (its true birth) and SUCCEED — not reject as
// entity_already_exists, which meteredMutation logs as "graph mutation rejected"
// (a false GRAPH_WRITE_REJECT that trips paid-run monitors). Success ⇒ no reject
// ⇒ no WARN (the WARN is emitted by meteredMutation only on a non-nil err).
func TestCreateWithTriples_ReStampsStub(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()
	const target = "c360.platform.test.sys.unit.001"

	// Materialize target as a bare referential stub.
	require.NoError(t, comp.ensureReferencedEntityExists(ctx, target, "c360.platform.test.sys.parent.001",
		message.Type{Domain: "test", Category: "fixture", Version: "v1"}))
	require.True(t, storedEntity(t, comp, target).IsStub(), "precondition: target is a stub")

	before := testutil.ToFloat64(comp.stubRestamps)

	// Real producer births it via create_with_triples carrying a real envelope.
	realType := message.Type{Domain: "workflow", Category: "task-unit", Version: "v1"}
	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: target, MessageType: realType},
		Triples: []message.Triple{
			{Subject: target, Predicate: "entity.identity.label", Object: "Born", Timestamp: time.Now(), Confidence: 1.0},
		},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	resp, err := comp.handleEntityCreateWithTriples(ctx, data)
	require.NoError(t, err, "create_with_triples on a stub must SUCCEED (re-stamp), not reject")
	require.NotNil(t, resp)

	born := storedEntity(t, comp, target)
	assert.False(t, born.IsStub(), "the stub envelope must be re-stamped to the real type")
	assert.Equal(t, realType, born.MessageType)
	assert.True(t, hasPredicate(born, "entity.identity.label"), "real triples merged onto the born entity")
	assert.Equal(t, before+1, testutil.ToFloat64(comp.stubRestamps),
		"a re-stamp ticks the distinct stub_restamps_total counter — a positive signal, not a rejection")
}

// gh#435 guard: a create_with_triples carrying a ZERO/invalid MessageType must
// NOT re-stamp a stub — MergeEntity overwrites MessageType unconditionally, so a
// zero-typed merge would demote the stub to a typeless non-stub entity (the gh#429
// dispatchable-non-real class). It must reject and leave the stub intact.
func TestCreateWithTriples_ZeroTypedDoesNotRestampStub(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()
	const target = "c360.platform.test.sys.unit.003"

	require.NoError(t, comp.ensureReferencedEntityExists(ctx, target, "c360.platform.test.sys.parent.001",
		message.Type{Domain: "test", Category: "fixture", Version: "v1"}))
	require.True(t, storedEntity(t, comp, target).IsStub(), "precondition: target is a stub")

	before := testutil.ToFloat64(comp.stubRestamps)

	// create_with_triples with a ZERO MessageType (no real envelope).
	req := graph.CreateEntityWithTriplesRequest{
		Entity:  &graph.EntityState{ID: target}, // zero MessageType
		Triples: []message.Triple{{Subject: target, Predicate: "entity.identity.label", Object: "x", Timestamp: time.Now(), Confidence: 1.0}},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	_, err = comp.handleEntityCreateWithTriples(ctx, data)
	require.Error(t, err, "a zero-typed create on a stub must reject, not demote the stub")
	var ce *errs.ClassifiedError
	require.True(t, errors.As(err, &ce), "reject must be classified")
	assert.Equal(t, graph.ErrorCodeEntityExists, ce.Code)

	still := storedEntity(t, comp, target)
	assert.True(t, still.IsStub(), "the stub envelope must be PRESERVED (not overwritten to typeless)")
	assert.Equal(t, before, testutil.ToFloat64(comp.stubRestamps), "a rejected zero-typed create must NOT tick the restamp counter")
}

// A create_with_triples collision with a REAL (non-stub) entity is a genuine
// conflict and must still reject with entity_already_exists (the restamp counter
// must NOT move).
func TestCreateWithTriples_RealCollisionStillRejects(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	ctx := context.Background()
	const target = "c360.platform.test.sys.unit.002"

	realType := message.Type{Domain: "workflow", Category: "task-unit", Version: "v1"}
	mk := func() []byte {
		req := graph.CreateEntityWithTriplesRequest{
			Entity:  &graph.EntityState{ID: target, MessageType: realType},
			Triples: []message.Triple{{Subject: target, Predicate: "entity.identity.label", Object: "v", Timestamp: time.Now(), Confidence: 1.0}},
		}
		data, _ := json.Marshal(req)
		return data
	}

	// First create succeeds (real birth, no stub involved).
	_, err := comp.handleEntityCreateWithTriples(ctx, mk())
	require.NoError(t, err)

	before := testutil.ToFloat64(comp.stubRestamps)

	// Second create on the now-real entity is a true conflict → reject.
	_, err = comp.handleEntityCreateWithTriples(ctx, mk())
	require.Error(t, err, "create on an existing REAL entity must still reject")
	var ce *errs.ClassifiedError
	require.True(t, errors.As(err, &ce), "reject must be classified")
	assert.Equal(t, graph.ErrorCodeEntityExists, ce.Code, "a real collision keeps the entity_already_exists reject")
	assert.Equal(t, before, testutil.ToFloat64(comp.stubRestamps), "a real conflict must NOT tick the restamp counter")
}
