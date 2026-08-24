# GH-1054 lifecycle drain flake design

Status: owner accepted on 2026-08-23 after `DESIGN REVIEW PASS` of SHA-256
`220bdbb0489b3885387a663a00f6f1947bfd576a500b95e479004b49e6c1b781`; this design is implementation authority.

## Accepted inventory identity

Locator: docs/proposals/gh1054-lifecycle-drain-flake-inventory.md
Baseline: 774c85dcf75bdce242f1f15ee2a5a310991ecf0d
SHA-256: 01806c6e2b6ba6a87efcdd45724f79c4be999fdc621f40464700d532ebf54489
Inventory verdict: INVENTORY PASS.
Hosted-run confirmation resolved the inventory's open evidence question without changing its identity:
graph-embedding first failed at lifecycle_owner_test.go:218, then Client.Close timed out because release remained open.
The accepted inventory is incorporated unchanged by the exact locator, baseline, and digest above.

## Decision boundary

The defect is test-owned. Five copied lifecycle-owner tests assert that Start cannot return while Stop drains,
although their fixtures set lifecycleUsed=true and each production Start immediately rejects that state.
When the scheduler runs Start before the test's default select, t.Fatal skips close(release).
The blocked NATS callback then causes the correctly implemented Client.Close native-drain wait to expire.

The valid behavior to preserve is narrower:

- admitted callback authority remains live while its exact subscription drains;
- owner children remain live until callback drain completes;
- after release, Stop cancels and joins runtime work, closes owner children, and returns;
- a completed repeated Stop is harmless;
- no test callback can remain blocked during t.Cleanup.

## Measured premises

P1. `rg -n "Start returned before.*Stop" processor --glob '*_test.go'` found exactly five copies:

- processor/graph-embedding/lifecycle_owner_test.go:218
- processor/graph-clustering/lifecycle_owner_test.go:216
- processor/graph-query/lifecycle_owner_test.go:182
- processor/graph-index-temporal/lifecycle_owner_test.go:181
- processor/graph-index-spatial/lifecycle_owner_test.go:189

P2. All five fixtures set lifecycleUsed=true before launching the concurrent Start.
Their production Start paths reject lifecycleUsed immediately:

- graph-embedding component.go:623-650
- graph-clustering component.go:928-955
- graph-query component.go:463-490
- graph-index-temporal component.go:464-491
- graph-index-spatial component.go:453-480

P3. Concurrent lifecycle calls and Start/Stop serialization are not portable guarantees:

- component/lifecycle.go:43-52
- openspec/changes/align-standard-lifecycle-tests/specs/component-lifecycle/spec.md:25-31
- docs/adr/095-one-shot-running-lifecycle-with-retained-failed-start-cleanup-authority.md:24-41

P4. Exact child-drain-before-cancel ordering remains required:

- component-lifecycle delta lines 3-23
- ADR-095 lines 26-30

P5. Client.Close awaiting native CLOSED is current truth at
openspec/specs/jetstream-consumer-policy/spec.md:287-308 and was deliberately corrected by PR #1019.

P6. The release gate is blocked by any nonzero deterministic test path:
openspec/specs/release-candidate-proof/spec.md:7-35,114-120,215-240.

P7. No sister repository uses WithDrainTimeout, while multiple sisters consume Client.Close and
NewSharedTestClient. A transport change would create adopter risk without addressing the first failure.

P8. Each affected file already imports sync for lifecycleObservedContext, so an idempotent release guard requires
no new package dependency or exported helper.

## Options

### Option A — Do nothing

Keep the five tests and rely on favorable scheduling or CI reruns.

Cost:

- the unsupported assertion remains scheduler-dependent;
- a losing schedule strands a callback and adds a 30-second secondary timeout;
- exact-candidate Test evidence remains nondeterministically red;
- tag authorization remains blocked whenever the failure occurs;
- rerunning until green violates the repository's flake discipline.

### Option B — Patch only graph-embedding and graph-clustering

Correct only the two packages named in #1054's hosted failures.

Cost:

- leaves the identical contradiction in graph-query, graph-index-temporal, and graph-index-spatial;
- moves the next failure rather than removing the class;
- fails the repository-first same-class collision inventory;
- requires future reviewers to rediscover why two copies differ from three siblings.

### Option C — Correct all five owner tests

Remove the unsupported concurrent Start/Stop serialization probe from all five copied tests.
Preserve deterministic proof of exact drain order, callback-context lifetime, child lifetime, finalization,
and completed repeated Stop.
Register an idempotent release cleanup before any assertion can terminate the test.

Cost:

- five test files change instead of two;
- test names and assertions must be reconciled consistently;
- focused repeated race proof and one full uncached integration gate are required.

Benefits:

- fixes the measured first failure at its owner;
- closes every same-class copy found by the accepted inventory;
- preserves production and native-drain behavior unchanged;
- creates no outward surface or downstream migration;
- makes an unexpected assertion failure release callbacks before Client.Close cleanup.

### Option D — Change production lifecycle, Client.Close, timeout, or CI scheduling

Possible shapes include delaying Start behind Stop, lowering/increasing drainTimeout, immediately force-closing,
changing TestClient cleanup, or restoring a package-parallelism cap.

Cost:

- production Start serialization would add a lifecycle promise current authority explicitly does not make;
- weakening Client.Close would regress #1019 and violate current native-drain truth;
- timeout changes alter exported framework behavior and affect sister callers;
- a CI parallelism cap may reduce scheduling exposure but leaves the contradictory tests intact;
- #736's Docker-contention program is adjacent and remains independently owned;
- all shapes act on secondary surfaces rather than the reproduced first failure.

## Recommendation

Recommend Option C: one narrow, test-only correction propagated across all five measured copies.

Do not change production components, natsclient, TestClient, CI parallelism, timeout constants, OpenSpec truth,
ADRs, schemas, configuration, NATS subjects/buckets/streams, or sister repositories.

This recommendation is advisory pending independent design review and owner acceptance.

## Exact five-file target

Files:

1. processor/graph-embedding/lifecycle_owner_test.go
2. processor/graph-clustering/lifecycle_owner_test.go
3. processor/graph-query/lifecycle_owner_test.go
4. processor/graph-index-temporal/lifecycle_owner_test.go
5. processor/graph-index-spatial/lifecycle_owner_test.go

In each drain-order test:

1. Immediately after creating release, create one idempotent release function using sync.Once.
2. Register that function with t.Cleanup after newLifecycleNATSClient has registered Client cleanup.
   Go's LIFO cleanup ordering then releases the callback before Client.Close begins native drain.
3. Replace normal-path close(release) with the same idempotent release function.
4. Remove startResult, the concurrent c.Start goroutine, the nonblocking default select, and the later
   receive asserting "already used".
5. Remove the concurrent second-Stop assertion from this drain-order proof; concurrent lifecycle behavior is
   outside the portable contract and is not needed to establish native drain ordering.
6. While the first Stop is blocked in native drain, retain deterministic assertions that:

   - the admitted callback context remains live;
   - the owned embedder/LLM child remains open where that component has one;
   - Stop has reached the drain boundary through lifecycleObservedContext.

7. Release the callback through the idempotent helper and await the first Stop result.
8. After Stop completes, assert:

   - callback authority has ended where the owner contract exposes that observation;
   - the embedder/LLM child is closed for graph-embedding, graph-clustering, and graph-query;
   - completed repeated Stop returns nil without repeating teardown.

9. Rename each test to describe drain/lifetime behavior without "serialization":

   - graph-embedding: TestLifecycleOwnerDrainKeepsCallbackAndChildLiveUntilCompletion
   - graph-clustering: TestLifecycleOwnerDrainKeepsCallbackAndChildLiveUntilCompletion
   - graph-query: TestLifecycleOwnerDrainKeepsCallbackAndChildLiveUntilCompletion
   - graph-index-temporal: TestLifecycleOwnerDrainOrderAndCompletedRepeat
   - graph-index-spatial: TestLifecycleOwnerDrainOrderAndCompletedRepeat

Existing TestLifecycleOwnerNoActionStopIsTerminal cases retain concrete one-shot Start rejection coverage.
The drain-order tests therefore lose no valid one-shot behavior evidence.

## Correction-propagation sweep

Implementation review must not stop after changing the five named assertion lines.

Run:

`rg -n "Start returned before.*Stop|Start returned before serialized Stop" processor --glob '*_test.go'`

Expected result: zero.

Within the five files, inventory every callback blocking on a release channel:

`rg -n "<-release|close\\(release\\)|t\\.Cleanup" <the-five-files>`

For every such callback:

- classify whether failure before normal release can reach Client.Close cleanup;
- require an idempotent cleanup release where it can;
- preserve existing rollback tests that already use sync.Once;
- correct other unguarded release cases in these same five files through the same test-only pattern.

Re-run:

`rg -n "Start returned before.*Stop|stop already in progress" <the-five-files>`

Any remaining occurrence must be tied to a named owner-specific requirement; unsupported concurrency probes are
removed rather than rewritten with sleeps, scheduler yields, or wider timeouts.

No production or sister-repository correction propagation is expected because no production defect or outward
contract delta was measured.

## TDD and verification plan

RED record:

Preserve the parent reproduction showing graph-embedding's line-218 assertion first and the later 30-second drain
timeout second. Do not create a sleep-based synthetic RED.

Focused deterministic GREEN:

`go test -race -count=100 \
  ./processor/graph-embedding \
  ./processor/graph-clustering \
  ./processor/graph-query \
  ./processor/graph-index-temporal \
  ./processor/graph-index-spatial \
  -run '^TestLifecycleOwnerDrain'`

Five-package full unit/race:

`go test -race -count=1 \
  ./processor/graph-embedding \
  ./processor/graph-clustering \
  ./processor/graph-query \
  ./processor/graph-index-temporal \
  ./processor/graph-index-spatial`

Five-package integration/race through the canonical wrapper:

`scripts/run-integration-tests.sh \
  ./processor/graph-embedding \
  ./processor/graph-clustering \
  ./processor/graph-query \
  ./processor/graph-index-temporal \
  ./processor/graph-index-spatial`

Repository gates:

- task lint
- go test -race -count=1 ./...
- scripts/run-integration-tests.sh
- go build ./...
- task schema:generate
- git diff --exit-code schemas/ specs/
- go test ./test/contract/...
- git diff --check
- openspec validate --all --strict --no-interactive

Hosted proof:

- all required CI jobs green on the exact corrected SHA;
- required Test job green without rerun;
- logs contain no "Start returned before Stop serialized" and no drain timeout;
- exact candidate identity is selected only after the correction is merged and the tree is clean.

Release proof:

- this test-only correction requires no new E2E tier because runtime, wire, config, and persisted state do not change;
- the eventual next-tag candidate still runs every existing release-candidate-proof gate;
- any earlier exact-SHA CI, review, or candidate evidence is invalidated when these test files change;
- a red full race/integration gate blocks candidate selection and tag authorization.

## Adopter and migration result

External adopters change nothing.

They receive:

- no API or behavior change;
- no new timeout or configuration knob;
- no migration instructions;
- no fresh-storage implication;
- no compatibility shim.

The framework continues observing native drain completion rather than asking adopters to predict callback timing.

## Decision skills

No canonical decision skill triggers:

- kv-or-stream: no communication path or storage primitive changes;
- orchestration-check: no rule, workflow, component orchestration, or multi-step runtime behavior changes;
- new-payload: no payload type or registry change;
- query-pattern: no remote operation or query adapter change.

## Non-goals

- no production lifecycle refactor;
- no Client.Close or nats.go behavior change;
- no drain-timeout tuning;
- no TestClient redesign;
- no CI package-parallelism ruling for #736;
- no repair of stale #1048 conformance/task truth in this implementation slice;
- no new exported test helper or scheduler hook;
- no sleep, Gosched, retry-until-green, or probabilistic assertion;
- no sister-repository mutation;
- no tag-readiness claim before exact-candidate proof.

## Owner rulings requested

Owner acceptance: on 2026-08-23, the owner accepted #1054 "as planned," approving R1-R5 below without deviation.

R1. Accept Option C as the bounded five-file test correction.
R2. Confirm unsupported concurrent Start and second-Stop probes are removed rather than retained as owner policy.
R3. Confirm the release guard propagates to every unguarded blocking callback in the same five files.
R4. Confirm no production, Client, timeout, CI-parallelism, spec, ADR, or sister-repository change belongs here.
R5. Confirm focused repeated race plus full canonical integration/CI is sufficient change-specific evidence,
while the eventual tag still requires the complete release-candidate-proof contract.

End design draft.
