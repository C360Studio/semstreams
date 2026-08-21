package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
)

// mockComponentHealthGetter implements componentHealthGetter for testing
type mockComponentHealthGetter struct {
	MockService
	health map[string]component.HealthStatus
}

func (m *mockComponentHealthGetter) GetComponentHealth() map[string]component.HealthStatus {
	return m.health
}

var _ componentHealthGetter = (*ComponentManager)(nil)

type heartbeatLogWriter struct {
	records chan []byte
}

func (w *heartbeatLogWriter) Write(p []byte) (int, error) {
	record := bytes.Clone(p)
	w.records <- record
	return len(p), nil
}

func newManagedHeartbeatForTest(t *testing.T, health map[string]component.HealthStatus) *HeartbeatService {
	t.Helper()
	registry := NewServiceRegistry()
	manager := NewServiceManager(registry)
	getter := &mockComponentHealthGetter{
		MockService: MockService{name: "component-manager"},
		health:      health,
	}
	if err := manager.RegisterInstance("component-manager", getter); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("heartbeat", NewHeartbeatService); err != nil {
		t.Fatal(err)
	}
	svc, err := manager.CreateService("heartbeat", json.RawMessage(`{"interval":"1s"}`), &Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	return svc.(*HeartbeatService)
}

func TestHeartbeatConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  HeartbeatConfig
		wantErr bool
	}{
		{
			name:    "empty interval uses default",
			config:  HeartbeatConfig{},
			wantErr: false,
		},
		{
			name:    "valid interval",
			config:  HeartbeatConfig{Interval: "30s"},
			wantErr: false,
		},
		{
			name:    "valid minute interval",
			config:  HeartbeatConfig{Interval: "1m"},
			wantErr: false,
		},
		{
			name:    "invalid duration format",
			config:  HeartbeatConfig{Interval: "invalid"},
			wantErr: true,
		},
		{
			name:    "negative interval",
			config:  HeartbeatConfig{Interval: "-1s"},
			wantErr: true,
		},
		{
			name:    "zero interval",
			config:  HeartbeatConfig{Interval: "0s"},
			wantErr: true,
		},
		{
			name:    "interval too short",
			config:  HeartbeatConfig{Interval: "500ms"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNewHeartbeatService(t *testing.T) {
	tests := []struct {
		name      string
		rawConfig json.RawMessage
		wantErr   bool
	}{
		{
			name:      "nil config uses defaults",
			rawConfig: nil,
			wantErr:   false,
		},
		{
			name:      "empty config uses defaults",
			rawConfig: json.RawMessage(`{}`),
			wantErr:   false,
		},
		{
			name:      "valid config",
			rawConfig: json.RawMessage(`{"interval": "10s"}`),
			wantErr:   false,
		},
		{
			name:      "invalid json",
			rawConfig: json.RawMessage(`{invalid`),
			wantErr:   true,
		},
		{
			name:      "invalid interval",
			rawConfig: json.RawMessage(`{"interval": "invalid"}`),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewHeartbeatService(tt.rawConfig, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewHeartbeatService() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && svc == nil {
				t.Error("NewHeartbeatService() returned nil service without error")
			}
		})
	}
}

func TestHeartbeatService_StartStop(t *testing.T) {
	hb := newManagedHeartbeatForTest(t, nil)

	ctx := context.Background()

	// Start service
	if err := hb.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if hb.Status() != StatusRunning {
		t.Errorf("Status() = %v, want %v", hb.Status(), StatusRunning)
	}

	// Verify start time was set
	if hb.startTime.IsZero() {
		t.Error("startTime should be set after Start()")
	}

	// Stop service
	if err := hb.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if hb.Status() != StatusStopped {
		t.Errorf("Status() = %v, want %v", hb.Status(), StatusStopped)
	}
}

func TestHeartbeatService_StartAlreadyRunning(t *testing.T) {
	hb := newManagedHeartbeatForTest(t, nil)

	ctx := context.Background()

	// Start service
	if err := hb.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer hb.Stop(context.Background())

	// Try to start again
	err := hb.Start(ctx)
	if err == nil {
		t.Error("Start() should return error when already running")
	}
}

func TestHeartbeatService_StopNotRunning(t *testing.T) {
	hb, err := newHeartbeatServiceForTest(&HeartbeatConfig{Interval: "1s"}, nil)
	if err != nil {
		t.Fatalf("newHeartbeatServiceForTest() error = %v", err)
	}

	// Stop without Start: an already-stopped service is clean success per the
	// Service contract (gh#520) — nil or ErrAlreadyStopped, never a fatal error.
	err = hb.Stop(context.Background())
	if err != nil && !errors.Is(err, ErrAlreadyStopped) {
		t.Errorf("Stop() when not running = %v, want nil or ErrAlreadyStopped", err)
	}
}

// TestHeartbeatService_StopIdempotent covers gh#549: repeated Stop calls are
// safe (no double-close of stopChan) and every call reports success.
func TestHeartbeatService_StopIdempotent(t *testing.T) {
	hb := newManagedHeartbeatForTest(t, nil)

	if err := hb.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	for i := range 3 {
		if err := hb.Stop(context.Background()); err != nil && !errors.Is(err, ErrAlreadyStopped) {
			t.Fatalf("Stop() call %d = %v, want nil or ErrAlreadyStopped", i+1, err)
		}
	}

	if hb.Status() != StatusStopped {
		t.Errorf("Status() = %v, want %v", hb.Status(), StatusStopped)
	}
}

// TestHeartbeatService_StartAfterStop locks the single-use contract: once Stop
// has run teardown, Start must fail loudly rather than report Running with a
// dead heartbeat loop.
func TestHeartbeatService_StartAfterStop(t *testing.T) {
	hb := newManagedHeartbeatForTest(t, nil)

	if err := hb.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := hb.Stop(context.Background()); err != nil && !errors.Is(err, ErrAlreadyStopped) {
		t.Fatalf("Stop() error = %v", err)
	}

	if err := hb.Start(context.Background()); err == nil {
		t.Error("Start() after Stop should fail: instances are single-use")
	}
}

func TestHeartbeatService_WithComponentManager(t *testing.T) {
	health := map[string]component.HealthStatus{
		"component1": {Healthy: true},
		"component2": {Healthy: true},
		"component3": {Healthy: false},
	}
	hb := newManagedHeartbeatForTest(t, health)

	if err := hb.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if hb.componentManager == nil {
		t.Fatal("Start() did not resolve component-manager")
	}
	if err := hb.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestHeartbeatService_StartRejectsInvalidManagedDependency(t *testing.T) {
	tests := []struct {
		name    string
		build   func(t *testing.T) *HeartbeatService
		wantErr string
	}{
		{
			name: "missing owner",
			build: func(t *testing.T) *HeartbeatService {
				t.Helper()
				svc, err := NewHeartbeatService(json.RawMessage(`{"interval":"1s"}`), nil)
				if err != nil {
					t.Fatal(err)
				}
				return svc.(*HeartbeatService)
			},
			wantErr: "service manager",
		},
		{
			name: "missing component manager",
			build: func(t *testing.T) *HeartbeatService {
				t.Helper()
				registry := NewServiceRegistry()
				if err := registry.Register("heartbeat", NewHeartbeatService); err != nil {
					t.Fatal(err)
				}
				manager := NewServiceManager(registry)
				svc, err := manager.CreateService("heartbeat", json.RawMessage(`{"interval":"1s"}`), &Dependencies{})
				if err != nil {
					t.Fatal(err)
				}
				return svc.(*HeartbeatService)
			},
			wantErr: "component-manager",
		},
		{
			name: "wrong component manager contract",
			build: func(t *testing.T) *HeartbeatService {
				t.Helper()
				registry := NewServiceRegistry()
				if err := registry.Register("heartbeat", NewHeartbeatService); err != nil {
					t.Fatal(err)
				}
				manager := NewServiceManager(registry)
				if err := manager.RegisterInstance("component-manager", &MockService{name: "component-manager"}); err != nil {
					t.Fatal(err)
				}
				service, err := manager.CreateService("heartbeat", json.RawMessage(`{"interval":"1s"}`), &Dependencies{})
				if err != nil {
					t.Fatal(err)
				}
				return service.(*HeartbeatService)
			},
			wantErr: "component health",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			hb := test.build(t)
			err := hb.Start(t.Context())
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Start() error = %v, want containing %q", err, test.wantErr)
			}
			if hb.Status() != StatusStopped {
				t.Fatalf("Status() = %v, want stopped after rejected Start", hb.Status())
			}
		})
	}
}

func TestHeartbeatService_ContextCancellation(t *testing.T) {
	hb := newManagedHeartbeatForTest(t, nil)

	ctx, cancel := context.WithCancel(context.Background())

	if err := hb.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Cancel context - this will cause the heartbeat loop to exit
	cancel()

	// Wait for contextMonitor to observe cancellation and self-stop the service
	deadline := time.Now().Add(2 * time.Second)
	for hb.Status() != StatusStopped {
		if time.Now().After(deadline) {
			t.Fatalf("Status() = %v, want %v after context cancellation", hb.Status(), StatusStopped)
		}
		time.Sleep(5 * time.Millisecond)
	}

	// Stop after cancellation already won the race is a clean shutdown and
	// still completes ticker/goroutine teardown (gh#549).
	if err := hb.Stop(context.Background()); err != nil && !errors.Is(err, ErrAlreadyStopped) {
		t.Errorf("Stop() after context cancellation = %v, want nil or ErrAlreadyStopped", err)
	}

	if hb.Status() != StatusStopped {
		t.Errorf("Status() = %v, want %v after context cancellation", hb.Status(), StatusStopped)
	}
}

func TestHeartbeatService_EmitHeartbeat(t *testing.T) {
	mockHealth := &mockComponentHealthGetter{
		health: map[string]component.HealthStatus{
			"comp1": {Healthy: true},
			"comp2": {Healthy: true},
			"comp3": {Healthy: false},
			"comp4": {Healthy: true},
		},
	}

	hb, err := newHeartbeatServiceForTest(&HeartbeatConfig{Interval: "1s"}, mockHealth)
	if err != nil {
		t.Fatalf("newHeartbeatServiceForTest() error = %v", err)
	}

	hb.startTime = time.Now()
	var logs bytes.Buffer
	hb.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	hb.emitHeartbeat()
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(logs.Bytes()), &record); err != nil {
		t.Fatalf("decode heartbeat log: %v\n%s", err, logs.String())
	}
	if got := int(record["components_healthy"].(float64)); got != 3 {
		t.Fatalf("components_healthy = %d, want 3", got)
	}
	if got := int(record["components_total"].(float64)); got != 4 {
		t.Fatalf("components_total = %d, want 4", got)
	}

	// Test with nil component manager
	hb2, err := newHeartbeatServiceForTest(&HeartbeatConfig{Interval: "1s"}, nil)
	if err != nil {
		t.Fatalf("newHeartbeatServiceForTest() error = %v", err)
	}
	hb2.startTime = time.Now()
	hb2.emitHeartbeat()
}

func TestHeartbeatService_StartEmitsResolvedComponentHealth(t *testing.T) {
	hb := newManagedHeartbeatForTest(t, map[string]component.HealthStatus{
		"healthy":   {Healthy: true},
		"unhealthy": {Healthy: false},
	})
	writer := &heartbeatLogWriter{records: make(chan []byte, 8)}
	hb.logger = slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	if err := hb.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := hb.Stop(context.Background()); err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	for {
		select {
		case encoded := <-writer.records:
			var record map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(encoded), &record); err != nil {
				t.Fatalf("decode heartbeat log: %v", err)
			}
			if record["msg"] != "System heartbeat" {
				continue
			}
			if got := int(record["components_healthy"].(float64)); got != 1 {
				t.Fatalf("components_healthy = %d, want 1", got)
			}
			if got := int(record["components_total"].(float64)); got != 2 {
				t.Fatalf("components_total = %d, want 2", got)
			}
			return
		case <-ctx.Done():
			t.Fatalf("initial System heartbeat not observed: %v", ctx.Err())
		}
	}
}

func TestHeartbeatService_Name(t *testing.T) {
	hb, err := newHeartbeatServiceForTest(&HeartbeatConfig{Interval: "1s"}, nil)
	if err != nil {
		t.Fatalf("newHeartbeatServiceForTest() error = %v", err)
	}

	if hb.Name() != "heartbeat" {
		t.Errorf("Name() = %v, want heartbeat", hb.Name())
	}
}
