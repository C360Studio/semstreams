# Tasks — Post-Foundation-B declaration generation

This change is inactive. Every task remains unchecked until implementation is explicitly started.

Each slice receives independent SemStreams review before the next starts. Later slices MUST NOT reopen
owner-accepted rulings absent implementation evidence of an internal contradiction.

## A. Service composition, restart truth, and dead surfaces

- [ ] A.1 Characterize unchanged arbitration: newer file pushes file state; equal/older file selects KV;
  equal-version content edit does not apply.
- [ ] A.2 When KV is selected, replace only Services from current `services.*` keys; retain existing synchronization
  for every other top-level section.
- [ ] A.3 Add pure non-mutating outer `ServiceConfigs` resolver: map-key identity, canonical raw JSON, mandatory entry
  materialization, explicit-false preservation, optional outer defaults, no inner service interpretation.
- [ ] A.4 Construct from post-Start `SafeConfig` and retain immutable clone of exact resolved boot desired map.
- [ ] A.5 Make `CreateService` and error-returning `RegisterInstance` pre-seal APIs; reject duplicates and post-seal
  calls typed, with no void wrapper.
- [ ] A.6 Validate configured/mandatory composition, register fixed root services, seal sorted full identity set, then
  start/bind routes/expose OpenAPI.
- [ ] A.7 Remove service-manager `services.*` mutation subscription/watcher/channel/diff/apply,
  `RuntimeConfigurable`, runtime schema marker, and exported `StartService`/`StopService`/`RemoveService`.
- [ ] A.8 Remove `ServiceConfig.Name`, loader message-logger injection, logger inner `enabled`/`log_level`, and metrics
  inner `enabled`; strict config rejects them without aliases.
- [ ] A.9 Add deterministic `GET /services` `restart_required` plus sorted pending
  add/enable/disable/remove/reconfigure classifications.
- [ ] A.10 Prove runtime rows, `HTTPHandler` routes, and OpenAPI contributors match sealed subsets while
  health/readiness/`GRAPH_STATUS` remain unchanged.
- [ ] A.11 Obtain independent review for Slice A.

## B. Registry generation snapshots and resource admission

- [ ] B.1 Add failing tests for exactly one `InputPorts`/`OutputPorts` capture per successful generation, defensive
  cloning, factory identity, and no record on failed admission.
- [ ] B.2 Retain component, factory identity, cloned effective ports, normalized facts, exclusive-resource facts, and
  local generation as one immutable record.
- [ ] B.3 Make Registry sole declaration-derived exclusive-resource admission owner; remove ComponentManager parallel
  resource tracker and bookkeeping.
- [ ] B.4 Remove/internalize identity-free direct admission with no alias or shim.
- [ ] B.5 Add internal framework-only, complete-set, latest-state, non-blocking coalescing observation, including
  initial empty state and cancellation; create no cross-repo API/contract or ADR promise and use no KV, JetStream,
  durable store, or durable replay.
- [ ] B.6 Prove normalized-fact equality before declaration-neutral component/config mutation.
- [ ] B.7 Reject declaration-changing live updates before mutation with typed
  `declaration_change_requires_replacement`, or prepare full replacement off-Registry.
- [ ] B.8 Make replacement/removal atomically update component, factory identity, declaration, and resource
  projections; failed preparation changes nothing.
- [ ] B.9 Keep admitted start failures inspectable without implying readiness, grouping, cohort membership, provider
  phase, or orchestration progress.
- [ ] B.10 Obtain independent review for Slice B.

## C. Consumer migrations and message logger

- [ ] C.1 Move flowgraph, capability publication, management responses, and conflict reporting to defensive Registry
  snapshots; no component port re-read remains.
- [ ] C.2 Capture capability snapshots before asynchronous publication.
- [ ] C.3 Make only an outer-enabled, started message-logger in `"*"` mode attach Registry observer and lazily reconcile
  declared subjects.
- [ ] C.4 Reconcile complete add/replace/remove snapshot sets; union explicit subjects; cancel observer and all
  logger-owned subscriptions on Stop.
- [ ] C.5 Deliberately deduplicate exact and the three accepted containment overlaps—new
  `agent.toolcall.proposed.*` under raw `agent.toolcall.proposed.>`; raw `agent.toolcall.approved.*` under new
  `agent.toolcall.approved.>`; raw `agent.toolcall.rejected.*` under new `agent.toolcall.rejected.>`—and expose the
  resolved union/overlap handling through runtime inspection.
- [ ] C.6 Delete raw component-config port parsing from message-logger.
- [ ] C.7 Prove 25-config census: 389/245/51 raw becomes 565/380/66 effective; delta 176/135/15, zero removals, 41
  exact collapses, three named wildcard overlaps without duplicate capture.
- [ ] C.8 Prove omitted/disabled logger creates no instance, buffer, observer, subscription, route, delivery work,
  capture, or backpressure.
- [ ] C.9 Obtain independent review for Slice C.

## D. Stream-planning invariant and removal searches

- [ ] D.1 Preserve preconstruction `PortConfig` provisioning intent and separate accepted runtime Registry declaration
  fact.
- [ ] D.2 Prove both consume canonical resolution/facts and import none of the other's policy.
- [ ] D.3 Add structural 61 default-only JetStream-output census: 61 explicitly covered by `AGENT` / `agent.>`, zero
  uncovered.
- [ ] D.4 Reject any future uncovered default-only output rather than guessing a stream from runtime snapshots.
- [ ] D.5 Run exact production searches proving absence of identity-free admission, ComponentManager resource
  tracking/re-reads, dynamic service mutation surfaces, retired service fields/helpers, compatibility shims, durable
  declaration stores, group/cohort/readiness fields, and speculative restart-success state.
- [ ] D.6 Confirm no index, hierarchy, research, retention, readiness, `GRAPH_STATUS`, service-state bucket/stream,
  scheduler, or dynamic mux work entered diff.
- [ ] D.7 Obtain independent review for Slice D.

## E. Verification, E2E, and downstream holdouts

- [ ] E.1 Run `task lint` and `task build`.
- [ ] E.2 Run `go test -race ./...`.
- [ ] E.3 Run `task test:integration`.
- [ ] E.4 Run `task schema:generate` and prove no unintended `schemas/` or `specs/` drift.
- [ ] E.5 Run `go test ./test/contract/...` and `task openspec:validate`.
- [ ] E.6 Run breaking relevant tiers independently: `task e2e:core`, `task e2e:agentic`,
  `task e2e:semantic`; then `task e2e:all`.
- [ ] E.7 Record every command, baseline, result, and intentionally excluded gate.
- [ ] E.8 Obtain `semstreams-reviewer` approval on complete implementation and OpenSpec diff.
- [ ] E.9 Perform read-only parity census of `semdev`, `semmachina`, `semsource`, `semboids`, `semdragon`,
  `semstreams-ui`, `semteams`, `semconnect`, `semlink`, `semops` only after framework gates. Do not implement
  downstream here; differences are migration evidence and do not reopen/block framework ruling.
- [ ] E.10 Re-run all negative searches on merged tree and archive only when task truth/evidence complete.
