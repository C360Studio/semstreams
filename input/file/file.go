// Package file provides a file input component for reading JSONL files and publishing to NATS
package file

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/prometheus/client_golang/prometheus"
)

// Buffer size constants for scanner
const (
	scannerInitialBuffer = 1 * 1024 * 1024  // 1MB initial buffer
	scannerMaxBuffer     = 10 * 1024 * 1024 // 10MB max buffer
	contextCheckInterval = 100              // Check context every N lines
)

// Metrics holds Prometheus metrics for file input component
type Metrics struct {
	linesRead      prometheus.Counter
	linesPublished prometheus.Counter
	bytesRead      prometheus.Counter
	parseErrors    prometheus.Counter
	filesProcessed prometheus.Counter
}

// newMetrics creates and registers file input metrics
func newMetrics(registry *metric.MetricsRegistry, name string) *Metrics {
	if registry == nil {
		return nil
	}

	metrics := &Metrics{
		linesRead: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "file_input",
			Name:      "lines_read_total",
			Help:      "Total lines read from files",
		}),
		linesPublished: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "file_input",
			Name:      "lines_published_total",
			Help:      "Total lines published to NATS",
		}),
		bytesRead: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "file_input",
			Name:      "bytes_read_total",
			Help:      "Total bytes read from files",
		}),
		parseErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "file_input",
			Name:      "parse_errors_total",
			Help:      "Total JSON parse errors",
		}),
		filesProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "semstreams",
			Subsystem: "file_input",
			Name:      "files_processed_total",
			Help:      "Total files processed",
		}),
	}

	serviceName := fmt.Sprintf("file_input_%s", name)
	registry.RegisterCounter(serviceName, "lines_read", metrics.linesRead)
	registry.RegisterCounter(serviceName, "lines_published", metrics.linesPublished)
	registry.RegisterCounter(serviceName, "bytes_read", metrics.bytesRead)
	registry.RegisterCounter(serviceName, "parse_errors", metrics.parseErrors)
	registry.RegisterCounter(serviceName, "files_processed", metrics.filesProcessed)

	return metrics
}

// Input implements a file reader that publishes lines to NATS
type Input struct {
	name            string
	path            string
	format          string
	interval        time.Duration
	loop            bool
	subject         string
	config          Config // Store full config for port type checking
	inputPorts      []component.Port
	outputPorts     []component.Port
	jetStreamOutput bool
	natsClient      *natsclient.Client
	logger          *slog.Logger

	// Lifecycle management - use separate mutex for lifecycle operations
	lifecycleMu sync.Mutex // Protects start/stop operations
	shutdown    chan struct{}
	done        chan struct{}
	running     atomic.Bool
	startTime   time.Time
	mu          sync.RWMutex // Protects data access
	wg          sync.WaitGroup

	// Metrics (atomic for thread safety)
	linesRead      atomic.Int64
	linesPublished atomic.Int64
	bytesRead      atomic.Int64
	errors         atomic.Int64

	// Prometheus metrics
	metrics *Metrics

	// Scanner buffer pool for memory efficiency
	scannerBufferPool *sync.Pool
}

// Ensure Input implements all required interfaces
var _ component.Discoverable = (*Input)(nil)
var _ component.LifecycleComponent = (*Input)(nil)

// fileSchema defines the configuration schema for file input component
var fileSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Config holds configuration for file input component
type Config struct {
	Ports    *component.PortConfig `json:"ports" schema:"type:ports,description:Port configuration,category:basic"`
	Path     string                `json:"path" schema:"type:string,description:File path or glob pattern,required:true"`
	Format   string                `json:"format" schema:"type:string,description:File format (jsonl or json),default:jsonl"`
	Interval string                `json:"interval" schema:"type:string,description:Delay between lines,default:10ms"`
	Loop     bool                  `json:"loop" schema:"type:boolean,description:Loop file when complete,default:false"`
}

// Validate implements component.Validatable interface
func (c *Config) Validate() error {
	if c.Path == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Config", "Validate", "path is required")
	}

	if c.Format != "" && c.Format != "jsonl" && c.Format != "json" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "format must be 'jsonl' or 'json'")
	}

	if c.Interval != "" {
		if _, err := time.ParseDuration(c.Interval); err != nil {
			return errs.WrapInvalid(err, "Config", "Validate", "invalid interval")
		}
	}

	if c.Ports == nil || len(c.Ports.Outputs) != 1 {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Config", "Validate", "exactly one output port is required")
	}
	output, err := c.Ports.Outputs[0].Resolve(component.DirectionOutput)
	if err != nil {
		return errs.WrapInvalid(err, "Config", "Validate", "resolve output port")
	}
	facts, err := output.Facts()
	if err != nil {
		return errs.WrapInvalid(err, "Config", "Validate", "project output port facts")
	}
	if facts.Kind() != component.PortKindNATS && facts.Kind() != component.PortKindJetStream {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "output port must be nats or jetstream")
	}
	if len(facts.NATSSubjects()) != 1 {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Config", "Validate", "output port must declare exactly one NATS subject")
	}

	return nil
}

// DefaultConfig returns default configuration
func DefaultConfig() Config {
	return Config{
		Format:   "jsonl",
		Interval: "10ms",
		Loop:     false,
	}
}

// getConfiguredPorts extracts port configuration
func (c *Config) getConfiguredPorts() (path, subject string, interval time.Duration, loop bool, inputPorts, outputPorts []component.Port, jetStreamOutput bool, err error) {
	path = c.Path

	// Default interval
	interval = 10 * time.Millisecond
	if c.Interval != "" {
		interval, err = time.ParseDuration(c.Interval)
		if err != nil {
			return "", "", 0, false, nil, nil, false, err
		}
	}

	loop = c.Loop

	input, err := (component.PortDefinition{
		Name: "file_source", Required: true,
		Description: fmt.Sprintf("File source: %s", path),
		Config:      component.FilePort{Path: path, Pattern: c.Format},
	}).Resolve(component.DirectionInput)
	if err != nil {
		return "", "", 0, false, nil, nil, false, err
	}
	output, err := c.Ports.Outputs[0].Resolve(component.DirectionOutput)
	if err != nil {
		return "", "", 0, false, nil, nil, false, err
	}
	facts, err := output.Facts()
	if err != nil {
		return "", "", 0, false, nil, nil, false, err
	}
	subject = facts.NATSSubjects()[0]

	return path, subject, interval, loop, []component.Port{input}, []component.Port{output}, facts.Kind() == component.PortKindJetStream, nil
}

// InputDeps holds runtime dependencies for file input component
type InputDeps struct {
	Name            string
	Config          Config
	NATSClient      *natsclient.Client
	MetricsRegistry *metric.MetricsRegistry
	Logger          *slog.Logger
}

// NewInput creates a new file input component
func NewInput(deps InputDeps) (*Input, error) {
	if err := deps.Config.Validate(); err != nil {
		return nil, err
	}
	path, subject, interval, loop, inputPorts, outputPorts, jetStreamOutput, err := deps.Config.getConfiguredPorts()
	if err != nil {
		return nil, errs.WrapInvalid(err, "file-input", "NewInput", "resolve configured ports")
	}

	var metrics *Metrics
	if deps.MetricsRegistry != nil {
		metrics = newMetrics(deps.MetricsRegistry, deps.Name)
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default().With("component", "file-input", "path", path)
	}

	return &Input{
		name:            deps.Name,
		path:            path,
		format:          deps.Config.Format,
		interval:        interval,
		loop:            loop,
		subject:         subject,
		config:          deps.Config, // Store full config for port type checking
		inputPorts:      inputPorts,
		outputPorts:     outputPorts,
		jetStreamOutput: jetStreamOutput,
		natsClient:      deps.NATSClient,
		logger:          logger,
		startTime:       time.Now(),
		metrics:         metrics,
		scannerBufferPool: &sync.Pool{
			New: func() any {
				buf := make([]byte, scannerInitialBuffer)
				return &buf
			},
		},
	}, nil
}

// Meta returns the component metadata
func (f *Input) Meta() component.Metadata {
	name := f.name
	if name == "" {
		name = fmt.Sprintf("file-input-%s", filepath.Base(f.path))
	}

	return component.Metadata{
		Name:        name,
		Type:        "input",
		Description: fmt.Sprintf("File input reading from %s publishing to %s", f.path, f.subject),
		Version:     "1.0.0",
	}
}

// InputPorts returns the input ports for this component
func (f *Input) InputPorts() []component.Port {
	return append([]component.Port(nil), f.inputPorts...)
}

// OutputPorts returns the output ports for this component
func (f *Input) OutputPorts() []component.Port {
	return append([]component.Port(nil), f.outputPorts...)
}

// ConfigSchema returns the configuration schema
func (f *Input) ConfigSchema() component.ConfigSchema {
	return fileSchema
}

// Health returns the current health status
func (f *Input) Health() component.HealthStatus {
	f.mu.RLock()
	running := f.running.Load()
	f.mu.RUnlock()

	errorCount := f.errors.Load()

	return component.HealthStatus{
		Healthy:    running,
		LastCheck:  time.Now(),
		ErrorCount: int(errorCount),
		Uptime:     time.Since(f.startTime),
	}
}

// DataFlow returns the current data flow metrics
func (f *Input) DataFlow() component.FlowMetrics {
	lines := f.linesPublished.Load()
	bytes := f.bytesRead.Load()
	errorCount := f.errors.Load()

	var messagesPerSecond float64
	var bytesPerSecond float64
	var errorRate float64

	if uptime := time.Since(f.startTime).Seconds(); uptime > 0 {
		messagesPerSecond = float64(lines) / uptime
		bytesPerSecond = float64(bytes) / uptime
	}

	if lines > 0 {
		errorRate = float64(errorCount) / float64(lines)
	}

	return component.FlowMetrics{
		MessagesPerSecond: messagesPerSecond,
		BytesPerSecond:    bytesPerSecond,
		ErrorRate:         errorRate,
		LastActivity:      time.Now(),
	}
}

// Initialize prepares the file input component
func (f *Input) Initialize() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.path == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "file-input", "Initialize", "path is required")
	}

	if f.subject == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "file-input", "Initialize", "subject is required")
	}

	if f.natsClient == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "file-input", "Initialize", "NATS client is required")
	}

	// Verify file exists (or glob pattern matches)
	matches, err := filepath.Glob(f.path)
	if err != nil {
		return errs.WrapInvalid(err, "file-input", "Initialize", "invalid path pattern")
	}
	if len(matches) == 0 {
		return errs.WrapInvalid(errs.ErrConfigNotFound, "file-input", "Initialize", "no files match path")
	}

	return nil
}

// Start begins reading files and publishing to NATS
func (f *Input) Start(ctx context.Context) error {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Input", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Input", "Start", "context already cancelled")
	}

	f.lifecycleMu.Lock()
	defer f.lifecycleMu.Unlock()

	if f.running.Load() {
		return nil // Already running
	}

	// Create channels before starting goroutine
	f.shutdown = make(chan struct{})
	f.done = make(chan struct{})

	// Set running state and add to waitgroup before spawning goroutine
	f.wg.Add(1)
	f.running.Store(true)

	// Spawn goroutine outside of any data mutex
	go f.readLoop(ctx)

	f.logger.Info("File input started", "path", f.path, "subject", f.subject)
	return nil
}

// Stop gracefully shuts down the file input
func (f *Input) Stop(timeout time.Duration) error {
	f.lifecycleMu.Lock()

	if !f.running.Load() {
		f.lifecycleMu.Unlock()
		return nil
	}

	// Set running to false first to prevent concurrent stops
	f.running.Store(false)

	// Signal shutdown - safe to close channel here
	close(f.shutdown)
	f.lifecycleMu.Unlock()

	// Wait for goroutine to finish with timeout
	done := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		f.logger.Info("File input stopped gracefully")
	case <-time.After(timeout):
		f.logger.Warn("File input stop timed out")
	}

	return nil
}

// readLoop reads files and publishes lines to NATS
func (f *Input) readLoop(ctx context.Context) {
	defer f.wg.Done()
	defer close(f.done)

	for {
		// Check context before expensive glob operation
		if ctx.Err() != nil {
			return
		}

		// Get files matching pattern
		matches, err := filepath.Glob(f.path)
		if err != nil {
			f.logger.Error("Failed to glob path", "error", err)
			f.errors.Add(1)
			return
		}

		for _, filePath := range matches {
			// Check for shutdown/cancellation before processing each file
			select {
			case <-ctx.Done():
				return
			case <-f.shutdown:
				return
			default:
			}

			if err := f.processFile(ctx, filePath); err != nil {
				if err == context.Canceled {
					return
				}
				f.logger.Error("Failed to process file", "path", filePath, "error", err)
				f.errors.Add(1)
			}

			if f.metrics != nil {
				f.metrics.filesProcessed.Inc()
			}
		}

		// Check if we should loop
		if !f.loop {
			f.logger.Debug("File processing complete", "files", len(matches))
			return
		}

		// Wait before reprocessing
		select {
		case <-ctx.Done():
			return
		case <-f.shutdown:
			return
		case <-time.After(time.Second):
			// Continue loop
		}
	}
}

// processFile reads and publishes a single file
func (f *Input) processFile(ctx context.Context, filePath string) error {
	// Check context before expensive file open
	if ctx.Err() != nil {
		return ctx.Err()
	}

	file, err := os.Open(filePath)
	if err != nil {
		return errs.WrapTransient(err, "Input", "processFile", "open file")
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)

	// Use pooled buffer for memory efficiency
	bufPtr := f.scannerBufferPool.Get().(*[]byte)
	defer f.scannerBufferPool.Put(bufPtr)
	scanner.Buffer(*bufPtr, scannerMaxBuffer)

	lineCount := 0
	for scanner.Scan() {
		lineCount++

		// Check context periodically to avoid performance penalty on every line
		if lineCount%contextCheckInterval == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-f.shutdown:
				return nil
			default:
			}
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Record line read metrics
		f.recordLineRead(len(line))

		// Validate JSON if format is jsonl
		if f.format == "jsonl" {
			var js json.RawMessage
			if err := json.Unmarshal(line, &js); err != nil {
				f.logger.Warn("Invalid JSON line", "error", err)
				f.recordParseError()
				continue
			}
		}

		// Publish to NATS
		if err := f.publishToNATS(ctx, line); err != nil {
			f.logger.Warn("Failed to publish", "error", err)
			f.errors.Add(1)
			continue
		}

		// Record line published metrics
		f.recordLinePublished()

		// Apply interval delay
		if f.interval > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-f.shutdown:
				return nil
			case <-time.After(f.interval):
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return errs.WrapTransient(err, "Input", "processFile", "scan file")
	}

	return nil
}

// recordLineRead updates metrics for a line read
func (f *Input) recordLineRead(size int) {
	f.linesRead.Add(1)
	f.bytesRead.Add(int64(size))
	if f.metrics != nil {
		f.metrics.linesRead.Inc()
		f.metrics.bytesRead.Add(float64(size))
	}
}

// recordLinePublished updates metrics for a line published
func (f *Input) recordLinePublished() {
	f.linesPublished.Add(1)
	if f.metrics != nil {
		f.metrics.linesPublished.Inc()
	}
}

// recordParseError updates metrics for a parse error
func (f *Input) recordParseError() {
	f.errors.Add(1)
	if f.metrics != nil {
		f.metrics.parseErrors.Inc()
	}
}

// isJetStreamPortBySubject checks if an output port with the given subject is configured for JetStream
func (f *Input) isJetStreamPortBySubject(subject string) bool {
	return subject == f.subject && f.jetStreamOutput
}

// publishToNATS publishes a line to the configured NATS subject
func (f *Input) publishToNATS(ctx context.Context, data []byte) error {
	// Check if output port is configured for JetStream
	if f.isJetStreamPortBySubject(f.subject) {
		if err := f.natsClient.PublishToStream(ctx, f.subject, data); err != nil {
			return errs.WrapTransient(err, "file-input", "publishToNATS", "JetStream publish")
		}
		return nil
	}

	// Fallback to core NATS for non-JetStream ports
	nc := f.natsClient.GetConnection()
	if nc == nil {
		return errs.WrapTransient(errs.ErrNoConnection, "file-input", "publishToNATS", "NATS connection check")
	}

	if err := nc.Publish(f.subject, data); err != nil {
		return errs.WrapTransient(err, "file-input", "publishToNATS", "NATS publish")
	}

	return nil
}

// CreateInput creates a file input component following service pattern
func CreateInput(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	cfg := DefaultConfig()

	if len(rawConfig) > 0 {
		type configOverride Config
		var override configOverride
		if err := component.SafeUnmarshal(rawConfig, &override); err != nil {
			return nil, errs.Wrap(err, "file-input-factory", "create", "secure config parsing")
		}
		userConfig := Config(override)

		// Apply user overrides
		if userConfig.Ports != nil {
			cfg.Ports = userConfig.Ports
		}
		if userConfig.Path != "" {
			cfg.Path = userConfig.Path
		}
		if userConfig.Format != "" {
			cfg.Format = userConfig.Format
		}
		if userConfig.Interval != "" {
			cfg.Interval = userConfig.Interval
		}
		cfg.Loop = userConfig.Loop
	}

	if deps.NATSClient == nil {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "file-input-factory", "CreateInput", "NATS client is required")
	}

	inputDeps := InputDeps{
		Name:            "file-input",
		Config:          cfg,
		NATSClient:      deps.NATSClient,
		MetricsRegistry: deps.MetricsRegistry,
		Logger:          deps.GetLoggerWithComponent("file-input"),
	}

	return NewInput(inputDeps)
}

// Register registers the file input component with the given registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "file_input",
		Factory:     CreateInput,
		Schema:      fileSchema,
		Type:        "input",
		Protocol:    "file",
		Domain:      "data",
		Description: "File input component for reading JSONL/JSON files and publishing to NATS",
		Version:     "1.0.0",
	})
}
