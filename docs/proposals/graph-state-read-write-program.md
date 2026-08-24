# Graph state read/write foundation program

**Status:** RETIRED 2026-08-24 — historical. See [Program closure](#program-closure) at the end of this file.

**Decision records:** [ADR-090](../adr/090-authoritative-current-state-and-materialized-views.md) and
[ADR-091](../adr/091-graph-mutation-authority-without-semantic-ownership.md).

**Executable baton:**
[establish-graph-read-write-foundation](../../openspec/changes/archive/2026-08-07-establish-graph-read-write-foundation/).

**Frozen evidence:** [inventory](graph-state-read-write-inventory.md), the accepted change inventory, and the
content-addressed review/approval records inside the executable baton.

This was the program summary while active; it is no longer routing. Task truth lives in the archived changes. ADRs record
the decisions; archived changes and rejected designs are history, not executable work.

## Identity and priorities

SemStreams is an **offline-first, edge-capable, tiered semantic graph framework**. It is not an event-log product, a
general CQRS runtime, or a collection of storage adapters presented as architecture.

Priorities are ordered:

1. predictable graph results;
2. pragmatic operation on an edge node with local or clustered NATS;
3. easy adoption through one default path;
4. easy comprehension by a developer who has not read the implementation;
5. optional statistical and semantic enhancement without weakening lower tiers;
6. performance after the contract is understandable and correct.

An optimization, abstraction, compatibility layer, issue-queue patch, or downstream usage pattern does not outrank these
priorities.

## Binding foundation

- `ENTITY_STATES` is canonical current shared semantic state with history 1.
- Graph-ingest is its sole physical writer. Physical/catalog responsibility is not semantic predicate authority.
- Any component may request one of four admitted typed mutations through its declared `nats-request` port.
- Entity birth uses atomic Create. Every existing-key authority write uses observed-revision CAS.
- Exact entity reads return the canonical entity and same-entry KV revision.
- Relationship-object absence is valid eventual graph state and is reported on dereference.
- Opt-in hierarchy remains on Graphable ingest only; inferred containers use Create and inverse writes use CAS.
- Local projection contracts validate mutation shape but grant no global permission.
- Semantic claims, leases, tokens, presence, heartbeats, foreign-edge modes, overlap enforcement, and referential stubs
  are deleted.
- `GRAPH_STATUS` retains readiness and poison semantics. Missing references do not become global failure.
- There is no SemStreams checkpoint/restore product, event sourcing, CQRS runtime, leader election, exactly-once ledger,
  pending-edge queue, or compatibility layer.
- One graph-ingest process is the supported topology. NATS clustering remains supported.
- Remote exact reads use GraphQL; embedded framework callers use one operation-specific adapter, not a general client.

The sixteen exact owner rulings and their evidence are recorded in the executable baton. A correction to implementation
mechanics may not redesign one of those rulings. A deviation stops and returns to the owner.

## Delivery shape

The program has two merges:

1. **Foundation record:** archive the recovery-era investigation, adopt ADR-091, and make the approved OpenSpec deltas
   and
   task order durable. This merge changes no runtime.
2. **Coordinated cutover:** one draft implementation PR carries every runtime slice. Commits are independently reviewed,
   but no half-migrated subject, schema, caller, binary, or ownership deletion lands on `main`.

The issue queue, `semantic-tier-split`, `discovery-under-stream-shapes`, and the earlier pre-v1 hardening program remain
frozen unless the foundation program explicitly releases them.

## Runtime slices

1. Add behavior-level failing tests for the four operations, typed ports, exact reads, cross-lane races, hierarchy
   placement, unresolved references, and lost replies.
2. Make the typed component-port contract real and eliminate hidden mutation subjects.
3. Add exact authority reads through GraphQL and one embedded adapter.
4. Implement strict create, revision-fenced reconcile, partial append, conditional delete, and honest transport
   outcomes.
5. Migrate projection, rules, lifecycle, gated-DAG, tools, inference/research writers, configs, both binaries, and E2E
   harnesses.
6. Retain hierarchy only on Graphable ingest under the same Create/CAS discipline; delete automatic target stubs and
   claim-driven foreign-edge behavior.
7. Delete `pkg/ownership`, `OwnershipService`, buckets, config, schemas, metrics, wiring, and obsolete tests while
   preserving the independent graph-state guard and catalog cleanliness check.
8. Regenerate artifacts, prove conformance to every ruling, run the final gates, and merge one breaking cutover.

OpenSpec `tasks.md` is authoritative for completion state and evidence.

## Complexity budget

The target has:

- four mutation subjects, down from eight;
- one mutation protocol interface and family;
- one exact entity result and one narrow embedded adapter;
- zero new buckets, streams, durable consumers, services, status keys, config knobs, coordination primitives,
  compatibility paths, or MCP surfaces;
- deletion of the 4,599-line ownership package and the 866-line OwnershipService cohort;
- net-negative production code after generated artifacts are excluded.

A net-positive result or a new primitive returns to design review with line-by-line justification.

## Test and CI cadence

The program does not pay the full-suite cost after every commit.

- Inner loop: touched-package unit tests.
- Slice gate: affected packages with race detection and focused tagged integration tests.
- Full race/integration gates: after the mutation kernel stabilizes and once at final cutover.
- Final E2E: core, structural, semantic, lifecycle, and agentic, with active state polling and fast abort when wedged.
- Statistical E2E is not a blocker unless this change directly alters its contract; its known population nondeterminism
  cannot adjudicate this foundation.
- Schema generation, contract tests, strict OpenSpec validation, and a clean generated diff are final requirements.

Every nontrivial slice receives SemStreams developer implementation and SemStreams reviewer approval. Evidence claims
must
name reproducible commands/artifacts.

## Downstream holdout set

semdev, semmachina, semsource, semboids, semdragon, semstreams-ui, semteams, semconnect, semlink, and semops remain
hands-off. After the new wire stabilizes, the program performs a grep-level census and publishes an old-to-new migration
notice. Findings do not alter the accepted foundation, block the cutover, or authorize downstream edits.

## Stop conditions

Stop implementation and return for a ruling if a slice proposes:

- a mechanism or surface outside the approved inventory;
- recovery, CQRS, leadership, ownership, pending work, exactly-once, or compatibility machinery;
- an exported surface with no present consumer;
- a second interpreter of a fact an existing primitive already owns;
- a half-migrated binary or caller;
- a net-positive production diff without explicit review;
- a claim that missing/broken references require global refusal;
- a test workaround that hides nondeterminism or spends full-suite wall clock without increasing evidence.

Unrelated defects are recorded and deferred unless they prevent proving an approved foundation requirement.

## Program closure

Retired 2026-08-24. This file stays as the durable statement of identity, priorities, and the binding foundation that
ADR-090, ADR-091, and the docs linking here cite; nothing above is executable.

- Executable change archived as `openspec/changes/archive/2026-08-07-establish-graph-read-write-foundation/`; cutover
  commit `dbdc9bd8` (`refactor(graph)!: cut over to typed mutations and exact reads`) deleted `pkg/ownership`
  (`git log -1 -- pkg/ownership`).
- Follow-on changes archived: `2026-08-08-foundation-b-port-language`,
  `2026-08-09-post-foundation-b-declaration-generation`, `2026-08-11-post-foundation-b-graph-query-contract-closure`.
  Their `tasks.md` hold what actually landed. The post-GS-01 reality audit
  ([Part I accepted, Part II design](post-gs01-graph-read-derived-foundation-design.md)) re-baselined the work that
  followed and directed this file to historical status.
- What this program froze is resolved: `discovery-under-stream-shapes` archived 2026-08-07, `semantic-tier-split`
  archived 2026-08-21, and the [pre-v1 hardening record](prev1-program.md) retired 2026-08-24. No successor baton file
  exists: session state lives in the operator's session-memory handoff blocks; shared truth is the issues and
  `task openspec:queue`.
- The downstream old-to-new migration notice promised under "Downstream holdout set" is tracked as gh#753
  (`status:needs-decision`); the holdout set stays hands-off.
