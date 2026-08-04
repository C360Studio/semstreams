## Why

SemStreams' defining feature is predictable semantic graph state, but its current
authority, mutation, materialized-view, readiness, and query contracts evolved
independently. The result is not a missing event-sourced CQRS framework. It is a
current-state authority surrounded by owners and callers that use different
vocabulary for revision, completion, failure, readiness, and recovery.

ADR-090 chose authoritative current semantic state with role-specific
materialized views. #894 and #895 applied its deletion test to two unconsumed
durable views. Before another package issue changes graph behavior, GS-00 must
bind the small shared contract that surviving paths implement incrementally.

## What Changes

- Establish one canonical living program record for ADR-090 implementation.
- Define the target typed authoritative-read contract: current value plus
  per-entity revision and classified failure.
- Define two-axis mutation results: honest commit knowledge plus optional current
  authority observation, without upgrading unknown or conflating view visibility.
- Record a lifecycle-declaration acceptance candidate for later GS-03 design.
- Preserve owner-specific trigger, ordering, currency, repair, and fallback
  semantics rather than forcing them into one runtime.
- Establish the three-owner evidence gate that must pass before a shared durable
  view runtime can be proposed.
- Bind offline-first and tier behavior: higher-tier absence degrades explicitly
  and cannot weaken lower-tier graph truth.
- Correct canonical query and KV guidance that promises unimplemented MCP graph
  access, universal eventual consistency, or retained `ENTITY_STATES` history.
- Freeze ten downstream projects until one SemStreams foundation tag candidate,
  followed by coordinated migration and a new evidence review.
- Define the objective Foundation tag-candidate gate whose recorded `PASS` is
  required before releasing the candidate or migrating a holdout.
- Sequence a bounded deterministic E2E harness increment before read-front and
  concept consolidation; reserve the three-run proof for the final gate.
- Establish a measurable complexity ratchet and WIP limit of one GS increment.

## Nature of this change

GS-00 is governance and design only. It changes documentation and decision skills
and records non-normative acceptance candidates in `design.md`. It has no runtime
spec delta and adds no runtime type, bucket, subject, configuration, query
handler, or compatibility path. Per-ruling review evidence is recorded in the
[GS-00 conformance map](../../../../docs/proposals/graph-state-read-write-ruling-conformance.md).

GS-00 archives after architecture acceptance. Bounded GS-01+ changes each create
the appropriate capability delta, implement and validate it, promote it to
current truth, and archive before the next increment starts.

## Non-goals

- No event sourcing or general CQRS/read-model runtime.
- No runtime implementation of typed reads, mutation outcomes, or lifecycle
  declarations in GS-00.
- No package-level issue fix selected from queue order.
- No downstream holdout migration or renewed sister-repository census.
- No wholesale rewrite of the 31 concept documents.
- No MCP graph endpoint, tool, or availability promise.
- No compatibility layer to preserve a graph anti-pattern.
- No durable mutation receipt, query-by-request-ID API, idempotency primitive, or
  resolution of #869. That requires separate atomicity, retention, and recovery
  design after the foundation contract.

## Impact

- **Affected current records:** ADR-090, the frozen inventory and decision record,
  and the pre-v1 baton's scope statement.
- **Affected canonical guidance:** query-pattern, kv-or-stream, and their directly
  linked query/KV concepts where they contradict ADR-090.
- **Future capability homes:** graph state, graph ingest, mutation client,
  lifecycle, graph query, view readiness, and owner capabilities. No general
  reactive subscription is pre-authorized.
- **Runtime impact:** none in GS-00.
- **Downstream impact:** none until the Foundation tag-candidate gate records
  `PASS` and the coordinated migration window begins.
