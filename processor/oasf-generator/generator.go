package oasfgenerator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/pkg/cache"
	"github.com/c360studio/semstreams/pkg/errs"
	"github.com/nats-io/nats.go/jetstream"
)

// Generator handles the core OASF record generation logic.
// It maintains state for pending generations and handles debouncing.
type Generator struct {
	mapper     *Mapper
	natsClient *natsclient.Client
	config     Config
	logger     *slog.Logger

	// Coalescing: deduplicates rapid entity updates within a fixed time window
	coalescer *cache.CoalescingSet

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc

	// KV stores
	entityKV *natsclient.KVStore
	oasfKV   *natsclient.KVStore
}

// NewGenerator creates a new OASF generator.
func NewGenerator(mapper *Mapper, natsClient *natsclient.Client, config Config, logger *slog.Logger) *Generator {
	return &Generator{
		mapper:     mapper,
		natsClient: natsClient,
		config:     config,
		logger:     logger,
	}
}

// Initialize sets up KV stores and coalescer for the generator.
func (g *Generator) Initialize(ctx context.Context) error {
	// Store parent context for background operations
	g.ctx, g.cancel = context.WithCancel(ctx)

	// Get or create entity KV bucket
	entityBucket, err := g.getOrCreateKVBucket(ctx, g.config.EntityKVBucket)
	if err != nil {
		return errs.Wrap(err, "Generator", "Initialize", "get entity KV bucket")
	}
	g.entityKV = g.natsClient.NewKVStore(entityBucket)

	// Get or create OASF KV bucket
	oasfBucket, err := g.getOrCreateKVBucket(ctx, g.config.OASFKVBucket)
	if err != nil {
		return errs.Wrap(err, "Generator", "Initialize", "get OASF KV bucket")
	}
	g.oasfKV = g.natsClient.NewKVStore(oasfBucket)

	// Create coalescer for batching rapid entity updates
	g.coalescer = cache.NewCoalescingSet(ctx, g.config.GetGenerationDebounce(), func(entityIDs []string) {
		g.processEntityBatch(entityIDs)
	})

	return nil
}

// getOrCreateKVBucket gets or creates a KV bucket.
func (g *Generator) getOrCreateKVBucket(ctx context.Context, bucketName string) (jetstream.KeyValue, error) {
	// Try to get existing bucket first
	bucket, err := g.natsClient.GetKeyValueBucket(ctx, bucketName)
	if err == nil {
		return bucket, nil
	}

	// Create bucket if it doesn't exist
	bucket, err = g.natsClient.CreateKeyValueBucket(ctx, jetstream.KeyValueConfig{
		Bucket: bucketName,
	})
	if err != nil {
		return nil, errs.Wrap(err, "Generator", "getOrCreateKVBucket", "create bucket")
	}

	return bucket, nil
}

// QueueGeneration queues an entity for OASF generation.
// The actual generation happens after the coalescing window expires.
func (g *Generator) QueueGeneration(entityID string) {
	if g.coalescer != nil {
		g.coalescer.Add(entityID)
	}
}

// processEntityBatch generates OASF records for a batch of coalesced entities.
// Called by CoalescingSet callback after the debounce window expires.
func (g *Generator) processEntityBatch(entityIDs []string) {
	ctx := g.ctx
	if ctx == nil || ctx.Err() != nil {
		return
	}

	for _, entityID := range entityIDs {
		if ctx.Err() != nil {
			g.logger.Debug("Generation cancelled",
				slog.Int("remaining_entities", len(entityIDs)))
			return
		}

		if err := g.GenerateForEntity(ctx, entityID); err != nil {
			g.logger.Error("Failed to generate OASF record",
				slog.String("entity_id", entityID),
				slog.Any("error", err))
		}
	}
}

// GenerateForEntity generates an OASF record for a specific entity.
func (g *Generator) GenerateForEntity(ctx context.Context, entityID string) error {
	// Fetch entity triples from KV store
	triples, err := g.fetchEntityTriples(ctx, entityID)
	if err != nil {
		return errs.Wrap(err, "Generator", "GenerateForEntity", "fetch entity triples")
	}

	if len(triples) == 0 {
		g.logger.Debug("No triples found for entity, skipping OASF generation",
			slog.String("entity_id", entityID))
		return nil
	}

	// Check if this entity has capability predicates (is an agent)
	if !hasCapabilityPredicates(triples) {
		g.logger.Debug("Entity has no capability predicates, skipping OASF generation",
			slog.String("entity_id", entityID))
		return nil
	}

	// Generate OASF record
	record, err := g.mapper.MapTriplesToOASF(entityID, triples)
	if err != nil {
		return errs.Wrap(err, "Generator", "GenerateForEntity", "map triples to OASF")
	}

	// Validate record
	if err := record.Validate(); err != nil {
		return errs.WrapInvalid(err, "Generator", "GenerateForEntity", "validate OASF record")
	}

	// Store in KV and publish event
	if err := g.storeAndPublish(ctx, entityID, record); err != nil {
		return errs.Wrap(err, "Generator", "GenerateForEntity", "store and publish")
	}

	g.logger.Debug("Generated OASF record",
		slog.String("entity_id", entityID),
		slog.String("agent_name", record.Name),
		slog.Int("skill_count", len(record.Skills)))

	return nil
}

// fetchEntityTriples fetches triples for an entity from the KV store.
func (g *Generator) fetchEntityTriples(ctx context.Context, entityID string) ([]message.Triple, error) {
	if g.entityKV == nil {
		return nil, errs.WrapFatal(errs.ErrNotStarted, "Generator", "fetchEntityTriples", "entity KV not initialized")
	}

	entry, err := g.entityKV.Get(ctx, entityID)
	if err != nil {
		if natsclient.IsKVNotFoundError(err) {
			// Entity doesn't exist yet
			return nil, nil
		}
		return nil, errs.Wrap(err, "Generator", "fetchEntityTriples", "get entity state")
	}

	// Parse entity state (expected to contain triples). Uses the
	// canonical graph.EntityState rather than a local shadow so any
	// future oasf logic that needs MessageType (filter by source),
	// Version (optimistic-concurrency), or StorageRef sees real values
	// rather than zero values from a silently-stripped JSON decode.
	// Audit finding 2026-05-08 (Finding 4); closed in this commit.
	var entityState graph.EntityState
	if err := json.Unmarshal(entry.Value, &entityState); err != nil {
		return nil, errs.Wrap(err, "Generator", "fetchEntityTriples", "unmarshal entity state")
	}

	return entityState.Triples, nil
}

// storeAndPublish stores the OASF record and publishes a generation event.
func (g *Generator) storeAndPublish(ctx context.Context, entityID string, record *OASFRecord) error {
	if g.oasfKV == nil {
		return errs.WrapFatal(errs.ErrNotStarted, "Generator", "storeAndPublish", "OASF KV not initialized")
	}

	// Serialize record
	data, err := json.Marshal(record)
	if err != nil {
		return errs.Wrap(err, "Generator", "storeAndPublish", "marshal OASF record")
	}

	// Store in OASF KV bucket
	if _, err := g.oasfKV.Put(ctx, entityID, data); err != nil {
		return errs.Wrap(err, "Generator", "storeAndPublish", "put OASF record")
	}

	// Publish generation event
	eventSubject := fmt.Sprintf("oasf.record.generated.%s", sanitizeSubject(entityID))
	event := OASFGeneratedEvent{
		EntityID:  entityID,
		Record:    record,
		Timestamp: time.Now().UTC(),
	}
	eventData, err := json.Marshal(event)
	if err != nil {
		return errs.Wrap(err, "Generator", "storeAndPublish", "marshal event")
	}

	if err := g.natsClient.PublishToStream(ctx, eventSubject, eventData); err != nil {
		// Log but don't fail - KV storage succeeded
		g.logger.Warn("Failed to publish OASF generation event",
			slog.String("subject", eventSubject),
			slog.Any("error", err))
	}

	return nil
}

// OASFGeneratedEvent is published when an OASF record is generated.
type OASFGeneratedEvent struct {
	EntityID  string      `json:"entity_id"`
	Record    *OASFRecord `json:"record"`
	Timestamp time.Time   `json:"timestamp"`
}

// hasCapabilityPredicates checks if any triples use capability predicates.
func hasCapabilityPredicates(triples []message.Triple) bool {
	for _, t := range triples {
		if isCapabilityPredicate(t.Predicate) {
			return true
		}
	}
	return false
}

// isCapabilityPredicate checks if a predicate is a capability-related predicate.
func isCapabilityPredicate(predicate string) bool {
	// Check if predicate starts with "agent.capability."
	return len(predicate) > 17 && predicate[:17] == "agent.capability."
}

// sanitizeSubject converts an entity ID to a valid NATS subject component.
func sanitizeSubject(entityID string) string {
	// Replace dots with dashes for subject safety
	result := make([]byte, len(entityID))
	for i := 0; i < len(entityID); i++ {
		if entityID[i] == '.' {
			result[i] = '-'
		} else {
			result[i] = entityID[i]
		}
	}
	return string(result)
}

// Stop cleans up the generator resources.
func (g *Generator) Stop() {
	if g.coalescer != nil {
		_ = g.coalescer.Close()
	}

	if g.cancel != nil {
		g.cancel()
	}
}
