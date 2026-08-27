package lessons

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/projection"
	"github.com/c360studio/semstreams/processor/agentic-loop/lessonmatch"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/c360studio/semstreams/vocabulary"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

func TestScenarioStagesAreExactAndOrdered(t *testing.T) {
	s := NewScenario()
	gotStages := s.stages()
	got := make([]string, len(gotStages))
	for i := range gotStages {
		got[i] = gotStages[i].name
	}
	want := []string{
		"create-and-prove-proposed",
		"promote-and-prove-match",
		"recreate-and-prove-convergence",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stage plan = %v, want %v", got, want)
	}
}

func TestRunStagesDoesNotCountFailedStageOrRunLaterStages(t *testing.T) {
	wantErr := errors.New("stage failed")
	result := &scenarios.Result{}
	laterRan := false
	failed, err := runStages(t.Context(), result, []stage{
		{name: "first", run: func(context.Context) error { return nil }},
		{name: "second", run: func(context.Context) error { return wantErr }},
		{name: "third", run: func(context.Context) error { laterRan = true; return nil }},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("runStages error = %v, want %v", err, wantErr)
	}
	if failed != "second" {
		t.Fatalf("failed stage = %q, want second", failed)
	}
	if result.AssertionsRun != 1 {
		t.Fatalf("AssertionsRun = %d, want 1", result.AssertionsRun)
	}
	if laterRan {
		t.Fatal("later stage ran after failure")
	}
}

func TestProductLessonFixtureHasAcceptedIdentityAndCompleteTuples(t *testing.T) {
	fixture := newProductLessonFixture(defaultConfig())
	if fixture.entityID != "c360.streamkit-pure.agent.lesson.record.54b545de-8f18-5419-b996-220d3c992c5c" {
		t.Fatalf("entity ID = %q", fixture.entityID)
	}
	if got := fixture.messageType.Key(); got != "agentic.agent_lesson.v1" {
		t.Fatalf("message type = %q", got)
	}
	if len(fixture.triples) != 10 {
		t.Fatalf("triple count = %d, want 10", len(fixture.triples))
	}
	wantObjects := map[string]string{
		"agent.lesson.category":       "retention-policy",
		"agent.lesson.polarity":       "best_practice",
		"agent.lesson.severity":       "warning",
		"agent.lesson.status":         "proposed",
		"agent.lesson.created-at":     fixture.timestamp.Format("2006-01-02T15:04:05Z07:00"),
		"agent.lesson.summary":        "Scope retention sweeps to entity-owned buckets.",
		"agent.lesson.detail":         "Entity-owned retention prevents unrelated state from being swept together.",
		"agent.lesson.injection-form": "Scope retention sweeps to entity-owned buckets.",
		"agent.lesson.evidence":       evidenceEntityID,
		"agent.lesson.applies-to":     "tag:product-lesson-e2e",
	}
	for _, triple := range fixture.triples {
		object, ok := triple.Object.(string)
		if !ok {
			t.Fatalf("%s object has type %T, want string", triple.Predicate, triple.Object)
		}
		if want, ok := wantObjects[triple.Predicate]; !ok || object != want {
			t.Fatalf("unexpected tuple %s=%q", triple.Predicate, object)
		}
		delete(wantObjects, triple.Predicate)
		if triple.Subject != fixture.entityID || triple.Source != fixtureSource ||
			triple.Context != lessonCreateRequestID || !triple.Timestamp.Equal(fixture.timestamp) ||
			triple.Confidence != 1 || triple.Datatype != "" || triple.ExpiresAt != nil {
			t.Fatalf("incomplete tuple metadata for %s: %+v", triple.Predicate, triple)
		}
	}
	if len(wantObjects) != 0 {
		t.Fatalf("missing predicates: %v", wantObjects)
	}
}

func TestEvidenceContractAndFixtureAreExact(t *testing.T) {
	contract := evidenceContract()
	if contract.Name != "e2e.lessons.evidence" || contract.MessageType != "test.fixture.v1" ||
		contract.EntityPattern != evidenceEntityID || contract.IndexingProfile != "control" {
		t.Fatalf("evidence contract = %+v", contract)
	}
	if !reflect.DeepEqual(contract.BirthPredicates, []string{vocabulary.DCTermsTitle}) || len(contract.Groups) != 0 {
		t.Fatalf("evidence predicates/groups = %v/%v", contract.BirthPredicates, contract.Groups)
	}
	mutation := evidenceCreateMutation()
	if mutation.Entity.ID != evidenceEntityID || mutation.Entity.MessageType.Key() != "test.fixture.v1" ||
		mutation.Entity.Version != 1 || !mutation.Entity.UpdatedAt.Equal(fixtureTimestamp) {
		t.Fatalf("evidence entity = %+v", mutation.Entity)
	}
	if err := requireExactTriples(mutation.Triples, []message.Triple{{
		Subject: evidenceEntityID, Predicate: vocabulary.DCTermsTitle,
		Object: "product lesson E2E evidence", Source: fixtureSource,
		Context: evidenceCreateRequestID, Timestamp: fixtureTimestamp, Confidence: 1,
	}}); err != nil {
		t.Fatal(err)
	}
	exact := &graph.ExactEntity{Entity: mutation.Entity.Clone(), KVRevision: 1}
	exact.Entity.Triples = append([]message.Triple(nil), mutation.Triples...)
	exact.Entity.Triples = append(exact.Entity.Triples, canonicalProfileTriple(evidenceEntityID, vocabulary.IndexingProfileControl))
	if err := requireEvidenceAuthority(exact); err != nil {
		t.Fatal(err)
	}
}

func TestAuthorityComparatorsIncludeDatatypeAndLifecycleRules(t *testing.T) {
	fixture := newProductLessonFixture(defaultConfig())
	proposed := exactLesson(fixture, fixture.triples)
	if err := requireProposedAuthority(proposed, fixture); err != nil {
		t.Fatalf("valid proposed authority: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(*message.Triple)
	}{
		{name: "context", mutate: func(triple *message.Triple) { triple.Context = "changed-context" }},
		{name: "datatype", mutate: func(triple *message.Triple) { triple.Datatype = "xsd:string" }},
		{name: "expires-at", mutate: func(triple *message.Triple) {
			expiresAt := fixtureTimestamp.Add(time.Hour)
			triple.ExpiresAt = &expiresAt
		}},
	}
	for _, tc := range mutations {
		t.Run("proposed-"+tc.name, func(t *testing.T) {
			changed := exactLesson(fixture, append([]message.Triple(nil), fixture.triples...))
			tc.mutate(&changed.Entity.Triples[0])
			if err := requireProposedAuthority(changed, fixture); err == nil {
				t.Fatalf("proposed comparator accepted changed caller %s", tc.name)
			}
		})
	}

	activeTriples := nonLifecycle(fixture.triples)
	activeTriples = append(activeTriples, message.Triple{
		Subject: fixture.entityID, Predicate: agvocab.LessonStatus, Object: "active",
		Source: "ops-lesson-curator", Timestamp: fixtureTimestamp.Add(time.Second), Confidence: 1,
	})
	active := exactLesson(fixture, activeTriples)
	if err := requireActiveAuthority(active, fixture); err != nil {
		t.Fatalf("valid active authority: %v", err)
	}
	for _, tc := range mutations {
		t.Run("active-"+tc.name, func(t *testing.T) {
			changed := exactLesson(fixture, append([]message.Triple(nil), activeTriples...))
			tc.mutate(&changed.Entity.Triples[0])
			if err := requireActiveAuthority(changed, fixture); err == nil {
				t.Fatalf("active comparator accepted changed caller %s", tc.name)
			}
		})
	}

	withSibling := exactLesson(fixture, append(append([]message.Triple(nil), activeTriples...), message.Triple{
		Subject: fixture.entityID, Predicate: agvocab.LessonRetiredAt, Object: fixtureTimestamp.Format(time.RFC3339),
	}))
	if err := requireActiveAuthority(withSibling, fixture); err == nil {
		t.Fatal("active comparator accepted retired-at sibling")
	}
}

func TestAuthorityComparatorsRequireOneCanonicalIndexingProfileStamp(t *testing.T) {
	fixture := newProductLessonFixture(defaultConfig())
	valid := exactLesson(fixture, fixture.triples)
	if err := requireProposedAuthority(valid, fixture); err != nil {
		t.Fatalf("canonical profile stamp: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*graph.ExactEntity)
	}{
		{name: "missing", mutate: func(exact *graph.ExactEntity) {
			exact.Entity.Triples = exact.Entity.Triples[:len(exact.Entity.Triples)-1]
		}},
		{name: "duplicate", mutate: func(exact *graph.ExactEntity) {
			exact.Entity.Triples = append(exact.Entity.Triples, canonicalProfileTriple(fixture.entityID, vocabulary.IndexingProfileContent))
		}},
		{name: "wrong object", mutate: func(exact *graph.ExactEntity) {
			exact.Entity.Triples[len(exact.Entity.Triples)-1].Object = vocabulary.IndexingProfileTrace
		}},
		{name: "wrong source", mutate: func(exact *graph.ExactEntity) {
			exact.Entity.Triples[len(exact.Entity.Triples)-1].Source = "caller"
		}},
		{name: "wrong subject", mutate: func(exact *graph.ExactEntity) {
			exact.Entity.Triples[len(exact.Entity.Triples)-1].Subject = evidenceEntityID
		}},
		{name: "confidence", mutate: func(exact *graph.ExactEntity) {
			exact.Entity.Triples[len(exact.Entity.Triples)-1].Confidence = 0.5
		}},
		{name: "context", mutate: func(exact *graph.ExactEntity) {
			exact.Entity.Triples[len(exact.Entity.Triples)-1].Context = "unexpected"
		}},
		{name: "datatype", mutate: func(exact *graph.ExactEntity) {
			exact.Entity.Triples[len(exact.Entity.Triples)-1].Datatype = "xsd:string"
		}},
		{name: "expires-at", mutate: func(exact *graph.ExactEntity) {
			expiresAt := fixtureTimestamp.Add(time.Hour)
			exact.Entity.Triples[len(exact.Entity.Triples)-1].ExpiresAt = &expiresAt
		}},
		{name: "zero timestamp", mutate: func(exact *graph.ExactEntity) {
			exact.Entity.Triples[len(exact.Entity.Triples)-1].Timestamp = time.Time{}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exact := exactLesson(fixture, fixture.triples)
			tc.mutate(exact)
			if err := requireProposedAuthority(exact, fixture); err == nil {
				t.Fatalf("accepted %s indexing-profile stamp", tc.name)
			}
		})
	}
}

func TestScenarioReaderMatcherExcludesProposedAndIncludesExactActiveLesson(t *testing.T) {
	fixture := newProductLessonFixture(defaultConfig())
	s := NewScenario()
	reader := &fakeLessonReader{lessons: []lessonmatch.Lesson{{
		EntityID: fixture.entityID, Status: "proposed", Severity: "warning",
		CreatedAt: fixture.timestamp.Format(time.RFC3339), AppliesTo: []string{lessonScopeKey},
		InjectionForm: lessonInjectionForm,
	}}}
	s.clients.reader = reader
	candidates, matched, err := s.readAndMatch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := requireReaderTarget(candidates, fixture.entityID, "proposed", lessonInjectionForm); err != nil {
		t.Fatal(err)
	}
	if matched.MatchedCount != 0 || matched.IncludedCount != 0 {
		t.Fatalf("proposed matcher result = %+v", matched)
	}

	reader.lessons[0].Status = "active"
	_, first, err := s.readAndMatch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := requireExactMatch(first, fixture.entityID, lessonInjectionForm); err != nil {
		t.Fatal(err)
	}
	_, afterRecreate, err := s.readAndMatch(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterRecreate, first) {
		t.Fatalf("post-recreate matcher = %+v, want %+v", afterRecreate, first)
	}
}

func TestSetupUsesOneNATSOwnerAndTeardownClosesItOnce(t *testing.T) {
	raw := &natsclient.Client{}
	owner := &fakeValidationClient{client: raw}
	s := NewScenario()
	openCalls := 0
	s.openNATS = func(context.Context, string) (validationClient, error) {
		openCalls++
		return owner, nil
	}
	composeCalls := 0
	s.compose = func(got *natsclient.Client, _ time.Duration) (scenarioClients, error) {
		composeCalls++
		if got != raw {
			t.Fatalf("compose client = %p, want sole owner client %p", got, raw)
		}
		return scenarioClients{}, nil
	}
	if err := s.Setup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Teardown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Teardown(t.Context()); err != nil {
		t.Fatal(err)
	}
	if openCalls != 1 || composeCalls != 1 || owner.closeCalls != 1 {
		t.Fatalf("open=%d compose=%d close=%d, want 1/1/1", openCalls, composeCalls, owner.closeCalls)
	}
}

func TestComposeScenarioClientsRegistersBuiltinsBeforeContractValidation(t *testing.T) {
	restore := vocabulary.SnapshotRegistry()
	defer restore()
	vocabulary.ClearRegistry()
	// ClearRegistry also removes init-registered cross-product vocabulary. Keep
	// this test focused on the explicit agentic built-ins precondition while
	// retaining the evidence contract's independent title declaration.
	vocabulary.Register(vocabulary.DCTermsTitle)

	lessonContract := agentictools.LessonProjectionContract()
	if err := lessonContract.Validate(); err == nil {
		t.Fatal("lesson contract unexpectedly validated before built-in vocabulary registration")
	}
	if err := vocabulary.RequireDeclaredPredicate(agvocab.LessonStatus); err == nil {
		t.Fatal("lesson status unexpectedly remained registered after ClearRegistry")
	}

	raw := &natsclient.Client{}
	first, err := composeScenarioClients(raw, time.Second)
	if err != nil {
		t.Fatalf("first composition: %v", err)
	}
	if first.mutations == nil || first.store == nil || first.curator == nil || first.reader == nil {
		t.Fatalf("first composition returned incomplete clients: %+v", first)
	}
	for _, predicate := range []string{
		agvocab.LessonCategory,
		agvocab.LessonStatus,
		agvocab.LessonRetiredAt,
		agvocab.LessonSupersededBy,
	} {
		if err := vocabulary.RequireDeclaredPredicate(predicate); err != nil {
			t.Fatalf("composition did not register %q: %v", predicate, err)
		}
	}
	if err := lessonContract.Validate(); err != nil {
		t.Fatalf("lesson contract after composition: %v", err)
	}
	if _, err := composeScenarioClients(raw, time.Second); err != nil {
		t.Fatalf("idempotent second composition: %v", err)
	}
}

func TestLifecycleBoundariesRejectNilContext(t *testing.T) {
	s := NewScenario()
	if err := s.Setup(nil); err == nil {
		t.Fatal("Setup accepted nil context")
	}
	if _, err := s.Execute(nil); err == nil {
		t.Fatal("Execute accepted nil context")
	}
	if err := s.Teardown(nil); err == nil {
		t.Fatal("Teardown accepted nil context")
	}
}

func TestSetupRollbackJoinsCompositionAndCloseErrors(t *testing.T) {
	composeErr := errors.New("compose failed")
	closeErr := errors.New("close failed")
	owner := &fakeValidationClient{client: &natsclient.Client{}, closeErr: closeErr}
	s := NewScenario()
	s.openNATS = func(context.Context, string) (validationClient, error) { return owner, nil }
	s.compose = func(*natsclient.Client, time.Duration) (scenarioClients, error) {
		return scenarioClients{}, composeErr
	}
	err := s.Setup(t.Context())
	if !errors.Is(err, composeErr) || !errors.Is(err, closeErr) {
		t.Fatalf("Setup error = %v, want both composition and close causes", err)
	}
	if owner.closeCalls != 1 || s.nats != nil {
		t.Fatalf("rollback close calls=%d retained owner=%v", owner.closeCalls, s.nats)
	}
}

func TestCleanupDeletesOnlyTrackedIDsAtObservedRevisions(t *testing.T) {
	cleaner := &fakeCleaner{entities: map[string]*graph.ExactEntity{
		"c360.test.one.entity.record.1":       {Entity: &graph.EntityState{ID: "c360.test.one.entity.record.1"}, KVRevision: 17},
		"c360.test.two.entity.record.2":       {Entity: &graph.EntityState{ID: "c360.test.two.entity.record.2"}, KVRevision: 29},
		"c360.test.untracked.entity.record.3": {Entity: &graph.EntityState{ID: "c360.test.untracked.entity.record.3"}, KVRevision: 41},
	}}
	err := cleanupTracked(t.Context(), cleaner, []string{
		"c360.test.one.entity.record.1",
		"c360.test.two.entity.record.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []projection.DeleteMutation{
		{EntityID: "c360.test.two.entity.record.2", ExpectedRevision: 29,
			Metadata: projection.MutationMetadata{RequestID: "e2e-lessons-cleanup:c360.test.two.entity.record.2"}},
		{EntityID: "c360.test.one.entity.record.1", ExpectedRevision: 17,
			Metadata: projection.MutationMetadata{RequestID: "e2e-lessons-cleanup:c360.test.one.entity.record.1"}},
	}
	if !reflect.DeepEqual(cleaner.deletes, want) {
		t.Fatalf("delete requests = %+v, want %+v", cleaner.deletes, want)
	}
	if !reflect.DeepEqual(cleaner.readIDs, []string{
		"c360.test.two.entity.record.2",
		"c360.test.one.entity.record.1",
	}) {
		t.Fatalf("cleanup reads = %v, want tracked IDs only", cleaner.readIDs)
	}
}

func TestStageAndCleanupErrorsAreJoined(t *testing.T) {
	primaryErr := errors.New("primary")
	cleanupErr := errors.New("cleanup")
	result := &scenarios.Result{}
	cleaner := &fakeCleaner{
		entities: map[string]*graph.ExactEntity{"c360.test.one.entity.record.1": {
			Entity: &graph.EntityState{ID: "c360.test.one.entity.record.1"}, KVRevision: 7,
		}},
		deleteErr: cleanupErr,
	}
	tracked := []string{"c360.test.one.entity.record.1"}
	err := runStagesAndCleanup(t.Context(), result, []stage{{
		name: "failing", run: func(context.Context) error { return primaryErr },
	}}, cleaner, &tracked)
	if !errors.Is(err, primaryErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("joined error = %v, want both causes", err)
	}
	if result.AssertionsRun != 0 {
		t.Fatalf("AssertionsRun = %d, want 0", result.AssertionsRun)
	}
}

func TestRunStagesAndCleanupSeesIDsTrackedDuringStages(t *testing.T) {
	const entityID = "c360.test.one.entity.record.1"
	cleaner := &fakeCleaner{entities: map[string]*graph.ExactEntity{
		entityID: {Entity: &graph.EntityState{ID: entityID}, KVRevision: 53},
	}}
	var tracked []string
	err := runStagesAndCleanup(t.Context(), &scenarios.Result{}, []stage{{
		name: "track", run: func(context.Context) error {
			tracked = append(tracked, entityID)
			return nil
		},
	}}, cleaner, &tracked)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleaner.deletes) != 1 || cleaner.deletes[0].EntityID != entityID ||
		cleaner.deletes[0].ExpectedRevision != 53 {
		t.Fatalf("cleanup deletes = %+v", cleaner.deletes)
	}
}

func exactLesson(fixture productLessonFixture, triples []message.Triple) *graph.ExactEntity {
	withProfile := append([]message.Triple(nil), triples...)
	withProfile = append(withProfile, canonicalProfileTriple(fixture.entityID, vocabulary.IndexingProfileContent))
	return &graph.ExactEntity{Entity: &graph.EntityState{
		ID: fixture.entityID, MessageType: fixture.messageType, Triples: withProfile,
	}, KVRevision: 1}
}

// canonicalProfileTriple is the create-seam stamp graph-ingest adds: the
// evidence fixture (test.fixture.v1) takes the fixtures' control floor, a
// lesson (agentic.agent_lesson.v1) takes its registered content floor
// (ADR-103, O-3).
func canonicalProfileTriple(subject, profile string) message.Triple {
	return message.Triple{
		Subject: subject, Predicate: vocabulary.EntityIndexingProfile,
		Object: profile, Source: "graph-ingest-indexing-profile",
		Timestamp: fixtureTimestamp.Add(time.Minute), Confidence: 1,
	}
}

type fakeLessonReader struct {
	lessons []lessonmatch.Lesson
	err     error
}

func (r *fakeLessonReader) ReadLessons(context.Context, string) ([]lessonmatch.Lesson, error) {
	return append([]lessonmatch.Lesson(nil), r.lessons...), r.err
}

type fakeValidationClient struct {
	client     *natsclient.Client
	closeCalls int
	closeErr   error
}

func (c *fakeValidationClient) Client() *natsclient.Client { return c.client }
func (c *fakeValidationClient) Close(context.Context) error {
	c.closeCalls++
	return c.closeErr
}

type fakeCleaner struct {
	entities  map[string]*graph.ExactEntity
	readErr   error
	deleteErr error
	deletes   []projection.DeleteMutation
	readIDs   []string
}

func (c *fakeCleaner) ReadAuthoritative(_ context.Context, entityID string) (*graph.ExactEntity, error) {
	c.readIDs = append(c.readIDs, entityID)
	if c.readErr != nil {
		return nil, c.readErr
	}
	entity, ok := c.entities[entityID]
	if !ok {
		return nil, &projection.MutationError{Kind: projection.MutationNotFound}
	}
	return entity, nil
}

func (c *fakeCleaner) Delete(_ context.Context, mutation projection.DeleteMutation) (projection.MutationReceipt, error) {
	c.deletes = append(c.deletes, mutation)
	return projection.MutationReceipt{Commit: projection.CommitVerified}, c.deleteErr
}
