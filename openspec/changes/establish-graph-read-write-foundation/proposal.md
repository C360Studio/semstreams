# Establish the graph read/write foundation

## Why

Graph reads and writes are SemStreams' defining capability, but their current contract is obscured by two overlapping
ideas: graph-ingest is the physical writer of `ENTITY_STATES`, while `pkg/ownership` attempts to authorize semantic
predicate writers through claims, leases, heartbeats, tokens, foreign-edge modes, and boot wiring. The latter predicts
permission before a write instead of relying on explicit operation semantics and observed storage outcomes. It adds
substantial complexity without preventing the real lost-update path between Graphable ingest and RPC mutations.

The accepted inventory measured 4,599 lines in `pkg/ownership`, 866 more in `OwnershipService`, eight mutation subjects,
undeclared mutation-handler ports, divergent caller contracts, unconditional existing-key `Put` paths, and authority
reads that omit the KV revision. The approved design removes semantic ownership and replaces it with a smaller
foundation:
typed component ports, four explicit request/reply mutations, exact reads carrying their same-entry revision, and
atomic Create/CAS writes on every authority lane.

## What Changes

- Make a typed `nats-request` component port with interface `semstreams.graph.mutation` v1 and family
  `graph.mutation.>` the canonical mutation API declaration.
- Replace eight mutation subjects with strict create, revision-fenced reconcile, partial per-subject append, and
  revision-fenced delete.
- Add one exact entity result carrying the canonical entity and same-entry KV revision through GraphQL and one narrow
  embedded adapter.
- Require atomic `Create` for birth and observed-revision CAS for every existing-key `ENTITY_STATES` write across
  Graphable ingest, RPC mutation, and hierarchy inverse writes.
- Keep opt-in hierarchy on Graphable ingest only, with atomic inferred-container birth and CAS inverse writes.
- Treat a relationship to an absent object as valid eventual graph state; report absence on dereference and create no
  target stub, pending queue, or repair subsystem.
- Retain projection contracts as local mutation schemas while deleting global semantic owner claims, leases, tokens,
  presence, heartbeats, foreign-edge modes, and overlap enforcement.
- Remove the ownership package, service, buckets, configuration, schemas, metrics, wiring, and obsolete tests.
- Supersede the ownership portions of ADR-055, ADR-056, ADR-058, and ADR-060 through ADR-091 while preserving their
  still-valid fact/request, lifecycle-composition, and classified-error decisions.
- Deliver runtime work in one draft implementation PR and one coordinated breaking merge; no half-migrated wire contract
  lands on `main`.

## Non-goals

- No event sourcing, CQRS runtime, command ledger, mutation stream, outbox, or exactly-once claim.
- No checkpoint, backup, restore, recovery gate, attestation, or recovery-orchestration capability.
- No leader election, multi-writer fencing, distributed lock, or multi-process graph-ingest support.
- No referential stub, pending-edge queue, automatic target birth, rollback protocol, or global broken-reference halt.
- No compatibility handler, alias subject, dual wire shape, online migration, or mixed-version guarantee.
- No general embedded graph client, MCP graph-read contract, or raw-KV application fallback.
- No downstream repository edits. A bounded grep census and migration notice are communication only and cannot redesign
  or block the accepted foundation.
- No unrelated issue-queue work during this program.

## Consumers

Every SemStreams adopter that declares graph mutation ports or consumes the exact entity contract is affected. The ten
known sister repositories—semdev, semmachina, semsource, semboids, semdragon, semstreams-ui, semteams, semconnect,
semlink, and semops—form a communicate-only holdout census. Their code is outside this change.

## Impact

- **Breaking API/wire impact:** eight subjects become four typed operations; old request fields and response shapes are
  removed; exact GraphQL entity reads add `kvRevision`.
- **Runtime impact:** graph-ingest remains the sole physical writer; all existing-key writes become CAS protected.
- **Storage impact:** `OWNER_CLAIMS`, `OWNER_PRESENCE`, and the declaration-only `PENDING_EDGES` spelling are removed.
  No new bucket or stream is introduced.
- **Complexity impact:** production code must be net-negative after generated artifacts are excluded. A net-positive
  result returns to design review with line-by-line justification.
- **Delivery impact:** this foundation-record change is documentation/specification only. Runtime implementation follows
  in one draft cutover PR after this record merges.
