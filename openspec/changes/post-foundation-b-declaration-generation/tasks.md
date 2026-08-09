# Tasks — Post-Foundation-B declaration generation

Implementation is active. Slices A, B, C, and D are complete and independently approved. Slice E verification is
complete through E.9; E.10 remains unchecked for merged-tree closeout and archive.

Each slice receives independent SemStreams review before the next starts. Later slices MUST NOT reopen
owner-accepted rulings absent implementation evidence of an internal contradiction.

## A. Service composition, restart truth, and dead surfaces

- [x] A.1 Characterize unchanged arbitration: newer file pushes file state; equal/older file selects KV;
  equal-version content edit does not apply.
- [x] A.2 When KV is selected, replace only Services from current `services.*` keys; retain existing synchronization
  for every other top-level section.
- [x] A.3 Add pure non-mutating outer `ServiceConfigs` resolver: map-key identity, canonical raw JSON, mandatory entry
  materialization, explicit-false preservation, optional outer defaults, no inner service interpretation.
- [x] A.4 Construct from post-Start `SafeConfig` and retain immutable clone of exact resolved boot desired map.
- [x] A.5 Make `CreateService` and error-returning `RegisterInstance` pre-seal APIs; reject duplicates and post-seal
  calls typed, with no void wrapper.
- [x] A.6 Validate configured/mandatory composition, register fixed root services, seal sorted full identity set, then
  start/bind routes/expose OpenAPI.
- [x] A.7 Remove service-manager `services.*` mutation subscription/watcher/channel/diff/apply,
  `RuntimeConfigurable`, runtime schema marker, and exported `StartService`/`StopService`/`RemoveService`.
- [x] A.8 Remove `ServiceConfig.Name`, loader message-logger injection, logger inner `enabled`/`log_level`, and metrics
  inner `enabled`; strict config rejects them without aliases.
- [x] A.9 Add deterministic `GET /services` `restart_required` plus sorted pending
  add/enable/disable/remove/reconfigure classifications.
- [x] A.10 Prove runtime rows, `HTTPHandler` routes, and OpenAPI contributors match sealed subsets while
  health/readiness/`GRAPH_STATUS` remain unchanged.
- [x] A.11 Obtain independent review for Slice A.

Slice A evidence: independent `semstreams-reviewer` verdict `REVIEW PASS / APPROVE`; all findings closed. Targeted
lifecycle evidence is `TestConfigureFromServicesRejectsPostSealWithoutRewritingBootTruth`,
`TestConfigureFromServicesRejectsFixedConfiguredIdentityCollision`, and
`TestServiceCompositionDesiredStateProductionSeam`, proving typed post-seal/collision rejection without boot-truth
mutation and stable desired-versus-sealed reporting. Uncached `go test -race -count=1 ./types ./config ./service`,
shipped-config checks, schema drift check, contract tests, and strict OpenSpec validation pass.

## B. Registry generation snapshots and resource admission

- [x] B.1 Add failing tests for exactly one `InputPorts`/`OutputPorts` capture per successful generation, defensive
  cloning, factory identity, and no record on failed admission.
- [x] B.2 Retain component, factory identity, cloned effective ports, normalized facts, exclusive-resource facts, and
  local generation as one immutable record.
- [x] B.3 Make Registry sole declaration-derived exclusive-resource admission owner; remove ComponentManager parallel
  resource tracker and bookkeeping.
- [x] B.4 Remove/internalize identity-free direct admission with no alias or shim.
- [x] B.5 Add internal framework-only, complete-set, latest-state, non-blocking coalescing observation, including
  initial empty state and cancellation; create no cross-repo API/contract or ADR promise and use no KV, JetStream,
  durable store, or durable replay.
- [x] B.6 Prove normalized-fact equality before declaration-neutral component/config mutation.
- [x] B.7 Reject declaration-changing live updates before mutation with typed
  `declaration_change_requires_replacement`, or prepare full replacement off-Registry.
- [x] B.8 Make replacement/removal atomically update component, factory identity, declaration, and resource
  projections; failed preparation changes nothing.
- [x] B.9 Keep admitted start failures inspectable without implying readiness, grouping, cohort membership, provider
  phase, or orchestration progress.
- [x] B.10 Obtain independent review for Slice B.

Slice B evidence: Registry admission-capture tests first failed on the absent generation record, then pass with exactly
one admission capture of each port lane, validated factory identity, defensive declaration/fact clones, no disabled,
invalid, conflicting, or failed-preparation record, atomic replacement/removal, and package-internal complete-set
add/replace/remove observation with initial empty state, coalescing, and cancellation. Generation snapshot/observer
reads are not exported; cross-package proof/replacement coordination requires a root `internal` access token and the
proof type is unexported. This exactly-once evidence is scoped to Registry admission capture; Slice C still owns
eliminating pre-existing capability/heartbeat and other consumer re-reads.

Replacement tests prove exclusive resources are reserved before initialization, a conflicting candidate is never
initialized, old state stays visible until causal commit, and canceled reservations remain quarantined until candidate
cleanup succeeds. Blocked cleanup rejects successor and same-name admission; failed cleanup is surfaced and retains the
quarantine. Remove/initialize-failure paths admit a successor claimant after successful cleanup. ComponentManager
live-PUT tests prove normalized-fact equality before mutation, generation-bound proof, typed
`declaration_change_requires_replacement` refusal before
component or retained-config mutation, and per-instance serialization across proof, live apply, retained-config commit,
replacement, and removal. Stale watcher snapshots are re-read after sequencing, so replacement/removal acts on the
authoritative managed generation. Old-generation Stop failure aborts replacement before commit, cleans the candidate,
retains the old Registry generation as failed, and prevents a new Start; removal Stop failure likewise leaves the old
component and Registry record admitted. Start-failure tests distinguish that pre-commit preservation from an admitted
replacement whose Start failed; the admitted replacement remains inspectable while lifecycle state reports failure
separately.
Focused component and service race tests pass repeatedly; full revive-backed `task lint` passes. Component
integration-tagged and service test binaries compiled in the Slice B pass, and strict OpenSpec validation passes.
Independent `semstreams-reviewer` verdict: `REVIEW PASS / APPROVE`; all Slice B findings are closed.

## C. Consumer migrations and message logger

- [x] C.1 Move flowgraph, capability publication, management responses, and conflict reporting to defensive Registry
  snapshots; no component port re-read remains.
- [x] C.2 Capture capability snapshots before asynchronous publication.
- [x] C.3 Make only an outer-enabled, started message-logger in `"*"` mode attach Registry observer and lazily reconcile
  declared subjects.
- [x] C.4 Reconcile complete add/replace/remove snapshot sets; union explicit subjects; cancel observer and all
  logger-owned subscriptions on Stop.
- [x] C.5 Deliberately deduplicate exact and the three accepted containment overlaps—new
  `agent.toolcall.proposed.*` under raw `agent.toolcall.proposed.>`; raw `agent.toolcall.approved.*` under new
  `agent.toolcall.approved.>`; raw `agent.toolcall.rejected.*` under new `agent.toolcall.rejected.>`—and expose the
  resolved union/overlap handling through runtime inspection.
- [x] C.6 Delete raw component-config port parsing from message-logger.
- [x] C.7 Prove 21-config census: 385/243/51 raw becomes 561/378/66 effective; delta 176/135/15, zero removals, 41
  exact collapses, three named wildcard overlaps without duplicate capture.
- [x] C.8 Prove omitted/disabled logger creates no instance, buffer, observer, subscription, route, delivery work,
  capture, or backpressure.
- [x] C.9 Obtain independent review for Slice C.

Slice C.1-C.6/C.8 evidence: Registry capability preparation, flowgraph construction, ComponentManager management
views, and flow validation consume defensive admitted-generation ports/facts; the only production component port-method
calls remaining are the Registry admission capture. Capability JSON and publish subject are captured before the
asynchronous publisher starts. Focused component, flowgraph, service, and race tests pass for retained publication,
management views, latest-state observer replacement/removal, logger add/replace/remove reconciliation, explicit-only
observer absence, exact/named-containment deduplication with runtime inspection, single capture, Stop cleanup, and
omitted/disabled no-construction/no-route behavior. Raw message-logger `PortConfig` parsing is removed.

C.7 evidence is the version-2 artifact at `service/testdata/message_logger_subject_census.json`, frozen against baseline
SHA `f2b7c4506ae78b1b8ace9fbc581994a2d14f1d55` and the 2026-08-09 owner ruling. The ruling retired exactly four
configurations whose enabled factories had no production registration:
`configs/http-gateway-semantic-search.json`, `configs/semantic-basic.json`,
`configs/examples/bm25-semantic-search.json`, and `configs/examples/pathrag-graph-traversal.json`. No alias, synthetic
factory, substitute configuration, or census exclusion was added.

The production-loader/real-factory harness discovers all 21 remaining shipped component configurations, constructs
every enabled component, accumulates every construction failure deterministically, and fails if any exist. The
completeness pass exposed and the owner ruled three bounded production prerequisites: WebSocket documented duration
decoding plus explicit `ws_control` in `configs/cloud-federation.json@1.0.2`; explicit `ALIAS_INDEX` in
`configs/hello-world.json@1.1.1`; and core-NATS declarations matching mission-command behavior in
`configs/lifecycle-flow.json@1.1.1`. The final unskipped pass proves 385/243/51 raw becomes 561/378/66 effective, with
the unchanged 176/135/15 delta, zero removals, 41 exact collapses, and the three accepted agentic overlaps.

Independent `semstreams-reviewer` verdict: `APPROVE`; all Slice C findings are closed. The final observer ordering
proof passed 800 race-enabled fresh-registry add/replace/remove sequences. Focused component, flowgraph, engine,
WebSocket, port-grammar, and service race suites pass; logger lifecycle and failed-subscription reconciliation tests
pass repeatedly; the validator retains real factory identity with no synthetic registration; the census mechanically
derives the 40 loop/dispatch and one governance exact-collapse attribution. Full Go tests, lint, build, contract tests,
strict OpenSpec validation (37/37), diff checks, and negative `flow-validation-` searches pass.

## D. Stream-planning invariant and removal searches

- [x] D.1 Preserve preconstruction `PortConfig` provisioning intent and separate accepted runtime Registry declaration
  fact.
- [x] D.2 Prove both consume canonical resolution/facts and import none of the other's policy.
- [x] D.3 Add structural 61 default-only JetStream-output census: 61 explicitly covered by `AGENT` / `agent.>`, zero
  uncovered.
- [x] D.4 Reject any future uncovered default-only output rather than guessing a stream from runtime snapshots.
- [x] D.5 Run exact production searches proving absence of identity-free admission, ComponentManager resource
  tracking/re-reads, dynamic service mutation surfaces, retired service fields/helpers, compatibility shims, durable
  declaration stores, group/cohort/readiness fields, and speculative restart-success state.
- [x] D.6 Confirm no index, hierarchy, research, retention, readiness, `GRAPH_STATUS`, service-state bucket/stream,
  scheduler, or dynamic mux work entered diff.
- [x] D.7 Obtain independent review for Slice D.

Slice D evidence: `service/stream_planning_census_test.go` extends the production-loader/real-factory 21-config census
without changing either runtime owner. Raw configured outputs are resolved through canonical `PortDefinition` and
`PortFacts`; admitted effective outputs come from defensive Registry generation snapshots. Coverage is read only from
explicit `config.streams` declarations. The guard proves exactly 61 default-only JetStream output rows, split 45
`agentic-loop` and 16 `agentic-dispatch`, and every row is covered only by `AGENT` / `agent.>`, leaving zero uncovered.
The synthetic `future.events` row has no explicit coverage and fails with a structural uncovered-default error; the
subject-family test also proves a `future.>` declaration is not falsely treated as covered by `future.*`.

Developer-reported historical RED — **UNVERIFIED**, because no retained artifact preserves the preimplementation run:
the absent structural implementation returned 0/0/0 instead of 61/61/0 and failed to reject the synthetic future row.
The current green is verified by the retained tests: the focused tests and existing message-logger census pass under
`go test -race -count=1`; canonical component/config port-facts and stream-planning tests pass under race as well.
`task lint`, strict OpenSpec validation (37/37), `git diff --check`, and the schema/spec drift check pass.

D.5 is reproducible with the following exact production searches; each returns zero matches:

```bash
rg -n 'func \([^)]*\*Registry\) RegisterInstance\(|RegisterInstance(Legacy|Compat)' \
  component -g '*.go' -g '!**/*_test.go'
rg -n -e 'checkPortConflicts|registerPorts|unregisterPorts' \
  -e 'registeredPorts|resourceTracker|portResources' \
  -e '\.InputPorts\(\)|\.OutputPorts\(\)' \
  service -g 'component_manager*.go' -g '!**/*_test.go'
rg -n -e 'RuntimeConfigurable' \
  -e 'func \([^)]*\*Manager\) (StartService|StopService|RemoveService)\(' \
  -e 'serviceConfigUpdates|watchServiceConfig|applyServiceConfig|diffService' \
  -e 'Subscribe\([^)]*"services\.\*"' \
  service -g '*.go' -g '!**/*_test.go'
rg -n 'Name[[:space:]]+string' types/service.go
rg -n 'json:"(log_level|enabled)"|LogLevel|Runtime:[[:space:]]*true' \
  service/message_logger.go service/metrics.go
rg -n -e 'RegisterInstance(Legacy|Compat)|StartService(Legacy|Compat)' \
  -e 'StopService(Legacy|Compat)|RemoveService(Legacy|Compat)' \
  -e 'RuntimeConfigurableCompat|ServiceConfigName' \
  component service types config cmd -g '*.go' -g '!**/*_test.go'
rg -n -e 'DeclarationSnapshot(KV|Stream|Bucket|Store)' \
  -e 'declaration[_-](snapshot|generation)[_-](kv|stream|bucket|store)' \
  -e 'DECLARATION_(SNAPSHOT|GENERATION|STORE|BUCKET|STREAM)' \
  component service config graph -g '*.go' -g '!**/*_test.go'
rg -n '(Group|Cohort|ProviderPhase|Ready|Readiness|Healthy|Started|Enabled)[[:space:]]+(bool|string)' \
  component/registry.go
rg -n 'restart_blocked|restart_success|restart_succeeded|will_succeed|config_error' \
  service -g 'service_manager*.go' -g '!**/*_test.go'
```

D.6 is reproducible with the following exact added-line searches; both return zero matches:

```bash
git diff -U0 -- docs/proposals/post-foundation-b-declaration-generation-design-review.md \
  docs/proposals/post-foundation-b-declaration-generation-design.md \
  openspec/changes/post-foundation-b-declaration-generation/design.md service/message_logger.go | \
  rg -e '^\+.*(GRAPH_STATUS|KeyGraph|Bucket[A-Za-z]*Status|Readiness|Scheduler)' \
    -e '^\+.*(DynamicMux|Hierarchy|Research|Retention|service[_-]?state)'
rg -n -e 'GRAPH_STATUS|KeyGraph|Bucket[A-Za-z]*Status|Readiness|Scheduler' \
  -e 'DynamicMux|Hierarchy|Research|Retention|service[_-]?state' \
  service/stream_planning_census_test.go
```

The only Go changes are the structural test and the corrected message-logger config comment. Three tracked
design/review files only propagate the already-approved C.9 review status, and this task file records Slice D truth.
Independent `semstreams-reviewer` verdict: `APPROVE`; all findings are closed. The reviewer independently reproduced
the 61/61/0 census, exercised the subject-family inclusion helper across 160 valid patterns and 1,092 concrete
subjects, ran the exact D.5/D.6 searches, and confirmed no exported or runtime surface entered the slice.

## Slice D owner-ruling conformance

| Owner ruling | Evidence | Deviation |
|---|:---:|:---:|
| R4: default-only JetStream coverage is exactly 61/61/0 | D1 | None |
| R7: provisioning intent and runtime declaration remain distinct | D2 | None |
| R10: classification is shared while owner policy remains local | D3 | None |
| Future uncovered defaults fail without Registry-derived guessing | D4 | None |
| No exported/runtime surface or named non-goal entered | D5 | None |

- **D1:** `service/stream_planning_census_test.go:34-64,111-197`.
- **D2:** `config/stream_bounds.go:245-299`; `component/registry.go:841-888`;
  `service/stream_planning_census_test.go:123-165`.
- **D3:** `config/stream_bounds.go:254-281`; `component/registry.go:872-888`;
  `service/stream_planning_census_test.go:199-271`.
- **D4:** `service/stream_planning_census_test.go:66-100,179-212`.
- **D5:** added-line D.5/D.6 searches recorded above; `git status --short` file set.

## E. Verification, E2E, and downstream holdouts

- [x] E.1 Run `task lint` and `task build`.
- [x] E.2 Run `go test -race ./...`.
- [x] E.3 Run `task test:integration`.
- [x] E.4 Run `task schema:generate` and prove no unintended `schemas/` or `specs/` drift.
- [x] E.5 Run `go test ./test/contract/...` and `task openspec:validate`.
- [x] E.6 Run breaking relevant tiers independently: `task e2e:core`, `task e2e:agentic`,
  `task e2e:semantic`; then `task e2e:all`.
- [x] E.7 Record every command, baseline, result, and intentionally excluded gate.
- [x] E.8 Obtain `semstreams-reviewer` approval on complete implementation and OpenSpec diff.
- [x] E.9 Publish the owner-approved communicate-only downstream migration notice after framework gates. Do not perform
  an exhaustive parity audit or implement downstream migrations here; downstream teams own tag adoption, compilation,
  explicit migration fixes, flow validation, and product E2E.
- [ ] E.10 Re-run all negative searches on merged tree and archive only when task truth/evidence complete.

Slice E.1-E.7 evidence baseline: clean `13157ff0633655de593771bda61ed925b40442c8`. Docker commands required
sandbox escalation for environment permission; this was not a code failure.

| Task | Exact command | Verified result |
|:---:|---|---|
| E.1 | `task lint` | PASS in about 4.11s |
| E.1 | `task build` | PASS in about 2.82s |
| E.2 | `go test -race ./...` | PASS in about 56.05s |
| E.3 | `task test:integration` | PASS in about 119.58s; Docker/NATS actively polled |
| E.4 | `task schema:generate` | PASS in about 1.43s; only the existing nonfatal missing meta-schema warning |
| E.4 | `git diff --exit-code -- schemas specs` | PASS; no schema or OpenAPI drift |
| E.5 | `go test ./test/contract/...` | PASS in about 2.86s |
| E.5 | `task openspec:validate` | PASS; 37/37 strict validations |
| E.6 | `task e2e:core` | PASS; 3/3 scenarios |
| E.6 | `task e2e:agentic` | PASS in about 556ms |
| E.6 | `task e2e:semantic` | PASS, exit 0; 48/48 in 10m56s |
| E.6 | `task e2e:all` | PASS, exit 0; every tier green |
| E.7 | `git diff --check` | PASS |
| E.7 | `git status --short` | Clean verification worktree |

Integration's longest packages all completed: `natsclient` 103.112s, `service` 73.843s, `graph-index` 70.326s,
`graph-ingest` 67.586s, `storage/objectstore` 67.129s, `rule` 65.218s, and `agentic-loop` 54.109s. No package
remained stalled beyond twice its expected duration.

The independent agentic tier recorded one governance decision, one tool execution, ten trajectory facts, ten loop
triples, and six model triples. The independent semantic tier resolved 89 embeddings with zero failed and zero
pending, passed known answers 7/7, and proved exact batch reconciliation.

The semantic recorder is deliberately non-gating. It observed one cold-chain degraded-floor violation under
`answer_synthesis_timeout` in each semantic pass: the independent semantic run and the semantic tier inside
`task e2e:all`. The hard known-answer gate remained 7/7, no answer was fabricated, fallback worked, and both commands
exited successfully. The all-tier run passed core 3/3, structural 38/38, statistical 41/41, semantic 48/48 in 10m49s,
and agentic PASS.

E.9 closure follows the owner's communicate-only scope ruling. The published
`docs/operations/37-port-and-declaration-generation-cutover.md` records every intentional break, adopter action, the
related GS-01 mapping, and the absence of compatibility shims. A quick read-only sizing check found retired flat
`PortDefinition` usage in `semsource` and `semteams`; `semsource` also has pre-existing graph-foundation
`OpenCatalogBucket` and `pkg/ownership` debt. These examples are not exhaustive and do not block the framework change.

E.8 final independent review found and closed one production blocker: explicit-only message-logger subscriptions
inherited a context canceled immediately after successful `Start`, silently suppressing later delivery. The corrected
implementation separates subscription and retry lifetimes; a Docker-backed real-NATS integration regression proves a
live callback and capture after `Start`, canceled no-capture behavior after `Stop` even when unsubscribe initially
fails, and successful retained-handle unsubscribe on the second `Stop`. The review also replaced current flowgraph
guidance for the deleted `AddComponentNode` API with the supported root-owned `ComponentManager.GetFlowGraph` boundary.
A final medium finding about callback/test ordering was corrected and independently confirmed closed. Final
`semstreams-reviewer` disposition: `APPROVE`.

Focused post-review verification passed under race for the new integration regression and for `./service` plus
`./component/flowgraph`; focused vet, `git diff --check`, and strict OpenSpec validation (37/37) also pass. E.10 remains
excluded until merged-tree negative searches and archive readiness are due. The final fixed tree also passed
`task lint`, `task build`, `go test -race ./...`, and `task test:integration`, with `natsclient` longest at 97.474s.
It also passed `task schema:generate` with no `schemas/` or `specs/` drift, `go test ./test/contract/...`, and
`task e2e:core` 3/3. No commit or push is part of this evidence update.

## Slice C owner-ruling conformance

| Owner ruling | Implementation evidence | Deviation |
|---|---|---|
| Retire exactly four unregistered-factory configs | C1 | None |
| Add no alias, synthetic factory, substitute config, or exclusion | C2 | None |
| Derive the complete 21-config census through production paths | C3 | None |
| Fail on every future unknown enabled factory | C4 | None |
| Decode documented WebSocket durations within the component seam | C5 | None |
| Preserve strict WebSocket port replacement and declare both outputs | C6 | None |
| Preserve strict graph-index validation and declare `ALIAS_INDEX` | C7 | None |
| Describe mission-command's actual core-NATS behavior | C8 | None |
| Remove live usage references while preserving historical evidence | C9 | None |
| Keep C.9 unchecked until independent approval and do not begin D before C review | C10 | None |

- **C1:** artifact ruling at `service/testdata/message_logger_subject_census.json:8-18`; absence guard at
  `service/message_logger_census_test.go:76-120`.
- **C2:** prohibited substitutes at `service/testdata/message_logger_subject_census.json:14-18`; construction has no
  exclusion lane at `service/message_logger_census_test.go:220-248`.
- **C3:** production loader/factories and exact comparisons at
  `service/message_logger_census_test.go:89-163,210-248`; frozen values at
  `service/testdata/message_logger_subject_census.json:25-73`.
- **C4:** complete-set failure gate and unknown-factory test at
  `service/message_logger_census_test.go:122-124,166-185,243-248`.
- **C5:** private duration decoder at `input/websocket/register.go:42-125`; string, numeric, default, and invalid tests
  at `input/websocket/register_test.go:20-100`.
- **C6:** explicit versioned config at `configs/cloud-federation.json:2,58-80`; corrected examples at
  `input/websocket/README.md:63-106` and `input/websocket/doc.go:144-187`.
- **C7:** version and explicit fourth output at `configs/hello-world.json:2,196-225`.
- **C8:** version and canonical NATS declarations at `configs/lifecycle-flow.json:2,95-125`.
- **C9:** live links end at `gateway/http/README.md:467-470`; historical rows remain at
  `docs/proposals/foundation-b-port-language-worklist.tsv:274,380-383` and
  `docs/adr/065-predicate-index-composite-key-sharding.md:373`.
- **C10:** C.9 changed only after independent approval; Slice D began afterward and received its own independent
  review before Slice E at `openspec/changes/post-foundation-b-declaration-generation/tasks.md:134-230`.
