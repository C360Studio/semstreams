## Context

This artifact is the durable OpenSpec handoff for the approved Foundation B implementation. Its controlling inputs are
the files as they exist in the working tree on 2026-08-07:

- `docs/proposals/foundation-b-port-language-design.md`: 112 lines, 8,895 bytes, SHA-256
  `9ef118a5e2837cb0adfdcca3c9962fa4e23dd4dac99d1562de45225d4940c48d`;
- `docs/proposals/foundation-b-port-language-control.md`: 142 lines, 9,795 bytes, SHA-256
  `f6c1d0c9d2ca1bca5661d424a96dcd9f285b02abbbbd6b1db9080679e1d3c39e`;
- accepted inventory `docs/proposals/foundation-b-port-language-inventory.md`: 955 lines, 53,247 bytes, SHA-256
  `d957dfd00a2ca9bbf3ee3cf4aa2d0d9005008eb78198c7762403aa2c66ba9000`.

The immutable worklist and disposition ledgers remain historical migration authority. The amended target accounts for
512 surviving frozen configuration rows, ten approved deletions, sixteen new graph-gateway output rows, 528 actual
canonical configuration rows, and 136 production Go declaration identities. See proposal.md for motivation.

## Goals / Non-Goals

**Goals:**

- Make one binding table own decoding, validation, normalization, identity, interface, interaction, subjects, and
  stream facts for the twelve canonical kinds.
- Give all shared consumers one immutable normalized facts projection without exporting a second grammar.
- Record the declaration corrections and strict graph-gateway and graph-mutation composition contracts approved during
  checkpoints 1-4.
- Keep the breaking release closed until every checkpoint-5 gate is recorded with actual evidence.

**Non-Goals:**

- Foundation C declaration authorship/snapshot lifecycle, graph query behavior, GraphQL or MCP behavior, indexes,
  downstream migration, custom kinds, aliases, or dual decoders.

## Decisions

### One strict declaration and runtime grammar

The only exported kinds are `timer`, `network`, `file`, `http-client`, `nats`, `nats-request`, `jetstream`, `kv-watch`,
`kv-read`, `kv-write`, `store-read`, and `store-provide`. A definition retains common metadata plus typed `Config`; a
runtime port uses that same envelope. Only network host `0.0.0.0` and request timeout `1s` default. Complete replacement
is preferred over field merging because callers must not predict precedence between flat and typed values.

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

### Canonical request ports define mutation topology

Graph-ingest declares exactly one required input with canonical kind `nats-request`, interface
`semstreams.graph.mutation` version `v1`, and family `graph.mutation.>`. Compatible components declare outputs. Static
composition validates exactly one provider from normalized facts; it does not predict account-wide process cardinality
or introduce a stream, election, or lease.

### Checkpoints 1-4 are implementation history; checkpoint 5 controls release

Commits `b7de684a`, `19ce5f7c`, `bb43c5e6`, `6877a461`, and `26417f25` implement the grammar/codec, owned migration,
shared consumers, renderer/runtime sweep, and approved graph-gateway amendment. They are not independently releasable.
Schema, contract, race/integration, E2E, independent review, and the mandatory post-B inventory remain open release
gates.

## Risks / Trade-offs

- **External configurations fail startup after the clean break** → publish the exact envelope and graph-gateway
  migration below; never silently accept an old field or port name.
- **A shared consumer can recreate grammar drift** → structural guards require normalized facts and retain only the
  two named temporary raw-config owners.
- **A specialized provisioner can grow into a parallel authority** → limit the exception to gated-DAG's four
  unrepresentable physical policies and keep all discoverable stream facts canonical.
- **Green focused guards can hide a cross-stack break** → checkpoint 5 includes all required race, integration,
  contract, and breaking E2E gates before release.

## Migration Plan

1. Component authors replace flat port fields, aliases, top-level KV side lanes, and the runtime `type`/`data` envelope
   with the canonical typed `config.kind` envelope. Old Go declarations fail compilation; old JSON fails typed boot
   validation.
2. Graph-gateway configurations remove every input and replace `queries` with the three required outputs and matching
   subject families. There is no auto-fill or compatibility alias.
3. Run every checkpoint-5 gate in tasks.md and obtain independent SemStreams reviewer approval.
4. Re-inventory the merged tree. Stop if an alias, flat discriminator, top-level side lane, dead type, independent
   shared projection, false KV declaration, or undeclared runtime-policy dependency remains.
5. Archive this change only after the release and post-B inventory gates are truthful. Rollback is whole-cutover
   rollback; there is no dual-wire runtime mode.
