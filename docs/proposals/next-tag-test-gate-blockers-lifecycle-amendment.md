# Next-tag test gate blockers lifecycle amendment

## #1062 lifecycle-lane design amendment

### Checkpoint

- Accepted inventory: `docs/proposals/next-tag-test-gate-blockers-inventory.md`.
- Accepted inventory SHA-256:
  `1c8c5a6e99085c3f4c70306f3dfa58d85d5998dd2f42d7ed44893c61fd02b880`.
- Inventory review: `INVENTORY PASS`.
- The owner selected the corrected lifecycle direction:
  - controlled shutdown keeps Start authority live through Stop;
  - accepted-parent cancellation is an abort lane;
  - abort does not promise orderly native drain, but a separately bounded Stop must join and finalize consistently;
  - no lifecycle signature or interface redesign.
- This amendment remains advisory until independently reviewed and accepted at its exact hash.

### Owner acceptance

The owner accepted the independently reviewed amendment on 2026-08-23 at exact SHA-256
`82c02d41468988987d159cfee3b758b038c39151b13155cd10b393aa9be1f307`.

### Measurable premises

| Premise | Measurement |
|---|---|
| Readiness tests still cancel Start authority before Cleanup | `processor/rule/readiness_integration_test.go:63-64,110-111` |
| Processor Stop is registered later through Cleanup | `processor/rule/readiness_integration_test.go:80-90,138-148` |
| Cleanup is LIFO | Go `testing.go:1293-1295,1630-1660` |
| Start derives all Rule work from its accepted parent | `processor/rule/processor.go:875-885,938-973,999-1029` |
| KV Watch binds native lifetime to that context | pinned `nats.go/jetstream/kv.go:1304-1319` |
| Controlled Rule cleanup drains with live callback authority | `processor/rule/processor.go:1289-1300` |
| Main entity cleanup normalizes prior native closure | `processor/rule/processor.go:1302-1307` |
| Hot-reload Stop returns raw watcher errors | `processor/rule/kv_config_integration.go:220-240` |
| Controlled and canceled-parent cases are distinct portable lanes | `component/lifecycle_test_suite.go:63-84` |
| Rule lacks the shared accepted-parent proof | standard-suite adopter search in the accepted inventory |
| Failure is deterministic and bounded | focused real-NATS run failed 20/20; independent rerun reproduced both tests |
| Borrow cancellation is not the new blocker | unchanged white-box proof passed race count100 in 1.508s |

### Options considered

#### Option 0: do nothing

The readiness tests continue selecting the abort lane accidentally. Cleanup produces native errors, and the Rule owner
remains unproved against the accepted-parent portable requirement.

#### Option 1: correct only readiness teardown

This restores the tests' intended controlled lane and removes the immediate gate failure. It leaves the independently
reproduced Rule abort-cleanup defect unresolved.

#### Option 2: normalize production errors without correcting the tests

This may hide the readiness failure while leaving the tests in the wrong lifecycle lane. It also risks suppressing an
unclassified native failure.

#### Option 3: two explicit slices

1. Correct the readiness tests to execute controlled shutdown.
2. Independently prove and narrowly correct Rule abort cleanup, changing only causally established prior-closed native
   handling and lifecycle truth.

This is recommended. The slices have separate rollback boundaries.

#### Option 4: redesign lifecycle interfaces or increase bounds

No evidence requires another context parameter, retained context, timeout knob, retry, longer Stop budget, generic
lifecycle wrapper, or production dispatcher. This is rejected.

### Slice A: readiness tests use controlled shutdown

For each Rule readiness test, registration order SHALL be:

1. `NewTestClient` registers `TestClient.Terminate`.
2. Create the finite Start-parent context and register its cancellation with `t.Cleanup`.
3. Start the Processor.
4. Register bounded Processor Stop Cleanup last.

Testing Cleanup LIFO then executes exactly:

```text
Processor.Stop -> cancel accepted Start parent -> TestClient.Terminate
```

At Processor Cleanup entry:

- assert that the accepted Start parent is still live;
- assert that TestClient/NATS is still live;
- report either ordering failure without calling `FailNow`;
- still attempt Stop using a fresh five-second terminal context;
- assert the Stop result with Rule-owner attribution.

The test explicitly names this the controlled-shutdown lane. Five seconds remains containment, not a performance SLO.
No production file changes in this slice.

#### Slice A RED/GREEN

RED is the current post-Terminate-removal state:

- retain `defer cancel()`;
- add the Start-authority-live Cleanup assertion;
- the focused tests fail promptly before Stop and return the observed native cleanup error.

GREEN:

- register parent cancellation between TestClient cleanup and Processor cleanup;
- both Start parent and NATS remain live at Stop entry;
- the bounded Stop succeeds.

Forced omission in an isolated copy restores either function `defer cancel()` or registers cancel after Processor
Cleanup. The selected readiness test must fail the exact Start-authority ordering assertion without waiting for the
package timeout.

### Slice B: Rule accepted-parent abort cleanup

Add a dedicated real-NATS owner test independent of readiness assertions:

`TestIntegration_RuleStopAfterAcceptedStartParentCancellation`

It SHALL:

1. create the real TestClient first;
2. create a cancellable Start parent;
3. initialize and Start a Rule Processor using an entity watcher and the normal hot-reload owner;
4. establish successful Start and live NATS;
5. cancel the accepted Start parent before invoking Stop;
6. invoke Stop with a fresh five-second context;
7. require bounded completion and nil error;
8. prove NATS remained live throughout Processor cleanup;
9. terminate TestClient only after Stop;
10. use no sleeps or elapsed-time classification.

This is the abort lane. It does not assert orderly drain, message delivery, or callback completion after parent
cancellation. It asserts that continuing work ends and bounded Stop consistently joins and finalizes exact resources.

#### Exact phase attribution

Before changing error interpretation, preserve phase identity for every touched error:

- entity watcher terminal Stop;
- Rule hot-reload ConfigManager Stop;
- ConfigManager's exact native watcher Stop;
- any late watcher acquired after cancellation.

Use private wrapping with `%w`; add no exported error type. The baseline real-NATS RED must identify the exact owner
producing `ErrBadSubscription` or `ErrConsumerNotFound`.

If the attributed failure is not an already-closed exact native watcher outcome, stop implementation and reopen design.
Do not suppress another error to obtain GREEN.

#### Narrow production correction

If causal RED confirms prior-closed watcher outcomes, introduce one private Rule-package terminal-watcher primitive. It
SHALL:

- call Stop on the exact watcher once;
- treat `nats.ErrBadSubscription` as successful prior closure;
- treat `jetstream.ErrConsumerNotFound` as successful prior closure only when reproduced by accepted-parent
  cancellation;
- return every other error unchanged;
- be used by Rule terminal watcher cleanup and ConfigManager terminal/rollback watcher cleanup so this fact has one
  interpreter;
- not hide runtime add, remove, or replacement failures;
- retain no context and launch no goroutine.

The correction means the exact native resource is already terminal, not that all NATS errors during shutdown are
harmless. ConfigManager and Processor retain exact phase wrapping for genuine errors after normalization.

#### Slice B unit proofs

Add private tests proving:

- nil watcher is harmless if the helper admits nil;
- `ErrBadSubscription` is normalized;
- causally confirmed `ErrConsumerNotFound` is normalized;
- an arbitrary sentinel error is preserved through `errors.Is`;
- Stop is invoked exactly once;
- ConfigManager and Processor retain owner-phase attribution for the arbitrary error.

### Lifecycle truth correction

The completed `align-standard-lifecycle-tests` change belongs to merged PR #1048. Its accepted R1-R8 tasks and
conformance evidence SHALL NOT be edited to carry this later clarification. Before the #1062 truth correction lands:

1. archive #1048's completed change without changing its accepted capability text;
2. establish its component-lifecycle capability as current truth if the archive workflow has not already done so;
3. create a fresh #1062 OpenSpec change with its own proposal, design, tasks, component-lifecycle delta,
   runtime-context-ownership delta, and conformance record.

The new #1062 component-lifecycle delta and `LifecycleComponent` GoDoc SHALL state, without changing signatures:

- `Start(ctx)` owns continuing work.
- During controlled shutdown, the caller keeps the accepted Start context live until bounded Stop returns.
- Ending the accepted Start context first is abort cancellation.
- Abort cancellation ends continuing work and may forfeit orderly native drain.
- A separately bounded Stop is still required to join and finalize owned resources.
- Confirmed prior closure of an exact native handle is terminal completion, not a cleanup failure.
- Genuine cleanup errors remain errors.

The new #1062 runtime-context-ownership delta SHALL state consistently:

- composition uses a separate bounded Stop context;
- controlled shutdown normally calls Stop before canceling the Start parent;
- unexpected Start-parent cancellation does not authorize invented replacement work authority;
- bounded Stop after abort observes and finalizes existing ownership only.

No context is retained, detached, defaulted, or recovered through a provider. The #1062 change does not claim
conformance until its implementation, focused owner proofs, current-spec projection, and strict validation are green.

### Adopter seam

Specific adopter: an external component caller composing `LifecycleComponent`.

They must know two facts:

1. Call bounded Stop while Start authority is live for controlled shutdown.
2. If Start authority ends first, still call bounded Stop, but treat the path as abort rather than orderly drain.

If they do nothing, parent cancellation ends continuing work; owner cleanup must still settle exact resources, but
delivery and drain guarantees are lost.

They find this in exported GoDoc, current capability truth, and the shared lifecycle suite. Owner violations produce
focused lifecycle-test failures rather than low-level unexplained native errors.

They should not know watcher types, consumer identities, NATS error sentinels, cleanup phase ordering, or timeout
internals. Those remain framework-owned.

### RED, GREEN, and forced omission

Slice B RED:

- add phase attribution and the isolated real-NATS abort test;
- run against unchanged cleanup interpretation;
- capture the exact attributed prior-closed error.

Slice B GREEN:

- add only the causally admitted terminal-watcher normalization;
- the abort test passes repeatedly;
- controlled readiness tests remain green;
- arbitrary native errors remain visible.

Forced omissions in isolated copies:

1. Restore raw native watcher Stop: abort test fails with the exact attributed prior-closed error.
2. Normalize an arbitrary sentinel: the preservation unit test fails.
3. Remove phase wrapping: the attribution unit test fails.
4. Cancel Start only after Stop in the abort test: a precondition assertion fails because the test no longer exercises
   the abort lane.

Restore every mutation and verify production checksums before continuing.

### Focused gates

```text
go test -race ./processor/rule \
  -run '^(TestRuleTerminalWatcherOutcome|TestConfigManager.*Terminal.*Attribution)$' \
  -count=100 -timeout=30s

go test -v -race -tags=integration ./processor/rule \
  -run '^(TestIntegration_RuleReadiness_(EmptyReplayIsAuthoritativelyNothingToDo|NonEmptyReplayReportsScope)|TestIntegration_RuleStopAfterAcceptedStartParentCancellation)$' \
  -count=20 -timeout=120s
```

Verbose progress is observed actively. Lack of progress beyond twice the measured per-test envelope triggers stack
capture and abort; the 120-second timeout is containment, not a waiting plan.

Then run normal repository gates and the canonical integration task. No `-p1`, retry-until-green, timeout increase, or
20-minute passive wait is admitted.

### Separate audit and guard work

Extend the already separate lifecycle-test population audit to classify:

- controlled-shutdown tests;
- accepted-parent abort tests;
- accidental lane mismatches;
- cleanup registration order;
- bounded versus unbounded Stop;
- substrate lifetime.

The audit must be AST/type-aware and package-at-a-time. It does not mechanically rewrite the existing textual census
and does not block the two narrow corrections once their focused proofs pass.

### Landing and rollback

Land two independently reversible changes:

1. #1062 controlled readiness teardown correction.
2. Rule accepted-parent abort conformance plus lifecycle truth correction.

Both are pre-tag because the second repairs an independently reproduced violation of the accepted portable lifecycle
floor.

No ADR, E2E scenario, exported helper, configuration, schema, wire contract, payload, subject, bucket, stream, or
lifecycle signature changes. The fresh #1062 OpenSpec change owns the capability clarification; completed #1048
artifacts remain immutable evidence.

### Canonical skills

No canonical decision skill triggers: no new communication path, payload, query surface, or orchestration behavior.

### Review gate

Materialize this amendment, record its exact SHA-256, and obtain independent `DESIGN REVIEW PASS`. Implementation must
not proceed on the production normalization until the reviewed amendment is explicitly accepted.
