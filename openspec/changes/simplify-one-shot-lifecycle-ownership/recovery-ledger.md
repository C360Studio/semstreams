# Lifecycle simplification recovery ledger

This is the durable execution authority for `simplify-one-shot-lifecycle-ownership`. It exists because design approval,
a merged contract PR, strict OpenSpec validation, green tests for the old machinery, and runtime migration were
previously conflated. Compaction, a new agent session, or a later PR summary must start here rather than infer state
from proposal language.

## Binding truth

- The owner-approved simpler design was merged as documentation; the repository-wide runtime migration was not.
- No accepted implementation was lost in a merge. At recovery baseline `main`
  `9fcc841ee792a080a7b9998bfb51400cd81b24fe`, only one of the historical 42 production owners had been migrated away
  from the lifecycle framework.
- PR #987 removed zero owners. PR #988 removed one owner. Draft PR #990 is boot-only composition work and receives
  zero lifecycle-migration credit.
- Green tests currently demonstrate consistency of machinery that must be removed. They do not demonstrate that the
  target design has landed.
- The runtime migration is incomplete until every mechanical zero gate and every positive proof in this ledger passes.

## Freeze and authority

Until this ledger and its OpenSpec reconciliation are durable, no lifecycle implementation branch or PR #990 may
merge. The freeze lifts only in the ordered sequence below; it does not authorize parallel speculative lifecycle work.

This change is the sole tracker for:

- migration from `Generation`, `Operation`, `StopWithQuiesce`, and their manufactured lifecycle semantics;
- owner-local cancel, done/WaitGroup, native handle, failed-Start, and `startDone` ownership;
- removal of the old lifecycle helper surface and its behavior-preserving tests;
- controlled restart, dirty recovery, settlement ordering, and final deletion proof for that migration.

Other active changes have narrow, non-overlapping authority:

- `restore-go-lifecycle-ownership` owns stored-context and invented-root removal only;
- `require-restart-for-config-activation` owns boot-only component composition and rules-only hot reload only;
- suspended or future changes cannot claim lifecycle migration credit by cross-reference.

Do not create another lifecycle simplification spec or duplicate these tasks. A newly discovered defect may have an
issue or evidence record, but completion is recorded here. Changing this boundary requires explicit owner approval.

## Archetype-family execution authority — 2026-08-19

On 2026-08-19 the owner approved replacing steady-state one-owner-at-a-time execution with bounded archetype-family
waves. This changes execution granularity only; it does not change the approved lifecycle target, owner-migrated
definition, completion vocabulary, or any gate.

A family wave is authorized only when its reviewed inventory and design:

- freeze the exact owner membership, baseline commit, shared lifecycle contract, and member-specific exceptions before
  implementation;
- group only genuinely equivalent ownership shapes and make no opportunistic additions after review;
- isolate owners with distinct native protocols, context/root debt, manager admission, provider boundaries,
  observation collisions, or unresolved prerequisites into separately reviewed exceptions;
- preserve per-owner behavior proof, focused race evidence, source identities, and exact per-owner plus wave census
  movement;
- inherit the one independently passed global inventory and target-wave design while exact membership, contract,
  exceptions, API rulings, and measured premises remain unchanged; each wave still requires TDD evidence and
  independent implementation review before owner-migrated credit;
- trigger renewed inventory/design review only for a membership split, source/census drift that changes a premise, a
  new or changed exported surface, a new native/context/observation exception, or a prerequisite API-shape change;
  ordinary reviewed-ancestor commits are not drift; and
- withhold completion credit for a failed wave and block only its declared dependents. Any independent reviewed wave
  whose prerequisites are complete may proceed concurrently in an isolated worktree; there is no single global
  next-wave lock.

This process approval does not check task 2.1, 2.2, or 2.3; does not grant owner, Gate A, Gate B, Gate C,
runtime-migration, proof, release, archive, or tag credit; and does not weaken any Gate A/B/C, test-surface,
positive-proof, archive, or tag requirement. Unique and protocol-specific exceptions remain single coherent owner
slices when the reviewed inventory cannot establish a genuine family.

### Reviewed dependency authority

The reviewed global wave artifact records the full DAG. Execution status is dependency-based, not ordinal. R1, SM1,
and ML1 have no implementation dependency. R1 births final
`internal/lifecyclecleanup.RollbackFailedStart(parent, rollback)`. I1 depends on R1; G1/M1/CM1 depend on R1; S1
depends on I1; OT1 depends on I1; A1/O1/H1/OS1/RU1/GI1 depend on S1; N1 depends on every owner wave and all
exported/adopter proof. R1 is the selected first helper-birth family, but selection is not an exclusive repository-wide
lock: SM1 and ML1 may proceed independently once the global design is finally accepted. A wave may be `blocked`,
`ready`, `in progress`, `implementation review`, or `complete`.

### Rejected standalone F0 worktree state — 2026-08-19

Owner ruling rejected the zero-owner F0, `lifecyclecleanup.Wait`, Background-root RollbackFailedStart signature, and
lifecyclejoin forwarding. Current uncommitted evidence is rejected and receives zero task/owner/gate/proof credit:

- untracked `internal/lifecyclecleanup/lifecyclecleanup.go`, SHA-256
  `fee8993472188a989f9444f5a325cd12366446e3ba73edbe4d760ab8481aac9e`;
- untracked `internal/lifecyclecleanup/lifecyclecleanup_test.go`, SHA-256
  `4822f3c209851babd446b03da5548ef8b26ea15c449b9cdd9a6092fb837eff82`;
- tracked diffs in `internal/lifecyclejoin/rollback.go` and `internal/lifecyclejoin/generation_test.go`, combined
  binary-diff SHA-256 `ea02c32427ff2ab79a20817e001010854081f106948c4d7ff845ef4a2cd02514`, 16 insertions/22
  deletions.

After this evidence is durable, cleanup restores only the two tracked lifecyclejoin files to baseline and removes only
the two untracked lifecyclecleanup files. It must not touch untracked `metrics-http-owner-inventory.md` or any other
change. No fragment is copied; R1 writes accepted parent-aware helper/tests from contract.

The hashes provide durable provenance only. Because the owner explicitly rejected this experiment, no recoverable
patch is required; after target-exact cleanup the bytes are intentionally discarded. Do not commit, stash, or copy
them into R1. If the owner wants recoverability despite rejection, the writer first materializes the actual patch—not
merely a hash.

## Completion vocabulary

Use only these terms in status reports, PR descriptions, and handoffs:

- **Contract complete:** the approved design text is merged. This says nothing about production code.
- **Checkpoint measured:** counts were reproduced at an exact commit with the searches defined below.
- **Owner migrated:** one production owner has no prohibited lifecycle dependency, follows its classified native
  ownership order, and passes focused race tests. This does not complete a gate or the runtime migration.
- **Implementation gate complete:** every owner and test assigned to that gate passes its stated exit criteria at an
  exact commit. It does not imply restart proof or release readiness.
- **Runtime migration complete:** all implementation gates and all production and test-surface zero gates pass.
- **Proof complete:** required controlled-process, dirty-process, settlement, race, integration, and relevant E2E
  evidence is recorded against the exact candidate commit.
- **Spec complete:** runtime migration and proof are complete and every remaining task is truthfully checked.
- **Tag ready:** spec complete, archive gates pass, CI is green, downstream migration notes are current, and the
  candidate tag commit is the proven commit.

Do not report unqualified status such as “done,” “closed,” “migrated,” “green,” or “validated.”
Use a qualified meaning above and name its commit.

## Pinned recovery checkpoint

Recovery baseline: merged `main` at `9fcc841ee792a080a7b9998bfb51400cd81b24fe`.

These are commit-qualified Git object searches executed from a clean measuring worktree; `main` was clean. They do not
include or describe the separate dirty keyed-dispatcher experiment recorded under the workspace checkpoint.

| Measurement | Count |
|---|---:|
| Production owner files importing `internal/lifecyclejoin` | 41 |
| `lifecyclejoin.NewGeneration` | 44 |
| `Generation.Stop` | 49 |
| External `Generation.Cancel` | 10 |
| External `Generation.Signal` | 0 |
| `Generation.StopWithQuiesce` | 8 |
| `lifecyclejoin.NewOperation` | 3 |
| `Operation.Run` | 3 |
| External `RunPartialStartRollback` calls | 21 |

The historical 42-owner census at `63a733a2378dff9f09c74c461ba776d352f79221` remains immutable in
[`inventory.md`](inventory.md). These recovery counts are a later checkpoint, not a correction to that census.

The checkpoint was reproduced with these production-only searches:

```text
git grep -l 'internal/lifecyclejoin' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n 'lifecyclejoin.NewGeneration' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n -E '(generation|Generation)\.Stop\(' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n '\.StopWithQuiesce(' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n -E '(generation|Generation)\.Cancel\(\)' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n 'generation.Signal(' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n 'lifecyclejoin.NewOperation' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n -E '(shutdownOp|poolStop|stopOp)\.Run\(' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
git grep -n 'lifecyclejoin.RunPartialStartRollback(' 9fcc841ee792a080a7b9998bfb51400cd81b24fe -- '*.go' ':!*_test.go'
```

Count complete output lines for each command; count unique lines for the owner-file search. The variable-name patterns
are a pinned-baseline reproduction aid, not a future-proof substitute for type-aware inspection.

### Measurement definitions

- **Production** means tracked `*.go` files excluding `*_test.go`, generated fixtures, vendored code, and archived
  OpenSpec artifacts.
- **Production owner file** means a production Go file importing `internal/lifecyclejoin`, counted once regardless of
  the number of symbols used.
- **External** means a call outside the defining `internal/lifecyclejoin` package. Tests are counted separately.
- **Generation.Stop** includes calls through variables or fields whose receiver is a `Generation`; the raw text count
  must be reconciled with type-aware inspection when naming differs.
- **Operation.Run** likewise includes every call on a lifecycle `Operation`, not unrelated methods named `Run`.
- **Rollback calls** count external uses of the old symbol. The approved bounded failed-Start invariant may retain an
  equivalent stateless helper, but the old package and symbol receive no exemption from the final zero gates.
- A checkpoint is valid only when it records the full commit, clean/dirty state, exact search commands, and results.
  Counts copied from a prior report are not a new checkpoint.

## Post-PR #997 merged-main checkpoint — 2026-08-18

Checkpoint measured on clean merged `main` at
`8117858367e1cc9d1dc434d211989e7a2ed1e552`. The measuring worktree had an empty porcelain status before this ledger
entry was added.

| Measurement | Count |
|---|---:|
| Production owner files importing `internal/lifecyclejoin` | 41 |
| `lifecyclejoin.NewGeneration` | 43 |
| `Generation.Stop` | 48 |
| External `Generation.Cancel` | 5 |
| External `Generation.Signal` | 0 |
| `Generation.StopWithQuiesce` | 8 |
| `lifecyclejoin.NewOperation` | 3 |
| `Operation.Run` | 3 |
| External `RunPartialStartRollback` calls | 20 |

These counts were reproduced from the merged commit with the same production-only searches defined by this ledger:

```text
git grep -l 'internal/lifecyclejoin' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n 'lifecyclejoin.NewGeneration' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n -E '(generation|Generation)\.Stop\(' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n '\.StopWithQuiesce(' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n -E '(generation|Generation)\.Cancel\(\)' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n 'generation.Signal(' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n 'lifecyclejoin.NewOperation' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n -E '(shutdownOp|poolStop|stopOp)\.Run\(' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
git grep -n 'lifecyclejoin.RunPartialStartRollback(' 8117858367e1cc9d1dc434d211989e7a2ed1e552 -- '*.go' ':!*_test.go'
```

PR #997 removed zero production owner files and earns zero lifecycle-migration, proof, release, archive, or tag credit.
Lower helper-call counts without a lower production-owner count are not owner migration. The next authorized action is
implementation Gate A; this checkpoint introduces no design change or completion claim.

## Gate A two-input implementation worktree checkpoint — 2026-08-18

Checkpoint measured in a dirty implementation worktree based on full commit
`cd6f570ec9fc8e0fed43eabb2c353b4de36a6d29`. The runtime/test slice is limited to
`input/file/file.go`, `input/file/file_lifecycle_test.go`, `input/http/http.go`, and the new
`input/http/http_lifecycle_test.go`; this ledger and `tasks.md` are the only task-truth additions. This is not a clean
candidate-commit checkpoint and earns no Gate A, proof, release, archive, or tag completion credit.

The two architect-approved S-owner candidates replace `Generation` with one private owner-local cancel function and
the existing lifecycle mutex, running flag, and wait group. Start publishes cancel/join authority before launching
work. Stop consumes cancel once, cancels before the caller-context-bounded join, reports a deadline honestly, and
makes a completed repeated Stop nil/no-op without concurrent execution, rejoin, or result replay. Redundant shutdown
and done channels are absent. `input/file` no longer invokes `component.StandardLifecycleTests`; both owners instead
have focused deterministic Start/Stop, parent-cancellation, blocked-join deadline, and completed-repeat tests.

Stable worktree source identities for this review are:

- `input/file/file.go`: `cba90b467715767bedcd26b21d15b31f04b20ba37832447fd171fd809c75d802`
- `input/file/file_lifecycle_test.go`: `8d55ae618e8abc9319d99aeb3e90b6abfd243c0538010017471646d44d2491db`
- `input/http/http.go`: `914a4171ac3011fa98ca7ca4f70db36f5b4563497113f17a35d5389bd15ed86b`
- `input/http/http_lifecycle_test.go`: `67c78b1e2970272880efd126bd25389c34d1f02b550dcbf8125bfce005db78ed`

| Ruling | Evidence | Checkpoint result |
|---|---|---|
| Owner-local allowed state only | `A01` | Independently approved for this slice. |
| Start publishes authority before launch | `A02` | Independently approved for this slice. |
| One-shot cancel precedes caller-bounded join | `A03` | Independently approved for this slice. |
| Honest timeout, no rejoin, completed repeat nil | `A04` | Independently approved for this slice. |
| Prohibited mechanisms absent | `Z01` | Independently approved for this slice. |
| Focused behavior and race tests | `T01` | Independently approved for this slice. |
| Required census movement | `C01` | Independently approved delta; no gate credit. |
| Adjacent outputs remain out of slice | `U01` | Approved Q-primary/F classification; zero credit. |

Independent `semstreams-reviewer` verdict on 2026-08-18: `CORRECTIONS CONFIRMED`. The verdict approves `A01`-`A04`,
`Z01`, `T01`, `C01`, and `U01` for this exact two-owner slice and the stable source identities above. It does not
approve Gate A or any broader runtime-migration or proof claim.

Exact anchor definitions:

- `A01`: owner state at `input/file/file.go:104-110` and `input/http/http.go:78-83` is limited to the lifecycle
  mutex, private cancel function, running flag, and WaitGroup; no context is stored.
- `A02`: `input/file/file.go:379-401` and `input/http/http.go:251-272` validate Start context, derive the run context,
  and publish cancel, running, and WaitGroup ownership before launching work.
- `A03`: `input/file/file.go:408-437` and `input/http/http.go:284-313` consume and clear cancel once under the
  lifecycle mutex, cancel before waiting, and bound only the join with the caller context.
- `A04`: production repeat/timeout branches are `input/file/file.go:413-436` and `input/http/http.go:289-312`.
  Behavior proof is `input/file/file_lifecycle_test.go:65-82,98-143` and
  `input/http/http_lifecycle_test.go:57-83,106-132`.
- `T01`: focused behavior proof spans `input/file/file_lifecycle_test.go:65-143` and
  `input/http/http_lifecycle_test.go:57-132`; the focused race command and result are recorded below.

The compact search evidence for the table is:

```text
Z01:
! git grep -n -E \
  -e 'internal/lifecyclejoin|lifecyclejoin\.|Generation|StopWithQuiesce' \
  -e 'NewOperation|RunPartialStartRollback|shutdown[[:space:]]+chan' \
  -e 'done[[:space:]]+chan|\.shutdown|\.done|rejoin' \
  -- input/file/file.go input/http/http.go
result: no matches

C01:
git grep -l 'internal/lifecyclejoin' -- '*.go' ':!*_test.go' | wc -l
=> 39
git grep -n 'lifecyclejoin.NewGeneration' -- '*.go' ':!*_test.go' | wc -l
=> 41
git grep -n -E '(generation|Generation)\.Stop\(' -- '*.go' ':!*_test.go' | wc -l
=> 46

U01: git diff --quiet -- output/file/file.go output/httppost/httppost.go
result: exit 0; both output files untouched
```

No deviation is recorded for this two-owner slice. The independently approved dirty-worktree findings grant
owner-migrated credit to `input/file` and `input/http` only; they do not complete Gate A.

Focused race evidence from this worktree:

```text
go test -race ./input/file ./input/http -count=1 -timeout=120s
ok github.com/c360studio/semstreams/input/file 1.302s
ok github.com/c360studio/semstreams/input/http 1.432s
```

The production-only recovery searches defined above report this exact delta from the post-PR #997 checkpoint:

| Measurement | Post-PR #997 | This worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 41 | 39 | -2 |
| `lifecyclejoin.NewGeneration` | 43 | 41 | -2 |
| `Generation.Stop` | 48 | 46 | -2 |
| External `Generation.Cancel` | 5 | 5 | 0 |
| External `Generation.Signal` | 0 | 0 | 0 |
| `Generation.StopWithQuiesce` | 8 | 8 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |
| `Operation.Run` | 3 | 3 | 0 |
| External `RunPartialStartRollback` calls | 20 | 20 | 0 |

Owner-migrated credit is granted to `input/file` and `input/http` only. Gate A remains incomplete because its remaining
assigned owners and gate-wide exit criteria are not complete. Runtime migration, proof, release, archive, and tag
completion remain incomplete.

The current architect reclassification is binding for the adjacent output owners and changes no historical inventory:

- `output/file/file.go` is Q-primary with an F facet, not S, pending exact native subscription/consumer handles,
  protocol-specific callback-admission ordering, and retained partial-Start cleanup authority.
- `output/httppost/httppost.go` is Q-primary with an F facet, not S, pending exact native subscription/consumer
  handles, protocol-specific callback-admission ordering, and retained partial-Start cleanup authority.

Neither output owner is edited or receives migration credit in this checkpoint.

## Graph-index parent and dispatcher implementation worktree checkpoint — 2026-08-18

Checkpoint measured in a dirty implementation worktree based on merged `main` at full commit
`e7789f6cf5714e5b5fb04c0221cb9b2def17d3a0`. The runtime/test slice is limited to the graph-index parent and its
private keyed dispatcher: `processor/graph-index/component.go`, `processor/graph-index/keyed_dispatcher.go`, focused
lifecycle proof in `processor/graph-index/lifecycle_order_test.go`, and the three existing dispatcher test-cleanup
call sites in `processor/graph-index/ordered_dispatch_test.go` and
`processor/graph-index/owner_filter_load_integration_test.go`. This ledger and `tasks.md` are the only task-truth
additions. This is not a clean candidate-commit checkpoint and earns no Gate A, proof, release, archive, or tag
completion credit.

Stable worktree source identities for this checkpoint are:

- `processor/graph-index/component.go`: `01d83a26fc2affc4136da86d60a5bb31a49e070716ba3d8ac6a8dca288711f5e`
- `processor/graph-index/keyed_dispatcher.go`: `45added4af11d00c1a4eae158c612f868648e3e0f54f802b169060468503ede1`
- `processor/graph-index/lifecycle_order_test.go`: `1689efb74f06d970b91b29b8a3b4355e2b4ace04829ced2f4a3cadaba5acad1f`
- `processor/graph-index/ordered_dispatch_test.go`: `0a337acf0e8ae9aab274acf8220c8a83a77bc93ca8e3a4d819077a77bcc0c7dc`
- `processor/graph-index/owner_filter_load_integration_test.go`:
  `3d3e1fb464bdd09a502a4b140ea464b6b7eec5623d273a2f346cd9655ffb4711`

| Ruling | Exact implementation evidence | Checkpoint result |
|---|---|---|
| Parent adds only failed-Start phase state | `processor/graph-index/component.go:241-253` | Existing lifecycle/resource fields remain; only private `cleanupPending` was added and no context is stored. |
| Authority precedes escaping work | `processor/graph-index/component.go:608-650,698-712` | Cancel/runDone/cleanupPending publish first; the one runDone waiter starts after all Add sites are sealed. |
| Failed Start uses bounded exact cleanup | `processor/graph-index/component.go:627-651,781-832` | Exact subscriptions drain before cancel; runDone, coalescer.done, and pool.done are awaited under the terminal bound. |
| Failed-Start authority alone is retryable | `processor/graph-index/component.go:589-595,725-752` | Start rejects retained cleanup; later Stop clears authority only after complete cleanup. |
| Normal Stop remains one-shot | `processor/graph-index/component.go:753-779` | Cancel authority is claimed once; timeout is honest and later normal Stop is a no-op. |
| Dispatcher is a parent-owned child | `processor/graph-index/keyed_dispatcher.go:12-74` | Generation and independent Stop are absent; parent cancellation plus exact done replaces them. |
| Channel-synchronized behavior proof | `processor/graph-index/lifecycle_order_test.go:127-214` | Failed cleanup retention/retry and dispatcher-child join are proved without sleep-based synchronization. |
| Existing dispatcher cleanup follows target | `processor/graph-index/ordered_dispatch_test.go:46-59,378-403`; `processor/graph-index/owner_filter_load_integration_test.go:392-397` | Tests cancel their exact parent context and await private done. |
| Census moves by the reviewed owner count | searches and table below | Dispatcher receives owner-migrated credit; the already lifecyclejoin-free parent receives none. |

Focused and package race evidence from this worktree:

```text
go test -race ./processor/graph-index -run \
  'TestComponent(StartFailure|FailedStart|Stop)|TestKeyedDispatcher' -count=1 -timeout=120s
ok github.com/c360studio/semstreams/processor/graph-index 1.341s

go test -race ./processor/graph-index -count=1 -timeout=120s
ok github.com/c360studio/semstreams/processor/graph-index 1.798s
```

The production-only recovery searches defined above report this exact delta from merged baseline `e7789f6c`:

| Measurement | Merged baseline | This worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 39 | 38 | -1 |
| `lifecyclejoin.NewGeneration` | 41 | 40 | -1 |
| `Generation.Stop` | 46 | 45 | -1 |
| External `Generation.Cancel` | 5 | 5 | 0 |
| External `Generation.Signal` | 0 | 0 | 0 |
| `Generation.StopWithQuiesce` | 8 | 8 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |
| `Operation.Run` | 3 | 3 | 0 |
| External `RunPartialStartRollback` calls | 20 | 20 | 0 |

Owner-migrated credit is granted to `processor/graph-index/keyed_dispatcher.go` only. The graph-index parent already
had no production `internal/lifecyclejoin` import at the merged baseline, so its failed-Start correction receives no
owner-count credit. Task 2.3, Gate A, runtime migration, proof, release, archive, and tag completion remain incomplete.

## Dispatcher Stop-error prerequisite worktree checkpoint — 2026-08-18

Dirty worktree baseline: merged `main` `0f7687a7f371be7e937b70fe27d2a1e9f5587eba`. Scope is limited to
`pkg/dispatch/{dispatcher.go,completion_watcher.go,doc.go}` and focused tests. This prerequisite corrects the exact
`BoundedDispatcher` handle needed by later owner migrations and grants zero owner, Gate, proof, release, archive, or
tag credit.

| Ruling | Exact evidence | Result |
|---|---|---|
| Nil Stop context rejects before action | `pkg/dispatch/dispatcher.go:196-199`; `pkg/dispatch/dispatcher_test.go:272-285` | Pool remains usable after rejection. |
| Finished caller context receives zero pool budget | `pkg/dispatch/dispatcher.go:200-230`; `pkg/dispatch/dispatcher_test.go:287-359` | Both canceled and expired contexts return promptly. |
| Pool and caller failures are returned | `pkg/dispatch/dispatcher.go:231-239`; `pkg/dispatch/dispatcher_test.go:287-359` | Error matches `worker.ErrStopTimeout` and the exact context cause. |
| Completion watcher is canceled and bounded | `pkg/dispatch/completion_watcher.go:180-195`; `pkg/dispatch/completion_watcher_test.go:84-132` | Unobserved callback join returns the caller context error. |
| Failed Stop is terminal; completed repeat is nil | `pkg/dispatch/dispatcher.go:23-26,183-195`; `pkg/dispatch/doc.go:94-102`; `pkg/dispatch/dispatcher_test.go:259-269` | No retry/rejoin/result state was added. |

Evidence:

```text
go test -race ./pkg/dispatch -run 'TestDispatcher_Stop|TestCompletionWatcherStop' -count=1 -timeout=120s
ok github.com/c360studio/semstreams/pkg/dispatch 1.446s

go test -race ./pkg/dispatch -count=1 -timeout=120s
ok github.com/c360studio/semstreams/pkg/dispatch 1.850s

go test -tags=integration -race ./pkg/dispatch -count=1 -timeout=120s
ok github.com/c360studio/semstreams/pkg/dispatch 4.847s

task lint
PASS
```

The real-NATS integration gate proves the existing successful completion-watcher path still joins. It does not claim
a real-NATS pool-timeout injection; channel-synchronized unit proof covers the failed boundary. Production lifecycle
counts remain unchanged from the baseline: owner files 38, NewGeneration 40, Generation.Stop 45, external Cancel 5,
StopWithQuiesce 8, NewOperation 3, Operation.Run 3, and old rollback calls 20.

## Gated-DAG one-shot owner worktree checkpoint — 2026-08-18

Dirty worktree baseline: merged `main` `a1a68a784d6f82c5f6cbfb81fe81c86945eeda67`. Runtime/test scope is limited
to `processor/gated-dag/{executor.go,component.go,executor_lifecycle_test.go,executor_integration_test.go}`; this ledger
and `tasks.md` are the only task-truth additions. This checkpoint grants owner-migrated credit only to
`processor/gated-dag/executor.go`; Gate A, runtime migration, proof, release, archive, and tag completion remain
unchecked and incomplete.

Stable worktree source identities:

- `processor/gated-dag/executor.go`: `b772ebc214679ab8a688b833b17428e1566fae2c83a85058a5aad9c57eded28e`
- `processor/gated-dag/component.go`: `1b867b93425165b4c7f6a2540fa38ede92465876a182da115b4b1be2e3a5062f`
- `processor/gated-dag/executor_lifecycle_test.go`:
  `2ab86ca28c0d78cc4bddaf47d5edd0ab1fade8d89093529e7757bc2a6d93e747`
- `processor/gated-dag/executor_integration_test.go`:
  `90f911716bea721046bc60d6a177319212cc143735c92fdaa8fa97dae20d352a`

| Ruling | Exact implementation evidence | Checkpoint result |
|---|---|---|
| Owner-local lifecycle only | `processor/gated-dag/executor.go:71-73,188-194` | `Generation` is replaced by one private cancel, one done channel, and the existing WaitGroup; no context is stored. |
| Exact dispatcher and failed-Watch rollback | `processor/gated-dag/executor.go:95-123`; `processor/gated-dag/executor_lifecycle_test.go:90-104` | The exact dispatcher is retained; failed lifecycle-Watch acquisition returns the joined Watch and dispatcher rollback result. |
| Goroutine-local KV watcher | `processor/gated-dag/executor.go:374-407` | The exact watcher stays local to its goroutine and LIFO defers stop it before WaitGroup completion; no watcher catalog or field is added. |
| One-shot Stop order | `processor/gated-dag/executor.go:207-227`; `processor/gated-dag/executor_lifecycle_test.go:14-40` | Stop consumes cancel once, cancels, stops the dispatcher, and bounds the existing done join; later executor Stop is nil/no-op. |
| Boot-only parent and honest failed Stop | `processor/gated-dag/component.go:136-149,272-274,297-320`; `processor/gated-dag/executor_lifecycle_test.go:42-88` | The successful executor pointer is retained as used-instance evidence; a claimed failed Stop cannot become later Component success or healthy runtime. |
| Fresh-component restart proof | `processor/gated-dag/executor_integration_test.go:189-218` | Same-instance restart rejects; a fresh Component boots against retained NATS and reconciles authoritative state. |
| Touched sleep removal | `processor/gated-dag/executor_integration_test.go:34-89,146-184` | Graph-ingest readiness is observed through request/reply and dedup is driven by explicit passes/submission counts; the touched file has no `time.Sleep`. |

TDD red evidence:

```text
go test -race ./processor/gated-dag -run \
  'TestExecutorStop|TestComponent(FailedStop|CompletedStop|RejectsSameInstance)|TestExecutorStartWatchFailure' \
  -count=1 -timeout=120s
=> build failed: executor had no owner-local cancel/done fields

go test -tags=integration -race ./processor/gated-dag -run TestIntegration_BootReconcile -count=1 -timeout=120s
=> failed: the old test attempted same-instance Start after completed Stop and received the required boot-only rejection
```

Green evidence:

```text
go test -race ./processor/gated-dag -count=1 -timeout=120s
ok github.com/c360studio/semstreams/processor/gated-dag 1.348s

go test -tags=integration -race ./processor/gated-dag -count=1 -timeout=180s
ok github.com/c360studio/semstreams/processor/gated-dag 8.869s
```

The production-only recovery searches report this exact delta from baseline `a1a68a78`:

| Measurement | Baseline | Worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 38 | 37 | -1 |
| `lifecyclejoin.NewGeneration` | 40 | 39 | -1 |
| `Generation.Stop` | 45 | 44 | -1 |
| External `Generation.Cancel` | 5 | 4 | -1 |
| External `Generation.Signal` | 0 | 0 | 0 |
| `Generation.StopWithQuiesce` | 8 | 8 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |
| `Operation.Run` | 3 | 3 | 0 |
| External `RunPartialStartRollback` calls | 20 | 20 | 0 |

## Gate A BaseService dirty-worktree checkpoint — 2026-08-19

This dirty implementation checkpoint is based on clean merged `main`
`c5953972bbc56f013bf4665674e99f03c11395f6`. The owner-selected handoff is
[`base-service-owner-slice.md`](base-service-owner-slice.md), SHA-256
`663094c67f65b444b2539ea9861cd51dd60f692b83eedb67817db68172c3f114`. The handoff limits production credit to
`service/base.go` and includes only BaseService-specific test correction propagation.

Exact reviewed file hashes:

- `service/base.go`: `470cddafe9ffbd5d48f655ca05ee178e0228fe3cafd437433435ac729d202856`
- `service/base_lifecycle_test.go`: `8b37f82a66a33e44c0cad2cfcac879c2657628a9f113d5dc00051f2d8cfad7ca`
- `service/base_test.go`: `44e283023ba248eacf09bf26be942397c9ea71b5a9b42f10623a72d78dbb4d5d`
- `service/lifecycle_context_contract_test.go`:
  `ce575266e5ba10a6dc3a7bc492e754171bb7b78e7053dbe8965e9cb64c19cfcb`
- `service/service_manager_stopall_test.go`:
  `761e9c06733a68c0c538918b98a3c1ecfcd6c8ec5eec95c10a6a6bc2307465e9`
- `openspec/changes/simplify-one-shot-lifecycle-ownership/base-service-owner-slice.md`:
  `663094c67f65b444b2539ea9861cd51dd60f692b83eedb67817db68172c3f114`

The final independent `semstreams-reviewer` verdict is `APPROVE` with no findings. Its conformance rulings are:

- `service/base.go:111-114,254-291` replaces `Generation` and retained terminal results with private cancel,
  done, and WaitGroup authority; no context is retained, and authority is published before owned goroutines escape.
- `service/base.go:232-247` rejects nil, already-canceled, and same-instance reused Start before starting new work.
- `service/base.go:296-349` makes Stop one-shot: it consumes cancel, stops ticker admission, cancels before the exact
  caller-context-bounded join, returns timeout honestly, and gives later calls no rejoin or result-replay path.
- `service/base.go:281-290,460-478` publishes `StatusStopped` only after both owned goroutines return; parent
  cancellation converges through that same exact owner join.
- `service/base_lifecycle_test.go:11-134` deterministically covers canceled Start, canceled/deadline Stop, no rejoin,
  same-instance rejection, Stop-before-Start terminal use, parent cancellation, and owner completion.
- `service/base_test.go:291-329` proves restart by fresh composition rather than same-instance generation reuse.
- `service/lifecycle_context_contract_test.go:55-68,89-101` retains nil-context and cancel-before-wait contract coverage
  while removing tests that required shared Stop completion or later rejoin.
- `service/service_manager_stopall_test.go:27-29,125-136` corrects wording to exact completion and proves completed
  repeated BaseService Stop without treating `StatusStopping` as completion. No Manager behavior changed.

The developer supplied the following historical TDD red transcript. The reviewer found the failures consistent with
the final change but marked TDD history `UNVERIFIED` because no in-tree or CI artifact preserves the runs. Record it
as qualified execution history, not durable proof.

```text
go test -race ./service -run \
  '^TestBaseService(StartRejectsCanceledContext|StopIsOneShotAfterCanceledJoin|'\
'CompletedStopRejectsSameInstanceRestart)$' \
  -count=1 -timeout=20s
=> FAIL: canceled Start wanted context.Canceled, got nil; repeated Stop timed out after 1s while rejoining blocked
   work; same-instance restart wanted an error, got nil; package failed in 1.509s

go test -race ./service -run '^TestBaseServiceStopIsOneShotAfterFailedJoin$' -count=1 -timeout=20s
=> FAIL: canceled and deadline subtests wanted StatusStopping (3), got StatusStopped (0):
   "repeat Stop must not predict owner completion"; package failed in 0.513s

go test -race ./service -run '^TestBaseServiceStopBeforeStartRejectsSameInstanceStart$' -count=1 -timeout=20s
=> FAIL: base_lifecycle_test.go:117 wanted an error, got nil; package failed in 0.500s
```

Final green evidence after the reviewer-required corrections:

```text
# Independent root/Program Manager verification, before the final comment-only correction
go test -race ./service -count=1 -timeout=180s
=> ok github.com/c360studio/semstreams/service 7.045s

go test -tags=integration -race ./service -run '^TestService_FreshInstanceRestart$' -count=1 -timeout=120s
=> ok github.com/c360studio/semstreams/service 3.599s

task lint
=> PASS

openspec validate simplify-one-shot-lifecycle-ownership --strict --no-interactive
=> Change 'simplify-one-shot-lifecycle-ownership' is valid

git diff --check
=> PASS

# Developer rerun after the final comment-only correction
go test -race ./service -count=1 -timeout=180s
=> ok github.com/c360studio/semstreams/service 6.703s

go test -race ./service -run \
  '^(TestServiceManager_StopAll_Idempotency|TestBaseService_CompletedStopIsIdempotent)$' \
  -count=1 -timeout=30s
=> ok github.com/c360studio/semstreams/service 1.472s

git diff --check
=> PASS
```

The production-only recovery census has this exact baseline-to-worktree delta:

| Measurement | Baseline | Worktree | Delta |
|---|---:|---:|---:|
| Production owner files importing `internal/lifecyclejoin` | 37 | 36 | -1 |
| `lifecyclejoin.NewGeneration` | 39 | 38 | -1 |
| `Generation.Stop` | 44 | 43 | -1 |
| External `Generation.Cancel` | 4 | 4 | 0 |
| External `Generation.Signal` | 0 | 0 | 0 |
| `Generation.StopWithQuiesce` | 8 | 8 | 0 |
| `lifecyclejoin.NewOperation` | 3 | 3 | 0 |
| `Operation.Run` | 3 | 3 | 0 |
| External `RunPartialStartRollback` calls | 20 | 20 | 0 |

Owner-migrated credit is limited to `service/base.go`. This remains a dirty-worktree checkpoint: task 2.3 and Gate A
remain incomplete, and it grants no controlled/dirty proof, release, archive, or tag credit.

## PR #1001 integration-runner interrupt prerequisite inventory — 2026-08-19

The repeated CI failure in `TestIntegrationRunner_InterruptReapsPullBeforeReleasingLock` is inventoried at
[`testinfra-interrupt-inventory.md`](testinfra-interrupt-inventory.md), baseline
`6d9d754af2f13d0f09145ed34ce81f3d8b013885`, SHA-256
`b183233d955680d4f00fcb8749b7a4b09370fa24479f79d476f8427093024737`. This is an inventory-only testinfra
prerequisite. Independent review returned `INVENTORY PASS`. The narrowed D1 target is recorded at
[`testinfra-interrupt-design.md`](testinfra-interrupt-design.md), SHA-256
`c0f8754b241741c45523b00b2007a668e16f38b676a1c31138f960f766a3e764`. It grants zero lifecycle migration,
restart-proof, release, archive, or tag credit. Independent review returned `DESIGN PASS`, and the owner approved the
narrowed D1 implementation against that exact design hash.

Dirty implementation checkpoint at baseline `6d9d754af2f13d0f09145ed34ce81f3d8b013885` is limited to
`test/testinfra/integration_runner_contract_test.go`, SHA-256
`81b04d4bca469dfb6df79d221afd9a0b4fc03f3d51379e4f40814d6a950d995f`. The inventory, design, and this ledger are the
only test-truth additions. No production script, environment contract, exported API, Docker resource, or sister
repository changed.

| D1 ruling | Exact evidence | Checkpoint result |
|---|---|---|
| Stable termination case and causal helper | `test/testinfra/integration_runner_contract_test.go:206-245,355-399,688-691` | The renamed case re-execs only its fake pull. The helper installs `SIGTERM` notification before publishing its PID and gives the inherited release pipe one reader: pre-TERM release or EOF exits successfully; after TERM, the helper acknowledges TERM and joins that same release read before exit. |
| Parent-ready follows Bash PID retention | `test/testinfra/integration_runner_contract_test.go:228-234,263-276,301-306` | The private date wrapper signals only after the helper PID marker exists; this runner call occurs after Bash retained `$!`. |
| Exact four-pipe ownership | `test/testinfra/integration_runner_contract_test.go:247-299,401-465` | The runner case passes parent-ready, TERM-ack, release, and reap-check endpoints. Its waiter and release-first cleanup register immediately after successful Start, before fallible inherited-endpoint closes. The direct pre-TERM case uses only two `/dev/null` descriptor placeholders and the release pipe. |
| Lock remains while acknowledged child is blocked | `test/testinfra/integration_runner_contract_test.go:307-325` | The runner receives `SIGTERM`; exact lock-owner bytes remain unchanged, `kill -0` proves the PID exists, and the private `rmdir` mutation probe refuses removal. |
| Reap precedes lock removal | `test/testinfra/integration_runner_contract_test.go:235-245,327-352` | The wrapper refuses exact lock removal while the PID exists, including zombie state; only PID absence emits reap acknowledgement and delegates real `rmdir`. |
| Actual exit, never timeout-as-success | `test/testinfra/integration_runner_contract_test.go:333-348,441-464,779-830` | The sole waiter returns an actual `*exec.ExitError` code 130 with exited `ProcessState`; typed timeout is a hard failure and no second `Cmd.Wait` exists. The pre-TERM case exact-matches the helper PID and proves it is absent after Wait. |
| Early cleanup converges on the same waiter | `test/testinfra/integration_runner_contract_test.go:280-299,438-450,795-830` | Cleanup is registered before any post-Start failure can escape, releases the helper first, kills the runner only while still live, and joins the existing waiter. |
| Adjacent contracts remain intact | `test/testinfra/integration_runner_contract_test.go:401-490,519-574` | The pre-TERM release regression, typed timeout result, and holder's bounded contention/release behavior use the same single-Wait ownership contract. |

TDD red evidence:

```text
go test -race ./test/testinfra -run TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock \
  -count=1 -timeout=30s
=> FAIL after 3.02s: wait for parent retained pull PID: i/o timeout
```

The failure is the intended missing causal milestone before the helper/date producer was wired. It is not accepted as
runner completion.

Correction red evidence after independent review found that early cleanup could close release before TERM:

```text
go test -race ./test/testinfra -run '^TestIntegrationRunnerFakePullHelper_PreTERMReleaseExits$' \
  -count=1 -timeout=20s
=> FAIL after 3.01s: helper did not accept pre-TERM release: command did not exit within 3s
```

The helper previously waited for TERM before reading release, so EOF could not converge early cleanup. The corrected
helper causally waits for either event and, if TERM wins, joins the same release read after acknowledging TERM.

Green focused evidence:

```text
go test -race ./test/testinfra -run \
  '^(TestIntegrationRunner_TerminationReapsPullBeforeReleasingLock|TestIntegrationRunnerFakePullHelper_PreTERMReleaseExits)$' \
  -count=20 -timeout=180s
PASS: both causal cases passed 20/20 under the race detector

go test -race ./test/testinfra -run \
  'TestIntegrationRunner_HostLockHasBoundedContentionDiagnostics|TestCommandWaiter' -count=1 -timeout=60s
ok github.com/c360studio/semstreams/test/testinfra 2.555s

go test -race ./test/testinfra -skip '^TestInfrastructurePolicyGuard$' -count=1 -timeout=120s
ok github.com/c360studio/semstreams/test/testinfra 6.964s

go test -race ./test/testinfra -count=1 -timeout=120s
LOCAL GATE NOT GREEN: TestInfrastructurePolicyGuard reported 742 findings under two excluded, user-owned
`.claude/worktrees/agent-*` trees. No reported finding points to this test file. The worktrees were not mutated or
removed; an isolated checkout/CI must run the full package gate.

task lint
PASS

openspec validate simplify-one-shot-lifecycle-ownership --strict --no-interactive
Change 'simplify-one-shot-lifecycle-ownership' is valid

git diff --check
PASS
```

The twenty termination runs each completed actual runner Wait, exit-code/state validation, post-reap acknowledgement,
PID absence, and lock absence. The twenty pre-TERM runs each completed the exact helper Wait and PID-absence proof.
Test cleanup left no owned process or FD endpoint. This prerequisite receives zero owner, lifecycle, Gate,
runtime-proof, release, archive, or tag credit. The full-package gate remains explicitly unverified until run without
the excluded user-owned worktrees in the repository scan root.

## Workspace recovery checkpoint

Authority collisions are inventoried in
[`authority-reconciliation-inventory.md`](authority-reconciliation-inventory.md) (SHA-256
`d495b4acb908bb846194eec0ce9c97076691bf3cba5d3143b8c2453335792f4e`); inventory only, with no completion credit.
The inventory-first reconciliation design is recorded in
[`authority-reconciliation-design.md`](authority-reconciliation-design.md) (SHA-256
`9af0ceeacae2dc0a854b0337f5eb6dd19710a90fde586944e7f9bca0b06df039`); design only, with no runtime or proof credit.
Applying the approved authority reconciliation changes no runtime-migration or proof completion credit.

PR #990 truth-reset inventory revision 1 was superseded before its first commit; its bytes are not a durable artifact.
This ledger retains only SHA-256 `a3e83b5843381b6d69183c959b0497be7c6d0f0d4538aade5f6604d637e69817`, measured from current-main baseline
`eb1f6d7758f75a2ff5598e2ca92af92e8c21d753` against historical PR head
`8f19ef3678a549913385b090e4de1766a7a43a27`, independent `semstreams-reviewer` verdict
`INVENTORY CHANGES REQUESTED`, and the zero implementation, lifecycle, proof, or release-credit outcome.

PR #990 truth-reset inventory revision 2:
[`pr990-truth-reset-inventory.md`](../require-restart-for-config-activation/pr990-truth-reset-inventory.md),
measured from the same baseline and historical head. SHA-256:
`5256057932030c7e854a3889ae2756fbec577870ee5e5c9c7c0e8ab86874541d`. Independent
`semstreams-reviewer` verdict: `INVENTORY PASS`. Revision 2
supersedes revision 1 only as the inventory submitted for re-review. PR #990
continues to receive zero implementation, lifecycle, proof, or release credit.

PR #990 binding boot-only disposition:
[`pr990-boot-only-disposition.md`](../require-restart-for-config-activation/pr990-boot-only-disposition.md), SHA-256
`40b2534b604a14f64aacbb8f4db86bdbc38129f3f114e0ac40118c9f7259fc41`, disposition baseline
`42f349b02bfa9517cff575a9c2a1af3094e591ce`, historical PR head
`8f19ef3678a549913385b090e4de1766a7a43a27`. Owner binding disposition: reject historical PR #990 as a merge,
rebase, commit-replay, or cherry-pick unit; permit only the recorded narrow reconstruction. This checkpoint grants
zero implementation, lifecycle, proof, release, archive, or tag credit.

The authoritative eight-worktree evidence, binding classifications, bounded preservation sets, and exact cleanup
scope are recorded in [`worktree-recovery-manifest.md`](worktree-recovery-manifest.md). No dirty lifecycle worktree may
be removed until that manifest is merged and its two bounded preservation artifacts are verified.

Known worktrees at approval time:

- **Draft PR #990:** `/private/tmp/semstreams-gh986-boot-only-flow-activation`, branch
  `codex/gh986-boot-only-flow-activation`, clean at `8f19ef36`.
- **Rejected cleanup experiment:** `/private/tmp/semstreams-generation-removal-1`, branch
  `codex/remove-generation-leaf-owners`, with uncommitted edits to
  `processor/graph-index/keyed_dispatcher.go` and a new lifecycle test.
- **Earlier rejected experiments:** separate legacy dirty worktrees. Inventory each before cleanup; none is
  implementation authority.

Workspace cleanup is ordered and evidence-preserving:

1. Enumerate every worktree with its path, branch or detached state, HEAD, porcelain status, changed-file list, and
   diff stat. Do not infer that two similarly named worktrees contain the same experiment.
2. For every dirty worktree, record a patch SHA-256 and classification: accepted, superseded, rejected, or unknown.
   Preserve the manifest before removing anything.
3. Treat the keyed-dispatcher experiment as rejected. It must not be committed, copied forward, or expanded into a
   generic admission/shutdown mechanism.
4. Remove rejected worktrees only after their manifest is durable. Do not touch unrelated Docker containers, sister
   repositories, or the clean PR #990 worktree.
5. Finish with one clean `main`, the clean PR #990 worktree, and one explicitly approved implementation worktree.
   Record the resulting `git worktree list` and porcelain status before implementation resumes.

## Ordered recovery sequence

### 1. Make the truth reset durable

- Land this ledger and reconcile the three active changes to the authority boundaries above.
- Keep all runtime tasks unchecked.
- Complete and preserve the workspace manifest and cleanup evidence.
- Reproduce the pinned checkpoint or record an explained delta before touching runtime code.

### 2. Re-review PR #990 as boot-only work

PR #990 is reviewed only after step 1. Its clean reference is worktree
`/private/tmp/semstreams-gh986-boot-only-flow-activation`, branch `codex/gh986-boot-only-flow-activation`, commit
`8f19ef36`.

The review must establish that it:

- preserves boot-only component topology and rules-only hot reload;
- does not introduce component hot reload, lifecycle wrappers, retained Stop results, or new restart machinery;
- remains understandable as ordinary composition code on the touched ComponentManager and Rule surfaces;
- is green and mergeable without relying on abandoned worktree changes.

If it passes, merge it before lifecycle owner migration because it changes owner surfaces. Assign it zero lifecycle
completion credit, then pin and reproduce a new full census on merged `main`. If it fails, narrow or close it; do not
repair it by adding lifecycle machinery.

### 3. Implementation gate A: simple and failed-Start owners

Migrate the rebased S and F owners, plus only leaf owners proven to have the same budget:

- direct owner-local cancel and done/WaitGroup joins;
- fence real local admission before cancellation when the owner actually has admission;
- preserve acquired native handles until cleanup succeeds;
- retain `cleanupPending` and reject Start when bounded failed-Start cleanup expires;
- The only final shared helper is `internal/lifecyclecleanup.RollbackFailedStart(parent, rollback)`, born with R1 and
  immediately bounded as `WithTimeout(WithoutCancel(parent), 5s)`. Exact completion waits remain owner-local. The old
  lifecyclejoin rollback implementation remains unchanged only for unmigrated owners; migrated owners never import it.

No generic generation, operation election, retained result, rejoin channel, concurrent-Stop coordinator, detached
cleanup, or bespoke shutdown state machine is allowed. A leaf does not independently solve shutdown ordering that its
parent composition boundary owns.

Gate A completes only when each assigned owner has focused behavior tests and race proof, the census decreases by the
reviewed owner count, and no prohibited helper or semantics were added elsewhere.

### 4. Implementation gate B: native protocol and manager owners

Migrate Q, P, and M owners against their exact protocols:

- await `startDone` before choosing running Stop or retained failed-Start cleanup;
- stop admission through the native NATS, HTTP, WebSocket, worker-pool, exporter, or subscriber handle;
- keep callback authority live while the native protocol drains or closes;
- cancel the run context only at the protocol's correct boundary, then join owner goroutines;
- use a caller-provided Stop context for waits; a bounded cleanup context is permitted only for the documented
  terminal-finalization exception;
- do not discover lifecycle authority by name, catalog children, replace same-name consumers, or mix backlog
  observation into lifecycle control.

Gate B completes only with protocol-specific tests, focused race proof, failed-Start proof where applicable, and an
updated census demonstrating that every assigned owner left the old framework.

### 5. Implementation gate C: deletion and end-to-end proof

- Remove `Generation`, `Operation`, `StopWithQuiesce`, their constructors/methods, and old helper files.
- Remove or rewrite every test that requires concurrent Stop result sharing, canceled or expired Stop rejoin, retained
  error replay, executor election, or StopWithQuiesce behavior.
- Retain the useful contract that a completed repeated Stop is nil/no-op, plus honest timeout behavior and
  failed-Start cleanup authority.
- Audit every production ACK site for effect-before-ACK and declared idempotency, durable progress/outbox, or explicit
  external at-most-once limits.
- Prove controlled shutdown and fresh boot with a real process.
- Prove dirty recovery by killing the process between effect and ACK and observing redelivery and convergence; also
  cover the required NATS interruption/restart path.
- Run focused race tests, repository race tests, integration tests, relevant E2E tiers, schema validation, lint, build,
  and CI against the same candidate commit.

There is no predeclared PR count. Keep reviews bounded by coherent ownership surfaces, but do not equate PR count,
merged documentation, or intermediate green CI with completion.

## Test-surface exit gate

Before runtime migration can be called complete, repository-wide production and test searches must show no dependency
on the rejected semantics. In particular, there must be no test whose required outcome is:

- concurrent Stop callers electing one executor or sharing a retained result;
- a Stop that returned on cancellation/deadline later rejoining the same terminal operation;
- replay of a previously retained Stop error;
- `Generation`, `Operation`, or `StopWithQuiesce` behavior;
- automatic same-name consumer replacement, lifecycle catalogs, or delete-on-Stop knobs.

Positive tests must instead prove owner-visible behavior: completed repeated Stop is nil/no-op, Start/Stop ordering is
race-free, accepted work follows the declared native drain semantics, timeouts return honestly, failed-Start authority
is retained, and all owned goroutines join. Arbitrary sleeps are prohibited; use explicit synchronization.

## Archive and tag gate

The change may not be archived, and no dependent tag may be cut, until one clean candidate commit satisfies all of the
following. “Expected,” “covered by another spec,” and “green before the final rebase” are not exemptions.

### Mechanical production zeros

| Search surface | Required count |
|---|---:|
| Production files importing `internal/lifecyclejoin` | 0 |
| `lifecyclejoin.NewGeneration` | 0 |
| Calls on `Generation.Stop` | 0 |
| External `Generation.Cancel` | 0 |
| External `Generation.Signal` | 0 |
| `Generation.StopWithQuiesce` | 0 |
| `lifecyclejoin.NewOperation` | 0 |
| Calls on lifecycle `Operation.Run` | 0 |
| External `RunPartialStartRollback` using the old symbol | 0 |
| Production declarations of `Generation`, `Operation`, or `StopWithQuiesce` | 0 |
| `Client.StopConsumer` declarations and calls | 0 |
| `Client.StopAndDeleteConsumer` declarations and calls | 0 |
| `Client.StopAllConsumers` declarations and calls | 0 |
| SemStreams `ManagedConsumer` wrapper/type declarations and uses | 0 |
| `DrainAndDelete` declarations and calls | 0 |
| Client lifecycle child catalogs, bindings, and `stopAllConsumers` | 0 |
| `DeleteConsumerOnStop` configuration fields | 0 |
| Automatic same-name consumer pre-stop or replacement paths | 0 |
| `ConsumeDurable` declarations and calls | 0 |
| Lifecycle-bound `OutstandingWork` or name-routed backlog calls | 0 |

Removal credit for `ConsumeDurable` belongs only to N1 and requires owner-approved `NewDurableHandler`, equivalent
heartbeat/AckWait and settlement proof, and a SemStreams migration map for ten sibling production calls. A lower local
call count without replacement earns zero credit.

The only justified final helper is `internal/lifecyclecleanup.RollbackFailedStart`. There is no shared Wait helper. N1
deletes the unchanged legacy rollback implementation and all lifecyclejoin declarations/imports after every owner
migration.

The five historical `DeleteConsumerOnStop` fields must all be absent; one renamed or generated-schema copy still
fails the zero gate. Exact-name searches are the first check for the client rows, followed by type-aware inspection for
renamed receivers, aliases, wrappers, catalogs, bindings, and equivalent same-name replacement or lifecycle-routed
backlog behavior. Read-only backlog observation may remain only when it is independent of lifecycle handles and bound
to a documented product contract.

### Mechanical test zeros

- Zero imports or direct uses of the removed lifecycle API in tests.
- Zero shared-suite cases enforcing executor election, concurrent result sharing, canceled/deadline rejoin, or retained
  result replay.
- Zero delete-on-Stop knob, managed-consumer, name-routed Stop/delete, or same-name replacement contract tests.

### Positive evidence

- Every production owner from the rebased census is mapped to a reviewed replacement and no owner is unaccounted for.
- Controlled shutdown, fresh boot, process exit, failed-Start cleanup, duplicate identity rejection, and owned-goroutine
  join evidence is green at the candidate commit.
- Every production ACK site has a recorded effect-before-ACK and recovery posture; dirty process and NATS interruption
  tests demonstrate redelivery/convergence for crash-critical paths.
- Focused race, repository race, integration, relevant E2E, schema, lint, build, and required CI checks are green at the
  candidate commit.
- Downstream impact is documented only in SemStreams migration notes; all sister repositories remain read-only.
- Every task in this change is truthfully complete. Strict OpenSpec validation passes after, not instead of, those
  runtime and proof gates.

Only after all three sections pass may this change be archived and the next tag be declared ready.
