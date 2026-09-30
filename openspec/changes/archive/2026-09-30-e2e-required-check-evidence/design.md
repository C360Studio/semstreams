# Selected required-check evidence design

base: 12ae633381b8b8b26c333efe5f5c8691cfa47fb4
status: Proposed design; independent design review and owner acceptance pending.

## Reviewed authority and unchanged inventory

This design follows INVENTORY PASS, not an implementation or owner approval. The accepted inventory is incorporated
verbatim by its frozen package, not rewritten here:

- inventory manifest: 584fa0db8b0d3fb44940578ec8dccf53c5b0642816827241e7a4bcf0152ec34a
- focused inventory: 15b9b62de5f840aae1418da323782f4ff907564e21ecac80636b6d13d46feb44
- independent review: 03f19cf75207715b502161a824cc7af7eddc34672ca7611f66802fadb0789d3f
- files: inventory.md, adopter-seams.md, evidence/gate-inventory.md, evidence/structural-inventory.md,
  evidence/adjacent-claims.json, and evidence/inventory-review.md.

The root subsequently checked the three inventory tasks and added a HOLD marker. Those documentary changes do not
refresh the inventory's baseline or hashes. Its task pins describe the reviewed baseline intentionally.

Scope authority is #1222, beta.165 / #1134 package C. This design is an internal testing-contract change, not a
production execution framework, new deployment state, new ADR, or permission to implement. Source and mutation
experiments remain unrun. No suite-cost estimate below is represented as a new measurement.

## Problem and measured premises

A successful callback, a nonzero assertion total, an available task and a saved file are different observations.
The shared runner currently accepts Result.Success, logs AssertionsRun, and only saves successful nonnil Structured
results. Named checks can warning-return nil; three special dispatch branches lose flags; task core owns assertions
outside Scenario. These mechanisms can produce a green process without the evidence a caller thinks it requested.

| Premise | Reviewed measurement | Design consequence |
|---|---|---|
| Result already owns outcome and observations | scenario.go31–53 | Extend Result; do not add a competing scenario result |
| TestRun already groups Results and writes JSON | results/writer.go19,105,184 | Wire and extend this existing run writer |
| Typed tier JSON has separate readers | results.go738/765; cmd/e2e/compare.go | Retain typed analysis as a projection, not authority |
| Counts use different units | agentic/scenario.go321; core_slow_consumer.go212 | A count cannot decide completeness |
| Required-set models already exist | stages/components.go76; stages/indexes.go16/45 | Reuse membership semantics; never claim absence |
| Extracted verifiers are unwired in tracked Go | inventory correction searches27–29 | Do not delete or force adoption of stale wrapper sets |
| Live validators have their own semantics | validate_infra.go20; validate_structural.go14 | Adapt real observation sites, not unused helpers |
| Special aliases bypass normal configuration | cmd/e2e/main.go314–322,650 | Resolve selection once before execution |
| Core task asserts beyond Scenario | core.yml102–253 | Record task-owned checks through actual observed assertions |
| CI runs statistical and slow-consumer | e2e-ladder.yml44/74 | Do not silently select more CI work |
| Release proof already owns candidate identity | release-candidate-proof/spec.md109–161 | Link run evidence; do not replace authorization |

Historical cost evidence: #1390's archive records task e2e:all at source05182c2e taking1381 seconds, with semantic
11m20s and agentic5m35.5s (archive tasks.md174–183). This was the five-family sequence, not thirteen-family coverage.
Its raw log was a session scratchpad and has not been verified here. It supports avoiding an unmeasured all-tier CI
mandate; it does not establish current cold/warm cost, per-check cost or runner capacity. Implementation records
actual selected command durations; #1117/#769 still own CI budget decisions.

## Options and recommendation

| Option | Benefit | Cost and remaining failure |
|---|---|---|
| Do nothing; narrow published claims | No code or execution cost | Known required-check warning paths and absent failed artifacts remain; does not meet #1222 |
| Patch three callbacks and require nonzero AssertionsRun | Small local diff | Stage totals still hide missing named checks, shell obligations and legacy zero semantics; insufficient |
| Extend Result/TestRun and existing Task report/exit seams | One outcome/evidence owner; keeps current execution and shell assertions | Requires assertion-site records, failed-artifact handling and explicit legacy disposition; task crash can leave incomplete evidence |
| Put Task beneath a cmd/e2e process envelope and move shell assertions to Go | Centralizes child status and observation | Inverts public tasks, adds a process coordinator and lifecycle migration; larger change than required; not recommended |
| New generic scenario engine, event journal or testing service | Could replace every runner idiom | New lifecycle/storage/coordination and migration burden; no measured need; outside scope |

Recommend the third, smaller existing-owner option, with a bounded first required set. Required membership is explicit
and narrow; unchanged
checks can continue running and failing as today without being advertised as newly proven obligations. The shared
contract is general enough for existing scenarios, but this change does not bless every old stage or force every
capability owner to migrate immediately. Incomplete adoption must be visible and must not satisfy a required suite.

## Acceptance model and proposed surface

The shared acceptance fact remains in test/e2e/scenarios.Result. Add named expected checks and observed outcomes to
that type, and derive final Success from proof for adopted records. Legacy records retain execution Success only
with explicit unattested disposition; no shared report interprets that boolean as required-proof success. Use one small
E2E-local value model:

- Check requirement: stable ID plus required or diagnostic classification.
- Check observation: ID, passed/failed/skipped status, reason and bounded evidence references/identity values.
- Coverage disposition on Result: complete or unattested. Missing expected observations are derived, not stored as
  a second independent count. Setup/execution/teardown failures are separate lifecycle failures, not fabricated
  behavioral passes.

The concrete exported types are CheckRequirement and CheckObservation alongside Result; their present consumers are
agentic, core and tiered scenario packages, cmd/e2e and the TestRun writer. Add a small recording method on Result
that checks declaration and duplicate identity, and accepts a completed observation; it does not execute callbacks.
No arbitrary step registry, plugin dispatch, retries, dependencies or generic test DSL is introduced. Finalization
accepts expected declarations before observations and rejects duplicate declarations, contradictory/duplicate
observations, unknown IDs, empty required membership and missing/non-passed required observations. A repeated check
uses a declared distinct round ID; overwriting failed evidence with a later pass is forbidden.

An observation is recorded at the existing behavioral assertion point after the input/expected identity and actual
value have been compared. Recording passed at the end of a stage merely because it returned nil is forbidden.
Required observation errors record failed with the error class/reason. Intentional skips record skipped with a
reason and cannot satisfy required membership. Diagnostics may fail or skip without changing required acceptance;
they remain separately visible. Existing execution errors continue to fail even if every required check passed.

Result.AssertionsRun remains a supplemental compatibility field during this change: define its new value as the
number of evaluated named behavioral observations (passed plus failed), excluding missing/skipped/diagnostic-only
records. It is derived at finalization, never used as the oracle. Reports label the named set and its scope first.
Update its comment and affected tests together; do not compare old stage totals20 with new behavioral totals.
No reliance on omitempty to distinguish zero from unknown remains: coverage disposition states that distinction.

The existing ComponentResult.Required/Missing pattern supplies set comparison, and existing condition-attempt
counting supplies assertion placement. IndexSpec.Required describes capability membership, not generic success:
translate its actual missing/error observations through the same Result finalizer where the live check is adopted.
Do not import the unwired ComponentVerifier wholesale: its component set omits live graph-index/graph-gateway.
The extracted helpers remain intact and explicitly outside assembled proof until their separate consumer is changed.
No second generic validator is added to stages. The one shared Result finalizer owns required-evidence acceptance;
capability helpers continue owning observation and capability-specific identity checks.

## Selection, required scope and default behavior

Resolve CLI names once to an execution selection, retaining caller flags. Remove the separate semantic/rules
execution interpretations: semantic resolves the same explicit semantic variant as --scenario tiered --variant
semantic; rules resolves structural. Reject an unknown variant rather than falling through to common stages.
Empty CLI selection and --scenario all retain the two core scenarios, but help calls this core-scenarios explicitly.
Task e2e:core remains the larger task including shell lifecycle/refusal and graph roundtrip. Do not conflate them.

Preserve the existing five-family composite under the descriptive task name e2e:core-inference-agentic. Keep
 e2e:all as a documented alias that prints its exact included/excluded membership, not an all-capability claim.
The task's current cost/order is preserved; no new family is run by aliasing. Required suite membership is:

| Selection | Required execution members | Initial required behavioral set |
|---|---|---|
| core-scenarios (CLI default/all) | core-health, core-dataflow | configured component health; run-bound pass-through output count, content and identity |
| task core | readiness/heartbeat, minted authority, core-scenarios, normal shutdown, early boot cancellation, preidentity refusal, graph roundtrip | existing exact identity/lifecycle assertions plus core-dataflow repairs |
| structural | tiered structural | required components; graph roundtrip identity; structural zero-ML obligations |
| statistical | tiered statistical | required components; graph roundtrip identity; controlled search fixture returned through actual query consumer |
| semantic | tiered semantic | required components; graph roundtrip identity; semembed available; controlled query identity, retaining all existing hard failures |
| agentic | agentic scenario | matching terminal task/loop; request-bound tool execution; selected streaming chunks observation; all existing hard failures remain fatal |
| core-inference-agentic / all | task core, structural, statistical, semantic, agentic | union of these declared sets, no skipped member permitted |

The initial catalog names only the following proof obligations; it does not automatically convert every successful
stage to required evidence: core-health.components; core-dataflow.pass-through-count/content/identity; core.readiness,
core.heartbeat, core.minted-authority, core.shutdown.exit/log/listeners, core.early-cancel.exit/no-services,
core.preidentity.refusal/no-record, core.graph-roundtrip.identity; each inference variant's components and
 graph-roundtrip.identity; structural.zero-embeddings/zero-clustering-runs; statistical.controlled-search.identity;
semantic.semembed and controlled-search.identity; agentic.terminal.task-loop, tool.request-result,
streaming.chunks. TTFT remains a named diagnostic, matching its existing non-fatal semantics. Slow-consumer's existing
individually evaluated conditions retain their distinct names rather
than collapsing into one callback check. Subchecks share a run identity and retain expected/observed values.

Checks outside this enumerated initial proof set retain their existing fatal execution behavior and visible
observations, but are labeled outside the admitted proof contract. A complete record means complete for this named
set, never that every capability or legacy check in a tier has been audited. Deleting an existing fatal assertion is
not authorized by moving to named evidence. Documentation must state this bounded guarantee prominently.

Structural zero-ML means absence of embedding execution and clustering runs/component activity as observed by the
existing structural checks. `structural.zero-clustering-runs` does not assert zero stored community records. No new
community-state deletion or stronger absence guarantee is implied.

For semantic, the design does not convert the B0 thematic or B2 partition quality recorders into hard gates or
calibrate new scores. Existing independent hard path checks remain; the implementation's check declaration must
name the actual deterministic checks, not the stage list. #1117 owns any later per-PR path/quality selection change.

Intentional composite exclusions are slow-consumer, lessons, research-graph (both rounds), deep-research, CRUD-tools,
ops, lifecycle, throughput, heavy semantic8b/frontier/fallback variants and live OpenAI adapter checks. They remain
individually selectable and are explicitly printed with exclusion reason: outside this composite, not proven by it.
Slow-consumer keeps its existing independent CI job and MUST adopt its existing per-condition assertion set in this
landing; it is not left legacy-unattested. This adds no suite member or new behavior threshold. Release-candidate proof
retains its separately required gates.

The bounded required set does not imply all legacy observations in these tiers have been audited. Every result
lists the admitted named required set and legacy/diagnostic observations separately. Existing fatal errors remain
fatal regardless of classification. Complete evidence means complete for the declared set, not exhaustive capability
coverage. A completely unadapted scenario is marked unattested; it does not gain proof merely by returning Success.

Default execution of legacy individual tasks remains available with its existing error/exit semantics. Before work,
the CLI prints the resolved selection and unattested disposition; at completion it says execution completed with
unattested evidence, not required checks passed. Its artifact preserves that distinction and contains no complete
required-proof summary. There is no opt-in strict flag: the named required suites and adopted selections always
require their declared observations. A legacy scenario cannot be inserted as a required member without a declaration;
that selection refuses before work. Empty required selections refuse when the caller requests a required suite.
Legacy diagnostic invocation is not a zero-member required suite and is labeled as such, not inferred as complete.

Thus default core-scenarios, core-inference-agentic, all five constituent tiers and slow-consumer are adopted and can
pass. Other existing tasks continue executing while honestly stating their evidence limit. This closes #1222's shared
contract plus representative repairs; it does not assert every historical tier is fully migrated. The existing release
owner must not cite an unattested report as required named proof. A current release gate's capability-specific proof
still rests on its own named assertions and existing release contract until its owner adopts the new report contract;
#1222 neither waives that gate nor adds a second owner for its defects. The docs enumerate this adoption boundary.

If the owner requires all existing release commands to supply the new named-report contract immediately, that is a
broader adoption option with explicit per-owner work and measured costs. It is not silently made a closure dependency
of this bounded implementation, and no new issue is created merely to offload its own acceptance.

## Task-owned checks and existing execution authority

Task keeps direct execution, Compose lifecycle and exit authority. No cmd/e2e process launches Task; no public task is
inverted into an internal target. Existing rc capture/defer blocks remain responsible for preserving child failure
through cleanup. The change extends their reporting, not their scheduling. Core's shell checks remain shell checks.

Add a narrow report seam in the existing cmd/e2e/results writer path. Task wrappers initialize the existing TestRun
format with resolved selection and invocation identity, record shell-check observations, and finalize it with the
observed command/cleanup status. The CLI's report mode is serialization/finalization only: it cannot execute arbitrary
commands, schedule work, retry, or decide success independently of the shared Result finalizer. Its consumers at
birth are core's existing shell assertion blocks and the existing tier/composite task wrappers. The report inputs
are typed records carrying a declared check ID, observed expected/actual values, outcome/reason and run/member ID.
They are not a new scripting language or a command that accepts only an arbitrary 'pass' string.

At core's existing comparison sites, emit records only after evaluating the actual exit code, shutdown log, listener
closure, blocked-boot cancellation, preidentity refusal and no-identity-record observations. The failure branch records
failed before preserving its existing exit. The healthy branch records passed only after the same condition. A final
'[OK]' banner or successful shell block does not create observations. A missing record is missing required evidence.
The reporter does not authenticate the test author; a Go author can also fabricate a result. Negative controls must
prove assertion reachability and that omission/wrong input/error becomes a failed record and outer task failure.

Only the task wrapper writes its aggregate TestRun; Scenario invocations and shell reporters write distinct member
records via the same existing Writer. Wrapper initialization supplies an opaque generated run directory/ID through
existing Task variables/environment so callers do not predict IDs. Child records include scenario/variant/round and
parent identity, preventing prior or sibling files from satisfying a member. Use unique files and atomic writes,
not a shared append journal. Finalization reads only the initialized run's declared records and rejects duplicates,
foreign run IDs, missing required members and conflicting outcomes. Comparison reports are not member evidence.

The reporting boundary begins at the first executable task command AFTER prerequisite dependencies succeed. Preserve
existing Task deps for build and check-ports, including check-ports' e2e:clean dependency: cleanup still precedes the
port probe. Do not move initialization ahead of those prerequisites or claim structured evidence before the reporter
binary exists. An adopted composite that previously had no build dependency gains the existing build:e2e prerequisite
solely so its first report command is executable. No new build runner is added. Existing dependency execution and
failure semantics remain Task-owned; a successful bootstrap is required before initializing a run.

Concretely, each adopted direct task initializes its run record as the first post-dependency command. Its existing
command body uses the established shell rc-capture pattern in one controlled command block: record command failure,
perform the existing cleanup, record cleanup failure, finalize the report, and exit nonzero if any of those failed.
Use one cleanup/finalization path so separate Task defers cannot reorder the final write ahead of cleanup. Preserve
original command status when later cleanup or reporting also fails; retain all failures in the record. A trap for
ordinary shell exit/signal invokes that same path where the shell can execute it; it never overrides a prior failure.
Task remains the executor, and Compose operations remain the same operations. No process coordination moves to Go.

An adopted composite initializes only after its own build/check-ports deps. It captures the exit of each existing
child Task invocation and finalizes after that child's own cleanup/reporting returns. A child whose prerequisites
fail has no child JSON; its already initialized parent records that member's nonzero Task status, missing report and
log reference, then fails required acceptance. A directly invoked task whose own build, e2e:clean or port-preflight
dependency fails BEFORE initialization has only outer Task status and logs: no structured run or final JSON is
promised. CI retains these logs and reports artifact absence explicitly. Local shell output is the evidence unless
the caller retained it. This exception applies equally to all three bootstrap failure classes and composites whose
own prerequisites fail, not just build failure.

After successful initialization, controlled setup, scenario and cleanup returns retain status/logs and attempt
finalization. A missing later member remains failed/incomplete. Reporter/finalizer failure itself makes the required
task fail; preserve the original failure as well. Abrupt Task/host death may leave only the initial incomplete record
and available child records/logs; no final-write or crash-resume promise is made. Failed initialization also produces
only outer failure/logs and cannot claim a complete artifact. Every boundary without a record remains unproven,
never a completed zero-check success.

Task selection membership remains explicit in the fixed E2E selection declarations, tested against actual task/CLI
commands and help. The test guards both missing and extra execution members; shell assertion obligations are named
in the same selected scope. Task owns order; declarations own the proof claim, and code observation sites own evidence.
No independent catalog of runtime components, production status store or task engine is added.

| Same-class responsibility | Existing owner retained |
|---|---|
| Selection and order | Task targets and cmd/e2e resolver; declaration-to-command contract tests |
| Start, stop and failure exit | Existing Task/Compose commands, rc capture and defer cleanup |
| Outcome classification | Scenario.Result finalization; no second task-specific success interpreter |
| Persistence/readers | Existing Result/TestRun Writer and comparison/release consumers |
| Interrupted run | Incomplete file evidence and task/CI logs; no new recovery owner |

This smaller option supplies structured selected-invocation evidence from successful report initialization onward,
including infrastructure/cleanup status and missing members. Pre-initialization dependency/build/report-init failure
has outer Task status/logs only; abrupt death can leave incomplete artifacts. Thus whole-invocation evidence includes
explicit log-only bootstrap failures and never promises terminal JSON for every controlled Task return. The required
gate remains red without complete evidence. These limits do not justify adding another execution owner.

## Evidence persistence and provenance

Wire results.CreateTestRun/Writer into the actual cmd/e2e path and narrow task-report mode. Required-proof summaries
include
coverage disposition and cannot use legacy AllPassed alone; comparison readers preserve this distinction. Version the
TestRun schema and extend it with selection,
required member IDs, per-member outcomes, provenance and evidence completeness. Keep Result as the child record;
TieredResults is optional domain detail projected from finalized Result. Rebuild metadata after final validation, so
Structured.Metadata.Success and Result.Success cannot disagree in an emitted record. Existing typed comparison
exports remain available and are explicitly analysis files; do not make their filename or score the acceptance oracle.
Legacy TestRun readers recognize old records as lacking required-proof identity, not as implicitly complete. This is
file/report compatibility only; it introduces no beta production-state migration or alias persistence service.

After its existing prerequisites succeed, each invocation with proof intent writes an initial incomplete envelope
before the post-bootstrap test infrastructure/body and attempts a terminal record on every controlled body return,
including Setup, Execute, validation and teardown failure. Build, prerequisite cleanup, port preflight or report-init
failure before that boundary retains only outer Task status/logs; no JSON artifact is promised. Capture failed partial
observations; do not replace them with an empty result. Write failures are hard proof failures and emit the failure
on stderr with intended absolute path; never claim an artifact exists when storage failed. Initial envelope plus
per-child records permit investigation after SIGKILL, but no guarantee of final serialization under process/host death.
Use atomic replacement for the aggregate file; unique child files are written once. No automatic old-run deletion.

Record: exact argv and resolved absolute working/output directories; UTC start/end; run/parent IDs; source full SHA
and dirty status; a digest of any working-tree patch and relevant untracked inputs; runner executable digest and Go
build information; actual app container image ID/digest and observed binary version/build identity; selected Compose
files/profiles/config/fixture digests and effective nonsecret settings; child exit/status; named expected/completed
checks; task log and artifact digests. Use runtime/compose observations, not the author's guessed image tag or config
path. Exclude secrets and secret environment values. Explicitly label scripted model/search fixtures and what they
prove. Runtime identity unavailable or a dirty tree is recorded, never filled with zero values or the runner's SHA as
though it were the app's. Candidate proof additionally requires clean exact-source match under the existing release
spec.

For direct remote endpoint runs, app/config provenance may be unavailable. The result can retain observed checks but
is evidence-incomplete and cannot claim full proof. Explicit diagnostic execution remains available, but no such
result satisfies a declared required suite or candidate proof; this is an inspection path, not optional enforcement.
No new production introspection API is authorized merely to fill a field. Existing Docker/task executions can observe
their configured binaries and artifacts; any unavailable identity stays a stated blocker for that proof scope.

Canonical Task paths resolve output directories from repo root rather than the current cd cmd/e2e spelling. CI's
existing jobs upload the run directory with always() after execution, including failures; artifact upload failure
must remain visible as missing hosted evidence. Local file success alone is not hosted retention. This is a narrow
change to current jobs, not new scheduling. Release owners attach/digest the selected run evidence under the existing
candidate-proof schema; local success is not tag authorization.

## Representative behavior repairs

Agentic: bind each observation to the submitted task/loop and expected tool request/result. Required tool proof reads
the actual execution outcome for that identity; a broad aggregate metric cannot substitute. Required streaming proof
observes the configured request's streaming path and required series/reachability, preserving request identity through
available trajectory/fixture evidence. Missing chunk-metric transport, absent required series or zero chunks is failed
required evidence. TTFT remains a
visible diagnostic with passed/failed/skipped status; a TTFT requirement would need a separate measured contract
decision and is not introduced here. If a metric lacks per-request labels, isolate the run and prove the baseline
and exact controlled delta alongside the request/terminal identity; do not assert attribution from a process total.
Preserve existing durable replay and approval/replacement tests; do not duplicate their whole race matrix in Docker.

Statistical: retain quality diagnostics, but add an independent deterministic controlled fixture/query requirement.
The expected entity IDs come from the submitted fixture and are compared with the actual graph/search consumer result,
not searchStats recomputation. Empty/wrong identity, observation failure or omitted query cannot pass. This does not
set a model relevance threshold. Existing general search statistics remain visibly diagnostic except existing hard
checks. Required components use the live required set, not the unwired helper's older set.

Core: prove the current protocol-flow pass-through configuration, not selective filtering. At the reviewed base,
configs/protocol-flow.json declares criteria={} and mappings=[]; historical >50 comments are not the oracle.
Generate controlled input records with run marker, sequence and known values, retaining a test-owned expected set
before transmission. Select output by that exact run marker. Require at least the configured MinProcessed distinct
successfully sent sequence IDs within the existing bounded wait; each observed record must have valid JSON, a known
sent sequence and the same expected fields/values for that sequence. Preserve multiplicity separately so duplicate
lines cannot inflate the distinct count. Any foreign/unsent identity in the run's selected output or corrupted field
fails content/identity proof. Inputs below and above50 are eligible under pass-through, with no fabricated >50
rejection rule. File count/retrieval failures fail; component health cannot substitute for output evidence.

This proves the configured pass-through path produced a minimum count of correct, run-correlated records. It does
not claim delivery of every UDP message, selective rejection, selective mapping, exactly-once output or ordering.
The existing MinProcessed requirement remains the count floor; no new selective-filter fixture, config/compose
change or production behavior is added. Mutation controls replace a selected record with malformed JSON, a wrong
run/sequence or wrong expected value, suppress run-correlated output below the floor, and restore the old
component-only observation fallback. The expected set and minimum criterion stay fixed across those controls.

Research/throughput defects remain #1224/#1288/#1195. Their results remain unattested until their owning contract is
adopted; this design does not repair a direct-route or result-readback bug by changing classification alone.

## Invariants and proposed spec home

There is no current e2e-tiers capability spec. After design review and owner acceptance, add an active delta for
`e2e-evidence` with the following proposed requirements; no current spec is promoted by this draft. Synchronize only
on completed implementation. Reference release-candidate-proof rather than duplicate its authorization rules.

| Proposed requirement | Invariant for every input/history |
|---|---|
| Selection determines proof scope | Unknown/empty required selection fails; selected members are explicit; exclusions never count as passes |
| Required observations determine success | Complete required-proof success implies every unique required ID has exactly one passed behavioral observation and no lifecycle failure |
| Evidence cannot improve by omission | Removing/renaming a required observation, skipping it or making it fail never yields success; diagnostic results do not fill its place |
| Observation identity is preserved | A result for another run/member/entity/loop cannot discharge the current obligation |
| Failure reaches the outer gate | Child/process/required-check/required-write failure yields nonzero proof command and task/job failure |
| Run evidence binds one invocation | Completed records preserve selection, identity, time and outcomes; partial/crashed/legacy records cannot be accepted as complete |
| Domain projections follow final outcome | Emitted TestRun/Result/TieredResults summaries agree with finalized required and lifecycle status |
| Diagnostics remain explicit | Optional recorded quality/measurement failure is visible and does not silently become required or passed |

The adoption enumeration is the reviewed Scenario implementations, Task core's shell checks, existing CI tasks and
release proof. Each outside the first required set remains unattested with named ownership, not an inferred migration
obligation. No new standalone adoption epic or fourth beta.165 package is created.

## Verification and sensitivity plan

PBT decision: generated properties are appropriate for required-set finalization, merge/projection and reordered
member records. Independent oracle is set equality and the proposed invariants above, not the production finalizer.
Generate small bounded named sets and observation sequences including duplicates, unknown IDs, wrong run IDs,
missing/failed/skipped required members and diagnostic outcomes. Deterministic examples force empty required set,
all missing, duplicate conflicts, failure followed by attempted overwrite, wrong identity and interrupted execution.
Assert activation by creating at least one required member in nonempty properties; separately exercise emptiness.
Use existing Rapid; retain replay witnesses. No new framework or random-hit quota.

Use native fuzz targets for new/changed exported result decoding/selection parsing: malformed/truncated/unknown
version input cannot be accepted as completed proof; valid encode/decode preserves required identity and status.
Ordinary seed replay is not claimed as exploratory fuzzing. Bound generated collections and run duration.

Named process-level examples drive the real cmd/e2e and fixed Task reporting wrapper through deterministic shell/runner
fixtures, retaining real Task exit semantics.
For each representative scenario, prove healthy control plus absent result, wrong result identity, observation error
and skipped callback; assert the named outcome, Result summary, child exit, task exit and retained failed artifact.
Drive default/all/semantic/rules/tiered selections, variant override/conflict, output directory propagation, unknown
selection, missing child, zero required members, serialization failure and post-execution validation failure. Force each
pre-initialization build, prerequisite cleanup and port-preflight failure: direct Task must fail with retained logs
and no claimed JSON; initialized composite must fail with the absent child's status/log reference. Also force
post-init setup failure, cleanup failure after otherwise passed checks, initializer failure and finalizer failure;
verify cleanup-before-probe ordering and cleanup-before-finalize ordering without Docker using existing task fixtures.
A stub command tests wrapper propagation only; it does not establish SemStreams behavior. Real assembled core,
statistical and agentic tiers provide production-path proof for the repaired observations; semantic/structural run
only as required by the shared selection/wiring change's blast radius. Serialize heavy gates with existing host rules.

Mutation criteria apply: this repairs regressions prior checks allowed to pass and introduces consequential acceptance
logic. Required bounded mutations: reintroduce tool/streaming warning-nil, omit statistical expected-identity assertion,
restore core component fallback/pass-through-content warning, turn required missing into diagnostic, discard child exit,
and bypass
failed-result save. Hold tests/fixtures/config fixed; demonstrate passing baseline, valid reached mutation, intended
assertion failure and restored passing baseline. Use cp backups plus hashes or isolated copies, never Git restoration.
Record survivors/inconclusive results and reviewer dispositions. No mutation or PBT execution has yet occurred.

No production test-hook gate is added for proof accounting. Fixtures inject transport/result failures through existing
HTTP/NATS observation seams or test-owned runner boundaries. Mechanism-level interleavings remain existing integration
and race tests; only representative composed paths run in Docker. Cost evidence records exact source, cold/warm cache
state, selected commands and actual UTC duration; no promised budget is invented from the historical1381s record.

## Adopter outcome, files and sequencing

Scenario authors declare expected named checks and record observations at assertions; the shared finalizer owns
acceptance and persistence. Forgetting declaration/observation fails at runtime with the missing ID before a green
claim, and declaration/dispatch tests catch missing adoption before expensive runs. CLI users choose a real scope;
resolution/output handling is shared. Evidence consumers read one authoritative TestRun with explicit completeness;
typed comparisons remain analysis. Unknown external report consumers are a compatibility risk, recorded in release
notes and schema versioning, not assumed nonexistent.

Planned write scope after acceptance: test/e2e/scenarios/scenario.go and local evidence tests/helpers; existing
results/writer.go and typed result projection/reader tests; cmd/e2e selection/execution/report paths and tests;
core dataflow and core lifecycle/refusal shell report sites; live tiered/statistical observation sites; agentic
observation sites and tests; Taskfile.yml and affected taskfiles for report/member propagation; existing E2E ladder
artifact retention; docs/contributing/01-testing.md and02-e2e-tests.md; one canonical pointer in reviewer/preflight
rather than copied rule prose; active e2e-evidence delta and minimal release-candidate-proof linkage if necessary.
No production config authority, recovery, graph API, NATS subject/bucket or deployed binary interface is added.

Sequence: reconcile landed #1402/#1403/#1404 before shared files; pin accepted declarations and proposed clauses;
implement shared finalization/writer and deterministic RED controls; adapt representative checks and shell record sites;
wire Task reporting wrapper and explicit selection; run focused/race/appropriate composed proof; measure resulting
costs;
update docs and evidence; independent implementation review; archive/spec sync last. Default enforcement cannot land
as a falsely green intermediate state. The developer may split commits but acceptance is the complete agreed contract.

#1117 retains semantic CI and path/quality decisions; #769/#1128 retain agentic/CRUD CI/persona; #1293 retains general
verification plumbing; #1195/#1224/#1288 retain capability defects. #1188's owner retains scenario/config authority
files until reconciled. No paid model call or full all-tier CI expansion is implied by acceptance of this design.

Decision skill: orchestration-check was read and applied. Existing Task sequences tests; cmd/e2e serializes and
finalizes observed
proof, with no production rule/lifecycle ownership. kv-or-stream/new-payload/query-pattern do not trigger: no new
NATS communication, production payload or graph query front door. Existing operation-specific query clients remain.

## Decisions and limits for owner review

The principal adoption choice is the bounded named required suites above versus an immediate all-existing-task
report migration. The recommendation keeps legacy execution available and explicitly unattested, while required
selections always enforce missing-required => nonzero. There is no permission to waive a release gate or claim a
legacy task has complete proof. Existing Task wrappers plus assertion-site records cover the adopted reporting boundary
without a new process coordinator.

Exact runner/app provenance availability, external report consumers and implementation cold/warm cost remain to be
measured. A direct remote run may remain diagnostic when identity is unavailable; no new production API is authorized
by that limitation. Full test and release acceptance remains pending implementation evidence and independent review.

Current coordination supplied by root after inventory: #1402 has merged; main visible through #1403's base is
73ea4f2650ded4792a0a7d53a3eb1306f0e16cdf. #1403 remains draft/open at
d7e27de276a7763ed9aa3f10a78ef61521079034; #1404 remains draft/open at
eac6b44cd81740ab324d2a10ed7d40cc2d96a2a1. This is an external coordination snapshot, not a rebase or source review.
Before implementation reconcile landed recovery/config changes and refresh affected source pins and design premises;
preserve the reviewed12ae inventory checkpoint as history. No shared file ownership is displaced.

This draft is ready for independent pre-owner design review. It is not approved, and owner acceptance is still required
before runtime/spec-delta implementation.

## Bounded design-review correction record

Independent review of draft3b2dfe96 requested two corrections (review SHA
 e87742960e134ef36305c2d65335448b183ba3ce8788ba0ca3a22f25e4c50865). This revision preserves the inventory identity.
First, it explicitly places reporting after existing Task prerequisites, retains e2e:clean-before-check-ports, defines
direct/composite log-only bootstrap failures and a single cleanup-before-finalization path, and adds deterministic
negative controls. Second, following root's chosen scope, it replaces selective-filter proof with current-config
pass-through count/content/identity and states the exact input-derived oracle and nonclaims. Structural zero-ML IDs
now explicitly describe clustering execution absence, not stored community absence. No source/spec changes, new
inventory sweep, experiments or owner acceptance are inferred. Independent bounded re-review remains pending.
