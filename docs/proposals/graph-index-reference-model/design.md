# Graph-index reference-model proof design

Status: draft for independent design review. Issue #1292 and PR #1393 remain the acceptance record.
Review the complete accepted [inventory](inventory.md) before this design; its verdict and identity are in
[review.md](review.md). The independently reviewed [timing supplement](timing-inventory.md) extends that inventory.
This is proof-only work, with no OpenSpec behavioral delta.

## Scope and alternatives

This change adds tests in new files only. No runtime, API, storage, configuration or broker-contract change.

| Option | Benefit | Cost or limitation |
| --- | --- | --- |
| Extend existing fixture/test files | Smallest initial implementation | Touches shared fixtures/tests outside this claim's file boundary. |
| New test-owned fixture reusing existing low-level mock buckets | Exercises production reconciliation without broker orchestration | Must model authority revisions and state broker/watcher limits. |
| Do nothing | No new maintenance | Leaves #1292's generated-history and sensitivity obligations unmet. |

Use the second option. Reuse `newMockKVBucket` for derived storage; retain fixture ownership, authority records,
status adapter, fault configuration and observations in new test files. Retain bucket references directly rather
than adding each generated owner to the global component-to-mock registry. Existing service and graph-query
properties provide the action/model and independent semantic expectation patterns.

## Production seam and evidence boundary

Drive `Component.processEntityWork` synchronously with `entityIndexWork`: production authority refetch, validation,
planning, replacement, bounded write retries, failure tracking and watermark completion. Drive recovery through
`repairFailedEntities`, using synchronous submission when no dispatcher is installed.

Observe production incoming, outgoing, predicate and by-name query handlers and `computeIndexStatus`. Separately
inspect OUTGOING owner presence: absent ownership and a present empty array can produce identical query results.

The fixture supplies current authoritative entity bytes and a separate status reader reporting monotonic committed
revision. Register delivered revisions with the real watermark. Do not set `indexBootstrapped` directly. Enumeration
completion and its fixed target are fixture inputs; production status code decides bootstrap completion.

This tier proves the reconciliation callback, repair entry, completion and readiness for admitted sequential
histories. It does not prove WatchAll delivery, dispatcher FIFO/concurrency, periodic repair scheduling, full Start
wiring, status publication or NATS filters. Existing focused tests retain those obligations. No arbitrary sleeps or
background work is needed in this model.

### Virtual time and Rapid boundary

Each generated case runs its synchronous production/model driver in a fresh `testing/synctest` bubble. Rapid stays
outside: `rapid.Check` receives the ordinary outer `testing.T`, then draws the complete bounded history before calling
`synctest.Test(outerT, ...)`. Construct the fixture, contexts, authority, stores and component inside the bubble.
Run the unchanged production reconciliation/repair methods and return copied semantic observations, activation
counts and structured driver errors. After the bubble returns, assert with `rapid.T`, naming the history/failing step.

The driver makes no Rapid calls and uses no Fatal, FailNow, require, T.Run or T.Deadline inside the bubble.
Behavioral mismatches return errors; Rapid receives them in its own callback so shrinking/replay remains effective.
Named witnesses use the same error-returning driver and assert outside their bubbles with ordinary testing.T.
Create/cancel operation contexts inside; return no bubble channels, timers, contexts or component pointers.
No background component runtime or external I/O starts. Each replay/shrink invocation gets a fresh fixture/bubble.

Production retry timers remain unchanged. Synctest advances virtual time when the synchronous driver blocks.
Persistent backend faults remain backend errors through all retries; cancellation never substitutes for exhaustion.
No TB adapter, runtime clock hook, shared-helper edit or dependency is introduced.

## Contract and independent oracle

Use exact applicable `// spec:` citations:

- `graph-index / Every surviving derived index declares storage responsibility and reconciliation capability`
- `graph-index / INCOMING rows are retracted by their source owner`
- `graph-index / Readiness is authoritative and consumers fail closed on incomplete indexes`
- `graph-index / The index buckets rebuild from entity state on boot after the format cutover`
- `graph-index-readiness / The envelope reports bootstrap completion`
- `graph-index-readiness / Read consumers retry the readiness transient`

The accepted source-owned requirement and ADR-077 section 5 govern deletion. The inventory's stale legacy paragraph
is not an alternative contract.

Hold logical entities independently: existence, name, literal predicate memberships and relationship pairs.
Generated operations update this map; a separate adapter encodes canonical EntityState bytes for production.

Expected answers are direct projections of the logical map:

- PREDICATE: entities containing each predicate, including predicates on names and relationships.
- NAME: memberships for bounded names, retaining original spelling.
- INCOMING: source/predicate pairs asserted by live sources, including assertions whose targets are absent.
- OUTGOING: relationships for each present entity; present empty entities retain `[]`, absent entities have no key.

Do not derive expectations through production key builders, filters, buildEntityIndexPlan, computeIndexProjection,
relationship classification or reconciliation. Relationship versus literal is explicit in generated logical input.
Use an enumerated name-equivalence table rather than production normalization to compute expected matches.

Compare exact semantic sets and reject duplicate members. Query the entire bounded vocabulary, including values
removed earlier in the history, to detect missing results and stale survivors. No claim of full ranking, pagination,
value-filter or compound-query conformance. ALIAS replacement, retired buckets, malformed-authority poison recovery
and arbitrary same-key conflicting names are outside this model.

## Histories and assertion activation

One bounded Rapid property and named witnesses share the driver and oracle. Each generated case starts with a short
deterministic prefix. Assert activation counters for required observations; choosing an action is not proof it ran.

1. Cold nonempty owner: two distinct delivered entities remain pending when enumeration completes. Queries refuse
   before work and after only one entity finishes. After both complete, compare exact results.
2. Replacement: change a source's name, literal predicate and target from A to B, then empty. Query old and new
   values after each catch-up boundary; assert explicit empty OUTGOING storage.
3. Ownership: a second source still points at a target that is deleted. Preserve that source's INCOMING assertion.
   Deleting a source removes only its assertions.
4. Replay/stale work: execute an already delivered older work item after authority advances; refetch current state.
   Repeat work without authority mutation and require unchanged exact results. Never deliver descending observations.
5. Failure/repair: inject a required predicate Put fault persisting through production retries. Assert fault hits,
   degraded/not-ready status and classified refusal even when completion reaches the delivered revision. Disable
   the fault, call repairFailedEntities and require exact convergence and healthy status.
6. Fresh-owner hydration: deep-copy current authority into a new component with empty derived stores and fresh
   watermark/bootstrap state. Replay current entries in ascending retained-revision order, refusing incomplete
   hydration and comparing exact parity afterward.

Named witnesses also guarantee required-delete failure/repair, stable-key name case replacement, an empty initial
graph and healthy post-bootstrap lag that still serves. Compare full semantic equality only after the stated work
boundary; the lag control does not require live-head equality.

For hydration, initially use nonempty authority whose latest live record carries current head revision. Empty initial
storage has head zero. Do not invent a revision contract for an empty retained bucket after deletions or tombstone expiry.

## Finite bounds

Initial hard bounds per case: four canonical entity IDs also used as targets; two literal predicates, two relationship
predicates and one name predicate; four enumerated names including Alpha/ALPHA; at most one name, two literal
memberships and two distinct relationships per entity; twelve suffix actions; at most one additional generated
failure/repair pair and one additional hydration. No concurrent cases or t.Parallel.

Generate create/replace, clear, delete, duplicate reconciliation, delayed older work, repair and hydration only under
their stated preconditions. Authority write revisions increase globally; delivered observations ascend, although work
may be delayed. These are local test budgets, not CI/runtime limits.

## Execution and sensitivity

Normal execution must complete all 100 checks, without short mode, reduced check flags, global flag changes or
production timing changes. Use two explicit seeds:

```bash
go test ./processor/graph-index \
  -run '^(TestPropGraphIndexReconciliation|TestGraphIndexModel)' \
  -count=1 -race -timeout=120s -v \
  -rapid.checks=100 -rapid.seed=1292 -rapid.shrinktime=3s
```

Repeat with seed 1293. Print and record both seeds, commands, actual valid-check counts, activation counters and
per-top-level-test real elapsed time. Check that no RAPID_* environment override changes the recorded run.
The hard action limit is in the test; rapid.steps is not a cap. Also run with no checks override to verify normal defaults.
Measure outside bubbles: each new unit top-level test must meet the five-second race ceiling. The process timeout
does not replace that ceiling. Rapid's own duration and shrinking budget remain in real time.

Virtual time removes mandatory retry waits but does not prove CPU/runtime cost. If default-100 execution exceeds the
ceiling, report and correct the new test implementation without reducing checks or required assertion activation.
Until measured, budget compliance remains unproven.

Before relying on the composition, demonstrate both fixed seeds passing 100 checks; an intentional semantic mismatch
returned by the driver failing the intended Rapid assertion outside the bubble; recorded shrinking/replay reaching
the same intended assertion; and restored exact baseline bytes passing 100 checks. A compatibility spike proves only
composition. The actual repository property independently owes execution, mutation and restoration evidence.

| Mutation | Required detection |
| --- | --- |
| Omit stale-row deletion in reconcileOwnedRows | Exact membership mismatch after A-to-B or B-to-empty. |
| Bypass bootstrap check in ensureQueryReady | Successful admission while initial owner work remains pending. |
| Suppress required-write failure tracking | Successful admission or non-degraded envelope after an actually injected persistent fault. |

Run one mutation at a time in an isolated disposable copy with fixed tests/model/expectations/command. Retain only new
test files in the implementation diff. Use cp backups and checksums; preserve baseline pass, compiling mutant's
intended assertion failure, and restored pass. Never alter another owner's worktree. Use rapid.nofailfile for synthetic
mutations. Keep named witnesses and exact mutation patches/commands in PR evidence; triage genuine findings separately.

## Implementation slice and remaining gates

New files only:

1. `processor/graph-index/reconciliation_model_helpers_test.go`: local fixture, authority adapter, driver, semantic
   model, direct query observations and fault-hit accounting.
2. `processor/graph-index/reconciliation_model_test.go`: named required histories and assertion activation.
3. `processor/graph-index/reconciliation_prop_test.go`: bounded generators/property and exact citations.

Completion requires focused race runs, citation validation, sensitivity comparisons and independent review.
No edits to existing fixtures/runtime, smoke cleanup, common NATS helpers, CI, Taskfile or runner.

Real-NATS confirmation remains pending: run existing replacement/restart and failure/repair integration witnesses
through the canonical runner only when the shared lock is released without competing with another claim. Until then, unit
evidence does not complete broker-confirmation acceptance. No new NATS test or container is proposed.

No further behavioral owner decision is identified. A runtime defect, production seam change, incompatible authority
history or unresolved required-evidence deferral must be reported rather than silently expanding this proof-only scope.
