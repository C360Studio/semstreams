package natsclient

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
)

type recordingClientCloser struct {
	calls int
	close func(context.Context) error
}

func (r *recordingClientCloser) Close(ctx context.Context) error {
	r.calls++
	return r.close(ctx)
}

type recordingContainerTerminator struct {
	calls     int
	terminate func(context.Context) error
}

func (r *recordingContainerTerminator) Terminate(
	ctx context.Context,
	_ ...testcontainers.TerminateOption,
) error {
	r.calls++
	return r.terminate(ctx)
}

func TestNATSReadyWaitStrategy_UsesInternalServerSignal(t *testing.T) {
	t.Parallel()

	const startupTimeout = 37 * time.Second
	strategy := natsReadyWaitStrategy(startupTimeout)

	if strategy.Log != "Server is ready" {
		t.Fatalf("readiness log = %q, want %q", strategy.Log, "Server is ready")
	}
	if strategy.IsRegexp {
		t.Fatal("readiness must use the exact NATS log text, not a broader regular expression")
	}
	if strategy.Occurrence != 1 {
		t.Fatalf("readiness occurrence = %d, want 1", strategy.Occurrence)
	}
	if timeout := strategy.Timeout(); timeout == nil || *timeout != startupTimeout {
		t.Fatalf("startup timeout = %v, want %s", timeout, startupTimeout)
	}
}

func TestDefaultTestContainerStartTimeout_IsThirtySeconds(t *testing.T) {
	t.Parallel()

	if defaultTestContainerStartTimeout != 30*time.Second {
		t.Fatalf("default startup timeout = %s, want 30s", defaultTestContainerStartTimeout)
	}
}

func TestRequiredPortObservationBudget_IsTenSeconds(t *testing.T) {
	t.Parallel()

	if requiredPortObservationBudget != 10*time.Second {
		t.Fatalf("required-port observation budget = %s, want 10s", requiredPortObservationBudget)
	}
}

func TestTestContainerRequest_MonitoringIsExplicitCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		opts        []TestOption
		wantCommand []string
		wantPorts   []string
	}{
		{
			name:        "default exposes only client port",
			wantCommand: []string{"--port", "4222"},
			wantPorts:   []string{requiredClientPort},
		},
		{
			name:        "monitoring adds command and exposed port",
			opts:        []TestOption{WithMonitoring()},
			wantCommand: []string{"--port", "4222", "--http_port", "8222"},
			wantPorts:   []string{requiredClientPort, requiredMonitoringPort},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := defaultTestConfig()
			for _, opt := range tt.opts {
				opt(cfg)
			}
			request := newTestContainerRequest(cfg).ContainerRequest
			if !slices.Equal(request.Cmd, tt.wantCommand) {
				t.Fatalf("command = %v, want %v", request.Cmd, tt.wantCommand)
			}
			if !slices.Equal(request.ExposedPorts, tt.wantPorts) {
				t.Fatalf("exposed ports = %v, want %v", request.ExposedPorts, tt.wantPorts)
			}
			if !slices.Equal([]string(requiredPortsForConfig(cfg)), tt.wantPorts) {
				t.Fatalf("required ports = %v, want %v", requiredPortsForConfig(cfg), tt.wantPorts)
			}
		})
	}
}

func TestMonitoringURL_IsEmptyByDefaultAndUsableWhenEnabled(t *testing.T) {
	t.Parallel()

	resolved := requiredPortObservation{mappedPorts: map[string]string{
		requiredClientPort:     "14222",
		requiredMonitoringPort: "18222",
	}}
	if got := monitoringURLForConfig(defaultTestConfig(), "127.0.0.1", resolved); got != "" {
		t.Fatalf("default MonitoringURL = %q, want empty", got)
	}

	monitoringConfig := defaultTestConfig()
	WithMonitoring()(monitoringConfig)
	if got := monitoringURLForConfig(monitoringConfig, "127.0.0.1", resolved); got != "http://127.0.0.1:18222" {
		t.Fatalf("monitoring URL = %q, want usable mapped URL", got)
	}
}

func TestTestClientConstructorsConsumeSharedFactory(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test_client.go", nil, 0)
	if err != nil {
		t.Fatalf("parse test_client.go: %v", err)
	}

	for _, constructor := range []string{"NewSharedTestClient", "NewTestClient"} {
		var declaration *ast.FuncDecl
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Name.Name == constructor {
				declaration = fn
				break
			}
		}
		if declaration == nil {
			t.Fatalf("constructor %s not found", constructor)
		}

		factoryCalls := 0
		ast.Inspect(declaration.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && identifier.Name == "newTestClient" {
				factoryCalls++
			}
			return true
		})

		if factoryCalls != 1 {
			t.Fatalf("%s calls shared newTestClient factory %d times, want exactly 1", constructor, factoryCalls)
		}
	}

	var attemptDeclaration *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "runTestClientAttempt" {
			attemptDeclaration = fn
			break
		}
	}
	if attemptDeclaration == nil {
		t.Fatal("runTestClientAttempt not found")
	}

	containerStarts := 0
	ast.Inspect(attemptDeclaration.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) != 2 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "GenericContainer" {
			return true
		}
		builder, ok := call.Args[1].(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := builder.Fun.(*ast.Ident)
		if ok && identifier.Name == "newTestContainerRequest" {
			containerStarts++
		}
		return true
	})
	if containerStarts != 1 {
		t.Fatalf("runTestClientAttempt starts through newTestContainerRequest %d times, want exactly 1", containerStarts)
	}
}

func TestRequiredPortObserver_UsesOneInspectAndNoMappedPort(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test_client.go", nil, 0)
	if err != nil {
		t.Fatalf("parse test_client.go: %v", err)
	}

	var observerDeclaration *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "containerRequiredPortObserver" {
			observerDeclaration = fn
			break
		}
	}
	if observerDeclaration == nil {
		t.Fatal("containerRequiredPortObserver not found")
	}

	inspectCalls := 0
	mappedPortCalls := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if selector.Sel.Name == "MappedPort" {
			mappedPortCalls++
		}
		return true
	})
	ast.Inspect(observerDeclaration.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "Inspect" {
			inspectCalls++
		}
		return true
	})

	if inspectCalls != 1 {
		t.Fatalf("containerRequiredPortObserver Inspect calls = %d, want exactly 1", inspectCalls)
	}
	if mappedPortCalls != 0 {
		t.Fatalf("test_client.go MappedPort calls = %d, want none", mappedPortCalls)
	}
}

func TestTestClientContextRoots_SetupFollowsTestAndCleanupIsIndependent(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test_client.go", nil, 0)
	if err != nil {
		t.Fatalf("parse test_client.go: %v", err)
	}

	findFunction := func(name string) *ast.FuncDecl {
		t.Helper()
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Name.Name == name {
				return fn
			}
		}
		t.Fatalf("function %s not found", name)
		return nil
	}
	contextRoot := func(function string) string {
		t.Helper()
		root := ""
		ast.Inspect(findFunction(function).Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			factory, ok := call.Fun.(*ast.Ident)
			if !ok || factory.Name != "newTestClient" {
				return true
			}
			parent, ok := call.Args[0].(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := parent.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if ok {
				root = receiver.Name + "." + selector.Sel.Name
			}
			return true
		})
		return root
	}

	if got := contextRoot("NewTestClient"); got != "t.Context" {
		t.Fatalf("NewTestClient setup context root = %q, want t.Context", got)
	}
	if got := contextRoot("NewSharedTestClient"); got != "context.Background" {
		t.Fatalf("NewSharedTestClient setup context root = %q, want context.Background", got)
	}

	independentCleanupRoots := 0
	ast.Inspect(findFunction("cleanupTestInfrastructureWithin").Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "WithTimeout" {
			return true
		}
		background, ok := call.Args[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		backgroundSelector, ok := background.Fun.(*ast.SelectorExpr)
		if !ok || backgroundSelector.Sel.Name != "Background" {
			return true
		}
		receiver, ok := backgroundSelector.X.(*ast.Ident)
		if ok && receiver.Name == "context" {
			independentCleanupRoots++
		}
		return true
	})
	if independentCleanupRoots != 2 {
		t.Fatalf("cleanup independent Background roots = %d, want one each for client and container", independentCleanupRoots)
	}
}

func TestTestClientAttempt_BoundsResourceSetupToFreshConfiguredDeadline(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test_client.go", nil, 0)
	if err != nil {
		t.Fatalf("parse test_client.go: %v", err)
	}

	var declaration *ast.FuncDecl
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "runTestClientAttempt" {
			declaration = fn
			break
		}
	}
	if declaration == nil {
		t.Fatal("runTestClientAttempt not found")
	}

	contextAssignments := 0
	resourceCalls := map[string]int{"setupKVBuckets": 0, "setupStreams": 0}
	ast.Inspect(declaration.Body, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if ok && len(assignment.Lhs) == 2 && len(assignment.Rhs) == 1 {
			setupContext, contextOK := assignment.Lhs[0].(*ast.Ident)
			setupCancel, cancelOK := assignment.Lhs[1].(*ast.Ident)
			call, callOK := assignment.Rhs[0].(*ast.CallExpr)
			if contextOK && cancelOK && callOK && setupContext.Name == "resourceSetupCtx" &&
				setupCancel.Name == "resourceSetupCancel" && len(call.Args) == 2 {
				selector, selectorOK := call.Fun.(*ast.SelectorExpr)
				parent, parentOK := call.Args[0].(*ast.Ident)
				timeout, timeoutOK := call.Args[1].(*ast.SelectorExpr)
				cfg, cfgOK := timeout.X.(*ast.Ident)
				if selectorOK && selector.Sel.Name == "WithTimeout" && parentOK && parent.Name == "ctx" &&
					timeoutOK && timeout.Sel.Name == "timeout" && cfgOK && cfg.Name == "cfg" {
					contextAssignments++
				}
			}
		}

		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if _, tracked := resourceCalls[selector.Sel.Name]; !tracked {
			return true
		}
		ctx, ok := call.Args[0].(*ast.Ident)
		if ok && ctx.Name == "resourceSetupCtx" {
			resourceCalls[selector.Sel.Name]++
		}
		return true
	})

	if contextAssignments != 1 {
		t.Errorf("runTestClientAttempt fresh cfg.timeout resource context assignments = %d, want 1", contextAssignments)
	}
	for operation, calls := range resourceCalls {
		if calls != 1 {
			t.Errorf("runTestClientAttempt calls %s with resourceSetupCtx %d times, want 1", operation, calls)
		}
	}
}

func TestCleanupTestInfrastructure_PreservesCloseAndTerminateErrors(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("close failed")
	terminateErr := errors.New("terminate failed")
	client := &recordingClientCloser{close: func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("client close context must have a deadline")
		}
		return closeErr
	}}
	container := &recordingContainerTerminator{terminate: func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("container termination context must have a deadline")
		}
		return terminateErr
	}}

	err := cleanupTestInfrastructureWithin(client, container, 100*time.Millisecond)
	if !errors.Is(err, closeErr) || !errors.Is(err, terminateErr) {
		t.Fatalf("cleanup error = %v, want both close and terminate causes", err)
	}
	if client.calls != 1 || container.calls != 1 {
		t.Fatalf("cleanup calls = close:%d terminate:%d, want one each", client.calls, container.calls)
	}
}

func TestCleanupTestInfrastructure_GivesEachOperationItsOwnBudget(t *testing.T) {
	t.Parallel()

	const cleanupBudget = 50 * time.Millisecond
	client := &recordingClientCloser{close: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}
	container := &recordingContainerTerminator{terminate: func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			t.Fatalf("terminate inherited exhausted close deadline: %v", err)
		}
		<-ctx.Done()
		return ctx.Err()
	}}

	start := time.Now()
	err := cleanupTestInfrastructureWithin(client, container, cleanupBudget)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cleanup error = %v, want deadline cause", err)
	}
	if client.calls != 1 || container.calls != 1 {
		t.Fatalf("cleanup calls = close:%d terminate:%d, want one each", client.calls, container.calls)
	}
	if elapsed < cleanupBudget+cleanupBudget/2 || elapsed > time.Second {
		t.Fatalf("cleanup returned after %s, want two independent %s budgets", elapsed, cleanupBudget)
	}
}

func TestCleanupAfterTestClientSetupFailure_PreservesEveryCause(t *testing.T) {
	t.Parallel()

	setupErr := errors.New("setup failed")
	closeErr := errors.New("close failed")
	terminateErr := errors.New("terminate failed")
	client := &recordingClientCloser{close: func(context.Context) error { return closeErr }}
	container := &recordingContainerTerminator{
		terminate: func(context.Context) error { return terminateErr },
	}

	err := cleanupAfterTestClientSetupFailure(client, container, setupErr)
	for name, cause := range map[string]error{
		"setup":     setupErr,
		"close":     closeErr,
		"terminate": terminateErr,
	} {
		if !errors.Is(err, cause) {
			t.Errorf("%s cause was masked: %v", name, err)
		}
	}
}

func TestTestClientTerminate_ReturnsCleanupErrorOnce(t *testing.T) {
	t.Parallel()

	cleanupErr := errors.New("cleanup failed")
	calls := 0
	testClient := &TestClient{cleanup: func() error {
		calls++
		return cleanupErr
	}}

	for attempt := 1; attempt <= 2; attempt++ {
		if err := testClient.Terminate(); !errors.Is(err, cleanupErr) {
			t.Fatalf("Terminate attempt %d error = %v, want cleanup cause", attempt, err)
		}
	}
	if calls != 1 {
		t.Fatalf("cleanup called %d times, want exactly 1", calls)
	}
}

func TestCleanupContainerAfterStartFailure_PreservesBothErrors(t *testing.T) {
	t.Parallel()

	startErr := errors.New("start failed")
	cleanupErr := errors.New("terminate failed")
	container := &recordingContainerTerminator{terminate: func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("cleanup context must have an independent deadline")
		}
		if err := ctx.Err(); err != nil {
			t.Errorf("cleanup context started unusable: %v", err)
		}
		return cleanupErr
	}}

	err := cleanupContainerAfterStartFailureWithin(container, startErr, 100*time.Millisecond)
	if !errors.Is(err, startErr) {
		t.Fatalf("start cause was masked: %v", err)
	}
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("cleanup cause was not surfaced: %v", err)
	}
	if container.calls != 1 {
		t.Fatalf("Terminate called %d times, want 1", container.calls)
	}
}

func TestCleanupContainerAfterStartFailure_BoundsBlockingCleanup(t *testing.T) {
	t.Parallel()

	const cleanupBudget = 60 * time.Millisecond
	startErr := errors.New("start failed")
	container := &recordingContainerTerminator{terminate: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}

	start := time.Now()
	err := cleanupContainerAfterStartFailureWithin(container, startErr, cleanupBudget)
	elapsed := time.Since(start)

	if !errors.Is(err, startErr) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want both start and cleanup deadline causes", err)
	}
	if elapsed < cleanupBudget/2 || elapsed > time.Second {
		t.Fatalf("blocking cleanup returned after %s, want near %s", elapsed, cleanupBudget)
	}
}

func TestCleanupContainerAfterStartFailure_NilContainerKeepsStartError(t *testing.T) {
	t.Parallel()

	startErr := errors.New("create failed before returning a container")
	if err := cleanupContainerAfterStartFailureWithin(nil, startErr, time.Second); !errors.Is(err, startErr) {
		t.Fatalf("error = %v, want original start cause", err)
	}
}
