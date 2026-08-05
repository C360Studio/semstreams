# ADR-091: Graph Mutation Authority Without Semantic Ownership

## Status

**Accepted** — 2026-08-05. The owner accepted the independently reviewed GS-01 revision-39 design and authorized a
coordinated breaking pre-v1 implementation without compatibility layers. Mechanics live in the
`establish-graph-read-write-foundation` OpenSpec change.

This decision supersedes ADR-056 in full. It supersedes ADR-055's mutation-lane taxonomy while retaining its
fact-versus-request distinction, envelope-bearing entity birth, must-exist non-create mutations, and graph-ingest's
physical write responsibility. It supersedes the ownership-specific boot/service/shutdown parts of ADR-058 and removes
`owner_lease_stale` from ADR-060's otherwise-retained classified RPC error contract. ADR-090 remains controlling for
current authority, materialized views, eventual consistency, and the no-recovery/no-CQRS boundary.

## Context

SemStreams' defining feature is predictable graph state. `ENTITY_STATES` is canonical current authority and graph-ingest
is its physical writer. Over time, a second concern grew around that storage topology: semantic ownership attempted to
decide which producer may mutate each predicate through claims, presence, heartbeats, incarnation tokens, foreign-edge
modes, overlap checks, quiescence, boot services, configuration, and request fields.

That model asks producers and the framework to predict permission before acting. It does not close the actual
lost-update
path: Graphable ingest and mutation RPCs can race, and unconditional existing-key writes can erase an acknowledged
change. Enforcement is uneven, fails open in several cases, is enabled in only a small shipped configuration cohort,
and imposes substantial framework and adopter cost.

The accepted inventory measured 4,599 lines in `pkg/ownership` and 866 more in `OwnershipService`. It also found eight
mutation subjects, graph-ingest handlers operating outside declared component ports, exact authority reads that omit KV
revision, automatic relationship-target stubs, and write lanes without one storage discipline.

SemStreams is an offline-first, edge-capable, tiered semantic graph framework. Eventual consistency and temporarily
unresolved references are valid. The framework needs honest observable outcomes and lost-update protection, not a
financial-system authorization model or exactly-once fiction.

## Decision

### 1. Separate physical write responsibility from semantic authorship

Graph-ingest remains the sole physical writer to `ENTITY_STATES`. Any component may request an admitted mutation.
SemStreams owns mutation safety and shape; applications own whether a mutation is appropriate for their domain.

Catalog or derived-store “owner” language continues to identify storage, lifecycle, convergence, or retraction
responsibility. It grants no predicate authority. Global semantic owner claims, leases, presence, heartbeats, tokens,
foreign-edge modes, overlap enforcement, and their runtime service are deleted.

### 2. Component ports are the mutation API contract

Core NATS request/reply remains the command primitive. Graph-ingest declares one typed provider input port and
requesters
declare typed output ports for interface `semstreams.graph.mutation` v1 under family `graph.mutation.>`. Four typed
operations replace the eight-subject surface: strict create, revision-fenced reconcile, partial exact-tuple append, and
revision-fenced delete.

The component flow validates exactly one declared provider and any number of requesters. That is a static
API-composition rule, not leader election or account-wide process fencing. One graph-ingest process is the supported
deployment topology.
No JetStream mutation stream, command ledger, outbox, or exactly-once mechanism is added.

### 3. Atomic Create and observed-revision CAS are the authority discipline

A genuine entity birth uses atomic KV `Create`. Every existing-key write on every lane—Graphable ingest, RPC mutation,
or hierarchy inverse—uses CAS against state read at a specific revision. Local keyed ingest dispatch may improve
throughput but is not a correctness or coordination primitive.

Reconcile and delete require caller-supplied nonzero revisions from an exact read and return typed revision mismatch
rather than silently overwriting newer state. Retry-safe append and Graphable merge may re-evaluate against current
state.
A lost reply after possible delivery is `commit_unknown`; matching later content does not prove which request wrote it.

### 4. Exact reads expose storage evidence through admitted surfaces

The exact entity result carries a validated entity and the nonzero KV revision from the same entry. Remote applications
consume the result through GraphQL. Embedded framework services use one operation-specific typed adapter. There is no
general embedded graph client, MCP read contract, or raw-KV application fallback.

### 5. Missing relationship objects are valid eventual graph state

A valid relationship may name an object that is not yet present. The source edge remains current authority; exact
dereference, hydration, or traversal reports the unresolved object. Graph-ingest creates no referential stub, pending
record, delayed drain, or repair workflow. A later real birth makes future reads resolve without replaying the source.

Opt-in hierarchy remains a Graphable-ingest semantic projection. Its containers are real inferred entities, not
referential stubs; their births use atomic Create and inverse writes use CAS. RPC create has no hierarchy side effects.

### 6. The cutover is clean and complexity-decreasing

The pre-v1 cutover removes old subjects, request fields, readers, schemas, buckets, services, and configuration in one
coordinated merge. No alias, dual format, online migration, or compatibility period is shipped. Production code must be
net-negative after generated artifacts are excluded, and the change adds no bucket, stream, service, status key,
coordination primitive, compatibility path, or MCP surface.

## Consequences

- Components reason about four operations, typed outcomes, and their local projection shape instead of owner IDs,
  claims, leases, tokens, heartbeats, and foreign-edge modes.
- Fighting writers become an observable runtime conflict rather than a boot-time predicted prohibition. A bounded
  per-operation revision-mismatch signal identifies the condition; no detector service is introduced.
- Rule reconcile performs one exact read and one mutation request. It surfaces `revision_mismatch` and
  `commit_unknown` without automatic retry; the component owns any later operation-specific retry decision.
- Broken or missing references do not stop the graph. Readers report them precisely and continue serving valid state.
- Operators run one graph-ingest process and retain existing `GRAPH_STATUS` readiness/poison semantics. NATS clustering
  remains supported; operators own deployment backup/checkpoint procedures.
- Ten sister repositories receive a communicate-only wire census and migration notice. They do not constrain the target
  and are not edited by this change.

## Alternatives rejected

- **Keep or harden semantic ownership.** It preserves a prediction substrate whose complexity is disproportionate to
  its enforcement and still does not close cross-lane lost updates.
- **Route every write through one local keyed queue.** A process-local queue does not protect against other processes
  and
  would obscure the actual KV revision boundary. CAS already supplies the needed observable race break.
- **Add a mutation stream or event-sourced ledger.** Mutations need immediate classified outcomes and caller-controlled
  retry. `ENTITY_STATES` is current authority, not an event-sourced log.
- **Create stubs or queue unresolved relationships.** Object absence is ordinary eventual graph state; manufacturing
  entities or durable pending work creates more lifecycle and ownership questions than it solves.
- **Move hierarchy to a new derived-view subsystem now.** Retaining the existing opt-in semantic behavior under the same
  Create/CAS rule closes the blocker without creating another framework abstraction.
- **Add leader election or recovery tooling.** Neither is required for the supported topology or mutation correctness,
  and both would revive previously rejected complexity.
