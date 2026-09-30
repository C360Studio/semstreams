# #1222 CLI implementation review

Mode: independent implementation review, read-only. Verdict: **CHANGES REQUESTED — C1 and C2 (HIGH)**.
This is a bounded six-file review, not issue completion or merge approval. No tests, Docker, integration,
source edits, Git mutations, or GitHub writes were performed. Only this requested review artifact was written.

## Reviewed identity and authority

Worktree: `/Users/coby/.codex/worktrees/e2e-required-proof/semstreams`.
Accepted implementation base: `fe6e2cc03e16f5db47e293f55939548572f204cc`.
Accepted design SHA-256: `a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c` (verified).
Read reviewer contract, project purpose/boundary, active proposal/design/acceptance/tasks/spec delta,
implementation handoff, adopter seams, runner developer/pause records, Result/Writer round-one review,
relevant release-candidate-proof requirements and testing discipline. Reviewed the complete tracked CLI diff
against the accepted base and all four new CLI files, plus bounded caller/callee ranges.

All six frozen hashes matched runner-developer.md before inspection and again afterward:

| File | SHA-256 |
| --- | --- |
| cmd/e2e/main.go | 8181c5950fe1295bf56d4726391254733cbb106d6ff383244871d648bd2c0873 |
| cmd/e2e/main_test.go | ba9a256823d901f7fe8909f1902b651eec9e3bce72bd8f6b03c7a730e5bb9a29 |
| cmd/e2e/runner.go | eee05b0c830a04d7bd1391c89058516da89d2a3ce1076649e8c97f1228f86243 |
| cmd/e2e/runner_test.go | 85a03436b5d0cf8614624bb8779c3d1a13bc998fef3091d0ae2b100e25166408 |
| cmd/e2e/selection.go | 2615ed372246af83ae6b3e7d0cf072ea511601edcd38f01cdc1d450728515de4 |
| cmd/e2e/selection_test.go | 35e94d6fdc4f16decec6486dabe33e9bf1b87889688166e02725643112c4bdcb |

Result/Writer dependencies are concurrently being corrected for R1/R2. They are not an immutable reviewed
dependency snapshot. Partial core scenario changes are outside this verdict and were not treated as verified.

## Findings

### C1 — HIGH cmd/e2e/runner.go:145 — Existing preidentity commands fail the new name admission check

Mechanism: selection.go:83–85 leaves unclassified member IDs equal to the CLI spelling. Existing dispatch
main.go:388–393 maps `core-pre-identity-seed` and `core-pre-identity-assert` to NewPreIdentityBucketScenario,
whose Name is `core-pre-identity-bucket-` plus its mode (platform_identity.go:149). The new equality check
therefore returns one before Setup. Existing taskfiles/e2e/core.yml:222 and :252 invoke those exact spellings.
This breaks both halves of the core refusal proof independently of the intentionally pending required adoption.
The existing `minted-authority` alias has the same problem: main.go:382 accepts it, while Name at
platform_identity.go:37 is `core-minted-authority` and the resolver leaves the alias unchanged.

Fix: resolve these existing spellings to their actual canonical member identities before the admission check,
preserving selection/round distinctions and the identity guard. Do not weaken the guard or rename the held scenario.

Verification/refutation: traced both real Task callers, constructors and Name implementations. Other apparent
mismatches were refuted: health/dataflow/graph-roundtrip/slow-consumer have explicit canonical mappings;
research-graph-execute sets its distinct Name in research-graph/scenario.go:178–181; tiered Name is `tiered`
and its variant suffix is intentionally removed at runner.go:144. Existing resolver tests cover none of the
three failing spellings. A constructor-to-selection test across the admitted CLI names should catch the defect
without Setup or Docker. No executed reproduction is claimed.

### C2 — HIGH cmd/e2e/runner.go:207 — Legacy failures save stale successful typed analysis

Mechanism: executeScenario only invokes FinalizeChecks (and its projection synchronization) for an adopted
scenario, runner.go:74–85. The legacy path at :87–95 returns a failed Result without synchronizing Structured.
The selected runner then calls saveScenarioAnalysis at :207, before WriteRun at :225. The helper saves
Structured even on failure (main.go:513–525). A real reachable producer is semantic-fallback: selection.go:44–49
leaves it legacy, tiered.go:567–570 builds Structured while Success=true, then validateFallbackBehavior can
set Result.Success=false at :578–581. validate_search.go:443–472 has actual error returns, including an
unexpected available semembed. The emitted typed file can therefore say success while Result and outer exit
say failure. A teardown error after a legacy success has the same shape.

Fix: emit typed analysis only after synchronizing it from the retained final Result for both adopted and legacy
execution, using the existing projection owner. Keep legacy evidence unattested; do not force it through required
finalization just to repair metadata. Ensure the standalone file and aggregate agree after all lifecycle errors.

Verification/refutation: Writer's later metadata repair cannot repair bytes already saved by SaveStructuredResults.
This differs from R1's shallow-copy aliasing inside Writer and remains a CLI ordering problem after R1 is fixed.
runner_test.go:145–151 only checks an adopted missing-observation fixture, which is synchronized by FinalizeChecks.
main_test.go's legacy teardown case has no Structured object. Require a legacy final-validation/teardown failure
with previously successful Structured, then inspect both typed file and aggregate. Aggregate/exit do remain red;
this is contradictory persisted analysis, not a claim that the aggregate passes. No execution was performed.

### C3 — MEDIUM cmd/e2e/main.go:416 — Resolved custom endpoints do not reach every tier consumer

Mechanism: resolution copies baseURL and tests assert that copy, but createScenario still sets GraphQLURL to
localhost:38180 for semantic and localhost:38080 for other variants (:416/:428). ServiceManagerURL and GatewayURL
use the caller's baseURL (:403–404). Thus `--scenario semantic --base-url http://remote:1234` combines remote
observations with local graph roundtrip/search, whose consumers are tiered.go:437 and validate_search.go:26–28.
The accepted Selection determines proof scope clause and developer handoff promise endpoint preservation.

Fix: carry the existing configured service base through the GraphQL consumer consistently, while preserving
the intended default semantic endpoint behavior. Do not add an unrelated endpoint API without reconciliation.

Verification/refutation: this hardcoded constructor behavior predates the CLI diff; it is an unresolved endpoint
acceptance gap, not a newly introduced hardcoded value. The alias now reaches that constructor. Existing Task
commands use matching default ports, so those callers mask it. --graphql-url is explicitly throughput-only
(main.go:164–165), so it is not a current override for tiered. Test constructor/consumer routing, not just the
copied flags. Real adopted tier execution remains pending and currently refuses; this finding does not assert
a completed remote run or extend scope to a new endpoint flag.

### C4 — MEDIUM cmd/e2e/main.go:322 — Legacy pre-execution output omits its evidence disposition

Mechanism: before work the command logs `required=false`, then ordinary Running/Setting up/Executing messages.
The first explicit `unattested` text is after successful execution at runner.go:93, or the aggregate completion
at :236. The active Selection determines proof scope clause requires pre-execution output to identify legacy
evidence as unattested, including executions that fail or are interrupted before completion.

Fix: add explicit resolved evidence disposition to the existing selection announcement before Setup. Keep the
completion distinction already present.

Verification/refutation: required=false identifies selection intent but does not communicate the accepted
unattested limitation; there is no earlier legacy-specific banner. The initial JSON records that disposition
when an output directory is supplied, but legacy commands such as lessons can execute with no directory.
Observe the announcement before a fixture Setup that fails; no test currently asserts it.

## Conformance and refuted concerns

| Obligation | Source evidence / assessment |
| --- | --- |
| Default/all means exactly two cores | selection.go:24–31; main.go:586–591 creates health and dataflow only. No graph/shell proof is inferred. |
| Semantic/rules resolve explicit variants | selection.go:32–57; conflicting and unknown variants reject. Explicit known semantic-fallback stays legacy. |
| Unknown scenario refuses | Resolver leaves unknown names to createScenario; main.go:334–337 refuses before Setup. This is not unknown-name success. |
| Declarations precede Setup | runner.go:149–159 snapshots/validates every selected declaration before the loop; :39–48 declares before Setup. |
| Returned identity/membership cannot redefine expectation | runner.go:75–79 compares run/member and exact ID/classification set, adds retained failure, then finalizes. |
| Nil Result does not panic or pass | runner.go:55–64 retains the initialized Result and adds execute failure; main_test.go:72 covers helper exit/log. |
| Setup/execute/teardown errors persist | runner.go:49–71 appends phase errors; :214 retains each Result; :225 attempts terminal persistence. |
| Required storage refusal | Missing output directory refuses at :135; initial write failure refuses before Setup at :194; terminal failure returns one at :225–228. |
| Writer-finalized exit propagates | runner.go:230 reads finalized run.ExitCode; main.go:87–88 passes it to os.Exit. |
| Failed partial observations survive | Existing Execute result is retained at :60, phase failures append, and aggregate includes it regardless of member exit. |
| Legacy proof remains unattested | runner.go:87 and :236; required intent is copied into run.Config.RequireEvidence at :164. C4 covers missing initial announcement. |
| Caller output and core endpoints survive resolution | Flags copied, output normalized once at :187–192, all uses caller UDP/WS/base at main.go:572–588; C3 qualifies tier GraphQL. |

This change's shape is admission of declared set membership plus terminal persistence. It extends the existing
Result finalizer and TestRun Writer, matching the accepted inventory's ComponentResult.Required/Missing and
IndexSpec.Required precedents; it does not add another storage or execution owner. New CLI helpers/interfaces
are private and have present dispatch/test consumers. `sameCheckRequirements` compares the declaration snapshot
with the returned declaration; it does not duplicate observation acceptance. No production stored context,
payload registration, NATS primitive or graph state owner is introduced in the CLI slice.

## Evidence and limits

Retained `/private/tmp/semstreams-1222-runner-green.log` was read; SHA-256:
`64f5a70309e820257f9eec41c1b4088bb68144c90dc56d54e14c81727f76f442`.
It contains `ok github.com/c360studio/semstreams/cmd/e2e 2.353s`. Author identifies the command as
`go test ./cmd/e2e -count=1`; the log alone does not show argv or frozen dependency hashes. Treat it as retained
author package GREEN at the earlier checkpoint, not reviewer execution or proof against current concurrent edits.
Historical RED claims have no separately retained raw outputs and remain UNVERIFIED. API-absence RED is not
compiled behavioral mutation evidence.

The deterministic cases are reasonable for finite CLI aliases and lifecycle boundaries; a second generated
required-set oracle is unnecessary because that responsibility belongs to Result/Writer. However the present
examples omit the concrete aliases and legacy projection path above, terminal write failure after successful
initialization, setup-failure retained JSON, returned wrong declaration/run identity, and required empty/unadopted
selection side-effect refusal. The fixture has a setupErr field but no selected-run case uses it. The current
synthetic provenance fixture omits R2's retained reference fields and must be reconciled with the corrected Writer
before its prior complete-proof expectation is reused.

Mutation criteria apply to omission acceptance, outer exit propagation and failed serialization. Deferral is
accepted only for this read-only intermediate checkpoint while dependent fixes/adoption are in flight; final
verification still requires compiled/reached baseline-mutant-restored evidence with cp backups and hashes.
No -race, exploratory fuzz, mutation, real Task, Docker, paid model, assembled E2E, hosted retention or push gates
were run here. Core/tier/agentic adoption and Task/report API remain separate pending work; a synthetic CLI fixture
cannot satisfy them. The paused wording in tasks.md describes the prior checkpoint and needs root reconciliation
after the owner's resume, not a reviewer edit or a claim that implementation tasks are complete.

Structural tool limitation: attempted gopls references/implementation; package loading failed on denied
`/Users/coby/Library/Caches/go-build` access. Initial line-offset attempts were corrected, but no reliable complete
structural enumeration resulted. No escalation or cache mutation was requested. Bounded fallback used `git grep`
for tracked dispatch/SaveStructuredResults callers and `rg` for untracked runner helpers, all Scenario Name and
constructor declarations, selected Task commands, GraphQL consumers and fallback validation. This is explicitly
source inspection, not compiler-backed completeness.

**CHANGES REQUESTED — C1 and C2.** C3/C4 remain concrete corrections/acceptance gaps. Freeze may be released by
the root for coordinated author fixes; re-review exact changed bytes and regression evidence afterward.
Result/Writer R1/R2 need a separate stable snapshot and separate verdict. Nothing here approves the whole issue.
