# Ooze utility pilot: execution plan

Issue #1318 / draft PR #1319. This plan authorizes no tool adoption or production change.

## Evidence and review identity

The accepted inventory is incorporated unchanged by reference:
`docs/experiments/ooze-pilot/inventory.md`,
SHA-256 `b9ecc720ef153d292931137bf15c5a031c834462b7e14b2d4231eda6468d1f5c`.
The caller records its independent INVENTORY PASS and the mechanical 60/60 pin check.
SemStreams source baseline: `84fc01e46c104d890bffe32b0046e72f006df454`.
The caller records this plan's hash and independent execution-plan review before experiments.

Upstream provenance is retained in `upstream-source.json`: Ooze commit
`87c15dcb180492ba96f30278efc0146dd248f09b`,
module version `v0.2.1-0.20260819134008-87c15dcb1804`.
The measured host toolchain is Go 1.26.4, darwin/arm64; use `GOTOOLCHAIN=local`.
Current source declares Go 1.26.3 and Rapid 1.3.0.

## Alternatives and selected experiment shape

1. Keep manual mutation checks only: no new tooling cost, but no measurement of Ooze's practical value.
2. Use one isolated pilot module and small harness: measurable automation with bounded setup and interpretation cost.
3. Integrate a runner into root dependencies or CI: broader maintenance and policy effects, outside this claim.

Use option 2 for qualification. This is an experiment choice, not an adoption recommendation.
The existing controlled-comparison discipline supplies the method; no new reporting framework is needed.
No communication, orchestration, payload, query, durable-state, or framework API design is introduced.

## Isolation and source identity

Commit only the pilot module, harness, documentation, and evidence beneath `docs/experiments/ooze-pilot/`.
Keep the root module, production files, existing tests, CI, and other worktrees unchanged.
Build disposable inputs from the pinned Git source, using real copies rather than source-worktree symlinks.
Preserve original package paths, source bytes, testdata, and dependency manifests in those inputs.
The measured internal closure for pkg/types tests is pkg/types → pkg/errs → pkg/retry.
Record selected files and checksums, dependency versions, environment, exact commands, and test selectors.
Verify the copied files against the pinned source before testing and source hashes again after each experiment.
Use `-count=1`, fixed Rapid seeds where used, and `-rapid.nofailfile`; retain curated witness files.
Do not read `.env` or invoke root Task suites for this pilot.
Run one experiment at a time with Go build/package parallelism bounded for this invocation only.
No Docker, NATS service, paid provider, integration/E2E suite, or global cleanup is part of execution.
Recheck live claims and host contention before work; pause owned processes if they interfere with another claim.
Every child command has a timeout; each experiment has a separate process-tree deadline and owned cleanup.

## Calibration before discovery

Use tiny synthetic fixtures for runner failure controls; label them separately from SemStreams evidence.
Each row records raw Ooze output, process status, and independently interpreted evidence.

| Control | Required observation |
|---|---|
| Positive detection | The entity-ID `>` to `>=` fault rejects exactly 256 bytes; the intended assertion fails. |
| Negative sensitivity | The same fault survives checks excluding boundary coverage; restoring coverage detects it. |
| Invalid build | A recorded implementation mutation fails compilation; it is invalid, never assertion detection. |
| Broken command | Missing executable and nonzero command failure are distinguished from a test assertion. |
| Zero tests | A selector matching no tests is visible as zero execution, regardless of command exit or score. |
| Zero mutations | A source/operator selection with no eligible mutations is visible as zero evaluation. |
| Test timeout | A bounded Go test timeout is distinguished from the intended behavioral assertion. |
| Cancellation | Cancel an owned blocking fixture; verify bounded parent/descendant exit and temporary-file cleanup. |
| Equivalent candidate | A valid known-equivalent fixture survives; equivalence is a separate reviewed argument. |
| Source preservation | Check mutated-file isolation and an unmutated sentinel's behavior through Ooze's symlinks. |

For equivalence, a small integer fixture such as `x < x` versus `x > x` permits an explicit domain argument.
Record whether each candidate came from Ooze's operators or a manually seeded fixture.
For preservation, deliberately write an unmutated sentinel only inside a disposable source copy.
Measure whether that write reaches Ooze's input copy; verify the pinned source/worktree remains unchanged.
Record temporary paths and owned PIDs so normal completion, failure, and cancellation can be checked.
Check ordinary tool cancellation separately from outer-deadline enforcement; do not attribute the latter to Ooze.
An unsafe result ends that run immediately; clean only resources demonstrably owned by this experiment.
Classification failures may still permit a bounded discovery pass with independent inspection.
Do not widen the pilot into repairing Ooze; record the limitation and its interpretation cost.

## Entity-ID discovery and controlled replay

First run the unchanged pkg/types checks and record the tests actually executed and their passing baseline.
Limit automated discovery to comparison mutations in `pkg/types/entity_id.go`; exclude dependencies and tests.
Keep the initial tests, generators, seeds, fixtures, and runner configuration fixed.
Record eligible/generated/evaluated counts, skipped candidates, survivors, errors, and elapsed time.
Bound the discovery run to 15 minutes; any unevaluated candidates remain explicitly unevaluated.
Use a shorter per-mutant timeout justified by the measured baseline; record its value before discovery.
Retain exact changed source or patches so each reported candidate is independently reproducible.
For meaningful detections and survivors, replay the exact mutation manually against the same selected checks.
For generated failures, replay the identical witness/input on mutant and original, not merely two random runs.
Use baseline → mutant → byte restoration/checksum → restored pass; never restore with Git commands.
Record compiled assertion detection separately from invalid or inconclusive execution.
Assess survivors individually as missing input/assertion, equivalent, outside scope, deferred, or unresolved.
The shared MaxEntityIDBytes constant makes the explicit 255/256/257 table distinct evidence from the property.
A strengthened check lives only in the disposable experiment; establish a new baseline before comparing it.
Keep any discovered production/test correction as a finding for its proper scope, not an incidental pilot edit.

## Costs, optional second slice, and completion

Record cold setup time, warm baseline time, mutation count, total execution, replay time, and interpretation effort.
Compare selected identical faults under Ooze and manual checks; separate setup cost from recurring work.
Measure active human effort separately from machine elapsed time; label estimates instead of inventing precision.
List important fault classes the selected comparison operators cannot express.
If entity-ID evidence is trustworthy and useful, evaluate only the existing shutdown sentinel-filter slice.
Use its named deterministic retry test; do not rely on the truncated Rapid stream as paired replay.
Record the service copy's dependency closure and baseline before execution; do not modify shared runtime owners.
Defer that slice if setup, resource use, interpretation cost, or calibration findings outweigh its added evidence.
The manager-owned teardown reachability gap remains #1219 and is outside the pilot.
Retain commands, manifests, patches, logs, outcomes, and limits in the pilot/PR evidence record.
Finish with independent review and a measured recommendation: explicit tool, small reporting addition, or defer.
No score threshold decides utility; no new CI gate, root dependency, runtime contract, or downstream adoption follows.
