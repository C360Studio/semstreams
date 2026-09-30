# Graph-query test cleanup design

Status: accepted by coordinator after independent DESIGN REVIEW PASS on 2026-09-30; implementation next.
Baseline: `714bb0fa`, following the accepted refresh at `80fab70ab6e9c2d8ab1afa460f95fea80ca46f20`.
Claim: #1433 / draft PR #1434. Production behavior and public contracts are outside this batch.

## Evidence checkpoint and premises

Preserve the accepted inventories by reference, without rewriting their historical text:

- `inventory.md`, SHA-256 `990e720cca7b45f62bd7400674bda662e39390ad7c9b38b65b5092517ab5eaa2`;
  independent INVENTORY PASS in `review/inventory-review.md`, checkpoint `fe424c96`.
- `inventory-refresh.md`, SHA-256 `a6e00de7f2382c715cb1dc6e29267ae343bd111e7c6b16edc8303cc9821480d3`;
  independent INVENTORY PASS in `review/inventory-refresh-review.md`, checkpoint `714bb0fa`.

| Measured premise | Evidence and consequence |
|---|---|
| Exactly 39 baseline identities in five files remain unchanged | Inventory B01–B39; refresh JSON comparison and five source hashes. Remove these exact identities only after replacement admission. |
| Global baseline is 273 legacy / 96 resolutions; none of the resolutions concerns graph-query | Refresh manifest comparison. This batch's expected remaining legacy count is 234, subject to unrelated concurrent changes. |
| Existing registrations follow fallible Initialize/Start work | Inventory problem statement and B ledger. Protect acquisition before those exits rather than merely replacing the final context. |
| The three setup helpers return unstarted components or NATS substrate | Inventory setup boundaries and typed caller sets 19/33/16. Their signatures need not change; no running-owner transfer API is needed. |
| Eight ordinary Stop sites and one accepted-Start/no-Stop case exist outside the ledger | Inventory ordinary-call table and `component_test.go:897`. Give them explicit dispositions below without changing their test intent. |
| Existing private owners already model the required shape | `component/lifecycle_test_suite.go:29`, `processor/graph-ingest/test_owner_support_test.go:11`, `processor/rule/test_owner_support_test.go:13`. Adopt that shape locally, with a concrete receiver. |
| Graph-query has resource-specific terminal order and contextless operations | Inventory native terminal section; `component.go:661`, `:672`, `:676`, `:686`, `:693`. Deadline supply cannot certify complete joins or total wall time. |
| The merged KV repair supplies no graph-query join guarantee | Accepted refresh dependency section. Its full-KVStore terminal delivery drain is not a fixture finalizer or a replacement for graph-query WatchAll/view ownership. |

The refreshed inventory covers the surface and adopter seam. This design adds only private test support for present
consumers. No new durable state, communication path, payload, query access or production orchestration is introduced;
entity-or-bucket, kv-or-stream, new-payload, query-pattern and orchestration-check do not trigger.

## Options and recommendation

| Option | Benefit | Cost or limit |
|---|---|---|
| Adopt the existing concrete private fixture-owner pattern in graph-query tests | Covers setup exits, checked results, once-only attempts and Start/substrate order together | Five fixture files plus small local support/proofs; explicit treatment of lifecycle probes |
| Write a bounded cleanup closure at every site | No new private type | Repeats the same ownership protocol across 39 sites and makes error, cancellation and early-exit consistency harder to review |
| Extend/export the shared lifecycle owner or change all unstarted helper signatures | One cross-package abstraction or automatic helper registration | Shared owner is private; exporting it adds an unnecessary framework contract. Helper changes affect pure-handler/metadata callers and registered Cleanup is too late for live Start authority |
| Do nothing | No source churn | Retains 39 exemptions and the measured setup escapes; does not satisfy this batch |

Recommend the first option. It adopts an established pattern and does not establish a reusable framework primitive.
Do not route ordinary fixture cleanup through production RollbackFailedStart: that primitive owns a different phase.

## Scope and file ownership

Modify only the five ledger files, the exact manifest entries, and small package-private test-support/proof files:

- `processor/graph-query/attack_test.go`
- `processor/graph-query/batch_passthrough_integration_test.go`
- `processor/graph-query/component_integration_test.go`
- `processor/graph-query/component_test.go`
- `processor/graph-query/summary_bucket_late_attach_integration_test.go`
- New `processor/graph-query/test_owner_support_test.go`, with focused proof files as needed.
- `test/testinfra/cleanup_baseline.json` and this change's specification/evidence/task records.

Do not change createTestComponent, createTestComponentWithMockClient or setupTestNATS signatures or unrelated callers.
Do not add provisional-transfer methods: no running helper currently transfers ownership in this population.
The package's existing lifecycle_owner tests, embedded-server cleanup debt and supervisor-only sibling fixtures remain
separate measured surfaces. Their deliberate nil, repeated Stop, failed-Start retry and concurrent probes stay intact.

## Private owner and authority protocol

Add one unexported graphQueryTestOwner with a concrete `*Component`, private Start cancellation, attempted state,
and concrete Stop/terminal-context results following the shared owner's existing separation. It stores no context,
provider, generic Stop callback or interface-dispatched component. The lexical test goroutine owns it; it is not a
concurrent lifecycle executor.

Use the same small operations as the existing owners: construction, one Start-child derivation, explicit terminal
attempt and lexical finalization. Pass operation and Start contexts explicitly into terminal operations. A boolean
explicit-abort expectation may suppress only the controlled-path requirement that Start authority is still live;
it must not discard concrete errors or terminal expiry. Preserve the concrete result separately from added diagnostic
causes so tests can distinguish native error, expired terminal authority and unexpectedly ended work authority.

Each owning case obtains an operation context from its own `t.Context()` and immediately defers its cancellation.
Use the existing five-second unit convention; use 60 seconds for formerly Background integration operation scopes,
created after NewTestClient setup. Keep narrower request and lifecycle-probe contexts and existing readiness budgets.
These are finite supplied bounds, not measured latency claims or permission to enlarge an existing wait.

Immediately after acquiring the concrete component, create its owner, derive its private Start child and install
lexical finalization before Initialize, Start, hook assertions or other fallible setup. The intervening owner/context
construction must perform no fallible work. For direct production constructors, establish ownership as soon as the
concrete component exists; no new constructor or factory wrapper is needed. Configuration work before acquisition
has no component to finalize.

Derive the Start child exactly once per fixture. Pass that same child to the first Start and any deliberate duplicate
Start call. A rejected later Start must not replace the stored cancellation or revoke the first accepted authority.
A deliberately short Start probe derives from its existing short parent beneath the separate operation context.
Canceled/expired request contexts remain separate from component Start authority.

The terminal attempt performs these steps in order:

1. Refuse a second ordinary owner attempt; mark attempted before entering real Component.Stop. An error or panic
   never authorizes implicit retry.
2. Construct a fresh `WithTimeout(WithoutCancel(operationCtx), 5*time.Second)` context immediately before Stop.
   Defer its cancellation. This is terminal authority only; never pass it to Start, Watch or continuing work.
3. Call the concrete Component.Stop synchronously. Retain its result and inspect terminal-context expiry separately.
   On a controlled path, also report ended operation/Start authority as a failed controlled-cleanup premise.
4. Cancel only the private Start child after the synchronous attempt, including error exits. Keep the operation
   context usable for post-Stop assertions until the caller's earlier deferred cancellation.

The deferred finalizer calls this operation only if no concrete attempt occurred, reports failure with `t.Errorf`,
and preserves any original test failure. It does not use Fatal, hide errors, retry or launch background Stop work.
Explicit terminal fences assert the joined result before allowing the next phase; later finalization does not repeat
that attempt. An explicit abort remains a separately named test expectation, not a rule inferred from any cancellation.

Component finalization is lexical, including B12/B13/B38, whose registered component Cleanup becomes lexical.
Go cancels `t.Context()` before registered Cleanup; keeping those registrations would turn normal exit into abort.
The resulting order is component terminal attempt, private Start cancellation, operation cancellation, then existing
NATS TestClient registered cleanup. The existing no-op setupTestNATS callback may remain; never add a second
TestClient.Terminate or client/container owner.

A failed Start may already have performed production rollback. If it retained cleanupPending, the fixture's one
explicit Stop is allowed by that owner-specific contract. A failed fixture Stop still suppresses implicit retry.
No running-generation rejoin promise is added.

## Exact population and deliberate probes

B01–B39 all receive the owner before Initialize/Start. Existing query, wire, result, routing and readiness assertions
remain. Remove only their old terminal registrations. A test's explicit API calls remain visible in its body.

| Adjacent case | Disposition |
|---|---|
| attack_test.go:190, repeated fresh lifecycle | Own each of ten instances inside an iteration-local lexical scope; checked explicit Stop; a setup or terminal failure stops later admission. Do not defer all ten Stops until the outer test returns. |
| attack_test.go:217, accepted Start cancellation | Keep explicit cancellation before Stop and current mock-specific nil-result assertion. Use fresh terminal authority and an explicit abort expectation; add early-exit ownership. |
| attack_test.go:444, health after disconnect | Keep the health assertion and order; make the explicit terminal result and expiry checked, with fallback ownership installed before setup. |
| component_test.go:412, Stop success | Preserve explicit controlled Stop and its result assertion; terminal attempt consumes finalizer ownership. |
| component_test.go:421, Stop before Start | Preserve the no-action API probe; no Start is inserted. Use finite terminal authority and verify its result. |
| component_test.go:883, accepted Start cancellation | Preserve cancellation before Stop and current concrete nil-result expectation; do not relabel it controlled shutdown. |
| component_integration_test.go:70, lifecycle sequence | Preserve production construction, Initialize/Start/health/explicit Stop; use its existing NATS fixture for the native ordering proof below. |
| component_integration_test.go:394, orderly generation fence | Keep checked Stop before exact generation revocation, empty-cache and no-retry assertions. Keep operation authority live afterward; no second Stop. |
| component_test.go:897, short Start/no Stop | Retain the 100 ms Start input and existing successful-or-context-error expectation. Add ownership before Initialize and a checked terminal attempt on either return path. Intentional Start expiry is an abort expectation, not permission to omit Stop. |

For the cancellation examples, observe the captured runtime completion channel through a bounded causal wait instead
of their existing propagation sleeps when claiming completion. Capture it before Stop clears lifecycle handles.
Keep aggregate goroutine-count assertions as supplementary observations; they do not replace that completion signal.
Do not undertake a broad sleep or timeout rewrite in unrelated request/handler tests.

B21/B31 retain canceled/timed-out request inputs while Start remains live. B36 retains the duplicate-Start rejection
and now explicitly verifies its first Start context remains live before finalization. B01/B02 retain ownership of
all 100 test workers: observe every worker result before component finalization and bound failure observation without
abandoning test goroutines or invoking Fatal from them. Do not introduce a new worker scheduler.

## Invariants and specification home

The governing home is `test-cleanup-policy / Lexical ownership of lifecycle test fixtures` at current spec line 213,
with its setup-exit, caller-exit, explicit-fence and deadline-versus-completion scenarios. The accompanying MODIFIED
delta retains that requirement and adds two test-fixture clarifications; it creates no production lifecycle promise.

- Every in-scope acquired component has terminal ownership before subsequent fallible setup can exit.
- Each ordinary owner makes at most one concrete Stop attempt, including errors and panics; native deliberate retry
  probes outside this owner retain their existing contracts.
- Controlled Stop receives fresh finite authority while accepted Start authority is live; owner cancellation and
  substrate cleanup follow the synchronous attempt.
- Concrete error and expired terminal context remain distinguishable and observable in the test outcome.
- Explicit successful fences retain operation authority and prohibit implicit finalizer retry.
- Rejected duplicate Start preserves the original accepted Start authority.
- A short-lived accepted Start still has an owner and a separately bounded terminal attempt.

Production ordering remains governed by component-lifecycle and runtime-context-ownership, not by private helper
implementation. Finite supply cannot interrupt graphview.Stop, native contextless watcher Stop or LLM Close.
Stop can return its runtime-wait deadline without complete join. Running Stop can terminalize and clear handles after
an error. Report those observations honestly; do not fix them through a hidden retry, widened bound or production edit.

## Test-first proof selection

PBT decision: named examples are sufficient for this bounded fixture protocol. Its relevant histories are acquisition
exit before initialization, exit after accepted Start, controlled finish, explicit successful/failed fence, rejected
duplicate Start, request cancellation with live Start, intentional Start cancellation/expiry, concrete Stop error and
terminal expiry. Exercise these histories deterministically, including finish after attempted Stop and repeated fresh
instances. There is no grammar or arbitrary operation-sequence space being changed; no broad generated harness is
proposed. Existing native lifecycle probes retain their independent obligations.

The oracle comes from the cited requirement and native completion contracts, not private owner field values alone.
Use real Component.Stop with existing component-local seams; never substitute an interface fake Stop merely to prove
the wrapper calls it. Separate the following evidence:

| Obligation and plausible violation | Lowest sufficient observation |
|---|---|
| Setup exit loses cleanup because ownership is registered late | Selected child test exits fatally before Initialize/Start; a later substrate marker observes the real component terminal transition before testing Cleanup. |
| Caller assertion or concrete cleanup error is hidden | Selected child accepts Start through the existing mock seam, then fails intentionally; a contextless test LLM Close returns a sentinel. Parent requires both original and terminal diagnostic witnesses, expected child failure and substrate-after-terminal order. |
| Explicit failed fence admits later work | Child executes the real Stop with sentinel failure; parent requires failure witness and absence of the next-phase marker. |
| Failed attempt is retried | A real Component with retained failed-Start cleanup and a counting sentinel Close sees one invocation across explicit failure and finalization. Native retry capability makes a missing attempted guard observable. |
| Controlled cleanup cancels Start early | Real Component cleanup's existing private cancellation/Close seams observe the accepted Start parent still live; after return the owner's Start child is canceled while operation authority remains live. |
| Terminal expiry is ignored when native cleanup returns nil | In testing/synctest, a controlled contextless test Close returns nil after the five-second terminal deadline. Require raw nil plus observable terminal-expiry failure; no real-time five-second wait or stranded resource. |
| Fresh terminal authority accidentally inherits ended operation authority | End operation authority deliberately; concrete cleanup still receives a usable terminal attempt, while the result reports ended controlled authority. Do not describe this as controlled success. |
| A deadline check passes without real completion | Extend existing native cases as described below; require actual callback, runtime and exact-view completion observations. |

For the child proofs, reuse the existing graph-ingest selected-self-execution pattern. Run the current test binary
with an exact test selector/count and explicit mode; reject unknown modes or conflicting selectors. Use one owned
process and synchronous Wait/CombinedOutput, a finite parent context, expected failure exit, required scenario markers,
and rejection of timeout, panic, race report or unintended test selection. No shell, nested go build, recursive suite,
new TestMain, production hook or generic process framework is needed. Group or split cases so measured race execution
respects the existing five-second top-level ceiling. The existing graph-query package has no TestMain.

Every controlled test resource has an early-installed release/join path. Timers contain failures; channels or observed
state establish phase entry and completion. Synthetic state tests prove the selected cleanup boundary, not successful
real-NATS Start. A compilation failure while introducing support is not the required behavioral red.

Before replacing the old registrations, run at least one selected ownership/assertion-exit witness against their
current late-registration/unbounded shape and retain its intended behavioral failure. If additional checks first
require the new owner, establish their baseline and use the named mutations below; label that sensitivity separately
from the pre-change red. Do not claim a scaffold-only witness covers every converted caller.

## Native proof without another container

Use the existing TestIntegration_ComponentLifecycle fixture and real production constructor/Start. Through the
existing subscribeForRequests seam, wrap one real responder callback with entered/release/completed signals; do not
replace native Subscription.Drain. Admit a request, begin the owner's Stop in a joined test goroutine, and observe
Component.stopping under its existing lifecycle lock (or its completed result) before checking the pending callback.
Require callback authority remains live while admitted work is held, then release it and observe callback completion,
Stop return and the captured runtimeDone channel. Require operation authority remains live and a post-Stop NATS
operation succeeds before registered infrastructure teardown. An early completed Stop is failure, not readiness.

This test owns and joins both the request task, if one is spawned, and the Stop task. Its failure cleanup releases
callback gates first, joins the already-started terminal attempt and request, and only then allows outer fixture
finalization/substrate cleanup. The finalizer never races a still-running explicit Stop or launches another one.
Use existing bounded polling only for the lock-protected stopping observation; do not infer drain entry from elapsed
sleep. Preserve the existing lifecycle/health assertions.

The existing orderly-generation test retains its exact unpublication/no-retry fence. In B39, reuse its existing
attached view and summaryViewStopped hook to observe Stop of that exact view after the late-summary assertions,
plus captured runtimeDone before substrate teardown. No separate container is needed. These establish component,
callback and view completion at the exercised boundaries, not native SDK goroutine joining or consumer deletion.

Do not re-run #1435's expensive native counterexample as graph-query evidence. This batch has no production change
requiring a new E2E tier; current required repository gates remain mandatory.

## Mutation criteria, execution and limits

Targeted sensitivity is required: this changes enforcement against owned work remaining after test shutdown and
addresses omissions existing tests tolerated. Use bounded experiments with fixed assertions, cp backups and checksum
restoration under the developer/reviewer contract. Retain source revision, commands, test selection, baseline,
compiling mutant, intended assertion failure and restored baseline. Existing evidence may be cited only when it
covers the same unchanged check and fault.

Selected faults are late/removed lexical registration, Start cancellation before Stop, discarded concrete error,
ignored terminal expiry, and implicit retry after attempted Stop. The corresponding table oracles must detect them.
The native admitted-callback and exact-view observations challenge successful completion separately; no outer timeout
alone establishes detection. Stop if a mutant survives or faults the harness; strengthen the specific oracle and
re-establish baseline rather than broadening into an unbounded matrix. No repository-wide mutation score is sought.

Record actual executed cases, skipped bodies, child selection, race timings, native top-level/package duration and
cleanup observation costs. The planned warm target is under one second for an in-process proof; subprocess race
startup may cost more and must be measured against the existing ceiling. Native augmentation reuses current
containers; source duration constants are not performance measurements. Any native hang, unexpected cleanup error or
budget breach is new evidence to reconcile, not authorization to weaken assertions or change production here.

## Manifest, validation and handoff

After source freeze, run the existing cleanup guard. Remove only obsolete B01–B39 identities; preserve the other
234 legacy records and all 96 existing resolutions byte-semantically unless concurrent independently reviewed work
changes the baseline. Require zero new unapproved cleanup uncertainty/debt and no stale approvals. Do not approve
new helper paths wholesale. A new exact manual dependency question requires independent source review, not a design
assumption that an exemption will be granted.

Validation sequence: focused race proofs and affected ordinary cases; focused real graph-query integration through
`scripts/run-integration-tests.sh` with its canonical flags/host lock; exact guard reconciliation; required
`task check:push`; independent implementation review; archive/spec synchronization and narrow final review.
Use a selected runner -run expression for iteration, then the required complete gate. Do not run heavy gates while
another host owner is active. Record actual results and coverage limits; no test or mutation has run in this design
phase. Do not credit the #1435 waiver or its CI as this batch's validation.

The technical writer materializes this draft and companion spec/tasks, records exact hashes and the source baseline,
then submits them for independent pre-owner review. Routine private fixture choices remain within the continuation's
authorized scope after that review and coordinator acceptance. No new public or production contract needs an owner
ruling in this recommendation. Any discovered necessity for such a change reopens inventory/design at a named seam.

Open review questions are proof adequacy and measured cost, not policy delegation: can the selected deterministic
histories detect each named failure; do the native observers establish the claimed boundary without rescuing it;
and does actual race/native execution fit the existing budgets? These remain pending until implementation evidence.
