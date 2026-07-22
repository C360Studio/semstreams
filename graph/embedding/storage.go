package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
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

	// maxCASRetries bounds the revision compare-and-set loop on the two save lanes.
	// The SourceRevision ordering guard guarantees a losing writer DROPS at the guard
	// rather than spinning, so the loop converges quickly under same-entity churn; a
	// small ceiling is only insurance against pathological contention, never a stall.
	maxCASRetries = 3
)

// ErrCASExhausted reports that the revision compare-and-set loop on a save lane did
// not converge within maxCASRetries. It is transient (a caller may re-drive), and
// under the SourceRevision ordering guard it should be effectively unreachable.
var ErrCASExhausted = errors.New("embedding index write did not converge under revision CAS")

// ErrRecordGone reports that the EMBEDDING_INDEX record a save was meant to
// UPDATE no longer exists, so the save was dropped without writing.
//
// It is a normal outcome, not a fault: since gh#614 the hop-1 entity tombstone
// deletes an entity's index key from the watcher goroutine while a hop-2 worker
// may still be inside an embedder round trip for that same entity. Callers should
// treat it as "this entity is no longer supposed to have an embedding" and stop —
// in particular they must not report it as a generation failure or fire the
// generated callback, which would push a vector for a dead entity into caches.
var ErrRecordGone = errors.New("embedding index record no longer exists")

// ErrSupersededRevision reports that the save was dropped because a newer source
// revision's outcome already landed for this entity (the ordering guard, #614
// part 2). Like ErrRecordGone it is a normal, non-failure outcome: the caller must
// NOT fire the generated callback, because the vector it holds is the OLDER
// revision's and firing would push a stale vector into any WithOnGenerated
// consumer's cache — the same hazard ErrRecordGone guards against, for the same
// reason. Callers must not report it as a failure either; the newer outcome is
// authoritative.
var ErrSupersededRevision = errors.New("embedding index write superseded by a newer source revision")

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

	// SourceRevision is the ENTITY_STATES stream revision that produced this record.
	// It is threaded from the hop-1 watcher so hop-2 can complete the embedding
	// readiness watermark at the terminal transition (ADR-066 §3), and it is now
	// PERSISTED onto generated/failed records so SaveGenerated/SaveFailed can order
	// concurrent writes by it: a record already carrying a higher SourceRevision has
	// a newer vector, so a late older-revision write is dropped (#614 part 2). 0
	// means "unknown" (a legacy record written before this field existed) — treated
	// as oldest, so any real revision wins the ordering guard, and the watermark
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

// DedupRecord stores content-addressed embeddings for deduplication.
//
// Model and Dimensions record WHICH vector space the stored vector belongs to.
// The dedup KEY already folds in embedder identity (see DedupKey, gh#612), so a
// mismatch here should be unreachable; carrying the fields anyway makes a stale
// record detectable after the fact instead of silently servable — the original
// defect was that a bm25 vector could be returned and re-stamped with a neural
// model's name with nothing in the record to contradict it.
type DedupRecord struct {
	Vector         []float32 `json:"vector"`
	EntityIDs      []string  `json:"entity_ids"` // Entities sharing this content
	FirstGenerated time.Time `json:"first_generated"`
	Model          string    `json:"model,omitempty"`
	Dimensions     int       `json:"dimensions,omitempty"`
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

// SaveGenerated persists a generated embedding under revision compare-and-set,
// ordered by source revision.
//
// contentHash is the hop-2 dedup key of the exact bytes embedded (#623); it is
// STORED as the record's content hash rather than copied from the pending record,
// which since the hop-2 key move carries an empty hash. sourceRevision is the
// ENTITY_STATES revision this generation is completing. ContentHash and Vector are
// both taken from the passed arguments — never from `existing` — so they can never
// desync across revisions (#614 part 2).
//
// The read of `existing` no longer exists to copy a field forward; it exists ONLY
// for the CAS revision and the ordering guard.
func (s *Storage) SaveGenerated(
	ctx context.Context,
	entityID string,
	vector []float32,
	model string,
	dimensions int,
	contentHash string,
	sourceRevision uint64,
) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "SaveGenerated", "entity_id is empty")
	}

	for attempt := 0; attempt < maxCASRetries; attempt++ {
		existing, revision, err := s.getEmbeddingWithRevision(ctx, entityID)
		if err != nil {
			return errs.WrapTransient(err, "Storage", "SaveGenerated", "get existing record")
		}

		// The record vanished while the vector was being generated. DROP the write;
		// do not resurrect the key.
		//
		// Both ways the key can disappear mean "this entity must not have a vector
		// right now": the entity was tombstoned in ENTITY_STATES (hop-1
		// DeleteEmbedding, gh#614), or the no-source-text path deleted its pending
		// record. Writing here would re-create exactly the dangling vector gh#614
		// removes: semantic search keeps returning an entity ID that graph-query can
		// no longer resolve, and nothing deletes it a second time because the tombstone
		// has already fired. Dropping fails in the recoverable direction — if the
		// entity returns, hop-1 writes a fresh pending record and hop-2 regenerates.
		if existing == nil {
			return ErrRecordGone
		}

		// A newer source revision's vector already landed. This is SEMANTIC ORDERING,
		// not lost-update: plain revision CAS would let a stale write win the race if
		// it merely read first, so the guard is on SourceRevision. Drop — the correct
		// outcome, not a failure — but return ErrSupersededRevision, not bare nil, so
		// saveAndNotify can tell a superseded drop from a real write and skip the
		// generated callback (firing it would cache THIS call's older vector).
		// (Legacy records carry SourceRevision 0, treated as oldest, so any real
		// revision wins.)
		if existing.SourceRevision > sourceRevision {
			return ErrSupersededRevision
		}

		record := &Record{
			EntityID:       entityID,
			Vector:         vector,
			ContentHash:    contentHash,
			Model:          model,
			Dimensions:     dimensions,
			GeneratedAt:    time.Now(),
			Status:         StatusGenerated,
			SourceRevision: sourceRevision,
		}
		data, err := json.Marshal(record)
		if err != nil {
			return errs.WrapInvalid(err, "Storage", "SaveGenerated", "marshal embedding record")
		}

		if _, err := s.indexBucket.Update(ctx, entityID, data, revision); err != nil {
			if isRevisionMismatch(err) {
				continue // a concurrent writer moved the key; re-read and re-evaluate
			}
			return errs.WrapTransient(err, "Storage", "SaveGenerated", "update generated embedding")
		}
		return nil
	}
	return errs.WrapTransient(ErrCASExhausted, "Storage", "SaveGenerated", "revision CAS did not converge")
}

// SaveFailed marks an embedding as failed under the same revision CAS and ordering
// guard as SaveGenerated, so a stale failure cannot clobber a newer success and the
// failed record persists its own source revision for later ordering.
func (s *Storage) SaveFailed(ctx context.Context, entityID, errorMsg string, sourceRevision uint64) error {
	if entityID == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "SaveFailed", "entity_id is empty")
	}

	for attempt := 0; attempt < maxCASRetries; attempt++ {
		existing, revision, err := s.getEmbeddingWithRevision(ctx, entityID)
		if err != nil {
			return errs.WrapTransient(err, "Storage", "SaveFailed", "get existing record")
		}

		// Nothing to mark: the record is already gone. A missing key means the entity
		// was tombstoned (or its pending record deleted) while generation was in
		// flight. Creating a failed record here would leave a permanent
		// EMBEDDING_INDEX entry for an entity that no longer exists.
		if existing == nil {
			return ErrRecordGone
		}

		// A newer outcome already landed for this entity; do not overwrite it with an
		// older revision's failure. Return ErrSupersededRevision (not nil) so markFailed
		// does not count a failure for a revision the newer outcome already resolved.
		if existing.SourceRevision > sourceRevision {
			return ErrSupersededRevision
		}

		// Equal-revision terminal precedence: a GENERATED outcome wins over a failure at
		// the SAME revision. Without this, a duplicate/racing SaveFailed(R) after
		// SaveGenerated(R) already produced a StatusGenerated record would flip that good
		// vector to StatusFailed — the strict `>` guard above does not fire at equal R.
		// Treat it as already-resolved (the same non-failure sentinel markFailed does not
		// count), so a generated vector at R is immutable to a same-revision failure.
		if existing.Status == StatusGenerated && existing.SourceRevision == sourceRevision {
			return ErrSupersededRevision
		}

		existing.Status = StatusFailed
		existing.ErrorMsg = errorMsg
		existing.SourceRevision = sourceRevision

		data, err := json.Marshal(existing)
		if err != nil {
			return errs.WrapInvalid(err, "Storage", "SaveFailed", "marshal embedding record")
		}

		if _, err := s.indexBucket.Update(ctx, entityID, data, revision); err != nil {
			if isRevisionMismatch(err) {
				continue
			}
			return errs.WrapTransient(err, "Storage", "SaveFailed", "update failed embedding")
		}
		return nil
	}
	return errs.WrapTransient(ErrCASExhausted, "Storage", "SaveFailed", "revision CAS did not converge")
}

// isRevisionMismatch reports whether a KV Update error is a compare-and-set
// conflict (the key's last sequence moved since it was read). NATS returns the
// wrong-last-sequence API error (code 10071, surfaced as jetstream.ErrKeyExists);
// the string fallbacks cover wrappers that carry only the message.
func isRevisionMismatch(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, jetstream.ErrKeyExists) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "wrong last sequence") || strings.Contains(msg, "10071")
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

// getEmbeddingWithRevision is the revision-aware sibling of GetEmbedding: it returns
// the record AND the KV revision it was read at, so the two save lanes can Update
// under compare-and-set. A missing key is (nil, 0, nil) — not found is not an error,
// and the caller treats nil as ErrRecordGone.
func (s *Storage) getEmbeddingWithRevision(ctx context.Context, entityID string) (*Record, uint64, error) {
	if entityID == "" {
		return nil, 0, errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "getEmbeddingWithRevision", "entity_id is empty")
	}

	entry, err := s.indexBucket.Get(ctx, entityID)
	if err != nil {
		if errors.Is(err, jetstream.ErrKeyNotFound) {
			return nil, 0, nil
		}
		return nil, 0, errs.WrapTransient(err, "Storage", "getEmbeddingWithRevision", "get embedding")
	}

	var record Record
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		return nil, 0, errs.WrapInvalid(err, "Storage", "getEmbeddingWithRevision", "unmarshal embedding record")
	}

	return &record, entry.Revision(), nil
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

// SaveDedup saves a content-addressed embedding for deduplication.
//
// model and dimensions identify the vector space the vector belongs to; callers
// pass the generating embedder's own values so a stale record is auditable
// (gh#612). contentHash MUST come from DedupKey, not ContentHash — the durable
// dedup bucket outlives any one embedder configuration.
func (s *Storage) SaveDedup(
	ctx context.Context,
	contentHash string,
	vector []float32,
	entityID, model string,
	dimensions int,
) error {
	if contentHash == "" {
		return errs.WrapInvalid(errs.ErrMissingConfig, "Storage", "SaveDedup", "content_hash is empty")
	}

	// Check if dedup record exists
	existing, err := s.GetByContentHash(ctx, contentHash)
	if err != nil {
		return err
	}

	var record *DedupRecord
	if existing != nil && existing.Model == model && existing.Dimensions == dimensions {
		// Same vector space: this entity simply shares content with earlier ones,
		// so it joins the record and the stored vector stands.
		record = existing
		record.EntityIDs = append(record.EntityIDs, entityID)
	} else {
		// No record, or a record from a different/unknown vector space — REPLACE it.
		//
		// This branch must not merely backfill identity onto the old record. Doing
		// that stamped a legacy identityless record with the current model's name
		// while keeping its old vector, so the next entity with the same content
		// passed dedupRecordUsable's identity check and was served that stale
		// vector: gh#612 surviving one hop past its own guard. The vector passed in
		// here was generated by the current embedder and is authoritative for this
		// key, so the prior entity list goes with the prior vector — those entities
		// regenerate under the same rule and re-join.
		record = &DedupRecord{
			Vector:         vector,
			EntityIDs:      []string{entityID},
			FirstGenerated: time.Now(),
			Model:          model,
			Dimensions:     dimensions,
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
