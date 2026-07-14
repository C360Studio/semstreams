package graph

import "github.com/c360studio/semstreams/message"

// StubMessageType is the envelope of a referential-integrity stub: the entity
// graph-ingest materializes at a referenced-but-not-yet-born ID so a
// relationship's target always resolves to a node (ADR-056 Decision 4 lane-ii).
// A stub is PROFILE-LESS and carries only the three stub-marker triples below;
// no domain content and no depends_on edges.
//
// At the referenced entity's TRUE BIRTH the real producer's write overwrites
// this envelope with its own MessageType — but ONLY on a re-stamping lane:
// fact-arrival merge (unconditional overwrite), or create_with_triples /
// update_with_triples carrying a NON-ZERO MessageType (preserve-when-zero,
// non-zero wins). A birth via triple.add / add_batch, or a zero-typed
// update_with_triples, does NOT re-stamp the envelope — so a producer that
// wants its entity to stop reading as a stub must birth it on a re-stamping
// lane with its real type.
//
// Consumers that ENUMERATE entities — notably the gated-DAG executor, which
// reads every entity under a unit prefix — MUST treat a stub as not-yet-real:
// dispatching a stub would bypass dependency ordering because it carries no
// depends_on edges (gh#429). Use [EntityState.IsStub].
var StubMessageType = message.Type{Domain: "core", Category: "identity.stub", Version: "v1"}

// Stub-marker predicates carried by a referential-integrity stub. Exported so
// the producer (graph-ingest) and enumerating consumers share one definition.
const (
	// PredStubMarker marks the entity as a referential-integrity stub. Note:
	// this triple PERSISTS after real birth (nothing removes it), so it is NOT a
	// reliable "still a stub" signal — use [EntityState.IsStub] (envelope-based).
	PredStubMarker = "core.identity.stub"
	// PredStubReferencedBy records the entity ID whose relationship triple
	// caused this stub to be materialized.
	PredStubReferencedBy = "core.identity.referenced-by"
	// PredStubOwner records the stub's owner identity (the referencing entity's
	// type key, or the framework referential producer for an untyped source).
	PredStubOwner = "core.identity.stub-owner"
)

// IsStub reports whether this entity is still a bare referential-integrity stub
// — one whose envelope is StubMessageType, i.e. no real producer has re-stamped
// it yet. It keys on the ENVELOPE, not the PredStubMarker triple: the triple
// persists after real birth, whereas the envelope is overwritten by the real
// producer at birth, so the envelope is the signal that correctly flips from
// stub → real. See gh#429 and [StubMessageType] for the re-stamping-lane
// contract.
func (es EntityState) IsStub() bool {
	return es.MessageType.Equal(StubMessageType)
}
