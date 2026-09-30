# Bounded Writer finalization API clarification

Role: semstreams-architect, read-only implementation-shape advice within the accepted design. This is not code
approval, a test result, or an independent review of my earlier API handoff. No source/spec changes or tests run.

## Identity and inspected boundary

Shape artifact: `/private/tmp/semstreams-1222-writer-finalization-shape.md`, SHA256
 ee7ba84278db5f0d49c5df562e40edeed6ea688bbe2d9557d42a44f4a67a44e4.
Inspected file hashes match that snapshot exactly:

- results/writer.go: 92651bcbcce944fae814e5baa112e3bfac10157d555b075b68f9088326661695
- scenarios/scenario.go: 55944b35bb995c94b72d2f3376c78d170d4bbb8b128e2f7a3149f2ec1c5248e4
- scenarios/evidence.go: 5ffa1c2f0416a3bdcd672f03c1ad4952cfa9998bd9c68e6ad270285f61069c60

Read only Writer types19–58, WriteRun120–195, aggregate write guard229 onward, LoadRun312/decodeRun,
ValidateRequiredEvidence368, evaluateRequiredProof373 and checkRequiredEvidence380 onward; compared accepted design
persistence/final-outcome clauses and active delta Failure reaches the outer gate, Run evidence binds one invocation,
Domain projections follow final outcome and bounded legacy selection. I did not inspect active runner edits or
review the complete implementation. The author's snapshot is explicitly untested after its last patch.

## Decision: existing WriteRun owns final overall exit

Choose B. Keep existing exported `Writer.WriteRun(run *TestRun) (string, error)` and private evidence evaluation.
Do not introduce exported ValidateRequiredEvidence or a second exported FinalizeRun phase merely to work around
write ordering. Private evidence checks remain shared by writing and loading; Result.FinalizeChecks remains the
single child check classifier. This is an implementation detail of the accepted single outcome/writer owner.

A is defensible if callers genuinely need acceptance before persistence for another operation, but the observed
consumer only needs a coherent final artifact and return status. Exporting a preflight check adds a required call
order and lets a caller forget it while the Writer still computes its own status. FinalizeRun would also work but
creates another public phase/return shape without a demonstrated second operation. Neither is needed for this slice.

Pin `TestRun.ExitCode` as the OVERALL proof/execution command exit, not the directly observed child exit. The CLI
supplies a provisional status from its observed member/execution/cleanup outcomes after retaining those observations
separately. Writer must never rewrite a child/process/cleanup observation to disguise it as an observed failure.
The final persisted aggregate records why proof failed even when the child executed successfully.

The concrete sequence is:

1. Initial WriteRun: ExitCode=nil and CompletedAt zero remain absent; do not infer observed zero or set terminal time.
   It writes incomplete/unattested evidence at the stable run path.
2. CLI finishes execution/cleanup, retains individual outcomes, sets CompletedAt and provisional overall ExitCode.
3. Terminal WriteRun evaluates required identity/member/child/provenance evidence BEFORE marshalling. Preserve any
   supplied nonzero exit. For a required-proof invocation only, downgrade provisional zero to one if evidence or
   terminal chronology is incomplete/invalid. Recompute proof summary/EvidenceStatus from the finalized fields and
   project child metadata after child outcomes have been settled. Persist that first coherent terminal aggregate.
4. CLI returns the finalized `*run.ExitCode` only after successful required persistence. WriteRun error always makes
   the outer command nonzero; no returned path means no successful-write claim. The initial artifact may remain.
5. Loading must validate final fields without repairing a malformed terminal proof silently. A required invocation
   stored with overall zero and incomplete proof is invalid, not complete evidence with a cosmetic warning.

Reject or retain as incomplete any partial-terminal shape (only completion time or only exit); never synthesize the
missing observed state. Whether a controlled invalid caller returns a validation error or writes a failed terminal
record may follow the existing retained-failure API, but it cannot produce a successful command or complete proof.
Do not set CompletedAt inside Writer merely to make such a caller valid; the runner observes execution completion.

## Required versus legacy intent must survive to Writer

This is a necessary consequence of the accepted bounded adoption. An entirely legacy invocation may execute with
zero exit and explicit unattested evidence. Applying the required checker to every schema2 terminal run would turn
those allowed legacy executions red and contradict acceptance. Conversely, treating an empty RequiredMembers slice
as automatically legacy would let an invalid empty required selection escape.

Carry the resolver's explicit required-proof disposition into TestRunConfig. If no unambiguous field is already
present in the coordinated implementation, the concrete minimal representation is:

```go
RequireEvidence bool `json:"require_evidence"`
```

Its present consumers are cmd/e2e selection/initialization, Writer terminal finalization and LoadRun validation.
This is derived from the resolved selection; it is NOT a user-facing optional-strict flag. A required suite with an
empty set has RequireEvidence=true and fails, while a legacy diagnostic/execution-only selection has false, stays
unattested and preserves its execution exit. Complete named proof cannot be emitted for RequireEvidence=false.
The initial and terminal aggregate preserve this choice. Prefer this explicit boolean over nil-versus-empty
RequiredMembers: JSON omitempty currently collapses both, and even removing omitempty would make slice allocation
or reconstruction change the meaning of the same selected scope. A separate named intention round-trips clearly;
RequiredMembers remains only the membership list. Writer must reject a contradictory non-required run presented as
complete proof, and CLI's resolved required selections always set RequireEvidence=true. There is no second selector
catalog inside Writer and no public opt-out flag. Reader/Writer tests must exercise both branches; do not
infer it from a successful child, count, array length or schema version. This clarification carries accepted intent,
not a new proof mode or broader adoption decision. Root should propagate the chosen field spelling to both authors
and the API handoff; no independent acceptance interpreter belongs in CLI.

## Aggregate rewrite restriction

The accepted design requires same-run initial/final atomic aggregate replacement, unique invocation paths and
write-once child records. It does not require a new terminal-aggregate immutability API. The guard at writer.go229
was an implementation inference, not an owner ruling, and B does not depend on it.

Remove the unconditional aggregate terminal-refusal as a prerequisite to the public WriteRun contract, while keeping
cross-invocation identity/start/parent protection and child write-once behavior. No corrective terminal rewrite is
needed in the normal B flow: the FIRST terminal write must be coherent. This does not authorize constructing a new
run over an old path, overwriting child failures or discarding a recorded failed observation. A same-run repeat must
still satisfy the same accepted declaration/identity/outcome invariants. Do not build a monotonic history service or
new revision protocol to support hypothetical correction workflows. If implementation depends on changing a prior
terminal's observation history, stop and present that concrete need to root; it is outside this bounded ordering fix.

The smallest regression controls are a same-run initial->terminal write, a same terminal replay or permitted coherent
same-run update, and another-run/sibling collision refusal. A blanket terminal-refusal test must not be presented as
required by the accepted spec. Root can choose to retain a private stricter guard only if it does not create a new
caller obligation or block the agreed contract; it is not a reason to add preflight exports.

## Required focused verification, not yet run

Use existing WriteRun plus real CLI return-path tests to verify: initial nil exit remains nil; complete required
terminal zero stays zero; missing member/provenance with provisional zero persists final one; an original nonzero is
preserved; cleanup failure cannot be cleared; legacy zero/unattested remains allowed; required empty set fails;
write failure yields outer nonzero; reader rejects forged zero/incomplete required proof. The first terminal bytes
must already contain the final exit, with no dependence on a second corrective write. Preserve child/process/cleanup
status separately and assert it remains the originally observed value. Required-check failures and diagnostic failures
remain distinct. No proof is claimed from a method-return test alone when the CLI return status is the property.

## Limits and handoff

B fits the accepted outcome/persistence ownership; no new owner approval is needed for this sequencing detail.
Any broader adoption, provenance waiver, legacy refusal policy or new coordinator requires root's decision rather
than this clarification. This report does not approve current source: the new export is still present at the snapshot,
the terminal guard is still present, and implementation/tests must be reviewed after the agreed change stabilizes.
