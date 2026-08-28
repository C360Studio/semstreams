package lessonmatch

import (
	"strings"
	"testing"
)

// active is a terse constructor for an eligible-status candidate.
func active(id, sev, createdAt string, appliesTo []string, form string) Lesson {
	return Lesson{
		EntityID:      id,
		Status:        "active",
		Severity:      sev,
		CreatedAt:     createdAt,
		AppliesTo:     appliesTo,
		InjectionForm: form,
	}
}

func idsOf(included []MatchedLesson) []string {
	out := make([]string, len(included))
	for i, m := range included {
		out[i] = m.EntityID
	}
	return out
}

// --- Eligibility: scope matching + segment boundaries ---

func TestMatch_Eligibility(t *testing.T) {
	tests := []struct {
		name      string
		lesson    Lesson
		scope     Scope
		wantMatch bool
	}{
		{
			name:      "tag match",
			lesson:    active("c360.ops.lesson.agent.record.a", "info", "", []string{"tag:ops"}, "x"),
			scope:     Scope{Tags: []string{"ops"}},
			wantMatch: true,
		},
		{
			name:      "tag miss",
			lesson:    active("c360.ops.lesson.agent.record.a", "info", "", []string{"tag:researcher"}, "x"),
			scope:     Scope{Tags: []string{"ops"}},
			wantMatch: false,
		},
		{
			name:      "id-prefix match on segment boundary",
			lesson:    active("c360.ops.lesson.agent.record.a", "info", "", []string{"id:c360.ops.robotics"}, "x"),
			scope:     Scope{EntityIDs: []string{"c360.ops.robotics.gcs.drone.001"}},
			wantMatch: true,
		},
		{
			// SPEC SCENARIO "Prefix matching respects segment boundaries":
			// id:c360.ops.robotics must NOT match c360.ops-agent.robotics...
			// (segment "ops" != "ops-agent") even though "c360.ops" is a raw
			// string prefix of "c360.ops-agent".
			name:      "id-prefix rejects mid-segment string prefix",
			lesson:    active("c360.ops.lesson.agent.record.a", "info", "", []string{"id:c360.ops.robotics"}, "x"),
			scope:     Scope{EntityIDs: []string{"c360.ops-agent.robotics.gcs.drone.001"}},
			wantMatch: false,
		},
		{
			name:      "id-prefix equal to full entity ID matches",
			lesson:    active("c360.ops.lesson.agent.record.a", "info", "", []string{"id:c360.ops.robotics.gcs.drone.001"}, "x"),
			scope:     Scope{EntityIDs: []string{"c360.ops.robotics.gcs.drone.001"}},
			wantMatch: true,
		},
		{
			name:      "id-prefix longer than entity ID cannot match",
			lesson:    active("c360.ops.lesson.agent.record.a", "info", "", []string{"id:c360.ops.robotics.gcs.drone.001.extra"}, "x"),
			scope:     Scope{EntityIDs: []string{"c360.ops.robotics.gcs.drone.001"}},
			wantMatch: false,
		},
		{
			name:      "any-key match (one of several keys matches)",
			lesson:    active("c360.ops.lesson.agent.record.a", "info", "", []string{"tag:nope", "id:c360.ops.robotics"}, "x"),
			scope:     Scope{EntityIDs: []string{"c360.ops.robotics.gcs.drone.001"}},
			wantMatch: true,
		},
		{
			name:      "empty scope matches nothing (no firehose)",
			lesson:    active("c360.ops.lesson.agent.record.a", "info", "", []string{"tag:ops"}, "x"),
			scope:     Scope{},
			wantMatch: false,
		},
		{
			name:      "lesson with no applies_to matches nothing",
			lesson:    active("c360.ops.lesson.agent.record.a", "info", "", nil, "x"),
			scope:     Scope{Tags: []string{"ops"}, EntityIDs: []string{"c360.ops.robotics.gcs.drone.001"}},
			wantMatch: false,
		},
		{
			name:      "untyped scope key ignored",
			lesson:    active("c360.ops.lesson.agent.record.a", "info", "", []string{"c360.ops.robotics"}, "x"),
			scope:     Scope{EntityIDs: []string{"c360.ops.robotics.gcs.drone.001"}},
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Match([]Lesson{tt.lesson}, tt.scope, Opts{})
			if (got.MatchedCount == 1) != tt.wantMatch {
				t.Fatalf("MatchedCount=%d IncludedCount=%d, wantMatch=%v", got.MatchedCount, got.IncludedCount, tt.wantMatch)
			}
			if tt.wantMatch && got.IncludedCount != 1 {
				t.Errorf("eligible lesson within bounds must be included; IncludedCount=%d", got.IncludedCount)
			}
		})
	}
}

// --- Exclusions: only active is eligible ---

func TestMatch_ExcludesNonActiveStatus(t *testing.T) {
	scope := Scope{Tags: []string{"ops"}}
	for _, status := range []string{"proposed", "retired", "superseded", "", "ACTIVE", "unknown"} {
		t.Run(status, func(t *testing.T) {
			l := active("c360.ops.lesson.agent.record.a", "info", "", []string{"tag:ops"}, "x")
			l.Status = status
			got := Match([]Lesson{l}, scope, Opts{})
			if got.MatchedCount != 0 || got.IncludedCount != 0 {
				t.Errorf("status %q must be excluded; got Matched=%d Included=%d", status, got.MatchedCount, got.IncludedCount)
			}
		})
	}
}

// --- Ordering: severity DESC → created-at DESC → entity-ID ASC ---

func TestMatch_OrderingBySeverity(t *testing.T) {
	scope := Scope{Tags: []string{"ops"}}
	// Same created-at, differing severity; supply out of order.
	ls := []Lesson{
		active("c360.ops.lesson.agent.record.info", "info", "2026-07-19T10:00:00Z", []string{"tag:ops"}, "i"),
		active("c360.ops.lesson.agent.record.crit", "critical", "2026-07-19T10:00:00Z", []string{"tag:ops"}, "c"),
		active("c360.ops.lesson.agent.record.warn", "warning", "2026-07-19T10:00:00Z", []string{"tag:ops"}, "w"),
	}
	got := Match(ls, scope, Opts{})
	want := []string{
		"c360.ops.lesson.agent.record.crit",
		"c360.ops.lesson.agent.record.warn",
		"c360.ops.lesson.agent.record.info",
	}
	assertOrder(t, idsOf(got.Included), want)
}

func TestMatch_OrderingByCreatedAtWithinSeverity(t *testing.T) {
	scope := Scope{Tags: []string{"ops"}}
	ls := []Lesson{
		active("c360.ops.lesson.agent.record.old", "warning", "2026-07-19T08:00:00Z", []string{"tag:ops"}, "o"),
		active("c360.ops.lesson.agent.record.new", "warning", "2026-07-19T12:00:00Z", []string{"tag:ops"}, "n"),
		active("c360.ops.lesson.agent.record.mid", "warning", "2026-07-19T10:00:00Z", []string{"tag:ops"}, "m"),
	}
	got := Match(ls, scope, Opts{})
	want := []string{
		"c360.ops.lesson.agent.record.new", // newest first
		"c360.ops.lesson.agent.record.mid",
		"c360.ops.lesson.agent.record.old",
	}
	assertOrder(t, idsOf(got.Included), want)
}

func TestMatch_OrderingTiebreakByEntityID(t *testing.T) {
	scope := Scope{Tags: []string{"ops"}}
	// Identical severity AND created-at ⇒ entity-ID ASC decides.
	ls := []Lesson{
		active("c360.ops.lesson.agent.record.ccc", "warning", "2026-07-19T10:00:00Z", []string{"tag:ops"}, "c"),
		active("c360.ops.lesson.agent.record.aaa", "warning", "2026-07-19T10:00:00Z", []string{"tag:ops"}, "a"),
		active("c360.ops.lesson.agent.record.bbb", "warning", "2026-07-19T10:00:00Z", []string{"tag:ops"}, "b"),
	}
	got := Match(ls, scope, Opts{})
	want := []string{
		"c360.ops.lesson.agent.record.aaa",
		"c360.ops.lesson.agent.record.bbb",
		"c360.ops.lesson.agent.record.ccc",
	}
	assertOrder(t, idsOf(got.Included), want)
}

func TestMatch_MissingOrUnparseableCreatedAtSortsLast(t *testing.T) {
	scope := Scope{Tags: []string{"ops"}}
	ls := []Lesson{
		active("c360.ops.lesson.agent.record.empty", "warning", "", []string{"tag:ops"}, "e"),
		active("c360.ops.lesson.agent.record.bad", "warning", "not-a-timestamp", []string{"tag:ops"}, "b"),
		active("c360.ops.lesson.agent.record.good", "warning", "2026-07-19T10:00:00Z", []string{"tag:ops"}, "g"),
	}
	got := Match(ls, scope, Opts{})
	// good (parseable) first; empty/bad both unparseable → entity-ID ASC among
	// them: "bad" < "empty".
	want := []string{
		"c360.ops.lesson.agent.record.good",
		"c360.ops.lesson.agent.record.bad",
		"c360.ops.lesson.agent.record.empty",
	}
	assertOrder(t, idsOf(got.Included), want)
}

// --- Bounds: K ceiling, byte budget, matched-vs-included counts ---

func TestMatch_KCeiling(t *testing.T) {
	scope := Scope{Tags: []string{"ops"}}
	ls := makeN(30, "info", "tag:ops")

	// Default K = 10.
	got := Match(ls, scope, Opts{})
	if got.MatchedCount != 30 {
		t.Errorf("MatchedCount = %d, want 30 (all eligible)", got.MatchedCount)
	}
	if got.IncludedCount != DefaultK {
		t.Errorf("IncludedCount = %d, want DefaultK=%d", got.IncludedCount, DefaultK)
	}

	// Requested K above MaxK clamps to MaxK.
	got = Match(ls, scope, Opts{K: 100, ByteBudget: 1 << 20})
	if got.IncludedCount != MaxK {
		t.Errorf("IncludedCount = %d, want MaxK=%d (clamp)", got.IncludedCount, MaxK)
	}

	// Explicit small K honoured.
	got = Match(ls, scope, Opts{K: 3, ByteBudget: 1 << 20})
	if got.IncludedCount != 3 {
		t.Errorf("IncludedCount = %d, want 3", got.IncludedCount)
	}
}

func TestMatch_ByteBudget(t *testing.T) {
	scope := Scope{Tags: []string{"ops"}}
	// Five 100-byte injection forms, all critical, distinct created-at so order
	// is deterministic (newest first).
	ls := []Lesson{
		active("c360.ops.lesson.agent.record.e", "critical", "2026-07-19T10:00:05Z", []string{"tag:ops"}, strings.Repeat("e", 100)),
		active("c360.ops.lesson.agent.record.d", "critical", "2026-07-19T10:00:04Z", []string{"tag:ops"}, strings.Repeat("d", 100)),
		active("c360.ops.lesson.agent.record.c", "critical", "2026-07-19T10:00:03Z", []string{"tag:ops"}, strings.Repeat("c", 100)),
		active("c360.ops.lesson.agent.record.b", "critical", "2026-07-19T10:00:02Z", []string{"tag:ops"}, strings.Repeat("b", 100)),
		active("c360.ops.lesson.agent.record.a", "critical", "2026-07-19T10:00:01Z", []string{"tag:ops"}, strings.Repeat("a", 100)),
	}
	// Budget of 250 bytes fits exactly two 100-byte forms (third would hit 300 > 250).
	got := Match(ls, scope, Opts{K: 25, ByteBudget: 250})
	if got.MatchedCount != 5 {
		t.Errorf("MatchedCount = %d, want 5", got.MatchedCount)
	}
	if got.IncludedCount != 2 {
		t.Errorf("IncludedCount = %d, want 2 (byte budget stops at 200/250)", got.IncludedCount)
	}
	// Included are the two newest (ranked prefix).
	assertOrder(t, idsOf(got.Included), []string{
		"c360.ops.lesson.agent.record.e",
		"c360.ops.lesson.agent.record.d",
	})
}

func TestMatch_MatchedVsIncludedObservable(t *testing.T) {
	scope := Scope{Tags: []string{"ops"}}
	ls := makeN(15, "info", "tag:ops")
	got := Match(ls, scope, Opts{K: 5, ByteBudget: 1 << 20})
	if got.MatchedCount != 15 {
		t.Errorf("MatchedCount = %d, want 15", got.MatchedCount)
	}
	if got.IncludedCount != 5 {
		t.Errorf("IncludedCount = %d, want 5", got.IncludedCount)
	}
	if got.MatchedCount <= got.IncludedCount {
		t.Errorf("truncation must be observable: MatchedCount %d must exceed IncludedCount %d", got.MatchedCount, got.IncludedCount)
	}
}

// --- Empty / no-match ---

func TestMatch_EmptyCandidates(t *testing.T) {
	got := Match(nil, Scope{Tags: []string{"ops"}}, Opts{})
	if got.MatchedCount != 0 || got.IncludedCount != 0 || len(got.Included) != 0 {
		t.Errorf("nil candidates must yield empty result, got %+v", got)
	}
}

func TestMatch_NoScopeMatchYieldsEmpty(t *testing.T) {
	ls := makeN(5, "critical", "tag:researcher")
	got := Match(ls, Scope{Tags: []string{"ops"}}, Opts{})
	if got.MatchedCount != 0 || got.IncludedCount != 0 {
		t.Errorf("no scope match must yield empty; got Matched=%d Included=%d", got.MatchedCount, got.IncludedCount)
	}
}

// --- Determinism: identical inputs → identical output ---

func TestMatch_DeterministicAcrossCalls(t *testing.T) {
	scope := Scope{Tags: []string{"ops"}, EntityIDs: []string{"c360.ops.robotics.gcs.drone.001"}}
	// Mixed severities, created-ats, and scope-key kinds; input order shuffled.
	ls := []Lesson{
		active("c360.ops.lesson.agent.record.z", "info", "2026-07-19T09:00:00Z", []string{"tag:ops"}, "z"),
		active("c360.ops.lesson.agent.record.m", "critical", "2026-07-19T08:00:00Z", []string{"id:c360.ops.robotics"}, "m"),
		active("c360.ops.lesson.agent.record.a", "critical", "2026-07-19T08:00:00Z", []string{"tag:ops"}, "a"),
		active("c360.ops.lesson.agent.record.q", "warning", "2026-07-19T11:00:00Z", []string{"tag:ops"}, "q"),
	}
	first := Match(ls, scope, Opts{})
	second := Match(ls, scope, Opts{})
	assertOrder(t, idsOf(second.Included), idsOf(first.Included))
	// Sanity: the expected total order.
	assertOrder(t, idsOf(first.Included), []string{
		"c360.ops.lesson.agent.record.a", // critical, 08:00, id "a" < "m"
		"c360.ops.lesson.agent.record.m", // critical, 08:00
		"c360.ops.lesson.agent.record.q", // warning, 11:00
		"c360.ops.lesson.agent.record.z", // info, 09:00
	})
}

// makeN builds n active lessons sharing severity and one scope key, with
// entity IDs record.000..record.NNN and an empty created-at (so ordering falls
// to entity-ID ASC, making the ranked prefix predictable).
func makeN(n int, sev, scopeKey string) []Lesson {
	out := make([]Lesson, 0, n)
	for i := range n {
		id := "c360.ops.lesson.agent.record." + pad3(i)
		out = append(out, active(id, sev, "", []string{scopeKey}, "f"+pad3(i)))
	}
	return out
}

func pad3(i int) string {
	s := "00" + itoa(i)
	return s[len(s)-3:]
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func assertOrder(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("length mismatch: got %d %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order mismatch at %d: got %v, want %v", i, got, want)
		}
	}
}
