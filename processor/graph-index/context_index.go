package graphindex

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"

	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// contextHashHex returns the fixed-width, dot-free hex token used as the
// CONTEXT_INDEX key's context axis.
//
// Hashing is required, not a style choice: raw context values are a dotted,
// self-nesting vocabulary (e.g. "inference.hierarchy" vs
// "inference.hierarchy.deep"), so a raw context token would collide across
// dot-token prefixes. A fixed-width hex digest can't. The raw context value is not
// recoverable from the digest, so it rides in the row value (contextIndexValue).
func contextHashHex(contextValue string) string {
	sum := sha256.Sum256([]byte(contextValue))
	return hex.EncodeToString(sum[:])
}

// contextIndexKey builds the CONTEXT_INDEX composite key for one
// (entityID, contextValue, predicate) triple:
// key = entityID + "." + hash(contextValue) + "." + hex(predicate).
//
// The ENTITY is the key PREFIX (gh#474 Codex P1f). CONTEXT has no production
// reader (design.md D2), so nothing needs a by-context scan; keying by entity
// instead makes the index self-reconciling and self-cleaning, which the former
// hash(context)-prefix layout could not do:
//   - update: prefix-scan "entityID." to retract the entity's superseded
//     memberships before writing its current ones (contextIndexEntityPrefix);
//   - delete: prefix-scan "entityID." to remove the entity's whole keyset.
//
// The reconcile-delete-then-write is bounded by ONE entity's context memberships
// (small), not the O(fan-in) shared-context list the pre-gh#474 writer merged —
// so it does not reintroduce the CAS-contention class this change removed.
//
// The predicate is hex-encoded (encodePredicateToken) for KV-safety, decoded back
// in contextEntryFromKey. The raw context value is not recoverable from its hash,
// so it rides in the value.
func contextIndexKey(entityID, contextHash, predicate string) string {
	return entityID + "." + contextHash + "." + encodePredicateToken(predicate)
}

// contextIndexEntityPrefix is the KeysByPrefix argument enumerating every
// CONTEXT_INDEX entry OWNED BY entityID (all contexts, all predicates). Used by
// the update-reconcile and delete paths.
func contextIndexEntityPrefix(entityID string) string {
	return entityID + "."
}

func contextIndexEntityFilter(entityID string) string {
	return contextIndexEntityPrefix(entityID) + ">"
}

// contextIndexValue is the JSON value stored at each CONTEXT_INDEX composite key.
// The raw context value is not recoverable from the sha256 key token, so it rides
// here (a property shared with NAME_INDEX's name hash).
type contextIndexValue struct {
	Context string `json:"context"`
}

// contextEntryFromKey reconstructs (entityID, predicate) from a CONTEXT_INDEX
// composite key "entityID.hash(context).hex(predicate)". entityID is the first 6
// dot-separated tokens; the sha256 token and the hex predicate token are each
// dot-free. The raw context is not in the key — read it from the row value.
// Returns ("", "", false) when the key is malformed.
func contextEntryFromKey(key string) (entityID, predicate string, ok bool) {
	// parts[0..5] = entityID tokens, parts[6] = context hash, parts[7] = hex predicate.
	parts := strings.SplitN(key, ".", 8)
	if len(parts) < 8 {
		return "", "", false
	}
	entityID = strings.Join(parts[:6], ".")
	predicate, decoded := decodePredicateToken(parts[7])
	if !decoded || predicate == "" || !message.IsValidEntityID(entityID) {
		return "", "", false
	}
	return entityID, predicate, true
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
		logger.Debug("context index: empty predicate, skipping",
			slog.String("entity_id", entityID))
		return false
	}
	if _, err := vocabulary.ParsePredicate(predicate); err != nil {
		logger.Debug("context index: invalid predicate, skipping",
			slog.String("entity_id", entityID), slog.Any("error", err))
		return false
	}
	return true
}
