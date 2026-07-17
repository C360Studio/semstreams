package predicateaudit

import (
	"os"
	"path/filepath"
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

func TestAuditClassifiesExplicitNegativeFixtureAndSkipsGoTests(t *testing.T) {
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
	if len(findings) != 0 {
		t.Fatalf("findings = %#v, want classified/ignored fixtures", findings)
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

// Exact classifications for malformed predicates embedded in the temporary
// production/test source strings above. Locations bind physical occurrences.
// predicate-audit:invalid {"location":"line:14:column:28:embedded-structured:inner-offset:88","kind":"stored-predicate","value":"legacy.bad_name","reason":"arity"}
// predicate-audit:invalid {"location":"line:35:column:28:embedded-structured:inner-offset:117","kind":"stored-predicate","value":"legacy.bad_name","reason":"arity"}
// predicate-audit:invalid {"location":"line:38:column:28:embedded-structured:inner-offset:43","kind":"stored-predicate","value":"also.bad_name","reason":"arity"}
