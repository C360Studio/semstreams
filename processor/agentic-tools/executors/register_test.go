package executors

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	agentictools "github.com/c360studio/semstreams/processor/agentic-tools"
	"github.com/c360studio/semstreams/processor/rule"
)

// TestRegisterRules_NilManagerSkips — every Pattern-B wire function must
// treat a nil manager as "skip registration" so callers can ship partial
// configs without exploding on missing deps. With per-test registries we
// can assert "skipped" directly: the registry must be empty afterwards.
func TestRegisterRules_NilManagerSkips(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	registerRules(reg, nil, slog.Default())
	if got := len(reg.ListTools()); got != 0 {
		t.Fatalf("registerRules(nil) registered %d tools, want 0", got)
	}
}

func TestRegisterFlows_NilManagerSkips(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	registerFlows(reg, nil, slog.Default())
	if got := len(reg.ListTools()); got != 0 {
		t.Fatalf("registerFlows(nil) registered %d tools, want 0", got)
	}
}

func TestRegisterPersonas_NilManagerSkips(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	registerPersonas(reg, nil, slog.Default())
	if got := len(reg.ListTools()); got != 0 {
		t.Fatalf("registerPersonas(nil) registered %d tools, want 0", got)
	}
}

func TestRegisterFlowTemplates_NilManagerSkips(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	registerFlowTemplates(reg, nil, slog.Default())
	if got := len(reg.ListTools()); got != 0 {
		t.Fatalf("registerFlowTemplates(nil) registered %d tools, want 0", got)
	}
}

// TestRegisterBuiltins_NilNATSClientSkipsStatefulTools — RegisterBuiltins
// contract: when NATSClient is nil, stateful tools (read_loop_result,
// decide, emit_diagnosis, query_entity) are skipped so the binary boots
// cleanly in environments without NATS. Pattern-B manager-backed tools
// fire independently of NATSClient nil-ness.
func TestRegisterBuiltins_NilNATSClientSkipsStatefulTools(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	mgr := &recordingRuleManager{}
	deps := ToolDependencies{
		NATSClient:  nil,
		RuleManager: mgr,
		Logger:      slog.Default(),
	}
	if err := RegisterBuiltins(context.Background(), reg, deps); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}

	names := reg.ListTools()
	if !containsName(names, ruleToolList) {
		t.Errorf("registerRules should fire even with nil NATSClient; %q not found", ruleToolList)
	}
	// Stateful tools should NOT be present.
	if containsName(names, "read_loop_result") {
		t.Errorf("read_loop_result should be skipped when NATSClient is nil")
	}
	if containsName(names, "decide") {
		t.Errorf("decide should be skipped when NATSClient is nil")
	}
}

// TestRegisterBuiltins_NilRegistryErrors — callers must pass a non-nil
// registry. A nil registry returns an error so misconfiguration surfaces
// at boot rather than silently dropping registrations.
func TestRegisterBuiltins_NilRegistryErrors(t *testing.T) {
	t.Parallel()
	if err := RegisterBuiltins(context.Background(), nil, ToolDependencies{}); err == nil {
		t.Fatalf("RegisterBuiltins(nil registry) should error")
	}
}

func containsName(tools []agentic.ToolDefinition, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

// recordingRuleManager satisfies RuleManager for the RegisterBuiltins
// smoke test. Tracks call counts so we can assert behaviour without
// spinning up KV.
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
