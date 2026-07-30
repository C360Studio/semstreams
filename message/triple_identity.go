package message

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Add-lane triple identity.
//
// The add lane (graph.mutation.triple.add / add_batch, hierarchy inference's
// in-process adder, foreign-edge regroup, the rule engine's add_triple, and
// pkg/projection's AppendEvidence) is append-only: it preserves multiple
// distinct values under one predicate, which is what multi-valued predicates
// such as hierarchy containment and sibling edges require. What it must NOT do
// is store the same assertion twice, because a producer that re-derives its
// facts on restart would then grow the entity on every restart.
//
// Identity for that purpose is exactly six fields — Subject, Predicate, Object,
// Datatype, Source, Context. Timestamp, Confidence, and ExpiresAt are
// deliberately excluded:
//
//   - Timestamp is the consequential exclusion. Producers stamp time.Now(), so
//     including it would mean identity never matched for anyone. Stored
//     Timestamp is therefore FIRST-assertion time, not latest-assertion time.
//   - Confidence is a float, so equality on it is fragile, and a
//     confidence-only refresh appending a second contradictory copy is not a
//     behavior worth preserving. Producers for which confidence is
//     identity-bearing already encode it into the predicate.
//   - ExpiresAt is excluded, which makes a re-assert-to-extend a no-op.
//     FORWARD HAZARD: triple TTL is currently unenforced (Triple.IsExpired has
//     no non-test consumers). If TTL enforcement ever lands, either revisit
//     this key or move TTL refresh onto a replace verb — do not discover this
//     by watching extensions silently fail.
//
// KNOWN HOLE, recorded so the next reader does not rediscover it: a STRUCT-
// valued Object never matches its own stored form. encoding/json emits a struct
// in field-declaration order, but that same value read back out of
// ENTITY_STATES is a map[string]any, which marshals in sorted-key order — two
// different keys for one fact. This is not a regression (the predecessor
// objectsEqual had the identical hole) and it fails in the safe direction: a
// missed suppression appends a duplicate, it never falsely suppresses. The fix,
// if it is ever worth paying for, is to normalize through a decode before
// keying. In practice Triple.Object is a scalar or an entity-ID string on every
// in-repo producer.
//
// These functions are I/O-free and are the single implementation shared by the
// graph-ingest server lane and the pkg/projection client, so the two cannot
// drift into "two definitions that agree today".

// AppendIdentityKey returns the canonical six-field add-lane identity key for a
// triple. Two triples denote the same assertion for append purposes if and only
// if their keys are equal; SameAppendTuple is defined in exactly those terms so
// a key-based set (O(n+k)) and a pairwise comparison can never disagree.
//
// The key is LENGTH-PREFIXED, not delimiter-joined. Predicates, sources, and
// contexts are arbitrary operator- and producer-supplied strings; joining them
// on a dot or a pipe would make ("a.b", "c") and ("a", "b.c") the same key —
// the raw-key-collision class (gh#741) that causes silent data loss elsewhere
// in the system. Length prefixes make the encoding injective for every possible
// field content, including embedded NUL bytes and embedded digits.
func AppendIdentityKey(t Triple) string {
	var b strings.Builder
	// Rough preallocation; the object encoding dominates and is unknown here.
	b.Grow(len(t.Subject) + len(t.Predicate) + len(t.Datatype) + len(t.Source) + len(t.Context) + 48)
	writeIdentityField(&b, t.Subject)
	writeIdentityField(&b, t.Predicate)
	writeIdentityField(&b, t.Datatype)
	writeIdentityField(&b, t.Source)
	writeIdentityField(&b, t.Context)
	writeIdentityField(&b, canonicalObjectKey(t.Object))
	return b.String()
}

// SameAppendTuple reports whether two triples are the same assertion on the add
// lane. It is exactly key equality — see AppendIdentityKey.
func SameAppendTuple(left, right Triple) bool {
	return AppendIdentityKey(left) == AppendIdentityKey(right)
}

// DedupeAppendTriples returns the subset of incoming triples that are not
// already present in stored and not repeated earlier within incoming itself,
// preserving first-input order, plus the number suppressed.
//
// Both the stored set and the within-request collapse are seeded into one hash
// set, so the cost is O(len(stored) + len(incoming)) rather than the O(n*k) a
// pairwise scan would cost. That matters precisely where duplicate suppression
// fires hardest: a hierarchy container entity accumulates one containment edge
// per child, so its stored triple count grows with the population it contains.
//
// stored may be nil, which reduces this to a within-request collapse.
func DedupeAppendTriples(stored, incoming []Triple) (survivors []Triple, suppressed int) {
	if len(incoming) == 0 {
		return nil, 0
	}
	seen := make(map[string]struct{}, len(stored)+len(incoming))
	for i := range stored {
		seen[AppendIdentityKey(stored[i])] = struct{}{}
	}
	survivors = make([]Triple, 0, len(incoming))
	for i := range incoming {
		key := AppendIdentityKey(incoming[i])
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		survivors = append(survivors, incoming[i])
	}
	return survivors, len(incoming) - len(survivors)
}

// writeIdentityField appends one length-prefixed field to the key.
func writeIdentityField(b *strings.Builder, field string) {
	b.WriteString(strconv.Itoa(len(field)))
	b.WriteByte(0)
	b.WriteString(field)
}

// canonicalObjectKey renders Triple.Object (typed as any) into a canonical
// string so that values which compare equal on the wire share one key rather
// than splitting the keyspace. JSON is the canonical form because every stored
// triple round-trips through JSON on its way into ENTITY_STATES: a stored
// int(85) and an incoming float64(85) are the same stored fact and must produce
// the same key. encoding/json sorts map keys, so object encodings are stable.
//
// An object that cannot be marshalled cannot be persisted either, so the
// fallback only has to be conservative: it renders the Go value, which keeps
// distinct unmarshalable values distinct (no false suppression). The "!" prefix
// cannot collide with any JSON encoding, none of which begins with that byte.
//
// Relation to the nine-field sameFullTriple's objectsEqual, which is the union
// DeepEqual ∪ jsonEqual: this key is jsonEqual alone, i.e. a strict SUBSET, so
// it is the STRICTER of the two. Where they diverge (0.0 vs -0.0: equal under
// ==, so DeepEqual, but "0" vs "-0" in JSON) the key distinguishes values the
// other calls identical. That asymmetry is the safe one — the lane can only
// ever fail to suppress, never suppress something a caller considers distinct.
// See also the struct-ordering hole in the package comment above.
func canonicalObjectKey(object any) string {
	encoded, err := json.Marshal(object)
	if err != nil {
		return "!" + fmt.Sprintf("%#v", object)
	}
	return string(encoded)
}
