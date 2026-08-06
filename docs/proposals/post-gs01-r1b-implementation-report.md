# Post-GS-01 R1b lifecycle poison-localization implementation report

## Scope and baseline

Implemented the reviewed R1b slice on repository baseline `dd02a715ac055b8f5ea8bf8cd9391740537ff6d9`. The reviewed
design was verified at SHA-256 `16c29ab9f2f6d9551f32ff7a2e267b937c096b17b3564392b4687a795d31210e`.

Before the RED mutation, `pkg/lifecycle/watch_atomic_bootstrap_test.go` and its backup both hashed
`1b171981919e21b5ff8b41f0fea25f44`. The deterministic regression test started a matching A watch, delivered malformed
A through its controlled pattern watcher, waited for that subscription to close, and then exact-read valid B through
the same Manager. On the baseline:

```text
go test ./pkg/lifecycle -run '^TestLifecycleMatchingWatchPoisonDoesNotBlockUnrelatedExactRead$' -count=1
--- FAIL: TestLifecycleMatchingWatchPoisonDoesNotBlockUnrelatedExactRead
unrelated exact read after matching watch poison: lifecycle.Manager.getEntity:
authoritative graph state requires operator reset and canonical reingest failed:
graph_state_reset_required: noncanonical_predicate
```

The original file was restored from the backup and its MD5 re-verified as
`1b171981919e21b5ff8b41f0fea25f44` before the target test file was restored. After final goroutine-cleanup edits, the
current test-file MD5 is `1f3cd9c2a63f8242e4e07d34de0dce22`.

The production request/reply characterization is separate at
`pkg/lifecycle/graph_state_contract_test.go:109`: the production reply classifier retains fatal
`graph_state_reset_required` plus reason/entity detail while reconstructing a plain cause rather than
`*graph.StateContractError`; no mutation is emitted.

## Ruling conformance

| Reviewed ruling | Implementation evidence |
|---|---|
| Delete Manager-wide latch/guard/barriers/degradation lifecycle | `pkg/lifecycle/manager.go:47`; `pkg/lifecycle/manager_query.go:205`; deletion grep recorded below |
| Exact poison affects only the requested entity and emits no partial/mutation | `pkg/lifecycle/manager.go:205`; `pkg/lifecycle/graph_state_contract_test.go:16`; `pkg/lifecycle/graph_state_contract_test.go:47` |
| Repaired A is evaluated on its next real read | `pkg/lifecycle/graph_state_contract_test.go:16` |
| List filters workflow before decode; matching poison fails without partial | `pkg/lifecycle/manager_query.go:36`; `pkg/lifecycle/graph_state_contract_test.go:62` |
| Each watch owns one workflow-pattern subscription | `pkg/lifecycle/manager.go:30`; `pkg/lifecycle/manager_query.go:205`; `pkg/lifecycle/reader_capabilities_test.go:9` |
| Matching poison emits no output/mutation and closes only that subscription | `pkg/lifecycle/manager_query.go:230`; `pkg/lifecycle/watch_atomic_bootstrap_test.go:62`; `pkg/lifecycle/watch_atomic_bootstrap_test.go:113` |
| One WARN with workflow/entity/revision/code/reason | `pkg/lifecycle/manager_query.go:257`; `pkg/lifecycle/watch_atomic_bootstrap_test.go:134` |
| Transport close is local; cancellation is quiet | `pkg/lifecycle/manager_query.go:242`; `pkg/lifecycle/watch_atomic_bootstrap_test.go:174`; `pkg/lifecycle/watch_atomic_bootstrap_test.go:212` |
| No terminal channel/status/metric/config/API | Existing `Watch`/`WatchEvents` signatures remain at `pkg/lifecycle/manager_query.go:142` and `pkg/lifecycle/manager_query.go:177`; lifecycle spec at `openspec/specs/lifecycle/spec.md:219` |
| Active GS-01 target no longer requires lifecycle guard | `openspec/changes/establish-graph-read-write-foundation/specs/framework-composition/spec.md:13`; `openspec/changes/establish-graph-read-write-foundation/tasks.md:76`; successor disposition at `openspec/changes/establish-graph-read-write-foundation/successor-dispositions/r1b-lifecycle-guard.md:1` |
| ADR/spec/package current truth | `docs/adr/092-lifecycle-poison-localization.md:1`; `openspec/specs/lifecycle/spec.md:170`; `pkg/lifecycle/doc.go:24` |
| Ordered poison-A then later valid-B production-wire proof | `test/e2e/scenarios/lifecycle/scenario.go:326` |

There are no deviations from the reviewed contract.

## Deletion and historical-integrity evidence

The required forbidden-symbol grep over `pkg/lifecycle` returned zero matches. Production lifecycle contains zero
`WatchAll`; only `fakeBucket.WatchAll` remains because that fixture must implement the broad third-party
`jetstream.KeyValue` interface, and it panics if called. The package-local `entityStatesReader` method set is exactly
`ListKeys` and `Watch`.

ADR hashes before and after implementation are unchanged:

```text
e4d4362756571ca19eef24bf5e7ed835c16f685f6c99e01d101bc1ab3a57cebe  ADR-079
df5f6692225a4eddc2a4382592467ce943e329a466bc59664e30ca83f44635ec  ADR-081
```

The accepted GS-01 design, approval, design review, inventory, inventory review, and implementation evidence have no
diff. Their historical guard statements are intentionally preserved; the successor disposition carries current truth.

## Verification record

```text
GOCACHE=/private/tmp/semstreams-r1b-gocache go test ./pkg/lifecycle ./test/e2e/scenarios/lifecycle -count=1
ok github.com/c360studio/semstreams/pkg/lifecycle
?  github.com/c360studio/semstreams/test/e2e/scenarios/lifecycle [no test files]

GOCACHE=/private/tmp/semstreams-r1b-gocache go test -race ./pkg/lifecycle -count=1
ok github.com/c360studio/semstreams/pkg/lifecycle

openspec validate establish-graph-read-write-foundation --strict
Change 'establish-graph-read-write-foundation' is valid

openspec validate --all --strict
Totals: 36 passed, 0 failed (36 items)

GOCACHE=/private/tmp/semstreams-r1b-gocache go test ./test/contract/... -count=1
ok github.com/c360studio/semstreams/test/contract

GOCACHE=/private/tmp/semstreams-r1b-full-gocache task check:push
PASS: build, lint, default/integration/live_llm vet, schema drift, contract tests,
full race unit suite, and Docker-backed integration suite

task e2e:lifecycle
PASS: lifecycle scenario completed successfully in 211.631167ms; Docker stack and volume removed
```

The first sandboxed full-gate attempt could not bind local test sockets and was discarded as environment-only. Before
the unrestricted green rerun, the gate also found and prompted correction of a shifted entity-ID audit annotation and
an E2E function-length lint warning. The results above are from the corrected current worktree.
