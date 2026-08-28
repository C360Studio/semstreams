package composition_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/composition"
	semtypes "github.com/c360studio/semstreams/pkg/types"
	"github.com/c360studio/semstreams/types"
)

func overlapFixture(t *testing.T) (*component.Registry, map[string]types.ComponentConfig) {
	t.Helper()
	registry := fakeRegistry(t,
		fakeSpec{name: "src", typ: "input", outputs: []component.PortDefinition{natsOut("out", "data.raw", nil)}},
		fakeSpec{name: "sink", typ: "output", inputs: []component.PortDefinition{natsIn("in", "data.raw", true, nil)}},
	)
	return registry, map[string]types.ComponentConfig{
		"src":  instance("src", types.ComponentTypeInput),
		"sink": instance("sink", types.ComponentTypeOutput),
	}
}

func overlapFindings(result composition.Result) []composition.Finding {
	var out []composition.Finding
	for _, finding := range append(append([]composition.Finding{}, result.Errors...), result.Warnings...) {
		if finding.Type == composition.TypeEntityDomainOverlap {
			out = append(out, finding)
		}
	}
	return out
}

// TestValidateReportsSharedEntityDomainAsNonBlockingFinding pins the owner
// ruling of 2026-08-28: two producers delegating one domain is permitted, and
// is REPORTED by the offline validator rather than refused at boot. The
// severity assertion is the load-bearing half — an error-severity finding
// would trip the #1101 boot refusal on the intended case.
func TestValidateReportsSharedEntityDomainAsNonBlockingFinding(t *testing.T) {
	t.Parallel()
	registry, components := overlapFixture(t)
	cfg := compositionOf(components)

	result, err := composition.Validate(registry, cfg,
		semtypes.EntityDomainDelegation{Producer: "semsource", Domain: "web"},
		semtypes.EntityDomainDelegation{Producer: "semdragon", Domain: "web"},
		semtypes.EntityDomainDelegation{Producer: "semsource", Domain: "git"},
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	data, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatalf("marshal result: %v", marshalErr)
	}
	var decoded composition.Result
	if unmarshalErr := json.Unmarshal(data, &decoded); unmarshalErr != nil {
		t.Fatalf("decode result: %v", unmarshalErr)
	}

	found := overlapFindings(decoded)
	if len(found) != 1 {
		t.Fatalf("overlap findings = %#v, want exactly one for the shared domain", found)
	}
	if found[0].Severity != composition.SeverityWarning {
		t.Fatalf("finding = %#v, want warning severity: an error trips the boot refusal on the intended case", found[0])
	}
	for _, finding := range decoded.Errors {
		if finding.Type == composition.TypeEntityDomainOverlap {
			t.Fatalf("errors = %#v, an overlap must never be an error-severity finding", decoded.Errors)
		}
	}
	if decoded.Status == composition.StatusErrors {
		t.Fatalf("status = %q, an overlap alone must not make a composition invalid", decoded.Status)
	}
	if !strings.Contains(found[0].Message, "web") ||
		!strings.Contains(found[0].Message, "semsource") || !strings.Contains(found[0].Message, "semdragon") {
		t.Fatalf("message = %q, want the shared domain and both producers named", found[0].Message)
	}
	if len(found[0].Suggestions) == 0 {
		t.Fatalf("finding = %#v, every finding carries suggestions", found[0])
	}
}

// TestValidateDoesNotReportUnsharedOrRepeatedDelegations pins the negative
// half: one producer's own repeated or narrowed delegation is not an overlap,
// and neither is a domain only one producer declares.
func TestValidateDoesNotReportUnsharedOrRepeatedDelegations(t *testing.T) {
	t.Parallel()
	registry, components := overlapFixture(t)

	result, err := composition.Validate(registry, compositionOf(components),
		semtypes.EntityDomainDelegation{Producer: "semsource", Domain: "web"},
		semtypes.EntityDomainDelegation{Producer: "semsource", Domain: "web"},
		semtypes.EntityDomainDelegation{Producer: "semsource", Domain: "web", Type: "page"},
		semtypes.EntityDomainDelegation{Producer: "semdragon", Domain: "game"},
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if found := overlapFindings(*result); len(found) != 0 {
		t.Fatalf("overlap findings = %#v, want none: one producer repeating or narrowing its own domain is not overlap", found)
	}

	noDelegations, err := composition.Validate(registry, compositionOf(components))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if found := overlapFindings(*noDelegations); len(found) != 0 {
		t.Fatalf("overlap findings = %#v, want none when no delegations are supplied", found)
	}
}

// TestBootAnalysisCannotSeeEntityDomainOverlap pins the structural guarantee
// behind the ruling: the boot refusal runs composition.Analyze
// (service/component_manager.go analyzeBootComposition), which takes no
// delegations at all, so the overlap report is unreachable from it by
// construction — not merely by severity.
func TestBootAnalysisCannotSeeEntityDomainOverlap(t *testing.T) {
	t.Parallel()
	registry, components := overlapFixture(t)
	cfg := compositionOf(components)

	withOverlap, err := composition.Validate(registry, cfg,
		semtypes.EntityDomainDelegation{Producer: "semsource", Domain: "web"},
		semtypes.EntityDomainDelegation{Producer: "semdragon", Domain: "web"},
	)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(overlapFindings(*withOverlap)) != 1 {
		t.Fatalf("fixture did not produce the overlap the boot check is measured against: %#v", withOverlap)
	}

	declarations := make([]component.Declaration, 0, len(components))
	for name, entry := range components {
		declaration, declareErr := registry.Declare(name, entry)
		if declareErr != nil {
			t.Fatalf("Declare %s: %v", name, declareErr)
		}
		declarations = append(declarations, declaration)
	}
	analysis := composition.Analyze(declarations, nil)
	for _, finding := range append(append([]composition.Finding{}, analysis.Errors...), analysis.Warnings...) {
		if finding.Type == composition.TypeEntityDomainOverlap {
			t.Fatalf("Analyze emitted %#v; the boot refusal path must not be able to observe an overlap", finding)
		}
	}
}
