# Async-test reliability repair plan

Status: advisory draft after independent INVENTORY PASS, including the adopter-seam addendum. Baseline:
`3dc4ccbef32e87096c6d998fde7e76e896cf2f3c`. The accepted inventories and reviews accompany this text unchanged.
No implementation, test runs, or GitHub mutations have occurred.

The evidence supports a recurring design problem: some tests predict scheduler timing, cleanup can conceal the original
failure, and broad runner timeouts become the first effective protection against a deadlock. It also establishes
production lifecycle defects. Treating all of these as “flaky tests” would hide bugs the tests correctly expose.

The existing foundation is substantial: causal synchronization policy, bounded cleanup, a canonical NATS substrate,
an integration runner, policy guards, and examples using `testing/synctest`. The immediate work should repair and
enforce those foundations. An entirely new async testing framework is unnecessary for the measured problems.

## Evidence and options

| Measured premise | Implication |
|---|---|
| #1397 retains four 3-second pipe waits; #1290 already repaired the same timing assumption elsewhere. | Local repairs have not prevented recurrence of the pattern. |
| #1283 identifies orphaned runtime fences and receives after cancellation. | A timeout context cannot protect callers when production code ignores it. |
| Run `36343596313` spent 1,200 seconds in `internal/boot`; its stack shows cron settlement under `Stop(context.Background())`. | Fix the observed cron path and test cleanup independently of proving the runtime-fence mechanism. |
| Testing policy already requires diagnostic waits and bounded cleanup; the runner still permits 20 minutes per package. | Policy adoption and enforcement lag behind the stated contract. |
| Successful baseline CI took 13m34s; container counts and phase distributions were not measured. | There is substantial recurring cost, but no defensible percentage speedup or universal new timeout yet. |
| #736 records full-suite `-p 1` at 23m37s versus `-p 2` at 12m45s. | Blanket serialization would increase the measured waste. |

Options considered:

- **Continue rerunning or waive individual PRs.** Lowest immediate implementation cost; preserves the recurring wall-clock
  loss and leaves known required-job failures unresolved. Reject as the repair strategy.
- **Widen waits or replace every local wait with the package deadline.** Can suppress scheduler-related false failures,
  but missing signals still consume minutes. Reject as a complete fix.
- **Extend existing production owners, test helpers and guards.** Requires several bounded changes and causal regression
  evidence; addresses the demonstrated mechanisms without another abstraction layer. Recommended.
- **Build a generic async framework and migrate the suite.** Large migration surface, incomplete helper census, and delayed
  relief. Defer; introduce a shared primitive only if implementing these repairs establishes a specific unmet need.

## 1. Repair the current blockers before another full validation cycle

Keep #1397 and #1283 as separate implementation owners and review boundaries within this first work package.

For **#1397**, preserve the test’s actual assertion: the runner retains its lock until its owned pull process has been
terminated and reaped. Remove the four scheduler-latency assertions. Use the existing rendezvous and single-waiter
ownership to observe progress, terminal child failure, and EOF. Retain a separately declared containment deadline;
inheritance of the 20-minute package timeout alone does not satisfy this repair.

A child exit or explicit error is immediately actionable. A live, silent child cannot be distinguished from a slow
child without a deadline or another observation. The design must state that containment bound and its measured basis,
rather than claiming that silence proves a deadlock.

For **#1283**, repair production terminal ownership through its existing rule implementation. Force the final-drain versus
fence-admission window deterministically. Separately establish and repair the cron settlement failure captured in the
PR #1404 stack. Do not declare that CI incident resolved solely because a runtime-command regression passes.

The current component-lifecycle specification supplies the invariant: a running component’s `Stop(ctx)` is caller-bounded
(`openspec/specs/component-lifecycle/spec.md:34`). A finite caller context must obtain a finite bound; existing APIs do
not reject every deadline-free context. Preserve the specified distinction between successful completion and deadline
failure, including the absence of a portable later-rejoin guarantee.

The owning PR #1404 session should correct its `startedRuleProcessor` cleanup after resumption: fresh finite terminal
context, checked Stop error, and component shutdown before substrate teardown. That correction limits amplification;
it does not excuse an underlying production hang.

Use owner-local gates and existing `synctest` patterns for in-process scheduling. Where exercising broken production
code can block the test process despite cancellation, contain that regression in a test subprocess with capture,
termination and reaping. A goroutine wrapper that times out while leaving the blocked work alive is not containment.

| Acceptance case | Required evidence |
|---|---|
| Healthy rendezvous delayed beyond the former 3-second threshold | Correct lock/reap assertion still passes. |
| Child exits before its expected signal | Named phase and exit evidence appear without exhausting the containment window. |
| Child remains alive but never signals | Declared bound produces diagnostics; owned processes are terminated and reaped. |
| Rule final-drain/fence race | Causal schedule fails old production behavior without waiting for the package alarm. |
| Observed cron settlement path | Separate causal regression discriminates its repair; no attribution by resemblance. |
| Cleanup failure | Failure remains visible, substrate ordering is preserved, and no successful-cleanup claim is fabricated. |

Use focused race checks while iterating. Then run the required canonical package/full gates once the candidate is stable.
Preserve RED/GREEN or mutation evidence with restoration checksums. Repeated green stress runs may supplement these
controls; they cannot replace them.

## 2. Establish copyable patterns and targeted regression protection

Keep this PR focused on the repaired tests. Demonstrate the existing discipline through production-relevant assertions:

- In-process scheduling and retries use causal gates or virtual time.
- Broker behavior uses the real broker and observed consumer/readiness state.
- Observation waits report the condition, elapsed time, last value/error and resource identity.
- Cleanup has an explicit owner, finite terminal authority, checked outcome and correct substrate ordering.
- A timeout result and completed cleanup are separate observations; returning while owned work remains is not a clean shutdown.

Add targeted regression protection where the repaired behavior supplies a precise rule. Preserve RED/GREEN or controlled
mutation evidence; repeated stress passes are supplementary. Update the canonical testing guidance only where these
repairs expose an ambiguity, and provide examples future tests can copy. The existing policy already expresses most of
this discipline; another policy document is not the remedy.

The repository-wide typed census, cleanup guard and debt migration remain **#1064**. Its historical 517 matches and
295 cleanup sites are search populations, not current confirmed defects. They are not acceptance criteria for this PR.
Existing `testutil.WaitForMessage` and `WaitForMessageCount` remain in the inventory; neither absence nor universal
suitability was established. **#1349** retains ownership of shared KV fake fidelity, without replacing real-broker evidence.

Named schedule examples address the identified races because the obligations are specific lifecycle orderings. This
plan introduces no new input grammar or state-machine model requiring a new property-testing framework. A changed
implementation with additional state-history obligations must still apply the existing PBT decision at its own design.

## 3. Record follow-up gaps without expanding this PR

Record obvious gaps against their existing owners as this work encounters them:

- **#1293:** cheap-gate ordering, duplicate local full-suite execution, and clearer runner progress/failure evidence. Retain its rc.1 placement.
- **#1064:** repository-wide typed cleanup census and regression guard.
- **#736:** container-start pressure, churn and measured resource optimization.
- **#1349:** shared KV fake fidelity and consolidation.

Each note should identify the observed source or failure, its consequence and the owning issue. These are follow-up
records, not acceptance criteria for this PR. Do not add a generic testing framework, repository-wide cleanup, telemetry
project or pipeline redesign. #1222/PR #1406 retains E2E required-check evidence; #1287 retains performance calibration;
#1317 retains actual bind-failure handling.

## Scope, dependencies and completion

The planned PR focuses on the observed test failures: #1397’s synchronization assumptions, the identified unbounded
cleanup, and clear testing patterns demonstrated by their repairs. Use causal synchronization, checked cleanup outcomes,
correct resource ordering and bounded failure containment. Add targeted regression protection where the repaired
behavior supplies a precise rule; leave the broader census and guard to #1064.

**#1283 remains a production correctness dependency with a separate fix owner.** Test changes must expose that defect
honestly. A finite cleanup context cannot preempt a production receive that ignores cancellation, and subprocess
containment cannot establish successful production shutdown. Keep the observed cron settlement failure distinct from
the runtime-command fence defect until causal evidence connects them. Package 1's production-fix description is the
required dependency outcome, not authorization to absorb a runtime redesign into the test PR. Coordinate the boot
fixture repair with PR #1404's existing owner; paused sessions have not been transferred or resumed by this audit.

Owner direction is to complete this focused repair and establish repeatable testing discipline. Broad runner improvements
remain follow-up work under #1293; no scheduling change or general runtime redesign is authorized by this scope.

Completion requires discriminating regressions for the repaired tests, observable bounded failure handling, checked
cleanup and resource ownership, focused race verification, and the required final gates. Any unresolved production
dependency remains explicit; a green rerun does not waive it. Implementation follows independent design review and
acceptance of this narrowed plan.
