package predicateaudit

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/vocabulary"
)

func TestAuditTestFixturesRemainsSeparateFromProductionAudit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "producer.go", `package fixture
var _ = Triple{Predicate: "production.state.bad_name"}
`)
	writeAuditFixture(t, root, "producer_test.go", `package fixture
var _ = Triple{Predicate: "test.state.bad_name"}
`)
	writeAuditFixture(t, root, "nested/testdata/seed.json", `{"predicate":"seed.state.bad_name"}`)
	manifest := writeFixtureManifest(t, root, `{"version":1,"entries":[]}`)

	productionCandidates, productionFindings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(productionCandidates) != 1 || len(productionFindings) != 1 || productionFindings[0].Predicate != "production.state.bad_name" {
		t.Fatalf("production audit = candidates %#v findings %#v, want production only", productionCandidates, productionFindings)
	}

	fixture, err := AuditTestFixtures(manifest, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Candidates) != 2 {
		t.Fatalf("fixture candidates = %#v, want Go test and nested testdata only", fixture.Candidates)
	}
	for _, candidate := range fixture.Candidates {
		if candidate.Predicate == "production.state.bad_name" {
			t.Fatalf("fixture audit included production candidate: %#v", candidate)
		}
	}
}

func TestAuditTestFixturesAcceptsOneExactSourceClassification(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "negative_test.go", `package fixture
var _ = Triple{Predicate: "legacy.state.bad_name"} // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
`)

	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("findings = %#v, want exact negative accepted", result.Findings)
	}
	if result.Classifications != 1 {
		t.Fatalf("classifications = %d, want 1", result.Classifications)
	}
}

func TestAuditTestFixturesAcceptsExactUnrelatedGoOccurrences(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := "package fixture\n" +
		unrelatedPredicateFieldLine(`"robotics.state.armed"`, "robotics.state.armed", "go-field:Predicate", "reviewed:non-semantic-selector") + "\n" +
		unrelatedPredicateFieldLine(`"legacy.state.bad_name"`, "legacy.state.bad_name", "go-field:Predicate", "reviewed:nats-request-subject") + "\n" +
		"func fixture(value string) {\n" +
		unrelatedPredicateFieldLine("value", "", "go-field:Predicate", "reviewed:opaque-filter-name") + "\n}\n"
	writeAuditFixture(t, root, "unrelated_test.go", source)

	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("findings = %#v, want exact valid, invalid, and unresolved occurrences disposed", result.Findings)
	}
	if result.Dispositions != 3 || result.Classifications != 0 {
		t.Fatalf("dispositions = %d classifications = %d, want 3 and 0", result.Dispositions, result.Classifications)
	}
}

func TestAuditTestFixturesUnrelatedAnnotationsFailClosedAtDecode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "missing column",
			source: `package fixture
var _ = Triple{Predicate: "robotics.state.armed"} // predicate-audit:unrelated {"surface":"go-field:Predicate","value":"robotics.state.armed","basis":"reviewed:other"}
`,
		},
		{
			name: "missing value including explicit empty requirement",
			source: `package fixture
var _ = Triple{Predicate: value} // predicate-audit:unrelated {"column":27,"surface":"go-field:Predicate","basis":"reviewed:other"}
`,
		},
		{
			name: "unknown field",
			source: `package fixture
var _ = Triple{Predicate: "robotics.state.armed"} // predicate-audit:unrelated {"column":27,"surface":"go-field:Predicate","value":"robotics.state.armed","basis":"reviewed:other","reason":"broad"}
`,
		},
		{
			name: "file and line are derived",
			source: `package fixture
var _ = Triple{Predicate: "robotics.state.armed"} // predicate-audit:unrelated {"file":"elsewhere_test.go","line":1,"column":27,"surface":"go-field:Predicate","value":"robotics.state.armed","basis":"reviewed:other"}
`,
		},
		{
			name: "zero column",
			source: `package fixture
var _ = Triple{Predicate: "robotics.state.armed"} // predicate-audit:unrelated {"column":0,"surface":"go-field:Predicate","value":"robotics.state.armed","basis":"reviewed:other"}
`,
		},
		{
			name: "blank basis",
			source: `package fixture
var _ = Triple{Predicate: "robotics.state.armed"} // predicate-audit:unrelated {"column":27,"surface":"go-field:Predicate","value":"robotics.state.armed","basis":"  "}
`,
		},
		{
			name:   "basis too long",
			source: "package fixture\nvar _ = Triple{Predicate: \"robotics.state.armed\"} // predicate-audit:unrelated {\"column\":27,\"surface\":\"go-field:Predicate\",\"value\":\"robotics.state.armed\",\"basis\":" + strconv.Quote(strings.Repeat("x", maxFixtureDispositionBasis+1)) + "}\n",
		},
		{
			name: "block comment",
			source: `package fixture
var _ = Triple{Predicate: "robotics.state.armed"} /* predicate-audit:unrelated {"column":27,"surface":"go-field:Predicate","value":"robotics.state.armed","basis":"reviewed:other"} */
`,
		},
		{
			name: "authority surface",
			source: `package fixture
var _ = ParsePredicate("robotics.state.armed") // predicate-audit:unrelated {"column":9,"surface":"go-call:ParsePredicate","value":"robotics.state.armed","basis":"reviewed:other"}
`,
		},
		{
			name: "register surface",
			source: `package fixture
var _ = Triple{Predicate: "robotics.state.armed"} // predicate-audit:unrelated {"column":27,"surface":"go-register","value":"robotics.state.armed","basis":"reviewed:other"}
`,
		},
		{
			name: "empty name-derived surface",
			source: `package fixture
var _ = Triple{Predicate: "robotics.state.armed"} // predicate-audit:unrelated {"column":27,"surface":"go-field:","value":"robotics.state.armed","basis":"reviewed:other"}
`,
		},
		{
			name: "conflicts with invalid marker",
			source: `package fixture
var _ = Triple{Predicate: "legacy.state.bad_name"} // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"} predicate-audit:unrelated {"column":27,"surface":"go-field:Predicate","value":"legacy.state.bad_name","basis":"reviewed:other"}
`,
		},
		{
			name: "duplicate marker",
			source: `package fixture
var _ = Triple{Predicate: "robotics.state.armed"} // predicate-audit:unrelated {"column":27,"surface":"go-field:Predicate","value":"robotics.state.armed","basis":"reviewed:one"} predicate-audit:unrelated {"column":27,"surface":"go-field:Predicate","value":"robotics.state.armed","basis":"reviewed:two"}
`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeAuditFixture(t, root, "unrelated_test.go", test.source)
			if _, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root); err == nil {
				t.Fatal("AuditTestFixtures() error = nil, want malformed unrelated annotation rejected")
			}
		})
	}
}

func TestAuditTestFixturesRejectsDuplicateUnrelatedJSONMembers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		payload string
	}{
		{
			name:    "column",
			payload: `{"column":99,"column":27,"surface":"go-field:Predicate","value":"robotics.state.armed","basis":"reviewed:exact"}`,
		},
		{
			name:    "surface",
			payload: `{"column":27,"surface":"go-assignment:Predicate","surface":"go-field:Predicate","value":"robotics.state.armed","basis":"reviewed:exact"}`,
		},
		{
			name:    "value",
			payload: `{"column":27,"surface":"go-field:Predicate","value":"other.state.value","value":"robotics.state.armed","basis":"reviewed:exact"}`,
		},
		{
			name:    "basis",
			payload: `{"column":27,"surface":"go-field:Predicate","value":"robotics.state.armed","basis":"","basis":"reviewed:exact"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeAuditFixture(
				t,
				root,
				"unrelated_test.go",
				"package fixture\nvar _ = Triple{Predicate: \"robotics.state.armed\"} // "+FixtureUnrelatedMarker+" "+test.payload+"\n",
			)
			if _, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root); err == nil {
				t.Fatal("AuditTestFixtures() error = nil, want duplicate unrelated JSON member rejected")
			}
		})
	}
}

func TestAuditTestFixturesUnrelatedDispositionLocatorsFailClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		line     string
		wantCode string
		wantOne  bool
	}{
		{
			name:     "moved column",
			line:     `var _ = Triple{Predicate: "robotics.state.armed"} // predicate-audit:unrelated {"column":99,"surface":"go-field:Predicate","value":"robotics.state.armed","basis":"reviewed:other"}`,
			wantCode: "stale-disposition",
		},
		{
			name:     "wrong surface",
			line:     `var _ = Triple{Predicate: "robotics.state.armed"} // predicate-audit:unrelated {"column":27,"surface":"go-assignment:Predicate","value":"robotics.state.armed","basis":"reviewed:other"}`,
			wantCode: "wrong-disposition-surface",
		},
		{
			name:     "wrong value has no grammar noise",
			line:     `var _ = Triple{Predicate: "legacy.state.bad_name"} // predicate-audit:unrelated {"column":27,"surface":"go-field:Predicate","value":"other.state.bad_name","basis":"reviewed:other"}`,
			wantCode: "wrong-disposition-value",
			wantOne:  true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeAuditFixture(t, root, "unrelated_test.go", "package fixture\n"+test.line+"\n")
			result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
			if err != nil {
				t.Fatal(err)
			}
			assertFixtureFindingCode(t, result.Findings, test.wantCode)
			if test.wantOne && len(result.Findings) != 1 {
				t.Fatalf("findings = %#v, want only locator-specific finding", result.Findings)
			}
		})
	}
}

func TestUnrelatedDispositionUsesColumnAndSurfaceIdentity(t *testing.T) {
	t.Parallel()
	candidates := deduplicateFixtureCandidates([]FixtureCandidate{
		{File: "fixture_test.go", Line: 2, Column: 27, Location: "line:2:column:27", Predicate: "robotics.state.armed", Surface: "go-field:Predicate", Unresolved: true},
		{File: "fixture_test.go", Line: 2, Column: 60, Location: "line:2:column:60", Predicate: "robotics.state.armed", Surface: "go-field:Predicate", Unresolved: true},
		{File: "fixture_test.go", Line: 2, Column: 27, Location: "line:2:column:27", Predicate: "robotics.state.armed", Surface: "go-assignment:Predicate", Unresolved: true},
	})
	if len(candidates) != 3 {
		t.Fatalf("deduplicated candidates = %#v, want both physical columns and both surfaces", candidates)
	}
	value := "robotics.state.armed"
	result := classifyFixtureCandidates(candidates, nil, []sourceFixtureDisposition{{
		File: "fixture_test.go", Line: 2, Column: 27, Surface: "go-field:Predicate", Value: &value, Basis: "reviewed:other",
	}}, nil)
	if len(result.Findings) != 2 {
		t.Fatalf("findings = %#v, want only the other column and other surface unclassified", result.Findings)
	}
	for _, finding := range result.Findings {
		if finding.Code != "unresolved-go-surface" {
			t.Fatalf("finding = %#v, want exact disposition to leave only unresolved siblings", finding)
		}
	}
}

func TestUnrelatedDispositionFailsClosedWhenBroad(t *testing.T) {
	t.Parallel()
	value := "robotics.state.armed"
	candidate := FixtureCandidate{
		File: "fixture_test.go", Line: 2, Column: 27, Location: "line:2:column:27",
		Predicate: "robotics.state.armed", Surface: "go-field:Predicate",
	}
	result := classifyFixtureCandidates(
		[]FixtureCandidate{candidate, candidate},
		nil,
		[]sourceFixtureDisposition{{
			File: candidate.File, Line: candidate.Line, Column: candidate.Column,
			Surface: candidate.Surface, Value: &value, Basis: "reviewed:other",
		}},
		nil,
	)
	assertFixtureFindingCode(t, result.Findings, "broad-disposition")
}

func TestUnrelatedDispositionFailsClosedWhenDuplicatedOrConflicting(t *testing.T) {
	t.Parallel()
	value := "robotics.state.armed"
	candidate := FixtureCandidate{
		File: "fixture_test.go", Line: 2, Column: 27, Location: "line:2:column:27",
		Predicate: "robotics.state.armed", Surface: "go-field:Predicate",
	}
	disposition := sourceFixtureDisposition{
		File: candidate.File, Line: candidate.Line, Column: candidate.Column,
		Surface: candidate.Surface, Value: &value, Basis: "reviewed:other",
	}

	duplicate := classifyFixtureCandidates(
		[]FixtureCandidate{candidate}, nil, []sourceFixtureDisposition{disposition, disposition}, nil,
	)
	assertFixtureFindingCode(t, duplicate.Findings, "duplicate-disposition")

	conflicting := classifyFixtureCandidates(
		[]FixtureCandidate{candidate},
		[]sourceFixtureClassification{{
			File: candidate.File, Line: candidate.Line,
			Kind: FixtureStoredPredicateKind, Value: &value, Reason: "segment_character",
		}},
		[]sourceFixtureDisposition{disposition},
		nil,
	)
	assertFixtureFindingCode(t, conflicting.Findings, "conflicting-disposition")
}

func TestAuditTestFixturesSourceClassificationsFailClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		source   string
		wantCode string
	}{
		{
			name: "wrong reason",
			source: `package fixture
var _ = Triple{Predicate: "legacy.state.bad_name"} // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"arity"}
`,
			wantCode: "wrong-reason",
		},
		{
			name: "wrong kind",
			source: `package fixture
var _ = Triple{Predicate: "legacy.state.bad_name"} // predicate-audit:invalid {"kind":"query-pattern","value":"legacy.state.bad_name","reason":"segment_character"}
`,
			wantCode: "wrong-kind",
		},
		{
			name: "stale valid value",
			source: `package fixture
var _ = Triple{Predicate: "robotics.state.armed"} // predicate-audit:invalid {"kind":"stored-predicate","value":"robotics.state.armed","reason":"segment_character"}
`,
			wantCode: "stale-classification",
		},
		{
			name: "adjacent raw literal not swallowed",
			source: `package fixture
var _, _ = Triple{Predicate: "legacy.state.bad_name"}, Triple{Predicate: "other.state.bad_name"} // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
`,
			wantCode: "unclassified",
		},
		{
			name: "identical adjacent occurrence is broad",
			source: `package fixture
var _, _ = Triple{Predicate: "legacy.state.bad_name"}, Triple{Predicate: "legacy.state.bad_name"} // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
`,
			wantCode: "broad-classification",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeAuditFixture(t, root, "negative_test.go", test.source)
			result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
			if err != nil {
				t.Fatal(err)
			}
			assertFixtureFindingCode(t, result.Findings, test.wantCode)
		})
	}
}

func TestAuditTestFixturesRejectsDuplicateSourceClassification(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "negative_test.go", `package fixture
var _ = Triple{Predicate: "legacy.state.bad_name"} // predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"} predicate-audit:invalid {"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
`)
	_, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err == nil {
		t.Fatal("AuditTestFixtures() error = nil, want malformed duplicate source payload rejected")
	}
}

func TestAuditTestFixturesUsesExactStructuredLocations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "nested/testdata/seed.json", `{"records":[{"predicate":"legacy.state.bad_name"},{"predicate":"other.state.bad_name"}]}`)
	manifest := writeFixtureManifest(t, root, `{
  "version": 1,
  "entries": [
    {"file":"nested/testdata/seed.json","location":"/records/0/predicate","document":1,"record":0,"occurrence":1,"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
  ]
}`)

	result, err := AuditTestFixtures(manifest, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Predicate != "other.state.bad_name" ||
		result.Findings[0].Location != "/records/1/predicate" || result.Findings[0].Occurrence != 1 {
		t.Fatalf("findings = %#v, want only exact second record unclassified", result.Findings)
	}
}

func TestAuditTestFixturesUsesExactJSONLRecordLocations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "testdata/seed.jsonl", "{\"predicate\":\"legacy.state.bad_name\"}\n{\"predicate\":\"legacy.state.bad_name\"}\n")
	manifest := writeFixtureManifest(t, root, `{
  "version": 1,
  "entries": [
    {"file":"testdata/seed.jsonl","location":"/predicate","document":1,"record":2,"occurrence":1,"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
  ]
}`)

	result, err := AuditTestFixtures(manifest, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Location != "/predicate" || result.Findings[0].Record != 1 {
		t.Fatalf("findings = %#v, want record 1 only", result.Findings)
	}
}

func TestAuditTestFixturesManifestFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{
			name: "duplicate",
			manifest: `{"version":1,"entries":[
{"file":"testdata/seed.json","location":"/predicate","document":1,"record":0,"occurrence":1,"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"},
{"file":"testdata/seed.json","location":"/predicate","document":1,"record":0,"occurrence":1,"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
]}`,
			want: "duplicate",
		},
		{
			name: "broad missing location",
			manifest: `{"version":1,"entries":[
{"file":"testdata/seed.json","location":"","document":1,"record":0,"occurrence":1,"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
]}`,
			want: "must set",
		},
		{
			name:     "unknown field",
			manifest: `{"version":1,"entries":[],"allow_all":true}`,
			want:     "unknown field",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			manifest := writeFixtureManifest(t, root, test.manifest)
			_, err := AuditTestFixtures(manifest, root)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("AuditTestFixtures() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAuditTestFixturesRecognizesAuthoritativePredicateHelper(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
import (
  "testing"
  "github.com/c360studio/semstreams/internal/semantictest"
)
func TestFixture(t *testing.T) {
  domain := dynamicDomain()
  _ = semantictest.Predicate(t, "robotics", "state", "armed")
  _ = semantictest.Predicate(t, domain, "state", "armed")
  _ = Triple{Predicate: "legacy.state.bad_name"}
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Predicate != "legacy.state.bad_name" {
		t.Fatalf("findings = %#v, want adjacent raw invalid only", result.Findings)
	}
	helperCandidates := 0
	dynamic := false
	for _, candidate := range result.Candidates {
		if candidate.Surface != "go-call:semantictest.Predicate" {
			continue
		}
		helperCandidates++
		dynamic = dynamic || candidate.Predicate == ""
	}
	if helperCandidates != 2 || !dynamic {
		t.Fatalf("candidates = %#v, want literal and dynamic authoritative helper calls", result.Candidates)
	}
}

func TestAuditTestFixturesPredicateHelperDoesNotHideMalformedPositions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		positions [3]string
		want      string
	}{
		{name: "uppercase", positions: [3]string{"Robotics", "state", "armed"}, want: "Robotics.state.armed"},
		{name: "underscore", positions: [3]string{"robotics", "unit_state", "armed"}, want: "robotics.unit_state.armed"},
		{name: "bad hyphen", positions: [3]string{"robotics", "state", "armed-"}, want: "robotics.state.armed-"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := `package fixture
import (
  "testing"
  "github.com/c360studio/semstreams/internal/semantictest"
)
func TestFixture(t *testing.T) { _ = semantictest.Predicate(t, ` +
				strconv.Quote(test.positions[0]) + `, ` + strconv.Quote(test.positions[1]) + `, ` + strconv.Quote(test.positions[2]) + `) }
`
			writeAuditFixture(t, root, "fixture_test.go", source)
			result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Findings) != 1 || result.Findings[0].Predicate != test.want {
				t.Fatalf("findings = %#v, want exact helper input %q rejected", result.Findings, test.want)
			}
		})
	}
}

func TestAuditTestFixturesRejectsAmbiguousPredicateHelperResolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "alias",
			source: `package fixture
import st "github.com/c360studio/semstreams/internal/semantictest"
var _ = st.Predicate
`,
			want: "canonical unaliased import name",
		},
		{
			name: "dot import",
			source: `package fixture
import . "github.com/c360studio/semstreams/internal/semantictest"
var _ = Predicate
`,
			want: "canonical unaliased import name",
		},
		{
			name: "shadow",
			source: `package fixture
import "github.com/c360studio/semstreams/internal/semantictest"
func fixture() { semantictest := struct{ Predicate string }{}; _ = semantictest.Predicate }
`,
			want: "shadows the canonical",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeAuditFixture(t, root, "fixture_test.go", test.source)
			_, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("AuditTestFixtures() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestAuditTestFixturesDoesNotTrustLookalikeHelper(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
var semantictest = struct{ Predicate func(...string) string }{}
func fixture() { _ = semantictest.Predicate("robotics", "state", "armed") }
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range result.Candidates {
		if candidate.Surface == "go-call:semantictest.Predicate" {
			t.Fatalf("lookalike call trusted as authoritative: %#v", candidate)
		}
	}
}

func TestAuditTestFixturesTreatsTableTestNameAsDescriptive(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
var _ = []struct{name, input string}{
  {name: "invalid $entity.triple.X remains unchanged", input: "$entity.triple.robotics.state.armed"},
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("findings = %#v, want descriptive name excluded", result.Findings)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Predicate != "robotics.state.armed" {
		t.Fatalf("candidates = %#v, want semantic input only", result.Candidates)
	}
}

func TestAuditTestFixturesChecksRawAuthorityCallsAndExplicitNegativeParts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "internal/semantictest/fixture_test.go", `package semantictest
var _ = validatePredicateFixture("legacy", "state", "bad_name")
var _ = []struct {
  parts [3]string
  wantReason PredicateValidationReason
}{
  {parts: [3]string{"Agentic", "loop", "state"}, wantReason: PredicateReasonSegmentStart},
}
`)
	writeAuditFixture(t, root, "vocabulary/parser_test.go", `package vocabulary
func fixture() { _, _ = ParsePredicate("other.state.bad_name") }
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]bool{
		"legacy.state.bad_name": false,
		"Agentic.loop.state":    false,
		"other.state.bad_name":  false,
	}
	for _, finding := range result.Findings {
		if _, expected := wants[finding.Predicate]; expected {
			wants[finding.Predicate] = true
		}
	}
	for predicate, found := range wants {
		if !found {
			t.Fatalf("findings = %#v, want raw authority fixture %q", result.Findings, predicate)
		}
	}
}

func TestAuditTestFixturesPreservesDuplicateJSONPhysicalOccurrences(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		json string
	}{
		{name: "identical", json: `{"predicate":"legacy.state.bad_name","predicate":"legacy.state.bad_name"}`},
		{name: "different", json: `{"predicate":"legacy.state.bad_name","predicate":"other.state.bad_name"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeAuditFixture(t, root, "testdata/duplicate.json", test.json)
			result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Candidates) != 2 || len(result.Findings) != 2 {
				t.Fatalf("result = %#v, want two physical duplicate-key occurrences", result)
			}
			if result.Candidates[0].Location != "/predicate" || result.Candidates[0].Occurrence != 1 ||
				result.Candidates[1].Location != "/predicate" || result.Candidates[1].Occurrence != 2 {
				t.Fatalf("candidates = %#v, want distinct duplicate-key locations", result.Candidates)
			}
		})
	}
}

func TestAuditTestFixturesPreservesDuplicateJSONLKeysWithinRecord(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(
		t,
		root,
		"testdata/duplicate.jsonl",
		"{\"predicate\":\"legacy.state.bad_name\",\"predicate\":\"other.state.bad_name\"}\n",
	)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 || result.Candidates[0].Location != "/predicate" ||
		result.Candidates[0].Record != 1 || result.Candidates[0].Occurrence != 1 ||
		result.Candidates[1].Location != "/predicate" || result.Candidates[1].Record != 1 ||
		result.Candidates[1].Occurrence != 2 {
		t.Fatalf("candidates = %#v, want both duplicate JSONL keys in one record", result.Candidates)
	}
}

func TestAuditTestFixturesPreservesYAMLDuplicatesDocumentsAndAliasUse(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "testdata/duplicates.yaml", `anchor: &bad legacy.state.bad_name
predicate: *bad
predicate: other.state.bad_name
---
predicate: third.state.bad_name
predicate: third.state.bad_name
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 4 {
		t.Fatalf("candidates = %#v, want alias use plus different and identical duplicate keys across documents", result.Candidates)
	}
	wantLocations := map[string]bool{
		"1:/predicate:1": false,
		"1:/predicate:2": false,
		"2:/predicate:1": false,
		"2:/predicate:2": false,
	}
	for _, candidate := range result.Candidates {
		identity := fmt.Sprintf("%d:%s:%d", candidate.Document, candidate.Location, candidate.Occurrence)
		if _, ok := wantLocations[identity]; ok {
			wantLocations[identity] = true
		}
		if candidate.Predicate == "legacy.state.bad_name" && candidate.Line != 2 {
			t.Fatalf("alias candidate = %#v, want semantic use at alias line 2", candidate)
		}
	}
	for location, found := range wantLocations {
		if !found {
			t.Fatalf("candidates = %#v, missing %s", result.Candidates, location)
		}
	}
}

func TestAuditTestFixturesManifestIdentityIsPhysicalLocationNotValue(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "testdata/seed.json", `{"predicate":"legacy.state.bad_name"}`)
	manifest := writeFixtureManifest(t, root, `{"version":1,"entries":[
{"file":"testdata/seed.json","location":"/predicate","document":1,"record":0,"occurrence":1,"kind":"stored-predicate","value":"other.state.bad_name","reason":"segment_character"}
]}`)
	result, err := AuditTestFixtures(manifest, root)
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureFindingCode(t, result.Findings, "wrong-value")
	for _, finding := range result.Findings {
		if finding.Code == "stale-classification" || finding.Code == "unclassified" {
			t.Fatalf("findings = %#v, location identity must resolve before value validation", result.Findings)
		}
	}
}

func TestAuditTestFixturesFailsUnresolvedKnownGoSurfaceAndInventoriesVocabularyFixtures(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
func fixture(predicate string) { _ = Triple{Predicate: predicate} }
`)
	writeAuditFixture(t, root, "vocabulary/predicate_contract_test.go", `package vocabulary
import (
  "strings"
  "testing"
)
var _ = []struct{ predicate string }{
  {predicate: "agent.run." + strings.Repeat("a", MaxPredicateSegmentBytes+1)},
}
func FuzzParsePredicateProbe(f *testing.F) {
  for _, seed := range []string{"", "agent.run.*"} { f.Add(seed) }
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureFindingCode(t, result.Findings, "unresolved-go-surface")
	wants := map[string]bool{
		"agent.run." + strings.Repeat("a", vocabulary.MaxPredicateSegmentBytes+1): false,
		"":            false,
		"agent.run.*": false,
	}
	for _, candidate := range result.Candidates {
		if _, ok := wants[candidate.Predicate]; ok && !candidate.Unresolved {
			wants[candidate.Predicate] = true
		}
	}
	for predicate, found := range wants {
		if !found {
			t.Fatalf("candidates = %#v, missing exact vocabulary fixture %q", result.Candidates, predicate)
		}
	}
}

func TestAuditTestFixturesDoesNotTrustArbitraryParserNames(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
func ParsePredicate(string) (string, error) { return "", nil }
func IsValidPredicate(string) bool { return true }
func fixture() { _, _ = ParsePredicate("legacy.state.bad_name"); _ = IsValidPredicate("other.state.bad_name") }
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 0 || len(result.Findings) != 0 {
		t.Fatalf("result = %#v, arbitrary same-named functions are unrelated", result)
	}
}

func TestAuditTestFixturesRejectsPredicateHelperAliasesAndWrappers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
import "github.com/c360studio/semstreams/internal/semantictest"
var helper = semantictest.Predicate
func wrapped(t interface{}) string {
  value := semantictest.Predicate(t, "robotics", "state", "armed")
  return value
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureFindingCode(t, result.Findings, "helper-alias")
	assertFixtureFindingCode(t, result.Findings, "helper-wrapper")
}

func TestAuditTestFixturesDoesNotTreatSelfPackagePredicateFieldAsHelperAlias(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "internal/semantictest/fixtures_test.go", `package semantictest
import "github.com/c360studio/semstreams/vocabulary"
func fixture(validationError *vocabulary.PredicateValidationError) { _ = validationError.Predicate }
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("findings = %#v, a selector field is not the package helper", result.Findings)
	}
}

func TestAuditTestFixturesDefersDirectKnownSurfacesToPredicateHelper(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
import (
  "testing"
  "github.com/c360studio/semstreams/internal/semantictest"
)
func TestFixture(t *testing.T) {
  triple := Triple{Predicate: semantictest.Predicate(t, "robotics", "state", "armed")}
  triple.Predicate = semantictest.Predicate(t, "robotics", "state", "ready")
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("findings = %#v, exact direct helper calls must own the known scalar surfaces", result.Findings)
	}
	helperCandidates := 0
	for _, candidate := range result.Candidates {
		if candidate.Surface == "go-call:semantictest.Predicate" {
			helperCandidates++
		}
	}
	if helperCandidates != 2 {
		t.Fatalf("candidates = %#v, want two authoritative helper occurrences", result.Candidates)
	}
}

func TestAuditTestFixturesKeepsWrappedAndForwardedPredicateHelpersUnresolved(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
import (
  "strings"
  "testing"
  "github.com/c360studio/semstreams/internal/semantictest"
)
func TestFixture(t *testing.T) {
  _ = Triple{Predicate: strings.TrimSpace(semantictest.Predicate(t, "robotics", "state", "armed"))}
  forwarded := semantictest.Predicate(t, "robotics", "state", "ready")
  _ = Triple{Predicate: forwarded}
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	unresolved := 0
	for _, finding := range result.Findings {
		if finding.Code == "unresolved-go-surface" {
			unresolved++
		}
	}
	if unresolved != 2 {
		t.Fatalf("findings = %#v, want outer call and forwarded identifier rejected", result.Findings)
	}
}

func TestAuditTestFixturesExpandsExactPluralPredicateContainers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
import (
  "testing"
  "github.com/c360studio/semstreams/internal/semantictest"
)
func TestFixture(t *testing.T) {
  _ = Config{Predicates: []string{
    semantictest.Predicate(t, "robotics", "state", "armed"),
    "robotics.state.ready",
  }}
  _ = Config{ReferencePredicates: []any{"robotics.link.parent", "robotics.link.child"}}
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("findings = %#v, exact plural literals must be audited element-by-element", result.Findings)
	}
	wants := map[string]bool{
		"robotics.state.armed": false,
		"robotics.state.ready": false,
		"robotics.link.parent": false,
		"robotics.link.child":  false,
	}
	for _, candidate := range result.Candidates {
		if _, wanted := wants[candidate.Predicate]; wanted && !candidate.Unresolved {
			wants[candidate.Predicate] = true
		}
	}
	for predicate, found := range wants {
		if !found {
			t.Fatalf("candidates = %#v, missing physical plural element %q", result.Candidates, predicate)
		}
	}
}

func TestAuditTestFixturesRejectsPluralAliasesWrappersAndDynamicElements(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
func fixture() {
  base := []string{"robotics.state.armed"}
  alias := base
  _ = Config{Predicates: base}
  _ = Config{Predicates: alias}
  _ = Config{ReferencePredicates: buildPredicates()}
  _ = Config{Predicates: []interface{}{"robotics.state.ready", dynamicPredicate()}}
  mutable := []string{"robotics.state.mutable"}
  mutable[0] = dynamicPredicate()
  _ = Config{Predicates: mutable}
  aliased := []string{"robotics.state.aliased"}
  aliasView := aliased
  aliasView[0] = dynamicPredicate()
  _ = Config{Predicates: aliased}
  copied := append([]string(nil), base...)
  _ = Config{Predicates: copied}
  escaped := []string{"robotics.state.escaped"}
  consume(escaped)
  _ = Config{Predicates: escaped}
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	unresolved := 0
	for _, finding := range result.Findings {
		if finding.Code == "unresolved-go-surface" {
			unresolved++
		}
	}
	if unresolved != 8 {
		t.Fatalf("findings = %#v, want every reference, alias, wrapper, mutation, copy, escape, and dynamic element rejected", result.Findings)
	}
}

func TestAuditTestFixturesRejectsNonSlicePluralPredicateSurfaces(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
import (
  "testing"
  "github.com/c360studio/semstreams/internal/semantictest"
)
func TestFixture(t *testing.T) {
  _ = Config{Predicates: "robotics.state.armed"}
  _ = Config{Predicates: semantictest.Predicate(t, "robotics", "state", "ready")}
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	unresolved := 0
	helper := 0
	for _, candidate := range result.Candidates {
		if candidate.Unresolved && strings.HasPrefix(candidate.Surface, "go-field:Predicates") {
			unresolved++
		}
		if candidate.Surface == "go-call:semantictest.Predicate" {
			helper++
		}
	}
	if unresolved != 2 || helper != 1 {
		t.Fatalf("candidates = %#v, want both plural surfaces unresolved and helper independently inventoried", result.Candidates)
	}
	findings := 0
	for _, finding := range result.Findings {
		if finding.Code == "unresolved-go-surface" {
			findings++
		}
	}
	if findings != 2 {
		t.Fatalf("findings = %#v, want both non-slice plural surfaces rejected", result.Findings)
	}
}

func TestAuditTestFixturesSkipsOnlyDefinitelyNonStringPredicateControls(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	source := `package fixture
var _ = []map[string]any{
  {"include_predicates": false},
  {"include_predicates": !true},
  {"include_predicates": bool(false)},
  {"include_predicates": 1 == 1},
  {"include_predicates": ` + strconv.Quote("false") + `},
  {"predicate": "robotics.state.armed"},
  {"predicate": false},
  {"predicates": false},
  {"predicates": 7},
}
var _ = Config{IncludePredicates: false}
var _ = Config{Predicate: false}
var _ = Config{Predicates: 7}
`
	writeAuditFixture(t, root, "fixture_test.go", source)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	unresolved := 0
	invalidStrings := 0
	for _, finding := range result.Findings {
		switch {
		case finding.Code == "unresolved-go-surface":
			unresolved++
		case finding.Predicate == "false":
			invalidStrings++
		}
	}
	if unresolved != 5 || invalidStrings != 1 {
		t.Fatalf("result = %#v, want only include_predicates controls excluded and wrong-typed predicate fields unresolved", result)
	}
}

func TestAuditTestFixturesRequiresUnsupportedArtifactDispositionAndParsesNDJSON(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "testdata/config.toml", `predicates = ["legacy.state.bad_name"]`)
	writeAuditFixture(t, root, "testdata/seed.ndjson", "{\"predicate\":\"other.state.bad_name\"}\n")

	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	assertFixtureFindingCode(t, result.Findings, "unsupported-artifact")
	foundNDJSON := false
	for _, candidate := range result.Candidates {
		foundNDJSON = foundNDJSON || candidate.File == "testdata/seed.ndjson"
	}
	if !foundNDJSON {
		t.Fatalf("candidates = %#v, want .ndjson structurally parsed", result.Candidates)
	}

	manifest := writeFixtureManifest(t, root, `{"version":1,"entries":[],"unrelated_artifacts":[
{"file":"testdata/config.toml","classification":"unrelated","basis":"probe has no supported TOML parser"}
]}`)
	result, err = AuditTestFixtures(manifest, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range result.Findings {
		if finding.Code == "unsupported-artifact" {
			t.Fatalf("findings = %#v, exact TOML disposition should satisfy inventory", result.Findings)
		}
	}
}

func TestAuditTestFixturesEmbeddedMultilineOccurrencesUseInnerCoordinates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	base := `package fixture
var config = ` + "`" + `{
  "predicate": "legacy.state.bad_name", "predicate": "legacy.state.bad_name"
}` + "`" + `
`
	writeAuditFixture(t, root, "fixture_test.go", base)
	manifest := writeFixtureManifest(t, root, `{"version":1,"entries":[]}`)
	result, err := AuditTestFixtures(manifest, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 || result.Candidates[0].Line != 3 ||
		result.Candidates[0].Column == result.Candidates[1].Column {
		t.Fatalf("candidates = %#v, want two inner-line physical occurrences", result.Candidates)
	}

	annotation := `// predicate-audit:invalid {"location":` + strconv.Quote(result.Candidates[0].Location) +
		`,"kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}` + "\n"
	writeAuditFixture(t, root, "fixture_test.go", base+annotation)
	result, err = AuditTestFixtures(manifest, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 1 || result.Findings[0].Location != result.Candidates[1].Location {
		t.Fatalf("findings = %#v, want exact embedded occurrence annotation only", result.Findings)
	}
}

func TestAuditTestFixturesInventoriesEmptyAndSymbolicStructuredValues(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "testdata/values.json", `{"predicate":"","predicate":"CONNECTED_TO"}`)
	manifest := writeFixtureManifest(t, root, `{"version":1,"entries":[
{"file":"testdata/values.json","location":"/predicate","document":1,"record":0,"occurrence":1,"kind":"stored-predicate","value":"","reason":"empty"}
]}`)
	result, err := AuditTestFixtures(manifest, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 || len(result.Findings) != 1 ||
		result.Findings[0].Predicate != "CONNECTED_TO" {
		t.Fatalf("result = %#v, want classified empty plus visible symbolic candidate", result)
	}
	missingValueManifest := writeFixtureManifest(t, root, `{"version":1,"entries":[
{"file":"testdata/values.json","location":"/predicate","document":1,"record":0,"occurrence":1,"kind":"stored-predicate","reason":"empty"}
]}`)
	if _, err := AuditTestFixtures(missingValueManifest, root); err == nil ||
		!strings.Contains(err.Error(), "value") {
		t.Fatalf("AuditTestFixtures() error = %v, want missing value rejected", err)
	}
}

func TestAuditTestFixturesMapsInterpretedEmbeddedStringOffsets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
var config = "predicate = \"legacy.state.bad_name\"\npredicate = \"other.state.bad_name\""
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 || result.Candidates[0].Line != 2 || result.Candidates[1].Line != 2 ||
		result.Candidates[0].Column == result.Candidates[1].Column {
		t.Fatalf("candidates = %#v, want two decoded occurrences mapped to physical token offsets", result.Candidates)
	}
	for _, candidate := range result.Candidates {
		if !strings.Contains(candidate.Location, "inner-offset:") {
			t.Fatalf("candidate = %#v, want explicit decoded inner offset", candidate)
		}
	}
}

func TestAuditTestFixturesFindsStringMapKeysAndAssignmentTargets(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
func fixture(value string) {
  _ = map[string]string{"predicate": value}
  var config struct{ Predicate string }
  config.Predicate = value
  values := map[string]string{}
  values["predicate"] = value
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	unresolved := 0
	for _, finding := range result.Findings {
		if finding.Code == "unresolved-go-surface" {
			unresolved++
		}
	}
	if unresolved != 3 {
		t.Fatalf("findings = %#v, want map-key, selector, and index assignment unresolved", result.Findings)
	}
}

func TestAuditTestFixturesRejectsClosureVoidAndOutputHelperForwarding(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
import (
  "testing"
  "github.com/c360studio/semstreams/internal/semantictest"
)
func TestOuter(t *testing.T) {
  closure := func() { _ = semantictest.Predicate(t, "robotics", "state", "armed") }
  closure()
}
func fill(t interface{}, output *Triple) { output.Predicate = semantictest.Predicate(t, "robotics", "state", "armed") }
func void(t interface{}) { consume(semantictest.Predicate(t, "robotics", "state", "armed")) }
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	wrappers := 0
	for _, finding := range result.Findings {
		if finding.Code == "helper-wrapper" {
			wrappers++
		}
	}
	if wrappers != 3 {
		t.Fatalf("findings = %#v, want closure, output, and void forwarding rejected", result.Findings)
	}
}

func TestAuditTestFixturesOnlyExemptsExactGoTestEntrypoints(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
import (
  "testing"
  "github.com/c360studio/semstreams/internal/semantictest"
)
type suite struct{}
func TestFake(t interface{}) { _ = semantictest.Predicate(t, "robotics", "state", "armed") }
func TestExtra(t *testing.T, extra string) { _ = semantictest.Predicate(t, "robotics", "state", "armed") }
func (suite) TestMethod(t *testing.T) { _ = semantictest.Predicate(t, "robotics", "state", "armed") }
func BenchmarkWrong(t *testing.T) { _ = semantictest.Predicate(t, "robotics", "state", "armed") }
func TestGood(t *testing.T) { _ = semantictest.Predicate(t, "robotics", "state", "armed") }
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	wrappers := 0
	for _, finding := range result.Findings {
		if finding.Code == "helper-wrapper" {
			wrappers++
		}
	}
	if wrappers != 4 {
		t.Fatalf("findings = %#v, want every fake entrypoint rejected and exact TestGood exempted", result.Findings)
	}
}

func TestAuditTestFixturesAllowsExactTestingRunCallbacks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
import (
  "testing"
  "github.com/c360studio/semstreams/internal/semantictest"
)
func TestFixture(t *testing.T) {
  t.Run("outer", func(t *testing.T) {
    _ = Triple{Predicate: semantictest.Predicate(t, "robotics", "state", "armed")}
    t.Run("inner", func(inner *testing.T) {
      _ = semantictest.Predicate(inner, "robotics", "state", "ready")
    })
  })
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("findings = %#v, exact testing.T Run callbacks are test entrypoints", result.Findings)
	}
}

func TestAuditTestFixturesRejectsFakeAndMalformedRunCallbacks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "fixture_test.go", `package fixture
import (
  "testing"
  "github.com/c360studio/semstreams/internal/semantictest"
)
type fakeRunner struct{}
func (fakeRunner) Run(string, func(*testing.T)) {}
func TestFixture(t *testing.T) {
  fakeRunner{}.Run("fake", func(t *testing.T) {
    _ = semantictest.Predicate(t, "robotics", "state", "armed")
  })
  t.Run("wrong", func(value interface{}) {
    _ = semantictest.Predicate(value, "robotics", "state", "ready")
  })
  run := t.Run
  run("alias", func(t *testing.T) {
    _ = semantictest.Predicate(t, "robotics", "state", "landed")
  })
  receiverAlias := t
  receiverAlias.Run("receiver alias", func(t *testing.T) {
    _ = semantictest.Predicate(t, "robotics", "state", "hovering")
  })
  callback := func(t *testing.T) {
    _ = semantictest.Predicate(t, "robotics", "state", "forwarded")
  }
  t.Run("forwarded callback", callback)
  t.Run("nested closure", func(t *testing.T) {
    closure := func() {
      _ = semantictest.Predicate(t, "robotics", "state", "nested")
    }
    closure()
  })
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	wrappers := 0
	for _, finding := range result.Findings {
		if finding.Code == "helper-wrapper" {
			wrappers++
		}
	}
	if wrappers != 6 {
		t.Fatalf("findings = %#v, want fake receiver, wrong signature, Run/receiver aliases, forwarded callback, and nested closure rejected", result.Findings)
	}
}

func TestAuditTestFixturesInventoriesDirectAndReferencedFuzzAdds(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "vocabulary/fuzz_test.go", `package vocabulary
import "testing"
var referencedSeeds = []string{"agent.run.*", ""}
func FuzzPredicateInputs(f *testing.F) {
  f.Add("direct.state.bad_name")
  for _, seed := range referencedSeeds { f.Add(seed) }
  localSeeds := []string{"Agent.run.phase"}
  for _, local := range localSeeds { f.Add(local) }
  dynamic := dynamicSeed()
  f.Add(dynamic)
}
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]bool{
		"direct.state.bad_name": false,
		"agent.run.*":           false,
		"":                      false,
		"Agent.run.phase":       false,
	}
	for _, candidate := range result.Candidates {
		if _, ok := wants[candidate.Predicate]; ok && candidate.Surface == "go-fuzz-seed" && !candidate.Unresolved {
			wants[candidate.Predicate] = true
		}
	}
	for value, found := range wants {
		if !found {
			t.Fatalf("candidates = %#v, missing fuzz seed %q", result.Candidates, value)
		}
	}
	assertFixtureFindingCode(t, result.Findings, "unresolved-go-surface")
}

func TestAuditTestFixturesFailsClosedAndInventoriesFuzzAliases(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "vocabulary/fuzz_alias_test.go", `package vocabulary
import "testing"
func FuzzAliases(f *testing.F) {
  add := f.Add
  add("alias.state.bad_name")
  ff := f
  ff.Add("receiver.state.bad_name")
  f.Add("direct.state.bad_name")
}
func FuzzFake(f interface{ Add(...any) }) { f.Add("fake.state.bad_name") }
`)
	result, err := AuditTestFixtures(writeFixtureManifest(t, root, `{"version":1,"entries":[]}`), root)
	if err != nil {
		t.Fatal(err)
	}
	wants := map[string]bool{
		"alias.state.bad_name":    false,
		"receiver.state.bad_name": false,
		"direct.state.bad_name":   false,
	}
	unresolved := 0
	for _, candidate := range result.Candidates {
		if candidate.Unresolved && (candidate.Surface == "go-fuzz-add-alias" || candidate.Surface == "go-fuzz-receiver-alias") {
			unresolved++
		}
		if _, ok := wants[candidate.Predicate]; ok && candidate.Surface == "go-fuzz-seed" && !candidate.Unresolved {
			wants[candidate.Predicate] = true
		}
		if candidate.Predicate == "fake.state.bad_name" && candidate.Surface == "go-fuzz-seed" {
			t.Fatalf("candidates = %#v, fake fuzz signature must not establish audit authority", result.Candidates)
		}
	}
	if unresolved != 2 {
		t.Fatalf("candidates = %#v, want method and receiver alias bypasses rejected", result.Candidates)
	}
	for value, found := range wants {
		if !found {
			t.Fatalf("candidates = %#v, missing fuzz alias input %q", result.Candidates, value)
		}
	}
}

func assertFixtureFindingCode(t *testing.T, findings []FixtureFinding, want string) {
	t.Helper()
	for _, finding := range findings {
		if finding.Code == want {
			return
		}
	}
	t.Fatalf("findings = %#v, want code %q", findings, want)
}

func writeFixtureManifest(t *testing.T, root, content string) string {
	t.Helper()
	path := filepath.Join(root, "predicate-invalid-fixtures.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func unrelatedPredicateFieldLine(expression, value, surface, basis string) string {
	const prefix = "var _ = Triple{Predicate: "
	return fmt.Sprintf(
		"%s%s} // %s {\"column\":%d,\"surface\":%s,\"value\":%s,\"basis\":%s}",
		prefix,
		expression,
		FixtureUnrelatedMarker,
		len(prefix)+1,
		strconv.Quote(surface),
		strconv.Quote(value),
		strconv.Quote(basis),
	)
}

// Exact self-corpus classifications for malformed predicates embedded in the
// source and structured fixture strings above. Location binds each physical
// occurrence without changing the fixture content exercised by these tests.
// predicate-audit:invalid {"location":"line:1106:column:71:embedded-structured:inner-offset:14","kind":"stored-predicate","value":"other.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:1140:column:17:embedded-structured:inner-offset:18","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:1140:column:55:embedded-structured:inner-offset:56","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:1169:column:68:embedded-structured:inner-offset:14","kind":"stored-predicate","value":"","reason":"empty"}
// predicate-audit:invalid {"location":"line:1169:column:83:embedded-structured:inner-offset:29","kind":"stored-predicate","value":"CONNECTED_TO","reason":"arity"}
// predicate-audit:invalid {"location":"line:162:column:28:embedded-structured:inner-offset:43","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:18:column:28:embedded-structured:inner-offset:43","kind":"stored-predicate","value":"production.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:21:column:28:embedded-structured:inner-offset:43","kind":"stored-predicate","value":"test.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:23:column:73:embedded-structured:inner-offset:14","kind":"stored-predicate","value":"seed.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:244:column:42:embedded-structured:inner-offset:27","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:348:column:28:embedded-structured:inner-offset:43","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:355:column:28:embedded-structured:inner-offset:43","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:369:column:31:embedded-structured:inner-offset:46","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:369:column:75:embedded-structured:inner-offset:90","kind":"stored-predicate","value":"other.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:376:column:31:embedded-structured:inner-offset:46","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:376:column:75:embedded-structured:inner-offset:90","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:400:column:28:embedded-structured:inner-offset:43","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:411:column:123:embedded-structured:inner-offset:64","kind":"stored-predicate","value":"other.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:411:column:85:embedded-structured:inner-offset:26","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:432:column:113:embedded-structured:inner-offset:52","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:432:column:70:embedded-structured:inner-offset:14","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:502:column:26:embedded-structured:inner-offset:304","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:52:column:28:embedded-structured:inner-offset:43","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:629:column:34:embedded-substitution:inner-offset:87","kind":"stored-predicate","value":"X","reason":"arity"}
// predicate-audit:invalid {"location":"line:686:column:44:embedded-structured:inner-offset:14","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:686:column:80:embedded-structured:inner-offset:50","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:687:column:44:embedded-structured:inner-offset:14","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:687:column:80:embedded-structured:inner-offset:50","kind":"stored-predicate","value":"other.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:716:column:21:embedded-structured:inner-offset:14","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:716:column:61:embedded-structured:inner-offset:50","kind":"stored-predicate","value":"other.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:772:column:66:embedded-structured:inner-offset:14","kind":"stored-predicate","value":"legacy.state.bad_name","reason":"segment_character"}
// predicate-audit:invalid {"location":"line:800:column:16:embedded-structured:inner-offset:107","kind":"stored-predicate","value":"agent.run.","reason":"segment_empty"}
