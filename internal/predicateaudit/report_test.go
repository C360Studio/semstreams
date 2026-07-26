package predicateaudit

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestReportIsVersionedDeterministicAndComplete(t *testing.T) {
	t.Parallel()
	candidates := []Candidate{
		{
			File: "z.go", Line: 3, Column: 14, Predicate: "quest.failed",
			Surface: "go-assignment:predicate", Authority: AuthorityPredicateShaped,
			Status: CandidateStatusClassifiedUnrelated, ClassificationBasis: "reviewed event",
		},
		{
			File: "a.go", Line: 2, Column: 27, Predicate: "legacy.bad_name",
			Surface: "go-field:Predicate", Authority: AuthorityStoredPredicate,
			Status: CandidateStatusFinding,
		},
	}
	findings := []Finding{{
		Candidate: candidates[1], Code: FindingInvalidPredicate, Reason: "predicate invalid",
	}}

	report := BuildReport([]string{"z-root", "a-root"}, candidates, findings)
	first, err := MarshalReport(report)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MarshalReport(BuildReport([]string{"z-root", "a-root"}, candidates, findings))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("report is not deterministic:\n%s\n%s", first, second)
	}

	var decoded Report
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Version != PredicateAuditReportVersion {
		t.Fatalf("version = %d, want %d", decoded.Version, PredicateAuditReportVersion)
	}
	if !reflect.DeepEqual(decoded.Roots, []string{"a-root", "z-root"}) {
		t.Fatalf("roots = %#v, want sorted roots", decoded.Roots)
	}
	if decoded.Counts.Candidates != 2 || decoded.Counts.Classifications != 1 || decoded.Counts.Findings != 1 {
		t.Fatalf("counts = %#v", decoded.Counts)
	}
	if len(decoded.Candidates) != 2 || len(decoded.Classifications) != 1 || len(decoded.Findings) != 1 {
		t.Fatalf("report = %#v, want candidates/classifications/findings", decoded)
	}
	classification := decoded.Classifications[0]
	if classification.File != "z.go" || classification.Line != 3 || classification.Column != 14 ||
		classification.Surface != "go-assignment:predicate" || classification.Value != "quest.failed" ||
		classification.Basis != "reviewed event" {
		t.Fatalf("classification = %#v, want exact locator and basis", classification)
	}
	if decoded.Findings[0].Code != FindingInvalidPredicate {
		t.Fatalf("finding = %#v, want stable code", decoded.Findings[0])
	}
}

// Exact classifications for malformed predicate report fixtures above.
// predicate-audit:invalid {"location":"line:14:column:50","kind":"stored-predicate","value":"quest.failed","reason":"arity"}
// predicate-audit:invalid {"location":"line:19:column:50","kind":"stored-predicate","value":"legacy.bad_name","reason":"arity"}
