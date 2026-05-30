package rule

import (
	"strings"
	"testing"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
)

func TestApplyEntityPartsSubstitutions_ValidSixPartID(t *testing.T) {
	t.Parallel()

	in := "org=$entity.org plat=$entity.platform domain=$entity.domain " +
		"system=$entity.system type=$entity.type instance=$entity.instance"
	want := "org=c360 plat=osh-demo-001 domain=agent system=agentic-loop " +
		"type=execution instance=c1e90237-1cd5-4def-99ab-aabbccddeeff"

	id := "c360.osh-demo-001.agent.agentic-loop.execution.c1e90237-1cd5-4def-99ab-aabbccddeeff"
	if got := applyEntityPartsSubstitutions(in, "entity", id); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestApplyEntityPartsSubstitutions_RelatedPrefix(t *testing.T) {
	t.Parallel()

	in := "rel.org=$related.org rel.instance=$related.instance"
	want := "rel.org=c360 rel.instance=001"

	if got := applyEntityPartsSubstitutions(in, "related", "c360.platform.domain.system.type.001"); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// A non-conforming entity ID (wrong segment count, empty, contains invalid
// characters) MUST leave tokens untouched so the unresolvedTemplateVarRe
// warning in execution_context.go surfaces the misuse — silently rendering
// empty would mask author errors and let bad keys land downstream.
func TestApplyEntityPartsSubstitutions_InvalidIDLeavesTokens(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"three parts", "a.b.c"},
		{"five parts", "a.b.c.d.e"},
		{"seven parts", "a.b.c.d.e.f.g"},
		{"invalid char in part", "c360.osh.agent.loop.execution.uuid$bad"},
	}

	in := "instance=$entity.instance type=$entity.type"
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := applyEntityPartsSubstitutions(in, "entity", tt.id)
			if got != in {
				t.Errorf("expected tokens to survive for invalid id %q, got %q", tt.id, got)
			}
		})
	}
}

// Template with no part tokens must pass through unchanged regardless of
// whether the entity ID is valid.
func TestApplyEntityPartsSubstitutions_NoTokens(t *testing.T) {
	t.Parallel()

	in := "entity=$entity.id state=$state.iteration"
	got := applyEntityPartsSubstitutions(in, "entity", "acme.ops.robotics.gcs.drone.001")
	if got != in {
		t.Errorf("got %q, want unchanged %q", got, in)
	}
}

// End-to-end through ExecutionContext.SubstituteVariables. Confirms the
// part substitution fires alongside $entity.id (the full form) so a
// template can reference both — useful for log messages that want both
// the human-readable full ID and a tool-arg-shaped instance.
func TestSubstituteVariables_EntityParts_FullPipeline(t *testing.T) {
	t.Parallel()

	ec := &ExecutionContext{
		EntityID:  "c360.osh-demo-001.agent.agentic-loop.execution.c1e90237",
		RelatedID: "c360.fleet.cars.toyota.sedan.vin-001",
	}

	in := "loop=$entity.instance full=$entity.id rel.type=$related.type"
	want := "loop=c1e90237 full=c360.osh-demo-001.agent.agentic-loop.execution.c1e90237 rel.type=sedan"

	if got := ec.SubstituteVariables(in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// Pins two co-existence properties that the substitution-pass ordering
// relies on for correctness:
//
//  1. `$entity.org` (the new part token) and `$entity.triple.org` (an
//     existing triple-predicate token) are independently resolvable in
//     the same template — neither pass interferes with the other. This
//     is the regression a "reorder the substitution passes for perf"
//     refactor would silently break.
//  2. `$entity.id` (full federated string) does NOT eat into
//     `$entity.instance` as a prefix-match. The two share the leading
//     `$entity.i` characters, and `strings.ReplaceAll` is literal-only
//     by design, but pinning the property explicitly guards against a
//     future regex-based substitution refactor accidentally matching
//     prefixes.
func TestSubstituteVariables_EntityParts_NoCollisionWithIDOrTriple(t *testing.T) {
	t.Parallel()

	ec := &ExecutionContext{
		EntityID: "acme.ops.robotics.gcs.drone.001",
		Entity: &gtypes.EntityState{
			Triples: []message.Triple{
				{Predicate: "org", Object: "triple-derived-org"},
			},
		},
	}

	in := "part=$entity.org triple=$entity.triple.org id=$entity.id instance=$entity.instance"
	want := "part=acme triple=triple-derived-org id=acme.ops.robotics.gcs.drone.001 instance=001"
	if got := ec.SubstituteVariables(in); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// gh#160: when two triples share a predicate prefix
// (e.g. "lineage.researcher-plan" vs "lineage.researcher-plan-entity"),
// the substitution must resolve the longer reference to its own value
// rather than letting the shorter predicate swallow the longer one and
// leave the suffix dangling.
//
// Pre-fix behaviour depended on triple-iteration order: if the shorter
// predicate landed first in the loop, `strings.ReplaceAll` would
// substitute its value inside `$entity.triple.lineage.researcher-plan-entity`
// and leave the literal `-entity` suffix orphaned — producing a phantom
// subject downstream. Sorting triples by predicate length descending
// makes the longest match-first, so prefix predicates can never swallow
// longer-prefix references.
//
// Reproduces the path-A workaround SemTeams shipped for #159 that
// surfaced this race in production smoke runs.
func TestSubstituteVariables_TriplePrefixCollision_LongestMatchFirst(t *testing.T) {
	t.Parallel()

	planUUID := "78a0e9ed-3da3-4bc4-ae33-1c3c8f4a0001"
	planEntity := "c360.bootstrap-001.agent.agentic-loop.execution." + planUUID

	tests := []struct {
		name        string
		tripleOrder []message.Triple
	}{
		{
			name: "shorter predicate first (regression case)",
			tripleOrder: []message.Triple{
				{Predicate: "lineage.researcher-plan", Object: planUUID},
				{Predicate: "lineage.researcher-plan-entity", Object: planEntity},
			},
		},
		{
			name: "longer predicate first (previously-passing case)",
			tripleOrder: []message.Triple{
				{Predicate: "lineage.researcher-plan-entity", Object: planEntity},
				{Predicate: "lineage.researcher-plan", Object: planUUID},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ec := &ExecutionContext{
				EntityID: "c360.bootstrap-001.agent.agentic-loop.execution.child",
				Entity: &gtypes.EntityState{
					Triples: tt.tripleOrder,
				},
			}

			in := "subject=$entity.triple.lineage.researcher-plan-entity short=$entity.triple.lineage.researcher-plan"
			want := "subject=" + planEntity + " short=" + planUUID

			if got := ec.SubstituteVariables(in); got != want {
				t.Errorf("got  %q\nwant %q", got, want)
			}
		})
	}
}

// Confirms the unresolvedTemplateVarRe in execution_context.go matches
// $entity.<part> tokens when the entity ID isn't 6-part, so authors see
// the warning rather than a silent empty render. Mirrors the surfacing
// behaviour for $entity.triple.X when the predicate is missing.
func TestSubstituteVariables_EntityParts_NonConformingIDWarnsViaRegex(t *testing.T) {
	t.Parallel()

	in := "instance=$entity.instance"
	leftovers := unresolvedTemplateVarRe.FindAllString(in, -1)
	if len(leftovers) != 1 || !strings.Contains(leftovers[0], "$entity.instance") {
		t.Fatalf("unresolvedTemplateVarRe did not match $entity.instance — got %v", leftovers)
	}
}
