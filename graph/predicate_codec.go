package graph

import "encoding/hex"

// EncodePredicateToken hex-encodes a predicate for use as a single key token in
// the INCOMING / NAME / CONTEXT composite-key reverse indexes (gh#474 Codex P1a).
//
// graph-ingest accepts any non-empty predicate, including values that are not
// NATS-KV-key-safe (spaces, unicode, wildcard tokens). A raw predicate token would
// make those Puts fail while the hashed PREDICATE_INDEX and raw ENTITY_STATES /
// OUTGOING paths succeed — silently desyncing the forward and reverse views.
//
// Hex (not a hash) is deliberate: it is reversible, so a reader reconstructs the
// exact predicate from the key with no per-row value lookup — keeping INCOMING a
// pure prefix key-scan on its hot path. Hex is a fixed-alphabet ([0-9a-f]),
// dot-free encoding, so an encoded predicate is always exactly one KV-safe token
// regardless of the dots or unsafe bytes in the original.
//
// Shared here (not in the graph-index package) because graph-clustering and other
// reverse-index readers reconstruct these keys and must decode with the identical
// codec.
func EncodePredicateToken(predicate string) string {
	return hex.EncodeToString([]byte(predicate))
}

// DecodePredicateToken reverses EncodePredicateToken. Returns (predicate, true) on
// success, ("", false) when the token is not valid hex (a malformed key token the
// caller should skip rather than surface as data).
func DecodePredicateToken(token string) (string, bool) {
	raw, err := hex.DecodeString(token)
	if err != nil {
		return "", false
	}
	return string(raw), true
}
