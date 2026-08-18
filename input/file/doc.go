// Package file provides a file input component for reading JSONL/JSON files and publishing to NATS.
//
// # Overview
//
// The file input component reads JSON Lines (JSONL) or JSON files and publishes each
// line/record as a message to NATS subjects. It supports glob patterns for reading
// multiple files, configurable delays between messages for rate control, and optional
// continuous looping for replay scenarios. It implements the StreamKit component
// interfaces for lifecycle management and observability.
//
// # Quick Start
//
// Read JSONL files and publish to NATS:
//
//	config := file.Config{
//	    Ports: &component.PortConfig{
//	        Outputs: []component.PortDefinition{
//	            {Name: "output", Config: component.NATSPort{Subject: "events.ingest"}, Required: true},
//	        },
//	    },
//	    Path:     "/data/events/*.jsonl",
//	    Format:   "jsonl",
//	    Interval: "10ms",
//	    Loop:     false,
//	}
//
//	rawConfig, _ := json.Marshal(config)
//	input, err := file.CreateInput(rawConfig, deps)
//
// # Configuration
//
// The Config struct controls file reading behavior:
//
//   - Path: File path or glob pattern (required)
//   - Format: File format - "jsonl" or "json" (default: "jsonl")
//   - Interval: Delay between publishing lines (default: "10ms")
//   - Loop: Continuously re-read files when complete (default: false)
//   - Ports: Output port configuration for NATS publishing
//
// # File Formats
//
// **JSONL (JSON Lines):**
//
// One JSON object per line, no outer array:
//
//	{"id": "1", "type": "event", "data": "..."}
//	{"id": "2", "type": "event", "data": "..."}
//
// Each line is validated as valid JSON before publishing. Invalid lines
// are logged and skipped without stopping processing.
//
// **JSON:**
//
// Standard JSON format (array or single object). For arrays, each element
// is published as a separate message.
//
// # Glob Patterns
//
// The Path field supports standard glob patterns:
//
//	// Single file
//	Path: "/data/events.jsonl"
//
//	// All .jsonl files in directory
//	Path: "/data/*.jsonl"
//
//	// Recursive pattern (one level)
//	Path: "/data/2024-*/*.jsonl"
//
// Files matching the pattern are processed in filesystem order. If the pattern
// matches no files at Initialize time, an error is returned.
//
// # Rate Control
//
// The Interval setting controls publishing rate to prevent overwhelming
// downstream consumers:
//
//	Interval: "10ms"   // 100 messages/second max
//	Interval: "1ms"    // 1000 messages/second max
//	Interval: "0"      // No delay, maximum throughput
//
// Typical use cases:
//   - Replay scenarios: Match original event timing
//   - Load testing: Control ingestion rate
//   - Gentle startup: Avoid thundering herd on restart
//
// # Loop Mode
//
// When Loop is true, the component continuously re-reads files after completion:
//
//	Loop: true
//	// Process all files
//	// Wait 1 second
//	// Process all files again
//	// Repeat until stopped
//
// Useful for:
//   - Continuous test data generation
//   - Simulation scenarios
//   - Development/debugging
//
// # NATS Publishing
//
// The component supports both core NATS and JetStream publishing:
//
// **Core NATS:**
//
//	Ports:
//	  Outputs:
//	    - Config:
//	        Kind: "nats"
//	        Subject: "events.raw"
//
// **JetStream:**
//
//	Ports:
//	  Outputs:
//	    - Config:
//	        Kind: "jetstream"
//	        Subjects: ["events.raw"]
//
// JetStream publishing uses acknowledgments and ensures message durability.
//
// # Lifecycle Management
//
// Proper component lifecycle with graceful shutdown:
//
//	// Initialize (validate path, check files exist)
//	input.Initialize()
//
//	// Start reading and publishing
//	input.Start(ctx)
//
//	// Graceful shutdown
//	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer shutdownCancel()
//	_ = input.Stop(shutdownCtx)
//
// During shutdown:
//  1. Cancel the owner-local run context created by Start
//  2. Join the component's goroutines through its WaitGroup
//  3. Close all resources
//
// # Observability
//
// The component implements component.Discoverable for monitoring:
//
//	meta := input.Meta()
//	// Name: file-input-{filename}
//	// Type: input
//	// Description: File input reading from {path}
//
//	health := input.Health()
//	// Healthy: true if component running
//	// ErrorCount: Parse/publish errors
//	// Uptime: Time since Start()
//
//	dataFlow := input.DataFlow()
//	// MessagesPerSecond: Publishing rate
//	// BytesPerSecond: Byte throughput
//	// ErrorRate: Error percentage
//
// Prometheus metrics:
//   - file_input_lines_read_total: Total lines read from files
//   - file_input_lines_published_total: Lines successfully published
//   - file_input_bytes_read_total: Total bytes read
//   - file_input_parse_errors_total: JSON parse failures
//   - file_input_files_processed_total: Files completely processed
//
// # Performance Characteristics
//
//   - Throughput: 10,000+ lines/second (without interval delay)
//   - Memory: O(1) per file (buffered line-by-line reading)
//   - Buffer: 1MB initial, 10MB max per line
//   - Context checks: Every 100 lines (prevents blocking on shutdown)
//
// # Error Handling
//
// The component uses streamkit/errors for consistent error classification:
//
//   - Invalid config: errs.WrapInvalid (empty path, invalid format)
//   - Missing files: errs.WrapInvalid (no files match glob)
//   - Parse errors: Logged and skipped (doesn't stop processing)
//   - Publish errors: errs.WrapTransient (NATS unavailable)
//
// Individual line errors don't stop file processing. File errors are logged
// but don't stop glob pattern processing.
//
// # Common Use Cases
//
// **Data Replay:**
//
//	Path: "/archive/events-2024-01-15.jsonl"
//	Format: "jsonl"
//	Interval: "10ms"
//	Loop: false
//
// **Continuous Test Data:**
//
//	Path: "/testdata/sample-events.jsonl"
//	Format: "jsonl"
//	Interval: "100ms"
//	Loop: true
//
// **Batch Import:**
//
//	Path: "/import/*.jsonl"
//	Format: "jsonl"
//	Interval: "0"  // Max throughput
//	Loop: false
//
// # Thread Safety
//
// The component is thread-safe:
//
//   - Lifecycle operations protected by mutex
//   - Metrics use atomic operations
//   - Start/Stop can be called from any goroutine
//
// # Concurrency Patterns
//
// The implementation uses standard Go concurrency patterns:
//
//	// Lifecycle mutex (separate from data mutex)
//	f.lifecycleMu.Lock()
//	defer f.lifecycleMu.Unlock()
//
//	// Start derives a run context and retains only its cancel function.
//	runCtx, cancel := context.WithCancel(ctx)
//	f.cancel = cancel
//
//	// Owned goroutines observe runCtx and always join the WaitGroup.
//	f.wg.Add(1)
//	go f.readLoop(runCtx)
//
// Stop invokes the retained cancel function and waits for the WaitGroup to
// join within the caller's shutdown budget.
//
// # Scanner Buffer Pool
//
// Memory-efficient reading using pooled buffers:
//
//	scannerInitialBuffer = 1MB   // Initial allocation
//	scannerMaxBuffer     = 10MB  // Maximum line length
//
// Buffers are pooled and reused across file reads, reducing GC pressure
// for high-throughput scenarios.
//
// # Testing
//
// The package follows standard testing patterns:
//
//	go test ./input/file -v
//	go test ./input/file -race  // Race detector
//
// # Limitations
//
// Current version limitations:
//
//   - No compression support (gzip, zstd)
//   - No offset tracking (always starts from beginning)
//   - No file watching (new files require restart)
//   - No parallel file processing
//   - Single output subject only
//
// # Example: Complete Configuration
//
//	{
//	  "ports": {
//	    "outputs": [
//	      {"name":"output","required":true,"config":{"kind":"jetstream","subjects":["events.raw"]}}
//	    ]
//	  },
//	  "path": "/data/events/*.jsonl",
//	  "format": "jsonl",
//	  "interval": "10ms",
//	  "loop": false
//	}
//
// # See Also
//
// Related packages:
//   - [github.com/c360studio/semstreams/component]: Component interfaces
//   - [github.com/c360studio/semstreams/natsclient]: NATS connection and publishing
//   - [github.com/c360studio/semstreams/input/udp]: UDP input component
//   - [github.com/c360studio/semstreams/input/websocket]: WebSocket input component
package file
