# Design — Foundation B port language

## Context

This artifact is the durable OpenSpec handoff for the approved Foundation B implementation. Its controlling inputs are
the files as they exist in the working tree on 2026-08-07:

- `docs/proposals/foundation-b-port-language-design.md`: 112 lines, 8,895 bytes, SHA-256
  `9ef118a5e2837cb0adfdcca3c9962fa4e23dd4dac99d1562de45225d4940c48d`;
- `docs/proposals/foundation-b-port-language-control.md`: 142 lines, 9,795 bytes, SHA-256
  `f6c1d0c9d2ca1bca5661d424a96dcd9f285b02abbbbd6b1db9080679e1d3c39e`;
- accepted inventory `docs/proposals/foundation-b-port-language-inventory.md`: 955 lines, 53,247 bytes, SHA-256
  `d957dfd00a2ca9bbf3ee3cf4aa2d0d9005008eb78198c7762403aa2c66ba9000`.
- accepted trajectory inventory `docs/proposals/agentic-trajectory-contract-inventory.md`: commit `8c6997a6`,
  426 lines, 34,359 bytes, SHA-256
  `5a7dcf3591cc643ee93654515763ec69982f36c78c296cf02bb8234b3000dd2a`;
- accepted trajectory contract `docs/proposals/agentic-trajectory-target-contract.md`: commit `139b8b1c`, 499 lines,
  28,672 bytes, SHA-256 `53b169fbdf2cd25dfb9d3e4c87d1fb7135713ec5053d1ed1e6d93409b57b537e`.

The immutable worklist and disposition ledgers remain historical migration authority. The amended target accounts for
512 surviving frozen configuration rows, ten approved deletions, sixteen new graph-gateway output rows, 528 actual
canonical configuration rows, and 136 production Go declaration identities. See proposal.md for motivation.

The implementation baseline remains `main` at `5ffc1d1f`; accepted-contract HEAD `139b8b1c` is 16 commits ahead.
Local lint, build, tagged vet, race, integration, schema-cleanliness, contract, and OpenSpec results recorded at
`d630c8fd` predate the trajectory contract and are historical only. Runtime implementation, all current release
validation, breaking E2E, independent review, and the post-B inventory remain open.

## Goals / Non-Goals

**Goals:**

- Make one binding table own decoding, validation, normalization, identity, interface, interaction, subjects, and
  stream facts for the twelve canonical kinds.
- Give all shared consumers one immutable normalized facts projection without exporting a second grammar.
- Record the declaration corrections and strict graph-gateway and graph-mutation composition contracts approved during
  checkpoints 1-4.
- Replace process-local aggregate trajectory authority with immutable KV observations and full evidence through the
  registered Store lifecycle.
- Make audit loss loud in existing observability while preserving the agent work outcome.
- Make GraphQL the public trajectory read surface, with typed internal NATS routing and observed-only coverage.
- Keep the breaking release closed until every checkpoint-5 gate is recorded with actual evidence.

**Non-Goals:**

- Foundation C declaration authorship/snapshot lifecycle, unrelated graph-query/GraphQL/MCP behavior, indexes,
  downstream migration, custom kinds, aliases, or dual decoders.
- Hierarchy placement, research create-before-append semantics, or any redesign inferred from the research-graph E2E.
- Trajectory completeness proof, terminal seal, aggregate summary, cache authority, graph projection, repair service,
  automatic expiry, or redesign of `COMPLETE_` and terminal-event contracts.

## Decisions

### One strict declaration and runtime grammar

The only exported kinds are `timer`, `network`, `file`, `http-client`, `nats`, `nats-request`, `jetstream`, `kv-watch`,
`kv-read`, `kv-write`, `store-read`, and `store-provide`. A definition retains common metadata plus typed `Config`; a
runtime port uses that same envelope. Only network host `0.0.0.0` and request timeout `1s` default. Complete replacement
is preferred over field merging because callers must not predict precedence between flat and typed values.

Every JetStream declaration carries at least one non-empty subject. JetStream inputs additionally carry an explicit,
non-empty backing `stream_name`; consumer components never infer it from a subject or a component-local default.
JetStream outputs may omit `stream_name` because the one canonical generic provisioner owns output stream-name
derivation. That narrow provisioning behavior does not make a subject-only declaration a valid input.

`PortConfig` decoding resolves every definition using its enclosing lane direction before publishing the decoded
value. It builds both normalized lanes first and assigns only after every definition succeeds, so wrong-direction or
missing-field failures cannot leave a partially decoded receiver. The retired top-level agentic-model `stream_name`
field is deleted; agentic-model stream identity lives only on its canonical JetStream ports.

The rejected alternatives are aliases, default-to-NATS behavior, `Config any`, a second runtime wire, custom-kind
registration, or a migration decoder. Each would preserve more than one interpretation at the adopter seam.

### One normalized fact for every shared observer

The resolver validates kind and direction once and emits immutable facts. Registry capabilities/conflicts, flowgraph,
ComponentManager reporting, schema generation, and ordinary stream provisioning consume those facts and do not switch
on concrete configurations. Message-logger and stream planning remain the two explicitly bounded raw-config owner
families until Foundation C supplies an effective snapshot; neither may define another grammar.

`kv-read`, `kv-watch`, and `kv-write` share `kv:<bucket>` connection identity but retain distinct interaction patterns.
Exact/list reads use `read`; only watches use `watch`. `store-read` remains backend-neutral federation.

### Truthful declarations replace predictions

Graph-query gains no false exact-read declaration. Graph-clustering and agentic-tools declare the five real exact/list
KV reads; an exact reader never provisions its bucket. The dead `KVWrite` side lane is removed, and shipped writes use
ordinary outputs.

Graph-gateway owns no shared-mux composition input. It accepts exactly three required canonical `nats-request` outputs
named `graph_queries`, `graph_index_queries`, and `agentic_queries`, with families `graph.query.*`,
`graph.index.query.*`, and `agentic.query.*`. Configured family overrides remain runtime routing authority after
validation. `bind_address` remains standalone server configuration, not a port claim.

### Provision from normalized stream facts, with one bounded exception

Ordinary stream planning derives only from normalized JetStream facts and never reinterprets flat fields. Gated-DAG is
the sole specialized physical provisioner for its dispatch stream because the generic GiB/day declaration cannot
express its byte-exact limit, discard-new, max-age, and deduplication policy. That exception does not authorize another
consumer to infer or provision those settings.

The generic provisioner is also the only owner allowed to derive an omitted output stream name from declared subjects.
Consumers always receive an explicit input backing name and do not reproduce that derivation.

### Canonical request ports define mutation topology

Graph-ingest declares exactly one required input with canonical kind `nats-request`, interface
`semstreams.graph.mutation` version `v1`, and family `graph.mutation.>`. Compatible components declare outputs. Static
composition validates exactly one provider from normalized facts; it does not predict account-wide process cardinality
or introduce a stream, election, or lease.

### Release fallout remains implementation history

Commits `b7de684a`, `19ce5f7c`, `bb43c5e6`, `6877a461`, and `26417f25` implement the grammar/codec, owned migration,
shared consumers, renderer/runtime sweep, and approved graph-gateway amendment. They are not independently releasable.
Commit `fe4e5018` corrects JetStream input identity. Release fallout then migrated canonical engine fixtures
(`7ba82c8f`), corrected input factory default/override behavior (`02cd51e1`), enforced agentic-tools contracts
(`ffe0f705`), enforced agentic-model and agentic-governance roles (`8178a10c`), migrated agentic-loop integration
fixture names (`69a723f5`), and routed rule subscriptions by canonical port kind (`d630c8fd`).

Local schema, contract, lint/build/vet, race, and integration evidence is green at `d630c8fd`. E2E, independent review,
and the mandatory post-B inventory remain open release gates.

### Append-only attempt observations replace aggregate trajectory authority

`AGENT_TRAJECTORIES` is a history-1 KV bucket whose immutable keys are
`v1.<base32-sha256(loop_id)>.<attempt_id>`. Each fact-recording invocation allocates a bounded framework attempt ID and
a monotonically observed per-loop ordinal. Store and KV retries within that invocation reuse its exact identity, key,
and canonical bytes; redelivery is a new attempt and therefore appends another visible fact. Optional source
correlation links repeated attempts but never becomes identity. Reader order is
`(iteration, phase_rank, source_ordinal, attempt_ordinal, attempt_id)`.

The fact envelope is finite and internally bounded to 8 KiB. It carries enums, counters, digests, bounded previews,
and an optional `message.StorageReference`; it never embeds prompts, messages, tool arguments/results, URLs,
unbounded metadata, or raw error strings. Full canonical `TrajectoryEvidenceV1` is captured before execution
truncation, hashed, and stored at `trajectory-evidence/v1/sha256/<digest>` through the lazily resolved configured
`storage.Store`. The shipped logical instance is `objectstore`, backed by physical bucket `AGENT_CONTENT` in all seven
agentic assemblies. Agentic-loop borrows through `StoreRegistry`; it constructs, caches, closes, or owns no backend
handle.

Evidence loss still attempts an honest ordinary fact with `evidence_capture="missing"`, a digest/size when available,
a bounded reason, and no fabricated reference. Any evidence or fact failure emits an ERROR without body content,
increments `semstreams_agentic_loop_trajectory_audit_failures_total{stage,kind,reason}`, and latches existing Health
degraded. It never rejects, NAKs, cancels, or fails the agent work. If fact creation also fails, no durable gap marker
is manufactured; logs, metrics, and Health are the only operational evidence.

All reads prefix-list visible facts, validate and causally sort them, and report only `coverage: observed` plus
`observed_totals`. An ordinary `loop.terminal` fact means one terminal outcome was observed. Redelivery may append
another terminal fact; no fact is a seal or completeness proof, and no terminal state is inferred from `COMPLETE_`,
events, cache, process memory, or graph state. GraphQL is the sole public application surface. Typed internal NATS
request/reply uses graph-gateway's existing `agentic.query.*` family and agentic-loop's declared exact
`agentic.query.trajectory` input.

Agentic-loop's defaults declare required `kv-write` output `trajectories` for `AGENT_TRAJECTORIES` with interface
`agentic.trajectory.fact` v1 and required `nats-request` input `trajectory_query` for `agentic.query.trajectory` with
interface `agentic.query` v1. Graph-gateway retains exactly three outputs; `agentic_queries` remains the required
`agentic.query.*` family with interface `agentic.query` v1. The seven redundant `trajectories` overrides are deleted
because named overrides are complete replacements and would erase required/interface facts. Isolated deployments may
use explicit complete paired query overrides; there is no platform-derived owner, alias, dual subscription, or shim.

Aggregate/public `Trajectory`, terminal cache, `trajectory_detail`, private `content_bucket` construction,
timestamp-derived evidence keys, direct trajectory HTTP/OpenAPI, and terminal batch graph writes are deleted cleanly.
Graph indexing is deferred: any later graph trace is a separate projection consuming the durable fact log.

The `kv-or-stream` decision is KV: these are immutable observed facts whose readers rehydrate by prefix/watch, not
queued requests requiring acknowledgement. Agent work requests remain on their existing JetStream paths.

### Store providers start and register before subscribing consumers

`ComponentManager` adds one narrow cold-boot phase around the existing `component.StoreProvider` interface. Providers
start concurrently in a first barrier and each store registers immediately after provider Start. Invalid or duplicate
instances are provider startup errors that fail the barrier without clobbering the incumbent. Only then do all
non-provider components start concurrently in the existing consumer barrier.

Agentic-loop validates its configured logical provider after that phase and before installing subscriptions. Absence
does not fail Start: it logs, increments the bounded provider-resolve metric, latches Health degraded, installs
subscriptions, and continues work. Each evidence operation still resolves lazily so later provider addition or
reconfiguration is observed. No sleep, readiness deadline, polling loop, port-derived dependency graph, or general
topological scheduler is introduced.

### Hierarchy and research consequences remain deferred inputs

Whether hierarchy belongs on the graph write path or in a derived index, including the performance and complexity
trade-offs, belongs to the post-Foundation graph index program. Research create-before-append and hierarchy
consequences are inputs to that program. Foundation B retains `task e2e:research-graph` solely as an existing cutover
validation gate; a failure there does not widen this change into hierarchy or research redesign.

## Risks / Trade-offs

- **External configurations fail startup after the clean break** → publish the exact envelope and graph-gateway
  migration below; never silently accept an old field or port name.
- **A shared consumer can recreate grammar drift** → structural guards require normalized facts and retain only the
  two named temporary raw-config owners.
- **A specialized provisioner can grow into a parallel authority** → limit the exception to gated-DAG's four
  unrepresentable physical policies and keep all discoverable stream facts canonical.
- **Green focused guards can hide a cross-stack break** → checkpoint 5 includes all required race, integration,
  contract, and breaking E2E gates before release.
- **Best-effort audit can be mistaken for completeness** → every response says `coverage: observed`, totals are named
  `observed_totals`, and static tests prohibit seals, manifests, counters, and completeness classifications.
- **Provider startup can race subscribers** → use one narrow StoreProvider barrier before the existing parallel
  consumer barrier; do not invent sleeps or a general dependency scheduler.
- **Audit storage failure can become work failure** → failure assertions cover publish/ACK continuation for every
  Store/KV stage and latch existing observability instead of changing the work result.
- **A later graph index can leak back into the write path** → Foundation B deletes trajectory graph writes and treats
  any later graph trace as a separate post-foundation projection design.

## Migration Plan

1. Component authors replace flat port fields, aliases, top-level KV side lanes, and the runtime `type`/`data` envelope
   with the canonical typed `config.kind` envelope. Old Go declarations fail compilation; old JSON fails typed boot
   validation.
2. Graph-gateway configurations remove every input and replace `queries` with the three required outputs and matching
   subject families. There is no auto-fill or compatibility alias.
3. Implement the accepted append-only trajectory contract: immutable attempt facts, registered full-fidelity evidence,
   non-blocking degradation, provider-first startup, canonical NATS routing, GraphQL observed-only reads, seven
   assembly corrections, and clean deletion of aggregate/cache/graph/HTTP authority.
4. Re-inventory the merged tree. Stop if an alias, flat discriminator, top-level side lane, dead type, independent
   shared projection, false KV declaration, undeclared runtime-policy dependency, trajectory cache/aggregate, private
   ObjectStore handle, direct HTTP route, or completeness machinery remains.
5. Archive this change only after the release and post-B inventory gates are truthful. Rollback is whole-cutover
   rollback; there is no dual-wire runtime mode.
