# Rule test cleanup design

Status: draft for independent design review and coordinator acceptance within the owner's authorized test-only scope.
No implementation or merge approval is asserted.

## Accepted evidence and boundary

The accepted inventory checkpoint is `047be916111816f353d2998a06b40e24bcecb0e6` against source
`caa98f5acae60efbc669ad1e1795ab6e903abd42`. Incorporate these existing artifacts unchanged; do not rewrite their
historical phase labels or observations as target-state truth:

- `inventory.md`: SHA256 `ed8e5ce8c3c49416b5a0e3e2fe6477389a640881bfb4342253cb84535666ad73`.
- `review/ownership-ledger.md`: SHA256 `1b74ac431316ec3d945780bbab0768b0b6a3072c856960609d931623effbb1b6`.
- Independent INVENTORY PASS: `review/inventory-review-final.md`, source report SHA256
  `3246b5de8904b6ab5aa2755d0b1720b794d973bab01a0f76b7348566b2cd4a4f`.

The design covers B00–B23: 24 exact cleanup identities in ten files, and H01–H37: the 37 physical callers of the
four cleanup-owning helpers. These sets overlap. Their concrete runtime owners are Processor, CronScheduler and
graph-ingest Component. Unstarted revision trackers, metrics registries and mock buckets are support, not extra
runtime owners. Internal `rule` and external `rule_test` remain separate packages.

No production lifecycle, exported API, payload, communication path, durable state, workflow or query contract changes.
There is no new reusable framework primitive; the existing private lexical-owner shape is adopted locally. The
communication/orchestration/payload/query decision skills do not trigger for this test-fixture adaptation.

## Options and recommendation

| Option | Benefit | Cost and failure left behind |
|---|---|---|
| Do nothing; retain exact baseline debt | No source churn | Leaves setup ownership gaps, discarded terminal errors, canceled-Start cleanup and duplicate attempts measured in B/H rows. Does not complete #1428. |
| Replace Background Stop arguments with inline finite contexts | Small textual change; can satisfy a context-provenance check | Does not protect setup before registration, move testing cleanup before testing-context cancellation, separate phase-operation authority, or suppress error-path retries. Correctly adding all of those inline would repeat the full ownership protocol across roots/callers. |
| Adopt the existing private lexical-owner shape with concrete receiver types | Reuses #1419/#1424 ownership mechanics and makes explicit phase stops share the same attempt state | Requires caller edits and a small package-local duplicate for external tests; concrete owners still have different native limits. |

Recommend the third option. The external-package duplicate is a short test-only adapter, not a new exported sharing
surface. Do not introduce a generic callback/interface Stop registry merely to deduplicate these adapters: the cleanup
guard must retain statically resolved concrete Stop calls and their context provenance.

## Contract and premises

The existing `test-cleanup-policy` requirement “Lexical ownership of lifecycle test fixtures” is the authority.
Its scenarios “Setup assertion before transfer”, “Caller assertion after transfer”, “Explicit terminal phase fence”
and “Deadline supply versus completion” already cover this change. `component-lifecycle` and
`runtime-context-ownership` retain their existing controlled-stop, abort and owner-specific failed-Start contracts.
No stronger production guarantee is needed or proposed.

| Premise | Accepted measurement |
|---|---|
| Registration currently follows fallible setup | B00/B01/B04/B23; graph-ingest Initialize/Start/Flush at actions_run_scope:126–128 and revision:70–72; cron Processor Initialize/Start at cron_scheduler_integration:82/86 |
| Native contexts and substrate order differ | Profiles S/C/G/V/D/E/R and B12/B21/B22; operation and Start authorities are enumerated per row |
| Explicit terminal attempts can leave fallback armed | H27–H30 and B12; cron stops at 235/315/329, hardening Stop/flag at 118/119 |
| Two private ownership shapes already exist | component/lifecycle_test_suite.go:29–92; processor/graph-ingest/test_owner_support_test.go:11–66 |
| A finite Stop context is not generic interruption/join proof | inventory native-contract section; Processor contextless watcher/cache calls and post-deadline command-lane receives; concrete scheduler retry behavior |
| Exact baseline removal can stay narrow | 24 exact entries; 297 total debt and 90 resolutions; five exposed resolutions have no declaration dependency intersecting this case/helper set |

The state properties below are requirements from the existing cleanup spec, not properties reconstructed from the
new adapter implementation:

1. An acquired owner has terminal protection before later fallible setup; helper ownership persists until transfer.
2. A controlled terminal attempt runs before private Start cancellation and before substrate teardown.
3. One ordinary fixture owner makes at most one concrete terminal attempt, including nonnil return or panic.
4. Concrete errors and terminal expiry remain observable; failure prevents the next explicit phase.
5. Operation authority remains independent of an earlier owner's Start cancellation when another phase follows.
6. Deadline supply and observed completion remain separate claims.

## Concrete private test owners

Use ordinary `_test.go` support in `processor/rule`, with integration build constraints where imports require them:

| Test package / suggested file | Concrete receiver stored | Present consumers |
|---|---|---|
| `rule`, `test_owner_support_test.go` | private `processorTestOwner` with `*Processor`; private `cronSchedulerTestOwner` with `*CronScheduler` | Internal Processor roots and H25–H30; B02–B04 and H01–H24 |
| `rule`, `test_graph_ingest_owner_integration_test.go` | private `graphIngestTestOwner` with `*graphingest.Component` | B00/B23, H31–H37 |
| `rule_test`, `test_owner_external_integration_test.go` | private `processorTestOwner` with `*rule.Processor` | B13–B20 |

Each owner stores only its concrete instance, private Start cancel function, terminal-attempt flag and, only for
returning helpers, transfer state. It does not retain operation/Start contexts or generic context providers. The
existing test harness `h.ctx` remains its operation context, not a way to recover canceled Start authority.

The small owner protocol follows the existing graph-ingest test owner:

- Construct the owner immediately after successful concrete acquisition. Install its lexical `defer finish(ctx, t)`
  before Initialize, Register, Start, Flush or later assertions. A nil failed acquisition is reported without calling
  methods on nil. If an acquisition actually returns a nonnil instance with an error, protect that instance before
  reporting the error; do not infer ownership from a constructor error alone.
- `startContext(operationCtx)` derives `context.WithCancel(operationCtx)` and retains only the private cancel function.
  Invoke it once for an ordinary owner. Pass the returned local context directly to native Start; no new root.
- `stop(operationCtx)` refuses a second explicit owner attempt, sets `attempted` **before** calling native Stop, and
  defers private Start cancellation so even a panic cannot leave that cancellation owned nowhere.
- It synchronously invokes the concrete native Stop with
  `context.WithTimeout(context.WithoutCancel(operationCtx), 5*time.Second)`, defers terminal cancellation, and joins
  the returned error with a separately observed terminal `Err()`. Also report an ended operation context as failed
  controlled authority. Do not replace that authority or suppress accurate abort errors.
- `finish(operationCtx, t)` skips an already attempted owner; otherwise it invokes `stop` and reports with `t.Errorf`,
  preserving any earlier failure. It does not call Fatal, spawn a Stop goroutine, install a watchdog or retry.
- Returning helpers install `defer provisionalFinish(operationCtx, t)` as soon as they own the concrete instance.
  Transfer is marked only after all setup succeeds, immediately before return. Every caller installs lexical
  finalization as its first action after receiving the owner/harness, before its next fallible operation.

The five-second terminal allowance is the existing shared-support/graph-ingest test-owner allowance. It is a supplied
cooperative deadline, not a five-second wall-clock promise. It is below the testing policy's ten-second cleanup
ceiling; no new exception or production timeout change is requested. A native overrun is recorded honestly and
escalated as a separately evidenced production/native issue, not hidden by asynchronous abandonment.

For operation contexts, preserve the existing 10/15/20/30-second scopes in B12 and B15–B22 where present, deriving
from the current `t.Context()` instead of Background. Previously unbounded ordinary scheduler cases receive a
five-second operation scope; previously unbounded integration fixtures receive a 30-second operation/setup scope.
These are work admission budgets for the owning test/subtest, not readiness waits. Do not lengthen an existing probe
or work budget merely to turn a failure green. Deliberate nil/abort/repeat/concurrency probe inputs remain unchanged.
Operation cancellation is lexically deferred before the later owner finalizer, so finalization executes first.

## Wiring the accepted population

| Accepted cases | Concrete edit and ownership boundary |
|---|---|
| B02/B03 and H01–H24 through B04 | Create the private scheduler owner immediately after each returned scheduler, before rule creation/registration. Install caller lexical finalization. Change `startSchedulerForTest` to Start that already-owned scheduler using its private Start child; remove its component `t.Cleanup`. Keep raw scheduler access for the existing fire/registration/probe assertions. Preserve B03's second Start rejection and B02's real robfig firing. |
| B01 and H25–H30 | `startCronProcessorForTest` accepts caller operation context, owns its Processor provisionally before Initialize/Start, and returns the private owner plus registry after transfer. Each of the six call sites retains the owner and defers finish immediately. Each restart phase gets a distinct owner and private Start child. |
| B00/B23 and H31–H37 | The two returning harnesses accept caller operation context and carry their concrete graph-ingest owner. Protect before Initialize/Start/Flush and transfer only after bucket/manager/mutation-client setup. All seven callers immediately defer that owner's finish. Existing `h.ctx` refers to operation authority shared by later action/mutation assertions. |
| B05–B11 | Each direct test or table subtest owns its own Processor before Start. Operation scope belongs to that test/subtest. The parent shared TestClient stays alive through all child finalizers; no parent cancellation is used to terminate a child before its Stop. Keep the 3/5/3 subtest cases and their coalescer expectations. |
| B12 | Replace the successful-stop flag with the same private owner attempt state. Explicit checked owner Stop remains before borrow/coalescer assertions. Error prevents those assertions/next phase and leaves finalization unable to retry. Lexical fallback precedes operation cancellation. |
| B13–B20 | Keep external `rule_test`. Install its concrete private Processor owner before Initialize/Start. Preserve separate Background-derived operation versus Start scopes by replacing each with the appropriate finite testing-derived scope, not by making all I/O depend on the private Start child. |
| B21/B22 | Same internal Processor owner. Remove redundant Terminate registrations/defers around `NewTestClient`; its canonical callback owns substrate termination. Preserve the deleted-state/prefix-sibling and stateful-state assertions. |

For H27–H30, replace `proc.Stop(Background())` phase fences with `owner.stop(operationCtx)` and assert the result
before stale timestamp writes, subsequent Processor acquisition, or post-stop counts. Start cancellation occurs
inside that owner only. The still-live operation context remains available for KV seeding and the second owner.
The later lexical finalizer skips the first attempt whether it succeeded or failed. No generic rejoin is created.

Constructor-only and deliberate-probe adjacency remains as inventoried. Avoid changing shared constructor signatures
merely to reach those 25 retained references. Their existing raw scheduler return remains usable; ordinary scoped
callers install the new owner immediately after return. Any actual nonnil-on-error acquisition encountered during
implementation is handled explicitly before its assertion and recorded, rather than expanding every adjacent probe.

## Substrate and support ordering

`NewTestClient` retains sole client/container cleanup ownership. Shared-client call sites and the two existing shared
client helpers keep one checked terminal callback; replace ignored Terminate errors with nonfatal reporting. Their
existing startup/test options remain unchanged. The external shared helper's three retained adjacent consumers see
only improved substrate error reporting, not a new component owner or changed API.

A component lexical finalizer runs before testing cleanup callbacks. Register observation-subscription cleanup before
or during body setup, retain handles where a touched case currently discards them, and check Unsubscribe results.
For the two cron restart tests, move lexical subscription defers to checked testing cleanup so the observer remains
available through component finalization. Client cleanup was registered first, so subscriptions precede the client
when callbacks run. Never terminate substrate to make a component Stop finish.

Preserve H05/H22's explicit WaitGroup joins for their test-created fire goroutines. New proof fixtures use explicit
release/join signals; error paths release test-owned gates before waiting for those goroutines. Do not introduce
arbitrary sleeps, blanket serialization, aggregate goroutine-count success claims, or another support registry.
Existing unrelated sleeps/probes are retained unless a directly touched observation needs a causal replacement.

## Proportional test-first proof

Use the accepted #1419/#1424 shape and link their exact existing sensitivity records for unchanged mechanics; do not
repeat a generic lifecycle test framework or a Cartesian owner-by-exit matrix. The coordinator's pre-change targeted
rule unit run is passing behavior evidence, not sensitivity evidence for this change. Preserve its revision/command.

Add the minimum new evidence that observes this adaptation through real native Stop seams:

1. **Rule Processor setup escape and controlled order.** A focused internal unit fixture reaches the production
   Processor lifecycle/Stop path using the existing owner-local deterministic seams. Observe an admitted callback or
   owner-lane completion, not only a fake Stop counter. Exercise a scope exiting after acquisition/setup but before
   successful transfer, and one after transfer. Native teardown must observe live Start authority, finish the named
   work, and precede private cancellation/substrate-marker cleanup. Use the existing native consumer/lane seams,
   channels and `synctest` where applicable; do not alter `startRuleRuntimeForTest` or its five-resolution dependencies
   just to obtain the new test. Ordinary return/error and fatal-assertion behavior can share the existing reviewed
   lexical-defer proof, but at least one actual returning rule helper's escape wiring must be exercised.
2. **Scheduler concrete failure and once-only decision.** Start a real CronScheduler with the existing blocking
   executor seam. Observe admission, preserve the private Start child through a controlled Stop, release the action
   causally and observe fire/dispatcher completion. A separate deterministic short-bound lane makes the actual Stop
   return its context error; assert that the owner reports the error/expiry and treats the concrete attempt as final.
   Exercise fallback after that error and the owner refusal of a second explicit attempt. Do not substitute a fake
   Stop implementation for this production seam or treat the expiry lane as joined. Test cleanup owns release/join
   of the deliberately blocked action, without retrying component Stop.
3. **Integration wiring for helper transfer and external package.** Reuse existing named rule integration cases as
   the main coverage: both graph-ingest harness families, the two cron restart cases, hardening B12 and an external
   Processor case. Add one narrowly scoped setup-escape witness in an actual returning helper if the unit/linked
   evidence cannot activate its provisional finalizer. A small test-only setup fault or bounded re-exec test may
   activate Fatal without changing production; it must traverse the actual helper and concrete Stop. Reuse existing
   support for observing intentional test failure, not a new exported assertion runner. Prove component-before-NATS
   order and checked native result. Preserve independent operation authority through the restart/post-Stop phase.

The implementation evidence table must name each assertion, its activation signal, native seam, exact executed test
and result. Source-only review of all B/H wiring complements these witnesses; it does not replace the required
native seam observations. Conversely, a local witness does not claim all 37 paths executed if the run skipped them.
The external private adapter must be reached by an executed external-package case, not merely compiled.

A selected proof must fail against the old wiring or a single named fault before being used as regression evidence.
Do not manufacture an exhaustive new test suite to prove unchanged production semantics.

## PBT and mutation decision

**PBT decision: named deterministic examples sufficient.** Shutdown/transfer history makes the policy applicable.
The independent oracle is the existing cleanup spec's four named scenarios and the controlled/abort distinction,
not the adapter's flags. The relevant histories are: acquisition then setup exit; successful transfer then body
exit; successful explicit Stop then later operation/new owner; failed explicit Stop then fallback; and ended work
or terminal authority. Repetition is covered by fallback after an already attempted Stop and the second explicit
owner call; native deliberate completed-repeat tests remain separate. These finite, named order distinctions and
causal release points provide stronger activation evidence here than random action generation. No data grammar,
codec, revision law or new production state machine is added. Native arbitrary action histories are not covered.

Targeted mutation is required because this fixes a regression class existing tests admitted and protects owned work.
Reuse exact reviewed evidence where the unchanged shape/source/checks cover the fault; record that mapping rather
than claiming every copied adapter is automatically covered. For remaining adaptation risks, select the smallest
set of mutation runs that independently exercises:

- omission of provisional/caller lexical finalization on the chosen actual-helper escape path;
- cancellation moved before real Stop on the chosen admitted-work order witness;
- attempted state recorded only on success, leaving fallback eligible after a concrete Stop error.

One selected test may expose multiple faults, but run one mutation at a time and retain which assertion detected it.
No repository-wide mutation sweep or score target is required. Use `cp` backups and SHA256 before mutation; mutate
only implementation/fixture-owner wiring while checks and expectations stay fixed. Record passing baseline, compiled
mutant reaching the intended assertion, observed failure, exact-byte restoration, matching checksums and restored
pass. Never use Git restore/stash/reset or overwrite concurrent edits; use an isolated disposable copy if exclusive
ownership cannot be guaranteed. Survivors, invalid mutants, skipped witnesses or containment timeouts are not proof.
A timeout cannot substitute for the intended ordering/error assertion.

## Baseline reconciliation, verification and gates

Remove only the repaired 24 semantic identities enumerated in `review/baseline-exposure.json`, after their source and
proof reconciliation. Starting from the accepted snapshot, expected residual debt is 273 and reviewed resolutions
remain 90. Preserve all remaining entries and resolution dependency fingerprints byte-for-byte unless a separately
measured change requires exact review. Do not regenerate the baseline, weaken the guard or add broad exceptions.
The five exposed resolutions remain unchanged; the new owner support must remain statically analysable. Any new
uncertain cleanup binding is fixed at the concrete test seam rather than concealed by an invented approval.

Run focused race unit checks and the canonical focused integration runner for rule. Keep #1062 readiness ordering,
#1283 owner-lane/deadline/cache/watcher proof, and #1404 bounded-stop evidence intact. Record executed/skipped coverage,
then independent implementation review and the canonical `task check:push` gates. Use the existing preflight skill
for gate selection/runner lock ownership during implementation. No E2E/BREAKING requirement is newly triggered by this
test-only design; unexpected production/API changes require separate review and applicable gates.

#1416 stays open for its recorded owner disposition. #1421 remains a merge hold: the #1404-only waiver does not cover
#1429. A fix or explicit owner waiver is required for that merge gate; it is not authorization to repair graph-index
in this batch. #1293 and #1411/#1412 retain their scopes. Claude retains #1426/#1427 E2E assertion work.

The current cleanup specification already admits this implementation; `spec-disposition.md` records why no normative delta
is proposed. No new approval question is manufactured: independent design review and coordinator acceptance within
the existing owner-authorized scope are the remaining design handoff steps. Binding rulings stay with the owner.
