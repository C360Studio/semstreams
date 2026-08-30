package graphingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/internal/graphmutation"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCanonicalMutationRoutesComeOnlyFromTypedProvider(t *testing.T) {
	c := createTestComponentWithMockKV(t, withAuthority("acme", "ops"))
	routes, err := c.canonicalMutationRoutes()
	if err != nil {
		t.Fatalf("canonicalMutationRoutes: %v", err)
	}
	want := map[graphmutation.Operation]string{
		graphmutation.CreateEntity:        "graph.mutation.entity.create",
		graphmutation.ReconcilePredicates: "graph.mutation.entity.reconcile",
		graphmutation.AppendTriples:       "graph.mutation.triple.append",
		graphmutation.DeleteEntity:        "graph.mutation.entity.delete",
	}
	if len(routes) != len(want) {
		t.Fatalf("routes = %#v", routes)
	}
	for _, route := range routes {
		if route.subject != want[route.operation] {
			t.Fatalf("route %q = %q, want %q", route.operation, route.subject, want[route.operation])
		}
		delete(want, route.operation)
	}
	if len(want) != 0 {
		t.Fatalf("missing routes = %v", want)
	}
}

func TestCanonicalMutationRoutesRejectLegacyCompanionPort(t *testing.T) {
	c := createTestComponentWithMockKV(t, withAuthority("acme", "ops"))
	c.config.Ports.Inputs = append(c.config.Ports.Inputs, component.PortDefinition{
		Name: "legacy_mutations", Config: component.NATSRequestPort{Subject: "graph.mutation.*"}, Required: true,
	})
	if routes, err := c.canonicalMutationRoutes(); err == nil {
		t.Fatalf("legacy mutation port was accepted beside canonical routes: %#v", routes)
	}
}

const (
	canonicalEntityA = "acme.ops.robotics.gcs.drone.001"
	canonicalEntityB = "acme.ops.robotics.gcs.sensor.002"
)

func TestCanonicalCreateHasNoHierarchyOrRelationshipSideEffects(t *testing.T) {
	c, bucket := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
	c.config.EnableHierarchy = true

	request := graph.CreateEntityRequest{
		Entity: canonicalMutationEntity(canonicalEntityA),
		Triples: []message.Triple{
			canonicalTriple(canonicalEntityA, "test.state.value", "ready"),
			{
				Subject: canonicalEntityA, Predicate: "test.link.target", Object: canonicalEntityB,
				Datatype: message.EntityReferenceDatatype, Source: "canonical-test",
			},
		},
		RequestID: "create-1",
	}
	responseData, err := c.handleCanonicalCreate(context.Background(), mustCanonicalJSON(t, request))
	if err != nil {
		t.Fatalf("handleCanonicalCreate: %v", err)
	}
	var response graph.CreateEntityResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Outcome != graph.MutationApplied || response.Entity == nil || response.KVRevision == 0 {
		t.Fatalf("response = %#v", response)
	}

	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	if len(bucket.data) != 1 {
		t.Fatalf("authority keys = %v, want only caller entity", bucket.data)
	}
	if _, exists := bucket.data[canonicalEntityB]; exists {
		t.Fatal("absent relationship target was created")
	}
	if _, exists := bucket.data["acme.ops.robotics.gcs.hierarchy.drone"]; exists {
		t.Fatal("RPC create manufactured a hierarchy container")
	}
}

func TestCanonicalCreateAllowsTriplelessEntity(t *testing.T) {
	c, _ := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))

	request := graph.CreateEntityRequest{
		Entity:    canonicalMutationEntity(canonicalEntityA),
		Triples:   []message.Triple{},
		RequestID: "create-tripleless",
	}
	responseData, err := c.handleCanonicalCreate(context.Background(), mustCanonicalJSON(t, request))
	if err != nil {
		t.Fatalf("handleCanonicalCreate: %v", err)
	}
	var response graph.CreateEntityResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Outcome != graph.MutationApplied || response.Entity == nil || response.KVRevision == 0 {
		t.Fatalf("response = %#v", response)
	}
	stored, revision, err := c.fetchEntityState(context.Background(), canonicalEntityA)
	if err != nil {
		t.Fatalf("fetchEntityState: %v", err)
	}
	if revision != response.KVRevision || stored.ID != canonicalEntityA {
		t.Fatalf("stored = %#v at revision %d, response revision %d", stored, revision, response.KVRevision)
	}
	for _, triple := range stored.Triples {
		if triple.Predicate != vocabulary.EntityIndexingProfile {
			t.Fatalf("triple-less create stored caller fact %#v", triple)
		}
	}
	if err := graph.ValidateEntityStateContract(stored); err != nil {
		t.Fatalf("stored triple-less entity is not canonical: %v", err)
	}
}

func TestCanonicalCreateRejectsLegacyNestedTriplesAndUnknownFields(t *testing.T) {
	c, bucket := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
	entity := canonicalMutationEntity(canonicalEntityA)
	entity.Triples = []message.Triple{canonicalTriple(canonicalEntityA, "test.state.value", "legacy")}
	legacy := mustCanonicalJSON(t, map[string]any{"entity": entity})
	_, err := c.handleCanonicalCreate(context.Background(), legacy)
	assertCanonicalCode(t, err, graph.ErrorCodeInvalidRequest)

	canonical := graph.CreateEntityRequest{
		Entity: canonicalMutationEntity(canonicalEntityA), Triples: []message.Triple{},
	}
	_, err = c.handleCanonicalCreate(context.Background(), withCanonicalField(t, canonical, "owner_token", "retired"))
	assertCanonicalCode(t, err, graph.ErrorCodeInvalidRequest)

	bucket.mu.Lock()
	defer bucket.mu.Unlock()
	if len(bucket.data) != 0 {
		t.Fatalf("rejected create reached authority: %v", bucket.data)
	}
}

func TestCanonicalHandlersRejectUnknownFieldsBeforeAuthorityIO(t *testing.T) {
	triple := canonicalTriple(canonicalEntityA, "test.state.value", "ready")
	tests := []struct {
		name    string
		request any
		run     func(*Component, context.Context, []byte) ([]byte, error)
	}{
		{name: "create", request: graph.CreateEntityRequest{Entity: canonicalMutationEntity(canonicalEntityA), Triples: []message.Triple{}}, run: (*Component).handleCanonicalCreate},
		{name: "reconcile", request: graph.ReconcilePredicatesRequest{EntityID: canonicalEntityA, ExpectedRevision: 1, Predicates: []string{triple.Predicate}, Desired: []message.Triple{triple}}, run: (*Component).handleCanonicalReconcile},
		{name: "append", request: graph.AppendTriplesRequest{Triples: []message.Triple{triple}}, run: (*Component).handleCanonicalAppend},
		{name: "delete", request: graph.DeleteEntityRequest{EntityID: canonicalEntityA, ExpectedRevision: 1}, run: (*Component).handleCanonicalDelete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, bucket := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
			var calls atomic.Int32
			bucket.createFunc = func(context.Context, string, []byte, ...jetstream.KVCreateOpt) (uint64, error) {
				calls.Add(1)
				return 1, nil
			}
			bucket.getFunc = func(context.Context, string) (jetstream.KeyValueEntry, error) {
				calls.Add(1)
				return nil, jetstream.ErrKeyNotFound
			}
			_, err := tt.run(c, context.Background(), withCanonicalField(t, tt.request, "owner_token", "retired"))
			assertCanonicalCode(t, err, graph.ErrorCodeInvalidRequest)
			if calls.Load() != 0 {
				t.Fatalf("unknown field reached authority %d times", calls.Load())
			}
		})
	}
}

func TestCanonicalReconcileUnchangedDoesNotAdvanceRevision(t *testing.T) {
	c, bucket := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
	created := createCanonicalEntity(t, c, canonicalEntityA, []message.Triple{
		canonicalTriple(canonicalEntityA, "test.state.value", "ready"),
		canonicalTriple(canonicalEntityA, "test.state.sibling", "keep"),
	})

	request := graph.ReconcilePredicatesRequest{
		EntityID: canonicalEntityA, ExpectedRevision: created.KVRevision,
		Predicates: []string{"test.state.value"},
		Desired:    []message.Triple{canonicalTriple(canonicalEntityA, "test.state.value", "ready")},
	}
	responseData, err := c.handleCanonicalReconcile(context.Background(), mustCanonicalJSON(t, request))
	if err != nil {
		t.Fatalf("handleCanonicalReconcile: %v", err)
	}
	var response graph.ReconcilePredicatesResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Outcome != graph.MutationUnchanged || response.KVRevision != created.KVRevision {
		t.Fatalf("response = %#v", response)
	}
	entry, err := bucket.Get(context.Background(), canonicalEntityA)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry.Revision() != created.KVRevision {
		t.Fatalf("revision advanced on no-op: got %d want %d", entry.Revision(), created.KVRevision)
	}
}

func TestCanonicalReconcileCompetingCASAllowsOneWinner(t *testing.T) {
	c, _ := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
	created := createCanonicalEntity(t, c, canonicalEntityA, []message.Triple{
		canonicalTriple(canonicalEntityA, "test.state.value", "before"),
	})

	request := func(value string) graph.ReconcilePredicatesRequest {
		return graph.ReconcilePredicatesRequest{
			EntityID: canonicalEntityA, ExpectedRevision: created.KVRevision,
			Predicates: []string{"test.state.value"},
			Desired:    []message.Triple{canonicalTriple(canonicalEntityA, "test.state.value", value)},
		}
	}
	if _, err := c.handleCanonicalReconcile(context.Background(), mustCanonicalJSON(t, request("winner"))); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	_, err := c.handleCanonicalReconcile(context.Background(), mustCanonicalJSON(t, request("loser")))
	assertCanonicalCode(t, err, graph.ErrorCodeRevisionMismatch)

	stored, _, err := c.fetchEntityState(context.Background(), canonicalEntityA)
	if err != nil {
		t.Fatalf("fetchEntityState: %v", err)
	}
	if value, ok := stored.GetPropertyValue("test.state.value"); !ok || value != "winner" {
		t.Fatalf("stored value = %#v, %v", value, ok)
	}
}

func TestCanonicalAppendRacesGraphableMergeWithoutLosingFacts(t *testing.T) {
	c, _ := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
	created := createCanonicalEntity(t, c, canonicalEntityA, []message.Triple{
		canonicalTriple(canonicalEntityA, "test.state.seed", "present"),
	})
	if created.KVRevision == 0 {
		t.Fatal("create returned zero revision")
	}
	appendRequest := mustCanonicalJSON(t, graph.AppendTriplesRequest{Triples: []message.Triple{
		canonicalTriple(canonicalEntityA, "test.state.appended", "request"),
	}})

	start := make(chan struct{})
	errorsOut := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		<-start
		errorsOut <- c.MergeEntity(context.Background(), &graph.EntityState{
			ID:          canonicalEntityA,
			MessageType: canonicalMutationEntity(canonicalEntityA).MessageType,
			Triples: []message.Triple{
				canonicalTriple(canonicalEntityA, "test.state.graphable", "stream"),
			},
		})
	}()
	go func() {
		defer workers.Done()
		<-start
		_, err := c.handleCanonicalAppend(context.Background(), appendRequest)
		errorsOut <- err
	}()
	close(start)
	workers.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Fatalf("concurrent write: %v", err)
		}
	}

	stored, _, err := c.fetchEntityState(context.Background(), canonicalEntityA)
	if err != nil {
		t.Fatalf("fetchEntityState: %v", err)
	}
	for predicate, want := range map[string]any{
		"test.state.seed":      "present",
		"test.state.graphable": "stream",
		"test.state.appended":  "request",
	} {
		got, ok := stored.GetPropertyValue(predicate)
		if !ok || got != want {
			t.Fatalf("predicate %q = %#v, %v; want %#v", predicate, got, ok, want)
		}
	}
}

func TestCanonicalReconcileEmptyDesiredRemovesOnlySelectedPredicates(t *testing.T) {
	c, _ := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
	selected := canonicalTriple(canonicalEntityA, "test.state.value", "remove")
	sibling := canonicalTriple(canonicalEntityA, "test.state.sibling", "keep")
	created := createCanonicalEntity(t, c, canonicalEntityA, []message.Triple{selected, sibling})

	responseData, err := c.handleCanonicalReconcile(context.Background(), mustCanonicalJSON(t,
		graph.ReconcilePredicatesRequest{
			EntityID: canonicalEntityA, ExpectedRevision: created.KVRevision,
			Predicates: []string{selected.Predicate}, Desired: []message.Triple{},
		}))
	if err != nil {
		t.Fatalf("handleCanonicalReconcile: %v", err)
	}
	var response graph.ReconcilePredicatesResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Outcome != graph.MutationApplied || response.KVRevision == created.KVRevision {
		t.Fatalf("response = %#v", response)
	}
	for _, triple := range response.Entity.Triples {
		if triple.Predicate == selected.Predicate {
			t.Fatalf("selected predicate survived: %#v", triple)
		}
	}
	if value, ok := response.Entity.GetPropertyValue(sibling.Predicate); !ok || value != sibling.Object {
		t.Fatalf("sibling = %#v, %v", value, ok)
	}
}

func TestCanonicalReconcileAnnotationOnlyChangeAppliesThenExactRepeatIsUnchanged(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*message.Triple)
	}{
		{name: "confidence", mutate: func(triple *message.Triple) { triple.Confidence = 0.75 }},
		{name: "timestamp", mutate: func(triple *message.Triple) {
			triple.Timestamp = time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC)
		}},
		{name: "expiry", mutate: func(triple *message.Triple) {
			expires := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
			triple.ExpiresAt = &expires
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
			original := canonicalTriple(canonicalEntityA, "test.state.value", "same-object")
			created := createCanonicalEntity(t, c, canonicalEntityA, []message.Triple{original})
			desired := original
			tt.mutate(&desired)

			request := graph.ReconcilePredicatesRequest{
				EntityID: canonicalEntityA, ExpectedRevision: created.KVRevision,
				Predicates: []string{original.Predicate}, Desired: []message.Triple{desired},
			}
			data, err := c.handleCanonicalReconcile(context.Background(), mustCanonicalJSON(t, request))
			if err != nil {
				t.Fatalf("reconcile changed field: %v", err)
			}
			var applied graph.ReconcilePredicatesResponse
			if err := json.Unmarshal(data, &applied); err != nil {
				t.Fatalf("decode applied: %v", err)
			}
			if applied.Outcome != graph.MutationApplied || applied.KVRevision == created.KVRevision {
				t.Fatalf("applied = %#v", applied)
			}

			request.ExpectedRevision = applied.KVRevision
			data, err = c.handleCanonicalReconcile(context.Background(), mustCanonicalJSON(t, request))
			if err != nil {
				t.Fatalf("reconcile exact repeat: %v", err)
			}
			var unchanged graph.ReconcilePredicatesResponse
			if err := json.Unmarshal(data, &unchanged); err != nil {
				t.Fatalf("decode unchanged: %v", err)
			}
			if unchanged.Outcome != graph.MutationUnchanged || unchanged.KVRevision != applied.KVRevision {
				t.Fatalf("unchanged = %#v", unchanged)
			}
		})
	}
}

func TestCanonicalAppendReportsPartialAndDoesNotBirthAbsentSubject(t *testing.T) {
	c, bucket := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
	createCanonicalEntity(t, c, canonicalEntityA, nil)

	request := graph.AppendTriplesRequest{Triples: []message.Triple{
		canonicalTriple(canonicalEntityA, "test.evidence.value", "present"),
		canonicalTriple(canonicalEntityB, "test.evidence.value", "absent"),
	}}
	responseData, err := c.handleCanonicalAppend(context.Background(), mustCanonicalJSON(t, request))
	if err != nil {
		t.Fatalf("handleCanonicalAppend: %v", err)
	}
	var response graph.AppendTriplesResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Results) != 2 {
		t.Fatalf("results = %#v", response.Results)
	}
	if response.Results[0].EntityID != canonicalEntityA || response.Results[0].Outcome != graph.MutationApplied || response.Results[0].KVRevision == 0 {
		t.Fatalf("applied result = %#v", response.Results[0])
	}
	if response.Results[1].EntityID != canonicalEntityB || response.Results[1].Outcome != graph.MutationEntityNotFound {
		t.Fatalf("absent result = %#v", response.Results[1])
	}
	if _, err := bucket.Get(context.Background(), canonicalEntityB); !errors.Is(err, jetstream.ErrKeyNotFound) {
		t.Fatalf("absent subject read = %v, want not found", err)
	}
}

func TestCanonicalAppendReturnsBatchCancellation(t *testing.T) {
	c, _ := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
	createCanonicalEntity(t, c, canonicalEntityA, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	data, err := c.handleCanonicalAppend(ctx, mustCanonicalJSON(t, graph.AppendTriplesRequest{
		Triples: []message.Triple{
			canonicalTriple(canonicalEntityA, "test.evidence.value", "not-written"),
		},
	}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("handleCanonicalAppend error = %v, want context.Canceled", err)
	}
	if len(data) != 0 {
		t.Fatalf("canceled batch response = %s, want no fabricated subject result", data)
	}
	stored, _, fetchErr := c.fetchEntityState(context.Background(), canonicalEntityA)
	if fetchErr != nil {
		t.Fatalf("fetch entity: %v", fetchErr)
	}
	for _, triple := range stored.Triples {
		if triple.Predicate == "test.evidence.value" && triple.Object == "not-written" {
			t.Fatal("canceled append mutated authority")
		}
	}
}

func TestCanonicalAppendAccountingRequiresExactlyOneOutcomePerSubject(t *testing.T) {
	t.Parallel()

	subjects := []string{canonicalEntityA, canonicalEntityB}
	valid := addTriplesResult{
		CommittedRevisions: map[string]uint64{canonicalEntityA: 2},
		UnchangedSubjects:  map[string]struct{}{canonicalEntityB: {}},
	}
	if err := validateCanonicalAppendAccounting(subjects, valid); err != nil {
		t.Fatalf("valid accounting: %v", err)
	}
	missing := valid
	missing.UnchangedSubjects = nil
	if err := validateCanonicalAppendAccounting(subjects, missing); err == nil {
		t.Fatal("missing subject outcome was accepted")
	}
	ambiguous := valid
	ambiguous.NotFoundSubjects = map[string]struct{}{canonicalEntityA: {}}
	if err := validateCanonicalAppendAccounting(subjects, ambiguous); err == nil {
		t.Fatal("multiple subject outcomes were accepted")
	}
}

func TestCanonicalAppendPreservesCommitBeforeTypedSubjectFailure(t *testing.T) {
	c, bucket := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
	createCanonicalEntity(t, c, canonicalEntityA, nil)
	bucket.mu.Lock()
	bucket.data[canonicalEntityB] = mockKVData{value: []byte(`{"id":`), revision: 1}
	bucket.mu.Unlock()

	data, err := c.handleCanonicalAppend(context.Background(), mustCanonicalJSON(t, graph.AppendTriplesRequest{
		Triples: []message.Triple{
			canonicalTriple(canonicalEntityA, "test.evidence.value", "committed"),
			canonicalTriple(canonicalEntityB, "test.evidence.value", "poison"),
		},
	}))
	if err != nil {
		t.Fatalf("handleCanonicalAppend: %v", err)
	}
	var response graph.AppendTriplesResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Results) != 2 || response.Results[0].Outcome != graph.MutationApplied ||
		response.Results[0].KVRevision == 0 {
		t.Fatalf("known commit lost: %#v", response.Results)
	}
	failed := response.Results[1]
	if failed.EntityID != canonicalEntityB || failed.Outcome != graph.MutationFailed || failed.Error == nil ||
		failed.Error.Class != "fatal" || failed.Error.Code != graph.ErrorCodeGraphStateResetRequired {
		t.Fatalf("typed failure = %#v", failed)
	}
}

func TestCanonicalConcurrentIdenticalAppendStoresOneTuple(t *testing.T) {
	c, _ := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
	createCanonicalEntity(t, c, canonicalEntityA, nil)
	request := mustCanonicalJSON(t, graph.AppendTriplesRequest{Triples: []message.Triple{
		canonicalTriple(canonicalEntityA, "test.evidence.value", "once"),
	}})

	var wg sync.WaitGroup
	outcomes := make(chan graph.MutationOutcome, 2)
	errsCh := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			data, err := c.handleCanonicalAppend(context.Background(), request)
			if err != nil {
				errsCh <- err
				return
			}
			var response graph.AppendTriplesResponse
			if err := json.Unmarshal(data, &response); err != nil {
				errsCh <- err
				return
			}
			outcomes <- response.Results[0].Outcome
		}()
	}
	wg.Wait()
	close(errsCh)
	for err := range errsCh {
		t.Fatalf("append: %v", err)
	}
	close(outcomes)
	counts := map[graph.MutationOutcome]int{}
	for outcome := range outcomes {
		counts[outcome]++
	}
	if counts[graph.MutationApplied] != 1 || counts[graph.MutationUnchanged] != 1 {
		t.Fatalf("outcomes = %v", counts)
	}
	stored, _, err := c.fetchEntityState(context.Background(), canonicalEntityA)
	if err != nil {
		t.Fatalf("fetchEntityState: %v", err)
	}
	count := 0
	for _, triple := range stored.Triples {
		if triple.Predicate == "test.evidence.value" && triple.Object == "once" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("stored duplicate count = %d", count)
	}
}

func TestCanonicalDeleteIsRevisionFenced(t *testing.T) {
	c, _ := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
	created := createCanonicalEntity(t, c, canonicalEntityA, nil)

	stale := graph.DeleteEntityRequest{EntityID: canonicalEntityA, ExpectedRevision: created.KVRevision + 1}
	_, err := c.handleCanonicalDelete(context.Background(), mustCanonicalJSON(t, stale))
	assertCanonicalCode(t, err, graph.ErrorCodeRevisionMismatch)

	request := graph.DeleteEntityRequest{EntityID: canonicalEntityA, ExpectedRevision: created.KVRevision}
	responseData, err := c.handleCanonicalDelete(context.Background(), mustCanonicalJSON(t, request))
	if err != nil {
		t.Fatalf("handleCanonicalDelete: %v", err)
	}
	var response graph.DeleteEntityResponse
	if err := json.Unmarshal(responseData, &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Outcome != graph.MutationApplied || response.ExpectedRevision != created.KVRevision {
		t.Fatalf("response = %#v", response)
	}
}

func TestCanonicalMutationOutcomeMetricAndRevisionMismatchLog(t *testing.T) {
	c, _ := createTestComponentWithMockKVBucket(t, withAuthority("acme", "ops"))
	created := createCanonicalEntity(t, c, canonicalEntityA, nil)
	var logs bytes.Buffer
	c.logger = slog.New(slog.NewTextHandler(&logs, nil))

	operation := graphmutation.ReconcilePredicates
	counter := c.mutationOutcomes.WithLabelValues(string(operation), string(graph.MutationRevisionMismatch))
	before := testutil.ToFloat64(counter)
	route := canonicalMutationRoute{
		operation: operation, subject: "graph.mutation.entity.reconcile", handler: c.handleCanonicalReconcile,
	}
	request := graph.ReconcilePredicatesRequest{
		EntityID: canonicalEntityA, ExpectedRevision: created.KVRevision + 1,
		Predicates: []string{"test.state.value"}, Desired: []message.Triple{},
	}
	_, err := c.meteredCanonicalMutation(route)(context.Background(), mustCanonicalJSON(t, request))
	assertCanonicalCode(t, err, graph.ErrorCodeRevisionMismatch)
	if after := testutil.ToFloat64(counter); after != before+1 {
		t.Fatalf("revision mismatch outcome delta = %v, want 1", after-before)
	}
	text := logs.String()
	for _, fragment := range []string{"graph mutation revision mismatch", "operation=entity.reconcile", "entity_id=" + canonicalEntityA, "expected_revision="} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("log %q missing %q", text, fragment)
		}
	}
}

func TestCanonicalAppendOutcomeMetricCountsEachSubjectResult(t *testing.T) {
	c := createTestComponentWithMockKV(t, withAuthority("acme", "ops"))
	operation := graphmutation.AppendTriples
	applied := c.mutationOutcomes.WithLabelValues(string(operation), string(graph.MutationApplied))
	failed := c.mutationOutcomes.WithLabelValues(string(operation), graph.ErrorCodeInternal)
	beforeApplied := testutil.ToFloat64(applied)
	beforeFailed := testutil.ToFloat64(failed)
	data := mustCanonicalJSON(t, graph.AppendTriplesResponse{Results: []graph.AppendSubjectResult{
		{EntityID: canonicalEntityA, Outcome: graph.MutationApplied, KVRevision: 2},
		{EntityID: canonicalEntityB, Outcome: graph.MutationFailed,
			Error: &graph.MutationFailure{Class: "transient", Code: graph.ErrorCodeInternal}},
	}})
	if err := c.recordCanonicalResponse(operation, data); err != nil {
		t.Fatalf("recordCanonicalResponse: %v", err)
	}
	if delta := testutil.ToFloat64(applied) - beforeApplied; delta != 1 {
		t.Fatalf("applied delta = %v", delta)
	}
	if delta := testutil.ToFloat64(failed) - beforeFailed; delta != 1 {
		t.Fatalf("failure delta = %v", delta)
	}
}

func TestCanonicalAppendOutcomeMetricRejectsFailureWithoutDetail(t *testing.T) {
	c := createTestComponentWithMockKV(t, withAuthority("acme", "ops"))
	data := mustCanonicalJSON(t, graph.AppendTriplesResponse{Results: []graph.AppendSubjectResult{{
		EntityID: canonicalEntityA, Outcome: graph.MutationFailed,
	}}})
	if err := c.recordCanonicalResponse(graphmutation.AppendTriples, data); err == nil {
		t.Fatal("recordCanonicalResponse accepted failed append result without error detail")
	}
}

func canonicalMutationEntity(entityID string) *graph.EntityState {
	return &graph.EntityState{
		ID:          entityID,
		MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"},
	}
}

func canonicalTriple(subject, predicate string, object any) message.Triple {
	return message.Triple{Subject: subject, Predicate: predicate, Object: object, Source: "canonical-test"}
}

func createCanonicalEntity(t *testing.T, c *Component, entityID string, triples []message.Triple) graph.CreateEntityResponse {
	t.Helper()
	data, err := c.handleCanonicalCreate(context.Background(), mustCanonicalJSON(t, graph.CreateEntityRequest{
		Entity: canonicalMutationEntity(entityID), Triples: triples,
	}))
	if err != nil {
		t.Fatalf("handleCanonicalCreate: %v", err)
	}
	var response graph.CreateEntityResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return response
}

func mustCanonicalJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return data
}

func withCanonicalField(t *testing.T, value any, field string, extra any) []byte {
	t.Helper()
	data := mustCanonicalJSON(t, value)
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("decode canonical fields: %v", err)
	}
	fields[field] = extra
	return mustCanonicalJSON(t, fields)
}

func assertCanonicalCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want code %s", code)
	}
	var classified *errs.ClassifiedError
	if !errors.As(err, &classified) || classified.Code != code {
		t.Fatalf("error = %v, want classified code %s", err, code)
	}
}
