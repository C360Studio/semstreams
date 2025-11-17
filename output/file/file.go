// Package file provides file output component for writing messages to files
package file

import (
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

	"github.com/c360/semstreams/component"
	"github.com/c360/semstreams/errors"
	"github.com/c360/semstreams/natsclient"
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
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate", "directory is required")
	}

	validFormats := map[string]bool{"json": true, "jsonl": true, "raw": true}
	if !validFormats[c.Format] {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate",
			"format must be one of: json, jsonl, raw")
	}

	if c.BufferSize < 0 {
		return errors.WrapInvalid(errors.ErrInvalidConfig, "Config", "Validate",
			"buffer_size cannot be negative")
	}

	return nil
}

// DefaultConfig returns default configuration for file output
func DefaultConfig() Config {
	inputDefs := []component.PortDefinition{
		{
			Name:        "nats_input",
			Type:        "nats",
			Subject:     "output.>",
			Required:    true,
			Description: "NATS subjects to write to files",
		},
	}

	outputDefs := []component.PortDefinition{
		{
			Name:        "file_output",
			Type:        "file",
			Subject:     "/tmp/streamkit/output.jsonl",
			Required:    false,
			Description: "File path for output",
		},
	}

	return Config{
		Ports: &component.PortConfig{
			Inputs:  inputDefs,
			Outputs: outputDefs,
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
	name       string
	subjects   []string
	directory  string
	filePrefix string
	format     string
	append     bool
	bufferSize int
	natsClient *natsclient.Client
	logger     *slog.Logger

	// File handling
	file   *os.File
	fileMu sync.Mutex

	// Buffer for batching writes
	buffer   [][]byte
	bufferMu sync.Mutex

	// Lifecycle management
	shutdown    chan struct{}
	done        chan struct{}
	closeOnce   sync.Once
	running     bool
	startTime   time.Time
	mu          sync.RWMutex
	lifecycleMu sync.Mutex
	wg          *sync.WaitGroup

	// Metrics
	messagesWritten int64
	bytesWritten    int64
	errors          int64
	lastActivity    time.Time
}

// NewOutput creates a new file output from configuration
func NewOutput(rawConfig json.RawMessage, deps component.Dependencies) (component.Discoverable, error) {
	var config Config
	if err := component.SafeUnmarshal(rawConfig, &config); err != nil {
		return nil, errors.WrapInvalid(err, "Output", "NewOutput", "config unmarshal")
	}

	if config.Ports == nil {
		config = DefaultConfig()
	}

	// Extract subjects from port configuration
	var inputSubjects []string
	for _, input := range config.Ports.Inputs {
		if input.Type == "nats" {
			inputSubjects = append(inputSubjects, input.Subject)
		}
	}

	if len(inputSubjects) == 0 {
		return nil, errors.WrapInvalid(errors.ErrInvalidConfig, "Output", "NewOutput", "no input subjects configured")
	}

	// Validate directory
	if config.Directory == "" {
		config.Directory = "/tmp/streamkit"
	}

	return &Output{
		name:       "file-output",
		subjects:   inputSubjects,
		directory:  config.Directory,
		filePrefix: config.FilePrefix,
		format:     config.Format,
		append:     config.Append,
		bufferSize: config.BufferSize,
		natsClient: deps.NATSClient,
		logger:     deps.GetLogger(),
		buffer:     make([][]byte, 0, config.BufferSize),
		shutdown:   make(chan struct{}),
		done:       make(chan struct{}),
		wg:         &sync.WaitGroup{},
	}, nil
}

// Initialize prepares the output (creates directory)
func (f *Output) Initialize() error {
	// Create output directory if it doesn't exist
	if err := os.MkdirAll(f.directory, 0755); err != nil {
		return errors.WrapFatal(err, "Output", "Initialize", "create output directory")
	}

	return nil
}

// Start begins writing messages to files
func (f *Output) Start(ctx context.Context) error {
	f.logger.Info("Output.Start called",
		"component", f.name,
		"subjects_count", len(f.subjects),
		"directory", f.directory,
		"file_prefix", f.filePrefix,
		"format", f.format)

	f.lifecycleMu.Lock()
	defer f.lifecycleMu.Unlock()

	if f.running {
		return errors.WrapFatal(errors.ErrAlreadyStarted, "Output", "Start", "check running state")
	}

	if f.natsClient == nil {
		return errors.WrapFatal(errors.ErrMissingConfig, "Output", "Start", "NATS client required")
	}

	// Open output file
	filename := filepath.Join(f.directory, fmt.Sprintf("%s.%s", f.filePrefix, f.format))
	var err error
	flags := os.O_CREATE | os.O_WRONLY
	if f.append {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}

	f.file, err = os.OpenFile(filename, flags, 0644)
	if err != nil {
		return errors.WrapFatal(err, "Output", "Start", "open output file")
	}

	// Subscribe to input subjects
	for _, subject := range f.subjects {
		f.logger.Debug("Subscribing to NATS subject",
			"component", f.name,
			"subject", subject)

		if err := f.natsClient.Subscribe(ctx, subject, f.handleMessage); err != nil {
			f.logger.Error("Failed to subscribe to NATS subject",
				"component", f.name,
				"subject", subject,
				"error", err)
			return errors.WrapTransient(err, "Output", "Start", fmt.Sprintf("subscribe to %s", subject))
		}

		f.logger.Debug("Subscribed to NATS subject successfully",
			"component", f.name,
			"subject", subject,
			"output_file", filename,
			"format", f.format,
			"buffer_size", f.bufferSize)
	}

	// Start flush goroutine
	f.wg.Add(1)
	go f.flushLoop()

	f.mu.Lock()
	f.running = true
	f.startTime = time.Now()
	f.mu.Unlock()

	f.logger.Info("File output started",
		"component", f.name,
		"input_subjects", f.subjects,
		"output_file", filename,
		"format", f.format,
		"append", f.append,
		"buffer_size", f.bufferSize)

	return nil
}

// Stop gracefully stops the output
func (f *Output) Stop(timeout time.Duration) error {
	f.lifecycleMu.Lock()
	defer f.lifecycleMu.Unlock()

	if !f.running {
		return nil
	}

	// Signal shutdown
	close(f.shutdown)

	// Wait for goroutines with timeout
	waitCh := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		// Clean shutdown
	case <-time.After(timeout):
		return errors.WrapTransient(fmt.Errorf("shutdown timeout after %v", timeout), "Output", "Stop", "shutdown")
	}

	// Flush remaining buffer
	f.flush()

	// Close file
	f.fileMu.Lock()
	if f.file != nil {
		if err := f.file.Close(); err != nil {
			f.logger.Warn("failed to close output file", "error", err, "path", f.file.Name())
		}
		f.file = nil
	}
	f.fileMu.Unlock()

	f.mu.Lock()
	f.running = false
	f.mu.Unlock()

	// Close done channel exactly once, even if Stop() called multiple times
	f.closeOnce.Do(func() {
		close(f.done)
	})

	return nil
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
func (f *Output) flushLoop() {
	defer f.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-f.shutdown:
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
		f.logger.Debug("Flush called with empty buffer",
			"component", f.name)
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
	ports := make([]component.Port, len(f.subjects))
	for i, subj := range f.subjects {
		ports[i] = component.Port{
			Name:      fmt.Sprintf("input_%d", i),
			Direction: component.DirectionInput,
			Required:  true,
			Config:    component.NATSPort{Subject: subj},
		}
	}
	return ports
}

// OutputPorts returns configured output port definitions (none for file output)
func (f *Output) OutputPorts() []component.Port {
	// File output has no NATS output ports
	return []component.Port{}
}

// ConfigSchema returns the configuration schema
func (f *Output) ConfigSchema() component.ConfigSchema {
	return fileSchema
}

// Health returns the current health status
func (f *Output) Health() component.HealthStatus {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return component.HealthStatus{
		Healthy:    f.running && f.file != nil,
		LastCheck:  time.Now(),
		ErrorCount: int(atomic.LoadInt64(&f.errors)),
		Uptime:     time.Since(f.startTime),
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
