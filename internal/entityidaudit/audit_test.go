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
	writeFixture(t, root, "entities_test.go", `package fixture
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
	writeFixture(t, root, "constructor_test.go", `package fixture
var _ = EntityID{Org: "acme", Platform: "ops", System: "robotics", Domain: "gcs", Type: "drone", Instance: "001"}
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

func findingReasons(findings []Finding) map[string][]string {
	out := map[string][]string{}
	for _, finding := range findings {
		out[finding.Reason] = append(out[finding.Reason], finding.Surface+"="+finding.Value)
	}
	return out
}

// TestAuditFlagsAuthorityLiteral pins the segment rule `authority_literal`: a
// production builder whose platform position is a literal product name is a
// finding (ADR-102 d3: product names are provenance, not identity), seen
// through the new go-format-prefix surface.
func TestAuditFlagsAuthorityLiteral(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "builder.go", `package fixture
import "fmt"
type payload struct{ Repo, SHA string }
func (p payload) EntityID() string { return fmt.Sprintf("acme.semsource.%s.git.commit.%s", p.Repo, p.SHA) }
var _ = EntityDomainDelegation{Producer: "semsource", Domain: "git"}
`)
	_, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	reasons := findingReasons(findings)
	if got := reasons["authority_literal"]; len(got) != 1 || !strings.HasPrefix(got[0], "go-format-prefix") || !strings.Contains(got[0], "acme.semsource.%s.git.commit.%s") {
		t.Fatalf("findings = %#v, want one authority_literal on the go-format-prefix surface", findings)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %#v, want exactly the authority finding (git is delegated)", findings)
	}
}

// TestAuditFlagsFormatPrefixAuthorityLiteral pins both new surfaces: a
// trailing-dot dotted constant and a `semstreams.framework.%s…` format string
// both report authority_literal.
func TestAuditFlagsFormatPrefixAuthorityLiteral(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "prefix.go", `package fixture
import "fmt"
const alertEntityPrefix = "semstreams.framework.graph.rules.alert."
func build(a, b, c, d string) string { return fmt.Sprintf("semstreams.framework.%s.%s.%s.%s", a, b, c, d) }
`)
	_, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	reasons := findingReasons(findings)
	got := reasons["authority_literal"]
	if len(got) != 2 {
		t.Fatalf("findings = %#v, want two authority_literal findings", findings)
	}
	surfaces := strings.Join(got, "\n")
	if !strings.Contains(surfaces, "go-dotted-constant") || !strings.Contains(surfaces, "go-format-prefix") {
		t.Fatalf("findings = %#v, want one on go-dotted-constant and one on go-format-prefix", findings)
	}
}

// TestAuditFlagsUnregisteredDomain pins the segment rule `domain_unregistered`:
// a literal position-4 value in production Go outside the framework-reserved
// set and not a registered EntityDomainDelegation is a finding; reserved and
// delegated domains are not.
func TestAuditFlagsUnregisteredDomain(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "domains.go", `package fixture
import "fmt"
const entityIDPattern = "*.*.src.media.*.*"
const chainEntityIDPattern = "*.*.chain.agent.execution.*"
const gitEntityIDPattern = "*.*.src.git.*.*"
const webEntityIDPattern = "*.*.src.web.*.*"
var _ = EntityDomainDelegation{Producer: "semsource", Domain: "git"}
var _ = []EntityDomainDelegation{{Producer: "semsource", Domain: "web"}}
type payload struct{ ID string }
func (p payload) EntityID() string { return fmt.Sprintf("%s.%s.world.game.quest.%s", "a", "b", p.ID) }
`)
	_, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	reasons := findingReasons(findings)
	got := reasons["domain_unregistered"]
	if len(got) != 2 {
		t.Fatalf("findings = %#v, want media (pattern) and game (format builder) as the only unregistered domains", findings)
	}
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "*.*.src.media.*.*") || !strings.Contains(joined, "world.game.quest") {
		t.Fatalf("findings = %#v, want the media pattern and the game builder", findings)
	}
	if len(reasons["authority_literal"]) != 0 {
		t.Fatalf("findings = %#v, wildcard and template authorities are not literals", findings)
	}
}

// TestAuditFlagsReservedInstanceToken pins that a production instance value
// equal to a container padding token is a finding (the tokens are
// contract-reserved until gh606 retires containers).
func TestAuditFlagsReservedInstanceToken(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "reserved.go", `package fixture
var _ = EntityState{ID: "acme.dep1.src.agent.commit.group"}
var _ = EntityState{ID: "acme.dep1.src.agent.commit.a1"}
`)
	_, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	reasons := findingReasons(findings)
	if got := reasons["instance_reserved"]; len(got) != 1 || !strings.Contains(got[0], ".group") {
		t.Fatalf("findings = %#v, want one instance_reserved finding for the group token", findings)
	}
}

// TestAuditSegmentRulesSkipTestFilesAndSeeConfigPatterns pins the corpus the
// segment rules cover: `_test.go` fixtures and testdata are lexical-only,
// while a rule-pack `entity.pattern` or an ENTITY_STATES watch pattern in a
// config is a declaration pattern subject to authority_literal.
func TestAuditSegmentRulesSkipTestFilesAndSeeConfigPatterns(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "fixture_test.go", `package fixture
const entityIDPattern = "acme.ops.src.media.*.*"
`)
	writeFixture(t, root, "configs/rules.json", `{
  "entity_watch_buckets": {"ENTITY_STATES": ["c360.*.*.*.*.*"], "AGENT_LOOPS": ["COMPLETE_*"]},
  "rules": [{"entity": {"pattern": "*.*.src.agent.*.*"}}, {"entity": {"pattern": "c360.test.gcs.lifecycle.mission.*"}}]
}`)
	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	reasons := findingReasons(findings)
	got := reasons["authority_literal"]
	if len(got) != 2 {
		t.Fatalf("findings = %#v, want the two config patterns with a literal org", findings)
	}
	if len(reasons["domain_unregistered"]) != 0 {
		t.Fatalf("findings = %#v, the test fixture and config patterns are outside the domain rule", findings)
	}
	patterns := 0
	for _, candidate := range candidates {
		if candidate.Language == LanguageDeclarationPattern && strings.HasPrefix(candidate.Surface, "config:") {
			patterns++
		}
	}
	if patterns != 3 {
		t.Fatalf("candidates = %#v, want three config declaration patterns (ENTITY_STATES watch + two rule entity patterns), never the AGENT_LOOPS key glob", candidates)
	}
}

// TestAuditSeesProjectionContractEntityPatterns pins the projection-contract
// declaration surface named as an ADR-102 enforcement point: the EntityPattern
// field of a contract.Contract literal in production Go, and
// projection_contracts[].entity_pattern in a config, are declaration patterns
// the segment rules see. The query-classifier Options["entity_pattern"] key is
// a different vocabulary — an entity *type* token, graph/query/classifier.go —
// and stays out of the corpus.
func TestAuditSeesProjectionContractEntityPatterns(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "contracts.go", `package fixture
import "github.com/c360studio/semstreams/pkg/projection/contract"

var _ = contract.Contract{Name: "canonical", EntityPattern: "*.*.agentic-loop.agent.execution.*"}
var _ = contract.Contract{Name: "retired-order", EntityPattern: "*.*.agent.agentic-loop.execution.*"}
var _ = map[string]any{"entity_pattern": "shipment"}
`)
	writeFixture(t, root, "configs/rulepack.json", `{
  "projection_contracts": [{"name":"lesson","entity_pattern":"acme.ops.lesson.agent.record.*"}],
  "examples": [{"intent":"entity_lookup","options":{"entity_pattern":"sensor"}}]
}`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.Language != LanguageDeclarationPattern {
			t.Fatalf("candidate = %#v, want every contract pattern classified as a declaration pattern", candidate)
		}
		values = append(values, candidate.Surface+"="+candidate.Value)
	}
	want := []string{
		"config:projection_contracts.entity_pattern=acme.ops.lesson.agent.record.*",
		"go-field:Contract.EntityPattern=*.*.agentic-loop.agent.execution.*",
		"go-field:Contract.EntityPattern=*.*.agent.agentic-loop.execution.*",
	}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("candidates = %#v, want %#v — the classifier option key is a different vocabulary", values, want)
	}
	reasons := findingReasons(findings)
	if got := reasons["domain_unregistered"]; len(got) != 1 || !strings.Contains(got[0], "*.*.agent.agentic-loop.execution.*") {
		t.Fatalf("findings = %#v, want the retired-order contract pattern reported as an unregistered domain", findings)
	}
	if got := reasons["authority_literal"]; len(got) != 1 || !strings.Contains(got[0], "acme.ops.lesson.agent.record.*") {
		t.Fatalf("findings = %#v, want the literal-authority config contract pattern reported", findings)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %#v, want exactly the retired-order domain and the literal authority", findings)
	}
}
