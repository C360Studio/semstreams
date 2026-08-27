package agenticloop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/processor/agentic-loop/lessonmatch"
	agvocab "github.com/c360studio/semstreams/vocabulary/agentic"
)

// queryPrefixSubject is the NATS subject for prefix entity listing from
// graph-ingest (processor/graph-ingest/query.go). The LessonReader lists all
// agent.lesson.record entities through it.
const queryPrefixSubject = "graph.ingest.query.prefix"

// readLessonsTimeout bounds the per-dispatch lesson listing. Like the todo
// read, failure surfaces silently — the brief just carries no lesson block for
// this dispatch (availability over consistency): a one-dispatch omission is
// better than blocking the whole loop on a transient graph-gateway blip.
const readLessonsTimeout = 3 * time.Second

// maxLessonPages bounds pagination defensively. Lesson volume is expected to
// stay well under one page (graph.DefaultPrefixQueryLimit = 1000); this cap
// stops a pathological cursor loop from unbounded reads.
const maxLessonPages = 16

// LessonReader is the narrow surface MessageHandler uses to fetch lesson
// candidates for brief-assembly injection (ADR-080 push delivery). It returns
// EVERY lesson-record entity's matcher-relevant projection — including status —
// and the caller's matcher (lessonmatch.Match) performs the active-only
// exclusion and scope filtering, so the exclusion lives in ONE place the future
// fusion facet reuses. Production satisfies it via a NATS prefix query against
// graph.ingest.query.prefix; tests substitute an in-memory implementation.
// A nil reader disables injection entirely (back-compat).
type LessonReader interface {
	// ReadLessons lists the matcher-relevant projection of every lesson record
	// under recordPrefix (the {org}.{platform}.lesson.agent.record 5-part
	// prefix). The caller filters/orders/bounds the result.
	ReadLessons(ctx context.Context, recordPrefix string) ([]lessonmatch.Lesson, error)
}

// natsLessonReader adapts natsclient.Client to LessonReader by issuing a
// paginated graph.ingest.query.prefix request and projecting each returned
// entity's triples into a lessonmatch.Lesson.
type natsLessonReader struct {
	client *natsclient.Client
	logger *slog.Logger
}

// NewNATSLessonReader builds a LessonReader backed by the
// graph.ingest.query.prefix NATS surface.
func NewNATSLessonReader(client *natsclient.Client) LessonReader {
	return &natsLessonReader{client: client, logger: slog.Default()}
}

func (r *natsLessonReader) ReadLessons(ctx context.Context, recordPrefix string) ([]lessonmatch.Lesson, error) {
	var out []lessonmatch.Lesson
	cursor := ""
	pagesRead := 0
	for pagesRead < maxLessonPages {
		req := graph.PrefixQueryRequest{Prefix: recordPrefix, Cursor: cursor}
		reqData, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("marshal prefix query: %w", err)
		}

		// RequestClassified (NOT the retry variant): this is a QUERY — retrying a
		// hung query masks a responder problem as latency (natsclient
		// mutation-vs-query rule). An empty graph returns an empty entities list,
		// not an error, so there is no not-found case to fail-open on.
		respData, err := r.client.RequestClassified(ctx, queryPrefixSubject, reqData, readLessonsTimeout)
		if err != nil {
			return nil, fmt.Errorf("request %s: %w", queryPrefixSubject, err)
		}

		var resp graph.PrefixQueryResponse
		if err := json.Unmarshal(respData, &resp); err != nil {
			return nil, fmt.Errorf("unmarshal prefix response: %w", err)
		}
		pagesRead++
		for i := range resp.Entities {
			out = append(out, projectLessonEntity(&resp.Entities[i]))
		}
		cursor = resp.NextCursor
		if cursor == "" {
			break
		}
	}
	// A still-non-empty cursor means the loop exited on the page cap, NOT on
	// exhaustion: the candidate set is PARTIAL and some active in-scope lessons
	// may silently never inject. Make that coverage loss visible (v1 volume is
	// << one page, so this never fires in practice).
	warnIfLessonPageCapHit(r.logger, recordPrefix, cursor, pagesRead, len(out))
	return out, nil
}

// warnIfLessonPageCapHit emits an operator Warn when lesson pagination stopped
// because it hit maxLessonPages with a still-non-empty cursor (partial coverage).
// A no-op when the cursor is empty (pagination completed — full coverage).
func warnIfLessonPageCapHit(logger *slog.Logger, recordPrefix, cursor string, pagesRead, lessonsRead int) {
	if cursor == "" {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	logger.Warn("lesson page cap hit; brief coverage truncated",
		slog.String("record_prefix", recordPrefix),
		slog.Int("pages", pagesRead),
		slog.Int("cap", maxLessonPages),
		slog.Int("lessons_read", lessonsRead))
}

// projectLessonEntity maps a lesson-record EntityState onto the matcher-relevant
// projection. applies-to is multi-valued (one triple per key); the scalar
// predicates take the first matching triple. Absent predicates leave zero
// values — the matcher tolerates them (empty status excludes; empty created-at
// sorts last).
func projectLessonEntity(es *graph.EntityState) lessonmatch.Lesson {
	l := lessonmatch.Lesson{EntityID: es.ID}
	for i := range es.Triples {
		tr := &es.Triples[i]
		s, ok := tripleObjectAsString(tr.Object)
		if !ok {
			continue
		}
		switch tr.Predicate {
		case agvocab.LessonStatus:
			l.Status = s
		case agvocab.LessonSeverity:
			l.Severity = s
		case agvocab.LessonCreatedAt:
			l.CreatedAt = s
		case agvocab.LessonInjectionForm:
			l.InjectionForm = s
		case agvocab.LessonAppliesTo:
			l.AppliesTo = append(l.AppliesTo, s)
		}
	}
	return l
}

// deriveLoopScope builds the matcher scope for a task. v1 derivation is
// intentionally minimal and documented: the TaskMessage carries no explicit
// entity-scope field, so scope is tag-by-role only — the loop's role becomes a
// single `tag:<role>` match key. The spec scenarios exercise exactly role-tag
// and id-prefix matching; id-prefix scope arrives once a task declares in-scope
// entity IDs (a later increment), at which point they populate Scope.EntityIDs
// here without any matcher change. An empty role yields an empty scope, which
// the matcher treats as "match nothing" (never a firehose).
func deriveLoopScope(task TaskMessage) lessonmatch.Scope {
	var scope lessonmatch.Scope
	if role := strings.TrimSpace(task.Role); role != "" {
		scope.Tags = []string{role}
	}
	return scope
}

// renderLessonBlock formats a matcher Result into a system-prompt block. Empty
// selection returns "" (the caller appends nothing). The header states the
// matched-vs-included counts so truncation is observable in the prompt itself
// (spec: "the injection block states matched-versus-included counts"); each
// line carries the injection form and its lesson entity ID (for governed
// dereference via query_entity — no dedicated lesson search tool exists).
func renderLessonBlock(res lessonmatch.Result) string {
	if res.IncludedCount == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b,
		"[Lessons — durable guidance distilled from prior work; matched %d, showing %d]\n",
		res.MatchedCount, res.IncludedCount)
	for _, m := range res.Included {
		fmt.Fprintf(&b, "- %s (%s)\n", m.InjectionForm, m.EntityID)
	}
	return strings.TrimRight(b.String(), "\n")
}
