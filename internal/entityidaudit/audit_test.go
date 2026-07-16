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
	files, err := walkSourceFiles([]string{"testdata/corpus"})
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := auditSurfaces(files)
	if err != nil {
		t.Fatal(err)
	}
	report := BuildReport([]string{"testdata/corpus"}, "fixture", Result{Candidates: candidates, Findings: findings, Surfaces: surfaces})
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
// entity-id-audit:classify intentional-malformed "bad.id" line=3 column=25 surface=go-field:EntityState.ID parser rejection fixture one
var _ = EntityState{ID: "bad.id"}
// entity-id-audit:classify intentional-malformed "bad.id" line=5 column=25 surface=go-field:EntityState.ID parser rejection fixture two
var _ = EntityState{ID: "bad.id"}
// entity-id-audit:classify unrelated-glob "raw.sensor.>" line=7 column=23 surface=go-unrelated-glob NATS subscription filter
var _ = Port{Subject: "raw.sensor.>"}
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
		languages[candidate.Language] = true
	}
	if !languages[LanguageIntentionalMalformed] || !languages[LanguageUnrelatedGlob] {
		t.Fatalf("languages = %#v, want both explicit classifications", languages)
	}
	for _, candidate := range candidates {
		if candidate.Value == "bad.id" && candidate.Language != LanguageIntentionalMalformed {
			t.Fatalf("candidate = %#v, want every exact occurrence classified", candidate)
		}
	}

	writeFixture(t, root, "bad_annotation.go", "package fixture\n// entity-id-audit:classify intentional-malformed \"bad.id\" line=3 column=25 surface=go-field:EntityState.ID "+string(make([]byte, maxAnnotationReasonBytes+1))+"\nvar _ = EntityState{ID: \"bad.id\"}\n")
	if _, _, err := Audit(root); err == nil {
		t.Fatal("Audit() error = nil, want oversized annotation rejection")
	}
}

func TestAuditRejectsAnnotationThatDoesNotMatchExactOccurrence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "negative.go", `package fixture
// entity-id-audit:classify intentional-malformed "bad.id" line=3 column=24 surface=go-field:EntityState.ID wrong column
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

func TestSchemaRegexConstantIsSurfaceNotDeclarationPattern(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "schema.go", `package fixture
const entityIDPattern = "^[A-Za-z0-9_-]+$"
`)
	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 || len(findings) != 0 {
		t.Fatalf("candidates = %#v, findings = %#v, want regex excluded from value corpus", candidates, findings)
	}
	files, err := walkSourceFiles([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := auditSurfaces(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(surfaces) != 1 || surfaces[0].Kind != "schema-regex" {
		t.Fatalf("surfaces = %#v, want schema-regex inventory", surfaces)
	}
}

func TestSurfaceInventoryCoversAPISchemaKVAndDirectImplementations(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFixture(t, root, "surfaces.go", `package fixture
import "strings"
func ValidateEntityID(value string) error { return nil }
func buildEntityKey(entityID string) string { return "prefix." + entityID }
func use(entityID, entityPattern string) {
  _ = ValidateEntityID(entityID)
  _ = strings.Split(entityID, ".")
  _ = matchPattern(entityPattern, entityID)
  _, _ = kv.Get(entityID)
}
`)
	writeFixture(t, root, "schemas/entity.json", `{"properties":{"entity_id":{"type":"string"},"entity_id_prefix":{"type":"string"}}}`)
	files, err := walkSourceFiles([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	surfaces, err := auditSurfaces(files)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, surface := range surfaces {
		kinds[surface.Kind] = true
		if !strings.HasPrefix(surface.Classification, "unreviewed:") {
			t.Fatalf("surface = %#v, want unreviewed classification without a checked exact disposition", surface)
		}
		if len(surface.Locations) == 0 || surface.Locations[0].Line == 0 || surface.Locations[0].Column == 0 {
			t.Fatalf("surface = %#v, want at least one exact location", surface)
		}
	}
	for _, want := range []string{"parser-validator-api-declaration", "parser-validator-api-call", "string-builder-candidate", "kv-call", "direct-split", "match-family-call", "schema-contract-field"} {
		if !kinds[want] {
			t.Fatalf("surfaces = %#v, missing kind %s", surfaces, want)
		}
	}
}

func TestUnreviewedSurfaceFailsReportGeneration(t *testing.T) {
	t.Parallel()
	surfaces := groupSurfaces([]surfaceOccurrence{{file: "example.go", kind: "kv-call", name: "Get in load", line: 4, column: 2}})
	if len(surfaces) != 1 || !strings.HasPrefix(surfaces[0].Classification, "unreviewed:") {
		t.Fatalf("surfaces = %#v, want one unreviewed group", surfaces)
	}
	report := BuildReport([]string{"."}, "fixture", Result{Surfaces: surfaces})
	if _, err := MarshalReport(report); err == nil || !strings.Contains(err.Error(), "unreviewed") {
		t.Fatalf("MarshalReport() error = %v, want unreviewed rejection", err)
	}
	candidates, err := MarshalSurfaceDispositionCandidates(surfaces)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(candidates), `"classification":"unreviewed"`) || !strings.Contains(string(candidates), `"basis":"REVIEW REQUIRED"`) {
		t.Fatalf("disposition candidate = %s, want explicit review-required entry", candidates)
	}
}

func TestDispositionCoverageFailsClosedForOmissionAndStaleEntry(t *testing.T) {
	t.Parallel()
	surface := AuditedSurface{
		File: "example.go", Kind: "kv-call", Name: "Get in load",
		Classification: "unreviewed:missing-disposition", Locations: []Location{{Line: 4, Column: 2}},
	}
	if err := validateSurfaceDispositionCoverage([]AuditedSurface{surface}, map[string]SurfaceDisposition{}); err == nil || !strings.Contains(err.Error(), "unreviewed") {
		t.Fatalf("missing disposition error = %v, want unreviewed rejection", err)
	}

	surface.Classification = "unrelated:reviewed:not-entity-id-contract"
	stale := map[string]SurfaceDisposition{
		"removed.go|kv-call|Get in removed": {
			File: "removed.go", Kind: "kv-call", Name: "Get in removed",
			Classification: "unrelated", Basis: "reviewed:not-entity-id-contract",
		},
	}
	if err := validateSurfaceDispositionCoverage([]AuditedSurface{surface}, stale); err == nil || !strings.Contains(err.Error(), "no inventoried surface") {
		t.Fatalf("stale disposition error = %v, want exact-coverage rejection", err)
	}
}

func TestCheckedDispositionManifestPinsRepresentativeCoreSurfaces(t *testing.T) {
	t.Parallel()
	required := []string{
		"graph/id_prefix_test.go|match-family-call|MatchesAnyIDPrefix in TestMatchesAnyIDPrefix",
		"graph/query/client.go|kv-call|Get in GetEntity",
		"pkg/fusion/retrieval.go|go-contract-field|ResolveQuery.Scope",
		"pkg/lifecycle/manager.go|kv-call|Get in getEntity",
		"pkg/lifecycle/manager_query.go|direct-split|strings.Split in matchPattern",
		"pkg/types/entity_id.go|direct-split|strings.Split in ParseEntityID",
		"processor/graph-embedding/query.go|go-contract-field|SearchRequest.Scope",
		"processor/graph-index/context_index.go|string-builder-candidate|contextIndexKey",
		"processor/graph-index/incoming_index.go|string-builder-candidate|incomingIndexKey",
		"processor/graph-index/name_index.go|string-builder-candidate|nameCompositeKey",
		"processor/graph-index/predicate_index.go|string-builder-candidate|predicateIndexKey",
		"processor/graph-index/component.go|kv-call|Put in UpdatePredicateIndex",
		"processor/graph-ingest/component.go|kv-call|Put in createEntity",
		"processor/graph-query/summary.go|direct-split|strings.Split in aggregateEntityTypes",
	}
	for _, key := range required {
		disposition, ok := checkedSurfaceDispositions[key]
		if !ok {
			t.Errorf("missing required checked surface disposition %s", key)
			continue
		}
		if disposition.Classification != "relevant" {
			t.Errorf("disposition %s = %#v, want relevant", key, disposition)
		}
	}
}

func TestEntityIDNamedStringBuildersHaveConstructorOrFixtureDispositions(t *testing.T) {
	t.Parallel()
	matched := 0
	for key, disposition := range checkedSurfaceDispositions {
		if disposition.Kind != "string-builder-candidate" || !strings.Contains(strings.ToLower(disposition.Name), "entityid") {
			continue
		}
		matched++
		switch disposition.Classification {
		case "relevant":
			if disposition.Basis != "reviewed:entity-id-constructor" && disposition.Basis != "reviewed:graphable-entity-id-constructor" {
				t.Errorf("relevant EntityID builder %s has non-constructor basis %q", key, disposition.Basis)
			}
		case "unrelated":
			if disposition.Basis != "reviewed:auditor-implementation-helper" && !strings.HasPrefix(disposition.Basis, "reviewed:test-fixture-") {
				t.Errorf("unrelated EntityID builder %s lacks explicit fixture/auditor basis: %q", key, disposition.Basis)
			}
		default:
			t.Errorf("EntityID builder %s has invalid classification %q", key, disposition.Classification)
		}
	}
	if matched == 0 {
		t.Fatal("checked disposition manifest has no EntityID-named string builders")
	}

	requiredConstructors := []string{
		"agentic/entity_ids.go|string-builder-candidate|ModelEndpointEntityID",
		"agentic/entity_ids.go|string-builder-candidate|TryLoopExecutionEntityID",
		"agentic/ops_diagnosis_entity.go|string-builder-candidate|OpsDiagnosisEntityID",
		"agentic/web_observation_entity.go|string-builder-candidate|TryWebObservationEntityID",
		"cmd/e2e-semstreams/mission/command.go|string-builder-candidate|EntityIDFor",
		"examples/processors/document/payload_document.go|string-builder-candidate|EntityID",
		"examples/processors/iot_sensor/payload.go|string-builder-candidate|ZoneEntityID",
		"examples/processors/weather_station/payload.go|string-builder-candidate|EntityID",
		"processor/agentic-loop/handlers.go|string-builder-candidate|resolveRunEntityID",
	}
	for _, key := range requiredConstructors {
		if disposition := checkedSurfaceDispositions[key]; disposition.Classification != "relevant" {
			t.Errorf("constructor disposition %s = %#v, want relevant", key, disposition)
		}
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
