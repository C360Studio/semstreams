// Package udp provides a UDP input component for receiving data over UDP sockets.
//
// # Overview
//
// The UDP input component enables receiving datagram messages over UDP, with built-in
// buffer overflow handling, retry logic, and NATS integration for message distribution.
// It implements the StreamKit component interfaces for lifecycle management and observability.
//
// # Quick Start
//
// Create a UDP input listening on port 5000:
//
//	config := udp.InputConfig{
//	    Ports: &component.PortConfig{
//	        Outputs: []component.PortDefinition{
//	            {Name: "output", Type: "nats", Subject: "udp.messages", Required: true},
//	        },
//	    },
//	    Address:            "0.0.0.0:5000",
//	    MaxDatagramSize:    8192,
//	    BufferSize:         1000,
//	    BufferOverflowMode: "drop_oldest",
//	}
//
//	rawConfig, _ := json.Marshal(config)
//	input, err := udp.NewInput(rawConfig, deps)
//
// # Configuration
//
// The UDPInputConfig struct controls all aspects of UDP reception:
//
//   - Address: IP:Port to bind to (e.g., "0.0.0.0:5000", ":5000")
//   - MaxDatagramSize: Maximum UDP datagram size in bytes (default: 8192)
//   - BufferSize: Internal buffer size for handling bursts (default: 1000)
//   - BufferOverflowMode: How to handle buffer overflow ("drop_oldest" or "drop_newest")
//   - RetryPolicy: Retry configuration for transient errors
//
// # Buffer Overflow Handling
//
// The component includes sophisticated buffer management to handle traffic bursts:
//
//	// Drop oldest messages when buffer is full
//	BufferOverflowMode: "drop_oldest"
//
//	// Or drop newest messages
//	BufferOverflowMode: "drop_newest"
//
// When buffer overflow occurs:
//  1. Overflow counter increments
//  2. Message logged at Warn level
//  3. Configured drop policy applied
//  4. Processing continues
//
// # Retry Logic
//
// Transient errors (network issues, NATS temporary failures) trigger automatic retry:
//
//	RetryPolicy: &retry.Config{
//	    MaxAttempts:  3,
//	    InitialDelay: 100 * time.Millisecond,
//	    MaxDelay:     5 * time.Second,
//	    Multiplier:   2.0,
//	    AddJitter:    true,
//	}
//
// # Message Flow
//
//	UDP Socket → Buffer → Processing Goroutine → NATS Subject
//	                ↓
//	        Overflow Handling (if buffer full)
//
// # Lifecycle Management
//
// The component implements proper lifecycle with graceful shutdown:
//
//	// Start receiving
//	input.Start(ctx)
//
//	// Graceful shutdown with timeout
//	input.Stop(5 * time.Second)
//
// During shutdown:
//  1. Stop accepting new datagrams
//  2. Drain buffer (process remaining messages)
//  3. Close UDP socket
//  4. Wait for goroutines to complete (with timeout)
//
// # Observability
//
// The component implements component.Discoverable for monitoring:
//
//	meta := input.Meta()
//	// Name: udp-input
//	// Type: input
//	// Description: UDP datagram input
//
//	health := input.Health()
//	// Healthy: true/false
//	// ErrorCount: Total errors encountered
//	// Uptime: Time since Start()
//
//	dataFlow := input.DataFlow()
//	// MessagesPerSecond: Current throughput
//	// BytesPerSecond: Current byte rate
//	// ErrorRate: Error percentage
//
// # Performance Characteristics
//
//   - Throughput: 10,000+ datagrams/second (8KB datagrams)
//   - Memory: O(BufferSize) + per-datagram allocations
//   - Latency: Sub-millisecond for buffered messages
//   - CPU: Minimal (single goroutine for reception)
//
// # Error Handling
//
// The component uses semstreams/errors for consistent error classification:
//
//   - Invalid config: errors.WrapInvalid (bad configuration)
//   - Network errors: errors.WrapTransient (retryable)
//   - NATS errors: errors.WrapTransient (connection issues)
//
// Errors are logged and counted but don't stop the component unless fatal.
//
// # Common Use Cases
//
// **IoT Sensor Data:**
//
//	// Receive sensor datagrams on port 5000
//	Address: "0.0.0.0:5000"
//	MaxDatagramSize: 1024
//	BufferSize: 5000  // Handle bursts from 100s of sensors
//
// **Network Monitoring:**
//
//	// Receive syslog or netflow data
//	Address: "0.0.0.0:514"
//	MaxDatagramSize: 2048
//	BufferOverflowMode: "drop_oldest"  // Keep newest logs
//
// **Multicast Reception:**
//
//	// Join multicast group
//	Address: "239.0.0.1:9999"
//	MaxDatagramSize: 8192
//
// # Thread Safety
//
// The component is fully thread-safe:
//
//   - Start/Stop can be called from any goroutine
//   - Metrics updates use atomic operations
//   - Buffer access protected by sync.Mutex
//
// # Testing
//
// The package includes comprehensive test coverage:
//
//   - Unit tests: Config validation, buffer overflow, error handling
//   - Integration tests: Real UDP sockets with testcontainers NATS
//   - Race tests: Concurrent Start/Stop, buffer access
//   - Leak tests: Goroutine cleanup verification
//   - Panic tests: Error recovery
//
// Run tests:
//
//	go test ./input/udp -v              # Unit tests
//	INTEGRATION_TESTS=1 go test ./input/udp -v  # Integration tests
//	go test ./input/udp -race           # Race detector
//
// # Limitations
//
// Current version limitations:
//
//   - IPv4 only (IPv6 support planned)
//   - Single UDP socket per component instance
//   - No built-in message deduplication
//   - No message ordering guarantees (UDP is unordered)
//
// # Example: Complete Configuration
//
//	{
//	  "ports": {
//	    "outputs": [
//	      {"name": "output", "type": "nats", "subject": "sensors.udp", "required": true}
//	    ]
//	  },
//	  "address": "0.0.0.0:5000",
//	  "max_datagram_size": 8192,
//	  "buffer_size": 1000,
//	  "buffer_overflow_mode": "drop_oldest",
//	  "retry_policy": {
//	    "max_attempts": 3,
//	    "initial_delay": "100ms",
//	    "max_delay": "5s",
//	    "multiplier": 2.0,
//	    "add_jitter": true
//	  }
//	}
package udp
