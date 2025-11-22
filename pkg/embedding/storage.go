package embedding

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360/semstreams/errors"
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
	SourceText  string    `json:"source_text,omitempty"` // Stored for pending records
	Model       string    `json:"model,omitempty"`
	Dimensions  int       `json:"dimensions,omitempty"`
	GeneratedAt time.Time `json:"generated_at,omitempty"`
	Status      Status    `json:"status"`
	ErrorMsg    string    `json:"error_msg,omitempty"` // If status=failed
}

// DedupRecord stores content-addressed embeddings for deduplication
type DedupRecord struct {
	Vector         []float32 `json:"vector"`
	EntityIDs      []string  `json:"entity_ids"` // Entities sharing this content
	FirstGenerated time.Time `json:"first_generated"`
}

// Storage handles persistence of embeddings to NATS KV buckets
type Storage struct {
	indexBucket jetstream.KeyValue // EMBEDDING_INDEX
	dedupBucket jetstream.KeyValue // EMBEDDING_DEDUP
}

// NewStorage creates a new embedding storage instance
func NewStorage(indexBucket, dedupBucket jetstream.KeyValue) *Storage {
	return &Storage{
		indexBucket: indexBucket,
		dedupBucket: dedupBucket,
	}
}

// SavePending saves a pending embedding request
func (s *Storage) SavePending(ctx context.Context, entityID, contentHash, sourceText string) error {
	if entityID == "" {
		return errors.WrapInvalid(errors.ErrMissingConfig, "Storage", "SavePending", "entity_id is empty")
	}

	record := &Record{
		EntityID:    entityID,
		ContentHash: contentHash,
		SourceText:  sourceText,
		Status:      StatusPending,
	}

	data, err := json.Marshal(record)
	if err != nil {
		return errors.WrapInvalid(err, "Storage", "SavePending", "marshal embedding record")
	}

	if _, err := s.indexBucket.Put(ctx, entityID, data); err != nil {
		return errors.WrapTransient(err, "Storage", "SavePending", "put pending embedding")
	}

	return nil
}

// SaveGenerated saves a generated embedding with metadata
func (s *Storage) SaveGenerated(ctx context.Context, entityID string, vector []float32, model string, dimensions int) error {
	if entityID == "" {
		return errors.WrapInvalid(errors.ErrMissingConfig, "Storage", "SaveGenerated", "entity_id is empty")
	}

	// Get existing record to preserve content_hash
	existing, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		return errors.WrapTransient(err, "Storage", "SaveGenerated", "get existing record")
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
		return errors.WrapInvalid(err, "Storage", "SaveGenerated", "marshal embedding record")
	}

	if _, err := s.indexBucket.Put(ctx, entityID, data); err != nil {
		return errors.WrapTransient(err, "Storage", "SaveGenerated", "put generated embedding")
	}

	return nil
}

// SaveFailed marks an embedding as failed with error message
func (s *Storage) SaveFailed(ctx context.Context, entityID, errorMsg string) error {
	if entityID == "" {
		return errors.WrapInvalid(errors.ErrMissingConfig, "Storage", "SaveFailed", "entity_id is empty")
	}

	// Get existing record to preserve metadata
	existing, err := s.GetEmbedding(ctx, entityID)
	if err != nil {
		return errors.WrapTransient(err, "Storage", "SaveFailed", "get existing record")
	}

	existing.Status = StatusFailed
	existing.ErrorMsg = errorMsg

	data, err := json.Marshal(existing)
	if err != nil {
		return errors.WrapInvalid(err, "Storage", "SaveFailed", "marshal embedding record")
	}

	if _, err := s.indexBucket.Put(ctx, entityID, data); err != nil {
		return errors.WrapTransient(err, "Storage", "SaveFailed", "put failed embedding")
	}

	return nil
}

// GetEmbedding retrieves an embedding by entity ID
func (s *Storage) GetEmbedding(ctx context.Context, entityID string) (*Record, error) {
	if entityID == "" {
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "Storage", "GetEmbedding", "entity_id is empty")
	}

	entry, err := s.indexBucket.Get(ctx, entityID)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, nil // Not found is not an error
		}
		return nil, errors.WrapTransient(err, "Storage", "GetEmbedding", "get embedding")
	}

	var record Record
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		return nil, errors.WrapInvalid(err, "Storage", "GetEmbedding", "unmarshal embedding record")
	}

	return &record, nil
}

// GetByContentHash retrieves an embedding by content hash (for deduplication)
func (s *Storage) GetByContentHash(ctx context.Context, contentHash string) (*DedupRecord, error) {
	if contentHash == "" {
		return nil, errors.WrapInvalid(errors.ErrMissingConfig, "Storage", "GetByContentHash", "content_hash is empty")
	}

	entry, err := s.dedupBucket.Get(ctx, contentHash)
	if err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil, nil // Not found is not an error
		}
		return nil, errors.WrapTransient(err, "Storage", "GetByContentHash", "get dedup record")
	}

	var record DedupRecord
	if err := json.Unmarshal(entry.Value(), &record); err != nil {
		return nil, errors.WrapInvalid(err, "Storage", "GetByContentHash", "unmarshal dedup record")
	}

	return &record, nil
}

// SaveDedup saves a content-addressed embedding for deduplication
func (s *Storage) SaveDedup(ctx context.Context, contentHash string, vector []float32, entityID string) error {
	if contentHash == "" {
		return errors.WrapInvalid(errors.ErrMissingConfig, "Storage", "SaveDedup", "content_hash is empty")
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
		return errors.WrapInvalid(err, "Storage", "SaveDedup", "marshal dedup record")
	}

	if _, err := s.dedupBucket.Put(ctx, contentHash, data); err != nil {
		return errors.WrapTransient(err, "Storage", "SaveDedup", "put dedup record")
	}

	return nil
}

// DeleteEmbedding removes an embedding record
func (s *Storage) DeleteEmbedding(ctx context.Context, entityID string) error {
	if entityID == "" {
		return errors.WrapInvalid(errors.ErrMissingConfig, "Storage", "DeleteEmbedding", "entity_id is empty")
	}

	if err := s.indexBucket.Delete(ctx, entityID); err != nil {
		if err == jetstream.ErrKeyNotFound {
			return nil // Already deleted
		}
		return errors.WrapTransient(err, "Storage", "DeleteEmbedding", "delete embedding")
	}

	return nil
}
