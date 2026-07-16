//go:build integration

// gh#429: the executor must not dispatch a referential-integrity stub — an
// entity graph-ingest materializes at an entity_id-referenced-but-not-yet-born
// ID (under the same unit prefix). A stub carries no depends_on edges, so
// pre-fix the brain derived it as dependency-free and dispatched it ahead of its
// real content, bypassing dependency ordering. These drive the REAL graph-ingest
// mutation/query wire + the executor, proving: (1) a bare stub is never
// dispatched; (2) it dispatches once its real content lands on a RE-STAMPING
// lane (update_with_triples, non-zero MessageType) — the self-heal; and (3) a
// non-re-stamping "birth" (triple.add only) does NOT self-heal — locking the
// self-heal precondition into CI so a future write-lane change fails loudly
// rather than silently wedging a paid run.
package gateddagexec_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
)

// stubByReference materializes targetUnitID as a referential-integrity stub by
// creating an entity OUTSIDE the unit prefix (so the referencer itself is not a
// unit) whose relationship triple names targetUnitID. graph-ingest's referential
// integrity synchronously creates the stub under targetUnitID.
func (fs *fullStack) stubByReference(t *testing.T, refID, targetUnitID string) {
	t.Helper()
	require.NoError(t, fs.gi.CreateEntity(context.Background(), &gtypes.EntityState{
		ID:          refID,
		MessageType: message.Type{Domain: "fs", Category: "holder", Version: "v1"},
		Triples: []message.Triple{
			{Subject: refID, Predicate: "fs.reference.target", Object: targetUnitID, Timestamp: time.Now(), Confidence: 1.0},
		},
		Version:   1,
		UpdatedAt: time.Now(),
	}))
	// Confirm the stub actually landed (and carries the stub envelope) before the
	// assertions depend on it.
	require.True(t, fs.unitIsStub(t, targetUnitID), "reference must have materialized %s as a stub", targetUnitID)
}

// birthRestamping writes real content on a RE-STAMPING lane: update_with_triples
// with a NON-ZERO MessageType overwrites the stub envelope (preserve-when-zero,
// non-zero wins), so IsStub flips false.
func (fs *fullStack) birthRestamping(t *testing.T, id string) {
	t.Helper()
	req := gtypes.UpdateEntityWithTriplesRequest{
		Entity: &gtypes.EntityState{ID: id, MessageType: message.Type{Domain: "fs", Category: "unit", Version: "v1"}},
		AddTriples: []message.Triple{
			{Subject: id, Predicate: "fs.unit.ready", Object: true, Timestamp: time.Now(), Confidence: 1.0},
		},
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)
	_, err = fs.nc.RequestWithRetryClassified(context.Background(), "graph.mutation.entity.update_with_triples", data, 5*time.Second, natsclient.DefaultRetryConfig())
	require.NoError(t, err)
}

// unitIsStub reads the entity authoritatively and reports whether it still
// carries the stub envelope.
func (fs *fullStack) unitIsStub(t *testing.T, id string) bool {
	t.Helper()
	data, err := json.Marshal(gtypes.PrefixQueryRequest{Prefix: fs.prefix, Limit: 100})
	require.NoError(t, err)
	resp, err := fs.nc.RequestClassified(context.Background(), "graph.ingest.query.prefix", data, 5*time.Second)
	require.NoError(t, err)
	var pr gtypes.PrefixQueryResponse
	require.NoError(t, json.Unmarshal(resp, &pr))
	for i := range pr.Entities {
		if pr.Entities[i].ID == id {
			return pr.Entities[i].IsStub()
		}
	}
	return false
}

func TestFullStack_ReferentialStubNotDispatchedUntilBorn(t *testing.T) {
	const subject = "fs.dispatch.s8"
	prefix := "fs.test.s8.fanout.unit"
	fs := setupFullStack(t, fsOpts{prefix: prefix, subject: subject})

	selfHeal := fsUnit("s8", "selfheal") // stubbed, then born on a re-stamping lane
	stuck := fsUnit("s8", "stuck")       // stubbed, then "born" via triple.add only

	// 1. Materialize both as referential stubs (referencers live outside the unit
	//    prefix so they are not themselves units).
	fs.stubByReference(t, "fs.holder.ref.entity.owner.one", selfHeal)
	fs.stubByReference(t, "fs.holder.ref.entity.owner.two", stuck)

	// 2. Neither dispatches while a bare stub — several backstop passes prove it.
	time.Sleep(2 * time.Second)
	require.Equal(t, 0, fs.dispatched.count(selfHeal), "a bare stub must not be dispatched (gh#429)")
	require.Equal(t, 0, fs.dispatched.count(stuck), "a bare stub must not be dispatched (gh#429)")

	// 3. Self-heal: birth selfHeal on a re-stamping lane → envelope flips off the
	//    stub → the executor dispatches it.
	fs.birthRestamping(t, selfHeal)
	require.Eventually(t, func() bool { return fs.dispatched.count(selfHeal) == 1 }, fsEventually, 100*time.Millisecond,
		"once a real producer re-stamps the stub envelope, the unit dispatches (self-heal)")
	require.Eventually(t, func() bool { return fs.unitHasPredicate(t, selfHeal, pCompleted) }, fsEventually, 100*time.Millisecond,
		"the self-healed unit must persist the canonical completion marker after dispatch")

	// 4. Precondition (negative): "birth" stuck via triple.add only. triple.add
	//    never touches MessageType, so the stub envelope survives → the unit stays
	//    filtered → it never dispatches. A future semspec write-lane change to a
	//    non-re-stamping birth would surface HERE, not as a silent paid-run wedge.
	addMarker(context.Background(), fs.nc, message.Triple{
		Subject: stuck, Predicate: semantictest.Predicate(t, "fs", "unit", "content"), Object: true, Timestamp: time.Now(), Confidence: 1.0,
	})
	require.True(t, fs.unitIsStub(t, stuck), "triple.add must not re-stamp the stub envelope")
	time.Sleep(2 * time.Second)
	require.Equal(t, 0, fs.dispatched.count(stuck),
		"a triple.add-only birth does not re-stamp the envelope, so the unit stays filtered (precondition)")
}
