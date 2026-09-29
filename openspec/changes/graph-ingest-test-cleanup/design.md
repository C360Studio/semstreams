# Graph-ingest test cleanup design

Status: accepted for implementation on 2026-09-29 under the owner's instruction to continue the planned
#1417 package batch. Independent DESIGN REVIEW PASS and exact draft hashes are recorded in
`review/design-review.md`. This accepts private test-pattern adoption, not a new production contract.

## Evidence checkpoint and scope

The accepted inventory remains verbatim at
`0085323e:openspec/changes/graph-ingest-test-cleanup/inventory.md`, SHA256
`c7b4178dc00f4fdab4fad2f39a21db7969aa56132e3df9873ba232eb39e55e12`.
Its exact baseline and typed-helper-reference companions are part of that checkpoint.
It accounts for 32 legacy cleanup identities in 16 files, six returned-runtime helpers and 44 typed calls,
17 adjacent ordinary Stop locations, production teardown, existing patterns and the private adopter boundary.
This design adopts #1417/#1419 for that population; it changes no production behavior or exported API.

Current manifest is 329 debt entries / 89 resolutions, not the historical 334. None of its manual resolutions
references graph-ingest. The accepted inventory explains the independent five-entry rule reduction.
The default package baseline at the checkpoint passed with race detection: 5.017 seconds wall clock,
202 top-level passes and six skipped tests. This is neither integration nor production cleanup evidence.

## Options and recommendation

| Option | Benefit | Cost or limit |
|---|---|---|
| Adopt the existing lexical-owner pattern privately in graph-ingest tests | Fixes ownership, checked results and cancellation order together | Changes the six helper signatures and their measured callers as well as the 16 baseline files |
| Replace each Background Stop with an inline finite context | Small individual edits | Leaves helper assertion escapes and caller cancellation before registered Cleanup; insufficient |
| Reuse RollbackFailedStart as the normal finalizer | Existing detached finite callback | Owns failed-Start rollback, not normal fixture ownership, transfer or once-only terminal attempts; wrong semantic boundary |
| Do nothing | No test churn | Preserves all 32 exemptions and the measured early-exit holes; does not satisfy #1423 |

Recommend the first option. Follow the private owner in `component/lifecycle_test_suite.go:29`, including its
attempt-before-call rule and checked concrete/context errors. Its symbol remains private; do not export it or build
a generic test framework. The new package-local owner has current consumers in the inventoried tests.
No communication, payload, query or orchestration surface is introduced; those decision skills do not trigger.

## Private owner and authority

Add one small, unexported test-support file available to default and integration tests. Its owner holds a concrete
`*Component`, private Start cancellation and terminal-attempt state; it retains no context or context provider.
It does not synchronize concurrent lifecycle calls: the lexical caller alone owns finalization.
Contexts enter methods explicitly. Existing production owner tests retain their deliberate concurrent probes.

Each ordinary test creates a finite operation context from its own `t.Context()` and defers its cancel immediately.
Use 60 seconds initially where an integration operation previously used Background; preserve existing 60/90/120-second
readiness limits. Default unit examples use at most five seconds. These are supplied operation limits, not measured
completion promises. Retain narrower existing operation/probe contexts and their intentional cancellation semantics.
The owner derives a private cancelable Start child from that operation context and passes it directly to real Start.
Do not replace a deliberately canceled CreateEntity input with the Start child.

The terminal method creates a fresh five-second context immediately before calling real Component.Stop,
using `WithTimeout(WithoutCancel(operationCtx), 5*time.Second)`, and cancels that context on return.
It marks the attempt before entering Stop, returns the concrete Stop error joined with any terminal-context error,
and reports an unexpectedly ended operation authority on the ordinary controlled path.
It cancels only the private Start child after the synchronous Stop call finishes, including error exits.
The operation context remains usable for post-Stop assertions until its caller's deferred cancel.

The deferred finalizer reports cleanup failure with `t.Errorf`, preserving the original assertion failure and naming
the terminal phase. It neither calls Fatal nor logs-and-passes. An earlier explicit terminal attempt suppresses
implicit retry; its caller must already have asserted the returned error. Do not add generic retry/rejoin authority.
A Start failure's production rollback remains unchanged. If that rollback retains cleanupPending, the already-owned
test finalizer can make its one explicit finite Stop attempt under the existing failed-start contract.

Go cancels `t.Context()` before registered Cleanup. Therefore component shutdown is a lexical defer that executes
before that cancellation and before the caller's earlier deferred operation cancel. TestClient's existing registered
cleanup remains the substrate owner and runs afterward. Merely changing Start to t.Context while keeping registered
component Cleanup would recreate an abort-shaped normal exit and is not an acceptable adoption.

## Ownership through setup helpers

Take ownership immediately when a concrete component exists, before Initialize, Start or a later fatal assertion.
Each of the six returning helpers accepts the caller's operation context as its first argument.
It installs a provisional lexical finalizer before any initialization/start assertion, derives Start authority through
the owner, and retains ownership through all setup work, including authority's post-Start Flush.
Only successful return transfers ownership. A boolean transfer marker controls the provisional defer; it is set
immediately before return, after the final setup assertion. Fatal/Goexit and ordinary error exits before that point
therefore finalize locally while the caller's operation authority and NATS client are still live.

Return the owner as an extra private result, or as a private field on the existing authority harness. Remove redundant
returned operation-context results when the context has become a caller input. Every caller installs its lexical
finalizer immediately after the helper returns and before assertions or further work. Update all 44 measured calls;
this intentionally reaches test files beyond the original 16. The companion reference census is the review checklist.
No helper-local unconditional defer may stop the successfully returned runtime before its caller uses it.

For direct setups, install the owner/finalizer immediately after construction and before Initialize/Start.
The hierarchy constructor remains construction-only; its callers acquire ownership before initialization.
No TestClient cleanup is duplicated. Remove the two explicit Terminate defers in readiness gauges/durability;
NewTestClient already registers checked, once-only substrate cleanup.
On an unexpected setup or explicit terminal error, the test fails immediately and admits no next phase; already-owned
fixture work still follows its lexical finalizers. Preserve existing worker synchronization; do not call Fatal in workers.
If a touched worker path needs early failure propagation, collect its result on the owning test goroutine and join the
already-started workers before component finalization. This is not a new scheduler or a package-wide worker rewrite.

## Explicit probes and skipped bodies

Replay seed Stop is a phase fence: route it through the seed owner, assert success, then create the fresh replay
component on the existing substrate. Give the seed provisional/final ownership before it can fail during setup.
The readiness AbsentKey producer Stop is likewise an asserted fence before Purge. Its operation context must remain
live for Purge and reads. Both fences consume the owner's single attempt, so later defers do not retry them.

Preserve deliberate Stop-before-Start, nil/expired Stop, running deadline terminality, repeated completed Stop and
failed-Start overlap probes. Their contract inputs are not ordinary cleanup roots to rewrite indiscriminately.
Do not infer abort intent from a cancellation elsewhere in a test. Running Stop error terminalizes the instance;
only the existing failed-start cleanupPending case has retryable retained handles.

Keep all six current skips. Repair the three skipped bodies' cleanup source using the private owner and align the
skipped AlreadyStarted assertion with the existing one-shot ErrAlreadyStarted contract. Do not enable these bodies
or claim they executed. Report 29 integration-tagged source repairs and three skipped source repairs separately.
Existing active one-shot owner/integration tests supply runtime contract evidence; the skipped tests do not add it.
No unrelated benchmark migration or broad timing cleanup is included.

## Invariants and proof limits

Draft `test-cleanup-policy` delta below is the test-fixture spec home for these obligations:
1. Every acquired fixture component has one lexical terminal owner before fallible setup continues.
2. Successful helper return transfers that ownership; early exits do not abandon it.
3. Controlled Stop receives a fresh finite context and finishes before owner cancellation or substrate teardown.
4. Every concrete terminal result is checked; an error or panic never grants an implicit second attempt.
5. An explicit successful fence leaves the operation context usable and prevents later implicit Stop.

These are fixture obligations, not a stronger implementation promise from Component.Stop.
Native consumer Drain is contextless. HybridCache.Close and TTL.Close each have an independent five-second wait and
run sequentially outside the Stop context. KeyedPool.Stop can return a context error before lanes finish, followed by
Component's pool cancellation. A five-second supplied Stop deadline therefore proves neither five-second wall time
nor complete join on failure. No background Stop goroutine, generic watchdog or production change is proposed.
Record a real violation if observed; do not hide it by retry, enlarged blanket budgets or a guard exemption.

## Test-first evidence and mutation plan

Use named examples because this is a small ownership protocol with explicit transfer and terminal transitions;
there is no new input grammar or combinatorial domain requiring PBT. Table the supported paths before implementation:
construction/initialization exit, Start/setup assertion exit, transferred caller assertion exit, normal finish,
explicit fence, Stop error and operation cancellation. The oracle is ownership/order/results, not helper field values.

Use the real Component.Stop boundary and existing owner-test resource shapes: controlled consumer Closed, real keyed
pool work and observed core subscription drain. Causal channels establish entry, release and completion; timers bound
failure waits and never stand in for readiness. Owned fixture releases/joins must themselves execute on early failure.
Do not create another NATS container solely for these unit examples.

Prove quick useful failure with a sentinel terminal error at the existing injectable core-subscription boundary.
Assert that the original error and phase reach the test outcome and that no next operation is admitted. Prove a
successful cleanup holds substrate teardown until owned processing and consumer completion are observed. Keep the
failure case distinct from the successful-join case; deadline expiry must not be described as completed cleanup.

Intentional Fatal/Goexit proof may use one selected self-execution of the current test binary, with setup-escape and
post-transfer-escape subcases. Exercise the private ownership path against a real Component with controlled resources;
this proves assertion-exit ownership, not successful real-NATS Start. Capture owned output, use one synchronous Wait,
and bound/reap the selected child; no shell, nested go build, recursive full suite or generic process supervisor.
Target under one second warm for the selected proof, with the existing five-second unit ceiling; measure under race.
Do not force a Go deadlock or deliberately strand a context-ignoring native resource to prove a narrower contract.

Run actual existing integration helpers and the replay/readiness cases through the focused canonical runner to prove
production construction, accepted Start, owned Stop and substrate ordering. Reuse their current substrate acquisition.
Preserve their domain assertions. Measure top-level and cleanup timing separately; compare package costs with the
existing testing policy rather than treating the supplied context durations as performance evidence.

Named mutations, restored by cp backup and checksum, must fail their intended oracle:
- Remove provisional cleanup: setup assertion-exit ownership fails.
- Cancel Start before Stop: live-authority/order proof fails.
- Discard the terminal error: sentinel failure-outcome proof fails.
- Retry after an explicit attempt: terminal-call count/fence proof fails.
- Remove the explicit readiness/replay fence: existing phase-order oracle fails; strengthen a causal observation if needed.
Use focused cases only, not a Cartesian matrix of all callers. Preserve exact command, exit, oracle and restoration evidence.

## Guard, validation and completion

After source freeze, run the existing semantic guard and reconcile each of the exact 32 original identities with
current source and proof. Remove only those obsolete legacy entries. With no independent changes, remaining legacy
debt is 297 and graph-ingest is zero; counts alone do not establish that replacements are admitted or cleanup joins.
Require zero new unapproved guarded debt/uncertainty and no stale approvals. Any helper-origin record or manual
dependency change requires its own exact source-based independent review; never mass-approve the new private helper.
Keep unrelated 89 resolutions unchanged unless the measured guard establishes a concrete dependency change.

Validation sequence: focused race unit/protocol examples; focused real integration helper/fence tests through
`scripts/run-integration-tests.sh -run '<selected cases>' ./processor/graph-ingest`; exact guard reconciliation;
independent implementation review; canonical check:push and any repository-required final gates.
Do not duplicate full integration runs while another owner holds the host. No new runner/CI bypass or full-suite tax.
Refresh changed-source inventory pins after the implementation while preserving the accepted checkpoint above.
The final report separates source debt removal, skipped-source repairs, real integration results and unproven native
wall-clock behavior. #1423 closes only through its merge; #1417 remains open for later measured package batches.

No new owner policy question is proposed. Independent design review and coordinator acceptance precede implementation.
