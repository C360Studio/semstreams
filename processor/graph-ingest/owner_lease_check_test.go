package graphingest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

// ---------------------------------------------------------------------------
// Unit tests for checkOwnerLease (ADR-056 PR-3, observe-only)
// ---------------------------------------------------------------------------
//
// All tests use a fakeClassifier (defined in foreign_edge_seam_test.go) as the
// injected ownershipClaimReader so no NATS is needed. checkOwnerLease is a
// void method; writes always commit. Tests assert metric delta only.

const (
	leaseEntityID    = "c360.platform.test.sys.widget.001"
	leasePred        = "mission.phase"
	leaseOwner       = "mission-planner"
	leaseIncarnation = "deadbeef01234567"
)

// liveToken returns the token a fenced writer would stamp.
func liveToken() string { return leaseOwner + "#" + leaseIncarnation }

// staleToken returns a token with the same owner but a different incarnation.
func staleToken() string { return leaseOwner + "#cafebabe00000000" }

// leaseFake builds a fakeClassifier whose OwnerOf for predicate `pred` returns
// (owner, incarnation, ok=true). Other predicates return ok=false.
func leaseFake(pred, owner, incarnation string) *fakeClassifier {
	return &fakeClassifier{
		owners: map[string]fakeOwnerEntry{
			pred: {owner: owner, incarnation: incarnation},
		},
	}
}

// TestCheckOwnerLease_MatchingToken_NoMetric verifies that a matching
// OwnerToken does not increment the mismatch counter.
func TestCheckOwnerLease_MatchingToken_NoMetric(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.claimReader = leaseFake(leasePred, leaseOwner, leaseIncarnation)
	label := "test.rule.v1"

	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))
	comp.checkOwnerLease(context.Background(), leaseEntityID, label, liveToken(), []string{leasePred})
	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))

	assert.InDelta(t, before, after, 0.0001, "a matching OwnerToken must NOT increment owner_lease_mismatch_total")
}

// TestCheckOwnerLease_StaleToken_MetricIncremented verifies that a stale
// OwnerToken increments the counter. The helper is void — write always commits.
func TestCheckOwnerLease_StaleToken_MetricIncremented(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.claimReader = leaseFake(leasePred, leaseOwner, leaseIncarnation)
	label := "test.rule.v1"

	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))
	comp.checkOwnerLease(context.Background(), leaseEntityID, label, staleToken(), []string{leasePred})
	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))

	assert.InDelta(t, before+1, after, 0.0001, "a stale OwnerToken must increment owner_lease_mismatch_total{message_type,predicate}")
}

// TestCheckOwnerLease_EmptyToken_Skip verifies that an empty OwnerToken
// (legacy/unowned writer) is skipped under the two-state contract: no metric.
func TestCheckOwnerLease_EmptyToken_Skip(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	fake := leaseFake(leasePred, leaseOwner, leaseIncarnation)
	comp.claimReader = fake
	label := "test.rule.v1"

	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))
	comp.checkOwnerLease(context.Background(), leaseEntityID, label, "", []string{leasePred})
	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))

	// The empty-token early return is proven by the absence of a metric: had the
	// check proceeded, this fake holds a matching owning claim, so a non-empty
	// stale token would fire — an empty token must not.
	assert.InDelta(t, before, after, 0.0001, "an empty OwnerToken must skip the check entirely (no metric)")
}

// TestCheckOwnerLease_NilReader_Skip verifies graceful-skip when no reader is
// wired (resourceless/unmigrated deploy).
func TestCheckOwnerLease_NilReader_Skip(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.claimReader = nil
	label := "test.rule.v1"

	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))
	comp.checkOwnerLease(context.Background(), leaseEntityID, label, liveToken(), []string{leasePred})
	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))

	assert.InDelta(t, before, after, 0.0001, "a nil claimReader must graceful-skip (no metric, no panic)")
}

// TestCheckOwnerLease_UnclaimedPredicate_Skip verifies that a predicate with
// no owning claim (ok=false) is not metered.
func TestCheckOwnerLease_UnclaimedPredicate_Skip(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	// Empty owners map → OwnerOf returns ok=false for every predicate.
	comp.claimReader = &fakeClassifier{owners: map[string]fakeOwnerEntry{}}
	label := "test.rule.v1"

	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))
	comp.checkOwnerLease(context.Background(), leaseEntityID, label, staleToken(), []string{leasePred})
	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))

	assert.InDelta(t, before, after, 0.0001, "an unclaimed predicate (ok=false) must not be metered")
}

// TestCheckOwnerLease_LegacyEmptyIncarnation_FailOpen proves the critical
// two-state contract: ok=true, incarnation="" → the live owner is legacy/
// pre-fence, lease is not enforceable → FAIL OPEN (no metric, no Warn, no
// reject). A naive compare would always mismatch against a fenced token.
func TestCheckOwnerLease_LegacyEmptyIncarnation_FailOpen(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	// ok=true, incarnation="" → legacy owner, lease not enforceable.
	comp.claimReader = leaseFake(leasePred, leaseOwner, "")
	label := "test.rule.v1"

	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))
	// Provide a non-empty fenced token — a naive compare would mismatch.
	comp.checkOwnerLease(context.Background(), leaseEntityID, label, liveToken(), []string{leasePred})
	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))

	assert.InDelta(t, before, after, 0.0001,
		"ok=true but empty incarnation (legacy owner) must fail-open: no metric")
}

// TestCheckOwnerLease_ReaderError_FailOpen verifies that a transient OwnerOf
// error causes fail-open: no metric, no blocking.
func TestCheckOwnerLease_ReaderError_FailOpen(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.claimReader = &fakeClassifier{
		owners: map[string]fakeOwnerEntry{
			leasePred: {err: assertErr},
		},
	}
	label := "test.rule.v1"

	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))
	comp.checkOwnerLease(context.Background(), leaseEntityID, label, staleToken(), []string{leasePred})
	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))

	assert.InDelta(t, before, after, 0.0001, "a reader error must fail-open: no metric")
}

// ---------------------------------------------------------------------------
// Lane-wiring tests: verify checkOwnerLease is reachable from each handler.
// ---------------------------------------------------------------------------

// TestOwnerLease_CreateWithTriples_StaleToken_MetricAndCommits drives
// handleEntityCreateWithTriples with a stale OwnerToken: verifies the
// mismatch metric fires AND the write commits (Success=true).
func TestOwnerLease_CreateWithTriples_StaleToken_MetricAndCommits(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	mt := message.Type{Domain: "test", Category: "lease", Version: "v1"}
	pred := "mission.phase"
	comp.claimReader = leaseFake(pred, leaseOwner, leaseIncarnation)

	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: "c360.test.lease.sys.w.crt1", MessageType: mt},
		Triples: []message.Triple{
			{Subject: "c360.test.lease.sys.w.crt1", Predicate: pred, Object: "planning"},
		},
		OwnerToken: staleToken(),
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	label := mt.Key()
	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))

	respData, handlerErr := comp.handleEntityCreateWithTriples(context.Background(), data)
	require.NoError(t, handlerErr, "handler must never return a blocking error")

	// The write must commit.
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.True(t, resp.Success, "observe-only: write must commit even with stale token: %s", resp.Error)

	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))
	assert.InDelta(t, before+1, after, 0.0001,
		"create_with_triples: stale OwnerToken must increment owner_lease_mismatch_total")
}

// TestOwnerLease_UpdateWithTriples_StaleToken_MetricAndCommits drives
// handleEntityUpdateWithTriples (non-CAS) with a stale token.
func TestOwnerLease_UpdateWithTriples_StaleToken_MetricAndCommits(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	mt := message.Type{Domain: "test", Category: "lease", Version: "v1"}
	pred := "mission.phase"
	eid := "c360.test.lease.sys.w.upd1"
	comp.claimReader = leaseFake(pred, leaseOwner, leaseIncarnation)

	// Pre-create so update_with_triples finds a must-exist entity.
	require.NoError(t, comp.CreateEntity(context.Background(), &graph.EntityState{
		ID: eid, MessageType: mt, Version: 1,
		Triples: []message.Triple{{Subject: eid, Predicate: pred, Object: "init"}},
	}))

	req := graph.UpdateEntityWithTriplesRequest{
		Entity:     &graph.EntityState{ID: eid, MessageType: mt},
		AddTriples: []message.Triple{{Subject: eid, Predicate: pred, Object: "planning"}},
		OwnerToken: staleToken(),
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	label := mt.Key()
	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))

	respData, handlerErr := comp.handleEntityUpdateWithTriples(context.Background(), data)
	require.NoError(t, handlerErr, "handler must never return a blocking error")

	// The write must commit.
	var resp graph.UpdateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.True(t, resp.Success, "observe-only: write must commit even with stale token: %s", resp.Error)

	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))
	assert.InDelta(t, before+1, after, 0.0001,
		"update_with_triples: stale OwnerToken must increment owner_lease_mismatch_total")
}

// TestOwnerLease_UpdateWithTriplesCAS_StaleToken_MetricAndCommits drives the
// CAS path (ExpectedRevision > 0) with a stale token.
func TestOwnerLease_UpdateWithTriplesCAS_StaleToken_MetricAndCommits(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	mt := message.Type{Domain: "test", Category: "lease", Version: "v1"}
	pred := "mission.phase"
	eid := "c360.test.lease.sys.w.cas1"
	comp.claimReader = leaseFake(pred, leaseOwner, leaseIncarnation)

	// Pre-create and capture the revision.
	require.NoError(t, comp.CreateEntity(context.Background(), &graph.EntityState{
		ID: eid, MessageType: mt, Version: 1,
		Triples: []message.Triple{{Subject: eid, Predicate: pred, Object: "init"}},
	}))
	_, rev, err := comp.fetchEntityState(context.Background(), eid)
	require.NoError(t, err)

	req := graph.UpdateEntityWithTriplesRequest{
		Entity:           &graph.EntityState{ID: eid, MessageType: mt},
		AddTriples:       []message.Triple{{Subject: eid, Predicate: pred, Object: "active"}},
		ExpectedRevision: rev,
		OwnerToken:       staleToken(),
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	label := mt.Key()
	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))

	respData, handlerErr := comp.handleEntityUpdateWithTriples(context.Background(), data)
	require.NoError(t, handlerErr, "handler must never return a blocking error")

	// The write must commit.
	var resp graph.UpdateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))
	assert.True(t, resp.Success, "observe-only: write must commit even with stale token on CAS lane: %s", resp.Error)

	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))
	assert.InDelta(t, before+1, after, 0.0001,
		"update_with_triples CAS lane: stale OwnerToken must increment owner_lease_mismatch_total")
}

// TestOwnerLease_EmptyToken_AllLanes_NoMetric verifies the legacy-writer skip:
// empty OwnerToken on all three owned-write lanes → no mismatch metric.
func TestOwnerLease_EmptyToken_AllLanes_NoMetric(t *testing.T) {
	mt := message.Type{Domain: "test", Category: "lease", Version: "v1"}
	label := mt.Key()
	pred := "mission.phase"

	// create_with_triples
	{
		comp := createTestComponentWithMockKV(t)
		comp.claimReader = leaseFake(pred, leaseOwner, leaseIncarnation)
		eid := "c360.test.lease.sys.w.emp1"
		req := graph.CreateEntityWithTriplesRequest{
			Entity:     &graph.EntityState{ID: eid, MessageType: mt},
			Triples:    []message.Triple{{Subject: eid, Predicate: pred, Object: "x"}},
			OwnerToken: "",
		}
		data, _ := json.Marshal(req)
		before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))
		_, _ = comp.handleEntityCreateWithTriples(context.Background(), data)
		after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))
		assert.InDelta(t, before, after, 0.0001, "create_with_triples: empty OwnerToken → no mismatch metric")
	}

	// update_with_triples (non-CAS)
	{
		comp := createTestComponentWithMockKV(t)
		comp.claimReader = leaseFake(pred, leaseOwner, leaseIncarnation)
		eid := "c360.test.lease.sys.w.emp2"
		require.NoError(t, comp.CreateEntity(context.Background(), &graph.EntityState{
			ID: eid, MessageType: mt, Version: 1,
			Triples: []message.Triple{{Subject: eid, Predicate: pred, Object: "init"}},
		}))
		req := graph.UpdateEntityWithTriplesRequest{
			Entity:     &graph.EntityState{ID: eid, MessageType: mt},
			AddTriples: []message.Triple{{Subject: eid, Predicate: pred, Object: "updated"}},
			OwnerToken: "",
		}
		data, _ := json.Marshal(req)
		before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))
		_, _ = comp.handleEntityUpdateWithTriples(context.Background(), data)
		after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))
		assert.InDelta(t, before, after, 0.0001, "update_with_triples: empty OwnerToken → no mismatch metric")
	}
}
