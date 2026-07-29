package inference

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// Key patterns (in ANOMALY_INDEX — see graph.BucketAnomalyIndex; the bucket
// handle is injected by the owner, graph-clustering):
// - anomaly.{id}                     - Full anomaly JSON
// - anomaly.idx.status.{status}.{id} - Status index (value is empty)
// - anomaly.idx.type.{type}.{id}     - Type index (value is empty)

// ErrConcurrentModification is returned when an optimistic lock check fails.
var ErrConcurrentModification = stderrors.New("concurrent modification detected")

// Storage defines the interface for persisting structural anomalies.
type Storage interface {
	// Save persists an anomaly (creates or updates).
	Save(ctx context.Context, anomaly *StructuralAnomaly) error

	// SaveWithRevision persists an anomaly with optimistic locking.
	// Returns ErrConcurrentModification if the revision has changed.
	// Pass revision=0 for new anomalies.
	SaveWithRevision(ctx context.Context, anomaly *StructuralAnomaly, revision uint64) error

	// GetWithRevision retrieves an anomaly by ID along with its KV revision.
	// The revision can be used with SaveWithRevision for optimistic locking.
	GetWithRevision(ctx context.Context, id string) (*StructuralAnomaly, uint64, error)

	// Get retrieves an anomaly by ID.
	Get(ctx context.Context, id string) (*StructuralAnomaly, error)

	// GetByStatus retrieves all anomalies with the given status.
	GetByStatus(ctx context.Context, status AnomalyStatus) ([]*StructuralAnomaly, error)

	// GetByType retrieves all anomalies of the given type.
	GetByType(ctx context.Context, anomalyType AnomalyType) ([]*StructuralAnomaly, error)

	// UpdateStatus updates an anomaly's status and optional review info.
	UpdateStatus(ctx context.Context, id string, status AnomalyStatus, reviewedBy, notes string) error

	// Delete removes an anomaly.
	Delete(ctx context.Context, id string) error

	// Watch returns a channel of anomalies as they're created/updated.
	// Used by ReviewWorker to process new pending anomalies.
	Watch(ctx context.Context) (<-chan *StructuralAnomaly, error)

	// Cleanup removes old resolved anomalies (applied/rejected) older than retention.
	// Returns the count of deleted anomalies.
	Cleanup(ctx context.Context, retention time.Duration) (int, error)

	// Count returns the total number of anomalies by status.
	Count(ctx context.Context) (map[AnomalyStatus]int, error)

	// IsDismissedPair checks if an entity pair is already tracked (any status including pending).
	// This prevents re-detecting the same semantic gap repeatedly across detection runs.
	IsDismissedPair(ctx context.Context, entityA, entityB string) (bool, error)

	// HasEntityAnomaly checks if an anomaly already exists for an entity+type combination.
	// This prevents re-detecting the same core isolation/demotion across detection runs.
	HasEntityAnomaly(ctx context.Context, entityID string, anomalyType AnomalyType) (bool, error)
}

// NATSAnomalyStorage implements Storage using NATS KV.
type NATSAnomalyStorage struct {
	kv        jetstream.KeyValue
	logger    *slog.Logger
	testStore *testAnomalyStore // In-memory store for testing when kv is nil
}

// testAnomalyStore provides in-memory storage for unit testing.
// Thread-safe via mutex for concurrent detector testing.
type testAnomalyStore struct {
	mu        sync.RWMutex
	anomalies map[string]*StructuralAnomaly
}

// NewNATSAnomalyStorage creates a new NATS-backed anomaly storage.
func NewNATSAnomalyStorage(kv jetstream.KeyValue, logger *slog.Logger) *NATSAnomalyStorage {
	storage := &NATSAnomalyStorage{
		kv:     kv,
		logger: logger,
	}

	// Initialize in-memory test store if KV is nil
	if kv == nil {
		storage.testStore = &testAnomalyStore{
			anomalies: make(map[string]*StructuralAnomaly),
		}
	}

	return storage
}

// Save persists an anomaly to NATS KV.
func (s *NATSAnomalyStorage) Save(ctx context.Context, anomaly *StructuralAnomaly) error {
	// Delegate to SaveWithRevision with revision=0 (no optimistic locking)
	return s.SaveWithRevision(ctx, anomaly, 0)
}

// SaveWithRevision persists an anomaly with optimistic locking.
// If revision > 0, the save will fail if the KV entry has been modified.
func (s *NATSAnomalyStorage) SaveWithRevision(ctx context.Context, anomaly *StructuralAnomaly, revision uint64) error {
	if anomaly == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSAnomalyStorage", "SaveWithRevision", "anomaly is nil")
	}

	if anomaly.ID == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSAnomalyStorage", "SaveWithRevision", "anomaly ID is empty")
	}

	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		s.testStore.mu.Lock()
		defer s.testStore.mu.Unlock()
		// For test store, revision check is based on existence
		if revision > 0 {
			existing, exists := s.testStore.anomalies[anomaly.ID]
			if !exists || existing.Status != StatusPending {
				return ErrConcurrentModification
			}
		}
		s.testStore.anomalies[anomaly.ID] = anomaly
		return nil
	}

	if s.kv == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSAnomalyStorage", "SaveWithRevision", "kv is nil")
	}

	// Check if this is an update (need to clean up old status index)
	existingAnomaly, _, err := s.GetWithRevision(ctx, anomaly.ID)
	if err != nil {
		return errs.WrapTransient(err, "NATSAnomalyStorage", "SaveWithRevision", "check existing")
	}

	// Store the anomaly
	data, err := json.Marshal(anomaly)
	if err != nil {
		return errs.WrapInvalid(err, "NATSAnomalyStorage", "SaveWithRevision", "marshal anomaly")
	}

	// Use optimistic locking if revision provided
	if revision > 0 {
		_, err = s.kv.Update(ctx, anomalyKey(anomaly.ID), data, revision)
		if err != nil {
			if stderrors.Is(err, jetstream.ErrKeyExists) {
				return ErrConcurrentModification
			}
			return errs.WrapTransient(err, "NATSAnomalyStorage", "SaveWithRevision", "update anomaly with revision")
		}
	} else {
		if _, err := s.kv.Put(ctx, anomalyKey(anomaly.ID), data); err != nil {
			return errs.WrapTransient(err, "NATSAnomalyStorage", "SaveWithRevision", "put anomaly")
		}
	}

	// Update status index
	if existingAnomaly != nil && existingAnomaly.Status != anomaly.Status {
		// Remove old status index entry
		oldStatusKey := statusIndexKey(existingAnomaly.Status, anomaly.ID)
		_ = s.kv.Delete(ctx, oldStatusKey) // Ignore errors on cleanup
	}

	// Add new status index entry
	statusKey := statusIndexKey(anomaly.Status, anomaly.ID)
	if _, err := s.kv.Put(ctx, statusKey, []byte{}); err != nil {
		return errs.WrapTransient(err, "NATSAnomalyStorage", "SaveWithRevision", "put status index")
	}

	// Update type index (only on create)
	if existingAnomaly == nil {
		typeKey := typeIndexKey(anomaly.Type, anomaly.ID)
		if _, err := s.kv.Put(ctx, typeKey, []byte{}); err != nil {
			return errs.WrapTransient(err, "NATSAnomalyStorage", "SaveWithRevision", "put type index")
		}

		// For semantic gap anomalies, mark the pair as tracked to prevent re-detection
		// This is done on create only (existingAnomaly == nil)
		if anomaly.Type == AnomalySemanticStructuralGap && anomaly.EntityA != "" && anomaly.EntityB != "" {
			pairKey := makeDismissedPairKey(anomaly.EntityA, anomaly.EntityB)
			if _, err := s.kv.Put(ctx, dismissedPairKey(pairKey), []byte{}); err != nil {
				// Log but don't fail - this is an optimization
				s.logger.Warn("failed to mark pair as tracked",
					"entity_a", anomaly.EntityA,
					"entity_b", anomaly.EntityB,
					"error", err)
			}
		}

		// For core isolation/demotion anomalies, mark the entity as tracked
		if (anomaly.Type == AnomalyCoreIsolation || anomaly.Type == AnomalyCoreDemotion) && anomaly.EntityA != "" {
			entityKey := entityAnomalyKey(anomaly.Type, anomaly.EntityA)
			if _, err := s.kv.Put(ctx, entityKey, []byte{}); err != nil {
				// Log but don't fail - this is an optimization
				s.logger.Warn("failed to mark entity as tracked",
					"entity", anomaly.EntityA,
					"type", anomaly.Type,
					"error", err)
			}
		}
	}

	return nil
}

// Get retrieves an anomaly by ID.
func (s *NATSAnomalyStorage) Get(ctx context.Context, id string) (*StructuralAnomaly, error) {
	anomaly, _, err := s.GetWithRevision(ctx, id)
	return anomaly, err
}

// GetWithRevision retrieves an anomaly by ID along with its KV revision.
func (s *NATSAnomalyStorage) GetWithRevision(ctx context.Context, id string) (*StructuralAnomaly, uint64, error) {
	if id == "" {
		return nil, 0, errs.WrapInvalid(errs.ErrMissingConfig, "NATSAnomalyStorage", "GetWithRevision", "id is empty")
	}

	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		s.testStore.mu.RLock()
		anomaly, exists := s.testStore.anomalies[id]
		s.testStore.mu.RUnlock()
		if !exists {
			return nil, 0, nil
		}
		// Return revision 1 for test store (simulates existing entry)
		return anomaly, 1, nil
	}

	if s.kv == nil {
		return nil, 0, nil
	}

	entry, err := s.kv.Get(ctx, anomalyKey(id))
	if err != nil {
		if stderrors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, 0, nil
		}
		return nil, 0, errs.WrapTransient(err, "NATSAnomalyStorage", "GetWithRevision", "get anomaly")
	}

	var anomaly StructuralAnomaly
	if err := json.Unmarshal(entry.Value(), &anomaly); err != nil {
		return nil, 0, errs.WrapInvalid(err, "NATSAnomalyStorage", "GetWithRevision", "unmarshal anomaly")
	}

	return &anomaly, entry.Revision(), nil
}

// GetByStatus retrieves all anomalies with the given status.
func (s *NATSAnomalyStorage) GetByStatus(ctx context.Context, status AnomalyStatus) ([]*StructuralAnomaly, error) {
	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		s.testStore.mu.RLock()
		defer s.testStore.mu.RUnlock()
		var result []*StructuralAnomaly
		for _, a := range s.testStore.anomalies {
			if a.Status == status {
				result = append(result, a)
			}
		}
		return result, nil
	}

	if s.kv == nil {
		return nil, nil
	}

	return s.getByIndexPrefix(ctx, statusIndexPrefix(status))
}

// GetByType retrieves all anomalies of the given type.
func (s *NATSAnomalyStorage) GetByType(ctx context.Context, anomalyType AnomalyType) ([]*StructuralAnomaly, error) {
	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		s.testStore.mu.RLock()
		defer s.testStore.mu.RUnlock()
		var result []*StructuralAnomaly
		for _, a := range s.testStore.anomalies {
			if a.Type == anomalyType {
				result = append(result, a)
			}
		}
		return result, nil
	}

	if s.kv == nil {
		return nil, nil
	}

	return s.getByIndexPrefix(ctx, typeIndexPrefix(anomalyType))
}

// getByIndexPrefix retrieves anomalies by scanning an index prefix.
func (s *NATSAnomalyStorage) getByIndexPrefix(ctx context.Context, prefix string) ([]*StructuralAnomaly, error) {
	// Use server-side prefix filtering instead of loading all keys
	keys, err := natsclient.FilteredKeys(ctx, s.kv, prefix+">")
	if err != nil {
		return nil, errs.WrapTransient(err, "NATSAnomalyStorage", "getByIndexPrefix", "list keys")
	}

	var result []*StructuralAnomaly
	for _, key := range keys {
		// Extract anomaly ID from index key
		id := extractIDFromIndexKey(key)
		if id == "" {
			continue
		}

		anomaly, err := s.Get(ctx, id)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to get anomaly from index",
					"key", key, "id", id, "error", err)
			}
			continue
		}
		if anomaly != nil {
			result = append(result, anomaly)
		}
	}

	return result, nil
}

// UpdateStatus updates an anomaly's status and optional review info.
func (s *NATSAnomalyStorage) UpdateStatus(
	ctx context.Context,
	id string,
	status AnomalyStatus,
	reviewedBy, notes string,
) error {
	anomaly, err := s.Get(ctx, id)
	if err != nil {
		return errs.WrapTransient(err, "NATSAnomalyStorage", "UpdateStatus", "get anomaly")
	}
	if anomaly == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSAnomalyStorage", "UpdateStatus",
			fmt.Sprintf("anomaly not found: %s", id))
	}

	// Update fields
	anomaly.Status = status
	if reviewedBy != "" {
		anomaly.ReviewedBy = reviewedBy
		now := time.Now()
		anomaly.ReviewedAt = &now
	}
	if notes != "" {
		anomaly.ReviewNotes = notes
	}

	return s.Save(ctx, anomaly)
}

// Delete removes an anomaly.
func (s *NATSAnomalyStorage) Delete(ctx context.Context, id string) error {
	if id == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "NATSAnomalyStorage", "Delete", "id is empty")
	}

	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		s.testStore.mu.Lock()
		delete(s.testStore.anomalies, id)
		s.testStore.mu.Unlock()
		return nil
	}

	if s.kv == nil {
		return nil
	}

	// Get anomaly first to clean up indexes
	anomaly, err := s.Get(ctx, id)
	if err != nil {
		return errs.WrapTransient(err, "NATSAnomalyStorage", "Delete", "get anomaly")
	}
	if anomaly == nil {
		return nil // Already deleted
	}

	// Delete status index
	statusKey := statusIndexKey(anomaly.Status, id)
	_ = s.kv.Delete(ctx, statusKey) // Ignore errors

	// Delete type index
	typeKey := typeIndexKey(anomaly.Type, id)
	_ = s.kv.Delete(ctx, typeKey) // Ignore errors

	// Delete anomaly
	if err := s.kv.Delete(ctx, anomalyKey(id)); err != nil {
		if !stderrors.Is(err, jetstream.ErrKeyNotFound) {
			return errs.WrapTransient(err, "NATSAnomalyStorage", "Delete", "delete anomaly")
		}
	}

	return nil
}

// Watch returns a channel of anomalies as they're created/updated.
func (s *NATSAnomalyStorage) Watch(ctx context.Context) (<-chan *StructuralAnomaly, error) {
	if s.kv == nil {
		// Return closed channel for test store
		ch := make(chan *StructuralAnomaly)
		close(ch)
		return ch, nil
	}

	watcher, err := s.kv.WatchAll(ctx)
	if err != nil {
		return nil, errs.WrapTransient(err, "NATSAnomalyStorage", "Watch", "create watcher")
	}

	ch := make(chan *StructuralAnomaly, 100)

	go func() {
		defer close(ch)
		defer watcher.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case entry, ok := <-watcher.Updates():
				if !ok {
					return
				}
				if entry == nil {
					continue // Initial nil after catching up
				}

				// Only process anomaly entries (not indexes)
				key := entry.Key()
				if !strings.HasPrefix(key, "anomaly.") || strings.HasPrefix(key, "anomaly.idx.") {
					continue
				}

				// Skip deletions
				if entry.Operation() == jetstream.KeyValueDelete {
					continue
				}

				var anomaly StructuralAnomaly
				if err := json.Unmarshal(entry.Value(), &anomaly); err != nil {
					if s.logger != nil {
						s.logger.Warn("failed to unmarshal watched anomaly",
							"key", key, "error", err)
					}
					continue
				}

				select {
				case ch <- &anomaly:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}

// Cleanup removes old resolved anomalies older than retention.
func (s *NATSAnomalyStorage) Cleanup(ctx context.Context, retention time.Duration) (int, error) {
	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		cutoff := time.Now().Add(-retention)
		deleted := 0
		s.testStore.mu.Lock()
		for id, a := range s.testStore.anomalies {
			if a.IsResolved() && a.DetectedAt.Before(cutoff) {
				delete(s.testStore.anomalies, id)
				deleted++
			}
		}
		s.testStore.mu.Unlock()
		return deleted, nil
	}

	if s.kv == nil {
		return 0, nil
	}

	cutoff := time.Now().Add(-retention)
	deleted := 0

	// Get all anomalies with resolved statuses
	resolvedStatuses := []AnomalyStatus{StatusApplied, StatusRejected, StatusLLMRejected}

	for _, status := range resolvedStatuses {
		anomalies, err := s.GetByStatus(ctx, status)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn("failed to get anomalies for cleanup",
					"status", status, "error", err)
			}
			continue
		}

		for _, anomaly := range anomalies {
			if anomaly.DetectedAt.Before(cutoff) {
				if err := s.Delete(ctx, anomaly.ID); err != nil {
					if s.logger != nil {
						s.logger.Warn("failed to delete anomaly during cleanup",
							"id", anomaly.ID, "error", err)
					}
					continue
				}
				deleted++
			}
		}
	}

	return deleted, nil
}

// Count returns the total number of anomalies by status.
func (s *NATSAnomalyStorage) Count(ctx context.Context) (map[AnomalyStatus]int, error) {
	counts := make(map[AnomalyStatus]int)

	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		s.testStore.mu.RLock()
		for _, a := range s.testStore.anomalies {
			counts[a.Status]++
		}
		s.testStore.mu.RUnlock()
		return counts, nil
	}

	if s.kv == nil {
		return counts, nil
	}

	// Count status index entries using per-status server-side filtering
	statuses := []AnomalyStatus{
		StatusPending, StatusLLMReviewing, StatusLLMApproved, StatusLLMRejected,
		StatusHumanReview, StatusApproved, StatusRejected, StatusApplied,
	}

	for _, status := range statuses {
		prefix := statusIndexPrefix(status)
		keys, err := natsclient.FilteredKeys(ctx, s.kv, prefix+">")
		if err != nil {
			return nil, errs.WrapTransient(err, "NATSAnomalyStorage", "Count", "list keys for status "+string(status))
		}
		counts[status] = len(keys)
	}

	return counts, nil
}

// IsDismissedPair checks if an entity pair is already tracked (any status including pending).
// This prevents re-detecting the same semantic gap repeatedly across detection runs.
func (s *NATSAnomalyStorage) IsDismissedPair(ctx context.Context, entityA, entityB string) (bool, error) {
	// Create canonical pair key (order-independent)
	pairKey := makeDismissedPairKey(entityA, entityB)

	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		s.testStore.mu.RLock()
		defer s.testStore.mu.RUnlock()
		// Check all anomalies for this pair - skip if already tracked (any status)
		for _, a := range s.testStore.anomalies {
			if a.Type != AnomalySemanticStructuralGap {
				continue
			}
			existingPairKey := makeDismissedPairKey(a.EntityA, a.EntityB)
			if existingPairKey == pairKey {
				// Skip any existing anomaly for this pair to prevent re-detection
				// This includes pending anomalies, not just resolved ones
				if a.Status == StatusPending || a.Status == StatusHumanReview ||
					a.Status == StatusDismissed || a.Status == StatusAutoApplied ||
					a.Status == StatusApplied || a.Status == StatusRejected {
					return true, nil
				}
			}
		}
		return false, nil
	}

	if s.kv == nil {
		return false, nil
	}

	// Check the pair index - this is populated for ALL anomalies (pending and resolved)
	// via markPairTracked() called from SaveWithRevision()
	_, err := s.kv.Get(ctx, dismissedPairKey(pairKey))
	if err == nil {
		return true, nil
	}
	if stderrors.Is(err, jetstream.ErrKeyNotFound) {
		return false, nil
	}

	return false, errs.WrapTransient(err, "NATSAnomalyStorage", "IsDismissedPair", "check pair index")
}

// HasEntityAnomaly checks if an anomaly already exists for an entity+type combination.
// This prevents re-detecting the same core isolation/demotion across detection runs.
func (s *NATSAnomalyStorage) HasEntityAnomaly(ctx context.Context, entityID string, anomalyType AnomalyType) (bool, error) {
	// Use test store if KV is nil
	if s.kv == nil && s.testStore != nil {
		s.testStore.mu.RLock()
		defer s.testStore.mu.RUnlock()
		for _, a := range s.testStore.anomalies {
			if a.Type == anomalyType && a.EntityA == entityID {
				return true, nil
			}
		}
		return false, nil
	}

	if s.kv == nil {
		return false, nil
	}

	// Check the entity index
	key := entityAnomalyKey(anomalyType, entityID)
	_, err := s.kv.Get(ctx, key)
	if err == nil {
		return true, nil
	}
	if stderrors.Is(err, jetstream.ErrKeyNotFound) {
		return false, nil
	}

	return false, errs.WrapTransient(err, "NATSAnomalyStorage", "HasEntityAnomaly", "check entity index")
}

// MarkPairDismissed creates an index entry to prevent future re-detection.
// Called when an anomaly is dismissed, rejected, or auto-applied.
func (s *NATSAnomalyStorage) MarkPairDismissed(ctx context.Context, entityA, entityB string) error {
	pairKey := makeDismissedPairKey(entityA, entityB)

	// Use test store if KV is nil - no persistent index needed
	if s.kv == nil {
		return nil
	}

	// Create dismissed pair index entry
	_, err := s.kv.Put(ctx, dismissedPairKey(pairKey), []byte{})
	if err != nil {
		return errs.WrapTransient(err, "NATSAnomalyStorage", "MarkPairDismissed", "put index")
	}
	return nil
}

// makeDismissedPairKey creates a canonical key for an entity pair (order-independent).
// Uses "::" as separator for internal representation.
func makeDismissedPairKey(a, b string) string {
	if a < b {
		return a + "::" + b
	}
	return b + "::" + a
}

// dismissedPairKey creates a NATS KV key for the dismissed pair index.
// Uses SHA256 hash to avoid issues with dots in entity IDs (hierarchical notation).
// Only point lookups are performed on this index, so hashing is safe.
func dismissedPairKey(pairKey string) string {
	hash := sha256.Sum256([]byte(pairKey))
	return fmt.Sprintf("anomaly.idx.dismissed.%s", hex.EncodeToString(hash[:16]))
}

// Key generation helpers

func anomalyKey(id string) string {
	return fmt.Sprintf("anomaly.%s", id)
}

func statusIndexKey(status AnomalyStatus, id string) string {
	return fmt.Sprintf("anomaly.idx.status.%s.%s", status, id)
}

func statusIndexPrefix(status AnomalyStatus) string {
	return fmt.Sprintf("anomaly.idx.status.%s.", status)
}

func typeIndexKey(anomalyType AnomalyType, id string) string {
	return fmt.Sprintf("anomaly.idx.type.%s.%s", anomalyType, id)
}

func typeIndexPrefix(anomalyType AnomalyType) string {
	return fmt.Sprintf("anomaly.idx.type.%s.", anomalyType)
}

// entityAnomalyKey creates a NATS KV key for the entity anomaly index.
// Uses SHA256 hash to avoid issues with dots in entity IDs (hierarchical notation).
func entityAnomalyKey(anomalyType AnomalyType, entityID string) string {
	key := string(anomalyType) + "::" + entityID
	hash := sha256.Sum256([]byte(key))
	return fmt.Sprintf("anomaly.idx.entity.%s", hex.EncodeToString(hash[:16]))
}

// extractIDFromIndexKey extracts the anomaly ID from an index key.
// Index keys are formatted as: anomaly.idx.{index_type}.{value}.{id}
func extractIDFromIndexKey(key string) string {
	parts := strings.Split(key, ".")
	if len(parts) < 5 {
		return ""
	}
	return parts[len(parts)-1]
}
