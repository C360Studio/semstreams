# #1222 Task report API plan for architect review

This is a pre-implementation API/file plan, not a report that wrappers are wired.
Current CLI slice and Result/Writer authority are in the same #1222 worktree.
No Taskfile, workflow, report-mode or Writer-child code has been changed here.

## Exact identity hierarchy

1. Composite invocation `C` has TestRun.ID=C, Selection=`core-inference-agentic`,
   RequiredMembers=`core,structural,statistical,semantic,agentic`.
2. Each direct Task invocation `D` gets a new TestRun.ID, ParentID=C and an
   exact parent slot. Propose the smallest missing field, TestRun.ParentMemberID,
   because ParentID alone cannot distinguish repeated same-selection rounds.
   The slot is one declared composite member (or a round-qualified ID).
3. Each child CLI invocation `E` gets another TestRun.ID, ParentID=D and
   ParentMemberID naming its direct Task slot. CLI `all` has E's two Results:
   `core-health` and `core-dataflow`, both RunID=E and distinct MemberIDs.
4. Direct Task D records existing shell comparisons as its own Result members,
   with RunID=D; preidentity seed is setup, not a behavioral proof member.
   Required observations are the refusal and no-record comparisons made after
   the poisoned boot. No child Result is copied into D or C with a new ID.

The parent must validate child schema, complete required status, exact
ParentID+ParentMemberID+Selection, required member coverage, and artifact
digest. Exit zero alone and legacy/unattested child records are insufficient.
The parent then records only its own child-artifact verification observation,
with child path/digest as evidence; it does not restate the child's checks.
This verifier belongs in `test/e2e/results` beside Writer.LoadRun and accepts
expected parent ID, exact parent slot, selection and artifact path. Its present
consumer is report finalization for the direct and composite Task wrappers.

Concrete proposed verifier seam (subject to architect shape review):
`results.Writer.VerifyChildRun(path string, expect ChildExpectation) (ChildReference, error)`,
where `ChildExpectation` carries `ParentID`, `ParentMemberID`, and `Selection`,
and `ChildReference` carries the child's immutable `ID`, relative path and
SHA-256. Verification calls the same `LoadRun`/required-proof acceptance as
other readers and checks the expected parent slot before returning. The
reporter records a failed parent observation on verifier error. It never
changes or flattens child `Result.RunID`/`MemberID`. The CLI child accepts
parent ID and slot as explicit invocation inputs and persists them on its
initial record; sibling runs in repeated rounds therefore cannot substitute.

## Narrow CLI report commands

Use one mutually exclusive report action under cmd/e2e; no command execution,
Compose launch, retry or scheduling in Go.

- `--report-init --selection <fixed adopted task selection> --output-dir <absolute>`
  after Task deps creates an incomplete TestRun through CreateTestRun/WriteRun,
  returns machine-readable run ID and aggregate path. Optional generated parent
  ID and parent slot are passed from the owning Task wrapper, never guessed.
- `--report-record --run-dir <path> --observation-file <typed JSON>` reads one
  declared shell member's typed Result/CheckObservation. The shell supplies the
  actual comparison at the existing assertion site, including failed branch;
  record validates ID/status/reason and writes one immutable member file via
  Writer. No arbitrary executable or free-form pass command is accepted.
- `--report-child --run-dir <path> --member <declared slot> --child-path <path>
  --child-exit <n> --log-path <path>` verifies an available child artifact as
  above and records the parent member. Missing child JSON (including child
  prerequisite failure), invalid/foreign/legacy child, or nonzero child exit
  records failed with status/log reference; it never fabricates a pass.
- `--report-finalize --run-dir <path> --command-exit <n> --cleanup-exit <n>
  --log-path <path>` reads only the initialized run's declared member records,
  rejects duplicate/foreign/missing members, retains command/cleanup failure
  and log digest, then calls the same Writer.WriteRun exactly once for terminal
  evidence. The Writer decides the final proof exit/status. Failure to write
  returns nonzero and reports the intended absolute path on stderr.

The report actions consume fixed selections from the actual Task/CLI command
membership contract test; no second catalog of scenario check IDs lives in
cmd/e2e. Shell member IDs are declared once with the Task scope and recorded
after each comparison. `Result.RecordCheck`/`FinalizeChecks` remain the one
check classifier. The aggregate Writer remains the one filesystem owner.

## Task file edits after API review

- `taskfiles/e2e/core.yml`: keep build+check-ports deps (including clean before
  port probe). A single post-deps controlled body initializes D, runs existing
  Compose/startup/early-cancel/preidentity/fixture phases, emits shell records
  at each comparison, captures the CLI `all` and graph child reports, cleans
  up, then finalizes. The existing inner early-boot cleanup cannot override
  the outer finalization trap. Preserve all existing fatal comparisons.
- `taskfiles/e2e/{structural,statistical,semantic,agentic,slow-consumer}.yml`:
  keep public target and deps; initialize after deps, capture current Compose
  and CLI statuses, observe provenance while containers still exist, cleanup,
  finalize. No public target becomes an internal Go-run target.
- `Taskfile.yml`: descriptive `e2e:core-inference-agentic` owns five direct
  child invocations; `e2e:all` prints exact membership and exclusions as an
  alias. Parent C captures each child Task status/report before advancing.
  Child boot dependency failure has no child JSON; C records failed member
  with available Task status/log reference.
- `.github/workflows/e2e-ladder.yml`: keep statistical and independent
  slow-consumer jobs; add always() artifact upload from the canonical run
  directory, with missing uploads visibly unsuccessful.

Task controls command and Compose execution. A report helper, if factored,
may only serialize typed observations, collect bounded provenance or define
the current target's cleanup/finalize glue. It cannot execute arbitrary work.

## Provenance and bootstrap boundary

Task passes the root-absolute output directory; cmd/e2e does not infer it from
`cd cmd/e2e`. Initial report occurs only after build and check-ports deps.
Before init failure is Task/CI logs and nonzero only. After init, one cleanup
path runs before final report; ordinary shell signal/exit trap uses that path.

Observe actual Docker image IDs (`sha256:<64hex>`), binary/build identity,
selected Compose/profile/config/fixture bytes and nonsecret effective settings
before container removal. Registry RepoDigest may be unavailable for locally
built images and must be labeled so; local immutable image ID suffices for the
identity field. Core has both production and fixture app phases; a retained
constituent manifest lists each phase/path/digest/image ID, referenced by
artifact_manifest_path/sha256. Do not overwrite the production identity with
the later fixture identity. Source SHA/dirty state and patch/untracked digests
are observed, never guessed. Keep secrets out of records.

Concrete manifest producer/consumer seam: after Task observes the actual
files, container image IDs and logs, the reporter accepts a bounded typed
manifest input with entries `{role, path, sha256, image_id?, build?}` and
writes a canonical JSON manifest under the same run directory through Writer
before terminal aggregate serialization. Roles are a fixed allowlist
(`source_patch`, `source_untracked`, `runner`, `compose`, `config`, `fixture`,
`effective_settings`, `app_phase`, `task_log`, `artifact`); `app_phase`
supports multiple entries such as production and fixtures. File entries
require a real existing file and a digest recomputed by the Writer; image
entries carry the observed immutable Docker image ID and phase, with registry
digest explicitly unavailable when local. The aggregate references this
retained manifest by relative path plus SHA-256 in its existing Environment.
The Writer's terminal required-proof check verifies the manifest exists,
matches its digest and contains the source/config/app/log constituents that
the corresponding Environment keys summarize. LoadRun repeats that check
from the aggregate's directory for a purportedly complete run; a missing or
changed manifest demotes/rejects proof. Initial incomplete records need no
manifest yet. This makes the existing artifact_manifest_path/sha256 keys an
actual acceptance input rather than optional decoration. A digest alone is
insufficient because it loses constituent identity and cannot be inspected.
No separate provenance struct is added to TestRun; the typed manifest is a
retained referenced artifact with one present Writer reader.

## Review questions before new exported/report surfaces

1. Confirm TestRun.ParentMemberID as the minimum exact parent-slot field and
   `results` child verifier method shape; both have direct Task consumers.
2. Confirm the fixed Task selection/member declarations and shell Result
   cardinality, especially core's setup-only preidentity seed and cleanup
   lifecycle failure representation.
3. Confirm whether separate command and cleanup exit values are retained via
   named allowed Environment keys plus digested logs, or need a more explicit
   TestRun field. Never collapse a failed child plus failed cleanup into an
   unexplained single status.
4. Confirm reporter member-file ordering and one-write rule when an assertion
   fails and a shell trap runs, so a later pass cannot overwrite failed proof.
