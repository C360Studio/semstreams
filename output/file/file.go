// Package file provides file output component for writing messages to files
package file

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/internal/lifecyclecleanup"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Config holds configuration for file output component
type Config struct {
	Ports      *component.PortConfig `json:"ports"       schema:"type:ports,description:Port configuration,category:basic"`
	Directory  string                `json:"directory"   schema:"type:string,description:Output directory,category:basic"`
	FilePrefix string                `json:"file_prefix" schema:"type:string,description:Prefix,category:basic"`
	Format     string                `json:"format"      schema:"type:enum,enum:json|jsonl|raw,category:basic"`
	Append     bool                  `json:"append"      schema:"type:bool,description:Append mode,category:advanced"`
	BufferSize int                   `json:"buffer_size" schema:"type:int,description:Buffer size,category:advanced"`
}

// Validate checks the configuration for errors
func (c *Config) Validate() error {
	if c.Directory == "" {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate", "directory is required")
	}

	validFormats := map[string]bool{"json": true, "jsonl": true, "raw": true}
	if !validFormats[c.Format] {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
			"format must be one of: json, jsonl, raw")
	}

	if c.BufferSize < 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
			"buffer_size cannot be negative")
	}
	if c.Ports != nil && len(c.Ports.Outputs) != 0 {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Config", "Validate",
			"file output ports are derived from directory, file_prefix, and format; remove ports.outputs")
	}

	return nil
}

// DefaultConfig returns default configuration for file output
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name: "nats_input", Config: component.NATSPort{Subject: "output.>"}, Required: true,
			Description: "NATS subjects to write to files",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs: inputDefs,
		},
		Directory:  "/tmp/streamkit",
		FilePrefix: "output",
		Format:     "jsonl",
		Append:     true,
		BufferSize: 100,
	}
}

// fileSchema defines the configuration schema for file output component
var fileSchema = component.GenerateConfigSchema(reflect.TypeOf(Config{}))

// Output implements file writing for NATS messages
type Output struct {
	name        string
	subjects    []string
	directory   string
	filePrefix  string
	format      string
	append      bool
	bufferSize  int
	config      Config // Store full config for port type checking
	inputPorts  []component.Port
	outputPorts []component.Port
	filePath    string
	natsClient  *natsclient.Client
	logger      *slog.Logger

	// File handling
	file   *os.File
	fileMu sync.Mutex

	// Buffer for batching writes
	buffer   [][]byte
	bufferMu sync.Mutex

	// Lifecycle management
	running        bool
	startTime      time.Time
	mu             sync.RWMutex
	lifecycleMu    sync.Mutex
	lifecycleUsed  bool
	terminal       bool
	stopping       bool
	cleanupPending bool
	cancel         context.CancelFunc
	flushDone      chan struct{}
	subscriptions  []coreSubscription
	consumers      []streamConsumerBinding

	waitForStreamInput func(context.Context, string) error
	subscribeCore      func(context.Context, string, func(context.Context, *nats.Msg)) (coreSubscription, error)
	consumeStream      func(context.Context, natsclient.PortConsumerContext, natsclient.StreamConsumerConfig, func(context.Context, jetstream.Msg)) (jetstream.ConsumeContext, error)
	waitConsumerClosed func(context.Context, <-chan struct{}) error

	// Metrics
	messagesWritten int64
	bytesWritten    int64
	errors          int64
	lastActivity    time.Time
}

type coreSubscription interface {
	Drain(context.Context) error
}

type streamConsumerBinding struct {
	handle      jetstream.ConsumeContext
	drainIssued bool
}

// NewOutput creates a new file output from configuration
func NewOutput(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := component.SafeUnmarshal(rawConfig, &config); err != nil {
		return nil, errs.WrapInvalid(err, "Output", "NewOutput", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}
	if err := config.Validate(); err != nil {
		return nil, errs.WrapInvalid(err, "Output", "NewOutput", "validate config")
	}

	inputPorts := make([]component.Port, len(config.Ports.Inputs))
	var inputSubjects []string
	for index, definition := range config.Ports.Inputs {
		port, err := definition.Resolve(component.DirectionInput)
		if err != nil {
			return nil, errs.WrapInvalid(err, "Output", "NewOutput", "resolve input port")
		}
		facts, err := port.Facts()
		if err != nil {
			return nil, errs.WrapInvalid(err, "Output", "NewOutput", "project input port facts")
		}
		if facts.Kind() != component.PortKindNATS && facts.Kind() != component.PortKindJetStream {
			return nil, errs.WrapInvalid(fmt.Errorf("input port %q kind %q is not nats or jetstream", port.Name, facts.Kind()), "Output", "NewOutput", "validate input port")
		}
		subjects := facts.NATSSubjects()
		if len(subjects) != 1 {
			return nil, errs.WrapInvalid(fmt.Errorf("input port %q declares %d subjects, want one", port.Name, len(subjects)), "Output", "NewOutput", "validate input port")
		}
		inputPorts[index] = port
		inputSubjects = append(inputSubjects, subjects[0])
	}

	if len(inputSubjects) == 0 {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "Output", "NewOutput", "no input subjects configured")
	}

	filePath := filepath.Join(config.Directory, fmt.Sprintf("%s.%s", config.FilePrefix, config.Format))
	fileOutput, err := (component.PortDefinition{
		Name:        "file_output",
		Config:      component.FilePort{Path: filePath},
		Description: "File path for output",
	}).Resolve(component.DirectionOutput)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Output", "NewOutput", "resolve file output port")
	}

	return &Output{
		name:        "file-output",
		subjects:    inputSubjects,
		directory:   config.Directory,
		filePrefix:  config.FilePrefix,
		format:      config.Format,
		append:      config.Append,
		bufferSize:  config.BufferSize,
		config:      config, // Store full config for port type checking
		inputPorts:  inputPorts,
		outputPorts: []component.Port{fileOutput},
		filePath:    filePath,
		natsClient:  deps.NATSClient,
		logger:      deps.GetLogger(),
		buffer:      make([][]byte, 0, config.BufferSize),
	}, nil
}

// Initialize prepares the output (creates directory)
func (f *Output) Initialize() error {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(f.directory, 0755); err != nil {
		return errs.WrapFatal(err, "Output", "Initialize", "create output directory")
	}

	return nil
}

// Start begins writing messages to files
func (f *Output) Start(ctx context.Context) (startErr error) {
	// Validate context
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidConfig, "Output", "Start", "context cannot be nil")
	}
	if err := ctx.Err(); err != nil {
		return errs.WrapInvalid(err, "Output", "Start", "context already cancelled")
	}

	f.logger.Debug("Output.Start called",
		"component", f.name,
		"subjects_count", len(f.subjects),
		"directory", f.directory,
		"file_prefix", f.filePrefix,
		"format", f.format)

	f.lifecycleMu.Lock()
	defer f.lifecycleMu.Unlock()

	if f.lifecycleUsed {
		return errs.WrapFatal(errs.ErrAlreadyStarted, "Output", "Start", "check running state")
	}

	if f.natsClient == nil {
		return errs.WrapFatal(errs.ErrMissingConfig, "Output", "Start", "NATS client required")
	}

	parent := ctx
	runCtx, cancel := context.WithCancel(ctx)
	f.lifecycleUsed = true
	f.cleanupPending = true
	f.cancel = cancel
	committed := false
	defer func() {
		if committed {
			f.cleanupPending = false
			return
		}
		rollbackErr := lifecyclecleanup.RollbackFailedStart(parent, f.cleanup)
		startErr = errors.Join(startErr, rollbackErr)
		if rollbackErr == nil {
			f.cleanupPending = false
			f.terminal = true
			f.clearLifecycleHandles()
		}
	}()

	// Open output file
	var err error
	flags := os.O_CREATE | os.O_WRONLY
	if f.append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	openedFile, err := os.OpenFile(f.filePath, flags, 0644)
	if err != nil {
		return errs.WrapFatal(err, "Output", "Start", "open output file")
	}
	f.fileMu.Lock()
	f.file = openedFile
	f.fileMu.Unlock()

	// Subscribe to input ports based on port type
	if err := f.setupSubscriptions(runCtx); err != nil {
		return err
	}

	// Start flush goroutine
	f.flushDone = make(chan struct{})
	go f.flushLoop(runCtx, f.flushDone)

	f.mu.Lock()
	f.running = true
	f.startTime = time.Now()
	f.mu.Unlock()
	committed = true

	f.logger.Info("File output started",
		"component", f.name,
		"input_subjects", f.subjects,
		"output_file", f.filePath,
		"format", f.format,
		"append", f.append,
		"buffer_size", f.bufferSize)

	return nil
}

// setupSubscriptions creates subscriptions for input ports based on port type
func (f *Output) setupSubscriptions(ctx context.Context) error {
	for _, port := range f.inputPorts {
		facts, err := port.Facts()
		if err != nil {
			return errs.WrapInvalid(err, "Output", "Start", "project input port facts")
		}
		subject := facts.NATSSubjects()[0]

		switch facts.Kind() {
		case component.PortKindJetStream:
			if err := f.setupJetStreamConsumer(ctx, port); err != nil {
				return errs.WrapTransient(err, "Output", "Start",
					fmt.Sprintf("JetStream consumer for %s", subject))
			}

		case component.PortKindNATS:
			subscribe := func(ctx context.Context, subject string, handler func(context.Context, *nats.Msg)) (coreSubscription, error) {
				return f.natsClient.Subscribe(ctx, subject, handler)
			}
			if f.subscribeCore != nil {
				subscribe = f.subscribeCore
			}
			sub, err := subscribe(ctx, subject, func(ctx context.Context, msg *nats.Msg) {
				f.handleMessage(ctx, msg.Data)
			})
			if err != nil {
				f.logger.Error("Failed to subscribe to NATS subject",
					"component", f.name,
					"subject", subject,
					"error", err)
				return errs.WrapTransient(err, "Output", "Start",
					fmt.Sprintf("subscribe to %s", subject))
			}
			f.subscriptions = append(f.subscriptions, sub)
			f.logger.Debug("Subscribed to NATS subject successfully",
				"component", f.name,
				"subject", subject)
		}
	}
	return nil
}

// setupJetStreamConsumer creates a JetStream consumer for an input port
func (f *Output) setupJetStreamConsumer(ctx context.Context, port component.Port) error {
	facts, err := port.Facts()
	if err != nil {
		return err
	}
	stream, ok := facts.Stream()
	if !ok {
		return fmt.Errorf("port %q does not declare JetStream facts", port.Name)
	}
	subject := facts.NATSSubjects()[0]
	streamName := stream.Name()

	waitForStream := f.waitForStream
	if f.waitForStreamInput != nil {
		waitForStream = f.waitForStreamInput
	}
	if err := waitForStream(ctx, streamName); err != nil {
		return errs.WrapTransient(err, "Output", "setupJetStreamConsumer",
			fmt.Sprintf("wait for stream %s", streamName))
	}

	sanitizedSubject := strings.ReplaceAll(subject, ".", "-")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, "*", "all")
	sanitizedSubject = strings.ReplaceAll(sanitizedSubject, ">", "wildcard")
	consumerName := fmt.Sprintf("file-output-%s", sanitizedSubject)

	f.logger.Debug("Setting up JetStream consumer",
		"stream", streamName,
		"consumer", consumerName,
		"filter_subject", subject)

	// Get consumer config from port definition (allows user configuration)
	consumerCfg, consumerErr := component.GetConsumerConfig(port)
	if consumerErr != nil {
		return errs.WrapInvalid(consumerErr, "Output", "setupJetStreamConsumer", "resolve consumer config")
	}

	cfg := natsclient.StreamConsumerConfig{
		StreamName:    streamName,
		ConsumerName:  consumerName,
		FilterSubject: subject,
		DeliverPolicy: consumerCfg.DeliverPolicy,
		AckPolicy:     consumerCfg.AckPolicy,
		MaxDeliver:    consumerCfg.MaxDeliver,
		MaxAckPending: consumerCfg.MaxAckPending,
		AutoCreate:    false,
	}

	consumeStream := f.natsClient.ConsumeStreamWithConfig
	if f.consumeStream != nil {
		consumeStream = f.consumeStream
	}
	handle, err := consumeStream(ctx, natsclient.PortConsumerContext{Component: f.Meta().Name, Port: port.Name}, cfg, func(msgCtx context.Context, msg jetstream.Msg) {
		f.handleMessage(msgCtx, msg.Data())
		if ackErr := msg.Ack(); ackErr != nil {
			f.logger.Error("Failed to ack JetStream message", "error", ackErr)
		}
	})
	if err != nil {
		return errs.WrapTransient(err, "Output", "setupJetStreamConsumer",
			fmt.Sprintf("setup consumer for stream %s", streamName))
	}
	f.consumers = append(f.consumers, streamConsumerBinding{handle: handle})

	f.logger.Debug("File output subscribed (JetStream)", "subject", subject, "stream", streamName)
	return nil
}

// waitForStream waits for a JetStream stream to be available
func (f *Output) waitForStream(ctx context.Context, streamName string) error {
	js, err := f.natsClient.JetStream()
	if err != nil {
		return errs.WrapTransient(err, "Output", "waitForStream", "get JetStream context")
	}

	maxRetries := 30
	retryInterval := 100 * time.Millisecond
	maxInterval := 2 * time.Second

	for i := 0; i < maxRetries; i++ {
		_, err := js.Stream(ctx, streamName)
		if err == nil {
			return nil
		}
		if i < maxRetries-1 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryInterval):
				retryInterval = min(retryInterval*2, maxInterval)
			}
		}
	}
	return errs.WrapTransient(errs.ErrStorageUnavailable, "Output", "waitForStream",
		fmt.Sprintf("stream %s not available after %d retries", streamName, maxRetries))
}

// Stop gracefully stops the output
func (f *Output) Stop(ctx context.Context) error {
	if ctx == nil {
		return errs.WrapInvalid(errs.ErrInvalidData, "LifecycleComponent", "Stop", "nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	f.lifecycleMu.Lock()
	if !f.lifecycleUsed {
		f.lifecycleUsed = true
		f.terminal = true
		f.lifecycleMu.Unlock()
		return nil
	}
	if f.terminal {
		f.lifecycleMu.Unlock()
		return nil
	}
	if f.stopping {
		f.lifecycleMu.Unlock()
		return errs.WrapTransient(errors.New("stop already in progress"), "Output", "Stop", "concurrent Stop is unsupported")
	}
	retryable := f.cleanupPending
	f.stopping = true
	f.lifecycleMu.Unlock()
	stopErr := f.cleanup(ctx)
	f.lifecycleMu.Lock()
	f.stopping = false
	if retryable && stopErr != nil {
		f.lifecycleMu.Unlock()
		return stopErr
	}
	f.cleanupPending = false
	f.terminal = true
	f.clearLifecycleHandles()
	f.mu.Lock()
	f.running = false
	f.mu.Unlock()
	f.lifecycleMu.Unlock()
	return stopErr
}

func (f *Output) cleanup(ctx context.Context) error {
	var cleanupErr error
	for index := range f.consumers {
		binding := &f.consumers[index]
		if !binding.drainIssued {
			binding.handle.Drain()
			binding.drainIssued = true
		}
	}
	for _, sub := range f.subscriptions {
		cleanupErr = errors.Join(cleanupErr, sub.Drain(ctx))
	}
	for index := range f.consumers {
		closed := f.consumers[index].handle.Closed()
		if f.waitConsumerClosed != nil {
			cleanupErr = errors.Join(cleanupErr, f.waitConsumerClosed(ctx, closed))
			continue
		}
		select {
		case <-closed:
		case <-ctx.Done():
			cleanupErr = errors.Join(cleanupErr, ctx.Err())
		}
	}
	if f.cancel != nil {
		f.cancel()
	}
	joined := true
	if f.flushDone != nil {
		select {
		case <-f.flushDone:
		case <-ctx.Done():
			joined = false
			cleanupErr = errors.Join(cleanupErr, ctx.Err())
		}
	}
	if cleanupErr == nil && joined {
		f.flush()
		f.fileMu.Lock()
		if f.file != nil {
			if err := f.file.Close(); err != nil {
				f.logger.Warn("failed to close output file", "error", err, "path", f.file.Name())
				cleanupErr = errors.Join(cleanupErr, err)
			} else {
				f.file = nil
			}
		}
		f.fileMu.Unlock()
	}
	return errors.Join(cleanupErr, ctx.Err())
}

func (f *Output) clearLifecycleHandles() {
	f.cancel = nil
	f.flushDone = nil
	f.subscriptions = nil
	f.consumers = nil
}

// handleMessage processes incoming messages
func (f *Output) handleMessage(ctx context.Context, msgData []byte) {
	f.logger.Debug("Received message",
		"component", f.name,
		"size_bytes", len(msgData))

	f.bufferMu.Lock()
	f.buffer = append(f.buffer, msgData)
	bufferLen := len(f.buffer)
	shouldFlush := bufferLen >= f.bufferSize
	f.bufferMu.Unlock()

	f.logger.Debug("Message buffered",
		"component", f.name,
		"buffer_length", bufferLen,
		"buffer_size", f.bufferSize,
		"should_flush", shouldFlush)

	if shouldFlush {
		// Check context before potentially expensive flush operation
		select {
		case <-ctx.Done():
			f.logger.Debug("Context cancelled before flush",
				"component", f.name)
			return
		default:
		}

		f.logger.Debug("Buffer full, flushing",
			"component", f.name)
		f.flush()
	}

	f.mu.Lock()
	f.lastActivity = time.Now()
	f.mu.Unlock()
}

// flushLoop periodically flushes the buffer
func (f *Output) flushLoop(ctx context.Context, done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.flush()
		}
	}
}

// flush writes buffered messages to file
func (f *Output) flush() {
	f.bufferMu.Lock()
	if len(f.buffer) == 0 {
		f.bufferMu.Unlock()
		// No logging for empty buffer - this is normal during periodic flush
		return
	}

	messages := f.buffer
	messageCount := len(messages)
	f.buffer = make([][]byte, 0, f.bufferSize)
	f.bufferMu.Unlock()

	f.logger.Debug("Flushing buffer to file",
		"component", f.name,
		"message_count", messageCount,
		"format", f.format)

	f.fileMu.Lock()
	defer f.fileMu.Unlock()

	if f.file == nil {
		atomic.AddInt64(&f.errors, int64(len(messages)))
		f.logger.Error("File handle is nil during flush",
			"component", f.name,
			"messages_lost", len(messages))
		return
	}

	for i, msg := range messages {
		var writeData []byte
		switch f.format {
		case "jsonl":
			// JSON Lines format - one JSON object per line
			writeData = append(msg, '\n')
		case "json":
			// Pretty-printed JSON with newline
			var obj any
			if err := json.Unmarshal(msg, &obj); err == nil {
				if formatted, err := json.MarshalIndent(obj, "", "  "); err == nil {
					writeData = append(formatted, '\n')
				} else {
					writeData = append(msg, '\n')
				}
			} else {
				writeData = append(msg, '\n')
			}
		case "raw":
			// Raw bytes
			writeData = msg
		default:
			writeData = append(msg, '\n')
		}

		n, err := f.file.Write(writeData)
		if err != nil {
			atomic.AddInt64(&f.errors, 1)
			f.logger.Error("Failed to write message to file",
				"component", f.name,
				"message_index", i,
				"error", err)
		} else {
			atomic.AddInt64(&f.messagesWritten, 1)
			atomic.AddInt64(&f.bytesWritten, int64(n))
			f.logger.Debug("Message written to file",
				"component", f.name,
				"message_index", i,
				"bytes_written", n)
		}
	}

	f.logger.Debug("Flush completed",
		"component", f.name,
		"messages_written", messageCount,
		"total_written", atomic.LoadInt64(&f.messagesWritten),
		"total_errors", atomic.LoadInt64(&f.errors))
}

// Discoverable interface implementation

// Meta returns component metadata
func (f *Output) Meta() component.Metadata {
	return component.Metadata{
		Name:        f.name,
		Type:        "output",
		Description: "File output for writing messages to disk",
		Version:     "0.1.0",
	}
}

// InputPorts returns configured input port definitions
func (f *Output) InputPorts() []component.Port {
	return append([]component.Port(nil), f.inputPorts...)
}

// OutputPorts returns the resolved file resource written by this component.
func (f *Output) OutputPorts() []component.Port {
	return append([]component.Port(nil), f.outputPorts...)
}

// ConfigSchema returns the configuration schema
func (f *Output) ConfigSchema() component.ConfigSchema {
	return fileSchema
}

// Health returns the current health status
func (f *Output) Health() component.HealthStatus {
	f.mu.RLock()
	running := f.running
	startTime := f.startTime
	f.mu.RUnlock()
	f.fileMu.Lock()
	fileOpen := f.file != nil
	f.fileMu.Unlock()

	return component.HealthStatus{
		Healthy:    running && fileOpen,
		LastCheck:  time.Now(),
		ErrorCount: int(atomic.LoadInt64(&f.errors)),
		Uptime:     time.Since(startTime),
	}
}

// DataFlow returns current data flow metrics
func (f *Output) DataFlow() component.FlowMetrics {
	f.mu.RLock()
	defer f.mu.RUnlock()

	written := atomic.LoadInt64(&f.messagesWritten)
	errorCount := atomic.LoadInt64(&f.errors)

	var errorRate float64
	if written > 0 {
		errorRate = float64(errorCount) / float64(written)
	}

	return component.FlowMetrics{
		MessagesPerSecond: 0, // TODO: Calculate rate
		BytesPerSecond:    0,
		ErrorRate:         errorRate,
		LastActivity:      f.lastActivity,
	}
}

// Register registers the file output component with the given registry
func Register(registry *component.Registry) error {
	return registry.RegisterWithConfig(component.RegistrationConfig{
		Name:        "file",
		Factory:     NewOutput,
		Schema:      fileSchema,
		Type:        "output",
		Protocol:    "file",
		Domain:      "storage",
		Description: "File output for writing messages to disk in JSON, JSONL, or raw format",
		Version:     "0.1.0",
	})
}
