# Tasks: agentic-loop restart-safe settlement

Every behavior test added by this change SHALL carry a source comment in the exact form
`// spec: <capability> / <Requirement heading>`. The capability and heading SHALL match an active delta exactly;
design-section shorthand is not a valid citation. Every implementation slice follows RED → implementation → GREEN.

## Current execution checkpoint — sequential chat and restart (owner accepted 2026-09-08)

The owner accepted the product-level reset: preserve settlement and retained-result reuse work, and make a
restart-surviving two-turn conversation the next executable checkpoint. One bounded loop execution may complete
each turn without ending its conversation. Sequential chat does not require mid-flight steering or completion of
#1244's broader transition review.

The different-TaskID continuation-removal recommendation in
`design-task4-continuation-response-proof-2026-09-08.md` is **not owner-approved** and SHALL NOT be implemented.
The owner subsequently approved the reviewed PriorMessages field on three existing inputs and AutoContinue=false.
No other public API, conversation store, or recovery runtime is authorized. Existing unfinished settlement
obligations below remain open.

- [x] C.1 Establish the smallest supported task/dispatch history handoff from the existing surface inventory;
  reconcile its reviewed design and capability delta before implementation.
- [x] C.1a RED → implementation → GREEN for the approved fields, plain-text history validation, HTTP/channel
  normalization, retained-source history comparison, loop/context reconstruction, AutoContinue=false, and explicit
  command targeting. Preserve explicit attachment without supplied history. Verify registered-envelope round trips
  and fuzz the production input boundary. Independently review code, schema, and migration documentation.
- [x] C.2 Through real NATS and a deterministic fake model, submit a first turn and observe its answer; replace the
  relevant components and submit a follow-up through the supported entry point. Assert that the second model request
  contains the prior exchange, not merely that two answers were produced. Do not seed private process caches.
- [x] C.3 At the provider boundary, prove that replacement after matching result persistence but before source ACK
  reuses that result, while replacement before result persistence permits another provider call.
- [ ] C.4 Prove that required persistence or publication failure never ACKs its source. Retain separate tool-effect
  and approval proofs; the chat checkpoint does not imply that those unfinished paths are complete.

Verification update, 2026-09-08: C.2 now passes through started dispatch, loop, and model components with real NATS
and a deterministic HTTP provider. After stop/join and replacement, the follow-up uses a distinct LoopID, fresh
iteration budget, and the ordered displayed exchange exactly once. The answer requires facts absent from the current
prompt. The earlier implementation produced `lantern=unknown; token=unknown` (RED); history assembly produces
`lantern=amber; token=cobalt` (GREEN). A separate supported terminal-stream/HTTP proof confirms Decision.Reason, rather
than raw Result, supplies the displayed assistant text.

C.3 now uses an actual first provider response and interrupted source ACK, not a preloaded response fixture. The
test observes retained output and pending source, stops/joins the first component, and observes real redelivery to a
fresh component with provider calls still 1. Disabling retained-response reuse fails with calls 2; restoration passed
three consecutive race-enabled runs. Existing absence and failed-publication replacement proofs remain green.

The root reran the final lint-clean implementation with full affected-package unit/race suites (agentic, dispatch,
loop, model), plus serialized real-NATS/race acceptance and model/loop recovery cases: all passed. Production-input
fuzzing passed 15 seeds and 1,324 executions in a five-second, single-worker race-enabled run. Schema/OpenAPI generation
completed; the generator's optional meta-schema check was skipped because that file is absent. Strict OpenSpec
validation passes. These are component-replacement proofs, not an OS-process kill or graph/evidence E2E claim.

The new acceptance tests are reproducible independently:

```sh
go test -race -tags=integration ./processor/agentic-dispatch \
  -run '^TestIntegrationSequentialChat' -count=1
go test -race -tags=integration ./processor/agentic-model \
  -run '^TestIntegrationPostResponsePubAckReplacementReusesLiveProviderResult$' -count=1
```

Independent implementation review returned APPROVE for this field/default slice and its acceptance tests, with no
blocking/high findings. The reviewer independently reran focused unit/race and grammar seeds, both real-NATS chat
cases, and the live post-PubAck replacement case; all passed. The conformance-table correction is resolved. Root
repository lint and contract tests with `-race` also pass. C.4 and the older lane-specific obligations stay open;
this is not whole-task-4 or whole-PR approval.

The draft-push gate exposed a test-isolation defect: terminal-signal tests assumed the process-wide metrics
singleton started at zero. Repeating the response test with `-race -count=2` reproduced it. The three adjacent
tests now assert exact changes from captured baselines, without resetting globals or changing production code.
Independent narrow review returned APPROVE and reran all three with `-race -count=5`; premature and duplicate
completion mutations still fail.

The bounded C.4 terminal-marker proof now passes through real task intake and the actual heartbeat owner: terminal
PubAck and COMPLETE_ commit, the final bare LoopEntity write fails, the input receives delayed Retry without ACK,
and the nonterminal KV revision remains unchanged. After Stop/join, a fresh component receives the same stream
sequence and bytes, recovers the retained request, writes the final marker, and only then ACKs. Production is unchanged.
The current-source canonical integration suite passes this test with `-race`. The final isolated canonical run also
passes (test 30.47s, package 32.516s). An overlay-only mutation that ignores the final Put error fails the required
Retry assertion (test 0.37s, package 1.346s). Fresh logs are retained in
`/private/tmp/gh1146-task5-20260909.UBR3lx/c4-green.log` and `c4-mutant-red.log`; the original standalone logs were not
saved and are not evidence for this checkpoint. No worktree source was mutated. This is a component-replacement
proof, not an OS-process kill. Independent review returned APPROVE for this terminal-marker proof only; the C.4
checkbox still includes separate tool/approval obligations.

```sh
go test -race -tags=integration ./processor/agentic-loop \
  -run '^TestIntegrationTerminalMarkerFailureRedeliversAfterComponentReplacement$' -count=1
```

The full integration gate also exposed an incomplete simulated ToolResult in `TestIntegration_LoopWithToolCalls`:
it omitted the dispatched tool name and correctly hit the correlation refusal. The fixture now echoes `call.Name`,
as the production tool component does; the production guard is unchanged. The focused case reproduced RED and
passed three race-enabled integration runs after correction.

Draft checkpoint verification after both independently approved fixture corrections: the full canonical
`scripts/run-integration-tests.sh` passed, including all packages with `-race -tags=integration -count=1`.
`go test -race ./...`, `task lint`, `go mod tidy -diff`, Linux/amd64 build, schema/OpenAPI no-drift, contract tests,
entity-ID corpus audit, fixed-port/inventory guard fixtures, API-compatibility guard fixtures, and strict OpenSpec
validation pass. The reporting-only API check reports 14 incompatible Tier 1 packages against beta.162; it is not a
compatibility approval. The agentic E2E gate in 3A.3 is now green as recorded below; C.4 and remaining lane work stay open.

The sequential-chat inventory addendum passed independent review at SHA-256
`4e4b178db78bc8e41f836e7eae21d3ba461a55a5de5e94edf23425f7047c8fb8` (73/73 pins). The public-input candidate is
`design-sequential-chat-handoff-2026-09-08.md`, independent DESIGN REVIEW PASS at SHA-256
`b10470f4a67066f1dc27e3ff19688a3eea2b4352c83b5e1ed105c0d3789ec1cb`. The bounded judge separated the input contract
from AutoContinue's default choice. The owner accepted both on 2026-09-08; canonical design and both capability deltas
now carry that target. Implementation, C.2/C.3 acceptance, and scoped independent implementation review are complete.
Automatic hosted conversation recall is not claimed.

The bounded approval-ACK correction is implemented and independently approved: gate setup errors propagate;
UpdateLoop retains the newly stored triggering ToolResult; an error-path terminal-state label cannot authorize ACK
without a constructed terminal outcome. Six focused regressions cover setup, retained result, existing invalid
classification, durability failure, unsettled cancellation followed by a tool result, and failed timeout-output
construction. Existing successful timeout settlement remains green. Full loop/model/dispatch unit/race suites pass.
Repository lint also passes. This is a reviewed slice, not completion of task 4 or approval reconstruction in task 6.

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
  `afd93139bc520651c3432fc00df792cab12afc426fb9666439228d15d58be8d1`.
- [x] 0.10 Accept the independently reviewed dispatch edge-gateway checkpoint
  `design-dispatch-edge-gateway-2026-09-04.md`, current SHA-256
  `d26c0667692e5b5a6e3950f5b097966c17d2750b90aaeb8e54d2873a564275b5`. The owner-cited pre-final token-
  projection checkpoint `339cf2b2c734ef48a2898ce6b79c3783577a8b4ae152b65a1078b00445949b76` is provenance only and is superseded.
- [x] 0.11 Receive independent `DESIGN REVIEW PASS` of the synchronized edge-gateway target state before
  implementation. The review verified lane-scoped correlation, ordinary at-least-once publication, exclusive
  dispatch edge ownership, seven capability deltas, corrected checkpoint provenance, exact MODIFIED headings, and
  graph-view lifecycle preservation.
- [x] 0.12 Accept producer LoopID inventory SHA-256
  `7b273f91996e71df860226c83d615691e6b08de0fa0153c7c5d4869a53a78c26`, independent `INVENTORY PASS`, 69/69 pins;
  accept design SHA-256 `70ca0bb503465c76dce06a08f0be21a88870f184a1d0ebdfc77d88ff14c8818f`, independent
  `DESIGN REVIEW PASS`; and record owner acceptance in #1146 comment `5575482141`.

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

- [x] 2.1 RED: prove stable TaskID, random LoopID minting for new work, retained-`TaskMessage` LoopID recovery on
  redelivery, and conflicting TaskID-to-LoopID quarantine. Cite exactly
  `// spec: agentic-dispatch / Dispatch task redelivery recovers the committed LoopID`.
- [x] 2.2 Implement the TaskID-to-retained-`TaskMessage` recovery path. Mint LoopID randomly only when exact retained
  task evidence proves this is new work; reuse the retained LoopID on redelivery. Add no route-claim state or
  deterministic LoopID derivation.
- [x] 2.3 RED: prove RequestID distinguishes logical provider work and framework execution identity distinguishes tool
  work across same-CallID/different-RequestID cases. Cite exactly
  `// spec: agentic-model / Model request settlement is bound to a durable response`,
  `// spec: agentic-loop / Tool execution has stable framework correlation`, and
  `// spec: agentic-tools / Completed tool outcome identity is globally unambiguous`.
- [x] 2.4 Implement RequestID and execution identity only on provider/tool/governance-correlation paths that need
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
- [x] 2.6 Remove general exact committed-output lookup and canonical-output fingerprint work. Retain exact reads only
  for retained `TaskMessage` recovery, matching retained provider-response reuse, approval reconstruction,
  governance verdict recovery,
  explicit LoopID/terminal routing, durable applied-state proof, and immutable completed tool outcomes at the
  executor-effect boundary.

## 3. Provider settlement

- [x] 3.1 RED: add first-delivery, matching-retained-response, typed-absence redelivery, retained-read failure,
  pre-call replacement, post-return/pre-PubAck replacement, at-least-once response publication, unavailable metadata,
  and correlation-conflict tests. Cite exactly
  `// spec: agentic-model / Model request settlement is bound to a durable response`,
  `// spec: agentic-model / Model response publication is durably at-least-once`, and
  `// spec: agentic-model / Started markers do not claim invocation certainty` as applicable.
- [x] 3.2 Implement the operation-specific exact retained-response read before provider invocation. Reuse a validated
  matching response with zero provider calls; quarantine conflicting correlation; treat lookup failure as Retry; and
  invoke the provider with the same stable RequestID on typed absence, including redelivery after ambiguous process
  replacement. Publish every required success or provider-error response and wait for PubAck before source ACK. Add
  no provider ambiguity config, commit-unknown failure kind, provider reconciliation seam or endpoint census,
  pre-call started marker, ledger, outbox, supervisor, or provider dependency on AGENT replay admission.
- [x] 3.3 GREEN: prove by counter that a matching retained response invokes the provider zero times and typed absence
  invokes once for each delivered attempt. Prove retained-read failure invokes zero times, conflict quarantines,
  every newly produced response receives PubAck before source ACK, and post-return/pre-PubAck replacement may invoke
  the provider again when no matching response committed.

## 3A. Task producer identity prerequisite

- [x] 3A.1 RED: prove TaskMessage validation returns an ordinary error naming missing LoopID; dispatch and rule produce
  canonical v4 LoopID before marshal; and rule, direct HandleTask, and durable intake classify invalid before side
  effects. Cite exactly
  `// spec: entity-id-contract / A loop instance token is minted at its framework birth seam`,
  `// spec: agentic-loop / Loop task, request, and tool work use only required correlation`, and
  `// spec: rule-agent-publishing / Publish-agent preserves the registered payload boundary`.
- [x] 3A.2 Implement producer-owned task identity. Keep dispatch's retained-byte path; mint rule LoopID once per
  TaskMessage construction before Validate/marshal; require LoopID in TaskMessage validation and schema; remove
  TaskMessage-path fallback minting from preflight and HandleTask; and validate/classify the direct HandleTask seam.
  Add no exported helper, constructor, configurable generator, deterministic derivation, scan, map, ledger, bucket, or
  second owner. Do not alter tasks 9.5–9.7 admission, subject coverage, classifier, registry, or PubAck contracts.
- [x] 3A.3 GREEN: through real NATS, redeliver the exact rule-produced registered bytes across agentic-loop process
  replacement and prove one TaskID-to-LoopID mapping, one loop identity, and no fallback mint. Independently prove
  loud missing-ID refusal through the registered typed-delivery boundary and direct handler.
  The proof covers retry/redelivery of the same already-marshaled task, not fresh upstream rule-action execution. Run
  affected unit/race tests and serialized `task e2e:agentic`; update generated schema, examples, ADR-105, and the
  beta.162-to-beta.163 migration note before task 4 review resumes.

  Prior blocked attempts, 2026-09-08: the real-NATS proof, affected unit/race tests, full test/race suites, schema generation, lint,
  contract tests, and strict OpenSpec validation pass. Three earlier serialized `task e2e:agentic` attempts stopped
  on Docker Hub metadata lookup. The retry on pushed checkpoint `14ae0437` built and started successfully, then failed
  `verify-durable-tool-replay` after six assertions: its synthetic ToolCall lacked required execution correlation and
  was rejected before publication. The local fixture correction now captures a fresh actual loop-produced call and
  verifies its ExecutionID-based result/MsgID plus full correlation. Paired unit/race tests, independent narrow
  APPROVE, and root lint pass; exactly-one executor invocation remains required. This is fixture-only approval,
  not task-4 review.

  The first corrected run reached nine assertions, then failed the Stage-A completed-replay barrier because its
  synthetic calls also lacked execution correlation. The 2026-09-09 fixture-only correction now obtains all three
  barrier calls from fresh loop tasks, uses ExecutionID for outcome/result reads, supplies canonical terminal UUIDs,
  and checks source settlement while permitting ordinary repeated dispatch publication. It checks the latest retained
  response's full correlation; it does not claim to inspect every duplicate. Exactly one completed executor effect
  and zero replacement executor invocations remain required. Production, mock provider, and barrier harness are unchanged.

  The resulting serialized `task e2e:agentic` run passed `verify-durable-tool-replay` and
  `verify-stage-a-process-replacement`, then failed `walk-approval-path` after ten completed assertion stages:
  no approval-pending event appeared within 30 seconds. Total scenario duration was 2m41.033s. Test containers and the
  disposable NATS volume were removed. The fixture package passes `go test -race ./test/e2e/scenarios/agentic -count=1`;
  root `task lint` and strict OpenSpec validation also pass. That attempt did not establish full-tier GREEN.

  Independent review returned APPROVE for the four-file fixture checkpoint only; five focused race-enabled checks
  passed independently. The reviewer's full focused rerun could not bind its HTTP test listener under the sandbox,
  and its escalation timed out. No whole-PR or C.4 approval is implied.

  Bounded read-only attribution at checkpoint `14ae0437` found an unfinished task-5 path: `validateColdToolResult`
  returned Retry for matching retained work because ordered batch reconstruction/applied proof was deferred.
  The tool-result consumer permits only one
  unacknowledged delivery (`processor/agentic-loop/component.go:1117`), so that cold result can hold later approval
  results behind it. During this run, the consumer had one pending acknowledgement and two queued results after
  replacement; the later approval-required executor refusal was observable. This fits the mechanism, but exact
  pending sequence-to-payload attribution was not retained and is not claimed. No further production correction or
  rerun was attempted under that fixture-only scope. Full-tier GREEN, push, and C.4 review were held at that point.

  The rule-byte integration proof is now strengthened: normal Start, interrupted first source ACK, bounded Stop/join,
  and a started replacement receive the same server stream/consumer/source sequence and bytes with increased delivery
  count, one durable TaskID/LoopID mapping, and final server ACK-floor advancement. Its race-enabled binary passes
  in 30.55s; forwarding the first ACK makes the pending-acknowledgement assertion fail. Independent proof review
  returned APPROVE at source SHA-256 `ac6fbc8b062e726c40093829e8e45a3dae575a9ccb5069b5883df25175bebead`.
  The separate refusal cases are `TestMissingLoopIDIsTerminatedAtIntakeBeforeState`
  (`processor/agentic-loop/loop_token_intake_test.go:97`, registered malformed envelope, typed owner and refusal
  metric) and `TestDirectHandleTaskRefusesMissingLoopIDBeforeState`
  (`processor/agentic-loop/delivery_owner_test.go:406`, direct handler). Those are not live-NATS delivery proofs.
  The live case is a started-component replacement proof, not an OS-process kill. After the task-5 correction below, a new
  full-tier run reached nine assertion stages, then failed the Stage-A dispatch quarantine metric wait in 1m49.973s.
  Retained JSZ shows the injected terminal at stream sequence 87 still queued while dispatch delivery and ACK floor
  remained at sequence 84; USER held the intended 5/5-message fault. The ten-second metric observation expired before
  this input reached dispatch. The fixture now captures its native PubAck sequence and waits for dispatch delivery
  before the unchanged publication/quarantine checks. Its intended no-op RED and focused race GREEN passed review;
  all original outcome assertions remain. The underlying NATS pause/resume delay is not claimed fixed.
  Two earlier starts ran no scenario because
  Docker credential lookup hung; an isolated anonymous client directory resolved that without changing saved settings.
  Final standard `task e2e:agentic` passed all 15 assertion stages in 2m25.790882542s, including Stage-A process
  replacement, the approval walk, cancellation, and terminal response. The disposable stack and volume were removed.
  The final fixture package also passed with `-race -tags=integration` (1.436s). This resolves the 3A.3 gate and
  permits C.4/task-4 review to resume; fixture review is not task-4 approval.
  Hosted E2E Ladder run `34244917179` separately passed slow-consumer attribution
  and failed statistical HTTP search with `community index is not ready`. Run `34362908293` passed both jobs,
  but the relevant statistical/query code is unchanged; this is not fix evidence or a flake waiver.
  Read-only attribution ties the same stage/error to [#609](https://github.com/C360Studio/semstreams/issues/609).
  The gateway's one-shot `globalSearch` precedes the later community wait; the earlier search-quality stage uses
  `semanticSearch` and proves no community-cache readiness. A valid empty cache generation is distinct from no
  valid generation. The failed run retained no generation diagnostics, so initial replay delay versus later watcher
  loss remains unproven. Keep that reproduced readiness remainder under #609; do not add generic clustering
  readiness state or treat moving the raw-KV community wait as proof of query-cache readiness.

  Fixture conformance (paths below are under `test/e2e/scenarios/agentic/`):

  | Approved constraint | Implementation/evidence |
  | --- | --- |
  | Loop owns execution correlation; fixtures do not derive it | `stage_a_process_replacement.go:525`, `scenario.go:422`, `stage_a_fixture_test.go:22` |
  | Outcome/result lookup uses ExecutionID with full correlation | `stage_a_process_replacement.go:121`, `stage_a_process_replacement.go:599`, `scenario.go:498`, `stage_a_fixture_test.go:46` |
  | Completed outcome reuse does not repeat the executor effect | `stage_a_process_replacement.go:150`, `stage_a_process_replacement.go:158` |
  | Terminal fixtures use canonical loop tokens | `stage_a_process_replacement.go:494` |
  | Observe exact source delivery before fault consequences | `stage_a_process_replacement.go:310`, `stage_a_process_replacement.go:354`, `stage_a_process_replacement.go:764`, `stage_a_fixture_test.go:103` |
  | Ordinary publication may repeat; source work must settle | `stage_a_process_replacement.go:429`, `stage_a_process_replacement.go:442`, `stage_a_process_replacement.go:790`, `stage_a_fixture_test.go:139`, `stage_a_fixture_test.go:181` |

## 4. Loop task and response settlement

- [ ] 4.1 RED: add task-birth, post-registration failure, dropped initial-request publication, response cold-read,
  proven duplicate, conflict, missing retained evidence, and every KV/Store/publication failure test. Cite exactly
  `// spec: agentic-loop / All six loop input classes settle after owner-specific durable done`,
  `// spec: agentic-loop / Loop recovery is lane-specific and read-through`, and
  `// spec: agentic-loop / Loop task, request, and tool work use only required correlation`.
- [ ] 4.2 Migrate task, response, and tool-result bindings from the legacy helper to the permanent typed heartbeat
  owner. Add direct LoopEntity read-through by LoopID; reconstruct response configuration from committed request;
  settle loop/graph birth, lineage, initial request, created event, and terminal failure at the delivery boundary.
  Preserve every exit for #1244 as a declared transition or refusal and never encode log-and-ACK as success. Persist
  bare terminal LoopEntity last as the lane-applied marker; discard speculative process-local terminal state on
  pre-marker failure and add no replay cache, ledger, or second state owner.
- [ ] 4.3 GREEN: prove matching retained provider response prevents another call and retained absence remains durably
  at-least-once, no response/tool-result log-and-ACK, no stale-correlation loss, and no replay beyond admitted evidence
  across real process replacement. Prove the brief COMPLETE/event-before-terminal-LoopEntity window remains
  unsettled, exact current RequestID plus the final marker proves model-response application, and terminal state alone
  never proves a particular tool result.

## 5. Tool result and completed outcome

Current bounded slice, owner accepted 2026-09-09: implement the existing cold tool-result recovery path and prove
it through the started durable consumer, then rerun the full agentic tier. Reuse current LoopEntity and retained
request/response/result evidence; add no runtime, store, ledger, dispatcher role, configuration, or public surface.
The regression must observe actual source redelivery after Stop/join/replacement and subsequent approval-required
work reaching its durable pending state and event. Task 6's separate approval-continuation replacement/mechanism
ruling is not included. The mixed task-5 checkboxes remain open until their complete wording is proved.

The ordinary cold-result unit regression reproduced the explicit deferred-recovery Retry, then passed after the
initial correction. The first started-owner attempt instead deadlocked its own pre-handler test gate; its captured
stack is fixture evidence, not a production recovery RED. The corrected fixture cancels its caller-owned Start
context after the real result checkpoint commits, then uses bounded Stop/join and actual server redelivery.
`TestIntegrationColdToolResultRedeliveryUnblocksLaterApproval` passed with race instrumentation in 30.44s; disabling
only cold recovery in a separately compiled mutant failed its redelivered-source ACK assertion in 30.36s. Those
integration binaries precede the two subsequent review corrections and are not current-source gate evidence.
Review reproduced double iteration charging when task recovery restores the loop first, including false budget
exhaustion, and loss of an earlier assistant/tool exchange from emitted context. Both regressions passed after
correction in the existing restoration owner (focused race run 1.641s; full loop package race run 3.004s).
Scoped re-review returned APPROVE for the corrected seven-file manifest. Current-source lint, integration/live-tag
vet, full unit/race, Linux build, module tidy-diff, schema no-drift, and the canonical full integration suite pass;
the integration loop package took 128.572s and includes the corrected started-owner regression. Full agentic E2E
passes all 15 stages as recorded in 3A.3. That ordinary-batch checkpoint proves application through a later committed
request. The direct-terminal slice below adds StopLoop/max-iteration proof; already-awaiting-approval cold-result
recovery remains Retry. It SHALL NOT be replaced by a terminal-state-only ACK; task 6's continuation gate is separate.

Bounded implementation conformance (paths below are under `processor/agentic-loop/`):

| Accepted constraint | Implementation and proof |
|---|---|
| Exact read-through, same state owner, no new authority | `settlement_recovery.go:408` uses existing exact readers; `state.go:465` extends LoopManager restoration; `component.go:1949` rejoins the normal handler. |
| Ordered batch, stable execution identity, preserved conversation | `tool_result_recovery_test.go:17` covers completed prefixes and repeated provider IDs; `settlement_recovery.go:483` uses the stamped batch and ordinal-specific applied result. |
| Persist results and publish the next request before ACK | `handlers.go:2590` retains batch evidence; `state.go:1123` owns extraction; `tool_result_redelivery_integration_test.go:66` observes KV and required output before native ACK. |
| Publication retry spends an iteration once | `state.go:478` reconciles the persisted ordinary batch, including already-restored loop memory at `state.go:385`; `tool_result_recovery_test.go:128` checks the budget boundary and `tool_result_redelivery_integration_test.go:221` checks persisted iteration after replacement. |
| Preserve earlier tool exchanges in emitted model context | `state.go:364` keeps non-system conversation messages together; `tool_result_recovery_test.go:157` checks the registered next request's complete prior and current exchange order. |
| Missing evidence retries; conflicts quarantine; no bare-terminal shortcut | `tool_result_recovery_test.go:86` tests absent/conflicting evidence without installed state; `settlement_recovery.go:512` requires terminal proof and keeps unresolved approval recovery unsettled. |

The final seven-file source manifest, commands, and RED/GREEN logs are retained locally in
`/private/tmp/gh1146-task5-20260909.UBR3lx/task5-conformance.md`. This evidence covers started-component replacement,
not an OS-process kill; the full agentic tier separately exercises process replacement. No new production export,
field, configuration, store, ledger, runtime, or dispatcher responsibility was introduced.

The next bounded slice recognizes exact direct-terminal results from the same retained records. StopLoop requires
the matching successful result; iteration-limit failure requires the full ordinary batch and the existing persisted
iteration-limit error. Review reproduced a timeout-at-cap false ACK before the latter check was added: timeout can
retain the same batch and iteration count, but is not the declared iteration-limit consequence. The handler and
recovery check now share the handler's existing error formatting; no new durable reason or marker was added.

Both direct-terminal replay regressions first failed with missing applied proof (0.805s). The separate timeout
regression reproduced the false ACK (0.861s). Corrected loop-package race tests pass (3.202s), as does lint.
`TestIntegrationTerminalToolResultAppliedAfterReplacement` passes both cases through real started consumers
(package 62.795s). It seeds a retained request/response/batch checkpoint, withholds the first ACK after the actual
terminal record commits, replaces the component, and observes the same native source sequence/bytes with increased
delivery count. Final ACK-floor advancement occurs without another terminal publication or marker revision.
This proves started-component replacement from a seeded checkpoint, not task birth, executor effects, or OS-process
kill. The earlier host-lock refusal ran no scenario and is not test evidence. Scoped source/proof review returned
APPROVE, including the timeout correction. Full repository unit/race, lint, both tagged vet gates, build, contract
tests, module tidy-diff, schema no-drift, and the staged entity-ID corpus audit pass.
The original canonical full integration run passed the loop package (207.212s), but failed
`TestIntegration_RuleStopAfterAcceptedStartParentCancellation`: `processor/rule/processor.go:1507` clears
`runtimeWG` while the started sweeper's defer reads it at line 1022. At that checkpoint, the affected rule files
were unchanged by this slice or the #1146 diff from its frozen parent; the same code was present on main.
The retained failure trace is `/private/tmp/gh1146-terminal-tool-20260909.Djv9m8/integration-all.log`.

The owner approved the separate correction, claimed by #1273 / draft PR #1274. Its reviewed commit `382f7887`
is integrated here as `b62c66d6`, with all three rule-file hashes unchanged and the frozen parent still exact
`417beae5552f8f15ad3540edd7d8504c87174c13`. Start registration stays counted in the existing WaitGroup;
workers retain their exact admitted group/completion/wake handles instead of rereading cleared fields.
Deterministic regressions reproduced premature completion, a delayed readiness-worker panic, and the coordinator's
wake-channel race. The correction adds no public surface, state, timeout, join, or rejoin policy; controlled Stop,
deadline-bounded abort, and failed-Start rollback keep their existing contracts.

The separate correction's full local gates and independent review passed. The combined tree now passes
`task check:push`, including full unit/race and canonical integration (loop 191.552s; rule 67.846s). Linux/amd64
build, module tidy-diff, schema/OpenAPI no-drift, contract tests, tagged vet, identity audit, guard fixtures, and
strict OpenSpec validation also pass. Narrow integration review returned APPROVE with unchanged source hashes;
its independent focused rule race run passed in 1.385s. Evidence is retained in
`/private/tmp/gh1146-rule-integration-20260909.BvGL25`.
This resolves the local rule-race verification hold, not the remaining mixed task-4/task-5/C.4 or task-6 obligations.
The 15-stage agentic E2E result remains the earlier `5be9b43f` checkpoint's evidence; it was not rerun for these
internal corrections. No retry-to-green waiver, parent advance, whole-PR approval, archive, or closure is claimed.

Direct-terminal conformance (paths below are under `processor/agentic-loop/`):

| Existing constraint | Implementation and proof |
| --- | --- |
| Exact retained execution plus final marker, never terminal state alone | `settlement_recovery.go:532`; `terminal_tool_recovery_test.go:16` drives the normal handler and persistence boundary. |
| Existing result owner and existing terminal consequence | `state.go:427` shares retained-batch validation; `handlers.go:2526` owns iteration-limit error formatting. |
| A timeout at the cap is not iteration-limit proof | `terminal_tool_recovery_test.go:114` observes Retry after production response recovery persists the timeout. |
| Native redelivery settles without repeating terminal outputs | `terminal_tool_redelivery_integration_test.go` verifies both direct terminal cases through the actual started delivery owner. |

The exact source manifest, commands, positive/negative proof, and seeded-checkpoint limits are retained in
`/private/tmp/gh1146-terminal-tool-20260909.Djv9m8/terminal-tool-conformance.md`.

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

The 2026-09-09 local first-approve integration proof reaches the intended RED on production checkpoint
`8c65dac0`: real task/model/tools owners persist pending approval, the source ToolResult and original dispatch
notifications settle, loop and dispatch stop/join, and fresh owners retain byte-identical pending state. HTTP
approval then returns 409 (`loop not awaiting approval`) because the endpoint still consults `LoopTracker`.
The exact source ACK floor is 6 for ToolResult sequence 6; pending KV revision is 3. The focused race-enabled
canonical run took 1.468s (test 0.46s), and independent review approved this first-approve test's fidelity.
This is a component-replacement RED, not an OS-process E2E pass, proof of missing durable evidence, or the
task 6.6 mechanism ruling. Graph/evidence E2E and the complete decision/error/correlation matrix remain open.
The narrow explicit-approval authority-read correction is part of 6.9/6.10; it does not complete tracker removal
or the shared-view work. Earlier trace-expectation failures were test-fixture failures, not restart findings.

The same unchanged production checkpoint then reproduced the 409 in standard `task e2e:agentic` with the
deterministic local mock and the new `walk-approval-after-restart` stage. The application was killed/restarted
while NATS stayed up; native source and dispatch-notification settlement, process replacement, and retained
pending state were checked before the HTTP request. The run failed at that request in 2m41.749s with
`assertions_run=12`; the tier now declares 16 assertion stages. This is OS-process RED evidence, not E2E success.
Normal test-stack teardown completed. The full fixture race suite and scoped vet passed, and independent review
approved the test's fidelity. Missing-report-witness fixture tests are not runtime fault-injection proofs.

The subsequent local explicit-approval endpoint correction uses the existing exact authority reader and typed
validation, preserves admission/second-party permission and publication, and removes tracker-based approval
selection/clearing. Independent review approved this endpoint-only slice; the full dispatch package passed with
`-race` in 2.256s, and the deliberate refusal-log/counter omission failed its tests as intended. The unchanged
real-NATS replacement test now passes HTTP 200 and observes the exact ApprovalResponse's native ACK, then fails
because the fresh loop ignores the approval with empty process state and never redispatches the tool (test 5.51s,
package 8.031s). This exposes the separate 6.3/6.5/7.6 cold-approval false-ACK obligation; it is not a recovery PASS
or evidence that retained continuation data is missing. That intermediate run preceded the loop correction below.

The bounded first-approve loop correction now restores the existing context/tool owner from current pending
state and the exact retained request/response. It validates the execution tuple, preserves the pending trace, and
preserves the live approval gate's discard of queued sibling tools. It adds no new state owner, durable field,
bucket, public surface, or recovery runtime. Missing process state alone no longer authorizes an approval ACK.
Review reproduced two fast-result ordering windows. The correction snapshots the resolved approval before
publication, waits for the existing required PubAck, then uses native KV Update against the revision from the
original authority read. A conflict retries without overwriting newer state or inventing branch-applied proof.
The final source and regressions received independent APPROVE; full loop race passed in 3.023s and vet passed.

The unchanged real-NATS replacement test now passes (test 0.47s, package 4.037s): settled original inputs,
fresh loop/dispatch owners, HTTP 200, exact approved execution, native ACKs, one executor effect, and durable
successful completion. Independent evidence review returned APPROVE. This fixture deliberately excludes
graph/trajectory-storage evidence; its logged unavailable audit storage is not success evidence.

Standard `task e2e:agentic` then passed all 16 assertion stages in 2m32.252s using the deterministic local mock.
The explicit `walk-approval-after-restart` stage passed in 6.506s after real application kill/restart retaining
NATS, with unchanged pending state, no premature execution, exact approved tool correlation, one executor effect,
and successful terminal completion. The tier also passed its existing graph/trajectory and replacement stages;
that does not imply every approval audit consequence has a separate assertion. Normal ephemeral-stack teardown
completed. Root repository unit/race and lint passed. This is the first approve-path GREEN, not the complete
decision/error/correlation matrix, confirmed-retention-absence behavior, applied/redelivery proof, sweeper work,
tracker/shared-view removal, or task 6.6's owner mechanism ruling. All mixed task checkboxes remain open.

The source manifest and native evidence are retained in
`/private/tmp/gh1146-approval-replacement-20260909.J0xSCQ/loop-approval-first-approve-handoff.md`.
The standard E2E log is `/private/tmp/gh1146-approval-restart-20260909.GmOOLg/e2e-process-current.log`, SHA-256
`125ad9a5baa38c41d7786c8256c5a3dab15cfe1c777e29a0eee6af292389c493`. Missing-report-witness negatives remain
fixture/validator tests, not omitted-restart runtime mutants. No Store removal or third mechanism is authorized.

The first full pre-push gate exposed an older graph-cancellation fixture that supplied only process-local
approval state. Initializing its existing KV and producing a valid pending checkpoint through the existing
handlers exposed a real terminal-error regression: the new branch split had changed terminal persistence
failure from Quarantine to Retry. The terminal branch now restores its original Quarantine and contextual error;
the new nonterminal snapshot/PubAck/conditional-Update path is unchanged. Neither graph cancellation/join nor
zero-settlement/exact-owner assertions were weakened. Both native fixture cases pass (0.28s and 0.27s,
package 2.505s); full loop race passes in 3.209s and integration-tagged vet passes. The original full-gate failure
and both focused REDs remain retained under `/private/tmp/gh1146-check-push.5RHqVN`.

Final corrected-source `task check:push` passes, including full race-enabled integration (loop 189.757s,
dispatch 76.129s). Standard deterministic-mock agentic E2E then passes all 16 asserting stages (18 total) in
2m32.172s; approval after process replacement passes in 6.474s. All Go source hashes remain unchanged across
these final runs, and schemas/OpenAPI have no drift. Linux/amd64 build, module tidy-diff, identity audit,
fixed-port/inventory/API guard fixtures, and strict OpenSpec validation pass. API comparison remains reporting-only
with 14 incompatible packages against beta.162, not compatibility approval. Final gate evidence is retained in
`/private/tmp/gh1146-final-gates.KNIdAO`; earlier REDs remain recorded, not waived or rerun away.
This verifies the bounded first-approve checkpoint; the mixed tasks and task 6.6 remain open.

The next test-only checkpoint extends the same started-owner replacement fixture to modify and reject. Modify
executes nonnil replacement arguments while retaining the original execution correlation. Reject executes no tool
and supplies the paired synthetic permission/reason result to the next model request, which completes normally.
The original approve proof remains intact. A fourth case produces an earlier ordinary tool exchange in the same
loop, retains both model responses with the same provider CallID but different RequestIDs and arguments, and proves
replacement follows only the current request's response. This completes task 6.4, not the full decision matrix.

All four cases pass together through the canonical race-enabled real-NATS runner (package 3.966s). Temporary
Go overlays that ignore modified arguments or lose the rejection reason each make the corresponding test fail;
these are falsification proofs, not newly discovered production regressions. Production code is unchanged.
Evidence: `/private/tmp/gh1146-approval-decisions.TANE3j/final-native-green.log`, SHA-256
`2e6342fe5d5a14a7194342ff05af5db42bfdd16cea0fedee9716e72aa9896517`.
At that checkpoint, timeout, approval/tool-result redelivery, the evidence-error matrix, confirmed-retention absence,
and task 6.6's explicit owner storage ruling remained open. These component-replacement proofs add no OS-process E2E claim.

The timeout replacement case first reproduces RED on unchanged production `52064acd`: the original 8s deadline
survives replacement configured for 1m, but no continuation appears within the allowed 15s observation window.
The original ToolResult is settled (stream sequence/ACK floor 6/6), both original owners stop before expiry, and
pending KV revision 3 remains byte-identical. No HTTP approval or manual sweep wakes the replacement; executor
calls remain zero. This is a missing-deadline-discovery proof, not missing storage or proof of three actual sweeps.
Independent review approves its test fidelity only. Native test 15.50s, package 16.531s; retained log
`/private/tmp/gh1146-approval-timeout.DSuKTN/timeout-native-red.log`, SHA-256
`e9623aec4e56e2190b2e49df8c87b749199219d68c6eb911462de096c26b904f`.
The sweeper's separate direct-apply and subsequent durable-response publication are not exercised by this RED.
No successful timeout settlement, full matrix, or task 6.6 ruling is implied by this checkpoint.

The subsequent timeout correction restores positive persisted deadlines before input admission using one initial
AGENT_LOOPS snapshot and exact current loop reads. The sweeper only publishes the existing ApprovalResponse;
the native approval consumer owns reconstruction, required effects, persistence, and settlement. Publication
failure retains pending state and emits the existing error log plus a dedicated counter in the existing metrics
owner. There is no new durable state, public configuration, supervisor, or continuing startup watcher.

Review exposed a timer-only restoration regression: response replay acknowledged after dropping retained system
and user history (RED 0.501s). The correction uses actual request-route membership before taking the warm path,
and requires that exact route before reusing a nil-batch restoration. Cold and fully correlated warm controls
remain intact. The state guard alone stayed RED; both existing-owner checks are required. Pre-lint focused race
tests pass in 1.551s, the loop unit/race suite in 3.029s, and tagged vet exits zero.

The pre-lint native approval run passes approve, modify, reject, same-CallID isolation, and timeout in 15.846s.
The timeout case takes 10.48s, preserves the original 8s deadline under replacement config 1m, executes no gated
tool, and proves exact rejection, durable completion, source sequence 8 with ACK=1/NAK=0/TERM=0, and drained
consumers. Its test observer is explicitly durable so inactivity cannot invalidate the final drain assertion.
Log: `/private/tmp/gh1146-approval-timeout.DSuKTN/timeout-final-native-matrix.log`, SHA-256
`239f0af1762d6561f22c45267f95cd7c870e3b656fa31aef6b51347c0a6db7b2`; exact source manifest is `final-source.sha256`
in that directory. The concept guide now explains timer recovery and decision publication versus application.
This is local bounded-slice evidence, not the full redelivery/error matrix, new E2E proof, or task 6.6's ruling.
Mixed task checkboxes remain open.

The pre-push lint gate exposed two function-length limits. The correction only extracts the existing startup
initializer-hook selection and shares an empty test snapshot; acquisition, deadline restoration, and admission
remain ordered in Start. Post-correction native approval cases pass in 15.900s (timeout 10.47s), with exact source
in `post-lint-source.sha256` and conformance handoff `timeout-final-handoff.md` in the same evidence directory.
The subsequent full `task check:push` passes: loop unit/race 4.682s, integration/race 201.061s; dispatch 4.682s and
75.498s. All 2230 Go file hashes remain unchanged across the gate, with no schema or module drift. Gate log:
`/private/tmp/gh1146-timeout-checkpoint.ZIukPh/check-push-corrected.log`, SHA-256
`b917ba9a29f60e55f6f650ab19e84f6bf3ecf5e43c0e5e4f2b8489cc6c186428`.
These local gates do not waive hosted holds, complete the full matrix, or authorize task 6.6's storage decision.

The next test-only checkpoint on base `615997c6` expands the six existing unit callback refusal cases across
approve, modify, reject, and timeout: all 24 combinations pass with race detection in 1.495s. A temporary Go
overlay disabling the argument-conflict guard makes all four affected decision cases fail as intended; production
source is unchanged. This proves branch-independent refusal, not confirmed-retention absence or the full matrix.

The new native applied-approval redelivery proof reaches its intended RED in 30.50s (package 31.488s): the real
approval completes, its input ACK is withheld, and fresh owners receive identical source bytes at sequence 8 with
delivery count increasing from 1 to 2. The replacement returns ACK=0/NAK=1/TERM=0 at the existing non-awaiting-state
guard. Final loop revision 7 and bytes remain unchanged, with one gated execution and two provider calls. Retained
request/result evidence is logged; this demonstrates an unimplemented applied-proof path, not that a new durable
fact or Store is necessary. Both test containers terminate normally. Evidence directory:
`/private/tmp/gh1146-approval-applied.k5BLPg`; native log `native-applied-red.log`, SHA-256
`f96e5f8a475ef7c1a48ac21030f12af8eb38312b5593644296ef271268ce056e`. The failing regression remains local and
unpushed; no production change, task completion, mechanism ruling, or frozen-parent advance is claimed.
Independent review approves the unit expansion and native RED fidelity, not runtime completion. The five existing
native replacement controls also pass on the changed fixture in 14.309s (timeout 10.51s), with unchanged source
hashes; log `native-existing-controls.log` in the same directory, SHA-256
`a68876bd959a2f558208908721892b9db6f884d735f0984aac2df0561fdbdf57`. The known applied-redelivery failure was
excluded from that control run, not fixed or rerun away.

The separate seeded callback counterexample `TestOldApprovalCannotResolveLaterRequestWithSameCallID` also reaches
its intended RED (race, package 0.563s). Decision A is serialized before installing an internally consistent B
checkpoint with the same LoopID/provider CallID but different RequestID, ExecutionID, and arguments. The production
callback returns Ack/nil, constructs B's ToolCall with A's reviewer, and clears B's pending state in the fake KV.
Both no-wrong-dispatch and unchanged-authority assertions fail. This proves input-to-execution misassociation at
the unit seam, not native ACK/PubAck, executor effects, two chronological approval gates, or timestamp authority.
Independent review approves that limited fidelity. Evidence: `/private/tmp/gh1146-approval-late-identity.zapI1e`,
log `late-approval-red.log`, SHA-256 `c345f4afa7393044bb3b55f985c35325fe5d63eda3dddd4ccc556ff4d0dd5e89`;
test source SHA-256 `b2522d6a4d8ff4e805ef6d265a20f0fa25510dc19fbf61b72e754215e89d66b2`.
This is distinct from the applied-state Retry gap: the current checkpoint identifies B, but the incoming decision
does not identify its intended execution. No new identity, disposition, persistence mechanism, or task 6.6 ruling
is selected by this test. Both failing regressions remain local and unpushed.

The bounded approval surface inventory is independently INVENTORY PASS at
`inventory-approval-applied-boundary-2026-09-10.md`, SHA-256
`6640c375572e2171790d7910de7663cf5928ea2b8aab99dd5c3d68ce50f197cb` (159/159 pins verified).
It preserves its earlier test-source checkpoint; the newer counterexample is recorded separately above.

Owner acceptance, 2026-09-10: independently reviewed
`design-approval-gate-identity-2026-09-10.md`, SHA-256
`92a19372c2fe2f19ac30264b3525654b5b68a005847f3435681dc0e991322fc4`, is accepted by
[comment 5618375806](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5618375806).
Preserve that design artifact and the accepted inventory unchanged. This accepts required ExecutionID echo,
observable effect-free noncurrent-gate ACK, one logical gate per execution, and the narrow API/spec amendments.
It does not mark implementation or acceptance tests complete, prove historical winning-decision provenance, or
revoke comment `5463183450` and task 6.6's Store fallback. Existing RED evidence remains a historical checkpoint.

Implementation checkpoint, 2026-09-10: required ExecutionID echo, dispatch admission, pending projection, loop
matching, and observable inapplicable decision settlement pass focused race tests. All six native approval cases
pass the canonical integration runner (package 46.339s), including exact approval-source redelivery after owner
replacement: source sequence 8, delivery 2, ACK 1 / NAK 0 / Term 0, executor calls 1, provider calls 2, unchanged
durable KV revision/bytes, and inapplicability log plus counter. This proves the accepted conditional outcome,
not historical winning-decision provenance. The native log SHA-256 is
`2e7276fa080f2d92789d45542701eb4b0cabd44a5300f55eaf6e9cc4428ddfd2`; the frozen 18-source manifest SHA-256 is
`e4067802ba0308e385b9333d5e6ee222ce3d0442f8170de5902b405fc0a59b3a`. Exact commands and per-ruling evidence are in
`/private/tmp/gh1146-approval-execution-echo.Rm4ZA5/checkpoint.md`, SHA-256
`3a0ef0bb343c810d05da2f2cc134e4dfabc065a06a2d908ad027d76febf7895a`.

The distinct `TestApprovalRequiredResultReplayCannotReopenClosedGate` remains RED: replaying the original gated
ToolResult after ordinary approval closure recreates the same execution's pending gate. No correction or new
mechanism is authorized by that observation. Its bounded inventory is
`inventory-approval-required-replay-2026-09-10.md`, SHA-256
`1f5afffddf5ead0e612127ad053291627887c8d63cd9ea2b25885a7235416a8a` (60/60 pins mechanically verified).
Scoped implementation review excludes that known blocker and requests HTTP/OpenAPI prose alignment. Remaining
fixture migration, schema proof, diagnostic mutation, full gates, and breaking-change E2E are not complete.
No task checkbox, task 6.6 ruling, or merge readiness follows from this checkpoint.

Bounded cleanup completed later on 2026-09-10: endpoint/resolver prose, required approval RuleFields, and existing
valid-fixture echoes are synchronized. Component schemas are byte-identical; OpenAPI differs only in the approved
approval identities and endpoint descriptions. Independent cleanup review returned scoped APPROVE. The final
three-package race run passes agentic (5.163s) and dispatch (2.478s); loop fails solely at the unchanged no-reopen
test (2.091s). Overall exit remains 1, not a passing push gate. Final log SHA-256:
`24dc68a241795ac34ba2967b9755b50fa8f53009d4444e22b6cfd980cc373448`. The preserved 32-source manifest
`/private/tmp/gh1146-approval-execution-echo.Rm4ZA5/final-cleanup-source.sha256` has SHA-256
`980009110aa821a1be3e195f91cc7d30c3ac712c576a7e7f9c51d785f43a6535`; root verified every entry. These cleanup
results supersede the fixture/schema/prose holds above, not the earlier native checkpoint's exact source identity.
The no-reopen correction, remaining proof/mutation gates, breaking-change E2E, and task 6.6 remain open.

The owner accepted the narrow approval-required ToolResult amendment in
[comment 5619622099](https://github.com/C360Studio/semstreams/issues/1146#issuecomment-5619622099):
pre-mutation exact authority even on warm routes, and effect-free superseded ACK only from positive validated
later-phase evidence for the exact execution. Ordinary final-result proofs and task 6.6 remain unchanged.
Replay inventory SHA-256 `1f5afffddf5ead0e612127ad053291627887c8d63cd9ea2b25885a7235416a8a` was revalidated
against the final cleanup checkpoint: 49 pins unchanged, 11 moved, all 60 snippets retained. The inventory and
initial reviewed design remain unchanged; this acceptance completes no implementation or verification task.

The first phase-supersession callback correction passes the original no-reopen regression, warm/cold supersession,
and warm/cold unseen-sibling refusal (race, package 1.612s). Superseded callbacks leave authority, process results,
and audit facts unchanged and emit the observed log plus private counter; the sibling is refused before insertion.
Log: `/private/tmp/gh1146-superseded-gate-runtime.VfFbCr/first-focused.log`, SHA-256
`20e7ecf7538086faf5cd8b296446828999f0d5a1b32b01a8b99d5a6f50a857aa`. Exact first-GREEN source copies are retained
alongside it. This is unit callback evidence, not native settlement, prompt PubAck, stale-revision, full matrix,
confirmed-absence, or E2E completion. Tasks 6.5c, 6.5d, and 6.6 remain open.

The E2E identity fixture fence cleared after #1262 merged and active claims were checked for overlap. Only
`approval_signal.go` and `approval_restart.go` were migrated; both echo the original displayed/checkpoint execution
and treat 409 as refusal without replacement identity. Independent fixture review APPROVE and non-Docker package
race PASS (1.419s) do not constitute a full tier run. Handoff:
`/private/tmp/gh1146-e2e-approval-echo.ptNlW3/handoff.md`, SHA-256
`59a992cf5c261d547b2727a2bca8c29c9f3c1ca643503e45675983ba0a297119`.

The subsequent prompt/CAS checkpoint passes 12 focused tests (race, 1.484s): pending replay preserves the original
snapshot, and a new gate uses the exact observed revision before prompt/audit publication. A cold conflicting
revision test proves no unconditional Put or speculative output, not a concurrent newer-closure restoration race.
The broader three-package run then exposed six fixture failures before their intended faults: exact retained
evidence was missing from the shared warm fixture. Tests now retain that evidence, fail Update behind successful
Get, and persist their timeout precondition. Cancellation retains no-ACK protection at the authoritative refusal;
the impossible-JSON channel fault remains direct handler/classifier coverage, not a wire-callback claim.

Independent review separately found an optional originating-call loop/trace correlation hole in the new positive
supersession proof. Paired conflict controls reached RED before the narrow approval-required guard was added;
matching and empty optional values remain accepted. The corrected three-package race run passes agentic (5.173s),
loop (3.367s), and dispatch (2.142s). Log:
`/private/tmp/gh1146-superseded-gate-runtime.VfFbCr/corrected-three-package-race.log`, SHA-256
`b460f860af1c408ee07d9c9615a40e908803bee905c9bfbf9e38169fd8414333`. The original package RED remains at
`/private/tmp/gh1146-phase-checkpoint.ZbBv7C/three-package-race.log`, SHA-256
`3384535056bf2da1c13a79feeded1cbb8279c97110d2ed866c8367a93b77087c`.
Later-history proof, concurrent stale-restoration proof, confirmed absence, diagnostic mutations, native/E2E,
full repository gates, and task 6.6 remain open. No mixed task checkbox is completed by this package checkpoint.

After independent correction/fixture review APPROVE, the existing six native approval controls pass together
(race, package 46.958s): approve, applied ApprovalResponse redelivery, modify, reject, older same-CallID isolation,
and original-deadline timeout. Ten pinned Go files remain byte-identical across the canonical run; its shared host
lock is released. Log: `/private/tmp/gh1146-phase-checkpoint.ZbBv7C/native-six-controls.log`, SHA-256
`accca76116f869c40af10a5ecd471ceda42645b7f2a678f72fe0dde6bdbd5ff8`; source manifest SHA-256
`ca2d02866a89ddadd4a80fac1bc7d87c9325952df4da0d4f78e5bc74451d5134`. These are ApprovalResponse controls,
not the distinct original approval-required ToolResult redelivery proof or a new full E2E tier pass.

The standard agentic E2E tier subsequently passes in 2m32.222s with 16 asserting stages, including approval after
OS-process restart (6.505s), on frozen production and the reviewed ExecutionID fixture migration. The local mock
LLM and a new isolated Compose project were used; only its temporary containers/volume/network were cleaned up.
All 967 pinned non-test Go, module, and relevant configuration files remain unchanged across the run. Log:
`/private/tmp/gh1146-phase-checkpoint.ZbBv7C/e2e-agentic.log`, SHA-256
`762441c3f2447c4f877c8a70e7a329aabaa108c8d6ee594e18eb4ea378ca0b5a`; source manifest SHA-256
`69863eb24e1b1c8e14d33805b0f7e9644c3cd9d2ba8236ca3aa2e1a4ab7a04c6`. The native ToolResult replay test
extension was being edited separately and is excluded from that non-test source manifest. This E2E pass does not
prove that new native test, later-history/concurrent-restoration/absence rows, or the full repository push gates.

The distinct native `TestIntegrationApprovalRequiredResultRedeliversAfterClosedGate` now passes in the seven-case
approval group (race, package 77.061s). It withholds only the original gated ToolResult ACK after the pending/prompt
witness, commits approval closure on a replacement, and observes source sequence 6 redeliver to a third owner as
delivery 2: ACK 1 / NAK 0 / Term 0. Before real ACK releases the native `MaxAckPending=1` slot, the test verifies
unchanged closure revision/bytes, executor/provider counts and pending-prompt sequence plus supersession
diagnostics. The queued final result then completes normally and all consumers drain. This is closed-gate proof,
not post-final or later-history proof. The earlier unexecuted test sequencing was rejected in review and corrected
without changing production policy or AckWait. Independent corrected-fixture review returned APPROVE.
Log: `/private/tmp/gh1146-phase-checkpoint.ZbBv7C/native-seven-controls.log`; source manifest and verification are
retained beside it. Remaining history, concurrent-restoration, confirmed-absence, diagnostic-mutation, full-push
gate, and task 6.6 obligations stay open.

The final pre-push lint correction extracts only the existing matching-pending prompt branch into a private helper;
validation, refusal text, publication order, and caller return semantics are unchanged. Independent source review
returned APPROVE at `settlement_recovery.go` SHA-256
`f2e1133873c0a1232ca82bc478ee56c11952e51a489830eed63bb7a07af3d042`. The original 86-statement and intermediate
81-statement lint failures remain retained; the limit and tests were not relaxed.

Final corrected-source `task check:push` passes, including full unit/race (loop 3.452s, dispatch 3.177s) and canonical
integration/race (loop 269.133s, dispatch 77.173s). This integration run includes the new closed-gate native proof.
The standard agentic E2E then passes all 16 asserting stages in 2m32.155s, including approval after OS-process
restart (6.540s), using the local deterministic mock. Only the isolated test stack and its temporary data were
removed. All 2230 pinned Go sources remain unchanged across both final gates; module files and generated outputs
have no drift from their intended checkpoint. Logs in `/private/tmp/gh1146-phase-checkpoint.ZbBv7C`:
`check-push-corrected.log` SHA-256 `1ad34a7a386d43d41f2f1b9ccb4e3f845ef41298fff8a8d7901f570313b8ef32`;
`e2e-agentic-final.log` SHA-256 `d6bef537d2ae97031094b5db2ade248a31a8cb48d753be50a53e1bd419081ae9`;
`final-gate-source.sha256` SHA-256 `0b9fbd2463c00fef581265537d03b2c5772e3bd94054c64f74a527b0b1e1809e`.
Linux/amd64 build, read-only module tidy-diff, identity audit (1299 candidates), all three guard fixtures, and
strict OpenSpec validation (55/55) also pass. The API report's first offline attempt could not fetch its analysis
tool; the unchanged command with network access completes all 62 comparisons and reports 14 incompatible
packages against beta.162. That is reporting-only evidence, not compatibility approval or a flake waiver.
These final-source gates supersede the local push/E2E holds above, not the missing later-history, concurrent
restoration, confirmed-retention-absence, diagnostic-mutation, broader matrix, or task 6.6 obligations.
Mixed task checkboxes remain open. This is a bounded draft checkpoint, not archive, merge, closure, or parent cutover.

- [ ] 6.1 RED: run the real-NATS approval replacement gate after an approval-required `ToolResult` fully settles.
  Replace loop and dispatch, discard every process map/cache, retain `AGENT` and `AGENT_LOOPS`, and independently
  exercise approve, modify, reject, timeout, and redelivery. Cite exactly
  `// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded`.
- [ ] 6.1a Add an explicit assertion stage to the standard agentic E2E tier: reach durably settled pending
  approval, kill and restart the application while retaining NATS, prove process replacement, submit approval
  through the existing HTTP route, and verify the exact gated tool execution and terminal completion. Reuse the
  existing process controller and approval helpers; add no production recovery hook or new storage. Keep the
  full decision/error/correlation matrix in real-NATS integration tests. Prove the stage fails when restart or
  approval recovery is omitted; count its assertions honestly. Cite the same requirement as 6.1. The existing
  live approval walk and unrelated process-replacement stage do not satisfy this task. Owner accepted this
  explicit E2E addition on 2026-09-09; task 6.6's mechanism ruling remains unchanged.
- [ ] 6.2 Prove the settled approval-required `ToolResult` is available from current
  `LoopEntity.PendingToolResults[PendingApproval.ExecutionID]` and agrees with pending RequestID, ExecutionID,
  ordinal, provider CallID, name, LoopID, trace, and approval-required classification. Match provider CallID only
  within the current response and perform no `ToolResult` stream lookup. Tests cite exactly
  `// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded`.
- [ ] 6.3 Implement only operation-specific exact reads for latest `agent.request.<LoopID>` and exact
  `agent.response.<RequestID>`. Validate envelopes, payloads, cross-record identities, current-call uniqueness, and
  canonical arguments. Perform no `AGENT` list or scan. Tests cite exactly
  `// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded`.
- [x] 6.4 Prove same-CallID/different-RequestID isolation with two retained responses carrying conflicting arguments.
  Reconstruction follows only the RequestID named by the current request. Cite exactly
  `// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded`.
- [ ] 6.5 Table-test approve, modify, reject, and timeout across transient/unresolved Retry, confirmed retained
  matching-gate evidence absence to durable `continuation_unavailable`, malformed/matching-gate identity conflict
  to Quarantine, exact match to continue or prove the branch already applied, and validated noncurrent gate to
  observable effect-free ACK. The latter does not prove which historical decision applied. Ordinary applicable
  branch publication remains at-least-once.
  Any branch that becomes terminal proves every settlement-required terminal effect before the final bare terminal
  LoopEntity marker; bare terminal state alone never proves which ToolResult applied. Cite exactly
  `// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded` and
  `// spec: agentic-loop / Loop task, request, and tool work use only required correlation` as applicable.
- [ ] 6.5a Carry existing ExecutionID through approval pending output, the existing authority-backed pending HTTP
  projection, required HTTP ApprovalRequest, ApprovalResponse, and timeout production. Compare the displayed echo
  at dispatch admission and again at loop application. Return HTTP 400 for omission and 409 for a noncurrent gate
  without publication; terminate identity-less direct wire input. Implement noncurrent-gate ACK through the
  existing refusal/skip diagnostic ownership with a structured log and one narrow private loop counter; keep
  identities out of metric labels. Add no receipt, public status/helper/configuration, authority, or recovery read.
  This task does not include tracker retirement or unrelated projection work. Tests cite exactly
  `// spec: agentic-loop / Approval continuation after replacement is exact and evidence-bounded` and
  `// spec: agentic-dispatch / Dispatch uses one authority-backed current-state projection`.
- [ ] 6.5b GREEN: prove the reviewed native applied-approval redelivery and seeded old-A/current-B regressions under
  the accepted echo semantics without overstating either fixture. Cover HTTP missing/stale echo, post-publication
  gate change, timeout snapshot identity, all four matching branches, same-gate correlation conflict, and
  missing/unreadable/malformed authority. For inapplicable delivery assert log plus metric, zero business
  publications, unchanged durable authority, and no applied-decision audit event. Prove one gate per execution,
  replay without reopening after closure, preserved approver/arguments and rejection provenance, and existing
  same-gate contention obligations; return with measured failure before adding coordination. Verify only the
  declared pending JSON/OpenAPI identity addition, and require the relevant agentic E2E green before breaking
  changes land. Cite the same requirements as 6.5a and the terminal-release requirement named in 7.7.
- [ ] 6.5c RED: extend the no-reopen callback regression with warm/cold exact-authority observation before mutation,
  approve/modify closure, reject/timeout/final-result supersession, and positive later-request history. Cover
  missing/conflicting evidence, a matching pending prompt with failed PubAck, a genuinely new gate, an unseen
  different gated sibling, and closure winning after a stale observation. Assert unchanged authority/results and
  zero business publication on superseded/refused paths; preserve ordinary unequal-final-result refusal tests.
  Observe the supersession log and private unlabeled counter, and prove diagnostic omission fails the assertions.
  Cite exactly `// spec: agentic-loop / Approval-required tool statuses settle by observed execution phase`.
- [ ] 6.5d Implement the narrow approval-required branch in the existing delivery/recovery owner using the existing
  revision-bearing exact read, admitted correlation evidence, and conditional KV Update before gate publication.
  Do not infer replay from the accumulator after insertion, replay a matching prompt by reopening its gate, or
  persist an unseen gated sibling as consumed evidence. Prevent stale restoration/write from regressing closure;
  document and test any necessary bounded local critical section using existing owner primitives. GREEN the
  scoped callback/race/replacement tests and required agentic E2E; retain task 6.6 and all unrelated fences.
  Return an exact failing boundary before adding coordination, storage, or another proof mechanism.
  Cite exactly `// spec: agentic-loop / Approval-required tool statuses settle by observed execution phase`.
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
  `LoopInfo` JSON/schema is unchanged except for `execution_id` on existing nested `PendingApprovalInfo`, and
  contains no aggregate-to-directional token mapping. This exception permits no unrelated projection expansion.
  Cite exactly
  `// spec: agentic-dispatch / The shared loop view classifies the mixed bucket`.
- [ ] 6.9 Delete `LoopTracker`, its approval buffer, created/pending dispatch inputs and consumers,
  `Component.LoopTracker()`, and all tracker-driven correctness. Preserve created/pending loop outputs and external
  subscribers. Prove dispatch neither creates nor advances intermediate loop state and settles validated routeless
  non-user terminal events without `user.response`. Cite exactly
  `// spec: agentic-dispatch / Dispatch is exclusively an edge gateway`.
- [ ] 6.10 Replace `CommandContext.LoopTracker` with classified `LookupLoopOwner`. Preserve `LoopInfo` only as the
  immutable view-derived `/loops` and `/debug/state` response DTO and prove its JSON/OpenAPI schema is unchanged
  except for `execution_id` on existing nested `PendingApprovalInfo`. Preserve all unrelated fields and mappings;
  the approval correction does not itself authorize this task's tracker retirement or other projection work.
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
  waiter, duplicate/conflict, panic, replacement, terminal-marker ordering, and pre-marker Retry tests. Prove
  best-effort audit and nonblocking graph evidence remain outside settlement-required effects. Cite exactly
  `// spec: agentic-loop / All six loop input classes settle after owner-specific durable done` and
  `// spec: agentic-loop / Loop task, request, and tool work use only required correlation` as applicable.
- [ ] 7.6 Refactor cancel, approval-response, approved-verdict, and rejected-verdict through their four existing
  private owners. Unknown UserSignal terminates; cancel waits for current state, `COMPLETE_<loopID>`, and terminal
  PubAck; missing process correlation never authorizes ACK. Keep `ResponseAction.Signal` and
  `ClassifiedIntent.SignalType` outside durable `agent.signal.*` ownership. Every terminal branch persists the bare
  terminal LoopEntity only after its settlement-required terminal effects; best-effort audit and nonblocking graph
  evidence remain nonblocking.
- [ ] 7.7 Reconcile current `agentic-loop` requirement
  `Per-loop in-process state is released at terminal, through the one release point`: preserve single-point release
  and unaffected scenarios. Preserve ordinary final-tool/model lane-specific durable applied proof, Retry on
  unresolved evidence, and Quarantine on correlation conflict/impossible transition. Approval-required statuses
  additionally use positive phase-supersession proof under the requirement named in 6.5c, with a pre-mutation exact
  authority read even on warm delivery. Preserve existing confirmed-retained-absence handling. For ApprovalResponse,
  permit validated coherent exact
  current authority with another pending ExecutionID or no gate to establish observable effect-free inapplicable
  ACK, without historical applied-decision proof. Assert log plus metric, no business publication or authority
  mutation, and no fabricated applied provenance. Process absence or failed authority observation establishes
  nothing; matching-gate correlation conflict quarantines. Cite exactly
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
  `task e2e:agentic`, including 6.1a's approval-after-process-restart stage. The AGENT DiscardNew cutover and
  first-party publisher path require covering E2E green.
- [ ] 11.3 Correct false restart claims in concepts 03, 17, and 27; link concept 33 without duplicating its message-
  pump explanation. Document provider at-least-once recovery, retained-response reuse, stable RequestID, and
  ambiguous-replacement duplicate risk; continuation/Store; AGENT admission and DiscardNew backpressure,
  loop authority, boot order, metrics, raw external-executor migration, heartbeat defaults, and rule-agent publisher
  admission. Document the user-facing control contract: cancel, durable ApprovalResponse wait, retry/restart from the
  last durable boundary, lifecycle quiesce, no arbitrary pause/resume, and future suspension only as a new contract.
  Reconcile schemas and every example/fixture. Remove paused vocabulary from `agentic/README.md`,
  `processor/agentic-loop/README.md`, `docs/concepts/13-agentic-systems.md`,
  `docs/operations/migration-beta162-to-beta163.md`, and generated `specs/openapi.v3.yaml`.
  Document required producer-owned TaskMessage LoopID, same-marshaled-publication retry, downstream retained-byte
  redelivery, direct sister migrations, and the absence of any empty-ID compatibility path or consumer recovery owner.
- [ ] 11.4 Confirm PR #1159 carries the complete #1146 claim set, `implemented-by: Sol`, `Closes #1146`,
  `Refs #759`, `Refs #1155`, `Refs #1249`, and explicit “#1146-owned tranche; #1155 remains open” wording. It SHALL
  NOT carry `Closes #1155`. Preserve PR #1251 as #1239's retained authorship/review record and PR #1156 as final
  default-branch closing authority after #1249 and the combined proof gate.
- [ ] 11.5 Complete implementation/proof, then obtain SemStreams implementation review of the complete claim set.
- [ ] 11.6 Obtain owner-requested cross-agent review, apply every finding, and repeat both reviews until accepted.
- [ ] 11.7 Before archive, reconcile every exact modified or replaced current requirement, then archive
  `agentic-loop-restart-safety` as the final content commit and sync all eight current specs: `agentic-dispatch`,
  `agentic-governance`, `agentic-loop`, `agentic-model`, `agentic-tools`, `entity-id-contract`,
  `graph-view-subscription`, and
  `rule-agent-publishing`. Preserve every unaffected scenario and valid citation from each replaced current
  requirement.
- [ ] 11.8 Obtain narrow archive/current-spec-sync review. Only afterward run hosted CI, remote-base verification,
  undraft, and non-default merge. The staged merge creates checkpoint `A` for #1249.

## Transferred: AgentRun

Tasks H.1/H.2 remain removed. #1249 owns post-#1146 inventory, design, complete/failed migration, and replacement
proof against exact staged checkpoint `A`. This transfer narrows no other #1146 subscription.
