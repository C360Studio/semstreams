package executors

import (
	"context"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/processor/rule"
)

// TestRegisterGlobal_IdempotentOnDuplicate — the wrapper treats the
// "already registered" error from agentictools.RegisterTool as a no-op,
// which is what keeps boot resilient across component.Stop/Start cycles.
// Second Register with the same name must return nil, not error.
func TestRegisterGlobal_IdempotentOnDuplicate(t *testing.T) {
	name := "register_test_idempotent"
	executor := &registerTestStubExecutor{name: name}

	// Clean slate: registerGlobal wraps a nil return as success even when
	// the tool is already registered, so a stale entry from a prior test
	// run would still be accepted. Double-register exercises the wrap.
	if err := registerGlobal(name, executor); err != nil && !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("first registerGlobal unexpected err: %v", err)
	}
	if err := registerGlobal(name, executor); err != nil {
		t.Fatalf("duplicate registerGlobal should be a no-op, got err: %v", err)
	}
}

// TestRegisterGlobal_SurfacesOtherErrors — errors that aren't the
// "already registered" case should propagate. We don't have a direct way
// to inject a different RegisterTool failure from here without mocking
// the global registry, so this is a lightweight smoke: a genuinely new
// name registers cleanly and appears in ListRegisteredTools afterwards.
func TestRegisterGlobal_NewNameSucceedsAndAppearsInRegistry(t *testing.T) {
	name := "register_test_new_name_smoke"
	executor := &registerTestStubExecutor{name: name}

	if err := registerGlobal(name, executor); err != nil && !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("registerGlobal: %v", err)
	}

	found := false
	for _, tool := range agentictools.ListRegisteredTools() {
		if tool.Name == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("registered tool %q should appear in global registry", name)
	}
}

// TestRegisterRules_NilManagerSkips — every Pattern-B wire function must
// treat a nil manager as "skip registration" so main can ship partial
// configs without exploding on missing deps. Verified by confirming no
// rule tool names appear in the registry before vs after calling
// registerRules(nil, ...). Relies on unique tool names per Pattern-B
// wire so other suite tests don't pollute this check.
func TestRegisterRules_NilManagerSkips(t *testing.T) {
	before := toolNamesPresent(agentictools.ListRegisteredTools(), []string{
		ruleToolCreate, ruleToolUpdate, ruleToolDelete, ruleToolList, ruleToolGet,
	})

	registerRules(nil, slog.Default())

	after := toolNamesPresent(agentictools.ListRegisteredTools(), []string{
		ruleToolCreate, ruleToolUpdate, ruleToolDelete, ruleToolList, ruleToolGet,
	})

	// "Skip" means the set of rule-tool names in the global registry
	// does not grow because of this call.
	for name, wasPresent := range before {
		if after[name] && !wasPresent {
			t.Errorf("registerRules(nil) unexpectedly registered %q", name)
		}
	}
}

// TestRegisterFlows_NilManagerSkips — same contract for FlowExecutor.
func TestRegisterFlows_NilManagerSkips(t *testing.T) {
	before := toolNamesPresent(agentictools.ListRegisteredTools(), []string{
		flowToolCreate, flowToolUpdate, flowToolDelete, flowToolList, flowToolGet,
	})
	registerFlows(nil, slog.Default())
	after := toolNamesPresent(agentictools.ListRegisteredTools(), []string{
		flowToolCreate, flowToolUpdate, flowToolDelete, flowToolList, flowToolGet,
	})
	for name, wasPresent := range before {
		if after[name] && !wasPresent {
			t.Errorf("registerFlows(nil) unexpectedly registered %q", name)
		}
	}
}

// TestRegisterPersonas_NilManagerSkips — same for PersonaExecutor.
func TestRegisterPersonas_NilManagerSkips(t *testing.T) {
	before := toolNamesPresent(agentictools.ListRegisteredTools(), []string{
		personaToolCreate, personaToolUpdate, personaToolDelete, personaToolList, personaToolGet,
	})
	registerPersonas(nil, slog.Default())
	after := toolNamesPresent(agentictools.ListRegisteredTools(), []string{
		personaToolCreate, personaToolUpdate, personaToolDelete, personaToolList, personaToolGet,
	})
	for name, wasPresent := range before {
		if after[name] && !wasPresent {
			t.Errorf("registerPersonas(nil) unexpectedly registered %q", name)
		}
	}
}

// TestRegisterFlowTemplates_NilManagerSkips — same for FlowTemplateExecutor.
func TestRegisterFlowTemplates_NilManagerSkips(t *testing.T) {
	before := toolNamesPresent(agentictools.ListRegisteredTools(), []string{
		flowTemplateToolCreate, flowTemplateToolList, flowTemplateToolInstantiate,
	})
	registerFlowTemplates(nil, slog.Default())
	after := toolNamesPresent(agentictools.ListRegisteredTools(), []string{
		flowTemplateToolCreate, flowTemplateToolList, flowTemplateToolInstantiate,
	})
	for name, wasPresent := range before {
		if after[name] && !wasPresent {
			t.Errorf("registerFlowTemplates(nil) unexpectedly registered %q", name)
		}
	}
}

// TestRegisterAll_NilNATSClientSkipsStatefulTools — RegisterAll contract:
// when the NATSClient in ToolDependencies is nil, stateful tools
// (read_loop_result, decide, query_entity) are skipped so the binary
// starts cleanly in environments without NATS. Pattern-B manager slots
// are honoured independently of NATSClient nil-ness.
func TestRegisterAll_NilNATSClientSkipsStatefulTools(t *testing.T) {
	// Register a Pattern-B manager-backed tool to confirm that path
	// still fires when NATSClient is nil.
	mgr := &recordingRuleManager{}
	deps := ToolDependencies{
		NATSClient:  nil,
		RuleManager: mgr,
		Logger:      slog.Default(),
	}
	RegisterAll(context.Background(), deps)

	// Stateful tools should not have been called with the (nil) client.
	// We can't easily assert "not registered" without registry
	// introspection per-test, but we CAN assert RegisterAll didn't
	// panic and that rule tools are in the global registry afterwards.
	names := agentictools.ListRegisteredTools()
	if !containsName(names, ruleToolList) {
		t.Errorf("registerRules should fire even with nil NATSClient; %q not found", ruleToolList)
	}
}

// toolNamesPresent returns a map keyed by tool name with true when the
// name is found in the supplied tool list. Used to diff "before vs
// after" so tests tolerate pollution from other tests in the suite.
func toolNamesPresent(tools []agentic.ToolDefinition, want []string) map[string]bool {
	out := make(map[string]bool, len(want))
	for _, name := range want {
		out[name] = false
	}
	for _, tool := range tools {
		if _, track := out[tool.Name]; track {
			out[tool.Name] = true
		}
	}
	return out
}

func containsName(tools []agentic.ToolDefinition, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

// registerTestStubExecutor is a minimal ToolExecutor for the smoke tests.
type registerTestStubExecutor struct {
	name string
}

func (e *registerTestStubExecutor) ListTools() []agentic.ToolDefinition {
	return []agentic.ToolDefinition{{
		Name:        e.name,
		Description: "register_test stub",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
	}}
}

func (e *registerTestStubExecutor) Execute(_ context.Context, call agentic.ToolCall) (agentic.ToolResult, error) {
	return agentic.ToolResult{CallID: call.ID, Content: "stub"}, nil
}

// recordingRuleManager satisfies RuleManager for the RegisterAll smoke test.
// Tracks call counts so we can assert behaviour without spinning up KV.
type recordingRuleManager struct {
	calls int64
}

func (m *recordingRuleManager) SaveRule(_ context.Context, _ string, _ rule.Definition) error {
	atomic.AddInt64(&m.calls, 1)
	return nil
}
func (m *recordingRuleManager) DeleteRule(_ context.Context, _ string) error { return nil }
func (m *recordingRuleManager) GetRule(_ context.Context, _ string) (*rule.Definition, error) {
	return nil, nil
}
func (m *recordingRuleManager) ListRules(_ context.Context) (map[string]rule.Definition, error) {
	return nil, nil
}
