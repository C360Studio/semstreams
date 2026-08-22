# ADR-097: Export the Lesson Contract Snapshot, Preserve Local Curator Composition

## Status

**Accepted** — 2026-08-22 after historical-constructor correction review.

## Context

SemStreams owns the built-in lesson-record projection declaration in an `internal` package. External products
construct local projection clients and inject narrow reconcile/read capabilities into `LessonCurator`, but cannot
import that declaration.

A valid copied contract can classify a lesson birth predicate as lifecycle-mutable. A later transition can delete the
birth fact and report verified success.

SemStreams previously exported `NewNATSLessonCurator`. Commit `9a48638d` deliberately removed it after migrating
then-current callers and recording bounded zero-reference and parity evidence. That helper wrapped legacy raw mutation
and query adapters; it did not own a projection contract. The later graph-foundation decision retained local
composition-root clients and narrow curator interfaces.

After those decisions, semdev became a production curator adopter and had to hand-mirror the private lesson contract.
This new evidence supersedes the prior no-external-consumer premise but does not require reversing local client
composition.

## Decision

SemStreams exposes one purpose-scoped `LessonProjectionContract()` function returning an independent snapshot of the
canonical lesson-record projection contract.

Products include the snapshot in their composition-root-local projection mutation client and inject only
`PredicateReconciler` and `AuthoritativeReader` into `LessonCurator`.

The canonical declaration has one implementation source shared by the internal built-in aggregate and the public
snapshot. The built-in aggregate remains internal. `NewNATSLessonCurator` remains retired.

Generic projection contracts remain caller-local. Graph-ingest gains no projection-contract identity or immutable
predicate policy. This change introduces no bespoke agent, LLM persona, prompt role, or framework agent type.

## Consequences

- External products no longer copy framework-owned lesson contract literals.
- Products retain one local client and narrow consumer dependencies.
- The public snapshot is a cross-repository API and returns independent nested slices.
- Existing `NewLessonCurator` callers remain source-compatible.
- Existing mirror callers opt into the new snapshot; doing nothing retains drift risk.
- No NATS factory, hidden client, wire, storage, configuration, or global contract catalog is added.
- Broader immutable-birth enforcement remains with #818.
- SemStreams publishes migration guidance but does not edit sister repositories.
