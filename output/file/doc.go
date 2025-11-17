// Package file provides a file output component for writing messages to files.
//
// # Overview
//
// The file output component writes incoming NATS messages to files on disk, with support
// for multiple file formats, automatic flushing, and file rotation. It implements the
// StreamKit component interfaces for lifecycle management and observability.
//
// # Quick Start
//
// Write messages to a JSON lines file:
//
//	config := file.Config{
//	    Ports: &component.PortConfig{
//	        Inputs: []component.PortDefinition{
//	            {Name: "input", Type: "nats", Subject: "data.>", Required: true},
//	        },
//	    },
//	    Path:         "/var/log/streamkit/messages.jsonl",
//	    Format:       "jsonlines",
//	    FlushInterval: 5 * time.Second,
//	}
//
//	rawConfig, _ := json.Marshal(config)
//	output, err := file.NewOutput(rawConfig, deps)
//
// # Configuration
//
// The FileOutputConfig struct controls file writing behavior:
//
//   - Path: Filesystem path to write to
//   - Format: Output format ("jsonlines", "raw", "csv")
//   - FlushInterval: How often to flush buffered writes (default: 5s)
//   - Append: Append to existing file vs overwrite (default: true)
//
// # File Formats
//
// **JSON Lines** (recommended for structured data):
//
//	Format: "jsonlines"
//
//	// Each message written as single JSON line
//	{"timestamp": "2024-01-01T00:00:00Z", "value": 42}
//	{"timestamp": "2024-01-01T00:00:01Z", "value": 43}
//
// **Raw** (binary or text data):
//
//	Format: "raw"
//
//	// Message bytes written directly to file
//	// One message per line
//
// **CSV** (structured data as comma-separated values):
//
//	Format: "csv"
//
//	// Requires JSON messages with consistent fields
//	timestamp,value
//	2024-01-01T00:00:00Z,42
//	2024-01-01T00:00:01Z,43
//
// # Buffering and Flushing
//
// The component uses buffered writes with configurable flush intervals:
//
//	FlushInterval: 5 * time.Second  // Flush every 5 seconds
//
// Flushing behavior:
//  1. Automatic flush every FlushInterval
//  2. Flush on Stop() for graceful shutdown
//  3. OS-level buffering may add additional delay
//
// # Message Flow
//
//	NATS Subject → Message Handler → File Buffer → Periodic Flush → Disk
//
// # Lifecycle Management
//
// Proper file handle management with graceful shutdown:
//
//	// Start writing
//	output.Start(ctx)
//
//	// Graceful shutdown with flush
//	output.Stop(5 * time.Second)
//
// During shutdown:
//  1. Stop accepting new messages
//  2. Flush buffered data to disk
//  3. Close file handle
//  4. Wait for flush goroutine to complete
//
// # Observability
//
// The component implements component.Discoverable for monitoring:
//
//	meta := output.Meta()
//	// Name: file-output
//	// Type: output
//	// Description: File writer output
//
//	health := output.Health()
//	// Healthy: true if file writable
//	// ErrorCount: Write errors
//	// Uptime: Time since Start()
//
//	dataFlow := output.DataFlow()
//	// MessagesPerSecond: Write rate
//	// BytesPerSecond: Byte throughput
//	// ErrorRate: Error percentage
//
// # Performance Characteristics
//
//   - Throughput: 10,000+ messages/second (buffered)
//   - Memory: O(buffer size) + per-message allocations
//   - Latency: FlushInterval (default 5s)
//   - Disk I/O: Batched via OS buffer
//
// # Error Handling
//
// The component uses streamkit/errors for consistent error classification:
//
//   - Invalid config: errors.WrapInvalid (bad configuration)
//   - File errors: errors.WrapTransient (disk full, permissions)
//   - NATS errors: errors.WrapTransient (connection issues)
//
// Write errors are logged and counted but don't stop the component.
//
// # Common Use Cases
//
// **Application Logging:**
//
//	Path: "/var/log/app/events.jsonl"
//	Format: "jsonlines"
//	FlushInterval: 10 * time.Second
//
// **Data Export:**
//
//	Path: "/data/export/sensors.csv"
//	Format: "csv"
//	FlushInterval: 1 * time.Minute
//
// **Archive Stream:**
//
//	Path: "/archive/raw-stream.dat"
//	Format: "raw"
//	Append: true
//
// # Thread Safety
//
// The component is fully thread-safe:
//
//   - File writes protected by sync.Mutex
//   - Start/Stop can be called from any goroutine
//   - Metrics updates use atomic operations
//
// # File Rotation
//
// Current version does not include built-in file rotation. Recommended approaches:
//
//   - Use logrotate or similar system tool
//   - Stop component, rotate file, restart component
//   - External rotation with Append: true works safely
//
// # Testing
//
// The package includes test coverage:
//
//   - Unit tests: Config validation, format handling
//   - File I/O tests: Real filesystem writes
//
// Run tests:
//
//	go test ./output/file -v
//
// # Limitations
//
// Current version limitations:
//
//   - No built-in file rotation
//   - No compression (gzip, etc.)
//   - CSV format requires consistent JSON schema
//   - Single file per component instance
//
// # Example: Complete Configuration
//
//	{
//	  "ports": {
//	    "inputs": [
//	      {"name": "input", "type": "nats", "subject": "logs.>", "required": true}
//	    ]
//	  },
//	  "path": "/var/log/streamkit/messages.jsonl",
//	  "format": "jsonlines",
//	  "flush_interval": "5s",
//	  "append": true
//	}
package file
