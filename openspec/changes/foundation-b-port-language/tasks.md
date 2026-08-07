# Tasks — Foundation B port language

> Checkpoints 1-4 and the recorded release-fallout corrections are completed implementation history on the breaking
> branch. Checkpoint 5 remains the release gate; local gates 5.1-5.5 are green, while breaking E2E, independent review,
> and inventory remain open.

## 1. Grammar and codec — completed

- [x] 1.1 Add failing table tests for every canonical kind/direction, strict unknown-field/kind refusal, required data,
  malformed duration/port values, duplicate names, and JSON round trips.
- [x] 1.2 Implement the twelve `PortKind` values, typed configs, one strict common codec, the resolver/facts projection,
  complete-replacement merge, and field-complete JetStream/store round trips (`b7de684a`).

## 2. Owned declaration migration — completed

- [x] 2.1 Migrate the frozen Go and JSON population, delete aliases and top-level KV side lanes, and account for every
  frozen ledger identity (`19ce5f7c`).
- [x] 2.2 Delete the two false graph-query reads; add the five truthful graph-clustering/agentic-tools exact reads; make
  AGENT_LOOPS acquisition lazy and non-provisioning.

## 3. Shared normalized consumers — completed

- [x] 3.1 Move flowgraph, Registry capabilities/conflicts, ComponentManager reporting, schema generation, and ordinary
  stream provisioning to normalized facts (`bb43c5e6`).
- [x] 3.2 Retain and structurally bound only the two accepted temporary raw-config owner families.

## 4. Renderer/runtime sweep — completed

- [x] 4.1 Migrate shipped renderers and prove no flat `Type` reads, old aliases/fields, dead types, projection type
  switches, top-level `kv_read`/`kv_write`, or direct runtime-port grammar remain (`6877a461`).
- [x] 4.2 Add truthful HTTP, file, store, gated-DAG, read-pattern, and runtime-completeness declarations/guards.
- [x] 4.3 Apply the owner-approved graph-gateway clean break: no inputs and exactly three required canonical
  query-family outputs across the eight shipped configurations (`26417f25`).

## 5. Release gate — in progress

- [x] 5.0 Correct JetStream input identity at the closed binding: require subjects in both directions and explicit
  `stream_name` on inputs, preserve generic provisioner-owned subject-only output derivation, migrate the 61 shipped
  inputs across 14 configurations, resolve declarations by lane during atomic `PortConfig` decoding, delete
  consumer-local fallbacks and the dead top-level agentic-model stream-name surface, and add structural shipped-config
  guards.
- [x] 5.0a Correct UDP, file, HTTP, and WebSocket input factory default/override sequencing, preserve UDP complete
  named-port replacement, and add focused factory-level regression tests.

Release fallout through branch HEAD `d630c8fd` also migrated canonical engine fixtures (`7ba82c8f`), corrected merged
input defaults (`02cd51e1`, recorded as 5.0a), enforced agentic-tools contracts (`ffe0f705`), enforced agentic-model and
agentic-governance roles (`8178a10c`), migrated agentic-loop integration fixture names (`69a723f5`), and routed rule
subscriptions by canonical port kind (`d630c8fd`). This branch is 13 commits after baseline `5ffc1d1f`.

- [x] 5.1 At `d630c8fd`, `task lint`, `task build`, `go vet -tags=integration ./...`, and
  `go vet -tags=live_llm ./...` exited successfully.
- [x] 5.2 At `d630c8fd`, `go test -race ./...` exited successfully.
- [x] 5.3 At `d630c8fd`, `task test:integration` exited successfully.
- [x] 5.4 At `d630c8fd`, `task schema:generate` exited successfully and `git diff -- schemas/ specs/` was clean.
- [x] 5.5 At `d630c8fd`, `go test ./test/contract/...` and `task openspec:validate` exited successfully.
- [ ] 5.6 Run the breaking E2E gates `task e2e:agentic`, `task e2e:semantic`, `task e2e:all`, and
  `task e2e:research-graph`; record each result independently. Current evidence: `task e2e:agentic` failed fast during
  startup. Summarized cause: agentic-loop rejected the configured `trajectories` output as an unknown override. The
  semantic, all-tier, and research-graph gates have not run. The durable trajectory disposition is an unresolved owner
  ruling: adjudicate the bounded choice between a declared KV materialized view and graph-native durable
  reconstruction before deleting any trajectory configuration/documentation or restoring a runtime port.
- [ ] 5.7 Obtain an independent SemStreams reviewer pass on the complete implementation and OpenSpec diff.
- [ ] 5.8 Re-inventory the merged Foundation B tree and hard-stop on any alias, flat discriminator, top-level side lane,
  dead type, independent shared projection, false KV declaration, JetStream input without explicit stream identity,
  consumer-local stream-name derivation fallback, or undeclared runtime-policy dependency.
- [ ] 5.9 Record the actual merged baseline and implementation evidence; do not begin Foundation C before a new accepted
  inventory and owner remap.
- [ ] 5.10 Archive `foundation-b-port-language` only after tasks 5.1-5.9 are truthful.

The hierarchy write-path-versus-derived-index placement question, including its performance and complexity trade-offs,
is a post-Foundation graph index-program input. The research create-before-append and hierarchy consequences are inputs
to that same program, not Foundation B changes. `task e2e:research-graph` remains only the existing Foundation B cutover
validation gate; it does not authorize hierarchy or research redesign in this change.
