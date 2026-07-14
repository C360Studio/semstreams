package graphindex

import (
	"log/slog"
	"strings"

	"github.com/c360studio/semstreams/graph"
	"github.com/c360studio/semstreams/message"
	"github.com/c360studio/semstreams/vocabulary"
)

// incomingIndexMarker is the fixed, content-free value stored at every
// INCOMING_INDEX composite key. Empty by construction, not a timestamp:
// the worker pool that drives indexing has no cross-entity completion order,
// so a timestamp would invite a future reader to treat it as "most recent,"
// which this design does not guarantee (ADR-065).
var incomingIndexMarker = []byte{}

// incomingIndexKey builds the INCOMING_INDEX composite key for one directed
// edge: key = targetID + "." + sourceID + "." + hex(predicate).
//
// The predicate is hex-encoded (encodePredicateToken, gh#474 Codex P1a): a
// free-form predicate can contain KV-unsafe bytes, and a raw token would make the
// Put fail asymmetrically vs the hashed PREDICATE_INDEX. Hex keeps it reversible,
// so the reader recovers the predicate from the key with no value lookup — INCOMING
// stays a pure prefix key-scan (empty row value).
//
// Using the raw targetID as the scan prefix is collision-safe: a valid 6-token
// entity ID (dot-separated, enforced by IsValidEntityID) cannot be a NATS
// token-level prefix of another valid entity ID — there is no valid ID that
// starts with another valid ID followed by a dot-token. This is the same
// reasoning graph-ingest applies for its handleQueryPrefixNATS prefix scans
// (design.md D1).
//
// Unconditional Put, no CAS — ADR-065 footgun comment: each (targetID,
// sourceID, predicate) triple is globally unique; no two callers ever contend
// on the same key, so CAS read-modify-write buys nothing and costs an extra
// round-trip per edge. If this bucket's writes ever need CAS again, that means
// the key-uniqueness invariant no longer holds — don't "fix" this back to
// UpdateWithRetry without re-establishing it.
func incomingIndexKey(targetID, sourceID, predicate string) string {
	return targetID + "." + sourceID + "." + encodePredicateToken(predicate)
}

// incomingIndexPrefix builds the KeysByPrefix argument that enumerates every
// incoming edge whose target is targetID: all keys of the form
// "targetID.sourceID.predicate".
func incomingIndexPrefix(targetID string) string {
	return targetID + "."
}

// incomingIndexSourceFilter enumerates every assertion owned by sourceID across
// all targets: target6.source6.hex(predicate).
func incomingIndexSourceFilter(sourceID string) string {
	return strings.Repeat("*.", 6) + sourceID + ".*"
}

// incomingEntryFromKey reconstructs a graph.IncomingEntry from a composite
// INCOMING_INDEX key. The key format is "targetID.sourceID.predicate", where
// targetID is stripped as the known prefix. The source entity ID is exactly 6
// dot-separated tokens; the predicate is everything after the 6th token.
//
// Returns (entry, true) on success, (zero, false) when the key is malformed
// (too short, invalid sourceID, or empty predicate after splitting).
func incomingEntryFromKey(key, targetID string) (graph.IncomingEntry, bool) {
	prefix := incomingIndexPrefix(targetID)
	if !strings.HasPrefix(key, prefix) {
		return graph.IncomingEntry{}, false
	}
	suffix := key[len(prefix):]

	// suffix = "sourceID.hex(predicate)"; sourceID is exactly 6 dot-separated
	// tokens; the hex predicate token is dot-free by construction. SplitN with
	// n=7 yields parts[0..5] = sourceID tokens, parts[6] = the hex predicate.
	parts := strings.SplitN(suffix, ".", 7)
	if len(parts) < 7 {
		return graph.IncomingEntry{}, false
	}
	sourceID := strings.Join(parts[:6], ".")
	predicate, ok := decodePredicateToken(parts[6])
	if !ok || predicate == "" {
		return graph.IncomingEntry{}, false
	}
	if !message.IsValidEntityID(sourceID) {
		slog.Debug("incoming index: invalid source entity ID parsed from key",
			slog.String("key", key),
			slog.String("source_id", sourceID))
		return graph.IncomingEntry{}, false
	}
	return graph.IncomingEntry{
		FromEntityID: sourceID,
		Predicate:    predicate,
	}, true
}

// validateIncomingKeyInputs checks that targetID, sourceID, and predicate are
// all valid for constructing an INCOMING_INDEX composite key. Returns false and
// logs at Debug if validation fails. This is structural, not a doc caveat
// (design.md D1): the source axis is only upstream-validated, so an invalid
// entity ID reaching this layer is a real gap, not a normal operating
// condition.
func validateIncomingKeyInputs(targetID, sourceID, predicate string, logger *slog.Logger) bool {
	if !message.IsValidEntityID(targetID) {
		logger.Debug("incoming index: invalid target entity ID, skipping",
			slog.String("target_id", targetID))
		return false
	}
	if !message.IsValidEntityID(sourceID) {
		logger.Debug("incoming index: invalid source entity ID, skipping",
			slog.String("source_id", sourceID))
		return false
	}
	if predicate == "" {
		logger.Debug("incoming index: empty predicate would produce trailing-dot key, skipping",
			slog.String("target_id", targetID),
			slog.String("source_id", sourceID))
		return false
	}
	if _, err := vocabulary.ParsePredicate(predicate); err != nil {
		logger.Debug("incoming index: invalid predicate, skipping",
			slog.String("target_id", targetID),
			slog.String("source_id", sourceID),
			slog.Any("error", err))
		return false
	}
	return true
}
