# #1222 bounded Task/report API clarification

Role: canonical architect, read-only clarification of the already accepted design, not implementation approval.
Base remains fe6e2cc03e16f5db47e293f55939548572f204cc; frozen inventory checkpoint and accepted design a31a5c69
remain unchanged. Input: /private/tmp/semstreams-1222-task-report-api-plan.md, read in full. The canonical architect
contract was reread. This does not supersede Result/Writer review findings or independently approve my own API work.

The proposed Task execution boundary, post-prerequisite initialization and reporting-only Go seam fit the accepted
design. Choose the following minimal shapes. The root should reconcile these into implementation-handoff.md before
new exports are implemented. No new owner scope ruling is required unless implementation changes these obligations.

## Identity and child verification

Add `ParentMemberID string` with JSON `parent_member_id,omitempty` alongside TestRun.ParentID. Require both parent
fields together or neither. ParentMemberID identifies an exact declared slot, including any explicitly declared
round suffix; do not derive it from Selection. The caller passes an already observed/generated parent identity.
Do not rebind or copy child Results into the parent's identity.

Keep the verifier with existing results.Writer, which already owns LoadRun. Concrete shape:

```go
type ChildExpectation struct {
    ParentID string
    ParentMemberID string
    Selection string
    RequiredMembers []string
}
type ChildArtifact struct {
    RunID string
    Path string
    SHA256 string
}
func (w *Writer) VerifyChild(path string, expected ChildExpectation) (ChildArtifact, error)
```

Its birth consumer is report-child for direct and composite Task invocations. Read bytes once, derive their digest,
and decode/validate those same bytes through the existing reader. Require terminal complete required proof,
matching parent ID/slot/selection and exact expected required-member set; preserve child's own run identity. The
expected set comes from the resolved selected scope already used at initialization, never a fresh second catalog or
what the child happened to report. Returning a verified reference is sufficient; do not export an execution handle.

The external observed child process exit is separately required to be zero by report-child. A valid artifact cannot
turn nonzero process exit into success. Missing artifact, failed prerequisites, digest/reference mismatch, foreign
parent/slot, wrong selection or legacy/incomplete evidence becomes a failed parent's required observation with the
reason and available log reference. A failed verifier need not pretend it returned a verified artifact; reporter
still retains the supplied path and failure reason as diagnostic evidence. Do not demand authenticated authorship,
recursive remote filesystem availability, or a separate signature/security layer.

## One declaration and member-file owner

Initialization snapshots resolved requirements before the body. Reuse the existing Result representation for each
initial member declaration (RunID, MemberID, CheckRequirements, unattested); no TaskResult or second shell classifier.
RequiredMembers remains the exact suite membership authority. Initial declarations and final observed records are
states of the same existing aggregate, not a recovery service.

Use one shell Result member per independently emitted assertion site/group. A group may contain several checks only
when that same owning shell block retains and emits its entire declared observation set once. For core's existing
separate comparison sites, individual members avoid requiring mutable intermediate member files. Each can contain
one named required CheckRequirement. The same initialized declaration supplies RecordCheck validation; do not create
successful-stage observations. Preidentity seed remains setup only; refusal/no-record observations are actual checks.
The CLI-child member and graph-child member are separate verified artifact observations, not restated child checks.

Writer member methods with immediate report-record/finalize consumers:

```go
func (w *Writer) WriteMember(run *TestRun, member *scenarios.Result) (string, error)
func (w *Writer) LoadMember(run *TestRun, memberID string) (*scenarios.Result, error)
```

`run` is the initialized aggregate snapshot, not an arbitrary new CreateTestRun. Require the member slot and its exact
declarations to match that snapshot; identities must match. Finalize the observed member using existing Result
methods. Failed partial members are valid retained artifacts and must be written; failure is an outcome, not a reason
to discard their bytes. Unknown/foreign/duplicate submissions must fail, with the caller retaining that report error
as an aggregate lifecycle failure. Filenames derive from validated run/member identities within the existing Writer;
creation is atomic no-clobber. LoadMember uses the same filename derivation, validates decoded identity/declarations,
and preserves failed evidence. Do not locate members by broad directory glob or newest-file lookup.

These signatures introduce no parallel acceptance owner: existing Result finalizes checks; existing Writer checks
membership/persistence; the reporter only resolves input and propagates errors. If implementation can reuse an
existing identical method after reconciliation, reuse it rather than adding these names beside it.

## Command, cleanup and first-terminal ordering

Add `CommandExitCode *int` and `CleanupExitCode *int` to TestRun, with JSON command_exit_code and cleanup_exit_code
omitempty. These are observed execution facts used by terminal reporting, not provenance strings. Nil means not
observed/not applicable and never a guessed successful zero. Existing ExitCode remains overall proof-command exit,
finalized by Writer. Record both nonzero statuses even when one alone is enough to fail. Actual setup/execute/teardown
error reasons remain with existing Result/Error/Errors or the retained lifecycle log; no invented behavioral check is
needed just to mirror an exit code.

Present consumers are report-finalize and evidence readers. Task reports both observed statuses after its one cleanup
path. A direct CLI can leave task cleanup status absent and retain its existing scenario teardown errors. Do not make
missing task fields an unconditional failure for unrelated direct/legacy selectors. Require applicable task statuses
according to the initialized resolved task scope; a selected task cannot erase cleanup status to obtain complete proof.

If an assertion fails, record that failed member once, then enter the existing cleanup/finalization trap. The trap
never records the member again or overwrites it with a pass. If recording fails, preserve the report failure and force
outer nonzero even if later checks/cleanup succeed. Finalize reads all declared member paths, retains present partial
outcomes, derives missing members and command/cleanup/report failures, then invokes WriteRun once for terminal bytes.
WriteRun downgrades provisional zero before serialization; write errors always return nonzero with intended path.
An initial nil exit stays nil. Bootstrap dependency failures remain log/status only before initialization.

## Provenance and validation boundaries

Retain a digested constituent manifest through the existing Environment artifact_manifest_path/sha256 references.
The manifest records each selected path/digest and distinct observed app phase, including core's production and
fixture binary/image/build identities. Never overwrite the first phase with the second. Include resolved absolute
output location, observed selected settings and log references/digests. Unavailable required provenance prevents a
complete claim. This is the pending producer adoption for Result/Writer review R2, not permission to weaken the writer
predicate until producers exist. No new production introspection or secret serialization is authorized.

The plan's four report actions may use private option structs/helpers. These transport typed records; they do not
run arbitrary work. Their declaration source must be the one resolved scope tested against actual public Task
membership. Tests must exercise actual report dispatch, not a duplicated report-only classification path.

Focused controls: valid child; same parent wrong slot; wrong selection/membership; legacy/missing child; child exit
nonzero despite complete JSON; duplicate member cannot replace prior failure; failed shell comparison followed by
successful cleanup remains red; command and cleanup both fail and both survive round-trip; missing cleanup status
for a task cannot pass; first write/read uses exact bytes/digest; two core phases remain distinct in the manifest.
Use the accepted properties/spec homes for identity, omission, outer failure and invocation binding. These are
planned controls, not evidence they have executed. No Task edit or implementation is approved by this note.
