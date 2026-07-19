package agentictools

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// fakeOwnedFactWriter models the production owned-fact REPLACE lane faithfully:
// ReplaceTriples applies removePredicates then graph.MergeTriples
// (replace-by-(subject,predicate)) — the SAME merge update_with_triples runs —
// so a test can prove single-valued replace, not append. It enforces must-exist:
// a replace on an un-born entity surfaces the classified entity_not_found code,
// exactly as graph-ingest does.
type fakeOwnedFactWriter struct {
	mu           sync.Mutex
	born         map[string][]message.Triple
	replaceErr   error
	replaceCalls int
	lastRemove   []string
}

func newFakeOwnedFactWriter() *fakeOwnedFactWriter {
	return &fakeOwnedFactWriter{born: map[string][]message.Triple{}}
}

// birth seeds an already-created entity with its current triples.
func (w *fakeOwnedFactWriter) birth(entityID string, triples ...message.Triple) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.born[entityID] = triples
}

func (w *fakeOwnedFactWriter) ReplaceTriples(_ context.Context, entityID string, add []message.Triple, removePredicates []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.replaceCalls++
	w.lastRemove = removePredicates
	if w.replaceErr != nil {
		return w.replaceErr
	}
	cur, ok := w.born[entityID]
	if !ok {
		return errors.New("replace_triples mutation failed [" + graph.ErrorCodeEntityNotFound + "]: entity not found")
	}
	if len(removePredicates) > 0 {
		rm := make(map[string]struct{}, len(removePredicates))
		for _, p := range removePredicates {
			rm[p] = struct{}{}
		}
		kept := make([]message.Triple, 0, len(cur))
		for _, t := range cur {
			if _, drop := rm[t.Predicate]; !drop {
				kept = append(kept, t)
			}
		}
		cur = kept
	}
	w.born[entityID] = graph.MergeTriples(cur, add)
	return nil
}

func (w *fakeOwnedFactWriter) ReadOwnedPredicates(_ context.Context, _ string, _ string) ([]string, error) {
	return nil, nil
}

// objects returns the object strings of every triple on entityID with predicate.
func (w *fakeOwnedFactWriter) objects(entityID, predicate string) []string {
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

func (r *fakeLessonReader) ReadLessonEvidence(_ context.Context, lessonEntityID string) ([]string, bool, error) {
	if r.evidenceErr != nil {
		return nil, false, r.evidenceErr
	}
	ev, ok := r.evidence[lessonEntityID]
	if !ok {
		return nil, false, nil
	}
	return ev, true, nil
}

func (r *fakeLessonReader) EntityExists(_ context.Context, entityID string) (bool, error) {
	if r.existsErr != nil {
		return false, r.existsErr
	}
	return r.present[entityID], nil
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

// --- Promotion happy path: proposed→active when all evidence exists ---

func TestLessonCurator_Promote_HappyPath(t *testing.T) {
	w := newFakeOwnedFactWriter()
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
	w := newFakeOwnedFactWriter()
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
	w := newFakeOwnedFactWriter()
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
	c := NewLessonCurator(newFakeOwnedFactWriter(), newFakeLessonReader(), nil)
	if err := c.Promote(context.Background(), "not-an-entity-id"); err == nil {
		t.Fatal("promote must reject a malformed entity ID")
	}
}

// --- Re-promoting stays single-valued (idempotent-ish; no append) ---

func TestLessonCurator_Promote_TwiceStaysSingleValued(t *testing.T) {
	w := newFakeOwnedFactWriter()
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
	w := newFakeOwnedFactWriter()
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
	w := newFakeOwnedFactWriter()
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
	w := newFakeOwnedFactWriter()
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
}

func TestLessonCurator_Supersede_RejectsMalformedByID(t *testing.T) {
	w := newFakeOwnedFactWriter()
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
	w := newFakeOwnedFactWriter() // entity never born
	c := NewLessonCurator(w, newFakeLessonReader(), nil)
	err := c.Retire(context.Background(), testLessonID)
	if err == nil || !strings.Contains(err.Error(), graph.ErrorCodeEntityNotFound) {
		t.Fatalf("retire of an un-born lesson must surface entity_not_found, got: %v", err)
	}
}
