# Rule standard lifecycle coverage (#1410)

PR: https://github.com/C360Studio/semstreams/pull/1413

The rule processor now runs through the existing portable lifecycle suite using its registered production factory
and one fresh NATS container. An empty entity-watch pattern enters the real watcher path. No production runtime,
public API, shared suite, or runner behavior changes.

## Scope and conformance

| Approved obligation | Implementation |
| --- | --- |
| Real rule processor in `StandardLifecycleTests` | `processor/rule/lifecycle_integration_test.go:58` |
| Production NATS and platform dependencies | `processor/rule/lifecycle_integration_test.go:31` |
| Entity watcher path rather than an inactive default port | `processor/rule/lifecycle_integration_test.go:27` |
| Fresh instances and concurrency-safe ownership | `processor/rule/lifecycle_integration_test.go:71` |
| Processor cleanup before NATS teardown, one aggregate budget | `processor/rule/lifecycle_integration_test.go:41` |
| Preserve existing unit checks; correct obsolete exclusion rationale | `processor/rule/rule_lifecycle_test.go:8` |
| Keep CronScheduler API, #1064, #1411, #1412, and #1293 outside this change | Production code and shared infrastructure unchanged |

The inventory and design are retained in [inventory.md](inventory.md) and [design.md](design.md).
No OpenSpec delta or archive is needed: existing lifecycle requirements and suite semantics are unchanged.

## Independent reviews

- Inventory: **INVENTORY PASS**, SemStreams reviewer (Astra/high), 2026-09-28.
  Reviewed SHA-256 `597e19ad35b71df23ca21ba58e314919ea22875d6b50c10bb58102f864bca6a3`; verifier 64/64.
  Sharing one fresh container inside this lifecycle test does not establish a policy violation. Shared readiness
  writes are measured; no interference with lifecycle assertions was established.
- Design round 1: **CHANGES REQUESTED** at SHA-256
  `a62efb1acb9867e3e4113b93c7b9d04882c051e90af88f093aa37a5a17c1a088`.
  A factory panic could bypass worker cleanup; acquisition success was overclaimed.
- Design round 2: **DESIGN REVIEW PASS**, same independent reviewer, at SHA-256
  `0aebecff4c6f98aa234999cf2f96553a61752f7d8aaac9585c77a6f9e851304a`.
  Factory failures use `t.Errorf` plus nil; resource-acquisition limits are explicit.
- Implementation: **APPROVE**, SemStreams reviewer (Astra/high), 2026-09-28, at the test and production
  hashes recorded below. Verified 253 successful starts, watcher acquisitions, and stops, one NATS container,
  expected warning classification, and baseline/mutant/restored evidence. No blocking/high findings.

The architect produced the inventory and fixture slice. The coordinator materialized the reviewed test-only slice
under the developer contract after both a new developer spawn and the prior developer resume were refused by the
session's agent-thread limit. The independent implementation review above covers that materialized slice.

## Execution evidence

All commands ran in the claim worktree on base `29429020` plus the new test and comment correction.
Test SHA-256: `0a358a7dc98c5cdb76217dd8d6ca295fb1ff0395a3334a28ff5b0aa367c07213`.
Unchanged production `processor.go` SHA-256:
`49ce7ffc8b9a18308311606b897d862b837b9f104869ca1a2f67228a1b87fb71`.

Focused command:

```bash
scripts/run-integration-tests.sh -run '^TestIntegration_RuleStandardLifecycle$' -v ./processor/rule/...
```

Result: exit 0; the top-level test passed in **1.62 s**, rule package **3.178 s**. All seven portable cases,
both rejected-Start cases, parallel fresh instances, and the leak loop executed. The expression subpackage selected
zero tests and supplies no new evidence. One NATS container was created and terminated; Ryuk is runner crash safety.
The leak loop observed goroutine growth 0 and memory growth 254,488 bytes. These are observations, not complete
resource-leak proof. Verbose output contains 257 expected no-rules warnings and no other WARN/ERROR class.
No setup-degradation warning was observed. Full output: [focused.log.gz](evidence/focused.log.gz).

### Mutation sensitivity

A synthetic production fault changes only the nil branch of `Processor.Stop` from its invalid-context error to nil.
The new test and the shared suite remain unchanged. This supplements wiring evidence; it is not an orphan-fence
regression or independent hang-containment experiment.

```bash
scripts/run-integration-tests.sh \
  -run '^TestIntegration_RuleStandardLifecycle$/^PortableFloor$/^NilStopContext$' -v ./processor/rule
```

- Baseline: exit 0, package 1.962 s; [log](evidence/mutation-baseline.log.gz).
- Mutant: exit 1; `Stop must reject a nil context` / `An error is expected but got nil.` at the existing suite's
  line 115. Top-level test including container cleanup 0.52 s; package 0.990 s. [Log](evidence/mutant.log.gz).
- Restore: exact `cp` backup restored, production checksum above matched and production diff was empty.
  Same selected check passed; [log](evidence/mutation-restored.log.gz).

The defect was **detected** at its intended assertion. Container termination is visible on the failed path.
The nil-Stop case never starts runtime work; it does not prove cleanup of an abandoned running processor.

PBT decision: named examples sufficient for wiring the existing supported lifecycle sequences; no new state-machine
semantics or generated-history guarantee. Oracle and limits are in [design.md](design.md).

## Remaining limits and gates

The suite calls Stop synchronously. Its finite contexts are cooperative bounds, not protection against an
implementation that ignores cancellation. It does not force the deadline to win. Resource-specific rollback,
drain ordering and orphan-fence coverage remain with the existing focused owner tests. Shared readiness values are
not asserted. Start can log acquisition failures and continue; this fixture does not add per-instance acquisition
assertions. The process-wide leak heuristic allows bounded variance and does not enumerate NATS server resources.

Full `task check:push` and current-head hosted CI results are recorded in the PR with their exact commit identity.
The focused evidence above is not a substitute for those required gates or merge authorization.
