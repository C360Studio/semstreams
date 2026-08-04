# Design — graph-state foundation (GS-00)

## 1. Design posture

SemStreams is an offline-first, edge-capable, tiered semantic graph framework.
The design optimizes first for predictable results, easy adoption, and a mental
model a developer can hold without reading implementation packages.

ADR-090 rejects event sourcing and a general CQRS runtime. GS-00 therefore shares
contract vocabulary and conformance questions, not execution machinery.

Every shape in this document is a non-normative acceptance candidate. GS-00 has
no spec delta. A candidate becomes normative only in its bounded GS-01+ change
with representation, implementation, conformance evidence, promotion, and
archive.

## 2. Authority and revision

`ENTITY_STATES` is current state with history 1. A revision is evidence about the
specific current entity KV entry returned or committed. It is not:

- a global graph transaction number;
- a retained command sequence;
- an authority-recovery ledger; or
- proof that any derived view has observed the write.

The target authoritative read shape is conceptually:

```text
AuthoritativeRead[T]
  entity_id
  value T
  revision uint64
  outcome found | not_found | poison | unavailable | canceled
```

`value` and `revision` are present together only for `found`. Implementations may
use existing result and error types when they preserve those semantics; GS-00 does
not authorize a second wrapper solely to match this spelling.

Acceptance examples for GS-01 are: found returns value and revision from one
authority read; not-found fabricates neither; poison stays distinct from absence;
and history 1 provides no prior-version or disaster-recovery API.

GS-01 also delivers and verifies the coordinated authority snapshot/restore
runbook for `ENTITY_STATES`, referenced content, and
`GRAPH_INGEST_APPLIED_SEQ` reset-or-restore. Fact-stream replay is not authority
disaster recovery.

## 3. Mutation outcome

The target mutation result has two independent axes:

```text
MutationOutcome
  entity_id
  request_id optional transport echo
  commit_state committed | not_committed | unknown
  committed_revision present only with causal committed reply
  reason classified

AuthorityObservation optional
  read_outcome found | not_found | poison | unavailable | canceled
  observed_revision present only when found
  intent_state satisfied | divergent | indeterminate
```

- `committed` means this request is attributed to the commit and includes the
  exact resulting entity revision.
- `not_committed` means no authority write is attributed to this request. A no-op
  is explicit and never presented as committed success.
- `unknown` means transport ended before attribution was known. It remains
  unknown even when a later authoritative observation satisfies the intent.
- Authority observation describes only current state. It never attributes a
  prior commit through content, timestamp, provenance, request-ID echo, or a view.
- Request ID is transport correlation unless a separately designed durable
  receipt contract is accepted. GS-00 creates no receipt store or lookup API.

The existing commit-state vocabulary should be reused or narrowed if it can
express this contract. A new exported hierarchy must prove it deletes more public
surface than it adds. GS-02 retires the atomic-create content-match upgrade and
renames or retires `CommitVerified`; it does not preserve misleading vocabulary.

GS-02 covers both graph-write seams: retention-bounded, redeliverable at-least-once
`Graphable` fact delivery with stream acceptance and observability but no
synchronous authority receipt or authority-recovery promise, and typed semantic
mutation with classified commit knowledge. Adopters
choose by intent, never transport spelling; raw subjects stay internal.

GS-02 also moves authoritative semantic write intents out of the misleading
`pkg/projection` namespace into a graph-write/semantic-mutation home and removes
the old mixed name under the pre-v1 clean break.

Acceptance examples for GS-02 include causal committed reply, explicit no-op,
lost reply, and a later competing commit with matching content. In both ambiguous
cases commit knowledge remains unknown; observation may say current intent is
satisfied but cannot attribute the commit. Atomic-create content matching no
longer upgrades success, and request-ID echo creates no lookup contract.

## 4. Read model

Adopters make two separate decisions:

1. Choose a front door: a currently admitted remote HTTP operation or an embedded
   typed operation when one exists, a declared dependency seam for projection
   owners, or raw KV diagnostics for operators. No general typed reactive
   subscription or MCP graph front door is implemented.
2. Use the operation's answer source. Today the entity query returns a value
   without revision and view behavior is operation-specific. GS-01 targets
   authority value plus revision; GS-03 through GS-10 target owner status.

Exact-entity authority may be exposed through an admitted remote operation,
named typed adapter, or both. A caller does not select a bucket or protocol to
change truth semantics. The operation contract names its source and therefore
what its answer proves.

The current remote surface is a hand-written GraphQL-shaped facade, not a real
schema executor, and the current `graph/query.Client` mixes typed RPC with direct
KV reads. GS-12 makes the required gateway conformant GraphQL and retires or
internalizes the aggregate client. Current documentation may not promise schema
validation, selection projection, or a complete subject-hiding embedded client.

### Reactive subscription boundary

GS-00 does not admit or schedule a general typed reactive subscription. Raw
watches stay within an authority or projection owner, or operator diagnostics;
they are not an adopter fallback. `pkg/graphview` has no production graph-state
adopter and is precedent evidence, not justification.

An owner-specific typed observation operation requires a fresh named-adopter
census, a before/after public-surface and adopter-decision table, and owner
acceptance proving less knowledge and total surface. That bounded proposal owns
its representation and conformance. GS-03 instead owns role declaration
representation and conformance.

## 5. Role and lifecycle declaration

The role candidate asks each current role instance to declare consumer,
dependencies, trigger/reread behavior, ownership, removal, failure, repair,
rebuild, instance model, currency, read behavior, offline behavior, fallback, and
side effects. GS-03 defines representation, a commit-pinned census, and
conformance without creating a general subscription or shared runtime.

Allowed variation is deliberate:

- required query views may use authority revisions and exact coverage;
- optional enrichment may use work state or capability availability;
- periodic partitions may use cycle completion and wall-clock staleness;
- internal dedup or reverse bookkeeping inherits its owning capability;
- serving caches may be rebuilt from their durable owner and need not publish an
  authority watermark.

Every declaration still answers removal, dependency change, failure, repair,
reset, instance model, offline behavior, and read behavior.

GS-03 also applies the deletion test to `COMPONENT_STATUS`: delete it, or retain
it only with a named semantic consumer and status contract. GS-04 through GS-10
cannot exit until each durable owner proves enforced single-active deployment or
owner-specific active/active safety.

GS-01 adopts authority startup validation. GS-03 dispositions reactive consumers
and serving caches. GS-04 through GS-07 prove required-view,
internal-accelerator, and reverse-bookkeeping roles one owner at a time. GS-08
proves optional embedding with inherited dedup. GS-09 proves periodic community
and summary/serving-cache roles; GS-10 proves effectful inference.

GS-01 also proves graph-ingest single-active enforcement or accepted
active/active safety; GS-02 re-verifies that proof under both mutation seams.

Every GS-03 census row names its exact later owner increment, is deleted or made
internal-only, or proves conformance in GS-03. “Dispositioned” without an
implementation/conformance home is not an exit state.

Readiness uses the strongest unit each owner can prove: authority revision
coverage for graph-index, work/capability state for embedding, and cycle plus
wall-clock staleness for periodic clustering. No owner fabricates a common
watermark.

## 6. Three-owner evidence gate

Graph-index, graph-embedding, and graph-clustering are the proof set because they
cover required views, optional enrichment, and periodic second-order projection.
They share lifecycle questions but not a single mechanism.

No shared runtime proposal is admissible until declarations and conformance work
show the same bootstrap, ordering, removal, repair, watermark, and reset mechanism
in all three owners. It also needs repeated-cost evidence and a before/after table
for one outside component author. The table counts public types,
subjects/buckets, config knobs, lifecycle choices, failure/recovery choices, and
documents to read. A prototype must reduce total decisions and production lines
without owner-specific escape hatches.

Each surviving view family in GS-04 through GS-10 also implements a
capability-scoped operator rebuild with completion/readiness evidence and a
semantic comparison proving expected keys and stale-key absence. GS-10 removes
graph-gateway direct `ANOMALY_INDEX` writes and retires the
`graph.events.relationship.create` applier unless an explicit producer/consumer
contract and authority bridge are accepted.

Any surviving GS-10 inference application declares its live authorization
condition, durable correlation/idempotency, loop bound, typed authoritative
mutation outcome with revision evidence, and failure independently from anomaly
detection.

## 7. Offline and tiers

- Current authority and Tier 0 graph relationships remain usable with local NATS.
- Tier 1 statistical capability remains local and optional.
- Tier 2 external semantic services add capability without becoming authority.
- A missing optional tier returns capability-unavailable or the declared lower
  tier, never a fabricated empty graph.
- Rebuild is effect-free. Authorized inference application is a separate
  operation with correlation, idempotency, and loop bounds.

## 8. Holdouts and tag migration

Ten downstream projects are frozen holdouts during the SemStreams foundation
program. Their current usage is migration evidence, not a design constraint. The
program records the Foundation tag-candidate gate `PASS`, produces the exact
approved tag candidate, migrates all ten in the approved window, then takes stock
of missing seams.

This avoids ten repeated censuses and prevents partial downstream migrations from
voting the framework back toward a retired anti-pattern.

## 9. Complexity gate

The baseline is 14 durable derived catalog descriptors and 31 concept documents
at `1c17958a`. GS-00 adds no runtime surface and no concept document.

Later increments must report deltas for public types, subjects, buckets, config
knobs, concept files, and production lines. New concepts normally consolidate or
delete existing conceptual surface. A new bucket or protocol needs a named
semantic consumer and complete lifecycle contract.

GS-13 produces a 31-row concept disposition manifest and one default path of at
most five documents. That path teaches governed graph identity, history-1 current
authority, the two write seams, named view ownership/lifecycle, and front-door vs
answer-source separation. Advanced algorithms and tuning move to advanced or
operations material; normative mechanics move to specs; historical decisions stay
in ADR/archive/migration records.

GS-12 first dispositions every remote and embedded read method. The required
graph gateway becomes a conformant GraphQL parser/schema executor with truthful
introspection, validation, selection projection, and GraphQL errors. The
aggregate `graph/query.Client` is retired or internalized to declared owner seams;
only measured operation-specific typed adapters survive. GS-12 archives before
GS-13 concept consolidation begins.

## 10. Deterministic E2E proof harness

GS-11 owns fixed inputs/seeds and spec-owned deterministic population and
assertion invariants for every relevant E2E tier. It eliminates the known
statistical-tier population drift and absorbs or explicitly dispositions relevant
suspended test work without creating a second executable increment. The final
three clean isolated runs are a tag-candidate gate, not each slice's edit loop.

## 11. Foundation tag-candidate gate

GS-14 cannot release a foundation tag candidate, and holdout migration cannot
begin, until the canonical program records `PASS` for all of these conditions:

1. GS-01 through GS-13 are accepted and implemented, with target requirements
   promoted or archived as current truth where appropriate.
2. Authority-read, mutation-outcome, and role/lifecycle conformance
   suites are green.
3. Authority snapshot/restore proves coordinated `ENTITY_STATES`, referenced
   content, and ingest-guard reset-or-restore; `COMPONENT_STATUS` is dispositioned;
   every durable owner proves single-active enforcement or accepted active/active
   safety; and every surviving family proves scoped rebuild and stale-key removal.
   Every accepted role family/current instance has a disposition and applicable
   conformance evidence.
4. Each relevant E2E tier names a fixed input/seed and spec-owned deterministic
   population and assertion invariants. Three consecutive clean isolated runs on
   the same commit have identical seeded input population and assertion counts,
   with all deterministic invariants green. External-model output may use bounded
   semantic assertions, but harness population/case counts must still match; any
   count drift blocks `PASS`.
5. The final complexity report is accepted and concept consolidation is complete.
6. Every remote method and embedded method/subject has an accepted
   admit/internalize/rewrite/retire disposition.
7. The registered `/mcp` config, handler, and OpenAPI advertisement are removed
   or replaced by a specified implementation, making discovery truthful.
8. Canonical documentation and decision skills match the verified runtime.
9. Effect-free rebuild is separate from authorized inference application;
   graph-gateway no longer writes `ANOMALY_INDEX`, and the relationship-create
   event applier is retired unless its bridge contract was accepted. Any surviving
   inference application proves live authorization, durable idempotency, loop
   bounds, typed mutation outcome/revision, and distinct failure.
10. The required gateway passes GraphQL parser/schema, introspection, selection,
    variable, and error conformance. The aggregate client is retired or
    internalized; only measured named adapters remain.
11. The owner approves the exact tag/version and coordinated migration window.

The `PASS` record links each evidence item and names the approved tag/version and
window. This is a SemStreams release gate, not a per-slice downstream census or a
holdout vote on architecture.

## 12. Alternatives rejected

### Build a generic durable-view runtime now

Rejected because the three owners use different ordering and currency units. A
shared noun is not evidence of a shared mechanism.

### Let each issue choose its own contract

Rejected because the issue queue already demonstrates repeated local answers to
the same missing primitives.

### Make downstream migration a gate for each slice

Rejected because it multiplies WIP and lets historical usage preserve the defects
the foundation is intended to remove.

### Rewrite all concept documentation in GS-00

Rejected because a 31-file rewrite is not reviewable evidence. GS-00 corrects
only contradictions; a bounded inventory and consolidation plan precedes the tag.

## 13. Owner choices not required for GS-00

These decisions remain owner-controlled but do not block accepting the foundation
vocabulary:

1. The exact foundation tag version and migration window, which must be resolved
   before the Foundation tag-candidate gate can pass.
2. Whether `ANOMALY_INDEX` survives after its ownership and effect boundary is
   repaired.

Implementation-level reuse of existing Go result types is an architecture review
question, not a reason to invent a new owner choice.
