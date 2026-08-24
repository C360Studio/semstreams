// Package natsclient provides a robust NATS client with circuit breaker protection,
// automatic reconnection, and comprehensive JetStream/KV support for distributed edge systems.
//
// The natsclient package wraps the standard NATS Go client with additional reliability
// features including circuit breaker pattern for failure protection, exponential backoff
// for reconnection, and proper context propagation throughout all operations. It serves
// as the foundation for all NATS communication in the StreamKit framework.
//
// # Core Features
//
// Circuit Breaker Pattern: Prevents cascading failures by failing fast after a threshold
// of consecutive failures (default: 5). The circuit opens to prevent further attempts,
// then gradually tests the connection with exponential backoff.
//
// Connection Lifecycle Management: Handles connection states automatically through the
// lifecycle: Disconnected → Connecting → Connected → Reconnecting → Connected. The client
// manages all transitions with configurable callbacks for state changes.
//
// JetStream Support: Full support for JetStream streams, consumers, and Key-Value stores
// with proper error handling and circuit breaker integration.
//
// KVStore Abstraction: High-level abstraction over NATS KV providing automatic CAS
// (Compare-And-Swap) retry logic, JSON helpers, and consistent error handling for
// configuration management scenarios.
//
// # Basic Usage
//
// Creating and connecting to NATS:
//
//	client, err := natsclient.NewClient("nats://localhost:4222")
//	if err != nil {
//	    return err
//	}
//
//	ctx := context.Background()
//	err = client.Connect(ctx)
//	if err != nil {
//	    return err
//	}
//	defer client.Close(ctx)
//
//	// Publish a message
//	err = client.Publish(ctx, "subject.name", []byte("message data"))
//
//	// Subscribe to messages (receives full *nats.Msg for access to Subject, Data, Headers)
//	err = client.Subscribe(ctx, "subject.*", func(msgCtx context.Context, msg *nats.Msg) {
//	    // Handle message with context (30s timeout per message)
//	    // For wildcard subscriptions, msg.Subject contains the actual subject
//	    fmt.Printf("Received on %s: %s\n", msg.Subject, string(msg.Data))
//	})
//
// # Advanced Configuration
//
// Creating client with options:
//
//	client, err := natsclient.NewClient("nats://localhost:4222",
//	    natsclient.WithMaxReconnects(-1),  // Infinite reconnects
//	    natsclient.WithReconnectWait(2*time.Second),
//	    natsclient.WithCircuitBreakerThreshold(10),
//	    natsclient.WithDisconnectCallback(func(err error) {
//	        log.Printf("Disconnected: %v", err)
//	    }),
//	    natsclient.WithReconnectCallback(func() {
//	        log.Println("Reconnected successfully")
//	    }),
//	)
//
// # Who owns a stream's limits
//
// A stream's limits belong to the component that DECLARES it. If you only READ a
// stream, bind it by name with GetStream and declare nothing:
//
//	if _, err := client.GetStream(ctx, streamName); err != nil {
//	    if errors.Is(err, jetstream.ErrStreamNotFound) {
//	        // The stream is not there RIGHT NOW. Handle that — and do not create
//	        // the stream to make the error go away.
//	        return nil
//	    }
//	    return err
//	}
//
// One probe's not-found is NOT durable evidence that the declaring component is
// absent. On a clustered deployment it is equally what a node that has not applied
// the meta assignment says about a stream that exists. So a decision taken for the
// PROCESS LIFETIME on that answer — disabling a subscriber, skipping a component,
// reporting a deployment shape — is decided by cluster timing, and it fails in the
// worst direction: boot SUCCEEDS and the work silently never happens.
//
// Take that decision from the guarded consumer setup instead.
// ConsumeStreamWithConfig, ConsumeStreamWithConfigContexts and
// ConsumeInternalStreamWithConfig wait out the propagation window, and the error
// they return carries ErrStreamNotVisible only when the stream stayed absent for
// the framework's whole visibility budget. Branch on that sentinel — never on a
// bare jetstream.ErrStreamNotFound, which those same operations also return for
// a consumer-creation or consumer-observation fault against a stream that is
// present.
//
// This is not style. EnsureStream is get-or-create: a reader that calls it with
// its own configuration either creates the stream with limits it does not own, or
// binds an existing one and has its declaration silently discarded — after which
// the stream's limits are decided permanently by boot order. Since the framework's
// provisioner reconciles retention drift, two components declaring one stream
// differently is worse than that: each repairs the other's value on every boot,
// forever. Neither can detect it locally, because a caller sees its own
// declaration and the live stream, never the other declaration.
//
// EnsureStream reports a divergence between what you declared and what a bound
// stream actually carries (see DiffDeclaredStream). A report that returns on every
// boot, with the observed value alternating, is that flap.
//
// # JetStream Operations
//
// Working with JetStream streams and consumers:
//
//	// Create a stream. An ordinary stream MUST declare a finite MaxAge and a
//	// finite MaxBytes: 0 and -1 both mean unlimited to JetStream, so neither is a
//	// declaration, and creation is refused without them. Set Discard explicitly
//	// too — it cannot be required (DiscardOld is the field's zero value) but at a
//	// finite MaxBytes it decides whether the oldest messages are evicted or the
//	// newest are refused.
//	stream, err := client.CreateStream(ctx, jetstream.StreamConfig{
//	    Name:     "EVENTS",
//	    Subjects: []string{"events.>"},
//	    MaxAge:   24 * time.Hour,
//	    MaxBytes: 1 << 30,
//	    Discard:  jetstream.DiscardOld,
//	})
//
//	// Publish to stream
//	err = client.PublishToStream(ctx, "events.user.created", []byte(`{"user_id": "123"}`))
//
//	// A port-backed consumer supplies bounded owner context. The client observes
//	// requested versus effective acknowledgement admission before delivery.
//	ownerCtx, cancelOwner := context.WithCancel(ctx)
//	consumeHandle, err := client.ConsumeStreamWithConfig(ownerCtx,
//	    natsclient.PortConsumerContext{Component: "example", Port: "events"},
//	    natsclient.StreamConsumerConfig{
//	        StreamName: "EVENTS", ConsumerName: "example-events",
//	        FilterSubject: "events.>", AckPolicy: "explicit",
//	    }, func(_ context.Context, msg jetstream.Msg) {
//	    // Process event - msg.Subject() contains actual subject for wildcard filters
//	    processEvent(msg.Subject(), msg.Data())
//	    msg.Ack()
//	})
//	if err != nil {
//	    cancelOwner()
//	    return err
//	}
//	// The owner retains consumeHandle and cancelOwner while running. During Stop,
//	// drain and await exact native completion before canceling callback authority.
//	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancelShutdown()
//	consumeHandle.Drain()
//	select {
//	case <-consumeHandle.Closed():
//	case <-shutdownCtx.Done():
//	    cancelOwner()
//	    return shutdownCtx.Err()
//	}
//	cancelOwner()
//
//	// Framework-internal consumers with no JetStreamPort contract retain exact
//	// native ownership. Drain and await Closed before canceling handler authority.
//	internalHandle, err := client.ConsumeInternalStreamWithConfig(ctx, internalConfig, internalHandler)
//	if err == nil {
//	    internalHandle.Drain()
//	    <-internalHandle.Closed()
//	}
//
// # Key-Value Store
//
// Using KVStore for configuration management with atomic updates:
//
//	// Create or get KV bucket
//	bucket, err := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
//	    Bucket:   "config",
//	    History:  5,
//	    Replicas: 3,
//	})
//
//	// Create KVStore wrapper
//	kvStore := client.NewKVStore(bucket)
//
//	// Atomic JSON update with automatic CAS retry
//	err = kvStore.UpdateJSON(ctx, "service.config", func(config map[string]any) error {
//	    // This function may be called multiple times on conflict
//	    config["enabled"] = true
//	    config["workers"] = 10
//	    return nil
//	})
//
//	// Get JSON value
//	var config map[string]any
//	err = kvStore.GetJSON(ctx, "service.config", &config)
//
// # Circuit Breaker Pattern
//
// The circuit breaker protects against cascading failures:
//
//	// Circuit states:
//	// - Closed: Normal operation, requests pass through
//	// - Open: Failures exceeded threshold, failing fast
//	// - Half-Open: Testing if system recovered
//
//	err := client.Connect(ctx)
//	if errors.Is(err, natsclient.ErrCircuitOpen) {
//	    // Circuit is open, wait for it to test recovery
//	    log.Println("Circuit breaker is open, backing off...")
//	    time.Sleep(client.Backoff())
//	    // Retry later
//	}
//
// Circuit breaker configuration:
//
//	client, err := natsclient.NewClient(url,
//	    natsclient.WithCircuitBreakerThreshold(5),  // Open after 5 failures
//	    natsclient.WithMaxBackoff(time.Minute),     // Max backoff duration
//	)
//
// # Connection Status and Health
//
// Monitoring connection health:
//
//	// Check current status
//	status := client.Status()
//	switch status {
//	case natsclient.StatusConnected:
//	    // Healthy and ready
//	case natsclient.StatusReconnecting:
//	    // Temporarily disconnected, reconnecting
//	case natsclient.StatusCircuitOpen:
//	    // Circuit breaker is open
//	case natsclient.StatusDisconnected:
//	    // Not connected
//	}
//
//	// Get detailed status
//	statusInfo := client.GetStatus()
//	log.Printf("Status: %v, Failures: %d, RTT: %v",
//	    statusInfo.Status,
//	    statusInfo.FailureCount,
//	    statusInfo.RTT)
//
//	// Wait for connection
//	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
//	defer cancel()
//	err := client.WaitForConnection(ctx)
//
// Health monitoring with callbacks:
//
//	client, err := natsclient.NewClient(url,
//	    natsclient.WithHealthCheck(10*time.Second),
//	    natsclient.WithHealthChangeCallback(func(healthy bool) {
//	        if healthy {
//	            log.Println("Connection restored")
//	        } else {
//	            log.Println("Connection lost")
//	        }
//	    }),
//	)
//
// # Error Handling
//
// The package defines specific error types for different failure scenarios:
//
//	var (
//	    ErrCircuitOpen        = errors.New("circuit breaker is open")
//	    ErrNotConnected       = errors.New("not connected to NATS")
//	    ErrConnectionTimeout  = errors.New("connection timeout")
//	)
//
// Error detection patterns:
//
//	err := client.Publish(ctx, "subject", data)
//	if err != nil {
//	    // Check for circuit breaker
//	    if errors.Is(err, natsclient.ErrCircuitOpen) {
//	        // Back off and retry later
//	        return
//	    }
//
//	    // Check for connection issues
//	    if errors.Is(err, natsclient.ErrNotConnected) {
//	        // Trigger reconnection
//	        return
//	    }
//
//	    // Other error
//	    log.Printf("Publish failed: %v", err)
//	}
//
// KV-specific error handling:
//
//	err := kvStore.UpdateJSON(ctx, key, updateFn)
//	if err != nil {
//	    // Check for key not found
//	    if natsclient.IsKVNotFoundError(err) {
//	        // Key doesn't exist, create it
//	    }
//
//	    // Check for conflict (CAS failed after retries)
//	    if natsclient.IsKVConflictError(err) {
//	        // Too many concurrent updates
//	    }
//	}
//
// # Connection Options
//
// Available configuration options:
//
//	WithMaxReconnects(n int)              // Maximum reconnection attempts (-1 = infinite)
//	WithReconnectWait(d time.Duration)    // Wait between reconnection attempts
//	WithTimeout(d time.Duration)          // Connection timeout
//	WithDrainTimeout(d time.Duration)     // Timeout for graceful shutdown
//	WithPingInterval(d time.Duration)     // Health check interval
//	WithCircuitBreakerThreshold(n int)    // Failures before circuit opens
//	WithMaxBackoff(d time.Duration)       // Maximum backoff duration
//	WithLogger(logger Logger)             // Custom logger for debug output
//	WithHealthCheck(d time.Duration)      // Enable health monitoring
//	WithClientName(name string)           // Client identification
//
// # Authentication and Security
//
// Username/password authentication:
//
//	client, err := natsclient.NewClient(url,
//	    natsclient.WithCredentials("username", "password"),
//	)
//
// Token authentication:
//
//	client, err := natsclient.NewClient(url,
//	    natsclient.WithToken("auth-token"),
//	)
//
// TLS configuration:
//
//	client, err := natsclient.NewClient(url,
//	    natsclient.WithTLS(true),
//	    natsclient.WithTLSCerts("client.crt", "client.key"),
//	    natsclient.WithTLSCA("ca.crt"),
//	)
//
// Note: Credentials are cleared from memory when the client is closed.
//
// # Testing
//
// The package provides test utilities for integration testing:
//
//	func TestMyService(t *testing.T) {
//	    // Create test client with real NATS via testcontainers
//	    testClient := natsclient.NewTestClient(t,
//	        natsclient.WithJetStream(),
//	        natsclient.WithKV(),
//	    )
//	    defer testClient.Close()
//
//	    client := testClient.Client
//
//	    // Test with real NATS server
//	    err := client.Publish(ctx, "test.subject", []byte("test data"))
//	    assert.NoError(t, err)
//	}
//
// Testing patterns:
//   - Uses real NATS server via testcontainers (no mocks)
//   - Tests actual behavior including connection lifecycle
//   - Thread-safe testing with proper synchronization
//   - Comprehensive circuit breaker scenario testing
//
// # Thread Safety
//
// The Client type is thread-safe and can be used concurrently from multiple goroutines:
//   - All public methods are safe for concurrent use
//   - Connection state is managed with atomic operations and mutexes
//   - Subscriptions and consumers can be created from any goroutine
//   - Close() can only be called once (subsequent calls are no-ops)
//
// # Performance Considerations
//
// Concurrency: Thread-safe for concurrent use from multiple goroutines. No artificial
// concurrency limits - scales with available system resources.
//
// Memory: Memory usage scales with number of active subscriptions and consumers. Each
// subscription maintains its own message buffer. Health monitoring adds minimal overhead
// (one goroutine with configurable interval).
//
// Throughput: Limited primarily by network latency and NATS server performance. Circuit
// breaker adds negligible overhead in normal operation and fails fast when open.
//
// Connection Lifecycle: Reconnection uses exponential backoff to avoid overwhelming the
// server during failures. Maximum backoff is configurable (default: 1 minute).
//
// # Distributed Tracing
//
// The package provides W3C-compliant trace context propagation for distributed tracing.
// All publish and request methods automatically generate trace context if none exists,
// ensuring complete observability across the message flow.
//
// Trace headers are injected into all outbound NATS messages:
//   - traceparent: W3C Trace Context header (00-{trace_id}-{span_id}-{flags})
//   - X-Trace-ID: Simplified trace ID header
//   - X-Span-ID: Current span identifier
//   - X-Parent-Span-ID: Parent span for nested operations
//
// Using trace context:
//
//	// Traces are auto-generated if not present
//	err := client.Publish(ctx, "subject", data)
//
//	// Or provide explicit trace context
//	tc := natsclient.NewTraceContext()
//	ctx = natsclient.ContextWithTrace(ctx, tc)
//	err := client.Publish(ctx, "subject", data)
//
//	// Extract trace from received message
//	tc := natsclient.ExtractTrace(msg)
//	if tc != nil {
//	    log.Printf("Trace ID: %s, Span ID: %s", tc.TraceID, tc.SpanID)
//	}
//
// Creating child spans for nested operations:
//
//	parentTC, _ := natsclient.TraceContextFromContext(ctx)
//	childTC := parentTC.NewSpan()
//	childCtx := natsclient.ContextWithTrace(ctx, childTC)
//	err := client.Request(childCtx, "service.action", data, timeout)
//
// # The unified RPC error contract (ADR-060)
//
// A request/reply is EITHER a success body (nil Go error) OR a single typed
// error value. A SubscribeForRequests handler that returns a Go error has it
// sent as a header-classified reply:
//
//   - Headers: X-Status: error + X-Error-Class: invalid|transient|fatal
//   - X-Error-Code: <stable machine code> (when coded)
//   - Body: the standard {"message": "...", "detail": {...}} envelope
//
// There is no in-band error channel and no legacy "error: <msg>" body — a
// reply with no X-Status header is success.
//
// Callers use RequestClassified, or RequestWithRetryClassified only when an
// operation-specific non-graph contract authorizes redelivery. Both surface the
// classified error via the err return (transport AND handler failures,
// uniformly). Canonical graph mutations always use one RequestClassified
// attempt; components own any later retry decision:
//
//	data, err := c.RequestClassified(ctx, "subject", body, 5*time.Second)
//	if err != nil {
//	    if errors.Is(err, errs.ErrRevisionMismatch) { /* caller may re-read and retry */ }
//	    if errs.IsInvalid(err)   { /* 4xx — bad input */ }
//	    if errs.IsTransient(err) { /* surface or apply an explicit operation policy */ }
//	    var ce *errs.ClassifiedError
//	    if errors.As(err, &ce)   { /* ce.Code, ce.Detail */ }
//	}
//	json.Unmarshal(data, &resp) // success body; err already handled
//
// FOOTGUN: plain Request() / RequestWithHeaders() do NOT inspect the X-Status
// header — they return the error envelope body with err == nil, which a caller
// would silently json.Unmarshal as a zero-valued success. New callers MUST use
// RequestClassified (or run ClassifyReply on the reply when they need to send
// custom headers). See feedback_silent_handler_error_payload_audit memory.
//
// Handler-side, return a classified error so the headers carry truth:
//
//	return nil, errs.ClassifiedCode(errs.ErrorInvalid,
//	    graph.ErrorCodeEntityNotFound, fmt.Errorf("not found: %s", req.ID))
//	// Consumer via RequestClassified: errs.IsInvalid(err) == true,
//	// errors.As → ce.Code == "entity_not_found"; gateways map that to 404.
//
// # Architecture Integration
//
// The natsclient package integrates with StreamKit components:
//
//   - service: Services use natsclient for pub/sub communication
//   - config: Manager uses KV store for runtime configuration
//   - component: Components receive natsclient for messaging
//   - engine: Flow engine coordinates component communication via NATS
//
// Data flow:
//
//	Application → Client → Circuit Breaker → NATS Connection → NATS Server
//
// # Design Decisions
//
// Circuit Breaker over Simple Retry: Chose circuit breaker pattern to prevent cascade
// failures in distributed systems. After threshold failures, the circuit opens to fail
// fast rather than continuously retry, giving the system time to recover.
//
// Context-First API: Every I/O operation requires context.Context as first parameter
// for proper cancellation and timeout support, essential for production systems.
//
// KVStore Abstraction: Created high-level KV abstraction with built-in CAS retry logic
// to eliminate code duplication across services. Centralizes revision conflict handling
// and retry logic.
//
// Testcontainers over Mocks: Integration tests use real NATS server via testcontainers
// to catch actual integration issues. Mock-based testing can miss edge cases in the
// NATS protocol implementation.
//
// # Examples
//
// Resilient publisher with automatic reconnection:
//
//	package main
//
//	import (
//	    "context"
//	    "log"
//	    "time"
//
//	    "github.com/c360studio/semstreams/natsclient"
//	)
//
//	func main() {
//	    client, err := natsclient.NewClient("nats://localhost:4222",
//	        natsclient.WithMaxReconnects(-1),
//	        natsclient.WithLogger(log.Default()),
//	    )
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//
//	    ctx := context.Background()
//	    if err := client.Connect(ctx); err != nil {
//	        log.Fatal(err)
//	    }
//	    defer client.Close(ctx)
//
//	    // Publish with automatic reconnection handling
//	    for {
//	        err := client.Publish(ctx, "telemetry.data", []byte("sensor reading"))
//	        if err != nil {
//	            if errors.Is(err, natsclient.ErrCircuitOpen) {
//	                log.Println("Circuit open, waiting...")
//	                time.Sleep(5 * time.Second)
//	                continue
//	            }
//	            log.Printf("Publish error: %v", err)
//	        }
//	        time.Sleep(time.Second)
//	    }
//	}
//
// Configuration management with atomic updates:
//
//	// Manage service configuration with optimistic locking
//	bucket, _ := client.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
//	    Bucket:   "config",
//	    History:  5,
//	    Replicas: 3,
//	})
//
//	kvStore := client.NewKVStore(bucket)
//
//	// Atomic configuration update with automatic retry
//	err = kvStore.UpdateJSON(ctx, "services.processor", func(config map[string]any) error {
//	    // This function may be called multiple times on conflict
//	    config["workers"] = 10
//	    config["timeout"] = "30s"
//	    return nil
//	})
//
// For more examples and detailed usage, see the README.md in this directory.
package natsclient
