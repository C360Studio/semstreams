package graphindex

import (
	"errors"

	"github.com/c360studio/semstreams/graph"
)

// Predicate tokens in the INCOMING, NAME, and CONTEXT composite keys are
// hex-encoded via the shared graph.EncodePredicateToken codec (gh#474 Codex P1a).
// These thin wrappers keep the graph-index call sites terse; the rationale and the
// canonical implementation live in graph/predicate_codec.go (shared so
// graph-clustering and other reverse-index readers decode identically).
func encodePredicateToken(predicate string) string { return graph.EncodePredicateToken(predicate) }

func decodePredicateToken(token string) (string, bool) { return graph.DecodePredicateToken(token) }

// errIndexWritePartial is the sentinel returned when one or more required
// reverse-index writes/retractions fail during an entity update (gh#474 Codex
// P1b). It must propagate to the caller so readiness is withheld — a derived
// projection that silently drops writes but reports "caught up" is the failure
// class ADR-066 exists to prevent.
var errIndexWritePartial = errors.New("graph-index: one or more required index writes failed")

// errAuthoritativeIdentityMismatch marks an ENTITY_STATES row whose canonical
// value ID does not equal its canonical KV key. The revision cannot be completed:
// projecting it under either identity would corrupt owner-scoped indexes.
var errAuthoritativeIdentityMismatch = errors.New("graph-index: authoritative entity key/value identity mismatch")
