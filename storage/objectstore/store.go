// Package objectstore provides simple ObjectStore wrapper for immutable message storage.
// It uses NATS ObjectStore (NOT KeyValue) to store parsed messages as JSON blobs
// with time-bucketed slash-based keys for efficient organization and retrieval.
package objectstore

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/c360studio/semstreams/storage"
	"github.com/nats-io/nats.go/jetstream"
)

// Verify Store implements storage.Store interface
var (
	_ storage.Store           = (*Store)(nil)
	_ storage.StreamableStore = (*Store)(nil)
)

// Store provides simple ObjectStore wrapper for message storage.
// Messages are stored as immutable JSON blobs with time-bucketed slash-based keys.
//
// Composition-Friendly Design:
//   - Pluggable KeyGenerator (inject custom key generation)
//   - Pluggable MetadataExtractor (inject custom metadata extraction)
//   - No hardcoded semantic requirements
type Store struct {
	client            *natsclient.Client
	bucketName        string
	store             jetstream.ObjectStore
	dataCache         cache.Cache[[]byte]
	keyGenerator      storage.KeyGenerator
	metadataExtractor storage.MetadataExtractor
	metrics           *storeMetrics
	decoder           *message.Decoder
}

// SetDecoder installs the payload Decoder used by FetchContent's
// fallback path (extractContentFromBaseMessage). Required ONLY when
// callers store raw BaseMessage bytes via Store and later read them
// back via FetchContent — that path needs to type-discriminate the
// envelope. Pure StoreContent (write-side) and Open (streaming
// read-side) consumers can leave this nil; only the FetchContent
// fallback exercises the decoder.
//
// agentic-loop and graph-embedding both construct stores directly
// without calling SetDecoder; both are write-only / streaming-only
// consumers and never reach the FetchContent fallback. The
// objectstore Component (used by graph-ingest) wires the decoder via
// its lifecycle Start because its read path uses FetchContent.
func (s *Store) SetDecoder(d *message.Decoder) {
	s.decoder = d
}

// NewStoreWithConfig creates a new ObjectStore with cache configuration.
// Uses NATS ObjectStore (NOT KeyValue) for immutable message storage.
func NewStoreWithConfig(ctx context.Context, client *natsclient.Client, cfg Config) (*Store, error) {
	return NewStoreWithConfigAndMetrics(ctx, client, cfg, nil)
}

// NewStoreWithConfigAndMetrics creates a new ObjectStore with cache configuration and optional metrics.
// Uses NATS ObjectStore (NOT KeyValue) for immutable message storage.
func NewStoreWithConfigAndMetrics(
	ctx context.Context,
	client *natsclient.Client,
	cfg Config,
	metricsRegistry *metric.MetricsRegistry,
) (*Store, error) {
	bucketName := cfg.BucketName
	if bucketName == "" {
		bucketName = "MESSAGES"
	}

	js, err := client.JetStream()
	if err != nil {
		return nil, errs.WrapTransient(err, "Store", "NewStoreWithConfigAndMetrics", "get JetStream context")
	}

	storeConfig := jetstream.ObjectStoreConfig{
		Bucket:      bucketName,
		Description: "Immutable message storage",
		TTL:         24 * time.Hour, // Optional retention
	}

	// Use caller's context for ObjectStore operations
	store, err := js.CreateObjectStore(ctx, storeConfig)
	if err != nil {
		// Try to get existing store
		store, err = js.ObjectStore(ctx, bucketName)
		if err != nil {
			return nil, errs.WrapTransient(err, "Store", "NewStoreWithConfigAndMetrics", "create/get object store")
		}
	}

	// Create cache based on configuration with metrics
	dataCache, err := cache.NewFromConfig[[]byte](ctx, cfg.DataCache,
		cache.WithMetrics[[]byte](metricsRegistry, "objectstore"),
	)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Store", "NewStoreWithConfigAndMetrics", "create data cache")
	}

	// Use provided generators or defaults
	keyGen := cfg.KeyGenerator
	if keyGen == nil {
		keyGen = &DefaultKeyGenerator{}
	}

	metaExtractor := cfg.MetadataExtractor
	// metaExtractor can be nil - that's fine, we'll check before using

	// Initialize metrics if registry provided
	metrics, err := newStoreMetrics(metricsRegistry, bucketName)
	if err != nil {
		return nil, errs.WrapInvalid(err, "Store", "NewStoreWithConfigAndMetrics", "initialize metrics")
	}

	return &Store{
		client:            client,
		bucketName:        bucketName,
		store:             store,
		dataCache:         dataCache,
		keyGenerator:      keyGen,
		metadataExtractor: metaExtractor,
		metrics:           metrics,
	}, nil
}

// Put stores raw binary data at the specified key (implements storage.Store interface).
// This is the low-level storage operation - callers provide the key and raw bytes.
// For message-specific storage with automatic key generation, use Store() instead.
func (s *Store) Put(ctx context.Context, key string, data []byte) error {
	start := time.Now()
	s.metrics.recordWriteOp("put")

	// Store bytes in NATS ObjectStore
	_, err := s.store.PutBytes(ctx, key, data)
	if err != nil {
		s.metrics.recordError("put")
		return errs.WrapTransient(err, "Store", "Put", "put data")
	}

	s.metrics.recordWriteLatency("put", time.Since(start).Seconds())

	// Add to cache after successful storage
	if s.dataCache != nil {
		s.dataCache.Set(key, data)
	}

	return nil
}

// Store saves a message and returns the generated key.
// Messages are stored as JSON bytes with keys generated by the configured KeyGenerator.
// If a MetadataExtractor is configured, metadata is extracted and stored with the object.
//
// Special handling for raw byte inputs:
//   - []byte: stored directly without re-marshaling (avoids base64 encoding)
//   - json.RawMessage: stored directly as raw JSON bytes
//   - other types: marshaled to JSON via json.Marshal
func (s *Store) Store(ctx context.Context, msg any) (string, error) {
	start := time.Now()
	s.metrics.recordWriteOp("store")

	// Generate key using pluggable generator
	key := s.keyGenerator.GenerateKey(msg)

	// Handle raw bytes directly to avoid double-encoding.
	// json.Marshal([]byte) produces a base64-encoded string, which corrupts
	// JSON data that was already serialized by the caller.
	var data []byte
	var err error
	switch v := msg.(type) {
	case []byte:
		data = v
	case json.RawMessage:
		data = []byte(v)
	default:
		data, err = json.Marshal(msg)
		if err != nil {
			s.metrics.recordError("store")
			return "", errs.WrapInvalid(err, "Store", "Store", "marshal message")
		}
	}

	// Extract metadata if extractor is configured
	var meta map[string][]string
	if s.metadataExtractor != nil {
		meta = s.metadataExtractor.ExtractMetadata(msg)
	}

	// Store JSON bytes in ObjectStore with metadata
	if meta != nil {
		_, err = s.store.Put(ctx, jetstream.ObjectMeta{
			Name:    key,
			Headers: meta,
		}, strings.NewReader(string(data)))
	} else {
		// No metadata - simple storage
		_, err = s.store.PutBytes(ctx, key, data)
	}

	if err != nil {
		s.metrics.recordError("store")
		return "", errs.WrapTransient(err, "Store", "Store", "store message")
	}

	s.metrics.recordWriteLatency("store", time.Since(start).Seconds())

	// Add to cache after successful storage
	if s.dataCache != nil {
		s.dataCache.Set(key, data)
	}

	return key, nil
}

// Get retrieves a message by key.
// Checks cache first, then NATS ObjectStore if cache miss.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	start := time.Now()
	s.metrics.recordReadOp("get")

	// Check cache first
	if s.dataCache != nil {
		if data, found := s.dataCache.Get(key); found {
			s.metrics.recordCacheHit()
			s.metrics.recordReadLatency("get", time.Since(start).Seconds())
			return data, nil
		}
		s.metrics.recordCacheMiss()
	}

	// Cache miss - get from NATS ObjectStore
	data, err := s.store.GetBytes(ctx, key)
	if err != nil {
		s.metrics.recordError("get")
		return nil, errs.WrapTransient(err, "Store", "Get", "get message")
	}

	s.metrics.recordReadLatency("get", time.Since(start).Seconds())

	// Add to cache for future requests
	if s.dataCache != nil {
		s.dataCache.Set(key, data)
	}

	return data, nil
}

// Open returns a streaming reader for the content at key.
// Uses NATS ObjectStore's native chunked streaming — content is read
// in chunks without loading the entire object into memory.
// The caller MUST close the returned reader when done.
func (s *Store) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	s.metrics.recordReadOp("open")
	start := time.Now()

	result, err := s.store.Get(ctx, key)
	if err != nil {
		s.metrics.recordError("open")
		return nil, errs.WrapTransient(err, "Store", "Open", "open object stream")
	}

	s.metrics.recordReadLatency("open", time.Since(start).Seconds())
	return result, nil
}

// Delete removes a message (optional, messages are typically immutable).
// Also removes the message from cache if cached.
func (s *Store) Delete(ctx context.Context, key string) error {
	start := time.Now()
	s.metrics.recordDeleteOp()

	// Remove from cache first
	if s.dataCache != nil {
		s.dataCache.Delete(key)
	}

	// Remove from NATS ObjectStore
	err := s.store.Delete(ctx, key)
	if err != nil {
		s.metrics.recordError("delete")
		return err
	}

	s.metrics.recordDeleteLatency(time.Since(start).Seconds())
	return nil
}

// List returns keys matching a prefix (for debugging/inspection).
func (s *Store) List(ctx context.Context, prefix string) ([]string, error) {
	start := time.Now()
	s.metrics.recordListOp()

	var keys []string

	// Use List method which returns slice of ObjectInfo directly
	entries, err := s.store.List(ctx)
	if err != nil {
		s.metrics.recordError("list")
		return nil, errs.WrapTransient(err, "Store", "List", "list objects")
	}

	for _, entry := range entries {
		if strings.HasPrefix(entry.Name, prefix) {
			keys = append(keys, entry.Name)
		}
	}

	s.metrics.recordListLatency(time.Since(start).Seconds())
	return keys, nil
}

// GetMetadata returns the metadata associated with a stored object.
// Returns empty map if the object has no metadata.
func (s *Store) GetMetadata(ctx context.Context, key string) (map[string][]string, error) {
	start := time.Now()
	s.metrics.recordReadOp("get_metadata")

	// Get object info which includes headers
	info, err := s.store.GetInfo(ctx, key)
	if err != nil {
		s.metrics.recordError("get_metadata")
		return nil, errs.WrapTransient(err, "Store", "GetMetadata", "get object info")
	}

	s.metrics.recordReadLatency("get_metadata", time.Since(start).Seconds())

	if info.Headers == nil {
		return make(map[string][]string), nil
	}

	return info.Headers, nil
}

// Close properly shuts down the ObjectStore and releases resources.
// This includes closing the cache if it's enabled.
func (s *Store) Close() error {
	if s.dataCache != nil {
		return s.dataCache.Close()
	}
	return nil
}

// StoreContent stores content from a ContentStorable and returns a StorageReference.
// This is the high-level method for storing document content separately from triples.
//
// Flow:
//  1. Extract RawContent() and ContentFields() from ContentStorable
//  2. Create StoredContent envelope with timestamp
//  3. Store as JSON in ObjectStore
//  4. Return StorageReference for embedding in messages
func (s *Store) StoreContent(ctx context.Context, cs message.ContentStorable) (*message.StorageReference, error) {
	start := time.Now()
	s.metrics.recordWriteOp("store_content")

	entityID := cs.EntityID()
	binaryRefs := make(map[string]BinaryRef)

	// If implements BinaryStorable, store binary content first
	if bs, ok := cs.(message.BinaryStorable); ok {
		for fieldName, bin := range bs.BinaryFields() {
			binKey := s.generateBinaryKey(entityID, fieldName)

			// Store binary data directly (no JSON, no base64)
			_, err := s.store.PutBytes(ctx, binKey, bin.Data)
			if err != nil {
				s.metrics.recordError("store_content")
				return nil, errs.WrapTransient(err, "Store", "StoreContent", fmt.Sprintf("store binary %s", fieldName))
			}

			binaryRefs[fieldName] = BinaryRef{
				ContentType: bin.ContentType,
				Size:        int64(len(bin.Data)),
				Key:         binKey,
			}
		}
	}

	// Create StoredContent envelope (text fields + binary references)
	var stored *StoredContent
	if len(binaryRefs) > 0 {
		stored = NewStoredContentWithBinary(
			entityID,
			cs.RawContent(),
			cs.ContentFields(),
			binaryRefs,
		)
	} else {
		stored = NewStoredContent(
			entityID,
			cs.RawContent(),
			cs.ContentFields(),
		)
	}

	// Generate key using entity ID
	key := s.generateContentKey(entityID)

	// Marshal to JSON
	data, err := json.Marshal(stored)
	if err != nil {
		s.metrics.recordError("store_content")
		return nil, errs.WrapInvalid(err, "Store", "StoreContent", "marshal content")
	}

	// Store in ObjectStore
	_, err = s.store.PutBytes(ctx, key, data)
	if err != nil {
		s.metrics.recordError("store_content")
		return nil, errs.WrapTransient(err, "Store", "StoreContent", "store content")
	}

	s.metrics.recordWriteLatency("store_content", time.Since(start).Seconds())

	// Add to cache
	if s.dataCache != nil {
		s.dataCache.Set(key, data)
	}

	// Return reference
	return &message.StorageReference{
		StorageInstance: s.bucketName,
		Key:             key,
		ContentType:     "application/json",
		Size:            int64(len(data)),
	}, nil
}

// FetchContent retrieves stored content by StorageReference.
// Returns the StoredContent with fields and content mapping.
func (s *Store) FetchContent(ctx context.Context, ref *message.StorageReference) (*StoredContent, error) {
	if ref == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "Store", "FetchContent", "storage reference is nil")
	}

	start := time.Now()
	s.metrics.recordReadOp("fetch_content")

	// Check cache first
	var data []byte
	var found bool

	if s.dataCache != nil {
		data, found = s.dataCache.Get(ref.Key)
		if found {
			s.metrics.recordCacheHit()
		} else {
			s.metrics.recordCacheMiss()
		}
	}

	if !found {
		// Fetch from ObjectStore
		var err error
		data, err = s.store.GetBytes(ctx, ref.Key)
		if err != nil {
			s.metrics.recordError("fetch_content")
			return nil, errs.WrapTransient(err, "Store", "FetchContent", "fetch content")
		}

		// Add to cache
		if s.dataCache != nil {
			s.dataCache.Set(ref.Key, data)
		}
	}

	s.metrics.recordReadLatency("fetch_content", time.Since(start).Seconds())

	// Try to unmarshal as StoredContent first (direct content storage format)
	var stored StoredContent
	if err := json.Unmarshal(data, &stored); err == nil && stored.EntityID != "" {
		return &stored, nil
	}

	// Fallback: try to parse as BaseMessage and extract content
	// This handles cases where raw messages were stored
	return s.extractContentFromBaseMessage(data)
}

// extractContentFromBaseMessage extracts content from a stored BaseMessage.
// This enables ContentStorable pattern when raw messages are stored.
func (s *Store) extractContentFromBaseMessage(data []byte) (*StoredContent, error) {
	if s.decoder == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidConfig, "Store", "extractContentFromBaseMessage",
			"no payload decoder configured; call (*Store).SetDecoder before FetchContent")
	}
	baseMsg, err := s.decoder.Decode(data)
	if err != nil {
		// Include data prefix for debugging malformed content
		prefix := string(data)
		if len(prefix) > 100 {
			prefix = prefix[:100] + "..."
		}
		return nil, errs.WrapInvalid(err, "Store", "extractContentFromBaseMessage",
			fmt.Sprintf("parse as BaseMessage (len=%d, prefix=%q)", len(data), prefix))
	}

	payload := baseMsg.Payload()
	if payload == nil {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "Store", "extractContentFromBaseMessage", "BaseMessage has nil payload")
	}

	// Check if payload implements ContentStorable
	contentStorable, ok := payload.(message.ContentStorable)
	if !ok {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "Store", "extractContentFromBaseMessage",
			fmt.Sprintf("payload does not implement ContentStorable: %T", payload))
	}

	// Get content fields mapping
	contentFields := contentStorable.ContentFields()
	if len(contentFields) == 0 {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "Store", "extractContentFromBaseMessage", "ContentStorable has no content fields")
	}

	// Get entity ID
	graphable, ok := payload.(interface{ EntityID() string })
	if !ok {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "Store", "extractContentFromBaseMessage", "payload does not have EntityID")
	}

	// Extract content text from Graphable's triples or properties
	fields := s.extractTextFields(payload, contentFields)

	return &StoredContent{
		EntityID:      graphable.EntityID(),
		Fields:        fields,
		ContentFields: contentFields,
		StoredAt:      time.Now(),
	}, nil
}

// extractTextFields extracts text content from payload using ContentFields mapping.
// It tries multiple interfaces in priority order:
//  1. RawContent() map[string]string - ContentStorable interface
//  2. Properties() map[string]any - generic properties interface
func (s *Store) extractTextFields(payload message.Payload, contentFields map[string]string) map[string]string {
	fields := make(map[string]string)

	// Priority 1: Try RawContent (ContentStorable interface)
	// This is the primary path for Document and other ContentStorable types
	if rawContent, ok := payload.(interface{ RawContent() map[string]string }); ok {
		content := rawContent.RawContent()
		for _, fieldName := range contentFields {
			if val, ok := content[fieldName]; ok && val != "" {
				fields[fieldName] = val
			}
		}
		if len(fields) > 0 {
			return fields
		}
	}

	// Priority 2: Try Properties (generic properties interface)
	if propsHolder, ok := payload.(interface{ Properties() map[string]any }); ok {
		props := propsHolder.Properties()
		for _, fieldName := range contentFields {
			if val, ok := props[fieldName]; ok {
				if str, ok := val.(string); ok && str != "" {
					fields[fieldName] = str
				}
			}
		}
	}

	return fields
}

// generateContentKey creates a time-bucketed key for content storage.
// Format: content/year/month/day/hour/entityID_timestamp
func (s *Store) generateContentKey(entityID string) string {
	now := time.Now()
	return fmt.Sprintf("content/%04d/%02d/%02d/%02d/%s_%d",
		now.Year(),
		now.Month(),
		now.Day(),
		now.Hour(),
		entityID,
		now.UnixNano(),
	)
}

// generateBinaryKey creates a time-bucketed key for binary content storage.
// Format: binary/year/month/day/entityID_fieldName_timestamp
func (s *Store) generateBinaryKey(entityID, fieldName string) string {
	now := time.Now()
	return fmt.Sprintf("binary/%04d/%02d/%02d/%s_%s_%d",
		now.Year(),
		now.Month(),
		now.Day(),
		entityID,
		fieldName,
		now.UnixNano(),
	)
}

// FetchBinary retrieves binary content by its reference.
// Returns the raw binary data (no JSON, no base64 - direct bytes).
func (s *Store) FetchBinary(ctx context.Context, ref BinaryRef) ([]byte, error) {
	if ref.Key == "" {
		return nil, errs.WrapInvalid(errs.ErrInvalidData, "Store", "FetchBinary", "binary reference has empty key")
	}

	start := time.Now()
	s.metrics.recordReadOp("fetch_binary")

	// Check cache first
	if s.dataCache != nil {
		if data, found := s.dataCache.Get(ref.Key); found {
			s.metrics.recordCacheHit()
			s.metrics.recordReadLatency("fetch_binary", time.Since(start).Seconds())
			return data, nil
		}
		s.metrics.recordCacheMiss()
	}

	// Fetch from ObjectStore
	data, err := s.store.GetBytes(ctx, ref.Key)
	if err != nil {
		s.metrics.recordError("fetch_binary")
		return nil, errs.WrapTransient(err, "Store", "FetchBinary", "fetch binary")
	}

	s.metrics.recordReadLatency("fetch_binary", time.Since(start).Seconds())

	// Add to cache for future requests
	if s.dataCache != nil {
		s.dataCache.Set(ref.Key, data)
	}

	return data, nil
}

// DefaultKeyGenerator provides time-based key generation.
// Keys are formatted as: type/year/month/day/hour/identifier_timestamp
//
// The generator uses behavioral interfaces for optional enhancement:
//   - message.Typeable: Provides type information for key prefix
//   - message.Identifiable: Provides identifier for key uniqueness
//
// If interfaces aren't implemented, sensible defaults are used.
type DefaultKeyGenerator struct{}

// GenerateKey creates a time-bucketed slash-based key.
// Format: type/year/month/day/hour/identifier_timestamp
// Example: sensor.temperature.v1/2024/01/19/14/device-123_1705677443
func (g *DefaultKeyGenerator) GenerateKey(msg any) string {
	now := time.Now()

	// Extract type if message implements Message interface
	msgType := "message"
	if m, ok := msg.(message.Message); ok {
		msgType = m.Type().String()
	}

	// Extract identifier from message ID
	identifier := "unknown"
	if m, ok := msg.(message.Message); ok {
		identifier = m.ID()
	}

	// Format: type/year/month/day/hour/identifier_timestamp
	// Example: sensor.temperature.v1/2024/01/19/14/msg-uuid_1705677443
	key := fmt.Sprintf("%s/%04d/%02d/%02d/%02d/%s_%d",
		msgType,
		now.Year(),
		now.Month(),
		now.Day(),
		now.Hour(),
		identifier,
		now.Unix(),
	)

	return key
}
