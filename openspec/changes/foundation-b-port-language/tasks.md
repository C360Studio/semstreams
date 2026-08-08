# Tasks — Foundation B port language

> Checkpoints 1-4, the append-only trajectory cutover, response-bound work, and clean ObjectStore API retirement are
> implemented in the Foundation B working tree. Release evidence below records the completed-tree results; independent
> review, post-merge inventory, merged-baseline recording, and archive remain open.

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

- [x] 5.0b Implement deterministic bounded `TrajectoryFactV1` encoding, per-invocation attempt identity and ordering,
  immutable KV Create/Get verification, distinct redelivery observations, and the internal 8 KiB fact limit.
- [x] 5.0c Capture full canonical model/tool/compaction/terminal evidence before operational truncation; store and
  verify digest-addressed bodies through the configured lazy `StoreRegistry` lookup; remove agentic-loop's private
  ObjectStore handle and lifecycle.
- [x] 5.0d Add bounded audit-failure logging/metrics and sticky Health degradation for provider, evidence, encode, and
  fact failures; prove every failure path preserves downstream state transition, publication, and ACK.
- [x] 5.0e Start/register all StoreProvider components in a provider barrier before the parallel consumer barrier;
  propagate invalid/duplicate registration as startup failure without clobbering the incumbent; start agentic-loop
  degraded when its configured provider is absent.
- [x] 5.0f Add the canonical agentic-loop `trajectories` and `trajectory_query` ports, route through graph-gateway's
  existing typed `agentic_queries` family, add `objectstore`/`AGENT_CONTENT` to all seven assemblies, and delete their
  redundant trajectory overrides.
- [x] 5.0g Replace aggregate/cache/manager reads with prefix-listed causal facts; expose GraphQL-only trajectory reads
  as strict cursor-paged metadata/references with `coverage: observed`, page-local `observed_totals`, and ordinary
  ordered terminal observations; delete evidence hydration, direct HTTP/OpenAPI, and terminal trajectory graph writes.
- [x] 5.0h Add the frozen unit, integration, static, crash-boundary, restart, provider-lifecycle, routing, seven-config,
  GraphQL, and absence-of-seal/cache/graph/projector tests specified by the accepted contract.
- [x] 5.0i Add narrow connected-server `MaxPayload` observation and make the shared request responder translate only
  an observed success-publish `nats.ErrMaxPayload` to canonical `invalid/response_too_large`.
- [x] 5.0j Replace the static graph-prefix budget and list-only GraphQL projection with exactly fitted typed pages and
  end-to-end `next_cursor`; reject an indivisible first entity that cannot fit.
- [x] 5.0k Delete the ObjectStore request/reply API, default `api` input, DTOs/handlers/docs/tests, and dormant NATS
  content fetcher; reject old `api`/`nats-request` inputs at construction and retain registered Store access only.

- [x] 5.1 Run `task lint`, `task build`, `go vet -tags=integration ./...`, and
  `go vet -tags=live_llm ./...` against the completed accepted-contract implementation; record actual results.
- [x] 5.2 Run `go test -race ./...` against the completed implementation; record the actual result.
- [x] 5.3 Run `task test:integration` against the completed implementation; record the actual result.
- [x] 5.4 Run `task schema:generate`, verify regeneration introduces no additional `schemas/` or `specs/` diff, and
  record the actual result.
- [x] 5.5 Run `go test ./test/contract/...` and `task openspec:validate`; record the actual results.
- [x] 5.6 Run the breaking E2E gates `task e2e:agentic`, `task e2e:semantic`, `task e2e:all`, and
  `task e2e:research-graph`; record each result independently. Historical evidence: the pre-contract agentic E2E at
  `d630c8fd` failed fast because a redundant complete-replacement `trajectories` override did not match runtime
  defaults. The completed-tree executions below supersede that historical failure.

Completed-tree evidence on 2026-08-07 is retained in
`docs/proposals/foundation-b-release-evidence.md` and summarized here:

- `task lint`, `task build`, `go vet -tags=integration ./...`, and `go vet -tags=live_llm ./...`: pass.
- `go test -race ./...`: pass.
- `task test:integration`: pass with the integration tag, race detector, and uncapped package parallelism.
- `task schema:generate`: pass; the two intentionally changed generated artifacts were byte-identical before and after
  regeneration (`agentic-loop.v1.json` SHA-256 `870a2833...d13bc`, `openapi.v3.yaml` SHA-256
  `11fd3e43...7879`). No additional generated drift appeared.
- `go test ./test/contract/...`: pass; `task openspec:validate`: 35 passed, 0 failed.
- `task e2e:all`: pass, including its core, structural, statistical, semantic, and agentic executions;
  `task e2e:research-graph`: pass. The semantic execution completed all 48 stages and the agentic execution observed
  ten strict trajectory facts plus terminal completion.
- [x] 5.7 Obtain an independent SemStreams reviewer pass on the complete implementation and OpenSpec diff. Final
  verdict: `REVIEW PASS — APPROVE`; no blocking or high findings remained.
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
