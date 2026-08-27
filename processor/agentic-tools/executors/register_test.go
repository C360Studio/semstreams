package executors

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/c360studio/semstreams/agentic"
	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/projection"
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
	if !containsName(names, "list_rules") {
		t.Errorf("registerRules should fire even with nil NATSClient; %q not found", "list_rules")
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

type noOpPredicateReconciler struct{}

func (noOpPredicateReconciler) Reconcile(
	context.Context,
	projection.ReconcileMutation,
) (projection.MutationReceipt, error) {
	return projection.MutationReceipt{Commit: projection.CommitVerified}, nil
}

func TestRegisterWriteTodosRequiresProjectionClient(t *testing.T) {
	t.Parallel()
	registry := agentictools.NewExecutorRegistry()
	if err := registerWriteTodos(
		registry, nil, component.PlatformMeta{}, slog.Default(),
	); err == nil {
		t.Fatal("nil projection client must block write_todos registration")
	}
	if len(registry.ListTools()) != 0 {
		t.Fatal("nil projection client registered write_todos")
	}
}

func TestRegisterWriteTodosUsesProjectionCapability(t *testing.T) {
	t.Parallel()
	registry := agentictools.NewExecutorRegistry()
	if err := registerWriteTodos(
		registry,
		noOpPredicateReconciler{},
		component.PlatformMeta{Org: "acme", Platform: "test"},
		slog.Default(),
	); err != nil {
		t.Fatalf("registerWriteTodos: %v", err)
	}
	if !containsName(registry.ListTools(), agentictools.WriteTodosToolName) {
		t.Fatal("write_todos was not registered")
	}
}

func TestRegisterBuiltins_WriteTodosRequiresMutationClient(t *testing.T) {
	t.Parallel()
	registry := agentictools.NewExecutorRegistry()

	err := RegisterBuiltins(context.Background(), registry, writeTodosOnlyDependencies(nil))
	if err == nil {
		t.Fatal("nil mutation client must block write_todos registration")
	}
	if !errors.Is(err, errWriteTodosMutationClientRequired) {
		t.Fatalf("RegisterBuiltins error = %q, want missing mutation client error", err)
	}
	if containsName(registry.ListTools(), agentictools.WriteTodosToolName) {
		t.Fatal("nil mutation client registered write_todos")
	}
}

func TestRegisterBuiltins_WriteTodosUsesMutationClient(t *testing.T) {
	t.Parallel()
	registry := agentictools.NewExecutorRegistry()

	if err := RegisterBuiltins(
		context.Background(),
		registry,
		writeTodosOnlyDependencies(&projection.MutationClient{}),
	); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	if !containsName(registry.ListTools(), agentictools.WriteTodosToolName) {
		t.Fatal("non-nil mutation client did not register write_todos")
	}
}

func TestRegisterBuiltins_SkipWriteTodosDoesNotRequireMutationClient(t *testing.T) {
	t.Parallel()
	registry := agentictools.NewExecutorRegistry()
	deps := writeTodosOnlyDependencies(nil)
	deps.SkipBuiltins = append(deps.SkipBuiltins, "write_todos")

	if err := RegisterBuiltins(context.Background(), registry, deps); err != nil {
		t.Fatalf("RegisterBuiltins with write_todos skipped: %v", err)
	}
	if containsName(registry.ListTools(), agentictools.WriteTodosToolName) {
		t.Fatal("SkipBuiltins registered write_todos")
	}
}

func writeTodosOnlyDependencies(mutations *projection.MutationClient) ToolDependencies {
	skip := make([]string, 0, len(BuiltinGroupKeys)-1)
	for _, key := range BuiltinGroupKeys {
		if key != "write_todos" {
			skip = append(skip, key)
		}
	}
	return ToolDependencies{
		// A non-nil NATS dependency drives RegisterBuiltins through its
		// stateful registration branch. The skipped groups keep this test
		// focused on write_todos and do not require a live connection.
		NATSClient:     &natsclient.Client{},
		MutationClient: mutations,
		Logger:         slog.Default(),
		SkipBuiltins:   skip,
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

// TestRegisterBuiltins_SkipBuiltins_BashOmitted is the canonical
// semteams use case: the product shell wants to register its own
// chain-scoped `bash` implementation under the canonical name. Setting
// SkipBuiltins=["bash"] omits the framework bash; the product shell's
// subsequent RegisterTool("bash", custom) succeeds because the slot
// is empty.
func TestRegisterBuiltins_SkipBuiltins_BashOmitted(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()

	err := RegisterBuiltins(context.Background(), reg, ToolDependencies{
		Logger:       slog.Default(),
		SkipBuiltins: []string{"bash"},
	})
	if err != nil {
		t.Fatalf("RegisterBuiltins with SkipBuiltins=[bash]: %v", err)
	}

	tools := reg.ListTools()
	if containsName(tools, "bash") {
		t.Errorf("bash should be skipped, but is registered")
	}
	// Sanity-check: other builtins still register.
	if !containsName(tools, "web_search") {
		t.Errorf("web_search should still register when only bash is skipped")
	}
	if !containsName(tools, "http_request") {
		t.Errorf("http_request should still register when only bash is skipped")
	}

	// Product-shell can now register a replacement under the canonical name.
	stub := &registerTestStubExecutor{name: "bash"}
	if err := reg.RegisterTool("bash", stub); err != nil {
		t.Fatalf("product-shell bash replacement should succeed after SkipBuiltins[bash]: %v", err)
	}
}

// TestRegisterBuiltins_SkipBuiltins_MultiToolGroup verifies that a
// group-key skip (e.g., "graph_query") omits ALL tools the register
// function would have advertised — necessary because RegisterExecutor
// is atomic over an executor's full ListTools().
func TestRegisterBuiltins_SkipBuiltins_MultiToolGroup(t *testing.T) {
	t.Parallel()
	// NATS-required tools skip when NATSClient is nil, so pass a non-nil
	// path via the same skip mechanism: skip graph_query and verify that
	// query_entity / query_relationships / etc. are absent. We test
	// without NATSClient first to verify the skip semantics are
	// orthogonal to the NATSClient-nil skip path.
	t.Run("group_skip_with_nil_nats_no_op", func(t *testing.T) {
		t.Parallel()
		reg := agentictools.NewExecutorRegistry()
		err := RegisterBuiltins(context.Background(), reg, ToolDependencies{
			Logger:       slog.Default(),
			SkipBuiltins: []string{"graph_query"},
		})
		if err != nil {
			t.Fatalf("RegisterBuiltins: %v", err)
		}
		// With NATSClient=nil, graph_query wouldn't fire anyway; skip is a no-op.
		// query_entity should be absent either way.
		if containsName(reg.ListTools(), "query_entity") {
			t.Errorf("query_entity should not register when NATSClient is nil regardless of skip")
		}
	})
}

// TestRegisterBuiltins_SkipBuiltins_MultipleNames verifies that
// multiple skip entries compose: all named groups are absent and
// everything else registers as usual.
func TestRegisterBuiltins_SkipBuiltins_MultipleNames(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	err := RegisterBuiltins(context.Background(), reg, ToolDependencies{
		Logger:       slog.Default(),
		SkipBuiltins: []string{"bash", "web_search"},
	})
	if err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	tools := reg.ListTools()
	for _, omitted := range []string{"bash", "web_search"} {
		if containsName(tools, omitted) {
			t.Errorf("%q should be skipped", omitted)
		}
	}
	// http_request not in skip list — should register
	if !containsName(tools, "http_request") {
		t.Errorf("http_request should still register")
	}
}

// TestRegisterBuiltins_SkipBuiltins_UnknownNameErrors is the
// loud-typo-catching contract: an unknown group key must error before
// any registration happens, with the valid set in the message so
// operators can self-correct without grepping.
func TestRegisterBuiltins_SkipBuiltins_UnknownNameErrors(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	err := RegisterBuiltins(context.Background(), reg, ToolDependencies{
		Logger:       slog.Default(),
		SkipBuiltins: []string{"bahs"}, // typo for "bash"
	})
	if err == nil {
		t.Fatalf("unknown SkipBuiltins entry must error, got nil")
	}
	if !strings.Contains(err.Error(), "bahs") {
		t.Errorf("error must reference the unknown name, got: %v", err)
	}
	if !strings.Contains(err.Error(), "valid keys") {
		t.Errorf("error must reference the valid-keys list, got: %v", err)
	}
	// No registration should have happened.
	if len(reg.ListTools()) != 0 {
		t.Errorf("registry must be empty on validation failure, got %d tools", len(reg.ListTools()))
	}
}

// TestRegisterBuiltins_SkipBuiltins_EmptyNoOp pins backward-compat:
// nil or empty SkipBuiltins must produce identical behaviour to the
// pre-feature call (all builtins register).
func TestRegisterBuiltins_SkipBuiltins_EmptyNoOp(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	err := RegisterBuiltins(context.Background(), reg, ToolDependencies{
		Logger:       slog.Default(),
		SkipBuiltins: nil,
	})
	if err != nil {
		t.Fatalf("nil SkipBuiltins: %v", err)
	}
	if !containsName(reg.ListTools(), "bash") {
		t.Errorf("bash should register when SkipBuiltins is nil")
	}

	reg2 := agentictools.NewExecutorRegistry()
	err = RegisterBuiltins(context.Background(), reg2, ToolDependencies{
		Logger:       slog.Default(),
		SkipBuiltins: []string{},
	})
	if err != nil {
		t.Fatalf("empty SkipBuiltins: %v", err)
	}
	if !containsName(reg2.ListTools(), "bash") {
		t.Errorf("bash should register when SkipBuiltins is empty")
	}
}

// TestBuiltinGroupKeys_Stability locks the canonical set + ordering
// of BuiltinGroupKeys. External callers iterate this slice for
// validation/docs; reordering or renaming an entry is a BREAKING
// change worth flagging at PR review.
func TestBuiltinGroupKeys_Stability(t *testing.T) {
	t.Parallel()
	want := []string{
		"bash", "web_search", "http_request",
		"read_loop_result", "decide", "emit_diagnosis", "emit_lesson",
		"write_todos", "scratchpad",
		"graph_query",
		"rules", "personas",
		"component_catalog",
	}
	if len(BuiltinGroupKeys) != len(want) {
		t.Fatalf("BuiltinGroupKeys count = %d, want %d. Adding a builtin? Update this test AND consumer docs.", len(BuiltinGroupKeys), len(want))
	}
	for i, k := range want {
		if BuiltinGroupKeys[i] != k {
			t.Errorf("BuiltinGroupKeys[%d] = %q, want %q. Order matters for golden-test reproducibility.", i, BuiltinGroupKeys[i], k)
		}
	}
}

// TestBuiltins_EmitLessonWriteOnly_NoLessonSearchTool is the enumeration oracle
// for the spec scenario "No dedicated lesson search tool exists" (ADR-080
// decision 2: memory is PUSH, not pull). The built-in set MUST contain
// emit_lesson (the sole agent-facing lesson WRITE path) and MUST NOT contain any
// dedicated lesson search/list/query tool — lessons reach agents by bounded
// deterministic brief injection, never a pull tool. Generic graph-read tools
// in the query_* family remain, governed by per-role allowlists.
func TestBuiltins_EmitLessonWriteOnly_NoLessonSearchTool(t *testing.T) {
	t.Parallel()

	found := false
	for _, k := range BuiltinGroupKeys {
		if k == "emit_lesson" {
			found = true
		}
		// Any lesson-named builtin OTHER than the emit (write) tool would be a
		// dedicated lesson read surface — forbidden by ADR-080 decision 2.
		if strings.Contains(strings.ToLower(k), "lesson") && k != "emit_lesson" {
			t.Errorf("built-in tool group %q is a dedicated lesson tool other than emit_lesson; memory is push-only, no lesson search/list/query tool", k)
		}
	}
	if !found {
		t.Errorf("built-in tool groups must contain emit_lesson (the lesson write path); got %v", BuiltinGroupKeys)
	}
}

func TestCoreToolGroupsExcludeProductAndCapabilityTools(t *testing.T) {
	t.Parallel()
	for _, forbidden := range []string{"github", "research_graph"} {
		for _, group := range BuiltinGroupKeys {
			if group == forbidden {
				t.Errorf("core tool groups unexpectedly contain %q", forbidden)
			}
		}
	}
	for _, required := range []string{"graph_query"} {
		found := false
		for _, group := range BuiltinGroupKeys {
			if group == required {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("core tool groups are missing direct graph group %q", required)
		}
	}
}

// retiredFlowToolNames is the eleven-tool set ADR-100 decision D5 removes,
// spelled out rather than derived: a list derived from the registration table
// that registers them would agree with itself after the table was emptied AND
// after it was refilled.
var retiredFlowToolNames = []string{
	"create_flow", "update_flow", "delete_flow", "list_flows", "get_flow",
	"create_flow_template", "update_flow_template", "delete_flow_template",
	"list_flow_templates", "get_flow_template", "instantiate_flow_template",
}

// TestToolRegistryHasNoFlowTools is the absence guard for ADR-100 D5 on the
// agent-facing surface. It drives RegisterBuiltins with every dependency the
// framework still has non-nil, so no gate is skipped for want of a manager and
// the maximal registration is what gets inspected. Agents reach compositions
// through the read-only catalog / validate / graph verbs instead
// (composition-validation).
func TestToolRegistryHasNoFlowTools(t *testing.T) {
	t.Parallel()
	reg := agentictools.NewExecutorRegistry()
	err := RegisterBuiltins(context.Background(), reg, ToolDependencies{
		NATSClient:        &natsclient.Client{},
		MutationClient:    &projection.MutationClient{},
		Logger:            slog.Default(),
		RuleManager:       &recordingRuleManager{},
		PersonaManager:    newMockPersonaManager(),
		ComponentRegistry: component.NewRegistry(),
	})
	if err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}

	registered := reg.ListTools()
	for _, name := range retiredFlowToolNames {
		if containsName(registered, name) {
			t.Errorf("tool registry still advertises %q; ADR-100 D5 removes it without an alias", name)
		}
	}

	// Name-shaped backstop: a re-entry under a spelling the list above does not
	// carry is still the retired surface returning.
	for _, tool := range registered {
		lowered := strings.ToLower(tool.Name)
		if strings.Contains(lowered, "_flow") || strings.HasSuffix(lowered, "_flows") {
			t.Errorf("tool %q names the retired flow-authoring surface", tool.Name)
		}
	}

	for _, key := range BuiltinGroupKeys {
		if key == "flows" || key == "flow_templates" {
			t.Errorf("BuiltinGroupKeys still carries %q; the group it skipped no longer exists", key)
		}
	}
}
