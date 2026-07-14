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
	"github.com/c360studio/semstreams/pkg/errs"
)

// ---------------------------------------------------------------------------
// Unit tests for checkOwnerLease (ADR-056 PR-3 observe-only + PR-5 gated reject)
// ---------------------------------------------------------------------------
//
// All tests use a fakeClassifier (defined in foreign_edge_seam_test.go) as the
// injected ownershipClaimReader so no NATS is needed. checkOwnerLease returns a
// *leaseViolation: nil in the default observe-only posture (writes always commit,
// tests assert metric delta only); non-nil ONLY when Config.EnforceOwnerLease is
// set AND a mismatch is confirmed (PR-5 reject). The fail-open cases stay nil
// even under enforcement.

const (
	leaseEntityID    = "c360.platform.test.sys.widget.001"
	leasePred        = "mission.state.phase"
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
	pred := "mission.state.phase"
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

	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))
	assert.InDelta(t, before+1, after, 0.0001,
		"create_with_triples: stale OwnerToken must increment owner_lease_mismatch_total")
}

// TestOwnerLease_UpdateWithTriples_StaleToken_MetricAndCommits drives
// handleEntityUpdateWithTriples (non-CAS) with a stale token.
func TestOwnerLease_UpdateWithTriples_StaleToken_MetricAndCommits(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	mt := message.Type{Domain: "test", Category: "lease", Version: "v1"}
	pred := "mission.state.phase"
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

	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))
	assert.InDelta(t, before+1, after, 0.0001,
		"update_with_triples: stale OwnerToken must increment owner_lease_mismatch_total")
}

// TestOwnerLease_UpdateWithTriplesCAS_StaleToken_MetricAndCommits drives the
// CAS path (ExpectedRevision > 0) with a stale token.
func TestOwnerLease_UpdateWithTriplesCAS_StaleToken_MetricAndCommits(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	mt := message.Type{Domain: "test", Category: "lease", Version: "v1"}
	pred := "mission.state.phase"
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

	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, pred))
	assert.InDelta(t, before+1, after, 0.0001,
		"update_with_triples CAS lane: stale OwnerToken must increment owner_lease_mismatch_total")
}

// TestOwnerLease_EmptyToken_AllLanes_NoMetric verifies the legacy-writer skip:
// empty OwnerToken on all three owned-write lanes → no mismatch metric.
func TestOwnerLease_EmptyToken_AllLanes_NoMetric(t *testing.T) {
	mt := message.Type{Domain: "test", Category: "lease", Version: "v1"}
	label := mt.Key()
	pred := "mission.state.phase"

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

// ---------------------------------------------------------------------------
// PR-5 enforce-mode unit tests for checkOwnerLease (the gated reject).
// ---------------------------------------------------------------------------
//
// The enforce toggle (Config.EnforceOwnerLease) ONLY changes the verdict on a
// CONFIRMED mismatch (ok=true, incarnation!="", token!=expected). It must NEVER
// turn a fail-open path (empty token, nil reader, legacy incarnation, reader
// blip) into a reject — those stay nil even under enforcement.

// TestCheckOwnerLease_EnforceOff_StaleToken_NoViolation proves the default
// posture: a stale token meters the mismatch but returns nil (write commits).
func TestCheckOwnerLease_EnforceOff_StaleToken_NoViolation(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.config.EnforceOwnerLease = false // explicit: the default
	comp.claimReader = leaseFake(leasePred, leaseOwner, leaseIncarnation)
	label := "test.rule.v1"

	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))
	v := comp.checkOwnerLease(context.Background(), leaseEntityID, label, staleToken(), []string{leasePred})
	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))

	assert.Nil(t, v, "observe-only (enforce off): a stale token must NOT yield a reject verdict")
	assert.InDelta(t, before+1, after, 0.0001, "observe-only still meters the mismatch")
}

// TestCheckOwnerLease_EnforceOn_StaleToken_Violation proves the reject verdict
// carries the contested predicate, the live owner id (not the nonce), and the
// presented token — and still meters the mismatch.
func TestCheckOwnerLease_EnforceOn_StaleToken_Violation(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.config.EnforceOwnerLease = true
	comp.claimReader = leaseFake(leasePred, leaseOwner, leaseIncarnation)
	label := "test.rule.v1"

	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))
	v := comp.checkOwnerLease(context.Background(), leaseEntityID, label, staleToken(), []string{leasePred})
	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues(label, leasePred))

	require.NotNil(t, v, "enforce on: a stale token must yield a reject verdict")
	assert.Equal(t, leasePred, v.predicate)
	assert.Equal(t, leaseOwner, v.expectedOwner, "expectedOwner is the owner id only — no incarnation nonce")
	assert.NotContains(t, v.expectedOwner, "#", "the caller-facing owner must not leak the live incarnation nonce")
	assert.Equal(t, staleToken(), v.got)
	assert.InDelta(t, before+1, after, 0.0001, "enforce on still meters the mismatch")
}

// TestCheckOwnerLease_EnforceOn_MatchingToken_NoViolation proves a matching
// token never rejects, even with enforcement on.
func TestCheckOwnerLease_EnforceOn_MatchingToken_NoViolation(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.config.EnforceOwnerLease = true
	comp.claimReader = leaseFake(leasePred, leaseOwner, leaseIncarnation)

	v := comp.checkOwnerLease(context.Background(), leaseEntityID, "test.rule.v1", liveToken(), []string{leasePred})
	assert.Nil(t, v, "enforce on: a matching token must NOT reject")
}

// TestCheckOwnerLease_EnforceOn_LegacyIncarnation_FailOpen is the critical
// invariant: a legacy/pre-fence owner (ok=true, incarnation="") must fail-open
// even under enforcement — a naive compare against a fenced token would always
// mismatch and brick every legacy owner's writes.
func TestCheckOwnerLease_EnforceOn_LegacyIncarnation_FailOpen(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.config.EnforceOwnerLease = true
	comp.claimReader = leaseFake(leasePred, leaseOwner, "") // legacy owner

	before := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues("test.rule.v1", leasePred))
	v := comp.checkOwnerLease(context.Background(), leaseEntityID, "test.rule.v1", liveToken(), []string{leasePred})
	after := testutil.ToFloat64(comp.ownerLeaseMismatch.WithLabelValues("test.rule.v1", leasePred))

	assert.Nil(t, v, "enforce on: a legacy/pre-fence owner must fail-open (no reject)")
	assert.InDelta(t, before, after, 0.0001, "legacy owner: no metric even under enforcement")
}

// TestCheckOwnerLease_EnforceOn_ReaderError_FailOpen proves a transient reader
// blip never blocks, even under enforcement.
func TestCheckOwnerLease_EnforceOn_ReaderError_FailOpen(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.config.EnforceOwnerLease = true
	comp.claimReader = &fakeClassifier{owners: map[string]fakeOwnerEntry{leasePred: {err: assertErr}}}

	v := comp.checkOwnerLease(context.Background(), leaseEntityID, "test.rule.v1", staleToken(), []string{leasePred})
	assert.Nil(t, v, "enforce on: a reader error must fail-open (no reject)")
}

// TestCheckOwnerLease_EnforceOn_EmptyTokenAndNilReader_NoViolation proves the
// two-state skips never reject under enforcement.
func TestCheckOwnerLease_EnforceOn_EmptyTokenAndNilReader_NoViolation(t *testing.T) {
	// empty token
	{
		comp := createTestComponentWithMockKV(t)
		comp.config.EnforceOwnerLease = true
		comp.claimReader = leaseFake(leasePred, leaseOwner, leaseIncarnation)
		v := comp.checkOwnerLease(context.Background(), leaseEntityID, "test.rule.v1", "", []string{leasePred})
		assert.Nil(t, v, "enforce on: empty OwnerToken (legacy writer) must NOT reject")
	}
	// nil reader
	{
		comp := createTestComponentWithMockKV(t)
		comp.config.EnforceOwnerLease = true
		comp.claimReader = nil
		v := comp.checkOwnerLease(context.Background(), leaseEntityID, "test.rule.v1", liveToken(), []string{leasePred})
		assert.Nil(t, v, "enforce on: nil claimReader must graceful-skip (no reject)")
	}
}

// TestCheckOwnerLease_EnforceOn_MismatchThenReaderError_HonorsEarlierMismatch
// proves the err-path return still surfaces a mismatch confirmed on an EARLIER
// predicate (the reader blip on a later predicate must not erase a real reject).
func TestCheckOwnerLease_EnforceOn_MismatchThenReaderError_HonorsEarlierMismatch(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.config.EnforceOwnerLease = true
	const predA, predB = "test.lease.first", "test.lease.second"
	comp.claimReader = &fakeClassifier{owners: map[string]fakeOwnerEntry{
		predA: {owner: leaseOwner, incarnation: leaseIncarnation}, // confirmed mismatch vs stale token
		predB: {err: assertErr},                                   // reader blip on the next predicate
	}}

	v := comp.checkOwnerLease(context.Background(), leaseEntityID, "test.rule.v1", staleToken(), []string{predA, predB})
	require.NotNil(t, v, "a mismatch on predA must reject even though predB hit a reader error")
	assert.Equal(t, predA, v.predicate)
}

// ---------------------------------------------------------------------------
// PR-5 lane-wiring reject tests: enforce on → handler returns the coded reject
// and (for update lanes) the prior state is unchanged.
// ---------------------------------------------------------------------------

// TestOwnerLease_CreateWithTriples_EnforceOn_Rejects proves the create lane
// returns Success=false + ErrorCodeOwnerLeaseStale on a stale token.
func TestOwnerLease_CreateWithTriples_EnforceOn_Rejects(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.config.EnforceOwnerLease = true
	mt := message.Type{Domain: "test", Category: "lease", Version: "v1"}
	pred := "mission.state.phase"
	eid := "c360.test.lease.sys.w.crtrej"
	comp.claimReader = leaseFake(pred, leaseOwner, leaseIncarnation)

	req := graph.CreateEntityWithTriplesRequest{
		Entity:     &graph.EntityState{ID: eid, MessageType: mt},
		Triples:    []message.Triple{{Subject: eid, Predicate: pred, Object: "planning"}},
		OwnerToken: staleToken(),
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	// ADR-060: the reject is a typed error (owner_lease_stale / invalid class),
	// not an in-body Success=false.
	respData, handlerErr := comp.handleEntityCreateWithTriples(context.Background(), data)
	require.Error(t, handlerErr, "enforce on: create with a stale token must be rejected")
	assert.Nil(t, respData, "a hard failure returns no body")
	var ce *errs.ClassifiedError
	require.ErrorAs(t, handlerErr, &ce)
	assert.Equal(t, graph.ErrorCodeOwnerLeaseStale, ce.Code)
	assert.True(t, errs.IsInvalid(handlerErr))
}

// TestOwnerLease_UpdateWithTriples_EnforceOn_Rejects proves the update lane
// rejects AND leaves the prior state untouched (the delta never applied).
func TestOwnerLease_UpdateWithTriples_EnforceOn_Rejects(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.config.EnforceOwnerLease = true
	mt := message.Type{Domain: "test", Category: "lease", Version: "v1"}
	pred := "mission.state.phase"
	eid := "c360.test.lease.sys.w.updrej"
	comp.claimReader = leaseFake(pred, leaseOwner, leaseIncarnation)

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

	respData, handlerErr := comp.handleEntityUpdateWithTriples(context.Background(), data)
	require.Error(t, handlerErr, "enforce on: update with a stale token must be rejected")
	assert.Nil(t, respData, "a hard failure returns no body")
	var ce *errs.ClassifiedError
	require.ErrorAs(t, handlerErr, &ce)
	assert.Equal(t, graph.ErrorCodeOwnerLeaseStale, ce.Code)
	assert.True(t, errs.IsInvalid(handlerErr))

	// The prior value must be untouched — the reject returns before UpdateWithRetry.
	stored, _, readErr := comp.fetchEntityState(context.Background(), eid)
	require.NoError(t, readErr)
	var got string
	for _, tr := range stored.Triples {
		if tr.Predicate == pred {
			got, _ = tr.Object.(string)
		}
	}
	assert.Equal(t, "init", got, "rejected update must NOT have applied the delta")
}

// TestOwnerLease_UpdateWithTriplesCAS_EnforceOn_Rejects proves the CAS lane is
// covered by the single pre-dispatch check (reject before the CAS write).
func TestOwnerLease_UpdateWithTriplesCAS_EnforceOn_Rejects(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.config.EnforceOwnerLease = true
	mt := message.Type{Domain: "test", Category: "lease", Version: "v1"}
	pred := "mission.state.phase"
	eid := "c360.test.lease.sys.w.casrej"
	comp.claimReader = leaseFake(pred, leaseOwner, leaseIncarnation)

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

	respData, handlerErr := comp.handleEntityUpdateWithTriples(context.Background(), data)
	require.Error(t, handlerErr, "enforce on: CAS update with a stale token must be rejected")
	assert.Nil(t, respData, "a hard failure returns no body")
	var ce *errs.ClassifiedError
	require.ErrorAs(t, handlerErr, &ce)
	assert.Equal(t, graph.ErrorCodeOwnerLeaseStale, ce.Code)
	assert.True(t, errs.IsInvalid(handlerErr))
}

// TestOwnerLease_EnforceOn_MeteredMutation_RecordsRejection proves the reject
// flows through meteredMutation as mutation_rejections_total{reason=owner_lease_stale}
// — the operator-facing rejection metric, for free via the wrapper.
func TestOwnerLease_EnforceOn_MeteredMutation_RecordsRejection(t *testing.T) {
	comp := createTestComponentWithMockKV(t)
	comp.config.EnforceOwnerLease = true
	mt := message.Type{Domain: "test", Category: "lease", Version: "v1"}
	pred := "mission.state.phase"
	eid := "c360.test.lease.sys.w.metrej"
	comp.claimReader = leaseFake(pred, leaseOwner, leaseIncarnation)

	req := graph.CreateEntityWithTriplesRequest{
		Entity:     &graph.EntityState{ID: eid, MessageType: mt},
		Triples:    []message.Triple{{Subject: eid, Predicate: pred, Object: "planning"}},
		OwnerToken: staleToken(),
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	before := testutil.ToFloat64(comp.mutationRejections.WithLabelValues(SubjectEntityCreateWithTriples, graph.ErrorCodeOwnerLeaseStale))
	handler := comp.meteredMutation(SubjectEntityCreateWithTriples, comp.handleEntityCreateWithTriples)
	_, handlerErr := handler(context.Background(), data)
	// ADR-060: the reject now flows through meteredMutation's error path, which
	// reads the rejection reason from ce.Code (owner_lease_stale).
	require.Error(t, handlerErr)
	after := testutil.ToFloat64(comp.mutationRejections.WithLabelValues(SubjectEntityCreateWithTriples, graph.ErrorCodeOwnerLeaseStale))

	assert.InDelta(t, before+1, after, 0.0001,
		"meteredMutation must record mutation_rejections_total{reason=owner_lease_stale} on the gated reject")
}
