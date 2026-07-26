package predicateaudit

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAuditExtractsStructuredProductionCandidates(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "producer.go", `package fixture
const PredicateGood = "robotics.state.armed"
var _ = Triple{Predicate: "legacy.bad_name"}
`)
	writeAuditFixture(t, root, "rules.json", `{"conditions":[{"field":"workflow.state.phase"}],"value":"$entity.triple.gather.child.completed.triples"}`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 4 {
		t.Fatalf("candidate count = %d, want 4: %#v", len(candidates), candidates)
	}
	if len(findings) != 1 || findings[0].Predicate != "legacy.bad_name" {
		t.Fatalf("findings = %#v, want legacy.bad_name", findings)
	}
}

func TestAuditRootSpellingsProduceIdenticalRepositoryRelativeEvidence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "handler.go", `package fixture
// predicate-audit:classify unrelated "robotics.state.armed" line=4 column=14 surface=go-assignment:predicate reviewed event label
func handle() {
predicate := "robotics.state.armed"
_ = predicate
}
`)
	writeAuditFixture(t, root, "vocabulary/unrelated.go", `package vocabulary
const EventLabel = "robotics.state.armed"
`)
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeRoot, err := filepath.Rel(workingDirectory, root)
	if err != nil {
		t.Fatal(err)
	}

	absoluteCandidates, absoluteFindings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	relativeCandidates, relativeFindings, err := Audit(relativeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(absoluteCandidates, relativeCandidates) ||
		!reflect.DeepEqual(absoluteFindings, relativeFindings) {
		t.Fatalf(
			"absolute = %#v/%#v, relative = %#v/%#v",
			absoluteCandidates,
			absoluteFindings,
			relativeCandidates,
			relativeFindings,
		)
	}
	if len(absoluteCandidates) != 1 || absoluteCandidates[0].File != "handler.go" {
		t.Fatalf("candidates = %#v, want one repository-relative classified occurrence", absoluteCandidates)
	}

	absoluteReport, err := MarshalReport(BuildReport([]string{root}, absoluteCandidates, absoluteFindings))
	if err != nil {
		t.Fatal(err)
	}
	relativeReport, err := MarshalReport(BuildReport([]string{relativeRoot}, relativeCandidates, relativeFindings))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(absoluteReport, relativeReport) {
		t.Fatalf("absolute report:\n%s\nrelative report:\n%s", absoluteReport, relativeReport)
	}
}

func TestAuditFileRootSpellingsProduceRepositoryRelativeFilename(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "rule.go", `package fixture
var _ = Triple{Predicate: "robotics.state.armed"}
`)
	absoluteFile := filepath.Join(root, "rule.go")
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relativeFile, err := filepath.Rel(workingDirectory, absoluteFile)
	if err != nil {
		t.Fatal(err)
	}

	absoluteCandidates, absoluteFindings, err := Audit(absoluteFile)
	if err != nil {
		t.Fatal(err)
	}
	relativeCandidates, relativeFindings, err := Audit(relativeFile)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(absoluteCandidates, relativeCandidates) ||
		!reflect.DeepEqual(absoluteFindings, relativeFindings) {
		t.Fatalf(
			"absolute = %#v/%#v, relative = %#v/%#v",
			absoluteCandidates,
			absoluteFindings,
			relativeCandidates,
			relativeFindings,
		)
	}
	if len(absoluteCandidates) != 1 || absoluteCandidates[0].File != "rule.go" {
		t.Fatalf("candidates = %#v, want repository-relative file root", absoluteCandidates)
	}

	absoluteReport, err := MarshalReport(BuildReport([]string{absoluteFile}, absoluteCandidates, absoluteFindings))
	if err != nil {
		t.Fatal(err)
	}
	relativeReport, err := MarshalReport(BuildReport([]string{relativeFile}, relativeCandidates, relativeFindings))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(absoluteReport, relativeReport) {
		t.Fatalf("absolute report:\n%s\nrelative report:\n%s", absoluteReport, relativeReport)
	}
}

func TestAuditUsesGitWorktreeRootForNestedDirectoryAndFileRoots(t *testing.T) {
	t.Parallel()
	repository := t.TempDir()
	writeAuditFixture(t, repository, ".git", "gitdir: /tmp/example-worktree-metadata\n")
	writeAuditFixture(t, repository, "processor/gated-dag/config.go", `package gateddag
var _ = Triple{Predicate: "robotics.state.armed"}
`)
	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	targets := []struct {
		name       string
		absolute   string
		reportRoot string
	}{
		{name: "repository", absolute: repository, reportRoot: "."},
		{
			name: "subdirectory", absolute: filepath.Join(repository, "processor", "gated-dag"),
			reportRoot: "processor/gated-dag",
		},
		{
			name: "file", absolute: filepath.Join(repository, "processor", "gated-dag", "config.go"),
			reportRoot: "processor/gated-dag/config.go",
		},
	}
	for _, target := range targets {
		target := target
		t.Run(target.name, func(t *testing.T) {
			t.Parallel()
			relative, err := filepath.Rel(workingDirectory, target.absolute)
			if err != nil {
				t.Fatal(err)
			}
			absoluteCandidates, absoluteFindings, err := Audit(target.absolute)
			if err != nil {
				t.Fatal(err)
			}
			relativeCandidates, relativeFindings, err := Audit(relative)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(absoluteCandidates, relativeCandidates) ||
				!reflect.DeepEqual(absoluteFindings, relativeFindings) {
				t.Fatalf(
					"absolute = %#v/%#v, relative = %#v/%#v",
					absoluteCandidates,
					absoluteFindings,
					relativeCandidates,
					relativeFindings,
				)
			}
			if len(absoluteCandidates) != 1 ||
				absoluteCandidates[0].File != "processor/gated-dag/config.go" {
				t.Fatalf("candidates = %#v, want Git-root-relative path", absoluteCandidates)
			}
			report := BuildReport([]string{target.absolute}, absoluteCandidates, absoluteFindings)
			if !reflect.DeepEqual(report.Roots, []string{target.reportRoot}) {
				t.Fatalf("report roots = %#v, want %q", report.Roots, target.reportRoot)
			}
		})
	}
}

func TestAuditRejectsLegacyBroadAllowanceAndSkipsGoTests(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "negative.go", `package fixture
// predicate-audit:allow-invalid legacy.bad_name parser rejection fixture
var _ = Triple{Predicate: "legacy.bad_name"}
`)
	writeAuditFixture(t, root, "legacy_test.go", `package fixture
var _ = Triple{Predicate: "also.bad_name"}
`)

	_, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %#v, want legacy allowance plus unsuppressed invalid predicate", findings)
	}
	if findings[0].Code != FindingLegacyBroadAllowance || findings[1].Code != FindingInvalidPredicate {
		t.Fatalf("finding codes = %q, %q, want stable legacy/invalid codes", findings[0].Code, findings[1].Code)
	}
}

func TestAuditRejectsLegacyBroadAllowancesInNonGoCommentsOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "rules.yaml", `note: "predicate-audit:allow-invalid is documentation data"
# predicate-audit:allow-invalid legacy.bad_name removed YAML allowance
predicate: robotics.state.armed
`)
	writeAuditFixture(t, root, "rules.ts", `const note = "predicate-audit:allow-invalid is documentation data";
// predicate-audit:allow-invalid legacy.bad_name removed TypeScript allowance
const rule = { predicate: "robotics.state.ready" };
`)

	_, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %#v, want exactly the YAML and TypeScript comments", findings)
	}
	for _, finding := range findings {
		if finding.Code != FindingLegacyBroadAllowance {
			t.Fatalf("finding = %#v, want legacy-broad-allowance", finding)
		}
	}
}

func TestAuditRejectsLegacyBroadAllowancesInJSON5AndSvelteMarkupComments(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "rules.json5", `{
  note: "predicate-audit:allow-invalid is documentation data",
  // predicate-audit:allow-invalid legacy.bad_name removed JSON5 allowance
  predicate: "robotics.state.armed",
}
`)
	writeAuditFixture(t, root, "Rule.svelte", `<script>
const note = "predicate-audit:allow-invalid is documentation data";
</script>
<!-- predicate-audit:allow-invalid legacy.bad_name removed Svelte allowance -->
<p>ready</p>
`)

	_, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %#v, want exactly the JSON5 and Svelte comments", findings)
	}
	for _, finding := range findings {
		if finding.Code != FindingLegacyBroadAllowance {
			t.Fatalf("finding = %#v, want legacy-broad-allowance", finding)
		}
	}
}

func TestAuditSlashLanguagesIgnoreLegacyMarkersInContinuedStrings(t *testing.T) {
	t.Parallel()
	const marker = "predicate-audit:allow-invalid legacy.bad_name"
	for _, extension := range []string{".js", ".ts", ".json5", ".svelte"} {
		extension := extension
		t.Run(extension, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := "const note = \"documentation data " + "\\" + "\n" +
				"// " + marker + " continued string data\";\n" +
				"// " + marker + " removed real allowance\n"
			if extension == ".svelte" {
				source = "<script>\n" + source + "</script>\n"
			}
			writeAuditFixture(t, root, "rules"+extension, source)

			_, findings, err := Audit(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != 1 ||
				findings[0].Code != FindingLegacyBroadAllowance {
				t.Fatalf("findings = %#v, want only the real comment", findings)
			}
		})
	}
}

func TestSlashCommentScannerHonorsTrailingBackslashParity(t *testing.T) {
	t.Parallel()
	const marker = "predicate-audit:allow-invalid legacy.bad_name"
	odd := "const note = \"data " + strings.Repeat("\\", 3) + "\n" +
		"// " + marker + " continued string data\";\n"
	if comments := slashProductionComments([]byte(odd)); len(comments) != 0 {
		t.Fatalf("odd-backslash comments = %#v, want continued string data ignored", comments)
	}

	even := "const broken = \"data " + strings.Repeat("\\", 2) + "\n" +
		"// " + marker + " real comment after non-continuation\n"
	comments := slashProductionComments([]byte(even))
	if len(comments) != 1 || !strings.Contains(comments[0].text, marker) {
		t.Fatalf("even-backslash comments = %#v, want real comment", comments)
	}
}

func TestAuditIgnoresHashMarkersInsideMultilineStrings(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "rules.py", `note = """
# predicate-audit:allow-invalid is Python documentation data
"""
# predicate-audit:allow-invalid legacy.bad_name removed Python allowance
`)
	writeAuditFixture(t, root, "rules.toml", `note = '''
# predicate-audit:allow-invalid is TOML documentation data
'''
# predicate-audit:allow-invalid legacy.bad_name removed TOML allowance
`)

	_, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 {
		t.Fatalf("findings = %#v, want only real Python and TOML comments", findings)
	}
	for _, finding := range findings {
		if finding.Code != FindingLegacyBroadAllowance || finding.Line != 4 {
			t.Fatalf("finding = %#v, want line-four legacy comment", finding)
		}
	}
}

func TestAuditIgnoresYAMLLegacyMarkersInsideBlockScalars(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "rules.yaml", `literal: |-
  # predicate-audit:allow-invalid is literal scalar data
folded: >+
  # predicate-audit:allow-invalid is folded scalar data
explicit: |2-
  # predicate-audit:allow-invalid is explicit-indent scalar data
# predicate-audit:allow-invalid legacy.bad_name removed YAML allowance
predicate: robotics.state.armed
`)

	_, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 ||
		findings[0].Code != FindingLegacyBroadAllowance ||
		findings[0].Line != 7 {
		t.Fatalf("findings = %#v, want only the real YAML comment", findings)
	}
}

func TestAuditMalformedJSONIsAnOperationalParseError(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "rules.json", `{"predicate":`)

	if _, _, err := Audit(root); err == nil || !strings.Contains(err.Error(), "parse JSON") {
		t.Fatalf("Audit() error = %v, want strict JSON parse error", err)
	}
}

func TestAuditClassifiesOneExactHeuristicOccurrenceAsUnrelated(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "handler.go", `package fixture
// predicate-audit:classify unrelated "quest.failed" line=4 column=14 surface=go-assignment:predicate reviewed quest event name
func handle() {
predicate := "quest.failed"
predicate = "quest.failed"
}
`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || len(findings) != 1 {
		t.Fatalf("candidates = %#v, findings = %#v, want one classified and one independent finding", candidates, findings)
	}
	if candidates[0].Authority != AuthorityPredicateShaped ||
		candidates[0].Status != CandidateStatusClassifiedUnrelated ||
		candidates[0].ClassificationBasis != "reviewed quest event name" {
		t.Fatalf("classified candidate = %#v", candidates[0])
	}
	if findings[0].Code != FindingInvalidPredicate || findings[0].Line != 5 {
		t.Fatalf("findings = %#v, want same-value second occurrence independently invalid", findings)
	}
}

func TestAuditRejectsUnrelatedClassificationForStoredPredicate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "triple.go", `package fixture
// predicate-audit:classify unrelated "quest.failed" line=3 column=27 surface=go-field:Predicate reviewed quest event name
var _ = Triple{Predicate: "quest.failed"}
`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Authority != AuthorityStoredPredicate {
		t.Fatalf("candidates = %#v, want one authoritative stored predicate", candidates)
	}
	gotCodes := findingCodes(findings)
	wantCodes := []FindingCode{FindingClassificationIneligible, FindingInvalidPredicate}
	if !reflect.DeepEqual(gotCodes, wantCodes) {
		t.Fatalf("finding codes = %#v, want %#v", gotCodes, wantCodes)
	}
}

func TestAuditClassifiedValueCannotHideSameValueStoredPredicate(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "mixed.go", `package fixture
// predicate-audit:classify unrelated "quest.failed" line=4 column=14 surface=go-assignment:predicate reviewed quest event name
func handle() {
predicate := "quest.failed"
_ = predicate
}
var _ = Triple{Predicate: "quest.failed"}
`)

	candidates, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 2 || len(findings) != 1 {
		t.Fatalf("candidates = %#v, findings = %#v, want exact heuristic classification plus stored finding", candidates, findings)
	}
	if candidates[0].Status != CandidateStatusClassifiedUnrelated {
		t.Fatalf("first candidate = %#v, want classified heuristic", candidates[0])
	}
	if findings[0].Code != FindingInvalidPredicate ||
		findings[0].Authority != AuthorityStoredPredicate ||
		findings[0].Surface != "go-field:Predicate" {
		t.Fatalf("findings = %#v, same-value stored predicate must remain a finding", findings)
	}
}

func TestAuditRejectsWrongAndStaleClassificationLocators(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		line string
		code FindingCode
	}{
		{
			name: "wrong-line",
			line: `// predicate-audit:classify unrelated "quest.failed" line=5 column=14 surface=go-assignment:predicate reviewed event`,
			code: FindingClassificationWrongLine,
		},
		{
			name: "wrong-column",
			line: `// predicate-audit:classify unrelated "quest.failed" line=4 column=13 surface=go-assignment:predicate reviewed event`,
			code: FindingClassificationWrongColumn,
		},
		{
			name: "wrong-surface",
			line: `// predicate-audit:classify unrelated "quest.failed" line=4 column=14 surface=go-declaration:predicate reviewed event`,
			code: FindingClassificationWrongSurface,
		},
		{
			name: "wrong-value",
			line: `// predicate-audit:classify unrelated "quest.succeeded" line=4 column=14 surface=go-assignment:predicate reviewed event`,
			code: FindingClassificationWrongValue,
		},
		{
			name: "stale",
			line: `// predicate-audit:classify unrelated "other.failed" line=99 column=99 surface=go-assignment:predicate reviewed event`,
			code: FindingClassificationStale,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeAuditFixture(t, root, "handler.go", "package fixture\n"+test.line+"\nfunc handle() {\npredicate := \"quest.failed\"\n}\n")
			_, findings, err := Audit(root)
			if err != nil {
				t.Fatal(err)
			}
			if !containsFindingCode(findings, test.code) {
				t.Fatalf("findings = %#v, want %q", findings, test.code)
			}
			if !containsFindingCode(findings, FindingInvalidPredicate) {
				t.Fatalf("findings = %#v, malformed candidate must remain audited", findings)
			}
		})
	}
}

func TestAuditRejectsDuplicateAndAmbiguousClassifications(t *testing.T) {
	t.Parallel()
	t.Run("duplicate", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeAuditFixture(t, root, "handler.go", `package fixture
// predicate-audit:classify unrelated "quest.failed" line=5 column=14 surface=go-assignment:predicate reviewed event one
// predicate-audit:classify unrelated "quest.failed" line=5 column=14 surface=go-assignment:predicate reviewed event two
func handle() {
predicate := "quest.failed"
}
`)
		_, findings, err := Audit(root)
		if err != nil {
			t.Fatal(err)
		}
		if !containsFindingCode(findings, FindingClassificationDuplicate) {
			t.Fatalf("findings = %#v, want duplicate classification finding", findings)
		}
	})
	t.Run("ambiguous", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeAuditFixture(t, root, "state.go", `package fixture
// predicate-audit:classify unrelated "quest.failed" line=4 column=14 surface=go-lifecycle-tag reviewed repeated tag
type State struct {
Field string `+"`predicate=quest.failed predicate=quest.failed`"+`
}
`)
		_, findings, err := Audit(root)
		if err != nil {
			t.Fatal(err)
		}
		if !containsFindingCode(findings, FindingClassificationAmbiguous) {
			t.Fatalf("findings = %#v, want ambiguous classification finding", findings)
		}
	})
}

func TestAuditRejectsMalformedClassificationAndBoundsBasis(t *testing.T) {
	t.Parallel()
	for _, annotation := range []string{
		`// predicate-audit:classify unrelated quest.failed line=4 column=14 surface=go-assignment:predicate reviewed`,
		`// predicate-audit:classify unrelated "quest.failed" line=4 column=14 surface=go-assignment:predicate `,
		`// predicate-audit:classify unrelated "quest.failed" line=4 column=14 surface=go-assignment:predicate ` + strings.Repeat("x", maxClassificationBasisBytes+1),
	} {
		root := t.TempDir()
		writeAuditFixture(t, root, "handler.go", "package fixture\n"+annotation+"\nfunc handle() {\npredicate := \"quest.failed\"\n}\n")
		_, findings, err := Audit(root)
		if err != nil {
			t.Fatal(err)
		}
		if !containsFindingCode(findings, FindingClassificationMalformed) {
			t.Fatalf("findings = %#v, want malformed classification", findings)
		}
	}
}

func TestAuditMarksAuthoritativeAndHeuristicSurfaces(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "surfaces.go", `package fixture
const PredicateEvent = "quest.failed"
var _ = Triple{Predicate: "robotics.state.armed"}
func assign(triple *Triple) {
predicate := "quest.failed"
triple.Predicate = "quest.failed"
_ = predicate
}
`)

	candidates, _, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	authorities := make(map[string]Authority)
	for _, candidate := range candidates {
		authorities[candidate.Surface] = candidate.Authority
	}
	if authorities["go-field:Predicate"] != AuthorityStoredPredicate {
		t.Fatalf("authorities = %#v, Triple.Predicate must be authoritative", authorities)
	}
	if authorities["go-assignment:Predicate"] != AuthorityStoredPredicate {
		t.Fatalf("authorities = %#v, Triple.Predicate assignment must be authoritative", authorities)
	}
	if authorities["go-declaration:PredicateEvent"] != AuthorityPredicateShaped ||
		authorities["go-assignment:predicate"] != AuthorityPredicateShaped {
		t.Fatalf("authorities = %#v, name-derived declaration/assignment must be heuristic", authorities)
	}
}

func TestAuditResolvesPredicateIdentifiers(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeAuditFixture(t, root, "local.go", `package fixture
const bad = "legacy.local_bad"
var _ = Triple{Predicate: bad}
`)
	writeAuditFixture(t, root, "helper/predicates.go", `package helper
const Bad = "legacy.selector_bad"
`)
	writeAuditFixture(t, root, "selector.go", `package fixture
import "example/helper"
var _ = Triple{Predicate: helper.Bad}
`)
	writeAuditFixture(t, root, "agentic/predicates.go", `package agentic
const Bad = "legacy.alias_bad"
`)
	writeAuditFixture(t, root, "alias.go", `package fixture
import agvocab "example/agentic"
var _ = Triple{Predicate: agvocab.Bad}
`)

	_, findings, err := Audit(root)
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]bool)
	for _, finding := range findings {
		got[finding.Predicate] = true
	}
	for _, want := range []string{"legacy.local_bad", "legacy.selector_bad", "legacy.alias_bad"} {
		if !got[want] {
			t.Fatalf("findings = %#v, want resolved %s", findings, want)
		}
	}
}

func writeAuditFixture(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func findingCodes(findings []Finding) []FindingCode {
	codes := make([]FindingCode, 0, len(findings))
	for _, finding := range findings {
		codes = append(codes, finding.Code)
	}
	return codes
}

func containsFindingCode(findings []Finding, code FindingCode) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

// Exact classifications for malformed predicates embedded in the temporary
// production/test source strings above. Locations bind physical occurrences.
// predicate-audit:invalid {"location":"line:16:column:28:embedded-structured:inner-offset:88","kind":"stored-predicate","value":"legacy.bad_name","reason":"arity"}
// predicate-audit:invalid {"location":"line:210:column:28:embedded-structured:inner-offset:117","kind":"stored-predicate","value":"legacy.bad_name","reason":"arity"}
// predicate-audit:invalid {"location":"line:213:column:28:embedded-structured:inner-offset:43","kind":"stored-predicate","value":"also.bad_name","reason":"arity"}
// predicate-audit:invalid {"location":"line:398:column:14:embedded-structured:inner-offset:201","kind":"stored-predicate","value":"quest.failed","reason":"arity"}
// predicate-audit:invalid {"location":"line:424:column:28:embedded-structured:inner-offset:166","kind":"stored-predicate","value":"quest.failed","reason":"arity"}
// predicate-audit:invalid {"location":"line:450:column:28:embedded-structured:inner-offset:231","kind":"stored-predicate","value":"quest.failed","reason":"arity"}
// predicate-audit:invalid {"location":"line:589:column:21:embedded-structured:inner-offset:182","kind":"stored-predicate","value":"quest.failed","reason":"arity"}
