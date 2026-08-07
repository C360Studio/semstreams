# Tasks — Foundation B port language

> Checkpoints 1-4 are completed implementation history on the breaking branch. Checkpoint 5 remains the release gate;
> no focused or target guard below substitutes for its unchecked validation, review, and inventory tasks.

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

## 5. Release gate — remaining

- [x] 5.0 Correct JetStream input identity at the closed binding: require subjects in both directions and explicit
  `stream_name` on inputs, preserve generic provisioner-owned subject-only output derivation, migrate the 61 shipped
  inputs across 14 configurations, resolve declarations by lane during atomic `PortConfig` decoding, delete
  consumer-local fallbacks and the dead top-level agentic-model stream-name surface, and add structural shipped-config
  guards.
- [x] 5.0a Correct UDP, file, HTTP, and WebSocket input factory default/override sequencing, preserve UDP complete
  named-port replacement, and add focused factory-level regression tests.
- [ ] 5.1 Run `task lint` and record its exit status.
- [ ] 5.2 Run `go test -race ./...` and record its exit status.
- [ ] 5.3 Run `task test:integration` and record its exit status.
- [ ] 5.4 Run `task schema:generate`, then prove `git diff -- schemas/ specs/` is clean or commit the generated truth.
- [ ] 5.5 Run `go test ./test/contract/...` and record its exit status.
- [ ] 5.6 Run the breaking E2E gates `task e2e:agentic`, `task e2e:semantic`, `task e2e:all`, and
  `task e2e:research-graph`; record each result independently.
- [ ] 5.7 Obtain an independent SemStreams reviewer pass on the complete implementation and OpenSpec diff.
- [ ] 5.8 Re-inventory the merged Foundation B tree and hard-stop on any alias, flat discriminator, top-level side lane,
  dead type, independent shared projection, false KV declaration, JetStream input without explicit stream identity,
  consumer-local stream-name derivation fallback, or undeclared runtime-policy dependency.
- [ ] 5.9 Record the actual merged baseline and implementation evidence; do not begin Foundation C before a new accepted
  inventory and owner remap.
- [ ] 5.10 Archive `foundation-b-port-language` only after tasks 5.1-5.9 are truthful.
