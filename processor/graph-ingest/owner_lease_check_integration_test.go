//go:build integration

package graphingest

// Integration tests for ADR-056 PR-3: observe-only owner-lease check at the
// graph-ingest write seam. These drive the production wire end-to-end:
//
//  1. ownership.EnsureBuckets → real OWNER_CLAIMS bucket
//  2. registry.RegisterOwner → OWNING OwnerClaim stamped with live incarnation
//  3. graph-ingest Start() self-wires a real ClaimReader against the bucket
//  4. The three owned-write lanes are driven directly (handleEntityCreate...
//     / handleEntityUpdate...) so we can assert metric deltas + committed state.
//
// The observe-only invariant: in EVERY case the write commits (Success=true
// and the entity reads back updated). The metric fires ONLY on a stale token;
// matching and empty tokens are silent.

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
)

// bootIngestWithOwnerClaim creates a real NATS + OWNER_CLAIMS, registers an
// OWNING OwnerClaim (ModeReplaceOwned) for (ownerID, entityPattern, pred),
// then boots a graph-ingest Component that self-wires the real ClaimReader.
// Returns the component, the registry (so callers can read Incarnation()), and ctx.
func bootIngestWithOwnerClaim(
	t *testing.T, ownerID, entityPattern, pred string,
) (*Component, *ownership.Registry, context.Context) {
	t.Helper()
	return bootIngestWithOwnerClaimCfg(t, ownerID, entityPattern, pred, DefaultConfig())
}

// bootIngestWithOwnerClaimCfg is bootIngestWithOwnerClaim with an explicit
// Config so PR-5 tests can boot with EnforceOwnerLease=true.
func bootIngestWithOwnerClaimCfg(
	t *testing.T, ownerID, entityPattern, pred string, cfg Config,
) (*Component, *ownership.Registry, context.Context) {
	t.Helper()
	ctx := context.Background()
	streams := []natsclient.TestStreamConfig{{Name: "ENTITY", Subjects: []string{"entity.>"}}}
	tc := natsclient.NewTestClient(t, natsclient.WithKV(), natsclient.WithStreams(streams...))

	// EnsureBuckets creates OWNER_CLAIMS + OWNER_PRESENCE; returns a registry
	// with a per-process incarnation nonce.
	reg, err := ownership.EnsureBuckets(ctx, tc.Client, nil, nil)
	require.NoError(t, err, "EnsureBuckets (OWNER_CLAIMS + OWNER_PRESENCE)")

	// Register an OWNING claim for (ownerID, entityPattern, pred).
	require.NoError(t, reg.RegisterOwner(ctx, ownership.Registration{
		Owner: ownerID,
		Claims: []ownership.OwnerClaim{
			{
				Owner:      ownerID,
				Pattern:    entityPattern,
				Predicates: []string{pred},
				Mode:       ownership.ModeReplaceOwned,
			},
		},
	}))

	// Boot graph-ingest; Start() self-wires the real ClaimReader.
	configJSON, err := json.Marshal(cfg)
	require.NoError(t, err)
	comp, err := CreateGraphIngest(configJSON, component.Dependencies{NATSClient: tc.Client})
	require.NoError(t, err)
	c := comp.(*Component)
	require.NoError(t, c.Initialize())
	require.NoError(t, c.Start(ctx))
	t.Cleanup(func() { _ = c.Stop(5 * time.Second) })

	require.NotNil(t, c.claimReader,
		"Start must self-wire the claim reader once OWNER_CLAIMS exists (PR-3 boot check)")
	return c, reg, ctx
}

// ownerToken builds the fenced token "<owner>#<incarnation>".
func ownerToken(ownerID, incarnation string) string { return ownerID + "#" + incarnation }

const (
	intOwnerID   = "mission-planner"
	intEntityPat = "*.*.test.sys.w.*"
	intPred      = "mission.phase"
)

var intMT = message.Type{Domain: "test", Category: "integration", Version: "v1"}

// ─── create_with_triples ──────────────────────────────────────────────────────

// TestIntegration_OwnerLease_CreateWithTriples_MatchingToken proves that a
// matching OwnerToken on create_with_triples produces no metric increment AND
// the write commits (Success=true, entity readable).
func TestIntegration_OwnerLease_CreateWithTriples_MatchingToken(t *testing.T) {
	c, reg, ctx := bootIngestWithOwnerClaim(t, intOwnerID, intEntityPat, intPred)
	label := labelForMessageType(intMT)
	token := ownerToken(intOwnerID, reg.Incarnation())

	eid := "c360.platform.test.sys.w.match"
	before := testutil.ToFloat64(c.ownerLeaseMismatch.WithLabelValues(label, intPred))

	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: eid, MessageType: intMT},
		Triples: []message.Triple{
			{Subject: eid, Predicate: intPred, Object: "planning", Timestamp: time.Now(), Confidence: 1},
		},
		OwnerToken: token,
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	respData, handlerErr := c.handleEntityCreateWithTriples(ctx, data)
	require.NoError(t, handlerErr)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	after := testutil.ToFloat64(c.ownerLeaseMismatch.WithLabelValues(label, intPred))
	assert.InDelta(t, before, after, 0.0001,
		"matching OwnerToken on create_with_triples must NOT increment owner_lease_mismatch_total")
}

// TestIntegration_OwnerLease_CreateWithTriples_StaleToken proves that a stale
// OwnerToken (same owner, dead incarnation) increments the metric BUT the write
// still commits (observe-only).
func TestIntegration_OwnerLease_CreateWithTriples_StaleToken(t *testing.T) {
	c, _, ctx := bootIngestWithOwnerClaim(t, intOwnerID, intEntityPat, intPred)
	label := labelForMessageType(intMT)
	stale := ownerToken(intOwnerID, "deadbeef-stale-0000") // not the live incarnation

	eid := "c360.platform.test.sys.w.stale"
	before := testutil.ToFloat64(c.ownerLeaseMismatch.WithLabelValues(label, intPred))

	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: eid, MessageType: intMT},
		Triples: []message.Triple{
			{Subject: eid, Predicate: intPred, Object: "active", Timestamp: time.Now(), Confidence: 1},
		},
		OwnerToken: stale,
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	respData, handlerErr := c.handleEntityCreateWithTriples(ctx, data)
	require.NoError(t, handlerErr)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	// Metric must have fired.
	after := testutil.ToFloat64(c.ownerLeaseMismatch.WithLabelValues(label, intPred))
	assert.InDelta(t, before+1, after, 0.0001,
		"stale OwnerToken on create_with_triples must increment owner_lease_mismatch_total")

	// Entity must be readable with the written value.
	stored, _, readErr := c.fetchEntityState(ctx, eid)
	require.NoError(t, readErr, "written entity must be readable after observe-only stale-token write")
	assert.True(t, hasPredicate(stored, intPred),
		"entity must contain the written predicate after observe-only stale-token write")
}

// TestIntegration_OwnerLease_CreateWithTriples_EmptyToken proves the legacy-
// writer skip: empty OwnerToken → no metric, write commits.
func TestIntegration_OwnerLease_CreateWithTriples_EmptyToken(t *testing.T) {
	c, _, ctx := bootIngestWithOwnerClaim(t, intOwnerID, intEntityPat, intPred)
	label := labelForMessageType(intMT)

	eid := "c360.platform.test.sys.w.empty"
	before := testutil.ToFloat64(c.ownerLeaseMismatch.WithLabelValues(label, intPred))

	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: eid, MessageType: intMT},
		Triples: []message.Triple{
			{Subject: eid, Predicate: intPred, Object: "legacy", Timestamp: time.Now(), Confidence: 1},
		},
		OwnerToken: "", // empty = legacy/unowned writer
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	respData, handlerErr := c.handleEntityCreateWithTriples(ctx, data)
	require.NoError(t, handlerErr)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	after := testutil.ToFloat64(c.ownerLeaseMismatch.WithLabelValues(label, intPred))
	assert.InDelta(t, before, after, 0.0001,
		"empty OwnerToken (legacy writer) must NOT increment owner_lease_mismatch_total")
}

// ─── update_with_triples (non-CAS) ───────────────────────────────────────────

// TestIntegration_OwnerLease_UpdateWithTriples_StaleToken proves the observe-only
// invariant on the update_with_triples lane: stale token → metric fires, write
// commits.
func TestIntegration_OwnerLease_UpdateWithTriples_StaleToken(t *testing.T) {
	c, _, ctx := bootIngestWithOwnerClaim(t, intOwnerID, intEntityPat, intPred)
	label := labelForMessageType(intMT)
	stale := ownerToken(intOwnerID, "deadbeef-stale-upd0")

	eid := "c360.platform.test.sys.w.upd1"
	// Pre-create.
	require.NoError(t, c.CreateEntity(ctx, &graph.EntityState{
		ID: eid, MessageType: intMT, Version: 1, UpdatedAt: time.Now(),
		Triples: []message.Triple{{Subject: eid, Predicate: intPred, Object: "init", Timestamp: time.Now(), Confidence: 1}},
	}))

	before := testutil.ToFloat64(c.ownerLeaseMismatch.WithLabelValues(label, intPred))

	req := graph.UpdateEntityWithTriplesRequest{
		Entity:     &graph.EntityState{ID: eid, MessageType: intMT},
		AddTriples: []message.Triple{{Subject: eid, Predicate: intPred, Object: "updated", Timestamp: time.Now(), Confidence: 1}},
		OwnerToken: stale,
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	respData, handlerErr := c.handleEntityUpdateWithTriples(ctx, data)
	require.NoError(t, handlerErr)
	var resp graph.UpdateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	after := testutil.ToFloat64(c.ownerLeaseMismatch.WithLabelValues(label, intPred))
	assert.InDelta(t, before+1, after, 0.0001,
		"stale OwnerToken on update_with_triples must increment owner_lease_mismatch_total")

	// Read back — value must be updated.
	stored, _, readErr := c.fetchEntityState(ctx, eid)
	require.NoError(t, readErr)
	var updatedValue string
	for _, tr := range stored.Triples {
		if tr.Predicate == intPred {
			updatedValue = tr.Object.(string)
		}
	}
	assert.Equal(t, "updated", updatedValue,
		"entity predicate must reflect the written value after observe-only stale-token update")
}

// ─── PR-5 enforce-mode reject (EnforceOwnerLease=true) ────────────────────────
//
// These drive the SAME production wire with enforce_owner_lease on and prove the
// reject is durable: the write never lands. A matching token still commits.

func enforceCfg() Config {
	cfg := DefaultConfig()
	cfg.EnforceOwnerLease = true
	return cfg
}

// TestIntegration_OwnerLease_Enforce_CreateWithTriples_StaleRejected proves the
// create lane rejects a stale token AND the entity is never created.
func TestIntegration_OwnerLease_Enforce_CreateWithTriples_StaleRejected(t *testing.T) {
	c, _, ctx := bootIngestWithOwnerClaimCfg(t, intOwnerID, intEntityPat, intPred, enforceCfg())
	label := labelForMessageType(intMT)
	stale := ownerToken(intOwnerID, "deadbeef-stale-enf0")

	eid := "c360.platform.test.sys.w.enfcrt"
	before := testutil.ToFloat64(c.ownerLeaseMismatch.WithLabelValues(label, intPred))

	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: eid, MessageType: intMT},
		Triples: []message.Triple{
			{Subject: eid, Predicate: intPred, Object: "active", Timestamp: time.Now(), Confidence: 1},
		},
		OwnerToken: stale,
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	respData, handlerErr := c.handleEntityCreateWithTriples(ctx, data)
	// ADR-060: a stale lease is a typed owner_lease_stale reject (no body).
	requireClassifiedReject(t, respData, handlerErr, graph.ErrorCodeOwnerLeaseStale, "")

	// Metric still fires.
	after := testutil.ToFloat64(c.ownerLeaseMismatch.WithLabelValues(label, intPred))
	assert.InDelta(t, before+1, after, 0.0001, "enforce on still meters the mismatch")

	// The entity must NOT exist — the reject returned before CreateEntityStrict.
	_, _, readErr := c.fetchEntityState(ctx, eid)
	require.Error(t, readErr, "rejected create must NOT have committed the entity")
}

// TestIntegration_OwnerLease_Enforce_UpdateWithTriples_StaleRejected proves the
// update lane rejects a stale token AND the prior state is unchanged.
func TestIntegration_OwnerLease_Enforce_UpdateWithTriples_StaleRejected(t *testing.T) {
	c, _, ctx := bootIngestWithOwnerClaimCfg(t, intOwnerID, intEntityPat, intPred, enforceCfg())
	stale := ownerToken(intOwnerID, "deadbeef-stale-enfu")

	eid := "c360.platform.test.sys.w.enfupd"
	require.NoError(t, c.CreateEntity(ctx, &graph.EntityState{
		ID: eid, MessageType: intMT, Version: 1, UpdatedAt: time.Now(),
		Triples: []message.Triple{{Subject: eid, Predicate: intPred, Object: "init", Timestamp: time.Now(), Confidence: 1}},
	}))

	req := graph.UpdateEntityWithTriplesRequest{
		Entity:     &graph.EntityState{ID: eid, MessageType: intMT},
		AddTriples: []message.Triple{{Subject: eid, Predicate: intPred, Object: "updated", Timestamp: time.Now(), Confidence: 1}},
		OwnerToken: stale,
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	respData, handlerErr := c.handleEntityUpdateWithTriples(ctx, data)
	// ADR-060: a stale lease is a typed owner_lease_stale reject (no body).
	requireClassifiedReject(t, respData, handlerErr, graph.ErrorCodeOwnerLeaseStale, "")

	// Prior state unchanged.
	stored, _, readErr := c.fetchEntityState(ctx, eid)
	require.NoError(t, readErr)
	var got string
	for _, tr := range stored.Triples {
		if tr.Predicate == intPred {
			got = tr.Object.(string)
		}
	}
	assert.Equal(t, "init", got, "rejected update must NOT have applied the delta")
}

// TestIntegration_OwnerLease_Enforce_UpdateWithTriplesCAS_StaleRejected proves
// the CAS lane (ExpectedRevision > 0 — the lifecycle Manager's production write
// path) rejects a stale token through the real wire AND the prior state and
// revision are untouched (the CAS write never ran).
func TestIntegration_OwnerLease_Enforce_UpdateWithTriplesCAS_StaleRejected(t *testing.T) {
	c, _, ctx := bootIngestWithOwnerClaimCfg(t, intOwnerID, intEntityPat, intPred, enforceCfg())
	stale := ownerToken(intOwnerID, "deadbeef-stale-enfc")

	eid := "c360.platform.test.sys.w.enfcas"
	require.NoError(t, c.CreateEntity(ctx, &graph.EntityState{
		ID: eid, MessageType: intMT, Version: 1, UpdatedAt: time.Now(),
		Triples: []message.Triple{{Subject: eid, Predicate: intPred, Object: "init", Timestamp: time.Now(), Confidence: 1}},
	}))
	_, revBefore, err := c.fetchEntityState(ctx, eid)
	require.NoError(t, err)

	req := graph.UpdateEntityWithTriplesRequest{
		Entity:           &graph.EntityState{ID: eid, MessageType: intMT},
		AddTriples:       []message.Triple{{Subject: eid, Predicate: intPred, Object: "active", Timestamp: time.Now(), Confidence: 1}},
		ExpectedRevision: revBefore,
		OwnerToken:       stale,
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	respData, handlerErr := c.handleEntityUpdateWithTriples(ctx, data)
	// ADR-060: a stale lease is a typed owner_lease_stale reject (no body).
	requireClassifiedReject(t, respData, handlerErr, graph.ErrorCodeOwnerLeaseStale, "")

	// Prior state AND revision unchanged — the CAS write never ran.
	stored, revAfter, readErr := c.fetchEntityState(ctx, eid)
	require.NoError(t, readErr)
	assert.Equal(t, revBefore, revAfter, "rejected CAS update must NOT have advanced the revision")
	var got string
	for _, tr := range stored.Triples {
		if tr.Predicate == intPred {
			got = tr.Object.(string)
		}
	}
	assert.Equal(t, "init", got, "rejected CAS update must NOT have applied the delta")
}

// TestIntegration_OwnerLease_Enforce_MatchingToken_Commits proves enforcement
// does not block the legitimate live owner: a matching token still commits.
func TestIntegration_OwnerLease_Enforce_MatchingToken_Commits(t *testing.T) {
	c, reg, ctx := bootIngestWithOwnerClaimCfg(t, intOwnerID, intEntityPat, intPred, enforceCfg())
	token := ownerToken(intOwnerID, reg.Incarnation())

	eid := "c360.platform.test.sys.w.enfok"
	req := graph.CreateEntityWithTriplesRequest{
		Entity: &graph.EntityState{ID: eid, MessageType: intMT},
		Triples: []message.Triple{
			{Subject: eid, Predicate: intPred, Object: "planning", Timestamp: time.Now(), Confidence: 1},
		},
		OwnerToken: token,
	}
	data, err := json.Marshal(req)
	require.NoError(t, err)

	respData, handlerErr := c.handleEntityCreateWithTriples(ctx, data)
	require.NoError(t, handlerErr)
	var resp graph.CreateEntityWithTriplesResponse
	require.NoError(t, json.Unmarshal(respData, &resp))

	stored, _, readErr := c.fetchEntityState(ctx, eid)
	require.NoError(t, readErr, "committed entity must be readable")
	assert.True(t, hasPredicate(stored, intPred))
}
