# Next-tag test gate blockers design

## Checkpoint

- Accepted inventory:
  `docs/proposals/next-tag-test-gate-blockers-inventory.md`
- Accepted inventory SHA-256:
  `d91a49caa42d027df482c0c8adc4ebe4f290459e5b161a81bf8d11a372662d7a`
- Inventory review: `INVENTORY PASS`.
- This document is advisory target state. Binding decisions remain with the owner.

## Owner acceptance

The owner accepted the independently reviewed design on 2026-08-23 at exact SHA-256
`56fa9dc95a4dbf6f3f7d121912972e036f7c1c2d55a8834e991eeafa8b37ae7a`.

## Recommendation

Land two independent pre-tag truth corrections.

1. **#1062 narrow four-site ownership fix.** Remove the two redundant early `defer tc.Terminate()` calls. Replace the
   two ignored Background Stop cleanups with finite, asserted terminal Stop contexts. Add a deterministic owner-order
   RED and a separate white-box classification proof for the entity-borrow cancellation seam. Change no production
   lifecycle behavior.
2. **#1063 current-truth correction.** Remove the false wall-clock parallelism oracle, its two synchronization sleeps,
   and every outward claim that this single wire consumer supplies local parallel tool execution. Retain a causal
   multiple-call/result integration proof. State explicitly that `MaxAckPending=3` is acknowledgement admission, not
   executor concurrency. Add no runtime dispatcher before the tag.

File the remaining #1062 cleanup-root population and an AST/type-aware guard as a distinct audit issue/change. A
mechanical next-tag rewrite of 295 textual sites would mix deliberate contract tests, unrelated Stop methods, helpers,
and actual unbounded roots, repeating the mechanical migration that created the observed regression.

A future local-concurrency feature for agentic-tools is viable only as separate post-tag design work. It must own
execution bounds, effect overlap, completion ordering, heartbeat settlement, shutdown joins, rate pressure, retry
ambiguity, executor safety, status, and E2E. It is not a test repair.

## Measurable premises

| Premise | Measurement |
|---|---|
| #1062 causal surface | `processor/rule/readiness_integration_test.go:67,82,106,132` |
| TestClient owns cleanup | `natsclient/test_client.go:852-869`; idempotence at lines 929-938 |
| Correct order already exists | TestClient Cleanup registers first; processor Cleanup later; Cleanup is LIFO |
| Operation/test context is unusable in Cleanup | function defers and `testing.T.Context()` cancel before Cleanup |
| Finite detached terminal Stop is admitted | `component/lifecycle.go:43-57` and context-ownership exception |
| Observed borrow wait honors caller ctx | `processor/rule/processor.go:1329-1331,1404-1423` |
| Full Stop deadline behavior is not completely classified | command fence cancels then joins at lines 1426-1445 |
| Population is not one safe patch | 295 syntax-only sites across 90 files in the post-#1059 tree |
| #1063 wire path is serial | one handler; inline nats.go callback; joined heartbeat work |
| MaxAckPending is not local concurrency | `jetstream-consumer-policy` truth and gh963 inventory |
| Empirical result matches serial work | ten exact runs in 6.438s, about 0.64s per three 200ms calls |
| False claim is outward | README, package GoDoc, concepts guide, JetStream tuning guide |
| Exact sleeps are ratcheted debt | policy baseline entries guarded by testinfra policy tests |
| Current agentic-tools capability has no concurrency promise | no concurrency language in current spec |

## Options

### #1062

#### Option 0: do nothing

One cleanup can still consume the global 20-minute gate and run after destroying its substrate.

#### Option 1: add only finite Stop contexts

This reports failure promptly but preserves substrate-before-owner teardown. It treats the symptom and leaves the
causal ownership defect.

#### Option 2: fix teardown order and bound/assert Stop

Remove the two redundant termination defers and bound/assert the two Cleanup Stops. This restores owner-before-substrate
teardown, makes non-settlement attributable, preserves production behavior, and is independently reversible.

This is recommended.

#### Option 3: migrate or guard all 295 textual sites now

The census is not semantic. It mixes APIs, deliberate contract calls, cleanup roots, and false positives. A broad
rewrite cannot be reviewed as one behavior. Retain it as a separate audited program.

#### Option 4: change production Rule cleanup now

The observed test destroyed NATS before Stop. It does not prove a production defect. A direct helper already honors its
caller context, while another phase deliberately joins after cancellation. No production change has causal evidence.

### #1063

#### Option 0: do nothing or retry

The test keeps both its false positive and false negative.

#### Option 1: widen 800 milliseconds

This makes the false-positive problem worse and remains host-speed classification.

#### Option 2: correct current truth

Replace timing classification with exact result causality, remove sleeps, correct all outward documentation and current
capability truth, and add no runtime dispatcher. This is small, reversible, and faithful to implementation.

This is recommended before the tag.

#### Option 3: implement true local concurrency

A component-owned bounded execution design is required. A barrier-only test edit cannot make one serial native callback
concurrent. Multiple consume handles or fetch/dispatch workers change durable delivery, heartbeat and Stop ownership,
effect ambiguity, and external executor obligations. This is post-tag feature work.

## Slice 1: #1062 narrow lifecycle cleanup

### Target state

For both Rule readiness integration tests:

1. Keep `natsclient.NewTestClient(t, ...)` as the sole TestClient cleanup registration.
2. Remove the redundant `defer tc.Terminate()`.
3. Register processor cleanup only after successful Start.
4. At Cleanup entry, observe that the TestClient/NATS substrate is still healthy. If it is not, record the exact
   owner-order failure but still attempt bounded Stop.
5. Immediately derive `context.WithTimeout(context.Background(), 5*time.Second)` in terminal cleanup. Five seconds
   restores the pre-`61fbd48f` test safety budget; it is not a performance assertion.
6. Defer cancel locally, call `processor.Stop(stopCtx)`, and assert its returned error with exact test/owner attribution.
7. Let NewTestClient's earlier Cleanup run afterward by LIFO.

The health precondition must not call `FailNow` before Stop. Cleanup must still attempt Stop when order is broken and
report both order and Stop failures without masking the first attribution.

No exported helper, timeout knob, production field, context provider, production goroutine, or detached production task
is added. A private file-local helper is permitted only if both current tests use it and it accepts the Stop operation
rather than creating a general lifecycle abstraction.

### Production classification proof

Add one package-local white-box test for `awaitEntityBorrowSettlement`:

- hold the exact done channel open;
- provide a cancel spy;
- end the supplied context causally;
- assert the helper returns the exact context error and invokes cancellation once;
- run the blocking helper in one test-owned goroutine so the forced omission remains observable;
- release every held channel and join that exact goroutine during Cleanup, including omission/failure paths;
- use a generous two-second outer liveness bound only as containment, never as the behavior oracle.

This classifies the seam named by the dump. It does not claim that the entire Rule Stop sequence returns at an exact
deadline. If unchanged production fails this proof, stop and reopen production design instead of weakening the test.

### RED, GREEN, and omission proof

RED:

1. Add the NATS-live-at-processor-cleanup precondition and finite asserted Stop while temporarily retaining the two
   explicit Terminate defers.
2. Run the two focused tests. Both must fail promptly because the function defer ran before processor Cleanup.
3. Capture the owner-order message.

GREEN: remove only the two redundant defers; the same tests pass.

The historical 824.035-second stall is provenance and must not be repeated.

In an isolated copy:

- reinsert one explicit termination defer; its test must fail the substrate-live precondition before bounded Stop
  completes;
- make `awaitEntityBorrowSettlement` ignore caller cancellation; its white-box test must hit the two-second containment
  bound with exact attribution, then release the held completion signal and join the test-owned helper goroutine;
- restore both mutations and verify the shared production file is unchanged.

### Focused bounded gate

```text
go test -v -race -tags=integration ./processor/rule \
  -run '^TestIntegration_RuleReadiness_(EmptyReplayIsAuthoritativelyNothingToDo|NonEmptyReplayReportsScope)$' \
  -count=20 -timeout=120s

go test -race ./processor/rule \
  -run '^TestAwaitEntityBorrowSettlement_' \
  -count=100 -timeout=30s
```

Verbose test start/pass lines are the progress signal. If they stop advancing beyond twice the measured 2.144-second
envelope per test, capture stacks and abort. Do not wait for the 120-second containment timeout.

## Separate #1062 population audit and guard

File a distinct issue before tag authorization requiring:

- AST/type-aware enumeration of Cleanup/defer Stop calls, including aliases, multiline calls, and derived contexts;
- classification as unbounded terminal root, bounded call, deliberate API-contract call, non-lifecycle Stop, or
  uncertain owner;
- stable manifest identities not keyed only by line number;
- package-at-a-time remediation;
- an AST guard rejecting new unbounded lifecycle Stop cleanup roots while grandfathering reviewed exact entries;
- fixtures for direct, closure, helper, alias, multiline, bounded, deliberate-contract, and false-positive shapes;
- zero production behavior change.

This follow-up is not satisfied by mechanically editing the 295 grep matches.

## Slice 2: #1063 tool-execution truth correction

### Target state

Rename the integration test to `TestIntegration_MultipleToolCallsProduceAllResults`. It must:

1. Use three executor names and three call IDs.
2. Add no artificial execution delay.
3. Rely on successful component Start as consumer-commit evidence, not a 200-millisecond sleep.
4. Subscribe before publishing and rely on successful Subscribe, not a 100-millisecond sleep.
5. Deliver decoded results into a buffered channel.
6. Publish all calls and collect exactly the expected IDs under one generous finite liveness context.
7. Reject duplicate, missing, unexpected, or error-bearing results with exact call attribution.
8. Use a fresh finite terminal Cleanup Stop context and assert Stop.
9. Use no `time.Now`, `time.Since`, elapsed comparison, ratio, or concurrency language.

The test proves multiple wire calls settle. Current implementation evidence uses one serial callback path, but the test
and capability promise neither serialization nor overlap. They do not turn the pinned dependency mechanism into a
forever API promise and do not infer overlap from completion.

Remove the two obsolete sleep entries for the old test from `test/testinfra/policy_baseline.json`. The policy guard must
pass with no stale entry.

Correct all outward surfaces in this slice:

- `processor/agentic-tools/README.md`: remove concurrency as a feature; record the current one-callback implementation
  as nonnormative evidence and state that the wire contract promises neither serialization nor overlap.
- `processor/agentic-tools/doc.go`: remove own-goroutine, parallel-call, and broad component concurrency claims; retain
  registry lookup thread safety and per-call context cancellation; promise neither serialization nor overlap.
- `docs/concepts/13-agentic-systems.md`: describe multiple-tool result aggregation, with the current serial callback path
  labeled implementation detail rather than capability. Keep source-ordinal ordering where another caller produces
  concurrent completions.
- `docs/advanced/11-jetstream-tuning.md`: preserve admission-versus-execution truth; remove tables/comments equating
  MaxAckPending with local tool concurrency; describe value 3 as delivered-but-unacknowledged admission only.

Do not change MaxAckPending, subjects, consumer names, AckWait, heartbeats, durable outcomes, registry API, or production
component code.

### Executor-author seam

An external executor author should know only:

1. this wire consumer provides no local parallel-dispatch guarantee;
2. MaxAckPending is not a thread-safety promise.

Direct exported execution callers remain outside the wire guarantee. An already thread-safe executor remains safe. No
new worker count, timeout, subject, configuration, or lifecycle knowledge is imposed.

### RED, GREEN, and omission proof

RED evidence includes the historical 811.816917-millisecond failure and the isolated ten-run mean of about 0.64 seconds,
which proves the old test accepted serial execution.

For the policy guard RED, remove the two baseline entries before removing the sleeps. Run
`TestInfrastructurePolicyGuard`; it must be selected by name and fail. Then remove the sleeps and run that same named
guard to GREEN. Package-only `ok` output without the selected test name is not evidence.

GREEN requires repeated exact result-ID settlement under race, zero timing/concurrency oracle in the test, a clean policy
guard, and documentation/spec agreement on admission versus execution.

In an isolated copy:

- omit publication of one call; the collector must fail at its finite bound and name the missing ID;
- restore either removed sleep after its policy entry is gone; the policy guard must fail;
- restore one outward claim that MaxAckPending allows parallel tools; design/spec conformance review must reject it.

Do not manufacture host load to make an elapsed assertion fail.

### Focused bounded gate

```text
go test -v -race -tags=integration ./processor/agentic-tools \
  -run '^TestIntegration_MultipleToolCallsProduceAllResults$' \
  -count=20 -timeout=90s

go test -v ./test/testinfra \
  -run '^TestInfrastructurePolicyGuard$' \
  -count=1 -timeout=30s

go test ./test/contract/... -count=1 -timeout=120s
```

If exact result IDs stop advancing, capture missing-ID state and abort rather than waiting for the package/global timeout.

## OpenSpec delta

Target: `openspec/specs/agentic-tools/spec.md`.

### Requirement: Wire tool execution does not infer local parallelism from acknowledgement admission

The agentic-tools `tool.execute.>` path SHALL produce a correlated terminal outcome and result for each admitted tool
call. The component SHALL NOT claim that `MaxAckPending=3` supplies local executor parallelism. That value governs
delivered-but-unacknowledged admission only. The wire contract SHALL promise neither serialized execution nor execution
overlap to executor authors or direct callers.

Multiple queued tool calls SHALL each produce their correlated durable result. Their correctness SHALL be proved by
exact call/result causality under a finite liveness bound, not by elapsed wall-clock classification.

The current implementation uses one native callback through outcome persistence, result publication, and delivery
settlement before that callback returns. That is nonnormative implementation evidence, not a stable serialized-execution
contract. This requirement governs correlated wire outcomes and makes no execution-overlap promise for arbitrary direct
callers of exported execution methods.

#### Scenario: multiple wire calls settle

- **GIVEN** three admitted tool calls with distinct call IDs
- **WHEN** the wire consumer processes them
- **THEN** each call produces its exact correlated result
- **AND** the proof uses no elapsed-time threshold

#### Scenario: acknowledgement admission is three

- **GIVEN** agentic-tools uses its component-owned `MaxAckPending=3`
- **WHEN** the consumer is observed
- **THEN** the value bounds delivered-but-unacknowledged messages
- **AND** no executor-concurrency claim is inferred

No #1062 capability delta is needed because that slice makes tests conform to current lifecycle truth. No ADR is needed
for either recommended slice. A future production concurrency feature requires fresh OpenSpec design and may require an
ADR if it creates a cross-repository executor-safety or delivery contract.

## Gates and landing order

Land separate commits and PRs:

1. #1062 four-site ownership plus diagnostic test.
2. #1063 test, documentation, capability, and policy-baseline truth correction.
3. Separately filed #1062 population audit/guard remains independently scheduled.

After each focused proof:

```text
task lint
go test -race ./...
go build ./...
task schema:generate
git diff --exit-code -- schemas/ specs/
go test ./test/contract/...
openspec validate --all --strict --no-interactive
```

Run the canonical Docker integration gate only after focused proof. Observe package progress actively and capture/abort
on a proven wedge. The 20-minute test timeout is final containment, never the waiting strategy.

No `-p1`, blanket timeout increase, retry-until-green, production lifecycle change, or local concurrency feature belongs
in these slices.

## Canonical skills

No canonical skill changes these slices:

- `kv-or-stream`: no new communication path.
- `new-payload`: no payload.
- `query-pattern`: no query surface.
- `orchestration-check`: the recommended work adds no orchestration. Apply it if a future local concurrency feature is
  selected; execution remains component work but still needs bounded dispatch/lifecycle design.

## Owner rulings requested

1. Accept #1062 Option 2, including teardown order and finite asserted Stop.
2. Accept five seconds as restored test safety bound, not a performance SLO.
3. Accept the test-only entity-borrow classification proof and no production Rule change absent failure.
4. Require a separate AST/type-aware population audit/guard rather than a 295-site mechanical migration.
5. Accept #1063 Option 2: correct current one-callback implementation evidence, promise neither serialization nor
   overlap, and add no local concurrency feature before the tag.
6. Accept correction of all four outward documentation surfaces plus the agentic-tools capability delta.
7. Accept exact-result causality and removal of both policy-baseline sleeps.
8. Defer true local tool concurrency to fresh post-tag design.
9. Accept separate commits/PRs and bounded focused gates before the monitored canonical integration gate.
