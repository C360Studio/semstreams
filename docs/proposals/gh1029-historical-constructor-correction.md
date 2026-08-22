# Issue #1029 Historical Constructor Correction

Status: accepted owner correction pending independent design review.

Baseline: `7b6ff1e1a2718b4dd3087904748c296cb73215d2`.

This artifact corrects the accepted #1029 inventory/design only where they treated
`NewNATSLessonCurator` as a novel additive surface. The concrete drift trace, six-producer plus Graphable census,
and #818 boundary remain accepted.

## Historical inventory correction

`NewNATSLessonCurator` was introduced in commit `338e847e` and deliberately removed in `9a48638d`.

The retired implementation was:

```go
func NewNATSLessonCurator(client *natsclient.Client, logger *slog.Logger) *LessonCurator {
    return NewLessonCurator(NewNATSOwnedFactWriter(client), NewNATSLessonReader(client), logger)
}
```

Evidence:

- `git show 9a48638d^:processor/agentic-tools/lesson_promotion.go:76-81`
- `openspec/changes/archive/2026-07-30-public-projection-mutation-client/tasks.md:128-132,204-214`
- `openspec/changes/archive/2026-07-30-public-projection-mutation-client/specs/projection-mutation-client/spec.md:511-543`
- `openspec/changes/archive/2026-08-07-establish-graph-read-write-foundation/design.md:396-434`

The removal followed a bounded zero-reference/parity audit and migrated the curator to direct narrow
`PredicateReconciler` and `AuthoritativeReader` capabilities over a composition-root-local client.

The prior #1029 inventory excluded archived changes and searched only current source. It correctly found no current
factory, but incorrectly inferred no historical ruling.

## Changed evidence

The old zero-reference evidence was true when measured. It ceased to answer the adopter question after:

- semdev added its literal lesson-contract mirror on 2026-08-16;
- semdev added live production `NewLessonCurator` wiring on 2026-08-21; and
- the #1029 census measured the mirror, wrapper, production curator, and literal conformance pin.

semdev followed the retained architecture—one local client and narrow injection—but had to copy an unimportable
framework declaration. That is new evidence against the no-external-consumer premise, not evidence against narrow
injection or local client composition.

## Retired and proposed factory comparison

| Dimension | Retired factory | Rejected #1029 factory draft |
|---|---|---|
| Adapter | Legacy raw owned writer plus raw reader | Private `projection.MutationClient` |
| Contract authority | None | `builtinprojection.Contracts()` |
| Return | One value | Value plus error |
| Client ownership | Hidden raw adapters | Hidden second local client |
| Historical status | Explicitly removed | Reused retired name incompatibly |

The draft was not code-identical resurrection, but it reused a retired exported name with incompatible source shape
and reversed the retained composition decision.

## Options

1. **Do nothing:** preserves history but leaves the semdev copy and silent-drift risk.
2. **Revive the old name:** strongest encapsulation, but hides a client, reverses two accepted decisions, and reuses an
   incompatible historical signature.
3. **Use a new factory name:** avoids name collision but still reverses local-client/narrow-injection architecture.
4. **Export one purpose-scoped contract snapshot:** preserves local construction and narrow injection while removing
   all copied literals.

## Corrected decision

Choose option 4.

Add:

```go
// LessonProjectionContract returns an independent snapshot of the canonical
// projection contract required by LessonCurator lifecycle mutations.
func LessonProjectionContract() projection.Contract
```

Internally, `internal/builtinprojection.Contracts()` and the public adapter derive from the same canonical lesson
contract function. Every call returns independent top-level and nested slices.

`NewLessonCurator` remains unchanged and supported. `NewNATSLessonCurator` remains absent.

No generic catalog, NATS factory, hidden client, wire change, graph-ingest policy, lifecycle/rule/gated-DAG/Graphable
change, bespoke agent, LLM persona, prompt role, or framework agent type is introduced.

## Corrected adopter seam

A standard product composition root:

1. includes `agentictools.LessonProjectionContract()` in its local client's complete contract set; and
2. injects that client through `NewLessonCurator`'s narrow capabilities.

It no longer knows the contract/group names, entity pattern, 11/3 predicate split, or omission set.

A missing snapshot remains a first-use typed error through the explicit constructor. A duplicate or invalid snapshot
fails local client construction at boot. Existing mirror users keep compiling but retain drift risk until migration.

## Corrected TDD slice

1. RED: `LessonProjectionContract()` is undefined.
2. Prove it exactly equals the internal canonical lesson declaration.
3. Mutate the returned contract and every nested slice; prove a later call is unchanged.
4. Build the real integration-test client at composition from the public snapshot.
5. Inject it through `NewLessonCurator`.
6. Retain the causal promote/retire/supersede proof for all 11 birth object sets and lifecycle sibling cleanup.
7. Keep the E2E binary's shared composition-root client and narrow injection.
8. Retain the ops identity assertions and nine-stage assertion accounting.

## Corrected OpenSpec target

- The framework exposes a purpose-scoped function returning an independent canonical lesson-record contract snapshot.
- External composition can include that snapshot without copied literals.
- `LessonCurator` continues to receive only `PredicateReconciler` and `AuthoritativeReader`.
- `NewNATSLessonCurator` remains retired.
- All 11 birth predicates survive lifecycle transitions and lifecycle siblings remain complete-set reconciled.

## Owner supersession ruling

Accepted by the primary owner session on 2026-08-22, subject to independent review:

1. The prior claim that `NewNATSLessonCurator` was novel is withdrawn.
2. Commit `9a48638d`'s helper retirement and the later local-client/narrow-injection decision remain binding.
3. New semdev evidence supersedes only the old no-external-consumer premise.
4. `NewNATSLessonCurator` MUST remain retired.
5. SemStreams MAY add `agentictools.LessonProjectionContract() projection.Contract`.
6. The public snapshot and internal built-in aggregate MUST share one canonical declaration and return independent
   nested slices.
7. `NewLessonCurator` remains the supported narrow constructor.
8. Generic mutation semantics and #818 policy do not change.
9. #1029 does not claim external retirement until semdev adopts the released snapshot.
10. No bespoke agent/persona/role surface is introduced.

## ADR correction

ADR-097 records: export the lesson contract snapshot and preserve local curator composition. It must cite the retired
factory history and explicitly state that the new semdev evidence supersedes the no-consumer premise without
superseding narrow injection or local client construction.

## Release and verification

The accessor is additive and does not reuse a retired name. It changes no state, wire, subject, bucket, stream,
payload, or schema. No migration/fresh-state cutover is required. `task e2e:ops` remains the proportional gate.
SemStreams publishes migration instructions but does not edit sister repositories.
