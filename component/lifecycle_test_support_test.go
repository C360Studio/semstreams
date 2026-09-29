package component

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type lifecycleSupportProbe struct {
	*SimpleMockComponent
	mu            sync.Mutex
	initializeErr error
	startErr      error
	stopErr       error
	onStart       func(context.Context)
	onStop        func(context.Context, context.Context) error
	startCtx      context.Context
	stopCalls     int
}

func newLifecycleSupportProbe() *lifecycleSupportProbe {
	return &lifecycleSupportProbe{SimpleMockComponent: &SimpleMockComponent{name: "owner-probe"}}
}

func (p *lifecycleSupportProbe) Initialize() error { return p.initializeErr }

func (p *lifecycleSupportProbe) Start(ctx context.Context) error {
	p.mu.Lock()
	p.startCtx = ctx
	onStart := p.onStart
	p.mu.Unlock()
	if onStart != nil {
		onStart(ctx)
	}
	return p.startErr
}

func (p *lifecycleSupportProbe) Stop(ctx context.Context) error {
	p.mu.Lock()
	p.stopCalls++
	startCtx := p.startCtx
	onStop := p.onStop
	p.mu.Unlock()
	fmt.Fprintln(os.Stdout, "LIFECYCLE_EVENT stop")
	if onStop != nil {
		return onStop(startCtx, ctx)
	}
	return p.stopErr
}

func (p *lifecycleSupportProbe) stopCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stopCalls
}

func TestSharedLifecycleEarlyFailureChild(t *testing.T) {
	if os.Getenv("SEMSTREAMS_LIFECYCLE_CHILD") != "initialize" {
		return
	}
	testNoResourceLeaks(t, func() LifecycleComponent {
		fmt.Fprintln(os.Stdout, "LIFECYCLE_EVENT factory")
		probe := newLifecycleSupportProbe()
		probe.initializeErr = errors.New("injected initialize failure")
		return probe
	})
}

func runLifecycleSupportChild(t *testing.T, marker, runPattern string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	outputPath := filepath.Join(t.TempDir(), "child-output.log")
	output, err := os.Create(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+runPattern, "-test.v")
	cmd.Env = append(os.Environ(), "SEMSTREAMS_LIFECYCLE_CHILD="+marker)
	cmd.Stdout, cmd.Stderr = output, output
	runErr := cmd.Run() // The child has no descendants; Cmd.Run owns its only Wait.
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil {
		t.Fatalf("child exceeded four-second containment budget: %v; output=%s", ctx.Err(), raw)
	}
	return string(raw), runErr
}

func TestSharedLifecycleEarlyFailureFinalizes(t *testing.T) {
	output, runErr := runLifecycleSupportChild(t, "initialize", "^TestSharedLifecycleEarlyFailureChild$")
	if runErr == nil {
		t.Fatalf("unexpected Initialize failure was not reported: %s", output)
	}
	if got := strings.Count(output, "LIFECYCLE_EVENT factory"); got != 1 {
		t.Fatalf("factory calls = %d, want one before admission stops: %s", got, output)
	}
	if got := strings.Count(output, "LIFECYCLE_EVENT stop"); got != 1 {
		t.Fatalf("terminal calls = %d, want one after early Initialize failure: %s", got, output)
	}
	if !strings.Contains(output, "injected initialize failure") {
		t.Fatalf("missing original operation error: %s", output)
	}
}

func TestSharedLifecycleFatalChild(t *testing.T) {
	if os.Getenv("SEMSTREAMS_LIFECYCLE_CHILD") != "fatal" {
		return
	}
	t.Cleanup(func() { fmt.Fprintln(os.Stdout, "LIFECYCLE_EVENT substrate") })
	StandardLifecycleTests(t, func() LifecycleComponent {
		fmt.Fprintln(os.Stdout, "LIFECYCLE_EVENT factory")
		probe := newLifecycleSupportProbe()
		probe.initializeErr = errors.New("fatal Initialize fixture")
		return probe
	})
}

func TestSharedLifecycleFatalExitPrecedesSubstrateCleanup(t *testing.T) {
	output, runErr := runLifecycleSupportChild(t, "fatal", "^TestSharedLifecycleFatalChild$/^PortableFloor$/^ControlledStopWithLiveStartAuthority$")
	if runErr == nil {
		t.Fatalf("fatal Initialize fixture unexpectedly passed: %s", output)
	}
	if strings.Count(output, "LIFECYCLE_EVENT factory") != 1 || strings.Count(output, "LIFECYCLE_EVENT stop") != 1 {
		t.Fatalf("fatal exit lost or duplicated returned-instance finalization: %s", output)
	}
	stopAt, substrateAt := strings.Index(output, "LIFECYCLE_EVENT stop"), strings.Index(output, "LIFECYCLE_EVENT substrate")
	if stopAt < 0 || substrateAt < 0 || stopAt >= substrateAt {
		t.Fatalf("base Stop did not complete before substrate cleanup: %s", output)
	}
	if !strings.Contains(output, "fatal Initialize fixture") {
		t.Fatalf("fatal operation error missing: %s", output)
	}
}

func TestSharedLifecycleInjectionFinalizesBase(t *testing.T) {
	var bases []*lifecycleSupportProbe
	TestErrorInjection(t, func() LifecycleComponent {
		base := newLifecycleSupportProbe()
		bases = append(bases, base)
		return base
	})
	if len(bases) != 3 {
		t.Fatalf("base instances = %d, want three injected cases", len(bases))
	}
	for i, base := range bases {
		if got := base.stopCount(); got != 1 {
			t.Errorf("base %d concrete Stop calls = %d, want exactly one despite wrapper injection", i, got)
		}
	}
}

func TestSharedLifecycleAbortRequiresConcreteCallerCause(t *testing.T) {
	for _, tt := range []struct {
		name        string
		concreteErr error
		wantMissing bool
	}{
		{"nil", nil, true},
		{"wrong", errors.New("wrong terminal cause"), true},
		{"exact", context.Canceled, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			probe := newLifecycleSupportProbe()
			probe.stopErr = tt.concreteErr
			owner := newLifecycleTestOwner(probe)
			stopCtx, cancel := context.WithCancel(t.Context())
			cancel()
			if err := owner.stopWithContext(stopCtx, nil, true); !errors.Is(err, context.Canceled) {
				t.Fatalf("support must report expired caller bound independently: %v", err)
			}
			contractErr := owner.abortContractError()
			if (contractErr != nil) != tt.wantMissing {
				t.Fatalf("concrete Stop result %v, contract error %v, want missing=%v", tt.concreteErr, contractErr, tt.wantMissing)
			}
			if probe.stopCount() != 1 {
				t.Fatalf("concrete Stop calls = %d, want one", probe.stopCount())
			}
		})
	}
}

func TestSharedLifecycleOwnedCycleTransitions(t *testing.T) {
	initializeFailure := errors.New("initialize transition failed")
	startFailure := errors.New("start transition failed")
	stopFailure := errors.New("stop transition failed")
	for _, tt := range []struct {
		name       string
		phaseError error
		wantError  error
		wantStops  int
	}{
		{"normal", nil, nil, 1},
		{"initialize_failure", initializeFailure, initializeFailure, 1},
		{"start_failure", startFailure, startFailure, 1},
		{"stop_failure", stopFailure, stopFailure, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			probe := newLifecycleSupportProbe()
			switch tt.name {
			case "initialize_failure":
				probe.initializeErr = tt.phaseError
			case "start_failure":
				probe.startErr = tt.phaseError
			case "stop_failure":
				probe.stopErr = tt.phaseError
			}
			probe.onStop = func(startCtx, stopCtx context.Context) error {
				if _, finite := stopCtx.Deadline(); !finite {
					return errors.New("Stop lacked finite caller authority")
				}
				if startCtx != nil {
					if _, finite := startCtx.Deadline(); !finite {
						return errors.New("Start lacked finite work authority")
					}
					if startCtx.Err() != nil {
						return errors.New("Start authority ended before Stop")
					}
				}
				return probe.stopErr
			}
			got := runLifecycleCycle(t.Context(), func() LifecycleComponent { return probe }, tt.name)
			if tt.wantError == nil && got != nil {
				t.Fatalf("cycle error: %v", got)
			}
			if tt.wantError != nil && !errors.Is(got, tt.wantError) {
				t.Fatalf("cycle error = %v, want %v", got, tt.wantError)
			}
			if got := probe.stopCount(); got != tt.wantStops {
				t.Fatalf("concrete Stop calls = %d, want %d", got, tt.wantStops)
			}
		})
	}
	t.Run("nil_factory", func(t *testing.T) {
		if err := runLifecycleCycle(t.Context(), func() LifecycleComponent { return nil }, "nil_factory"); err == nil || !strings.Contains(err.Error(), "factory returned nil") {
			t.Fatalf("nil factory result = %v", err)
		}
	})
	t.Run("typed_nil_factory", func(t *testing.T) {
		var typedNil *lifecycleSupportProbe
		if err := runLifecycleCycle(t.Context(), func() LifecycleComponent { return typedNil }, "typed_nil_factory"); err == nil || !strings.Contains(err.Error(), "factory returned nil") {
			t.Fatalf("typed nil factory result = %v; expected acquisition refusal before any method call", err)
		}
	})
}

func TestSharedLifecycleStopBoundAndNoImplicitRetry(t *testing.T) {
	probe := newLifecycleSupportProbe()
	probe.onStop = func(_, stopCtx context.Context) error {
		<-stopCtx.Done() // Cooperative fake; no wall-clock containment is inferred.
		return stopCtx.Err()
	}
	owner := newLifecycleTestOwner(probe)
	stopCtx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := owner.stopWithContext(stopCtx, nil, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cooperative Stop bound = %v, want caller cancellation", err)
	}
	if err := owner.finish(nil, false); err != nil {
		t.Fatalf("finalizer replayed attempted terminal Stop: %v", err)
	}
	if got := probe.stopCount(); got != 1 {
		t.Fatalf("concrete Stop calls = %d, want one after bound wins", got)
	}
}

func TestSharedLifecycleParallelFailureChild(t *testing.T) {
	if os.Getenv("SEMSTREAMS_LIFECYCLE_CHILD") != "parallel" {
		return
	}
	testParallelFreshInstances(t, func() LifecycleComponent {
		fmt.Fprintln(os.Stdout, "LIFECYCLE_EVENT factory")
		probe := newLifecycleSupportProbe()
		probe.initializeErr = errors.New("parallel Initialize failure")
		return probe
	})
}

func TestSharedLifecycleParallelFailureDrainsOwnedInstances(t *testing.T) {
	output, runErr := runLifecycleSupportChild(t, "parallel", "^TestSharedLifecycleParallelFailureChild$")
	if runErr == nil || !strings.Contains(output, "parallel Initialize failure") {
		t.Fatalf("parallel failure not reported: %v %s", runErr, output)
	}
	factories := strings.Count(output, "LIFECYCLE_EVENT factory")
	stops := strings.Count(output, "LIFECYCLE_EVENT stop")
	if factories == 0 || factories > 10 || stops != factories {
		t.Fatalf("admission/drain after failure: factories=%d stops=%d, want 1..10 owned and all finalized: %s", factories, stops, output)
	}
}

func TestSharedLifecycleParallelReportedFailureKeepsLivePeerOwned(t *testing.T) {
	fixtureCtx, cancelFixture := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancelFixture()
	started := make(chan struct{})
	reportedFailure := make(chan struct{})
	var reportedOnce sync.Once
	var admitted atomic.Int32
	livePeer := newLifecycleSupportProbe()
	livePeer.onStart = func(context.Context) { close(started) }
	livePeer.onStop = func(startCtx, stopCtx context.Context) error {
		select {
		case <-reportedFailure: // Parent observed peer failure before Stop proceeds.
		case <-fixtureCtx.Done():
			return errors.New("fixture missing reported peer failure before live Stop")
		}
		if _, finite := stopCtx.Deadline(); !finite {
			return errors.New("live peer Stop lacked finite authority")
		}
		if startCtx == nil || startCtx.Err() != nil {
			return errors.New("live peer Start authority ended before terminal return")
		}
		return nil
	}
	var reports []error
	runParallelFreshInstances(t.Context(), func() LifecycleComponent {
		if admitted.Add(1) == 1 {
			return livePeer
		}
		select {
		case <-started: // A failure cannot precede the live peer's accepted Start.
		case <-fixtureCtx.Done():
			probe := newLifecycleSupportProbe()
			probe.initializeErr = errors.New("fixture missing accepted Start phase")
			return probe
		}
		probe := newLifecycleSupportProbe()
		probe.initializeErr = errors.New("peer Initialize failure")
		return probe
	}, func(err error) {
		reports = append(reports, err)
		if strings.Contains(err.Error(), "peer Initialize failure") {
			reportedOnce.Do(func() { close(reportedFailure) })
		}
	})
	if fixtureCtx.Err() != nil {
		t.Fatalf("fixture missing accepted Start or reported peer failure; all workers joined: %v; reports=%v", fixtureCtx.Err(), reports)
	}
	if len(reports) == 0 || !strings.Contains(reports[0].Error(), "peer Initialize failure") {
		t.Fatalf("expected reported peer failure, got %v", reports)
	}
	for _, err := range reports {
		if !strings.Contains(err.Error(), "peer Initialize failure") {
			t.Errorf("live owner or peer reported unexpected error: %v", err)
		}
	}
	if livePeer.stopCount() != 1 || livePeer.startCtx == nil || livePeer.startCtx.Err() == nil {
		t.Fatalf("live peer terminal/post-return cancellation: stops=%d Start-context=%v", livePeer.stopCount(), livePeer.startCtx)
	}
}

func TestSharedLifecyclePrerequisiteFailureChild(t *testing.T) {
	marker := os.Getenv("SEMSTREAMS_LIFECYCLE_CHILD")
	if marker != "prereq_initialize" && marker != "prereq_start" {
		return
	}
	t.Cleanup(func() { fmt.Fprintln(os.Stdout, "LIFECYCLE_EVENT substrate") })
	TestErrorInjection(t, func() LifecycleComponent {
		fmt.Fprintln(os.Stdout, "LIFECYCLE_EVENT factory")
		probe := newLifecycleSupportProbe()
		if marker == "prereq_initialize" {
			probe.initializeErr = errors.New("base Initialize prerequisite failure")
		} else {
			probe.startErr = errors.New("base Start prerequisite failure")
		}
		return probe
	})
}

func TestSharedLifecycleInjectionRefusesFailedPrerequisites(t *testing.T) {
	for _, tt := range []struct{ marker, selected, cause string }{
		{"prereq_initialize", "inject_start_error", "base Initialize prerequisite failure"},
		{"prereq_start", "inject_stop_error", "base Start prerequisite failure"},
	} {
		t.Run(tt.marker, func(t *testing.T) {
			pattern := "^TestSharedLifecyclePrerequisiteFailureChild$/^" + tt.selected + "$/"
			output, runErr := runLifecycleSupportChild(t, tt.marker, pattern)
			if runErr == nil || !strings.Contains(output, tt.cause) {
				t.Fatalf("base prerequisite failure was not reported: %v %s", runErr, output)
			}
			if strings.Count(output, "LIFECYCLE_EVENT factory") != 1 || strings.Count(output, "LIFECYCLE_EVENT stop") != 1 {
				t.Fatalf("failed prerequisite lost returned-instance finalization: %s", output)
			}
			if strings.Index(output, "LIFECYCLE_EVENT stop") > strings.Index(output, "LIFECYCLE_EVENT substrate") {
				t.Fatalf("base finalization followed substrate cleanup: %s", output)
			}
		})
	}
}

func TestSharedLifecycleOperationAndCleanupErrorsRemainDistinct(t *testing.T) {
	operationErr := errors.New("operation failure")
	cleanupErr := errors.New("cleanup failure")
	probe := newLifecycleSupportProbe()
	probe.initializeErr, probe.stopErr = operationErr, cleanupErr
	err := runLifecycleCycle(t.Context(), func() LifecycleComponent { return probe }, "two-errors")
	if !errors.Is(err, operationErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("cycle lost operation or cleanup error: %v", err)
	}
	if probe.stopCount() != 1 {
		t.Fatalf("cleanup calls=%d, want one", probe.stopCount())
	}
}

func BenchmarkSharedLifecycleFailureChild(b *testing.B) {
	if os.Getenv("SEMSTREAMS_LIFECYCLE_CHILD") != "benchmark" {
		return
	}
	BenchmarkLifecycleMethods(b, func() LifecycleComponent {
		fmt.Fprintln(os.Stdout, "LIFECYCLE_EVENT factory")
		probe := newLifecycleSupportProbe()
		probe.initializeErr = errors.New("benchmark Initialize failure")
		return probe
	})
}

func TestSharedLifecycleBenchmarkChecksOperationAndFinalizer(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	outputPath := filepath.Join(t.TempDir(), "benchmark-child.log")
	output, err := os.Create(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$", "-test.bench=^BenchmarkSharedLifecycleFailureChild$/^Initialize$", "-test.benchtime=1x", "-test.v")
	cmd.Env = append(os.Environ(), "SEMSTREAMS_LIFECYCLE_CHILD=benchmark")
	cmd.Stdout, cmd.Stderr = output, output
	runErr := cmd.Run()
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.Err() != nil || runErr == nil || !strings.Contains(string(raw), "benchmark Initialize failure") {
		t.Fatalf("benchmark did not report operation failure within child bound: context=%v run=%v output=%s", ctx.Err(), runErr, raw)
	}
	if strings.Count(string(raw), "LIFECYCLE_EVENT factory") != 1 || strings.Count(string(raw), "LIFECYCLE_EVENT stop") != 1 {
		t.Fatalf("benchmark iteration lost or duplicated finalization: %s", raw)
	}
}
