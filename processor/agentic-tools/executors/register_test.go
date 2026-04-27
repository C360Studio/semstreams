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

// TestRegisterRules_NilManagerSkips — every Pattern-B wire function must
// treat a nil manager as an intentional skip (deployment choice), not an
// error. The registry must be empty afterwards and the function must
// return nil so RegisterBuiltins doesn't aggregate a spurious error.
func TestRegisterRules_NilManagerSkips(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	if err := registerRules(reg, nil, slog.Default()); err != nil {
		t.Fatalf("registerRules(nil) should be a clean skip, got err: %v", err)
	}
	if got := len(reg.ListTools()); got != 0 {
		t.Fatalf("registerRules(nil) registered %d tools, want 0", got)
	}
}

func TestRegisterFlows_NilManagerSkips(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	if err := registerFlows(reg, nil, slog.Default()); err != nil {
		t.Fatalf("registerFlows(nil) should be a clean skip, got err: %v", err)
	}
	if got := len(reg.ListTools()); got != 0 {
		t.Fatalf("registerFlows(nil) registered %d tools, want 0", got)
	}
}

func TestRegisterPersonas_NilManagerSkips(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	if err := registerPersonas(reg, nil, slog.Default()); err != nil {
		t.Fatalf("registerPersonas(nil) should be a clean skip, got err: %v", err)
	}
	if got := len(reg.ListTools()); got != 0 {
		t.Fatalf("registerPersonas(nil) registered %d tools, want 0", got)
	}
}

func TestRegisterFlowTemplates_NilManagerSkips(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	if err := registerFlowTemplates(reg, nil, slog.Default()); err != nil {
		t.Fatalf("registerFlowTemplates(nil) should be a clean skip, got err: %v", err)
	}
	if got := len(reg.ListTools()); got != 0 {
		t.Fatalf("registerFlowTemplates(nil) registered %d tools, want 0", got)
	}
}

// TestRegisterBuiltins_DuplicateNameAggregatesError — boot-time
// duplicate-name collisions must return an error to the caller (main),
// not be silently swallowed. Pre-registering "bash" on the same
// registry forces registerBash to collide; RegisterBuiltins must
// surface the collision in its returned error.
func TestRegisterBuiltins_DuplicateNameAggregatesError(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()

	// Pre-occupy "bash" so registerBash will collide.
	stub := &registerTestStubExecutor{name: "bash"}
	if err := reg.RegisterTool("bash", stub); err != nil {
		t.Fatalf("preload bash: %v", err)
	}

	err := RegisterBuiltins(context.Background(), reg, ToolDependencies{
		Logger: slog.Default(),
	})
	if err == nil {
		t.Fatalf("RegisterBuiltins should return an error when a builtin name collides")
	}
	// The aggregate must mention the colliding tool name so operators
	// can diagnose without re-running with verbose logging.
	if !strings.Contains(err.Error(), "bash") {
		t.Fatalf("aggregated error should reference the colliding tool name, got: %v", err)
	}
}

// registerTestStubExecutor is a minimal ToolExecutor for the collision
// smoke test. Does no work; the registration check is what's exercised.
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
