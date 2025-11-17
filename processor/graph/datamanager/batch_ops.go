package datamanager

import (
	"context"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
)

// Batch Operations

// BatchWrite performs batch write operations
func (m *Manager) BatchWrite(ctx context.Context, writes []EntityWrite) error {

	// If buffer is enabled, use it
	if m.writeBuffer != nil && m.config.BufferConfig.BatchingEnabled {
		for _, write := range writes {
			if err := m.writeBuffer.Write(&write); err != nil {
				if write.Callback != nil {
					write.Callback(err)
				}
				return errors.Wrap(err, "DataManager", "BatchWrite", "buffer write")
			}
		}
		return nil
	}

	// Direct writes
	for _, write := range writes {
		var err error

		switch write.Operation {
		case OperationCreate:
			if write.Entity != nil {
				_, err = m.createEntityDirect(ctx, write.Entity)
			}
		case OperationUpdate:
			if write.Entity != nil {
				_, err = m.updateEntityDirect(ctx, write.Entity)
			}
		case OperationDelete:
			if write.Entity != nil {
				err = m.deleteEntityDirect(ctx, write.Entity.Node.ID)
			}
		default:
			err = errors.WrapInvalid(nil, "DataManager", "BatchWrite", "invalid operation")
		}

		if write.Callback != nil {
			write.Callback(err)
		}

		if err != nil {
			return err
		}
	}
	return nil
}

// BatchGet retrieves multiple entities
func (m *Manager) BatchGet(ctx context.Context, ids []string) ([]*gtypes.EntityState, error) {

	entities := make([]*gtypes.EntityState, 0, len(ids))
	for _, id := range ids {
		entity, err := m.GetEntity(ctx, id)
		if err != nil {
			// Skip not found entities
			if errors.IsInvalid(err) {
				continue
			}
			return nil, errors.Wrap(err, "DataManager", "BatchGet", "get entity")
		}
		entities = append(entities, entity)
	}
	return entities, nil
}

// List returns entity IDs matching a pattern
func (m *Manager) List(ctx context.Context, _ string) ([]string, error) {

	keys, err := m.kvBucket.Keys(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "DataManager", "List", "list keys")
	}

	// TODO: Add pattern matching if needed
	return keys, nil
}

// GetCacheStats returns cache statistics
func (m *Manager) GetCacheStats() CacheStats {
	stats := CacheStats{}

	// Get L1 stats
	if m.l1Cache != nil {
		if l1Stats := m.l1Cache.Stats(); l1Stats != nil {
			stats.L1Hits = l1Stats.Hits()
			stats.L1Misses = l1Stats.Misses()
			stats.L1Size = m.l1Cache.Size()
			if total := stats.L1Hits + stats.L1Misses; total > 0 {
				stats.L1HitRatio = float64(stats.L1Hits) / float64(total)
			}
			stats.L1Evictions = l1Stats.Evictions()
		}
	}

	// Get L2 stats
	if m.l2Cache != nil {
		if l2Stats := m.l2Cache.Stats(); l2Stats != nil {
			stats.L2Hits = l2Stats.Hits()
			stats.L2Misses = l2Stats.Misses()
			stats.L2Size = m.l2Cache.Size()
			if total := stats.L2Hits + stats.L2Misses; total > 0 {
				stats.L2HitRatio = float64(stats.L2Hits) / float64(total)
			}
			stats.L2Evictions = l2Stats.Evictions()
		}
	}

	// Calculate overall stats
	stats.TotalHits = stats.L1Hits + stats.L2Hits
	stats.TotalMisses = stats.L2Misses // Only L2 misses count as total misses
	if total := stats.TotalHits + stats.TotalMisses; total > 0 {
		stats.OverallHitRatio = float64(stats.TotalHits) / float64(total)
	}

	// KV fetches would be L2 misses
	stats.KVFetches = stats.L2Misses

	return stats
}
