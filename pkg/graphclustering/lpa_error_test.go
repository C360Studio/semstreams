package graphclustering

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FailingMockProvider simulates provider failures for error testing
type FailingMockProvider struct {
	failOn string // Which method to fail: "GetAllEntityIDs", "GetNeighbors", "GetEdgeWeight"
	err    error  // Error to return
}

func (m *FailingMockProvider) GetAllEntityIDs(ctx context.Context) ([]string, error) {
	if m.failOn == "GetAllEntityIDs" {
		return nil, m.err
	}
	return []string{"A", "B", "C"}, nil
}

func (m *FailingMockProvider) GetNeighbors(ctx context.Context, entityID string, direction string) ([]string, error) {
	if m.failOn == "GetNeighbors" {
		return nil, m.err
	}
	return []string{}, nil
}

func (m *FailingMockProvider) GetEdgeWeight(ctx context.Context, fromID, toID string) (float64, error) {
	if m.failOn == "GetEdgeWeight" {
		return 0, m.err
	}
	return 1.0, nil
}

// FailingMockStorage simulates storage failures for error testing
type FailingMockStorage struct {
	failOn string // Which method to fail
	err    error
}

func (m *FailingMockStorage) SaveCommunity(ctx context.Context, community *Community) error {
	if m.failOn == "SaveCommunity" {
		return m.err
	}
	return nil
}

func (m *FailingMockStorage) GetCommunity(ctx context.Context, id string) (*Community, error) {
	if m.failOn == "GetCommunity" {
		return nil, m.err
	}
	return nil, nil
}

func (m *FailingMockStorage) GetCommunitiesByLevel(ctx context.Context, level int) ([]*Community, error) {
	return []*Community{}, nil
}

func (m *FailingMockStorage) GetEntityCommunity(ctx context.Context, entityID string, level int) (*Community, error) {
	return nil, nil
}

func (m *FailingMockStorage) DeleteCommunity(ctx context.Context, id string) error {
	return nil
}

func (m *FailingMockStorage) Clear(ctx context.Context) error {
	if m.failOn == "Clear" {
		return m.err
	}
	return nil
}

// Test provider failures
func TestLPADetector_ProviderGetAllEntityIDsError(t *testing.T) {
	provider := &FailingMockProvider{
		failOn: "GetAllEntityIDs",
		err:    errors.New("connection lost"),
	}
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection lost")
}

func TestLPADetector_ProviderGetNeighborsError(t *testing.T) {
	provider := &FailingMockProvider{
		failOn: "GetNeighbors",
		err:    errors.New("network timeout"),
	}
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network timeout")
}

// Test storage failures
func TestLPADetector_StorageClearError(t *testing.T) {
	provider := NewMockGraphProvider()
	provider.AddEntity("A")
	provider.AddEntity("B")

	storage := &FailingMockStorage{
		failOn: "Clear",
		err:    errors.New("storage unavailable"),
	}

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage unavailable")
}

func TestLPADetector_StorageSaveError(t *testing.T) {
	provider := NewMockGraphProvider()
	provider.AddEntity("A")
	provider.AddEntity("B")
	provider.AddEdge("A", "B", 1.0)

	storage := &FailingMockStorage{
		failOn: "SaveCommunity",
		err:    errors.New("storage full"),
	}

	detector := NewLPADetector(provider, storage)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage full")
}

// Test context cancellation
func TestLPADetector_ContextCancellation(t *testing.T) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	// Create large graph to ensure multiple iterations
	for i := 0; i < 100; i++ {
		provider.AddEntity(string(rune('A' + i)))
	}

	detector := NewLPADetector(provider, storage)
	detector.WithMaxIterations(1000) // Ensure many iterations

	// Create context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context")
}

// Test nil provider/storage validation
func TestLPADetector_NilProvider(t *testing.T) {
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(nil, storage)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graphProvider is nil")
}

func TestLPADetector_NilStorage(t *testing.T) {
	provider := NewMockGraphProvider()
	provider.AddEntity("A")

	detector := NewLPADetector(provider, nil)
	ctx := context.Background()

	_, err := detector.DetectCommunities(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "storage is nil")
}

// Test input validation
func TestLPADetector_WithMaxIterations_Validation(t *testing.T) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(provider, storage)

	// Test negative value gets clamped to default
	detector.WithMaxIterations(-10)
	assert.Equal(t, DefaultMaxIterations, detector.maxIterations)

	// Test zero value gets clamped to default
	detector.WithMaxIterations(0)
	assert.Equal(t, DefaultMaxIterations, detector.maxIterations)

	// Test excessive value gets capped
	detector.WithMaxIterations(20000)
	assert.Equal(t, MaxIterationsLimit, detector.maxIterations)

	// Test valid value is preserved
	detector.WithMaxIterations(50)
	assert.Equal(t, 50, detector.maxIterations)
}

func TestLPADetector_WithLevels_Validation(t *testing.T) {
	provider := NewMockGraphProvider()
	storage := NewMockCommunityStorage()

	detector := NewLPADetector(provider, storage)

	// Test negative value gets clamped to default
	detector.WithLevels(-5)
	assert.Equal(t, DefaultLevels, detector.levels)

	// Test zero value gets clamped to default
	detector.WithLevels(0)
	assert.Equal(t, DefaultLevels, detector.levels)

	// Test excessive value gets capped
	detector.WithLevels(20)
	assert.Equal(t, MaxLevelsLimit, detector.levels)

	// Test valid value is preserved
	detector.WithLevels(5)
	assert.Equal(t, 5, detector.levels)
}
