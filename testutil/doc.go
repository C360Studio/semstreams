// Package testutil provides testing utilities for StreamKit integration tests.
//
// # Overview
//
// The testutil package contains mock implementations, test data generators, and
// helper functions designed to simplify writing integration tests for StreamKit
// components. All utilities are framework-agnostic with ZERO semantic domain
// concepts (no EntityID, MAVLink, robotics, or SOSA/SSN knowledge).
//
// # Core Components
//
// Mock Implementations:
//
// MockNATSClient - In-memory NATS client for testing pub/sub patterns:
//   - Thread-safe for concurrent use
//   - Stores all published messages for verification
//   - Supports subscription handlers
//   - No external NATS server required
//
// MockKVStore - In-memory key-value store for testing storage:
//   - Thread-safe for concurrent use
//   - Simple Put/Get/Delete/Keys/Clear interface
//   - No external database required
//
// MockComponent - Generic lifecycle component for testing:
//   - Tracks Start/Stop/Process call counts
//   - Thread-safe counters
//   - Configurable error injection
//   - Lifecycle state tracking
//
// MockPort - Simple port abstraction for testing:
//   - Stores messages sent to port
//   - Thread-safe message list
//
// Test Data Generators:
//
// Provides generic test data for common formats:
//   - Generic JSON objects (no semantic meaning)
//   - CSV data
//   - HTTP request/response pairs
//   - Binary buffer data (small, medium, large)
//
// Flow Configuration Builder:
//
// FlowBuilder - Programmatic flow configuration:
//   - Fluent API for building flow configurations
//   - Method chaining for readability
//   - Sensible defaults
//   - Minimal boilerplate in tests
//
// Test Helpers:
//
//   - WaitForMessage: Polls for message with timeout
//   - WaitForMessageCount: Waits for N messages
//   - AssertMessageReceived: Verifies message delivery
//   - AssertNoMessages: Verifies no messages sent
//
// # Design Principles
//
// Framework-Agnostic:
//
// All test utilities avoid semantic domain knowledge. Instead of:
//   - ❌ CreateRoboticsEvent() - semantic concept
//   - ✅ CreateGenericJSON() - domain-agnostic
//
// This ensures testutil can be used across different applications without
// coupling to specific domains.
//
// Thread Safety:
//
// All mock types are safe for concurrent use from multiple goroutines.
// This enables testing concurrent message flows without data races:
//
//	// Safe to use from multiple goroutines
//	go client.Publish(ctx, "subject1", data1)
//	go client.Publish(ctx, "subject2", data2)
//	go handler1(client.GetMessages("subject1"))
//	go handler2(client.GetMessages("subject2"))
//
// Real Dependencies Preferred:
//
// Use mocks ONLY when real dependencies are impractical:
//   - ✅ Use testcontainers for NATS (real behavior)
//   - ⚠️ Use MockNATSClient when testcontainers unavailable
//   - ❌ Don't mock when real dependencies are fast/easy
//
// # Usage Examples
//
// Basic MockNATSClient:
//
//	func TestPublishSubscribe(t *testing.T) {
//	    client := testutil.NewMockNATSClient()
//
//	    // Subscribe to subject
//	    var received []byte
//	    err := client.Subscribe(ctx, "test.subject", func(_ context.Context, data []byte) {
//	        received = data
//	    })
//	    require.NoError(t, err)
//
//	    // Publish message
//	    err = client.Publish(ctx, "test.subject", []byte("hello"))
//	    require.NoError(t, err)
//
//	    // Verify message received
//	    assert.Equal(t, []byte("hello"), received)
//	}
//
// Wait Helpers:
//
//	func TestMessageFlow(t *testing.T) {
//	    client := testutil.NewMockNATSClient()
//
//	    // Start async publisher
//	    go func() {
//	        time.Sleep(100 * time.Millisecond)
//	        client.Publish(ctx, "events", []byte("data"))
//	    }()
//
//	    // Wait for message with timeout
//	    msg := testutil.WaitForMessage(t, client, "events", time.Second)
//	    assert.Equal(t, []byte("data"), msg)
//	}
//
// FlowBuilder:
//
//	func TestFlowConfiguration(t *testing.T) {
//	    // Build flow programmatically
//	    flow := testutil.NewFlowBuilder("test-flow").
//	        AddInput("udp-input", "udp", map[string]any{
//	            "port": 14550,
//	        }).
//	        AddProcessor("filter", "json_filter", map[string]any{
//	            "path": "$.type",
//	        }).
//	        AddOutput("ws-output", "websocket", map[string]any{
//	            "port": 8080,
//	        }).
//	        Build()
//
//	    // Use in test
//	    flowJSON, _ := json.Marshal(flow)
//	    // ... test flow configuration
//	}
//
// MockKVStore:
//
//	func TestStorage(t *testing.T) {
//	    kv := testutil.NewMockKVStore()
//
//	    // Store values
//	    err := kv.Put("key1", []byte("value1"))
//	    require.NoError(t, err)
//
//	    // Retrieve values
//	    val, err := kv.Get("key1")
//	    require.NoError(t, err)
//	    assert.Equal(t, []byte("value1"), val)
//
//	    // List keys
//	    keys := kv.Keys()
//	    assert.Contains(t, keys, "key1")
//	}
//
// MockComponent:
//
//	func TestComponentLifecycle(t *testing.T) {
//	    mock := testutil.NewMockComponent()
//
//	    // Start component
//	    err := mock.Start(ctx)
//	    require.NoError(t, err)
//	    assert.Equal(t, 1, mock.StartCalls)
//	    assert.True(t, mock.Started)
//
//	    // Process data
//	    err = mock.Process("test-data")
//	    require.NoError(t, err)
//	    assert.Equal(t, 1, mock.ProcessCalls)
//
//	    // Stop component
//	    err = mock.Stop(ctx)
//	    require.NoError(t, err)
//	    assert.Equal(t, 1, mock.StopCalls)
//	    assert.False(t, mock.Started)
//	}
//
// # Thread Safety Guarantees
//
// All mock types use sync.RWMutex for thread safety:
//
// MockNATSClient:
//   - Publish() - Write lock for map updates, releases before calling handlers
//   - Subscribe() - Write lock
//   - GetMessages() - Read lock, returns copy of slice
//   - Clear()/ClearAll() - Write lock
//
// MockKVStore:
//   - Put()/Delete() - Write lock
//   - Get() - Read lock, returns copy of value
//   - Keys() - Read lock
//   - Clear() - Write lock
//
// MockComponent:
//   - Start()/Stop()/Process() - Write lock for counter updates
//   - Thread-safe call counters
//
// # Performance Considerations
//
// WaitForMessage Polling:
//
// The WaitForMessage helper uses polling (10ms intervals) which adds latency:
//   - Each wait adds minimum 10ms to test execution
//   - With 100 tests using this → +1 second total
//
// For unit tests, prefer direct assertions. For integration tests where async
// behavior is expected, the polling overhead is acceptable.
//
// Mock vs Real Dependencies:
//
// Decision matrix:
//
//	| Scenario                    | Use Mock          | Use Real (testcontainers) |
//	|-----------------------------|-------------------|---------------------------|
//	| Unit test (component logic) | ✅ MockNATSClient | ❌ Overkill               |
//	| Integration test (E2E flow) | ❌ Incomplete     | ✅ Real NATS              |
//	| CI/CD pipeline             | ⚠️ Fast but fake   | ✅ Slow but real          |
//	| Local development          | ✅ Fast iteration | ⚠️ Docker overhead         |
//
// # Test Data Guidelines
//
// Generic vs Semantic Data:
//
// testutil provides GENERIC data - avoid creating semantic test data:
//
//	// ❌ BAD - semantic robotics data
//	robotEvent := map[string]any{
//	    "entity_id": "robot-123",
//	    "pose": {"x": 1.0, "y": 2.0},
//	}
//
//	// ✅ GOOD - generic JSON
//	genericEvent := testutil.TestJSONData["simple"]
//
// If you need semantic data, create it in your test package, not in testutil.
//
// # Integration with Framework
//
// The testutil package integrates with StreamKit testing patterns:
//
// Shared NATS Client Pattern:
//
// For integration tests using real NATS:
//
//	func getSharedNATSClient(t *testing.T) *natsclient.Client {
//	    // Use testcontainers - one NATS server per test run
//	    // NOT testutil.MockNATSClient
//	}
//
// Use MockNATSClient only for unit tests or when testcontainers unavailable.
//
// # Known Limitations
//
//  1. WaitForMessage uses polling (10ms) - adds latency to tests
//  2. No support for NATS features like headers, request/reply patterns
//  3. MockKVStore doesn't support transactions or atomic operations
//  4. No metrics or observability for mock operations
//  5. FlowBuilder doesn't validate flow correctness
//
// These are design trade-offs - mocks prioritize simplicity over completeness.
//
// # See Also
//
//   - component: Component interface and lifecycle
//   - natsclient: Real NATS client wrapper
//   - types: Shared domain types
package testutil
