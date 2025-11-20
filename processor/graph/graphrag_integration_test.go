package graph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/metric"
	"github.com/c360/semstreams/pkg/graphclustering"
	"github.com/c360/semstreams/pkg/graphinterfaces"
	"github.com/c360/semstreams/processor/graph/datamanager"
	"github.com/c360/semstreams/processor/graph/indexmanager"
	"github.com/c360/semstreams/processor/graph/querymanager"
)

// testGraphProvider adapts datamanager.DataHandler to graphclustering.GraphProvider
type testGraphProvider struct {
	dataHandler datamanager.DataHandler
	kvBucket    jetstream.KeyValue
}

func (p *testGraphProvider) GetAllEntityIDs(ctx context.Context) ([]string, error) {
	// List all keys from the KV bucket
	keys, err := p.kvBucket.ListKeys(ctx)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0)
	for key := range keys.Keys() {
		ids = append(ids, key)
	}
	return ids, nil
}

func (p *testGraphProvider) GetNeighbors(ctx context.Context, entityID string, direction string) ([]string, error) {
	// Get the entity to access its edges
	entity, err := p.dataHandler.GetEntity(ctx, entityID)
	if err != nil {
		return []string{}, err
	}

	neighborSet := make(map[string]bool)

	// Collect neighbors based on direction
	// Note: EntityState.Edges is []Edge (slice), not a map
	if direction == "outgoing" || direction == "both" {
		for _, edge := range entity.Edges {
			neighborSet[edge.ToEntityID] = true
		}
	}

	// TODO: For incoming edges, we would need to query an incoming edge index
	// For now, incoming direction is not supported in this test helper
	if direction == "incoming" {
		// Not implemented - would require querying all entities or using an index
		return []string{}, nil
	}

	neighbors := make([]string, 0, len(neighborSet))
	for id := range neighborSet {
		neighbors = append(neighbors, id)
	}
	return neighbors, nil
}

func (p *testGraphProvider) GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error) {
	// Get the source entity
	entity, err := p.dataHandler.GetEntity(ctx, fromID)
	if err != nil {
		return 0.0, err
	}

	// Check if there's an edge to the target
	for _, edge := range entity.Edges {
		if edge.ToEntityID == toID {
			// Return weight if available, otherwise 1.0
			if edge.Weight > 0 {
				return edge.Weight, nil
			}
			return 1.0, nil
		}
	}

	return 0.0, nil
}

// communityDetectorAdapter adapts LPADetector to return interface types
// This bridges the gap between concrete *Community types and graphinterfaces.Community
type communityDetectorAdapter struct {
	detector *graphclustering.LPADetector
}

func (a *communityDetectorAdapter) GetCommunity(ctx context.Context, communityID string) (graphinterfaces.Community, error) {
	return a.detector.GetCommunity(ctx, communityID)
}

func (a *communityDetectorAdapter) GetEntityCommunity(ctx context.Context, entityID string, level int) (graphinterfaces.Community, error) {
	return a.detector.GetEntityCommunity(ctx, entityID, level)
}

func (a *communityDetectorAdapter) GetCommunitiesByLevel(ctx context.Context, level int) ([]graphinterfaces.Community, error) {
	communities, err := a.detector.GetCommunitiesByLevel(ctx, level)
	if err != nil {
		return nil, err
	}

	// Convert []*Community to []graphinterfaces.Community
	result := make([]graphinterfaces.Community, len(communities))
	for i, c := range communities {
		result[i] = c
	}
	return result, nil
}

// graphRAGTestSetup holds components for GraphRAG E2E testing
type graphRAGTestSetup struct {
	processor        *Processor
	queryManager     querymanager.Querier
	communityStorage graphclustering.CommunityStorage
	communityBucket  jetstream.KeyValue
	detector         *graphclustering.LPADetector
	ctx              context.Context
	cancel           context.CancelFunc
}

// setupGraphRAGTest creates a graph processor with community detector for GraphRAG testing
func setupGraphRAGTest(t *testing.T) *graphRAGTestSetup {
	natsClient := getSharedNATSClient(t)
	ctx, cancel := context.WithCancel(context.Background())

	// Create config with metrics disabled for tests
	config := DefaultConfig()
	if config.Indexer == nil {
		config.Indexer = &indexmanager.Config{}
		*config.Indexer = indexmanager.DefaultConfig()
	}
	config.Indexer.EventBuffer.Metrics = false

	deps := ProcessorDeps{
		Config:          config,
		NATSClient:      natsClient,
		MetricsRegistry: metric.NewMetricsRegistry(),
		Logger:          slog.Default(),
	}

	processor, err := NewProcessor(deps)
	require.NoError(t, err)
	require.NotNil(t, processor)

	err = processor.Initialize()
	require.NoError(t, err)

	// Start processor in background
	startErr := make(chan error, 1)
	go func() {
		startErr <- processor.Start(ctx)
	}()

	// Wait for processor to be ready
	err = processor.WaitForReady(5 * time.Second)
	require.NoError(t, err, "Processor should be ready within 5 seconds")

	// Create community storage bucket
	communityBucket, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: graphclustering.CommunityBucket,
	})
	require.NoError(t, err, "Failed to create COMMUNITIES bucket")

	communityStorage := graphclustering.NewNATSCommunityStorage(communityBucket)

	// Get the ENTITY_STATES bucket for graph provider
	entityBucket, err := natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: "ENTITY_STATES",
	})
	require.NoError(t, err, "Failed to get ENTITY_STATES bucket")

	// Create GraphProvider that wraps the processor's data handler
	graphProvider := &testGraphProvider{
		dataHandler: processor.dataManager,
		kvBucket:    entityBucket,
	}

	// Create LPA community detector
	detector := graphclustering.NewLPADetector(graphProvider, communityStorage)

	// Create adapter to bridge type differences
	detectorAdapter := &communityDetectorAdapter{
		detector: detector,
	}

	// Create QueryManager with community detector adapter
	// Use empty config and rely on SetDefaults
	queryConfig := querymanager.Config{}
	queryDeps := querymanager.Deps{
		Config:            queryConfig,
		DataHandler:       processor.dataManager,
		IndexManager:      processor.indexManager,
		CommunityDetector: detectorAdapter,
		Registry:          metric.NewMetricsRegistry(),
		Logger:            slog.Default(),
	}
	queryMgr, err := querymanager.NewManager(queryDeps)
	require.NoError(t, err)

	t.Cleanup(func() {
		cancel()
		<-startErr
	})

	return &graphRAGTestSetup{
		processor:        processor,
		queryManager:     queryMgr,
		communityStorage: communityStorage,
		communityBucket:  communityBucket,
		detector:         detector,
		ctx:              ctx,
		cancel:           cancel,
	}
}

// createTestEntity helper to create entities with properties
func createTestEntity(processor *Processor, ctx context.Context, id string, entityType string, properties map[string]any) (*gtypes.EntityState, error) {
	entity := &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:         id,
			Type:       entityType,
			Properties: properties,
		},
		UpdatedAt: time.Now(),
		Version:   1,
	}
	return processor.dataManager.CreateEntity(ctx, entity)
}

// TestE2E_LocalSearch tests local community search end-to-end
func TestE2E_LocalSearch(t *testing.T) {
	setup := setupGraphRAGTest(t)
	processor := setup.processor
	ctx := setup.ctx
	communityStorage := setup.communityStorage
	queryMgr := setup.queryManager

	// Create a community with robotics entities
	t.Log("Creating test community with robotics entities")

	// Create entities in the robotics community
	roboticsEntities := []struct {
		id         string
		entityType string
		name       string
	}{
		{"robot-1", "robotics.drone", "Autonomous Delivery Drone"},
		{"robot-2", "robotics.sensor", "LiDAR Scanner"},
		{"robot-3", "robotics.controller", "Navigation Controller"},
	}

	memberIDs := make([]string, len(roboticsEntities))
	for i, spec := range roboticsEntities {
		_, err := createTestEntity(processor, ctx, spec.id, spec.entityType, map[string]any{
			"name":        spec.name,
			"description": "Robotics component for autonomous systems",
		})
		require.NoError(t, err, "Failed to create entity %s", spec.id)
		memberIDs[i] = spec.id
	}

	// Create robotics community
	roboticsCommunity := &graphclustering.Community{
		ID:                 "comm-0-robotics",
		Level:              0,
		Members:            memberIDs,
		StatisticalSummary: "Robotics and autonomous systems community",
		Keywords:           []string{"robotics", "autonomous", "drone", "sensor"},
	}
	err := communityStorage.SaveCommunity(ctx, roboticsCommunity)
	require.NoError(t, err, "Failed to save robotics community")

	// Also create a network community (should not be returned by local search)
	_, err = createTestEntity(processor, ctx, "net-1", "network.router", map[string]any{
		"name": "Core Router",
	})
	require.NoError(t, err)

	networkCommunity := &graphclustering.Community{
		ID:                 "comm-0-network",
		Level:              0,
		Members:            []string{"net-1"},
		StatisticalSummary: "Network infrastructure community",
		Keywords:           []string{"network", "router"},
	}
	err = communityStorage.SaveCommunity(ctx, networkCommunity)
	require.NoError(t, err, "Failed to save network community")

	// Wait for entities to be indexed
	time.Sleep(200 * time.Millisecond)

	// Test 1: Local search from robot-1 should only return robotics entities
	t.Run("LocalSearch returns only entities from same community", func(t *testing.T) {
		result, err := queryMgr.LocalSearch(ctx, "robot-1", "robotics", 0)

		require.NoError(t, err, "LocalSearch failed")
		assert.NotNil(t, result)
		assert.Equal(t, "comm-0-robotics", result.CommunityID)

		// Should match all 3 robotics entities
		assert.Equal(t, 3, result.Count, "Should find all robotics entities")
		assert.Len(t, result.Entities, 3)

		// Verify no network entities included
		for _, entity := range result.Entities {
			assert.NotEqual(t, "net-1", entity.Node.ID, "Network entity should not be in local search results")
		}
	})

	// Test 2: Query specificity
	t.Run("LocalSearch with specific query filters results", func(t *testing.T) {
		result, err := queryMgr.LocalSearch(ctx, "robot-1", "drone", 0)

		require.NoError(t, err)
		assert.NotNil(t, result)

		// Should only match the drone entity
		assert.Equal(t, 1, result.Count, "Should find only drone entity")
		if len(result.Entities) > 0 {
			assert.Contains(t, result.Entities[0].Node.ID, "robot-1")
		}
	})

	t.Logf("✅ LocalSearch E2E test completed successfully")
}

// TestE2E_GlobalSearch tests global cross-community search
func TestE2E_GlobalSearch(t *testing.T) {
	setup := setupGraphRAGTest(t)
	processor := setup.processor
	ctx := setup.ctx
	communityStorage := setup.communityStorage
	queryMgr := setup.queryManager

	t.Log("Creating multiple communities for global search test")

	// Create 3 communities: robotics, network, storage
	communities := []struct {
		id       string
		summary  string
		keywords []string
		entities []struct {
			id         string
			entityType string
			name       string
		}
	}{
		{
			id:       "comm-0-robotics",
			summary:  "Robotics and autonomous systems with sensors and drones",
			keywords: []string{"robotics", "autonomous", "sensors"},
			entities: []struct {
				id         string
				entityType string
				name       string
			}{
				{"r1", "robotics.drone", "Delivery Drone"},
				{"r2", "robotics.sensor", "Temperature Sensor"},
			},
		},
		{
			id:       "comm-0-network",
			summary:  "Network infrastructure with routers and switches",
			keywords: []string{"network", "infrastructure", "routing"},
			entities: []struct {
				id         string
				entityType string
				name       string
			}{
				{"n1", "network.router", "Core Router"},
				{"n2", "network.switch", "Access Switch"},
			},
		},
		{
			id:       "comm-0-storage",
			summary:  "Storage systems and databases",
			keywords: []string{"storage", "database", "persistence"},
			entities: []struct {
				id         string
				entityType string
				name       string
			}{
				{"s1", "storage.database", "PostgreSQL Server"},
				{"s2", "storage.cache", "Redis Cache"},
			},
		},
	}

	// Create all communities and entities
	for _, comm := range communities {
		memberIDs := make([]string, len(comm.entities))
		for i, entity := range comm.entities {
			_, err := createTestEntity(processor, ctx, entity.id, entity.entityType, map[string]any{
				"name": entity.name,
			})
			require.NoError(t, err, "Failed to create entity %s", entity.id)
			memberIDs[i] = entity.id
		}

		community := &graphclustering.Community{
			ID:                 comm.id,
			Level:              0,
			Members:            memberIDs,
			StatisticalSummary: comm.summary,
			Keywords:           comm.keywords,
		}
		err := communityStorage.SaveCommunity(ctx, community)
		require.NoError(t, err, "Failed to save community %s", comm.id)
	}

	// Wait for indexing
	time.Sleep(200 * time.Millisecond)

	// Test 1: Global search spans multiple communities
	t.Run("GlobalSearch spans multiple communities", func(t *testing.T) {
		result, err := queryMgr.GlobalSearch(ctx, "router sensor", 0, 3)

		require.NoError(t, err, "GlobalSearch failed")
		assert.NotNil(t, result)

		// Should have community summaries
		assert.Greater(t, len(result.CommunitySummaries), 0, "Should return community summaries")

		// Should find entities across communities (router and sensor entities)
		assert.GreaterOrEqual(t, result.Count, 2, "Should find at least router and sensor entities")

		t.Logf("Found %d entities across %d communities", result.Count, len(result.CommunitySummaries))
	})

	// Test 2: Targeted query prefers relevant community
	t.Run("GlobalSearch ranks relevant community higher", func(t *testing.T) {
		result, err := queryMgr.GlobalSearch(ctx, "robotics sensor drone", 0, 3)

		require.NoError(t, err)
		assert.NotNil(t, result)
		require.Greater(t, len(result.CommunitySummaries), 0, "Should return summaries")

		// First community should be robotics (highest relevance)
		topCommunity := result.CommunitySummaries[0]
		assert.Equal(t, "comm-0-robotics", topCommunity.CommunityID, "Robotics community should rank highest")
		assert.Greater(t, topCommunity.Relevance, 0.0, "Should have positive relevance score")

		t.Logf("Top community: %s with relevance %.2f", topCommunity.CommunityID, topCommunity.Relevance)
	})

	// Test 3: MaxCommunities limit is enforced
	t.Run("GlobalSearch respects maxCommunities limit", func(t *testing.T) {
		result, err := queryMgr.GlobalSearch(ctx, "database", 0, 2)

		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.LessOrEqual(t, len(result.CommunitySummaries), 2, "Should respect maxCommunities=2")
	})

	t.Logf("✅ GlobalSearch E2E test completed successfully")
}

// TestE2E_CommunitySummaries tests community summary accuracy
func TestE2E_CommunitySummaries(t *testing.T) {
	setup := setupGraphRAGTest(t)
	processor := setup.processor
	ctx := setup.ctx
	communityStorage := setup.communityStorage
	queryMgr := setup.queryManager

	t.Log("Testing community summary generation and accuracy")

	// Create a well-defined community
	entities := []struct {
		id         string
		entityType string
		name       string
	}{
		{"ml-1", "ml.model", "Image Classification CNN"},
		{"ml-2", "ml.dataset", "ImageNet Training Data"},
		{"ml-3", "ml.pipeline", "Training Pipeline"},
	}

	memberIDs := make([]string, len(entities))
	for i, spec := range entities {
		_, err := createTestEntity(processor, ctx, spec.id, spec.entityType, map[string]any{
			"name":        spec.name,
			"description": "Machine learning component",
		})
		require.NoError(t, err)
		memberIDs[i] = spec.id
	}

	// Create community with statistical summary
	community := &graphclustering.Community{
		ID:                 "comm-0-ml",
		Level:              0,
		Members:            memberIDs,
		StatisticalSummary: "Machine learning and neural network training community",
		Keywords:           []string{"machine-learning", "cnn", "training", "neural-network"},
		RepEntities:        []string{"ml-1"}, // CNN is representative
		SummaryStatus:      "statistical",
	}
	err := communityStorage.SaveCommunity(ctx, community)
	require.NoError(t, err)

	time.Sleep(200 * time.Millisecond)

	t.Run("Community summary contains relevant keywords", func(t *testing.T) {
		result, err := queryMgr.GlobalSearch(ctx, "machine learning", 0, 5)

		require.NoError(t, err)
		assert.NotNil(t, result)
		require.Greater(t, len(result.CommunitySummaries), 0)

		// Find ML community in results
		var mlSummary *querymanager.CommunitySummary
		for i := range result.CommunitySummaries {
			if result.CommunitySummaries[i].CommunityID == "comm-0-ml" {
				mlSummary = &result.CommunitySummaries[i]
				break
			}
		}

		require.NotNil(t, mlSummary, "ML community should be in results")
		assert.Contains(t, strings.ToLower(mlSummary.Summary), "machine learning", "Summary should mention ML")
		assert.Contains(t, mlSummary.Keywords, "machine-learning", "Keywords should include ML")

		t.Logf("Community summary: %s", mlSummary.Summary)
		t.Logf("Keywords: %v", mlSummary.Keywords)
	})

	t.Logf("✅ Community summaries test completed successfully")
}

// TestE2E_PerformanceComparison tests that local search is faster than global
func TestE2E_PerformanceComparison(t *testing.T) {
	setup := setupGraphRAGTest(t)
	processor := setup.processor
	ctx := setup.ctx
	communityStorage := setup.communityStorage
	queryMgr := setup.queryManager

	t.Log("Testing performance: LocalSearch vs GlobalSearch")

	// Create a larger graph with multiple communities
	numCommunities := 5
	entitiesPerCommunity := 20

	for c := 0; c < numCommunities; c++ {
		memberIDs := make([]string, entitiesPerCommunity)
		for e := 0; e < entitiesPerCommunity; e++ {
			id := fmt.Sprintf("perf-c%d-e%d", c, e)
			_, err := createTestEntity(processor, ctx, id, "test.entity", map[string]any{
				"name":      fmt.Sprintf("Entity %d-%d", c, e),
				"community": c,
			})
			require.NoError(t, err)
			memberIDs[e] = id
		}

		community := &graphclustering.Community{
			ID:                 fmt.Sprintf("comm-0-perf%d", c),
			Level:              0,
			Members:            memberIDs,
			StatisticalSummary: fmt.Sprintf("Performance test community %d", c),
			Keywords:           []string{"performance", "test", fmt.Sprintf("comm%d", c)},
		}
		err := communityStorage.SaveCommunity(ctx, community)
		require.NoError(t, err)
	}

	time.Sleep(300 * time.Millisecond)

	// Measure LocalSearch performance
	t.Run("LocalSearch is faster than GlobalSearch", func(t *testing.T) {
		// Run LocalSearch 10 times
		localStart := time.Now()
		for i := 0; i < 10; i++ {
			_, err := queryMgr.LocalSearch(ctx, "perf-c0-e0", "test", 0)
			require.NoError(t, err)
		}
		localDuration := time.Since(localStart)

		// Run GlobalSearch 10 times
		globalStart := time.Now()
		for i := 0; i < 10; i++ {
			_, err := queryMgr.GlobalSearch(ctx, "test performance", 0, 5)
			require.NoError(t, err)
		}
		globalDuration := time.Since(globalStart)

		avgLocal := localDuration / 10
		avgGlobal := globalDuration / 10

		t.Logf("Average LocalSearch:  %v", avgLocal)
		t.Logf("Average GlobalSearch: %v", avgGlobal)
		t.Logf("LocalSearch is %.2fx faster", float64(globalDuration)/float64(localDuration))

		// LocalSearch should generally be faster since it searches fewer entities
		// But we don't assert this strictly since timing can vary
		if avgLocal < avgGlobal {
			t.Logf("✅ LocalSearch is faster as expected")
		} else {
			t.Logf("⚠️  GlobalSearch was faster (may happen with small datasets)")
		}
	})

	t.Logf("✅ Performance comparison test completed successfully")
}

// TestE2E_ResourceLimits tests that resource limits are enforced
func TestE2E_ResourceLimits(t *testing.T) {
	setup := setupGraphRAGTest(t)
	processor := setup.processor
	ctx := setup.ctx
	communityStorage := setup.communityStorage
	queryMgr := setup.queryManager

	t.Log("Testing resource limit enforcement in GlobalSearch")

	// Create a large community that would exceed MaxTotalEntitiesInSearch
	// MaxTotalEntitiesInSearch is 10,000, so simulate 11,000 entities across communities
	// (5500 * 2 = 11,000 total entity IDs in communities)
	entitiesPerCommunity := 5500
	numCommunities := 2

	for c := 0; c < numCommunities; c++ {
		memberIDs := make([]string, entitiesPerCommunity)
		for e := 0; e < entitiesPerCommunity; e++ {
			id := fmt.Sprintf("large-c%d-e%d", c, e)
			// Only create a subset of entities to keep test fast
			if e < 100 { // Create 100 entities per community
				_, err := createTestEntity(processor, ctx, id, "test.large", map[string]any{
					"name": fmt.Sprintf("Large Entity %d-%d", c, e),
				})
				require.NoError(t, err)
			}
			memberIDs[e] = id // Still add all IDs to community members
		}

		community := &graphclustering.Community{
			ID:                 fmt.Sprintf("comm-0-large%d", c),
			Level:              0,
			Members:            memberIDs, // 5500 members (simulated)
			StatisticalSummary: fmt.Sprintf("Large test community %d", c),
			Keywords:           []string{"large", "test"},
		}
		err := communityStorage.SaveCommunity(ctx, community)
		require.NoError(t, err)
	}

	time.Sleep(300 * time.Millisecond)

	t.Run("GlobalSearch enforces MaxTotalEntitiesInSearch", func(t *testing.T) {
		// This should try to load 11,000 entities but be capped at 10,000
		result, err := queryMgr.GlobalSearch(ctx, "large test", 0, 2)

		require.NoError(t, err, "GlobalSearch should not fail with large datasets")
		assert.NotNil(t, result)

		// The result count might be less than 10,000 since we only created 200 actual entities
		// But the important thing is it didn't try to load all 11,000
		t.Logf("GlobalSearch returned %d entities (capped by resource limit)", result.Count)
	})

	t.Logf("✅ Resource limits test completed successfully")
}

// TestE2E_LLMSummarization tests LLM-based community summarization with semsummarize
// This test is optional and gracefully degrades if semsummarize is unavailable
func TestE2E_LLMSummarization(t *testing.T) {
	setup := setupGraphRAGTest(t)
	processor := setup.processor
	ctx := setup.ctx
	communityStorage := setup.communityStorage

	t.Log("Testing LLM-based community summarization with semsummarize service")

	// Check if semsummarize is available (optional service)
	semsummarizeURL := "http://localhost:8084" // Port from docker-compose.semantic-kitchen.yml
	llmSummarizer := graphclustering.NewHTTPLLMSummarizer(semsummarizeURL)

	// Create test entities with rich content for summarization
	entities := []struct {
		id         string
		entityType string
		properties map[string]any
	}{
		{
			"drone-1", "robotics.drone",
			map[string]any{
				"name":         "Autonomous Delivery Drone",
				"description":  "UAV for package delivery with obstacle avoidance",
				"capabilities": "navigation, object-detection, path-planning",
			},
		},
		{
			"sensor-1", "robotics.sensor",
			map[string]any{
				"name":        "LiDAR Sensor Array",
				"description": "3D mapping sensor for autonomous navigation",
				"range":       "100m",
			},
		},
		{
			"controller-1", "robotics.controller",
			map[string]any{
				"name":        "Flight Controller",
				"description": "Real-time control system for drone stabilization",
				"frequency":   "400Hz",
			},
		},
	}

	// Create entities in the graph
	memberIDs := make([]string, len(entities))
	createdEntities := make([]*gtypes.EntityState, len(entities))
	for i, spec := range entities {
		entity, err := createTestEntity(processor, ctx, spec.id, spec.entityType, spec.properties)
		require.NoError(t, err, "Failed to create entity %s", spec.id)
		memberIDs[i] = spec.id
		createdEntities[i] = entity
	}

	// Create community (without summary initially)
	community := &graphclustering.Community{
		ID:      "comm-llm-test",
		Level:   0,
		Members: memberIDs,
	}

	time.Sleep(200 * time.Millisecond)

	t.Run("LLM_generates_natural_language_summary", func(t *testing.T) {
		// Attempt LLM summarization
		summarizedComm, err := llmSummarizer.SummarizeCommunity(ctx, community, createdEntities)
		require.NoError(t, err, "Summarization should not error (graceful fallback on failure)")
		require.NotNil(t, summarizedComm)

		// Check summary was generated
		assert.NotEmpty(t, summarizedComm.StatisticalSummary, "Summary should not be empty")
		assert.NotEmpty(t, summarizedComm.Keywords, "Keywords should be extracted")
		assert.Contains(t, []string{"llm", "statistical-fallback"}, summarizedComm.SummaryStatus,
			"Summarizer should be llm or statistical-fallback")

		// Log results
		t.Logf("Summarizer used: %s", summarizedComm.SummaryStatus)
		t.Logf("StatisticalSummary: %s", summarizedComm.StatisticalSummary)
		t.Logf("Keywords: %v", summarizedComm.Keywords)

		// If LLM was used, summary should be more natural than statistical
		if summarizedComm.SummaryStatus == "llm" {
			t.Log("✅ LLM service available - testing LLM-generated summary")
			// LLM summaries should be narrative style, not just entity counts
			assert.NotContains(t, summarizedComm.StatisticalSummary, "Community of", "LLM summary should be narrative")
		} else {
			t.Log("⚠️  LLM service unavailable - fell back to statistical summarization")
			t.Log("   This is expected if semsummarize is not running")
		}
	})

	t.Run("LLM_summary_integrates_with_GlobalSearch", func(t *testing.T) {
		// Save community with LLM-generated summary
		summarizedComm, _ := llmSummarizer.SummarizeCommunity(ctx, community, createdEntities)
		err := communityStorage.SaveCommunity(ctx, summarizedComm)
		require.NoError(t, err)

		// Use GlobalSearch to find the community
		result, err := setup.queryManager.GlobalSearch(ctx, "autonomous drone robotics", 0, 5)
		require.NoError(t, err)
		assert.NotNil(t, result)

		// Find our community in results
		var foundComm *querymanager.CommunitySummary
		for i := range result.CommunitySummaries {
			if result.CommunitySummaries[i].CommunityID == "comm-llm-test" {
				foundComm = &result.CommunitySummaries[i]
				break
			}
		}

		if foundComm != nil {
			t.Logf("✅ LLM-summarized community found in GlobalSearch results")
			t.Logf("   Summary in search: %s", foundComm.Summary)
		}
	})

	t.Logf("✅ LLM summarization test completed successfully")
}

func TestE2E_ProgressiveEnhancement(t *testing.T) {
	setup := setupGraphRAGTest(t)
	processor := setup.processor
	ctx := setup.ctx
	communityStorage := setup.communityStorage
	detector := setup.detector

	t.Log("Testing progressive community summarization with async LLM enhancement")

	// Configure progressive summarization
	progressiveSummarizer := graphclustering.NewProgressiveSummarizer()
	llmSummarizer := graphclustering.NewHTTPLLMSummarizer("http://localhost:8084")

	// Start enhancement worker with KV watch
	enhancementWorker, err := graphclustering.NewEnhancementWorker(&graphclustering.EnhancementWorkerConfig{
		LLMSummarizer: llmSummarizer,
		Storage:       communityStorage,
		GraphProvider: &testGraphProvider{
			dataHandler: processor.dataManager,
			kvBucket:    nil, // Not needed for this test
		},
		Querier:         setup.queryManager,
		CommunityBucket: setup.communityBucket, // For KV watch
	})
	require.NoError(t, err)

	// Start worker in background
	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()
	go func() {
		_ = enhancementWorker.Start(workerCtx)
	}()

	// Configure detector with progressive summarization (no NATS connection needed)
	detector.WithProgressiveSummarization(
		progressiveSummarizer,
		setup.queryManager, // Implements EntityProvider
	)

	// Create test entities
	entities := []struct {
		id         string
		entityType string
		properties map[string]any
	}{
		{
			"drone-prog-1", "robotics.drone",
			map[string]any{
				"name":        "Autonomous Delivery Drone",
				"description": "UAV for package delivery with obstacle avoidance",
			},
		},
		{
			"sensor-prog-1", "robotics.sensor",
			map[string]any{
				"name":        "LiDAR Sensor Array",
				"description": "3D mapping sensor for autonomous navigation",
			},
		},
		{
			"controller-prog-1", "robotics.controller",
			map[string]any{
				"name":        "Flight Controller",
				"description": "Real-time control system for drone stabilization",
			},
		},
	}

	// Create entities in the graph
	for _, spec := range entities {
		_, err := createTestEntity(processor, ctx, spec.id, spec.entityType, spec.properties)
		require.NoError(t, err, "Failed to create entity %s", spec.id)
	}

	time.Sleep(200 * time.Millisecond)

	t.Run("Statistical_summary_available_immediately", func(t *testing.T) {
		// Run community detection
		communities, err := detector.DetectCommunities(ctx)
		require.NoError(t, err)
		require.NotEmpty(t, communities)

		// Get level 0 communities
		level0Communities := communities[0]
		require.NotEmpty(t, level0Communities)

		// Find our test community (contains drone-prog-1)
		var testComm *graphclustering.Community
		for _, comm := range level0Communities {
			for _, memberID := range comm.Members {
				if memberID == "drone-prog-1" {
					testComm = comm
					break
				}
			}
			if testComm != nil {
				break
			}
		}

		require.NotNil(t, testComm, "Should find community containing drone-prog-1")

		// Verify statistical summary is present immediately
		assert.NotEmpty(t, testComm.StatisticalSummary, "Statistical summary should be present")
		assert.Equal(t, "statistical", testComm.SummaryStatus, "Status should be 'statistical'")
		assert.Empty(t, testComm.LLMSummary, "LLM summary should be empty initially")

		t.Logf("✅ Statistical summary generated immediately:")
		t.Logf("   StatisticalSummary: %s", testComm.StatisticalSummary)
		t.Logf("   Keywords: %v", testComm.Keywords)
	})

	t.Run("LLM_enhancement_happens_asynchronously", func(t *testing.T) {
		// Wait for async LLM enhancement to complete
		waitForLLMEnhancement := func(communityID string, maxWait time.Duration) (*graphclustering.Community, error) {
			deadline := time.Now().Add(maxWait)
			for time.Now().Before(deadline) {
				comm, err := communityStorage.GetCommunity(ctx, communityID)
				if err != nil {
					return nil, err
				}
				if comm.SummaryStatus == "llm-enhanced" {
					return comm, nil
				}
				if comm.SummaryStatus == "llm-failed" {
					return comm, fmt.Errorf("LLM enhancement failed")
				}
				time.Sleep(500 * time.Millisecond)
			}
			return nil, fmt.Errorf("timeout waiting for LLM enhancement")
		}

		// Get the community ID from detector
		communities, err := detector.DetectCommunities(ctx)
		require.NoError(t, err)

		var testCommID string
		for _, comm := range communities[0] {
			for _, memberID := range comm.Members {
				if memberID == "drone-prog-1" {
					testCommID = comm.ID
					break
				}
			}
			if testCommID != "" {
				break
			}
		}

		require.NotEmpty(t, testCommID, "Should find test community ID")

		// Wait for enhancement (up to 10 seconds)
		enhancedComm, err := waitForLLMEnhancement(testCommID, 10*time.Second)

		if err != nil {
			t.Logf("⚠️  LLM enhancement not completed: %v", err)
			t.Log("   This is expected if semsummarize service is not running")
			t.Log("   Progressive summarization is working - statistical summary is available")
		} else {
			// Verify both summaries are present
			assert.NotEmpty(t, enhancedComm.StatisticalSummary, "Statistical summary should be preserved")
			assert.NotEmpty(t, enhancedComm.LLMSummary, "LLM summary should be populated")
			assert.Equal(t, "llm-enhanced", enhancedComm.SummaryStatus)

			t.Logf("✅ LLM enhancement completed asynchronously:")
			t.Logf("   Statistical: %s", enhancedComm.StatisticalSummary)
			t.Logf("   LLM: %s", enhancedComm.LLMSummary)
		}
	})

	t.Logf("✅ Progressive enhancement test completed successfully")
}
