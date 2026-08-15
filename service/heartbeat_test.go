package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// mockComponentHealthGetter implements componentHealthGetter for testing
type mockComponentHealthGetter struct {
	health map[string]bool
}

func (m *mockComponentHealthGetter) GetComponentHealth() map[string]bool {
	return m.health
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
	hb, err := newHeartbeatServiceForTest(&HeartbeatConfig{Interval: "100ms"}, nil)
	if err != nil {
		t.Fatalf("newHeartbeatServiceForTest() error = %v", err)
	}

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

	// Wait for at least one heartbeat tick
	time.Sleep(150 * time.Millisecond)

	// Stop service
	if err := hb.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if hb.Status() != StatusStopped {
		t.Errorf("Status() = %v, want %v", hb.Status(), StatusStopped)
	}
}

func TestHeartbeatService_StartAlreadyRunning(t *testing.T) {
	hb, err := newHeartbeatServiceForTest(&HeartbeatConfig{Interval: "1s"}, nil)
	if err != nil {
		t.Fatalf("newHeartbeatServiceForTest() error = %v", err)
	}

	ctx := context.Background()

	// Start service
	if err := hb.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer hb.Stop(context.Background())

	// Try to start again
	err = hb.Start(ctx)
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
	hb, err := newHeartbeatServiceForTest(&HeartbeatConfig{Interval: "1s"}, nil)
	if err != nil {
		t.Fatalf("newHeartbeatServiceForTest() error = %v", err)
	}

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
	hb, err := newHeartbeatServiceForTest(&HeartbeatConfig{Interval: "1s"}, nil)
	if err != nil {
		t.Fatalf("newHeartbeatServiceForTest() error = %v", err)
	}

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
	mockHealth := &mockComponentHealthGetter{
		health: map[string]bool{
			"component1": true,
			"component2": true,
			"component3": false,
		},
	}

	hb, err := newHeartbeatServiceForTest(&HeartbeatConfig{Interval: "100ms"}, mockHealth)
	if err != nil {
		t.Fatalf("newHeartbeatServiceForTest() error = %v", err)
	}

	ctx := context.Background()

	if err := hb.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Let it emit a heartbeat
	time.Sleep(150 * time.Millisecond)

	if err := hb.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// The test verifies no panics occur when component manager is present
	// Actual log output would need to be captured for verification
}

func TestHeartbeatService_ContextCancellation(t *testing.T) {
	hb, err := newHeartbeatServiceForTest(&HeartbeatConfig{Interval: "1s"}, nil)
	if err != nil {
		t.Fatalf("newHeartbeatServiceForTest() error = %v", err)
	}

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
		health: map[string]bool{
			"comp1": true,
			"comp2": true,
			"comp3": false,
			"comp4": true,
		},
	}

	hb, err := newHeartbeatServiceForTest(&HeartbeatConfig{Interval: "1s"}, mockHealth)
	if err != nil {
		t.Fatalf("newHeartbeatServiceForTest() error = %v", err)
	}

	hb.startTime = time.Now()

	// Call emitHeartbeat directly - should not panic
	hb.emitHeartbeat()

	// Test with nil component manager
	hb2, err := newHeartbeatServiceForTest(&HeartbeatConfig{Interval: "1s"}, nil)
	if err != nil {
		t.Fatalf("newHeartbeatServiceForTest() error = %v", err)
	}
	hb2.startTime = time.Now()
	hb2.emitHeartbeat() // Should not panic with nil componentManager
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
