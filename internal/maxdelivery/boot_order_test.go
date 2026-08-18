package maxdelivery

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBinaryBootOrder is a sister-binary guard: both production assemblies must
// complete the shared Phase-A client, effective-config, and stream-provisioning
// chain before starting the MaxDeliver observer or handing control to the
// function whose first action is Manager.StartAll.
func TestBinaryBootOrder(t *testing.T) {
	t.Parallel()

	productionPath := filepath.Join("..", "..", "cmd", "semstreams", "main.go")
	productionRun := functionDecl(t, productionPath, "run")
	productionCalls := functionCalls(t, productionPath, "run")
	requireCallOrder(t, productionCalls,
		"bootstrapobservability.NewProductionPhaseA",
		"connectToNATSWithSpinner",
		"bootstrapobservability.StartValidatedConfigManager",
		"ensureStreamsWithSpinner",
		"bootstrapobservability.NewForwardingHandler",
		"phaseLogging.Steady",
		"maxdelivery.Start",
		"setupRegistriesAndManager",
		"configureAndCreateServices",
		"runWithSignalHandling",
	)
	require.NotContains(t, productionCalls, "bootstrapobservability.NewE2EPhaseA")
	productionPhase, err := assignedCallResult(productionRun, "bootstrapobservability.NewProductionPhaseA", 1)
	require.NoError(t, err)
	productionMetrics, err := siblingAssignedResult(productionRun, productionPhase, 0)
	require.NoError(t, err)
	require.NoError(t, checkBootstrapDataflow(productionRun, productionPhase, productionMetrics,
		"connectToNATSWithSpinner", "bootstrapobservability.StartValidatedConfigManager", "ensureStreamsWithSpinner"))
	productionClient, err := assignedCallResult(productionRun, "connectToNATSWithSpinner", 0)
	require.NoError(t, err)
	productionEffective, err := assignedCallResult(
		productionRun, "bootstrapobservability.StartValidatedConfigManager", 1,
	)
	require.NoError(t, err)
	require.NoError(t, requireSelectorArgument(productionRun,
		"bootstrapobservability.NewForwardingHandler", 0, productionEffective, "Services"))
	require.NoError(t, requireIdentArgument(productionRun,
		"bootstrapobservability.NewForwardingHandler", 1, productionClient))
	require.NoError(t, requireSelectorArgument(productionRun,
		"bootstrapobservability.NewForwardingHandler", 2, productionPhase, "Process"))
	forwarding, err := assignedCallResult(productionRun, "bootstrapobservability.NewForwardingHandler", 0)
	require.NoError(t, err)
	require.NoError(t, requireIdentArgument(productionRun, "phaseLogging.Steady", 0, forwarding))
	requireConnectionChain(t, productionPath)
	requireStreamWrapper(t, productionPath)
	requireCompositionConstructors(t, productionPath)
	requireStartsManager(t, productionPath)

	e2ePath := filepath.Join("..", "..", "cmd", "e2e-semstreams", "main.go")
	e2eRun := functionDecl(t, e2ePath, "run")
	e2eCalls := functionCalls(t, e2ePath, "run")
	requireCallOrder(t, e2eCalls,
		"bootstrapobservability.NewE2EPhaseA",
		"completeE2EPhaseA",
		"maxdelivery.Start",
		"setupRegistriesAndManager",
		"configureAndCreateServices",
		"runWithSignalHandling",
	)
	require.NotContains(t, e2eCalls, "bootstrapobservability.NewProductionPhaseA")
	e2ePhase, err := assignedCallResult(e2eRun, "bootstrapobservability.NewE2EPhaseA", 1)
	require.NoError(t, err)
	e2eMetrics, err := siblingAssignedResult(e2eRun, e2ePhase, 0)
	require.NoError(t, err)
	require.NoError(t, requireIdentArgument(e2eRun, "completeE2EPhaseA", 2, e2ePhase))
	require.NoError(t, requireIdentArgument(e2eRun, "completeE2EPhaseA", 3, e2eMetrics))
	e2eComplete := functionDecl(t, e2ePath, "completeE2EPhaseA")
	e2ePhaseParam, err := parameterName(e2eComplete, 2)
	require.NoError(t, err)
	e2eMetricsParam, err := parameterName(e2eComplete, 3)
	require.NoError(t, err)
	require.NoError(t, checkBootstrapDataflow(e2eComplete, e2ePhaseParam, e2eMetricsParam,
		"connectToNATSWithSpinner", "bootstrapobservability.StartValidatedConfigManager", "ensureStreamsWithSpinner"))
	require.NoError(t, requireIdentArgument(e2eComplete, "phaseLogging.Steady", 0, "nil"))
	e2ePhaseCalls := functionCalls(t, e2ePath, "completeE2EPhaseA")
	requireCallOrder(t, e2ePhaseCalls,
		"connectToNATSWithSpinner",
		"bootstrapobservability.StartValidatedConfigManager",
		"ensureStreamsWithSpinner",
		"phaseLogging.Steady",
	)
	require.NotContains(t, e2eCalls, "bootstrapobservability.NewForwardingHandler")
	require.NotContains(t, e2ePhaseCalls, "bootstrapobservability.NewForwardingHandler")
	requireConnectionChain(t, e2ePath)
	requireStreamWrapper(t, e2ePath)
	requireCompositionConstructors(t, e2ePath)
	requireStartsManager(t, e2ePath)

	sharedPath := filepath.Join("..", "bootstrapobservability", "bootstrap.go")
	requireCallOrder(t, functionCalls(t, sharedPath, "NewProductionPhaseA"),
		"metric.NewMetricsRegistry", "NewLocalHandler", "NewPhaseALogging")
	require.Contains(t, functionCalls(t, sharedPath, "NewProductionPhaseA"), "logging.NewCounterHandler")
	requireCallOrder(t, functionCalls(t, sharedPath, "NewE2EPhaseA"),
		"metric.NewMetricsRegistry", "NewLocalHandler", "NewPhaseALogging")
	require.NotContains(t, functionCalls(t, sharedPath, "NewE2EPhaseA"), "logging.NewCounterHandler")
	requireCallOrder(t, functionCalls(t, sharedPath, "ConnectClient"),
		"client.Connect", "client.WaitForConnection")
	validatedConfigCalls := functionCalls(t, sharedPath, "StartValidatedConfigManager")
	requireCallOrder(t, validatedConfigCalls, "StartConfigManager", "ValidateEffectiveConfig")
	validatedConfig := functionDecl(t, sharedPath, "StartValidatedConfigManager")
	validatedManager, err := assignedCallResult(validatedConfig, "StartConfigManager", 0)
	require.NoError(t, err)
	validatedEffective, err := assignedCallResult(validatedConfig, "StartConfigManager", 1)
	require.NoError(t, err)
	require.NoError(t, requireIdentArgument(validatedConfig, "ValidateEffectiveConfig", 0, validatedEffective))
	require.NoError(t, requireReturnIdentifiers(validatedConfig, validatedManager, validatedEffective, "nil"))
	requireCallOrder(t, functionCalls(t, sharedPath, "StartConfigManager"),
		"config.NewConfigManager", "manager.Start", "manager.GetConfig")
	requireCallOrder(t, functionCalls(t, sharedPath, "ValidateEffectiveConfig"),
		"cfg.Validate", "rulepackcap.ValidateConfig", "graphresearch.ValidateConfig")
	streamCalls := functionCalls(t, sharedPath, "EnsureEffectiveStreams")
	requireCallOrder(t, streamCalls,
		"config.NewStreamsManager", "manager.VerifyJetStreamLimits", "manager.EnsureStreams")
	require.NotContains(t, streamCalls, "maxdelivery.EnsureCaptureStream")
}

func TestBootOrderDataflowRejectsMutations(t *testing.T) {
	t.Parallel()

	const valid = `package fixture
func run() {
	metrics, phase, err := bootstrapobservability.NewProductionPhaseA()
	client, err := connect(ctx, cfg, phase.Client, metrics)
	manager, effective, err := start(ctx, cfg, client, phase.ConfigManager)
	err = streams(ctx, effective, client, phase.ConfigManager)
	forwarding, err := bootstrapobservability.NewForwardingHandler()
	logger := phase.Steady(forwarding)
	_, _, _, _, _, _ = err, manager, effective, client, logger, metrics
}`

	mutations := []struct {
		name    string
		old     string
		new     string
		message string
	}{
		{name: "client logger", old: "phase.Client", new: "phase.Process", message: "connect logger"},
		{name: "config logger", old: "phase.ConfigManager", new: "phase.Process", message: "start logger"},
		{name: "effective config", old: "streams(ctx, effective", new: "streams(ctx, cfg", message: "stream config"},
		{name: "shared client", old: "effective, client", new: "effective, otherClient", message: "stream client"},
		{name: "steady destination", old: "phase.Steady(forwarding)", new: "phase.Steady(nil)", message: "argument"},
	}
	for _, mutation := range mutations {
		mutation := mutation
		t.Run(mutation.name, func(t *testing.T) {
			t.Parallel()
			fn := parseFunctionSource(t, strings.Replace(valid, mutation.old, mutation.new, 1), "run")
			phase, err := assignedCallResult(fn, "bootstrapobservability.NewProductionPhaseA", 1)
			require.NoError(t, err)
			metrics, metricsErr := siblingAssignedResult(fn, phase, 0)
			require.NoError(t, metricsErr)
			if mutation.name == "steady destination" {
				forwarding, assignErr := assignedCallResult(fn, "bootstrapobservability.NewForwardingHandler", 0)
				require.NoError(t, assignErr)
				err = requireIdentArgument(fn, "phase.Steady", 0, forwarding)
			} else {
				err = checkBootstrapDataflow(fn, phase, metrics, "connect", "start", "streams")
			}
			require.ErrorContains(t, err, mutation.message)
		})
	}
}

func TestRemainingBootDataflowRejectsMutations(t *testing.T) {
	t.Parallel()

	t.Run("E2E metrics handoff", func(t *testing.T) {
		const source = `package fixture
func run() {
	metrics, phase, err := bootstrapobservability.NewE2EPhaseA()
	result, err := complete(ctx, cfg, phase, otherMetrics)
	_, _, _ = metrics, result, err
}`
		fn := parseFunctionSource(t, source, "run")
		phase, err := assignedCallResult(fn, "bootstrapobservability.NewE2EPhaseA", 1)
		require.NoError(t, err)
		metrics, err := siblingAssignedResult(fn, phase, 0)
		require.NoError(t, err)
		require.Error(t, requireIdentArgument(fn, "complete", 3, metrics))
	})

	t.Run("validated effective config", func(t *testing.T) {
		const valid = `package fixture
func startValidated() (any, any, error) {
	manager, effective, err := StartConfigManager()
	if err := ValidateEffectiveConfig(effective); err != nil { return nil, nil, err }
	return manager, effective, nil
}`
		mutations := []struct {
			name string
			old  string
			new  string
		}{
			{name: "validation input", old: "ValidateEffectiveConfig(effective)", new: "ValidateEffectiveConfig(initial)"},
			{name: "returned config", old: "return manager, effective, nil", new: "return manager, initial, nil"},
		}
		for _, mutation := range mutations {
			mutation := mutation
			t.Run(mutation.name, func(t *testing.T) {
				fn := parseFunctionSource(t, strings.Replace(valid, mutation.old, mutation.new, 1), "startValidated")
				manager, err := assignedCallResult(fn, "StartConfigManager", 0)
				require.NoError(t, err)
				effective, err := assignedCallResult(fn, "StartConfigManager", 1)
				require.NoError(t, err)
				if mutation.name == "validation input" {
					require.Error(t, requireIdentArgument(fn, "ValidateEffectiveConfig", 0, effective))
					return
				}
				require.Error(t, requireReturnIdentifiers(fn, manager, effective, "nil"))
			})
		}
	})

	t.Run("forwarder inputs", func(t *testing.T) {
		const valid = `package fixture
func run() {
	client, err := connect()
	manager, effective, err := start()
	forwarding, err := bootstrapobservability.NewForwardingHandler(effective.Services, client, phase.Process)
	_, _, _, _ = err, manager, forwarding, phase
}`
		mutations := []struct {
			name string
			old  string
			new  string
		}{
			{name: "services", old: "effective.Services", new: "initial.Services"},
			{name: "client", old: ", client,", new: ", otherClient,"},
			{name: "logger", old: "phase.Process", new: "otherPhase.Process"},
		}
		for _, mutation := range mutations {
			mutation := mutation
			t.Run(mutation.name, func(t *testing.T) {
				fn := parseFunctionSource(t, strings.Replace(valid, mutation.old, mutation.new, 1), "run")
				client, err := assignedCallResult(fn, "connect", 0)
				require.NoError(t, err)
				effective, err := assignedCallResult(fn, "start", 1)
				require.NoError(t, err)
				checks := []error{
					requireSelectorArgument(fn, "bootstrapobservability.NewForwardingHandler", 0, effective, "Services"),
					requireIdentArgument(fn, "bootstrapobservability.NewForwardingHandler", 1, client),
					requireSelectorArgument(fn, "bootstrapobservability.NewForwardingHandler", 2, "phase", "Process"),
				}
				require.Error(t, checks[map[string]int{"services": 0, "client": 1, "logger": 2}[mutation.name]])
			})
		}
	})
}

func requireConnectionChain(t *testing.T, path string) {
	t.Helper()
	requireCallOrder(t, functionCalls(t, path, "connectToNATSWithSpinner"),
		"createNATSClient", "bootstrapobservability.ConnectClient")
	require.Contains(t, functionCalls(t, path, "createNATSClient"), "bootstrapobservability.NewClient")
}

func requireStreamWrapper(t *testing.T, path string) {
	t.Helper()
	ensureCalls := functionCalls(t, path, "ensureStreamsWithSpinner")
	require.Contains(t, ensureCalls, "bootstrapobservability.EnsureEffectiveStreams")
	require.NotContains(t, ensureCalls, "maxdelivery.EnsureCaptureStream")
}

func requireCompositionConstructors(t *testing.T, path string) {
	t.Helper()
	registryCalls := functionCalls(t, path, "setupRegistriesAndManager")
	require.Contains(t, registryCalls, "component.NewRegistry")
	require.Contains(t, registryCalls, "service.NewServiceManager")
	require.Contains(t, functionCalls(t, path, "configureAndCreateServices"), "manager.ConfigureFromServices")
}

func requireStartsManager(t *testing.T, path string) {
	t.Helper()
	require.Contains(t, functionCalls(t, path, "runWithSignalHandling"), "runUntilShutdown")
	require.Contains(t, functionCalls(t, path, "runUntilShutdown"), "manager.StartAll")
}

func checkBootstrapDataflow(
	fn *ast.FuncDecl,
	phase, metrics, connectCall, configCall, streamCall string,
) error {
	client, err := assignedCallResult(fn, connectCall, 0)
	if err != nil {
		return err
	}
	if err := requireSelectorArgument(fn, connectCall, 2, phase, "Client"); err != nil {
		return fmt.Errorf("connect logger: %w", err)
	}
	if err := requireIdentArgument(fn, connectCall, 3, metrics); err != nil {
		return fmt.Errorf("connect metrics: %w", err)
	}
	effective, err := assignedCallResult(fn, configCall, 1)
	if err != nil {
		return err
	}
	if err := requireIdentArgument(fn, configCall, 2, client); err != nil {
		return fmt.Errorf("config client: %w", err)
	}
	if err := requireSelectorArgument(fn, configCall, 3, phase, "ConfigManager"); err != nil {
		return fmt.Errorf("start logger: %w", err)
	}
	if err := requireIdentArgument(fn, streamCall, 1, effective); err != nil {
		return fmt.Errorf("stream config: %w", err)
	}
	if err := requireIdentArgument(fn, streamCall, 2, client); err != nil {
		return fmt.Errorf("stream client: %w", err)
	}
	if err := requireSelectorArgument(fn, streamCall, 3, phase, "ConfigManager"); err != nil {
		return fmt.Errorf("stream logger: %w", err)
	}
	return nil
}

func siblingAssignedResult(fn *ast.FuncDecl, sibling string, index int) (string, error) {
	var result string
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || index >= len(assignment.Lhs) || len(assignment.Rhs) != 1 {
			return true
		}
		containsSibling := false
		for _, lhs := range assignment.Lhs {
			ident, identOK := lhs.(*ast.Ident)
			if identOK && ident.Name == sibling {
				containsSibling = true
				break
			}
		}
		if !containsSibling {
			return true
		}
		ident, ok := assignment.Lhs[index].(*ast.Ident)
		if ok {
			result = ident.Name
		}
		return false
	})
	if result == "" {
		return "", fmt.Errorf("result %d beside %s not assigned to an identifier", index, sibling)
	}
	return result, nil
}

func assignedCallResult(fn *ast.FuncDecl, target string, index int) (string, error) {
	var result string
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Rhs) != 1 {
			return true
		}
		call, ok := assignment.Rhs[0].(*ast.CallExpr)
		if !ok || callName(call.Fun) != target {
			return true
		}
		if index >= len(assignment.Lhs) {
			return false
		}
		ident, ok := assignment.Lhs[index].(*ast.Ident)
		if ok {
			result = ident.Name
		}
		return false
	})
	if result == "" {
		return "", fmt.Errorf("result %d from %s not assigned to an identifier", index, target)
	}
	return result, nil
}

func requireSelectorArgument(
	fn *ast.FuncDecl,
	target string,
	index int,
	base, selector string,
) error {
	argument, err := callArgument(fn, target, index)
	if err != nil {
		return err
	}
	selected, ok := argument.(*ast.SelectorExpr)
	if !ok {
		return fmt.Errorf("argument %d to %s is not %s.%s", index, target, base, selector)
	}
	ident, ok := selected.X.(*ast.Ident)
	if !ok || ident.Name != base || selected.Sel.Name != selector {
		return fmt.Errorf("argument %d to %s is not %s.%s", index, target, base, selector)
	}
	return nil
}

func requireIdentArgument(fn *ast.FuncDecl, target string, index int, want string) error {
	argument, err := callArgument(fn, target, index)
	if err != nil {
		return err
	}
	ident, ok := argument.(*ast.Ident)
	if !ok || ident.Name != want {
		return fmt.Errorf("argument %d to %s is not identifier %s", index, target, want)
	}
	return nil
}

func requireReturnIdentifiers(fn *ast.FuncDecl, want ...string) error {
	matched := false
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		result, ok := node.(*ast.ReturnStmt)
		if !ok || len(result.Results) != len(want) {
			return true
		}
		for index, expression := range result.Results {
			ident, identOK := expression.(*ast.Ident)
			if !identOK || ident.Name != want[index] {
				return true
			}
		}
		matched = true
		return false
	})
	if !matched {
		return fmt.Errorf("return identifiers %v not found", want)
	}
	return nil
}

func callArgument(fn *ast.FuncDecl, target string, index int) (ast.Expr, error) {
	var argument ast.Expr
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || callName(call.Fun) != target {
			return true
		}
		if index < len(call.Args) {
			argument = call.Args[index]
		}
		return false
	})
	if argument == nil {
		return nil, fmt.Errorf("argument %d to %s not found", index, target)
	}
	return argument, nil
}

func parameterName(fn *ast.FuncDecl, index int) (string, error) {
	position := 0
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			if position == index {
				return name.Name, nil
			}
			position++
		}
	}
	return "", fmt.Errorf("parameter %d not found", index)
}

func functionCalls(t *testing.T, path, function string) []string {
	t.Helper()
	fn := functionDecl(t, path, function)
	return callsInFunction(fn)
}

func functionDecl(t *testing.T, path, function string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	require.NoError(t, err)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function || fn.Body == nil {
			continue
		}
		return fn
	}
	t.Fatalf("function %s not found in %s", function, path)
	return nil
}

func parseFunctionSource(t *testing.T, source, function string) *ast.FuncDecl {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", source, 0)
	require.NoError(t, err)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == function {
			return fn
		}
	}
	t.Fatalf("function %s not found in fixture", function)
	return nil
}

func callsInFunction(fn *ast.FuncDecl) []string {
	var calls []string
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if name := callName(call.Fun); name != "" {
			calls = append(calls, name)
		}
		return true
	})
	return calls
}

func callName(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.SelectorExpr:
		if base, ok := value.X.(*ast.Ident); ok {
			return base.Name + "." + value.Sel.Name
		}
	}
	return ""
}

func requireCallOrder(t *testing.T, calls []string, ordered ...string) {
	t.Helper()
	position := -1
	for _, want := range ordered {
		found := -1
		for i := position + 1; i < len(calls); i++ {
			if calls[i] == want {
				found = i
				break
			}
		}
		require.NotEqualf(t, -1, found, "call %s absent or out of order in %v", want, calls)
		position = found
	}
}
