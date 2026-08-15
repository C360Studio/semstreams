package otel

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
)

func TestNewComponent(t *testing.T) {
	cfg := DefaultConfig()

	rawConfig, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("failed to marshal config: %v", err)
	}

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	if comp == nil {
		t.Fatal("expected component, got nil")
	}

	// Verify it implements Discoverable
	discoverable, ok := comp.(component.Discoverable)
	if !ok {
		t.Fatal("component does not implement Discoverable")
	}

	meta := discoverable.Meta()
	if meta.Name != "otel-exporter" {
		t.Errorf("expected name 'otel-exporter', got %q", meta.Name)
	}

	if meta.Type != "output" {
		t.Errorf("expected type 'output', got %q", meta.Type)
	}
}

func TestNewComponentInvalidConfig(t *testing.T) {
	tests := []struct {
		name      string
		rawConfig string
		wantErr   bool
	}{
		{
			name:      "invalid json",
			rawConfig: `{not valid json}`,
			wantErr:   true,
		},
		{
			name:      "invalid protocol",
			rawConfig: `{"ports": {}, "protocol": "websocket"}`,
			wantErr:   true,
		},
		{
			name:      "invalid sampling rate",
			rawConfig: `{"ports": {}, "sampling_rate": 2.0}`,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := component.Dependencies{}

			_, err := NewComponent([]byte(tt.rawConfig), deps)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestComponentInitialize(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)
	if err := otelComp.Initialize(); err != nil {
		t.Errorf("Initialize failed: %v", err)
	}

	// Verify span collector was created
	if otelComp.spanCollector == nil {
		t.Error("span collector should be created during Initialize")
	}

	// Verify metric mapper was created
	if otelComp.metricMapper == nil {
		t.Error("metric mapper should be created during Initialize")
	}
}

func TestComponentStartWithoutNATSClient(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{
		// NATSClient is nil
	}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)
	_ = otelComp.Initialize()

	ctx := context.Background()
	err = otelComp.Start(ctx)
	if err == nil {
		t.Error("expected error when starting without NATS client")
	}
}

func TestComponentStartNilContext(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)
	_ = otelComp.Initialize()

	// nolint:staticcheck // Testing nil context behavior
	err = otelComp.Start(nil)
	if err == nil {
		t.Error("expected error when starting with nil context")
	}
}

func TestComponentStartCancelledContext(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)
	_ = otelComp.Initialize()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err = otelComp.Start(ctx)
	if err == nil {
		t.Error("expected error when starting with cancelled context")
	}
}

func TestComponentMeta(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)
	meta := otelComp.Meta()

	if meta.Name != "otel-exporter" {
		t.Errorf("expected name 'otel-exporter', got %q", meta.Name)
	}
	if meta.Type != "output" {
		t.Errorf("expected type 'output', got %q", meta.Type)
	}
	if meta.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", meta.Version)
	}
}

func TestComponentInputPorts(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)
	ports := otelComp.InputPorts()

	if len(ports) == 0 {
		t.Error("expected at least one input port")
	}

	// Verify first port
	if len(ports) > 0 {
		port := ports[0]
		if port.Direction != component.DirectionInput {
			t.Errorf("expected input direction, got %v", port.Direction)
		}
	}
}

func TestComponentOutputPorts(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)
	ports := otelComp.OutputPorts()

	// OTEL exporter has no NATS output ports
	if len(ports) != 0 {
		t.Errorf("expected 0 output ports, got %d", len(ports))
	}
}

func TestComponentHealth(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)

	// Before starting
	health := otelComp.Health()
	if health.Healthy {
		t.Error("expected unhealthy before start")
	}
	if health.Status != "stopped" {
		t.Errorf("expected status 'stopped', got %q", health.Status)
	}
}

func TestComponentDataFlow(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)
	flow := otelComp.DataFlow()

	// No activity yet
	if flow.ErrorRate != 0 {
		t.Errorf("expected error rate 0, got %f", flow.ErrorRate)
	}
}

func TestComponentStopWhenNotRunning(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)

	// Stop when not running should not error
	err = otelComp.Stop(5 * time.Second)
	if err != nil {
		t.Errorf("Stop should not error when not running: %v", err)
	}
}

func TestDirectPolicyObservationPrecedesFetchAndCleanupFollowsFetchExit(t *testing.T) {
	events := make(chan string, 4)
	ctx, cancel := context.WithCancel(context.Background())
	comp := &Component{
		running: true,
		cancel:  cancel,
		logger:  slog.Default(),
		observePolicy: func(
			context.Context,
			natsclient.PortConsumerContext,
			jetstream.ConsumerConfig,
			jetstream.Consumer,
		) (func(), error) {
			events <- "observe"
			return func() { events <- "cleanup" }, nil
		},
		consumeFrom: func(fetchCtx context.Context, _ jetstream.Consumer) {
			events <- "fetch-start"
			<-fetchCtx.Done()
			events <- "fetch-exit"
		},
	}

	observed, err := comp.prepareObservedSubscription(ctx,
		natsclient.PortConsumerContext{Component: "otel-exporter", Port: "agent_events"},
		jetstream.ConsumerConfig{MaxAckPending: 4}, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertOTELPolicyEvent(t, events, "observe")
	comp.startObservedSubscription(ctx, observed)
	assertOTELPolicyEvent(t, events, "fetch-start")

	if err := comp.Stop(time.Second); err != nil {
		t.Fatal(err)
	}
	assertOTELPolicyEvent(t, events, "fetch-exit")
	assertOTELPolicyEvent(t, events, "cleanup")
}

func assertOTELPolicyEvent(t *testing.T, events <-chan string, want string) {
	t.Helper()
	select {
	case got := <-events:
		if got != want {
			t.Fatalf("event = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %q", want)
	}
}

func TestComponentConfigSchema(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)
	schema := otelComp.ConfigSchema()

	// Should have properties
	if len(schema.Properties) == 0 {
		t.Error("expected schema to have properties")
	}
}

func TestComponentSetExporter(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)
	_ = otelComp.Initialize()

	// Set mock exporter
	mockExp := &MockExporter{}
	otelComp.SetExporter(mockExp)

	if otelComp.exporter != mockExp {
		t.Error("expected exporter to be set")
	}
}

func TestExportCountersAdvanceOnlyAfterExporterAcceptsBatch(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)
	created, err := NewComponent(rawConfig, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	c := created.(*Component)
	if err := c.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	exporter := &MockExporter{ExportMetricsErr: errors.New("collector unavailable")}
	c.SetExporter(exporter)
	c.metricMapper.RecordCounter("attempt", "", "1", 1, nil)
	c.exportData(context.Background())
	if c.metricsExported != 0 {
		t.Fatalf("metrics_exported = %d after rejected batch, want 0", c.metricsExported)
	}

	exporter.ExportMetricsErr = nil
	c.metricMapper.RecordCounter("accepted", "", "1", 1, nil)
	c.exportData(context.Background())
	if c.metricsExported != 1 {
		t.Fatalf("metrics_exported = %d after accepted batch, want 1", c.metricsExported)
	}
}

func TestExportDataBoundsEachFlushWithExportTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ExportTraces = false
	cfg.ExportTimeout = "20ms"
	rawConfig, _ := json.Marshal(cfg)
	created, err := NewComponent(rawConfig, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	c := created.(*Component)
	if err := c.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	exporter := newHangingExporter()
	defer close(exporter.release)
	c.SetExporter(exporter)
	c.metricMapper.RecordCounter("bounded", "", "1", 1, nil)

	done := make(chan struct{})
	go func() {
		c.exportData(context.Background())
		close(done)
	}()

	call := <-exporter.started
	if !call.hasDeadline {
		t.Fatal("periodic export context has no deadline")
	}
	if call.initiallyCanceled {
		t.Fatal("periodic export context was already canceled")
	}
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("hanging exporter exceeded export_timeout")
	}
	if c.metricsExported != 0 {
		t.Fatalf("metrics_exported = %d after timed-out batch, want 0", c.metricsExported)
	}
	if c.errors != 1 {
		t.Fatalf("errors = %d after timed-out batch, want 1", c.errors)
	}
}

func TestExportLoopFinalFlushUsesFreshBoundedContext(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ExportTraces = false
	cfg.ExportTimeout = "20ms"
	rawConfig, _ := json.Marshal(cfg)
	created, err := NewComponent(rawConfig, component.Dependencies{})
	if err != nil {
		t.Fatalf("NewComponent: %v", err)
	}
	c := created.(*Component)
	if err := c.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	exporter := newHangingExporter()
	defer close(exporter.release)
	c.SetExporter(exporter)
	c.metricMapper.RecordCounter("shutdown", "", "1", 1, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.wg.Add(1)
	done := make(chan struct{})
	go func() {
		c.exportLoop(ctx)
		close(done)
	}()

	call := <-exporter.started
	if !call.hasDeadline {
		t.Fatal("shutdown flush context has no deadline")
	}
	if call.initiallyCanceled {
		t.Fatal("shutdown flush reused the canceled component context")
	}
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("final flush exceeded export_timeout")
	}
}

func TestComponentGetSpanCollector(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)
	_ = otelComp.Initialize()

	sc := otelComp.GetSpanCollector()
	if sc == nil {
		t.Error("expected span collector, got nil")
	}
}

func TestComponentGetMetricMapper(t *testing.T) {
	cfg := DefaultConfig()
	rawConfig, _ := json.Marshal(cfg)

	deps := component.Dependencies{}

	comp, err := NewComponent(rawConfig, deps)
	if err != nil {
		t.Fatalf("NewComponent failed: %v", err)
	}

	otelComp := comp.(*Component)
	_ = otelComp.Initialize()

	mm := otelComp.GetMetricMapper()
	if mm == nil {
		t.Error("expected metric mapper, got nil")
	}
}

func TestRegister(t *testing.T) {
	tests := []struct {
		name     string
		registry RegistryInterface
		wantErr  bool
	}{
		{
			name:     "nil registry",
			registry: nil,
			wantErr:  true,
		},
		{
			name:     "valid registry",
			registry: &mockRegistry{},
			wantErr:  false,
		},
		{
			name:     "registry returns error",
			registry: &mockRegistry{err: errMock},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Register(tt.registry)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// MockExporter implements Exporter for testing.
type MockExporter struct {
	mu sync.Mutex

	SpansExported   []*SpanData
	MetricsExported []*MetricData

	ExportSpansErr   error
	ExportMetricsErr error
	ShutdownErr      error

	shutdownCalled bool
}

type exportCall struct {
	hasDeadline       bool
	initiallyCanceled bool
}

type hangingExporter struct {
	started chan exportCall
	release chan struct{}
}

func newHangingExporter() *hangingExporter {
	return &hangingExporter{
		started: make(chan exportCall, 1),
		release: make(chan struct{}),
	}
}

func (e *hangingExporter) ExportSpans(ctx context.Context, _ []*SpanData) error {
	return e.block(ctx)
}

func (e *hangingExporter) ExportMetrics(ctx context.Context, _ []*MetricData) error {
	return e.block(ctx)
}

func (e *hangingExporter) block(ctx context.Context) error {
	_, hasDeadline := ctx.Deadline()
	e.started <- exportCall{hasDeadline: hasDeadline, initiallyCanceled: ctx.Err() != nil}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.release:
		return nil
	}
}

func (e *hangingExporter) Shutdown(context.Context) error { return nil }

func (m *MockExporter) ExportSpans(_ context.Context, spans []*SpanData) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ExportSpansErr != nil {
		return m.ExportSpansErr
	}

	m.SpansExported = append(m.SpansExported, spans...)
	return nil
}

func (m *MockExporter) ExportMetrics(_ context.Context, metrics []*MetricData) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ExportMetricsErr != nil {
		return m.ExportMetricsErr
	}

	m.MetricsExported = append(m.MetricsExported, metrics...)
	return nil
}

func (m *MockExporter) Shutdown(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.shutdownCalled = true
	return m.ShutdownErr
}

func (m *MockExporter) GetExportedSpans() []*SpanData {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*SpanData, len(m.SpansExported))
	copy(result, m.SpansExported)
	return result
}

func (m *MockExporter) GetExportedMetrics() []*MetricData {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*MetricData, len(m.MetricsExported))
	copy(result, m.MetricsExported)
	return result
}

func (m *MockExporter) WasShutdownCalled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shutdownCalled
}

// mockRegistry implements RegistryInterface for testing.
type mockRegistry struct {
	err error
}

var errMock = &mockError{msg: "mock error"}

type mockError struct {
	msg string
}

func (e *mockError) Error() string {
	return e.msg
}

func (m *mockRegistry) RegisterWithConfig(_ component.RegistrationConfig) error {
	return m.err
}
