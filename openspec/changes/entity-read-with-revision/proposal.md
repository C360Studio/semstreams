# Proposal — entity-read-with-revision (gh#851)

## Why

The public CAS contract is half-shipped. The mutation side is complete and typed:
`graph.UpdateEntityWithTriplesRequest.ExpectedRevision` opts into single-pass CAS
(`graph/mutation_requests.go:102-108`), mutation receipts return the committed `KVRevision`
(`graph/mutation_responses.go:78`), and `revision_mismatch` is the one designated control-flow
sentinel (`processor/graph-ingest/mutations.go:1181-1183`). But the read side never crosses the
wire: the authoritative `graph.ingest.query.entity` handler reads `entry.Revision`, uses it for
poison bookkeeping, and returns only `entry.Value` (`processor/graph-ingest/query.go:87-104`).
The only public revision-bearing read in the framework is `lifecycle.Manager.GetWithRevision` —
workflow-scoped, requiring a registered lifecycle participant and a phase triple.

Consequently `MutationRevisionConflict` is defined and mapped in the projection client
(`pkg/projection/mutation_client.go:487-488`) yet **unreachable**: no client method ever sets
`ExpectedRevision` (`ReplaceOwned` builds its request without it, `mutation_client.go:711-718`).
A public read-modify-write caller cannot use the CAS primitive without binding the
framework-owned `ENTITY_STATES` bucket directly — an ownership violation.

Downstream, this blocks SemMachina `mystery-companion-acceptance` task 8.1: the companion hint
ladder must reset exactly when a newly committed knowledge grant becomes authoritative, and
without read-revision + conditional mutation, a delayed unconditioned reset can move the ladder
backward. Their process-local keyed lock cannot make active-active consumers safe.

## What Changes

- **Revision-bearing authoritative read** on the existing `graph.ingest.query.entity` subject
  via a request opt-in: a caller asking for the revision receives the entity **and** the KV
  revision of the exact entry whose bytes produced it. The bare response shape is preserved for
  every existing caller; no new subject is introduced (deliberate — the gh#810 rework is
  mid-flight building a declared-subject registry, and this change must not grow that surface
  under it).
- **The projection client's authoritative read returns the revision** (additive variant), and
  `ReplaceOwnedMutation` gains an optional `ExpectedRevision`, passed through to the wire —
  making the already-mapped `MutationRevisionConflict` reachable and the read → conditional
  write → typed mismatch → refetch → retry loop expressible entirely through public API.
- **Production-wire proof**: an integration test drives the full loop over NATS — read with
  revision, two competing conditional writes, exactly one winner, loser refetches and
  succeeds. (The existing `cas_integration_test.go` exercises the handler seam in-process,
  not the wire.)
- **Documentation**: the versioned read-modify-write example gh#851 requests.

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `graph-ingest`: the authoritative entity query lane gains the opt-in revision-bearing
  response (the query lane has no spec requirement today; this seeds it).
- `projection-mutation-client`: "Authoritative read-back" gains the revision-bearing read;
  "Schema-derived owned replacement" gains the optional expected-revision condition and the
  reachable revision-conflict outcome.

## Impact

- `processor/graph-ingest/query.go` — opt-in response envelope.
- `graph/` query request/response types — additive fields.
- `pkg/projection` — `ReadAuthoritative` revision variant; `ReplaceOwnedMutation.ExpectedRevision`.
- Consumers: **SemMachina** (task 8.1 hint-ladder reset, active-active ladder advancement —
  the named blocked caller), **gated-dag** (gh#689's claim primitive consumes this read in its
  own change), any sister implementing conditional state transitions. Existing bare-read
  consumers are untouched (opt-in).
- Version skew: a new client against an old server receives the bare shape and must treat it
  as "revision unavailable" — covered by a scenario.

## Non-goals

- **The reusable claim primitive gh#689 requests.** This change ships the missing *read half*
  and the minimal conditional pass-through; the contract-bound claim (owner-token-required CAS
  transition, ambiguity read-back, conditional unclaim, gated-dag migration) is its own change
  consuming this surface. This is the explicit answer to gh#851's "is this the reusable CAS
  surface" question: it is the substrate for it, not the whole of it.
- **Batch/prefix revision-bearing reads.** gh#851 asks whether batch reads carry revisions:
  not in this change — no named consumer needs per-entity revisions in batch shapes
  (SemMachina's reset is a single-entity read-modify-write), and prefix/batch responses add
  per-entity weight for every caller. Recorded so the reader is not left inferring.
- **Changing merge semantics, lease enforcement, or immutability policy** — gh#818's
  territory, deliberately disjoint.
