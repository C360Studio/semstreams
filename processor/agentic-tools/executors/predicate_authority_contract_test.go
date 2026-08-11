package executors

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/projection"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
)

// predicateBearingParams are input-schema property names that would hand a
// model direct control over what predicate reaches the graph.
//
// This is the enforcement boundary that actually exists. `predicate-contract-
// enforcement` task 5.6c originally proposed a principal-bearing mutation
// envelope; that was rescoped (Fable ruling, 2026-07-31) because there is no
// principal to bear — NATS auth is connection-level and no Principal/Actor
// concept exists in graph/ or pkg/projection/ — and because denying one
// namespace does not bound an actor who can write every other one.
//
// What IS real: the LLM is the only semi-trusted principal in the system, and
// its graph-writing tools (emit_lesson, emit_diagnosis, decide, scratchpad)
// construct predicates internally. A model therefore cannot mint
// `agent.lineage.*` today. That property was intact and unasserted; this
// asserts it.
var predicateBearingParams = []string{
	"predicate",
	"predicates",
	"triple",
	"triples",
}

// scanForCallerControlledPredicate walks a tool's JSON-schema parameters and
// returns the offending property paths, if any.
//
// It recurses, because a predicate handed over inside a nested object or array
// item is caller-controlled just the same as one at the top level.
func scanForCallerControlledPredicate(path string, schema map[string]any) []string {
	var found []string

	if props, ok := schema["properties"].(map[string]any); ok {
		for name, raw := range props {
			here := name
			if path != "" {
				here = path + "." + name
			}
			if isPredicateBearingName(name) && typeCanCarryGrammar(raw) {
				found = append(found, here)
			}
			if sub, ok := raw.(map[string]any); ok {
				found = append(found, scanForCallerControlledPredicate(here, sub)...)
			}
		}
	}
	if items, ok := schema["items"].(map[string]any); ok {
		found = append(found, scanForCallerControlledPredicate(path+"[]", items)...)
	}
	// Schema combinators and shared definitions hide properties from a
	// properties-only walk — the spec bans the grammar-bearing value wherever
	// it appears, so a `oneOf` arm or a `$defs` entry is the same surface
	// (2026-08-02 audit: the previous walk missed all of these).
	for _, combinator := range []string{"oneOf", "anyOf", "allOf"} {
		if arms, ok := schema[combinator].([]any); ok {
			for i, raw := range arms {
				if sub, ok := raw.(map[string]any); ok {
					found = append(found, scanForCallerControlledPredicate(
						fmt.Sprintf("%s{%s[%d]}", path, combinator, i), sub)...)
				}
			}
		}
	}
	for _, defs := range []string{"$defs", "definitions"} {
		if m, ok := schema[defs].(map[string]any); ok {
			for name, raw := range m {
				if sub, ok := raw.(map[string]any); ok {
					found = append(found, scanForCallerControlledPredicate(path+"{"+defs+"."+name+"}", sub)...)
				}
			}
		}
	}
	if ap, ok := schema["additionalProperties"].(map[string]any); ok {
		found = append(found, scanForCallerControlledPredicate(path+"{additionalProperties}", ap)...)
	}
	return found
}

// isPredicateBearingName matches by SUBSTRING, not exact equality: the spec
// bans "a predicate, triple, or equivalent grammar-bearing value", and
// `predicate_uri` or `target_predicate` is the same authority handed over
// under a name the old exact-match list (2026-08-02 audit) waved through.
func isPredicateBearingName(name string) bool {
	lower := strings.ToLower(name)
	for _, banned := range predicateBearingParams {
		if strings.Contains(lower, banned) {
			return true
		}
	}
	return false
}

// typeCanCarryGrammar reports whether a property's declared JSON-schema type
// can convey a grammar-bearing VALUE. The spec bans accepting "a predicate,
// triple, or equivalent grammar-bearing value" — a boolean or numeric toggle
// whose NAME mentions predicates but carries only a verbosity toggle conveys
// no grammar and is not the banned surface. Absent or
// string/array/object types are conservatively treated as carriers.
func typeCanCarryGrammar(raw any) bool {
	schema, ok := raw.(map[string]any)
	if !ok {
		return true
	}
	switch schema["type"] {
	case "boolean", "number", "integer":
		return false
	default:
		return true
	}
}

// auditRegistryForPredicateAuthority is the check itself, run over a registry
// rather than a hand-maintained list of tool names. A static list would be two
// things that happen to agree today (Fable's condition on this task): it would
// pass for a NEW tool nobody remembered to add.
func auditRegistryForPredicateAuthority(reg *agentictools.ExecutorRegistry) []string {
	var violations []string
	for _, def := range reg.ListTools() {
		for _, offending := range scanForCallerControlledPredicate("", def.Parameters) {
			violations = append(violations, fmt.Sprintf("%s: parameter %q", def.Name, offending))
		}
	}
	sort.Strings(violations)
	return violations
}

// canaryExecutor declares a tool that DOES take a caller-controlled predicate.
// It exists only to prove the audit can fail.
type canaryExecutor struct{}

func (canaryExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name:        "canary_forge_lineage",
		Description: "Deliberately unsafe tool used only to prove the audit fires.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"entity_id": map[string]any{"type": "string"},
				"predicate": map[string]any{"type": "string"},
			},
		},
	}}
}

func (canaryExecutor) Execute(context.Context, agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{}, fmt.Errorf("canary executor must never run")
}

// TestAgentToolsCannotMintPredicates_CanaryFires proves the audit is capable of
// failing BEFORE the next test claims a clean registry means anything.
//
// Without this, "no violations found" is indistinguishable from "the scan never
// looked" — the failure mode this program keeps finding in its own guards.
func TestAgentToolsCannotMintPredicates_CanaryFires(t *testing.T) {
	t.Parallel()

	reg := agentictools.NewExecutorRegistry()
	if err := reg.RegisterExecutor(canaryExecutor{}); err != nil {
		t.Fatalf("register canary: %v", err)
	}

	violations := auditRegistryForPredicateAuthority(reg)
	if len(violations) == 0 {
		t.Fatal("audit reported CLEAN on a registry containing a tool with a `predicate` " +
			"parameter — the scan is not looking, and every other result from it is worthless")
	}
	if !strings.Contains(violations[0], "canary_forge_lineage") {
		t.Errorf("expected the canary to be named in the violation, got: %v", violations)
	}
}

// TestAgentToolsCannotMintPredicates is the contract: no builtin agent tool
// hands the model control over a predicate reaching the graph.
//
// Registry-derived, so a tool added later is covered without anyone
// remembering to update a list.
func TestAgentToolsCannotMintPredicates(t *testing.T) {
	t.Parallel()

	reg := agentictools.NewExecutorRegistry()
	// A NON-NIL client is required, and is the whole point. RegisterBuiltins
	// gates the stateful tools on `deps.NATSClient == nil` and SKIPS them
	// otherwise — including every graph-writing tool this contract is about
	// (decide, emit_diagnosis, emit_lesson, scratchpad). Passing zero deps
	// registered 3 tools, none of which touch the graph, and the audit passed
	// vacuously. Registration only stores the handle; nothing dials, so this
	// stays a unit test.
	if err := RegisterBuiltins(context.Background(), reg, ToolDependencies{
		NATSClient:     &natsclient.Client{},
		MutationClient: &projection.MutationClient{},
	}); err != nil {
		t.Fatalf("register builtins: %v", err)
	}

	tools := reg.ListTools()
	// A registry that registered almost nothing would pass the audit trivially.
	// This floor is not decoration: it CAUGHT the vacuous version of this test
	// (3 tools, no graph writers) during development.
	t.Logf("auditing %d builtin tools: %v", len(tools), toolNames(tools))
	if len(tools) < 5 {
		t.Fatalf("only %d builtin tools registered; the audit is running against a nearly empty "+
			"registry and proves nothing. Tools: %v", len(tools), toolNames(tools))
	}
	// The floor alone is vacuous the moment five graph-free tools register
	// (2026-08-02 audit): assert the graph-WRITING tools — the ones this
	// contract exists for — are individually present, so a future gating
	// change in RegisterBuiltins cannot silently drop them from the audit.
	registered := make(map[string]bool, len(tools))
	for _, name := range toolNames(tools) {
		registered[name] = true
	}
	for _, required := range []string{"decide", "emit_diagnosis", "emit_lesson", "scratchpad", "write_todos"} {
		if !registered[required] {
			t.Fatalf("graph-writing tool %q did not register — the predicate-authority audit is "+
				"no longer covering the surface it exists for. Registered: %v", required, toolNames(tools))
		}
	}

	if violations := auditRegistryForPredicateAuthority(reg); len(violations) > 0 {
		t.Errorf("agent tool(s) accept a caller-controlled predicate, which lets a model mint "+
			"arbitrary graph triples (predicate-contract-enforcement 5.6c):\n  %s\n\n"+
			"Graph-writing tools must construct predicates internally. If a tool genuinely needs "+
			"caller-chosen predicates, that is a namespace-authority decision, not a tool change — "+
			"see the deferred principal-envelope issue.",
			strings.Join(violations, "\n  "))
	}
}

func toolNames(defs []agentic.ToolDefinition) []string {
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	sort.Strings(names)
	return names
}
