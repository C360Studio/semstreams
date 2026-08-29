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
	"log/slog"
	"sync"
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
	logs       *authorityLogCapture
}

// authorityLogCapture records what the component logged. The requirement makes
// the WARN an operator surface — "a loud log naming the lane and the segment
// index, never the identity" — so it is asserted like any other output rather
// than discarded. Locking keeps -race clean regardless of who logs.
type authorityLogCapture struct {
	mu      sync.Mutex
	records []authorityLogRecord
}

type authorityLogRecord struct {
	level slog.Level
	msg   string
	attrs map[string]string
}

func (h *authorityLogCapture) Enabled(context.Context, slog.Level) bool { return true }

func (h *authorityLogCapture) Handle(_ context.Context, r slog.Record) error {
	captured := authorityLogRecord{level: r.Level, msg: r.Message, attrs: map[string]string{}}
	r.Attrs(func(a slog.Attr) bool {
		captured.attrs[a.Key] = a.Value.String()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, captured)
	h.mu.Unlock()
	return nil
}

func (h *authorityLogCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *authorityLogCapture) WithGroup(string) slog.Handler      { return h }

func (h *authorityLogCapture) withMessage(msg string) []authorityLogRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]authorityLogRecord, 0, len(h.records))
	for _, record := range h.records {
		if record.msg == msg {
			out = append(out, record)
		}
	}
	return out
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

	logs := &authorityLogCapture{}
	created, err := CreateGraphIngest(configJSON, component.Dependencies{
		NATSClient:      testClient.Client,
		PayloadRegistry: newTestPayloadRegistry(t),
		Platform:        component.PlatformMeta{Org: authorityOrg, Platform: authorityPlatform},
		Logger:          slog.New(logs),
	})
	require.NoError(t, err)

	c := created.(*Component)
	require.NoError(t, c.Initialize())
	registerMergeTestPayload(t, c) // decoder BEFORE Start (no consumer race)
	require.NoError(t, c.Start(ctx))
	require.NoError(t, testClient.GetNativeConnection().Flush())
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	return &authorityGateHarness{ctx: ctx, component: c, testClient: testClient, logs: logs}
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

// TestAuthorityGateRefusesForeignReconcileRegardlessOfExistence pins what the
// reconcile test above cannot: the verdict is decided from the IDENTITY alone
// and never from the entity's stored state. Asserting a code and an unchanged
// revision is equally consistent with reading the entity and then refusing.
//
// The probe is absence. A missing entity is observable ONLY through
// fetchEntityState, which reports it as entity_not_found. Reconciling a
// never-persisted FOREIGN id returns entity_id_authority_invalid instead; the
// identical request against a never-persisted LOCAL id DOES return
// entity_not_found. The local partner is what makes the foreign case evidence
// rather than coincidence: absence IS reachable and IS reported on this lane, so
// the foreign answer is not "no entity here" wearing a different code.
//
// What this does NOT show — and an earlier revision of this comment, and of the
// scenario naming it, both claimed it did — is that no KV read occurred. A gate
// that fetched the state FIRST and then authorized, discarding the fetch result
// when the authority check fails, would satisfy every assertion below.
// "Before any KV I/O" (ADR-102 d5) is a code-level invariant: authorizeSubject
// precedes fetchEntityState in every canonical handler. What this test defends
// is the regression that actually threatens that invariant — moving the
// authorization after the fetch and letting not-found win — which it kills.
func TestAuthorityGateRefusesForeignReconcileRegardlessOfExistence(t *testing.T) {
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

	// Negative space first: absence IS reachable and IS reported on this lane, so
	// the foreign answer below is a statement about which input decides the
	// verdict, and not merely about which error code outranks which.
	local := reconcileOf(t, authorityLocalAbsentID)
	require.Equal(t, graph.ErrorCodeEntityNotFound, local.Code,
		"a never-persisted LOCAL subject must reach fetchEntityState and be reported absent — "+
			"if this stops holding, the foreign assertion below stops proving anything")

	foreign := reconcileOf(t, authorityForeignAbsentID)
	assert.Equal(t, semtypes.ErrorCodeEntityIDAuthorityInvalid, foreign.Code,
		"the authority gate must answer for a foreign subject")
	assert.Equal(t, semtypes.EntityIDReasonForeignAuthority, foreign.Detail[semtypes.EntityIDDetailReason])
	assert.NotEqual(t, graph.ErrorCodeEntityNotFound, foreign.Code,
		"a foreign subject that does not exist must NOT be reported absent: the verdict does not "+
			"depend on whether the entity is there, which is what moving the authorization after "+
			"the fetch would break")
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

// TestAuthorityGateMetersDirectPersistenceRejectionsOnEveryDirectSeam closes
// review finding 4. The requirement covers direct persistence in the same
// breath as the other two lanes — "on every lane — Graphable fact arrival,
// every graph.mutation.> operation, and direct persistence" — and then says
// each rejection is metered exactly once and loudly logged. The direct guards
// previously only returned the classified error: a framework component calling
// CreateEntity or MergeEntity in-process with a foreign subject was refused
// correctly and invisibly, so an operator's `mutation_rejections` panel and
// their log stream both read as if nothing had been refused.
//
// It enumerates the direct seams from the guard sites rather than from the two
// public methods, because three of the five are reached through an adapter or a
// shared body and a test covering only the public pair would leave them open:
// the hierarchy inverse-edge adapter, the batch-append body, and the delete
// body all carry their own guard.
//
// The counter is process-wide (sync.Once), so every assertion is a DELTA.
func TestAuthorityGateMetersDirectPersistenceRejectionsOnEveryDirectSeam(t *testing.T) {
	h := startAuthorityGateComponent(t, false)

	counter := h.component.mutationRejections.WithLabelValues(arrivalDirect, authorityMetricReasonForeign)
	foreignEntity := func() *graph.EntityState {
		return &graph.EntityState{
			ID: authorityForeignID, MessageType: testEntityType(), Version: 1, UpdatedAt: time.Now(),
		}
	}
	foreignTriple := message.Triple{
		Subject: authorityForeignID, Predicate: semantictest.Predicate(t, "test", "fixture", "value"),
		Object: "v", Timestamp: time.Now(), Confidence: 1.0,
	}

	seams := []struct {
		name string
		call func() error
	}{
		{name: "CreateEntity", call: func() error {
			return h.component.CreateEntity(h.ctx, foreignEntity())
		}},
		{name: "MergeEntity", call: func() error {
			return h.component.MergeEntity(h.ctx, foreignEntity())
		}},
		{name: "hierarchy inverse-edge adapter", call: func() error {
			return (&tripleAdderAdapter{component: h.component}).AddTriple(h.ctx, foreignTriple)
		}},
		{name: "batch append body", call: func() error {
			_, err := h.component.addTriplesLane(h.ctx, []message.Triple{foreignTriple}, dedupLaneAddBatch)
			return err
		}},
		{name: "delete body", call: func() error {
			return h.component.deleteEntityAtRevision(h.ctx, authorityForeignID, 1)
		}},
	}

	for _, seam := range seams {
		t.Run(seam.name, func(t *testing.T) {
			before := testutil.ToFloat64(counter)
			logsBefore := len(h.logs.withMessage(authorityRejectionLogMessage))

			err := seam.call()
			require.Error(t, err, "a foreign subject must be refused on the direct lane")
			var classified *errs.ClassifiedError
			require.ErrorAs(t, err, &classified)
			assert.Equal(t, semtypes.ErrorCodeEntityIDAuthorityInvalid, classified.Code,
				"the caller still receives the classified error; metering does not replace it")

			assert.InDelta(t, before+1, testutil.ToFloat64(counter), 0.0001,
				"the direct-lane rejection is metered exactly once under arrival=%q", arrivalDirect)

			records := h.logs.withMessage(authorityRejectionLogMessage)
			require.Len(t, records, logsBefore+1, "exactly one loud log per refused direct call")
			record := records[len(records)-1]
			assert.Equal(t, slog.LevelWarn, record.level, "the log must be loud")
			assert.Equal(t, arrivalDirect, record.attrs["arrival"])
			assert.Equal(t, authorityMetricReasonForeign, record.attrs["reason"])
			assert.Equal(t, semtypes.EntityIDLaneLocal, record.attrs["lane"])
			assert.Equal(t, "1", record.attrs["segment_index"],
				"the log names the failing segment index — authorityForeignID shares the org and "+
					"differs at position 2 (platform), which is index 1")
			for key, value := range record.attrs {
				assert.NotContains(t, value, authorityForeignID,
					"the refused identity is not this deployment's to publish; attr %q leaked it", key)
			}
		})
	}
}
