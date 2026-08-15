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
)

// setupTestNATS starts canonical JetStream-backed test infrastructure.
func setupTestNATS(t *testing.T) (*natsclient.Client, func()) {
	t.Helper()

	testClient := natsclient.NewTestClient(t, natsclient.WithJetStream())
	// NewTestClient owns and reports cleanup through t.Cleanup. The retained
	// callback keeps the package helper contract used by sibling integration
	// files without adding a second termination path.
	return testClient.Client, func() {}
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
	err = graphQuery.Stop(context.Background())
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
	defer graphQuery.Stop(context.Background())

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
	defer graphQuery.Stop(context.Background())

	// Test PathSearch request structure
	req := PathSearchRequest{
		StartEntity: "test.fixture.graph.query.entity.001",
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
// - Race conditions while publishing the first community generation
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
	config.RecheckInterval = 100 * time.Millisecond // Fast recheck for test
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := CreateGraphQuery(configJSON, deps)
	require.NoError(t, err)

	graphQuery := comp.(*Component)
	published := make(chan uint64, 1)
	graphQuery.communityPublished = func(generation uint64) { published <- generation }
	require.NoError(t, graphQuery.Initialize())

	// Start component - COMMUNITY_INDEX doesn't exist yet
	require.NoError(t, graphQuery.Start(ctx))
	defer graphQuery.Stop(context.Background())

	// Verify GraphRAG is disabled initially (community cache should not be ready)
	assert.Nil(t, graphQuery.communityCache.acquire(),
		"community cache should not be ready when bucket doesn't exist")

	// Create COMMUNITY_INDEX bucket (simulating graph-clustering starting)
	_, err = js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket:      "COMMUNITY_INDEX",
		Description: "Test community index",
	})
	require.NoError(t, err, "should create COMMUNITY_INDEX bucket")

	select {
	case <-published:
	case <-time.After(2 * time.Second):
		t.Fatal("community generation was not published after bucket creation")
	}
	require.NotNil(t, graphQuery.communityCache.acquire())

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

// TestIntegration_GraphRAGOrderlyCancellation proves a real WatchAll generation
// stops with the component without being classified as unexpected watch loss.
func TestIntegration_GraphRAGOrderlyCancellation(t *testing.T) {
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
	config.RecheckInterval = 100 * time.Millisecond
	configJSON, err := json.Marshal(config)
	require.NoError(t, err)

	deps := component.Dependencies{
		NATSClient: natsClient,
	}

	comp, err := CreateGraphQuery(configJSON, deps)
	require.NoError(t, err)

	graphQuery := comp.(*Component)
	published := make(chan uint64, 2)
	unpublished := make(chan uint64, 1)
	retryCalled := make(chan struct{}, 1)
	graphQuery.communityPublished = func(generation uint64) { published <- generation }
	graphQuery.communityUnpublished = func(generation uint64) { unpublished <- generation }
	graphQuery.waitCommunityRetry = func(context.Context, time.Duration) bool {
		retryCalled <- struct{}{}
		return false
	}
	require.NoError(t, graphQuery.Initialize())
	require.NoError(t, graphQuery.Start(ctx))

	var firstGeneration uint64
	select {
	case firstGeneration = <-published:
	case <-time.After(2 * time.Second):
		t.Fatal("initial community generation was not published")
	}

	// Verify global search works
	globalReq, _ := json.Marshal(GlobalSearchRequest{Query: "test", Level: 0, MaxCommunities: 5})
	resp, err := graphQuery.handleGlobalSearch(ctx, globalReq)
	require.NoError(t, err, "global search should work initially")
	require.NotNil(t, resp)

	require.NoError(t, graphQuery.Stop(context.Background()))
	select {
	case revokedGeneration := <-unpublished:
		require.Equal(t, firstGeneration, revokedGeneration)
	case <-time.After(time.Second):
		t.Fatal("orderly cancellation did not revoke the active generation")
	}
	require.Nil(t, graphQuery.communityCache.acquire())
	select {
	case <-retryCalled:
		t.Fatal("orderly cancellation was classified as retryable watcher loss")
	default:
	}
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

	// Write two communities with LLM summaries and RepEntities.
	// Keys use the production format {level}.{community_id} written by
	// clustering.NATSCommunityStorage.SaveCommunity — the cache derives the level
	// from the key, so a bare-ID fixture would not exercise the real wire.
	communities := []struct {
		id    string
		level int
		data  map[string]any
	}{
		{
			id:    "comm-0-abc",
			level: 0,
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
			id:    "comm-0-def",
			level: 0,
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
		_, err = communityBucket.Put(ctx, communityKVKey(c.level, c.id), data)
		require.NoError(t, err)
	}

	// Create and start component with template synthesizer (no LLM endpoint needed)
	config := DefaultConfig()
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
	defer graphQuery.Stop(context.Background())

	// Wait for community cache to be ready and populated
	require.Eventually(t, func() bool {
		lease := graphQuery.communityCache.acquire()
		if lease == nil {
			return false
		}
		allComms := lease.getAllCommunities()
		return len(allComms) >= 2
	}, 3*time.Second, 50*time.Millisecond,
		"community cache should have 2 communities")

	// Test findCommunitiesForEntities — the community cache should match
	// member entity IDs against known communities.
	matchedEntityIDs := []string{
		"acme.ops.robotics.gcs.drone.001",
		"acme.ops.logistics.warehouse.worker.w1",
	}
	queryCtx, _ := graphQuery.withCommunityRequestLease(ctx)
	commSummaries := graphQuery.findCommunitiesForEntities(queryCtx, matchedEntityIDs)
	assert.Len(t, commSummaries, 2, "should match 2 communities")

	for _, cs := range commSummaries {
		assert.NotEmpty(t, cs.Summary, "each community should have a summary")
		assert.Greater(t, cs.MemberCount, 0, "MemberCount should be set at construction")
	}

	// Test enrichCommunitySummaries — this calls resolveEntityLabels which
	// requires loadEntities (no graph-ingest running), but enrichment should
	// still populate MemberCount and gracefully handle label resolution failure.
	enriched, err := graphQuery.enrichCommunitySummaries(ctx, commSummaries, nil)
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
	_, err = communityBucket.Put(ctx, communityKVKey(0, "comm-0-test"), commData)
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
	config.RecheckInterval = 100 * time.Millisecond
	configJSON, _ := json.Marshal(config)

	comp, err := CreateGraphQuery(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)

	graphQuery := comp.(*Component)
	require.NoError(t, graphQuery.Initialize())
	require.NoError(t, graphQuery.Start(ctx))
	defer graphQuery.Stop(context.Background())

	// Wait for community cache
	require.Eventually(t, func() bool {
		lease := graphQuery.communityCache.acquire()
		return lease != nil && len(lease.getAllCommunities()) >= 1
	}, 3*time.Second, 50*time.Millisecond)

	// Test enrichGlobalResponse
	entityIDs := []string{"acme.ops.robotics.gcs.drone.001", "acme.ops.robotics.gcs.drone.002"}
	response := GlobalSearchResponse{
		Count: len(entityIDs),
	}
	queryCtx, _ := graphQuery.withCommunityRequestLease(ctx)
	require.NoError(t, graphQuery.enrichGlobalResponse(queryCtx, &response, "drone operations", entityIDs, nil))

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
	defer graphQuery.Stop(context.Background())

	// Verify static routing works for known query types
	subject := graphQuery.router.Route("entity")
	assert.Equal(t, "graph.ingest.query.entity", subject, "should route entity queries to graph-ingest")

	subject = graphQuery.router.Route("outgoing")
	assert.Equal(t, "graph.index.query.outgoing", subject, "should route outgoing queries to graph-index")

	// Verify unknown query types return empty string
	subject = graphQuery.router.Route("unknown")
	assert.Equal(t, "", subject, "should return empty string for unknown query type")
}

// TestIntegration_CommunityCacheCrossLevelCollision replays a real detection cycle
// against real NATS KV and asserts through the production GlobalSearch handler.
//
// graph-clustering writes every level, then prunes the previous partition's
// leftovers LAST (write-then-prune, ADR-085). Community IDs are seed entity IDs
// re-drawn from the same pool at every level, so the same ID lands at level 0 and
// level 1 of one run. A cache keyed by bare community ID collapsed its level-0
// index on that trailing delete, and globalSearchTextBased has no NATS fallback —
// the empty index reads as an authoritative "no communities in this graph".
func TestIntegration_CommunityCacheCrossLevelCollision(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	natsClient, cleanup := setupTestNATS(t)
	defer cleanup()

	js, err := natsClient.JetStream()
	require.NoError(t, err)

	communityBucket, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{
		Bucket: "COMMUNITY_INDEX",
	})
	require.NoError(t, err)

	// loadEntities must resolve or globalSearchTextBased errors before it can
	// report on the communities it selected.
	_, err = natsClient.SubscribeForRequests(ctx, "graph.ingest.query.batch",
		func(_ context.Context, _ []byte) ([]byte, error) {
			return json.Marshal(map[string]any{
				"entities": []map[string]any{
					{
						"id": entDrone1,
						"triples": []map[string]any{
							{"subject": entDrone1, "predicate": "dc.terms.title", "object": "Alpha Drone"},
						},
					},
				},
			})
		})
	require.NoError(t, err)

	config := DefaultConfig()
	config.RecheckInterval = 100 * time.Millisecond
	configJSON, _ := json.Marshal(config)

	comp, err := CreateGraphQuery(configJSON, component.Dependencies{NATSClient: natsClient})
	require.NoError(t, err)

	graphQuery := comp.(*Component)
	require.NoError(t, graphQuery.Initialize())
	require.NoError(t, graphQuery.Start(ctx))
	defer graphQuery.Stop(context.Background())

	require.Eventually(t, func() bool { return graphQuery.communityCache.acquire() != nil },
		5*time.Second, 50*time.Millisecond, "community cache must complete initial sync")

	putCommunity := func(level int, id string, members []string) {
		t.Helper()
		data, err := json.Marshal(map[string]any{
			"id":                  id,
			"level":               level,
			"members":             members,
			"statistical_summary": "Cluster of drone entities in the GCS system.",
			"keywords":            []string{"drone", "autonomous"},
			"rep_entities":        []string{members[0]},
		})
		require.NoError(t, err)
		_, err = communityBucket.Put(ctx, communityKVKey(level, id), data)
		require.NoError(t, err)
	}

	// Level-0 pass.
	putCommunity(0, entDrone1, []string{entDrone1, entDrone2})
	putCommunity(0, entDrone3, []string{entDrone3})
	require.Eventually(t, func() bool {
		lease := graphQuery.communityCache.acquire()
		return lease != nil && len(lease.getCommunitiesByLevel(0)) == 2
	},
		5*time.Second, 25*time.Millisecond, "both level-0 communities must reach the cache")

	// Level-1 pass re-uses entDrone1 as a community ID.
	putCommunity(1, entDrone1, []string{entDrone1, entDrone2, entDrone3})
	require.Eventually(t, func() bool {
		lease := graphQuery.communityCache.acquire()
		return lease != nil && len(lease.getCommunitiesByLevel(1)) == 1
	},
		5*time.Second, 25*time.Millisecond, "the level-1 community must reach the cache")

	// Prune of the previous partition lands last.
	require.NoError(t, communityBucket.Delete(ctx, communityKVKey(0, entDrone3)))
	require.Eventually(t, func() bool {
		lease := graphQuery.communityCache.acquire()
		return lease != nil && len(lease.getCommunitiesByLevel(0)) == 1
	},
		5*time.Second, 25*time.Millisecond,
		"level-0 index must settle at exactly the surviving community, not collapse to empty")

	// The production surface: GlobalSearch at level 0 must still find it.
	includeSummaries := true
	reqData, err := json.Marshal(GlobalSearchRequest{
		Query:            "drone",
		Level:            0,
		MaxCommunities:   5,
		IncludeSummaries: &includeSummaries,
	})
	require.NoError(t, err)

	respData, err := graphQuery.handleGlobalSearch(ctx, reqData)
	require.NoError(t, err)

	var resp GlobalSearchResponse
	require.NoError(t, json.Unmarshal(respData, &resp))
	require.Len(t, resp.CommunitySummaries, 1,
		"GlobalSearch(level=0) must report the surviving level-0 community")
	assert.Equal(t, entDrone1, resp.CommunitySummaries[0].CommunityID)
	assert.Equal(t, 0, resp.CommunitySummaries[0].Level)
	assert.Equal(t, 2, resp.CommunitySummaries[0].MemberCount,
		"MemberCount must come from the level-0 record, not the 3-member level-1 record")

	// The SAME query at level 1 must resolve the OTHER community sharing this ID.
	//
	// This half is what makes the assertions above non-vacuous. Every summary-building
	// site stamps Level off the cache record, and enrichCommunitySummaries then re-reads
	// the community with GetCommunity(summary.Level, summary.CommunityID). A dropped or
	// zero-valued Level therefore resolves silently to level 0 — i.e. exactly the
	// pre-fix behavior — and a level-0-only test cannot tell the difference, because
	// Level 0 is also the Go zero value. MemberCount is the discriminator: 2 members at
	// level 0, 3 at level 1.
	reqL1, err := json.Marshal(GlobalSearchRequest{
		Query:            "drone",
		Level:            1,
		MaxCommunities:   5,
		IncludeSummaries: &includeSummaries,
	})
	require.NoError(t, err)

	respL1Data, err := graphQuery.handleGlobalSearch(ctx, reqL1)
	require.NoError(t, err)

	var respL1 GlobalSearchResponse
	require.NoError(t, json.Unmarshal(respL1Data, &respL1))
	require.Len(t, respL1.CommunitySummaries, 1,
		"GlobalSearch(level=1) must report the level-1 community")
	assert.Equal(t, entDrone1, respL1.CommunitySummaries[0].CommunityID)
	assert.Equal(t, 1, respL1.CommunitySummaries[0].Level,
		"the summary must carry the level it was built from; a zero here means the "+
			"Level stamp was lost and every downstream lookup silently used level 0")
	assert.Equal(t, 3, respL1.CommunitySummaries[0].MemberCount,
		"MemberCount must come from the 3-member level-1 record, not the 2-member "+
			"level-0 record sharing this community ID")
}
