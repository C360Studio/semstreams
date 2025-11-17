package querymanager

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/pkg/graphinterfaces"
	"github.com/c360/semstreams/processor/graph/datamanager"
)

// mockCommunity implements graphinterfaces.Community for testing
type mockCommunity struct {
	id                 string
	level              int
	members            []string
	parentID           *string
	statisticalSummary string
	llmSummary         string
	keywords           []string
	repEntities        []string
	summaryStatus      string
	metadata           map[string]interface{}
}

func (m *mockCommunity) GetID() string                       { return m.id }
func (m *mockCommunity) GetLevel() int                       { return m.level }
func (m *mockCommunity) GetMembers() []string                { return m.members }
func (m *mockCommunity) GetParentID() *string                { return m.parentID }
func (m *mockCommunity) GetStatisticalSummary() string       { return m.statisticalSummary }
func (m *mockCommunity) GetLLMSummary() string               { return m.llmSummary }
func (m *mockCommunity) GetKeywords() []string               { return m.keywords }
func (m *mockCommunity) GetRepEntities() []string            { return m.repEntities }
func (m *mockCommunity) GetSummaryStatus() string            { return m.summaryStatus }
func (m *mockCommunity) GetMetadata() map[string]interface{} { return m.metadata }

// mockCommunityDetector implements communityDetectorInterface for testing
type mockCommunityDetector struct {
	communities map[string]graphinterfaces.Community // by ID
	entityComm  map[string]map[int]graphinterfaces.Community // by entityID -> level -> community
	getErr      error
	listErr     error
}

func (m *mockCommunityDetector) GetCommunity(ctx context.Context, communityID string) (graphinterfaces.Community, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.communities == nil {
		return nil, nil
	}
	return m.communities[communityID], nil
}

func (m *mockCommunityDetector) GetEntityCommunity(ctx context.Context, entityID string, level int) (graphinterfaces.Community, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.entityComm == nil {
		return nil, nil
	}
	if levelMap, ok := m.entityComm[entityID]; ok {
		return levelMap[level], nil
	}
	return nil, nil
}

func (m *mockCommunityDetector) GetCommunitiesByLevel(ctx context.Context, level int) ([]graphinterfaces.Community, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []graphinterfaces.Community
	for _, comm := range m.communities {
		if comm.GetLevel() == level {
			result = append(result, comm)
		}
	}
	return result, nil
}

// mockDataHandler implements minimal DataHandler for testing
type mockDataHandler struct {
	entities map[string]*gtypes.EntityState
}

// BatchGet implements the only method we need for GraphRAG search tests
func (m *mockDataHandler) BatchGet(ctx context.Context, ids []string) ([]*gtypes.EntityState, error) {
	result := make([]*gtypes.EntityState, 0, len(ids))
	for _, id := range ids {
		if entity, ok := m.entities[id]; ok {
			result = append(result, entity)
		}
	}
	return result, nil
}

// Stub implementations for interface compliance (not used in these tests)
func (m *mockDataHandler) Run(ctx context.Context) error                         { return nil }
func (m *mockDataHandler) CreateEntity(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error) {
	return nil, nil
}
func (m *mockDataHandler) UpdateEntity(ctx context.Context, entity *gtypes.EntityState) (*gtypes.EntityState, error) {
	return nil, nil
}
func (m *mockDataHandler) DeleteEntity(ctx context.Context, id string) error { return nil }
func (m *mockDataHandler) GetEntity(ctx context.Context, id string) (*gtypes.EntityState, error) {
	return m.entities[id], nil
}
func (m *mockDataHandler) ExistsEntity(ctx context.Context, id string) (bool, error) { return false, nil }
func (m *mockDataHandler) CreateEntityWithEdges(ctx context.Context, entity *gtypes.EntityState, edges []gtypes.Edge) (*gtypes.EntityState, error) {
	return nil, nil
}
func (m *mockDataHandler) UpdateEntityWithEdges(ctx context.Context, entity *gtypes.EntityState, addEdges []gtypes.Edge, removeEdges []string) (*gtypes.EntityState, error) {
	return nil, nil
}
func (m *mockDataHandler) AddEdge(ctx context.Context, fromEntityID string, edge gtypes.Edge) error {
	return nil
}
func (m *mockDataHandler) RemoveEdge(ctx context.Context, fromEntityID, toEntityID, edgeType string) error {
	return nil
}
func (m *mockDataHandler) GetEdges(ctx context.Context, id string, direction string) ([]gtypes.Edge, error) {
	return nil, nil
}
func (m *mockDataHandler) BatchWrite(ctx context.Context, writes []datamanager.EntityWrite) error {
	return nil
}
func (m *mockDataHandler) List(ctx context.Context, pattern string) ([]string, error) {
	return nil, nil
}
func (m *mockDataHandler) CreateRelationship(ctx context.Context, fromEntityID, toEntityID string, edgeType string, properties map[string]any) error {
	return nil
}
func (m *mockDataHandler) DeleteRelationship(ctx context.Context, fromEntityID, toEntityID string) error {
	return nil
}
func (m *mockDataHandler) CleanupIncomingReferences(ctx context.Context, deletedEntityID string, outgoingEdges []gtypes.Edge) error {
	return nil
}
func (m *mockDataHandler) CheckOutgoingEdgesConsistency(ctx context.Context, entityID string, entity *gtypes.EntityState, status *datamanager.EntityIndexStatus) {
}
func (m *mockDataHandler) HasEdgeToEntity(entity *gtypes.EntityState, targetEntityID string) bool {
	return false
}
func (m *mockDataHandler) GetCacheStats() datamanager.CacheStats {
	return datamanager.CacheStats{}
}
func (m *mockDataHandler) FlushPendingWrites(ctx context.Context) error { return nil }
func (m *mockDataHandler) GetPendingWriteCount() int                    { return 0 }

func Test_scoreCommunitySummaries(t *testing.T) {
	m := &Manager{}

	t.Run("Empty communities", func(t *testing.T) {
		result := m.scoreCommunitySummaries([]graphinterfaces.Community{}, "test query")
		assert.Empty(t, result)
	})

	t.Run("Score by summary match", func(t *testing.T) {
		communities := []graphinterfaces.Community{
			&mockCommunity{
				id:      "comm-1",
				statisticalSummary: "This is about robotics and automation",
			},
			&mockCommunity{
				id:      "comm-2",
				statisticalSummary: "This is about web development",
			},
		}

		result := m.scoreCommunitySummaries(communities, "robotics")

		require.Len(t, result, 2)
		assert.Equal(t, "comm-1", result[0].GetID(), "robotics community should be first")
		assert.Equal(t, "comm-2", result[1].GetID())
	})

	t.Run("Score by keyword match", func(t *testing.T) {
		communities := []graphinterfaces.Community{
			&mockCommunity{
				id:       "comm-1",
				keywords: []string{"python", "django", "flask"},
			},
			&mockCommunity{
				id:       "comm-2",
				keywords: []string{"go", "concurrent", "microservices"},
			},
		}

		result := m.scoreCommunitySummaries(communities, "go microservices")

		require.Len(t, result, 2)
		assert.Equal(t, "comm-2", result[0].GetID(), "go community should be first")
	})

	t.Run("Combined scoring", func(t *testing.T) {
		communities := []graphinterfaces.Community{
			&mockCommunity{
				id:       "comm-1",
				statisticalSummary:  "Machine learning models",
				keywords: []string{"ml", "ai"},
			},
			&mockCommunity{
				id:       "comm-2",
				statisticalSummary:  "AI and machine learning techniques",
				keywords: []string{"machine-learning", "deep-learning"},
			},
			&mockCommunity{
				id:       "comm-3",
				statisticalSummary:  "Web development frameworks",
				keywords: []string{"web", "http"},
			},
		}

		result := m.scoreCommunitySummaries(communities, "machine learning")

		require.Len(t, result, 3)
		// comm-2 should score highest (matches in both summary and keywords)
		assert.Equal(t, "comm-2", result[0].GetID())
		// comm-1 should be second (matches in summary)
		assert.Equal(t, "comm-1", result[1].GetID())
		// comm-3 should be last (no matches)
		assert.Equal(t, "comm-3", result[2].GetID())
	})
}

func Test_filterEntitiesByQuery(t *testing.T) {
	m := &Manager{}

	entities := []*gtypes.EntityState{
		{
			Node: gtypes.NodeProperties{
				ID:   "e1",
				Type: "robotics.drone",
				Properties: map[string]any{
					"name": "Autonomous Drone",
				},
			},
		},
		{
			Node: gtypes.NodeProperties{
				ID:   "e2",
				Type: "network.router",
				Properties: map[string]any{
					"name": "Core Router",
				},
			},
		},
		{
			Node: gtypes.NodeProperties{
				ID:   "e3",
				Type: "robotics.sensor",
				Properties: map[string]any{
					"name": "LiDAR Sensor",
				},
			},
		},
	}

	t.Run("Match by type", func(t *testing.T) {
		result := m.filterEntitiesByQuery(entities, "robotics")
		assert.Len(t, result, 2, "Should match 2 robotics entities")
	})

	t.Run("Match by property", func(t *testing.T) {
		result := m.filterEntitiesByQuery(entities, "drone")
		assert.Len(t, result, 1)
		assert.Equal(t, "e1", result[0].Node.ID)
	})

	t.Run("Match by ID", func(t *testing.T) {
		result := m.filterEntitiesByQuery(entities, "e2")
		assert.Len(t, result, 1)
		assert.Equal(t, "e2", result[0].Node.ID)
	})

	t.Run("No match", func(t *testing.T) {
		result := m.filterEntitiesByQuery(entities, "nonexistent")
		assert.Empty(t, result)
	})

	t.Run("Empty query returns all", func(t *testing.T) {
		result := m.filterEntitiesByQuery(entities, "")
		assert.Len(t, result, 3)
	})

	t.Run("Multiple terms - any match", func(t *testing.T) {
		result := m.filterEntitiesByQuery(entities, "drone router")
		assert.Len(t, result, 2, "Should match entities with either 'drone' or 'router'")
	})
}

func Test_entityMatchesQuery(t *testing.T) {
	m := &Manager{}

	entity := &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:   "test-entity",
			Type: "robotics.drone",
			Properties: map[string]any{
				"name":        "Test Drone",
				"description": "Autonomous navigation system",
				"version":     "2.0",
			},
		},
	}

	tests := []struct {
		name       string
		queryTerms []string
		want       bool
	}{
		{
			name:       "Match ID",
			queryTerms: []string{"test-entity"},
			want:       true,
		},
		{
			name:       "Match type",
			queryTerms: []string{"robotics"},
			want:       true,
		},
		{
			name:       "Match property key",
			queryTerms: []string{"description"},
			want:       true,
		},
		{
			name:       "Match property value",
			queryTerms: []string{"autonomous"},
			want:       true,
		},
		{
			name:       "No match",
			queryTerms: []string{"nonexistent"},
			want:       false,
		},
		{
			name:       "Case insensitive",
			queryTerms: []string{"DRONE"},
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.entityMatchesQuery(entity, tt.queryTerms)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLocalSearch_Success(t *testing.T) {
	ctx := context.Background()

	// Setup mock community detector
	comm := &mockCommunity{
		id:      "comm-0-robotics",
		level:   0,
		members: []string{"e1", "e2", "e3"},
		statisticalSummary: "Robotics community",
	}

	detector := &mockCommunityDetector{
		entityComm: map[string]map[int]graphinterfaces.Community{
			"e1": {0: comm},
		},
	}

	// Setup mock data handler
	dataHandler := &mockDataHandler{
		entities: map[string]*gtypes.EntityState{
			"e1": {
				Node: gtypes.NodeProperties{
					ID:   "e1",
					Type: "robotics.drone",
					Properties: map[string]any{
						"name": "Test Drone",
					},
				},
			},
			"e2": {
				Node: gtypes.NodeProperties{
					ID:   "e2",
					Type: "robotics.sensor",
					Properties: map[string]any{
						"name": "Sensor",
					},
				},
			},
			"e3": {
				Node: gtypes.NodeProperties{
					ID:   "e3",
					Type: "network.router",
					Properties: map[string]any{
						"name": "Router",
					},
				},
			},
		},
	}

	m := &Manager{
		communityDetector: detector,
		dataHandler:       dataHandler,
	}

	result, err := m.LocalSearch(ctx, "e1", "robotics", 0)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "comm-0-robotics", result.CommunityID)
	assert.Equal(t, 2, result.Count, "Should match 2 robotics entities")
	assert.Len(t, result.Entities, 2)
}

func TestLocalSearch_EntityNotInCommunity(t *testing.T) {
	ctx := context.Background()

	detector := &mockCommunityDetector{
		entityComm: map[string]map[int]graphinterfaces.Community{},
	}

	m := &Manager{
		communityDetector: detector,
		dataHandler:       &mockDataHandler{entities: map[string]*gtypes.EntityState{}},
	}

	result, err := m.LocalSearch(ctx, "nonexistent", "query", 0)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not in any community")
}

func TestLocalSearch_CommunityDetectorUnavailable(t *testing.T) {
	ctx := context.Background()

	m := &Manager{
		communityDetector: nil,
		dataHandler:       &mockDataHandler{entities: map[string]*gtypes.EntityState{}},
	}

	result, err := m.LocalSearch(ctx, "e1", "query", 0)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "not available")
}

func TestGlobalSearch_Success(t *testing.T) {
	ctx := context.Background()

	// Setup communities
	comm1 := &mockCommunity{
		id:       "comm-0-robotics",
		level:    0,
		members:  []string{"e1", "e2"},
		statisticalSummary:  "Robotics and autonomous systems",
		keywords: []string{"robotics", "autonomous", "drone"},
	}
	comm2 := &mockCommunity{
		id:       "comm-0-network",
		level:    0,
		members:  []string{"e3"},
		statisticalSummary:  "Network infrastructure",
		keywords: []string{"network", "router", "switch"},
	}

	detector := &mockCommunityDetector{
		communities: map[string]graphinterfaces.Community{
			"comm-0-robotics": comm1,
			"comm-0-network":  comm2,
		},
	}

	dataHandler := &mockDataHandler{
		entities: map[string]*gtypes.EntityState{
			"e1": {
				Node: gtypes.NodeProperties{
					ID:   "e1",
					Type: "robotics.drone",
					Properties: map[string]any{
						"name": "Autonomous Drone",
					},
				},
			},
			"e2": {
				Node: gtypes.NodeProperties{
					ID:   "e2",
					Type: "robotics.sensor",
					Properties: map[string]any{
						"name": "Sensor",
					},
				},
			},
			"e3": {
				Node: gtypes.NodeProperties{
					ID:   "e3",
					Type: "network.router",
					Properties: map[string]any{
						"name": "Router",
					},
				},
			},
		},
	}

	m := &Manager{
		communityDetector: detector,
		dataHandler:       dataHandler,
	}

	result, err := m.GlobalSearch(ctx, "robotics autonomous", 0, 1)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.CommunitySummaries, 1, "Should return top 1 community")
	assert.Equal(t, "comm-0-robotics", result.CommunitySummaries[0].CommunityID)
	assert.Greater(t, result.CommunitySummaries[0].Relevance, 0.0)
	assert.Equal(t, 2, result.Count, "Should find 2 entities in robotics community")
}

func TestGlobalSearch_EmptyCommunities(t *testing.T) {
	ctx := context.Background()

	detector := &mockCommunityDetector{
		communities: map[string]graphinterfaces.Community{},
	}

	m := &Manager{
		communityDetector: detector,
		dataHandler:       &mockDataHandler{entities: map[string]*gtypes.EntityState{}},
	}

	result, err := m.GlobalSearch(ctx, "query", 0, 5)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.Entities)
	assert.Empty(t, result.CommunitySummaries)
	assert.Equal(t, 0, result.Count)
}

func TestGlobalSearch_MaxCommunitiesLimit(t *testing.T) {
	ctx := context.Background()

	// Create 10 communities
	communities := make(map[string]graphinterfaces.Community)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("comm-0-%d", i)
		communities[id] = &mockCommunity{
			id:       id,
			level:    0,
			members:  []string{fmt.Sprintf("e%d", i)},
			statisticalSummary:  fmt.Sprintf("Community %d about testing", i),
			keywords: []string{"test", "community"},
		}
	}

	detector := &mockCommunityDetector{
		communities: communities,
	}

	// Create corresponding entities
	entities := make(map[string]*gtypes.EntityState)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("e%d", i)
		entities[id] = &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID:   id,
				Type: "test.entity",
				Properties: map[string]any{
					"name": fmt.Sprintf("Entity %d", i),
				},
			},
		}
	}

	dataHandler := &mockDataHandler{
		entities: entities,
	}

	m := &Manager{
		communityDetector: detector,
		dataHandler:       dataHandler,
	}

	// Request only top 3 communities
	result, err := m.GlobalSearch(ctx, "testing", 0, 3)

	require.NoError(t, err)
	assert.Len(t, result.CommunitySummaries, 3, "Should limit to 3 communities")
}

func TestGlobalSearch_DefaultMaxCommunities(t *testing.T) {
	ctx := context.Background()

	detector := &mockCommunityDetector{
		communities: map[string]graphinterfaces.Community{},
	}

	m := &Manager{
		communityDetector: detector,
		dataHandler:       &mockDataHandler{entities: map[string]*gtypes.EntityState{}},
	}

	// maxCommunities = 0 should default to 5
	result, err := m.GlobalSearch(ctx, "query", 0, 0)

	require.NoError(t, err)
	assert.NotNil(t, result)
	// Result should be empty due to no communities, but the code path is tested
}

// Benchmarks for GraphRAG hot paths

func BenchmarkLocalSearch(b *testing.B) {
	ctx := context.Background()

	// Setup: Create a realistic community with 100 entities
	memberIDs := make([]string, 100)
	entities := make(map[string]*gtypes.EntityState)
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("entity-%d", i)
		memberIDs[i] = id
		entities[id] = &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID:   id,
				Type: "robotics.sensor",
				Properties: map[string]any{
					"name":        fmt.Sprintf("Sensor %d", i),
					"description": "Temperature and humidity monitoring",
					"location":    fmt.Sprintf("Building-%d", i%10),
				},
			},
		}
	}

	comm := &mockCommunity{
		id:      "bench-comm",
		level:   0,
		members: memberIDs,
		statisticalSummary: "IoT sensor network for environmental monitoring",
	}

	detector := &mockCommunityDetector{
		entityComm: map[string]map[int]graphinterfaces.Community{
			"entity-0": {0: comm},
		},
	}

	m := &Manager{
		communityDetector: detector,
		dataHandler:       &mockDataHandler{entities: entities},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := m.LocalSearch(ctx, "entity-0", "temperature sensor", 0)
		if err != nil {
			b.Fatalf("LocalSearch failed: %v", err)
		}
	}
}

func BenchmarkGlobalSearch(b *testing.B) {
	ctx := context.Background()

	// Setup: Create 10 communities with 50 entities each
	communities := make(map[string]graphinterfaces.Community)
	allEntities := make(map[string]*gtypes.EntityState)

	for commIdx := 0; commIdx < 10; commIdx++ {
		memberIDs := make([]string, 50)
		for i := 0; i < 50; i++ {
			id := fmt.Sprintf("e-%d-%d", commIdx, i)
			memberIDs[i] = id
			allEntities[id] = &gtypes.EntityState{
				Node: gtypes.NodeProperties{
					ID:   id,
					Type: "test.entity",
					Properties: map[string]any{
						"name": fmt.Sprintf("Entity %d-%d", commIdx, i),
						"tag":  fmt.Sprintf("comm-%d", commIdx),
					},
				},
			}
		}

		commID := fmt.Sprintf("comm-%d", commIdx)
		communities[commID] = &mockCommunity{
			id:       commID,
			level:    0,
			members:  memberIDs,
			statisticalSummary:  fmt.Sprintf("Community %d with test entities", commIdx),
			keywords: []string{fmt.Sprintf("test-%d", commIdx), "benchmark"},
		}
	}

	detector := &mockCommunityDetector{
		communities: communities,
	}

	m := &Manager{
		communityDetector: detector,
		dataHandler:       &mockDataHandler{entities: allEntities},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := m.GlobalSearch(ctx, "test benchmark entity", 0, 5)
		if err != nil {
			b.Fatalf("GlobalSearch failed: %v", err)
		}
	}
}

func BenchmarkScoreCommunitySummaries(b *testing.B) {
	m := &Manager{}

	// Create 100 communities with varying summaries and keywords
	communities := make([]graphinterfaces.Community, 100)
	for i := 0; i < 100; i++ {
		communities[i] = &mockCommunity{
			id:       fmt.Sprintf("comm-%d", i),
			level:    0,
			statisticalSummary:  fmt.Sprintf("Community about robotics automation and sensor networks topic-%d", i),
			keywords: []string{"robotics", "automation", "sensors", fmt.Sprintf("topic-%d", i)},
		}
	}

	query := "robotics sensor automation"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.scoreCommunitySummaries(communities, query)
	}
}

func BenchmarkFilterEntitiesByQuery(b *testing.B) {
	m := &Manager{}

	// Create 1000 entities
	entities := make([]*gtypes.EntityState, 1000)
	for i := 0; i < 1000; i++ {
		entities[i] = &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID:   fmt.Sprintf("entity-%d", i),
				Type: fmt.Sprintf("type-%d", i%10),
				Properties: map[string]any{
					"name":        fmt.Sprintf("Test Entity %d", i),
					"description": "Description with various keywords",
					"category":    fmt.Sprintf("cat-%d", i%5),
				},
			},
		}
	}

	query := "test entity description"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.filterEntitiesByQuery(entities, query)
	}
}
