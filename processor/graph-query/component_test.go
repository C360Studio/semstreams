package graphquery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ====================================================================================
// Mock NATS Client for Request/Reply Testing
// ====================================================================================

type mockNATSClient struct {
	requestFunc func(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
	// requestClassifiedFunc, when set, overrides the full RequestClassified
	// implementation. Use this when a test must inject a classified error
	// (e.g. Transient, Fatal) that the legacy body-prefix path cannot
	// produce — the legacy path always classifies body-prefix errors as
	// Invalid (gh#163 test coverage for the 3-way branch).
	requestClassifiedFunc func(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error)
	subscribed            map[string]bool
	connected             bool
	status                natsclient.ConnectionStatus
	// buckets allows tests to configure which KV buckets exist.
	// If a bucket name is in this map, GetKeyValueBucket returns it.
	// If not, GetKeyValueBucket returns error immediately (no retry delay).
	buckets map[string]jetstream.KeyValue
}

func newMockNATSClient() *mockNATSClient {
	return &mockNATSClient{
		subscribed: make(map[string]bool),
		connected:  true,
		status:     natsclient.StatusConnected,
		buckets:    make(map[string]jetstream.KeyValue),
	}
}

func (m *mockNATSClient) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) {
	if m.requestFunc != nil {
		return m.requestFunc(ctx, subject, data, timeout)
	}
	return nil, errors.New("no mock response configured")
}

// RequestClassified routes through requestClassifiedFunc when set
// (for tests injecting classified errors directly, e.g. Transient/Fatal),
// otherwise falls through to Request + ClassifyReply using a synthetic
// success-only message (no X-Status header). ADR-060 PR-D removed the
// legacy "error: " body-prefix fallback from ClassifyReply; a body
// returned by requestFunc with nil error is now always treated as success.
// Tests that simulate handler errors must set requestClassifiedFunc and
// return (nil, classifiedErr) directly.
func (m *mockNATSClient) RequestClassified(ctx context.Context, subject string, data []byte, timeout time.Duration) ([]byte, error) {
	if m.requestClassifiedFunc != nil {
		return m.requestClassifiedFunc(ctx, subject, data, timeout)
	}
	body, err := m.Request(ctx, subject, data, timeout)
	if err != nil {
		return nil, err
	}
	// Synthesize a *nats.Msg with no X-Status header so ClassifyReply
	// treats the body as a success response (the only shape it accepts
	// after ADR-060 PR-D). Avoids depending on a real connection.
	msg := &nats.Msg{Data: body, Header: nats.Header{}}
	return natsclient.ClassifyReply(msg)
}

func (m *mockNATSClient) SubscribeForRequests(_ context.Context, subject string, _ func(ctx context.Context, data []byte) ([]byte, error)) (*natsclient.Subscription, error) {
	m.subscribed[subject] = true
	// Return a nil subscription since the mock doesn't actually subscribe
	return nil, nil
}

func (m *mockNATSClient) Status() natsclient.ConnectionStatus {
	return m.status
}

func (m *mockNATSClient) Connect(_ context.Context) error {
	m.connected = true
	return nil
}

func (m *mockNATSClient) WaitForConnection(_ context.Context) error {
	if !m.connected {
		return errors.New("not connected")
	}
	return nil
}

func (m *mockNATSClient) JetStream() (jetstream.JetStream, error) {
	return nil, errors.New("mock: JetStream not implemented")
}

func (m *mockNATSClient) GetKeyValueBucket(_ context.Context, name string) (jetstream.KeyValue, error) {
	if bucket, ok := m.buckets[name]; ok {
		return bucket, nil
	}
	// Return error immediately - no bucket configured means instant "not found"
	// This prevents 5s retry delays in tests
	return nil, errors.New("mock: bucket not available")
}

// ====================================================================================
// Config Tests
// ====================================================================================

func TestConfig_Validate_ValidConfig(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "valid minimal config",
			config: Config{
				Ports: &component.PortConfig{
					Inputs: []component.PortDefinition{
						{Name: "query_entity", Type: "nats-request", Subject: "graph.query.entity"},
					},
					Outputs: []component.PortDefinition{},
				},
			},
		},
		{
			name: "valid full config",
			config: Config{
				Ports: &component.PortConfig{
					Inputs: []component.PortDefinition{
						{Name: "query_entity", Type: "nats-request", Subject: "graph.query.entity"},
						{Name: "query_relationships", Type: "nats-request", Subject: "graph.query.relationships"},
						{Name: "query_path_search", Type: "nats-request", Subject: "graph.query.pathSearch"},
					},
					Outputs: []component.PortDefinition{},
				},
				QueryTimeout: 5 * time.Second,
				MaxDepth:     10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestConfig_Validate_MissingPorts(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "missing ports config",
			config: Config{
				Ports: nil,
			},
			wantErr: true,
		},
		{
			name: "empty inputs",
			config: Config{
				Ports: &component.PortConfig{
					Inputs:  []component.PortDefinition{},
					Outputs: []component.PortDefinition{},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfig_ApplyDefaults(t *testing.T) {
	config := Config{
		Ports: &component.PortConfig{
			Inputs:  []component.PortDefinition{{Name: "query_entity", Type: "nats-request", Subject: "graph.query.entity"}},
			Outputs: []component.PortDefinition{},
		},
	}

	config.ApplyDefaults()

	// Verify defaults are applied
	assert.Equal(t, 5*time.Second, config.QueryTimeout, "QueryTimeout should default to 5s")
	assert.Equal(t, 10, config.MaxDepth, "MaxDepth should default to 10")
}

func TestDefaultConfig_ReturnsValidConfig(t *testing.T) {
	config := DefaultConfig()

	// Default config should pass validation
	err := config.Validate()
	assert.NoError(t, err)

	// Verify expected defaults
	assert.NotNil(t, config.Ports)
	assert.NotEmpty(t, config.Ports.Inputs)
	assert.Equal(t, 5*time.Second, config.QueryTimeout)
	assert.Equal(t, 10, config.MaxDepth)
}

// ====================================================================================
// Discoverable Interface Tests
// ====================================================================================

func TestComponent_Meta_ReturnsCorrectMetadata(t *testing.T) {
	comp := createTestComponent(t)

	meta := comp.Meta()

	assert.Equal(t, "processor", meta.Type)
	assert.Equal(t, "graph-query", meta.Name)
	assert.NotEmpty(t, meta.Description)
	assert.NotEmpty(t, meta.Version)
}

func TestComponent_InputPorts_ReturnsNATSRequestPorts(t *testing.T) {
	comp := createTestComponent(t)
	require.NoError(t, comp.Initialize())

	ports := comp.InputPorts()

	require.NotEmpty(t, ports, "should have at least one input port")

	// Verify all ports are NATS request ports
	for _, port := range ports {
		assert.NotEmpty(t, port.Name)
		assert.Equal(t, component.DirectionInput, port.Direction)

		if port.Config != nil {
			_, ok := port.Config.(component.NATSRequestPort)
			assert.True(t, ok, "input ports should be NATSRequestPort type")
		}
	}
}

func TestComponent_OutputPorts_ReturnsEmpty(t *testing.T) {
	comp := createTestComponent(t)
	require.NoError(t, comp.Initialize())

	ports := comp.OutputPorts()

	// Query coordinator has no output ports - it returns data via request/reply
	assert.Empty(t, ports, "graph-query should have no output ports")
}

func TestComponent_ConfigSchema_ReturnsValidSchema(t *testing.T) {
	comp := createTestComponent(t)

	schema := comp.ConfigSchema()

	// Verify schema has required properties
	assert.NotNil(t, schema.Properties)

	// Check for expected config properties
	_, hasPortsProperty := schema.Properties["ports"]
	assert.True(t, hasPortsProperty, "schema should have 'ports' property")

	_, hasTimeoutProperty := schema.Properties["query_timeout"]
	assert.True(t, hasTimeoutProperty, "schema should have 'query_timeout' property")

	_, hasMaxDepthProperty := schema.Properties["max_depth"]
	assert.True(t, hasMaxDepthProperty, "schema should have 'max_depth' property")
}

func TestComponent_Health_NotStarted(t *testing.T) {
	comp := createTestComponent(t)

	health := comp.Health()

	assert.False(t, health.Healthy, "component should be unhealthy before start")
	assert.Equal(t, 0, health.ErrorCount)
}

func TestComponent_DataFlow_ReturnsMetrics(t *testing.T) {
	comp := createTestComponent(t)

	metrics := comp.DataFlow()

	// Verify FlowMetrics structure
	assert.GreaterOrEqual(t, metrics.MessagesPerSecond, float64(0))
	assert.GreaterOrEqual(t, metrics.BytesPerSecond, float64(0))
	assert.GreaterOrEqual(t, metrics.ErrorRate, float64(0))
}

// ====================================================================================
// LifecycleComponent Interface Tests
// ====================================================================================

func TestComponent_Initialize_Success(t *testing.T) {
	comp := createTestComponent(t)

	err := comp.Initialize()

	assert.NoError(t, err)
}

func TestComponent_Initialize_InvalidConfig(t *testing.T) {
	mockClient := newMockNATSClient()

	comp := &Component{
		config: Config{
			Ports: nil, // Invalid - missing ports
		},
		natsClient: mockClient,
	}

	err := comp.Initialize()

	assert.Error(t, err, "Initialize should fail with invalid config")
}

func TestComponent_Start_Success(t *testing.T) {
	comp := createTestComponent(t)
	ctx := context.Background()

	require.NoError(t, comp.Initialize())
	err := comp.Start(ctx)
	defer comp.Stop(1 * time.Second)

	assert.NoError(t, err)
}

func TestComponent_Start_BeforeInitialize(t *testing.T) {
	comp := createTestComponent(t)
	ctx := context.Background()

	// Start without Initialize
	err := comp.Start(ctx)

	assert.Error(t, err, "Start should fail if not initialized")
}

func TestComponent_Start_AlreadyStarted(t *testing.T) {
	comp := createTestComponent(t)
	ctx := context.Background()

	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(ctx))
	defer comp.Stop(1 * time.Second)

	// Start again - should be idempotent
	err := comp.Start(ctx)

	assert.NoError(t, err, "Start should be idempotent")
}

func TestComponent_Stop_Success(t *testing.T) {
	comp := createTestComponent(t)
	ctx := context.Background()

	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(ctx))

	err := comp.Stop(5 * time.Second)

	assert.NoError(t, err)
}

func TestComponent_Stop_BeforeStart(t *testing.T) {
	comp := createTestComponent(t)

	// Stop without Start
	err := comp.Stop(1 * time.Second)

	assert.NoError(t, err, "Stop should be safe even if not started")
}

// ====================================================================================
// Factory and Registration Tests
// ====================================================================================

func TestCreateGraphQuery_ValidConfig(t *testing.T) {
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	// Use real natsclient.Client for factory test (doesn't need to be connected)
	realClient, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: realClient,
	}

	comp, err := CreateGraphQuery(configJSON, deps)

	assert.NoError(t, err)
	assert.NotNil(t, comp)
	assert.Implements(t, (*component.Discoverable)(nil), comp)
	assert.Implements(t, (*component.LifecycleComponent)(nil), comp)
}

func TestCreateGraphQuery_InvalidConfig(t *testing.T) {
	// Invalid JSON
	invalidJSON := []byte(`{invalid json}`)

	// Use real natsclient.Client for factory test (doesn't need to be connected)
	realClient, err := natsclient.NewClient("nats://localhost:4222")
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: realClient,
	}

	comp, err := CreateGraphQuery(invalidJSON, deps)

	assert.Error(t, err)
	assert.Nil(t, comp)
}

func TestCreateGraphQuery_MissingDependencies(t *testing.T) {
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	// Missing NATSClient dependency
	deps := component.Dependencies{
		NATSClient: nil,
	}

	comp, err := CreateGraphQuery(configJSON, deps)

	assert.Error(t, err, "should fail without NATSClient")
	assert.Nil(t, comp)
}

func TestRegister_AddsToRegistry(t *testing.T) {
	registry := component.NewRegistry()

	err := Register(registry)

	assert.NoError(t, err)

	// Verify factory was registered
	factories := registry.ListFactories()
	_, exists := factories["graph-query"]
	assert.True(t, exists, "graph-query factory should be registered")
}

// ====================================================================================
// Query Handler Tests - Passthrough Queries
// ====================================================================================

func TestComponent_QueryEntity_PassthroughSuccess(t *testing.T) {
	mockClient := newMockNATSClient()

	// Mock response from graph-ingest
	entityResponse := []byte(`{"id":"test.entity.001","triples":[]}`)
	mockClient.requestFunc = func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		// Actual query should go to graph-ingest
		assert.Equal(t, "graph.ingest.query.entity", subject, "should forward to graph-ingest")

		var req map[string]string
		err := json.Unmarshal(data, &req)
		require.NoError(t, err)
		assert.Equal(t, "test.entity.001", req["id"])

		return entityResponse, nil
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	queryData := []byte(`{"id":"test.entity.001"}`)

	response, err := comp.handleQueryEntity(ctx, queryData)

	assert.NoError(t, err)
	assert.Equal(t, entityResponse, response)
}

func TestComponent_QueryEntity_ComponentUnavailable(t *testing.T) {
	mockClient := newMockNATSClient()

	// Mock timeout error from graph-ingest
	mockClient.requestFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		return nil, nats.ErrTimeout
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	queryData := []byte(`{"id":"test.entity.001"}`)

	response, err := comp.handleQueryEntity(ctx, queryData)

	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "timeout", "should report timeout error")
}

func TestComponent_QueryEntity_InvalidRequest(t *testing.T) {
	mockClient := newMockNATSClient()

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	invalidData := []byte(`{invalid json}`)

	response, err := comp.handleQueryEntity(ctx, invalidData)

	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "invalid", "should report invalid request")
}

func TestComponent_QueryRelationships_TransformSuccess(t *testing.T) {
	mockClient := newMockNATSClient()

	// Mock response from graph-index (QueryResponse envelope with OutgoingRelationshipsData)
	indexResponse := []byte(`{"data":{"relationships":[{"to_entity_id":"test.entity.002","predicate":"test.relationship"}]},"timestamp":"2026-01-09T00:00:00Z"}`)
	mockClient.requestFunc = func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		// Actual query should go to graph-index
		assert.Equal(t, "graph.index.query.outgoing", subject, "should forward to graph-index")

		var req map[string]string
		err := json.Unmarshal(data, &req)
		require.NoError(t, err)
		assert.Equal(t, "test.entity.001", req["entity_id"])

		return indexResponse, nil
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	queryData := []byte(`{"entity_id":"test.entity.001"}`)

	response, err := comp.handleQueryRelationships(ctx, queryData)

	assert.NoError(t, err)
	// Handler transforms graph-index format to normalized API format
	expectedResponse := `[{"edge_type":"test.relationship","from_entity_id":"test.entity.001","to_entity_id":"test.entity.002"}]`
	assert.JSONEq(t, expectedResponse, string(response))
}

// ====================================================================================
// Query Handler Tests - PathRAG Orchestration
// ====================================================================================

func TestComponent_PathSearch_SimpleTraversal(t *testing.T) {
	mockClient := newMockNATSClient()

	// Track request sequence
	requestCount := 0
	mockClient.requestFunc = func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		requestCount++

		switch subject {
		case "graph.ingest.query.entity":
			// Return start entity
			return []byte(`{"id":"test.entity.001","triples":[]}`), nil

		case "graph.index.query.outgoing":
			// Return relationships (1 hop) in QueryResponse envelope format
			var req map[string]string
			json.Unmarshal(data, &req)

			if req["entity_id"] == "test.entity.001" {
				return []byte(`{"data":{"relationships":[{"to_entity_id":"test.entity.002","predicate":"relates_to"}]},"timestamp":"2026-01-09T00:00:00Z"}`), nil
			}
			return []byte(`{"data":{"relationships":[]},"timestamp":"2026-01-09T00:00:00Z"}`), nil

		default:
			return nil, errors.New("unexpected subject")
		}
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	queryData := []byte(`{"start_entity":"test.entity.001","max_depth":2}`)

	response, err := comp.handlePathSearch(ctx, queryData)

	assert.NoError(t, err)
	assert.NotNil(t, response)

	// Verify traversal occurred
	assert.Greater(t, requestCount, 1, "should make multiple requests for traversal")

	// Verify response contains entities
	var result map[string]interface{}
	err = json.Unmarshal(response, &result)
	require.NoError(t, err)

	entities, ok := result["entities"]
	assert.True(t, ok, "response should contain entities field")
	assert.NotNil(t, entities)
}

func TestComponent_PathSearch_MaxDepthEnforced(t *testing.T) {
	mockClient := newMockNATSClient()

	callCount := 0
	mockClient.requestFunc = func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		callCount++

		switch subject {
		case "graph.ingest.query.entity":
			return []byte(`{"id":"test.entity.001","triples":[]}`), nil

		case "graph.index.query.outgoing":
			// Always return next entity (infinite graph simulation) in QueryResponse envelope format
			var req map[string]string
			json.Unmarshal(data, &req)
			nextID := req["entity_id"] + ".next"
			return []byte(`{"data":{"relationships":[{"to_entity_id":"` + nextID + `","predicate":"relates_to"}]},"timestamp":"2026-01-09T00:00:00Z"}`), nil

		default:
			return nil, errors.New("unexpected subject")
		}
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	comp.config.MaxDepth = 3 // Override default
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	queryData := []byte(`{"start_entity":"test.entity.001","max_depth":3}`)

	response, err := comp.handlePathSearch(ctx, queryData)

	assert.NoError(t, err)
	assert.NotNil(t, response)

	// Should stop at max_depth, not traverse infinitely
	assert.Less(t, callCount, 20, "should respect max depth and not traverse indefinitely")
}

func TestComponent_PathSearch_ContextCancellation(t *testing.T) {
	mockClient := newMockNATSClient()

	mockClient.requestFunc = func(ctx context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		// Check context cancellation
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Simulate slow response
		time.Sleep(50 * time.Millisecond)
		return []byte(`{"id":"test.entity.001","triples":[]}`), nil
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	queryData := []byte(`{"start_entity":"test.entity.001","max_depth":2}`)

	response, err := comp.handlePathSearch(ctx, queryData)

	// Should fail due to context cancellation
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "context", "should report context cancellation")
}

func TestComponent_PathSearch_Timeout(t *testing.T) {
	mockClient := newMockNATSClient()

	mockClient.requestFunc = func(_ context.Context, _ string, _ []byte, _ time.Duration) ([]byte, error) {
		// Simulate timeout
		return nil, context.DeadlineExceeded
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	queryData := []byte(`{"start_entity":"test.entity.001","max_depth":2}`)

	response, err := comp.handlePathSearch(ctx, queryData)

	assert.Error(t, err)
	assert.Nil(t, response)
}

func TestComponent_PathSearch_StartEntityNotFound(t *testing.T) {
	mockClient := newMockNATSClient()

	mockClient.requestFunc = func(_ context.Context, subject string, _ []byte, _ time.Duration) ([]byte, error) {
		if subject == "graph.ingest.query.entity" {
			return nil, errors.New("entity not found")
		}
		return nil, errors.New("unexpected subject")
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	queryData := []byte(`{"start_entity":"nonexistent.entity","max_depth":2}`)

	response, err := comp.handlePathSearch(ctx, queryData)

	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "entity", "should report entity not found")
}

func TestComponent_PathSearch_CyclicGraph(t *testing.T) {
	mockClient := newMockNATSClient()

	mockClient.requestFunc = func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.ingest.query.entity":
			return []byte(`{"id":"test.entity.001","triples":[]}`), nil

		case "graph.index.query.outgoing":
			var req map[string]string
			json.Unmarshal(data, &req)

			// Create cycle: 001 -> 002 -> 003 -> 001 (using QueryResponse envelope format)
			switch req["entity_id"] {
			case "test.entity.001":
				return []byte(`{"data":{"relationships":[{"to_entity_id":"test.entity.002","predicate":"relates_to"}]},"timestamp":"2026-01-09T00:00:00Z"}`), nil
			case "test.entity.002":
				return []byte(`{"data":{"relationships":[{"to_entity_id":"test.entity.003","predicate":"relates_to"}]},"timestamp":"2026-01-09T00:00:00Z"}`), nil
			case "test.entity.003":
				return []byte(`{"data":{"relationships":[{"to_entity_id":"test.entity.001","predicate":"relates_to"}]},"timestamp":"2026-01-09T00:00:00Z"}`), nil
			default:
				return []byte(`{"data":{"relationships":[]},"timestamp":"2026-01-09T00:00:00Z"}`), nil
			}

		default:
			return nil, errors.New("unexpected subject")
		}
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	queryData := []byte(`{"start_entity":"test.entity.001","max_depth":10}`)

	response, err := comp.handlePathSearch(ctx, queryData)

	// Should handle cycles without infinite loop
	assert.NoError(t, err)
	assert.NotNil(t, response)

	// Parse response
	var result map[string]interface{}
	err = json.Unmarshal(response, &result)
	require.NoError(t, err)

	// Should visit each entity only once (no duplicates)
	entities, ok := result["entities"].([]interface{})
	assert.True(t, ok)
	assert.LessOrEqual(t, len(entities), 3, "should not revisit entities in cycle")
}

// ====================================================================================
// Error Handling Tests
// ====================================================================================

func TestComponent_RespectsContext_Cancellation(t *testing.T) {
	mockClient := newMockNATSClient()

	comp := createTestComponentWithMockClient(t, mockClient)
	ctx, cancel := context.WithCancel(context.Background())

	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(ctx))

	// Cancel context
	cancel()

	// Allow time for cancellation to propagate
	time.Sleep(100 * time.Millisecond)

	// Component should handle cancellation gracefully
	err := comp.Stop(1 * time.Second)
	assert.NoError(t, err)
}

func TestComponent_RespectsContext_Timeout(t *testing.T) {
	mockClient := newMockNATSClient()

	comp := createTestComponentWithMockClient(t, mockClient)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	require.NoError(t, comp.Initialize())

	// Start with timeout context
	err := comp.Start(ctx)

	// Should either succeed or handle timeout gracefully
	if err != nil {
		// If error, it should be context-related
		assert.Contains(t, err.Error(), "context")
	}
}

// ====================================================================================
// Helper Functions
// ====================================================================================

// createTestComponent creates a basic test component with mock NATS client
func createTestComponent(t *testing.T) *Component {
	t.Helper()

	mockClient := newMockNATSClient()
	return createTestComponentWithMockClient(t, mockClient)
}

// createTestComponentWithMockClient creates a test component with provided mock client
func createTestComponentWithMockClient(t *testing.T, mockClient *mockNATSClient) *Component {
	t.Helper()

	config := DefaultConfig()
	config.ApplyDefaults()

	// Use fast startup settings for tests - 1 attempt with minimal delay
	// This prevents 5s+ waits per Start() when testing without real NATS
	config.StartupAttempts = 1
	config.StartupInterval = time.Millisecond
	config.RecheckInterval = time.Hour // Disable background recheck in tests

	// Construct directly, bypassing CreateGraphQuery to inject mock
	return &Component{
		natsClient:       mockClient, // Interface field accepts mock
		config:           config,
		logger:           slog.Default(),
		lastMetricsReset: time.Now(),
	}
}

// ====================================================================================
// ADR-009: PathRAG Direction Control Tests
// ====================================================================================

func TestComponent_PathSearch_DirectionIncoming(t *testing.T) {
	mockClient := newMockNATSClient()

	// Track which subjects are queried
	queriedSubjects := make(map[string]int)

	mockClient.requestFunc = func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		queriedSubjects[subject]++

		switch subject {
		case "graph.ingest.query.entity":
			return []byte(`{"id":"test.entity.002","triples":[]}`), nil

		case "graph.index.query.incoming":
			// Return incoming relationships (entities pointing TO this one)
			var req map[string]string
			json.Unmarshal(data, &req)

			if req["entity_id"] == "test.entity.002" {
				// Entity 001 points to 002
				return []byte(`{"data":{"relationships":[{"from_entity_id":"test.entity.001","predicate":"relates_to"}]},"timestamp":"2026-01-09T00:00:00Z"}`), nil
			}
			return []byte(`{"data":{"relationships":[]},"timestamp":"2026-01-09T00:00:00Z"}`), nil

		case "graph.index.query.outgoing":
			// Should NOT be called when direction is "incoming"
			t.Error("outgoing index should not be queried when direction is incoming")
			return []byte(`{"data":{"relationships":[]},"timestamp":"2026-01-09T00:00:00Z"}`), nil

		default:
			return nil, errors.New("unexpected subject: " + subject)
		}
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	queryData := []byte(`{"start_entity":"test.entity.002","max_depth":2,"direction":"incoming"}`)

	response, err := comp.handlePathSearch(ctx, queryData)

	assert.NoError(t, err)
	assert.NotNil(t, response)

	// Verify incoming index was queried
	assert.Greater(t, queriedSubjects["graph.index.query.incoming"], 0, "should query incoming index")

	// Verify response contains the incoming entity
	var result PathSearchResponse
	err = json.Unmarshal(response, &result)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Entities), 1, "should find at least start entity")
}

func TestComponent_PathSearch_DirectionBoth(t *testing.T) {
	mockClient := newMockNATSClient()

	queriedSubjects := make(map[string]int)

	mockClient.requestFunc = func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		queriedSubjects[subject]++

		switch subject {
		case "graph.ingest.query.entity":
			return []byte(`{"id":"test.entity.002","triples":[]}`), nil

		case "graph.index.query.outgoing":
			var req map[string]string
			json.Unmarshal(data, &req)
			if req["entity_id"] == "test.entity.002" {
				return []byte(`{"data":{"relationships":[{"to_entity_id":"test.entity.003","predicate":"relates_to"}]},"timestamp":"2026-01-09T00:00:00Z"}`), nil
			}
			return []byte(`{"data":{"relationships":[]},"timestamp":"2026-01-09T00:00:00Z"}`), nil

		case "graph.index.query.incoming":
			var req map[string]string
			json.Unmarshal(data, &req)
			if req["entity_id"] == "test.entity.002" {
				return []byte(`{"data":{"relationships":[{"from_entity_id":"test.entity.001","predicate":"relates_to"}]},"timestamp":"2026-01-09T00:00:00Z"}`), nil
			}
			return []byte(`{"data":{"relationships":[]},"timestamp":"2026-01-09T00:00:00Z"}`), nil

		default:
			return nil, errors.New("unexpected subject: " + subject)
		}
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	queryData := []byte(`{"start_entity":"test.entity.002","max_depth":2,"direction":"both"}`)

	response, err := comp.handlePathSearch(ctx, queryData)

	assert.NoError(t, err)
	assert.NotNil(t, response)

	// Verify both indexes were queried
	assert.Greater(t, queriedSubjects["graph.index.query.outgoing"], 0, "should query outgoing index")
	assert.Greater(t, queriedSubjects["graph.index.query.incoming"], 0, "should query incoming index")

	// Verify response contains entities from both directions
	var result PathSearchResponse
	err = json.Unmarshal(response, &result)
	require.NoError(t, err)
	// Should have: start entity + outgoing neighbor + incoming neighbor = 3
	assert.GreaterOrEqual(t, len(result.Entities), 2, "should find entities from both directions")
}

// ====================================================================================
// ADR-009: PathRAG Predicate Filtering Tests
// ====================================================================================

func TestComponent_PathSearch_PredicateFilter_Single(t *testing.T) {
	mockClient := newMockNATSClient()

	mockClient.requestFunc = func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.ingest.query.entity":
			return []byte(`{"id":"test.entity.001","triples":[]}`), nil

		case "graph.index.query.outgoing":
			// Return multiple relationships with different predicates
			var req map[string]string
			json.Unmarshal(data, &req)
			if req["entity_id"] == "test.entity.001" {
				return []byte(`{"data":{"relationships":[
					{"to_entity_id":"test.entity.002","predicate":"member_of"},
					{"to_entity_id":"test.entity.003","predicate":"relates_to"},
					{"to_entity_id":"test.entity.004","predicate":"member_of"}
				]},"timestamp":"2026-01-09T00:00:00Z"}`), nil
			}
			return []byte(`{"data":{"relationships":[]},"timestamp":"2026-01-09T00:00:00Z"}`), nil

		default:
			return nil, errors.New("unexpected subject: " + subject)
		}
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	// Filter to only "member_of" predicate
	queryData := []byte(`{"start_entity":"test.entity.001","max_depth":2,"predicates":["member_of"]}`)

	response, err := comp.handlePathSearch(ctx, queryData)

	assert.NoError(t, err)
	assert.NotNil(t, response)

	var result PathSearchResponse
	err = json.Unmarshal(response, &result)
	require.NoError(t, err)

	// Should have start + 2 member_of targets (002, 004), NOT 003 (relates_to)
	// Find entity IDs
	entityIDs := make(map[string]bool)
	for _, e := range result.Entities {
		entityIDs[e.ID] = true
	}

	assert.True(t, entityIDs["test.entity.001"], "should include start entity")
	assert.True(t, entityIDs["test.entity.002"], "should include member_of target 002")
	assert.True(t, entityIDs["test.entity.004"], "should include member_of target 004")
	assert.False(t, entityIDs["test.entity.003"], "should NOT include relates_to target 003")
}

func TestComponent_PathSearch_PredicateFilter_NoMatch(t *testing.T) {
	mockClient := newMockNATSClient()

	mockClient.requestFunc = func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.ingest.query.entity":
			return []byte(`{"id":"test.entity.001","triples":[]}`), nil

		case "graph.index.query.outgoing":
			// Return relationships that don't match filter
			var req map[string]string
			json.Unmarshal(data, &req)
			if req["entity_id"] == "test.entity.001" {
				return []byte(`{"data":{"relationships":[
					{"to_entity_id":"test.entity.002","predicate":"relates_to"}
				]},"timestamp":"2026-01-09T00:00:00Z"}`), nil
			}
			return []byte(`{"data":{"relationships":[]},"timestamp":"2026-01-09T00:00:00Z"}`), nil

		default:
			return nil, errors.New("unexpected subject: " + subject)
		}
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	// Filter to predicate that doesn't exist
	queryData := []byte(`{"start_entity":"test.entity.001","max_depth":2,"predicates":["nonexistent_pred"]}`)

	response, err := comp.handlePathSearch(ctx, queryData)

	assert.NoError(t, err)
	assert.NotNil(t, response)

	var result PathSearchResponse
	err = json.Unmarshal(response, &result)
	require.NoError(t, err)

	// Should only have start entity (no traversal matches)
	assert.Equal(t, 1, len(result.Entities), "should only have start entity when no predicates match")
}

// ====================================================================================
// ADR-009: PathRAG MaxPaths Limit Test
// ====================================================================================

func TestComponent_PathSearch_MaxPathsLimit(t *testing.T) {
	mockClient := newMockNATSClient()

	mockClient.requestFunc = func(_ context.Context, subject string, data []byte, _ time.Duration) ([]byte, error) {
		switch subject {
		case "graph.ingest.query.entity":
			return []byte(`{"id":"test.entity.001","triples":[]}`), nil

		case "graph.index.query.outgoing":
			// Return many relationships
			var req map[string]string
			json.Unmarshal(data, &req)
			if req["entity_id"] == "test.entity.001" {
				return []byte(`{"data":{"relationships":[
					{"to_entity_id":"test.entity.002","predicate":"r"},
					{"to_entity_id":"test.entity.003","predicate":"r"},
					{"to_entity_id":"test.entity.004","predicate":"r"},
					{"to_entity_id":"test.entity.005","predicate":"r"},
					{"to_entity_id":"test.entity.006","predicate":"r"}
				]},"timestamp":"2026-01-09T00:00:00Z"}`), nil
			}
			return []byte(`{"data":{"relationships":[]},"timestamp":"2026-01-09T00:00:00Z"}`), nil

		default:
			return nil, errors.New("unexpected subject: " + subject)
		}
	}

	comp := createTestComponentWithMockClient(t, mockClient)
	require.NoError(t, comp.Initialize())
	require.NoError(t, comp.Start(context.Background()))
	defer comp.Stop(1 * time.Second)

	ctx := context.Background()
	// Limit to 3 paths (start + 2 more)
	queryData := []byte(`{"start_entity":"test.entity.001","max_depth":2,"max_paths":3}`)

	response, err := comp.handlePathSearch(ctx, queryData)

	assert.NoError(t, err)
	assert.NotNil(t, response)

	var result PathSearchResponse
	err = json.Unmarshal(response, &result)
	require.NoError(t, err)

	// Should be truncated at max_paths
	assert.LessOrEqual(t, len(result.Entities), 3, "should respect max_paths limit")
	assert.True(t, result.Truncated, "should be marked as truncated")
}

// ====================================================================================
// ADR-009: GraphRAG Helper Function Tests
// ====================================================================================

func TestExtractRelationships_BothEndsPresent(t *testing.T) {
	comp := createTestComponent(t)

	// Use valid 6-part entity IDs (IsRelationship() requires this format)
	entity001 := "org.platform.domain.system.type.001"
	entity002 := "org.platform.domain.system.type.002"

	// Create entities with relationships
	entities := []*gtypes.EntityState{
		{
			ID: entity001,
			Triples: []message.Triple{
				{Subject: entity001, Predicate: "relates_to", Object: entity002},
				{Subject: entity001, Predicate: "name", Object: "Test Entity"}, // property, not relationship
			},
		},
		{
			ID: entity002,
			Triples: []message.Triple{
				{Subject: entity002, Predicate: "belongs_to", Object: entity001},
			},
		},
	}

	relationships := comp.extractRelationships(context.Background(), entities)

	// Should extract relationships where both ends are in entity set
	assert.Equal(t, 2, len(relationships), "should extract 2 relationships")

	// Verify relationship content
	relMap := make(map[string]Relationship)
	for _, r := range relationships {
		relMap[r.FromEntityID+":"+r.ToEntityID] = r
	}

	assert.Contains(t, relMap, entity001+":"+entity002)
	assert.Contains(t, relMap, entity002+":"+entity001)
}

func TestExtractRelationships_OneEndMissing(t *testing.T) {
	comp := createTestComponent(t)

	entity001 := "org.platform.domain.system.type.001"
	entity999 := "org.platform.domain.system.type.999" // valid format but not in result set

	// Create entity with relationship to entity NOT in result set
	entities := []*gtypes.EntityState{
		{
			ID: entity001,
			Triples: []message.Triple{
				{Subject: entity001, Predicate: "relates_to", Object: entity999}, // 999 not in set
			},
		},
	}

	relationships := comp.extractRelationships(context.Background(), entities)

	// Should NOT extract relationships where target is missing
	assert.Equal(t, 0, len(relationships), "should not extract relationship when target missing")
}

func TestBuildSources_SemanticScores(t *testing.T) {
	comp := createTestComponent(t)

	entities := []*gtypes.EntityState{
		{ID: "entity.001"},
		{ID: "entity.002"},
	}

	semanticHits := []SemanticHit{
		{EntityID: "entity.001", Score: 0.95},
		{EntityID: "entity.002", Score: 0.85},
	}

	sources := comp.buildSources(entities, semanticHits, nil)

	assert.Equal(t, 2, len(sources))

	// Should use semantic scores when available
	sourceMap := make(map[string]Source)
	for _, s := range sources {
		sourceMap[s.EntityID] = s
	}

	assert.InDelta(t, 0.95, sourceMap["entity.001"].Relevance, 0.01)
	assert.InDelta(t, 0.85, sourceMap["entity.002"].Relevance, 0.01)
}

func TestBuildSources_PositionBased(t *testing.T) {
	comp := createTestComponent(t)

	entities := []*gtypes.EntityState{
		{ID: "entity.001"},
		{ID: "entity.002"},
		{ID: "entity.003"},
	}

	// No semantic hits - should fall back to position-based scoring
	sources := comp.buildSources(entities, nil, nil)

	assert.Equal(t, 3, len(sources))

	// First entity should have highest relevance (position-based)
	// Sorted by relevance descending
	assert.Equal(t, "entity.001", sources[0].EntityID)
	assert.Greater(t, sources[0].Relevance, sources[1].Relevance)
	assert.Greater(t, sources[1].Relevance, sources[2].Relevance)
}

func TestGlobalSearchRequest_ShouldIncludeSummaries_Default(t *testing.T) {
	// Default (nil) should return true for backward compatibility
	req := GlobalSearchRequest{}
	assert.True(t, req.shouldIncludeSummaries(), "should default to true")
}

func TestGlobalSearchRequest_ShouldIncludeSummaries_ExplicitFalse(t *testing.T) {
	falseVal := false
	req := GlobalSearchRequest{IncludeSummaries: &falseVal}
	assert.False(t, req.shouldIncludeSummaries(), "should return false when explicitly set")
}

func TestGlobalSearchRequest_ShouldIncludeSummaries_ExplicitTrue(t *testing.T) {
	trueVal := true
	req := GlobalSearchRequest{IncludeSummaries: &trueVal}
	assert.True(t, req.shouldIncludeSummaries(), "should return true when explicitly set")
}
