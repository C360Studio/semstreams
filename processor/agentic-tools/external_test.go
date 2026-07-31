package agentictools_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// projectedCanonicalFields are the agentic.ToolDefinition fields that the
// discovery DTO (agentictools.ToolDefinition) carries through to tool.list
// consumers.
var projectedCanonicalFields = []string{
	"Name",        // the identity a caller dispatches on
	"Description", // what discovery is for
	"Effect",      // gh#749 — served resolved, always present
}

// droppedCanonicalFields are the agentic.ToolDefinition fields the discovery
// DTO deliberately does NOT carry, each with the reason it is dropped:
//
//   - Parameters: full JSON Schema per tool. Payload weight on every discovery
//     response with no in-repo consumer; discovery answers "which tools exist
//     and what do they do", not "how do I call them" (the loop obtains
//     Parameters from the registry directly, not over tool.list).
//   - Strict: an OpenAI strict-mode constrained-decoding flag. A provider-wire
//     concern between the loop and the model; a discovery consumer cannot act
//     on it.
//   - Paginated: a contract between an executor and the agent loop's
//     buildToolMessages continuation handling. A loop concern, not a
//     discovery one.
var droppedCanonicalFields = []string{
	"Parameters",
	"Strict",
	"Paginated",
}

// TestDiscoveryProjection_CoversEveryCanonicalField fails when a field is added
// to agentic.ToolDefinition without a decision about whether discovery carries
// it.
//
// This exists because the projection in Component.ListTools is hand-maintained
// and had already silently dropped Strict and Paginated before anyone noticed —
// the same shape as the #787/#792 findings, where a hand-maintained enumeration
// drifted from the surface it claimed to describe. A hand-maintained
// enumeration is correct at most once; this makes the next drop deliberate
// instead of silent.
//
// The fix when this fails is NOT to add the field to droppedCanonicalFields
// reflexively — it is to decide, and to record the reason in the comment above
// whichever list it joins.
func TestDiscoveryProjection_CoversEveryCanonicalField(t *testing.T) {
	canonical := reflect.TypeOf(agentic.ToolDefinition{})

	classified := make(map[string]string, len(projectedCanonicalFields)+len(droppedCanonicalFields))
	for _, name := range projectedCanonicalFields {
		classified[name] = "projected"
	}
	for _, name := range droppedCanonicalFields {
		if prior, dup := classified[name]; dup {
			t.Fatalf("field %q appears in both lists (already %s): a field is projected or dropped, not both", name, prior)
		}
		classified[name] = "dropped"
	}

	var actual []string
	for i := 0; i < canonical.NumField(); i++ {
		field := canonical.Field(i)
		if !field.IsExported() {
			continue
		}
		actual = append(actual, field.Name)
		if _, ok := classified[field.Name]; !ok {
			t.Errorf("agentic.ToolDefinition.%s is neither projected nor deliberately dropped by the discovery DTO.\n"+
				"Decide: add it to projectedCanonicalFields (and populate it in Component.ListTools), "+
				"or to droppedCanonicalFields with the reason it has no discovery consumer.",
				field.Name)
		}
	}

	// The reverse direction: a name in either list that no longer exists on the
	// canonical struct means the lists have gone stale (field renamed or
	// removed), which would let a real field slip through unclassified.
	present := make(map[string]bool, len(actual))
	for _, name := range actual {
		present[name] = true
	}
	var stale []string
	for name := range classified {
		if !present[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("listed field(s) %v no longer exist on agentic.ToolDefinition — the projection lists are stale", stale)
	}

	// Guard the guard: a canonical struct that reflected as zero exported
	// fields would make every assertion above vacuous.
	if len(actual) == 0 {
		t.Fatal("reflected zero exported fields on agentic.ToolDefinition — the check is not looking at what it claims to")
	}
}

// TestDiscoveryDTO_EffectIsAlwaysPresent locks the wire shape of the DTO's
// effect field: no omitempty, so an unclassified tool serves the literal
// "unknown" rather than an absent key. A discovery consumer must never have to
// distinguish "absent" from "unknown" — that is the rule the framework boundary
// exists to absorb.
func TestDiscoveryDTO_EffectIsAlwaysPresent(t *testing.T) {
	field, ok := reflect.TypeOf(agentictools.ToolDefinition{}).FieldByName("Effect")
	if !ok {
		t.Fatal("discovery ToolDefinition has no Effect field")
	}
	if got := field.Tag.Get("json"); got != "effect" {
		t.Errorf("Effect json tag = %q, want %q (omitempty would elide the fail-safe value)", got, "effect")
	}
}
