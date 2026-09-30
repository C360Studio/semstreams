# Accepted E2E implementation handoff

Implementation base: fe6e2cc03e16f5db47e293f55939548572f204cc.
Authority: accepted design a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c;
acceptance.md and issue1222 comment5857272941. This is a concrete implementation handoff within that design,
not approval of code or verification. No source/spec file was changed by the architect.

## Single owners and exact first-slice shapes

Keep Scenario's existing five methods unchanged so legacy implementers remain usable. Place these value types and
methods with existing Result in test/e2e/scenarios (a focused evidence.go file is acceptable). No production pkg,
NATS, graph or second stages validator is added.

```go
type CheckRequirement struct {
    ID string `json:"id"`
    Required bool `json:"required"`
}

type CheckObservation struct {
    ID string `json:"id"`
    RunID string `json:"run_id"`
    MemberID string `json:"member_id"`
    Status string `json:"status"` // passed, failed, skipped; reject all others
    Reason string `json:"reason,omitempty"`
    Evidence map[string]string `json:"evidence,omitempty"`
}

// Add to Result; retain existing time, errors, details, structured fields.
RunID string `json:"run_id,omitempty"`
MemberID string `json:"member_id,omitempty"`
CheckRequirements []CheckRequirement `json:"check_requirements,omitempty"`
CheckObservations []CheckObservation `json:"check_observations,omitempty"`
EvidenceStatus string `json:"evidence_status"` // complete or unattested

func (r *Result) DeclareChecks(runID, memberID string, checks []CheckRequirement) error
func (r *Result) RecordCheck(observation CheckObservation) error
func (r *Result) FinalizeChecks() error
```

DeclareChecks is called once before observation; validate nonempty identity/IDs, uniqueness and a nonempty required
set, copy declarations, initialize unattested. RecordCheck validates declared ID, run/member equality, known status,
reason for non-passed status, and absence of a prior observation for that ID before append. Evidence holds bounded
string identities, expected/actual summaries and artifact references, never payload bodies/secrets. Evidence map
keys do not independently establish acceptance; the capability assertion compares values before recording.

On refusal, preserve existing observations and append the failure to Result.Errors so ignoring a returned recording
error cannot later produce proof success. FinalizeChecks revalidates serialized fields (callers/decoders can construct
structs directly), derives missing/non-passed required IDs, incorporates existing Error/Errors and any separately
recorded setup/execute/teardown errors, and computes AssertionsRun from passed+failed REQUIRED observations only.
Only successful finalization sets EvidenceStatus=complete and Success=true. Failure sets Success=false and retains
unattested plus actual failed/partial observations and specific errors. Unattested thus does not hide whether the
failure was missing, skipped or observed: those values remain in records/errors. No separate mutable missing counter.

Finalization must be repeatable without accumulating duplicate derived errors; keep derived diagnostics distinct
internally or rebuild deterministically from preserved source errors. It must never erase an original execution or
recording error. Do not pass Result.Success=true into finalization as an independent acceptance vote. Legacy callers
with no declaration retain existing execution Success and unattested status; the CLI does not call strict finalization
on them unless selected as a REQUIRED member (which refuses). No change to legacy count meaning is claimed before
adoption; required-proof readers ignore legacy count/Success.

Declared IDs belong to the accepted selection/capability, not to successful stage names. Repeated rounds get distinct
member or check IDs before execution. CheckRequirement.Required supplies classification; do not add a second enum
or duplicate flag in CheckObservation. No callbacks, state-machine engine, retries or plug-in registry live here.

## Runner declaration bridge

The runner must know requirements before Setup. Use an OPTIONAL private interface in cmd/e2e:

```go
type checkDeclarer interface {
    CheckRequirements() []scenarios.CheckRequirement
}
```

Adopted scenario types implement that method; legacy Scenario interface remains unchanged. The same method supplies
Execute's result declaration, avoiding a copied expected list. CLI resolves selection, obtains declarations before
Setup, creates its initial Result identity, and passes run/member identity through the existing explicit scenario
configuration for adopted constructors (not stored context/global state). For first-slice tests, use a small fixture
Scenario implementing the optional method. Do not edit held agentic/tier-authority/platform-identity files yet.

On Execute return, validate the returned declared IDs and run/member identity against the resolved expectation;
never replace expected membership with what the returned result happened to contain. Setup/Execute/teardown errors
populate the retained Result even when Execute returns nil. Nil-result/nil-error is failed execution, not a panic or
legacy success. Finalize before projecting metadata/writing, attempt failed writes before returning exit, and make
required persistence failures nonzero. Legacy output must say execution-only/unattested rather than proof passed.
Semantic/rules/default selection reconciliation is a separate coherent CLI slice; do not silently mark all constructors
adopted while only the shared fixture supports declarations.

## Existing Writer/TestRun extension

Keep results.Writer the only filesystem owner and Result the child representation. Add schema version and explicit
proof disposition to TestRun; extend TestRunConfig with resolved required member IDs and exclusions. Retain its ID,
Timestamp, Duration, Config, Scenarios, Summary and Environment. Use the same Result fields for scenario and shell
records; do not create TaskResult/ProofResult or a second shell classifier.

```go
// Add to existing TestRun.
SchemaVersion int `json:"schema_version"` // new writer emits 2; absent old version stays unattested
ParentID string `json:"parent_id,omitempty"`
EvidenceStatus string `json:"evidence_status"`
Command []string `json:"command,omitempty"`
WorkingDir string `json:"working_dir,omitempty"`
StartedAt time.Time `json:"started_at"`
CompletedAt time.Time `json:"completed_at,omitempty"`
ExitCode *int `json:"exit_code,omitempty"` // absent means not observed, never successful zero

// Add to existing TestRunConfig.
Selection string `json:"selection"`
RequireEvidence bool `json:"require_evidence"` // derived from resolver, never an optional-strict user flag
RequiredMembers []string `json:"required_members,omitempty"`
ExcludedMembers []string `json:"excluded_members,omitempty"`
```

For the first coherent slice, use existing Environment as the named string map for provenance; do not grow a competing
provenance struct without a present reader. Defined keys: source_sha, source_dirty, source_patch_sha256,
source_untracked_sha256, runner_sha256, runner_build, app_image_id, app_image_digest, app_binary_sha256,
app_build, compose_sha256, profiles, config_sha256, fixture_sha256, effective_settings_sha256. Missing observations
use explicit unavailable entries/reasons; never guess. Logs/artifacts are retained paths/digests in the run's existing
metadata/evidence references. Do not record secrets. Exact multi-file digest sets must retain constituent paths/digests,
not only a combined digest; use a referenced retained manifest rather than opaque packed JSON in Environment.

Wire CreateTestRun/WriteRun from CLI; update summary generation to distinguish legacy AllPassed from complete proof.
A required run is complete only when exact required member set is present once, identities match, each required Result
finalizes successfully, command/cleanup status is successful and required evidence writing/provenance is available.
Use the existing run ID generated by the writer boundary and preserve it across initial/final aggregate writes;
do not call CreateTestRun again to generate a second identity on finalization. Writer errors return normally and are
not swallowed. Initial record has absent exit/completion and unattested status. Writer owns filename identity as well
as bytes: use a unique generated run ID in the aggregate basename
(e.g. e2e-results-<variant>-<runID>.json), not variant plus seconds. Initial and final writes resolve to that SAME
aggregate path; atomic replacement is for that invocation only. Child member basenames include run/member identity
and cannot overwrite siblings or another run. This preserves ListRuns' e2e-results discovery convention; never add
a second CLI filesystem writer. Reject a foreign/duplicate member before writing, rather than overwriting its file.
Preserve failed partial Results.

Extend existing WriteRun rather than adding a parallel artifact family. Add a member-write method only when task
reporting consumes it in the same implementation slice; first CLI slice can write the aggregate containing its one
Result. Existing WriteLatest remains optional and cannot be proof authority. Existing typed SaveStructuredResults
is an explicitly labeled analysis export rebuilt from finalized Result, never the only persisted run record.
LoadRun accepts legacy schema as unattested; unknown newer schema or malformed required-proof records must be errors,
not silently decoded complete proof. Validate on read before acceptance/comparison. No old deployed-state migration.

## Slice order and independent oracles

1. Result declaration/record/finalization with deterministic boundary cases and Rapid properties. Cite the active
   e2e-evidence requirement headings exactly: Required observations determine success, Evidence cannot improve by
   omission, Observation identity is preserved, Diagnostics remain explicit. Oracle is independently declared sets
   and generated observations, not a call to FinalizeChecks for expected values. Force empty/duplicate/missing,
   wrong identity, failed/skipped, diagnostic-only, ignored error and attempted overwrite cases.
2. Existing Writer and readers: initial/failed/final records, stable ID, unavailable identity, malformed/newer schema,
   legacy unattested, atomic write failure. Cite Run evidence binds one invocation and Domain projections follow
   final outcome. Fuzz decode input; ordinary seed replay is labeled separately from fuzz exploration.
3. CLI using fixture Scenario and real runScenario exit path: setup fail, nil result, execution fail, cleanup fail,
   final semantic-style failure after Structured was built, output write fail, unknown/empty required selection.
   Cite Selection determines proof scope and Failure reaches the outer gate. No paid/Docker run is needed for these
   sensitivity controls, but they do not substitute for later real core/statistical/agentic adoption proof.

Mutation plan remains accepted design: make missing required pass, overwrite failed with passed, swallow outer error,
skip failed serialization, and restore representative warning-only callbacks during later adoption. Baseline/mutant/
restored results remain unrun; preserve cp/hash isolation and exact proof artifacts when executed.

Task reporter serialization, shell assertion record sites and post-prerequisite rc/finalization wiring are later
coherent slices using the same models. They must preserve bootstrap log-only exceptions and cleanup-before-probe;
no Go process coordinator, new task engine or lifecycle-shell migration is authorized. Shared #1404-owned files
remain held. No first-slice GREEN result claims the real scenario/task adoption is finished.

## Implementation clarifications within the accepted design

On the first implementation slice, the developers identified two representation gaps. The root resolved them
without changing the accepted proof scope:

- Resolve run/member identity before constructing adopted scenarios. Internal CLI fields carry those values into
  explicit constructor configuration; no mutable identity-binding interface, context value or global is added.
  The runner snapshots requirements before Setup and retains that expectation through execution.
- Add `TestRunConfig.Selection` for the resolved selection, because a core-scenarios run has no model `Variant`.
  Variant retains its tier meaning. Filename validation uses the resolved safe selection when necessary; run/member
  identity and validated report content remain authoritative.

The CLI may factor a private execution helper returning the retained Result and exit status, while one invocation
owns initial/final aggregate writes for all its selected members. Tests and real dispatch must use the same
lifecycle/finalization/persistence path. These are implementation details of the accepted single-writer boundary.

The bounded [Writer shape assessment](evidence/writer-shape-review.md) resolves terminal ordering without adding
an exported preflight validator. `WriteRun` retains the existing results-owned acceptance check and finalizes the
overall command exit before its first terminal marshal. An observed provisional nonzero is preserved; an observed
zero becomes one when a required invocation lacks proof. Initial absent exit/completion stays absent. CLI returns
the finalized exit after persistence, or nonzero on write failure. Underlying child/process/cleanup observations
remain unchanged and separately retained. Readers reject forged zero/incomplete required proof without repair.

`TestRunConfig.RequireEvidence` preserves the existing resolver's required/legacy intent independently of membership.
True with an empty required set fails. False remains unattested and may retain successful legacy execution. Adopted
selectors always set true; there is no caller-facing strictness opt-out or second selection catalog. The unconditional
terminal-aggregate rewrite refusal was an implementation inference and is removed; same-run atomic writes, cross-run
identity protection and child write-once behavior remain. No new correction history or process coordination is added.

## Resumed Task/report implementation handoff

On the owner's explicit resume, root reconciled and adopted the bounded
[Task/report API clarification](evidence/task-report-api-clarification.md) within the already accepted design.
Its input plan and content hashes are retained beside it. This authorizes implementation of those concrete shapes,
not a claim that code or evidence has passed review. Required proof and legacy semantics stay unchanged.

Use `ParentMemberID` for the exact declared parent slot; preserve child run identity. Writer owns `VerifyChild`,
`WriteMember` and `LoadMember` as specified in that clarification. Declaration snapshots and expected child selection
and membership originate from initialization's resolved scope, never the submitted observation/child or another catalog.
Retain observed `CommandExitCode` and `CleanupExitCode` separately from overall Writer-finalized `ExitCode`;
applicability belongs to the initialized Task scope, not a caller omission. No reporting command launches work.

For R2 and the actual producer, Environment uses `log_path`/`log_sha256`,
`artifact_manifest_path`/`artifact_manifest_sha256`, and `output_dir`. Writer records its resolved absolute output
directory. Complete proof requires those applicable reference shapes and declared provenance; missing/unavailable
references fail. Historical record loading does not reopen original machine paths. Actual Task/CLI producers and
child verification must demonstrate retained bytes and matching digests, including both core app phases.
No authentication layer or new production introspection is added. If implementing the concrete declaration snapshot
requires a further public shape beyond this handoff, return that bounded need for reconciliation before adding it.

Task initialization additionally persists `TestRunConfig.ChildExpectations []ChildExpectation` as a typed snapshot,
with JSON `child_expectations,omitempty`. ParentID may be omitted there and supplied from the enclosing run ID
when verifying; parent slot, selection and exact required membership come from the stored declaration. Reject
unknown/duplicate slots and empty required child membership. The private Task resolver reuses the existing CLI
selector for CLI-child membership and is checked against actual public Task commands; it does not duplicate that map.

`TestRunConfig.RequireTaskStatuses bool` (`require_task_statuses,omitempty`) records applicability selected by
Task report initialization. It has no optional-strict user flag. Required Task proof needs observed zero command
and cleanup exits; direct CLI does not inherit a fictitious task cleanup. Preserve this bit and all declaration
configuration across initial/final writes. ChildExpectation retains the same applicability bit for composite
children, preventing a loaded child from dropping Task status obligations while retaining a superficially complete
artifact. This is the smallest representation of the accepted initialized Task scope, not a Writer selector catalog.

## Constituent manifest persistence clarification

The bounded [architect advice](evidence/manifest-shape-advice.md) and
[independent shape assessment](evidence/manifest-shape-review.md) extend the existing Writer at a measured missing
persistence seam. Root authorizes this representation within the accepted design; code review remains outstanding.

Rename the uncommitted ChildArtifact value to ArtifactReference, with RunID, Path and SHA256 and no compatibility
alias. VerifyChild returns it. Add Writer.WriteManifest(initialized *TestRun, data []byte)
(ArtifactReference, error), using the retained initialized declaration snapshot before observed members replace it.
Reject terminal/foreign snapshots and malformed/non-object JSON; derive a fixed manifest filename; synchronously
retain exact bytes atomically without overwrite and calculate their digest. Reporter remains the only manifest
content/phase interpreter. Duplicate or failed writes return no successful reference. Manifest failure still attempts
a failed terminal aggregate and returns nonzero. Later same-run terminal corrections reuse the immutable manifest.

ArtifactReference.Path is uniformly relative to the emitting Writer's resolved absolute output directory, available
as its initialized run.Environment.output_dir. VerifyChild normalizes relative Writer construction and absolute input
before deriving that reference. The parent resolves the returned child-report reference against its own output_dir;
VerifyChild resolves the child's embedded manifest/log references against the child's recorded output_dir.
No caller predicts filenames or digests; no new path abstraction or generic artifact service is added.

Actual child verification reads retained log/manifest bytes and compares their recorded digests. Historical LoadRun
does not reopen machine-local references. Shell capture closes the lifecycle log after cleanup and before hashing;
reporter/finalizer output must not append to the already-digested log. Core's two app phases stay distinct in the
reporter-prepared constituent manifest. Source/fixture/runtime identity is observed, never filled with runner guesses.

## Actual CLI child provenance and closed logs

The first consumer wiring established a concrete sequencing gap: parent Task provenance cannot make an already
incomplete CLI child complete, and the CLI cannot digest Task's redirected child log while that process is still
writing it. This is an implementation obligation within the accepted design, not a new proof exception.

Use one private typed evidence-input schema and preparation path for CLI/report finalization. An internal
`--evidence-input` path carries Task-observed application phases/image/binary/build facts and selected constituent
paths. Read the input once, validate its typed content and relevant parent/slot/selection, and recompute constituent
file digests. The producer observes its own source SHA/dirty/patch/untracked inputs, runner binary/build information,
resolved output paths and effective nonsecret CLI settings. Callers do not predict facts already available there.
Supplied log/manifest digest strings cannot replace hashing the retained bytes.

CLI captures its own execution log through the existing logging seam and flushes/closes it after scenario teardown.
It then prepares/persists the manifest and attempts terminal WriteRun; subsequent reporter diagnostics go elsewhere.
Task's outer lifecycle log stays separate and closes after cleanup before parent finalization. Raw log capture belongs
to its execution producer; Writer remains the authoritative structured run/member/manifest persistence owner.
No generic file-handle service or process coordinator is needed.

Absent remote app provenance remains explicit and prevents complete required proof; this input is not a strictness
opt-out. Missing/wrong input identity, malformed input, unavailable observations and failed log/manifest persistence
need behavioral controls. A manifest/log failure still attempts failed terminal evidence with partial observations.
The private schema/preparation serves both CLI and reporter, with the existing Result/Writer acceptance unchanged.

## Preserve initialized behavioral declarations

The consolidated Writer review identified two missing pieces of the original declaration-snapshot contract.
When an initial aggregate stores each member's CheckRequirements, terminal required acceptance must preserve that
exact set, including each ID and Required flag. Removing, renaming or demoting an obligation cannot improve proof.
Failed/missing members still retain failed terminal evidence; observations and outcomes legitimately change during
execution. No Writer selector/check catalog or authorship guarantee is introduced.

The adopted CLI stores its pre-Setup Result declarations in the initial aggregate, keeps an untouched snapshot for
manifest creation, and replaces those member slots with actual results instead of appending duplicates. Behavioral
controls read the actual initial JSON during Setup and exercise valid/failed/missing/drifted terminal transitions.
This corrects W1/C5 within the accepted handoff rather than adding a new proof scope.
