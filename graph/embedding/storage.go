package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360studio/semstreams/pkg/errs"
)

const (
	// EmbeddingIndexBucket stores entity embeddings with metadata
	EmbeddingIndexBucket = "EMBEDDING_INDEX"

	// EmbeddingDedupBucket stores content-addressed embeddings for deduplication
	EmbeddingDedupBucket = "EMBEDDING_DEDUP"
)

// Status represents the processing status of an embedding
type Status string

const (
	// StatusPending awaits generation
	StatusPending Status = "pending"
	// StatusGenerated is successfully generated
	StatusGenerated Status = "generated"
	// StatusFailed indicates generation failed
	StatusFailed Status = "failed"
)

// Record represents a stored embedding with metadata
type Record struct {
	EntityID    string    `json:"entity_id"`
	Vector      []float32 `json:"vector,omitempty"`
	ContentHash string    `json:"content_hash"`
	SourceText  string    `json:"source_text,omitempty"` // Stored for pending records (legacy)
	Model       string    `json:"model,omitempty"`
	Dimensions  int       `json:"dimensions,omitempty"`
	GeneratedAt time.Time `json:"generated_at,omitempty"`
	Status      Status    `json:"status"`
	ErrorMsg    string    `json:"error_msg,omitempty"` // If status=failed

	// SourceRevision is the ENTITY_STATES stream revision that produced this pending
	// record. It is threaded from the hop-1 watcher so hop-2 can complete the
	// embedding readiness watermark at the terminal transition (ADR-066 §3). Only
	// meaningful on pending records; SaveGenerated/SaveFailed rebuild the record and
	// drop it (those records only ever hit hop-2's not-pending skip). 0 means
	// "unknown" (a legacy record written before this field existed) — the watermark
	// completion treats 0 as a no-op.
	SourceRevision uint64 `json:"source_revision,omitempty"`

	// ContentStorable support (Feature 008)
	// When StorageRef is set, Worker fetches content from ObjectStore
	// and uses ContentFields to extract text for embedding.
	StorageRef    *StorageRef       `json:"storage_ref,omitempty"`
	ContentFields map[string]string `json:"content_fields,omitempty"` // Role → field name
}

// StorageRef is a simplified reference for embedding storage.
// Mirrors message.StorageReference structure.
type StorageRef struct {
	StorageInstance string `json:"storage_instance"`
	Key             string `json:"key"`
}

// DedupRecord stores content-addressed embeddings for deduplication
type DedupRecord struct {
	Vector         []float32 `json:"vector"`
	EntityIDs      []string  `json:"entity_ids"` // Entities sharing this content
	FirstGenerated time.Time `json:"first_generated"`
}

// ScoredEntity pairs an entity ID with its cosine similarity score.
// Returned by FindSimilarFromCache for zero-KV similarity queries.
type ScoredEntity struct {
	EntityID   string
	Similarity float64
}

// Storage handles persistence of embeddings to NATS KV buckets.
// It also maintains an in-memory vector cache, kept current via a
// KV watcher on the index bucket, to serve similarity queries without
// any network round-trips.
type Storage struct {
	indexBucket jetstream.KeyValue // EMBEDDING_INDEX
	dedupBucket jetstream.KeyValue // EMBEDDING_DEDUP

	// vectorCache is populated and maintained by StartVectorCache.
	// Only StatusGenerated entries with non-empty vectors are stored.
	vectorCache   map[string][]float32
	vectorCacheMu sync.RWMutex
	cacheReady    chan struct{} // closed once initial watcher sync completes
	cacheStarted  bool
	// cacheWatchHealthy is true only while the WatchAll stream that populated
	// vectorCache is intact and every observed record was decodable. Once the
	// watcher is lost or a record cannot be decoded, the cache is permanently
	// non-authoritative for this Storage lifetime and callers fall back to KV.
	cacheWatchHealthy bool // guarded by vectorCacheMu
}

// NewStorage creates a new embedding storage instance
func NewStorage(indexBucket, dedupBucket jetstream.KeyValue) *Storage {
	return &Storage{
		indexBucket: indexBucket,
		dedupBucket: dedupBucket,
		vectorCache: make(map[string][]float32),
		cacheReady:  make(chan struct{}),
	}
}

// SavePending saves a pending embedding request with source text (legacy mode).
// sourceRevision is the ENTITY_STATES revision that produced this record (ADR-066
// §3 readiness watermark); pass 0 when unknown.
func (s *Storage) SavePending(ctx context.Context, entityID, contentHash, sourceText string, sourceRevision uint64) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "SavePending", "entity_id is empty")
	}

	record := &Record{
		EntityID:       entityID,
		ContentHash:    contentHash,
		SourceText:     sourceText,
		Status:         StatusPending,
		SourceRevision: sourceRevision,
	}

	data, err := json.Marshal(record)
	if err != nil {
		return errs.WrapInvalid(err, "Storage", "SavePending", "marshal embedding record")
	}

	if _, err := s.indexBucket.Put(ctx, entityID, data); err != nil {
		return errs.WrapTransient(err, "Storage", "SavePending", "put pending embedding")
	}

	return nil
}

// SavePendingWithStorageRef saves a pending embedding request with storage reference.
// This enables the ContentStorable pattern where text is fetched from ObjectStore.
// The contentHash is still used for deduplication if provided.
func (s *Storage) SavePendingWithStorageRef(
	ctx context.Context,
	entityID, contentHash string,
	storageRef *StorageRef,
	contentFields map[string]string,
	sourceRevision uint64,
) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "SavePendingWithStorageRef", "entity_id is empty")
	}
	if storageRef == nil {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "SavePendingWithStorageRef", "storage_ref is nil")
	}

	record := &Record{
		EntityID:       entityID,
		ContentHash:    contentHash,
		StorageRef:     storageRef,
		ContentFields:  contentFields,
		Status:         StatusPending,
		SourceRevision: sourceRevision,
	}

	data, err := json.Marshal(record)
	if err != nil {
		return errs.WrapInvalid(err, "Storage", "SavePendingWithStorageRef", "marshal embedding record")
	}

	if _, err := s.indexBucket.Put(ctx, entityID, data); err != nil {
		return errs.WrapTransient(err, "Storage", "SavePendingWithStorageRef", "put pending embedding")
	}

	return nil
}

// SaveGenerated saves a generated embedding with metadata
func (s *Storage) SaveGenerated(ctx context.Context, entityID string, vector []float32, model string, dimensions int) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "SaveGenerated", "entity_id is empty")
	}

	// Get existing record to preserve content_hash
	existing, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		return errs.WrapTransient(err, "Storage", "SaveGenerated", "get existing record")
	}

	record := &Record{
		EntityID:    entityID,
		Vector:      vector,
		ContentHash: existing.ContentHash, // Preserve from pending record
		Model:       model,
		Dimensions:  dimensions,
		GeneratedAt: time.Now(),
		Status:      StatusGenerated,
	}

	data, err := json.Marshal(record)
	if err != nil {
		return errs.WrapInvalid(err, "Storage", "SaveGenerated", "marshal embedding record")
	}

	if _, err := s.indexBucket.Put(ctx, entityID, data); err != nil {
		return errs.WrapTransient(err, "Storage", "SaveGenerated", "put generated embedding")
	}

	return nil
}

// SaveFailed marks an embedding as failed with error message
func (s *Storage) SaveFailed(ctx context.Context, entityID, errorMsg string) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "SaveFailed", "entity_id is empty")
	}

	// Get existing record to preserve metadata
	existing, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		return errs.WrapTransient(err, "Storage", "SaveFailed", "get existing record")
	}

	existing.Status = StatusFailed
	existing.ErrorMsg = errorMsg

	data, err := json.Marshal(existing)
	if err != nil {
		return errs.WrapInvalid(err, "Storage", "SaveFailed", "marshal embedding record")
	}

	if _, err := s.indexBucket.Put(ctx, entityID, data); err != nil {
		return errs.WrapTransient(err, "Storage", "SaveFailed", "put failed embedding")
	}

	return nil
}

// GetEmbedding retrieves an embedding by entity ID
func (s *Storage) GetEmbedding(ctx context.Context, entityID string) (*Record, error) {
	if entityID == "" {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "GetEmbedding", "entity_id is empty")
	}

	entry, err := s.indexBucket.Get(ctx, entityID)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, nil // Not found is not an error
		}
		return nil, errs.WrapTransient(err, "Storage", "GetEmbedding", "get embedding")
	}

	var record Record
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		return nil, errs.WrapInvalid(err, "Storage", "GetEmbedding", "unmarshal embedding record")
	}

	return &record, nil
}

// GetByContentHash retrieves an embedding by content hash (for deduplication)
func (s *Storage) GetByContentHash(ctx context.Context, contentHash string) (*DedupRecord, error) {
	if contentHash == "" {
		return nil, errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "GetByContentHash", "content_hash is empty")
	}

	entry, err := s.dedupBucket.Get(ctx, contentHash)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, nil // Not found is not an error
		}
		return nil, errs.WrapTransient(err, "Storage", "GetByContentHash", "get dedup record")
	}

	var record DedupRecord
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		return nil, errs.WrapInvalid(err, "Storage", "GetByContentHash", "unmarshal dedup record")
	}

	return &record, nil
}

// SaveDedup saves a content-addressed embedding for deduplication
func (s *Storage) SaveDedup(ctx context.Context, contentHash string, vector []float32, entityID string) error {
	if contentHash == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "SaveDedup", "content_hash is empty")
	}

	// Check if dedup record exists
	existing, err := s.GetByContentHash(ctx, contentHash)
	if err != nil {
		return err
	}

	var record *DedupRecord
	if existing != nil {
		// Add entity to existing list
		record = existing
		record.EntityIDs = append(record.EntityIDs, entityID)
	} else {
		// Create new dedup record
		record = &DedupRecord{
			Vector:         vector,
			EntityIDs:      []string{entityID},
			FirstGenerated: time.Now(),
		}
	}

	data, err := json.Marshal(record)
	if err != nil {
		return errs.WrapInvalid(err, "Storage", "SaveDedup", "marshal dedup record")
	}

	if _, err := s.dedupBucket.Put(ctx, contentHash, data); err != nil {
		return errs.WrapTransient(err, "Storage", "SaveDedup", "put dedup record")
	}

	return nil
}

// DeleteEmbedding removes an embedding record
func (s *Storage) DeleteEmbedding(ctx context.Context, entityID string) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "DeleteEmbedding", "entity_id is empty")
	}

	if err := s.indexBucket.Delete(ctx, entityID); err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil // Already deleted
		}
		return errs.WrapTransient(err, "Storage", "DeleteEmbedding", "delete embedding")
	}

	return nil
}

// ListGeneratedEntityIDs returns all entity IDs that have embeddings in storage.
// This is used for pre-warming the vector cache on startup.
func (s *Storage) ListGeneratedEntityIDs(ctx context.Context) ([]string, error) {
	keys, err := s.indexBucket.ListKeys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) || errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, nil
		}
		return nil, errs.WrapTransient(err, "Storage", "ListGeneratedEntityIDs", "list keys")
	}
	defer func() { _ = keys.Stop() }()

	var entityIDs []string
	keyUpdates := keys.Keys()
	for {
		select {
		case <-ctx.Done():
			return nil, errs.WrapTransient(ctx.Err(), "Storage", "ListGeneratedEntityIDs", "list keys")
		case key, ok := <-keyUpdates:
			if !ok {
				if err := ctx.Err(); err != nil {
					return nil, errs.WrapTransient(err, "Storage", "ListGeneratedEntityIDs", "list keys")
				}
				return entityIDs, nil
			}
			entityIDs = append(entityIDs, key)
		}
	}
}

// StartVectorCache launches a goroutine that keeps the in-memory vector cache
// synchronised with the EMBEDDING_INDEX KV bucket via WatchAll.
//
// The goroutine runs until ctx is cancelled. It is safe to call only once; a
// second call is a no-op. cacheReady is closed after the initial snapshot has
// been applied (nil delimiter received), so FindSimilarFromCache will not
// return results until the cache is warm.
func (s *Storage) StartVectorCache(ctx context.Context) error {
	s.vectorCacheMu.Lock()
	if s.cacheStarted {
		s.vectorCacheMu.Unlock()
		return nil
	}
	s.cacheStarted = true
	s.vectorCacheMu.Unlock()

	watcher, err := s.indexBucket.WatchAll(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil
		}
		return errs.WrapTransient(err, "Storage", "StartVectorCache", "watch index bucket")
	}
	s.setCacheWatchHealthy(true)

	go func() {
		// NOTE: explicit watcher.Stop() before each return avoids the nats.go
		// race between Stop() and the internal message-handler goroutine.
		initialSyncDone := false

		for {
			select {
			case <-ctx.Done():
				s.invalidateVectorCache()
				watcher.Stop()
				return
			case entry, ok := <-watcher.Updates():
				if !ok {
					s.invalidateVectorCache()
					watcher.Stop()
					return
				}

				// nil entry is the initial-sync delimiter.
				if entry == nil {
					if !initialSyncDone {
						initialSyncDone = true
						close(s.cacheReady)
					}
					continue
				}

				entityID := entry.Key()

				if entry.Operation() == jetstream.KeyValueDelete ||
					entry.Operation() == jetstream.KeyValuePurge {
					s.vectorCacheMu.Lock()
					delete(s.vectorCache, entityID)
					s.vectorCacheMu.Unlock()
					continue
				}

				var record Record
				if err := json.Unmarshal(entry.Value(), &record); err != nil {
					// A malformed update may replace a vector already held in the
					// cache. Continuing would make stale memory query-authoritative.
					// Keep consuming for lifecycle hygiene, but permanently force
					// callers onto authoritative KV for this Storage lifetime.
					s.invalidateVectorCache()
					continue
				}

				if record.Status == StatusGenerated && len(record.Vector) > 0 {
					s.vectorCacheMu.Lock()
					s.vectorCache[entityID] = record.Vector
					s.vectorCacheMu.Unlock()
				} else {
					// Record exists but is pending or failed — remove stale vector.
					s.vectorCacheMu.Lock()
					delete(s.vectorCache, entityID)
					s.vectorCacheMu.Unlock()
				}
			}
		}
	}()

	return nil
}

// setCacheWatchHealthy changes cache authority in the same mutex domain as the
// cached vectors. This makes watcher invalidation and a query's health-check +
// vector scan one linearizable operation: invalidation cannot become visible
// between a query observing healthy and acquiring the cache read lock.
func (s *Storage) setCacheWatchHealthy(healthy bool) {
	s.vectorCacheMu.Lock()
	s.cacheWatchHealthy = healthy
	s.vectorCacheMu.Unlock()
}

func (s *Storage) invalidateVectorCache() {
	s.setCacheWatchHealthy(false)
}

// FindSimilarFromCache scans the in-memory vector cache for entities whose
// cosine similarity to queryVector is highest, excluding the entity identified
// by excludeID (pass "" to skip exclusion).
//
// keep, when non-nil, is a candidate predicate applied BEFORE cosine similarity:
// only entity IDs for which keep returns true are scored. Pass nil to score
// every cached entity (no filter). This is how a scoped semantic search
// (ADR-071) constrains candidates at the source on the warm path — the caller
// builds keep from the requested ID prefixes so filtering happens before the
// expensive cosine, and identically to the cold KV-scan fallback.
//
// The second return value reports whether the cache was ready (warm) and its
// maintaining watcher was still healthy at the time of the call. Callers must
// fall back to authoritative KV when it is false.
func (s *Storage) FindSimilarFromCache(excludeID string, queryVector []float32, keep func(string) bool, limit int) ([]ScoredEntity, bool) {
	// Non-blocking check: is the initial sync complete?
	select {
	case <-s.cacheReady:
	default:
		return nil, false
	}
	s.vectorCacheMu.RLock()
	defer s.vectorCacheMu.RUnlock()
	if !s.cacheWatchHealthy {
		return nil, false
	}

	results := make([]ScoredEntity, 0, len(s.vectorCache))
	for entityID, vector := range s.vectorCache {
		if entityID == excludeID {
			continue
		}
		if keep != nil && !keep(entityID) {
			continue
		}
		sim := CosineSimilarity(queryVector, vector)
		results = append(results, ScoredEntity{EntityID: entityID, Similarity: sim})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	if len(results) > limit {
		results = results[:limit]
	}

	return results, true
}
