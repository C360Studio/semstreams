package natsclient

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func mappedPortAttemptError(
	attempt int,
	missingPorts []string,
	cause error,
	cleanupErr error,
) error {
	return &testClientSetupError{
		attempt:      attempt,
		phase:        testClientSetupPhaseMappedPort,
		missingPorts: missingPorts,
		cause:        cause,
		cleanupErr:   cleanupErr,
	}
}

func observedAbsenceError(missingPorts ...string) *requiredPortResolutionError {
	return &requiredPortResolutionError{
		attempts: 2,
		budget:   requiredPortObservationBudget,
		lastSuccessfulObservation: &successfulRequiredPortObservation{
			attempt:      1,
			missingPorts: missingPorts,
		},
		terminalErr: context.DeadlineExceeded,
	}
}

func TestNewTestClientFactory_HappyPathStartsOnce(t *testing.T) {
	t.Parallel()

	want := &TestClient{}
	starts := 0
	got, err := newTestClient(t.Context(), testClientFactoryDeps{
		attempt: func(context.Context, *testConfig, int) (*TestClient, error) {
			starts++
			return want, nil
		},
	})
	if err != nil {
		t.Fatalf("newTestClient: %v", err)
	}
	if got != want {
		t.Fatalf("client = %p, want %p", got, want)
	}
	if starts != 1 {
		t.Fatalf("starts = %d, want 1", starts)
	}
}

func TestNewTestClientFactory_MissingThenDeadlineRemainsEligibleAndCleansBeforeReplacement(t *testing.T) {
	t.Parallel()

	want := &TestClient{}
	portErr := observedAbsenceError(requiredClientPort)
	events := make([]string, 0, 3)
	got, err := newTestClient(t.Context(), testClientFactoryDeps{
		attempt: func(_ context.Context, _ *testConfig, attempt int) (*TestClient, error) {
			events = append(events, "start")
			if attempt == 1 {
				events = append(events, "cleanup")
				return nil, mappedPortAttemptError(
					attempt, []string{requiredClientPort}, portErr, nil,
				)
			}
			return want, nil
		},
	})
	if err != nil {
		t.Fatalf("newTestClient: %v", err)
	}
	if got != want {
		t.Fatalf("client = %p, want %p", got, want)
	}
	if gotEvents := strings.Join(events, ","); gotEvents != "start,cleanup,start" {
		t.Fatalf("events = %s, want cleanup before replacement start", gotEvents)
	}
}

func TestNewTestClientFactory_CleanupFailureSuppressesReplacement(t *testing.T) {
	t.Parallel()

	portErr := observedAbsenceError(requiredMonitoringPort)
	cleanupErr := errors.New("termination failed")
	starts := 0
	_, err := newTestClient(t.Context(), testClientFactoryDeps{
		attempt: func(_ context.Context, _ *testConfig, attempt int) (*TestClient, error) {
			starts++
			return nil, mappedPortAttemptError(
				attempt, []string{requiredMonitoringPort}, portErr, cleanupErr,
			)
		},
	}, WithMonitoring())
	if err == nil {
		t.Fatal("expected setup failure")
	}
	if starts != 1 {
		t.Fatalf("starts = %d, want 1 when cleanup fails", starts)
	}
	if !errors.Is(err, portErr) || !errors.Is(err, cleanupErr) {
		t.Fatalf("error = %v, want mapping and cleanup causes", err)
	}
}

func TestNewTestClientFactory_DoesNotRetryNoneligibleFailures(t *testing.T) {
	t.Parallel()

	notFound := errors.New("not found outside mapped-port phase")
	tests := []struct {
		name string
		err  error
	}{
		{
			name: "startup not found",
			err: &testClientSetupError{
				attempt: 1,
				phase:   testClientSetupPhaseStart,
				cause:   notFound,
			},
		},
		{
			name: "host lookup",
			err: &testClientSetupError{
				attempt: 1,
				phase:   testClientSetupPhaseHost,
				cause:   notFound,
			},
		},
		{
			name: "all inspections failed",
			err: mappedPortAttemptError(
				1,
				nil,
				&requiredPortResolutionError{lastInspectErr: errors.New("inspect failed")},
				nil,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			starts := 0
			_, err := newTestClient(t.Context(), testClientFactoryDeps{
				attempt: func(context.Context, *testConfig, int) (*TestClient, error) {
					starts++
					return nil, tt.err
				},
			})
			if err == nil {
				t.Fatal("expected setup failure")
			}
			if starts != 1 {
				t.Fatalf("starts = %d, want 1", starts)
			}
		})
	}
}

func TestNewTestClientFactory_DoesNotRetryAfterParentCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	resolutionErr := observedAbsenceError(requiredMonitoringPort)
	resolutionErr.terminalErr = context.Canceled
	starts := 0
	_, err := newTestClient(ctx, testClientFactoryDeps{
		attempt: func(context.Context, *testConfig, int) (*TestClient, error) {
			starts++
			cancel()
			return nil, mappedPortAttemptError(
				1,
				[]string{requiredMonitoringPort},
				resolutionErr,
				nil,
			)
		},
	}, WithMonitoring())
	if err == nil {
		t.Fatal("expected setup failure")
	}
	if starts != 1 {
		t.Fatalf("starts = %d, want 1 after parent cancellation", starts)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation cause", err)
	}
}

func TestNewTestClientFactory_TwoEligibleFailuresStopAfterTwoAttempts(t *testing.T) {
	t.Parallel()

	firstErr := observedAbsenceError(requiredMonitoringPort)
	secondErr := observedAbsenceError(requiredMonitoringPort)
	starts := 0
	cleanups := 0
	_, err := newTestClient(t.Context(), testClientFactoryDeps{
		attempt: func(_ context.Context, _ *testConfig, attempt int) (*TestClient, error) {
			starts++
			cleanups++
			if attempt == 1 {
				return nil, mappedPortAttemptError(
					attempt, []string{requiredMonitoringPort}, firstErr, nil,
				)
			}
			return nil, mappedPortAttemptError(
				attempt, []string{requiredMonitoringPort}, secondErr, nil,
			)
		},
	}, WithMonitoring())
	if err == nil {
		t.Fatal("expected setup failure")
	}
	if starts != 2 || cleanups != 2 {
		t.Fatalf("starts/cleanups = %d/%d, want 2/2", starts, cleanups)
	}
	if !errors.Is(err, firstErr) || !errors.Is(err, secondErr) {
		t.Fatalf("error = %v, want both attempt causes", err)
	}
	for _, label := range []string{
		"attempt 1", "attempt 2", "phase mapped-port", requiredMonitoringPort,
	} {
		if !strings.Contains(err.Error(), label) {
			t.Errorf("error %q missing label %q", err, label)
		}
	}
}

func TestNewTestClientFactory_RetriesObservedRequiredMonitoringPortAbsence(t *testing.T) {
	t.Parallel()

	want := &TestClient{}
	starts := 0
	got, err := newTestClient(t.Context(), testClientFactoryDeps{
		attempt: func(_ context.Context, _ *testConfig, attempt int) (*TestClient, error) {
			starts++
			if attempt == 1 {
				return nil, mappedPortAttemptError(
					attempt,
					[]string{requiredMonitoringPort},
					observedAbsenceError(requiredMonitoringPort),
					nil,
				)
			}
			return want, nil
		},
	}, WithMonitoring())
	if err != nil {
		t.Fatalf("newTestClient: %v", err)
	}
	if got != want || starts != 2 {
		t.Fatalf("client/starts = %p/%d, want %p/2", got, starts, want)
	}
}

func TestNewTestClientFactory_DefaultDoesNotRetryUnconfiguredMonitoringAbsence(t *testing.T) {
	t.Parallel()

	starts := 0
	_, err := newTestClient(t.Context(), testClientFactoryDeps{
		attempt: func(_ context.Context, _ *testConfig, attempt int) (*TestClient, error) {
			starts++
			return nil, mappedPortAttemptError(
				attempt,
				[]string{requiredMonitoringPort},
				observedAbsenceError(requiredMonitoringPort),
				nil,
			)
		},
	})
	if err == nil {
		t.Fatal("expected setup failure")
	}
	if starts != 1 {
		t.Fatalf("starts = %d, want 1 for unconfigured monitoring absence", starts)
	}
}

func TestNewTestClientFactory_DiagnosticsIncludeContainerAndParentState(t *testing.T) {
	t.Parallel()

	cause := errors.New("host lookup failed")
	_, err := newTestClient(t.Context(), testClientFactoryDeps{
		attempt: func(context.Context, *testConfig, int) (*TestClient, error) {
			return nil, &testClientSetupError{
				attempt:     1,
				phase:       testClientSetupPhaseHost,
				containerID: "container-123",
				cause:       cause,
			}
		},
	})
	if err == nil {
		t.Fatal("expected setup failure")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want original cause", err)
	}
	for _, label := range []string{"container container-123", "parent context live"} {
		if !strings.Contains(err.Error(), label) {
			t.Errorf("error %q missing label %q", err, label)
		}
	}
}
