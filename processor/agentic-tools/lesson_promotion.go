package agentictools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// lessonCuratorSource tags the lifecycle triples this writer publishes so
// operators can distinguish a curator promotion/retirement from the ops
// agent's original emit_lesson births at a glance (mirrors emitLessonSource).
const lessonCuratorSource = "ops-lesson-curator"

// Lesson lifecycle status values the curator writes. Born status is
// lessonBornStatus ("proposed", emit_lesson.go); these are the reachable
// transitions. Only "active" lessons are injectable at brief assembly.
const (
	lessonStatusActive     = "active"
	lessonStatusRetired    = "retired"
	lessonStatusSuperseded = "superseded"
)

// LessonReader is the narrow graph read surface the promotion writer needs to
// resolve evidence existence before flipping a lesson active (ADR-080: the
// evidence citation is well-formed-gated at emit and EXISTENCE-resolved at
// promotion). It is deliberately read-only and separate from the write surface
// (OwnedFactWriter): promotion reads, decides, then writes through the owned
// lane. Production satisfies it with a natsclient adapter; tests use a fake.
type LessonReader interface {
	// ReadLessonEvidence returns the evidence entity IDs the lesson cites
	// (its agent.lesson.evidence objects), with found=false when the lesson
	// entity itself is absent from the graph.
	ReadLessonEvidence(ctx context.Context, lessonEntityID string) (evidence []string, found bool, err error)
	// EntityExists reports whether entityID is currently present in the graph.
	EntityExists(ctx context.Context, entityID string) (bool, error)
}

// LessonCurator is the reference VALIDATED lesson lifecycle writer (ADR-080
// gated lifecycle, task 4.1). It promotes, retires, and supersedes lesson
// records through the canonical owned-fact REPLACE lane (ADR-056), composing an
// OwnedFactWriter (write) and a LessonReader (evidence resolution). Every
// transition is a SINGLE-VALUED replace, not an append: the update_with_triples
// merge is replace-by-(subject,predicate) (graph.MergeTriples), so writing a
// fresh agent.lesson.status triple drops the prior value and leaves exactly one.
//
// This is the OPERATOR/PRODUCT curation path, NOT an agent tool: ADR-080 makes
// operator/product review the default promotion gate, so the framework ships no
// `promote_lesson` tool. A product may wrap Promote in a curation UI, a rule
// `replace_owned` action (for mechanical/retirement transitions), or an
// explicit auto-promotion policy — but the evidence-existence resolution below
// is what makes a promotion HONEST, so the validated path routes through here.
type LessonCurator struct {
	writer OwnedFactWriter
	reader LessonReader
	logger *slog.Logger
}

// NewLessonCurator builds a curator over an explicit write + read surface
// (testable with fakes). A nil logger falls back to slog.Default().
func NewLessonCurator(writer OwnedFactWriter, reader LessonReader, logger *slog.Logger) *LessonCurator {
	if logger == nil {
		logger = slog.Default()
	}
	return &LessonCurator{writer: writer, reader: reader, logger: logger}
}

// NewNATSLessonCurator wires a curator backed by the shared graph
// mutation/query NATS surfaces (the same update_with_triples + query.entity
// lanes emit_lesson and OwnedFactWriter use).
func NewNATSLessonCurator(client *natsclient.Client, logger *slog.Logger) *LessonCurator {
	return NewLessonCurator(NewNATSOwnedFactWriter(client), NewNATSLessonReader(client), logger)
}

// Promote flips a lesson proposed→active, but ONLY after resolving that every
// cited evidence entity exists in the graph. If ANY cited evidence entity is
// missing the promotion is REFUSED with an instructive error and the lesson is
// left untouched (status stays proposed) — spec scenario "Promotion resolves
// evidence existence". This is the validated promotion path; the bare rule
// `replace_owned` lane performs no such check and is for mechanical transitions.
func (c *LessonCurator) Promote(ctx context.Context, lessonEntityID string) error {
	if !message.IsValidEntityID(lessonEntityID) {
		return fmt.Errorf("promote lesson: %q is not a well-formed 6-part entity ID", lessonEntityID)
	}

	evidence, found, err := c.reader.ReadLessonEvidence(ctx, lessonEntityID)
	if err != nil {
		return fmt.Errorf("promote lesson %s: read cited evidence: %w", lessonEntityID, err)
	}
	if !found {
		return fmt.Errorf("promote lesson %s: refused — lesson entity not found in the graph", lessonEntityID)
	}
	if len(evidence) == 0 {
		// Defensive: emit_lesson requires >=1 evidence, so this only fires if a
		// lesson lost its evidence out-of-band. A promotion still must not fabricate.
		return fmt.Errorf("promote lesson %s: refused — lesson cites no evidence; it remains proposed", lessonEntityID)
	}

	var missing []string
	for _, ev := range evidence {
		exists, existsErr := c.reader.EntityExists(ctx, ev)
		if existsErr != nil {
			return fmt.Errorf("promote lesson %s: resolve evidence %s: %w", lessonEntityID, ev, existsErr)
		}
		if !exists {
			missing = append(missing, ev)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"promote lesson %s: refused — %d of %d cited evidence entities are absent from the graph %v; the lesson remains proposed until its evidence resolves",
			lessonEntityID, len(missing), len(evidence), missing)
	}

	// All evidence resolved: single-valued replace proposed→active.
	if err := c.replace(ctx, lessonEntityID,
		lessonTriple(lessonEntityID, agvocab.LessonStatus, lessonStatusActive)); err != nil {
		return fmt.Errorf("promote lesson %s: %w", lessonEntityID, err)
	}
	c.logger.Info("lesson promoted to active",
		slog.String("lesson", lessonEntityID),
		slog.Int("evidence_resolved", len(evidence)))
	return nil
}

// Retire flips a lesson's status to retired and stamps agent.lesson.retired-at.
// No evidence check: retirement removes a lesson from future briefs and does
// not assert anything about the world, so it never fails on evidence. The
// entity remains durable in the graph for audit (spec scenario "Retired lesson
// leaves the brief, not the graph"). Both writes are single-valued replaces.
func (c *LessonCurator) Retire(ctx context.Context, lessonEntityID string) error {
	if !message.IsValidEntityID(lessonEntityID) {
		return fmt.Errorf("retire lesson: %q is not a well-formed 6-part entity ID", lessonEntityID)
	}
	now := time.Now()
	if err := c.replace(ctx, lessonEntityID,
		lessonTripleAt(lessonEntityID, agvocab.LessonStatus, lessonStatusRetired, now),
		lessonTripleAt(lessonEntityID, agvocab.LessonRetiredAt, now.UTC().Format(time.RFC3339), now),
	); err != nil {
		return fmt.Errorf("retire lesson %s: %w", lessonEntityID, err)
	}
	c.logger.Info("lesson retired", slog.String("lesson", lessonEntityID))
	return nil
}

// Supersede flips a lesson's status to superseded and records the replacing
// lesson's entity ID in agent.lesson.superseded-by. byEntityID must be a
// well-formed 6-part entity ID (the replacing lesson). The superseded lesson
// remains durable for audit. Both writes are single-valued replaces.
func (c *LessonCurator) Supersede(ctx context.Context, lessonEntityID, byEntityID string) error {
	if !message.IsValidEntityID(lessonEntityID) {
		return fmt.Errorf("supersede lesson: %q is not a well-formed 6-part entity ID", lessonEntityID)
	}
	if !message.IsValidEntityID(byEntityID) {
		return fmt.Errorf("supersede lesson %s: superseded-by %q is not a well-formed 6-part entity ID", lessonEntityID, byEntityID)
	}
	now := time.Now()
	if err := c.replace(ctx, lessonEntityID,
		lessonTripleAt(lessonEntityID, agvocab.LessonStatus, lessonStatusSuperseded, now),
		lessonTripleAt(lessonEntityID, agvocab.LessonSupersededBy, byEntityID, now),
	); err != nil {
		return fmt.Errorf("supersede lesson %s: %w", lessonEntityID, err)
	}
	c.logger.Info("lesson superseded",
		slog.String("lesson", lessonEntityID), slog.String("by", byEntityID))
	return nil
}

// replace writes the lifecycle triples through the owned-fact REPLACE lane. No
// removePredicates are needed for a same-predicate flip: update_with_triples
// merges via graph.MergeTriples (replace-by-(subject,predicate)), so a fresh
// value for an already-present single-valued predicate fully replaces the prior
// one — the append that a bare triple.add would produce is what this lane
// deliberately avoids. MUST-EXIST: a never-created lesson surfaces the
// classified entity_not_found from the handler.
func (c *LessonCurator) replace(ctx context.Context, lessonEntityID string, add ...message.Triple) error {
	return c.writer.ReplaceTriples(ctx, lessonEntityID, add, nil)
}

// lessonTriple builds a curator lifecycle triple stamped now.
func lessonTriple(subject, predicate, object string) message.Triple {
	return lessonTripleAt(subject, predicate, object, time.Now())
}

// lessonTripleAt builds a curator lifecycle triple with an explicit timestamp,
// so a multi-triple transition (retire/supersede) shares one wall-clock stamp.
func lessonTripleAt(subject, predicate, object string, now time.Time) message.Triple {
	return message.Triple{
		Subject:    subject,
		Predicate:  predicate,
		Object:     object,
		Source:     lessonCuratorSource,
		Timestamp:  now,
		Confidence: 1.0,
	}
}

// natsLessonReader adapts natsclient.Client to LessonReader, routing entity
// reads through graph.ingest.query.entity (the same query lane emit_lesson's
// read-back and OwnedFactWriter.ReadOwnedPredicates use).
type natsLessonReader struct {
	client *natsclient.Client
}

// NewNATSLessonReader builds a LessonReader backed by the shared graph query
// surface.
func NewNATSLessonReader(client *natsclient.Client) LessonReader {
	return &natsLessonReader{client: client}
}

func (r *natsLessonReader) ReadLessonEvidence(ctx context.Context, lessonEntityID string) ([]string, bool, error) {
	entity, found, err := r.queryEntity(ctx, lessonEntityID)
	if err != nil {
		return nil, false, err
	}
	// A bare referential-integrity stub is not a real lesson (no producer ever
	// born it): treat it as not-found so a stub can never be promoted.
	if !found || entity.IsStub() {
		return nil, false, nil
	}
	var evidence []string
	for _, tr := range entity.Triples {
		if tr.Predicate == agvocab.LessonEvidence {
			if s, ok := tr.Object.(string); ok {
				evidence = append(evidence, s)
			}
		}
	}
	return evidence, true, nil
}

func (r *natsLessonReader) EntityExists(ctx context.Context, entityID string) (bool, error) {
	entity, found, err := r.queryEntity(ctx, entityID)
	if err != nil {
		return false, err
	}
	// Citing an entity in a lesson's evidence auto-creates a referential-
	// integrity STUB for that target (graph.StubMessageType). A stub is a
	// placeholder, NOT proof the cited entity was ever really produced — so it
	// does NOT satisfy evidence existence. Only a real (non-stub) entity counts;
	// otherwise the promotion gate would be a no-op (every citation self-stubs).
	return found && !entity.IsStub(), nil
}

// queryEntity reads a single entity via graph.ingest.query.entity, collapsing
// the classified entity_not_found into (nil, false, nil). RequestClassified
// (NOT the retry variant) — a QUERY: retrying a hung query masks a responder
// problem as latency (natsclient mutation-vs-query rule). Handler errors arrive
// as the classified err (the `error: ` response-payload convention), never a
// zero-valued success.
func (r *natsLessonReader) queryEntity(ctx context.Context, entityID string) (*graph.EntityState, bool, error) {
	reqData, err := json.Marshal(entityQueryRequest{ID: entityID})
	if err != nil {
		return nil, false, fmt.Errorf("marshal entity query: %w", err)
	}
	respData, err := r.client.RequestClassified(ctx, ownedFactQuerySubject, reqData, ownedFactTimeout)
	if err != nil {
		var ce *errs.ClassifiedError
		if errors.As(err, &ce) && ce.Code == graph.ErrorCodeEntityNotFound {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("request %s: %w", ownedFactQuerySubject, err)
	}
	var entity graph.EntityState
	if err := graph.UnmarshalEntityState(respData, &entity); err != nil {
		return nil, false, fmt.Errorf("unmarshal entity query: %w", err)
	}
	return &entity, true, nil
}
