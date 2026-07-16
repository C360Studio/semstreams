package entityidaudit

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGoldenCorpusReport(t *testing.T) {
	t.Parallel()
	candidates, findings, err := Audit("testdata/corpus")
	if err != nil {
		t.Fatal(err)
	}
	report := BuildReport([]string{"testdata/corpus"}, "fixture", Result{Candidates: candidates, Findings: findings})
	got, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("UPDATE_ENTITY_ID_AUDIT_GOLDEN") == "1" {
		if err := os.WriteFile("testdata/corpus-report.json", got, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile("testdata/corpus-report.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("corpus report drifted\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestAuditExtractsTheThreeEntityIDLanguages(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "entities.go", `package fixture
const entityID = "acme.ops.robotics.gcs.drone.001"
const entityPattern = "acme.ops.robotics.*.drone.*"
const entityPrefix = "acme.ops.robotics"
var _ = EntityState{ID: entityID}
var _ = Workflow{EntityIDPattern: entityPattern}
var _ = Query{EntityIDPrefix: entityPrefix}
`)
	writeFixture(t, root, "rules.json", `{
  "entity_id": "acme.ops.robotics.gcs.sensor.001",
  "entity_id_pattern": "acme.ops.robotics.*.sensor.*",
  "entity_id_prefix": "acme.ops.robotics.gcs"
}`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	want := map[Language]bool{LanguageLiteral: true, LanguageDeclarationPattern: true, LanguageQueryPrefix: true}
	for _, candidate := range candidates {
		delete(want, candidate.Language)
	}
	if len(want) != 0 {
		t.Fatalf("candidates = %#v, missing languages %#v", candidates, want)
	}
}

func TestAuditExtractsStaticEntityIDConstructorsAndGraphableReturns(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "constructor.go", `package fixture
var _ = EntityID{Org: "acme", Platform: "ops", Domain: "robotics", System: "gcs", Type: "drone", Instance: "001"}
type payload struct{}
func (payload) EntityID() string { return "acme.ops.robotics.gcs.sensor.002" }
`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	values := map[string]bool{}
	for _, candidate := range candidates {
		values[candidate.Value] = true
	}
	for _, want := range []string{"acme.ops.robotics.gcs.drone.001", "acme.ops.robotics.gcs.sensor.002"} {
		if !values[want] {
			t.Fatalf("candidates = %#v, missing %q", candidates, want)
		}
	}
}

func TestAuditExtractsTripleSubjectsAndTypedReferencesButNotNATSSubjects(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "triples.json", `{
  "triples": [
    {"subject":"acme.ops.robotics.gcs.drone.001","predicate":"robotics.state.related-to","object":"acme.ops.robotics.gcs.sensor.002","object_type":"@id"}
  ],
  "inputs": [{"subject":"raw.sensor.>","type":"jetstream"}]
}`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.Value)
	}
	want := []string{"acme.ops.robotics.gcs.drone.001", "acme.ops.robotics.gcs.sensor.002"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("values = %#v, want %#v", values, want)
	}
}

func TestAuditExtractsJSONLReferenceSeeds(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "seeds.jsonl", "{\"entity_id\":\"acme.ops.robotics.gcs.drone.001\"}\n{\"entity_id_prefix\":\"acme.ops\"}\n")

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 || len(candidates) != 2 {
		t.Fatalf("candidates = %#v, findings = %#v, want two valid JSONL candidates", candidates, findings)
	}
}

func TestAuditRequiresBoundedExplicitClassifications(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "negative.go", `package fixture
// entity-id-audit:classify intentional-malformed "bad.id" line=3 column=25 surface=go-field:EntityState.ID entity_id_invalid:arity parser rejection fixture one
var _ = EntityState{ID: "bad.id"}
// entity-id-audit:classify intentional-malformed "bad.id" line=5 column=25 surface=go-field:EntityState.ID entity_id_invalid:arity parser rejection fixture two
var _ = EntityState{ID: "bad.id"}
// entity-id-audit:classify unrelated-glob "raw.sensor.>" line=7 column=25 surface=go-declaration:entityIDPattern entity_id_pattern_invalid:arity NATS subscription filter
const entityIDPattern = "raw.sensor.>"
`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want classified candidates", findings)
	}
	languages := map[Language]bool{}
	for _, candidate := range candidates {
		languages[candidate.Classification] = true
	}
	if !languages[LanguageIntentionalMalformed] || !languages[LanguageUnrelatedGlob] {
		t.Fatalf("languages = %#v, want both explicit classifications", languages)
	}
	for _, candidate := range candidates {
		if candidate.Value == "bad.id" && candidate.Classification != LanguageIntentionalMalformed {
			t.Fatalf("candidate = %#v, want every exact occurrence classified", candidate)
		}
	}

	writeFixture(t, root, "bad_annotation.go", "package fixture\n// entity-id-audit:classify intentional-malformed \"bad.id\" line=3 column=25 surface=go-field:EntityState.ID "+string(make([]byte, maxAnnotationReasonBytes+1))+"\nvar _ = EntityState{ID: \"bad.id\"}\n")
	if _, _, err := Audit(root); err == nil {
		t.Fatal("Audit() error = nil, want oversized annotation rejection")
	}
}

func TestAuditExtractsContextualEmptyEntityIDsButNotUnrelatedEmptyStrings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "empty.go", `package fixture
var _ = EntityState{ID: ""}
var _ = IsValidEntityID("")
var _ = struct{ Name string }{Name: ""}
`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || len(findings) != 2 {
		t.Fatalf("candidates = %#v, findings = %#v, want two contextual empty entity-ID findings", candidates, findings)
	}
	for _, finding := range findings {
		if finding.Value != "" || finding.Reason != "entity_id_invalid:empty" {
			t.Fatalf("finding = %#v, want exact empty entity-ID classification", finding)
		}
	}
	if candidates[0].Surface != "go-field:EntityState.ID" || candidates[1].Surface != "go-call:IsValidEntityID" {
		t.Fatalf("candidates = %#v, unrelated empty string must remain excluded", candidates)
	}
}

func TestAuditEmptyEntityIDAnnotationResolvesOneExactOccurrence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const classificationReason = "entity_id_invalid:empty deliberate empty root fixture"
	writeFixture(t, root, "empty.go", `package fixture
// entity-id-audit:classify intentional-malformed "" line=3 column=25 surface=go-field:EntityState.ID entity_id_invalid:empty deliberate empty root fixture
var _ = EntityState{ID: ""}
var _ = EntityState{ID: ""}
`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || len(findings) != 1 {
		t.Fatalf("candidates = %#v, findings = %#v, want one exact empty occurrence classified and one finding", candidates, findings)
	}
	if candidates[0].Classification != LanguageIntentionalMalformed || candidates[0].ClassificationReason != classificationReason {
		t.Fatalf("first candidate = %#v, want exact occurrence and stable classification reason", candidates[0])
	}
	if candidates[1].Language != LanguageLiteral || findings[0].Line != 4 || findings[0].Reason != "entity_id_invalid:empty" {
		t.Fatalf("second candidate = %#v, findings = %#v, want unclassified second occurrence", candidates[1], findings)
	}
}

func TestAuditIntentionalSentinelRequiresOneExactOccurrence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "sentinel.go", `package fixture
// entity-id-audit:classify intentional-sentinel "" line=3 column=36 surface=go-field:PrefixQueryRequest.Prefix entity_id_prefix_invalid:empty documented match-all prefix
var _ = PrefixQueryRequest{Prefix: ""}
`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || len(findings) != 0 {
		t.Fatalf("candidates = %#v, findings = %#v, want one classified sentinel", candidates, findings)
	}
	if candidates[0].Classification != LanguageIntentionalSentinel {
		t.Fatalf("candidate = %#v, want intentional sentinel", candidates[0])
	}
}

func TestAuditIntentionalTemplateRequiresOneExactSubstitution(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "template.go", `package fixture
// entity-id-audit:classify intentional-template "$entity.id" line=3 column=18 surface=go-declaration:entityID entity_id_invalid:arity runtime substitution
const entityID = "$entity.id"
`)
	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || len(findings) != 0 || candidates[0].Classification != LanguageIntentionalTemplate {
		t.Fatalf("candidates = %#v, findings = %#v, want one exact template classification", candidates, findings)
	}

	writeFixture(t, root, "not_template.go", `package fixture
// entity-id-audit:classify intentional-template "$garbage" line=3 column=18 surface=go-declaration:entityID entity_id_invalid:arity unknown substitution syntax
const entityID = "$garbage"
`)
	if _, _, err := Audit(root); err == nil {
		t.Fatal("Audit() error = nil, want non-substitution template classification rejection")
	}
}

func TestAuditRejectsReasonMismatchAndValidMalformedClassification(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		name    string
		content string
	}{
		{
			name: "wrong_reason.go",
			content: `package fixture
// entity-id-audit:classify intentional-malformed "bad.id" line=3 column=25 surface=go-field:EntityState.ID entity_id_invalid:alphabet wrong reason
var _ = EntityState{ID: "bad.id"}
`,
		},
		{
			name: "valid_value.go",
			content: `package fixture
// entity-id-audit:classify intentional-malformed "acme.ops.robotics.gcs.drone.001" line=3 column=25 surface=go-field:EntityState.ID entity_id_invalid:arity must not classify valid values
var _ = EntityState{ID: "acme.ops.robotics.gcs.drone.001"}
`,
		},
	} {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFixture(t, root, fixture.name, fixture.content)
			if _, _, err := Audit(root); err == nil {
				t.Fatal("Audit() error = nil, want classification contract rejection")
			}
		})
	}
}

func TestAuditRejectsUnmatchedUnrelatedGlobClassification(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "stale.go", `package fixture
// entity-id-audit:classify unrelated-glob "raw.sensor.>" line=3 column=25 surface=go-declaration:entityIDPattern entity_id_pattern_invalid:arity stale NATS glob
const anotherName = "raw.sensor.>"
`)
	if _, _, err := Audit(root); err == nil {
		t.Fatal("Audit() error = nil, want stale unrelated-glob annotation rejection")
	}
}

func TestAuditExtractsStringKeyedSemanticMapFields(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "maps.go", `package fixture
var _ = map[string]any{"entity_id": "legacy-id"}
var _ = map[string]string{"entity_id_prefix": "acme.ops"}
`)
	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || len(findings) != 1 {
		t.Fatalf("candidates = %#v, findings = %#v, want two map candidates and one malformed ID", candidates, findings)
	}
	if findings[0].Value != "legacy-id" || findings[0].Reason != "entity_id_invalid:arity" {
		t.Fatalf("finding = %#v, want malformed string-keyed entity_id", findings[0])
	}
}

func TestAuditRejectsAnnotationThatDoesNotMatchExactOccurrence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "negative.go", `package fixture
// entity-id-audit:classify intentional-malformed "bad.id" line=3 column=24 surface=go-field:EntityState.ID entity_id_invalid:arity wrong column
var _ = EntityState{ID: "bad.id"}
`)
	if _, _, err := Audit(root); err == nil {
		t.Fatal("Audit() error = nil, want nonmatching occurrence annotation rejection")
	}
}

func TestAuditReportsInvalidUnclassifiedCandidatesInStableOrder(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "z.go", `package fixture
var _ = EntityState{ID: "bad"}
`)
	writeFixture(t, root, "a.json", `{"entity_id_prefix":"bad.*"}`)

	firstCandidates, firstFindings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	secondCandidates, secondFindings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstCandidates, secondCandidates) || !reflect.DeepEqual(firstFindings, secondFindings) {
		t.Fatal("Audit() output is not deterministic")
	}
	if len(firstFindings) != 2 {
		t.Fatalf("findings = %#v, want two", firstFindings)
	}
	if filepath.Base(firstFindings[0].File) != "a.json" || filepath.Base(firstFindings[1].File) != "z.go" {
		t.Fatalf("findings = %#v, want file-sorted order", firstFindings)
	}
}

func TestAuditPreservesRepeatedConfigOccurrencesAndExactLocations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "repeated.json", `{
  "items": [
    {"entity_id":"acme.ops.robotics.gcs.drone.001"},
    {"entity_id":"acme.ops.robotics.gcs.drone.001"}
  ]
}`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 || len(candidates) != 2 {
		t.Fatalf("candidates = %#v, findings = %#v, want two distinct occurrences", candidates, findings)
	}
	if candidates[0].Line != 3 || candidates[1].Line != 4 || candidates[0].Column == 0 || candidates[1].Column == 0 {
		t.Fatalf("candidates = %#v, want exact lines 3/4 and nonzero columns", candidates)
	}
}

func TestAuditDoesNotDeduplicateRepeatedValuesOnOneLine(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "same-line.json", `{"items":[{"entity_id":"acme.ops.robotics.gcs.drone.001"},{"entity_id":"acme.ops.robotics.gcs.drone.001"}]}`)
	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 || len(candidates) != 2 {
		t.Fatalf("candidates = %#v, findings = %#v, want two same-line occurrences", candidates, findings)
	}
	if candidates[0].Line != 1 || candidates[1].Line != 1 || candidates[0].Column == candidates[1].Column {
		t.Fatalf("candidates = %#v, want distinct exact columns on line 1", candidates)
	}
}

func TestAuditPreservesRepeatedYAMLAndJSONLOccurrences(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		name    string
		content string
	}{
		{"repeated.yaml", "items:\n  - entity_id: acme.ops.robotics.gcs.drone.001\n  - entity_id: acme.ops.robotics.gcs.drone.001\n"},
		{"repeated.jsonl", "{\"entity_id\":\"acme.ops.robotics.gcs.drone.001\"}\n{\"entity_id\":\"acme.ops.robotics.gcs.drone.001\"}\n"},
	} {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFixture(t, root, fixture.name, fixture.content)
			candidates, findings, err := Audit(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != 0 || len(candidates) != 2 || candidates[0].Line == candidates[1].Line {
				t.Fatalf("candidates = %#v, findings = %#v, want two exact line occurrences", candidates, findings)
			}
		})
	}
}

func TestAuditRejectsMalformedJSONWithoutTextFallback(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "malformed.json5", `{entity_id: "bad"}`)
	if _, _, err := Audit(root); err == nil {
		t.Fatal("Audit() error = nil, want loud malformed JSON5 rejection")
	}
}

func TestRegexLikeSemanticPatternIsAudited(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "semantic.go", `package fixture
const entityIDPattern = "a.b.[bad].d.e.f"
`)
	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || len(findings) != 1 {
		t.Fatalf("candidates = %#v, findings = %#v, want regex-like semantic pattern reported", candidates, findings)
	}
	if findings[0].Value != "a.b.[bad].d.e.f" || !strings.Contains(findings[0].Reason, "first_byte") {
		t.Fatalf("finding = %#v, want malformed declaration-pattern first-byte finding", findings[0])
	}
}

func TestEntityIDSchemaRegexExclusionIsPathAndNameBounded(t *testing.T) {
	t.Parallel()
	authorityPath := filepath.Join(t.TempDir(), "pkg", "types", "entity_id.go")
	for _, path := range []string{authorityPath, filepath.Join("pkg", "types", "entity_id.go")} {
		if !isEntityIDSchemaRegexDeclaration(path, "EntityIDDeclarationPattern") {
			t.Errorf("canonical schema regex declaration at %q was not excluded", path)
		}
	}
	if isEntityIDSchemaRegexDeclaration(authorityPath, "entityIDPattern") {
		t.Fatal("ordinary semantic entityIDPattern declaration was excluded")
	}
	if isEntityIDSchemaRegexDeclaration(filepath.Join(t.TempDir(), "schema.go"), "EntityIDDeclarationPattern") {
		t.Fatal("same-named declaration outside canonical authority was excluded")
	}
}

func TestAuditIncludesTestsAndFixturesButExcludesArchivedOpenSpec(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "local_test.go", `package fixture
var _ = EntityState{ID: "acme.ops.robotics.gcs.drone.test"}
`)
	writeFixture(t, root, "testdata/seed.json", `{"entity_id":"acme.ops.robotics.gcs.drone.seed"}`)
	writeFixture(t, root, "openspec/changes/archive/old/seed.json", `{"entity_id":"bad"}`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want archived OpenSpec excluded", findings)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates = %#v, want Go test and testdata seed", candidates)
	}
}

func TestAuditRecognizesSemanticTestEntityIDCalls(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "fixture_test.go", `package fixture
import "github.com/c360studio/semstreams/internal/semantictest"
var rawEntityID = "bad"
func testFixture(t interface{}) string {
  return semantictest.EntityID(t, "acme", "ops", "robotics", "gcs", "drone", "001")
}
`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Value != "bad" {
		t.Fatalf("findings = %#v, want adjacent raw fixture preserved", findings)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates = %#v, want helper-built identity plus raw fixture", candidates)
	}
	foundHelper := false
	for _, candidate := range candidates {
		if candidate.Surface != "go-call:semantictest.EntityID" {
			continue
		}
		foundHelper = true
		if got, want := candidate.Value, "acme.ops.robotics.gcs.drone.001"; got != want {
			t.Fatalf("helper candidate = %q, want %q", got, want)
		}
	}
	if !foundHelper {
		t.Fatalf("candidates = %#v, want exact semantic helper surface", candidates)
	}
}

func TestSemanticTestImportPolicyRejectsAmbiguousResolution(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "explicit alias",
			content: `package fixture
import st "github.com/c360studio/semstreams/internal/semantictest"
var _ = st.EntityID
`,
			want: "canonical unaliased import name",
		},
		{
			name: "dot import",
			content: `package fixture
import . "github.com/c360studio/semstreams/internal/semantictest"
var _ = EntityID
`,
			want: "canonical unaliased import name",
		},
		{
			name: "local shadow",
			content: `package fixture
import "github.com/c360studio/semstreams/internal/semantictest"
func fixture() {
  semantictest := struct{ EntityID string }{}
  _ = semantictest.EntityID
}
`,
			want: "shadows the canonical",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFixture(t, root, "fixture_test.go", test.content)
			if _, _, err := Audit(root); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Audit() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestSemanticTestPackageRejectsDirectEntityIDShadowing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "parameter",
			content: `package semantictest
func fixture(EntityID func(...string) string) {
  _ = EntityID("acme", "ops", "robotics", "gcs", "drone", "001")
}
`,
		},
		{
			name: "local closure",
			content: `package semantictest
func fixture() {
  EntityID := func(...string) string { return "not-authoritative" }
  _ = EntityID("acme", "ops", "robotics", "gcs", "drone", "001")
}
`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFixture(t, root, "internal/semantictest/fixture_test.go", test.content)
			if _, _, err := Audit(root); err == nil || !strings.Contains(err.Error(), "shadows the package semantictest.EntityID") {
				t.Fatalf("Audit() error = %v, want direct-helper shadow rejection", err)
			}
		})
	}
}

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
