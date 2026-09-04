# Tasks: agentic-loop restart-safe settlement

Every behavior test added by this change SHALL carry a source comment in the exact form
`// spec: <capability> / <Requirement heading>`. The capability and heading SHALL match an active delta exactly;
design-section shorthand is not a valid citation. Every implementation slice follows RED → implementation → GREEN.

## 0. Accepted gates

- [x] 0.1 Complete the original file:line surface, lane, state, lifecycle, and adopter inventory.
- [x] 0.2 Receive independent `INVENTORY PASS` for the exact #759 foundation inventory, SHA-256
  `3b53c6d3d4f3298d63ffc2231b209aa8e1f4379a6c1bf75b7aa5edc6a4f65ffb`, 555/555 pins.
- [x] 0.3 Receive independent design review and owner acceptance of the two choices on #1146 comment `5516511726`.
- [x] 0.4 Integrate nested PR #1251 and receive post-integration `INVENTORY PASS`, SHA-256
  `2888e28a7439ff4dc62345bf9a1e476054c292326ac291ab1d4519f9c0600a73`, 181/181 pins.
- [x] 0.5 Receive publisher-addendum `INVENTORY PASS`, SHA-256
  `0adba4f0092017d84f1ef181ebaf3299323f5cc75b999825bd1e16d6e292930f`, 226/226 pins.
- [x] 0.6 Materialize the accepted target into proposal, canonical design, tasks, and seven capability deltas.
- [x] 0.7 Receive independent pre-implementation design review of the complete active OpenSpec.
- [x] 0.8 Accept `inventory-dispatch-bridge-boundary-2026-09-04.md`, base
  `79b0f29f82ce5391013f6c931fae69a28216ac93`, SHA-256
  `cf5660a3b4196324a3695dc1174dacfb804cef56e2336536d4a9f7d8f4197daa`, after independent `INVENTORY PASS` with
  249/249 pins.
- [x] 0.9 Accept `inventory-task-loop-cardinality-2026-09-04.md`, SHA-256
  `22d593d5de5eea2d15a94da36162cae8b5a3a36cbfcc7790003c13a52ba7d340`.
- [x] 0.10 Accept the independently reviewed dispatch edge-gateway checkpoint
  `design-dispatch-edge-gateway-2026-09-04.md`, current SHA-256
  `aba1202c38856d71d6c551f7cb9f690a03d7eeaa981e6de5e4165b09e0ea938a`. The owner-cited pre-final token-
  projection checkpoint `339cf2b2c734ef48a2898ce6b79c3783577a8b4ae152b65a1078b00445949b76` is provenance only and is superseded.
- [x] 0.11 Receive independent `DESIGN REVIEW PASS` of the synchronized edge-gateway target state before
  implementation. The review verified lane-scoped correlation, ordinary at-least-once publication, exclusive
  dispatch edge ownership, seven capability deltas, corrected checkpoint provenance, exact MODIFIED headings, and
  graph-view lifecycle preservation.

## 1. Settlement foundation and consumer authority

- [x] 1.1 Verify PR #1159 still targets `codex/gh759-semantic-settlement`, the remote parent remains exact
  `F=417beae5552f8f15ad3540edd7d8504c87174c13`, and implementation begins from exact post-#1251 checkpoint
  `P=09ba38b1de5e7200e72281c8e4b8941d81be1da2`. Any parent advance or inventory drift stops work for rebase,
  reinventory, retest, and re-review.
- [x] 1.2 RED: add setup-failure tests for model heartbeat 90s/AckWait 120s and loop heartbeat 60s/BackOff
  `[30s,2m]`, plus passing 60s/120s and 15s/`[30s,2m]` cases. Cite exactly
  `// spec: agentic-model / Model heartbeat policy is valid before acquisition` and
  `// spec: agentic-loop / Long-running loop heartbeat policy is valid before acquisition`.
- [x] 1.2a RED: add model and loop owner-fatal health tests plus loop MaxDeliver 1 rejection and MaxDeliver 2
  acquisition tests. Prove exact-handle drain, no work/heartbeat/settlement, first-cause retention, and exactly one
  existing error-count increment. Cite exactly
  `// spec: agentic-model / Model request settlement is bound to a durable response`,
  `// spec: agentic-loop / All six loop input classes settle after owner-specific durable done`, and
  `// spec: agentic-loop / Long-running loop heartbeat policy is valid before acquisition` as applicable.
- [x] 1.3 Implement model default 60s and loop default/schema 15s; require loop MaxDeliver at least the fixed BackOff
  length 2 and reject explicit 1 before allocation without truncating BackOff. Reconcile typed configuration,
  defaults, generated schemas, docs, and every fixture. Latch the first model/loop delivery-owner fatal result into
  existing negative health and one error-count increment before draining the exact handle; add no health surface.
- [x] 1.4 GREEN: prove invalid policy allocates zero consumers, valid policy reaches acquisition, unavailable delivery
  metadata quarantines and stops the exact heartbeat owner, and immutable `DeliveryAttempt` exposes no native message,
  settlement method, sequence, consumer identity, header, or mutable state. Lease-math tests cite the two heartbeat-
  policy requirements from 1.2. Model metadata/attempt tests cite
  `// spec: agentic-model / Model request settlement is bound to a durable response`; loop metadata/owner-health tests
  cite `// spec: agentic-loop / All six loop input classes settle after owner-specific durable done`.
- [x] 1.5 RED: add the `natsclient` settlement truth table for valid Ack, Retry, Terminate, Quarantine, every invalid
  decision/error tuple, nil message, and each terminal-method error. Capture callbacks from all eight actual production
  non-heartbeat setup branches and prove each reaches its typed business handler and observable business/settlement
  effect. Prove governance publication without PubAck, failed dispatch task publication, a rejected pending-approval
  projection, required loop KV/publication failure, and a missing or full verdict waiter cannot become Ack. Add
  focused approval-panic, first-fatal Health, exact-handle drain, and terminal graph-write join tests. Use no
  synthetic AckWait-derived deadline or wall-clock lease-margin run. Cite its exact owner requirement:
  `// spec: jetstream-consumer-policy / settlement-only delivery decisions use one shared interpreter` for the shared
  truth table, and
  `// spec: agentic-dispatch / Every dispatch durable input settles through its owner`,
  `// spec: agentic-governance / Governance validation settles after its declared consequence`, or
  `// spec: agentic-loop / All six loop input classes settle after owner-specific durable done`.
- [x] 1.6 Implement `natsclient.SettleDelivery` as the shared tuple interpreter and immediate terminal-method mapper.
  Route each non-heartbeat production callback through its typed handler and this helper after work joins. Retain work
  invocation, callback context, panic recovery, admission, health latch, and exact-handle drain in the existing
  private binding. Propagate required JetStream PubAck and loop-state KV failures; until later identity and
  lane-specific recovery tasks prove another attempt safe, classify partial or commit-unknown outcomes as Quarantine
  without inventing recovery state. Retain approval panic as a non-nil fatal result and make `runWithBudget`
  synchronous.
  Add no `DeliveryPolicy`, deadline validator, owner framework, test-only API, metric family, goroutine, or durable
  state.
- [x] 1.7 GREEN: prove the shared settlement truth table and each captured production branch's real handler/effect.
  Prove invalid or quarantined outcomes perform no terminal method, approval panic performs no persistence or
  settlement and drains only its exact owner, the first fatal cause reaches existing negative Health exactly once,
  and terminal approval rejection cannot settle while graph-write work remains live. This proves the absence of
  false-positive settlement and the exact owner reaction; it does not claim deterministic replay or convergence,
  which remain owned by tasks 2, 6, 7, and 8.

## 2. Lane-scoped task, request, and tool-work correlation

- [ ] 2.1 RED: prove stable TaskID, random LoopID minting for new work, retained-`TaskMessage` LoopID recovery on
  redelivery, and conflicting TaskID-to-LoopID quarantine. Cite exactly
  `// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID`.
- [ ] 2.2 Implement the TaskID-to-retained-`TaskMessage` recovery path. Mint LoopID randomly only when exact retained
  task evidence proves this is new work; reuse the retained LoopID on redelivery. Add no route-claim state or
  deterministic LoopID derivation.
- [ ] 2.3 RED: prove RequestID distinguishes logical provider work and framework execution identity distinguishes tool
  work across same-CallID/different-RequestID cases. Cite exactly
  `// spec: agentic-model / Model request settlement is bound to a durable response`,
  `// spec: agentic-loop / Tool execution has stable framework correlation`, and
  `// spec: agentic-tools / Completed tool outcome identity is globally unambiguous`.
- [ ] 2.4 Implement RequestID and execution identity only on provider/tool/governance-correlation paths that need
  them. Preserve provider CallID as request-scoped conversation data.
- [ ] 2.5 RED/GREEN: prove ordinary task/control, created, request, response, approval, terminal, governance, and
  result publications are at-least-once, source ACK waits for required PubAck, and uncertain PubAck may republish.
  Prove `Nats-Msg-Id` suppresses only within the configured duplicate window and no test treats it as retained
  commitment evidence. Cite exactly
  `// spec: agentic-dispatch / Every dispatch durable input settles through its owner`,
  `// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID`,
  `// spec: agentic-model / Model response publication is durably at-least-once`,
  `// spec: agentic-loop / Loop task, request, and tool work use only required correlation`,
  `// spec: agentic-governance / Governance publications are durably at-least-once`, and
  `// spec: agentic-tools / Tool-result publication is durably at-least-once`.
- [ ] 2.6 Remove general exact committed-output lookup and canonical-output fingerprint work. Retain exact reads only
  for retained `TaskMessage` recovery, provider ambiguity, approval reconstruction, governance verdict recovery,
  explicit LoopID/terminal routing, durable applied-state proof, and immutable completed tool outcomes at the
  executor-effect boundary.

## 3. Provider settlement

- [ ] 3.1 RED: add first-delivery, retained-response provider protection, pre-call replacement, post-return/pre-PubAck
  replacement, at-least-once response publication, unavailable metadata, correlation conflict, config omission/
  invalid-enum, unsupported provider reconciliation, and policy-table tests. Cite exactly
  `// spec: agentic-model / Model request settlement is bound to a durable response`,
  `// spec: agentic-model / Model response publication is durably at-least-once`,
  `// spec: agentic-model / Provider commit-unknown behavior is explicit`,
  `// spec: agentic-model / Provider commit-unknown is machine-readable`, and
  `// spec: agentic-model / Started markers do not claim invocation certainty` as applicable.
- [ ] 3.2 Implement `fail_commit_unknown`, `at_least_once`, and admitted `provider_reconcile`; default to
  `fail_commit_unknown` through exact JSON key `provider_ambiguity_policy`; omission/empty defaults to that value and
  the only other values are `at_least_once` and `provider_reconcile`. Reconcile exact committed matching response
  before invocation. Add exact optional `AgentResponse.failure_kind` JSON string with closed
  `AgentResponseFailureKind=provider_commit_unknown` only for error status; never infer it from error text or use a
  pre-call started marker as invocation proof. Add the package-private `providerCommitReconciler` exact method/result
  seam; enumerate every endpoint reachable through direct/default/capability routing and refuse unsupported
  `provider_reconcile` before consumer allocation. Reconcile config struct/defaults/validation, generated schema,
  shipped fixtures, and model docs.
- [ ] 3.3 GREEN: prove by counter that a matching retained response and default unresolved redelivery invoke the
  provider zero times; prove `at_least_once` repeats only by explicit opt-in, every required response gets PubAck
  before source ACK, and uncertain response publication may repeat without repeating provider work.

## 4. Loop task and response settlement

- [ ] 4.1 RED: add task-birth, post-registration failure, dropped initial-request publication, response cold-read,
  proven duplicate, conflict, missing retained evidence, and every KV/Store/publication failure test. Cite exactly
  `// spec: agentic-loop / All six loop input classes settle after owner-specific durable done`,
  `// spec: agentic-loop / Loop recovery is lane-specific and read-through`, and
  `// spec: agentic-loop / Loop task, request, and tool work use only required correlation`.
- [ ] 4.2 Migrate task, response, and tool-result bindings from the legacy helper to the permanent typed heartbeat
  owner. Add direct LoopEntity read-through by LoopID; reconstruct response configuration from committed request;
  settle loop/graph birth, lineage, initial request, created event, and terminal failure at the delivery boundary.
  Preserve every exit for #1244 as a declared transition or refusal and never encode log-and-ACK as success.
- [ ] 4.3 GREEN: prove no provider double-call, no response/tool-result log-and-ACK, no stale-correlation loss, and no
  replay beyond admitted evidence across real process replacement.

## 5. Tool result and completed outcome

- [ ] 5.1 RED: add repeated provider CallID, completed replay, partial batch, missing/colliding execution identity,
  persistence-before-next-output, and post-effect ambiguity tests. Cite exactly
  `// spec: agentic-tools / Tool outcomes preserve framework execution correlation`,
  `// spec: agentic-tools / Completed tool outcome identity is globally unambiguous`,
  `// spec: agentic-tools / Tool replay remains the sole tool-effect recovery authority`, and
  `// spec: agentic-tools / Tool delivery retains the permanent typed owner contract`,
  `// spec: agentic-tools / Tool-call completion SHALL be durable before request acknowledgement`,
  `// spec: agentic-tools / Tool-result bounds SHALL be observed rather than predicted`, and
  `// spec: agentic-tools / Executor panic and ambiguous pre-completion effects SHALL be explicit`.
- [ ] 5.2 Stamp RequestID/execution identity on every ToolCall/ToolResult path, evolve `TOOL_CALL_OUTCOMES` identity,
  reconstruct ordered batches from committed request/response/results, replace `stale_callid` log-and-drop with a
  classified outcome, and persist each result before the next publication. Reconcile current requirements
  `Tool-call completion SHALL be durable before request acknowledgement`,
  `Tool-result bounds SHALL be observed rather than predicted`, and
  `Executor panic and ambiguous pre-completion effects SHALL be explicit` so provider CallID/surrogate/effectful
  idempotency claims use framework execution identity and operation-specific effect authority. Add no claimed/in-
  progress ledger.
- [ ] 5.3 GREEN: prove exact completed replay reuses the authoritative outcome without executor invocation and
  republishes its `ToolResult` at least once until PubAck; prove every result-persistence/downstream-PubAck replacement
  boundary remains safe.

## 6. Dispatch edge gateway and approval continuation gate

- [ ] 6.1 RED: run the real-NATS approval replacement gate after an approval-required `ToolResult` fully settles.
  Replace loop and dispatch, discard every process map/cache, retain `AGENT` and `AGENT_LOOPS`, and independently
  exercise approve, modify, reject, timeout, and redelivery. Cite exactly
  `// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded`.
- [ ] 6.2 Prove the settled approval-required `ToolResult` is available from current
  `LoopEntity.PendingToolResults[PendingApproval.CallID]` and agrees with pending CallID, name, LoopID, trace, and
  approval-required classification. Perform no `ToolResult` stream lookup. Tests cite exactly
  `// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded`.
- [ ] 6.3 Implement only operation-specific exact reads for latest `agent.request.<LoopID>` and exact
  `agent.response.<RequestID>`. Validate envelopes, payloads, cross-record identities, current-call uniqueness, and
  canonical arguments. Perform no `AGENT` list or scan. Tests cite exactly
  `// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded`.
- [ ] 6.4 Prove same-CallID/different-RequestID isolation with two retained responses carrying conflicting arguments.
  Reconstruction follows only the RequestID named by the current request. Cite exactly
  `// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded`.
- [ ] 6.5 Table-test approve, modify, reject, and timeout across transient/unresolved Retry, confirmed retained
  absence to durable `continuation_unavailable`, malformed/identity conflict to Quarantine, exact match to continue,
  and durable current state proving the branch already applied. Ordinary branch publication remains at-least-once.
  Cite exactly `// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded`.
- [ ] 6.6 Stop for an owner mechanism ruling after the evidence gate. On PASS, obtain explicit revocation of comment
  `5463183450`, then remove `ApprovalContinuationV1`, Store config, digest, cleanup, and deliberate AGENT-eviction
  claims. On FAIL, retain the already-approved ObjectStore plan unchanged. Introduce no third mechanism.
- [ ] 6.7 Implement one mixed `AGENT_LOOPS` classifier for canonical current `LoopEntity` keys, activity-only
  `COMPLETE_` keys, and every current research-pipeline namespace. Known research records return `keep=false`;
  malformed would-be loop keys poison. Tests cite exactly
  `// spec: agentic-dispatch / The shared loop view classifies the mixed bucket`.
- [ ] 6.8 Add a real mixed-bucket proof covering valid current `LoopEntity`, typed terminal `COMPLETE_`, registered
  `SearchResult` `COMPLETE_`, every research namespace, malformed current/unknown keys, malformed completion,
  tombstone, and healing. Marshal and unmarshal `SearchResult` through the registered production `BaseMessage`
  envelope; prove the suffix supplies LoopID and the immutable `LoopInfo` projection reports complete, success,
  synthesis, and iterations while directional token fields remain zero. Regenerate OpenAPI and prove the existing
  `LoopInfo` JSON/schema is unchanged and contains no aggregate-to-directional token mapping. Cite exactly
  `// spec: agentic-dispatch / The shared loop view classifies the mixed bucket`.
- [ ] 6.9 Delete `LoopTracker`, its approval buffer, created/pending dispatch inputs and consumers,
  `Component.LoopTracker()`, and all tracker-driven correctness. Preserve created/pending loop outputs and external
  subscribers. Prove dispatch neither creates nor advances intermediate loop state and settles validated routeless
  non-user terminal events without `user.response`. Cite exactly
  `// spec: agentic-dispatch / Dispatch is exclusively an edge gateway`.
- [ ] 6.10 Replace `CommandContext.LoopTracker` with classified `LookupLoopOwner`. Preserve `LoopInfo` only as the
  immutable view-derived `/loops` and `/debug/state` response DTO and prove its JSON/OpenAPI schema is unchanged.
  Return 503 instead of false empty state and expose caught-up readiness/current poison diagnostics. Preserve exact
  recorded-state reporting after replacement. Cite exactly
  `// spec: agentic-dispatch / Loop existence and ownership are merged facts, never process memory alone` and
  `// spec: agentic-dispatch / Dispatch uses one authority-backed current-state projection`.
- [ ] 6.11 Serve `/activity`, `/loops`, `/debug/state`, and AutoContinue from one caught-up view. AutoContinue matches
  exact `(UserID, ChannelType, ChannelID)` with no fallback. Prove the post-task-PubAck/pre-`LoopEntity` birth gap can
  yield a second loop for route-only input and explicit LoopID provides continuity; add no route claim. Cite exactly
  `// spec: agentic-dispatch / Dispatch uses one authority-backed current-state projection` and
  `// spec: agentic-dispatch / Dispatch is exclusively an edge gateway`.
- [ ] 6.12 Remove `router_active_loops` with no authoritative Prometheus replacement. Delete exported
  `graphview.View.Restart()` and every retained context/restart closure. Recreate a failed view only inside dispatch's
  lifecycle-control owner. Prove detach-buffer release, explicit subscriber shutdown, failed-view replacement,
  shutdown/replacement race, complete join, and no surviving context/provider. Cite exactly
  `// spec: graph-view-subscription / View lifecycle and ownership` and
  `// spec: agentic-dispatch / Dispatch shutdown closes every owner without retaining context`.
- [ ] 6.13 Prove terminal user-response routing only inside source-event intersection loop-state retention, including
  replacement, route conflict, transient absence, validated routeless non-user terminal, deletion/purge, and expiry.
  Add migration notes for command, `LoopInfo`/debug behavior, metric removal, graphview `Restart` removal,
  AutoContinue tuple/birth-gap semantics, and semteams' obsolete signal endpoint; run the relevant agentic E2E before
  the breaking change lands. Cite exactly
  `// spec: agentic-dispatch / Terminal user-response routing is retention-intersection bounded` and
  `// spec: agentic-dispatch / Dispatch is exclusively an edge gateway`.

## 7. Control vocabulary, cancel, approval-response, and verdict fast lanes

- [x] 7.1 Integrate reviewed PR #1251: pause/resume handlers, persisted request fields, and unused signal verbs are
  removed. Binding product ruling #1239 comment `5526837992`, linked from #1146 comment `5526837994`, supersedes
  prior paused-state preservation.
- [ ] 7.2 RED: add failing tests that `paused` is absent from code and schema vocabulary and refused by exported
  transitions, persisted-state decoding, configuration/examples, and public documentation. Enumerate and pin
  `agentic/README.md`, `processor/agentic-loop/README.md`, `docs/concepts/13-agentic-systems.md`,
  `docs/operations/migration-beta162-to-beta163.md`, and generated `specs/openapi.v3.yaml`. Prove there is no shim,
  alias, reserved enum, or migration. Cite exactly
  `// spec: agentic-loop / All six loop input classes settle after owner-specific durable done`.
- [ ] 7.3 Remove `LoopStatePaused` and wire value `paused` from constants, validation, exported transition APIs,
  generated schema, examples, fixtures, and docs. Refuse persisted `state:"paused"` without compatibility handling.
  Add no checkpoint, supervisor, workflow state machine, or suspend placeholder.
- [ ] 7.4 GREEN: prove complete paused-state removal. Document cancel, durable ApprovalResponse wait, restart/retry
  from the last durable boundary, and operational quiesce by stop-admission/drain/cooperative-cancel/join. State that
  future suspend-at-next-durable-boundary requires a new evidence-backed contract.
- [ ] 7.5 RED: add cancel-only UserSignal vocabulary, durable cancel completion, separate ApprovalResponse, missing
  waiter, duplicate/conflict, panic, and replacement tests. Cite exactly
  `// spec: agentic-loop / All six loop input classes settle after owner-specific durable done`.
- [ ] 7.6 Refactor cancel, approval-response, approved-verdict, and rejected-verdict through their four existing
  private owners. Unknown UserSignal terminates; cancel waits for current state, `COMPLETE_<loopID>`, and terminal
  PubAck; missing process correlation never authorizes ACK. Keep `ResponseAction.Signal` and
  `ClassifiedIntent.SignalType` outside durable `agent.signal.*` ownership.
- [ ] 7.7 Reconcile current `agentic-loop` requirement
  `Per-loop in-process state is released at terminal, through the one release point`: preserve single-point release
  and unaffected scenarios, replace unconditional quiet settled-drop with lane-specific durable applied proof, Retry
  on unresolved evidence, and Quarantine on correlation conflict/impossible transition. Cite exactly
  `// spec: agentic-loop / Per-loop in-process state is released at terminal, through the one release point` in the
  new boundary tests.
- [ ] 7.8 GREEN: prove all four lanes meet their owner-specific durable-done/refusal contract across replacement.

## 8. Governance settlement and correlation

- [ ] 8.1 RED: add allowed/blocked/filter-failure/panic/budget, missing/full waiter, retained-verdict recovery, and
  proposal/verdict replacement-boundary tests. Prove uncertain ordinary validated-output PubAck retries at-least-once
  without an exact committed-output lookup. Cite exactly
  `// spec: agentic-governance / Governance validation settles after its declared consequence`,
  `// spec: agentic-governance / Governance verdict correlation survives process replacement`, and
  `// spec: agentic-governance / Governance publications are durably at-least-once`.
- [ ] 8.2 Convert all three validation handlers from void to classified outcomes through their private owners.
  Publish allowed messages through declared JetStream outputs and wait for PubAck; preserve deliberate blocked
  non-forwarding and nonblocking audit. Use RequestID, execution identity, and proposal fingerprint only for proposal-
  verdict correlation. Add retained-verdict exact read; add no exact committed-output lookup for ordinary validated
  output.
- [ ] 8.3 GREEN: prove replacement before proposal, after proposal, after verdict ACK, and before tool publication.
  Prove a verdict remains recoverable after waiter loss and ordinary publications remain at-least-once. If retained
  verdict plus response redelivery is insufficient, stop at the named failpoint; do not add a bucket.

## 9. AGENT admission, first-party publisher, and loop authority

- [x] 9.1 Preserve the owner-selected strong observed `DiscardNew` and affected-closure contracts.
- [ ] 9.2 RED: add model/dispatch/governance/loop tests for caller-local requirements, divergent configs, resolved
  overrides, under-admission, unavailable StreamInfo, queued USER, non-agentic zero lookup, and zero dependent
  allocation/positive settlement. Cite exactly
  `// spec: agentic-loop / Restart-safe replay observes and admits local stream bounds`.
- [ ] 9.3 Implement one pure repo-internal `internal/agentstreamadmission.ObserveAndValidate`. Each affected owner
  invokes it after its own resolved PortFacts and before its own dependent allocation. Requirements use only local
  AckWait, BackOff, MaxDeliver, maximum work/replay need, and producer PubAck dependency; no cross-component config,
  shared maxima, factory/raw-JSON switch, state, watcher, mutation, or exported API. Refuse DiscardOld, insufficient
  MaxAge, or earlier message bounds with typed `agent_stream_replay_inadmissible` and exact observed/required fields.
- [ ] 9.4 GREEN: prove each affected closure refuses independently, non-agentic components perform zero lookup,
  dispatch leaves queued USER unconsumed, and full DiscardNew backpressure retains source work without core-NATS
  fallback under the citation in 9.2.
- [ ] 9.5 RED: add all-six-configuration and four-static-producer real-NATS tests, both `agent.task`/`agent_task`
  names, declaration-only future rule, uncovered dynamic subject, missing publisher, registered non-Graphable task,
  and malformed/unregistered envelope tests. Cite exactly
  `// spec: rule-agent-publishing / First-party publish-agent output is admitted before action execution`,
  `// spec: rule-agent-publishing / Publish-agent classification uses canonical wildcard coverage and durable publication`,
  and `// spec: rule-agent-publishing / Publish-agent preserves the registered payload boundary`.
- [ ] 9.6 Implement rule-processor caller-local admission through the same internal validator before evaluator start.
  Resolve only its own PortFacts, preserve configured names including `agent_task`, and call canonical
  `component/flowgraph.SubjectCovers(declaredFilter, concreteSubject)` in that exact direction from the existing
  `actionPublisher`; do not duplicate the matcher. Covered task subjects use `PublishToStream`/PubAck; uncovered/refused
  output fails before post-send side effects. Require registered `TaskMessage` Payload, never Graphable; add no gate,
  classifier API, or #1158-wide publisher census.
- [ ] 9.7 GREEN: prove six classifier surfaces cannot select core NATS for covered task subjects, four static producer
  configs durably feed row 15, declaration-only configs cannot regress, and non-agentic rule processors pay zero
  lookup under the citations in 9.5.
- [ ] 9.8 RED: add fake and real-NATS loop-bucket tests for absent, matching retained, same/foreign-config race,
  History/TTL/MaxBytes drift, status failure, non-not-found lookup with zero create, and create-exists with exactly one
  race-get. Cite exactly `// spec: agentic-loop / Loop-state authority is acquired and observed before loop work`.
- [ ] 9.9 Implement internal `loopbucket.AcquireOwner`: KeyValue first; create only for typed
  `jetstream.ErrBucketNotFound`; on typed `jetstream.ErrBucketExists`, one KeyValue retry; then observe exact History
  10, TTL 24h, and non-binding MaxBytes. Publish no handle or dependent consumer/sweeper before success and never
  reconcile retained drift. Validate approval lifetime here, not in AGENT stream admission.
- [ ] 9.10 GREEN: prove every fresh-boot/race/drift/error case and zero forbidden mutation under the citation in 9.8.
- [ ] 9.11 Measure actual USER and TOOL source-stream retention for physical rows 1, 11, and 17. Prove observed bounds
  sufficient before the complete 15-subscription claim or stop for inventory/design amendment; AGENT admission is not proof
  for another stream.

## 10. Context and lifecycle closure

- [ ] 10.1 RED: add active-callback Stop races for dispatch, governance, model, loop, and tools, plus trajectory-batch
  cancellation tests. Cite exactly the applicable requirement:
  `// spec: agentic-dispatch / Dispatch shutdown closes every owner without retaining context`,
  `// spec: agentic-governance / Governance shutdown closes every delivery owner`,
  `// spec: agentic-model / Model shutdown closes its delivery owner`,
  `// spec: agentic-loop / Loop shutdown closes every delivery owner`,
  `// spec: agentic-loop / Delivery work joins before settlement`, or
  `// spec: agentic-tools / Tool delivery retains the permanent typed owner contract`.
- [ ] 10.2 Pass the exact delivery context to every blocking NATS, KV, Store, provider, and filter operation. Remove
  return-before-join behavior. Every owner stops admission, drains exact retained handles, awaits exact `Closed`, then
  cancels and joins observers/work; no production struct retains context.
- [ ] 10.3 GREEN: prove callback cancellation joins before settlement and each Stop returns only after no later ACK,
  publication, authority mutation, or goroutine activity. A cancellation-ignoring dependency remains a lifecycle
  blocker rather than heartbeat evidence.

## 11. Complete proof, documentation, and landing

- [ ] 11.1 Run the #1146-owned tranche of #1155's real-NATS process-replacement matrix across every #1146 durable
  boundary and all eight non-heartbeat production callbacks. Property/fuzz tests cite exact active requirement headings;
  unknown decisions cannot ACK. Leave #1155 open until #1249 supplies AgentRun complete/failed proof and the later
  combined gate passes.
- [ ] 11.2 Run focused race and integration tests, lint, build, schema generation, contract tests, and serialized
  `task e2e:agentic`. The AGENT DiscardNew cutover and first-party publisher path require covering E2E green.
- [ ] 11.3 Correct false restart claims in concepts 03, 17, and 27; link concept 33 without duplicating its message-
  pump explanation. Document provider ambiguity, continuation/Store, AGENT admission and DiscardNew backpressure,
  loop authority, boot order, metrics, raw external-executor migration, heartbeat defaults, and rule-agent publisher
  admission. Document the user-facing control contract: cancel, durable ApprovalResponse wait, retry/restart from the
  last durable boundary, lifecycle quiesce, no arbitrary pause/resume, and future suspension only as a new contract.
  Reconcile schemas and every example/fixture. Remove paused vocabulary from `agentic/README.md`,
  `processor/agentic-loop/README.md`, `docs/concepts/13-agentic-systems.md`,
  `docs/operations/migration-beta162-to-beta163.md`, and generated `specs/openapi.v3.yaml`.
- [ ] 11.4 Confirm PR #1159 carries the complete #1146 claim set, `implemented-by: Sol`, `Closes #1146`,
  `Refs #759`, `Refs #1155`, `Refs #1249`, and explicit “#1146-owned tranche; #1155 remains open” wording. It SHALL
  NOT carry `Closes #1155`. Preserve PR #1251 as #1239's retained authorship/review record and PR #1156 as final
  default-branch closing authority after #1249 and the combined proof gate.
- [ ] 11.5 Complete implementation/proof, then obtain SemStreams implementation review of the complete claim set.
- [ ] 11.6 Obtain owner-requested cross-agent review, apply every finding, and repeat both reviews until accepted.
- [ ] 11.7 Before archive, reconcile every exact MODIFIED current requirement, then archive
  `agentic-loop-restart-safety` as the final content commit and sync all seven current specs: `agentic-dispatch`,
  `agentic-governance`, `agentic-loop`, `agentic-model`, `agentic-tools`, `graph-view-subscription`, and
  `rule-agent-publishing`. Preserve every unaffected scenario and valid citation from each replaced current
  requirement.
- [ ] 11.8 Obtain narrow archive/current-spec-sync review. Only afterward run hosted CI, remote-base verification,
  undraft, and non-default merge. The staged merge creates checkpoint `A` for #1249.

## Transferred: AgentRun

Tasks H.1/H.2 remain removed. #1249 owns post-#1146 inventory, design, complete/failed migration, and replacement
proof against exact staged checkpoint `A`. This transfer narrows no other #1146 subscription.
