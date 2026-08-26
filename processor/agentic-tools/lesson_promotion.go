package agentictools

import (
	"context"
	"errors"
	"fmt"
	"github.com/c360studio/semstreams/agentic"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/pkg/projection"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// lessonCuratorSource tags the lifecycle triples this writer publishes so
// operators can distinguish a curator promotion/retirement from the ops
// agent's original emit_lesson births at a glance (the lesson entity's own source is ops-emit-lesson).
const lessonCuratorSource = "ops-lesson-curator"

// Lesson lifecycle status values the curator writes. Born status is
// lessonBornStatus ("proposed", emit_lesson.go); these are the reachable
// transitions. Only "active" lessons are injectable at brief assembly.
const (
	lessonStatusActive     = "active"
	lessonStatusRetired    = "retired"
	lessonStatusSuperseded = "superseded"
)

// LessonCurator is the reference VALIDATED lesson lifecycle writer (ADR-080
// gated lifecycle, task 4.1). It promotes, retires, and supersedes lesson
// records through the contract-bound reconcile lane, composing the
// framework's least-privilege PredicateReconciler and AuthoritativeReader
// capabilities. Every transition reconciles the complete lifecycle group, so
// mutually exclusive sibling predicates cannot survive a transition.
//
// This is the OPERATOR/PRODUCT curation path, NOT an agent tool: ADR-080 makes
// operator/product review the default promotion gate, so the framework ships no
// `promote_lesson` tool. A product may wrap Promote in a curation UI, a rule
// `reconcile_predicates` action (for mechanical/retirement transitions), or an
// explicit auto-promotion policy — but the evidence-existence resolution below
// is what makes a promotion HONEST, so the validated path routes through here.
type LessonCurator struct {
	writer projection.PredicateReconciler
	reader projection.AuthoritativeReader
	logger *slog.Logger
}

// LessonProjectionContract returns an independent snapshot of the canonical
// projection contract required by LessonCurator lifecycle mutations — the
// contract registered with agentic.agent_lesson.v1 in the payload registry.
func LessonProjectionContract() projection.Contract { return agentic.LessonContract() }

// NewLessonCurator builds a curator over explicitly supplied write and read
// capabilities for tests and specialized composition. A nil logger uses
// slog.Default().
func NewLessonCurator(writer projection.PredicateReconciler, reader projection.AuthoritativeReader, logger *slog.Logger) *LessonCurator {
	if logger == nil {
		logger = slog.Default()
	}
	return &LessonCurator{writer: writer, reader: reader, logger: logger}
}

// Promote flips a lesson proposed→active, but ONLY after resolving that every
// cited evidence entity exists in the graph. If ANY cited evidence entity is
// missing the promotion is REFUSED with an instructive error and the lesson is
// left untouched (status stays proposed) — spec scenario "Promotion resolves
// evidence existence". This is the validated promotion path; the bare rule
// `reconcile_predicates` lane performs no such check and is for mechanical transitions.
func (c *LessonCurator) Promote(ctx context.Context, lessonEntityID string) error {
	if !message.IsValidEntityID(lessonEntityID) {
		return fmt.Errorf("promote lesson: %q is not a well-formed 6-part entity ID", lessonEntityID)
	}

	exactLesson, err := c.reader.ReadAuthoritative(ctx, lessonEntityID)
	if err != nil {
		if isProjectionNotFound(err) {
			return fmt.Errorf("promote lesson %s: refused — lesson entity not found in the graph", lessonEntityID)
		}
		return fmt.Errorf("promote lesson %s: read cited evidence: %w", lessonEntityID, err)
	}
	lesson := exactLesson.Entity
	evidence := lessonEvidence(lesson)
	if len(evidence) == 0 {
		// Defensive: emit_lesson requires >=1 evidence, so this only fires if a
		// lesson lost its evidence out-of-band. A promotion still must not fabricate.
		return fmt.Errorf("promote lesson %s: refused — lesson cites no evidence; it remains proposed", lessonEntityID)
	}

	var missing []string
	for _, ev := range evidence {
		_, existsErr := c.reader.ReadAuthoritative(ctx, ev)
		if existsErr != nil {
			if isProjectionNotFound(existsErr) {
				missing = append(missing, ev)
				continue
			}
			return fmt.Errorf("promote lesson %s: resolve evidence %s: %w", lessonEntityID, ev, existsErr)
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

// replace reconciles the complete lifecycle group in one contract-bound
// mutation. Predicates omitted from add are removed, which prevents retired-at
// and superseded-by from surviving mutually exclusive transitions.
func (c *LessonCurator) replace(ctx context.Context, lessonEntityID string, add ...message.Triple) error {
	timestamp := time.Now()
	if len(add) != 0 {
		timestamp = add[0].Timestamp
	}
	_, err := c.writer.Reconcile(ctx, projection.ReconcileMutation{
		Contract: agentic.LessonRecordContractName,
		Group:    agentic.LessonLifecycleGroupName,
		EntityID: lessonEntityID,
		Desired:  add,
		Metadata: projection.MutationMetadata{
			RequestID: "lesson-curator:" + lessonEntityID + ":" + lifecycleOperation(add),
			Source:    lessonCuratorSource,
			Timestamp: timestamp,
		},
	})
	return err
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

func lessonEvidence(entity *graph.EntityState) []string {
	var evidence []string
	for _, tr := range entity.Triples {
		if tr.Predicate == agvocab.LessonEvidence {
			if s, ok := tr.Object.(string); ok {
				evidence = append(evidence, s)
			}
		}
	}
	return evidence
}

func isProjectionNotFound(err error) bool {
	var mutationErr *projection.MutationError
	return errors.As(err, &mutationErr) && mutationErr.Kind == projection.MutationNotFound
}

func lifecycleOperation(triples []message.Triple) string {
	for _, triple := range triples {
		if triple.Predicate != agvocab.LessonStatus {
			continue
		}
		if status, ok := triple.Object.(string); ok {
			return status
		}
	}
	return "transition"
}
