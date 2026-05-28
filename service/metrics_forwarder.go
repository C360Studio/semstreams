// Package service provides the MetricsForwarder service for forwarding metrics to NATS
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/c360studio/semstreams/metric"
	dto "github.com/prometheus/client_model/go"
)

// MetricsGatherer defines the interface for gathering metrics.
// This allows for easier testing with mocks.
type MetricsGatherer interface {
	Gather() ([]*dto.MetricFamily, error)
}

// NewMetricsForwarderService creates a new metrics forwarder service using the standard constructor pattern
func NewMetricsForwarderService(rawConfig json.RawMessage, deps *Dependencies) (Service, error) {
	// Parse config - handle empty or invalid JSON properly
	var cfg MetricsForwarderConfig
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &cfg); err != nil {
			return nil, fmt.Errorf("parse metrics-forwarder config: %w", err)
		}
	}

	// Check if push_interval was explicitly set to empty string (invalid)
	// We need to do this before applying defaults
	var rawCfgMap map[string]interface{}
	if len(rawConfig) > 0 {
		if err := json.Unmarshal(rawConfig, &rawCfgMap); err == nil {
			if interval, exists := rawCfgMap["push_interval"]; exists {
				if strInterval, ok := interval.(string); ok && strInterval == "" {
					return nil, fmt.Errorf("validate metrics-forwarder config: invalid push interval: cannot be empty")
				}
			}
		}
	}

	// Apply defaults
	if cfg.PushInterval == "" {
		cfg.PushInterval = "5s"
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate metrics-forwarder config: %w", err)
	}

	// Check required dependencies
	if deps.NATSClient == nil {
		return nil, fmt.Errorf("metrics-forwarder requires NATS client")
	}
	if deps.MetricsRegistry == nil {
		return nil, fmt.Errorf("metrics-forwarder requires metrics registry")
	}

	// Create the MetricsForwarder with dependencies
	var opts []Option
	if deps.Logger != nil {
		opts = append(opts, WithLogger(deps.Logger))
	}
	if deps.MetricsRegistry != nil {
		opts = append(opts, WithMetrics(deps.MetricsRegistry))
	}

	// The NATS client should implement the publisher interface
	publisher, ok := interface{}(deps.NATSClient).(natsPublisher)
	if !ok {
		return nil, fmt.Errorf("NATS client does not implement PublishToStream method")
	}

	return newMetricsForwarderWithPublisher(&cfg, publisher, deps.MetricsRegistry, opts...)
}

// MetricsForwarderConfig holds configuration for the MetricsForwarder service
// Note: The service is enabled/disabled via types.ServiceConfig.Enabled at the outer level.
// If the service is created, it will forward metrics.
type MetricsForwarderConfig struct {
	// Push interval for metrics publishing (e.g., "5s", "1m")
	PushInterval string `json:"push_interval"`

	// IncludeGoMetrics enables forwarding of go_* runtime metrics (goroutines, memory, GC)
	// Default: false (excluded to reduce noise)
	IncludeGoMetrics bool `json:"include_go_metrics"`

	// IncludeProcMetrics enables forwarding of process_* metrics (CPU, open FDs, memory)
	// Default: false (excluded to reduce noise)
	IncludeProcMetrics bool `json:"include_proc_metrics"`
}

// Validate checks if the configuration is valid
func (c MetricsForwarderConfig) Validate() error {
	// Validate push interval - empty string is not valid
	if c.PushInterval == "" {
		return fmt.Errorf("invalid push interval: cannot be empty")
	}

	duration, err := time.ParseDuration(c.PushInterval)
	if err != nil {
		return fmt.Errorf("invalid push interval: %w", err)
	}
	if duration <= 0 {
		return fmt.Errorf("invalid push interval: must be positive")
	}

	return nil
}

// MetricsForwarder implements periodic metrics publishing to NATS
type MetricsForwarder struct {
	*BaseService

	config MetricsForwarderConfig

	// NATS publisher for publishing (interface for testability)
	publisher natsPublisher

	// Metrics gatherer (interface for testability)
	gatherer MetricsGatherer

	// Push interval duration
	pushInterval time.Duration

	// Ticker for periodic publishing
	ticker *time.Ticker

	// Stop channel for goroutine coordination
	stopChan chan struct{}

	// WaitGroup for goroutine tracking
	wg sync.WaitGroup

	// Internal logger
	logger *slog.Logger
}

// newMetricsForwarderWithPublisher creates a new MetricsForwarder with a custom publisher (for testing)
func newMetricsForwarderWithPublisher(
	config *MetricsForwarderConfig,
	publisher natsPublisher,
	registry *metric.MetricsRegistry,
	opts ...Option,
) (*MetricsForwarder, error) {
	if config == nil {
		config = &MetricsForwarderConfig{
			PushInterval: "5s",
		}
	}

	// Parse push interval
	pushInterval, err := time.ParseDuration(config.PushInterval)
	if err != nil {
		return nil, fmt.Errorf("invalid push interval: %w", err)
	}

	// Create base service
	baseService := NewBaseServiceWithOptions("metrics-forwarder", nil, opts...)

	// Use registry as gatherer (it implements Gather via PrometheusRegistry)
	var gatherer MetricsGatherer
	if registry != nil {
		gatherer = registry.PrometheusRegistry()
	}

	mf := &MetricsForwarder{
		BaseService:  baseService,
		config:       *config,
		publisher:    publisher,
		gatherer:     gatherer,
		pushInterval: pushInterval,
		stopChan:     make(chan struct{}),
		logger:       slog.Default().With("component", "metrics-forwarder"),
	}

	return mf, nil
}

// Start begins metrics forwarding
func (mf *MetricsForwarder) Start(ctx context.Context) error {
	// Check if already running
	if mf.Status() == StatusRunning {
		return fmt.Errorf("metrics forwarder already running")
	}

	if err := mf.BaseService.Start(ctx); err != nil {
		return err
	}

	mf.logger.Info("MetricsForwarder started",
		"push_interval", mf.config.PushInterval)

	// Start publishing loop
	mf.ticker = time.NewTicker(mf.pushInterval)
	mf.wg.Add(1)
	go mf.publishLoop(ctx)

	return nil
}

// Stop gracefully stops the MetricsForwarder
func (mf *MetricsForwarder) Stop(timeout time.Duration) error {
	// Check if not running
	status := mf.Status()
	if status != StatusRunning && status != StatusStarting {
		return fmt.Errorf("metrics forwarder not running (status: %v)", status)
	}

	mf.logger.Info("MetricsForwarder stopping")

	// Stop the ticker if it exists
	if mf.ticker != nil {
		mf.ticker.Stop()
	}

	// Signal stop and wait for goroutine
	close(mf.stopChan)

	// Wait for publishing goroutine with timeout
	done := make(chan struct{})
	go func() {
		mf.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Goroutine finished
	case <-time.After(timeout):
		// Timeout waiting for goroutine
		mf.logger.Warn("MetricsForwarder stop timeout waiting for goroutine")
	}

	return mf.BaseService.Stop(timeout)
}

// publishLoop periodically publishes metrics to NATS.
//
// Fires an immediate first publish before entering the tick loop so the
// METRICS JetStream stream is seeded as soon as MetricsForwarder.Start
// completes. Otherwise fresh clients connecting between T+0 and T+pushInterval
// see an empty stream under last_per_subject delivery and time out waiting
// for `component_metrics` envelopes (e2e dataflow flake — see
// project_websocket_flake_diagnosis).
func (mf *MetricsForwarder) publishLoop(ctx context.Context) {
	defer mf.wg.Done()

	mf.publishMetrics(ctx) // seed stream immediately

	for {
		select {
		case <-mf.stopChan:
			return
		case <-mf.ticker.C:
			mf.publishMetrics(ctx)
		}
	}
}

// publishMetrics gathers and publishes all metrics to NATS
func (mf *MetricsForwarder) publishMetrics(ctx context.Context) {
	if mf.gatherer == nil {
		mf.logger.Debug("metrics gatherer not available")
		return
	}

	// Gather metrics from registry
	metricFamilies, err := mf.gatherer.Gather()
	if err != nil {
		mf.logger.Debug("failed to gather metrics", "error", err)
		return
	}

	// Process each metric family
	for _, family := range metricFamilies {
		mf.processMetricFamily(ctx, family)
	}
}

// processMetricFamily processes a single metric family and publishes its metrics
func (mf *MetricsForwarder) processMetricFamily(ctx context.Context, family *dto.MetricFamily) {
	if family == nil || family.Name == nil {
		return
	}

	metricName := *family.Name

	// Check if this metric should be skipped based on config
	if mf.shouldSkipMetric(metricName) {
		return
	}

	metricType := mf.metricTypeToString(family.Type)

	// Process each metric in the family
	for _, metric := range family.Metric {
		mf.processMetric(ctx, metricName, metricType, metric)
	}
}

// shouldSkipMetric checks if a metric should be skipped based on config.
// Go runtime (go_*) and process (process_*) metrics are opt-in via config.
// All application metrics (semstreams_* and others) are always included.
func (mf *MetricsForwarder) shouldSkipMetric(name string) bool {
	// Skip Go runtime metrics unless explicitly included
	if len(name) >= 3 && name[:3] == "go_" {
		return !mf.config.IncludeGoMetrics
	}
	// Skip process metrics unless explicitly included
	if len(name) >= 8 && name[:8] == "process_" {
		return !mf.config.IncludeProcMetrics
	}
	// Include all other metrics (application metrics like semstreams_*)
	return false
}

// processMetric processes a single metric and publishes it to NATS
func (mf *MetricsForwarder) processMetric(ctx context.Context, name string, metricType string, metric *dto.Metric) {
	// Extract labels
	labels := make(map[string]string)
	for _, label := range metric.Label {
		if label.Name != nil && label.Value != nil {
			labels[*label.Name] = *label.Value
		}
	}

	// Extract component from labels (fallback to "system")
	component := "system"
	if comp, ok := labels["component"]; ok {
		component = comp
	}

	// Extract value based on metric type
	value := mf.extractMetricValue(metric)

	// Build NATS subject: metrics.{component}.{metric_name}
	subject := fmt.Sprintf("metrics.%s.%s", component, name)

	// Build metrics entry
	entry := map[string]interface{}{
		"timestamp": time.Now().UnixMilli(),
		"name":      name,
		"component": component,
		"type":      metricType,
		"value":     value,
		"labels":    labels,
	}

	// Marshal to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		mf.logger.Debug("failed to marshal metrics entry", "error", err)
		return
	}

	// Publish to NATS JetStream for persistence and consistency with other observability streams
	if err := mf.publisher.PublishToStream(ctx, subject, data); err != nil {
		mf.logger.Debug("failed to publish metrics to NATS",
			"subject", subject,
			"error", err)
	}
}

// extractMetricValue extracts the numeric value from a metric
func (mf *MetricsForwarder) extractMetricValue(metric *dto.Metric) float64 {
	switch {
	case metric.Counter != nil && metric.Counter.Value != nil:
		return *metric.Counter.Value
	case metric.Gauge != nil && metric.Gauge.Value != nil:
		return *metric.Gauge.Value
	case metric.Histogram != nil && metric.Histogram.SampleCount != nil:
		return float64(*metric.Histogram.SampleCount)
	case metric.Summary != nil && metric.Summary.SampleCount != nil:
		return float64(*metric.Summary.SampleCount)
	case metric.Untyped != nil && metric.Untyped.Value != nil:
		return *metric.Untyped.Value
	default:
		return 0
	}
}

// metricTypeToString converts a Prometheus metric type to a string
func (mf *MetricsForwarder) metricTypeToString(metricType *dto.MetricType) string {
	if metricType == nil {
		return "untyped"
	}

	switch *metricType {
	case dto.MetricType_COUNTER:
		return "counter"
	case dto.MetricType_GAUGE:
		return "gauge"
	case dto.MetricType_HISTOGRAM:
		return "histogram"
	case dto.MetricType_SUMMARY:
		return "summary"
	case dto.MetricType_UNTYPED:
		return "untyped"
	default:
		return "untyped"
	}
}

// newMetricsForwarderForTest creates a MetricsForwarder for testing with a mock publisher.
// This is used by test helpers to bypass the Dependencies type constraint.
func newMetricsForwarderForTest(
	config *MetricsForwarderConfig,
	publisher natsPublisher,
	registry *metric.MetricsRegistry,
) (*MetricsForwarder, error) {
	return newMetricsForwarderWithPublisher(config, publisher, registry)
}

// newMetricsForwarderForTestWithMockRegistry creates a MetricsForwarder for testing with a mock gatherer.
// This allows tests to inject a custom gatherer that returns errors or specific metrics.
func newMetricsForwarderForTestWithMockRegistry(
	config *MetricsForwarderConfig,
	publisher natsPublisher,
	gatherer MetricsGatherer,
) (*MetricsForwarder, error) {
	if config == nil {
		config = &MetricsForwarderConfig{
			PushInterval: "5s",
		}
	}

	// Parse push interval
	pushInterval, err := time.ParseDuration(config.PushInterval)
	if err != nil {
		return nil, fmt.Errorf("invalid push interval: %w", err)
	}

	// Create base service
	baseService := NewBaseServiceWithOptions("metrics-forwarder", nil)

	mf := &MetricsForwarder{
		BaseService:  baseService,
		config:       *config,
		publisher:    publisher,
		gatherer:     gatherer,
		pushInterval: pushInterval,
		stopChan:     make(chan struct{}),
		logger:       slog.Default().With("component", "metrics-forwarder"),
	}

	return mf, nil
}
