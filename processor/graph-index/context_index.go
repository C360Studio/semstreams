package graphindex

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"

	"github.com/c360studio/semstreams/message"
)

// contextHashHex returns the fixed-width, dot-free hex token used as the
// CONTEXT_INDEX key prefix for a context value.
//
// Hashing is required, not a style choice: raw context values are a dotted,
// self-nesting vocabulary (e.g. "inference.hierarchy" vs
// "inference.hierarchy.deep"), and NATS KV's KeysByPrefix wildcarding matches
// on token position, not string prefix — a raw dotted prefix would silently
// absorb membership for any context value that shares a dot-token prefix with
// it. A fixed-width hex digest can't collide that way. See ADR-065 (the same
// argument applies identically to CONTEXT as to PREDICATE_INDEX and NAME_INDEX).
func contextHashHex(contextValue string) string {
	sum := sha256.Sum256([]byte(contextValue))
	return hex.EncodeToString(sum[:])
}

// contextIndexKey builds the CONTEXT_INDEX composite key for one
// (contextValue, entityID, predicate) triple:
// key = hash(contextValue) + "." + entityID + "." + predicate.
//
// Unconditional Put, no CAS — ADR-065 footgun comment: each (contextValue,
// entityID, predicate) triple is globally unique; no two callers ever contend
// on the same key. The raw Get+Put sequence this replaces had a lost-update
// race between concurrent writers; per-key unconditional Puts eliminate it by
// construction (design.md, CONTEXT row).
//
// Retraction tradeoff (retention, gh#433 / ADR-073): the replaced merge-list
// writer removed the entity's prior entries before re-adding current ones, so a
// re-index of C:{p1,p2}→C:{p1} pruned the p2 membership. Per-key Puts do NOT
// retract a superseded (context,entity,predicate) key — it leaks until an owner
// cleans it. This is why a tombstone carrying only last-known triples is
// insufficient (it can't name the orphaned p2 key); cleanup needs a durable
// per-entity reverse projection. Harmless today only because CONTEXT has no
// production reader (design.md D2).
func contextIndexKey(contextHash, entityID, predicate string) string {
	return contextHash + "." + entityID + "." + predicate
}

// contextIndexPrefix builds the KeysByPrefix argument for enumerating all
// CONTEXT_INDEX entries for a given context value.
func contextIndexPrefix(contextValue string) string {
	return contextHashHex(contextValue) + "."
}

// contextIndexValue is the JSON value stored at each CONTEXT_INDEX composite
// key. The raw context value is NOT recoverable from the sha256 prefix alone
// (a property shared with NAME_INDEX's name hash), so it rides in the value.
type contextIndexValue struct {
	Context string `json:"context"`
}

// validateContextKeyInputs checks that entityID and predicate are valid for
// constructing a CONTEXT_INDEX composite key. Returns false and logs at Debug
// if validation fails (structural guard, not a doc caveat — design.md D1).
func validateContextKeyInputs(entityID, predicate string, logger *slog.Logger) bool {
	if !message.IsValidEntityID(entityID) {
		logger.Debug("context index: invalid entity ID, skipping",
			slog.String("entity_id", entityID))
		return false
	}
	if predicate == "" {
		logger.Debug("context index: empty predicate would produce trailing-dot key, skipping",
			slog.String("entity_id", entityID))
		return false
	}
	return true
}
