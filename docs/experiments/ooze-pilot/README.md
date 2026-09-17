# Ooze utility pilot

Issue [#1318](https://github.com/C360Studio/semstreams/issues/1318) ·
PR [#1319](https://github.com/C360Studio/semstreams/pull/1319)

## Recommendation

Defer routine adoption of this pinned Ooze revision. Require targeted sensitivity evidence when the
[testing policy's mutation criteria](../../contributing/01-testing.md#when-targeted-mutation-evidence-is-required)
apply, using existing tools. This evaluation does not justify a general mutation-testing integration.
Ooze is useful as a bounded candidate generator: it produced ten relevant entity-ID mutations, all
independently detected and replayed. Its raw score does not supply the evidence our testing policy needs.
Compilation errors, unrelated command failures, and test timeouts were credited as kills. Cancellation
and source-write isolation also required protection outside Ooze.

This evaluation adds a reproducible experiment, not a supported runner, root dependency, or CI gate.
A later adoption proposal should account for classification, baseline selection, cancellation, and source
protection before promising less work. Do not turn this one-off recorder into a maintained platform by default.
No production defect or missing check was discovered in the selected comparison slice.

The owner accepted a policy follow-up within this PR: record which mutation criteria apply to an issue,
require controlled evidence in the PR when triggered, and have the reviewer independently check applicability
and results. The canonical policy and developer/reviewer prompts carry that instruction. This is review-time
evidence at a recorded revision; it does not continuously check later test changes. A persistent automated
check can be justified separately for a demonstrated gap. No tool-adoption implementation issue is opened.

## Scope and provenance

- Subject: SemStreams `84fc01e46c104d890bffe32b0046e72f006df454`; `pkg/types` tests with the existing
  `pkg/errs` and `pkg/retry` dependency closure. Inputs are real copies from Git, including testdata and
  original root dependency manifests. Production files and existing checks were not edited in the worktree.
- Tool: Ooze `v0.2.1-0.20260819134008-87c15dcb1804`, commit
  `87c15dcb180492ba96f30278efc0146dd248f09b`; Rapid 1.3.0; Go 1.26.4 on darwin/arm64.
- Scope: comparison mutations in `pkg/types/entity_id.go`. Other operators and platforms were not evaluated.
- Execution: serial; `GOMAXPROCS=2`, `GOFLAGS=-p=2`, `GOTOOLCHAIN=local`, `-parallel=1`, `-count=1`,
  Rapid seed 1318 and `-rapid.nofailfile`. Exact selectors and commands are retained per experiment.
- Experiment isolation: dedicated claim/worktree, with experiment files confined to this directory. The
  subsequent owner-approved policy follow-up also updates the testing policy and developer/reviewer contracts.
  No Docker/NATS, shared service, root dependency/CI edits, sister-repository writes, or other claims are changed.

The [inventory](inventory.md), [reviewed plan](plan.md), [source observations](upstream-notes.md),
[upstream hashes](upstream-source.json), and [review record](reviews.md) separate prior knowledge from results.

## Calibration findings

“Killed” and “survived” below are Ooze's labels. The interpretation column applies the existing
[testing discipline](../../contributing/01-testing.md#establish-sensitivity-to-a-selected-mutation).
The score threshold was zero for observation; a zero harness exit is not a correctness verdict.

| Control | Ooze observation | Independent interpretation |
|---|---|---|
| 256-byte `>` → `>=` | Killed | Detected: the boundary property reports a rejected 256-byte canonical ID. |
| Same mutation, round-trip property only | Survived | Survived: its generated IDs do not reach that boundary. |
| Boundary coverage restored | Killed | Detected after a new passing baseline with the restored selection. |
| Synthetic mutant returning an integer as bool | Killed | Invalid: compile failure, no behavioral assertion executed. |
| Non-test command exits 23 | Killed | Inconclusive: a command failure does not establish sensitivity. |
| Missing executable | Panic / unsuccessful harness | Inconclusive supervision failure; not credited as a kill. |
| No selected tests | Survived | Inconclusive: zero tests executed; output warns about the empty selection. |
| No eligible mutations | Total 0, unsuccessful harness | Zero evaluation; not evidence of sensitivity. |
| Go test exceeds 100 ms | Killed | Inconclusive: baseline also times out; no relevant assertion. |
| Integer `x < x` → `x > x` | Survived | Equivalent over all Go `int` values: both strict self-comparisons are false. |
| Test command exits with a live descendant | Survived | Normal Ooze supervision removed the descendant. This is a control, not sensitivity evidence. |
| SIGINT to Ooze test process | Interrupted | Two live descendants and one temporary directory remained before harness cleanup. |
| SIGINT to Python driver | Exception after cleanup | The recorder removed all three owned members across two groups and the temporary directory. |
| Independent outer deadline | Interrupted | Same cleanup requirement; bounded termination came from the harness. |
| Unmutated sentinel write | Input copy changed | Unix symlinks permit write-through; only the disposable sentinel changed. |

The zero-mutation fixture has a failing baseline; it establishes only zero candidate evaluation and
the score/exit behavior, not a clean-baseline empty-selection qualification. No candidate command ran.

The equivalent assessment is an argument about the integer domain, separate from the observed survival.
The mutated target itself was a regular file; the unmutated sentinel remained a symlink. Never point this
experiment at a working checkout and assume Ooze's temporary directory protects every input file.
All process/temp cleanup claims distinguish the state before and after the pilot's own intervention.

## Discovery and replay

The ten generated candidates were all evaluated; none survived or remained unevaluated. Each changed
only the implementation and reached the selected tests. The independent replay retained each exact
mutant, ran the same command on original/mutant/restored bytes, and obtained exit codes 0/1/0 with
checksum restoration. Discovery baselines actually executed 77 Go test events; that count includes
subtests and is not 77 independent obligations.

| Candidates | Change | Observed violation |
|---|---|---|
| 01 | Serialized size `>` → `>=` | Rejects a valid 256-byte ID. |
| 02–03 | Minimum/maximum part-count inequality | Rejects valid six-part IDs. |
| 04 | Byte loop `<` → `<=` | Index panic on valid input; detected as an acceptance-contract violation. |
| 05–10 | Tighten ASCII endpoints `a/z/A/Z/0/9` | Rejects valid alphanumeric bytes. |

Candidates 06, 08, and 10 were detected only by the Rapid properties in this selected suite.
Exact generated inputs were promoted into disposable deterministic witnesses for 04, 06, 08, and 10;
each witness received a fresh original/mutant/restored 0/1/0 comparison. The curated synthetic
256-byte Rapid witness was also replayed explicitly on both implementations. Those witnesses show
sensitivity to intentional faults, not bugs that were present in the original implementation.

This is a narrow result. The byte-bound property shares its constant with production; independent
255/256/257 table checks remain distinct protection. The selected operator cannot express every
plausible failure, including omitted sentinel filtering, choosing the wrong state owner, or omitted cleanup.
A 100% score on these ten comparisons says nothing about those obligations.

## Cost and limits

The accepted run generated and tested ten candidates in **10.903 seconds**. Direct execution of the
same ten saved mutants took **11.401 seconds**; adding the twenty original/restored checks brought
manual replay to **30.366 seconds**. The warm baseline took **1.772 seconds**.
Full timing and archive identities are recorded in [results.json](results.json).
The comparison separates mutant execution from extra paired baselines/restoration; counting all
manual replay checks against only Ooze's mutant runs would exaggerate a speed difference.
Dependency/build caches were warm, so the measurements are not a cold-install benchmark.
Human setup and triage were agent-assisted and not timed independently; developer-time savings remain unproven.

Qualification required an external recorder, source copies, command baselines, process-group ownership,
failure interpretation, and manual replay. Review found and corrected gaps in the recorder's own cleanup.
That work is part of the evaluation cost, not a capability supplied by Ooze. The committed scripts are
reproduction artifacts for this pilot, not a supported general-purpose mutation service.

The optional shutdown slice is deferred. The first slice already establishes the qualification limits,
and the selected comparison operator does not model the sentinel-filter omission. Building a custom
operator and copying the broader service dependency closure would add cost without resolving those limits.
The separately tracked manager-teardown property gap remains issue #1219.

## Reproduce and inspect

Run on a quiet macOS host with Python 3, Go 1.26.4, Git, and the pinned source commit available.
The first run may need network access to download pinned modules. The script reads source from Git,
not current uncommitted files; its new output directory must not already exist.

```bash
python3 docs/experiments/ooze-pilot/run.py /tmp/ooze-pilot-new-results
```

The script retains its own runtime copies for inspection and reports their unique path. Cleanup is
limited to owned process groups and Ooze temporary directories. It never reads `.env` or uses Docker.
The source worktree, copied source, and recorder identity are checked separately from tool output.

The lossless [evidence archive](evidence.tar.gz) contains all raw logs, commands, manifests, patches,
mutated source, exact witnesses, and recorder snapshots. `evidence-accepted/` is the final measurement;
earlier directories are exploratory runs superseded during recorder review. See [results.json](results.json)
for the archive hash and accepted identities. Inspect without executing archived code:

```bash
mkdir /tmp/ooze-pilot-evidence
tar -xzf docs/experiments/ooze-pilot/evidence.tar.gz -C /tmp/ooze-pilot-evidence
```

The accepted run's `commands.json`, per-control `result.json`, `manual-replay.json`, witness logs,
and integrity records are the detailed evidence behind this report. Absolute temporary paths identify
original runs; reproduction creates fresh paths. Repository preflight is separate from experimental commands.
