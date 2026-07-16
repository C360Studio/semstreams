//go:build integration

package graphquery

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// setupTestNATS starts a NATS server using testcontainers
func setupTestNATS(t *testing.T) (*natsclient.Client, func()) {
	t.Helper()

	ctx := context.Background()

	// Start NATS container with JetStream.
	// Wait strategy mirrors natsclient/test_client.go: combined port + HTTP
	// health on the monitoring port with explicit startup timeout. Bare
	// ForListeningPort (without timeout) flakes under Docker API pressure.
	req := testcontainers.ContainerRequest{
		Image:        "nats:2.12-alpine",
		ExposedPorts: []string{"4222/tcp", "8222/tcp"},
		Cmd:          []string{"-js", "-m", "8222"}, // Enable JetStream + monitoring
		WaitingFor: wait.ForAll(
			wait.ForListeningPort("4222/tcp"),
			wait.ForHTTP("/").WithPort("8222/tcp").WithStartupTimeout(2*time.Minute),
		),
	}

	natsContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err)

	// Get connection URL
	host, err := natsContainer.Host(ctx)
	require.NoError(t, err)

	mappedPort, err := natsContainer.MappedPort(ctx, "4222")
	require.NoError(t, err)

	natsURL := "nats://" + host + ":" + mappedPort.Port()

	// Create NATS client
	client, err := natsclient.NewClient(natsURL,
		natsclient.WithMaxReconnects(-1),
		natsclient.WithReconnectWait(1*time.Second),
	)
	require.NoError(t, err)

	// Connect
	err = client.Connect(ctx)
	require.NoError(t, err)

	cleanup := func() {
		client.Close(ctx)
		_ = natsContainer.Terminate(ctx)
	}

	return client, cleanup
}

func TestIntegration_ComponentLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	// Create component
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := CreateGraphQuery(configJSON, deps)
	require.NoError(t, err)
	require.NotNil(t, comp)

	// Type assert to access component-specific fields
	graphQuery, ok := comp.(*Component)
	require.True(t, ok, "component should be *Component type")

	// Test lifecycle
	ctx := context.Background()

	// Initialize
	err = graphQuery.Initialize()
	assert.NoError(t, err)

	// Start
	err = graphQuery.Start(ctx)
	assert.NoError(t, err)

	// Check health
	health := graphQuery.Health()
	assert.True(t, health.Healthy, "component should be healthy after start")

	// Stop
	err = graphQuery.Stop(5 * time.Second)
	assert.NoError(t, err)
}

func TestIntegration_ComponentDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	// Create component
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := CreateGraphQuery(configJSON, deps)
	require.NoError(t, err)

	// Test Discoverable interface methods
	meta := comp.Meta()
	assert.Equal(t, "processor", meta.Type)
	assert.Equal(t, "graph-query", meta.Name)

	inputPorts := comp.InputPorts()
	assert.NotEmpty(t, inputPorts, "should have input ports")

	outputPorts := comp.OutputPorts()
	assert.Empty(t, outputPorts, "query coordinator should have no output ports")

	schema := comp.ConfigSchema()
	assert.NotNil(t, schema.Properties)
}

// TestIntegration_QueryCapabilities_GracefulDegradation was removed because
// the handleQueryCapabilities method no longer exists after refactoring to
// static routing. The test is no longer relevant to the current implementation.

func TestIntegration_ConfigValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	tests := []struct {
		name        string
		config      Config
		expectError bool
	}{
		{
			name:        "valid config",
			config:      DefaultConfig(),
			expectError: false,
		},
		{
			name: "missing ports",
			config: Config{
				Ports: nil,
			},
			expectError: true,
		},
		{
			name: "empty inputs",
			config: Config{
				Ports: &component.PortConfig{
					Inputs:  []component.PortDefinition{},
					Outputs: []component.PortDefinition{},
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configJSON, err := json.Marshal(tt.config)
			require.NoError(t, err)

			deps := component.Dependencies{
				NATSClient: natsClient,
			}

			comp, err := CreateGraphQuery(configJSON, deps)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, comp)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, comp)
			}
		})
	}
}

func TestIntegration_MetricsTracking(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	// Create and start component
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := CreateGraphQuery(configJSON, deps)
	require.NoError(t, err)

	graphQuery := comp.(*Component)
	require.NoError(t, graphQuery.Initialize())

	ctx := context.Background()
	require.NoError(t, graphQuery.Start(ctx))
	defer graphQuery.Stop(1 * time.Second)

	// Get initial metrics
	metrics := graphQuery.DataFlow()
	assert.GreaterOrEqual(t, metrics.MessagesPerSecond, float64(0))
	assert.GreaterOrEqual(t, metrics.BytesPerSecond, float64(0))
	assert.GreaterOrEqual(t, metrics.ErrorRate, float64(0))
}

func TestIntegration_PathSearch_Structure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	// Create and start component
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := CreateGraphQuery(configJSON, deps)
	require.NoError(t, err)

	graphQuery := comp.(*Component)
	require.NoError(t, graphQuery.Initialize())

	ctx := context.Background()
	require.NoError(t, graphQuery.Start(ctx))
	defer graphQuery.Stop(1 * time.Second)

	// Test PathSearch request structure
	req := PathSearchRequest{
		StartEntity: "test.entity.001",
		MaxDepth:    3,
	}

	reqData, err := json.Marshal(req)
	require.NoError(t, err)

	// This will fail because graph-ingest isn't running,
	// but it validates request parsing and structure
	response, err := graphQuery.handlePathSearch(ctx, reqData)

	// Should error (entity not found) but not panic
	assert.Error(t, err)
	assert.Nil(t, response)
	assert.Contains(t, err.Error(), "entity")
}

// TestIntegration_StaticRouting verifies that static routing works correctly.
// This test replaces the previous dynamic discovery tests since we now use
// static routing based on query type strings.
// TestIntegration_GraphRAGLifecycle tests that GraphRAG handlers become available
// when the COMMUNITY_INDEX bucket is created after component startup.
// This catches issues like:
// - Handlers never registered after bucket appears
// - Recheck interval too long
// - OnAvailable callback not firing
// - Race conditions in enableGraphRAG()
func TestIntegration_GraphRAGLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	// Get JetStream for bucket operations
	js, err := natsClient.JetStream()
	require.NoError(t, err)

	// Ensure COMMUNITY_INDEX bucket does NOT exist initially
	_ = js.DeleteKeyValue(ctx, "COMMUNITY_INDEX")

	// Create component with short recheck interval for testing
	config := DefaultConfig()
	config.StartupAttempts = 1 // Fail fast on startup
	config.StartupInterval = 10 * time.Millisecond
	config.RecheckInterval = 100 * time.Millisecond // Fast recheck for test
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := CreateGraphQuery(configJSON, deps)
	require.NoError(t, err)

	graphQuery := comp.(*Component)
	require.NoError(t, graphQuery.Initialize())

	// Start component - COMMUNITY_INDEX doesn't exist yet
	require.NoError(t, graphQuery.Start(ctx))
	defer graphQuery.Stop(5 * time.Second)

	// Verify GraphRAG is disabled initially (community cache should not be ready)
	assert.False(t, graphQuery.communityCache.IsReady(),
		"community cache should not be ready when bucket doesn't exist")

	// Create COMMUNITY_INDEX bucket (simulating graph-clustering starting)
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      "COMMUNITY_INDEX",
		Description: "Test community index",
	})
	require.NoError(t, err, "should create COMMUNITY_INDEX bucket")

	// Wait for resource watcher to detect the bucket and enable GraphRAG
	// With 100ms recheck interval, should detect within 500ms
	require.Eventually(t, func() bool {
		// Check if GraphRAG handlers are registered by attempting a request
		// The request will fail (no communities) but shouldn't return "not subscribed" error
		reqData, _ := json.Marshal(GlobalSearchRequest{
			Query:          "test",
			Level:          0,
			MaxCommunities: 5,
		})

		// Try to call the handler directly - if GraphRAG is enabled, handler exists
		resp, err := graphQuery.handleGlobalSearch(ctx, reqData)
		// Accept: response returned (even empty) means handlers work
		return err == nil && resp != nil
	}, 2*time.Second, 50*time.Millisecond,
		"GraphRAG should become available within 2s after bucket creation")

	// Verify community cache watcher started (indicated by cache being ready after initial sync)
	// Note: Cache will be "ready" after receiving nil entry (initial state complete)
	require.Eventually(t, func() bool {
		return graphQuery.communityCache.IsReady()
	}, 2*time.Second, 50*time.Millisecond,
		"community cache should become ready after bucket is available")

	// Verify we can make GraphRAG requests (they return empty results, not errors)
	globalReq, _ := json.Marshal(GlobalSearchRequest{
		Query:          "test query",
		Level:          0,
		MaxCommunities: 5,
	})
	resp, err := graphQuery.handleGlobalSearch(ctx, globalReq)
	require.NoError(t, err, "global search should succeed (returning empty results)")
	require.NotNil(t, resp)

	var globalResp GlobalSearchResponse
	require.NoError(t, json.Unmarshal(resp, &globalResp))
	assert.Empty(t, globalResp.Entities, "should return empty entities (no communities exist)")
	assert.Empty(t, globalResp.CommunitySummaries, "should return empty summaries")
}

// TestIntegration_GraphRAGBucketRecovery tests that GraphRAG recovers when
// the COMMUNITY_INDEX bucket is deleted and recreated.
func TestIntegration_GraphRAGBucketRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	js, err := natsClient.JetStream()
	require.NoError(t, err)

	// Create COMMUNITY_INDEX bucket first
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      "COMMUNITY_INDEX",
		Description: "Test community index",
	})
	require.NoError(t, err)

	// Create component with short intervals for testing
	config := DefaultConfig()
	config.StartupAttempts = 3
	config.StartupInterval = 50 * time.Millisecond
	config.RecheckInterval = 100 * time.Millisecond
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := CreateGraphQuery(configJSON, deps)
	require.NoError(t, err)

	graphQuery := comp.(*Component)
	require.NoError(t, graphQuery.Initialize())
	require.NoError(t, graphQuery.Start(ctx))
	defer graphQuery.Stop(5 * time.Second)

	// Wait for GraphRAG to be enabled (bucket exists at startup)
	require.Eventually(t, func() bool {
		return graphQuery.communityCache.IsReady()
	}, 2*time.Second, 50*time.Millisecond,
		"community cache should be ready when bucket exists at startup")

	// Verify global search works
	globalReq, _ := json.Marshal(GlobalSearchRequest{Query: "test", Level: 0, MaxCommunities: 5})
	resp, err := graphQuery.handleGlobalSearch(ctx, globalReq)
	require.NoError(t, err, "global search should work initially")
	require.NotNil(t, resp)

	// Delete the bucket (simulating graph-clustering crash/restart)
	err = js.DeleteKeyValue(ctx, "COMMUNITY_INDEX")
	require.NoError(t, err, "should delete COMMUNITY_INDEX bucket")

	// Give time for health check to detect loss (healthInterval defaults to 30s,
	// but our watcher will detect on next check cycle)
	time.Sleep(200 * time.Millisecond)

	// Recreate the bucket
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      "COMMUNITY_INDEX",
		Description: "Recreated community index",
	})
	require.NoError(t, err, "should recreate COMMUNITY_INDEX bucket")

	// Verify GraphRAG recovers
	require.Eventually(t, func() bool {
		resp, err := graphQuery.handleGlobalSearch(ctx, globalReq)
		return err == nil && resp != nil
	}, 3*time.Second, 100*time.Millisecond,
		"GraphRAG should recover after bucket is recreated")
}

// TestIntegration_AnswerSynthesis verifies that globalSearch produces an answer
// and enriched community summaries when communities with LLM summaries exist.
// Tests the full path: community cache → enrichment → answer synthesis.
func TestIntegration_AnswerSynthesis(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	js, err := natsClient.JetStream()
	require.NoError(t, err)

	// Create COMMUNITY_INDEX bucket with test communities
	communityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      "COMMUNITY_INDEX",
		Description: "Test community index",
	})
	require.NoError(t, err)

	// Write two communities with LLM summaries and RepEntities
	communities := []struct {
		id   string
		data map[string]any
	}{
		{
			id: "comm-0-abc",
			data: map[string]any{
				"id":                  "comm-0-abc",
				"level":               0,
				"members":             []string{"acme.ops.robotics.gcs.drone.001", "acme.ops.robotics.gcs.drone.002", "acme.ops.robotics.gcs.sensor.temp-001"},
				"statistical_summary": "Cluster of drone and sensor entities in the GCS system.",
				"llm_summary":         "This community contains autonomous drones and temperature sensors operating within the ground control station.",
				"keywords":            []string{"drone", "sensor", "autonomous", "navigation"},
				"rep_entities":        []string{"acme.ops.robotics.gcs.drone.001"},
				"summary_status":      "llm-enhanced",
			},
		},
		{
			id: "comm-0-def",
			data: map[string]any{
				"id":                  "comm-0-def",
				"level":               0,
				"members":             []string{"acme.ops.logistics.warehouse.worker.w1", "acme.ops.logistics.warehouse.worker.w2"},
				"statistical_summary": "Warehouse worker entities in logistics.",
				"llm_summary":         "This community represents warehouse workers assigned to logistics operations.",
				"keywords":            []string{"warehouse", "logistics", "worker"},
				"rep_entities":        []string{"acme.ops.logistics.warehouse.worker.w1"},
				"summary_status":      "llm-enhanced",
			},
		},
	}

	for _, c := range communities {
		data, err := json.Marshal(c.data)
		require.NoError(t, err)
		_, err = communityBucket.Put(ctx, c.id, data)
		require.NoError(t, err)
	}

	// Create and start component with template synthesizer (no LLM endpoint needed)
	config := DefaultConfig()
	config.StartupAttempts = 3
	config.StartupInterval = 50 * time.Millisecond
	config.RecheckInterval = 100 * time.Millisecond
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := CreateGraphQuery(configJSON, deps)
	require.NoError(t, err)

	graphQuery := comp.(*Component)
	require.NoError(t, graphQuery.Initialize())
	require.NoError(t, graphQuery.Start(ctx))
	defer graphQuery.Stop(5 * time.Second)

	// Wait for community cache to be ready and populated
	require.Eventually(t, func() bool {
		if !graphQuery.communityCache.IsReady() {
			return false
		}
		allComms := graphQuery.communityCache.GetAllCommunities()
		return len(allComms) >= 2
	}, 3*time.Second, 50*time.Millisecond,
		"community cache should have 2 communities")

	// Test findCommunitiesForEntities — the community cache should match
	// member entity IDs against known communities.
	matchedEntityIDs := []string{
		"acme.ops.robotics.gcs.drone.001",
		"acme.ops.logistics.warehouse.worker.w1",
	}
	commSummaries := graphQuery.findCommunitiesForEntities(matchedEntityIDs)
	assert.Len(t, commSummaries, 2, "should match 2 communities")

	for _, cs := range commSummaries {
		assert.NotEmpty(t, cs.Summary, "each community should have a summary")
		assert.Greater(t, cs.MemberCount, 0, "MemberCount should be set at construction")
	}

	// Test enrichCommunitySummaries — this calls resolveEntityLabels which
	// requires loadEntities (no graph-ingest running), but enrichment should
	// still populate MemberCount and gracefully handle label resolution failure.
	enriched, err := graphQuery.enrichCommunitySummaries(ctx, commSummaries)
	require.NoError(t, err)
	assert.Len(t, enriched, 2)
	for _, cs := range enriched {
		assert.Greater(t, cs.MemberCount, 0, "MemberCount should be populated after enrichment")
	}

	// Test synthesizeQueryAnswer — uses the template path (no LLM endpoint
	// configured for this integration test). Returns SynthesisOutcome
	// with Degraded=false because pure-template deployments don't lie
	// about being degraded — template IS canonical when no LLM was
	// ever configured (see beta.45 SynthesisOutcome doc).
	synth := graphQuery.synthesizeQueryAnswer(ctx, "drone sensor operations", enriched, 5)
	assert.NotEmpty(t, synth.Answer, "Answer should be populated from community summaries")
	assert.Contains(t, synth.Answer, "knowledge cluster", "Answer should contain cluster header")
	assert.Contains(t, synth.Answer, "5 entities", "Answer should mention entity count")
	assert.Empty(t, synth.Model, "Model should be empty for template path")
	assert.False(t, synth.Degraded, "Degraded should be false for pure-template (no LLM ever configured)")

	// Verify the answer includes community narrative content
	assert.True(t,
		assert.ObjectsAreEqual(true, len(synth.Answer) > 100),
		"Answer should be substantial (>100 chars), got %d chars", len(synth.Answer))
}

// TestIntegration_EnrichGlobalResponse verifies the enrichment pipeline end-to-end:
// communities in KV → enrichGlobalResponse → Answer + CommunitySummaries populated.
// Uses a mock NATS responder for graph.ingest.query.entities so loadEntities works.
func TestIntegration_EnrichGlobalResponse(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	js, err := natsClient.JetStream()
	require.NoError(t, err)

	// Create COMMUNITY_INDEX with test communities
	communityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: "COMMUNITY_INDEX",
	})
	require.NoError(t, err)

	commData, _ := json.Marshal(map[string]any{
		"id":             "comm-0-test",
		"level":          0,
		"members":        []string{"acme.ops.robotics.gcs.drone.001", "acme.ops.robotics.gcs.drone.002"},
		"llm_summary":    "Autonomous drones in the ground control station.",
		"keywords":       []string{"drone", "autonomous"},
		"rep_entities":   []string{"acme.ops.robotics.gcs.drone.001"},
		"summary_status": "llm-enhanced",
	})
	_, err = communityBucket.Put(ctx, "comm-0-test", commData)
	require.NoError(t, err)

	// Set up a mock responder for graph.ingest.query.entities
	// This enables loadEntities() to work in the test
	_, err = natsClient.SubscribeForRequests(ctx, "graph.ingest.query.batch",
		func(_ context.Context, _ []byte) ([]byte, error) {
			resp := map[string]any{
				"entities": []map[string]any{
					{
						"id": "acme.ops.robotics.gcs.drone.001",
						"triples": []map[string]any{
							{"subject": "acme.ops.robotics.gcs.drone.001", "predicate": "dc.terms.title", "object": "Alpha Drone"},
							{"subject": "acme.ops.robotics.gcs.drone.001", "predicate": "drone.state.status", "object": "active"},
						},
					},
				},
			}
			return json.Marshal(resp)
		})
	require.NoError(t, err)

	// Create and start component
	config := DefaultConfig()
	config.StartupAttempts = 3
	config.StartupInterval = 50 * time.Millisecond
	config.RecheckInterval = 100 * time.Millisecond
	configJSON, _ := json.Marshal(config)

	comp, err := CreateGraphQuery(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)

	graphQuery := comp.(*Component)
	require.NoError(t, graphQuery.Initialize())
	require.NoError(t, graphQuery.Start(ctx))
	defer graphQuery.Stop(5 * time.Second)

	// Wait for community cache
	require.Eventually(t, func() bool {
		return graphQuery.communityCache.IsReady() && len(graphQuery.communityCache.GetAllCommunities()) >= 1
	}, 3*time.Second, 50*time.Millisecond)

	// Test enrichGlobalResponse
	entityIDs := []string{"acme.ops.robotics.gcs.drone.001", "acme.ops.robotics.gcs.drone.002"}
	response := GlobalSearchResponse{
		Count: len(entityIDs),
	}
	require.NoError(t, graphQuery.enrichGlobalResponse(ctx, &response, "drone operations", entityIDs))

	assert.NotEmpty(t, response.CommunitySummaries, "CommunitySummaries should be populated")
	assert.NotEmpty(t, response.Answer, "Answer should be synthesized")

	// Verify community summaries have RepEntity digests with resolved labels
	if len(response.CommunitySummaries) > 0 {
		cs := response.CommunitySummaries[0]
		assert.Greater(t, cs.MemberCount, 0)
		if len(cs.Entities) > 0 {
			assert.Equal(t, "drone", cs.Entities[0].Type)
			assert.Equal(t, "Alpha Drone", cs.Entities[0].Label, "label should be resolved from dc.terms.title")
		}
	}

	// Test buildEntityDigestsFromEntities with loaded entities
	entities, loadErr := graphQuery.loadEntities(ctx, []string{"acme.ops.robotics.gcs.drone.001"})
	require.NoError(t, loadErr)
	require.Len(t, entities, 1)

	digests := buildEntityDigestsFromEntities(entities)
	require.Len(t, digests, 1)
	assert.Equal(t, "drone", digests[0].Type)
	assert.Equal(t, "Alpha Drone", digests[0].Label)
}

func TestIntegration_StaticRouting(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	// Create and start graph-query
	config := DefaultConfig()
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := CreateGraphQuery(configJSON, deps)
	require.NoError(t, err)

	graphQuery := comp.(*Component)
	require.NoError(t, graphQuery.Initialize())
	require.NoError(t, graphQuery.Start(ctx))
	defer graphQuery.Stop(1 * time.Second)

	// Verify static routing works for known query types
	subject := graphQuery.router.Route("entity")
	assert.Equal(t, "graph.ingest.query.entity", subject, "should route entity queries to graph-ingest")

	subject = graphQuery.router.Route("outgoing")
	assert.Equal(t, "graph.index.query.outgoing", subject, "should route outgoing queries to graph-index")

	// Verify unknown query types return empty string
	subject = graphQuery.router.Route("unknown")
	assert.Equal(t, "", subject, "should return empty string for unknown query type")
}
