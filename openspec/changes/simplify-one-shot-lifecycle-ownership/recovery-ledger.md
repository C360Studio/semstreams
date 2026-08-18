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

## Workspace recovery checkpoint

Authority collisions are inventoried in
[`authority-reconciliation-inventory.md`](authority-reconciliation-inventory.md) (SHA-256
`d495b4acb908bb846194eec0ce9c97076691bf3cba5d3143b8c2453335792f4e`); inventory only, with no completion credit.
The inventory-first reconciliation design is recorded in
[`authority-reconciliation-design.md`](authority-reconciliation-design.md) (SHA-256
`9af0ceeacae2dc0a854b0337f5eb6dd19710a90fde586944e7f9bca0b06df039`); design only, with no runtime or proof credit.
Applying the approved authority reconciliation changes no runtime-migration or proof completion credit.

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
- keep the single stateless bounded rollback helper only while it has measured callers.

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

The obsolete lifecycle helper files and package must be deleted. If a bounded failed-Start helper remains justified, it
must be stateless, narrowly named and located, independently reviewed, and absent from every old-symbol search above.

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
