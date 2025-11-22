package graphclustering

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockGraphProvider implements GraphProvider for testing
type MockGraphProvider struct {
	entities  []string
	neighbors map[string][]string
	weights   map[string]map[string]float64
}

func NewMockGraphProvider() *MockGraphProvider {
	return &MockGraphProvider{
		entities:  make([]string, 0),
		neighbors: make(map[string][]string),
		weights:   make(map[string]map[string]float64),
	}
}

func (m *MockGraphProvider) AddEntity(id string) {
	m.entities = append(m.entities, id)
}

func (m *MockGraphProvider) AddEdge(fromID, toID string, weight float64) {
	// Add bidirectional edge
	m.neighbors[fromID] = append(m.neighbors[fromID], toID)
	m.neighbors[toID] = append(m.neighbors[toID], fromID)

	// Store weights
	if m.weights[fromID] == nil {
		m.weights[fromID] = make(map[string]float64)
	}
	if m.weights[toID] == nil {
		m.weights[toID] = make(map[string]float64)
	}
	m.weights[fromID][toID] = weight
	m.weights[toID][fromID] = weight
}

func (m *MockGraphProvider) GetAllEntityIDs(_ context.Context) ([]string, error) {
	return m.entities, nil
}

func (m *MockGraphProvider) GetNeighbors(_ context.Context, entityID string, _ string) ([]string, error) {
	return m.neighbors[entityID], nil
}

func (m *MockGraphProvider) GetEdgeWeight(_ context.Context, fromID, toID string) (float64, error) {
	if weights, ok := m.weights[fromID]; ok {
		if weight, ok := weights[toID]; ok {
			return weight, nil
		}
	}
	return 0.0, nil
}

// MockCommunityStorage implements CommunityStorage for testing
type MockCommunityStorage struct {
	communities     map[string]*Community
	entityCommunity map[int]map[string]string // level -> entityID -> communityID
}

func NewMockCommunityStorage() *MockCommunityStorage {
	return &MockCommunityStorage{
		communities:     make(map[string]*Community),
		entityCommunity: make(map[int]map[string]string),
	}
}

func (m *MockCommunityStorage) SaveCommunity(_ context.Context, community *Community) error {
	m.communities[community.ID] = community

	// Update entity -> community mapping
	if m.entityCommunity[community.Level] == nil {
		m.entityCommunity[community.Level] = make(map[string]string)
	}
	for _, entityID := range community.Members {
		m.entityCommunity[community.Level][entityID] = community.ID
	}

	return nil
}

func (m *MockCommunityStorage) GetCommunity(_ context.Context, id string) (*Community, error) {
	if community, ok := m.communities[id]; ok {
		return community, nil
	}
	return nil, nil
}

func (m *MockCommunityStorage) GetCommunitiesByLevel(_ context.Context, level int) ([]*Community, error) {
	communities := make([]*Community, 0)
	for _, community := range m.communities {
		if community.Level == level {
			communities = append(communities, community)
		}
	}
	return communities, nil
}

func (m *MockCommunityStorage) GetEntityCommunity(ctx context.Context, entityID string, level int) (*Community, error) {
	if levelMap, ok := m.entityCommunity[level]; ok {
		if communityID, ok := levelMap[entityID]; ok {
			return m.GetCommunity(ctx, communityID)
		}
	}
	return nil, nil
}

func (m *MockCommunityStorage) DeleteCommunity(_ context.Context, id string) error {
	delete(m.communities, id)
	return nil
}

func (m *MockCommunityStorage) Clear(_ context.Context) error {
	m.communities = make(map[string]*Community)
	m.entityCommunity = make(map[int]map[string]string)
	return nil
}

// Test Cases

func TestLPADetector_SimpleGraph(t *testing.T) {
	// Create a simple graph with two clear communities:
	// Community 1: A-B-C (triangle)
	// Community 2: D-E-F (triangle)
	// Bridge: C-D (weak connection)

	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	// Community 1
	provider.AddEntity("A")
	provider.AddEntity("B")
	provider.AddEntity("C")
	provider.AddEdge("A", "B", 1.0)
	provider.AddEdge("B", "C", 1.0)
	provider.AddEdge("C", "A", 1.0)

	// Community 2
	provider.AddEntity("D")
	provider.AddEntity("E")
	provider.AddEntity("F")
	provider.AddEdge("D", "E", 1.0)
	provider.AddEdge("E", "F", 1.0)
	provider.AddEdge("F", "D", 1.0)

	// Bridge (weak connection)
	provider.AddEdge("C", "D", 0.1)

	detector := NewLPADetector(provider, storage)
	detector.WithMaxIterations(50)

	ctx := context.Background()
	communities, err := detector.DetectCommunities(ctx)
	require.NoError(t, err)

	// Should detect 2 communities at level 0
	level0 := communities[0]
	assert.Equal(t, 2, len(level0), "Should detect 2 communities")

	// Verify all entities are assigned
	allMembers := make(map[string]bool)
	for _, community := range level0 {
		assert.GreaterOrEqual(t, len(community.Members), 3, "Each community should have at least 3 members")
		for _, member := range community.Members {
			allMembers[member] = true
		}
	}
	assert.Equal(t, 6, len(allMembers), "All 6 entities should be assigned")
}

func TestLPADetector_IsolatedNodes(t *testing.T) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	// Three isolated nodes
	provider.AddEntity("A")
	provider.AddEntity("B")
	provider.AddEntity("C")

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	communities, err := detector.DetectCommunities(ctx)
	require.NoError(t, err)

	// Each isolated node should form its own community
	level0 := communities[0]
	assert.Equal(t, 3, len(level0), "Should have 3 communities (one per isolated node)")

	for _, community := range level0 {
		assert.Equal(t, 1, len(community.Members), "Isolated node should be alone in community")
	}
}

func TestLPADetector_FullyConnected(t *testing.T) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	// Create a complete graph K5 (fully connected 5 nodes)
	nodes := []string{"A", "B", "C", "D", "E"}
	for _, node := range nodes {
		provider.AddEntity(node)
	}

	for i, node1 := range nodes {
		for j, node2 := range nodes {
			if i < j {
				provider.AddEdge(node1, node2, 1.0)
			}
		}
	}

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	communities, err := detector.DetectCommunities(ctx)
	require.NoError(t, err)

	// Fully connected graph should converge to single community
	level0 := communities[0]
	assert.Equal(t, 1, len(level0), "Fully connected graph should form single community")
	assert.Equal(t, 5, len(level0[0].Members), "Community should contain all 5 nodes")
}

func TestLPADetector_HierarchicalLevels(t *testing.T) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	// Create a larger graph with clear community structure
	// 4 tight clusters of 3 nodes each
	clusters := [][]string{
		{"A1", "A2", "A3"},
		{"B1", "B2", "B3"},
		{"C1", "C2", "C3"},
		{"D1", "D2", "D3"},
	}

	for _, cluster := range clusters {
		for _, node := range cluster {
			provider.AddEntity(node)
		}
		// Fully connect within cluster
		provider.AddEdge(cluster[0], cluster[1], 1.0)
		provider.AddEdge(cluster[1], cluster[2], 1.0)
		provider.AddEdge(cluster[2], cluster[0], 1.0)
	}

	// Add weak inter-cluster connections
	provider.AddEdge("A3", "B1", 0.1)
	provider.AddEdge("B3", "C1", 0.1)
	provider.AddEdge("C3", "D1", 0.1)

	detector := NewLPADetector(provider, storage).WithLevels(3)
	ctx := context.Background()

	communities, err := detector.DetectCommunities(ctx)
	require.NoError(t, err)

	// Level 0: Should detect 4 communities
	level0 := communities[0]
	assert.GreaterOrEqual(t, len(level0), 2, "Should detect multiple communities at level 0")

	// Level 1: Should have fewer communities (hierarchical aggregation)
	level1 := communities[1]
	assert.LessOrEqual(t, len(level1), len(level0), "Level 1 should have fewer or equal communities")

	// Level 2: Top level (may be single community)
	level2 := communities[2]
	assert.GreaterOrEqual(t, len(level2), 1, "Should have at least one top-level community")
}

func TestLPADetector_GetEntityCommunity(t *testing.T) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	// Simple triangle
	provider.AddEntity("A")
	provider.AddEntity("B")
	provider.AddEntity("C")
	provider.AddEdge("A", "B", 1.0)
	provider.AddEdge("B", "C", 1.0)
	provider.AddEdge("C", "A", 1.0)

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.NoError(t, err)

	// Query entity community
	community, err := detector.GetEntityCommunity(ctx, "A", 0)
	require.NoError(t, err)
	require.NotNil(t, community)

	// Verify A, B, C are in same community
	memberSet := make(map[string]bool)
	for _, member := range community.Members {
		memberSet[member] = true
	}
	assert.True(t, memberSet["A"], "A should be in community")
	assert.True(t, memberSet["B"], "B should be in community")
	assert.True(t, memberSet["C"], "C should be in community")
}

func TestLPADetector_EmptyGraph(t *testing.T) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	communities, err := detector.DetectCommunities(ctx)
	require.NoError(t, err)
	assert.Empty(t, communities[0], "Empty graph should produce no communities")
}
