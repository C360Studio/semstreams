# Tasks — Foundation B port language

> Checkpoints 1-4 and the pre-trajectory release-fallout corrections are completed implementation history. The
> append-only trajectory contract accepted at `139b8b1c` is not implemented. Every release gate remains unchecked;
> results recorded at older commits are historical evidence only.

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
subscriptions by canonical port kind (`d630c8fd`). Accepted-contract HEAD `139b8b1c` is 16 commits after baseline
`5ffc1d1f`. The historical gates at `d630c8fd` predate the accepted trajectory contract.

- [ ] 5.0b Implement deterministic bounded `TrajectoryFactV1` encoding, per-invocation attempt identity and ordering,
  immutable KV Create/Get verification, distinct redelivery observations, and the internal 8 KiB fact limit.
- [ ] 5.0c Capture full canonical model/tool/compaction/terminal evidence before operational truncation; store and
  verify digest-addressed bodies through the configured lazy `StoreRegistry` lookup; remove agentic-loop's private
  ObjectStore handle and lifecycle.
- [ ] 5.0d Add bounded audit-failure logging/metrics and sticky Health degradation for provider, evidence, encode, and
  fact failures; prove every failure path preserves downstream state transition, publication, and ACK.
- [ ] 5.0e Start/register all StoreProvider components in a provider barrier before the parallel consumer barrier;
  propagate invalid/duplicate registration as startup failure without clobbering the incumbent; start agentic-loop
  degraded when its configured provider is absent.
- [ ] 5.0f Add the canonical agentic-loop `trajectories` and `trajectory_query` ports, route through graph-gateway's
  existing typed `agentic_queries` family, add `objectstore`/`AGENT_CONTENT` to all seven assemblies, and delete their
  redundant trajectory overrides.
- [ ] 5.0g Replace aggregate/cache/manager reads with prefix-listed causal facts; expose GraphQL-only trajectory reads
  with `coverage: observed`, `observed_totals`, evidence hydration, and ordinary ordered terminal observations; delete
  direct HTTP/OpenAPI and terminal trajectory graph writes.
- [ ] 5.0h Add the frozen unit, integration, static, crash-boundary, restart, provider-lifecycle, routing, seven-config,
  GraphQL, and absence-of-seal/cache/graph/projector tests specified by the accepted contract.

- [ ] 5.1 Run `task lint`, `task build`, `go vet -tags=integration ./...`, and
  `go vet -tags=live_llm ./...` against the completed accepted-contract implementation; record actual results.
- [ ] 5.2 Run `go test -race ./...` against the completed implementation; record the actual result.
- [ ] 5.3 Run `task test:integration` against the completed implementation; record the actual result.
- [ ] 5.4 Run `task schema:generate`, verify `git diff -- schemas/ specs/` is clean, and record the actual result.
- [ ] 5.5 Run `go test ./test/contract/...` and `task openspec:validate`; record the actual results.
- [ ] 5.6 Run the breaking E2E gates `task e2e:agentic`, `task e2e:semantic`, `task e2e:all`, and
  `task e2e:research-graph`; record each result independently. Historical evidence: the pre-contract agentic E2E at
  `d630c8fd` failed fast because a redundant complete-replacement `trajectories` override did not match runtime
  defaults. That cause now has an accepted disposition, but no E2E has run against its implementation.
- [ ] 5.7 Obtain an independent SemStreams reviewer pass on the complete implementation and OpenSpec diff.
- [ ] 5.8 Re-inventory the merged Foundation B tree and hard-stop on any alias, flat discriminator, top-level side lane,
  dead type, independent shared projection, false KV declaration, JetStream input without explicit stream identity,
  consumer-local stream-name derivation fallback, undeclared runtime-policy dependency, trajectory aggregate/cache,
  private ObjectStore handle, direct trajectory HTTP/OpenAPI, trajectory graph write, or completeness machinery.
- [ ] 5.9 Record the actual merged baseline and implementation evidence; do not begin Foundation C before a new accepted
  inventory and owner remap.
- [ ] 5.10 Archive `foundation-b-port-language` only after tasks 5.1-5.9 are truthful.

The hierarchy write-path-versus-derived-index placement question, including its performance and complexity trade-offs,
is a post-Foundation graph index-program input. The research create-before-append and hierarchy consequences are inputs
to that same program, not Foundation B changes. `task e2e:research-graph` remains only the existing Foundation B cutover
validation gate; it does not authorize hierarchy or research redesign in this change.
