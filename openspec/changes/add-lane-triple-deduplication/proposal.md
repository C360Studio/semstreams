## Why

A graph-ingest restart against an existing graph appends already-present triples and advances
entity revisions even when nothing in the world changed. gh#713 measured it in the field: a
seed-disabled restart of the same immutable image moved six model-registry hierarchy entities
(containers `6 -> 9`, endpoints `4 -> 6`, `3 -> 5`, `2 -> 4`), and repeating the restart repeats
the append. Semdragon's replay-parity gate is blocked on it, and it will not unblock by adding a
readiness signal — a perfectly quiesced comparison of a corrupted store still fails.

**The mechanism, verified from code, is not what gh#713 suspected.** It named
`graph/inference/hierarchy.go`'s three `tripleAdder.AddTriple` sites as the "suspected surface".
Those are where the writes land, but the trigger is upstream and is an asymmetry between two
sibling lanes:

- `createEntity` calls `GetHierarchyTriples` **unconditionally**, with no absence gate, at
  `processor/graph-ingest/component.go:2542` — and **before** the KV write at `:2574`/`:2581`.
  That call is not a pure read: it commits container-inverse edges (`hierarchy.go:368`) and
  sibling-inverse edges (`hierarchy.go:313`) as side effects through `tripleAdder`.
- On an already-present ID, `Create` returns `natsclient.ErrKVKeyExists` and `createEntity`
  returns the sentinel early at `:2575-2579` — **the inverse edges have already committed.**
- `MergeEntity`, by contrast, *does* gate hierarchy behind an absence probe (`:2391`), which is
  why the fact lane does not re-fire and the request lane does.

That arithmetic reproduces gh#713's reported revision deltas exactly: three re-registered
endpoints give each container three inverse-`contains` appends (`6 -> 9`), each endpoint two
inverse-`sibling` appends from its two siblings (`4 -> 6`, `3 -> 5`, `2 -> 4`), and each
endpoint's own create is a no-write 409 (zero self-revisions).

**gh#697's original scope would not have fixed this.** It is written against the
`graph.mutation.triple.add_batch` request handler and conditions dedup on a non-empty
`RequestID` matching each `Triple.Context`. Both halves miss:

1. **Wrong lane.** There are two CAS append bodies sharing no code — `Component.AddTriple`
   (closure `component.go:2995`, blind append `:3012`) and `Component.AddTriples` (closure
   `:3124`, blind append `:3140`). gh#713's writes arrive via `tripleAdderAdapter.AddTriple`
   (`:527`) into the **first**; `add_batch` reaches only the second.
2. **The condition never fires.** Hierarchy stamps `Context: "inference.hierarchy"`
   (`hierarchy.go:298, :309, :349, :364`) and never a request ID, so a request-ID-scoped dedup
   is inert against exactly the traffic that produced the defect.

Both corrections follow the owner's triage on gh#713 (2026-07-28): dedup on the full tuple
**unconditionally**, and short-circuit **before** the write so a fully-duplicate group advances
no revision. That triage's governing instruction is to fix the **lane, not the consumer** — a
hierarchy-local exists-check patches one caller and leaves the class open for every replaying
inference or projection consumer.

**One correction to the triage's reasoning, recorded so it is not carried forward.** It argued
the identity tuple from gh#713's evidence ("duplicates are identical across all six fields
including confidence"). Hierarchy hard-codes `Confidence: 1.0`, leaves `Source` empty and
`Timestamp` zero (`hierarchy.go:299, :310, :350, :365`), so gh#713 is fixed by *any* key from six
to nine fields — it does not discriminate. The real basis is that **merged code already defines
this key**: `sameAppendTuple` (`pkg/projection/mutation_client.go:1324`) matches on exactly
Subject, Predicate, Datatype, Source, Context, and `objectsEqual(Object)`, with the nine-field
`sameFullTriple` (`:1333`) reserved for replace/create verification. A server-side key that
differs from the client's creates a drift class between the two.

## What Changes

- **Deduplicate the add lane by six-field tuple, unconditionally**, inside both CAS closures,
  covering `graph.mutation.triple.add`, `graph.mutation.triple.add_batch`, hierarchy inference,
  ADR-056 foreign-edge regroup, the rule engine's `add_triple`, agentic tool/loop writers, and
  `pkg/projection`'s `AppendEvidence` — every add-lane emitter, from one placement.
- **A fully-duplicate write commits nothing**: no `kv.Update`/`Create`, therefore no revision
  advance and no `ENTITY_STATES` watcher fire; no `Version++`, no `UpdatedAt`, no error counter.
  Modeled on the existing `errNoOpRemove` sentinel (`component.go:3202`), whose comment already
  records the hazard being avoided: "`return current, nil` would be an identity rewrite (revision
  bump + watcher re-fire), not a skip".
- **Identity excludes `Confidence`, `Timestamp`, and `ExpiresAt`**, from one shared
  implementation used by both client and server.
- **`WrittenCount` counts only newly appended tuples.** A fully-duplicate batch returns
  `WrittenCount: 0` with empty `FailedSubjects` and no error — a previously impossible state. An
  additive `Deduplicated` count is returned so a caller can distinguish it from "nothing
  happened" without a read-back.
- **Fix merged client code this breaks.** `appendFactsPresent`
  (`pkg/projection/mutation_client.go:1272`) verifies evidence by consuming matches from a
  **multiset**: two identical evidence triples require two stored copies. Under dedup only one
  exists, so verification returns `found < 0` and the append is reported as
  `CommitNotCommitted` with a fatal `MutationInternal`. `canonicalizeAppend` (`:1157`) must
  collapse duplicates preserving first-input order, and the check must move to set presence.
- **Suppressed duplicates are observable** via a lane-labeled counter, so a silent skip is
  distinguishable from absent traffic.
- **Correct a false statement in the `graph-ingest` spec.** The requirement at
  `openspec/specs/graph-ingest/spec.md:6` closes with "This matches the mutation (`AddTriples`)
  lane's merge semantics." That has never been true on merged main — `AddTriples` blind-appends
  (`component.go:3140`); it does not replace per `(subject, predicate)`. After this change the
  two lanes are still not the same thing (exact-tuple dedup is not predicate-level replacement),
  so the clause is replaced rather than repaired.

## Non-Goals

- **Hierarchy's unconditional re-fire is not fixed here.** Dedup makes the duplicate *writes*
  free but leaves the reads — `createEntity` still runs `GetHierarchyTriples`, an O(N)
  `ListWithPrefix` (`hierarchy.go:281`) plus three container existence reads, on every
  re-registration. Filed separately; folding it in is precisely the fix-the-consumer scope the
  triage rejected.
- **No backfill.** Entities already carrying duplicates keep them and stay readable. No
  duplicate rule is added to `ValidateEntityStateContract` — that would poison every entity that
  already accumulated duplicates. gh#713's parity is measured from a fresh volume (its repro step
  1); a dirty store's remedy is wipe and reseed, the established pre-v1 posture.
- **The Graphable merge lane and whole-candidate create are untouched.** `MergeEntity` resolves
  by predicate-level replacement and `createEntity` writes a caller-supplied candidate; a
  candidate carrying its own internal duplicates still commits them.
- **`update_with_triples` / `create_with_triples` are replace verbs** and are out of scope.

## Coordination

The unarchived change `public-projection-mutation-client` (70/71, gh#700 open) carries two
statements this change falsifies, in
`openspec/changes/public-projection-mutation-client/specs/projection-mutation-client/spec.md`:

- `:343-345` — "Deployments requiring strict no-retry behavior MUST configure `Retry.MaxRetries=0`
  **until the server-side idempotency primitive tracked by issue #697 exists**."
- `:355` — "**AND** the result remains vulnerable to the original attempt committing late and
  double-applying."

`projection-mutation-client` is not a seeded capability home in `openspec/specs/`, so this change
cannot express a MODIFIED delta against it. **Owner routes these two corrections to that thread**
before or during its archive. This change does not edit another thread's change directory.

## Impact

**BREAKING.** A relevant e2e tier must be green before merge (CLAUDE.md). Grounds: a
previously-impossible success state on a cross-repo mutation contract; reversal of a documented
client guarantee sister repos coded against; repeated identical assertions silently collapse, so
any producer relying on multiplicity loses data; and behavior of merged client code flips.

Tier: **`task e2e:structural`** — `validate-hierarchy-inference` is gated on
`structural`/`statistical`/`semantic` (`test/e2e/scenarios/tiered.go:261`) and is absent from
`e2e:core`. The per-PR CI `e2e:statistical` also covers it.

gh#713's restart-replay regression belongs in integration, not e2e, which is what the issue asks
for; homes exist at `processor/graph-ingest/hierarchy_integration_test.go` and
`hierarchy_sync_integration_test.go`.

Closes gh#697. Closes gh#713.
