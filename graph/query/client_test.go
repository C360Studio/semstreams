package query

import (
	"context"
	"testing"
	"time"

	gtypes "github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/pkg/cache"
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
	assert.True(t, config.EntityCache.Enabled)
	assert.Equal(t, 1000, config.EntityCache.MaxSize)
	assert.Equal(t, 5*time.Minute, config.EntityCache.TTL)
	assert.Equal(t, 1*time.Minute, config.EntityCache.CleanupInterval)

	// Test bucket configurations. TTL is 0 on every live graph bucket — a TTL
	// here can win the get-or-create race and silently expire ENTITY_STATES
	// (ADR-068 D1). See TestDefaultConfig_NoLifecycleRetentionOnGraphBuckets.
	assert.Equal(t, time.Duration(0), config.EntityStates.TTL)
	assert.Equal(t, uint8(3), config.EntityStates.History)
	assert.Equal(t, 1, config.EntityStates.Replicas)

	assert.Equal(t, time.Duration(0), config.SpatialIndex.TTL)
	assert.Equal(t, uint8(1), config.SpatialIndex.History)
	assert.Equal(t, 1, config.SpatialIndex.Replicas)

	assert.Equal(t, time.Duration(0), config.IncomingIndex.TTL)
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
	// ID format: org.platform.domain.system.type.instance
	entity := &gtypes.EntityState{
		ID: "c360.platform.robotics.system.drone.test-entity",
	}

	// Create a mock client to test the helper method
	client := &natsClient{}

	t.Run("matches by type", func(t *testing.T) {
		// Type is now extracted from ID (5th component) - "drone"
		criteria := map[string]any{"type": "drone"}
		assert.True(t, client.entityMatchesCriteria(entity, criteria))
	})

	t.Run("fails on wrong type", func(t *testing.T) {
		// Type is now extracted from ID (5th component)
		criteria := map[string]any{"type": "gcs"}
		assert.False(t, client.entityMatchesCriteria(entity, criteria))
	})

	t.Run("handles nil entity", func(t *testing.T) {
		criteria := map[string]any{"type": "test"}
		assert.False(t, client.entityMatchesCriteria(nil, criteria))
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
