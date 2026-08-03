package natsclient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/docker/go-connections/nat"
)

func observedPorts(ports map[string]string) requiredPortObservation {
	return requiredPortObservation{mappedPorts: ports}
}

func TestRequiredPortsFromSnapshot_UsesConfiguredSet(t *testing.T) {
	t.Parallel()

	portMap := nat.PortMap{
		nat.Port(requiredClientPort):     {{HostPort: "14222"}},
		nat.Port(requiredMonitoringPort): {{HostPort: "18222"}},
	}
	tests := []struct {
		name     string
		required requiredPortSet
		want     map[string]string
	}{
		{
			name:     "default ignores unconfigured monitoring mapping",
			required: requiredPortSet{requiredClientPort},
			want:     map[string]string{requiredClientPort: "14222"},
		},
		{
			name:     "monitoring resolves both mappings from one snapshot",
			required: requiredPortSet{requiredClientPort, requiredMonitoringPort},
			want: map[string]string{
				requiredClientPort:     "14222",
				requiredMonitoringPort: "18222",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			observation := requiredPortsFromSnapshot(tt.required, false, portMap)
			if len(observation.missingPorts) != 0 {
				t.Fatalf("missing ports = %v, want none", observation.missingPorts)
			}
			for port, want := range tt.want {
				if got := observation.mappedPorts[port]; got != want {
					t.Errorf("mapped port %s = %q, want %q", port, got, want)
				}
			}
			if len(observation.mappedPorts) != len(tt.want) {
				t.Fatalf("mapped ports = %v, want only %v", observation.mappedPorts, tt.want)
			}
		})
	}
}

func TestRequiredPortsFromSnapshot_HostNetworkUsesConfiguredInternalPorts(t *testing.T) {
	t.Parallel()

	observation := requiredPortsFromSnapshot(
		requiredPortSet{requiredClientPort, requiredMonitoringPort},
		true,
		nil,
	)
	if got := observation.mappedPorts[requiredClientPort]; got != "4222" {
		t.Fatalf("client host-network port = %q, want 4222", got)
	}
	if got := observation.mappedPorts[requiredMonitoringPort]; got != "8222" {
		t.Fatalf("monitoring host-network port = %q, want 8222", got)
	}
}

func TestRequiredPortsFromSnapshot_ReportsOnlyConfiguredMissingPorts(t *testing.T) {
	t.Parallel()

	defaultObservation := requiredPortsFromSnapshot(
		requiredPortSet{requiredClientPort},
		false,
		nat.PortMap{nat.Port(requiredClientPort): {{HostPort: "14222"}}},
	)
	if len(defaultObservation.missingPorts) != 0 {
		t.Fatalf("default missing ports = %v, want none", defaultObservation.missingPorts)
	}

	monitoringObservation := requiredPortsFromSnapshot(
		requiredPortSet{requiredClientPort, requiredMonitoringPort},
		false,
		nat.PortMap{nat.Port(requiredClientPort): {{HostPort: "14222"}}},
	)
	if strings.Join(monitoringObservation.missingPorts, ",") != requiredMonitoringPort {
		t.Fatalf("monitoring missing ports = %v, want %s", monitoringObservation.missingPorts, requiredMonitoringPort)
	}
}

func TestResolveRequiredPorts_DefaultSucceedsWithoutMonitoring(t *testing.T) {
	t.Parallel()

	calls := 0
	observer := func(context.Context) (requiredPortObservation, error) {
		calls++
		return observedPorts(map[string]string{requiredClientPort: "14222"}), nil
	}

	observation, err := resolveRequiredPortsWithin(
		t.Context(), requiredPortSet{requiredClientPort}, observer, time.Second, 0,
	)
	if err != nil {
		t.Fatalf("resolveRequiredPortsWithin: %v", err)
	}
	if got := observation.mappedPorts[requiredClientPort]; got != "14222" {
		t.Fatalf("client port = %q, want 14222", got)
	}
	if calls != 1 {
		t.Fatalf("observer calls = %d, want exactly 1", calls)
	}
}

func TestResolveRequiredPorts_MonitoringRefusesSnapshotWithout8222(t *testing.T) {
	t.Parallel()

	observer := func(ctx context.Context) (requiredPortObservation, error) {
		select {
		case <-ctx.Done():
			return requiredPortObservation{}, ctx.Err()
		default:
			return observedPorts(map[string]string{requiredClientPort: "14222"}), nil
		}
	}

	_, err := resolveRequiredPortsWithin(
		t.Context(),
		requiredPortSet{requiredClientPort, requiredMonitoringPort},
		observer,
		20*time.Millisecond,
		0,
	)
	if err == nil {
		t.Fatal("expected monitoring mapping to remain required")
	}
	var resolutionErr *requiredPortResolutionError
	if !errors.As(err, &resolutionErr) ||
		!resolutionErr.observedRequiredPortAbsence(requiredPortSet{requiredMonitoringPort}) {
		t.Fatalf("error = %v, want observed monitoring-port absence", err)
	}
}

func TestResolveRequiredPorts_AlternatingPartialSnapshotsNeverCombine(t *testing.T) {
	t.Parallel()

	inspectErr := errors.New("final inspect failed")
	calls := 0
	observer := func(ctx context.Context) (requiredPortObservation, error) {
		calls++
		switch calls {
		case 1:
			return observedPorts(map[string]string{requiredClientPort: "14222"}), nil
		case 2:
			return observedPorts(map[string]string{requiredMonitoringPort: "18222"}), nil
		default:
			<-ctx.Done()
			return requiredPortObservation{}, errors.Join(inspectErr, ctx.Err())
		}
	}

	_, err := resolveRequiredPortsWithin(
		t.Context(),
		requiredPortSet{requiredClientPort, requiredMonitoringPort},
		observer,
		40*time.Millisecond,
		0,
	)
	if err == nil || !errors.Is(err, inspectErr) {
		t.Fatalf("error = %v, want final inspect cause", err)
	}
	if calls != 3 {
		t.Fatalf("observer calls = %d, want 3", calls)
	}
	for _, want := range []string{"last successful observation attempt 2", requiredClientPort} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestResolveRequiredPorts_MissingThenDeadlinePreservesAbsenceAndInspectError(t *testing.T) {
	t.Parallel()

	inspectErr := errors.New("inspect unavailable")
	calls := 0
	observer := func(ctx context.Context) (requiredPortObservation, error) {
		calls++
		if calls == 1 {
			return observedPorts(map[string]string{requiredClientPort: "14222"}), nil
		}
		<-ctx.Done()
		return requiredPortObservation{}, errors.Join(inspectErr, ctx.Err())
	}

	_, err := resolveRequiredPortsWithin(
		t.Context(),
		requiredPortSet{requiredClientPort, requiredMonitoringPort},
		observer,
		40*time.Millisecond,
		0,
	)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, inspectErr) {
		t.Fatalf("error = %v, want deadline and inspect causes", err)
	}
	var resolutionErr *requiredPortResolutionError
	if !errors.As(err, &resolutionErr) ||
		!resolutionErr.observedRequiredPortAbsence(requiredPortSet{requiredMonitoringPort}) {
		t.Fatalf("error = %v, want substantive absence retained at deadline", err)
	}
	for _, want := range []string{
		"mapping budget 40ms", "last successful observation attempt 1",
		requiredMonitoringPort, "last inspect error attempt 2",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing diagnostic %q", err, want)
		}
	}
}

func TestResolveRequiredPorts_AllInspectErrorsHaveNoObservedAbsence(t *testing.T) {
	t.Parallel()

	inspectErr := errors.New("inspect unavailable")
	observer := func(ctx context.Context) (requiredPortObservation, error) {
		<-ctx.Done()
		return requiredPortObservation{}, errors.Join(inspectErr, ctx.Err())
	}

	_, err := resolveRequiredPortsWithin(
		t.Context(), requiredPortSet{requiredClientPort}, observer, 30*time.Millisecond, 0,
	)
	if err == nil {
		t.Fatal("expected inspect failure")
	}
	var resolutionErr *requiredPortResolutionError
	if !errors.As(err, &resolutionErr) {
		t.Fatalf("error type = %T, want requiredPortResolutionError", err)
	}
	if resolutionErr.observedRequiredPortAbsence(requiredPortSet{requiredClientPort}) {
		t.Fatalf("error = %v, failed inspections cannot prove port absence", err)
	}
	if !strings.Contains(err.Error(), "no successful port observation") {
		t.Fatalf("error = %v, want explicit lack of successful snapshot", err)
	}
}

func TestResolveRequiredPorts_StopsOnParentCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	observer := func(context.Context) (requiredPortObservation, error) {
		calls++
		return requiredPortObservation{}, nil
	}

	_, err := resolveRequiredPortsWithin(
		ctx, requiredPortSet{requiredClientPort}, observer, time.Second, 0,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
	if calls != 0 {
		t.Fatalf("observer calls = %d, want 0 after prior cancellation", calls)
	}
}
