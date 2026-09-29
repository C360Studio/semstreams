# Shared lifecycle test cleanup — accepted design

Status: accepted for implementation on 2026-09-29; exact draft hashes and independent approval are retained in
`review/design-review.md`. This record is not a claim of completed verification.

## Accepted evidence checkpoint

The accepted inventory remains verbatim at
`32c01357:openspec/changes/shared-lifecycle-test-cleanup/inventory.md`, SHA256
`1d225f08a8c1148a16e130ef4d00ff04728b932467753086d31558bbf003c424`.
Its source baseline is `926677874711f888cb06c86584484996a001d9f6` over `bb98043a`.
Companion exposure JSON is SHA256 `555716939a4322dc8b4979f8e77a3cc6235e852bef58f96509a5816f42467a4d`.
This design incorporates that complete checkpoint by reference and does not replace or refresh its evidence.

## Options and recommendation

1. Extend the existing shared suite internally. Retain all exported signatures, centralize instance ownership,
   checked terminal invocation and finalization, and use the existing four adopters plus WebSocket support as proof.
   Cost: meaningful private refactoring and four exact manual-resolution reconciliations; no adopter API migration.
2. Reuse `internal/lifecyclecleanup.RollbackFailedStart` as the suite finalizer. It already supplies a detached,
   five-second terminal context and joins callback/context errors (`internal/lifecyclecleanup/lifecyclecleanup.go:17`).
   Its contract is specifically failed-Start rollback, and production owners already invoke it before Start returns
   (`processor/rule/processor.go:1013`). Applying it to every test exit would give controlled test shutdown the wrong
   semantic owner and would still not solve factory handoff, wrapper bypass, worker reporting, or duplicate Stop.
   Keep that production primitive unchanged; use its synchronous, bounded, error-preserving shape where appropriate.
3. Do nothing or patch only the rule cohort. This leaves the demonstrated shared early-exit and injected-wrapper
   defects in place and makes each adopter carry the same ownership burden. No improvement to the first shared batch.

Recommend option 1. No exported API, runtime behavior, generic lifecycle manager, Stop watchdog, or new shared package.
The present consumers are the four existing suite adopters and existing WebSocket injection/benchmark callers.
No new communication, payload, query or domain orchestration path is introduced; those decision skills do not trigger.

## Premises and boundaries

- The suite creates 259 instances normally, nine in short mode; factory calls occur inside workers
  (`component/lifecycle_test_suite.go:169`). Its public factory cannot report a structured construction error (:16).
- Initialize/Start failures skip Stop in workers (:174–182); NoLeaks logs and skips Initialize failures (:222–223).
  Fatal assertions can bypass later Stop because cleanup ownership is not installed at handoff (:52–54).
- ErrorInjection's finalizer (:455) hits the wrapper's early error return (:384–388), bypassing base cleanup.
- NATS cleanup is already registered by `NewTestClient` (`natsclient/test_client.go:863`); suite work must finish first.
- Go cancels `t.Context()` before registered cleanup, as the testing policy states
  (`docs/contributing/01-testing.md`, Synchronization and Contexts). Moving controlled Stop into t.Cleanup while
  deriving Start from that context would therefore create abort behavior, including on an assertion failure.
- A finite context cannot interrupt a synchronous implementation that ignores it. Neither this design nor the
  existing aggregate rule fallback establishes generic wall-clock containment or complete joining after a bound wins.
- Production rule/config/boot changes remain with #1404. #1417 package repair work remains open; #1293, #1411 and
  #1412 are unchanged. No claimed #1416 reproducer is invented.

## One private ownership path

Keep `LifecycleFactory`, `StandardLifecycleTests`, `TestErrorInjection`, `ErrorInjectingComponent`, and
`BenchmarkLifecycleMethods` signatures and wrapper fault semantics unchanged.

Implement private support within `component/lifecycle_test_suite.go`, factored only as needed for direct tests.
Use one small per-instance lexical owner: returned component, whether its concrete terminal operation has already
been attempted, and private cancellation/result bookkeeping. Keep operation contexts local; introduce no retained
context/provider on a production struct and no exported cancellation handle.

1. Invoke the factory in the invoking case/iteration. A nonnil return transfers that exact instance to the suite.
   Install its finalization defer immediately, before Initialize or an assertion can exit. A nil return is a named
   acquisition failure, never an object to initialize. Construction that panics, exits its goroutine, or never returns
   cannot transfer an undisclosed instance: pre-return resources remain the factory's responsibility. Do not recover
   arbitrary factory/production failures into success or claim the unchanged signature solves hidden acquisition.
2. The lexical owner lasts one subtest or one loop iteration, not an entire worker/batch. Its defer synchronously
   finalizes any still-owned component and then cancels the owned Start context. A nested defer cancels even if Stop
   panics. This executes on normal return and testing FailNow/Goexit, before the testing runner cancels t.Context()
   and before substrate t.Cleanup callbacks. Do not pair a deferred Start cancel with a later t.Cleanup Stop.
3. Create accepted work contexts at this test composition boundary from the current t.Context() (b.Context() in benchmarks), with an explicit
   finite work budget. Keep each alive through its controlled Stop. Retain the existing five-second terminal budget
   and use a five-second per-instance work budget initially; measure focused normal/race/integration costs before
   accepting those defaults. Deliberately nil, pre-canceled, pre-expired and explicit abort inputs remain test inputs.
4. Ordinary explicit Stop checks and fallback cleanup use the same private terminal-attempt bookkeeping. Pass a
   fresh finite Background child for terminal invocation, cancel it after return, and preserve both the returned
   error and an expired caller context. Record attempted before invoking concrete Stop, so panic or nonnil return
   does not cause a second implicit invocation. Cancellation of continuing work follows the returned Stop attempt.
5. A finalizer supplies one terminal attempt when none has been made: after Initialize-only, Initialize/Start error,
   rejected Start, nil contract probes, or an intervening fatal assertion. A failed Start may retain owner-specific
   cleanup; this one Stop is its existing permitted retry, not a running-generation rejoin. Once a concrete running
   Stop has been attempted, its nonnil result is reported and preserved; fallback does not invent another attempt.
6. The CompletedRepeatedStop case alone performs its explicit second contract call after the first successful Stop.
   Nil Stop probes and injected wrapper errors are contract operations, not evidence that the base owner finalized.
   Keep those paths outside the concrete-owner attempted marker. No portable concurrent Stop/restart promise is added.

Factories still owe fresh independent instances and safe concurrent invocation. Document that they must not call
Fatal/FailNow from a suite worker and must own resources until a component is returned. Existing rule factory uses
Errorf plus nil on failure. No new factory protocol is justified by this repair.

## Results, workers, injection and benchmarks

Controlled Stop expects nil; unexpected operation/cleanup errors fail with case/iteration/phase identity. An expired
Stop bound fails controlled cleanup even if the implementation returns nil. Preserve operation and cleanup errors
separately so one cannot hide the other. Cleanup diagnostics use nonfatal reporting while all owned work is joined.

The explicit AcceptedStartParentCancellation case still permits accurate native nonnil abort results and records
those results; it asserts the caller-context error if its Stop bound expires. This is not a blanket exemption for
NoLeaks or the normal worker path, which are controlled shutdown. Neither an accepted abort error nor a terminal
flag establishes complete joining. If a work context unexpectedly expires, report that failure and accurate abort
cleanup details rather than pretending the subsequent Stop was controlled success.

Each parallel iteration uses the same lexical owner and returns its errors. Workers never call require/FailNow.
On first reported failure, stop admitting further iterations without canceling Start authority of already-owned
instances; drain their results and join workers before the subtest returns. Report failures while results arrive,
not only after a WaitGroup wait. Use channel/WaitGroup ownership, not sleeps, and do not run Stop in an orphanable
helper goroutine. NoLeaks uses the same checked cycle and stops on failure after that instance is finalized;
aggregate memory/goroutine checks remain supplementary evidence, not a substitute for checked terminal outcomes.

ErrorInjection acquires ownership of the base component before wrapping. Check prerequisites instead of discarding
their errors. Keep Start live during the injected Stop operation; supply it a finite operation context. Its expected
injected error does not consume the base owner's terminal attempt. Finalization calls the base component directly,
with a fresh finite context, checks its result, and cancels Start afterward. The exported wrapper still returns the
configured error before forwarding; its testing behavior is not silently redefined.

Include `BenchmarkLifecycleMethods` in this shared-support repair: check factory/lifecycle results and finalize each
iteration through the same owner. Keep cleanup outside the timed region except where Stop/full lifecycle is the
operation measured. Abort a failed benchmark only after its iteration finalizer runs. Measure one-iteration smoke
execution; do not add stress/calibration runs or claim continuity of benchmark numbers across changed bookkeeping.

## Real rule adopter and #1416 disposition

Remove the rule adopter's duplicate 259-instance cleanup registry/fallback once shared ownership is proven. Preserve
one fresh TestClient, production `CreateRuleProcessor`, explicit platform, and configured ENTITY_STATES watcher.
The factory is unchanged in meaning and remains concurrent-safe; it needs no private cleanup policy or t.Fatal.
All returned instances finalize on the suite's lexical paths before the top-level test can reach NATS cleanup.
Add a private test-local forwarding probe around each real returned processor. Record nonnil Stop entry/return and
finite context supply while forwarding exact arguments and results; nil contract probes remain distinct. Keep the
observations concurrency-safe and register an assertion-only cleanup after NewTestClient: every returned instance
must have completed a nonnil terminal call before substrate cleanup. Derive expected population from actual factory
returns, so focused subtest selection works. This observer never calls Stop or substitutes production behavior.

Distinguish successful controlled Stop evidence from an abort result; never translate the latter into a claim that
every internal worker joined. Keep exact owner-local blocked-join evidence in existing rule tests. No exported
observation or production lifecycle hook is added.

Source already refutes the broad #1416 running-error-repeat premise: accepted running Start clears cleanupPending;
a returning running Stop becomes terminal even on error (`processor/rule/processor.go:1220–1241`). At the accepted pre-change checkpoint, NoLeaks called Stop
before cancelStart; those historical coordinates are retained in the immutable inventory. Failed-Start pending cleanup and early exits are
separate paths. Record this exact disposition plus the removed fallback's proof in #1416; do not say an intermittent
failure was reproduced or close it merely because code was simplified. Owner/coordinator decides issue disposition.

## Invariants and spec homes

The component-lifecycle ADDED requirements below are the prospective home for these invariants:

- Every nonnil factory handoff reaches its lexical terminal decision before its owner scope returns, including
  assertion/Initialize/Start failure; no undisclosed pre-return resource is claimed by the suite.
- Controlled cleanup calls concrete Stop with live accepted Start authority, then cancels it; explicit abort has
  separate expectations. A Stop result is never silently discarded or converted into complete-join evidence.
- A concrete terminal attempt suppresses implicit repetition; only the existing explicit repeated-Stop contract
  assertion adds a second running call. Injected wrapper errors do not consume base cleanup ownership.
- Workers transfer errors, not testing goroutine termination; owner scopes finish before worker/substrate teardown.

No runtime-context-ownership or test-cleanup-policy contract changes are needed. The former already supplies the
composition ordering; the latter continues to reject new debt, unresolved cleanup ownership and stale evidence.

## Bounded proof and costs

Use named table-driven owner fixtures with independent event/counter expectations for the finite transition set:
normal stop; Initialize failure; Start failure; nil factory; assertion exit; worker failure; unexpected Stop error;
cooperative Stop deadline; explicit abort; completed repeated Stop; each injected operation; benchmark failure.
Include a gated worker that needs live Start authority to drain, and compare concrete base/wrapper Stop counts.
Join fixture goroutines explicitly on success and failure. A cooperative deadline fixture observes ctx.Done(); it
must not be described as an ignored-cancellation or general hang-containment test.

Run actual fatal-assertion and benchmark-failure paths in a self-reexecuted test binary only where necessary to
observe a deliberately failing child without failing the parent. Child fixtures spawn no subprocess descendants;
use one Cmd.Wait owner, bounded overall/cleanup observation and EOF/early-exit diagnostics. Release all gates and
join the child on every path. File-backed output avoids output-copy ownership surprises. Reuse existing fixture
ownership conventions; do not introduce a generic supervisor package or a server/cache framework.

PBT decision: named table-driven cases plus explicit event-order assertions cover this small closed transition set.
There is no new input grammar, codec or unbounded state exploration; a random property harness would duplicate the
same cases. Each table expectation comes from the spec invariants above, not the helper's implementation.

Before implementation retain red evidence for at least early failure skipping base cleanup and injected Stop
bypassing it. Mutation proof removes finalizer installation, cancels Start before Stop, reintroduces wrapper cleanup,
and discards cleanup errors independently. Each must fail a targeted causal assertion promptly, not only the outer
process timeout. Use cp backups and before/after checksums; restore immediately and rerun the relevant green proof.

Measure focused race top-level tests against the policy's one-second target/five-second ceiling; split distinct
proofs rather than hide a long Cartesian subprocess matrix. Retain the existing five-second terminal budget for
real operations; private proof inputs may supply an already-expired context or causally gated cooperative deadline
without sleeping for five seconds. Bound a failing child with reserved cleanup time inside the top-level ceiling.
No budget increases or generic containment claims are authorized. Report observed cold/warm test and child costs,
real rule run duration, container count and cleanup outcome. One real rule container remains the allowed count.

## Exact guard reconciliation and verification

The selected shared/cohort sources have zero of the 334 legacy debt entries. Report that honestly; this repair is
not a promised reduction in 334. Eight adjacent WebSocket debt entries are not selected merely by file proximity.
The four exposed manual resolutions are wrapper Stop, testNilStopContext, testNoResourceLeaks and
 testParallelFreshInstances. Edits to their exact declaration dependencies can invalidate records even if a call
looks unchanged. Run the current guard, compare identities/ownership/dependencies, remove obsolete records, and
obtain independent source review for any necessary replacement. Never regenerate approvals or relax the guard.
A new helper-origin record requires its own accurate classification/evidence, not an automatic fifth approval.

Verification order: focused support red/green and race proofs; focused default adopters and WebSocket injection;
one-iteration benchmark smoke; current guard/source-evidence review; focused rule and graph-index integration
through the canonical host-owning runner; required check:push once implementation/review is ready. Coordinate the
shared host. No unrelated full stress or E2E is justified by this nonbreaking test-only API-preserving repair.
If new evidence shows an actual breaking behavior/API change, stop and select its required E2E evidence explicitly.

No new binding policy decision was requested. The mechanics and measured budget fit the existing scope;
independent design review and coordinator acceptance preceded implementation (`review/design-review.md`).
