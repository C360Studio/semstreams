package contract

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
	"github.com/c360studio/semstreams/vocabulary/builtins"
	"github.com/c360studio/semstreams/vocabulary/export"
)

// Repository contract guards for the closed predicate datatype vocabulary
// (gh#1267, ADR-107). Two different jobs, deliberately not one test:
//
//   - registration validation enforces the SET — every declared value is
//     canonical or refused. It cannot enforce completeness, because an absent
//     datatype must stay legal: three sister repositories register nothing but
//     a predicate name, and vocabulary/rulepacks/predicates.go registers
//     thirteen bare names on purpose;
//   - these guards enforce that the closed set has exactly one RDF rendering
//     each at the export edge, and that the set of framework predicates
//     declaring NOTHING can only shrink.

// canonicalDataTypes is the closed declaration vocabulary as the CONTRACT sees
// it: the seven exported constants, written out here rather than read back from
// the vocabulary package's own membership check.
//
// Two reasons it is spelled out. The membership check is unexported (gh#1267
// owner ruling "in package": ADR-106 freezes this Tier 1 surface, so a helper
// no adopter asked for is a permanent bill). And a guard that asked the
// implementation "is this canonical?" could not see a value dropped from the
// set — it would agree with whatever the implementation now believes. The
// exported constants ARE the contract an adopter writes against, so they are
// the right thing to walk.
var canonicalDataTypes = []string{
	vocabulary.DataTypeString,
	vocabulary.DataTypeEntityID,
	vocabulary.DataTypeInt,
	vocabulary.DataTypeFloat,
	vocabulary.DataTypeBool,
	vocabulary.DataTypeDateTime,
	vocabulary.DataTypeJSON,
}

func isCanonicalDataType(s string) bool {
	return slices.Contains(canonicalDataTypes, s)
}

// canonicalDataTypeRenderings is the complete export mapping for the closed
// declaration vocabulary: one row per canonical value, and the row says what
// the serializer emits for an object that carries it.
//
// ADR-107 puts this mapping at the export edge, so the assertion is on
// vocabulary/export's OUTPUT rather than on constant equality: the declaration
// values are deliberately NOT the per-triple markers (`entity_id` is not
// `@id`, `json` is not `rdf:JSON`), and an equality check between them would
// assert the opposite of the ruling.
var canonicalDataTypeRenderings = []struct {
	declared string
	object   any
	// want is the exact N-Triples object term the serializer must emit.
	want string
}{
	{vocabulary.DataTypeString, "hovering", `"hovering"`},
	{
		vocabulary.DataTypeEntityID,
		"acme.ops.gcs.robotics.drone.002",
		"<https://semstreams.semanticstream.ing/entities/acme/ops/gcs/robotics/drone/002>",
	},
	{vocabulary.DataTypeInt, 5.0, `"5"^^<http://www.w3.org/2001/XMLSchema#integer>`},
	{vocabulary.DataTypeFloat, 5.5, `"5.5"^^<http://www.w3.org/2001/XMLSchema#double>`},
	{vocabulary.DataTypeBool, true, `"true"^^<http://www.w3.org/2001/XMLSchema#boolean>`},
	{
		vocabulary.DataTypeDateTime,
		time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC),
		`"2026-09-09T12:00:00Z"^^<http://www.w3.org/2001/XMLSchema#dateTime>`,
	},
	{
		vocabulary.DataTypeJSON,
		`{"done":true}`,
		`"{\"done\":true}"^^<http://www.w3.org/1999/02/22-rdf-syntax-ns#JSON>`,
	},
}

// TestEveryCanonicalDataTypeHasExactlyOneExportRendering walks the closed
// vocabulary and asserts each value renders, that no two render the same way,
// and that entity_id renders as a resource rather than as a literal.
func TestEveryCanonicalDataTypeHasExactlyOneExportRendering(t *testing.T) {
	restore := vocabulary.SnapshotRegistry()
	defer restore()

	const subject = "acme.ops.gcs.robotics.drone.001"
	seenDeclared := make(map[string]bool, len(canonicalDataTypeRenderings))
	seenRendering := make(map[string]string, len(canonicalDataTypeRenderings))

	for i, row := range canonicalDataTypeRenderings {
		if !isCanonicalDataType(row.declared) {
			t.Errorf("row %d declares %q, which is not in the closed vocabulary", i, row.declared)
			continue
		}
		if seenDeclared[row.declared] {
			t.Errorf("%q has more than one row; the mapping must be exactly one rendering per value",
				row.declared)
		}
		seenDeclared[row.declared] = true

		predicate := fmt.Sprintf("datatype.rendering.p%d", i)
		vocabulary.Register(predicate, vocabulary.WithDataType(row.declared))

		out, err := export.SerializeToString([]message.Triple{{
			Subject: subject, Predicate: predicate, Object: row.object,
		}}, export.NTriples)
		if err != nil {
			t.Errorf("%q: SerializeToString: %v", row.declared, err)
			continue
		}
		got := nTriplesObjectTerm(t, out)
		if got != row.want {
			t.Errorf("%q renders as %s, want %s", row.declared, got, row.want)
			continue
		}
		if other, ok := seenRendering[got]; ok {
			t.Errorf("%q and %q render identically as %s; each value needs its own rendering",
				other, row.declared, got)
		}
		seenRendering[got] = row.declared
	}

	// The whole closed set is covered. canonicalDataTypes is the membership
	// oracle; the count is the tripwire that an eighth constant added without a
	// rendering cannot pass silently.
	if len(seenDeclared) != len(canonicalDataTypes) {
		t.Errorf("the mapping covers %d canonical values; the closed vocabulary has %d",
			len(seenDeclared), len(canonicalDataTypes))
	}
	if len(canonicalDataTypes) != 7 {
		t.Errorf("the closed vocabulary is %d values, not 7 — every guard in this file walks this set "+
			"and the export mapping above must gain a row with it", len(canonicalDataTypes))
	}

	// entity_id is the one value whose rendering is not a literal at all.
	entityIDRendering := seenRendering["<https://semstreams.semanticstream.ing/entities/acme/ops/gcs/robotics/drone/002>"]
	if entityIDRendering != vocabulary.DataTypeEntityID {
		t.Errorf("entity_id does not render as an IRI resource; got the resource rendering from %q",
			entityIDRendering)
	}
	for rendering, declared := range seenRendering {
		if declared == vocabulary.DataTypeEntityID && strings.HasPrefix(rendering, `"`) {
			t.Errorf("entity_id rendered as the literal %s; it must be a resource", rendering)
		}
	}
}

// nTriplesObjectTerm returns the object term of a single-line N-Triples document.
func nTriplesObjectTerm(t *testing.T, ntriples string) string {
	t.Helper()
	fields := strings.SplitN(strings.TrimSpace(ntriples), " ", 3)
	if len(fields) != 3 {
		t.Fatalf("not a single N-Triples statement: %q", ntriples)
	}
	return strings.TrimSuffix(strings.TrimSpace(fields[2]), " .")
}

// predicatesDeclaringNoDataType is the measured set of framework predicates
// that declare no datatype at all, as of gh#1267. Absence is legal, so this is
// not a defect list — it is a RATCHET: the guard below fails when a predicate
// outside this set declares nothing, so the set can only shrink.
//
// It exists because the completeness scenario the change specifies
// ("the framework's own declarations are complete") was measured false against
// the code: 81 framework predicates declare nothing, and thirteen of them are
// deliberately bare (vocabulary/rulepacks/predicates.go registers names only).
// Declaring a datatype for each is a separate pass of 81 semantic judgements,
// and inventing them here would be the fabrication this change exists to stop.
var predicatesDeclaringNoDataType = map[string]bool{
	"agent.loop.role":                   true,
	"agent.run.last-transition-at":      true,
	"agent.run.last-transition-from":    true,
	"agent.run.last-transition-note":    true,
	"agent.run.last-transition-source":  true,
	"agent.run.origin-entity-id":        true,
	"agent.run.parent-entity-id":        true,
	"agent.run.phase":                   true,
	"agentic.checkpoint.completed":      true,
	"agentic.checkpoint.iteration":      true,
	"agentic.checkpoint.started":        true,
	"agentic.decision.detected":         true,
	"agentic.file.modified":             true,
	"agentic.tool.file-operation":       true,
	"agentic.tool.used":                 true,
	"coordinator.decision.next-action":  true,
	"coordinator.decision.reason":       true,
	"coordinator.decision.sap-coerced":  true,
	"coordinator.decision.subtopics":    true,
	"coordinator.decision.synthetic":    true,
	"entity.identity.type":              true,
	"gateddag.fanout.phase":             true,
	"gateddag.unit.claim":               true,
	"gateddag.unit.completed":           true,
	"gateddag.unit.depends-on":          true,
	"gateddag.unit.dirtied":             true,
	"gateddag.unit.failed":              true,
	"gather.child.completed":            true,
	"graph.rel.blocked-by":              true,
	"graph.rel.communicates":            true,
	"graph.rel.contains":                true,
	"graph.rel.depends-on":              true,
	"graph.rel.discusses":               true,
	"graph.rel.implements":              true,
	"graph.rel.influences":              true,
	"graph.rel.near":                    true,
	"graph.rel.references":              true,
	"graph.rel.related-to":              true,
	"graph.rel.supersedes":              true,
	"graph.rel.triggered-by":            true,
	"lifecycle.transition.at":           true,
	"lifecycle.transition.from":         true,
	"lifecycle.transition.note":         true,
	"lifecycle.transition.source":       true,
	"lifecycle.transition.to":           true,
	"ops.config.accuracy":               true,
	"ops.config.active":                 true,
	"ops.config.cost-per-task":          true,
	"ops.config.p95-latency":            true,
	"ops.config.parent":                 true,
	"ops.diagnosis.confidence":          true,
	"ops.diagnosis.evidence":            true,
	"ops.diagnosis.finding":             true,
	"ops.diagnosis.observed-role":       true,
	"ops.diagnosis.recommendation":      true,
	"ops.diagnosis.severity":            true,
	"research.assess.complete":          true,
	"research.assess.sufficient":        true,
	"research.classify.candidate-count": true,
	"research.classify.complete":        true,
	"research.classify.degraded":        true,
	"research.evidence.present":         true,
	"research.execute.complete":         true,
	"research.execute.evidence-count":   true,
	"research.loop.id":                  true,
	"research.parent.loop":              true,
	"research.parent.role":              true,
	"research.request.budget-tokens":    true,
	"research.request.hint":             true,
	"research.request.max-iterations":   true,
	"research.request.received":         true,
	"research.request.topic":            true,
	"research.route.action":             true,
	"research.route.complete":           true,
	"research.search-result.complete":   true,
	"research.search-result.ref":        true,
	"research.state.status":             true,
	"workflow.review.rejections":        true,
	"workflow.state.phase":              true,
	"workflow.state.status":             true,
	"workflow.tokens.total":             true,
}

// TestFrameworkPredicateDataTypesAreCanonicalAndRatcheted walks every predicate
// the framework registers — package init() and the explicit composition root —
// and asserts two things: a declared datatype is always canonical, and a
// predicate declaring nothing is one of the measured exemptions.
func TestFrameworkPredicateDataTypesAreCanonicalAndRatcheted(t *testing.T) {
	restore := vocabulary.SnapshotRegistry()
	defer restore()

	declared := frameworkPredicateDataTypes()
	noncanonical, unexpectedlyBare := auditPredicateDataTypes(declared)

	if len(noncanonical) > 0 {
		t.Errorf("predicates carrying a datatype outside the closed vocabulary: %v", noncanonical)
	}
	if len(unexpectedlyBare) > 0 {
		t.Errorf("predicates declaring no datatype that are not in the measured exemption set: %v\n"+
			"Declare one of %v, or — if absence is genuinely right for it — add it to "+
			"predicatesDeclaringNoDataType with the reason.",
			unexpectedlyBare, canonicalDataTypes)
	}
}

// TestPredicateDataTypeRatchetCanFail shows the guard above is capable of
// failing, per the registry-audit precedent the predicate-contract spec sets.
func TestPredicateDataTypeRatchetCanFail(t *testing.T) {
	seeded := map[string]string{
		"contract.probe.bare":         "",
		"contract.probe.noncanonical": "float64",
		"contract.probe.fine":         vocabulary.DataTypeString,
	}
	for exempt := range predicatesDeclaringNoDataType {
		seeded[exempt] = ""
		break
	}

	noncanonical, unexpectedlyBare := auditPredicateDataTypes(seeded)
	if len(noncanonical) != 1 || noncanonical[0] != "contract.probe.noncanonical=float64" {
		t.Errorf("noncanonical = %v, want exactly [contract.probe.noncanonical=float64]", noncanonical)
	}
	if len(unexpectedlyBare) != 1 || unexpectedlyBare[0] != "contract.probe.bare" {
		t.Errorf("unexpectedlyBare = %v, want exactly [contract.probe.bare] — "+
			"an exempt bare predicate must not be reported", unexpectedlyBare)
	}
}

// frameworkPredicateDataTypes collects every predicate the framework declares,
// from BOTH sources: the package init() registrations present in any binary
// that imports the vocabulary packages, and the explicit composition root
// builtins.Register(). The caller holds a registry snapshot.
func frameworkPredicateDataTypes() map[string]string {
	declared := make(map[string]string)
	for _, predicate := range vocabulary.ListRegisteredPredicates() {
		declared[predicate] = vocabulary.GetPredicateMetadata(predicate).DataType
	}

	vocabulary.ClearRegistry()
	builtins.Register()
	for _, predicate := range vocabulary.ListRegisteredPredicates() {
		if _, seen := declared[predicate]; !seen {
			declared[predicate] = vocabulary.GetPredicateMetadata(predicate).DataType
		}
	}
	return declared
}

// auditPredicateDataTypes is the guard's decision, extracted so the
// capable-of-failing test drives the same code the live walk does.
func auditPredicateDataTypes(declared map[string]string) (noncanonical, unexpectedlyBare []string) {
	for predicate, dataType := range declared {
		switch {
		case dataType == "":
			if !predicatesDeclaringNoDataType[predicate] {
				unexpectedlyBare = append(unexpectedlyBare, predicate)
			}
		case !isCanonicalDataType(dataType):
			noncanonical = append(noncanonical, predicate+"="+dataType)
		}
	}
	sort.Strings(noncanonical)
	sort.Strings(unexpectedlyBare)
	return noncanonical, unexpectedlyBare
}
