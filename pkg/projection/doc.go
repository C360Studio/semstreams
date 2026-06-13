// Package projection implements the ADR-056 Decision 6 graph projection
// contract — the missing middle layer between "what type is flowing" (the
// payload registry) and "who may author these facts at runtime" (pkg/ownership).
//
// A Contract is declared ONCE, beside the type / Graphable / gateway-resource
// projection code, and answers: "what graph facts does this projection emit,
// and in what write mode is each predicate group?" Ownership claims are then
// DERIVED from the contract and bound to an owner id at boot — they are not
// hand-maintained as a parallel registry that drifts from the projection.
//
//	payload type registration
//	  └─ optionally declares a projection.Contract
//	       (entity pattern · predicates × write mode · foreign-edge claims · indexing profile)
//	component / gateway boot
//	  └─ projection.Bind(ctx, ownerRegistry, ownerID, contracts...)
//	       derives the ownership.OwnerClaim/ForeignEdgeClaim set and registers it
//	graph-ingest
//	  └─ enforces the registered claims at the write boundary (a later increment)
//
// This package is the DERIVATION + DECLARATION layer. It does not enforce
// anything — pkg/ownership is the enforcement substrate, and graph-ingest is the
// write-boundary enforcer. Manual ownership.RegisterOwner remains the low-level
// escape hatch for owners whose pattern is not derivable from a single payload
// type (the lifecycle Manager) or for migration scaffolding.
//
// A Contract carries no CoordinationWaiver: a legitimately-overlapping
// projection (the cross-product / mutual-consent case) is an explicit NON-GOAL
// of the contract layer and drops to manual ownership.RegisterOwner. If
// contract-declared overlaps ever become common, that is a signal to add waiver
// support here rather than let owners drift off the derivation path.
//
// See docs/adr/056-authoritative-semantic-state.md Decision 6.
package projection
