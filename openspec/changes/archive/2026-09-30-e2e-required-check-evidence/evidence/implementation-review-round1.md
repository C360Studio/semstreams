# #1222 implementation review — Result/Writer slice, round 1

Mode: independent implementation review, read-only. Verdict: **CHANGES REQUESTED** for R1 and R2 below.
This is a five-file static slice review, not issue acceptance, merge approval, or independent reapproval of my
architecture/API advice. Implementation authors are separate. No tests, source edits, Docker, paid operations,
GitHub writes or state-changing Git commands were performed by this reviewer.

## Exact scope and authority

Worktree: /Users/coby/.codex/worktrees/e2e-required-proof/semstreams.
Base: fe6e2cc03e16f5db47e293f55939548572f204cc.
Accepted design: a31a5c690da763b124a7d1e1727836e57a270f943b5b61576216d7f386b9b48c.
Governing target: active e2e-required-check-evidence/specs/e2e-evidence/spec.md and
implementation-handoff.md, including accepted WriteRun terminal ordering and RequireEvidence clarification.
The reviewer contract was reread in full. Active tasks correctly leave implementation and verification unchecked.

The following hashes matched the author handoff before review and again after static inspection:

| File | SHA-256 |
| --- | --- |
| test/e2e/scenarios/scenario.go | 55944b35bb995c94b72d2f3376c78d170d4bbb8b128e2f7a3149f2ec1c5248e4 |
| test/e2e/scenarios/evidence.go | 5ffa1c2f0416a3bdcd672f03c1ad4952cfa9998bd9c68e6ad270285f61069c60 |
| test/e2e/scenarios/evidence_test.go | 823db1af71fa13646d85715ddc5b2792de697aa65185b8694cfed727ac5c6a58 |
| test/e2e/results/writer.go | 0ee3c6343c85c5d0209a9b668ca804cf5fa43ebbb3a047e9a8dacb304dae8fbe |
| test/e2e/results/writer_test.go | 7b306d9c83d75d7fd202bdffa42d034043ae36a05b638888af723eb0ddfd86df |

Author handoff: /private/tmp/semstreams-1222-result-developer.md,
SHA-256 0550d479f435ea4ae3ee9fa1cdabc6f747655b0bb957cc4a77648bd7b233f58d.
Tracked scenario.go/writer.go diff and complete new evidence.go/evidence_test.go/writer_test.go were read.
Active cmd/e2e implementation edits were deliberately excluded pending their own stable snapshot.

## Findings

### R1 — HIGH test/e2e/results/writer.go:420 — Validation changes a shared typed projection

Mechanism: checkRequiredEvidence copies Result by value, then FinalizeChecks synchronizes its Structured pointer.
The copy still aliases the original TieredResults. WriteRun initially projects child.Success at lines162–175, but
subsequent evaluation at lines178 and184 can overwrite that projection through the copy. For an otherwise valid
required child with all observations passed, set child.Success=false without an Error/Errors string and attach
Structured. The aggregate correctly fails because line415 observes the false Result, but FinalizeChecks on the
copy sets Success=true (evidence.go:150), then mutates the original Structured.Metadata.Success at line164.
The persisted Result remains false while the typed metadata becomes true. Conversely, a direct struct with stale
true Success and a missing observation can leave true Result beside false typed metadata. This violates the
accepted explicit invariant that emitted Result and TieredResults success agree.

Smallest fix: make validation free of mutations to the original child's typed projection (detach Structured from
validation copies, or equivalent), and project the actual finalized retained Result at the writer boundary.
Do not silently convert a false execution outcome into a successful child. Ensure LoadRun validation has the same
non-mutating behavior; it currently invokes the same shallow-copy path.

Refutation: the existing TestWriteRunProjectsFinalFailureIntoTierMetadata supplies a nonempty Error. That causes
copy finalization to stay false, so it does not cover the demonstrated false-without-error case. The aggregate
proof/exit does remain red; this is a concrete contradictory analysis signal, not a claim that the aggregate goes
green. No executed reproduction is claimed. Add direct-struct/round-trip controls with the false/no-error case
and missing-observation case, checking both persisted representations and input mutation.

### R2 — HIGH test/e2e/results/writer.go:444 — Complete proof accepts omitted retained provenance references

Mechanism: hasRequiredProvenance validates source/runner/application and combined input digests, but never requires
log_path/log_sha256 or artifact_manifest_path/artifact_manifest_sha256, despite allowing those fields at
lines499–500. There is also no retained absolute output-path field/reference requirement. The completeRunFixture
(writer_test.go:37–74) contains none of these references and is explicitly accepted as complete. Thus this is a
present acceptance hole, not merely an unimplemented Task collector: WriteRun and LoadRun can certify a terminal
required invocation with only opaque combined input digests and no retained constituent manifest/log reference.
The accepted handoff requires constituent paths/digests, and Run evidence binds one invocation requires absolute
output and log/artifact provenance. Runtime identity may not be replaced by unavailable/empty evidence.

Smallest fix: keep this obligation in the existing results-owned acceptance check; require the applicable retained
reference/digest fields and reject or retain unattested when unavailable. Record specific unavailable reasons.
Reconcile actual producer wiring in its later Task/CLI slice, rather than marking this synthetic fixture complete
under a temporarily weaker definition. The precise path representation can use the accepted existing map/reference
seam; a new framework or anti-forgery/authentication mechanism is neither needed nor requested.

Refutation and boundary: first-slice tests may use synthetic provenance and no real Docker observation is expected
here. Nor must this writer independently authenticate arbitrary authors or reopen relocated historical artifacts.
However synthetic test references still need to model the required record shape, and unavailable required fields
must not certify completeness. Task log capture, real input manifest production and CI retention remain pending
adoption work, not extra capability work demanded in this slice. Test omitted/unavailable manifest/log references
and absolute output evidence through both write and read acceptance.

## Conformance and refuted false-green candidates

| Accepted obligation | Evidence and result |
| --- | --- |
| One Result finalizer | evidence.go:83 owns the set decision; Writer reuses it rather than implementing check membership twice. |
| Sticky ignored record refusal | evidence.go:175 appends Errors; FinalizeChecks:135 incorporates them. Unknown/foreign/overwrite tests exercise it. |
| Missing/failed/skipped cannot pass | evidence.go:121–130 derives against requirements; stage Success is not an acceptance vote. |
| Declared required empty cannot pass | evidence.go:183 validates requirements; writer.go:402 refuses empty suite membership independently of explicit intent. |
| Diagnostics do not fill required evidence | evidence.go:121–124 and diagnostic-skip test preserve a diagnostic observation without incrementing required count. |
| First-terminal ordering | writer.go:177–189 downgrades provisional zero before marshal:201; initial nil stays nil and existing nonzero stays nonzero. |
| Legacy execution remains unattested | writer.go:340–350 and RequireEvidence=false path preserve distinction without forcing legacy zero exit to fail. |
| Foreign/duplicate/missing members | writer.go:405–439 rejects them; tests cover ordinary foreign and missing shapes. |
| Atomic same-invocation aggregate | writer.go:241–279 validates prior ID/start/parent and atomically links/replaces; run-ID naming prevents ordinary sibling collision. |
| Final domain projection | R1: aliasing violates emitted agreement on direct structs. |
| Complete invocation provenance | R2: record-shape acceptance is incomplete; actual producers remain future slices. |
| Required write failures | Writer returns errors normally. CLI propagation is intentionally awaiting separate stable review. |

A derived finalization error is rebuilt rather than appended repeatedly; original Error and Errors remain failures.
The proof check does not use vacuous legacy Summary.AllPassed as its acceptance oracle. Unknown schema and forged
zero-exit/incomplete required reports are refused. No second runtime owner or exported preflight validator was added.
This is a set-membership admission shape extending existing Result/Writer; reviewed inventory covers the related
ComponentResult.Required/Missing and IndexSpec.Required owners without deleting their unwired capabilities.

## Evidence quality and remaining bounded work

Retained author command output was read and hash verified:
/private/tmp/semstreams-1222-result-focused.log,
SHA-256 5ce33cf5871bc5320ce4d0d8d176120dda4cba11d2a3b322b5ad72c12448432b.
It records successful `go test ./test/e2e/scenarios ./test/e2e/results -count=1 -v`, including two Rapid properties
with100 generated cases each and native fuzz seed replay. This was not a reviewer-run command. Rapid's independent
status-vector and constructed omission oracle are appropriate, with exact active-spec citations. Generated1–6
sets cover ordinary membership but not empty declarations, direct malformed structs, or diagnostic-only declarations.
Those boundaries require deterministic cases. The handoff overstates deterministic empty/duplicate declaration
coverage: current Result tests cover duplicate observations/unknown identities, not empty/duplicate declarations.
Writer empty-member coverage does not substitute for Result declaration boundary coverage.

Historical behavioral RED claims have no retained raw artifact and remain UNVERIFIED. No mutation sensitivity,
-race, exploratory fuzz, real Task, Docker or hosted-retention proof is claimed. Required mutation deferral is accepted
only for this static checkpoint; it is not accepted as final verification of the guard changes. Later verification
must demonstrate missing-required and overwrite-failure sensitivity with byte-verified restoration.

Existing results.Compare (writer.go:577) still drops evidence disposition in its output. It must preserve explicit
analysis/legacy disposition before whole-change acceptance; the developer handoff already marks comparison-consumer
work pending. This is not a reason to invent another comparison engine or to block every isolated implementation
slice. Member write-once files likewise belong to the later present Task consumer, not phantom exports now.

Structural tool limit: no callable gopls tool is present in this resumed review runtime; earlier documented
workspace/cache restrictions were not retried. Bounded tracked fallback `git grep -n -E
'results\.(Compare|CreateTestRun|NewWriter)|\.WriteComparison\(' -- ':!cmd/e2e/*' ':!openspec/changes/*'`
found no other tracked production consumers. Full caller verification remains pending the frozen runner slice;
no claim of complete exported-surface adoption is made. Local `rg` enumerated Writer/finalizer/readers and typed
comparison names without reading active runner edits.

## Verdict

**CHANGES REQUESTED — R1 and R2.** Five reviewed files may be unfrozen for the owner's coordinated corrections.
The finding mechanisms above are statically established, not executed mutation results. Re-review the exact changed
bytes and regression evidence, then continue the separate CLI and actual scenario/Task adoption reviews.
