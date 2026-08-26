package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/pkg/projection"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// fakeLessonReplacer models complete lifecycle-group reconciliation and
// enforces must-exist, matching the public projection capability.
type fakeLessonReplacer struct {
	mu           sync.Mutex
	born         map[string][]message.Triple
	replaceErr   error
	replaceCalls int
	lastRequest  projection.ReconcileMutation
}

func newFakeLessonReplacer() *fakeLessonReplacer {
	return &fakeLessonReplacer{born: map[string][]message.Triple{}}
}

// birth seeds an already-created entity with its current triples.
func (w *fakeLessonReplacer) birth(entityID string, triples ...message.Triple) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.born[entityID] = triples
}

func (w *fakeLessonReplacer) Reconcile(_ context.Context, req projection.ReconcileMutation) (projection.MutationReceipt, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.replaceCalls++
	w.lastRequest = req
	if w.replaceErr != nil {
		return projection.MutationReceipt{}, w.replaceErr
	}
	cur, ok := w.born[req.EntityID]
	if !ok {
		return projection.MutationReceipt{}, errors.New("replace mutation failed [" + graph.ErrorCodeEntityNotFound + "]: entity not found")
	}
	rm := map[string]struct{}{
		agvocab.LessonStatus: {}, agvocab.LessonSupersededBy: {}, agvocab.LessonRetiredAt: {},
	}
	kept := make([]message.Triple, 0, len(cur))
	for _, triple := range cur {
		if _, drop := rm[triple.Predicate]; !drop {
			kept = append(kept, triple)
		}
	}
	w.born[req.EntityID] = graph.MergeTriples(kept, req.Desired)
	return projection.MutationReceipt{Commit: projection.CommitVerified}, nil
}

// objects returns the object strings of every triple on entityID with predicate.
func (w *fakeLessonReplacer) objects(entityID, predicate string) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []string
	for _, t := range w.born[entityID] {
		if t.Predicate == predicate {
			if s, ok := t.Object.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// fakeLessonReader is an in-memory LessonReader. evidence maps a lesson to its
// cited evidence IDs (absence ⇒ lesson not found); present is the set of
// entities that exist in the graph (evidence-existence resolution).
type fakeLessonReader struct {
	evidence    map[string][]string
	present     map[string]bool
	evidenceErr error
	existsErr   error
}

func newFakeLessonReader() *fakeLessonReader {
	return &fakeLessonReader{evidence: map[string][]string{}, present: map[string]bool{}}
}

func (r *fakeLessonReader) ReadAuthoritative(_ context.Context, entityID string) (*graph.ExactEntity, error) {
	if evidence, ok := r.evidence[entityID]; ok {
		if r.evidenceErr != nil {
			return nil, r.evidenceErr
		}
		triples := make([]message.Triple, 0, len(evidence))
		for _, evidenceID := range evidence {
			triples = append(triples, message.Triple{
				Subject: entityID, Predicate: agvocab.LessonEvidence, Object: evidenceID,
			})
		}
		return &graph.ExactEntity{Entity: &graph.EntityState{ID: entityID, Triples: triples}, KVRevision: 1}, nil
	}
	if r.existsErr != nil {
		return nil, r.existsErr
	}
	if r.present[entityID] {
		return &graph.ExactEntity{Entity: &graph.EntityState{ID: entityID}, KVRevision: 1}, nil
	}
	return nil, &projection.MutationError{Kind: projection.MutationNotFound, Err: errors.New("entity not found")}
}

const (
	testLessonID   = "acme.ops.agent.lesson.record.11111111-1111-5111-8111-111111111111"
	testEvidence1  = "acme.ops.agent.agentic-loop.execution.loop-a"
	testEvidence2  = "acme.ops.agent.agentic-loop.execution.loop-b"
	testSupersedID = "acme.ops.agent.lesson.record.22222222-2222-5222-8222-222222222222"
)

func statusTriple(entityID, status string) message.Triple {
	return message.Triple{Subject: entityID, Predicate: agvocab.LessonStatus, Object: status, Confidence: 1.0}
}

func TestLessonProjectionContractMatchesCanonicalAndReturnsIndependentSnapshots(t *testing.T) {
	canonicalJSON := marshalProjectionContract(t, agentic.LessonContract())
	assertContractJSON(t, "initial public snapshot", canonicalJSON, LessonProjectionContract())

	publicSnapshot := LessonProjectionContract()
	mutateProjectionContractSlices(t, &publicSnapshot)
	assertContractJSON(t, "public snapshot after public mutation", canonicalJSON, LessonProjectionContract())
	assertContractJSON(t, "internal aggregate after public mutation", canonicalJSON, lessonContractFromAggregate(t))

	internalAggregateSnapshot := lessonContractFromAggregate(t)
	mutateProjectionContractSlices(t, &internalAggregateSnapshot)
	assertContractJSON(t, "public snapshot after aggregate mutation", canonicalJSON, LessonProjectionContract())

	internalCanonicalSnapshot := agentic.LessonContract()
	mutateProjectionContractSlices(t, &internalCanonicalSnapshot)
	assertContractJSON(t, "public snapshot after canonical-helper mutation", canonicalJSON, LessonProjectionContract())
}

func marshalProjectionContract(t *testing.T, contract projection.Contract) string {
	t.Helper()
	encoded, err := json.Marshal(contract)
	if err != nil {
		t.Fatalf("marshal projection contract: %v", err)
	}
	return string(encoded)
}

func assertContractJSON(t *testing.T, name, want string, got projection.Contract) {
	t.Helper()
	if encoded := marshalProjectionContract(t, got); encoded != want {
		t.Fatalf("%s = %s, want immutable canonical %s", name, encoded, want)
	}
}

func lessonContractFromAggregate(t *testing.T) projection.Contract {
	t.Helper()
	for _, contract := range payloadregistry.NewWithSubset(t, agentic.RegisterPayloads).Contracts() {
		if contract.Name == agentic.LessonRecordContractName {
			return contract
		}
	}
	t.Fatal("the agentic registry has no canonical lesson contract")
	return projection.Contract{}
}

func mutateProjectionContractSlices(t *testing.T, contract *projection.Contract) {
	t.Helper()
	if len(contract.BirthPredicates) == 0 || len(contract.Groups) == 0 {
		t.Fatalf("contract lacks mutable top-level slices: %#v", contract)
	}
	contract.BirthPredicates[0] = "mutated.birth.predicate"
	contract.BirthPredicates = append(contract.BirthPredicates, "mutated.birth.appended")
	for index := range contract.Groups {
		if len(contract.Groups[index].Predicates) == 0 {
			t.Fatalf("group[%d] lacks nested predicate slice: %#v", index, contract.Groups[index])
		}
		contract.Groups[index].Predicates[0] = "mutated.group.predicate"
		contract.Groups[index].Predicates = append(
			contract.Groups[index].Predicates,
			"mutated.group.appended",
		)
	}
	contract.Groups[0].Name = "mutated-group"
	contract.Groups = append(contract.Groups, projection.PredicateGroup{Name: "mutated-appended-group"})
}

// --- Promotion happy path: proposed→active when all evidence exists ---

func TestLessonCurator_Promote_HappyPath(t *testing.T) {
	w := newFakeLessonReplacer()
	w.birth(testLessonID, statusTriple(testLessonID, lessonBornStatus))
	r := newFakeLessonReader()
	r.evidence[testLessonID] = []string{testEvidence1, testEvidence2}
	r.present[testEvidence1] = true
	r.present[testEvidence2] = true

	c := NewLessonCurator(w, r, nil)
	if err := c.Promote(context.Background(), testLessonID); err != nil {
		t.Fatalf("promote must succeed when all evidence exists: %v", err)
	}

	status := w.objects(testLessonID, agvocab.LessonStatus)
	if len(status) != 1 {
		t.Fatalf("single-valued replace: want exactly one status triple, got %d (%v)", len(status), status)
	}
	if status[0] != lessonStatusActive {
		t.Errorf("status = %q, want active (proposed replaced, not appended)", status[0])
	}
	if w.replaceCalls != 1 {
		t.Errorf("replaceCalls = %d, want 1", w.replaceCalls)
	}
}

// --- Promotion REFUSED + stays proposed when a cited evidence entity is missing ---

func TestLessonCurator_Promote_RefusedWhenEvidenceMissing(t *testing.T) {
	w := newFakeLessonReplacer()
	w.birth(testLessonID, statusTriple(testLessonID, lessonBornStatus))
	r := newFakeLessonReader()
	r.evidence[testLessonID] = []string{testEvidence1, testEvidence2}
	r.present[testEvidence1] = true
	// testEvidence2 deliberately absent.

	c := NewLessonCurator(w, r, nil)
	err := c.Promote(context.Background(), testLessonID)
	if err == nil {
		t.Fatal("promote must be REFUSED when a cited evidence entity is missing")
	}
	if !strings.Contains(err.Error(), testEvidence2) {
		t.Errorf("error must name the missing evidence entity, got: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "proposed") {
		t.Errorf("error must state the lesson remains proposed, got: %v", err)
	}
	if w.replaceCalls != 0 {
		t.Errorf("a refused promotion must not write (replaceCalls=%d)", w.replaceCalls)
	}
	if got := w.objects(testLessonID, agvocab.LessonStatus); len(got) != 1 || got[0] != lessonBornStatus {
		t.Errorf("status must remain proposed after refusal, got %v", got)
	}
}

func TestLessonCurator_Promote_LessonNotFound(t *testing.T) {
	w := newFakeLessonReplacer()
	r := newFakeLessonReader() // no evidence entry ⇒ lesson absent

	c := NewLessonCurator(w, r, nil)
	err := c.Promote(context.Background(), testLessonID)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Fatalf("promote of an absent lesson must error naming not-found, got: %v", err)
	}
	if w.replaceCalls != 0 {
		t.Errorf("no write on a missing lesson (replaceCalls=%d)", w.replaceCalls)
	}
}

func TestLessonCurator_Promote_RejectsMalformedID(t *testing.T) {
	c := NewLessonCurator(newFakeLessonReplacer(), newFakeLessonReader(), nil)
	if err := c.Promote(context.Background(), "not-an-entity-id"); err == nil {
		t.Fatal("promote must reject a malformed entity ID")
	}
}

// --- Re-promoting stays single-valued (idempotent-ish; no append) ---

func TestLessonCurator_Promote_TwiceStaysSingleValued(t *testing.T) {
	w := newFakeLessonReplacer()
	w.birth(testLessonID, statusTriple(testLessonID, lessonBornStatus))
	r := newFakeLessonReader()
	r.evidence[testLessonID] = []string{testEvidence1}
	r.present[testEvidence1] = true

	c := NewLessonCurator(w, r, nil)
	if err := c.Promote(context.Background(), testLessonID); err != nil {
		t.Fatalf("first promote: %v", err)
	}
	if err := c.Promote(context.Background(), testLessonID); err != nil {
		t.Fatalf("second promote: %v", err)
	}
	if got := w.objects(testLessonID, agvocab.LessonStatus); len(got) != 1 || got[0] != lessonStatusActive {
		t.Errorf("re-promotion must not append a second status triple, got %v", got)
	}
}

// --- Retirement: status→retired + retired-at ---

func TestLessonCurator_Retire(t *testing.T) {
	w := newFakeLessonReplacer()
	w.birth(testLessonID, statusTriple(testLessonID, lessonStatusActive))
	c := NewLessonCurator(w, newFakeLessonReader(), nil)

	if err := c.Retire(context.Background(), testLessonID); err != nil {
		t.Fatalf("retire: %v", err)
	}
	status := w.objects(testLessonID, agvocab.LessonStatus)
	if len(status) != 1 || status[0] != lessonStatusRetired {
		t.Errorf("status = %v, want single [retired]", status)
	}
	retiredAt := w.objects(testLessonID, agvocab.LessonRetiredAt)
	if len(retiredAt) != 1 || retiredAt[0] == "" {
		t.Errorf("retired-at = %v, want a single non-empty timestamp", retiredAt)
	}
}

func TestLessonCurator_Retire_NoEvidenceCheck(t *testing.T) {
	// Retirement must not consult the reader; a reader that errors on every call
	// proves retirement never resolves evidence.
	w := newFakeLessonReplacer()
	w.birth(testLessonID, statusTriple(testLessonID, lessonStatusActive))
	r := newFakeLessonReader()
	r.evidenceErr = errors.New("reader must not be called")
	r.existsErr = errors.New("reader must not be called")
	c := NewLessonCurator(w, r, nil)

	if err := c.Retire(context.Background(), testLessonID); err != nil {
		t.Fatalf("retire must not resolve evidence: %v", err)
	}
}

// --- Supersession: status→superseded + superseded-by ---

func TestLessonCurator_Supersede(t *testing.T) {
	w := newFakeLessonReplacer()
	w.birth(testLessonID, statusTriple(testLessonID, lessonStatusActive))
	c := NewLessonCurator(w, newFakeLessonReader(), nil)

	if err := c.Supersede(context.Background(), testLessonID, testSupersedID); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	status := w.objects(testLessonID, agvocab.LessonStatus)
	if len(status) != 1 || status[0] != lessonStatusSuperseded {
		t.Errorf("status = %v, want single [superseded]", status)
	}
	by := w.objects(testLessonID, agvocab.LessonSupersededBy)
	if len(by) != 1 || by[0] != testSupersedID {
		t.Errorf("superseded-by = %v, want single [%s]", by, testSupersedID)
	}
	if w.lastRequest.Contract != "agentic.lesson-record" ||
		w.lastRequest.Group != "lesson-lifecycle" {
		t.Errorf("replace contract/group = %q/%q", w.lastRequest.Contract, w.lastRequest.Group)
	}
}

func TestLessonCurator_TransitionsClearMutuallyExclusiveSiblings(t *testing.T) {
	w := newFakeLessonReplacer()
	w.birth(testLessonID,
		statusTriple(testLessonID, lessonStatusSuperseded),
		message.Triple{
			Subject: testLessonID, Predicate: agvocab.LessonSupersededBy,
			Object: testSupersedID,
		},
	)
	c := NewLessonCurator(w, newFakeLessonReader(), nil)

	if err := c.Retire(context.Background(), testLessonID); err != nil {
		t.Fatalf("retire: %v", err)
	}
	if got := w.objects(testLessonID, agvocab.LessonSupersededBy); len(got) != 0 {
		t.Fatalf("retire retained superseded-by sibling: %v", got)
	}

	if err := c.Supersede(context.Background(), testLessonID, testSupersedID); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if got := w.objects(testLessonID, agvocab.LessonRetiredAt); len(got) != 0 {
		t.Fatalf("supersede retained retired-at sibling: %v", got)
	}
}

func TestLessonCurator_Supersede_RejectsMalformedByID(t *testing.T) {
	w := newFakeLessonReplacer()
	w.birth(testLessonID, statusTriple(testLessonID, lessonStatusActive))
	c := NewLessonCurator(w, newFakeLessonReader(), nil)

	err := c.Supersede(context.Background(), testLessonID, "bad-id")
	if err == nil || !strings.Contains(err.Error(), "superseded-by") {
		t.Fatalf("supersede must reject a malformed byEntityID, got: %v", err)
	}
	if w.replaceCalls != 0 {
		t.Errorf("no write on a malformed byEntityID (replaceCalls=%d)", w.replaceCalls)
	}
}

// --- Writer errors surface (must-exist and transport) ---

func TestLessonCurator_Retire_MustExistSurfaces(t *testing.T) {
	w := newFakeLessonReplacer() // entity never born
	c := NewLessonCurator(w, newFakeLessonReader(), nil)
	err := c.Retire(context.Background(), testLessonID)
	if err == nil || !strings.Contains(err.Error(), graph.ErrorCodeEntityNotFound) {
		t.Fatalf("retire of an un-born lesson must surface entity_not_found, got: %v", err)
	}
}

// TestLessonProjectionContractIsTheRegisteredContract: the public snapshot is
// the contract the registry holds for agentic.agent_lesson.v1 — one table.
func TestLessonProjectionContractIsTheRegisteredContract(t *testing.T) {
	reg := payloadregistry.NewWithSubset(t, agentic.RegisterPayloads)
	registered, ok := reg.GetRegistration(agentic.AgentLessonMessageType().Key())
	if !ok {
		t.Fatal("agentic.agent_lesson.v1 is not registered")
	}
	if len(registered.Contracts) != 1 {
		t.Fatalf("registered lesson contracts = %d, want 1", len(registered.Contracts))
	}
	assertContractJSON(t, "registered lesson contract", marshalProjectionContract(t, registered.Contracts[0]), LessonProjectionContract())
}
