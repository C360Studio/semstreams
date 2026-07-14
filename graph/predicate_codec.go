package graph

import "encoding/hex"

// EncodePredicateToken hex-encodes a predicate for use as a single key token in
// the INCOMING / NAME / CONTEXT composite-key reverse indexes (gh#474 Codex P1a).
//
// Canonical predicates are validated before persistence and are NATS-key-safe,
// but their three semantic segments contain two dots. Encoding the raw predicate
// in a composite key would consume three physical tokens and change the fixed
// token positions and arity used by INCOMING / NAME / CONTEXT filters.
//
// Hex (not a hash) is deliberate: it is reversible, so a reader reconstructs the
// exact predicate from the key with no per-row value lookup — keeping INCOMING a
// pure prefix key-scan on its hot path. Hex is a fixed-alphabet ([0-9a-f]),
// dot-free encoding, so an encoded predicate is always exactly one KV-safe token.
// This is only a physical codec; it provides no compatibility path and does not
// weaken canonical predicate validation.
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
