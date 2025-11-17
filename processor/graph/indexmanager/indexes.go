package indexmanager

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/c360/semstreams/errors"
	gtypes "github.com/c360/semstreams/graph"
	"github.com/c360/semstreams/message"
	"github.com/c360/semstreams/natsclient"
	"github.com/c360/semstreams/vocabulary"
)

// sanitizeNATSKey sanitizes a string to be a valid NATS KV key with security hardening
// NATS KV keys cannot contain spaces, consecutive dots, or leading/trailing dots
// Security: Prevents injection attacks, directory traversal, and DoS via long keys
func sanitizeNATSKey(key string) string {
	// Enforce maximum length to prevent DoS
	const maxKeyLength = 255
	if len(key) > maxKeyLength {
		key = key[:maxKeyLength]
	}

	// Replace spaces with underscores
	key = strings.ReplaceAll(key, " ", "_")

	// Remove any invalid characters (keep only NATS-valid chars:
	// alphanumeric, dots, dashes, underscores, forward slash, equals)
	// NATS KV key pattern: ^[-/_=\.a-zA-Z0-9]+$
	reg := regexp.MustCompile(`[^a-zA-Z0-9.\-_/=]`)
	key = reg.ReplaceAllString(key, "")

	// Prevent directory traversal attacks
	key = strings.ReplaceAll(key, "../", "")
	key = strings.ReplaceAll(key, "/..", "")

	// Remove consecutive dots and slashes (security hardening)
	for strings.Contains(key, "..") {
		key = strings.ReplaceAll(key, "..", ".")
	}
	for strings.Contains(key, "//") {
		key = strings.ReplaceAll(key, "//", "/")
	}

	// Remove leading/trailing dots and slashes
	key = strings.Trim(key, "./")

	// Ensure key is not empty
	if key == "" {
		key = "unknown"
	}

	return key
}

// Index defines the interface that all index types must implement
type Index interface {
	HandleCreate(ctx context.Context, entityID string, entityState interface{}) error
	HandleUpdate(ctx context.Context, entityID string, entityState interface{}) error
	HandleDelete(ctx context.Context, entityID string) error
}

// PredicateIndex handles predicate-based indexing for triple queries
type PredicateIndex struct {
	bucket      jetstream.KeyValue
	kvStore     *natsclient.KVStore
	metrics     *InternalMetrics
	promMetrics *PrometheusMetrics
	logger      *slog.Logger
}

// NewPredicateIndex creates a new PredicateIndex
func NewPredicateIndex(
	bucket jetstream.KeyValue,
	natsClient *natsclient.Client,
	metrics *InternalMetrics,
	promMetrics *PrometheusMetrics,
	logger *slog.Logger,
) *PredicateIndex {
	if logger == nil {
		logger = slog.Default()
	}

	var kvStore *natsclient.KVStore
	if natsClient != nil {
		kvStore = natsClient.NewKVStore(bucket)
	}

	return &PredicateIndex{
		bucket:      bucket,
		kvStore:     kvStore,
		metrics:     metrics,
		promMetrics: promMetrics,
		logger:      logger,
	}
}

// HandleCreate processes entity creation for predicate index
func (pi *PredicateIndex) HandleCreate(ctx context.Context, entityID string, entityState interface{}) error {
	state, ok := entityState.(*gtypes.EntityState)
	if !ok {
		return errors.WrapInvalid(errors.ErrInvalidData, "IndexManager", "HandleCreate", "invalid entity state type")
	}

	// Use existing triples directly from entity state (semantic triples as single source of truth)
	return pi.updatePredicateIndex(ctx, entityID, state.Triples)
}

// HandleUpdate processes entity updates for predicate index
func (pi *PredicateIndex) HandleUpdate(ctx context.Context, entityID string, entityState interface{}) error {
	state, ok := entityState.(*gtypes.EntityState)
	if !ok {
		return errors.WrapInvalid(errors.ErrInvalidData, "IndexManager", "HandleUpdate", "invalid entity state type")
	}

	// Use existing triples directly from entity state (semantic triples as single source of truth)
	// For updates, we need to remove old entries and add new ones
	// For simplicity in Phase 1, we'll just update all predicates
	return pi.updatePredicateIndex(ctx, entityID, state.Triples)
}

// HandleDelete processes entity deletion for predicate index
func (pi *PredicateIndex) HandleDelete(ctx context.Context, entityID string) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// For deletion, we would need to remove the entity from all predicate indexes
	// This requires knowing what predicates the entity had, which is complex
	// For Phase 1, we'll implement a simple cleanup approach
	pi.logger.Warn("Entity deleted - cleanup not fully implemented", "entity", entityID)
	return nil
}

// updatePredicateIndex updates the predicate index with entity triples
func (pi *PredicateIndex) updatePredicateIndex(
	ctx context.Context,
	entityID string,
	triples []message.Triple,
) error {
	for _, triple := range triples {
		if err := pi.addEntityToPredicateIndex(ctx, triple.Predicate, entityID); err != nil {
			errMsg := fmt.Sprintf("update index for predicate %s", triple.Predicate)
			return errors.WrapTransient(err, "IndexManager", "UpdatePredicateIndex", errMsg)
		}
	}
	return nil
}

// addEntityToPredicateIndex adds an entity to a specific predicate index using CAS for race-free updates
func (pi *PredicateIndex) addEntityToPredicateIndex(ctx context.Context, predicate, entityID string) error {
	sanitizedKey := sanitizeNATSKey(predicate)
	pi.logger.Debug("Adding entity to predicate", "entity", entityID, "predicate", predicate, "sanitized_key", sanitizedKey)

	if err := pi.validatePredicateKey(sanitizedKey, predicate); err != nil {
		return err
	}

	// Use KVStore if available for cleaner CAS operations
	if pi.kvStore != nil {
		return pi.addEntityWithKVStore(ctx, sanitizedKey, predicate, entityID)
	}

	// Fallback to manual CAS retry loop
	return pi.addEntityWithManualCAS(ctx, sanitizedKey, predicate, entityID)
}

// validatePredicateKey validates a sanitized predicate key
func (pi *PredicateIndex) validatePredicateKey(sanitizedKey, predicate string) error {
	if len(sanitizedKey) == 0 {
		pi.logger.Error("Empty key after sanitization", "original_predicate", predicate)
		errMsg := fmt.Sprintf("empty key after sanitization of predicate %q", predicate)
		return errors.WrapInvalid(errors.ErrInvalidData, "IndexManager", "updatePredicateIndexInternal", errMsg)
	}

	if len(sanitizedKey) > 255 {
		pi.logger.Error("Key too long", "length", len(sanitizedKey), "max", 255, "key", sanitizedKey)
		errMsg := fmt.Sprintf("key too long after sanitization: %d chars", len(sanitizedKey))
		return errors.WrapInvalid(errors.ErrInvalidData, "IndexManager", "updatePredicateIndexInternal", errMsg)
	}

	validKeyRe := regexp.MustCompile(`^[-/_=\.a-zA-Z0-9]+$`)
	if !validKeyRe.MatchString(sanitizedKey) {
		pi.logger.Error("Key fails NATS validation", "key", sanitizedKey)
		errMsg := fmt.Sprintf("invalid NATS key after sanitization: %q", sanitizedKey)
		return errors.WrapInvalid(errors.ErrInvalidData, "IndexManager", "updatePredicateIndexInternal", errMsg)
	}

	return nil
}

// parseEntityList parses entity list from bytes, handling both simple and legacy formats
func parseEntityList(data []byte) ([]string, error) {
	var entities []string

	if len(data) == 0 {
		return entities, nil
	}

	// Try new simple format first ([]string)
	var existingEntities []string
	if err := json.Unmarshal(data, &existingEntities); err == nil {
		return existingEntities, nil
	}

	// Fallback to legacy format for migration compatibility
	var predicateData map[string]interface{}
	if err := json.Unmarshal(data, &predicateData); err != nil {
		return nil, errors.WrapInvalid(err, "IndexManager", "parseEntityList", "predicate data unmarshal failed")
	}

	if existingEntities, exists := predicateData["entities"]; exists {
		if entitiesArray, ok := existingEntities.([]interface{}); ok {
			for _, e := range entitiesArray {
				if entityStr, ok := e.(string); ok {
					entities = append(entities, entityStr)
				}
			}
		}
	}

	return entities, nil
}

// entityAlreadyIndexed checks if entity exists in the list
func entityAlreadyIndexed(entities []string, entityID string) bool {
	for _, existing := range entities {
		if existing == entityID {
			return true
		}
	}
	return false
}

// addEntityWithKVStore adds entity using KVStore UpdateWithRetry helper
func (pi *PredicateIndex) addEntityWithKVStore(
	ctx context.Context,
	sanitizedKey, predicate, entityID string,
) error {
	err := pi.kvStore.UpdateWithRetry(ctx, sanitizedKey, func(currentBytes []byte) ([]byte, error) {
		entities, err := parseEntityList(currentBytes)
		if err != nil {
			return nil, err
		}

		if entityAlreadyIndexed(entities, entityID) {
			pi.logger.Debug("Entity already indexed in predicate", "entity", entityID, "predicate", predicate)
			return currentBytes, nil
		}

		entities = append(entities, entityID)
		return json.Marshal(entities)
	})

	if err != nil {
		return pi.handleKVStoreError(ctx, err, sanitizedKey, predicate, entityID)
	}

	return nil
}

// handleKVStoreError handles errors from KVStore operations
func (pi *PredicateIndex) handleKVStoreError(
	ctx context.Context,
	err error,
	sanitizedKey, predicate, entityID string,
) error {
	if stderrors.Is(err, natsclient.ErrKVKeyNotFound) {
		entities := []string{entityID}
		data, _ := json.Marshal(entities)
		_, createErr := pi.kvStore.Create(ctx, sanitizedKey, data)
		if createErr != nil && !stderrors.Is(createErr, natsclient.ErrKVKeyExists) {
			errMsg := fmt.Sprintf("create predicate index for %s", predicate)
			return errors.WrapTransient(createErr, "IndexManager", "addEntityToPredicateIndex", errMsg)
		}
		return nil
	}

	if stderrors.Is(err, natsclient.ErrKVMaxRetriesExceeded) {
		errMsg := fmt.Sprintf("predicate index update after max retries: %s", predicate)
		return errors.WrapTransient(err, "IndexManager", "addEntityToPredicateIndex", errMsg)
	}

	if strings.Contains(err.Error(), "invalid key") {
		pi.logger.Error("Invalid key error", "predicate", predicate, "entity", entityID, "sanitized_key", sanitizedKey)
		errMsg := fmt.Sprintf("invalid key for predicate index: %s", predicate)
		return errors.WrapInvalid(err, "IndexManager", "addEntityToPredicateIndex", errMsg)
	}

	errMsg := fmt.Sprintf("store predicate index for %s", predicate)
	return errors.WrapTransient(err, "IndexManager", "addEntityToPredicateIndex", errMsg)
}

// addEntityWithManualCAS adds entity using manual CAS retry loop
func (pi *PredicateIndex) addEntityWithManualCAS(
	ctx context.Context,
	sanitizedKey, predicate, entityID string,
) error {
	maxRetries := 5
	for retry := 0; retry < maxRetries; retry++ {
		entities, currentRevision, err := pi.fetchCurrentEntities(ctx, sanitizedKey)
		if err != nil {
			return err
		}

		if entityAlreadyIndexed(entities, entityID) {
			pi.logger.Debug("Entity already indexed in predicate", "entity", entityID, "predicate", predicate)
			return nil
		}

		entities = append(entities, entityID)
		data, err := json.Marshal(entities)
		if err != nil {
			return errors.WrapInvalid(err, "IndexManager", "updatePredicateIndexInternal", "marshal predicate data")
		}

		pi.logger.Debug("Attempting CAS update", "key", sanitizedKey, "revision", currentRevision,
			"entities_count", len(entities), "retry", retry)

		if err := pi.performCASUpdate(ctx, sanitizedKey, data, currentRevision, retry); err != nil {
			if pi.shouldRetryCAS(err) {
				continue
			}
			return pi.handleCASError(err, sanitizedKey, predicate, entityID)
		}

		pi.logger.Debug("CAS operation successful", "key", sanitizedKey, "entities_count", len(entities), "retry", retry)
		return nil
	}

	errMsg := fmt.Sprintf("predicate index update after %d retries: %s", maxRetries, predicate)
	return errors.WrapTransient(errors.ErrMaxRetriesExceeded, "IndexManager", "updatePredicateIndexInternal", errMsg)
}

// fetchCurrentEntities retrieves current entities from bucket
func (pi *PredicateIndex) fetchCurrentEntities(ctx context.Context, sanitizedKey string) ([]string, uint64, error) {
	entry, err := pi.bucket.Get(ctx, sanitizedKey)
	if err != nil {
		return []string{}, 0, nil
	}

	entities, err := parseEntityList(entry.Value())
	if err != nil {
		return nil, 0, err
	}

	return entities, entry.Revision(), nil
}

// performCASUpdate performs CAS create or update operation
func (pi *PredicateIndex) performCASUpdate(
	ctx context.Context,
	sanitizedKey string,
	data []byte,
	currentRevision uint64,
	retry int,
) error {
	var err error
	if currentRevision == 0 {
		_, err = pi.bucket.Create(ctx, sanitizedKey, data)
	} else {
		_, err = pi.bucket.Update(ctx, sanitizedKey, data, currentRevision)
	}

	if err != nil {
		pi.logger.Error("CAS operation failed", "key", sanitizedKey, "retry", retry, "error", err)
	}

	return err
}

// shouldRetryCAS checks if CAS error is retryable
func (pi *PredicateIndex) shouldRetryCAS(err error) bool {
	if strings.Contains(err.Error(), "wrong last sequence") || strings.Contains(err.Error(), "revision") {
		return true
	}
	return false
}

// handleCASError handles CAS operation errors
func (pi *PredicateIndex) handleCASError(err error, sanitizedKey, predicate, entityID string) error {
	if strings.Contains(err.Error(), "invalid key") {
		pi.logger.Error("Invalid key error", "predicate", predicate, "entity", entityID, "sanitized_key", sanitizedKey)
		errMsg := fmt.Sprintf("invalid key for predicate index: %s", predicate)
		return errors.WrapInvalid(err, "IndexManager", "updatePredicateIndexInternal", errMsg)
	}

	pi.logger.Error("Non-retryable error", "predicate", predicate, "entity", entityID, "sanitized_key", sanitizedKey)
	errMsg := fmt.Sprintf("store predicate index for %s", predicate)
	return errors.WrapTransient(err, "IndexManager", "updatePredicateIndexInternal", errMsg)
}

// IncomingIndex handles incoming relationship indexing
type IncomingIndex struct {
	bucket      jetstream.KeyValue
	metrics     *InternalMetrics
	promMetrics *PrometheusMetrics
	logger      *slog.Logger
}

// NewIncomingIndex creates a new IncomingIndex
func NewIncomingIndex(
	bucket jetstream.KeyValue,
	metrics *InternalMetrics,
	promMetrics *PrometheusMetrics,
	logger *slog.Logger,
) *IncomingIndex {
	if logger == nil {
		logger = slog.Default()
	}
	return &IncomingIndex{
		bucket:      bucket,
		metrics:     metrics,
		promMetrics: promMetrics,
		logger:      logger,
	}
}

// HandleCreate processes entity creation for incoming index
func (ii *IncomingIndex) HandleCreate(ctx context.Context, entityID string, entityState interface{}) error {
	state, ok := entityState.(*gtypes.EntityState)
	if !ok {
		return errors.WrapInvalid(errors.ErrInvalidData, "IncomingIndex", "HandleCreate", "invalid entity state type")
	}

	return ii.updateIncomingIndex(ctx, entityID, state.Edges)
}

// HandleUpdate processes entity updates for incoming index
func (ii *IncomingIndex) HandleUpdate(ctx context.Context, entityID string, entityState interface{}) error {
	state, ok := entityState.(*gtypes.EntityState)
	if !ok {
		return errors.WrapInvalid(errors.ErrInvalidData, "IncomingIndex", "HandleUpdate", "invalid entity state type")
	}

	return ii.updateIncomingIndex(ctx, entityID, state.Edges)
}

// HandleDelete processes entity deletion for incoming index
func (ii *IncomingIndex) HandleDelete(ctx context.Context, entityID string) error {
	// Remove the incoming index entry for the deleted entity
	return ii.bucket.Delete(ctx, entityID)
}

// updateIncomingIndex updates incoming relationships for all target entities
func (ii *IncomingIndex) updateIncomingIndex(
	ctx context.Context,
	fromEntityID string,
	edges []gtypes.Edge,
) error {
	for _, edge := range edges {
		if err := ii.AddIncomingReference(ctx, edge.ToEntityID, fromEntityID); err != nil {
			ii.logger.Error(
				"Failed to update incoming reference",
				"from", fromEntityID,
				"to", edge.ToEntityID,
				"error", err,
			)
		}
	}
	return nil
}

// AddIncomingReference adds an incoming reference to a target entity (exported for direct use)
func (ii *IncomingIndex) AddIncomingReference(ctx context.Context, toEntityID, fromEntityID string) error {
	// Get existing incoming references - stored as direct array
	var incomingRefs []string
	entry, err := ii.bucket.Get(ctx, toEntityID)
	if err == nil {
		if err := json.Unmarshal(entry.Value(), &incomingRefs); err != nil {
			return errors.WrapInvalid(err, "IncomingIndex", "AddIncomingReference", "unmarshal incoming index data")
		}
	} else {
		incomingRefs = []string{}
	}

	// Check for duplicates
	for _, existing := range incomingRefs {
		if existing == fromEntityID {
			return nil // Already exists
		}
	}

	// Add new reference
	incomingRefs = append(incomingRefs, fromEntityID)

	// Store updated index - direct array, no wrapper
	data, err := json.Marshal(incomingRefs)
	if err != nil {
		return errors.WrapInvalid(err, "IncomingIndex", "AddIncomingReference", "marshal incoming index data")
	}

	_, err = ii.bucket.Put(ctx, toEntityID, data)
	return err
}

// AliasIndex handles entity alias resolution with bidirectional storage.
// Forward index: alias--<sanitized_alias> -> entity_id
// Reverse index: entity--<entity_id> -> []aliases (for cleanup)
// Uses vocabulary registry to discover alias predicates.
type AliasIndex struct {
	bucket      jetstream.KeyValue
	kvStore     *natsclient.KVStore
	metrics     *InternalMetrics
	promMetrics *PrometheusMetrics
	logger      *slog.Logger
}

// NewAliasIndex creates a new AliasIndex
func NewAliasIndex(
	bucket jetstream.KeyValue,
	natsClient *natsclient.Client,
	metrics *InternalMetrics,
	promMetrics *PrometheusMetrics,
	logger *slog.Logger,
) *AliasIndex {
	if logger == nil {
		logger = slog.Default()
	}

	var kvStore *natsclient.KVStore
	if natsClient != nil {
		kvStore = natsClient.NewKVStore(bucket)
	}

	return &AliasIndex{
		bucket:      bucket,
		kvStore:     kvStore,
		metrics:     metrics,
		promMetrics: promMetrics,
		logger:      logger,
	}
}

// HandleCreate processes entity creation for alias index
func (ai *AliasIndex) HandleCreate(ctx context.Context, entityID string, entityState interface{}) error {
	state, ok := entityState.(*gtypes.EntityState)
	if !ok {
		return errors.WrapInvalid(errors.ErrInvalidData, "AliasIndex", "HandleCreate", "invalid entity state type")
	}

	// Extract aliases from triples using vocabulary registry
	aliases := ai.extractAliases(state.Triples)
	if len(aliases) == 0 {
		return nil // No aliases to index
	}

	// Index all aliases with bidirectional storage
	return ai.indexAliases(ctx, entityID, aliases)
}

// HandleUpdate processes entity updates for alias index
func (ai *AliasIndex) HandleUpdate(ctx context.Context, entityID string, entityState interface{}) error {
	state, ok := entityState.(*gtypes.EntityState)
	if !ok {
		return errors.WrapInvalid(errors.ErrInvalidData, "AliasIndex", "HandleUpdate", "invalid entity state type")
	}

	// For updates, we need to:
	// 1. Get current aliases from reverse index
	// 2. Extract new aliases from current state
	// 3. Remove old aliases that are no longer present
	// 4. Add new aliases that didn't exist before

	// Extract new aliases from current triples
	newAliases := ai.extractAliases(state.Triples)

	// Get existing aliases from reverse index
	existingAliases, err := ai.getEntityAliases(ctx, entityID)
	if err != nil && !stderrors.Is(err, jetstream.ErrKeyNotFound) {
		return errors.WrapTransient(err, "AliasIndex", "HandleUpdate", "get existing aliases")
	}

	// Determine which aliases to remove and which to add
	aliasesToRemove := ai.findAliasesToRemove(existingAliases, newAliases)
	aliasesToAdd := ai.findAliasesToAdd(existingAliases, newAliases)

	// Remove old aliases
	for _, alias := range aliasesToRemove {
		if err := ai.removeAliasForward(ctx, alias); err != nil {
			ai.logger.Error("Failed to remove alias", "alias", alias, "entity", entityID, "error", err)
		}
	}

	// Add new aliases
	if len(aliasesToAdd) > 0 {
		if err := ai.indexAliases(ctx, entityID, aliasesToAdd); err != nil {
			return err
		}
	}

	// Update reverse index if aliases changed
	if len(aliasesToRemove) > 0 || len(aliasesToAdd) > 0 {
		if err := ai.updateReverseIndex(ctx, entityID, newAliases); err != nil {
			return err
		}
	}

	return nil
}

// HandleDelete processes entity deletion for alias index
func (ai *AliasIndex) HandleDelete(ctx context.Context, entityID string) error {
	// Get all aliases for this entity from reverse index
	aliases, err := ai.getEntityAliases(ctx, entityID)
	if err != nil {
		if stderrors.Is(err, jetstream.ErrKeyNotFound) {
			return nil // No aliases to clean up
		}
		return errors.WrapTransient(err, "AliasIndex", "HandleDelete", "get entity aliases")
	}

	// Remove all forward index entries (alias -> entityID)
	for _, alias := range aliases {
		if err := ai.removeAliasForward(ctx, alias); err != nil {
			ai.logger.Error("Failed to remove alias on delete", "alias", alias, "entity", entityID, "error", err)
		}
	}

	// Remove reverse index entry (entity -> aliases)
	if err := ai.bucket.Delete(ctx, fmt.Sprintf("entity--%s", entityID)); err != nil {
		if !stderrors.Is(err, jetstream.ErrKeyNotFound) {
			ai.logger.Error("Failed to remove reverse index on delete", "entity", entityID, "error", err)
		}
	}

	return nil
}

// extractAliases extracts alias values from triples using vocabulary registry.
// Only extracts aliases that can resolve to entity IDs (skips labels).
func (ai *AliasIndex) extractAliases(triples []message.Triple) []string {
	// Discover alias predicates from vocabulary registry
	aliasPredicates := vocabulary.DiscoverAliasPredicates()
	if len(aliasPredicates) == 0 {
		// No alias predicates registered - graceful degradation
		ai.logger.Debug("No alias predicates registered in vocabulary")
		return nil
	}

	var aliases []string
	for _, triple := range triples {
		// Check if this predicate is an alias predicate
		if _, isAlias := aliasPredicates[triple.Predicate]; !isAlias {
			continue
		}

		// Get predicate metadata to check if it's resolvable
		meta := vocabulary.GetPredicateMetadata(triple.Predicate)
		if meta == nil {
			continue
		}

		// Skip labels - they are NOT resolvable (ambiguous, display-only)
		if !meta.AliasType.CanResolveToEntityID() {
			ai.logger.Debug("Skipping non-resolvable alias",
				"predicate", triple.Predicate,
				"type", meta.AliasType.String())
			continue
		}

		// Extract string value
		aliasValue, ok := triple.Object.(string)
		if !ok {
			ai.logger.Warn("Alias predicate has non-string value",
				"predicate", triple.Predicate,
				"value", triple.Object)
			continue
		}

		if aliasValue != "" {
			aliases = append(aliases, aliasValue)
		}
	}

	return aliases
}

// indexAliases indexes all aliases with bidirectional storage.
// Forward: alias:<sanitized> -> entityID
// Reverse: entity:<entityID> -> []aliases
func (ai *AliasIndex) indexAliases(ctx context.Context, entityID string, aliases []string) error {
	// Index forward mappings (alias -> entity)
	for _, alias := range aliases {
		if err := ai.addAliasForward(ctx, alias, entityID); err != nil {
			return err
		}
	}

	// Update reverse index (entity -> aliases) for cleanup
	return ai.updateReverseIndex(ctx, entityID, aliases)
}

// addAliasForward adds a forward index entry: alias--<sanitized> -> entityID
func (ai *AliasIndex) addAliasForward(ctx context.Context, alias, entityID string) error {
	// Sanitize alias for NATS key
	sanitizedAlias := sanitizeNATSKey(alias)
	key := fmt.Sprintf("alias--%s", sanitizedAlias)

	ai.logger.Debug("Adding alias forward index", "alias", alias, "sanitized", sanitizedAlias, "entity", entityID)

	// Store alias -> entityID mapping
	data := []byte(entityID)
	_, err := ai.bucket.Put(ctx, key, data)
	if err != nil {
		errMsg := fmt.Sprintf("store alias forward index for %s", alias)
		return errors.WrapTransient(err, "AliasIndex", "addAliasForward", errMsg)
	}

	return nil
}

// removeAliasForward removes a forward index entry: alias--<sanitized> -> entityID
func (ai *AliasIndex) removeAliasForward(ctx context.Context, alias string) error {
	sanitizedAlias := sanitizeNATSKey(alias)
	key := fmt.Sprintf("alias--%s", sanitizedAlias)

	if err := ai.bucket.Delete(ctx, key); err != nil {
		if !stderrors.Is(err, jetstream.ErrKeyNotFound) {
			return errors.WrapTransient(err, "AliasIndex", "removeAliasForward", "delete alias")
		}
	}

	return nil
}

// updateReverseIndex updates the reverse index: entity--<entityID> -> []aliases
func (ai *AliasIndex) updateReverseIndex(ctx context.Context, entityID string, aliases []string) error {
	key := fmt.Sprintf("entity--%s", entityID)

	// Marshal aliases list
	data, err := json.Marshal(aliases)
	if err != nil {
		return errors.WrapInvalid(err, "AliasIndex", "updateReverseIndex", "marshal aliases")
	}

	// Store entity -> aliases mapping
	_, err = ai.bucket.Put(ctx, key, data)
	if err != nil {
		errMsg := fmt.Sprintf("store reverse index for entity %s", entityID)
		return errors.WrapTransient(err, "AliasIndex", "updateReverseIndex", errMsg)
	}

	return nil
}

// getEntityAliases retrieves aliases for an entity from reverse index
func (ai *AliasIndex) getEntityAliases(ctx context.Context, entityID string) ([]string, error) {
	key := fmt.Sprintf("entity--%s", entityID)

	entry, err := ai.bucket.Get(ctx, key)
	if err != nil {
		return nil, err
	}

	var aliases []string
	if err := json.Unmarshal(entry.Value(), &aliases); err != nil {
		return nil, errors.WrapInvalid(err, "AliasIndex", "getEntityAliases", "unmarshal aliases")
	}

	return aliases, nil
}

// findAliasesToRemove finds aliases that exist in old but not in new
func (ai *AliasIndex) findAliasesToRemove(existingAliases, newAliases []string) []string {
	newSet := make(map[string]bool)
	for _, alias := range newAliases {
		newSet[alias] = true
	}

	var toRemove []string
	for _, alias := range existingAliases {
		if !newSet[alias] {
			toRemove = append(toRemove, alias)
		}
	}

	return toRemove
}

// findAliasesToAdd finds aliases that exist in new but not in old
func (ai *AliasIndex) findAliasesToAdd(existingAliases, newAliases []string) []string {
	existingSet := make(map[string]bool)
	for _, alias := range existingAliases {
		existingSet[alias] = true
	}

	var toAdd []string
	for _, alias := range newAliases {
		if !existingSet[alias] {
			toAdd = append(toAdd, alias)
		}
	}

	return toAdd
}

// SpatialIndex handles geospatial indexing
type SpatialIndex struct {
	bucket      jetstream.KeyValue
	kvStore     *natsclient.KVStore
	metrics     *InternalMetrics
	promMetrics *PrometheusMetrics
	logger      *slog.Logger
	precision   int // Geohash precision (default: 7 = ~150m resolution)
}

// NewSpatialIndex creates a new SpatialIndex
func NewSpatialIndex(
	bucket jetstream.KeyValue,
	natsClient *natsclient.Client,
	metrics *InternalMetrics,
	promMetrics *PrometheusMetrics,
	logger *slog.Logger,
) *SpatialIndex {
	if logger == nil {
		logger = slog.Default()
	}

	var kvStore *natsclient.KVStore
	if natsClient != nil {
		kvStore = natsClient.NewKVStore(bucket)
	}

	return &SpatialIndex{
		bucket:      bucket,
		kvStore:     kvStore,
		metrics:     metrics,
		promMetrics: promMetrics,
		logger:      logger,
		// Default: ~150m resolution (4=~2.5km, 5=~600m, 6=~120m, 7=~30m, 8=~5m)
		precision: 7,
	}
}

// HandleCreate processes entity creation for spatial index
func (si *SpatialIndex) HandleCreate(ctx context.Context, entityID string, entityState interface{}) error {
	return si.updateSpatialIndex(ctx, entityID, entityState)
}

// HandleUpdate processes entity updates for spatial index
func (si *SpatialIndex) HandleUpdate(ctx context.Context, entityID string, entityState interface{}) error {
	return si.updateSpatialIndex(ctx, entityID, entityState)
}

// HandleDelete processes entity deletion for spatial index
func (si *SpatialIndex) HandleDelete(ctx context.Context, entityID string) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Remove from spatial index - would need to find which geohash contains this entity
	si.logger.Warn("Entity deleted - cleanup not fully implemented", "entity", entityID)
	return nil
}

// updateSpatialIndex updates spatial index if entity has position data
func (si *SpatialIndex) updateSpatialIndex(ctx context.Context, entityID string, entityState interface{}) error {
	state, ok := entityState.(*gtypes.EntityState)
	if !ok {
		return errors.WrapInvalid(errors.ErrInvalidData, "SpatialIndex", "HandleCreate", "invalid entity state type")
	}

	// Extract latitude and longitude from triples (TRIPLES ONLY - no fallbacks)
	var latitude, longitude, altitude *float64
	for _, triple := range state.Triples {
		switch triple.Predicate {
		case "geo.location.latitude":
			if lat, ok := triple.Object.(float64); ok {
				latitude = &lat
			}
		case "geo.location.longitude":
			if lon, ok := triple.Object.(float64); ok {
				longitude = &lon
			}
		case "geo.location.altitude":
			if alt, ok := triple.Object.(float64); ok {
				altitude = &alt
			}
		}
	}

	if latitude == nil || longitude == nil {
		return nil // No position data, skip spatial indexing
	}

	// Set default altitude if not provided
	altValue := 0.0
	if altitude != nil {
		altValue = *altitude
	}

	// Calculate geohash with configurable precision
	geohash := si.calculateGeohash(*latitude, *longitude, si.precision)

	// REFERENCE IMPLEMENTATION: Use KVStore.UpdateWithRetry for clean CAS operations
	// Multiple entities may share the same geohash, so we must merge, not overwrite
	if si.kvStore != nil {
		// Use the clean CAS pattern from natsclient
		err := si.kvStore.UpdateWithRetry(ctx, geohash, func(currentBytes []byte) ([]byte, error) {
			var spatialData map[string]interface{}

			if len(currentBytes) > 0 {
				// Existing geohash bucket - merge our entity into it
				if err := json.Unmarshal(currentBytes, &spatialData); err != nil {
					return nil, errors.WrapInvalid(
						err,
						"IndexManager",
						"updateSpatialIndex",
						"spatial data unmarshal failed",
					)
				}
			} else {
				// New geohash bucket - initialize structure
				spatialData = map[string]interface{}{
					"entities":    map[string]interface{}{},
					"last_update": time.Now().Unix(),
				}
			}

			// Get or create entities map
			entities, ok := spatialData["entities"].(map[string]interface{})
			if !ok {
				entities = map[string]interface{}{}
			}

			// Add/update this entity's position
			entities[entityID] = map[string]interface{}{
				"lat":     *latitude,
				"lon":     *longitude,
				"alt":     altValue,
				"updated": time.Now().Unix(),
			}

			spatialData["entities"] = entities
			spatialData["last_update"] = time.Now().Unix()

			// Return the updated data
			return json.Marshal(spatialData)
		})

		if err != nil {
			errMsg := fmt.Sprintf("update spatial index for geohash %s", geohash)
			return errors.WrapTransient(err, "SpatialIndex", "updateSpatialIndex", errMsg)
		}
		return nil
	}

	// Fallback to manual retry pattern if kvStore not available (for tests)
	var spatialData map[string]interface{}

	// Get existing data for this geohash
	entry, err := si.bucket.Get(ctx, geohash)
	if err == nil {
		// Existing geohash bucket - merge our entity into it
		if err := json.Unmarshal(entry.Value(), &spatialData); err != nil {
			return errors.WrapInvalid(err, "SpatialIndex", "updateSpatialIndex", "unmarshal existing spatial data")
		}
	} else {
		// New geohash bucket - initialize structure
		spatialData = map[string]interface{}{
			"entities":    map[string]interface{}{},
			"last_update": time.Now().Unix(),
		}
	}

	// Get or create entities map
	entities, ok := spatialData["entities"].(map[string]interface{})
	if !ok {
		entities = map[string]interface{}{}
	}

	// Add/update this entity's position
	entities[entityID] = map[string]interface{}{
		"lat":     *latitude,
		"lon":     *longitude,
		"alt":     altValue,
		"updated": time.Now().Unix(),
	}

	spatialData["entities"] = entities
	spatialData["last_update"] = time.Now().Unix()

	// Store updated spatial index
	data, err := json.Marshal(spatialData)
	if err != nil {
		return errors.WrapInvalid(err, "SpatialIndex", "updateSpatialIndex", "marshal spatial data")
	}

	// Use Update with revision for existing entries, Create for new ones
	if entry != nil {
		_, err = si.bucket.Update(ctx, geohash, data, entry.Revision())
		if errors.IsTransient(err) {
			// Revision mismatch - someone else updated, retry
			si.logger.Debug("Spatial index update conflict, retrying", "geohash", geohash)
			return si.updateSpatialIndex(ctx, entityID, entityState)
		}
	} else {
		_, err = si.bucket.Create(ctx, geohash, data)
		if errors.IsTransient(err) {
			// Someone else created it first, retry
			si.logger.Debug("Spatial index create conflict, retrying", "geohash", geohash)
			return si.updateSpatialIndex(ctx, entityID, entityState)
		}
	}

	return err
}

// calculateGeohash calculates a configurable-precision geohash for spatial indexing
func (si *SpatialIndex) calculateGeohash(lat, lon float64, precision int) string {
	// Configurable spatial binning based on precision:
	// precision=4: ~2.5km bins  (multiplier=10)
	// precision=5: ~600m bins   (multiplier=50)
	// precision=6: ~120m bins   (multiplier=100)
	// precision=7: ~30m bins    (multiplier=300)  <- Default
	// precision=8: ~5m bins     (multiplier=1000)

	var multiplier float64
	switch precision {
	case 4:
		multiplier = 10.0 // ~2.5km resolution
	case 5:
		multiplier = 50.0 // ~600m resolution
	case 6:
		multiplier = 100.0 // ~120m resolution
	case 7:
		multiplier = 300.0 // ~30m resolution (default)
	case 8:
		multiplier = 1000.0 // ~5m resolution
	default:
		multiplier = 300.0 // Default to precision 7
	}

	// Normalize coordinates to positive integers with precision-based binning
	latInt := int(math.Floor((lat + 90.0) * multiplier))
	lonInt := int(math.Floor((lon + 180.0) * multiplier))
	return fmt.Sprintf("geo_%d_%d_%d", precision, latInt, lonInt)
}

// TemporalIndex handles time-based indexing
type TemporalIndex struct {
	bucket      jetstream.KeyValue
	kvStore     *natsclient.KVStore
	metrics     *InternalMetrics
	promMetrics *PrometheusMetrics
	logger      *slog.Logger
}

// NewTemporalIndex creates a new TemporalIndex
func NewTemporalIndex(
	bucket jetstream.KeyValue,
	natsClient *natsclient.Client,
	metrics *InternalMetrics,
	promMetrics *PrometheusMetrics,
	logger *slog.Logger,
) *TemporalIndex {
	if logger == nil {
		logger = slog.Default()
	}

	var kvStore *natsclient.KVStore
	if natsClient != nil {
		kvStore = natsClient.NewKVStore(bucket)
	}

	return &TemporalIndex{
		bucket:      bucket,
		kvStore:     kvStore,
		metrics:     metrics,
		promMetrics: promMetrics,
		logger:      logger,
	}
}

// HandleCreate processes entity creation for temporal index
func (ti *TemporalIndex) HandleCreate(ctx context.Context, entityID string, entityState interface{}) error {
	return ti.updateTemporalIndex(ctx, entityID, entityState)
}

// HandleUpdate processes entity updates for temporal index
func (ti *TemporalIndex) HandleUpdate(ctx context.Context, entityID string, entityState interface{}) error {
	return ti.updateTemporalIndex(ctx, entityID, entityState)
}

// HandleDelete processes entity deletion for temporal index
func (ti *TemporalIndex) HandleDelete(ctx context.Context, entityID string) error {
	// Check for cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Remove from temporal index - complex operation not fully implemented in Phase 1
	ti.logger.Warn("Entity deleted - cleanup not fully implemented", "entity", entityID)
	return nil
}

// updateTemporalIndex updates temporal index with entity timestamp
func (ti *TemporalIndex) updateTemporalIndex(ctx context.Context, entityID string, entityState interface{}) error {
	state, ok := entityState.(*gtypes.EntityState)
	if !ok {
		return errors.WrapInvalid(errors.ErrInvalidData, "TemporalIndex", "HandleCreate", "invalid entity state type")
	}

	// Use entity's UpdatedAt timestamp
	timestamp := state.UpdatedAt

	// Create time bucket key (hour precision)
	timeKey := fmt.Sprintf("%04d.%02d.%02d.%02d",
		timestamp.Year(),
		timestamp.Month(),
		timestamp.Day(),
		timestamp.Hour())

	// REFERENCE IMPLEMENTATION: Use KVStore.UpdateWithRetry for clean CAS operations
	// Temporal indexes must accumulate ALL events for complete history
	if ti.kvStore != nil {
		// Use the clean CAS pattern from natsclient
		err := ti.kvStore.UpdateWithRetry(ctx, timeKey, func(currentBytes []byte) ([]byte, error) {
			var temporalData map[string]interface{}

			if len(currentBytes) > 0 {
				// Unmarshal existing data
				if err := json.Unmarshal(currentBytes, &temporalData); err != nil {
					return nil, errors.WrapInvalid(
						err,
						"IndexManager",
						"updateTemporalIndex",
						"temporal data unmarshal failed",
					)
				}
			} else {
				// Initialize new temporal data structure
				temporalData = map[string]interface{}{
					"events":       []interface{}{},
					"entity_count": 0,
				}
			}

			// Append new event to existing events
			events, _ := temporalData["events"].([]interface{})
			newEvent := map[string]interface{}{
				"entity":    entityID,
				"type":      "update",
				"timestamp": timestamp.Format(time.RFC3339),
			}

			// REFERENCE IMPLEMENTATION: Always append events for complete history
			// Temporal indexes should accumulate ALL events, not deduplicate
			// This allows queries like "what happened between time X and Y"
			events = append(events, newEvent)
			temporalData["events"] = events

			// Track unique entities for count (but keep all events)
			uniqueEntities := make(map[string]bool)
			for _, evt := range events {
				if eventMap, ok := evt.(map[string]interface{}); ok {
					if entity, ok := eventMap["entity"].(string); ok {
						uniqueEntities[entity] = true
					}
				}
			}
			temporalData["entity_count"] = len(uniqueEntities)

			// Return the updated data
			return json.Marshal(temporalData)
		})

		if err != nil {
			errMsg := fmt.Sprintf("update temporal index for time bucket %s", timeKey)
			return errors.WrapTransient(err, "TemporalIndex", "updateTemporalIndex", errMsg)
		}
		return nil
	}

	// Fallback to simple pattern if kvStore not available (for tests)
	var temporalData map[string]interface{}
	entry, err := ti.bucket.Get(ctx, timeKey)
	if err == nil {
		// Unmarshal existing data
		if err := json.Unmarshal(entry.Value(), &temporalData); err != nil {
			return errors.WrapInvalid(err, "TemporalIndex", "updateTemporalIndex", "unmarshal existing temporal data")
		}
	} else {
		// Initialize new temporal data structure
		temporalData = map[string]interface{}{
			"events":       []interface{}{},
			"entity_count": 0,
		}
	}

	// Append new event to existing events
	events, _ := temporalData["events"].([]interface{})
	newEvent := map[string]interface{}{
		"entity":    entityID,
		"type":      "update",
		"timestamp": timestamp.Format(time.RFC3339),
	}

	// REFERENCE IMPLEMENTATION: Always append events for complete history
	events = append(events, newEvent)
	temporalData["events"] = events

	// Track unique entities for count (but keep all events)
	uniqueEntities := make(map[string]bool)
	for _, evt := range events {
		if eventMap, ok := evt.(map[string]interface{}); ok {
			if entity, ok := eventMap["entity"].(string); ok {
				uniqueEntities[entity] = true
			}
		}
	}
	temporalData["entity_count"] = len(uniqueEntities)

	// Store updated temporal index
	data, err := json.Marshal(temporalData)
	if err != nil {
		return errors.WrapInvalid(err, "TemporalIndex", "updateTemporalIndex", "marshal temporal data")
	}

	_, err = ti.bucket.Put(ctx, timeKey, data)
	return err
}
