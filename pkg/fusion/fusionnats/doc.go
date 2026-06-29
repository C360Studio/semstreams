// Package fusionnats is the production NATS implementation of
// fusion.RetrievalClient (ADR-062 increment "B2"): it wires the engine's
// resolve/expand/status surface onto the existing public graph query subjects
// so the lens-driven fusion engine can run against a live graph.
//
// It is a SUBPACKAGE of pkg/fusion deliberately: pkg/fusion is a near-leaf
// (message only, no NATS) so the engine stays deterministic and unit-testable
// without a live graph. This package is where the engine touches the wire — it
// imports natsclient (via a minimal requester interface) and the graph query
// contracts, and returns the leaf fusion types.
//
// Subject mapping (all PUBLIC graph.query.* / graph.index.query.* subjects, the
// stable external surface a standalone fusion service can reach):
//
//	Status              -> graph.index.query.status   (gh#397)
//	Resolve(symbol)     -> graph.query.byName         (gh#376)
//	Resolve(prefix)     -> graph.query.prefix
//	Resolve(nl)         -> graph.query.semantic
//	Entity              -> graph.query.entity
//	Entities            -> graph.query.batch
//	Neighbors           -> graph.query.relationships
//	Names               -> graph.query.byName         (matched names for did_you_mean)
//
// Every call uses RequestClassified, so a classified handler error (ADR-060)
// surfaces as a typed *errs.ClassifiedError rather than a silently-decoded
// "error: " body. Entity translates the entity_not_found classified error into
// (nil, nil) to honor the interface's absence contract; every other error
// propagates so the engine never mistakes a backend fault for a not-found.
package fusionnats
