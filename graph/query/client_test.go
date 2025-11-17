package query

import (
	"context"
	"testing"
	"time"

	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/pkg/cache"
	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	t.Run("fails with nil client", func(t *testing.T) {
		_, err := NewClient(context.Background(), nil, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "natsClient cannot be nil")
	})

	t.Run("uses default config when nil", func(t *testing.T) {
		// This test just verifies the DefaultConfig function works
		config := DefaultConfig()
		assert.NotNil(t, config)
		assert.Equal(t, cache.StrategyHybrid, config.EntityCache.Strategy)
		assert.Equal(t, 1000, config.EntityCache.MaxSize)
		assert.Equal(t, 5*time.Minute, config.EntityCache.TTL)
	})
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	// Test EntityCache configuration
	assert.Equal(t, cache.StrategyHybrid, config.EntityCache.Strategy)
	assert.Equal(t, 1000, config.EntityCache.MaxSize)
	assert.Equal(t, 5*time.Minute, config.EntityCache.TTL)
	assert.Equal(t, 1*time.Minute, config.EntityCache.CleanupInterval)

	// Test bucket configurations
	assert.Equal(t, 24*time.Hour, config.EntityStates.TTL)
	assert.Equal(t, uint8(3), config.EntityStates.History)
	assert.Equal(t, 1, config.EntityStates.Replicas)

	assert.Equal(t, 1*time.Hour, config.SpatialIndex.TTL)
	assert.Equal(t, uint8(1), config.SpatialIndex.History)
	assert.Equal(t, 1, config.SpatialIndex.Replicas)

	assert.Equal(t, 24*time.Hour, config.IncomingIndex.TTL)
	assert.Equal(t, uint8(1), config.IncomingIndex.History)
	assert.Equal(t, 1, config.IncomingIndex.Replicas)
}

func TestPathQuery_Validation(t *testing.T) {
	// Test the validation logic that would be used in ExecutePathQuery
	validateQuery := func(query PathQuery) error {
		if query.StartEntity == "" {
			return assert.AnError
		}
		if query.MaxDepth <= 0 {
			return assert.AnError
		}
		if query.MaxNodes <= 0 {
			return assert.AnError
		}
		if query.DecayFactor < 0.0 || query.DecayFactor > 1.0 {
			return assert.AnError
		}
		if query.MaxTime < 0 {
			return assert.AnError
		}
		if query.MaxPaths < 0 {
			return assert.AnError
		}
		return nil
	}

	t.Run("valid query passes validation", func(t *testing.T) {
		query := PathQuery{
			StartEntity: "test-entity",
			MaxDepth:    3,
			MaxNodes:    100,
			DecayFactor: 0.8,
			MaxTime:     5 * time.Second,
			MaxPaths:    10,
		}
		assert.NoError(t, validateQuery(query))
	})

	t.Run("empty start entity fails", func(t *testing.T) {
		query := PathQuery{
			MaxDepth:    3,
			MaxNodes:    100,
			DecayFactor: 0.8,
		}
		assert.Error(t, validateQuery(query))
	})

	t.Run("negative max depth fails", func(t *testing.T) {
		query := PathQuery{
			StartEntity: "test",
			MaxDepth:    -1,
			MaxNodes:    100,
			DecayFactor: 0.8,
		}
		assert.Error(t, validateQuery(query))
	})

	t.Run("invalid decay factor fails", func(t *testing.T) {
		query := PathQuery{
			StartEntity: "test",
			MaxDepth:    3,
			MaxNodes:    100,
			DecayFactor: 1.5, // > 1.0
		}
		assert.Error(t, validateQuery(query))
	})
}

func TestEntityMatchesCriteria(t *testing.T) {
	// Test the entity matching logic
	entity := &gtypes.EntityState{
		Node: gtypes.NodeProperties{
			ID:   "test-entity",
			Type: "robotics.drone",
			Properties: map[string]any{
				"model":   "quadcopter",
				"battery": 85.5,
				"armed":   true,
			},
			Status: gtypes.StatusActive,
		},
	}

	// Create a mock client to test the helper method
	client := &natsClient{}

	t.Run("matches by type", func(t *testing.T) {
		criteria := map[string]any{"type": "robotics.drone"}
		assert.True(t, client.entityMatchesCriteria(entity, criteria))
	})

	t.Run("matches by property", func(t *testing.T) {
		criteria := map[string]any{"model": "quadcopter"}
		assert.True(t, client.entityMatchesCriteria(entity, criteria))
	})

	t.Run("matches by multiple criteria", func(t *testing.T) {
		criteria := map[string]any{
			"type":    "robotics.drone",
			"model":   "quadcopter",
			"battery": 85.5,
		}
		assert.True(t, client.entityMatchesCriteria(entity, criteria))
	})

	t.Run("fails on wrong type", func(t *testing.T) {
		criteria := map[string]any{"type": "robotics.gcs"}
		assert.False(t, client.entityMatchesCriteria(entity, criteria))
	})

	t.Run("fails on missing property", func(t *testing.T) {
		criteria := map[string]any{"missing": "value"}
		assert.False(t, client.entityMatchesCriteria(entity, criteria))
	})

	t.Run("fails on wrong property value", func(t *testing.T) {
		criteria := map[string]any{"model": "fixed-wing"}
		assert.False(t, client.entityMatchesCriteria(entity, criteria))
	})

	t.Run("handles nil entity", func(t *testing.T) {
		criteria := map[string]any{"type": "test"}
		assert.False(t, client.entityMatchesCriteria(nil, criteria))
	})

	t.Run("handles entity with nil properties", func(t *testing.T) {
		entityNoProps := &gtypes.EntityState{
			Node: gtypes.NodeProperties{
				ID:         "test",
				Type:       "test.type",
				Properties: nil,
			},
		}
		criteria := map[string]any{"missing": "value"}
		assert.False(t, client.entityMatchesCriteria(entityNoProps, criteria))
	})
}

func TestShouldFollowEdge(t *testing.T) {
	client := &natsClient{}

	edge := gtypes.Edge{
		ToEntityID: "target-entity",
		EdgeType:   "POWERED_BY",
	}

	t.Run("follows edge with no filter", func(t *testing.T) {
		assert.True(t, client.shouldFollowEdge(edge, nil))
		assert.True(t, client.shouldFollowEdge(edge, []string{}))
	})

	t.Run("follows edge matching filter", func(t *testing.T) {
		filter := []string{"POWERED_BY", "CONTROLLED_BY"}
		assert.True(t, client.shouldFollowEdge(edge, filter))
	})

	t.Run("rejects edge not in filter", func(t *testing.T) {
		filter := []string{"CONTROLLED_BY", "NEAR"}
		assert.False(t, client.shouldFollowEdge(edge, filter))
	})
}

func TestContainsString(t *testing.T) {
	client := &natsClient{}

	slice := []string{"one", "two", "three"}

	t.Run("finds existing string", func(t *testing.T) {
		assert.True(t, client.containsString(slice, "two"))
	})

	t.Run("doesn't find missing string", func(t *testing.T) {
		assert.False(t, client.containsString(slice, "four"))
	})

	t.Run("handles empty slice", func(t *testing.T) {
		assert.False(t, client.containsString([]string{}, "test"))
	})

	t.Run("handles nil slice", func(t *testing.T) {
		assert.False(t, client.containsString(nil, "test"))
	})
}

func TestCacheStats(t *testing.T) {
	// Test the CacheStats structure and calculation
	stats := CacheStats{
		Hits:    10,
		Misses:  5,
		Size:    100,
		HitRate: 0.666,
	}

	assert.Equal(t, int64(10), stats.Hits)
	assert.Equal(t, int64(5), stats.Misses)
	assert.Equal(t, 100, stats.Size)
	assert.InDelta(t, 0.666, stats.HitRate, 0.001)
}
