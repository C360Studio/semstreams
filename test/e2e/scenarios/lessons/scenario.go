// Package lessons provides the direct-product lesson E2E scenario.
package lessons

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/projection"
	agenticloop "github.com/c360studio/semstreams/processor/agentic-loop"
	"github.com/c360studio/semstreams/processor/agentic-loop/lessonmatch"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/test/e2e/client"
	e2econfig "github.com/c360studio/semstreams/test/e2e/config"
	"github.com/c360studio/semstreams/test/e2e/scenarios"
	"github.com/c360studio/semstreams/vocabulary"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
	"github.com/c360studio/semstreams/vocabulary/builtins"
)

// evidenceEntityID is minted under the CORE stack's own authority — the lessons
// tier boots docker/compose/e2e.yml, the same composition the core tier does —
// composed from e2econfig.CoreAuthority rather than restated. A hardcoded copy
// is a second home for a value one place owns, and the graph refuses any pair
// but the deployment's (ADR-102 d5), so drift here is a refused create three
// stages later rather than a compile error (review MEDIUM-5).
var evidenceEntityID = e2econfig.CoreAuthority + ".test.fixture.evidence.product-lesson"

const (
	evidenceContractName    = "e2e.lessons.evidence"
	evidenceCreateRequestID = "e2e-lessons-evidence-create"
	lessonCreateRequestID   = "e2e-lessons-create"
	fixtureSource           = "e2e-product-lesson"
	lessonIdentityToken     = "54b545de-8f18-5419-b996-220d3c992c5c"
	lessonScopeKey          = "tag:product-lesson-e2e"
	lessonScopeTag          = "product-lesson-e2e"
	lessonInjectionForm     = "Scope retention sweeps to entity-owned buckets."
	indexingProfileSource   = "graph-ingest-indexing-profile"
)

var fixtureTimestamp = time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)

// config contains only the existing runtime address, platform identity, and
// operation budget needed by the standalone lesson scenario.
type config struct {
	NATSURL          string
	Org              string
	Platform         string
	OperationTimeout time.Duration
}

func defaultConfig() *config {
	return &config{
		NATSURL:          e2econfig.DefaultEndpoints.NATS,
		Org:              "c360",
		Platform:         "streamkit-pure",
		OperationTimeout: e2econfig.DefaultTestConfig.Timeout,
	}
}

type validationClient interface {
	Client() *natsclient.Client
	Close(context.Context) error
}

type scenarioClients struct {
	mutations *projection.MutationClient
	store     agentictools.LessonStore
	curator   *agentictools.LessonCurator
	reader    agenticloop.LessonReader
}

type authoritativeCleaner interface {
	projection.AuthoritativeReader
	projection.EntityDeleter
}

type stage struct {
	name string
	run  func(context.Context) error
}

type productLessonFixture struct {
	entityID    string
	messageType message.Type
	triples     []message.Triple
	timestamp   time.Time
}

// Scenario proves direct product birth, curation, reader/matcher eligibility,
// idempotent recreate, and exact-ID cleanup without running an agent loop.
type Scenario struct {
	config  *config
	nats    validationClient
	clients scenarioClients
	fixture productLessonFixture

	trackedIDs []string

	openNATS func(context.Context, string) (validationClient, error)
	compose  func(*natsclient.Client, time.Duration) (scenarioClients, error)
}

// NewScenario constructs the standalone direct-product lesson scenario over
// the fixed production-target core E2E stack.
func NewScenario() *Scenario {
	config := defaultConfig()
	return &Scenario{
		config:  config,
		fixture: newProductLessonFixture(config),
		openNATS: func(ctx context.Context, url string) (validationClient, error) {
			return client.NewNATSValidationClient(ctx, url)
		},
		compose: composeScenarioClients,
	}
}

// Name returns the CLI scenario identifier.
func (*Scenario) Name() string { return "lessons" }

// Description returns the exact assembled behavior under test.
func (*Scenario) Description() string {
	return "Direct product lesson birth, lifecycle, reader/matcher eligibility, recreate convergence, and cleanup"
}

// Setup opens the sole owning NATS client and composes non-owning lesson clients over it.
func (s *Scenario) Setup(ctx context.Context) error {
	if ctx == nil {
		return errors.New("setup context is required")
	}
	owner, err := s.openNATS(ctx, s.config.NATSURL)
	if err != nil {
		return fmt.Errorf("open validation NATS client: %w", err)
	}
	clients, composeErr := s.compose(owner.Client(), s.config.OperationTimeout)
	if composeErr != nil {
		closeErr := owner.Close(ctx)
		return errors.Join(fmt.Errorf("compose lesson clients: %w", composeErr), closeErr)
	}
	s.nats = owner
	s.clients = clients
	return nil
}

func composeScenarioClients(raw *natsclient.Client, timeout time.Duration) (scenarioClients, error) {
	// Match both production composition roots: projection contracts validate
	// semantic declarations explicitly, so first-party vocabulary registration
	// must precede local mutation-client construction.
	builtins.Register()
	mutations, err := projection.NewMutationClient(projection.MutationClientConfig{
		NATS: raw,
		Contracts: []projection.Contract{
			agentictools.LessonProjectionContract(),
			evidenceContract(),
		},
		Timeout: timeout,
	})
	if err != nil {
		return scenarioClients{}, err
	}
	return scenarioClients{
		mutations: mutations,
		store:     agentictools.NewNATSLessonStore(raw),
		curator:   agentictools.NewLessonCurator(mutations, mutations, slog.Default()),
		reader:    agenticloop.NewNATSLessonReader(raw),
	}, nil
}

// Execute runs the exact three-stage product path and cleans every tracked ID
// before reporting success. Stage and cleanup errors retain both causes.
func (s *Scenario) Execute(ctx context.Context) (*scenarios.Result, error) {
	if ctx == nil {
		return nil, errors.New("execute context is required")
	}
	result := &scenarios.Result{
		ScenarioName: s.Name(),
		StartTime:    time.Now(),
		Metrics:      make(map[string]any),
		Details:      make(map[string]any),
	}

	combined := runStagesAndCleanup(ctx, result, s.stages(), s.clients.mutations, &s.trackedIDs)
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)
	if combined != nil {
		result.Error = combined.Error()
		result.Errors = []string{combined.Error()}
		return result, combined
	}
	result.Success = true
	return result, nil
}

func runStagesAndCleanup(
	ctx context.Context,
	result *scenarios.Result,
	stages []stage,
	cleaner authoritativeCleaner,
	tracked *[]string,
) error {
	failedStage, stageErr := runStages(ctx, result, stages)
	if stageErr != nil {
		stageErr = fmt.Errorf("%s failed: %w", failedStage, stageErr)
	}
	return errors.Join(stageErr, cleanupTracked(ctx, cleaner, *tracked))
}

func (s *Scenario) stages() []stage {
	return []stage{
		{name: "create-and-prove-proposed", run: s.withTimeout(s.createAndProveProposed)},
		{name: "promote-and-prove-match", run: s.withTimeout(s.promoteAndProveMatch)},
		{name: "recreate-and-prove-convergence", run: s.withTimeout(s.recreateAndProveConvergence)},
	}
}

func (s *Scenario) withTimeout(run func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		stageCtx, cancel := context.WithTimeout(ctx, s.config.OperationTimeout)
		defer cancel()
		return run(stageCtx)
	}
}

func runStages(ctx context.Context, result *scenarios.Result, stages []stage) (string, error) {
	for _, current := range stages {
		if err := current.run(ctx); err != nil {
			return current.name, err
		}
		result.AssertionsRun++
	}
	return "", nil
}

func (s *Scenario) createAndProveProposed(ctx context.Context) error {
	s.track(evidenceEntityID)
	receipt, err := s.clients.mutations.Create(ctx, evidenceCreateMutation())
	if err != nil {
		return fmt.Errorf("create evidence fixture: %w", err)
	}
	if receipt.Commit != projection.CommitVerified {
		return fmt.Errorf("evidence create commit = %q, want %q", receipt.Commit, projection.CommitVerified)
	}
	evidence, err := s.clients.mutations.ReadAuthoritative(ctx, evidenceEntityID)
	if err != nil {
		return fmt.Errorf("read evidence authority: %w", err)
	}
	if err := requireEvidenceAuthority(evidence); err != nil {
		return err
	}

	s.track(s.fixture.entityID)
	created, err := s.clients.store.CreateLesson(ctx, s.fixture.entityID, s.fixture.messageType, s.fixture.triples)
	if err != nil {
		return fmt.Errorf("create product lesson: %w", err)
	}
	if !created {
		return errors.New("direct product lesson create returned created=false, want true")
	}
	status, found, err := s.clients.store.ReadLessonStatus(ctx, s.fixture.entityID)
	if err != nil {
		return fmt.Errorf("read proposed status: %w", err)
	}
	if !found || status != "proposed" {
		return fmt.Errorf("lesson status = %q found=%t, want proposed found=true", status, found)
	}
	exact, err := s.clients.mutations.ReadAuthoritative(ctx, s.fixture.entityID)
	if err != nil {
		return fmt.Errorf("read proposed authority: %w", err)
	}
	if err := requireProposedAuthority(exact, s.fixture); err != nil {
		return err
	}
	candidates, matched, err := s.readAndMatch(ctx)
	if err != nil {
		return err
	}
	if err := requireReaderTarget(candidates, s.fixture.entityID, "proposed", lessonInjectionForm); err != nil {
		return err
	}
	if matched.MatchedCount != 0 || matched.IncludedCount != 0 || len(matched.Included) != 0 {
		return fmt.Errorf("proposed matcher result = %+v, want empty", matched)
	}
	return nil
}

func (s *Scenario) promoteAndProveMatch(ctx context.Context) error {
	if err := s.clients.curator.Promote(ctx, s.fixture.entityID); err != nil {
		return fmt.Errorf("promote product lesson: %w", err)
	}
	exact, err := s.clients.mutations.ReadAuthoritative(ctx, s.fixture.entityID)
	if err != nil {
		return fmt.Errorf("read active authority: %w", err)
	}
	if err := requireActiveAuthority(exact, s.fixture); err != nil {
		return err
	}
	candidates, matched, err := s.readAndMatch(ctx)
	if err != nil {
		return err
	}
	if err := requireReaderTarget(candidates, s.fixture.entityID, "active", lessonInjectionForm); err != nil {
		return err
	}
	return requireExactMatch(matched, s.fixture.entityID, lessonInjectionForm)
}

func (s *Scenario) recreateAndProveConvergence(ctx context.Context) error {
	created, err := s.clients.store.CreateLesson(ctx, s.fixture.entityID, s.fixture.messageType, s.fixture.triples)
	if err != nil {
		return fmt.Errorf("recreate product lesson: %w", err)
	}
	if created {
		return errors.New("identical product lesson recreate returned created=true, want false")
	}
	status, found, err := s.clients.store.ReadLessonStatus(ctx, s.fixture.entityID)
	if err != nil {
		return fmt.Errorf("read post-recreate status: %w", err)
	}
	if !found || status != "active" {
		return fmt.Errorf("post-recreate status = %q found=%t, want active found=true", status, found)
	}
	exact, err := s.clients.mutations.ReadAuthoritative(ctx, s.fixture.entityID)
	if err != nil {
		return fmt.Errorf("read post-recreate authority: %w", err)
	}
	if err := requireActiveAuthority(exact, s.fixture); err != nil {
		return fmt.Errorf("post-recreate authority: %w", err)
	}
	candidates, matched, err := s.readAndMatch(ctx)
	if err != nil {
		return err
	}
	if err := requireReaderTarget(candidates, s.fixture.entityID, "active", lessonInjectionForm); err != nil {
		return err
	}
	return requireExactMatch(matched, s.fixture.entityID, lessonInjectionForm)
}

func (s *Scenario) readAndMatch(ctx context.Context) ([]lessonmatch.Lesson, lessonmatch.Result, error) {
	candidates, err := s.clients.reader.ReadLessons(ctx, agentic.AgentLessonRecordPrefix(s.config.Org, s.config.Platform))
	if err != nil {
		return nil, lessonmatch.Result{}, fmt.Errorf("read lesson candidates: %w", err)
	}
	matched := lessonmatch.Match(candidates, lessonmatch.Scope{Tags: []string{lessonScopeTag}}, lessonmatch.Opts{})
	return candidates, matched, nil
}

func (s *Scenario) track(entityID string) {
	for _, tracked := range s.trackedIDs {
		if tracked == entityID {
			return
		}
	}
	s.trackedIDs = append(s.trackedIDs, entityID)
}

// Teardown closes only the scenario-owned validation client.
func (s *Scenario) Teardown(ctx context.Context) error {
	if ctx == nil {
		return errors.New("teardown context is required")
	}
	if s.nats == nil {
		return nil
	}
	owner := s.nats
	s.nats = nil
	return owner.Close(ctx)
}

func evidenceContract() projection.Contract {
	return projection.Contract{
		Name:            evidenceContractName,
		MessageType:     message.Type{Domain: "test", Category: "fixture", Version: "v1"},
		EntityPattern:   evidenceEntityID,
		BirthPredicates: []string{vocabulary.DCTermsTitle},
		IndexingProfile: "control",
	}
}

func evidenceCreateMutation() projection.CreateMutation {
	triple := message.Triple{
		Subject: evidenceEntityID, Predicate: vocabulary.DCTermsTitle,
		Object: "product lesson E2E evidence", Source: fixtureSource,
		Context: evidenceCreateRequestID, Timestamp: fixtureTimestamp,
		Confidence: 1,
	}
	return projection.CreateMutation{
		Contract: evidenceContractName,
		Entity: &graph.EntityState{
			ID: evidenceEntityID, MessageType: message.Type{Domain: "test", Category: "fixture", Version: "v1"},
			Version: 1, UpdatedAt: fixtureTimestamp,
		},
		Triples: []message.Triple{triple},
		Metadata: projection.MutationMetadata{
			RequestID: evidenceCreateRequestID, Source: fixtureSource, Timestamp: fixtureTimestamp,
		},
	}
}

func newProductLessonFixture(config *config) productLessonFixture {
	entityID := agentic.AgentLessonEntityID(config.Org, config.Platform, lessonIdentityToken)
	objects := []struct {
		predicate string
		object    string
	}{
		{agvocab.LessonCategory, "retention-policy"},
		{agvocab.LessonPolarity, "best_practice"},
		{agvocab.LessonSeverity, "warning"},
		{agvocab.LessonStatus, "proposed"},
		{agvocab.LessonCreatedAt, fixtureTimestamp.Format(time.RFC3339)},
		{agvocab.LessonSummary, "Scope retention sweeps to entity-owned buckets."},
		{agvocab.LessonDetail, "Entity-owned retention prevents unrelated state from being swept together."},
		{agvocab.LessonInjectionForm, lessonInjectionForm},
		{agvocab.LessonEvidence, evidenceEntityID},
		{agvocab.LessonAppliesTo, lessonScopeKey},
	}
	triples := make([]message.Triple, 0, len(objects))
	for _, item := range objects {
		triples = append(triples, message.Triple{
			Subject: entityID, Predicate: item.predicate, Object: item.object,
			Source: fixtureSource, Context: lessonCreateRequestID,
			Timestamp: fixtureTimestamp, Confidence: 1,
		})
	}
	return productLessonFixture{
		entityID: entityID, messageType: agentic.AgentLessonMessageType(),
		triples: triples, timestamp: fixtureTimestamp,
	}
}

func requireEvidenceAuthority(exact *graph.ExactEntity) error {
	want := evidenceCreateMutation()
	if exact == nil || exact.Entity == nil {
		return errors.New("evidence authority is empty")
	}
	if exact.KVRevision == 0 {
		return errors.New("evidence authority has zero KV revision")
	}
	if exact.Entity.ID != evidenceEntityID || exact.Entity.MessageType != want.Entity.MessageType ||
		exact.Entity.Version != 1 || !exact.Entity.UpdatedAt.Equal(fixtureTimestamp) {
		return fmt.Errorf("evidence envelope = %+v, want fixed test.fixture.v1 envelope", exact.Entity)
	}
	callerTriples, err := requireCanonicalIndexingProfile(
		exact.Entity.ID, exact.Entity.Triples, vocabulary.IndexingProfileControl)
	if err != nil {
		return fmt.Errorf("evidence indexing profile: %w", err)
	}
	return requireExactTriples(callerTriples, want.Triples)
}

func requireProposedAuthority(exact *graph.ExactEntity, fixture productLessonFixture) error {
	if err := requireLessonEnvelope(exact, fixture); err != nil {
		return err
	}
	callerTriples, err := requireCanonicalIndexingProfile(
		exact.Entity.ID, exact.Entity.Triples, vocabulary.IndexingProfileContent) // ADR-103 O-3: the lesson's registered floor
	if err != nil {
		return fmt.Errorf("proposed indexing profile: %w", err)
	}
	if err := requireExactTriples(callerTriples, fixture.triples); err != nil {
		return fmt.Errorf("proposed tuples: %w", err)
	}
	return requireAbsentAttributionAndSiblings(callerTriples)
}

func requireActiveAuthority(exact *graph.ExactEntity, fixture productLessonFixture) error {
	if err := requireLessonEnvelope(exact, fixture); err != nil {
		return err
	}
	callerTriples, err := requireCanonicalIndexingProfile(
		exact.Entity.ID, exact.Entity.Triples, vocabulary.IndexingProfileContent) // ADR-103 O-3: the lesson's registered floor
	if err != nil {
		return fmt.Errorf("active indexing profile: %w", err)
	}
	if err := requireAbsentAttributionAndSiblings(callerTriples); err != nil {
		return err
	}
	statusCount := 0
	for _, triple := range callerTriples {
		if triple.Predicate == agvocab.LessonStatus {
			statusCount++
			if triple.Object != "active" {
				return fmt.Errorf("active status object = %v", triple.Object)
			}
		}
	}
	if statusCount != 1 {
		return fmt.Errorf("active status tuple count = %d, want 1", statusCount)
	}
	return requireExactTriples(nonLifecycle(callerTriples), nonLifecycle(fixture.triples))
}

// requireCanonicalIndexingProfile separates the framework-owned create stamp
// from caller-authored facts and validates the exact single-valued shape that
// graph-ingest's appendIndexingProfileTriple emits.
func requireCanonicalIndexingProfile(
	entityID string,
	triples []message.Triple,
	expectedProfile string,
) ([]message.Triple, error) {
	callerTriples := make([]message.Triple, 0, len(triples))
	profileTriples := make([]message.Triple, 0, 1)
	for _, triple := range triples {
		if triple.Predicate == vocabulary.EntityIndexingProfile {
			profileTriples = append(profileTriples, triple)
			continue
		}
		callerTriples = append(callerTriples, triple)
	}
	if len(profileTriples) != 1 {
		return nil, fmt.Errorf("stamp count = %d, want 1", len(profileTriples))
	}
	profile := profileTriples[0]
	if profile.Subject != entityID {
		return nil, fmt.Errorf("subject = %q, want %q", profile.Subject, entityID)
	}
	object, ok := profile.Object.(string)
	if !ok || object != expectedProfile {
		return nil, fmt.Errorf("object = %#v, want %q", profile.Object, expectedProfile)
	}
	if profile.Source != indexingProfileSource {
		return nil, fmt.Errorf("source = %q, want %q", profile.Source, indexingProfileSource)
	}
	if profile.Timestamp.IsZero() {
		return nil, errors.New("timestamp is zero")
	}
	if profile.Confidence != 1 {
		return nil, fmt.Errorf("confidence = %v, want 1", profile.Confidence)
	}
	if profile.Context != "" || profile.Datatype != "" || profile.ExpiresAt != nil {
		return nil, fmt.Errorf(
			"optional metadata = context:%q datatype:%q expires_at:%v, want empty/empty/nil",
			profile.Context, profile.Datatype, profile.ExpiresAt)
	}
	return callerTriples, nil
}

func requireLessonEnvelope(exact *graph.ExactEntity, fixture productLessonFixture) error {
	if exact == nil || exact.Entity == nil {
		return errors.New("lesson authority is empty")
	}
	if exact.KVRevision == 0 {
		return errors.New("lesson authority has zero KV revision")
	}
	if exact.Entity.ID != fixture.entityID {
		return fmt.Errorf("lesson entity ID = %q, want %q", exact.Entity.ID, fixture.entityID)
	}
	if exact.Entity.MessageType != fixture.messageType {
		return fmt.Errorf("lesson message type = %q, want %q", exact.Entity.MessageType.Key(), fixture.messageType.Key())
	}
	return nil
}

func requireAbsentAttributionAndSiblings(triples []message.Triple) error {
	absent := map[string]bool{
		agvocab.LessonObservedRole: true,
		agvocab.ActionExecutedBy:   true,
		agvocab.LessonRetiredAt:    true,
		agvocab.LessonSupersededBy: true,
	}
	for _, triple := range triples {
		if absent[triple.Predicate] {
			return fmt.Errorf("unexpected predicate %q", triple.Predicate)
		}
	}
	return nil
}

func nonLifecycle(triples []message.Triple) []message.Triple {
	result := make([]message.Triple, 0, len(triples))
	for _, triple := range triples {
		switch triple.Predicate {
		case agvocab.LessonStatus, agvocab.LessonRetiredAt, agvocab.LessonSupersededBy:
			continue
		default:
			result = append(result, triple)
		}
	}
	return result
}

type completeTuple struct {
	Subject    string
	Predicate  string
	Object     string
	Source     string
	Timestamp  string
	Confidence float64
	Context    string
	Datatype   string
	ExpiresAt  *string
}

func requireExactTriples(got, want []message.Triple) error {
	gotKeys, err := tupleKeys(got)
	if err != nil {
		return err
	}
	wantKeys, err := tupleKeys(want)
	if err != nil {
		return err
	}
	if len(gotKeys) != len(wantKeys) {
		return fmt.Errorf("tuple count = %d, want %d; got=%v want=%v", len(gotKeys), len(wantKeys), gotKeys, wantKeys)
	}
	for i := range gotKeys {
		if gotKeys[i] != wantKeys[i] {
			return fmt.Errorf("tuple[%d] = %s, want %s", i, gotKeys[i], wantKeys[i])
		}
	}
	return nil
}

func tupleKeys(triples []message.Triple) ([]string, error) {
	keys := make([]string, 0, len(triples))
	for _, triple := range triples {
		object, err := json.Marshal(triple.Object)
		if err != nil {
			return nil, fmt.Errorf("marshal object for %s: %w", triple.Predicate, err)
		}
		var expiresAt *string
		if triple.ExpiresAt != nil {
			formatted := triple.ExpiresAt.UTC().Format(time.RFC3339Nano)
			expiresAt = &formatted
		}
		encoded, err := json.Marshal(completeTuple{
			Subject: triple.Subject, Predicate: triple.Predicate, Object: string(object),
			Source: triple.Source, Timestamp: triple.Timestamp.UTC().Format(time.RFC3339Nano),
			Confidence: triple.Confidence, Context: triple.Context, Datatype: triple.Datatype,
			ExpiresAt: expiresAt,
		})
		if err != nil {
			return nil, err
		}
		keys = append(keys, string(encoded))
	}
	sort.Strings(keys)
	return keys, nil
}

func requireReaderTarget(candidates []lessonmatch.Lesson, entityID, status, injection string) error {
	count := 0
	for _, candidate := range candidates {
		if candidate.EntityID != entityID {
			continue
		}
		count++
		if candidate.Status != status || candidate.InjectionForm != injection {
			return fmt.Errorf("reader target = %+v, want status=%s injection=%q", candidate, status, injection)
		}
	}
	if count != 1 {
		return fmt.Errorf("reader target count = %d, want 1", count)
	}
	return nil
}

func requireExactMatch(result lessonmatch.Result, entityID, injection string) error {
	if result.MatchedCount != 1 || result.IncludedCount != 1 || len(result.Included) != 1 {
		return fmt.Errorf("matcher result = %+v, want one matched and included", result)
	}
	item := result.Included[0]
	if item.EntityID != entityID || item.InjectionForm != injection {
		return fmt.Errorf("included lesson = %+v, want %s %q", item, entityID, injection)
	}
	return nil
}

func cleanupTracked(ctx context.Context, cleaner authoritativeCleaner, tracked []string) error {
	if cleaner == nil {
		if len(tracked) == 0 {
			return nil
		}
		return errors.New("cleanup authority client is unavailable")
	}
	var cleanupErr error
	for i := len(tracked) - 1; i >= 0; i-- {
		entityID := tracked[i]
		exact, err := cleaner.ReadAuthoritative(ctx, entityID)
		if err != nil {
			if isProjectionNotFound(err) {
				continue
			}
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup read %s: %w", entityID, err))
			continue
		}
		if exact == nil || exact.Entity == nil || exact.Entity.ID != entityID || exact.KVRevision == 0 {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup read %s returned no revision", entityID))
			continue
		}
		receipt, err := cleaner.Delete(ctx, projection.DeleteMutation{
			EntityID: entityID, ExpectedRevision: exact.KVRevision,
			Metadata: projection.MutationMetadata{RequestID: "e2e-lessons-cleanup:" + entityID},
		})
		if err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup delete %s: %w", entityID, err))
			continue
		}
		if receipt.Commit != projection.CommitVerified {
			cleanupErr = errors.Join(cleanupErr,
				fmt.Errorf("cleanup delete %s commit = %q, want %q", entityID, receipt.Commit, projection.CommitVerified))
		}
	}
	return cleanupErr
}

func isProjectionNotFound(err error) bool {
	var mutationErr *projection.MutationError
	return errors.As(err, &mutationErr) && mutationErr.Kind == projection.MutationNotFound
}
