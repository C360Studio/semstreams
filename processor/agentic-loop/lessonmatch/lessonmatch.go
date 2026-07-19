// Package lessonmatch is the deterministic, PURE lesson selector that powers
// bounded push-based lesson delivery (ADR-080, agent-memory-lesson-substrate).
//
// It answers one question: given a set of lesson candidates already fetched
// from the graph and a loop's scope, which active lessons — in which order,
// within which bounds — should be injected into that loop's brief?
//
// Two properties are load-bearing:
//
//   - PURE. Match issues no graph queries, reads no clock, touches no I/O. The
//     caller fetches candidates (agentic-loop's LessonReader today; the future
//     `want:[lessons]` fusion facet tomorrow) and hands them in; this package
//     only ranks and bounds. That separation is what lets both consumers reuse
//     one selector without dragging in a transport.
//   - DETERMINISTIC. Ordering is severity → immutable created-at → entity-ID,
//     with no similarity, embedding, or relevance scoring (the fusion
//     determinism contract). The same candidates + scope always yield the same
//     Result, including across an ADR-073 from-zero reingest — because the
//     ordering key is the lesson's IMMUTABLE birth timestamp, never a KV
//     revision or a re-stamped update time.
package lessonmatch

import (
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	// DefaultK is the count ceiling applied when Opts.K is unset (<= 0).
	DefaultK = 10

	// MaxK is the hard upper bound on the count ceiling. A requested K above
	// this is clamped down — briefs stay small regardless of caller intent.
	MaxK = 25

	// DefaultByteBudget is the total injection-form byte budget applied when
	// Opts.ByteBudget is unset (<= 0). ~4 KB (≈1000 tokens) — large enough that
	// a default-K selection of byte-bounded (≤320B) injection forms passes, so
	// K is the biting bound in the common case and the byte budget is the
	// secondary safety net. Callers configure the real budget.
	DefaultByteBudget = 4096

	// statusActive is the sole injectable lifecycle status. proposed / retired /
	// superseded lessons are excluded (ADR-080 gated lifecycle).
	statusActive = "active"

	// scope-key type prefixes (mirrors the emit_lesson writer grammar).
	tagKeyPrefix = "tag:"
	idKeyPrefix  = "id:"
)

// Lesson is one candidate's matcher-relevant projection. The caller populates
// it from an agent.lesson.record entity's triples. Fields the matcher does not
// read (summary, detail, polarity, evidence, ...) are deliberately absent.
type Lesson struct {
	// EntityID is the lesson record's 6-part entity ID. Used verbatim in the
	// rendered block and as the final deterministic ordering tiebreak.
	EntityID string

	// Status is agent.lesson.status. Only "active" lessons are eligible; every
	// other value (proposed/retired/superseded/empty) is excluded here so the
	// exclusion lives in ONE place both consumers share.
	Status string

	// Severity is agent.lesson.severity (critical|warning|info). An unrecognised
	// or empty value ranks below "info" (lowest urgency), never panics.
	Severity string

	// CreatedAt is agent.lesson.created-at — the IMMUTABLE RFC3339 birth
	// timestamp. The replay-stable ordering key. Empty or unparseable sorts
	// last, deterministically (see less).
	CreatedAt string

	// AppliesTo is every agent.lesson.applies-to scope key on the lesson
	// (multi-valued). Grammar: "tag:<token>" | "id:<entity-id-prefix>".
	AppliesTo []string

	// InjectionForm is agent.lesson.injection-form — the bounded string rendered
	// verbatim into the brief. Its byte length is charged against ByteBudget.
	InjectionForm string
}

// Scope is the loop's declared applicability scope. A lesson is eligible when
// ANY of its AppliesTo keys matches this scope. An empty Scope (no tags AND no
// entity IDs) matches nothing — never a firehose.
type Scope struct {
	// EntityIDs are the loop's in-scope entity IDs, matched against `id:` keys
	// on entity-ID SEGMENT boundaries (see idPrefixMatchesEntity).
	EntityIDs []string

	// Tags are the loop's scope tags (e.g. the loop role), matched against
	// `tag:` keys by exact string equality.
	Tags []string
}

// Opts bounds the selection.
type Opts struct {
	// K is the count ceiling. <= 0 uses DefaultK; values above MaxK clamp to MaxK.
	K int

	// ByteBudget is the total injection-form byte budget. <= 0 uses
	// DefaultByteBudget. Selection stops at the first ranked lesson that would
	// push the running injection-form byte sum over the budget.
	ByteBudget int
}

// MatchedLesson is one included lesson, in ranked order.
type MatchedLesson struct {
	EntityID      string
	InjectionForm string
}

// Result is the bounded, ordered selection plus the observability counts that
// make truncation visible: MatchedCount is every eligible lesson (post scope +
// status filter, pre-bounds); IncludedCount is len(Included) after the K and
// byte bounds. MatchedCount > IncludedCount means the brief was truncated.
type Result struct {
	Included      []MatchedLesson
	MatchedCount  int
	IncludedCount int
}

// Match selects, orders, and bounds the active lessons whose scope keys match
// the given scope. Pure and deterministic; see the package doc.
func Match(candidates []Lesson, scope Scope, opts Opts) Result {
	// 1. Eligibility: active status AND at least one scope-key match.
	eligible := make([]Lesson, 0, len(candidates))
	for _, l := range candidates {
		if l.Status != statusActive {
			continue
		}
		if lessonMatchesScope(l, scope) {
			eligible = append(eligible, l)
		}
	}

	// 2. Deterministic ordering: severity DESC → created-at DESC → entity-ID ASC.
	sort.SliceStable(eligible, func(i, j int) bool { return less(eligible[i], eligible[j]) })

	// 3. Bounds: fill in ranked order until either the count ceiling or the
	//    total-byte budget is hit.
	k := clampK(opts.K)
	byteBudget := opts.ByteBudget
	if byteBudget <= 0 {
		byteBudget = DefaultByteBudget
	}

	included := make([]MatchedLesson, 0, k)
	runningBytes := 0
	for _, l := range eligible {
		if len(included) >= k {
			break
		}
		formBytes := len(l.InjectionForm)
		if runningBytes+formBytes > byteBudget {
			// Ranked-prefix truncation: stop at the first lesson that would
			// overflow rather than skipping it and continuing (which would break
			// the "bounded prefix of the ranked order" contract the facet reuses).
			break
		}
		runningBytes += formBytes
		included = append(included, MatchedLesson{EntityID: l.EntityID, InjectionForm: l.InjectionForm})
	}

	return Result{
		Included:      included,
		MatchedCount:  len(eligible),
		IncludedCount: len(included),
	}
}

// clampK maps a requested K to the effective ceiling: <= 0 → DefaultK, above
// MaxK → MaxK, otherwise the request.
func clampK(k int) int {
	switch {
	case k <= 0:
		return DefaultK
	case k > MaxK:
		return MaxK
	default:
		return k
	}
}

// lessonMatchesScope reports whether ANY of the lesson's scope keys matches the
// scope. Unknown-typed keys are ignored (the emit_lesson writer gates the
// grammar; this is defense in depth, not the enforcement point).
func lessonMatchesScope(l Lesson, scope Scope) bool {
	for _, key := range l.AppliesTo {
		switch {
		case strings.HasPrefix(key, tagKeyPrefix):
			token := key[len(tagKeyPrefix):]
			if token != "" && slices.Contains(scope.Tags, token) {
				return true
			}
		case strings.HasPrefix(key, idKeyPrefix):
			prefix := key[len(idKeyPrefix):]
			if prefix == "" {
				continue
			}
			for _, eid := range scope.EntityIDs {
				if idPrefixMatchesEntity(prefix, eid) {
					return true
				}
			}
		}
	}
	return false
}

// idPrefixMatchesEntity reports whether prefix is a SEGMENT-BOUNDARY prefix of
// entityID: the dot-split prefix segments must equal the leading entity-ID
// segments exactly. This is NOT strings.HasPrefix — the spec's segment-boundary
// scenario requires that `c360.ops.robotics` NOT match
// `c360.ops-agent.robotics.gcs.drone.001` (segment "ops" != "ops-agent") even
// though the raw string "c360.ops" is a character prefix of "c360.ops-agent".
func idPrefixMatchesEntity(prefix, entityID string) bool {
	ps := strings.Split(prefix, ".")
	es := strings.Split(entityID, ".")
	if len(ps) > len(es) {
		return false
	}
	for i, seg := range ps {
		if es[i] != seg {
			return false
		}
	}
	return true
}

// less is the deterministic ordering predicate: severity DESC, then created-at
// DESC (present-and-parseable before missing/unparseable, newer before older),
// then entity-ID ASC. Entity IDs are unique, so this is a total order.
func less(a, b Lesson) bool {
	sa, sb := severityRank(a.Severity), severityRank(b.Severity)
	if sa != sb {
		return sa > sb // higher severity first
	}

	ta, oka := parseCreatedAt(a.CreatedAt)
	tb, okb := parseCreatedAt(b.CreatedAt)
	if oka != okb {
		// A present timestamp always sorts before a missing/unparseable one.
		return oka
	}
	if oka && okb && !ta.Equal(tb) {
		return ta.After(tb) // newer first
	}

	return a.EntityID < b.EntityID // stable, unique tiebreak
}

// severityRank maps a severity string to a sortable rank. Unknown/empty ranks
// below every known level so a malformed lesson never outranks a real one.
func severityRank(s string) int {
	switch s {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

// parseCreatedAt parses the immutable RFC3339 birth timestamp. Empty or
// unparseable returns ok=false so the caller sorts it last, deterministically.
func parseCreatedAt(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
