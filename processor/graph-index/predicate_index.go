package graphindex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// predicateIndexMarker is the fixed, content-free value stored at every
// PREDICATE_INDEX composite key. Empty by construction, not a timestamp:
// the worker pool that drives indexing has no cross-entity completion
// order, so a timestamp would invite a future reader to treat it as
// "most recent," which this design does not guarantee (ADR-065).
var predicateIndexMarker = []byte{}

// predicateHashHex returns the fixed-width, dot-free hex token used as the
// PREDICATE_INDEX key prefix for a predicate.
//
// The hash+catalog representation remains the active ADR-065 format while the
// fixed-nine-token candidate is measured under the graph-index reconciliation
// decision gate. Canonical predicate enforcement is independent of this codec.
func predicateHashHex(predicate string) string {
	sum := sha256.Sum256([]byte(predicate))
	return hex.EncodeToString(sum[:])
}

// predicateIndexKey builds the PREDICATE_INDEX composite key for one
// (predicate, entity) membership pair: hash(predicate) + "." + entityID.
func predicateIndexKey(predicate, entityID string) string {
	return predicateHashHex(predicate) + "." + entityID
}

// predicateIndexPrefix builds the KeysByPrefix argument that enumerates
// every entity currently carrying the given predicate.
func predicateIndexPrefix(predicate string) string {
	return predicateHashHex(predicate) + "."
}

// predicateIndexEntityFilter enumerates the complete predicate membership set
// owned by one entity under the current hash(predicate).entity6 layout.
func predicateIndexEntityFilter(entityID string) string {
	return "*." + entityID
}

// entityIDFromPredicateKey strips a known predicate's hash prefix from a
// PREDICATE_INDEX composite key, recovering the entity ID. The hash
// prefix is fixed-width, so the split is unambiguous regardless of the
// entity ID's own token count.
func entityIDFromPredicateKey(key, predicate string) string {
	return strings.TrimPrefix(key, predicateIndexPrefix(predicate))
}

// updatePredicateCatalog records that predicate has been seen, so
// handleQueryPredicateListNATS can recover human-readable predicate names
// (and serve namespace-prefix queries) without ever prefix-matching the
// membership bucket itself. Idempotent and cheap: an unconditional Put
// keyed on the raw, unhashed predicate name. Safe as a key here (unlike
// on the membership bucket) because PREDICATE_CATALOG carries no
// membership data — a stray prefix match can only surface a superset of
// predicate *names* sharing a namespace, never corrupt which entities
// carry which predicate. See ADR-065.
func (c *Component) updatePredicateCatalog(ctx context.Context, predicate string) error {
	if c.predicateCatalogBucket == nil {
		return nil
	}
	_, err := c.predicateCatalogBucket.Put(ctx, predicate, predicateIndexMarker)
	return err
}
