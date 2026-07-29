package structural

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// Key patterns (in STRUCTURAL_INDEX — see graph.BucketStructuralIndex):
// - structural.kcore._meta           - KCore index metadata and global stats
// - structural.kcore.entity.{id}     - Per-entity core number
// - structural.pivot._meta           - Pivot index metadata (pivots list, computed_at)
// - structural.pivot.entity.{id}     - Per-entity distance vector

// Storage defines the interface for persisting structural indices.
type Storage interface {
	// SaveKCoreIndex persists the k-core index.
	SaveKCoreIndex(ctx context.Context, index *KCoreIndex) error

	// GetKCoreIndex retrieves the k-core index.
	GetKCoreIndex(ctx context.Context) (*KCoreIndex, error)

	// SavePivotIndex persists the pivot index.
	SavePivotIndex(ctx context.Context, index *PivotIndex) error

	// GetPivotIndex retrieves the pivot index.
	GetPivotIndex(ctx context.Context) (*PivotIndex, error)

	// Clear removes all structural index data.
	Clear(ctx context.Context) error
}

// kcoreMeta holds the metadata portion of KCoreIndex for storage.
type kcoreMeta struct {
	MaxCore     int      `json:"max_core"`
	ComputedAt  string   `json:"computed_at"`
	EntityCount int      `json:"entity_count"`
	CoreBuckets []string `json:"core_buckets"` // JSON keys for core buckets
}

// pivotMeta holds the metadata portion of PivotIndex for storage.
type pivotMeta struct {
	Pivots      []string `json:"pivots"`
	ComputedAt  string   `json:"computed_at"`
	EntityCount int      `json:"entity_count"`
}

// NATSStructuralIndexStorage implements StructuralIndexStorage using NATS KV.
type NATSStructuralIndexStorage struct {
	kv        jetstream.KeyValue
	testStore *testStore // In-memory store for testing when kv is nil
}

// testStore provides in-memory storage for unit testing.
// NOT GOROUTINE-SAFE: Only use in single-threaded unit tests.
// For concurrent testing, use real NATS KV with integration tests.
type testStore struct {
	kcore *KCoreIndex
	pivot *PivotIndex
}

// NewNATSStructuralIndexStorage creates a new NATS-backed structural index storage.
func NewNATSStructuralIndexStorage(kv jetstream.KeyValue) *NATSStructuralIndexStorage {
	storage := &NATSStructuralIndexStorage{
		kv: kv,
	}

	// Initialize in-memory test store if KV is nil
	if kv == nil {
		storage.testStore = &testStore{}
	}

	return storage
}

// SaveKCoreIndex persists the k-core index to NATS KV.
// Stores metadata separately from per-entity core numbers for efficient lookups.
func (s *NATSStructuralIndexStorage) SaveKCoreIndex(ctx context.Context, index *KCoreIndex) error {
	if index == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSStructuralIndexStorage", "SaveKCoreIndex", "index is nil")
	}

	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		s.testStore.kcore = index
		return nil
	}

	if s.kv == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSStructuralIndexStorage", "SaveKCoreIndex", "kv is nil")
	}

	// Store per-entity core numbers
	for entityID, coreNum := range index.CoreNumbers {
		key := kcoreEntityKey(entityID)
		value := []byte(fmt.Sprintf("%d", coreNum))
		if _, err := s.kv.Put(ctx, key, value); err != nil {
			return errs.WrapTransient(err, "NATSStructuralIndexStorage", "SaveKCoreIndex", "put entity core")
		}
	}

	// Store core buckets as JSON arrays
	bucketKeys := make([]string, 0, len(index.CoreBuckets))
	for core, entities := range index.CoreBuckets {
		key := kcoreBucketKey(core)
		bucketKeys = append(bucketKeys, key)

		data, err := json.Marshal(entities)
		if err != nil {
			return errs.WrapInvalid(err, "NATSStructuralIndexStorage", "SaveKCoreIndex", "marshal bucket")
		}
		if _, err := s.kv.Put(ctx, key, data); err != nil {
			return errs.WrapTransient(err, "NATSStructuralIndexStorage", "SaveKCoreIndex", "put bucket")
		}
	}

	// Store metadata
	meta := kcoreMeta{
		MaxCore:     index.MaxCore,
		ComputedAt:  index.ComputedAt.Format("2006-01-02T15:04:05Z07:00"),
		EntityCount: index.EntityCount,
		CoreBuckets: bucketKeys,
	}

	metaData, err := json.Marshal(meta)
	if err != nil {
		return errs.WrapInvalid(err, "NATSStructuralIndexStorage", "SaveKCoreIndex", "marshal meta")
	}

	if _, err := s.kv.Put(ctx, kcoreMetaKey(), metaData); err != nil {
		return errs.WrapTransient(err, "NATSStructuralIndexStorage", "SaveKCoreIndex", "put meta")
	}

	return nil
}

// GetKCoreIndex retrieves the k-core index from NATS KV.
func (s *NATSStructuralIndexStorage) GetKCoreIndex(ctx context.Context) (*KCoreIndex, error) {
	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		return s.testStore.kcore, nil
	}

	if s.kv == nil {
		return nil, nil
	}

	// Get metadata
	metaEntry, err := s.kv.Get(ctx, kcoreMetaKey())
	if err != nil {
		if stderrors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, errs.WrapTransient(err, "NATSStructuralIndexStorage", "GetKCoreIndex", "get meta")
	}

	var meta kcoreMeta
	if err := json.Unmarshal(metaEntry.Value(), &meta); err != nil {
		return nil, errs.WrapInvalid(err, "NATSStructuralIndexStorage", "GetKCoreIndex", "unmarshal meta")
	}

	// Parse computed time
	computedAt, err := parseTime(meta.ComputedAt)
	if err != nil {
		return nil, errs.WrapInvalid(err, "NATSStructuralIndexStorage", "GetKCoreIndex", "parse computed_at")
	}

	// Load core buckets
	coreBuckets := make(map[int][]string)
	for _, bucketKey := range meta.CoreBuckets {
		entry, err := s.kv.Get(ctx, bucketKey)
		if err != nil {
			if stderrors.Is(err, jetstream.ErrKeyNotFound) {
				continue
			}
			return nil, errs.WrapTransient(err, "NATSStructuralIndexStorage", "GetKCoreIndex", "get bucket")
		}

		var entities []string
		if err := json.Unmarshal(entry.Value(), &entities); err != nil {
			return nil, errs.WrapInvalid(err, "NATSStructuralIndexStorage", "GetKCoreIndex", "unmarshal bucket")
		}

		// Extract core number from key
		core := parseCoreFromBucketKey(bucketKey)
		coreBuckets[core] = entities
	}

	// Rebuild core numbers from buckets
	coreNumbers := make(map[string]int)
	for core, entities := range coreBuckets {
		for _, entityID := range entities {
			coreNumbers[entityID] = core
		}
	}

	return &KCoreIndex{
		CoreNumbers: coreNumbers,
		MaxCore:     meta.MaxCore,
		CoreBuckets: coreBuckets,
		ComputedAt:  computedAt,
		EntityCount: meta.EntityCount,
	}, nil
}

// SavePivotIndex persists the pivot index to NATS KV.
func (s *NATSStructuralIndexStorage) SavePivotIndex(ctx context.Context, index *PivotIndex) error {
	if index == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSStructuralIndexStorage", "SavePivotIndex", "index is nil")
	}

	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		s.testStore.pivot = index
		return nil
	}

	if s.kv == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSStructuralIndexStorage", "SavePivotIndex", "kv is nil")
	}

	// Store per-entity distance vectors
	for entityID, distances := range index.DistanceVectors {
		key := pivotEntityKey(entityID)
		data, err := json.Marshal(distances)
		if err != nil {
			return errs.WrapInvalid(err, "NATSStructuralIndexStorage", "SavePivotIndex", "marshal distances")
		}
		if _, err := s.kv.Put(ctx, key, data); err != nil {
			return errs.WrapTransient(err, "NATSStructuralIndexStorage", "SavePivotIndex", "put entity distances")
		}
	}

	// Store metadata
	meta := pivotMeta{
		Pivots:      index.Pivots,
		ComputedAt:  index.ComputedAt.Format("2006-01-02T15:04:05Z07:00"),
		EntityCount: index.EntityCount,
	}

	metaData, err := json.Marshal(meta)
	if err != nil {
		return errs.WrapInvalid(err, "NATSStructuralIndexStorage", "SavePivotIndex", "marshal meta")
	}

	if _, err := s.kv.Put(ctx, pivotMetaKey(), metaData); err != nil {
		return errs.WrapTransient(err, "NATSStructuralIndexStorage", "SavePivotIndex", "put meta")
	}

	return nil
}

// GetPivotIndex retrieves the pivot index from NATS KV.
func (s *NATSStructuralIndexStorage) GetPivotIndex(ctx context.Context) (*PivotIndex, error) {
	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		return s.testStore.pivot, nil
	}

	if s.kv == nil {
		return nil, nil
	}

	// Get metadata
	metaEntry, err := s.kv.Get(ctx, pivotMetaKey())
	if err != nil {
		if stderrors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, errs.WrapTransient(err, "NATSStructuralIndexStorage", "GetPivotIndex", "get meta")
	}

	var meta pivotMeta
	if err := json.Unmarshal(metaEntry.Value(), &meta); err != nil {
		return nil, errs.WrapInvalid(err, "NATSStructuralIndexStorage", "GetPivotIndex", "unmarshal meta")
	}

	// Parse computed time
	computedAt, err := parseTime(meta.ComputedAt)
	if err != nil {
		return nil, errs.WrapInvalid(err, "NATSStructuralIndexStorage", "GetPivotIndex", "parse computed_at")
	}

	// Load distance vectors using server-side prefix filtering
	distanceVectors := make(map[string][]int)
	const pivotEntityPrefix = "structural.pivot.entity."
	keys, err := natsclient.FilteredKeys(ctx, s.kv, pivotEntityPrefix+">")
	if err != nil {
		return nil, errs.WrapTransient(err, "NATSStructuralIndexStorage", "GetPivotIndex", "list keys")
	}

	for _, key := range keys {
		entry, err := s.kv.Get(ctx, key)
		if err != nil {
			continue // Skip errors for individual entries
		}

		var distances []int
		if err := json.Unmarshal(entry.Value(), &distances); err != nil {
			continue
		}

		entityID := strings.TrimPrefix(key, pivotEntityPrefix)
		distanceVectors[entityID] = distances
	}

	return &PivotIndex{
		Pivots:          meta.Pivots,
		DistanceVectors: distanceVectors,
		ComputedAt:      computedAt,
		EntityCount:     meta.EntityCount,
	}, nil
}

// Clear removes all structural index data from NATS KV.
func (s *NATSStructuralIndexStorage) Clear(ctx context.Context) error {
	// Clear test store if using it
	if s.kv == nil && s.testStore != nil {
		s.testStore.kcore = nil
		s.testStore.pivot = nil
		return nil
	}

	if s.kv == nil {
		return nil
	}

	// Use server-side prefix filtering to find only structural keys
	keys, err := natsclient.FilteredKeys(ctx, s.kv, "structural.>")
	if err != nil {
		return errs.WrapTransient(err, "NATSStructuralIndexStorage", "Clear", "list keys")
	}

	var deleteErrs []error
	for _, key := range keys {
		if err := s.kv.Delete(ctx, key); err != nil {
			deleteErrs = append(deleteErrs, fmt.Errorf("failed to delete %s: %w", key, err))
		}
	}

	if len(deleteErrs) > 0 {
		return errs.WrapTransient(
			fmt.Errorf("%d deletion errors: %v", len(deleteErrs), deleteErrs),
			"NATSStructuralIndexStorage",
			"Clear",
			"partial clear failure",
		)
	}

	return nil
}

// Key generation helpers

func kcoreMetaKey() string {
	return "structural.kcore._meta"
}

func kcoreEntityKey(entityID string) string {
	return fmt.Sprintf("structural.kcore.entity.%s", entityID)
}

func kcoreBucketKey(core int) string {
	return fmt.Sprintf("structural.kcore.bucket.%d", core)
}

func pivotMetaKey() string {
	return "structural.pivot._meta"
}

func pivotEntityKey(entityID string) string {
	return fmt.Sprintf("structural.pivot.entity.%s", entityID)
}

// parseCoreFromBucketKey extracts the core number from a bucket key.
func parseCoreFromBucketKey(key string) int {
	// Key format: structural.kcore.bucket.{core}
	parts := strings.Split(key, ".")
	if len(parts) < 4 {
		return 0
	}
	var core int
	fmt.Sscanf(parts[3], "%d", &core)
	return core
}

// parseTime parses an RFC3339 formatted time string.
func parseTime(s string) (time.Time, error) {
	return time.Parse("2006-01-02T15:04:05Z07:00", s)
}
