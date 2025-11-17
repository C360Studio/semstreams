// Package httppost provides an HTTP POST output component for sending messages to HTTP endpoints.
//
// # Overview
//
// The HTTP POST output component sends incoming NATS messages to HTTP/HTTPS endpoints via
// POST requests, with automatic retry logic, exponential backoff, and configurable headers.
// It implements the StreamKit component interfaces for lifecycle management and observability.
//
// # Quick Start
//
// Send messages to an HTTP endpoint:
//
//	config := httppost.Config{
//	    Ports: &component.PortConfig{
//	        Inputs: []component.PortDefinition{
//	            {Name: "input", Type: "nats", Subject: "webhooks.>", Required: true},
//	        },
//	    },
//	    URL:     "https://api.example.com/events",
//	    Timeout: 10 * time.Second,
//	    Headers: map[string]string{
//	        "Content-Type":  "application/json",
//	        "Authorization": "Bearer ${API_KEY}",
//	    },
//	}
//
//	rawConfig, _ := json.Marshal(config)
//	output, err := httppost.NewOutput(rawConfig, deps)
//
// # Configuration
//
// The HTTPPostConfig struct controls HTTP request behavior:
//
//   - URL: Target HTTP/HTTPS endpoint
//   - Method: HTTP method (default: "POST")
//   - Headers: Custom HTTP headers (map[string]string)
//   - Timeout: Request timeout (default: 10s)
//   - RetryCount: Number of retry attempts (default: 3)
//   - RetryDelay: Initial retry delay (default: 1s)
//   - RetryBackoff: Backoff multiplier (default: 2.0)
//
// # Retry Logic
//
// Automatic retry with exponential backoff for failed requests:
//
//	RetryCount: 3
//	RetryDelay: 1 * time.Second
//	RetryBackoff: 2.0
//
//	// Retry schedule:
//	// Attempt 1: Immediate
//	// Attempt 2: After 1s
//	// Attempt 3: After 2s
//	// Attempt 4: After 4s
//
// Retryable conditions:
//   - Network errors (connection refused, timeout)
//   - 5xx server errors (500, 502, 503, 504)
//   - Temporary failures (429 Too Many Requests)
//
// Non-retryable conditions:
//   - 4xx client errors (except 429)
//   - Invalid configuration
//   - Request body marshaling errors
//
// # HTTP Headers
//
// Custom headers support environment variable expansion:
//
//	Headers: map[string]string{
//	    "Content-Type":  "application/json",
//	    "Authorization": "Bearer ${API_KEY}",  // Reads from env
//	    "X-Custom":      "static-value",
//	}
//
// Standard headers automatically set:
//   - Content-Type: application/json (if not specified)
//   - Content-Length: Calculated from request body
//
// # Message Flow
//
//	NATS Subject → Message Handler → HTTP POST → Retry (if failed) → Success/Error
//
// # Response Handling
//
// HTTP response codes determine success/failure:
//
//	2xx: Success
//	3xx: Redirect (followed automatically up to 10 times)
//	4xx: Client error (not retried, except 429)
//	5xx: Server error (retried with backoff)
//
// Response bodies are read and discarded to enable connection reuse.
//
// # Lifecycle Management
//
// Proper HTTP client lifecycle with graceful shutdown:
//
//	// Start posting
//	output.Start(ctx)
//
//	// Graceful shutdown
//	output.Stop(5 * time.Second)
//
// During shutdown:
//  1. Stop accepting new messages
//  2. Wait for in-flight requests to complete
//  3. Close HTTP client connections
//
// # Observability
//
// The component implements component.Discoverable for monitoring:
//
//	meta := output.Meta()
//	// Name: httppost-output
//	// Type: output
//	// Description: HTTP POST output
//
//	health := output.Health()
//	// Healthy: true if recent requests succeeded
//	// ErrorCount: Failed requests
//	// Uptime: Time since Start()
//
//	dataFlow := output.DataFlow()
//	// MessagesPerSecond: POST rate
//	// BytesPerSecond: Byte throughput
//	// ErrorRate: Failure percentage
//
// # Performance Characteristics
//
//   - Throughput: Network-dependent (100-1000 requests/second typical)
//   - Memory: O(concurrent requests)
//   - Latency: Network RTT + server processing
//   - Connections: Reused via HTTP keep-alive
//
// # Error Handling
//
// The component uses streamkit/errors for consistent error classification:
//
//   - Invalid config: errors.WrapInvalid (bad URL, invalid headers)
//   - Network errors: errors.WrapTransient (retryable)
//   - HTTP 4xx: errors.WrapInvalid (client error, not retried)
//   - HTTP 5xx: errors.WrapTransient (server error, retried)
//
// All errors are logged with structured context (URL, status code, retry attempt).
//
// # Common Use Cases
//
// **Webhook Integration:**
//
//	URL: "https://webhook.site/unique-id"
//	Timeout: 5 * time.Second
//	RetryCount: 3
//
// **API Integration:**
//
//	URL: "https://api.example.com/v1/events"
//	Headers: {"Authorization": "Bearer ${TOKEN}"}
//	Timeout: 10 * time.Second
//
// **Third-Party Service:**
//
//	URL: "https://metrics.service.com/ingest"
//	Method: "POST"
//	RetryCount: 5  // Higher retry for critical data
//
// # Thread Safety
//
// The component is fully thread-safe:
//
//   - HTTP client is thread-safe (shared across goroutines)
//   - Start/Stop can be called from any goroutine
//   - Metrics updates use atomic operations
//
// # Testing
//
// The package includes comprehensive test coverage:
//
//   - Unit tests: Config validation, retry logic, status codes
//   - HTTP tests: Using httptest for mocked endpoints
//   - Backoff tests: Exponential backoff verification
//   - Header tests: Custom header handling
//
// Run tests:
//
//	go test ./output/httppost -v
//
// # Limitations
//
// Current version limitations:
//
//   - No request batching (one HTTP request per message)
//   - No circuit breaker pattern
//   - No rate limiting
//   - No request signing (HMAC, AWS Signature v4, etc.)
//   - POST only (no PUT, PATCH, etc.) - use Method field for others
//
// # Security Considerations
//
//   - HTTPS strongly recommended for sensitive data
//   - API keys should use environment variables, not hardcoded
//   - Validate SSL certificates (no InsecureSkipVerify)
//   - Use timeouts to prevent hanging requests
//
// # Example: Complete Configuration
//
//	{
//	  "ports": {
//	    "inputs": [
//	      {"name": "input", "type": "nats", "subject": "events.webhook", "required": true}
//	    ]
//	  },
//	  "url": "https://api.example.com/webhook",
//	  "method": "POST",
//	  "headers": {
//	    "Content-Type": "application/json",
//	    "Authorization": "Bearer ${API_TOKEN}",
//	    "X-Source": "streamkit"
//	  },
//	  "timeout": "10s",
//	  "retry_count": 3,
//	  "retry_delay": "1s",
//	  "retry_backoff": 2.0
//	}
package httppost
