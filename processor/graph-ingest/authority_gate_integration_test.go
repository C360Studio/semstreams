//go:build integration

// Task 2.5 (entity-id-segment-semantics, slice B): the deployment-authority
// gate. ADR-102 decision 5 — every graph boundary enforces the deployment's own
// org.platform on the candidate SUBJECT identity, on every lane, before any KV
// I/O, unless the write arrived on an input port the operator declared
// "import": true. `@id` OBJECTS are never authority-checked, so a local entity
// may cite an imported one. An import is a read-only mirror (ruled O-12(a)): no
// local lane mutates a foreign subject.
//
// These drive the assembled component against real NATS: the JetStream consume
// closure -> keyed pool -> processIngest for the fact lane, and the
// SubscribeForRequests-registered canonical handlers over the request/reply wire
// for the mutation lane. A handler-only test would not prove the lane the port
// declares reaches the gate.

package graphingest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/semantictest"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	semtypes "github.com/c360studio/semstreams/pkg/types"
)

const (
	// authorityOrg / authorityPlatform are the deployment's own authority for
	// every test in this file: deps.Platform, and nothing else, decides it.
	authorityOrg      = "acme"
	authorityPlatform = "dep1"

	// authorityForeignID is a peer deployment's own entity. It is structurally
	// canonical — only its positions 1-2 differ — so a structural rejection
	// here would prove nothing about the authority gate.
	authorityForeignID = "acme.dep2.src.git.commit.a1"
	// authorityForeignSiblingID shares authorityForeignID's five-position type
	// prefix. Sibling edges need no container, so with hierarchy enabled a
	// second import under the same prefix is the ONE path that would put a
	// hierarchy triple on an imported entity even while every container birth
	// is refused elsewhere — which is what makes the skip in
	// GetHierarchyTriples load-bearing rather than shadowed.
	authorityForeignSiblingID = "acme.dep2.src.git.commit.a2"
	// authorityLocalClaimID carries THIS deployment's pair and is what a peer
	// must not be able to mint through an import lane.
	authorityLocalClaimID = "acme.dep1.src.git.commit.a1"
	// authorityForeignAbsentID / authorityLocalAbsentID are never persisted by
	// any test. They are the ordering probe for "the rejection happens before
	// the entity's state is read": absence is observable ONLY through the fetch,
	// so which of the two errors comes back says whether the fetch was reached.
	authorityForeignAbsentID = "acme.dep2.src.git.commit.zz"
	authorityLocalAbsentID   = "acme.dep1.src.git.commit.zz"
	// authorityLocalLoopID is a local subject that references the import.
	authorityLocalLoopID = "acme.dep1.agentic-loop.agent.execution.a1b2c3d4"

	authorityImportStream  = "IMPORT_ENTITY"
	authorityImportSubject = "import.entity."

	// The two mutation lanes review round 1 (HIGH-2) found uncovered.
	authorityReconcileSubject = "graph.mutation.entity.reconcile"
	authorityDeleteSubject    = "graph.mutation.entity.delete"
)

// authorityGateHarness is the assembled component plus the test client, so a
// test can publish on either lane and read ENTITY_STATES directly.
type authorityGateHarness struct {
	ctx        context.Context
	component  *Component
	testClient *natsclient.TestClient
}

// startAuthorityGateComponent builds graph-ingest with TWO JetStream input
// ports — one ordinary, one declared "import": true — plus the required
// mutation-provider port, and gives it the deployment authority through
// deps.Platform.
func startAuthorityGateComponent(t *testing.T, enableHierarchy bool) *authorityGateHarness {
	t.Helper()
	ctx := context.Background()

	testClient := natsclient.NewTestClient(t,
		natsclient.WithKV(),
		natsclient.WithStreams(
			natsclient.TestStreamConfig{Name: "ENTITY", Subjects: []string{"entity.>"}},
			natsclient.TestStreamConfig{Name: authorityImportStream, Subjects: []string{"import.entity.>"}},
		),
	)

	config := DefaultConfig()
	config.EnableHierarchy = enableHierarchy
	config.Ports.Inputs = append(config.Ports.Inputs, component.PortDefinition{
		Name: "peer_import",
		Config: component.JetStreamPort{
			StreamName:    authorityImportStream,
			Subjects:      []string{"import.entity.>"},
			DeliverPolicy: "all",
			Import:        true,
		},
	})

	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	created, err := CreateGraphIngest(configJSON, component.Dependencies{
		NATSClient:      testClient.Client,
		PayloadRegistry: newTestPayloadRegistry(t),
		Platform:        component.PlatformMeta{Org: authorityOrg, Platform: authorityPlatform},
	})
	require.NoError(t, err)

	c := created.(*Component)
	require.NoError(t, c.Initialize())
	registerMergeTestPayload(t, c) // decoder BEFORE Start (no consumer race)
	require.NoError(t, c.Start(ctx))
	require.NoError(t, testClient.GetNativeConnection().Flush())
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	return &authorityGateHarness{ctx: ctx, component: c, testClient: testClient}
}

// publishFact publishes a Graphable on the given subject prefix and returns
// once the publish is acked. The caller decides what to assert about arrival.
func (h *authorityGateHarness) publishFact(t *testing.T, subjectPrefix, entityID string) {
	t.Helper()
	payload := &mergeTestGraphable{
		entityID: entityID,
		triples: []message.Triple{{
			Subject: entityID, Predicate: semantictest.Predicate(t, "test", "fixture", "value"),
			Object: "v", Timestamp: time.Now(), Confidence: 1.0,
		}},
	}
	baseMsg := message.NewBaseMessage(payload.Schema(), payload, "peer-source")
	data, err := json.Marshal(baseMsg)
	require.NoError(t, err)
	require.NoError(t, h.testClient.Client.PublishToStream(h.ctx, subjectPrefix+entityID, data))
}

func (h *authorityGateHarness) absent(t *testing.T, entityID string) {
	t.Helper()
	_, err := h.component.entityBucket.Get(h.ctx, entityID)
	require.Error(t, err, "nothing may be persisted for %q", entityID)
	assert.True(t, errors.Is(err, natsclient.ErrKVKeyNotFound))
}

func (h *authorityGateHarness) awaitEntity(t *testing.T, entityID string) *graph.EntityState {
	t.Helper()
	require.Eventually(t, func() bool {
		stored, _, err := h.component.fetchEntityState(h.ctx, entityID)
		return err == nil && stored != nil
	}, 5*time.Second, 20*time.Millisecond, "entity %q never landed in ENTITY_STATES", entityID)
	stored, _, err := h.component.fetchEntityState(h.ctx, entityID)
	require.NoError(t, err)
	return stored
}

// TestAuthorityGateRejectsForeignOnFactLane — a peer's own entity arriving on a
// port that is NOT an import lane never reaches ENTITY_STATES, and the
// rejection is metered exactly once under authority_foreign.
func TestAuthorityGateRejectsForeignOnFactLane(t *testing.T) {
	h := startAuthorityGateComponent(t, false)

	counter := h.component.mutationRejections.WithLabelValues("entity.>", authorityMetricReasonForeign)
	before := testutil.ToFloat64(counter)

	h.publishFact(t, "entity.", authorityForeignID)

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(counter) == before+1
	}, 5*time.Second, 20*time.Millisecond,
		"mutation_rejections{reason=%q} must increment exactly once", authorityMetricReasonForeign)

	h.absent(t, authorityForeignID)
	assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
		"the rejection is metered once, not once per retry")
}

// TestAuthorityGateRejectsForeignOnMutationLane — the same foreign subject over
// the real graph.mutation.> request/reply wire returns the coded authority
// error, decoded into a fresh value, and NOT the structural code.
func TestAuthorityGateRejectsForeignOnMutationLane(t *testing.T) {
	h := startAuthorityGateComponent(t, false)

	reqBytes, err := json.Marshal(graph.CreateEntityRequest{
		Entity: &graph.EntityState{
			ID:          authorityForeignID,
			MessageType: testEntityType(),
			Version:     1,
			UpdatedAt:   time.Now(),
		},
	})
	require.NoError(t, err)

	respBytes, err := h.component.natsClient.RequestClassified(
		h.ctx, structuralCreateSubject, reqBytes, 2*time.Second)
	require.Error(t, err, "a foreign subject on the mutation lane must be refused")
	assert.Nil(t, respBytes)

	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, semtypes.ErrorCodeEntityIDAuthorityInvalid, classified.Code,
		"authority rejection is coded distinctly from a structural one")
	assert.NotEqual(t, semtypes.ErrorCodeEntityIDInvalid, classified.Code,
		"a structurally canonical candidate must never be reported as entity_id_invalid")

	h.absent(t, authorityForeignID)
}

// TestAuthorityGateAllowsForeignReferenceObject — @id OBJECTS are not
// authority-checked. A local entity may cite an imported one; the reference
// persists byte-for-byte and no stub entity is created for the target.
func TestAuthorityGateAllowsForeignReferenceObject(t *testing.T) {
	h := startAuthorityGateComponent(t, false)

	// Import the peer entity first, through the declared import lane.
	h.publishFact(t, authorityImportSubject, authorityForeignID)
	h.awaitEntity(t, authorityForeignID)

	reference := message.Triple{
		Subject:    authorityLocalLoopID,
		Predicate:  semantictest.Predicate(t, "test", "fixture", "origin"),
		Object:     authorityForeignID,
		Datatype:   message.EntityReferenceDatatype,
		Timestamp:  time.Now(),
		Confidence: 1.0,
	}
	reqBytes, err := json.Marshal(graph.CreateEntityRequest{
		Entity: &graph.EntityState{
			ID:          authorityLocalLoopID,
			MessageType: testEntityType(),
			Version:     1,
			UpdatedAt:   time.Now(),
		},
		Triples: []message.Triple{reference},
	})
	require.NoError(t, err)

	respBytes, err := h.component.natsClient.RequestClassified(
		h.ctx, structuralCreateSubject, reqBytes, 2*time.Second)
	require.NoError(t, err, "a local subject citing a foreign @id object must be accepted")

	var response graph.CreateEntityResponse
	require.NoError(t, json.Unmarshal(respBytes, &response))
	assert.Equal(t, graph.MutationApplied, response.Outcome)

	stored := h.awaitEntity(t, authorityLocalLoopID)
	found := false
	for _, tr := range stored.Triples {
		if tr.Predicate == reference.Predicate {
			found = true
			assert.Equal(t, authorityForeignID, tr.Object,
				"the reference persists unchanged — identity is never rewritten")
			assert.Equal(t, message.EntityReferenceDatatype, tr.Datatype)
		}
	}
	assert.True(t, found, "the @id reference triple must be persisted")
}

// TestImportLaneAcceptsForeignRejectsLocalClaim — the import lane inverts the
// test: a foreign subject is persisted with its bytes unchanged, and a subject
// claiming THIS deployment's pair is refused with local_authority_claimed.
func TestImportLaneAcceptsForeignRejectsLocalClaim(t *testing.T) {
	h := startAuthorityGateComponent(t, false)

	h.publishFact(t, authorityImportSubject, authorityForeignID)
	stored := h.awaitEntity(t, authorityForeignID)
	assert.Equal(t, authorityForeignID, stored.ID,
		"an imported identity is persisted verbatim, never rewritten to the local authority")

	counter := h.component.mutationRejections.WithLabelValues(
		"import.entity.>", authorityMetricReasonClaimed)
	before := testutil.ToFloat64(counter)

	h.publishFact(t, authorityImportSubject, authorityLocalClaimID)

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(counter) == before+1
	}, 5*time.Second, 20*time.Millisecond,
		"a peer claiming the local authority must be refused with %q", authorityMetricReasonClaimed)
	h.absent(t, authorityLocalClaimID)
}

// TestHierarchySkipsForeignAuthority — the framework never mints under a
// foreign authority, so an imported entity gets no container, no membership
// triple, and no inverse sibling edge, on the import lane as on any other.
func TestHierarchySkipsForeignAuthority(t *testing.T) {
	h := startAuthorityGateComponent(t, true)

	// Two imports sharing a type prefix: the second would take the sibling-edge
	// path, which mints a forward hierarchy triple onto the entity itself
	// without needing a container.
	h.publishFact(t, authorityImportSubject, authorityForeignID)
	h.awaitEntity(t, authorityForeignID)
	h.publishFact(t, authorityImportSubject, authorityForeignSiblingID)
	h.awaitEntity(t, authorityForeignSiblingID)

	for _, id := range []string{authorityForeignID, authorityForeignSiblingID} {
		for _, tr := range h.awaitEntity(t, id).Triples {
			assert.NotContains(t, tr.Predicate, "hierarchy.",
				"imported entity %q must carry no hierarchy triple (got %q)", id, tr.Predicate)
		}
	}
	// No container was born under the peer's authority.
	parsed, err := semtypes.ParseEntityID(authorityForeignID)
	require.NoError(t, err)
	for _, containerID := range []string{
		parsed.TypePrefix() + ".group",
		parsed.TaxonomyPrefix() + ".group.container",
		parsed.SourcePrefix() + ".group.container.level",
	} {
		h.absent(t, containerID)
	}
}

// TestAuthorityGateRejectsAnnotationOfImportedSubject — an import is a
// READ-ONLY mirror (ruled O-12(a)). A triple.append from a local lane naming
// the imported subject is refused and the import's revision does not move.
func TestAuthorityGateRejectsAnnotationOfImportedSubject(t *testing.T) {
	h := startAuthorityGateComponent(t, false)

	h.publishFact(t, authorityImportSubject, authorityForeignID)
	h.awaitEntity(t, authorityForeignID)

	entry, err := h.component.entityBucket.Get(h.ctx, authorityForeignID)
	require.NoError(t, err)
	revisionBefore := entry.Revision

	reqBytes, err := json.Marshal(graph.AppendTriplesRequest{Triples: []message.Triple{{
		Subject:   authorityForeignID,
		Predicate: semantictest.Predicate(t, "test", "fixture", "annotation"),
		Object:    "curated", Timestamp: time.Now(), Confidence: 1.0,
	}}})
	require.NoError(t, err)

	respBytes, err := h.component.natsClient.RequestClassified(
		h.ctx, structuralAppendSubject, reqBytes, 2*time.Second)
	require.Error(t, err, "no local lane may mutate a foreign subject")
	assert.Nil(t, respBytes)

	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, semtypes.ErrorCodeEntityIDAuthorityInvalid, classified.Code)
	assert.Equal(t, semtypes.EntityIDReasonForeignAuthority, classified.Detail[semtypes.EntityIDDetailReason])

	after, err := h.component.entityBucket.Get(h.ctx, authorityForeignID)
	require.NoError(t, err)
	assert.Equal(t, revisionBefore, after.Revision,
		"the import's revision must be untouched by the refused annotation")
}

// TestAuthorityGateRejectsReconcileOfImportedSubject closes the lane with the
// LEAST margin for error: handleCanonicalReconcile writes straight through
// entityBucket.Update and never enters mergeEntityOnLane, so its own gate is
// the ONLY thing between a local reconcile and mutation of an imported mirror
// — there is no backstop behind it. It is also the lane pkg/lifecycle.Manager
// writes through, so an unguarded reconcile would let any lifecycle participant
// attach itself to a peer's entity.
//
// Deleting that gate previously left both suites green (review HIGH-2).
func TestAuthorityGateRejectsReconcileOfImportedSubject(t *testing.T) {
	h := startAuthorityGateComponent(t, false)

	h.publishFact(t, authorityImportSubject, authorityForeignID)
	h.awaitEntity(t, authorityForeignID)

	entry, err := h.component.entityBucket.Get(h.ctx, authorityForeignID)
	require.NoError(t, err)
	revisionBefore := entry.Revision

	reqBytes, err := json.Marshal(graph.ReconcilePredicatesRequest{
		EntityID:         authorityForeignID,
		ExpectedRevision: revisionBefore,
		Predicates:       []string{semantictest.Predicate(t, "test", "fixture", "curation")},
		Desired: []message.Triple{{
			Subject:   authorityForeignID,
			Predicate: semantictest.Predicate(t, "test", "fixture", "curation"),
			Object:    "curated", Timestamp: time.Now(), Confidence: 1.0,
		}},
	})
	require.NoError(t, err)

	respBytes, err := h.component.natsClient.RequestClassified(
		h.ctx, authorityReconcileSubject, reqBytes, 2*time.Second)
	require.Error(t, err, "reconcile is a mutation; no local lane may reconcile a foreign subject")
	assert.Nil(t, respBytes)

	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, semtypes.ErrorCodeEntityIDAuthorityInvalid, classified.Code)
	assert.Equal(t, semtypes.EntityIDReasonForeignAuthority, classified.Detail[semtypes.EntityIDDetailReason])

	after, err := h.component.entityBucket.Get(h.ctx, authorityForeignID)
	require.NoError(t, err)
	assert.Equal(t, revisionBefore, after.Revision,
		"the import's revision must be untouched by the refused reconcile")
}

// TestAuthorityGateRejectsReconcileBeforeReadingState proves the ordering half
// of ADR-102 d5 — "before any KV I/O" — which the reconcile test above states
// but cannot show: asserting the error code and an unchanged revision is equally
// consistent with reading the entity and then refusing.
//
// The probe is absence. A missing entity is observable ONLY through
// fetchEntityState, which reports it as entity_not_found, so the fetch is the
// one step whose execution leaves a distinguishable trace. Reconciling a
// never-persisted FOREIGN id returns entity_id_authority_invalid, not
// entity_not_found — the fetch never ran.
//
// The local partner is what makes that evidence rather than coincidence: the
// identical request against a never-persisted LOCAL id DOES return
// entity_not_found, so reaching the fetch demonstrably produces a different
// outcome. Without it the foreign case would only show that an authority error
// outranks a not-found error, which is not an ordering claim at all.
func TestAuthorityGateRejectsReconcileBeforeReadingState(t *testing.T) {
	h := startAuthorityGateComponent(t, false)

	reconcileOf := func(t *testing.T, entityID string) *errs.ClassifiedError {
		t.Helper()
		reqBytes, err := json.Marshal(graph.ReconcilePredicatesRequest{
			EntityID:         entityID,
			ExpectedRevision: 1,
			Predicates:       []string{semantictest.Predicate(t, "test", "fixture", "curation")},
			Desired: []message.Triple{{
				Subject:   entityID,
				Predicate: semantictest.Predicate(t, "test", "fixture", "curation"),
				Object:    "curated", Timestamp: time.Now(), Confidence: 1.0,
			}},
		})
		require.NoError(t, err)

		respBytes, err := h.component.natsClient.RequestClassified(
			h.ctx, authorityReconcileSubject, reqBytes, 2*time.Second)
		require.Error(t, err)
		assert.Nil(t, respBytes)
		var classified *errs.ClassifiedError
		require.ErrorAs(t, err, &classified)
		return classified
	}

	// Negative space first: absence IS reachable and IS reported, so the code
	// below is a statement about ordering and not about which error wins.
	local := reconcileOf(t, authorityLocalAbsentID)
	require.Equal(t, graph.ErrorCodeEntityNotFound, local.Code,
		"a never-persisted LOCAL subject must reach fetchEntityState and be reported absent — "+
			"if this stops holding, the foreign assertion below stops proving anything")

	foreign := reconcileOf(t, authorityForeignAbsentID)
	assert.Equal(t, semtypes.ErrorCodeEntityIDAuthorityInvalid, foreign.Code,
		"the authority gate must answer for a foreign subject")
	assert.Equal(t, semtypes.EntityIDReasonForeignAuthority, foreign.Detail[semtypes.EntityIDDetailReason])
	assert.NotEqual(t, graph.ErrorCodeEntityNotFound, foreign.Code,
		"a foreign subject that does not exist must NOT be reported absent: learning it is absent "+
			"requires the KV read the gate is specified to precede")
}

// TestAuthorityGateRejectsDeleteOfImportedSubject pins the delete lane. An
// import is a read-only MIRROR, not local property: reclaiming a peer's entity
// is as much a mutation as annotating it, and the mirror must outlive any local
// decision about it. Deleting BOTH delete-lane gates previously left the suites
// green (review HIGH-2).
func TestAuthorityGateRejectsDeleteOfImportedSubject(t *testing.T) {
	h := startAuthorityGateComponent(t, false)

	h.publishFact(t, authorityImportSubject, authorityForeignID)
	h.awaitEntity(t, authorityForeignID)

	entry, err := h.component.entityBucket.Get(h.ctx, authorityForeignID)
	require.NoError(t, err)
	revisionBefore := entry.Revision

	reqBytes, err := json.Marshal(graph.DeleteEntityRequest{
		EntityID:         authorityForeignID,
		ExpectedRevision: revisionBefore,
	})
	require.NoError(t, err)

	respBytes, err := h.component.natsClient.RequestClassified(
		h.ctx, authorityDeleteSubject, reqBytes, 2*time.Second)
	require.Error(t, err, "no local lane may delete a foreign subject")
	assert.Nil(t, respBytes)

	var classified *errs.ClassifiedError
	require.ErrorAs(t, err, &classified)
	assert.Equal(t, semtypes.ErrorCodeEntityIDAuthorityInvalid, classified.Code)
	assert.Equal(t, semtypes.EntityIDReasonForeignAuthority, classified.Detail[semtypes.EntityIDDetailReason])

	// The mirror survives, byte-for-byte and at the same revision.
	after, err := h.component.entityBucket.Get(h.ctx, authorityForeignID)
	require.NoError(t, err)
	assert.Equal(t, revisionBefore, after.Revision,
		"the import must still exist at its original revision after the refused delete")
}
