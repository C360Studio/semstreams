package operatingmodel

import (
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/message"
)

// Lesson is a single compaction-extracted insight tied to a user's profile.
//
// Pre-Phase-1, agentic-memory's compaction handler called LLMExtractor.ExtractFacts
// and emitted unstamped triples that no reader could find. Lessons are the
// user-scoped, queryable shape of that extraction: each Lesson carries a
// short text summary, the originating session/loop ID, and a learned-at
// timestamp so the assembler can rank by recency.
//
// LessonID is opaque (UUID-like); callers should not parse it. SessionID
// records which loop the insight came from for trajectory back-tracing.
type Lesson struct {
	// LessonID is the stable identifier; used as the instance part of the
	// entity ID. UUID or other dot-free string.
	LessonID string `json:"lesson_id"`

	// Summary is the lesson text — typically one or two sentences extracted
	// from the compaction summary.
	Summary string `json:"summary"`

	// SessionID is the loop ID the lesson was learned during. Empty when the
	// lesson is system-injected rather than session-derived.
	SessionID string `json:"session_id,omitempty"`

	// LearnedAt is the wall-clock time the compaction event ran; used for
	// recency ranking when multiple lessons compete for the same render
	// budget.
	LearnedAt time.Time `json:"learned_at"`
}

// Validate checks required fields. SessionID is optional; LessonID + Summary
// + a non-zero LearnedAt are required.
func (l *Lesson) Validate() error {
	if strings.TrimSpace(l.LessonID) == "" {
		return fmt.Errorf("lesson_id required")
	}
	if strings.Contains(l.LessonID, ".") {
		return fmt.Errorf("lesson_id %q must not contain dots", l.LessonID)
	}
	if strings.TrimSpace(l.Summary) == "" {
		return fmt.Errorf("summary required")
	}
	if l.LearnedAt.IsZero() {
		return fmt.Errorf("learned_at required")
	}
	return nil
}

// LessonTriples emits the triples that persist a Lesson as a queryable
// entity tied to the user's profile. The ProfileRef.Version field is unused
// here — lessons are not versioned the way operating-model entries are; they
// accumulate over time and rank by LearnedAt.
//
// Triples emitted:
//
//	(profile, has_lesson, lesson)              — relationship from profile
//	(lesson, summary, l.Summary)
//	(lesson, session_id, l.SessionID)          — only if non-empty
//	(lesson, learned_at, l.LearnedAt RFC3339)
func LessonTriples(ref ProfileRef, l Lesson) []message.Triple {
	profileID := ProfileEntityID(ref.Org, ref.Platform, ref.UserID)
	lessonID := LessonEntityID(ref.Org, ref.Platform, l.LessonID)
	now := l.LearnedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}

	triples := []message.Triple{
		{
			Subject:    profileID,
			Predicate:  PredicateProfileHasLesson,
			Object:     lessonID,
			Source:     LessonTripleSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    lessonID,
			Predicate:  PredicateLessonSummary,
			Object:     l.Summary,
			Source:     LessonTripleSource,
			Timestamp:  now,
			Confidence: 1.0,
		},
		{
			Subject:    lessonID,
			Predicate:  PredicateLessonLearnedAt,
			Object:     now.UTC().Format(time.RFC3339),
			Source:     LessonTripleSource,
			Timestamp:  now,
			Confidence: 1.0,
			Datatype:   "xsd:dateTime",
		},
	}
	if l.SessionID != "" {
		triples = append(triples, message.Triple{
			Subject:    lessonID,
			Predicate:  PredicateLessonSessionID,
			Object:     l.SessionID,
			Source:     LessonTripleSource,
			Timestamp:  now,
			Confidence: 1.0,
		})
	}
	return triples
}
